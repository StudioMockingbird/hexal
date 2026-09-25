// merge.go owns program-wide merging: the programEmission record, module
// merging, and the per-family and route merge helpers it drives.
package generator

import (
	"slices"
	"strings"

	compilerTypes "hexal/compiler/types"
)

// programEmission is the program-wide aggregate of every reachable module's
// built-in machinery. hexal.h is generated from it, so the once-per-process
// runtime cores and the shared type and literal definitions cover every
// module, not just the entrypoint's discovery: built-in specialization
// identity depends only on the constructor and its canonical arguments,
// never on the requesting module.
type programEmission struct {
	errorUsed        bool
	printUsed        bool
	heapState        *heapHelpers
	sliceState       *generatedSliceState
	stringState      *literalRegistry
	listState        *generatedListState
	dictState        *generatedDictState
	arrayState       *generatedArrayState
	concurrencyState *generatedConcurrencyState
	wrapState        *generatedWrapState
	sizeLiterals     []string
	conversionSpecs  []conversionSpec
	divisionTypes    []compilerTypes.Type
	shiftSpecs       []shiftSpec
	bitCastSpecs     []bitCastSpec
	endianSpecs      []endianSpec
	// equalityNeed is true when any module's equality state requires the
	// shared String equality helper.
	equalityNeed bool
	// orderingNeed is true when any module's equality state requires the
	// shared String ordering helper.
	orderingNeed bool
	// hashNeed is true when any module has a Dict keyed by inline text,
	// selecting the shared text hash and equality helpers.
	hashNeed bool
	// interpolationNeed is true when any module contains a checked
	// String.interpolate call, selecting the String component's
	// demand-driven formatter helpers.
	interpolationNeed bool
	// validatorNeed is true when any module constructs text from bytes or
	// concatenates text, the only operations that call the UTF-8 validator.
	// It selects the validator's declaration, definition, and the private
	// utf8proc include.
	validatorNeed bool
	// runeLengthNeed is true when any module counts text scalars, selecting
	// the utf8proc-stepping length helper. It also raises validatorNeed, since
	// the helper and the validator share the private utf8proc include.
	runeLengthNeed bool
	// runeIterationNeed is true when any module steps through text by scalar:
	// Rune iteration and RuneCursor both use the shared utf8proc decode step.
	// It also raises validatorNeed, since both share the private utf8proc
	// include.
	runeIterationNeed bool
	// byteCursorNeed is true when any module scans text with a ByteCursor,
	// selecting the cursor descriptor and its index-arithmetic helpers. Byte
	// stepping needs no utf8proc.
	byteCursorNeed bool
	// runeCursorNeed is true when any module scans text with a RuneCursor,
	// selecting the cursor descriptor and its utf8proc-stepping helpers.
	runeCursorNeed bool
	// runeEncodeNeed is true when any module encodes a scalar sequence into
	// text, selecting the utf8proc-encoding constructor.
	runeEncodeNeed bool
	// runePropertiesNeed is true when any module reads a Tier 2 Rune property,
	// selecting the utf8proc property helpers.
	runePropertiesNeed bool
	// runeCategoryNeed is true when any module reads a scalar's general
	// category, selecting the utf8proc category helper and the shared
	// UnicodeCategory tag registration.
	runeCategoryNeed bool
	// graphemeLengthNeed is true when any module counts text grapheme
	// clusters, selecting the utf8proc break-state helper.
	graphemeLengthNeed bool
	// graphemeNeed is true when any module uses the Grapheme type or its
	// cursor, selecting the borrowed-range struct and the stateful segmenter.
	graphemeNeed bool
	// casefoldNeed is true when any module folds text, selecting the
	// utf8proc-mapped transform.
	casefoldNeed bool
	// normalizeNeed is true when any module normalizes text, selecting the
	// utf8proc-mapped transform and the shared NormalizationForm tags.
	normalizeNeed bool
	// equalityTypes collects the program-owned types needing equality
	// helpers, merged from every module's equality state for the component
	// builder.
	equalityTypes []compilerTypes.Type
	// ioState merges every module's stream families; selecting IO, Bytes, or
	// print emits the component pair once program-wide.
	ioState *generatedStreamState
	// timeState merges every module's time demand; any time type or
	// operation emits hexal/time.h and hexal/time.c once program-wide.
	timeState *generatedTimeState
	// fileState merges every module's File demand; File emits hexal/file.h and
	// hexal/file.c once program-wide.
	fileState *generatedFileState
	// networkState merges every module's Address/Dns/Tcp demand; reachable
	// use emits hexal/network.h and hexal/network.c once program-wide.
	networkState *generatedNetworkState
	// processState merges every module's Process/Pipe/ProcessOptions demand;
	// reachable use emits hexal/process.h and hexal/process.c once
	// program-wide.
	processState *generatedProcessState
	// signalState merges every module's Signal/Signals demand; reachable use
	// emits hexal/signal.h and hexal/signal.c once program-wide.
	signalState *generatedSignalState
	// terminalState merges every module's TerminalSize/Terminal demand;
	// reachable use emits hexal/terminal.h and hexal/terminal.c once
	// program-wide.
	terminalState *generatedTerminalState
	// corelibState merges every module's core-library module demand;
	// reachable use emits hexal/program.h/.c and hexal/entropy.h/.c once
	// program-wide.
	corelibState *generatedCorelibState
	// seekUsed is true when any module's stream state reaches Bytes.seek or
	// IO.seek, selecting hexal/seek.h once program-wide. It is tracked
	// separately from ioState's own four merged flags, which exist only for
	// the event bridge's native-descriptor demand fact and deliberately
	// exclude seekBytes (an in-memory seek never blocks).
	seekUsed bool
	// stashUsed is true when any module constructs or operates on a Stash,
	// selecting the shared type-erased hexal/stash.h/.c core once
	// program-wide.
	stashUsed bool
	// poolState merges every module's reachable Pool<T> specializations,
	// each fully monomorphized like List<T> (see pool_component.go).
	poolState *generatedPoolState
	// requirements is the demand-driven standard-header and hex_eos set built
	// from every reachable module's checked types and selected helper
	// families.
	requirements *cHeaderRequirements
	// tags is the program-wide discriminant registry, finalized before any
	// file text renders; hexal.h carries the enum, and every tag spelling in
	// module headers and bodies resolves through it.
	tags *tagRegistry
	// adapterSites routes every spawn site to the canonical id of the module
	// that owns the spawned function, so the entry adapter is emitted beside
	// the function definition it calls (the adapter never leaves its
	// function's translation unit).
	adapterSites map[string][]spawnSite
	// foreignIndex resolves the defining header of every foreign declaration
	// in the program. It is nil when no module declares a foreign block, so
	// foreign include discovery stays a no-op for every ordinary program.
	foreignIndex *foreignIndex
}

