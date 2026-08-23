package parser

import "hexal/compiler/lexer"

// expression starts the precedence ladder. mut is only valid before a binding
// or member name in a declaration, never as an expression prefix.
func (parser *Parser) expression() (Expression, error) {
	return parser.orExpression()
}

func (parser *Parser) orExpression() (Expression, error) {
	expression, err := parser.andExpression()
	if err != nil {
		return nil, err
	}
	for parser.check(lexer.Or) {
		operator := parser.advance()
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
	for parser.check(lexer.Caret) {
		operator := parser.advance()
		right, err := parser.bitwiseAndExpression()
		if err != nil {
			return nil, err
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
		return nil, parser.errorAtCurrent("mut is not valid on the right-hand side; use ref value")
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
	case parser.check(lexer.Ref):
		keyword := parser.advance()
		place, err := parser.place()
		if err != nil {
			return nil, err
		}
		return RefExpression{Keyword: keyword, Place: place}, nil
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

// place parses a syntactic place accepted by ref and assignment targets.
// Member names are intentionally left unresolved for the checker.
func (parser *Parser) place() (Expression, error) {
	name, err := parser.consume(lexer.Identifier, "a place identifier")
	if err != nil {
		return nil, err
	}
	expression := Expression(VariableExpression{Name: name})
	// A place is an addressable root followed by any ordered sequence of
	// member and index suffixes, so `ref rows[0].field` and
	// `ref grid[0].cells[1].value` are valid. The checker derives capability
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
			// ref accepts addressable collection elements too, so
			// `ref values[2]` refers to one element without creating an array
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
	if parser.check(lexer.LeftParen) {
		return nil, parser.errorAtCurrent("ref requires a place")
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
		typeName := parser.advance()
		if parser.check(lexer.Less) && parser.genericObjectFollows() {
			arguments, err := parser.typeArgumentList()
			if err != nil {
				return nil, err
			}
			literal, err := parser.objectLiteral(typeName)
			if err != nil {
				return nil, err
			}
			literal.TypeArguments = arguments
			expression = literal
		} else if parser.check(lexer.LeftBrace) {
			var err error
			expression, err = parser.objectLiteral(typeName)
			if err != nil {
				return nil, err
			}
		} else {
			expression = VariableExpression{Name: typeName}
		}
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
			// List<T>.new(h) and Dict<K,V>.new(h): a type-argument list on a
			// type name followed by a method call.
			arguments, err := parser.typeArgumentList()
			if err != nil {
				return nil, err
			}
			if _, err := parser.consume(lexer.Dot, "'.' after collection type arguments"); err != nil {
				return nil, err
			}
			property, err := parser.consume(lexer.Identifier, "a constructor name after '.'")
			if err != nil {
				return nil, err
			}
			call, err := parser.callArguments(PropertyExpression{Receiver: expression, Property: property})
			if err != nil {
				return nil, err
			}
			call.TypeArguments = arguments
			expression = call
		case parser.check(lexer.Less) && parser.genericVariantFollows():
			arguments, err := parser.typeArgumentList()
			if err != nil {
				return nil, err
			}
			if _, err := parser.consume(lexer.Dot, "'.' after generic owner arguments"); err != nil {
				return nil, err
			}
			variant, err := parser.consume(lexer.Identifier, "a variant name after '.'")
			if err != nil {
				return nil, err
			}
			constructor := QualifiedVariantExpression{Owner: expression.(VariableExpression).Name, OwnerArguments: arguments, Variant: variant}
			if parser.check(lexer.LeftBrace) {
				payload, err := parser.variantPayload(variant)
				if err != nil {
					return nil, err
				}
				constructor.Payload = &payload
			}
			expression = constructor
		case parser.check(lexer.Dot):
			parser.advance()
			property, err := parser.consume(lexer.Identifier, "an identifier after '.'")
			if err != nil {
				return nil, err
			}
			propertyExpression := PropertyExpression{Receiver: expression, Property: property}
			if parser.check(lexer.LeftBrace) {
				if variable, isVariable := expression.(VariableExpression); isVariable {
					payload, err := parser.variantPayload(property)
					if err != nil {
						return nil, err
					}
					expression = QualifiedVariantExpression{Owner: variable.Name, Variant: property, Payload: &payload}
					continue
				}
			}
			expression = propertyExpression
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

// genericObjectFollows reports whether a balanced type-argument list at the
// current '<' is immediately followed by an object literal brace.
func (parser *Parser) genericObjectFollows() bool {
	end := parser.balancedTypeArgumentEnd()
	return end >= 0 && end+1 < len(parser.tokens) && parser.tokens[end+1].Kind == lexer.LeftBrace
}

// genericConstructorFollows reports whether a balanced type-argument list at
// the current '<' is immediately followed by ".name(", the collection
// constructor form List<T>.new(h). It must be recognized before the
// qualified-variant form, which also matches "<...>.name".
func (parser *Parser) genericConstructorFollows() bool {
	end := parser.balancedTypeArgumentEnd()
	return end >= 0 && end+3 < len(parser.tokens) &&
		parser.tokens[end+1].Kind == lexer.Dot &&
		parser.tokens[end+2].Kind == lexer.Identifier &&
		parser.tokens[end+3].Kind == lexer.LeftParen
}

// typeArgumentList parses "<" type-expression { "," type-expression } ">".
func (parser *Parser) typeArgumentList() ([]TypeExpression, error) {
	if _, err := parser.consume(lexer.Less, "'<'"); err != nil {
		return nil, err
	}
	arguments := make([]TypeExpression, 0, 1)
	argument, err := parser.typeExpression()
	if err != nil {
		return nil, err
	}
	arguments = append(arguments, argument)
	for parser.check(lexer.Comma) {
		parser.advance()
		argument, err := parser.typeExpression()
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

// variantPayload parses a qualified record variant constructor's
// `{ field = expr, ... }` initializer list.
func (parser *Parser) variantPayload(variant lexer.Token) ([]MemberInitializer, error) {
	if _, err := parser.consume(lexer.LeftBrace, "'{' after a variant name"); err != nil {
		return nil, err
	}
	initializers := make([]MemberInitializer, 0)
	if !parser.check(lexer.RightBrace) {
		for {
			initializer, err := parser.memberInitializer()
			if err != nil {
				return nil, err
			}
			initializers = append(initializers, initializer)
			if !parser.check(lexer.Comma) {
				break
			}
			parser.advance()
			if parser.check(lexer.RightBrace) {
				break
			}
		}
	}
	if _, err := parser.consume(lexer.RightBrace, "'}' after a variant payload initializer"); err != nil {
		return nil, err
	}
	return initializers, nil
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
	scrutinee, err := parser.orExpression()
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
		armExpression, err := parser.orExpression()
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
// checker; the parser accepts booleans, else, qualified variants, and type
// patterns in either mode.
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
		variant := parser.advance()
		return VariantPattern{Owner: owner, Variant: variant}, nil
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
// line-sensitive; arguments may break across lines freely.
func (parser *Parser) callArguments(callee Expression) (CallExpression, error) {
	openParen := parser.advance()
	arguments := make([]Expression, 0)
	if !parser.check(lexer.RightParen) {
		for {
			argument, err := parser.expression()
			if err != nil {
				return CallExpression{}, err
			}
			arguments = append(arguments, argument)
			if !parser.check(lexer.Comma) {
				break
			}
			parser.advance()
		}
	}
	if _, err := parser.consume(lexer.RightParen, "')' after the argument list"); err != nil {
		return CallExpression{}, err
	}
	return CallExpression{Callee: callee, OpenParen: openParen, Arguments: arguments}, nil
}

// objectLiteral parses a named object value. An empty literal is syntactically
// valid; exhaustiveness and member validity belong to semantic checking.
func (parser *Parser) objectLiteral(typeName lexer.Token) (ObjectLiteral, error) {
	openBrace, err := parser.consume(lexer.LeftBrace, "'{' after an object type name")
	if err != nil {
		return ObjectLiteral{}, err
	}

	initializers := make([]MemberInitializer, 0)
	if !parser.check(lexer.RightBrace) {
		initializer, err := parser.memberInitializer()
		if err != nil {
			return ObjectLiteral{}, err
		}
		initializers = append(initializers, initializer)
		for parser.check(lexer.Comma) {
			parser.advance()
			if parser.check(lexer.RightBrace) {
				break
			}
			initializer, err := parser.memberInitializer()
			if err != nil {
				return ObjectLiteral{}, err
			}
			initializers = append(initializers, initializer)
		}
	}

	closeBrace, err := parser.consume(lexer.RightBrace, "'}' after object literal")
	if err != nil {
		return ObjectLiteral{}, err
	}
	return ObjectLiteral{
		TypeName:     typeName,
		OpenBrace:    openBrace,
		Initializers: initializers,
		CloseBrace:   closeBrace,
	}, nil
}

func (parser *Parser) memberInitializer() (MemberInitializer, error) {
	if parser.check(lexer.Mut) {
		return MemberInitializer{}, parser.errorAtCurrent("mut is not allowed in an object literal")
	}
	name, err := parser.consume(lexer.Identifier, "an object member name")
	if err != nil {
		return MemberInitializer{}, err
	}
	equal, err := parser.consume(lexer.Equal, "'=' after an object member name")
	if err != nil {
		return MemberInitializer{}, err
	}
	value, err := parser.expression()
	if err != nil {
		return MemberInitializer{}, err
	}
	return MemberInitializer{Name: name, Equal: equal, Value: value}, nil
}
