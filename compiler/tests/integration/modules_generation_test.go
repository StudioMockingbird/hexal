package integration

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
	if !strings.Contains(mathC, "int32_t hex_f_m4_math_add(const int32_t") {
		t.Fatalf("math.c lacks the external encoded definition:\n%s", mathC)
	}
	if !strings.Contains(result.Files["modules/math.h"], "HEX_MODULE_m4_math_H") {
		t.Fatalf("math.h lacks the encoded guard")
	}
	if !strings.Contains(result.Files["modules/app.h"], "HEX_MODULE_m3_app_H") {
		t.Fatalf("app.h lacks the encoded guard")
	}
	if !strings.Contains(result.Files["modules/app.c"], "hex_f_m4_math_add") {
		t.Fatalf("app.c does not call the encoded math symbol")
	}
	appH := result.Files["modules/app.h"]
	if !strings.Contains(appH, "int32_t hex_f_m4_math_add(int32_t") {
		t.Fatalf("app.h lacks the cross-module prototype:\n%s", appH)
	}
	// Private functions stay static and headerless.
	if strings.Contains(result.Files["modules/math.h"], "hex_f_m4_math_secret") {
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
	if count := strings.Count(keys["modules/constants.c"], "hex_f_m9_constants_value"); count != 1 {
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

// The root run exists exactly once, in the entrypoint module's pair: the
// entrypoint C file defines it, the entrypoint header declares it, main.c
// calls it, and no non-root pair mentions it (RFC 0034 §Generated C
// artifacts).
func TestModuleGenerationRootRunOnlyInRootPair(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n",
		"math.hex": "export fun add(a: Int32, b: Int32): Int32\n    return a + b\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	appC := result.Files["modules/app.c"]
	appH := result.Files["modules/app.h"]
	if !strings.Contains(appC, "int hex_module_root_run(void) {") {
		t.Fatalf("app.c lacks the root run definition:\n%s", appC)
	}
	if !strings.Contains(appH, "int hex_module_root_run(void);") {
		t.Fatalf("app.h lacks the root run declaration:\n%s", appH)
	}
	if !strings.Contains(result.Files["main.c"], "return hex_module_root_run();") {
		t.Fatalf("main.c does not call the root run:\n%s", result.Files["main.c"])
	}
	mathC := result.Files["modules/math.c"]
	mathH := result.Files["modules/math.h"]
	if strings.Contains(mathC, "hex_module_root_run") {
		t.Fatalf("math.c contains a root run definition:\n%s", mathC)
	}
	if strings.Contains(mathH, "hex_module_root_run") {
		t.Fatalf("math.h contains a root run declaration:\n%s", mathH)
	}
}

// Built-in machinery used only in a non-root module is aggregated into the
// program-wide pair: main.h carries the shared definitions and the one
// canonical literal table, main.c stays thin, and the module's pair holds
// only its own user content plus inline helpers (RFC 0034 built-in generic
// ownership).
func TestModuleGenerationBuiltinMachineryProgramWide(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.compute()\n",
		"math.hex": "export fun compute(): Int32\n    items: List<Int32> = List<Int32>.new(Heap.new())\n    items.push(7)\n    print(\"hello\")\n    return items[0]\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	mainH := result.Files["main.h"]
	mathC := result.Files["modules/math.c"]
	// The list machinery and the literal table live in main.h, driven by the
	// program-wide aggregate even though only math uses them.
	if !strings.Contains(mainH, "typedef struct hex_list_Int32") {
		t.Fatalf("main.h lacks the list definition from math:\n%s", mainH)
	}
	if !strings.Contains(mainH, "static const uint8_t hex_lit_0_bytes[6] = { 104, 101, 108, 108, 111, 0 };") {
		t.Fatalf("main.h lacks the canonical \"hello\" literal (index 0):\n%s", mainH)
	}
	// The module's own references use the program-wide indices.
	if !strings.Contains(mathC, "&hex_lit_0") {
		t.Fatalf("math.c does not reference the program-wide literal:\n%s", mathC)
	}
	// The stdio gate covers modules too: print helpers in math.h need fputs.
	if !strings.Contains(mainH, "#include <stdio.h>") {
		t.Fatalf("main.h lacks the stdio include for math's print helpers:\n%s", mainH)
	}
	if strings.Contains(mathC, "int main(void)") {
		t.Fatalf("math.c contains the process entry point:\n%s", mathC)
	}
}

// Concurrency machinery used only in a non-root module is emitted once per
// process: the scheduler runtime and the spawn entry prototypes live in the
// main pair, while the entry adapter and its argument frame live beside the
// spawned function's own definition (RFC 0034 per-module generation, RFC
// 0037 spawn linkage).
func TestModuleGenerationConcurrencyOwnedByDefiningModule(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nx: Int32 | Error = Math.compute()\n",
		"math.hex": "fun double(v: Int32): Int32\n    return v * 2\nend\nexport fun compute(): Int32 | Error\n    task: Task<Int32> = try spawn double(21)\n    return task.join()\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	mainC := result.Files["main.c"]
	mainH := result.Files["main.h"]
	mathC := result.Files["modules/math.c"]
	mathH := result.Files["modules/math.h"]
	// The scheduler runtime and its externs are program-wide.
	if !strings.Contains(mainC, "hex_scheduler_init") {
		t.Fatalf("main.c lacks the scheduler runtime for math's spawn:\n%s", mainC)
	}
	if !strings.Contains(mainC, "hex_scheduler_init();") {
		t.Fatalf("main.c does not initialize the scheduler:\n%s", mainC)
	}
	if !strings.Contains(mainH, "void hex_task_complete(hex_task *task);") {
		t.Fatalf("main.h lacks the task-complete extern the adapters call:\n%s", mainH)
	}
	// The entry adapter lives in the spawned function's own module pair with
	// external linkage and its argument frame beside it; main.c carries no
	// adapters.
	if !strings.Contains(mainH, "void hex_task_entry_hex_f_m4_math_double(hex_task *task);") {
		t.Fatalf("main.h lacks the spawn entry prototype:\n%s", mainH)
	}
	if !strings.Contains(mathC, "void hex_task_entry_hex_f_m4_math_double(hex_task *task) {") {
		t.Fatalf("math.c lacks the adapter definition:\n%s", mathC)
	}
	if !strings.Contains(mathC, "hex_f_m4_math_double(") {
		t.Fatalf("math.c adapter does not call its function:\n%s", mathC)
	}
	if !strings.Contains(mathH, "typedef struct hex_task_args_hex_f_m4_math_double") {
		t.Fatalf("math.h lacks the adapter argument frame:\n%s", mathH)
	}
	if strings.Contains(mainC, "hex_task_entry_") {
		t.Fatalf("main.c carries spawn adapters:\n%s", mainC)
	}
	if strings.Contains(mathC, "hex_module_root_run") {
		t.Fatalf("math.c carries the root run:\n%s", mathC)
	}
}

// The I/O gate used only in a non-root module is emitted once in main.c, and
// the module's inline helpers call it through the main.h externs.
func TestModuleGenerationIOGateProgramWide(t *testing.T) {
	sources := map[string]string{
		"app.hex":   "module Files = import \"./files\"\nx: Nil | Error = Files.write_line()\n",
		"files.hex": "export fun write_line(): Nil | Error\n    result: Nil | Error = try Stdio.stdout().write_text(\"hi\")\n    return result\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	mainC := result.Files["main.c"]
	mainH := result.Files["main.h"]
	if !strings.Contains(mainC, "hex_io_gate_lock") {
		t.Fatalf("main.c lacks the IO gate for files.hex:\n%s", mainC)
	}
	if !strings.Contains(mainH, "extern bool hex_io_gate_closed;") {
		t.Fatalf("main.h lacks the IO gate externs:\n%s", mainH)
	}
}
