package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// isContextualExpression reports whether a form takes its type from the
// expected type reaching it, for contextual union injection.
func isContextualExpression(expression parser.Expression) bool {
	return contextualExpression(expression, false)
}

// isContextualForInference is the same question asked by RFC 0090's `:=`,
// which must reject an initializer that determines no type of its own.
//
// It is deliberately the same function. Two predicates answering "does this
// form take its type from context" would drift apart silently, and the drift
// is not symmetric: a missing case is *accepted* and quietly defaulted, which
// is how `match` was nearly shipped taking Int32 from nowhere.
//
// The flag exists because the two callers ask over different sets. Union
// injection reaches only the arithmetic-and-numeric-literal forms, and
// widening it would change which contextual unions compile today. Inference
// must additionally reject every form that consumes context.expected — string
// literals, nil, array literals, and match — none of which the union path
// ever offers.
func isContextualForInference(expression parser.Expression) bool {
	return contextualExpression(expression, true)
}

// contextualExpression is the shared recursion. The general rule: a form is
// contextual if it forwards the expected type to sub-expressions and all of
// those are contextual. Any construct added later that threads
// context.expected inward belongs here — grep for it.
func contextualExpression(expression parser.Expression, forInference bool) bool {
	switch expression := expression.(type) {
	case parser.IntegerLiteral, parser.DecimalLiteral, parser.NegatedNumericLiteral:
		return true
	case parser.UnaryExpression:
		return expression.Operator.Lexeme == "-" || contextualExpression(expression.Operand, forInference)
	case parser.BinaryExpression:
		return isArithmeticToken(expression.Operator.Lexeme) &&
			contextualExpression(expression.Left, forInference) && contextualExpression(expression.Right, forInference)
	case parser.StringLiteral:
		// A string literal is valid as String and as Strand.
		return forInference
	case parser.NilLiteral:
		// nil needs an expected union containing Nil.
		return forInference
	case parser.ArrayLiteralExpression:
		// An array literal needs an element type and a length.
		return forInference
	case parser.MatchExpression:
		// A match forwards the expected type into every arm, so it is
		// contextual exactly when every arm's expression is. The scrutinee
		// has its own type and never receives the expected one.
		if !forInference {
			return false
		}
		for _, arm := range expression.Arms {
			if !contextualExpression(arm.Expression, forInference) {
				return false
			}
		}
		return len(expression.Arms) > 0
	default:
		return false
	}
}

func isArithmeticToken(lexeme string) bool {
	switch lexeme {
	case "+", "-", "*", "/", "%":
		return true
	default:
		return false
	}
}

func checkContextualUnion(expression parser.Expression, expected compilerTypes.TypeUse, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	originalFlow := names.flow
	contextFree := inferExpressionType(expression, compilerTypes.Type{}, names, typeEnvironment)
	if contextFree.diagnostic != nil {
		return checkedExpression{token: contextFree.token, diagnostic: contextFree.diagnostic}
	}
	for _, candidate := range expected.Candidates {
		candidateFlow := originalFlow
		if originalFlow != nil {
			candidateFlow = originalFlow.clone()
			names.flow = candidateFlow
		}
		checked := checkExpression(expression, expressionContext{expected: candidate, foldConstants: true}, names, typeEnvironment)
		if len(initializerDiagnostics(checked)) == 0 {
			return injectIntoUnion(checked, expected.Type)
		}
		names.flow = originalFlow
	}
	names.flow = originalFlow
	token := contextFree.token
	if token.Line == 0 {
		token = expressionToken(expression)
	}
	return checkedExpression{
		token: token,
		diagnostic: func() *compilerTypes.Diagnostic {
			diagnostic := typeErrorAt(token, fmt.Sprintf("no member of %s accepts this expression", expected.Type.Name))
			return &diagnostic
		}(),
	}
}

func injectIntoUnion(source checkedExpression, destination compilerTypes.Type) checkedExpression {
	if !compilerTypes.IsUnion(destination) || compilerTypes.Equal(source.typ, destination) {
		return source
	}
	node := expressionNode(source.source)
	if compilerTypes.IsUnion(source.typ) {
		mapping := make([]int, 0, len(compilerTypes.UnionMembers(source.typ)))
		for _, sourceMember := range compilerTypes.UnionMembers(source.typ) {
			mapping = append(mapping, unionDestinationIndex(destination, sourceMember))
		}
		checkedNode := Expression{
			Kind:        UnionWidenExpression,
			Operand:     &node,
			OperandType: source.typ,
			ResultType:  destination,
			MemberMap:   mapping,
		}
		result := Operand{Kind: ExpressionOperand, Type: destination, Node: checkedNode}
		return checkedExpression{source: result, typ: destination, token: source.token}
	}
	checkedNode := Expression{
		Kind:        UnionInjectionExpression,
		Operand:     &node,
		OperandType: source.typ,
		ResultType:  destination,
		MemberIndex: unionDestinationIndex(destination, source.typ),
	}
	result := Operand{Kind: ExpressionOperand, Type: destination, Node: checkedNode}
	return checkedExpression{source: result, typ: destination, token: source.token}
}

func unionDestinationIndex(destination, source compilerTypes.Type) int {
	for index, member := range compilerTypes.UnionMembers(destination) {
		if compilerTypes.Equal(member, source) {
			return index
		}
	}
	for index, member := range compilerTypes.UnionMembers(destination) {
		if compilerTypes.Assignable(member, source) {
			return index
		}
	}
	return -1
}

