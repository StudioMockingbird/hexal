package checker

// Named immutable binding reads: an ordinary read keeps the binding's
// variable read in the checked program, while the initializer value stays
// available as known-value metadata for every compile-time diagnostic and
// constant-required check that used the substituted value before.

import (
	"testing"
)

func TestCheckKnownImmutableReadStaysVariableRead(t *testing.T) {
	checked := requireAccepted(t, "enabled: Bool = true flag: Bool = enabled")
	flag := checked.Statements[1].(Declaration)
	if flag.Source.Kind != VariableOperand || flag.Source.Node.Kind != VariableExpression || flag.Source.Node.Name != "enabled" {
		t.Fatalf("flag source = %#v, want a variable read of enabled", flag.Source)
	}
}

func TestCheckKnownImmutableReadStaysReadThroughArithmetic(t *testing.T) {
	checked := requireAccepted(t, "count: UInt8 = 200 next: UInt8 = count + 1")
	next := checked.Statements[1].(Declaration)
	if next.Source.Kind != ExpressionOperand || next.Source.Node.Kind != BinaryOperationExpression {
		t.Fatalf("next source = %#v, want a runtime binary operation", next.Source)
	}
	if next.Source.Node.Left == nil || next.Source.Node.Left.Kind != VariableExpression || next.Source.Node.Left.Name != "count" {
		t.Fatalf("left operand = %#v, want a read of count", next.Source.Node.Left)
	}
}

func TestCheckKnownImmutableReadStaysReadThroughUnaryNegation(t *testing.T) {
	checked := requireAccepted(t, "negativeZero: Float64 = -0.0 negated: Float64 = -negativeZero")
	negated := checked.Statements[1].(Declaration)
	if negated.Source.Kind != ExpressionOperand || negated.Source.Node.Kind != UnaryOperationExpression || negated.Source.Node.Operator != NegateOperator {
		t.Fatalf("negated source = %#v, want a runtime negation", negated.Source)
	}
	if negated.Source.Node.Operand == nil || negated.Source.Node.Operand.Kind != VariableExpression || negated.Source.Node.Operand.Name != "negativeZero" {
		t.Fatalf("negation operand = %#v, want a read of negativeZero", negated.Source.Node.Operand)
	}
}

func TestCheckKnownImmutableReadStaysReadInCondition(t *testing.T) {
	checked := requireAccepted(t, "enabled: Bool = true if enabled then end")
	statement := checked.Statements[1].(IfStatement)
	if statement.Condition.Kind != VariableOperand || statement.Condition.Node.Kind != VariableExpression || statement.Condition.Node.Name != "enabled" {
		t.Fatalf("condition = %#v, want a variable read of enabled", statement.Condition)
	}
}

func TestCheckKnownImmutableShiftCountStillDiagnosed(t *testing.T) {
	requireDiagnostic(t, "count: Int32 = 32 bad: Int32 = 1 << count", "shift count 32 is outside the valid range for Int32")
}

func TestCheckKnownImmutableZeroDivisorStillDiagnosed(t *testing.T) {
	requireDiagnostic(t, "zero: Int32 = 0 bad: Int32 = 10 / zero", "division by zero")
	requireDiagnostic(t, "zero: Int32 = 0 bad: Int32 = 10 % zero", "division by zero")
	// An immutable binding copied from another known binding keeps its
	// known value through the copy.
	requireDiagnostic(t, "zero: Int32 = 0 divisor: Int32 = zero bad: Int32 = 10 / divisor", "division by zero")
}

func TestCheckKnownImmutableArrayIndexBoundsStillDiagnosed(t *testing.T) {
	requireDiagnostic(t, "size: Int32 = 3 arr: Array<Int32, 2> = [1, 2] bad: Int32 = arr[size]", "array index 3 is out of bounds for Array<Int32, 2>")
	requireDiagnostic(t, "size: Int32 = -1 arr: Array<Int32, 2> = [1, 2] bad: Int32 = arr[size]", "an array index must be non-negative")
}

func TestCheckKnownImmutableSliceBoundsStillDiagnosed(t *testing.T) {
	requireDiagnostic(t, "begin: Int32 = 1 limit: Int32 = 5 arr: Array<Int32, 2> = [1, 2] view: View<Int32> = arr.slice(begin, limit)", "slice range [1, 5) is out of bounds for Array<Int32, 2>")
}

func TestCheckKnownImmutableChannelCapacityStillDiagnosed(t *testing.T) {
	requireDiagnostic(t, "h: Heap = Heap.new() capacity: Size = 0 ch: Channel<Int32> = Channel<Int32>.new(h, capacity)", "compile-time Channel capacity must be positive")
}

func TestCheckKnownImmutableWhileTrueStillStarves(t *testing.T) {
	requireDiagnostic(t, "fun worker(): Bool do\n    enabled: Bool = true\n    while enabled do\n    end\n    return true\nend\nfun run(): Int32 | Error do\n    task: Task<Bool> = try spawn worker()\n    task.join()\n    return 0\nend\n", "while true loop must execute Task.yield() on every repeating path")
}

func TestCheckKnownImmutableShortCircuitSkipsUnreachableDivision(t *testing.T) {
	// The short-circuit decision consults the binding's known-value
	// metadata, so the unreachable right-hand division stays unchecked
	// exactly as it was before reads stopped being substituted, and the
	// read itself stays in the runtime expression.
	checked := requireAccepted(t, "guard: Bool = true result: Bool = guard or (1 / 0 == 0)")
	result := checked.Statements[1].(Declaration)
	if result.Source.Kind != ExpressionOperand || result.Source.Node.Kind != BinaryOperationExpression || result.Source.Node.Operator != LogicalOrOperator {
		t.Fatalf("result source = %#v, want a runtime logical or", result.Source)
	}
	if result.Source.Node.Left == nil || result.Source.Node.Left.Kind != VariableExpression || result.Source.Node.Left.Name != "guard" {
		t.Fatalf("left operand = %#v, want a read of guard", result.Source.Node.Left)
	}
}
