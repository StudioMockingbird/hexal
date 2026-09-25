package integration

// Import resolution and the dependency graph drive this facet: user-program
// diagnostics (resolution and checker errors) always surface before
// generation, and a clean multi-module program compiles to one C/header pair
// per reachable module.

import (
	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
	"maps"
	"slices"
	"strings"
	"testing"
)

// assertStderrContains requires an already-computed failing result to carry
// every want somewhere in its diagnostics.
func assertStderrContains(t *testing.T, result compiler.CompilationResult, want ...string) {
	t.Helper()
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("expected failure; got %#v", result)
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
		"app.hex":  "import\n    Math from \"./math\"\nend\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "math")
	if len(result.Stderr) != 0 {
		t.Fatalf("stderr = %#v, want no diagnostics", result.Stderr)
	}
}

func TestRelativeImportsResolve(t *testing.T) {
	sources := map[string]string{
		"app.hex":             "import\n    Tools from \"./libs/tools\"\nend\n",
		"libs/tools.hex":      "import\n    Shared from \"../shared\"\nend\n",
		"shared.hex":          "fun shared_helper(): Int32 do\n    return 1\nend\n",
		"graphics/shapes.hex": "fun area(): Int32 do\n    return 1\nend\n",
	}
	// The nested and the ./graphics/shapes spelling both canonicalize; the
	// unreachable graphics/shapes module contributes no artifacts.
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "libs/tools", "shared")
}

func TestImportAboveRootFails(t *testing.T) {
	result := compiler.Compile(map[string]string{"app.hex": "import\n    Up from \"../up\"\nend\n"}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "import resolves above the logical source-map root")
}

func TestImportPathMustBeRelative(t *testing.T) {
	result := compiler.Compile(map[string]string{"app.hex": "import\n    M from \"math\"\nend\n"}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "quoted import paths must begin with ./ or ../")
}

func TestHexSuffixSpellingResolvesSameModule(t *testing.T) {
	// "./math" and "./math.hex" canonicalize identically; the second import
	// of the same canonical module in one file is a duplicate, not a
	// missing module.
	result := compiler.Compile(map[string]string{
		"app.hex":  "import\n    A from \"./math\"\n,\n    B from \"./math.hex\"\nend\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "duplicate import of canonical module math")
}

func TestImportNotFound(t *testing.T) {
	result := compiler.Compile(map[string]string{"app.hex": "import\n    Nope from \"./nope\"\nend\n"}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "imported module ./nope was not found")

	// A non-.hex extension is a different module name, not a suffix rule.
	result = compiler.Compile(map[string]string{"app.hex": "import\n    Txt from \"./math.txt\"\nend\n"}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "imported module ./math.txt was not found")
}

func TestImportCycleReportsCanonicalChain(t *testing.T) {
	sources := map[string]string{
		"app.hex":       "import\n    Math from \"./math\"\nend\n",
		"math.hex":      "import\n    Constants from \"./constants\"\nend\n",
		"constants.hex": "import\n    App from \"./app\"\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "import cycle: app -> math -> constants -> app")
}

func TestCaseDistinctModulesAreDistinct(t *testing.T) {
	// math.hex and Math.hex are different modules: only math.hex is
	// imported, so the unreachable Math.hex's bad contents are ignored and
	// only app + math artifacts are generated.
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
		"Math.hex": "broken executable\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "math")
	if len(result.Stderr) != 0 {
		t.Fatalf("stderr = %#v, want no diagnostics", result.Stderr)
	}
}

func TestImportAliasConflictsWithExistingBinding(t *testing.T) {
	sources := map[string]string{
		"app.hex":   "import\n    Math from \"./math\"\n,\n    Math from \"./math2\"\nend\n",
		"math.hex":  "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
		"math2.hex": "fun sub(x: Int32, y: Int32): Int32 do\n    return x - y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "import alias Math conflicts with an existing name")
}

func TestParameterCannotShadowImportAlias(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\nfun f(Math: Int32) do\nend\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "import alias Math conflicts with an existing name")
}

func TestImportsMustPrecedeOtherItems(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "let value: Int32 = 1\nimport\n    Math from \"./math\"\nend\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "import block must be the first top-level construct")
}

// The import prefix ends at the first non-import item of any kind: a type,
// function, impl, or executable statement that precedes an import is a
// grammar violation with a positioned diagnostic, not a silent acceptance.
func TestImportAfterAnyDeclarationRejected(t *testing.T) {
	cases := []struct {
		name   string
		prefix string
	}{
		{"type declaration", "type T is struct n: Int32 end\n"},
		{"function declaration", "fun helper(): Int32 do\n    return 1\nend\n"},
		{"impl declaration", "type P is struct x: Int32 end\nmethod P.get(): Int32 do\n    return self.x\nend\n"},
		{"executable statement", "let count: Int32 = 1\n"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			sources := map[string]string{
				"app.hex":  testCase.prefix + "import\n    Math from \"./math\"\nend\n",
				"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\nexport\n    add\nend\n",
			}
			result := compiler.Compile(sources, "app.hex", compiler.Project{})
			assertStderrContains(t, result, "import block must be the first top-level construct")
		})
	}
	// Imports-only-first programs remain accepted.
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\ntype T is struct n: Int32 end\nfun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\nexport\n    add\nend\n",
		"math.hex": "fun helper(): Int32 do\n    return 1\nend\nexport\n    helper\nend\n",
	}
	compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, compiler.Compile(sources, "app.hex", compiler.Project{}), "app", "math")
}

