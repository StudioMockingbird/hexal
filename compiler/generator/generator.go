// Package generator emits readable C23 from checked Hexal data.
package generator

import (
	"hexal/compiler/checker"
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
func GenerateChecked(programs map[string]checker.Program, order []string, entrypointCanonical string) (map[string]string, error) {
	files := make(map[string]string, 1+2*len(order))
	modules := make([]*moduleEmission, 0, len(order))
	for _, canonical := range order {
		key := canonical + ".hex"
		program, ok := programs[key]
		if !ok {
			key = canonical
			program, ok = programs[key]
		}
		if !ok {
			continue
		}
		emission, discoveryErr := discoverModuleEmission(program, canonical, key)
		if discoveryErr != nil {
			return nil, discoveryErr
		}
		modules = append(modules, emission)
	}
	merged := mergeProgramEmission(modules)
	var root *moduleEmission
	for _, emission := range modules {
		isRoot := emission.canonicalID == entrypointCanonical
		moduleC, moduleH, emissionErr := emitModulePair(emission, merged, isRoot)
		if emissionErr != nil {
			return nil, emissionErr
		}
		files["modules/"+emission.canonicalID+".c"] = moduleC
		files["modules/"+emission.canonicalID+".h"] = moduleH
		if isRoot {
			root = emission
		}
	}
	if root != nil {
		files["hexal.h"] = hexalHeader(hexalHeaderInput{
			errorUsed:    merged.errorUsed,
			heaps:        merged.heapState,
			views:        merged.viewState,
			stringState:  merged.stringState,
			lists:        merged.listState,
			dicts:        merged.dictState,
			arrays:       merged.arrayState,
			concurrency:  merged.concurrencyState,
			wrap:         merged.wrapState,
			sizeLiterals: merged.sizeLiterals,
			requirements: merged.requirements,
		})
	}
	return files, nil
}
