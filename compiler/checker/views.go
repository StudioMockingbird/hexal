package checker

import (
	"fmt"
	"go/constant"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveViewTypeUse resolves the built-in View<T> form written as an ordinary
// generic type expression. T must be an inline element; views of views or of
// owning values are rejected because a view read never creates or transfers
// an owning payload.
func resolveViewTypeUse(expression parser.GenericTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	if len(expression.Arguments) != 1 {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  "View requires exactly one element type",
		}
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	view := typeEnvironment.ViewType(elementUse.Type)
	if view == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Name.Line,
			Column:   expression.Name.Column,
			Message:  elementUse.Type.Name + " is not an inline view element type",
		}
	}
	return compilerTypes.NewTypeUse(view), nil
}

// checkSliceMethod checks start.slice(begin, end) for Array and View
// receivers. Array receivers must be stable places; View receivers preserve
// their recorded root chain, and slicing a temporary view keeps its original
// stable root. Known constant bounds against a known array length fail at
// compile time; all other invalid ranges trap at runtime.
func checkSliceMethod(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
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

	var element compilerTypes.Type
	if receiver.typ.Array != nil {
		element = receiver.typ.Array.Element
	} else {
		element = receiver.typ.View.Element
	}
	if receiver.typ.Array != nil {
		// A view must be rooted in stable storage: an Array local, parameter,
		// or member place. A temporary Array has no addressable storage.
		if !receiver.source.Addressable {
			diagnostic := typeErrorAt(callee.Property, "a view cannot be rooted in a temporary Array")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		if start.Constant != nil && start.Constant.Kind() == constant.Int && end.Constant != nil && end.Constant.Kind() == constant.Int {
			startValue, startExact := constant.Int64Val(start.Constant)
			endValue, endExact := constant.Int64Val(end.Constant)
			if startExact && endExact && (startValue > endValue || endValue > int64(receiver.typ.Array.Length)) {
				diagnostic := typeErrorAt(tokenOf(call.Arguments[0]), fmt.Sprintf("slice range [%d, %d) is out of bounds for %s", startValue, endValue, receiver.typ.Name))
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
			}
		}
	}

	viewType := typeEnvironment.ViewType(element)
	if viewType == (compilerTypes.Type{}) {
		diagnostic := typeErrorAt(callee.Property, element.Name+" is not an inline view element type")
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	node := Expression{
		Kind:        CollectionSliceExpression,
		Name:        "slice",
		Operand:     &receiver.source.Node,
		Arguments:   []Operand{start, end},
		OperandType: receiver.typ,
		ResultType:  viewType,
		Element:     element,
	}
	source := Operand{Kind: ExpressionOperand, Type: viewType, Name: "slice", Node: node}
	return checkedExpression{source: source, typ: viewType, token: callee.Property}
}
