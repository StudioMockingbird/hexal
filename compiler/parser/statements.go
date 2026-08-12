package parser

import (
	"strings"

	"hexal/compiler/lexer"
)

func (parser *Parser) typeDeclaration() (TypeDeclaration, error) {
	keyword, err := parser.consume(lexer.Type, "'type'")
	if err != nil {
		return TypeDeclaration{}, err
	}
	name, err := parser.consume(lexer.Identifier, "an identifier after 'type'")
	if err != nil {
		return TypeDeclaration{}, err
	}
	parameters, err := parser.genericParameterList()
	if err != nil {
		return TypeDeclaration{}, err
	}
	if _, err := parser.consume(lexer.Equal, "'='"); err != nil {
		return TypeDeclaration{}, err
	}
	target, err := parser.typeDefinitionExpression()
	if err != nil {
		return TypeDeclaration{}, err
	}
	return TypeDeclaration{Keyword: keyword, Name: name, Parameters: parameters, Target: target}, nil
}

// genericParameterList parses an optional "<" identifier { "," identifier }
// ">" generic parameter list and returns the parameter tokens. An absent
// list returns an empty slice.
func (parser *Parser) genericParameterList() ([]lexer.Token, error) {
	if !parser.check(lexer.Less) {
		return nil, nil
	}
	if _, err := parser.consume(lexer.Less, "'<'"); err != nil {
		return nil, err
	}
	parameters := make([]lexer.Token, 0, 1)
	parameter, err := parser.consume(lexer.Identifier, "a generic parameter name")
	if err != nil {
		return nil, err
	}
	parameters = append(parameters, parameter)
	for parser.check(lexer.Comma) {
		parser.advance()
		parameter, err := parser.consume(lexer.Identifier, "a generic parameter name")
		if err != nil {
			return nil, err
		}
		parameters = append(parameters, parameter)
	}
	if _, err := parser.consume(lexer.Greater, "'>' after a generic parameter list"); err != nil {
		return nil, err
	}
	return parameters, nil
}

func (parser *Parser) functionDeclaration() (FunctionDeclaration, error) {
	keyword := parser.advance()
	name, err := parser.consume(lexer.Identifier, "a function name after 'fun'")
	if err != nil {
		return FunctionDeclaration{}, err
	}
	typeParameters, err := parser.genericParameterList()
	if err != nil {
		return FunctionDeclaration{}, err
	}
	parameters, returnType, err := parser.signature()
	if err != nil {
		return FunctionDeclaration{}, err
	}
	diagnosticsBeforeBody := len(parser.diagnostics)
	body, end, err := parser.body("function " + name.Lexeme)
	if err != nil {
		return FunctionDeclaration{}, err
	}
	return FunctionDeclaration{
		Keyword:         keyword,
		Name:            name,
		TypeParameters:  typeParameters,
		Parameters:      parameters,
		Return:          returnType,
		Body:            body,
		End:             end,
		HasSyntaxErrors: len(parser.diagnostics) > diagnosticsBeforeBody,
	}, nil
}

func (parser *Parser) implDeclaration() (ImplDeclaration, error) {
	keyword := parser.advance()
	// The receiver forms are exactly the identifier and pointer-constructor
	// type expressions, so the shared type grammar covers them.
	selfType, err := parser.typeExpression()
	if err != nil {
		return ImplDeclaration{}, err
	}
	if _, err := parser.consume(lexer.Dot, "'.' after an impl receiver type"); err != nil {
		return ImplDeclaration{}, err
	}
	name, err := parser.consume(lexer.Identifier, "a method name after '.'")
	if err != nil {
		return ImplDeclaration{}, err
	}
	typeParameters, err := parser.genericParameterList()
	if err != nil {
		return ImplDeclaration{}, err
	}
	parameters, returnType, err := parser.signature()
	if err != nil {
		return ImplDeclaration{}, err
	}
	diagnosticsBeforeBody := len(parser.diagnostics)
	body, end, err := parser.body("method " + name.Lexeme)
	if err != nil {
		return ImplDeclaration{}, err
	}
	return ImplDeclaration{
		Keyword:         keyword,
		SelfType:        selfType,
		Name:            name,
		TypeParameters:  typeParameters,
		Parameters:      parameters,
		Return:          returnType,
		Body:            body,
		End:             end,
		HasSyntaxErrors: len(parser.diagnostics) > diagnosticsBeforeBody,
	}, nil
}

