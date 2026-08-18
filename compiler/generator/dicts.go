package generator

import (
	"slices"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedDictState records the dictionary types that need header and
// helper definitions, in deterministic order.
type generatedDictState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.DictInfo]bool
}

// discoverGeneratedDicts walks every type reachable from the program and
// collects the distinct dictionary types. Discovery order is then sorted by
// C name so the generated header is deterministic.
func discoverGeneratedDicts(program checker.Program) *generatedDictState {
	state := &generatedDictState{seen: make(map[*compilerTypes.DictInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) {
			if typ.Dict != nil {
				if !state.seen[typ.Dict] {
					state.seen[typ.Dict] = true
					state.order = append(state.order, typ)
				}
			}
		},
	}
	walkProgram(program, visitor)

	slices.SortStableFunc(state.order, func(left, right compilerTypes.Type) int {
		return strings.Compare(left.CName, right.CName)
	})
	return state
}
func dictSuffix(dict compilerTypes.Type) string {
	return strings.TrimPrefix(dict.CName, "hex_dict_")
}
