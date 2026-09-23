package parser

import (
	"errors"
	"strings"
	"testing"

	"hexal/compiler/lexer"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// parserDiagnostics extracts the structured diagnostics a Parse error carries,
// so a test can assert the span beside the rendered position.
func parserDiagnostics(t *testing.T, err error) compilerTypes.Diagnostics {
	t.Helper()
	var diagnostics compilerTypes.Diagnostics
	if !errors.As(err, &diagnostics) {
		t.Fatalf("Parse error %v carries no Diagnostics", err)
	}
	return diagnostics
}

// A parser diagnostic's Span is the offending token's span, and a missing token
// at end of input is the zero-width insertion point at the end offset. The
// retained Line and Column are the same start resolved by the table.
func TestParseDiagnosticCarriesSpan(t *testing.T) {
	const file = "test.hex"
	cases := []struct {
		name   string
		source string
		want   span.Span
	}{
		// The initializer is missing; the diagnostic sits at the EOF insertion
		// point, which is zero-width.
		{"missing-value-eof", "let x: Int32 = ", span.Span{File: file, Start: 15, End: 15}},
		// `type` is followed by EOF, so the expected identifier is reported at
		// the end-of-input insertion point.
		{"missing-type-name-eof", "type", span.Span{File: file, Start: 4, End: 4}},
		// The error points at the token that ends the malformed statement.
		{"expected-assignment", "let x: Int32 = 13 y let z: Int32 = 14", span.Span{File: file, Start: 20, End: 23}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tokens, lexErr := lexer.Lex(file, testCase.source)
			if lexErr != nil {
				t.Fatalf("Lex(%q) returned an error: %v", testCase.source, lexErr)
			}
			_, err := Parse(tokens)
			diagnostics := parserDiagnostics(t, err)
			if len(diagnostics) != 1 {
				t.Fatalf("Parse(%q) produced %d diagnostics, want 1", testCase.source, len(diagnostics))
			}
			if got := diagnostics[0].Span; got != testCase.want {
				t.Fatalf("Parse(%q) diagnostic span = %+v, want %+v", testCase.source, got, testCase.want)
			}
		})
	}
}

// A parser diagnostic's span resolves to its retained line and column under the
// one table convention. This is the invariant that lets Line and Column be the
// derived form of the span rather than a second, independent location.
func TestParseDiagnosticSpanResolvesToRetainedLocation(t *testing.T) {
	const file = "resolves.hex"
	sources := []string{
		"let x: Int32 = ",                         // EOF insertion, single line
		"let x: Int32 = 1 --[ c\nd ]--\nlet y = ", // multiline comment, then EOF
		"let s: String<8> = \"\u00e9\" let y = ",  // multibyte Unicode before EOF
	}
	for _, source := range sources {
		tokens, lexErr := lexer.Lex(file, source)
		if lexErr != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, lexErr)
		}
		_, err := Parse(tokens)
		diagnostics := parserDiagnostics(t, err)
		if len(diagnostics) == 0 {
			t.Fatalf("Parse(%q) produced no diagnostics", source)
		}

		table := span.NewTable()
		table.Add(file, source)
		for _, diagnostic := range diagnostics {
			got := table.Position(diagnostic.Span)
			if got.Line != diagnostic.Line || got.Column != diagnostic.Column {
				t.Fatalf("Parse(%q) span %+v resolves to %+v, retained location = %d:%d",
					source, diagnostic.Span, got, diagnostic.Line, diagnostic.Column)
			}
		}
	}
}

