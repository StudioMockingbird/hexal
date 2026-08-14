package tests

// RFC 0034 Task 7: per-module C/header emission, encoded symbols, and
// deterministic multi-module generation, end to end through Compile.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestModuleGenerationEmitsOnePairPerModule(t *testing.T) {
	sources := map[string]string{
		"app.hex":             "module Math = import \"./math\"\nmodule Shapes = import \"./graphics/shapes\"\n",
		"math.hex":            "export fun add(a: Int32, b: Int32): Int32\n    return a + b\nend\n",
		"graphics/shapes.hex": "export fun area(): Int32\n    return 1\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("multi-module generation failed: %#v", result.Stderr)
	}
	wantKeys := map[string]bool{
		"main.c": true, "main.h": true,
		"modules/app.c": true, "modules/app.h": true,
		"modules/math.c": true, "modules/math.h": true,
		"modules/graphics/shapes.c": true, "modules/graphics/shapes.h": true,
	}
	if len(result.Files) != len(wantKeys) {
		t.Fatalf("Files = %#v, want %d keys", result.Files, len(wantKeys))
	}
	for key := range result.Files {
		if !wantKeys[key] {
			t.Fatalf("Files contains unexpected key %q", key)
		}
	}
	if result.MainC != result.Files["main.c"] || result.MainH != result.Files["main.h"] {
		t.Fatalf("MainC/MainH do not mirror the entrypoint entries")
	}
}

func TestModuleGenerationSymbolsAndGuards(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n",
		"math.hex": "export fun add(a: Int32, b: Int32): Int32\n    return a + b\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	// "math" has four letters, "app" three: the length-delimited encoding is
	// visible in every module-owned symbol.
	mathC := result.Files["modules/math.c"]
	if !strings.Contains(mathC, "int32_t hex_f_4_math_add(const int32_t") {
		t.Fatalf("math.c lacks the external encoded definition:\n%s", mathC)
	}
	if !strings.Contains(result.Files["modules/math.h"], "HEX_MODULE_4_math_H") {
		t.Fatalf("math.h lacks the encoded guard")
	}
	if !strings.Contains(result.Files["modules/app.h"], "HEX_MODULE_3_app_H") {
		t.Fatalf("app.h lacks the encoded guard")
	}
	if !strings.Contains(result.Files["modules/app.c"], "hex_f_4_math_add") {
		t.Fatalf("app.c does not call the encoded math symbol")
	}
	appH := result.Files["modules/app.h"]
	if !strings.Contains(appH, "int32_t hex_f_4_math_add(int32_t") {
		t.Fatalf("app.h lacks the cross-module prototype:\n%s", appH)
	}
	// Private functions stay static and headerless.
	if strings.Contains(result.Files["modules/math.h"], "hex_f_4_math_secret") {
		t.Fatalf("private function leaked into the header")
	}
}

func TestModuleGenerationPrivateStaysStatic(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.secret(1)\n",
		"math.hex": "fun secret(a: Int32): Int32\n    return a\nend\n",
	}
	// The private call fails at checking before generation.
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "declaration secret is private to module math")
}

func TestModuleGenerationDiamondEmittedOnce(t *testing.T) {
	sources := map[string]string{
		"app.hex":       "module Math = import \"./math\"\nmodule Shapes = import \"./shapes\"\n",
		"math.hex":      "module Constants = import \"./constants\"\nexport fun half(): Int32\n    return Constants.value() / 2\nend\n",
		"shapes.hex":    "module Constants = import \"./constants\"\nexport fun area(): Int32\n    return Constants.value()\nend\n",
		"constants.hex": "export fun value(): Int32\n    return 10\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("diamond generation failed: %#v", result.Stderr)
	}
	// The shared dependency's pair appears exactly once.
	keys := result.Files
	if _, ok := keys["modules/constants.c"]; !ok {
		t.Fatalf("constants pair missing: %#v", keys)
	}
	if count := strings.Count(keys["modules/constants.c"], "hex_f_9_constants_value"); count != 1 {
		t.Fatalf("constants.c defines its function %d times, want 1", count)
	}
	// No init functions for non-root modules: main.c calls only the root run.
	mainC := keys["main.c"]
	if strings.Contains(mainC, "hex_module_init") || strings.Count(mainC, "hex_module_root_run") != 1 {
		t.Fatalf("main.c root-run wiring wrong:\n%s", mainC)
	}
}

func TestModuleGenerationLineDirectivesPerModule(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "export fun add(a: Int32, b: Int32): Int32\n    return a + b\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	mathC := result.Files["modules/math.c"]
	if !strings.Contains(mathC, "#line 1 \"math.hex\"") || strings.Contains(mathC, "#line") && strings.Contains(strings.Split(mathC, "#line 1 \"math.hex\"")[1], "\"app.hex\"") {
		t.Fatalf("math.c #line directives do not reference math.hex:\n%s", mathC)
	}
}

func TestModuleGenerationDeterministic(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n",
		"math.hex": "export fun add(a: Int32, b: Int32): Int32\n    return a + b\nend\n",
	}
	first := compileMulti(sources, "app.hex")
	second := compileMulti(sources, "app.hex")
	if first.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", first.Stderr)
	}
	for key, content := range first.Files {
		if second.Files[key] != content {
			t.Fatalf("Files[%q] differs between identical compilations", key)
		}
	}
}

func TestModuleGenerationUnreachableModulesProduceNoArtifacts(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "export fun add(a: Int32, b: Int32): Int32\n    return a + b\nend\n",
		"junk.hex": "broken executable\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	if _, ok := result.Files["modules/junk.c"]; ok {
		t.Fatalf("unreachable module produced artifacts: %#v", result.Files)
	}
	// app.hex is one logical line, math.hex three; sourceLineCount counts
	// the trailing newline of each, so the reachable total is 2 + 4 = 6.
	// junk.hex (3) is excluded.
	if result.Stats.SourceLines != 6 {
		t.Fatalf("SourceLines = %d, want 6 (app + math only)", result.Stats.SourceLines)
	}
}
