package hexal_test

import (
	_ "embed"
	"strings"
	"testing"

	"golang.org/x/exp/ebnf"
)

//go:embed GRAMMAR.ebnf
var grammarSource string

func TestGrammarIsVerifiable(t *testing.T) {
	grammar, err := ebnf.Parse("GRAMMAR.ebnf", strings.NewReader(grammarSource))
	if err != nil {
		t.Fatalf("grammar parse failed: %v", err)
	}
	if err := ebnf.Verify(grammar, "Program"); err != nil {
		t.Fatalf("grammar verification failed: %v", err)
	}
}
