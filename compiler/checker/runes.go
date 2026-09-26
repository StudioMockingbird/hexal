package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
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

// checkRuneTypeCall resolves the type-level Rune.from(value) constructor: the
// one operation that turns a UInt32 into a checked scalar, rejecting
// surrogates and values above U+10FFFF at runtime.
func checkRuneTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	fail := func(message diag.Message) checkedExpression {
		diagnostic := messageAt(callee, message)
		return checkedExpression{token: callee, diagnostic: &diagnostic}
	}
	if call.Callee.(parser.PropertyExpression).Property.Lexeme != "from" {
		return fail(diag.RuneOperationUnsupported())
	}
	if len(call.Arguments) != 1 {
		return fail(diag.RuneFromArgumentCount())
	}
	if len(call.TypeArguments) != 0 {
		return fail(diag.RuneFromTypeArguments())
	}
	value := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.UInt32), callee, ctx)
	if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
		return checkedExpression{token: value.token, diagnostics: diagnostics}
	}
	if !compilerTypes.Equal(value.typ, compilerTypes.UInt32) {
		return fail(diag.RuneFromValueType(value.typ.Name))
	}
	union, failure := textFailureUnion(compilerTypes.Rune, callee, ctx)
	if failure != nil {
		return *failure
	}
	node := Expression{
		Kind:        RuneMethodCallExpression,
		Name:        "from",
		Arguments:   []Operand{value.source},
		OperandType: compilerTypes.Rune,
		ResultType:  union,
		Span:        callee.Span,
	}
	source := Operand{Kind: ExpressionOperand, Type: union, Name: "from", Node: node}
	return checkedExpression{source: source, typ: union, token: callee}
}
