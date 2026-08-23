package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// discoverErrorUsed reports whether the program references the built-in
// Error type anywhere (directly or inside a union), which requires its
// generated object definition.
func discoverErrorUsed(program checker.Program) bool {
	used := false
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			if compilerTypes.IsError(typ) || compilerTypes.IsUnion(typ) && unionMemberIndex(typ, compilerTypes.ErrorType) >= 0 {
				used = true
			}
			return nil
		},
		Operand: func(source checker.Operand) error {
			if compilerTypes.IsError(source.Type) || compilerTypes.IsUnion(source.Type) && unionMemberIndex(source.Type, compilerTypes.ErrorType) >= 0 {
				used = true
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			if compilerTypes.IsError(node.OperandType) || compilerTypes.IsError(node.ResultType) || compilerTypes.IsUnion(node.OperandType) && unionMemberIndex(node.OperandType, compilerTypes.ErrorType) >= 0 || compilerTypes.IsUnion(node.ResultType) && unionMemberIndex(node.ResultType, compilerTypes.ErrorType) >= 0 {
				used = true
			}
			return nil
		},
	}
	walkProgram(program, visitor)
	return used
}
func hasPendingErrDefers(state *expressionValidation) bool {
	for _, scope := range state.deferStack {
		for _, action := range scope {
			if action.Err {
				return true
			}
		}
	}
	return false
}

// hasPendingActions reports whether any enclosing scope has registered a
// deferred or errdeferred action that must run before an exit edge.
func hasPendingActions(state *expressionValidation) bool {
	for _, scope := range state.deferStack {
		if len(scope) > 0 {
			return true
		}
	}
	return false
}

// returnErrorExit renders the compile-time or runtime exit classification of
// one return value: true when the value is exactly Error, a runtime tag test
// when the value is a union containing Error, and false otherwise.
func returnErrorExit(valueType compilerTypes.Type, valueName string, tags *tagRegistry) string {
	if compilerTypes.IsError(valueType) {
		return "true"
	}
	if compilerTypes.IsUnion(valueType) {
		if index := unionMemberIndex(valueType, compilerTypes.ErrorType); index >= 0 {
			members := compilerTypes.UnionMembers(valueType)
			errorMember, _ := members.At(index)
			return fmt.Sprintf("(%s.tag == %s)", valueName, tags.unionMemberTag(errorMember))
		}
	}
	return "false"
}

// hoistTryInStatement walks one checked statement's expressions in
// evaluation order and emits each try prologue (the operand temporary plus
// the Error-return branch) before the statement renders. Each try node is
// then replaced by its hoisted success value.
func hoistTryInStatement(statement checker.Statement, body *strings.Builder, state *expressionValidation, result *compilerTypes.Type, indent string) error {
	// Expression traversal lives in the shared walkStatementExpressions,
	// which visits only this statement's own expressions: a try inside a
	// nested statement body is hoisted when that body's own statement list
	// renders, so the prologue lands inside the block that contains it.
	if err := walkStatementExpressions(statement, func(node checker.Expression) error {
		if node.Kind == checker.TryExpression && node.Operand != nil {
			return hoistTry(node, body, state, result, indent)
		}
		return nil
	}); err != nil {
		return err
	}
	switch statement.(type) {
	case checker.IfStatement, checker.ForStatement, checker.WhileStatement,
		checker.Declaration, checker.Assignment, checker.CallStatement, checker.TryStatement,
		checker.ReturnStatement, checker.BreakStatement, checker.ContinueStatement,
		checker.DeferStatement, checker.ErrdeferStatement, checker.FunctionDeclaration,
		checker.MethodDeclaration, checker.LocalFunctionDeclaration:
		// Block statements carry no expressions beyond their own operands,
		// and leaf statements none; nested bodies hoist at their own
		// statement list.
	default:
		return unknownExpressionDiagnostic("unsupported checked statement")
	}
	return nil
}

