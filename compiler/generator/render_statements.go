// render_statements.go owns statement-list rendering: writeStatements and
// writeStatementsAt, control-header and call and return statement lowering,
// and condition truthiness rendering.
package generator

import (
	"fmt"
	"strconv"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// statementFrame is the enclosing-function context one nested statement list
// renders under: the declared result gating return-flow checks, whether the
// list sits inside a function body at all, and the checked scope's registered
// deferred actions emitted in reverse order when the list completes.
type statementFrame struct {
	result     *compilerTypes.Type
	inFunction bool
	defers     []checker.DeferredAction
}

// writeStatements renders one statement list at a single indentation level.
// main's module statements and a function body share it.
func writeStatements(body *strings.Builder, statements []checker.Statement, state *expressionValidation, result *compilerTypes.Type, inFunction bool, defers []checker.DeferredAction) error {
	return writeStatementsAt(body, statements, state, statementFrame{result: result, inFunction: inFunction, defers: defers}, "    ")
}

// isFullyParenthesized reports whether expr is exactly one top-level,
// balanced parenthesis pair spanning its entire length -- the shape every
// comparison and logical-operator rendering produces. writeControlHeader
// uses this to avoid wrapping it in a second pair: "((x == y))" is exactly
// what -Wparentheses-equality flags under -Werror, one paren pair is all
// C's if/while grammar needs, and the inner one already supplies it.
func isFullyParenthesized(expr string) bool {
	if len(expr) < 2 || expr[0] != '(' || expr[len(expr)-1] != ')' {
		return false
	}
	depth := 0
	for index, char := range expr {
		switch char {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 && index != len(expr)-1 {
				return false
			}
		}
	}
	return depth == 0
}

// controlHeaderModel carries one control opener's decided indent, keyword
// prefix, and condition; each opener form reads the fields it spells.
type controlHeaderModel struct {
	Indent    string
	Prefix    string
	Condition string
}

// callStmtModel carries one call statement's decided call expression.
type callStmtModel struct {
	Indent string
	Call   string
}

// lineDirectiveModel carries one #line directive's source line and file.
type lineDirectiveModel struct {
	Line string
	File string
}

// objectFieldModel is one object literal's decided member initializer.
type objectFieldModel struct {
	Name  string
	Value string
}

// objectLiteralModel carries one object literal's result type and ordered
// member initializers in written order.
type objectLiteralModel struct {
	Type   string
	Fields []objectFieldModel
}

func writeControlHeader(body *strings.Builder, indent, prefix, condition string, keywordLine, conditionLine int, filename string) error {
	if err := writeLineDirective(body, keywordLine, filename); err != nil {
		return err
	}
	if isFullyParenthesized(condition) {
		condition = condition[1 : len(condition)-1]
	}
	if conditionLine > 0 && conditionLine != keywordLine {
		if err := renderInto(body, "module.c", "control_keyword", controlHeaderModel{Indent: indent, Prefix: prefix}); err != nil {
			return err
		}
		if err := writeLineDirective(body, conditionLine, filename); err != nil {
			return err
		}
		return renderInto(body, "module.c", "control_condition", controlHeaderModel{Indent: indent, Condition: condition})
	}
	return renderInto(body, "module.c", "control_open", controlHeaderModel{Indent: indent, Prefix: prefix, Condition: condition})
}

func writeStatementsAt(body *strings.Builder, statements []checker.Statement, state *expressionValidation, frame statementFrame, indent string) error {
	if len(state.activeScopes) == 0 {
		state.pushScope()
		defer state.popScope()
	}
	state.deferStack = append(state.deferStack, frame.defers)
	defer func() { state.deferStack = state.deferStack[:len(state.deferStack)-1] }()
	for _, statement := range statements {
		// Spawn prologues emit before the try prologues so a try operand
		// that spawns can name the already-created task handle.
		if err := hoistConcurrencyInStatement(statement, body, state, indent); err != nil {
			return err
		}
		if err := hoistDictFindInStatement(statement, body, state, indent); err != nil {
			return err
		}
		// Try prologues for this statement emit before it renders, in
		// evaluation order, so nested and repeated operands evaluate once.
		if err := hoistTryInStatement(statement, body, state, frame.result, indent); err != nil {
			return err
		}
		// String.interpolate prologues emit after try (a try embedded in an
		// interpolated value must already be resolved when its segment
		// renders) and before evaluation-order sequencing.
		if err := hoistInterpolateInStatement(statement, body, state, indent); err != nil {
			return err
		}
		// Evaluation-order sequencing runs last: it treats every
		// already-hoisted try/spawn/Dict.find node above as a resolved
		// effect boundary rather than recursing into it a second time.
		if err := hoistEvaluationOrderInStatement(statement, body, state, indent); err != nil {
			return err
		}
		switch statement := statement.(type) {
		case checker.Declaration:
			if err := writeDeclarationStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.Assignment:
			if err := writeAssignmentStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.CallStatement:
			if err := writeCallStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.TryStatement:
			// The try prologue already hoisted above; the success value is
			// discarded, so the statement renders nothing.
			if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
				return err
			}
		case checker.ReturnStatement:
			if err := writeReturnStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.RootReturnStatement:
			if err := writeRootReturnStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.IfStatement:
			if err := writeIfStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.WhileStatement:
			if err := writeWhileStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.ForStatement:
			if err := renderForStatement(body, statement, state, frame.result, frame.inFunction, indent); err != nil {
				return err
			}
		case checker.UnsafeStatement:
			// The permission region has no runtime meaning, so it emits no
			// braces, guard, or marker of its own: the enclosed statements
			// render at this level, in source order. Their source scope
			// survives through the generator's own unique-name scope.
			state.pushScope()
			err := writeStatementsAt(body, statement.Body, state, statementFrame{result: frame.result, inFunction: frame.inFunction, defers: statement.BodyDefers}, indent)
			state.popScope()
			if err != nil {
				return err
			}
		case checker.BreakStatement:
			if err := writeBreakStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.ContinueStatement:
			if err := writeContinueStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.DeferStatement:
			if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
				return err
			}
			if err := writeDeferStatement(body, statement, state, indent); err != nil {
				return err
			}
		case checker.ErrdeferStatement:
			if err := writeErrdeferStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.FunctionDeclaration:
			if err := writeFunctionDeclarationStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		case checker.MethodDeclaration:
			if err := writeMethodDeclarationStatement(statement, body, state, frame, indent); err != nil {
				return err
			}
		default:
			return unknownExpressionDiagnostic()
		}
	}
	if len(frame.defers) > 0 {
		if err := writeDeferredActions(body, frame.defers, state, indent, "false"); err != nil {
			return err
		}
	}
	return nil
}

