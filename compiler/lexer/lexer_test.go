package lexer

import (
	"strings"
	"testing"

	"hexal/internal/span"
)

// sameToken compares the legacy fields a token carries at the migration
// boundary. Span identity and file key are asserted separately by
// TestLexTokenSpans, which pins exact byte offsets.
func sameToken(got, want Token) bool {
	return got.Kind == want.Kind && got.Lexeme == want.Lexeme && got.Line == want.Line && got.Column == want.Column
}

func TestLexDeclaration(t *testing.T) {
	tokens, err := Lex("test.hex", "let x: Int32 = 13")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: Let, Lexeme: "let", Line: 1, Column: 1},
		{Kind: Identifier, Lexeme: "x", Line: 1, Column: 5},
		{Kind: Colon, Lexeme: ":", Line: 1, Column: 6},
		{Kind: Identifier, Lexeme: "Int32", Line: 1, Column: 8},
		{Kind: Equal, Lexeme: "=", Line: 1, Column: 14},
		{Kind: Integer, Lexeme: "13", Line: 1, Column: 16},
		{Kind: EOF, Line: 1, Column: 18},
	}

	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if !sameToken(tokens[index], want[index]) {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexRejectsUnexpectedCharacter(t *testing.T) {
	_, err := Lex("test.hex", "x: Int32 := $")
	if err == nil {
		t.Fatal("Lex accepted an unexpected character")
	}
	if err.Error() != `[Syntax Error] unexpected character '$' at 1:13` {
		t.Fatalf("Lex error = %q", err)
	}
}

func TestLexBooleanKeywordsAndIdentifiers(t *testing.T) {
	tokens, err := Lex("test.hex", "true false trueValue")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: True, Lexeme: "true", Line: 1, Column: 1},
		{Kind: False, Lexeme: "false", Line: 1, Column: 6},
		{Kind: Identifier, Lexeme: "trueValue", Line: 1, Column: 12},
		{Kind: EOF, Line: 1, Column: 21},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if !sameToken(tokens[index], want[index]) {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexNilPipeAndProtectedTypeIdentifiers(t *testing.T) {
	tokens, err := Lex("test.hex", "nil | Nil Unknown nilValue")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	wantKinds := []TokenKind{NilLiteral, Pipe, Identifier, Identifier, Identifier, EOF}
	wantLexemes := []string{"nil", "|", "Nil", "Unknown", "nilValue", ""}
	wantColumns := []int{1, 5, 7, 11, 19, 27}
	if len(tokens) != len(wantKinds) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(wantKinds))
	}
	for index := range wantKinds {
		if got := tokens[index].Kind; got != wantKinds[index] {
			t.Errorf("token %d kind = %v, want %v", index, got, wantKinds[index])
		}
		if tokens[index].Lexeme != wantLexemes[index] {
			t.Errorf("token %d lexeme = %q, want %q", index, tokens[index].Lexeme, wantLexemes[index])
		}
		if tokens[index].Column != wantColumns[index] {
			t.Errorf("token %d column = %d, want %d", index, tokens[index].Column, wantColumns[index])
		}
	}
	if tokens[0].Kind == Identifier || tokens[1].Kind == Identifier {
		t.Fatal("nil and | must have distinct token kinds from identifiers")
	}
}

func TestLexIsAsReservedWord(t *testing.T) {
	tokens, err := Lex("test.hex", "value is Int32 | Nil")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	if tokens[1].Kind == Identifier {
		t.Fatalf("is token = %#v, want a reserved token", tokens[1])
	}
	if tokens[3].Kind != Pipe {
		t.Fatalf("pipe token = %#v, want Pipe", tokens[3])
	}
}

func TestLexCoreOperatorsAndMaximalMunch(t *testing.T) {
	tokens, err := Lex("test.hex", "a!=b==c<=d>=e+f*g/h%i!() and or android")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	wantKinds := []TokenKind{
		Identifier, BangEqual, Identifier,
		EqualEqual, Identifier,
		LessEqual, Identifier,
		GreaterEqual, Identifier,
		Plus, Identifier,
		Star, Identifier,
		Slash, Identifier,
		Percent, Identifier,
		Bang, LeftParen, RightParen,
		And, Or, Identifier, EOF,
	}
	if len(tokens) != len(wantKinds) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(wantKinds))
	}
	for index, want := range wantKinds {
		if tokens[index].Kind != want {
			t.Fatalf("token %d kind = %v, want %v", index, tokens[index].Kind, want)
		}
	}
}

