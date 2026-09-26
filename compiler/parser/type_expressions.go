package parser

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
)

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

// QualifiedGenericTypeExpression refers to an exported generic type of an
// imported module with concrete type arguments: Alias.Name<Arguments>.
// Unlike QualifiedTypeExpression's dotted chain, exactly one alias and one
// exported declaration name are carried; the qualified generic form is
// not an arbitrary dotted path.
type QualifiedGenericTypeExpression struct {
	Module    lexer.Token
	Name      lexer.Token
	Arguments []TypeExpression
}

func (QualifiedGenericTypeExpression) typeExpressionNode() {}

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

// PtrTypeExpression describes the Ptr<T> and Ptr<mut T> type constructors.
// Writable distinguishes the writable-pointee form from the read-only
// pointee form; the element is a syntax node so type resolution remains
// the checker's responsibility.
type PtrTypeExpression struct {
	Keyword  lexer.Token
	Element  TypeExpression
	Writable bool
}

func (PtrTypeExpression) typeExpressionNode() {}

// SliceTypeExpression describes the Slice<T> and Slice<mut T> type
// constructors. Writable selects the writable-element mode; like Ptr, the
// element is a syntax node for the checker to resolve.
type SliceTypeExpression struct {
	Keyword  lexer.Token
	Element  TypeExpression
	Writable bool
}

func (SliceTypeExpression) typeExpressionNode() {}

// StringTypeExpression names the inline text type String<N>. Arguments are
// kept as written so the checker owns every capacity diagnostic: exactly one
// positive decimal literal is valid, and anything else is reported there.
type StringTypeExpression struct {
	Keyword   lexer.Token
	Arguments []TypeExpression
}

func (StringTypeExpression) typeExpressionNode() {}

// LiteralTypeArgument is a numeric literal in type-argument position: the
// capacity of String<N> or of a widen<M>() call. It is not a type; every
// consumer other than a capacity position rejects it through type resolution.
type LiteralTypeArgument struct {
	Token lexer.Token
}

func (LiteralTypeArgument) typeExpressionNode() {}

// MutTypeArgument marks one `mut T` call-site type argument, as in
// Slice<mut T>.from_pointer(...). Only the Slice bridge consumes the
// marking; every other generic consumer rejects it through type resolution.
type MutTypeArgument struct {
	Mut  lexer.Token
	Type TypeExpression
}

func (MutTypeArgument) typeExpressionNode() {}

// FunctionTypeExpression is the written Fun<(T, U) : R> type. Return is nil for
// the no-return form Fun<(T)>. The parser only records the shape; whether the
// position is a supported one is the checker's decision.
type FunctionTypeExpression struct {
	Keyword    lexer.Token
	Parameters []TypeExpression
	Return     TypeExpression
	// RestFlags and RestTokens are parallel to Parameters: RestFlags[i] is true
	// when parameter i is written `T...`, and RestTokens[i] carries that
	// ellipsis for diagnostics. A rest flag is valid only on the final
	// parameter.
	RestFlags  []bool
	RestTokens []lexer.Token
}

func (FunctionTypeExpression) typeExpressionNode() {}

