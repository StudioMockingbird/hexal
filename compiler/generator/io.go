package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The byte-stream component's Go side: discovery of checked stream
// expressions, call-site rendering, and the module-owned inline adapters that
// translate core transfer results into each structural result union. Adapter
// names derive from the canonical union's C name, so renderer and writer
// agree without a registry.

// Failure message payloads. Discovery interns exactly the payloads the
// reachable families need; rendering resolves them from the shared registry.
const (
	streamMessageOpen        = "open failed"
	streamMessageReadFailed  = "read failed"
	streamMessageNotReadable = "stream is not readable"
	streamMessageSelfRead    = "memory stream cannot read into its backing list"
	streamMessageWriteFailed = "write failed"
	streamMessageNotWritable = "stream is not writable"
	streamMessageOverlap     = "memory stream cannot write from its backing list"
	streamMessageSeekFailed  = "seek failed"
	streamMessageCloseFailed = "close failed"
)

// generatedStreamState records which stream families one module uses and the
// distinct result unions they produce.
type generatedStreamState struct {
	used        bool
	constructor bool // any IO.stdin/stdout/stderr
	bytesOver   bool
	readIO      bool
	readBytes   bool
	writeIO     bool
	writeBytes  bool
	seekIO      bool
	seekBytes   bool
	closeIO     bool
	openUnions  []compilerTypes.Type
	readUnions  []compilerTypes.Type
	writeUnions []compilerTypes.Type
	seekUnions  []compilerTypes.Type
	closeUnions []compilerTypes.Type
	fileLiteral literalHandle
}

func appendUnionOnce(order []compilerTypes.Type, union compilerTypes.Type) []compilerTypes.Type {
	for _, existing := range order {
		if existing.CName == union.CName {
			return order
		}
	}
	return append(order, union)
}

// discoverGeneratedStreams walks one module's checked program collecting the
// stream families it uses. The module's source key interns as the Error file
// literal every failure construction at this module's call sites carries.
func discoverGeneratedStreams(program checker.Program, logicalKey string, literals *literalRegistry) *generatedStreamState {
	state := &generatedStreamState{}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.StreamConstructorExpression:
				state.used = true
				state.constructor = true
				state.openUnions = appendUnionOnce(state.openUnions, node.ResultType)
			case checker.BytesOverExpression:
				state.used = true
				state.bytesOver = true
			case checker.StreamMethodCallExpression:
				state.used = true
				memory := isBytesReceiver(node)
				switch node.Name {
				case "read":
					if memory {
						state.readBytes = true
					} else {
						state.readIO = true
					}
					state.readUnions = appendUnionOnce(state.readUnions, node.ResultType)
				case "write":
					if memory {
						state.writeBytes = true
					} else {
						state.writeIO = true
					}
					state.writeUnions = appendUnionOnce(state.writeUnions, node.ResultType)
				case "seek":
					if memory {
						state.seekBytes = true
					} else {
						state.seekIO = true
					}
					state.seekUnions = appendUnionOnce(state.seekUnions, node.ResultType)
				case "close":
					state.closeIO = true
					state.closeUnions = appendUnionOnce(state.closeUnions, node.ResultType)
				}
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	if state.used {
		state.fileLiteral = literals.Intern(logicalKey)
		internStreamMessages(state, literals)
	}
	return state
}

// internStreamMessages registers the failure payloads of every reachable
// family so rendering resolves them deterministically.
func internStreamMessages(state *generatedStreamState, literals *literalRegistry) {
	intern := func(familyUsed bool, payloads ...string) {
		if !familyUsed {
			return
		}
		for _, payload := range payloads {
			literals.Intern(payload)
		}
	}
	intern(state.constructor, streamMessageOpen)
	intern(state.readIO, streamMessageReadFailed, streamMessageNotReadable)
	intern(state.readBytes, streamMessageReadFailed, streamMessageSelfRead)
	intern(state.writeIO, streamMessageWriteFailed, streamMessageNotWritable)
	intern(state.writeBytes, streamMessageWriteFailed, streamMessageOverlap)
	intern(state.seekIO || state.seekBytes, streamMessageSeekFailed)
	intern(state.closeIO, streamMessageCloseFailed)
}

