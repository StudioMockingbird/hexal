package generator

import (
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
