package checker

import (
	"go/constant"
	"math"
	"strings"
	"testing"

	compilerTypes "hexal/compiler/types"
)

func TestOperationNodeCarriesResolvedOperatorAndTypes(t *testing.T) {
	left := Expression{Kind: VariableExpression, Name: "left"}
	right := Expression{Kind: VariableExpression, Name: "right"}
	node := operationBinaryNode(AddOperator, left, right, compilerTypes.Int32, compilerTypes.Int32)

	if node.Kind != BinaryOperationExpression {
		t.Fatalf("kind = %v, want %v", node.Kind, BinaryOperationExpression)
	}
	if node.Operator != AddOperator {
		t.Fatalf("operator = %v, want %v", node.Operator, AddOperator)
	}
	if node.Left == nil || node.Left.Kind != left.Kind || node.Left.Name != left.Name {
		t.Fatalf("left = %#v, want %#v", node.Left, left)
	}
	if node.Right == nil || node.Right.Kind != right.Kind || node.Right.Name != right.Name {
		t.Fatalf("right = %#v, want %#v", node.Right, right)
	}
	if node.OperandType != compilerTypes.Int32 || node.ResultType != compilerTypes.Int32 {
		t.Fatalf("types = %v/%v, want Int32/Int32", node.OperandType.Name, node.ResultType.Name)
	}
}

func TestCheckContextualTypingThroughOperators(t *testing.T) {
	checked, err := Check(parseProgram(t, "total: Int64 = 5_000_000_000 + 1 ratio: Float32 = 1.5 * 2.0"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}

	for index, want := range []compilerTypes.Type{compilerTypes.Int64, compilerTypes.Float32} {
		declaration := checked.Statements[index].(Declaration)
		if !compilerTypes.Equal(declaration.Source.Type, want) {
			t.Fatalf("declaration %d source type = %s, want %s", index, declaration.Source.Type.Name, want.Name)
		}
		if declaration.Source.Kind != ConstantOperand {
			t.Fatalf("declaration %d source = %#v, want an exact folded constant", index, declaration.Source)
		}
	}
}

func TestCheckContextualTypingUsesOnlyAdmissibleExpectedTypes(t *testing.T) {
	_, err := Check(parseProgram(t, "big: Bool = 5_000_000_000 > 1"))
	if err == nil || !strings.Contains(err.Error(), "given value is outside the Int32 range") {
		t.Fatalf("Check error = %v, want Int32 fallback range diagnostic", err)
	}

	checked, err := Check(parseProgram(t, "mut threshold: Int64 = 5_000_000_000 big: Bool = threshold > 1"))
	if err != nil {
		t.Fatalf("Check returned an error for a typed comparison: %v", err)
	}
	comparison := checked.Statements[1].(Declaration)
	if comparison.Source.Node.Right == nil || comparison.Source.Node.Right.Constant == nil || !compilerTypes.Equal(comparison.Source.Node.Right.Constant.Type, compilerTypes.Int64) {
		t.Fatalf("comparison right operand = %#v, want an Int64 contextual literal", comparison.Source.Node.Right)
	}
}

func TestCheckContextualTypingAllowsLosslessResultWidening(t *testing.T) {
	// An expected destination may widen a completed arithmetic
	// result losslessly.
	checked, err := Check(parseProgram(t, "a: Int32 = 2 doubled: Int64 = a + a"))
	if err != nil {
		t.Fatalf("Check error = %v, want successful lossless widening", err)
	}
	if got := checked.Statements[1].(Declaration).Source.Node.ResultType; !compilerTypes.Equal(got, compilerTypes.Int32) {
		t.Fatalf("arithmetic result type = %s, want the selected common type Int32", got.Name)
	}
	if got := checked.Statements[1].(Declaration).Type; !compilerTypes.Equal(got, compilerTypes.Int64) {
		t.Fatalf("declaration type = %s, want the expected destination Int64", got.Name)
	}
}

func TestCheckOperatorDomains(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"value: Float64 = 1.0 % 2.0", "operator % requires integer operands; got Float64"},
		{"left: Bool = true right: Bool = false bad: Bool = left < right", "ordering is unavailable for Bool values"},
		{"count: UInt32 = 5 bad: Int32 = -count", "negation requires a signed type; got UInt32"},
	} {
		_, err := Check(parseProgram(t, testCase.source))
		if err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Errorf("Check(%q) error = %v, want diagnostic containing %q", testCase.source, err, testCase.want)
		}
	}
}

