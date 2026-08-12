// Package generator emits readable C23 from checked Hexal data.
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

const mainHeaderPrefix = "#ifndef HEXAL_MAIN_H\n#define HEXAL_MAIN_H\n\n#include <stdint.h>\n#include <stdbool.h>\n#include <limits.h>\n#include <stdlib.h>\n"
const sourceFilename = "main.hex"

// NameKind identifies the Hexal-owned declaration namespace lowered by the
// generator. The mapping is deliberately stateless and never consults a C
// keyword list or a name registry.
type NameKind uint8

const (
	ValueName NameKind = iota
	TypeName
	MemberName
	FunctionName
)

// PrivateCName applies the RFC 0004 private C prefix exactly once at the
// declaration/reference rendering boundary.
func PrivateCName(kind NameKind, source string) string {
	prefix := "hex_v_"
	switch kind {
	case TypeName:
		prefix = "hex_t_"
	case MemberName:
		prefix = "hex_m_"
	case FunctionName:
		prefix = "hex_f_"
	}
	return prefix + source
}

func validSourceName(name string) bool {
	if name == "" || !isASCIILetter(name[0]) {
		return false
	}
	for index := 1; index < len(name); index++ {
		character := name[index]
		if !isASCIILetter(character) && (character < '0' || character > '9') && character != '_' {
			return false
		}
	}
	return true
}

func isASCIILetter(character byte) bool {
	return character >= 'a' && character <= 'z' || character >= 'A' && character <= 'Z'
}

// Generate is the compatibility wrapper used by package-level callers. The
// compiler itself uses GenerateChecked so an internal generation failure stays
// a structured diagnostic instead of being silently turned into source.
func Generate(program checker.Program) (mainC string, mainH string) {
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		return GenerateFailure()
	}
	return mainC, mainH
}

// GenerateChecked emits direct C23 scalar and pointer operations. Checked
// literal metadata, not raw source text, is the authority for every
// initializer.
func GenerateChecked(program checker.Program) (mainC string, mainH string, err error) {
	functions, functionErr := declaredFunctions(program)
	if functionErr != nil {
		return "", "", functionErr
	}
	methods, methodErr := declaredMethods(program)
	if methodErr != nil {
		return "", "", methodErr
	}
	if validationErr := validateCheckedProgram(program, functions, methods); validationErr != nil {
		return "", "", validationErr
	}
	float32Used, float64Used, nilUsed := usedFloatTypes(program)
	objects, objectErr := objectDefinitions(program)
	if objectErr != nil {
		return "", "", objectErr
	}
	errorUsed := discoverErrorUsed(program)
	declared := declaredObjects(program)
	if errorUsed {
		// RFC 0029: the built-in Error object definition accompanies the
		// program when Error is referenced directly or inside a union. It is
		// emitted before the unions that may carry it as a payload member.
		declared[compilerTypes.ErrorType.Object] = true
	}
	unionState, unionErr := discoverGeneratedUnions(program)
	if unionErr != nil {
		return "", "", unionErr
	}
	heapState, heapErr := discoverHeapHelpers(program)
	if heapErr != nil {
		return "", "", heapErr
	}
	adtState, adtErr := discoverGeneratedADTs(program)
	if adtErr != nil {
		return "", "", adtErr
	}
	arrayState, arrayErr := discoverGeneratedArrays(program)
	if arrayErr != nil {
		return "", "", arrayErr
	}
	viewState, viewErr := discoverGeneratedViews(program)
	if viewErr != nil {
		return "", "", viewErr
	}
	stringState, stringErr := discoverGeneratedStrings(program)
	if stringErr != nil {
		return "", "", stringErr
	}
	listState, listErr := discoverGeneratedLists(program)
	if listErr != nil {
		return "", "", listErr
	}
	dictState, dictErr := discoverGeneratedDicts(program)
	if dictErr != nil {
		return "", "", dictErr
	}
	equalityState, equalityErr := discoverEqualityTypes(program)
	if equalityErr != nil {
		return "", "", equalityErr
	}
	conversionSpecs, conversionErr := discoverGeneratedConversions(program)
	if conversionErr != nil {
		return "", "", conversionErr
	}
	divisionTypes := discoverGeneratedDivisions(program)
	streamState, streamErr := discoverGeneratedStreams(program)
	if streamErr != nil {
		return "", "", streamErr
	}
	shiftSpecs := discoverGeneratedShifts(program)
	bitCastSpecs := discoverGeneratedBitCasts(program)
	endianSpecs := discoverGeneratedEndian(program)
	printState := discoverGeneratedPrint(program)
	ioState := discoverGeneratedIO(program, stringState)
	concurrencyState, concurrencyErr := discoverGeneratedConcurrency(program, functions, stringState)
	if concurrencyErr != nil {
		return "", "", concurrencyErr
	}
	if concurrencyState.used {
		// RFC 0037: the task runtime needs the String and Strand typedefs and
		// the Error object for the failure Errors every recoverable
		// operation constructs; the discovery pass registered the literals.
		// The Channel and Mutex helpers take a hex_heap argument, so the heap
		// machinery is required too.
		stringState.used = true
		stringState.needStrand = true
		heapState.required = true
	}
	if stringState.used {
		ensureViewUInt8(viewState)
		// The String helpers allocate through the heap machinery.
		heapState.required = true
	}
	if len(listState.order) > 0 || len(dictState.order) > 0 || len(streamState.order) > 0 {
		// The List, Dict, and Stream helpers allocate and trap through the
		// heap machinery and fputs.
		heapState.required = true
	}
	typeState := &generatedTypeValidation{declaredObjects: errorDeclaredObjects(program)}
	var body strings.Builder
	body.WriteString("#include \"main.h\"\n\n")

	// Function definitions sit at file scope in source order, after the object
	// definitions the header already carries and before main. Only self-
	// recursion and calls to earlier definitions are legal, so no prototype
	// region is needed.
	for _, statement := range program.Statements {
		switch declared := statement.(type) {
		case checker.FunctionDeclaration:
			if definitionErr := writeFunctionDefinition(&body, declared, functions, methods, typeState, stringState); definitionErr != nil {
				return "", "", definitionErr
			}
		case checker.MethodDeclaration:
			if definitionErr := writeMethodDefinition(&body, declared, functions, methods, typeState, stringState); definitionErr != nil {
				return "", "", definitionErr
			}
		}
	}

	// Concrete specializations are emitted after the regular definitions.
	// Their bodies can call functions declared before their generic template
	// (already emitted) or other specializations (any order), so each gets a
	// prototype first and the definitions follow in cache order.
	if err := writeSpecializedPrototypes(&body, program.SpecializedFunctions, program.SpecializedMethods, typeState); err != nil {
		return "", "", err
	}
	if err := writeSpecializedDefinitions(&body, program.SpecializedFunctions, program.SpecializedMethods, functions, methods, typeState, stringState); err != nil {
		return "", "", err
	}

	// RFC 0037: the spawn entry adapters follow every function definition
	// because they call the spawned functions directly.
	if err := writeSpawnAdaptersChecked(&body, concurrencyState); err != nil {
		return "", "", err
	}

	body.WriteString("int main(void) {\n")
	if concurrencyState.used {
		// RFC 0037: the scheduler initializes before any Hexal source
		// runs; the module statements execute as the root Task on worker
		// zero, and hex_task_complete below hands the process back to main.
		body.WriteString("    hex_scheduler_init();\n")
	}
	renderState := &expressionValidation{
		variables:      make(map[string]generatedBinding),
		bindings:       make(map[checker.BindingID]generatedBinding),
		bindingNames:   make(map[checker.BindingID]string),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
	}
	renderState.pushScope()
	// Module-level storage stays inside main; a function body cannot reach it,
	// so nothing is promoted to static storage duration.
	if statementErr := writeStatements(&body, program.Statements, renderState, nil, false, program.Defers); statementErr != nil {
		return "", "", statementErr
	}
	if concurrencyState.used {
		// RFC 0037: completing the root Task wakes the scheduler, stops the
		// workers, and switches back to main so it returns normally. Tasks
		// still active are abandoned to process termination, as the spec
		// requires.
		body.WriteString("    hex_task_complete(hex_root_task);\n")
	}

	body.WriteString("    return EXIT_SUCCESS;\n}\n")
	return body.String(), headerWithUnions(float32Used, float64Used, nilUsed, unionState, heapState, adtState, arrayState, viewState, stringState, listState, dictState, streamState, equalityState, conversionSpecs, divisionTypes, shiftSpecs, bitCastSpecs, endianSpecs, objects, errorUsed, printState, concurrencyState, ioState), nil
}

// writeStatements renders one statement list at a single indentation level.
// main's module statements and a function body share it; result is the
// enclosing function's declared result, and inFunction gates return. defers
// are the checked scope's registered deferred actions, emitted in reverse
// order when the list completes.
func writeStatements(body *strings.Builder, statements []checker.Statement, state *expressionValidation, result *compilerTypes.Type, inFunction bool, defers []checker.DeferredAction) error {
	return writeStatementsAt(body, statements, state, result, inFunction, "    ", defers)
}

func writeControlHeader(body *strings.Builder, indent, prefix, condition string, keywordLine, conditionLine int) {
	writeLineDirective(body, keywordLine)
	if conditionLine > 0 && conditionLine != keywordLine {
		fmt.Fprintf(body, "%s%s (\n", indent, prefix)
		writeLineDirective(body, conditionLine)
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
			writeLineDirective(body, statement.SourceLine)
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
			writeLineDirective(body, statement.SourceLine)
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
			writeLineDirective(body, statement.SourceLine)
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
		case checker.ReturnStatement:
			if !inFunction {
				return unknownExpressionDiagnostic("return outside a function body")
			}
			writeLineDirective(body, statement.SourceLine)
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
			writeControlHeader(body, indent, "if", condition, statement.SourceLine, statement.ConditionLine)
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
				writeControlHeader(body, indent, "} else if", condition, branch.SourceLine, branch.ConditionLine)
				state.pushScope()
				if err := writeStatementsAt(body, branch.Body, state, result, inFunction, indent+"    ", branchDefers(statement, branchIndex)); err != nil {
					return err
				}
				state.popScope()
			}
			if statement.Else != nil {
				writeLineDirective(body, statement.ElseLine)
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
			writeControlHeader(body, indent, "while", condition, statement.SourceLine, statement.ConditionLine)
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
			writeLineDirective(body, statement.SourceLine)
			if err := unwindToLoopDepth(body, state, indent, "false"); err != nil {
				return err
			}
			fmt.Fprintf(body, "%sbreak;\n", indent)
		case checker.ContinueStatement:
			if state.loopDepth == 0 {
				return unknownExpressionDiagnostic("checked continue outside a while loop")
			}
			writeLineDirective(body, statement.SourceLine)
			if err := unwindToLoopDepth(body, state, indent, "false"); err != nil {
				return err
			}
			fmt.Fprintf(body, "%scontinue;\n", indent)
		case checker.DeferStatement:
			writeLineDirective(body, statement.SourceLine)
			if err := writeDeferStatement(body, statement, state, indent); err != nil {
				return err
			}
		case checker.ErrdeferStatement:
			// RFC 0029: errdefer registers exactly like defer; the Err flag
			// decides at the exit edge whether the action runs.
			writeLineDirective(body, statement.SourceLine)
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
	case checker.CallExpression, checker.MethodCallExpression, checker.StringMethodCallExpression, checker.CollectionMethodCallExpression, checker.ListNewExpression, checker.DictNewExpression, checker.StreamMethodCallExpression, checker.StreamConstructorExpression,
		checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression,
		checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression,
		checker.MutexConstructorExpression, checker.MutexMethodCallExpression,
		checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression,
		checker.VolatileWriteExpression, checker.FileMethodCallExpression:
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

// declaredFunctions collects every module-level function so a body can name
// itself and the callees around it. Declaration order is the checker's rule,
// not the generator's, so the whole table is visible everywhere here.
func declaredFunctions(program checker.Program) (map[string]compilerTypes.Type, error) {
	functions := make(map[string]compilerTypes.Type)
	for _, statement := range program.Statements {
		declared, ok := statement.(checker.FunctionDeclaration)
		if !ok {
			continue
		}
		if !validSourceName(declared.Name) {
			return nil, unknownExpressionDiagnostic("invalid checked function declaration name")
		}
		if _, exists := functions[declared.Name]; exists {
			return nil, unknownExpressionDiagnostic("duplicate checked function declaration name")
		}
		if declared.Type.Signature == nil {
			return nil, unknownExpressionDiagnostic("function declaration without a checked Fun type")
		}
		functions[declared.Name] = declared.Type
	}
	for _, declared := range program.SpecializedFunctions {
		if !validSourceName(declared.Name) {
			return nil, unknownExpressionDiagnostic("invalid checked function declaration name")
		}
		if _, exists := functions[declared.Name]; exists {
			return nil, unknownExpressionDiagnostic("duplicate checked function declaration name")
		}
		if declared.Type.Signature == nil {
			return nil, unknownExpressionDiagnostic("function declaration without a checked Fun type")
		}
		functions[declared.Name] = declared.Type
	}
	return functions, nil
}

// declaredMethods collects checked methods by the same source-derived stem
// used by the checker for C-name collision diagnostics. Method calls carry the
// owning nominal object, so this table lets generation validate that a call
// refers to a checked method with the expected receiver and signature.
func declaredMethods(program checker.Program) (map[string]checker.MethodDeclaration, error) {
	methods := make(map[string]checker.MethodDeclaration)
	for _, statement := range program.Statements {
		declared, ok := statement.(checker.MethodDeclaration)
		if !ok {
			continue
		}
		if declared.Object == nil || !validSourceName(compilerTypes.SanitizeIdentifier(declared.Object.Name)) || !validSourceName(declared.Name) {
			return nil, unknownExpressionDiagnostic("invalid checked method declaration name")
		}
		key := methodKey(declared.Object, declared.Name)
		if _, exists := methods[key]; exists {
			return nil, unknownExpressionDiagnostic("duplicate checked method declaration")
		}
		methods[key] = declared
	}
	for _, declared := range program.SpecializedMethods {
		if declared.Object == nil || !validSourceName(compilerTypes.SanitizeIdentifier(declared.Object.Name)) || !validSourceName(declared.Name) {
			return nil, unknownExpressionDiagnostic("invalid checked method declaration name")
		}
		key := methodKey(declared.Object, declared.Name)
		if _, exists := methods[key]; exists {
			return nil, unknownExpressionDiagnostic("duplicate checked method declaration")
		}
		methods[key] = declared
	}
	return methods, nil
}

func methodKey(object *compilerTypes.ObjectType, name string) string {
	if object == nil {
		return ""
	}
	return compilerTypes.SanitizeIdentifier(object.Name) + "_" + name
}

func methodCName(object *compilerTypes.ObjectType, name string) string {
	return PrivateCName(FunctionName, methodKey(object, name))
}

// writeFunctionDefinition emits one static C function. Parameters are fixed
// bindings, so their declarators carry top-level const.
func writeFunctionDefinition(body *strings.Builder, declared checker.FunctionDeclaration, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, typeState *generatedTypeValidation, stringState *generatedStringState) error {
	signature := declared.Type.Signature
	if signature == nil || !validateGeneratedType(declared.Type, typeState, false) {
		return unknownExpressionDiagnostic("function declaration without a checked Fun type")
	}
	if len(signature.Parameters) != len(declared.Parameters) {
		return unknownExpressionDiagnostic("function declaration parameter count does not match its checked type")
	}
	resultSpelling := "void"
	if declared.Result != nil {
		if declared.Result.Signature != nil {
			return unknownExpressionDiagnostic("Fun function results are not supported")
		}
		if !validateGeneratedType(*declared.Result, typeState, false) {
			return unknownExpressionDiagnostic("unsupported checked function result type")
		}
		if signature.Result == nil || !compilerTypes.Equal(*signature.Result, *declared.Result) {
			return unknownExpressionDiagnostic("function result does not match its checked type")
		}
		resultSpelling = typeSpelling(*declared.Result)
	} else if signature.Result != nil {
		return unknownExpressionDiagnostic("function result does not match its checked type")
	}
	if declared.Result != nil && checker.FallsThrough(declared.Body) {
		return unknownExpressionDiagnostic("checked returning function may fall through without returning")
	}

	state := &expressionValidation{
		variables:      make(map[string]generatedBinding, len(declared.Parameters)),
		bindings:       make(map[checker.BindingID]generatedBinding, len(declared.Parameters)),
		bindingNames:   make(map[checker.BindingID]string, len(declared.Parameters)),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
	}
	state.pushScope()
	parameters := make([]string, len(declared.Parameters))
	for index, parameter := range declared.Parameters {
		if !validSourceName(parameter.Name) {
			return unknownExpressionDiagnostic("invalid checked function parameter name")
		}
		if !validateGeneratedType(parameter.Type, typeState, false) {
			return unknownExpressionDiagnostic("unsupported checked function parameter type")
		}
		if !compilerTypes.Equal(signature.Parameters[index], parameter.Type) {
			return unknownExpressionDiagnostic("function parameter does not match its checked type")
		}
		name, nameErr := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false)
		if nameErr != nil {
			return nameErr
		}
		parameters[index] = declaration(parameter.Type, name, false)
	}

	writeLineDirective(body, declared.SourceLine)
	fmt.Fprintf(body, "static %s %s(%s) {\n", resultSpelling, PrivateCName(FunctionName, declared.Name), parameterList(parameters))
	if err := writeStatements(body, declared.Body, state, declared.Result, true, declared.Defers); err != nil {
		return err
	}
	body.WriteString("}\n\n")
	return nil
}

// writeMethodDefinition emits a checked impl method as a file-scope C
// function. The implicit receiver is the first fixed parameter; its written
// receiver type determines whether C receives a structure copy, a read-only
// pointer, or a writable pointer.
func writeMethodDefinition(body *strings.Builder, declared checker.MethodDeclaration, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration, typeState *generatedTypeValidation, stringState *generatedStringState) error {
	if declared.Object == nil || declared.SelfBinding == 0 || !validSourceName(declared.Name) {
		return unknownExpressionDiagnostic("method declaration is missing checked receiver metadata")
	}
	if !validateGeneratedType(declared.SelfType, typeState, false) {
		return unknownExpressionDiagnostic("unsupported checked method receiver type")
	}
	if declared.SelfType.Object != declared.Object && (declared.SelfType.Element == nil || declared.SelfType.Element.Object != declared.Object) {
		return unknownExpressionDiagnostic("method receiver does not match its checked owner")
	}
	resultSpelling := "void"
	if declared.Result != nil {
		if declared.Result.Signature != nil || !validateGeneratedType(*declared.Result, typeState, false) {
			return unknownExpressionDiagnostic("unsupported checked method result type")
		}
		resultSpelling = typeSpelling(*declared.Result)
	}
	if declared.Result != nil && checker.FallsThrough(declared.Body) {
		return unknownExpressionDiagnostic("checked returning method may fall through without returning")
	}

	state := &expressionValidation{
		variables:      make(map[string]generatedBinding, len(declared.Parameters)+1),
		bindings:       make(map[checker.BindingID]generatedBinding, len(declared.Parameters)+1),
		bindingNames:   make(map[checker.BindingID]string, len(declared.Parameters)+1),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
		strings:        stringState,
	}
	state.pushScope()
	selfName, selfErr := state.allocateBinding(declared.SelfBinding, "self", declared.SelfType, false)
	if selfErr != nil {
		return selfErr
	}
	parameters := []string{declaration(declared.SelfType, selfName, false)}
	for _, parameter := range declared.Parameters {
		if !validSourceName(parameter.Name) || parameter.Binding == 0 || !validateGeneratedType(parameter.Type, typeState, false) {
			return unknownExpressionDiagnostic("invalid checked method parameter")
		}
		name, nameErr := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false)
		if nameErr != nil {
			return nameErr
		}
		parameters = append(parameters, declaration(parameter.Type, name, false))
	}

	writeLineDirective(body, declared.SourceLine)
	fmt.Fprintf(body, "static %s %s(%s) {\n", resultSpelling, methodCName(declared.Object, declared.Name), parameterList(parameters))
	if err := writeStatements(body, declared.Body, state, declared.Result, true, declared.Defers); err != nil {
		return err
	}
	body.WriteString("}\n\n")
	return nil
}

