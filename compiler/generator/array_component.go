package generator

import (
	compilerTypes "hexal/compiler/types"
)

// arrayComponentModel is the typed render model for packages/array.h: one
// pre-sorted record per reachable Array specialization.
type arrayComponentModel struct {
	Arrays []arrayComponentRecord
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
}

// arrayComponentRecordFor builds the spelling record of one Array
// specialization.
func arrayComponentRecordFor(array compilerTypes.Type, viewState *generatedViewState) arrayComponentRecord {
	element := array.Array.Element
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
		records = append(records, arrayComponentRecordFor(array, merged.viewState))
	}
	if len(records) == 0 {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/array.h",
		template: "array.h",
		model:    arrayComponentModel{Arrays: records},
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
