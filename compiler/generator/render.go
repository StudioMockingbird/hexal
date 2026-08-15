// render.go owns shared statement and expression rendering (RFC 0059):
// rendering dispatch, writeStatements and writeStatementsAt, shared C
// declaration spelling, and the shared rendering state (expressionValidation,
// generatedBinding, and their scope/binding/name-resolution methods).
package generator

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"strconv"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// writeStatements renders one statement list at a single indentation level.
// main's module statements and a function body share it; result is the
// enclosing function's declared result, and inFunction gates return. defers
// are the checked scope's registered deferred actions, emitted in reverse
// order when the list completes.
func writeStatements(body *strings.Builder, statements []checker.Statement, state *expressionValidation, result *compilerTypes.Type, inFunction bool, defers []checker.DeferredAction) error {
	return writeStatementsAt(body, statements, state, result, inFunction, "    ", defers)
}

func writeControlHeader(body *strings.Builder, indent, prefix, condition string, keywordLine, conditionLine int, filename string) {
	writeLineDirective(body, keywordLine, filename)
	if conditionLine > 0 && conditionLine != keywordLine {
		fmt.Fprintf(body, "%s%s (\n", indent, prefix)
		writeLineDirective(body, conditionLine, filename)
		fmt.Fprintf(body, "%s    %s) {\n", indent, condition)
		return
	}
	fmt.Fprintf(body, "%s%s (%s) {\n", indent, prefix, condition)
}

func writeStatementsAt(body *strings.Builder, statements []checker.Statement, state *expressionValidation, result *compilerTypes.Type, inFunction bool, indent string, defers []checker.DeferredAction) error {
	if len(state.activeScopes) == 0 {
		state.pushScope()
		defer state.popScope()
	}
	state.deferStack = append(state.deferStack, defers)
	defer func() { state.deferStack = state.deferStack[:len(state.deferStack)-1] }()
	for _, statement := range statements {
		// RFC 0037: spawn prologues emit before the try prologues so a try
		// operand that spawns can name the already-created task handle.
		if err := hoistConcurrencyInStatement(statement, body, state, indent); err != nil {
			return err
		}
		// RFC 0029: try prologues for this statement emit before it renders,
		// in evaluation order, so nested and repeated operands evaluate once.
		if err := hoistTryInStatement(statement, body, state, result, indent); err != nil {
			return err
		}
		switch statement := statement.(type) {
		case checker.Declaration:
			if !supportedGeneratedTypeWithState(statement.Type, state) {
				return unknownExpressionDiagnostic("unsupported checked declaration type")
			}
			writeLineDirective(body, statement.SourceLine, state.filename)
			name, nameErr := state.allocateBinding(statement.Binding, statement.Name, statement.Type, statement.Mutable)
			if nameErr != nil {
				return nameErr
			}
			if statement.Source.Node.Kind == checker.MatchExpression {
				resultName, matchErr := renderMatchStatement(body, statement.Source.Node, state, indent)
				if matchErr != nil {
					return matchErr
				}
				fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(statement.Type, name, statement.Mutable), resultName)
				break
			}
			value, literalErr := renderOperandWithState(statement.Source, state)
			if literalErr != nil {
				return literalErr
			}
			fmt.Fprintf(body, "%s%s = %s;\n", indent, declaration(statement.Type, name, statement.Mutable), value)
		case checker.Assignment:
			if !supportedGeneratedTypeWithState(statement.Type, state) || !supportedGeneratedTypeWithState(statement.Target.Type, state) {
				return unknownExpressionDiagnostic("unsupported checked assignment type")
			}
			writeLineDirective(body, statement.SourceLine, state.filename)
			target, expressionErr := renderOperandWithState(statement.Target, state)
			if expressionErr != nil {
				return expressionErr
			}
			if target == "" {
				return unknownExpressionDiagnostic("empty checked assignment target")
			}
			if statement.Source.Node.Kind == checker.MatchExpression {
				resultName, matchErr := renderMatchStatement(body, statement.Source.Node, state, indent)
				if matchErr != nil {
					return matchErr
				}
				fmt.Fprintf(body, "%s%s = %s;\n", indent, target, resultName)
				break
			}
			value, literalErr := renderOperandWithState(statement.Source, state)
			if literalErr != nil {
				return literalErr
			}
			fmt.Fprintf(body, "%s%s = %s;\n", indent, target, value)
		case checker.CallStatement:
			writeLineDirective(body, statement.SourceLine, state.filename)
			if statement.Call.Node.Kind == checker.PrintExpression {
				// RFC 0030: print is a statement-level builtin producing no
				// value; it renders its own temporaries and helper calls.
				if err := renderPrintStatement(body, statement.Call.Node, state, indent); err != nil {
					return err
				}
				break
			}
			call, callErr := renderCallStatement(statement, state)
			if callErr != nil {
				return callErr
			}
			fmt.Fprintf(body, "%s%s;\n", indent, call)
		case checker.TryStatement:
			// RFC 0049 item 8.3: the prologue already hoisted above; the
			// success value is discarded, so the statement renders nothing.
			writeLineDirective(body, statement.SourceLine, state.filename)
		case checker.ReturnStatement:
			if !inFunction {
				return unknownExpressionDiagnostic("return outside a function body")
			}
			writeLineDirective(body, statement.SourceLine, state.filename)
			text, returnErr := renderReturnStatement(statement, result, state, indent)
			if returnErr != nil {
				return returnErr
			}
			body.WriteString(text)
		case checker.IfStatement:
			condition, conditionErr := renderTruthiness(&statement.Condition, state)
			if conditionErr != nil {
				return conditionErr
			}
			writeControlHeader(body, indent, "if", condition, statement.SourceLine, statement.ConditionLine, state.filename)
			state.pushScope()
			if err := writeStatementsAt(body, statement.Then, state, result, inFunction, indent+"    ", statement.ThenDefers); err != nil {
				return err
			}
			state.popScope()
			for branchIndex, branch := range statement.ElseIf {
				condition, branchErr := renderTruthiness(&branch.Condition, state)
				if branchErr != nil {
					return branchErr
				}
				writeControlHeader(body, indent, "} else if", condition, branch.SourceLine, branch.ConditionLine, state.filename)
				state.pushScope()
				if err := writeStatementsAt(body, branch.Body, state, result, inFunction, indent+"    ", branchDefers(statement, branchIndex)); err != nil {
					return err
				}
				state.popScope()
			}
			if statement.Else != nil {
				writeLineDirective(body, statement.ElseLine, state.filename)
				fmt.Fprintf(body, "%s} else {\n", indent)
				state.pushScope()
				if err := writeStatementsAt(body, statement.Else, state, result, inFunction, indent+"    ", statement.ElseDefers); err != nil {
					return err
				}
				state.popScope()
			}
			fmt.Fprintf(body, "%s}\n", indent)
		case checker.WhileStatement:
			condition, conditionErr := renderTruthiness(&statement.Condition, state)
			if conditionErr != nil {
				return conditionErr
			}
			writeControlHeader(body, indent, "while", condition, statement.SourceLine, statement.ConditionLine, state.filename)
			state.pushScope()
			previousLoopDepth := state.loopDepth
			state.loopDepth++
			state.loopDepths = append(state.loopDepths, len(state.deferStack))
			err := writeStatementsAt(body, statement.Body, state, result, inFunction, indent+"    ", statement.BodyDefers)
			state.loopDepths = state.loopDepths[:len(state.loopDepths)-1]
			state.loopDepth = previousLoopDepth
			state.popScope()
			if err != nil {
				return err
			}
			fmt.Fprintf(body, "%s}\n", indent)
		case checker.ForStatement:
			if err := renderForStatement(body, statement, state, result, inFunction, indent); err != nil {
				return err
			}
		case checker.BreakStatement:
			if state.loopDepth == 0 {
				return unknownExpressionDiagnostic("checked break outside a while loop")
			}
			writeLineDirective(body, statement.SourceLine, state.filename)
			if err := unwindToLoopDepth(body, state, indent, "false"); err != nil {
				return err
			}
			fmt.Fprintf(body, "%sbreak;\n", indent)
		case checker.ContinueStatement:
			if state.loopDepth == 0 {
				return unknownExpressionDiagnostic("checked continue outside a while loop")
			}
			writeLineDirective(body, statement.SourceLine, state.filename)
			if err := unwindToLoopDepth(body, state, indent, "false"); err != nil {
				return err
			}
			fmt.Fprintf(body, "%scontinue;\n", indent)
		case checker.DeferStatement:
			writeLineDirective(body, statement.SourceLine, state.filename)
			if err := writeDeferStatement(body, statement, state, indent); err != nil {
				return err
			}
		case checker.ErrdeferStatement:
			// RFC 0029: errdefer registers exactly like defer; the Err flag
			// decides at the exit edge whether the action runs.
			writeLineDirective(body, statement.SourceLine, state.filename)
			if err := writeDeferStatement(body, checker.DeferStatement{Expression: statement.Expression, Action: statement.Action, SourceLine: statement.SourceLine, SourceColumn: statement.SourceColumn}, state, indent); err != nil {
				return err
			}
		case checker.FunctionDeclaration:
			// Already emitted at file scope; a nested one is not representable.
			if inFunction {
				return unknownExpressionDiagnostic("function declaration inside a function body")
			}
			if len(state.activeScopes) > 1 {
				return unknownExpressionDiagnostic("function declaration inside a module-level control-flow block")
			}
		case checker.MethodDeclaration:
			// Already emitted at file scope; a nested one is not representable.
			if inFunction {
				return unknownExpressionDiagnostic("method declaration inside a function body")
			}
			if len(state.activeScopes) > 1 {
				return unknownExpressionDiagnostic("method declaration inside a module-level control-flow block")
			}
		default:
			return unknownExpressionDiagnostic("unsupported checked statement")
		}
	}
	if len(defers) > 0 {
		if err := writeDeferredActions(body, defers, state, indent, "false"); err != nil {
			return err
		}
	}
	return nil
}

