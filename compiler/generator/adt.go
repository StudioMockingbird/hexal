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

func discoverGeneratedADTs(program checker.Program) (*generatedAdtState, error) {
	state := &generatedAdtState{seen: make(map[*compilerTypes.AdtType]bool)}
	seenObjects := make(map[*compilerTypes.ObjectType]bool)
	var walkType func(compilerTypes.Type) error
	var walkOperand func(checker.Operand) error
	var walkExpression func(checker.Expression) error
	var walkStatements func([]checker.Statement) error
	walkType = func(typ compilerTypes.Type) error {
		if typ.Adt != nil {
			if !state.seen[typ.Adt] {
				state.seen[typ.Adt] = true
				state.order = append(state.order, typ)
				for _, variant := range typ.Adt.Variants {
					for _, member := range variant.Payload {
						if err := walkType(member.Type); err != nil {
							return err
						}
					}
				}
			}
			return nil
		}
		if typ.Union != nil {
			for _, member := range typ.Union.Members {
				if err := walkType(member); err != nil {
					return err
				}
			}
		}
		if typ.Element != nil {
			return walkType(*typ.Element)
		}
		if typ.Signature != nil {
			for _, parameter := range typ.Signature.Parameters {
				if err := walkType(parameter); err != nil {
					return err
				}
			}
			if typ.Signature.Result != nil {
				return walkType(*typ.Signature.Result)
			}
		}
		if typ.Object != nil {
			if seenObjects[typ.Object] {
				return nil
			}
			seenObjects[typ.Object] = true
			for _, member := range typ.Object.Members {
				if err := walkType(member.Type); err != nil {
					return err
				}
			}
		}
		return nil
	}
	walkExpression = func(node checker.Expression) error {
		if err := walkType(node.OperandType); err != nil {
			return err
		}
		if err := walkType(node.ResultType); err != nil {
			return err
		}
		if node.Operand != nil {
			if err := walkExpression(*node.Operand); err != nil {
				return err
			}
		}
		if node.Left != nil {
			if err := walkExpression(*node.Left); err != nil {
				return err
			}
		}
		if node.Right != nil {
			if err := walkExpression(*node.Right); err != nil {
				return err
			}
		}
		for _, argument := range node.Arguments {
			if err := walkOperand(argument); err != nil {
				return err
			}
		}
		return nil
	}
	walkOperand = func(operand checker.Operand) error {
		if err := walkType(operand.Type); err != nil {
			return err
		}
		return walkExpression(operand.Node)
	}
	walkStatements = func(statements []checker.Statement) error {
		for _, statement := range statements {
			switch statement := statement.(type) {
			case checker.Declaration:
				if err := walkType(statement.Type); err != nil {
					return err
				}
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
			case checker.Assignment:
				if err := walkOperand(statement.Target); err != nil {
					return err
				}
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
			case checker.CallStatement:
				if err := walkOperand(statement.Call); err != nil {
					return err
				}
			case checker.ReturnStatement:
				if statement.Value != nil {
					if err := walkOperand(*statement.Value); err != nil {
						return err
					}
				}
			case checker.DeferStatement:
				if err := walkOperand(statement.Expression); err != nil {
					return err
				}
			case checker.IfStatement:
				if err := walkOperand(statement.Condition); err != nil {
					return err
				}
				if err := walkStatements(statement.Then); err != nil {
					return err
				}
				for _, branch := range statement.ElseIf {
					if err := walkOperand(branch.Condition); err != nil {
						return err
					}
					if err := walkStatements(branch.Body); err != nil {
						return err
					}
				}
				if err := walkStatements(statement.Else); err != nil {
					return err
				}
			case checker.ForStatement:
				if err := walkOperand(statement.Source); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.WhileStatement:
				if err := walkOperand(statement.Condition); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.FunctionDeclaration:
				if err := walkType(statement.Type); err != nil {
					return err
				}
				for _, parameter := range statement.Parameters {
					if err := walkType(parameter.Type); err != nil {
						return err
					}
				}
				if statement.Result != nil {
					if err := walkType(*statement.Result); err != nil {
						return err
					}
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			case checker.MethodDeclaration:
				if err := walkType(statement.SelfType); err != nil {
					return err
				}
				if err := walkStatements(statement.Body); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, declaration := range program.TypeDeclarations {
		if err := walkType(declaration.Type); err != nil {
			return nil, err
		}
	}
	if err := walkStatements(program.Statements); err != nil {
		return nil, err
	}
	for _, function := range program.SpecializedFunctions {
		if err := walkStatements(function.Body); err != nil {
			return nil, err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := walkStatements(method.Body); err != nil {
			return nil, err
		}
	}
	return state, nil
}

func adtTagName(adt *compilerTypes.AdtType, index int) string {
	return "sw_" + compilerTypes.SanitizeIdentifier(adt.Name) + "_" + compilerTypes.SanitizeIdentifier(adt.Variants[index].Name)
}

func writeAdtDefinitions(result *strings.Builder, state *generatedAdtState) {
	if state == nil || len(state.order) == 0 {
		return
	}
	for _, adtType := range state.order {
		adt := adtType.Adt
		name := compilerTypes.SanitizeIdentifier(adt.Name)
		result.WriteString("\n")
		fmt.Fprintf(result, "typedef enum {\n")
		for index := range adt.Variants {
			fmt.Fprintf(result, "    %s,\n", adtTagName(adt, index))
		}
		fmt.Fprintf(result, "} sw_%s_tag;\n", name)
		fmt.Fprintf(result, "typedef struct sw_%s {\n", name)
		fmt.Fprintf(result, "    sw_%s_tag tag;\n", name)
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
					fmt.Fprintf(result, "            %s %s;\n", typeSpelling(member.Type), PrivateCName(MemberName, member.Name))
				}
				fmt.Fprintf(result, "        } %s;\n", variantName)
			}
			fmt.Fprintf(result, "    } payload;\n")
		}
		fmt.Fprintf(result, "} sw_%s;\n", name)
	}
}

func renderAdtConstruct(node checker.Expression, state *expressionValidation) (string, error) {
	adt := node.ResultType.Adt
	if adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) {
		return "", unknownExpressionDiagnostic("ADT construction has invalid checked metadata")
	}
	variant := &adt.Variants[node.VariantIndex]
	base := compilerTypes.SanitizeIdentifier(adt.Name)
	var builder strings.Builder
	fmt.Fprintf(&builder, "(sw_%s){ .tag = %s", base, adtTagName(adt, node.VariantIndex))
	if len(variant.Payload) > 0 {
		if len(node.Arguments) != len(variant.Payload) {
			return "", unknownExpressionDiagnostic("ADT construction payload count does not match its variant")
		}
		fmt.Fprintf(&builder, ", .payload.%s = {", compilerTypes.SanitizeIdentifier(variant.Name))
		for index, member := range variant.Payload {
			value, err := renderOperandWithState(node.Arguments[index], state)
			if err != nil {
				return "", err
			}
			fmt.Fprintf(&builder, " .%s = %s,", PrivateCName(MemberName, member.Name), value)
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
	receiver, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
	if err != nil {
		return "", err
	}
	if !atomic {
		receiver = "(" + receiver + ")"
	}
	return receiver + ".payload." + compilerTypes.SanitizeIdentifier(variant.Name) + "." + PrivateCName(MemberName, variant.Payload[node.MemberIndex].Name), nil
}

// renderMatchStatement lowers a match expression to statement-level if/else
// control flow and returns the name of the result variable.
func renderMatchStatement(body *strings.Builder, node checker.Expression, state *expressionValidation, indent string) (string, error) {
	if node.Operand == nil || node.ResultType == (compilerTypes.Type{}) || len(node.Arguments) != len(node.MemberMap) {
		return "", unknownExpressionDiagnostic("match expression has invalid checked metadata")
	}
	state.matchCounter++
	temp := fmt.Sprintf("sw_match_scrutinee_%d", state.matchCounter)
	result := fmt.Sprintf("sw_match_result_%d", state.matchCounter)
	scrutinee, err := renderExpressionExpectedWithState(*node.Operand, node.OperandType, true, state)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(node.OperandType, temp, false), scrutinee)
	fmt.Fprintf(body, "%s%s;\n", indent, declaration(node.ResultType, result, true))
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
				fmt.Fprintf(body, "%sif (%s.tag == %s) {\n", indent, temp, adtTagName(node.OperandType.Adt, tag))
			case node.OperandType.Union != nil:
				fmt.Fprintf(body, "%sif (%s.tag == %s) {\n", indent, temp, unionTagName(node.OperandType, tag))
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
