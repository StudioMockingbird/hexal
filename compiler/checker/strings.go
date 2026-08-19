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
	resultType := compilerTypes.StringType
	if compilerTypes.IsStrand(expected) {
		if len(payload) > 31 {
			return checkedExpression{token: expression.Token, diagnostic: diagnosticAt(typeErrorAt(expression.Token, "Strand literal exceeds 31 UTF-8 bytes"))}
		}
		for _, character := range payload {
			if character == 0 {
				return checkedExpression{token: expression.Token, diagnostic: diagnosticAt(typeErrorAt(expression.Token, "Strand literal cannot contain NUL"))}
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
	return checkedExpression{source: source, typ: resultType, token: expression.Token}
}

// checkStringTypeCall resolves a call written as String.<name>(...), the
// built-in type constructors String.from_bytes(heap, view) and
// String.from_runes(heap, view).
func checkStringTypeCall(call parser.CallExpression, callee lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	name := call.Callee.(parser.PropertyExpression).Property.Lexeme
	viewType := compilerTypes.UInt8
	if name == "from_runes" {
		viewType = compilerTypes.Rune
	}
	if (name != "from_bytes" && name != "from_runes") || len(call.Arguments) != 2 {
		return checkedExpression{token: callee, diagnostic: diagnosticAt(typeErrorAt(callee, "String has no such operation; use String.from_bytes(heap, view) or String.from_runes(heap, view)"))}
	}
	heap := checkValue(call.Arguments[0], names, typeEnvironment)
	if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
		return heap
	}
	if !compilerTypes.IsHeap(heap.typ) {
		diagnostic := typeErrorAt(heap.token, "String."+name+" requires a Heap; got "+heap.typ.Name)
		return checkedExpression{token: heap.token, diagnostic: &diagnostic}
	}
	view := checkValue(call.Arguments[1], names, typeEnvironment)
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

// checkStringMethodCall dispatches the String method surface:
// length, bytes, slice, rune_cursor, to_string, concat, and free,
// and rejects the removed at name with its replacement. Indexing
// ([index]) resolves through checkIndexPlace.
func checkStringMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
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
		view := typeEnvironment.ViewType(compilerTypes.UInt8)
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
		start, _, diagnostic := checkArrayIndex(call.Arguments[0], callee.Property, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		end, _, diagnostic := checkArrayIndex(call.Arguments[1], callee.Property, names, typeEnvironment)
		if diagnostic != nil {
			return checkedExpression{token: callee.Property, diagnostic: diagnostic}
		}
		view := typeEnvironment.ViewType(compilerTypes.UInt8)
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
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
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
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
		if diagnostics := initializerDiagnostics(heap); len(diagnostics) > 0 {
			return heap
		}
		if !compilerTypes.IsHeap(heap.typ) {
			diagnostic := typeErrorAt(heap.token, "concat requires a Heap; got "+heap.typ.Name)
			return checkedExpression{token: heap.token, diagnostic: &diagnostic}
		}
		other := checkValue(call.Arguments[1], names, typeEnvironment)
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
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
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
func checkStrandMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
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
		heap := checkValue(call.Arguments[0], names, typeEnvironment)
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
func checkRuneCursorMethodCall(call parser.CallExpression, callee parser.PropertyExpression, receiver checkedExpression, _ *scope, _ *compilerTypes.Environment) checkedExpression {
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
