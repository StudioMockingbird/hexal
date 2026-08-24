package integration

import (
	"strings"
	"testing"

	"hexal/compiler"
)

// Generated-C-text tests for left-to-right evaluation order: call arguments,
// method receivers, binary operands, array elements, object literal fields,
// assignment targets, nested compound expressions, and short-circuit
// operands each get a temporary boundary or an explicit ordering check
// exactly where C's own operand order would otherwise be unspecified.

// order returns the index of each substring's first occurrence in body, in
// the order the substrings were given, failing the test if any is absent.
// A caller compares the returned positions to assert relative order.
func order(t *testing.T, body string, substrings ...string) []int {
	t.Helper()
	positions := make([]int, len(substrings))
	for index, substring := range substrings {
		position := strings.Index(body, substring)
		if position < 0 {
			t.Fatalf("generated C does not contain %q:\n%s", substring, body)
		}
		positions[index] = position
	}
	return positions
}

func requireAscending(t *testing.T, positions []int, labels ...string) {
	t.Helper()
	for index := 1; index < len(positions); index++ {
		if positions[index] <= positions[index-1] {
			t.Fatalf("%s must appear before %s", labels[index-1], labels[index])
		}
	}
}

func TestCallArgumentsEvaluateLeftToRight(t *testing.T) {
	result := assertCompiles(t, "fun a(): Int32 do\n    return 1\nend\n"+
		"fun b(): Int32 do\n    return 2\nend\n"+
		"fun total(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n"+
		"result: Int32 := total(a(), b())\n")
	body := rootC(t, result)
	// A bare, side-effect-free function reference callee is never hoisted,
	// so total calls through its plain name directly.
	positions := order(t, body, "_a();", "_b();", "hex_f_m3_app_total(hex_seq_1, hex_seq_2)")
	requireAscending(t, positions, "a()", "b()", "total(...)")
}

func TestMethodReceiverEvaluatesBeforeArguments(t *testing.T) {
	result := assertCompiles(t, "type Point = { mut x: Int32, mut y: Int32, }\n"+
		"impl Point.sum(delta: Int32): Int32 do\n    return self.x + self.y + delta\nend\n"+
		"fun make_point(): Point do\n    return Point { x = 1, y = 2 }\nend\n"+
		"fun delta(): Int32 do\n    return 5\nend\n"+
		"result: Int32 := make_point().sum(delta())\n")
	body := rootC(t, result)
	positions := order(t, body, "_make_point();", "_delta();", "_sum(hex_seq_1, hex_seq_2)")
	requireAscending(t, positions, "make_point()", "delta()", "sum(...)")
}

func TestBinaryOperandsEvaluateLeftToRight(t *testing.T) {
	result := assertCompiles(t, "fun a(): Int32 do\n    return 1\nend\n"+
		"fun b(): Int32 do\n    return 2\nend\n"+
		"result: Int32 := a() + b()\n")
	body := rootC(t, result)
	positions := order(t, body, "_a();", "_b();", "hex_wrap_add_int32_t(hex_seq_1, hex_seq_2)")
	requireAscending(t, positions, "a()", "b()", "add(...)")
}

func TestArrayLiteralElementsEvaluateLeftToRight(t *testing.T) {
	result := assertCompiles(t, "fun a(): Int32 do\n    return 1\nend\n"+
		"fun b(): Int32 do\n    return 2\nend\n"+
		"values: Array<Int32, 2> := [a(), b()]\n")
	body := rootC(t, result)
	positions := order(t, body, "_a();", "_b();", "{{hex_seq_1, hex_seq_2}}")
	requireAscending(t, positions, "a()", "b()", "array literal")
}

// An object literal's fields evaluate left to right in written order, not
// declaration order, while the compound literal itself still assigns each
// field from the correct temporary regardless of which order they were
// hoisted in.
func TestObjectLiteralFieldsEvaluateInWrittenOrder(t *testing.T) {
	result := assertCompiles(t, "type Pair = { a: Int32, b: Int32, }\n"+
		"fun side_a(): Int32 do\n    return 10\nend\n"+
		"fun side_b(): Int32 do\n    return 20\nend\n"+
		"value: Pair := Pair { b = side_b(), a = side_a() }\n")
	body := rootC(t, result)
	// b is written first, so its temporary is hoisted first even though a
	// is declared first and assigned first in the final compound literal.
	positions := order(t, body, "_side_b();", "_side_a();", ".hex_m_a = hex_seq_2", ".hex_m_b = hex_seq_1")
	requireAscending(t, positions, "side_b() hoisted", "side_a() hoisted", ".a assigned from hex_seq_2", ".b assigned from hex_seq_1")
}