func renderCallStatement(statement checker.CallStatement, state *expressionValidation) (string, error) {
	switch statement.Call.Node.Kind {
	case checker.CallExpression, checker.MethodCallExpression, checker.StringMethodCallExpression, checker.CollectionMethodCallExpression, checker.ListNewExpression, checker.DictNewExpression,
		checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression,
		checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression,
		checker.MutexConstructorExpression, checker.MutexMethodCallExpression,
		checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression,
		checker.VolatileWriteExpression,
		checker.RuneCursorMethodCallExpression, checker.HeapFreeExpression:
		// RFC 0035: discarding a constructor result is legal; it simply
		// leaks the allocation, which is the programmer's choice.
	default:
		return "", unknownExpressionDiagnostic("call statement without a checked call")
	}
	if !compilerTypes.Equal(statement.Call.Type, statement.Call.Node.ResultType) {
		return "", unknownExpressionDiagnostic("call statement type does not match its checked call")
	}
	return renderExpressionExpectedWithState(statement.Call.Node, compilerTypes.Type{}, false, state)
}

func renderReturnStatement(statement checker.ReturnStatement, result *compilerTypes.Type, state *expressionValidation, indent string) (string, error) {
	if statement.Value == nil {
		if result != nil {
			return "", unknownExpressionDiagnostic("bare return in a function that declares a result")
		}
		return indent + "return;\n", nil
	}
	if result == nil {
		return "", unknownExpressionDiagnostic("return value in a function that declares no result")
	}
	if !generatedAssignable(*result, statement.Value.Type) {
		return "", unknownExpressionDiagnostic("return value type does not match its checked function result")
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
		builder.WriteString(indent + "return " + resultName + ";\n")
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
	// defers (RFC 0029).
	if hasPendingActions(state) {
		state.returnCounter++
		name := fmt.Sprintf("hex_return_%d", state.returnCounter)
		var builder strings.Builder
		fmt.Fprintf(&builder, "%s%s = %s;\n", indent, declaration(*result, name, false), value)
		if hasPendingErrDefers(state) {
			state.returnCounter++
			errorName := fmt.Sprintf("hex_err_%d", state.returnCounter)
			fmt.Fprintf(&builder, "%sconst bool %s = %s;\n", indent, errorName, returnErrorExit(statement.Value.Type, name))
			if err := unwindAllDefers(&builder, state, indent, errorName); err != nil {
				return "", err
			}
		} else if err := unwindAllDefers(&builder, state, indent, "false"); err != nil {
			return "", err
		}
		builder.WriteString(indent + "return " + name + ";\n")
		return builder.String(), nil
	}
	return indent + "return " + value + ";\n", nil
}

// hasPendingDefers reports whether any enclosing scope has registered a
// deferred action that must run before an exit edge.
func hasPendingDefers(state *expressionValidation) bool {
	for _, scope := range state.deferStack {
		if len(scope) > 0 {
			return true
		}
	}
	return false
}

// renderTruthiness renders a checked condition (RFC 0023): nil is false,
// Bool and nullable values render as themselves, and every other value is
// evaluated once and then yields true.
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

// renderTruthinessChild renders a logical operand per RFC 0023. The nil
// literal renders as false without touching the (RFC 0010 fail-closed) nil
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
	rendered, err := renderExpressionExpectedWithState(*child, compilerTypes.Type{}, false, state)
	if err != nil {
		return "", err
	}
	return truthinessExpression(childType, rendered, state)
}

// truthinessExpression wraps a rendered value so its truthiness is the C
// result (RFC 0023): Bool stays as-is, nil becomes false, a nullable becomes
// a null test, and every other value is evaluated once and then yields true.
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
			return "", unknownExpressionDiagnostic("nullable operand in a truthiness context is not pointer-like")
		}
		return "(" + rendered + " != NULL)", nil
	case compilerTypes.TruthinessUnion:
		if typ.Union == nil {
			return "", unknownExpressionDiagnostic("tagged union truthiness has no union metadata")
		}
		return unionTruthinessCall(typ, rendered), nil
	case compilerTypes.TruthinessAlwaysTrue:
		// RFC 0023: a non-Bool, non-Nil, non-union value is always truthy.
		// The (void) cast marks the discarded operand intentional so the
		// generated C is warning-free under -Wunused-value.
		return "((void)(" + rendered + "), true)", nil
	default:
		return "", unknownExpressionDiagnostic("unsupported operand in a truthiness context")
	}
}

type expressionValidation struct {
	expressions    map[*checker.Expression]bool
	objects        map[*checker.ObjectValue]bool
	variables      map[string]generatedBinding
	bindings       map[checker.BindingID]generatedBinding
	bindingNames   map[checker.BindingID]string
	activeScopes   []map[checker.BindingID]bool
	loopDepth      int
	usedNames      map[string]bool
	functions      map[string]compilerTypes.Type
	methods        map[string]checker.MethodDeclaration
	generatedTypes *generatedTypeValidation
	deferStack     [][]checker.DeferredAction
	// owner is the RFC 0034 encoded module owner of the module being
	// generated; filename is its logical source key for #line directives;
	// moduleID is the module's canonical identity, used to distinguish
	// foreign method calls from local ones.
	owner          string
	filename       string
	moduleID       string
	loopDepths     []int
	captureCounter int
	returnCounter  int
	captures       map[*checker.Operand][]string
	matchCounter   int
	loopCounter    int // unique hex_for_N stem for for-in lowering (RFC 0028)
	tryCounter     int // unique hex_try_N stems for RFC 0029 hoisting
	hoistedTries   map[*checker.Expression]string
	// spawnCounter and hoistedSpawns carry RFC 0037 spawn prologues: each
	// spawn's argument frame and task handle are declared before the
	// statement renders, and the expression renders as the task handle.
	spawnCounter  int
	hoistedSpawns map[*checker.Expression]string
	// registeredDefers records the deferred actions whose statements were
	// processed, in registration order. A try error branch may render
	// earlier than a later defer statement, and must not run it (RFC 0029).
	registeredDefers []checker.DeferredAction
	printCounter     int                   // unique hex_print_arg_N stems for RFC 0030
	strings          *generatedStringState // literal index lookup for rendering
}

type generatedBinding struct {
	typ     compilerTypes.Type
	mutable bool
}

func (state *expressionValidation) pushScope() {
	state.activeScopes = append(state.activeScopes, make(map[checker.BindingID]bool))
}

func (state *expressionValidation) popScope() {
	if len(state.activeScopes) > 0 {
		state.activeScopes = state.activeScopes[:len(state.activeScopes)-1]
	}
}

func (state *expressionValidation) bindingActive(id checker.BindingID) bool {
	for index := len(state.activeScopes) - 1; index >= 0; index-- {
		if state.activeScopes[index][id] {
			return true
		}
	}
	return false
}

func (state *expressionValidation) allocateBinding(id checker.BindingID, sourceName string, typ compilerTypes.Type, mutable bool) (string, error) {
	if id == 0 {
		if state.variables == nil {
			state.variables = make(map[string]generatedBinding)
		}
		state.variables[sourceName] = generatedBinding{typ: typ, mutable: mutable}
		return PrivateCName(ValueName, sourceName, ""), nil
	}
	if state.bindings == nil {
		state.bindings = make(map[checker.BindingID]generatedBinding)
		state.bindingNames = make(map[checker.BindingID]string)
	}
	if state.usedNames == nil {
		state.usedNames = make(map[string]bool)
	}
	if _, exists := state.bindings[id]; exists {
		return "", unknownExpressionDiagnostic("duplicate checked binding identity")
	}
	base := PrivateCName(ValueName, sourceName, "")
	name := base
	for suffix := 2; state.usedNames[name]; suffix++ {
		name = fmt.Sprintf("%s_%d", base, suffix)
	}
	state.usedNames[name] = true
	state.bindings[id] = generatedBinding{typ: typ, mutable: mutable}
	state.bindingNames[id] = name
	if len(state.activeScopes) == 0 {
		state.pushScope()
	}
	state.activeScopes[len(state.activeScopes)-1][id] = true
	return name, nil
}

func (state *expressionValidation) bindingFor(node checker.Expression) (generatedBinding, bool) {
	if node.Binding != 0 {
		if !state.bindingActive(node.Binding) {
			return generatedBinding{}, false
		}
		binding, ok := state.bindings[node.Binding]
		return binding, ok
	}
	binding, ok := state.variables[node.Name]
	return binding, ok
}

func (state *expressionValidation) cNameFor(node checker.Expression) (string, bool) {
	if node.Binding != 0 {
		name, ok := state.bindingNames[node.Binding]
		if !ok || !state.bindingActive(node.Binding) {
			return "", false
		}
		return name, true
	}
	return PrivateCName(ValueName, node.Name, ""), true
}

// pointerSpelling renders a pointer type's C declarator base from the type
// chain alone, outermost layer contributing pointee `const` exactly when it is
// a read-only Ptr. It never includes the binding's own trailing `const`.
func pointerSpelling(typ compilerTypes.Type) string {
	layers := make([]compilerTypes.Type, 0)
	base := typ
	for base.Element != nil {
		layers = append(layers, base)
		base = *base.Element
	}
	result := base.CName
	for index := len(layers) - 1; index >= 0; index-- {
		if !layers[index].PointeeWritable {
			if index == len(layers)-1 {
				result = "const " + result
			} else {
				result = qualifyLastPointer(result)
			}
		}
		if strings.HasSuffix(result, "*") {
			result += "*"
		} else {
			result += " *"
		}
	}
	return result
}