// mergeProgramEmission folds the per-module discovery results into one
// program-wide aggregate. Every merge is deterministic: modules contribute
// in the dependency-first order slice, deduplicated collections are keyed by
// canonical identity, and slice order is preserved or re-sorted explicitly.
func mergeProgramEmission(modules []*moduleEmission, literals *literalRegistry) (*programEmission, error) {
	merged := &programEmission{
		heapState:   &heapHelpers{seen: make(map[string]bool), alignedSeen: make(map[string]bool)},
		sliceState:  &generatedSliceState{seen: make(map[*compilerTypes.SliceInfo]bool)},
		stringState: literals,
		listState:   &generatedListState{seen: make(map[*compilerTypes.ListInfo]bool)},
		dictState:   &generatedDictState{seen: make(map[*compilerTypes.DictInfo]bool)},
		poolState:   &generatedPoolState{seen: make(map[*compilerTypes.PoolInfo]bool)},
		arrayState:  &generatedArrayState{seen: make(map[*compilerTypes.ArrayInfo]bool), demand: make(map[*compilerTypes.ArrayInfo]arrayAccessorDemand)},
		concurrencyState: &generatedConcurrencyState{
			taskTypes:            make(map[string]compilerTypes.Type),
			joinTypes:            make(map[string]compilerTypes.Type),
			channels:             make(map[string]compilerTypes.Type),
			atomics:              make(map[string]compilerTypes.Type),
			channelNewUnions:     make(map[string]compilerTypes.Type),
			channelSendUnions:    make(map[string]compilerTypes.Type),
			channelReceiveUnions: make(map[string]compilerTypes.Type),
		},
		wrapState:     &generatedWrapState{seen: make(map[string]bool)},
		ioState:       &generatedStreamState{},
		timeState:     &generatedTimeState{},
		fileState:     &generatedFileState{},
		networkState:  &generatedNetworkState{},
		processState:  &generatedProcessState{},
		signalState:   &generatedSignalState{},
		terminalState: &generatedTerminalState{},
		corelibState:  &generatedCorelibState{},
		adapterSites:  make(map[string][]spawnSite),
	}
	viewOrders := make([][]compilerTypes.Type, 0, len(modules))
	arrayOrders := make([][]compilerTypes.Type, 0, len(modules))
	listOrders := make([][]compilerTypes.Type, 0, len(modules))
	dictOrders := make([][]compilerTypes.Type, 0, len(modules))
	poolOrders := make([][]compilerTypes.Type, 0, len(modules))
	unionOrders := make([][]compilerTypes.Type, 0, len(modules))
	adtOrders := make([][]compilerTypes.Type, 0, len(modules))
	sizeSeen := make(map[string]bool)
	spawnedSites := make(map[string]bool)
	for _, module := range modules {
		merged.errorUsed = merged.errorUsed || module.errorUsed
		if module.equalityState != nil {
			merged.equalityNeed = merged.equalityNeed || module.equalityState.needString
			merged.orderingNeed = merged.orderingNeed || module.equalityState.compareNeed
		}
		merged.interpolationNeed = merged.interpolationNeed || module.interpolationUsed
		if module.textState != nil {
			merged.validatorNeed = merged.validatorNeed || module.textState.used || module.textState.runeLength || module.textState.runeIteration || module.textState.runeCursor || len(module.textState.heapFromRunes) > 0 || module.textState.runeProperties || module.textState.runeCategory || module.textState.graphemeLength || module.textState.graphemeCursor || len(module.textState.casefold) > 0 || len(module.textState.normalize) > 0
			merged.runeLengthNeed = merged.runeLengthNeed || module.textState.runeLength
			merged.runeIterationNeed = merged.runeIterationNeed || module.textState.runeIteration || module.textState.runeCursor
			merged.byteCursorNeed = merged.byteCursorNeed || module.textState.byteCursor
			merged.runeCursorNeed = merged.runeCursorNeed || module.textState.runeCursor
			merged.runeEncodeNeed = merged.runeEncodeNeed || len(module.textState.heapFromRunes) > 0
			merged.runePropertiesNeed = merged.runePropertiesNeed || module.textState.runeProperties
			merged.runeCategoryNeed = merged.runeCategoryNeed || module.textState.runeCategory
			merged.graphemeLengthNeed = merged.graphemeLengthNeed || module.textState.graphemeLength
			merged.graphemeNeed = merged.graphemeNeed || module.textState.graphemeCursor
			merged.casefoldNeed = merged.casefoldNeed || len(module.textState.casefold) > 0
			merged.normalizeNeed = merged.normalizeNeed || len(module.textState.normalize) > 0
		}
		if module.dictState != nil {
			for _, dict := range module.dictState.order {
				if compilerTypes.IsText(dict.Dict.Key) {
					// A text key is probed by the shared hash and the shared
					// equality helper, so both are selected with it.
					merged.hashNeed = true
					merged.equalityNeed = true
				}
			}
		}
		if module.printState != nil && module.printState.used {
			merged.printUsed = true
		}
		if module.ioState != nil && module.ioState.used {
			merged.ioState.used = true
			// The event bridge's demand fact (concurrencyComponents) needs
			// exactly these four operation flags program-wide; every other
			// generatedStreamState field stays module-local, discovered
			// directly from each module's own rendering pass.
			merged.ioState.readIO = merged.ioState.readIO || module.ioState.readIO
			merged.ioState.writeIO = merged.ioState.writeIO || module.ioState.writeIO
			merged.ioState.seekIO = merged.ioState.seekIO || module.ioState.seekIO
			merged.ioState.closeIO = merged.ioState.closeIO || module.ioState.closeIO
			merged.seekUsed = merged.seekUsed || module.ioState.seekIO || module.ioState.seekBytes
		}
		mergeTimeInto(merged.timeState, module.timeState)
		mergeFileInto(merged.fileState, module.fileState)
		mergeNetworkInto(merged.networkState, module.networkState)
		mergeProcessInto(merged.processState, module.processState)
		mergeSignalInto(merged.signalState, module.signalState)
		mergeTerminalInto(merged.terminalState, module.terminalState)
		mergeCorelibInto(merged.corelibState, module.corelibState)
		merged.seekUsed = merged.seekUsed || module.fileState != nil && module.fileState.seek
		mergeHeapInto(merged.heapState, module.heapState)
		mergeConcurrencyInto(merged.concurrencyState, module.concurrencyState, spawnedSites)
		mergeWrapState(merged.wrapState, module.wrapState)
		if module.unionState != nil {
			unionOrders = append(unionOrders, module.unionState.order)
		}
		if module.adtState != nil {
			adtOrders = append(adtOrders, module.adtState.order)
		}
		if module.arrayState != nil {
			arrayOrders = append(arrayOrders, module.arrayState.order)
			// Accessor demand is recorded while a module body renders, which
			// happens after this merge. Every module state therefore shares
			// the merged map, so a write from any renderer is visible to the
			// component builders that run once every module has rendered.
			module.arrayState.demand = merged.arrayState.demand
		}
		if module.sliceState != nil {
			viewOrders = append(viewOrders, module.sliceState.slices)
			merged.sliceState.required = merged.sliceState.required || module.sliceState.required
		}
		if module.listState != nil {
			listOrders = append(listOrders, module.listState.order)
		}
		if module.dictState != nil {
			dictOrders = append(dictOrders, module.dictState.order)
		}
		if module.poolState != nil {
			poolOrders = append(poolOrders, module.poolState.order)
		}
		if module.stashState != nil && module.stashState.required {
			merged.stashUsed = true
		}
		for _, digits := range module.sizeLiterals {
			if !sizeSeen[digits] {
				sizeSeen[digits] = true
				merged.sizeLiterals = append(merged.sizeLiterals, digits)
			}
		}
		mergeNumericSpecs(merged, module)
		mergeEqualityTypes(merged, module)
	}
	if merged.networkState != nil && (merged.networkState.dns || merged.networkState.tcp) {
		// DNS and TCP have no synchronous fallback path: every reachable
		// operation parks, so the scheduler bootstrap is required even
		// without an explicit Task, Channel, Mutex, or spawn elsewhere.
		merged.concurrencyState.used = true
	}
	if merged.processState != nil && merged.processState.operations {
		// Every Process/Pipe operation parks or spawns a native resource;
		// merely constructing or inspecting ProcessOptions and its sibling
		// inline types selects none of this.
		merged.concurrencyState.used = true
	}
	if merged.signalState != nil && merged.signalState.operations {
		// Every Signals construction or method parks or registers a native
		// watcher; merely constructing or matching a Signal value selects
		// none of this.
		merged.concurrencyState.used = true
	}
	if libuvSelected(merged) {
		// The native bootstrap installs mimalloc as libuv's allocator.
		merged.heapState.required = true
	}
	if merged.concurrencyState.used {
		// Scheduler-owned control blocks use the same allocator boundary as
		// every other Hexal-owned dynamic allocation.
		merged.heapState.required = true
	}
	sortMergedNumericSpecs(merged)
	sortMergedEqualityTypes(merged)
	merged.sliceState.slices = mergeTypeOrders(viewOrders)
	merged.arrayState.order = mergeTypeOrders(arrayOrders)
	merged.listState.order = mergeTypeOrders(listOrders)
	merged.dictState.order = mergeTypeOrders(dictOrders)
	merged.poolState.order = mergeTypeOrders(poolOrders)
	merged.adapterSites = routeSpawnSites(merged.concurrencyState)
	// The discriminant registry finalizes before any file text renders, from
	// the program-wide union and ADT reachability.
	merged.tags = buildTagRegistry(unionOrders, adtOrders)
	// The standard-header and hex_eos requirements aggregate after every
	// family state is merged, so the umbrella set covers the complete
	// reachable generated program.
	requirements, requirementsErr := computeHeaderRequirements(merged, modules)
	if requirementsErr != nil {
		return nil, requirementsErr
	}
	merged.requirements = requirements
	return merged, nil
}

