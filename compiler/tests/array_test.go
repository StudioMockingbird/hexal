package tests

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0020 Phase A: fixed inline arrays — declaration, literals, indexing,
// built-in methods, and bounds-safe element access.

func TestArrayDeclarationLiteralAndIndexing(t *testing.T) {
	result := compiler.Compile("fixed: Array<Int32, 3> = [10, 20, 30] total: Int32 = fixed[0] + fixed[2] count: Size = fixed.length() empty: Bool = fixed.is_empty() last: Int32 = fixed.at(2)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_array_Int32_3 {",
		"int32_t data[3];",
		"const hex_array_Int32_3 hex_v_fixed = (hex_array_Int32_3){{10, 20, 30}};",
		"UINT64_C(3)",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
	for _, want := range []string{
		"*hex_array_at_Int32_3(&hex_v_fixed, (size_t)(0))",
		"*hex_array_at_Int32_3(&hex_v_fixed, (size_t)(2))",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestArrayMutableElementWrite(t *testing.T) {
	result := compiler.Compile("mut fixed: Array<Int32, 2> = [1, 2] fixed[0] = 7 fixed[1] = fixed[0] + 1")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"*hex_array_at_mut_Int32_2(&hex_v_fixed, (size_t)(0)) = 7;",
		"if (index >= UINT64_C(2))",
		"\"[Runtime Error] array index out of bounds\\n\"",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestArrayConstantIndexOutOfBoundsIsACompileError(t *testing.T) {
	for _, source := range []string{
		"fixed: Array<Int32, 2> = [1, 2] bad: Int32 = fixed[2]",
		"fixed: Array<Int32, 2> = [1, 2] bad: Int32 = fixed.at(2)",
	} {
		result := compiler.Compile(source)
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
		{"fixed: Array<Int32, 3> = [1, 2]", "Array<Int32, 3> requires exactly 3 elements, got 2"},
		{"fixed: Array<Int32, 2> = [1, 2, 3]", "Array<Int32, 2> requires exactly 2 elements, got 3"},
		{"fixed: Array<Int32, 2> = [1, true]", "expected Int32 initializer, got Bool"},
		{"empty: Array<Int32, 2> = []", "an array literal requires at least one element"},
	} {
		result := compiler.Compile(testCase.source)
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
		{"fixed: Array<Int32, 2> = [1, 2] bad: Int32 = fixed.at()", "at expects 1 argument, got 0"},
		{"type A = Array<Int32, 0>", "an array length must be a positive decimal integer"},
	} {
		result := compiler.Compile(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestArrayElementClassRejectsFunctionValues(t *testing.T) {
	result := compiler.Compile("type Holder = { callbacks: Array<Fun<(Int32)>, 2>, }")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "not an inline array element type") {
		t.Fatalf("Compile stderr = %#v, want inline-element diagnostic", result.Stderr)
	}
}

func TestArrayMembersAndFunctions(t *testing.T) {
	result := compiler.Compile("type Pair = { mut values: Array<Int32, 2>, }\nmut pair: Pair = Pair { values = [3, 4], }\nsum: Int32 = pair.values[0] + pair.values.at(1)\npair.values[1] = 9\nfun first(values: Array<Int32, 3>): Int32\n    return values[0]\nend\nhead: Int32 = first([5, 6, 7])")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"int32_t data[2];",
		"*hex_array_at_mut_Int32_2(&hex_v_pair.hex_m_values, (size_t)(0))",
		"*hex_array_at_Int32_2(&hex_v_pair.hex_m_values, (size_t)(1))",
		"*hex_array_at_mut_Int32_2(&hex_v_pair.hex_m_values, (size_t)(1)) = 9;",
		"*hex_array_at_Int32_3(&hex_v_values, (size_t)(0))",
		"hex_v_head = hex_f_first((hex_array_Int32_3){{5, 6, 7}});",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestNestedArrays(t *testing.T) {
	result := compiler.Compile("grid: Array<Array<Int32, 2>, 2> = [[1, 2], [3, 4]] corner: Int32 = grid[1][0]")
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
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestArrayTrailingCommaLiteral(t *testing.T) {
	result := compiler.Compile("fixed: Array<Int32, 3> = [10, 20, 30, ]")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "(hex_array_Int32_3){{10, 20, 30}}") {
		t.Fatalf("main.c = %q, want trailing-comma array literal", result.MainC)
	}
}