// renderRestSliceArgument packs a rest call's trailing arguments into the one
// read-only Slice<T> the callee's C signature takes. Zero elements pass the
// canonical empty Slice; one or more pass a compound-literal array. Every
// element expression has already been evaluated, once and in source order,
// into its own temporary by the sequencing pass when it may observe, so the
// initializer list itself introduces no source-visible evaluation.
func renderRestSliceArgument(node *checker.Expression, arguments []string) []string {
	if !node.Rest {
		return arguments
	}
	fixed := node.RestStart
	if fixed > len(arguments) {
		fixed = len(arguments)
	}
	rest := arguments[fixed:]
	data := "nullptr"
	if len(rest) > 0 {
		data = "(" + typeSpelling(node.RestElement) + "[]){" + strings.Join(rest, ", ") + "}"
	}
	literal := "(" + typeSpelling(node.RestSlice) + "){.data = " + data + ", .length = " + strconv.Itoa(len(rest)) + "}"
	packed := make([]string, 0, fixed+1)
	packed = append(packed, arguments[:fixed]...)
	packed = append(packed, literal)
	return packed
}

// validateCallStatement checks a discarded call's structure without rendering
// it. The preflight state has no program-wide tag registry, so rendering an
// argument that constructs an ADT would dereference a nil registry; the
// emission pass owns rendering, with the registry, and re-checks the same
// structure.
func validateCallStatement(statement checker.CallStatement, state *expressionValidation) error {
	if statement.Call.Kind == checker.ObjectOperand {
		if statement.Call.Object == nil || !compilerTypes.Equal(statement.Call.Type, statement.Call.Object.Type) {
			return unknownExpressionDiagnostic()
		}
		return validateCheckedOperandWithState(statement.Call, state)
	}
	switch statement.Call.Node.Kind {
	case checker.CallExpression, checker.MethodCallExpression, checker.StringMethodCallExpression, checker.CollectionMethodCallExpression, checker.ListNewExpression, checker.DictNewExpression,
		checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression,
		checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression,
		checker.MutexConstructorExpression, checker.MutexMethodCallExpression,
		checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression,
		checker.StashConstructorExpression, checker.StashMethodCallExpression,
		checker.PoolConstructorExpression, checker.PoolMethodCallExpression,
		checker.VolatileReadExpression, checker.VolatileWriteExpression,
		checker.HeapFreeExpression, checker.HeapAllocateExpression,
		checker.HeapAllocateAlignedExpression,
		checker.BitCastExpression, checker.EndianConversionExpression, checker.ConversionExpression,
		checker.RuneMethodCallExpression,
		checker.CursorMethodCallExpression,
		checker.GraphemeMethodCallExpression,
		checker.LayoutExpression, checker.SliceBridgeExpression, checker.BytesOverExpression,
		checker.StreamConstructorExpression, checker.StreamMethodCallExpression, checker.TimeExpression,
		checker.NetworkExpression, checker.CorelibCallExpression:
		// Discarding a constructor or a pure computation's result is legal;
		// at worst it leaks an allocation or wastes a computation, both the
		// programmer's choice.
	default:
		return unknownExpressionDiagnostic()
	}
	if !compilerTypes.Equal(statement.Call.Type, statement.Call.Node.ResultType) {
		return unknownExpressionDiagnostic()
	}
	return validateExpressionNode(statement.Call.Node, nil, state)
}

