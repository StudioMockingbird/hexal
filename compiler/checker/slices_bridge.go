package checker

import (
	diag "hexal/compiler/diagnostics"
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
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.SliceTypeArgumentCount()))}
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
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.InvalidSliceElementType(elementUse.Type.Name)))}
	}
	element := elementUse.Type
	switch property.Lexeme {
	case "from_pointer":
		if len(call.Arguments) != 2 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diag.SliceFromPointerArgumentCount(writable)))}
		}
		pointer := checkValue(call.Arguments[0], ctx)
		if diagnostics := initializerDiagnostics(pointer); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[0]), diagnostics: diagnostics}
		}
		if pointer.typ.Element == nil || compilerTypes.IsNullable(pointer.typ) {
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(messageAt(pointer.token, diag.SlicePointerMustBeNarrowed()))}
		}
		if !compilerTypes.Equal(*pointer.typ.Element, element) {
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(messageAt(pointer.token, diag.SliceFromPointerTypeMismatch(writable, element.Name, pointer.typ.Name)))}
		}
		if writable && !pointer.typ.PointeeWritable {
			return checkedExpression{token: pointer.token, diagnostic: diagnosticAt(messageAt(pointer.token, diag.SliceFromPointerTypeMismatch(writable, element.Name, pointer.typ.Name)))}
		}
		length := checkInitializer(call.Arguments[1], compilerTypes.NewTypeUse(compilerTypes.SizeType), tokenOf(call.Arguments[1]), ctx)
		if diagnostics := initializerDiagnostics(length); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.Arguments[1]), diagnostics: diagnostics}
		}
		if !assignable(compilerTypes.SizeType, length.typ) {
			return checkedExpression{token: length.token, diagnostic: diagnosticAt(messageAt(length.token, diag.SliceLengthNotRepresentableAsSize()))}
		}
		// The region's length, lifetime, alignment, initialization, and
		// provenance are the caller's assertion; every check above is the part
		// the compiler can still prove and runs first.
		if diagnostic := requireUnsafe(ctx, property, unsafeSliceFromPointer, ""); diagnostic != nil {
			return checkedExpression{token: property, diagnostic: diagnostic}
		}
		node := Expression{Kind: SliceBridgeExpression, Name: "from_pointer", Arguments: []Operand{pointer.source, length.source}, OperandType: slice, ResultType: slice, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: slice, Name: "from_pointer", Node: node}
		return checkedExpression{source: source, typ: slice, token: property}
	case "empty":
		if len(call.Arguments) != 0 {
			return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diag.SliceEmptyArguments(writable)))}
		}
		node := Expression{Kind: SliceBridgeExpression, Name: "empty", OperandType: slice, ResultType: slice, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: slice, Name: "empty", Node: node}
		return checkedExpression{source: source, typ: slice, token: property}
	}
	return checkedExpression{token: property, diagnostic: diagnosticAt(messageAt(property, diag.SliceBridgeOperationNotFound(writable)))}
}

// checkSlicePointer resolves the raw-address bridge `slice.pointer()`. It
// exposes the address already present in the Slice, preserving access mode,
// and returns Nil exactly when the Slice has no backing address. It allocates
// and copies nothing and requires unsafe, because the returned address may
// outlive the Slice.
func checkSlicePointer(call methodCall) checkedExpression {
	if call.receiver.typ.Slice == nil {
		diagnostic := messageAt(call.callee.Property, diag.SlicePointerMethodNotFound(call.receiver.typ.Name))
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	if len(call.call.Arguments) != 0 {
		diagnostic := messageAt(call.callee.Property, diag.SlicePointerMethodArguments())
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	element := call.receiver.typ.Slice.Element
	var pointer compilerTypes.Type
	if call.receiver.typ.Slice.Writable {
		pointer = call.ctx.typeEnvironment.MutPtrType(element)
	} else {
		pointer = call.ctx.typeEnvironment.PtrType(element)
	}
	if pointer == (compilerTypes.Type{}) {
		diagnostic := messageAt(call.callee.Property, diag.SlicePointerElementInvalid(element.Name))
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	nullable := call.ctx.typeEnvironment.NullableType(pointer)
	if diagnostic := requireUnsafe(call.ctx, call.callee.Property, unsafeSlicePointer, ""); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	node := Expression{Kind: CollectionMethodCallExpression, Name: "pointer", Operand: &call.receiver.source.Node, OperandType: call.receiver.typ, ResultType: nullable, Element: element}
	source := Operand{Kind: ExpressionOperand, Type: nullable, Name: "pointer", Node: node}
	return checkedExpression{source: source, typ: nullable, token: call.callee.Property}
}

// nodeTracesToRef reports whether a checked node traces to address-taking of// local storage: directly through an address node, or through a binding
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
