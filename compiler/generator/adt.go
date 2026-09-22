package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// generatedAdtState records ADTs in deterministic declaration order.
type generatedAdtState struct {
	order []compilerTypes.Type
	seen  map[*compilerTypes.AdtType]bool
}

// ensureRegistered records typ in the ADT order if it is not already
// present, even though no source expression in this module may construct it.
// ErrorKind uses this: selecting Error registers every ErrorKind variant into
// the program-wide tag registry, not only the variants a program constructs.
func (state *generatedAdtState) ensureRegistered(typ compilerTypes.Type) {
	if typ.Adt == nil || state.seen[typ.Adt] {
		return
	}
	state.seen[typ.Adt] = true
	state.order = append(state.order, typ)
}

func discoverGeneratedADTs(program checker.Program) *generatedAdtState {
	state := &generatedAdtState{seen: make(map[*compilerTypes.AdtType]bool)}
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if typ.Adt != nil {
				if !state.seen[typ.Adt] {
					state.seen[typ.Adt] = true
					state.order = append(state.order, typ)
				}
			}
			return nil
		},
	}
	walkProgram(program, visitor)

	return state
}

// adtBodyModel carries one ADT's struct body. Variants holds only the
// payload-carrying variants, and HasPayload gates the payload union.
type adtBodyModel struct {
	Name       string
	HasPayload bool
	Variants   []adtVariantModel
}

// adtVariantModel is one payload-carrying variant's sanitized name and
// decided member declarators.
type adtVariantModel struct {
	Name    string
	Members []string
}

// writeAdtForwardDeclarations emits `typedef struct CName CName;` for every
// discovered ADT, ahead of every full body: a pointer-typed member naming an
// ADT needs only this forward name, regardless of full-body emission order.
func writeAdtForwardDeclarations(result *strings.Builder, state *generatedAdtState) error {
	if state == nil {
		return nil
	}
	for _, adtType := range state.order {
		if compilerTypes.IsBuiltinAdt(adtType) {
			continue
		}
		if err := renderInto(result, "module.h", "nominal_forward", nominalForwardModel{Name: adtType.Adt.CName}); err != nil {
			return err
		}
	}
	return nil
}

// writeOneAdtBody emits one ADT's full struct body (the tag discriminant and,
// when any variant carries fields, the payload union of per-variant anonymous
// structs). Its own forward typedef must already be in scope; a payload field
// naming another nominal type by value additionally needs that type's own
// full body already written, which the dependency-ordered driver in
// emission.go guarantees before calling this.
func writeOneAdtBody(result *strings.Builder, adtType compilerTypes.Type) error {
	adt := adtType.Adt
	model := adtBodyModel{Name: adt.CName}
	for _, variant := range adt.Variants {
		if len(variant.Payload) > 0 {
			model.HasPayload = true
		}
	}
	if model.HasPayload {
		for _, variant := range adt.Variants {
			if len(variant.Payload) == 0 {
				continue
			}
			variantModel := adtVariantModel{Name: compilerTypes.SanitizeIdentifier(variant.Name)}
			for _, member := range variant.Payload {
				variantModel.Members = append(variantModel.Members, typeSpelling(member.Type)+" "+privateCName(memberName, member.Name, ""))
			}
			model.Variants = append(model.Variants, variantModel)
		}
	}
	return renderInto(result, "module.h", "adt_body", model)
}

// adtConstructModel carries one compound-literal construction's decided
// parts. A non-empty OtherHeader or PayloadOpen selects that section; the
// field assignments arrive as complete decided fragments.
type adtConstructModel struct {
	CName       string
	Tag         string
	OtherHeader string
	PayloadOpen string
	Fields      []string
}

