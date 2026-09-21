package integration

import (
	"slices"
	"strings"

	"hexal/compiler"
	"testing"
)

func TestConversionMethods(t *testing.T) {
	result := compileSource("fun demo() do\n    let wide: Int64 = 9_000_000_000\n    let small: Int8 = 12\n    let narrowed: Int8 = wide.to<Int8>()\n    let whole: Int32 = 3.75.to<Int32>()\n    let size: Size = small.to<Size>()\n    let count: UInt32 = size.to<UInt32>()\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static inline int8_t hex_convert_int64_t_int8_t(int64_t value) {",
		"hex_v_narrowed = hex_convert_int64_t_int8_t(hex_v_wide);",
		"const int32_t hex_v_whole = 3;",
		"hex_convert_int8_t_size_t(hex_v_small)",
		"hex_convert_size_t_uint32_t(hex_v_size)",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(numericH(t, result), want) {
			t.Fatalf("generated output = %q %q %q, want %q", rootC(t, result), rootH(t, result), numericH(t, result), want)
		}
	}
}

func TestCheckedConstantConversionDiagnostics(t *testing.T) {
	result := compileSource("let bad: Int8 = (200).to<Int8>()")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "outside the range of Int8") {
		t.Fatalf("Compile stderr = %#v, want checked-conversion range diagnostic", result.Stderr)
	}
}

