package integration

// The selected root module's C/header pair (modules/app.c, modules/app.h)
// holds every user-facing declaration, statement, #line mapping, the
// process-wide runtime definitions, and the process entry point. hexal.h
// holds only the shared program-support machinery. This split is the ground
// contract for per-module artifacts; no thin-entry pair exists.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestRootModuleArtifactsSplit(t *testing.T) {
	source := "type Point = { x: Int32, }\nfun area(point: Point): Int32 do\n    return point.x\nend\nvalue: Int32 := 13\n"
	result := assertCompiles(t, source)

	rootC := rootC(t, result)
	rootH := rootH(t, result)
	hexalH := hexalH(t, result)

	if !strings.Contains(rootH, "typedef struct hex_t_m3_app_Point") {
		t.Fatalf("user type must live in the entrypoint module header; got C=%q H=%q", rootC, rootH)
	}
	if !strings.Contains(rootC, "static int32_t hex_f_m3_app_area(") {
		t.Fatalf("user function must live in modules/app.c; got %q", rootC)
	}
	if !strings.Contains(rootC, "int main(void)") {
		t.Fatalf("modules/app.c must define the process entry point; got %q", rootC)
	}
	if !strings.Contains(rootC, "#line 5 \"app.hex\"") {
		t.Fatalf("module statements must carry app.hex line mappings; got %q", rootC)
	}
	if strings.Contains(hexalH, "hex_f_m3_app_area") || strings.Contains(hexalH, "hex_t_m3_app_Point") {
		t.Fatalf("user code leaked into hexal.h: %q", hexalH)
	}
	if strings.Contains(rootH, "int main(void)") {
		t.Fatalf("the module header must not declare the process entry point; got %q", rootH)
	}
	generated := hexalH + "\n" + rootC + "\n" + rootH
	for _, forbidden := range []string{"#include \"main.h\"", "HEXAL_MAIN_H", "hex_module_root_run"} {
		if strings.Contains(generated, forbidden) {
			t.Fatalf("generated content must not contain %q:\n%s", forbidden, generated)
		}
	}
	if !strings.Contains(hexalH, "#ifndef HEXAL_H") {
		t.Fatalf("hexal.h must carry the HEXAL_H guard; got %q", hexalH)
	}
	if len(result.Files) != 3 {
		t.Fatalf("successful compilation must produce exactly 3 artifacts; got %v", sortedKeys(result.Files))
	}
}

func TestFailureReturnsNoArtifacts(t *testing.T) {
	result := compileSource("x: Int32 := true")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want failure; got %#v", result)
	}
	if result.Files == nil {
		t.Fatalf("Files must be non-nil on failure")
	}
	if len(result.Files) != 0 {
		t.Fatalf("failure must produce no artifacts; got %v", sortedKeys(result.Files))
	}
}

