package integration

// Import resolution and the dependency graph drive this facet: user-program
// diagnostics (resolution and checker errors) always surface before
// generation, and a clean multi-module program compiles to one C/header pair
// per reachable module.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func compileMulti(sources map[string]string, entrypoint string) compiler.CompilationResult {
	return compiler.Compile(sources, entrypoint)
}

func wantStderr(t *testing.T, result compiler.CompilationResult, want ...string) {
	t.Helper()
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("expected failure, got %#v", result)
	}
	message := strings.Join(result.Stderr, "\n")
	for _, want := range want {
		if !strings.Contains(message, want) {
			t.Fatalf("stderr = %#v, want a diagnostic containing %q", result.Stderr, want)
		}
	}
}

func TestMultiModuleCleanProgramsGenerate(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantMultiSuccess(t, result, "app", "math")
	if len(result.Stderr) != 0 {
		t.Fatalf("stderr = %#v, want no diagnostics", result.Stderr)
	}
}

func TestRelativeImportsResolve(t *testing.T) {
	sources := map[string]string{
		"app.hex":             "module Tools = import \"./libs/tools\"\n",
		"libs/tools.hex":      "module Shared = import \"../shared\"\n",
		"shared.hex":          "fun shared_helper(): Int32 do\n    return 1\nend\n",
		"graphics/shapes.hex": "fun area(): Int32 do\n    return 1\nend\n",
	}
	// The nested and the ./graphics/shapes spelling both canonicalize; the
	// unreachable graphics/shapes module contributes no artifacts.
	result := compileMulti(sources, "app.hex")
	wantMultiSuccess(t, result, "app", "libs/tools", "shared")
}

func TestImportAboveRootFails(t *testing.T) {
	result := compileMulti(map[string]string{"app.hex": "module Up = import \"../up\"\n"}, "app.hex")
	wantStderr(t, result, "import resolves above the logical source-map root")
}

func TestImportPathMustBeRelative(t *testing.T) {
	result := compileMulti(map[string]string{"app.hex": "module M = import \"math\"\n"}, "app.hex")
	wantStderr(t, result, "import path \"math\" is not relative")
}

func TestHexSuffixSpellingResolvesSameModule(t *testing.T) {
	// "./math" and "./math.hex" canonicalize identically; the second import
	// of the same canonical module in one file is a duplicate, not a
	// missing module.
	result := compileMulti(map[string]string{
		"app.hex":  "module A = import \"./math\"\nmodule B = import \"./math.hex\"\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}, "app.hex")
	wantStderr(t, result, "duplicate import of canonical module math")
}

func TestImportNotFound(t *testing.T) {
	result := compileMulti(map[string]string{"app.hex": "module Nope = import \"./nope\"\n"}, "app.hex")
	wantStderr(t, result, "imported module \"./nope\" was not found")

	// A non-.hex extension is a different module name, not a suffix rule.
	result = compileMulti(map[string]string{"app.hex": "module Txt = import \"./math.txt\"\n"}, "app.hex")
	wantStderr(t, result, "imported module \"./math.txt\" was not found")
}

func TestImportCycleReportsCanonicalChain(t *testing.T) {
	sources := map[string]string{
		"app.hex":       "module Math = import \"./math\"\n",
		"math.hex":      "module Constants = import \"./constants\"\n",
		"constants.hex": "module App = import \"./app\"\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "import cycle: app -> math -> constants -> app")
}

func TestCaseDistinctModulesAreDistinct(t *testing.T) {
	// math.hex and Math.hex are different modules: only math.hex is
	// imported, so the unreachable Math.hex's bad contents are ignored and
	// only app + math artifacts are generated.
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
		"Math.hex": "broken executable\n",
	}
	result := compileMulti(sources, "app.hex")
	wantMultiSuccess(t, result, "app", "math")
	if len(result.Stderr) != 0 {
		t.Fatalf("stderr = %#v, want no diagnostics", result.Stderr)
	}
}

func TestImportAliasConflictsWithExistingBinding(t *testing.T) {
	sources := map[string]string{
		"app.hex":   "module Math = import \"./math\"\nmodule Math = import \"./math2\"\n",
		"math.hex":  "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
		"math2.hex": "fun sub(x: Int32, y: Int32): Int32 do\n    return x - y\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "import alias Math conflicts with an existing name")
}

func TestParameterCannotShadowImportAlias(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nfun f(Math: Int32) do\nend\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "import alias Math conflicts with an existing name")
}

func TestImportsMustPrecedeOtherItems(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "value: Int32 = 1\nmodule Math = import \"./math\"\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "imports must precede all other items")
}

func TestImportedModuleIsDeclarationsOnly(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "value: Int32 = 1\n",
	}
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "imported module math contains executable statements")
}

func TestUnreachableSourcesAreIgnored(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "value: Int32 = 1\n",
		"junk.hex": "broken executable\nx: Bogus = 1\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("unreachable junk.hex leaked diagnostics: %#v", result)
	}
	wantKeys := map[string]bool{"hexal.h": true, "modules/app.c": true, "modules/app.h": true}
	if len(result.Files) != len(wantKeys) {
		t.Fatalf("Files = %#v, want exactly %d keys", result.Files, len(wantKeys))
	}
	for key := range result.Files {
		if !wantKeys[key] {
			t.Fatalf("Files contains unexpected key %q", key)
		}
	}
	// app.hex is one logical line; sourceLineCount counts the trailing
	// newline, so the reachable total is 2.
	if result.Stats.SourceLines != 2 {
		t.Fatalf("SourceLines = %d, want 2 (app.hex only)", result.Stats.SourceLines)
	}
}

func TestStatsSumOverReachableModules(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	wantMultiSuccess(t, result, "app", "math")
	// Two logical lines (1 app + 3 math), plus one trailing newline per
	// source file: 2 + 4 = 6.
	if result.Stats.SourceLines != 6 {
		t.Fatalf("SourceLines = %d, want 6 (app + math, trailing newlines counted)", result.Stats.SourceLines)
	}
	if result.Stats.TokenCount == 0 {
		t.Fatalf("TokenCount = 0, want the summed reachable token count")
	}
}

func TestEntrypointAbsentFromSources(t *testing.T) {
	result := compileMulti(map[string]string{"other.hex": "value: Int32 = 1\n"}, "app.hex")
	wantStderr(t, result, "entrypoint app.hex was not found in the supplied sources")
}
