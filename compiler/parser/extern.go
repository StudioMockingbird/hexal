package parser

// Foreign (C interoperability) blocks. `extern c from <header> do ... end`
// declares the C names and checked Hexal types of one requested header. It is
// the handwritten binding form; an automatic import prepares the same
// declarations as ordinary source. The block is leading: it follows the
// optional import block and precedes every ordinary top-level item.

import (
	"strings"

	"hexal/compiler/lexer"
)

// atExternBlock reports whether the current token opens a foreign block.
// `extern` is contextual and reserved only at statement start.
func (parser *Parser) atExternBlock() bool {
	return parser.atContextual("extern")
}

// atContextual reports whether the current token is the given contextual
// identifier: an ordinary Identifier whose lexeme matches.
func (parser *Parser) atContextual(name string) bool {
	token := parser.peek()
	return token.Kind == lexer.Identifier && token.Lexeme == name
}

// externBlock parses one `extern c from <header> do ... end` block.
func (parser *Parser) externBlock() (ExternBlock, error) {
	keyword := parser.advance()
	c := parser.peek()
	if c.Kind != lexer.Identifier || c.Lexeme != "c" {
		return ExternBlock{}, parser.errorAtCurrent("expected 'c' after 'extern'")
	}
	parser.advance()
	from := parser.peek()
	if from.Kind != lexer.Identifier || from.Lexeme != "from" {
		return ExternBlock{}, parser.errorAtCurrent("foreign declaration requires a C header")
	}
	parser.advance()
	header, err := parser.cHeaderLiteral(from)
	if err != nil {
		return ExternBlock{}, err
	}
	do := parser.peek()
	if do.Kind != lexer.Do {
		return ExternBlock{}, parser.errorAtCurrent("expected 'do' after the foreign header")
	}
	parser.advance()
	declarations := make([]ExternDeclaration, 0)
	for !parser.check(lexer.End) && !parser.check(lexer.EOF) {
		declaration, declarationErr := parser.externDeclaration()
		if declarationErr != nil {
			return ExternBlock{}, declarationErr
		}
		declarations = append(declarations, declaration)
	}
	end, err := parser.consume(lexer.End, "'end' to close the foreign block")
	if err != nil {
		return ExternBlock{}, err
	}
	return ExternBlock{Keyword: keyword, C: c, From: from, Header: header, Do: do, Declarations: declarations, End: end}, nil
}

// externDeclaration parses one declaration inside a foreign block.
func (parser *Parser) externDeclaration() (ExternDeclaration, error) {
	switch {
	case parser.check(lexer.Type):
		return parser.externType()
	case parser.check(lexer.Fun):
		return parser.externFunction()
	case parser.atContextual("constant"):
		return parser.externConstant()
	case parser.atContextual("global"):
		return parser.externGlobal()
	default:
		return nil, parser.errorAtCurrent("unsupported foreign declaration")
	}
}

// externType parses `type Name [as "C name"] is <alias-target | opaque | struct ... end>`.
func (parser *Parser) externType() (ExternDeclaration, error) {
	keyword := parser.advance()
	name, err := parser.consume(lexer.Identifier, "a foreign type name")
	if err != nil {
		return nil, err
	}
	cName, err := parser.consumeCSpellingAttribute()
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.Is, "'is' in a foreign type declaration"); err != nil {
		return nil, err
	}
	if parser.atContextual("opaque") {
		parser.advance()
		return ExternType{Keyword: keyword, Name: name, CName: cName, Opaque: true}, nil
	}
	if parser.check(lexer.Struct) {
		parser.advance()
		members, end, memberErr := parser.externMembers()
		if memberErr != nil {
			return nil, memberErr
		}
		return ExternType{Keyword: keyword, Name: name, CName: cName, Members: members, End: end}, nil
	}
	target, err := parser.aliasTarget()
	if err != nil {
		return nil, err
	}
	return ExternType{Keyword: keyword, Name: name, CName: cName, Alias: target}, nil
}

// externMembers parses a complete foreign struct's member list through its
// closing `end`.
func (parser *Parser) externMembers() ([]ExternMember, lexer.Token, error) {
	members := make([]ExternMember, 0)
	for !parser.check(lexer.End) && !parser.check(lexer.EOF) {
		mutable := false
		if parser.check(lexer.Mut) {
			parser.advance()
			mutable = true
		}
		name, err := parser.consume(lexer.Identifier, "a foreign member name")
		if err != nil {
			return nil, lexer.Token{}, err
		}
		cName, err := parser.consumeCSpellingAttribute()
		if err != nil {
			return nil, lexer.Token{}, err
		}
		if _, err := parser.consume(lexer.Colon, "':' after a foreign member name"); err != nil {
			return nil, lexer.Token{}, err
		}
		memberType, err := parser.typeExpression()
		if err != nil {
			return nil, lexer.Token{}, err
		}
		members = append(members, ExternMember{Mutable: mutable, Name: name, CName: cName, Type: memberType})
		if parser.check(lexer.Comma) {
			parser.advance()
			if parser.check(lexer.End) {
				break
			}
			continue
		}
		break
	}
	end, err := parser.consume(lexer.End, "'end' to close the foreign struct")
	if err != nil {
		return nil, lexer.Token{}, err
	}
	return members, end, nil
}

