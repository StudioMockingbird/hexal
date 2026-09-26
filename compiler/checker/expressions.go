package checker

import (
	"go/constant"

	"hexal/compiler/corelib"
	diag "hexal/compiler/diagnostics"
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
func checkInitializer(initializer parser.Expression, expectedUse compilerTypes.TypeUse, fallback lexer.Token, ctx checkContext) initializerValue {
	return checkInitializerRest(initializer, expectedUse, fallback, ctx, false)
}

// checkInitializerRest is checkInitializer with an explicit allowance for a
// rest-backed Slice value. Only a fixed local alias, a non-retaining
// compiler-owned formatting operation, and iteration may consume one; every
// other destination rejects it here.
func checkInitializerRest(initializer parser.Expression, expectedUse compilerTypes.TypeUse, fallback lexer.Token, ctx checkContext, allowRestBacked bool) initializerValue {
	// Initializers are the boundary where exact constants can be retained. A
	// later mutable read still becomes an expression through valueFromPlace.
	checked := checkExpression(initializer, expressionContext{expected: expectedUse, foldConstants: true}, ctx)
	// A value whose type is an open type parameter may become a member of the
	// expected union under some substitution, so union injection and physical
	// reconciliation are deferred rather than rejected. The concrete
	// specialization re-decides both.
	dependentValue := compilerTypes.ContainsTypeParameter(checked.typ)
	if len(initializerDiagnostics(checked)) == 0 && !dependentValue && expectedUse.Type.Name != "" && compilerTypes.IsUnion(expectedUse.Type) && !compilerTypes.Equal(expectedUse.Type, checked.typ) {
		checked = injectIntoUnion(checked, expectedUse.Type)
	}
	if len(initializerDiagnostics(checked)) == 0 && !dependentValue && expectedUse.Type.Name != "" {
		checked = reconcilePhysicalRepresentation(checked, expectedUse.Type)
	}
	if checked.token.Line == 0 {
		checked.token = fallback
	}
	if !allowRestBacked && checked.source.RestBacked && len(initializerDiagnostics(checked)) == 0 {
		diagnostic := messageAt(checked.token, diag.RestBackedSliceCannotEscape())
		checked.diagnostics = append(checked.diagnostics, diagnostic)
		checked.diagnostic = &checked.diagnostics[0]
	}
	return checked
}

// checkStructConstructorCall checks a bare Type(field = value, ...) call
// against a nominal struct's declared members. typeName is the callee's own
// name token; expectedType lets a generic owner's arguments be inferred from
// the construction's destination when the call gives none explicitly.
func checkStructConstructorCall(call parser.CallExpression, typeName lexer.Token, expectedType compilerTypes.Type, ctx checkContext) initializerValue {
	literalType, ok := ctx.typeEnvironment.Lookup(typeName.Lexeme)
	if !ok && ctx.names.generics != nil {
		// A generic constructor call names an open generic template. With
		// explicit type arguments it specializes directly; otherwise the
		// arguments are inferred from the expected destination type when it
		// is a specialization of the same template.
		if open, generic := ctx.names.generics.types[typeName.Lexeme]; generic {
			if len(call.TypeArguments) > 0 {
				specializedUse, diagnostic := specializeTypeUse(parser.GenericTypeExpression{Name: typeName, Arguments: call.TypeArguments}, typeName, ctx.typeEnvironment, ctx.names.generics)
				if diagnostic != nil {
					return initializerValue{token: typeName, diagnostic: diagnostic}
				}
				literalType = specializedUse.Type
				ok = literalType.Object != nil
			} else if expectedType.Object != nil {
				if expectedOpen := ctx.names.generics.objectOpen[expectedType.Object]; expectedOpen == open {
					specializedUse, diagnostic := specializeTypeUseArguments(open, ctx.names.generics.objectArguments[expectedType.Object], typeName, ctx.typeEnvironment, ctx.names.generics)
					if diagnostic != nil {
						return initializerValue{token: typeName, diagnostic: diagnostic}
					}
					literalType = specializedUse.Type
					ok = literalType.Object != nil
				}
			}
			if !ok {
				return initializerValue{token: typeName, diagnostic: diagnosticAt(messageAt(typeName, diag.GenericCallCannotInferParameter(typeName.Lexeme)))}
			}
		}
	}
	if !ok {
		return initializerValue{token: typeName, diagnostic: diagnosticAt(messageAt(typeName, diag.UnknownType(typeName.Lexeme)))}
	}
	return checkObjectConstructorFields(call, typeName, literalType, expectedType, ctx)
}

// liveMemberType re-resolves one declared member's checked type through the
// compilation's arena. A builtin object or variant declared at Go init() time
// (ProcessOptions, Environment's Replace payload) carries List members with a
// fixed, non-arena identity, because List's own interning lives on the
// per-compilation arena and is unreachable at package init(); re-resolution
// recovers the identity a real List<T> binding has, so construction,
// member reads, and equality all agree on one canonical type. For a member
// already declared against this arena it is a cache hit returning the
// identical type unchanged.
func liveMemberType(memberType compilerTypes.Type, typeEnvironment *compilerTypes.Environment) compilerTypes.Type {
	if memberType.List == nil {
		return memberType
	}
	if live := typeEnvironment.ListType(memberType.List.Element); live != (compilerTypes.Type{}) {
		return live
	}
	return memberType
}

// checkObjectConstructorFields checks one constructor call's arguments
// against an already-resolved nominal struct type's declared members.
// Shared by a bare Type(...) call (checkStructConstructorCall, which
// resolves literalType from a local name or generic template) and a
// qualified Alias.Type(...) or Alias.Type<T>(...) call (which resolves it
// from the target module's exported interface instead).
func checkObjectConstructorFields(call parser.CallExpression, typeName lexer.Token, literalType compilerTypes.Type, expectedType compilerTypes.Type, ctx checkContext) initializerValue {
	if literalType.Object == nil {
		return initializerValue{typ: literalType, token: typeName, diagnostic: diagnosticAt(messageAt(typeName, diag.TypeIsNotConstructible(typeName.Lexeme)))}
	}
	if compilerTypes.ForeignRecordIncomplete(literalType) {
		return initializerValue{typ: literalType, token: typeName, diagnostic: foreignIncompletePlacementDiagnostic(literalType, typeName, "a construction position")}
	}
	if expectedType.Name != "" && !compilerTypes.Assignable(expectedType, literalType) {
		return initializerValue{typ: literalType, token: typeName, diagnostic: diagnosticAt(messageAt(typeName, diag.ExpectedTypeGot(expectedType.Name, literalType.Name)))}
	}

	values := make([]ObjectMemberValue, 0, len(call.Arguments))
	seen := make(map[string]bool, len(call.Arguments))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for index, argument := range call.Arguments {
		label := call.ArgumentLabels[index]
		if label == nil {
			diagnostics = append(diagnostics, messageAt(tokenOf(argument), diag.ConstructorArgumentsMustBeNamed()))
			continue
		}
		member, exists := literalType.Object.Member(label.Lexeme)
		if !exists {
			diagnostics = append(diagnostics, messageAt(*label, diag.TypeHasNoMember(literalType.Name, label.Lexeme)))
			continue
		}
		if seen[member.Name] {
			diagnostics = append(diagnostics, messageAt(*label, diag.DuplicateConstructorMember(literalType.Name, member.Name)))
			continue
		}
		seen[member.Name] = true
		memberType := liveMemberType(member.Type, ctx.typeEnvironment)
		memberUse := member.Use
		if memberUse.Type == (compilerTypes.Type{}) || memberType != member.Type {
			memberUse = compilerTypes.NewTypeUse(memberType)
		}
		checked := checkInitializer(argument, memberUse, *label, ctx)
		if nestedDiagnostics := initializerDiagnostics(checked); len(nestedDiagnostics) > 0 {
			diagnostics = append(diagnostics, nestedDiagnostics...)
			continue
		}
		if !assignable(memberType, checked.typ) {
			diagnostics = append(diagnostics, typeMismatchDiagnostic(memberType, checked.typ, checked.token))
			continue
		}
		if diagnostic := restEscapeDiagnostic(checked.source, checked.token); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		values = append(values, ObjectMemberValue{Member: member, Source: checked.source})
	}
	for index := range literalType.Object.Members {
		member := &literalType.Object.Members[index]
		if !seen[member.Name] {
			diagnostics = append(diagnostics, messageAt(typeName, diag.MissingConstructorMember(literalType.Name, member.Name)))
		}
	}
	value := &ObjectValue{Type: literalType, Initializers: values}
	return initializerValue{
		source:      Operand{Kind: ObjectOperand, Type: literalType, Object: value, Node: Expression{Kind: ObjectExpression, Object: value}},
		typ:         literalType,
		token:       typeName,
		diagnostics: diagnostics,
		diagnostic: func() *compilerTypes.Diagnostic {
			if len(diagnostics) == 0 {
				return nil
			}
			return &diagnostics[0]
		}(),
	}
}

// checkQualifiedTypeConstructorCall checks Alias.Type(...) and
// Alias.Type<T>(...): construction of a nominal struct exported by the
// target module, concrete or generic. A concrete exported type's use is
// resolved in the target module's own checked interface, exactly like a
// non-generic exported function's signature. A generic exported type's
// concrete arguments are resolved in the requesting module -- explicit
// type arguments, or inferred from expectedType against the same open
// template -- but the template itself specializes in its defining module's
// retained context, exactly like an imported generic function. False means
// name is not an exported type or generic type of target, so the caller
// tries the next construction form (an ADT variant) or the qualified
// visibility diagnostic.
func checkQualifiedTypeConstructorCall(call parser.CallExpression, property lexer.Token, target string, expectedType compilerTypes.Type, ctx checkContext) (initializerValue, bool) {
	name := property.Lexeme
	if corelib.IsModule(target) {
		// A core library has no registry interface; its exported object types
		// are canonical compiler/types records, so construction checks them
		// directly.
		if typ, ok := corelib.LookupType(target, name); ok && typ.Object != nil {
			return checkObjectConstructorFields(call, property, typ, expectedType, ctx), true
		}
		return initializerValue{}, false
	}
	if use, ok := ctx.names.registry.exportedType(target, name); ok {
		return checkObjectConstructorFields(call, property, use.Type, expectedType, ctx), true
	}
	open, ok := ctx.names.registry.genericType(target, name)
	if !ok {
		return initializerValue{}, false
	}
	definingCtx, ok := ctx.names.registry.definingContext(target)
	if !ok {
		diagnostic := unknownAt(property)
		return initializerValue{token: property, diagnostic: &diagnostic}, true
	}
	var literalType compilerTypes.Type
	if len(call.TypeArguments) > 0 {
		arguments := make([]compilerTypes.Type, 0, len(call.TypeArguments))
		for _, argumentExpression := range call.TypeArguments {
			argumentUse, diagnostic := resolveTypeUse(argumentExpression, property, ctx.typeEnvironment, ctx.names.generics)
			if diagnostic != nil {
				return initializerValue{token: property, diagnostic: diagnostic}, true
			}
			arguments = append(arguments, argumentUse.Type)
		}
		specializedUse, diagnostic := specializeTypeUseArguments(open, arguments, property, definingCtx.typeEnvironment, definingCtx.names.generics)
		if diagnostic := diagnosticInDefiningModule(diagnostic, definingCtx.names.logicalKey); diagnostic != nil {
			return initializerValue{token: property, diagnostic: diagnostic}, true
		}
		literalType = specializedUse.Type
	} else if expectedType.Object != nil {
		if expectedOpen := definingCtx.names.generics.objectOpen[expectedType.Object]; expectedOpen == open {
			specializedUse, diagnostic := specializeTypeUseArguments(open, definingCtx.names.generics.objectArguments[expectedType.Object], property, definingCtx.typeEnvironment, definingCtx.names.generics)
			if diagnostic := diagnosticInDefiningModule(diagnostic, definingCtx.names.logicalKey); diagnostic != nil {
				return initializerValue{token: property, diagnostic: diagnostic}, true
			}
			literalType = specializedUse.Type
		}
	}
	if literalType.Object == nil {
		diagnostic := messageAt(property, diag.GenericCallCannotInferParameter(name))
		return initializerValue{token: property, diagnostic: &diagnostic}, true
	}
	return checkObjectConstructorFields(call, property, literalType, expectedType, ctx), true
}

// checkValue resolves an expression in value context. Assignment and
// address-taking call checkPlace instead to retain place mode.
func checkValue(expression parser.Expression, ctx checkContext) checkedExpression {
	return checkExpression(expression, expressionContext{}, ctx)
}

// checkContext bundles the scope and type environment threaded together
// across nearly every expression-checking function. It carries only that
// stable pair: a lexer token, an expected type, a generic table, or a flow
// fact is operation-specific and stays an explicit parameter beside it.
type checkContext struct {
	names           *scope
	typeEnvironment *compilerTypes.Environment
	// rootIndex is the source item index of the declaration being checked, so
	// capture visibility can compare it against a root binding's own index.
	rootIndex int
}

// methodCall bundles the four values every builtin method-call dispatcher
// receives: the written call, its property, the already-checked receiver, and
// the checking context. They always travel together through the receiver-type
// dispatch in checkMethodCall, so naming the group removes the repeated
// four-name signature and the risk of passing them in the wrong order.
type methodCall struct {
	call     parser.CallExpression
	callee   parser.PropertyExpression
	receiver checkedExpression
	ctx      checkContext
}

func checkExpression(expression parser.Expression, context expressionContext, ctx checkContext) checkedExpression {
	if len(context.expected.Candidates) > 1 && isContextualExpression(expression) {
		return checkContextualUnion(expression, context.expected, ctx)
	}
	switch expression := expression.(type) {
	case parser.AnonymousFunctionLiteral:
		return checkAnonymousFunctionLiteral(expression, context, ctx)
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
		diagnostic := messageAt(expression.Token, diag.NilRequiresExpectedNullableUnion())
		return checkedExpression{token: expression.Token, diagnostic: &diagnostic}
	case parser.EosLiteral:
		source := eosOperand(expression.Token.Lexeme)
		known := source
		return checkedExpression{source: source, typ: compilerTypes.EoS, token: expression.Token, known: &known}
	case parser.StringLiteral:
		return checkStringLiteral(expression, context.expected.Type)
	case parser.RawStringLiteral:
		return checkRawStringLiteral(expression, context.expected.Type)
	case parser.InterpolationTemplateExpression:
		diagnostic := messageAt(expression.Start, diag.StringInterpolationRequiresAPI())
		return checkedExpression{token: expression.Start, diagnostic: &diagnostic}
	case parser.ByteLiteral:
		return checkByteLiteral(expression)
	case parser.RuneLiteral:
		return checkRuneLiteral(expression)
	case parser.ArrayLiteralExpression:
		return checkArrayLiteral(expression, context.expected.Type, ctx)
	case parser.MatchExpression:
		return checkMatchExpression(expression, context, ctx)
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		if variable, isVariable := expression.(parser.VariableExpression); isVariable && context.expected.Type.Signature != nil {
			if reference, diagnostic := checkGenericFunctionReference(variable.Name, context.expected.Type, ctx); reference != nil || diagnostic != nil {
				if diagnostic != nil {
					return checkedExpression{token: variable.Name, diagnostic: diagnostic}
				}
				return *reference
			}
		}
		place := checkPlace(expression, ctx)
		if place.diagnostic != nil {
			return place
		}
		return valueFromPlace(place)
	case parser.AddressExpression:
		return checkAddress(expression, ctx)
	case parser.DereferenceExpression:
		place := checkDereferencePlace(expression, ctx)
		if place.diagnostic != nil {
			return place
		}
		return valueFromPlace(place)
	case parser.CallExpression:
		return checkCallValue(expression, context.expected.Type, ctx)
	case parser.UnaryExpression:
		return checkUnaryExpression(expression, context, ctx)
	case parser.SpawnExpression:
		return checkSpawnExpression(expression, ctx)
	case parser.TryExpression:
		return checkTryExpression(expression, context, ctx)
	case parser.BinaryExpression:
		return checkBinaryExpression(expression, context, ctx)
	case parser.TypeTestExpression:
		return checkUnionTypeTest(expression, ctx)
	default:
		return checkedExpression{
			diagnostic: diagnosticAt(unknownAt(lexer.Token{Line: 1, Column: 1})),
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

// memberNode builds a value member-read node. checkedType is the read's
// arena-resolved type: it is stamped so the generator recovers the checked
// identity directly instead of re-deriving it from the declared member,
// whose builtin List fields keep a fixed pre-arena identity.
func memberNode(operand Expression, member *compilerTypes.ObjectMember, checkedType compilerTypes.Type) Expression {
	node := Expression{Kind: MemberExpression, Operand: &operand, Member: member, ResultType: checkedType}
	if member != nil && isTrackedCollection(member.Type) {
		node.CollectionRoot = collectionRootOfNode(&operand)
	}
	return node
}
