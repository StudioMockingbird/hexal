package generator

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// listComponentModel is the typed render model for packages/list.h: one
// pre-sorted record per reachable List specialization.
type listComponentModel struct {
	Lists []listComponentRecord
	// NeedsSlice is true when some specialization has a slice helper, which
	// is the only content naming the slice component.
	NeedsSlice bool
	// NeedsHeapString is true when some specialization's element is String,
	// whose spelling (a pointer to hex_string) is defined by
	// hexal/string.h -- a direct dependency of this file, not something a
	// consumer's own include order can be relied on to supply first.
	NeedsHeapString bool
}

// listComponentRecord is one reachable List specialization's spelling facts:
// the struct C name, the accessor suffix, the spelled element type, the at
// read return spelling (the element spelling plus a leading const unless the
// element is already pointer-like), and the matching Slice C name when a slice
// over the element is reachable. The template lays out the struct and the
// typed inline operations from these fields; canonical naming, ordering, and
// C spelling stay Go decisions.
type listComponentRecord struct {
	CName           string
	Suffix          string
	ElementSpelling string
	AtReadReturn    string
	// SliceCName and MutSliceCName name the read-only and writable slice
	// descriptors over the element when reachable; either may be empty.
	SliceCName    string
	MutSliceCName string
	// NeedsHeapString is true when this specialization's element is String.
	NeedsHeapString bool
}

// listComponentRecordFor builds the spelling record of one List
// specialization. The matching slice of a builtin element is a component slice;
// the matching slice of a module element is a module-header slice, and the
// record is only built for the artifact that owns the list.
func listComponentRecordFor(list compilerTypes.Type, sliceState *generatedSliceState) listComponentRecord {
	element := list.List.Element
	elementSpelling := typeSpelling(element)
	atReadReturn := "const " + elementSpelling + " *"
	if strings.Contains(elementSpelling, "*") {
		atReadReturn = elementSpelling + " *"
	}
	sliceCName := ""
	if slice := matchingSlice(sliceState, element, false); slice != (compilerTypes.Type{}) {
		sliceCName = slice.CName
	}
	mutSliceCName := ""
	if slice := matchingSlice(sliceState, element, true); slice != (compilerTypes.Type{}) {
		mutSliceCName = slice.CName
	}
	return listComponentRecord{
		CName:           list.CName,
		Suffix:          listSuffix(list),
		ElementSpelling: elementSpelling,
		AtReadReturn:    atReadReturn,
		SliceCName:      sliceCName,
		MutSliceCName:   mutSliceCName,
		NeedsHeapString: compilerTypes.IsString(element),
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
		records = append(records, listComponentRecordFor(list, merged.sliceState))
	}
	if len(records) == 0 {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/list.h",
		template: "list.h",
		model:    listComponentModel{Lists: records, NeedsSlice: listRecordsNeedSlice(records), NeedsHeapString: listRecordsNeedHeapString(records)},
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

// listRecordsNeedSlice reports whether any list record renders a slice
// helper, which is the only content in packages/list.h that names the slice
// component.
func listRecordsNeedSlice(records []listComponentRecord) bool {
	for _, record := range records {
		if record.SliceCName != "" || record.MutSliceCName != "" {
			return true
		}
	}
	return false
}

// listRecordsNeedHeapString reports whether any list record's element is
// String, the only content in packages/list.h that names hex_string.
func listRecordsNeedHeapString(records []listComponentRecord) bool {
	for _, record := range records {
		if record.NeedsHeapString {
			return true
		}
	}
	return false
}
