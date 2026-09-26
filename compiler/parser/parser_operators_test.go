package parser

// Expression operators: unary and logical forms, associativity,
// precedence and grouping, every binary operator, and address-of place
// rules.

import (
	"strings"
	"testing"

	"hexal/compiler/lexer"
)

func TestParseGeneralUnaryMinus(t *testing.T) {
	initializer := parseInitializer(t, "let x: Int32 = -value")
	unary, ok := initializer.(UnaryExpression)
	if !ok || unary.Operator.Kind != lexer.Minus {
		t.Fatalf("initializer = %#v, want unary minus", initializer)
	}
	operand, ok := unary.Operand.(VariableExpression)
	if !ok || operand.Name.Lexeme != "value" {
		t.Fatalf("operand = %#v, want variable value", unary.Operand)
	}
}

func TestParseLogicalNot(t *testing.T) {
	initializer := parseInitializer(t, "let flag: Bool = !ready")
	unary, ok := initializer.(UnaryExpression)
	if !ok || unary.Operator.Kind != lexer.Bang {
		t.Fatalf("initializer = %#v, want logical not", initializer)
	}
	operand, ok := unary.Operand.(VariableExpression)
	if !ok || operand.Name.Lexeme != "ready" {
		t.Fatalf("operand = %#v, want variable ready", unary.Operand)
	}
}

func TestParseUnaryOperatorsAssociateRight(t *testing.T) {
	initializer := parseInitializer(t, "let x: Int32 = - -value")
	outer, ok := initializer.(UnaryExpression)
	if !ok || outer.Operator.Kind != lexer.Minus {
		t.Fatalf("initializer = %#v, want outer unary minus", initializer)
	}
	inner, ok := outer.Operand.(UnaryExpression)
	if !ok || inner.Operator.Kind != lexer.Minus {
		t.Fatalf("outer operand = %#v, want inner unary minus", outer.Operand)
	}
	if operand, ok := inner.Operand.(VariableExpression); !ok || operand.Name.Lexeme != "value" {
		t.Fatalf("inner operand = %#v, want variable value", inner.Operand)
	}
}

func TestParseDirectMinusLiteralRetainsLiteralNode(t *testing.T) {
	for _, source := range []string{"let x: Int8 = -128", "let x: Float32 = -1.5"} {
		initializer := parseInitializer(t, source)
		negative, ok := initializer.(NegatedNumericLiteral)
		if !ok {
			t.Fatalf("initializer for %q = %#v, want negated numeric literal", source, initializer)
		}
		switch literal := negative.Literal.(type) {
		case IntegerLiteral:
			if literal.Token.Lexeme != "128" {
				t.Fatalf("integer literal for %q = %q, want 128", source, literal.Token.Lexeme)
			}
		case DecimalLiteral:
			if literal.Token.Lexeme != "1.5" {
				t.Fatalf("decimal literal for %q = %q, want 1.5", source, literal.Token.Lexeme)
			}
		default:
			t.Fatalf("literal for %q = %#v, want numeric literal", source, negative.Literal)
		}
	}
}

func TestParseBinaryPrecedenceAndGrouping(t *testing.T) {
	message := parseError(t, "let x: Int32 = 2 + 3 * 4")
	if !strings.Contains(message, "mixed binary operators require parentheses; found '*' after '+'") {
		t.Fatalf("message = %q, want the mixed-operator diagnostic", message)
	}

	rightGrouped := parseInitializer(t, "let x: Int32 = 2 + (3 * 4)").(BinaryExpression)
	if rightGrouped.Operator.Kind != lexer.Plus {
		t.Fatalf("root operator = %v, want +", rightGrouped.Operator.Kind)
	}
	right, ok := rightGrouped.Right.(BinaryExpression)
	if !ok || right.Operator.Kind != lexer.Star {
		t.Fatalf("right operand = %#v, want multiplication", rightGrouped.Right)
	}

	leftGrouped := parseInitializer(t, "let x: Int32 = (2 + 3) * 4").(BinaryExpression)
	if leftGrouped.Operator.Kind != lexer.Star {
		t.Fatalf("grouped root operator = %v, want *", leftGrouped.Operator.Kind)
	}
	left, ok := leftGrouped.Left.(BinaryExpression)
	if !ok || left.Operator.Kind != lexer.Plus {
		t.Fatalf("grouped left operand = %#v, want addition", leftGrouped.Left)
	}
}

func TestParseBinaryOperatorsAssociateLeft(t *testing.T) {
	initializer := parseInitializer(t, "let x: Int32 = a - b - c")
	outer, ok := initializer.(BinaryExpression)
	if !ok || outer.Operator.Kind != lexer.Minus {
		t.Fatalf("initializer = %#v, want outer subtraction", initializer)
	}
	inner, ok := outer.Left.(BinaryExpression)
	if !ok || inner.Operator.Kind != lexer.Minus {
		t.Fatalf("left operand = %#v, want inner subtraction", outer.Left)
	}
}