// An ADT variant constructor's payload fields evaluate left to right in
// written order, matching the object literal case above; the checker-level
// value-correctness half of this is TestCheckADTPayloadOutOfOrderAssignsCorrectFields.
func TestAdtLiteralFieldsEvaluateInWrittenOrder(t *testing.T) {
	result := assertCompiles(t, "type W as | A { first: Int32, second: Int32, } | B { x: Int32, } end\n"+
		"fun side_first(): Int32 do\n    return 10\nend\n"+
		"fun side_second(): Int32 do\n    return 20\nend\n"+
		"w: W := W.A { second = side_second(), first = side_first() }\n")
	body := rootC(t, result)
	positions := order(t, body, "_side_second();", "_side_first();", ".hex_m_first = hex_seq_2", ".hex_m_second = hex_seq_1")
	requireAscending(t, positions, "side_second() hoisted", "side_first() hoisted", ".first assigned from hex_seq_2", ".second assigned from hex_seq_1")
}

// An explicitly grouped mixed-operator tree keeps its written associativity
// while still evaluating left to right: a() + (b() * c()) must still
// evaluate a(), then b(), then c(), in that order.
func TestMixedOperatorsPreservePrecedenceAndLeftToRightEvaluation(t *testing.T) {
	result := assertCompiles(t, "fun a(): Int32 do\n    return 1\nend\n"+
		"fun b(): Int32 do\n    return 2\nend\n"+
		"fun c(): Int32 do\n    return 3\nend\n"+
		"result: Int32 := a() + (b() * c())\n")
	body := rootC(t, result)
	if !strings.Contains(body, "hex_wrap_mul_int32_t(hex_seq_2, hex_seq_3)") {
		t.Fatalf("generated C = %q, want b() * c() grouped by precedence into its own temporary", body)
	}
	positions := order(t, body, "_a();", "_b();", "_c();", "hex_wrap_mul_int32_t(hex_seq_2, hex_seq_3)", "hex_wrap_add_int32_t(hex_seq_1, hex_seq_4)")
	requireAscending(t, positions, "a()", "b()", "c()", "b()*c()", "a()+...")
}

// DeepEqualityExpression (String equality here) is still a binary expression
// from the source language's point of view and must sequence its two
// operands left to right exactly like BinaryOperationExpression.
func TestStringEqualityOperandsEvaluateLeftToRight(t *testing.T) {
	result := assertCompiles(t, "fun left(): String do\n    return \"a\"\nend\n"+
		"fun right(): String do\n    return \"b\"\nend\n"+
		"result: Bool := left() == right()\n")
	body := rootC(t, result)
	positions := order(t, body, "_left();", "_right();", "hex_equal_hex_string(hex_seq_1, hex_seq_2)")
	requireAscending(t, positions, "left()", "right()", "equality(...)")
}

// Atomic.compare_exchange combines its receiver and both operands into one
// C call with no operand-order guarantee; expected and desired must still
// evaluate left to right.
func TestAtomicCompareExchangeOperandsEvaluateLeftToRight(t *testing.T) {
	result := assertCompiles(t, "fun expected(): Int32 do\n    return 6\nend\n"+
		"fun desired(): Int32 do\n    return 7\nend\n"+
		"fun run(): Bool do\n    counter: Atomic<Int32> := Atomic<Int32>.new(0)\n    return counter.compare_exchange(expected(), desired())\nend\n")
	body := rootC(t, result)
	positions := order(t, body, "_expected();", "_desired();", "hex_atomic_Int32_compare_exchange(&(hex_v_counter), hex_seq_1, hex_seq_2)")
	requireAscending(t, positions, "expected()", "desired()", "compare_exchange(...)")
}

// An assignment evaluates the target place, including its index, before the
// source value.
func TestAssignmentEvaluatesTargetBeforeSource(t *testing.T) {
	result := assertCompiles(t, "fun idx(): Size do\n    return 1\nend\n"+
		"fun value(): Int32 do\n    return 9\nend\n"+
		"mut values: Array<Int32, 3> := [0, 0, 0]\n"+
		"values[idx()] = value()\n")
	body := rootC(t, result)
	positions := order(t, body, "_idx();", "_value();")
	requireAscending(t, positions, "idx()", "value()")
}

