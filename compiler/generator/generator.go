// Package generator emits readable C23 from checked Hexal data.
package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// GenerateChecked emits every reachable module's C/header pair under
// modules/<canonical>.c/.h, the shared program-support header hexal.h, and
// the process entry point in the selected root module's C file. Checked
// literal metadata, not raw source text, is the authority for every
// initializer. Generation is two-phase: every module is discovered and
// validated first, the built-in machinery is aggregated program-wide, and
// only then is any file text written, so each reachable built-in
// specialization is emitted exactly once where external identity or state is
// required. Deterministic: the order slice is the canonical dependency-first
// order from the resolver, and every merged collection is deduplicated by
// canonical identity in that order. config carries the build-time settings
// that reach the generated runtime; its zero value selects the defaults.
//
// GenerationResult contains generated artifacts and path-free native runtime
// requirements selected by the same demand facts that emitted the artifacts.
type GenerationResult struct {
	Files        map[string]string
	Dependencies []string
}

// GenerateChecked preserves the original artifact-only generator API for
// generator tests and internal callers that do not consume build metadata.
func GenerateChecked(graph *checker.ModuleGraph, programs map[string]checker.Program, config Config) (map[string]string, error) {
	result, err := GenerateCheckedWithMetadata(graph, programs, config)
	if err != nil {
		return nil, err
	}
	return result.Files, nil
}

// GenerateCheckedWithMetadata emits artifacts and the deterministic native
// dependency identities required to compile and link those artifacts.
func GenerateCheckedWithMetadata(graph *checker.ModuleGraph, programs map[string]checker.Program, config Config) (GenerationResult, error) {
	files := make(map[string]string, 1+2*len(graph.Order))
	modules := make([]*moduleEmission, 0, len(graph.Order))
	literals := newLiteralRegistry()
	entrypointCanonical := graph.Root
	for _, canonical := range graph.Order {
		key := graph.Modules[canonical].LogicalKey
		program, ok := programs[key]
		if !ok {
			// CheckModules emits one entry per graph node, so this lookup is
			// total by construction; a caller that assembled the checked map
			// independently of the graph gets a diagnostic, never a silently
			// omitted module.
			return GenerationResult{}, generatorDiagnostic()
		}
		emission, discoveryErr := discoverModuleEmission(program, canonical, key, literals, config.SourceTable)
		if discoveryErr != nil {
			return GenerationResult{}, compilerTypes.StampModule(discoveryErr, key)
		}
		modules = append(modules, emission)
	}
	merged, mergeErr := mergeProgramEmission(modules, literals)
	if mergeErr != nil {
		return GenerationResult{}, mergeErr
	}
	merged.foreignIndex = buildForeignIndex(graph, programs)
	var root *moduleEmission
	for _, emission := range modules {
		isRoot := emission.canonicalID == entrypointCanonical
		moduleC, moduleH, emissionErr := emitModulePair(emission, merged, isRoot, config)
		if emissionErr != nil {
			return GenerationResult{}, compilerTypes.StampModule(emissionErr, emission.logicalKey)
		}
		stem := compilerTypes.ModuleArtifactStem(emission.canonicalID)
		files[stem+".c"] = moduleC
		files[stem+".h"] = moduleH
		if isRoot {
			root = emission
		}
	}
	if root == nil {
		// The entrypoint module is always emitted; its absence means the
		// caller's order or program keys disagree with the root name, a
		// generation defect, never a quiet hexal.h-less success.
		return GenerationResult{}, generatorDiagnostic()
	}
	header, headerErr := hexalHeader(hexalHeaderInput{
		sizeLiterals: merged.sizeLiterals,
		requirements: merged.requirements,
		tags:         merged.tags,
	})
	if headerErr != nil {
		return GenerationResult{}, headerErr
	}
	files["hexal.h"] = header
	// The demand-driven runtime components render after every
	// module pair; a component key colliding with an existing artifact is an
	// internal error, never a silent overwrite.
	components, componentErr := renderComponentArtifacts(merged, config)
	if componentErr != nil {
		return GenerationResult{}, componentErr
	}
	for key, content := range components {
		if _, exists := files[key]; exists {
			return GenerationResult{}, generatorDiagnostic()
		}
		files[key] = content
	}
	if tagErr := merged.tags.settled(); tagErr != nil {
		return GenerationResult{}, tagErr
	}
	selected, selectionErr := selectedComponentIDs(components)
	if selectionErr != nil {
		return GenerationResult{}, selectionErr
	}
	dependencies, dependencyErr := selectedRuntimeDependencies(selected, merged)
	if dependencyErr != nil {
		return GenerationResult{}, dependencyErr
	}
	return GenerationResult{Files: files, Dependencies: dependencies}, nil
}
