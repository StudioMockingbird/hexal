package generator

import (
	"slices"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedViewState records the view types that need definitions, in
// deterministic order, plus whether the view component must exist even
// without view records because another component declares it as a dependency
// .
type generatedViewState struct {
	views    []compilerTypes.Type
	seen     map[*compilerTypes.ViewInfo]bool
	required bool
}

// discoverGeneratedViews walks every type reachable from the program and
// collects the distinct view types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedViews(program checker.Program) *generatedViewState {
	state := &generatedViewState{seen: make(map[*compilerTypes.ViewInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) {
			if typ.View != nil {
				if !state.seen[typ.View] {
					state.seen[typ.View] = true
					state.views = append(state.views, typ)
				}
			}
		},
	}
	walkProgram(program, visitor)

	slices.SortStableFunc(state.views, func(left, right compilerTypes.Type) int {
		return strings.Compare(left.CName, right.CName)
	})
	return state
}

// ensureViewUInt8 adds the byte view type to the view state if missing; the
// String helpers always reference it.
func ensureViewUInt8(state *generatedViewState) {
	if state == nil {
		return
	}
	for _, view := range state.views {
		if view.CName == "hex_view_UInt8" {
			return
		}
	}
	view := compilerTypes.NewEnvironment().ViewType(compilerTypes.UInt8)
	state.seen[view.View] = true
	state.views = append(state.views, view)
}

// viewCName returns the C struct name of the view type over one element.

func validateViewBridgeExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.ViewBridgeExpression:
		if node.OperandType.View == nil || !compilerTypes.Equal(node.Element, node.OperandType.View.Element) || !compilerTypes.Equal(node.ResultType, node.OperandType) {
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

func renderViewBridgeExpression(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.ViewBridgeExpression:
		// The descriptor is one pointer-and-count initialization; the pointer
		// expression precedes the length expression in source order and each
		// appears exactly once.
		if node.OperandType.View == nil {
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
