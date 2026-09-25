// constant_folding.go owns compile-time evaluation of operator
// expressions: unary and binary folding over integer, float, and boolean
// operands, widened arithmetic, and the static constant value of a checked
// expression.
package checker

import (
	"go/constant"
	gotoken "go/token"
	"math"

	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

// Constant folding classifies every constant operation under one invariant:
// go/constant computes the exact value; Hexal decides what the result type
// permits and what to call the failure. Direct computes through go/constant,
// wrapped computes through go/constant and then reduces to a fixed-width
// Hexal result, and Hexal-owned computes outside go/constant because the
// value, rule, or diagnostic is a language fact the width-free engine cannot
// express. One row per constant operation:
//
//	direct         integer literal parse and normalization (MakeFromLiteral)
//	direct         float literal parse (MakeFromLiteral)
//	direct         unary negation of an exact numeric constant (UnaryOp)
//	direct         integer arithmetic + - * / % (BinaryOp with truncated division)
//	direct         bitwise & ^ | and ~ (BinaryOp over a width-derived mask)
//	direct         shifts (Shift), beside Hexal's own shift-count range check
//	direct         widened mixed-type arithmetic at the selected common type
//	direct         integer equality and ordering (Compare)
//	direct         Boolean literal construction (MakeBool)
//	direct         Boolean equality (Compare) and Boolean constant reads (BoolVal)
//	direct         exact-to-inexact float conversion and rounding (ToFloat, Float32Val, Float64Val)
//	direct         whole-value integer conversion (ToInt)
//	direct         float-to-int truncation of exact rationals (truncateTowardZero)
//	direct         converted-integer range comparison (Compare against Hexal bounds)
//	direct         sign checks (Sign)
//	direct         integer reads feeding a Hexal range or capacity rule (Int64Val, Uint64Val)
//	wrapped        arithmetic and bitwise result reduction (wrapIntegerConstant)
//	wrapped        signed conversion reinterpretation (reduceSigned)
//	wrapped        signed minimum divided by -1, quotient and remainder
//	Hexal-owned    target widths and signed/unsigned bounds (integerBounds, constantIntegerRange)
//	Hexal-owned    shift-count range validation: go/constant Shift takes a uint count
//	Hexal-owned    float32/float64 arithmetic, comparison, and negation: IEEE rounding at the target width plus signed zero and NaN live in the checked bits, and go/constant represents none of them
//	Hexal-owned    non-finite float results kept as MakeUnknown (NaN, infinity)
//	Hexal-owned    float literal rounding and infinity rejection
//	Hexal-owned    float value reconstruction from checked bits (MakeFloat64, UnaryOp)
//	Hexal-owned    lossless common-type selection for mixed operands
//	Hexal-owned    literal contextual typing and radix metadata
//	Hexal-owned    slice range validation and Pool/Channel capacity positivity
//	Hexal-owned    conversion source/destination eligibility (conversionPairValid)
//	Hexal-owned    Boolean connective folds (!, &&, ||): the fold classifies operands by Hexal truthiness, whose domain includes Nil, eos, and always-true typed constants that carry no Boolean value
//	Hexal-owned    truthiness of non-Bool types, nil and eos singleton equality
//	Hexal-owned    starvation literal-true propagation for while-loop reachability
//	Hexal-owned    every diagnostic and its position, including division by zero and out-of-range rejection
func foldUnary(operator Operator, operand checkedExpression, operandType, resultType compilerTypes.Type, token lexer.Token, evaluate bool) checkedExpression {
	runtime := operationUnaryResult(operator, operand, operandType, resultType, token)
	if !evaluate || operand.source.Kind != ConstantOperand {
		return runtime
	}

	switch operator {
	case NegateOperator:
		if compilerTypes.IsInteger(operandType) && operand.source.Constant != nil {
			value := constant.UnaryOp(gotoken.SUB, operand.source.Constant, 0)
			minimum, maximum := integerBounds(operandType)
			if constant.Compare(value, gotoken.LSS, minimum) || constant.Compare(value, gotoken.GTR, maximum) {
				return checkedExpression{typ: resultType, token: token, diagnostic: valueOutOfRangeDiagnostic(token, operandType)}
			}
			return foldedIntegerResult(value, resultType, token)
		}
		if compilerTypes.IsFloat(operandType) {
			bits := operand.source.FloatBits
			if compilerTypes.Equal(operandType, compilerTypes.Float32) {
				bits ^= uint64(1) << 31
			} else {
				bits ^= uint64(1) << 63
			}
			return foldedFloatResult(resultType, bits, token)
		}
	case LogicalNotOperator:
		if value, known := knownTruthiness(operand); known {
			return foldedBoolResult(!value, token)
		}
	case BitwiseNotOperator:
		// Complement inverts every bit of the fixed-width
		// representation and reconstructs the signed value when needed.
		if compilerTypes.IsInteger(operandType) && operand.source.Constant != nil {
			width := uint(operandType.Bits)
			mask := constant.MakeUint64(0)
			if width >= 64 {
				mask = constant.MakeUint64(^uint64(0))
			} else {
				mask = constant.MakeUint64(uint64(1)<<width - 1)
			}
			value := constant.BinaryOp(operand.source.Constant, gotoken.XOR, mask)
			value = wrapIntegerConstant(value, operandType)
			return foldedIntegerResult(value, resultType, token)
		}
	}
	return runtime
}

func foldBinary(operator Operator, left, right checkedExpression, operandType, resultType compilerTypes.Type, token lexer.Token, evaluate bool) checkedExpression {
	runtime := operationBinaryResult(operator, left, right, operandType, resultType, token)
	if !evaluate {
		return runtime
	}
	if diagnostic := staticDivisionDiagnostic(operator, left, right, operandType, token); diagnostic != nil {
		return checkedExpression{typ: resultType, token: token, diagnostic: diagnostic}
	}

	if operator == LogicalAndOperator || operator == LogicalOrOperator {
		if leftValue, known := knownTruthiness(left); known {
			if operator == LogicalAndOperator && !leftValue {
				return foldedBoolResult(false, token)
			}
			if operator == LogicalOrOperator && leftValue {
				return foldedBoolResult(true, token)
			}
			if rightValue, known := knownTruthiness(right); known {
				return foldedBoolResult(rightValue, token)
			}
		}
		return runtime
	}

	if left.source.Kind != ConstantOperand || right.source.Kind != ConstantOperand {
		return runtime
	}

	switch {
	case compilerTypes.IsInteger(operandType) && (isIntegerArithmetic(operator) || isBitwiseArithmetic(operator) || isShiftOperator(operator)):
		operation, ok := integerConstantOperator(operator)
		if !ok || left.source.Constant == nil || right.source.Constant == nil {
			return runtime
		}
		value := left.source.Constant
		if isShiftOperator(operator) {
			// go/constant shifts require a uint count; the checker
			// validated the count range before folding.
			countValue, _ := constant.Uint64Val(right.source.Constant)
			value = constant.Shift(left.source.Constant, operation, uint(countValue))
		} else {
			value = constant.BinaryOp(left.source.Constant, operation, right.source.Constant)
		}
		// Integer arithmetic wraps to the result type; the
		// signed-minimum/-1 division and remainder pairs fold to their
		// defined values.
		if operator == DivideOperator || operator == RemainderOperator {
			if compilerTypes.IsSignedInteger(operandType) {
				minimum, _ := integerBounds(operandType)
				if constant.Compare(left.source.Constant, gotoken.EQL, minimum) &&
					constant.Compare(right.source.Constant, gotoken.EQL, constant.MakeInt64(-1)) {
					if operator == RemainderOperator {
						value = constant.MakeInt64(0)
					} else {
						value = minimum
					}
				}
			}
		}
		value = wrapIntegerConstant(value, operandType)
		return foldedIntegerResult(value, resultType, token)
	case compilerTypes.IsFloat(operandType) && isFloatArithmetic(operator):
		return foldedFloatResultFromBinary(operator, left.source, right.source, resultType, token)
	case operator == EqualOperator || operator == NotEqualOperator ||
		operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator:
		value, ok := compareConstantOperands(operator, left.source, right.source, operandType)
		if ok {
			return foldedBoolResult(value, token)
		}
	}
	return runtime
}

func operationUnaryResult(operator Operator, operand checkedExpression, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	node := operationUnaryNode(operator, expressionNode(operand.source), operandType, resultType)
	source := Operand{Kind: ExpressionOperand, Type: resultType, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

func operationBinaryResult(operator Operator, left, right checkedExpression, operandType, resultType compilerTypes.Type, token lexer.Token) checkedExpression {
	node := operationBinaryNode(operator, expressionNode(left.source), expressionNode(right.source), operandType, resultType)
	source := Operand{Kind: ExpressionOperand, Type: resultType, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

func foldedIntegerResult(value constant.Value, typ compilerTypes.Type, token lexer.Token) checkedExpression {
	source := constantOperand(typ, value, value.ExactString())
	source.Negative = constant.Sign(value) < 0
	if compilerTypes.IsFloat(typ) {
		// A folded float result keeps its rounded IEEE bits so a later
		// explicit conversion reasons from the rounded value rather than
		// the exact integer the fold keeps.
		if compilerTypes.Equal(typ, compilerTypes.Float32) {
			converted, _ := constant.Float32Val(value)
			source.FloatBits = uint64(math.Float32bits(converted))
		} else {
			converted, _ := constant.Float64Val(value)
			source.FloatBits = math.Float64bits(converted)
		}
	}
	source.Node = constantNode(source)
	known := source
	return checkedExpression{source: source, typ: typ, token: token, known: &known}
}

// wrapIntegerConstant reduces an exact integer result to the result type's
// range using the defined two's-complement-style wrapping rule.
func wrapIntegerConstant(value constant.Value, typ compilerTypes.Type) constant.Value {
	minimum, maximum := integerBounds(typ)
	if constant.Compare(value, gotoken.GEQ, minimum) && constant.Compare(value, gotoken.LEQ, maximum) {
		return value
	}
	width := uint(typ.Bits)
	modulus := constant.MakeUint64(0)
	if width >= 64 {
		modulus = constant.MakeUint64(^uint64(0))
	} else {
		modulus = constant.MakeUint64(uint64(1) << width)
	}
	// Reduce modulo 2^n into [0, 2^n).
	reduced := constant.BinaryOp(value, gotoken.REM, modulus)
	if constant.Compare(reduced, gotoken.LSS, constant.MakeInt64(0)) {
		reduced = constant.BinaryOp(reduced, gotoken.ADD, modulus)
	}
	if !compilerTypes.IsSignedInteger(typ) {
		return reduced
	}
	half := constant.MakeUint64(0)
	if width >= 64 {
		half = constant.MakeUint64(uint64(1) << 63)
	} else {
		half = constant.MakeUint64(uint64(1) << (width - 1))
	}
	if constant.Compare(reduced, gotoken.LSS, half) {
		return reduced
	}
	return constant.BinaryOp(reduced, gotoken.SUB, modulus)
}

// foldWidenedArithmetic folds a mixed-type constant arithmetic operation
// after selecting the common numeric type. Only genuine literal constants
// fold; a read of a named immutable binding stays a runtime operation while
// its known-value metadata still feeds the division-by-zero diagnostic.
func foldWidenedArithmetic(operator Operator, left, right checkedExpression, common compilerTypes.Type, token lexer.Token) checkedExpression {
	if operator == DivideOperator || operator == RemainderOperator {
		if diagnostic := staticDivisionDiagnostic(operator, left, right, common, token); diagnostic != nil {
			return checkedExpression{typ: common, token: token, diagnostic: diagnostic}
		}
	}
	operation, ok := integerConstantOperator(operator)
	if !ok || left.source.Constant == nil || right.source.Constant == nil {
		diagnostic := unknownAt(token, "unfoldable widened arithmetic")
		return checkedExpression{typ: common, token: token, diagnostic: &diagnostic}
	}
	value := constant.BinaryOp(left.source.Constant, operation, right.source.Constant)
	if operator == DivideOperator || operator == RemainderOperator && compilerTypes.IsSignedInteger(common) {
		minimum, _ := integerBounds(common)
		if constant.Compare(left.source.Constant, gotoken.EQL, minimum) && constant.Compare(right.source.Constant, gotoken.EQL, constant.MakeInt64(-1)) {
			if operator == RemainderOperator {
				value = constant.MakeInt64(0)
			} else {
				value = minimum
			}
		}
	}
	value = wrapIntegerConstant(value, common)
	return foldedIntegerResult(value, common, token)
}

func foldedBoolResult(value bool, token lexer.Token) checkedExpression {
	literal := "false"
	if value {
		literal = "true"
	}
	source := constantOperand(compilerTypes.Bool, constant.MakeBool(value), literal)
	source.Node = constantNode(source)
	known := source
	return checkedExpression{source: source, typ: compilerTypes.Bool, token: token, known: &known}
}

func foldedFloatResult(typ compilerTypes.Type, bits uint64, token lexer.Token) checkedExpression {
	var value constant.Value
	var negative bool
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		floatValue := math.Float32frombits(uint32(bits))
		negative = math.Signbit(float64(floatValue))
		if math.IsNaN(float64(floatValue)) || math.IsInf(float64(floatValue), 0) {
			value = constant.MakeUnknown()
		} else {
			value = constant.MakeFloat64(float64(floatValue))
		}
	} else {
		floatValue := math.Float64frombits(bits)
		negative = math.Signbit(floatValue)
		if math.IsNaN(floatValue) || math.IsInf(floatValue, 0) {
			value = constant.MakeUnknown()
		} else {
			value = constant.MakeFloat64(floatValue)
		}
	}
	source := constantOperand(typ, value, "")
	source.FloatBits = bits
	source.Negative = negative
	source.Node = constantNode(source)
	known := source
	return checkedExpression{source: source, typ: typ, token: token, known: &known}
}

func foldedFloatResultFromBinary(operator Operator, left, right Operand, typ compilerTypes.Type, token lexer.Token) checkedExpression {
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		leftValue := math.Float32frombits(uint32(left.FloatBits))
		rightValue := math.Float32frombits(uint32(right.FloatBits))
		var result float32
		switch operator {
		case AddOperator:
			result = leftValue + rightValue
		case SubtractOperator:
			result = leftValue - rightValue
		case MultiplyOperator:
			result = leftValue * rightValue
		case DivideOperator:
			result = leftValue / rightValue
		default:
			return checkedExpression{source: Operand{Kind: ExpressionOperand, Type: typ}, typ: typ, token: token}
		}
		return foldedFloatResult(typ, uint64(math.Float32bits(result)), token)
	}

	leftValue := math.Float64frombits(left.FloatBits)
	rightValue := math.Float64frombits(right.FloatBits)
	var result float64
	switch operator {
	case AddOperator:
		result = leftValue + rightValue
	case SubtractOperator:
		result = leftValue - rightValue
	case MultiplyOperator:
		result = leftValue * rightValue
	case DivideOperator:
		result = leftValue / rightValue
	default:
		return checkedExpression{source: Operand{Kind: ExpressionOperand, Type: typ}, typ: typ, token: token}
	}
	return foldedFloatResult(typ, math.Float64bits(result), token)
}

// staticConstantValue returns the compile-time value a constant-required
// diagnostic may rely on for a checked expression: a true literal constant,
// or the known-value metadata of a named immutable binding read. The read
// itself stays in the checked program; only the diagnostic consumes the
// metadata.
func staticConstantValue(expression checkedExpression) constant.Value {
	if expression.source.Kind == ConstantOperand && expression.source.Constant != nil {
		return expression.source.Constant
	}
	if expression.known != nil && expression.known.Kind == ConstantOperand && expression.known.Constant != nil {
		return expression.known.Constant
	}
	return nil
}
