package checker

import (
	diag "hexal/compiler/diagnostics"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

func trackablePointerBinding(bound binding) bool {
	// IO bindings ride the freed machinery so close records a versioned
	// closed state with the same rebinding and branch-merge semantics as
	// Heap.free. Stash and Pool handle bindings ride the same machinery so
	// destroy (and, for Pool, a rejected destroy-while-live) records a
	// versioned freed state for the handle itself, distinct from the
	// provenance-tracked allocations it owns.
	return bound.kind == dataBinding && !bound.parameter && !bound.loopBinder &&
		(bound.typ.Element != nil || compilerTypes.IsIO(bound.typ) || compilerTypes.IsFile(bound.typ) || compilerTypes.IsStash(bound.typ) || compilerTypes.IsPool(bound.typ) ||
			compilerTypes.IsTcpConnection(bound.typ) || compilerTypes.IsTcpListener(bound.typ) ||
			compilerTypes.IsProcess(bound.typ) || compilerTypes.IsPipe(bound.typ) || compilerTypes.IsSignals(bound.typ))
}

func directPointerBinding(source Operand, target compilerTypes.Type) BindingID {
	if target.Element == nil || source.Type.Element == nil || source.Node.Kind != VariableExpression || source.Node.Binding == 0 {
		return 0
	}
	return source.Node.Binding
}

func checkTypeDeclaration(declaration parser.TypeDeclaration, ctx checkContext, itemIndex int, functionIndexByName map[string]int) (TypeDeclaration, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	name := declaration.Name.Lexeme
	previousType, hadPreviousType := ctx.typeEnvironment.Lookup(name)
	previousUse, hadPreviousUse := ctx.typeEnvironment.LookupUse(name)
	functionIndex, declaredAsFunction := functionIndexByName[name]
	if compilerTypes.IsProtectedTypeName(name) {
		diagnostics = append(diagnostics, messageAt(declaration.Name, diag.BuiltinTypeCannotBeRedeclared(name, name == "Ptr" || name == "MutPtr")))
	} else if ctx.typeEnvironment.Contains(name) {
		diagnostics = append(diagnostics, messageAt(declaration.Name, diag.TypeAlreadyDeclared(name)))
	} else if ctx.names.declaredHere(name) || (declaredAsFunction && functionIndex < itemIndex) {
		diagnostics = append(diagnostics, messageAt(declaration.Name, diag.TypeAlreadyDeclaredAsValue(name)))
	}

	if len(declaration.Parameters) > 0 {
		genericDiagnostics := registerGenericTypeDeclaration(declaration, ctx)
		return TypeDeclaration{Name: name, Span: declaration.Name.Span}, append(diagnostics, genericDiagnostics...)
	}

	if adt, isADT := declaration.Target.(parser.AdtDefinitionExpression); isADT {
		checked, adtDiagnostics := checkADTDeclaration(declaration, adt, ctx)
		return checked, append(diagnostics, adtDiagnostics...)
	}

	if object, ok := declaration.Target.(parser.ObjectTypeExpression); ok {
		// A struct definition may declare zero members (an empty struct); an
		// ADT payload's own member list is validated separately
		// in resolveADTPayload and still requires at least one field.
		// Publish a provisional nominal identity before resolving members so a
		// member may reach this object behind at least one pointer layer. The
		// identity is abandoned if any member fails and finalized only on
		// complete success. BeginObject stamps the declaring module's
		// canonical id and encoded owner.
		beginResult := ctx.typeEnvironment.BeginObject(name, declaration.Name.Line, declaration.Name.Column)
		members, memberDiagnostics := resolveObjectMembers(name, object, ctx.typeEnvironment, ctx.names.generics)
		diagnostics = append(diagnostics, memberDiagnostics...)
		if len(diagnostics) == 0 {
			resolved := ctx.typeEnvironment.CompleteObject(name, members)
			if !compilerTypes.Equal(resolved, beginResult) {
				return TypeDeclaration{Name: name, Span: declaration.Name.Span}, compilerTypes.Diagnostics{compilerTypes.Locationless(diag.CheckerFailure())}
			}
			return TypeDeclaration{
				Name:    name,
				Type:    resolved,
				TypeUse: compilerTypes.NewTypeUse(resolved),
				Span:    declaration.Name.Span,
			}, nil
		}
		if hadPreviousType {
			if hadPreviousUse {
				ctx.typeEnvironment.DeclareAliasUse(name, previousUse)
			} else {
				ctx.typeEnvironment.DeclareAlias(name, previousType)
			}
		} else {
			ctx.typeEnvironment.AbandonObject(name)
		}
		return TypeDeclaration{Name: name, Span: declaration.Name.Span}, diagnostics
	}

	if containsTypeName(declaration.Target, name) {
		diagnostics = append(diagnostics, messageAt(declaration.Name, diag.TypeAliasCannotReferenceItself(name)))
	} else if resolvedUse, diagnostic := resolveTypeUse(declaration.Target, declaration.Name, ctx.typeEnvironment, ctx.names.generics); diagnostic != nil {
		diagnostics = append(diagnostics, *diagnostic)
	} else if len(diagnostics) == 0 {
		return TypeDeclaration{
			Name:    name,
			Type:    resolvedUse.Type,
			TypeUse: resolvedUse,
			Span:    declaration.Name.Span,
		}, nil
	}

	return TypeDeclaration{
		Name: name,
		Span: declaration.Name.Span,
	}, diagnostics
}

func resolveObjectMembers(objectName string, expression parser.ObjectTypeExpression, typeEnvironment *compilerTypes.Environment, generics *genericTable) ([]compilerTypes.ObjectMember, compilerTypes.Diagnostics) {
	members := make([]compilerTypes.ObjectMember, 0, len(expression.Members))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(expression.Members))
	for _, declaration := range expression.Members {
		if seen[declaration.Name.Lexeme] {
			diagnostics = append(diagnostics, messageAt(declaration.Name, diag.DuplicateObjectMember(objectName, declaration.Name.Lexeme)))
			continue
		}
		seen[declaration.Name.Lexeme] = true

		if containsTypeName(declaration.Type, objectName) && !containsPointerType(declaration.Type) {
			diagnostics = append(diagnostics, messageAt(declaration.Name, diag.ObjectCannotContainItselfByValue(objectName)))
			continue
		}

		resolvedUse, diagnostic := resolveTypeUse(declaration.Type, declaration.Name, typeEnvironment, generics)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		resolved := resolvedUse.Type
		if diagnostic := valueTypeDiagnostic(declaration.Type, declaration.Name, resolved); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		// Any complete, finitely sized value may be an object member except
		// Unknown and Atomic at non-construction positions. Fun<...> is valid
		// as an object member for explicit dispatch tables. An open type
		// parameter defers to specialization rechecking.
		if !compilerTypes.ContainsTypeParameter(resolved) && !compilerTypes.Storable(resolved, compilerTypes.PositionObjectMember) {
			diagnostics = append(diagnostics, messageAt(declaration.Name, diag.UnsupportedObjectMemberType(resolved.Name)))
			continue
		}
		members = append(members, compilerTypes.ObjectMember{
			Name:         declaration.Name.Lexeme,
			Type:         resolved,
			Use:          resolvedUse,
			Mutable:      declaration.Mutable,
			SourceLine:   declaration.Name.Line,
			SourceColumn: declaration.Name.Column,
		})
	}
	return members, diagnostics
}

