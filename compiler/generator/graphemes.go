package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// validateGraphemeMethod checks one Grapheme method's checked metadata: bytes
// yields a Slice<UInt8> and rune_length yields Size.
func validateGraphemeMethod(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if node.Operand == nil || !compilerTypes.IsGrapheme(node.OperandType) {
		return unknownExpressionDiagnostic()
	}
	switch node.Name {
	case "bytes":
		if len(node.Arguments) != 0 || node.ResultType.Slice == nil || !compilerTypes.Equal(node.ResultType.Slice.Element, compilerTypes.UInt8) {
			return unknownExpressionDiagnostic()
		}
	case "rune_length":
		if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
			return unknownExpressionDiagnostic()
		}
	default:
		return unknownExpressionDiagnostic()
	}
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionChildWithState(node.Operand, node.OperandType, state)
}

// renderGraphemeMethod renders one Grapheme method over the borrowed range.
// Both helpers take the range by value, so a receiver such as another cursor
// step is evaluated exactly once.
func renderGraphemeMethod(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic()
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	switch node.Name {
	case "bytes":
		return "hex_grapheme_bytes(" + receiver + ")", nil
	case "rune_length":
		return "hex_grapheme_rune_length(" + receiver + ")", nil
	}
	return "", unknownExpressionDiagnostic()
}
