package compiler

// Scalar types, numeric literals, radices, contextual typing, and ranges. Spec 0003.

import (
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
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
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
		"const float hex_v_f32 = -0x0p+0f;",
		"const double hex_v_f64 = 0x1.fde9f10a8d361p+78;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestIntegerRadices(t *testing.T) {
	result := Compile("decimal: UInt16 = 255 hexadecimal: UInt16 = 0xFF binary: UInt16 = 0b1111_1111 octal: UInt16 = 0o377")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const uint16_t hex_v_decimal = 255;",
		"const uint16_t hex_v_hexadecimal = 0xFF;",
		"const uint16_t hex_v_binary = 0b11111111;",
		"const uint16_t hex_v_octal = 255;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestContextualAssignmentAndPointerValue(t *testing.T) {
	result := Compile("mut byte: UInt8 = 0 byte = 255 mut value: Int8 = 0 writer: MutPtr<Int8> = ref value writer.value = -128")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{"hex_v_byte = 255;", "*hex_v_writer = INT8_MIN;"} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
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
		{"x: Float32 = 1", "expected Float32 initializer, got Int32"},
		{"x: Int32 = 1.0", "expected Int32 initializer, got Float64"},
		{"x: Bool = 1", "expected Bool initializer, got Int32"},
	}
	for _, testCase := range testCases {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], testCase.want) {
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
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Errorf("Compile(%q) = %#v, want diagnostic containing %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestGeneralNegativeSyntax(t *testing.T) {
	result := Compile("mut name: Int32 = 5 negative: Int32 = -name repeated: Int32 = - -name")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile general unary negation = %#v, want success", result)
	}
	negative := "((uint64_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_name) <= (uint64_t)INT32_MAX ? (int32_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_name) : INT32_MIN + (int32_t)((uint64_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_name) - (uint64_t)INT32_MAX - (uint64_t)1))"
	for _, want := range []string{
		"const int32_t hex_v_negative = " + negative + ";",
		"const int32_t hex_v_repeated = ((uint64_t)(uint32_t)((uint64_t)0 - (uint64_t)(",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want unary-negation fragment %q", result.MainC, want)
		}
	}

	result = Compile("x: Int32 = - -- a comment\n 128")
	if result.ExitCode != ExitSuccess || !strings.Contains(result.MainC, "const int32_t hex_v_x = -128;") {
		t.Fatalf("Compile with comment-separated negative literal = %#v, want success", result)
	}
}

func TestFloatUnaryNegation(t *testing.T) {
	result := Compile("mut f32Value: Float32 = 1.5 negative32: Float32 = -f32Value mut f64Value: Float64 = 2.5 negative64: Float64 = -f64Value")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile float unary negation = %#v, want success", result)
	}
	for _, want := range []string{
		"const float hex_v_negative32 = (-hex_v_f32Value);",
		"const double hex_v_negative64 = (-hex_v_f64Value);",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want float unary-negation fragment %q", result.MainC, want)
		}
	}
}

func TestNegativeZeroUnaryFolding(t *testing.T) {
	result := Compile("f32: Float32 = -(0.0) f64: Float64 = -(0.0)")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile negative-zero unary folding = %#v, want success", result)
	}
	for _, want := range []string{
		"const float hex_v_f32 = -0x0p+0f;",
		"const double hex_v_f64 = -0x0p+0;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want negative-zero fragment %q", result.MainC, want)
		}
	}
}

func TestFloatRoundingAndUnderflow(t *testing.T) {
	result := Compile("tie: Float32 = 1.000000059604644775390625 underflow: Float32 = 1e-1000 negative: Float32 = -1e-1000")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"const float hex_v_tie = 0x1p+0f;",
		"const float hex_v_underflow = 0x0p+0f;",
		"const float hex_v_negative = -0x0p+0f;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
	for _, source := range []string{"x: Float32 = 1e1000", "x: Float64 = 1e10000"} {
		result = Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], "outside the") {
			t.Errorf("Compile(%q) = %#v, want finite-range diagnostic", source, result.Stderr)
		}
	}
}

