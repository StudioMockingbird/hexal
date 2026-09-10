package generator

import (
	"strings"
	"testing"
)

// A slice-using program emits hexal/slice.h with every reachable
// specialization exactly once, in C-name order, with its guard, its
// hexal.h include, and exactly one trailing newline; the owning module
// header includes the component.
func TestSliceComponentEmitsReachableSpecializationsOnce(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(data: Ptr<UInt8>) do\n    first: Slice<Int32> := Slice<Int32>.empty()\n    second: Slice<UInt8> := Slice<UInt8>.from_pointer(data, 0)\nend")
	files := generateOne(t, program)
	viewH := files["hexal/slice.h"]
	if viewH == "" {
		t.Fatalf("generated files %v lack hexal/slice.h", files)
	}
	if !strings.HasPrefix(viewH, "#ifndef HEXAL_SLICE_H\n#define HEXAL_SLICE_H\n\n#include \"hexal.h\"\n") {
		t.Fatalf("hexal/slice.h lost its guard or its hexal.h include: %q", viewH)
	}
	if !strings.HasSuffix(viewH, "\n#endif\n") {
		t.Fatalf("hexal/slice.h must close its guard with exactly one trailing newline: %q", viewH)
	}
	if count := strings.Count(viewH, "typedef struct hex_slice_"); count != 2 {
		t.Fatalf("hexal/slice.h defines %d specializations, want 2: %q", count, viewH)
	}
	if !strings.Contains(viewH, "typedef struct hex_slice_Int32 {") || !strings.Contains(viewH, "typedef struct hex_slice_UInt8 {") {
		t.Fatalf("hexal/slice.h = %q, want Int32 and UInt8 specializations", viewH)
	}
	if strings.Index(viewH, "typedef struct hex_slice_Int32") > strings.Index(viewH, "typedef struct hex_slice_UInt8") {
		t.Fatalf("hexal/slice.h = %q, specializations must follow C-name order", viewH)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/slice.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/slice.h component include", files["modules/app.h"])
	}
}

// hexal.h owns none of the slice family: a slice-only program leaves hexal.h
// free of hex_slice_ text, and the rendered slice.h matches the previous
// Go-written definitions byte for byte (struct, guards, and trap messages).
func TestSliceComponentHexalHeaderOwnsNoSliceText(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    view: Slice<Int32> := Slice<Int32>.empty()\n    count: Size := view.length()\nend")
	files := generateOne(t, program)
	if strings.Contains(files["hexal.h"], "hex_slice_") {
		t.Fatalf("hexal.h = %q, view definitions must live in hexal/slice.h", files["hexal.h"])
	}
	want := `#ifndef HEXAL_SLICE_H
#define HEXAL_SLICE_H

#include "hexal.h"

typedef struct hex_slice_Int32 {
    const int32_t *data;
    size_t length;
} hex_slice_Int32;
static inline const int32_t *hex_slice_at_Int32(hex_slice_Int32 slice, size_t index) {
    if (index >= slice.length) {
        hex_runtime_trap("[Runtime Error] slice index out of bounds\n");
    }
    return &slice.data[index];
}
static inline hex_slice_Int32 hex_slice_slice_Int32(hex_slice_Int32 slice, uint64_t start, uint64_t end) {
    if (!(start <= end && end <= slice.length)) {
        hex_runtime_trap("[Runtime Error] slice slice bounds out of range\n");
    }
    return (hex_slice_Int32){slice.data == nullptr ? nullptr : &slice.data[start], end - start};
}

#endif
`
	if got := files["hexal/slice.h"]; got != want {
		t.Fatalf("hexal/slice.h = %q, want %q", got, want)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/slice.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/slice.h component include", files["modules/app.h"])
	}
}

// An array that is never sliced reaches no slice, and the array component
// renders no slice helper: nothing names the slice component and no
// artifact is emitted for it. A component that declared the dependency
// anyway would ship a header holding only its include guard.
func TestSliceComponentAbsentWithoutReachableSlices(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    fixed: Array<Int32, 3> := [1, 2, 3]\n    first: Int32 := fixed[0]\nend")
	files := generateOne(t, program)
	if viewH, exists := files["hexal/slice.h"]; exists {
		t.Fatalf("array-only program emitted hexal/slice.h with nothing to declare: %q", viewH)
	}
	if strings.Contains(files["hexal/array.h"], "hexal/slice.h") {
		t.Fatalf("hexal/array.h = %q, want no slice include: no specialization has a slice helper", files["hexal/array.h"])
	}
	if strings.Contains(files["modules/app.h"], "hexal/slice.h") {
		t.Fatalf("modules/app.h = %q, want no slice include", files["modules/app.h"])
	}
}

// Equivalent compilations render identical slice.h bytes.
