package compiler

import (
	"strings"
	"testing"
)

// RFC 0038: the one explicit scalar conversion spelling `source.to<Dest>()`.

func TestConversionMethods(t *testing.T) {
	result := Compile("fun demo()\n    wide: Int64 = 9_000_000_000\n    small: Int8 = 12\n    narrowed: Int8 = wide.to<Int8>()\n    whole: Int32 = 3.75.to<Int32>()\n    size: Size = small.to<Size>()\n    count: UInt32 = size.to<UInt32>()\n    letter: Rune = wide.to<Rune>()\n    code: UInt32 = letter.to<UInt32>()\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static inline int8_t hex_convert_int64_t_int8_t(int64_t value) {",
		"hex_v_narrowed = hex_convert_int64_t_int8_t(hex_v_wide);",
		"const int32_t hex_v_whole = 3;",
		"hex_convert_int8_t_size_t(hex_v_small)",
		"hex_convert_size_t_uint32_t(hex_v_size)",
		"static inline uint32_t hex_convert_int64_t_rune(int64_t value) {",
		"hex_v_letter = hex_convert_int64_t_rune(hex_v_wide);",
		"hex_convert_rune_uint32_t(hex_v_letter)",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestCheckedConstantConversionDiagnostics(t *testing.T) {
	result := Compile("bad: Int8 = (200).to<Int8>()")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "outside the range of Int8") {
		t.Fatalf("Compile stderr = %#v, want checked-conversion range diagnostic", result.Stderr)
	}
}

func TestRuneScalarValidity(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"good: Rune = (0x1F600).to<Rune>()", ""},
		{"surrogate: Rune = (0xD800).to<Rune>()", "not a valid Unicode scalar value"},
		{"high: Rune = (0x110000).to<Rune>()", "not a valid Unicode scalar value"},
		{"negative: Rune = (-1).to<Rune>()", "not a valid Unicode scalar value"},
	} {
		result := Compile(testCase.source)
		if testCase.want == "" {
			if result.ExitCode != ExitSuccess {
				t.Fatalf("Compile(%q) stderr = %#v, want 0", testCase.source, result.Stderr)
			}
			continue
		}
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestConversionMatrixRejections(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo()\n    flag: Bool = true\n    bad: Int32 = flag.to<Int32>()\nend", "Bool has no method named to"},
		{"fun demo()\n    letter: Rune = (65).to<Rune>()\n    bad: Rune = letter.to<Rune>()\nend", "supported scalar source and destination"},
		{"fun demo()\n    whole: Float64 = 1.5\n    bad: Rune = whole.to<Rune>()\nend", "supported scalar source and destination"},
		{"fun demo()\n    value: Int32 = 1\n    bad: Bool = value.to<Bool>()\nend", "supported scalar source and destination"},
		{"fun demo()\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: UInt64 = pointer.to<UInt64>()\nend", "Ptr<Int32> has no method named to"},
		{"fun demo()\n    value: Int32 = 1\n    bad: Int32 = value.to()\nend", "to requires exactly 1 explicit type argument"},
		{"fun demo()\n    value: Int32 = 1\n    bad: Int32 = value.to(1)\nend", "to requires exactly 1 explicit type argument"},
		{"fun demo()\n    value: Int32 = 1\n    bad: Int32 = value.to<Int32>(1)\nend", "to accepts no value arguments"},
		{"fun demo()\n    value: Int32 = 1\n    bad: Int32 = value.to_int32()\nend", "Int32 has no method named to_int32"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestConversionGenericSpecialization(t *testing.T) {
	result := Compile("fun convert<Source, Destination>(value: Source): Destination\n    return value.to<Destination>()\nend\nfun demo()\n    good: Int32 = convert<Int64, Int32>(10)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	result = Compile("fun convert<Source, Destination>(value: Source): Destination\n    return value.to<Destination>()\nend\nfun demo()\n    bad: Bool = convert<Int32, Bool>(10)\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("Compile stderr = %#v, want specialization rejection", result.Stderr)
	}
}

func TestConversionAliasCanonicalizes(t *testing.T) {
	result := Compile("type Count = Int32\nfun demo()\n    value: Int64 = 5\n    count: Count = value.to<Count>()\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}

// RFC 0049 item 6: Size is fully C-target-driven, so implicit conversions
// must hold on every conforming target. Size has no implicit conversion or
// binary common type with any distinct numeric type; identity and literal
// contextual typing remain, and the explicit to<Size>()/to<T>() conversions
// are the portable routes.
func TestSizeHasNoImplicitNumericMixing(t *testing.T) {
	rejected := []string{
		"raw: UInt64 = 42\ncount: Size = raw\n",
		"count: Size = 1\nraw: UInt64 = count\n",
		"count: Size = 1\nraw: UInt64 = 2\ntotal: Size = count + raw\n",
		"count: Size = 1\noffset: Int32 = 2\ntotal: Size = count + offset\n",
		"count: Size = 1\nwide: Float64 = count\n",
		"count: Size = 1\nsize: Float32 = count\n",
	}
	for _, source := range rejected {
		if result := Compile(source); result.ExitCode != ExitFailure {
			t.Fatalf("want reject, got exit=%d stderr=%v\nsource: %s", result.ExitCode, result.Stderr, source)
		}
	}
	accepted := []string{
		"count: Size = 1\nother: Size = count\n",
		"count: Size = 1\ntotal: Size = count + 2\n",
		"raw: UInt64 = 42\ncount: Size = raw.to<Size>()\n",
		"count: Size = 1\nraw: UInt64 = count.to<UInt64>()\n",
		"count: Size = 1\nsmall: UInt8 = count.to<UInt8>()\n",
	}
	for _, source := range accepted {
		if result := Compile(source); result.ExitCode != ExitSuccess {
			t.Fatalf("want accept, got exit=%d stderr=%v\nsource: %s", result.ExitCode, result.Stderr, source)
		}
	}
	bad := "a: Array<Size, 2> = [1, 2]\nb: Array<UInt64, 2> = a\n"
	if result := Compile(bad); result.ExitCode != ExitFailure {
		t.Fatalf("want Array<Size> and Array<UInt64> distinct, got accept")
	}
}

// The full implicit lossless widening table (RFC 0016, preserved by RFC
// 0049): a typed value converts implicitly to exactly its permitted targets
// in binding-initializer position, and to nothing else. Byte is a
// transparent UInt8 alias; Rune and Size widen never.
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
	scalars := []string{"Int8", "Int16", "Int32", "Int64", "UInt8", "UInt16", "UInt32", "UInt64", "Float32", "Float64", "Rune"}
	literal := map[string]string{"Float32": "1.5", "Float64": "1.5"}
	for source, targets := range widening {
		for _, target := range targets {
			t.Run(source+"_to_"+target, func(t *testing.T) {
				value := "1"
				if source == "Float32" || source == "Float64" {
					value = literal[source]
				}
				assertCompiles(t, "value: "+source+" = "+value+"\ndest: "+target+" = value\n")
			})
		}
	}
	// Identity is excluded from the table; every other pair with a fixed
	// source type is a rejection. Byte is a transparent UInt8 alias, so its
	// rows collapse into the UInt8 rows.
	for _, source := range scalars {
		for _, target := range scalars {
			if source == target || slicesContains(widening[source], target) {
				continue
			}
			t.Run("reject_"+source+"_to_"+target, func(t *testing.T) {
				value := "1"
				if source == "Float32" || source == "Float64" {
					value = literal[source]
				}
				assertRejects(t, "value: "+source+" = "+value+"\ndest: "+target+" = value\n", "expected "+target+" initializer, got "+source)
			})
		}
	}
	// Int64, UInt64, Float64, and Rune widen nowhere.
	for _, source := range []string{"Int64", "UInt64", "Float64", "Rune"} {
		for _, target := range scalars {
			if source == target {
				continue
			}
			t.Run("reject_"+source+"_to_"+target, func(t *testing.T) {
				value := "1"
				if source == "Float32" || source == "Float64" {
					value = literal[source]
				}
				assertRejects(t, "value: "+source+" = "+value+"\ndest: "+target+" = value\n", "expected "+target+" initializer, got "+source)
			})
		}
	}
}

func slicesContains(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

// A negated literal may not target an unsigned type, even at zero.
func TestNegatedLiteralRequiresSignedDestination(t *testing.T) {
	for _, target := range []string{"UInt8", "UInt16", "UInt32", "UInt64"} {
		t.Run(target, func(t *testing.T) {
			assertRejects(t, "value: "+target+" = -0\n", "negated integer literal requires a signed destination")
		})
	}
}
