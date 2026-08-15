package checker

import (
	"fmt"
	"go/constant"
	"strconv"

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
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Length.Line,
			Column:   expression.Length.Column,
			Message:  "an array length must be a positive decimal integer",
		}
	}
	array := typeEnvironment.ArrayType(elementUse.Type, length)
	if array == (compilerTypes.Type{}) {
		return compilerTypes.TypeUse{}, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Keyword.Line,
			Column:   expression.Keyword.Column,
			Message:  elementUse.Type.Name + " is not an inline array element type",
		}
	}
	return compilerTypes.NewTypeUse(array), nil
}

// checkArrayLiteral checks a bracket literal against an expected Array<T, N>
// destination. The literal must contain exactly N elements, each assignable
// to T, evaluated left-to-right.
func checkArrayLiteral(expression parser.ArrayLiteralExpression, expected compilerTypes.Type, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(expression.Elements) == 0 {
		return checkedExpression{token: expression.OpenBracket, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.OpenBracket.Line,
			Column:   expression.OpenBracket.Column,
			Message:  "an array literal requires at least one element",
		}}
	}
	if expected.Array == nil {
		return checkedExpression{token: expression.OpenBracket, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.OpenBracket.Line,
			Column:   expression.OpenBracket.Column,
			Message:  "an array literal requires an expected Array<T, N> destination type",
		}}
	}
	length := expected.Array.Length
	if uint64(len(expression.Elements)) != length {
		return checkedExpression{token: expression.OpenBracket, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.OpenBracket.Line,
			Column:   expression.OpenBracket.Column,
			Message:  fmt.Sprintf("Array<%s, %d> requires exactly %d elements, got %d", expected.Array.Element.Name, length, length, len(expression.Elements)),
		}}
	}
	elementUse := compilerTypes.NewTypeUse(expected.Array.Element)
	diagnostics := make(compilerTypes.Diagnostics, 0)
	elements := make([]Operand, 0, len(expression.Elements))
	for _, element := range expression.Elements {
		checked := checkInitializer(element, elementUse, expression.OpenBracket, environment, typeEnvironment)
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
func checkArrayIndex(expression parser.Expression, fallback lexer.Token, environment *scope, typeEnvironment *compilerTypes.Environment) (Operand, *Operand, *compilerTypes.Diagnostic) {
	checked := checkExpression(expression, expressionContext{}, environment, typeEnvironment)
	if nested := initializerDiagnostics(checked); len(nested) > 0 {
		return Operand{}, nil, &nested[0]
	}
	if !compilerTypes.IsInteger(checked.typ) {
		diagnostic := typeErrorAt(checked.token, "an array index must be an integer; got "+checked.typ.Name)
		return Operand{}, nil, &diagnostic
	}
	if checked.known != nil && checked.known.Kind == ConstantOperand && checked.known.Constant != nil && checked.known.Constant.Kind() == constant.Int {
		if value, exact := constant.Int64Val(checked.known.Constant); exact && value < 0 {
			diagnostic := typeErrorAt(checked.token, "an array index must be non-negative")
			return Operand{}, nil, &diagnostic
		}
	}
	return checked.source, checked.known, nil
}

// checkIndexPlace resolves array[index] or view[index] as a place: readable
// always, writable only for a mutable Array. A View element place is never
// writable, though a MutPtr element's pointee keeps its own capability.
func checkIndexPlace(expression parser.IndexExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	var receiver checkedExpression
	if _, temporary := expression.Receiver.(parser.ObjectLiteral); temporary {
		receiver = checkValue(expression.Receiver, environment, typeEnvironment)
	} else {
		receiver = checkPlace(expression.Receiver, environment, typeEnvironment)
	}
	if receiver.diagnostic != nil {
		return receiver
	}
	element := compilerTypes.Type{}
	writable := false
	if receiver.typ.Array != nil {
		element = receiver.typ.Array.Element
		writable = receiver.source.Writable
	} else if receiver.typ.View != nil {
		element = receiver.typ.View.Element
	} else if receiver.typ.List != nil {
		// Every live List reference permits mutation through the heap
		// object; no mut binding is required.
		element = receiver.typ.List.Element
		writable = true
	} else if compilerTypes.IsString(receiver.typ) || compilerTypes.IsStrand(receiver.typ) {
		// RFC 0044: text indexing is rune-oriented and read-only.
		element = compilerTypes.Rune
	}
	if element == (compilerTypes.Type{}) {
		diagnostic := typeErrorAt(expression.OpenBracket, "cannot index "+receiver.typ.Name+"; expected Array<T, N>, View<T>, List<T>, String, or Strand")
		return checkedExpression{token: expression.OpenBracket, diagnostic: &diagnostic}
	}
	index, indexKnown, diagnostic := checkArrayIndex(expression.Index, expression.OpenBracket, environment, typeEnvironment)
	if diagnostic != nil {
		return checkedExpression{token: expression.OpenBracket, diagnostic: diagnostic}
	}
	if indexKnown != nil && indexKnown.Constant != nil && indexKnown.Constant.Kind() == constant.Int && receiver.typ.Array != nil {
		if value, exact := constant.Uint64Val(indexKnown.Constant); exact && value >= receiver.typ.Array.Length {
			indexToken := tokenOf(expression.Index)
			diagnostic := typeErrorAt(indexToken, fmt.Sprintf("array index %d is out of bounds for %s", value, receiver.typ.Name))
			return checkedExpression{token: expression.OpenBracket, diagnostic: &diagnostic}
		}
	}
	checked := checkedExpression{
		source: Operand{
			Kind: VariableOperand,
			Type: element,
			Node: Expression{
				Kind:        IndexExpression,
				Operand:     &receiver.source.Node,
				Arguments:   []Operand{index},
				OperandType: receiver.typ,
				ResultType:  element,
			},
			Addressable: receiver.source.Addressable,
			Writable:    writable,
		},
		typ:   element,
		token: expression.OpenBracket,
	}
	return checked
}

// checkCollectionMethodCall dispatches the built-in Array and View methods
// length and slice (RFC 0063: is_empty and at were removed — their
// replacements `length() == 0` and `[index]` are identical and O(1)).
func checkCollectionMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := callee.Property.Lexeme
	collectionType := receiver.typ
	switch name {
	case "length":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "length expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: CollectionMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: collectionType, ResultType: compilerTypes.SizeType}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: callee.Property}
	case "slice":
		return checkSliceMethod(call, callee, receiver, environment, typeEnvironment)
	case "at":
		diagnostic := typeErrorAt(callee.Property, "`at` was removed; use `receiver[index]`")
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	case "is_empty":
		diagnostic := typeErrorAt(callee.Property, "`is_empty` was removed for Array, View, and List; use `receiver.length() == 0`")
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	default:
		diagnostic := typeErrorAt(callee.Property, collectionType.Name+" has no method "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
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