// A nested compound expression hoists innermost first: a() and b(), from
// g's own argument list, get their temporaries before g(...)'s own
// temporary, which in turn precedes c()'s.
func TestNestedCompoundExpressionsHoistInnermostFirst(t *testing.T) {
	result := assertCompiles(t, "fun a(): Int32 do\n    return 1\nend\n"+
		"fun b(): Int32 do\n    return 2\nend\n"+
		"fun c(): Int32 do\n    return 3\nend\n"+
		"fun g(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n"+
		"fun f(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n"+
		"result: Int32 := f(g(a(), b()), c())\n")
	body := rootC(t, result)
	// A bare, side-effect-free function reference callee is never hoisted:
	// it has no observable evaluation-order effect of its own, so g and f
	// call through their plain names directly rather than through a
	// hoisted temporary.
	positions := order(t, body, "_a();", "_b();", "hex_f_m3_app_g(hex_seq_1, hex_seq_2)", "_c();", "hex_f_m3_app_f(hex_seq_3, hex_seq_4)")
	requireAscending(t, positions, "a()", "b()", "g(...)", "c()", "f(...)")
}

// and/or keep evaluating their right operand only when reached: a call
// inside the right operand must not be lifted into an unconditional
// prologue ahead of the left operand's own test.
func TestShortCircuitOperandsAreNotHoisted(t *testing.T) {
	result := assertCompiles(t, "fun guard(): Bool do\n    return false\nend\n"+
		"fun a(): Int32 do\n    return 1\nend\n"+
		"fun b(): Int32 do\n    return 2\nend\n"+
		"result: Bool := guard() and (a() == b())\n")
	body := rootC(t, result)
	if strings.Contains(body, "hex_seq_") {
		t.Fatalf("short-circuited operand was hoisted into an unconditional prologue:\n%s", body)
	}
	guardPosition := strings.Index(body, "hex_f_m3_app_guard()")
	andPosition := strings.Index(body, "&&")
	if guardPosition < 0 || andPosition < 0 || andPosition <= guardPosition {
		t.Fatalf("generated C = %q, want guard() before && with a() and b() still inside the right operand", body)
	}
}

// A statement mixing an existing hoist (try) with an unrelated multi-operand
// binary expression sequences both correctly: the try's own operand
// resolves through hoistTry unchanged, and the surrounding addition still
// gets its own temporaries for its two operands.
func TestTryAndSequencingHoistsCoexistInOneStatement(t *testing.T) {
	result := assertCompiles(t, "fun risky(): Int32 | Error do\n    return 1\nend\n"+
		"fun b(): Int32 do\n    return 2\nend\n"+
		"fun total(): Int32 | Error do\n    result: Int32 := (try risky()) + b()\n    return result\nend\n")
	body := rootC(t, result)
	if !strings.Contains(body, "hex_try_1") {
		t.Fatalf("generated C = %q, want the existing try hoist to still fire", body)
	}
	positions := order(t, body, "hex_try_1", "_b();", "hex_wrap_add_int32_t(hex_seq_1, hex_seq_2)")
	requireAscending(t, positions, "try hoist", "b()", "add(...)")
}

// Repeated compilations of the same source produce byte-identical generated
// files: the hoist counters and map iteration involved carry no
// nondeterminism.
func TestEvaluationOrderHoistingIsDeterministic(t *testing.T) {
	source := "type Pair = { a: Int32, b: Int32, }\n" +
		"fun a(): Int32 do\n    return 1\nend\n" +
		"fun b(): Int32 do\n    return 2\nend\n" +
		"fun c(): Int32 do\n    return 3\nend\n" +
		"fun g(x: Int32, y: Int32): Int32 do\n    return x + y\nend\n" +
		"value: Pair := Pair { b = b(), a = a() }\n" +
		"sum: Int32 := g(a(), b()) + c()\n"
	first := compileSourceFiles(t, source)
	second := compileSourceFiles(t, source)
	if len(first) != len(second) {
		t.Fatalf("file count changed across repeated compilation: %d vs %d", len(first), len(second))
	}
	for name, content := range first {
		other, ok := second[name]
		if !ok {
			t.Fatalf("second compilation is missing artifact %q", name)
		}
		if other != content {
			t.Fatalf("artifact %q differs across repeated compilation:\n--- first ---\n%s\n--- second ---\n%s", name, content, other)
		}
	}
}

func compileSourceFiles(t *testing.T, source string) map[string]string {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("expected success; got %d diagnostic(s):\n%s", len(result.Stderr), strings.Join(result.Stderr, "\n"))
	}
	return result.Files
}
