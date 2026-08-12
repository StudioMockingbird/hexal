package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// RFC 0040: the synchronous File API, the FileMode variants, and the
// protected Stdio intrinsic qualifier.

// fileModeVariants maps each FileMode variant to its C enum spelling.
var fileModeVariants = map[string]string{
	"Read":   "HEX_FILE_READ",
	"Write":  "HEX_FILE_WRITE",
	"Append": "HEX_FILE_APPEND",
}

// checkFileModeVariant resolves FileMode.Read, FileMode.Write, and
// FileMode.Append into FileMode constants.
func checkFileModeVariant(owner, variant lexer.Token) (*checkedExpression, *compilerTypes.Diagnostic) {
	if _, ok := fileModeVariants[variant.Lexeme]; !ok {
		diagnostic := typeErrorAt(variant, "FileMode has no variant "+variant.Lexeme)
		return nil, &diagnostic
	}
	node := Expression{Kind: FileModeLiteralExpression, Name: variant.Lexeme, ResultType: compilerTypes.FileModeType}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.FileModeType, Name: variant.Lexeme, Node: node}
	checked := checkedExpression{source: source, typ: compilerTypes.FileModeType, token: variant}
	return &checked, nil
}

// checkFileOpenCall resolves File.open(path, mode): the path is a String
// with a v1 ASCII path contract, the mode is a FileMode, and the result is
// File | Error.
func checkFileOpenCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "open" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Line, Column: callee.Column,
			Message: "File has no such operation; use File.open(path, mode)",
		}}
	}
	if len(call.Arguments) != 2 {
		return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: property.Line, Column: property.Column,
			Message: "File.open expects 2 arguments (path, mode)",
		}}
	}
	path := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.StringType), tokenOf(call.Arguments[0]), names, typeEnvironment)
	if diagnostics := initializerDiagnostics(path); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !compilerTypes.IsString(path.typ) {
		return checkedExpression{token: path.token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: path.token.Line, Column: path.token.Column,
			Message: "File.open path must be String; got " + path.typ.Name,
		}}
	}
	if path.source.Node.Kind == StringLiteralExpression {
		if diagnostic := checkLiteralPath(path.source.Node.Name, path.token); diagnostic != nil {
			return checkedExpression{token: path.token, diagnostic: diagnostic}
		}
	}
	mode := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.FileModeType), tokenOf(call.Arguments[1]), names, typeEnvironment)
	if diagnostics := initializerDiagnostics(mode); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
	}
	if !compilerTypes.IsFileMode(mode.typ) {
		return checkedExpression{token: mode.token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: mode.token.Line, Column: mode.token.Column,
			Message: "File.open mode must be FileMode; got " + mode.typ.Name,
		}}
	}
	result := typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.FileType, compilerTypes.ErrorType})
	node := Expression{Kind: FileOpenExpression, Arguments: []Operand{path.source, mode.source}, OperandType: compilerTypes.FileType, ResultType: result, Element: compilerTypes.StringType, SourceLine: callee.Line, SourceColumn: callee.Column}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: "open", Node: node}
	return checkedExpression{source: source, typ: result, token: property}
}

// checkLiteralPath validates a literal v1 path at compile time: non-empty,
// NUL-free, and ASCII-only.
func checkLiteralPath(payload string, token lexer.Token) *compilerTypes.Diagnostic {
	if len(payload) == 0 {
		return &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: token.Line, Column: token.Column,
			Message: "literal path cannot be empty",
		}
	}
	for _, character := range []byte(payload) {
		if character == 0 {
			return &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: token.Line, Column: token.Column,
				Message: "literal path contains NUL",
			}
		}
		if character > 0x7F {
			return &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: token.Line, Column: token.Column,
				Message: "v1 literal path must contain only ASCII",
			}
		}
	}
	return nil
}

// checkStdioCall resolves Stdio.stdin(), Stdio.stdout(), and Stdio.stderr()
// into borrowed File handles with fixed modes.
func checkStdioCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	mode := ""
	switch property.Lexeme {
	case "stdin":
		mode = "Read"
	case "stdout":
		mode = "Write"
	case "stderr":
		mode = "Write"
	default:
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Line, Column: callee.Column,
			Message: "Stdio has no such operation; use Stdio.stdin(), Stdio.stdout(), or Stdio.stderr()",
		}}
	}
	if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: property.Line, Column: property.Column,
			Message: "Stdio." + property.Lexeme + " takes no arguments",
		}}
	}
	node := Expression{Kind: StdioCallExpression, Name: property.Lexeme, ResultType: compilerTypes.FileType, Element: compilerTypes.FileModeType, MemberIndex: fileModeIndex(mode)}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.FileType, Name: property.Lexeme, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.FileType, token: property}
}

