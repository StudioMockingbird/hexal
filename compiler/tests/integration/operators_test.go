package integration

import (
	"fmt"
	"hexal/compiler"
	"strings"
	"testing"
)

const completeOperatorsSource = "let mut left: Int32 = 7 let mut right: Int32 = 3 " +
	"let sum: Int32 = left + right let difference: Int32 = left - right " +
	"let product: Int32 = left * right let quotient: Int32 = left / right " +
	"let remainder: Int32 = left % right let negative: Int32 = -left " +
	"let equal: Bool = left == right let notEqual: Bool = left != right " +
	"let less: Bool = left < right let lessEqual: Bool = left <= right " +
	"let greater: Bool = left > right let greaterEqual: Bool = left >= right " +
	"let mut ready: Bool = true let mut loaded: Bool = false " +
	"let notReady: Bool = !ready let both: Bool = ready and loaded " +
	"let either: Bool = ready or loaded let boolEqual: Bool = ready == loaded " +
	"let boolNotEqual: Bool = ready != loaded " +
	"let mut f32Left: Float32 = 1.5 let mut f32Right: Float32 = 2.0 " +
	"let f32Sum: Float32 = f32Left + f32Right " +
	"let f32Difference: Float32 = f32Left - f32Right " +
	"let f32Product: Float32 = f32Left * f32Right " +
	"let f32Quotient: Float32 = f32Left / f32Right " +
	"let f32Less: Bool = f32Left < f32Right " +
	"let mut first: Float64 = 2.0 let mut second: Float64 = 3.0 let mut third: Float64 = 4.0 " +
	"let f64Sum: Float64 = first + second let f64Difference: Float64 = first - second " +
	"let f64Product: Float64 = first * second let f64Quotient: Float64 = first / second " +
	"let f64Less: Bool = first < second let f64Equal: Bool = first == second " +
	"let precedence: Float64 = first + (second * third) " +
	"let grouped: Float64 = (first + second) * third " +
	"let mut unsignedLeft: UInt32 = 7 let mut unsignedRight: UInt32 = 3 " +
	"let unsignedSum: UInt32 = unsignedLeft + unsignedRight " +
	"let unsignedDifference: UInt32 = unsignedLeft - unsignedRight " +
	"let unsignedProduct: UInt32 = unsignedLeft * unsignedRight " +
	"let unsignedQuotient: UInt32 = unsignedLeft / unsignedRight " +
	"let unsignedRemainder: UInt32 = unsignedLeft % unsignedRight"

func TestCompleteOperatorProgram(t *testing.T) {
	result := compileSource(completeOperatorsSource)
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful operator program", result)
	}

	for _, want := range []string{
		"hex_wrap_add_int32_t(hex_v_left, hex_v_right)",
		"hex_div_int32_t(hex_v_left, hex_v_right)",
		"hex_rem_int32_t(hex_v_left, hex_v_right)",
		"hex_wrap_neg_int32_t(hex_v_left)",
		"(hex_v_left == hex_v_right)",
		"(hex_v_left != hex_v_right)",
		"(hex_v_left < hex_v_right)",
		"(hex_v_left <= hex_v_right)",
		"(hex_v_left > hex_v_right)",
		"(hex_v_left >= hex_v_right)",
		"(!hex_v_ready)",
		"(hex_v_ready && hex_v_loaded)",
		"(hex_v_ready || hex_v_loaded)",
		"(hex_v_ready == hex_v_loaded)",
		"(hex_v_f32Left + hex_v_f32Right)",
		"(hex_v_f32Left < hex_v_f32Right)",
		"(hex_v_first + hex_v_second)",
		"(hex_v_first < hex_v_second)",
		"(hex_v_first == hex_v_second)",
		"(hex_v_first + (hex_v_second * hex_v_third))",
		"((hex_v_first + hex_v_second) * hex_v_third)",
		"(uint32_t)((uintmax_t)hex_v_unsignedLeft + hex_v_unsignedRight)",
		"hex_rem_uint32_t(hex_v_unsignedLeft, hex_v_unsignedRight)",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want fragment %q", rootC(t, result), want)
		}
	}
	for _, forbidden := range []string{"static_assert(sizeof(float)", "static_assert(sizeof(double)"} {
		if strings.Contains(rootH(t, result), forbidden) || strings.Contains(hexalH(t, result), forbidden) {
			t.Fatalf("modules/app.h = %q, removed target probe %q emitted", rootH(t, result), forbidden)
		}
	}
}