// mergeTypeOrders unions per-module specialization lists of one built-in
// family by canonical C name and sorts the result, so identical canonical
// types from different modules are one specialization: built-in
// specialization identity depends only on the constructor and its canonical
// arguments.
func mergeTypeOrders(orders [][]compilerTypes.Type) []compilerTypes.Type {
	merged := make([]compilerTypes.Type, 0)
	seen := make(map[string]bool)
	for _, order := range orders {
		for _, typ := range order {
			if !seen[typ.CName] {
				seen[typ.CName] = true
				merged = append(merged, typ)
			}
		}
	}
	slices.SortStableFunc(merged, func(left, right compilerTypes.Type) int {
		return strings.Compare(left.CName, right.CName)
	})
	return merged
}

// mergeHeapInto unions one module's typed heap allocations into the
// program-wide helper set, preserving each module's discovery order.
func mergeHeapInto(merged, state *heapHelpers) {
	if state == nil {
		return
	}
	if state.required {
		merged.required = true
	}
	for _, element := range state.elements {
		if !merged.seen[element.Name] {
			merged.seen[element.Name] = true
			merged.elements = append(merged.elements, element)
		}
	}
	if merged.alignedSeen == nil {
		merged.alignedSeen = make(map[string]bool)
	}
	for _, element := range state.alignedElements {
		if !merged.alignedSeen[element.Name] {
			merged.alignedSeen[element.Name] = true
			merged.alignedElements = append(merged.alignedElements, element)
		}
	}
}

