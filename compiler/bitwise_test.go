package compiler

import (
	"strings"
	"testing"
)

// RFC 0032: bitwise and shift operators, bit_cast<T>(), and endian byte
// conversion.

func TestBitwiseOperators(t *testing.T) {
	result := Compile("fun demo()\n    masked: UInt32 = 0xFFFF0000 & 0x0000FFFF\n    xor: UInt32 = 0xFF00 ^ 0x0FF0\n    combined: UInt8 = 0xF0 | 0x0F\n    complement: UInt8 = ~0x0F\n    small: UInt8 = 0xF0\n    wide: UInt16 = 0x0F0F\n    widened: UInt16 = small & wide\n    flags: UInt32 = 0xFFFF\n    result: UInt32 = flags & 0x00FF\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const uint32_t sw_v_masked = 0;",
		"const uint32_t sw_v_xor = 61680;",
		"const uint8_t sw_v_combined = 255;",
		"const uint8_t sw_v_complement = 240;",
		"const uint16_t sw_v_widened = 0;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestBitwiseSignedReconstruction(t *testing.T) {
	result := Compile("fun demo()\n    mask: Int8 = ~0\n    low: Int8 = 0x0F\n    signed: Int8 = mask & low\n    negative: Int32 = -1\n    bits: UInt32 = 0x80000000\n    cross: Int32 = negative & 0x7FFFFFFF\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const int8_t sw_v_mask = -1;",
		"const int8_t sw_v_signed = 15;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestShiftOperators(t *testing.T) {
	result := Compile("fun demo()\n    mut value: UInt32 = 1\n    shifted: UInt32 = value << 4\n    right: UInt32 = shifted >> 2\n    mut signed: Int8 = 64\n    wrapped: Int8 = signed << 1\n    mut negative: Int8 = -4\n    halved: Int8 = negative >> 1\n    mixed: UInt16 = 1 << 8\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"sw_shl_uint32_t(sw_v_value, (uint64_t)(4))",
		"sw_shr_uint32_t(sw_v_shifted, (uint64_t)(2))",
		"sw_v_wrapped = sw_shl_int8_t(sw_v_signed, (uint64_t)(1));",
		"sw_v_halved = sw_shr_int8_t(sw_v_negative, (uint64_t)(1));",
		"static inline int8_t sw_shl_int8_t(int8_t left, uint64_t count) {",
		"if (!(count < 8ULL)) {",
		"static inline int8_t sw_shr_int8_t(int8_t left, uint64_t count) {",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestShiftCountValidation(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo()\n    value: UInt32 = 1\n    bad: UInt32 = value << 32\nend", "shift count 32 is outside the valid range for UInt32"},
		{"fun demo()\n    signed_value: Int32 = 1\n    bad: Int32 = signed_value >> -1\nend", "shift count -1 is outside the valid range for Int32"},
		{"fun demo()\n    signed_value: Int32 = 1\n    bad: Int32 = signed_value << -2\nend", "shift count -2 is outside the valid range for Int32"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestBitwiseDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo()\n    value: Float64 = 1.5\n    bad: Float64 = value & value\nend", "operator & requires integer operands"},
		{"fun demo()\n    letter: Rune = (65).to<Rune>()\n    bad: Rune = letter | letter\nend", "operator | requires integer operands"},
		{"fun demo()\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: Ptr<Int32> = pointer << 1\nend", "operator << requires an integer left operand"},
		{"fun demo()\n    value: Int32 = 1\n    flag: Bool = true\n    bad: Int32 = value << flag\nend", "shift count must be an integer"},
		{"fun demo()\n    value: Float64 = 1.5\n    bad: Float64 = ~value\nend", "operator ~ requires an integer operand"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestBitCast(t *testing.T) {
	result := Compile("fun demo()\n    floating: Float64 = 1.0\n    bits: UInt64 = floating.bit_cast<UInt64>()\n    again: Float64 = bits.bit_cast<Float64>()\n    signed: Int32 = -1\n    unsigned: UInt32 = signed.bit_cast<UInt32>()\n    back: Int32 = unsigned.bit_cast<Int32>()\n    narrow: Float32 = 1.5\n    narrow_bits: UInt32 = narrow.bit_cast<UInt32>()\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static inline uint64_t sw_bitcast_double_uint64_t(double value) {",
		"memcpy(&result, &(value), sizeof(result));",
		"sw_v_bits = sw_bitcast_double_uint64_t(sw_v_floating);",
		"sw_v_unsigned = sw_bitcast_int32_t_uint32_t(sw_v_signed);",
		"static inline uint32_t sw_bitcast_float_uint32_t(float value) {",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestBitCastDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo()\n    value: Float64 = 1.5\n    bad: UInt32 = value.bit_cast<UInt32>()\nend", "bit_cast requires equal-width eligible scalar types"},
		{"fun demo()\n    value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: UInt64 = pointer.bit_cast<UInt64>()\nend", "Ptr<Int32> has no method named bit_cast"},
		{"fun demo()\n    value: Float64 = 1.5\n    bad: UInt64 = value.bit_cast()\nend", "bit_cast requires exactly 1 explicit type argument"},
		{"fun demo()\n    value: Float64 = 1.5\n    bad: UInt64 = value.bit_cast<UInt64>(1)\nend", "bit_cast accepts no value arguments"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestEndianByteConversion(t *testing.T) {
	result := Compile("fun demo()\n    value: UInt32 = 0x01020304\n    little: Array<UInt8, 4> = value.to_le_bytes()\n    big: Array<UInt8, 4> = value.to_be_bytes()\n    from_little: UInt32 = UInt32.from_le_bytes(little)\n    from_big: UInt32 = UInt32.from_be_bytes(big)\n    signed: Int16 = -2\n    signed_little: Array<UInt8, 2> = signed.to_le_bytes()\n    signed_back: Int16 = Int16.from_le_bytes(signed_little)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static inline sw_array_UInt8_4 sw_to_le_bytes_uint32_t(uint32_t value) {",
		"result.data[0] = (uint8_t)((uint32_t)value >> 0);",
		"result.data[3] = (uint8_t)((uint32_t)value >> 24);",
		"result.data[0] = (uint8_t)((uint32_t)value >> 24);",
		"sw_v_little = sw_to_le_bytes_uint32_t(sw_v_value);",
		"sw_v_from_little = sw_from_le_bytes_uint32_t(&(sw_v_little));",
		"sw_v_from_big = sw_from_be_bytes_uint32_t(&(sw_v_big));",
		"static inline int16_t sw_from_le_bytes_int16_t(const sw_array_UInt8_2 *bytes) {",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestEndianDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo()\n    count: Size = 1\n    bad: Array<UInt8, 8> = count.to_le_bytes()\nend", "to_le_bytes requires a fixed-width integer receiver; got Size"},
		{"fun demo()\n    count: Size = 1\n    bad: Size = Size.from_le_bytes([1, 2, 3, 4, 5, 6, 7, 8])\nend", "from_le_bytes and from_be_bytes require a fixed-width integer type"},
		{"fun demo()\n    value: UInt32 = 1\n    bad: UInt32 = UInt32.from_le_bytes([1, 2, 3])\nend", "requires exactly 4 elements"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestBitwisePrecedence(t *testing.T) {
	result := Compile("fun demo()\n    red: UInt32 = 1\n    green: UInt32 = 2\n    blue: UInt32 = 3\n    packed: UInt32 = red << 16 | green << 8 | blue\n    mixed: Bool = (1 | 2) == 3 and (1 & 3) == 1\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"const uint32_t sw_v_packed = 66051;",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestNestedGenericClosersStillParse(t *testing.T) {
	result := Compile("type Link<T> = { value: T, mut next: MutPtr<Link<T>> | Nil, } link: Link<Int32> = Link<Int32> { value = 1, next = nil } fun demo()\n    pointer: Ptr<Ptr<Int32>> | Nil = nil\n    inner: Ptr<Int32> | Nil = nil\n    outer: Ptr<Ptr<Int32>> | Nil = nil\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}