// signature parses the shared parameter list and optional return clause. A
// missing return clause means the declaration produces no value.
func (parser *Parser) signature() ([]Parameter, TypeExpression, error) {
	if _, err := parser.consume(lexer.LeftParen, "'(' after a declaration name"); err != nil {
		return nil, nil, err
	}
	parameters := make([]Parameter, 0)
	if !parser.check(lexer.RightParen) {
		for {
			parameter, err := parser.parameter()
			if err != nil {
				return nil, nil, err
			}
			parameters = append(parameters, parameter)
			if !parser.check(lexer.Comma) {
				break
			}
			parser.advance()
		}
	}
	if _, err := parser.consume(lexer.RightParen, "')' after a parameter list"); err != nil {
		return nil, nil, err
	}
	if !parser.check(lexer.Colon) {
		return parameters, nil, nil
	}
	parser.advance()
	returnType, err := parser.typeExpression()
	if err != nil {
		return nil, nil, err
	}
	return parameters, returnType, nil
}

func (parser *Parser) parameter() (Parameter, error) {
	name, err := parser.consume(lexer.Identifier, "a parameter name")
	if err != nil {
		return Parameter{}, err
	}
	if !parser.check(lexer.Colon) {
		return Parameter{}, parser.errorAt(name, "function parameters require type annotations")
	}
	parser.advance()
	typeExpression, err := parser.typeExpression()
	if err != nil {
		return Parameter{}, err
	}
	return Parameter{Name: name, Type: typeExpression}, nil
}

// body parses a function or method body up to its closing `end`. Control-flow
// bodies use block directly so each construct can own its delimiter set.
func (parser *Parser) body(owner string) ([]Statement, lexer.Token, error) {
	parser.bodyDepth++
	defer func() { parser.bodyDepth-- }()

	statements, err := parser.block(owner, lexer.End)
	if err != nil {
		return nil, lexer.Token{}, err
	}
	return statements, parser.advance(), nil
}

// block parses statements until one of the construct-specific delimiters is
// reached. Delimiters are left for the owner to consume, which preserves the
// nearest-open construct during nested recovery.
func (parser *Parser) block(owner string, stops ...lexer.TokenKind) ([]Statement, error) {
	parser.blockStack = append(parser.blockStack, blockOwner(owner))
	defer func() { parser.blockStack = parser.blockStack[:len(parser.blockStack)-1] }()
	statements := make([]Statement, 0)
	for {
		if parser.atBlockStop(stops) {
			return statements, nil
		}
		if parser.check(lexer.EOF) {
			return nil, blockFailure{err: parser.errorAtCurrent("expected end to close " + owner)}
		}
		start := parser.current
		statement, err := parser.statement()
		if err != nil {
			switch err.(type) {
			case blockFailure, blockRecovery:
				// Sentinel errors carry their own (or an outer block's)
				// diagnostic and must propagate without re-recording.
				return nil, err
			}
			parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(err)...)
			parser.synchronizeBlock(start)
			if parser.check(lexer.EOF) {
				parser.diagnostics = append(parser.diagnostics, diagnosticsFrom(parser.errorAtCurrent("expected end to close "+owner))...)
				return nil, blockRecovery{}
			}
			// A delimiter that is not this block's stop was the reported
			// error's own token, such as a stray 'else' inside a while body.
			// Consume it after recording the diagnostic so this block keeps
			// parsing until its own delimiter closes it.
			if isBlockDelimiter(parser.peek().Kind) && !parser.atBlockStop(stops) {
				parser.advance()
			}
			continue
		}
		statements = append(statements, statement)
	}
}

func (parser *Parser) atBlockStop(stops []lexer.TokenKind) bool {
	for _, stop := range stops {
		if parser.check(stop) {
			return true
		}
	}
	return false
}

// blockOwner maps a block's owner description to the kind used for stack
// bookkeeping. Control-flow constructs keep their own name; function and
// method bodies collapse to one kind because neither owns elseif or else.
func blockOwner(owner string) string {
	switch owner {
	case "while":
		return "while"
	case "else":
		return "else"
	default:
		if strings.HasPrefix(owner, "function ") || strings.HasPrefix(owner, "method ") {
			return "function"
		}
		return "if"
	}
}

