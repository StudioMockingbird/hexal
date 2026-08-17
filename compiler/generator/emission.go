// emission.go owns module/program emission: per-module discovery state,
// program-wide state merging, module and main C/header pair emission, and
// header assembly.
package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

const hexalHeaderPrefix = "#ifndef HEXAL_H\n#define HEXAL_H\n\n"
const sourceFilename = "main.hex"

// cHeaderRequirements is the program-wide demand-driven standard-header set,
// EoS representation requirement, and runtime-trap requirement. Standard
// headers are rendered once, in lexical order, immediately after the HEXAL_H
// guard; the hex_eos typedef is rendered before any type that references it.
type cHeaderRequirements struct {
	headers map[string]bool
	eos     bool
	trap    bool
}

func (requirements *cHeaderRequirements) add(headers ...string) {
	if requirements.headers == nil {
		requirements.headers = make(map[string]bool)
	}
	for _, header := range headers {
		requirements.headers[header] = true
	}
}

// moduleEmission is one module's validated discovery result: every built-in
// machinery state its program needs, kept separate from emission so the
// program-wide aggregate can be computed before any file text is written.
// The per-module states still drive the module's own header content, which
// may repeat C-safe inline helpers across headers.
type moduleEmission struct {
	canonicalID string
	logicalKey  string
	program     checker.Program
	functions   map[string]compilerTypes.Type
	methods     map[string]checker.MethodDeclaration
	typeState   *generatedTypeValidation

	errorUsed        bool
	unionState       *generatedUnionState
	heapState        *heapHelpers
	adtState         *generatedAdtState
	arrayState       *generatedArrayState
	viewState        *generatedViewState
	stringState      *generatedStringState
	listState        *generatedListState
	dictState        *generatedDictState
	equalityState    *generatedEqualityState
	conversionSpecs  []conversionSpec
	sizeLiterals     []string
	divisionTypes    []compilerTypes.Type
	shiftSpecs       []shiftSpec
	bitCastSpecs     []bitCastSpec
	endianSpecs      []endianSpec
	printState       *generatedPrintState
	concurrencyState *generatedConcurrencyState
	wrapState        *generatedWrapState
	objects          []*compilerTypes.ObjectType
}

