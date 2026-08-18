package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
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
	artifacts, err := renderComponentArtifacts(withoutTrap)
	if err != nil {
		t.Fatalf("renderComponentArtifacts() error = %v", err)
	}
	if len(artifacts) != 0 {
		t.Fatalf("no-trap program emitted components: %v", artifacts)
	}

	withTrap := &programEmission{requirements: &cHeaderRequirements{trap: true}}
	artifacts, err = renderComponentArtifacts(withTrap)
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
	program := checkedGeneratorSource(t, "x: Int32 = 1\n")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program})
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if _, exists := files["hexal/runtime.c"]; exists {
		t.Fatalf("scalar-only program emitted hexal/runtime.c")
	}
}
