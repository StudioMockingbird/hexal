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

func discoverHeapHelpers(program checker.Program) (*heapHelpers, error) {
	state := &heapHelpers{seen: make(map[string]bool)}
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.HeapAllocateExpression {
				if node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) {
					return unknownExpressionDiagnostic("heap allocation without a complete checked element type")
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
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	return state, nil
}
func writeHeapDefinitions(result *strings.Builder, state *heapHelpers) {
	if state == nil || len(state.elements) == 0 && !state.required {
		return
	}
	result.WriteString("\n")
	result.WriteString("typedef struct hex_heap {\n    uintptr_t identity;\n} hex_heap;\n\n")
	result.WriteString("#define HEX_HEAP_DEFAULT 0\n\n")
	// The header's last size_t slot is the offset marker every free reads
	// at (pointer - sizeof(size_t)). The live flag must not share that
	// region: the marker write at base + offset - 8 would clobber it.
	result.WriteString("typedef struct hex_heap_header {\n    uintptr_t allocator;\n    size_t size;\n    size_t offset;\n    bool live;\n    size_t marker;\n} hex_heap_header;\n\n")
	// Alignment rounding and the payload offset must not wrap; align == 0
	// is rejected first so align - 1 is never evaluated for it. Both
	// additions use ckd_add from <stdckdint.h> (RFC 0069).
	result.WriteString("static void *hex_heap_raw_allocate(uintptr_t allocator, size_t size, size_t align) {\n")
	result.WriteString("    size_t padded;\n")
	result.WriteString("    if (align == 0 || ckd_add(&padded, sizeof(hex_heap_header), align - 1)) {\n")
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] allocation size is not representable\\n\");\n    }\n")
	result.WriteString("    size_t offset = padded & ~(align - 1);\n")
	result.WriteString("    size_t total;\n")
	result.WriteString("    if (ckd_add(&total, offset, size)) {\n")
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] allocation size is not representable\\n\");\n    }\n")
	result.WriteString("    unsigned char *base = (unsigned char *)malloc(total);\n")
	result.WriteString("    if (base == nullptr) {\n")
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] heap allocation failed\\n\");\n    }\n")
	result.WriteString("    hex_heap_header *header = (hex_heap_header *)base;\n")
	result.WriteString("    header->allocator = allocator;\n")
	result.WriteString("    header->size = size;\n")
	result.WriteString("    header->offset = offset;\n")
	result.WriteString("    header->live = true;\n")
	result.WriteString("    *((size_t *)(base + offset - sizeof(size_t))) = offset;\n")
	result.WriteString("    return base + offset;\n")
	result.WriteString("}\n\n")
	result.WriteString("static void hex_heap_free(void *pointer, uintptr_t allocator) {\n")
	result.WriteString("    if (pointer == nullptr) {\n")
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] double deallocation\\n\");\n    }\n")
	result.WriteString("    size_t offset = *((size_t *)((unsigned char *)pointer - sizeof(size_t)));\n")
	result.WriteString("    hex_heap_header *header = (hex_heap_header *)((unsigned char *)pointer - offset);\n")
	result.WriteString("    if (header->allocator != allocator) {\n")
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] deallocation used the wrong allocator\\n\");\n    }\n")
	result.WriteString("    if (!header->live) {\n")
	result.WriteString("        hex_runtime_trap(\"[Runtime Error] double deallocation\\n\");\n    }\n")
	result.WriteString("    header->live = false;\n")
	result.WriteString("    free(header);\n")
	result.WriteString("}\n")
}

// writeHeapAllocateHelpers emits the typed allocation helpers into the module
// header. They are per-module because the element types are module-owned
// (objects, ADTs, unions) and must be defined before the helper; the shared
// raw machinery lives in hexal.h (RFC 0062).
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
