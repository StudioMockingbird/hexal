// render.go owns shared statement and expression rendering: rendering
// dispatch, writeStatements and writeStatementsAt, shared C declaration
// spelling, and the shared rendering state (expressionValidation,
// generatedBinding, and their scope/binding/name-resolution methods).
package generator

import (
	"fmt"
	"go/constant"
	gotoken "go/token"
	"math"
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
				// print is a statement-level builtin producing no value; it
				// renders its own temporaries and helper calls.
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
			// The try prologue already hoisted above; the success value is
			// discarded, so the statement renders nothing.
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
			// errdefer registers exactly like defer; the Err flag decides at
			// the exit edge whether the action runs.
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
		// Discarding a constructor result is legal; it simply leaks the
		// allocation, which is the programmer's choice.
	default:
		return "", unknownExpressionDiagnostic("call statement without a checked call")
	}
	if !compilerTypes.Equal(statement.Call.Type, statement.Call.Node.ResultType) {
		return "", unknownExpressionDiagnostic("call statement type does not match its checked call")
	}
	return renderExpressionExpectedWithState(statement.Call.Node, nil, state)
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
	// defers.
	if hasPendingActions(state) {
		state.returnCounter++
		name := fmt.Sprintf("hex_return_%d", state.returnCounter)
		var builder strings.Builder
		fmt.Fprintf(&builder, "%s%s = %s;\n", indent, declaration(*result, name, false), value)
		if hasPendingErrDefers(state) {
			state.returnCounter++
			errorName := fmt.Sprintf("hex_err_%d", state.returnCounter)
			fmt.Fprintf(&builder, "%sconst bool %s = %s;\n", indent, errorName, returnErrorExit(statement.Value.Type, name, state.tags))
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
			return "", unknownExpressionDiagnostic("nullable operand in a truthiness context is not pointer-like")
		}
		return "(" + rendered + " != nullptr)", nil
	case compilerTypes.TruthinessUnion:
		if typ.Union == nil {
			return "", unknownExpressionDiagnostic("tagged union truthiness has no union metadata")
		}
		return unionTruthinessCall(typ, rendered), nil
	case compilerTypes.TruthinessAlwaysTrue:
		// A non-Bool, non-Nil, non-union value is always truthy. The (void)
		// cast marks the discarded operand intentional so the generated C is
		// warning-free under -Wunused-value.
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
	// owner is the encoded module owner of the module being generated;
	// filename is its logical source key for #line directives; moduleID is
	// the module's canonical identity, used to distinguish foreign method
	// calls from local ones.
	owner            string
	filename         string
	moduleID         string
	loopDepths       []int
	captureCounter   int
	returnCounter    int
	captures         map[*checker.Operand][]string
	matchCounter     int
	loopCounter      int // unique hex_for_N stem for for-in lowering
	tryCounter       int // unique hex_try_N stems for try prologue hoisting
	hoistedTries     map[*checker.Expression]string
	findCounter      int
	hoistedDictFinds map[*checker.Expression]string
	// spawnCounter and hoistedSpawns carry spawn prologues: each spawn's
	// argument frame and task handle are declared before the statement
	// renders, and the expression renders as the task handle.
	spawnCounter  int
	hoistedSpawns map[*checker.Expression]string
	// registeredDefers records the deferred actions whose statements were
	// processed, in registration order. A try error branch may render
	// earlier than a later defer statement, and must not run it.
	registeredDefers []checker.DeferredAction
	printCounter     int              // unique hex_print_arg_N stems for print temporaries
	strings          *literalRegistry // payload lookup for checked string rendering
	tags             *tagRegistry     // program-wide discriminant lookups
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
		return privateCName(valueName, sourceName, ""), nil
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
	base := privateCName(valueName, sourceName, "")
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
	return privateCName(valueName, node.Name, ""), true
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
		// A RuneCursor is a mutable-through descriptor; next() advances its
		// offset, so the binding carries no top-level const even without a
		// mut declaration.
		return typ.CName + " " + name
	}
	if typ.Atomic != nil {
		// An Atomic is a mutable-through wrapper; its accessors take a
		// non-const receiver, so the binding carries no top-level const even
		// without a mut declaration.
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
// top-level parameter const lives on the definition's local binding, never on
// the type, and C ignores it when comparing function types.
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
	return renderExpressionExpectedWithState(node, nil, state)
}

