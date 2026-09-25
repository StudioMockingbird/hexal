// headers.go owns shared header assembly: the input and model records plus
// the hexal.h and per-module header builders.
package generator

import (
	"maps"
	"slices"
	"strings"

	compilerTypes "hexal/compiler/types"
)

// hexalHeaderInput carries the two values consumed by the shared
// program-support header builder.
type hexalHeaderInput struct {
	sizeLiterals []string
	// requirements is the demand-driven standard-header and hex_eos set.
	requirements *cHeaderRequirements
	// tags carries the finalized program-wide discriminants for the hex_tag
	// enum block.
	tags *tagRegistry
}

// moduleHeaderInput carries every value the module-header builder consumes.
// One field per argument, no derived or cached state.
type moduleHeaderInput struct {
	unions      *generatedUnionState
	adts        *generatedAdtState
	equality    *generatedEqualityState
	objects     []*compilerTypes.ObjectType
	heaps       *heapHelpers
	printState  *generatedPrintState
	streams     *generatedStreamState
	time        *generatedTimeState
	files       *generatedFileState
	text        *generatedTextState
	network     *generatedNetworkState
	process     *generatedProcessState
	signal      *generatedSignalState
	terminal    *generatedTerminalState
	corelib     *generatedCorelibState
	concurrency *generatedConcurrencyState
	// event selects the Task-parking form of a core-library adapter,
	// program-wide and compile-time, matching every other bridged family.
	event       bool
	stringState *literalRegistry
	tags        *tagRegistry
	canonicalID string
	// Collection states feed the module-owned specialization region; the
	// program-wide component partition keeps the builtin-element records.
	slices *generatedSliceState
	arrays *generatedArrayState
	lists  *generatedListState
	dicts  *generatedDictState
	pools  *generatedPoolState
	stash  *stashHelpers

	prototypes  string
	extraFrames string
	filename    string
	// components are the path-qualified component headers this module needs,
	// in dependency order.
	components []string
	// foreignHeaders are the exact C include directives this module requires,
	// in first-use order. They follow the component includes and precede the
	// declarations that name a foreign type.
	foreignHeaders []string
}

// Render models for the module header shell, the root main scaffolding, and
// the object declarations embedded in a module header. Every value is decided
// in Go before rendering; the templates hold presentation only.
type moduleHeaderOpenModel struct {
	Guard          string
	Components     []string
	ForeignHeaders []string
}

type moduleHeaderCloseModel struct {
	Prototypes  string
	ExtraFrames string
}

type moduleIncludeModel struct {
	Stem string
}

type rootArgSetupModel struct {
	Windows string
	POSIX   string
}

type rootEnvDeclModel struct {
	Name string
}

type objectForwardModel struct {
	CName    string
	Filename string
	Line     int
}

type objectMemberModel struct {
	Declaration string
	Filename    string
	Line        int
}

type objectBodyModel struct {
	CName    string
	Filename string
	Line     int
	Empty    bool
	Members  []objectMemberModel
}

// hexalHeaderModel is the render model for the hexal.h template:
// the guard, the demand-driven program-wide standard-header umbrella, the
// retained source-dependent Size-literal assertions, the hex_eos typedef when
// required, and the extern declaration of the one program-wide diagnostic
// trap.
type hexalHeaderModel struct {
	Includes     []string
	SizeAsserts  []string
	Eos          bool
	TrapDeclared bool
	// NativeDeclared declares the program-wide libuv bootstrap.
	NativeDeclared bool
	// Tags are the finalized program-wide discriminant constants, in enum
	// order; empty when no reachable general union or ADT exists.
	Tags []string
}

// hexalHeader emits hexal.h. Everything here is included by every translation
// unit through each module header, so no definition in hexal.h may hold
// static storage. The standard-header set satisfies the complete reachable
// generated program. Generic toolchain qualification is a supported-toolchain
// contract, not a generated probe; only source-dependent target assertions
// are emitted. The source of truth for the shell is packages/hexal.h; the
// state data is the program-wide aggregate.
func hexalHeader(input hexalHeaderInput) (string, error) {
	model := hexalHeaderModel{SizeAsserts: input.sizeLiterals, Tags: input.tags.constantNames()}
	if input.requirements != nil {
		headers := slices.Sorted(maps.Keys(input.requirements.headers))
		model.Includes = headers
		model.Eos = input.requirements.eos
		model.TrapDeclared = input.requirements.trap
		model.NativeDeclared = input.requirements.native
	}
	return renderComponent(componentArtifact{key: "hexal.h", template: "hexal.h", model: model})
}

