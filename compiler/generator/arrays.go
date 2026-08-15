package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedArrayState records the array types that need struct and element
// accessor definitions, in deterministic order.
type generatedArrayState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.ArrayInfo]bool
}

// discoverGeneratedArrays walks every type reachable from the program and
// collects the distinct array types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedArrays(program checker.Program) (*generatedArrayState, error) {
	state := &generatedArrayState{seen: make(map[*compilerTypes.ArrayInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.Array != nil {
				if !state.seen[typ.Array] {
					state.seen[typ.Array] = true
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
func writeArrayDefinitions(result *strings.Builder, arrays *generatedArrayState, views *generatedViewState) {
	if arrays == nil {
		return
	}
	// An array's element may itself be an array (Array<Array<Int32, 3>, 2>),
	// and the nested struct is embedded by value, so the inner definition
	// must precede the outer one. The discovery order is sorted by C name,
	// which does not encode nesting; emit dependency-first instead.
	order := arrayDependencyOrder(arrays.order)
	for _, array := range order {
		element := array.Array.Element
		length := array.Array.Length
		fmt.Fprintf(result, "\ntypedef struct %s {\n    %s data[%d];\n} %s;\n", array.CName, pointerSpelling(element), length, array.CName)
		fmt.Fprintf(result, "static inline const %s *hex_array_at_%s(const %s *array, size_t index) {\n", pointerSpelling(element), arrayAccessorSuffix(array), array.CName)
		writeArrayBoundsGuard(result, length)
		result.WriteString("    return &array->data[index];\n}\n")
		fmt.Fprintf(result, "static inline %s *hex_array_at_mut_%s(%s *array, size_t index) {\n", pointerSpelling(element), arrayAccessorSuffix(array), array.CName)
		writeArrayBoundsGuard(result, length)
		result.WriteString("    return &array->data[index];\n}\n")
		if view := matchingView(views, element); view != (compilerTypes.Type{}) {
			writeArraySliceHelper(result, array, view)
		}
	}
}

// arrayDependencyOrder orders array types so every element-array appears
// before the array embedding it, preserving the discovery order otherwise.
func arrayDependencyOrder(order []compilerTypes.Type) []compilerTypes.Type {
	byName := make(map[string]compilerTypes.Type, len(order))
	for _, array := range order {
		byName[array.CName] = array
	}
	visited := make(map[string]bool)
	result := make([]compilerTypes.Type, 0, len(order))
	var visit func(array compilerTypes.Type)
	visit = func(array compilerTypes.Type) {
		if visited[array.CName] {
			return
		}
		visited[array.CName] = true
		if element := array.Array.Element; element.Array != nil {
			if inner, ok := byName[element.CName]; ok {
				visit(inner)
			}
		}
		result = append(result, array)
	}
	for _, array := range order {
		visit(array)
	}
	return result
}

// matchingView returns the discovered view type over one element, or the zero
// Type when no such view is used.
func matchingView(views *generatedViewState, element compilerTypes.Type) compilerTypes.Type {
	if views == nil {
		return compilerTypes.Type{}
	}
	for _, view := range views.views {
		if compilerTypes.Equal(view.View.Element, element) {
			return view
		}
	}
	return compilerTypes.Type{}
}

func writeArrayBoundsGuard(result *strings.Builder, length uint64) {
	fmt.Fprintf(result, "    if (index >= UINT64_C(%d)) {\n", length)
	result.WriteString("        fputs(\"[Runtime Error] array index out of bounds\\n\", stderr);\n        abort();\n    }\n")
}

func arrayAccessorSuffix(array compilerTypes.Type) string {
	return strings.TrimPrefix(array.CName, "hex_array_")
}

// arrayAccessorCName selects the read or write accessor for one array type;
// writable selects the mutable variant.
func arrayAccessorCName(array compilerTypes.Type, writable bool) string {
	name := "hex_array_at_" + arrayAccessorSuffix(array)
	if writable {
		name = "hex_array_at_mut_" + arrayAccessorSuffix(array)
	}
	return name
}
