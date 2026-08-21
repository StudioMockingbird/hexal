package generator

import (
	"strings"
	"testing"
)

// A program using a checked conversion emits hexal/numeric.h containing that
// helper; a scalar-only program emits no numeric component.
func TestNumericComponentEmitsConversionHelper(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    x: Float64 := 42.0\n    y: Int32 := x.to<Int32>()\nend")
	files := generateOne(t, program)
	numeric, exists := files["hexal/numeric.h"]
	if !exists {
		t.Fatalf("conversion program emitted no hexal/numeric.h: %v", files)
	}
	if !strings.HasPrefix(numeric, "#ifndef HEXAL_NUMERIC_H\n#define HEXAL_NUMERIC_H\n") || !strings.HasSuffix(numeric, "\n#endif\n") {
		t.Fatalf("hexal/numeric.h lost its guard: %q", numeric)
	}
	if !strings.Contains(numeric, "#include \"hexal.h\"") {
		t.Fatalf("hexal/numeric.h lacks hexal.h include: %q", numeric)
	}
	if !strings.Contains(numeric, "hex_convert_double_int32_t") {
		t.Fatalf("hexal/numeric.h lacks the Float64->Int32 conversion: %q", numeric)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/numeric.h\"") {
		t.Fatalf("modules/app.h = %q, want the numeric component include", files["modules/app.h"])
	}
}

// A program using division emits guarded div/rem helpers in numeric.h.
func TestNumericComponentEmitsDivisionHelpers(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(a: Int32, b: Int32): Int32 do\n    return a / b\nend")
	files := generateOne(t, program)
	numeric, exists := files["hexal/numeric.h"]
	if !exists {
		t.Fatalf("division program emitted no hexal/numeric.h: %v", files)
	}
	if !strings.Contains(numeric, "hex_div_int32") || !strings.Contains(numeric, "hex_rem_int32") {
		t.Fatalf("hexal/numeric.h lacks division helpers: %q", numeric)
	}
}

// A direct-only conversion program emits no numeric component.
func TestNumericComponentAbsentForDirectConversions(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    x: Int32 := 42\n    y: Float64 := x.to<Float64>()\nend")
	files := generateOne(t, program)
	if _, exists := files["hexal/numeric.h"]; exists {
		t.Fatalf("direct-conversion program emitted hexal/numeric.h")
	}
	if strings.Contains(files["modules/app.h"], "hexal/numeric.h") {
		t.Fatalf("modules/app.h = %q, must not include unselected numeric component", files["modules/app.h"])
	}
}

// Equivalent compilations render identical numeric artifacts.
func TestNumericTemplatesRenderDeterministic(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    x: Float64 := 42.0\n    y: Int32 := x.to<Int32>()\nend")
	first := generateOne(t, program)
	second := generateOne(t, program)
	if first["hexal/numeric.h"] != second["hexal/numeric.h"] {
		t.Fatalf("equivalent compilations rendered hexal/numeric.h differently")
	}
}