// hexal.h is demand-driven. Standard headers appear once, after the guard
// and before any declaration; no generic target probe or hex_eos is emitted
// without a source-dependent reason.
func TestHexalHeaderDemandDrivenMinimal(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		source    string
		includes  []string // required include spellings
		forbidden []string // must not appear anywhere in hexal.h
	}{
		{
			name:      "bool-only",
			source:    "flag: Bool := true",
			includes:  nil,
			forbidden: []string{"#include", "static_assert", "hex_eos"},
		},
		{
			name:      "float-only",
			source:    "ratio: Float64 := 1.5",
			includes:  nil,
			forbidden: []string{"static_assert", "FLT_MANT_DIG", "DBL_MANT_DIG", "#include <float.h>", "#include <math.h>"},
		},
		{
			name:      "special-float-selects-math",
			source:    "x: Float64 := 0.0 / 0.0 y: Float64 := 1.0 / 0.0",
			includes:  []string{"#include <math.h>"},
			forbidden: []string{"#include <float.h>"},
		},
		{
			name:      "size-small",
			source:    "count: Size := 3",
			includes:  []string{"#include <stddef.h>"},
			forbidden: []string{"static_assert", "hex_eos", "#include <stdint.h>"},
		},
		{
			name:      "size-dependent",
			source:    "count: Size := 5000000000",
			includes:  []string{"#include <stddef.h>", "#include <stdint.h>", "static_assert(5000000000 <= SIZE_MAX"},
			forbidden: nil,
		},
		{
			name:      "eos-literal",
			source:    "end_marker: EoS := eos",
			includes:  []string{"#include <stdint.h>", "typedef uint8_t hex_eos;"},
			forbidden: nil,
		},
		{
			name:      "atomic-only",
			source:    "counter: Atomic<Int32> := Atomic<Int32>.new(5) value: Int32 := counter.load()",
			includes:  []string{"#include <stdint.h>", "#include <stdatomic.h>"},
			forbidden: []string{"#include <stdio.h>", "#include <stdlib.h>", "hex_scheduler_init"},
		},
		{
			name:      "heap",
			source:    "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) h.free(p)",
			includes:  []string{"#include <stdckdint.h>", "#include <stddef.h>", "#include <stdint.h>", "#include <stdio.h>", "#include <stdlib.h>"},
			forbidden: nil,
		},
		{
			name:      "list-checked-arithmetic",
			source:    "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\nend",
			includes:  []string{"#include <stdckdint.h>"},
			forbidden: nil,
		},
		{
			name:      "view-size-triggers-stddef",
			source:    "values: Array<Int32, 3> := [1, 2, 3] view: View<Int32> := values.slice(0, 2)",
			includes:  []string{"#include <stddef.h>", "#include <stdint.h>", "#include <stdio.h>", "#include <stdlib.h>"},
			forbidden: nil,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := assertCompiles(t, testCase.source)
			header := hexalH(t, result)
			for _, include := range testCase.includes {
				if strings.Count(header, include) != 1 {
					t.Fatalf("hexal.h = %q, want exactly one %q", header, include)
				}
			}
			for _, forbidden := range testCase.forbidden {
				if strings.Contains(header, forbidden) {
					t.Fatalf("hexal.h = %q, want no %q", header, forbidden)
				}
			}
		})
	}
}

// An Int32-only program emits the guard and <stdint.h> and nothing else,
// and the root C returns the C-defined successful status directly without
// needing <stdlib.h>.
func TestHexalHeaderInt32OnlyMinimal(t *testing.T) {
	result := assertCompiles(t, "x: Int32 := 13")
	header := hexalH(t, result)
	want := "#ifndef HEXAL_H\n#define HEXAL_H\n\n#include <stdint.h>\n\n\n#endif\n"
	if header != want {
		t.Fatalf("hexal.h = %q, want %q", header, want)
	}
	if _, exists := result.Files["hexal/runtime.c"]; exists {
		t.Fatalf("scalar-only program must emit no runtime component: %q", header)
	}
	if strings.Contains(rootC(t, result), "EXIT_") {
		t.Fatalf("root C must not reference EXIT_ macros: %q", rootC(t, result))
	}
}

