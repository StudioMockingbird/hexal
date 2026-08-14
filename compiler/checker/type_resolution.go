package checker

import (
	"fmt"
	"strings"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveType is the canonical-only compatibility wrapper around the
// contextual type-use resolver.
func resolveType(expression parser.TypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.Type, *compilerTypes.Diagnostic) {
	use, diagnostic := resolveTypeUse(expression, fallback, typeEnvironment, generics)
	return use.Type, diagnostic
}

// resolveTypeUse resolves syntax into canonical identity plus the written
// candidate order retained for contextual expression checking.
func resolveTypeUse(expression parser.TypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	switch expression := expression.(type) {
	case parser.NilTypeExpression:
		// RFC 0049 item 8.1: standalone Nil is invalid in every written type
		// position (aliases, bindings, parameters, results, members,
		// payloads, collection positions, generic arguments). Union members
		// and match type patterns resolve through resolveUnionMemberUse.
		diagnostic := typeErrorAt(expression.Token, "Nil is valid only as a member of a union with a non-Nil type")
		return compilerTypes.TypeUse{}, &diagnostic
	case parser.UnknownTypeExpression:
		return compilerTypes.NewTypeUse(compilerTypes.Unknown), nil
	case parser.NamedTypeExpression:
		if generics != nil && generics.frame != nil {
			if bound, ok := generics.frame[expression.Name.Lexeme]; ok {
				return compilerTypes.NewTypeUse(bound), nil
			}
		}
		resolved, ok := typeEnvironment.LookupUse(expression.Name.Lexeme)
		if !ok {
			message := "unknown type " + expression.Name.Lexeme
			return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError,
				Stage:    "checker",
				Line:     expression.Name.Line,
				Column:   expression.Name.Column,
				Message:  message,
			}
		}
		return resolved, nil
	case parser.QualifiedTypeExpression:
		// RFC 0034 Task 5: a dotted type name whose leftmost name is an
		// import alias resolves against the target module's exported type
		// records. A known alias whose name is absent or private reports the
		// visibility failure; an unknown leftmost name keeps the Task 3
		// Module Error.
		if generics != nil && generics.registry != nil {
			if target, ok := generics.registry.importTarget(generics.moduleID, expression.Module.Lexeme); ok {
				use, found := generics.registry.exportedType(target, expression.Names[0].Lexeme)
				if !found {
					diagnostic := privateToModuleDiagnostic(expression.Names[0], expression.Names[0].Lexeme, target)
					return compilerTypes.TypeUse{}, &diagnostic
				}
				return use, nil
			}
		}
		message := "unknown module alias " + expression.Module.Lexeme
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.ModuleError,
			Stage:    "checker",
			Line:     expression.Module.Line,
			Column:   expression.Module.Column,
			Message:  message,
		}
	case parser.GenericTypeExpression:
		if expression.Name.Lexeme == "View" {
			return resolveViewTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "List" {
			return resolveListTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Dict" {
			return resolveDictTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Stream" {
			return resolveStreamTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Task" {
			return resolveTaskTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Channel" {
			return resolveChannelTypeUse(expression, fallback, typeEnvironment, generics)
		}
		if expression.Name.Lexeme == "Atomic" {
			return resolveAtomicTypeUse(expression, fallback, typeEnvironment, generics)
		}
		return specializeTypeUse(expression, fallback, typeEnvironment, generics)
	case parser.GroupedTypeExpression:
		return resolveTypeUse(expression.Inner, fallback, typeEnvironment, generics)
	case parser.ArrayTypeExpression:
		return resolveArrayTypeUse(expression, fallback, typeEnvironment, generics)
	case parser.PtrTypeExpression:
		elementUse, diagnostic := resolveTypeUse(expression.Element, expression.Keyword, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		element := elementUse.Type
		if element.Signature != nil {
			// Supported-position whitelist: a pointer to a function pointer
			// needs C declarator and FFI rules this RFC defers.
			diagnostic := typeErrorAt(expression.Keyword, expression.Keyword.Lexeme+"<"+element.Name+"> is not supported")
			return compilerTypes.TypeUse{}, &diagnostic
		}
		var pointer compilerTypes.Type
		if expression.Writable {
			pointer = typeEnvironment.MutPtrType(element)
		} else {
			pointer = typeEnvironment.PtrType(element)
		}
		if pointer == (compilerTypes.Type{}) {
			return compilerTypes.TypeUse{}, typeErrorPointerConstruction(expression.Keyword)
		}
		return compilerTypes.PointerTypeUse(pointer, elementUse), nil
	case parser.FunctionTypeExpression:
		return resolveFunctionTypeUse(expression, typeEnvironment, generics)
	case parser.UnionTypeExpression:
		members := make([]compilerTypes.TypeUse, 0, len(expression.Members))
		canonical := make([]compilerTypes.Type, 0, len(expression.Members))
		for _, memberExpression := range expression.Members {
			member, diagnostic := resolveUnionMemberUse(memberExpression, fallback, typeEnvironment, generics)
			if diagnostic != nil {
				return compilerTypes.TypeUse{}, diagnostic
			}
			for _, candidate := range typeUseCandidates(member) {
				duplicate := false
				for _, existing := range members {
					if compilerTypes.Equal(existing.Type, candidate.Type) {
						duplicate = true
						break
					}
				}
				if duplicate {
					continue
				}
				members = append(members, candidate)
				canonical = append(canonical, candidate.Type)
			}
		}
		// RFC 0049 item 8.1: alias resolution and generic substitution run
		// before the member count, so a written union that collapses to fewer
		// than two distinct canonical members is an error, never an alias for
		// the survivor.
		if len(canonical) < 2 {
			names := make([]string, 0, len(expression.Members))
			for _, memberExpression := range expression.Members {
				use, _ := resolveUnionMemberUse(memberExpression, fallback, typeEnvironment, generics)
				names = append(names, use.Type.Name)
			}
			diagnostic := typeErrorAt(typeExpressionToken(expression, fallback), fmt.Sprintf("a union requires at least two distinct members; %s has one", strings.Join(names, " | ")))
			return compilerTypes.TypeUse{}, &diagnostic
		}
		union := typeEnvironment.UnionType(canonical)
		if union == (compilerTypes.Type{}) {
			for _, member := range members {
				if compilerTypes.IsUnknown(member.Type) {
					diagnostic := typeErrorAt(typeExpressionToken(expression, fallback), "Unknown | Nil is not a value type; use Ptr<Unknown> | Nil")
					return compilerTypes.TypeUse{}, &diagnostic
				}
			}
			diagnostic := typeErrorAt(typeExpressionToken(expression, fallback), "could not construct union type")
			return compilerTypes.TypeUse{}, &diagnostic
		}
		if len(members) == 1 {
			return members[0], nil
		}
		return compilerTypes.UnionTypeUse(union, members), nil
	default:
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     fallback.Line,
			Column:   fallback.Column,
			Message:  "unsupported type expression",
		}
	}
}

