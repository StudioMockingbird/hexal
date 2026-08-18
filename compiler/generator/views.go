package generator

import (
	"sort"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedViewState records the view types that need definitions, in
// deterministic order, plus whether the view component must exist even
// without view records because another component declares it as a dependency
// .
type generatedViewState struct {
	views    []compilerTypes.Type
	seen     map[*compilerTypes.ViewInfo]bool
	required bool
}

// discoverGeneratedViews walks every type reachable from the program and
// collects the distinct view types. Discovery order is then sorted by C name
// so the generated header is deterministic.
func discoverGeneratedViews(program checker.Program) *generatedViewState {
	state := &generatedViewState{seen: make(map[*compilerTypes.ViewInfo]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) {
			if typ.View != nil {
				if !state.seen[typ.View] {
					state.seen[typ.View] = true
					state.views = append(state.views, typ)
				}
			}
		},
	}
	walkProgram(program, visitor)

	sort.SliceStable(state.views, func(left, right int) bool {
		return state.views[left].CName < state.views[right].CName
	})
	return state
}

// ensureViewUInt8 adds the byte view type to the view state if missing; the
// String helpers always reference it.
func ensureViewUInt8(state *generatedViewState) {
	if state == nil {
		return
	}
	for _, view := range state.views {
		if view.CName == "hex_view_UInt8" {
			return
		}
	}
	view := compilerTypes.NewEnvironment().ViewType(compilerTypes.UInt8)
	state.seen[view.View] = true
	state.views = append(state.views, view)
}

// viewCName returns the C struct name of the view type over one element.
func viewCName(element compilerTypes.Type) string {
	return "hex_view_" + compilerTypes.SanitizeIdentifier(element.Name)
}
