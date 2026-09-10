package generator

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// sliceComponentModel is the typed render model for packages/slice.h: one
// pre-sorted record per reachable Slice specialization.
type sliceComponentModel struct {
	Slices []sliceComponentRecord
	// NeedsHeapString is true when some specialization's element is String,
	// whose spelling is hex_string. This file forward-declares it rather
	// than including hexal/string.h: that file itself unconditionally needs
	// hex_slice_UInt8, so a full #include here would cycle back into this
	// file's own include guard whenever slice.h is entered first. Every
	// hex_string reference this file emits is through a pointer, so the
	// forward declaration is all it needs.
	NeedsHeapString bool
}

// sliceComponentRecord is one reachable Slice specialization's spelling facts:
// the struct C name, the helper-name prefix and accessor suffix, the spelled
// element type, and the access mode. The template lays out the struct, bounds
// guards, and slice helper from these fields; canonical naming, ordering, and
// C spelling stay Go decisions.
type sliceComponentRecord struct {
	CName           string
	HelperPrefix    string
	Suffix          string
	ElementSpelling string
	Writable        bool
	// NeedsHeapString is true when this specialization's element is String.
	NeedsHeapString bool
}

// sliceComponentRecordFor builds the spelling record of one Slice
// specialization.
func sliceComponentRecordFor(slice compilerTypes.Type) sliceComponentRecord {
	prefix := "hex_slice_"
	if slice.Slice.Writable {
		prefix = "hex_mut_slice_"
	}
	return sliceComponentRecord{
		CName:           slice.CName,
		HelperPrefix:    prefix,
		Suffix:          strings.TrimPrefix(slice.CName, prefix),
		ElementSpelling: typeSpelling(slice.Slice.Element),
		Writable:        slice.Slice.Writable,
		NeedsHeapString: compilerTypes.IsString(slice.Slice.Element),
	}
}

// sliceRecordsNeedHeapString reports whether any slice record's element is
// String, the only content in packages/slice.h that names hex_string.
func sliceRecordsNeedHeapString(records []sliceComponentRecord) bool {
	for _, record := range records {
		if record.NeedsHeapString {
			return true
		}
	}
	return false
}

// sliceComponents returns the generated hexal/slice.h artifact when builtin-
// element Slice specializations are reachable or another component declares
// slice.h as a dependency. Module-owned element slices emit into the consuming
// module headers instead.
func sliceComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.sliceState == nil || len(merged.sliceState.slices) == 0 && !merged.sliceState.required {
		return nil, nil
	}
	records := make([]sliceComponentRecord, 0, len(merged.sliceState.slices))
	for _, slice := range merged.sliceState.slices {
		if collectionElementModuleTyped(slice) {
			continue
		}
		records = append(records, sliceComponentRecordFor(slice))
	}
	if len(records) == 0 && !merged.sliceState.required {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/slice.h",
		template: "slice.h",
		model:    sliceComponentModel{Slices: records, NeedsHeapString: sliceRecordsNeedHeapString(records)},
	}}, nil
}

// moduleSliceComponent selects hexal/slice.h for a module with reachable
// builtin-element Slice specializations; a module whose only slices are
// module-owned re-emits them in its own header and includes nothing.
func moduleSliceComponent(emission *moduleEmission) []string {
	if emission == nil || emission.sliceState == nil || len(emission.sliceState.slices) == 0 {
		return nil
	}
	for _, slice := range emission.sliceState.slices {
		if !collectionElementModuleTyped(slice) {
			return []string{"hexal/slice.h"}
		}
	}
	return nil
}
