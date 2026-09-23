package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// Text is one family of validated UTF-8 bytes: the heap String and every
// inline String<N>. This file checks its literals, its constructors, its
// methods, and the one place text coerces into a bounded capacity implicitly.

// checkStringLiteral resolves a string literal into a text value carrying its
// decoded payload. In a String<N> position the same payload becomes an inline
// value, checked against N at compile time.
func checkStringLiteral(expression parser.StringLiteral, expected compilerTypes.Type) checkedExpression {
	payload, diagnostic := decodeStringLiteral(expression.Token)
	if diagnostic != nil {
		return checkedExpression{token: expression.Token, diagnostic: diagnostic}
	}
	return checkedStringLiteralValue(payload, expected, expression.Token)
}

// checkRawStringLiteral resolves a raw string literal: byte-for-byte identical
// to an interpreted literal containing the same UTF-8 bytes, since raw content
// performs no escape or interpolation processing.
func checkRawStringLiteral(expression parser.RawStringLiteral, expected compilerTypes.Type) checkedExpression {
	payload, diagnostic := decodeRawStringLiteral(expression.Token)
	if diagnostic != nil {
		return checkedExpression{token: expression.Token, diagnostic: diagnostic}
	}
	return checkedStringLiteralValue(payload, expected, expression.Token)
}

// checkedStringLiteralValue builds the checked String or String<N> value
// shared by an interpreted and a raw string literal, once each has decoded its
// own payload bytes.
func checkedStringLiteralValue(payload []byte, expected compilerTypes.Type, token lexer.Token) checkedExpression {
	resultType := compilerTypes.StringType
	if compilerTypes.IsInlineString(expected) {
		capacity := expected.InlineString.Capacity
		if uint64(len(payload)) > capacity {
			message := fmt.Sprintf("%s literal exceeds %d UTF-8 bytes", expected.Name, capacity)
			return checkedExpression{token: token, diagnostic: diagnosticAt(typeErrorAt(token, message))}
		}
		resultType = expected
	}
	node := Expression{
		Kind:       StringLiteralExpression,
		Name:       string(payload),
		ResultType: resultType,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

// checkBoundedText checks one argument that coerces into a bounded inline
// capacity: the message of Error(kind, message) or the header of
// ErrorKind.Other. These are the only two sites where text of any form
// converts to a fixed capacity implicitly, because error construction is the
// one operation whose failure has nowhere to go. A literal is measured at
// compile time; any other text is measured when it is copied, and traps when
// it does not fit. Nothing is ever truncated.
func checkBoundedText(expression parser.Expression, destination compilerTypes.Type, label string, ctx checkContext) checkedExpression {
	var literal []byte
	var literalToken lexer.Token
	switch expression := expression.(type) {
	case parser.StringLiteral:
		payload, diagnostic := decodeStringLiteral(expression.Token)
		if diagnostic != nil {
			return checkedExpression{token: expression.Token, diagnostic: diagnostic}
		}
		literal, literalToken = payload, expression.Token
	case parser.RawStringLiteral:
		payload, diagnostic := decodeRawStringLiteral(expression.Token)
		if diagnostic != nil {
			return checkedExpression{token: expression.Token, diagnostic: diagnostic}
		}
		literal, literalToken = payload, expression.Token
	}
	if literalToken.Line != 0 {
		capacity := destination.InlineString.Capacity
		if uint64(len(literal)) > capacity {
			message := fmt.Sprintf("%s literal exceeds %d UTF-8 bytes", label, capacity)
			return checkedExpression{token: literalToken, diagnostic: diagnosticAt(typeErrorAt(literalToken, message))}
		}
		return checkedStringLiteralValue(literal, destination, literalToken)
	}
	value := checkValue(expression, ctx)
	if diagnostics := initializerDiagnostics(value); len(diagnostics) > 0 {
		return checkedExpression{token: tokenOf(expression), diagnostics: diagnostics}
	}
	if !compilerTypes.IsText(value.typ) {
		return checkedExpression{token: value.token, diagnostic: diagnosticAt(typeErrorAt(value.token, label+" requires text; got "+value.typ.Name))}
	}
	if compilerTypes.Equal(value.typ, destination) {
		return value
	}
	needsCheck := 1
	if compilerTypes.IsInlineString(value.typ) && value.typ.InlineString.Capacity <= destination.InlineString.Capacity {
		needsCheck = 0
	}
	node := Expression{
		Kind:        TextCoerceExpression,
		Name:        label,
		Operand:     &value.source.Node,
		OperandType: value.typ,
		ResultType:  destination,
		MemberIndex: needsCheck,
	}
	source := Operand{Kind: ExpressionOperand, Type: destination, Name: "coerce", Node: node}
	return checkedExpression{source: source, typ: destination, token: value.token}
}

// stringTypeCallUsage is the shared "no such operation" diagnostic text for
// every unrecognized String.<name>(...) call.
const stringTypeCallUsage = "String has no such operation; use String.from_bytes(heap, view), String.from_runes(heap, runes), or String.interpolate(heap, template)"

// checkStringTypeCall resolves a call written as String.<name>(...) or
// String<N>.<name>(...): the built-in constructors from_bytes, concat, and
// interpolate. The heap form takes a Heap first; the inline form names its
// capacity and takes none.
func checkStringTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	name := call.Callee.(parser.PropertyExpression).Property.Lexeme
	if len(call.TypeArguments) != 0 {
		if len(call.TypeArguments) != 1 {
			return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "String takes at most one capacity argument"))}
		}
		capacity, diagnostic := inlineStringCapacity(call.TypeArguments[0], callee)
		if diagnostic != nil {
			return checkedExpression{token: callee, diagnostic: diagnostic}
		}
		return checkInlineStringTypeCall(call, callee, name, ctx.typeEnvironment.InlineStringType(capacity), ctx)
	}
	switch name {
	case "interpolate":
		return checkStringInterpolate(call, callee, ctx)
	case "from_bytes":
		return checkHeapFromBytes(call, callee, ctx)
	case "from_runes":
		return checkHeapFromRunes(call, callee, ctx)
	}
	return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, stringTypeCallUsage))}
}

