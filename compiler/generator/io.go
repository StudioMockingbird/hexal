package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0040: the synchronous File API built on C23 stdio. FileMode lowers to
// one small enum; File to one small struct naming FILE *, mode, and
// ownership. All lengths use size_t and no operation allocates a wrapper.

// generatedIOState records the File operations and failure messages the
// program uses.
type generatedIOState struct {
	used           bool
	open           bool
	readBytes      bool
	readText       bool
	write          bool
	writeText      bool
	flush          bool
	close          bool
	stdin          bool
	stdout         bool
	stderr         bool
	openUnion      compilerTypes.Type
	readBytesUnion compilerTypes.Type
	readTextUnion  compilerTypes.Type
	writeUnion     compilerTypes.Type
	listType       compilerTypes.Type
	fileLiteral    string
	headerLiteral  string
}

// ioErrorMessages are the static portable RFC 0040 failure messages.
const (
	ioCouldNotOpen  = "could not open file"
	ioCouldNotRead  = "could not read file"
	ioCouldNotWrite = "could not write file"
	ioCouldNotFlush = "could not flush file"
	ioInvalidUTF8   = "input is not valid UTF-8"
	ioModeError     = "operation is not permitted by the File mode"
	ioBinaryError   = "binary I/O is unavailable on a standard text File"
	ioPathEmpty     = "path cannot be empty"
	ioPathNUL       = "path contains NUL"
	ioPathASCII     = "v1 path contains a non-ASCII character"
)

// discoverGeneratedIO walks the checked program for RFC 0040 operations and
// registers the failure literals.
func discoverGeneratedIO(program checker.Program, stringState *generatedStringState) *generatedIOState {
	state := &generatedIOState{}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.FileOpenExpression:
				state.used = true
				state.open = true
				state.openUnion = node.ResultType
			case checker.StdioCallExpression:
				state.used = true
				switch node.Name {
				case "stdin":
					state.stdin = true
				case "stdout":
					state.stdout = true
				case "stderr":
					state.stderr = true
				}
			case checker.FileMethodCallExpression:
				state.used = true
				switch node.Name {
				case "read_bytes":
					state.readBytes = true
					state.readBytesUnion = node.ResultType
					for _, member := range compilerTypes.UnionMembers(node.ResultType) {
						if member.List != nil {
							state.listType = member
							break
						}
					}
				case "read_text":
					state.readText = true
					state.readTextUnion = node.ResultType
				case "write":
					state.write = true
					state.writeUnion = node.ResultType
				case "write_text":
					state.writeText = true
					state.writeUnion = node.ResultType
				case "flush":
					state.flush = true
					state.writeUnion = node.ResultType
				case "close":
					state.close = true
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		panic(err)
	}
	if state.used && stringState != nil {
		stringState.used = true
		stringState.needStrand = true
		state.fileLiteral = registerConcurrencyLiteral(stringState, sourceFilename)
		state.headerLiteral = registerConcurrencyLiteral(stringState, "I/O Error")
		for _, payload := range []string{ioCouldNotOpen, ioCouldNotRead, ioCouldNotWrite, ioCouldNotFlush, ioInvalidUTF8, ioModeError, ioBinaryError, ioPathEmpty, ioPathNUL, ioPathASCII} {
			registerConcurrencyLiteral(stringState, payload)
		}
	}
	return state
}
func writeIOPrelude(result *strings.Builder, state *generatedIOState) {
	if state == nil || !state.used {
		return
	}
	result.WriteString("\n/* RFC 0040 handle definitions */\n")
	result.WriteString("typedef enum hex_file_mode {\n    HEX_FILE_READ,\n    HEX_FILE_WRITE,\n    HEX_FILE_APPEND,\n} hex_file_mode;\n")
	result.WriteString("typedef struct hex_file {\n    FILE *stream;\n    hex_file_mode mode;\n    bool owned;\n} hex_file;\n")
	if state.readBytes && state.listType.List != nil {
		fmt.Fprintf(result, "typedef struct %s %s;\n", state.listType.CName, state.listType.CName)
	}
}