// renderAdtConstruct lowers an ADT construction to a compound literal whose
// tag is the shared variant discriminant.
func renderAdtConstruct(node checker.Expression, state *expressionValidation) (string, error) {
	adt := node.ResultType.Adt
	if adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) {
		return "", unknownExpressionDiagnostic("ADT construction has invalid checked metadata")
	}
	variant := &adt.Variants[node.VariantIndex]
	model := adtConstructModel{
		CName: adt.CName,
		Tag:   state.tags.adtVariantTag(adt, node.VariantIndex),
	}
	if compilerTypes.IsErrorKind(node.ResultType) {
		// ErrorKind's Other is the only payload-carrying variant among 26, so
		// its header lives in one flat other_header field rather than a
		// per-variant payload union (see hexal/error.h).
		if len(node.Arguments) != len(variant.Payload) {
			return "", unknownExpressionDiagnostic("ADT construction payload count does not match its variant")
		}
		if len(variant.Payload) == 1 {
			value, err := renderHoistedOperand(&node.Arguments[0].Node, node.Arguments[0], state)
			if err != nil {
				return "", err
			}
			model.OtherHeader = value
		}
	} else if len(variant.Payload) > 0 {
		if len(node.Arguments) != len(variant.Payload) {
			return "", unknownExpressionDiagnostic("ADT construction payload count does not match its variant")
		}
		model.PayloadOpen = ", .payload." + compilerTypes.SanitizeIdentifier(variant.Name) + " = {"
		for index, member := range variant.Payload {
			// A field written out of declaration order was hoisted into its
			// own written-order temporary by hoistAdtSequence; the compound
			// literal itself always assembles in declaration order.
			value, err := renderHoistedOperand(&node.Arguments[index].Node, node.Arguments[index], state)
			if err != nil {
				return "", err
			}
			model.Fields = append(model.Fields, fmt.Sprintf(" .%s = %s,", privateCName(memberName, member.Name, ""), value))
		}
	}
	var builder strings.Builder
	if err := renderInto(&builder, "module.c", "adt_construct", model); err != nil {
		return "", err
	}
	return builder.String(), nil
}

// renderAdtPayload renders a payload read after its tag proof:
// <scrutinee>.payload.<variant>.<member>.
func renderAdtPayload(node checker.Expression, state *expressionValidation) (string, error) {
	adt := node.OperandType.Adt
	if node.Operand == nil || adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) {
		return "", unknownExpressionDiagnostic("ADT payload read has invalid checked metadata")
	}
	variant := &adt.Variants[node.VariantIndex]
	if node.MemberIndex < 0 || node.MemberIndex >= len(variant.Payload) {
		return "", unknownExpressionDiagnostic("ADT payload read has an invalid member index")
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	return receiver + ".payload." + compilerTypes.SanitizeIdentifier(variant.Name) + "." + privateCName(memberName, variant.Payload[node.MemberIndex].Name, ""), nil
}

// matchAssignModel carries one match assignment or declaration line: the
// target and an optional value, both decided in Go.
type matchAssignModel struct {
	Indent string
	Target string
	Value  string
}

// matchOpenModel carries one conditionally-opened match block: Prefix is the
// decided `if` or `else if` spelling, Condition the decided C test.
type matchOpenModel struct {
	Indent    string
	Prefix    string
	Condition string
}

// indentModel carries a fixed statement line's decided indent; block closes,
// else-openers, and return lines share it across emitters.
type indentModel struct {
	Indent string
}