func TestConversionMatrixRejections(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo() do\n    let flag: Bool = true\n    let bad: Int32 = flag.to<Int32>()\nend", "Bool has no method named to"},
		{"fun demo() do\n    let value: Int32 = 1\n    let bad: Bool = value.to<Bool>()\nend", "supported scalar source and destination"},
		{"fun demo() do\n    let value: Int32 = 1\n    let pointer: Ptr<Int32> = @value\n    let bad: UInt64 = pointer.to<UInt64>()\nend", "Ptr<Int32> has no method named to"},
		{"fun demo() do\n    let value: Int32 = 1\n    let bad: Int32 = value.to()\nend", "to requires exactly 1 explicit type argument"},
		{"fun demo() do\n    let value: Int32 = 1\n    let bad: Int32 = value.to(1)\nend", "to requires exactly 1 explicit type argument"},
		{"fun demo() do\n    let value: Int32 = 1\n    let bad: Int32 = value.to<Int32>(1)\nend", "to accepts no value arguments"},
		{"fun demo() do\n    let value: Int32 = 1\n    let bad: Int32 = value.to_int32()\nend", "Int32 has no method named to_int32"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestConversionGenericSpecialization(t *testing.T) {
	result := compileSource("fun convert<Source, Destination>(value: Source): Destination do\n    return value.to<Destination>()\nend\nfun demo() do\n    let good: Int32 = convert<Int64, Int32>(10)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	result = compileSource("fun convert<Source, Destination>(value: Source): Destination do\n    return value.to<Destination>()\nend\nfun demo() do\n    let bad: Bool = convert<Int32, Bool>(10)\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("Compile stderr = %#v, want specialization rejection", result.Stderr)
	}
}

func TestConversionAliasCanonicalizes(t *testing.T) {
	result := compileSource("type Count is Int32\nfun demo() do\n    let value: Int64 = 5\n    let count: Count = value.to<Count>()\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

// Size is fully C-target-driven, so its implicit conversions must hold on
// every conforming target. Size has no implicit conversion or binary common
// type with any distinct numeric type; identity and literal contextual
// typing remain, and the explicit to<Size>()/to<T>() conversions are the
// portable routes.
func TestSizeHasNoImplicitNumericMixing(t *testing.T) {
	rejected := []string{
		"let raw: UInt64 = 42\nlet count: Size = raw\n",
		"let count: Size = 1\nlet raw: UInt64 = count\n",
		"let count: Size = 1\nlet raw: UInt64 = 2\nlet total: Size = count + raw\n",
		"let count: Size = 1\nlet offset: Int32 = 2\nlet total: Size = count + offset\n",
		"let count: Size = 1\nlet wide: Float64 = count\n",
		"let count: Size = 1\nlet size: Float32 = count\n",
	}
	for _, source := range rejected {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure {
			t.Fatalf("want reject; got exit=%d stderr=%v\nsource: %s", result.ExitCode, result.Stderr, source)
		}
	}
	accepted := []string{
		"let count: Size = 1\nlet other: Size = count\n",
		"let count: Size = 1\nlet total: Size = count + 2\n",
		"let raw: UInt64 = 42\nlet count: Size = raw.to<Size>()\n",
		"let count: Size = 1\nlet raw: UInt64 = count.to<UInt64>()\n",
		"let count: Size = 1\nlet small: UInt8 = count.to<UInt8>()\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got exit=%d stderr=%v\nsource: %s", result.ExitCode, result.Stderr, source)
		}
	}
	bad := "let a: Array<Size, 2> = [1, 2]\nlet b: Array<UInt64, 2> = a\n"
	if result := compileSource(bad); result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want Array<Size> and Array<UInt64> distinct; got accept")
	}
}

// A typed value converts implicitly to exactly its permitted widening
// targets in binding-initializer position, and to nothing else. Size never
// widens.
func TestLosslessWideningPairTable(t *testing.T) {
	widening := map[string][]string{
		"Int8":    {"Int16", "Int32", "Int64", "Float32", "Float64"},
		"Int16":   {"Int32", "Int64", "Float32", "Float64"},
		"Int32":   {"Int64", "Float64"},
		"UInt8":   {"UInt16", "UInt32", "UInt64", "Int16", "Int32", "Int64", "Float32", "Float64"},
		"Byte":    {"UInt16", "UInt32", "UInt64", "Int16", "Int32", "Int64", "Float32", "Float64"},
		"UInt16":  {"UInt32", "UInt64", "Int32", "Int64", "Float32", "Float64"},
		"UInt32":  {"UInt64", "Int64", "Float64"},
		"Float32": {"Float64"},
	}
	scalars := []string{"Int8", "Int16", "Int32", "Int64", "UInt8", "UInt16", "UInt32", "UInt64", "Float32", "Float64"}
	literal := map[string]string{"Float32": "1.5", "Float64": "1.5"}
	for source, targets := range widening {
		for _, target := range targets {
			t.Run(source+"_to_"+target, func(t *testing.T) {
				value := "1"
				if source == "Float32" || source == "Float64" {
					value = literal[source]
				}
				assertCompiles(t, "let value: "+source+" = "+value+"\nlet dest: "+target+" = value\n")
			})
		}
	}
	// Identity is excluded from the table; every other pair with a fixed
	// source type is a rejection. Byte is a transparent UInt8 alias, so its
	// rows collapse into the UInt8 rows.
	for _, source := range scalars {
		for _, target := range scalars {
			if source == target || slices.Contains(widening[source], target) {
				continue
			}
			t.Run("reject_"+source+"_to_"+target, func(t *testing.T) {
				value := "1"
				if source == "Float32" || source == "Float64" {
					value = literal[source]
				}
				assertRejects(t, "let value: "+source+" = "+value+"\nlet dest: "+target+" = value\n", "expected "+target+" initializer; got "+source)
			})
		}
	}
	// Int64, UInt64, and Float64 widen nowhere.
	for _, source := range []string{"Int64", "UInt64", "Float64"} {
		for _, target := range scalars {
			if source == target {
				continue
			}
			t.Run("reject_"+source+"_to_"+target, func(t *testing.T) {
				value := "1"
				if source == "Float32" || source == "Float64" {
					value = literal[source]
				}
				assertRejects(t, "let value: "+source+" = "+value+"\nlet dest: "+target+" = value\n", "expected "+target+" initializer; got "+source)
			})
		}
	}
}

// A negated literal may not target an unsigned type, even at zero.
func TestNegatedLiteralRequiresSignedDestination(t *testing.T) {
	for _, target := range []string{"UInt8", "UInt16", "UInt32", "UInt64"} {
		t.Run(target, func(t *testing.T) {
			assertRejects(t, "let value: "+target+" = -0\n", "negated integer literal requires a signed destination")
		})
	}
}

func TestDirectConversionLowering(t *testing.T) {
	// Two uses of one safe pair emit two casts and no helper.
	result := assertCompiles(t, "fun demo() do\n    let value: UInt8 = 12\n    let a: Float64 = value.to<Float64>()\n    let b: Float64 = value.to<Float64>()\nend")
	bodyC, bodyH := rootC(t, result), rootH(t, result)
	if strings.Count(bodyC, "(double)hex_v_value") != 2 {
		t.Fatalf("modules/app.c = %q, want two inline casts", bodyC)
	}
	if strings.Contains(bodyH, "hex_convert_uint8_t_double") {
		t.Fatalf("modules/app.h = %q, safe pair must not emit a helper", bodyH)
	}
	// Widening integer, unsigned to float, and float widening all cast
	// inline.
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"let value: Int16 = 3\nlet wide: Int64 = value.to<Int64>()", "(int64_t)hex_v_value"},
		{"let value: UInt64 = 3\nlet wide: Float32 = value.to<Float32>()", "(float)hex_v_value"},
		{"let value: Float32 = 1.5\nlet wide: Float64 = value.to<Float64>()", "(double)hex_v_value"},
	} {
		source := "fun demo() do\n    " + testCase.source + "\nend"
		result := assertCompiles(t, source)
		if !strings.Contains(rootC(t, result), testCase.want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), testCase.want)
		}
	}
	// Identity emits neither helper nor cast.
	result = assertCompiles(t, "fun demo() do\n    let value: Int32 = 3\n    let same: Int32 = value.to<Int32>()\nend")
	bodyC, bodyH = rootC(t, result), rootH(t, result)
	if !strings.Contains(bodyC, "const int32_t hex_v_same = hex_v_value;") {
		t.Fatalf("modules/app.c = %q, want the identity operand read", bodyC)
	}
	if strings.Contains(bodyH, "hex_convert_int32_t_int32_t") || strings.Contains(bodyC, "(int32_t)hex_v_value") {
		t.Fatalf("identity conversion = C:%q H:%q, want no cast and no helper", bodyC, bodyH)
	}
	// A non-atomic operand expression appears exactly once inside the
	// direct cast.
	result = assertCompiles(t, "fun demo(left: Float32, right: Float32) do\n    let total: Float64 = (left + right).to<Float64>()\nend")
	bodyC = rootC(t, result)
	initializer := ""
	for _, line := range strings.Split(bodyC, "\n") {
		if strings.Contains(line, "hex_v_total") {
			initializer = line
			break
		}
	}
	if strings.Count(initializer, "hex_v_left") != 1 || strings.Count(initializer, "hex_v_right") != 1 || !strings.Contains(initializer, "(double)((hex_v_left + hex_v_right))") {
		t.Fatalf("modules/app.c = %q, want the operand rendered exactly once inside the cast", bodyC)
	}
}