// writeIOGate emits the RFC 0040 process-wide output gate into the root
// module's C file: the gate mutex and its closed flag, plus the
// lock/unlock/shutdown functions. The gate exists once per process, so it
// cannot live in a header every translation unit includes; the module
// headers' inline helpers call it through the hexal.h extern declarations.
func writeIOGate(result *strings.Builder, state *generatedIOState, schedulerLinked bool) {
	if state == nil || !state.used {
		return
	}
	result.WriteString("\n#include <stdio.h>\n")
	if schedulerLinked {
		result.WriteString("#include <threads.h>\n")
	}
	// RFC 0040: standard text writes and flushes share one process-wide
	// output gate. With the scheduler linked the gate is a real mutex that
	// closes before root completion; without it the helpers are no-ops.
	if schedulerLinked {
		result.WriteString("\nstatic mtx_t hex_io_gate;\nbool hex_io_gate_closed;\n")
		result.WriteString("\nvoid hex_io_gate_lock(void) {\n    mtx_lock(&hex_io_gate);\n}\n")
		result.WriteString("void hex_io_gate_unlock(void) {\n    mtx_unlock(&hex_io_gate);\n}\n")
		result.WriteString("\nstatic void hex_io_gate_shutdown(void) {\n    mtx_lock(&hex_io_gate);\n    hex_io_gate_closed = true;\n    fflush(stdout);\n    fflush(stderr);\n    mtx_unlock(&hex_io_gate);\n}\n")
	} else {
		result.WriteString("\nbool hex_io_gate_closed;\n")
		result.WriteString("void hex_io_gate_lock(void) {\n}\n")
		result.WriteString("void hex_io_gate_unlock(void) {\n}\n")
		result.WriteString("\nstatic void hex_io_gate_shutdown(void) {\n    fflush(stdout);\n    fflush(stderr);\n}\n")
	}
}

// writeIOExterns emits, into hexal.h, the output gate entry points the
// module headers' inline write and flush helpers call.
func writeIOExterns(result *strings.Builder, state *generatedIOState) {
	if state == nil || !state.used {
		return
	}
	result.WriteString("\n/* RFC 0040 output gate, defined in the root module's C file */\n")
	result.WriteString("extern bool hex_io_gate_closed;\n")
	result.WriteString("void hex_io_gate_lock(void);\n")
	result.WriteString("void hex_io_gate_unlock(void);\n")
}

