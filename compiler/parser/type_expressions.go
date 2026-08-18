package parser

import "hexal/compiler/lexer"

// TypeExpression is the syntax-tree root for a declared type.
//
// Keeping type syntax separate from value expressions leaves a clear extension
// point for sum types and other type forms without complicating expression
// parsing.
type TypeExpression interface {
	typeExpressionNode()
}

// NamedTypeExpression refers to a type by name. The checker resolves the name
// against the types available in the current compilation environment.
type NamedTypeExpression struct {
	Name lexer.Token
}

func (NamedTypeExpression) typeExpressionNode() {}

// QualifiedTypeExpression refers to a type through an import alias:
// Module.Names is a dotted chain whose first component is the alias. Names
// always has at least one element; the checker resolves the chain inside the
// imported module.
type QualifiedTypeExpression struct {
	Module lexer.Token
	Names  []lexer.Token
}

func (QualifiedTypeExpression) typeExpressionNode() {}

// GenericTypeExpression names a user generic type with concrete arguments.
type GenericTypeExpression struct {
	Name      lexer.Token
	Arguments []TypeExpression
}

func (GenericTypeExpression) typeExpressionNode() {}

// NilTypeExpression names the singleton Nil type. The checker owns its
// semantic identity and value restrictions.
type NilTypeExpression struct {
	Token lexer.Token
}

func (NilTypeExpression) typeExpressionNode() {}

// UnknownTypeExpression names the incomplete Unknown type. The checker owns
// the restriction that it may only appear behind a pointer constructor.
type UnknownTypeExpression struct {
	Token lexer.Token
}

func (UnknownTypeExpression) typeExpressionNode() {}

// PtrTypeExpression describes the Ptr<T> and MutPtr<T> type constructors.
// Writable distinguishes the writable-pointee constructor from the read-only
// pointee constructor; the element is a syntax node so type resolution remains
// the checker's responsibility.
type PtrTypeExpression struct {
	Keyword  lexer.Token
	Element  TypeExpression
	Writable bool
}

func (PtrTypeExpression) typeExpressionNode() {}

// FunctionTypeExpression is the written Fun<(T, U) : R> type. Return is nil for
// the no-return form Fun<(T)>. The parser only records the shape; whether the
// position is a supported one is the checker's decision.
type FunctionTypeExpression struct {
	Keyword    lexer.Token
	Parameters []TypeExpression
	Return     TypeExpression
}

func (FunctionTypeExpression) typeExpressionNode() {}