func renderCallStatement(statement checker.CallStatement, state *expressionValidation) (string, error) {
	if statement.Call.Kind == checker.ObjectOperand {
		// Error.new(...) checks to an ObjectOperand rather than a Node-carrying
		// ExpressionOperand; discarding it (like any other call result) is
		// legal, but it has no Node for the switch below to dispatch on.
		if statement.Call.Object == nil || !compilerTypes.Equal(statement.Call.Type, statement.Call.Object.Type) {
			return "", unknownExpressionDiagnostic()
		}
		return objectLiteralWithState(statement.Call.Object, state)
	}
	switch statement.Call.Node.Kind {
	case checker.CallExpression, checker.MethodCallExpression, checker.StringMethodCallExpression, checker.CollectionMethodCallExpression, checker.ListNewExpression, checker.DictNewExpression,
		checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression,
		checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression,
		checker.MutexConstructorExpression, checker.MutexMethodCallExpression,
		checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression,
		checker.StashConstructorExpression, checker.StashMethodCallExpression,
		checker.PoolConstructorExpression, checker.PoolMethodCallExpression,
		checker.VolatileReadExpression, checker.VolatileWriteExpression,
		checker.HeapFreeExpression, checker.HeapAllocateExpression,
		checker.HeapAllocateAlignedExpression,
		checker.BitCastExpression, checker.EndianConversionExpression, checker.ConversionExpression,
		checker.RuneMethodCallExpression,
		checker.CursorMethodCallExpression,
		checker.GraphemeMethodCallExpression,
		checker.LayoutExpression, checker.SliceBridgeExpression, checker.BytesOverExpression,
		checker.StreamConstructorExpression, checker.StreamMethodCallExpression, checker.TimeExpression,
		checker.NetworkExpression, checker.CorelibCallExpression:
		// Discarding a constructor or a pure computation's result is legal;
		// at worst it leaks an allocation or wastes a computation, both the
		// programmer's choice.
	default:
		return "", unknownExpressionDiagnostic()
	}
	if !compilerTypes.Equal(statement.Call.Type, statement.Call.Node.ResultType) {
		return "", unknownExpressionDiagnostic()
	}
	return renderExpressionExpectedWithState(statement.Call.Node, nil, state)
}

