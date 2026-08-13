package compiler

// Unary and binary operators: lowering, folding, precedence, wrapping. Spec 0009.

import (
	"fmt"
	"strings"
	"testing"
)

const completeOperatorsSource = "mut left: Int32 = 7 mut right: Int32 = 3 " +
	"sum: Int32 = left + right difference: Int32 = left - right " +
	"product: Int32 = left * right quotient: Int32 = left / right " +
	"remainder: Int32 = left % right negative: Int32 = -left " +
	"equal: Bool = left == right notEqual: Bool = left != right " +
	"less: Bool = left < right lessEqual: Bool = left <= right " +
	"greater: Bool = left > right greaterEqual: Bool = left >= right " +
	"mut ready: Bool = true mut loaded: Bool = false " +
	"notReady: Bool = !ready both: Bool = ready and loaded " +
	"either: Bool = ready or loaded boolEqual: Bool = ready == loaded " +
	"boolNotEqual: Bool = ready != loaded " +
	"mut f32Left: Float32 = 1.5 mut f32Right: Float32 = 2.0 " +
	"f32Sum: Float32 = f32Left + f32Right " +
	"f32Difference: Float32 = f32Left - f32Right " +
	"f32Product: Float32 = f32Left * f32Right " +
	"f32Quotient: Float32 = f32Left / f32Right " +
	"f32Less: Bool = f32Left < f32Right " +
	"mut first: Float64 = 2.0 mut second: Float64 = 3.0 mut third: Float64 = 4.0 " +
	"f64Sum: Float64 = first + second f64Difference: Float64 = first - second " +
	"f64Product: Float64 = first * second f64Quotient: Float64 = first / second " +
	"f64Less: Bool = first < second f64Equal: Bool = first == second " +
	"precedence: Float64 = first + second * third " +
	"grouped: Float64 = (first + second) * third " +
	"mut unsignedLeft: UInt32 = 7 mut unsignedRight: UInt32 = 3 " +
	"unsignedSum: UInt32 = unsignedLeft + unsignedRight " +
	"unsignedDifference: UInt32 = unsignedLeft - unsignedRight " +
	"unsignedProduct: UInt32 = unsignedLeft * unsignedRight " +
	"unsignedQuotient: UInt32 = unsignedLeft / unsignedRight " +
	"unsignedRemainder: UInt32 = unsignedLeft % unsignedRight"

func TestCompleteOperatorProgram(t *testing.T) {
	result := Compile(completeOperatorsSource)
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful operator program", result)
	}

	for _, want := range []string{
		"((uint64_t)(uint32_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) <= (uint64_t)INT32_MAX ? (int32_t)(uint32_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) : INT32_MIN + (int32_t)((uint64_t)(uint32_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) - (uint64_t)INT32_MAX - (uint64_t)1))",
		"hex_div_int32_t(hex_v_left, hex_v_right)",
		"hex_rem_int32_t(hex_v_left, hex_v_right)",
		"((uint64_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_left) <= (uint64_t)INT32_MAX ? (int32_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_left) : INT32_MIN + (int32_t)((uint64_t)(uint32_t)((uint64_t)0 - (uint64_t)hex_v_left) - (uint64_t)INT32_MAX - (uint64_t)1))",
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
		"(uint32_t)((uint64_t)hex_v_unsignedLeft + (uint64_t)hex_v_unsignedRight)",
		"hex_rem_uint32_t(hex_v_unsignedLeft, hex_v_unsignedRight)",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want fragment %q", result.MainC, want)
		}
	}
	for _, want := range []string{
		"static_assert(sizeof(float) == 4",
		"static_assert(sizeof(double) == 8",
	} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want fragment %q", result.MainH, want)
		}
	}
}

func TestMutableWrappingRemainsRuntimeArithmetic(t *testing.T) {
	result := Compile("mut unsigned: UInt8 = 200 wrappedUnsigned: UInt8 = unsigned + 100 mut signed: Int8 = 127 wrappedSigned: Int8 = signed + 1")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful mutable wrapping program", result)
	}
	for _, want := range []string{
		"(uint8_t)((uint32_t)hex_v_unsigned + (uint32_t)100)",
		"((uint64_t)(uint8_t)((uint64_t)hex_v_signed + (uint64_t)1) <= (uint64_t)INT8_MAX ? (int8_t)(uint8_t)((uint64_t)hex_v_signed + (uint64_t)1) : INT8_MIN + (int8_t)((uint64_t)(uint8_t)((uint64_t)hex_v_signed + (uint64_t)1) - (uint64_t)INT8_MAX - (uint64_t)1))",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want runtime wrapping fragment %q", result.MainC, want)
		}
	}
}

