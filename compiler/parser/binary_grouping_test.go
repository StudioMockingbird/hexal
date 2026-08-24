package parser

import (
	"fmt"
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

// binaryOperatorSample is one of the 19 binary operator token kinds this RFC
// governs, spelled exactly as it appears in source, plus a right-hand
// fragment that completes one use of it into a syntactically valid partial
// expression. is is the only kind whose right side is a type expression
// rather than a value.
type binaryOperatorSample struct {
	lexeme string
	kind   lexer.TokenKind
	rhs    string
}

var allBinaryOperatorSamples = []binaryOperatorSample{
	{"*", lexer.Star, "b"},
	{"/", lexer.Slash, "b"},
	{"%", lexer.Percent, "b"},
	{"+", lexer.Plus, "b"},
	{"-", lexer.Minus, "b"},
	{"<<", lexer.ShiftLeft, "b"},
	{">>", lexer.ShiftRight, "b"},
	{"<", lexer.Less, "b"},
	{"<=", lexer.LessEqual, "b"},
	{">", lexer.Greater, "b"},
	{">=", lexer.GreaterEqual, "b"},
	{"==", lexer.EqualEqual, "b"},
	{"!=", lexer.BangEqual, "b"},
	{"&", lexer.Amp, "b"},
	{"^", lexer.Caret, "b"},
	{"|", lexer.Pipe, "b"},
	{"and", lexer.And, "b"},
	{"or", lexer.Or, "b"},
	{"is", lexer.Is, "Int32"},
}

// Every binary operator kind parses on its own, and a chain of three
// repeated uses of the same kind parses without a mixed-operator diagnostic.
// is is excluded: chained is tests are rejected by an existing, unrelated
// rule.
func TestRepeatedSameBinaryOperatorChainsParse(t *testing.T) {
	for _, sample := range allBinaryOperatorSamples {
		if sample.kind == lexer.Is {
			continue
		}
		source := fmt.Sprintf("result: Bool := a %s b %s c %s d", sample.lexeme, sample.lexeme, sample.lexeme)
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err != nil {
			t.Errorf("Parse(%q) = %v, want a repeated %q chain to parse", source, err, sample.lexeme)
		}
	}
}

// isUnreachableAfterIsCompletes reports whether second can never follow a
// completed is test in one grammatically meaningful sequence. is's own right
// side is a type expression, not a value: once it completes, only a level at
// or above equality in the ladder (whose own right-recursion redescends
// through is) can still see a following operator in the same region.
// Relational, shift, additive, and multiplicative kinds sit below is and are
// simply unreachable there, leaving trailing tokens no level ever consumes.
// | is separately exempt: an unparenthesized | on is's right side is a
// type-expression union separator, never an expression operator, so
// "is Int32 | b" is one complete type test, not two operators.
func isUnreachableAfterIsCompletes(second binaryOperatorSample) bool {
	switch second.kind {
	case lexer.Star, lexer.Slash, lexer.Percent, lexer.Plus, lexer.Minus,
		lexer.ShiftLeft, lexer.ShiftRight,
		lexer.Less, lexer.LessEqual, lexer.Greater, lexer.GreaterEqual,
		lexer.Pipe:
		return true
	default:
		return false
	}
}

// Every ordered pair of different binary operator kinds is rejected in one
// unparenthesized expression region, with the diagnostic naming the first
// operator, the first differing later operator, and the later token's
// position. Pairs where is is first and second is unreachable after it
// completes (see isUnreachableAfterIsCompletes) are not one grammatically
// meaningful region and are excluded.
func TestMixedBinaryOperatorPairsAreRejected(t *testing.T) {
	for _, first := range allBinaryOperatorSamples {
		for _, second := range allBinaryOperatorSamples {
			if first.kind == second.kind {
				continue
			}
			if first.kind == lexer.Is && isUnreachableAfterIsCompletes(second) {
				continue
			}
			source := fmt.Sprintf("result: Bool := a %s %s %s %s", first.lexeme, first.rhs, second.lexeme, second.rhs)
			tokens, err := lexer.Lex(source)
			if err != nil {
				t.Fatalf("Lex(%q) returned an error: %v", source, err)
			}
			_, parseErr := Parse(tokens)
			if parseErr == nil {
				t.Errorf("Parse(%q) accepted mixed %q and %q", source, first.lexeme, second.lexeme)
				continue
			}
			want := fmt.Sprintf("mixed binary operators require parentheses; found '%s' after '%s'", second.lexeme, first.lexeme)
			if !strings.Contains(parseErr.Error(), want) {
				t.Errorf("Parse(%q) error = %q, want it to contain %q", source, parseErr.Error(), want)
			}
		}
	}
}

// Parentheses permit every pair of different binary operators in both
// nesting directions, and the resulting tree keeps the parenthesized side
// as the corresponding operand.
func TestParenthesizedMixedOperatorsAcceptBothNestingDirections(t *testing.T) {
	rightNested := parseInitializer(t, "x: Int32 := a + (b * c)").(BinaryExpression)
	if rightNested.Operator.Kind != lexer.Plus {
		t.Fatalf("root = %v, want +", rightNested.Operator.Kind)
	}
	if _, ok := rightNested.Right.(BinaryExpression); !ok {
		t.Fatalf("right operand = %#v, want a nested binary expression", rightNested.Right)
	}

	leftNested := parseInitializer(t, "x: Int32 := (a * b) + c").(BinaryExpression)
	if leftNested.Operator.Kind != lexer.Plus {
		t.Fatalf("root = %v, want +", leftNested.Operator.Kind)
	}
	if _, ok := leftNested.Left.(BinaryExpression); !ok {
		t.Fatalf("left operand = %#v, want a nested binary expression", leftNested.Left)
	}
}

// Different operators in independently delimited nested expressions do not
// conflict with each other or with the operator in their containing region:
// grouping, call arguments, index expressions, array elements, and object
// member initializers each own their own region.
func TestNestedExpressionRegionsIsolateOperatorKinds(t *testing.T) {
	for _, source := range []string{
		// Grouping: the containing region's + does not see the group's *.
		"x: Int32 := (a * b) + (c / d)",
		// Call arguments: each argument is its own region.
		"x: Int32 := outer(a + b, c * d)",
		// Index expression: the index's + is independent of the outer *.
		"x: Int32 := values[a + b] * scale",
		// Array elements: each element is its own region.
		"x: Array<Int32, 2> := [a + b, c * d]",
		// Object member initializers: each initializer is its own region,
		// independent of a mixed root region.
		"x: Point := Point { a = b + c, d = e * f } and flag",
	} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err != nil {
			t.Errorf("Parse(%q) = %v, want nested regions to isolate operator kinds", source, err)
		}
	}
}

