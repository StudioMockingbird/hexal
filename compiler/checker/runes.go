package checker

import (
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkRuneMethodCall dispatches the Rune value surface. Rune lowers to the
// uint32_t scalar, so value and utf8_length are pure reads with no runtime
// allocation.
func checkRuneMethodCall(call methodCall) checkedExpression {
	fail := func(message string) checkedExpression {
		diagnostic := typeErrorAt(call.callee.Property, message)
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
		return fail("Rune has no method " + call.callee.Property.Lexeme)
	}
	if len(call.call.Arguments) != 0 {
		return fail(call.callee.Property.Lexeme + " expects no arguments")
	}
	if len(call.call.TypeArguments) != 0 {
		return fail(call.callee.Property.Lexeme + " takes no type arguments")
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
	fail := func(message string) checkedExpression {
		diagnostic := typeErrorAt(callee, message)
		return checkedExpression{token: callee, diagnostic: &diagnostic}
	}
	if call.Callee.(parser.PropertyExpression).Property.Lexeme != "from" {
		return fail("Rune has no such operation; use Rune.from(value)")
	}
	if len(call.Arguments) != 1 {
		return fail("Rune.from expects 1 argument")
	}
	if len(call.TypeArguments) != 0 {
		return fail("Rune.from takes no type arguments")
	}
	value := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.UInt32), callee, ctx)
	if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
		return checkedExpression{token: value.token, diagnostics: diagnostics}
	}
	if !compilerTypes.Equal(value.typ, compilerTypes.UInt32) {
		return fail("Rune.from requires a UInt32; got " + value.typ.Name)
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
