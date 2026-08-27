package generator

import (
	"slices"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedPoolState records the pool types that need header and helper
// definitions, in deterministic order. Pool<T> is fully monomorphized (like
// List<T>), not a shared type-erased handle (unlike Stash<T>): O(1)
// allocate/free needs contiguous, compile-time-typed slot storage.
type generatedPoolState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.PoolInfo]bool
}

// discoverGeneratedPool walks every type reachable from the program and
// collects the distinct pool types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedPool(program checker.Program) *generatedPoolState {
	state := &generatedPoolState{seen: make(map[*compilerTypes.PoolInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.Pool != nil {
				if !state.seen[typ.Pool] {
					state.seen[typ.Pool] = true
					state.order = append(state.order, typ)
				}
			}
			return nil
		},
	}
	walkProgram(program, visitor)

	slices.SortStableFunc(state.order, func(left, right compilerTypes.Type) int {
		return strings.Compare(left.CName, right.CName)
	})
	return state
}

func poolSuffix(pool compilerTypes.Type) string {
	return strings.TrimPrefix(pool.CName, "hex_pool_")
}

func poolNewHelper(pool compilerTypes.Type) string     { return "hex_pool_new_" + poolSuffix(pool) }
func poolAllocHelper(pool compilerTypes.Type) string   { return "hex_pool_alloc_" + poolSuffix(pool) }
func poolFreeHelper(pool compilerTypes.Type) string    { return "hex_pool_free_" + poolSuffix(pool) }
func poolDestroyHelper(pool compilerTypes.Type) string { return "hex_pool_destroy_" + poolSuffix(pool) }

func renderPoolConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if node.OperandType.Pool == nil || len(node.Arguments) != 1 {
		return "", unknownExpressionDiagnostic("pool constructor has invalid checked metadata")
	}
	capacity, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return poolNewHelper(node.OperandType) + "(" + capacity + ")", nil
}

func renderPoolMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Pool == nil {
		return "", unknownExpressionDiagnostic("pool method has invalid checked metadata")
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "allocate":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("pool allocate has invalid checked metadata")
		}
		initial, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return poolAllocHelper(node.OperandType) + "(" + receiver + ", " + initial + ")", nil
	case "free":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("pool free has invalid checked metadata")
		}
		pointer, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return poolFreeHelper(node.OperandType) + "(" + receiver + ", " + pointer + ")", nil
	case "destroy":
		return poolDestroyHelper(node.OperandType) + "(" + receiver + ")", nil
	default:
		return "", unknownExpressionDiagnostic("unknown pool method " + node.Name)
	}
}

// validatePoolExpression is the fail-closed structural check for every Pool
// checked expression kind, reached from validateExpressionNode.
func validatePoolExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.PoolConstructorExpression:
		if node.OperandType.Pool == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.Element, node.OperandType.Pool.Element) || !compilerTypes.Equal(node.ResultType, node.OperandType) {
			return unknownExpressionDiagnostic("pool constructor has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("pool constructor result type does not match its expected type")
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.PoolMethodCallExpression:
		if node.Operand == nil || node.OperandType.Pool == nil || !compilerTypes.Equal(node.Element, node.OperandType.Pool.Element) {
			return unknownExpressionDiagnostic("pool method has invalid checked metadata")
		}
		switch node.Name {
		case "allocate":
			if len(node.Arguments) != 1 || node.ResultType.Element == nil || !node.ResultType.PointeeWritable || !compilerTypes.Equal(*node.ResultType.Element, node.Element) {
				return unknownExpressionDiagnostic("pool allocate has invalid checked metadata")
			}
		case "free":
			if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("pool free has invalid checked metadata")
			}
		case "destroy":
			if len(node.Arguments) != 0 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("pool destroy has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown pool method " + node.Name)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("pool method result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	}
	return unknownExpressionDiagnostic("unknown pool expression kind")
}