func TestPointerScalarMappings(t *testing.T) {
	result := Compile("mut value: UInt8 = 1 reader: Ptr<UInt8> = ref value writer: MutPtr<UInt8> = ref value mut float_value: Float32 = 1.0 float_writer: MutPtr<Float32> = ref float_value nested: Ptr<MutPtr<Float32>> = ref float_writer")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %#v", result.Stderr)
	}
	for _, want := range []string{
		"uint8_t hex_v_value = 1;",
		"const uint8_t *const hex_v_reader = &hex_v_value;",
		"uint8_t *const hex_v_writer = &hex_v_value;",
		"float *const *const hex_v_nested = &hex_v_float_writer;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

// RFC 0049 item 6: a Size literal whose fit depends on the C target emits a
// static_assert against SIZE_MAX, and the generated C no longer asserts a
// 64-bit size_t profile.
func TestSizeLiteralTargetGuard(t *testing.T) {
	result := Compile("count: Size = 5000000000\nsmall: Size = 3\n")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static_assert(5000000000 <= SIZE_MAX, \"Size literal 5000000000 requires a size_t target wide enough\");",
		"const size_t hex_v_count = 5000000000;",
		"const size_t hex_v_small = 3;",
	} {
		if !strings.Contains(result.MainH, want) && !strings.Contains(result.MainC, want) {
			t.Fatalf("generated C lacks %q:\nmain.h:\n%s\nmain.c:\n%s", want, result.MainH, result.MainC)
		}
	}
	if strings.Contains(result.MainH, "sizeof(size_t) == 8") {
		t.Fatalf("generated C still asserts a 64-bit size_t profile:\n%s", result.MainH)
	}
}

func TestInt32Declaration(t *testing.T) {
	result := Compile("x: Int32 = 13")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitSuccess)
	}
	if len(result.Stderr) != 0 {
		t.Fatalf("Compile stderr = %#v, want empty", result.Stderr)
	}

	wantC := "#include \"main.h\"\n\nint main(void) {\n#line 1 \"main.hex\"\n    const int32_t hex_v_x = 13;\n    return EXIT_SUCCESS;\n}\n"
	if result.MainC != wantC {
		t.Fatalf("main.c = %q, want %q", result.MainC, wantC)
	}

	for _, want := range []string{"#include <limits.h>", "static_assert(CHAR_BIT == 8", "static_assert(sizeof(uint64_t)"} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want %q", result.MainH, want)
		}
	}
}

func TestBoolDeclaration(t *testing.T) {
	result := Compile("flag: Bool = true")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}

	wantC := "#include \"main.h\"\n\nint main(void) {\n#line 1 \"main.hex\"\n    const bool hex_v_flag = true;\n    return EXIT_SUCCESS;\n}\n"
	if result.MainC != wantC {
		t.Fatalf("main.c = %q, want %q", result.MainC, wantC)
	}
}

func TestByteAndByteTypeBehavior(t *testing.T) {
	// RFC 0044: Byte is the canonical transparent alias of UInt8 and byte
	// literals b'...' are ordinary UInt8 constants.
	result := Compile("letter: UInt8 = b'A'")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile returned %#v, want byte literal accepted", result)
	}
	if !strings.Contains(result.MainC, "65") {
		t.Fatalf("generated C lacks the byte value: %s", result.MainC)
	}
	result = Compile("letter: Byte = 65 again: UInt8 = letter")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile returned %#v, want Byte alias accepted", result)
	}
}

