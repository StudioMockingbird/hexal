package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

// checkRuneMethodCall dispatches the Rune value surface. Rune lowers to the
// uint32_t scalar, so value and utf8_length are pure reads with no runtime
// allocation.
func checkRuneMethodCall(call methodCall) checkedExpression {
	fail := func(message diag.Message) checkedExpression {
		diagnostic := messageAt(call.callee.Property, message)
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	var result compilerTypes.Type
	switch call.callee.Property.Lexeme {
	case "value":
		result = compilerTypes.UInt32
	case "utf8_length":
		result = compilerTypes.SizeType
	case "is_lower", "is_upper", "is_alphabetic", "is_numeric", "is_whitespace":
		result = compilerTypes.Bool
	case "to_lower", "to_upper", "to_title":
		result = compilerTypes.Rune
	case "display_width":
		result = compilerTypes.Int32
	case "combining_class":
		result = compilerTypes.UInt8
	case "category":
		result = compilerTypes.UnicodeCategoryType
	default:
		return fail(diag.RuneMethodNotFound(call.callee.Property.Lexeme))
	}
	if len(call.call.Arguments) != 0 {
		return fail(diag.RuneMethodNoValueArguments(call.callee.Property.Lexeme))
	}
	if len(call.call.TypeArguments) != 0 {
		return fail(diag.RuneMethodNoTypeArguments(call.callee.Property.Lexeme))
	}
	node := Expression{
		Kind:        RuneMethodCallExpression,
		Name:        call.callee.Property.Lexeme,
		Operand:     &call.receiver.source.Node,
		OperandType: compilerTypes.Rune,
		ResultType:  result,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: call.callee.Property.Lexeme, Node: node}
	return checkedExpression{source: source, typ: result, token: call.callee.Property}
}

// rejectRuneTypeCall rejects every Rune.<name>(...) call: the type has no
// type-level operations, and a scalar built from an integer is
// value.to<Rune>().
func rejectRuneTypeCall(callee lexer.Token) checkedExpression {
	diagnostic := messageAt(callee, diag.RuneOperationUnsupported())
	return checkedExpression{token: callee, diagnostic: &diagnostic}
}