func TestTokenKindStringForCoreOperators(t *testing.T) {
	want := map[TokenKind]string{
		Bang:         "!",
		BangEqual:    "!=",
		EqualEqual:   "==",
		LessEqual:    "<=",
		GreaterEqual: ">=",
		Plus:         "+",
		Star:         "*",
		Slash:        "/",
		Percent:      "%",
		LeftParen:    "(",
		RightParen:   ")",
		And:          "and",
		Or:           "or",
	}
	for kind, want := range want {
		if got := kind.String(); got != want {
			t.Errorf("TokenKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

func TestLexOperatorLocationsCommentsAndNestedPointers(t *testing.T) {
	tokens, err := Lex("test.hex", "!= ! == = <= < >= >\nPtr<Ptr<Int32>>\n-- comment\nand or")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: BangEqual, Lexeme: "!=", Line: 1, Column: 1},
		{Kind: Bang, Lexeme: "!", Line: 1, Column: 4},
		{Kind: EqualEqual, Lexeme: "==", Line: 1, Column: 6},
		{Kind: Equal, Lexeme: "=", Line: 1, Column: 9},
		{Kind: LessEqual, Lexeme: "<=", Line: 1, Column: 11},
		{Kind: Less, Lexeme: "<", Line: 1, Column: 14},
		{Kind: GreaterEqual, Lexeme: ">=", Line: 1, Column: 16},
		{Kind: Greater, Lexeme: ">", Line: 1, Column: 19},
		{Kind: Identifier, Lexeme: "Ptr", Line: 2, Column: 1},
		{Kind: Less, Lexeme: "<", Line: 2, Column: 4},
		{Kind: Identifier, Lexeme: "Ptr", Line: 2, Column: 5},
		{Kind: Less, Lexeme: "<", Line: 2, Column: 8},
		{Kind: Identifier, Lexeme: "Int32", Line: 2, Column: 9},
		{Kind: ShiftRight, Lexeme: ">>", Line: 2, Column: 14},
		{Kind: And, Lexeme: "and", Line: 4, Column: 1},
		{Kind: Or, Lexeme: "or", Line: 4, Column: 5},
		{Kind: EOF, Line: 4, Column: 7},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if !sameToken(tokens[index], want[index]) {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexRejectsLeadingUnderscoreIdentifier(t *testing.T) {
	_, err := Lex("test.hex", "_player: Int32 := 1")
	if err == nil {
		t.Fatal("Lex accepted an identifier beginning with an underscore")
	}
	if got, want := err.Error(), "[Syntax Error] identifiers must begin with a letter at 1:1"; got != want {
		t.Fatalf("Lex error = %q, want %q", got, want)
	}
}

func TestLexRejectsDigitStartIdentifier(t *testing.T) {
	_, err := Lex("test.hex", "2player: Int32 = 2")
	if err == nil {
		t.Fatal("Lex accepted an identifier beginning with a digit")
	}
	if got, want := err.Error(), "[Syntax Error] identifiers must begin with a letter at 1:1"; got != want {
		t.Fatalf("Lex error = %q, want %q", got, want)
	}
}

func TestLexAcceptsUnderscoreAfterLetter(t *testing.T) {
	tokens, err := Lex("test.hex", "player_2")
	if err != nil {
		t.Fatalf("Lex rejected an underscore after the leading letter: %v", err)
	}
	if got, want := tokens[0].Lexeme, "player_2"; got != want {
		t.Fatalf("identifier lexeme = %q, want %q", got, want)
	}
}

func TestLexPointerKeywordsAndProperties(t *testing.T) {
	tokens, err := Lex("test.hex", "@ mut Ptr<Int32>.value")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: At, Lexeme: "@", Line: 1, Column: 1},
		{Kind: Mut, Lexeme: "mut", Line: 1, Column: 3},
		{Kind: Identifier, Lexeme: "Ptr", Line: 1, Column: 7},
		{Kind: Less, Lexeme: "<", Line: 1, Column: 10},
		{Kind: Identifier, Lexeme: "Int32", Line: 1, Column: 11},
		{Kind: Greater, Lexeme: ">", Line: 1, Column: 16},
		{Kind: Dot, Lexeme: ".", Line: 1, Column: 17},
		{Kind: Identifier, Lexeme: "value", Line: 1, Column: 18},
		{Kind: EOF, Line: 1, Column: 23},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if !sameToken(tokens[index], want[index]) {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexTypeKeywordAndPtrIdentifier(t *testing.T) {
	tokens, err := Lex("test.hex", "type Coordinate is Ptr<Int32>")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	wantKinds := []TokenKind{Type, Identifier, Is, Identifier, Less, Identifier, Greater, EOF}
	if len(tokens) != len(wantKinds) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(wantKinds))
	}
	for index, want := range wantKinds {
		if tokens[index].Kind != want {
			t.Fatalf("token %d kind = %v, want %v", index, tokens[index].Kind, want)
		}
	}
	if tokens[3].Lexeme != "Ptr" {
		t.Fatalf("Ptr token lexeme = %q, want Ptr", tokens[3].Lexeme)
	}
}

func TestLexStructDelimiters(t *testing.T) {
	tokens, err := Lex("test.hex", "type Point is struct mut x: Int32, y: Int32, end")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	wantKinds := []TokenKind{
		Type, Identifier, Is, Struct, Mut, Identifier, Colon,
		Identifier, Comma, Identifier, Colon, Identifier, Comma, End, EOF,
	}
	if len(tokens) != len(wantKinds) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(wantKinds))
	}
	for index, want := range wantKinds {
		if tokens[index].Kind != want {
			t.Fatalf("token %d kind = %v, want %v", index, tokens[index].Kind, want)
		}
	}
}

func TestLexMinusAndAllIntegerBases(t *testing.T) {
	tokens, err := Lex("test.hex", "- 0xFF 0b1010_0101 0o755 1_000 3.14 7e3")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	wantKinds := []TokenKind{Minus, HexInteger, BinaryInteger, OctalInteger, Integer, DecimalFloat, DecimalFloat, EOF}
	if len(tokens) != len(wantKinds) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(wantKinds))
	}
	for index, want := range wantKinds {
		if tokens[index].Kind != want {
			t.Fatalf("token %d kind = %v, want %v", index, tokens[index].Kind, want)
		}
	}
}

