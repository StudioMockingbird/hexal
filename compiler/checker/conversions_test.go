package checker

// Explicit numeric conversion constant folding. The fold applies the
// truncate-then-range-check contract to the source type's already-rounded
// IEEE value without an Int64 intermediate, so valid UInt64 results above
// INT64_MAX fold and Float32 literals reason from their rounded value.

import (
	"go/constant"
	"testing"

	compilerTypes "hexal/compiler/types"
)

func TestCheckFoldsFloat64TwoToSixtyThreeToUInt64(t *testing.T) {
	checked := requireAccepted(t, "big: UInt64 := 9223372036854775808.0.to<UInt64>()")
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Kind != ConstantOperand {
		t.Fatalf("big source = %#v, want a folded UInt64 constant", declaration.Source)
	}
	if got, ok := constant.Uint64Val(declaration.Source.Constant); !ok || got != uint64(1)<<63 {
		t.Fatalf("folded UInt64 = %v, want 2^63", declaration.Source.Constant)
	}
}

func TestCheckRejectsFloat64TwoToSixtyFourForUInt64(t *testing.T) {
	requireDiagnostic(t, "bad: UInt64 := 18446744073709551616.0.to<UInt64>()", "value 18446744073709551616 is outside the range of UInt64")
}

func TestCheckRejectsFloat64TwoToSixtyThreeForInt64(t *testing.T) {
	requireDiagnostic(t, "bad: Int64 := 9223372036854775808.0.to<Int64>()", "value 9223372036854775808 is outside the range of Int64")
}

func TestCheckFoldsFloat32ConversionFromRoundedBits(t *testing.T) {
	// 16777217.5 is exactly representable as Float64 but rounds to
	// 16777218.0 when converted to Float32; the second fold must reason
	// from the already-rounded Float32 value, not the lexical spelling.
	checked := requireAccepted(t, "value: UInt32 := 16777217.5.to<Float32>().to<UInt32>()")
	declaration := checked.Statements[0].(Declaration)
	if got, ok := constant.Uint64Val(declaration.Source.Constant); !ok || got != 16777218 {
		t.Fatalf("folded UInt32 = %v, want 16777218 (the rounded Float32 value)", declaration.Source.Constant)
	}
}

func TestCheckFoldsFloat64ExactValueBeforeTruncation(t *testing.T) {
	// The same spelling without the Float32 rounding step truncates the
	// exact Float64 value.
	checked := requireAccepted(t, "value: UInt32 := 16777217.5.to<UInt32>()")
	declaration := checked.Statements[0].(Declaration)
	if got, ok := constant.Uint64Val(declaration.Source.Constant); !ok || got != 16777217 {
		t.Fatalf("folded UInt32 = %v, want 16777217 (the exact Float64 truncation)", declaration.Source.Constant)
	}
}

func TestCheckFoldsIntegerToFloat32RoundingForChainedConversion(t *testing.T) {
	// 16777217 does not fit a Float32 mantissa: the integer-valued float
	// rounds to 16777216.0 when converted to Float32, and the chained
	// float-to-integer fold must use that rounded value.
	checked := requireAccepted(t, "value: Int32 := 16777217.0.to<Float32>().to<Int32>()")
	declaration := checked.Statements[0].(Declaration)
	if got, ok := constant.Int64Val(declaration.Source.Constant); !ok || got != 16777216 {
		t.Fatalf("folded Int32 = %v, want 16777216 (the rounded Float32 value)", declaration.Source.Constant)
	}
}

func TestCheckFoldsNegativeFractionToUnsignedZero(t *testing.T) {
	// A value in (-1, 0) truncates to signed zero and is valid for an
	// unsigned destination.
	checked := requireAccepted(t, "zero: UInt32 := (-0.5).to<UInt32>()")
	declaration := checked.Statements[0].(Declaration)
	if got, ok := constant.Uint64Val(declaration.Source.Constant); !ok || got != 0 {
		t.Fatalf("folded UInt32 = %v, want 0 (truncated signed zero)", declaration.Source.Constant)
	}
}

