package generator

import (
	"testing"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// printStatement is the checked statement form of a print call: a node with
// no value type whose arguments validate at check time.
func printStatement() checker.CallStatement {
	return checker.CallStatement{Call: checker.Operand{
		Kind: checker.ExpressionOperand,
		Node: checker.Expression{
			Kind:       checker.PrintExpression,
			Arguments:  []checker.Operand{},
			ResultType: compilerTypes.Type{},
		},
	}}
}

// deferredNoResultCall is a defer of a no-value call, the shape Heap.free and
// friends produce: IsCall with a checked node and an empty value type.
func deferredNoResultCall() checker.DeferStatement {
	return checker.DeferStatement{Action: checker.DeferredAction{
		IsCall: true,
		Call: &checker.Operand{
			Kind: checker.ExpressionOperand,
			Type: compilerTypes.Type{},
			Node: constantExpression(intSource(compilerTypes.Int32, 13, "13")),
		},
	}}
}

// invalidFollowUp is a statement preflight must reject after it finishes the
// statement before it: an assignment whose name is not a valid source name.
// validateStatements returns before touching the operand fields, so a bare
// literal-shaped record proves only that preflight reached the statement.
func invalidFollowUp() checker.Assignment {
	return checker.Assignment{Name: "1bad", Type: compilerTypes.Type{}}
}

// Preflight must validate the statements that follow a no-result print or a
// no-result deferred call; a return-on-first-shaped-statement would skip them
// and leave a silently invalid body behind.
func TestValidateStatementsContinuesPastNoValueStatements(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		first checker.Statement
	}{
		{"print", printStatement()},
		{"deferred no-result call", deferredNoResultCall()},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateStatements(
				[]checker.Statement{testCase.first, invalidFollowUp()},
				&expressionValidation{},
				&generatedTypeValidation{},
			)
			if err == nil {
				t.Fatalf("validateStatements accepted a body whose trailing statement preflight rejects")
			}
		})
	}
}

// The preflight statement walk also validates a lone no-result deferred call
// on its own, so the break path does not suppress the call's own validation.
func TestValidateStatementsAcceptsDeferredNoResultCallAlone(t *testing.T) {
	err := validateStatements(
		[]checker.Statement{deferredNoResultCall()},
		&expressionValidation{},
		&generatedTypeValidation{},
	)
	if err != nil {
		t.Fatalf("validateStatements rejected a lone valid deferred call: %v", err)
	}
}
