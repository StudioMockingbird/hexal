package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveStashTypeUse resolves the built-in Stash<T> form written as an
// ordinary generic type expression. T must be complete, finite, and valid
// for HeapAllocation -- the same eligibility Heap.allocate<T> enforces.
func resolveStashTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, "Stash requires exactly one element type"))
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	stash := typeEnvironment.StashType(elementUse.Type)
	if stash == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, "Stash element type must be complete, finite, and valid for allocation; got "+elementUse.Type.Name))
	}
	return compilerTypes.NewTypeUse(stash), nil
}

// checkStashTypeCall resolves Stash<T>.new() into a fresh independent typed
// bump-allocator handle. It takes no arguments: construction always uses
// Hexal's default allocation primitives and retains no parent Heap.
func checkStashTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	stashUse, diagnostic := resolveStashTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "Stash", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if property.Lexeme != "new" || len(call.Arguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "Stash has no such operation; use Stash<T>.new()"))}
	}
	node := Expression{Kind: StashConstructorExpression, OperandType: stashUse.Type, ResultType: stashUse.Type, Element: stashUse.Type.Stash.Element}
	source := Operand{Kind: ExpressionOperand, Type: stashUse.Type, Name: "new", Node: node}
	return checkedExpression{source: source, typ: stashUse.Type, token: property}
}

// checkStashMethodCall dispatches the built-in Stash methods: allocate,
// reset, and destroy. A Stash allocation cannot be individually released;
// stash.free(pointer) is rejected by name.
func checkStashMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	stashType := receiver.typ
	element := stashType.Stash.Element
	if name == "free" {
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Stash allocations are released by reset or destroy"))}
	}
	if diagnostic := checkHandleNotDestroyed(receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	switch name {
	case "allocate":
		if len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Stash allocation accepts no type arguments; its element type is fixed by the receiver"))}
		}
		if len(call.Arguments) != 1 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "allocate expects 1 argument"))}
		}
		initial := checkInitializer(call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.Arguments[0]), ctx)
		if diagnostics := initializerDiagnostics(initial); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if !assignable(element, initial.typ) {
			return checkedExpression{token: initial.token, diagnostic: diagnosticAt(typeErrorAt(initial.token, fmt.Sprintf("Stash allocation initializer requires %s; got %s", element.Name, initial.typ.Name)))}
		}
		result := ctx.typeEnvironment.MutPtrType(element)
		node := Expression{Kind: StashMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{initial.source}, OperandType: stashType, ResultType: result, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
		return checkedExpression{source: source, typ: result, token: callee.Property}
	case "reset":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "reset expects no arguments"))}
		}
		if ctx.names.cleanupDepth == 0 {
			ctx.names.flow.invalidateAllocationsFrom(receiverVariableBinding(receiver.source))
		}
		node := Expression{Kind: StashMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: stashType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	case "destroy":
		if len(call.Arguments) != 0 || len(call.TypeArguments) != 0 {
			return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "destroy expects no arguments"))}
		}
		if ctx.names.cleanupDepth == 0 {
			ctx.names.flow.invalidateAllocationsFrom(receiverVariableBinding(receiver.source))
			if diagnostic := checkTrackedHeapFreeInState(receiver.source, callee.Property, ctx.names.flow); diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
		}
		node := Expression{Kind: StashMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: stashType, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		return checkedExpression{token: callee.Property, diagnostic: diagnosticAt(typeErrorAt(callee.Property, "Stash has no method "+name+"; use allocate, reset, or destroy"))}
	}
}
