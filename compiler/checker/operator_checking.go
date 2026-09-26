package checker

import (
	"go/constant"
	gotoken "go/token"
	"math"

	diagnosticsPkg "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

func checkUnaryExpression(expression parser.UnaryExpression, context expressionContext, ctx checkContext) checkedExpression {
	operator, ok := operatorFromToken(expression.Operator)
	if expression.Operator.Kind == lexer.Minus {
		operator = NegateOperator
		ok = true
	}
	if !ok {
		return unsupportedOperatorExpression(expression.Operator)
	}

	hint := inferExpressionType(expression.Operand, operandContextType(operator, context.expected.Type), ctx)
	if hint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: hint.diagnostic}
	}
	operandType := hint.typ
	if hint.contextual {
		if expected := operandContextType(operator, context.expected.Type); expected.Name != "" {
			operandType = expected
		}
	}
	operand := checkExpression(expression.Operand, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: context.foldConstants}, ctx)
	if diagnostics := initializerDiagnostics(operand); len(diagnostics) > 0 {
		return checkedExpression{token: expression.Operator, diagnostics: diagnostics}
	}
	if ctx.names.generics != nil && ctx.names.generics.open && compilerTypes.ContainsTypeParameter(operand.typ) {
		// An operation whose validity depends on a substituted type
		// is deferred during open generic checking and rechecked at
		// specialization with concrete types.
		return operationUnaryResult(operator, operand, operand.typ, operand.typ, expression.Operator)
	}
	if !operatorAllowsType(operator, operand.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: unaryOperatorDiagnostic(operator, operand.typ, expression.Operator),
		}
	}

	resultType := operand.typ
	if operator == LogicalNotOperator {
		resultType = compilerTypes.Bool
	}
	return foldUnary(operator, operand, operand.typ, resultType, expression.Operator, context.foldConstants)
}

