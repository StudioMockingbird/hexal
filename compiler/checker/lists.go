package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveListTypeUse resolves the built-in List<T> form written as an
// ordinary generic type expression. T must be a collection element: an
// inline element or a direct String.
func resolveListTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diag.ListTypeArgumentCount()))
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	list := typeEnvironment.ListType(elementUse.Type)
	if list == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Name, diag.InvalidListElementType(elementUse.Type.Name)))
	}
	return compilerTypes.NewTypeUse(list), nil
}

// checkListTypeCall resolves List<T>(heap) into a fresh owning list.
func checkListTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.TypeArguments) != 1 || len(call.Arguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.ListConstructorArgumentShape()))}
	}
	listUse, diagnostic := resolveListTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "List", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := messageAt(heap.token, diag.ListConstructorHeapType(heap.typ.Name))
		return checkedExpression{token: heap.token, diagnostic: &diagnostic}
	}
	node := Expression{
		Kind:        ListNewExpression,
		Operand:     &heap.source.Node,
		Arguments:   []Operand{heap.source},
		OperandType: compilerTypes.Heap,
		ResultType:  listUse.Type,
		Element:     listUse.Type.List.Element,
	}
	source := Operand{Kind: ExpressionOperand, Type: listUse.Type, Name: "new", Node: node}
	return checkedExpression{source: source, typ: listUse.Type, token: callee}
}

// checkListMethodCall dispatches the built-in List methods: length, slice,
// push, pop, set, clear, and free.
func checkListMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	listType := call.receiver.typ
	element := listType.List.Element
	if !hasBuiltinMethod(listType, name) {
		diagnostic := messageAt(call.callee.Property, diag.CollectionHasNoMethod(listType.Name, name))
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	switch name {
	case "length":
		if len(call.call.Arguments) != 0 {
			diagnostic := messageAt(call.callee.Property, diag.CollectionMethodNoArguments("length"))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: listType, ResultType: compilerTypes.SizeType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: call.callee.Property}
	case "slice":
		return checkSliceMethod(call, false)
	case "mut_slice":
		return checkSliceMethod(call, true)
	case "push", "clear", "pop":
		switch name {
		case "push":
			if len(call.call.Arguments) != 1 {
				diagnostic := messageAt(call.callee.Property, diag.ListMethodArgumentCount("push", 1, len(call.call.Arguments)))
				return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
			}
			value, diagnostic := listElementArgument(call.call.Arguments[0], call.callee.Property, element, call.ctx)
			if diagnostic != nil {
				return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
			}
			node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, Arguments: []Operand{value}, OperandType: listType, ResultType: compilerTypes.Type{}, Element: element}
			source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
			return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
		case "clear":
			if len(call.call.Arguments) != 0 {
				diagnostic := messageAt(call.callee.Property, diag.CollectionMethodNoArguments("clear"))
				return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
			}
			node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: listType, ResultType: compilerTypes.Type{}, Element: element}
			source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
			return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
		case "pop":
			if len(call.call.Arguments) != 0 {
				diagnostic := messageAt(call.callee.Property, diag.CollectionMethodNoArguments("pop"))
				return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
			}
			node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: listType, ResultType: element, Element: element}
			source := Operand{Kind: ExpressionOperand, Type: element, Name: name, Node: node}
			return checkedExpression{source: source, typ: element, token: call.callee.Property}
		}
	case "free":
		if len(call.call.Arguments) != 1 {
			diagnostic := messageAt(call.callee.Property, diag.ListMethodArgumentCount("free", 1, len(call.call.Arguments)))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
		heap := checkValue(call.call.Arguments[0], call.ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := messageAt(heap.token, diag.ListFreeHeapType(heap.typ.Name))
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        CollectionMethodCallExpression,
			Name:        name,
			Operand:     &call.receiver.source.Node,
			Arguments:   []Operand{heap.source},
			OperandType: listType,
			ResultType:  compilerTypes.Type{},
			Element:     element,
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		// A locally proved free releases the allocation every Bytes stream
		// borrowed from this exact binding; borrowers consult the mark. A
		// deferred free releases at scope exit, outside local proof, so it
		// takes the documented unknown-state envelope instead.
		if call.ctx.names.flow != nil && call.ctx.names.cleanupDepth == 0 {
			if binding := baseBindingID(&call.receiver.source.Node); binding != 0 {
				call.ctx.names.flow.releaseSource(binding)
			}
		}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	default:
		return unexpectedBuiltinMethod(listType, call.callee.Property)
	}
	return unexpectedBuiltinMethod(listType, call.callee.Property)
}

// listElementArgument checks one push or set value against the element type.
// A String result is stored by shallow handle copy; cleanup stays the
// programmer's responsibility.
func listElementArgument(expression parser.Expression, fallback lexer.Token, element compilerTypes.Type, ctx checkContext) (Operand, *compilerTypes.Diagnostic) {
	checked := checkInitializer(expression, compilerTypes.NewTypeUse(element), fallback, ctx)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return Operand{}, &diagnostics[0]
	}
	if !assignable(element, checked.typ) {
		diagnostic := messageAt(checked.token, diag.ListElementTypeMismatch(element.Name, checked.typ.Name, textMismatchDetails(element, checked.typ)))
		return Operand{}, &diagnostic
	}
	if diagnostic := atomicCopyDiagnostic(checked.source, fallback); diagnostic != nil {
		return Operand{}, diagnostic
	}
	return checked.source, nil
}
