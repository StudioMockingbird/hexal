package checker

import (
	diagnosticsPkg "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveStashTypeUse resolves the built-in Stash<T> form written as an
// ordinary generic type expression. T must be complete, finite, and valid
// for HeapAllocation -- the same eligibility Heap.allocate<T> enforces.
func resolveStashTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.AllocatorRequiresElementType("Stash")))
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	stash := typeEnvironment.StashType(elementUse.Type)
	if stash == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.AllocatorElementMustBeAllocatable("Stash", elementUse.Type.Name)))
	}
	return compilerTypes.NewTypeUse(stash), nil
}

// checkStashTypeCall resolves Stash<T>() into a fresh independent typed
// bump-allocator handle. It takes no arguments: construction always uses
// Hexal's default allocation primitives and retains no parent Heap.
func checkStashTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	stashUse, diagnostic := resolveStashTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Stash", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if len(call.Arguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diagnosticsPkg.StashConstructorUsage()))}
	}
	node := Expression{Kind: StashConstructorExpression, OperandType: stashUse.Type, ResultType: stashUse.Type, Element: stashUse.Type.Stash.Element}
	source := Operand{Kind: ExpressionOperand, Type: stashUse.Type, Name: "new", Node: node}
	return checkedExpression{source: source, typ: stashUse.Type, token: callee}
}

// checkStashMethodCall dispatches the built-in Stash methods: allocate,
// reset, and destroy. A Stash allocation cannot be individually released;
// stash.free(pointer) is rejected by name.
func checkStashMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	stashType := call.receiver.typ
	element := stashType.Stash.Element
	if name == "free" {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.StashAllocationsReleasedByResetOrDestroy()))}
	}
	if diagnostic := checkHandleNotDestroyed(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	if !hasBuiltinMethod(stashType, name) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownStashMethod(name)))}
	}
	switch name {
	case "allocate":
		if len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.FixedElementAllocatorRejectsTypeArguments("Stash")))}
		}
		if len(call.call.Arguments) != 1 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AllocationRequiresOneArgument("allocate")))}
		}
		initial := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.call.Arguments[0]), call.ctx)
		if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.call.Arguments[0]), diagnostics: diagnostics}
		}
		if !assignable(element, initial.typ) {
			return checkedExpression{token: initial.token, diagnostic: diagnosticAt(messageAt(initial.token, diagnosticsPkg.AllocationInitializerType("Stash", element.Name, initial.typ.Name)))}
		}
		result := call.ctx.typeEnvironment.MutPtrType(element)
		node := Expression{Kind: StashMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{initial.source}, OperandType: stashType, ResultType: result, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: call.callee.Property}
	case "reset":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AllocatorOperationNoArguments("reset")))}
		}
		if call.ctx.names.cleanupDepth == 0 {
			call.ctx.names.flow.invalidateAllocationsFrom(receiverVariableBinding(call.receiver.source))
		}
		node := Expression{Kind: StashMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: stashType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	case "destroy":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AllocatorOperationNoArguments("destroy")))}
		}
		if call.ctx.names.cleanupDepth == 0 {
			call.ctx.names.flow.invalidateAllocationsFrom(receiverVariableBinding(call.receiver.source))
			if diagnostic := checkTrackedHeapFreeInState(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
				return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
			}
		}
		node := Expression{Kind: StashMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: stashType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	default:
		return unexpectedBuiltinMethod(stashType, call.callee.Property)
	}
}
