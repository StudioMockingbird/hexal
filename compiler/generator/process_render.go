package generator

import (
	"fmt"

	"hexal/compiler/checker"
)

// isProcessOperation reports whether name is one of the Process/Pipe
// operations sharing checker.NetworkExpression with Address/Dns/Tcp.
func isProcessOperation(name string) bool {
	switch name {
	case "process_start", "process_wait", "process_terminate", "process_close",
		"pipe_read", "pipe_write", "pipe_shutdown", "pipe_close":
		return true
	}
	return false
}

// renderProcessExpression renders one process/IPC operation through its
// per-module adapter.
func renderProcessExpression(node checker.Expression, state *expressionValidation) (string, error) {
	suffix := streamAdapterSuffix(node.ResultType)
	site := fmt.Sprintf("%d, %d", node.SourceLine, node.SourceColumn)
	switch node.Name {
	case "process_start":
		options, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_process_start_%s(%s, %s)", suffix, options, site), nil
	case "process_wait":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_process_wait_%s(%s, %s)", suffix, receiver, site), nil
	case "process_terminate":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_process_terminate_%s(%s, %s)", suffix, receiver, site), nil
	case "process_close":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_process_close_%s(%s, %s)", suffix, receiver, site), nil
	case "pipe_read":
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
		return fmt.Sprintf("hex_pipe_read_%s(%s, %s, %s, %s)", suffix, receiver, into, max, site), nil
	case "pipe_write":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		from, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_pipe_write_%s(%s, %s, %s)", suffix, receiver, from, site), nil
	case "pipe_shutdown":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_pipe_shutdown_%s(%s, %s)", suffix, receiver, site), nil
	case "pipe_close":
		receiver, _, err := renderHoistedExpressionNode(node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("hex_pipe_close_%s(%s, %s)", suffix, receiver, site), nil
	}
	return "", unknownExpressionDiagnostic("unknown process operation " + node.Name)
}

// validateProcessExpression checks one process/IPC operation fail-closed: a
// known operation name and checked metadata consistent with its shape.
func validateProcessExpression(node checker.Expression, state *expressionValidation) error {
	wantArguments := -1
	wantOperand := false
	switch node.Name {
	case "process_start":
		wantArguments = 1
	case "process_wait", "process_terminate", "process_close", "pipe_shutdown", "pipe_close":
		wantArguments, wantOperand = 0, true
	case "pipe_read":
		wantArguments, wantOperand = 2, true
	case "pipe_write":
		wantArguments, wantOperand = 1, true
	default:
		return unknownExpressionDiagnostic("unknown process operation " + node.Name)
	}
	if len(node.Arguments) != wantArguments {
		return unknownExpressionDiagnostic("process operation has invalid checked metadata")
	}
	if wantOperand {
		if node.Operand == nil {
			return unknownExpressionDiagnostic("process method operation has no checked receiver")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
	} else if node.Operand != nil {
		return unknownExpressionDiagnostic("process static operation has an unexpected checked receiver")
	}
	for _, argument := range node.Arguments {
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}
