package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// checkADTDeclaration validates and registers one ADT declaration.
func checkADTDeclaration(declaration parser.TypeDeclaration, target parser.AdtDefinitionExpression, typeEnvironment *compilerTypes.Environment, environment *scope) (TypeDeclaration, compilerTypes.Diagnostics) {
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

	typeEnvironment.BeginADT(name, declaration.Name.Line, declaration.Name.Column)
	variants := make([]compilerTypes.AdtVariant, 0, len(target.Variants))
	for _, variant := range target.Variants {
		resolved := compilerTypes.AdtVariant{Name: variant.Name.Lexeme}
		if variant.Payload != nil {
			payload, payloadDiagnostics := resolveADTPayload(name, *variant.Payload, typeEnvironment, environment.generics)
			diagnostics = append(diagnostics, payloadDiagnostics...)
			resolved.Payload = payload
		}
		variants = append(variants, resolved)
	}
	if len(diagnostics) > 0 {
		typeEnvironment.AbandonADT(name)
		return TypeDeclaration{Name: name, SourceLine: declaration.Name.Line, SourceColumn: declaration.Name.Column}, diagnostics
	}
	completed := typeEnvironment.CompleteADT(name, variants)
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
		if resolvedUse.Type.Signature != nil {
			diagnostics = append(diagnostics, typeErrorAt(member.Name, "Fun<…> ADT payload fields are not supported"))
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
func resolveVariantOwner(owner string, ownerArguments []parser.TypeExpression, expectedType compilerTypes.Type, environment *scope, typeEnvironment *compilerTypes.Environment) (compilerTypes.Type, *compilerTypes.AdtVariant, *compilerTypes.Diagnostic) {
	if adtType, ok := typeEnvironment.Lookup(owner); ok && adtType.Adt != nil {
		return adtType, nil, nil
	}
	if environment.generics == nil {
		return compilerTypes.Type{}, nil, nil
	}
	open, generic := environment.generics.types[owner]
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
			use, diagnostic := resolveTypeUse(argument, lexer.Token{}, typeEnvironment, environment.generics)
			if diagnostic != nil {
				return compilerTypes.Type{}, nil, diagnostic
			}
			arguments = append(arguments, use.Type)
		}
	} else if expectedType.Adt != nil {
		if expectedOpen := environment.generics.adtOpen[expectedType.Adt]; expectedOpen == open {
			arguments = environment.generics.adtArguments[expectedType.Adt]
		}
	}
	if len(arguments) == 0 {
		return compilerTypes.Type{}, nil, &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Message:  fmt.Sprintf("cannot infer generic parameter for %s", owner),
		}
	}
	specialized, diagnostic := specializeADTType(open, arguments, lexer.Token{}, typeEnvironment, environment.generics)
	if diagnostic != nil {
		return compilerTypes.Type{}, nil, diagnostic
	}
	return specialized, nil, nil
}

// checkQualifiedVariant resolves a record-variant constructor.
func checkQualifiedVariant(expression parser.QualifiedVariantExpression, expectedType compilerTypes.Type, environment *scope, typeEnvironment *compilerTypes.Environment) initializerValue {
	// RFC 0034 Task 5: an import-alias owner routes to the target module's
	// exported ADT variants before ordinary owner resolution.
	if target, ok := environment.importAliasTarget(expression.Owner.Lexeme); ok {
		return checkModuleVariantConstructor(expression, target, environment, typeEnvironment)
	}
	adtType, _, ownerDiagnostic := resolveVariantOwner(expression.Owner.Lexeme, expression.OwnerArguments, expectedType, environment, typeEnvironment)
	if ownerDiagnostic != nil {
		return initializerValue{token: expression.Variant, diagnostic: ownerDiagnostic}
	}
	variant, ok := typeEnvironment.AdtVariant(expression.Owner.Lexeme, expression.Variant.Lexeme)
	if !ok && adtType.Adt != nil {
		index := adtVariantIndex(adtType, expression.Variant.Lexeme)
		if index >= 0 {
			variant = &adtType.Adt.Variants[index]
			ok = true
		}
	}
	if !ok {
		return initializerValue{token: expression.Variant, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Variant.Line,
			Column:   expression.Variant.Column,
			Message:  fmt.Sprintf("unknown qualified variant %s.%s", expression.Owner.Lexeme, expression.Variant.Lexeme),
		}}
	}
	return buildVariantConstructor(expression, adtType, variant, environment, typeEnvironment)
}