func parameterList(parameters []string) string {
	if len(parameters) == 0 {
		return "void"
	}
	return strings.Join(parameters, ", ")
}

func validateCheckedProgram(program checker.Program, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration) error {
	typeState := &generatedTypeValidation{declaredObjects: errorDeclaredObjects(program)}
	state := &expressionValidation{
		variables:      make(map[string]generatedBinding),
		bindings:       make(map[checker.BindingID]generatedBinding),
		bindingNames:   make(map[checker.BindingID]string),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
	}
	state.pushScope()
	for _, typeDeclaration := range program.TypeDeclarations {
		if !validSourceName(typeDeclaration.Name) {
			return unknownExpressionDiagnostic("invalid checked type declaration name")
		}
		if !validateGeneratedType(typeDeclaration.Type, typeState, false) {
			return unknownExpressionDiagnostic("unsupported checked type declaration")
		}
	}
	if err := validateStatements(program.Statements, state, typeState); err != nil {
		return err
	}
	for _, function := range program.SpecializedFunctions {
		if err := validateFunctionDeclaration(function, typeState, functions, methods); err != nil {
			return err
		}
	}
	for _, method := range program.SpecializedMethods {
		if err := validateMethodDeclaration(method, typeState, functions, methods); err != nil {
			return err
		}
	}
	return nil
}

// validateFunctionDeclaration validates one concrete function declaration and
// its body without mutating the main statement state.
func validateFunctionDeclaration(declared checker.FunctionDeclaration, typeState *generatedTypeValidation, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration) error {
	if !validSourceName(declared.Name) || declared.Type.Signature == nil || !validateGeneratedType(declared.Type, typeState, false) {
		return unknownExpressionDiagnostic("unsupported checked specialized function")
	}
	state := &expressionValidation{
		variables:      make(map[string]generatedBinding, len(declared.Parameters)),
		bindings:       make(map[checker.BindingID]generatedBinding, len(declared.Parameters)),
		bindingNames:   make(map[checker.BindingID]string, len(declared.Parameters)),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
	}
	state.pushScope()
	for _, parameter := range declared.Parameters {
		if _, err := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false); err != nil {
			return err
		}
	}
	return validateStatements(declared.Body, state, typeState)
}

// validateMethodDeclaration validates one concrete method declaration and its
// body.
func validateMethodDeclaration(declared checker.MethodDeclaration, typeState *generatedTypeValidation, functions map[string]compilerTypes.Type, methods map[string]checker.MethodDeclaration) error {
	if declared.Object == nil || !validSourceName(declared.Name) || !validateGeneratedType(declared.SelfType, typeState, false) {
		return unknownExpressionDiagnostic("unsupported checked specialized method")
	}
	state := &expressionValidation{
		variables:      make(map[string]generatedBinding, len(declared.Parameters)),
		bindings:       make(map[checker.BindingID]generatedBinding, len(declared.Parameters)),
		bindingNames:   make(map[checker.BindingID]string, len(declared.Parameters)),
		usedNames:      make(map[string]bool),
		functions:      functions,
		methods:        methods,
		generatedTypes: typeState,
	}
	state.pushScope()
	if _, err := state.allocateBinding(declared.SelfBinding, "self", declared.SelfType, false); err != nil {
		return err
	}
	for _, parameter := range declared.Parameters {
		if _, err := state.allocateBinding(parameter.Binding, parameter.Name, parameter.Type, false); err != nil {
			return err
		}
	}
	return validateStatements(declared.Body, state, typeState)
}

func validateStatements(statements []checker.Statement, state *expressionValidation, typeState *generatedTypeValidation) error {
	if len(state.activeScopes) == 0 {
		state.pushScope()
		defer state.popScope()
	}
	for _, statement := range statements {
		switch statement := statement.(type) {
		case checker.Declaration:
			if !validSourceName(statement.Name) || !validateGeneratedType(statement.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported checked declaration")
			}
			if statement.Binding == 0 {
				if _, exists := state.variables[statement.Name]; exists {
					return unknownExpressionDiagnostic("duplicate checked declaration name")
				}
			}
			if err := validateCheckedOperandWithState(statement.Source, state); err != nil {
				return err
			}
			if !generatedAssignable(statement.Type, statement.Source.Type) {
				return unknownExpressionDiagnostic("declaration source type does not match its checked type")
			}
			if _, err := state.allocateBinding(statement.Binding, statement.Name, statement.Type, statement.Mutable); err != nil {
				return err
			}
		case checker.Assignment:
			if !validSourceName(statement.Name) || !validateGeneratedType(statement.Type, typeState, false) || !validateGeneratedType(statement.Target.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported checked assignment")
			}
			if err := validateCheckedOperandWithState(statement.Target, state); err != nil {
				return err
			}
			targetPlace, err := checkedPlaceMetadata(statement.Target.Node, state)
			if err != nil {
				return err
			}
			if !targetPlace.addressable || !targetPlace.writable {
				return unknownExpressionDiagnostic("assignment target is not an addressable writable place")
			}
			if err := validateCheckedOperandWithState(statement.Source, state); err != nil {
				return err
			}
			// The target names the declared storage slot, so its checked type
			// is that slot's type exactly, except that RFC 0010 branch
			// narrowing may present it as the non-Nil member or as Nil.
			targetMatches := compilerTypes.Equal(statement.Type, statement.Target.Type)
			if !targetMatches {
				if base, nullable := compilerTypes.NullableBase(statement.Type); !nullable ||
					!compilerTypes.Equal(base, statement.Target.Type) && !compilerTypes.IsNil(statement.Target.Type) {
					return unknownExpressionDiagnostic("assignment target type does not match its checked type")
				}
			}
			if !generatedAssignable(statement.Type, statement.Source.Type) {
				return unknownExpressionDiagnostic("assignment operand type does not match its checked type")
			}
		case checker.CallStatement:
			if statement.Call.Node.Kind == checker.PrintExpression {
				// RFC 0030: print validates its arguments and produces no
				// value; the statement renderer emits its own statements.
				return nil
			}
			if _, err := renderCallStatement(statement, state); err != nil {
				return err
			}
		case checker.DeferStatement:
			if statement.Action.IsCall {
				if statement.Action.Call == nil {
					return unknownExpressionDiagnostic("deferred call action without a checked call")
				}
				if statement.Action.Call.Type == (compilerTypes.Type{}) {
					// A no-result call such as Heap.free validates its node
					// directly; it has no value type to check.
					return validateExpressionNode(statement.Action.Call.Node, compilerTypes.Type{}, false, state)
				}
				if err := validateCheckedOperandWithState(*statement.Action.Call, state); err != nil {
					return err
				}
			} else if statement.Action.Value != nil {
				if err := validateCheckedOperandWithState(*statement.Action.Value, state); err != nil {
					return err
				}
			}
		case checker.ReturnStatement:
			// Function return signatures are checked while rendering their
			// definitions; the preflight pass only validates the value shape.
			if statement.Value != nil {
				if err := validateCheckedOperandWithState(*statement.Value, state); err != nil {
					return err
				}
			}
		case checker.IfStatement:
			if err := validateCondition(statement.Condition, state); err != nil {
				return err
			}
			state.pushScope()
			if err := validateStatements(statement.Then, state, typeState); err != nil {
				return err
			}
			state.popScope()
			for _, branch := range statement.ElseIf {
				if err := validateCondition(branch.Condition, state); err != nil {
					return err
				}
				state.pushScope()
				if err := validateStatements(branch.Body, state, typeState); err != nil {
					return err
				}
				state.popScope()
			}
			if statement.Else != nil {
				state.pushScope()
				if err := validateStatements(statement.Else, state, typeState); err != nil {
					return err
				}
				state.popScope()
			}
		case checker.WhileStatement:
			if err := validateCondition(statement.Condition, state); err != nil {
				return err
			}
			state.pushScope()
			previousLoopDepth := state.loopDepth
			state.loopDepth++
			err := validateStatements(statement.Body, state, typeState)
			state.loopDepth = previousLoopDepth
			state.popScope()
			if err != nil {
				return err
			}
		case checker.BreakStatement:
			if state.loopDepth == 0 {
				return unknownExpressionDiagnostic("checked break outside a while loop")
			}
		case checker.ContinueStatement:
			if state.loopDepth == 0 {
				return unknownExpressionDiagnostic("checked continue outside a while loop")
			}
		case checker.FunctionDeclaration:
			if len(state.activeScopes) > 1 {
				return unknownExpressionDiagnostic("function declaration inside a module-level control-flow block")
			}
			continue
		case checker.MethodDeclaration:
			if len(state.activeScopes) > 1 {
				return unknownExpressionDiagnostic("method declaration inside a module-level control-flow block")
			}
			continue
		default:
			return unknownExpressionDiagnostic("unsupported checked statement")
		}
	}
	return nil
}

func validateCondition(condition checker.Operand, state *expressionValidation) error {
	// RFC 0023: nil is always falsey and needs no further validation (the
	// nil literal's other generator paths fail closed by RFC 0010).
	switch compilerTypes.Truthiness(condition.Type) {
	case compilerTypes.TruthinessNil:
		return nil
	case compilerTypes.TruthinessInvalid:
		return unknownExpressionDiagnostic("cannot determine the truthiness of a checked control-flow condition")
	}
	return validateCheckedOperandWithState(condition, state)
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
		return "(" + rendered + ", true)", nil
	default:
		return "", unknownExpressionDiagnostic("unsupported operand in a truthiness context")
	}
}

func supportedGeneratedType(typ compilerTypes.Type) bool {
	return validateGeneratedType(typ, &generatedTypeValidation{}, false)
}

type generatedTypeValidation struct {
	activeObjects   map[*compilerTypes.ObjectType]bool
	validObjects    map[*compilerTypes.ObjectType]bool
	declaredObjects map[*compilerTypes.ObjectType]bool
}