func TestMutableWrappingRemainsRuntimeArithmetic(t *testing.T) {
	result := compileSource("let mut unsigned: UInt8 = 200 let wrappedUnsigned: UInt8 = unsigned + 100 let mut signed: Int8 = 127 let wrappedSigned: Int8 = signed + 1")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful mutable wrapping program", result)
	}
	for _, want := range []string{
		"(uint8_t)((uintmax_t)hex_v_unsigned + 100)",
		"hex_wrap_add_int8_t(hex_v_signed, 1)",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want runtime wrapping fragment %q", rootC(t, result), want)
		}
	}
}

func TestImmutableArithmeticStaysRuntimeAndLiteralFoldingSurvives(t *testing.T) {
	result := compileSource("let count: UInt8 = 200 let next: UInt8 = count + 1")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 || !strings.Contains(rootC(t, result), "const uint8_t hex_v_next = (uint8_t)((uintmax_t)hex_v_count + 1);") {
		t.Fatalf("Compile returned %#v, want runtime UInt8 wrapping on the binding read", result)
	}

	// Integer overflow wraps at the result type during folding.
	result = compileSource("let over: UInt8 = 200 + 100")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 || !strings.Contains(rootC(t, result), "const uint8_t hex_v_over = 44;") {
		t.Fatalf("Compile returned %#v, want folded wrapped value 44", result)
	}

	// Signed minimum divided by -1 folds to the signed minimum.
	result = compileSource("let quotient: Int8 = -128 / -1 let remainder: Int8 = -128 % -1")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 ||
		!strings.Contains(rootC(t, result), "const int8_t hex_v_quotient = INT8_MIN;") ||
		!strings.Contains(rootC(t, result), "const int8_t hex_v_remainder = 0;") {
		t.Fatalf("Compile returned %#v, want folded minimum/-1 quotient and remainder", result)
	}
}

func TestPrecedenceChain(t *testing.T) {
	result := compileSource("let mut first: Float64 = 1.0 let mut second: Float64 = 2.0 let mut third: Float64 = 3.0 let mut limit: Float64 = 8.0 let mut expected: Bool = true let mut all: Bool = false let mut either: Bool = true let result: Bool = !(((((first + (second * third)) < limit) == expected) and all) or either)")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful precedence-chain program", result)
	}
	want := "(!(((((hex_v_first + (hex_v_second * hex_v_third)) < hex_v_limit) == hex_v_expected) && hex_v_all) || hex_v_either))"
	if !strings.Contains(rootC(t, result), want) {
		t.Fatalf("modules/app.c = %q, want precedence-chain fragment %q", rootC(t, result), want)
	}
}

func TestOperatorDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"let result: Int32 = 1 / 0", "[Type Error type.division-by-zero] division by zero at app.hex:1:23"},
		{"let mut total: Int32 = 10 let bad: Int32 = total / 0", "[Type Error type.division-by-zero] division by zero at app.hex:1:50"},
		{"let mut total: Int32 = 10 let bad: Int32 = total % 0", "[Type Error type.division-by-zero] division by zero at app.hex:1:50"},
		{"let mut total: Int32 = 10 let bad: Int32 = total / (2 - 2)", "[Type Error type.division-by-zero] division by zero at app.hex:1:50"},
		{"let value: Float64 = 1.0 let bad: Float64 = value % 2.0", "[Type Error type.remainder-requires-integer] operator % requires integer operands; got Float64 at app.hex:1:51"},
		{"let left: Bool = true let right: Bool = false let bad: Bool = left < right", "[Type Error type.ordering-unavailable-for] ordering is unavailable for Bool values at app.hex:1:68"},
		{"let count: UInt32 = 5 let bad: Int32 = -count", "[Type Error type.negation-requires-signed-type] negation requires a signed type; got UInt32 at app.hex:1:40"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
			t.Errorf("Compile(%q) stderr = %#v, want [%q]", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestShortCircuitReachability(t *testing.T) {
	for _, source := range []string{
		"let result: Bool = true or ((1 / 0) == 0)",
		"let result: Bool = false and ((1 / 0) == 0)",
		// Mixed-type logical operands are valid; the unreachable
		// RHS folds away without ever evaluating.
		"let result: Bool = true or (1 and 2)",
		"let result: Bool = false and (1 or 2)",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
			t.Errorf("Compile(%q) = %#v, want successful constant short-circuit", source, result)
		}
	}

	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"let mut guard: Bool = true let result: Bool = guard or ((1 / 0) == 0)", "division by zero"},
		{"let mut guard: Bool = false let result: Bool = guard and ((1 / 0) == 0)", "division by zero"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Errorf("Compile(%q) stderr = %#v, want diagnostic containing %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestAllIntegerWidths(t *testing.T) {
	for _, testCase := range []struct {
		typ  string
		want string
	}{
		{"UInt8", "(uint8_t)((uintmax_t)hex_v_left + hex_v_right)"},
		{"UInt16", "(uint16_t)((uintmax_t)hex_v_left + hex_v_right)"},
		{"UInt32", "(uint32_t)((uintmax_t)hex_v_left + hex_v_right)"},
		{"UInt64", "(uint64_t)((uintmax_t)hex_v_left + hex_v_right)"},
		{"Int8", "hex_wrap_add_int8_t(hex_v_left, hex_v_right)"},
		{"Int16", "hex_wrap_add_int16_t(hex_v_left, hex_v_right)"},
		{"Int32", "hex_wrap_add_int32_t(hex_v_left, hex_v_right)"},
		{"Int64", "hex_wrap_add_int64_t(hex_v_left, hex_v_right)"},
	} {
		t.Run(testCase.typ, func(t *testing.T) {
			source := fmt.Sprintf("let mut left: %s = 1 let mut right: %s = 2 let result: %s = left + right", testCase.typ, testCase.typ, testCase.typ)
			result := compileSource(source)
			if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
				t.Fatalf("Compile returned %#v, want successful %s program", result, testCase.typ)
			}
			if !strings.Contains(rootC(t, result), testCase.want) {
				t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), testCase.want)
			}
		})
	}
}

func TestSignedWrappingBoundaries(t *testing.T) {
	result := compileSource("let mut signed8: Int8 = 127 let wrapped8: Int8 = signed8 + 1 let mut minimum64: Int64 = -9223372036854775808 let wrapped64: Int64 = minimum64 - 1 let negative64: Int64 = -minimum64")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful signed boundary program", result)
	}
	for _, old := range []string{
		"(int8_t)(uint8_t)((uint64_t)hex_v_signed8 + (uint64_t)1)",
		"(int64_t)(uint64_t)((uint64_t)hex_v_minimum64 - (uint64_t)INT64_C(1))",
		"(int64_t)(uint64_t)((uint64_t)0 - (uint64_t)hex_v_minimum64)",
	} {
		if strings.Contains(rootC(t, result), old) {
			t.Fatalf("modules/app.c contains implementation-defined signed conversion %q: %q", old, rootC(t, result))
		}
	}
	for _, want := range []string{
		"hex_wrap_add_int8_t(hex_v_signed8, 1)",
		"hex_wrap_sub_int64_t(hex_v_minimum64, INT64_C(1))",
		"hex_wrap_neg_int64_t(hex_v_minimum64)",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want wrap helper %q", rootC(t, result), want)
		}
	}
	// The selected helpers are owned by hexal/wrap.h, the using module
	// includes the component header, and hexal.h spells no helper.
	wrapH := moduleFile(t, result, "hexal/wrap.h")
	for _, want := range []string{
		"static inline int8_t hex_wrap_add_int8_t(int8_t a, int8_t b) {\n    int8_t r;\n    ckd_add(&r, a, b);\n    return r;\n}",
		"static inline int64_t hex_wrap_sub_int64_t(int64_t a, int64_t b) {\n    int64_t r;\n    ckd_sub(&r, a, b);\n    return r;\n}",
		"static inline int64_t hex_wrap_neg_int64_t(int64_t a) {\n    int64_t r;\n    ckd_sub(&r, 0, a);\n    return r;\n}",
	} {
		if !strings.Contains(wrapH, want) {
			t.Fatalf("hexal/wrap.h = %q, want helper %q", wrapH, want)
		}
	}
	if strings.Contains(hexalH(t, result), "hex_wrap") {
		t.Fatalf("hexal.h = %q, wrapping helpers must leave hexal.h", hexalH(t, result))
	}
	if !strings.Contains(rootH(t, result), "#include \"hexal/wrap.h\"") {
		t.Fatalf("modules/app.h = %q, want the wrap component include", rootH(t, result))
	}
}