// renderMatchStatement lowers a match expression to statement-level if/else
// control flow and returns the name of the result variable.
func renderMatchStatement(body *strings.Builder, node checker.Expression, state *expressionValidation, indent string) (string, error) {
	if node.Operand == nil || node.ResultType == (compilerTypes.Type{}) || len(node.Arguments) != len(node.MemberMap) {
		return "", unknownExpressionDiagnostic("match expression has invalid checked metadata")
	}
	state.matchCounter++
	temp := fmt.Sprintf("hex_match_scrutinee_%d", state.matchCounter)
	result := fmt.Sprintf("hex_match_result_%d", state.matchCounter)
	scrutinee, err := renderExpressionExpectedWithState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	if err := renderInto(body, "module.c", "match_assign", matchAssignModel{
		Indent: indent,
		Target: declaration(node.OperandType, temp, false),
		Value:  scrutinee,
	}); err != nil {
		return "", err
	}
	if err := renderInto(body, "module.c", "match_assign", matchAssignModel{
		Indent: indent,
		Target: declaration(node.ResultType, result, true),
	}); err != nil {
		return "", err
	}
	// An arm body that names the scrutinee (shape.radius, say) renders its
	// own separate reference to the original binding, not to temp above. GCC
	// cannot always prove the two copies' payloads agree, and warns
	// -Wmaybe-uninitialized on the arm's payload read even though the tag
	// check that guards it passed on temp. Rebinding the scrutinee's name to
	// temp for the arms keeps every read within one match on the same C
	// value the tag was just checked against, which is also simply the more
	// direct rendering: no need for a reader to know the two names agree.
	if node.Operand.Binding != 0 {
		previous, hadPrevious := state.bindingNames[node.Operand.Binding]
		state.bindingNames[node.Operand.Binding] = temp
		defer func() {
			if hadPrevious {
				state.bindingNames[node.Operand.Binding] = previous
			} else {
				delete(state.bindingNames, node.Operand.Binding)
			}
		}()
	}
	scrutineeMembers := compilerTypes.UnionMembers(node.OperandType)
	// A trailing else binds to the nearest preceding if in C, so a match with
	// an else must chain its explicit arms with else-if; otherwise an earlier
	// arm's result is overwritten by the else. A match with no else has
	// mutually exclusive arms and keeps its separate plain ifs unchanged.
	hasElse := false
	for _, tag := range node.MemberMap {
		if tag == -1 {
			hasElse = true
			break
		}
	}
	emittedIf := false
	for armIndex, arm := range node.Arguments {
		armValue, err := renderOperandWithState(arm, state)
		if err != nil {
			return "", err
		}
		tag := node.MemberMap[armIndex]
		if tag == checker.MatchScalarTag {
			if armIndex >= len(node.MatchConstants) || node.MatchConstants[armIndex].Kind != checker.ConstantOperand {
				return "", unknownExpressionDiagnostic("scalar match arm without a checked constant")
			}
			rendered, renderErr := renderOperandWithState(node.MatchConstants[armIndex], state)
			if renderErr != nil {
				return "", renderErr
			}
			// A scalar match always ends in the required else, so its arms
			// chain with else-if: first matching arm wins. A plain second if
			// would let the trailing else overwrite an earlier match.
			prefix := "if "
			if emittedIf {
				prefix = "else if "
			}
			if err := renderInto(body, "module.c", "match_open", matchOpenModel{
				Indent:    indent,
				Prefix:    prefix,
				Condition: temp + " == " + rendered,
			}); err != nil {
				return "", err
			}
			emittedIf = true
			if err := renderInto(body, "module.c", "match_assign", matchAssignModel{
				Indent: indent + "    ",
				Target: result,
				Value:  armValue,
			}); err != nil {
				return "", err
			}
			if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
				return "", err
			}
			continue
		}
		isElse := tag == -1 || tag == -2
		if isElse && !emittedIf {
			if err := renderInto(body, "module.c", "match_assign", matchAssignModel{
				Indent: indent,
				Target: result,
				Value:  armValue,
			}); err != nil {
				return "", err
			}
			continue
		}
		if isElse {
			if err := renderInto(body, "module.c", "match_else", indentModel{Indent: indent}); err != nil {
				return "", err
			}
		} else {
			keyword := "if"
			if emittedIf && hasElse {
				keyword = "else if"
			}
			emittedIf = true
			var condition string
			switch {
			case node.OperandType.Adt != nil:
				condition = fmt.Sprintf("%s.tag == %s", temp, state.tags.adtVariantTag(node.OperandType.Adt, tag))
			case node.OperandType.Union != nil:
				member, _ := scrutineeMembers.At(tag)
				condition = fmt.Sprintf("%s.tag == %s", temp, state.tags.unionMemberTag(member))
			case tag == 1:
				condition = temp
			default:
				condition = "!" + temp
			}
			if err := renderInto(body, "module.c", "match_open", matchOpenModel{
				Indent:    indent,
				Prefix:    keyword + " ",
				Condition: condition,
			}); err != nil {
				return "", err
			}
		}
		if err := renderInto(body, "module.c", "match_assign", matchAssignModel{
			Indent: indent + "    ",
			Target: result,
			Value:  armValue,
		}); err != nil {
			return "", err
		}
		if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
			return "", err
		}
	}
	return result, nil
}
