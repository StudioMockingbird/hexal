package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// branchDefers returns the checked deferred actions registered in one elseif
// branch body.
func branchDefers(statement checker.IfStatement, branchIndex int) []checker.DeferredAction {
	if branchIndex < 0 || branchIndex >= len(statement.ElseIfDefers) {
		return nil
	}
	return statement.ElseIfDefers[branchIndex]
}

// writeDeferStatement registers a deferred action. A direct call captures its
// callee receiver and arguments into temporaries immediately; any other
// expression is recorded whole and evaluated at scope exit.
func writeDeferStatement(body *strings.Builder, statement checker.DeferStatement, state *expressionValidation, indent string) error {
	action := statement.Action
	// An action is active only after its defer or errdefer statement is
	// reached. The checked scope lists every action of the scope, so a try
	// error branch that renders earlier must not run actions whose statements
	// appear later in source order.
	state.registeredDefers = append(state.registeredDefers, action)
	if !action.IsCall {
		return nil
	}
	if action.Call == nil {
		return unknownExpressionDiagnostic("deferred call action without a checked call")
	}
	if state.captures == nil {
		state.captures = make(map[*checker.Operand][]string)
	}
	node := action.Call.Node
	captured := make([]string, 0, len(node.Arguments)+1)
	switch node.Kind {
	case checker.MethodCallExpression:
		if node.Operand == nil {
			return unknownExpressionDiagnostic("deferred method call without a receiver")
		}
		receiverOperand := checker.Operand{Kind: checker.ExpressionOperand, Type: node.OperandType, Node: *node.Operand}
		name, err := state.captureOperand(body, receiverOperand, indent)
		if err != nil {
			return err
		}
		captured = append(captured, name)
	case checker.HeapFreeExpression:
		if node.Operand == nil {
			return unknownExpressionDiagnostic("deferred heap free without a receiver")
		}
		receiverOperand := checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Heap, Node: *node.Operand}
		name, err := state.captureOperand(body, receiverOperand, indent)
		if err != nil {
			return err
		}
		captured = append(captured, name)
	case checker.StringMethodCallExpression:
		// defer text.free(h) captures the owning handle at registration so
		// the cleanup always frees the exact object the defer saw.
		if node.Operand == nil {
			return unknownExpressionDiagnostic("deferred string method without a receiver")
		}
		receiverOperand := checker.Operand{Kind: checker.ExpressionOperand, Type: node.OperandType, Node: *node.Operand}
		name, err := state.captureOperand(body, receiverOperand, indent)
		if err != nil {
			return err
		}
		captured = append(captured, name)
	case checker.CollectionMethodCallExpression:
		// defer list.free(h) or dict.free(h) captures the owning header
		// handle at registration so the cleanup always frees the exact
		// object.
		if node.Name != "free" || node.OperandType.List == nil && node.OperandType.Dict == nil || node.Operand == nil {
			return unknownExpressionDiagnostic("deferred collection free without a receiver")
		}
		receiverOperand := checker.Operand{Kind: checker.ExpressionOperand, Type: node.OperandType, Node: *node.Operand}
		name, err := state.captureOperand(body, receiverOperand, indent)
		if err != nil {
			return err
		}
		captured = append(captured, name)
	case checker.ChannelMethodCallExpression, checker.MutexMethodCallExpression, checker.TaskMethodCallExpression:
		// defer ch.free(h), mutex.unlock(), or task.join() captures the
		// handle at registration so the cleanup always targets the exact
		// handle the defer saw.
		if node.Operand == nil {
			return unknownExpressionDiagnostic("deferred handle method without a receiver")
		}
		receiverOperand := checker.Operand{Kind: checker.ExpressionOperand, Type: node.OperandType, Node: *node.Operand}
		name, err := state.captureOperand(body, receiverOperand, indent)
		if err != nil {
			return err
		}
		captured = append(captured, name)
	}
	for _, argument := range node.Arguments {
		name, err := state.captureOperand(body, argument, indent)
		if err != nil {
			return err
		}
		captured = append(captured, name)
	}
	state.captures[action.Call] = captured
	return nil
}

