package checker

import (
	"fmt"
	"slices"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkADTDeclaration validates and registers one ADT declaration.
func checkADTDeclaration(declaration parser.TypeDeclaration, target parser.AdtDefinitionExpression, ctx checkContext) (TypeDeclaration, compilerTypes.Diagnostics) {
	name := declaration.Name.Lexeme
	diagnostics := make(compilerTypes.Diagnostics, 0)
	if len(target.Variants) < 2 {
		diagnostics = append(diagnostics, typeErrorAt(declaration.Name, "ADT declarations require at least two variants"))
		return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, diagnostics
	}
	seen := make(map[string]bool, len(target.Variants))
	for _, variant := range target.Variants {
		if seen[variant.Name.Lexeme] {
			diagnostics = append(diagnostics, typeErrorAt(variant.Name, "ADT variant name is duplicated"))
			continue
		}
		seen[variant.Name.Lexeme] = true
	}
	if len(diagnostics) > 0 {
		return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, diagnostics
	}

	// Like object declarations, an ADT is stamped with the declaring
	// module's canonical id: that id owns its canonical key. BeginADT
	// applies the stamp.
	ctx.typeEnvironment.BeginADT(name, declaration.Name.Line, declaration.Name.Column)
	variants := make([]compilerTypes.AdtVariant, 0, len(target.Variants))
	for _, variant := range target.Variants {
		resolved := compilerTypes.AdtVariant{Name: variant.Name.Lexeme}
		if variant.Payload != nil {
			payload, payloadDiagnostics := resolveADTPayload(name, *variant.Payload, ctx.typeEnvironment, ctx.names.generics)
			diagnostics = append(diagnostics, payloadDiagnostics...)
			resolved.Payload = payload
		}
		variants = append(variants, resolved)
	}
	if len(diagnostics) > 0 {
		ctx.typeEnvironment.AbandonADT(name)
		return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, diagnostics
	}
	completed := ctx.typeEnvironment.CompleteADT(name, variants)
	return TypeDeclaration{
		Name:         name,
		Type:         completed,
		TypeUse:      compilerTypes.NewTypeUse(completed),
		SourceLine:   declaration.Name.Line,
		SourceColumn: declaration.Name.Column,
	}, nil
}

// resolveADTPayload resolves one variant's payload member list, rejecting
// mutable fields and by-value recursion.
func resolveADTPayload(adtName string, expression parser.ObjectTypeExpression, typeEnvironment *compilerTypes.Environment, generics *genericTable) ([]compilerTypes.ObjectMember, compilerTypes.Diagnostics) {
	members := make([]compilerTypes.ObjectMember, 0, len(expression.Members))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	seen := make(map[string]bool, len(expression.Members))
	for _, member := range expression.Members {
		if seen[member.Name.Lexeme] {
			diagnostics = append(diagnostics, typeErrorAt(member.Name, fmt.Sprintf("variant payload declares field %s more than once", member.Name.Lexeme)))
			continue
		}
		seen[member.Name.Lexeme] = true
		if containsTypeName(member.Type, adtName) && !containsPointerType(member.Type) {
			diagnostics = append(diagnostics, typeErrorAt(member.Name, "ADT recursion has no finite representation"))
			continue
		}
		resolvedUse, diagnostic := resolveTypeUse(member.Type, member.Name, typeEnvironment, generics)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if diagnostic := valueTypeDiagnostic(member.Type, member.Name, resolvedUse.Type); diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if compilerTypes.ContainsAtomic(resolvedUse.Type) {
			diagnostics = append(diagnostics, typeErrorAt(member.Name, "Atomic values cannot be copied, assigned, addressed, or stored here"))
			continue
		}
		if !compilerTypes.ContainsTypeParameter(resolvedUse.Type) && !compilerTypes.Storable(resolvedUse.Type, compilerTypes.PositionADTPayload) {
			diagnostics = append(diagnostics, typeErrorAt(member.Name, "unsupported ADT payload field type "+resolvedUse.Type.Name))
			continue
		}
		members = append(members, compilerTypes.ObjectMember{
			Name:         member.Name.Lexeme,
			Type:         resolvedUse.Type,
			Use:          resolvedUse,
			SourceLine:   member.Name.Line,
			SourceColumn: member.Name.Column,
		})
	}
	return members, diagnostics
}

// resolveVariantOwner resolves a qualified variant's owner ADT, handling
// generic owners through explicit arguments or expected-type inference.
func resolveVariantOwner(owner string, ownerArguments []parser.TypeExpression, expectedType compilerTypes.Type, token lexer.Token, ctx checkContext) (compilerTypes.Type, *compilerTypes.AdtVariant, *compilerTypes.Diagnostic) {
	if adtType, ok := ctx.typeEnvironment.Lookup(owner); ok && adtType.Adt != nil {
		return adtType, nil, nil
	}
	if ctx.names.generics == nil {
		return compilerTypes.Type{}, nil, nil
	}
	open, generic := ctx.names.generics.types[owner]
	if !generic {
		return compilerTypes.Type{}, nil, nil
	}
	if _, isADT := open.Target.(parser.AdtDefinitionExpression); !isADT {
		return compilerTypes.Type{}, nil, nil
	}
	var arguments []compilerTypes.Type
	if len(ownerArguments) > 0 {
		arguments = make([]compilerTypes.Type, 0, len(ownerArguments))
		for _, argument := range ownerArguments {
			use, diagnostic := resolveTypeUse(argument, lexer.Token{}, ctx.typeEnvironment, ctx.names.generics)
			if diagnostic != nil {
				return compilerTypes.Type{}, nil, diagnostic
			}
			arguments = append(arguments, use.Type)
		}
	} else if expectedType.Adt != nil {
		if expectedOpen := ctx.names.generics.adtOpen[expectedType.Adt]; expectedOpen == open {
			arguments = ctx.names.generics.adtArguments[expectedType.Adt]
		}
	}
	if len(arguments) == 0 {
		return compilerTypes.Type{}, nil, diagnosticAt(typeErrorAt(token, fmt.Sprintf("cannot infer generic parameter for %s", owner)))
	}
	specialized, diagnostic := specializeADTType(open, arguments, lexer.Token{}, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return compilerTypes.Type{}, nil, diagnostic
	}
	return specialized, nil, nil
}

// checkQualifiedVariant resolves a record-variant constructor.
func checkQualifiedVariant(expression parser.QualifiedVariantExpression, expectedType compilerTypes.Type, ctx checkContext) initializerValue {
	// An import-alias owner routes to the target module's exported ADT
	// variants before ordinary owner resolution.
	if target, ok := ctx.names.importAliasTarget(expression.Owner.Lexeme); ok {
		return checkModuleVariantConstructor(expression, target, ctx)
	}
	adtType, _, ownerDiagnostic := resolveVariantOwner(expression.Owner.Lexeme, expression.OwnerArguments, expectedType, expression.Owner, ctx)
	if ownerDiagnostic != nil {
		return initializerValue{token: expression.Variant, diagnostic: ownerDiagnostic}
	}
	variant, ok := ctx.typeEnvironment.AdtVariant(expression.Owner.Lexeme, expression.Variant.Lexeme)
	if !ok && adtType.Adt != nil {
		index := adtVariantIndex(adtType, expression.Variant.Lexeme)
		if index >= 0 {
			variant = &adtType.Adt.Variants[index]
			ok = true
		}
	}
	if !ok {
		return initializerValue{token: expression.Variant, diagnostic: diagnosticAt(typeErrorAt(expression.Variant, fmt.Sprintf("unknown qualified variant %s.%s", expression.Owner.Lexeme, expression.Variant.Lexeme)))}
	}
	return buildVariantConstructor(expression, adtType, variant, ctx)
}

// checkModuleVariantConstructor resolves Owner.Variant {...} where Owner is an
// import alias: the variant must belong to an exported ADT of the target
// module.
func checkModuleVariantConstructor(expression parser.QualifiedVariantExpression, target string, ctx checkContext) initializerValue {
	adtType, variant, ok := ctx.names.registry.findExportedADTVariant(target, expression.Variant.Lexeme)
	if !ok {
		diagnostic := privateToModuleDiagnostic(expression.Variant, expression.Variant.Lexeme, target)
		return initializerValue{token: expression.Variant, diagnostic: &diagnostic}
	}
	return buildVariantConstructor(expression, adtType, variant, ctx)
}

// buildVariantConstructor checks the payload of one resolved record-variant
// constructor and builds its AdtConstructExpression. Unit and payload shapes
// are checked against the variant record exactly once, whichever path
// resolved it.
func buildVariantConstructor(expression parser.QualifiedVariantExpression, adtType compilerTypes.Type, variant *compilerTypes.AdtVariant, ctx checkContext) initializerValue {
	if expression.Payload == nil {
		return adtUnitVariant(adtType, variant, expression.Variant)
	}
	if len(variant.Payload) == 0 {
		return initializerValue{token: expression.Variant, diagnostic: diagnosticAt(typeErrorAt(expression.Variant, fmt.Sprintf("%s.%s is a unit variant and takes no payload", expression.Owner.Lexeme, expression.Variant.Lexeme)))}
	}
	seen := make(map[string]bool, len(*expression.Payload))
	// byField and evaluationOrder are populated in written order, but
	// Arguments below is assembled in variant.Payload declaration order:
	// renderAdtConstruct indexes Arguments positionally against
	// variant.Payload, so declaration order is what the checked tree must
	// carry. evaluationOrder separately records, as indices into the
	// declaration-ordered Arguments, the order fields were actually
	// written in, so generation can still sequence side effects in
	// written order without reordering the field assignment itself.
	byField := make(map[string]Operand, len(*expression.Payload))
	evaluationOrder := make([]int, 0, len(*expression.Payload))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for _, initializer := range *expression.Payload {
		field, exists := variantField(variant, initializer.Name.Lexeme)
		if !exists {
			diagnostics = append(diagnostics, typeErrorAt(initializer.Name, fmt.Sprintf("%s has no field named %s", variant.Name, initializer.Name.Lexeme)))
			continue
		}
		if seen[field.Name] {
			diagnostics = append(diagnostics, typeErrorAt(initializer.Name, fmt.Sprintf("%s initializes field %s more than once", variant.Name, field.Name)))
			continue
		}
		seen[field.Name] = true
		checked := checkInitializer(initializer.Value, field.Use, initializer.Name, ctx)
		if nestedDiagnostics := initializerDiagnostics(checked); len(nestedDiagnostics) > 0 {
			diagnostics = append(diagnostics, nestedDiagnostics...)
			continue
		}
		if !assignable(field.Type, checked.typ) {
			diagnostics = append(diagnostics, typeMismatchDiagnostic(field.Type, checked.typ, checked.token))
			continue
		}
		byField[field.Name] = checked.source
		declaredIndex := -1
		for index := range variant.Payload {
			if variant.Payload[index].Name == field.Name {
				declaredIndex = index
				break
			}
		}
		evaluationOrder = append(evaluationOrder, declaredIndex)
	}
	for index := range variant.Payload {
		if !seen[variant.Payload[index].Name] {
			diagnostics = append(diagnostics, typeErrorAt(expression.Variant, fmt.Sprintf("variant constructor requires the payload field %s", variant.Payload[index].Name)))
		}
	}
	if len(diagnostics) > 0 {
		return initializerValue{token: expression.Variant, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	arguments := make([]Operand, len(variant.Payload))
	for index := range variant.Payload {
		arguments[index] = byField[variant.Payload[index].Name]
	}
	node := Expression{
		Kind:            AdtConstructExpression,
		OperandType:     adtType,
		ResultType:      adtType,
		VariantIndex:    adtVariantIndex(adtType, variant.Name),
		Arguments:       arguments,
		EvaluationOrder: evaluationOrder,
	}
	source := Operand{Kind: ExpressionOperand, Type: adtType, Node: node}
	return initializerValue{source: source, typ: adtType, token: expression.Variant}
}

func variantField(variant *compilerTypes.AdtVariant, name string) (*compilerTypes.ObjectMember, bool) {
	for index := range variant.Payload {
		if variant.Payload[index].Name == name {
			return &variant.Payload[index], true
		}
	}
	return nil, false
}

func adtVariantIndex(adtType compilerTypes.Type, variant string) int {
	if adtType.Adt == nil {
		return -1
	}
	for index := range adtType.Adt.Variants {
		if adtType.Adt.Variants[index].Name == variant {
			return index
		}
	}
	return -1
}

// checkUnitVariant resolves a bare Owner.Variant chain as a unit variant value
// when the owner names an ADT (or a generic ADT resolvable from the expected
// type).
func checkUnitVariant(owner, variant lexer.Token, expectedType compilerTypes.Type, ctx checkContext) (*checkedExpression, *compilerTypes.Diagnostic) {
	adtType, _, ownerDiagnostic := resolveVariantOwner(owner.Lexeme, nil, expectedType, owner, ctx)
	if ownerDiagnostic != nil {
		return nil, ownerDiagnostic
	}
	adtVariant, ok := ctx.typeEnvironment.AdtVariant(owner.Lexeme, variant.Lexeme)
	if !ok && adtType.Adt != nil {
		index := adtVariantIndex(adtType, variant.Lexeme)
		if index >= 0 {
			adtVariant = &adtType.Adt.Variants[index]
			ok = true
		}
	}
	if !ok {
		return nil, nil
	}
	value := adtUnitVariant(adtType, adtVariant, variant)
	return &value, nil
}

func adtUnitVariant(adtType compilerTypes.Type, variant *compilerTypes.AdtVariant, token lexer.Token) checkedExpression {
	node := Expression{
		Kind:         AdtConstructExpression,
		OperandType:  adtType,
		ResultType:   adtType,
		VariantIndex: adtVariantIndex(adtType, variant.Name),
	}
	source := Operand{Kind: ExpressionOperand, Type: adtType, Node: node}
	return checkedExpression{source: source, typ: adtType, token: token}
}

// unionMemberIndex returns the canonical index of member within union.
func unionMemberIndex(union, member compilerTypes.Type) int {
	members := compilerTypes.UnionMembers(union)
	for index := 0; index < members.Len(); index++ {
		if candidate, _ := members.At(index); compilerTypes.Equal(candidate, member) {
			return index
		}
	}
	return -1
}

// variantPayloadPlace resolves member access on a variant-narrowed ADT
// binding, wrapping the read in AdtPayloadExpression so the generator only
// reads the payload after the tag proof.
func variantPayloadPlace(receiver checkedExpression, property lexer.Token) checkedExpression {
	if receiver.variant == nil {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, "ADT payload fields are only accessible inside a narrowed match arm"))}
	}
	memberIndex := -1
	for index := range receiver.variant.Payload {
		if receiver.variant.Payload[index].Name == property.Lexeme {
			memberIndex = index
			break
		}
	}
	if memberIndex < 0 {
		return checkedExpression{token: property, diagnostic: diagnosticAt(typeErrorAt(property, fmt.Sprintf("%s has no field named %s", receiver.variant.Name, property.Lexeme)))}
	}
	member := receiver.variant.Payload[memberIndex]
	receiverNode := receiver.source.Node
	node := Expression{
		Kind:         AdtPayloadExpression,
		Operand:      &receiverNode,
		OperandType:  receiver.storageType,
		ResultType:   member.Type,
		VariantIndex: adtVariantIndex(receiver.storageType, receiver.variant.Name),
		MemberIndex:  memberIndex,
	}
	source := Operand{Kind: ExpressionOperand, Type: member.Type, Node: node}
	return checkedExpression{source: source, typ: member.Type, token: property}
}

