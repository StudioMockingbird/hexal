package checker

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func checkUnaryExpression(expression parser.UnaryExpression, context expressionContext, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	operator, ok := operatorFromToken(expression.Operator)
	if expression.Operator.Kind == lexer.Minus {
		operator = NegateOperator
		ok = true
	}
	if !ok {
		return unsupportedOperatorExpression(expression.Operator)
	}

	hint := inferExpressionType(expression.Operand, operandContextType(operator, context.expected.Type), names, typeEnvironment)
	if hint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: hint.diagnostic}
	}
	operandType := hint.typ
	if hint.contextual {
		if expected := operandContextType(operator, context.expected.Type); expected.Name != "" {
			operandType = expected
		}
	}
	operand := checkExpression(expression.Operand, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: context.foldConstants}, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(operand); len(diagnostics) > 0 {
		return checkedExpression{token: expression.Operator, diagnostics: diagnostics}
	}
	if names.generics != nil && names.generics.open && compilerTypes.ContainsTypeParameter(operand.typ) {
		// An operation whose validity depends on a substituted type
		// is deferred during open generic checking and rechecked at
		// specialization with concrete types.
		return operationUnaryResult(operator, operand, operand.typ, operand.typ, expression.Operator)
	}
	if !operatorAllowsType(operator, operand.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: unaryOperatorDiagnostic(operator, operand.typ, expression.Operator),
		}
	}

	resultType := operand.typ
	if operator == LogicalNotOperator {
		resultType = compilerTypes.Bool
	}
	return foldUnary(operator, operand, operand.typ, resultType, expression.Operator, context.foldConstants)
}

