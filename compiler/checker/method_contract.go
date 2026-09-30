package checker

import (
	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"

	diagnosticsPkg "hexal/compiler/diagnostics"
)

// Method contract: a method declared without mut is readonly, and its body
// may not write receiver-owned storage, take a writable address of it, or
// call a mut method on it. The declaration is the contract; the body is
// verified against it at the first proven write.

// rootedAtSelfThroughValues reports whether node names storage the receiver
// owns: self, or a member or inline-List element path below it that crosses
// no indirection. A pointer dereference, Slice, allocated List, Dict, or
// String handle changes separately owned storage instead, so it ends the walk.
func rootedAtSelfThroughValues(node *Expression, selfID BindingID) bool {
	for node != nil {
		switch node.Kind {
		case VariableExpression:
			return selfID != 0 && node.Binding == selfID
		case MemberExpression, UnionPayloadExpression, AdtPayloadExpression:
			node = node.Operand
		case IndexExpression:
			if node.OperandType.InlineList == nil {
				return false
			}
			node = node.Operand
		default:
			return false
		}
	}
	return false
}

// readonlySelfWrite reports whether node is receiver-owned storage inside the
// body of a method declared without mut.
func (names *scope) readonlySelfWrite(node *Expression) bool {
	return names.self != nil && !names.selfMutating && rootedAtSelfThroughValues(node, names.selfID)
}

// readonlySelfWriteDiagnostic names the receiver type and method of the body
// being checked and the write that breaks its readonly contract.
func readonlySelfWriteDiagnostic(names *scope, token lexer.Token, action string) compilerTypes.Diagnostic {
	owner := names.self.Name
	if names.self.Object != nil {
		owner = names.self.Object.Name
	}
	return messageAt(token, diagnosticsPkg.ReadonlyMethodWritesSelf(owner, names.owner, action))
}

// inlineListMutator reports whether a built-in inline List method writes the
// list's own bytes or hands out a writable view of them.
func inlineListMutator(name string) bool {
	switch name {
	case "push", "pop", "clear", "mut_slice":
		return true
	}
	return false
}
