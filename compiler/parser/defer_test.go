package parser

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

func TestLexDeferKeyword(t *testing.T) {
	tokens, err := lexer.Lex("defer cleanup()")
	if err != nil {
		t.Fatal(err)
	}
	if tokens[0].Kind != lexer.Defer {
		t.Fatalf("first token = %#v, want Defer", tokens[0])
	}
}

func TestParseDeferStatement(t *testing.T) {
	program := parseOneItem(t, "defer cleanup()").(DeferStatement)
	if _, ok := program.Expression.(CallExpression); !ok {
		t.Fatalf("expression = %#v, want call", program.Expression)
	}
}

func TestParseDeferValueExpression(t *testing.T) {
	program := parseOneItem(t, "defer value").(DeferStatement)
	if _, ok := program.Expression.(VariableExpression); !ok {
		t.Fatalf("expression = %#v, want variable", program.Expression)
	}
}

func TestParseDeferRejectsBlockStatements(t *testing.T) {
	for _, source := range []string{"defer if flag cleanup() end", "defer while flag do cleanup() end", "defer defer cleanup()"} {
		if _, err := Parse(mustLex(t, source)); err == nil || !strings.Contains(err.Error(), "expected a value") {
			t.Fatalf("Parse(%q) error = %v, want value diagnostic", source, err)
		}
	}
}