func checkBinaryExpression(expression parser.BinaryExpression, context expressionContext, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	operator, ok := operatorFromToken(expression.Operator)
	if !ok || operator == NegateOperator || operator == LogicalNotOperator {
		return unsupportedOperatorExpression(expression.Operator)
	}

	expected := operandContextType(operator, context.expected.Type)
	leftHint := inferExpressionType(expression.Left, expected, names, typeEnvironment)
	rightHint := inferExpressionType(expression.Right, expected, names, typeEnvironment)
	if leftHint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: leftHint.diagnostic}
	}
	if rightHint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: rightHint.diagnostic}
	}
	operandType := binaryOperandType(operator, expected, leftHint, rightHint)
	left := checkExpression(expression.Left, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: context.foldConstants}, names, typeEnvironment)
	rightEvaluation := context.foldConstants
	if rightEvaluation && (operator == LogicalAndOperator || operator == LogicalOrOperator) {
		if leftValue, known := knownTruthinessMetadata(left); known {
			rightEvaluation = (operator == LogicalAndOperator && leftValue) || (operator == LogicalOrOperator && !leftValue)
		}
	}
	right := checkExpression(expression.Right, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: rightEvaluation}, names, typeEnvironment)
	diagnostics := append(initializerDiagnostics(left), initializerDiagnostics(right)...)
	if len(diagnostics) > 0 {
		return checkedExpression{token: expression.Operator, diagnostics: diagnostics}
	}

	// Null tests own the == and != pairs that mention Nil: a null
	// test yields Bool, while pairs without a Nil side stay with ordinary
	// scalar equality below.
	if operator == EqualOperator || operator == NotEqualOperator {
		// EoS is a singleton, so eos == eos is provably true and
		// eos != eos is provably false, matching nil == nil.
		if compilerTypes.IsEoS(left.typ) && compilerTypes.IsEoS(right.typ) {
			result := foldedBoolResult(operator == EqualOperator, expression.Operator)
			return result
		}
		if result := checkNullTest(operator, left, right, expression.Operator); result != nil {
			return *result
		}
		if result := checkUnionEquality(operator, left, right, expression.Operator); result != nil {
			return *result
		}
	}

	if names.generics != nil && names.generics.open &&
		(compilerTypes.ContainsTypeParameter(left.typ) || compilerTypes.ContainsTypeParameter(right.typ)) {
		// An operation whose validity depends on a substituted type
		// is deferred during open generic checking and rechecked at
		// specialization with concrete types.
		resultType := left.typ
		if operator == EqualOperator || operator == NotEqualOperator ||
			operator == LessOperator || operator == LessEqualOperator ||
			operator == GreaterOperator || operator == GreaterEqualOperator ||
			operator == LogicalAndOperator || operator == LogicalOrOperator {
			resultType = compilerTypes.Bool
		}
		return operationBinaryResult(operator, left, right, left.typ, resultType, expression.Operator)
	}

	// Rune is not an arithmetic operand. Reject any binary
	// arithmetic with a Rune operand before folding or common-type selection,
	// owning same-type and mixed cases with one diagnostic at the operator.
	if (operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator ||
		operator == DivideOperator || operator == RemainderOperator) &&
		(compilerTypes.IsRune(left.typ) || compilerTypes.IsRune(right.typ)) {
		diagnostic := typeErrorAt(expression.Operator, fmt.Sprintf("operator %s requires numeric operands; got %s and %s", operator, left.typ.Name, right.typ.Name))
		return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
	}

	// Mixed numeric arithmetic selects the unique least
	// lossless common type before the operation; the result has that type
	// and wraps at it.
	if (operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator ||
		operator == DivideOperator || operator == RemainderOperator) &&
		(compilerTypes.IsInteger(left.typ) || compilerTypes.IsFloat(left.typ)) &&
		(compilerTypes.IsInteger(right.typ) || compilerTypes.IsFloat(right.typ)) &&
		!compilerTypes.Equal(left.typ, right.typ) {
		common, ok := compilerTypes.LosslessCommonType(left.typ, right.typ)
		if !ok {
			diagnostic := typeErrorAt(expression.Operator, "numeric values have no unique lossless common type")
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			return foldWidenedArithmetic(operator, left, right, common, expression.Operator)
		}
		leftNode := widenNode(expressionNode(left.source), left.typ, common)
		rightNode := widenNode(expressionNode(right.source), right.typ, common)
		if operator == DivideOperator || operator == RemainderOperator {
			if diagnostic := staticDivisionDiagnostic(operator, left, right, common, expression.Operator); diagnostic != nil {
				return checkedExpression{typ: common, token: expression.Operator, diagnostic: diagnostic}
			}
		}
		node := operationBinaryNode(operator, leftNode, rightNode, common, common)
		source := Operand{Kind: ExpressionOperand, Type: common, Node: node}
		return checkedExpression{source: source, typ: common, token: expression.Operator}
	}

	// Bitwise &, ^, and | use the unique least lossless common
	// integer type; the operation happens at that exact width and wraps at
	// it.
	if isBitwiseArithmetic(operator) {
		if !isBitwiseEligible(left.typ) || !isBitwiseEligible(right.typ) {
			diagnostic := typeErrorAt(expression.Operator, fmt.Sprintf("operator %s requires integer operands; got %s and %s", operator, left.typ.Name, right.typ.Name))
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		common := left.typ
		if !compilerTypes.Equal(left.typ, right.typ) {
			var ok bool
			common, ok = compilerTypes.LosslessCommonType(left.typ, right.typ)
			if !ok {
				diagnostic := typeErrorAt(expression.Operator, "integer operands have no unique lossless common type")
				return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
			}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			return foldWidenedArithmetic(operator, left, right, common, expression.Operator)
		}
		leftNode := widenNode(expressionNode(left.source), left.typ, common)
		rightNode := widenNode(expressionNode(right.source), right.typ, common)
		node := operationBinaryNode(operator, leftNode, rightNode, common, common)
		source := Operand{Kind: ExpressionOperand, Type: common, Node: node}
		return checkedExpression{source: source, typ: common, token: expression.Operator}
	}

	// Shifts preserve the left operand's type; the count is any
	// integer and never participates in common-type selection.
	if isShiftOperator(operator) {
		if !isBitwiseEligible(left.typ) {
			diagnostic := typeErrorAt(expression.Operator, fmt.Sprintf("operator %s requires an integer left operand; got %s", operator, left.typ.Name))
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		if !compilerTypes.IsInteger(right.typ) {
			diagnostic := typeErrorAt(expression.Operator, "shift count must be an integer; got "+right.typ.Name)
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		if count := staticConstantValue(right); count != nil && count.Kind() == constant.Int {
			value, exact := constant.Int64Val(count)
			if exact && (value < 0 || value >= int64(left.typ.Bits)) {
				diagnostic := typeErrorAt(expression.Operator, fmt.Sprintf("shift count %d is outside the valid range for %s", value, left.typ.Name))
				return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
			}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			operation, ok := integerConstantOperator(operator)
			if ok && left.source.Constant != nil && right.source.Constant != nil {
				// go/constant shifts require a uint count; the range
				// validation above guarantees the value fits.
				countValue, _ := constant.Uint64Val(right.source.Constant)
				value := constant.Shift(left.source.Constant, operation, uint(countValue))
				value = wrapIntegerConstant(value, left.typ)
				return foldedIntegerResult(value, left.typ, expression.Operator)
			}
		}
		node := operationBinaryNode(operator, expressionNode(left.source), expressionNode(right.source), left.typ, left.typ)
		source := Operand{Kind: ExpressionOperand, Type: left.typ, Node: node}
		return checkedExpression{source: source, typ: left.typ, token: expression.Operator}
	}

	// Equality and ordering resolve through the lossless numeric
	// widening and the recursive deep-comparison rules before the ordinary
	// identical-scalar path.
	if operator == EqualOperator || operator == NotEqualOperator ||
		operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator {
		if result := checkDeepComparison(operator, left, right, expression.Operator, names); result != nil {
			return *result
		}
	}

	if !operatorAllowsType(operator, left.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: binaryOperatorDiagnostic(operator, left.typ, expression.Operator),
		}
	}
	if !operatorAllowsType(operator, right.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: binaryOperatorDiagnostic(operator, right.typ, expression.Operator),
		}
	}
	if !compilerTypes.Equal(left.typ, right.typ) && operator != LogicalAndOperator && operator != LogicalOrOperator {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: diagnosticAt(typeErrorAt(expression.Operator, fmt.Sprintf("operator %s requires identical operand types; got %s and %s", operator, left.typ.Name, right.typ.Name))),
		}
	}
	resultType := left.typ
	if operator == EqualOperator || operator == NotEqualOperator ||
		operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator ||
		operator == LogicalAndOperator || operator == LogicalOrOperator {
		resultType = compilerTypes.Bool
	}
	return foldBinary(operator, left, right, left.typ, resultType, expression.Operator, context.foldConstants)
}