// writeIOInlineHelpers emits the state-free File operation helpers into the
// module header: the path and UTF-8 validators, the open/read/write/flush/
// close families, and the Error-construction helper. They run after the
// Error object, the String machinery, and the List definitions (read_bytes
// constructs a List<Byte> through the generated list helpers).
func writeIOInlineHelpers(result *strings.Builder, state *generatedIOState, stringState *generatedStringState) {
	if state == nil || !state.used {
		return
	}
	result.WriteString("\n#include <stdio.h>\n")
	state.writeIOErrorHelper(result)
	result.WriteString("\nstatic inline bool hex_utf8_valid(const uint8_t *data, size_t length) {\n    size_t index = 0;\n    while (index < length) {\n        uint8_t lead = data[index];\n        size_t width;\n        if (lead < 0x80) {\n            width = 1;\n        } else if (lead < 0xE0) {\n            width = 2;\n        } else if (lead < 0xF0) {\n            width = 3;\n        } else if (lead < 0xF8) {\n            width = 4;\n        } else {\n            return false;\n        }\n        if (index + width > length) {\n            return false;\n        }\n        for (size_t continuation = 1; continuation < width; continuation++) {\n            if ((data[index + continuation] & 0xC0) != 0x80) {\n                return false;\n            }\n        }\n        index += width;\n    }\n    return true;\n}\n")
	result.WriteString("\nstatic inline bool hex_file_path_valid(const uint8_t *data, size_t length) {\n    if (length == 0) {\n        return false;\n    }\n    for (size_t index = 0; index < length; index++) {\n        if (data[index] == 0) {\n            return false;\n        }\n        if (data[index] > 0x7F) {\n            return false;\n        }\n    }\n    return true;\n}\n")
	if state.open {
		union := state.openUnion
		if union != (compilerTypes.Type{}) {
			fileIndex := unionMemberIndex(union, compilerTypes.FileType)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			message := state.ioMessage(stringState, ioCouldNotOpen)
			fmt.Fprintf(result, "\nstatic inline %s hex_file_open(const uint8_t *path, size_t length, hex_file_mode mode, size_t line, size_t column, const hex_string *message) {\n    (void)message;\n    if (!hex_file_path_valid(path, length)) {\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    FILE *stream = fopen((const char *)path, mode == HEX_FILE_READ ? \"rb\" : (mode == HEX_FILE_WRITE ? \"wb\" : \"ab\"));\n    if (stream == NULL) {\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    return (%s){ .tag = %s, .payload.member_%d = (hex_file){ stream, mode, true } };\n}\n",
				union.CName, union.CName, unionTagName(union, errorIndex), errorIndex, message,
				union.CName, unionTagName(union, errorIndex), errorIndex, message,
				union.CName, unionTagName(union, fileIndex), fileIndex)
		}
	}
	if state.readBytes {
		union := state.readBytesUnion
		listType := state.listType
		if union != (compilerTypes.Type{}) && listType.List != nil {
			listIndex := unionMemberIndex(union, listType)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			message := state.ioMessage(stringState, ioCouldNotRead)
			fmt.Fprintf(result, "\nstatic inline %s hex_file_read_bytes_%s(hex_file file, hex_heap h, size_t line, size_t column, const hex_string *message) {\n    (void)message;\n    if (file.mode == HEX_FILE_WRITE || file.mode == HEX_FILE_APPEND) {\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    %s *list = hex_list_new_%s(h);\n    uint8_t buffer[4096];\n    for (;;) {\n        size_t count = fread(buffer, 1, sizeof buffer, file.stream);\n        for (size_t index = 0; index < count; index++) {\n            hex_list_push_%s(list, buffer[index]);\n        }\n        if (count < sizeof buffer) {\n            if (ferror(file.stream)) {\n                hex_list_free_%s(h, list);\n                return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n            }\n            break;\n        }\n    }\n    return (%s){ .tag = %s, .payload.member_%d = list };\n}\n",
				union.CName, listSuffix(listType), union.CName, unionTagName(union, errorIndex), errorIndex, message,
				listType.CName, listSuffix(listType), listSuffix(listType), listSuffix(listType),
				union.CName, unionTagName(union, errorIndex), errorIndex, message,
				union.CName, unionTagName(union, listIndex), listIndex)
		}
	}
	if state.readText {
		union := state.readTextUnion
		if union != (compilerTypes.Type{}) {
			stringIndex := unionMemberIndex(union, compilerTypes.StringType)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			message := state.ioMessage(stringState, ioCouldNotRead)
			utf8Message := state.ioMessage(stringState, ioInvalidUTF8)
			fmt.Fprintf(result, "\nstatic inline %s hex_file_read_text(hex_file file, hex_heap h, size_t line, size_t column, const hex_string *message, const hex_string *utf8_message) {\n    (void)message;\n    (void)utf8_message;\n    if (file.mode == HEX_FILE_WRITE || file.mode == HEX_FILE_APPEND) {\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    size_t capacity = 4096;\n    size_t length = 0;\n    uint8_t *buffer = (uint8_t *)malloc(capacity);\n    if (buffer == NULL) {\n        fputs(\"[Runtime Error] heap allocation failed\\n\", stderr);\n        abort();\n    }\n    for (;;) {\n        if (length + 4096 > capacity) {\n            size_t next = capacity * 2;\n            if (next < capacity) {\n                fputs(\"[Runtime Error] file read size is not representable\\n\", stderr);\n                abort();\n            }\n            capacity = next;\n            uint8_t *grown = (uint8_t *)realloc(buffer, capacity);\n            if (grown == NULL) {\n                free(buffer);\n                fputs(\"[Runtime Error] heap allocation failed\\n\", stderr);\n                abort();\n            }\n            buffer = grown;\n        }\n        size_t count = fread(buffer + length, 1, 4096, file.stream);\n        length += count;\n        if (count < 4096) {\n            if (ferror(file.stream)) {\n                free(buffer);\n                return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n            }\n            break;\n        }\n    }\n    if (!hex_utf8_valid(buffer, length)) {\n        free(buffer);\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    const hex_string *text = hex_string_from_bytes(h, buffer, length);\n    free(buffer);\n    return (%s){ .tag = %s, .payload.member_%d = text };\n}\n",
				union.CName, union.CName, unionTagName(union, errorIndex), errorIndex, message,
				union.CName, unionTagName(union, errorIndex), errorIndex, message,
				union.CName, unionTagName(union, errorIndex), errorIndex, utf8Message,
				union.CName, unionTagName(union, stringIndex), stringIndex)
		}
	}
	if state.write {
		union := state.writeUnion
		if union != (compilerTypes.Type{}) {
			nilIndex := unionMemberIndex(union, compilerTypes.Nil)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			message := state.ioMessage(stringState, ioCouldNotWrite)
			modeMessage := state.ioMessage(stringState, ioModeError)
			fmt.Fprintf(result, "\nstatic inline %s hex_file_write_bytes(hex_file file, const uint8_t *data, size_t length, size_t line, size_t column, const hex_string *message, const hex_string *mode_message) {\n    (void)message;\n    (void)mode_message;\n    if (file.mode == HEX_FILE_READ) {\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    size_t written = 0;\n    while (written < length) {\n        size_t count = fwrite(data + written, 1, length - written, file.stream);\n        if (count == 0) {\n            return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n        }\n        written += count;\n    }\n    return (%s){ .tag = %s };\n}\n",
				union.CName, union.CName, unionTagName(union, errorIndex), errorIndex, modeMessage,
				union.CName, unionTagName(union, errorIndex), errorIndex, message,
				union.CName, unionTagName(union, nilIndex))
		}
	}
	if state.writeText {
		union := state.writeUnion
		if union != (compilerTypes.Type{}) {
			nilIndex := unionMemberIndex(union, compilerTypes.Nil)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			message := state.ioMessage(stringState, ioCouldNotWrite)
			modeMessage := state.ioMessage(stringState, ioModeError)
			fmt.Fprintf(result, "\nstatic inline %s hex_file_write_text(hex_file file, const hex_string *text, size_t line, size_t column, const hex_string *message, const hex_string *mode_message, bool standard_gate) {\n    (void)message;\n    (void)mode_message;\n    if (standard_gate) {\n        hex_io_gate_lock();\n        if (hex_io_gate_closed) {\n            hex_io_gate_unlock();\n            return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n        }\n    }\n    if (file.mode == HEX_FILE_READ) {\n        if (standard_gate) {\n            hex_io_gate_unlock();\n        }\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    size_t written = 0;\n    while (written < text->byte_length) {\n        size_t count = fwrite(text->data + written, 1, text->byte_length - written, file.stream);\n        if (count == 0) {\n            if (standard_gate) {\n                hex_io_gate_unlock();\n            }\n            return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n        }\n        written += count;\n    }\n    if (standard_gate) {\n        hex_io_gate_unlock();\n    }\n    return (%s){ .tag = %s };\n}\n",
				union.CName, union.CName, unionTagName(union, errorIndex), errorIndex, modeMessage,
				union.CName, unionTagName(union, errorIndex), errorIndex, modeMessage,
				union.CName, unionTagName(union, errorIndex), errorIndex, message,
				union.CName, unionTagName(union, nilIndex))
		}
	}
	if state.flush {
		union := state.writeUnion
		if union != (compilerTypes.Type{}) {
			nilIndex := unionMemberIndex(union, compilerTypes.Nil)
			errorIndex := unionMemberIndex(union, compilerTypes.ErrorType)
			message := state.ioMessage(stringState, ioCouldNotFlush)
			modeMessage := state.ioMessage(stringState, ioModeError)
			fmt.Fprintf(result, "\nstatic inline %s hex_file_flush(hex_file file, size_t line, size_t column, const hex_string *message, const hex_string *mode_message, bool standard_gate) {\n    (void)message;\n    (void)mode_message;\n    if (standard_gate) {\n        hex_io_gate_lock();\n        if (hex_io_gate_closed) {\n            hex_io_gate_unlock();\n            return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n        }\n    }\n    if (file.mode == HEX_FILE_READ) {\n        if (standard_gate) {\n            hex_io_gate_unlock();\n        }\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    if (fflush(file.stream) != 0) {\n        if (standard_gate) {\n            hex_io_gate_unlock();\n        }\n        return (%s){ .tag = %s, .payload.member_%d = hex_io_error(line, column, &%s) };\n    }\n    if (standard_gate) {\n        hex_io_gate_unlock();\n    }\n    return (%s){ .tag = %s };\n}\n",
				union.CName, union.CName, unionTagName(union, errorIndex), errorIndex, modeMessage,
				union.CName, unionTagName(union, errorIndex), errorIndex, modeMessage,
				union.CName, unionTagName(union, errorIndex), errorIndex, message,
				union.CName, unionTagName(union, nilIndex))
		}
	}
	if state.close {
		result.WriteString("\nstatic inline void hex_file_close(hex_file file) {\n")
		result.WriteString("    if (!file.owned) {\n        fputs(\"[Runtime Error] standard File is not owned\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    if (fclose(file.stream) != 0) {\n        fputs(\"[Runtime Error] file close failed\\n\", stderr);\n        abort();\n    }\n}\n")
	}
}