func (parser *Parser) typeExpression() (TypeExpression, error) {
	exit, enterErr := parser.enterSyntax()
	defer exit()
	if enterErr != nil {
		return nil, enterErr
	}
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
		return nil, parser.errorAtCurrent(diag.ParserMutInsidePtr())
	}
	name, err := parser.consume(lexer.Identifier, "a type name")
	if err != nil {
		return nil, err
	}
	// A dotted chain in type position is an import-qualified type
	// (Module.Names). The chain is greedy; the method receiver parse peels its
	// final component back into the method name. It is suppressed inside a
	// method receiver's union members, where the dot is the method delimiter.
	if parser.check(lexer.Dot) && !(parser.methodReceiver && parser.unionMemberDepth > 0) {
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
		// Exactly one alias and one exported declaration name, followed by
		// '<', is the qualified generic type form; a longer dotted chain, or
		// no following '<', stays the ordinary qualified type.
		if len(names) == 1 && parser.check(lexer.Less) {
			arguments, err := parser.genericArgumentList()
			if err != nil {
				return nil, err
			}
			return QualifiedGenericTypeExpression{Module: name, Name: names[0], Arguments: arguments}, nil
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
	if name.Lexeme == "String" && parser.check(lexer.Less) {
		arguments, err := parser.typeArgumentList()
		if err != nil {
			return nil, err
		}
		return StringTypeExpression{Keyword: name, Arguments: arguments}, nil
	}
	if name.Lexeme == "Ptr" {
		if _, err := parser.consume(lexer.Less, "'<'"); err != nil {
			return nil, err
		}
		writable := false
		if parser.check(lexer.Mut) {
			parser.advance()
			writable = true
		}
		element, err := parser.typeExpression()
		if err != nil {
			return nil, err
		}
		if _, err := parser.consumeGenericClose("'>'"); err != nil {
			return nil, err
		}
		return PtrTypeExpression{Keyword: name, Element: element, Writable: writable}, nil
	}
	if name.Lexeme == "Slice" {
		if _, err := parser.consume(lexer.Less, "'<'"); err != nil {
			return nil, err
		}
		writable := false
		if parser.check(lexer.Mut) {
			parser.advance()
			writable = true
		}
		element, err := parser.typeExpression()
		if err != nil {
			return nil, err
		}
		if _, err := parser.consumeGenericClose("'>'"); err != nil {
			return nil, err
		}
		return SliceTypeExpression{Keyword: name, Element: element, Writable: writable}, nil
	}
	if parser.check(lexer.Less) {
		arguments, err := parser.genericArgumentList()
		if err != nil {
			return nil, err
		}
		return GenericTypeExpression{Name: name, Arguments: arguments}, nil
	}
	return NamedTypeExpression{Name: name}, nil
}

// genericArgumentList parses '<' argument (',' argument)* '>', the type
// argument list shared by a local generic type use (Name<Args>) and a
// qualified one (Alias.Name<Args>). The caller has already confirmed '<' is
// next.
func (parser *Parser) genericArgumentList() ([]TypeExpression, error) {
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
	return arguments, nil
}

func (parser *Parser) functionTypeExpression(keyword lexer.Token) (FunctionTypeExpression, error) {
	if _, err := parser.consume(lexer.Less, "'<' after Fun"); err != nil {
		return FunctionTypeExpression{}, err
	}
	if _, err := parser.consume(lexer.LeftParen, "'(' for the Fun parameter types"); err != nil {
		return FunctionTypeExpression{}, err
	}
	parameters := make([]TypeExpression, 0)
	restFlags := make([]bool, 0)
	restTokens := make([]lexer.Token, 0)
	if !parser.check(lexer.RightParen) {
		sawRest := false
		for {
			if sawRest {
				return FunctionTypeExpression{}, parser.errorAtCurrent(diag.ParserRestFinal())
			}
			parameter, err := parser.typeExpression()
			if err != nil {
				return FunctionTypeExpression{}, err
			}
			rest := false
			var ellipsis lexer.Token
			if parser.check(lexer.Ellipsis) {
				ellipsis = parser.advance()
				rest = true
			}
			parameters = append(parameters, parameter)
			restFlags = append(restFlags, rest)
			restTokens = append(restTokens, ellipsis)
			sawRest = rest
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
	return FunctionTypeExpression{Keyword: keyword, Parameters: parameters, Return: returnType, RestFlags: restFlags, RestTokens: restTokens}, nil
}

// typeDefinition is the grammar used after `type Name is`, dispatching on the
// first token by an unambiguous selection table:
//
//	struct                        -> struct definition (may be empty)
//	union <type>                  -> structural-union definition
//	union |                       -> payload-capable ADT definition
//	identifier |                  -> all-unit ADT shorthand
//	otherwise                     -> transparent alias
func (parser *Parser) typeDefinition() (TypeExpression, error) {
	if parser.check(lexer.Struct) {
		return parser.structDefinition()
	}
	if parser.check(lexer.Union) {
		parser.advance()
		if parser.check(lexer.Pipe) {
			return parser.adtDefinition()
		}
		return parser.structuralUnionDefinition()
	}
	if parser.check(lexer.LeftParen) {
		return nil, parser.errorAtCurrent(diag.ParserSumUnionForm())
	}
	if parser.check(lexer.Identifier) && parser.peekAt(1).Kind == lexer.Pipe {
		return parser.unitAdtShorthand()
	}
	target, err := parser.aliasTarget()
	if err != nil {
		return nil, err
	}
	if parser.check(lexer.Pipe) {
		return nil, parser.errorAtCurrent(diag.ParserSumUnionForm())
	}
	return target, nil
}

// aliasTarget parses a plain alias target: a named, generic, array, pointer,
// or function type expression. It never accepts a written top-level union;
// `typeDefinition` rejects one immediately after this returns.
func (parser *Parser) aliasTarget() (TypeExpression, error) {
	return parser.primaryTypeExpression()
}

// structDefinition parses `struct` member-body, including the empty body:
// "struct" , [ member-declaration , { "," , member-declaration } , [ "," ] ] , "end".
func (parser *Parser) structDefinition() (ObjectTypeExpression, error) {
	keyword := parser.advance() // 'struct'
	return parser.objectMemberBody(keyword, true, diag.ParserPayloadNeedsField())
}

// structuralUnionDefinition parses "union" primary-type-expression "|"
// primary-type-expression { "|" primary-type-expression } "end": the named
// structural-sum form. At least one '|' is required.
func (parser *Parser) structuralUnionDefinition() (TypeExpression, error) {
	first, err := parser.primaryTypeExpression()
	if err != nil {
		return nil, err
	}
	if !parser.check(lexer.Pipe) {
		return nil, parser.errorAtCurrent(diag.ParserSumNeedsBar())
	}
	members := []TypeExpression{first}
	pipes := make([]lexer.Token, 0, 1)
	for parser.check(lexer.Pipe) {
		pipes = append(pipes, parser.advance())
		member, err := parser.primaryTypeExpression()
		if err != nil {
			return nil, err
		}
		members = append(members, member)
	}
	if _, err := parser.consume(lexer.End, "'end' after a structural sum declaration"); err != nil {
		return nil, err
	}
	return UnionTypeExpression{Members: members, Pipes: pipes}, nil
}

// adtDefinition parses the payload-capable ADT body introduced by `union`
// directly followed by `|`: adt-variant { adt-variant } "end". A variant is
// an identifier optionally followed by `as ... end` payload fields.
func (parser *Parser) adtDefinition() (AdtDefinitionExpression, error) {
	variants := make([]AdtVariantDeclaration, 0, 2)
	for parser.check(lexer.Pipe) {
		parser.advance()
		name, err := parser.consume(lexer.Identifier, "a variant name after '|'")
		if err != nil {
			return AdtDefinitionExpression{}, err
		}
		variant := AdtVariantDeclaration{Name: name}
		if parser.check(lexer.LeftBrace) {
			return AdtDefinitionExpression{}, parser.errorAtCurrent(diag.ParserAdtPayloadForm())
		}
		if parser.check(lexer.As) {
			asKeyword := parser.advance()
			payload, err := parser.objectMemberBody(asKeyword, false, diag.ParserPayloadNeedsField())
			if err != nil {
				return AdtDefinitionExpression{}, err
			}
			for _, member := range payload.Members {
				if member.Mutable {
					return AdtDefinitionExpression{}, parser.errorAt(member.Name, diag.ParserAdtPayloadMutable())
				}
			}
			variant.Payload = &payload
		}
		variants = append(variants, variant)
	}
	if _, err := parser.consume(lexer.End, "'end' after ADT declaration"); err != nil {
		return AdtDefinitionExpression{}, err
	}
	return AdtDefinitionExpression{Variants: variants}, nil
}

// unitAdtShorthand parses "Identifier | Identifier { | Identifier } end" and
// lowers it to the same AdtDefinitionExpression the long form produces: every
// identifier declares a fresh unit variant, never a reference to an existing
// type of the same spelling.
func (parser *Parser) unitAdtShorthand() (AdtDefinitionExpression, error) {
	variants := make([]AdtVariantDeclaration, 0, 2)
	name, err := parser.consume(lexer.Identifier, "a variant name")
	if err != nil {
		return AdtDefinitionExpression{}, err
	}
	variants = append(variants, AdtVariantDeclaration{Name: name})
	for parser.check(lexer.Pipe) {
		parser.advance()
		name, err := parser.consume(lexer.Identifier, "a variant name after '|'")
		if err != nil {
			return AdtDefinitionExpression{}, err
		}
		variants = append(variants, AdtVariantDeclaration{Name: name})
	}
	if _, err := parser.consume(lexer.End, "'end' after an all-unit ADT declaration"); err != nil {
		return AdtDefinitionExpression{}, err
	}
	return AdtDefinitionExpression{Variants: variants}, nil
}

// objectMemberBody parses the comma-delimited member list shared by struct
// bodies (which may be empty) and ADT payload bodies (which may not): the
// keyword introducing the body has already been consumed. emptyDiagnostic is
// used only when allowEmpty is false and the body is empty.
func (parser *Parser) objectMemberBody(keyword lexer.Token, allowEmpty bool, emptyDiagnostic diag.Message) (ObjectTypeExpression, error) {
	if parser.check(lexer.End) {
		if !allowEmpty {
			return ObjectTypeExpression{}, parser.errorAtCurrent(emptyDiagnostic)
		}
		end := parser.advance()
		return ObjectTypeExpression{Keyword: keyword, End: end}, nil
	}

	members := make([]ObjectMemberDeclaration, 0)
	member, err := parser.objectMemberDeclaration()
	if err != nil {
		return ObjectTypeExpression{}, err
	}
	members = append(members, member)

	for parser.check(lexer.Comma) {
		parser.advance()
		if parser.check(lexer.End) {
			break
		}
		member, err := parser.objectMemberDeclaration()
		if err != nil {
			return ObjectTypeExpression{}, err
		}
		members = append(members, member)
	}

	end, err := parser.consume(lexer.End, "'end' after a member list")
	if err != nil {
		return ObjectTypeExpression{}, err
	}
	return ObjectTypeExpression{
		Keyword: keyword,
		Members: members,
		End:     end,
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