// streamAdapterSuffix derives the deterministic adapter-name stem from the
// canonical result union.
func streamAdapterSuffix(union compilerTypes.Type) string {
	return strings.TrimPrefix(union.CName, "hex_t_")
}

// renderStreamConstructor renders one standard-handle constructor through its
// per-module open adapter.
func renderStreamConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if !compilerTypes.IsIO(node.OperandType) {
		return "", unknownExpressionDiagnostic("stream constructor without a checked IO result")
	}
	switch node.Name {
	case "stdin", "stdout", "stderr":
		return fmt.Sprintf("hex_io_open_%s(%d, %d)", node.Name, node.SourceLine, node.SourceColumn), nil
	}
	return "", unknownExpressionDiagnostic("unknown stream constructor " + node.Name)
}

// renderBytesOver renders Bytes.over(buffer) directly to the core value.
func renderBytesOver(node checker.Expression, state *expressionValidation) (string, error) {
	if len(node.Arguments) != 1 || !compilerTypes.IsBytes(node.ResultType) {
		return "", unknownExpressionDiagnostic("bytes-over without a checked list")
	}
	buffer, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return "hex_bytes_over(" + buffer + ")", nil
}

// isBytesReceiver reports whether a StreamMethodCallExpression's adapted
// receiver type is the MutPtr<Bytes> form.
func isBytesReceiver(node checker.Expression) bool {
	return node.OperandType.Element != nil && compilerTypes.IsBytes(*node.OperandType.Element)
}

// renderStreamMethod renders one read/write/seek/close through its per-module
// result-union adapter. The receiver arrives already adapted: an IO value for
// OS-backed operations, a MutPtr<Bytes> value for memory ones.
func renderStreamMethod(node checker.Expression, state *expressionValidation) (string, error) {
	receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	site := fmt.Sprintf("%d, %d", node.SourceLine, node.SourceColumn)
	suffix := streamAdapterSuffix(node.ResultType)
	memory := isBytesReceiver(node)
	prefix := "hex_io_"
	if memory {
		prefix = "hex_bytes_"
	}
	switch node.Name {
	case "read":
		if len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic("stream read without checked arguments")
		}
		into, intoErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if intoErr != nil {
			return "", intoErr
		}
		maximum, maxErr := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
		if maxErr != nil {
			return "", maxErr
		}
		return fmt.Sprintf("%sread_%s(%s, %s, %s, %s)", prefix, suffix, receiver, into, maximum, site), nil
	case "write":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("stream write without a checked view")
		}
		from, fromErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if fromErr != nil {
			return "", fromErr
		}
		return fmt.Sprintf("%swrite_%s(%s, %s, %s)", prefix, suffix, receiver, from, site), nil
	case "seek":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("stream seek without a checked position")
		}
		to, toErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if toErr != nil {
			return "", toErr
		}
		return fmt.Sprintf("%sseek_%s(%s, %s, %s)", prefix, suffix, receiver, to, site), nil
	case "close":
		return fmt.Sprintf("hex_io_close_%s(%s, %s)", suffix, receiver, site), nil
	}
	return "", unknownExpressionDiagnostic("unknown stream operation " + node.Name)
}

// streamMemberRef resolves one union member's tag constant and payload field.
func streamMemberRef(tags *tagRegistry, union compilerTypes.Type, member compilerTypes.Type) (string, string) {
	index := unionMemberIndex(union, member)
	resolved, _ := compilerTypes.UnionMembers(union).At(index)
	return tags.unionMemberTag(resolved), tags.unionPayloadField(resolved)
}

// streamErrorArm spells one Error payload construction for a stream adapter.
func streamErrorArm(tags *tagRegistry, literals *literalRegistry, file string, union compilerTypes.Type, operation, code, payload string) (string, error) {
	handle, ok := literals.Lookup(payload)
	if !ok {
		return "", unknownExpressionDiagnostic("stream failure message is missing from the literal registry: " + payload)
	}
	tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = hex_io_error(line, column, %s, \"%s\", %s, &%s) }",
		union.CName, tag, field, file, operation, code, literals.CName(handle)), nil
}

