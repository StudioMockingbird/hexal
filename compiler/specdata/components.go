package specdata

import "fmt"

// Runtime-component facts: one record per component, naming the generated files
// it owns, the native dependencies its generated code can pull, and the
// standard C headers that code needs. Demand is deliberately absent: the
// generator's explicit Go builder decides whether a component is emitted, and a
// builder may claim more than one identity when one demand site emits two
// components.
//
// The names are primitives, not compiler types, so this package stays below
// compiler/types in the import graph. The generator imports this package; this
// package imports nothing from the compiler.

// ComponentID names one demand-driven runtime support component. It is the
// registry key that connects a generated artifact to the metadata describing
// it.
type ComponentID string

// DependencyID names one native runtime input a component's generated code can
// require.
type DependencyID string

// Component identities, one per builder family. ComponentProgram and
// ComponentEntropy share the corelibComponents demand site, and
// ComponentRuntime is the trap artifact any trapping program emits.
const (
	ComponentRuntime     ComponentID = "runtime"
	ComponentWrap        ComponentID = "wrap"
	ComponentHeap        ComponentID = "heap"
	ComponentSlice       ComponentID = "slice"
	ComponentString      ComponentID = "string"
	ComponentError       ComponentID = "error"
	ComponentSeek        ComponentID = "seek"
	ComponentStash       ComponentID = "stash"
	ComponentPool        ComponentID = "pool"
	ComponentList        ComponentID = "list"
	ComponentDict        ComponentID = "dict"
	ComponentArray       ComponentID = "array"
	ComponentNumeric     ComponentID = "numeric"
	ComponentPrint       ComponentID = "print"
	ComponentEquality    ComponentID = "equality"
	ComponentIO          ComponentID = "io"
	ComponentConcurrency ComponentID = "concurrency"
	ComponentEvent       ComponentID = "event"
	ComponentTime        ComponentID = "time"
	ComponentHandle      ComponentID = "handle"
	ComponentFile        ComponentID = "file"
	ComponentNetwork     ComponentID = "network"
	ComponentProcess     ComponentID = "process"
	ComponentSignal      ComponentID = "signal"
	ComponentTerminal    ComponentID = "terminal"
	ComponentProgram     ComponentID = "program"
	ComponentEntropy     ComponentID = "entropy"
)

// Dependency identities. These are the sole declarations of the native input
// names; the compiler package's exported RuntimeDependency constants alias them
// while their last consumer remains.
const (
	DependencyMimalloc DependencyID = "mimalloc"
	DependencyLibuv    DependencyID = "libuv"
	DependencyUtf8proc DependencyID = "utf8proc"
)

// DependencySpec is one native runtime input identity.
type DependencySpec struct {
	ID DependencyID
}

// HeaderCondition names one program-wide Go demand predicate that gates a
// conditional standard-header contribution. The registry owns the condition
// names and the headers they gate; the generator owns the predicate that
// decides which conditions are active, so no predicate becomes data.
type HeaderCondition string

// The conditional header groups a component record can declare. Each is the
// registry-side name of one explicit Go predicate at a generator demand site.
const (
	// HeaderConditionEqualityAborting gates <stdlib.h> for union and ADT
	// equality helpers, whose exhaustive switch carries an abort() catch-all.
	HeaderConditionEqualityAborting HeaderCondition = "equality-aborting"
	// HeaderConditionConcurrencyAtomic gates <stdatomic.h> for the atomic
	// handle typedefs; a scheduler-only program declares no atomic.
	HeaderConditionConcurrencyAtomic HeaderCondition = "concurrency-atomic"
	// HeaderConditionConversionFloat gates <math.h> for a checked conversion
	// whose source is a float; integer-source conversions never classify.
	HeaderConditionConversionFloat HeaderCondition = "conversion-float"
	// HeaderConditionBitCast gates <string.h> for bit_cast's memcpy
	// reinterpretation.
	HeaderConditionBitCast HeaderCondition = "bit-cast"
	// HeaderConditionCorelibPaths gates the checked size arithmetic,
	// malloc/free, and memcpy/strlen of std/program's path and parallelism
	// surface.
	HeaderConditionCorelibPaths HeaderCondition = "corelib-paths"
	// HeaderConditionCorelibArguments gates the malloc/free and memcpy of the
	// std/program argument snapshot.
	HeaderConditionCorelibArguments HeaderCondition = "corelib-arguments"
	// HeaderConditionCorelibEntropy gates the malloc/free and memcpy of the
	// std/entropy fill.
	HeaderConditionCorelibEntropy HeaderCondition = "corelib-entropy"
)

// ConditionalHeaders is one named conditional standard-header contribution: a
// HeaderCondition the generator decides, and the headers it gates.
type ConditionalHeaders struct {
	Condition HeaderCondition
	Headers   []string
}