func renderExpressionExpectedWithState(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) (string, error) {
	if err := validateExpressionNode(node, expected, state); err != nil {
		return "", err
	}
	return renderExpressionUncheckedWithState(node, state)
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
		return privateCName(functionNameKind, node.Name, moduleOwner(node.Module, state.owner)), nil
	case checker.CallExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("call without a checked callee")
		}
		callee, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
		receiver, _, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, &receiverType, state)
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
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, optionalType(operandType, hasOperandType), state)
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
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, &operandType, state)
		if err != nil {
			return "", err
		}
		if atomic {
			return "*" + operand, nil
		}
		return "*(" + operand + ")", nil
	case checker.IndexExpression, checker.ArrayLiteralExpression, checker.CollectionMethodCallExpression, checker.CollectionSliceExpression:
		return renderCollectionExpression(node, state)
	case checker.StringLiteralExpression, checker.StringMethodCallExpression, checker.StringFromBytesExpression, checker.StringFromRunesExpression, checker.RuneCursorMethodCallExpression:
		return renderTextExpression(node, state)
	case checker.ListNewExpression, checker.DictNewExpression:
		return renderCollectionConstructor(node, state)
	case checker.WideningExpression:
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("widening without an operand")
		}
		operand, atomic, operandErr := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
		left, _, leftErr := renderExpressionNodeWithExpectedState(*node.Left, &node.OperandType, state)
		if leftErr != nil {
			return "", leftErr
		}
		right, _, rightErr := renderExpressionNodeWithExpectedState(*node.Right, &node.OperandType, state)
		if rightErr != nil {
			return "", rightErr
		}
		if !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) && !compilerTypes.IsList(node.OperandType) {
			left = "&(" + left + ")"
			right = "&(" + right + ")"
		}
		if compilerTypes.IsStrand(node.OperandType) {
			// Strand equality is a direct memcmp of the canonical 32-byte
			// zero-filled representation.
			result := "(memcmp(" + left + ".data, " + right + ".data, 32) == 0)"
			if node.Operator == checker.NotEqualOperator {
				result = "(memcmp(" + left + ".data, " + right + ".data, 32) != 0)"
			}
			return result, nil
		}
		result := equalityHelperName(node.OperandType) + "(" + left + ", " + right + ")"
		if node.Operator == checker.NotEqualOperator {
			result = "(!" + result + ")"
		}
		return result, nil
	case checker.StringCompareExpression:
		return renderTextComparison(node, state)
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
		// The C23 compiler is the final authority for the selected target
		// layout; the checker already proved T complete.
		if node.Name == "align_of" {
			return "(size_t)alignof(" + typeSpelling(node.OperandType) + ")", nil
		}
		return "(size_t)sizeof(" + typeSpelling(node.OperandType) + ")", nil
	case checker.VolatileReadExpression:
		if node.Operand == nil || node.OperandType.Element == nil {
			return "", unknownExpressionDiagnostic("volatile read without a checked pointer")
		}
		receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
		receiver, _, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
		if err != nil {
			return "", err
		}
		value, valueErr := renderOperandWithState(node.Arguments[0], state)
		if valueErr != nil {
			return "", valueErr
		}
		return "*(volatile " + typeSpelling(node.Element) + " *)(" + receiver + ") = " + value, nil
	case checker.ViewBridgeExpression:
		return renderViewBridgeExpression(node, state)
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
		return receiver + "." + privateCName(memberName, node.Member.Name, ""), nil
	case checker.NullTestExpression:
		// The nullable union shares its base pointer's null niche, so the
		// test lowers to the ordinary C null pointer comparison.
		if node.Operand == nil {
			return "", unknownExpressionDiagnostic("null test without a checked operand")
		}
		operator := "=="
		if node.Operator == checker.NotEqualOperator {
			operator = "!="
		}
		operand, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, &node.OperandType, state)
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
		return operand + ".tag " + operator + " " + state.tags.unionMemberTag(compilerTypes.UnionMembers(representation)[index]), nil
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
	operand, err := renderExpressionExpectedWithState(*node.Operand, &node.OperandType, state)
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