// writeIOErrorHelper emits the Error constructor shared by every recoverable
// I/O operation.
func (state *generatedIOState) writeIOErrorHelper(result *strings.Builder) {
	fmt.Fprintf(result, "\nstatic inline hex_t_Error hex_io_error(size_t line, size_t column, const hex_string *message) {\n")
	fmt.Fprintf(result, "    return (hex_t_Error){\n")
	fmt.Fprintf(result, "        .hex_m_file = &%s,\n", state.fileLiteral)
	fmt.Fprintf(result, "        .hex_m_line = line,\n")
	fmt.Fprintf(result, "        .hex_m_column = column,\n")
	fmt.Fprintf(result, "        .hex_m_header = (hex_strand){{")
	for _, character := range []byte("I/O Error") {
		fmt.Fprintf(result, " %d,", character)
	}
	fmt.Fprintf(result, " 0 }},\n")
	fmt.Fprintf(result, "        .hex_m_message = message,\n")
	fmt.Fprintf(result, "    };\n}\n")
}

// ioMessage resolves one failure message literal.
func (state *generatedIOState) ioMessage(stringState *generatedStringState, payload string) string {
	if index, ok := stringState.seen[payload]; ok {
		return stringLiteralCName(index - 1)
	}
	return state.fileLiteral
}

