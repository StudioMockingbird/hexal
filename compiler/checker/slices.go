package checker

import (
	"fmt"
	"go/constant"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveSliceTypeUse resolves the built-in Slice<T> and Slice<mut T>
// forms. T must satisfy the shared slice-element position; each access mode
// interns separately.
func resolveSliceTypeUse(expression parser.SliceTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	elementUse, diagnostic := resolveTypeUse(expression.Element, fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	slice := typeEnvironment.SliceType(elementUse.Type, expression.Writable)
	if slice == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Keyword, elementUse.Type.Name+" is not a valid Slice element type"))
	}
	return compilerTypes.NewTypeUse(slice), nil
}

// checkSliceMethod checks start.slice(begin, end) and start.mut_slice(begin,
// end) for Array, List, and Slice receivers. The mutable form is valid only
// on Array and List: re-slicing a Slice preserves the receiver's own access
// mode through slice. Array receivers must be stable places, and mutable
// Array slicing needs a writable place; a fixed List handle already permits
// interior element mutation. Known constant bounds against a known array
// length fail at compile time; all other invalid ranges trap at runtime.
func checkSliceMethod(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext, mutable bool) checkedExpression {
	name := "slice"
	if mutable {
		name = "mut_slice"
	}
	if len(call.Arguments) != 2 {
		diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("%s expects 2 arguments; got %d", name, len(call.Arguments)))
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	start, startKnown, diagnostic := checkArrayIndex(call.Arguments[0], callee.Property, ctx)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}
	end, endKnown, diagnostic := checkArrayIndex(call.Arguments[1], callee.Property, ctx)
	if diagnostic != nil {
		return checkedExpression{token: callee.Property, diagnostic: diagnostic}
	}

	var element compilerTypes.Type
	kind := ""
	writable := mutable
	switch {
	case receiver.typ.Array != nil:
		element = receiver.typ.Array.Element
		kind = "Array"
	case receiver.typ.List != nil:
		element = receiver.typ.List.Element
		kind = "List"
	case receiver.typ.Slice != nil:
		element = receiver.typ.Slice.Element
		writable = receiver.typ.Slice.Writable
	}
	if kind != "" {
		// A slice must be rooted in stable storage. A temporary
		// Array or List has no addressable storage.
		if !receiver.source.Addressable {
			diagnostic := typeErrorAt(callee.Property, "a Slice cannot be rooted in a temporary "+kind)
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
	}
	if receiver.typ.Array != nil {
		if mutable && !receiver.source.Writable {
			diagnostic := typeErrorAt(callee.Property, "mut_slice requires a writable Array place")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		if startKnown != nil && startKnown.Constant != nil && startKnown.Constant.Kind() == constant.Int &&
			endKnown != nil && endKnown.Constant != nil && endKnown.Constant.Kind() == constant.Int {
			startValue, startExact := constant.Int64Val(startKnown.Constant)
			endValue, endExact := constant.Int64Val(endKnown.Constant)
			if startExact && endExact && (startValue > endValue || endValue > int64(receiver.typ.Array.Length)) {
				diagnostic := typeErrorAt(tokenOf(call.Arguments[0]), fmt.Sprintf("slice range [%d, %d) is out of bounds for %s", startValue, endValue, receiver.typ.Name))
				return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
			}
		}
	}

	sliceType := ctx.typeEnvironment.SliceType(element, writable)
	if sliceType == (compilerTypes.Type{}) {
		diagnostic := typeErrorAt(callee.Property, element.Name+" is not a valid Slice element type")
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
	node := Expression{
		Kind:        CollectionSliceExpression,
		Name:        name,
		Operand:     &receiver.source.Node,
		Arguments:   []Operand{start, end},
		OperandType: receiver.typ,
		ResultType:  sliceType,
		Element:     element,
	}
	source := Operand{Kind: ExpressionOperand, Type: sliceType, Name: name, Node: node}
	return checkedExpression{source: source, typ: sliceType, token: callee.Property}
}

// viewRootIsLocal reports whether root names storage that dies with the
// enclosing function: a non-parameter, non-receiver binding.
func viewRootIsLocal(root BindingID, names *scope) bool {
	bound, ok := names.lookupBinding(root)
	return ok && !bound.parameter && root != names.selfID
}

func findLocalViewRoot(node *Expression, names *scope, seen map[*Expression]bool) bool {
	if node == nil || seen[node] {
		return false
	}
	seen[node] = true
	if node.RootKind == ViewRootBindings {
		for _, root := range node.ViewRoots {
			if viewRootIsLocal(root, names) {
				return true
			}
		}
	}
	switch node.Kind {
	case ObjectExpression:
		if node.Object != nil {
			for index := range node.Object.Initializers {
				if findLocalViewRoot(&node.Object.Initializers[index].Source.Node, names, seen) {
					return true
				}
			}
		}
	case AdtConstructExpression, ArrayLiteralExpression, MatchExpression:
		for index := range node.Arguments {
			if findLocalViewRoot(&node.Arguments[index].Node, names, seen) {
				return true
			}
		}
	case UnionInjectionExpression, UnionWidenExpression, TryExpression, MemberExpression, AdtPayloadExpression, IndexExpression:
		if findLocalViewRoot(node.Operand, names, seen) {
			return true
		}
	}
	return false
}

// ptrReturnDiagnostic rejects a returned pointer rooted in a local binding
// of this function. It reuses findLocalViewRoot rather than a flat RootKind
// check so a pointer widened into Ptr<T> | Nil before return is still
// caught. Parameter-reachable, self-reached, and rootless pointers
// (Heap.allocate, a received parameter) are permitted.
func ptrReturnDiagnostic(node Expression, token lexer.Token, names *scope, writable bool) *compilerTypes.Diagnostic {
	if !findLocalViewRoot(&node, names, make(map[*Expression]bool)) {
		return nil
	}
	label := "Ptr"
	if writable {
		label = "Ptr<mut T>"
	}
	return diagnosticAt(typeErrorAt(token, fmt.Sprintf("a %s cannot be returned when it borrows a local of this function", label)))
}