func checkBinaryExpression(expression parser.BinaryExpression, context expressionContext, ctx checkContext) checkedExpression {
	operator, ok := operatorFromToken(expression.Operator)
	if !ok || operator == NegateOperator || operator == LogicalNotOperator {
		return unsupportedOperatorExpression(expression.Operator)
	}

	expected := operandContextType(operator, context.expected.Type)
	leftHint := inferExpressionType(expression.Left, expected, ctx)
	rightHint := inferExpressionType(expression.Right, expected, ctx)
	if leftHint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: leftHint.diagnostic}
	}
	if rightHint.diagnostic != nil {
		return checkedExpression{token: expression.Operator, diagnostic: rightHint.diagnostic}
	}
	operandType := binaryOperandType(operator, expected, leftHint, rightHint)
	left := checkExpression(expression.Left, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: context.foldConstants}, ctx)
	rightEvaluation := context.foldConstants
	if rightEvaluation && (operator == LogicalAndOperator || operator == LogicalOrOperator) {
		if leftValue, known := knownTruthinessMetadata(left); known {
			rightEvaluation = (operator == LogicalAndOperator && leftValue) || (operator == LogicalOrOperator && !leftValue)
		}
	}
	right := checkExpression(expression.Right, expressionContext{expected: compilerTypes.NewTypeUse(operandType), foldConstants: rightEvaluation}, ctx)
	diagnostics := append(initializerDiagnostics(left), initializerDiagnostics(right)...)
	if len(diagnostics) > 0 {
		return checkedExpression{token: expression.Operator, diagnostics: diagnostics}
	}

	// Time values admit only their defined comparisons and arithmetic.
	if compilerTypes.IsTime(left.typ) || compilerTypes.IsTime(right.typ) {
		return checkTimeBinary(operator, left, right, expression.Operator)
	}

	// Null tests own the == and != pairs that mention Nil: a null
	// test yields Bool, while pairs without a Nil side stay with ordinary
	// scalar equality below.
	if operator == EqualOperator || operator == NotEqualOperator {
		// EoS is a singleton, so eos == eos is provably true and
		// eos != eos is provably false, matching nil == nil.
		if compilerTypes.IsEoS(left.typ) && compilerTypes.IsEoS(right.typ) {
			result := foldedBoolResult(operator == EqualOperator, expression.Operator)
			return result
		}
		if result := checkNullTest(operator, left, right, expression.Operator); result != nil {
			return *result
		}
		if result := checkUnionEquality(operator, left, right, expression.Operator); result != nil {
			return *result
		}
	}

	if ctx.names.generics != nil && ctx.names.generics.open &&
		(compilerTypes.ContainsTypeParameter(left.typ) || compilerTypes.ContainsTypeParameter(right.typ)) {
		// An operation whose validity depends on a substituted type
		// is deferred during open generic checking and rechecked at
		// specialization with concrete types.
		resultType := left.typ
		if operator == EqualOperator || operator == NotEqualOperator ||
			operator == LessOperator || operator == LessEqualOperator ||
			operator == GreaterOperator || operator == GreaterEqualOperator ||
			operator == LogicalAndOperator || operator == LogicalOrOperator {
			resultType = compilerTypes.Bool
		}
		return operationBinaryResult(operator, left, right, left.typ, resultType, expression.Operator)
	}

	// Mixed numeric arithmetic selects the unique least
	// lossless common type before the operation; the result has that type
	// and wraps at it.
	if (operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator ||
		operator == DivideOperator || operator == RemainderOperator) &&
		(compilerTypes.IsInteger(left.typ) || compilerTypes.IsFloat(left.typ)) &&
		(compilerTypes.IsInteger(right.typ) || compilerTypes.IsFloat(right.typ)) &&
		!compilerTypes.Equal(left.typ, right.typ) {
		common, ok := compilerTypes.LosslessCommonType(left.typ, right.typ)
		if !ok {
			diagnostic := messageAt(expression.Operator, diagnosticsPkg.NumericOperandsNoCommonType())
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		// Remainder is integer-only; a mixed Int/Float pair whose common type
		// widens to Float is rejected here rather than folded or rendered,
		// since go/constant has no float remainder operation.
		if operator == RemainderOperator && !compilerTypes.IsInteger(common) {
			return checkedExpression{token: expression.Operator, diagnostic: binaryOperatorDiagnostic(RemainderOperator, common, expression.Operator)}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			return foldWidenedArithmetic(operator, left, right, common, expression.Operator)
		}
		leftNode := widenNode(expressionNode(left.source), left.typ, common)
		rightNode := widenNode(expressionNode(right.source), right.typ, common)
		if operator == DivideOperator || operator == RemainderOperator {
			if diagnostic := staticDivisionDiagnostic(operator, left, right, common, expression.Operator); diagnostic != nil {
				return checkedExpression{typ: common, token: expression.Operator, diagnostic: diagnostic}
			}
		}
		node := operationBinaryNode(operator, leftNode, rightNode, common, common)
		source := Operand{Kind: ExpressionOperand, Type: common, Node: node}
		return checkedExpression{source: source, typ: common, token: expression.Operator}
	}

	// Bitwise &, ^, and | use the unique least lossless common
	// integer type; the operation happens at that exact width and wraps at
	// it.
	if isBitwiseArithmetic(operator) {
		if !isBitwiseEligible(left.typ) || !isBitwiseEligible(right.typ) {
			diagnostic := messageAt(expression.Operator, diagnosticsPkg.OperatorRequiresIntegerOperands(operator.String(), left.typ.Name, right.typ.Name))
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		common := left.typ
		if !compilerTypes.Equal(left.typ, right.typ) {
			var ok bool
			common, ok = compilerTypes.LosslessCommonType(left.typ, right.typ)
			if !ok {
				diagnostic := messageAt(expression.Operator, diagnosticsPkg.IntegerOperandsNoCommonType())
				return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
			}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			return foldWidenedArithmetic(operator, left, right, common, expression.Operator)
		}
		leftNode := widenNode(expressionNode(left.source), left.typ, common)
		rightNode := widenNode(expressionNode(right.source), right.typ, common)
		node := operationBinaryNode(operator, leftNode, rightNode, common, common)
		source := Operand{Kind: ExpressionOperand, Type: common, Node: node}
		return checkedExpression{source: source, typ: common, token: expression.Operator}
	}

	// Shifts preserve the left operand's type; the count is any
	// integer and never participates in common-type selection.
	if isShiftOperator(operator) {
		if !isBitwiseEligible(left.typ) {
			diagnostic := messageAt(expression.Operator, diagnosticsPkg.OperatorRequiresIntegerLeft(operator.String(), left.typ.Name))
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		if !compilerTypes.IsInteger(right.typ) {
			diagnostic := messageAt(expression.Operator, diagnosticsPkg.ShiftCountRequiresInteger(right.typ.Name))
			return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
		}
		if count := staticConstantValue(right); count != nil && count.Kind() == constant.Int {
			value, exact := constant.Int64Val(count)
			if exact && (value < 0 || value >= int64(left.typ.Bits)) {
				diagnostic := messageAt(expression.Operator, diagnosticsPkg.ShiftCountOutOfRange(value, left.typ.Name))
				return checkedExpression{token: expression.Operator, diagnostic: &diagnostic}
			}
		}
		if context.foldConstants && left.source.Kind == ConstantOperand && right.source.Kind == ConstantOperand {
			operation, ok := integerConstantOperator(operator)
			if ok && left.source.Constant != nil && right.source.Constant != nil {
				// go/constant shifts require a uint count; the range
				// validation above guarantees the value fits.
				countValue, _ := constant.Uint64Val(right.source.Constant)
				value := constant.Shift(left.source.Constant, operation, uint(countValue))
				value = wrapIntegerConstant(value, left.typ)
				return foldedIntegerResult(value, left.typ, expression.Operator)
			}
		}
		node := operationBinaryNode(operator, expressionNode(left.source), expressionNode(right.source), left.typ, left.typ)
		source := Operand{Kind: ExpressionOperand, Type: left.typ, Node: node}
		return checkedExpression{source: source, typ: left.typ, token: expression.Operator}
	}

	// Equality and ordering resolve through the lossless numeric
	// widening and the recursive deep-comparison rules before the ordinary
	// identical-scalar path.
	if operator == EqualOperator || operator == NotEqualOperator ||
		operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator {
		if result := checkDeepComparison(operator, left, right, expression.Operator, ctx.names); result != nil {
			return *result
		}
	}

	if !operatorAllowsType(operator, left.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: binaryOperatorDiagnostic(operator, left.typ, expression.Operator),
		}
	}
	if !operatorAllowsType(operator, right.typ) {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: binaryOperatorDiagnostic(operator, right.typ, expression.Operator),
		}
	}
	if !compilerTypes.Equal(left.typ, right.typ) && operator != LogicalAndOperator && operator != LogicalOrOperator {
		return checkedExpression{
			token:      expression.Operator,
			diagnostic: diagnosticAt(messageAt(expression.Operator, diagnosticsPkg.OperatorRequiresIdenticalTypes(operator.String(), left.typ.Name, right.typ.Name))),
		}
	}
	resultType := left.typ
	if operator == EqualOperator || operator == NotEqualOperator ||
		operator == LessOperator || operator == LessEqualOperator ||
		operator == GreaterOperator || operator == GreaterEqualOperator ||
		operator == LogicalAndOperator || operator == LogicalOrOperator {
		resultType = compilerTypes.Bool
	}
	return foldBinary(operator, left, right, left.typ, resultType, expression.Operator, context.foldConstants)
}

