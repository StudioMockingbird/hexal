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
	order      []compilerTypes.Type
	seen       map[*compilerTypes.ListInfo]bool
	seenInline map[*compilerTypes.InlineListInfo]bool
	// componentOwned marks a specialization whose body belongs in the shared
	// hexal/list.h artifact even though its element type routes to module
	// headers: a core component unit spells the specialization, and the
	// element's own core header (not any module header) defines its C type,
	// so the shared artifact can see both.
	componentOwned map[*compilerTypes.ListInfo]bool
}

// newListState returns an empty list discovery state.
func newListState() *generatedListState {
	return &generatedListState{
		seen:           make(map[*compilerTypes.ListInfo]bool),
		seenInline:     make(map[*compilerTypes.InlineListInfo]bool),
		componentOwned: make(map[*compilerTypes.ListInfo]bool),
	}
}

// discoverGeneratedLists walks every type reachable from the program and
// collects the distinct list types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedLists(program checker.Program) *generatedListState {
	state := newListState()
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.List != nil {
				if !state.seen[typ.List] {
					state.seen[typ.List] = true
					state.order = append(state.order, typ)
				}
			}
			if typ.InlineList != nil {
				if !state.seenInline[typ.InlineList] {
					state.seenInline[typ.InlineList] = true
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
	if list.InlineList != nil {
		return strings.TrimPrefix(list.CName, "hex_list_inline_")
	}
	return strings.TrimPrefix(list.CName, "hex_list_")
}
