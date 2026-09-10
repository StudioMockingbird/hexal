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
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, "View requires exactly one element type"))
	}
	elementUse, diagnostic := resolveTypeUse(expression.Arguments[0], fallback, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	view := typeEnvironment.ViewType(elementUse.Type)
	if view == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(typeErrorAt(expression.Name, elementUse.Type.Name+" is not an inline view element type"))
	}
	return compilerTypes.NewTypeUse(view), nil
}

// checkSliceMethod checks start.slice(begin, end) for Array and View
// receivers. Array receivers must be stable places; View receivers preserve
// their recorded root chain, and slicing a temporary view keeps its original
// stable root. Known constant bounds against a known array length fail at
// compile time; all other invalid ranges trap at runtime.
func checkSliceMethod(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 2 {
		diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("slice expects 2 arguments; got %d", len(call.Arguments)))
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

	viewType := ctx.typeEnvironment.ViewType(element)
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
	if receiver.typ.View != nil {
		// Slicing a View preserves its recorded root chain.
		node.ViewRoots = receiver.source.Node.ViewRoots
		node.RootKind = receiver.source.Node.RootKind
	} else if root := baseBindingID(&receiver.source.Node); root != 0 {
		node.ViewRoots = []BindingID{root}
		node.RootKind = ViewRootBindings
	}
	source := Operand{Kind: ExpressionOperand, Type: viewType, Name: "slice", Node: node}
	return checkedExpression{source: source, typ: viewType, token: callee.Property}
}

// operandNodes projects checked operands to their expression records for
// provenance merging.
func operandNodes(operands []Operand) []Expression {
	nodes := make([]Expression, 0, len(operands))
	for _, operand := range operands {
		nodes = append(nodes, operand.Node)
	}
	return nodes
}

// mergeViewProvenance unions the View root sets of nested value components
// into one construction-level record: any Bindings child wins with the union
// of binding roots in first-seen order; else any Foreign child wins; else
// the merge is rootless. A local root in any component taints the whole
// value, while parameter roots and foreign regions stay returnable. Pointer
// targets are opaque and never contribute: only inline components merge.
func mergeViewProvenance(nodes []Expression) ([]BindingID, ViewRootKind) {
	var roots []BindingID
	seen := make(map[BindingID]bool)
	kind := ViewRootNone
	for _, node := range nodes {
		switch node.RootKind {
		case ViewRootBindings:
			kind = ViewRootBindings
			for _, root := range node.ViewRoots {
				if !seen[root] {
					seen[root] = true
					roots = append(roots, root)
				}
			}
		case ViewRootForeign:
			if kind == ViewRootNone {
				kind = ViewRootForeign
			}
		}
	}
	return roots, kind
}

// viewRootIsLocal reports whether root names storage that dies with the
// enclosing function: a non-parameter, non-receiver binding. This is the
// same ownership test as the direct View return check.
func viewRootIsLocal(root BindingID, names *scope) bool {
	bound, ok := names.lookupBinding(root)
	return ok && !bound.parameter && root != names.selfID
}

// nestedViewReturnDiagnostic rejects a returned aggregate when any nested
// View it contains borrows a local of this function. The walk consumes
// carried roots at View leaves and variable uses, descends through inline
// construction and transparent wrappers, and treats member and index
// projections as their receiver's storage. List, Dict, and pointer targets
// carry no inline provenance and stay out of the walk under the current
// contracts.
func nestedViewReturnDiagnostic(node Expression, token lexer.Token, names *scope) *compilerTypes.Diagnostic {
	if findLocalViewRoot(&node, names, make(map[*Expression]bool)) {
		return diagnosticAt(typeErrorAt(token, "a returned value contains a View that borrows a local of this function"))
	}
	return nil
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

// typeCanContainView reports whether a value of typ can hold a View inline:
// a View itself, or an object, ADT, union, or Array with a View inside.
// List, Dict, and pointer targets are opaque under the current contracts.
// The seen set keys canonical identities so recursive nominal shapes
// terminate; direct by-value recursion is rejected elsewhere, so every
// reachable walk is finite.
func typeCanContainView(typ compilerTypes.Type, seen map[string]bool) bool {
	if typ.View != nil {
		return true
	}
	if key := typ.CanonicalKey; key != "" {
		if seen[key] {
			return false
		}
		seen[key] = true
	}
	switch {
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if typeCanContainView(member.Type, seen) {
				return true
			}
		}
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, field := range variant.Payload {
				if typeCanContainView(field.Type, seen) {
					return true
				}
			}
		}
	case typ.Union != nil:
		members := compilerTypes.UnionMembers(typ)
		for index := 0; index < members.Len(); index++ {
			if member, _ := members.At(index); typeCanContainView(member, seen) {
				return true
			}
		}
	case typ.Array != nil:
		return typeCanContainView(typ.Array.Element, seen)
	}
	if base, ok := compilerTypes.NullableBase(typ); ok {
		return typeCanContainView(base, seen)
	}
	return false
}

// viewReturnDiagnostic rejects a returned View rooted in a local binding of
// this function. Parameter-reachable, foreign (from_pointer), and rootless
// (empty) Views are permitted; a local binding's storage dies at the return.
func viewReturnDiagnostic(node Expression, token lexer.Token, names *scope) *compilerTypes.Diagnostic {
	if node.RootKind != ViewRootBindings {
		return nil
	}
	for _, root := range node.ViewRoots {
		if bound, ok := names.lookupBinding(root); ok && !bound.parameter && root != names.selfID {
			return diagnosticAt(typeErrorAt(token, "a View cannot be returned when it borrows a local of this function"))
		}
	}
	return nil
}

// ptrReturnDiagnostic rejects a returned Ptr/MutPtr rooted in a local binding
// of this function: the same dangling-address shape viewReturnDiagnostic
// already rejects for View, applied to ref's own result. It reuses
// findLocalViewRoot rather than a flat RootKind check so a Ptr widened into
// Ptr<T> | Nil before return is still caught. Parameter-reachable,
// self-reached, and rootless pointers (Heap.allocate, a received parameter)
// are permitted, matching viewReturnDiagnostic's exceptions.
func ptrReturnDiagnostic(node Expression, token lexer.Token, names *scope, writable bool) *compilerTypes.Diagnostic {
	if !findLocalViewRoot(&node, names, make(map[*Expression]bool)) {
		return nil
	}
	label := "Ptr"
	if writable {
		label = "MutPtr"
	}
	return diagnosticAt(typeErrorAt(token, fmt.Sprintf("a %s cannot be returned when it borrows a local of this function", label)))
}
