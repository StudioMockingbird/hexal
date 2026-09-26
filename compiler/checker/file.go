package checker

// The File builtins: File.open(path, mode) and the read, write,
// seek, flush, and close operations. File checking reuses the IO stream
// machinery end to end -- the stream expression kinds, argument checks,
// capability facts, and close tracking -- while File keeps its own
// representation and operation set.

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkFileTypeCall resolves File.open(path: String, mode: FileMode).
func checkFileTypeCall(call parser.CallExpression, variable parser.VariableExpression, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "open" || len(call.TypeArguments) != 0 {
		return checkedExpression{token: variable.Name, diagnostic: diagnosticAt(messageAt(variable.Name, diag.FileOperationUnsupported()))}
	}
	if len(call.Arguments) != 2 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diag.FileOpenArity(len(call.Arguments))))}
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
		return checkedExpression{token: property, diagnostic: diagnosticAt(unknownAt(property))}
	}
	node := Expression{
		Kind:        StreamConstructorExpression,
		Name:        "open",
		Arguments:   arguments,
		OperandType: compilerTypes.FileType,
		ResultType:  resultUnion,
		Span:        property.Span,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultUnion, Name: "open", Node: node}
	return checkedExpression{source: source, typ: resultUnion, token: property}
}

// checkFileMethodCall resolves read, write, seek, flush, and close on File.
func checkFileMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	switch name {
	case "read", "write", "seek", "flush", "close":
	default:
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.FileMethodNotFound(name)))}
	}
	if len(call.call.TypeArguments) != 0 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.FileMethodNoTypeArguments(name)))}
	}
	if call.ctx.names.cleanupDepth > 0 && name != "close" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.OnlyFileCloseMayBeDeferred()))}
	}
	call.receiver = valueFromPlace(call.receiver)
	if diagnostic := streamClosedDiagnostic(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	switch name {
	case "read":
		if diagnostic := streamCapabilityMismatch("read", call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
		}
	case "write", "flush":
		if diagnostic := streamCapabilityMismatch("write", call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
		}
	}
	var arguments []Operand
	var diagnostics compilerTypes.Diagnostics
	switch name {
	case "read":
		arguments, diagnostics = checkStreamReadArguments(call)
	case "write":
		arguments, diagnostics = checkStreamWriteArguments(call)
	case "seek":
		arguments, diagnostics = checkStreamSeekArguments(call)
	default:
		if len(call.call.Arguments) != 0 {
			diagnostics = compilerTypes.Diagnostics{messageAt(call.callee.Property, diag.CollectionMethodNoArguments(name))}
		}
	}
	if len(diagnostics) > 0 {
		return checkedExpression{token: call.callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	resultName := name
	if name == "flush" {
		resultName = "close"
	}
	resultUnion := streamResultUnion(resultName, call.ctx.typeEnvironment)
	if resultUnion == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(unknownAt(call.callee.Property))}
	}
	node := Expression{
		Kind:        StreamMethodCallExpression,
		Name:        name,
		Operand:     &call.receiver.source.Node,
		Arguments:   arguments,
		OperandType: compilerTypes.FileType,
		ResultType:  resultUnion,
		Span:        call.callee.Property.Span,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultUnion, Name: name, Node: node}
	checked := checkedExpression{source: source, typ: resultUnion, token: call.callee.Property}
	if name == "close" && call.ctx.names.cleanupDepth == 0 {
		markStreamClosed(call.receiver.source, call.callee.Property, call.ctx.names.flow)
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
