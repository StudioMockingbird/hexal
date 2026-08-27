package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkDeferStatement types a deferred expression and registers its action on
// the current lexical scope. A direct call is captured at registration; every
// other expression evaluates when the scope exits.
func checkDeferStatement(statement parser.DeferStatement, ctx checkContext) (DeferStatement, compilerTypes.Diagnostics) {
	ctx.names.cleanupDepth++
	defer func() { ctx.names.cleanupDepth-- }()
	action := DeferredAction{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}
	var source Operand
	if call, isCall := statement.Expression.(parser.CallExpression); isCall {
		checked := checkCall(call, ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return DeferStatement{}, diagnostics
		}
		action.IsCall = true
		action.Call = &checked.source
		captureDeferredHeapFree(&action, ctx.names)
		source = checked.source
	} else {
		checked := checkExitTimeExpression(statement.Expression, ctx)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return DeferStatement{}, diagnostics
		}
		action.Value = &checked.source
		source = checked.source
	}
	ctx.names.defers = append(ctx.names.defers, action)
	return DeferStatement{
		Expression:   source,
		Action:       action,
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
	}, nil
}

func checkExitTimeExpression(expression parser.Expression, ctx checkContext) checkedExpression {
	checking := ctx.names
	if ctx.names != nil && ctx.names.flow != nil {
		checking = ctx.names.child()
		checking.flow = ctx.names.flow.withoutFreedChecks()
		checking.cleanupDepth = ctx.names.cleanupDepth
	}
	return checkExpression(expression, expressionContext{inCleanup: true}, checkContext{names: checking, typeEnvironment: ctx.typeEnvironment})
}

// captureDeferredHeapFree captures the tracked binding+version a deferred
// release call targets, at registration time, so a later rebinding of the
// same slot before the deferred call fires does not change which value it
// validates against. Four call shapes release a tracked value: Heap.free and
// Pool.free target their pointer argument; Stash.destroy and Pool.destroy
// target their own receiver.
func captureDeferredHeapFree(action *DeferredAction, names *scope) {
	if action == nil || !action.IsCall || action.Call == nil || names == nil || names.flow == nil {
		return
	}
	node := action.Call.Node
	target, ok := trackedReleaseTarget(node)
	if !ok {
		return
	}
	binding := directPointerBinding(target, target.Type)
	if binding == 0 && target.Node.Kind == VariableExpression {
		// Stash/Pool handles are not pointer types, so directPointerBinding
		// (which requires target.Element != nil) never matches their
		// receiver; a bare variable read of the handle itself is enough.
		binding = target.Node.Binding
	}
	version, ok := names.flow.trackedVersion(binding)
	if !ok {
		return
	}
	action.TrackedFreeBinding = binding
	action.TrackedFreeVersion = version
}

// trackedReleaseTarget identifies the operand one release call invalidates:
// Heap.free/Pool.free's pointer argument, or Stash.destroy/Pool.destroy's
// own receiver.
func trackedReleaseTarget(node Expression) (Operand, bool) {
	switch {
	case node.Kind == HeapFreeExpression && len(node.Arguments) == 1:
		return node.Arguments[0], true
	case node.Kind == PoolMethodCallExpression && node.Name == "free" && len(node.Arguments) == 1:
		return node.Arguments[0], true
	case node.Kind == PoolMethodCallExpression && node.Name == "destroy" && node.Operand != nil:
		return Operand{Kind: ExpressionOperand, Type: node.OperandType, Node: *node.Operand}, true
	case node.Kind == StashMethodCallExpression && node.Name == "destroy" && node.Operand != nil:
		return Operand{Kind: ExpressionOperand, Type: node.OperandType, Node: *node.Operand}, true
	}
	return Operand{}, false
}