// Every generated diagnostic trap reports through one program-wide
// hex_runtime_trap — declared once in hexal.h, defined once in
// hexal/runtime.c, [[noreturn]], owning <stdio.h>/<stdlib.h> — and no
// per-family trap or raw fputs/abort pair remains in generated C.
func TestSingleRuntimeTrapContract(t *testing.T) {
	source := "mut h: Heap := Heap.new()\nitems: List<Int32> := List<Int32>.new(h)\nitems.push(7)\nvalues: Array<Int32, 2> := [1, 2]\nview: View<Int32> := values.slice(0, 1)\ntext: String := \"hello\"\nmut count: Int32 := 0\nmut shift: Int32 := 40\nprint(text)\ncount = 10 / count\ncount = 1 << shift\n"
	result := assertCompiles(t, source)
	header := hexalH(t, result)
	declaration := "[[noreturn]] void hex_runtime_trap(const char *message);"
	if strings.Count(header, declaration) != 1 {
		t.Fatalf("hexal.h = %q, want exactly one trap declaration %q", header, declaration)
	}
	runtime := result.Files["hexal/runtime.c"]
	definition := "void hex_runtime_trap(const char *message) {"
	if strings.Count(runtime, definition) != 1 {
		t.Fatalf("hexal/runtime.c = %q, want exactly one trap definition %q", runtime, definition)
	}
	root := rootC(t, result)
	if strings.Contains(root, "hex_runtime_trap(const char *message) {") {
		t.Fatalf("root C must not define the trap: %q", root)
	}
	// The trap definition is the single legitimate fputs owner: the header
	// and every other artifact report through hex_runtime_trap only.
	if strings.Contains(header, "fputs(") {
		t.Fatalf("hexal.h contains a raw fputs call: %q", header)
	}
	if strings.Count(runtime, "fputs(") != 1 {
		t.Fatalf("hexal/runtime.c = %q, want exactly one fputs inside the trap definition", runtime)
	}
	for name, artifact := range result.Files {
		if name == "hexal/runtime.c" {
			continue
		}
		if strings.Contains(artifact, "fputs(") {
			t.Fatalf("generated artifact %q contains a raw fputs call: %q", name, artifact)
		}
	}
	for _, forbidden := range []string{"hex_numeric_trap", "hex_print_failure"} {
		if strings.Contains(header, forbidden) || strings.Contains(root, forbidden) {
			t.Fatalf("generated output contains removed trap %q", forbidden)
		}
	}
}

// Every #include in hexal.h precedes its first declaration, and no helper
// writer inserts a later include.
func TestHexalHeaderIncludesPrecedeDeclarations(t *testing.T) {
	result := assertCompiles(t, "fun count(): Int32 do\n    items: List<Int32> := List<Int32>.new(Heap.new())\n    items.push(7)\n    print(\"hello\")\n    return items[0]\nend\ncount()\n")
	header := hexalH(t, result)
	lines := strings.Split(header, "\n")
	seenDeclaration := false
	for _, line := range lines {
		if line == "" || strings.HasPrefix(line, "#ifndef") || strings.HasPrefix(line, "#define") || strings.HasPrefix(line, "#include") {
			continue
		}
		seenDeclaration = true
		if strings.HasPrefix(line, "#include") {
			t.Fatalf("hexal.h = %q, include %q appears after declarations", header, line)
		}
	}
	if !seenDeclaration {
		t.Fatalf("hexal.h = %q, want at least one declaration", header)
	}
}

// EoS is one shared typedef across modules, emitted exactly when the
// generated program represents completion (Channel receive unions, or a
// written EoS).
func TestHexalHeaderEosSharedAcrossModules(t *testing.T) {
	sources := map[string]string{
		"app.hex":   "module Files = import \"./files\"\nfun run(): Int32 do\n    return 1\nend\n",
		"files.hex": "export fun helper(): Bool do\n    end_marker: EoS := eos\n    return true\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("multi-module EoS generation failed: %#v", result.Stderr)
	}
	if count := strings.Count(result.Files["hexal.h"], "typedef uint8_t hex_eos;"); count != 1 {
		t.Fatalf("hexal.h must define hex_eos exactly once; got %d:\n%s", count, result.Files["hexal.h"])
	}
	// A program with no EoS anywhere spells no hex_eos at all.
	without := assertCompiles(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) h.free(p)")
	if strings.Contains(hexalH(t, without), "hex_eos") {
		t.Fatalf("hexal.h = %q, want no hex_eos spelling", hexalH(t, without))
	}
}

// Unselected helper families contribute no standard headers.
func TestHexalHeaderUnselectedFamiliesContributeNothing(t *testing.T) {
	result := assertCompiles(t, "x: Int32 := 13")
	header := hexalH(t, result)
	for _, forbidden := range []string{
		"<string.h>", "<inttypes.h>", "<math.h>", "<stdatomic.h>", "<stdio.h>", "<stdlib.h>", "<stddef.h>",
	} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("hexal.h = %q, unselected family header %q emitted", header, forbidden)
		}
	}
}