// checkNullTest resolves null tests: == and != accept exactly the
// operand pairs where one side is Nil and the other is any union containing
// Nil. The result is Bool, and the checked node is normalized so the union
// operand always sits in the node's Operand slot. Pairs without a Nil side
// return nil so ordinary equality keeps its own rules.
func checkNullTest(operator Operator, left, right checkedExpression, token lexer.Token) *checkedExpression {
	if !compilerTypes.IsNil(left.typ) && !compilerTypes.IsNil(right.typ) {
		return nil
	}
	var operand checkedExpression
	switch {
	case compilerTypes.IsNil(left.typ) && compilerTypes.IsNil(right.typ):
		// Nil is a singleton, so nil == nil is provably true and nil != nil
		// is provably false. This is the only folded null-test case; a test
		// against a nullable operand always stays runtime.
		result := foldedBoolResult(operator == EqualOperator, token)
		return &result
	case compilerTypes.IsEoS(left.typ) && compilerTypes.IsEoS(right.typ):
		// EoS is a singleton, so eos == eos is provably true and
		// eos != eos is provably false, matching nil == nil.
		result := foldedBoolResult(operator == EqualOperator, token)
		return &result
	case compilerTypes.IsNil(left.typ):
		operand = right
	default:
		operand = left
	}
	if !compilerTypes.IsUnion(operand.typ) || !compilerTypes.ContainsUnionMember(operand.typ, compilerTypes.Nil) {
		// The operand's type can never hold Nil, which makes the test a
		// constant result; the diagnostic names that reason instead of
		// rejecting the test outright.
		verdict := "true"
		if operator == EqualOperator {
			verdict = "false"
		}
		diagnostic := messageAt(token, diagnosticsPkg.TypeIsNeverNil(operand.typ.Name, verdict))
		return &checkedExpression{token: token, diagnostic: &diagnostic}
	}
	operandNode := expressionNode(operand.source)
	node := Expression{
		Kind:        NullTestExpression,
		Operand:     &operandNode,
		Operator:    operator,
		OperandType: operand.typ,
		ResultType:  compilerTypes.Bool,
	}
	source := Operand{Kind: ExpressionOperand, Type: compilerTypes.Bool, Node: node}
	return &checkedExpression{source: source, typ: compilerTypes.Bool, token: token}
}