// IsCanonical owns identity and recursive type metadata. This pass keeps only
// generator-specific source-name and declaration checks.
func validateGeneratedType(typ compilerTypes.Type, state *generatedTypeValidation, throughPointer bool) bool {
	if !compilerTypes.IsCanonical(typ) {
		// Unknown is canonical only behind a pointer layer, exactly as the
		// type environment interning rule states: Ptr<Unknown> and
		// MutPtr<Unknown> are the erased object pointer types.
		if compilerTypes.IsUnknown(typ) {
			return throughPointer
		}
		return false
	}
	if typ.Signature != nil {
		// A Fun result would have to wrap a second declarator around the name,
		// which RFC 0008 defers, so it fails closed here.
		if typ.Signature.Result != nil && (typ.Signature.Result.Signature != nil || !validateGeneratedType(*typ.Signature.Result, state, false)) {
			return false
		}
		for _, parameter := range typ.Signature.Parameters {
			if !validateGeneratedType(parameter, state, false) {
				return false
			}
		}
		return true
	}
	if typ.Union != nil {
		if len(typ.Union.Members) < 2 || typ.CName == "" {
			return false
		}
		for _, member := range typ.Union.Members {
			if !validateGeneratedType(member, state, false) {
				return false
			}
		}
		return true
	}
	if typ.Element != nil {
		return validateGeneratedType(*typ.Element, state, true)
	}
	if typ.Array != nil {
		return validateGeneratedType(typ.Array.Element, state, false)
	}
	if typ.View != nil {
		return validateGeneratedType(typ.View.Element, state, false)
	}
	if typ.List != nil {
		return validateGeneratedType(typ.List.Element, state, false)
	}
	if typ.Dict != nil {
		return validateGeneratedType(typ.Dict.Key, state, false) && validateGeneratedType(typ.Dict.Value, state, false)
	}
	if typ.Stream != nil {
		return validateGeneratedType(typ.Stream.Element, state, false)
	}
	if compilerTypes.IsEoS(typ) {
		return true
	}
	if typ.Object == nil {
		return true
	}
	object := typ.Object
	if state.declaredObjects != nil && !state.declaredObjects[object] || !validSourceName(compilerTypes.SanitizeIdentifier(object.Name)) || object.CName != "hex_t_"+compilerTypes.SanitizeIdentifier(object.Name) {
		return false
	}
	if state.activeObjects == nil {
		state.activeObjects = make(map[*compilerTypes.ObjectType]bool)
		state.validObjects = make(map[*compilerTypes.ObjectType]bool)
	}
	if state.validObjects[object] {
		return true
	}
	if state.activeObjects[object] {
		return throughPointer
	}
	if len(object.Members) == 0 {
		return false
	}
	state.activeObjects[object] = true
	seenNames := make(map[string]bool, len(object.Members))
	for _, member := range object.Members {
		if !validSourceName(member.Name) || seenNames[member.Name] || !validateGeneratedType(member.Type, state, false) {
			delete(state.activeObjects, object)
			return false
		}
		seenNames[member.Name] = true
	}
	delete(state.activeObjects, object)
	state.validObjects[object] = true
	return true
}

func supportedGeneratedScalarType(typ compilerTypes.Type) bool {
	return typ.Element == nil && typ.Object == nil && typ.Signature == nil && compilerTypes.IsCanonical(typ)
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
		return PrivateCName(ValueName, sourceName), nil
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
	base := PrivateCName(ValueName, sourceName)
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
	return PrivateCName(ValueName, node.Name), true
}

type generatedPlace struct {
	typ         compilerTypes.Type
	addressable bool
	writable    bool
}

func declaredObjects(program checker.Program) map[*compilerTypes.ObjectType]bool {
	objects := make(map[*compilerTypes.ObjectType]bool)
	for _, declaration := range program.TypeDeclarations {
		if declaration.Type.Object != nil {
			objects[declaration.Type.Object] = true
		}
	}
	return objects
}

// errorDeclaredObjects augments the declared object table with the built-in
// Error object when the program references it (RFC 0029).
func errorDeclaredObjects(program checker.Program) map[*compilerTypes.ObjectType]bool {
	objects := declaredObjects(program)
	if discoverErrorUsed(program) {
		objects[compilerTypes.ErrorType.Object] = true
	}
	return objects
}

func supportedGeneratedTypeWithState(typ compilerTypes.Type, state *expressionValidation) bool {
	if state != nil && state.generatedTypes != nil {
		return validateGeneratedType(typ, state.generatedTypes, false)
	}
	return supportedGeneratedType(typ)
}

func validateCheckedOperand(source checker.Operand) error {
	return validateCheckedOperandWithState(source, &expressionValidation{})
}

func validateConstantOperand(source checker.Operand) error {
	// RFC 0029: object constants (Error.new results wrapped by union
	// injection) validate their object value.
	if source.Object != nil {
		return validateObjectValue(source.Object, &expressionValidation{})
	}
	// Nil is the singleton type: its one value is nullptr and it carries no
	// go/constant, so it is validated before the constant value is required.
	if compilerTypes.IsNil(source.Type) {
		return nil
	}
	// EoS is the RFC 0031 singleton: its one value is a tag-only marker and
	// carries no go/constant, like Nil.
	if compilerTypes.IsEoS(source.Type) {
		return nil
	}
	// Heap is a singleton handle: Heap.new() carries no go/constant.
	if compilerTypes.IsHeap(source.Type) {
		return nil
	}
	if source.Constant == nil {
		return unknownExpressionDiagnostic("constant operand without a checked value")
	}
	switch source.Type.ScalarKind {
	case compilerTypes.ScalarBool:
		if !compilerTypes.Equal(source.Type, compilerTypes.Bool) || source.Constant.Kind() != constant.Bool {
			return unknownExpressionDiagnostic("invalid checked Bool constant")
		}
	case compilerTypes.ScalarUnsignedInteger, compilerTypes.ScalarSignedInteger:
		if !supportedGeneratedScalarType(source.Type) || source.Constant.Kind() != constant.Int {
			return unknownExpressionDiagnostic("invalid checked integer constant")
		}
		if _, err := integerLiteral(source); err != nil {
			return err
		}
	case compilerTypes.ScalarFloat:
		return validateFloatConstant(source)
	default:
		return unknownExpressionDiagnostic("unsupported checked constant type")
	}
	return nil
}

func validateFloatConstant(source checker.Operand) error {
	bitSize := 64
	if compilerTypes.Equal(source.Type, compilerTypes.Float32) {
		bitSize = 32
	} else if !compilerTypes.Equal(source.Type, compilerTypes.Float64) {
		return unknownExpressionDiagnostic("invalid checked float constant")
	}
	if bitSize == 32 && source.FloatBits > math.MaxUint32 {
		return unknownExpressionDiagnostic("Float32 constant has bits outside its declared width")
	}

	bits := source.FloatBits
	if bitSize == 32 {
		bits = uint64(uint32(bits))
	}
	signBit, special := floatSignAndSpecial(bits, bitSize)
	if source.Negative != signBit {
		return unknownExpressionDiagnostic("float sign metadata does not match its checked value")
	}
	if source.Constant == nil {
		return unknownExpressionDiagnostic("float constant without a checked value")
	}

	if special {
		if source.Constant.Kind() != constant.Unknown || source.Literal != "" {
			return unknownExpressionDiagnostic("special float constant has malformed metadata")
		}
		return nil
	}
	if source.Constant.Kind() != constant.Int && source.Constant.Kind() != constant.Float {
		return unknownExpressionDiagnostic("float constant is not numeric")
	}

	if source.Literal != "" {
		literal := strings.ReplaceAll(source.Literal, "_", "")
		if strings.HasPrefix(literal, "+") || strings.HasPrefix(literal, "-") {
			return unknownExpressionDiagnostic("float literal sign is stored in malformed metadata")
		}
		literalValue := constant.MakeFromLiteral(literal, gotoken.FLOAT, 0)
		if literalValue == nil || literalValue.Kind() == constant.Unknown || (literalValue.Kind() != constant.Int && literalValue.Kind() != constant.Float) || constant.Sign(source.Constant) < 0 || !constant.Compare(source.Constant, gotoken.EQL, literalValue) {
			return unknownExpressionDiagnostic("checked float literal does not match its value")
		}
		if floatBitsForConstant(literalValue, bitSize, source.Negative) != bits {
			return unknownExpressionDiagnostic("checked float literal does not match its rounded bits")
		}
		return nil
	}
	if floatBitsForConstant(source.Constant, bitSize, source.Negative) != bits {
		return unknownExpressionDiagnostic("checked float does not match its rounded bits")
	}
	valueSign := constant.Sign(source.Constant)
	if valueSign < 0 && !signBit || valueSign > 0 && signBit {
		return unknownExpressionDiagnostic("float sign metadata does not match its checked value")
	}
	return nil
}

func floatSignAndSpecial(bits uint64, bitSize int) (bool, bool) {
	if bitSize == 32 {
		value := uint32(bits)
		return value>>31 != 0, value&0x7f800000 == 0x7f800000
	}
	return bits>>63 != 0, bits&0x7ff0000000000000 == 0x7ff0000000000000
}

func floatBitsForConstant(value constant.Value, bitSize int, negative bool) uint64 {
	if bitSize == 32 {
		converted, _ := constant.Float32Val(value)
		bits := uint64(math.Float32bits(converted))
		if negative {
			bits |= uint64(1) << 31
		}
		return bits
	}
	converted, _ := constant.Float64Val(value)
	bits := math.Float64bits(converted)
	if negative {
		bits |= uint64(1) << 63
	}
	return bits
}

func validateObjectValue(value *checker.ObjectValue, state *expressionValidation) error {
	if value == nil || value.Type.Object == nil || !supportedGeneratedTypeWithState(value.Type, state) {
		return unknownExpressionDiagnostic("object operand without a checked object value")
	}
	if state.objects == nil {
		state.objects = make(map[*checker.ObjectValue]bool)
	}
	if state.objects[value] {
		return unknownExpressionDiagnostic("cyclic checked object value")
	}
	state.objects[value] = true
	defer delete(state.objects, value)

	seen := make(map[*compilerTypes.ObjectMember]bool, len(value.Initializers))
	for _, initializer := range value.Initializers {
		if initializer.Member == nil {
			return unknownExpressionDiagnostic("object initializer without a checked member")
		}
		canonical, ok := objectMember(value.Type.Object, initializer.Member)
		if !ok || seen[initializer.Member] || !compilerTypes.Equal(canonical.Type, initializer.Member.Type) {
			return unknownExpressionDiagnostic("object initializer has a forged checked member")
		}
		seen[initializer.Member] = true
		if !generatedAssignable(canonical.Type, initializer.Source.Type) {
			return unknownExpressionDiagnostic("object initializer type does not match its checked member")
		}
		if err := validateCheckedOperandWithState(initializer.Source, state); err != nil {
			return err
		}
	}
	if len(seen) != len(value.Type.Object.Members) {
		return unknownExpressionDiagnostic("incomplete checked object value")
	}
	return nil
}

func objectMember(object *compilerTypes.ObjectType, member *compilerTypes.ObjectMember) (*compilerTypes.ObjectMember, bool) {
	if object == nil || member == nil {
		return nil, false
	}
	for index := range object.Members {
		if &object.Members[index] == member {
			return &object.Members[index], true
		}
	}
	return nil, false
}

// generatedAssignable re-validates the checker's assignment relation so the
// generator never accepts a program the checker rejected. It is the complete
// type-level relation: RFC 0007 weakening, RFC 0010 nullable injection (P or
// Nil into P | Nil) and the one-row Unknown erasure/recovery table.
func generatedAssignable(target, source compilerTypes.Type) bool {
	return compilerTypes.Assignable(target, source)
}

func validateCheckedOperandWithState(source checker.Operand, state *expressionValidation) error {
	if !supportedGeneratedTypeWithState(source.Type, state) {
		return unknownExpressionDiagnostic("operand has an unsupported checked type")
	}
	switch source.Kind {
	case checker.ObjectOperand:
		if source.Object == nil || !compilerTypes.Equal(source.Type, source.Object.Type) {
			return unknownExpressionDiagnostic("object operand has mismatched checked types")
		}
		return validateObjectValue(source.Object, state)
	case checker.VariableOperand, checker.ExpressionOperand:
		if err := validateExpressionNode(source.Node, source.Type, true, state); err != nil {
			return err
		}
		if expressionType, ok := expressionResultType(source.Node); ok && !compilerTypes.Equal(source.Type, expressionType) && !compilerTypes.WidensTo(expressionType, source.Type) {
			return unknownExpressionDiagnostic("operand expression type does not match its checked type")
		}
	case checker.ConstantOperand:
		return validateConstantOperand(source)
	default:
		return unknownExpressionDiagnostic("unsupported checked operand")
	}
	return nil
}