// checkHeapTypeCall resolves a call written as Heap.<name>(...) where the
// receiver names the built-in type itself rather than a Heap value.
func checkHeapTypeCall(call parser.CallExpression, token parser.VariableExpression, ctx checkContext) checkedExpression {
	name := call.Callee.(parser.PropertyExpression).Property.Lexeme
	if name != "new" || len(call.Arguments) != 0 {
		return checkedExpression{token: token.Name, diagnostic: diagnosticAt(typeErrorAt(token.Name, "Heap has no such operation; use Heap.new()"))}
	}
	source := constantOperand(compilerTypes.Heap, nil, "")
	source.Node = constantNode(source)
	return checkedExpression{source: source, typ: compilerTypes.Heap, token: token.Name, known: &source}
}

// checkHeapAllocate resolves h.allocate<T>(initial) into a checked
// HeapAllocateExpression returning MutPtr<T>.
func checkHeapAllocate(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 1 {
		message := "allocation requires an explicit initializer"
		if len(call.Arguments) > 1 {
			message = "allocate expects 1 argument; got " + fmt.Sprint(len(call.Arguments))
		}
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, message))}
	}
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "allocate requires exactly one type argument"))}
	}
	elementUse, diagnostic := resolveTypeUse(call.TypeArguments[0], callee.Property, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	element := elementUse.Type
	if !compilerTypes.Eligible(element, compilerTypes.PositionHeapAllocation) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "allocation requires a complete finite type"))}
	}
	initial := checkInitializer(call.Arguments[0], elementUse, callee.Property, ctx)
	if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if initial.typ != (compilerTypes.Type{}) && !assignable(element, initial.typ) {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, fmt.Sprintf("allocation initializer requires %s; got %s", element.Name, initial.typ.Name)))}
	}
	receiverNode := expressionNode(receiver.source)
	result := ctx.typeEnvironment.MutPtrType(element)
	node := Expression{
		Kind:        HeapAllocateExpression,
		Operand:     &receiverNode,
		Arguments:   []Operand{initial.source},
		OperandType: compilerTypes.Heap,
		ResultType:  result,
		Element:     element,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Node: node}
	return checkedExpression{source: source, typ: result, token: callee.Property}
}

