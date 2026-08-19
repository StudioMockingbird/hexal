package integration

import (
	"fmt"
	"strings"
	"testing"
)

// RFC 0072: a maximal unsigned +, -, * tree evaluates in one uintmax_t domain
// and narrows once at its result boundary. These tests assert the generated C
// for the shapes where the tree ends and a boundary begins.

// The motivating packet-header expression: one seed, one narrowing, and the
// explicit conversions preserved — not one widening/narrowing pair per
// addition.
func TestUnsignedRingPacketHeaderLowersToOneSeedAndOneNarrowing(t *testing.T) {
	source := "fun header(version: UInt8, low: UInt8, high: UInt8, mode: UInt8, port: UInt8): UInt32 do\n" +
		"    total: UInt32 := version.to<UInt32>() + low.to<UInt32>() + high.to<UInt32>() + mode.to<UInt32>() + port.to<UInt32>()\n" +
		"    return total\nend\n" +
		"value: UInt32 := header(1, 2, 3, 4, 5)\n"
	generated := rootC(t, assertCompiles(t, source))
	want := "const uint32_t hex_v_total = (uint32_t)((uintmax_t)(uint32_t)hex_v_version + (uint32_t)hex_v_low + " +
		"(uint32_t)hex_v_high + (uint32_t)hex_v_mode + (uint32_t)hex_v_port);"
	if !strings.Contains(generated, want) {
		t.Fatalf("modules/app.c = %q, want %q", generated, want)
	}
	if count := strings.Count(generated, "uintmax_t"); count != 1 {
		t.Fatalf("modules/app.c has %d uintmax_t seeds, want exactly 1: %q", count, generated)
	}
}

// A composite boundary can lose against its ring parent's precedence, so it
// keeps its grouping. Division, remainder, shift, and bitwise operands are
// boundaries by definition: each completes at the Hexal type before the tree
// lifts it, and a ring tree consumed by one narrows first.
func TestUnsignedRingBoundariesKeepTheirTypedValue(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{
			name:   "equal-precedence composite boundary",
			source: "fun demo(a: UInt32, b: UInt32, c: UInt32): UInt32 do\n    return a * (b / c)\nend\n",
			want:   "return (uint32_t)((uintmax_t)hex_v_a * (hex_div_uint32_t(hex_v_b, hex_v_c)));",
		},
		{
			name:   "lower-precedence composite boundary",
			source: "fun demo(a: UInt32, b: UInt32, c: UInt32): UInt32 do\n    return a + (b << c)\nend\n",
			want:   "return (uint32_t)((uintmax_t)hex_v_a + (hex_shl_uint32_t(hex_v_b, (uint64_t)(hex_v_c))));",
		},
		{
			name:   "remainder boundary",
			source: "fun demo(a: UInt32, b: UInt32, c: UInt32): UInt32 do\n    return a - (b % c)\nend\n",
			want:   "return (uint32_t)((uintmax_t)hex_v_a - (hex_rem_uint32_t(hex_v_b, hex_v_c)));",
		},
		{
			name:   "bitwise boundary",
			source: "fun demo(a: UInt32, b: UInt32, c: UInt32): UInt32 do\n    return a + (b & c)\nend\n",
			want:   "return (uint32_t)((uintmax_t)hex_v_a + ((uint32_t)((uint32_t)hex_v_b & (uint32_t)hex_v_c)));",
		},
		{
			name:   "tree narrows before division consumes it",
			source: "fun demo(a: UInt32, b: UInt32, c: UInt32): UInt32 do\n    return (a + b) / c\nend\n",
			want:   "return hex_div_uint32_t((uint32_t)((uintmax_t)hex_v_a + hex_v_b), hex_v_c);",
		},
		{
			name:   "tree narrows before a comparison consumes it",
			source: "fun demo(a: UInt32, b: UInt32, c: UInt32): Bool do\n    return a + b > c\nend\n",
			want:   "return ((uint32_t)((uintmax_t)hex_v_a + hex_v_b) > hex_v_c);",
		},
		{
			name:   "tree narrows before a shift consumes it",
			source: "fun demo(a: UInt32, b: UInt32, c: UInt32): UInt32 do\n    return (a + b) << c\nend\n",
			want:   "return hex_shl_uint32_t((uint32_t)((uintmax_t)hex_v_a + hex_v_b), (uint64_t)(hex_v_c));",
		},
		{
			name:   "tree narrows before a conversion consumes it",
			source: "fun demo(a: UInt32, b: UInt32): UInt64 do\n    return (a + b).to<UInt64>()\nend\n",
			want:   "return (uint64_t)((uint32_t)((uintmax_t)hex_v_a + hex_v_b));",
		},
		{
			name:   "a boundary's own ring subtree starts a new tree",
			source: "fun demo(a: UInt32, b: UInt32, c: UInt32, d: UInt32): UInt32 do\n    return a + ((b + c) / d)\nend\n",
			want:   "return (uint32_t)((uintmax_t)hex_v_a + (hex_div_uint32_t((uint32_t)((uintmax_t)hex_v_b + hex_v_c), hex_v_d)));",
		},
		{
			name:   "an explicit wider-to-narrower conversion stays inside the tree",
			source: "fun demo(value: UInt64, other: UInt32): UInt32 do\n    return value.to<UInt32>() + other\nend\n",
			want:   "return (uint32_t)((uintmax_t)hex_convert_uint64_t_uint32_t(hex_v_value) + hex_v_other);",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			generated := rootC(t, assertCompiles(t, testCase.source))
			if !strings.Contains(generated, testCase.want) {
				t.Fatalf("modules/app.c = %q, want %q", generated, testCase.want)
			}
		})
	}
}

