package parser

import (
	"fmt"

	"hexal/compiler/lexer"
)

// pushExpressionRegion starts a fresh binary-operator-kind region for one
// independently parsed expression, returning a restore closure the caller
// must invoke exactly once, on every exit path including an error return, to
// bring back the containing region's own recorded kind.
func (parser *Parser) pushExpressionRegion() func() {
	recorded, kind, token := parser.binaryOperatorRecorded, parser.binaryOperatorKind, parser.binaryOperatorToken
	parser.binaryOperatorRecorded = false
	return func() {
		parser.binaryOperatorRecorded, parser.binaryOperatorKind, parser.binaryOperatorToken = recorded, kind, token
	}
}

// recordBinaryOperator enforces the one-binary-operator-kind rule for the
// current expression region. The first operator recorded in a region sets
// its allowed kind and always succeeds; a later operator of that same kind
// also succeeds; a later operator of a different kind is the exact
// mixed-operator syntax error this rule exists to report, located at the
// later token.
func (parser *Parser) recordBinaryOperator(token lexer.Token) error {
	if !parser.binaryOperatorRecorded {
		parser.binaryOperatorRecorded = true
		parser.binaryOperatorKind = token.Kind
		parser.binaryOperatorToken = token
		return nil
	}
	if token.Kind == parser.binaryOperatorKind {
		return nil
	}
	return parser.errorAt(token, fmt.Sprintf("mixed binary operators require parentheses; found '%s' after '%s'", token.Lexeme, parser.binaryOperatorToken.Lexeme))
}

// expression starts the precedence ladder. mut is only valid before a binding
// or member name in a declaration, never as an expression prefix. expression
// is the ordinary entrypoint for one independently delimited expression
// region; it owns pushing and restoring that region's binary-operator-kind
// state so every caller gets region isolation for free. matchExpression's
// scrutinee and arm parses are the sole exception, since they enter the
// precedence ladder directly at orExpression and so push their own regions.
func (parser *Parser) expression() (Expression, error) {
	exit, err := parser.enterSyntax()
	defer exit()
	if err != nil {
		return nil, err
	}
	restore := parser.pushExpressionRegion()
	defer restore()
	return parser.orExpression()
}

// interpolationTemplate parses the token sequence the lexer produces for an
// interpreted string it found to contain interpolation: InterpStringStart,
// then one InterpText segment followed by an InterpOpen/expression/InterpClose
// triple for each embedded expression, repeated, then a final InterpText and
// InterpStringEnd. The lexer never reaches the parser with a malformed
// sequence (a lexer diagnostic short-circuits parsing entirely), so the only
// genuine syntax error this can report is an empty "{{}}" interpolation; it
// enters the shared syntax-depth budget once for the template itself, and
// each embedded expression enters it again through the ordinary expression()
// call.
func (parser *Parser) interpolationTemplate() (Expression, error) {
	exit, err := parser.enterSyntax()
	defer exit()
	if err != nil {
		return nil, err
	}
	start, err := parser.consume(lexer.InterpStringStart, "an interpolated string")
	if err != nil {
		return nil, err
	}
	segments := make([]InterpolationSegment, 0, 4)
	for {
		text, err := parser.consume(lexer.InterpText, "interpolated string text")
		if err != nil {
			return nil, err
		}
		segments = append(segments, InterpolationSegment{Text: &text})
		if parser.check(lexer.InterpStringEnd) {
			end := parser.advance()
			return InterpolationTemplateExpression{Start: start, Segments: segments, End: end}, nil
		}
		open, err := parser.consume(lexer.InterpOpen, "'{{' after interpolation text")
		if err != nil {
			return nil, err
		}
		if parser.check(lexer.InterpClose) {
			return nil, parser.errorAt(parser.peek(), "string interpolation requires an expression")
		}
		value, err := parser.expression()
		if err != nil {
			return nil, err
		}
		close, err := parser.consume(lexer.InterpClose, "'}}' after the interpolated expression")
		if err != nil {
			return nil, err
		}
		segments = append(segments, InterpolationSegment{Expression: value, Open: open, Close: close})
	}
}

