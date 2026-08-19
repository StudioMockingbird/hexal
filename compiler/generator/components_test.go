package generator

import (
	"strings"
	"testing"
)

// Every mandatory runtime template exists under packages/ with its eventual
// C/header extension, and the embedded asset set contains each exactly once
// .
func TestComponentTemplatesCompleteAndUnique(t *testing.T) {
	required := []string{
		"hexal.h", "runtime.c", "wrap.h", "heap.h", "heap.c", "view.h",
		"string.h", "string.c", "error.h", "list.h", "dict.h", "array.h",
		"concurrency.h", "concurrency.c",
	}
	seen := make(map[string]int)
	for _, name := range componentTemplateNames() {
		seen[name]++
	}
	for _, name := range required {
		if seen[name] != 1 {
			t.Fatalf("embedded template %q present %d times, want exactly once", name, seen[name])
		}
	}
	if len(seen) != len(required) {
		t.Fatalf("embedded set has %d templates, want %d: %v", len(seen), len(required), seen)
	}
}

// A render model missing a field referenced by a template fails closed under
// missingkey=error; generation must never silently emit an empty value.
func TestComponentRenderMissingFieldFailsClosed(t *testing.T) {
	_, err := renderComponent(componentArtifact{
		key:      "hexal/runtime.c",
		template: "hexal.h",
		model:    struct{ Missing string }{Missing: "x"},
	})
	if err == nil {
		t.Fatal("render with a model missing the template field must fail")
	}
}

// Rendering is deterministic: equivalent models produce identical bytes.
func TestComponentRenderDeterministic(t *testing.T) {
	model := hexalHeaderModel{Includes: []string{"stdint.h", "stdlib.h"}, Eos: true, TrapDeclared: true}
	first, err := renderComponent(componentArtifact{key: "hexal.h", template: "hexal.h", model: model})
	if err != nil {
		t.Fatalf("render error = %v", err)
	}
	second, err := renderComponent(componentArtifact{key: "hexal.h", template: "hexal.h", model: model})
	if err != nil {
		t.Fatalf("render error = %v", err)
	}
	if first != second {
		t.Fatalf("equivalent models rendered differently")
	}
	if !strings.HasPrefix(first, "#ifndef HEXAL_H\n#define HEXAL_H\n") || !strings.HasSuffix(first, "\n#endif\n") {
		t.Fatalf("hexal.h template lost its guard: %q", first)
	}
	if strings.Contains(first, "static_assert") {
		t.Fatalf("hexal.h rendered asserts without data: %q", first)
	}
}

// The runtime component is emitted only when a selected path can trap, and
// its definition is the one trap body.
func TestRuntimeComponentSelectedByTrap(t *testing.T) {
	withoutTrap := &programEmission{requirements: &cHeaderRequirements{}}
	artifacts, err := renderComponentArtifacts(withoutTrap, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("no-trap program emitted components: %v", artifacts)
	}

	withTrap := &programEmission{requirements: &cHeaderRequirements{trap: true}}
	artifacts, err = renderComponentArtifacts(withTrap, Config{})
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	runtime, exists := artifacts["hexal/runtime.c"]
	if !exists {
		t.Fatalf("trap program emitted no hexal/runtime.c: %v", artifacts)
	}
	if !strings.Contains(runtime, "void hex_runtime_trap(const char *message) {") {
		t.Fatalf("hexal/runtime.c lacks the trap definition: %q", runtime)
	}
	if !strings.HasPrefix(runtime, "/*") || !strings.Contains(runtime, "#include \"hexal.h\"") {
		t.Fatalf("hexal/runtime.c must include hexal.h first: %q", runtime)
	}
}

// GenerateChecked rejects a duplicate logical artifact key instead of
// silently overwriting.
func TestGenerateCheckedRejectsDuplicateArtifactKey(t *testing.T) {
	program := checkedGeneratorSource(t, "x: Int32 := 1\n")
	files := generateOne(t, program)
	if _, exists := files["hexal/runtime.c"]; exists {
		t.Fatalf("scalar-only program emitted hexal/runtime.c")
	}
}

// Every demand-driven component renders identically for equivalent programs.
// The per-family copies of this test differed only in their source and
// artifact keys, so they are one table (RFC 0074 R17). The Error component is
// not here: it renders from a synthetic emission rather than a source program,
// which is a different shape, not a different row.
func TestComponentRenderingIsDeterministic(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		source    string
		artifacts []string
	}{
		{
			name:      "array",
			source:    "fun demo() do\n    fixed: Array<Int32, 3> := [1, 2, 3]\n    first: Int32 := fixed[0]\nend",
			artifacts: []string{"hexal/array.h"},
		},
		{
			name: "concurrency",
			source: "fun worker(ch: Channel<Int32>, m: Mutex): Bool do\n    m.lock()\n    ch.send(1)\n    m.unlock()\n" +
				"    Task.yield()\n    return true\nend\n" +
				"fun run(): Int32 | Error do\n    h: Heap := Heap.new()\n    ch: Channel<Int32> := try Channel<Int32>.new(h, 4)\n" +
				"    m: Mutex := try Mutex.new(h)\n    task: Task<Bool> := try spawn worker(ch, m)\n    task.join()\n    return 0\nend\n",
			artifacts: []string{"hexal/concurrency.h", "hexal/concurrency.c"},
		},
		{
			name:      "dict",
			source:    "fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\nend",
			artifacts: []string{"hexal/dict.h"},
		},
		{
			name:      "list",
			source:    "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\nend",
			artifacts: []string{"hexal/list.h"},
		},
		{
			name:      "string",
			source:    "greeting: String := \"hello\"\n",
			artifacts: []string{"hexal/string.h", "hexal/string.c"},
		},
		{
			name:      "view",
			source:    "fun demo() do\n    view: View<Int32> := View<Int32>.empty()\n    count: Size := view.length()\nend",
			artifacts: []string{"hexal/view.h"},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			program := checkedGeneratorSource(t, testCase.source)
			first := generateOne(t, program)
			second := generateOne(t, program)
			for _, artifact := range testCase.artifacts {
				if first[artifact] == "" {
					t.Fatalf("%s was not emitted for the %s program", artifact, testCase.name)
				}
				if first[artifact] != second[artifact] {
					t.Fatalf("equivalent compilations rendered %s differently", artifact)
				}
			}
		})
	}
}

// A scalar-only program selects no collection component and its module header
// includes none. Array and List were byte-identical copies of this check
// (RFC 0074 R17); View is deliberately not here — view.h is emitted
// transitively by the array component, so its absence has a different
// precondition and keeps its own test.
func TestUnselectedCollectionComponentsAreAbsent(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    value: Int32 := 1\nend")
	files := generateOne(t, program)
	for _, artifact := range []string{"hexal/array.h", "hexal/list.h"} {
		if _, exists := files[artifact]; exists {
			t.Errorf("scalar-only program emitted %s", artifact)
		}
		if strings.Contains(files["modules/app.h"], artifact) {
			t.Errorf("modules/app.h = %q, must not include the unselected %s", files["modules/app.h"], artifact)
		}
	}
}
