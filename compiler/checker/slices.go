package checker

import (
	"go/constant"

	diag "hexal/compiler/diagnostics"
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
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Keyword, diag.InvalidSliceElementType(elementUse.Type.Name)))
	}
	return compilerTypes.NewTypeUse(slice), nil
}

// checkSliceMethod checks start.slice(begin, end) and start.mut_slice(begin,
// end) for inline List, allocated List, and Slice receivers. The mutable form
// is valid only on Lists: re-slicing a Slice preserves the call.receiver's own
// access mode through slice. Inline Lists require stable places and writable
// access for mut_slice; allocated Lists permit interior element mutation.
// Known constant bounds fail at compile time when provable; other invalid
// ranges trap at runtime.
func checkSliceMethod(call methodCall, mutable bool) checkedExpression {
	name := "slice"
	if mutable {
		name = "mut_slice"
	}
	if len(call.call.Arguments) != 2 {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.SliceMethodArgumentCount(name, len(call.call.Arguments))))}
	}
	start, startKnown, diagnostic := checkSequenceIndex(call.call.Arguments[0], call.callee.Property, call.ctx)
	if diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	end, endKnown, diagnostic := checkSequenceIndex(call.call.Arguments[1], call.callee.Property, call.ctx)
	if diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}

	var element compilerTypes.Type
	kind := ""
	capacity := uint64(0)
	writable := mutable
	switch {
	case call.receiver.typ.List != nil:
		element = call.receiver.typ.List.Element
		kind = "List"
	case call.receiver.typ.InlineList != nil:
		element = call.receiver.typ.InlineList.Element
		capacity = call.receiver.typ.InlineList.Capacity
		kind = "inline List"
	case call.receiver.typ.Slice != nil:
		element = call.receiver.typ.Slice.Element
		writable = call.receiver.typ.Slice.Writable
	}
	if kind != "" {
		// A slice must be rooted in stable storage. A temporary List has no
		// addressable storage.
		if !call.receiver.source.Addressable {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.SliceRootedInTemporary(kind)))}
		}
	}
	if call.receiver.typ.InlineList != nil {
		if mutable && !call.receiver.source.Writable {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.InlineListMutationRequiresWritable()))}
		}
		if startKnown != nil && startKnown.Constant != nil && startKnown.Constant.Kind() == constant.Int &&
			endKnown != nil && endKnown.Constant != nil && endKnown.Constant.Kind() == constant.Int {
			startValue, startExact := constant.Int64Val(startKnown.Constant)
			endValue, endExact := constant.Int64Val(endKnown.Constant)
			if startExact && endExact && (startValue > endValue || endValue < 0 || uint64(endValue) > capacity) {
				return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(tokenOf(call.call.Arguments[0]), diag.SliceRangeOutOfBounds(startValue, endValue, call.receiver.typ.Name)))}
			}
		}
	}

	sliceType := call.ctx.typeEnvironment.SliceType(element, writable)
	if sliceType == (compilerTypes.Type{}) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.InvalidSliceElementType(element.Name)))}
	}
	node := Expression{
		Kind:        CollectionSliceExpression,
		Name:        name,
		Operand:     &call.receiver.source.Node,
		Arguments:   []Operand{start, end},
		OperandType: call.receiver.typ,
		ResultType:  sliceType,
		Element:     element,
	}
	source := Operand{Kind: ExpressionOperand, Type: sliceType, Name: name, Node: node}
	// A Slice derived from a rest-backed call.receiver stays rest-backed: its
	// backing region is the same invocation-owned region.
	source.RestBacked = call.receiver.source.RestBacked
	return checkedExpression{source: source, typ: sliceType, token: call.callee.Property}
}

// viewRootIsLocal reports whether root names storage that dies with the
// enclosing function: a non-parameter, non-call.receiver binding.
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
	case AdtConstructExpression, InlineListLiteralExpression, MatchExpression:
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
	return diagnosticAt(messageAt(token, diag.PointerReturnBorrowsLocal(label)))
}
