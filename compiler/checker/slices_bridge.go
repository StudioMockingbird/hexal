package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkSliceBridgeCall resolves the explicit pointer-plus-length Slice
// constructors and the empty Slice in both access modes. The single
// call-site type argument selects the mode: Slice<T> accepts Ptr<T> or
// Ptr<mut T> and stays read-only, while Slice<mut T> requires Ptr<mut T>.
// The length must be a Size under ordinary lossless conversion rules. No
// provenance, lifetime, or validity analysis constrains construction: the
// programmer owns the backing storage contract.
func checkSliceBridgeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	property := call.Callee.(parser.PropertyExpression).Property
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "Slice requires exactly one element type"))}
	}
	writable := false
	argument := call.TypeArguments[0]
	if mut, ok := argument.(parser.MutTypeArgument); ok {
		writable = true
		argument = mut.Type
	}
	elementUse, diagnostic := resolveTypeUse(argument, callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	slice := ctx.typeEnvironment.SliceType(elementUse.Type, writable)
	if slice == (compilerTypes.Type{}) {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, elementUse.Type.Name+" is not a valid Slice element type"))}
	}
	element := elementUse.Type
	constructor := "Slice"
	if writable {
		constructor = "Slice<mut T>"
	}
	switch property.Lexeme {
	case "from_pointer":
		if len(call.Arguments) != 2 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, constructor+".from_pointer expects 2 arguments (pointer, length)"))}
		}
		pointer := checkValue(call.Arguments[0], ctx)
		if diagnostics := initializerDiagnostics(pointer); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if pointer.typ.Element == nil || compilerTypes.IsNullable(pointer.typ) {
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(typeErrorAt(pointer.token, "nullable pointer must be narrowed before Slice construction"))}
		}
		if !compilerTypes.Equal(*pointer.typ.Element, element) {
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(typeErrorAt(pointer.token, fmt.Sprintf("%s.from_pointer requires %s; got %s", constructor, requiredFromPointerMode(element, writable), pointer.typ.Name)))}
		}
		if writable && !pointer.typ.PointeeWritable {
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(typeErrorAt(pointer.token, fmt.Sprintf("%s.from_pointer requires %s; got %s", constructor, requiredFromPointerMode(element, writable), pointer.typ.Name)))}
		}
		length := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[1]), ctx)
		if diagnostics := initializerDiagnostics(length); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
		}
		if !assignable(compilerTypes.SizeType, length.typ) {
			return checkedExpression{token: length.token, diagnostic: diagnosticAt(typeErrorAt(length.token, "Slice length cannot be represented as Size"))}
		}
		node := Expression{Kind: SliceBridgeExpression, Name: "from_pointer", Arguments: []Operand{pointer.source, length.source}, OperandType: slice, ResultType: slice, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: slice, Name: "from_pointer", Node: node}
		return checkedExpression{source: source, typ: slice, token: property}
	case "empty":
		if len(call.Arguments) != 0 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, constructor+".empty expects no arguments"))}
		}
		node := Expression{Kind: SliceBridgeExpression, Name: "empty", OperandType: slice, ResultType: slice, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: slice, Name: "empty", Node: node}
		return checkedExpression{source: source, typ: slice, token: property}
	}
	return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, constructor+" has no such operation; use from_pointer or empty"))}
}

// requiredFromPointerMode spells the accepted pointer mode for one
// from_pointer constructor: read-only construction accepts either pointer
// mode, writable construction accepts only the writable mode.
func requiredFromPointerMode(element compilerTypes.Type, writable bool) string {
	if writable {
		return "Ptr<mut " + element.Name + ">"
	}
	return "Ptr<" + element.Name + "> or Ptr<mut " + element.Name + ">"
}

// nodeTracesToRef reports whether a checked node traces to address-taking of
// local storage: directly through an address node, or through a binding
// whose value originated from one. Heap.free uses it to reject stack
// storage; from_pointer performs no such analysis.
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
