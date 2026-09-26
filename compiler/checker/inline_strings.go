package checker

import (
	"strconv"
	"strings"

	"hexal/compiler/config"
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// inlineStringCapacity reads one capacity argument: a positive decimal integer
// literal no larger than the inline maximum. Digit separators are accepted, as
// in every decimal literal. Anything else, including a named constant or a
// generic parameter, is not a capacity.
func inlineStringCapacity(argument parser.TypeExpression, fallback lexer.Token) (uint64, *compilerTypes.Diagnostic) {
	literal, ok := argument.(parser.LiteralTypeArgument)
	if !ok {
		return 0, diagnosticAt(messageAt(typeExpressionToken(argument, fallback), diag.InvalidStringCapacityLiteral()))
	}
	if literal.Token.Kind != lexer.Integer {
		return 0, diagnosticAt(messageAt(literal.Token, diag.InvalidStringCapacityLiteral()))
	}
	capacity, err := strconv.ParseUint(strings.ReplaceAll(literal.Token.Lexeme, "_", ""), 10, 64)
	if err != nil || capacity == 0 {
		return 0, diagnosticAt(messageAt(literal.Token, diag.InvalidStringCapacityLiteral()))
	}
	if capacity > config.MaxInlineStringCapacity {
		return 0, diagnosticAt(messageAt(literal.Token, diag.StringCapacityExceedsMaximum(capacity, config.MaxInlineStringCapacity)))
	}
	return capacity, nil
}

// resolveStringTypeUse resolves the written String<N> form.
func resolveStringTypeUse(expression parser.StringTypeExpression, typeEnvironment *compilerTypes.Environment) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Keyword, diag.StringCapacityArgumentCount()))
	}
	capacity, diagnostic := inlineStringCapacity(expression.Arguments[0], expression.Keyword)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	return compilerTypes.NewTypeUse(typeEnvironment.InlineStringType(capacity)), nil
}

func textMismatchDetails(destination, source compilerTypes.Type) diag.TextMismatch {
	switch {
	case !compilerTypes.IsText(destination) || !compilerTypes.IsText(source):
		return diag.TextMismatch{Kind: diag.NoTextMismatch}
	case compilerTypes.IsString(destination):
		return diag.TextMismatch{Kind: diag.CopyTextToString}
	case compilerTypes.IsInlineString(source) && source.InlineString.Capacity <= destination.InlineString.Capacity:
		return diag.TextMismatch{Kind: diag.WidenInlineText, Capacity: destination.InlineString.Capacity}
	default:
		return diag.TextMismatch{Kind: diag.ConvertTextFromBytes, Destination: destination.Name}
	}
}
