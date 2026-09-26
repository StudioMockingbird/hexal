package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The File family's Go side. File reuses the stream expression
// kinds; io.go routes every node whose operand type is File here, so File
// never selects the IO component and IO never selects File.

const (
	fileMessageOpen        = "file open failed"
	fileMessageRead        = "file read failed"
	fileMessageWrite       = "file write failed"
	fileMessageSeek        = "file seek failed"
	fileMessageFlush       = "file flush failed"
	fileMessageClose       = "file close failed"
	fileMessageNotReadable = "file is not readable"
	fileMessageNotWritable = "file is not writable"
)

// generatedFileState records one module's (or the merged program's) File
// operations and the result unions each produces.
type generatedFileState struct {
	used        bool
	seek        bool
	openUnions  []compilerTypes.Type
	readUnions  []compilerTypes.Type
	writeUnions []compilerTypes.Type
	seekUnions  []compilerTypes.Type
	flushUnions []compilerTypes.Type
	closeUnions []compilerTypes.Type
	fileLiteral literalHandle
}

// isFileNode reports whether a stream expression acts on File.
func isFileNode(node checker.Expression) bool {
	return compilerTypes.IsFile(node.OperandType)
}

// discoverGeneratedFiles walks one module for File types and operations.
func discoverGeneratedFiles(program checker.Program, logicalKey string, literals *literalRegistry) *generatedFileState {
	state := &generatedFileState{}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsFile(typ) || compilerTypes.IsFileMode(typ) {
				state.used = true
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if (node.Kind != checker.StreamConstructorExpression && node.Kind != checker.StreamMethodCallExpression) || !isFileNode(node) {
				return nil
			}
			state.used = true
			if node.Kind == checker.StreamConstructorExpression {
				state.openUnions = appendUnionOnce(state.openUnions, node.ResultType)
				return nil
			}
			switch node.Name {
			case "read":
				state.readUnions = appendUnionOnce(state.readUnions, node.ResultType)
			case "write":
				state.writeUnions = appendUnionOnce(state.writeUnions, node.ResultType)
			case "seek":
				state.seek = true
				state.seekUnions = appendUnionOnce(state.seekUnions, node.ResultType)
			case "flush":
				state.flushUnions = appendUnionOnce(state.flushUnions, node.ResultType)
			case "close":
				state.closeUnions = appendUnionOnce(state.closeUnions, node.ResultType)
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	if state.used {
		state.fileLiteral = literals.Intern(logicalKey)
		for _, message := range []string{fileMessageOpen, fileMessageRead, fileMessageWrite, fileMessageSeek, fileMessageFlush, fileMessageClose, fileMessageNotReadable, fileMessageNotWritable} {
			literals.Intern(message)
		}
	}
	return state
}

// renderFileOpen renders File.open through its per-module adapter.
func renderFileOpen(node checker.Expression, state *expressionValidation) (string, error) {
	if len(node.Arguments) != 2 {
		return "", unknownExpressionDiagnostic()
	}
	path, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	mode, err := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_file_open_%s(%s, %s, %d, %d)", streamAdapterSuffix(node.ResultType), path, mode, state.line(node.Span), state.column(node.Span)), nil
}

// renderFileMethod renders one File operation through its per-module adapter.
func renderFileMethod(node checker.Expression, state *expressionValidation) (string, error) {
	receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	arguments := []string{receiver}
	for index := range node.Arguments {
		rendered, err := renderHoistedOperand(&node.Arguments[index].Node, node.Arguments[index], state)
		if err != nil {
			return "", err
		}
		arguments = append(arguments, rendered)
	}
	return fileMethodCall(node, arguments, state)
}

// fileMethodCall spells one File operation over rendered operands, receiver
// first; the deferred-close path shares it.
func fileMethodCall(node checker.Expression, arguments []string, state *expressionValidation) (string, error) {
	want := map[string]int{"read": 3, "write": 2, "seek": 2, "flush": 1, "close": 1}[node.Name]
	if want == 0 || len(arguments) != want {
		return "", unknownExpressionDiagnostic()
	}
	return fmt.Sprintf("hex_file_%s_%s(%s, %d, %d)", node.Name, streamAdapterSuffix(node.ResultType), strings.Join(arguments, ", "), state.line(node.Span), state.column(node.Span)), nil
}

// validateFileConstructor checks File.open fail-closed.
func validateFileConstructor(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Name != "open" || len(node.Arguments) != 2 || node.Operand != nil || state.line(node.Span) == 0 ||
		!compilerTypes.IsString(node.Arguments[0].Type) || !compilerTypes.IsFileMode(node.Arguments[1].Type) {
		return unknownExpressionDiagnostic()
	}
	members := compilerTypes.UnionMembers(node.ResultType)
	if node.ResultType.Union == nil || members.Len() != 2 || !unionHasMember(members, compilerTypes.FileType) || !unionHasMember(members, compilerTypes.ErrorType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

// validateFileMethodCall checks one File operation fail-closed.
func validateFileMethodCall(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	var arguments int
	var contract []compilerTypes.Type
	switch node.Name {
	case "read":
		arguments, contract = 2, []compilerTypes.Type{compilerTypes.SizeType, compilerTypes.EoS, compilerTypes.ErrorType}
	case "write", "seek":
		arguments, contract = 1, []compilerTypes.Type{compilerTypes.SizeType, compilerTypes.ErrorType}
	case "flush", "close":
		arguments, contract = 0, []compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType}
	default:
		return unknownExpressionDiagnostic()
	}
	members := compilerTypes.UnionMembers(node.ResultType)
	if node.Operand == nil || len(node.Arguments) != arguments || node.ResultType.Union == nil || members.Len() != len(contract) || state.line(node.Span) == 0 {
		return unknownExpressionDiagnostic()
	}
	for _, required := range contract {
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

// writeFileInlineHelpers emits the module-owned File adapters: each wraps the
// component core in one structural result union and builds failures with this
// module's file literal, the portable header the core selects, and a static
// operation message.
// fileOpenAdapterModel carries one file opener's decided union type, name
// suffix, file-mode case text, open arm tag and payload field, and failure
// arm text.
type fileOpenAdapterModel struct {
	CName   string
	Suffix  string
	Cases   string
	Success string
	Field   string
	Failure string
}

// fileSizeAdapterModel carries one file size-operation adapter's decided
// union type, operation, name suffix, parameter list, transfer call,
// count arm tag and payload field, optional guard text, and failure arm
// text.
type fileSizeAdapterModel struct {
	CName      string
	Operation  string
	Suffix     string
	Parameters string
	Call       string
	Success    string
	Field      string
	Gate       string
	Failure    string
}

// fileStatusAdapterModel carries one file status-operation adapter's
// decided union type, operation, name suffix, success arm tag, optional
// guard text, and failure arm text.
type fileStatusAdapterModel struct {
	CName     string
	Operation string
	Suffix    string
	Success   string
	Gate      string
	Error     string
}

func writeFileInlineHelpers(result *strings.Builder, state *generatedFileState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || !state.used {
		return nil
	}
	file := "&" + literals.CName(state.fileLiteral)
	message := func(payload string) (string, error) {
		handle, ok := literals.Lookup(payload)
		if !ok {
			return "", unknownExpressionDiagnostic()
		}
		return "&" + literals.CName(handle), nil
	}
	errorArm := func(union compilerTypes.Type, status string, opening bool, payload string) (string, error) {
		text, err := message(payload)
		if err != nil {
			return "", err
		}
		tag, field := streamMemberRef(tags, union, compilerTypes.ErrorType)
		return fmt.Sprintf("(%s){ .tag = %s, .payload.%s = hex_file_error(line, column, %s, %s, %t, %s) }", union.CName, tag, field, file, status, opening, text), nil
	}

	for _, union := range state.openUnions {
		fileTag, fileField := streamMemberRef(tags, union, compilerTypes.FileType)
		failure, err := errorArm(union, "opened.status", true, fileMessageOpen)
		if err != nil {
			return err
		}
		var cases strings.Builder
		for index := range compilerTypes.FileModeType.Adt.Variants {
			if err := renderInto(&cases, "module.h", "file_mode_case", formCaseModel{Tag: tags.adtVariantTag(compilerTypes.FileModeType.Adt, index), Index: fmt.Sprintf("%d", index)}); err != nil {
				return err
			}
		}
		if err := renderInto(result, "module.h", "file_open_adapter", fileOpenAdapterModel{
			CName:   union.CName,
			Suffix:  streamAdapterSuffix(union),
			Cases:   cases.String(),
			Success: fileTag,
			Field:   fileField,
			Failure: failure,
		}); err != nil {
			return err
		}
	}

	for _, union := range state.readUnions {
		sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
		eosTag, _ := streamMemberRef(tags, union, compilerTypes.EoS)
		notReadable, err := errorArm(union, "transfer.status", false, fileMessageNotReadable)
		if err != nil {
			return err
		}
		failed, err := errorArm(union, "transfer.status", false, fileMessageRead)
		if err != nil {
			return err
		}
		if err := renderInto(result, "module.h", "file_read_adapter", streamReadAdapterModel{
			Suffix:  streamAdapterSuffix(union),
			CName:   union.CName,
			Success: sizeTag,
			Field:   sizeField,
			EosTag:  eosTag,
			Guard:   notReadable,
			Error:   failed,
		}); err != nil {
			return err
		}
	}

	sizeAdapter := func(unions []compilerTypes.Type, operation, parameters, call, payload string, gate bool) error {
		for _, union := range unions {
			sizeTag, sizeField := streamMemberRef(tags, union, compilerTypes.SizeType)
			failed, err := errorArm(union, "transfer.status", false, payload)
			if err != nil {
				return err
			}
			notWritable := ""
			if gate {
				arm, err := errorArm(union, "transfer.status", false, fileMessageNotWritable)
				if err != nil {
					return err
				}
				notWritable = "    if (transfer.status == HEX_FILE_NOT_WRITABLE) {\n        return " + arm + ";\n    }\n"
			}
			if err := renderInto(result, "module.h", "file_size_adapter", fileSizeAdapterModel{
				CName:      union.CName,
				Operation:  operation,
				Suffix:     streamAdapterSuffix(union),
				Parameters: parameters,
				Call:       call,
				Success:    sizeTag,
				Field:      sizeField,
				Gate:       notWritable,
				Failure:    failed,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := sizeAdapter(state.writeUnions, "write", "hex_slice_UInt8 from", "hex_file_write(file, from)", fileMessageWrite, true); err != nil {
		return err
	}
	seekCall := ""
	if len(state.seekUnions) > 0 {
		seekCall = fmt.Sprintf("to.tag == %s ? hex_file_seek(file, 0u, (int64_t)to.payload.Start.hex_m_position) : to.tag == %s ? hex_file_seek(file, 1u, to.payload.Current.hex_m_offset) : hex_file_seek(file, 2u, to.payload.End.hex_m_offset)",
			streamSeekTag(tags, 0), streamSeekTag(tags, 1))
	}
	if err := sizeAdapter(state.seekUnions, "seek", "hex_t_Seek to", seekCall, fileMessageSeek, false); err != nil {
		return err
	}

	statusAdapter := func(unions []compilerTypes.Type, operation, payload string) error {
		for _, union := range unions {
			nilTag, _ := streamMemberRef(tags, union, compilerTypes.Nil)
			failed, err := errorArm(union, "status", false, payload)
			if err != nil {
				return err
			}
			notWritable := ""
			if operation == "flush" {
				arm, err := errorArm(union, "status", false, fileMessageNotWritable)
				if err != nil {
					return err
				}
				notWritable = "    if (status == HEX_FILE_NOT_WRITABLE) {\n        return " + arm + ";\n    }\n"
			}
			if err := renderInto(result, "module.h", "file_status_adapter", fileStatusAdapterModel{
				CName:     union.CName,
				Operation: operation,
				Suffix:    streamAdapterSuffix(union),
				Success:   nilTag,
				Gate:      notWritable,
				Error:     failed,
			}); err != nil {
				return err
			}
		}
		return nil
	}
	if err := statusAdapter(state.flushUnions, "flush", fileMessageFlush); err != nil {
		return err
	}
	return statusAdapter(state.closeUnions, "close", fileMessageClose)
}

// mergeFileInto unions one module's File demand into the program state.
func mergeFileInto(merged, state *generatedFileState) {
	if state == nil {
		return
	}
	merged.used = merged.used || state.used
	merged.seek = merged.seek || state.seek
}

// fileComponents returns hexal/file.h and hexal/file.c when File is reachable.
func fileComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged.fileState == nil || !merged.fileState.used {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/file.h", template: "file.h", model: struct{}{}},
		{key: "hexal/file.c", template: "file.c", model: fileSourceModel{Event: eventSelected(merged)}},
	}, nil
}

// fileSourceModel adds the Task-parking request path when the program also
// selects the event bridge.
type fileSourceModel struct {
	Event bool
}

// moduleFileComponent selects hexal/file.h for a module naming File or
// FileMode.
func moduleFileComponent(emission *moduleEmission) []string {
	if emission == nil || emission.fileState == nil || !emission.fileState.used {
		return nil
	}
	return []string{"hexal/file.h"}
}