func TestLexHexadecimalInteger(t *testing.T) {
	tokens, err := Lex("test.hex", "let mask: Int32 = 0xFF")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if got, want := tokens[5].Kind, HexInteger; got != want {
		t.Fatalf("hex token kind = %v, want %v", got, want)
	}
	if got, want := tokens[5].Lexeme, "0xFF"; got != want {
		t.Fatalf("hex token lexeme = %q, want %q", got, want)
	}
}

func TestLexRejectsMalformedHexadecimalInteger(t *testing.T) {
	for _, source := range []string{"x: Int32 := 0x", "x: Int32 := 0xG", "x: Int32 := 0x12G"} {
		_, err := Lex("test.hex", source)
		if err == nil {
			t.Fatalf("Lex accepted malformed hexadecimal literal in %q", source)
		}
		want := "[Syntax Error] malformed hexadecimal literal at 1:13"
		if got := err.Error(); got != want {
			t.Fatalf("Lex error for %q = %q, want %q", source, got, want)
		}
	}

	_, err := Lex("test.hex", "x: Int32 := 0XFF")
	if err == nil {
		t.Fatal("Lex accepted an uppercase hexadecimal prefix")
	}
	if got, want := err.Error(), "[Syntax Error] integer base prefixes must be lowercase at 1:13"; got != want {
		t.Fatalf("Lex error = %q, want %q", got, want)
	}
}

