// emission.go owns one module's C/header pair emission: the moduleEmission
// record and shared header-requirement type, emitModulePair, and the root
// entry assembly it writes.
package generator

import (
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// cHeaderRequirements is the program-wide demand-driven standard-header set,
// EoS representation requirement, and runtime-trap requirement. Standard
// headers are rendered once, in lexical order, immediately after the HEXAL_H
// guard; the hex_eos typedef is rendered before any type that references it.
type cHeaderRequirements struct {
	headers map[string]bool
	eos     bool
	trap    bool
	// native selects the program-wide libuv bootstrap: its hexal.h
	// declaration, its hexal/runtime.c definition, and the root main call.
	native bool
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

	errorUsed   bool
	unionState  *generatedUnionState
	heapState   *heapHelpers
	adtState    *generatedAdtState
	arrayState  *generatedArrayState
	sliceState  *generatedSliceState
	stringState *literalRegistry
	stringUsed  bool // module-local text dependency selection
	// interpolationUsed is true when this module contains a checked
	// String.interpolate call, selecting the String component's demand-driven
	// formatter helpers.
	interpolationUsed bool
	listState         *generatedListState
	dictState         *generatedDictState
	equalityState     *generatedEqualityState
	conversionSpecs   []conversionSpec
	sizeLiterals      []string
	divisionTypes     []compilerTypes.Type
	shiftSpecs        []shiftSpec
	bitCastSpecs      []bitCastSpec
	endianSpecs       []endianSpec
	printState        *generatedPrintState
	ioState           *generatedStreamState
	timeState         *generatedTimeState
	fileState         *generatedFileState
	textState         *generatedTextState
	networkState      *generatedNetworkState
	processState      *generatedProcessState
	signalState       *generatedSignalState
	terminalState     *generatedTerminalState
	corelibState      *generatedCorelibState
	// rootReturn is true when this module's root scope contains a checked
	// root return, selecting the entry status slot and cleanup label.
	rootReturn       bool
	concurrencyState *generatedConcurrencyState
	wrapState        *generatedWrapState
	stashState       *stashHelpers
	poolState        *generatedPoolState
	objects          []*compilerTypes.ObjectType
}

// emitModulePair writes one module's C/header pair from its own discovery
// states plus the program-wide aggregate. The entrypoint module (isRoot)
// additionally carries the process-wide runtime cores and the C entry point;
// the entry adapters of every spawn site routed to this module are emitted
// after the function definitions they call; all literal references use the
// program-wide table.
//
// Definition order within the module body: local named function and
// anonymous literal helper prototypes first, then ordinary module function
// and method prototypes, then concrete generic specialization prototypes --
// every prototype the module can need exists before any definition, so an
// ordinary definition's body may already call a specialization a later
// ordinary definition is the first to require -- then ordinary module
// function and method definitions in source order, then concrete generic
// specialization definitions, then the local/anonymous helper definitions
// themselves, then (root only) the entry point. A helper may therefore call
// its enclosing named function - already fully defined by the time helpers
// are emitted - and an enclosing function may call its own helper through
// the prototype emitted up front; every helper is static and carries no
// owner encoding, since collectLocalHelpers's ordinal alone is unique
// within the module.
func emitModulePair(emission *moduleEmission, merged *programEmission, isRoot bool, config Config) (moduleC string, moduleH string, err error) {
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
	if err := renderInto(&moduleBody, "module.c", "module_include", moduleIncludeModel{Stem: compilerTypes.ModuleArtifactStem(canonicalID)}); err != nil {
		return "", "", err
	}

	// The entry environment type precedes every prototype and definition that
	// names it, and never appears in a generated header.
	if err := writeEntryEnvironmentType(&moduleBody, program); err != nil {
		return "", "", err
	}

	// Module value definitions precede every function/method definition and
	// prototype in this file: their static initializers reference no other
	// declaration, so ordinary C forward-declaration concerns do not apply,
	// but every function in this module may read or write them from the
	// first line of the file onward.
	moduleValueRenderState := newExpressionValidation()
	moduleValueRenderState.functions = functions
	moduleValueRenderState.methods = methods
	moduleValueRenderState.generatedTypes = typeState
	moduleValueRenderState.strings = stringState
	moduleValueRenderState.tags = merged.tags
	moduleValueRenderState.owner = owner
	moduleValueRenderState.filename = logicalKey
	moduleValueRenderState.moduleID = canonicalID
	moduleValueRenderState.table = config.SourceTable
	if err := writeModuleValueDefinitions(&moduleBody, program.ModuleValues, owner, moduleValueRenderState); err != nil {
		return "", "", err
	}

	// Function definitions sit at file scope in source order, after the
	// object definitions the header already carries. Module-level
	// visibility is order-independent, so a private function or method may
	// call or mutually recurse with another declared later; writeModulePrototypes
	// below gives every one of them a static prototype ahead of the
	// definitions for that to compile. Functions the module's own spawn
	// prologues name keep external linkage; everything else stays static
	// inside the module C file.
	spawned := make(map[string]bool)
	if emission.concurrencyState != nil {
		for _, site := range emission.concurrencyState.spawns {
			spawned[site.function] = true
		}
	}
	definitions := definitionContext{
		body:         &moduleBody,
		functions:    functions,
		methods:      methods,
		typeState:    typeState,
		strings:      stringState,
		owner:        owner,
		filename:     logicalKey,
		tags:         merged.tags,
		envFunctions: entryEnvironmentFunctions(program),
		envMethods:   entryEnvironmentMethods(program),
		table:        config.SourceTable,
	}
	// Local named function and anonymous literal helpers get one shared
	// module-local ordinal stream. Their prototypes are emitted first, so an
	// ordinary function or method defined below can already call one; their
	// definitions follow the ordinary and specialized definitions, before
	// the root main body.
	localHelpers, localHelpersErr := collectLocalHelpers(program)
	if localHelpersErr != nil {
		return "", "", localHelpersErr
	}
	if err := writeLocalHelperPrototypes(&moduleBody, localHelpers, typeState); err != nil {
		return "", "", err
	}
	if err := writeModulePrototypes(&moduleBody, program, owner); err != nil {
		return "", "", err
	}
	// Concrete specialization prototypes are emitted here, before any
	// definition: an ordinary function's body (drain<S>'s caller, say) may
	// call a specialization that only a later ordinary function first
	// demands, and C23 rejects a call with no declaration in scope. Their
	// definitions still follow every ordinary definition, since a
	// specialization's body can call a function declared before its generic
	// template (already defined by then) or another specialization (any
	// order via its own prototype here).
	if err := writeSpecializedPrototypes(&moduleBody, program.SpecializedFunctions, program.SpecializedMethods, typeState, owner); err != nil {
		return "", "", err
	}
	for _, statement := range program.Statements {
		switch declared := statement.(type) {
		case checker.FunctionDeclaration:
			if definitionErr := definitions.writeFunctionDefinition(declared, spawned[privateCName(functionNameKind, declared.Name, owner)]); definitionErr != nil {
				return "", "", definitionErr
			}
		case checker.MethodDeclaration:
			if definitionErr := definitions.writeMethodDefinition(declared); definitionErr != nil {
				return "", "", definitionErr
			}
		}
	}

	if err := definitions.writeSpecializedDefinitions(program.SpecializedFunctions, program.SpecializedMethods); err != nil {
		return "", "", err
	}

	// Local helper definitions follow the ordinary and specialized
	// definitions and precede the root main body: an enclosing named
	// function's own definition, already emitted above, can therefore
	// reference its helper only through the prototype emitted earlier, and
	// a helper can call its enclosing named function because that
	// definition already exists by this point.
	if err := writeLocalHelperDefinitions(definitions, localHelpers); err != nil {
		return "", "", err
	}

	// The spawn entry adapters follow every function definition because they
	// call the spawned functions directly. The adapter of a spawn lives
	// beside the spawned function's own definition, so the call never
	// crosses a translation unit.
	if err := writeSpawnAdapters(&moduleBody, merged.adapterSites[canonicalID]); err != nil {
		return "", "", err
	}

	renderState := newExpressionValidation()
	renderState.functions = functions
	renderState.methods = methods
	renderState.generatedTypes = typeState
	renderState.strings = stringState
	renderState.tags = merged.tags
	renderState.owner = owner
	renderState.filename = logicalKey
	renderState.moduleID = canonicalID
	renderState.table = config.SourceTable
	renderState.envFunctions = entryEnvironmentFunctions(program)
	renderState.envMethods = entryEnvironmentMethods(program)
	if isRoot {
		// The selected root module's C file owns the process entry point.
		// Runtime definitions and state live in the component artifacts
		// under generated hexal/; the module C file is not the
		// fallback runtime container.
		renderState.pushScope()
		// The module statements execute directly inside main(). Module-level
		// storage stays inside main; a function body cannot reach it, so
		// nothing is promoted to static storage duration. With concurrency
		// the statements run as the root task between scheduler
		// initialization and hex_task_complete; without it they run before
		// main returns C's recorded status directly. No non-root module ever
		// declares or defines main() or process-wide runtime state.
		argumentsReachable := merged.corelibState != nil && merged.corelibState.arguments
		executableReachable := merged.corelibState != nil && merged.corelibState.executable
		entryAdapter := argumentsReachable || executableReachable
		if executableReachable {
			// uv_setup_args must run after the native bootstrap and before the
			// first uv_exepath. Declaring it privately keeps <uv.h> and every
			// libuv name out of the generated module headers.
			if err := renderInto(&moduleBody, "module.c", "root_uv_setup_decl", struct{}{}); err != nil {
				return "", "", err
			}
		}
		if err := writeRootEntrySignature(&moduleBody, config, entryAdapter); err != nil {
			return "", "", err
		}
		if merged.requirements != nil && merged.requirements.native {
			// The native bootstrap precedes every module statement and the
			// scheduler, so no libuv call can run before its allocator.
			if err := renderInto(&moduleBody, "module.c", "root_native_init", struct{}{}); err != nil {
				return "", "", err
			}
		}
		if executableReachable {
			// The returned pointer is authoritative for POSIX argument
			// conversion only when arguments are reachable; otherwise the
			// call exists solely to satisfy uv_setup_args's required ordering.
			posixSetup := "(void)uv_setup_args(argc, argv);"
			if argumentsReachable {
				posixSetup = "argv = uv_setup_args(argc, argv);"
			}
			if err := writeRootArgumentSetup(&moduleBody, config, "(void)uv_setup_args(__argc, __argv);", posixSetup); err != nil {
				return "", "", err
			}
		}
		if argumentsReachable {
			if err := writeRootArgumentSetup(&moduleBody, config, "hex_program_arguments_init();", "hex_program_arguments_init(argc, argv);"); err != nil {
				return "", "", err
			}
		}
		if emission.rootReturn {
			// One status slot owns the process exit classification; a root
			// return assigns it and jumps to the single cleanup label.
			if err := renderInto(&moduleBody, "module.c", "root_exit_status", struct{}{}); err != nil {
				return "", "", err
			}
		}
		if handleSelected(merged) {
			// The handle registry backs every copied-handle capability's
			// resolve, so it must exist before any module statement runs.
			if err := renderInto(&moduleBody, "module.c", "root_handle_init", struct{}{}); err != nil {
				return "", "", err
			}
		}
		if merged.concurrencyState != nil && merged.concurrencyState.used {
			if err := renderInto(&moduleBody, "module.c", "root_scheduler_init", struct{}{}); err != nil {
				return "", "", err
			}
		}
		if len(program.EntryCaptures) > 0 {
			// The one environment instance is an automatic local in main; no
			// mutable C file-scope object exists.
			if err := renderInto(&moduleBody, "module.c", "root_env_decl", rootEnvDeclModel{Name: entryEnvironmentName}); err != nil {
				return "", "", err
			}
			renderState.envPointer = "&env"
		}
		if statementErr := writeStatements(&moduleBody, program.Statements, renderState, nil, false, program.Defers); statementErr != nil {
			return "", "", statementErr
		}
		if scopeErr := renderState.requireRootScope("root body"); scopeErr != nil {
			return "", "", scopeErr
		}
		if emission.rootReturn {
			// The cleanup label precedes the shared epilogue so an early root
			// return still completes the root Task before C returns.
			if err := renderInto(&moduleBody, "module.c", "root_exit_label", struct{}{}); err != nil {
				return "", "", err
			}
		}
		if merged.concurrencyState != nil && merged.concurrencyState.used {
			// Completing the root Task wakes the scheduler, stops the
			// workers, and switches back to main so it returns normally.
			// Tasks still active are abandoned to process termination.
			if err := renderInto(&moduleBody, "module.c", "root_task_complete", struct{}{}); err != nil {
				return "", "", err
			}
		}
		if emission.rootReturn {
			if err := renderInto(&moduleBody, "module.c", "root_return_status", struct{}{}); err != nil {
				return "", "", err
			}
		} else {
			if err := renderInto(&moduleBody, "module.c", "root_return_ok", struct{}{}); err != nil {
				return "", "", err
			}
		}
	}

	// The module's own header declares its exported declarations and every
	// foreign symbol it calls; importers never include another module's
	// header. Every module header includes hexal.h for the shared
	// program-support contract. The entry adapters routed to this module
	// read their argument frames, so those frames are declared here too,
	// self-contained in the owning module's translation unit.
	var headerPrototypes strings.Builder
	if err := writeExportedPrototypes(&headerPrototypes, program, owner); err != nil {
		return "", "", err
	}
	if err := writeExportedModuleValueDeclarations(&headerPrototypes, program.ModuleValues, owner); err != nil {
		return "", "", err
	}
	if err := writeForeignPrototypes(&headerPrototypes, program, renderState); err != nil {
		return "", "", err
	}
	var extraFrames strings.Builder
	if err := writeSpawnArgFrames(&extraFrames, routedFrames(emission, merged.adapterSites[canonicalID])); err != nil {
		return "", "", err
	}

	foreignIncludes, foreignErr := moduleForeignIncludes(emission.program, merged.foreignIndex)
	if foreignErr != nil {
		return "", "", foreignErr
	}
	moduleHeader, headerErr := moduleHeader(moduleHeaderInput{
		unions:         emission.unionState,
		adts:           emission.adtState,
		equality:       emission.equalityState,
		objects:        emission.objects,
		heaps:          emission.heapState,
		printState:     emission.printState,
		streams:        emission.ioState,
		time:           emission.timeState,
		files:          emission.fileState,
		text:           emission.textState,
		network:        emission.networkState,
		process:        emission.processState,
		signal:         emission.signalState,
		terminal:       emission.terminalState,
		corelib:        emission.corelibState,
		concurrency:    emission.concurrencyState,
		event:          eventSelected(merged),
		stringState:    stringState,
		tags:           merged.tags,
		slices:         emission.sliceState,
		arrays:         emission.arrayState,
		lists:          emission.listState,
		dicts:          emission.dictState,
		pools:          emission.poolState,
		stash:          emission.stashState,
		canonicalID:    canonicalID,
		prototypes:     headerPrototypes.String(),
		extraFrames:    extraFrames.String(),
		filename:       logicalKey,
		components:     moduleComponentHeaders(emission),
		foreignHeaders: foreignIncludes,
	})
	if headerErr != nil {
		return "", "", headerErr
	}
	return moduleBody.String(), moduleHeader, nil
}

// writeRootEntrySignature emits the root entry function's signature. A
// program with no host-invocation demand keeps the unconditional
// int main(void). The qualified Windows profile always keeps int main(void)
// and reads the MinGW CRT globals; a POSIX profile widens to argc/argv.
// Host-neutral output spells both under #if defined(_WIN32) and lets the C
// compiler choose.
func writeRootEntrySignature(body *strings.Builder, config Config, demand bool) error {
	block := ""
	switch {
	case !demand, targetIsWindows(config):
		block = "root_entry_void"
	case config.Target != "":
		block = "root_entry_args"
	default:
		block = "root_entry_dual"
	}
	return renderInto(body, "module.c", block, struct{}{})
}

// writeRootArgumentSetup emits one host-invocation setup statement, keeping
// the Windows and POSIX spellings under the same target selection as the
// entry signature.
func writeRootArgumentSetup(body *strings.Builder, config Config, windows, posix string) error {
	block := ""
	switch {
	case targetIsWindows(config):
		block = "root_arg_windows"
	case config.Target != "":
		block = "root_arg_posix"
	default:
		block = "root_arg_dual"
	}
	return renderInto(body, "module.c", block, rootArgSetupModel{Windows: windows, POSIX: posix})
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
// module's header includes, in dependency order: wrap, heap, slice,
// string, error, seek, concurrency, stash, pool, list, dict, array. Each
// migrated family selects itself here; a family still owned by hexal.h
// during the component migration contributes nothing.
//
// concurrency precedes the generic containers (list, dict, array) because a
// List<Task<T>>, Dict<K, Channel<T>>, or Array<Task<T>, N> specialization
// spells its element type as Task/Channel's per-instantiation typedef
// (hex_task_T / hex_chan_T), and that typedef is declared in
// hexal/concurrency.h, not defined by the container itself; the container's
// header must be able to see it.
func moduleComponentHeaders(emission *moduleEmission) []string {
	var components []string
	components = append(components, moduleWrapComponent(emission)...)
	components = append(components, moduleHeapComponent(emission)...)
	components = append(components, moduleSliceComponent(emission)...)
	components = append(components, moduleStringComponent(emission)...)
	components = append(components, moduleErrorComponent(emission)...)
	components = append(components, moduleCorelibComponent(emission)...)
	components = append(components, moduleSeekComponent(emission)...)
	components = append(components, moduleConcurrencyComponent(emission)...)
	components = append(components, moduleStashComponent(emission)...)
	components = append(components, modulePoolComponent(emission)...)
	components = append(components, moduleListComponent(emission)...)
	components = append(components, moduleDictComponent(emission)...)
	components = append(components, moduleArrayComponent(emission)...)
	components = append(components, moduleNumericComponent(emission)...)
	components = append(components, modulePrintComponent(emission)...)
	components = append(components, moduleStreamComponent(emission)...)
	components = append(components, moduleFileComponent(emission)...)
	components = append(components, moduleNetworkComponent(emission)...)
	components = append(components, moduleProcessComponent(emission)...)
	components = append(components, moduleSignalComponent(emission)...)
	components = append(components, moduleTerminalComponent(emission)...)
	components = append(components, moduleTimeComponent(emission)...)
	components = append(components, moduleEqualityComponent(emission)...)
	return components
}
