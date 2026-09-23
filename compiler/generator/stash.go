package generator

import (
	"strings"

	"hexal/compiler/checker"
	"hexal/compiler/specdata"
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
// stashHelperModel carries one stash helper's decided name and element
// spelling.
type stashHelperModel struct {
	Name     string
	Spelling string
}

func writeStashHelpers(result *strings.Builder, state *stashHelpers) error {
	if state == nil {
		return nil
	}
	for _, element := range state.elements {
		spelling := typeSpelling(element)
		if err := renderInto(result, "module.h", "stash_new_helper", stashHelperModel{
			Name:     stashNewHelper(element),
			Spelling: spelling,
		}); err != nil {
			return err
		}
		allocateName, allocateErr := stashAllocateHelper(element)
		if allocateErr != nil {
			return allocateErr
		}
		if err := renderInto(result, "module.h", "stash_allocate_helper", stashHelperModel{
			Name:     allocateName,
			Spelling: spelling,
		}); err != nil {
			return err
		}
	}
	return nil
}

// stashNewHelper names one Stash<T> constructor wrapper. The registry records
// no constructor runtime symbol, so the name stays literal here; it is a
// distinct symbol from the packages/stash.h-owned type-erased hex_stash_new.
func stashNewHelper(element compilerTypes.Type) string {
	return "hex_stash_new_" + element.CName
}

// stashAllocateHelper names one Stash<T>.allocate wrapper through the recorded
// Stash.allocate symbol, with the element's C name as the suffix.
func stashAllocateHelper(element compilerTypes.Type) (string, error) {
	return builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeStash), "allocate", element.CName)
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
		symbol, symbolErr := stashAllocateHelper(node.Element)
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ", " + initial + ")", nil
	case "reset":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeStash), "reset", "")
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
	case "destroy":
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeStash), "destroy", "")
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + receiver + ")", nil
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
