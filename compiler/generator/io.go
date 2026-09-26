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
			if isFileNode(node) {
				// File owns its own discovery; it never selects the IO pair.
				return nil
			}
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
	if isFileNode(node) {
		return renderFileOpen(node, state)
	}
	if !compilerTypes.IsIO(node.OperandType) {
		return "", unknownExpressionDiagnostic()
	}
	switch node.Name {
	case "stdin", "stdout", "stderr":
		return fmt.Sprintf("hex_io_open_%s(%d, %d)", node.Name, state.line(node.Span), state.column(node.Span)), nil
	}
	return "", unknownExpressionDiagnostic()
}

// renderBytesOver renders Bytes.over(buffer) directly to the core value.
func renderBytesOver(node checker.Expression, state *expressionValidation) (string, error) {
	if len(node.Arguments) != 1 || !compilerTypes.IsBytes(node.ResultType) {
		return "", unknownExpressionDiagnostic()
	}
	buffer, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return "hex_bytes_over(" + buffer + ")", nil
}

// isBytesReceiver reports whether a StreamMethodCallExpression's adapted
// receiver type is the Ptr<mut Bytes> form.
func isBytesReceiver(node checker.Expression) bool {
	return node.OperandType.Element != nil && compilerTypes.IsBytes(*node.OperandType.Element)
}

// renderStreamMethod renders one read/write/seek/close through its per-module
// result-union adapter. The receiver arrives already adapted: an IO value for
// OS-backed operations, a Ptr<mut Bytes> value for memory ones.
func renderStreamMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if isFileNode(node) {
		return renderFileMethod(node, state)
	}
	receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	site := fmt.Sprintf("%d, %d", state.line(node.Span), state.column(node.Span))
	suffix := streamAdapterSuffix(node.ResultType)
	memory := isBytesReceiver(node)
	prefix := "hex_io_"
	if memory {
		prefix = "hex_bytes_"
	}
	switch node.Name {
	case "read":
		if len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic()
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
			return "", unknownExpressionDiagnostic()
		}
		from, fromErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if fromErr != nil {
			return "", fromErr
		}
		return fmt.Sprintf("%swrite_%s(%s, %s, %s)", prefix, suffix, receiver, from, site), nil
	case "seek":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic()
		}
		to, toErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if toErr != nil {
			return "", toErr
		}
		return fmt.Sprintf("%sseek_%s(%s, %s, %s)", prefix, suffix, receiver, to, site), nil
	case "close":
		return fmt.Sprintf("hex_io_close_%s(%s, %s)", suffix, receiver, site), nil
	}
	return "", unknownExpressionDiagnostic()
}

// streamMemberRef resolves one union member's tag constant and payload field.
func streamMemberRef(tags *tagRegistry, union compilerTypes.Type, member compilerTypes.Type) (string, string) {
	index := unionMemberIndex(union, member)
	resolved, _ := compilerTypes.UnionMembers(union).At(index)
	return tags.unionMemberTag(resolved), tags.unionPayloadField(resolved)
}

// streamErrorArm spells one Error payload construction for a stream adapter
// whose failure carries a native code classified through hex_io_error.
func streamErrorArm(tags *tagRegistry, literals *literalRegistry, file string, union compilerTypes.Type, operation, code, payload string) (string, error) {
	handle, ok := literals.Lookup(payload)
	if !ok {
		return "", unknownExpressionDiagnostic()
	}
	tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = hex_io_error(line, column, %s, \"%s\", %s, &%s) }",
		union.CName, tag, field, file, operation, code, literals.CName(handle)), nil
}

// streamErrorArmWithKind spells one Error payload construction for a
// non-native stream failure (a capability mismatch or a Bytes contract
// violation): it carries no native code, so it constructs its ErrorKind
// directly instead of routing through hex_io_error's native classification.
func streamErrorArmWithKind(tags *tagRegistry, literals *literalRegistry, file string, union compilerTypes.Type, kindVariant, payload string) (string, error) {
	handle, ok := literals.Lookup(payload)
	if !ok {
		return "", unknownExpressionDiagnostic()
	}
	tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
	return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = (hex_t_Error){ .hex_m_file = %s, .hex_m_line = line, .hex_m_column = column, .hex_m_kind = (hex_t_ErrorKind){ .tag = %s }, .hex_m_message = hex_error_message(hex_text_heap(&%s)) } }",
		union.CName, tag, field, file, errorKindTag(tags, kindVariant), literals.CName(handle)), nil
}