// knownTruthinessMetadata extends knownTruthiness to the known-value
// metadata of a named immutable binding read. The read stays in the checked
// program; only compile-time short-circuit reasoning (whether the other
// side is reachable) consults the metadata.
func knownTruthinessMetadata(expression checkedExpression) (bool, bool) {
	if expression.known == nil {
		return false, false
	}
	switch compilerTypes.Truthiness(expression.typ) {
	case compilerTypes.TruthinessBool:
		if expression.known.Constant == nil || expression.known.Constant.Kind() != constant.Bool {
			return false, false
		}
		return constant.BoolVal(expression.known.Constant), true
	case compilerTypes.TruthinessAlwaysTrue:
		return true, true
	case compilerTypes.TruthinessNil:
		return false, true
	}
	return false, false
}

// knownTruthiness reports whether the operand's truthiness is decided at
// compile time: a constant Bool carries its value, nil is falsey,
// and a constant of an always-truthy type is truthy. Non-constant operands
// are never folded here: their evaluation must survive in the checked AST.
func knownTruthiness(expression checkedExpression) (bool, bool) {
	if expression.source.Kind != ConstantOperand {
		return false, false
	}
	switch compilerTypes.Truthiness(expression.typ) {
	case compilerTypes.TruthinessBool:
		if expression.source.Constant == nil || expression.source.Constant.Kind() != constant.Bool {
			return false, false
		}
		return constant.BoolVal(expression.source.Constant), true
	case compilerTypes.TruthinessAlwaysTrue:
		return true, true
	case compilerTypes.TruthinessNil:
		return false, true
	}
	return false, false
}

func isIntegerArithmetic(operator Operator) bool {
	return operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator || operator == DivideOperator || operator == RemainderOperator
}

func isBitwiseArithmetic(operator Operator) bool {
	return operator == BitwiseAndOperator || operator == BitwiseXorOperator || operator == BitwiseOrOperator
}

func isShiftOperator(operator Operator) bool {
	return operator == ShiftLeftOperator || operator == ShiftRightOperator
}

func isFloatArithmetic(operator Operator) bool {
	return operator == AddOperator || operator == SubtractOperator || operator == MultiplyOperator || operator == DivideOperator
}

func integerConstantOperator(operator Operator) (gotoken.Token, bool) {
	switch operator {
	case AddOperator:
		return gotoken.ADD, true
	case SubtractOperator:
		return gotoken.SUB, true
	case MultiplyOperator:
		return gotoken.MUL, true
	case DivideOperator:
		return gotoken.QUO_ASSIGN, true
	case RemainderOperator:
		return gotoken.REM, true
	case BitwiseAndOperator:
		return gotoken.AND, true
	case BitwiseXorOperator:
		return gotoken.XOR, true
	case BitwiseOrOperator:
		return gotoken.OR, true
	case ShiftLeftOperator:
		return gotoken.SHL, true
	case ShiftRightOperator:
		return gotoken.SHR, true
	default:
		return gotoken.ILLEGAL, false
	}
}