// fileModeIndex returns the runtime mode index of one variant.
func fileModeIndex(mode string) int {
	switch mode {
	case "Write":
		return 1
	case "Append":
		return 2
	}
	return 0
}

// checkFileMethodCall resolves one File method. A direct Stdio intrinsic
// receiver (the immediate result of Stdio.stdin/stdout/stderr) is rejected
// statically where its mode or ownership makes the operation invalid; other
// receivers rely on the runtime mode and ownership checks.
func checkFileMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	directStdio := ""
	if receiver.source.Node.Kind == StdioCallExpression {
		directStdio = receiver.source.Node.Name
	}
	switch name {
	case "read_bytes", "read_text":
		if directStdio == "stdout" || directStdio == "stderr" {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "Stdio." + directStdio + " is write-only",
			}}
		}
		if len(call.Arguments) != 1 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: name + " expects 1 argument (heap)",
			}}
		}
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			return checkedExpression{token: heap.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: heap.token.Line, Column: heap.token.Column,
				Message: name + " requires Heap; got " + heap.typ.Name,
			}}
		}
		resultType := compilerTypes.StringType
		element := compilerTypes.StringType
		if name == "read_bytes" {
			resultType = typeEnvironment.ListType(compilerTypes.UInt8)
			element = compilerTypes.UInt8
		}
		result := typeEnvironment.UnionType([]compilerTypes.Type{resultType, compilerTypes.ErrorType})
		node := Expression{Kind: FileMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{heap.source}, OperandType: receiver.typ, ResultType: result, Element: element, SourceLine: callee.Property.Line, SourceColumn: callee.Property.Column}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: callee.Property}
	case "write":
		if directStdio != "" {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "Stdio." + directStdio + " is a text-only standard File",
			}}
		}
		if len(call.Arguments) != 1 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "write expects 1 argument (View<Byte>)",
			}}
		}
		view := checkValue(call.Arguments[0], names, typeEnvironment)
		if diagnostics := initializerDiagnostics(view); len(diagnostics) > 0 {
			return view
		}
		if view.typ.View == nil || !compilerTypes.Equal(view.typ.View.Element, compilerTypes.UInt8) {
			return checkedExpression{token: view.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: view.token.Line, Column: view.token.Column,
				Message: "File.write requires View<Byte>; got " + view.typ.Name,
			}}
		}
		result := typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
		node := Expression{Kind: FileMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{view.source}, OperandType: receiver.typ, ResultType: result, Element: compilerTypes.UInt8, SourceLine: callee.Property.Line, SourceColumn: callee.Property.Column}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: callee.Property}
	case "write_text":
		if len(call.Arguments) != 1 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "write_text expects 1 argument (String)",
			}}
		}
		text := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.StringType), tokenOf(call.Arguments[0]), names, typeEnvironment)
		if diagnostics := initializerDiagnostics(text); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if !compilerTypes.IsString(text.typ) {
			return checkedExpression{token: text.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: text.token.Line, Column: text.token.Column,
				Message: "File.write_text requires String; got " + text.typ.Name,
			}}
		}
		if directStdio == "stdin" {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "Stdio.stdin is read-only",
			}}
		}
		result := typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
		node := Expression{Kind: FileMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{text.source}, OperandType: receiver.typ, ResultType: result, Element: compilerTypes.StringType, SourceLine: callee.Property.Line, SourceColumn: callee.Property.Column}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: callee.Property}
	case "flush":
		if directStdio == "stdin" {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "Stdio.stdin is read-only",
			}}
		}
		if len(call.Arguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "flush expects no arguments",
			}}
		}
		result := typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.Nil, compilerTypes.ErrorType})
		node := Expression{Kind: FileMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: result, Element: compilerTypes.UInt8, SourceLine: callee.Property.Line, SourceColumn: callee.Property.Column}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: callee.Property}
	case "close":
		if directStdio != "" {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "cannot close a borrowed standard File",
			}}
		}
		if len(call.Arguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: callee.Property.Line, Column: callee.Property.Column,
				Message: "close expects no arguments",
			}}
		}
		node := Expression{Kind: FileMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: compilerTypes.Type{}, Element: compilerTypes.UInt8}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	}
	return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError, Stage: "checker",
		Line: callee.Property.Line, Column: callee.Property.Column,
		Message: fmt.Sprintf("File has no method %s; use open, read_bytes, read_text, write, write_text, flush, or close", name),
	}}
}