// textFailureUnion is the `success | Error` result of every text operation
// that can fail.
func textFailureUnion(success compilerTypes.Type, callee lexer.Token, ctx checkContext) (compilerTypes.Type, *checkedExpression) {
	union := ctx.typeEnvironment.UnionType([]compilerTypes.Type{success, compilerTypes.ErrorType})
	if union == (compilerTypes.Type{}) {
		diagnostic := typeErrorAt(callee, success.Name+" has no result union with Error")
		return compilerTypes.Type{}, &checkedExpression{token: callee, diagnostic: &diagnostic}
	}
	return union, nil
}

// checkByteView checks one operand that must be a Slice<Byte>, the universal
// read-only text parameter: bytes() gives one from any text form in O(1).
func checkByteView(argument parser.Expression, label string, ctx checkContext) (checkedExpression, *checkedExpression) {
	view := checkValue(argument, ctx)
	if diagnostics := initializerDiagnostics(view); len(diagnostics) > 0 {
		return checkedExpression{}, &view
	}
	if view.typ.Slice == nil || !compilerTypes.Equal(view.typ.Slice.Element, compilerTypes.UInt8) {
		diagnostic := typeErrorAt(view.token, label+" requires Slice<Byte>; got "+view.typ.Name)
		return checkedExpression{}, &checkedExpression{token: view.token, diagnostic: &diagnostic}
	}
	return view, nil
}

// checkHeapFromBytes resolves String.from_bytes(heap, bytes).
func checkHeapFromBytes(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 2 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, stringTypeCallUsage))}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := typeErrorAt(heap.token, "String.from_bytes requires a Heap; got "+heap.typ.Name)
		return checkedExpression{token: heap.token, diagnostic: &diagnostic}
	}
	view, failure := checkByteView(call.Arguments[1], "String.from_bytes", ctx)
	if failure != nil {
		return *failure
	}
	union, failure := textFailureUnion(compilerTypes.StringType, callee, ctx)
	if failure != nil {
		return *failure
	}
	node := Expression{
		Kind:         StringFromBytesExpression,
		Operand:      &heap.source.Node,
		Arguments:    []Operand{view.source},
		OperandType:  compilerTypes.Heap,
		ResultType:   union,
		SourceLine:   callee.Line,
		SourceColumn: callee.Column,
	}
	source := Operand{Kind: ExpressionOperand, Type: union, Name: "from_bytes", Node: node}
	return checkedExpression{source: source, typ: union, token: callee}
}

