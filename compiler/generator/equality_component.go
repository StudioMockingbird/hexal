package generator

import (
	"slices"
	"strings"

	compilerTypes "hexal/compiler/types"
)

// equalityHelperModel carries one pre-rendered static inline equality helper
// body for a program-owned type.
type equalityHelperModel struct {
	Body string
}

// equalityComponentModel is the render model for packages/equality.h.
type equalityComponentModel struct {
	Includes   []string
	NeedStddef bool
	NeedString bool
	NeedStdlib bool
	Helpers    []equalityHelperModel
}

// equalityComponents returns the generated hexal/equality.h artifact when
// program-owned equality types exist. Program-owned types are those whose
// C definitions are not module-emitted and are not builtins owned by other
// components (String, Strand, scalars, pointers).
func equalityComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.equalityTypes == nil {
		return nil, nil
	}
	model := buildEqualityComponentModel(merged)
	if len(model.Helpers) == 0 {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/equality.h",
		template: "equality.h",
		model:    model,
	}}, nil
}

// buildEqualityComponentModel pre-renders every program-owned equality
// helper body.
func buildEqualityComponentModel(merged *programEmission) equalityComponentModel {
	model := equalityComponentModel{}
	for _, typ := range merged.equalityTypes {
		if !isProgramOwnedEqualityType(typ) {
			continue
		}
		collectEqualityComponentDependencies(typ, &model, make(map[string]bool))
		var buf strings.Builder
		writeEqualityHelper(&buf, typ, merged.tags)
		model.Helpers = append(model.Helpers, equalityHelperModel{Body: buf.String()})
	}
	model.Includes = equalityComponentIncludes(model)
	return model
}

// collectEqualityComponentDependencies records the component headers needed
// by helper parameter and member types, including recursive String and Strand
// representations. It follows only inline compared values; pointer identity
// and unsupported dictionaries do not introduce a dependency.
func collectEqualityComponentDependencies(typ compilerTypes.Type, model *equalityComponentModel, seen map[string]bool) {
	if model == nil {
		return
	}
	key := typ.CanonicalKey
	if key == "" {
		key = typ.CName + "|" + typ.Name
	}
	if seen[key] {
		return
	}
	seen[key] = true
	switch {
	case compilerTypes.IsString(typ), compilerTypes.IsStrand(typ):
		model.Includes = appendUnique(model.Includes, "hexal/string.h")
		if compilerTypes.IsStrand(typ) {
			model.NeedString = true
		}
	case compilerTypes.IsError(typ):
		model.Includes = appendUnique(model.Includes, "hexal/error.h")
		model.Includes = appendUnique(model.Includes, "hexal/string.h")
		model.NeedStddef = true
		model.NeedString = true
	case compilerTypes.IsSeek(typ):
		model.Includes = appendUnique(model.Includes, "hexal/seek.h")
	case typ.Array != nil:
		model.Includes = appendUnique(model.Includes, "hexal/array.h")
		collectEqualityComponentDependencies(typ.Array.Element, model, seen)
	case typ.View != nil:
		model.Includes = appendUnique(model.Includes, "hexal/view.h")
		model.NeedStddef = true
		collectEqualityComponentDependencies(typ.View.Element, model, seen)
	case typ.List != nil:
		model.Includes = appendUnique(model.Includes, "hexal/list.h")
		model.NeedStddef = true
		collectEqualityComponentDependencies(typ.List.Element, model, seen)
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			collectEqualityComponentDependencies(member.Type, model, seen)
		}
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				collectEqualityComponentDependencies(member.Type, model, seen)
			}
		}
	case typ.Union != nil:
		model.NeedStdlib = true
		for _, member := range typ.Union.Members {
			collectEqualityComponentDependencies(member, model, seen)
		}
	}
}

func appendUnique(values []string, value string) []string {
	for _, existing := range values {
		if existing == value {
			return values
		}
	}
	return append(values, value)
}

func equalityComponentIncludes(model equalityComponentModel) []string {
	order := []string{
		"hexal/string.h",
		"hexal/error.h",
		"hexal/seek.h",
		"hexal/view.h",
		"hexal/list.h",
		"hexal/array.h",
	}
	selected := make(map[string]bool, len(model.Includes))
	for _, include := range model.Includes {
		selected[include] = true
	}
	includes := make([]string, 0, len(model.Includes))
	for _, include := range order {
		if selected[include] {
			includes = append(includes, include)
		}
	}
	return includes
}

// isProgramOwnedEqualityType reports whether typ's equality helper belongs
// in the program-wide component rather than a module header.
func isProgramOwnedEqualityType(typ compilerTypes.Type) bool {
	if typeIsModuleEmitted(typ) {
		return false
	}
	if compilerTypes.IsString(typ) || compilerTypes.IsStrand(typ) {
		return false
	}
	if typ.ScalarKind != compilerTypes.ScalarNone {
		return false
	}
	if typ.Element != nil {
		return false
	}
	return true
}

// moduleEqualityComponent selects hexal/equality.h for a module whose
// equality state contains program-owned types.
func moduleEqualityComponent(emission *moduleEmission) []string {
	if emission == nil || emission.equalityState == nil {
		return nil
	}
	for _, typ := range emission.equalityState.order {
		if isProgramOwnedEqualityType(typ) {
			return []string{"hexal/equality.h"}
		}
	}
	return nil
}

// mergeEqualityTypes collects the program-owned types from every module's
// equality state into the program-wide emission for the component builder.
func mergeEqualityTypes(merged *programEmission, module *moduleEmission) {
	if module.equalityState == nil {
		return
	}
	seen := make(map[string]bool, len(merged.equalityTypes))
	for _, typ := range merged.equalityTypes {
		seen[canonicalTypeKey(typ)] = true
	}
	for _, typ := range module.equalityState.order {
		key := canonicalTypeKey(typ)
		if !seen[key] {
			seen[key] = true
			merged.equalityTypes = append(merged.equalityTypes, typ)
		}
	}
}

func sortMergedEqualityTypes(merged *programEmission) {
	slices.SortStableFunc(merged.equalityTypes, func(left, right compilerTypes.Type) int {
		return strings.Compare(canonicalTypeKey(left), canonicalTypeKey(right))
	})
}
