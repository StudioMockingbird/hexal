package generator

import (
	"sort"
)

// concurrencyComponents returns the generated hexal/concurrency.h and
// hexal/concurrency.c artifacts when Task, Channel, Mutex, or Atomic support
// is selected. The program-wide type prelude, runtime
// declarations, runtime definitions, and process-wide state migrate here
// from hexal.h and the root module C file; typed inline helpers, argument
// frames, and spawn entry adapters remain module-owned.
func concurrencyComponents(merged *programEmission) ([]componentArtifact, error) {
	state := merged.concurrencyState
	if state == nil || !state.used && len(state.atomics) == 0 {
		return nil, nil
	}
	artifacts := []componentArtifact{
		{key: "hexal/concurrency.h", template: "concurrency.h", model: concurrencyHeaderModelFrom(state)},
	}
	source := concurrencySourceModelFrom(state)
	if source.Scheduler || source.Channels || source.Mutex {
		// The source artifact is emitted only when it contains at least one
		// runtime core definition; an atomic-only program gets
		// the header alone.
		artifacts = append(artifacts, componentArtifact{key: "hexal/concurrency.c", template: "concurrency.c", model: source})
	}
	return artifacts, nil
}

// concurrencyHeaderModel is the render model for packages/concurrency.h: the
// scheduler requirement and the pre-sorted program-wide handle, atomic, and
// spawn-entry records. An atomic-only program renders the Atomic typedefs
// without the scheduler prelude or the runtime entry-point declarations.
type concurrencyHeaderModel struct {
	Scheduler    bool
	Tasks        []string
	Channels     []string
	Atomics      []concurrencyAtomicModel
	SpawnEntries []string
}

// concurrencyAtomicModel is one Atomic<T> typedef record: the suffix after
// hex_atomic_ and the spelled element type.
type concurrencyAtomicModel struct {
	Suffix  string
	Element string
}

// concurrencySourceModel is the render model for packages/concurrency.c: one
// flag per runtime core family, so a program renders exactly the cores it
// selected.
type concurrencySourceModel struct {
	Scheduler bool
	Channels  bool
	Mutex     bool
}

// concurrencyHeaderModelFrom builds the header model from the program-wide
// state, pre-sorting every data-driven handle and entry list by its C name.
func concurrencyHeaderModelFrom(state *generatedConcurrencyState) concurrencyHeaderModel {
	model := concurrencyHeaderModel{Scheduler: state.used}
	taskNames := make([]string, 0, len(state.taskTypes))
	for name := range state.taskTypes {
		taskNames = append(taskNames, name)
	}
	sort.Strings(taskNames)
	for _, name := range taskNames {
		model.Tasks = append(model.Tasks, taskSuffix(state.taskTypes[name]))
	}
	channelNames := make([]string, 0, len(state.channels))
	for name := range state.channels {
		channelNames = append(channelNames, name)
	}
	sort.Strings(channelNames)
	for _, name := range channelNames {
		model.Channels = append(model.Channels, channelSuffix(state.channels[name]))
	}
	atomicNames := make([]string, 0, len(state.atomics))
	for name := range state.atomics {
		atomicNames = append(atomicNames, name)
	}
	sort.Strings(atomicNames)
	for _, name := range atomicNames {
		atomic := state.atomics[name]
		model.Atomics = append(model.Atomics, concurrencyAtomicModel{
			Suffix:  atomicSuffix(atomic),
			Element: typeSpelling(atomic.Atomic.Element),
		})
	}
	entryNames := make([]string, 0, len(state.spawns))
	for _, site := range state.spawns {
		entryNames = append(entryNames, site.function)
	}
	sort.Strings(entryNames)
	model.SpawnEntries = entryNames
	return model
}

// concurrencySourceModelFrom maps the program-wide operation flags onto the
// runtime core sections: the scheduler platform layer and Task machinery
// follow the scheduler requirement, the Channel core its handle use, and the
// Mutex core any selected Mutex operation.
func concurrencySourceModelFrom(state *generatedConcurrencyState) concurrencySourceModel {
	return concurrencySourceModel{
		Scheduler: state.used,
		Channels:  len(state.channels) > 0,
		Mutex:     state.mutexNew || state.mutexLock || state.mutexUnlock || state.mutexFree,
	}
}

// concurrencyFamilyContent is the drained transition seam: the prelude and
// runtime declarations own the concurrency component, so hexal.h contributes
// none of them.
func concurrencyFamilyContent(state *generatedConcurrencyState) string {
	return ""
}

// concurrencyRuntimeContent is the drained transition seam: the runtime
// definitions and state own the concurrency component, so the root module C
// file contributes none of them.
func concurrencyRuntimeContent(state *generatedConcurrencyState, stringState *generatedStringState) string {
	return ""
}

// moduleConcurrencyComponent selects hexal/concurrency.h for a module whose
// generated machinery uses the concurrency cores or atomics.
func moduleConcurrencyComponent(emission *moduleEmission) []string {
	state := emission.concurrencyState
	if state != nil && (state.used || len(state.atomics) > 0) {
		return []string{"hexal/concurrency.h"}
	}
	return nil
}