// captureOperand evaluates one operand into a fresh temporary at the current
// point and returns the temporary's name.
func (state *expressionValidation) captureOperand(body *strings.Builder, operand checker.Operand, indent string) (string, error) {
	rendered, err := renderOperandWithState(operand, state)
	if err != nil {
		return "", err
	}
	if !supportedGeneratedTypeWithState(operand.Type, state) {
		return "", unknownExpressionDiagnostic("deferred capture has an unsupported type")
	}
	state.captureCounter++
	name := fmt.Sprintf("hex_defer_capture_%d", state.captureCounter)
	fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(operand.Type, name, false), rendered)
	return name, nil
}

// writeDeferredActions emits one scope's actions in reverse registration order.
func writeDeferredActions(body *strings.Builder, actions []checker.DeferredAction, state *expressionValidation, indent string, errorExit string) error {
	for index := len(actions) - 1; index >= 0; index-- {
		action := actions[index]
		if !deferredActionRegistered(state, action) {
			continue
		}
		// errdefer actions run only on an Error exit; errorExit is the
		// literal "false", the literal "true", or a bool variable name.
		wrapErr := false
		if action.Err && errorExit != "false" && errorExit != "true" {
			wrapErr = true
		}
		if action.Err && errorExit == "false" {
			continue
		}
		if wrapErr {
			fmt.Fprintf(body, "%sif (%s) {\n", indent, errorExit)
		}
		err := writeDeferredAction(body, action, state, indent)
		if err != nil {
			return err
		}
		if wrapErr {
			fmt.Fprintf(body, "%s}\n", indent)
		}
	}
	return nil
}

// deferredActionRegistered reports whether one action's statement was
// processed. Actions share the checked Call or Value operand pointer across
// every copy of the DeferredAction value.
func deferredActionRegistered(state *expressionValidation, action checker.DeferredAction) bool {
	key := action.Call
	if key == nil {
		key = action.Value
	}
	if key == nil {
		return false
	}
	for _, registered := range state.registeredDefers {
		registeredKey := registered.Call
		if registeredKey == nil {
			registeredKey = registered.Value
		}
		if registeredKey == key {
			return true
		}
	}
	return false
}

func writeDeferredAction(body *strings.Builder, action checker.DeferredAction, state *expressionValidation, indent string) error {
	if action.IsCall {
		if action.Call == nil {
			return unknownExpressionDiagnostic("deferred call action without a checked call")
		}
		if action.Call.Node.Kind == checker.PrintExpression {
			return renderDeferredPrint(body, action, state, indent)
		}
		call, err := renderDeferredCall(action, state)
		if err != nil {
			return err
		}
		fmt.Fprintf(body, "%s%s;\n", indent, call)
		return nil
	}
	if action.Value == nil {
		return unknownExpressionDiagnostic("deferred expression action without a checked operand")
	}
	rendered, err := renderOperandWithState(*action.Value, state)
	if err != nil {
		return err
	}
	fmt.Fprintf(body, "%s(void)(%s);\n", indent, rendered)
	return nil
}

