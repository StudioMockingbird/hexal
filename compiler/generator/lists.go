package generator

import (
	"slices"
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
func discoverGeneratedLists(program checker.Program) *generatedListState {
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
	walkProgram(program, visitor)

	slices.SortStableFunc(state.order, func(left, right compilerTypes.Type) int {
		return strings.Compare(left.CName, right.CName)
	})
	return state
}
func listSuffix(list compilerTypes.Type) string {
	return strings.TrimPrefix(list.CName, "hex_list_")
}
