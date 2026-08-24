package generator

import (
	"maps"
	"slices"
	"strconv"
)

// DefaultTaskStackReserve and DefaultTaskStackCommit are the per-Task stack
// sizes a zero Config selects: 1 MiB of address space and 8 KiB committed at
// spawn. The POSIX backend lazily maps the whole reserve and never pre-commits;
// the Windows backend passes both to CreateFiberEx.
const (
	DefaultTaskStackReserve = 1 << 20
	DefaultTaskStackCommit  = 8 << 10
)

// Config carries the build-time settings that reach the generated runtime.
// The zero value selects the defaults, so a caller without settings gets
// the historical behavior. The compiler package's Project mirrors this
// struct and validates its values before forwarding them.
type Config struct {
	TaskStackReserve uint64
	TaskStackCommit  uint64
}

// blockingSelected reports whether the program selects the blocking pool:
// the scheduler runtime combined with a reachable native blocking path
// (IO.read, IO.write, IO.seek, owned IO.close, or print's descriptor
// write-all sink). Standard-handle lookup, capability checks, zero-length
// transfers, and Bytes operations are not blocking jobs and contribute no
// flag here, so a Bytes-only or Bytes-plus-Task program selects no pool.
func blockingSelected(merged *programEmission) bool {
	if merged.concurrencyState == nil || !merged.concurrencyState.used {
		return false
	}
	if merged.printUsed {
		return true
	}
	return merged.ioState != nil && (merged.ioState.readIO || merged.ioState.writeIO || merged.ioState.seekIO || merged.ioState.closeIO)
}

// concurrencyComponents returns the generated hexal/concurrency.h and
// hexal/concurrency.c artifacts when Task, Channel, Mutex, or Atomic support
// is selected. The program-wide type prelude, runtime
// declarations, runtime definitions, and process-wide state migrate here
// from hexal.h and the root module C file; typed inline helpers, argument
// frames, and spawn entry adapters remain module-owned.
func concurrencyComponents(merged *programEmission, config Config) ([]componentArtifact, error) {
	state := merged.concurrencyState
	if state == nil || !state.used && len(state.atomics) == 0 {
		return nil, nil
	}
	blocking := blockingSelected(merged)
	artifacts := []componentArtifact{
		{key: "hexal/concurrency.h", template: "concurrency.h", model: concurrencyHeaderModelFrom(state, blocking)},
	}
	source := concurrencySourceModelFrom(state, config, blocking)
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
	Blocking     bool
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
// selected, plus the spelled stack-size expressions the configured Task
// stack values render to.
type concurrencySourceModel struct {
	Scheduler bool
	Blocking  bool
	Channels  bool
	Mutex     bool
	// StackSizeExpression is the POSIX usable stack size; the historical
	// "1u << 20" for the default reserve, a decimal literal otherwise, so a
	// zero Config renders the runtime text byte-for-byte as before.
	StackSizeExpression string
	// FiberCommit and FiberReserve are the CreateFiberEx commit and reserve
	// arguments; "0" asks for the PE-header defaults.
	FiberCommit  string
	FiberReserve string
}

// concurrencyHeaderModelFrom builds the header model from the program-wide
// state, pre-sorting every data-driven handle and entry list by its C name.
func concurrencyHeaderModelFrom(state *generatedConcurrencyState, blocking bool) concurrencyHeaderModel {
	model := concurrencyHeaderModel{Scheduler: state.used, Blocking: blocking}
	taskNames := slices.Sorted(maps.Keys(state.taskTypes))
	for _, name := range taskNames {
		model.Tasks = append(model.Tasks, taskSuffix(state.taskTypes[name]))
	}
	channelNames := slices.Sorted(maps.Keys(state.channels))
	for _, name := range channelNames {
		model.Channels = append(model.Channels, channelSuffix(state.channels[name]))
	}
	atomicNames := slices.Sorted(maps.Keys(state.atomics))
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
	slices.Sort(entryNames)
	model.SpawnEntries = entryNames
	return model
}

// concurrencySourceModelFrom maps the program-wide operation flags onto the
// runtime core sections: the scheduler platform layer and Task machinery
// follow the scheduler requirement, the Channel core its handle use, and the
// Mutex core any selected Mutex operation. The config's stack sizes are
// spelled for the two platform allocation sites.
func concurrencySourceModelFrom(state *generatedConcurrencyState, config Config, blocking bool) concurrencySourceModel {
	return concurrencySourceModel{
		Scheduler:           state.used,
		Blocking:            blocking,
		Channels:            len(state.channels) > 0,
		Mutex:               state.mutexNew || state.mutexLock || state.mutexUnlock || state.mutexFree,
		StackSizeExpression: stackSizeExpression(config.TaskStackReserve),
		FiberCommit:         fiberArgument(config.TaskStackCommit),
		FiberReserve:        fiberArgument(config.TaskStackReserve),
	}
}

// stackSizeExpression spells the POSIX usable stack size: the historical
// "1u << 20" for the default reserve, a u-suffixed decimal literal otherwise,
// so a zero Config keeps the runtime text byte-identical.
func stackSizeExpression(reserve uint64) string {
	if reserve == 0 || reserve == DefaultTaskStackReserve {
		return "1u << 20"
	}
	return strconv.FormatUint(reserve, 10) + "u"
}

// fiberArgument spells one CreateFiberEx size argument: "0" asks for the
// PE-header default, a u-suffixed decimal literal otherwise. The suffix keeps
// a value above int64 range a valid size_t literal.
func fiberArgument(value uint64) string {
	if value == 0 {
		return "0"
	}
	return strconv.FormatUint(value, 10) + "u"
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
