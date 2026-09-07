package checker

import (
	"fmt"
	"go/constant"
	"strconv"
	"unicode/utf8"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// Strings are reference-like handles with C-style shallow copies
// and manual cleanup; no provenance or ownership state is tracked.

// decodeStringLiteral decodes a double-quoted literal's raw lexeme (including
// the surrounding quotes) into its payload bytes using the shared literal
// decoder, which validates every escape and UTF-8 validity.
func decodeStringLiteral(token lexer.Token) ([]byte, *compilerTypes.Diagnostic) {
	raw := token.Lexeme
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil, diagnosticAt(typeErrorAt(token, "malformed string literal"))
	}
	payload, message := lexer.DecodeLiteralBody(raw[1:len(raw)-1], lexer.StringEscapes)
	if message != "" {
		return nil, diagnosticAt(typeErrorAt(token, message))
	}
	return payload, nil
}

// decodeRawStringLiteral decodes a raw string literal's raw lexeme
// (including its 'r', hash delimiters, and surrounding quotes) into its
// payload bytes: copied byte-for-byte from source with no escape or
// interpolation processing, validated only for UTF-8.
func decodeRawStringLiteral(token lexer.Token) ([]byte, *compilerTypes.Diagnostic) {
	raw := token.Lexeme
	if len(raw) < 2 || raw[0] != 'r' {
		return nil, diagnosticAt(typeErrorAt(token, "malformed raw string literal"))
	}
	hashCount := 0
	for 1+hashCount < len(raw) && raw[1+hashCount] == '#' {
		hashCount++
	}
	openEnd := 1 + hashCount + 1 // past 'r', the hashes, and the opening '"'
	closeStart := len(raw) - hashCount - 1
	if openEnd > len(raw) || closeStart < openEnd || closeStart >= len(raw) || raw[openEnd-1] != '"' || raw[closeStart] != '"' {
		return nil, diagnosticAt(typeErrorAt(token, "malformed raw string literal"))
	}
	payload := []byte(raw[openEnd:closeStart])
	if !utf8.Valid(payload) {
		return nil, diagnosticAt(typeErrorAt(token, "string literal contains invalid UTF-8"))
	}
	return payload, nil
}

// decodeByteLiteral decodes a b'...' literal into its single byte.
func decodeByteLiteral(token lexer.Token) (byte, *compilerTypes.Diagnostic) {
	raw := token.Lexeme
	if len(raw) < 4 || raw[0] != 'b' || raw[1] != '\'' || raw[len(raw)-1] != '\'' {
		return 0, diagnosticAt(typeErrorAt(token, "malformed Byte literal"))
	}
	payload, message := lexer.DecodeLiteralBody(raw[2:len(raw)-1], lexer.ByteEscapes)
	if message != "" || len(payload) != 1 {
		return 0, diagnosticAt(typeErrorAt(token, "Byte literal must contain exactly one byte"))
	}
	return payload[0], nil
}

// decodeRuneLiteral decodes a '...' literal into its single Unicode scalar.
func decodeRuneLiteral(token lexer.Token) (rune, *compilerTypes.Diagnostic) {
	raw := token.Lexeme
	if len(raw) < 3 || raw[0] != '\'' || raw[len(raw)-1] != '\'' {
		return 0, diagnosticAt(typeErrorAt(token, "malformed Rune literal"))
	}
	payload, message := lexer.DecodeLiteralBody(raw[1:len(raw)-1], lexer.RuneEscapes)
	if message != "" {
		return 0, diagnosticAt(typeErrorAt(token, message))
	}
	value, _ := utf8.DecodeRune(payload)
	return value, nil
}

// checkByteLiteral resolves a b'...' literal into a UInt8 constant.
func checkByteLiteral(expression parser.ByteLiteral) checkedExpression {
	value, diagnostic := decodeByteLiteral(expression.Token)
	if diagnostic != nil {
		return checkedExpression{token: expression.Token, diagnostic: diagnostic}
	}
	spelling := strconv.FormatUint(uint64(value), 10)
	source := constantOperand(compilerTypes.UInt8, constant.MakeUint64(uint64(value)), spelling)
	source.Radix = DecimalRadix
	source.Node = constantNode(source)
	return checkedExpression{source: source, typ: compilerTypes.UInt8, token: expression.Token, known: &source}
}