// checkModuleVariantConstructor resolves Owner.Variant {...} where Owner is an
// import alias: the variant must belong to an exported ADT of the target
// module.
func checkModuleVariantConstructor(expression parser.QualifiedVariantExpression, target string, environment *scope, typeEnvironment *compilerTypes.Environment) initializerValue {
	adtType, variant, ok := environment.registry.findExportedADTVariant(target, expression.Variant.Lexeme)
	if !ok {
		diagnostic := privateToModuleDiagnostic(expression.Variant, expression.Variant.Lexeme, target)
		return initializerValue{token: expression.Variant, diagnostic: &diagnostic}
	}
	return buildVariantConstructor(expression, adtType, variant, environment, typeEnvironment)
}

// buildVariantConstructor checks the payload of one resolved record-variant
// constructor and builds its AdtConstructExpression. Unit and payload shapes
// are checked against the variant record exactly once, whichever path
// resolved it.
func buildVariantConstructor(expression parser.QualifiedVariantExpression, adtType compilerTypes.Type, variant *compilerTypes.AdtVariant, environment *scope, typeEnvironment *compilerTypes.Environment) initializerValue {
	if expression.Payload == nil {
		return adtUnitVariant(adtType, variant, expression.Variant)
	}
	if len(variant.Payload) == 0 {
		return initializerValue{token: expression.Variant, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError,
			Stage:    "checker",
			Line:     expression.Variant.Line,
			Column:   expression.Variant.Column,
			Message:  fmt.Sprintf("%s.%s is a unit variant and takes no payload", expression.Owner.Lexeme, expression.Variant.Lexeme),
		}}
	}
	seen := make(map[string]bool, len(*expression.Payload))
	arguments := make([]Operand, 0, len(*expression.Payload))
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
		checked := checkInitializer(initializer.Value, field.Use, initializer.Name, environment, typeEnvironment)
		if nestedDiagnostics := initializerDiagnostics(checked); len(nestedDiagnostics) > 0 {
			diagnostics = append(diagnostics, nestedDiagnostics...)
			continue
		}
		if !assignable(field.Type, checked.typ) {
			diagnostics = append(diagnostics, typeMismatchDiagnostic(field.Type, checked.typ, checked.token))
			continue
		}
		arguments = append(arguments, checked.source)
	}
	for index := range variant.Payload {
		if !seen[variant.Payload[index].Name] {
			diagnostics = append(diagnostics, typeErrorAt(expression.Variant, fmt.Sprintf("variant constructor requires the payload field %s", variant.Payload[index].Name)))
		}
	}
	if len(diagnostics) > 0 {
		return initializerValue{token: expression.Variant, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	node := Expression{
		Kind:         AdtConstructExpression,
		OperandType:  adtType,
		ResultType:   adtType,
		VariantIndex: adtVariantIndex(adtType, variant.Name),
		Arguments:    arguments,
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
func checkUnitVariant(owner, variant lexer.Token, expectedType compilerTypes.Type, environment *scope, typeEnvironment *compilerTypes.Environment) (*checkedExpression, *compilerTypes.Diagnostic) {
	adtType, _, ownerDiagnostic := resolveVariantOwner(owner.Lexeme, nil, expectedType, environment, typeEnvironment)
	if ownerDiagnostic != nil {
		return nil, ownerDiagnostic
	}
	adtVariant, ok := typeEnvironment.AdtVariant(owner.Lexeme, variant.Lexeme)
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
	for index, candidate := range compilerTypes.UnionMembers(union) {
		if compilerTypes.Equal(candidate, member) {
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
		return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: property.Line, Column: property.Column,
			Message: "ADT payload fields are only accessible inside a narrowed match arm",
		}}
	}
	memberIndex := -1
	for index := range receiver.variant.Payload {
		if receiver.variant.Payload[index].Name == property.Lexeme {
			memberIndex = index
			break
		}
	}
	if memberIndex < 0 {
		return checkedExpression{token: property, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: property.Line, Column: property.Column,
			Message: fmt.Sprintf("%s has no field named %s", receiver.variant.Name, property.Lexeme),
		}}
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
func checkMatchExpression(expression parser.MatchExpression, context expressionContext, environment *scope, typeEnvironment *compilerTypes.Environment) checkedExpression {
	scrutinee := checkValue(expression.Scrutinee, environment, typeEnvironment)
	if diagnostics := initializerDiagnostics(scrutinee); len(diagnostics) > 0 {
		return checkedExpression{token: expression.Keyword, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
	}
	scrutineeType := scrutinee.typ
	if scrutineeType == (compilerTypes.Type{}) {
		return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Keyword.Line, Column: expression.Keyword.Column,
			Message: "match scrutinee does not produce a value",
		}}
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
		for _, member := range compilerTypes.UnionMembers(scrutineeType) {
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
				return checkedExpression{token: pattern.Token, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: pattern.Token.Line, Column: pattern.Token.Column,
					Message: "else must be the final match arm",
				}}
			}
			for name := range remaining {
				delete(remaining, name)
			}
			armTags = append(armTags, -1)
			armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, nil, nil, context, environment, typeEnvironment)
			if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
				return checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
				return checkedExpression{token: arm.Then, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: arm.Then.Line, Column: arm.Then.Column,
					Message: "match arm result types do not agree",
				}}
			}
			resultType, hasResult = armResult.typ, true
			armResults = append(armResults, armResult.source)
		case parser.BoolPattern:
			if expression.TypeMode {
				return checkedExpression{token: pattern.Token, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: pattern.Token.Line, Column: pattern.Token.Column,
					Message: "value patterns are not valid in type mode",
				}}
			}
			if !isBool {
				return checkedExpression{token: pattern.Token, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: pattern.Token.Line, Column: pattern.Token.Column,
					Message: "match pattern does not belong to the scrutinee type",
				}}
			}
			tag := 0
			name := "false"
			if pattern.Token.Kind == lexer.True {
				tag = 1
				name = "true"
			}
			if !remaining[name] {
				return checkedExpression{token: pattern.Token, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: pattern.Token.Line, Column: pattern.Token.Column,
					Message: "duplicate or unreachable match pattern",
				}}
			}
			delete(remaining, name)
			armTags = append(armTags, tag)
			armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, nil, nil, context, environment, typeEnvironment)
			if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
				return checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
				return checkedExpression{token: arm.Then, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: arm.Then.Line, Column: arm.Then.Column,
					Message: "match arm result types do not agree",
				}}
			}
			resultType, hasResult = armResult.typ, true
			armResults = append(armResults, armResult.source)
		case parser.VariantPattern:
			if !expression.TypeMode {
				return checkedExpression{token: pattern.Variant, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: pattern.Variant.Line, Column: pattern.Variant.Column,
					Message: "type and variant patterns are not valid in value mode",
				}}
			}
			adtVariant, ok := typeEnvironment.AdtVariant(pattern.Owner.Lexeme, pattern.Variant.Lexeme)
			if !ok && isADT && environment.generics != nil {
				index := adtVariantIndex(scrutineeType, pattern.Variant.Lexeme)
				if index >= 0 {
					if open, generic := environment.generics.adtOpen[scrutineeType.Adt]; generic && open.Name == pattern.Owner.Lexeme {
						adtVariant = &scrutineeType.Adt.Variants[index]
						ok = true
					}
				}
			}
			if !ok {
				return checkedExpression{token: pattern.Variant, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: pattern.Variant.Line, Column: pattern.Variant.Column,
					Message: fmt.Sprintf("unknown qualified variant %s.%s", pattern.Owner.Lexeme, pattern.Variant.Lexeme),
				}}
			}
			ownerMatches := isADT && compilerTypes.Equal(scrutineeType, lookupADTType(typeEnvironment, pattern.Owner.Lexeme))
			if !ownerMatches && isADT && environment.generics != nil {
				if open, generic := environment.generics.adtOpen[scrutineeType.Adt]; generic && open.Name == pattern.Owner.Lexeme {
					ownerMatches = true
				}
			}
			if !isADT || !ownerMatches {
				return checkedExpression{token: pattern.Variant, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: pattern.Variant.Line, Column: pattern.Variant.Column,
					Message: "match pattern does not belong to the scrutinee type",
				}}
			}
			if !remaining[adtVariant.Name] {
				return checkedExpression{token: pattern.Variant, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: pattern.Variant.Line, Column: pattern.Variant.Column,
					Message: "duplicate or unreachable match pattern",
				}}
			}
			delete(remaining, adtVariant.Name)
			armTags = append(armTags, adtVariantIndex(scrutineeType, adtVariant.Name))
			armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, adtVariant, nil, context, environment, typeEnvironment)
			if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
				return checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
				return checkedExpression{token: arm.Then, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: arm.Then.Line, Column: arm.Then.Column,
					Message: "match arm result types do not agree",
				}}
			}
			resultType, hasResult = armResult.typ, true
			armResults = append(armResults, armResult.source)
		case parser.TypePattern:
			if !expression.TypeMode {
				return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: expression.Keyword.Line, Column: expression.Keyword.Column,
					Message: "type and variant patterns are not valid in value mode",
				}}
			}
			memberUse, diagnostic := resolveUnionMemberUse(pattern.Type, expression.Keyword, typeEnvironment, environment.generics)
			if diagnostic != nil {
				return checkedExpression{token: expression.Keyword, diagnostic: diagnostic}
			}
			member := memberUse.Type
			if isUnion {
				if !compilerTypes.ContainsUnionMember(scrutineeType, member) {
					return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
						Category: compilerTypes.TypeError, Stage: "checker",
						Line: expression.Keyword.Line, Column: expression.Keyword.Column,
						Message: "match pattern does not belong to the scrutinee type",
					}}
				}
				if !remaining[member.Name] {
					return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
						Category: compilerTypes.TypeError, Stage: "checker",
						Line: expression.Keyword.Line, Column: expression.Keyword.Column,
						Message: "duplicate or unreachable match pattern",
					}}
				}
				delete(remaining, member.Name)
				armTags = append(armTags, unionMemberIndex(scrutineeType, member))
			} else if !compilerTypes.Equal(scrutineeType, member) {
				return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: expression.Keyword.Line, Column: expression.Keyword.Column,
					Message: "match pattern does not belong to the scrutinee type",
				}}
			} else {
				armTags = append(armTags, -2)
			}
			armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, nil, &member, context, environment, typeEnvironment)
			if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
				return checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			}
			if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
				return checkedExpression{token: arm.Then, diagnostic: &compilerTypes.Diagnostic{
					Category: compilerTypes.TypeError, Stage: "checker",
					Line: arm.Then.Line, Column: arm.Then.Column,
					Message: "match arm result types do not agree",
				}}
			}
			resultType, hasResult = armResult.typ, true
			armResults = append(armResults, armResult.source)
		}
	}
	if len(remaining) > 0 {
		missing := ""
		for name := range remaining {
			missing = name
			break
		}
		if isADT {
			missing = scrutineeType.Name + "." + missing
		}
		return checkedExpression{token: expression.Keyword, diagnostic: &compilerTypes.Diagnostic{
			Category: compilerTypes.TypeError, Stage: "checker",
			Line: expression.Keyword.Line, Column: expression.Keyword.Column,
			Message: fmt.Sprintf("match is not exhaustive; missing %s", missing),
		}}
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
func checkMatchArm(scrutineeExpression parser.Expression, arm parser.MatchArm, scrutinee checkedExpression, variant *compilerTypes.AdtVariant, member *compilerTypes.Type, context expressionContext, environment *scope, typeEnvironment *compilerTypes.Environment) initializerValue {
	child := environment.child()
	if variable, isVariable := scrutineeExpression.(parser.VariableExpression); isVariable && environment.flow != nil {
		if bound, status := environment.lookup(variable.Name.Lexeme); status == nameFound && bound.id != 0 {
			childFlow := environment.flow.clone()
			if variant != nil {
				childFlow.narrowVariant(bound.id, variant)
			} else if member != nil {
				childFlow.narrow(bound.id, *member)
			}
			child.flow = childFlow
		}
	}
	return checkInitializer(arm.Expression, context.expected, arm.Then, child, typeEnvironment)
}

func lookupADTType(typeEnvironment *compilerTypes.Environment, name string) compilerTypes.Type {
	typ, _ := typeEnvironment.Lookup(name)
	return typ
}
