package generator

import (
	"strings"
	"testing"
)

// An array-using program emits hexal/array.h with every reachable
// specialization exactly once, in C-name order, with its guard, its declared
// hexal.h and view.h includes, and exactly one trailing newline; the owning
// module header includes the component.
func TestArrayComponentEmitsReachableSpecializationsOnce(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    fixed: Array<Int32, 3> := [1, 2, 3]\n    bytes: Array<UInt8, 2> := [4, 5]\n    first: Int32 := fixed[0]\nend")
	files := generateOne(t, program)
	arrayH := files["hexal/array.h"]
	if arrayH == "" {
		t.Fatalf("generated files %v lack hexal/array.h", files)
	}
	// No specialization here has a slice helper, so the header declares no
	// view dependency: a declared include is always a used one.
	if !strings.HasPrefix(arrayH, "#ifndef HEXAL_ARRAY_H\n#define HEXAL_ARRAY_H\n\n#include \"hexal.h\"\n") {
		t.Fatalf("hexal/array.h lost its guard or one of its declared includes: %q", arrayH)
	}
	if strings.Contains(arrayH, "#include \"hexal/view.h\"") {
		t.Fatalf("hexal/array.h declares a view dependency it does not use: %q", arrayH)
	}
	if !strings.HasSuffix(arrayH, "\n#endif\n") {
		t.Fatalf("hexal/array.h must close its guard with exactly one trailing newline: %q", arrayH)
	}
	if count := strings.Count(arrayH, "typedef struct hex_array_"); count != 2 {
		t.Fatalf("hexal/array.h defines %d specializations, want 2: %q", count, arrayH)
	}
	if !strings.Contains(arrayH, "typedef struct hex_array_Int32_3 {") || !strings.Contains(arrayH, "typedef struct hex_array_UInt8_2 {") {
		t.Fatalf("hexal/array.h = %q, want Int32 and UInt8 specializations", arrayH)
	}
	if strings.Index(arrayH, "typedef struct hex_array_Int32_3") > strings.Index(arrayH, "typedef struct hex_array_UInt8_2") {
		t.Fatalf("hexal/array.h = %q, specializations must follow C-name order", arrayH)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/array.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/array.h component include", files["modules/app.h"])
	}
}

// hexal.h owns none of the array family: an array-using program leaves
// hexal.h free of hex_array_ text, and the rendered array.h matches the
// expected definitions byte for byte (struct, the surviving accessor, its
// UINT64_C bounds guard, the slice helper returning the reachable view type,
// and trap messages).
//
// The index is a runtime parameter, so its check survives and hex_array_at_
// is emitted. The binding is immutable, so hex_array_at_mut_ has no surviving
// access and RFC 0088 filters it out — the golden covers both directions of
// the demand rule at once.
func TestArrayComponentHexalHeaderOwnsNoArrayText(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(i: Size) do\n    fixed: Array<Int32, 3> := [1, 2, 3]\n    view: View<Int32> := fixed.slice(0, 2)\n    first: Int32 := fixed[i]\nend")
	files := generateOne(t, program)
	if strings.Contains(files["hexal.h"], "hex_array_") {
		t.Fatalf("hexal.h = %q, array definitions must live in hexal/array.h", files["hexal.h"])
	}
	want := `#ifndef HEXAL_ARRAY_H
#define HEXAL_ARRAY_H

#include "hexal.h"
#include "hexal/view.h"

typedef struct hex_array_Int32_3 {
    int32_t data[3];
} hex_array_Int32_3;
static inline const int32_t *hex_array_at_Int32_3(const hex_array_Int32_3 *array, size_t index) {
    if (index >= UINT64_C(3)) {
        hex_runtime_trap("[Runtime Error] array index out of bounds\n");
    }
    return &array->data[index];
}

static inline hex_view_Int32 hex_array_slice_Int32_3(const hex_array_Int32_3 *array, uint64_t start, uint64_t end) {
    if (!(start <= end && end <= UINT64_C(3))) {
        hex_runtime_trap("[Runtime Error] array slice bounds out of range\n");
    }
    return (hex_view_Int32){&array->data[start], end - start};
}

#endif
`
	if got := files["hexal/array.h"]; got != want {
		t.Fatalf("hexal/array.h = %q, want %q", got, want)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/array.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/array.h component include", files["modules/app.h"])
	}
}

// A program without reachable Array types emits no array artifact and no
// module includes it.
// Equivalent compilations render identical array.h bytes.
// A specialization whose element is a module-owned object is emitted into the
// consuming module's header, after that element's typedef. The program-wide
// component cannot carry it: components are shared across modules and cannot
// declare a per-module type, and module headers never include one another.
// A program whose only specializations are module-typed therefore emits no
// component artifact and no include for one.
func TestArrayModuleOwnedElementSpecializationLivesInModuleHeader(t *testing.T) {
	program := checkedGeneratorSource(t, "type Point = { x: Int32, }\nfun demo() do\n    fixed: Array<Point, 2> := [Point { x = 1, }, Point { x = 2, }]\n    first: Int32 := fixed[0].x\nend")
	files := generateOne(t, program)
	if got := files["hexal/array.h"]; got != "" {
		t.Fatalf("hexal/array.h = %q, want no component artifact: its only specialization is module-typed", got)
	}
	header := files["modules/app.h"]
	specialization := strings.Index(header, "typedef struct hex_array_Point_2 {")
	if specialization < 0 {
		t.Fatalf("modules/app.h = %q, want the Point specialization", header)
	}
	element := strings.Index(header, "struct hex_t_m3_app_Point {")
	if element < 0 || element > specialization {
		t.Fatalf("modules/app.h declares hex_array_Point_2 at %d before its element type at %d; the element must precede it", specialization, element)
	}
	if strings.Contains(header, "#include \"hexal/array.h\"") {
		t.Fatalf("modules/app.h includes hexal/array.h, which is not emitted")
	}
}
