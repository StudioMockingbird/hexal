package checker

import (
	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

// isFreshAtomicConstruction reports whether source builds an Atomic-containing
// value entirely from fresh .new() constructions, with no read of existing
// storage. Such an expression may initialize a Binding or ObjectMember in
// place; any other use of an Atomic-containing value is a copy and is
// rejected.
func isFreshAtomicConstruction(source Operand) bool {
	switch source.Node.Kind {
	case AtomicConstructorExpression:
		return true
	case ObjectExpression:
		if source.Object == nil {
			return false
		}
		for _, member := range source.Object.Initializers {
			if !isFreshAtomicConstruction(member.Source) {
				return false
			}
		}
		return true
	case ArrayLiteralExpression:
		for _, element := range source.Node.Arguments {
			if !isFreshAtomicConstruction(element) {
				return false
			}
		}
		return true
	}
	return false
}

// atomicCopyDiagnostic reports the non-copyable-Atomic violation when a copy
// boundary receives an existing Atomic-containing value. Fresh construction is
// the one exemption. It returns nil when the value is copyable or fresh.
func atomicCopyDiagnostic(source Operand, token lexer.Token) *compilerTypes.Diagnostic {
	if !compilerTypes.ContainsAtomic(source.Type) || isFreshAtomicConstruction(source) {
		return nil
	}
	return diagnosticAt(typeErrorAt(token, "Atomic values cannot be copied, assigned, addressed, or stored here"))
}
