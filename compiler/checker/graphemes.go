package checker

import (
	diag "hexal/compiler/diagnostics"
	compilerTypes "hexal/compiler/types"
)

// checkGraphemeMethodCall dispatches the Grapheme surface: bytes, the borrowed
// range as a Slice<Byte>, and rune_length, its scalar count. A Grapheme is a
// view, so no operation copies or owns its bytes.
func checkGraphemeMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	fail := func(message diag.Message) checkedExpression {
		diagnostic := messageAt(call.callee.Property, message)
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	var result compilerTypes.Type
	switch name {
	case "bytes":
		result = call.ctx.typeEnvironment.SliceType(compilerTypes.UInt8, false)
		if result == (compilerTypes.Type{}) {
			return fail(diag.GraphemeBytesMissingSliceResult())
		}
	case "rune_length":
		result = compilerTypes.SizeType
	default:
		return fail(diag.GraphemeMethodNotFound(name))
	}
	if len(call.call.Arguments) != 0 {
		return fail(diag.GraphemeMethodNoValueArguments(name))
	}
	if len(call.call.TypeArguments) != 0 {
		return fail(diag.GraphemeMethodNoTypeArguments(name))
	}
	node := Expression{
		Kind:        GraphemeMethodCallExpression,
		Name:        name,
		Operand:     &call.receiver.source.Node,
		OperandType: compilerTypes.GraphemeType,
		ResultType:  result,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
	return checkedExpression{source: source, typ: result, token: call.callee.Property}
}