// checkHeapFree resolves h.free(value) into a no-result checked
// HeapFreeExpression. The value may be Ptr<T> or MutPtr<T>.
func checkHeapFree(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "free expects exactly one pointer argument"))}
	}
	value := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Type{}), callee.Property, ctx)
	if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if value.typ.Element == nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "value is not an allocation produced by this Heap"))}
	}
	if nodeTracesToRef(&value.source.Node, ctx.names) {
		diagnostic := freeLocalStorageDiagnostic(callee.Property)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	if ctx.names.cleanupDepth == 0 {
		if diagnostic := checkTrackedHeapFree(value.source, callee.Property, ctx.names); diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
	}
	receiverNode := expressionNode(receiver.source)
	node := Expression{
		Kind:        HeapFreeExpression,
		Operand:     &receiverNode,
		Arguments:   []Operand{value.source},
		OperandType: compilerTypes.Heap,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
}

func checkTrackedHeapFree(value Operand, token lexer.Token, names *scope) *compilerTypes.Diagnostic {
	if names == nil {
		return nil
	}
	return checkTrackedHeapFreeInState(value, token, names.flow)
}

func checkTrackedHeapFreeInState(value Operand, token lexer.Token, state *flowState) *compilerTypes.Diagnostic {
	if state == nil || value.Node.Kind != VariableExpression || value.Node.Binding == 0 || !state.tracked[value.Node.Binding] {
		return nil
	}
	if state.freed(value.Node.Binding) {
		diagnostic := doubleFreeDiagnostic(token)
		return &diagnostic
	}
	state.markFreed(value.Node.Binding)
	return nil
}

// checkHandleNotDestroyed rejects a Stash or Pool operation whose receiver is
// a directly tracked binding already proven destroyed. Unlike
// checkTrackedHeapFreeInState, this never marks anything freed: it is a pure
// precondition read, shared by every Stash/Pool method (allocate, reset,
// free) other than destroy itself, which instead marks the handle freed
// through checkTrackedHeapFreeInState.
func checkHandleNotDestroyed(receiver Operand, token lexer.Token, state *flowState) *compilerTypes.Diagnostic {
	if state == nil || receiver.Node.Kind != VariableExpression || receiver.Node.Binding == 0 || !state.freed(receiver.Node.Binding) {
		return nil
	}
	diagnostic := useAfterFreeDiagnostic(token)
	return &diagnostic
}

func checkTrackedHeapFreeVersion(token lexer.Token, state *flowState, binding BindingID, version uint64) *compilerTypes.Diagnostic {
	if state == nil || binding == 0 || version == 0 || !state.tracked[binding] {
		return nil
	}
	if state.freedAt(binding, version) {
		diagnostic := doubleFreeDiagnostic(token)
		return &diagnostic
	}
	state.markFreedVersion(binding, version)
	return nil
}

func validateDeferredActions(names *scope, normal bool) compilerTypes.Diagnostics {
	if names == nil || names.flow == nil {
		return nil
	}
	type diagnosticKey struct {
		line    int
		column  int
		message string
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[diagnosticKey]bool)
	appendUnique := func(actions []DeferredAction, state *flowState) {
		for _, diagnostic := range validateDeferredActionsInState(actions, state) {
			key := diagnosticKey{line: diagnostic.Line, column: diagnostic.Column, message: diagnostic.Message}
			if seen[key] {
				continue
			}
			seen[key] = true
			diagnostics = append(diagnostics, diagnostic)
		}
	}
	if normal {
		appendUnique(names.defers, names.flow)
	}
	for _, flow := range names.returnFlows {
		appendUnique(flow.actions, flow.state)
	}
	return diagnostics
}

func validateDeferredActionsInState(actions []DeferredAction, state *flowState) compilerTypes.Diagnostics {
	if state == nil {
		return nil
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for index := len(actions) - 1; index >= 0; index-- {
		action := actions[index]
		token := lexer.Token{Line: action.SourceLine, Column: action.SourceColumn}
		if action.IsCall {
			if action.Call == nil {
				continue
			}
			target, ok := trackedReleaseTarget(action.Call.Node)
			if !ok {
				continue
			}
			var diagnostic *compilerTypes.Diagnostic
			if action.TrackedFreeBinding != 0 && action.TrackedFreeVersion != 0 {
				diagnostic = checkTrackedHeapFreeVersion(token, state, action.TrackedFreeBinding, action.TrackedFreeVersion)
			} else {
				diagnostic = checkTrackedHeapFreeInState(target, token, state)
			}
			if diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)
			}
			continue
		}
		if action.Value == nil {
			continue
		}
		if diagnostic := checkDeferredExpression(&action.Value.Node, token, state); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	return diagnostics
}

func checkDeferredExpression(expression *Expression, token lexer.Token, state *flowState) *compilerTypes.Diagnostic {
	if expression == nil {
		return nil
	}
	if state != nil && expression.Operand != nil && expression.Operand.Kind == VariableExpression &&
		(expression.Kind == DereferenceExpression || expression.Kind == VolatileReadExpression || expression.Kind == VolatileWriteExpression) &&
		state.freed(expression.Operand.Binding) {
		diagnostic := useAfterFreeDiagnostic(token)
		return &diagnostic
	}
	if diagnostic := checkDeferredExpression(expression.Operand, token, state); diagnostic != nil {
		return diagnostic
	}
	if diagnostic := checkDeferredExpression(expression.Left, token, state); diagnostic != nil {
		return diagnostic
	}
	if diagnostic := checkDeferredExpression(expression.Right, token, state); diagnostic != nil {
		return diagnostic
	}
	if expression.Object != nil {
		for _, initializer := range expression.Object.Initializers {
			if diagnostic := checkDeferredExpression(&initializer.Source.Node, token, state); diagnostic != nil {
				return diagnostic
			}
		}
	}
	for index := range expression.Arguments {
		if diagnostic := checkDeferredExpression(&expression.Arguments[index].Node, token, state); diagnostic != nil {
			return diagnostic
		}
	}
	return nil
}
