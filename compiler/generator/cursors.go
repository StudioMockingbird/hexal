package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// validateCursorMethod checks one cursor method's checked metadata: has_next
// yields Bool, next and peek yield the cursor's element, and offset yields Size.
func validateCursorMethod(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsCursor(node.OperandType) {
		return unknownExpressionDiagnostic()
	}
	element := compilerTypes.Rune
	switch {
	case compilerTypes.IsByteCursor(node.OperandType):
		element = compilerTypes.UInt8
	case compilerTypes.IsGraphemeCursor(node.OperandType):
		element = compilerTypes.GraphemeType
	}
	switch node.Name {
	case "has_next":
		if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic()
		}
	case "next", "peek":
		if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, element) {
			return unknownExpressionDiagnostic()
		}
	case "offset":
		if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
			return unknownExpressionDiagnostic()
		}
	default:
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) && !compilerTypes.WidensTo(node.ResultType, *expected) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

// renderCursorMethod renders one cursor method. has_next, peek, and offset read
// a copy; next takes the receiver's address so it advances that binding.
func renderCursorMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic()
	}
	prefix := "hex_byte_cursor_"
	switch {
	case compilerTypes.IsRuneCursor(node.OperandType):
		prefix = "hex_rune_cursor_"
	case compilerTypes.IsGraphemeCursor(node.OperandType):
		prefix = "hex_grapheme_cursor_"
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "has_next":
		return prefix + "has_next(" + receiver + ")", nil
	case "next":
		return prefix + "next(&(" + receiver + "))", nil
	case "peek":
		if compilerTypes.IsGraphemeCursor(node.OperandType) {
			return prefix + "peek(&(" + receiver + "))", nil
		}
		return prefix + "peek(" + receiver + ")", nil
	case "offset":
		return prefix + "offset(" + receiver + ")", nil
	}
	return "", unknownExpressionDiagnostic()
}
