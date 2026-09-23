package generator

import (
	"go/constant"
	"strings"
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func restIntOperand(value int64, literal string) checker.Operand {
	return checker.Operand{Kind: checker.ConstantOperand, Type: compilerTypes.Int32, Constant: constant.MakeInt64(value), Literal: literal}
}

// conflictRestProgram builds a checked program whose call to a rest function
// carries restElement as its recorded element type. A consistent value passes
// preflight; a forged one must fail closed.
func restCallProgram(environment *compilerTypes.Environment, restElement compilerTypes.Type) checker.Program {
	result := compilerTypes.Int32
	slice := environment.SliceType(compilerTypes.Int32, false)
	fun := environment.FunTypeRest([]compilerTypes.Type{compilerTypes.Int32}, &result, true)
	declaration := checker.FunctionDeclaration{
		Name:       "sum",
		Parameters: []checker.FunctionParameter{{Name: "rest", Type: slice, Rest: true, RestElement: compilerTypes.Int32}},
		Result:     &result,
		Type:       fun,
		Body: []checker.Statement{checker.ReturnStatement{Value: &checker.Operand{
			Kind: checker.ConstantOperand, Type: compilerTypes.Int32, Constant: constant.MakeInt64(0), Literal: "0",
		}}},
	}
	forged := checker.Expression{
		Kind:        checker.CallExpression,
		Operand:     &checker.Expression{Kind: checker.FunctionReferenceExpression, Name: "sum", ResultType: fun},
		Arguments:   []checker.Operand{restIntOperand(1, "1")},
		OperandType: fun,
		ResultType:  result,
		Rest:        true,
		RestStart:   0,
		RestElement: restElement,
		RestSlice:   fun.Signature.RestSlice,
	}
	return checker.Program{Statements: []checker.Statement{
		declaration,
		checker.CallStatement{Call: checker.Operand{Kind: checker.ExpressionOperand, Type: compilerTypes.Int32, Node: forged}},
	}}
}

// Generator preflight rejects a forged rest boundary: the checked call's
// element type must agree with its canonical signature.
func TestGeneratorPreflightRejectsForgedRestMetadata(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	valid := restCallProgram(environment, compilerTypes.Int32)
	if _, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": valid}, Config{SourceTable: testSpanTable}); err != nil {
		t.Fatalf("a consistent rest call was rejected: %v", err)
	}
	forged := restCallProgram(environment, compilerTypes.Int64)
	_, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": forged}, Config{SourceTable: testSpanTable})
	if err == nil || !strings.Contains(err.Error(), "rest call metadata does not match its checked signature") {
		t.Fatalf("forged rest element accepted: %v", err)
	}
}
