package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestCoreScalars(t *testing.T) {
	source := "visible: Bool = true " +
		"u8: UInt8 = 255 u16: UInt16 = 65535 " +
		"u32: UInt32 = 4294967295 u64: UInt64 = 18446744073709551615 " +
		"i8: Int8 = -128 i16: Int16 = -32768 " +
		"i32: Int32 = -2147483648 i64: Int64 = -9223372036854775808 " +
		"f32: Float32 = -0.0 f64: Float64 = 6.02e23"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const bool hex_v_visible = true;",
		"const uint8_t hex_v_u8 = 255;",
		"const uint16_t hex_v_u16 = 65535;",
		"const uint32_t hex_v_u32 = 4294967295;",
		"const uint64_t hex_v_u64 = UINT64_C(18446744073709551615);",
		"const int8_t hex_v_i8 = INT8_MIN;",
		"const int16_t hex_v_i16 = INT16_MIN;",
		"const int32_t hex_v_i32 = INT32_MIN;",
		"const int64_t hex_v_i64 = INT64_MIN;",
		"const float hex_v_f32 = -0.0f;",
		"const double hex_v_f64 = 6.02e+23;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestIntegerRadices(t *testing.T) {
	result := compileSource("decimal: UInt16 = 255 hexadecimal: UInt16 = 0xFF binary: UInt16 = 0b1111_1111 octal: UInt16 = 0o377")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const uint16_t hex_v_decimal = 255;",
		"const uint16_t hex_v_hexadecimal = 0xFF;",
		"const uint16_t hex_v_binary = 0b11111111;",
		"const uint16_t hex_v_octal = 255;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestContextualAssignmentAndPointerValue(t *testing.T) {
	result := compileSource("mut byte: UInt8 = 0 byte = 255 mut value: Int8 = 0 writer: MutPtr<Int8> = ref value writer.value = -128")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{"hex_v_byte = 255;", "*hex_v_writer = INT8_MIN;"} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestRejectsScalarMismatchesAndRanges(t *testing.T) {
	testCases := []struct {
		source string
		want   string
	}{
		{"x: UInt8 = 256", "outside the UInt8 range"},
		{"x: Int8 = 128", "outside the Int8 range"},
		{"x: Int8 = -129", "outside the Int8 range"},
		{"x: UInt64 = 18446744073709551616", "outside the UInt64 range"},
		{"x: UInt8 = -0", "negated integer literal requires a signed destination"},
		{"x: Float32 = 1", "expected Float32 initializer; got Int32"},
		{"x: Int32 = 1.0", "expected Int32 initializer; got Float64"},
		{"x: Bool = 1", "expected Bool initializer; got Int32"},
	}
	for _, testCase := range testCases {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Errorf("Compile(%q) = %#v, want diagnostic containing %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestLexicalDiagnostics(t *testing.T) {
	testCases := []struct {
		source string
		want   string
	}{
		{"x: UInt8 = 007", "decimal integer literals cannot have leading zeros"},
		{"x: UInt8 = 0XFF", "integer base prefixes must be lowercase"},
		{"x: UInt8 = 0b102", "malformed binary integer literal"},
		{"x: UInt8 = 0o8", "malformed octal integer literal"},
		{"x: UInt8 = 0x_FF", "malformed hexadecimal literal"},
		{"x: Float32 = .5", "malformed floating literal"},
		{"x: Float32 = 1.", "malformed decimal floating literal"},
		{"x: Float32 = 1e", "malformed decimal floating literal"},
	}
	for _, testCase := range testCases {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Errorf("Compile(%q) = %#v, want diagnostic containing %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestGeneralNegativeSyntax(t *testing.T) {
	result := compileSource("mut name: Int32 = 5 negative: Int32 = -name repeated: Int32 = - -name")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile general unary negation = %#v, want success", result)
	}
	for _, want := range []string{
		"const int32_t hex_v_negative = hex_wrap_neg_int32_t(hex_v_name);",
		"const int32_t hex_v_repeated = hex_wrap_neg_int32_t(hex_wrap_neg_int32_t(hex_v_name));",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want unary-negation fragment %q", rootC(t, result), want)
		}
	}

	result = compileSource("x: Int32 = - -- a comment\n 128")
	if result.ExitCode != compiler.ExitSuccess || !strings.Contains(rootC(t, result), "const int32_t hex_v_x = -128;") {
		t.Fatalf("Compile with comment-separated negative literal = %#v, want success", result)
	}
}

func TestFloatUnaryNegation(t *testing.T) {
	result := compileSource("mut f32Value: Float32 = 1.5 negative32: Float32 = -f32Value mut f64Value: Float64 = 2.5 negative64: Float64 = -f64Value")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile float unary negation = %#v, want success", result)
	}
	for _, want := range []string{
		"const float hex_v_negative32 = (-hex_v_f32Value);",
		"const double hex_v_negative64 = (-hex_v_f64Value);",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want float unary-negation fragment %q", rootC(t, result), want)
		}
	}
}

func TestNegativeZeroUnaryFolding(t *testing.T) {
	result := compileSource("f32: Float32 = -(0.0) f64: Float64 = -(0.0)")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile negative-zero unary folding = %#v, want success", result)
	}
	for _, want := range []string{
		"const float hex_v_f32 = -0.0f;",
		"const double hex_v_f64 = -0.0;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want negative-zero fragment %q", rootC(t, result), want)
		}
	}
}