// renderLogicalNotWithState renders !operand: the operand's truthiness is
// negated, so any value-producing operand is valid.
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
	// An unsigned +, -, or * is one node of a ring tree. Only a maximal tree
	// reaches here: renderRingOperand renders same-type ring children itself
	// and never routes them back through this function, so every arrival is
	// a tree root and narrows once.
	if isUnsignedRingOperation(node) {
		return renderUnsignedRingTree(node, state)
	}
	// A shift count keeps its own integer type; it never takes the left
	// operand's type.
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
		// Bitwise operations require an eligible integer type at the
		// selected exact width.
		arithmeticResult = true
		if !compilerTypes.IsInteger(node.OperandType) || compilerTypes.IsRune(node.OperandType) {
			return "", unknownExpressionDiagnostic("bitwise operation with an unsupported type")
		}
	case checker.ShiftLeftOperator, checker.ShiftRightOperator:
		// Shifts preserve the left operand's type.
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
	left, err := renderExpressionExpectedWithState(*node.Left, &node.OperandType, state)
	if err != nil {
		return "", err
	}
	right, err := renderExpressionExpectedWithState(*node.Right, &rightExpected, state)
	if err != nil {
		return "", err
	}

	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator:
		if compilerTypes.IsSignedInteger(node.OperandType) {
			return renderSignedWrap(node.Operator, node.OperandType, left, right)
		}
		if compilerTypes.IsUnsignedInteger(node.OperandType) {
			// Unsigned +, -, and * are ring operations, routed to the tree
			// renderer above; reaching here means the operand and result
			// types disagree, which the ring predicate rejects.
			return "", unknownExpressionDiagnostic("unsigned arithmetic result type does not match its operand type")
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

// renderLogicalOperationWithState renders and/or: operands of any
// value-producing type are rendered through their truthiness; the generated
// &&/|| preserve the short-circuit rule, and the comma expressions keep each
// operand's evaluation when it is reached.
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
	if !compilerTypes.IsSignedInteger(typ) {
		return "", unknownExpressionDiagnostic("signed wrapping requires a signed integer type")
	}
	var name string
	switch operator {
	case checker.NegateOperator:
		name = "neg"
	case checker.AddOperator:
		name = "add"
	case checker.SubtractOperator:
		name = "sub"
	case checker.MultiplyOperator:
		name = "mul"
	case checker.InvalidOperator, checker.LogicalNotOperator, checker.DivideOperator,
		checker.RemainderOperator, checker.EqualOperator, checker.NotEqualOperator,
		checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator,
		checker.GreaterEqualOperator, checker.LogicalAndOperator, checker.LogicalOrOperator:
		return "", unknownExpressionDiagnostic("operator is not signed wrapping arithmetic")
	default:
		return "", unknownExpressionDiagnostic("unknown signed wrapping operator")
	}
	helper := wrapHelperName(wrapOperation{name: name, typ: typ})
	if name == "neg" {
		if right == "" {
			return "", unknownExpressionDiagnostic("signed negation without an operand")
		}
		return helper + "(" + right + ")", nil
	}
	if left == "" || right == "" {
		return "", unknownExpressionDiagnostic("signed operation without both operands")
	}
	return helper + "(" + left + ", " + right + ")", nil
}

// ringKeepEveryGrouping disables the redundant-parenthesis removal, leaving
// the construction's maximally parenthesized output. Correctness lives in the
// construction and readability in the removal, so a test can render both and
// assert they differ only in punctuation. Never set outside a test.
var ringKeepEveryGrouping = false

