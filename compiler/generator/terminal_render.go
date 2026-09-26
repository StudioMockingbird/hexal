package generator

import (
	"fmt"

	"hexal/compiler/checker"
)

// renderTerminalExpression renders one Terminal operation through its
// per-module adapter. Both operations are static calls (no operand): the
// namespace is recognized by its own operation name, exactly like
// address_parse and dns_resolve in network_render.go.
func renderTerminalExpression(node checker.Expression, state *expressionValidation) (string, error) {
	suffix := streamAdapterSuffix(node.ResultType)
	site := fmt.Sprintf("%d, %d", state.line(node.Span), state.column(node.Span))
	switch node.Name {
	case "terminal_is_attached":
		stream, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_terminal_is_attached_%s(%s, %s)", suffix, stream, site), nil
	case "terminal_size":
		stream, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_terminal_size_%s(%s, %s)", suffix, stream, site), nil
	}
	return "", unknownExpressionDiagnostic()
}

// validateTerminalExpression checks one Terminal operation fail-closed: a
// known operation name, exactly one argument, and no checked receiver.
func validateTerminalExpression(node checker.Expression, state *expressionValidation) error {
	switch node.Name {
	case "terminal_is_attached", "terminal_size":
	default:
		return unknownExpressionDiagnostic()
	}
	if len(node.Arguments) != 1 {
		return unknownExpressionDiagnostic()
	}
	if node.Operand != nil {
		return unknownExpressionDiagnostic()
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}
