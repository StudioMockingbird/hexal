// render_operations.go owns arithmetic, comparison, and logical operation
// rendering and the C operator mapping they lower through.
package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func renderOperationWithState(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.ConstantExpression:
		if node.Constant == nil || node.Constant.Kind != checker.ConstantOperand && node.Constant.Kind != checker.ObjectOperand ||
			!compilerTypes.Equal(node.ResultType, node.Constant.Type) ||
			!supportedGeneratedScalarType(node.ResultType) && node.Constant.Type.Object == nil && node.Constant.Type.Union == nil {
			return "", unknownExpressionDiagnostic("constant expression without a checked constant")
		}
		return renderOperandWithState(*node.Constant, state)
	case checker.UnaryOperationExpression:
		return renderUnaryOperationWithState(node, state)
	case checker.BinaryOperationExpression:
		return renderBinaryOperationWithState(node, state)
	case checker.InvalidExpression, checker.VariableExpression, checker.AddressOfExpression,
		checker.DereferenceExpression, checker.MemberExpression, checker.ObjectExpression,
		checker.FunctionReferenceExpression, checker.CallExpression, checker.MethodCallExpression:
		return "", unknownExpressionDiagnostic("non-operation passed to operation renderer")
	default:
		return "", unknownExpressionDiagnostic("unsupported checked operation")
	}
}

func renderUnaryOperationWithState(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic("unary operation without an operand")
	}
	if node.Operator == checker.LogicalNotOperator {
		return renderLogicalNotWithState(node, state)
	}
	if !supportedGeneratedScalarType(node.OperandType) || !supportedGeneratedScalarType(node.ResultType) {
		return "", unknownExpressionDiagnostic("unary operation with an unsupported type")
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return "", err
	}
	operand, err := renderExpressionExpectedWithState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}

	switch node.Operator {
	case checker.NegateOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return "", unknownExpressionDiagnostic("negation result type does not match its operand type")
		}
		if compilerTypes.IsSignedInteger(node.OperandType) {
			return renderSignedWrap(node.Operator, node.OperandType, "", operand)
		}
		if compilerTypes.IsFloat(node.OperandType) {
			return "(-" + operand + ")", nil
		}
		return "", unknownExpressionDiagnostic("negation of an unsupported type")
	case checker.BitwiseNotOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return "", unknownExpressionDiagnostic("complement result type does not match its operand type")
		}
		if !compilerTypes.IsInteger(node.OperandType) {
			return "", unknownExpressionDiagnostic("complement of an unsupported type")
		}
		return renderBitwiseComplement(node.OperandType, operand)
	case checker.InvalidOperator,
		checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator,
		checker.DivideOperator, checker.RemainderOperator, checker.EqualOperator,
		checker.NotEqualOperator, checker.LessOperator, checker.LessEqualOperator,
		checker.GreaterOperator, checker.GreaterEqualOperator, checker.LogicalAndOperator,
		checker.LogicalOrOperator, checker.BitwiseAndOperator, checker.BitwiseXorOperator,
		checker.BitwiseOrOperator, checker.ShiftLeftOperator, checker.ShiftRightOperator:
		return "", unknownExpressionDiagnostic("binary operator in unary operation")
	default:
		return "", unknownExpressionDiagnostic("unknown unary operator")
	}
}

// renderLogicalNotWithState renders !operand: the operand's truthiness is
// negated, so any value-producing operand is valid.
func renderLogicalNotWithState(node checker.Expression, state *expressionValidation) (string, error) {
	if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
		return "", unknownExpressionDiagnostic("logical not requires a truthy-compatible operand and a Bool result")
	}
	child, err := renderTruthinessChild(node.Operand, state, node.OperandType)
	if err != nil {
		return "", err
	}
	return "(!" + child + ")", nil
}

