// emission.go owns module/program emission (RFC 0059): per-module discovery
// state, program-wide state merging, module and main C/header pair emission,
// and header assembly.
package generator

import (
	"fmt"
	"sort"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

const mainHeaderPrefix = "#ifndef HEXAL_MAIN_H\n#define HEXAL_MAIN_H\n\n#include <stdint.h>\n#include <stdbool.h>\n#include <limits.h>\n#include <stdlib.h>\n"
const sourceFilename = "main.hex"

// RFC 0034: each module's generated C and header are modules/<canonical>.c and
// modules/<canonical>.h; the entrypoint module's sources map key is the only
// source file name the #line directives can know today (multi-module source
// mapping arrives with Task 7 of RFC 0034).
// moduleFilename is gone: RFC 0034 threads the per-module logical key
// through the emission writers for #line directives.

// moduleEmission is one module's validated discovery result: every built-in
// machinery state its program needs, kept separate from emission so the
// program-wide aggregate can be computed before any file text is written.
// The per-module states still drive the module's own header content, which
// may repeat C-safe inline helpers across headers (RFC 0034).
type moduleEmission struct {
	canonicalID string
	logicalKey  string
	program     checker.Program
	functions   map[string]compilerTypes.Type
	methods     map[string]checker.MethodDeclaration
	typeState   *generatedTypeValidation

	float32Used      bool
	float64Used      bool
	nilUsed          bool
	errorUsed        bool
	unionState       *generatedUnionState
	heapState        *heapHelpers
	adtState         *generatedAdtState
	arrayState       *generatedArrayState
	viewState        *generatedViewState
	stringState      *generatedStringState
	listState        *generatedListState
	dictState        *generatedDictState
	streamState      *generatedStreamState
	equalityState    *generatedEqualityState
	conversionSpecs  []conversionSpec
	sizeLiterals     []string
	divisionTypes    []compilerTypes.Type
	shiftSpecs       []shiftSpec
	bitCastSpecs     []bitCastSpec
	endianSpecs      []endianSpec
	printState       *generatedPrintState
	ioState          *generatedIOState
	concurrencyState *generatedConcurrencyState
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
	emission.float32Used, emission.float64Used, emission.nilUsed = usedFloatTypes(program)
	objects, objectErr := objectDefinitions(program)
	if objectErr != nil {
		return nil, objectErr
	}
	emission.objects = objects
	emission.errorUsed = discoverErrorUsed(program)
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
	streamState, streamErr := discoverGeneratedStreams(program, owner)
	if streamErr != nil {
		return nil, streamErr
	}
	emission.streamState = streamState
	emission.shiftSpecs = discoverGeneratedShifts(program)
	emission.bitCastSpecs = discoverGeneratedBitCasts(program)
	emission.endianSpecs = discoverGeneratedEndian(program)
	emission.printState = discoverGeneratedPrint(program)
	emission.ioState = discoverGeneratedIO(program, stringState)
	concurrencyState, concurrencyErr := discoverGeneratedConcurrency(program, functions, stringState, canonicalID, owner)
	if concurrencyErr != nil {
		return nil, concurrencyErr
	}
	emission.concurrencyState = concurrencyState
	if concurrencyState.used {
		// RFC 0037: the task runtime needs the String and Strand typedefs and
		// the Error object for the failure Errors every recoverable
		// operation constructs; the discovery pass registered the literals.
		// The Channel and Mutex helpers take a hex_heap argument, so the heap
		// machinery is required too.
		stringState.used = true
		stringState.needStrand = true
		heapState.required = true
	}
	if stringState.used {
		ensureViewUInt8(viewState)
		// The String helpers allocate through the heap machinery.
		heapState.required = true
	}
	if len(listState.order) > 0 || len(dictState.order) > 0 || len(streamState.order) > 0 {
		// The List, Dict, and Stream helpers allocate and trap through the
		// heap machinery and fputs.
		heapState.required = true
	}
	emission.typeState = &generatedTypeValidation{declaredObjects: errorDeclaredObjects(program)}
	return emission, nil
}

// programEmission is the program-wide aggregate of every reachable module's
// built-in machinery. main.c and main.h are generated from it, so the
// once-per-process runtime cores and the shared type and literal definitions
// cover every module, not just the entrypoint's discovery (RFC 0034:
// built-in specialization identity depends only on the constructor and its
// canonical arguments, never on the requesting module).
type programEmission struct {
	float32Used      bool
	float64Used      bool
	nilUsed          bool
	errorUsed        bool
	stdioNeeded      bool
	heapState        *heapHelpers
	viewState        *generatedViewState
	stringState      *generatedStringState
	listState        *generatedListState
	dictState        *generatedDictState
	arrayState       *generatedArrayState
	concurrencyState *generatedConcurrencyState
	ioState          *generatedIOState
	sizeLiterals     []string
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
		ioState:      &generatedIOState{},
		adapterSites: make(map[string][]spawnSite),
	}
	viewOrders := make([][]compilerTypes.Type, 0, len(modules))
	arrayOrders := make([][]compilerTypes.Type, 0, len(modules))
	listOrders := make([][]compilerTypes.Type, 0, len(modules))
	dictOrders := make([][]compilerTypes.Type, 0, len(modules))
	sizeSeen := make(map[string]bool)
	spawnedSites := make(map[string]bool)
	for _, module := range modules {
		merged.float32Used = merged.float32Used || module.float32Used
		merged.float64Used = merged.float64Used || module.float64Used
		merged.nilUsed = merged.nilUsed || module.nilUsed
		merged.errorUsed = merged.errorUsed || module.errorUsed
		if containsSizeConversion(module.conversionSpecs) ||
			module.printState != nil && module.printState.used ||
			module.arrayState != nil && len(module.arrayState.order) > 0 ||
			module.viewState != nil && len(module.viewState.views) > 0 ||
			module.stringState != nil && module.stringState.used ||
			module.listState != nil && len(module.listState.order) > 0 ||
			module.dictState != nil && len(module.dictState.order) > 0 {
			merged.stdioNeeded = true
		}
		mergeHeapInto(merged.heapState, module.heapState)
		mergeStringsInto(merged.stringState, module.stringState)
		mergeConcurrencyInto(merged.concurrencyState, module.concurrencyState, spawnedSites)
		mergeIOInto(merged.ioState, module.ioState)
		if module.arrayState != nil {
			arrayOrders = append(arrayOrders, module.arrayState.order)
		}
		if module.viewState != nil {
			viewOrders = append(viewOrders, module.viewState.views)
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
	// indices must match the program-wide table main.h defines.
	rebaseLiteralNames(modules, merged.stringState)
	return merged
}

// mergeTypeOrders unions per-module specialization lists of one built-in
// family by canonical C name and sorts the result, so identical canonical
// types from different modules are one specialization (RFC 0034: built-in
// specialization identity depends only on the constructor and its canonical
// arguments).
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

// mergeConcurrencyInto unions one module's RFC 0037 machinery into the
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

// mergeIOInto unions one module's RFC 0040 machinery into the program-wide
// state. The per-operation result unions are canonical, so the first
// non-empty record stands in deterministically for the family.
func mergeIOInto(merged, state *generatedIOState) {
	if state == nil || !state.used {
		return
	}
	merged.used = true
	merged.open = merged.open || state.open
	merged.readBytes = merged.readBytes || state.readBytes
	merged.readText = merged.readText || state.readText
	merged.write = merged.write || state.write
	merged.writeText = merged.writeText || state.writeText
	merged.flush = merged.flush || state.flush
	merged.close = merged.close || state.close
	merged.stdin = merged.stdin || state.stdin
	merged.stdout = merged.stdout || state.stdout
	merged.stderr = merged.stderr || state.stderr
	if merged.openUnion == (compilerTypes.Type{}) {
		merged.openUnion = state.openUnion
	}
	if merged.readBytesUnion == (compilerTypes.Type{}) {
		merged.readBytesUnion = state.readBytesUnion
	}
	if merged.readTextUnion == (compilerTypes.Type{}) {
		merged.readTextUnion = state.readTextUnion
	}
	if merged.writeUnion == (compilerTypes.Type{}) {
		merged.writeUnion = state.writeUnion
	}
	if merged.listType == (compilerTypes.Type{}) {
		merged.listType = state.listType
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
// name the aggregated index main.h defines (RFC 0034: one canonical literal
// set program-wide).
func rebaseLiteralNames(modules []*moduleEmission, stringState *generatedStringState) {
	for _, module := range modules {
		if module.concurrencyState != nil && module.concurrencyState.used {
			module.concurrencyState.fileLiteral = literalObjectName(stringState, sourceFilename)
			module.concurrencyState.headerLiteral = literalObjectName(stringState, "Scheduler")
		}
		if module.ioState != nil && module.ioState.used {
			module.ioState.fileLiteral = literalObjectName(stringState, sourceFilename)
			module.ioState.headerLiteral = literalObjectName(stringState, "I/O Error")
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
// additionally carries the root run function and its header declaration; the
// entry adapters of every spawn site routed to this module are emitted after
// the function definitions they call; all literal references use the
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

	// RFC 0034: the module's own C file carries every user definition, the
	// spawn entry adapters of the functions it owns, and, for the entrypoint
	// module only, the root run. The generated main.c owns the compiler
	// machinery that must exist once per process: the concurrency runtime
	// and the I/O gate, both built from the program-wide aggregate.
	var moduleBody strings.Builder
	moduleBody.WriteString("#include \"main.h\"\n#include \"modules/" + canonicalID + ".h\"\n\n")

	// Function definitions sit at file scope in source order, after the
	// object definitions the header already carries. Only self-recursion and
	// calls to earlier definitions are legal, so no prototype region is
	// needed. RFC 0034: functions the module's own spawn prologues name keep
	// external linkage; everything else stays static inside the module C
	// file.
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

	// RFC 0037: the spawn entry adapters follow every function definition
	// because they call the spawned functions directly. The adapter of a
	// spawn lives beside the spawned function's own definition, so the call
	// never crosses a translation unit (RFC 0034 per-module generation).
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
		renderState.pushScope()
		// RFC 0034: the module statements execute as hex_module_root_run,
		// called by the generated main after scheduler initialization.
		// Module-level storage stays inside the run; a function body cannot
		// reach it, so nothing is promoted to static storage duration. The
		// root run exists only in the entrypoint module's pair.
		moduleBody.WriteString("int hex_module_root_run(void) {\n")
		if statementErr := writeStatements(&moduleBody, program.Statements, renderState, nil, false, program.Defers); statementErr != nil {
			return "", "", statementErr
		}
		moduleBody.WriteString("    return EXIT_SUCCESS;\n}\n")
	}

	// RFC 0034: the module's own header declares its exported declarations
	// and every foreign symbol it calls; importers never include another
	// module's header. The entry adapters routed to this module read their
	// argument frames, so those frames are declared here too, self-contained
	// in the owning module's translation unit.
	var headerPrototypes strings.Builder
	writeExportedPrototypes(&headerPrototypes, program, owner)
	writeForeignPrototypes(&headerPrototypes, program, renderState)
	var extraFrames strings.Builder
	writeSpawnArgFrames(&extraFrames, routedFrames(emission, merged.adapterSites[canonicalID]))

	moduleHeader := moduleHeader(moduleHeaderInput{
		unions:        emission.unionState,
		adts:          emission.adtState,
		streams:       emission.streamState,
		equality:      emission.equalityState,
		conversions:   emission.conversionSpecs,
		divisionTypes: emission.divisionTypes,
		shiftSpecs:    emission.shiftSpecs,
		bitCastSpecs:  emission.bitCastSpecs,
		endianSpecs:   emission.endianSpecs,
		objects:       emission.objects,
		printState:    emission.printState,
		concurrency:   emission.concurrencyState,
		io:            emission.ioState,
		stringState:   stringState,
		canonicalID:   canonicalID,
		prototypes:    headerPrototypes.String(),
		extraFrames:   extraFrames.String(),
		rootRun:       isRoot,
		filename:      logicalKey,
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

// emitMainPair writes main.c and main.h from the program-wide aggregate plus
// the entrypoint module's identity. main.c owns the once-per-process runtime
// cores and the C entry point; main.h owns the shared declarations every
// translation unit needs before any module content (RFC 0034).
func emitMainPair(merged *programEmission, root *moduleEmission) (mainC string, mainH string, err error) {
	var mainBody strings.Builder
	// The entrypoint module's TU is the one main.c builds on: it includes
	// main.h and this module's own header.
	mainBody.WriteString("#include \"main.h\"\n#include \"modules/" + root.canonicalID + ".h\"\n\n")
	writeConcurrencyRuntime(&mainBody, merged.concurrencyState, merged.stringState)
	writeIOGate(&mainBody, merged.ioState, merged.concurrencyState != nil && merged.concurrencyState.used)

	mainBody.WriteString("int main(void) {\n")
	if merged.concurrencyState != nil && merged.concurrencyState.used {
		// RFC 0037: the scheduler initializes before any Hexal source runs;
		// the module statements execute as the root Task on worker zero, and
		// hex_task_complete below hands the process back to main.
		mainBody.WriteString("    hex_scheduler_init();\n")
		mainBody.WriteString("    hex_module_root_run();\n")
		// RFC 0037: completing the root Task wakes the scheduler, stops the
		// workers, and switches back to main so it returns normally. Tasks
		// still active are abandoned to process termination, as the spec
		// requires.
		mainBody.WriteString("    hex_task_complete(hex_root_task);\n")
	} else {
		mainBody.WriteString("    return hex_module_root_run();\n")
	}
	mainBody.WriteString("}\n")

	mainHeader := mainHeader(mainHeaderInput{
		float32Used:  merged.float32Used,
		float64Used:  merged.float64Used,
		nilUsed:      merged.nilUsed,
		errorUsed:    merged.errorUsed,
		stdioNeeded:  merged.stdioNeeded,
		heaps:        merged.heapState,
		views:        merged.viewState,
		stringState:  merged.stringState,
		lists:        merged.listState,
		dicts:        merged.dictState,
		arrays:       merged.arrayState,
		concurrency:  merged.concurrencyState,
		io:           merged.ioState,
		sizeLiterals: merged.sizeLiterals,
		filename:     root.logicalKey,
	})
	return mainBody.String(), mainHeader, nil
}

func header(float32Used, float64Used, nilUsed bool, objects []*compilerTypes.ObjectType) string {
	// header is the pre-RFC-0034 test compatibility surface: one module, no
	// machinery state. The program-wide aggregate for such a program is
	// exactly these flags plus empty specialization sets.
	return mainHeader(mainHeaderInput{float32Used: float32Used, float64Used: float64Used, nilUsed: nilUsed, filename: "app.hex"})
}

// mainHeaderInput carries every value the main-header builder consumes. One
// field per argument, no derived or cached state (RFC 0057 Item 6).
type mainHeaderInput struct {
	float32Used  bool
	float64Used  bool
	nilUsed      bool
	errorUsed    bool
	stdioNeeded  bool
	heaps        *heapHelpers
	views        *generatedViewState
	stringState  *generatedStringState
	lists        *generatedListState
	dicts        *generatedDictState
	arrays       *generatedArrayState
	concurrency  *generatedConcurrencyState
	io           *generatedIOState
	sizeLiterals []string
	filename     string
}

// moduleHeaderInput carries every value the module-header builder consumes.
// One field per argument, no derived or cached state (RFC 0057 Item 6).
type moduleHeaderInput struct {
	unions        *generatedUnionState
	adts          *generatedAdtState
	streams       *generatedStreamState
	equality      *generatedEqualityState
	conversions   []conversionSpec
	divisionTypes []compilerTypes.Type
	shiftSpecs    []shiftSpec
	bitCastSpecs  []bitCastSpec
	endianSpecs   []endianSpec
	objects       []*compilerTypes.ObjectType
	printState    *generatedPrintState
	concurrency   *generatedConcurrencyState
	io            *generatedIOState
	stringState   *generatedStringState
	canonicalID   string
	prototypes    string
	extraFrames   string
	rootRun       bool
	filename      string
}

// mainHeader emits main.h: the fixed C23 target-profile preamble, the shared
// value machinery that carries no process-wide state, and the extern
// declarations for the runtime functions the module header's inline helpers
// call. Everything here is included by both translation units, so no
// definition in main.h may hold static storage. The states are the
// program-wide aggregate: every module's built-in machinery must be declared
// here before any module content (RFC 0034 built-in generic ownership).
func mainHeader(input mainHeaderInput) string {
	var result strings.Builder
	result.WriteString(mainHeaderPrefix)
	result.WriteString("\nstatic_assert(CHAR_BIT == 8, \"Hexal requires 8-bit bytes\");\n")
	result.WriteString("static_assert(sizeof(uint8_t) * CHAR_BIT == 8 && UINT8_MAX == 255, \"Hexal requires UInt8\");\n")
	result.WriteString("static_assert(sizeof(uint16_t) * CHAR_BIT == 16 && UINT16_MAX == 65535, \"Hexal requires UInt16\");\n")
	result.WriteString("static_assert(sizeof(uint32_t) * CHAR_BIT == 32 && UINT32_MAX == 4294967295u, \"Hexal requires UInt32\");\n")
	result.WriteString("static_assert(sizeof(uint64_t) * CHAR_BIT == 64 && UINT64_MAX == UINT64_C(18446744073709551615), \"Hexal requires UInt64\");\n")
	result.WriteString("static_assert(sizeof(int8_t) * CHAR_BIT == 8 && INT8_MIN == -128 && INT8_MAX == 127, \"Hexal requires Int8\");\n")
	result.WriteString("static_assert(sizeof(int16_t) * CHAR_BIT == 16 && INT16_MIN == -32768 && INT16_MAX == 32767, \"Hexal requires Int16\");\n")
	result.WriteString("static_assert(sizeof(int32_t) * CHAR_BIT == 32 && INT32_MIN == (-2147483647 - 1) && INT32_MAX == 2147483647, \"Hexal requires Int32\");\n")
	result.WriteString("static_assert(sizeof(int64_t) * CHAR_BIT == 64 && INT64_MIN == (-INT64_C(9223372036854775807) - 1) && INT64_MAX == INT64_C(9223372036854775807), \"Hexal requires Int64\");\n")
	// RFC 0049 item 6: a Size literal above the smallest possible SIZE_MAX
	// (65535) fits only targets whose size_t is wide enough, so each one is
	// guarded against the selected target's actual SIZE_MAX.
	for _, digits := range input.sizeLiterals {
		fmt.Fprintf(&result, "static_assert(%s <= SIZE_MAX, \"Size literal %s requires a size_t target wide enough\");\n", digits, digits)
	}
	// RFC 0010: nullptr_t and the nullptr predefined constant live in
	// <stddef.h>, included only when a written name needs them.
	if input.nilUsed {
		result.WriteString("#include <stddef.h>\n\n")
	}
	if input.arrays != nil && len(input.arrays.order) > 0 || input.views != nil && len(input.views.views) > 0 || input.stringState != nil && input.stringState.used || input.lists != nil && len(input.lists.order) > 0 || input.dicts != nil && len(input.dicts.order) > 0 || input.stdioNeeded {
		// The bounds guards in the array, view, string, and list helpers
		// and the print helpers report through fputs/fwrite on stdout and
		// stderr.
		result.WriteString("#include <stdio.h>\n\n")
	}
	if input.float32Used || input.float64Used {
		result.WriteString("#include <float.h>\n#include <math.h>\n\n")
		result.WriteString("static_assert(FLT_RADIX == 2, \"Hexal requires binary floating point\");\n")
	}
	if input.float32Used {
		result.WriteString("static_assert(sizeof(float) == 4 && FLT_MANT_DIG == 24 && FLT_MAX_EXP == 128, \"Hexal Float32 requires the binary32 value set\");\n")
		result.WriteString("#if !defined(FLT_IS_IEC_60559) || FLT_IS_IEC_60559 != 1\n#error \"Hexal Float32 requires IEC 60559\"\n#endif\n")
	}
	if input.float64Used {
		result.WriteString("static_assert(sizeof(double) == 8 && DBL_MANT_DIG == 53 && DBL_MAX_EXP == 1024, \"Hexal Float64 requires the binary64 value set\");\n")
		result.WriteString("#if !defined(DBL_IS_IEC_60559) || DBL_IS_IEC_60559 != 1\n#error \"Hexal Float64 requires IEC 60559\"\n#endif\n")
	}
	// RFC 0031: the EoS singleton lowers to one compiler-owned byte.
	result.WriteString("typedef uint8_t hex_eos;\n\n")
	writeConcurrencyTypePrelude(&result, input.concurrency)
	writeIOPrelude(&result, input.io)
	// RFC 0034: the module header's inline helpers call the runtime core,
	// which lives in main.c with external linkage. main.h declares those
	// entry points before any module content.
	writeConcurrencyExterns(&result, input.concurrency)
	writeIOExterns(&result, input.io)
	writeHeapDefinitions(&result, input.heaps)
	writeViewDefinitions(&result, input.views)
	writeStringDefinitions(&result, input.stringState)
	if input.errorUsed {
		// RFC 0029: Error's complete definition follows the String and Strand
		// typedefs it needs and precedes the unions that may carry it as a
		// payload member.
		writeErrorDefinition(&result)
	}
	writeListDefinitions(&result, input.lists, input.views)
	writeDictDefinitions(&result, input.dicts)
	writeArrayDefinitions(&result, input.arrays, input.views)
	result.WriteString("\n#endif\n")
	return result.String()
}

// moduleHeader emits one module's header: everything that references the
// module's own types, plus the state-free inline helper families whose
// non-inline cores live in main.c. The guard is the RFC 0034 module header
// guard shape. input.extraFrames holds the entry-adapter argument frames of
// the spawn sites routed to this module; input.rootRun admits the root run
// declaration in the entrypoint module's header only.
func moduleHeader(input moduleHeaderInput) string {
	var result strings.Builder
	result.WriteString("#ifndef " + compilerTypes.ModuleHeaderGuard(input.canonicalID) + "\n#define " + compilerTypes.ModuleHeaderGuard(input.canonicalID) + "\n\n#include \"main.h\"\n")
	writeAdtDefinitions(&result, input.adts)
	writeUnionDefinitions(&result, input.unions)
	writeObjectDefinitions(&result, input.objects, input.filename)
	// The Stream families embed user object State types by value, so they
	// are emitted after every object definition (RFC 0031).
	writeStreamDefinitions(&result, input.streams)
	writeShiftDefinitions(&result, input.shiftSpecs)
	writeBitCastDefinitions(&result, input.bitCastSpecs)
	writeEndianDefinitions(&result, input.endianSpecs)
	writePrintDefinitions(&result, input.printState)
	writeEqualityDefinitions(&result, input.equality)
	writeDivisionDefinitions(&result, input.divisionTypes)
	writeConversionDefinitions(&result, input.conversions)
	writeConcurrencyInlineHelpers(&result, input.concurrency, input.stringState)
	writeIOInlineHelpers(&result, input.io, input.stringState)
	if input.prototypes != "" {
		result.WriteString("\n/* RFC 0034: exported and foreign function prototypes */\n")
		result.WriteString(input.prototypes)
	}
	if input.extraFrames != "" {
		result.WriteString("\n/* RFC 0037: spawn entry adapter argument frames */\n")
		result.WriteString(input.extraFrames)
	}
	if input.rootRun {
		result.WriteString("\nint hex_module_root_run(void);\n")
	}
	result.WriteString("\n#endif\n")
	return result.String()
}

func objectDefinitions(program checker.Program) ([]*compilerTypes.ObjectType, error) {
	objects := make([]*compilerTypes.ObjectType, 0)
	seen := make(map[*compilerTypes.ObjectType]bool)
	seenCNames := make(map[string]*compilerTypes.ObjectType)
	// RFC 0034: a module's header must carry every object type its
	// translation unit can name by value, including imported modules'
	// exported objects referenced through the import alias. They are
	// reachable through the checked statements' types, so the walk collects
	// local and foreign definitions together; each module file includes
	// only its own header plus main.h, so no translation unit ever sees two
	// definitions of one struct.
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
			// RFC 0035: reference-like members (String, List, Dict, Stream)
			// are pointer-sized handles, spelled like their declarations.
			fmt.Fprintf(result, "    %s;\n", declaration(member.Type, PrivateCName(MemberName, member.Name, ""), true))
		}
		fmt.Fprintf(result, "};\n")
	}
}

func usedFloatTypes(program checker.Program) (bool, bool, bool) {
	float32Used, float64Used, nilUsed := false, false, false
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			switch {
			case compilerTypes.Equal(typ, compilerTypes.Float32):
				float32Used = true
			case compilerTypes.Equal(typ, compilerTypes.Float64):
				float64Used = true
			case compilerTypes.IsNil(typ):
				// A written Nil type needs the nullptr_t name from <stddef.h>.
				nilUsed = true
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.NullTestExpression {
				// The test writes the nullptr constant even when no written type
				// needs the nullptr_t name.
				nilUsed = true
			}
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		panic(err)
	}
	return float32Used, float64Used, nilUsed
}
