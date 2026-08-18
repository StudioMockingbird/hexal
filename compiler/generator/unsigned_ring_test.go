package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// The redundant-parenthesis removal is a readability pass, not a correctness
// one: with it disabled the construction emits a pair around every operand,
// and the two renderings must differ only in punctuation. Comparing the
// non-parenthesis tokens of both is exactly that claim.
func TestRingParenthesisRemovalChangesOnlyPunctuation(t *testing.T) {
	typ := compilerTypes.UInt32
	ring := func(operator checker.Operator, left, right checker.Expression) checker.Expression {
		return binaryExpression(operator, typ, typ, left, right)
	}
	a, b, c, d := variableNode("a"), variableNode("b"), variableNode("c"), variableNode("d")
	for _, node := range []checker.Expression{
		ring(checker.AddOperator, ring(checker.AddOperator, a, b), c),
		ring(checker.MultiplyOperator, a, ring(checker.SubtractOperator, b, c)),
		ring(checker.MultiplyOperator, ring(checker.AddOperator, a, b), c),
		ring(checker.AddOperator, a, ring(checker.MultiplyOperator, b, c)),
		ring(checker.SubtractOperator, ring(checker.MultiplyOperator, a, b), ring(checker.AddOperator, c, d)),
	} {
		prettified, err := renderExpression(node)
		if err != nil {
			t.Fatalf("renderExpression error = %v", err)
		}
		ringKeepEveryGrouping = true
		constructed, err := renderExpression(node)
		ringKeepEveryGrouping = false
		if err != nil {
			t.Fatalf("renderExpression (grouped) error = %v", err)
		}
		if withoutParentheses(prettified) != withoutParentheses(constructed) {
			t.Errorf("prettified %q and constructed %q differ beyond punctuation", prettified, constructed)
		}
		if len(prettified) > len(constructed) {
			t.Errorf("prettified %q is longer than the constructed form %q", prettified, constructed)
		}
	}
}

func withoutParentheses(rendered string) string {
	return strings.NewReplacer("(", "", ")", "", " ", "").Replace(rendered)
}