func typeUseCandidates(use compilerTypes.TypeUse) []compilerTypes.TypeUse {
	if len(use.Candidates) == 0 {
		return []compilerTypes.TypeUse{use}
	}
	return append([]compilerTypes.TypeUse(nil), use.Candidates...)
}

// resolveUnionMemberUse resolves a type in a Nil-admitting context: a union
// member, a match type pattern, or an is-test query. Everywhere else Nil is
// rejected by resolveTypeUse (RFC 0049 item 8.1). Parenthesized members and
// nested written unions recurse so `Int32 | (Nil | Float32)` flattens
// correctly.
func resolveUnionMemberUse(expression parser.TypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	switch expression := expression.(type) {
	case parser.NilTypeExpression:
		return compilerTypes.NewTypeUse(compilerTypes.Nil), nil
	case parser.GroupedTypeExpression:
		return resolveUnionMemberUse(expression.Inner, fallback, typeEnvironment, generics)
	default:
		return resolveTypeUse(expression, fallback, typeEnvironment, generics)
	}
}

func typeErrorPointerConstruction(token lexer.Token) *compilerTypes.Diagnostic {
	diagnostic := typeErrorAt(token, "could not construct pointer type")
	return &diagnostic
}

func valueTypeDiagnostic(expression parser.TypeExpression, fallback lexer.Token, typ compilerTypes.Type) *compilerTypes.Diagnostic {
	if !compilerTypes.IsUnknown(typ) {
		return nil
	}
	diagnostic := typeErrorAt(typeExpressionToken(expression, fallback), "Unknown has no known size or layout; it may only be used behind a pointer")
	return &diagnostic
}

func typeExpressionToken(expression parser.TypeExpression, fallback lexer.Token) lexer.Token {
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		return expression.Name
	case parser.NilTypeExpression:
		return expression.Token
	case parser.UnknownTypeExpression:
		return expression.Token
	case parser.PtrTypeExpression:
		return expression.Keyword
	case parser.FunctionTypeExpression:
		return expression.Keyword
	case parser.UnionTypeExpression:
		if len(expression.Members) > 0 {
			return typeExpressionToken(expression.Members[0], fallback)
		}
	case parser.GroupedTypeExpression:
		return expression.OpenParen
	default:
		return fallback
	}
	return fallback
}

// resolveFunctionTypeUse resolves Fun<(T, U) : R> while retaining nested type
// views for contextual arguments and results.
func resolveFunctionTypeUse(expression parser.FunctionTypeExpression, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	parameterUses := make([]compilerTypes.TypeUse, 0, len(expression.Parameters))
	parameters := make([]compilerTypes.Type, 0, len(expression.Parameters))
	for _, parameter := range expression.Parameters {
		resolvedUse, diagnostic := resolveTypeUse(parameter, expression.Keyword, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		if diagnostic := valueTypeDiagnostic(parameter, expression.Keyword, resolvedUse.Type); diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		parameterUses = append(parameterUses, resolvedUse)
		parameters = append(parameters, resolvedUse.Type)
	}
	var result *compilerTypes.Type
	var resultUse *compilerTypes.TypeUse
	if expression.Return != nil {
		resolvedUse, diagnostic := resolveTypeUse(expression.Return, expression.Keyword, typeEnvironment, generics)
		if diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		if diagnostic := valueTypeDiagnostic(expression.Return, expression.Keyword, resolvedUse.Type); diagnostic != nil {
			return compilerTypes.TypeUse{}, diagnostic
		}
		if resolvedUse.Type.Signature != nil {
			diagnostic := typeErrorAt(expression.Keyword, "returning "+resolvedUse.Type.Name+" is not supported")
			return compilerTypes.TypeUse{}, &diagnostic
		}
		resolved := resolvedUse.Type
		result = &resolved
		resultUse = &resolvedUse
	}
	functionType := typeEnvironment.FunType(parameters, result)
	if functionType.Signature == nil {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Line:     expression.Keyword.Line,
			Column:   expression.Keyword.Column,
			Message:  "could not construct a Fun type",
		}
	}
	return compilerTypes.FunctionTypeUse(functionType, parameterUses, resultUse), nil
}