// validateStreamConstructor checks one standard-handle constructor fail-closed:
// IO receiver shape, a known handle name, and exactly the IO | Error union.
func validateStreamConstructor(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if isFileNode(node) {
		return validateFileConstructor(node, expected, state)
	}
	if !compilerTypes.IsIO(node.OperandType) || node.ResultType.Union == nil {
		return unknownExpressionDiagnostic()
	}
	switch node.Name {
	case "stdin", "stdout", "stderr":
	default:
		return unknownExpressionDiagnostic()
	}
	if state.line(node.Span) == 0 || len(node.Arguments) != 0 || node.Operand != nil {
		return unknownExpressionDiagnostic()
	}
	members := compilerTypes.UnionMembers(node.ResultType)
	if members.Len() != 2 || !unionHasMember(members, compilerTypes.IOType) || !unionHasMember(members, compilerTypes.ErrorType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return nil
}

// validateBytesOverExpression checks Bytes.over(buffer): one List<Byte>
// argument producing Bytes.
func validateBytesOverExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand != nil || len(node.Arguments) != 1 || node.OperandType.List == nil ||
		node.OperandType.List.Element != compilerTypes.UInt8 || !compilerTypes.IsBytes(node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateCheckedOperandWithState(node.Arguments[0], state)
}

// validateStreamMethodCall checks one read/write/seek/close: receiver form,
// argument count and shapes, and exactly the operation's canonical union.
func validateStreamMethodCall(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if isFileNode(node) {
		return validateFileMethodCall(node, expected, state)
	}
	if node.Operand == nil {
		return unknownExpressionDiagnostic()
	}
	memory := isBytesReceiver(node)
	directIO := compilerTypes.IsIO(node.OperandType)
	if !memory && !directIO {
		return unknownExpressionDiagnostic()
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
			return unknownExpressionDiagnostic()
		}
	default:
		return unknownExpressionDiagnostic()
	}
	if len(node.Arguments) != arguments || node.ResultType.Union == nil {
		return unknownExpressionDiagnostic()
	}
	members := compilerTypes.UnionMembers(node.ResultType)
	if members.Len() != len(contractMembers) {
		return unknownExpressionDiagnostic()
	}
	for _, required := range contractMembers {
		if !unionHasMember(members, required) {
			return unknownExpressionDiagnostic()
		}
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
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

// ioOpenAdapterModel carries one standard stream opener's decided union
// type, stream name, success arm tag and payload field, and failure arm
// text.
type ioOpenAdapterModel struct {
	CName   string
	Name    string
	Success string
	Field   string
	Error   string
}

// streamReadAdapterModel carries one read adapter's decided suffix, union
// type, count and end-of-stream arm tags, count payload field, the
// operation-specific guard arm (not-readable or self-read), and the
// transfer-failure arm.
type streamReadAdapterModel struct {
	Suffix  string
	CName   string
	Success string
	Field   string
	EosTag  string
	Guard   string
	Error   string
}

// streamWriteAdapterModel carries one write adapter's decided suffix, union
// type, count arm tag and payload field, the operation-specific guard arm
// (not-writable or overlap), and the transfer-failure arm.
type streamWriteAdapterModel struct {
	Suffix  string
	CName   string
	Success string
	Field   string
	Guard   string
	Error   string
}

// seekAdapterModel carries one seek adapter's decided receiver core,
// suffix, parameter type, per-origin tag and call triples, union type,
// success arm tag and payload field, and failure arm text.
type seekAdapterModel struct {
	CName       string
	Core        string
	Suffix      string
	Receiver    string
	StartTag    string
	StartCall   string
	CurrentTag  string
	CurrentCall string
	EndTag      string
	EndCall     string
	Success     string
	Field       string
	Error       string
}

// ioCloseAdapterModel carries one closer's decided suffix, union type,
// success arm tag, and failure arm text.
type ioCloseAdapterModel struct {
	Suffix  string
	CName   string
	Success string
	Error   string
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
	errorArmWithKind := func(kindVariant, payload string, union compilerTypes.Type) (string, error) {
		return streamErrorArmWithKind(tags, literals, file, union, kindVariant, payload)
	}

	if state.constructor && len(state.openUnions) > 0 {
		union := state.openUnions[0]
		ioTag, ioField := streamMemberRef(tags, union, compilerTypes.IOType)
		failure, armErr := errorArm("open", "opened.code", streamMessageOpen, union)
		if armErr != nil {
			return armErr
		}
		for _, name := range []string{"stdin", "stdout", "stderr"} {
			if err := renderInto(result, "module.h", "io_open_adapter", ioOpenAdapterModel{
				CName:   union.CName,
				Name:    name,
				Success: ioTag,
				Field:   ioField,
				Error:   failure,
			}); err != nil {
				return err
			}
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
			notReadable, armErr := errorArmWithKind("PermissionDenied", streamMessageNotReadable, union)
			if armErr != nil {
				return armErr
			}
			if err := renderInto(result, "module.h", "io_read_adapter", streamReadAdapterModel{
				Suffix:  streamAdapterSuffix(union),
				CName:   union.CName,
				Success: sizeTag,
				Field:   sizeField,
				EosTag:  eosTag,
				Guard:   notReadable,
				Error:   readFailed,
			}); err != nil {
				return err
			}
		}
		if state.readBytes {
			selfRead, armErr := errorArmWithKind("InvalidInput", streamMessageSelfRead, union)
			if armErr != nil {
				return armErr
			}
			if err := renderInto(result, "module.h", "bytes_read_adapter", streamReadAdapterModel{
				Suffix:  streamAdapterSuffix(union),
				CName:   union.CName,
				Success: sizeTag,
				Field:   sizeField,
				EosTag:  eosTag,
				Guard:   selfRead,
				Error:   readFailed,
			}); err != nil {
				return err
			}
		}
	}

	for _, union := range state.writeUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
		writeFailed, armErr := errorArm("write", "transfer.code", streamMessageWriteFailed, union)
		if armErr != nil {
			return armErr
		}
		if state.writeIO {
			notWritable, armErr := errorArmWithKind("PermissionDenied", streamMessageNotWritable, union)
			if armErr != nil {
				return armErr
			}
			if err := renderInto(result, "module.h", "io_write_adapter", streamWriteAdapterModel{
				Suffix:  streamAdapterSuffix(union),
				CName:   union.CName,
				Success: sizeTag,
				Field:   sizeField,
				Guard:   notWritable,
				Error:   writeFailed,
			}); err != nil {
				return err
			}
		}
		if state.writeBytes {
			overlap, armErr := errorArmWithKind("InvalidInput", streamMessageOverlap, union)
			if armErr != nil {
				return armErr
			}
			if err := renderInto(result, "module.h", "bytes_write_adapter", streamWriteAdapterModel{
				Suffix:  streamAdapterSuffix(union),
				CName:   union.CName,
				Success: sizeTag,
				Field:   sizeField,
				Guard:   overlap,
				Error:   writeFailed,
			}); err != nil {
				return err
			}
		}
	}

	for _, union := range state.seekUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
		seekFailed, armErr := errorArm("seek", "moved.code", streamMessageSeekFailed, union)
		if armErr != nil {
			return armErr
		}
		emit := func(core string, receiverType string, startCall string, currentCall string, endCall string) error {
			if err := renderInto(result, "module.h", "seek_adapter", seekAdapterModel{
				CName:       union.CName,
				Core:        core,
				Suffix:      streamAdapterSuffix(union),
				Receiver:    receiverType,
				StartTag:    streamSeekTag(tags, 0),
				StartCall:   startCall,
				CurrentTag:  streamSeekTag(tags, 1),
				CurrentCall: currentCall,
				EndTag:      streamSeekTag(tags, 2),
				EndCall:     endCall,
				Success:     sizeTag,
				Field:       sizeField,
				Error:       seekFailed,
			}); err != nil {
				return err
			}
			return nil
		}
		if state.seekIO {
			if err := emit("hex_io_", "hex_io",
				"hex_io_seek_start(stream, to.payload.Start.hex_m_position)",
				"hex_io_seek_current(stream, to.payload.Current.hex_m_offset)",
				"hex_io_seek_end(stream, to.payload.End.hex_m_offset)"); err != nil {
				return err
			}
		}
		if state.seekBytes {
			if err := emit("hex_bytes_", "hex_bytes *",
				"hex_bytes_seek_from(stream, 0u, (int64_t)to.payload.Start.hex_m_position)",
				"hex_bytes_seek_from(stream, 1u, to.payload.Current.hex_m_offset)",
				"hex_bytes_seek_from(stream, 2u, to.payload.End.hex_m_offset)"); err != nil {
				return err
			}
		}
	}

	for _, union := range state.closeUnions {
		nilTag, _ := streamMemberRef(tags, union, compilerTypes.Nil)
		closeFailed, armErr := errorArm("close", "closed.code", streamMessageCloseFailed, union)
		if armErr != nil {
			return armErr
		}
		if state.closeIO {
			if err := renderInto(result, "module.h", "io_close_adapter", ioCloseAdapterModel{
				Suffix:  streamAdapterSuffix(union),
				CName:   union.CName,
				Success: nilTag,
				Error:   closeFailed,
			}); err != nil {
				return err
			}
		}
	}
	return nil
}