func TestImportedModuleIsDeclarationsOnly(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\n",
		"math.hex": "value = 1\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "imported module math contains executable statements")
}

func TestUnreachableSourcesAreIgnored(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "let value: Int32 = 1\n",
		"junk.hex": "broken executable\nlet x: Bogus = 1\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
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
		"app.hex":  "import\n    Math from \"./math\"\nend\n",
		"math.hex": "fun add(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	assertMultiModuleSuccess(t, result, "app", "math")
	// Six logical lines (3 app + 3 math), plus one trailing newline per
	// source file: 4 + 4 = 8.
	if result.Stats.SourceLines != 8 {
		t.Fatalf("SourceLines = %d, want 8 (app + math, trailing newlines counted)", result.Stats.SourceLines)
	}
	if result.Stats.TokenCount == 0 {
		t.Fatalf("TokenCount = 0, want the summed reachable token count")
	}
}

// Every CompilationStats field is asserted, so a permanently-unwritten or
// double-folded field cannot survive unnoticed (ParseDuration did).
func TestStatsFields(t *testing.T) {
	result := assertCompiles(t, "let value: Int32 = 1\n")
	if result.Stats.TokenCount == 0 || result.Stats.SourceLines == 0 {
		t.Fatalf("TokenCount=%d SourceLines=%d, want both nonzero", result.Stats.TokenCount, result.Stats.SourceLines)
	}
	subtotal := result.Stats.LexDuration + result.Stats.CheckDuration + result.Stats.GenerateDuration
	if result.Stats.PhaseSubtotal != subtotal {
		t.Fatalf("PhaseSubtotal = %v, want Lex+Check+Generate = %v", result.Stats.PhaseSubtotal, subtotal)
	}
	if result.Stats.TotalDuration < result.Stats.PhaseSubtotal {
		t.Fatalf("TotalDuration = %v, want >= PhaseSubtotal = %v", result.Stats.TotalDuration, result.Stats.PhaseSubtotal)
	}
}

func TestEntrypointAbsentFromSources(t *testing.T) {
	result := compiler.Compile(map[string]string{"other.hex": "let value: Int32 = 1\n"}, "app.hex", compiler.Project{})
	assertStderrContains(t, result, "entrypoint app.hex was not found in the supplied sources")
}