func renderReturnStatement(statement checker.ReturnStatement, result *compilerTypes.Type, state *expressionValidation, indent string) (string, error) {
	if statement.Value == nil {
		if result != nil {
			return "", unknownExpressionDiagnostic()
		}
		return indent + "return;\n", nil
	}
	if result == nil {
		return "", unknownExpressionDiagnostic()
	}
	if !generatedAssignable(*result, statement.Value.Type) {
		return "", unknownExpressionDiagnostic()
	}
	if statement.Value.Node.Kind == checker.MatchExpression {
		var builder strings.Builder
		resultName, err := renderMatchStatement(&builder, statement.Value.Node, state, indent)
		if err != nil {
			return "", err
		}
		if err := unwindAllDefers(&builder, state, indent, "false"); err != nil {
			return "", err
		}
		if err := renderInto(&builder, "module.c", "return_stmt", forStmtLineModel{Indent: indent, Value: resultName}); err != nil {
			return "", err
		}
		return builder.String(), nil
	}
	value, err := renderOperandWithState(*statement.Value, state)
	if err != nil {
		return "", err
	}
	// The return value is evaluated first, then every pending deferred action
	// runs from innermost to outermost scope, then the return executes. When
	// errdefers are pending, the exit classification decides which actions
	// run: an Error exit runs defers and errdefers, any other exit runs only
	// defers.
	if hasPendingActions(state) {
		state.returnCounter++
		name := fmt.Sprintf("hex_return_%d", state.returnCounter)
		var builder strings.Builder
		if err := renderInto(&builder, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: declaration(*result, name, false), Value: value}); err != nil {
			return "", err
		}
		if hasPendingErrDefers(state) {
			state.returnCounter++
			errorName := fmt.Sprintf("hex_err_%d", state.returnCounter)
			if err := renderInto(&builder, "module.c", "error_exit_decl", forStmtLineModel{Indent: indent, Name: errorName, Value: returnErrorExit(statement.Value.Type, name, state.tags)}); err != nil {
				return "", err
			}
			if err := unwindAllDefers(&builder, state, indent, errorName); err != nil {
				return "", err
			}
		} else if err := unwindAllDefers(&builder, state, indent, "false"); err != nil {
			return "", err
		}
		if err := renderInto(&builder, "module.c", "return_stmt", forStmtLineModel{Indent: indent, Value: name}); err != nil {
			return "", err
		}
		return builder.String(), nil
	}
	return indent + "return " + value + ";\n", nil
}

// renderRootReturnStatement lowers one entry-module return: the status value
// is evaluated once into the entry status slot, every active defer runs from
// the innermost scope outward, and control jumps to the entry cleanup label.
// A bare return records zero.
func renderRootReturnStatement(statement checker.RootReturnStatement, state *expressionValidation, indent string) (string, error) {
	var builder strings.Builder
	if statement.Value == nil {
		if err := renderInto(&builder, "module.c", "exit_status_zero", indentModel{Indent: indent}); err != nil {
			return "", err
		}
	} else {
		value, err := renderOperandWithState(*statement.Value, state)
		if err != nil {
			return "", err
		}
		if err := renderInto(&builder, "module.c", "exit_status_value", forStmtLineModel{Indent: indent, Value: value}); err != nil {
			return "", err
		}
	}
	if err := unwindAllDefers(&builder, state, indent, "false"); err != nil {
		return "", err
	}
	if err := renderInto(&builder, "module.c", "goto_exit", indentModel{Indent: indent}); err != nil {
		return "", err
	}
	return builder.String(), nil
}

