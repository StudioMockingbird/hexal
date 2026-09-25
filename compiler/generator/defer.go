package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	"hexal/compiler/specdata"
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
	case checker.CallExpression:
		// A Fun<>-valued callee is captured at registration so the scope
		// exit calls the exact function value the defer saw; a named-function
		// callee keeps its direct C name so generated output stays
		// byte-identical.
		if node.Operand == nil || node.Operand.Kind == checker.FunctionReferenceExpression {
			break
		}
		if node.OperandType.Signature == nil {
			return unknownExpressionDiagnostic("deferred call without a checked callee")
		}
		calleeOperand := checker.Operand{Kind: checker.ExpressionOperand, Type: node.OperandType, Node: *node.Operand}
		name, err := state.captureOperand(body, calleeOperand, indent)
		if err != nil {
			return err
		}
		captured = append(captured, name)
	case checker.ChannelMethodCallExpression, checker.MutexMethodCallExpression, checker.TaskMethodCallExpression, checker.StreamMethodCallExpression,
		checker.StashMethodCallExpression, checker.PoolMethodCallExpression, checker.NetworkExpression:
		// defer ch.free(h), mutex.unlock(), task.join(), stream.close(),
		// stash.destroy(), pool.free(node)/destroy(), or
		// connection/listener.close() captures the handle at registration so
		// the cleanup always targets the exact handle the defer saw.
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
	if err := renderInto(body, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: declaration(operand.Type, name, false), Value: rendered}); err != nil {
		return "", err
	}
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
			if err := renderInto(body, "module.c", "errdef_guard_open", forStmtLineModel{Indent: indent, Value: errorExit}); err != nil {
				return err
			}
		}
		err := writeDeferredAction(body, action, state, indent)
		if err != nil {
			return err
		}
		if wrapErr {
			if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
				return err
			}
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
		if err := renderInto(body, "module.c", "call_stmt", callStmtModel{Indent: indent, Call: call}); err != nil {
			return err
		}
		return nil
	}
	if action.Value == nil {
		return unknownExpressionDiagnostic("deferred expression action without a checked operand")
	}
	rendered, err := renderOperandWithState(*action.Value, state)
	if err != nil {
		return err
	}
	return renderInto(body, "module.c", "defer_discard", forStmtLineModel{Indent: indent, Value: rendered})
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
		methodArguments := renderRestSliceArgument(&node, arguments[1:])
		if state.envMethods[node.Name] {
			if state.envPointer == "" {
				return "", unknownExpressionDiagnostic("deferred call to an environment-dependent method without an environment")
			}
			methodArguments = append([]string{state.envPointer}, methodArguments...)
		}
		return methodCName(node.Owner, node.Name, moduleOwner(node.Owner.ModuleID, state.owner)) + "(" + strings.Join(append([]string{arguments[0]}, methodArguments...), ", ") + ")", nil
	case checker.CallExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("deferred call without a checked callee")
		}
		if node.Operand.Kind == checker.FunctionReferenceExpression {
			if node.Operand.Name == "" {
				return "", unknownExpressionDiagnostic("deferred call without a checked function callee")
			}
			callArguments := renderRestSliceArgument(&node, arguments)
			if state.envFunctions[node.Operand.Name] {
				if state.envPointer == "" {
					return "", unknownExpressionDiagnostic("deferred call to an environment-dependent function without an environment")
				}
				callArguments = append([]string{state.envPointer}, callArguments...)
			}
			return privateCName(functionNameKind, node.Operand.Name, moduleOwner(node.Operand.Module, state.owner)) + "(" + strings.Join(callArguments, ", ") + ")", nil
		}
		// A Fun<>-valued callee was captured at registration; the call is the
		// captured function value applied to the captured arguments.
		if len(arguments) < 1 {
			return "", unknownExpressionDiagnostic("deferred call without a captured callee")
		}
		callArguments := renderRestSliceArgument(&node, arguments[1:])
		return arguments[0] + "(" + strings.Join(callArguments, ", ") + ")", nil
	case checker.HeapFreeExpression:
		if len(arguments) != 2 {
			return "", unknownExpressionDiagnostic("deferred heap free without captured arguments")
		}
		// The captures hold the receiver (the Heap) first and the freed
		// pointer second; the helper takes them in the opposite order, with
		// the heap token.
		return "((void)(" + arguments[0] + "), hex_heap_free(" + arguments[1] + "))", nil
	case checker.StringMethodCallExpression:
		if node.Name != "free" || len(arguments) != 2 {
			return "", unknownExpressionDiagnostic("deferred string free without captured arguments")
		}
		// The captures hold the receiver (the owning handle) first and the
		// heap second; the helper takes them in the opposite order.
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ExactOwner(specdata.TypeString), "free", "")
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + arguments[1] + ", " + arguments[0] + ")", nil
	case checker.CollectionMethodCallExpression:
		if node.Name != "free" || node.OperandType.List == nil && node.OperandType.Dict == nil || len(arguments) != 2 {
			return "", unknownExpressionDiagnostic("deferred collection free without captured arguments")
		}
		// The captures hold the receiver (the owning header) first and the
		// heap second; the helper takes them in the opposite order.
		if node.OperandType.List != nil {
			symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeList), "free", listSuffix(node.OperandType))
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[1] + ", " + arguments[0] + ")", nil
		}
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeDict), "free", dictSuffix(node.OperandType))
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + arguments[1] + ", " + arguments[0] + ")", nil
	case checker.ChannelMethodCallExpression:
		if node.Name != "free" || len(arguments) != 2 {
			return "", unknownExpressionDiagnostic("deferred channel free without captured arguments")
		}
		// The captures hold the receiver (the Channel handle) first and the
		// heap second; the helper takes the heap token.
		symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeChannel), "free", channelSuffix(node.OperandType))
		if symbolErr != nil {
			return "", symbolErr
		}
		return symbol + "(" + arguments[1] + ", " + arguments[0] + ")", nil
	case checker.MutexMethodCallExpression:
		switch node.Name {
		case "lock", "unlock":
			symbol, symbolErr := builtinMethodCallSymbol(specdata.ExactOwner(specdata.TypeMutex), node.Name, "")
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[0] + ")", nil
		case "free":
			// The captures hold the receiver (the Mutex handle) first and
			// the heap second; the adapter takes the heap token.
			symbol, symbolErr := builtinMethodCallSymbol(specdata.ExactOwner(specdata.TypeMutex), "free", "")
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[1] + ", " + arguments[0] + ")", nil
		}
		return "", unknownExpressionDiagnostic("deferred mutex method without a captured receiver")
	case checker.TaskMethodCallExpression:
		switch node.Name {
		case "join", "detach":
			symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeTask), node.Name, taskSuffix(node.OperandType))
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[0] + ")", nil
		}
		return "", unknownExpressionDiagnostic("deferred task method without a captured receiver")
	case checker.TimeExpression:
		return timeCall(node, arguments, state)
	case checker.NetworkExpression:
		// The checker admits only close as a deferred networking or
		// process/IPC operation; the captured receiver feeds the
		// module-owned result-union adapter.
		if len(arguments) != 1 {
			return "", unknownExpressionDiagnostic("deferred network call without a captured receiver")
		}
		suffix := streamAdapterSuffix(node.ResultType)
		site := fmt.Sprintf("%d, %d", state.line(node.Span), state.column(node.Span))
		switch node.Name {
		case "tcp_close":
			if compilerTypes.IsTcpListener(node.OperandType) {
				return fmt.Sprintf("hex_tcp_listener_close_%s(%s, %s)", suffix, arguments[0], site), nil
			}
			return fmt.Sprintf("hex_tcp_close_%s(%s, %s)", suffix, arguments[0], site), nil
		case "process_close":
			return fmt.Sprintf("hex_process_close_%s(%s, %s)", suffix, arguments[0], site), nil
		case "pipe_close":
			return fmt.Sprintf("hex_pipe_close_%s(%s, %s)", suffix, arguments[0], site), nil
		case "signals_close":
			return fmt.Sprintf("hex_signals_close_%s(%s, %s)", suffix, arguments[0], site), nil
		}
		return "", unknownExpressionDiagnostic("deferred network call without a captured receiver")
	case checker.StreamMethodCallExpression:
		// The checker admits only close as a deferred stream operation; the
		// captured receiver feeds the module-owned result-union adapter.
		if isFileNode(node) {
			return fileMethodCall(node, arguments, state)
		}
		if node.Name != "close" || len(arguments) != 1 {
			return "", unknownExpressionDiagnostic("deferred stream call without a captured stream")
		}
		return fmt.Sprintf("hex_io_close_%s(%s, %d, %d)",
			streamAdapterSuffix(node.ResultType), arguments[0], state.line(node.Span), state.column(node.Span)), nil
	case checker.StashMethodCallExpression:
		if len(arguments) < 1 {
			return "", unknownExpressionDiagnostic("deferred stash method without a captured receiver")
		}
		switch node.Name {
		case "allocate":
			if len(arguments) != 2 {
				return "", unknownExpressionDiagnostic("deferred stash allocate without captured arguments")
			}
			symbol, symbolErr := stashAllocateHelper(node.Element)
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[0] + ", " + arguments[1] + ")", nil
		case "reset":
			symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeStash), "reset", "")
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[0] + ")", nil
		case "destroy":
			symbol, symbolErr := builtinMethodCallSymbol(specdata.ConstructorOwner(specdata.TypeStash), "destroy", "")
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[0] + ")", nil
		}
		return "", unknownExpressionDiagnostic("deferred stash method without a captured receiver")
	case checker.PoolMethodCallExpression:
		if len(arguments) < 1 {
			return "", unknownExpressionDiagnostic("deferred pool method without a captured receiver")
		}
		switch node.Name {
		case "allocate":
			if len(arguments) != 2 {
				return "", unknownExpressionDiagnostic("deferred pool allocate without captured arguments")
			}
			symbol, symbolErr := poolAllocHelper(node.OperandType)
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[0] + ", " + arguments[1] + ")", nil
		case "free":
			if len(arguments) != 2 {
				return "", unknownExpressionDiagnostic("deferred pool free without captured arguments")
			}
			symbol, symbolErr := poolFreeHelper(node.OperandType)
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[0] + ", " + arguments[1] + ")", nil
		case "destroy":
			symbol, symbolErr := poolDestroyHelper(node.OperandType)
			if symbolErr != nil {
				return "", symbolErr
			}
			return symbol + "(" + arguments[0] + ")", nil
		}
		return "", unknownExpressionDiagnostic("deferred pool method without a captured receiver")
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
