package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestArrayDeclarationLiteralAndIndexing(t *testing.T) {
	result := compileSource("fixed: Array<Int32, 3> = [10, 20, 30] total: Int32 = fixed[0] + fixed[2] count: Size = fixed.length() empty: Bool = fixed.length() == 0 last: Int32 = fixed[2]")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_array_Int32_3 {",
		"int32_t data[3];",
		"const hex_array_Int32_3 hex_v_fixed = (hex_array_Int32_3){{10, 20, 30}};",
		"UINT64_C(3)",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) && !strings.Contains(arrayH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
	// The specialization struct, the typed accessors, and their UINT64_C
	// bounds guards are owned by the array component, not hexal.h.
	arrayHeader := arrayH(t, result)
	for _, want := range []string{
		"#ifndef HEXAL_ARRAY_H",
		"#include \"hexal.h\"",
		"#include \"hexal/view.h\"",
		"typedef struct hex_array_Int32_3 {",
		"int32_t data[3];",
		"static inline const int32_t *hex_array_at_Int32_3(const hex_array_Int32_3 *array, size_t index) {",
		"if (index >= UINT64_C(3))",
		"hex_runtime_trap(\"[Runtime Error] array index out of bounds\\n\");",
		"static inline int32_t *hex_array_at_mut_Int32_3(hex_array_Int32_3 *array, size_t index) {",
		"#endif",
	} {
		if !strings.Contains(arrayHeader, want) {
			t.Fatalf("hexal/array.h = %q, want %q", arrayHeader, want)
		}
	}
	if !strings.Contains(rootH(t, result), "#include \"hexal/array.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/array.h component include", rootH(t, result))
	}
	for _, want := range []string{
		"*hex_array_at_Int32_3(&hex_v_fixed, (size_t)(0))",
		"*hex_array_at_Int32_3(&hex_v_fixed, (size_t)(2))",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestArrayMutableElementWrite(t *testing.T) {
	result := compileSource("mut fixed: Array<Int32, 2> = [1, 2] fixed[0] = 7 fixed[1] = fixed[0] + 1")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"*hex_array_at_mut_Int32_2(&hex_v_fixed, (size_t)(0)) = 7;",
		"if (index >= UINT64_C(2))",
		"\"[Runtime Error] array index out of bounds\\n\"",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) && !strings.Contains(arrayH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestArrayConstantIndexOutOfBoundsIsACompileError(t *testing.T) {
	for _, source := range []string{
		"fixed: Array<Int32, 2> = [1, 2] bad: Int32 = fixed[2]",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "out of bounds") {
			t.Fatalf("Compile(%q) stderr = %#v, want out-of-bounds diagnostic", source, result.Stderr)
		}
	}
}

func TestArrayLiteralRequiresExpectedTypeAndExactCount(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"value: Int32 = [1, 2]", "an array literal requires an expected Array<T, N> destination type"},
		{"fixed: Array<Int32, 3> = [1, 2]", "Array<Int32, 3> requires exactly 3 elements; got 2"},
		{"fixed: Array<Int32, 2> = [1, 2, 3]", "Array<Int32, 2> requires exactly 2 elements; got 3"},
		{"fixed: Array<Int32, 2> = [1, true]", "expected Int32 initializer; got Bool"},
		{"empty: Array<Int32, 2> = []", "an array literal requires at least one element"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestArrayIndexErrors(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fixed: Array<Int32, 2> = [1, 2] bad: Int32 = fixed[true]", "an array index must be an integer; got Bool"},
		{"fixed: Array<Int32, 2> = [1, 2] bad: Int32 = fixed[-1]", "an array index must be non-negative"},
		{"value: Int32 = 1 bad: Int32 = value[0]", "cannot index Int32; expected Array<T, N>"},
		{"fixed: Array<Int32, 2> = [1, 2] bad: Int32 = fixed.first()", "Array<Int32, 2> has no method first"},
		{"fixed: Array<Int32, 2> = [1, 2] bad: UInt64 = fixed.length(1)", "length expects no arguments"},
		{"fixed: Array<Int32, 2> = [1, 2] bad: Int32 = fixed.at()", "Array<Int32, 2> has no method at"},
		{"type A = Array<Int32, 0>", "an array length must be a positive decimal integer"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestArrayElementClassRejectsFunctionValues(t *testing.T) {
	result := compileSource("type Holder = { callbacks: Array<Fun<(Int32)>, 2>, }")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "not an inline array element type") {
		t.Fatalf("Compile stderr = %#v, want inline-element diagnostic", result.Stderr)
	}
}

func TestArrayMembersAndFunctions(t *testing.T) {
	result := compileSource("type Pair = { mut values: Array<Int32, 2>, }\nmut pair: Pair = Pair { values = [3, 4], }\nsum: Int32 = pair.values[0] + pair.values[1]\npair.values[1] = 9\nfun first(values: Array<Int32, 3>): Int32 do\n    return values[0]\nend\nhead: Int32 = first([5, 6, 7])")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"int32_t data[2];",
		"*hex_array_at_mut_Int32_2(&hex_v_pair.hex_m_values, (size_t)(0))",
		"*hex_array_at_mut_Int32_2(&hex_v_pair.hex_m_values, (size_t)(1))",
		"*hex_array_at_mut_Int32_2(&hex_v_pair.hex_m_values, (size_t)(1)) = 9;",
		"*hex_array_at_Int32_3(&hex_v_values, (size_t)(0))",
		"hex_v_head = hex_f_m3_app_first((hex_array_Int32_3){{5, 6, 7}});",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) && !strings.Contains(arrayH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestNestedArrays(t *testing.T) {
	result := compileSource("grid: Array<Array<Int32, 2>, 2> = [[1, 2], [3, 4]] corner: Int32 = grid[1][0]")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_array_Int32_2 {",
		"typedef struct hex_array_Array_Int32__2__2 {",
		"hex_array_Int32_2 data[2];",
		"const hex_array_Array_Int32__2__2 hex_v_grid = (hex_array_Array_Int32__2__2){{(hex_array_Int32_2){{1, 2}}, (hex_array_Int32_2){{3, 4}}}};",
		"*hex_array_at_Int32_2(&*hex_array_at_Array_Int32__2__2(&hex_v_grid, (size_t)(1)), (size_t)(0))",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(hexalH(t, result), want) && !strings.Contains(arrayH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
	// The nested specialization must precede the array embedding it: the
	// inner struct is embedded by value, so its definition must complete
	// first.
	arrayHeader := arrayH(t, result)
	if strings.Index(arrayHeader, "typedef struct hex_array_Int32_2 {") > strings.Index(arrayHeader, "typedef struct hex_array_Array_Int32__2__2 {") {
		t.Fatalf("hexal/array.h = %q, nested specialization must precede its embeder", arrayHeader)
	}
}

// The array slice helper is an Array specialization: it returns the view
// type spelled by the view component and lives in hexal/array.h with its
// UINT64_C range guard.
func TestArraySliceHelperLivesInArrayHeader(t *testing.T) {
	result := compileSource("fun demo() do\n    fixed: Array<Int32, 3> = [10, 20, 30]\n    view: View<Int32> = fixed.slice(0, 2)\n    first: Int32 = view[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	arrayHeader := arrayH(t, result)
	for _, want := range []string{
		"#include \"hexal/view.h\"",
		"static inline hex_view_Int32 hex_array_slice_Int32_3(const hex_array_Int32_3 *array, uint64_t start, uint64_t end) {",
		"if (!(start <= end && end <= UINT64_C(3)))",
		"hex_runtime_trap(\"[Runtime Error] array slice bounds out of range\\n\");",
		"return (hex_view_Int32){&array->data[start], end - start};",
	} {
		if !strings.Contains(arrayHeader, want) {
			t.Fatalf("hexal/array.h = %q, want %q", arrayHeader, want)
		}
	}
}

func TestArrayTrailingCommaLiteral(t *testing.T) {
	result := compileSource("fixed: Array<Int32, 3> = [10, 20, 30, ]")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "(hex_array_Int32_3){{10, 20, 30}}") {
		t.Fatalf("modules/app.c = %q, want trailing-comma array literal", rootC(t, result))
	}
}

// arrayH returns the generated array component header.