// checkHeapFromRunes resolves String.from_runes(heap, runes): it encodes a
// scalar sequence into one owned heap String, rejecting surrogates and values
// above U+10FFFF as InvalidInput.
func checkHeapFromRunes(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 2 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, stringTypeCallUsage))}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := typeErrorAt(heap.token, "String.from_runes requires a Heap; got "+heap.typ.Name)
		return checkedExpression{token: heap.token, diagnostic: &diagnostic}
	}
	runes := checkValue(call.Arguments[1], ctx)
	if diagnostics := initializerDiagnostics(runes); len(diagnostics) > 0 {
		return runes
	}
	if runes.typ.Slice == nil || !compilerTypes.Equal(runes.typ.Slice.Element, compilerTypes.Rune) {
		diagnostic := typeErrorAt(runes.token, "String.from_runes requires Slice<Rune>; got "+runes.typ.Name)
		return checkedExpression{token: runes.token, diagnostic: &diagnostic}
	}
	union, failure := textFailureUnion(compilerTypes.StringType, callee, ctx)
	if failure != nil {
		return *failure
	}
	node := Expression{
		Kind:         StringFromRunesExpression,
		Operand:      &heap.source.Node,
		Arguments:    []Operand{runes.source},
		OperandType:  compilerTypes.Heap,
		ResultType:   union,
		SourceLine:   callee.Line,
		SourceColumn: callee.Column,
	}
	source := Operand{Kind: ExpressionOperand, Type: union, Name: "from_runes", Node: node}
	return checkedExpression{source: source, typ: union, token: callee}
}

// checkInlineStringTypeCall resolves String<N>.from_bytes(bytes),
// String<N>.concat(left, right), and String<N>.interpolate(template). Each
// yields String<N> | Error, since the operands may not fit or may not be
// well-formed. None takes a Heap: no allocation happens.
func checkInlineStringTypeCall(call parser.CallExpression, callee lexer.Token, name string, destination compilerTypes.Type, ctx checkContext) checkedExpression {
	usage := destination.Name + " has no such operation; use " + destination.Name + ".from_bytes(view), " +
		destination.Name + ".concat(left, right), or " + destination.Name + ".interpolate(template)"
	label := destination.Name + "." + name
	var arguments []Operand
	var segments []InterpolationSegment
	switch name {
	case "from_bytes", "concat":
		want := 1
		if name == "concat" {
			want = 2
		}
		if len(call.Arguments) != want {
			diagnostic := typeErrorAt(callee, fmt.Sprintf("%s expects %d arguments; got %d", label, want, len(call.Arguments)))
			return checkedExpression{token: callee, diagnostic: &diagnostic}
		}
		for _, argument := range call.Arguments {
			view, failure := checkByteView(argument, label, ctx)
			if failure != nil {
				return *failure
			}
			arguments = append(arguments, view.source)
		}
	case "interpolate":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee, fmt.Sprintf("%s expects 1 argument; got %d", label, len(call.Arguments)))
			return checkedExpression{token: callee, diagnostic: &diagnostic}
		}
		checked, failure := checkInterpolationTemplate(call.Arguments[0], label, callee, ctx)
		if failure != nil {
			return *failure
		}
		segments = checked
	default:
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, usage))}
	}
	union, failure := textFailureUnion(destination, callee, ctx)
	if failure != nil {
		return *failure
	}
	node := Expression{
		Kind:                  InlineStringConstructExpression,
		Name:                  name,
		Arguments:             arguments,
		OperandType:           destination,
		ResultType:            union,
		InterpolationSegments: segments,
		SourceLine:            callee.Line,
		SourceColumn:          callee.Column,
	}
	source := Operand{Kind: ExpressionOperand, Type: union, Name: name, Node: node}
	return checkedExpression{source: source, typ: union, token: callee}
}

// interpolationSupportedType reports whether typ may appear as an embedded
// interpolation value: Bool, every fixed-width signed and unsigned integer,
// Size, Byte, Float32, Float64, and text of any form. Every other type -- Nil,
// pointers, unions, structs, ADTs, arrays, slices, lists, dictionaries,
// allocators, concurrency values, Error, and Fun -- is deliberately excluded.
func interpolationSupportedType(typ compilerTypes.Type) bool {
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		return true
	case compilerTypes.IsSignedInteger(typ), compilerTypes.IsUnsignedInteger(typ):
		return true
	case compilerTypes.Equal(typ, compilerTypes.Float32), compilerTypes.Equal(typ, compilerTypes.Float64):
		return true
	case compilerTypes.IsText(typ):
		return true
	}
	return false
}

