// Package generator emits readable C23 from checked Hexal data.
package generator

import (
	"hexal/compiler/checker"
)

// Generate is the compatibility wrapper used by package-level callers. The
// compiler itself uses GenerateChecked so an internal generation failure stays
// a structured diagnostic instead of being silently turned into source.
func Generate(program checker.Program) (mainC string, mainH string) {
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		return GenerateFailure()
	}
	return files["main.c"], files["main.h"]
}

// GenerateChecked emits direct C23 scalar and pointer operations. Checked
// literal metadata, not raw source text, is the authority for every
// initializer. RFC 0034: every reachable module emits its own C/header pair
// under modules/<canonical>.c/.h; main.c and main.h host the one-per-process
// runtime and the C entry point. Generation is two-phase: every module is
// discovered and validated first, the built-in machinery is aggregated
// program-wide, and only then is any file text written (RFC 0034: the
// compiler collects every reachable built-in specialization request across
// all modules, sorts it, and emits each exactly once where external identity
// or state is required). Deterministic: the order slice is the canonical
// dependency-first order from the resolver, and every merged collection is
// deduplicated by canonical identity in that order.
func GenerateChecked(programs map[string]checker.Program, order []string, entrypointCanonical string) (map[string]string, error) {
	files := make(map[string]string, 2+2*len(order))
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
		mainC, mainH, mainErr := emitMainPair(merged, root)
		if mainErr != nil {
			return nil, mainErr
		}
		files["main.c"] = mainC
		files["main.h"] = mainH
	}
	return files, nil
}

// GenerateFailure emits a complete C program that reports compilation
// failure, while retaining the target-profile header shape.
func GenerateFailure() (mainC string, mainH string) {
	return "#include \"main.h\"\n\nint main(void) {\n    return EXIT_FAILURE;\n}\n", header(false, false, false, nil)
}