// validateStreamConstructor checks one standard-handle constructor fail-closed:
// IO receiver shape, a known handle name, and exactly the IO | Error union.
func validateStreamConstructor(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if !compilerTypes.IsIO(node.OperandType) || node.ResultType.Union == nil {
		return unknownExpressionDiagnostic("stream constructor has invalid checked metadata")
	}
	switch node.Name {
	case "stdin", "stdout", "stderr":
	default:
		return unknownExpressionDiagnostic("unknown stream constructor in checked metadata")
	}
	if node.SourceLine == 0 || len(node.Arguments) != 0 || node.Operand != nil {
		return unknownExpressionDiagnostic("stream constructor carries incomplete metadata")
	}
	members := compilerTypes.UnionMembers(node.ResultType)
	if members.Len() != 2 || !unionHasMember(members, compilerTypes.IOType) || !unionHasMember(members, compilerTypes.ErrorType) {
		return unknownExpressionDiagnostic("stream constructor result is not IO | Error")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("stream constructor result does not match its expected type")
	}
	return nil
}

// validateBytesOverExpression checks Bytes.over(buffer): one List<Byte>
// argument producing Bytes.
func validateBytesOverExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand != nil || len(node.Arguments) != 1 || node.OperandType.List == nil ||
		node.OperandType.List.Element != compilerTypes.UInt8 || !compilerTypes.IsBytes(node.ResultType) {
		return unknownExpressionDiagnostic("bytes-over has invalid checked metadata")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("bytes-over result does not match its expected type")
	}
	return validateCheckedOperandWithState(node.Arguments[0], state)
}

