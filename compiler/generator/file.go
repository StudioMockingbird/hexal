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
		return "", unknownExpressionDiagnostic("file open without checked arguments")
	}
	path, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	mode, err := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_file_open_%s(%s, %s, %d, %d)", streamAdapterSuffix(node.ResultType), path, mode, node.SourceLine, node.SourceColumn), nil
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
	return fileMethodCall(node, arguments)
}

// fileMethodCall spells one File operation over rendered operands, receiver
// first; the deferred-close path shares it.
func fileMethodCall(node checker.Expression, arguments []string) (string, error) {
	want := map[string]int{"read": 3, "write": 2, "seek": 2, "flush": 1, "close": 1}[node.Name]
	if want == 0 || len(arguments) != want {
		return "", unknownExpressionDiagnostic("file operation has invalid rendered operands")
	}
	return fmt.Sprintf("hex_file_%s_%s(%s, %d, %d)", node.Name, streamAdapterSuffix(node.ResultType), strings.Join(arguments, ", "), node.SourceLine, node.SourceColumn), nil
}

// validateFileConstructor checks File.open fail-closed.
func validateFileConstructor(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Name != "open" || len(node.Arguments) != 2 || node.Operand != nil || node.SourceLine == 0 ||
		!compilerTypes.IsString(node.Arguments[0].Type) || !compilerTypes.IsFileMode(node.Arguments[1].Type) {
		return unknownExpressionDiagnostic("file open has invalid checked metadata")
	}
	members := compilerTypes.UnionMembers(node.ResultType)
	if node.ResultType.Union == nil || members.Len() != 2 || !unionHasMember(members, compilerTypes.FileType) || !unionHasMember(members, compilerTypes.ErrorType) {
		return unknownExpressionDiagnostic("file open result is not File | Error")
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("file open result does not match its expected type")
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
		return unknownExpressionDiagnostic("unknown file operation in checked metadata")
	}
	members := compilerTypes.UnionMembers(node.ResultType)
	if node.Operand == nil || len(node.Arguments) != arguments || node.ResultType.Union == nil || members.Len() != len(contract) || node.SourceLine == 0 {
		return unknownExpressionDiagnostic("file operation has invalid checked metadata")
	}
	for _, required := range contract {
		if !unionHasMember(members, required) {
			return unknownExpressionDiagnostic("file operation result is missing a contract member")
		}
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("file operation result does not match its expected type")
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
func writeFileInlineHelpers(result *strings.Builder, state *generatedFileState, literals *literalRegistry, tags *tagRegistry) error {
	if state == nil || !state.used {
		return nil
	}
	file := "&" + literals.CName(state.fileLiteral)
	message := func(payload string) (string, error) {
		handle, ok := literals.Lookup(payload)
		if !ok {
			return "", unknownExpressionDiagnostic("file failure message is missing from the literal registry: " + payload)
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
			fmt.Fprintf(&cases, "    case %s:\n        variant = %d;\n        break;\n", tags.adtVariantTag(compilerTypes.FileModeType.Adt, index), index)
		}
		fmt.Fprintf(result,
			"\nstatic inline %s hex_file_open_%s(const hex_string *path, hex_t_FileMode mode, size_t line, size_t column) {\n"+
				"    uint8_t variant;\n"+
				"    switch (mode.tag) {\n%s"+
				"    default:\n"+
				"        abort();\n"+
				"    }\n"+
				"    hex_file_opened opened = hex_file_open(path, variant);\n"+
				"    if (opened.status == 0) {\n"+
				"        return (%s){ .tag = %s, .payload.%s = opened.file };\n"+
				"    }\n"+
				"    return %s;\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union), cases.String(),
			union.CName, fileTag, fileField, failure)
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
		fmt.Fprintf(result,
			"\nstatic inline %s hex_file_read_%s(hex_file file, hex_list_UInt8 *into, size_t max, size_t line, size_t column) {\n"+
				"    hex_file_transfer transfer = hex_file_read(file, into, max);\n"+
				"    switch (transfer.status) {\n"+
				"    case 0:\n"+
				"        return (%s){ .tag = %s, .payload.%s = transfer.count };\n"+
				"    case HEX_FILE_EOS:\n"+
				"        return (%s){ .tag = %s };\n"+
				"    case HEX_FILE_NOT_READABLE:\n"+
				"        return %s;\n"+
				"    default:\n"+
				"        return %s;\n"+
				"    }\n"+
				"}\n",
			union.CName, streamAdapterSuffix(union),
			union.CName, sizeTag, sizeField,
			union.CName, eosTag,
			notReadable, failed)
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
			fmt.Fprintf(result,
				"\nstatic inline %s hex_file_%s_%s(hex_file file, %s, size_t line, size_t column) {\n"+
					"    hex_file_transfer transfer = %s;\n"+
					"    if (transfer.status == 0) {\n"+
					"        return (%s){ .tag = %s, .payload.%s = transfer.count };\n"+
					"    }\n%s"+
					"    return %s;\n"+
					"}\n",
				union.CName, operation, streamAdapterSuffix(union), parameters, call,
				union.CName, sizeTag, sizeField, notWritable, failed)
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
			fmt.Fprintf(result,
				"\nstatic inline %s hex_file_%s_%s(hex_file file, size_t line, size_t column) {\n"+
					"    int status = hex_file_%s(file);\n"+
					"    if (status == 0) {\n"+
					"        return (%s){ .tag = %s };\n"+
					"    }\n%s"+
					"    return %s;\n"+
					"}\n",
				union.CName, operation, streamAdapterSuffix(union), operation,
				union.CName, nilTag, notWritable, failed)
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