func TestCheckFoldsNegativeFractionForSignedDestination(t *testing.T) {
	checked := requireAccepted(t, "zero: Int32 := (-0.5).to<Int32>()")
	declaration := checked.Statements[0].(Declaration)
	if got, ok := constant.Int64Val(declaration.Source.Constant); !ok || got != 0 {
		t.Fatalf("folded Int32 = %v, want 0 (truncated signed zero)", declaration.Source.Constant)
	}
	checked = requireAccepted(t, "negative: Int32 := (-1.5).to<Int32>()")
	declaration = checked.Statements[0].(Declaration)
	if got, ok := constant.Int64Val(declaration.Source.Constant); !ok || got != -1 {
		t.Fatalf("folded Int32 = %v, want -1 (truncated toward zero)", declaration.Source.Constant)
	}
}

func TestCheckRejectsTruncatedNegativeForUnsigned(t *testing.T) {
	requireDiagnostic(t, "bad: UInt32 := (-1.5).to<UInt32>()", "value -1 is outside the range of UInt32")
}

func TestCheckFoldsPositiveFractionTowardZero(t *testing.T) {
	checked := requireAccepted(t, "value: Int32 := 2.5.to<Int32>()")
	declaration := checked.Statements[0].(Declaration)
	if got, ok := constant.Int64Val(declaration.Source.Constant); !ok || got != 2 {
		t.Fatalf("folded Int32 = %v, want 2 (truncated toward zero)", declaration.Source.Constant)
	}
}

func TestCheckRejectsNonFiniteAndHugeFloatConversion(t *testing.T) {
	// The Float32 conversion of 3.5e38 overflows to +Infinity, so the
	// chained conversion sees a non-finite source and is rejected.
	requireDiagnostic(t, "bad: Int32 := 3.5e38.to<Float32>().to<Int32>()", "floating value cannot be converted to Int32")
	requireDiagnostic(t, "bad: Int32 := 3.5e38.to<Int32>()", "value 350000000000000001565567347835409530880 is outside the range of Int32")
}

func TestCheckFoldsFloatToSizeKeepingTargetDependence(t *testing.T) {
	// A known Float-to-Size result above the guaranteed portable minimum
	// remains a Size constant; the target-dependent SIZE_MAX static_assert
	// stays on the generator side, so the fold must not claim a fixed
	// width by rejecting values the placeholder range admits.
	checked := requireAccepted(t, "size: Size := 100.0.to<Size>()")
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Kind != ConstantOperand || !compilerTypes.IsSize(declaration.Source.Type) {
		t.Fatalf("size source = %#v, want a folded Size constant", declaration.Source)
	}
	if got, ok := constant.Uint64Val(declaration.Source.Constant); !ok || got != 100 {
		t.Fatalf("folded Size = %v, want 100", declaration.Source.Constant)
	}
	checked = requireAccepted(t, "size: Size := 9223372036854775808.0.to<Size>()")
	declaration = checked.Statements[0].(Declaration)
	if got, ok := constant.Uint64Val(declaration.Source.Constant); !ok || got != uint64(1)<<63 {
		t.Fatalf("folded Size = %v, want 2^63", declaration.Source.Constant)
	}
	requireDiagnostic(t, "bad: Size := 18446744073709551616.0.to<Size>()", "value 18446744073709551616 is outside the range of Size")
}

func TestCheckConversionOfKnownBindingReadStaysDynamic(t *testing.T) {
	// A read of a named immutable binding through an explicit conversion
	// keeps the binding read in the checked program.
	checked := requireAccepted(t, "value: Float64 := 2.0 converted: Int32 := value.to<Int32>()")
	converted := checked.Statements[1].(Declaration)
	if converted.Source.Kind != ExpressionOperand || converted.Source.Node.Kind != ConversionExpression {
		t.Fatalf("converted source = %#v, want a conversion expression", converted.Source)
	}
	if converted.Source.Node.Operand == nil || converted.Source.Node.Operand.Kind != VariableExpression || converted.Source.Node.Operand.Name != "value" {
		t.Fatalf("conversion operand = %#v, want a read of value", converted.Source.Node.Operand)
	}
}
