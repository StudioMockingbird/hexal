package generator

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// viewComponentModel is the typed render model for packages/view.h: one
// pre-sorted record per reachable View specialization.
type viewComponentModel struct {
	Views []viewComponentRecord
}

// viewComponentRecord is one reachable View specialization's spelling facts:
// the struct C name, the accessor suffix, and the spelled element type. The
// template lays out the struct, bounds guards, and slice helper from these
// fields; canonical naming, ordering, and C spelling stay Go decisions.
type viewComponentRecord struct {
	CName           string
	Suffix          string
	ElementSpelling string
}

// viewComponentRecordFor builds the spelling record of one View
// specialization.
func viewComponentRecordFor(view compilerTypes.Type) viewComponentRecord {
	return viewComponentRecord{
		CName:           view.CName,
		Suffix:          strings.TrimPrefix(view.CName, "hex_view_"),
		ElementSpelling: pointerSpelling(view.View.Element),
	}
}

// viewComponents returns the generated hexal/view.h artifact when builtin-
// element View specializations are reachable or another component declares
// view.h as a dependency. Module-owned element views emit into the consuming
// module headers instead.
func viewComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.viewState == nil || len(merged.viewState.views) == 0 && !merged.viewState.required {
		return nil, nil
	}
	records := make([]viewComponentRecord, 0, len(merged.viewState.views))
	for _, view := range merged.viewState.views {
		if collectionElementModuleTyped(view) {
			continue
		}
		records = append(records, viewComponentRecordFor(view))
	}
	if len(records) == 0 && !merged.viewState.required {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/view.h",
		template: "view.h",
		model:    viewComponentModel{Views: records},
	}}, nil
}

// moduleViewComponent selects hexal/view.h for a module with reachable
// builtin-element View specializations; a module whose only views are
// module-owned re-emits them in its own header and includes nothing.
func moduleViewComponent(emission *moduleEmission) []string {
	if emission == nil || emission.viewState == nil || len(emission.viewState.views) == 0 {
		return nil
	}
	for _, view := range emission.viewState.views {
		if !collectionElementModuleTyped(view) {
			return []string{"hexal/view.h"}
		}
	}
	return nil
}
