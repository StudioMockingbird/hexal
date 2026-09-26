package checker

import (
	"go/constant"
	"strconv"

	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// resolveArrayTypeUse resolves the built-in Array<T, N> form. N must be a
// positive decimal literal, and T must be an inline element class.
func resolveArrayTypeUse(expression parser.ArrayTypeExpression, fallback lexer.Token, typeEnvironment *compilerTypes.Environment, generics *genericTable) (compilerTypes.TypeUse, *compilerTypes.Diagnostic) {
	elementUse, diagnostic := resolveTypeUse(expression.Element, expression.Keyword, typeEnvironment, generics)
	if diagnostic != nil {
		return compilerTypes.TypeUse{}, diagnostic
	}
	length, err := strconv.ParseUint(expression.Length.Lexeme, 10, 64)
	if err != nil || length == 0 {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Length, diag.InvalidArrayLength()))
	}
	array := typeEnvironment.ArrayType(elementUse.Type, length)
	if array == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, diagnosticAt(messageAt(expression.Keyword, diag.InvalidArrayElementType(elementUse.Type.Name)))
	}
	return compilerTypes.NewTypeUse(array), nil
}

// checkArrayLiteral checks a bracket literal against an expected Array<T, N>
// destination. The literal must contain exactly N elements, each assignable
// to T, evaluated left-to-right.
func checkArrayLiteral(expression parser.ArrayLiteralExpression, expected compilerTypes.Type, ctx checkContext) checkedExpression {
	if len(expression.Elements) == 0 {
		return checkedExpression{token: expression.OpenBracket, diagnostic: diagnosticAt(messageAt(expression.OpenBracket, diag.EmptyArrayLiteral()))}
	}
	if expected.Array == nil {
		return checkedExpression{token: expression.OpenBracket, diagnostic: diagnosticAt(messageAt(expression.OpenBracket, diag.ArrayLiteralNeedsContext()))}
	}
	length := expected.Array.Length
	if uint64(len(expression.Elements)) != length {
		return checkedExpression{token: expression.OpenBracket, diagnostic: diagnosticAt(messageAt(expression.OpenBracket, diag.ArrayLiteralElementCount(expected.Array.Element.Name, length, len(expression.Elements))))}
	}
	elementUse := compilerTypes.NewTypeUse(expected.Array.Element)
	diagnostics := make(compilerTypes.Diagnostics, 0)
	elements := make([]Operand, 0, len(expression.Elements))
	for _, element := range expression.Elements {
		checked := checkInitializer(element, elementUse, expression.OpenBracket, ctx)
		if nested := initializerDiagnostics(checked); len(nested) > 0 {
			diagnostics = append(diagnostics, nested...)
			continue
		}
		if !assignable(expected.Array.Element, checked.typ) {
			diagnostics = append(diagnostics, typeMismatchDiagnostic(expected.Array.Element, checked.typ, checked.token))
			continue
		}
		elements = append(elements, checked.source)
	}
	if len(diagnostics) > 0 {
		return checkedExpression{token: expression.OpenBracket, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	node := Expression{
		Kind:        ArrayLiteralExpression,
		OperandType: expected.Array.Element,
		ResultType:  expected,
		Arguments:   elements,
	}
	source := Operand{Kind: ExpressionOperand, Type: expected, Node: node}
	return checkedExpression{source: source, typ: expected, token: expression.OpenBracket}
}

// checkArrayIndex checks one index operand of an array access: it must be an
// integer scalar, and a known constant must be non-negative. Constant bounds
// against a known array length are checked by the caller. The known-value
// metadata is returned alongside the operand so the caller's constant-required
// bounds check sees through reads of named immutable bindings.
func checkArrayIndex(expression parser.Expression, fallback lexer.Token, ctx checkContext) (Operand, *Operand, *compilerTypes.Diagnostic) {
	checked := checkExpression(expression, expressionContext{}, ctx)
	if nested := initializerDiagnostics(checked); len(nested) > 0 {
		return Operand{}, nil, &nested[0]
	}
	if !compilerTypes.IsInteger(checked.typ) {
		diagnostic := messageAt(checked.token, diag.ArrayIndexNotInteger(checked.typ.Name))
		return Operand{}, nil, &diagnostic
	}
	if checked.known != nil && checked.known.Kind == ConstantOperand && checked.known.Constant != nil && checked.known.Constant.Kind() == constant.Int {
		if value, exact := constant.Int64Val(checked.known.Constant); exact && value < 0 {
			diagnostic := messageAt(checked.token, diag.ArrayIndexNegative())
			return Operand{}, nil, &diagnostic
		}
	}
	return checked.source, checked.known, nil
}

// checkIndexPlace resolves array[index] or slice[index] as a place:
// readable always, writable for a mutable Array, a writable Slice, or any
// List. A read-only Slice element place is never writable, though a MutPtr
// element's pointee keeps its own capability.
func checkIndexPlace(expression parser.IndexExpression, ctx checkContext) checkedExpression {
	var receiver checkedExpression
	if _, temporary := expression.Receiver.(parser.CallExpression); temporary {
		// A call result (including a struct or ADT-variant constructor) is
		// never a place; index it as the temporary value it is rather than
		// rejecting it through checkPlace's default case.
		receiver = checkValue(expression.Receiver, ctx)
	} else {
		receiver = checkPlace(expression.Receiver, ctx)
	}
	if receiver.diagnostic != nil {
		return receiver
	}
	element := compilerTypes.Type{}
	writable := false
	if receiver.typ.Array != nil {
		element = receiver.typ.Array.Element
		writable = receiver.source.Writable
	} else if receiver.typ.Slice != nil {
		element = receiver.typ.Slice.Element
		writable = receiver.typ.Slice.Writable
	} else if receiver.typ.List != nil {
		// Every live List reference permits mutation through the heap
		// object; no mut binding is required.
		element = receiver.typ.List.Element
		writable = true
	}
	if compilerTypes.IsText(receiver.typ) {
		// Text is UTF-8 and is a sequence of bytes, not of characters, so
		// indexing it would read as character access. bytes() gives indexed
		// byte access in constant time and names the unit.
		diagnostic := messageAt(expression.OpenBracket, diag.TextCannotBeIndexed(receiver.typ.Name))
		return checkedExpression{token: expression.OpenBracket, diagnostic: &diagnostic}
	}
	if element == (compilerTypes.Type{}) && receiver.typ.Element != nil {
		// A pointer indexes raw addresses rather than a bounded collection,
		// so it owns its own place rule.
		return checkPointerIndexPlace(expression, receiver, ctx)
	}
	if element == (compilerTypes.Type{}) {
		diagnostic := messageAt(expression.OpenBracket, diag.ValueNotIndexable(receiver.typ.Name))
		return checkedExpression{token: expression.OpenBracket, diagnostic: &diagnostic}
	}
	index, indexKnown, diagnostic := checkArrayIndex(expression.Index, expression.OpenBracket, ctx)
	if diagnostic != nil {
		return checkedExpression{token: expression.OpenBracket, diagnostic: diagnostic}
	}
	if indexKnown != nil && indexKnown.Constant != nil && indexKnown.Constant.Kind() == constant.Int && receiver.typ.Array != nil {
		if value, exact := constant.Uint64Val(indexKnown.Constant); exact && value >= receiver.typ.Array.Length {
			indexToken := tokenOf(expression.Index)
			diagnostic := messageAt(indexToken, diag.ArrayIndexOutOfBounds(value, receiver.typ.Name))
			return checkedExpression{token: expression.OpenBracket, diagnostic: &diagnostic}
		}
	}
	receiverNode := receiver.source.Node
	if receiver.storageType.Union != nil && !compilerTypes.IsUnion(receiver.typ) {
		receiverNode = valueFromPlace(receiver).source.Node
	}
	checked := checkedExpression{
		source: Operand{
			Kind: VariableOperand,
			Type: element,
			Node: Expression{
				Kind:        IndexExpression,
				Operand:     &receiverNode,
				Arguments:   []Operand{index},
				OperandType: receiver.typ,
				ResultType:  element,
			},
			Addressable: receiver.source.Addressable,
			Writable:    writable,
			// An element of a rest-backed Slice is a place inside the
			// invocation-owned region; its address must not escape.
			RestRegionPlace: receiver.source.RestBacked,
		},
		typ:   element,
		token: expression.OpenBracket,
	}
	return checked
}

// checkCollectionMethodCall dispatches the built-in Array and Slice methods
// length, slice, and mut_slice. The mutable form exists only for Array:
// re-slicing a Slice preserves the receiver's access mode through slice.
func checkCollectionMethodCall(call methodCall) checkedExpression {
	name := call.callee.Property.Lexeme
	collectionType := call.receiver.typ
	// Slice has no mut_slice: re-slicing preserves the receiver's access mode
	// through slice. The registry does not declare it, and its rejection names
	// the migration route, so it stays ahead of the registry gate.
	if name == "mut_slice" && call.receiver.typ.Slice != nil {
		diagnostic := messageAt(call.callee.Property, diag.SliceHasNoMutSlice())
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	if !hasBuiltinMethod(collectionType, name) {
		diagnostic := messageAt(call.callee.Property, diag.CollectionHasNoMethod(collectionType.Name, name))
		return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
	}
	switch name {
	case "length":
		if len(call.call.Arguments) != 0 {
			diagnostic := messageAt(call.callee.Property, diag.CollectionMethodNoArguments("length"))
			return checkedExpression{token: call.callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &call.receiver.source.Node, OperandType: collectionType, ResultType: compilerTypes.SizeType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: call.callee.Property}
	case "slice":
		return checkSliceMethod(call, false)
	case "mut_slice":
		return checkSliceMethod(call, true)
	case "pointer":
		return checkSlicePointer(call)
	default:
		return unexpectedBuiltinMethod(collectionType, call.callee.Property)
	}
}

// tokenOf recovers the anchor token of an expression for diagnostics.
func tokenOf(expression parser.Expression) lexer.Token {
	switch expression := expression.(type) {
	case parser.IntegerLiteral:
		return expression.Token
	case parser.VariableExpression:
		return expression.Name
	case parser.BinaryExpression:
		return expression.Operator
	default:
		return lexer.Token{Kind: lexer.Identifier, Lexeme: "index", Line: 0, Column: 0}
	}
}
