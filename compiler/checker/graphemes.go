package checker

import (
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkGraphemeMethodCall dispatches the Grapheme surface: bytes, the borrowed
// range as a Slice<Byte>, and rune_length, its scalar count. A Grapheme is a
// view, so no operation copies or owns its bytes.
func checkGraphemeMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	fail := func(message string) checkedExpression {
		diagnostic := typeErrorAt(callee.Property, message)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	var result compilerTypes.Type
	switch name {
	case "bytes":
		result = ctx.typeEnvironment.SliceType(compilerTypes.UInt8, false)
		if result == (compilerTypes.Type{}) {
			return fail("Grapheme.bytes has no slice result")
		}
	case "rune_length":
		result = compilerTypes.SizeType
	default:
		return fail("Grapheme has no method " + name)
	}
	if len(call.Arguments) != 0 {
		return fail(name + " expects no arguments")
	}
	if len(call.TypeArguments) != 0 {
		return fail(name + " takes no type arguments")
	}
	node := Expression{
		Kind:        GraphemeMethodCallExpression,
		Name:        name,
		Operand:     &receiver.source.Node,
		OperandType: compilerTypes.GraphemeType,
		ResultType:  result,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
	return checkedExpression{source: source, typ: result, token: callee.Property}
}
