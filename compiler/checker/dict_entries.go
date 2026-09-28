package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func resolveDictEntryTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 2 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diag.GenericTypeArgumentCount("DictEntry", 2, len(expression.Arguments))))
	}
	key, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	value, diagnostic := resolveTypeUse(expression.Arguments[1], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	typ := typeEnvironment.DictEntryType(key.Type, value.Type)
	if typ == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diag.InvalidListElementType("DictEntry fields")))
	}
	return compilerTypes.NewTypeUse(typ), nil
}
