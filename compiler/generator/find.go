package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// hoistDictFindInStatement evaluates each dictionary find once before its
// enclosing statement. The component helper returns a pointer so the module
// can construct the checked union without making a second probe.
func hoistDictFindInStatement(statement checker.Statement, body *strings.Builder, state *expressionValidation, indent string) error {
	return walkStatementExpressions(statement, func(node *checker.Expression) error {
		if node.Kind != checker.CollectionMethodCallExpression || node.Name != "find" || node.Operand == nil || node.OperandType.Dict == nil || len(node.Arguments) != 1 {
			return nil
		}
		if state.hoistedDictFinds == nil {
			state.hoistedDictFinds = make(map[*checker.Expression]string)
		}
		if _, ok := state.hoistedDictFinds[node.Operand]; ok {
			return nil
		}
		receiver, err := renderReceiver(node.Operand, node.OperandType, state)
		if err != nil {
			return err
		}
		key, err := renderOperandWithState(node.Arguments[0], state)
		if err != nil {
			return err
		}
		state.findCounter++
		temp := fmt.Sprintf("hex_dict_find_%d", state.findCounter)
		valueType := node.Element
		if valueType == (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("dictionary find has no checked value type")
		}
		fmt.Fprintf(body, "%sconst %s *%s = hex_dict_find_%s(%s, %s);\n", indent, typeSpelling(valueType), temp, dictSuffix(node.OperandType), receiver, key)
		state.hoistedDictFinds[node.Operand] = temp
		return nil
	})
}

func renderDictFindExpression(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || node.ResultType == (compilerTypes.Type{}) || !compilerTypes.IsUnion(node.ResultType) || !compilerTypes.ContainsUnionMember(node.ResultType, compilerTypes.Nil) {
		return "", unknownExpressionDiagnostic("dictionary find has invalid checked metadata")
	}
	temp, ok := state.hoistedDictFinds[node.Operand]
	if !ok {
		return "", unknownExpressionDiagnostic("dictionary find reached generation without hoisting")
	}
	if compilerTypes.IsNullable(node.ResultType) {
		return fmt.Sprintf("(%s == nullptr ? nullptr : *%s)", temp, temp), nil
	}
	valueIndex := unionMemberIndex(node.ResultType, node.Element)
	nilIndex := unionMemberIndex(node.ResultType, compilerTypes.Nil)
	if nilIndex < 0 {
		return "", unknownExpressionDiagnostic("dictionary find result has no Nil member")
	}
	var present string
	if node.Element.Union != nil && !compilerTypes.Equal(node.Element, node.ResultType) {
		present = unionWidenCall(node.Element, node.ResultType, "*"+temp)
	} else {
		if valueIndex < 0 {
			return "", unknownExpressionDiagnostic("dictionary find result has no value member")
		}
		present = fmt.Sprintf("(%s){ .tag = %s, .payload.%s = *%s }", node.ResultType.CName, state.tags.unionMemberTag(compilerTypes.UnionMembers(node.ResultType)[valueIndex]), state.tags.unionPayloadField(compilerTypes.UnionMembers(node.ResultType)[valueIndex]), temp)
	}
	missing := fmt.Sprintf("(%s){ .tag = %s }", node.ResultType.CName, state.tags.unionMemberTag(compilerTypes.UnionMembers(node.ResultType)[nilIndex]))
	return fmt.Sprintf("(%s == nullptr ? %s : %s)", temp, missing, present), nil
}