func checkUnionTypeTest(expression parser.TypeTestExpression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	operand := checkExpression(expression.Operand, expressionContext{}, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(operand); len(diagnostics) > 0 {
		return checkedExpression{token: expression.IsToken, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	queryUse, diagnostic := resolveUnionMemberUse(expression.Type, expression.IsToken, typeEnvironment, names.generics)
	if diagnostic != nil {
		return checkedExpression{token: expression.IsToken, diagnostic: diagnostic}
	}
	query := queryUse.Type
	if compilerTypes.IsUnion(query) {
		diagnostic := typeErrorAt(expression.IsToken, fmt.Sprintf("is requires one exact member type; %s is a union", query.Name))
		return checkedExpression{token: expression.IsToken, diagnostic: &diagnostic}
	}
	if compilerTypes.IsNil(query) {
		diagnostic := typeErrorAt(expression.IsToken, "is may not test Nil; use == nil or != nil")
		return checkedExpression{token: expression.IsToken, diagnostic: &diagnostic}
	}
	if !compilerTypes.IsUnion(operand.typ) || len(compilerTypes.UnionMembers(operand.typ)) < 2 {
		diagnostic := typeErrorAt(expression.IsToken, fmt.Sprintf("is requires a union value; got %s", operand.typ.Name))
		return checkedExpression{token: expression.IsToken, diagnostic: &diagnostic}
	}
	if variable, ok := expression.Operand.(parser.VariableExpression); ok && operand.source.Binding != 0 {
		if fact, escaped := names.flow.facts[operand.source.Binding]; escaped && fact.escaped {
			diagnostic := typeErrorAt(expression.IsToken, fmt.Sprintf("%s cannot be narrowed after its mutable address escapes", variable.Name.Lexeme))
			return checkedExpression{token: expression.IsToken, diagnostic: &diagnostic}
		}
	}
	memberIndex := -1
	for index, member := range compilerTypes.UnionMembers(operand.typ) {
		if compilerTypes.Equal(member, query) {
			memberIndex = index
			break
		}
	}
	if memberIndex < 0 {
		diagnostic := typeErrorAt(expression.IsToken, fmt.Sprintf("%s is not a member of %s", query.Name, operand.typ.Name))
		return checkedExpression{token: expression.IsToken, diagnostic: &diagnostic}
	}
	members := compilerTypes.UnionMembers(operand.typ)
	if len(members) == 2 && compilerTypes.ContainsUnionMember(operand.typ, compilerTypes.Nil) {
		diagnostic := typeErrorAt(expression.IsToken, fmt.Sprintf("is test of %s against %s is redundant; use != nil", query.Name, operand.typ.Name))
		return checkedExpression{token: expression.IsToken, diagnostic: &diagnostic}
	}
	operandNode := expressionNode(operand.source)
	node := Expression{
		Kind:        UnionTestExpression,
		Operand:     &operandNode,
		OperandType: operand.typ,
		ResultType:  compilerTypes.Bool,
		TestType:    query,
		MemberIndex: memberIndex,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
	return checkedExpression{source: source, typ: compilerTypes.Bool, token: expression.IsToken}
}

func checkUnionEquality(operator Operator, left, right checkedExpression, token lexer.Token) *checkedExpression {
	if !compilerTypes.IsUnion(left.typ) && !compilerTypes.IsUnion(right.typ) {
		return nil
	}
	if !compilerTypes.IsUnion(left.typ) || !compilerTypes.IsUnion(right.typ) || !compilerTypes.Equal(left.typ, right.typ) {
		diagnostic := typeErrorAt(token, fmt.Sprintf("union equality requires identical operand types; got %s and %s", left.typ.Name, right.typ.Name))
		return &checkedExpression{token: token, diagnostic: &diagnostic}
	}
	for _, member := range compilerTypes.UnionMembers(left.typ) {
		if !compilerTypes.IsNil(member) {
			if ok, _ := equalityAvailable(member); !ok {
				diagnostic := typeErrorAt(token, fmt.Sprintf("union member %s does not support equality", member.Name))
				return &checkedExpression{token: token, diagnostic: &diagnostic}
			}
		}
	}
	leftNode := expressionNode(left.source)
	rightNode := expressionNode(right.source)
	node := Expression{
		Kind:        UnionEqualityExpression,
		Left:        &leftNode,
		Right:       &rightNode,
		Operator:    operator,
		OperandType: left.typ,
		ResultType:  compilerTypes.Bool,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
	return &checkedExpression{source: source, typ: compilerTypes.Bool, token: token}
}

func expressionToken(expression parser.Expression) (token lexer.Token) {
	switch expression := expression.(type) {
	case parser.IntegerLiteral:
		return expression.Token
	case parser.DecimalLiteral:
		return expression.Token
	case parser.NegatedNumericLiteral:
		return expression.Minus
	case parser.BooleanLiteral:
		return expression.Token
	case parser.NilLiteral:
		return expression.Token
	case parser.VariableExpression:
		return expression.Name
	case parser.BinaryExpression:
		return expression.Operator
	case parser.UnaryExpression:
		return expression.Operator
	default:
		return lexer.Token{}
	}
}