func TestFoldsImmutableArithmeticAndWrapsOverflow(t *testing.T) {
	result := Compile("count: UInt8 = 200 next: UInt8 = count + 1")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 || !strings.Contains(result.MainC, "const uint8_t hex_v_next = 201;") {
		t.Fatalf("Compile returned %#v, want folded UInt8 value 201", result)
	}

	// RFC 0017: integer overflow wraps at the result type during folding.
	result = Compile("count: UInt8 = 200 over: UInt8 = count + 100")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 || !strings.Contains(result.MainC, "const uint8_t hex_v_over = 44;") {
		t.Fatalf("Compile returned %#v, want folded wrapped value 44", result)
	}

	// RFC 0017: signed minimum divided by -1 folds to the signed minimum.
	result = Compile("minimum: Int8 = -128 quotient: Int8 = minimum / -1 remainder: Int8 = minimum % -1")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 ||
		!strings.Contains(result.MainC, "const int8_t hex_v_quotient = INT8_MIN;") ||
		!strings.Contains(result.MainC, "const int8_t hex_v_remainder = 0;") {
		t.Fatalf("Compile returned %#v, want folded minimum/-1 quotient and remainder", result)
	}
}

func TestPrecedenceChain(t *testing.T) {
	result := Compile("mut first: Float64 = 1.0 mut second: Float64 = 2.0 mut third: Float64 = 3.0 mut limit: Float64 = 8.0 mut expected: Bool = true mut all: Bool = false mut either: Bool = true result: Bool = !(first + second * third < limit == expected and all or either)")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful precedence-chain program", result)
	}
	want := "(!(((((hex_v_first + (hex_v_second * hex_v_third)) < hex_v_limit) == hex_v_expected) && hex_v_all) || hex_v_either))"
	if !strings.Contains(result.MainC, want) {
		t.Fatalf("main.c = %q, want precedence-chain fragment %q", result.MainC, want)
	}
}

func TestOperatorDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"result: Int32 = 1 / 0", "[Type Error] division by zero at 1:19"},
		{"mut total: Int32 = 10 bad: Int32 = total / 0", "[Type Error] division by zero at 1:42"},
		{"mut total: Int32 = 10 bad: Int32 = total % 0", "[Type Error] division by zero at 1:42"},
		{"mut total: Int32 = 10 bad: Int32 = total / (2 - 2)", "[Type Error] division by zero at 1:42"},
		{"value: Float64 = 1.0 bad: Float64 = value % 2.0", "[Type Error] operator % requires integer operands; got Float64 at 1:43"},
		{"left: Bool = true right: Bool = false bad: Bool = left < right", "[Type Error] ordering is unavailable for Bool values at 1:56"},
		{"count: UInt32 = 5 bad: Int32 = -count", "[Type Error] negation requires a signed type; got UInt32 at 1:32"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || result.Stderr[0] != testCase.want {
			t.Errorf("Compile(%q) stderr = %#v, want [%q]", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestShortCircuitReachability(t *testing.T) {
	for _, source := range []string{
		"result: Bool = true or (1 / 0 == 0)",
		"result: Bool = false and (1 / 0 == 0)",
		// RFC 0023: mixed-type logical operands are valid; the unreachable
		// RHS folds away without ever evaluating.
		"result: Bool = true or (1 and 2)",
		"result: Bool = false and (1 or 2)",
	} {
		result := Compile(source)
		if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
			t.Errorf("Compile(%q) = %#v, want successful constant short-circuit", source, result)
		}
	}

	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"mut guard: Bool = true result: Bool = guard or (1 / 0 == 0)", "division by zero"},
		{"mut guard: Bool = false result: Bool = guard and (1 / 0 == 0)", "division by zero"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) != 1 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Errorf("Compile(%q) stderr = %#v, want diagnostic containing %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestAllIntegerWidths(t *testing.T) {
	for _, testCase := range []struct {
		typ  string
		want string
	}{
		{"UInt8", "(uint8_t)((uint32_t)hex_v_left + (uint32_t)hex_v_right)"},
		{"UInt16", "(uint16_t)((uint32_t)hex_v_left + (uint32_t)hex_v_right)"},
		{"UInt32", "(uint32_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right)"},
		{"UInt64", "(uint64_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right)"},
		{"Int8", "((uint64_t)(uint8_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) <= (uint64_t)INT8_MAX ? (int8_t)(uint8_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) : INT8_MIN + (int8_t)((uint64_t)(uint8_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) - (uint64_t)INT8_MAX - (uint64_t)1))"},
		{"Int16", "((uint64_t)(uint16_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) <= (uint64_t)INT16_MAX ? (int16_t)(uint16_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) : INT16_MIN + (int16_t)((uint64_t)(uint16_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) - (uint64_t)INT16_MAX - (uint64_t)1))"},
		{"Int32", "((uint64_t)(uint32_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) <= (uint64_t)INT32_MAX ? (int32_t)(uint32_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) : INT32_MIN + (int32_t)((uint64_t)(uint32_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) - (uint64_t)INT32_MAX - (uint64_t)1))"},
		{"Int64", "((uint64_t)(uint64_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) <= (uint64_t)INT64_MAX ? (int64_t)(uint64_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) : INT64_MIN + (int64_t)((uint64_t)(uint64_t)((uint64_t)hex_v_left + (uint64_t)hex_v_right) - (uint64_t)INT64_MAX - (uint64_t)1))"},
	} {
		t.Run(testCase.typ, func(t *testing.T) {
			source := fmt.Sprintf("mut left: %s = 1 mut right: %s = 2 result: %s = left + right", testCase.typ, testCase.typ, testCase.typ)
			result := Compile(source)
			if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
				t.Fatalf("Compile returned %#v, want successful %s program", result, testCase.typ)
			}
			if !strings.Contains(result.MainC, testCase.want) {
				t.Fatalf("main.c = %q, want %q", result.MainC, testCase.want)
			}
		})
	}
}

func TestSignedWrappingBoundaries(t *testing.T) {
	result := Compile("mut signed8: Int8 = 127 wrapped8: Int8 = signed8 + 1 mut minimum64: Int64 = -9223372036854775808 wrapped64: Int64 = minimum64 - 1 negative64: Int64 = -minimum64")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful signed boundary program", result)
	}
	for _, old := range []string{
		"const int8_t hex_v_wrapped8 = (int8_t)(uint8_t)((uint64_t)hex_v_signed8 + (uint64_t)1);",
		"const int64_t hex_v_wrapped64 = (int64_t)(uint64_t)((uint64_t)hex_v_minimum64 - (uint64_t)INT64_C(1));",
		"const int64_t hex_v_negative64 = (int64_t)(uint64_t)((uint64_t)0 - (uint64_t)hex_v_minimum64);",
	} {
		if strings.Contains(result.MainC, old) {
			t.Fatalf("main.c contains implementation-defined signed conversion %q: %q", old, result.MainC)
		}
	}
}