func TestFloatRoundingAndUnderflow(t *testing.T) {
	result := compileSource("tie: Float32 = 1.000000059604644775390625 underflow: Float32 = 1e-1000 negative: Float32 = -1e-1000")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const float hex_v_tie = 1.0f;",
		"const float hex_v_underflow = 0.0f;",
		"const float hex_v_negative = -0.0f;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
	for _, source := range []string{"x: Float32 = 1e1000", "x: Float64 = 1e10000"} {
		result = compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "outside the") {
			t.Errorf("Compile(%q) = %#v, want finite-range diagnostic", source, result.Stderr)
		}
	}
}

func TestPointerScalarMappings(t *testing.T) {
	result := compileSource("mut value: UInt8 = 1 reader: Ptr<UInt8> = ref value writer: MutPtr<UInt8> = ref value mut float_value: Float32 = 1.0 float_writer: MutPtr<Float32> = ref float_value nested: Ptr<MutPtr<Float32>> = ref float_writer")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"uint8_t hex_v_value = 1;",
		"const uint8_t *const hex_v_reader = &hex_v_value;",
		"uint8_t *const hex_v_writer = &hex_v_value;",
		"float *const *const hex_v_nested = &hex_v_float_writer;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestSizeLiteralTargetGuard(t *testing.T) {
	result := compileSource("count: Size = 5000000000\nsmall: Size = 3\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static_assert(5000000000 <= SIZE_MAX, \"Size literal 5000000000 requires a size_t target wide enough\");",
		"const size_t hex_v_count = 5000000000;",
		"const size_t hex_v_small = 3;",
	} {
		if !strings.Contains(rootH(t, result), want) && !strings.Contains(rootC(t, result), want) && !strings.Contains(hexalH(t, result), want) {
			t.Fatalf("generated C lacks %q:\nmodules/app.h:\n%s\nmodules/app.c:\n%s", want, rootH(t, result), rootC(t, result))
		}
	}
	if strings.Contains(rootH(t, result), "sizeof(size_t) == 8") || strings.Contains(hexalH(t, result), "sizeof(size_t) == 8") {
		t.Fatalf("generated C still asserts a 64-bit size_t profile:\n%s", rootH(t, result))
	}
}

func TestInt32Declaration(t *testing.T) {
	result := compileSource("x: Int32 = 13")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitSuccess)
	}
	if len(result.Stderr) != 0 {
		t.Fatalf("Compile stderr = %#v, want empty", result.Stderr)
	}

	wantC := "#include \"modules/app.h\"\n\nint main(void) {\n#line 1 \"app.hex\"\n    const int32_t hex_v_x = 13;\n    return 0;\n}\n"
	if rootC(t, result) != wantC {
		t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), wantC)
	}

	hexalH := hexalH(t, result)
	if !strings.Contains(hexalH, "#ifndef HEXAL_H") || !strings.Contains(hexalH, "#include <stdint.h>") {
		t.Fatalf("hexal.h = %q, want guard and <stdint.h>", hexalH)
	}
	for _, forbidden := range []string{
		"#include <stdbool.h>", "#include <limits.h>", "#include <float.h>",
		"#include <stdio.h>", "#include <stddef.h>", "#include <stdlib.h>",
		"static_assert", "hex_eos",
	} {
		if strings.Contains(hexalH, forbidden) {
			t.Fatalf("hexal.h = %q, want no %q", hexalH, forbidden)
		}
	}
}

func TestBoolDeclaration(t *testing.T) {
	result := compileSource("flag: Bool = true")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}

	wantC := "#include \"modules/app.h\"\n\nint main(void) {\n#line 1 \"app.hex\"\n    const bool hex_v_flag = true;\n    return 0;\n}\n"
	if rootC(t, result) != wantC {
		t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), wantC)
	}
}

