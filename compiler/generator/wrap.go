package generator

// Signed wrapping +, -, *, and unary - lower through ckd_* helpers. Hexal's
// contract is modulo-width wrapping with defined two's-complement results;
// the overflow flag is intentionally discarded. The stored result is
// guaranteed by the pinned GCC/Clang overflow-builtin behavior, not by C23
// §7.20.1 paragraph 5 alone, which WG14 issue 1063 leaves defect-affected
// for out-of-range signed results.

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// wrapOperation is one selected signed wrapping operation/type pair.
type wrapOperation struct {
	name string // "add", "sub", "mul", or "neg"
	typ  compilerTypes.Type
}

// wrapHelperName returns the program-wide helper name for one pair.
func wrapHelperName(operation wrapOperation) string {
	return "hex_wrap_" + operation.name + "_" + operation.typ.CName
}

// generatedWrapState records the wrapping operation/type pairs one module
// selects, in deterministic discovery order.
type generatedWrapState struct {
	order []wrapOperation
	seen  map[string]bool
}

// discoverGeneratedWraps walks the program collecting every runtime signed
// wrapping operation. Constant-folded arithmetic never reaches generation, so
// only runtime nodes are collected; identical pairs from any module are one
// program-wide specialization.
func discoverGeneratedWraps(program checker.Program) *generatedWrapState {
	state := &generatedWrapState{seen: make(map[string]bool)}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) {
			var name string
			switch {
			case node.Kind == checker.UnaryOperationExpression && node.Operator == checker.NegateOperator && compilerTypes.IsSignedInteger(node.OperandType):
				name = "neg"
			case node.Kind == checker.BinaryOperationExpression && compilerTypes.IsSignedInteger(node.OperandType):
				switch node.Operator {
				case checker.AddOperator:
					name = "add"
				case checker.SubtractOperator:
					name = "sub"
				case checker.MultiplyOperator:
					name = "mul"
				}
			}
			if name == "" || node.OperandType == (compilerTypes.Type{}) {
				return
			}
			key := name + ":" + node.OperandType.CName
			if !state.seen[key] {
				state.seen[key] = true
				state.order = append(state.order, wrapOperation{name: name, typ: node.OperandType})
			}
		},
	}
	walkProgram(program, visitor)
	return state
}

// mergeWrapState unions per-module wrap selections into the program-wide
// helper set, preserving module order and deduplicating by operation/type.
func mergeWrapState(merged *generatedWrapState, module *generatedWrapState) {
	if module == nil {
		return
	}
	for _, operation := range module.order {
		key := operation.name + ":" + operation.typ.CName
		if !merged.seen[key] {
			merged.seen[key] = true
			merged.order = append(merged.order, operation)
		}
	}
}
