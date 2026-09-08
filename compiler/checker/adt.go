package checker

import (
	"fmt"

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

// checkQualifiedVariantCall recognizes and checks call.Callee shaped as
// Owner.Variant(...) or Owner<Args>.Variant(...) where Owner names an ADT (or
// a generic ADT template): the current construction syntax for ADT variants.
// The second result is false when the callee does not name any ADT variant at
// all, so the caller can fall through to ordinary method/property dispatch.
// An import-alias owner is left to checkModuleVariantConstructorCall.
func checkQualifiedVariantCall(call parser.CallExpression, callee parser.PropertyExpression, expectedType compilerTypes.Type, ctx checkContext) (initializerValue, bool) {
	owner, isVariable := callee.Receiver.(parser.VariableExpression)
	if !isVariable {
		return initializerValue{}, false
	}
	if _, isAlias := ctx.names.importAliasTarget(owner.Name.Lexeme); isAlias {
		return initializerValue{}, false
	}
	adtType, _, ownerDiagnostic := resolveVariantOwner(owner.Name.Lexeme, call.TypeArguments, expectedType, owner.Name, ctx)
	if ownerDiagnostic != nil {
		return initializerValue{token: callee.Property, diagnostic: ownerDiagnostic}, true
	}
	if adtType == (compilerTypes.Type{}) {
		return initializerValue{}, false
	}
	variant, ok := ctx.typeEnvironment.AdtVariant(owner.Name.Lexeme, callee.Property.Lexeme)
	if !ok {
		index := adtVariantIndex(adtType, callee.Property.Lexeme)
		if index < 0 {
			diagnostic := typeErrorAt(callee.Property, fmt.Sprintf("unknown qualified variant %s.%s", owner.Name.Lexeme, callee.Property.Lexeme))
			return initializerValue{token: callee.Property, diagnostic: &diagnostic}, true
		}
		variant = &adtType.Adt.Variants[index]
	}
	return checkVariantConstructorCall(call, owner.Name.Lexeme, adtType, variant, callee.Property, ctx), true
}

// checkModuleVariantConstructorCall resolves Owner.Variant(...) where Owner is
// an import alias: the variant must belong to an exported ADT of the target
// module. The second result is false when the target module exports no such
// variant, so the caller falls through to the ordinary private-to-module
// diagnostic used for an unresolved qualified call.
func checkModuleVariantConstructorCall(call parser.CallExpression, ownerName string, property lexer.Token, target string, ctx checkContext) (initializerValue, bool) {
	adtType, variant, ok := ctx.names.registry.findExportedADTVariant(target, property.Lexeme)
	if !ok {
		return initializerValue{}, false
	}
	return checkVariantConstructorCall(call, ownerName, adtType, variant, property, ctx), true
}

// checkVariantConstructorCall checks one resolved ADT-variant constructor
// call's arguments and builds its AdtConstructExpression. Unit and payload
// shapes are checked against the variant record exactly once, whichever path
// resolved it. ownerName is the written owner spelling, used only for
// diagnostic text.
func checkVariantConstructorCall(call parser.CallExpression, ownerName string, adtType compilerTypes.Type, variant *compilerTypes.AdtVariant, variantToken lexer.Token, ctx checkContext) initializerValue {
	if len(variant.Payload) == 0 {
		if len(call.Arguments) != 0 {
			diagnostic := typeErrorAt(variantToken, fmt.Sprintf("%s.%s takes no arguments", ownerName, variant.Name))
			return initializerValue{token: variantToken, diagnostic: &diagnostic}
		}
		return adtUnitVariant(adtType, variant, variantToken)
	}
	if len(call.Arguments) == 0 {
		diagnostic := typeErrorAt(variantToken, fmt.Sprintf("%s.%s requires a payload", ownerName, variant.Name))
		return initializerValue{token: variantToken, diagnostic: &diagnostic}
	}
	seen := make(map[string]bool, len(call.Arguments))
	// byField and evaluationOrder are populated in written order, but
	// Arguments below is assembled in variant.Payload declaration order:
	// renderAdtConstruct indexes Arguments positionally against
	// variant.Payload, so declaration order is what the checked tree must
	// carry. evaluationOrder separately records, as indices into the
	// declaration-ordered Arguments, the order fields were actually
	// written in, so generation can still sequence side effects in
	// written order without reordering the field assignment itself.
	byField := make(map[string]Operand, len(call.Arguments))
	evaluationOrder := make([]int, 0, len(call.Arguments))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	for index, argumentExpression := range call.Arguments {
		label := call.ArgumentLabels[index]
		if label == nil {
			diagnostics = append(diagnostics, typeErrorAt(tokenOf(argumentExpression), "constructor arguments must be named"))
			continue
		}
		field, exists := variantField(variant, label.Lexeme)
		if !exists {
			diagnostics = append(diagnostics, typeErrorAt(*label, fmt.Sprintf("%s has no field named %s", variant.Name, label.Lexeme)))
			continue
		}
		if seen[field.Name] {
			diagnostics = append(diagnostics, typeErrorAt(*label, fmt.Sprintf("%s initializes field %s more than once", variant.Name, field.Name)))
			continue
		}
		seen[field.Name] = true
		checked := checkInitializer(argumentExpression, field.Use, *label, ctx)
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
			diagnostics = append(diagnostics, typeErrorAt(variantToken, fmt.Sprintf("variant constructor requires the payload field %s", variant.Payload[index].Name)))
		}
	}
	if len(diagnostics) > 0 {
		return initializerValue{token: variantToken, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
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
	return initializerValue{source: source, typ: adtType, token: variantToken}
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

// matchCase is one closed coverage case in table order: its canonical
// identity key, its lowering tag, and the resolved member or variant the arm
// narrows to. Bool cases key by value name, ADT cases by variant name within
// the one scrutinee ADT, and type cases by CanonicalKey, never Type.Name.
type matchCase struct {
	key     string
	tag     int
	member  compilerTypes.Type
	variant string
}

// matchCoverage is the one ordered table owning membership, remaining
// coverage, lowering tags, and first-missing order for a match. Covered
// flags are index-aligned with cases; no parallel name-keyed map exists.
type matchCoverage struct {
	cases   []matchCase
	covered []bool
	open    bool
}

// buildMatchCoverage constructs the closed domain for a scrutinee type.
// Value-mode Bool covers its two values; type mode covers ADT variants,
// canonical union members, or the one exact type. Any other value-mode type
// is an open domain where only else is a valid arm.
func buildMatchCoverage(scrutineeType compilerTypes.Type, typeMode bool) matchCoverage {
	isADT := compilerTypes.IsADT(scrutineeType)
	isUnion := compilerTypes.IsUnion(scrutineeType)
	isBool := compilerTypes.Equal(scrutineeType, compilerTypes.Bool)
	switch {
	case isADT:
		cases := make([]matchCase, 0, len(scrutineeType.Adt.Variants))
		for index, variant := range scrutineeType.Adt.Variants {
			cases = append(cases, matchCase{key: variant.Name, tag: index, variant: variant.Name})
		}
		return matchCoverage{cases: cases, covered: make([]bool, len(cases))}
	case isUnion:
		members := compilerTypes.UnionMembers(scrutineeType)
		cases := make([]matchCase, 0, members.Len())
		for index := 0; index < members.Len(); index++ {
			member, _ := members.At(index)
			cases = append(cases, matchCase{key: member.CanonicalKey, tag: unionMemberIndex(scrutineeType, member), member: member})
		}
		return matchCoverage{cases: cases, covered: make([]bool, len(cases))}
	case isBool && !typeMode:
		return matchCoverage{
			cases:   []matchCase{{key: "false", tag: 0}, {key: "true", tag: 1}},
			covered: make([]bool, 2),
		}
	case typeMode:
		return matchCoverage{
			cases:   []matchCase{{key: scrutineeType.CanonicalKey, tag: -2, member: scrutineeType}},
			covered: make([]bool, 1),
		}
	default:
		return matchCoverage{open: true}
	}
}

// cover marks one case covered, reporting false when it was already covered.
func (coverage *matchCoverage) cover(index int) bool {
	if coverage.covered[index] {
		return false
	}
	coverage.covered[index] = true
	return true
}

// coverAll marks every remaining case covered for a reachable else.
func (coverage *matchCoverage) coverAll() {
	for index := range coverage.covered {
		coverage.covered[index] = true
	}
}

// uncovered reports whether any closed case remains.
func (coverage *matchCoverage) uncovered() bool {
	for _, done := range coverage.covered {
		if !done {
			return true
		}
	}
	return false
}

// find returns the first case index with key, or -1.
func (coverage *matchCoverage) find(key string) int {
	for index := range coverage.cases {
		if coverage.cases[index].key == key {
			return index
		}
	}
	return -1
}

// firstMissing returns the first uncovered case in table order.
func (coverage *matchCoverage) firstMissing() matchCase {
	for index := range coverage.cases {
		if !coverage.covered[index] {
			return coverage.cases[index]
		}
	}
	return matchCase{}
}

// resolveDottedVariantArm resolves Owner.Name to an ADT variant through a
// local ADT owner, a generic-open ADT owner, or an import alias into the
// target module's exported variants, reusing the construction registry path.
// It reports whether the owner named any variant; membership against the
// scrutinee stays with the caller.
func resolveDottedVariantArm(pattern parser.DottedPattern, scrutineeType compilerTypes.Type, isADT bool, ctx checkContext) (*compilerTypes.AdtVariant, compilerTypes.Type, bool) {
	if adtVariant, ok := ctx.typeEnvironment.AdtVariant(pattern.Owner.Lexeme, pattern.Name.Lexeme); ok {
		owner, _ := ctx.typeEnvironment.Lookup(pattern.Owner.Lexeme)
		return adtVariant, owner, true
	}
	if isADT && ctx.names.generics != nil {
		if index := adtVariantIndex(scrutineeType, pattern.Name.Lexeme); index >= 0 {
			if open, generic := ctx.names.generics.adtOpen[scrutineeType.Adt]; generic && open.Name == pattern.Owner.Lexeme {
				return &scrutineeType.Adt.Variants[index], scrutineeType, true
			}
		}
	}
	if target, ok := ctx.names.importAliasTarget(pattern.Owner.Lexeme); ok {
		if adtType, adtVariant, ok := ctx.names.registry.findExportedADTVariant(target, pattern.Name.Lexeme); ok {
			return adtVariant, adtType, true
		}
	}
	return nil, compilerTypes.Type{}, false
}

// matchQualifiedNominal renders one nominal case for the exhaustiveness
// diagnostic: an imported nominal through the current module's
// lexicographically first alias, and a local, builtin, or otherwise
// alias-less nominal by short name.
func matchQualifiedNominal(name, moduleID string, ctx checkContext) string {
	if moduleID == "" || moduleID == ctx.names.moduleID {
		return name
	}
	if alias, ok := ctx.names.registry.aliasForModule(ctx.names.moduleID, moduleID); ok {
		return alias + "." + name
	}
	return name
}

// matchVariantOwnerName renders one ADT owner for the exhaustiveness
// diagnostic: an imported ADT through the current module's
// lexicographically first alias, and a local ADT by short name.
func matchVariantOwnerName(adtType compilerTypes.Type, ctx checkContext) string {
	if adtType.Adt == nil {
		return adtType.Name
	}
	moduleID := adtType.Adt.ModuleID
	if moduleID == "" || moduleID == ctx.names.moduleID {
		return adtType.Adt.Name
	}
	if alias, ok := ctx.names.registry.aliasForModule(ctx.names.moduleID, moduleID); ok {
		return alias
	}
	return adtType.Adt.Name
}

// matchMissingName renders one uncovered case: constructed types qualify
// their nominal leaves, and every other member renders by short or
// alias-qualified name.
func matchMissingName(member compilerTypes.Type, ctx checkContext) string {
	if member.Element != nil {
		constructor := "Ptr"
		if member.PointeeWritable {
			constructor = "MutPtr"
		}
		return constructor + "<" + matchMissingName(*member.Element, ctx) + ">"
	}
	if member.Object != nil {
		return matchQualifiedNominal(member.Object.Name, member.Object.ModuleID, ctx)
	}
	if member.Adt != nil {
		return matchQualifiedNominal(member.Name, member.Adt.ModuleID, ctx)
	}
	return member.Name
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
	coverage := buildMatchCoverage(scrutineeType, expression.TypeMode)

	scrutineeNode := expressionNode(scrutinee.source)
	armResults := make([]Operand, 0, len(expression.Arms))
	armTags := make([]int, 0, len(expression.Arms))
	var resultType compilerTypes.Type
	hasResult := false
	// finishArm checks one resolved arm body and enforces result agreement,
	// recording its lowering tag. A non-nil return is the arm diagnostic.
	finishArm := func(arm parser.MatchArm, tag int, variant *compilerTypes.AdtVariant, member *compilerTypes.Type) *checkedExpression {
		armTags = append(armTags, tag)
		armResult := checkMatchArm(expression.Scrutinee, arm, scrutinee, variant, member, context, ctx)
		if diagnostics := initializerDiagnostics(armResult); len(diagnostics) > 0 {
			failed := checkedExpression{token: arm.Then, diagnostics: diagnostics, diagnostic: &diagnostics[0]}
			return &failed
		}
		if hasResult && !compilerTypes.Equal(resultType, armResult.typ) {
			failed := checkedExpression{token: arm.Then, diagnostic: diagnosticAt(typeErrorAt(arm.Then, "match arm result types do not agree"))}
			return &failed
		}
		resultType, hasResult = armResult.typ, true
		armResults = append(armResults, armResult.source)
		return nil
	}
	for armIndex, arm := range expression.Arms {
		switch pattern := arm.Pattern.(type) {
		case parser.ElsePattern:
			if armIndex != len(expression.Arms)-1 {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "else must be the final match arm"))}
			}
			if !coverage.open && !coverage.uncovered() {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "duplicate or unreachable match pattern"))}
			}
			coverage.coverAll()
			if failed := finishArm(arm, -1, nil, nil); failed != nil {
				return *failed
			}
		case parser.BoolPattern:
			if expression.TypeMode {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "value patterns are not valid in type mode"))}
			}
			if !isBool {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "match pattern does not belong to the scrutinee type"))}
			}
			name := "false"
			if pattern.Token.Kind == lexer.True {
				name = "true"
			}
			index := coverage.find(name)
			if index < 0 || !coverage.cover(index) {
				return checkedExpression{token: pattern.Token, diagnostic: diagnosticAt(typeErrorAt(pattern.Token, "duplicate or unreachable match pattern"))}
			}
			if failed := finishArm(arm, coverage.cases[index].tag, nil, nil); failed != nil {
				return *failed
			}
		case parser.DottedPattern:
			if !expression.TypeMode {
				return checkedExpression{token: pattern.Name, diagnostic: diagnosticAt(typeErrorAt(pattern.Name, "type and variant patterns are not valid in value mode"))}
			}
			if _, isAlias := ctx.names.importAliasTarget(pattern.Owner.Lexeme); isAlias && !isADT {
				// A union or exact scrutinee reads Owner.Name as the
				// import-qualified type through the existing resolver, so
				// visibility and unknown-name diagnostics stay owned by
				// module resolution.
				memberUse, diagnostic := resolveUnionMemberUse(parser.QualifiedTypeExpression{Module: pattern.Owner, Names: []lexer.Token{pattern.Name}}, expression.Keyword, ctx.typeEnvironment, ctx.names.generics)
				if diagnostic != nil {
					return checkedExpression{token: expression.Keyword, diagnostic: diagnostic}
				}
				member := memberUse.Type
				if isUnion {
					if !compilerTypes.ContainsUnionMember(scrutineeType, member) {
						return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "match pattern does not belong to the scrutinee type"))}
					}
					index := coverage.find(member.CanonicalKey)
					if index < 0 || !coverage.cover(index) {
						return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "duplicate or unreachable match pattern"))}
					}
					if failed := finishArm(arm, coverage.cases[index].tag, nil, &member); failed != nil {
						return *failed
					}
				} else {
					if !compilerTypes.Equal(scrutineeType, member) {
						return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "match pattern does not belong to the scrutinee type"))}
					}
					index := coverage.find(scrutineeType.CanonicalKey)
					if index < 0 || !coverage.cover(index) {
						return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "duplicate or unreachable match pattern"))}
					}
					if failed := finishArm(arm, coverage.cases[index].tag, nil, &member); failed != nil {
						return *failed
					}
				}
				break
			}
			adtVariant, ownerType, ok := resolveDottedVariantArm(pattern, scrutineeType, isADT, ctx)
			if !ok {
				return checkedExpression{token: pattern.Name, diagnostic: diagnosticAt(typeErrorAt(pattern.Name, fmt.Sprintf("unknown qualified variant %s.%s", pattern.Owner.Lexeme, pattern.Name.Lexeme)))}
			}
			if !isADT || !compilerTypes.Equal(scrutineeType, ownerType) {
				return checkedExpression{token: pattern.Name, diagnostic: diagnosticAt(typeErrorAt(pattern.Name, "match pattern does not belong to the scrutinee type"))}
			}
			index := coverage.find(adtVariant.Name)
			if index < 0 || !coverage.cover(index) {
				return checkedExpression{token: pattern.Name, diagnostic: diagnosticAt(typeErrorAt(pattern.Name, "duplicate or unreachable match pattern"))}
			}
			if failed := finishArm(arm, coverage.cases[index].tag, adtVariant, nil); failed != nil {
				return *failed
			}
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
			index := coverage.find(adtVariant.Name)
			if index < 0 || !coverage.cover(index) {
				return checkedExpression{token: pattern.Variant, diagnostic: diagnosticAt(typeErrorAt(pattern.Variant, "duplicate or unreachable match pattern"))}
			}
			if failed := finishArm(arm, coverage.cases[index].tag, adtVariant, nil); failed != nil {
				return *failed
			}
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
				index := coverage.find(member.CanonicalKey)
				if index < 0 || !coverage.cover(index) {
					return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "duplicate or unreachable match pattern"))}
				}
				if failed := finishArm(arm, coverage.cases[index].tag, nil, &member); failed != nil {
					return *failed
				}
			} else {
				if !compilerTypes.Equal(scrutineeType, member) {
					return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "match pattern does not belong to the scrutinee type"))}
				}
				index := coverage.find(scrutineeType.CanonicalKey)
				if index < 0 || !coverage.cover(index) {
					return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, "duplicate or unreachable match pattern"))}
				}
				if failed := finishArm(arm, coverage.cases[index].tag, nil, &member); failed != nil {
					return *failed
				}
			}
		}
	}
	if coverage.uncovered() {
		missing := coverage.firstMissing()
		name := missing.key
		if isADT {
			name = matchVariantOwnerName(scrutineeType, ctx) + "." + missing.variant
		} else if missing.member != (compilerTypes.Type{}) {
			name = matchMissingName(missing.member, ctx)
		}
		return checkedExpression{token: expression.Keyword, diagnostic: diagnosticAt(typeErrorAt(expression.Keyword, fmt.Sprintf("match is not exhaustive; missing %s", name)))}
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
