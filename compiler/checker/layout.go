package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// size_of<T>(), align_of<T>(), and volatile integer pointer accesses.

// layoutBuiltins is the set of protected layout-query names.
var layoutBuiltins = map[string]bool{
	"size_of":  true,
	"align_of": true,
}

// checkLayoutCall resolves size_of<T>() and align_of<T>(): exactly one
// explicit type argument, no value arguments, result Size. The type must be
// complete and representable; a type parameter defers validation to the
// specialization pass, which re-checks the body with concrete arguments.
func checkLayoutCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.TypeArguments) != 1 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.LayoutQueryTypeArgumentCount(callee.Lexeme)))}
	}
	if len(call.Arguments) != 0 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.LayoutQueryValueArguments(callee.Lexeme)))}
	}
	use, diagnostic := resolveTypeUse(call.TypeArguments[0], callee, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return checkedExpression{token: callee, diagnostic: diagnostic}
	}
	if !layoutEligible(use.Type) {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(messageAt(callee, diag.LayoutQueryTypeIncomplete(callee.Lexeme, use.Type.Name)))}
	}
	node := Expression{Kind: LayoutExpression, Name: callee.Lexeme, OperandType: use.Type, ResultType: compilerTypes.SizeType}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: callee.Lexeme, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.SizeType, token: callee}
}

// layoutEligible reports whether one type has a settled generated-C layout:
// complete and finite-sized, excluding Unknown, incomplete types, and
// no-result function types. A type parameter defers the decision to
// specialization, where the concrete argument is validated.
func layoutEligible(typ compilerTypes.Type) bool {
	if typ == (compilerTypes.Type{}) {
		return false
	}
	if compilerTypes.ContainsTypeParameter(typ) {
		return true
	}
	if compilerTypes.IsUnknown(typ) || typ.Incomplete {
		return false
	}
	if typ.Signature != nil {
		return typ.Signature.Result != nil
	}
	return compilerTypes.IsCompleteValue(typ)
}

// volatileEligibleType reports whether one element type supports volatile
// access in v1: exactly the integer storage types.
func volatileEligibleType(typ compilerTypes.Type) bool {
	return compilerTypes.Equal(typ, compilerTypes.Int8) ||
		compilerTypes.Equal(typ, compilerTypes.Int16) ||
		compilerTypes.Equal(typ, compilerTypes.Int32) ||
		compilerTypes.Equal(typ, compilerTypes.Int64) ||
		compilerTypes.Equal(typ, compilerTypes.UInt8) ||
		compilerTypes.Equal(typ, compilerTypes.UInt16) ||
		compilerTypes.Equal(typ, compilerTypes.UInt32) ||
		compilerTypes.Equal(typ, compilerTypes.UInt64) ||
		compilerTypes.IsSize(typ)
}

// checkVolatileCall resolves read_volatile() and write_volatile(value) on
// Ptr<T> and Ptr<mut T> receivers whose element is an integer storage type.
func checkVolatileCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	element := *call.receiver.typ.Element
	if !volatileEligibleType(element) {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.VolatileTypeUnsupported(element.Name)))}
	}
	if diagnostic := freedPointeeDiagnostic(call.receiver, call.callee.Property, call.ctx.names.flow); diagnostic != nil {
		return checkedExpression{token: call.callee.Property, diagnostic: diagnostic}
	}
	switch name {
	case "read_volatile":
		if len(call.call.Arguments) != 0 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.VolatileReadArguments()))}
		}
		node := Expression{Kind: VolatileReadExpression, Operand: &call.receiver.source.Node, OperandType: call.receiver.typ, ResultType: element, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: element, Name: name, Node: node}
		return checkedExpression{source: source, typ: element, token: call.callee.Property}
	case "write_volatile":
		if call.receiver.typ.PointeeWritable == false {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.VolatileWriteReadOnlyPointer(element.Name)))}
		}
		if len(call.call.Arguments) != 1 || len(call.call.TypeArguments) != 0 {
			return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.VolatileWriteArgumentCount()))}
		}
		value := checkInitializer(call.call.Arguments[0], compilerTypes.NewTypeUse(element), tokenOf(call.call.Arguments[0]), call.ctx)
		if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
			return checkedExpression{token: tokenOf(call.call.Arguments[0]), diagnostics: diagnostics}
		}
		if !assignable(element, value.typ) {
			return checkedExpression{token: value.token, diagnostic: diagnosticAt(messageAt(value.token, diag.VolatileWriteTypeMismatch(element.Name, value.typ.Name)))}
		}
		node := Expression{Kind: VolatileWriteExpression, Operand: &call.receiver.source.Node, Arguments: []Operand{value.source}, OperandType: call.receiver.typ, ResultType: compilerTypes.Type{}, Element: element}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: call.callee.Property}
	}
	return checkedExpression{token: call.callee.Property, diagnostic: diagnosticAt(messageAt(call.callee.Property, diag.VolatileOperationUnsupported()))}
}