// renderTruthiness renders a checked condition: nil is false, Bool and
// nullable values render as themselves, and every other value is evaluated
// once and then yields true.
func renderTruthiness(operand *checker.Operand, state *expressionValidation) (string, error) {
	if compilerTypes.Truthiness(operand.Type) == compilerTypes.TruthinessNil {
		return "false", nil
	}
	if err := validateCondition(*operand, state); err != nil {
		return "", err
	}
	rendered, err := renderOperandWithState(*operand, state)
	if err != nil {
		return "", err
	}
	return truthinessExpression(operand.Type, rendered, state)
}

// renderTruthinessChild renders a logical operand through its truthiness.
// The nil literal renders as false without touching the fail-closed nil
// rendering paths. parentOperandType is a last-resort classification for
// nodes whose own type metadata is absent; well-formed checked operands
// always resolve their own type.
func renderTruthinessChild(child *checker.Expression, state *expressionValidation, parentOperandType compilerTypes.Type) (string, error) {
	if child.Kind == checker.NilExpression {
		return "false", nil
	}
	if err := validateTruthinessChild(child, state); err != nil {
		return "", err
	}
	childType, ok := expressionTypeWithState(*child, state)
	if !ok {
		childType = parentOperandType
	}
	if compilerTypes.Truthiness(childType) == compilerTypes.TruthinessNil {
		return "false", nil
	}
	rendered, err := renderExpressionExpectedWithState(*child, nil, state)
	if err != nil {
		return "", err
	}
	return truthinessExpression(childType, rendered, state)
}

// truthinessExpression wraps a rendered value so its truthiness is the C
// result: Bool stays as-is, nil becomes false, a nullable becomes a null
// test, and every other value is evaluated once and then yields true.
// The comma expression keeps evaluation order and side effects intact and
// composes with &&/|| short-circuiting.
func truthinessExpression(typ compilerTypes.Type, rendered string, state *expressionValidation) (string, error) {
	switch compilerTypes.Truthiness(typ) {
	case compilerTypes.TruthinessBool:
		return rendered, nil
	case compilerTypes.TruthinessNil:
		return "false", nil
	case compilerTypes.TruthinessNullable:
		base, ok := compilerTypes.NullableBase(typ)
		if !ok || !compilerTypes.IsPointerLike(base) {
			return "", unknownExpressionDiagnostic()
		}
		return "(" + rendered + " != nullptr)", nil
	case compilerTypes.TruthinessUnion:
		if typ.Union == nil {
			return "", unknownExpressionDiagnostic()
		}
		return unionTruthinessCall(typ, rendered), nil
	case compilerTypes.TruthinessAlwaysTrue:
		// A non-Bool, non-Nil, non-union value is always truthy. The (void)
		// cast marks the discarded operand intentional so the generated C is
		// warning-free under -Wunused-value.
		return "((void)(" + rendered + "), true)", nil
	default:
		return "", unknownExpressionDiagnostic()
	}
}