// checkMatchExpression checks a match expression: the scrutinee evaluates
// once, patterns are validated against the mode, arms narrow a named
// scrutinee, and exhaustiveness and arm typing are enforced.
func checkMatchExpression(expression parser.MatchExpression, context expressionContext, ctx checkContext) checkedExpression {
	scrutinee := checkValue(expression.Scrutinee, ctx)
	if diagnostics := initializerDiagnostics(scrutinee); len(diagnostics) > 0 {
		return checkedExpression{token: expression.Keyword, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	scrutineeType := scrutinee.typ
	if scrutineeType == (compilerTypes.Type{}) {
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "match scrutinee does not produce a value"))}
	}
	isADT := compilerTypes.IsADT(scrutineeType)
	isUnion := compilerTypes.IsUnion(scrutineeType)
	isBool := compilerTypes.Equal(scrutineeType, compilerTypes.Bool)

	remaining := make(map[string]bool)
	if isADT {
		for _, variant := range scrutineeType.Adt.Variants {
			remaining[variant.Name] = true
		}
	} else if isUnion {
		members := compilerTypes.UnionMembers(scrutineeType)
		for index := 0; index < members.Len(); index++ {
			member, _ := members.At(index)
			remaining[member.Name] = true
		}
	} else if isBool && !expression.TypeMode {
		remaining["true"] = true
		remaining["false"] = true
	}

	scrutineeNode := expressionNode(scrutinee.source)
	armResults := make([]Operand, 0, len(expression.Arms))
	armTags := make([]int, 0, len(expression.Arms))
	var resultType compilerTypes.Type
	hasResult := false
	for armIndex, arm := range expression.Arms {
		switch pattern := arm.Pattern.(type) {
		case parser.ElsePattern:
			if armIndex != len(expression.Arms)-1 {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "else must be the final match arm"))}
			}
			for name := range remaining {
				delete(remaining, name)
			}
			armTags = append(armTags, -1)
			armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, nil, nil, context, ctx)
			if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
				return checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
				return checkedExpression{token: arm.Then, diagnostic: diagnosticAt(typeErrorAt(arm.Then, "match arm result types do not agree"))}
			}
			resultType, hasResult = armResult.typ, true
			armResults = append(armResults, armResult.source)
		case parser.BoolPattern:
			if expression.TypeMode {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "value patterns are not valid in type mode"))}
			}
			if !isBool {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "match pattern does not belong to the scrutinee type"))}
			}
			tag := 0
			name := "false"
			if pattern.Token.Kind == lexer.True {
				tag = 1
				name = "true"
			}
			if !remaining[name] {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "duplicate or unreachable match pattern"))}
			}
			delete(remaining, name)
			armTags = append(armTags, tag)
			armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, nil, nil, context, ctx)
			if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
				return checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
				return checkedExpression{token: arm.Then, diagnostic: diagnosticAt(typeErrorAt(arm.Then, "match arm result types do not agree"))}
			}
			resultType, hasResult = armResult.typ, true
			armResults = append(armResults, armResult.source)
		case parser.VariantPattern:
			if !expression.TypeMode {
				return checkedExpression{token: pattern.Variant, diagnostic: diagnosticAt(typeErrorAt(pattern.Variant, "type and variant patterns are not valid in value mode"))}
			}
			adtVariant, ok := ctx.typeEnvironment.AdtVariant(pattern.Owner.Lexeme, pattern.Variant.Lexeme)
			if !ok && isADT && ctx.names.generics != nil {
				index := adtVariantIndex(scrutineeType, pattern.Variant.Lexeme)
				if index >= 0 {
					if open, generic := ctx.names.generics.adtOpen[scrutineeType.Adt]; generic && open.Name == pattern.Owner.Lexeme {
						adtVariant = &scrutineeType.Adt.Variants[index]
						ok = true
					}
				}
			}
			if !ok {
				return checkedExpression{token: pattern.Variant, diagnostic: diagnosticAt(typeErrorAt(pattern.Variant, fmt.Sprintf("unknown qualified variant %s.%s", pattern.Owner.Lexeme, pattern.Variant.Lexeme)))}
			}
			ownerMatches := isADT && compilerTypes.Equal(scrutineeType, lookupADTType(ctx.typeEnvironment, pattern.Owner.Lexeme))
			if !ownerMatches && isADT && ctx.names.generics != nil {
				if open, generic := ctx.names.generics.adtOpen[scrutineeType.Adt]; generic && open.Name == pattern.Owner.Lexeme {
					ownerMatches = true
				}
			}
			if !isADT || !ownerMatches {
				return checkedExpression{token: pattern.Variant, diagnostic: diagnosticAt(typeErrorAt(pattern.Variant, "match pattern does not belong to the scrutinee type"))}
			}
			if !remaining[adtVariant.Name] {
				return checkedExpression{token: pattern.Variant, diagnostic: diagnosticAt(typeErrorAt(pattern.Variant, "duplicate or unreachable match pattern"))}
			}
			delete(remaining, adtVariant.Name)
			armTags = append(armTags, adtVariantIndex(scrutineeType, adtVariant.Name))
			armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, adtVariant, nil, context, ctx)
			if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
				return checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
				return checkedExpression{token: arm.Then, diagnostic: diagnosticAt(typeErrorAt(arm.Then, "match arm result types do not agree"))}
			}
			resultType, hasResult = armResult.typ, true
			armResults = append(armResults, armResult.source)
		case parser.TypePattern:
			if !expression.TypeMode {
				return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "type and variant patterns are not valid in value mode"))}
			}
			memberUse, diagnostic := resolveUnionMemberUse(pattern.Type, expression.Keyword, ctx.typeEnvironment, ctx.names.generics)
			if diagnostic != nil {
				return checkedExpression{token: expression.Keyword, diagnostic: diagnostic}
			}
			member := memberUse.Type
			if isUnion {
				if !compilerTypes.ContainsUnionMember(scrutineeType, member) {
					return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "match pattern does not belong to the scrutinee type"))}
				}
				if !remaining[member.Name] {
					return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "duplicate or unreachable match pattern"))}
				}
				delete(remaining, member.Name)
				armTags = append(armTags, unionMemberIndex(scrutineeType, member))
			} else if !compilerTypes.Equal(scrutineeType, member) {
				return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "match pattern does not belong to the scrutinee type"))}
			} else {
				armTags = append(armTags, -2)
			}
			armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, nil, &member, context, ctx)
			if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
				return checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
				return checkedExpression{token: arm.Then, diagnostic: diagnosticAt(typeErrorAt(arm.Then, "match arm result types do not agree"))}
			}
			resultType, hasResult = armResult.typ, true
			armResults = append(armResults, armResult.source)
		}
	}
	if len(remaining) > 0 {
		missing := ""
		// Report the first missing member in canonical declaration order so
		// the diagnostic is deterministic; map iteration order is not.
		members := compilerTypes.UnionMembers(scrutineeType)
		for index := 0; index < members.Len(); index++ {
			if member, _ := members.At(index); remaining[member.Name] {
				missing = member.Name
				break
			}
		}
		if missing == "" {
			remainingNames := make([]string, 0, len(remaining))
			for name := range remaining {
				remainingNames = append(remainingNames, name)
			}
			slices.Sort(remainingNames)
			if len(remainingNames) > 0 {
				missing = remainingNames[0]
			}
		}
		if isADT {
			missing = scrutineeType.Name + "." + missing
		}
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, fmt.Sprintf("match is not exhaustive; missing %s", missing)))}
	}
	node := Expression{
		Kind:        MatchExpression,
		Operand:     &scrutineeNode,
		OperandType: scrutineeType,
		ResultType:  resultType,
		Arguments:   armResults,
		MemberMap:   armTags,
	}
	source := Operand{Kind: ExpressionOperand, Type: resultType, Node: node}
	return checkedExpression{source: source, typ: resultType, token: expression.Keyword}
}

// checkMatchArm checks one arm body in a child scope, narrowing a named
// scrutinee binding to the variant or exact member.
func checkMatchArm(scrutineeExpression parser.Expression, arm parser.MatchArm, scrutinee checkedExpression, variant *compilerTypes.AdtVariant, member *compilerTypes.Type, context expressionContext, ctx checkContext) initializerValue {
	child := ctx.names.child()
	if variable, isVariable := scrutineeExpression.(parser.VariableExpression); isVariable && ctx.names.flow != nil {
		if bound, status := ctx.names.lookup(variable.Name.Lexeme); status == nameFound && bound.id != 0 {
			childFlow := ctx.names.flow.clone()
			if variant != nil {
				childFlow.narrowVariant(bound.id, variant)
			} else if member != nil {
				childFlow.narrow(bound.id, *member)
			}
			child.flow = childFlow
		}
	}
	return checkInitializer(arm.Expression, context.expected, arm.Then, checkContext{names: child, typeEnvironment: ctx.typeEnvironment})
}

func lookupADTType(typeEnvironment *compilerTypes.Environment, name string) compilerTypes.Type {
	typ, _ := typeEnvironment.Lookup(name)
	return typ
}
