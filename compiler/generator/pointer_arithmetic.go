package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// Raw pointer traversal lowers to C's own pointer arithmetic, indexing, and
// casts with no helper, bounds formula, trap, or metadata: the language adds
// nothing the C abstract machine does not already define.

func renderPointerOffset(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.Element == nil {
		return "", unknownExpressionDiagnostic()
	}
	receiver, err := renderHoistedReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	count, countErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
	if countErr != nil {
		return "", countErr
	}
	return "(" + receiver + " + " + count + ")", nil
}

func renderPointerIndex(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.Element == nil {
		return "", unknownExpressionDiagnostic()
	}
	receiver, err := renderHoistedReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	index, indexErr := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
	if indexErr != nil {
		return "", indexErr
	}
	return receiver + "[" + index + "]", nil
}

func renderPointerCast(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.OperandType.Element == nil || node.ResultType.Element == nil {
		return "", unknownExpressionDiagnostic()
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	// The canonical nested spelling keeps C's const qualification on the
	// layer the result's access mode names.
	return "(" + pointerSpelling(node.ResultType) + ")" + receiver, nil
}

func validatePointerOffset(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || len(node.Arguments) != 1 || !isPointerType(node.OperandType) ||
		!compilerTypes.Equal(node.OperandType, node.ResultType) ||
		!compilerTypes.Equal(node.Element, *node.OperandType.Element) ||
		!compilerTypes.IsCompleteValue(node.Element) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return err
	}
	return validateCheckedOperandWithState(node.Arguments[0], state)
}

func validatePointerIndex(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || len(node.Arguments) != 1 || !isPointerType(node.OperandType) ||
		!compilerTypes.Equal(node.ResultType, *node.OperandType.Element) ||
		!compilerTypes.Equal(node.Element, node.ResultType) ||
		!compilerTypes.IsCompleteValue(node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return err
	}
	return validateCheckedOperandWithState(node.Arguments[0], state)
}

func validatePointerCast(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || len(node.Arguments) != 0 ||
		!isPointerType(node.OperandType) || !isPointerType(node.ResultType) ||
		node.OperandType.PointeeWritable != node.ResultType.PointeeWritable ||
		!compilerTypes.Equal(node.Element, *node.ResultType.Element) {
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}