func TestShortCircuitRuntime(t *testing.T) {
	result := compileSource("let mut zero: Int32 = 0 let mut guardOr: Bool = true let resultOr: Bool = guardOr or ((zero / zero) > 0) let mut guardAnd: Bool = false let resultAnd: Bool = guardAnd and ((zero / zero) > 0)")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful short-circuit program", result)
	}
	for _, want := range []string{
		"(hex_v_guardOr || (hex_div_int32_t(hex_v_zero, hex_v_zero) > 0))",
		"(hex_v_guardAnd && (hex_div_int32_t(hex_v_zero, hex_v_zero) > 0))",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want short-circuit fragment %q", rootC(t, result), want)
		}
	}
}

func TestNaNComparisons(t *testing.T) {
	result := compileSource("let mut zero32: Float32 = 0.0 let nan32Equal: Bool = (zero32 / zero32) == (zero32 / zero32) let nan32Different: Bool = (zero32 / zero32) != (zero32 / zero32) let nan32Less: Bool = (zero32 / zero32) < (zero32 / zero32) let mut zero: Float64 = 0.0 let nanEqual: Bool = (zero / zero) == (zero / zero) let nanDifferent: Bool = (zero / zero) != (zero / zero)")
	if result.ExitCode != compiler.ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful NaN comparison program", result)
	}
	for _, want := range []string{
		"((hex_v_zero32 / hex_v_zero32) == (hex_v_zero32 / hex_v_zero32))",
		"((hex_v_zero32 / hex_v_zero32) != (hex_v_zero32 / hex_v_zero32))",
		"((hex_v_zero32 / hex_v_zero32) < (hex_v_zero32 / hex_v_zero32))",
		"((hex_v_zero / hex_v_zero) == (hex_v_zero / hex_v_zero))",
		"((hex_v_zero / hex_v_zero) != (hex_v_zero / hex_v_zero))",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want NaN comparison fragment %q", rootC(t, result), want)
		}
	}
}

// Size-only arithmetic renders a uintmax_t intermediate, which requires
// <stdint.h> even though no written type spells an exact-width integer:
// Size selects <stddef.h> alone. The assertion is textual because the suite
// never invokes a toolchain, so an undeclared uintmax_t is otherwise
// invisible to it.
func TestSizeOnlyArithmeticSelectsStdint(t *testing.T) {
	source := "let a: Size = 1\nlet b: Size = 2\nlet c: Size = a + b\nlet d: Size = c * a\n"
	result := assertCompiles(t, source)
	if !strings.Contains(hexalH(t, result), "#include <stdint.h>") {
		t.Fatalf("hexal.h = %q, want <stdint.h> selected for the unsigned arithmetic intermediate", hexalH(t, result))
	}
	if !strings.Contains(rootC(t, result), "(size_t)((uintmax_t)hex_v_a + hex_v_b)") {
		t.Fatalf("modules/app.c = %q, want a uintmax_t intermediate narrowed to size_t", rootC(t, result))
	}
}
