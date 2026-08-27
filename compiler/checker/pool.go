package checker

import (
	"fmt"
	"go/constant"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolvePoolTypeUse resolves the built-in Pool<T> form written as an
// ordinary generic type expression. T must be complete, finite, and valid
// for HeapAllocation -- the same eligibility Heap.allocate<T> enforces.
func resolvePoolTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, "Pool requires exactly one element type"))
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	pool := typeEnvironment.PoolType(elementUse.Type)
	if pool == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, "Pool element type must be complete, finite, and valid for allocation; got "+elementUse.Type.Name))
	}
	return compilerTypes.NewTypeUse(pool), nil
}

// checkPoolTypeCall resolves Pool<T>.new(capacity) into a fresh independent
// typed fixed-capacity slot-allocator handle. Construction always uses
// Hexal's default allocation primitives and retains no parent Heap.
func checkPoolTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	poolUse, diagnostic := resolvePoolTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Pool", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if property.Lexeme != "new" || len(call.Arguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "Pool has no such operation; use Pool<T>.new(capacity)"))}
	}
	capacity := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[0]), ctx)
	if diagnostics := initializerDiagnostics(capacity); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
	}
	if !assignable(compilerTypes.SizeType, capacity.typ) {
		return checkedExpression{token: capacity.token, diagnostic: diagnosticAt(typeErrorAt(capacity.token, "Pool capacity must be a Size"))}
	}
	if capacity.known != nil && capacity.known.Constant != nil {
		if value, exact := constant.Uint64Val(capacity.known.Constant); exact && value == 0 {
			return checkedExpression{token: capacity.token, diagnostic: diagnosticAt(typeErrorAt(capacity.token, "Pool capacity must be positive"))}
		}
	}
	node := Expression{Kind: PoolConstructorExpression, Operand: &capacity.source.Node, Arguments: []Operand{capacity.source}, OperandType: poolUse.Type, ResultType: poolUse.Type, Element: poolUse.Type.Pool.Element}
	source := Operand{Kind: ExpressionOperand, Type: poolUse.Type, Name: "new", Node: node}
	return checkedExpression{source: source, typ: poolUse.Type, token: property}
}

// checkPoolMethodCall dispatches the built-in Pool methods: allocate, free,
// and destroy.
func checkPoolMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	poolType := receiver.typ
	element := poolType.Pool.Element
	if diagnostic := checkHandleNotDestroyed(receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	switch name {
	case "allocate":
		if len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Pool allocation accepts no type arguments; its element type is fixed by the receiver"))}
		}
		if len(call.Arguments) != 1 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "allocate expects 1 argument"))}
		}
		initial := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.Arguments[0]), ctx)
		if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if !assignable(element, initial.typ) {
			return checkedExpression{token: initial.token, diagnostic: diagnosticAt(typeErrorAt(initial.token, fmt.Sprintf("Pool allocation initializer requires %s; got %s", element.Name, initial.typ.Name)))}
		}
		result := ctx.typeEnvironment.MutPtrType(element)
		node := Expression{Kind: PoolMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{initial.source}, OperandType: poolType, ResultType: result, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: callee.Property}
	case "free":
		if len(call.Arguments) != 1 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "free expects 1 argument (pointer)"))}
		}
		pointer := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(compilerTypes.Type{}), tokenOf(call.Arguments[0]), ctx)
		if diagnostics := initializerDiagnostics(pointer); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if pointer.typ.Element == nil || !compilerTypes.Equal(*pointer.typ.Element, element) {
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(typeErrorAt(pointer.token, fmt.Sprintf("Pool free requires Ptr<%s> or MutPtr<%s>; got %s", element.Name, element.Name, pointer.typ.Name)))}
		}
		receiverBinding := receiverVariableBinding(receiver.source)
		if pointerBinding := receiverVariableBinding(pointer.source); pointerBinding != 0 {
			if source, ok := ctx.names.flow.provenance[pointerBinding]; ok && source != 0 && source != receiverBinding {
				return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(typeErrorAt(pointer.token, "pointer was allocated from a different Pool"))}
			}
		}
		if ctx.names.cleanupDepth == 0 {
			if diagnostic := checkTrackedHeapFreeInState(pointer.source, callee.Property, ctx.names.flow); diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
		}
		node := Expression{Kind: PoolMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{pointer.source}, OperandType: poolType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	case "destroy":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "destroy expects no arguments"))}
		}
		if ctx.names.cleanupDepth == 0 {
			if ctx.names.flow.hasLiveTrackedAllocation(receiverVariableBinding(receiver.source)) {
				return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Pool cannot be destroyed while a locally tracked slot is live"))}
			}
			if diagnostic := checkTrackedHeapFreeInState(receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
		}
		node := Expression{Kind: PoolMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: poolType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Pool has no method "+name+"; use allocate, free, or destroy"))}
	}
}