// checkInterpolationTemplate checks one interpolation template argument: its
// literal-text segments decode, and each embedded expression is checked left
// to right in the surrounding lexical scope against the supported value set.
// label names the operation in diagnostics.
func checkInterpolationTemplate(argument parser.Expression, label string, callee lexer.Token, ctx checkContext) ([]InterpolationSegment, *checkedExpression) {
	template, isTemplate := argument.(parser.InterpolationTemplateExpression)
	if !isTemplate {
		if _, isPlainString := argument.(parser.StringLiteral); isPlainString {
			diagnostic := typeErrorAt(callee, label+" requires at least one interpolation")
			return nil, &checkedExpression{token: callee, diagnostic: &diagnostic}
		}
		diagnostic := typeErrorAt(callee, label+" requires an interpreted interpolation template")
		return nil, &checkedExpression{token: callee, diagnostic: &diagnostic}
	}
	segments := make([]InterpolationSegment, 0, len(template.Segments))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, segment := range template.Segments {
		if segment.Text != nil {
			payload, message := lexer.DecodeLiteralBody(segment.Text.Lexeme, lexer.StringEscapes)
			if message != "" {
				diagnostics = append(diagnostics, typeErrorAt(*segment.Text, message))
				continue
			}
			segments = append(segments, InterpolationSegment{Text: string(payload)})
			continue
		}
		value := checkValue(segment.Expression, ctx)
		if nested := initializerDiagnostics(value); len(nested) > 0 {
			diagnostics = append(diagnostics, nested...)
			continue
		}
		if !interpolationSupportedType(value.typ) {
			diagnostics = append(diagnostics, typeErrorAt(value.token, "string interpolation does not support "+value.typ.Name))
			continue
		}
		segments = append(segments, InterpolationSegment{IsValue: true, Value: value.source})
	}
	if len(diagnostics) > 0 {
		return nil, &checkedExpression{token: callee, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	return segments, nil
}

// checkStringInterpolate resolves String.interpolate(heap, template): the
// compiler-known operation that builds one heap-owned String from a
// template's literal text and formatted embedded expressions. The Heap
// argument is checked first; the template's segments are then checked left to
// right. Nothing in it can fail, so the result is a plain String.
func checkStringInterpolate(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	if len(call.Arguments) != 2 {
		diagnostic := typeErrorAt(callee, fmt.Sprintf("String.interpolate expects 2 arguments; got %d", len(call.Arguments)))
		return checkedExpression{token: callee, diagnostic: &diagnostic}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := typeErrorAt(heap.token, "String.interpolate requires a Heap; got "+heap.typ.Name)
		return checkedExpression{token: heap.token, diagnostic: &diagnostic}
	}
	segments, failure := checkInterpolationTemplate(call.Arguments[1], "String.interpolate", callee, ctx)
	if failure != nil {
		return *failure
	}
	node := Expression{
		Kind:                  StringInterpolateExpression,
		Operand:               &heap.source.Node,
		OperandType:           compilerTypes.Heap,
		ResultType:            compilerTypes.StringType,
		InterpolationSegments: segments,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.StringType, Name: "interpolate", Node: node}
	return checkedExpression{source: source, typ: compilerTypes.StringType, token: callee}
}

// textMethodNode is the checked node of one text method call.
func textMethodNode(name string, receiver checkedExpression, arguments []Operand, result compilerTypes.Type, callee lexer.Token) checkedExpression {
	node := Expression{
		Kind:        StringMethodCallExpression,
		Name:        name,
		Operand:     &receiver.source.Node,
		Arguments:   arguments,
		OperandType: receiver.typ,
		ResultType:  result,
		Element:     compilerTypes.UInt8,
	}
	source := Operand{Kind: ExpressionOperand, Type: result, Name: name, Node: node}
	return checkedExpression{source: source, typ: result, token: callee}
}

// checkTextMethodCall dispatches the method surface of String and String<N>:
// length, bytes, slice, copy, and concat on both; free and c_pointer on the
// heap form; widen<M> on the inline form. Indexing ([index]) resolves through
// checkIndexPlace.
func checkTextMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	property := callee.Property
	inline := compilerTypes.IsInlineString(receiver.typ)
	fail := func(message string) checkedExpression {
		diagnostic := typeErrorAt(property, message)
		return checkedExpression{token: property, diagnostic: &diagnostic}
	}
	checkHeap := func(argument parser.Expression, label string) (checkedExpression, *checkedExpression) {
		heap := checkValue(argument, ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return checkedExpression{}, &heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := typeErrorAt(heap.token, label+" requires a Heap; got "+heap.typ.Name)
			return checkedExpression{}, &checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		return heap, nil
	}
	// The heap String's record covers its whole surface, so its dispatch is
	// registry-driven. The inline form's record is not yet complete -- it omits
	// the shared read-only operations String<N> accepts -- so only the heap
	// receiver gates on the registry; the inline form keeps its explicit switch.
	if !inline && !hasBuiltinMethod(receiver.typ, name) {
		return fail(receiver.typ.Name + " has no method " + name)
	}
	switch name {
	case "length":
		if len(call.Arguments) != 0 {
			return fail("length expects no arguments")
		}
		return textMethodNode(name, receiver, nil, compilerTypes.SizeType, property)
	case "rune_length":
		if len(call.Arguments) != 0 {
			return fail("rune_length expects no arguments")
		}
		return textMethodNode(name, receiver, nil, compilerTypes.SizeType, property)
	case "grapheme_length":
		if len(call.Arguments) != 0 {
			return fail("grapheme_length expects no arguments")
		}
		return textMethodNode(name, receiver, nil, compilerTypes.SizeType, property)
	case "byte_cursor":
		if len(call.Arguments) != 0 {
			return fail("byte_cursor expects no arguments")
		}
		if inline && !receiver.source.Addressable {
			return fail("a cursor cannot be rooted in a temporary " + receiver.typ.Name)
		}
		return textMethodNode(name, receiver, nil, compilerTypes.ByteCursorType, property)
	case "rune_cursor":
		if len(call.Arguments) != 0 {
			return fail("rune_cursor expects no arguments")
		}
		if inline && !receiver.source.Addressable {
			return fail("a cursor cannot be rooted in a temporary " + receiver.typ.Name)
		}
		return textMethodNode(name, receiver, nil, compilerTypes.RuneCursorType, property)
	case "grapheme_cursor":
		if len(call.Arguments) != 0 {
			return fail("grapheme_cursor expects no arguments")
		}
		if inline && !receiver.source.Addressable {
			return fail("a cursor cannot be rooted in a temporary " + receiver.typ.Name)
		}
		return textMethodNode(name, receiver, nil, compilerTypes.GraphemeCursorType, property)
	case "bytes":
		if len(call.Arguments) != 0 {
			return fail("bytes expects no arguments")
		}
		if inline && !receiver.source.Addressable {
			return fail("a Slice cannot be rooted in a temporary " + receiver.typ.Name)
		}
		slice := ctx.typeEnvironment.SliceType(compilerTypes.UInt8, false)
		return textMethodNode(name, receiver, nil, slice, property)
	case "slice":
		if len(call.Arguments) != 2 {
			return fail(fmt.Sprintf("slice expects 2 arguments; got %d", len(call.Arguments)))
		}
		if inline && !receiver.source.Addressable {
			return fail("a Slice cannot be rooted in a temporary " + receiver.typ.Name)
		}
		start, _, diagnostic := checkArrayIndex(call.Arguments[0], property, ctx)
		if diagnostic != nil {
			return checkedExpression{token: property, diagnostic: diagnostic}
		}
		end, _, diagnostic := checkArrayIndex(call.Arguments[1], property, ctx)
		if diagnostic != nil {
			return checkedExpression{token: property, diagnostic: diagnostic}
		}
		slice := ctx.typeEnvironment.SliceType(compilerTypes.UInt8, false)
		return textMethodNode(name, receiver, []Operand{start, end}, slice, property)
	case "copy":
		if len(call.Arguments) != 1 {
			return fail(fmt.Sprintf("copy expects 1 argument; got %d", len(call.Arguments)))
		}
		heap, failure := checkHeap(call.Arguments[0], "copy")
		if failure != nil {
			return *failure
		}
		return textMethodNode(name, receiver, []Operand{heap.source}, compilerTypes.StringType, property)
	case "casefold":
		if len(call.Arguments) != 1 {
			return fail(fmt.Sprintf("casefold expects 1 argument; got %d", len(call.Arguments)))
		}
		heap, failure := checkHeap(call.Arguments[0], "casefold")
		if failure != nil {
			return *failure
		}
		union, failure := textFailureUnion(compilerTypes.StringType, property, ctx)
		if failure != nil {
			return *failure
		}
		result := textMethodNode(name, receiver, []Operand{heap.source}, union, property)
		result.source.Node.SourceLine = property.Line
		result.source.Node.SourceColumn = property.Column
		return result
	case "normalize":
		if len(call.Arguments) != 2 {
			return fail(fmt.Sprintf("normalize expects 2 arguments; got %d", len(call.Arguments)))
		}
		heap, failure := checkHeap(call.Arguments[0], "normalize")
		if failure != nil {
			return *failure
		}
		form := checkValue(call.Arguments[1], ctx)
		if diagnostics := initializerDiagnostics(form); len(diagnostics) > 0 {
			return checkedExpression{token: form.token, diagnostics: diagnostics}
		}
		if !compilerTypes.IsNormalizationForm(form.typ) {
			return fail("normalize requires a NormalizationForm; got " + form.typ.Name)
		}
		union, failure := textFailureUnion(compilerTypes.StringType, property, ctx)
		if failure != nil {
			return *failure
		}
		result := textMethodNode(name, receiver, []Operand{heap.source, form.source}, union, property)
		result.source.Node.SourceLine = property.Line
		result.source.Node.SourceColumn = property.Column
		return result
	case "concat":
		if len(call.Arguments) != 2 {
			return fail(fmt.Sprintf("concat expects 2 arguments; got %d", len(call.Arguments)))
		}
		heap, failure := checkHeap(call.Arguments[0], "concat")
		if failure != nil {
			return *failure
		}
		other, failure := checkByteView(call.Arguments[1], "concat", ctx)
		if failure != nil {
			return *failure
		}
		union, failure := textFailureUnion(compilerTypes.StringType, property, ctx)
		if failure != nil {
			return *failure
		}
		result := textMethodNode(name, receiver, []Operand{heap.source, other.source}, union, property)
		result.source.Node.SourceLine = property.Line
		result.source.Node.SourceColumn = property.Column
		return result
	case "widen":
		if !inline {
			return fail(receiver.typ.Name + " has no method widen")
		}
		if len(call.TypeArguments) != 1 {
			return fail("widen requires exactly one capacity argument")
		}
		if len(call.Arguments) != 0 {
			return fail("widen expects no arguments")
		}
		capacity, diagnostic := inlineStringCapacity(call.TypeArguments[0], property)
		if diagnostic != nil {
			return checkedExpression{token: property, diagnostic: diagnostic}
		}
		if capacity < receiver.typ.InlineString.Capacity {
			return fail("widen<M> requires M greater than or equal to N")
		}
		return textMethodNode(name, receiver, nil, ctx.typeEnvironment.InlineStringType(capacity), property)
	case "free":
		if inline {
			return fail(receiver.typ.Name + " has no method free")
		}
		if len(call.Arguments) != 1 {
			return fail(fmt.Sprintf("free expects 1 argument; got %d", len(call.Arguments)))
		}
		heap, failure := checkHeap(call.Arguments[0], "free")
		if failure != nil {
			return *failure
		}
		if origins := stringOriginOf(receiver.source.Node, ctx); origins&stringOriginStatic != 0 {
			return fail("cannot free a String literal")
		}
		return textMethodNode(name, receiver, []Operand{heap.source}, compilerTypes.Type{}, property)
	case "c_pointer":
		if inline {
			return fail(receiver.typ.Name + " has no method c_pointer")
		}
		if len(call.Arguments) != 0 {
			return fail("c_pointer expects no arguments")
		}
		pointer := ctx.typeEnvironment.PtrType(compilerTypes.UInt8)
		if pointer == (compilerTypes.Type{}) {
			return fail("String.c_pointer has no pointer result")
		}
		// The address may outlive the String allocation, so the operation
		// carries the unsafe contract.
		if diagnostic := requireUnsafe(ctx, property, unsafeStringCPointer); diagnostic != nil {
			return checkedExpression{token: property, diagnostic: diagnostic}
		}
		return textMethodNode(name, receiver, nil, pointer, property)
	}
	return fail(receiver.typ.Name + " has no method " + name)
}