func compareConstantOperands(operator Operator, left, right Operand, typ compilerTypes.Type) (bool, bool) {
	if compilerTypes.Equal(typ, compilerTypes.Bool) {
		if left.Constant == nil || right.Constant == nil || left.Constant.Kind() != constant.Bool || right.Constant.Kind() != constant.Bool {
			return false, false
		}
		// Ordered tokens panic on Boolean operands, so equality is the only
		// comparison class that reaches constant.Compare here.
		switch operator {
		case EqualOperator:
			return constant.Compare(left.Constant, gotoken.EQL, right.Constant), true
		case NotEqualOperator:
			return constant.Compare(left.Constant, gotoken.NEQ, right.Constant), true
		default:
			return false, false
		}
	}
	if compilerTypes.IsFloat(typ) {
		return compareFloatOperands(operator, left.FloatBits, right.FloatBits, typ), true
	}
	if !compilerTypes.IsInteger(typ) || left.Constant == nil || right.Constant == nil {
		return false, false
	}
	comparison := func(token gotoken.Token) bool {
		return constant.Compare(left.Constant, token, right.Constant)
	}
	switch operator {
	case EqualOperator:
		return comparison(gotoken.EQL), true
	case NotEqualOperator:
		return comparison(gotoken.NEQ), true
	case LessOperator:
		return comparison(gotoken.LSS), true
	case LessEqualOperator:
		return comparison(gotoken.LEQ), true
	case GreaterOperator:
		return comparison(gotoken.GTR), true
	case GreaterEqualOperator:
		return comparison(gotoken.GEQ), true
	default:
		return false, false
	}
}

func compareFloatOperands(operator Operator, leftBits, rightBits uint64, typ compilerTypes.Type) bool {
	if compilerTypes.Equal(typ, compilerTypes.Float32) {
		left := math.Float32frombits(uint32(leftBits))
		right := math.Float32frombits(uint32(rightBits))
		switch operator {
		case EqualOperator:
			return left == right
		case NotEqualOperator:
			return left != right
		case LessOperator:
			return left < right
		case LessEqualOperator:
			return left <= right
		case GreaterOperator:
			return left > right
		case GreaterEqualOperator:
			return left >= right
		}
		return false
	}
	left := math.Float64frombits(leftBits)
	right := math.Float64frombits(rightBits)
	switch operator {
	case EqualOperator:
		return left == right
	case NotEqualOperator:
		return left != right
	case LessOperator:
		return left < right
	case LessEqualOperator:
		return left <= right
	case GreaterOperator:
		return left > right
	case GreaterEqualOperator:
		return left >= right
	default:
		return false
	}
}

func staticDivisionDiagnostic(operator Operator, left, right checkedExpression, operandType compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	if (operator != DivideOperator && operator != RemainderOperator) || !compilerTypes.IsInteger(operandType) {
		return nil
	}
	divisor := staticConstantValue(right)
	if divisor == nil || divisor.Kind() == constant.Unknown {
		return nil
	}
	if constant.Sign(divisor) == 0 {
		return diagnosticAt(messageAt(token, diagnosticsPkg.DivisionByZero()))
	}
	// Signed minimum divided by -1 wraps to the signed minimum and the
	// remainder is zero, both at compile time and at runtime.
	return nil
}