// moduleHeader emits one module's header: everything that references the
// module's own types, plus the state-free inline helper families whose
// non-inline cores live in the root module's C file. input.extraFrames
// holds the entry-adapter argument frames of the spawn sites routed to this
// module.
func moduleHeader(input moduleHeaderInput) (string, error) {
	var result strings.Builder
	// Component includes follow hexal.h in dependency order; foreign
	// includes follow the components and precede any declaration that names
	// a foreign type, each exactly once in first-use order.
	if err := renderInto(&result, "module.h", "module_header_open", moduleHeaderOpenModel{
		Guard:          compilerTypes.ModuleHeaderGuard(input.canonicalID),
		Components:     input.components,
		ForeignHeaders: input.foreignHeaders,
	}); err != nil {
		return "", err
	}
	// Forward typedefs for every object, ADT, and union come first,
	// regardless of any cross-reference between them: a pointer-typed member
	// naming any of them needs only this forward name. Full bodies then
	// follow in by-value dependency order (see writeNominalBodies), since an
	// ADT payload field or a non-nullable structural union's payload member
	// can name a nominal object type by value, and an object member can just
	// as well name an ADT or non-nullable union type by value; either
	// direction requires that member's own complete definition already in
	// scope, and a fixed category order cannot satisfy both directions when
	// a program uses each at once.
	if err := writeObjectForwardDeclarations(&result, input.objects, input.filename); err != nil {
		return "", err
	}
	if err := writeAdtForwardDeclarations(&result, input.adts); err != nil {
		return "", err
	}
	if err := writeUnionForwardDeclarations(&result, input.unions); err != nil {
		return "", err
	}
	if err := writeNominalBodies(&result, input.objects, input.adts, input.unions, input.filename, input.tags); err != nil {
		return "", err
	}
	if err := writeUnionDefinitions(&result, input.unions, input.tags); err != nil {
		return "", err
	}
	// Module-owned collection specializations follow their element
	// definitions: component artifacts are program-wide and cannot declare
	// per-module types, so each consuming module re-emits the specializations
	// it needs into its own header.
	if err := writeModuleCollectionSpecializations(&result, &input); err != nil {
		return "", err
	}
	// The typed heap allocation helpers reference module-owned element
	// types, so they follow the object definitions.
	if err := writeHeapAllocateHelpers(&result, input.heaps); err != nil {
		return "", err
	}
	if err := writeStashHelpers(&result, input.stash); err != nil {
		return "", err
	}
	if err := writePrintDefinitions(&result, input.printState, input.tags); err != nil {
		return "", err
	}
	if err := writeEqualityDefinitions(&result, input.equality, input.tags); err != nil {
		return "", err
	}
	if err := writeConcurrencyInlineHelpers(&result, input.concurrency, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeStreamInlineHelpers(&result, input.streams, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeTimeInlineHelpers(&result, input.time, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeFileInlineHelpers(&result, input.files, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeTextInlineHelpers(&result, input.text, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeNetworkInlineHelpers(&result, input.network, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeProcessInlineHelpers(&result, input.process, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeSignalInlineHelpers(&result, input.signal, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeTerminalInlineHelpers(&result, input.terminal, input.stringState, input.tags); err != nil {
		return "", err
	}
	if err := writeCorelibInlineHelpers(&result, input.corelib, input.stringState, input.tags, input.event); err != nil {
		return "", err
	}
	if err := renderInto(&result, "module.h", "module_header_close", moduleHeaderCloseModel{
		Prototypes:  input.prototypes,
		ExtraFrames: input.extraFrames,
	}); err != nil {
		return "", err
	}
	return result.String(), nil
}
