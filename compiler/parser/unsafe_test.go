package parser

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

func TestLexUnsafeKeyword(t *testing.T) {
	tokens, err := lexer.Lex("unsafe do end")
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].Kind != lexer.Unsafe {
		t.Fatalf("first token = %#v, want Unsafe", tokens[0])
	}
	if lexer.Unsafe.String() != "unsafe" {
		t.Fatalf("Unsafe.String() = %q, want %q", lexer.Unsafe.String(), "unsafe")
	}
}

func TestParseUnsafeStatement(t *testing.T) {
	statement := parseOneItem(t, "unsafe do\n    cleanup()\nend").(UnsafeStatement)
	if len(statement.Body) != 1 {
		t.Fatalf("body = %#v, want one statement", statement.Body)
	}
	if _, ok := statement.Body[0].(CallExpression); !ok {
		t.Fatalf("body statement = %#v, want call", statement.Body[0])
	}
	if statement.Keyword.Kind != lexer.Unsafe || statement.End.Kind != lexer.End {
		t.Fatalf("delimiters = %#v / %#v, want unsafe/end", statement.Keyword, statement.End)
	}
}

func TestParseEmptyUnsafeStatement(t *testing.T) {
	statement := parseOneItem(t, "unsafe do\nend").(UnsafeStatement)
	if len(statement.Body) != 0 {
		t.Fatalf("body = %#v, want no statements", statement.Body)
	}
}

func TestParseNestedUnsafeStatements(t *testing.T) {
	outer := parseOneItem(t, "unsafe do\n    unsafe do\n        cleanup()\n    end\nend").(UnsafeStatement)
	if len(outer.Body) != 1 {
		t.Fatalf("outer body = %#v, want one statement", outer.Body)
	}
	inner, ok := outer.Body[0].(UnsafeStatement)
	if !ok {
		t.Fatalf("outer body statement = %#v, want a nested unsafe block", outer.Body[0])
	}
	if len(inner.Body) != 1 {
		t.Fatalf("inner body = %#v, want one statement", inner.Body)
	}
}

func TestParseUnsafeRequiresDoAndEnd(t *testing.T) {
	for _, testCase := range []struct{ source, want string }{
		{"unsafe\n    cleanup()\nend", "'do' after 'unsafe'"},
		{"unsafe do\n    cleanup()\n", "expected end to close unsafe"},
	} {
		if _, err := Parse(mustLex(t, testCase.source)); err == nil || !strings.Contains(err.Error(), testCase.want) {
			t.Fatalf("Parse(%q) error = %v, want %q", testCase.source, err, testCase.want)
		}
	}
}

// `unsafe` is a reserved word: it can no longer name a binding.
func TestUnsafeIsReserved(t *testing.T) {
	if _, err := Parse(mustLex(t, "unsafe: Int32 := 1")); err == nil {
		t.Fatalf("Parse accepted unsafe as a binding name")
	}
}
