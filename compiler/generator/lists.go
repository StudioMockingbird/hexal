package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedListState records the list types that need header and helper
// definitions, in deterministic order.
type generatedListState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.ListInfo]bool
}

// discoverGeneratedLists walks every type reachable from the program and
// collects the distinct list types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedLists(program checker.Program) (*generatedListState, error) {
	state := &generatedListState{seen: make(map[*compilerTypes.ListInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.List != nil {
				if !state.seen[typ.List] {
					state.seen[typ.List] = true
					state.order = append(state.order, typ)
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}

	sort.SliceStable(state.order, func(left, right int) bool {
		return state.order[left].CName < state.order[right].CName
	})
	return state, nil
}
func listSuffix(list compilerTypes.Type) string {
	return strings.TrimPrefix(list.CName, "hex_list_")
}

// writeListDefinitions emits one header struct plus the element helpers per
// list type. RFC 0046: every element, String included, is stored and discarded
// by shallow C copy; free and clear release only the container's own region.
func writeListDefinitions(result *strings.Builder, lists *generatedListState, views *generatedViewState) {
	if lists == nil {
		return
	}
	for _, list := range lists.order {
		element := list.List.Element
		elementSpelling := typeSpelling(element)
		suffix := listSuffix(list)
		fmt.Fprintf(result, "\ntypedef struct %s {\n    %s *data;\n    size_t length;\n    size_t capacity;\n    uintptr_t allocator;\n} %s;\n", list.CName, elementSpelling, list.CName)
		writeListGrowHelper(result, list, elementSpelling)
		fmt.Fprintf(result, "static inline %s *hex_list_new_%s(hex_heap h) {\n", list.CName, suffix)
		result.WriteString("    " + list.CName + " *header = hex_heap_raw_allocate(h.identity, sizeof(" + list.CName + "), _Alignof(" + list.CName + "));\n")
		result.WriteString("    header->data = NULL;\n    header->length = 0;\n    header->capacity = 0;\n    header->allocator = h.identity;\n")
		fmt.Fprintf(result, "    return header;\n}\n")
		fmt.Fprintf(result, "static inline void hex_list_push_%s(%s *list, %s value) {\n", suffix, list.CName, elementSpelling)
		fmt.Fprintf(result, "    if (list->length == list->capacity) {\n        hex_list_grow_%s(list);\n    }\n", suffix)
		result.WriteString("    list->data[list->length++] = value;\n")
		result.WriteString("}\n")
		fmt.Fprintf(result, "static inline void hex_list_set_%s(%s *list, size_t index, %s value) {\n", suffix, list.CName, elementSpelling)
		writeListBoundsGuard(result, list)
		result.WriteString("    list->data[index] = value;\n")
		result.WriteString("}\n")
		fmt.Fprintf(result, "static inline %s hex_list_pop_%s(%s *list) {\n", elementSpelling, suffix, list.CName)
		fmt.Fprintf(result, "    if (list->length == 0) {\n        fputs(\"[Runtime Error] list index out of bounds\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    " + elementSpelling + " value = list->data[list->length - 1];\n")
		result.WriteString("    list->length--;\n    return value;\n}\n")
		fmt.Fprintf(result, "static inline void hex_list_clear_%s(%s *list) {\n", suffix, list.CName)
		result.WriteString("    list->length = 0;\n}\n")
		// The at-read returns a pointer to the slot; for pointer elements the
		// element spelling already carries its pointee const, so no extra
		// leading const is added.
		atReadReturn := "const " + elementSpelling + " *"
		if strings.Contains(elementSpelling, "*") {
			atReadReturn = elementSpelling + " *"
		}
		fmt.Fprintf(result, "static inline %s hex_list_at_%s(const %s *list, size_t index) {\n", atReadReturn, suffix, list.CName)
		writeListBoundsGuard(result, list)
		result.WriteString("    return &list->data[index];\n}\n")
		fmt.Fprintf(result, "static inline %s *hex_list_at_mut_%s(%s *list, size_t index) {\n", elementSpelling, suffix, list.CName)
		writeListBoundsGuard(result, list)
		result.WriteString("    return &list->data[index];\n}\n")
		fmt.Fprintf(result, "static inline void hex_list_free_%s(hex_heap h, %s *list) {\n", suffix, list.CName)
		result.WriteString("    if (list == NULL || list->allocator != h.identity) {\n")
		result.WriteString("        fputs(\"[Runtime Error] deallocation used the wrong allocator\\n\", stderr);\n        abort();\n    }\n")
		result.WriteString("    free(list->data);\n")
		result.WriteString("    free(list);\n}\n")
		if view := matchingView(views, element); view != (compilerTypes.Type{}) {
			fmt.Fprintf(result, "static inline %s hex_list_slice_%s(const %s *list, uint64_t start, uint64_t end) {\n", view.CName, suffix, list.CName)
			fmt.Fprintf(result, "    if (!(start <= end && end <= list->length)) {\n        fputs(\"[Runtime Error] list slice bounds out of range\\n\", stderr);\n        abort();\n    }\n")
			fmt.Fprintf(result, "    return (%s){&list->data[start], end - start};\n}\n", view.CName)
		}
	}
}

func writeListBoundsGuard(result *strings.Builder, list compilerTypes.Type) {
	fmt.Fprintf(result, "    if (index >= list->length) {\n        fputs(\"[Runtime Error] list index out of bounds\\n\", stderr);\n        abort();\n    }\n")
}

// writeListGrowHelper emits the growth helper for one list type: capacity
// doubling with overflow checks, a fresh region through the retained
// allocator, pointer-slot relocation, and release of the old region.
func writeListGrowHelper(result *strings.Builder, list compilerTypes.Type, elementSpelling string) {
	suffix := listSuffix(list)
	fmt.Fprintf(result, "static inline void hex_list_grow_%s(%s *list) {\n", suffix, list.CName)
	result.WriteString("    uint64_t next = list->capacity == 0 ? 1 : list->capacity * 2;\n")
	result.WriteString("    if (next < list->capacity || next > SIZE_MAX / sizeof(" + elementSpelling + ")) {\n")
	result.WriteString("        fputs(\"[Runtime Error] list capacity is not representable\\n\", stderr);\n        abort();\n    }\n")
	result.WriteString("    " + elementSpelling + " *region = hex_heap_raw_allocate(list->allocator, next * sizeof(" + elementSpelling + "), _Alignof(" + elementSpelling + "));\n")
	result.WriteString("    for (size_t index = 0; index < list->length; index++) {\n")
	result.WriteString("        region[index] = list->data[index];\n")
	result.WriteString("    }\n")
	result.WriteString("    free(list->data);\n")
	result.WriteString("    list->data = region;\n")
	result.WriteString("    list->capacity = next;\n}\n")
}