func writeDeclarationStatement(statement checker.Declaration, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	if !supportedGeneratedTypeWithState(statement.Type, state) {
		return unknownExpressionDiagnostic()
	}
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	var name string
	if statement.Captured {
		name = "env." + privateCName(valueName, statement.Name, "")
		if captureErr := state.registerCapture(checker.Capture{Name: statement.Name, Binding: statement.Binding, Type: statement.Type, Mutable: statement.Mutable}, name); captureErr != nil {
			return captureErr
		}
	} else {
		allocated, nameErr := state.allocateBinding(statement.Binding, statement.Name, statement.Type, statement.Mutable)
		if nameErr != nil {
			return nameErr
		}
		name = allocated
	}
	declared := declaration(statement.Type, name, statement.Mutable)
	if statement.Captured {
		declared = name
	}
	if statement.Source.Node.Kind == checker.MatchExpression {
		resultName, matchErr := renderMatchStatement(body, statement.Source.Node, state, indent)
		if matchErr != nil {
			return matchErr
		}
		if err := renderInto(body, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: declared, Value: resultName}); err != nil {
			return err
		}
		return nil
	}
	value, literalErr := renderOperandWithState(statement.Source, state)
	if literalErr != nil {
		return literalErr
	}
	if err := renderInto(body, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: declared, Value: value}); err != nil {
		return err
	}
	return nil
}

func writeAssignmentStatement(statement checker.Assignment, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	if !supportedGeneratedTypeWithState(statement.Type, state) || !supportedGeneratedTypeWithState(statement.Target.Type, state) {
		return unknownExpressionDiagnostic()
	}
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	target, expressionErr := renderOperandWithState(statement.Target, state)
	if expressionErr != nil {
		return expressionErr
	}
	if target == "" {
		return unknownExpressionDiagnostic()
	}
	if statement.Source.Node.Kind == checker.MatchExpression {
		resultName, matchErr := renderMatchStatement(body, statement.Source.Node, state, indent)
		if matchErr != nil {
			return matchErr
		}
		if err := renderInto(body, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: target, Value: resultName}); err != nil {
			return err
		}
		return nil
	}
	value, literalErr := renderOperandWithState(statement.Source, state)
	if literalErr != nil {
		return literalErr
	}
	if err := renderInto(body, "module.c", "match_assign", matchAssignModel{Indent: indent, Target: target, Value: value}); err != nil {
		return err
	}
	return nil
}

func writeCallStatement(statement checker.CallStatement, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	if statement.Call.Node.Kind == checker.PrintExpression {
		// print is a statement-level builtin producing no value; it
		// renders its own temporaries and helper calls.
		if err := renderPrintStatement(body, statement.Call.Node, state, indent); err != nil {
			return err
		}
		return nil
	}
	call, callErr := renderCallStatement(statement, state)
	if callErr != nil {
		return callErr
	}
	if err := renderInto(body, "module.c", "call_stmt", callStmtModel{Indent: indent, Call: call}); err != nil {
		return err
	}
	return nil
}

func writeReturnStatement(statement checker.ReturnStatement, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	if !frame.inFunction {
		return unknownExpressionDiagnostic()
	}
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	text, returnErr := renderReturnStatement(statement, frame.result, state, indent)
	if returnErr != nil {
		return returnErr
	}
	if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: text}); err != nil {
		return err
	}
	return nil
}

func writeRootReturnStatement(statement checker.RootReturnStatement, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	// Only the entry module's root scope produces one; a root return
	// reached inside a function body is a checker-to-generator
	// contract break, never a silent function return.
	if frame.inFunction {
		return unknownExpressionDiagnostic()
	}
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	text, returnErr := renderRootReturnStatement(statement, state, indent)
	if returnErr != nil {
		return returnErr
	}
	if err := renderInto(body, "module.c", "raw_text", rawTextModel{Text: text}); err != nil {
		return err
	}
	return nil
}

