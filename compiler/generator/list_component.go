package generator

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// listComponentModel is the typed render model for packages/list.h: one
// pre-sorted record per reachable List specialization.
type listComponentModel struct {
	Lists []listComponentRecord
	// NeedsView is true when some specialization has a slice helper, which
	// is the only content naming the view component.
	NeedsView bool
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

// listComponentRecordFor builds the spelling record of one List
// specialization. The matching view of a builtin element is a component view;
// the matching view of a module element is a module-header view, and the
// record is only built for the artifact that owns the list.
func listComponentRecordFor(list compilerTypes.Type, viewState *generatedViewState) listComponentRecord {
	element := list.List.Element
	elementSpelling := typeSpelling(element)
	atReadReturn := "const " + elementSpelling + " *"
	if strings.Contains(elementSpelling, "*") {
		atReadReturn = elementSpelling + " *"
	}
	viewCName := ""
	if view := matchingView(viewState, element); view != (compilerTypes.Type{}) {
		viewCName = view.CName
	}
	return listComponentRecord{
		CName:           list.CName,
		Suffix:          listSuffix(list),
		ElementSpelling: elementSpelling,
		AtReadReturn:    atReadReturn,
		ViewCName:       viewCName,
	}
}

// listComponents returns the generated hexal/list.h artifact when builtin-
// element List specializations are reachable; module-owned element
// specializations emit into the consuming module headers instead.
func listComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.listState == nil || len(merged.listState.order) == 0 {
		return nil, nil
	}
	records := make([]listComponentRecord, 0, len(merged.listState.order))
	for _, list := range merged.listState.order {
		if collectionElementModuleTyped(list) {
			continue
		}
		records = append(records, listComponentRecordFor(list, merged.viewState))
	}
	if len(records) == 0 {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/list.h",
		template: "list.h",
		model:    listComponentModel{Lists: records, NeedsView: listRecordsNeedView(records)},
	}}, nil
}

// moduleListComponent selects hexal/list.h for a module with reachable
// builtin-element List specializations; a module whose only lists are
// module-owned re-emits them in its own header and includes nothing.
func moduleListComponent(emission *moduleEmission) []string {
	if emission == nil || emission.listState == nil || len(emission.listState.order) == 0 {
		return nil
	}
	for _, list := range emission.listState.order {
		if !collectionElementModuleTyped(list) {
			return []string{"hexal/list.h"}
		}
	}
	return nil
}

// listRecordsNeedView reports whether any list record renders a slice
// helper, which is the only content in packages/list.h that names the view
// component.
func listRecordsNeedView(records []listComponentRecord) bool {
	for _, record := range records {
		if record.ViewCName != "" {
			return true
		}
	}
	return false
}