// isUnsignedRingOperation reports whether node is one ring operation: an
// unsigned +, -, or * whose operand and result type are the same unsigned
// integer type. Reduction modulo 2^N after arithmetic modulo 2^M is the same
// value as reducing after every node, so a connected tree of these evaluates
// in one uintmax_t domain and narrows once. Division, remainder,
// shifts, bitwise operations, comparisons, and conversions are not ring
// operations and terminate a tree.
func isUnsignedRingOperation(node checker.Expression) bool {
	if node.Kind != checker.BinaryOperationExpression || node.Left == nil || node.Right == nil {
		return false
	}
	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator:
	default:
		return false
	}
	return compilerTypes.IsUnsignedInteger(node.OperandType) &&
		compilerTypes.Equal(node.OperandType, node.ResultType)
}

// renderUnsignedRingTree renders one maximal ring tree: the whole tree
// evaluates in uintmax_t and narrows exactly once to its Hexal type, instead
// of widening and narrowing at every binary node.
func renderUnsignedRingTree(node checker.Expression, state *expressionValidation) (string, error) {
	unsigned, ok := unsignedCName(node.OperandType)
	if !ok {
		return "", unknownExpressionDiagnostic("unsigned arithmetic has an invalid width")
	}
	wide, err := renderRingWide(node, state)
	if err != nil {
		return "", err
	}
	return "(" + unsigned + ")(" + wide + ")", nil
}

// renderRingWide renders one ring node in the uintmax_t domain, without the
// narrowing cast its tree root carries. Per-node validation matches the
// ordinary binary path so a malformed node still fails closed.
func renderRingWide(node checker.Expression, state *expressionValidation) (string, error) {
	if !supportedGeneratedScalarType(node.OperandType) || !supportedGeneratedScalarType(node.ResultType) {
		return "", unknownExpressionDiagnostic("binary operation with an unsupported type")
	}
	if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
		return "", err
	}
	if err := validateExpressionChildWithState(node.Right, node.OperandType, state); err != nil {
		return "", err
	}
	operator, ok := binaryCOperator(node.Operator)
	if !ok {
		return "", unknownExpressionDiagnostic("unknown binary operator")
	}
	left, err := renderRingOperand(*node.Left, &node, true, state)
	if err != nil {
		return "", err
	}
	right, err := renderRingOperand(*node.Right, &node, false, state)
	if err != nil {
		return "", err
	}
	return left + " " + operator + " " + right, nil
}

// renderRingOperand renders one operand of a ring operation. A same-type ring
// child stays in the uintmax_t domain and is rendered here rather than through
// the ordinary renderer, which is what makes every arrival at
// renderBinaryOperationWithState a maximal tree root. Every other child is a
// boundary: it renders at the Hexal type through the ordinary renderer, and
// re-enters that renderer exactly once, so a ring subtree nested under a
// boundary starts its own tree with its own seed.
//
// A left boundary carries the single uintmax_t seed; C's usual arithmetic
// conversions then lift the right operand of that node and of every node above
// it. A right boundary needs no seed for the same reason.
func renderRingOperand(child checker.Expression, parent *checker.Expression, isLeft bool, state *expressionValidation) (string, error) {
	if isUnsignedRingOperation(child) && compilerTypes.Equal(child.OperandType, parent.OperandType) {
		inner, err := renderRingWide(child, state)
		if err != nil {
			return "", err
		}
		if ringGroupingRequired(parent.Operator, child.Operator, isLeft) {
			return "(" + inner + ")", nil
		}
		return inner, nil
	}
	rendered, atomic, err := renderExpressionNodeWithExpectedState(child, &parent.OperandType, state)
	if err != nil {
		return "", err
	}
	if ringKeepEveryGrouping || !atomic && child.Kind != checker.ConstantExpression {
		// A boundary that is not one C atom keeps its grouping: a division,
		// remainder, shift, bitwise expression, or comparison can lose
		// against its ring parent's precedence. Parenthesizing without
		// consulting a precedence table means the only possible error is
		// noisier C, never a different parse.
		rendered = "(" + rendered + ")"
	}
	if isLeft {
		return "(uintmax_t)" + rendered, nil
	}
	return rendered, nil
}