// discoverModuleEmission validates one module and runs every built-in
// discovery walk over it. canonicalID names the module ("graphics/shapes");
// logicalKey is its source-map filename for #line directives
// ("graphics/shapes.hex"). The module's own string literal registry is a
// contribution to the program-wide table merged before emission.
func discoverModuleEmission(program checker.Program, canonicalID, logicalKey string) (*moduleEmission, error) {
	owner := compilerTypes.EncodeModuleOwner(canonicalID)
	functions, functionErr := declaredFunctions(program)
	if functionErr != nil {
		return nil, functionErr
	}
	methods, methodErr := declaredMethods(program)
	if methodErr != nil {
		return nil, methodErr
	}
	emission := &moduleEmission{
		canonicalID: canonicalID,
		logicalKey:  logicalKey,
		program:     program,
		functions:   functions,
		methods:     methods,
	}
	// The literal registry is discovered before the preflight pass: the
	// preflight renders call statements to prove them renderable, and a
	// string-literal argument must resolve against the same registry the
	// emission pass uses. discoverGeneratedStrings never fails, so hoisting
	// it changes no diagnostic ordering.
	stringState, stringErr := discoverGeneratedStrings(program)
	if stringErr != nil {
		return nil, stringErr
	}
	emission.stringState = stringState
	if validationErr := validateCheckedProgram(program, functions, methods, stringState); validationErr != nil {
		return nil, validationErr
	}
	emission.errorUsed = discoverErrorUsed(program)
	objects, objectErr := objectDefinitions(program)
	if objectErr != nil {
		return nil, objectErr
	}
	emission.objects = objects
	unionState, unionErr := discoverGeneratedUnions(program)
	if unionErr != nil {
		return nil, unionErr
	}
	emission.unionState = unionState
	heapState, heapErr := discoverHeapHelpers(program)
	if heapErr != nil {
		return nil, heapErr
	}
	emission.heapState = heapState
	adtState, adtErr := discoverGeneratedADTs(program)
	if adtErr != nil {
		return nil, adtErr
	}
	emission.adtState = adtState
	arrayState, arrayErr := discoverGeneratedArrays(program)
	if arrayErr != nil {
		return nil, arrayErr
	}
	emission.arrayState = arrayState
	viewState, viewErr := discoverGeneratedViews(program)
	if viewErr != nil {
		return nil, viewErr
	}
	emission.viewState = viewState
	listState, listErr := discoverGeneratedLists(program)
	if listErr != nil {
		return nil, listErr
	}
	emission.listState = listState
	dictState, dictErr := discoverGeneratedDicts(program)
	if dictErr != nil {
		return nil, dictErr
	}
	emission.dictState = dictState
	equalityState, equalityErr := discoverEqualityTypes(program)
	if equalityErr != nil {
		return nil, equalityErr
	}
	emission.equalityState = equalityState
	conversionSpecs, sizeLiterals, conversionErr := discoverGeneratedConversions(program)
	if conversionErr != nil {
		return nil, conversionErr
	}
	emission.conversionSpecs = conversionSpecs
	emission.sizeLiterals = sizeLiterals
	emission.divisionTypes = discoverGeneratedDivisions(program)
	emission.shiftSpecs = discoverGeneratedShifts(program)
	emission.bitCastSpecs = discoverGeneratedBitCasts(program)
	emission.endianSpecs = discoverGeneratedEndian(program)
	emission.printState = discoverGeneratedPrint(program)
	emission.wrapState = discoverGeneratedWraps(program)
	concurrencyState, concurrencyErr := discoverGeneratedConcurrency(program, functions, stringState, canonicalID, owner)
	if concurrencyErr != nil {
		return nil, concurrencyErr
	}
	emission.concurrencyState = concurrencyState
	if concurrencyState.used {
		// The task runtime needs the String and Strand typedefs and the
		// Error object for the failure Errors every recoverable operation
		// constructs; the discovery pass registered the literals. The
		// Channel and Mutex helpers take a hex_heap argument, so the heap
		// machinery is required too.
		stringState.used = true
		stringState.needStrand = true
		heapState.required = true
	}
	if emission.errorUsed {
		// Error's representation names the String and Strand types, so the
		// string component is a required dependency of error.h.
		stringState.used = true
		stringState.needStrand = true
	}
	if len(dictState.order) > 0 {
		// The dict component header declares its String dependency, so the
		// string component must exist.
		stringState.used = true
	}
	if stringState.used {
		ensureViewUInt8(viewState)
		// The String helpers allocate through the heap machinery, and the
		// string component header declares its Heap and View dependencies.
		heapState.required = true
		viewState.required = true
	}
	if len(listState.order) > 0 || len(dictState.order) > 0 {
		// The List and Dict helpers allocate and trap through the heap
		// machinery and the shared runtime trap.
		heapState.required = true
	}
	if len(listState.order) > 0 || len(arrayState.order) > 0 {
		// The list and array component headers declare their View
		// dependency, so the view component must exist.
		viewState.required = true
	}
	emission.typeState = &generatedTypeValidation{declaredObjects: errorDeclaredObjects(program)}
	return emission, nil
}

// programEmission is the program-wide aggregate of every reachable module's
// built-in machinery. hexal.h is generated from it, so the once-per-process
// runtime cores and the shared type and literal definitions cover every
// module, not just the entrypoint's discovery: built-in specialization
// identity depends only on the constructor and its canonical arguments,
// never on the requesting module.
type programEmission struct {
	errorUsed        bool
	heapState        *heapHelpers
	viewState        *generatedViewState
	stringState      *generatedStringState
	listState        *generatedListState
	dictState        *generatedDictState
	arrayState       *generatedArrayState
	concurrencyState *generatedConcurrencyState
	wrapState        *generatedWrapState
	sizeLiterals     []string
	// requirements is the demand-driven standard-header and hex_eos set built
	// from every reachable module's checked types and selected helper
	// families.
	requirements *cHeaderRequirements
	// adapterSites routes every spawn site to the canonical id of the module
	// that owns the spawned function, so the entry adapter is emitted beside
	// the function definition it calls (the adapter never leaves its
	// function's translation unit).
	adapterSites map[string][]spawnSite
}