func (parser *Parser) ifStatement() (IfStatement, error) {
	keyword := parser.advance()
	condition, err := parser.condition("if")
	if err != nil {
		return IfStatement{}, err
	}
	thenBody, err := parser.block("if", lexer.ElseIf, lexer.Else, lexer.End)
	if err != nil {
		return IfStatement{}, err
	}

	branches := make([]ElseIfClause, 0)
	for parser.check(lexer.ElseIf) {
		branchKeyword := parser.advance()
		branchCondition, branchErr := parser.condition("elseif")
		if branchErr != nil {
			return IfStatement{}, branchErr
		}
		branchBody, bodyErr := parser.block("elseif", lexer.ElseIf, lexer.Else, lexer.End)
		if bodyErr != nil {
			return IfStatement{}, bodyErr
		}
		branches = append(branches, ElseIfClause{
			Keyword:   branchKeyword,
			Condition: branchCondition,
			Body:      branchBody,
		})
	}

	var elseBody []Statement
	var elseKeyword lexer.Token
	if parser.check(lexer.Else) {
		elseKeyword = parser.advance()
		elseBody, err = parser.block("else", lexer.End)
		if err != nil {
			return IfStatement{}, err
		}
	}
	end, err := parser.consume(lexer.End, "'end' to close if statement")
	if err != nil {
		return IfStatement{}, err
	}
	return IfStatement{
		Keyword:     keyword,
		Condition:   condition,
		Then:        thenBody,
		ElseIf:      branches,
		Else:        elseBody,
		ElseKeyword: elseKeyword,
		End:         end,
	}, nil
}

func (parser *Parser) whileStatement() (WhileStatement, error) {
	keyword := parser.advance()
	condition, err := parser.condition("while")
	if err != nil {
		return WhileStatement{}, err
	}
	// RFC 0028: `do` is the mandatory boundary between the header and body.
	if _, err := parser.consume(lexer.Do, "expected 'do' after while condition"); err != nil {
		return WhileStatement{}, err
	}
	body, err := parser.block("while", lexer.End)
	if err != nil {
		return WhileStatement{}, err
	}
	end, err := parser.consume(lexer.End, "'end' to close while statement")
	if err != nil {
		return WhileStatement{}, err
	}
	return WhileStatement{Keyword: keyword, Condition: condition, Body: body, End: end}, nil
}

// forStatement parses the RFC 0028 for-in form: a comma-separated binder list,
// `in`, one iterable source expression, `do`, a body, and `end`.
func (parser *Parser) forStatement() (ForStatement, error) {
	keyword := parser.advance()
	binders := make([]lexer.Token, 0, 3)
	for {
		name, err := parser.consume(lexer.Identifier, "a loop binder name after 'for'")
		if err != nil {
			return ForStatement{}, err
		}
		binders = append(binders, name)
		if !parser.check(lexer.Comma) {
			break
		}
		parser.advance()
		if len(binders) == 3 {
			return ForStatement{}, parser.errorAt(parser.peek(), "a for-in loop takes at most 3 binders")
		}
	}
	if len(binders) < 1 {
		return ForStatement{}, parser.errorAt(keyword, "a for-in loop needs at least one binder")
	}
	if _, err := parser.consume(lexer.In, "expected 'in' after loop binders"); err != nil {
		return ForStatement{}, err
	}
	source, err := parser.expression()
	if err != nil {
		return ForStatement{}, err
	}
	if _, err := parser.consume(lexer.Do, "expected 'do' after for source"); err != nil {
		return ForStatement{}, err
	}
	body, err := parser.block("for", lexer.End)
	if err != nil {
		return ForStatement{}, err
	}
	end, err := parser.consume(lexer.End, "'end' to close for statement")
	if err != nil {
		return ForStatement{}, err
	}
	return ForStatement{Keyword: keyword, Binders: binders, Source: source, Body: body, End: end}, nil
}

// condition gives control-flow headers a construct-specific diagnostic when
// the delimiter follows immediately, instead of exposing expression recovery
// as a misleading generic "expected a value" error.
func (parser *Parser) condition(keyword string) (Expression, error) {
	switch parser.peek().Kind {
	case lexer.EOF, lexer.ElseIf, lexer.Else, lexer.End:
		return nil, parser.errorAtCurrent("expected a condition after '" + keyword + "'")
	default:
		return parser.expression()
	}
}

func (parser *Parser) declaration(name lexer.Token, mutable bool) (Declaration, error) {
	if _, err := parser.consume(lexer.Colon, "':'"); err != nil {
		return Declaration{}, err
	}
	typeExpression, err := parser.typeExpression()
	if err != nil {
		return Declaration{}, err
	}
	if _, err := parser.consume(lexer.Equal, "'='"); err != nil {
		return Declaration{}, err
	}
	initializer, err := parser.expression()
	if err != nil {
		return Declaration{}, err
	}

	return Declaration{
		Name:        name,
		Mutable:     mutable,
		Type:        typeExpression,
		Initializer: initializer,
	}, nil
}