func inferExpressionType(expression parser.Expression, expected compilerTypes.Type, ctx checkContext) expressionTypeHint {
	switch expression := expression.(type) {
	case parser.IntegerLiteral:
		return expressionTypeHint{typ: contextualIntegerType(expected), contextual: true, token: expression.Token}
	case parser.DecimalLiteral:
		return expressionTypeHint{typ: contextualFloatType(expected), contextual: true, token: expression.Token}
	case parser.NegatedNumericLiteral:
		return expressionTypeHint{typ: negatedLiteralType(expression, expected), contextual: true, token: expression.Minus}
	case parser.BooleanLiteral:
		return expressionTypeHint{typ: compilerTypes.Bool, token: expression.Token}
	case parser.NilLiteral:
		return expressionTypeHint{typ: compilerTypes.Nil, token: expression.Token}
	case parser.EosLiteral:
		return expressionTypeHint{typ: compilerTypes.EoS, token: expression.Token}
	case parser.StringLiteral:
		// A literal in an expression position is String unless the
		// context demands an inline String<N>.
		typ := compilerTypes.StringType
		if compilerTypes.IsInlineString(expected) {
			typ = expected
		}
		return expressionTypeHint{typ: typ, token: expression.Token}
	case parser.RawStringLiteral:
		typ := compilerTypes.StringType
		if compilerTypes.IsInlineString(expected) {
			typ = expected
		}
		return expressionTypeHint{typ: typ, token: expression.Token}
	case parser.ByteLiteral:
		return expressionTypeHint{typ: compilerTypes.UInt8, token: expression.Token}
	case parser.RuneLiteral:
		return expressionTypeHint{typ: compilerTypes.Rune, token: expression.Token}
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		place := checkPlace(expression, ctx)
		return expressionTypeHint{typ: place.typ, token: place.token, diagnostic: place.diagnostic}
	case parser.ArrayLiteralExpression:
		checked := checkArrayLiteral(expression, expected, ctx)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.MatchExpression:
		checked := checkMatchExpression(expression, expressionContext{expected: compilerTypes.NewTypeUse(expected)}, ctx)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.TypeTestExpression:
		checked := checkUnionTypeTest(expression, ctx)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.AnonymousFunctionLiteral:
		checked := checkAnonymousFunctionLiteral(expression, expressionContext{expected: compilerTypes.NewTypeUse(expected)}, ctx)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.AddressExpression:
		checked := checkAddress(expression, ctx)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.DereferenceExpression:
		checked := checkDereferencePlace(expression, ctx)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.CallExpression:
		checked := checkCallValue(expression, expected, ctx)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.UnaryExpression:
		operator, ok := operatorFromToken(expression.Operator)
		if expression.Operator.Kind == lexer.Minus {
			operator = NegateOperator
			ok = true
		}
		if !ok {
			return expressionTypeHint{token: expression.Operator, diagnostic: unsupportedOperatorDiagnostic(expression.Operator)}
		}
		hint := inferExpressionType(expression.Operand, operandContextType(operator, expected), ctx)
		if hint.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: hint.diagnostic}
		}
		return expressionTypeHint{
			typ:        unaryResultType(operator, hint.typ),
			contextual: hint.contextual,
			token:      expression.Operator,
		}
	case parser.SpawnExpression:
		checked := checkSpawnExpression(expression, ctx)
		return expressionTypeHint{typ: checked.typ, token: checked.token, diagnostic: checked.diagnostic}
	case parser.TryExpression:
		// The try's true result is its checked success type; for literal
		// contextual typing the operand's hint is the closest estimate.
		hint := inferExpressionType(expression.Operand, expected, ctx)
		if hint.diagnostic != nil {
			return expressionTypeHint{token: expression.Keyword, diagnostic: hint.diagnostic}
		}
		return expressionTypeHint{typ: hint.typ, token: expression.Keyword}
	case parser.BinaryExpression:
		operator, ok := operatorFromToken(expression.Operator)
		if !ok || operator == NegateOperator || operator == LogicalNotOperator {
			return expressionTypeHint{token: expression.Operator, diagnostic: unsupportedOperatorDiagnostic(expression.Operator)}
		}
		operandExpected := operandContextType(operator, expected)
		left := inferExpressionType(expression.Left, operandExpected, ctx)
		right := inferExpressionType(expression.Right, operandExpected, ctx)
		if left.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: left.diagnostic}
		}
		if right.diagnostic != nil {
			return expressionTypeHint{token: expression.Operator, diagnostic: right.diagnostic}
		}
		operandType := binaryOperandType(operator, operandExpected, left, right)
		return expressionTypeHint{
			typ:        binaryResultType(operator, operandType),
			contextual: left.contextual && right.contextual,
			token:      expression.Operator,
		}
	default:
		return expressionTypeHint{diagnostic: diagnosticAt(unknownAt(lexer.Token{Line: 1, Column: 1}))}
	}
}

