// Package generator emits readable C23 from checked Hexal data.
package generator

import (
	"hexal/compiler/checker"
)

// GenerateChecked emits direct C23 scalar and pointer operations. Checked
// literal metadata, not raw source text, is the authority for every
// initializer. RFC 0034: every reachable module emits its own C/header pair
// under modules/<canonical>.c/.h; RFC 0060: the shared program-support
// header is hexal.h and the selected root module's C file owns the
// once-per-process runtime and the process entry point. Generation is
// two-phase: every module is discovered and validated first, the built-in
// machinery is aggregated program-wide, and only then is any file text
// written (RFC 0034: the compiler collects every reachable built-in
// specialization request across all modules, sorts it, and emits each exactly
// once where external identity or state is required). Deterministic: the
// order slice is the canonical dependency-first order from the resolver, and
// every merged collection is deduplicated by canonical identity in that
// order.
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
			sizeLiterals: merged.sizeLiterals,
			requirements: merged.requirements,
		})
	}
	return files, nil
}
