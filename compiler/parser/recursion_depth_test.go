package parser

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

// The parser enforces one shared recursive-syntax budget of 128 across every
// recursively entered production: expressions, type expressions, and
// statement blocks. Each helper below wraps a leaf in N repetitions of one
// construct. The outermost parse call is itself one budget entry, and
// reaching the innermost leaf consumes one more (an identifier or a
// single-element array/paren body still requires one final recursive call to
// parse), so N repetitions consume N+1 entries for every construct tested
// here. 127 repetitions therefore lands exactly at the 128 limit and must
// parse; 128 repetitions is one level past it and must be rejected.

func nestedParens(depth int) string {
	return "x: Int32 := " + strings.Repeat("(", depth) + "1" + strings.Repeat(")", depth)
}

func nestedArrayLiterals(depth int) string {
	return "x := " + strings.Repeat("[", depth) + "1" + strings.Repeat("]", depth)
}

func nestedMutPtrType(depth int) string {
	return "fun f(p: " + strings.Repeat("MutPtr<", depth) + "Int32" + strings.Repeat(">", depth) + ") do\nend"
}

func nestedIfBlocks(depth int) string {
	var source strings.Builder
	source.WriteString("fun run() do\n")
	for i := 0; i < depth; i++ {
		source.WriteString("if true then\n")
	}
	for i := 0; i < depth; i++ {
		source.WriteString("end\n")
	}
	source.WriteString("end")
	return source.String()
}

// mustParseOK fails the test if source does not parse cleanly.
func mustParseOK(t *testing.T, source string) {
	t.Helper()
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("Lex returned an error: %v", err)
	}
	if _, err := Parse(tokens); err != nil {
		t.Fatalf("Parse returned an error: %v", err)
	}
}

func TestNestingAtTheLimitParses(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"parentheses", nestedParens(127)},
		{"array literals", nestedArrayLiterals(127)},
		{"type constructors", nestedMutPtrType(127)},
		{"nested blocks", nestedIfBlocks(127)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			mustParseOK(t, testCase.source)
		})
	}
}

func TestNestingOneLevelPastTheLimitIsRejected(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"parentheses", nestedParens(128)},
		{"array literals", nestedArrayLiterals(128)},
		{"type constructors", nestedMutPtrType(128)},
		{"nested blocks", nestedIfBlocks(128)},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			message := parseError(t, testCase.source)
			if !strings.Contains(message, "nesting exceeds the maximum depth of 128") {
				t.Fatalf("%s error = %q, want the depth-exceeded message", testCase.name, message)
			}
		})
	}
}

// A pathological 100,000-parenthesis input is the original crash
// reproduction: unbounded recursive descent terminated the process with a Go
// fatal stack overflow, which recover() cannot catch because it is not a
// panic. The parser must reject the input with an ordinary diagnostic well
// before the Go call stack is threatened.
func TestPathologicalNestingSurvivesAndIsRejected(t *testing.T) {
	message := parseError(t, nestedParens(100000))
	if !strings.Contains(message, "nesting exceeds the maximum depth of 128") {
		t.Fatalf("error = %q, want the depth-exceeded message", message)
	}
}