// checkRuneLiteral resolves a '...' literal into a Rune constant.
func checkRuneLiteral(expression parser.RuneLiteral) checkedExpression {
	value, diagnostic := decodeRuneLiteral(expression.Token)
	if diagnostic != nil {
		return checkedExpression{token: expression.Token, diagnostic: diagnostic}
	}
	spelling := strconv.FormatUint(uint64(value), 10)
	source := constantOperand(compilerTypes.Rune, constant.MakeUint64(uint64(value)), spelling)
	source.Radix = DecimalRadix
	source.Node = constantNode(source)
	return checkedExpression{source: source, typ: compilerTypes.Rune, token: expression.Token, known: &source}
}

// checkStringLiteral resolves a string literal into a static String value
// carrying its decoded payload. In a Strand position the same payload becomes
// a Strand value; Strand keeps its literal-only construction and enforces
// the 31-byte, NUL-free, zero-filled invariants.
func checkStringLiteral(expression parser.StringLiteral, expected compilerTypes.Type) checkedExpression {
	payload, diagnostic := decodeStringLiteral(expression.Token)
	if diagnostic != nil {
		return checkedExpression{token: expression.Token, diagnostic: diagnostic}
	}
	return checkedStringLiteralValue(payload, expected, expression.Token)
}

// checkRawStringLiteral resolves a raw string literal into a static String
// or Strand value: byte-for-byte identical to an interpreted literal
// containing the same UTF-8 bytes, since raw content performs no escape or
// interpolation processing.
func checkRawStringLiteral(expression parser.RawStringLiteral, expected compilerTypes.Type) checkedExpression {
	payload, diagnostic := decodeRawStringLiteral(expression.Token)
	if diagnostic != nil {
		return checkedExpression{token: expression.Token, diagnostic: diagnostic}
	}
	return checkedStringLiteralValue(payload, expected, expression.Token)
}

// checkedStringLiteralValue builds the checked String or Strand value shared
// by an interpreted and a raw string literal, once each has decoded its own
// payload bytes.
func checkedStringLiteralValue(payload []byte, expected compilerTypes.Type, token lexer.Token) checkedExpression {
	resultType := compilerTypes.StringType
	if compilerTypes.IsStrand(expected) {
		if len(payload) > 31 {
			return checkedExpression{token: token, diagnostic: diagnosticAt(typeErrorAt(token, "Strand literal exceeds 31 UTF-8 bytes"))}
		}
		for _, character := range payload {
			if character == 0 {
				return checkedExpression{token: token, diagnostic: diagnosticAt(typeErrorAt(token, "Strand literal cannot contain NUL"))}
			}
		}
		resultType = compilerTypes.StrandType
	}
	node := Expression{
		Kind:       StringLiteralExpression,
		Name:       string(payload),
		ResultType: resultType,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Node: node}
	return checkedExpression{source: source, typ: resultType, token: token}
}

// stringTypeCallUsage is the shared "no such operation" diagnostic text for
// every unrecognized String.<name>(...) call.
const stringTypeCallUsage = "String has no such operation; use String.from_bytes(heap, view), String.from_runes(heap, view), or String.interpolate(heap, template)"

// checkStringTypeCall resolves a call written as String.<name>(...): the
// built-in type constructors String.from_bytes(heap, view) and
// String.from_runes(heap, view), and the compiler-known String.interpolate
// operation.
func checkStringTypeCall(call parser.CallExpression, callee lexer.Token, ctx checkContext) checkedExpression {
	name := call.Callee.(parser.PropertyExpression).Property.Lexeme
	switch name {
	case "interpolate":
		return checkStringInterpolate(call, callee, ctx)
	case "from_bytes", "from_runes":
	default:
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, stringTypeCallUsage))}
	}
	viewType := compilerTypes.UInt8
	if name == "from_runes" {
		viewType = compilerTypes.Rune
	}
	if len(call.Arguments) != 2 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, stringTypeCallUsage))}
	}
	heap := checkValue(call.Arguments[0], ctx)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := typeErrorAt(heap.token, "String."+name+" requires a Heap; got "+heap.typ.Name)
		return checkedExpression{token: heap.token, diagnostic: &diagnostic}
	}
	view := checkValue(call.Arguments[1], ctx)
	if diagnostics := initializerDiagnostics(view); len(diagnostics) > 0 {
		return view
	}
	if view.typ.View == nil || !compilerTypes.Equal(view.typ.View.Element, viewType) {
		display := "Byte"
		if viewType == compilerTypes.Rune {
			display = "Rune"
		}
		diagnostic := typeErrorAt(view.token, "String."+name+" requires View<"+display+">; got "+view.typ.Name)
		return checkedExpression{token: view.token, diagnostic: &diagnostic}
	}
	kind := StringFromBytesExpression
	if name == "from_runes" {
		kind = StringFromRunesExpression
	}
	node := Expression{
		Kind:        kind,
		Operand:     &heap.source.Node,
		Arguments:   []Operand{view.source},
		OperandType: compilerTypes.Heap,
		ResultType:  compilerTypes.StringType,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.StringType, Name: name, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.StringType, token: callee}
}

