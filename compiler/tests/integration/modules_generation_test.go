package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestModuleGenerationEmitsOnePairPerModule(t *testing.T) {
	sources := map[string]string{
		"app.hex":             "module Math = import \"./math\"\nmodule Shapes = import \"./graphics/shapes\"\n",
		"math.hex":            "export fun add(a: Int32, b: Int32): Int32 do\n    return a + b\nend\n",
		"graphics/shapes.hex": "export fun area(): Int32 do\n    return 1\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("multi-module generation failed: %#v", result.Stderr)
	}
	wantKeys := map[string]bool{
		"hexal.h":       true,
		"modules/app.c": true, "modules/app.h": true,
		"modules/math.c": true, "modules/math.h": true,
		"modules/graphics/shapes.c": true, "modules/graphics/shapes.h": true,
	}
	for key := range result.Files {
		if !wantKeys[key] {
			// The demand-driven component artifacts under hexal/
			// are legitimate additions selected by the reachable families;
			// every other key must be one of the mandatory artifacts.
			if strings.HasPrefix(key, "hexal/") {
				continue
			}
			t.Fatalf("Files contains unexpected key %q", key)
		}
	}
	for key := range wantKeys {
		if _, exists := result.Files[key]; !exists {
			t.Fatalf("Files lacks mandatory key %q", key)
		}
	}
}

func TestModuleGenerationSymbolsAndGuards(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n",
		"math.hex": "export fun add(a: Int32, b: Int32): Int32 do\n    return a + b\nend\n",
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
		"math.hex": "fun secret(a: Int32): Int32 do\n    return a\nend\n",
	}
	// The private call fails at checking before generation.
	result := compileMulti(sources, "app.hex")
	wantStderr(t, result, "declaration secret is private to module math")
}

