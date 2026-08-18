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
func checkDeferStatement(statement parser.DeferStatement, names *scope, typeEnvironment *compilerTypes.Environment) (DeferStatement, compilerTypes.Diagnostics) {
	names.cleanupDepth++
	defer func() { names.cleanupDepth-- }()
	action := DeferredAction{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}
	var source Operand
	if call, isCall := statement.Expression.(parser.CallExpression); isCall {
		checked := checkCall(call, names, typeEnvironment)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return DeferStatement{}, diagnostics
		}
		action.IsCall = true
		action.Call = &checked.source
		captureDeferredHeapFree(&action, names)
		source = checked.source
	} else {
		checked := checkExitTimeExpression(statement.Expression, names, typeEnvironment)
		if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
			return DeferStatement{}, diagnostics
		}
		action.Value = &checked.source
		source = checked.source
	}
	names.defers = append(names.defers, action)
	return DeferStatement{
		Expression:   source,
		Action:       action,
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
	}, nil
}

func checkExitTimeExpression(expression parser.Expression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	checking := names
	if names != nil && names.flow != nil {
		checking = names.child()
		checking.flow = names.flow.withoutFreedChecks()
		checking.cleanupDepth = names.cleanupDepth
	}
	return checkExpression(expression, expressionContext{inCleanup: true}, checking, typeEnvironment)
}

func captureDeferredHeapFree(action *DeferredAction, names *scope) {
	if action == nil || !action.IsCall || action.Call == nil || names == nil || names.flow == nil {
		return
	}
	node := action.Call.Node
	if node.Kind != HeapFreeExpression || len(node.Arguments) != 1 {
		return
	}
	argument := node.Arguments[0]
	binding := directPointerBinding(argument, argument.Type)
	version, ok := names.flow.trackedVersion(binding)
	if !ok {
		return
	}
	action.HeapFreeBinding = binding
	action.HeapFreeVersion = version
}

// checkHeapTypeCall resolves a call written as Heap.<name>(...) where the
// receiver names the built-in type itself rather than a Heap value.
func checkHeapTypeCall(call parser.CallExpression, token parser.VariableExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := call.Callee.(parser.PropertyExpression).Property.Lexeme
	if name != "new" || len(call.Arguments) != 0 {
		return checkedExpression{token: token.Name, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     token.Name.Line,
			Column:   token.Name.Column,
			Message:  "Heap has no such operation; use Heap.new()",
		}}
	}
	source := constantOperand(compilerTypes.Heap, nil, "")
	source.Node = constantNode(source)
	return checkedExpression{source: source, typ: compilerTypes.Heap, token: token.Name, known: &source}
}

// checkHeapAllocate resolves h.allocate<T>(initial) into a checked
// HeapAllocateExpression returning MutPtr<T>.
func checkHeapAllocate(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.Arguments) != 1 {
		message := "allocation requires an explicit initializer"
		if len(call.Arguments) > 1 {
			message = "allocate expects 1 argument, got " + fmt.Sprint(len(call.Arguments))
		}
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  message,
		}}
	}
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "allocate requires exactly one type argument",
		}}
	}
	elementUse, diagnostic := resolveTypeUse(call.TypeArguments[0], callee.Property, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	element := elementUse.Type
	if !compilerTypes.Eligible(element, compilerTypes.PositionHeapAllocation) {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "allocation requires a complete finite type",
		}}
	}
	initial := checkInitializer(call.Arguments[0], elementUse, callee.Property, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if initial.typ != (compilerTypes.Type{}) && !assignable(element, initial.typ) {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  fmt.Sprintf("allocation initializer requires %s, got %s", element.Name, initial.typ.Name),
		}}
	}
	receiverNode := expressionNode(receiver.source)
	result := typeEnvironment.MutPtrType(element)
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
func checkHeapFree(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "free expects exactly one pointer argument",
		}}
	}
	value := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Type{}), callee.Property, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
		return checkedExpression{token: callee.Property, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	if value.typ.Element == nil {
		return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Property.Line,
			Column:   callee.Property.Column,
			Message:  "value is not an allocation produced by this Heap",
		}}
	}
	if nodeTracesToRef(&value.source.Node, names) {
		diagnostic := freeLocalStorageDiagnostic(callee.Property)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	if names.cleanupDepth == 0 {
		if diagnostic := checkTrackedHeapFree(value.source, callee.Property, names); diagnostic != nil {
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
	states := make([]*flowState, 0, 1+len(names.returnFlows))
	if normal {
		states = append(states, names.flow)
	}
	states = append(states, names.returnFlows...)
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, state := range states {
		diagnostics = append(diagnostics, validateDeferredActionsInState(names.defers, state)...)
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
			if action.Call == nil || action.Call.Node.Kind != HeapFreeExpression || len(action.Call.Node.Arguments) != 1 {
				continue
			}
			var diagnostic *compilerTypes.Diagnostic
			if action.HeapFreeBinding != 0 && action.HeapFreeVersion != 0 {
				diagnostic = checkTrackedHeapFreeVersion(token, state, action.HeapFreeBinding, action.HeapFreeVersion)
			} else {
				diagnostic = checkTrackedHeapFreeInState(action.Call.Node.Arguments[0], token, state)
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
	if expression.Kind == DereferenceExpression && expression.Operand != nil &&
		expression.Operand.Kind == VariableExpression && state.freed(expression.Operand.Binding) {
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
