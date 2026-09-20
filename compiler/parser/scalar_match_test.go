package parser

// Scalar value-mode match patterns: literal arms and the eos singleton.

import (
	"testing"

	"hexal/compiler/lexer"
)

// parseMatchArms parses one match expression and returns its arms.
func parseMatchArms(t *testing.T, source string) []MatchArm {
	t.Helper()
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatal(err)
	}
	program, err := Parse(tokens)
	if err != nil {
		t.Fatal(err)
	}
	match, ok := program.Statements[0].(Declaration).Initializer.(MatchExpression)
	if !ok {
		t.Fatalf("initializer = %#v, want MatchExpression", program.Statements[0])
	}
	return match.Arms
}

func TestParseScalarMatchPatterns(t *testing.T) {
	arms := parseMatchArms(t, "let r: Int32 = match op\n| 1 then 1\n| -1 then 2\n| 0x1 then 3\n| 'a' then 4\n| b'a' then 5\n| eos then 6\n| else then 7\nend")
	if len(arms) != 7 {
		t.Fatalf("arms = %d, want 7", len(arms))
	}
	for index := 0; index < 5; index++ {
		if _, ok := arms[index].Pattern.(ScalarPattern); !ok {
			t.Fatalf("arm %d pattern = %T, want ScalarPattern", index, arms[index].Pattern)
		}
	}
	if _, ok := arms[5].Pattern.(EosPattern); !ok {
		t.Fatalf("arm 5 pattern = %T, want EosPattern", arms[5].Pattern)
	}
	if _, ok := arms[6].Pattern.(ElsePattern); !ok {
		t.Fatalf("arm 6 pattern = %T, want ElsePattern", arms[6].Pattern)
	}
	unsigned, ok := arms[0].Pattern.(ScalarPattern)
	if !ok || unsigned.Minus.Kind == lexer.Minus {
		t.Fatalf("arm 0 = %#v, want an unsigned ScalarPattern", arms[0].Pattern)
	}
	negated, ok := arms[1].Pattern.(ScalarPattern)
	if !ok || negated.Minus.Kind != lexer.Minus {
		t.Fatalf("arm 1 = %#v, want a negated ScalarPattern", arms[1].Pattern)
	}
}

// A bare identifier arm stays a type or neutral dotted pattern; only literal
// arms become ScalarPattern.
func TestParseScalarMatchKeepsTypePatterns(t *testing.T) {
	arms := parseMatchArms(t, "let r: Int32 = match op is\n| Foo then 1\n| A.B then 2\n| else then 3\nend")
	if _, ok := arms[0].Pattern.(TypePattern); !ok {
		t.Fatalf("arm 0 pattern = %T, want TypePattern", arms[0].Pattern)
	}
	if _, ok := arms[1].Pattern.(DottedPattern); !ok {
		t.Fatalf("arm 1 pattern = %T, want DottedPattern", arms[1].Pattern)
	}
}

// Float and string literal arms remain outside ScalarPattern.
func TestParseScalarMatchRejectsFloatAndStringArms(t *testing.T) {
	for _, arm := range []string{"| 1.5 then 1", `| "a" then 1`} {
		tokens, err := lexer.Lex("let r: Int32 = match op\n" + arm + "\n| else then 0\nend")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Parse(tokens); err == nil {
			t.Fatalf("arm %q parsed, want a parse error", arm)
		}
	}
}