func TestParseAllExpressionPrecedenceLevels(t *testing.T) {
	message := parseError(t, "let result: Bool = a + 1 > b and !done or ready == loaded")
	if !strings.Contains(message, "mixed binary operators require parentheses; found '>' after '+'") {
		t.Fatalf("message = %q, want the mixed-operator diagnostic", message)
	}

	initializer := parseInitializer(t, "let result: Bool = (((a + 1) > b) and !done) or (ready == loaded)")
	orExpression, ok := initializer.(BinaryExpression)
	if !ok || orExpression.Operator.Kind != lexer.Or {
		t.Fatalf("initializer = %#v, want or expression", initializer)
	}

	andExpression, ok := orExpression.Left.(BinaryExpression)
	if !ok || andExpression.Operator.Kind != lexer.And {
		t.Fatalf("or left = %#v, want and expression", orExpression.Left)
	}
	relational, ok := andExpression.Left.(BinaryExpression)
	if !ok || relational.Operator.Kind != lexer.Greater {
		t.Fatalf("and left = %#v, want relational expression", andExpression.Left)
	}
	additive, ok := relational.Left.(BinaryExpression)
	if !ok || additive.Operator.Kind != lexer.Plus {
		t.Fatalf("relational left = %#v, want additive expression", relational.Left)
	}
	logicalNot, ok := andExpression.Right.(UnaryExpression)
	if !ok || logicalNot.Operator.Kind != lexer.Bang {
		t.Fatalf("and right = %#v, want logical not", andExpression.Right)
	}
	equality, ok := orExpression.Right.(BinaryExpression)
	if !ok || equality.Operator.Kind != lexer.EqualEqual {
		t.Fatalf("or right = %#v, want equality expression", orExpression.Right)
	}
}

func TestParseEveryBinaryOperator(t *testing.T) {
	for _, testCase := range []struct {
		source   string
		operator lexer.TokenKind
	}{
		{"let x: Int32 = a * b", lexer.Star},
		{"let x: Int32 = a / b", lexer.Slash},
		{"let x: Int32 = a % b", lexer.Percent},
		{"let x: Int32 = a + b", lexer.Plus},
		{"let x: Int32 = a - b", lexer.Minus},
		{"let x: Bool = a < b", lexer.Less},
		{"let x: Bool = a <= b", lexer.LessEqual},
		{"let x: Bool = a > b", lexer.Greater},
		{"let x: Bool = a >= b", lexer.GreaterEqual},
		{"let x: Bool = a == b", lexer.EqualEqual},
		{"let x: Bool = a != b", lexer.BangEqual},
		{"let x: Bool = a and b", lexer.And},
		{"let x: Bool = a or b", lexer.Or},
	} {
		binary, ok := parseInitializer(t, testCase.source).(BinaryExpression)
		if !ok || binary.Operator.Kind != testCase.operator {
			t.Errorf("initializer for %q = %#v, want %v", testCase.source, binary, testCase.operator)
		}
	}
}

func TestParseRejectsMalformedOperatorsAndGrouping(t *testing.T) {
	for _, source := range []string{
		"let x: Int32 = 1 +",
		"let x: Bool = true and",
		"let x: Bool = !",
		"let x: Int32 = (1 + 2",
		"let x: Int32 = 1 * / 2",
	} {
		tokens, err := lexer.Lex("test.hex", source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err == nil {
			t.Errorf("Parse(%q) accepted malformed expression", source)
		}
	}
}

func TestParseReservesLogicalKeywords(t *testing.T) {
	for _, source := range []string{"let and: Bool = true", "let or: Bool = false"} {
		tokens, err := lexer.Lex("test.hex", source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		if _, err := Parse(tokens); err == nil {
			t.Errorf("Parse(%q) accepted reserved logical keyword as a name", source)
		}
	}
}

func TestParseAddressRemainsPlaceOnly(t *testing.T) {
	for _, source := range []string{"let p: Ptr<Int32> = @42", "let p: Ptr<Int32> = @nil"} {
		tokens, err := lexer.Lex("test.hex", source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", source, err)
		}
		_, err = Parse(tokens)
		if err == nil || err.Error() != "[Syntax Error syntax.expected-token] expected a place identifier at 1:22" {
			t.Fatalf("Parse(%q) error = %v, want place-only @diagnostic", source, err)
		}
	}
}

func TestParseAddressRejectsCalls(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"let p: Ptr<Int32> = @f()", "[Syntax Error syntax.call-same-line] a call's ( must follow its callee on the same line at 1:23"},
		{"let p: Ptr<Int32> = @value.compute()", "[Syntax Error syntax.call-same-line] a call's ( must follow its callee on the same line at 1:35"},
	} {
		tokens, err := lexer.Lex("test.hex", testCase.source)
		if err != nil {
			t.Fatalf("Lex(%q) returned an error: %v", testCase.source, err)
		}
		_, err = Parse(tokens)
		if err == nil || err.Error() != testCase.want {
			t.Errorf("Parse(%q) error = %v, want %q", testCase.source, err, testCase.want)
		}
	}
}