func TestLexSkipsSingleLineAndDocumentationComments(t *testing.T) {
	source := "--- declare the counter\nlet x: Int32 = 13 -- initialize\nx = 14 -- eof"
	tokens, err := Lex("test.hex", source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if len(tokens) != 10 {
		t.Fatalf("Lex returned %d tokens, want 10", len(tokens))
	}
	if got, want := tokens[0], (Token{Kind: Let, Lexeme: "let", Line: 2, Column: 1}); !sameToken(got, want) {
		t.Fatalf("first token = %#v, want %#v", got, want)
	}
	if got, want := tokens[1], (Token{Kind: Identifier, Lexeme: "x", Line: 2, Column: 5}); !sameToken(got, want) {
		t.Fatalf("declaration name token = %#v, want %#v", got, want)
	}
	if got, want := tokens[6], (Token{Kind: Identifier, Lexeme: "x", Line: 3, Column: 1}); !sameToken(got, want) {
		t.Fatalf("second statement token = %#v, want %#v", got, want)
	}
}

func TestLexSkipsMultilineCommentAndTracksLocation(t *testing.T) {
	source := "x: Int32 --[ comment\n   across lines ]-- = 13"
	tokens, err := Lex("test.hex", source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if got, want := tokens[3], (Token{Kind: Equal, Lexeme: "=", Line: 2, Column: 21}); !sameToken(got, want) {
		t.Fatalf("equal token = %#v, want %#v", got, want)
	}
	if got, want := tokens[4], (Token{Kind: Integer, Lexeme: "13", Line: 2, Column: 23}); !sameToken(got, want) {
		t.Fatalf("integer token = %#v, want %#v", got, want)
	}
}

func TestLexRejectsUnterminatedMultilineComment(t *testing.T) {
	_, err := Lex("test.hex", "x: Int32 := --[ missing close")
	if err == nil {
		t.Fatal("Lex accepted an unterminated multiline comment")
	}
	if got, want := err.Error(), "[Syntax Error] unterminated multiline comment at 1:13"; got != want {
		t.Fatalf("Lex error = %q, want %q", got, want)
	}
}

func TestLexByteAndNumericLiterals(t *testing.T) {
	tokens, err := Lex("test.hex", "b'A' 1_000 3.14 6.02e23")
	if err != nil || len(tokens) != 5 {
		t.Fatalf("Lex returned tokens=%#v err=%v, want one Byte literal, numeric tokens, and EOF", tokens, err)
	}
	if tokens[0].Kind != ByteLiteral || tokens[0].Lexeme != "b'A'" {
		t.Fatalf("first token = %#v, want a ByteLiteral b'A'", tokens[0])
	}
	if tokens[4].Kind != EOF {
		t.Fatalf("last token = %#v, want EOF", tokens[4])
	}
}

func TestLexRuneLiteral(t *testing.T) {
	tokens, err := Lex("test.hex", "'a' '\\u{41}' '\\u{1F600}'")
	if err != nil || len(tokens) != 4 {
		t.Fatalf("Lex returned tokens=%#v err=%v, want three Rune literals and EOF", tokens, err)
	}
	if tokens[0].Kind != RuneLiteral || tokens[0].Lexeme != "'a'" {
		t.Fatalf("first token = %#v, want a RuneLiteral 'a'", tokens[0])
	}
	if tokens[1].Kind != RuneLiteral || tokens[2].Kind != RuneLiteral {
		t.Fatalf("token kinds = %v %v, want RuneLiteral RuneLiteral", tokens[1].Kind, tokens[2].Kind)
	}
	if tokens[3].Kind != EOF {
		t.Fatalf("last token = %#v, want EOF", tokens[3])
	}
}

// A Rune literal admits exactly one non-surrogate Unicode scalar, so a
// multi-scalar body, a surrogate escape, and an unterminated body all fail.
func TestLexRejectsInvalidRuneForms(t *testing.T) {
	for _, source := range []string{"'ab'", "'\\u{D800}'", "'unterminated"} {
		if _, err := Lex("test.hex", source); err == nil {
			t.Fatalf("Lex(%q) accepted an invalid Rune literal", source)
		}
	}
}

func TestLexByteLiteralsAndStringEscapes(t *testing.T) {
	tokens, err := Lex("test.hex", "b'a' b'\\x41' \"h\u00e9llo \\u{1F600}\"")
	if err != nil || len(tokens) != 4 {
		t.Fatalf("Lex returned tokens=%#v err=%v, want two byte literals, a string literal, and EOF", tokens, err)
	}
	if tokens[0].Kind != ByteLiteral || tokens[1].Kind != ByteLiteral || tokens[2].Kind != StringLiteral {
		t.Fatalf("token kinds = %v %v %v, want ByteLiteral ByteLiteral StringLiteral", tokens[0].Kind, tokens[1].Kind, tokens[2].Kind)
	}
}

func TestLexRejectsInvalidByteForms(t *testing.T) {
	for _, source := range []string{"b'13'", "b'\\13'", "b'\\0xFF'", "b'\\xF'", "b'\\xFFF'", "b'é'", "b'\\u{41}'", "b'ab'"} {
		_, err := Lex("test.hex", source)
		if err == nil || !strings.Contains(err.Error(), "Byte literal") && !strings.Contains(err.Error(), "escape") {
			t.Fatalf("Lex(%q) error = %v, want a Byte literal diagnostic", source, err)
		}
	}
}

func TestLexFunctionKeywords(t *testing.T) {
	tokens, err := Lex("test.hex", "fun struct union method\nend return self")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: Fun, Lexeme: "fun", Line: 1, Column: 1},
		{Kind: Struct, Lexeme: "struct", Line: 1, Column: 5},
		{Kind: Union, Lexeme: "union", Line: 1, Column: 12},
		{Kind: Method, Lexeme: "method", Line: 1, Column: 18},
		{Kind: End, Lexeme: "end", Line: 2, Column: 1},
		{Kind: Return, Lexeme: "return", Line: 2, Column: 5},
		{Kind: Self, Lexeme: "self", Line: 2, Column: 12},
		{Kind: EOF, Line: 2, Column: 16},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if !sameToken(tokens[index], want[index]) {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestTokenKindStringForFunctionKeywords(t *testing.T) {
	want := map[TokenKind]string{
		Fun:    "fun",
		Struct: "struct",
		Union:  "union",
		Method: "method",
		End:    "end",
		Return: "return",
		Self:   "self",
	}
	for kind, want := range want {
		if got := kind.String(); got != want {
			t.Errorf("TokenKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

func TestLexControlFlowKeywords(t *testing.T) {
	tokens, err := Lex("test.hex", "if elseif else while\nbreak continue")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: If, Lexeme: "if", Line: 1, Column: 1},
		{Kind: ElseIf, Lexeme: "elseif", Line: 1, Column: 4},
		{Kind: Else, Lexeme: "else", Line: 1, Column: 11},
		{Kind: While, Lexeme: "while", Line: 1, Column: 16},
		{Kind: Break, Lexeme: "break", Line: 2, Column: 1},
		{Kind: Continue, Lexeme: "continue", Line: 2, Column: 7},
		{Kind: EOF, Line: 2, Column: 15},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if !sameToken(tokens[index], want[index]) {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestTokenKindStringForControlFlowKeywords(t *testing.T) {
	want := map[TokenKind]string{
		If:       "if",
		ElseIf:   "elseif",
		Else:     "else",
		While:    "while",
		Break:    "break",
		Continue: "continue",
	}
	for kind, want := range want {
		if got := kind.String(); got != want {
			t.Errorf("TokenKind(%d).String() = %q, want %q", kind, got, want)
		}
	}
}

func TestLexIdentifiersContainingFunctionKeywords(t *testing.T) {
	source := "fundamental ending returns implementation selfish myself"
	tokens, err := Lex("test.hex", source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	wantLexemes := strings.Fields(source)
	if len(tokens) != len(wantLexemes)+1 {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(wantLexemes)+1)
	}
	for index, lexeme := range wantLexemes {
		if tokens[index].Kind != Identifier || tokens[index].Lexeme != lexeme {
			t.Fatalf("token %d = %#v, want identifier %q", index, tokens[index], lexeme)
		}
	}
}

func TestLexFunctionKeywordsFollowedByPunctuation(t *testing.T) {
	tokens, err := Lex("test.hex", "end)\nself.x")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: End, Lexeme: "end", Line: 1, Column: 1},
		{Kind: RightParen, Lexeme: ")", Line: 1, Column: 4},
		{Kind: Self, Lexeme: "self", Line: 2, Column: 1},
		{Kind: Dot, Lexeme: ".", Line: 2, Column: 5},
		{Kind: Identifier, Lexeme: "x", Line: 2, Column: 6},
		{Kind: EOF, Line: 2, Column: 7},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if !sameToken(tokens[index], want[index]) {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexRejectsRawNewlinesInStringLiteral(t *testing.T) {
	cases := []struct{ name, source string }{
		{"lf", "\"a\nb\""},
		{"crlf", "\"a\r\nb\""},
		{"bare-cr", "\"a\rb\""},
		{"continuation", "\"a\\\nb\""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Lex("test.hex", tc.source)
			if err == nil || !strings.Contains(err.Error(), "raw newline") {
				t.Fatalf("want raw-newline diagnostic; got %v", err)
			}
		})
	}
}

func TestLexStringEscapedNewlinesRemainValid(t *testing.T) {
	for _, source := range []string{"\"a\\nb\"", "\"a\\rb\""} {
		tokens, err := Lex("test.hex", source)
		if err != nil {
			t.Fatalf("escaped newline rejected: %v", err)
		}
		if len(tokens) != 2 || tokens[0].Kind != StringLiteral {
			t.Fatalf("want one StringLiteral token; got %#v", tokens)
		}
	}
}

func TestLexRawNewlineRecoveryKeepsSourcePositions(t *testing.T) {
	tokens, err := Lex("test.hex", "\"a\nb\" x: Int32 := 1")
	if err == nil || !strings.Contains(err.Error(), "raw newline") {
		t.Fatalf("want raw-newline diagnostic; got %v", err)
	}
	if len(tokens) == 0 || tokens[0].Kind != Identifier || tokens[0].Line != 2 {
		t.Fatalf("want identifier x on line 2; got %#v", tokens)
	}
}

func TestLexClosedMultilineStringReportsOneDiagnostic(t *testing.T) {
	_, err := Lex("test.hex", "\"a\nb\nc\"")
	if err == nil {
		t.Fatal("want a diagnostic")
	}
	if n := strings.Count(err.Error(), "raw newline"); n != 1 {
		t.Fatalf("want exactly one raw-newline diagnostic; got %d", n)
	}
}

// `:` and `=` are independent tokens. Source adjacency carries no meaning, so
// `:=` and `: =` lex identically.
func TestLexColonAndEqualAreSeparateTokens(t *testing.T) {
	for _, source := range []string{"x := 13", "x : = 13"} {
		tokens, err := Lex("test.hex", source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		wantKinds := []TokenKind{Identifier, Colon, Equal, Integer, EOF}
		if len(tokens) != len(wantKinds) {
			t.Fatalf("Lex(%q) returned %d tokens, want %d: %#v", source, len(tokens), len(wantKinds), tokens)
		}
		for index, kind := range wantKinds {
			if tokens[index].Kind != kind {
				t.Fatalf("Lex(%q) token %d kind = %v, want %v", source, index, tokens[index].Kind, kind)
			}
		}
	}
}

// A token's Span is its exact half-open byte range in its logical file. The
// EOF token is zero-width at the end offset, the insertion point a diagnostic
// at end of input reports.
func TestLexTokenSpans(t *testing.T) {
	const file = "spans.hex"
	source := "let x: Int32 = 13"
	tokens, err := Lex(file, source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []span.Span{
		{File: file, Start: 0, End: 3},   // let
		{File: file, Start: 4, End: 5},   // x
		{File: file, Start: 5, End: 6},   // :
		{File: file, Start: 7, End: 12},  // Int32
		{File: file, Start: 13, End: 14}, // =
		{File: file, Start: 15, End: 17}, // 13
		{File: file, Start: 17, End: 17}, // EOF
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if tokens[index].Span != want[index] {
			t.Errorf("token %d span = %+v, want %+v", index, tokens[index].Span, want[index])
		}
	}
}

// The retained Line and Column are the span's start under the table
// convention. This pins the agreement at a single line, across a multiline
// comment, across multibyte Unicode, and for a lone carriage return.
func TestLexRetainedLocationsMatchSpanPositions(t *testing.T) {
	const file = "positions.hex"
	sources := []string{
		"let x: Int32 = 13",
		"x: Int32 --[ comment\n   across lines ]-- = 13",
		"\"\u00e9\" x",
		"a\rb",
	}
	for _, source := range sources {
		tokens, err := Lex(file, source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		table := span.NewTable()
		table.Add(file, source)
		for index, token := range tokens {
			got := table.Position(token.Span)
			if got.Line != token.Line || got.Column != token.Column {
				t.Errorf("Lex(%q) token %d (%v) span position = %+v, retained line/column = %d:%d",
					source, index, token.Kind, got, token.Line, token.Column)
			}
		}
	}
}
