package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// heapHelpers records the element types allocated through Heap so the
// generator can emit exactly one checked allocate helper per type.
type heapHelpers struct {
	elements []compilerTypes.Type
	seen     map[string]bool
	required bool // base helpers needed even without typed allocations (Strings)
}

func discoverHeapHelpers(program checker.Program) *heapHelpers {
	state := &heapHelpers{seen: make(map[string]bool)}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) {
			if node.Kind == checker.HeapAllocateExpression {
				if node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) {
					panic(unknownExpressionDiagnostic("heap allocation without a complete checked element type"))
				}
				if !state.seen[node.Element.Name] {
					state.seen[node.Element.Name] = true
					state.elements = append(state.elements, node.Element)
				}
			}
			// Heap.new() and Heap-typed parameters and bindings name the
			// hex_heap type in generated signatures and initializers, so
			// the base machinery is required even without any allocation.
			if compilerTypes.Equal(node.ResultType, compilerTypes.Heap) || compilerTypes.Equal(node.OperandType, compilerTypes.Heap) {
				state.required = true
			}
		},
	}
	walkProgram(program, visitor)
	return state
}

// writeHeapAllocateHelpers emits the typed allocation helpers into the module
// header. They are per-module because the element types are module-owned
// (objects, ADTs, unions) and must be defined before the helper; the shared
// raw machinery lives in hexal/heap.h.
func writeHeapAllocateHelpers(result *strings.Builder, state *heapHelpers) {
	if state == nil {
		return
	}
	for _, element := range state.elements {
		helper := "hex_heap_allocate_" + compilerTypes.SanitizeIdentifier(element.Name)
		fmt.Fprintf(result, "\nstatic %s %s(hex_heap h, %s initial) {\n", typeSpelling(compilerTypes.MutPtrType(element)), helper, typeSpelling(element))
		fmt.Fprintf(result, "    %s *pointer = hex_heap_raw_allocate(h.identity, sizeof(%s), _Alignof(%s));\n", typeSpelling(element), typeSpelling(element), typeSpelling(element))
		fmt.Fprintf(result, "    *pointer = initial;\n")
		fmt.Fprintf(result, "    return pointer;\n}\n")
	}
}

func heapAllocateHelper(element compilerTypes.Type) string {
	return "hex_heap_allocate_" + compilerTypes.SanitizeIdentifier(element.Name)
}

func renderHeapAllocate(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 1 || node.Element == (compilerTypes.Type{}) {
		return "", unknownExpressionDiagnostic("heap allocation has invalid checked metadata")
	}
	receiver, err := renderExpressionExpectedWithState(*node.Operand, compilerTypes.Heap, true, state)
	if err != nil {
		return "", err
	}
	initial, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return heapAllocateHelper(node.Element) + "(" + receiver + ", " + initial + ")", nil
}

func renderHeapFree(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 1 {
		return "", unknownExpressionDiagnostic("heap free has invalid checked metadata")
	}
	receiver, err := renderExpressionExpectedWithState(*node.Operand, compilerTypes.Heap, true, state)
	if err != nil {
		return "", err
	}
	value, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return "hex_heap_free(" + value + ", " + receiver + ".identity)", nil
}
