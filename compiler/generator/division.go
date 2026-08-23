package generator

import (
	"fmt"
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

func writeDivisionHelper(result *strings.Builder, typ compilerTypes.Type, operator checker.Operator, suffix string) error {
	cName := typ.CName
	fmt.Fprintf(result, "\nstatic inline %s hex_%s_%s(%s left, %s right) {\n", cName, suffix, cName, cName, cName)
	result.WriteString("    if (right == 0) {\n        hex_runtime_trap(\"[Runtime Error] numeric operation failed\\n\");\n    }\n")
	if compilerTypes.IsSignedInteger(typ) {
		minimum, minimumErr := signedMinimumMacro(typ)
		if minimumErr != nil {
			return minimumErr
		}
		fmt.Fprintf(result, "    if (left == %s && right == -1) {\n", minimum)
		if operator == checker.RemainderOperator {
			result.WriteString("        return 0;\n    }\n")
		} else {
			fmt.Fprintf(result, "        return %s;\n    }\n", minimum)
		}
	}
	operation := "/"
	if operator == checker.RemainderOperator {
		operation = "%"
	}
	result.WriteString("    return left " + operation + " right;\n}\n")
	return nil
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