// A match scrutinee and each arm result are independent regions: none of
// their operators conflict with each other or with the containing region.
func TestMatchRegionsIsolateOperatorKinds(t *testing.T) {
	source := "result: Int32 := match flag and other\n| true then a + b\n| false then c * d\nend\n"
	tokens, err := lexer.Lex(source)
	if err != nil {
		t.Fatalf("Lex(%q) returned an error: %v", source, err)
	}
	if _, err := Parse(tokens); err != nil {
		t.Errorf("Parse(%q) = %v, want the scrutinee and each arm to be independent regions", source, err)
	}
}

// A parenthesized type test may participate in another binary expression,
// and a union type on the right of is does not count its | as an expression
// operator.
func TestTypeTestNestingAndUnionRightHandSide(t *testing.T) {
	for _, source := range []string{
		"x: Bool := (value is Int32) == true",
		"x: Bool := (value is Int32) and ready",
		"x: Bool := value is Int32 | String",
	} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err != nil {
			t.Errorf("Parse(%q) = %v, want it to parse", source, err)
		}
	}
}

// Parentheses permit every ordered pair of different binary operator kinds,
// in both nesting directions: the parenthesized side is a self-contained
// region regardless of which operator surrounds it.
func TestParenthesesPermitEveryMixedOperatorPair(t *testing.T) {
	for _, first := range allBinaryOperatorSamples {
		for _, second := range allBinaryOperatorSamples {
			if first.kind == second.kind {
				continue
			}
			leftGrouped := fmt.Sprintf("result: Bool := (a %s %s) %s %s", first.lexeme, first.rhs, second.lexeme, second.rhs)
			tokens, err := lexer.Lex(leftGrouped)
			if err != nil {
				t.Fatalf("Lex(%q) returned an error: %v", leftGrouped, err)
			}
			if _, err := Parse(tokens); err != nil {
				t.Errorf("Parse(%q) = %v, want the left-grouped pair to parse", leftGrouped, err)
			}

			if second.kind == lexer.Is {
				// is cannot appear as the ungrouped, right-hand outer operator
				// with a value on its right: its own right side is always a
				// type. Left-grouping already covers this ordering.
				continue
			}
			rightGrouped := fmt.Sprintf("result: Bool := %s %s (a %s %s)", "b", second.lexeme, first.lexeme, first.rhs)
			tokens, err = lexer.Lex(rightGrouped)
			if err != nil {
				t.Fatalf("Lex(%q) returned an error: %v", rightGrouped, err)
			}
			if _, err := Parse(tokens); err != nil {
				t.Errorf("Parse(%q) = %v, want the right-grouped pair to parse", rightGrouped, err)
			}
		}
	}
}

// Unary and postfix forms are not binary operators and remain accepted
// alongside one binary operator kind.
func TestUnaryAndPostfixFormsMixFreelyWithOneBinaryKind(t *testing.T) {
	for _, source := range []string{
		"result: Int32 := -a + -b",
		"ready: Bool := !left and !right",
		"total: Int32 := values[i].amount() + values[j].amount()",
	} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err != nil {
			t.Errorf("Parse(%q) = %v, want unary/postfix forms to mix freely with one binary kind", source, err)
		}
	}
}

// A chained is test is rejected by its own existing diagnostic even when a
// differing operator follows: the chained-is check fires immediately after
// the second is completes, before any later operator could be recorded.
func TestChainedTypeTestRetainsOwnershipOverMixedOperator(t *testing.T) {
	message := parseError(t, "tested: Bool := value is Int32 is Bool and ready")
	if !strings.Contains(message, "is tests cannot be chained") {
		t.Fatalf("message = %q, want the chained-is diagnostic", message)
	}
}
