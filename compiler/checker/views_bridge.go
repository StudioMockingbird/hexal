package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkViewBridgeCall resolves the explicit pointer-plus-length View
// constructor and the empty View. The pointer must be a statically non-null
// Ptr<T> or MutPtr<T>; the length must be a Size under ordinary lossless
// conversion rules. The result View is read-only, like every View.
func checkViewBridgeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	viewUse, diagnostic := resolveViewTypeUse(parser.GenericTypeExpression{Name: lexer.Token{Kind: lexer.Identifier, Lexeme: "View", Line: callee.Line, Column: callee.Column}, Arguments: call.TypeArguments}, callee, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: callee.Line, Column: callee.Column,
			Message: "View requires exactly one element type",
		}}
	}
	element := viewUse.Type.View.Element
	switch property.Lexeme {
	case "from_pointer":
		if len(call.Arguments) != 2 {
			return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: property.Line, Column: property.Column,
				Message: "View.from_pointer expects 2 arguments (pointer, length)",
			}}
		}
		pointer := checkValue(call.Arguments[0], names, typeEnvironment)
		if diagnostics := initializerDiagnostics(pointer); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if pointer.typ.Element == nil || compilerTypes.IsNullable(pointer.typ) {
			return checkedExpression{token: pointer.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: pointer.token.Line, Column: pointer.token.Column,
				Message: "nullable pointer must be narrowed before View construction",
			}}
		}
		if !compilerTypes.Equal(*pointer.typ.Element, element) {
			return checkedExpression{token: pointer.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: pointer.token.Line, Column: pointer.token.Column,
				Message: fmt.Sprintf("View<%s>.from_pointer requires Ptr<%s> or MutPtr<%s>; got %s", element.Name, element.Name, element.Name, pointer.typ.Name),
			}}
		}
		length := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[1]), names, typeEnvironment)
		if diagnostics := initializerDiagnostics(length); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
		}
		if !assignable(compilerTypes.SizeType, length.typ) {
			return checkedExpression{token: length.token, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: length.token.Line, Column: length.token.Column,
				Message: "View length cannot be represented as Size",
			}}
		}
		if diagnostic := fromPointerRefTrace(pointer, names); diagnostic != nil {
			return checkedExpression{token: pointer.token, diagnostic: diagnostic}
		}
		node := Expression{Kind: ViewBridgeExpression, Name: "from_pointer", Arguments: []Operand{pointer.source, length.source}, OperandType: viewUse.Type, ResultType: viewUse.Type, Element: element, RootKind: ViewRootForeign}
		source := Operand{Kind: ExpressionOperand, Type: viewUse.Type, Name: "from_pointer", Node: node}
		return checkedExpression{source: source, typ: viewUse.Type, token: property}
	case "empty":
		if len(call.Arguments) != 0 {
			return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
				Category: compilerTypes.TypeError, Stage: "checker",
				Line: property.Line, Column: property.Column,
				Message: "View.empty expects no arguments",
			}}
		}
		node := Expression{Kind: ViewBridgeExpression, Name: "empty", OperandType: viewUse.Type, ResultType: viewUse.Type, Element: element, RootKind: ViewRootNone}
		source := Operand{Kind: ExpressionOperand, Type: viewUse.Type, Name: "empty", Node: node}
		return checkedExpression{source: source, typ: viewUse.Type, token: property}
	}
	return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError, Stage: "checker",
		Line: property.Line, Column: property.Column,
		Message: "View has no such operation; use from_pointer or empty",
	}}
}

// fromPointerRefTrace rejects a from_pointer pointer argument that traces,
// within this function body, to a ref of local storage. The walk follows
// member, dereference, and index steps and local bindings initialized from a
// ref; parameters, heap allocations, and opaque call results pass.
func fromPointerRefTrace(pointer checkedExpression, names *scope) *compilerTypes.Diagnostic {
	if nodeTracesToRef(&pointer.source.Node, names) {
		return &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: pointer.token.Line, Column: pointer.token.Column,
			Message: "from_pointer does not accept a pointer into this function's local storage",
		}
	}
	return nil
}

func nodeTracesToRef(node *Expression, names *scope) bool {
	for node != nil {
		switch node.Kind {
		case AddressOfExpression:
			return true
		case VariableExpression:
			if node.Binding != 0 {
				if bound, ok := names.lookupBinding(node.Binding); ok && bound.fromRef {
					return true
				}
			}
			return false
		case MemberExpression, DereferenceExpression, IndexExpression:
			node = node.Operand
		default:
			return false
		}
	}
	return false
}