// fileModeVariants maps each FileMode variant to its C enum spelling.
var fileModeVariants = map[string]string{
	"Read":   "HEX_FILE_READ",
	"Write":  "HEX_FILE_WRITE",
	"Append": "HEX_FILE_APPEND",
}

// renderFileModeLiteral renders one FileMode variant.
func renderFileModeLiteral(node checker.Expression) (string, error) {
	spelling, ok := fileModeVariants[node.Name]
	if !ok {
		return "", unknownExpressionDiagnostic("unknown FileMode variant " + node.Name)
	}
	return spelling, nil
}

// renderFileOpen renders File.open(path, mode) as its File | Error union.
func renderFileOpen(node checker.Expression, state *expressionValidation) (string, error) {
	if len(node.Arguments) != 2 {
		return "", unknownExpressionDiagnostic("File.open without checked operands")
	}
	path, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	mode, err := renderOperandWithState(node.Arguments[1], state)
	if err != nil {
		return "", err
	}
	message, err := errorMessageLiteral(state, ioCouldNotOpen)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_file_open((%s)->data, (%s)->byte_length, %s, %d, %d, &%s)", path, path, mode, node.SourceLine, node.SourceColumn, message), nil
}

// renderStdioCall renders one borrowed standard File descriptor.
func renderStdioCall(node checker.Expression) (string, error) {
	switch node.Name {
	case "stdin":
		return "((hex_file){ stdin, HEX_FILE_READ, false })", nil
	case "stdout":
		return "((hex_file){ stdout, HEX_FILE_WRITE, false })", nil
	case "stderr":
		return "((hex_file){ stderr, HEX_FILE_WRITE, false })", nil
	}
	return "", unknownExpressionDiagnostic("unknown Stdio operation " + node.Name)
}