// declaration builds the complete C declarator for typ bound to name. Every type but Fun<...> is spelled inside the declarator, which is why a CName prefix cannot express it.
func declaration(typ compilerTypes.Type, name string, mutable bool) string {
	if typ.Signature != nil {
		return funDeclaration(typ, name, mutable)
	}
	if compilerTypes.IsString(typ) {
		// A source String value is a pointer-sized handle to an immutable
		// hex_string object; the binding's own const follows the pointer.
		if mutable {
			return "const hex_string *" + name
		}
		return "const hex_string *const " + name
	}
	if compilerTypes.IsList(typ) {
		// A source List value is a pointer-sized owning handle to a mutable
		// heap header; mutation flows through it without a mut binding.
		if mutable {
			return typ.CName + " *" + name
		}
		return typ.CName + " *const " + name
	}
	if compilerTypes.IsDict(typ) {
		if mutable {
			return typ.CName + " *" + name
		}
		return typ.CName + " *const " + name
	}
	if compilerTypes.IsRuneCursor(typ) {
		// RFC 0044: a RuneCursor is a mutable-through descriptor; next()
		// advances its offset, so the binding carries no top-level const
		// even without a mut declaration.
		return typ.CName + " " + name
	}
	if typ.Element == nil {
		prefix := ""
		if !mutable {
			prefix = "const "
		}
		return prefix + typ.CName + " " + name
	}
	result := pointerSpelling(typ)
	if !mutable {
		result = qualifyLastPointer(result)
	}
	separator := ""
	if strings.HasSuffix(result, "const") {
		separator = " "
	}
	return result + separator + name
}

func qualifyLastPointer(typeName string) string { return typeName + "const" }

// funDeclaration renders a C function-pointer declarator, with name empty when
// the type appears in a position that declares nothing (a parameter of another
// function-pointer type). Its own parameters are always spelled unqualified:
// RFC 0008 keeps top-level parameter const on the definition's local binding,
// never on the type, and C ignores it when comparing function types.
func funDeclaration(typ compilerTypes.Type, name string, mutable bool) string {
	result := "void"
	if typ.Signature.Result != nil {
		result = typeSpelling(*typ.Signature.Result)
	}
	inner := "*"
	if !mutable {
		inner += "const"
	}
	if name != "" {
		if inner != "*" {
			inner += " "
		}
		inner += name
	}
	parameters := make([]string, len(typ.Signature.Parameters))
	for index, parameter := range typ.Signature.Parameters {
		parameters[index] = typeSpelling(parameter)
	}
	return result + " (" + inner + ")(" + parameterList(parameters) + ")"
}

// typeSpelling renders typ where no name is declared: a function-pointer
// type's parameter and result positions. It never adds a top-level qualifier.
func typeSpelling(typ compilerTypes.Type) string {
	if typ.Signature != nil {
		return funDeclaration(typ, "", true)
	}
	if compilerTypes.IsString(typ) {
		return "const hex_string *"
	}
	if compilerTypes.IsList(typ) {
		return typ.CName + " *"
	}
	if compilerTypes.IsDict(typ) {
		return typ.CName + " *"
	}
	if typ.Element != nil {
		return pointerSpelling(typ)
	}
	return typ.CName
}

func renderExpression(node checker.Expression) (string, error) {
	return renderExpressionWithState(node, &expressionValidation{})
}

func renderExpressionWithState(node checker.Expression, state *expressionValidation) (string, error) {
	return renderExpressionExpectedWithState(node, compilerTypes.Type{}, false, state)
}

func renderExpressionExpectedWithState(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) (string, error) {
	if err := validateExpressionNode(node, expected, hasExpected, state); err != nil {
		return "", err
	}
	return renderExpressionUncheckedWithState(node, state)
}

func renderExpressionUnchecked(node checker.Expression) (string, error) {
	return renderExpressionUncheckedWithState(node, &expressionValidation{})
}

