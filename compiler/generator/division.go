package generator

import (
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// Integer division and remainder: zero-divisor and signed-minimum/-1 guards
// run before any C division, and the trap never executes the invalid
// operation.

// discoverGeneratedDivisions collects the integer types whose division and
// remainder operations need guarded helpers.
func discoverGeneratedDivisions(program checker.Program) []compilerTypes.Type {
	seen := make(map[string]bool)
	var types []compilerTypes.Type
	visitor := &programVisitor{
		Expression: func(node checker.Expression) error {
			if node.Kind == checker.BinaryOperationExpression &&
				(node.Operator == checker.DivideOperator || node.Operator == checker.RemainderOperator) &&
				compilerTypes.IsInteger(node.OperandType) && !seen[node.OperandType.Name] {
				seen[node.OperandType.Name] = true
				types = append(types, node.OperandType)
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	return types
}

// divisionHelperModel carries one guarded division helper's decided result
// and parameter type, name suffix, signed-minimum macro, and operation;
// each emitted part reads the fields it spells.
type divisionHelperModel struct {
	CName     string
	Suffix    string
	Minimum   string
	Operation string
}

func writeDivisionHelper(result *strings.Builder, typ compilerTypes.Type, operator checker.Operator, suffix string) error {
	cName := typ.CName
	if err := renderInto(result, "module.h", "division_helper_open", divisionHelperModel{CName: cName, Suffix: suffix}); err != nil {
		return err
	}
	if err := renderInto(result, "module.h", "division_zero_guard", struct{}{}); err != nil {
		return err
	}
	if compilerTypes.IsSignedInteger(typ) {
		minimum, minimumErr := signedMinimumMacro(typ)
		if minimumErr != nil {
			return minimumErr
		}
		if err := renderInto(result, "module.h", "division_min_guard", divisionHelperModel{Minimum: minimum}); err != nil {
			return err
		}
		if operator == checker.RemainderOperator {
			if err := renderInto(result, "module.h", "division_rem_close", struct{}{}); err != nil {
				return err
			}
		} else {
			if err := renderInto(result, "module.h", "division_min_return", divisionHelperModel{Minimum: minimum}); err != nil {
				return err
			}
		}
	}
	operation := "/"
	if operator == checker.RemainderOperator {
		operation = "%"
	}
	return renderInto(result, "module.h", "division_helper_close", divisionHelperModel{Operation: operation})
}

// renderDivisionOperation routes integer division and remainder through the
// guarded helpers; floating division keeps its defined IEC behavior inline.
func renderDivisionOperation(node checker.Expression, left, right string) (string, error) {
	helper := "hex_div_" + node.OperandType.CName
	if node.Operator == checker.RemainderOperator {
		helper = "hex_rem_" + node.OperandType.CName
	}
	return helper + "(" + left + ", " + right + ")", nil
}
