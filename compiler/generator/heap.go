package generator

import (
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
	// alignedElements records the element types allocated with an explicit
	// alignment. It is separate from elements because the aligned typed
	// helper and the shared aligned primitive are emitted only where a
	// reachable aligned allocation actually needs them.
	alignedElements []compilerTypes.Type
	alignedSeen     map[string]bool
}

// selected reports whether this compilation needs the Heap machinery at all.
func (state *heapHelpers) selected() bool {
	return state != nil && (state.required || len(state.elements) > 0 || len(state.alignedElements) > 0)
}

func discoverHeapHelpers(program checker.Program) (*heapHelpers, error) {
	state := &heapHelpers{seen: make(map[string]bool), alignedSeen: make(map[string]bool)}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.HeapAllocateExpression || node.Kind == checker.HeapAllocateAlignedExpression {
				if node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) {
					return unknownExpressionDiagnostic("heap allocation without a complete checked element type")
				}
				if node.Kind == checker.HeapAllocateAlignedExpression {
					if !state.alignedSeen[node.Element.Name] {
						state.alignedSeen[node.Element.Name] = true
						state.alignedElements = append(state.alignedElements, node.Element)
					}
				} else if !state.seen[node.Element.Name] {
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
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}

// heapAllocateModel carries one typed allocation helper's decided spellings:
// the return pointer type, the helper name, and the element spelling.
type heapAllocateModel struct {
	Return  string
	Helper  string
	Element string
}

// writeHeapAllocateHelpers emits the typed allocation helpers into the module
// header. They are per-module because the element types are module-owned
// (objects, ADTs, unions) and must be defined before the helper; the shared
// raw machinery lives in hexal/heap.h.
func writeHeapAllocateHelpers(result *strings.Builder, state *heapHelpers) error {
	if state == nil {
		return nil
	}
	for _, element := range state.elements {
		// The token is unused: one default allocator has nothing to select.
		// The parameter stays so the source Heap expression still evaluates
		// once, in its written position.
		if err := renderInto(result, "module.h", "heap_allocate_helper", heapAllocateModel{
			Return:  typeSpelling(compilerTypes.MutPtrType(element)),
			Helper:  heapAllocateHelper(element),
			Element: typeSpelling(element),
		}); err != nil {
			return err
		}
	}
	for _, element := range state.alignedElements {
		// alignof is spelled in generated C because the target C compiler and
		// ABI, not the host-neutral checker, own the element's natural
		// alignment.
		if err := renderInto(result, "module.h", "heap_allocate_aligned_helper", heapAllocateModel{
			Return:  typeSpelling(compilerTypes.MutPtrType(element)),
			Helper:  heapAllocateAlignedHelper(element),
			Element: typeSpelling(element),
		}); err != nil {
			return err
		}
	}
	return nil
}

func heapAllocateHelper(element compilerTypes.Type) string {
	return "hex_heap_allocate_" + compilerTypes.SanitizeIdentifier(element.Name)
}

func heapAllocateAlignedHelper(element compilerTypes.Type) string {
	return "hex_heap_allocate_aligned_" + compilerTypes.SanitizeIdentifier(element.Name)
}

func renderHeapAllocate(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 1 || node.Element == (compilerTypes.Type{}) {
		return "", unknownExpressionDiagnostic("heap allocation has invalid checked metadata")
	}
	receiver, err := renderExpressionExpectedWithState(*node.Operand, &compilerTypes.Heap, state)
	if err != nil {
		return "", err
	}
	initial, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	return heapAllocateHelper(node.Element) + "(" + receiver + ", " + initial + ")", nil
}

func renderHeapAllocateAligned(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 2 || node.Element == (compilerTypes.Type{}) {
		return "", unknownExpressionDiagnostic("aligned heap allocation has invalid checked metadata")
	}
	receiver, err := renderHoistedReceiver(node.Operand, compilerTypes.Heap, state)
	if err != nil {
		return "", err
	}
	initial, initialErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
	if initialErr != nil {
		return "", initialErr
	}
	alignment, alignmentErr := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
	if alignmentErr != nil {
		return "", alignmentErr
	}
	return heapAllocateAlignedHelper(node.Element) + "(" + receiver + ", " + initial + ", " + alignment + ")", nil
}

func renderHeapFree(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 1 {
		return "", unknownExpressionDiagnostic("heap free has invalid checked metadata")
	}
	receiver, err := renderExpressionExpectedWithState(*node.Operand, &compilerTypes.Heap, state)
	if err != nil {
		return "", err
	}
	value, err := renderOperandWithState(node.Arguments[0], state)
	if err != nil {
		return "", err
	}
	// The one stateless allocator makes the receiver operationally
	// irrelevant, but a Heap expression may still have effects, so it is
	// evaluated in its source position ahead of the pointer argument.
	return "((void)(" + receiver + "), hex_heap_free(" + value + "))", nil
}