// mergeProgramEmission folds the per-module discovery results into one
// program-wide aggregate. Every merge is deterministic: modules contribute
// in the dependency-first order slice, deduplicated collections are keyed by
// canonical identity, and slice order is preserved or re-sorted explicitly.
func mergeProgramEmission(modules []*moduleEmission) *programEmission {
	merged := &programEmission{
		heapState:   &heapHelpers{seen: make(map[string]bool)},
		viewState:   &generatedViewState{seen: make(map[*compilerTypes.ViewInfo]bool)},
		stringState: &generatedStringState{seen: make(map[string]int)},
		listState:   &generatedListState{seen: make(map[*compilerTypes.ListInfo]bool)},
		dictState:   &generatedDictState{seen: make(map[*compilerTypes.DictInfo]bool)},
		arrayState:  &generatedArrayState{seen: make(map[*compilerTypes.ArrayInfo]bool)},
		concurrencyState: &generatedConcurrencyState{
			taskTypes:            make(map[string]compilerTypes.Type),
			joinTypes:            make(map[string]compilerTypes.Type),
			channels:             make(map[string]compilerTypes.Type),
			atomics:              make(map[string]compilerTypes.Type),
			channelNewUnions:     make(map[string]compilerTypes.Type),
			channelSendUnions:    make(map[string]compilerTypes.Type),
			channelReceiveUnions: make(map[string]compilerTypes.Type),
		},
		wrapState:    &generatedWrapState{seen: make(map[string]bool)},
		adapterSites: make(map[string][]spawnSite),
	}
	viewOrders := make([][]compilerTypes.Type, 0, len(modules))
	arrayOrders := make([][]compilerTypes.Type, 0, len(modules))
	listOrders := make([][]compilerTypes.Type, 0, len(modules))
	dictOrders := make([][]compilerTypes.Type, 0, len(modules))
	sizeSeen := make(map[string]bool)
	spawnedSites := make(map[string]bool)
	for _, module := range modules {
		merged.errorUsed = merged.errorUsed || module.errorUsed
		mergeHeapInto(merged.heapState, module.heapState)
		mergeStringsInto(merged.stringState, module.stringState)
		mergeConcurrencyInto(merged.concurrencyState, module.concurrencyState, spawnedSites)
		mergeWrapState(merged.wrapState, module.wrapState)
		if module.arrayState != nil {
			arrayOrders = append(arrayOrders, module.arrayState.order)
		}
		if module.viewState != nil {
			viewOrders = append(viewOrders, module.viewState.views)
			merged.viewState.required = merged.viewState.required || module.viewState.required
		}
		if module.listState != nil {
			listOrders = append(listOrders, module.listState.order)
		}
		if module.dictState != nil {
			dictOrders = append(dictOrders, module.dictState.order)
		}
		for _, digits := range module.sizeLiterals {
			if !sizeSeen[digits] {
				sizeSeen[digits] = true
				merged.sizeLiterals = append(merged.sizeLiterals, digits)
			}
		}
	}
	merged.viewState.views = mergeTypeOrders(viewOrders)
	merged.arrayState.order = mergeTypeOrders(arrayOrders)
	merged.listState.order = mergeTypeOrders(listOrders)
	merged.dictState.order = mergeTypeOrders(dictOrders)
	merged.adapterSites = routeSpawnSites(merged.concurrencyState)
	// The per-module runtime error literals were registered against each
	// module's own table during discovery; after aggregation the emitted
	// indices must match the program-wide table hexal.h defines.
	rebaseLiteralNames(modules, merged.stringState)
	// The standard-header and hex_eos requirements aggregate after every
	// family state is merged, so the umbrella set covers the complete
	// reachable generated program.
	merged.requirements = computeHeaderRequirements(merged, modules)
	return merged
}

