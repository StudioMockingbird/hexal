package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// stashHelpers records the element types allocated through a Stash so the
// generator can emit exactly one typed constructor/allocate helper pair per
// type. required tracks whether any Stash is used at all, independent of any
// particular element, selecting the shared type-erased hexal/stash.h core
// (a Stash<T> value is hex_stash * for every T; only the typed constructor
// and allocate wrappers need the element type).
type stashHelpers struct {
	elements []compilerTypes.Type
	seen     map[string]bool
	required bool
}

func discoverStashHelpers(program checker.Program) (*stashHelpers, error) {
	state := &stashHelpers{seen: make(map[string]bool)}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			switch node.Kind {
			case checker.StashConstructorExpression:
				state.required = true
				if node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) {
					return unknownExpressionDiagnostic("stash constructor without a complete checked element type")
				}
				if !state.seen[node.Element.CName] {
					state.seen[node.Element.CName] = true
					state.elements = append(state.elements, node.Element)
				}
			case checker.StashMethodCallExpression:
				state.required = true
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}

// writeStashHelpers emits the typed Stash constructor and allocate helpers
// into the module header. They are per-module because the element types are
// module-owned (objects, ADTs, unions) and must be defined before the
// helper; the shared type-erased bump-allocation core lives in
// hexal/stash.h -- reset and destroy call it directly with no per-T
// specialization, since neither touches T's representation.
func writeStashHelpers(result *strings.Builder, state *stashHelpers) {
	if state == nil {
		return
	}
	for _, element := range state.elements {
		spelling := typeSpelling(element)
		fmt.Fprintf(result, "\nstatic inline hex_stash *%s(void) {\n", stashNewHelper(element))
		fmt.Fprintf(result, "    return hex_stash_new(sizeof(%s), _Alignof(%s));\n}\n", spelling, spelling)
		fmt.Fprintf(result, "\nstatic inline %s *%s(hex_stash *stash, %s initial) {\n", spelling, stashAllocateHelper(element), spelling)
		fmt.Fprintf(result, "    %s *slot = (%s *)hex_stash_allocate(stash);\n", spelling, spelling)
		fmt.Fprintf(result, "    *slot = initial;\n")
		fmt.Fprintf(result, "    return slot;\n}\n")
	}
}

func stashNewHelper(element compilerTypes.Type) string {
	return "hex_stash_new_" + element.CName
}

func stashAllocateHelper(element compilerTypes.Type) string {
	return "hex_stash_alloc_" + element.CName
}

func renderStashConstructor(node checker.Expression, state *expressionValidation) (string, error) {
	if node.OperandType.Stash == nil || node.Element == (compilerTypes.Type{}) {
		return "", unknownExpressionDiagnostic("stash constructor has invalid checked metadata")
	}
	return stashNewHelper(node.Element) + "()", nil
}

func renderStashMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Stash == nil {
		return "", unknownExpressionDiagnostic("stash method has invalid checked metadata")
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "allocate":
		if len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("stash allocate has invalid checked metadata")
		}
		initial, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return stashAllocateHelper(node.Element) + "(" + receiver + ", " + initial + ")", nil
	case "reset":
		return "hex_stash_reset(" + receiver + ")", nil
	case "destroy":
		return "hex_stash_destroy(" + receiver + ")", nil
	default:
		return "", unknownExpressionDiagnostic("unknown stash method " + node.Name)
	}
}

// validateStashExpression is the fail-closed structural check for every
// Stash checked expression kind, reached from validateExpressionNode.
func validateStashExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	switch node.Kind {
	case checker.StashConstructorExpression:
		if node.OperandType.Stash == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.Element, node.OperandType.Stash.Element) || !compilerTypes.Equal(node.ResultType, node.OperandType) {
			return unknownExpressionDiagnostic("stash constructor has invalid checked metadata")
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("stash constructor result type does not match its expected type")
		}
		return nil
	case checker.StashMethodCallExpression:
		if node.Operand == nil || node.OperandType.Stash == nil || !compilerTypes.Equal(node.Element, node.OperandType.Stash.Element) {
			return unknownExpressionDiagnostic("stash method has invalid checked metadata")
		}
		switch node.Name {
		case "allocate":
			if len(node.Arguments) != 1 || node.ResultType.Element == nil || !node.ResultType.PointeeWritable || !compilerTypes.Equal(*node.ResultType.Element, node.Element) {
				return unknownExpressionDiagnostic("stash allocate has invalid checked metadata")
			}
		case "reset", "destroy":
			if len(node.Arguments) != 0 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("stash " + node.Name + " has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown stash method " + node.Name)
		}
		if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
			return unknownExpressionDiagnostic("stash method result type does not match its expected type")
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
	return unknownExpressionDiagnostic("unknown stash expression kind")
}