func operatorFromToken(token lexer.Token) (Operator, bool) {
	switch token.Kind {
	case lexer.Minus:
		return SubtractOperator, true
	case lexer.Plus:
		return AddOperator, true
	case lexer.Star:
		return MultiplyOperator, true
	case lexer.Slash:
		return DivideOperator, true
	case lexer.Percent:
		return RemainderOperator, true
	case lexer.EqualEqual:
		return EqualOperator, true
	case lexer.BangEqual:
		return NotEqualOperator, true
	case lexer.Less:
		return LessOperator, true
	case lexer.LessEqual:
		return LessEqualOperator, true
	case lexer.Greater:
		return GreaterOperator, true
	case lexer.GreaterEqual:
		return GreaterEqualOperator, true
	case lexer.And:
		return LogicalAndOperator, true
	case lexer.Or:
		return LogicalOrOperator, true
	case lexer.Bang:
		return LogicalNotOperator, true
	case lexer.Tilde:
		return BitwiseNotOperator, true
	case lexer.Amp:
		return BitwiseAndOperator, true
	case lexer.Caret:
		return BitwiseXorOperator, true
	case lexer.Pipe:
		return BitwiseOrOperator, true
	case lexer.ShiftLeft:
		return ShiftLeftOperator, true
	case lexer.ShiftRight:
		return ShiftRightOperator, true
	default:
		return InvalidOperator, false
	}
}

func operatorAllowsType(operator Operator, typ compilerTypes.Type) bool {
	id, ok := operatorIdentity(operator)
	if !ok {
		return false
	}
	spec, ok := specdata.Operator(id)
	if !ok {
		// A resolved operator must have a row; Validate rejects a registry
		// that cannot answer its own keys, so a miss is a compiler defect.
		panic("checker: operator " + string(id) + " has no specdata row")
	}
	if spec.AnyOperand {
		return true
	}
	operand, ok := compilerTypes.SpecID(typ)
	if !ok {
		return false
	}
	for _, allowed := range spec.Operands {
		if allowed == operand {
			return true
		}
	}
	return false
}

// operatorIdentity maps one resolved checker operator to its registry row. The
// unset operator admits no operand, matching a default rejection.
func operatorIdentity(operator Operator) (specdata.OperatorID, bool) {
	switch operator {
	case NegateOperator:
		return specdata.OperatorNegate, true
	case LogicalNotOperator:
		return specdata.OperatorLogicalNot, true
	case BitwiseNotOperator:
		return specdata.OperatorBitwiseNot, true
	case AddOperator:
		return specdata.OperatorAdd, true
	case SubtractOperator:
		return specdata.OperatorSubtract, true
	case MultiplyOperator:
		return specdata.OperatorMultiply, true
	case DivideOperator:
		return specdata.OperatorDivide, true
	case RemainderOperator:
		return specdata.OperatorRemainder, true
	case BitwiseAndOperator:
		return specdata.OperatorBitwiseAnd, true
	case BitwiseXorOperator:
		return specdata.OperatorBitwiseXor, true
	case BitwiseOrOperator:
		return specdata.OperatorBitwiseOr, true
	case ShiftLeftOperator:
		return specdata.OperatorShiftLeft, true
	case ShiftRightOperator:
		return specdata.OperatorShiftRight, true
	case EqualOperator:
		return specdata.OperatorEqual, true
	case NotEqualOperator:
		return specdata.OperatorNotEqual, true
	case LessOperator:
		return specdata.OperatorLess, true
	case LessEqualOperator:
		return specdata.OperatorLessEqual, true
	case GreaterOperator:
		return specdata.OperatorGreater, true
	case GreaterEqualOperator:
		return specdata.OperatorGreaterEqual, true
	case LogicalAndOperator:
		return specdata.OperatorLogicalAnd, true
	case LogicalOrOperator:
		return specdata.OperatorLogicalOr, true
	default:
		return "", false
	}
}

