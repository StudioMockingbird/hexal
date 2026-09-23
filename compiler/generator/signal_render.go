package generator

import (
	"fmt"

	"hexal/compiler/checker"
)

// renderSignalExpression renders one Signals operation through its
// per-module adapter.
func renderSignalExpression(node checker.Expression, state *expressionValidation) (string, error) {
	suffix := streamAdapterSuffix(node.ResultType)
	site := fmt.Sprintf("%d, %d", state.line(node.Span), state.column(node.Span))
	switch node.Name {
	case "signals_new":
		subscriptions, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_signals_new_%s(%s, %s)", suffix, subscriptions, site), nil
	case "signals_next":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_signals_next_%s(%s, %s)", suffix, receiver, site), nil
	case "signals_close":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_signals_close_%s(%s, %s)", suffix, receiver, site), nil
	}
	return "", unknownExpressionDiagnostic("unknown signal operation " + node.Name)
}

// validateSignalExpression checks one Signals operation fail-closed: a
// known operation name and checked metadata consistent with its shape.
func validateSignalExpression(node checker.Expression, state *expressionValidation) error {
	wantArguments := -1
	wantOperand := false
	switch node.Name {
	case "signals_new":
		wantArguments = 1
	case "signals_next", "signals_close":
		wantArguments, wantOperand = 0, true
	default:
		return unknownExpressionDiagnostic("unknown signal operation " + node.Name)
	}
	if len(node.Arguments) != wantArguments {
		return unknownExpressionDiagnostic("signal operation has invalid checked metadata")
	}
	if wantOperand {
		if node.Operand == nil {
			return unknownExpressionDiagnostic("signal method operation has no checked receiver")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
	} else if node.Operand != nil {
		return unknownExpressionDiagnostic("signal static operation has an unexpected checked receiver")
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}