func renderBinaryOperationWithState(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Left == nil || node.Right == nil {
		return "", unknownExpressionDiagnostic("binary operation without both operands")
	}
	if node.Operator == checker.LogicalAndOperator || node.Operator == checker.LogicalOrOperator {
		return renderLogicalOperationWithState(node, state)
	}
	if !supportedGeneratedScalarType(node.OperandType) && node.OperandType.Element == nil || !supportedGeneratedScalarType(node.ResultType) {
		return "", unknownExpressionDiagnostic("binary operation with an unsupported type")
	}
	// An unsigned +, -, or * is one node of a ring tree. Only a maximal tree
	// reaches here: renderRingOperand renders same-type ring children itself
	// and never routes them back through this function, so every arrival is
	// a tree root and narrows once.
	if isUnsignedRingOperation(node) {
		return renderUnsignedRingTree(node, state)
	}
	// A shift count keeps its own integer type; it never takes the left
	// operand's type.
	rightExpected := node.OperandType
	if node.Operator == checker.ShiftLeftOperator || node.Operator == checker.ShiftRightOperator {
		if rightType, ok := expressionTypeWithState(*node.Right, state); ok {
			rightExpected = rightType
		}
	}
	if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
		return "", err
	}
	if err := validateExpressionChildWithState(node.Right, rightExpected, state); err != nil {
		return "", err
	}
	resultIsBool := false
	arithmeticResult := false
	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator, checker.DivideOperator:
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return "", unknownExpressionDiagnostic("arithmetic operation with an unsupported type")
		}
	case checker.RemainderOperator:
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) {
			return "", unknownExpressionDiagnostic("remainder operation with a non-integer type")
		}
	case checker.EqualOperator, checker.NotEqualOperator:
		if node.OperandType.ScalarKind == compilerTypes.ScalarNone && node.OperandType.Element == nil {
			return "", unknownExpressionDiagnostic("equality operation with a non-scalar type")
		}
		resultIsBool = true
	case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return "", unknownExpressionDiagnostic("ordering operation with an unsupported type")
		}
		resultIsBool = true
	case checker.LogicalAndOperator, checker.LogicalOrOperator:
		// Unreachable: logical operations are routed to
		// renderLogicalOperationWithState before the scalar guards.
		resultIsBool = true
	case checker.BitwiseAndOperator, checker.BitwiseXorOperator, checker.BitwiseOrOperator:
		// Bitwise operations require an eligible integer type at the
		// selected exact width.
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) {
			return "", unknownExpressionDiagnostic("bitwise operation with an unsupported type")
		}
	case checker.ShiftLeftOperator, checker.ShiftRightOperator:
		// Shifts preserve the left operand's type.
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) {
			return "", unknownExpressionDiagnostic("shift operation with an unsupported type")
		}
	case checker.InvalidOperator, checker.NegateOperator, checker.LogicalNotOperator, checker.BitwiseNotOperator:
		return "", unknownExpressionDiagnostic("non-binary operator in binary operation")
	default:
		return "", unknownExpressionDiagnostic("unknown binary operator")
	}
	if arithmeticResult && !compilerTypes.Equal(node.OperandType, node.ResultType) {
		return "", unknownExpressionDiagnostic("arithmetic result type does not match its operand type")
	}
	if resultIsBool != compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
		return "", unknownExpressionDiagnostic("binary operation has an invalid result type")
	}
	left, err := renderHoistedExpressionExpected(node.Left, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	right, err := renderHoistedExpressionExpected(node.Right, &rightExpected, state)
	if err != nil {
		return "", err
	}

	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator:
		if compilerTypes.IsSignedInteger(node.OperandType) {
			return renderSignedWrap(node.Operator, node.OperandType, left, right)
		}
		if compilerTypes.IsUnsignedInteger(node.OperandType) {
			// Unsigned +, -, and * are ring operations, routed to the tree
			// renderer above; reaching here means the operand and result
			// types disagree, which the ring predicate rejects.
			return "", unknownExpressionDiagnostic("unsigned arithmetic result type does not match its operand type")
		}
	case checker.DivideOperator, checker.RemainderOperator:
		if compilerTypes.IsInteger(node.OperandType) {
			return renderDivisionOperation(node, left, right)
		}
	case checker.BitwiseAndOperator, checker.BitwiseXorOperator, checker.BitwiseOrOperator:
		return renderBitwiseOperation(node.Operator, node.OperandType, left, right)
	case checker.ShiftLeftOperator, checker.ShiftRightOperator:
		return shiftHelperName(shiftSpec{operator: node.Operator, typ: node.OperandType}) + "(" + left + ", (uint64_t)(" + right + "))", nil
	case checker.EqualOperator, checker.NotEqualOperator, checker.LessOperator,
		checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator,
		checker.LogicalAndOperator, checker.LogicalOrOperator:
		operator, ok := binaryCOperator(node.Operator)
		if !ok {
			return "", unknownExpressionDiagnostic("unknown binary operator")
		}
		return "(" + left + " " + operator + " " + right + ")", nil
	case checker.InvalidOperator, checker.NegateOperator, checker.LogicalNotOperator, checker.BitwiseNotOperator:
		return "", unknownExpressionDiagnostic("non-binary operator in binary operation")
	default:
		return "", unknownExpressionDiagnostic("unknown binary operator " + node.Operator.String())
	}
	operator, ok := binaryCOperator(node.Operator)
	if !ok {
		return "", unknownExpressionDiagnostic("unknown binary operator")
	}
	return "(" + left + " " + operator + " " + right + ")", nil
}