// Every token a parser node keeps is a lexer token, so a node's authoritative
// location is a span over its own file. These cases pin single-line, multiline,
// and multibyte-Unicode nodes.
func TestParseNodeTokensCarrySpans(t *testing.T) {
	const file = "nodes.hex"

	single := "let x: Int32 = 13"
	tokens, err := lexer.Lex(file, single)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	declaration, ok := program.Statements[0].(Declaration)
	if !ok {
		t.Fatalf("statement 0 = %T, want Declaration", program.Statements[0])
	}
	if got, want := declaration.Name.Span, (span.Span{File: file, Start: 4, End: 5}); got != want {
		t.Errorf("declaration name span = %+v, want %+v", got, want)
	}
	if got, want := declaration.Keyword.Span, (span.Span{File: file, Start: 0, End: 3}); got != want {
		t.Errorf("declaration keyword span = %+v, want %+v", got, want)
	}
	literal, ok := declaration.Initializer.(IntegerLiteral)
	if !ok {
		t.Fatalf("initializer = %T, want IntegerLiteral", declaration.Initializer)
	}
	if got, want := literal.Token.Span, (span.Span{File: file, Start: 15, End: 17}); got != want {
		t.Errorf("initializer token span = %+v, want %+v", got, want)
	}

	multiline := "let x: Int32 = 1 --[ c\nd ]--\nlet y: Int32 = x"
	tokens, err = lexer.Lex(file, multiline)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err = Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	second, ok := program.Statements[1].(Declaration)
	if !ok {
		t.Fatalf("statement 1 = %T, want Declaration", program.Statements[1])
	}
	yOffset := strings.Index(multiline, "y: Int32")
	if got, want := second.Name.Span, (span.Span{File: file, Start: yOffset, End: yOffset + 1}); got != want {
		t.Errorf("multiline declaration name span = %+v, want %+v", got, want)
	}

	unicode := "let s: String<8> = \"\u00e9\" let y: Int32 = 1"
	tokens, err = lexer.Lex(file, unicode)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err = Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	unicodeSecond, ok := program.Statements[1].(Declaration)
	if !ok {
		t.Fatalf("statement 1 = %T, want Declaration", program.Statements[1])
	}
	yOffset = strings.Index(unicode, "y: Int32")
	if got, want := unicodeSecond.Name.Span, (span.Span{File: file, Start: yOffset, End: yOffset + 1}); got != want {
		t.Errorf("unicode declaration name span = %+v, want %+v", got, want)
	}
}

// A parser node's span names the logical source key it was parsed from, never a
// host path: an embedded stdlib module and a prepared C binding are ordinary
// logical keys to the lexer and the parser.
func TestParseNodeSpansUseLogicalSourceKeys(t *testing.T) {
	for _, file := range []string{
		"stdlib/ascii.hex",
		"hexalc/x86_64-linux-gnu/c/stddef.hex",
	} {
		tokens, err := lexer.Lex(file, "let x: Int32 = 1")
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", file, err)
		}
		program, err := Parse(tokens)
		if err != nil {
			t.Fatalf("Parse(%q) returned an error: %v", file, err)
		}
		declaration, ok := program.Statements[0].(Declaration)
		if !ok {
			t.Fatalf("%s: statement 0 = %T, want Declaration", file, program.Statements[0])
		}
		if got := declaration.Name.Span.File; got != file {
			t.Fatalf("%s: declaration name span file = %q, want %q", file, got, file)
		}
	}
}

// A `>>` split into two generic closers yields two adjacent, non-overlapping
// spans over that token's two bytes, so neither synthetic closer invents a
// location: the first is the `>>` start, the second is its second byte.
func TestSplitGenericClosersCarryAdjacentSpans(t *testing.T) {
	const file = "split-spans.hex"
	tokens, err := lexer.Lex(file, "Box<Box<Int32>>")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	shift := -1
	for index, token := range tokens {
		if token.Kind == lexer.ShiftRight {
			shift = index
			break
		}
	}
	if shift < 0 {
		t.Fatal("Lex produced no ShiftRight token")
	}
	start := tokens[shift].Span.Start
	parser := &Parser{tokens: tokens, current: shift}
	first, err := parser.consumeGenericClose("'>' after a generic argument list")
	if err != nil {
		t.Fatalf("consumeGenericClose returned an error: %v", err)
	}
	second, err := parser.consume(lexer.Greater, "'>' after a generic argument list")
	if err != nil {
		t.Fatalf("consume returned an error: %v", err)
	}
	if got, want := first.Span, (span.Span{File: file, Start: start, End: start + 1}); got != want {
		t.Errorf("first closer span = %+v, want %+v", got, want)
	}
	if got, want := second.Span, (span.Span{File: file, Start: start + 1, End: start + 2}); got != want {
		t.Errorf("second closer span = %+v, want %+v", got, want)
	}
	if first.Span.End != second.Span.Start {
		t.Errorf("closers are not adjacent: first ends %d, second starts %d", first.Span.End, second.Span.Start)
	}
}

// A `>>` that closes two nested generic argument lists is split into two
// closers, and the split never invents a location: parsing succeeds and every
// surviving node token still carries its own span.
func TestParseSplitGenericClosersKeepNodeSpans(t *testing.T) {
	const file = "split.hex"
	source := "type Box<T> is struct value: T end\nlet x: Box<Box<Int32>> = Box(value = Box(value = 1))"
	tokens, err := lexer.Lex(file, source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
	declaration, ok := program.Statements[len(program.Statements)-1].(Declaration)
	if !ok {
		t.Fatalf("last statement = %T, want Declaration", program.Statements[len(program.Statements)-1])
	}
	xOffset := strings.Index(source, "x: Box")
	if got, want := declaration.Name.Span, (span.Span{File: file, Start: xOffset, End: xOffset + 1}); got != want {
		t.Fatalf("declaration name span = %+v, want %+v", got, want)
	}
}