func writeIfStatement(statement checker.IfStatement, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	condition, conditionErr := renderTruthiness(&statement.Condition, state)
	if conditionErr != nil {
		return conditionErr
	}
	if err := writeControlHeader(body, indent, "if", condition, state.line(statement.Span), state.line(statement.ConditionSpan), state.filename); err != nil {
		return err
	}
	state.pushScope()
	if err := writeStatementsAt(body, statement.Then, state, statementFrame{result: frame.result, inFunction: frame.inFunction, defers: statement.ThenDefers}, indent+"    "); err != nil {
		return err
	}
	state.popScope()
	for branchIndex, branch := range statement.ElseIf {
		condition, branchErr := renderTruthiness(&branch.Condition, state)
		if branchErr != nil {
			return branchErr
		}
		if err := writeControlHeader(body, indent, "} else if", condition, state.line(branch.Span), state.line(branch.ConditionSpan), state.filename); err != nil {
			return err
		}
		state.pushScope()
		if err := writeStatementsAt(body, branch.Body, state, statementFrame{result: frame.result, inFunction: frame.inFunction, defers: branchDefers(statement, branchIndex)}, indent+"    "); err != nil {
			return err
		}
		state.popScope()
	}
	if statement.Else != nil {
		if err := writeLineDirective(body, state.line(statement.ElseSpan), state.filename); err != nil {
			return err
		}
		if err := renderInto(body, "module.c", "else_open", indentModel{Indent: indent}); err != nil {
			return err
		}
		state.pushScope()
		if err := writeStatementsAt(body, statement.Else, state, statementFrame{result: frame.result, inFunction: frame.inFunction, defers: statement.ElseDefers}, indent+"    "); err != nil {
			return err
		}
		state.popScope()
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}

func writeWhileStatement(statement checker.WhileStatement, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	condition, conditionErr := renderTruthiness(&statement.Condition, state)
	if conditionErr != nil {
		return conditionErr
	}
	if err := writeControlHeader(body, indent, "while", condition, state.line(statement.Span), state.line(statement.ConditionSpan), state.filename); err != nil {
		return err
	}
	state.pushScope()
	previousLoopDepth := state.loopDepth
	state.loopDepth++
	state.loopDepths = append(state.loopDepths, len(state.deferStack))
	err := writeStatementsAt(body, statement.Body, state, statementFrame{result: frame.result, inFunction: frame.inFunction, defers: statement.BodyDefers}, indent+"    ")
	state.loopDepths = state.loopDepths[:len(state.loopDepths)-1]
	state.loopDepth = previousLoopDepth
	state.popScope()
	if err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "block_close", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}

func writeBreakStatement(statement checker.BreakStatement, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	if state.loopDepth == 0 {
		return unknownExpressionDiagnostic()
	}
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	if err := unwindToLoopDepth(body, state, indent, "false"); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "break_stmt", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}

func writeContinueStatement(statement checker.ContinueStatement, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	if state.loopDepth == 0 {
		return unknownExpressionDiagnostic()
	}
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	if err := unwindToLoopDepth(body, state, indent, "false"); err != nil {
		return err
	}
	if err := renderInto(body, "module.c", "continue_stmt", indentModel{Indent: indent}); err != nil {
		return err
	}
	return nil
}

func writeErrdeferStatement(statement checker.ErrdeferStatement, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	// errdefer registers exactly like defer; the Err flag decides at
	// the exit edge whether the action runs.
	if err := writeLineDirective(body, state.line(statement.Span), state.filename); err != nil {
		return err
	}
	if err := writeDeferStatement(body, checker.DeferStatement{Expression: statement.Expression, Action: statement.Action, Span: statement.Span}, state, indent); err != nil {
		return err
	}
	return nil
}

func writeFunctionDeclarationStatement(statement checker.FunctionDeclaration, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	// Already emitted at file scope; a nested one is not representable.
	if frame.inFunction {
		return unknownExpressionDiagnostic()
	}
	if len(state.activeScopes) > 1 {
		return unknownExpressionDiagnostic()
	}
	return nil
}

func writeMethodDeclarationStatement(statement checker.MethodDeclaration, body *strings.Builder, state *expressionValidation, frame statementFrame, indent string) error {
	// Already emitted at file scope; a nested one is not representable.
	if frame.inFunction {
		return unknownExpressionDiagnostic()
	}
	if len(state.activeScopes) > 1 {
		return unknownExpressionDiagnostic()
	}
	return nil
}
