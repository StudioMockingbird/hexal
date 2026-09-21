package checker

// Module constants: a fixed top-level `let` in a non-entry module, lowered to
// one immutable owner-qualified C object. The accepted initializer set is a
// closed allowlist the compiler can lower directly to a C constant, and no
// binding reference is part of it.

import (
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// moduleConstantInitializerDiagnostic rejects an initializer outside the
// closed static set.
func moduleConstantInitializerDiagnostic(name lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(name, "module constant "+name.Lexeme+" must be statically initialized")
}

// checkModuleConstant checks one fixed top-level declaration in a non-entry
// module as a module constant: its type, its initializer against the closed
// static-value allowlist, and its Atomic containment. A mutable declaration is
// rejected before its initializer is checked.
func checkModuleConstant(declaration parser.Declaration, moduleID string, ctx checkContext, itemIndex int, typeIndexByName map[string]int) (ModuleValueDeclaration, compilerTypes.Diagnostics) {
	if declaration.Mutable {
		return ModuleValueDeclaration{}, compilerTypes.Diagnostics{
			moduleErrorAt(declaration.Name, "imported module "+moduleID+" cannot declare mutable top-level binding "+declaration.Name.Lexeme+"; pass explicit state instead"),
		}
	}
	var expectedUse compilerTypes.TypeUse
	hasExpected := false
	if declaration.Type != nil {
		if token, tooLate := firstTypeNameDeclaredAtOrAfter(declaration.Type, itemIndex, typeIndexByName); tooLate {
			return ModuleValueDeclaration{}, compilerTypes.Diagnostics{typeErrorAt(token, unknownTypeMessage(token.Lexeme))}
		}
		use, diagnostic := resolveTypeUse(declaration.Type, declaration.Name, ctx.typeEnvironment, ctx.names.generics)
		if diagnostic != nil {
			return ModuleValueDeclaration{}, compilerTypes.Diagnostics{*diagnostic}
		}
		expectedUse = use
		hasExpected = true
	}
	var checked initializerValue
	if hasExpected {
		checked = checkInitializer(declaration.Initializer, expectedUse, declaration.Name, ctx)
	} else {
		checked = checkValue(declaration.Initializer, ctx)
	}
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return ModuleValueDeclaration{}, diagnostics
	}
	if hasExpected && !assignable(expectedUse.Type, checked.typ) {
		return ModuleValueDeclaration{}, compilerTypes.Diagnostics{typeMismatchDiagnostic(expectedUse.Type, checked.typ, checked.token)}
	}
	// An Atomic is mutable runtime state even when its binding is fixed, so a
	// constant type may not contain one directly or through any aggregate.
	if compilerTypes.ContainsAtomic(checked.typ) {
		return ModuleValueDeclaration{}, compilerTypes.Diagnostics{
			typeErrorAt(declaration.Name, "module constant "+declaration.Name.Lexeme+" cannot contain mutable Atomic state; pass explicit state instead"),
		}
	}
	if !isStaticInitializerOperand(checked.source) {
		return ModuleValueDeclaration{}, compilerTypes.Diagnostics{moduleConstantInitializerDiagnostic(declaration.Name)}
	}
	return ModuleValueDeclaration{
		Name:         declaration.Name.Lexeme,
		Binding:      ctx.names.newBindingID(),
		Type:         checked.typ,
		TypeUse:      checked.use,
		Source:       checked.source,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}, nil
}

// isStaticInitializerOperand reports whether operand is built entirely from
// the accepted static initializer tree: literals, Array literals, struct
// construction, ADT variant construction, and structural-union injection,
// recursively. It is a closed allowlist, never a rejection blocklist.
func isStaticInitializerOperand(operand Operand) bool {
	switch operand.Kind {
	case ConstantOperand:
		if operand.Object != nil {
			return isStaticInitializerObject(*operand.Object)
		}
		return true
	case ObjectOperand:
		if operand.Object == nil {
			return false
		}
		return isStaticInitializerObject(*operand.Object)
	case ExpressionOperand, VariableOperand:
		return isStaticInitializerExpression(operand.Node)
	}
	return false
}

func isStaticInitializerObject(value ObjectValue) bool {
	for _, initializer := range value.Initializers {
		if !isStaticInitializerOperand(initializer.Source) {
			return false
		}
	}
	return true
}

func isStaticInitializerExpression(node Expression) bool {
	switch node.Kind {
	case ArrayLiteralExpression:
		for _, argument := range node.Arguments {
			if !isStaticInitializerOperand(argument) {
				return false
			}
		}
		return true
	case AdtConstructExpression:
		for _, argument := range node.Arguments {
			if !isStaticInitializerOperand(argument) {
				return false
			}
		}
		return true
	case UnionInjectionExpression, WideningExpression:
		if node.Operand == nil {
			return false
		}
		return isStaticInitializerExpression(*node.Operand)
	case ConstantExpression:
		return true
	}
	return false
}
