// header_requirements.go owns C header requirement analysis: which
// standard headers the merged program's helpers and types need, and the
// predicates that decide each family.
package generator

import (
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// computeHeaderRequirements builds the program-wide standard-header and
// hex_eos requirement set from every reachable module's written types and
// selected helper families. hexal.h is included by every module header, so
// the set is the union over all modules; each family contributes the
// standard headers that own the declarations and macros its generated
// helpers actually call. Requirements are discovered from checked types and
// selection state, never by searching rendered C text.
func computeHeaderRequirements(merged *programEmission, modules []*moduleEmission) (*cHeaderRequirements, error) {
	requirements := &cHeaderRequirements{}
	for _, module := range modules {
		if err := collectModuleRequirements(module, requirements); err != nil {
			return nil, err
		}
	}
	if merged.wrapState != nil && len(merged.wrapState.order) > 0 {
		// The signed wrapping helpers adapt ckd_* result-pointer macros.
		if err := requirements.addComponentHeaders(specdata.ComponentWrap); err != nil {
			return nil, err
		}
	}
	if libuvSelected(merged) {
		// The native bootstrap traps when allocator installation fails.
		requirements.native = true
		requirements.trap = true
	}
	if requirements.trap {
		// The one program-wide trap declaration and root definition own
		// <stdio.h>/<stdlib.h>; families that only call it do not claim
		// those headers independently.
		if err := requirements.addComponentHeaders(specdata.ComponentRuntime); err != nil {
			return nil, err
		}
	}
	if len(merged.sizeLiterals) > 0 {
		// The retained SIZE_MAX static_assert is the one source-dependent
		// target probe; it needs size_t and SIZE_MAX themselves. No component
		// record owns this set: a size literal is a program-wide target probe,
		// not a runtime component's code.
		requirements.add("stddef.h", "stdint.h")
	}
	return requirements, nil
}

// equalityStateUsed reports whether any equality helper is emitted.
func equalityStateUsed(state *generatedEqualityState) bool {
	return state != nil && len(state.order) > 0
}

// abortingEqualityUsed reports whether any emitted equality helper is a
// union or ADT equality: both switch over the program-wide hex_tag enum and
// carry a default: abort() catch-all, since a switch exhaustive over one
// type's own tags still omits every other reachable type's tags as far as
// -Wswitch can tell.
func abortingEqualityUsed(state *generatedEqualityState) bool {
	if state == nil {
		return false
	}
	for _, typ := range state.order {
		if typ.Union != nil && unionSupportsEquality(typ) {
			return true
		}
		if typ.Adt != nil {
			return true
		}
	}
	return false
}

// conversionUsesMath reports whether any checked conversion classifies a
// float through <math.h>: float-to-integer helpers call isnan/isinf/trunc/
// truncf and Float64-to-Float32 calls isfinite. Integer-source helpers never
// do. The set holds only checked pairs, so a float source is the exact
// condition.
func conversionUsesMath(specs []conversionSpec) bool {
	for _, spec := range specs {
		if compilerTypes.IsFloat(spec.source) {
			return true
		}
	}
	return false
}

func collectModuleRequirements(module *moduleEmission, requirements *cHeaderRequirements) error {
	if err := collectTypeRequirements(module.program, requirements); err != nil {
		return err
	}
	if module.heapState.selected() {
		// The Heap operations use the selected allocator, size_t from
		// <stddef.h>, and the ckd_mul checked arithmetic
		// from <stdckdint.h>. Diagnostic traps report through the one
		// program-wide hex_runtime_trap.
		if err := requirements.addComponentHeaders(specdata.ComponentHeap); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.sliceState != nil && len(module.sliceState.slices) > 0 {
		// Slice structs carry size_t; bounds guards trap.
		if err := requirements.addComponentHeaders(specdata.ComponentSlice); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.arrayState != nil && len(module.arrayState.order) > 0 {
		// Array accessors use UINT64_C bounds and trap.
		if err := requirements.addComponentHeaders(specdata.ComponentArray); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.stringUsed {
		// hex_string storage, the inline text structs, and the UTF-8 validator use
		// uint8_t, size_t, free, ckd_add, and memcpy (<string.h>);
		// diagnostic traps report through hex_runtime_trap.
		if err := requirements.addComponentHeaders(specdata.ComponentString); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.listState != nil && len(module.listState.order) > 0 {
		// List headers carry size_t/uintptr_t, grow with ckd_mul, copy
		// the initialized prefix with memcpy, and trap.
		if err := requirements.addComponentHeaders(specdata.ComponentList); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.dictState != nil && len(module.dictState.order) > 0 {
		// Dict headers carry size_t/uintptr_t, grow with ckd_mul,
		// probe text keys with memcmp through the shared equality
		// helper (<string.h>), and trap.
		if err := requirements.addComponentHeaders(specdata.ComponentDict); err != nil {
			return err
		}
		requirements.trap = true
	}
	if equalityStateUsed(module.equalityState) {
		// Byte-compare helpers use memcmp over size_t lengths; union
		// equality helpers abort directly on an impossible tag.
		if err := requirements.addComponentHeaders(specdata.ComponentEquality); err != nil {
			return err
		}
		if err := requirements.addConditionalComponentHeaders(specdata.ComponentEquality, specdata.HeaderConditionEqualityAborting, abortingEqualityUsed(module.equalityState)); err != nil {
			return err
		}
	}
	if len(module.conversionSpecs) > 0 {
		// Checked conversion helpers use the exact-width limit macros
		// from <stdint.h> and call the one program-wide
		// hex_runtime_trap; float-source helpers additionally use
		// isnan/isinf/trunc/truncf from <math.h>. The set holds only
		// checked pairs: a program with only direct or
		// identity conversions selects no conversion trap or headers.
		if err := requirements.addComponentHeaders(specdata.ComponentNumeric); err != nil {
			return err
		}
		requirements.trap = true
		if err := requirements.addConditionalComponentHeaders(specdata.ComponentNumeric, specdata.HeaderConditionConversionFloat, conversionUsesMath(module.conversionSpecs)); err != nil {
			return err
		}
	}
	if len(module.divisionTypes) > 0 {
		// Division guards trap.
		requirements.trap = true
	}
	if len(module.shiftSpecs) > 0 {
		// Shift guards trap.
		requirements.trap = true
	}
	if len(module.bitCastSpecs) > 0 {
		// bit_cast helpers reinterpret through memcpy.
		if err := requirements.addConditionalComponentHeaders(specdata.ComponentNumeric, specdata.HeaderConditionBitCast, true); err != nil {
			return err
		}
	}
	if module.printState != nil && module.printState.used {
		// The print family formats with PRI* macros (<inttypes.h>) and
		// snprintf, classifies floats through <math.h>, and uses
		// uint8_t/size_t/bool; its write-failure trap reports through the
		// shared hex_runtime_trap.
		if err := requirements.addComponentHeaders(specdata.ComponentPrint); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.interpolationUsed {
		// String.interpolate's scalar formatters use the identical
		// PRI* macros, snprintf, and <math.h> float classification as
		// print, plus the checked hex_string allocation path. No component
		// record owns this set: interpolation is a String sub-feature
		// whose formatter is emitted into the string component, and its
		// header set is deliberately not the string record's.
		requirements.add("stdckdint.h", "stddef.h", "stdint.h", "stdio.h", "inttypes.h", "math.h", "string.h")
		requirements.trap = true
	}
	if module.ioState != nil && module.ioState.used {
		// The stream core spells descriptors as intptr_t and transfers
		// uint8_t bytes, reserves destination capacity with ckd_add, and
		// traps on borrowed closes; the memory backend interval-checks
		// flat uintptr_t addresses. Direct selection, not reliance on
		// the forced List specialization, keeps the demand honest.
		if err := requirements.addComponentHeaders(specdata.ComponentIO); err != nil {
			return err
		}
		requirements.trap = true
	}
	concurrency := module.concurrencyState
	if concurrency != nil && (concurrency.used || len(concurrency.atomics) > 0) {
		if concurrency.used {
			// The scheduler runtime (root C) and the
			// spawn/channel/mutex inline helpers use size_t, SIZE_MAX,
			// int64_t/uint8_t, allocator operations, and the shared trap;
			// Channel slot sizing uses ckd_mul. The receive machinery
			// represents completion as hex_eos for every channel.
			if err := requirements.addComponentHeaders(specdata.ComponentConcurrency); err != nil {
				return err
			}
			requirements.trap = true
			if len(concurrency.channels) > 0 {
				requirements.eos = true
			}
		}
		if len(concurrency.atomics) > 0 {
			// Atomic handle typedefs and the scheduler's shutdown flag
			// use _Atomic from <stdatomic.h>. An atomic-only program
			// links no scheduler runtime, so the portable headers above
			// are not required by this family alone.
			if err := requirements.addConditionalComponentHeaders(specdata.ComponentConcurrency, specdata.HeaderConditionConcurrencyAtomic, true); err != nil {
				return err
			}
		}
	}
	if module.fileState != nil && module.fileState.used {
		// The File core spells descriptors as intptr_t, reserves list
		// capacity with ckd_add, and traps on an unrepresentable size.
		if err := requirements.addComponentHeaders(specdata.ComponentFile); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.networkState != nil && module.networkState.used {
		// The network core spells ports and scopes as uint16_t/uint32_t,
		// reserves list capacity with ckd_add, and traps on an
		// unrepresentable size or a missing Task.
		if err := requirements.addComponentHeaders(specdata.ComponentNetwork); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.processState != nil && module.processState.operations {
		// The process core snapshots argv/envp sizes with ckd_add,
		// spells stream selectors as uint8_t, and traps on an
		// unrepresentable size or a missing Task.
		if err := requirements.addComponentHeaders(specdata.ComponentProcess); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.signalState != nil && module.signalState.operations {
		// The signal core spells raw subscription values as uint8_t and
		// traps on a missing Task.
		if err := requirements.addComponentHeaders(specdata.ComponentSignal); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.terminalState != nil && module.terminalState.used {
		// TerminalSize spells its two fields as size_t. Neither query
		// traps: every failure is a structured Error, never an internal
		// invariant violation.
		if err := requirements.addComponentHeaders(specdata.ComponentTerminal); err != nil {
			return err
		}
	}
	if module.corelibState != nil && module.corelibState.used {
		// The program/entropy runtimes spell uint8_t/size_t and report
		// through the shared trap and the common libuv mapper; the
		// libuv-backed path functions add checked size arithmetic,
		// malloc/free, and memcpy/strlen. The base headers are the
		// selected components' unconditional records; the path, argument,
		// and entropy contributions are their named conditional groups.
		if module.corelibState.program {
			if err := requirements.addComponentHeaders(specdata.ComponentProgram); err != nil {
				return err
			}
		}
		if module.corelibState.entropy {
			if err := requirements.addComponentHeaders(specdata.ComponentEntropy); err != nil {
				return err
			}
		}
		requirements.trap = true
		if module.corelibState.paths {
			if err := requirements.addConditionalComponentHeaders(specdata.ComponentProgram, specdata.HeaderConditionCorelibPaths, true); err != nil {
				return err
			}
		}
		if module.corelibState.entropy {
			if err := requirements.addConditionalComponentHeaders(specdata.ComponentEntropy, specdata.HeaderConditionCorelibEntropy, true); err != nil {
				return err
			}
		}
		if module.corelibState.arguments {
			if err := requirements.addConditionalComponentHeaders(specdata.ComponentProgram, specdata.HeaderConditionCorelibArguments, true); err != nil {
				return err
			}
		}
	}
	if module.timeState != nil && module.timeState.used {
		// Time values spell uint64_t/int64_t/uint32_t, and checked Duration
		// arithmetic and Instant subtraction trap.
		if err := requirements.addComponentHeaders(specdata.ComponentTime); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.stashState != nil && module.stashState.required {
		// The bump-allocation core sizes and grows blocks with ckd_add
		// and ckd_mul, spells sizes as size_t, and traps on an
		// unrepresentable size; hex_heap_allocate/hex_heap_free back
		// every block.
		if err := requirements.addComponentHeaders(specdata.ComponentStash); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.poolState != nil && len(module.poolState.order) > 0 {
		// Pool state sizes its slot/live/free-index storage with
		// ckd_mul, converts addresses through uintptr_t for the free
		// range check, and traps on exhaustion, an out-of-range or
		// not-live free, or a non-empty destroy.
		if err := requirements.addComponentHeaders(specdata.ComponentPool); err != nil {
			return err
		}
		requirements.trap = true
	}
	if module.unionState != nil {
		for _, union := range module.unionState.order {
			// Union truthiness, equality, and widening helpers abort
			// directly on an impossible tag; EoS members spell hex_eos
			// payloads. Nil members are tag-only and spell no C type.
			// No component record owns this set: a union is module-owned
			// and its helpers are emitted into the consuming module, not
			// a runtime component.
			requirements.add("stdlib.h")
			unionMembers := compilerTypes.UnionMembers(union)
			for index := 0; index < unionMembers.Len(); index++ {
				if member, _ := unionMembers.At(index); compilerTypes.IsEoS(member) {
					requirements.eos = true
					requirements.add("stdint.h")
				}
			}
		}
	}
	return nil
}
