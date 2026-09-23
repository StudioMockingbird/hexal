package generator

import (
	"fmt"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// renderNetworkExpression renders one networking operation through its
// per-module adapter (or, for address_format, directly to the core).
func renderNetworkExpression(node checker.Expression, state *expressionValidation) (string, error) {
	if isProcessOperation(node.Name) {
		return renderProcessExpression(node, state)
	}
	if isSignalOperation(node.Name) {
		return renderSignalExpression(node, state)
	}
	if isTerminalOperation(node.Name) {
		return renderTerminalExpression(node, state)
	}
	suffix := streamAdapterSuffix(node.ResultType)
	site := fmt.Sprintf("%d, %d", state.line(node.Span), state.column(node.Span))
	switch node.Name {
	case "address_parse":
		text, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		port, err := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_address_parse_%s(%s, %s, %s)", suffix, text, port, site), nil
	case "address_format":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		heap, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_address_format(%s, %s)", receiver, heap), nil
	case "dns_resolve":
		heap, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		host, err := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
		if err != nil {
			return "", err
		}
		service, err := renderHoistedOperand(&node.Arguments[2].Node, node.Arguments[2], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_dns_resolve_%s(%s, %s, %s, %s)", suffix, host, service, heap, site), nil
	case "tcp_connect":
		address, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_tcp_connect_%s(%s, %s)", suffix, address, site), nil
	case "tcp_listen":
		address, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		backlog, err := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_tcp_listen_%s(%s, %s, %s)", suffix, address, backlog, site), nil
	case "tcp_accept":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_tcp_accept_%s(%s, %s)", suffix, receiver, site), nil
	case "tcp_close":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		if compilerTypes.IsTcpListener(node.OperandType) {
			return fmt.Sprintf("hex_tcp_listener_close_%s(%s, %s)", suffix, receiver, site), nil
		}
		return fmt.Sprintf("hex_tcp_close_%s(%s, %s)", suffix, receiver, site), nil
	case "tcp_read":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		into, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		max, err := renderHoistedOperand(&node.Arguments[1].Node, node.Arguments[1], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_tcp_read_%s(%s, %s, %s, %s)", suffix, receiver, into, max, site), nil
	case "tcp_write":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		from, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_tcp_write_%s(%s, %s, %s)", suffix, receiver, from, site), nil
	case "tcp_shutdown":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_tcp_shutdown_%s(%s, %s)", suffix, receiver, site), nil
	case "tcp_no_delay":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		enabled, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_tcp_no_delay_%s(%s, %s, %s)", suffix, receiver, enabled, site), nil
	}
	return "", unknownExpressionDiagnostic("unknown network operation " + node.Name)
}

// validateNetworkExpression checks one networking operation fail-closed: a
// known operation name, checked metadata consistent with its shape, and
// operand validity.
func validateNetworkExpression(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) error {
	if expected != nil && !compilerTypes.Equal(*expected, node.ResultType) {
		return unknownExpressionDiagnostic("network operation result does not match its expected type")
	}
	if isProcessOperation(node.Name) {
		return validateProcessExpression(node, state)
	}
	if isSignalOperation(node.Name) {
		return validateSignalExpression(node, state)
	}
	if isTerminalOperation(node.Name) {
		return validateTerminalExpression(node, state)
	}
	wantArguments := -1
	wantOperand := false
	switch node.Name {
	case "address_parse":
		wantArguments = 2
	case "address_format":
		wantArguments, wantOperand = 1, true
	case "dns_resolve":
		wantArguments = 3
	case "tcp_connect":
		wantArguments = 1
	case "tcp_listen":
		wantArguments = 2
	case "tcp_accept", "tcp_close", "tcp_shutdown":
		wantArguments, wantOperand = 0, true
	case "tcp_read":
		wantArguments, wantOperand = 2, true
	case "tcp_write", "tcp_no_delay":
		wantArguments, wantOperand = 1, true
	default:
		return unknownExpressionDiagnostic("unknown network operation " + node.Name)
	}
	if len(node.Arguments) != wantArguments {
		return unknownExpressionDiagnostic("network operation has invalid checked metadata")
	}
	if wantOperand {
		if node.Operand == nil {
			return unknownExpressionDiagnostic("network method operation has no checked receiver")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
	} else if node.Operand != nil {
		return unknownExpressionDiagnostic("network static operation has an unexpected checked receiver")
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}