// checkNullTest resolves null tests: == and != accept exactly the
// operand pairs where one side is Nil and the other is any union containing
// Nil. The result is Bool, and the checked node is normalized so the union
// operand always sits in the node's Operand slot. Pairs without a Nil side
// return nil so ordinary equality keeps its own rules.
func checkNullTest(operator Operator, left, right checkedExpression, token lexer.Token) *checkedExpression {
	if !compilerTypes.IsNil(left.typ) && !compilerTypes.IsNil(right.typ) {
		return nil
	}
	var operand checkedExpression
	switch {
	case compilerTypes.IsNil(left.typ) && compilerTypes.IsNil(right.typ):
		// Nil is a singleton, so nil == nil is provably true and nil != nil
		// is provably false. This is the only folded null-test case; a test
		// against a nullable operand always stays runtime.
		result := foldedBoolResult(operator == EqualOperator, token)
		return &result
	case compilerTypes.IsEoS(left.typ) && compilerTypes.IsEoS(right.typ):
		// EoS is a singleton, so eos == eos is provably true and
		// eos != eos is provably false, matching nil == nil.
		result := foldedBoolResult(operator == EqualOperator, token)
		return &result
	case compilerTypes.IsNil(left.typ):
		operand = right
	default:
		operand = left
	}
	if !compilerTypes.IsUnion(operand.typ) || !compilerTypes.ContainsUnionMember(operand.typ, compilerTypes.Nil) {
		// The operand's type can never hold Nil, which makes the test a
		// constant result; the diagnostic names that reason instead of
		// rejecting the test outright.
		verdict := "true"
		if operator == EqualOperator {
			verdict = "false"
		}
		diagnostic := typeErrorAt(token, fmt.Sprintf("%s is never Nil; the test is always %s", operand.typ.Name, verdict))
		return &checkedExpression{token: token, diagnostic: &diagnostic}
	}
	operandNode := expressionNode(operand.source)
	node := Expression{
		Kind:        NullTestExpression,
		Operand:     &operandNode,
		Operator:    operator,
		OperandType: operand.typ,
		ResultType:  compilerTypes.Bool,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
	return &checkedExpression{source: source, typ: compilerTypes.Bool, token: token}
}

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
		return checkedExpression{typ: common, token: token, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "checker",
			Message:  "unfoldable widened arithmetic",
		}}
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

