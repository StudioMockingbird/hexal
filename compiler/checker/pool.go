package checker

import (
	"go/constant"

	diagnosticsPkg "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolvePoolTypeUse resolves the built-in Pool<T> form written as an
// ordinary generic type expression. T must be complete, finite, and valid
// for HeapAllocation -- the same eligibility Heap.allocate<T> enforces.
func resolvePoolTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.AllocatorRequiresElementType("Pool")))
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	pool := typeEnvironment.PoolType(elementUse.Type)
	if pool == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diagnosticsPkg.AllocatorElementMustBeAllocatable("Pool", elementUse.Type.Name)))
	}
	return compilerTypes.NewTypeUse(pool), nil
}

// checkPoolTypeCall resolves Pool<T>(capacity) into a fresh independent
// typed fixed-capacity slot-allocator handle. Construction always uses
// Hexal's default allocation primitives and retains no parent Heap.
func checkPoolTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	poolUse, diagnostic := resolvePoolTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Pool", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if len(call.Arguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diagnosticsPkg.PoolConstructorUsage()))}
	}
	capacity := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[0]), ctx)
	if diagnostics := initializerDiagnostics(capacity); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !assignable(compilerTypes.SizeType, capacity.typ) {
		return checkedExpression{token: capacity.token, diagnostic: diagnosticAt(messageAt(capacity.token, diagnosticsPkg.PoolCapacityMustBeSize()))}
	}
	if capacity.known != nil && capacity.known.Constant != nil {
		if value, exact := constant.Uint64Val(capacity.known.Constant); exact && value == 0 {
			return checkedExpression{token: capacity.token, diagnostic: diagnosticAt(messageAt(capacity.token, diagnosticsPkg.PoolCapacityMustBePositive()))}
		}
	}
	node := Expression{Kind: PoolConstructorExpression, Operand: &capacity.source.Node, Arguments: []Operand{capacity.source}, OperandType: poolUse.Type, ResultType: poolUse.Type, Element: poolUse.Type.Pool.Element}
	source := Operand{Kind: ExpressionOperand, Type: poolUse.Type, Name: "new", Node: node}
	return checkedExpression{source: source, typ: poolUse.Type, token: callee}
}

// checkPoolMethodCall dispatches the built-in Pool methods: allocate, free,
// and destroy.
func checkPoolMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	poolType := call.receiver.typ
	element := poolType.Pool.Element
	if diagnostic := checkHandleNotDestroyed(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	if !hasBuiltinMethod(poolType, name) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.UnknownPoolMethod(name)))}
	}
	switch name {
	case "allocate":
		if len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.FixedElementAllocatorRejectsTypeArguments("Pool")))}
		}
		if len(call.call.Arguments) != 1 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AllocationRequiresOneArgument("allocate")))}
		}
		initial := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.call.Arguments[0]), call.ctx)
		if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.call.Arguments[0]), diagnostics: diagnostics}
		}
		if !assignable(element, initial.typ) {
			return checkedExpression{token: initial.token, diagnostic: diagnosticAt(messageAt(initial.token, diagnosticsPkg.AllocationInitializerType("Pool", element.Name, initial.typ.Name)))}
		}
		result := call.ctx.typeEnvironment.MutPtrType(element)
		node := Expression{Kind: PoolMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{initial.source}, OperandType: poolType, ResultType: result, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: call.callee.Property}
	case "free":
		if len(call.call.Arguments) != 1 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.PoolFreeRequiresPointer()))}
		}
		pointer := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Type{}), tokenOf(call.call.Arguments[0]), call.ctx)
		if diagnostics := initializerDiagnostics(pointer); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.call.Arguments[0]), diagnostics: diagnostics}
		}
		if pointer.typ.Element == nil || !compilerTypes.Equal(*pointer.typ.Element, element) {
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(messageAt(pointer.token, diagnosticsPkg.PoolFreePointerType(element.Name, pointer.typ.Name)))}
		}
		switch call.ctx.names.flow.allocatorKindOf(receiverVariableBinding(pointer.source)) {
		case heapAllocator:
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(poolFreeHeapAllocatedDiagnostic(pointer.token))}
		case stashAllocator:
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(poolFreeStashAllocatedDiagnostic(pointer.token))}
		}
		receiverBinding := receiverVariableBinding(call.receiver.source)
		if pointerBinding := receiverVariableBinding(pointer.source); pointerBinding != 0 {
			if source, ok := call.ctx.names.flow.provenance[pointerBinding]; ok && source != 0 && source != receiverBinding {
				return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(messageAt(pointer.token, diagnosticsPkg.PoolPointerFromDifferentPool()))}
			}
		}
		if call.ctx.names.cleanupDepth == 0 {
			if diagnostic := checkTrackedHeapFreeInState(pointer.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
				return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
			}
		}
		node := Expression{Kind: PoolMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{pointer.source}, OperandType: poolType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	case "destroy":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.AllocatorOperationNoArguments("destroy")))}
		}
		if call.ctx.names.cleanupDepth == 0 {
			if call.ctx.names.flow.hasLiveTrackedAllocation(receiverVariableBinding(call.receiver.source)) {
				return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diagnosticsPkg.PoolHasLiveTrackedSlot()))}
			}
			if diagnostic := checkTrackedHeapFreeInState(call.receiver.source, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
				return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
			}
		}
		node := Expression{Kind: PoolMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: poolType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	default:
		return unexpectedBuiltinMethod(poolType, call.callee.Property)
	}
}