func renderExpressionUncheckedWithState(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.NilExpression:
		return "nullptr", nil
	case checker.EosExpression:
		return "((hex_eos){ 0 })", nil
	case checker.VariableExpression:
		if node.Name == "" {
			return "", unknownExpressionDiagnostic("variable without a source name")
		}
		name, ok := state.cNameFor(node)
		if !ok {
			return "", unknownExpressionDiagnostic("variable binding is not active")
		}
		return name, nil
	case checker.FunctionReferenceExpression:
		if node.Name == "" {
			return "", unknownExpressionDiagnostic("function reference without a source name")
		}
		return PrivateCName(FunctionName, node.Name, moduleOwner(node.Module, state.owner)), nil
	case checker.CallExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("call without a checked callee")
		}
		callee, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if err != nil {
			return "", err
		}
		if !atomic {
			callee = "(" + callee + ")"
		}
		arguments := make([]string, len(node.Arguments))
		for index, argument := range node.Arguments {
			rendered, argumentErr := renderOperandWithState(argument, state)
			if argumentErr != nil {
				return "", argumentErr
			}
			arguments[index] = rendered
		}
		return callee + "(" + strings.Join(arguments, ", ") + ")", nil
	case checker.MethodCallExpression:
		if node.Owner == nil || node.Operand == nil {
			return "", unknownExpressionDiagnostic("method call without a checked receiver")
		}
		receiverType, receiverErr := methodReceiverType(*node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		receiver, _, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, receiverType, state, true)
		if receiverErr != nil {
			return "", receiverErr
		}
		arguments := make([]string, len(node.Arguments))
		for index, argument := range node.Arguments {
			rendered, argumentErr := renderOperandWithState(argument, state)
			if argumentErr != nil {
				return "", argumentErr
			}
			arguments[index] = rendered
		}
		allArguments := append([]string{receiver}, arguments...)
		return methodCName(node.Owner, node.Name, moduleOwner(node.Owner.ModuleID, state.owner)) + "(" + strings.Join(allArguments, ", ") + ")", nil
	case checker.AddressOfExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("address-of without an operand")
		}
		operandType, hasOperandType := node.OperandType, node.OperandType != (compilerTypes.Type{})
		if !hasOperandType && node.ResultType.Element != nil {
			operandType, hasOperandType = *node.ResultType.Element, true
		}
		if !hasOperandType {
			operandType, hasOperandType = expressionTypeWithState(*node.Operand, state)
		}
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, operandType, state, hasOperandType)
		if err != nil {
			return "", err
		}
		if atomic {
			return "&" + operand, nil
		}
		return "&(" + operand + ")", nil
	case checker.DereferenceExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("dereference without an operand")
		}
		operandType, ok := expressionTypeWithState(*node.Operand, state)
		if !ok && node.OperandType != (compilerTypes.Type{}) {
			operandType, ok = node.OperandType, true
		}
		if !ok {
			return "", unknownExpressionDiagnostic("dereference receiver type is unavailable")
		}
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, operandType, state, true)
		if err != nil {
			return "", err
		}
		if atomic {
			return "*" + operand, nil
		}
		return "*(" + operand + ")", nil
	case checker.IndexExpression:
		place, placeErr := checkedPlaceMetadata(node, state)
		if placeErr != nil {
			return "", placeErr
		}
		receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		index, indexErr := renderOperandWithState(node.Arguments[0], state)
		if indexErr != nil {
			return "", indexErr
		}
		if node.OperandType.View != nil {
			return "*hex_view_at_" + strings.TrimPrefix(node.OperandType.CName, "hex_view_") + "(" + receiver + ", (size_t)(" + index + "))", nil
		}
		if node.OperandType.List != nil {
			if place.writable {
				return "*hex_list_at_mut_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + index + "))", nil
			}
			return "*hex_list_at_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + index + "))", nil
		}
		if compilerTypes.IsString(node.OperandType) {
			return "hex_string_at_rune(" + receiver + ", (size_t)(" + index + "))", nil
		}
		if compilerTypes.IsStrand(node.OperandType) {
			return "hex_strand_at_rune(" + receiver + ", (size_t)(" + index + "))", nil
		}
		return "*" + arrayAccessorCName(node.OperandType, place.writable) + "(&" + receiver + ", (size_t)(" + index + "))", nil
	case checker.ArrayLiteralExpression:
		if node.ResultType.Array == nil {
			return "", unknownExpressionDiagnostic("array literal without a checked array type")
		}
		elements := make([]string, len(node.Arguments))
		for index, element := range node.Arguments {
			rendered, elementErr := renderOperandWithState(element, state)
			if elementErr != nil {
				return "", elementErr
			}
			elements[index] = rendered
		}
		// The element region is the struct's single data member, so the
		// compound literal carries one extra brace layer.
		return "(" + node.ResultType.CName + "){{" + strings.Join(elements, ", ") + "}}", nil
	case checker.CollectionMethodCallExpression:
		switch node.Name {
		case "length":
			if node.OperandType.Array != nil {
				return fmt.Sprintf("(size_t)(%d)", node.OperandType.Array.Length), nil
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			if node.OperandType.List != nil || node.OperandType.Dict != nil {
				// List and Dict bindings are pointer-sized handles.
				return "(" + receiver + ")->length", nil
			}
			return "(" + receiver + ").length", nil
		case "push", "set", "clear", "pop":
			if node.Operand == nil || node.OperandType.List == nil {
				return "", unknownExpressionDiagnostic("list mutation without a checked list receiver")
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			suffix := listSuffix(node.OperandType)
			switch node.Name {
			case "push":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic("list push without a checked value")
				}
				value, valueErr := renderOperandWithState(node.Arguments[0], state)
				if valueErr != nil {
					return "", valueErr
				}
				return "hex_list_push_" + suffix + "(" + receiver + ", " + value + ")", nil
			case "set":
				if len(node.Arguments) != 2 {
					return "", unknownExpressionDiagnostic("list set without checked operands")
				}
				index, indexErr := renderOperandWithState(node.Arguments[0], state)
				if indexErr != nil {
					return "", indexErr
				}
				value, valueErr := renderOperandWithState(node.Arguments[1], state)
				if valueErr != nil {
					return "", valueErr
				}
				return "hex_list_set_" + suffix + "(" + receiver + ", (size_t)(" + index + "), " + value + ")", nil
			case "clear":
				return "hex_list_clear_" + suffix + "(" + receiver + ")", nil
			case "pop":
				return "hex_list_pop_" + suffix + "(" + receiver + ")", nil
			}
		case "free":
			if node.Operand == nil || len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic("collection free without a checked heap")
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			heap, heapErr := renderOperandWithState(node.Arguments[0], state)
			if heapErr != nil {
				return "", heapErr
			}
			if node.OperandType.List != nil {
				return "hex_list_free_" + listSuffix(node.OperandType) + "(" + heap + ", " + receiver + ")", nil
			}
			if node.OperandType.Dict != nil {
				return "hex_dict_free_" + dictSuffix(node.OperandType) + "(" + heap + ", " + receiver + ")", nil
			}
			return "", unknownExpressionDiagnostic("collection free without a list or dictionary receiver")
		case "insert", "get", "contains", "remove":
			if node.Operand == nil || node.OperandType.Dict == nil {
				return "", unknownExpressionDiagnostic("dictionary operation without a checked dictionary receiver")
			}
			receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
			if receiverErr != nil {
				return "", receiverErr
			}
			suffix := dictSuffix(node.OperandType)
			switch node.Name {
			case "insert":
				if len(node.Arguments) != 2 {
					return "", unknownExpressionDiagnostic("dictionary insert without checked operands")
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				value, valueErr := renderOperandWithState(node.Arguments[1], state)
				if valueErr != nil {
					return "", valueErr
				}
				return "hex_dict_insert_" + suffix + "(" + receiver + ", " + key + ", " + value + ")", nil
			case "get", "remove":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic("dictionary lookup without a checked key")
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				return "hex_dict_" + node.Name + "_" + suffix + "(" + receiver + ", " + key + ")", nil
			case "contains":
				if len(node.Arguments) != 1 {
					return "", unknownExpressionDiagnostic("dictionary contains without a checked key")
				}
				key, keyErr := renderOperandWithState(node.Arguments[0], state)
				if keyErr != nil {
					return "", keyErr
				}
				return "hex_dict_contains_" + suffix + "(" + receiver + ", " + key + ")", nil
			}
		}
		return "", unknownExpressionDiagnostic("unknown collection method")
	case checker.CollectionSliceExpression:
		if node.Operand == nil || len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic("collection slice without checked bounds")
		}
		receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		start, startErr := renderOperandWithState(node.Arguments[0], state)
		if startErr != nil {
			return "", startErr
		}
		end, endErr := renderOperandWithState(node.Arguments[1], state)
		if endErr != nil {
			return "", endErr
		}
		if node.OperandType.View != nil {
			return "hex_view_slice_" + strings.TrimPrefix(node.OperandType.CName, "hex_view_") + "(" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
		}
		if node.OperandType.List != nil {
			return "hex_list_slice_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
		}
		return "hex_array_slice_" + arrayAccessorSuffix(node.OperandType) + "(&" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
	case checker.StringLiteralExpression:
		if state.strings == nil {
			return "", unknownExpressionDiagnostic("string literal without the string literal registry")
		}
		index, ok := state.strings.seen[node.Name]
		if !ok {
			return "", unknownExpressionDiagnostic("string literal is missing from the checked literal registry: " + node.Name)
		}
		if compilerTypes.IsStrand(node.ResultType) {
			// RFC 0044: a Strand is a 32-byte zero-padded inline value.
			payload := node.Name
			var builder strings.Builder
			builder.WriteString("(hex_strand){{")
			for _, character := range []byte(payload) {
				fmt.Fprintf(&builder, " %d,", character)
			}
			builder.WriteString(" 0 }}")
			return builder.String(), nil
		}
		return "&" + stringLiteralCName(index-1), nil
	case checker.StringMethodCallExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("string method without a checked receiver")
		}
		receiver, receiverErr := renderReceiver(node.Operand, node.OperandType, state)
		if receiverErr != nil {
			return "", receiverErr
		}
		switch node.Name {
		case "length":
			if compilerTypes.IsStrand(node.OperandType) {
				return "hex_strand_rune_length(" + receiver + ")", nil
			}
			return "hex_string_rune_length(" + receiver + ")", nil
		case "is_empty":
			if compilerTypes.IsStrand(node.OperandType) {
				return "hex_strand_is_empty(" + receiver + ")", nil
			}
			return "hex_string_is_empty(" + receiver + ")", nil
		case "rune_cursor":
			return "hex_string_rune_cursor(" + receiver + ")", nil
		case "bytes":
			return "hex_string_bytes(" + receiver + ")", nil
		case "slice":
			if len(node.Arguments) != 2 {
				return "", unknownExpressionDiagnostic("string slice without checked bounds")
			}
			start, startErr := renderOperandWithState(node.Arguments[0], state)
			if startErr != nil {
				return "", startErr
			}
			end, endErr := renderOperandWithState(node.Arguments[1], state)
			if endErr != nil {
				return "", endErr
			}
			return "hex_string_slice(" + receiver + ", (size_t)(" + start + "), (size_t)(" + end + "))", nil
		case "to_string":
			if len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic("string to_string without a checked heap")
			}
			heap, heapErr := renderOperandWithState(node.Arguments[0], state)
			if heapErr != nil {
				return "", heapErr
			}
			if compilerTypes.IsStrand(node.OperandType) {
				return "hex_strand_to_string(" + heap + ", " + receiver + ")", nil
			}
			return "hex_string_to_string(" + heap + ", " + receiver + ")", nil
		case "concat":
			if len(node.Arguments) != 2 {
				return "", unknownExpressionDiagnostic("string concat without checked operands")
			}
			heap, heapErr := renderOperandWithState(node.Arguments[0], state)
			if heapErr != nil {
				return "", heapErr
			}
			other, otherErr := renderOperandWithState(node.Arguments[1], state)
			if otherErr != nil {
				return "", otherErr
			}
			return "hex_string_concat(" + heap + ", " + receiver + ", " + other + ")", nil
		case "free":
			if len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic("string free without a checked heap")
			}
			heap, heapErr := renderOperandWithState(node.Arguments[0], state)
			if heapErr != nil {
				return "", heapErr
			}
			return "hex_string_free(" + heap + ", " + receiver + ")", nil
		}
		return "", unknownExpressionDiagnostic("unknown string method")
	case checker.StringFromBytesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("String.from_bytes without checked operands")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, compilerTypes.Heap, state, true)
		if heapErr != nil {
			return "", heapErr
		}
		view, viewErr := renderOperandWithState(node.Arguments[0], state)
		if viewErr != nil {
			return "", viewErr
		}
		return "hex_string_from_bytes(" + heap + ", (" + view + ").data, (" + view + ").length)", nil
	case checker.StringFromRunesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("String.from_runes without checked operands")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, compilerTypes.Heap, state, true)
		if heapErr != nil {
			return "", heapErr
		}
		view, viewErr := renderOperandWithState(node.Arguments[0], state)
		if viewErr != nil {
			return "", viewErr
		}
		return "hex_string_from_runes(" + heap + ", (" + view + ").data, (" + view + ").length)", nil
	case checker.RuneCursorMethodCallExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("rune cursor method without a checked receiver")
		}
		receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if err != nil {
			return "", err
		}
		switch node.Name {
		case "has_next":
			return "hex_rune_cursor_has_next(" + receiver + ")", nil
		case "next":
			return "hex_rune_cursor_next(&(" + receiver + "))", nil
		}
		return "", unknownExpressionDiagnostic("unknown rune cursor method " + node.Name)
	case checker.ListNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("List<T>.new without a checked heap")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, compilerTypes.Heap, state, true)
		if heapErr != nil {
			return "", heapErr
		}
		return "hex_list_new_" + listSuffix(node.ResultType) + "(" + heap + ")", nil
	case checker.DictNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("Dict<K, V>.new without a checked heap")
		}
		heap, _, heapErr := renderExpressionNodeWithExpectedState(*node.Operand, compilerTypes.Heap, state, true)
		if heapErr != nil {
			return "", heapErr
		}
		return "hex_dict_new_" + dictSuffix(node.ResultType) + "(" + heap + ")", nil
	case checker.WideningExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("widening without an operand")
		}
		operand, atomic, operandErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if operandErr != nil {
			return "", operandErr
		}
		if !atomic {
			operand = "(" + operand + ")"
		}
		return "(" + node.ResultType.CName + ")(" + operand + ")", nil
	case checker.DeepEqualityExpression:
		if node.Left == nil || node.Right == nil {
			return "", unknownExpressionDiagnostic("deep equality without both operands")
		}
		left, _, leftErr := renderExpressionNodeWithExpectedState(*node.Left, node.OperandType, state, true)
		if leftErr != nil {
			return "", leftErr
		}
		right, _, rightErr := renderExpressionNodeWithExpectedState(*node.Right, node.OperandType, state, true)
		if rightErr != nil {
			return "", rightErr
		}
		if !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) && !compilerTypes.IsList(node.OperandType) {
			left = "&(" + left + ")"
			right = "&(" + right + ")"
		}
		result := equalityHelperName(node.OperandType) + "(" + left + ", " + right + ")"
		if node.Operator == checker.NotEqualOperator {
			result = "(!" + result + ")"
		}
		return result, nil
	case checker.StringCompareExpression:
		if node.Left == nil || node.Right == nil {
			return "", unknownExpressionDiagnostic("text ordering without both operands")
		}
		left, _, leftErr := renderExpressionNodeWithExpectedState(*node.Left, node.OperandType, state, true)
		if leftErr != nil {
			return "", leftErr
		}
		right, _, rightErr := renderExpressionNodeWithExpectedState(*node.Right, node.OperandType, state, true)
		if rightErr != nil {
			return "", rightErr
		}
		helper := "hex_compare_hex_string"
		if compilerTypes.IsStrand(node.OperandType) {
			helper = "hex_compare_hex_strand"
		}
		comparison := " < 0"
		switch node.Operator {
		case checker.LessEqualOperator:
			comparison = " <= 0"
		case checker.GreaterOperator:
			comparison = " > 0"
		case checker.GreaterEqualOperator:
			comparison = " >= 0"
		}
		return "(" + helper + "(" + left + ", " + right + ")" + comparison + ")", nil
	case checker.ConversionExpression:
		return renderConversion(node, state)
	case checker.BitCastExpression:
		return renderBitCast(node, state)
	case checker.EndianConversionExpression:
		return renderEndianConversion(node, state)
	case checker.TryExpression:
		return renderTryExpression(node, state)
	case checker.SpawnExpression:
		return renderSpawnExpression(node, state)
	case checker.TaskYieldExpression:
		return "hex_task_yield()", nil
	case checker.TaskMethodCallExpression:
		return renderTaskMethod(node, state)
	case checker.ChannelConstructorExpression:
		return renderChannelConstructor(node, state)
	case checker.ChannelMethodCallExpression:
		return renderChannelMethod(node, state)
	case checker.MutexConstructorExpression:
		return renderMutexConstructor(node, state)
	case checker.MutexMethodCallExpression:
		return renderMutexMethod(node, state)
	case checker.AtomicConstructorExpression:
		return renderAtomicConstructor(node, state)
	case checker.AtomicMethodCallExpression:
		return renderAtomicMethod(node, state)
	case checker.LayoutExpression:
		// RFC 0042: the C23 compiler is the final authority for the selected
		// target layout; the checker already proved T complete.
		if node.Name == "align_of" {
			return "(size_t)alignof(" + typeSpelling(node.OperandType) + ")", nil
		}
		return "(size_t)sizeof(" + typeSpelling(node.OperandType) + ")", nil
	case checker.VolatileReadExpression:
		if node.Operand == nil || node.OperandType.Element == nil {
			return "", unknownExpressionDiagnostic("volatile read without a checked pointer")
		}
		receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if err != nil {
			return "", err
		}
		qualifier := "volatile "
		if !node.OperandType.PointeeWritable {
			qualifier = "volatile const "
		}
		return "*(" + qualifier + typeSpelling(node.Element) + " *)(" + receiver + ")", nil
	case checker.VolatileWriteExpression:
		if node.Operand == nil || node.OperandType.Element == nil || len(node.Arguments) != 1 {
			return "", unknownExpressionDiagnostic("volatile write without checked operands")
		}
		receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if err != nil {
			return "", err
		}
		value, valueErr := renderOperandWithState(node.Arguments[0], state)
		if valueErr != nil {
			return "", valueErr
		}
		return "*(volatile " + typeSpelling(node.Element) + " *)(" + receiver + ") = " + value, nil
	case checker.ViewBridgeExpression:
		// RFC 0043: the descriptor is one pointer-and-count initialization;
		// the pointer expression precedes the length expression in source
		// order and each appears exactly once.
		if node.OperandType.View == nil {
			return "", unknownExpressionDiagnostic("view bridge without a checked View type")
		}
		if node.Name == "empty" {
			if len(node.Arguments) != 0 {
				return "", unknownExpressionDiagnostic("view bridge empty with unexpected arguments")
			}
			return "(" + node.OperandType.CName + "){ NULL, 0 }", nil
		}
		if len(node.Arguments) != 2 {
			return "", unknownExpressionDiagnostic("view bridge without checked pointer and length")
		}
		pointer, pointerErr := renderOperandWithState(node.Arguments[0], state)
		if pointerErr != nil {
			return "", pointerErr
		}
		length, lengthErr := renderOperandWithState(node.Arguments[1], state)
		if lengthErr != nil {
			return "", lengthErr
		}
		return "(" + node.OperandType.CName + "){ " + pointer + ", " + length + " }", nil
	case checker.MemberExpression:
		if node.Operand == nil || node.Member == nil {
			return "", unknownExpressionDiagnostic("member selection without a receiver or member")
		}
		receiverType, ok := expressionTypeWithState(*node.Operand, state)
		if !ok && node.OperandType != (compilerTypes.Type{}) {
			receiverType, ok = node.OperandType, true
		}
		if !ok {
			return "", unknownExpressionDiagnostic("member receiver type is unavailable")
		}
		receiver, err := renderReceiver(node.Operand, receiverType, state)
		if err != nil {
			return "", err
		}
		return receiver + "." + PrivateCName(MemberName, node.Member.Name, ""), nil
	case checker.NullTestExpression:
		// RFC 0010: the nullable union shares its base pointer's null niche,
		// so the test lowers to the ordinary C null pointer comparison.
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("null test without a checked operand")
		}
		operator := "=="
		if node.Operator == checker.NotEqualOperator {
			operator = "!="
		}
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if err != nil {
			return "", err
		}
		if !atomic {
			operand = "(" + operand + ")"
		}
		if compilerTypes.IsNullable(node.OperandType) {
			return operand + " " + operator + " nullptr", nil
		}
		representation, index, ok := remapUnionMember(node.Operand, node.OperandType, unionMemberIndex(node.OperandType, compilerTypes.Nil), state)
		if !ok {
			representation, index = node.OperandType, unionMemberIndex(node.OperandType, compilerTypes.Nil)
		}
		return operand + ".tag " + operator + " " + unionTagName(representation, index), nil
	case checker.UnionInjectionExpression:
		return renderUnionInjection(node, state)
	case checker.UnionWidenExpression:
		return renderUnionWiden(node, state)
	case checker.UnionTestExpression:
		return renderUnionTest(node, state)
	case checker.UnionPayloadExpression:
		return renderUnionPayload(node, state)
	case checker.UnionEqualityExpression:
		return renderUnionEquality(node, state)
	case checker.HeapAllocateExpression:
		return renderHeapAllocate(node, state)
	case checker.HeapFreeExpression:
		return renderHeapFree(node, state)
	case checker.AdtConstructExpression:
		return renderAdtConstruct(node, state)
	case checker.AdtPayloadExpression:
		return renderAdtPayload(node, state)
	case checker.MatchExpression:
		return "", unknownExpressionDiagnostic("match expressions lower at statement level")
	case checker.ObjectExpression:
		return objectLiteralWithState(node.Object, state)
	case checker.ConstantExpression, checker.UnaryOperationExpression, checker.BinaryOperationExpression:
		return renderOperationWithState(node, state)
	default:
		return "", unknownExpressionDiagnostic("unsupported checked expression")
	}
}

