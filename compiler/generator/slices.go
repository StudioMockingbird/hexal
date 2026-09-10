package generator

import (
	"slices"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedSliceState records the slice types that need definitions, in
// deterministic order, plus whether the slice component must exist even
// without slice records because another component declares it as a dependency
// .
type generatedSliceState struct {
	slices   []compilerTypes.Type
	seen     map[*compilerTypes.SliceInfo]bool
	required bool
}

// discoverGeneratedSlices walks every type reachable from the program and
// collects the distinct slice types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedSlices(program checker.Program) *generatedSliceState {
	state := &generatedSliceState{seen: make(map[*compilerTypes.SliceInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.Slice != nil {
				if !state.seen[typ.Slice] {
					state.seen[typ.Slice] = true
					state.slices = append(state.slices, typ)
				}
			}
			return nil
		},
	}
	walkProgram(program, visitor)

	slices.SortStableFunc(state.slices, func(left, right compilerTypes.Type) int {
		return strings.Compare(left.CName, right.CName)
	})
	return state
}

// ensureSliceUInt8 adds the byte slice type to the slice state if missing; the
// String helpers always reference it.
func ensureSliceUInt8(state *generatedSliceState) {
	if state == nil {
		return
	}
	for _, slice := range state.slices {
		if slice.CName == "hex_slice_UInt8" {
			return
		}
	}
	slice := compilerTypes.NewEnvironment().SliceType(compilerTypes.UInt8, false)
	state.seen[slice.Slice] = true
	state.slices = append(state.slices, slice)
}

// viewCName returns the C struct name of the slice type over one element.

func validateSliceBridgeExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.SliceBridgeExpression:
		if node.OperandType.Slice == nil || !compilerTypes.Equal(node.Element, node.OperandType.Slice.Element) || !compilerTypes.Equal(node.ResultType, node.OperandType) {
			return unknownExpressionDiagnostic("view bridge has invalid checked metadata")
		}
		switch node.Name {
		case "empty":
			if len(node.Arguments) != 0 {
				return unknownExpressionDiagnostic("view bridge empty has unexpected arguments")
			}
		case "from_pointer":
			if len(node.Arguments) != 2 {
				return unknownExpressionDiagnostic("view bridge from_pointer has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
			if err := validateCheckedOperandWithState(node.Arguments[1], state); err != nil {
				return err
			}
		default:
			return unknownExpressionDiagnostic("unknown view bridge form " + node.Name)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("view bridge result type does not match its expected type")
		}
		return nil
	}
	return unknownExpressionDiagnostic("unsupported view bridge expression")
}

func renderSliceBridgeExpression(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.SliceBridgeExpression:
		// The descriptor is one pointer-and-count initialization; the pointer
		// expression precedes the length expression in source order and each
		// appears exactly once.
		if node.OperandType.Slice == nil {
			return "", unknownExpressionDiagnostic("view bridge without a checked View type")
		}
		if node.Name == "empty" {
			if len(node.Arguments) != 0 {
				return "", unknownExpressionDiagnostic("view bridge empty with unexpected arguments")
			}
			return "(" + node.OperandType.CName + "){ nullptr, 0 }", nil
		}
		if len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic("view bridge without checked pointer and length")
		}
		pointer, pointerErr := renderOperandWithState(node.Arguments[0], state)
		if pointerErr != nil {
			return "", pointerErr
		}
		length, lengthErr := renderOperandWithState(node.Arguments[1], state)
		if lengthErr != nil {
			return "", lengthErr
		}
		return "(" + node.OperandType.CName + "){ " + pointer + ", " + length + " }", nil
	}
	return "", unknownExpressionDiagnostic("unsupported view bridge expression")
}
