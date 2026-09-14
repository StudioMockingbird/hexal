package checker

// Static module values: the `static [mut] name [: type] := expr` top-level
// declaration. Module storage introduces no executable initializer, so the
// accepted initializer set is a closed allowlist the compiler can lower
// directly to C static storage.

import (
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// moduleDataDiagnostic already exists for the unreachable-module-data case;
// staticInitializerDiagnostic is this feature's own rejection.
func staticInitializerDiagnostic(token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, "module value initializer requires a static value")
}

// checkModuleValueDeclaration checks one top-level `static` declaration: its
// type (declared or inferred), its initializer against the static-value
// allowlist, and registers it as module storage rather than a lexical local.
func checkModuleValueDeclaration(declaration parser.ModuleValueDeclaration, ctx checkContext) (ModuleValueDeclaration, compilerTypes.Diagnostics) {
	var expectedUse compilerTypes.TypeUse
	hasExpected := false
	if declaration.Type != nil {
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

	// The direct fixed Atomic<T> module value is the one constructed-value
	// exception: it is accepted only as a fixed (non-mut) binding whose
	// initializer is exactly Atomic<T>(literal).
	isAtomic := checked.typ.Atomic != nil
	if isAtomic {
		if declaration.Mutable {
			return ModuleValueDeclaration{}, compilerTypes.Diagnostics{typeErrorAt(declaration.Name, "a mut Atomic module value is invalid; Atomic mutation belongs to its own operations")}
		}
		if checked.source.Node.Kind != AtomicConstructorExpression {
			return ModuleValueDeclaration{}, compilerTypes.Diagnostics{staticInitializerDiagnostic(checked.token)}
		}
	} else if !isStaticInitializerOperand(checked.source) {
		return ModuleValueDeclaration{}, compilerTypes.Diagnostics{staticInitializerDiagnostic(checked.token)}
	}

	binding := ctx.names.newBindingID()
	result := ModuleValueDeclaration{
		Name:         declaration.Name.Lexeme,
		Binding:      binding,
		Type:         checked.typ,
		TypeUse:      checked.use,
		Source:       checked.source,
		Mutable:      declaration.Mutable,
		Atomic:       isAtomic,
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}
	return result, nil
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
