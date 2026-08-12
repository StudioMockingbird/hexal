package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0017 defined division and remainder: zero-divisor guards and the
// signed-minimum/-1 special cases run before any C division, and the trap
// never executes the invalid operation.

// discoverGeneratedDivisions collects the integer types whose division and
// remainder operations need guarded helpers.
func discoverGeneratedDivisions(program checker.Program) []compilerTypes.Type {
	seen := make(map[string]bool)
	var types []compilerTypes.Type
	var walkOperand func(checker.Operand)
	var walkExpression func(checker.Expression)
	var walkStatements func([]checker.Statement)
	walkExpression = func(node checker.Expression) {
		if node.Kind == checker.BinaryOperationExpression &&
			(node.Operator == checker.DivideOperator || node.Operator == checker.RemainderOperator) &&
			compilerTypes.IsInteger(node.OperandType) && !seen[node.OperandType.Name] {
			seen[node.OperandType.Name] = true
			types = append(types, node.OperandType)
		}
		if node.Operand != nil {
			walkExpression(*node.Operand)
		}
		if node.Left != nil {
			walkExpression(*node.Left)
		}
		if node.Right != nil {
			walkExpression(*node.Right)
		}
		for _, argument := range node.Arguments {
			walkOperand(argument)
		}
	}
	walkOperand = func(source checker.Operand) {
		if source.Node.Kind != checker.InvalidExpression {
			walkExpression(source.Node)
		}
	}
	walkStatements = func(statements []checker.Statement) {
		for _, statement := range statements {
			switch statement := statement.(type) {
			case checker.Declaration:
				walkOperand(statement.Source)
			case checker.Assignment:
				walkOperand(statement.Source)
				walkOperand(statement.Target)
			case checker.CallStatement:
				walkExpression(statement.Call.Node)
			case checker.ReturnStatement:
				if statement.Value != nil {
					walkOperand(*statement.Value)
				}
			case checker.IfStatement:
				walkOperand(statement.Condition)
				walkStatements(statement.Then)
				for _, branch := range statement.ElseIf {
					walkOperand(branch.Condition)
					walkStatements(branch.Body)
				}
				if statement.Else != nil {
					walkStatements(statement.Else)
				}
			case checker.ForStatement:
				walkOperand(statement.Source)
				walkStatements(statement.Body)
			case checker.WhileStatement:
				walkOperand(statement.Condition)
				walkStatements(statement.Body)
			case checker.FunctionDeclaration:
				walkStatements(statement.Body)
			case checker.MethodDeclaration:
				walkStatements(statement.Body)
			}
		}
	}
	walkStatements(program.Statements)
	for _, function := range program.SpecializedFunctions {
		walkStatements(function.Body)
	}
	for _, method := range program.SpecializedMethods {
		walkStatements(method.Body)
	}
	return types
}

// writeDivisionDefinitions emits the guarded division and remainder helpers
// for every collected integer type.
func writeDivisionDefinitions(result *strings.Builder, types []compilerTypes.Type) {
	if len(types) == 0 {
		return
	}
	result.WriteString("\n#ifndef SW_NUMERIC_TRAP_DEFINED\n#define SW_NUMERIC_TRAP_DEFINED\n")
	result.WriteString("static void sw_numeric_trap(void) {\n")
	result.WriteString("    fputs(\"[Runtime Error] numeric operation failed\\n\", stderr);\n    abort();\n}\n")
	result.WriteString("#endif\n")
	for _, typ := range types {
		writeDivisionHelper(result, typ, checker.DivideOperator, "div")
		writeDivisionHelper(result, typ, checker.RemainderOperator, "rem")
	}
}

func writeDivisionHelper(result *strings.Builder, typ compilerTypes.Type, operator checker.Operator, suffix string) {
	cName := typ.CName
	fmt.Fprintf(result, "\nstatic inline %s sw_%s_%s(%s left, %s right) {\n", cName, suffix, cName, cName, cName)
	result.WriteString("    if (right == 0) {\n        sw_numeric_trap();\n    }\n")
	if compilerTypes.IsSignedInteger(typ) {
		fmt.Fprintf(result, "    if (left == %s && right == -1) {\n", signedMinimumMacro(typ))
		if operator == checker.RemainderOperator {
			result.WriteString("        return 0;\n    }\n")
		} else {
			fmt.Fprintf(result, "        return %s;\n    }\n", signedMinimumMacro(typ))
		}
	}
	operation := "/"
	if operator == checker.RemainderOperator {
		operation = "%"
	}
	result.WriteString("    return left " + operation + " right;\n}\n")
}

// renderDivisionOperation routes integer division and remainder through the
// guarded helpers; floating division keeps its defined IEC behavior inline.
func renderDivisionOperation(node checker.Expression, left, right string) (string, error) {
	helper := "sw_div_" + node.OperandType.CName
	if node.Operator == checker.RemainderOperator {
		helper = "sw_rem_" + node.OperandType.CName
	}
	return helper + "(" + left + ", " + right + ")", nil
}
