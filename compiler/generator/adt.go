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

func writeAdtDefinitions(result *strings.Builder, state *generatedAdtState) {
	if state == nil || len(state.order) == 0 {
		return
	}
	for _, adtType := range state.order {
		if compilerTypes.IsSeek(adtType) {
			// Seek is a fixed, module-ownerless built-in ADT; its struct is
			// emitted once, by seekComponents/moduleSeekComponent, into a
			// shared header instead of repeated inline per module. It still
			// participates in this same discovery walk so the program-wide
			// tag registry (built from adtState.order) assigns its variants
			// a hex_tag value; only the per-module struct text is skipped
			// here.
			continue
		}
		adt := adtType.Adt
		name := adt.CName
		result.WriteString("\n")
		fmt.Fprintf(result, "typedef struct %s {\n", name)
		fmt.Fprintf(result, "    hex_tag tag;\n")
		hasPayload := false
		for _, variant := range adt.Variants {
			if len(variant.Payload) > 0 {
				hasPayload = true
			}
		}
		if hasPayload {
			fmt.Fprintf(result, "    union {\n")
			for _, variant := range adt.Variants {
				if len(variant.Payload) == 0 {
					continue
				}
				variantName := compilerTypes.SanitizeIdentifier(variant.Name)
				fmt.Fprintf(result, "        struct {\n")
				for _, member := range variant.Payload {
					fmt.Fprintf(result, "            %s %s;\n", typeSpelling(member.Type), privateCName(memberName, member.Name, ""))
				}
				fmt.Fprintf(result, "        } %s;\n", variantName)
			}
			fmt.Fprintf(result, "    } payload;\n")
		}
		fmt.Fprintf(result, "} %s;\n", name)
	}
}

// renderAdtConstruct lowers an ADT construction to a compound literal whose
// tag is the shared variant discriminant.
func renderAdtConstruct(node checker.Expression, state *expressionValidation) (string, error) {
	adt := node.ResultType.Adt
	if adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) {
		return "", unknownExpressionDiagnostic("ADT construction has invalid checked metadata")
	}
	variant := &adt.Variants[node.VariantIndex]
	var builder strings.Builder
	fmt.Fprintf(&builder, "(%s){ .tag = %s", adt.CName, state.tags.adtVariantTag(adt, node.VariantIndex))
	if len(variant.Payload) > 0 {
		if len(node.Arguments) != len(variant.Payload) {
			return "", unknownExpressionDiagnostic("ADT construction payload count does not match its variant")
		}
		fmt.Fprintf(&builder, ", .payload.%s = {", compilerTypes.SanitizeIdentifier(variant.Name))
		for index, member := range variant.Payload {
			// A field written out of declaration order was hoisted into its
			// own written-order temporary by hoistAdtSequence; the compound
			// literal itself always assembles in declaration order.
			value, err := renderHoistedOperand(&node.Arguments[index].Node, node.Arguments[index], state)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&builder, " .%s = %s,", privateCName(memberName, member.Name, ""), value)
		}
		builder.WriteString(" }")
	}
	builder.WriteString(" }")
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
	fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(node.OperandType, temp, false), scrutinee)
	fmt.Fprintf(body, "%s%s;\n", indent, declaration(node.ResultType, result, true))
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
	emittedIf := false
	for armIndex, arm := range node.Arguments {
		armValue, err := renderOperandWithState(arm, state)
		if err != nil {
			return "", err
		}
		tag := node.MemberMap[armIndex]
		isElse := tag == -1 || tag == -2
		if isElse && !emittedIf {
			fmt.Fprintf(body, "%s%s = %s;\n", indent, result, armValue)
			continue
		}
		if isElse {
			fmt.Fprintf(body, "%selse {\n", indent)
		} else {
			emittedIf = true
			switch {
			case node.OperandType.Adt != nil:
				fmt.Fprintf(body, "%sif (%s.tag == %s) {\n", indent, temp, state.tags.adtVariantTag(node.OperandType.Adt, tag))
			case node.OperandType.Union != nil:
				member, _ := scrutineeMembers.At(tag)
				fmt.Fprintf(body, "%sif (%s.tag == %s) {\n", indent, temp, state.tags.unionMemberTag(member))
			case tag == 1:
				fmt.Fprintf(body, "%sif (%s) {\n", indent, temp)
			default:
				fmt.Fprintf(body, "%sif (!%s) {\n", indent, temp)
			}
		}
		fmt.Fprintf(body, "%s    %s = %s;\n", indent, result, armValue)
		fmt.Fprintf(body, "%s}\n", indent)
	}
	return result, nil
}