// Byte is the canonical transparent alias of UInt8, and byte literals
// b'...' are ordinary UInt8 constants.
func TestByteAndByteTypeBehavior(t *testing.T) {
	result := compileSource("letter: UInt8 = b'A'")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile returned %#v, want byte literal accepted", result)
	}
	if !strings.Contains(rootC(t, result), "65") {
		t.Fatalf("generated C lacks the byte value: %s", rootC(t, result))
	}
	result = compileSource("letter: Byte = 65 again: UInt8 = letter")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile returned %#v, want Byte alias accepted", result)
	}
}

func TestAdditionalNumericTypes(t *testing.T) {
	result := compileSource("count: Int64 = 9_000_000_000 single: Float32 = 3.14 precise: Float64 = 6.02e23")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"int64_t hex_v_count = INT64_C(9000000000);",
		"float hex_v_single = 3.14f;",
		"double hex_v_precise = 6.02e+23;",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
	// Float representation is a toolchain contract: no generated probe or
	// <float.h> dependency appears for float use.
	for _, forbidden := range []string{"static_assert(sizeof(float)", "static_assert(sizeof(double)", "#include <float.h>"} {
		if strings.Contains(rootH(t, result), forbidden) || strings.Contains(hexalH(t, result), forbidden) {
			t.Fatalf("generated output contains the removed target probe %q", forbidden)
		}
	}
}

func TestNumericLiteralShapeDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"x: Byte = 13", ""},
		{"x: Float32 = 13", "[Type Error] expected Float32 initializer; got Int32 at app.hex:1:14"},
		{"x: Int64 = 13.0", "[Type Error] expected Int64 initializer; got Float64 at app.hex:1:12"},
	} {
		result := compileSource(testCase.source)
		if testCase.want == "" {
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("Compile(%q) failed: %v", testCase.source, result.Stderr)
			}
			continue
		}
		if len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
			t.Fatalf("Compile(%q) std.err = %#v, want [%q]", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestDefaultsUncontextualizedNumericFamilies(t *testing.T) {
	result := compileSource("x = 13 y = 3.14")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 2 {
		t.Fatalf("Compile returned %#v, want two unknown-variable diagnostics", result)
	}

	// The assignment forms are rejected before a checked operand can expose a
	// default type. Declarations provide the current contextual entry point.
	result = compileSource("whole: Int32 = 13 fraction: Float64 = 3.14")
	if result.ExitCode != compiler.ExitSuccess || !strings.Contains(rootC(t, result), "const int32_t hex_v_whole = 13;") || !strings.Contains(rootC(t, result), "const double hex_v_fraction = 3.14;") {
		t.Fatalf("Compile returned %#v, want Int32 and Float64 declarations", result)
	}
}

// The generated C keeps the literal's original spelling so a mask written in
// hexadecimal stays readable in the emitted source.
func TestHexLiteralPreservesSpelling(t *testing.T) {
	result := compileSource("mask: Int32 = 0xFF")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}

	wantC := "#include \"modules/app.h\"\n\nint main(void) {\n#line 1 \"app.hex\"\n    const int32_t hex_v_mask = 0xFF;\n    return 0;\n}\n"
	if rootC(t, result) != wantC {
		t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), wantC)
	}
}

func TestRejectsMismatchedInitializer(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"x: Int32 = true", "[Type Error] expected Int32 initializer; got Bool at app.hex:1:12"},
		{"flag: Bool = 1", "[Type Error] expected Bool initializer; got Int32 at app.hex:1:14"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure {
			t.Fatalf("Compile(%q) exit code = %d, want %d", testCase.source, result.ExitCode, compiler.ExitFailure)
		}
		if len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
			t.Fatalf("Compile(%q) std.err = %#v, want [%q]", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestRejectsHexPrefixWithoutDigits(t *testing.T) {
	result := compileSource("mask: Int32 = 0x")
	want := "[Syntax Error] malformed hexadecimal literal at app.hex:1:15"
	if len(result.Stderr) == 0 || result.Stderr[0] != want {
		t.Fatalf("std.err = %#v, want first entry %q", result.Stderr, want)
	}
}

func TestRejectsOutOfRangeHex(t *testing.T) {
	result := compileSource("mask: Int32 = 0x80000000")
	want := "[Type Error] given value is outside the Int32 range at app.hex:1:15"
	if len(result.Stderr) != 1 || result.Stderr[0] != want {
		t.Fatalf("std.err = %#v, want [%q]", result.Stderr, want)
	}
}

func TestRejectsMalformedHexadecimalLiteral(t *testing.T) {
	result := compileSource("x: Int32 = 0x")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, compiler.ExitFailure)
	}
	want := []string{"[Syntax Error] malformed hexadecimal literal at app.hex:1:12"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}