// knownTruthinessMetadata extends knownTruthiness to the known-value
// metadata of a named immutable binding read. The read stays in the checked
// program; only compile-time short-circuit reasoning (whether the other
// side is reachable) consults the metadata.
func knownTruthinessMetadata(expression checkedExpression) (bool, bool) {
	if expression.known == nil {
		return false, false
	}
	switch compilerTypes.Truthiness(expression.typ) {
	case compilerTypes.TruthinessBool:
		if expression.known.Constant == nil || expression.known.Constant.Kind() != constant.Bool {
			return false, false
		}
		return constant.BoolVal(expression.known.Constant), true
	case compilerTypes.TruthinessAlwaysTrue:
		return true, true
	case compilerTypes.TruthinessNil:
		return false, true
	}
	return false, false
}

// knownTruthiness reports whether the operand's truthiness is decided at
// compile time: a constant Bool carries its value, nil is falsey,
// and a constant of an always-truthy type is truthy. Non-constant operands
// are never folded here: their evaluation must survive in the checked AST.
func knownTruthiness(expression checkedExpression) (bool, bool) {
	if expression.source.Kind != ConstantOperand {
		return false, false
	}
	switch compilerTypes.Truthiness(expression.typ) {
	case compilerTypes.TruthinessBool:
		if expression.source.Constant == nil || expression.source.Constant.Kind() != constant.Bool {
			return false, false
		}
		return constant.BoolVal(expression.source.Constant), true
	case compilerTypes.TruthinessAlwaysTrue:
		return true, true
	case compilerTypes.TruthinessNil:
		return false, true
	}
	return false, false
}

func isIntegerArithmetic(operator Operator) bool {
	return operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator || operator == DivideOperator || operator == RemainderOperator
}

func isBitwiseArithmetic(operator Operator) bool {
	return operator == BitwiseAndOperator || operator == BitwiseXorOperator || operator == BitwiseOrOperator
}

func isShiftOperator(operator Operator) bool {
	return operator == ShiftLeftOperator || operator == ShiftRightOperator
}

func isFloatArithmetic(operator Operator) bool {
	return operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator || operator == DivideOperator
}

func integerConstantOperator(operator Operator) (gotoken.Token, bool) {
	switch operator {
	case AddOperator:
		return gotoken.ADD, true
	case SubtractOperator:
		return gotoken.SUB, true
	case MultiplyOperator:
		return gotoken.MUL, true
	case DivideOperator:
		return gotoken.QUO_ASSIGN, true
	case RemainderOperator:
		return gotoken.REM, true
	case BitwiseAndOperator:
		return gotoken.AND, true
	case BitwiseXorOperator:
		return gotoken.XOR, true
	case BitwiseOrOperator:
		return gotoken.OR, true
	case ShiftLeftOperator:
		return gotoken.SHL, true
	case ShiftRightOperator:
		return gotoken.SHR, true
	default:
		return gotoken.ILLEGAL, false
	}
}

func compareConstantOperands(operator Operator, left, right Operand, typ compilerTypes.Type) (bool, bool) {
	if compilerTypes.Equal(typ, compilerTypes.Bool) {
		if left.Constant == nil || right.Constant == nil || left.Constant.Kind() != constant.Bool || right.Constant.Kind() != constant.Bool {
			return false, false
		}
		leftValue := constant.BoolVal(left.Constant)
		rightValue := constant.BoolVal(right.Constant)
		switch operator {
		case EqualOperator:
			return leftValue == rightValue, true
		case NotEqualOperator:
			return leftValue != rightValue, true
		default:
			return false, false
		}
	}
	if compilerTypes.IsFloat(typ) {
		return compareFloatOperands(operator, left.FloatBits, right.FloatBits, typ), true
	}
	if !compilerTypes.IsInteger(typ) || left.Constant == nil || right.Constant == nil {
		return false, false
	}
	comparison := func(token gotoken.Token) bool {
		return constant.Compare(left.Constant, token, right.Constant)
	}
	switch operator {
	case EqualOperator:
		return comparison(gotoken.EQL), true
	case NotEqualOperator:
		return comparison(gotoken.NEQ), true
	case LessOperator:
		return comparison(gotoken.LSS), true
	case LessEqualOperator:
		return comparison(gotoken.LEQ), true
	case GreaterOperator:
		return comparison(gotoken.GTR), true
	case GreaterEqualOperator:
		return comparison(gotoken.GEQ), true
	default:
		return false, false
	}
}