func (parser *Parser) typeExpression() (TypeExpression, error) {
	first, err := parser.primaryTypeExpression()
	if err != nil {
		return nil, err
	}
	// When a `>>` token was split and one closer is still pending, that
	// closer belongs to an enclosing generic constructor; a `|` here must
	// not extend this union past it.
	if parser.pendingGreater || !parser.check(lexer.Pipe) {
		return first, nil
	}

	members := []TypeExpression{first}
	pipes := make([]lexer.Token, 0, 1)
	for parser.check(lexer.Pipe) {
		pipes = append(pipes, parser.advance())
		parser.unionMemberDepth++
		member, err := parser.primaryTypeExpression()
		parser.unionMemberDepth--
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	return UnionTypeExpression{Members: members, Pipes: pipes}, nil
}

// UnionTypeExpression preserves written member order and pipe locations. The
// checker normalizes the members for canonical identity while retaining this
// order for contextual candidate selection.
type UnionTypeExpression struct {
	Members []TypeExpression
	Pipes   []lexer.Token
}

func (UnionTypeExpression) typeExpressionNode() {}

// GroupedTypeExpression preserves explicit type grouping without changing the
// canonical type identity of its inner expression.
type GroupedTypeExpression struct {
	OpenParen lexer.Token
	Inner     TypeExpression
}

func (GroupedTypeExpression) typeExpressionNode() {}

// ArrayTypeExpression names the fixed inline array type Array<T, N>. The
// length is a positive decimal integer literal in the source.
type ArrayTypeExpression struct {
	Keyword lexer.Token
	Element TypeExpression
	Length  lexer.Token
}

func (ArrayTypeExpression) typeExpressionNode() {}

func (parser *Parser) primaryTypeExpression() (TypeExpression, error) {
	if parser.check(lexer.LeftParen) {
		open := parser.advance()
		inner, err := parser.typeExpression()
		if err != nil {
			return nil, err
		}
		_, err = parser.consume(lexer.RightParen, "')' after a grouped type expression")
		if err != nil {
			return nil, err
		}
		return GroupedTypeExpression{OpenParen: open, Inner: inner}, nil
	}
	if parser.check(lexer.Mut) {
		return nil, parser.errorAtCurrent("mut is not allowed inside Ptr<...>; use MutPtr<...>")
	}
	name, err := parser.consume(lexer.Identifier, "a type name")
	if err != nil {
		return nil, err
	}
	// A dotted chain in type position is an import-qualified type
	// (Module.Names). The chain is greedy; the impl receiver parse peels its
	// final component back into the method name. It is suppressed inside an
	// impl receiver's union members, where the dot is the method delimiter.
	if parser.check(lexer.Dot) && !(parser.implReceiver && parser.unionMemberDepth > 0) {
		names := make([]lexer.Token, 0, 1)
		for {
			if _, err := parser.consume(lexer.Dot, "'.' after a type name"); err != nil {
				return nil, err
			}
			component, err := parser.consume(lexer.Identifier, "a type name after '.'")
			if err != nil {
				return nil, err
			}
			names = append(names, component)
			if !parser.check(lexer.Dot) {
				break
			}
		}
		return QualifiedTypeExpression{Module: name, Names: names}, nil
	}
	switch name.Lexeme {
	case "Nil":
		return NilTypeExpression{Token: name}, nil
	case "Unknown":
		return UnknownTypeExpression{Token: name}, nil
	}
	// `Fun` is an ordinary identifier lexeme rather than a keyword, so it is
	// matched the same way the pointer constructors are.
	if name.Lexeme == "Fun" {
		return parser.functionTypeExpression(name)
	}
	if name.Lexeme == "Array" {
		if _, err := parser.consume(lexer.Less, "'<' after Array"); err != nil {
			return nil, err
		}
		element, err := parser.typeExpression()
		if err != nil {
			return nil, err
		}
		if _, err := parser.consume(lexer.Comma, "',' after the array element type"); err != nil {
			return nil, err
		}
		length, err := parser.consume(lexer.Integer, "a positive decimal array length")
		if err != nil {
			return nil, err
		}
		if _, err := parser.consumeGenericClose("'>' after the array length"); err != nil {
			return nil, err
		}
		return ArrayTypeExpression{Keyword: name, Element: element, Length: length}, nil
	}
	if name.Lexeme == "Ptr" || name.Lexeme == "MutPtr" {
		if _, err := parser.consume(lexer.Less, "'<'"); err != nil {
			return nil, err
		}
		element, err := parser.typeExpression()
		if err != nil {
			return nil, err
		}
		if _, err := parser.consumeGenericClose("'>'"); err != nil {
			return nil, err
		}
		return PtrTypeExpression{Keyword: name, Element: element, Writable: name.Lexeme == "MutPtr"}, nil
	}
	if parser.check(lexer.Less) {
		parser.advance()
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
		if _, err := parser.consumeGenericClose("'>' after a generic type argument list"); err != nil {
			return nil, err
		}
		return GenericTypeExpression{Name: name, Arguments: arguments}, nil
	}
	return NamedTypeExpression{Name: name}, nil
}

func (parser *Parser) functionTypeExpression(keyword lexer.Token) (FunctionTypeExpression, error) {
	if _, err := parser.consume(lexer.Less, "'<' after Fun"); err != nil {
		return FunctionTypeExpression{}, err
	}
	if _, err := parser.consume(lexer.LeftParen, "'(' for the Fun parameter types"); err != nil {
		return FunctionTypeExpression{}, err
	}
	parameters := make([]TypeExpression, 0)
	if !parser.check(lexer.RightParen) {
		for {
			parameter, err := parser.typeExpression()
			if err != nil {
				return FunctionTypeExpression{}, err
			}
			parameters = append(parameters, parameter)
			if !parser.check(lexer.Comma) {
				break
			}
			parser.advance()
		}
	}
	if _, err := parser.consume(lexer.RightParen, "')' after the Fun parameter types"); err != nil {
		return FunctionTypeExpression{}, err
	}
	var returnType TypeExpression
	if parser.check(lexer.Colon) {
		parser.advance()
		var err error
		returnType, err = parser.typeExpression()
		if err != nil {
			return FunctionTypeExpression{}, err
		}
	}
	if _, err := parser.consumeGenericClose("'>' to close a Fun type"); err != nil {
		return FunctionTypeExpression{}, err
	}
	return FunctionTypeExpression{Keyword: keyword, Parameters: parameters, Return: returnType}, nil
}

// typeDefinitionExpression is the wider grammar used only after
// `type Name =`. Object type expressions are deliberately not accepted by
// variable annotations or Ptr element positions.
func (parser *Parser) typeDefinitionExpression() (TypeExpression, error) {
	if parser.check(lexer.LeftBrace) {
		return parser.objectTypeExpression()
	}
	if parser.check(lexer.Pipe) {
		return parser.adtDefinitionExpression()
	}
	return parser.typeExpression()
}

// adtDefinitionExpression parses `"|" variant { "|" variant }`. A variant is
// an identifier optionally followed by "as" and a record payload body.
func (parser *Parser) adtDefinitionExpression() (AdtDefinitionExpression, error) {
	variants := make([]AdtVariantDeclaration, 0, 2)
	for parser.check(lexer.Pipe) {
		parser.advance()
		name, err := parser.consume(lexer.Identifier, "a variant name after '|'")
		if err != nil {
			return AdtDefinitionExpression{}, err
		}
		variant := AdtVariantDeclaration{Name: name}
		if parser.check(lexer.As) {
			parser.advance()
			payload, err := parser.objectTypeExpression()
			if err != nil {
				return AdtDefinitionExpression{}, err
			}
			for _, member := range payload.Members {
				if member.Mutable {
					return AdtDefinitionExpression{}, parser.errorAt(member.Name, "ADT payload fields cannot be mutable in this RFC")
				}
			}
			variant.Payload = &payload
		}
		variants = append(variants, variant)
	}
	return AdtDefinitionExpression{Variants: variants}, nil
}

func (parser *Parser) objectTypeExpression() (ObjectTypeExpression, error) {
	openBrace, err := parser.consume(lexer.LeftBrace, "'{' for an object type")
	if err != nil {
		return ObjectTypeExpression{}, err
	}
	if parser.check(lexer.RightBrace) {
		return ObjectTypeExpression{}, parser.errorAtCurrent("an object type must declare at least one member")
	}

	members := make([]ObjectMemberDeclaration, 0)
	member, err := parser.objectMemberDeclaration()
	if err != nil {
		return ObjectTypeExpression{}, err
	}
	members = append(members, member)

	for parser.check(lexer.Comma) {
		parser.advance()
		if parser.check(lexer.RightBrace) {
			break
		}
		member, err := parser.objectMemberDeclaration()
		if err != nil {
			return ObjectTypeExpression{}, err
		}
		members = append(members, member)
	}

	if !parser.check(lexer.RightBrace) {
		return ObjectTypeExpression{}, parser.errorAtCurrent("expected ',' or '}' after an object member")
	}
	closeBrace := parser.advance()
	return ObjectTypeExpression{
		OpenBrace:  openBrace,
		Members:    members,
		CloseBrace: closeBrace,
	}, nil
}

func (parser *Parser) objectMemberDeclaration() (ObjectMemberDeclaration, error) {
	mutable := false
	if parser.check(lexer.Mut) {
		parser.advance()
		mutable = true
	}
	name, err := parser.consume(lexer.Identifier, "an object member name")
	if err != nil {
		return ObjectMemberDeclaration{}, err
	}
	if _, err := parser.consume(lexer.Colon, "':' after an object member name"); err != nil {
		return ObjectMemberDeclaration{}, err
	}
	typeExpression, err := parser.typeExpression()
	if err != nil {
		return ObjectMemberDeclaration{}, err
	}
	return ObjectMemberDeclaration{Name: name, Mutable: mutable, Type: typeExpression}, nil
}