func TestCheckOperatorSelectsLosslessCommonArithmeticType(t *testing.T) {
	// Mixed arithmetic selects the unique least lossless
	// common type.
	checked, err := Check(parseProgram(t, "small: Int16 = 1 large: Int32 = 2 total: Int32 = small + large"))
	if err != nil {
		t.Fatalf("Check error = %v, want successful common-type arithmetic", err)
	}
	total := checked.Statements[2].(Declaration)
	if got := total.Source.Node.ResultType; !compilerTypes.Equal(got, compilerTypes.Int32) {
		t.Fatalf("arithmetic result type = %s, want Int32", got.Name)
	}
}

func TestCheckFoldsLiteralArithmeticAndWrapsOverflow(t *testing.T) {
	// Literal-only arithmetic still folds; a read of a named immutable
	// binding stays a runtime operation, so the fold assertions use
	// literal spellings.
	checked, err := Check(parseProgram(t, "next: UInt8 = 200 + 1"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	next := checked.Statements[0].(Declaration)
	if next.Source.Kind != ConstantOperand {
		t.Fatalf("next source = %#v, want a folded constant", next.Source)
	}
	if got, ok := constant.Uint64Val(next.Source.Constant); !ok || got != 201 {
		t.Fatalf("next value = %v, want 201", next.Source.Constant)
	}

	// Integer overflow wraps at the result type during folding.
	checked, err = Check(parseProgram(t, "over: UInt8 = 200 + 100"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	over := checked.Statements[0].(Declaration)
	if got, ok := constant.Uint64Val(over.Source.Constant); !ok || got != 44 {
		t.Fatalf("over value = %v, want wrapped 44", over.Source.Constant)
	}
}

func TestCheckKeepsMutableOperationsForRuntimeWrapping(t *testing.T) {
	checked, err := Check(parseProgram(t, "mut count: UInt8 = 200 next: UInt8 = count + 100"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	next := checked.Statements[1].(Declaration)
	if next.Source.Kind != ExpressionOperand || next.Source.Node.Kind != BinaryOperationExpression {
		t.Fatalf("next source = %#v, want an expression operand", next.Source)
	}
}

func TestCheckFoldsIntegerBooleanAndComparisonOperations(t *testing.T) {
	checked, err := Check(parseProgram(t, "sum: Int32 = 2 + 3 difference: Int32 = 9 - 4 product: Int32 = 3 * 4 quotient: Int32 = 9 / 2 remainder: Int32 = 9 % 4 inverted: Bool = !true equal: Bool = 2 == 2 ordered: Bool = 1 < 2 both: Bool = true and false either: Bool = false or true"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	wantIntegers := []int64{5, 5, 12, 4, 1}
	for index, want := range wantIntegers {
		declaration := checked.Statements[index].(Declaration)
		if declaration.Source.Kind != ConstantOperand {
			t.Fatalf("declaration %d source = %#v, want a folded constant", index, declaration.Source)
		}
		if got, ok := constant.Int64Val(declaration.Source.Constant); !ok || got != want {
			t.Fatalf("declaration %d value = %v, want %d", index, declaration.Source.Constant, want)
		}
	}
	wantBools := []bool{false, true, true, false, true}
	for index, want := range wantBools {
		declaration := checked.Statements[len(wantIntegers)+index].(Declaration)
		if declaration.Source.Kind != ConstantOperand || constant.BoolVal(declaration.Source.Constant) != want {
			t.Fatalf("boolean declaration %d source = %#v, want %t", index, declaration.Source, want)
		}
	}
}

func TestCheckFoldsFloatBitsAndIEEEComparisons(t *testing.T) {
	// Fold assertions use literal spellings: `-negativeZero` and `nan ==
	// nan` read named bindings, which stay runtime operations.
	checked, err := Check(parseProgram(t, "sum: Float32 = 1.5 + 2.25 negativeZero: Float64 = -0.0 negatedZero: Float64 = -(-0.0) nan: Float64 = 0.0 / 0.0 nanEqual: Bool = (0.0 / 0.0) == (0.0 / 0.0) nanDifferent: Bool = (0.0 / 0.0) != (0.0 / 0.0)"))
	if err != nil {
		t.Fatalf("Check returned an error: %v", err)
	}
	sum := checked.Statements[0].(Declaration)
	if sum.Source.Kind != ConstantOperand || sum.Source.FloatBits != uint64(math.Float32bits(float32(3.75))) {
		t.Fatalf("Float32 sum = %#v, want folded 3.75 bits", sum.Source)
	}
	negatedZero := checked.Statements[2].(Declaration)
	if negatedZero.Source.Kind != ConstantOperand || negatedZero.Source.FloatBits != math.Float64bits(0) {
		t.Fatalf("negated zero = %#v, want positive zero", negatedZero.Source)
	}
	nan := checked.Statements[3].(Declaration)
	if nan.Source.Kind != ConstantOperand || !math.IsNaN(math.Float64frombits(nan.Source.FloatBits)) {
		t.Fatalf("nan = %#v, want folded NaN", nan.Source)
	}
	for index, want := range []bool{false, true} {
		declaration := checked.Statements[4+index].(Declaration)
		if declaration.Source.Kind != ConstantOperand || constant.BoolVal(declaration.Source.Constant) != want {
			t.Fatalf("NaN comparison %d = %#v, want %t", index, declaration.Source, want)
		}
	}
}

func TestCheckRejectsStaticZeroDivisorsIncludingFoldedExpressions(t *testing.T) {
	for _, source := range []string{
		"result: Int32 = 1 / 0",
		"mut total: Int32 = 10 bad: Int32 = total / 0",
		"mut total: Int32 = 10 bad: Int32 = total % 0",
		"mut total: Int32 = 10 bad: Int32 = total / (2 - 2)",
	} {
		_, err := Check(parseProgram(t, source))
		if err == nil || !strings.Contains(err.Error(), "division by zero") {
			t.Errorf("Check(%q) error = %v, want division by zero", source, err)
		}
	}
}

func TestCheckFoldsStaticSignedMinimumDivisors(t *testing.T) {
	// Signed minimum divided by -1 wraps to the minimum; the
	// remainder is zero. The fold is asserted with literal spellings, since
	// a read of the named immutable `minimum` binding stays runtime.
	checked, err := Check(parseProgram(t, "quotient: Int8 = -128 / -1"))
	if err != nil {
		t.Fatalf("Check error = %v, want folded minimum/-1 division", err)
	}
	quotient := checked.Statements[0].(Declaration)
	if quotient.Source.Kind != ConstantOperand {
		t.Fatalf("quotient source = %#v, want a folded constant", quotient.Source)
	}
	if got, ok := constant.Int64Val(quotient.Source.Constant); !ok || got != -128 {
		t.Fatalf("quotient value = %v, want -128", quotient.Source.Constant)
	}
	checked, err = Check(parseProgram(t, "remainder: Int8 = -128 % -1"))
	if err != nil {
		t.Fatalf("Check error = %v, want folded minimum/-1 remainder", err)
	}
	remainder := checked.Statements[0].(Declaration)
	if got, ok := constant.Int64Val(remainder.Source.Constant); !ok || got != 0 {
		t.Fatalf("remainder value = %v, want 0", remainder.Source.Constant)
	}
}

func TestCheckShortCircuitReachability(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   bool
	}{
		{"result: Bool = true or (1 / 0 == 0)", true},
		{"result: Bool = false and (1 / 0 == 0)", false},
		// Mixed-type logical operands fold through their
		// truthiness; the unreachable RHS never evaluates.
		{"result: Bool = true or (1 and 2)", true},
		{"result: Bool = false and (1 or 2)", false},
	} {
		checked, err := Check(parseProgram(t, testCase.source))
		if err != nil {
			t.Fatalf("Check(%q) returned an error for unreachable RHS: %v", testCase.source, err)
		}
		result := checked.Statements[0].(Declaration)
		if result.Source.Kind != ConstantOperand || constant.BoolVal(result.Source.Constant) != testCase.want {
			t.Fatalf("Check(%q) source = %#v, want folded %t", testCase.source, result.Source, testCase.want)
		}
	}

	for _, source := range []string{
		"mut guard: Bool = true result: Bool = guard or (1 / 0 == 0)",
		"mut guard: Bool = false result: Bool = guard and (1 / 0 == 0)",
	} {
		_, err := Check(parseProgram(t, source))
		if err == nil || !strings.Contains(err.Error(), "division by zero") {
			t.Errorf("Check(%q) error = %v, want static zero-divisor diagnostic", source, err)
		}
	}
}

func TestCheckAcceptsUnknownRuntimeDivisors(t *testing.T) {
	if _, err := Check(parseProgram(t, "mut divisor: Int32 = 0 total: Int32 = 10 quotient: Int32 = total / divisor remainder: Int32 = total % divisor")); err != nil {
		t.Fatalf("Check rejected unknown runtime divisors: %v", err)
	}
}

// and/or accept mixed operand types and the result is always Bool;
// constant operands fold through their truthiness.
func TestCheckLogicalOperandsAcceptMixedTypes(t *testing.T) {
	checked, err := Check(parseProgram(t, "mut count: Int32 = 1 ready: Bool = true flag: Bool = count and ready"))
	if err != nil {
		t.Fatalf("Check returned an error for a mixed-type logical operation: %v", err)
	}
	declaration := checked.Statements[2].(Declaration)
	if declaration.Source.Type != compilerTypes.Bool || declaration.Source.Kind != ExpressionOperand {
		t.Fatalf("flag source = %#v, want a runtime Bool expression", declaration.Source)
	}
	if declaration.Source.Node.Kind != BinaryOperationExpression || declaration.Source.Node.Operator != LogicalAndOperator {
		t.Fatalf("flag node = %#v, want a logical and operation", declaration.Source.Node)
	}

	for _, testCase := range []struct {
		source string
		want   bool
	}{
		{"flag: Bool = 1 and 2", true},
		{"flag: Bool = 1 and nil", false},
		{"flag: Bool = nil or 2", true},
		{"flag: Bool = !0", false},
		{"flag: Bool = !nil", true},
	} {
		checked, err := Check(parseProgram(t, testCase.source))
		if err != nil {
			t.Fatalf("Check(%q) returned an error: %v", testCase.source, err)
		}
		flag := checked.Statements[0].(Declaration)
		if flag.Source.Kind != ConstantOperand || constant.BoolVal(flag.Source.Constant) != testCase.want {
			t.Fatalf("Check(%q) source = %#v, want folded %t", testCase.source, flag.Source, testCase.want)
		}
	}
}

// not accepts any value-producing operand; a runtime non-Bool
// operand stays a runtime operation.
func TestCheckLogicalNotAcceptsNonBoolOperand(t *testing.T) {
	checked, err := Check(parseProgram(t, "mut count: Int32 = 1 flag: Bool = !count"))
	if err != nil {
		t.Fatalf("Check returned an error for a non-Bool not operand: %v", err)
	}
	flag := checked.Statements[1].(Declaration)
	if flag.Source.Kind != ExpressionOperand || flag.Source.Node.Kind != UnaryOperationExpression || flag.Source.Node.Operator != LogicalNotOperator {
		t.Fatalf("flag source = %#v, want a runtime logical not operation", flag.Source)
	}
}

// A call that produces no value stays rejected in a condition.
func TestCheckConditionRejectsNoResultCall(t *testing.T) {
	requireDiagnostic(t, "fun reset() do\n    return\nend\nif reset() then end\n", "reset produces no value")
}
