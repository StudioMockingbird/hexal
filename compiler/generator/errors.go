package generator

import (
	"fmt"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// RFC 0029: Error values, `try` propagation, and error-only errdefer
// cleanup.

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
	if err := walkProgram(program, visitor); err != nil {
		panic(err)
	}
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
func returnErrorExit(valueType compilerTypes.Type, valueName string) string {
	if compilerTypes.IsError(valueType) {
		return "true"
	}
	if compilerTypes.IsUnion(valueType) {
		if index := unionMemberIndex(valueType, compilerTypes.ErrorType); index >= 0 {
			return fmt.Sprintf("(%s.tag == %s)", valueName, unionTagName(valueType, index))
		}
	}
	return "false"
}

// writeErrorDefinition emits the built-in Error object's full definition.
// It runs before the union definitions that may carry Error as a payload
// member (RFC 0029).
func writeErrorDefinition(result *strings.Builder) {
	object := compilerTypes.ErrorType.Object
	fmt.Fprintf(result, "\ntypedef struct %s %s;\nstruct %s {\n", object.CName, object.CName, object.CName)
	for _, member := range object.Members {
		fmt.Fprintf(result, "    %s;\n", declaration(member.Type, PrivateCName(MemberName, member.Name, ""), true))
	}
	fmt.Fprintf(result, "};\n")
}

// hoistTryInStatement walks one checked statement's expressions in
// evaluation order and emits the RFC 0029 try prologues (the operand
// temporary plus the Error-return branch) before the statement renders. Each
// try node is then replaced by its hoisted success value.
func hoistTryInStatement(statement checker.Statement, body *strings.Builder, state *expressionValidation, result *compilerTypes.Type, indent string) error {
	// RFC 0057 Item 5: expression traversal lives in the shared
	// walkStatementExpressions; this hoister only acts on try nodes and
	// recurses into nested statement bodies itself.
	if err := walkStatementExpressions(statement, func(node *checker.Expression) error {
		if node.Kind == checker.TryExpression && node.Operand != nil {
			return hoistTry(node, body, state, result, indent)
		}
		return nil
	}); err != nil {
		return err
	}
	switch statement := statement.(type) {
	case checker.IfStatement:
		for _, nested := range statement.Then {
			if err := hoistTryInStatement(nested, body, state, result, indent); err != nil {
				return err
			}
		}
		for _, branch := range statement.ElseIf {
			for _, nested := range branch.Body {
				if err := hoistTryInStatement(nested, body, state, result, indent); err != nil {
					return err
				}
			}
		}
		if statement.Else != nil {
			for _, nested := range statement.Else {
				if err := hoistTryInStatement(nested, body, state, result, indent); err != nil {
					return err
				}
			}
		}
	case checker.ForStatement:
		for _, nested := range statement.Body {
			if err := hoistTryInStatement(nested, body, state, result, indent); err != nil {
				return err
			}
		}
	case checker.WhileStatement:
		for _, nested := range statement.Body {
			if err := hoistTryInStatement(nested, body, state, result, indent); err != nil {
				return err
			}
		}
	}
	return nil
}

// hoistTry emits one try prologue: the operand evaluates exactly once into a
// temporary; on Error the eligible defers and errdefers unwind and the Error
// returns through the enclosing function's declared result; otherwise the
// temporary yields the active success value.
func hoistTry(node *checker.Expression, body *strings.Builder, state *expressionValidation, result *compilerTypes.Type, indent string) error {
	if node.Operand == nil || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 {
		return unknownExpressionDiagnostic("try expression has invalid checked metadata")
	}
	state.tryCounter++
	temp := fmt.Sprintf("hex_try_%d", state.tryCounter)
	if state.hoistedTries == nil {
		state.hoistedTries = make(map[*checker.Expression]string)
	}
	operand, err := renderExpressionExpectedWithState(*node.Operand, node.OperandType, true, state)
	if err != nil {
		return err
	}
	operandUnion := node.OperandType
	errorIndex := node.MemberIndex

	var builder strings.Builder
	fmt.Fprintf(&builder, "%sconst %s %s = %s;\n", indent, operandUnion.CName, temp, operand)
	resultType := node.Element
	resultErrorIndex := -1
	if compilerTypes.IsError(resultType) {
		fmt.Fprintf(&builder, "%sif (%s.tag == %s) {\n", indent, temp, unionTagName(operandUnion, errorIndex))
	} else {
		resultErrorIndex = unionMemberIndex(resultType, compilerTypes.ErrorType)
		if resultErrorIndex < 0 {
			return unknownExpressionDiagnostic("try result does not accept Error")
		}
		fmt.Fprintf(&builder, "%sif (%s.tag == %s) {\n", indent, temp, unionTagName(operandUnion, errorIndex))
	}
	// The deferred actions unwind only on the Error path, before the Error
	// returns: the success path runs them at the scope's own exit, and
	// running them twice would double-release the same resources. The
	// unwind must precede the return so it executes (RFC 0048 conformance:
	// the previous order emitted unreachable unwind code after the return).
	if err := unwindAllDefers(&builder, state, indent, "true"); err != nil {
		return err
	}
	if compilerTypes.IsError(resultType) {
		fmt.Fprintf(&builder, "%s    return %s.payload.member_%d;\n", indent, temp, errorIndex)
	} else {
		fmt.Fprintf(&builder, "%s    return (%s){ .tag = %s, .payload.member_%d = %s.payload.member_%d };\n", indent, resultType.CName, unionTagName(resultType, resultErrorIndex), resultErrorIndex, temp, errorIndex)
	}
	fmt.Fprintf(&builder, "%s}\n", indent)
	success := node.ResultType
	if success.Union == nil {
		// Single success member: the try renders as its active payload.
		successIndex := unionMemberIndex(operandUnion, success)
		if successIndex < 0 {
			return unknownExpressionDiagnostic("try success member is missing from its source union")
		}
		state.hoistedTries[node.Operand] = fmt.Sprintf("%s.payload.member_%d", temp, successIndex)
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
		fmt.Fprintf(&builder, "%sif (%s.tag == %s) {\n", indent, temp, unionTagName(operandUnion, errorIndex))
		fmt.Fprintf(&builder, "%s    return %s.payload.member_%d;\n", indent, temp, errorIndex)
	} else {
		resultErrorIndex := unionMemberIndex(resultType, compilerTypes.ErrorType)
		if resultErrorIndex < 0 {
			return unknownExpressionDiagnostic("try result does not accept Error")
		}
		fmt.Fprintf(&builder, "%sif (%s.tag == %s) {\n", indent, temp, unionTagName(operandUnion, errorIndex))
		fmt.Fprintf(&builder, "%s    return (%s){ .tag = %s, .payload.member_%d = %s.payload.member_%d };\n", indent, resultType.CName, unionTagName(resultType, resultErrorIndex), resultErrorIndex, temp, errorIndex)
	}
	if err := unwindAllDefers(&builder, state, indent, "true"); err != nil {
		return err
	}
	fmt.Fprintf(&builder, "%s}\n", indent)
	fmt.Fprintf(&builder, "%s%s;\n", indent, declaration(success, resultTemp, false))
	fmt.Fprintf(&builder, "%sswitch (%s.tag) {\n", indent, temp)
	successMembers := compilerTypes.UnionMembers(success)
	for _, successMember := range successMembers {
		sourceIndex := unionMemberIndex(operandUnion, successMember)
		if sourceIndex < 0 {
			return unknownExpressionDiagnostic("try success member is missing from its source union")
		}
		targetIndex := unionMemberIndex(success, successMember)
		fmt.Fprintf(&builder, "%scase %s:\n", indent, unionTagName(operandUnion, sourceIndex))
		fmt.Fprintf(&builder, "%s    %s = (%s){ .tag = %s, .payload.member_%d = %s.payload.member_%d };\n", indent, resultTemp, success.CName, unionTagName(success, targetIndex), targetIndex, temp, sourceIndex)
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