func renderOperation(node checker.Expression) (string, error) {
	return renderOperationWithState(node, &expressionValidation{})
}

func renderOperationWithState(node checker.Expression, state *expressionValidation) (string, error) {
	switch node.Kind {
	case checker.ConstantExpression:
		if node.Constant == nil || node.Constant.Kind != checker.ConstantOperand && node.Constant.Kind != checker.ObjectOperand ||
			!compilerTypes.Equal(node.ResultType, node.Constant.Type) ||
			!supportedGeneratedScalarType(node.ResultType) && node.Constant.Type.Object == nil && node.Constant.Type.Union == nil {
			return "", unknownExpressionDiagnostic("constant expression without a checked constant")
		}
		return renderOperandWithState(*node.Constant, state)
	case checker.UnaryOperationExpression:
		return renderUnaryOperationWithState(node, state)
	case checker.BinaryOperationExpression:
		return renderBinaryOperationWithState(node, state)
	case checker.InvalidExpression, checker.VariableExpression, checker.AddressOfExpression,
		checker.DereferenceExpression, checker.MemberExpression, checker.ObjectExpression,
		checker.FunctionReferenceExpression, checker.CallExpression, checker.MethodCallExpression:
		return "", unknownExpressionDiagnostic("non-operation passed to operation renderer")
	default:
		return "", unknownExpressionDiagnostic("unsupported checked operation")
	}
}

func renderUnaryOperation(node checker.Expression) (string, error) {
	return renderUnaryOperationWithState(node, &expressionValidation{})
}

func renderUnaryOperationWithState(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Operand == nil {
		return "", unknownExpressionDiagnostic("unary operation without an operand")
	}
	if node.Operator == checker.LogicalNotOperator {
		return renderLogicalNotWithState(node, state)
	}
	if !supportedGeneratedScalarType(node.OperandType) || !supportedGeneratedScalarType(node.ResultType) {
		return "", unknownExpressionDiagnostic("unary operation with an unsupported type")
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return "", err
	}
	operand, err := renderExpressionExpectedWithState(*node.Operand, node.OperandType, true, state)
	if err != nil {
		return "", err
	}

	switch node.Operator {
	case checker.NegateOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return "", unknownExpressionDiagnostic("negation result type does not match its operand type")
		}
		if compilerTypes.IsSignedInteger(node.OperandType) {
			return renderSignedWrap(node.Operator, node.OperandType, "", operand)
		}
		if compilerTypes.IsFloat(node.OperandType) {
			return "(-" + operand + ")", nil
		}
		return "", unknownExpressionDiagnostic("negation of an unsupported type")
	case checker.BitwiseNotOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return "", unknownExpressionDiagnostic("complement result type does not match its operand type")
		}
		if !compilerTypes.IsInteger(node.OperandType) || compilerTypes.IsRune(node.OperandType) {
			return "", unknownExpressionDiagnostic("complement of an unsupported type")
		}
		return renderBitwiseComplement(node.OperandType, operand)
	case checker.InvalidOperator,
		checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator,
		checker.DivideOperator, checker.RemainderOperator, checker.EqualOperator,
		checker.NotEqualOperator, checker.LessOperator, checker.LessEqualOperator,
		checker.GreaterOperator, checker.GreaterEqualOperator, checker.LogicalAndOperator,
		checker.LogicalOrOperator, checker.BitwiseAndOperator, checker.BitwiseXorOperator,
		checker.BitwiseOrOperator, checker.ShiftLeftOperator, checker.ShiftRightOperator:
		return "", unknownExpressionDiagnostic("binary operator in unary operation")
	default:
		return "", unknownExpressionDiagnostic("unknown unary operator")
	}
}

