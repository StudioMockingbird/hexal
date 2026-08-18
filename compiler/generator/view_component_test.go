package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
)

// A view-using program emits hexal/view.h with every reachable
// specialization exactly once, in C-name order, with its guard, its
// hexal.h include, and exactly one trailing newline; the owning module
// header includes the component.
func TestViewComponentEmitsReachableSpecializationsOnce(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(data: Ptr<UInt8>) do\n    first: View<Int32> = View<Int32>.empty()\n    second: View<UInt8> = View<UInt8>.from_pointer(data, 0)\nend")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	viewH := files["hexal/view.h"]
	if viewH == "" {
		t.Fatalf("generated files %v lack hexal/view.h", files)
	}
	if !strings.HasPrefix(viewH, "#ifndef HEXAL_VIEW_H\n#define HEXAL_VIEW_H\n\n#include \"hexal.h\"\n") {
		t.Fatalf("hexal/view.h lost its guard or its hexal.h include: %q", viewH)
	}
	if !strings.HasSuffix(viewH, "\n#endif\n") {
		t.Fatalf("hexal/view.h must close its guard with exactly one trailing newline: %q", viewH)
	}
	if count := strings.Count(viewH, "typedef struct hex_view_"); count != 2 {
		t.Fatalf("hexal/view.h defines %d specializations, want 2: %q", count, viewH)
	}
	if !strings.Contains(viewH, "typedef struct hex_view_Int32 {") || !strings.Contains(viewH, "typedef struct hex_view_UInt8 {") {
		t.Fatalf("hexal/view.h = %q, want Int32 and UInt8 specializations", viewH)
	}
	if strings.Index(viewH, "typedef struct hex_view_Int32") > strings.Index(viewH, "typedef struct hex_view_UInt8") {
		t.Fatalf("hexal/view.h = %q, specializations must follow C-name order", viewH)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/view.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/view.h component include", files["modules/app.h"])
	}
}

// hexal.h owns none of the view family: a view-only program leaves hexal.h
// free of hex_view_ text, and the rendered view.h matches the previous
// Go-written definitions byte for byte (struct, guards, and trap messages).
func TestViewComponentHexalHeaderOwnsNoViewText(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    view: View<Int32> = View<Int32>.empty()\n    count: Size = view.length()\nend")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if strings.Contains(files["hexal.h"], "hex_view_") {
		t.Fatalf("hexal.h = %q, view definitions must live in hexal/view.h", files["hexal.h"])
	}
	want := `#ifndef HEXAL_VIEW_H
#define HEXAL_VIEW_H

#include "hexal.h"

typedef struct hex_view_Int32 {
    const int32_t *data;
    size_t length;
} hex_view_Int32;
static inline const int32_t *hex_view_at_Int32(hex_view_Int32 view, size_t index) {
    if (index >= view.length) {
        hex_runtime_trap("[Runtime Error] view index out of bounds\n");
    }
    return &view.data[index];
}
static inline hex_view_Int32 hex_view_slice_Int32(hex_view_Int32 view, uint64_t start, uint64_t end) {
    if (!(start <= end && end <= view.length)) {
        hex_runtime_trap("[Runtime Error] view slice bounds out of range\n");
    }
    return (hex_view_Int32){view.data == nullptr ? nullptr : &view.data[start], end - start};
}

#endif
`
	if got := files["hexal/view.h"]; got != want {
		t.Fatalf("hexal/view.h = %q, want %q", got, want)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/view.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/view.h component include", files["modules/app.h"])
	}
}

// Arrays alone select no view records, but the array component declares
// view.h as a dependency, so the header exists as a dependency artifact while
// no module includes it directly.
func TestViewComponentAbsentWithoutReachableViews(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    fixed: Array<Int32, 3> = [1, 2, 3]\n    first: Int32 = fixed[0]\nend")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	viewH, exists := files["hexal/view.h"]
	if !exists {
		t.Fatalf("array-only program emitted no hexal/view.h dependency artifact: %v", files)
	}
	if strings.Contains(viewH, "hex_view_Int32") {
		t.Fatalf("array-only hexal/view.h carries view records: %q", viewH)
	}
	if strings.Contains(files["modules/app.h"], "hexal/view.h") {
		t.Fatalf("modules/app.h = %q, the array component supplies view.h transitively", files["modules/app.h"])
	}
}

// Equivalent compilations render identical view.h bytes.
func TestViewComponentDeterministic(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    view: View<Int32> = View<Int32>.empty()\n    count: Size = view.length()\nend")
	first, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	second, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if first["hexal/view.h"] != second["hexal/view.h"] {
		t.Fatalf("equivalent compilations rendered hexal/view.h differently")
	}
}
