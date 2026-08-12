package lexer

import (
	"strings"
	"testing"
)

func TestLexDeclaration(t *testing.T) {
	tokens, err := Lex("x: Int32 = 13")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: Identifier, Lexeme: "x", Line: 1, Column: 1},
		{Kind: Colon, Lexeme: ":", Line: 1, Column: 2},
		{Kind: Identifier, Lexeme: "Int32", Line: 1, Column: 4},
		{Kind: Equal, Lexeme: "=", Line: 1, Column: 10},
		{Kind: Integer, Lexeme: "13", Line: 1, Column: 12},
		{Kind: EOF, Line: 1, Column: 14},
	}

	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if tokens[index] != want[index] {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexRejectsUnexpectedCharacter(t *testing.T) {
	_, err := Lex("x: Int32 = @")
	if err == nil {
		t.Fatal("Lex accepted an unexpected character")
	}
	if err.Error() != `[Syntax Error] unexpected character '@' at 1:12` {
		t.Fatalf("Lex error = %q", err)
	}
}

func TestLexBooleanKeywordsAndIdentifiers(t *testing.T) {
	tokens, err := Lex("true false trueValue")
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
		if tokens[index] != want[index] {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexNilPipeAndProtectedTypeIdentifiers(t *testing.T) {
	tokens, err := Lex("nil | Nil Unknown nilValue")
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
	tokens, err := Lex("value is Int32 | Nil")
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
	tokens, err := Lex("a!=b==c<=d>=e+f*g/h%i!() and or android")
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
	tokens, err := Lex("!= ! == = <= < >= >\nPtr<Ptr<Int32>>\n-- comment\nand or")
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
		if tokens[index] != want[index] {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexRejectsLeadingUnderscoreIdentifier(t *testing.T) {
	_, err := Lex("_player: Int32 = 1")
	if err == nil {
		t.Fatal("Lex accepted an identifier beginning with an underscore")
	}
	if got, want := err.Error(), "[Syntax Error] identifiers must begin with a letter at 1:1"; got != want {
		t.Fatalf("Lex error = %q, want %q", got, want)
	}
}

func TestLexRejectsDigitStartIdentifier(t *testing.T) {
	_, err := Lex("2player: Int32 = 2")
	if err == nil {
		t.Fatal("Lex accepted an identifier beginning with a digit")
	}
	if got, want := err.Error(), "[Syntax Error] identifiers must begin with a letter at 1:1"; got != want {
		t.Fatalf("Lex error = %q, want %q", got, want)
	}
}

func TestLexAcceptsUnderscoreAfterLetter(t *testing.T) {
	tokens, err := Lex("player_2")
	if err != nil {
		t.Fatalf("Lex rejected an underscore after the leading letter: %v", err)
	}
	if got, want := tokens[0].Lexeme, "player_2"; got != want {
		t.Fatalf("identifier lexeme = %q, want %q", got, want)
	}
}

func TestLexPointerKeywordsAndProperties(t *testing.T) {
	tokens, err := Lex("ref mut Ptr<Int32>.value")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: Ref, Lexeme: "ref", Line: 1, Column: 1},
		{Kind: Mut, Lexeme: "mut", Line: 1, Column: 5},
		{Kind: Identifier, Lexeme: "Ptr", Line: 1, Column: 9},
		{Kind: Less, Lexeme: "<", Line: 1, Column: 12},
		{Kind: Identifier, Lexeme: "Int32", Line: 1, Column: 13},
		{Kind: Greater, Lexeme: ">", Line: 1, Column: 18},
		{Kind: Dot, Lexeme: ".", Line: 1, Column: 19},
		{Kind: Identifier, Lexeme: "value", Line: 1, Column: 20},
		{Kind: EOF, Line: 1, Column: 25},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if tokens[index] != want[index] {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestLexTypeKeywordAndPtrIdentifier(t *testing.T) {
	tokens, err := Lex("type Coordinate = Ptr<Int32>")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	wantKinds := []TokenKind{Type, Identifier, Equal, Identifier, Less, Identifier, Greater, EOF}
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

func TestLexObjectDelimiters(t *testing.T) {
	tokens, err := Lex("type Point = { mut x: Int32, y: Int32, }")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	wantKinds := []TokenKind{
		Type, Identifier, Equal, LeftBrace, Mut, Identifier, Colon,
		Identifier, Comma, Identifier, Colon, Identifier, Comma, RightBrace, EOF,
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
	tokens, err := Lex("- 0xFF 0b1010_0101 0o755 1_000 3.14 7e3")
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
	tokens, err := Lex("mask: Int32 = 0xFF")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if got, want := tokens[4].Kind, HexInteger; got != want {
		t.Fatalf("hex token kind = %v, want %v", got, want)
	}
	if got, want := tokens[4].Lexeme, "0xFF"; got != want {
		t.Fatalf("hex token lexeme = %q, want %q", got, want)
	}
}

func TestLexRejectsMalformedHexadecimalInteger(t *testing.T) {
	for _, source := range []string{"x: Int32 = 0x", "x: Int32 = 0xG", "x: Int32 = 0x12G"} {
		_, err := Lex(source)
		if err == nil {
			t.Fatalf("Lex accepted malformed hexadecimal literal in %q", source)
		}
		want := "[Syntax Error] malformed hexadecimal literal at 1:12"
		if got := err.Error(); got != want {
			t.Fatalf("Lex error for %q = %q, want %q", source, got, want)
		}
	}

	_, err := Lex("x: Int32 = 0XFF")
	if err == nil {
		t.Fatal("Lex accepted an uppercase hexadecimal prefix")
	}
	if got, want := err.Error(), "[Syntax Error] integer base prefixes must be lowercase at 1:12"; got != want {
		t.Fatalf("Lex error = %q, want %q", got, want)
	}
}

func TestLexSkipsSingleLineAndDocumentationComments(t *testing.T) {
	source := "--- declare the counter\nx: Int32 = 13 -- initialize\nx = 14 -- eof"
	tokens, err := Lex(source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if len(tokens) != 9 {
		t.Fatalf("Lex returned %d tokens, want 9", len(tokens))
	}
	if got, want := tokens[0], (Token{Kind: Identifier, Lexeme: "x", Line: 2, Column: 1}); got != want {
		t.Fatalf("first token = %#v, want %#v", got, want)
	}
	if got, want := tokens[5], (Token{Kind: Identifier, Lexeme: "x", Line: 3, Column: 1}); got != want {
		t.Fatalf("second statement token = %#v, want %#v", got, want)
	}
}

func TestLexSkipsMultilineCommentAndTracksLocation(t *testing.T) {
	source := "x: Int32 --[ comment\n   across lines ]-- = 13"
	tokens, err := Lex(source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	if got, want := tokens[3], (Token{Kind: Equal, Lexeme: "=", Line: 2, Column: 21}); got != want {
		t.Fatalf("equal token = %#v, want %#v", got, want)
	}
	if got, want := tokens[4], (Token{Kind: Integer, Lexeme: "13", Line: 2, Column: 23}); got != want {
		t.Fatalf("integer token = %#v, want %#v", got, want)
	}
}

func TestLexRejectsUnterminatedMultilineComment(t *testing.T) {
	_, err := Lex("x: Int32 = --[ missing close")
	if err == nil {
		t.Fatal("Lex accepted an unterminated multiline comment")
	}
	if got, want := err.Error(), "[Syntax Error] unterminated multiline comment at 1:12"; got != want {
		t.Fatalf("Lex error = %q, want %q", got, want)
	}
}

func TestLexByteAndNumericLiterals(t *testing.T) {
	tokens, err := Lex("b'A' 1_000 3.14 6.02e23")
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
	tokens, err := Lex("'A' 'é' '\\n' '\\u{1F980}'")
	if err != nil || len(tokens) != 5 {
		t.Fatalf("Lex returned tokens=%#v err=%v, want four Rune literals and EOF", tokens, err)
	}
	for _, token := range tokens[:4] {
		if token.Kind != RuneLiteral {
			t.Fatalf("token = %#v, want a RuneLiteral", token)
		}
	}
}

func TestLexRejectsInvalidByteForms(t *testing.T) {
	for _, source := range []string{"b'13'", "b'\\13'", "b'\\0xFF'", "b'\\xF'", "b'\\xFFF'", "b'é'", "b'\\u{41}'", "b'ab'"} {
		_, err := Lex(source)
		if err == nil || !strings.Contains(err.Error(), "Byte literal") && !strings.Contains(err.Error(), "escape") {
			t.Fatalf("Lex(%q) error = %v, want a Byte literal diagnostic", source, err)
		}
	}
}

func TestLexRejectsInvalidRuneForms(t *testing.T) {
	for _, source := range []string{"'ab'", "'\\u{D800}'", "'\\u{110000}'", "'\\x41'"} {
		_, err := Lex(source)
		if err == nil || !strings.Contains(err.Error(), "Rune literal") && !strings.Contains(err.Error(), "escape") && !strings.Contains(err.Error(), "Unicode scalar") {
			t.Fatalf("Lex(%q) error = %v, want a Rune literal diagnostic", source, err)
		}
	}
}

func TestLexFunctionKeywords(t *testing.T) {
	tokens, err := Lex("fun impl\nend return self")
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}

	want := []Token{
		{Kind: Fun, Lexeme: "fun", Line: 1, Column: 1},
		{Kind: Impl, Lexeme: "impl", Line: 1, Column: 5},
		{Kind: End, Lexeme: "end", Line: 2, Column: 1},
		{Kind: Return, Lexeme: "return", Line: 2, Column: 5},
		{Kind: Self, Lexeme: "self", Line: 2, Column: 12},
		{Kind: EOF, Line: 2, Column: 16},
	}
	if len(tokens) != len(want) {
		t.Fatalf("Lex returned %d tokens, want %d", len(tokens), len(want))
	}
	for index := range want {
		if tokens[index] != want[index] {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}

func TestTokenKindStringForFunctionKeywords(t *testing.T) {
	want := map[TokenKind]string{
		Fun:    "fun",
		Impl:   "impl",
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
	tokens, err := Lex("if elseif else while\nbreak continue")
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
		if tokens[index] != want[index] {
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
	tokens, err := Lex(source)
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
	tokens, err := Lex("end)\nself.x")
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
		if tokens[index] != want[index] {
			t.Fatalf("token %d = %#v, want %#v", index, tokens[index], want[index])
		}
	}
}