// renderLogicalNotWithState renders !operand per RFC 0023: the operand's
// truthiness is negated, so any value-producing operand is valid.
func renderLogicalNotWithState(node checker.Expression, state *expressionValidation) (string, error) {
	if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
		return "", unknownExpressionDiagnostic("logical not requires a truthy-compatible operand and a Bool result")
	}
	child, err := renderTruthinessChild(node.Operand, state, node.OperandType)
	if err != nil {
		return "", err
	}
	return "(!" + child + ")", nil
}

func renderBinaryOperation(node checker.Expression) (string, error) {
	return renderBinaryOperationWithState(node, &expressionValidation{})
}

func renderBinaryOperationWithState(node checker.Expression, state *expressionValidation) (string, error) {
	if node.Left == nil || node.Right == nil {
		return "", unknownExpressionDiagnostic("binary operation without both operands")
	}
	if node.Operator == checker.LogicalAndOperator || node.Operator == checker.LogicalOrOperator {
		return renderLogicalOperationWithState(node, state)
	}
	if !supportedGeneratedScalarType(node.OperandType) && node.OperandType.Element == nil || !supportedGeneratedScalarType(node.ResultType) {
		return "", unknownExpressionDiagnostic("binary operation with an unsupported type")
	}
	// RFC 0032: a shift count keeps its own integer type; it never takes the
	// left operand's type.
	rightExpected := node.OperandType
	if node.Operator == checker.ShiftLeftOperator || node.Operator == checker.ShiftRightOperator {
		if rightType, ok := expressionTypeWithState(*node.Right, state); ok {
			rightExpected = rightType
		}
	}
	if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
		return "", err
	}
	if err := validateExpressionChildWithState(node.Right, rightExpected, state); err != nil {
		return "", err
	}
	resultIsBool := false
	arithmeticResult := false
	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator, checker.DivideOperator:
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return "", unknownExpressionDiagnostic("arithmetic operation with an unsupported type")
		}
	case checker.RemainderOperator:
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) {
			return "", unknownExpressionDiagnostic("remainder operation with a non-integer type")
		}
	case checker.EqualOperator, checker.NotEqualOperator:
		if node.OperandType.ScalarKind == compilerTypes.ScalarNone && node.OperandType.Element == nil {
			return "", unknownExpressionDiagnostic("equality operation with a non-scalar type")
		}
		resultIsBool = true
	case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return "", unknownExpressionDiagnostic("ordering operation with an unsupported type")
		}
		resultIsBool = true
	case checker.LogicalAndOperator, checker.LogicalOrOperator:
		// Unreachable: logical operations are routed to
		// renderLogicalOperationWithState before the scalar guards.
		resultIsBool = true
	case checker.BitwiseAndOperator, checker.BitwiseXorOperator, checker.BitwiseOrOperator:
		// RFC 0032: bitwise operations require an eligible integer type at
		// the selected exact width.
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) || compilerTypes.IsRune(node.OperandType) {
			return "", unknownExpressionDiagnostic("bitwise operation with an unsupported type")
		}
	case checker.ShiftLeftOperator, checker.ShiftRightOperator:
		// RFC 0032: shifts preserve the left operand's type.
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) || compilerTypes.IsRune(node.OperandType) {
			return "", unknownExpressionDiagnostic("shift operation with an unsupported type")
		}
	case checker.InvalidOperator, checker.NegateOperator, checker.LogicalNotOperator, checker.BitwiseNotOperator:
		return "", unknownExpressionDiagnostic("non-binary operator in binary operation")
	default:
		return "", unknownExpressionDiagnostic("unknown binary operator")
	}
	if arithmeticResult && !compilerTypes.Equal(node.OperandType, node.ResultType) {
		return "", unknownExpressionDiagnostic("arithmetic result type does not match its operand type")
	}
	if resultIsBool != compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
		return "", unknownExpressionDiagnostic("binary operation has an invalid result type")
	}
	left, err := renderExpressionExpectedWithState(*node.Left, node.OperandType, true, state)
	if err != nil {
		return "", err
	}
	right, err := renderExpressionExpectedWithState(*node.Right, rightExpected, true, state)
	if err != nil {
		return "", err
	}

	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator:
		if compilerTypes.IsSignedInteger(node.OperandType) {
			return renderSignedWrap(node.Operator, node.OperandType, left, right)
		}
		if compilerTypes.IsUnsignedInteger(node.OperandType) {
			return renderUnsignedArithmetic(node.Operator, node.OperandType, left, right)
		}
	case checker.DivideOperator, checker.RemainderOperator:
		if compilerTypes.IsInteger(node.OperandType) {
			return renderDivisionOperation(node, left, right)
		}
	case checker.BitwiseAndOperator, checker.BitwiseXorOperator, checker.BitwiseOrOperator:
		return renderBitwiseOperation(node.Operator, node.OperandType, left, right)
	case checker.ShiftLeftOperator, checker.ShiftRightOperator:
		return shiftHelperName(shiftSpec{operator: node.Operator, typ: node.OperandType}) + "(" + left + ", (uint64_t)(" + right + "))", nil
	case checker.EqualOperator, checker.NotEqualOperator, checker.LessOperator,
		checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator,
		checker.LogicalAndOperator, checker.LogicalOrOperator:
		operator, ok := binaryCOperator(node.Operator)
		if !ok {
			return "", unknownExpressionDiagnostic("unknown binary operator")
		}
		return "(" + left + " " + operator + " " + right + ")", nil
	case checker.InvalidOperator, checker.NegateOperator, checker.LogicalNotOperator, checker.BitwiseNotOperator:
		return "", unknownExpressionDiagnostic("non-binary operator in binary operation")
	default:
		return "", unknownExpressionDiagnostic("unknown binary operator " + node.Operator.String())
	}
	operator, ok := binaryCOperator(node.Operator)
	if !ok {
		return "", unknownExpressionDiagnostic("unknown binary operator")
	}
	return "(" + left + " " + operator + " " + right + ")", nil
}

// renderLogicalOperationWithState renders and/or per RFC 0023: operands of
// any value-producing type are rendered through their truthiness; the
// generated &&/|| preserve the short-circuit rule from RFC 0015, and the
// comma expressions keep each operand's evaluation when it is reached.
func renderLogicalOperationWithState(node checker.Expression, state *expressionValidation) (string, error) {
	if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
		return "", unknownExpressionDiagnostic("logical operation requires a truthy-compatible operand and a Bool result")
	}
	left, err := renderTruthinessChild(node.Left, state, node.OperandType)
	if err != nil {
		return "", err
	}
	right, err := renderTruthinessChild(node.Right, state, node.OperandType)
	if err != nil {
		return "", err
	}
	operator := "&&"
	if node.Operator == checker.LogicalOrOperator {
		operator = "||"
	}
	return "(" + left + " " + operator + " " + right + ")", nil
}

func binaryCOperator(operator checker.Operator) (string, bool) {
	switch operator {
	case checker.AddOperator:
		return "+", true
	case checker.SubtractOperator:
		return "-", true
	case checker.MultiplyOperator:
		return "*", true
	case checker.DivideOperator:
		return "/", true
	case checker.RemainderOperator:
		return "%", true
	case checker.EqualOperator:
		return "==", true
	case checker.NotEqualOperator:
		return "!=", true
	case checker.LessOperator:
		return "<", true
	case checker.LessEqualOperator:
		return "<=", true
	case checker.GreaterOperator:
		return ">", true
	case checker.GreaterEqualOperator:
		return ">=", true
	case checker.LogicalAndOperator:
		return "&&", true
	case checker.LogicalOrOperator:
		return "||", true
	case checker.InvalidOperator, checker.NegateOperator, checker.LogicalNotOperator:
		return "", false
	default:
		return "", false
	}
}

func unsignedCName(typ compilerTypes.Type) (string, bool) {
	if !supportedGeneratedScalarType(typ) {
		return "", false
	}
	switch typ {
	case compilerTypes.UInt8, compilerTypes.Int8:
		return "uint8_t", true
	case compilerTypes.UInt16, compilerTypes.Int16:
		return "uint16_t", true
	case compilerTypes.UInt32, compilerTypes.Int32:
		return "uint32_t", true
	case compilerTypes.UInt64, compilerTypes.Int64:
		return "uint64_t", true
	case compilerTypes.SizeType:
		return "size_t", true
	case compilerTypes.Rune:
		return "uint32_t", true
	default:
		return "", false
	}
}

func renderSignedWrap(operator checker.Operator, typ compilerTypes.Type, left, right string) (string, error) {
	// Use uint64_t for every signed width so the operands cannot promote into a
	// signed type before the modular arithmetic is evaluated.
	if !compilerTypes.IsSignedInteger(typ) {
		return "", unknownExpressionDiagnostic("signed wrapping requires a signed integer type")
	}
	unsigned, ok := unsignedCName(typ)
	if !ok {
		return "", unknownExpressionDiagnostic("signed wrapping has an unsupported integer width")
	}
	intermediate := compilerTypes.UInt64.CName
	var unsignedResult string
	switch operator {
	case checker.NegateOperator:
		if right == "" {
			return "", unknownExpressionDiagnostic("signed negation without an operand")
		}
		unsignedResult = fmt.Sprintf("(%s)((%s)0 - (%s)%s)", unsigned, intermediate, intermediate, right)
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator:
		if left == "" || right == "" {
			return "", unknownExpressionDiagnostic("signed operation without both operands")
		}
		operatorText, ok := binaryCOperator(operator)
		if !ok {
			return "", unknownExpressionDiagnostic("unknown signed wrapping operator")
		}
		unsignedResult = fmt.Sprintf("(%s)((%s)%s %s (%s)%s)", unsigned, intermediate, left, operatorText, intermediate, right)
	case checker.InvalidOperator, checker.LogicalNotOperator, checker.DivideOperator,
		checker.RemainderOperator, checker.EqualOperator, checker.NotEqualOperator,
		checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator,
		checker.GreaterEqualOperator, checker.LogicalAndOperator, checker.LogicalOrOperator:
		return "", unknownExpressionDiagnostic("operator is not signed wrapping arithmetic")
	default:
		return "", unknownExpressionDiagnostic("unknown signed wrapping operator")
	}
	// C23 makes an out-of-range unsigned-to-signed conversion implementation-defined.
	// Both branches convert only values representable by the signed target.
	maximum := signedMaximumMacro(typ)
	return fmt.Sprintf("((%s)%s <= (%s)%s ? (%s)%s : %s + (%s)((%s)%s - (%s)%s - (%s)1))",
		intermediate, unsignedResult, intermediate, maximum,
		typ.CName, unsignedResult,
		signedMinimumMacro(typ), typ.CName, intermediate, unsignedResult, intermediate, maximum, intermediate), nil
}