// Resolution reads one canonical-identity index per compilation, so identical
// content resolves identically regardless of how the caller assembled the
// map, and each named index case keeps its exact diagnostics: shuffled
// insertion, missing imports, malformed colliding keys, reserved stdlib keys,
// and prepared C bindings.
func TestSourceKeyIndexResolution(t *testing.T) {
	type entry struct{ key, text string }
	build := func(entries []entry, order []int) map[string]string {
		sources := make(map[string]string, len(order))
		for _, index := range order {
			sources[entries[index].key] = entries[index].text
		}
		return sources
	}
	compareRuns := func(t *testing.T, first, second compiler.CompilationResult) {
		t.Helper()
		if first.ExitCode != second.ExitCode {
			t.Fatalf("exit = %d vs %d across insertion orders", first.ExitCode, second.ExitCode)
		}
		if !slices.Equal(first.Stderr, second.Stderr) {
			t.Fatalf("diagnostics differ across insertion orders:\n%v\nvs\n%v", first.Stderr, second.Stderr)
		}
		firstKeys := slices.Sorted(maps.Keys(first.Files))
		secondKeys := slices.Sorted(maps.Keys(second.Files))
		if !slices.Equal(firstKeys, secondKeys) {
			t.Fatalf("artifact keys differ: %v vs %v", firstKeys, secondKeys)
		}
		for _, key := range firstKeys {
			if first.Files[key] != second.Files[key] {
				t.Fatalf("artifact %q differs across insertion orders", key)
			}
		}
	}

	t.Run("shuffled insertion", func(t *testing.T) {
		entries := []entry{
			{"app.hex", "import\n    A from \"./a\"\n,\n    B from \"./b\"\nend\nlet value: Int32 = A.twice()\n"},
			{"a.hex", "fun twice(): Int32 do\n    return 2\nend\nexport\n    twice\nend\n"},
			{"b.hex", "fun broken(): Int32 do\n    return \"not an int\"\nend\nexport\n    broken\nend\n"},
		}
		compareRuns(t,
			compiler.Compile(build(entries, []int{0, 1, 2}), "app.hex", compiler.Project{}),
			compiler.Compile(build(entries, []int{2, 0, 1}), "app.hex", compiler.Project{}))

		discovery := []entry{
			{"app.hex", "import\n    Adder from c \"adder.h\"\n,\n    Std from c <stdio.h>\nend\nlet value: Int32 = 1\n"},
		}
		firstRequests, firstErr := compiler.DiscoverCImports(build(discovery, []int{0}), "app.hex")
		secondRequests, secondErr := compiler.DiscoverCImports(build(discovery, []int{0}), "app.hex")
		if firstErr != nil || secondErr != nil {
			t.Fatalf("DiscoverCImports errors: %v, %v", firstErr, secondErr)
		}
		if !slices.Equal(firstRequests, secondRequests) {
			t.Fatalf("requests differ: %v vs %v", firstRequests, secondRequests)
		}
	})

	t.Run("missing import", func(t *testing.T) {
		result := compiler.Compile(map[string]string{"app.hex": "import\n    Nope from \"./nope\"\nend\n"}, "app.hex", compiler.Project{})
		assertStderrContains(t, result, "imported module ./nope was not found")
	})

	// The malformed key canonicalizes identically to the valid one and sorts
	// first, so the index picks it deterministically: the invalid-key
	// diagnostic names "lib", never its neighbor, on every compilation.
	t.Run("malformed colliding keys", func(t *testing.T) {
		sources := map[string]string{
			"app.hex": "import\n    L from \"./lib\"\nend\nlet value: Int32 = 1\n",
			"lib.hex": "fun ok(): Int32 do\n    return 1\nend\nexport\n    ok\nend\n",
			"lib":     "fun ok(): Int32 do\n    return 1\nend\nexport\n    ok\nend\n",
		}
		for i := 0; i < 20; i++ {
			result := compiler.Compile(sources, "app.hex", compiler.Project{})
			assertStderrContains(t, result, `logical key "lib" is invalid`)
		}
	})

	t.Run("reserved stdlib key", func(t *testing.T) {
		result := compiler.Compile(map[string]string{
			"app.hex":       "import\n    S from \"./std/thing\"\nend\nlet value: Int32 = 1\n",
			"std/thing.hex": "let value: Int32 = 1\nexport\n    value\nend\n",
		}, "app.hex", compiler.Project{})
		assertStderrContains(t, result, `the "std" path prefix is reserved for the standard library`)
	})

	t.Run("prepared C binding", func(t *testing.T) {
		key := compiler.CBindingKey(string(compilerTypes.TargetX86_64WindowsGNU), compiler.CImportRequest{Header: "adder.h"})
		sources := map[string]string{
			"app.hex": "import\n    Adder from c \"adder.h\"\nend\nlet value: Int32 = 1\n",
			key:       "extern c from \"adder.h\" do\nend\n",
		}
		result := compiler.Compile(sources, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
		if result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("prepared binding must resolve through the index: %v", result.Stderr)
		}
	})
}
