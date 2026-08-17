package generator

import (
	"strings"
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

// viewComponent returns the generated hexal/view.h artifact when View
// specializations are reachable or another component declares view.h as a
// dependency.
func viewComponent(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.viewState == nil || len(merged.viewState.views) == 0 && !merged.viewState.required {
		return nil, nil
	}
	records := make([]viewComponentRecord, 0, len(merged.viewState.views))
	for _, view := range merged.viewState.views {
		records = append(records, viewComponentRecord{
			CName:           view.CName,
			Suffix:          strings.TrimPrefix(view.CName, "hex_view_"),
			ElementSpelling: pointerSpelling(view.View.Element),
		})
	}
	return []componentArtifact{{
		key:      "hexal/view.h",
		template: "view.h",
		model:    viewComponentModel{Views: records},
	}}, nil
}

// viewFamilyContent is the transition seam; the view definitions have moved
// to view.h, so hexal.h carries nothing.
func viewFamilyContent(state *generatedViewState) string {
	return ""
}

// moduleViewComponent selects hexal/view.h for a module with reachable View
// specializations.
func moduleViewComponent(emission *moduleEmission) []string {
	if emission == nil || emission.viewState == nil || len(emission.viewState.views) == 0 {
		return nil
	}
	return []string{"hexal/view.h"}
}