func renderUnsignedArithmetic(operator checker.Operator, typ compilerTypes.Type, left, right string) (string, error) {
	// Narrow unsigned operands promote to int, so compute them in uint32_t;
	// compute wider operands in uint64_t before narrowing the final result.
	if !compilerTypes.IsUnsignedInteger(typ) {
		return "", unknownExpressionDiagnostic("unsigned arithmetic requires an unsigned integer type")
	}
	unsigned, ok := unsignedCName(typ)
	if !ok || left == "" || right == "" {
		return "", unknownExpressionDiagnostic("unsigned arithmetic has invalid operands or width")
	}
	var operatorText string
	switch operator {
	case checker.AddOperator:
		operatorText = "+"
	case checker.SubtractOperator:
		operatorText = "-"
	case checker.MultiplyOperator:
		operatorText = "*"
	case checker.DivideOperator:
		operatorText = "/"
	case checker.RemainderOperator:
		operatorText = "%"
	case checker.InvalidOperator, checker.NegateOperator, checker.LogicalNotOperator,
		checker.EqualOperator, checker.NotEqualOperator, checker.LessOperator,
		checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator,
		checker.LogicalAndOperator, checker.LogicalOrOperator:
		return "", unknownExpressionDiagnostic("operator is not unsigned arithmetic")
	default:
		return "", unknownExpressionDiagnostic("operator is not unsigned arithmetic")
	}
	intermediate := compilerTypes.UInt64.CName
	if compilerTypes.Equal(typ, compilerTypes.UInt8) || compilerTypes.Equal(typ, compilerTypes.UInt16) {
		intermediate = compilerTypes.UInt32.CName
	}
	return fmt.Sprintf("(%s)((%s)%s %s (%s)%s)", unsigned, intermediate, left, operatorText, intermediate, right), nil
}

func renderExpressionNode(node checker.Expression) (string, bool, error) {
	return renderExpressionNodeWithState(node, &expressionValidation{})
}

func renderExpressionNodeWithState(node checker.Expression, state *expressionValidation) (string, bool, error) {
	return renderExpressionNodeWithExpectedState(node, compilerTypes.Type{}, state, false)
}

func renderExpressionNodeWithExpectedState(node checker.Expression, expected compilerTypes.Type, state *expressionValidation, hasExpected bool) (string, bool, error) {
	value, err := renderExpressionExpectedWithState(node, expected, hasExpected, state)
	if err != nil {
		return "", false, err
	}
	return value, node.Kind == checker.VariableExpression || node.Kind == checker.ObjectExpression ||
		node.Kind == checker.MemberExpression || node.Kind == checker.FunctionReferenceExpression ||
		node.Kind == checker.CallExpression || node.Kind == checker.MethodCallExpression || node.Kind == checker.NilExpression ||
		node.Kind == checker.IndexExpression || node.Kind == checker.CollectionMethodCallExpression || node.Kind == checker.CollectionSliceExpression ||
		node.Kind == checker.StringLiteralExpression || node.Kind == checker.StringMethodCallExpression || node.Kind == checker.StringFromBytesExpression || node.Kind == checker.ListNewExpression || node.Kind == checker.DictNewExpression ||
		node.Kind == checker.DeepEqualityExpression || node.Kind == checker.StringCompareExpression || node.Kind == checker.WideningExpression || node.Kind == checker.ConversionExpression, nil
}

// renderReceiver renders one method receiver with its checked expected type
// and parenthesizes it unless it is already one C atom. The receiver-
// render-and-parenthesize block lives only here (RFC 0057 Item 4); the nil
// guard keeps every call site safe even where earlier whole-expression
// validation already proved the operand present.
func renderReceiver(operand *checker.Expression, expected compilerTypes.Type, state *expressionValidation) (string, error) {
	if operand == nil {
		return "", unknownExpressionDiagnostic("receiver expression is missing")
	}
	receiver, atomic, err := renderExpressionNodeWithExpectedState(*operand, expected, state, true)
	if err != nil {
		return "", err
	}
	if !atomic {
		receiver = "(" + receiver + ")"
	}
	return receiver, nil
}

func expressionResultType(node checker.Expression) (compilerTypes.Type, bool) {
	switch node.Kind {
	case checker.NilExpression:
		return node.ResultType, true
	case checker.VariableExpression, checker.AddressOfExpression, checker.DereferenceExpression:
		if node.ResultType != (compilerTypes.Type{}) {
			return node.ResultType, true
		}
	case checker.ConstantExpression, checker.UnaryOperationExpression, checker.BinaryOperationExpression,
		checker.FunctionReferenceExpression, checker.NullTestExpression, checker.UnionInjectionExpression,
		checker.UnionWidenExpression, checker.UnionTestExpression, checker.UnionPayloadExpression,
		checker.UnionEqualityExpression, checker.HeapAllocateExpression,
		checker.AdtConstructExpression, checker.AdtPayloadExpression, checker.MatchExpression,
		checker.ArrayLiteralExpression, checker.IndexExpression, checker.CollectionMethodCallExpression,
		checker.CollectionSliceExpression, checker.StringLiteralExpression, checker.StringMethodCallExpression,
		checker.StringFromBytesExpression, checker.StringFromRunesExpression, checker.RuneCursorMethodCallExpression, checker.ListNewExpression, checker.DictNewExpression,
		checker.DeepEqualityExpression, checker.StringCompareExpression, checker.WideningExpression, checker.ConversionExpression,
		checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression,
		checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression,
		checker.MutexConstructorExpression, checker.MutexMethodCallExpression,
		checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression,
		checker.LayoutExpression, checker.VolatileReadExpression, checker.VolatileWriteExpression, checker.ViewBridgeExpression:
		return node.ResultType, true
	case checker.HeapFreeExpression:
		return compilerTypes.Type{}, false
	case checker.CallExpression, checker.MethodCallExpression:
		// A call that produces no value has no type to report.
		return node.ResultType, node.ResultType != (compilerTypes.Type{})
	case checker.MemberExpression:
		if node.Member != nil {
			return node.Member.Type, true
		}
	case checker.ObjectExpression:
		if node.Object != nil {
			return node.Object.Type, true
		}
	}
	return compilerTypes.Type{}, false
}

func expressionTypeWithState(node checker.Expression, state *expressionValidation) (compilerTypes.Type, bool) {
	return expressionTypeWithStateSeen(node, state, make(map[*checker.Expression]bool))
}

func expressionTypeWithStateSeen(node checker.Expression, state *expressionValidation, active map[*checker.Expression]bool) (compilerTypes.Type, bool) {
	if typ, ok := expressionResultType(node); ok {
		return typ, true
	}
	switch node.Kind {
	case checker.VariableExpression:
		if state != nil {
			binding, ok := state.bindingFor(node)
			return binding.typ, ok
		}
	case checker.AddressOfExpression:
		if node.Operand == nil {
			return compilerTypes.Type{}, false
		}
		if active[node.Operand] {
			return compilerTypes.Type{}, false
		}
		active[node.Operand] = true
		operandType, ok := expressionTypeWithStateSeen(*node.Operand, state, active)
		delete(active, node.Operand)
		if ok {
			return compilerTypes.PtrType(operandType), true
		}
	case checker.DereferenceExpression:
		if node.Operand == nil {
			return compilerTypes.Type{}, false
		}
		if active[node.Operand] {
			return compilerTypes.Type{}, false
		}
		active[node.Operand] = true
		receiverType, ok := expressionTypeWithStateSeen(*node.Operand, state, active)
		delete(active, node.Operand)
		if !ok && node.OperandType != (compilerTypes.Type{}) {
			receiverType, ok = node.OperandType, true
		}
		if ok && isPointerType(receiverType) {
			return *receiverType.Element, true
		}
	case checker.MemberExpression:
		if node.Operand == nil || node.Member == nil {
			return compilerTypes.Type{}, false
		}
		if active[node.Operand] {
			return compilerTypes.Type{}, false
		}
		active[node.Operand] = true
		receiverType, ok := expressionTypeWithStateSeen(*node.Operand, state, active)
		delete(active, node.Operand)
		if !ok && node.OperandType != (compilerTypes.Type{}) {
			receiverType, ok = node.OperandType, true
		}
		if ok && receiverType.Object != nil {
			return node.Member.Type, true
		}
	}
	return compilerTypes.Type{}, false
}

func writeLineDirective(body *strings.Builder, line int, filename string) {
	if line > 0 {
		fmt.Fprintf(body, "#line %d \"%s\"\n", line, filename)
	}
}

func renderOperand(source checker.Operand) (string, error) {
	return renderOperandWithState(source, &expressionValidation{})
}