// hoistTry emits one try prologue: the operand evaluates exactly once into a
// temporary; on Error the eligible defers and errdefers unwind and the Error
// returns through the enclosing function's declared result; otherwise the
// temporary yields the active success value.
func hoistTry(node checker.Expression, body *strings.Builder, state *expressionValidation, result *compilerTypes.Type, indent string) error {
	if node.Operand == nil || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 {
		return unknownExpressionDiagnostic("try expression has invalid checked metadata")
	}
	state.tryCounter++
	temp := fmt.Sprintf("hex_try_%d", state.tryCounter)
	if state.hoistedTries == nil {
		state.hoistedTries = make(map[*checker.Expression]string)
	}
	operand, err := renderExpressionExpectedWithState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return err
	}
	operandUnion := node.OperandType
	errorIndex := node.MemberIndex
	operandMembers := compilerTypes.UnionMembers(operandUnion)
	errorMember, _ := operandMembers.At(errorIndex)

	var builder strings.Builder
	fmt.Fprintf(&builder, "%sconst %s %s = %s;\n", indent, operandUnion.CName, temp, operand)
	resultType := node.Element
	resultErrorIndex := -1
	if compilerTypes.IsError(resultType) {
		fmt.Fprintf(&builder, "%sif (%s.tag == %s) {\n", indent, temp, state.tags.unionMemberTag(errorMember))
	} else {
		resultErrorIndex = unionMemberIndex(resultType, compilerTypes.ErrorType)
		if resultErrorIndex < 0 {
			return unknownExpressionDiagnostic("try result does not accept Error")
		}
		fmt.Fprintf(&builder, "%sif (%s.tag == %s) {\n", indent, temp, state.tags.unionMemberTag(errorMember))
	}
	// The deferred actions unwind only on the Error path, before the Error
	// returns: the success path runs them at the scope's own exit, and
	// running them twice would double-release the same resources. The
	// unwind must precede the return so it executes.
	if err := unwindAllDefers(&builder, state, indent, "true"); err != nil {
		return err
	}
	if compilerTypes.IsError(resultType) {
		fmt.Fprintf(&builder, "%s    return %s.payload.%s;\n", indent, temp, state.tags.unionPayloadField(errorMember))
	} else {
		resultMembers := compilerTypes.UnionMembers(resultType)
		resultErrorMember, _ := resultMembers.At(resultErrorIndex)
		fmt.Fprintf(&builder, "%s    return (%s){ .tag = %s, .payload.%s = %s.payload.%s };\n", indent, resultType.CName, state.tags.unionMemberTag(errorMember), state.tags.unionPayloadField(resultErrorMember), temp, state.tags.unionPayloadField(errorMember))
	}
	fmt.Fprintf(&builder, "%s}\n", indent)
	success := node.ResultType
	if success.Union == nil {
		// Single success member: the try renders as its active payload.
		successIndex := unionMemberIndex(operandUnion, success)
		if successIndex < 0 {
			return unknownExpressionDiagnostic("try success member is missing from its source union")
		}
		successSourceMember, _ := operandMembers.At(successIndex)
		state.hoistedTries[node.Operand] = fmt.Sprintf("%s.payload.%s", temp, state.tags.unionPayloadField(successSourceMember))
		body.WriteString(builder.String())
		return nil
	}
	// Multiple success members: a switch materializes the narrowed success
	// union into one named temporary.
	state.tryCounter++
	resultTemp := fmt.Sprintf("hex_try_result_%d", state.tryCounter)
	fmt.Fprintf(&builder, "%s%s %s;\n", indent, declaration(success, resultTemp, false), "")
	builder.Reset()
	fmt.Fprintf(&builder, "%sconst %s %s = %s;\n", indent, operandUnion.CName, temp, operand)
	if compilerTypes.IsError(resultType) {
		fmt.Fprintf(&builder, "%sif (%s.tag == %s) {\n", indent, temp, state.tags.unionMemberTag(errorMember))
		fmt.Fprintf(&builder, "%s    return %s.payload.%s;\n", indent, temp, state.tags.unionPayloadField(errorMember))
	} else {
		resultErrorIndex := unionMemberIndex(resultType, compilerTypes.ErrorType)
		if resultErrorIndex < 0 {
			return unknownExpressionDiagnostic("try result does not accept Error")
		}
		resultMembers := compilerTypes.UnionMembers(resultType)
		resultErrorMember, _ := resultMembers.At(resultErrorIndex)
		fmt.Fprintf(&builder, "%sif (%s.tag == %s) {\n", indent, temp, state.tags.unionMemberTag(errorMember))
		fmt.Fprintf(&builder, "%s    return (%s){ .tag = %s, .payload.%s = %s.payload.%s };\n", indent, resultType.CName, state.tags.unionMemberTag(errorMember), state.tags.unionPayloadField(resultErrorMember), temp, state.tags.unionPayloadField(errorMember))
	}
	if err := unwindAllDefers(&builder, state, indent, "true"); err != nil {
		return err
	}
	fmt.Fprintf(&builder, "%s}\n", indent)
	fmt.Fprintf(&builder, "%s%s;\n", indent, declaration(success, resultTemp, false))
	fmt.Fprintf(&builder, "%sswitch (%s.tag) {\n", indent, temp)
	successMembers := compilerTypes.UnionMembers(success)
	for index := 0; index < successMembers.Len(); index++ {
		successMember, _ := successMembers.At(index)
		sourceIndex := unionMemberIndex(operandUnion, successMember)
		if sourceIndex < 0 {
			return unknownExpressionDiagnostic("try success member is missing from its source union")
		}
		targetSourceMember, _ := operandMembers.At(sourceIndex)
		fmt.Fprintf(&builder, "%scase %s:\n", indent, state.tags.unionMemberTag(successMember))
		fmt.Fprintf(&builder, "%s    %s = (%s){ .tag = %s, .payload.%s = %s.payload.%s };\n", indent, resultTemp, success.CName, state.tags.unionMemberTag(successMember), state.tags.unionPayloadField(successMember), temp, state.tags.unionPayloadField(targetSourceMember))
		fmt.Fprintf(&builder, "%s    break;\n", indent)
	}
	fmt.Fprintf(&builder, "%sdefault:\n%s    abort();\n%s}\n", indent, indent, indent)
	state.hoistedTries[node.Operand] = resultTemp
	body.WriteString(builder.String())
	return nil
}

// renderTryExpression renders a hoisted try as its success value. The
// prologue was emitted before the enclosing statement; an unhoisted try is
// an internal compiler failure.
func renderTryExpression(node checker.Expression, state *expressionValidation) (string, error) {
	if state.hoistedTries == nil {
		return "", unknownExpressionDiagnostic("try expression reached generation without hoisting")
	}
	name, ok := state.hoistedTries[node.Operand]
	if !ok {
		return "", unknownExpressionDiagnostic("try expression reached generation without hoisting")
	}
	return name, nil
}