// mergeConcurrencyInto unions one module's concurrency machinery into the
// program-wide state: operation flags OR, per-element tables union by C
// name, and spawn sites concatenate in module order deduplicated by entry
// symbol (one adapter per spawned function).
func mergeConcurrencyInto(merged, state *generatedConcurrencyState, spawnedSites map[string]bool) {
	if state == nil {
		return
	}
	if state.used {
		merged.used = true
	}
	merged.detach = merged.detach || state.detach
	merged.yield = merged.yield || state.yield
	merged.mutexNew = merged.mutexNew || state.mutexNew
	merged.mutexLock = merged.mutexLock || state.mutexLock
	merged.mutexUnlock = merged.mutexUnlock || state.mutexUnlock
	merged.mutexFree = merged.mutexFree || state.mutexFree
	merged.spawnFail = merged.spawnFail || state.spawnFail
	merged.channelNew = merged.channelNew || state.channelNew
	merged.channelSend = merged.channelSend || state.channelSend
	merged.mutexCreate = merged.mutexCreate || state.mutexCreate
	mergeTypeMap(merged.taskTypes, state.taskTypes)
	mergeTypeMap(merged.joinTypes, state.joinTypes)
	mergeTypeMap(merged.channels, state.channels)
	mergeTypeMap(merged.atomics, state.atomics)
	mergeTypeMap(merged.channelNewUnions, state.channelNewUnions)
	mergeTypeMap(merged.channelSendUnions, state.channelSendUnions)
	mergeTypeMap(merged.channelReceiveUnions, state.channelReceiveUnions)
	if merged.mutexNewUnion == (compilerTypes.Type{}) {
		merged.mutexNewUnion = state.mutexNewUnion
	}
	for _, site := range state.spawns {
		if !spawnedSites[site.key()] {
			spawnedSites[site.key()] = true
			merged.spawns = append(merged.spawns, site)
		}
	}
}

// mergeTypeMap unions one per-element C-name table into the program-wide
// table; equal canonical types carry equal C names, so the union by C name is
// a union by identity.
func mergeTypeMap(merged, state map[string]compilerTypes.Type) {
	for name, typ := range state {
		if _, exists := merged[name]; !exists {
			merged[name] = typ
		}
	}
}

// routeSpawnSites assigns every program-wide spawn site to the canonical id
// of the module that owns the spawned function, deduplicated by entry
// symbol. emitModulePair places each module's adapters after its function
// definitions, so an adapter always calls its function within one
// translation unit.
func routeSpawnSites(merged *generatedConcurrencyState) map[string][]spawnSite {
	routed := make(map[string][]spawnSite)
	seen := make(map[string]bool)
	for _, site := range merged.spawns {
		if site.module == "" || seen[site.key()] {
			continue
		}
		seen[site.key()] = true
		routed[site.module] = append(routed[site.module], site)
	}
	return routed
}
