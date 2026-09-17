package lexer

import "testing"

// `...` lexes as one Ellipsis token by longest match; `.` member selection is
// unchanged, and `..` is still two Dot tokens.
func TestLexEllipsisByLongestMatch(t *testing.T) {
	tokens, err := Lex("a.b a... a..b")
	if err != nil {
		t.Fatal(err)
	}
	want := []TokenKind{
		Identifier, Dot, Identifier,
		Identifier, Ellipsis,
		Identifier, Dot, Dot, Identifier,
		EOF,
	}
	if len(tokens) != len(want) {
		t.Fatalf("got %d tokens, want %d: %#v", len(tokens), len(want), tokens)
	}
	for index, kind := range want {
		if tokens[index].Kind != kind {
			t.Fatalf("token %d = %v, want %v", index, tokens[index].Kind, kind)
		}
	}
	if tokens[4].Lexeme != "..." {
		t.Fatalf("ellipsis lexeme = %q, want ...", tokens[4].Lexeme)
	}
}
