package generator

import (
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
func discoverGeneratedArrays(program checker.Program) *generatedArrayState {
	state := &generatedArrayState{seen: make(map[*compilerTypes.ArrayInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) {
			if typ.Array != nil {
				if !state.seen[typ.Array] {
					state.seen[typ.Array] = true
					state.order = append(state.order, typ)
				}
			}
		},
	}
	walkProgram(program, visitor)

	sort.SliceStable(state.order, func(left, right int) bool {
		return state.order[left].CName < state.order[right].CName
	})
	return state
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