func TestCheckedConversionLowering(t *testing.T) {
	// Int64 to Int8 keeps one range-checking helper for repeated uses.
	result := assertCompiles(t, "fun demo() do\n    let wide: Int64 = 9_000_000_000\n    let a: Int8 = wide.to<Int8>()\n    let b: Int8 = wide.to<Int8>()\nend")
	if strings.Count(numericH(t, result), "static inline int8_t hex_convert_int64_t_int8_t") != 1 {
		t.Fatalf("hexal/numeric.h = %q, want one deduplicated helper", numericH(t, result))
	}
	// Signed to unsigned retains lower and upper checks.
	result = assertCompiles(t, "fun demo() do\n    let value: Int32 = -3\n    let code: UInt32 = value.to<UInt32>()\nend")
	headerText := numericH(t, result)
	for _, want := range []string{"static inline uint32_t hex_convert_int32_t_uint32_t(int32_t value) {", "if (!(value >= 0 && value <= UINT32_MAX)) {"} {
		if !strings.Contains(headerText, want) {
			t.Fatalf("hexal/numeric.h = %q, want %q", headerText, want)
		}
	}
	// Float64 to Float32 retains the finite-overflow validation.
	result = assertCompiles(t, "fun demo() do\n    let value: Float64 = 1.5\n    let small: Float32 = value.to<Float32>()\nend")
	headerText = numericH(t, result)
	for _, want := range []string{"static inline float hex_convert_double_float(double value) {", "if (isfinite(value) && isinf(result)) {"} {
		if !strings.Contains(headerText, want) {
			t.Fatalf("hexal/numeric.h = %q, want %q", headerText, want)
		}
	}
	// Mixed safe and checked emit helpers only for the checked pair.
	result = assertCompiles(t, "fun demo() do\n    let value: UInt8 = 12\n    let wide: Float64 = value.to<Float64>()\n    let big: Int64 = 9_000_000_000\n    let narrow: Int8 = big.to<Int8>()\nend")
	headerText = numericH(t, result)
	if !strings.Contains(headerText, "hex_convert_int64_t_int8_t") || strings.Contains(headerText, "hex_convert_uint8_t_double") {
		t.Fatalf("modules/app.h = %q, want only the checked helper", headerText)
	}
}

