// Package generator emits readable C23 from checked Hexal data.
package generator

import (
	"errors"
	"fmt"

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
// canonical identity in that order.
func GenerateChecked(graph *checker.ModuleGraph, programs map[string]checker.Program) (map[string]string, error) {
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
			return nil, fmt.Errorf("generator: the graph names module %s at source key %s, but no checked program has that key", canonical, key)
		}
		emission, discoveryErr := discoverModuleEmission(program, canonical, key, literals)
		if discoveryErr != nil {
			return nil, stampModule(discoveryErr, key)
		}
		modules = append(modules, emission)
	}
	merged, mergeErr := mergeProgramEmission(modules, literals)
	if mergeErr != nil {
		return nil, mergeErr
	}
	var root *moduleEmission
	for _, emission := range modules {
		isRoot := emission.canonicalID == entrypointCanonical
		moduleC, moduleH, emissionErr := emitModulePair(emission, merged, isRoot)
		if emissionErr != nil {
			return nil, stampModule(emissionErr, emission.logicalKey)
		}
		files["modules/"+emission.canonicalID+".c"] = moduleC
		files["modules/"+emission.canonicalID+".h"] = moduleH
		if isRoot {
			root = emission
		}
	}
	if root == nil {
		// The entrypoint module is always emitted; its absence means the
		// caller's order or program keys disagree with the root name, a
		// generation defect, never a quiet hexal.h-less success.
		return nil, fmt.Errorf("generator: the entrypoint module %s is not among the emitted modules", entrypointCanonical)
	}
	header, headerErr := hexalHeader(hexalHeaderInput{
		sizeLiterals: merged.sizeLiterals,
		requirements: merged.requirements,
	})
	if headerErr != nil {
		return nil, headerErr
	}
	files["hexal.h"] = header
	// The demand-driven runtime components render after every
	// module pair; a component key colliding with an existing artifact is an
	// internal error, never a silent overwrite.
	components, componentErr := renderComponentArtifacts(merged)
	if componentErr != nil {
		return nil, componentErr
	}
	for key, content := range components {
		if _, exists := files[key]; exists {
			return nil, fmt.Errorf("generator: duplicate generated artifact key %s", key)
		}
		files[key] = content
	}
	return files, nil
}

// stampModule attributes a generation error to the module being emitted.
// Discovery and emission construct diagnostics without knowing which module
// they are in; the per-module loops are where that is known (RFC 0074 R11).
func stampModule(err error, logicalKey string) error {
	if err == nil {
		return nil
	}
	var diagnostics compilerTypes.Diagnostics
	if errors.As(err, &diagnostics) {
		return diagnostics.InModule(logicalKey)
	}
	var diagnostic compilerTypes.Diagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.InModule(logicalKey)
	}
	return err
}