// ComponentSpec is the metadata of one runtime component: its identity, the
// generated files it owns, the native dependencies its generated code can pull,
// and the standard C headers the program-wide hexal.h must provide. RequiredCHeaders are the
// unconditional contribution when the component is demanded; ConditionalHeaders
// are the named sub-features the generator activates. A component whose own .c
// includes a standard header directly contributes none. Demand is not a field
// here.
type ComponentSpec struct {
	ID                  ComponentID
	Files               []string
	RuntimeDependencies []DependencyID
	RequiredCHeaders    []string
	ConditionalHeaders  []ConditionalHeaders
}

// dependencyRegistry declares every native runtime input exactly once.
var dependencyRegistry = []DependencySpec{
	{ID: DependencyMimalloc},
	{ID: DependencyLibuv},
	{ID: DependencyUtf8proc},
}

// componentRegistry declares every runtime component exactly once, in the
// generator's demand order. RuntimeDependencies is the set the component's
// generated code can pull, not a claim that every program selecting the
// component pulls all of them; the builder plus the program-wide dependency
// predicates decide that. RequiredCHeaders is the component's unconditional
// standard-header contribution, and ConditionalHeaders the named sub-features
// the generator activates; <stdio.h>/<stdlib.h> from the shared trap are
// recorded on ComponentRuntime, and the C library's own headers are not
// listed.
var componentRegistry = []ComponentSpec{
	{
		ID:                  ComponentRuntime,
		Files:               []string{"hexal/runtime.c"},
		RuntimeDependencies: []DependencyID{DependencyMimalloc, DependencyLibuv},
		RequiredCHeaders:    []string{"stdio.h", "stdlib.h"},
	},
	{
		ID:               ComponentWrap,
		Files:            []string{"hexal/wrap.h"},
		RequiredCHeaders: []string{"stdckdint.h"},
	},
	{
		ID:                  ComponentHeap,
		Files:               []string{"hexal/heap.h", "hexal/heap.c"},
		RuntimeDependencies: []DependencyID{DependencyMimalloc},
		RequiredCHeaders:    []string{"stdckdint.h", "stddef.h", "stdlib.h"},
	},
	{
		ID:               ComponentSlice,
		Files:            []string{"hexal/slice.h"},
		RequiredCHeaders: []string{"stddef.h", "stdint.h"},
	},
	{
		ID:                  ComponentString,
		Files:               []string{"hexal/string.h", "hexal/string.c"},
		RuntimeDependencies: []DependencyID{DependencyUtf8proc},
		RequiredCHeaders:    []string{"stdckdint.h", "stddef.h", "stdint.h", "stdlib.h", "string.h"},
	},
	{
		ID:    ComponentError,
		Files: []string{"hexal/error.h"},
	},
	{
		ID:    ComponentSeek,
		Files: []string{"hexal/seek.h"},
	},
	{
		ID:               ComponentStash,
		Files:            []string{"hexal/stash.h", "hexal/stash.c"},
		RequiredCHeaders: []string{"stdckdint.h", "stddef.h"},
	},
	{
		ID:               ComponentPool,
		Files:            []string{"hexal/pool.h"},
		RequiredCHeaders: []string{"stdckdint.h", "stddef.h", "stdint.h"},
	},
	{
		ID:               ComponentList,
		Files:            []string{"hexal/list.h"},
		RequiredCHeaders: []string{"stdckdint.h", "stddef.h", "stdint.h", "string.h"},
	},
	{
		ID:               ComponentDict,
		Files:            []string{"hexal/dict.h"},
		RequiredCHeaders: []string{"stdckdint.h", "stddef.h", "stdint.h", "string.h"},
	},
	{
		ID:               ComponentArray,
		Files:            []string{"hexal/array.h"},
		RequiredCHeaders: []string{"stdint.h"},
	},
	{
		ID:               ComponentNumeric,
		Files:            []string{"hexal/numeric.h"},
		RequiredCHeaders: []string{"stdint.h"},
		ConditionalHeaders: []ConditionalHeaders{
			{Condition: HeaderConditionConversionFloat, Headers: []string{"math.h"}},
			{Condition: HeaderConditionBitCast, Headers: []string{"string.h"}},
		},
	},
	{
		ID:               ComponentPrint,
		Files:            []string{"hexal/print.h", "hexal/print.c"},
		RequiredCHeaders: []string{"stddef.h", "stdint.h", "stdio.h", "inttypes.h", "math.h"},
	},
	{
		ID:               ComponentEquality,
		Files:            []string{"hexal/equality.h"},
		RequiredCHeaders: []string{"stddef.h", "string.h"},
		ConditionalHeaders: []ConditionalHeaders{
			{Condition: HeaderConditionEqualityAborting, Headers: []string{"stdlib.h"}},
		},
	},
	{
		ID:               ComponentIO,
		Files:            []string{"hexal/io.h", "hexal/io.c"},
		RequiredCHeaders: []string{"stdckdint.h", "stddef.h", "stdint.h"},
	},
	{
		ID:                  ComponentConcurrency,
		Files:               []string{"hexal/concurrency.h", "hexal/concurrency.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
		RequiredCHeaders:    []string{"stdckdint.h", "stddef.h", "stdint.h", "stdlib.h"},
		ConditionalHeaders: []ConditionalHeaders{
			{Condition: HeaderConditionConcurrencyAtomic, Headers: []string{"stdatomic.h"}},
		},
	},
	{
		ID:                  ComponentEvent,
		Files:               []string{"hexal/event.h", "hexal/event.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
	},
	{
		ID:                  ComponentTime,
		Files:               []string{"hexal/time.h", "hexal/time.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
		RequiredCHeaders:    []string{"stdint.h"},
	},
	{
		// handle.c self-includes <string.h> and <uv.h>, so the component
		// contributes no standard header to hexal.h; declaring one would emit
		// it for a type-only Process/Signal program that never uses the
		// registry's memcpy.
		ID:                  ComponentHandle,
		Files:               []string{"hexal/handle.h", "hexal/handle.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
	},
	{
		ID:                  ComponentFile,
		Files:               []string{"hexal/file.h", "hexal/file.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
		RequiredCHeaders:    []string{"stdckdint.h", "stddef.h", "stdint.h", "stdlib.h"},
	},
	{
		ID:                  ComponentNetwork,
		Files:               []string{"hexal/network.h", "hexal/network.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
		RequiredCHeaders:    []string{"stdckdint.h", "stddef.h", "stdint.h", "stdlib.h"},
	},
	{
		ID:                  ComponentProcess,
		Files:               []string{"hexal/process.h", "hexal/process.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
		RequiredCHeaders:    []string{"stdckdint.h", "stddef.h", "stdint.h", "stdlib.h"},
	},
	{
		ID:                  ComponentSignal,
		Files:               []string{"hexal/signal.h", "hexal/signal.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
		RequiredCHeaders:    []string{"stddef.h", "stdint.h"},
	},
	{
		ID:                  ComponentTerminal,
		Files:               []string{"hexal/terminal.h", "hexal/terminal.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
		RequiredCHeaders:    []string{"stddef.h"},
	},
	{
		ID:                  ComponentProgram,
		Files:               []string{"hexal/program.h", "hexal/program.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv, DependencyUtf8proc},
		RequiredCHeaders:    []string{"stddef.h", "stdint.h"},
		ConditionalHeaders: []ConditionalHeaders{
			{Condition: HeaderConditionCorelibPaths, Headers: []string{"stdckdint.h", "stdlib.h", "string.h"}},
			{Condition: HeaderConditionCorelibArguments, Headers: []string{"stdlib.h", "string.h"}},
		},
	},
	{
		ID:                  ComponentEntropy,
		Files:               []string{"hexal/entropy.h", "hexal/entropy.c"},
		RuntimeDependencies: []DependencyID{DependencyLibuv},
		RequiredCHeaders:    []string{"stddef.h", "stdint.h"},
		ConditionalHeaders: []ConditionalHeaders{
			{Condition: HeaderConditionCorelibEntropy, Headers: []string{"stdlib.h", "string.h"}},
		},
	},
}

// Components returns a defensive copy of every component record.
func Components() []ComponentSpec {
	components := make([]ComponentSpec, 0, len(componentRegistry))
	for _, component := range componentRegistry {
		components = append(components, cloneComponentSpec(component))
	}
	return components
}

// Component returns a defensive copy of one component record.
func Component(id ComponentID) (ComponentSpec, bool) {
	for _, component := range componentRegistry {
		if component.ID == id {
			return cloneComponentSpec(component), true
		}
	}
	return ComponentSpec{}, false
}

// Dependencies returns a defensive copy of every dependency record.
func Dependencies() []DependencySpec {
	return append([]DependencySpec(nil), dependencyRegistry...)
}

// Dependency reports whether id names a declared native runtime input.
func Dependency(id DependencyID) (DependencySpec, bool) {
	for _, dependency := range dependencyRegistry {
		if dependency.ID == id {
			return dependency, true
		}
	}
	return DependencySpec{}, false
}

// FileOwner returns the component that owns one generated file path.
func FileOwner(file string) (ComponentID, bool) {
	for _, component := range componentRegistry {
		for _, owned := range component.Files {
			if owned == file {
				return component.ID, true
			}
		}
	}
	return "", false
}

// cloneComponentSpec copies every slice so a caller cannot mutate the registry.
func cloneComponentSpec(component ComponentSpec) ComponentSpec {
	clone := component
	clone.Files = append([]string(nil), component.Files...)
	clone.RuntimeDependencies = append([]DependencyID(nil), component.RuntimeDependencies...)
	clone.RequiredCHeaders = append([]string(nil), component.RequiredCHeaders...)
	clone.ConditionalHeaders = make([]ConditionalHeaders, len(component.ConditionalHeaders))
	for index, group := range component.ConditionalHeaders {
		clone.ConditionalHeaders[index] = ConditionalHeaders{
			Condition: group.Condition,
			Headers:   append([]string(nil), group.Headers...),
		}
	}
	return clone
}

// knownHeaderCondition reports whether condition is one of the registry's
// declared header conditions.
func knownHeaderCondition(condition HeaderCondition) bool {
	switch condition {
	case HeaderConditionEqualityAborting, HeaderConditionConcurrencyAtomic,
		HeaderConditionConversionFloat, HeaderConditionBitCast,
		HeaderConditionCorelibPaths, HeaderConditionCorelibArguments,
		HeaderConditionCorelibEntropy:
		return true
	}
	return false
}

// validateComponents reports the first inconsistency in the component and
// dependency registries: an empty or repeated identity, a file claimed by two
// components, a dependency no record declares, a dependency no component
// references, a repeated dependency or header inside one record, or an unknown
// or repeated header condition.
func validateComponents() error {
	ids := make(map[ComponentID]bool, len(componentRegistry))
	owners := make(map[string]ComponentID)
	referenced := make(map[DependencyID]bool)
	for _, component := range componentRegistry {
		if component.ID == "" {
			return fmt.Errorf("empty component id")
		}
		if ids[component.ID] {
			return fmt.Errorf("duplicate component %s", component.ID)
		}
		ids[component.ID] = true
		if len(component.Files) == 0 {
			return fmt.Errorf("no files %s", component.ID)
		}
		for _, file := range component.Files {
			if file == "" {
				return fmt.Errorf("empty file %s", component.ID)
			}
			if owner, exists := owners[file]; exists {
				return fmt.Errorf("file owner %s %s", owner, component.ID)
			}
			owners[file] = component.ID
		}
		seenDependencies := make(map[DependencyID]bool, len(component.RuntimeDependencies))
		for _, dependency := range component.RuntimeDependencies {
			if _, declared := Dependency(dependency); !declared {
				return fmt.Errorf("unknown dependency %s", dependency)
			}
			if seenDependencies[dependency] {
				return fmt.Errorf("duplicate dependency %s", dependency)
			}
			seenDependencies[dependency] = true
			referenced[dependency] = true
		}
		seenHeaders := make(map[string]bool, len(component.RequiredCHeaders))
		for _, header := range component.RequiredCHeaders {
			if header == "" {
				return fmt.Errorf("empty header %s", component.ID)
			}
			if seenHeaders[header] {
				return fmt.Errorf("duplicate header %s", header)
			}
			seenHeaders[header] = true
		}
		seenConditions := make(map[HeaderCondition]bool, len(component.ConditionalHeaders))
		for _, group := range component.ConditionalHeaders {
			if !knownHeaderCondition(group.Condition) {
				return fmt.Errorf("unknown condition %s", string(component.ID)+":"+string(group.Condition))
			}
			if seenConditions[group.Condition] {
				return fmt.Errorf("repeat condition %s", string(component.ID)+":"+string(group.Condition))
			}
			seenConditions[group.Condition] = true
			if len(group.Headers) == 0 {
				return fmt.Errorf("empty condition %s", string(component.ID)+":"+string(group.Condition))
			}
			groupHeaders := make(map[string]bool, len(group.Headers))
			for _, header := range group.Headers {
				if header == "" {
					return fmt.Errorf("empty header %s", string(component.ID)+":"+string(group.Condition))
				}
				if groupHeaders[header] {
					return fmt.Errorf("repeat header %s", header)
				}
				groupHeaders[header] = true
				// A header may appear in more than one conditional group
				// because a program activates several at once and the set is
				// a union; only the unconditional set may not repeat it.
				if seenHeaders[header] {
					return fmt.Errorf("duplicate header %s", header)
				}
			}
		}
	}
	seenIDs := make(map[DependencyID]bool, len(dependencyRegistry))
	for _, dependency := range dependencyRegistry {
		if dependency.ID == "" {
			return fmt.Errorf("empty dependency id")
		}
		if seenIDs[dependency.ID] {
			return fmt.Errorf("duplicate dependency %s", dependency.ID)
		}
		seenIDs[dependency.ID] = true
		if !referenced[dependency.ID] {
			return fmt.Errorf("unused dependency %s", dependency.ID)
		}
	}
	return nil
}