func renderOperandWithState(source checker.Operand, state *expressionValidation) (string, error) {
	if err := validateCheckedOperandWithState(source, state); err != nil {
		return "", err
	}
	switch source.Kind {
	case checker.ObjectOperand:
		if source.Object == nil || !compilerTypes.Equal(source.Type, source.Object.Type) {
			return "", unknownExpressionDiagnostic("object operand has mismatched checked types")
		}
		return objectLiteralWithState(source.Object, state)
	case checker.VariableOperand, checker.ExpressionOperand:
		if expressionType, ok := expressionResultType(source.Node); ok && !compilerTypes.Equal(source.Type, expressionType) && !compilerTypes.WidensTo(expressionType, source.Type) {
			return "", unknownExpressionDiagnostic("operand expression type does not match its checked type")
		}
		return renderExpressionExpectedWithState(source.Node, source.Type, true, state)
	case checker.ConstantOperand:
		// RFC 0029: an object constant (Error.new result wrapped by union
		// injection) renders its object value.
		if source.Object != nil {
			return objectLiteralWithState(source.Object, state)
		}
		// Nil is the singleton type: its one value lowers to the C23 nullptr
		// predefined constant and carries no go/constant.
		if compilerTypes.IsNil(source.Type) {
			return "nullptr", nil
		}
		// EoS is the RFC 0031 singleton: its one value is the tag-only
		// marker and carries no go/constant.
		if compilerTypes.IsEoS(source.Type) {
			return "((hex_eos){ 0 })", nil
		}
		// Heap is a singleton handle: Heap.new() selects the default
		// allocator identity and performs no allocation.
		if compilerTypes.IsHeap(source.Type) {
			return "((hex_heap){ .identity = HEX_HEAP_DEFAULT })", nil
		}
		if source.Constant == nil {
			return "", unknownExpressionDiagnostic("constant operand without a checked value")
		}
		switch source.Type.ScalarKind {
		case compilerTypes.ScalarBool:
			if !compilerTypes.Equal(source.Type, compilerTypes.Bool) || source.Constant.Kind() != constant.Bool {
				return "", unknownExpressionDiagnostic("invalid checked Bool constant")
			}
		case compilerTypes.ScalarUnsignedInteger, compilerTypes.ScalarSignedInteger:
			if _, ok := unsignedCName(source.Type); !ok || source.Constant.Kind() != constant.Int {
				return "", unknownExpressionDiagnostic("invalid checked integer constant")
			}
		case compilerTypes.ScalarFloat:
			return renderFloatLiteral(source)
		default:
			return "", unknownExpressionDiagnostic("unsupported checked constant type")
		}
	default:
		return "", unknownExpressionDiagnostic("unsupported checked operand")
	}
	switch source.Type.ScalarKind {
	case compilerTypes.ScalarBool:
		return strconv.FormatBool(constant.BoolVal(source.Constant)), nil
	case compilerTypes.ScalarUnsignedInteger, compilerTypes.ScalarSignedInteger:
		return integerLiteral(source)
	case compilerTypes.ScalarFloat:
		return renderFloatLiteral(source)
	default:
		return "", compilerTypes.Diagnostic{
			Category: compilerTypes.UnknownError,
			Stage:    "generator",
			Message:  "unsupported checked scalar type " + source.Type.Name,
		}
	}
}

func objectLiteral(value *checker.ObjectValue) (string, error) {
	return objectLiteralWithState(value, &expressionValidation{})
}

func objectLiteralWithState(value *checker.ObjectValue, state *expressionValidation) (string, error) {
	if err := validateObjectValue(value, state); err != nil {
		return "", err
	}
	byMember := make(map[*compilerTypes.ObjectMember]checker.Operand, len(value.Initializers))
	for _, initializer := range value.Initializers {
		if initializer.Member == nil {
			return "", unknownExpressionDiagnostic("object initializer without a checked member")
		}
		byMember[initializer.Member] = initializer.Source
	}
	var result strings.Builder
	fmt.Fprintf(&result, "(%s){", value.Type.CName)
	for index := range value.Type.Object.Members {
		member := &value.Type.Object.Members[index]
		source, ok := byMember[member]
		if !ok {
			return "", unknownExpressionDiagnostic("incomplete checked object value")
		}
		rendered, err := renderOperandWithState(source, state)
		if err != nil {
			return "", err
		}
		fmt.Fprintf(&result, "\n        .%s = %s,", PrivateCName(MemberName, member.Name, ""), rendered)
	}
	result.WriteString("\n    }")
	return result.String(), nil
}

func renderFloatLiteral(source checker.Operand) (string, error) {
	if err := validateFloatConstant(source); err != nil {
		return "", err
	}
	bitSize := 64
	bits := source.FloatBits
	if compilerTypes.Equal(source.Type, compilerTypes.Float32) {
		bitSize = 32
		bits = uint64(uint32(bits))
	}
	_, special := floatSignAndSpecial(bits, bitSize)
	if special {
		fraction := bits & ((uint64(1) << 52) - 1)
		if bitSize == 32 {
			fraction = bits & ((uint64(1) << 23) - 1)
		}
		literal := "INFINITY"
		if fraction != 0 {
			literal = "NAN"
		}
		if source.Negative {
			literal = "-" + literal
		}
		return literal, nil
	}
	if bitSize == 32 {
		return formatHexFloat(bits, 32) + "f", nil
	}
	return formatHexFloat(bits, 64), nil
}

func integerLiteral(source checker.Operand) (string, error) {
	if err := validateIntegerConstant(source); err != nil {
		return "", err
	}
	value := source.Constant
	negative := source.Negative
	if constant.Sign(value) < 0 {
		value = constant.UnaryOp(gotoken.SUB, value, 0)
	}
	unsigned, ok := constant.Uint64Val(value)
	if !ok {
		return "", unknownExpressionDiagnostic("checked integer does not fit uint64")
	}
	if compilerTypes.IsSignedInteger(source.Type) {
		limit := uint64(1) << (source.Type.Bits - 1)
		if unsigned > limit || (!negative && unsigned == limit) {
			return "", unknownExpressionDiagnostic("checked integer does not fit its signed type")
		}
	} else {
		limit := ^uint64(0)
		if source.Type.Bits < 64 {
			limit = uint64(1)<<source.Type.Bits - 1
		}
		if unsigned > limit {
			return "", unknownExpressionDiagnostic("checked integer does not fit its unsigned type")
		}
	}
	if negative && unsigned == uint64(1)<<(source.Type.Bits-1) && compilerTypes.IsSignedInteger(source.Type) {
		return signedMinimumMacro(source.Type), nil
	}
	digits := formatInteger(unsigned, source.Radix)
	if compilerTypes.Equal(source.Type, compilerTypes.Int64) {
		if negative {
			return "-INT64_C(" + digits + ")", nil
		}
		return "INT64_C(" + digits + ")", nil
	}
	if compilerTypes.Equal(source.Type, compilerTypes.UInt64) {
		return "UINT64_C(" + digits + ")", nil
	}
	if negative {
		return "-" + digits, nil
	}
	return digits, nil
}

func signedMinimumMacro(typ compilerTypes.Type) string {
	switch typ.Name {
	case compilerTypes.Int8.Name:
		return "INT8_MIN"
	case compilerTypes.Int16.Name:
		return "INT16_MIN"
	case compilerTypes.Int32.Name:
		return "INT32_MIN"
	case compilerTypes.Int64.Name:
		return "INT64_MIN"
	default:
		panic("generator: unsupported signed minimum type " + typ.Name)
	}
}

func signedMaximumMacro(typ compilerTypes.Type) string {
	switch typ.Name {
	case compilerTypes.Int8.Name:
		return "INT8_MAX"
	case compilerTypes.Int16.Name:
		return "INT16_MAX"
	case compilerTypes.Int32.Name:
		return "INT32_MAX"
	case compilerTypes.Int64.Name:
		return "INT64_MAX"
	default:
		panic("generator: unsupported signed maximum type " + typ.Name)
	}
}

func formatInteger(value uint64, radix checker.LiteralRadix) string {
	switch radix {
	case checker.HexadecimalRadix:
		return "0x" + strings.ToUpper(strconv.FormatUint(value, 16))
	case checker.BinaryRadix:
		return "0b" + strconv.FormatUint(value, 2)
	default:
		return strconv.FormatUint(value, 10)
	}
}

// formatHexFloat renders an already rounded IEEE value as an exact C23
// hexadecimal floating constant. It handles normals, subnormals, zero, and
// the explicit sign bit without passing through a decimal approximation;
// IEC specials use the standard C macros instead of overflowing exponents.
func formatHexFloat(bits uint64, bitSize int) string {
	var sign uint64
	var exponent uint64
	var fraction uint64
	var bias, fractionDigits int
	if bitSize == 32 {
		bits = uint64(uint32(bits))
		sign = bits >> 31
		exponent = (bits >> 23) & 0xff
		fraction = bits & ((uint64(1) << 23) - 1)
		bias, fractionDigits = 127, 6
	} else {
		sign = bits >> 63
		exponent = (bits >> 52) & 0x7ff
		fraction = bits & ((uint64(1) << 52) - 1)
		bias, fractionDigits = 1023, 13
	}
	prefix := ""
	if sign != 0 {
		prefix = "-"
	}
	if bitSize == 64 && exponent == (uint64(1)<<11)-1 || bitSize == 32 && exponent == (uint64(1)<<8)-1 {
		if fraction == 0 {
			return prefix + "INFINITY"
		}
		return prefix + "NAN"
	}
	if exponent == 0 {
		if fraction == 0 {
			return prefix + "0x0p+0"
		}
		fractionText := fmt.Sprintf("%0*x", fractionDigits, fraction)
		fractionText = strings.TrimRight(fractionText, "0")
		return fmt.Sprintf("%s0x0.%sp-%d", prefix, fractionText, bias-1)
	}
	fractionText := fmt.Sprintf("%0*x", fractionDigits, fraction)
	fractionText = strings.TrimRight(fractionText, "0")
	unbiased := int(exponent) - bias
	if fractionText == "" {
		return fmt.Sprintf("%s0x1p%+d", prefix, unbiased)
	}
	return fmt.Sprintf("%s0x1.%sp%+d", prefix, fractionText, unbiased)
}
