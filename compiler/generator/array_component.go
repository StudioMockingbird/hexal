package generator

import (
	compilerTypes "hexal/compiler/types"
)

// arrayComponentModel is the typed render model for packages/array.h: one
// pre-sorted record per reachable Array specialization.
type arrayComponentModel struct {
	Arrays []arrayComponentRecord
	// NeedsView is true when some specialization has a slice helper, which
	// is the only content naming the view component. The include is guarded
	// on it so a declared dependency is always a used one.
	NeedsView bool
	// NeedsHeapString is true when some specialization's element is String,
	// whose spelling (hex_string) is defined by hexal/string.h -- a direct
	// dependency of this file, not something a consumer's own include order
	// can be relied on to supply first.
	NeedsHeapString bool
}

// arrayComponentRecord is one reachable Array specialization's spelling
// facts: the struct C name, the accessor suffix, the spelled element type,
// the compile-time length, and the matching View C name when a view over the
// element is reachable. The template lays out the struct, the UINT64_C
// bounds guards, and the slice helper from these fields; canonical naming,
// ordering, and C spelling stay Go decisions.
type arrayComponentRecord struct {
	CName           string
	Suffix          string
	ElementSpelling string
	Length          uint64
	ViewCName       string
	// NeedsAt and NeedsAtMut form the demand filter: an accessor is
	// emitted only where some access whose bounds check survived reaches it.
	// A program that only iterates arrays and indexes them with literals
	// reaches neither and gets the typedef alone.
	NeedsAt    bool
	NeedsAtMut bool
	// NeedsHeapString is true when this specialization's element is String.
	NeedsHeapString bool
}

// arrayComponentRecordFor builds the spelling record of one Array
// specialization.
func arrayComponentRecordFor(array compilerTypes.Type, viewState *generatedViewState, arrayState *generatedArrayState) arrayComponentRecord {
	element := array.Array.Element
	demand := arrayState.accessorDemandFor(array)
	viewCName := ""
	if view := matchingView(viewState, element); view != (compilerTypes.Type{}) {
		viewCName = view.CName
	}
	return arrayComponentRecord{
		CName:           array.CName,
		Suffix:          arrayAccessorSuffix(array),
		ElementSpelling: pointerSpelling(element),
		Length:          array.Array.Length,
		ViewCName:       viewCName,
		NeedsAt:         demand.read,
		NeedsAtMut:      demand.write,
		NeedsHeapString: compilerTypes.IsString(element),
	}
}

// arrayComponents returns the generated hexal/array.h artifact when builtin-
// element Array specializations are reachable; module-owned element
// specializations emit into the consuming module headers instead. Records
// follow the array dependency order so every element-array precedes the
// array embedding it.
func arrayComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.arrayState == nil || len(merged.arrayState.order) == 0 {
		return nil, nil
	}
	records := make([]arrayComponentRecord, 0, len(merged.arrayState.order))
	for _, array := range arrayDependencyOrder(merged.arrayState.order) {
		if collectionElementModuleTyped(array) {
			continue
		}
		records = append(records, arrayComponentRecordFor(array, merged.viewState, merged.arrayState))
	}
	if len(records) == 0 {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/array.h",
		template: "array.h",
		model:    arrayComponentModel{Arrays: records, NeedsView: recordsNeedView(records), NeedsHeapString: arrayRecordsNeedHeapString(records)},
	}}, nil
}

// moduleArrayComponent selects hexal/array.h for a module with reachable
// builtin-element Array specializations; a module whose only arrays are
// module-owned re-emits them in its own header and includes nothing.
func moduleArrayComponent(emission *moduleEmission) []string {
	if emission == nil || emission.arrayState == nil || len(emission.arrayState.order) == 0 {
		return nil
	}
	for _, array := range emission.arrayState.order {
		if !collectionElementModuleTyped(array) {
			return []string{"hexal/array.h"}
		}
	}
	return nil
}

// recordsNeedView reports whether any array record renders a slice helper,
// which is the only content in packages/array.h that names the view
// component.
func recordsNeedView(records []arrayComponentRecord) bool {
	for _, record := range records {
		if record.ViewCName != "" {
			return true
		}
	}
	return false
}

// arrayRecordsNeedHeapString reports whether any array record's element is
// String, the only content in packages/array.h that names hex_string.
func arrayRecordsNeedHeapString(records []arrayComponentRecord) bool {
	for _, record := range records {
		if record.NeedsHeapString {
			return true
		}
	}
	return false
}
