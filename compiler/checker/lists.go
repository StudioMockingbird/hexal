package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveListTypeUse resolves the built-in List<T> form written as an
// ordinary generic type expression. T must be a collection element: an
// inline element or a direct String.
func resolveListTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  "List requires exactly one element type",
		}
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	list := typeEnvironment.ListType(elementUse.Type)
	if list == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  elementUse.Type.Name + " is not a list element type",
		}
	}
	return compilerTypes.NewTypeUse(list), nil
}

// checkListTypeCall resolves List<T>.new(heap) into a fresh owning list.
func checkListTypeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if property.Lexeme != "new" || len(call.TypeArguments) != 1 || len(call.Arguments) != 1 {
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     callee.Line,
			Column:   callee.Column,
			Message:  "List has no such operation; use List<T>.new(heap)",
		}}
	}
	listUse, diagnostic := resolveListTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "List", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	heap := checkValue(call.Arguments[0], names, typeEnvironment)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := typeErrorAt(heap.token, "List<T>.new requires a Heap; got "+heap.typ.Name)
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

// checkListMethodCall dispatches the built-in List methods: length,
// is_empty, at, slice, push, pop, set, clear, and free.
func checkListMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	listType := receiver.typ
	element := listType.List.Element
	switch name {
	case "length":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "length expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: listType, ResultType: compilerTypes.SizeType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: callee.Property}
	case "is_empty":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "is_empty expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: listType, ResultType: compilerTypes.Bool}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Bool, token: callee.Property}
	case "at":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("at expects 1 argument, got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		index, diagnostic := checkArrayIndex(call.Arguments[0], callee.Property, environment, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		node := Expression{
			Kind:        CollectionMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{index},
			OperandType: listType,
			ResultType:  element,
			Element:     element,
		}
		source := Operand{Kind: ExpressionOperand, Type: element, Name: name, Node: node}
		return checkedExpression{source: source, typ: element, token: callee.Property}
	case "slice":
		if len(call.Arguments) != 2 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("slice expects 2 arguments, got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		start, diagnostic := checkArrayIndex(call.Arguments[0], callee.Property, environment, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		end, diagnostic := checkArrayIndex(call.Arguments[1], callee.Property, environment, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		if !receiver.source.Addressable {
			diagnostic := typeErrorAt(callee.Property, "a view cannot be rooted in a temporary List")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		viewType := typeEnvironment.ViewType(element)
		if viewType == (compilerTypes.Type{}) {
			diagnostic := typeErrorAt(callee.Property, element.Name+" is not an inline view element type")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        CollectionSliceExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{start, end},
			OperandType: listType,
			ResultType:  viewType,
			Element:     element,
		}
		source := Operand{Kind: ExpressionOperand, Type: viewType, Name: name, Node: node}
		return checkedExpression{source: source, typ: viewType, token: callee.Property}
	case "push", "set", "clear", "pop":
		switch name {
		case "push":
			if len(call.Arguments) != 1 {
				diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("push expects 1 argument, got %d", len(call.Arguments)))
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
			}
			value, diagnostic := listElementArgument(call.Arguments[0], callee.Property, element, environment, typeEnvironment)
			if diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
			node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{value}, OperandType: listType, ResultType: compilerTypes.Type{}, Element: element}
			source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
			return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
		case "set":
			if len(call.Arguments) != 2 {
				diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("set expects 2 arguments, got %d", len(call.Arguments)))
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
			}
			index, diagnostic := checkArrayIndex(call.Arguments[0], callee.Property, environment, typeEnvironment)
			if diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
			value, diagnostic := listElementArgument(call.Arguments[1], callee.Property, element, environment, typeEnvironment)
			if diagnostic != nil {
				return checkedExpression{token: callee.Property, diagnostic: diagnostic}
			}
			node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, Arguments: []Operand{index, value}, OperandType: listType, ResultType: compilerTypes.Type{}, Element: element}
			source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
			return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
		case "clear":
			if len(call.Arguments) != 0 {
				diagnostic := typeErrorAt(callee.Property, "clear expects no arguments")
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
			}
			node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: listType, ResultType: compilerTypes.Type{}, Element: element}
			source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
			return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
		case "pop":
			if len(call.Arguments) != 0 {
				diagnostic := typeErrorAt(callee.Property, "pop expects no arguments")
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
			}
			node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: listType, ResultType: element, Element: element}
			source := Operand{Kind: ExpressionOperand, Type: element, Name: name, Node: node}
			return checkedExpression{source: source, typ: element, token: callee.Property}
		}
	case "free":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("free expects 1 argument, got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		heap := checkValue(call.Arguments[0], environment, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := typeErrorAt(heap.token, "free requires a Heap; got "+heap.typ.Name)
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        CollectionMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{heap.source},
			OperandType: listType,
			ResultType:  compilerTypes.Type{},
			Element:     element,
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		diagnostic := typeErrorAt(callee.Property, listType.Name+" has no method "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	return checkedExpression{token: callee.Property, diagnostic: &compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     callee.Property.Line,
		Column:   callee.Property.Column,
		Message:  "unsupported list operation",
	}}
}

// listElementArgument checks one push or set value against the element type.
// RFC 0035: a String result is stored by shallow handle copy; cleanup stays
// the programmer's responsibility.
func listElementArgument(expression parser.Expression, fallback lexer.Token, element compilerTypes.Type, environment *scope, typeEnvironment *compilerTypes.Environment) (Operand, *compilerTypes.Diagnostic) {
	checked := checkInitializer(expression, compilerTypes.NewTypeUse(element), fallback, environment, typeEnvironment)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return Operand{}, &diagnostics[0]
	}
	if !assignable(element, checked.typ) {
		diagnostic := typeErrorAt(checked.token, "list element requires "+element.Name+"; got "+checked.typ.Name)
		return Operand{}, &diagnostic
	}
	return checked.source, nil
}