// interpolationSupportedType reports whether typ may appear as an embedded
// interpolation value: Bool, Rune, every fixed-width signed and unsigned
// integer, Size, Byte, Float32, Float64, String, and Strand. Every other
// type -- Nil, pointers, unions, structs, ADTs, arrays, views, lists,
// dictionaries, allocators, concurrency values, Error, and Fun -- is
// deliberately excluded.
func interpolationSupportedType(typ compilerTypes.Type) bool {
	switch {
	case compilerTypes.Equal(typ, compilerTypes.Bool):
		return true
	case compilerTypes.IsRune(typ):
		return true
	case compilerTypes.IsSignedInteger(typ), compilerTypes.IsUnsignedInteger(typ):
		return true
	case compilerTypes.Equal(typ, compilerTypes.Float32), compilerTypes.Equal(typ, compilerTypes.Float64):
		return true
	case compilerTypes.IsString(typ), compilerTypes.IsStrand(typ):
		return true
	}
	return false
}

// checkStringInterpolate resolves String.interpolate(heap, template): the
// compiler-known operation that builds one heap-owned String from a
// template's literal text and formatted embedded expressions. The Heap
// argument is checked first; the template's segments are then checked left
// to right in the surrounding lexical scope, each against the supported
// interpolation value set.
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
	template, isTemplate := call.Arguments[1].(parser.InterpolationTemplateExpression)
	if !isTemplate {
		if _, isPlainString := call.Arguments[1].(parser.StringLiteral); isPlainString {
			diagnostic := typeErrorAt(callee, "String.interpolate requires at least one interpolation")
			return checkedExpression{token: callee, diagnostic: &diagnostic}
		}
		diagnostic := typeErrorAt(callee, "String.interpolate requires an interpreted interpolation template")
		return checkedExpression{token: callee, diagnostic: &diagnostic}
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
		return checkedExpression{token: callee, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
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

// checkStringMethodCall dispatches the String method surface:
// length, bytes, slice, rune_cursor, to_string, concat, and free,
// and rejects the removed at name with its replacement. Indexing
// ([index]) resolves through checkIndexPlace.
func checkStringMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "length":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "length expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		// Text lengths return Size and count Runes.
		node := Expression{Kind: StringMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: compilerTypes.SizeType, Element: compilerTypes.UInt8}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: callee.Property}
	case "bytes":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "bytes expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		view := ctx.typeEnvironment.ViewType(compilerTypes.UInt8)
		node := Expression{
			Kind:        StringMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			OperandType: compilerTypes.StringType,
			ResultType:  view,
			Element:     compilerTypes.UInt8,
		}
		if root := baseBindingID(&receiver.source.Node); root != 0 {
			node.ViewRoots = []BindingID{root}
			node.RootKind = ViewRootBindings
		}
		source := Operand{Kind: ExpressionOperand, Type: view, Name: name, Node: node}
		return checkedExpression{source: source, typ: view, token: callee.Property}
	case "slice":
		if len(call.Arguments) != 2 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("slice expects 2 arguments; got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		start, _, diagnostic := checkArrayIndex(call.Arguments[0], callee.Property, ctx)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		end, _, diagnostic := checkArrayIndex(call.Arguments[1], callee.Property, ctx)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		view := ctx.typeEnvironment.ViewType(compilerTypes.UInt8)
		node := Expression{
			Kind:        StringMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{start, end},
			OperandType: compilerTypes.StringType,
			ResultType:  view,
			Element:     compilerTypes.UInt8,
		}
		if root := baseBindingID(&receiver.source.Node); root != 0 {
			node.ViewRoots = []BindingID{root}
			node.RootKind = ViewRootBindings
		}
		source := Operand{Kind: ExpressionOperand, Type: view, Name: name, Node: node}
		return checkedExpression{source: source, typ: view, token: callee.Property}
	case "rune_cursor":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "rune_cursor expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: StringMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: compilerTypes.StringType, ResultType: compilerTypes.RuneCursorType, Element: compilerTypes.UInt8}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.RuneCursorType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.RuneCursorType, token: callee.Property}
	case "to_string":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("to_string expects 1 argument; got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		heap := checkValue(call.Arguments[0], ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := typeErrorAt(heap.token, "to_string requires a Heap; got "+heap.typ.Name)
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        StringMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{heap.source},
			OperandType: compilerTypes.StringType,
			ResultType:  compilerTypes.StringType,
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.StringType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.StringType, token: callee.Property}
	case "concat":
		if len(call.Arguments) != 2 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("concat expects 2 arguments; got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		heap := checkValue(call.Arguments[0], ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := typeErrorAt(heap.token, "concat requires a Heap; got "+heap.typ.Name)
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		other := checkValue(call.Arguments[1], ctx)
		if diagnostics := initializerDiagnostics(other); len(diagnostics) > 0 {
			return other
		}
		if !compilerTypes.IsString(other.typ) {
			diagnostic := typeErrorAt(other.token, "concat requires a String; got "+other.typ.Name)
			return checkedExpression{token: other.token, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        StringMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{heap.source, other.source},
			OperandType: compilerTypes.StringType,
			ResultType:  compilerTypes.StringType,
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.StringType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.StringType, token: callee.Property}
	case "free":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("free expects 1 argument; got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		heap := checkValue(call.Arguments[0], ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := typeErrorAt(heap.token, "free requires a Heap; got "+heap.typ.Name)
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        StringMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{heap.source},
			OperandType: compilerTypes.StringType,
			ResultType:  compilerTypes.Type{},
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Type{}, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Type{}, token: callee.Property}
	default:
		diagnostic := typeErrorAt(callee.Property, "String has no method "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
}

// checkStrandMethodCall dispatches the Strand surface: length and to_string.
// Strand never exposes bytes, slice, rune_cursor, concat, or free.
func checkStrandMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, ctx checkContext) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "length":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "length expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: StringMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: compilerTypes.SizeType, Element: compilerTypes.UInt8}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.SizeType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.SizeType, token: callee.Property}
	case "to_string":
		if len(call.Arguments) != 1 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("to_string expects 1 argument; got %d", len(call.Arguments)))
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		heap := checkValue(call.Arguments[0], ctx)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := typeErrorAt(heap.token, "to_string requires a Heap; got "+heap.typ.Name)
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		node := Expression{
			Kind:        StringMethodCallExpression,
			Name:        name,
			Operand:     &receiver.source.Node,
			Arguments:   []Operand{heap.source},
			OperandType: compilerTypes.StrandType,
			ResultType:  compilerTypes.StringType,
		}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.StringType, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.StringType, token: callee.Property}
	default:
		diagnostic := typeErrorAt(callee.Property, "Strand has no method "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
}

// checkRuneCursorMethodCall dispatches the RuneCursor surface:
// has_next and next.
func checkRuneCursorMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, _ checkContext) checkedExpression {
	name := callee.Property.Lexeme
	switch name {
	case "has_next":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "has_next expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: RuneCursorMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: compilerTypes.Bool}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Bool, token: callee.Property}
	case "next":
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(callee.Property, "next expects no arguments")
			return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
		}
		node := Expression{Kind: RuneCursorMethodCallExpression, Name: name, Operand: &receiver.source.Node, OperandType: receiver.typ, ResultType: compilerTypes.Rune}
		source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Rune, Name: name, Node: node}
		return checkedExpression{source: source, typ: compilerTypes.Rune, token: callee.Property}
	default:
		diagnostic := typeErrorAt(callee.Property, "RuneCursor has no method "+name)
		return checkedExpression{token: callee.Property, diagnostic: &diagnostic}
	}
}

// stringConstantFoldIndex extracts a constant integer from an operand.