// ringGroupingRequired reports whether a ring child needs its own parentheses
// under a ring parent. Both sides render in the same C precedence class, so
// the usual rule applies: a left child may share its parent's precedence, a
// right child may not. Removing a pair only where the C parse is provably
// identical keeps grouping a property of the tree rather than of the text.
func ringGroupingRequired(parent, child checker.Operator, isLeft bool) bool {
	if ringKeepEveryGrouping {
		return true
	}
	if isLeft {
		return ringPrecedence(child) < ringPrecedence(parent)
	}
	return ringPrecedence(child) <= ringPrecedence(parent)
}

func ringPrecedence(operator checker.Operator) int {
	if operator == checker.MultiplyOperator {
		return 2
	}
	return 1
}

func renderExpressionNodeWithExpectedState(node checker.Expression, expected *compilerTypes.Type, state *expressionValidation) (string, bool, error) {
	value, err := renderExpressionExpectedWithState(node, expected, state)
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
// and parenthesizes it unless it is already one C atom. The receiver-render-
// and-parenthesize block lives only here; the nil guard keeps every call
// site safe even where earlier whole-expression validation already proved
// the operand present.
func renderReceiver(operand *checker.Expression, expected compilerTypes.Type, state *expressionValidation) (string, error) {
	if operand == nil {
		return "", unknownExpressionDiagnostic("receiver expression is missing")
	}
	receiver, atomic, err := renderExpressionNodeWithExpectedState(*operand, &expected, state)
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
		return renderExpressionExpectedWithState(source.Node, &source.Type, state)
	case checker.ConstantOperand:
		// An object constant (Error.new result wrapped by union injection)
		// renders its object value.
		if source.Object != nil {
			return objectLiteralWithState(source.Object, state)
		}
		// Nil is the singleton type: its one value lowers to the C23 nullptr
		// predefined constant and carries no go/constant.
		if compilerTypes.IsNil(source.Type) {
			return "nullptr", nil
		}
		// EoS is a singleton: its one value is the tag-only marker and
		// carries no go/constant.
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
		fmt.Fprintf(&result, "\n        .%s = %s,", privateCName(memberName, member.Name, ""), rendered)
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
		return formatDecimalFloat(bits, 32) + "f", nil
	}
	return formatDecimalFloat(bits, 64), nil
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
		minimum, minimumErr := signedMinimumMacro(source.Type)
		if minimumErr != nil {
			return "", minimumErr
		}
		return minimum, nil
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

func signedMinimumMacro(typ compilerTypes.Type) (string, error) {
	switch typ.Name {
	case compilerTypes.Int8.Name:
		return "INT8_MIN", nil
	case compilerTypes.Int16.Name:
		return "INT16_MIN", nil
	case compilerTypes.Int32.Name:
		return "INT32_MIN", nil
	case compilerTypes.Int64.Name:
		return "INT64_MIN", nil
	default:
		return "", unknownExpressionDiagnostic("no minimum macro for signed integer type " + typ.Name)
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

// formatDecimalFloat renders an already rounded IEEE value as the shortest
// readable decimal C literal that round-trips to the same bits.
// Formatting starts from the checked rounded bits, never from the original
// source spelling; the standard formatter produces the shortest decimal that
// reparses to those exact bits. An integral-looking mantissa receives a
// fractional point so the token stays a C floating constant.
func formatDecimalFloat(bits uint64, bitSize int) string {
	var value float64
	if bitSize == 32 {
		value = float64(math.Float32frombits(uint32(bits)))
	} else {
		value = math.Float64frombits(bits)
	}
	text := strconv.FormatFloat(value, 'g', -1, bitSize)
	if !strings.ContainsAny(text, ".eE") {
		text += ".0"
	}
	return text
}

// optionalType packages a (value, present) pair as the optional-expected-type
// pointer the validation and render entry points take. It exists only for the
// two callers that still receive the pair from elsewhere; nil means absent.
func optionalType(typ compilerTypes.Type, present bool) *compilerTypes.Type {
	if !present {
		return nil
	}
	return &typ
}