// validateStreamMethodCall checks one read/write/seek/close: receiver form,
// argument count and shapes, and exactly the operation's canonical union.
func validateStreamMethodCall(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("stream method without a checked receiver")
	}
	memory := isBytesReceiver(node)
	directIO := compilerTypes.IsIO(node.OperandType)
	if !memory && !directIO {
		return unknownExpressionDiagnostic("stream method receiver is neither IO nor MutPtr<Bytes>")
	}
	var arguments int
	var contractMembers []compilerTypes.Type
	switch node.Name {
	case "read":
		arguments = 2
		contractMembers = []compilerTypes.Type{compilerTypes.SizeType, compilerTypes.EoS, compilerTypes.ErrorType}
	case "write":
		arguments = 1
		contractMembers = []compilerTypes.Type{compilerTypes.SizeType, compilerTypes.ErrorType}
	case "seek":
		arguments = 1
		contractMembers = []compilerTypes.Type{compilerTypes.SizeType, compilerTypes.ErrorType}
	case "close":
		arguments = 0
		contractMembers = []compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType}
		if memory {
			return unknownExpressionDiagnostic("close exists only on IO streams")
		}
	default:
		return unknownExpressionDiagnostic("unknown stream operation in checked metadata")
	}
	if len(node.Arguments) != arguments || node.ResultType.Union == nil {
		return unknownExpressionDiagnostic("stream method has invalid checked metadata")
	}
	members := compilerTypes.UnionMembers(node.ResultType)
	if members.Len() != len(contractMembers) {
		return unknownExpressionDiagnostic("stream method result members do not match its contract")
	}
	for _, required := range contractMembers {
		if !unionHasMember(members, required) {
			return unknownExpressionDiagnostic("stream method result is missing a contract member")
		}
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("stream method result does not match its expected type")
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return err
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

func unionHasMember(members compilerTypes.UnionMemberView, member compilerTypes.Type) bool {
	for index := 0; index < members.Len(); index++ {
		if candidate, _ := members.At(index); compilerTypes.Equal(candidate, member) {
			return true
		}
	}
	return false
}

// Seek variant discriminants come from the compiler-owned ADT record in
// declaration order: Start, Current, End. The payload field spellings are
// fixed by that same record.
func streamSeekTag(tags *tagRegistry, index int) string {
	return tags.adtVariantTag(compilerTypes.SeekType.Adt, index)
}

// writeStreamInlineHelpers emits the module-owned stream adapters after every
// family that can fail. Each adapter wraps one structural result union around
// the component core and constructs failures with this module's file literal
// and the shared static failure messages.
func writeStreamInlineHelpers(result *strings.Builder, state *generatedStreamState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || !state.used {
		return nil
	}
	file := "&" + literals.CName(state.fileLiteral)
	errorArm := func(operation, code, payload string, union compilerTypes.Type) (string, error) {
		return streamErrorArm(tags, literals, file, union, operation, code, payload)
	}

	if state.constructor && len(state.openUnions) > 0 {
		union := state.openUnions[0]
		ioTag, ioField := streamMemberRef(tags, union, compilerTypes.IOType)
		failure, armErr := errorArm("open", "opened.code", streamMessageOpen, union)
		if armErr != nil {
			return armErr
		}
		for _, name := range []string{"stdin", "stdout", "stderr"} {
			fmt.Fprintf(result,
				"\nstatic inline %s hex_io_open_%s(size_t line, size_t column) {\n"+
					"    hex_io_open opened = hex_io_%s();\n"+
					"    if (opened.status == HEX_IO_OK) {\n"+
					"        return (%s){ .tag = %s, .payload.%s = opened.stream };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				union.CName, name, name,
				union.CName, ioTag, ioField,
				failure)
		}
	}

	for _, union := range state.readUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
		eosTag, _ := streamMemberRef(tags, union, compilerTypes.EoS)
		readFailed, armErr := errorArm("read", "transfer.code", streamMessageReadFailed, union)
		if armErr != nil {
			return armErr
		}
		if state.readIO {
			notReadable, armErr := errorArm("read", "0", streamMessageNotReadable, union)
			if armErr != nil {
				return armErr
			}
			fmt.Fprintf(result,
				"\nstatic inline %s hex_io_read_%s(hex_io stream, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {\n"+
					"    hex_io_transfer transfer = hex_io_read(stream, into, max);\n"+
					"    switch (transfer.status) {\n"+
					"    case HEX_IO_OK:\n"+
					"        return (%s){ .tag = %s, .payload.%s = transfer.count };\n"+
					"    case HEX_IO_EOS:\n"+
					"        return (%s){ .tag = %s };\n"+
					"    case HEX_IO_NOT_READABLE:\n"+
					"        return %s;\n"+
					"    default:\n"+
					"        return %s;\n"+
					"    }\n"+
					"}\n",
				union.CName, streamAdapterSuffix(union),
				union.CName, sizeTag, sizeField,
				union.CName, eosTag,
				notReadable,
				readFailed)
		}
		if state.readBytes {
			selfRead, armErr := errorArm("read", "0", streamMessageSelfRead, union)
			if armErr != nil {
				return armErr
			}
			fmt.Fprintf(result,
				"\nstatic inline %s hex_bytes_read_%s(hex_bytes *stream, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {\n"+
					"    hex_io_transfer transfer = hex_bytes_read(stream, into, max);\n"+
					"    switch (transfer.status) {\n"+
					"    case HEX_IO_OK:\n"+
					"        return (%s){ .tag = %s, .payload.%s = transfer.count };\n"+
					"    case HEX_IO_EOS:\n"+
					"        return (%s){ .tag = %s };\n"+
					"    case HEX_IO_SELF_READ:\n"+
					"        return %s;\n"+
					"    default:\n"+
					"        return %s;\n"+
					"    }\n"+
					"}\n",
				union.CName, streamAdapterSuffix(union),
				union.CName, sizeTag, sizeField,
				union.CName, eosTag,
				selfRead,
				readFailed)
		}
	}

	for _, union := range state.writeUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
		writeFailed, armErr := errorArm("write", "transfer.code", streamMessageWriteFailed, union)
		if armErr != nil {
			return armErr
		}
		if state.writeIO {
			notWritable, armErr := errorArm("write", "0", streamMessageNotWritable, union)
			if armErr != nil {
				return armErr
			}
			fmt.Fprintf(result,
				"\nstatic inline %s hex_io_write_%s(hex_io stream, hex_view_UInt8 from, size_t line, size_t column) {\n"+
					"    hex_io_transfer transfer = hex_io_write(stream, from);\n"+
					"    switch (transfer.status) {\n"+
					"    case HEX_IO_OK:\n"+
					"        return (%s){ .tag = %s, .payload.%s = transfer.count };\n"+
					"    case HEX_IO_NOT_WRITABLE:\n"+
					"        return %s;\n"+
					"    default:\n"+
					"        return %s;\n"+
					"    }\n"+
					"}\n",
				union.CName, streamAdapterSuffix(union),
				union.CName, sizeTag, sizeField,
				notWritable,
				writeFailed)
		}
		if state.writeBytes {
			overlap, armErr := errorArm("write", "0", streamMessageOverlap, union)
			if armErr != nil {
				return armErr
			}
			fmt.Fprintf(result,
				"\nstatic inline %s hex_bytes_write_%s(hex_bytes *stream, hex_view_UInt8 from, size_t line, size_t column) {\n"+
					"    hex_io_transfer transfer = hex_bytes_write(stream, from);\n"+
					"    switch (transfer.status) {\n"+
					"    case HEX_IO_OK:\n"+
					"        return (%s){ .tag = %s, .payload.%s = transfer.count };\n"+
					"    case HEX_IO_OVERLAP:\n"+
					"        return %s;\n"+
					"    default:\n"+
					"        return %s;\n"+
					"    }\n"+
					"}\n",
				union.CName, streamAdapterSuffix(union),
				union.CName, sizeTag, sizeField,
				overlap,
				writeFailed)
		}
	}

	for _, union := range state.seekUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
		seekFailed, armErr := errorArm("seek", "moved.code", streamMessageSeekFailed, union)
		if armErr != nil {
			return armErr
		}
		emit := func(core string, receiverType string, startCall string, currentCall string, endCall string) {
			fmt.Fprintf(result,
				"\nstatic inline %s %sseek_%s(%s stream, hex_t_Seek to, size_t line, size_t column) {\n"+
					"    hex_io_position moved;\n"+
					"    switch (to.tag) {\n"+
					"    case %s:\n"+
					"        moved = %s;\n"+
					"        break;\n"+
					"    case %s:\n"+
					"        moved = %s;\n"+
					"        break;\n"+
					"    case %s:\n"+
					"        moved = %s;\n"+
					"        break;\n"+
					"    default:\n"+
					"        abort();\n"+
					"    }\n"+
					"    if (moved.status == HEX_IO_OK) {\n"+
					"        return (%s){ .tag = %s, .payload.%s = (size_t)moved.position };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				union.CName, core, streamAdapterSuffix(union), receiverType,
				streamSeekTag(tags, 0), startCall,
				streamSeekTag(tags, 1), currentCall,
				streamSeekTag(tags, 2), endCall,
				union.CName, sizeTag, sizeField,
				seekFailed)
		}
		if state.seekIO {
			emit("hex_io_", "hex_io",
				"hex_io_seek_start(stream, to.payload.Start.hex_m_position)",
				"hex_io_seek_current(stream, to.payload.Current.hex_m_offset)",
				"hex_io_seek_end(stream, to.payload.End.hex_m_offset)")
		}
		if state.seekBytes {
			emit("hex_bytes_", "hex_bytes *",
				"hex_bytes_seek_from(stream, 0u, (int64_t)to.payload.Start.hex_m_position)",
				"hex_bytes_seek_from(stream, 1u, to.payload.Current.hex_m_offset)",
				"hex_bytes_seek_from(stream, 2u, to.payload.End.hex_m_offset)")
		}
	}

	for _, union := range state.closeUnions {
		nilTag, _ := streamMemberRef(tags, union, compilerTypes.Nil)
		closeFailed, armErr := errorArm("close", "closed.code", streamMessageCloseFailed, union)
		if armErr != nil {
			return armErr
		}
		if state.closeIO {
			fmt.Fprintf(result,
				"\nstatic inline %s hex_io_close_%s(hex_io stream, size_t line, size_t column) {\n"+
					"    hex_io_status_only closed = hex_io_close(stream);\n"+
					"    if (closed.status == HEX_IO_OK) {\n"+
					"        return (%s){ .tag = %s };\n"+
					"    }\n"+
					"    return %s;\n"+
					"}\n",
				union.CName, streamAdapterSuffix(union),
				union.CName, nilTag,
				closeFailed)
		}
	}
	return nil
}
