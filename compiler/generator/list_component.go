package generator

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// listComponentModel is the typed render model for packages/list.h: one
// pre-sorted record per reachable List specialization.
type listComponentModel struct {
	Lists []listComponentRecord
}

// listComponentRecord is one reachable List specialization's spelling facts:
// the struct C name, the accessor suffix, the spelled element type, the at
// read return spelling (the element spelling plus a leading const unless the
// element is already pointer-like), and the matching View C name when a view
// over the element is reachable. The template lays out the struct and the
// typed inline operations from these fields; canonical naming, ordering, and
// C spelling stay Go decisions.
type listComponentRecord struct {
	CName           string
	Suffix          string
	ElementSpelling string
	AtReadReturn    string
	ViewCName       string
}

// listComponents returns the generated hexal/list.h artifact when List
// specializations are reachable.
func listComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.listState == nil || len(merged.listState.order) == 0 {
		return nil, nil
	}
	records := make([]listComponentRecord, 0, len(merged.listState.order))
	for _, list := range merged.listState.order {
		element := list.List.Element
		elementSpelling := typeSpelling(element)
		atReadReturn := "const " + elementSpelling + " *"
		if strings.Contains(elementSpelling, "*") {
			atReadReturn = elementSpelling + " *"
		}
		viewCName := ""
		if view := matchingView(merged.viewState, element); view != (compilerTypes.Type{}) {
			viewCName = view.CName
		}
		records = append(records, listComponentRecord{
			CName:           list.CName,
			Suffix:          listSuffix(list),
			ElementSpelling: elementSpelling,
			AtReadReturn:    atReadReturn,
			ViewCName:       viewCName,
		})
	}
	return []componentArtifact{{
		key:      "hexal/list.h",
		template: "list.h",
		model:    listComponentModel{Lists: records},
	}}, nil
}

// listFamilyContent is the transition seam; the list definitions have moved
// to list.h, so hexal.h carries nothing.
func listFamilyContent(state *generatedListState, views *generatedViewState) string {
	return ""
}

// moduleListComponent selects hexal/list.h for a module with reachable List
// specializations.
func moduleListComponent(emission *moduleEmission) []string {
	if emission == nil || emission.listState == nil || len(emission.listState.order) == 0 {
		return nil
	}
	return []string{"hexal/list.h"}
}
