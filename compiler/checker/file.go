package checker

// The File builtins: File.open(path, mode) and the read, write,
// seek, flush, and close operations. File checking reuses the IO stream
// machinery end to end -- the stream expression kinds, argument checks,
// capability facts, and close tracking -- while File keeps its own
// representation and operation set.

import (
	"fmt"

	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkFileTypeCall resolves File.open(path: String, mode: FileMode).
func checkFileTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "open" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(typeErrorAt(variable.Name, "File has no such operation; use File.open(path, mode)"))}
	}
	if len(call.Arguments) != 2 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("open expects 2 arguments (path: String, mode: FileMode); got %d", len(call.Arguments))))}
	}
	var arguments []Operand
	for index, expected := range []compilerTypes.Type{compilerTypes.StringType, compilerTypes.FileModeType} {
		checked := checkInitializer(call.Arguments[index], compilerTypes.NewTypeUse(expected), tokenOf(call.Arguments[index]), ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return checkedExpression{token: property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
		}
		if !assignable(expected, checked.typ) {
			return checkedExpression{token: checked.token, diagnostic: diagnosticAt(typeMismatchDiagnostic(expected, checked.typ, checked.token))}
		}
		arguments = append(arguments, checked.source)
	}
	resultUnion := ctx.typeEnvironment.UnionType([]compilerTypes.Type{compilerTypes.FileType, compilerTypes.ErrorType})
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property, "could not construct the File | Error result union"))}
	}
	node := Expression{
		Kind:         StreamConstructorExpression,
		Name:         "open",
		Arguments:    arguments,
		OperandType:  compilerTypes.FileType,
		ResultType:   resultUnion,
		SourceLine:   property.Line,
		SourceColumn: property.Column,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultUnion, Name: "open", Node: node}
	return checkedExpression{source: source, typ: resultUnion, token: property}
}

// checkFileMethodCall resolves read, write, seek, flush, and close on File.
func checkFileMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "read", "write", "seek", "flush", "close":
	default:
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "File has no method "+name+"; use read, write, seek, flush, or close"))}
	}
	if len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, name+" takes no type arguments"))}
	}
	if ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "only File.close() may be deferred on a file"))}
	}
	receiver = valueFromPlace(receiver)
	if diagnostic := streamClosedDiagnostic(receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	switch name {
	case "read":
		if diagnostic := streamCapabilityMismatch("read", receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
	case "write", "flush":
		if diagnostic := streamCapabilityMismatch("write", receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
	}
	var arguments []Operand
	var diagnostics compilerTypes.Diagnostics
	switch name {
	case "read":
		arguments, diagnostics = checkStreamReadArguments(call, callee, ctx)
	case "write":
		arguments, diagnostics = checkStreamWriteArguments(call, callee, ctx)
	case "seek":
		arguments, diagnostics = checkStreamSeekArguments(call, callee, ctx)
	default:
		if len(call.Arguments) != 0 {
			diagnostics = compilerTypes.Diagnostics{typeErrorAt(callee.Property, name+" expects no arguments")}
		}
	}
	if len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	resultName := name
	if name == "flush" {
		resultName = "close"
	}
	resultUnion := streamResultUnion(resultName, ctx.typeEnvironment)
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(unknownAt(callee.Property, "could not construct the "+name+" result union"))}
	}
	node := Expression{
		Kind:         StreamMethodCallExpression,
		Name:         name,
		Operand:      &receiver.source.Node,
		Arguments:    arguments,
		OperandType:  compilerTypes.FileType,
		ResultType:   resultUnion,
		SourceLine:   callee.Property.Line,
		SourceColumn: callee.Property.Column,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultUnion, Name: name, Node: node}
	checked := checkedExpression{source: source, typ: resultUnion, token: callee.Property}
	if name == "close" && ctx.names.cleanupDepth == 0 {
		markStreamClosed(receiver.source, callee.Property, ctx.names.flow)
	}
	return checked
}

// fileOpenCapability reads the capability a File.open call provably carries:
// a directly written FileMode variant names it; any other mode is unknown.
func fileOpenCapability(node Expression) uint8 {
	if len(node.Arguments) != 2 {
		return uint8(compilerTypes.StreamUnknown)
	}
	mode := node.Arguments[1].Node
	if mode.Kind != AdtConstructExpression || !compilerTypes.IsFileMode(mode.ResultType) {
		return uint8(compilerTypes.StreamUnknown)
	}
	return uint8(compilerTypes.CapabilityFromFileMode(mode.VariantIndex))
}
