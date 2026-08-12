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
		"static inline int8_t sw_convert_int64_t_int8_t(int64_t value) {",
		"sw_v_narrowed = sw_convert_int64_t_int8_t(sw_v_wide);",
		"const int32_t sw_v_whole = 3;",
		"sw_convert_int8_t_size_t(sw_v_small)",
		"sw_convert_size_t_uint32_t(sw_v_size)",
		"static inline uint32_t sw_convert_int64_t_rune(int64_t value) {",
		"sw_v_letter = sw_convert_int64_t_rune(sw_v_wide);",
		"sw_convert_rune_uint32_t(sw_v_letter)",
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
