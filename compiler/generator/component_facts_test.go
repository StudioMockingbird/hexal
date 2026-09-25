package generator

import (
	"strings"
	"testing"

	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// addComponentHeaders adds exactly the unconditional headers a component's
// record declares, for every registered component. The registry is the sole
// owner of the names; the generator only decides selection.
func TestComponentHeaderHelpersReadRecords(t *testing.T) {
	for _, component := range specdata.Components() {
		requirements := &cHeaderRequirements{}
		if err := requirements.addComponentHeaders(component.ID); err != nil {
			t.Fatalf("addComponentHeaders(%s) error = %v", component.ID, err)
		}
		for _, header := range component.RequiredCHeaders {
			if !requirements.headers[header] {
				t.Errorf("addComponentHeaders(%s) omitted recorded header %s", component.ID, header)
			}
		}
		if len(requirements.headers) != len(component.RequiredCHeaders) {
			t.Errorf("addComponentHeaders(%s) = %v, want exactly %v", component.ID, requirements.headers, component.RequiredCHeaders)
		}
	}
}

// addConditionalComponentHeaders adds a named group only when active, and
// fails closed on an unregistered component or condition.
func TestConditionalComponentHeaderHelpersReadRecords(t *testing.T) {
	for _, component := range specdata.Components() {
		for _, group := range component.ConditionalHeaders {
			active := &cHeaderRequirements{}
			if err := active.addConditionalComponentHeaders(component.ID, group.Condition, true); err != nil {
				t.Fatalf("addConditionalComponentHeaders(%s, %s, true) error = %v", component.ID, group.Condition, err)
			}
			for _, header := range group.Headers {
				if !active.headers[header] {
					t.Errorf("active condition %s/%s omitted recorded header %s", component.ID, group.Condition, header)
				}
			}
			if len(active.headers) != len(group.Headers) {
				t.Errorf("active condition %s/%s = %v, want exactly %v", component.ID, group.Condition, active.headers, group.Headers)
			}
			inactive := &cHeaderRequirements{}
			if err := inactive.addConditionalComponentHeaders(component.ID, group.Condition, false); err != nil {
				t.Fatalf("addConditionalComponentHeaders(%s, %s, false) error = %v", component.ID, group.Condition, err)
			}
			if len(inactive.headers) != 0 {
				t.Errorf("inactive condition %s/%s added %v, want nothing", component.ID, group.Condition, inactive.headers)
			}
		}
	}
	if err := (&cHeaderRequirements{}).addConditionalComponentHeaders(specdata.ComponentHeap, specdata.HeaderConditionBitCast, true); err == nil {
		t.Fatal("a condition the component records no group for must fail closed")
	}
}

// selectedRuntimeDependencies reports a native input only when its demand
// predicate selects it and a selected component's record names it.
func TestSelectedRuntimeDependenciesReadRecords(t *testing.T) {
	heapSelected := &programEmission{heapState: &heapHelpers{required: true}}
	dependencies, err := selectedRuntimeDependencies(map[specdata.ComponentID]bool{specdata.ComponentHeap: true}, heapSelected)
	if err != nil {
		t.Fatalf("heap-only dependency error = %v", err)
	}
	if strings.Join(dependencies, ",") != string(specdata.DependencyMimalloc) {
		t.Fatalf("heap-only dependencies = %v, want [%s]", dependencies, specdata.DependencyMimalloc)
	}

	// The runtime component records libuv, and libuvSelected fires for a
	// scheduler program, so the record is what names the linked input.
	scheduled := &programEmission{concurrencyState: &generatedConcurrencyState{used: true}}
	dependencies, err = selectedRuntimeDependencies(map[specdata.ComponentID]bool{specdata.ComponentRuntime: true}, scheduled)
	if err != nil {
		t.Fatalf("scheduler dependency error = %v", err)
	}
	if strings.Join(dependencies, ",") != string(specdata.DependencyLibuv) {
		t.Fatalf("scheduler dependencies = %v, want [%s]", dependencies, specdata.DependencyLibuv)
	}

	validator := &programEmission{validatorNeed: true}
	dependencies, err = selectedRuntimeDependencies(map[specdata.ComponentID]bool{specdata.ComponentString: true}, validator)
	if err != nil {
		t.Fatalf("validator dependency error = %v", err)
	}
	if strings.Join(dependencies, ",") != string(specdata.DependencyUtf8proc) {
		t.Fatalf("validator dependencies = %v, want [%s]", dependencies, specdata.DependencyUtf8proc)
	}

	// A type-only Process program reaches no libuv operation but still
	// selects the Handle component, whose handle.c self-includes <uv.h> and
	// whose record names libuv; the demand must fire for exactly that case.
	typeOnlyProcess := &programEmission{processState: &generatedProcessState{used: true}}
	dependencies, err = selectedRuntimeDependencies(map[specdata.ComponentID]bool{specdata.ComponentHandle: true}, typeOnlyProcess)
	if err != nil {
		t.Fatalf("type-only process dependency error = %v", err)
	}
	if strings.Join(dependencies, ",") != string(specdata.DependencyLibuv) {
		t.Fatalf("type-only process dependencies = %v, want [%s]", dependencies, specdata.DependencyLibuv)
	}

	// A demand predicate firing while no selected component records the input
	// is a registry/builder disagreement, never a silently dropped link.
	if _, err := selectedRuntimeDependencies(map[specdata.ComponentID]bool{}, heapSelected); err == nil {
		t.Fatal("a demanded input no selected component records must fail closed")
	}
}

// computeHeaderRequirements reads the conditional header groups from the
// component records: the generator decides the condition, the registry owns the
// header names.
func TestComputeHeaderRequirementsReadsConditionalGroups(t *testing.T) {
	module := func(state *moduleEmission) []*moduleEmission { return []*moduleEmission{state} }

	// An atomic-only concurrency program adds the recorded <stdatomic.h> but
	// not the scheduler's portable headers.
	requirements, err := computeHeaderRequirements(&programEmission{}, module(&moduleEmission{
		concurrencyState: &generatedConcurrencyState{atomics: map[string]compilerTypes.Type{"Int32": {}}},
	}))
	if err != nil {
		t.Fatalf("atomic requirements error = %v", err)
	}
	if !requirements.headers["stdatomic.h"] {
		t.Fatalf("atomic-only concurrency must add the recorded <stdatomic.h>: %v", requirements.headers)
	}
	if requirements.headers["stdckdint.h"] {
		t.Fatalf("atomic-only concurrency must not add the scheduler headers: %v", requirements.headers)
	}

	// A float-source conversion adds the numeric component's recorded
	// <math.h>; the integer-source case is covered by the existing conversion
	// suite, which forbids it.
	requirements, err = computeHeaderRequirements(&programEmission{}, module(&moduleEmission{
		conversionSpecs: []conversionSpec{{source: compilerTypes.Float64}},
	}))
	if err != nil {
		t.Fatalf("float conversion requirements error = %v", err)
	}
	if !requirements.headers["math.h"] {
		t.Fatalf("float conversion must add the recorded <math.h>: %v", requirements.headers)
	}

	// The program component's argument group contributes <string.h> without
	// the path group's <stdckdint.h>.
	requirements, err = computeHeaderRequirements(&programEmission{}, module(&moduleEmission{
		corelibState: &generatedCorelibState{used: true, program: true, arguments: true},
	}))
	if err != nil {
		t.Fatalf("argument requirements error = %v", err)
	}
	if !requirements.headers["string.h"] {
		t.Fatalf("argument snapshot must add the recorded <string.h>: %v", requirements.headers)
	}
	if requirements.headers["stdckdint.h"] {
		t.Fatalf("argument snapshot must not add the path group's <stdckdint.h>: %v", requirements.headers)
	}

	// The path group contributes the recorded checked-arithmetic header.
	requirements, err = computeHeaderRequirements(&programEmission{}, module(&moduleEmission{
		corelibState: &generatedCorelibState{used: true, program: true, paths: true},
	}))
	if err != nil {
		t.Fatalf("path requirements error = %v", err)
	}
	if !requirements.headers["stdckdint.h"] {
		t.Fatalf("path query must add the recorded <stdckdint.h>: %v", requirements.headers)
	}

	// An ADT equality helper aborts through its default switch arm, so the
	// equality component's aborting group contributes the recorded <stdlib.h>;
	// scalar-only equality does not.
	requirements, err = computeHeaderRequirements(&programEmission{}, module(&moduleEmission{
		equalityState: &generatedEqualityState{order: []compilerTypes.Type{{Adt: &compilerTypes.AdtType{}}}},
	}))
	if err != nil {
		t.Fatalf("ADT equality requirements error = %v", err)
	}
	if !requirements.headers["stdlib.h"] {
		t.Fatalf("ADT equality must add the recorded <stdlib.h>: %v", requirements.headers)
	}
	requirements, err = computeHeaderRequirements(&programEmission{}, module(&moduleEmission{
		equalityState: &generatedEqualityState{order: []compilerTypes.Type{{Name: "Plain"}}},
	}))
	if err != nil {
		t.Fatalf("plain equality requirements error = %v", err)
	}
	if requirements.headers["stdlib.h"] {
		t.Fatalf("plain equality must not add the aborting group's <stdlib.h>: %v", requirements.headers)
	}
}