// renderLogicalOperationWithState renders and/or: operands of any
// value-producing type are rendered through their truthiness; the generated
// &&/|| preserve the short-circuit rule, and the comma expressions keep each
// operand's evaluation when it is reached.
func renderLogicalOperationWithState(node checker.Expression, state *expressionValidation) (string, error) {
	if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
		return "", unknownExpressionDiagnostic("logical operation requires a truthy-compatible operand and a Bool result")
	}
	left, err := renderTruthinessChild(node.Left, state, node.OperandType)
	if err != nil {
		return "", err
	}
	right, err := renderTruthinessChild(node.Right, state, node.OperandType)
	if err != nil {
		return "", err
	}
	operator := "&&"
	if node.Operator == checker.LogicalOrOperator {
		operator = "||"
	}
	return "(" + left + " " + operator + " " + right + ")", nil
}

func binaryCOperator(operator checker.Operator) (string, bool) {
	switch operator {
	case checker.AddOperator:
		return "+", true
	case checker.SubtractOperator:
		return "-", true
	case checker.MultiplyOperator:
		return "*", true
	case checker.DivideOperator:
		return "/", true
	case checker.RemainderOperator:
		return "%", true
	case checker.EqualOperator:
		return "==", true
	case checker.NotEqualOperator:
		return "!=", true
	case checker.LessOperator:
		return "<", true
	case checker.LessEqualOperator:
		return "<=", true
	case checker.GreaterOperator:
		return ">", true
	case checker.GreaterEqualOperator:
		return ">=", true
	case checker.LogicalAndOperator:
		return "&&", true
	case checker.LogicalOrOperator:
		return "||", true
	case checker.InvalidOperator, checker.NegateOperator, checker.LogicalNotOperator:
		return "", false
	default:
		return "", false
	}
}

func unsignedCName(typ compilerTypes.Type) (string, bool) {
	if !supportedGeneratedScalarType(typ) {
		return "", false
	}
	switch typ {
	case compilerTypes.UInt8, compilerTypes.Int8:
		return "uint8_t", true
	case compilerTypes.UInt16, compilerTypes.Int16:
		return "uint16_t", true
	case compilerTypes.UInt32, compilerTypes.Int32, compilerTypes.Rune:
		return "uint32_t", true
	case compilerTypes.UInt64, compilerTypes.Int64:
		return "uint64_t", true
	case compilerTypes.SizeType:
		return "size_t", true
	default:
		return "", false
	}
}