func validateExpressionNode(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if hasExpected && !supportedGeneratedTypeWithState(expected, state) {
		return unknownExpressionDiagnostic("expression has an unsupported expected type")
	}
	switch node.Kind {
	case checker.NilExpression:
		if !compilerTypes.IsNil(node.ResultType) || hasExpected && !compilerTypes.IsNil(expected) {
			return unknownExpressionDiagnostic("nil expression has invalid checked metadata")
		}
		return nil
	case checker.EosExpression:
		if !compilerTypes.IsEoS(node.ResultType) || hasExpected && !compilerTypes.IsEoS(expected) {
			return unknownExpressionDiagnostic("eos expression has invalid checked metadata")
		}
		return nil
	case checker.VariableExpression:
		if !validSourceName(node.Name) {
			return unknownExpressionDiagnostic("variable without a source name")
		}
		if state != nil && (state.variables != nil || state.bindings != nil) {
			binding, ok := state.bindingFor(node)
			if !ok {
				return unknownExpressionDiagnostic("variable is not present in checked bindings")
			}
			if hasExpected && !compilerTypes.Equal(binding.typ, expected) {
				// RFC 0010: a null test narrows a local binding's reads to
				// its non-Nil base (or to Nil) inside the branch where the
				// test holds; the binding itself still holds the declared
				// nullable type, so a narrowed read is a stricter type.
				if !compilerTypes.Assignable(binding.typ, expected) {
					return unknownExpressionDiagnostic("variable type does not match its checked type")
				}
			}
			for _, metadataType := range []compilerTypes.Type{node.OperandType, node.ResultType} {
				if metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(binding.typ, metadataType) {
					return unknownExpressionDiagnostic("variable metadata does not match its checked binding")
				}
			}
		}
		return validateExpressionMetadata(node, expected, hasExpected, state)
	case checker.FunctionReferenceExpression:
		return validateFunctionReference(node, expected, hasExpected, state)
	case checker.CallExpression:
		return validateCallExpression(node, expected, hasExpected, state)
	case checker.MethodCallExpression:
		return validateMethodCallExpression(node, expected, hasExpected, state)
	case checker.AddressOfExpression:
		return validateAddressExpression(node, expected, hasExpected, state)
	case checker.DereferenceExpression:
		return validateDereferenceExpression(node, expected, hasExpected, state)
	case checker.MemberExpression:
		return validateMemberExpression(node, expected, hasExpected, state)
	case checker.ObjectExpression:
		if node.Object == nil {
			return unknownExpressionDiagnostic("object expression without a checked object value")
		}
		if err := validateObjectValue(node.Object, state); err != nil {
			return err
		}
		if hasExpected && !compilerTypes.Equal(expected, node.Object.Type) {
			return unknownExpressionDiagnostic("object expression type does not match its expected type")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, node.Object.Type) {
			return unknownExpressionDiagnostic("object expression result type does not match its checked object")
		}
		return validateExpressionMetadata(node, expected, hasExpected, state)
	case checker.ConstantExpression:
		if node.Constant == nil || node.Constant.Kind != checker.ConstantOperand && node.Constant.Kind != checker.ObjectOperand ||
			!compilerTypes.Equal(node.ResultType, node.Constant.Type) ||
			!supportedGeneratedScalarType(node.ResultType) && node.Constant.Type.Object == nil && node.Constant.Type.Union == nil {
			detail := ""
			if node.Constant != nil {
				detail = fmt.Sprintf(" result=%s const=%s kind=%d literal=%q object=%v union=%v equal=%v", node.ResultType.Name, node.Constant.Type.Name, node.Constant.Kind, node.Constant.Literal, node.Constant.Type.Object != nil, node.Constant.Type.Union != nil, compilerTypes.Equal(node.ResultType, node.Constant.Type))
			}
			return unknownExpressionDiagnostic("constant expression without a checked constant" + detail)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("constant expression type does not match its expected type")
		}
		return validateConstantOperand(*node.Constant)
	case checker.UnaryOperationExpression:
		if node.Operand == nil {
			return unknownExpressionDiagnostic("unary operation with invalid checked metadata")
		}
		if node.Operator == checker.LogicalNotOperator {
			// RFC 0023: not accepts any value-producing operand; the
			// operand is validated through its truthiness.
			if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
				return unknownExpressionDiagnostic("logical not requires a truthy-compatible operand and a Bool result")
			}
			if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
				return unknownExpressionDiagnostic("unary operation result type does not match its expected type")
			}
			return validateTruthinessChild(node.Operand, state)
		}
		if !supportedGeneratedScalarType(node.OperandType) || !supportedGeneratedScalarType(node.ResultType) {
			return unknownExpressionDiagnostic("unary operation with invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("unary operation result type does not match its expected type")
		}
		if err := validateUnaryMetadata(node); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.BinaryOperationExpression:
		if node.Left == nil || node.Right == nil {
			return unknownExpressionDiagnostic("binary operation with invalid checked metadata")
		}
		if node.Operator == checker.LogicalAndOperator || node.Operator == checker.LogicalOrOperator {
			// RFC 0023: and/or accept any value-producing operands, mixed
			// types included; each side is validated through its truthiness.
			if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
				return unknownExpressionDiagnostic("logical operation requires a truthy-compatible operand and a Bool result")
			}
			if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
				return unknownExpressionDiagnostic("binary operation result type does not match its expected type")
			}
			if err := validateTruthinessChild(node.Left, state); err != nil {
				return err
			}
			return validateTruthinessChild(node.Right, state)
		}
		if !supportedGeneratedScalarType(node.OperandType) && node.OperandType.Element == nil || !supportedGeneratedScalarType(node.ResultType) {
			return unknownExpressionDiagnostic("binary operation with invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("binary operation result type does not match its expected type")
		}
		if err := validateBinaryMetadata(node); err != nil {
			return err
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Right, node.OperandType, state)
	case checker.NullTestExpression:
		// RFC 0010: == nil and != nil test a nullable operand's active member.
		// The operand carries the pre-test nullable type; the result is Bool.
		if node.Operand == nil {
			return unknownExpressionDiagnostic("null test without a checked operand")
		}
		if node.OperandType == (compilerTypes.Type{}) || !compilerTypes.IsUnion(node.OperandType) || !compilerTypes.ContainsUnionMember(node.OperandType, compilerTypes.Nil) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("null test has an invalid nullable operand type")
		}
		if node.Operator != checker.EqualOperator && node.Operator != checker.NotEqualOperator {
			return unknownExpressionDiagnostic("null test has an invalid operator")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("null test result type is not Bool")
		}
		if hasExpected && !compilerTypes.Equal(expected, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("null test result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.UnionInjectionExpression:
		return validateUnionInjection(node, expected, hasExpected, state)
	case checker.UnionWidenExpression:
		return validateUnionWiden(node, expected, hasExpected, state)
	case checker.UnionTestExpression:
		return validateUnionTest(node, expected, hasExpected, state)
	case checker.UnionPayloadExpression:
		return validateUnionPayload(node, expected, hasExpected, state)
	case checker.UnionEqualityExpression:
		return validateUnionEquality(node, expected, hasExpected, state)
	case checker.HeapAllocateExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.Element == (compilerTypes.Type{}) || !compilerTypes.IsCompleteValue(node.Element) || node.Element.Signature != nil || !supportedGeneratedTypeWithState(node.ResultType, state) || node.ResultType.Element == nil || !compilerTypes.Equal(*node.ResultType.Element, node.Element) {
			return unknownExpressionDiagnostic("heap allocation has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("heap allocation result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.HeapFreeExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("heap free has invalid checked metadata")
		}
		if node.Arguments[0].Type.Element == nil {
			return unknownExpressionDiagnostic("heap free operand is not a pointer")
		}
		if hasExpected {
			return unknownExpressionDiagnostic("heap free produces no value")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.AdtConstructExpression:
		adt := node.ResultType.Adt
		if adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("ADT construction has invalid checked metadata")
		}
		variant := &adt.Variants[node.VariantIndex]
		if len(node.Arguments) != len(variant.Payload) {
			return unknownExpressionDiagnostic("ADT construction payload count does not match its variant")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("ADT construction result does not match its expected type")
		}
		for index, member := range variant.Payload {
			if err := validateCheckedOperandWithState(node.Arguments[index], state); err != nil {
				return err
			}
			if !generatedAssignable(member.Type, node.Arguments[index].Type) {
				return unknownExpressionDiagnostic("ADT construction payload does not match its variant field")
			}
		}
		return nil
	case checker.AdtPayloadExpression:
		adt := node.OperandType.Adt
		if node.Operand == nil || adt == nil || node.VariantIndex < 0 || node.VariantIndex >= len(adt.Variants) || node.MemberIndex < 0 || node.MemberIndex >= len(adt.Variants[node.VariantIndex].Payload) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("ADT payload read has invalid checked metadata")
		}
		member := &adt.Variants[node.VariantIndex].Payload[node.MemberIndex]
		if !compilerTypes.Equal(node.ResultType, member.Type) || hasExpected && !compilerTypes.Equal(expected, member.Type) {
			return unknownExpressionDiagnostic("ADT payload read result does not match its checked field")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.MatchExpression:
		if node.Operand == nil || node.ResultType == (compilerTypes.Type{}) || len(node.Arguments) != len(node.MemberMap) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("match expression has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("match result does not match its expected type")
		}
		for _, arm := range node.Arguments {
			if !generatedAssignable(node.ResultType, arm.Type) {
				return unknownExpressionDiagnostic("match arm does not match its checked result type")
			}
			if err := validateCheckedOperandWithState(arm, state); err != nil {
				return err
			}
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ArrayLiteralExpression:
		if node.ResultType.Array == nil || !compilerTypes.Equal(node.OperandType, node.ResultType.Array.Element) || len(node.Arguments) != int(node.ResultType.Array.Length) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("array literal has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("array literal type does not match its expected type")
		}
		for _, element := range node.Arguments {
			if err := validateCheckedOperandWithState(element, state); err != nil {
				return err
			}
			if !generatedAssignable(node.OperandType, element.Type) {
				return unknownExpressionDiagnostic("array literal element does not match its element type")
			}
		}
		return nil
	case checker.IndexExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.Array == nil && node.OperandType.View == nil && node.OperandType.List == nil && !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("index expression has invalid checked metadata")
		}
		var element compilerTypes.Type
		if node.OperandType.Array != nil {
			element = node.OperandType.Array.Element
		} else if node.OperandType.View != nil {
			element = node.OperandType.View.Element
		} else if node.OperandType.List != nil {
			element = node.OperandType.List.Element
		} else {
			element = compilerTypes.Rune
		}
		if !compilerTypes.Equal(node.ResultType, element) {
			return unknownExpressionDiagnostic("index expression has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("index expression type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.CollectionMethodCallExpression:
		if node.Operand == nil || node.OperandType.Array == nil && node.OperandType.View == nil && node.OperandType.List == nil && node.OperandType.Dict == nil || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("collection method call has invalid checked metadata")
		}
		element := node.Element
		if node.OperandType.Array != nil {
			element = node.OperandType.Array.Element
		} else if node.OperandType.View != nil {
			element = node.OperandType.View.Element
		} else if node.OperandType.List != nil {
			element = node.OperandType.List.Element
		} else {
			element = node.OperandType.Dict.Value
		}
		switch node.Name {
		case "length":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) && !compilerTypes.Equal(node.ResultType, compilerTypes.UInt64) {
				return unknownExpressionDiagnostic("collection length call has invalid checked metadata")
			}
		case "is_empty":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("collection is_empty call has invalid checked metadata")
			}
		case "at":
			if len(node.Arguments) != 1 || !compilerTypes.Equal(node.Element, element) || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic("collection at call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "push":
			if node.OperandType.List == nil || len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("list push call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "set":
			if node.OperandType.List == nil || len(node.Arguments) != 2 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("list set call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "clear":
			if node.OperandType.List == nil || len(node.Arguments) != 0 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("list clear call has invalid checked metadata")
			}
		case "pop":
			if node.OperandType.List == nil || len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, element) {
				return unknownExpressionDiagnostic("list pop call has invalid checked metadata")
			}
		case "free":
			if len(node.Arguments) != 1 || node.ResultType != (compilerTypes.Type{}) || node.OperandType.List == nil && node.OperandType.Dict == nil {
				return unknownExpressionDiagnostic("collection free call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
			if hasExpected {
				return unknownExpressionDiagnostic("collection free produces no value")
			}
		case "insert":
			if node.OperandType.Dict == nil || len(node.Arguments) != 2 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("dictionary insert call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "get", "remove":
			if node.OperandType.Dict == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, element) {
				return unknownExpressionDiagnostic("dictionary lookup call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "contains":
			if node.OperandType.Dict == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("dictionary contains call has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		default:
			return unknownExpressionDiagnostic("unknown collection method")
		}
		if node.Name != "free" && node.Name != "insert" && hasExpected && !compilerTypes.Equal(expected, node.ResultType) && !compilerTypes.Assignable(expected, node.ResultType) {
			return unknownExpressionDiagnostic("collection method result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.CollectionSliceExpression:
		if node.Operand == nil || len(node.Arguments) != 2 || node.ResultType.View == nil || !compilerTypes.Equal(node.ResultType.View.Element, node.Element) || node.OperandType.Array == nil && node.OperandType.View == nil && node.OperandType.List == nil || !supportedGeneratedTypeWithState(node.OperandType, state) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("collection slice has invalid checked metadata")
		}
		var element compilerTypes.Type
		if node.OperandType.Array != nil {
			element = node.OperandType.Array.Element
		} else if node.OperandType.View != nil {
			element = node.OperandType.View.Element
		} else {
			element = node.OperandType.List.Element
		}
		if !compilerTypes.Equal(node.Element, element) {
			return unknownExpressionDiagnostic("collection slice element does not match its receiver")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("collection slice result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.StringLiteralExpression:
		if !compilerTypes.IsString(node.ResultType) && !compilerTypes.IsStrand(node.ResultType) {
			return unknownExpressionDiagnostic("string literal has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("string literal type does not match its expected type")
		}
		return nil
	case checker.StringMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) || !supportedGeneratedTypeWithState(node.OperandType, state) {
			return unknownExpressionDiagnostic("string method call has invalid checked metadata")
		}
		strand := compilerTypes.IsStrand(node.OperandType)
		switch node.Name {
		case "length":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
				return unknownExpressionDiagnostic("text length call has invalid checked metadata")
			}
		case "is_empty":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("text is_empty call has invalid checked metadata")
			}
		case "at":
			if len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, compilerTypes.Rune) {
				return unknownExpressionDiagnostic("text at call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "bytes":
			if strand || len(node.Arguments) != 0 || node.ResultType.View == nil || !compilerTypes.Equal(node.Element, compilerTypes.UInt8) {
				return unknownExpressionDiagnostic("string bytes call has invalid checked metadata")
			}
		case "slice":
			if strand || len(node.Arguments) != 2 || node.ResultType.View == nil || !compilerTypes.Equal(node.Element, compilerTypes.UInt8) {
				return unknownExpressionDiagnostic("string slice call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "rune_cursor":
			if strand || len(node.Arguments) != 0 || !compilerTypes.IsRuneCursor(node.ResultType) {
				return unknownExpressionDiagnostic("string rune_cursor call has invalid checked metadata")
			}
		case "to_string":
			if len(node.Arguments) != 1 || !compilerTypes.IsString(node.ResultType) {
				return unknownExpressionDiagnostic("text to_string call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
		case "concat", "free":
			if strand {
				return unknownExpressionDiagnostic("strand has no " + node.Name + " method")
			}
			if len(node.Arguments) != 2 && node.Name == "concat" || len(node.Arguments) != 1 && node.Name == "free" {
				return unknownExpressionDiagnostic("string " + node.Name + " call has invalid checked metadata")
			}
			for _, argument := range node.Arguments {
				if err := validateCheckedOperandWithState(argument, state); err != nil {
					return err
				}
			}
			if node.Name == "free" {
				if node.ResultType != (compilerTypes.Type{}) {
					return unknownExpressionDiagnostic("string free call has invalid checked metadata")
				}
				if hasExpected {
					return unknownExpressionDiagnostic("string free produces no value")
				}
			} else if !compilerTypes.IsString(node.ResultType) {
				return unknownExpressionDiagnostic("string concat call has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown text method")
		}
		if node.Name != "free" && hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("text method result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.StringFromBytesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || !compilerTypes.IsHeap(node.OperandType) || !compilerTypes.IsString(node.ResultType) || node.Arguments[0].Type.View == nil {
			return unknownExpressionDiagnostic("String.from_bytes has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("String.from_bytes result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.StringFromRunesExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || !compilerTypes.IsHeap(node.OperandType) || !compilerTypes.IsString(node.ResultType) || node.Arguments[0].Type.View == nil {
			return unknownExpressionDiagnostic("String.from_runes has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("String.from_runes result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.FileModeLiteralExpression:
		if node.ResultType == (compilerTypes.Type{}) || !compilerTypes.IsFileMode(node.ResultType) {
			return unknownExpressionDiagnostic("FileMode literal has invalid checked metadata")
		}
		if _, ok := fileModeVariants[node.Name]; !ok {
			return unknownExpressionDiagnostic("unknown FileMode variant " + node.Name)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("FileMode literal type does not match its expected type")
		}
		return nil
	case checker.FileOpenExpression:
		if len(node.Arguments) != 2 || !compilerTypes.IsFile(node.OperandType) || !compilerTypes.Equal(node.Element, compilerTypes.StringType) || node.ResultType.Union == nil || node.SourceLine <= 0 {
			return unknownExpressionDiagnostic("File.open has invalid checked metadata")
		}
		if unionMemberIndex(node.ResultType, compilerTypes.FileType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnostic("File.open result union is missing its File or Error member")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("File.open result type does not match its expected type")
		}
		if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[1], state)
	case checker.StdioCallExpression:
		if !compilerTypes.IsFile(node.ResultType) {
			return unknownExpressionDiagnostic("Stdio call has invalid checked metadata")
		}
		switch node.Name {
		case "stdin", "stdout", "stderr":
		default:
			return unknownExpressionDiagnostic("unknown Stdio operation " + node.Name)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("Stdio call result type does not match its expected type")
		}
		return nil
	case checker.FileMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsFile(node.OperandType) {
			return unknownExpressionDiagnostic("file method has invalid checked metadata")
		}
		switch node.Name {
		case "read_bytes", "read_text":
			if len(node.Arguments) != 1 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 || node.SourceLine <= 0 {
				return unknownExpressionDiagnostic("file read has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "write":
			if len(node.Arguments) != 1 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.Nil) < 0 || node.SourceLine <= 0 {
				return unknownExpressionDiagnostic("file write has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "write_text":
			if len(node.Arguments) != 1 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.Nil) < 0 || node.SourceLine <= 0 {
				return unknownExpressionDiagnostic("file write_text has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
		case "flush":
			if len(node.Arguments) != 0 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.Nil) < 0 || node.SourceLine <= 0 {
				return unknownExpressionDiagnostic("file flush has invalid checked metadata")
			}
		case "close":
			if len(node.Arguments) != 0 || node.ResultType != (compilerTypes.Type{}) {
				return unknownExpressionDiagnostic("file close has invalid checked metadata")
			}
			if hasExpected {
				return unknownExpressionDiagnostic("file close produces no value")
			}
		default:
			return unknownExpressionDiagnostic("unknown file method " + node.Name)
		}
		if node.Name != "close" && hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("file method result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.RuneCursorMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsRuneCursor(node.OperandType) {
			return unknownExpressionDiagnostic("rune cursor method has invalid checked metadata")
		}
		switch node.Name {
		case "has_next":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("rune cursor has_next has invalid checked metadata")
			}
		case "next":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Rune) {
				return unknownExpressionDiagnostic("rune cursor next has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown rune cursor method " + node.Name)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("rune cursor method result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ListNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType.List == nil || !compilerTypes.Equal(node.Element, node.ResultType.List.Element) || !compilerTypes.IsHeap(node.OperandType) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("List<T>.new has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("List<T>.new result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.DictNewExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.ResultType.Dict == nil || !compilerTypes.Equal(node.Element, node.ResultType.Dict.Value) || !compilerTypes.IsHeap(node.OperandType) || !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("Dict<K, V>.new has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("Dict<K, V>.new result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Heap, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.WideningExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("widening expression has invalid checked metadata")
		}
		if !compilerTypes.IsInteger(node.ResultType) && !compilerTypes.IsFloat(node.ResultType) {
			return unknownExpressionDiagnostic("widening destination is not numeric")
		}
		if common, ok := compilerTypes.LosslessCommonType(node.OperandType, node.ResultType); !ok || !compilerTypes.Equal(common, node.ResultType) {
			return unknownExpressionDiagnostic("widening is not a proven lossless conversion")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("widening result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.DeepEqualityExpression:
		if node.Left == nil || node.Right == nil || node.OperandType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || node.Operator != checker.EqualOperator && node.Operator != checker.NotEqualOperator {
			return unknownExpressionDiagnostic("deep equality has invalid checked metadata")
		}
		leftType, leftOK := expressionTypeWithState(*node.Left, state)
		rightType, rightOK := expressionTypeWithState(*node.Right, state)
		if !leftOK || !rightOK || !compilerTypes.Equal(leftType, node.OperandType) || !compilerTypes.Equal(rightType, node.OperandType) {
			return unknownExpressionDiagnostic("deep equality operand does not match its compared type")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("deep equality result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Right, node.OperandType, state)
	case checker.ConversionExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.MemberIndex > 2 {
			return unknownExpressionDiagnostic("numeric conversion has invalid checked metadata")
		}
		if !compilerTypes.IsInteger(node.ResultType) && !compilerTypes.IsFloat(node.ResultType) || node.MemberIndex != 0 && (!compilerTypes.IsInteger(node.OperandType) || !compilerTypes.IsInteger(node.ResultType)) || node.MemberIndex == 0 && !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("numeric conversion has invalid checked types")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) && !compilerTypes.WidensTo(node.ResultType, expected) {
			return unknownExpressionDiagnostic("numeric conversion result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.StreamConstructorExpression:
		if err := validateStreamConstructor(node, expected, hasExpected, state); err != nil {
			return err
		}
		return nil
	case checker.StreamMethodCallExpression:
		if err := validateStreamMethod(node, expected, hasExpected, state); err != nil {
			return err
		}
		return nil
	case checker.BitCastExpression:
		if node.Operand == nil || !bitCastEligible(node.OperandType) || !bitCastEligible(node.ResultType) || node.OperandType.Bits != node.ResultType.Bits {
			return unknownExpressionDiagnostic("bit cast has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("bit cast result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.EndianConversionExpression:
		if node.Operand == nil || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.MemberIndex > 1 {
			return unknownExpressionDiagnostic("endian conversion has invalid checked metadata")
		}
		if node.Name == "from" {
			if len(node.Arguments) != 1 || node.ResultType == (compilerTypes.Type{}) || node.OperandType.Array == nil {
				return unknownExpressionDiagnostic("endian from conversion has invalid checked metadata")
			}
			if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
				return unknownExpressionDiagnostic("endian from result does not match its expected type")
			}
			return validateCheckedOperandWithState(node.Arguments[0], state)
		}
		if len(node.Arguments) != 0 || node.ResultType.Array == nil {
			return unknownExpressionDiagnostic("endian to conversion has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("endian to result does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.TryExpression:
		if node.Operand == nil || node.OperandType == (compilerTypes.Type{}) || node.ResultType == (compilerTypes.Type{}) || node.Element == (compilerTypes.Type{}) || node.MemberIndex < 0 || node.OperandType.Union == nil {
			return unknownExpressionDiagnostic("try expression has invalid checked metadata")
		}
		if unionMemberIndex(node.OperandType, compilerTypes.ErrorType) != node.MemberIndex {
			return unknownExpressionDiagnostic("try expression error member does not match its source union")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.SpawnExpression:
		if node.Operand == nil || node.OperandType.Task == nil || node.OperandType.Task.Result == (compilerTypes.Type{}) || node.ResultType.Union == nil || !compilerTypes.Equal(node.Element, node.OperandType.Task.Result) || node.SourceLine <= 0 {
			return unknownExpressionDiagnostic("spawn expression has invalid checked metadata")
		}
		if unionMemberIndex(node.ResultType, node.OperandType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnostic("spawn result union is missing its Task or Error member")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("spawn result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, compilerTypes.Type{}, state); err != nil {
			return err
		}
		return nil
	case checker.TaskYieldExpression:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Nil) {
			return unknownExpressionDiagnostic("Task.yield() result type is not Nil")
		}
		return nil
	case checker.TaskMethodCallExpression:
		if node.Operand == nil || node.OperandType.Task == nil {
			return unknownExpressionDiagnostic("task method has invalid checked metadata")
		}
		switch node.Name {
		case "join":
			if !compilerTypes.Equal(node.Element, node.OperandType.Task.Result) || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic("task join result does not match its Task result")
			}
		case "detach":
			if !compilerTypes.Equal(node.ResultType, compilerTypes.Nil) {
				return unknownExpressionDiagnostic("task detach result type is not Nil")
			}
		default:
			return unknownExpressionDiagnostic("unknown task method " + node.Name)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("task method result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.ChannelConstructorExpression:
		if node.OperandType.Channel == nil || len(node.Arguments) != 2 || !compilerTypes.Equal(node.Element, node.OperandType.Channel.Element) || node.ResultType.Union == nil || node.SourceLine <= 0 {
			return unknownExpressionDiagnostic("channel constructor has invalid checked metadata")
		}
		if unionMemberIndex(node.ResultType, node.OperandType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnostic("channel constructor union is missing its Channel or Error member")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("channel constructor result type does not match its expected type")
		}
		if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[1], state)
	case checker.ChannelMethodCallExpression:
		if node.Operand == nil || node.OperandType.Channel == nil || !compilerTypes.Equal(node.Element, node.OperandType.Channel.Element) {
			return unknownExpressionDiagnostic("channel method has invalid checked metadata")
		}
		switch node.Name {
		case "send":
			if len(node.Arguments) != 1 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, compilerTypes.Nil) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 || node.SourceLine <= 0 {
				return unknownExpressionDiagnostic("channel send has invalid checked metadata")
			}
		case "receive":
			if len(node.Arguments) != 0 || node.ResultType.Union == nil || unionMemberIndex(node.ResultType, node.Element) < 0 || unionMemberIndex(node.ResultType, compilerTypes.EoS) < 0 {
				return unknownExpressionDiagnostic("channel receive has invalid checked metadata")
			}
		case "close":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Nil) {
				return unknownExpressionDiagnostic("channel close has invalid checked metadata")
			}
		case "length", "capacity":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) {
				return unknownExpressionDiagnostic("channel " + node.Name + " has invalid checked metadata")
			}
		case "is_closed":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("channel is_closed has invalid checked metadata")
			}
		case "free":
			if len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, compilerTypes.Nil) {
				return unknownExpressionDiagnostic("channel free has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown channel method " + node.Name)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("channel method result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.MutexConstructorExpression:
		if len(node.Arguments) != 1 || !compilerTypes.IsMutex(node.OperandType) || node.ResultType.Union == nil || node.SourceLine <= 0 {
			return unknownExpressionDiagnostic("mutex constructor has invalid checked metadata")
		}
		if unionMemberIndex(node.ResultType, compilerTypes.MutexType) < 0 || unionMemberIndex(node.ResultType, compilerTypes.ErrorType) < 0 {
			return unknownExpressionDiagnostic("mutex constructor union is missing its Mutex or Error member")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("mutex constructor result type does not match its expected type")
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.MutexMethodCallExpression:
		if node.Operand == nil || !compilerTypes.IsMutex(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Nil) {
			return unknownExpressionDiagnostic("mutex method has invalid checked metadata")
		}
		switch node.Name {
		case "lock", "unlock":
			if len(node.Arguments) != 0 {
				return unknownExpressionDiagnostic("mutex " + node.Name + " expects no arguments")
			}
		case "free":
			if len(node.Arguments) != 1 {
				return unknownExpressionDiagnostic("mutex free expects one argument")
			}
		default:
			return unknownExpressionDiagnostic("unknown mutex method " + node.Name)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("mutex method result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.AtomicConstructorExpression:
		if node.OperandType.Atomic == nil || len(node.Arguments) != 1 || !compilerTypes.Equal(node.Element, node.OperandType.Atomic.Element) || !compilerTypes.Equal(node.ResultType, node.OperandType) {
			return unknownExpressionDiagnostic("atomic constructor has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("atomic constructor result type does not match its expected type")
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.AtomicMethodCallExpression:
		if node.Operand == nil || node.OperandType.Atomic == nil || !compilerTypes.Equal(node.Element, node.OperandType.Atomic.Element) {
			return unknownExpressionDiagnostic("atomic method has invalid checked metadata")
		}
		switch node.Name {
		case "load":
			if len(node.Arguments) != 0 || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic("atomic load has invalid checked metadata")
			}
		case "store":
			if len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, compilerTypes.Nil) {
				return unknownExpressionDiagnostic("atomic store has invalid checked metadata")
			}
		case "exchange", "fetch_add", "fetch_sub":
			if len(node.Arguments) != 1 || !compilerTypes.Equal(node.ResultType, node.Element) {
				return unknownExpressionDiagnostic("atomic " + node.Name + " has invalid checked metadata")
			}
		case "compare_exchange":
			if len(node.Arguments) != 2 || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
				return unknownExpressionDiagnostic("atomic compare_exchange has invalid checked metadata")
			}
		default:
			return unknownExpressionDiagnostic("unknown atomic method " + node.Name)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("atomic method result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.LayoutExpression:
		if node.OperandType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, compilerTypes.SizeType) || node.Name != "size_of" && node.Name != "align_of" {
			return unknownExpressionDiagnostic("layout query has invalid checked metadata")
		}
		if !layoutEligibleGenerated(node.OperandType) {
			return unknownExpressionDiagnostic("layout query has an ineligible type")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("layout query result type does not match its expected type")
		}
		return nil
	case checker.VolatileReadExpression:
		if node.Operand == nil || node.OperandType.Element == nil || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || !compilerTypes.Equal(node.ResultType, node.Element) {
			return unknownExpressionDiagnostic("volatile read has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("volatile read result type does not match its expected type")
		}
		return validateExpressionChildWithState(node.Operand, node.OperandType, state)
	case checker.VolatileWriteExpression:
		if node.Operand == nil || node.OperandType.Element == nil || len(node.Arguments) != 1 || !node.OperandType.PointeeWritable || !volatileEligibleGenerated(node.Element) || !compilerTypes.Equal(node.Element, *node.OperandType.Element) || !compilerTypes.Equal(node.ResultType, compilerTypes.Nil) {
			return unknownExpressionDiagnostic("volatile write has invalid checked metadata")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("volatile write result type does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
			return err
		}
		return validateCheckedOperandWithState(node.Arguments[0], state)
	case checker.ViewBridgeExpression:
		if node.OperandType.View == nil || !compilerTypes.Equal(node.Element, node.OperandType.View.Element) || !compilerTypes.Equal(node.ResultType, node.OperandType) {
			return unknownExpressionDiagnostic("view bridge has invalid checked metadata")
		}
		switch node.Name {
		case "empty":
			if len(node.Arguments) != 0 {
				return unknownExpressionDiagnostic("view bridge empty has unexpected arguments")
			}
		case "from_pointer":
			if len(node.Arguments) != 2 {
				return unknownExpressionDiagnostic("view bridge from_pointer has invalid checked metadata")
			}
			if err := validateCheckedOperandWithState(node.Arguments[0], state); err != nil {
				return err
			}
			if err := validateCheckedOperandWithState(node.Arguments[1], state); err != nil {
				return err
			}
		default:
			return unknownExpressionDiagnostic("unknown view bridge form " + node.Name)
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("view bridge result type does not match its expected type")
		}
		return nil
	case checker.PrintExpression:
		if len(node.Arguments) == 0 || node.ResultType != (compilerTypes.Type{}) || hasExpected {
			return unknownExpressionDiagnostic("print call has invalid checked metadata")
		}
		for _, argument := range node.Arguments {
			if err := validateCheckedOperandWithState(argument, state); err != nil {
				return err
			}
		}
		return nil
	case checker.StringCompareExpression:
		if node.Left == nil || node.Right == nil || !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("text ordering has invalid checked metadata")
		}
		switch node.Operator {
		case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		default:
			return unknownExpressionDiagnostic("text ordering has an invalid operator")
		}
		leftType, leftOK := expressionTypeWithState(*node.Left, state)
		rightType, rightOK := expressionTypeWithState(*node.Right, state)
		if !leftOK || !rightOK || !compilerTypes.Equal(leftType, node.OperandType) || !compilerTypes.Equal(rightType, node.OperandType) {
			return unknownExpressionDiagnostic("text ordering operand does not match its compared type")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("text ordering result does not match its expected type")
		}
		if err := validateExpressionChildWithState(node.Left, node.OperandType, state); err != nil {
			return err
		}
		return validateExpressionChildWithState(node.Right, node.OperandType, state)
	default:
		return unknownExpressionDiagnostic("unsupported checked expression")
	}
}

func validateExpressionMetadata(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	var metadataType compilerTypes.Type
	for _, typ := range []compilerTypes.Type{node.OperandType, node.ResultType} {
		if typ == (compilerTypes.Type{}) {
			continue
		}
		if !supportedGeneratedTypeWithState(typ, state) || hasExpected && !compilerTypes.Equal(expected, typ) || metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(metadataType, typ) {
			return unknownExpressionDiagnostic("expression metadata does not match its expected type")
		}
		metadataType = typ
	}
	return nil
}

// validateFunctionReference accepts a declared function used as a Fun<â€¦> value.
// A function is not a place, so no addressability metadata is consulted.
func validateFunctionReference(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if !validSourceName(node.Name) {
		return unknownExpressionDiagnostic("function reference without a source name")
	}
	if node.ResultType.Signature == nil || !supportedGeneratedTypeWithState(node.ResultType, state) {
		return unknownExpressionDiagnostic("function reference without a checked Fun type")
	}
	if state.functions != nil {
		declared, ok := state.functions[node.Name]
		if !ok || !compilerTypes.Equal(declared, node.ResultType) {
			return unknownExpressionDiagnostic("function reference is not a declared checked function")
		}
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, node.ResultType) {
		return unknownExpressionDiagnostic("function reference metadata does not match its checked type")
	}
	if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
		return unknownExpressionDiagnostic("function reference type does not match its expected type")
	}
	return nil
}

// validateCallExpression checks a call against its callee's signature. The
// arguments carry no ordering metadata: RFC 0008 inherits C's unspecified
// argument evaluation order rather than introducing temporaries.
func validateCallExpression(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("call without a checked callee")
	}
	signature := node.OperandType.Signature
	if signature == nil || !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("call callee is not a checked Fun type")
	}
	if len(signature.Parameters) != len(node.Arguments) {
		return unknownExpressionDiagnostic("call argument count does not match its checked signature")
	}
	if signature.Result == nil {
		if node.ResultType != (compilerTypes.Type{}) {
			return unknownExpressionDiagnostic("call result type does not match its checked signature")
		}
		if hasExpected {
			return unknownExpressionDiagnostic("a call producing no value has no expected type")
		}
	} else {
		if !compilerTypes.Equal(*signature.Result, node.ResultType) {
			return unknownExpressionDiagnostic("call result type does not match its checked signature")
		}
		if hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("call result type does not match its expected type")
		}
	}
	if err := validateExpressionChildWithState(node.Operand, node.OperandType, state); err != nil {
		return err
	}
	for index, argument := range node.Arguments {
		if !generatedAssignable(signature.Parameters[index], argument.Type) {
			return unknownExpressionDiagnostic("call argument type does not match its checked parameter")
		}
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

func validateMethodCallExpression(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Owner == nil || !validSourceName(compilerTypes.SanitizeIdentifier(node.Owner.Name)) || !validSourceName(node.Name) || node.Operand == nil {
		return unknownExpressionDiagnostic("method call has incomplete checked metadata")
	}
	declared, ok := state.methods[methodKey(node.Owner, node.Name)]
	if !ok || declared.Object != node.Owner {
		return unknownExpressionDiagnostic("method call does not name a declared checked method")
	}
	if !compilerTypes.Equal(node.OperandType, declared.SelfType) || len(node.Arguments) != len(declared.Parameters) {
		return unknownExpressionDiagnostic("method call does not match its checked signature")
	}
	if declared.Result == nil {
		if node.ResultType != (compilerTypes.Type{}) || hasExpected {
			return unknownExpressionDiagnostic("method call result type does not match its checked signature")
		}
	} else {
		if node.ResultType == (compilerTypes.Type{}) || !compilerTypes.Equal(node.ResultType, *declared.Result) || hasExpected && !compilerTypes.Equal(expected, node.ResultType) {
			return unknownExpressionDiagnostic("method call result type does not match its checked signature")
		}
	}
	receiverType, receiverErr := methodReceiverType(*node.Operand, node.OperandType, state)
	if receiverErr != nil {
		return receiverErr
	}
	if !generatedAssignable(node.OperandType, receiverType) {
		return unknownExpressionDiagnostic("method call receiver type does not match its checked receiver")
	}
	if err := validateExpressionChildWithState(node.Operand, receiverType, state); err != nil {
		return err
	}
	for index, argument := range node.Arguments {
		if !generatedAssignable(declared.Parameters[index].Type, argument.Type) {
			return unknownExpressionDiagnostic("method call argument type does not match its checked parameter")
		}
		if err := validateCheckedOperandWithState(argument, state); err != nil {
			return err
		}
	}
	return nil
}

// methodReceiverType recovers the actual checked type of an adapted receiver.
// Address-of nodes intentionally omit result metadata because ordinary ref
// typing is contextual; the place's writability supplies the missing
// Ptr<T>/MutPtr<T> distinction here.
func methodReceiverType(node checker.Expression, target compilerTypes.Type, state *expressionValidation) (compilerTypes.Type, error) {
	if node.Kind == checker.AddressOfExpression {
		if node.Operand == nil {
			return compilerTypes.Type{}, unknownExpressionDiagnostic("method receiver address-of has no place")
		}
		place, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return compilerTypes.Type{}, err
		}
		pointer := compilerTypes.PtrType(place.typ)
		if place.writable {
			pointer = compilerTypes.MutPtrType(place.typ)
		}
		return pointer, nil
	}
	if typ, ok := expressionTypeWithState(node, state); ok {
		// RFC 0010: the checker only adapted a nullable receiver after a null
		// test narrowed it to its pointer member, so when the binding still
		// holds the declared nullable type and the method's self type is that
		// member, the receiver's effective type is the non-null member.
		if base, nullable := compilerTypes.NullableBase(typ); nullable && compilerTypes.Equal(base, target) {
			return base, nil
		}
		return typ, nil
	}
	return target, nil
}

func validateAddressExpression(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("address-of without an operand")
	}
	if node.OperandType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("address-of has an invalid operand type")
	}
	resultType, hasResult := expected, hasExpected
	if node.ResultType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.ResultType, state) || !isPointerType(node.ResultType) {
			return unknownExpressionDiagnostic("address-of result is not a valid pointer type")
		}
		if hasResult && !compilerTypes.Equal(resultType, node.ResultType) {
			return unknownExpressionDiagnostic("address-of result type does not match its expected type")
		}
		resultType, hasResult = node.ResultType, true
	}
	if !hasResult || !isPointerType(resultType) {
		return unknownExpressionDiagnostic("address-of result is not a pointer type")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(*resultType.Element, node.OperandType) {
		return unknownExpressionDiagnostic("address-of operand type does not match its result type")
	}
	if err := validateExpressionChildWithState(node.Operand, *resultType.Element, state); err != nil {
		return err
	}
	place, err := checkedPlaceMetadata(*node.Operand, state)
	if err != nil {
		return err
	}
	if !place.addressable {
		return unknownExpressionDiagnostic("address-of child is not addressable")
	}
	if !compilerTypes.Equal(place.typ, *resultType.Element) {
		return unknownExpressionDiagnostic("address-of child type does not match its result type")
	}
	if place.writable != resultType.PointeeWritable {
		return unknownExpressionDiagnostic("address-of result writability does not match its place")
	}
	return nil
}

func validateDereferenceExpression(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil {
		return unknownExpressionDiagnostic("dereference without an operand")
	}
	resultType, hasResult := expected, hasExpected
	if node.ResultType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.ResultType, state) {
			return unknownExpressionDiagnostic("dereference result type is not supported")
		}
		if hasResult && !compilerTypes.Equal(resultType, node.ResultType) {
			return unknownExpressionDiagnostic("dereference result type does not match its expected type")
		}
		resultType, hasResult = node.ResultType, true
	}
	if node.OperandType != (compilerTypes.Type{}) {
		if !supportedGeneratedTypeWithState(node.OperandType, state) || !isPointerType(node.OperandType) {
			return unknownExpressionDiagnostic("dereference operand is not a valid pointer type")
		}
		if hasResult && !compilerTypes.Equal(*node.OperandType.Element, resultType) {
			return unknownExpressionDiagnostic("dereference result type does not match its operand type")
		}
	}

	receiverType, ok := expressionTypeWithState(*node.Operand, state)
	if !ok && node.OperandType != (compilerTypes.Type{}) {
		receiverType, ok = node.OperandType, true
	}
	if !ok || !supportedGeneratedTypeWithState(receiverType, state) || !isPointerType(receiverType) {
		return unknownExpressionDiagnostic("dereference receiver is not a checked pointer expression")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(receiverType, node.OperandType) {
		return unknownExpressionDiagnostic("dereference receiver type does not match its checked operand type")
	}
	if !hasResult {
		resultType, hasResult = *receiverType.Element, true
	}
	if !hasResult || !compilerTypes.Equal(*receiverType.Element, resultType) {
		return unknownExpressionDiagnostic("dereference receiver element does not match its result type")
	}
	return validateExpressionChildWithState(node.Operand, receiverType, state)
}

func validateMemberExpression(node checker.Expression, expected compilerTypes.Type, hasExpected bool, state *expressionValidation) error {
	if node.Operand == nil || node.Member == nil || !validSourceName(node.Member.Name) || !supportedGeneratedTypeWithState(node.Member.Type, state) {
		return unknownExpressionDiagnostic("member selection has invalid checked metadata")
	}
	if hasExpected && !compilerTypes.Equal(expected, node.Member.Type) {
		return unknownExpressionDiagnostic("member type does not match its expected type")
	}
	if node.ResultType != (compilerTypes.Type{}) && (!supportedGeneratedTypeWithState(node.ResultType, state) || !compilerTypes.Equal(node.ResultType, node.Member.Type) || hasExpected && !compilerTypes.Equal(expected, node.ResultType)) {
		return unknownExpressionDiagnostic("member result type does not match its checked member")
	}
	if node.OperandType != (compilerTypes.Type{}) && !supportedGeneratedTypeWithState(node.OperandType, state) {
		return unknownExpressionDiagnostic("member receiver has an invalid checked type")
	}
	if err := validateExpressionChildWithState(node.Operand, compilerTypes.Type{}, state); err != nil {
		return err
	}
	receiverType, ok := expressionTypeWithState(*node.Operand, state)
	if !ok && node.OperandType != (compilerTypes.Type{}) {
		receiverType, ok = node.OperandType, true
	}
	if !ok || !supportedGeneratedTypeWithState(receiverType, state) || receiverType.Object == nil {
		return unknownExpressionDiagnostic("member receiver is not a checked object expression")
	}
	if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiverType) {
		return unknownExpressionDiagnostic("member receiver type does not match its checked receiver")
	}
	canonical, pointerOK := objectMember(receiverType.Object, node.Member)
	byName, nameOK := receiverType.Object.Member(node.Member.Name)
	if !pointerOK || !nameOK || canonical != byName || !compilerTypes.Equal(canonical.Type, node.Member.Type) {
		return unknownExpressionDiagnostic("member is not part of its checked object")
	}
	return nil
}

func validateUnaryMetadata(node checker.Expression) error {
	switch node.Operator {
	case checker.NegateOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) || !compilerTypes.IsSignedInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("negation has invalid checked types")
		}
	case checker.LogicalNotOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic("logical not requires a truthy-compatible operand and a Bool result")
		}
	case checker.BitwiseNotOperator:
		if !compilerTypes.Equal(node.OperandType, node.ResultType) || !compilerTypes.IsInteger(node.OperandType) || compilerTypes.IsRune(node.OperandType) {
			return unknownExpressionDiagnostic("complement has invalid checked types")
		}
	default:
		return unknownExpressionDiagnostic("unknown unary operator")
	}
	return nil
}

func validateBinaryMetadata(node checker.Expression) error {
	resultIsBool := false
	switch node.Operator {
	case checker.AddOperator, checker.SubtractOperator, checker.MultiplyOperator, checker.DivideOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) {
			return unknownExpressionDiagnostic("arithmetic operation with an unsupported type")
		}
		if !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("arithmetic result type does not match its operand type")
		}
	case checker.RemainderOperator:
		if !compilerTypes.IsInteger(node.OperandType) || !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("remainder operation has invalid checked types")
		}
	case checker.EqualOperator, checker.NotEqualOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("equality operation must produce Bool")
		}
		resultIsBool = true
	case checker.LessOperator, checker.LessEqualOperator, checker.GreaterOperator, checker.GreaterEqualOperator:
		if !compilerTypes.IsInteger(node.OperandType) && !compilerTypes.IsFloat(node.OperandType) || !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
			return unknownExpressionDiagnostic("ordering operation has invalid checked types")
		}
		resultIsBool = true
	case checker.LogicalAndOperator, checker.LogicalOrOperator:
		if !compilerTypes.Equal(node.ResultType, compilerTypes.Bool) || compilerTypes.Truthiness(node.OperandType) == compilerTypes.TruthinessInvalid {
			return unknownExpressionDiagnostic("logical operation requires a truthy-compatible operand and a Bool result")
		}
		resultIsBool = true
	case checker.BitwiseAndOperator, checker.BitwiseXorOperator, checker.BitwiseOrOperator,
		checker.ShiftLeftOperator, checker.ShiftRightOperator:
		if !compilerTypes.IsInteger(node.OperandType) || compilerTypes.IsRune(node.OperandType) || !compilerTypes.Equal(node.OperandType, node.ResultType) {
			return unknownExpressionDiagnostic("bitwise or shift operation has invalid checked types")
		}
	default:
		return unknownExpressionDiagnostic("unknown binary operator")
	}
	if resultIsBool != compilerTypes.Equal(node.ResultType, compilerTypes.Bool) {
		return unknownExpressionDiagnostic("binary operation has an invalid result type")
	}
	return nil
}

func validateExpressionChildWithState(child *checker.Expression, expected compilerTypes.Type, state *expressionValidation) error {
	if child == nil {
		return unknownExpressionDiagnostic("operation without a checked child")
	}
	if state.expressions == nil {
		state.expressions = make(map[*checker.Expression]bool)
	}
	if state.expressions[child] {
		return unknownExpressionDiagnostic("cyclic checked expression")
	}
	state.expressions[child] = true
	defer delete(state.expressions, child)
	return validateExpressionNode(*child, expected, expected != (compilerTypes.Type{}), state)
}

// validateTruthinessChild validates a logical operand per RFC 0023. The nil
// literal is checker-supported but its other generator paths fail closed
// (RFC 0010); truthiness contexts accept it as the constant false.
func validateTruthinessChild(child *checker.Expression, state *expressionValidation) error {
	if child.Kind == checker.NilExpression {
		return nil
	}
	return validateExpressionChildWithState(child, compilerTypes.Type{}, state)
}

func isPointerType(typ compilerTypes.Type) bool {
	return typ.Element != nil && typ.Object == nil && typ.ScalarKind == compilerTypes.ScalarNone && typ.Bits == 0
}

// checkedPlaceMetadata reconstructs place capabilities from generated bindings
// and nominal type metadata instead of trusting forged operand flags.
func checkedPlaceMetadata(node checker.Expression, state *expressionValidation) (generatedPlace, error) {
	switch node.Kind {
	case checker.VariableExpression:
		if !validSourceName(node.Name) || state == nil || state.variables == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place variable binding metadata is unavailable")
		}
		binding, ok := state.bindingFor(node)
		if !ok {
			return generatedPlace{}, unknownExpressionDiagnostic("place variable is not present in checked bindings")
		}
		for _, metadataType := range []compilerTypes.Type{node.OperandType, node.ResultType} {
			if metadataType != (compilerTypes.Type{}) && !compilerTypes.Equal(binding.typ, metadataType) {
				return generatedPlace{}, unknownExpressionDiagnostic("place variable metadata does not match its checked binding")
			}
		}
		return generatedPlace{typ: binding.typ, addressable: true, writable: binding.mutable}, nil
	case checker.MemberExpression:
		if node.Operand == nil || node.Member == nil || !validSourceName(node.Member.Name) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member has invalid checked metadata")
		}
		receiver, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if receiver.typ.Object == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place member receiver is not a checked object")
		}
		canonical, pointerOK := objectMember(receiver.typ.Object, node.Member)
		byName, nameOK := receiver.typ.Object.Member(node.Member.Name)
		if !pointerOK || !nameOK || canonical != byName || !compilerTypes.Equal(canonical.Type, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member is not part of its checked object")
		}
		if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiver.typ) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member receiver type does not match its checked receiver")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, node.Member.Type) {
			return generatedPlace{}, unknownExpressionDiagnostic("place member result type does not match its checked member")
		}
		return generatedPlace{
			typ:         node.Member.Type,
			addressable: receiver.addressable,
			writable:    receiver.writable && node.Member.Mutable,
		}, nil
	case checker.DereferenceExpression:
		if node.Operand == nil {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference has no pointer receiver")
		}
		var receiverType compilerTypes.Type
		var ok bool
		switch node.Operand.Kind {
		case checker.VariableExpression, checker.MemberExpression, checker.DereferenceExpression:
			receiver, err := checkedPlaceMetadata(*node.Operand, state)
			if err != nil {
				return generatedPlace{}, err
			}
			receiverType, ok = receiver.typ, true
		default:
			receiverType, ok = expressionTypeWithState(*node.Operand, state)
		}
		if !ok || !supportedGeneratedTypeWithState(receiverType, state) || !isPointerType(receiverType) {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference receiver is not a checked pointer")
		}
		if node.OperandType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.OperandType, receiverType) {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference receiver type does not match its checked receiver")
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, *receiverType.Element) {
			return generatedPlace{}, unknownExpressionDiagnostic("place dereference result type does not match its pointee")
		}
		return generatedPlace{typ: *receiverType.Element, addressable: true, writable: receiverType.PointeeWritable}, nil
	case checker.IndexExpression:
		if node.Operand == nil || len(node.Arguments) != 1 || node.OperandType.Array == nil && node.OperandType.View == nil && node.OperandType.List == nil && !compilerTypes.IsString(node.OperandType) && !compilerTypes.IsStrand(node.OperandType) {
			return generatedPlace{}, unknownExpressionDiagnostic("place index has invalid checked metadata")
		}
		receiver, err := checkedPlaceMetadata(*node.Operand, state)
		if err != nil {
			return generatedPlace{}, err
		}
		if !compilerTypes.Equal(node.OperandType, receiver.typ) {
			return generatedPlace{}, unknownExpressionDiagnostic("place index receiver type does not match its checked receiver")
		}
		var element compilerTypes.Type
		if node.OperandType.Array != nil {
			element = node.OperandType.Array.Element
		} else if node.OperandType.View != nil {
			element = node.OperandType.View.Element
		} else if node.OperandType.List != nil {
			element = node.OperandType.List.Element
		} else {
			element = compilerTypes.Rune
		}
		if node.ResultType != (compilerTypes.Type{}) && !compilerTypes.Equal(node.ResultType, element) {
			return generatedPlace{}, unknownExpressionDiagnostic("place index result type does not match its element type")
		}
		// A View element place is never writable; a mutable Array place or
		// any live List reference is. Text indexing is read-only.
		writable := node.OperandType.Array != nil && receiver.writable || node.OperandType.List != nil
		return generatedPlace{typ: element, addressable: receiver.addressable, writable: writable}, nil
	default:
		return generatedPlace{}, unknownExpressionDiagnostic("checked expression is not a place")
	}
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

// declaration builds the complete C declarator for typ bound to name. Every
// type but Fun<â€¦> is a prefix plus the name; a function pointer wraps the name
// inside the declarator, which is why a CName prefix cannot express it.
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
	if compilerTypes.IsStream(typ) {
		// A source Stream value is a pointer-sized owning handle to a
		// mutable header-and-state object (RFC 0031); mutation flows
		// through it without a mut binding.
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
		return PrivateCName(FunctionName, node.Name), nil
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
		return methodCName(node.Owner, node.Name) + "(" + strings.Join(allArguments, ", ") + ")", nil
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
		receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if receiverErr != nil {
			return "", receiverErr
		}
		if !atomic {
			receiver = "(" + receiver + ")"
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
			receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
			if receiverErr != nil {
				return "", receiverErr
			}
			if !atomic {
				receiver = "(" + receiver + ")"
			}
			if node.OperandType.List != nil || node.OperandType.Dict != nil {
				// List and Dict bindings are pointer-sized handles.
				return "(" + receiver + ")->length", nil
			}
			return "(" + receiver + ").length", nil
		case "is_empty":
			if node.OperandType.Array != nil {
				return "false", nil
			}
			receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
			if receiverErr != nil {
				return "", receiverErr
			}
			if !atomic {
				receiver = "(" + receiver + ")"
			}
			if node.OperandType.List != nil || node.OperandType.Dict != nil {
				return "((" + receiver + ")->length == 0)", nil
			}
			return "((" + receiver + ").length == 0)", nil
		case "at":
			if node.Operand == nil || len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic("collection at without a checked index")
			}
			receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
			if receiverErr != nil {
				return "", receiverErr
			}
			if !atomic {
				receiver = "(" + receiver + ")"
			}
			index, indexErr := renderOperandWithState(node.Arguments[0], state)
			if indexErr != nil {
				return "", indexErr
			}
			if node.OperandType.View != nil {
				return "*hex_view_at_" + strings.TrimPrefix(node.OperandType.CName, "hex_view_") + "(" + receiver + ", (size_t)(" + index + "))", nil
			}
			if node.OperandType.List != nil {
				return "*hex_list_at_" + listSuffix(node.OperandType) + "(" + receiver + ", (size_t)(" + index + "))", nil
			}
			return "*" + arrayAccessorCName(node.OperandType, false) + "(&" + receiver + ", (size_t)(" + index + "))", nil
		case "push", "set", "clear", "pop":
			if node.Operand == nil || node.OperandType.List == nil {
				return "", unknownExpressionDiagnostic("list mutation without a checked list receiver")
			}
			receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
			if receiverErr != nil {
				return "", receiverErr
			}
			if !atomic {
				receiver = "(" + receiver + ")"
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
			receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
			if receiverErr != nil {
				return "", receiverErr
			}
			if !atomic {
				receiver = "(" + receiver + ")"
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
			receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
			if receiverErr != nil {
				return "", receiverErr
			}
			if !atomic {
				receiver = "(" + receiver + ")"
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
		receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if receiverErr != nil {
			return "", receiverErr
		}
		if !atomic {
			receiver = "(" + receiver + ")"
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
		receiver, atomic, receiverErr := renderExpressionNodeWithExpectedState(*node.Operand, node.OperandType, state, true)
		if receiverErr != nil {
			return "", receiverErr
		}
		if !atomic {
			receiver = "(" + receiver + ")"
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
		case "at":
			if len(node.Arguments) != 1 {
				return "", unknownExpressionDiagnostic("string at without a checked index")
			}
			index, indexErr := renderOperandWithState(node.Arguments[0], state)
			if indexErr != nil {
				return "", indexErr
			}
			if compilerTypes.IsStrand(node.OperandType) {
				return "hex_strand_at_rune(" + receiver + ", (size_t)(" + index + "))", nil
			}
			return "hex_string_at_rune(" + receiver + ", (size_t)(" + index + "))", nil
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
	case checker.FileModeLiteralExpression:
		return renderFileModeLiteral(node)
	case checker.FileOpenExpression:
		return renderFileOpen(node, state)
	case checker.StdioCallExpression:
		return renderStdioCall(node)
	case checker.FileMethodCallExpression:
		return renderFileMethod(node, state)
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
	case checker.StreamConstructorExpression:
		return renderStreamConstructor(node, state)
	case checker.StreamMethodCallExpression:
		return renderStreamMethod(node, state)
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
		receiver, atomic, err := renderExpressionNodeWithExpectedState(*node.Operand, receiverType, state, true)
		if err != nil {
			return "", err
		}
		if !atomic {
			receiver = "(" + receiver + ")"
		}
		return receiver + "." + PrivateCName(MemberName, node.Member.Name), nil
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
		return operand + ".tag " + operator + " " + unionTagName(node.OperandType, unionMemberIndex(node.OperandType, compilerTypes.Nil)), nil
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

func validateExpressionChild(child checker.Expression, expected compilerTypes.Type) error {
	if !supportedGeneratedType(expected) {
		return unknownExpressionDiagnostic("operation has an unsupported operand type")
	}
	return validateExpressionNode(child, expected, true, &expressionValidation{})
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
		checker.StreamConstructorExpression, checker.StreamMethodCallExpression,
		checker.DeepEqualityExpression, checker.StringCompareExpression, checker.WideningExpression, checker.ConversionExpression,
		checker.SpawnExpression, checker.TaskYieldExpression, checker.TaskMethodCallExpression,
		checker.ChannelConstructorExpression, checker.ChannelMethodCallExpression,
		checker.MutexConstructorExpression, checker.MutexMethodCallExpression,
		checker.AtomicConstructorExpression, checker.AtomicMethodCallExpression,
		checker.LayoutExpression, checker.VolatileReadExpression, checker.VolatileWriteExpression, checker.ViewBridgeExpression, checker.FileModeLiteralExpression, checker.FileOpenExpression, checker.StdioCallExpression, checker.FileMethodCallExpression:
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

func unknownExpressionDiagnostic(detail string) error {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.UnknownError,
		Stage:    "generator",
		Message:  detail,
	}
}

// layoutEligibleGenerated is the generator-side layout gate: the type must
// have a settled representation at this point, so a type parameter is a
// generation failure (specialization must have resolved it).
func layoutEligibleGenerated(typ compilerTypes.Type) bool {
	if typ == (compilerTypes.Type{}) || compilerTypes.ContainsTypeParameter(typ) {
		return false
	}
	if compilerTypes.IsUnknown(typ) || typ.Incomplete {
		return false
	}
	if typ.Signature != nil {
		return typ.Signature.Result != nil
	}
	return compilerTypes.IsCompleteValue(typ)
}

// volatileEligibleGenerated mirrors the checker's integer-only volatile set.
func volatileEligibleGenerated(typ compilerTypes.Type) bool {
	return compilerTypes.Equal(typ, compilerTypes.Int8) ||
		compilerTypes.Equal(typ, compilerTypes.Int16) ||
		compilerTypes.Equal(typ, compilerTypes.Int32) ||
		compilerTypes.Equal(typ, compilerTypes.Int64) ||
		compilerTypes.Equal(typ, compilerTypes.UInt8) ||
		compilerTypes.Equal(typ, compilerTypes.UInt16) ||
		compilerTypes.Equal(typ, compilerTypes.UInt32) ||
		compilerTypes.Equal(typ, compilerTypes.UInt64) ||
		compilerTypes.IsSize(typ)
}

func writeLineDirective(body *strings.Builder, line int) {
	if line > 0 {
		fmt.Fprintf(body, "#line %d \"%s\"\n", line, sourceFilename)
	}
}

func header(float32Used, float64Used, nilUsed bool, objects []*compilerTypes.ObjectType) string {
	return headerWithUnions(float32Used, float64Used, nilUsed, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, objects, false, nil, nil, nil)
}

func headerWithUnions(float32Used, float64Used, nilUsed bool, unions *generatedUnionState, heaps *heapHelpers, adts *generatedAdtState, arrays *generatedArrayState, views *generatedViewState, stringState *generatedStringState, lists *generatedListState, dicts *generatedDictState, streams *generatedStreamState, equality *generatedEqualityState, conversions []conversionSpec, divisionTypes []compilerTypes.Type, shiftSpecs []shiftSpec, bitCastSpecs []bitCastSpec, endianSpecs []endianSpec, objects []*compilerTypes.ObjectType, errorUsed bool, printState *generatedPrintState, concurrency *generatedConcurrencyState, io *generatedIOState) string {
	var result strings.Builder
	result.WriteString(mainHeaderPrefix)
	result.WriteString("\nstatic_assert(CHAR_BIT == 8, \"Hexal requires 8-bit bytes\");\n")
	result.WriteString("static_assert(sizeof(uint8_t) * CHAR_BIT == 8 && UINT8_MAX == 255, \"Hexal requires UInt8\");\n")
	result.WriteString("static_assert(sizeof(uint16_t) * CHAR_BIT == 16 && UINT16_MAX == 65535, \"Hexal requires UInt16\");\n")
	result.WriteString("static_assert(sizeof(uint32_t) * CHAR_BIT == 32 && UINT32_MAX == 4294967295u, \"Hexal requires UInt32\");\n")
	result.WriteString("static_assert(sizeof(uint64_t) * CHAR_BIT == 64 && UINT64_MAX == UINT64_C(18446744073709551615), \"Hexal requires UInt64\");\n")
	result.WriteString("static_assert(sizeof(int8_t) * CHAR_BIT == 8 && INT8_MIN == -128 && INT8_MAX == 127, \"Hexal requires Int8\");\n")
	result.WriteString("static_assert(sizeof(int16_t) * CHAR_BIT == 16 && INT16_MIN == -32768 && INT16_MAX == 32767, \"Hexal requires Int16\");\n")
	result.WriteString("static_assert(sizeof(int32_t) * CHAR_BIT == 32 && INT32_MIN == (-2147483647 - 1) && INT32_MAX == 2147483647, \"Hexal requires Int32\");\n")
	result.WriteString("static_assert(sizeof(int64_t) * CHAR_BIT == 64 && INT64_MIN == (-INT64_C(9223372036854775807) - 1) && INT64_MAX == INT64_C(9223372036854775807), \"Hexal requires Int64\");\n")
	// RFC 0010: nullptr_t and the nullptr predefined constant live in
	// <stddef.h>, included only when a written name needs them.
	if nilUsed {
		result.WriteString("#include <stddef.h>\n\n")
	}
	if arrays != nil && len(arrays.order) > 0 || views != nil && len(views.views) > 0 || stringState != nil && stringState.used || lists != nil && len(lists.order) > 0 || dicts != nil && len(dicts.order) > 0 || containsSizeConversion(conversions) {
		// The bounds guards in the array, view, string, and list helpers
		// report through fputs on stderr.
		result.WriteString("#include <stdio.h>\n\n")
		// RFC 0036: the v1 target profile is a 64-bit size_t; the generated
		// C rejects an ABI mismatch before executing the program.
		result.WriteString("static_assert(sizeof(size_t) == 8, \"Hexal Size requires a 64-bit size_t target\");\n\n")
	}
	if float32Used || float64Used {
		result.WriteString("#include <float.h>\n#include <math.h>\n\n")
		result.WriteString("static_assert(FLT_RADIX == 2, \"Hexal requires binary floating point\");\n")
	}
	if float32Used {
		result.WriteString("static_assert(sizeof(float) == 4 && FLT_MANT_DIG == 24 && FLT_MAX_EXP == 128, \"Hexal Float32 requires the binary32 value set\");\n")
		result.WriteString("#if !defined(FLT_IS_IEC_60559) || FLT_IS_IEC_60559 != 1\n#error \"Hexal Float32 requires IEC 60559\"\n#endif\n")
	}
	if float64Used {
		result.WriteString("static_assert(sizeof(double) == 8 && DBL_MANT_DIG == 53 && DBL_MAX_EXP == 1024, \"Hexal Float64 requires the binary64 value set\");\n")
		result.WriteString("#if !defined(DBL_IS_IEC_60559) || DBL_IS_IEC_60559 != 1\n#error \"Hexal Float64 requires IEC 60559\"\n#endif\n")
	}
	// RFC 0031: the EoS singleton lowers to one compiler-owned byte.
	result.WriteString("typedef uint8_t hex_eos;\n\n")
	writeConcurrencyTypePrelude(&result, concurrency)
	writeIOPrelude(&result, io)
	writeAdtDefinitions(&result, adts)
	writeHeapDefinitions(&result, heaps)
	writeViewDefinitions(&result, views)
	writeStringDefinitions(&result, stringState)
	if errorUsed {
		// RFC 0029: Error's complete definition follows the String and Strand
		// typedefs it needs and precedes the unions that may carry it as a
		// payload member.
		writeErrorDefinition(&result)
	}
	writeUnionDefinitions(&result, unions)
	writeListDefinitions(&result, lists, views)
	writeDictDefinitions(&result, dicts)
	writeArrayDefinitions(&result, arrays, views)
	writeObjectDefinitions(&result, objects)
	// The Stream families embed user object State types by value, so they
	// are emitted after every object definition (RFC 0031).
	writeStreamDefinitions(&result, streams)
	writeShiftDefinitions(&result, shiftSpecs)
	writeBitCastDefinitions(&result, bitCastSpecs)
	writeEndianDefinitions(&result, endianSpecs)
	writePrintDefinitions(&result, printState)
	writeEqualityDefinitions(&result, equality)
	writeDivisionDefinitions(&result, divisionTypes)
	writeConversionDefinitions(&result, conversions)
	writeConcurrencyDefinitions(&result, concurrency, stringState)
	writeIODefinitions(&result, io, stringState, concurrency != nil && concurrency.used)
	result.WriteString("\n#endif\n")
	return result.String()
}

func objectDefinitions(program checker.Program) ([]*compilerTypes.ObjectType, error) {
	objects := make([]*compilerTypes.ObjectType, 0)
	seen := make(map[*compilerTypes.ObjectType]bool)
	seenCNames := make(map[string]*compilerTypes.ObjectType)
	for _, declaration := range program.TypeDeclarations {
		object := declaration.Type.Object
		if object == nil {
			continue
		}
		if previous, exists := seenCNames[object.CName]; exists && previous != object {
			return nil, unknownExpressionDiagnostic("conflicting generated object C name")
		}
		seenCNames[object.CName] = object
		if seen[object] {
			continue
		}
		seen[object] = true
		objects = append(objects, object)
	}
	return objects, nil
}

func writeObjectDefinitions(result *strings.Builder, objects []*compilerTypes.ObjectType) {
	// Forward typedef region first, in source declaration order, so recursive
	// and non-recursive objects share one shape and pointer members can name a
	// not-yet-defined object.
	for _, object := range objects {
		result.WriteString("\n")
		if object.SourceLine > 0 {
			fmt.Fprintf(result, "#line %d \"%s\"\n", object.SourceLine, sourceFilename)
		}
		fmt.Fprintf(result, "typedef struct %s %s;\n", object.CName, object.CName)
	}
	for _, object := range objects {
		result.WriteString("\n")
		if object.SourceLine > 0 {
			fmt.Fprintf(result, "#line %d \"%s\"\n", object.SourceLine, sourceFilename)
		}
		fmt.Fprintf(result, "struct %s {\n", object.CName)
		for _, member := range object.Members {
			if member.SourceLine > 0 {
				fmt.Fprintf(result, "#line %d \"%s\"\n", member.SourceLine, sourceFilename)
			}
			// RFC 0035: reference-like members (String, List, Dict, Stream)
			// are pointer-sized handles, spelled like their declarations.
			fmt.Fprintf(result, "    %s;\n", declaration(member.Type, PrivateCName(MemberName, member.Name), true))
		}
		fmt.Fprintf(result, "};\n")
	}
}

func usedFloatTypes(program checker.Program) (bool, bool, bool) {
	float32Used, float64Used, nilUsed := false, false, false
	seenObjects := make(map[*compilerTypes.ObjectType]bool)
	var walkOperand func(checker.Operand)
	var walkExpression func(checker.Expression)
	var walk func(compilerTypes.Type)
	walk = func(typ compilerTypes.Type) {
		if typ.Union != nil {
			for _, member := range typ.Union.Members {
				walk(member)
			}
			return
		}
		if typ.Signature != nil {
			for _, parameter := range typ.Signature.Parameters {
				walk(parameter)
			}
			if typ.Signature.Result != nil {
				walk(*typ.Signature.Result)
			}
			return
		}
		if typ.Element != nil {
			walk(*typ.Element)
			return
		}
		if typ.Array != nil {
			walk(typ.Array.Element)
			return
		}
		if typ.View != nil {
			walk(typ.View.Element)
			return
		}
		if typ.List != nil {
			walk(typ.List.Element)
			return
		}
		if typ.Object != nil {
			if seenObjects[typ.Object] {
				return
			}
			seenObjects[typ.Object] = true
			for _, member := range typ.Object.Members {
				walk(member.Type)
			}
			return
		}
		switch {
		case compilerTypes.Equal(typ, compilerTypes.Float32):
			float32Used = true
		case compilerTypes.Equal(typ, compilerTypes.Float64):
			float64Used = true
		case compilerTypes.IsNil(typ):
			// A written Nil type needs the nullptr_t name from <stddef.h>.
			nilUsed = true
		}
	}
	walkExpression = func(node checker.Expression) {
		switch node.Kind {
		case checker.VariableExpression:
			return
		case checker.NullTestExpression:
			// The test writes the nullptr constant even when no written type
			// needs the nullptr_t name.
			nilUsed = true
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
		case checker.UnionInjectionExpression, checker.UnionWidenExpression,
			checker.UnionTestExpression, checker.UnionPayloadExpression,
			checker.UnionEqualityExpression, checker.HeapAllocateExpression,
			checker.AdtConstructExpression, checker.AdtPayloadExpression,
			checker.MatchExpression:
			walk(node.Element)
			walk(node.OperandType)
			walk(node.ResultType)
			walk(node.TestType)
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
			if node.Left != nil {
				walkExpression(*node.Left)
			}
			if node.Right != nil {
				walkExpression(*node.Right)
			}
		case checker.FunctionReferenceExpression:
			walk(node.ResultType)
		case checker.CallExpression:
			walk(node.OperandType)
			walk(node.ResultType)
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
			for _, argument := range node.Arguments {
				walkOperand(argument)
			}
		case checker.MethodCallExpression:
			walk(node.OperandType)
			walk(node.ResultType)
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
			for _, argument := range node.Arguments {
				walkOperand(argument)
			}
		case checker.AddressOfExpression, checker.DereferenceExpression:
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
		case checker.ArrayLiteralExpression, checker.IndexExpression, checker.CollectionMethodCallExpression, checker.CollectionSliceExpression:
			walk(node.OperandType)
			walk(node.ResultType)
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
			for _, argument := range node.Arguments {
				walkOperand(argument)
			}
		case checker.StringLiteralExpression, checker.StringMethodCallExpression, checker.StringFromBytesExpression, checker.StringFromRunesExpression, checker.RuneCursorMethodCallExpression, checker.ListNewExpression, checker.DictNewExpression,
			checker.StreamConstructorExpression, checker.StreamMethodCallExpression, checker.BitCastExpression, checker.EndianConversionExpression, checker.TryExpression,
			checker.DeepEqualityExpression, checker.StringCompareExpression, checker.WideningExpression, checker.ConversionExpression:
			walk(node.OperandType)
			walk(node.ResultType)
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
			for _, argument := range node.Arguments {
				walkOperand(argument)
			}
		case checker.MemberExpression:
			if node.Member != nil {
				walk(node.Member.Type)
			}
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
		case checker.ObjectExpression:
			if node.Object != nil {
				walk(node.Object.Type)
				for _, initializer := range node.Object.Initializers {
					walkOperand(initializer.Source)
				}
			}
		case checker.ConstantExpression:
			if node.Constant != nil {
				walkOperand(*node.Constant)
			}
		case checker.UnaryOperationExpression:
			walk(node.OperandType)
			walk(node.ResultType)
			if node.Operand != nil {
				walkExpression(*node.Operand)
			}
		case checker.BinaryOperationExpression:
			walk(node.OperandType)
			walk(node.ResultType)
			if node.Left != nil {
				walkExpression(*node.Left)
			}
			if node.Right != nil {
				walkExpression(*node.Right)
			}
		case checker.InvalidExpression:
			return
		default:
			return
		}
	}
	walkOperand = func(source checker.Operand) {
		walk(source.Type)
		switch source.Kind {
		case checker.ObjectOperand:
			if source.Object != nil {
				walk(source.Object.Type)
				for _, initializer := range source.Object.Initializers {
					walkOperand(initializer.Source)
				}
			}
		case checker.VariableOperand, checker.ExpressionOperand:
			walkExpression(source.Node)
		case checker.ConstantOperand, checker.InvalidOperand:
			return
		default:
			return
		}
	}
	var walkStatements func([]checker.Statement)
	walkStatements = func(statements []checker.Statement) {
		for _, statement := range statements {
			switch statement := statement.(type) {
			case checker.Declaration:
				walk(statement.Type)
				walkOperand(statement.Source)
			case checker.Assignment:
				walk(statement.Type)
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
			case checker.WhileStatement:
				walkOperand(statement.Condition)
				walkStatements(statement.Body)
			case checker.BreakStatement, checker.ContinueStatement:
				continue
			case checker.FunctionDeclaration:
				walk(statement.Type)
				for _, parameter := range statement.Parameters {
					walk(parameter.Type)
				}
				if statement.Result != nil {
					walk(*statement.Result)
				}
				walkStatements(statement.Body)
			case checker.MethodDeclaration:
				walk(statement.SelfType)
				for _, parameter := range statement.Parameters {
					walk(parameter.Type)
				}
				if statement.Result != nil {
					walk(*statement.Result)
				}
				walkStatements(statement.Body)
			}
		}
	}
	for _, declaration := range program.TypeDeclarations {
		walk(declaration.Type)
	}
	walkStatements(program.Statements)
	return float32Used, float64Used, nilUsed
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
		fmt.Fprintf(&result, "\n        .%s = %s,", PrivateCName(MemberName, member.Name), rendered)
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

func validateIntegerConstant(source checker.Operand) error {
	if source.Kind != checker.ConstantOperand || source.Constant == nil || source.Constant.Kind() != constant.Int || !supportedGeneratedScalarType(source.Type) || !compilerTypes.IsInteger(source.Type) {
		return unknownExpressionDiagnostic("invalid checked integer constant")
	}
	if source.Radix < checker.DecimalRadix || source.Radix > checker.OctalRadix {
		return unknownExpressionDiagnostic("checked integer has an invalid radix")
	}
	value := source.Constant
	sign := constant.Sign(value)
	if compilerTypes.IsUnsignedInteger(source.Type) && (source.Negative || sign < 0) {
		return unknownExpressionDiagnostic("negative value for an unsigned integer constant")
	}
	if source.Negative && sign > 0 || !source.Negative && sign < 0 {
		return unknownExpressionDiagnostic("integer sign metadata does not match its checked value")
	}
	// Folded constants carry no literal text; only original literals are
	// re-validated against their value.
	if source.Literal == "" {
		return nil
	}
	magnitude, literalNegative, ok := parseIntegerLiteral(source.Literal, source.Radix)
	if !ok {
		return unknownExpressionDiagnostic("checked integer has an invalid literal value")
	}
	if literalNegative && !source.Negative {
		return unknownExpressionDiagnostic("integer literal sign does not match its checked metadata")
	}
	if source.Negative {
		literalValue := constant.UnaryOp(gotoken.SUB, magnitude, 0)
		if !constant.Compare(value, gotoken.EQL, literalValue) {
			return unknownExpressionDiagnostic("checked integer literal does not match its value")
		}
	} else if !constant.Compare(value, gotoken.EQL, magnitude) {
		return unknownExpressionDiagnostic("checked integer literal does not match its value")
	}
	return nil
}

func parseIntegerLiteral(literal string, radix checker.LiteralRadix) (constant.Value, bool, bool) {
	literal = strings.ReplaceAll(literal, "_", "")
	if literal == "" {
		return nil, false, false
	}
	negative := strings.HasPrefix(literal, "-")
	if negative {
		literal = literal[1:]
	}
	if literal == "" || strings.HasPrefix(literal, "+") {
		return nil, false, false
	}
	base := 10
	switch radix {
	case checker.DecimalRadix:
		if strings.HasPrefix(literal, "0x") || strings.HasPrefix(literal, "0b") || strings.HasPrefix(literal, "0o") {
			return nil, false, false
		}
	case checker.HexadecimalRadix:
		if !strings.HasPrefix(literal, "0x") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 16
	case checker.BinaryRadix:
		if !strings.HasPrefix(literal, "0b") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 2
	case checker.OctalRadix:
		if !strings.HasPrefix(literal, "0o") {
			return nil, false, false
		}
		literal = literal[2:]
		base = 8
	default:
		return nil, false, false
	}
	if literal == "" {
		return nil, false, false
	}
	value, err := strconv.ParseUint(literal, base, 64)
	if err != nil {
		return nil, false, false
	}
	return constant.MakeUint64(value), negative, true
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

// GenerateFailure emits a complete C program that reports compilation
// failure, while retaining the target-profile header shape.
func GenerateFailure() (mainC string, mainH string) {
	return "#include \"main.h\"\n\nint main(void) {\n    return EXIT_FAILURE;\n}\n", header(false, false, false, nil)
}

// writeSpecializedPrototypes emits one static prototype per concrete
// specialization so definitions can reference later specializations.
func writeSpecializedPrototypes(body *strings.Builder, functions []checker.FunctionDeclaration, methods []checker.MethodDeclaration, typeState *generatedTypeValidation) error {
	emitted := 0
	for _, declared := range functions {
		signature := declared.Type.Signature
		if signature == nil || !validateGeneratedType(declared.Type, typeState, false) {
			return unknownExpressionDiagnostic("specialized function without a checked Fun type")
		}
		resultSpelling := "void"
		if declared.Result != nil {
			if declared.Result.Signature != nil || !validateGeneratedType(*declared.Result, typeState, false) {
				return unknownExpressionDiagnostic("unsupported specialized function result type")
			}
			resultSpelling = typeSpelling(*declared.Result)
		}
		parameters := make([]string, len(declared.Parameters))
		for index, parameter := range declared.Parameters {
			if !validateGeneratedType(parameter.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported specialized function parameter type")
			}
			parameters[index] = typeSpelling(parameter.Type)
		}
		fmt.Fprintf(body, "static %s %s(%s);\n", resultSpelling, PrivateCName(FunctionName, declared.Name), parameterList(parameters))
		emitted++
	}
	for _, declared := range methods {
		if declared.Object == nil || !validateGeneratedType(declared.SelfType, typeState, false) {
			return unknownExpressionDiagnostic("specialized method without checked receiver metadata")
		}
		resultSpelling := "void"
		if declared.Result != nil {
			if declared.Result.Signature != nil || !validateGeneratedType(*declared.Result, typeState, false) {
				return unknownExpressionDiagnostic("unsupported specialized method result type")
			}
			resultSpelling = typeSpelling(*declared.Result)
		}
		parameters := make([]string, 0, len(declared.Parameters)+1)
		parameters = append(parameters, typeSpelling(declared.SelfType))
		for _, parameter := range declared.Parameters {
			if !validateGeneratedType(parameter.Type, typeState, false) {
				return unknownExpressionDiagnostic("unsupported specialized method parameter type")
			}
			parameters = append(parameters, typeSpelling(parameter.Type))
		}
		fmt.Fprintf(body, "static %s %s(%s);\n", resultSpelling, methodCName(declared.Object, declared.Name), parameterList(parameters))
		emitted++
	}
	if emitted > 0 {
		body.WriteString("\n")
	}
	return nil
}

// writeSpecializedDefinitions emits the concrete bodies of every
// specialization in cache order.
func writeSpecializedDefinitions(body *strings.Builder, functions []checker.FunctionDeclaration, methods []checker.MethodDeclaration, functionsTable map[string]compilerTypes.Type, methodsTable map[string]checker.MethodDeclaration, typeState *generatedTypeValidation, stringState *generatedStringState) error {
	for _, declared := range functions {
		if definitionErr := writeFunctionDefinition(body, declared, functionsTable, methodsTable, typeState, stringState); definitionErr != nil {
			return definitionErr
		}
	}
	for _, declared := range methods {
		if definitionErr := writeMethodDefinition(body, declared, functionsTable, methodsTable, typeState, stringState); definitionErr != nil {
			return definitionErr
		}
	}
	return nil
}