func TestShortCircuitRuntime(t *testing.T) {
	result := Compile("mut zero: Int32 = 0 mut guardOr: Bool = true resultOr: Bool = guardOr or (zero / zero > 0) mut guardAnd: Bool = false resultAnd: Bool = guardAnd and (zero / zero > 0)")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful short-circuit program", result)
	}
	for _, want := range []string{
		"(hex_v_guardOr || (hex_div_int32_t(hex_v_zero, hex_v_zero) > 0))",
		"(hex_v_guardAnd && (hex_div_int32_t(hex_v_zero, hex_v_zero) > 0))",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want short-circuit fragment %q", result.MainC, want)
		}
	}
}

func TestNaNComparisons(t *testing.T) {
	result := Compile("mut zero32: Float32 = 0.0 nan32Equal: Bool = (zero32 / zero32) == (zero32 / zero32) nan32Different: Bool = (zero32 / zero32) != (zero32 / zero32) nan32Less: Bool = (zero32 / zero32) < (zero32 / zero32) mut zero: Float64 = 0.0 nanEqual: Bool = (zero / zero) == (zero / zero) nanDifferent: Bool = (zero / zero) != (zero / zero)")
	if result.ExitCode != ExitSuccess || len(result.Stderr) != 0 {
		t.Fatalf("Compile returned %#v, want successful NaN comparison program", result)
	}
	for _, want := range []string{
		"((hex_v_zero32 / hex_v_zero32) == (hex_v_zero32 / hex_v_zero32))",
		"((hex_v_zero32 / hex_v_zero32) != (hex_v_zero32 / hex_v_zero32))",
		"((hex_v_zero32 / hex_v_zero32) < (hex_v_zero32 / hex_v_zero32))",
		"((hex_v_zero / hex_v_zero) == (hex_v_zero / hex_v_zero))",
		"((hex_v_zero / hex_v_zero) != (hex_v_zero / hex_v_zero))",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want NaN comparison fragment %q", result.MainC, want)
		}
	}
}

func TestRuneBinaryArithmeticRejected(t *testing.T) {
	rejected := []string{
		"r: Int32 = 'a' + 'b'\n",
		"r: Int32 = 'a' - 'b'\n",
		"r: Int32 = 'a' * 'b'\n",
		"r: Int32 = 'a' / 'b'\n",
		"r: Int32 = 'a' % 'b'\n",
		"r: Int32 = 'a' + 1\n",
		"r: Int32 = 1 + 'a'\n",
		"letter: Rune = 'a'\nr: Int32 = letter + 1\n",
		"letter: Rune = 'a'\nr: Int32 = 1 - letter\n",
	}
	for _, source := range rejected {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "requires numeric operands") {
			t.Fatalf("want numeric-operands diagnostic, got exit=%d stderr=%v\nsource: %s", result.ExitCode, result.Stderr, source)
		}
	}
	accepted := []string{
		"ok: Bool = 'a' < 'b'\n",
		"ok: Bool = 'a' == 'a'\n",
		"code: UInt32 = 'a'.to<UInt32>()\n",
		"code: UInt32 = 'a'.to<UInt32>() + 1\n",
	}
	for _, source := range accepted {
		if result := Compile(source); result.ExitCode != ExitSuccess {
			t.Fatalf("want accept, got %v:\n%s", result.Stderr, source)
		}
	}
}
