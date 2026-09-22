package checker

import (
	"fmt"
	"strconv"
	"strings"

	"hexal/compiler/config"
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
		return 0, diagnosticAt(typeErrorAt(typeExpressionToken(argument, fallback), "String capacity must be a positive integer literal"))
	}
	if literal.Token.Kind != lexer.Integer {
		return 0, diagnosticAt(typeErrorAt(literal.Token, "String capacity must be a positive integer literal"))
	}
	capacity, err := strconv.ParseUint(strings.ReplaceAll(literal.Token.Lexeme, "_", ""), 10, 64)
	if err != nil || capacity == 0 {
		return 0, diagnosticAt(typeErrorAt(literal.Token, "String capacity must be a positive integer literal"))
	}
	if capacity > config.MaxInlineStringCapacity {
		message := fmt.Sprintf("String capacity %d exceeds the maximum of %d", capacity, config.MaxInlineStringCapacity)
		return 0, diagnosticAt(typeErrorAt(literal.Token, message))
	}
	return capacity, nil
}

// resolveStringTypeUse resolves the written String<N> form.
func resolveStringTypeUse(expression parser.StringTypeExpression, typeEnvironment *compilerTypes.Environment) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Keyword, "String takes at most one capacity argument"))
	}
	capacity, diagnostic := inlineStringCapacity(expression.Arguments[0], expression.Keyword)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	return compilerTypes.NewTypeUse(typeEnvironment.InlineStringType(capacity)), nil
}

// textMismatchHint names the explicit conversion between two text forms that a
// position refuses to make implicitly: no position converts between text forms
// on its own, so a mismatch between them says how to convert. It is empty when
// either type is not text.
func textMismatchHint(destination, source compilerTypes.Type) string {
	switch {
	case !compilerTypes.IsText(destination) || !compilerTypes.IsText(source):
		return ""
	case compilerTypes.IsString(destination):
		return "; use copy(heap)"
	case compilerTypes.IsInlineString(source) && source.InlineString.Capacity <= destination.InlineString.Capacity:
		return fmt.Sprintf("; use widen<%d>()", destination.InlineString.Capacity)
	}
	return fmt.Sprintf("; use %s.from_bytes(...) for a checked conversion", destination.Name)
}

// removedTextTypeHints names the replacement for each former text type. The
// names are not reserved: a program may declare its own, and the hint appears
// only when a name resolves to nothing.
var removedTextTypeHints = map[string]string{
	"Strand": "use String<N> (String<31> keeps the former capacity)",
}

// unknownTypeMessage is the diagnostic for a type name that resolves to
// nothing, extended with the migration hint for a removed text type.
func unknownTypeMessage(name string) string {
	message := "unknown type " + name
	if hint, removed := removedTextTypeHints[name]; removed {
		message += "; " + hint
	}
	return message
}
