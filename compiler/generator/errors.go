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
		checker.ReturnStatement, checker.RootReturnStatement, checker.BreakStatement, checker.ContinueStatement,
		checker.DeferStatement, checker.ErrdeferStatement, checker.FunctionDeclaration,
		checker.MethodDeclaration, checker.UnsafeStatement:
		// Block statements carry no expressions beyond their own operands,
		// and leaf statements none; nested bodies hoist at their own
		// statement list.
	default:
		return unknownExpressionDiagnostic("unsupported checked statement")
	}
	return nil
}

// tryGuardModel carries one try guard's decided temp, tag test, and payload
// field; each guard form reads the fields it spells.
type tryGuardModel struct {
	Indent string
	Temp   string
	Tag    string
	Field  string
}

// tryArmModel carries one try Error or success arm's decided target, result
// type, tag, payload field, source temp, and source payload field.
type tryArmModel struct {
	Indent  string
	Name    string
	Type    string
	Tag     string
	Field   string
	Temp    string
	Payload string
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
	operand, err := renderExpressionExpectedWithState(*node.Operand, &node.OperandType, state)
	if err != nil {
		return err
	}
	operandUnion := node.OperandType
	errorIndex := node.MemberIndex
	operandMembers := compilerTypes.UnionMembers(operandUnion)
	errorMember, _ := operandMembers.At(errorIndex)
	// The temp copies the operand's real C variable, which keeps its
	// original (unnarrowed) declared type; declaring temp as the
	// flow-narrowed OperandType would initialize it from an incompatible
	// struct type whenever narrowing shrank the union. Tag values and
	// payload field names are shared across every union containing a given
	// member, so reading errorMember/success members off the widened temp
	// below still works unchanged.
	physicalType := operandUnion
	if node.OperandStorageType != (compilerTypes.Type{}) {
		physicalType = node.OperandStorageType
	}

	var builder strings.Builder
	if err := renderInto(&builder, "module.c", "const_decl", forStmtLineModel{Indent: indent, Type: physicalType.CName, Name: temp, Value: operand}); err != nil {
		return err
	}
	resultType := node.Element
	resultErrorIndex := -1
	if compilerTypes.IsError(resultType) {
		if err := renderInto(&builder, "module.c", "tag_test_open", tryGuardModel{Indent: indent, Temp: temp, Tag: state.tags.unionMemberTag(errorMember)}); err != nil {
			return err
		}
	} else {
		resultErrorIndex = unionMemberIndex(resultType, compilerTypes.ErrorType)
		if resultErrorIndex < 0 {
			return unknownExpressionDiagnostic("try result does not accept Error")
		}
		if err := renderInto(&builder, "module.c", "tag_test_open", tryGuardModel{Indent: indent, Temp: temp, Tag: state.tags.unionMemberTag(errorMember)}); err != nil {
			return err
		}
	}
	// The deferred actions unwind only on the Error path, before the Error
	// returns: the success path runs them at the scope's own exit, and
	// running them twice would double-release the same resources. The
	// unwind must precede the return so it executes.
	if err := unwindAllDefers(&builder, state, indent, "true"); err != nil {
		return err
	}
	if compilerTypes.IsError(resultType) {
		if err := renderInto(&builder, "module.c", "payload_return", tryGuardModel{Indent: indent, Temp: temp, Field: state.tags.unionPayloadField(errorMember)}); err != nil {
			return err
		}
	} else {
		resultMembers := compilerTypes.UnionMembers(resultType)
		resultErrorMember, _ := resultMembers.At(resultErrorIndex)
		if err := renderInto(&builder, "module.c", "union_return", tryArmModel{Indent: indent, Type: resultType.CName, Tag: state.tags.unionMemberTag(errorMember), Field: state.tags.unionPayloadField(resultErrorMember), Temp: temp, Payload: state.tags.unionPayloadField(errorMember)}); err != nil {
			return err
		}
	}
	if err := renderInto(&builder, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	success := node.ResultType
	if success.Union == nil {
		// Single success member: the try renders as its active payload.
		successIndex := unionMemberIndex(operandUnion, success)
		if successIndex < 0 {
			return unknownExpressionDiagnostic("try success member is missing from its source union")
		}
		successSourceMember, _ := operandMembers.At(successIndex)
		state.hoistedTries[node.Operand] = fmt.Sprintf("%s.payload.%s", temp, state.tags.unionPayloadField(successSourceMember))
		if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: builder.String()}); err != nil {
			return err
		}
		return nil
	}
	// Multiple success members: a switch materializes the narrowed success
	// union into one named temporary. The prologue above is rebuilt from
	// scratch below with the union-result return shape instead of reused,
	// so the accumulated single-return-shape copy is discarded first.
	state.tryCounter++
	resultTemp := fmt.Sprintf("hex_try_result_%d", state.tryCounter)
	builder.Reset()
	if err := renderInto(&builder, "module.c", "const_decl", forStmtLineModel{Indent: indent, Type: physicalType.CName, Name: temp, Value: operand}); err != nil {
		return err
	}
	resultErrorIndex = -1
	if !compilerTypes.IsError(resultType) {
		resultErrorIndex = unionMemberIndex(resultType, compilerTypes.ErrorType)
		if resultErrorIndex < 0 {
			return unknownExpressionDiagnostic("try result does not accept Error")
		}
	}
	if err := renderInto(&builder, "module.c", "tag_test_open", tryGuardModel{Indent: indent, Temp: temp, Tag: state.tags.unionMemberTag(errorMember)}); err != nil {
		return err
	}
	// The unwind must precede the return so it executes; see the identical
	// note on the first Error check above.
	if err := unwindAllDefers(&builder, state, indent, "true"); err != nil {
		return err
	}
	if compilerTypes.IsError(resultType) {
		if err := renderInto(&builder, "module.c", "payload_return", tryGuardModel{Indent: indent, Temp: temp, Field: state.tags.unionPayloadField(errorMember)}); err != nil {
			return err
		}
	} else {
		resultMembers := compilerTypes.UnionMembers(resultType)
		resultErrorMember, _ := resultMembers.At(resultErrorIndex)
		if err := renderInto(&builder, "module.c", "union_return", tryArmModel{Indent: indent, Type: resultType.CName, Tag: state.tags.unionMemberTag(errorMember), Field: state.tags.unionPayloadField(resultErrorMember), Temp: temp, Payload: state.tags.unionPayloadField(errorMember)}); err != nil {
			return err
		}
	}
	if err := renderInto(&builder, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	// mutable: every switch case below assigns resultTemp exactly once at
	// runtime, but a const declaration can't express that statically, and
	// the switch's own default: abort() is what makes every other case
	// unreachable, not something a const-then-assign pattern could express.
	if err := renderInto(&builder, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: declaration(success, resultTemp, true)}); err != nil {
		return err
	}
	if err := renderInto(&builder, "module.c", "switch_tag_open", tryGuardModel{Indent: indent, Temp: temp}); err != nil {
		return err
	}
	successMembers := compilerTypes.UnionMembers(success)
	for index := 0; index < successMembers.Len(); index++ {
		successMember, _ := successMembers.At(index)
		sourceIndex := unionMemberIndex(operandUnion, successMember)
		if sourceIndex < 0 {
			return unknownExpressionDiagnostic("try success member is missing from its source union")
		}
		targetSourceMember, _ := operandMembers.At(sourceIndex)
		if err := renderInto(&builder, "module.c", "case_tag", tryGuardModel{Indent: indent, Tag: state.tags.unionMemberTag(successMember)}); err != nil {
			return err
		}
		if compilerTypes.IsNil(successMember) || compilerTypes.IsEoS(successMember) {
			// Nil and EoS are tag-only members with no payload field to copy.
			if err := renderInto(&builder, "module.c", "tag_only_assign", tryArmModel{Indent: indent, Name: resultTemp, Type: success.CName, Tag: state.tags.unionMemberTag(successMember)}); err != nil {
				return err
			}
		} else {
			if err := renderInto(&builder, "module.c", "payload_assign", tryArmModel{Indent: indent, Name: resultTemp, Type: success.CName, Tag: state.tags.unionMemberTag(successMember), Field: state.tags.unionPayloadField(successMember), Temp: temp, Payload: state.tags.unionPayloadField(targetSourceMember)}); err != nil {
				return err
			}
		}
		if err := renderInto(&builder, "module.c", "break_stmt", indentModel{Indent: indent + "    "}); err != nil {
			return err
		}
	}
	if err := renderInto(&builder, "module.c", "default_abort", indentModel{Indent: indent}); err != nil {
		return err
	}
	state.hoistedTries[node.Operand] = resultTemp
	if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: builder.String()}); err != nil {
		return err
	}
	return nil
}

// renderTryExpression renders a hoisted try as its success value. The
// prologue was emitted before the enclosing statement; an unhoisted try is
// an internal compiler failure.
func renderTryExpression(node checker.Expression, state *expressionValidation) (string, error) {
	name, ok := state.hoistedTries[node.Operand]
	if !ok {
		return "", unknownExpressionDiagnostic("try expression reached generation without hoisting")
	}
	return name, nil
}

// renderErrorHeader renders Error.header(): the allocation-free header
// derived from the receiver's stored kind.
func renderErrorHeader(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || !compilerTypes.IsError(node.OperandType) {
		return "", unknownExpressionDiagnostic("Error.header has invalid checked metadata")
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_error_kind_header(%s.hex_m_kind)", receiver), nil
}

// renderErrorKindHeader renders ErrorKind.header(): the same derivation
// called directly on a classification value.
func renderErrorKindHeader(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil || !compilerTypes.IsErrorKind(node.OperandType) {
		return "", unknownExpressionDiagnostic("ErrorKind.header has invalid checked metadata")
	}
	receiver, err := renderReceiver(node.Operand, node.OperandType, state)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("hex_error_kind_header(%s)", receiver), nil
}