func TestModuleGenerationDiamondEmittedOnce(t *testing.T) {
	sources := map[string]string{
		"app.hex":       "module Math = import \"./math\"\nmodule Shapes = import \"./shapes\"\n",
		"math.hex":      "module Constants = import \"./constants\"\nexport fun half(): Int32 do\n    return Constants.value() / 2\nend\n",
		"shapes.hex":    "module Constants = import \"./constants\"\nexport fun area(): Int32 do\n    return Constants.value()\nend\n",
		"constants.hex": "export fun value(): Int32 do\n    return 10\nend\n",
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
	// No init functions for non-root modules: the root module C calls only
	// its own statements from main().
	appC := keys["modules/app.c"]
	if strings.Contains(appC, "hex_module_init") || strings.Count(appC, "int main(void)") != 1 {
		t.Fatalf("root module C entry-point wiring wrong:\n%s", appC)
	}
}

func TestModuleGenerationLineDirectivesPerModule(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\n",
		"math.hex": "export fun add(a: Int32, b: Int32): Int32 do\n    return a + b\nend\n",
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
		"math.hex": "export fun add(a: Int32, b: Int32): Int32 do\n    return a + b\nend\n",
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
		"math.hex": "export fun add(a: Int32, b: Int32): Int32 do\n    return a + b\nend\n",
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

// The process entry point and process-wide runtime state exist exactly once,
// in the entrypoint module's C file: main() lives in the root C file, and no
// non-root pair mentions it.
func TestModuleGenerationEntryOnlyInRootPair(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.add(2, 3)\n",
		"math.hex": "export fun add(a: Int32, b: Int32): Int32 do\n    return a + b\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	appC := result.Files["modules/app.c"]
	if !strings.Contains(appC, "int main(void) {") {
		t.Fatalf("app.c lacks the process entry point:\n%s", appC)
	}
	mathC := result.Files["modules/math.c"]
	mathH := result.Files["modules/math.h"]
	if strings.Contains(mathC, "int main(void)") {
		t.Fatalf("math.c contains the process entry point:\n%s", mathC)
	}
	if strings.Contains(mathH, "int main(void)") {
		t.Fatalf("math.h declares the process entry point:\n%s", mathH)
	}
	// Every module header includes hexal.h and no module header includes
	// another module header.
	for _, header := range []string{result.Files["modules/app.h"], mathH} {
		if !strings.Contains(header, "#include \"hexal.h\"") {
			t.Fatalf("module header lacks the shared program-support include:\n%s", header)
		}
	}
	for _, header := range []string{result.Files["modules/app.h"], mathH} {
		if strings.Contains(header, "#include \"modules/") {
			t.Fatalf("module header includes another module header:\n%s", header)
		}
	}
	if !strings.Contains(result.Files["modules/math.c"], "#include \"modules/math.h\"") {
		t.Fatalf("module C file must include only its own header:\n%s", mathC)
	}
	if strings.Contains(mathC, "#include \"modules/app.h\"") {
		t.Fatalf("module C file includes another module's header:\n%s", mathC)
	}
}

// Built-in machinery used only in a non-root module is aggregated into the
// program-wide support set: hexal.h and the demand-driven component
// artifacts under hexal/ carry the shared definitions and the one canonical
// literal table, the root module C stays thin, and the module's pair holds
// only its own user content plus inline helpers.
func TestModuleGenerationBuiltinMachineryProgramWide(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 = Math.compute()\n",
		"math.hex": "export fun compute(): Int32 do\n    items: List<Int32> = List<Int32>.new(Heap.new())\n    items.push(7)\n    print(\"hello\")\n    return items[0]\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	hexalH := result.Files["hexal.h"]
	mathC := result.Files["modules/math.c"]
	// The list machinery lives in hexal/list.h, driven by the program-wide
	// aggregate even though only math uses it; the literal table lives in
	// the string component.
	if !strings.Contains(result.Files["hexal/list.h"], "typedef struct hex_list_Int32") {
		t.Fatalf("hexal/list.h lacks the list definition from math:\n%s", result.Files["hexal/list.h"])
	}
	if !strings.Contains(result.Files["hexal/string.c"], "const uint8_t hex_lit_0_bytes[6] = { 104, 101, 108, 108, 111, 0 };") {
		t.Fatalf("hexal/string.c lacks the canonical \"hello\" literal (index 0):\n%s", result.Files["hexal/string.c"])
	}
	// The module's own references use the program-wide indices.
	if !strings.Contains(mathC, "&hex_lit_0") {
		t.Fatalf("math.c does not reference the program-wide literal:\n%s", mathC)
	}
	// The stdio gate covers modules too: the shared runtime trap that the
	// print helpers report through is declared in hexal.h.
	if !strings.Contains(hexalH, "#include <stdio.h>") {
		t.Fatalf("hexal.h lacks the stdio include for math's print helpers:\n%s", hexalH)
	}
	if strings.Contains(mathC, "int main(void)") {
		t.Fatalf("math.c contains the process entry point:\n%s", mathC)
	}
}

// Concurrency machinery used only in a non-root module is emitted once per
// process: the scheduler runtime and the spawn entry prototypes live in the
// root module's C file and hexal.h, while the entry adapter and its argument
// frame live beside the spawned function's own definition.
func TestModuleGenerationConcurrencyOwnedByDefiningModule(t *testing.T) {
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nx: Int32 | Error = Math.compute()\n",
		"math.hex": "fun double(v: Int32): Int32 do\n    return v * 2\nend\nexport fun compute(): Int32 | Error do\n    task: Task<Int32> = try spawn double(21)\n    return task.join()\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("generation failed: %#v", result.Stderr)
	}
	appC := result.Files["modules/app.c"]
	mathC := result.Files["modules/math.c"]
	mathH := result.Files["modules/math.h"]
	concurrencyH := result.Files["hexal/concurrency.h"]
	// The scheduler runtime and its externs are program-wide: the runtime
	// lives in the concurrency component, the root module C initializes and
	// completes the scheduler.
	if !strings.Contains(concurrencyH, "void hex_task_complete(hex_task *task);") {
		t.Fatalf("hexal/concurrency.h lacks the task-complete extern the adapters call:\n%s", concurrencyH)
	}
	if !strings.Contains(appC, "hex_scheduler_init();") {
		t.Fatalf("app.c does not initialize the scheduler:\n%s", appC)
	}
	// The entry adapter lives in the spawned function's own module pair with
	// external linkage and its argument frame beside it; the root module C
	// carries no adapters.
	if !strings.Contains(concurrencyH, "void hex_task_entry_hex_f_m4_math_double(hex_task *task);") {
		t.Fatalf("hexal/concurrency.h lacks the spawn entry prototype:\n%s", concurrencyH)
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
	if strings.Contains(appC, "hex_task_entry_") {
		t.Fatalf("app.c carries spawn adapters:\n%s", appC)
	}
}

func TestModuleGenerationLiteralOrder(t *testing.T) {
	// The root source places "root" before the imported source's "imported";
	// dependency-first discovery must still emit math's literals first.
	sources := map[string]string{
		"app.hex":  "module Math = import \"./math\"\nroot: String = \"root\"\nshared: String = \"shared\"\nresult: String = Math.text()\n",
		"math.hex": "export fun text(): String do\n    imported: String = \"imported\"\n    shared: String = \"shared\"\n    return shared\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("literal-order generation failed: %#v", result.Stderr)
	}
	output := result.Files["hexal/string.c"]
	definitions := []string{
		"const uint8_t hex_lit_0_bytes[9] = { 105, 109, 112, 111, 114, 116, 101, 100, 0 };\nconst hex_string hex_lit_0 = { hex_lit_0_bytes, 8 };",
		"const uint8_t hex_lit_1_bytes[7] = { 115, 104, 97, 114, 101, 100, 0 };\nconst hex_string hex_lit_1 = { hex_lit_1_bytes, 6 };",
		"const uint8_t hex_lit_2_bytes[5] = { 114, 111, 111, 116, 0 };\nconst hex_string hex_lit_2 = { hex_lit_2_bytes, 4 };",
	}
	previous := -1
	for _, definition := range definitions {
		if count := strings.Count(output, definition); count != 1 {
			t.Fatalf("hexal/string.c defines %q %d times, want once:\n%s", definition, count, output)
		}
		position := strings.Index(output, definition)
		if position <= previous {
			t.Fatalf("hexal/string.c literal order is not dependency-first:\n%s", output)
		}
		previous = position
	}
	mathC := result.Files["modules/math.c"]
	appC := result.Files["modules/app.c"]
	for _, want := range []string{
		"const hex_string *const hex_v_imported = &hex_lit_0;",
		"const hex_string *const hex_v_shared = &hex_lit_1;",
	} {
		if !strings.Contains(mathC, want) {
			t.Fatalf("math.c lacks literal use %q:\n%s", want, mathC)
		}
	}
	for _, want := range []string{
		"const hex_string *const hex_v_root = &hex_lit_2;",
		"const hex_string *const hex_v_shared = &hex_lit_1;",
	} {
		if !strings.Contains(appC, want) {
			t.Fatalf("app.c lacks literal use %q:\n%s", want, appC)
		}
	}
}

func TestModuleGenerationConcurrencyLiteralHandles(t *testing.T) {
	// root.hex is visited before math.hex and contributes the root payload;
	// app.hex repeats it. That earlier global entry shifts every generated
	// concurrency literal in the non-root math module.
	sources := map[string]string{
		"app.hex":  "module Root = import \"./root\"\nmodule Math = import \"./math\"\nroot: String = \"root\"\nx: Int32 | Error = Math.compute()\n",
		"root.hex": "export fun text(): String do\n    root: String = \"root\"\n    return root\nend\n",
		"math.hex": "fun double(v: Int32): Int32 do\n    return v * 2\nend\nexport fun compute(): Int32 | Error do\n    task: Task<Int32> = try spawn double(21)\n    return task.join()\nend\n",
	}
	result := compileMulti(sources, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("concurrency literal generation failed: %#v", result.Stderr)
	}
	stringOutput := result.Files["hexal/string.c"]
	definitions := []string{
		"const uint8_t hex_lit_0_bytes[5] = { 114, 111, 111, 116, 0 };\nconst hex_string hex_lit_0 = { hex_lit_0_bytes, 4 };",
		"const uint8_t hex_lit_1_bytes[9] = { 109, 97, 105, 110, 46, 104, 101, 120, 0 };\nconst hex_string hex_lit_1 = { hex_lit_1_bytes, 8 };",
		"const uint8_t hex_lit_2_bytes[10] = { 83, 99, 104, 101, 100, 117, 108, 101, 114, 0 };\nconst hex_string hex_lit_2 = { hex_lit_2_bytes, 9 };",
		"const uint8_t hex_lit_3_bytes[21] = { 116, 97, 115, 107, 32, 99, 114, 101, 97, 116, 105, 111, 110, 32, 102, 97, 105, 108, 101, 100, 0 };\nconst hex_string hex_lit_3 = { hex_lit_3_bytes, 20 };",
	}
	for _, definition := range definitions {
		if count := strings.Count(stringOutput, definition); count != 1 {
			t.Fatalf("hexal/string.c defines %q %d times, want once:\n%s", definition, count, stringOutput)
		}
	}
	mathC := result.Files["modules/math.c"]
	mathH := result.Files["modules/math.h"]
	if !strings.Contains(mathH, ".hex_m_file = &hex_lit_1") {
		t.Fatalf("math.h does not use the shifted program-wide source filename literal:\n%s", mathH)
	}
	if !strings.Contains(mathH, ".hex_m_header = (hex_strand){{ 83, 99, 104, 101, 100, 117, 108, 101, 114, 0 }}") {
		t.Fatalf("math.h does not use the exact Scheduler header literal:\n%s", mathH)
	}
	if !strings.Contains(mathC, "hex_sched_error(5, 29, &hex_lit_3)") {
		t.Fatalf("math.c does not use the shifted program-wide spawn failure literal:\n%s", mathC)
	}
	for _, file := range []struct {
		name string
		text string
	}{
		{name: "root.c", text: result.Files["modules/root.c"]},
		{name: "app.c", text: result.Files["modules/app.c"]},
	} {
		if !strings.Contains(file.text, "const hex_string *const hex_v_root = &hex_lit_0;") {
			t.Fatalf("modules/%s does not use the shared root literal:\n%s", file.name, file.text)
		}
	}
}