// Float-to-integer checks use one truncation, exact power-of-two bounds,
// and cast the truncated temporary.
func TestFloatToIntegerHelperBounds(t *testing.T) {
	result := assertCompiles(t, "fun demo() do\n    let value: Float64 = 3.75\n    let signed: Int64 = value.to<Int64>()\n    let code: UInt64 = value.to<UInt64>()\n    let count: Size = value.to<Size>()\nend")
	headerText := numericH(t, result)
	for _, want := range []string{
		"static inline int64_t hex_convert_double_int64_t(double value) {",
		"double truncated = trunc(value);",
		"if (!(truncated >= -0x1p63 && truncated < 0x1p63)) {",
		"return (int64_t)truncated;",
		"if (!(truncated >= 0.0 && truncated < 0x1p64)) {",
		"return (uint64_t)truncated;",
		"if (!(truncated >= 0.0 && truncated < (double)SIZE_MAX + 1.0)) {",
		"return (size_t)truncated;",
	} {
		if !strings.Contains(headerText, want) {
			t.Fatalf("hexal/numeric.h = %q, want %q", headerText, want)
		}
	}
	for _, forbidden := range []string{"INT64_MAX", "UINT64_MAX", "truncated <= SIZE_MAX", "fromfp", "ufromfp", "errno", "truncated >= -9223372036854775808.0"} {
		if strings.Contains(headerText, forbidden) {
			t.Fatalf("hexal/numeric.h = %q, must not contain %q", headerText, forbidden)
		}
	}
	// Float32 sources truncate with truncf and check exact bounds.
	result = assertCompiles(t, "fun demo() do\n    let value: Float32 = 3.75\n    let whole: Int16 = value.to<Int16>()\n    let count: Size = value.to<Size>()\nend")
	headerText = numericH(t, result)
	for _, want := range []string{
		"float truncated = truncf(value);",
		"if (!(truncated >= -0x1p15 && truncated < 0x1p15)) {",
		"if (!(truncated >= 0.0 && truncated < (float)SIZE_MAX + 1.0f)) {",
	} {
		if !strings.Contains(headerText, want) {
			t.Fatalf("hexal/numeric.h = %q, want %q", headerText, want)
		}
	}
}

func TestConversionTrapSelection(t *testing.T) {
	// A safe-conversion-only program gets no trap and no trap-owned headers.
	result := assertCompiles(t, "fun demo() do\n    let value: UInt8 = 12\n    let wide: Float64 = value.to<Float64>()\nend")
	header := hexalH(t, result)
	for _, forbidden := range []string{"hex_runtime_trap", "<stdio.h>", "<stdlib.h>", "<math.h>"} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("hexal.h = %q, safe-only program must not select %q", header, forbidden)
		}
	}
	// A checked integer conversion selects the trap but no <math.h>.
	result = assertCompiles(t, "fun demo() do\n    let value: Int64 = 9_000_000_000\n    let narrow: Int8 = value.to<Int8>()\nend")
	header = hexalH(t, result)
	if !strings.Contains(header, "[[noreturn]] void hex_runtime_trap(const char *message);") {
		t.Fatalf("hexal.h = %q, want the trap declaration", header)
	}
	if strings.Contains(header, "<math.h>") {
		t.Fatalf("hexal.h = %q, integer-source helpers must not select <math.h>", header)
	}
	// A checked float-to-integer conversion selects the trap and <math.h>;
	// stdio/stdlib come only from the trap definition.
	result = assertCompiles(t, "fun demo() do\n    let value: Float64 = 3.75\n    let whole: Int32 = value.to<Int32>()\nend")
	header = hexalH(t, result)
	for _, want := range []string{"[[noreturn]] void hex_runtime_trap(const char *message);", "#include <math.h>", "#include <stdio.h>", "#include <stdlib.h>"} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal.h = %q, want %q", header, want)
		}
	}
}
