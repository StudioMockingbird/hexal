// ring.go owns wrapping arithmetic lowering: signed overflow via compiler
// builtins and the unsigned modular ring's grouping and precedence.
package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func renderSignedWrap(operator checker.Operator, typ compilerTypes.Type, left, right string) (string, error) {
	if !compilerTypes.IsSignedInteger(typ) {
		return "", unknownExpressionDiagnostic("signed wrapping requires a signed integer type")
	}
	var name string
	switch operator {
	case checker.NegateOperator:
		name = "neg"
	case checker.AddOperator:
		name = "add"
	case checker.SubtractOperator:
		name = "sub"
	case checker.MultiplyOperator:
		name = "mul"
	case checker.InvalidOperator, checker.LogicalNotOperator, checker.DivideOperator,
		checker.RemainderOperator, checker.EqualOperator, checker.NotEqualOperator,
		checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator,
		checker.GreaterEqualOperator, checker.LogicalAndOperator, checker.LogicalOrOperator:
		return "", unknownExpressionDiagnostic("operator is not signed wrapping arithmetic")
	default:
		return "", unknownExpressionDiagnostic("unknown signed wrapping operator")
	}
	helper := wrapHelperName(wrapOperation{name: name, typ: typ})
	if name == "neg" {
		if right == "" {
			return "", unknownExpressionDiagnostic("signed negation without an operand")
		}
		return helper + "(" + right + ")", nil
	}
	if left == "" || right == "" {
		return "", unknownExpressionDiagnostic("signed operation without both operands")
	}
	return helper + "(" + left + ", " + right + ")", nil
}

// ringKeepEveryGrouping disables the redundant-parenthesis removal, leaving
// the construction's maximally parenthesized output. Correctness lives in the
// construction and readability in the removal, so a test can render both and
// assert they differ only in punctuation. Never set outside a test.
var ringKeepEveryGrouping = false

// isUnsignedRingOperation reports whether node is one ring operation: an
// unsigned +, -, or * whose operand and result type are the same unsigned
// integer type. Reduction modulo 2^N after arithmetic modulo 2^M is the same
// value as reducing after every node, so a connected tree of these evaluates
// in one uintmax_t domain and narrows once. Division, remainder,
// shifts, bitwise operations, comparisons, and conversions are not ring
// operations and terminate a tree.
func isUnsignedRingOperation(node checker.Expression) bool {
	if node.Kind != checker.BinaryOperationExpression || node.Left == nil || node.Right == nil {
		return false
	}
	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator:
	default:
		return false
	}
	return compilerTypes.IsUnsignedInteger(node.OperandType) &&
		compilerTypes.Equal(node.OperandType, node.ResultType)
}

// renderUnsignedRingTree renders one maximal ring tree: the whole tree
// evaluates in uintmax_t and narrows exactly once to its Hexal type, instead
// of widening and narrowing at every binary node.
func renderUnsignedRingTree(node checker.Expression, state *expressionValidation) (string, error) {
	unsigned, ok := unsignedCName(node.OperandType)
	if !ok {
		return "", unknownExpressionDiagnostic("unsigned arithmetic has an invalid width")
	}
	wide, err := renderRingWide(node, state)
	if err != nil {
		return "", err
	}
	return "(" + unsigned + ")(" + wide + ")", nil
}

// renderRingWide renders one ring node in the uintmax_t domain, without the
// narrowing cast its tree root carries. Per-node validation matches the
// ordinary binary path so a malformed node still fails closed.
func renderRingWide(node checker.Expression, state *expressionValidation) (string, error) {
	if !supportedGeneratedScalarType(node.OperandType) || !supportedGeneratedScalarType(node.ResultType) {
		return "", unknownExpressionDiagnostic("binary operation with an unsupported type")
	}
	if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
		return "", err
	}
	if err := validateExpressionChildWithState(node.Right, node.OperandType, state); err != nil {
		return "", err
	}
	operator, ok := binaryCOperator(node.Operator)
	if !ok {
		return "", unknownExpressionDiagnostic("unknown binary operator")
	}
	left, err := renderRingOperand(*node.Left, &node, true, state)
	if err != nil {
		return "", err
	}
	right, err := renderRingOperand(*node.Right, &node, false, state)
	if err != nil {
		return "", err
	}
	return left + " " + operator + " " + right, nil
}

// renderRingOperand renders one operand of a ring operation. A same-type ring
// child stays in the uintmax_t domain and is rendered here rather than through
// the ordinary renderer, which is what makes every arrival at
// renderBinaryOperationWithState a maximal tree root. Every other child is a
// boundary: it renders at the Hexal type through the ordinary renderer, and
// re-enters that renderer exactly once, so a ring subtree nested under a
// boundary starts its own tree with its own seed.
//
// A left boundary carries the single uintmax_t seed; C's usual arithmetic
// conversions then lift the right operand of that node and of every node above
// it. A right boundary needs no seed for the same reason.
func renderRingOperand(child checker.Expression, parent *checker.Expression, isLeft bool, state *expressionValidation) (string, error) {
	if isUnsignedRingOperation(child) && compilerTypes.Equal(child.OperandType, parent.OperandType) {
		inner, err := renderRingWide(child, state)
		if err != nil {
			return "", err
		}
		if ringGroupingRequired(parent.Operator, child.Operator, isLeft) {
			return "(" + inner + ")", nil
		}
		return inner, nil
	}
	rendered, atomic, err := renderExpressionNodeWithExpectedState(child, &parent.OperandType, state)
	if err != nil {
		return "", err
	}
	if ringKeepEveryGrouping || !atomic && child.Kind != checker.ConstantExpression {
		// A boundary that is not one C atom keeps its grouping: a division,
		// remainder, shift, bitwise expression, or comparison can lose
		// against its ring parent's precedence. Parenthesizing without
		// consulting a precedence table means the only possible error is
		// noisier C, never a different parse.
		rendered = "(" + rendered + ")"
	}
	if isLeft {
		return "(uintmax_t)" + rendered, nil
	}
	return rendered, nil
}

// ringGroupingRequired reports whether a ring child needs its own parentheses
// under a ring parent. Both sides render in the same C precedence class, so
// the usual rule applies: a left child may share its parent's precedence, a
// right child may not. Removing a pair only where the C parse is provably
// identical keeps grouping a property of the tree rather than of the text.
func ringGroupingRequired(parent, child checker.Operator, isLeft bool) bool {
	if ringKeepEveryGrouping {
		return true
	}
	if isLeft {
		return ringPrecedence(child) < ringPrecedence(parent)
	}
	return ringPrecedence(child) <= ringPrecedence(parent)
}

func ringPrecedence(operator checker.Operator) int {
	if operator == checker.MultiplyOperator {
		return 2
	}
	return 1
}