func TestAdditionalNumericTypes(t *testing.T) {
	result := Compile("count: Int64 = 9_000_000_000 single: Float32 = 3.14 precise: Float64 = 6.02e23")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"int64_t hex_v_count = INT64_C(9000000000);",
		"float hex_v_single = 0x1.48f5c3p+1f;",
		"double hex_v_precise = 0x1.fde9f10a8d361p+78;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
	for _, want := range []string{
		"static_assert(sizeof(float) == 4",
		"FLT_MANT_DIG == 24",
		"static_assert(sizeof(double) == 8",
		"DBL_MANT_DIG == 53",
	} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want %q", result.MainH, want)
		}
	}
}

func TestNumericLiteralShapeDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"x: Byte = 13", ""},
		{"x: Float32 = 13", "[Type Error] expected Float32 initializer, got Int32 at 1:14"},
		{"x: Int64 = 13.0", "[Type Error] expected Int64 initializer, got Float64 at 1:12"},
	} {
		result := Compile(testCase.source)
		if testCase.want == "" {
			if result.ExitCode != ExitSuccess {
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
	result := Compile("x = 13 y = 3.14")
	if result.ExitCode != ExitFailure || len(result.Stderr) != 2 {
		t.Fatalf("Compile returned %#v, want two unknown-variable diagnostics", result)
	}

	// The assignment forms are rejected before a checked operand can expose a
	// default type. Declarations provide the current contextual entry point.
	result = Compile("whole: Int32 = 13 fraction: Float64 = 3.14")
	if result.ExitCode != ExitSuccess || !strings.Contains(result.MainC, "const int32_t hex_v_whole = 13;") || !strings.Contains(result.MainC, "const double hex_v_fraction = 0x1.91eb851eb851fp+1;") {
		t.Fatalf("Compile returned %#v, want Int32 and Float64 declarations", result)
	}
}

// The generated C keeps the literal's original spelling so a mask written in
// hexadecimal stays readable in the emitted source.
func TestHexLiteralPreservesSpelling(t *testing.T) {
	result := Compile("mask: Int32 = 0xFF")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}

	wantC := "#include \"main.h\"\n\nint main(void) {\n#line 1 \"main.hex\"\n    const int32_t hex_v_mask = 0xFF;\n    return EXIT_SUCCESS;\n}\n"
	if result.MainC != wantC {
		t.Fatalf("main.c = %q, want %q", result.MainC, wantC)
	}
}

func TestRejectsMismatchedInitializer(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"x: Int32 = true", "[Type Error] expected Int32 initializer, got Bool at 1:12"},
		{"flag: Bool = 1", "[Type Error] expected Bool initializer, got Int32 at 1:14"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure {
			t.Fatalf("Compile(%q) exit code = %d, want %d", testCase.source, result.ExitCode, ExitFailure)
		}
		if len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
			t.Fatalf("Compile(%q) std.err = %#v, want [%q]", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestRejectsHexPrefixWithoutDigits(t *testing.T) {
	result := Compile("mask: Int32 = 0x")
	want := "[Syntax Error] malformed hexadecimal literal at 1:15"
	if len(result.Stderr) == 0 || result.Stderr[0] != want {
		t.Fatalf("std.err = %#v, want first entry %q", result.Stderr, want)
	}
}

func TestRejectsOutOfRangeHex(t *testing.T) {
	result := Compile("mask: Int32 = 0x80000000")
	want := "[Type Error] given value is outside the Int32 range at 1:15"
	if len(result.Stderr) != 1 || result.Stderr[0] != want {
		t.Fatalf("std.err = %#v, want [%q]", result.Stderr, want)
	}
}

func TestRejectsMalformedHexadecimalLiteral(t *testing.T) {
	result := Compile("x: Int32 = 0x")
	if result.ExitCode != ExitFailure {
		t.Fatalf("Compile exit code = %d, want %d", result.ExitCode, ExitFailure)
	}
	want := []string{"[Syntax Error] malformed hexadecimal literal at 1:12"}
	if len(result.Stderr) != len(want) || result.Stderr[0] != want[0] {
		t.Fatalf("std.err = %#v, want %#v", result.Stderr, want)
	}
}