func compareFloatOperands(operator Operator, leftBits, rightBits uint64, typ compilerTypes.Type) bool {
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		left := math.Float32frombits(uint32(leftBits))
		right := math.Float32frombits(uint32(rightBits))
		switch operator {
		case EqualOperator:
			return left == right
		case NotEqualOperator:
			return left != right
		case LessOperator:
			return left < right
		case LessEqualOperator:
			return left <= right
		case GreaterOperator:
			return left > right
		case GreaterEqualOperator:
			return left >= right
		}
		return false
	}
	left := math.Float64frombits(leftBits)
	right := math.Float64frombits(rightBits)
	switch operator {
	case EqualOperator:
		return left == right
	case NotEqualOperator:
		return left != right
	case LessOperator:
		return left < right
	case LessEqualOperator:
		return left <= right
	case GreaterOperator:
		return left > right
	case GreaterEqualOperator:
		return left >= right
	default:
		return false
	}
}

func staticDivisionDiagnostic(operator Operator, left, right checkedExpression, operandType compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	if (operator != DivideOperator && operator != RemainderOperator) || !compilerTypes.IsInteger(operandType) {
		return nil
	}
	divisor := staticConstantValue(right)
	if divisor == nil || divisor.Kind() == constant.Unknown {
		return nil
	}
	if constant.Sign(divisor) == 0 {
		return diagnosticAt(typeErrorAt(token, "division by zero"))
	}
	// Signed minimum divided by -1 wraps to the signed minimum and the
	// remainder is zero, both at compile time and at runtime.
	return nil
}

func inferExpressionType(expression parser.Expression, expected compilerTypes.Type, names *scope, typeEnvironment *compilerTypes.Environment) expressionTypeHint {
	switch expression := expression.(type) {
	case parser.IntegerLiteral:
		return expressionTypeHint{typ: contextualIntegerType(expected), contextual: true, token: expression.Token}
	case parser.DecimalLiteral:
		return expressionTypeHint{typ: contextualFloatType(expected), contextual: true, token: expression.Token}
	case parser.NegatedNumericLiteral:
		return expressionTypeHint{typ: negatedLiteralType(expression, expected), contextual: true, token: expression.Minus}
	case parser.BooleanLiteral:
		return expressionTypeHint{typ: compilerTypes.Bool, token: expression.Token}
	case parser.NilLiteral:
		return expressionTypeHint{typ: compilerTypes.Nil, token: expression.Token}
	case parser.EosLiteral:
		return expressionTypeHint{typ: compilerTypes.EoS, token: expression.Token}
	case parser.StringLiteral:
		// A literal in an expression position is String unless the
		// context demands a Strand.
		typ := compilerTypes.StringType
		if compilerTypes.IsStrand(expected) {
			typ = compilerTypes.StrandType
		}
		return expressionTypeHint{typ: typ, token: expression.Token}
	case parser.ByteLiteral:
		return expressionTypeHint{typ: compilerTypes.UInt8, token: expression.Token}
	case parser.RuneLiteral:
		return expressionTypeHint{typ: compilerTypes.Rune, token: expression.Token}
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		place := checkPlace(expression, names, typeEnvironment)
		return expressionTypeHint{typ: place.typ, token: place.token, diagnostic: place.diagnostic}
	case parser.ObjectLiteral:
		typ, ok := typeEnvironment.Lookup(expression.TypeName.Lexeme)
		if !ok {
			return expressionTypeHint{token: expression.TypeName, diagnostic: diagnosticAt(typeErrorAt(expression.TypeName, "unknown type "+expression.TypeName.Lexeme))}
		}
		return expressionTypeHint{typ: typ, token: expression.TypeName}
	case parser.RefExpression:
		checked := checkReference(expression, names, typeEnvironment)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.CallExpression:
		checked := checkCallValue(expression, names, typeEnvironment)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.UnaryExpression:
		operator, ok := operatorFromToken(expression.Operator)
		if expression.Operator.Kind == lexer.Minus {
			operator = NegateOperator
			ok = true
		}
		if !ok {
			return expressionTypeHint{token: expression.Operator, diagnostic: unsupportedOperatorDiagnostic(expression.Operator)}
		}
		hint := inferExpressionType(expression.Operand, operandContextType(operator, expected), names, typeEnvironment)
		if hint.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: hint.diagnostic}
		}
		return expressionTypeHint{
			typ:        unaryResultType(operator, hint.typ),
			contextual: hint.contextual,
			token:      expression.Operator,
		}
	case parser.SpawnExpression:
		checked := checkSpawnExpression(expression, names, typeEnvironment)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.TryExpression:
		// The try's true result is its checked success type; for literal
		// contextual typing the operand's hint is the closest estimate.
		hint := inferExpressionType(expression.Operand, expected, names, typeEnvironment)
		if hint.diagnostic != nil {
			return expressionTypeHint{token: expression.Keyword, diagnostic: hint.diagnostic}
		}
		return expressionTypeHint{typ: hint.typ, token: expression.Keyword}
	case parser.BinaryExpression:
		operator, ok := operatorFromToken(expression.Operator)
		if !ok || operator == NegateOperator || operator == LogicalNotOperator {
			return expressionTypeHint{token: expression.Operator, diagnostic: unsupportedOperatorDiagnostic(expression.Operator)}
		}
		operandExpected := operandContextType(operator, expected)
		left := inferExpressionType(expression.Left, operandExpected, names, typeEnvironment)
		right := inferExpressionType(expression.Right, operandExpected, names, typeEnvironment)
		if left.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: left.diagnostic}
		}
		if right.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: right.diagnostic}
		}
		operandType := binaryOperandType(operator, operandExpected, left, right)
		return expressionTypeHint{
			typ:        binaryResultType(operator, operandType),
			contextual: left.contextual && right.contextual,
			token:      expression.Operator,
		}
	default:
		return expressionTypeHint{diagnostic: diagnosticAt(unknownAt(lexer.Token{Line: 1, Column: 1}, "unsupported expression"))}
	}
}

