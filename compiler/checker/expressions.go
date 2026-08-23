package checker

import (
	"fmt"
	"go/constant"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

type initializerValue = checkedExpression

func initializerDiagnostics(initializer initializerValue) compilerTypes.Diagnostics {
	if len(initializer.diagnostics) > 0 {
		return initializer.diagnostics
	}
	if initializer.diagnostic != nil {
		return compilerTypes.Diagnostics{*initializer.diagnostic}
	}
	return nil
}

type expressionContext struct {
	expected           compilerTypes.TypeUse
	foldConstants      bool
	inCleanup          bool // checking a defer or errdefer action expression
	allowStandaloneNil bool // print arguments admit standalone Nil
}

type expressionTypeHint struct {
	typ        compilerTypes.Type
	contextual bool
	token      lexer.Token
	diagnostic *compilerTypes.Diagnostic
}

type checkedExpression struct {
	source      Operand
	typ         compilerTypes.Type
	use         compilerTypes.TypeUse
	storageType compilerTypes.Type
	variant     *compilerTypes.AdtVariant
	token       lexer.Token
	diagnostic  *compilerTypes.Diagnostic
	diagnostics compilerTypes.Diagnostics
	known       *Operand
	function    bool // the name of a declared function, which is not storage
	parameter   bool // a fixed function parameter binding
	self        bool // the implicit method receiver, a fixed binding
	loopBinder  bool // a for-in binder: fresh and immutable
}

// checkInitializer resolves a syntax expression into one checked operand.
// Numeric literals use the expected primitive type as context; operation trees
// carry that context only into untyped literals.
func checkInitializer(initializer parser.Expression, expectedUse compilerTypes.TypeUse, fallback lexer.Token, names *scope, typeEnvironment *compilerTypes.Environment) initializerValue {
	// Initializers are the boundary where exact constants can be retained. A
	// later mutable read still becomes an expression through valueFromPlace.
	checked := checkExpression(initializer, expressionContext{expected: expectedUse, foldConstants: true}, names, typeEnvironment)
	if len(initializerDiagnostics(checked)) == 0 && expectedUse.Type.Name != "" && compilerTypes.IsUnion(expectedUse.Type) && !compilerTypes.Equal(expectedUse.Type, checked.typ) {
		checked = injectIntoUnion(checked, expectedUse.Type)
	}
	if checked.token.Line == 0 {
		checked.token = fallback
	}
	return checked
}

func checkObjectLiteral(expression parser.ObjectLiteral, expectedType compilerTypes.Type, names *scope, typeEnvironment *compilerTypes.Environment) initializerValue {
	literalType, ok := typeEnvironment.Lookup(expression.TypeName.Lexeme)
	if compilerTypes.IsError(literalType) {
		// Error is built-in and constructed only through
		// Error.new(header, message); a raw object initializer is rejected.
		return initializerValue{token: expression.TypeName, diagnostic: diagnosticAt(typeErrorAt(expression.TypeName, "Error must be created with Error.new(header, message)"))}
	}
	if !ok && names.generics != nil {
		// A generic object literal names an open generic template. With
		// explicit type arguments it specializes directly; otherwise the
		// arguments are inferred from the expected destination type when it
		// is a specialization of the same template.
		if open, generic := names.generics.types[expression.TypeName.Lexeme]; generic {
			if len(expression.TypeArguments) > 0 {
				specializedUse, diagnostic := specializeTypeUse(parser.GenericTypeExpression{Name: expression.TypeName, Arguments: expression.TypeArguments}, expression.TypeName, typeEnvironment, names.generics)
				if diagnostic != nil {
					return initializerValue{token: expression.TypeName, diagnostic: diagnostic}
				}
				literalType = specializedUse.Type
				ok = literalType.Object != nil
			} else if expectedType.Object != nil {
				if expectedOpen := names.generics.objectOpen[expectedType.Object]; expectedOpen == open {
					specializedUse, diagnostic := specializeTypeUseArguments(open, names.generics.objectArguments[expectedType.Object], expression.TypeName, typeEnvironment, names.generics)
					if diagnostic != nil {
						return initializerValue{token: expression.TypeName, diagnostic: diagnostic}
					}
					literalType = specializedUse.Type
					ok = literalType.Object != nil
				}
			}
			if !ok {
				return initializerValue{token: expression.TypeName, diagnostic: diagnosticAt(typeErrorAt(expression.TypeName, fmt.Sprintf("cannot infer generic parameter for %s", expression.TypeName.Lexeme)))}
			}
		}
	}
	if !ok {
		return initializerValue{token: expression.TypeName, diagnostic: diagnosticAt(typeErrorAt(expression.TypeName, "unknown type "+expression.TypeName.Lexeme))}
	}
	if literalType.Object == nil {
		return initializerValue{typ: literalType, token: expression.TypeName, diagnostic: diagnosticAt(typeErrorAt(expression.TypeName, expression.TypeName.Lexeme+" is not an object type"))}
	}
	if expectedType.Name != "" && !compilerTypes.Assignable(expectedType, literalType) {
		return initializerValue{typ: literalType, token: expression.TypeName, diagnostic: diagnosticAt(typeErrorAt(expression.TypeName, fmt.Sprintf("expected %s; got %s", expectedType.Name, literalType.Name)))}
	}

	values := make([]ObjectMemberValue, 0, len(expression.Initializers))
	seen := make(map[string]bool, len(expression.Initializers))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, initializer := range expression.Initializers {
		member, exists := literalType.Object.Member(initializer.Name.Lexeme)
		if !exists {
			diagnostics = append(diagnostics, typeErrorAt(initializer.Name, fmt.Sprintf("%s has no member %s", literalType.Name, initializer.Name.Lexeme)))
			continue
		}
		if seen[member.Name] {
			diagnostics = append(diagnostics, typeErrorAt(initializer.Name, fmt.Sprintf("%s literal initializes member %s more than once", literalType.Name, member.Name)))
			continue
		}
		seen[member.Name] = true
		memberUse := member.Use
		if memberUse.Type == (compilerTypes.Type{}) {
			memberUse = compilerTypes.NewTypeUse(member.Type)
		}
		checked := checkInitializer(initializer.Value, memberUse, initializer.Name, names, typeEnvironment)
		if nestedDiagnostics := initializerDiagnostics(checked); len(nestedDiagnostics) > 0 {
			diagnostics = append(diagnostics, nestedDiagnostics...)
			continue
		}
		if !assignable(member.Type, checked.typ) {
			diagnostics = append(diagnostics, typeMismatchDiagnostic(member.Type, checked.typ, checked.token))
			continue
		}
		values = append(values, ObjectMemberValue{Member: member, Source: checked.source})
	}
	for index := range literalType.Object.Members {
		member := &literalType.Object.Members[index]
		if !seen[member.Name] {
			diagnostics = append(diagnostics, typeErrorAt(expression.TypeName, fmt.Sprintf("%s literal is missing member %s", literalType.Name, member.Name)))
		}
	}
	value := &ObjectValue{Type: literalType, Initializers: values}
	return initializerValue{
		source:      Operand{Kind: ObjectOperand, Type: literalType, Object: value, Node: Expression{Kind: ObjectExpression, Object: value}},
		typ:         literalType,
		token:       expression.TypeName,
		diagnostics: diagnostics,
		diagnostic: func() *compilerTypes.Diagnostic {
			if len(diagnostics) == 0 {
				return nil
			}
			return &diagnostics[0]
		}(),
	}
}

// checkValue resolves an expression in value context. Assignment and ref call
// checkPlace instead to retain place mode.
func checkValue(expression parser.Expression, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	return checkExpression(expression, expressionContext{}, names, typeEnvironment)
}

func checkExpression(expression parser.Expression, context expressionContext, names *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	if len(context.expected.Candidates) > 1 && isContextualExpression(expression) {
		return checkContextualUnion(expression, context.expected, names, typeEnvironment)
	}
	switch expression := expression.(type) {
	case parser.AnonymousFunctionLiteral:
		return checkAnonymousFunctionLiteral(expression, context, names, typeEnvironment)
	case parser.IntegerLiteral:
		return checkedFromInitializer(integerInitializer(expression.Token, contextualIntegerType(context.expected.Type), false))
	case parser.DecimalLiteral:
		return checkedFromInitializer(floatInitializer(expression.Token, contextualFloatType(context.expected.Type), false))
	case parser.NegatedNumericLiteral:
		return checkedFromInitializer(negatedInitializer(expression, context.expected.Type))
	case parser.BooleanLiteral:
		source := constantOperand(compilerTypes.Bool, constant.MakeBool(expression.Token.Kind == lexer.True), expression.Token.Lexeme)
		source.Node = constantNode(source)
		return checkedExpression{source: source, typ: compilerTypes.Bool, token: expression.Token, known: &source}
	case parser.NilLiteral:
		source := nilOperand(expression.Token.Lexeme)
		known := source
		expected := context.expected.Type
		// The nil literal requires a contextual union containing Nil, except
		// as a print argument (allowStandaloneNil), which is the sole position
		// admitting standalone Nil. A Nil expected type arises only from the
		// nil == nil / nil != nil equality path.
		if context.allowStandaloneNil || compilerTypes.IsNil(expected) ||
			(compilerTypes.IsUnion(expected) && compilerTypes.ContainsUnionMember(expected, compilerTypes.Nil)) {
			return checkedExpression{source: source, typ: compilerTypes.Nil, token: expression.Token, known: &known}
		}
		diagnostic := typeErrorAt(expression.Token, "nil requires an expected union containing Nil")
		return checkedExpression{token: expression.Token, diagnostic: &diagnostic}
	case parser.EosLiteral:
		source := eosOperand(expression.Token.Lexeme)
		known := source
		return checkedExpression{source: source, typ: compilerTypes.EoS, token: expression.Token, known: &known}
	case parser.StringLiteral:
		return checkStringLiteral(expression, context.expected.Type)
	case parser.ByteLiteral:
		return checkByteLiteral(expression)
	case parser.RuneLiteral:
		return checkRuneLiteral(expression)
	case parser.ObjectLiteral:
		return checkObjectLiteral(expression, context.expected.Type, names, typeEnvironment)
	case parser.ArrayLiteralExpression:
		return checkArrayLiteral(expression, context.expected.Type, names, typeEnvironment)
	case parser.QualifiedVariantExpression:
		return checkQualifiedVariant(expression, context.expected.Type, names, typeEnvironment)
	case parser.MatchExpression:
		return checkMatchExpression(expression, context, names, typeEnvironment)
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		if property, isProperty := expression.(parser.PropertyExpression); isProperty {
			if variable, isVariable := property.Receiver.(parser.VariableExpression); isVariable && property.Property.Kind == lexer.Identifier {
				if reference, diagnostic := checkUnitVariant(variable.Name, property.Property, context.expected.Type, names, typeEnvironment); reference != nil || diagnostic != nil {
					if diagnostic != nil {
						return checkedExpression{token: property.Property, diagnostic: diagnostic}
					}
					return *reference
				}
			}
		}
		if variable, isVariable := expression.(parser.VariableExpression); isVariable && context.expected.Type.Signature != nil {
			if reference, diagnostic := checkGenericFunctionReference(variable.Name, context.expected.Type, names, typeEnvironment); reference != nil || diagnostic != nil {
				if diagnostic != nil {
					return checkedExpression{token: variable.Name, diagnostic: diagnostic}
				}
				return *reference
			}
		}
		place := checkPlace(expression, names, typeEnvironment)
		if place.diagnostic != nil {
			return place
		}
		return valueFromPlace(place)
	case parser.RefExpression:
		return checkReference(expression, names, typeEnvironment)
	case parser.CallExpression:
		return checkCallValue(expression, names, typeEnvironment)
	case parser.UnaryExpression:
		return checkUnaryExpression(expression, context, names, typeEnvironment)
	case parser.SpawnExpression:
		return checkSpawnExpression(expression, names, typeEnvironment)
	case parser.TryExpression:
		return checkTryExpression(expression, context, names, typeEnvironment)
	case parser.BinaryExpression:
		return checkBinaryExpression(expression, context, names, typeEnvironment)
	case parser.TypeTestExpression:
		return checkUnionTypeTest(expression, names, typeEnvironment)
	default:
		return checkedExpression{
			diagnostic: diagnosticAt(unknownAt(lexer.Token{Line: 1, Column: 1}, "unsupported expression")),
		}
	}
}

func checkedFromInitializer(initializer initializerValue) checkedExpression {
	if initializer.known == nil && initializer.source.Kind == ConstantOperand {
		known := initializer.source
		initializer.known = &known
	}
	return initializer
}

func expressionNode(source Operand) Expression {
	if source.Node.Kind != InvalidExpression {
		return source.Node
	}
	return constantNode(source)
}

func variableNodeWithBinding(name string, binding BindingID) Expression {
	return Expression{Kind: VariableExpression, Name: name, Binding: binding}
}

func unaryNode(kind ExpressionKind, operand Expression) Expression {
	return Expression{Kind: kind, Operand: &operand}
}

func memberNode(operand Expression, member *compilerTypes.ObjectMember) Expression {
	node := Expression{Kind: MemberExpression, Operand: &operand, Member: member}
	if member != nil && isTrackedCollection(member.Type) {
		node.CollectionRoot = collectionRootOfNode(&operand)
	}
	return node
}
