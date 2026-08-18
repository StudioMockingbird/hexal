package generator

// wrapHeaderModel is the typed render model for packages/wrap.h: one
// pre-sorted record per selected signed wrapping operation/type pair.
type wrapHeaderModel struct {
	Helpers []wrapHelperRecord
}

// wrapHelperRecord is one rendered wrapping helper's spelling facts: the
// operation kind, the checked C type spelling, and the canonical program-wide
// helper name. The template lays out the four helper shapes from these
// fields; canonical naming, ordering, and C spelling stay Go decisions.
type wrapHelperRecord struct {
	Name   string
	CName  string
	Helper string
}

// wrapComponents returns the generated hexal/wrap.h artifact when signed
// wrapping helpers are selected. The helpers migrate here from hexal.h; the
// template renders presentation only, so Go resolves the ordered records and
// type spellings from the program-wide discovery state.
func wrapComponents(merged *programEmission) ([]componentArtifact, error) {
	state := merged.wrapState
	if state == nil || len(state.order) == 0 {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/wrap.h",
		template: "wrap.h",
		model:    wrapHeaderModelFor(state),
	}}, nil
}

// wrapHeaderModelFor converts the program-wide selection into pre-sorted
// render records: one record per operation/type pair in the existing
// discovery order, with the canonical helper name and checked C type
// spelling resolved in Go.
func wrapHeaderModelFor(state *generatedWrapState) wrapHeaderModel {
	records := make([]wrapHelperRecord, 0, len(state.order))
	for _, operation := range state.order {
		records = append(records, wrapHelperRecord{
			Name:   operation.name,
			CName:  operation.typ.CName,
			Helper: wrapHelperName(operation),
		})
	}
	return wrapHeaderModel{Helpers: records}
}

// moduleWrapComponent selects hexal/wrap.h for a module using signed
// wrapping arithmetic.
func moduleWrapComponent(emission *moduleEmission) []string {
	state := emission.wrapState
	if state == nil || len(state.order) == 0 {
		return nil
	}
	return []string{"hexal/wrap.h"}
}