// renderFileMethod renders one File method call.
func renderFileMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic("file method without a checked receiver")
	}
	receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if err != nil {
		return "", err
	}
	directStdio := ""
	if node.Operand.Kind == checker.StdioCallExpression {
		directStdio = node.Operand.Name
	}
	standardGate := directStdio != "" && directStdio != "stdin"
	switch node.Name {
	case "read_bytes":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("file read_bytes without a checked heap")
		}
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		listType := compilerTypes.Type{}
		for _, member := range compilerTypes.UnionMembers(node.ResultType) {
			if member.List != nil {
				listType = member
				break
			}
		}
		if listType == (compilerTypes.Type{}) {
			return "", unknownExpressionDiagnostic("file read_bytes result union lacks its List member")
		}
		message, messageErr := errorMessageLiteral(state, ioCouldNotRead)
		if messageErr != nil {
			return "", messageErr
		}
		return fmt.Sprintf("hex_file_read_bytes_%s(%s, %s, %d, %d, &%s)", listSuffix(listType), receiver, heap, node.SourceLine, node.SourceColumn, message), nil
	case "read_text":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("file read_text without a checked heap")
		}
		heap, heapErr := renderOperandWithState(node.Arguments[0], state)
		if heapErr != nil {
			return "", heapErr
		}
		message, messageErr := errorMessageLiteral(state, ioCouldNotRead)
		if messageErr != nil {
			return "", messageErr
		}
		utf8Message, utf8Err := errorMessageLiteral(state, ioInvalidUTF8)
		if utf8Err != nil {
			return "", utf8Err
		}
		return fmt.Sprintf("hex_file_read_text(%s, %s, %d, %d, &%s, &%s)", receiver, heap, node.SourceLine, node.SourceColumn, message, utf8Message), nil
	case "write":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("file write without a checked view")
		}
		view, viewErr := renderOperandWithState(node.Arguments[0], state)
		if viewErr != nil {
			return "", viewErr
		}
		message, messageErr := errorMessageLiteral(state, ioCouldNotWrite)
		if messageErr != nil {
			return "", messageErr
		}
		modeMessage, modeErr := errorMessageLiteral(state, ioModeError)
		if modeErr != nil {
			return "", modeErr
		}
		return fmt.Sprintf("hex_file_write_bytes(%s, (%s).data, (%s).length, %d, %d, &%s, &%s)", receiver, view, view, node.SourceLine, node.SourceColumn, message, modeMessage), nil
	case "write_text":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("file write_text without a checked string")
		}
		text, textErr := renderOperandWithState(node.Arguments[0], state)
		if textErr != nil {
			return "", textErr
		}
		message, messageErr := errorMessageLiteral(state, ioCouldNotWrite)
		if messageErr != nil {
			return "", messageErr
		}
		modeMessage, modeErr := errorMessageLiteral(state, ioModeError)
		if modeErr != nil {
			return "", modeErr
		}
		gate := "false"
		if standardGate {
			gate = "true"
		}
		return fmt.Sprintf("hex_file_write_text(%s, %s, %d, %d, &%s, &%s, %s)", receiver, text, node.SourceLine, node.SourceColumn, message, modeMessage, gate), nil
	case "flush":
		message, messageErr := errorMessageLiteral(state, ioCouldNotFlush)
		if messageErr != nil {
			return "", messageErr
		}
		modeMessage, modeErr := errorMessageLiteral(state, ioModeError)
		if modeErr != nil {
			return "", modeErr
		}
		gate := "false"
		if standardGate {
			gate = "true"
		}
		return fmt.Sprintf("hex_file_flush(%s, %d, %d, &%s, &%s, %s)", receiver, node.SourceLine, node.SourceColumn, message, modeMessage, gate), nil
	case "close":
		return "hex_file_close(" + receiver + ")", nil
	}
	return "", unknownExpressionDiagnostic("unknown file method " + node.Name)
}