func operatorFromToken(token lexer.Token) (Operator, bool) {
	switch token.Kind {
	case lexer.Minus:
		return SubtractOperator, true
	case lexer.Plus:
		return AddOperator, true
	case lexer.Star:
		return MultiplyOperator, true
	case lexer.Slash:
		return DivideOperator, true
	case lexer.Percent:
		return RemainderOperator, true
	case lexer.EqualEqual:
		return EqualOperator, true
	case lexer.BangEqual:
		return NotEqualOperator, true
	case lexer.Less:
		return LessOperator, true
	case lexer.LessEqual:
		return LessEqualOperator, true
	case lexer.Greater:
		return GreaterOperator, true
	case lexer.GreaterEqual:
		return GreaterEqualOperator, true
	case lexer.And:
		return LogicalAndOperator, true
	case lexer.Or:
		return LogicalOrOperator, true
	case lexer.Bang:
		return LogicalNotOperator, true
	case lexer.Tilde:
		return BitwiseNotOperator, true
	case lexer.Amp:
		return BitwiseAndOperator, true
	case lexer.Caret:
		return BitwiseXorOperator, true
	case lexer.Pipe:
		return BitwiseOrOperator, true
	case lexer.ShiftLeft:
		return ShiftLeftOperator, true
	case lexer.ShiftRight:
		return ShiftRightOperator, true
	default:
		return InvalidOperator, false
	}
}

func operatorAllowsType(operator Operator, typ compilerTypes.Type) bool {
	switch operator {
	case AddOperator, SubtractOperator, MultiplyOperator, DivideOperator:
		return compilerTypes.IsInteger(typ) || compilerTypes.IsFloat(typ)
	case RemainderOperator:
		return compilerTypes.IsInteger(typ)
	case BitwiseAndOperator, BitwiseXorOperator, BitwiseOrOperator:
		return isBitwiseEligible(typ)
	case ShiftLeftOperator, ShiftRightOperator:
		return isBitwiseEligible(typ)
	case BitwiseNotOperator:
		return isBitwiseEligible(typ)
	case EqualOperator, NotEqualOperator:
		return typ.ScalarKind != compilerTypes.ScalarNone
	case LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator:
		return compilerTypes.IsInteger(typ) || compilerTypes.IsFloat(typ)
	case LogicalAndOperator, LogicalOrOperator, LogicalNotOperator:
		// Any value-producing operand is allowed; truthiness is
		// contextual, never a Bool requirement.
		return true
	case NegateOperator:
		return compilerTypes.IsSignedInteger(typ) || compilerTypes.IsFloat(typ)
	default:
		return false
	}
}