func (parser *Parser) orExpression() (Expression, error) {
	expression, err := parser.andExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.Or) {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.andExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) andExpression() (Expression, error) {
	expression, err := parser.bitwiseOrExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.And) {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.bitwiseOrExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

// bitwiseOrExpression parses the `|` level. Inside a match position, an
// unparenthesized `|` is the next arm separator, not an operator.
func (parser *Parser) bitwiseOrExpression() (Expression, error) {
	expression, err := parser.bitwiseXorExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.Pipe) && parser.matchBoundary == noMatchBoundary {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.bitwiseXorExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) bitwiseXorExpression() (Expression, error) {
	expression, err := parser.bitwiseAndExpression()
	if err != nil {
		return nil, err
	}
	// A `^` separated from its left operand by a newline opens a prefix
	// dereference statement, not an XOR continuation: the grammar has no
	// statement terminator, so the caret must share its operand's line. A
	// caret ending its line still continues, so `a ^` newline `b` stays XOR.
	for parser.check(lexer.Caret) && parser.onPreviousTokenLine() {
		caret := parser.current
		recorded, kind, token := parser.binaryOperatorRecorded, parser.binaryOperatorKind, parser.binaryOperatorToken
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.bitwiseAndExpression()
		if err != nil {
			return nil, err
		}
		if parser.check(lexer.Equal) {
			// `^operand = ...` opens a dereference assignment statement,
			// not an XOR continuation: no valid program assigns to an XOR
			// result, so the caret began a prefix dereference. Rewind,
			// restoring the operator-kind region the speculative parse
			// recorded.
			parser.current = caret
			parser.binaryOperatorRecorded, parser.binaryOperatorKind, parser.binaryOperatorToken = recorded, kind, token
			break
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) bitwiseAndExpression() (Expression, error) {
	expression, err := parser.equalityExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.Amp) {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.equalityExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) equalityExpression() (Expression, error) {
	expression, err := parser.typeTestExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.EqualEqual) || parser.check(lexer.BangEqual) {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.typeTestExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) typeTestExpression() (Expression, error) {
	expression, err := parser.relationalExpression()
	if err != nil {
		return nil, err
	}
	// In a match scrutinee an unparenthesized `is` selects type mode rather
	// than testing the scrutinee type; a scrutinee containing `is` must be
	// parenthesized.
	if parser.matchBoundary == scrutineeBoundary || !parser.check(lexer.Is) {
		return expression, nil
	}
	isToken := parser.advance()
	if err := parser.recordBinaryOperator(isToken); err != nil {
		return nil, err
	}
	typeExpression, err := parser.typeExpression()
	if err != nil {
		return nil, err
	}
	if parser.check(lexer.Is) {
		return nil, parser.errorAtCurrent("is tests cannot be chained")
	}
	return TypeTestExpression{Operand: expression, IsToken: isToken, Type: typeExpression}, nil
}

func (parser *Parser) relationalExpression() (Expression, error) {
	expression, err := parser.shiftExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.Less) || parser.check(lexer.LessEqual) ||
		parser.check(lexer.Greater) || parser.check(lexer.GreaterEqual) {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.shiftExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) shiftExpression() (Expression, error) {
	expression, err := parser.additiveExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.ShiftLeft) || parser.check(lexer.ShiftRight) {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.additiveExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) additiveExpression() (Expression, error) {
	expression, err := parser.multiplicativeExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.Plus) || parser.check(lexer.Minus) {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.multiplicativeExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) multiplicativeExpression() (Expression, error) {
	expression, err := parser.unaryExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.Star) || parser.check(lexer.Slash) || parser.check(lexer.Percent) {
		operator := parser.advance()
		if err := parser.recordBinaryOperator(operator); err != nil {
			return nil, err
		}
		right, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		expression = BinaryExpression{Left: expression, Operator: operator, Right: right}
	}
	return expression, nil
}

func (parser *Parser) unaryExpression() (Expression, error) {
	if parser.check(lexer.Mut) {
		return nil, parser.errorAtCurrent("mut is not valid on the right-hand side; use @value")
	}

	switch {
	case parser.check(lexer.Minus):
		minus := parser.advance()
		if parser.isNumericLiteral() {
			literal, err := parser.numericLiteral()
			if err != nil {
				return nil, err
			}
			return NegatedNumericLiteral{Minus: minus, Literal: literal}, nil
		}
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return UnaryExpression{Operator: minus, Operand: operand}, nil
	case parser.check(lexer.Bang):
		operator := parser.advance()
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return UnaryExpression{Operator: operator, Operand: operand}, nil
	case parser.check(lexer.Tilde):
		operator := parser.advance()
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return UnaryExpression{Operator: operator, Operand: operand}, nil
	case parser.check(lexer.Try):
		keyword := parser.advance()
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return TryExpression{Keyword: keyword, Operand: operand}, nil
	case parser.check(lexer.Spawn):
		keyword := parser.advance()
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return SpawnExpression{Keyword: keyword, Operand: operand}, nil
	case parser.check(lexer.At):
		operator := parser.advance()
		operand, err := parser.addressOperand()
		if err != nil {
			return nil, err
		}
		return AddressExpression{Operator: operator, Place: operand}, nil
	case parser.check(lexer.Caret):
		operator := parser.advance()
		operand, err := parser.unaryExpression()
		if err != nil {
			return nil, err
		}
		return DereferenceExpression{Operator: operator, Operand: operand}, nil
	default:
		return parser.primaryExpression()
	}
}

func (parser *Parser) isNumericLiteral() bool {
	switch parser.peek().Kind {
	case lexer.Integer, lexer.HexInteger, lexer.BinaryInteger, lexer.OctalInteger, lexer.DecimalFloat:
		return true
	default:
		return false
	}
}

// numericLiteral parses the operand preserved by the direct negative-literal
// path.
func (parser *Parser) numericLiteral() (Expression, error) {
	switch parser.peek().Kind {
	case lexer.Integer, lexer.HexInteger, lexer.BinaryInteger, lexer.OctalInteger:
		return IntegerLiteral{Token: parser.advance()}, nil
	case lexer.DecimalFloat:
		return DecimalLiteral{Token: parser.advance()}, nil
	default:
		return nil, parser.errorAtCurrent("expected an integer or decimal floating literal after '-'")
	}
}

// addressOperand parses `@`'s operand: optional prefix `^` dereferences
// around one addressable place, so `@^pointer` addresses a dereferenced
// place while preserving its access mode.
func (parser *Parser) addressOperand() (Expression, error) {
	if parser.check(lexer.Caret) {
		operator := parser.advance()
		operand, err := parser.addressOperand()
		if err != nil {
			return nil, err
		}
		return DereferenceExpression{Operator: operator, Operand: operand}, nil
	}
	return parser.place()
}

// place parses a syntactic place accepted by @ and assignment targets.
// Member names are intentionally left unresolved for the checker.
func (parser *Parser) place() (Expression, error) {
	name, err := parser.consume(lexer.Identifier, "a place identifier")
	if err != nil {
		return nil, err
	}
	expression := Expression(VariableExpression{Name: name})
	// A place is an addressable root followed by any ordered sequence of
	// member and index suffixes, so `@rows[0].field` and
	// `@^(grid[0].cells[1])` are valid. The checker derives capability
	// from the complete place.
	for {
		if parser.check(lexer.Dot) {
			parser.advance()
			property, err := parser.consume(lexer.Identifier, "an identifier after '.'")
			if err != nil {
				return nil, err
			}
			expression = PropertyExpression{Receiver: expression, Property: property}
			continue
		}
		if parser.check(lexer.LeftBracket) {
			// @ accepts addressable collection elements too, so
			// `@values[2]` refers to one element without creating an array
			// pointer.
			open := parser.advance()
			index, err := parser.expression()
			if err != nil {
				return nil, err
			}
			close, err := parser.consume(lexer.RightBracket, "']'")
			if err != nil {
				return nil, err
			}
			expression = IndexExpression{Receiver: expression, OpenBracket: open, Index: index, CloseBracket: close}
			continue
		}
		break
	}
	return expression, nil
}

func (parser *Parser) primaryExpression() (Expression, error) {
	var expression Expression
	switch parser.peek().Kind {
	case lexer.Fun:
		// In expression position, `fun (` starts a concrete anonymous literal
		// and `fun<` starts a generic one. `fun` followed by anything else
		// is a syntax error at the `fun` keyword.
		if !parser.funBeginsAnonymousLiteral() {
			return nil, parser.errorAt(parser.peek(), "anonymous function requires '(' or '<' after 'fun'")
		}
		literal, err := parser.anonymousFunctionLiteral()
		if err != nil {
			return nil, err
		}
		expression = literal
	case lexer.Integer, lexer.HexInteger, lexer.BinaryInteger, lexer.OctalInteger:
		expression = IntegerLiteral{Token: parser.advance()}
	case lexer.DecimalFloat:
		expression = DecimalLiteral{Token: parser.advance()}
	case lexer.True, lexer.False:
		expression = BooleanLiteral{Token: parser.advance()}
	case lexer.NilLiteral:
		expression = NilLiteral{Token: parser.advance()}
	case lexer.Eos:
		expression = EosLiteral{Token: parser.advance()}
	case lexer.StringLiteral:
		expression = StringLiteral{Token: parser.advance()}
	case lexer.RawStringLiteral:
		expression = RawStringLiteral{Token: parser.advance()}
	case lexer.InterpStringStart:
		var err error
		expression, err = parser.interpolationTemplate()
		if err != nil {
			return nil, err
		}
	case lexer.ByteLiteral:
		expression = ByteLiteral{Token: parser.advance()}
	case lexer.RuneLiteral:
		expression = RuneLiteral{Token: parser.advance()}
	case lexer.Match:
		var err error
		expression, err = parser.matchExpression()
		if err != nil {
			return nil, err
		}
	case lexer.Self:
		// self is an ordinary receiver name to the parser. Binding it to an
		// impl body is the checker's job.
		expression = VariableExpression{Name: parser.advance()}
	case lexer.Identifier:
		expression = VariableExpression{Name: parser.advance()}
	case lexer.LeftParen:
		parser.advance()
		outer := parser.matchBoundary
		parser.matchBoundary = noMatchBoundary
		var err error
		expression, err = parser.expression()
		parser.matchBoundary = outer
		if err != nil {
			return nil, err
		}
		if _, err := parser.consume(lexer.RightParen, "')' after expression"); err != nil {
			return nil, err
		}
	case lexer.LeftBracket:
		open := parser.advance()
		elements := make([]Expression, 0)
		if !parser.check(lexer.RightBracket) {
			for {
				element, err := parser.expression()
				if err != nil {
					return nil, err
				}
				elements = append(elements, element)
				if !parser.check(lexer.Comma) {
					break
				}
				parser.advance()
				if parser.check(lexer.RightBracket) {
					break
				}
			}
		}
		close, err := parser.consume(lexer.RightBracket, "']' after the array literal elements")
		if err != nil {
			return nil, err
		}
		expression = ArrayLiteralExpression{OpenBracket: open, Elements: elements, CloseBracket: close}
	default:
		return nil, parser.errorAtCurrent("expected a value")
	}

	return parser.postfix(expression)
}

// postfix parses dotted member selections and calls, which alternate freely:
// point.translate(5, 5) is a member selection followed by a call. The checker
// resolves whether a selected name is an object member, a built-in pointer
// property, or a method. A generic call suffix name<T>(...) is recognized only
// when a balanced type-argument list is immediately followed by a call list.
func (parser *Parser) postfix(expression Expression) (Expression, error) {
	for {
		switch {
		case parser.check(lexer.Less) && parser.genericConstructorFollows():
			// Owner<Args>.name(...): a type-argument list on a bare type name
			// followed by a dotted call, e.g. a qualified generic ADT variant
			// constructor Result<Int32, String>.Ok(value).
			arguments, err := parser.typeArgumentList()
			if err != nil {
				return nil, err
			}
			if _, err := parser.consume(lexer.Dot, "'.' after generic owner arguments"); err != nil {
				return nil, err
			}
			property, err := parser.consume(lexer.Identifier, "a name after '.'")
			if err != nil {
				return nil, err
			}
			call, err := parser.callArguments(PropertyExpression{Receiver: expression, Property: property})
			if err != nil {
				return nil, err
			}
			call.TypeArguments = arguments
			expression = call
		case parser.check(lexer.Dot):
			parser.advance()
			property, err := parser.consume(lexer.Identifier, "an identifier after '.'")
			if err != nil {
				return nil, err
			}
			expression = PropertyExpression{Receiver: expression, Property: property}
		case parser.check(lexer.LeftBrace) && parser.onPreviousTokenLine():
			return nil, parser.errorAtCurrent("constructors use named arguments in parentheses")
		case parser.check(lexer.Less) && parser.genericCallFollows():
			arguments, err := parser.typeArgumentList()
			if err != nil {
				return nil, err
			}
			call, err := parser.callArguments(expression)
			if err != nil {
				return nil, err
			}
			call.TypeArguments = arguments
			expression = call
		case parser.check(lexer.LeftBracket):
			open := parser.advance()
			index, err := parser.expression()
			if err != nil {
				return nil, err
			}
			close, err := parser.consume(lexer.RightBracket, "']' after the index expression")
			if err != nil {
				return nil, err
			}
			expression = IndexExpression{Receiver: expression, OpenBracket: open, Index: index, CloseBracket: close}
		case parser.check(lexer.LeftParen) && parser.onPreviousTokenLine():
			call, err := parser.callArguments(expression)
			if err != nil {
				return nil, err
			}
			expression = call
		default:
			return expression, nil
		}
	}
}

// consumeGenericClose consumes one '>' generic closer, splitting a '>>'
// token into two closers when nested type arguments need both.
func (parser *Parser) consumeGenericClose(expected string) (lexer.Token, error) {
	if parser.pendingGreater {
		return parser.consume(lexer.Greater, expected)
	}
	if parser.check(lexer.ShiftRight) {
		token := parser.advance()
		parser.pendingGreater = true
		return lexer.Token{Kind: lexer.Greater, Lexeme: ">", Line: token.Line, Column: token.Column}, nil
	}
	return parser.consume(lexer.Greater, expected)
}

// balancedTypeArgumentEnd scans from a '<' at the current position to the
// matching '>' tracking nested '<' pairs, and returns the index of the
// matching '>' token, or -1 when no balanced close exists.
func (parser *Parser) balancedTypeArgumentEnd() int {
	depth := 0
	for index := parser.current; index < len(parser.tokens); index++ {
		switch parser.tokens[index].Kind {
		case lexer.Less:
			depth++
		case lexer.Greater:
			depth--
			if depth == 0 {
				return index
			}
		case lexer.ShiftRight:
			// A `>>` token closes two nested generic argument lists.
			depth -= 2
			if depth <= 0 {
				return index
			}
		}
	}
	return -1
}

// genericCallFollows reports whether a balanced type-argument list at the
// current '<' is immediately followed by a call argument list.
func (parser *Parser) genericCallFollows() bool {
	end := parser.balancedTypeArgumentEnd()
	return end >= 0 && end+1 < len(parser.tokens) && parser.tokens[end+1].Kind == lexer.LeftParen
}

// genericConstructorFollows reports whether a balanced type-argument list at
// the current '<' is immediately followed by ".name(": a qualified generic
// ADT variant constructor, e.g. Result<Int32, String>.Ok(value).
func (parser *Parser) genericConstructorFollows() bool {
	end := parser.balancedTypeArgumentEnd()
	return end >= 0 && end+3 < len(parser.tokens) &&
		parser.tokens[end+1].Kind == lexer.Dot &&
		parser.tokens[end+2].Kind == lexer.Identifier &&
		parser.tokens[end+3].Kind == lexer.LeftParen
}

// typeArgumentList parses "<" type-expression { "," type-expression } ">".
// A leading `mut` on one argument marks it without consuming the marking:
// only the Slice bridge interprets MutTypeArgument, and type resolution
// rejects it everywhere else.
func (parser *Parser) typeArgumentList() ([]TypeExpression, error) {
	if _, err := parser.consume(lexer.Less, "'<'"); err != nil {
		return nil, err
	}
	arguments := make([]TypeExpression, 0, 1)
	argument, err := parser.typeArgument()
	if err != nil {
		return nil, err
	}
	arguments = append(arguments, argument)
	for parser.check(lexer.Comma) {
		parser.advance()
		argument, err := parser.typeArgument()
		if err != nil {
			return nil, err
		}
		arguments = append(arguments, argument)
	}
	if _, err := parser.consumeGenericClose("'>' after a generic argument list"); err != nil {
		return nil, err
	}
	return arguments, nil
}

// typeArgument parses one call-site type argument, preserving an optional
// leading `mut` marking for the Slice bridge.
func (parser *Parser) typeArgument() (TypeExpression, error) {
	if parser.check(lexer.Mut) {
		mut := parser.advance()
		inner, err := parser.typeExpression()
		if err != nil {
			return nil, err
		}
		return MutTypeArgument{Mut: mut, Type: inner}, nil
	}
	return parser.typeExpression()
}

// onPreviousTokenLine reports whether the current token shares a source line
// with the token before it. The grammar has no statement terminator, so a
// call's '(' must not be separated from its callee by a newline: `compute`
// followed by `(value)` on the next line is two items, not one call. The
// callee's last token is always the one immediately before the '('.
func (parser *Parser) onPreviousTokenLine() bool {
	if parser.current == 0 || parser.current >= len(parser.tokens) {
		return false
	}
	return parser.tokens[parser.current].Line == parser.tokens[parser.current-1].Line
}

// genericVariantFollows reports whether a balanced type-argument list at the
// current '<' is immediately followed by a qualified-variant dot, as in
// Result<Int32, Bool>.Ok.
func (parser *Parser) genericVariantFollows() bool {
	end := parser.balancedTypeArgumentEnd()
	return end >= 0 && end+1 < len(parser.tokens) && parser.tokens[end+1].Kind == lexer.Dot
}

// matchExpression parses `match scrutinee [is] { "|" pattern "then" expression } "end"`.
// The scrutinee is parsed below the type-test level so an `is` immediately
// before the first arm selects type mode; a scrutinee containing `is` must be
// parenthesized.
func (parser *Parser) matchExpression() (Expression, error) {
	keyword := parser.advance()
	// The scrutinee uses the full expression grammar under a boundary that
	// stops the top-level chain before an unparenthesized `is` (type-mode
	// marker) or `|` (first arm). Parenthesized subexpressions suspend the
	// boundary.
	outer := parser.matchBoundary
	parser.matchBoundary = scrutineeBoundary
	exit, err := parser.enterSyntax()
	if err != nil {
		exit()
		parser.matchBoundary = outer
		return nil, err
	}
	restoreRegion := parser.pushExpressionRegion()
	scrutinee, err := parser.orExpression()
	restoreRegion()
	exit()
	parser.matchBoundary = outer
	if err != nil {
		return nil, err
	}
	typeMode := false
	if parser.check(lexer.Is) {
		parser.advance()
		typeMode = true
	}
	arms := make([]MatchArm, 0, 2)
	for parser.check(lexer.Pipe) {
		pipe := parser.advance()
		pattern, err := parser.matchPattern(typeMode)
		if err != nil {
			return nil, err
		}
		then, err := parser.consume(lexer.Then, "'then' after a match pattern")
		if err != nil {
			return nil, err
		}
		// The arm result uses the full expression grammar under a boundary
		// that stops before an unparenthesized `|` (next arm separator).
		parser.matchBoundary = armBoundary
		armExit, armErr := parser.enterSyntax()
		if armErr != nil {
			armExit()
			parser.matchBoundary = outer
			return nil, armErr
		}
		restoreRegion := parser.pushExpressionRegion()
		armExpression, err := parser.orExpression()
		restoreRegion()
		armExit()
		parser.matchBoundary = outer
		if err != nil {
			return nil, err
		}
		arms = append(arms, MatchArm{Pipe: pipe, Pattern: pattern, Then: then, Expression: armExpression})
	}
	if len(arms) == 0 {
		return nil, parser.errorAtCurrent("expected a match arm after '|'")
	}
	end, err := parser.consume(lexer.End, "'end' after a match expression")
	if err != nil {
		return nil, err
	}
	return MatchExpression{Keyword: keyword, Scrutinee: scrutinee, TypeMode: typeMode, Arms: arms, End: end}, nil
}

// matchPattern parses one arm pattern. Mode enforcement belongs to the
// checker; the parser accepts booleans, else, neutral dotted arms, explicit
// generic variants, and type patterns in either mode. A simple `Owner.Name`
// stays neutral so an import alias owner can later resolve as a type.
func (parser *Parser) matchPattern(typeMode bool) (MatchPattern, error) {
	if parser.check(lexer.Else) {
		return ElsePattern{Token: parser.advance()}, nil
	}
	if parser.check(lexer.True) || parser.check(lexer.False) {
		return BoolPattern{Token: parser.advance()}, nil
	}
	if parser.check(lexer.Identifier) && parser.peekAt(1).Kind == lexer.Less && parser.genericVariantFollows() {
		owner := parser.advance()
		arguments, err := parser.typeArgumentList()
		if err != nil {
			return nil, err
		}
		if _, err := parser.consume(lexer.Dot, "'.' after generic owner arguments"); err != nil {
			return nil, err
		}
		variant := parser.advance()
		return VariantPattern{Owner: owner, OwnerArguments: arguments, Variant: variant}, nil
	}
	if parser.check(lexer.Identifier) && parser.peekAt(1).Kind == lexer.Dot && parser.peekAt(2).Kind == lexer.Identifier {
		owner := parser.advance()
		parser.advance()
		name := parser.advance()
		return DottedPattern{Owner: owner, Name: name}, nil
	}
	typeExpression, err := parser.primaryTypeExpression()
	if err != nil {
		return nil, err
	}
	return TypePattern{Type: typeExpression}, nil
}

// peek returns the token at the given lookahead offset.
func (parser *Parser) peekAt(offset int) lexer.Token {
	index := parser.current + offset
	if index >= len(parser.tokens) {
		return lexer.Token{Kind: lexer.EOF, Line: 1, Column: 1}
	}
	return parser.tokens[index]
}

// callArguments parses the argument list. Only the '(' placement is
// line-sensitive; arguments may break across lines freely. Each argument may
// open with an `identifier =` label; a trailing comma is accepted after the
// last argument. Whether labels are required, forbidden, or matched against
// declared fields is the checker's decision once the callee resolves.
func (parser *Parser) callArguments(callee Expression) (CallExpression, error) {
	openParen := parser.advance()
	arguments := make([]Expression, 0)
	var labels []*lexer.Token
	if !parser.check(lexer.RightParen) {
		for {
			var label *lexer.Token
			if parser.check(lexer.Identifier) && parser.peekAt(1).Kind == lexer.Equal {
				name := parser.advance()
				parser.advance() // '='
				label = &name
			}
			argument, err := parser.expression()
			if err != nil {
				return CallExpression{}, err
			}
			arguments = append(arguments, argument)
			labels = append(labels, label)
			if !parser.check(lexer.Comma) {
				break
			}
			parser.advance()
			if parser.check(lexer.RightParen) {
				break
			}
		}
	}
	if _, err := parser.consume(lexer.RightParen, "')' after the argument list"); err != nil {
		return CallExpression{}, err
	}
	return CallExpression{Callee: callee, OpenParen: openParen, Arguments: arguments, ArgumentLabels: labels}, nil
}