// externFunction parses `fun Name [as "C name"](params) [: Type [as "C type"]]`.
func (parser *Parser) externFunction() (ExternDeclaration, error) {
	keyword := parser.advance()
	name, err := parser.consume(lexer.Identifier, "a foreign function name")
	if err != nil {
		return nil, err
	}
	cName, err := parser.consumeCSpellingAttribute()
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.LeftParen, "'(' after a foreign function name"); err != nil {
		return nil, err
	}
	parameters := make([]ExternParameter, 0)
	if !parser.check(lexer.RightParen) {
		for {
			parameterName, err := parser.consume(lexer.Identifier, "a foreign parameter name")
			if err != nil {
				return nil, err
			}
			if _, err := parser.consume(lexer.Colon, "':' after a foreign parameter name"); err != nil {
				return nil, err
			}
			parameterType, err := parser.typeExpression()
			if err != nil {
				return nil, err
			}
			parameterCType, err := parser.consumeCSpellingAttribute()
			if err != nil {
				return nil, err
			}
			parameters = append(parameters, ExternParameter{Name: parameterName, Type: parameterType, CType: parameterCType})
			if parser.check(lexer.Comma) {
				parser.advance()
				if parser.check(lexer.RightParen) {
					break
				}
				continue
			}
			break
		}
	}
	if _, err := parser.consume(lexer.RightParen, "')' after foreign parameters"); err != nil {
		return nil, err
	}
	declaration := ExternFunction{Keyword: keyword, Name: name, CName: cName, Parameters: parameters}
	if parser.check(lexer.Colon) {
		parser.advance()
		result, err := parser.typeExpression()
		if err != nil {
			return nil, err
		}
		resultCType, err := parser.consumeCSpellingAttribute()
		if err != nil {
			return nil, err
		}
		declaration.Result = result
		declaration.ResultCType = resultCType
	}
	return declaration, nil
}

// externConstant parses `constant Name [as "C name"] : Type`.
func (parser *Parser) externConstant() (ExternDeclaration, error) {
	keyword := parser.advance()
	name, err := parser.consume(lexer.Identifier, "a foreign constant name")
	if err != nil {
		return nil, err
	}
	cName, err := parser.consumeCSpellingAttribute()
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.Colon, "':' after a foreign constant name"); err != nil {
		return nil, err
	}
	constantType, err := parser.typeExpression()
	if err != nil {
		return nil, err
	}
	return ExternConstant{Keyword: keyword, Name: name, CName: cName, Type: constantType}, nil
}

// externGlobal parses `global [mut] Name [as "C name"] : Type`.
func (parser *Parser) externGlobal() (ExternDeclaration, error) {
	keyword := parser.advance()
	mutable := false
	if parser.check(lexer.Mut) {
		parser.advance()
		mutable = true
	}
	name, err := parser.consume(lexer.Identifier, "a foreign global name")
	if err != nil {
		return nil, err
	}
	cName, err := parser.consumeCSpellingAttribute()
	if err != nil {
		return nil, err
	}
	if _, err := parser.consume(lexer.Colon, "':' after a foreign global name"); err != nil {
		return nil, err
	}
	globalType, err := parser.typeExpression()
	if err != nil {
		return nil, err
	}
	return ExternGlobal{Keyword: keyword, Mutable: mutable, Name: name, CName: cName, Type: globalType}, nil
}

// consumeCSpellingAttribute consumes an optional `as "C spelling"` and returns
// its token, validating the restricted spelling the position allows. A missing
// attribute yields a nil token.
func (parser *Parser) consumeCSpellingAttribute() (*lexer.Token, error) {
	if !parser.check(lexer.As) {
		return nil, nil
	}
	parser.advance()
	literal, err := parser.consume(lexer.StringLiteral, "a quoted C spelling after 'as'")
	if err != nil {
		return nil, err
	}
	spelling := trimStringLiteral(literal.Lexeme)
	if !validCSpelling(spelling) {
		return nil, parser.errorAt(literal, "invalid C spelling "+spelling)
	}
	return &literal, nil
}

// trimStringLiteral strips the surrounding quotes from a string-literal token's
// raw spelling. C spellings contain no escapes, so no decoding is needed.
func trimStringLiteral(lexeme string) string {
	if len(lexeme) >= 2 && strings.HasPrefix(lexeme, "\"") && strings.HasSuffix(lexeme, "\"") {
		return lexeme[1 : len(lexeme)-1]
	}
	return lexeme
}

// validCSpelling reports whether a quoted C spelling is one identifier, one
// `struct X`/`union X` tag, or a pointer/scalar type spelling without the
// declarator syntax this version rejects (arrays, function declarators,
// initializers, attributes, and expression punctuation).
func validCSpelling(spelling string) bool {
	if spelling == "" {
		return false
	}
	for _, character := range spelling {
		switch character {
		case '[', ']', '(', ')', '{', '}', '=', '#', ';', ',', '\n', '\r', '\t':
			return false
		}
	}
	return true
}

// cHeaderLiteral consumes one C header literal and builds the C-header import
// reference. keyword anchors a missing-header diagnostic.
func (parser *Parser) cHeaderLiteral(keyword lexer.Token) (ImportReference, error) {
	if !parser.check(lexer.CHeaderLiteral) {
		return ImportReference{}, parser.errorAtCurrent("foreign declaration requires a C header")
	}
	literal := parser.advance()
	system := strings.HasPrefix(literal.Lexeme, "<")
	payload := literal.Lexeme
	if len(payload) >= 2 {
		if (strings.HasPrefix(payload, "<") && strings.HasSuffix(payload, ">")) ||
			(strings.HasPrefix(payload, "\"") && strings.HasSuffix(payload, "\"")) {
			payload = payload[1 : len(payload)-1]
		}
	}
	if !validCHeaderName(payload) {
		return ImportReference{}, parser.errorAt(literal, "invalid C header name "+payload)
	}
	return ImportReference{
		Kind:            CHeaderImportReference,
		Token:           keyword,
		DisplaySpelling: literal.Lexeme,
		CHeader:         payload,
		System:          system,
	}, nil
}