// isBitwiseEligible reports whether typ may participate in bitwise
// and shift operations: a fixed-width integer or Size.
func isBitwiseEligible(typ compilerTypes.Type) bool {
	return compilerTypes.IsInteger(typ)
}

func operandContextType(operator Operator, expected compilerTypes.Type) compilerTypes.Type {
	if expected.Name == "" {
		return compilerTypes.Type{}
	}
	switch operator {
	case EqualOperator, NotEqualOperator, LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator, LogicalAndOperator, LogicalOrOperator, LogicalNotOperator:
		// A result Bool is not a numeric operand context, and logical
		// operands are truthiness contexts. Nested arithmetic therefore keeps
		// its fallback literal type instead of becoming Bool.
		return compilerTypes.Type{}
	default:
		if operatorAllowsType(operator, expected) {
			return expected
		}
		return compilerTypes.Type{}
	}
}

func binaryOperandType(operator Operator, expected compilerTypes.Type, left, right expressionTypeHint) compilerTypes.Type {
	if left.contextual && !right.contextual && operatorAllowsType(operator, right.typ) {
		return right.typ
	}
	if right.contextual && !left.contextual && operatorAllowsType(operator, left.typ) {
		return left.typ
	}
	if left.contextual && right.contextual && operatorAllowsType(operator, expected) {
		return expected
	}
	return left.typ
}

func unaryResultType(operator Operator, operandType compilerTypes.Type) compilerTypes.Type {
	if operator == LogicalNotOperator {
		return compilerTypes.Bool
	}
	return operandType
}

func binaryResultType(operator Operator, operandType compilerTypes.Type) compilerTypes.Type {
	switch operator {
	case EqualOperator, NotEqualOperator, LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator, LogicalAndOperator, LogicalOrOperator:
		return compilerTypes.Bool
	default:
		return operandType
	}
}

func negatedLiteralType(expression parser.NegatedNumericLiteral, expected compilerTypes.Type) compilerTypes.Type {
	switch expression.Literal.(type) {
	case parser.IntegerLiteral:
		return contextualIntegerType(expected)
	case parser.DecimalLiteral:
		return contextualFloatType(expected)
	default:
		return compilerTypes.Int32
	}
}

func unsupportedOperatorExpression(token lexer.Token) checkedExpression {
	return checkedExpression{token: token, diagnostic: unsupportedOperatorDiagnostic(token)}
}

func unsupportedOperatorDiagnostic(token lexer.Token) *compilerTypes.Diagnostic {
	return diagnosticAt(messageAt(token, diagnosticsPkg.UnsupportedOperator(token.Lexeme)))
}

func unaryOperatorDiagnostic(operator Operator, typ compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	message := diagnosticsPkg.UnaryOperatorRequiresBool(operator.String(), typ.Name)
	if operator == NegateOperator {
		message = diagnosticsPkg.NegationRequiresSignedType(typ.Name)
	}
	if operator == BitwiseNotOperator {
		message = diagnosticsPkg.BitwiseNotRequiresInteger(typ.Name)
	}
	return diagnosticAt(messageAt(token, message))
}

func binaryOperatorDiagnostic(operator Operator, typ compilerTypes.Type, token lexer.Token) *compilerTypes.Diagnostic {
	message := diagnosticsPkg.OperatorRequiresNumericOperands(operator.String(), typ.Name)
	switch operator {
	case RemainderOperator:
		message = diagnosticsPkg.RemainderRequiresInteger(typ.Name)
	case LessOperator, LessEqualOperator, GreaterOperator, GreaterEqualOperator:
		message = diagnosticsPkg.OperatorRequiresOrderedOperands(operator.String(), typ.Name)
	case LogicalAndOperator, LogicalOrOperator:
		message = diagnosticsPkg.LogicalOperatorRequiresBool(operator.String(), typ.Name)
	case EqualOperator, NotEqualOperator:
		message = diagnosticsPkg.EqualityOperatorRequiresScalar(operator.String(), typ.Name)
	}
	return diagnosticAt(messageAt(token, message))
}