// isBitwiseEligible reports whether typ may participate in bitwise
// and shift operations: a fixed-width integer or Size, excluding Rune.
func isBitwiseEligible(typ compilerTypes.Type) bool {
	return compilerTypes.IsInteger(typ) && !compilerTypes.IsRune(typ)
}

func operandContextType(operator Operator, expected compilerTypes.Type) compilerTypes.Type {
	if expected.Name == "" {
		return compilerTypes.Type{}
	}
	switch operator {
	case EqualOperator, NotEqualOperator, LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator, LogicalAndOperator, LogicalOrOperator, LogicalNotOperator:
		// A result Bool is not a numeric operand context, and logical
		// operands are truthiness contexts. Nested arithmetic therefore keeps
		// its fallback literal type instead of becoming Bool.
		return compilerTypes.Type{}
	default:
		if operatorAllowsType(operator, expected) {
			return expected
		}
		return compilerTypes.Type{}
	}
}

func binaryOperandType(operator Operator, expected compilerTypes.Type, left, right expressionTypeHint) compilerTypes.Type {
	if left.contextual && !right.contextual && operatorAllowsType(operator, right.typ) {
		return right.typ
	}
	if right.contextual && !left.contextual && operatorAllowsType(operator, left.typ) {
		return left.typ
	}
	if left.contextual && right.contextual && operatorAllowsType(operator, expected) {
		return expected
	}
	return left.typ
}

func unaryResultType(operator Operator, operandType compilerTypes.Type) compilerTypes.Type {
	if operator == LogicalNotOperator {
		return compilerTypes.Bool
	}
	return operandType
}

func binaryResultType(operator Operator, operandType compilerTypes.Type) compilerTypes.Type {
	switch operator {
	case EqualOperator, NotEqualOperator, LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator, LogicalAndOperator, LogicalOrOperator:
		return compilerTypes.Bool
	default:
		return operandType
	}
}

func negatedLiteralType(expression parser.NegatedNumericLiteral, expected compilerTypes.Type) compilerTypes.Type {
	switch expression.Literal.(type) {
	case parser.IntegerLiteral:
		return contextualIntegerType(expected)
	case parser.DecimalLiteral:
		return contextualFloatType(expected)
	default:
		return compilerTypes.Int32
	}
}

func unsupportedOperatorExpression(token lexer.Token) checkedExpression {
	return checkedExpression{token: token, diagnostic: unsupportedOperatorDiagnostic(token)}
}

func unsupportedOperatorDiagnostic(token lexer.Token) *compilerTypes.Diagnostic {
	return diagnosticAt(typeErrorAt(token, "unsupported operator "+token.Lexeme))
}

func unaryOperatorDiagnostic(operator Operator, typ compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	message := fmt.Sprintf("operator %s requires Bool operands; got %s", operator, typ.Name)
	if operator == NegateOperator {
		message = fmt.Sprintf("negation requires a signed type; got %s", typ.Name)
	}
	if operator == BitwiseNotOperator {
		message = fmt.Sprintf("operator ~ requires an integer operand; got %s", typ.Name)
	}
	return diagnosticAt(typeErrorAt(token, message))
}

func binaryOperatorDiagnostic(operator Operator, typ compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	message := fmt.Sprintf("operator %s requires numeric operands; got %s", operator, typ.Name)
	switch operator {
	case RemainderOperator:
		message = fmt.Sprintf("operator %% requires integer operands; got %s", typ.Name)
	case LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator:
		message = fmt.Sprintf("operator %s requires ordered operands; got %s", operator, typ.Name)
	case LogicalAndOperator, LogicalOrOperator:
		message = fmt.Sprintf("operator %s requires Bool operands; got %s", operator, typ.Name)
	case EqualOperator, NotEqualOperator:
		message = fmt.Sprintf("operator %s requires scalar operands; got %s", operator, typ.Name)
	}
	return diagnosticAt(typeErrorAt(token, message))
}
