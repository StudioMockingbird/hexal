package generator

// component_facts.go is the consumer of the specdata component records: the
// program-wide standard-header set and the native dependency set are read from
// each selected component's record instead of restated at the demand site. The
// demand predicates stay explicit Go functions (Decision 6); only the fact a
// selected component contributes moves into the registry.

import (
	"hexal/compiler/specdata"
)

// unknownComponentDiagnostic reports a builder/registry disagreement: a
// component identity with no record is a compiler-development failure, never a
// silently omitted contribution.
func unknownComponentDiagnostic(id specdata.ComponentID) error {
	return generatorDiagnostic()
}

// addComponentHeaders adds a registered component's unconditional standard
// headers. The registry owns the names; the caller's demand predicate decides
// whether the component is selected.
func (requirements *cHeaderRequirements) addComponentHeaders(id specdata.ComponentID) error {
	component, ok := specdata.Component(id)
	if !ok {
		return unknownComponentDiagnostic(id)
	}
	requirements.add(component.RequiredCHeaders...)
	return nil
}

// addConditionalComponentHeaders adds one named conditional header group of a
// registered component when active. A component that records no such condition
// is a builder/registry disagreement, not a silently dropped contribution.
func (requirements *cHeaderRequirements) addConditionalComponentHeaders(id specdata.ComponentID, condition specdata.HeaderCondition, active bool) error {
	component, ok := specdata.Component(id)
	if !ok {
		return unknownComponentDiagnostic(id)
	}
	for _, group := range component.ConditionalHeaders {
		if group.Condition != condition {
			continue
		}
		if active {
			requirements.add(group.Headers...)
		}
		return nil
	}
	return generatorDiagnostic()
}

// nativeDependencyDemand pairs one declared native input with the explicit Go
// predicate that decides whether the selected components pulling it actually
// link it. The registry owns the dependency identities and which components can
// pull them; this predicate is the demand logic Decision 6 keeps in Go.
type nativeDependencyDemand struct {
	dependency specdata.DependencyID
	selected   func(*programEmission) bool
}

// nativeDependencyDemands lists every native input with its demand predicate,
// in the generator's stable output order.
func nativeDependencyDemands() []nativeDependencyDemand {
	return []nativeDependencyDemand{
		{dependency: specdata.DependencyMimalloc, selected: func(merged *programEmission) bool {
			return merged.heapState.selected()
		}},
		{dependency: specdata.DependencyLibuv, selected: libuvSelected},
		{dependency: specdata.DependencyUtf8proc, selected: utf8procSelected},
	}
}

// selectedRuntimeDependencies returns the native inputs the generated
// artifacts link. A demand predicate may select an input only when a selected
// component's record names it: the registry is the authority for which
// components can pull which input, so a predicate selecting an input no
// selected component records is a registry/builder disagreement reported as a
// compiler error, never a silently dropped link.
func selectedRuntimeDependencies(selected map[specdata.ComponentID]bool, merged *programEmission) ([]string, error) {
	recorded := make(map[specdata.DependencyID]bool)
	for id := range selected {
		component, ok := specdata.Component(id)
		if !ok {
			return nil, unknownComponentDiagnostic(id)
		}
		for _, dependency := range component.RuntimeDependencies {
			recorded[dependency] = true
		}
	}
	dependencies := make([]string, 0, 2)
	for _, demand := range nativeDependencyDemands() {
		if !demand.selected(merged) {
			continue
		}
		if !recorded[demand.dependency] {
			return nil, generatorDiagnostic()
		}
		dependencies = append(dependencies, string(demand.dependency))
	}
	return dependencies, nil
}

// selectedComponentIDs maps a rendered component artifact set to the registry
// identities that own it. renderComponentArtifacts has already validated that
// every emitted artifact belongs to a claimed component, so an unowned key here
// is a compiler-development failure.
func selectedComponentIDs(artifacts map[string]string) (map[specdata.ComponentID]bool, error) {
	selected := make(map[specdata.ComponentID]bool, len(artifacts))
	for key := range artifacts {
		owner, ok := specdata.FileOwner(key)
		if !ok {
			return nil, generatorDiagnostic()
		}
		selected[owner] = true
	}
	return selected, nil
}