// renderDeferredCall renders a captured deferred call. Method calls use the
// captured receiver as the first argument; free functions use the captured
// arguments only; a Heap free uses its two captured operands.
func renderDeferredCall(action checker.DeferredAction, state *expressionValidation) (string, error) {
	node := action.Call.Node
	arguments := state.captures[action.Call]
	switch node.Kind {
	case checker.MethodCallExpression:
		if node.Owner == nil || len(arguments) < 1 {
			return "", unknownExpressionDiagnostic("deferred method call without a captured receiver")
		}
		return methodCName(node.Owner, node.Name, moduleOwner(node.Owner.ModuleID, state.owner)) + "(" + strings.Join(arguments, ", ") + ")", nil
	case checker.CallExpression:
		if node.Operand == nil || node.Operand.Kind != checker.FunctionReferenceExpression {
			return "", unknownExpressionDiagnostic("deferred call without a checked function callee")
		}
		return PrivateCName(FunctionName, node.Operand.Name, moduleOwner(node.Operand.Module, state.owner)) + "(" + strings.Join(arguments, ", ") + ")", nil
	case checker.HeapFreeExpression:
		if len(arguments) != 2 {
			return "", unknownExpressionDiagnostic("deferred heap free without captured arguments")
		}
		// The captures hold the receiver (the Heap) first and the freed
		// pointer second; the helper takes them in the opposite order, with
		// the heap's identity token.
		return "hex_heap_free(" + arguments[1] + ", " + arguments[0] + ".identity)", nil
	case checker.StringMethodCallExpression:
		if node.Name != "free" || len(arguments) != 2 {
			return "", unknownExpressionDiagnostic("deferred string free without captured arguments")
		}
		// The captures hold the receiver (the owning handle) first and the
		// heap second; the helper takes them in the opposite order.
		return "hex_string_free(" + arguments[1] + ", " + arguments[0] + ")", nil
	case checker.CollectionMethodCallExpression:
		if node.Name != "free" || node.OperandType.List == nil && node.OperandType.Dict == nil || len(arguments) != 2 {
			return "", unknownExpressionDiagnostic("deferred collection free without captured arguments")
		}
		// The captures hold the receiver (the owning header) first and the
		// heap second; the helper takes them in the opposite order.
		if node.OperandType.List != nil {
			return "hex_list_free_" + listSuffix(node.OperandType) + "(" + arguments[1] + ", " + arguments[0] + ")", nil
		}
		return "hex_dict_free_" + dictSuffix(node.OperandType) + "(" + arguments[1] + ", " + arguments[0] + ")", nil
	case checker.ChannelMethodCallExpression:
		if node.Name != "free" || len(arguments) != 2 {
			return "", unknownExpressionDiagnostic("deferred channel free without captured arguments")
		}
		// The captures hold the receiver (the Channel handle) first and the
		// heap second; the helper takes the heap's identity token.
		return "hex_chan_free_" + channelSuffix(node.OperandType) + "(" + arguments[1] + ".identity, " + arguments[0] + ")", nil
	case checker.MutexMethodCallExpression:
		switch node.Name {
		case "lock":
			return "hex_mutex_lock(" + arguments[0] + ")", nil
		case "unlock":
			return "hex_mutex_unlock(" + arguments[0] + ")", nil
		case "free":
			// The captures hold the receiver (the Mutex handle) first and
			// the heap second; the adapter takes the heap's identity token.
			return "hex_mutex_free_hex_mutex(" + arguments[1] + ".identity, " + arguments[0] + ")", nil
		}
		return "", unknownExpressionDiagnostic("deferred mutex method without a captured receiver")
	case checker.TaskMethodCallExpression:
		switch node.Name {
		case "join":
			return "hex_task_join_" + taskSuffix(node.OperandType) + "(" + arguments[0] + ")", nil
		case "detach":
			return "hex_task_detach(" + arguments[0] + ")", nil
		}
		return "", unknownExpressionDiagnostic("deferred task method without a captured receiver")
	default:
		return "", unknownExpressionDiagnostic("unsupported deferred call node")
	}
}

// unwindAllDefers emits every pending scope's actions from innermost to
// outermost, as required before a return.
func unwindAllDefers(body *strings.Builder, state *expressionValidation, indent string, errorExit string) error {
	for index := len(state.deferStack) - 1; index >= 0; index-- {
		if err := writeDeferredActions(body, state.deferStack[index], state, indent, errorExit); err != nil {
			return err
		}
	}
	return nil
}

// unwindToLoopDepth emits the pending actions of the scopes inside the current
// loop body from innermost to outermost, as required before break or continue.
func unwindToLoopDepth(body *strings.Builder, state *expressionValidation, indent string, errorExit string) error {
	if len(state.loopDepths) == 0 {
		return unknownExpressionDiagnostic("loop unwinding outside a loop")
	}
	bottom := state.loopDepths[len(state.loopDepths)-1]
	for index := len(state.deferStack) - 1; index >= bottom; index-- {
		if err := writeDeferredActions(body, state.deferStack[index], state, indent, errorExit); err != nil {
			return err
		}
	}
	return nil
}

var _ = compilerTypes.Type{}