func containsPointerType(expression parser.TypeExpression) bool {
	switch expression := expression.(type) {
	case parser.PtrTypeExpression:
		return true
	case parser.UnionTypeExpression:
		for _, member := range expression.Members {
			if containsPointerType(member) {
				return true
			}
		}
		return false
	case parser.GroupedTypeExpression:
		return containsPointerType(expression.Inner)
	case parser.FunctionTypeExpression:
		for _, parameter := range expression.Parameters {
			if containsPointerType(parameter) {
				return true
			}
		}
		return expression.Return != nil && containsPointerType(expression.Return)
	case parser.ObjectTypeExpression:
		for _, member := range expression.Members {
			if containsPointerType(member.Type) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

func containsTypeName(expression parser.TypeExpression, name string) bool {
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		return expression.Name.Lexeme == name
	case parser.GenericTypeExpression:
		return expression.Name.Lexeme == name
	case parser.PtrTypeExpression:
		return containsTypeName(expression.Element, name)
	case parser.UnionTypeExpression:
		for _, member := range expression.Members {
			if containsTypeName(member, name) {
				return true
			}
		}
		return false
	case parser.GroupedTypeExpression:
		return containsTypeName(expression.Inner, name)
	case parser.FunctionTypeExpression:
		for _, parameter := range expression.Parameters {
			if containsTypeName(parameter, name) {
				return true
			}
		}
		return expression.Return != nil && containsTypeName(expression.Return, name)
	case parser.ObjectTypeExpression:
		for _, member := range expression.Members {
			if containsTypeName(member.Type, name) {
				return true
			}
		}
		return false
	default:
		return false
	}
}

// firstTypeNameDeclaredAtOrAfter walks a written type expression looking for
// a named reference to a type whose own declaration is at or after
// itemIndex, returning that name's token. A root declaration's own type
// annotation is checked against this before resolution, since root
// declarations keep their pre-existing source-order restriction even though
// function signatures and bodies gained forward visibility into every module
// type. itemIndex negative means unrestricted (nested declarations, which do
// share that forward visibility), and always reports no match.
func firstTypeNameDeclaredAtOrAfter(expression parser.TypeExpression, itemIndex int, typeIndexByName map[string]int) (lexer.Token, bool) {
	if itemIndex < 0 {
		return lexer.Token{}, false
	}
	tooLate := func(name lexer.Token) bool {
		declaredIndex, ok := typeIndexByName[name.Lexeme]
		return ok && declaredIndex >= itemIndex
	}
	switch expression := expression.(type) {
	case parser.NamedTypeExpression:
		if tooLate(expression.Name) {
			return expression.Name, true
		}
	case parser.GenericTypeExpression:
		if tooLate(expression.Name) {
			return expression.Name, true
		}
		for _, argument := range expression.Arguments {
			if token, found := firstTypeNameDeclaredAtOrAfter(argument, itemIndex, typeIndexByName); found {
				return token, true
			}
		}
	case parser.PtrTypeExpression:
		return firstTypeNameDeclaredAtOrAfter(expression.Element, itemIndex, typeIndexByName)
	case parser.ArrayTypeExpression:
		return firstTypeNameDeclaredAtOrAfter(expression.Element, itemIndex, typeIndexByName)
	case parser.UnionTypeExpression:
		for _, member := range expression.Members {
			if token, found := firstTypeNameDeclaredAtOrAfter(member, itemIndex, typeIndexByName); found {
				return token, true
			}
		}
	case parser.GroupedTypeExpression:
		return firstTypeNameDeclaredAtOrAfter(expression.Inner, itemIndex, typeIndexByName)
	case parser.FunctionTypeExpression:
		for _, parameter := range expression.Parameters {
			if token, found := firstTypeNameDeclaredAtOrAfter(parameter, itemIndex, typeIndexByName); found {
				return token, true
			}
		}
		if expression.Return != nil {
			return firstTypeNameDeclaredAtOrAfter(expression.Return, itemIndex, typeIndexByName)
		}
	}
	return lexer.Token{}, false
}

// registerGenericTypeDeclaration validates and stores one generic type or
// alias declaration as an open template. The target is not resolved yet:
// parameters are placeholders until a concrete specialization is requested.
func registerGenericTypeDeclaration(declaration parser.TypeDeclaration, ctx checkContext) compilerTypes.Diagnostics {
	name := declaration.Name.Lexeme
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(declaration.Parameters))
	parameterNames := make([]string, 0, len(declaration.Parameters))
	for _, parameter := range declaration.Parameters {
		if seen[parameter.Lexeme] {
			diagnostics = append(diagnostics, messageAt(parameter, diag.DuplicateGenericParameter(parameter.Lexeme)))
			continue
		}
		seen[parameter.Lexeme] = true
		if compilerTypes.IsProtectedTypeName(parameter.Lexeme) {
			diagnostics = append(diagnostics, messageAt(parameter, diag.ProtectedGenericParameter(parameter.Lexeme)))
			continue
		}
		parameterNames = append(parameterNames, parameter.Lexeme)
	}
	if len(diagnostics) > 0 {
		return diagnostics
	}
	generic := ctx.typeEnvironment.DeclareGeneric(name, len(declaration.Parameters), parameterNames)
	if generic == nil {
		return compilerTypes.Diagnostics{messageAt(declaration.Name, diag.TypeAlreadyDeclared(name))}
	}
	if _, objectTarget := declaration.Target.(parser.ObjectTypeExpression); objectTarget {
		if containsTypeName(declaration.Target, name) && !containsPointerType(declaration.Target) {
			return compilerTypes.Diagnostics{messageAt(declaration.Name, diag.ObjectCannotContainItselfByValue(name))}
		}
	} else if containsTypeName(declaration.Target, name) {
		return compilerTypes.Diagnostics{messageAt(declaration.Name, diag.TypeAliasCannotReferenceItself(name))}
	}
	ctx.names.generics.types[name] = &openGenericType{
		Name:        name,
		Parameters:  append([]lexer.Token(nil), declaration.Parameters...),
		Target:      declaration.Target,
		Declaration: generic,
	}
	return nil
}

func checkDeclaration(declaration parser.Declaration, ctx checkContext, itemIndex int, typeIndexByName map[string]int) (Declaration, binding, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	// A declaration without a type expression takes its type from the
	// initializer below.
	var declaredUse compilerTypes.TypeUse
	if declaration.Type != nil {
		if token, tooLate := firstTypeNameDeclaredAtOrAfter(declaration.Type, itemIndex, typeIndexByName); tooLate {
			diagnostics = append(diagnostics, messageAt(token, diag.UnknownType(token.Lexeme)))
		} else {
			resolved, typeDiagnostic := resolveTypeUse(declaration.Type, declaration.Name, ctx.typeEnvironment, ctx.names.generics)
			declaredUse = resolved
			if typeDiagnostic != nil {
				diagnostics = append(diagnostics, *typeDiagnostic)
			} else if diagnostic := valueTypeDiagnostic(declaration.Type, declaration.Name, resolved.Type); diagnostic != nil {
				diagnostics = append(diagnostics, *diagnostic)
			}
		}
	}
	declaredType := declaredUse.Type
	if declaration.Name.Lexeme == "print" {
		// The protected builtin name cannot be bound by a local or
		// module declaration.
		diagnostics = append(diagnostics, protectedBindingNameDiagnostic(declaration.Name, "print"))
	}
	if layoutBuiltins[declaration.Name.Lexeme] {
		// The layout query names cannot be bound by a local or
		// module declaration.
		diagnostics = append(diagnostics, protectedBindingNameDiagnostic(declaration.Name, declaration.Name.Lexeme))
	}
	if compilerTypes.IsProtectedTypeName(declaration.Name.Lexeme) {
		if declaration.Name.Lexeme == "Ptr" || declaration.Name.Lexeme == "MutPtr" {
			diagnostics = append(diagnostics, messageAt(declaration.Name, diag.BuiltinTypeCannotBeRedeclared(declaration.Name.Lexeme, true)))
		} else {
			diagnostics = append(diagnostics, messageAt(declaration.Name, diag.ValueNameConflictsWithType(declaration.Name.Lexeme)))
		}
	} else if ctx.typeEnvironment.Contains(declaration.Name.Lexeme) {
		diagnostics = append(diagnostics, messageAt(declaration.Name, diag.ValueNameConflictsWithType(declaration.Name.Lexeme)))
	}
	if ctx.names.declaredHere(declaration.Name.Lexeme) {
		diagnostics = append(diagnostics, messageAt(declaration.Name, diag.VariableAlreadyDeclaredInScope(declaration.Name.Lexeme)))
	}

	// An inferred binding has no destination context. A contextual initializer
	// would otherwise receive a silent literal default, so reject it before type
	// checking can select one.
	inferred := declaration.Type == nil
	if inferred && isContextualForInference(declaration.Initializer) {
		diagnostics = append(diagnostics, messageAt(declaration.Keyword, diag.ContextualInitializerNeedsAnnotation()))
	}

	initializer := checkInitializerRest(declaration.Initializer, declaredUse, declaration.Name, ctx, true)
	for _, diagnostic := range initializerDiagnostics(initializer) {
		diagnostics = append(diagnostics, diagnostic)
	}
	if inferred && len(diagnostics) == 0 {
		// The binding takes the initializer's resolved type and use, which is
		// exactly what an annotation would have produced for the same source.
		declaredUse = initializer.use
		declaredType = initializer.typ
		if declaredUse.Type.Name == "" {
			declaredUse = compilerTypes.NewTypeUse(declaredType)
		}
	}
	if len(diagnostics) == 0 {
		if diagnostic := atomicCopyDiagnostic(initializer.source, declaration.Name); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	if len(diagnostics) == 0 && declaredType.Name != "" && !assignable(declaredType, initializer.typ) {
		diagnostics = append(diagnostics, bindingMismatchDiagnostic(declaration.Name.Lexeme, declaredType, initializer.typ, initializer.token))
	}

	declaredBinding := binding{
		typ:     declaredType,
		use:     declaredUse,
		mutable: declaration.Mutable,
		id:      ctx.names.newBindingID(),
	}
	if initializer.source.RestBacked {
		// A rest-backed descriptor may only be re-bound to a fixed local
		// alias; the alias inherits the provenance so its own uses stay
		// restricted.
		if declaration.Mutable {
			diagnostics = append(diagnostics, messageAt(declaration.Name, diag.RestBackedSliceRequiresFixedAlias()))
		} else {
			declaredBinding.restBacked = true
		}
	}
	if isTrackedCollection(declaredType) {
		declaredBinding.collectionRoot = collectionRootForOperand(initializer.source, ctx.names, declaredBinding.id)
	}
	if initializer.source.Node.Kind == AddressOfExpression || nodeTracesToRef(&initializer.source.Node, ctx.names) {
		declaredBinding.fromRef = true
	}
	if ctx.names.flow != nil {
		recordStringBinding(ctx.names.flow, declaredBinding.id, initializer.source.Node, ctx)
	}
	if len(diagnostics) == 0 && !declaration.Mutable && initializer.known != nil {
		// The initializer's known value becomes this binding's known-value
		// metadata, whether the initializer is a literal constant or a read
		// of another named immutable binding (reads keep their variable
		// operand in the checked program, so the metadata must be copied
		// explicitly).
		known := *initializer.known
		declaredBinding.known = &known
	}
	if len(diagnostics) == 0 && ctx.names.flow != nil && trackablePointerBinding(declaredBinding) {
		ctx.names.flow.trackFreed(declaredBinding.id)
		if sourceBinding := directPointerBinding(initializer.source, declaredType); sourceBinding != 0 {
			ctx.names.flow.aliasFreed(sourceBinding, declaredBinding.id)
		}
	}
	if len(diagnostics) == 0 && ctx.names.flow != nil {
		seedStreamBindingFacts(ctx.names.flow, declaredBinding.id, declaredType, initializer.source)
	}
	return Declaration{
		Name:    declaration.Name.Lexeme,
		Binding: declaredBinding.id,
		Type:    declaredType,
		TypeUse: declaredUse,
		Source:  initializer.source,
		Mutable: declaration.Mutable,
		Span:    declaration.Name.Span,
	}, declaredBinding, diagnostics
}

func checkAssignment(assignment parser.Assignment, ctx checkContext) (Assignment, compilerTypes.Diagnostics) {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	nameToken := assignment.Name
	if nameToken == (lexer.Token{}) {
		// An operator-opened target (such as ^pointer) carries no name
		// token: diagnostics and source mapping fall back to the target's
		// own operator.
		nameToken = expressionToken(assignment.Target)
	}
	target := checkPlace(assignment.Target, ctx)
	switch {
	case target.diagnostic != nil:
		diagnostics = append(diagnostics, *target.diagnostic)
	case target.self:
		// Method rule 3: only the binding itself is fixed. A write through
		// self, such as self.x, is a member place and is checked as one.
		diagnostics = append(diagnostics, messageAt(nameToken, diag.CannotAssignToSelf()))
	case target.function:
		// A function declaration names code, not a replaceable storage slot.
		diagnostics = append(diagnostics, messageAt(nameToken, diag.CannotAssignToFunction(nameToken.Lexeme)))
	case target.parameter:
		diagnostics = append(diagnostics, messageAt(nameToken, diag.CannotAssignToParameter(nameToken.Lexeme)))
	case target.loopBinder:
		diagnostics = append(diagnostics, messageAt(nameToken, diag.LoopBinderIsImmutable(nameToken.Lexeme)))
	case !target.source.Writable:
		diagnostics = append(diagnostics, assignmentTargetDiagnostic(assignment.Target, nameToken))
	}

	// Assignment writes to the binding's declared storage slot, never to a
	// branch-local narrowed type, and an accepted assignment invalidates any
	// narrowing: the slot may hold nil again.
	targetType := target.typ
	targetBinding := BindingID(0)
	if variable, ok := assignment.Target.(parser.VariableExpression); ok && target.source.Binding != 0 {
		targetBinding = target.source.Binding
		if bound, status := ctx.names.lookup(variable.Name.Lexeme); status == nameFound {
			targetType = bound.typ
			target.source.Type = bound.typ
			target.use = bound.use
		}
	}
	targetUse := target.use
	if targetUse.Type == (compilerTypes.Type{}) {
		targetUse = compilerTypes.NewTypeUse(targetType)
	}
	initializer := checkInitializer(assignment.Initializer, targetUse, nameToken, ctx)
	for _, diagnostic := range initializerDiagnostics(initializer) {
		diagnostics = append(diagnostics, diagnostic)
	}
	if len(diagnostics) == 0 {
		if diagnostic := restEscapeDiagnostic(initializer.source, initializer.token); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	if len(diagnostics) == 0 {
		if diagnostic := atomicCopyDiagnostic(initializer.source, nameToken); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
		}
	}
	if len(diagnostics) == 0 && initializer.typ != (compilerTypes.Type{}) && !assignable(targetType, initializer.typ) {
		diagnostics = append(diagnostics, bindingMismatchDiagnostic(nameToken.Lexeme, targetType, initializer.typ, initializer.token))
	}
	if len(diagnostics) == 0 && ctx.names.flow != nil && targetBinding != 0 {
		ctx.names.flow.invalidateNarrowing(targetBinding)
		if sourceBinding := directPointerBinding(initializer.source, targetType); sourceBinding != 0 {
			ctx.names.flow.aliasFreed(sourceBinding, targetBinding)
		} else {
			ctx.names.flow.clearFreed(targetBinding)
		}
		seedStreamBindingFacts(ctx.names.flow, targetBinding, targetType, initializer.source)
	}
	if len(diagnostics) == 0 && ctx.names.flow != nil {
		recordStringAssignment(ctx.names.flow, target.source, initializer.source, targetBinding, ctx)
	}
	if len(diagnostics) == 0 && targetBinding != 0 {
		// Assignment re-sources the slot: the binding now holds the address-derived
		// value exactly when the assigned initializer traces to an @ expression, so the
		// flag is both set and cleared by the same check.
		ctx.names.setFromRef(targetBinding, nodeTracesToRef(&initializer.source.Node, ctx.names))
	}
	if len(diagnostics) == 0 && targetBinding != 0 && isTrackedCollection(targetType) {
		ctx.names.setCollectionRoot(targetBinding, collectionRootForOperand(initializer.source, ctx.names, targetBinding))
	}

	return Assignment{
		Name:   nameToken.Lexeme,
		Target: target.source,
		Type:   targetType,
		Source: initializer.source,
		Span:   nameToken.Span,
	}, diagnostics
}

func assignmentTargetDiagnostic(target parser.Expression, fallback lexer.Token) compilerTypes.Diagnostic {
	if variable, ok := target.(parser.VariableExpression); ok {
		return messageAt(variable.Name, diag.CannotAssignToConstant(variable.Name.Lexeme))
	}
	if property, ok := target.(parser.PropertyExpression); ok {
		return messageAt(property.Property, diag.CannotAssignToReadOnlyMember(placeDescription(target)))
	}
	at := fallback
	if at == (lexer.Token{}) {
		// An operator-opened target (such as ^pointer) carries no name
		// token, so the diagnostic points at the target's own operator.
		at = expressionToken(target)
	}
	return messageAt(at, diag.CannotWriteThroughReadOnlyPointer(placeDescription(target)))
}

// bindingMismatchDiagnostic names the binding for a function-pointer slot,
// where "expected X initializer" reads poorly against two Fun<...> spellings.
func bindingMismatchDiagnostic(name string, declaredType, actualType compilerTypes.Type, token lexer.Token) compilerTypes.Diagnostic {
	if declaredType.Signature != nil || actualType.Signature != nil {
		return messageAt(token, diag.FunctionBindingTypeMismatch(name, declaredType.Name, actualType.Name))
	}
	return typeMismatchDiagnostic(declaredType, actualType, token)
}

func typeMismatchDiagnostic(declaredType, actualType compilerTypes.Type, token lexer.Token) compilerTypes.Diagnostic {
	kind, erased := assignabilityMismatch(declaredType, actualType)
	message := diag.InitializerTypeMismatch(declaredType.Name, actualType.Name, kind, erased, textMismatchDetails(declaredType, actualType))
	return messageAt(token, message)
}

func assignabilityMismatch(target, source compilerTypes.Type) (diag.AssignabilityMismatchKind, string) {
	if compilerTypes.IsNil(source) || compilerTypes.IsNullable(source) {
		if !compilerTypes.IsNullable(target) {
			return diag.NullableSourceMismatch, ""
		}
	}
	if target.Element != nil && source.Element != nil {
		if target.PointeeWritable && !source.PointeeWritable && compilerTypes.IsUnknown(*source.Element) && !compilerTypes.IsUnknown(*target.Element) {
			return diag.WritablePointerAccessRecovery, ""
		}
		if target.Element.Element != nil && compilerTypes.IsUnknown(*target.Element.Element) && source.Element.Element != nil {
			return diag.NestedPointerSlotErasure, ""
		}
		if target.PointeeWritable && source.PointeeWritable &&
			!compilerTypes.IsUnknown(*target.Element) && !compilerTypes.IsUnknown(*source.Element) &&
			!compilerTypes.Equal(*target.Element, *source.Element) {
			erased := "Ptr<Unknown>"
			if source.PointeeWritable {
				erased = "Ptr<mut Unknown>"
			}
			return diag.PointerErasureRecoveryComposition, erased
		}
	}
	return diag.OrdinaryInitializerMismatch, ""
}