// computeHeaderRequirements builds the program-wide standard-header and
// hex_eos requirement set from every reachable module's written types and
// selected helper families. hexal.h is included by every module header, so
// the set is the union over all modules; each family contributes the
// standard headers that own the declarations and macros its generated
// helpers actually call. Requirements are discovered from checked types and
// selection state, never by searching rendered C text.
func computeHeaderRequirements(merged *programEmission, modules []*moduleEmission) *cHeaderRequirements {
	requirements := &cHeaderRequirements{}
	for _, module := range modules {
		collectTypeRequirements(module.program, requirements)
		heapState := module.heapState
		if heapState != nil && (heapState.required || len(heapState.elements) > 0) {
			// hex_heap_raw_allocate/free: malloc/free from <stdlib.h>,
			// uintptr_t from <stdint.h>, size_t from <stddef.h>, and the
			// ckd_add checked arithmetic from <stdckdint.h>. Diagnostic
			// traps report through the one program-wide hex_runtime_trap.
			requirements.add("stdckdint.h", "stddef.h", "stdint.h", "stdlib.h")
			requirements.trap = true
		}
		if module.viewState != nil && len(module.viewState.views) > 0 {
			// View structs carry size_t; bounds guards trap.
			requirements.add("stddef.h", "stdint.h")
			requirements.trap = true
		}
		if module.arrayState != nil && len(module.arrayState.order) > 0 {
			// Array accessors use UINT64_C bounds and trap.
			requirements.add("stdint.h")
			requirements.trap = true
		}
		if module.stringState != nil && module.stringState.used {
			// hex_string/hex_strand storage and the UTF-8 helpers use
			// uint8_t, size_t, free, ckd_add, and memcpy (<string.h>);
			// diagnostic traps report through hex_runtime_trap.
			requirements.add("stdckdint.h", "stddef.h", "stdint.h", "stdlib.h", "string.h")
			requirements.trap = true
		}
		if module.listState != nil && len(module.listState.order) > 0 {
			// List headers carry size_t/uintptr_t, grow with ckd_mul, copy
			// the initialized prefix with memcpy, and trap.
			requirements.add("stdckdint.h", "stddef.h", "stdint.h", "string.h")
			requirements.trap = true
		}
		if module.dictState != nil && len(module.dictState.order) > 0 {
			// Dict headers carry size_t/uintptr_t, grow with ckd_mul,
			// zero fresh regions with memset and probe Strand keys with
			// memcmp (<string.h>), and trap.
			requirements.add("stdckdint.h", "stddef.h", "stdint.h", "string.h")
			requirements.trap = true
		}
		if equalityStateUsed(module.equalityState) {
			// Byte-compare helpers use memcmp over size_t lengths; union
			// equality helpers abort directly on an impossible tag.
			requirements.add("stddef.h", "string.h")
			if unionEqualityUsed(module.equalityState) {
				requirements.add("stdlib.h")
			}
		}
		if len(module.conversionSpecs) > 0 {
			// Checked conversion helpers use the exact-width limit macros
			// from <stdint.h> and call the one program-wide
			// hex_runtime_trap; float-source helpers additionally use
			// isnan/isinf/trunc/truncf from <math.h>. The set holds only
			// checked pairs: a program with only direct or
			// identity conversions selects no conversion trap or headers.
			requirements.add("stdint.h")
			requirements.trap = true
			if conversionUsesMath(module.conversionSpecs) {
				requirements.add("math.h")
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
			requirements.add("string.h")
		}
		if module.printState != nil && module.printState.used {
			// The print family formats with PRI* macros (<inttypes.h>) and
			// snprintf/fwrite on stdout, classifies floats through
			// <math.h>, and uses uint8_t/size_t/bool; its write-failure trap
			// reports through hex_runtime_trap.
			requirements.add("stddef.h", "stdint.h", "stdio.h", "inttypes.h", "math.h")
			requirements.trap = true
		}
		concurrency := module.concurrencyState
		if concurrency != nil && (concurrency.used || len(concurrency.atomics) > 0) {
			if concurrency.used {
				// The scheduler runtime (root C) and the
				// spawn/channel/mutex inline helpers use size_t, SIZE_MAX,
				// int64_t/uint8_t, malloc/calloc/free, and the shared trap;
				// Channel slot sizing uses ckd_mul. The receive machinery
				// represents completion as hex_eos for every channel.
				requirements.add("stdckdint.h", "stddef.h", "stdint.h", "stdlib.h")
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
				requirements.add("stdatomic.h")
			}
		}
		if module.unionState != nil {
			for _, union := range module.unionState.order {
				// Union truthiness, equality, and widening helpers abort
				// directly on an impossible tag; EoS members spell hex_eos
				// payloads. Nil members are tag-only and spell no C type.
				requirements.add("stdlib.h")
				for _, member := range compilerTypes.UnionMembers(union) {
					if compilerTypes.IsEoS(member) {
						requirements.eos = true
						requirements.add("stdint.h")
					}
				}
			}
		}
	}
	if merged.wrapState != nil && len(merged.wrapState.order) > 0 {
		// The signed wrapping helpers adapt ckd_* result-pointer macros.
		requirements.add("stdckdint.h")
	}
	if requirements.trap {
		// The one program-wide trap declaration and root definition own
		// <stdio.h>/<stdlib.h>; families that only call it do not claim
		// those headers independently.
		requirements.add("stdio.h", "stdlib.h")
	}
	if len(merged.sizeLiterals) > 0 {
		// The retained SIZE_MAX static_assert is the one source-dependent
		// target probe; it needs size_t and SIZE_MAX themselves.
		requirements.add("stddef.h", "stdint.h")
	}
	return requirements
}

// equalityStateUsed reports whether any equality helper is emitted.
func equalityStateUsed(state *generatedEqualityState) bool {
	return state != nil && len(state.order) > 0
}

// unionEqualityUsed reports whether any emitted equality helper is a union
// equality (the only equality shape that aborts).
func unionEqualityUsed(state *generatedEqualityState) bool {
	if state == nil {
		return false
	}
	for _, typ := range state.order {
		if typ.Union != nil && unionSupportsEquality(typ) {
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
	sort.SliceStable(merged, func(left, right int) bool {
		return merged[left].CName < merged[right].CName
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
}

// mergeStringsInto folds one module's literal table into the program-wide
// table: flags OR, payloads concatenate in module order, deduplicated by
// first occurrence. The aggregated index is authoritative for every module's
// emitted literal references.
func mergeStringsInto(merged, state *generatedStringState) {
	if state == nil {
		return
	}
	if state.used {
		merged.used = true
	}
	if state.needStrand {
		merged.needStrand = true
	}
	for _, payload := range state.literals {
		if _, exists := merged.seen[payload]; !exists {
			merged.seen[payload] = len(merged.literals) + 1
			merged.literals = append(merged.literals, payload)
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
		if !spawnedSites[site.function] {
			spawnedSites[site.function] = true
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
		if site.module == "" || seen[site.function] {
			continue
		}
		seen[site.function] = true
		routed[site.module] = append(routed[site.module], site)
	}
	return routed
}

// rebaseLiteralNames rewrites each module's precomputed runtime failure
// literal names against the program-wide literal table: discovery registered
// the payloads into each module's own table, but every emitted reference must
// name the aggregated index hexal.h defines, the one canonical literal set
// program-wide.
func rebaseLiteralNames(modules []*moduleEmission, stringState *generatedStringState) {
	for _, module := range modules {
		if module.concurrencyState != nil && module.concurrencyState.used {
			module.concurrencyState.fileLiteral = literalObjectName(stringState, sourceFilename)
			module.concurrencyState.headerLiteral = literalObjectName(stringState, "Scheduler")
		}
	}
}

// literalObjectName returns the program-wide object name of one literal
// payload, or "" when the payload was never registered (unreachable for
// payloads the module registered during discovery).
func literalObjectName(stringState *generatedStringState, payload string) string {
	if index, ok := stringState.seen[payload]; ok {
		return stringLiteralCName(index - 1)
	}
	return ""
}

// emitModulePair writes one module's C/header pair from its own discovery
// states plus the program-wide aggregate. The entrypoint module (isRoot)
// additionally carries the process-wide runtime cores and the C entry point;
// the entry adapters of every spawn site routed to this module are emitted
// after the function definitions they call; all literal references use the
// program-wide table.
func emitModulePair(emission *moduleEmission, merged *programEmission, isRoot bool) (moduleC string, moduleH string, err error) {
	canonicalID := emission.canonicalID
	logicalKey := emission.logicalKey
	program := emission.program
	owner := compilerTypes.EncodeModuleOwner(canonicalID)
	functions := emission.functions
	methods := emission.methods
	typeState := emission.typeState
	stringState := merged.stringState

	// Every module C file includes only its own module header; the header
	// includes hexal.h, so the translation unit sees the shared
	// program-support contract exactly once.
	var moduleBody strings.Builder
	moduleBody.WriteString("#include \"modules/" + canonicalID + ".h\"\n\n")

	// Function definitions sit at file scope in source order, after the
	// object definitions the header already carries. Only self-recursion and
	// calls to earlier definitions are legal, so no prototype region is
	// needed. Functions the module's own spawn prologues name keep external
	// linkage; everything else stays static inside the module C file.
	spawned := make(map[string]bool)
	if emission.concurrencyState != nil {
		for _, site := range emission.concurrencyState.spawns {
			spawned[site.function] = true
		}
	}
	for _, statement := range program.Statements {
		switch declared := statement.(type) {
		case checker.FunctionDeclaration:
			if definitionErr := writeFunctionDefinition(&moduleBody, declared, functions, methods, typeState, stringState, owner, logicalKey, spawned[PrivateCName(FunctionName, declared.Name, owner)]); definitionErr != nil {
				return "", "", definitionErr
			}
		case checker.MethodDeclaration:
			if definitionErr := writeMethodDefinition(&moduleBody, declared, functions, methods, typeState, stringState, owner, logicalKey); definitionErr != nil {
				return "", "", definitionErr
			}
		}
	}

	// Concrete specializations are emitted after the regular definitions.
	// Their bodies can call functions declared before their generic template
	// (already emitted) or other specializations (any order), so each gets a
	// prototype first and the definitions follow in cache order.
	if err := writeSpecializedPrototypes(&moduleBody, program.SpecializedFunctions, program.SpecializedMethods, typeState, owner); err != nil {
		return "", "", err
	}
	if err := writeSpecializedDefinitions(&moduleBody, program.SpecializedFunctions, program.SpecializedMethods, functions, methods, typeState, stringState, owner, logicalKey); err != nil {
		return "", "", err
	}

	// The spawn entry adapters follow every function definition because they
	// call the spawned functions directly. The adapter of a spawn lives
	// beside the spawned function's own definition, so the call never
	// crosses a translation unit.
	writeSpawnAdapters(&moduleBody, merged.adapterSites[canonicalID])

	renderState := &expressionValidation{
		variables:      make(map[string]generatedBinding),
		bindings:       make(map[checker.BindingID]generatedBinding),
		bindingNames:   make(map[checker.BindingID]string),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
		owner:          owner,
		filename:       logicalKey,
		moduleID:       canonicalID,
	}
	if isRoot {
		// The selected root module's C file owns the process entry point.
		// Runtime definitions and state live in the component artifacts
		// under generated hexal/; the module C file is not the
		// fallback runtime container.
		moduleBody.WriteString(concurrencyRuntimeContent(merged.concurrencyState, merged.stringState))

		renderState.pushScope()
		// The module statements execute directly inside main(). Module-level
		// storage stays inside main; a function body cannot reach it, so
		// nothing is promoted to static storage duration. With concurrency
		// the statements run as the root task between scheduler
		// initialization and hex_task_complete; without it they run before
		// main returns C's successful integer status directly. No non-root
		// module ever declares or defines main() or process-wide runtime
		// state.
		moduleBody.WriteString("int main(void) {\n")
		if merged.concurrencyState != nil && merged.concurrencyState.used {
			moduleBody.WriteString("    hex_scheduler_init();\n")
		}
		if statementErr := writeStatements(&moduleBody, program.Statements, renderState, nil, false, program.Defers); statementErr != nil {
			return "", "", statementErr
		}
		if merged.concurrencyState != nil && merged.concurrencyState.used {
			// Completing the root Task wakes the scheduler, stops the
			// workers, and switches back to main so it returns normally.
			// Tasks still active are abandoned to process termination.
			moduleBody.WriteString("    hex_task_complete(hex_root_task);\n")
		}
		moduleBody.WriteString("    return 0;\n}\n")
	}

	// The module's own header declares its exported declarations and every
	// foreign symbol it calls; importers never include another module's
	// header. Every module header includes hexal.h for the shared
	// program-support contract. The entry adapters routed to this module
	// read their argument frames, so those frames are declared here too,
	// self-contained in the owning module's translation unit.
	var headerPrototypes strings.Builder
	writeExportedPrototypes(&headerPrototypes, program, owner)
	writeForeignPrototypes(&headerPrototypes, program, renderState)
	var extraFrames strings.Builder
	writeSpawnArgFrames(&extraFrames, routedFrames(emission, merged.adapterSites[canonicalID]))

	moduleHeader := moduleHeader(moduleHeaderInput{
		unions:        emission.unionState,
		adts:          emission.adtState,
		equality:      emission.equalityState,
		conversions:   emission.conversionSpecs,
		divisionTypes: emission.divisionTypes,
		shiftSpecs:    emission.shiftSpecs,
		bitCastSpecs:  emission.bitCastSpecs,
		endianSpecs:   emission.endianSpecs,
		objects:       emission.objects,
		heaps:         emission.heapState,
		printState:    emission.printState,
		concurrency:   emission.concurrencyState,
		stringState:   stringState,
		canonicalID:   canonicalID,
		prototypes:    headerPrototypes.String(),
		extraFrames:   extraFrames.String(),
		filename:      logicalKey,
		components:    moduleComponentHeaders(emission),
	})
	return moduleBody.String(), moduleHeader, nil
}

// routedFrames returns the entry-adapter argument frames this module's header
// must declare beyond its own spawn sites: the adapter sites routed here
// whose frames the inline concurrency helpers have not already emitted.
func routedFrames(emission *moduleEmission, sites []spawnSite) []spawnSite {
	own := make(map[string]bool)
	if emission.concurrencyState != nil {
		for _, site := range emission.concurrencyState.spawns {
			own[site.function] = true
		}
	}
	frames := make([]spawnSite, 0, len(sites))
	for _, site := range sites {
		if !own[site.function] {
			frames = append(frames, site)
		}
	}
	return frames
}

// moduleComponentHeaders returns the path-qualified component headers this
// module's header includes, in dependency order: wrap, heap, view,
// string, error, list, dict, array, concurrency. Each migrated family selects
// itself here; a family still owned by hexal.h during the component
// migration contributes nothing.
func moduleComponentHeaders(emission *moduleEmission) []string {
	var components []string
	components = append(components, moduleWrapComponent(emission)...)
	components = append(components, moduleHeapComponent(emission)...)
	components = append(components, moduleViewComponent(emission)...)
	components = append(components, moduleStringComponent(emission)...)
	components = append(components, moduleErrorComponent(emission)...)
	components = append(components, moduleListComponent(emission)...)
	components = append(components, moduleDictComponent(emission)...)
	components = append(components, moduleArrayComponent(emission)...)
	components = append(components, moduleConcurrencyComponent(emission)...)
	return components
}

// hexalHeaderInput carries every value the shared program-support header
// builder consumes. One field per argument, no derived or cached state.
type hexalHeaderInput struct {
	errorUsed    bool
	heaps        *heapHelpers
	views        *generatedViewState
	stringState  *generatedStringState
	lists        *generatedListState
	dicts        *generatedDictState
	arrays       *generatedArrayState
	concurrency  *generatedConcurrencyState
	wrap         *generatedWrapState
	sizeLiterals []string
	// requirements is the demand-driven standard-header and hex_eos set.
	requirements *cHeaderRequirements
}

// moduleHeaderInput carries every value the module-header builder consumes.
// One field per argument, no derived or cached state.
type moduleHeaderInput struct {
	unions        *generatedUnionState
	adts          *generatedAdtState
	equality      *generatedEqualityState
	conversions   []conversionSpec
	divisionTypes []compilerTypes.Type
	shiftSpecs    []shiftSpec
	bitCastSpecs  []bitCastSpec
	endianSpecs   []endianSpec
	objects       []*compilerTypes.ObjectType
	heaps         *heapHelpers
	printState    *generatedPrintState
	concurrency   *generatedConcurrencyState
	stringState   *generatedStringState
	canonicalID   string
	prototypes    string
	extraFrames   string
	filename      string
	// components are the path-qualified component headers this module needs,
	// in dependency order.
	components []string
}

// hexalHeaderModel is the render model for the hexal.h template:
// the guard, the demand-driven program-wide standard-header umbrella, the
// retained source-dependent Size-literal assertions, the hex_eos typedef when
// required, and the extern declaration of the one program-wide diagnostic
// trap. FamilyContent is the transition seam for families migrating to their
// component artifacts; it is empty once every family has moved.
type hexalHeaderModel struct {
	Includes      []string
	SizeAsserts   []string
	Eos           bool
	TrapDeclared  bool
	FamilyContent string
}

// hexalHeader emits hexal.h. Everything here is included by every translation
// unit through each module header, so no definition in hexal.h may hold
// static storage. The standard-header set satisfies the complete reachable
// generated program. Generic toolchain qualification is a supported-toolchain
// contract, not a generated probe; only source-dependent target assertions
// are emitted. The source of truth for the shell is packages/hexal.h; the
// state data is the program-wide aggregate.
func hexalHeader(input hexalHeaderInput) (string, error) {
	var familyContent strings.Builder
	familyContent.WriteString(wrapFamilyContent(input.wrap))
	familyContent.WriteString(concurrencyFamilyContent(input.concurrency))
	familyContent.WriteString(heapFamilyContent(input.heaps))
	familyContent.WriteString(viewFamilyContent(input.views))
	familyContent.WriteString(stringFamilyContent(input.stringState))
	if input.errorUsed {
		familyContent.WriteString(errorFamilyContent())
	}
	familyContent.WriteString(listFamilyContent(input.lists, input.views))
	familyContent.WriteString(dictFamilyContent(input.dicts))
	familyContent.WriteString(arrayFamilyContent(input.arrays, input.views))
	model := hexalHeaderModel{
		SizeAsserts:   input.sizeLiterals,
		FamilyContent: familyContent.String(),
	}
	if input.requirements != nil {
		headers := make([]string, 0, len(input.requirements.headers))
		for header := range input.requirements.headers {
			headers = append(headers, header)
		}
		sort.Strings(headers)
		model.Includes = headers
		model.Eos = input.requirements.eos
		model.TrapDeclared = input.requirements.trap
	}
	return renderComponent(componentArtifact{key: "hexal.h", template: "hexal.h", model: model})
}

// moduleHeader emits one module's header: everything that references the
// module's own types, plus the state-free inline helper families whose
// non-inline cores live in the root module's C file. input.extraFrames
// holds the entry-adapter argument frames of the spawn sites routed to this
// module.
func moduleHeader(input moduleHeaderInput) string {
	var result strings.Builder
	result.WriteString("#ifndef " + compilerTypes.ModuleHeaderGuard(input.canonicalID) + "\n#define " + compilerTypes.ModuleHeaderGuard(input.canonicalID) + "\n\n#include \"hexal.h\"\n")
	// The component headers this module needs follow hexal.h in dependency
	// order; a module never includes a component selected only by another
	// module.
	for _, component := range input.components {
		fmt.Fprintf(&result, "#include \"%s\"\n", component)
	}
	writeAdtDefinitions(&result, input.adts)
	writeUnionDefinitions(&result, input.unions)
	writeObjectDefinitions(&result, input.objects, input.filename)
	// The typed heap allocation helpers reference module-owned element
	// types, so they follow the object definitions.
	writeHeapAllocateHelpers(&result, input.heaps)
	writeShiftDefinitions(&result, input.shiftSpecs)
	writeBitCastDefinitions(&result, input.bitCastSpecs)
	writeEndianDefinitions(&result, input.endianSpecs)
	writePrintDefinitions(&result, input.printState)
	writeEqualityDefinitions(&result, input.equality)
	writeDivisionDefinitions(&result, input.divisionTypes)
	writeConversionDefinitions(&result, input.conversions)
	writeConcurrencyInlineHelpers(&result, input.concurrency, input.stringState)
	if input.prototypes != "" {
		result.WriteString("\n/* Exported and foreign function prototypes. */\n")
		result.WriteString(input.prototypes)
	}
	if input.extraFrames != "" {
		result.WriteString("\n/* Spawn entry adapter argument frames. */\n")
		result.WriteString(input.extraFrames)
	}
	result.WriteString("\n#endif\n")
	return result.String()
}

func objectDefinitions(program checker.Program) ([]*compilerTypes.ObjectType, error) {
	objects := make([]*compilerTypes.ObjectType, 0)
	seen := make(map[*compilerTypes.ObjectType]bool)
	seenCNames := make(map[string]*compilerTypes.ObjectType)
	// A module's header must carry every object type its translation unit
	// can name by value, including imported modules' exported objects
	// referenced through the import alias. They are reachable through the
	// checked statements' types, so the walk collects local and foreign
	// definitions together; each module file includes only its own header
	// plus hexal.h, so no translation unit ever sees two definitions of one
	// struct.
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			object := typ.Object
			if object == nil || typ.Incomplete {
				return nil
			}
			if previous, exists := seenCNames[object.CName]; exists && previous != object {
				return unknownExpressionDiagnostic("conflicting generated object C name")
			}
			seenCNames[object.CName] = object
			if seen[object] {
				return nil
			}
			seen[object] = true
			objects = append(objects, object)
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	for _, declaration := range program.TypeDeclarations {
		object := declaration.Type.Object
		if object == nil {
			continue
		}
		if previous, exists := seenCNames[object.CName]; exists && previous != object {
			return nil, unknownExpressionDiagnostic("conflicting generated object C name")
		}
		seenCNames[object.CName] = object
		if seen[object] {
			continue
		}
		seen[object] = true
		objects = append(objects, object)
	}
	return objects, nil
}

func writeObjectDefinitions(result *strings.Builder, objects []*compilerTypes.ObjectType, filename string) {
	// Forward typedef region first, in source declaration order, so recursive
	// and non-recursive objects share one shape and pointer members can name a
	// not-yet-defined object.
	for _, object := range objects {
		result.WriteString("\n")
		if object.SourceLine > 0 {
			fmt.Fprintf(result, "#line %d \"%s\"\n", object.SourceLine, filename)
		}
		fmt.Fprintf(result, "typedef struct %s %s;\n", object.CName, object.CName)
	}
	for _, object := range objects {
		result.WriteString("\n")
		if object.SourceLine > 0 {
			fmt.Fprintf(result, "#line %d \"%s\"\n", object.SourceLine, filename)
		}
		fmt.Fprintf(result, "struct %s {\n", object.CName)
		for _, member := range object.Members {
			if member.SourceLine > 0 {
				fmt.Fprintf(result, "#line %d \"%s\"\n", member.SourceLine, filename)
			}
			// Reference-like members (String, List, Dict) are pointer-sized
			// handles, spelled like their declarations.
			fmt.Fprintf(result, "    %s;\n", declaration(member.Type, PrivateCName(MemberName, member.Name, ""), true))
		}
		fmt.Fprintf(result, "};\n")
	}
}

// collectTypeRequirements folds one module's written checked types into the
// program-wide standard-header and hex_eos requirement set.
// Exact-width integers and their aliases (Rune, Byte) require <stdint.h>;
// Size and Nil require <stddef.h>; a written EoS type requires <stdint.h>
// and the hex_eos typedef. The walk descends into ADT payloads, union
// members, pointer pointees, signatures, and collection element types, so a
// nested EoS or Nil member is discovered wherever it is spelled.
func collectTypeRequirements(program checker.Program, requirements *cHeaderRequirements) {
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			switch {
			case compilerTypes.IsSize(typ):
				// Size spells size_t, owned by <stddef.h> independently of
				// any allocation or Nil usage. Checked before IsInteger:
				// Size is also an unsigned integer scalar.
				requirements.add("stddef.h")
			case compilerTypes.IsInteger(typ) || compilerTypes.IsRune(typ):
				// Int8..Int64, UInt8..UInt64, Rune, and the Byte alias all
				// spell exact-width C types from <stdint.h>.
				requirements.add("stdint.h")
			case compilerTypes.IsEoS(typ):
				// EoS spells hex_eos, one compiler-owned byte typedef.
				requirements.eos = true
				requirements.add("stdint.h")
			}
			return nil
		},
		Operand: func(source checker.Operand) error {
			// A special float literal renders the NAN/INFINITY macros from
			// <math.h>; a finite literal needs no header.
			if compilerTypes.IsFloat(source.Type) && source.FloatBits != 0 {
				bitSize := 64
				bits := source.FloatBits
				if compilerTypes.Equal(source.Type, compilerTypes.Float32) {
					bitSize = 32
					bits = uint64(uint32(bits))
				}
				if _, special := floatSignAndSpecial(bits, bitSize); special {
					requirements.add("math.h")
				}
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		panic(err)
	}
}