// The resolved-storage recursion rule: a nominal layout is valid exactly when
// every cycle from the declared type back to itself crosses an
// indirect-storage edge. The walker enters only by-value edges - object
// members, ADT payloads, structural union members, and inline List elements -
// because those are the edges that can sustain an infinite by-value chain;
// entering an indirect handle cannot close a cycle, so the walk stops instead
// of descending. Being on the walk matters, not having been seen: a type
// reached twice along two independent, non-cyclic paths is valid, so
// identities leave the path when their subtree is done.
//
// The classification is a direct switch over the current canonical kind set.
// A future constructed type must be classified here before it can hold a
// nominal value; every kind the switch does not name stops the path, which
// keeps a new storage representation from being treated as by-value storage
// before the rule decides what it is.

// layoutRecursionInvalid reports whether typ reaches the indicated nominal
// identity again while walking only by-value edges. The path set holds the
// identities currently on the walk; a revisit of any of them closes a
// by-value cycle, wherever it passes through, and is reported with the same
// finite-representation rejection the direct self-reference used to get.
func layoutRecursionInvalid(typ compilerTypes.Type, self any, path map[any]bool) bool {
	key := nominalShapeIdentity(typ)
	if key == self {
		return true
	}
	if key != nil {
		if path[key] {
			return true
		}
		path[key] = true
		defer delete(path, key)
	}
	switch {
	// Indirect storage: the cycle resolves through a pointer, so the walk
	// stops without descending. Incomplete, type-parameter, and unknown kinds
	// defer their own validity to specialization rechecking.
	case typ.List != nil, typ.Dict != nil, typ.Pool != nil, typ.Stash != nil,
		typ.Task != nil, typ.Channel != nil, typ.Slice != nil, typ.Signature != nil,
		typ.Element != nil, typ.Incomplete, typ.Generic != nil:
		return false
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if layoutRecursionInvalid(member.Type, self, path) {
				return true
			}
		}
		return false
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if layoutRecursionInvalid(member.Type, self, path) {
					return true
				}
			}
		}
		return false
	case typ.Union != nil:
		for _, member := range typ.Union.Members {
			if layoutRecursionInvalid(member, self, path) {
				return true
			}
		}
		return false
	case typ.InlineList != nil:
		return layoutRecursionInvalid(typ.InlineList.Element, self, path)
	}
	// Scalars, text, Nil, EoS, and any other leaf kind embed no nominal
	// value, so no by-value cycle can pass through them.
	return false
}

// nominalShapeIdentity is the identity a walk carries between by-value edges:
// the pointer behind one nominal record, or nil for every other kind.
func nominalShapeIdentity(typ compilerTypes.Type) any {
	switch {
	case typ.Object != nil:
		return typ.Object
	case typ.Adt != nil:
		return typ.Adt
	}
	return nil
}

// madeIndirectByOwnStorage reports whether one generic-owned type stores its
// element or payload through an indirection: the generated C holds a pointer
// or a type-erased handle, so any cycle crossing that storage is finite.
func madeIndirectByOwnStorage(name string) bool {
	switch name {
	case "List", "Dict", "Pool", "Stash", "Task", "Channel", "Fun":
		return true
	}
	return false
}

// writtenLayoutSelfRecursion reports whether the self name is reachable from
// a member's written type through by-value spellings only. Storage classes
// stay consistent with the resolved walker: pointer, Slice, and every
// indirect-handle spelling stop the path, inline `List<T, N>` is the one
// generic whose element is by-value storage, and the self name itself closes
// a by-value cycle. The declaration-time pass keeps the finite-representation
// diagnostic ahead of provisional-resolution failures in the members' own
// constructors; the resolved walker over substituted types re-checks the same
// rule after generic substitution, where the self name is spelled as the open
// parameter rather than the declared name.
func writtenLayoutSelfRecursion(expression parser.TypeExpression, selfName string) bool {
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		return expression.Name.Lexeme == selfName
	case parser.GenericTypeExpression:
		if expression.Name.Lexeme == selfName {
			return true
		}
		if madeIndirectByOwnStorage(expression.Name.Lexeme) {
			if expression.Name.Lexeme == "List" && len(expression.Arguments) >= 2 {
				// List<T, N> is the inline List: its element is by-value
				// storage, so the walk descends into the element.
				for _, argument := range expression.Arguments {
					if writtenLayoutSelfRecursion(argument, selfName) {
						return true
					}
				}
			}
			return false
		}
		for _, argument := range expression.Arguments {
			if writtenLayoutSelfRecursion(argument, selfName) {
				return true
			}
		}
		return false
	case parser.PtrTypeExpression:
		return false
	case parser.SliceTypeExpression:
		return false
	case parser.StringTypeExpression:
		return false
	case parser.FunctionTypeExpression:
		return false
	case parser.GroupedTypeExpression:
		return writtenLayoutSelfRecursion(expression.Inner, selfName)
	case parser.UnionTypeExpression:
		for _, member := range expression.Members {
			if writtenLayoutSelfRecursion(member, selfName) {
				return true
			}
		}
		return false
	case parser.ObjectTypeExpression:
		for _, member := range expression.Members {
			if writtenLayoutSelfRecursion(member.Type, selfName) {
				return true
			}
		}
		return false
	}
	// Nil, Unknown, qualified module spellings, and literal-capacity
	// arguments cannot name the declaring nominal type.
	return false
}