// Operands are neither duplicated nor reordered: a call inside a ring tree is
// rendered exactly once.
func TestUnsignedRingRendersEachCallExactlyOnce(t *testing.T) {
	source := "fun one(): UInt32 do\n    return 1\nend\n" +
		"fun demo(a: UInt32): UInt32 do\n    return one() + a + one()\nend\n" +
		"value: UInt32 := demo(2)\n"
	generated := rootC(t, assertCompiles(t, source))
	want := "return (uint32_t)((uintmax_t)hex_f_m3_app_one() + hex_v_a + hex_f_m3_app_one());"
	if !strings.Contains(generated, want) {
		t.Fatalf("modules/app.c = %q, want %q", generated, want)
	}
}

// Every covered type lowers its wrapping shapes to one narrowing at the tree's
// result boundary, whatever the shape does inside. The suite invokes no
// toolchain, so the checkable claim is structural: exactly one narrowing cast
// per maximal tree and at least one uintmax_t seed feeding it — a right-nested
// subtree adds its own seed but no second narrowing — which is the structure
// that makes the runtime result reduce modulo the type width exactly once.
func TestUnsignedRingWidthBoundaries(t *testing.T) {
	for _, typ := range []struct{ hexal, cName string }{
		{"UInt8", "uint8_t"}, {"Byte", "uint8_t"}, {"UInt16", "uint16_t"},
		{"UInt32", "uint32_t"}, {"UInt64", "uint64_t"}, {"Size", "size_t"},
	} {
		for _, shape := range []struct{ name, body string }{
			{"maximum plus one", "return a + b"},
			{"zero minus one", "return b - a"},
			{"overflowing multiplication", "return a * a"},
			{"tree wrapping more than once", "return a * a + a * a"},
			{"nested right-hand subtree", "return a + (b * a)"},
		} {
			t.Run(typ.hexal+"/"+shape.name, func(t *testing.T) {
				source := fmt.Sprintf("fun demo(a: %s, b: %s): %s do\n    %s\nend\n", typ.hexal, typ.hexal, typ.hexal, shape.body)
				generated := rootC(t, assertCompiles(t, source))
				narrowings := strings.Count(generated, "("+typ.cName+")(")
				seeds := strings.Count(generated, "(uintmax_t)")
				if narrowings != 1 {
					t.Fatalf("modules/app.c = %q, want exactly one narrowing to %s; got %d", generated, typ.cName, narrowings)
				}
				if seeds < 1 {
					t.Fatalf("modules/app.c = %q, want the tree seeded into the uintmax_t domain", generated)
				}
			})
		}
	}
}
