package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	compilerTypes "hexal/compiler/types"
)

// FallsThrough reports whether a checked statement sequence has a normal path
// that can reach its end. The generator reuses this conservative proof when it
// validates forged checked programs.
func FallsThrough(statements []Statement) bool {
	for _, statement := range statements {
		if !statementFallsThrough(statement) {
			return false
		}
	}
	return true
}

func statementFallsThrough(statement Statement) bool {
	switch statement := statement.(type) {
	case ReturnStatement:
		return false
	case IfStatement:
		if statement.Else == nil || FallsThrough(statement.Then) {
			return true
		}
		for _, branch := range statement.ElseIf {
			if FallsThrough(branch.Body) {
				return true
			}
		}
		return FallsThrough(statement.Else)
	case WhileStatement, ForStatement:
		return true
	case BreakStatement, ContinueStatement:
		return true
	default:
		return true
	}
}

// checkBody checks a function or method body with no active loop at entry.
func checkBody(statements []parser.Statement, names *scope, typeEnvironment *compilerTypes.Environment) ([]Statement, compilerTypes.Diagnostics) {
	return checkStatements(statements, names, typeEnvironment, 0)
}

// checkStatements recursively checks one lexical statement sequence. A child
// scope is supplied by the control-flow handlers; declarations are installed
// only in the current frame after their own diagnostics have cleared.
func checkStatements(statements []parser.Statement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) ([]Statement, compilerTypes.Diagnostics) {
	checked := make([]Statement, 0, len(statements))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	reachable := true
	for _, statement := range statements {
		returnFlowCount := len(names.returnFlows)
		// A statement that fails mid-check may already have mutated flow
		// facts (narrowing, escapes, freed marks). Restore the pre-statement
		// snapshot so a failed statement contributes no fact to a later
		// diagnostic; the successful statement's resulting state is kept.
		var flowSnapshot *flowState
		if names.flow != nil {
			flowSnapshot = names.flow.clone()
		}
		checkedStatement, declaredBinding, define, statementDiagnostics := checkStatement(statement, names, typeEnvironment, loopDepth)
		diagnostics = append(diagnostics, statementDiagnostics...)
		if len(statementDiagnostics) != 0 {
			names.returnFlows = names.returnFlows[:returnFlowCount]
			names.flow = flowSnapshot
			continue
		}
		if define {
			declaration := statement.(parser.Declaration)
			names.define(declaration.Name.Lexeme, declaredBinding)
		}
		if checkedStatement == nil {
			// A generic local function declaration checked clean: like its
			// module-level equivalent, it has no concrete function of its
			// own and emits nothing, so it never joins the executable
			// statement list or affects reachability.
			continue
		}
		checked = append(checked, checkedStatement)
		if reachable {
			if _, returns := checkedStatement.(ReturnStatement); returns {
				names.recordReturnFlow()
			}
		} else {
			// The statement was checked for its own diagnostics, but it cannot
			// add a return path after an earlier terminator.
			names.returnFlows = names.returnFlows[:returnFlowCount]
		}
		if reachable && statementTerminates(checkedStatement) {
			reachable = false
		}
	}
	diagnostics = append(diagnostics, validateDeferredActions(names, !sequenceTerminates(checked))...)
	return checked, diagnostics
}

func checkStatement(statement parser.Statement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) (Statement, binding, bool, compilerTypes.Diagnostics) {
	switch statement := statement.(type) {
	case parser.Declaration:
		if literal, isSugar := directFunctionLiteralSugar(statement); isSugar {
			// A direct inferred fixed literal declaration is checked as the
			// equivalent local named function declaration; the name is
			// registered inside checkLocalFunctionDeclaration itself, before
			// its own body is checked, so no post-hoc define is needed here.
			checked, diagnostics := checkLocalFunctionDeclaration(asLocalFunctionDeclaration(statement.Name, literal), names, typeEnvironment)
			if len(diagnostics) == 0 && checked.Type == (compilerTypes.Type{}) {
				return nil, binding{}, false, nil
			}
			return checked, binding{}, false, diagnostics
		}
		checked, declared, diagnostics := checkDeclaration(statement, names, typeEnvironment)
		return checked, declared, true, diagnostics
	case parser.Assignment:
		checked, diagnostics := checkAssignment(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.CallExpression:
		checked, diagnostics := checkCallStatement(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.ReturnStatement:
		checked, diagnostics := checkReturnStatement(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.IfStatement:
		checked, diagnostics := checkIfStatement(statement, names, typeEnvironment, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.WhileStatement:
		checked, diagnostics := checkWhileStatement(statement, names, typeEnvironment, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.ForStatement:
		checked, diagnostics := checkForStatement(statement, names, typeEnvironment, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.BreakStatement:
		if loopDepth == 0 {
			return BreakStatement{}, binding{}, false, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "break is only valid inside a loop")}
		}
		return BreakStatement{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}, binding{}, false, nil
	case parser.ContinueStatement:
		if loopDepth == 0 {
			return ContinueStatement{}, binding{}, false, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "continue is only valid inside a loop")}
		}
		return ContinueStatement{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}, binding{}, false, nil
	case parser.DeferStatement:
		checked, diagnostics := checkDeferStatement(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.ErrdeferStatement:
		checked, diagnostics := checkErrdeferStatement(statement, names, typeEnvironment)
		return checked, binding{}, false, diagnostics
	case parser.LocalFunctionDeclaration:
		// The declaration binds its own name into names.local before its
		// body is checked, exactly like a module function; unlike an
		// ordinary Declaration, no post-hoc define call is needed here.
		checked, diagnostics := checkLocalFunctionDeclaration(statement, names, typeEnvironment)
		if len(diagnostics) == 0 && checked.Type == (compilerTypes.Type{}) {
			// A generic declaration is an open template with no concrete
			// function of its own; it emits nothing, exactly like its
			// module-level equivalent.
			return nil, binding{}, false, nil
		}
		return checked, binding{}, false, diagnostics
	case parser.TryStatement:
		// A try statement reuses the try-expression validation and
		// propagation metadata; the success value is discarded.
		checkedTry := checkTryExpression(parser.TryExpression{Keyword: statement.Keyword, Operand: statement.Operand}, expressionContext{}, names, typeEnvironment)
		if diagnostics := initializerDiagnostics(checkedTry); len(diagnostics) > 0 {
			return nil, binding{}, false, diagnostics
		}
		return TryStatement{
			Expression:   checkedTry.source,
			SourceLine:   statement.Keyword.Line,
			SourceColumn: statement.Keyword.Column,
		}, binding{}, false, nil
	default:
		// Exhaustive over parser.Statement today; a new statement form
		// reaching this default is a compiler inconsistency and reports
		// [Unknown Error], never a user category.
		return nil, binding{}, false, compilerTypes.Diagnostics{
			unknownAt(lexer.Token{Line: 1, Column: 1}, "unsupported checked control-flow statement"),
		}
	}
}

func checkCondition(expression parser.Expression, names *scope, typeEnvironment *compilerTypes.Environment) (Operand, *Operand, lexer.Token, compilerTypes.Diagnostics) {
	checked := checkValue(expression, names, typeEnvironment)
	if diagnostics := initializerDiagnostics(checked); len(diagnostics) > 0 {
		return checked.source, nil, checked.token, diagnostics
	}
	// Every value-producing expression is a valid condition; its truthiness
	// decides the branch. No-result calls are rejected by checkValue before
	// this point. The known-value metadata of a named immutable binding read
	// is returned for constant-required consumers; the condition itself stays
	// the binding read.
	return checked.source, checked.known, checked.token, nil
}

// narrowingFact is the branch-local fact a checked null test proves about one
// binding: typ holds in the true branch, other holds in the false branch.
type narrowingFact struct {
	binding BindingID
	typ     compilerTypes.Type
	other   compilerTypes.Type
}

// conditionNarrowing extracts a branch-local fact from an explicit Nil or
// exact-member test. Only a bare local binding narrows; member paths and
// logical combinations remain non-narrowable.
func conditionNarrowing(condition Operand, state *flowState, typeEnvironment *compilerTypes.Environment) *narrowingFact {
	if state == nil || condition.Kind != ExpressionOperand {
		return nil
	}
	node := condition.Node
	if node.Kind != NullTestExpression && node.Kind != UnionTestExpression {
		return nil
	}
	operand := node.Operand
	if operand == nil || operand.Kind != VariableExpression || operand.Binding == 0 {
		return nil
	}
	if fact, exists := state.facts[operand.Binding]; exists && fact.escaped {
		return nil
	}
	if node.Kind == UnionTestExpression {
		other, ok := compilerTypes.RemoveUnionMember(typeEnvironment, node.OperandType, node.TestType)
		if !ok {
			return nil
		}
		return &narrowingFact{binding: operand.Binding, typ: node.TestType, other: other}
	}
	other, ok := compilerTypes.RemoveUnionMember(typeEnvironment, node.OperandType, compilerTypes.Nil)
	if !ok {
		return nil
	}
	if node.Operator == NotEqualOperator {
		return &narrowingFact{binding: operand.Binding, typ: other, other: compilerTypes.Nil}
	}
	if node.Operator == EqualOperator {
		return &narrowingFact{binding: operand.Binding, typ: compilerTypes.Nil, other: other}
	}
	return nil
}

func checkIfStatement(statement parser.IfStatement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) (IfStatement, compilerTypes.Diagnostics) {
	checked := IfStatement{
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
		EndLine:      statement.End.Line,
		EndColumn:    statement.End.Column,
		ElseLine:     statement.ElseKeyword.Line,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	condition, _, conditionToken, conditionDiagnostics := checkCondition(statement.Condition, names, typeEnvironment)
	diagnostics = append(diagnostics, conditionDiagnostics...)
	checked.Condition = condition
	checked.ConditionLine = conditionToken.Line
	checked.ConditionColumn = conditionToken.Column

	// Each branch checks a clone of the pre-test flow carrying its own
	// narrowing fact. Invalidations from a clean branch merge into the
	// pre-test flow only after every branch is checked, so the else side of
	// the construct never observes the then side's effects. Cloning is
	// unconditional so owning-state transitions inside one branch can never
	// leak into a sibling or the continuing path; the strict owner merge
	// then detects disagreements exactly.
	var fact *narrowingFact
	if len(conditionDiagnostics) == 0 {
		fact = conditionNarrowing(condition, names.flow, typeEnvironment)
	}
	parentState := names.flow
	thenState := parentState
	elseState := parentState
	if parentState != nil {
		thenState = parentState.clone()
		elseState = parentState.clone()
		if fact != nil {
			thenState.narrow(fact.binding, fact.typ)
			elseState.narrow(fact.binding, fact.other)
		}
	}

	thenScope := names.child()
	thenScope.flow = thenState
	thenBody, thenDiagnostics := checkStatements(statement.Then, thenScope, typeEnvironment, loopDepth)
	diagnostics = append(diagnostics, thenDiagnostics...)
	checked.Then = thenBody
	checked.ThenDefers = append(checked.ThenDefers, thenScope.defers...)
	if len(thenDiagnostics) == 0 {
		names.recordChildReturnFlows(thenScope.returnFlows)
	}

	continuing := make([]*flowState, 0, len(statement.ElseIf)+2)
	if len(thenDiagnostics) == 0 && !sequenceTerminates(thenBody) && thenState != nil {
		continuing = append(continuing, thenState)
	}

	for _, branch := range statement.ElseIf {
		// An elseif condition is checked where every previous condition was
		// false, so its state is the else-side chain; each body narrows a
		// clone of that chain and only its invalidations merge onward.
		conditionScope := names.child()
		conditionScope.flow = elseState
		branchCondition, _, branchToken, branchConditionDiagnostics := checkCondition(branch.Condition, conditionScope, typeEnvironment)
		diagnostics = append(diagnostics, branchConditionDiagnostics...)
		// Always clone the else-side chain for the branch body: its own
		// invalidations must not leak into the next elseif condition, and they
		// must still merge into the pre-test flow even when this condition
		// narrows nothing (otherwise a missing final else would drop them).
		branchState := elseState
		if elseState != nil {
			branchState = elseState.clone()
			if len(branchConditionDiagnostics) == 0 {
				if branchFact := conditionNarrowing(branchCondition, elseState, typeEnvironment); branchFact != nil {
					branchState.narrow(branchFact.binding, branchFact.typ)
					nextElseState := elseState.clone()
					nextElseState.narrow(branchFact.binding, branchFact.other)
					elseState = nextElseState
				}
			}
		}
		branchScope := names.child()
		branchScope.flow = branchState
		branchBody, branchDiagnostics := checkStatements(branch.Body, branchScope, typeEnvironment, loopDepth)
		diagnostics = append(diagnostics, branchDiagnostics...)
		checked.ElseIfDefers = append(checked.ElseIfDefers, append([]DeferredAction(nil), branchScope.defers...))
		if len(branchDiagnostics) == 0 {
			names.recordChildReturnFlows(branchScope.returnFlows)
		}
		checked.ElseIf = append(checked.ElseIf, IfBranch{
			Condition:       branchCondition,
			ConditionLine:   branchToken.Line,
			ConditionColumn: branchToken.Column,
			Body:            branchBody,
			SourceLine:      branch.Keyword.Line,
			SourceColumn:    branch.Keyword.Column,
		})
		if len(branchDiagnostics) == 0 && !sequenceTerminates(branchBody) && branchState != nil {
			continuing = append(continuing, branchState)
		}
	}
	if statement.Else != nil {
		elseScope := names.child()
		elseScope.flow = elseState
		elseBody, elseDiagnostics := checkStatements(statement.Else, elseScope, typeEnvironment, loopDepth)
		diagnostics = append(diagnostics, elseDiagnostics...)
		checked.Else = elseBody
		checked.ElseDefers = append(checked.ElseDefers, elseScope.defers...)
		if len(elseDiagnostics) == 0 {
			names.recordChildReturnFlows(elseScope.returnFlows)
		}
		if len(elseDiagnostics) == 0 && !sequenceTerminates(elseBody) && elseState != nil {
			continuing = append(continuing, elseState)
		}
	} else if elseState != nil {
		// A missing else is the implicit false path. Its narrowing survives
		// only when it is the sole continuation; cleanup facts use it in the
		// same conjunction as every explicit continuing path.
		continuing = append(continuing, elseState)
	}
	if parentState != nil {
		switch len(continuing) {
		case 1:
			parentState.adopt(continuing[0])
		case 2:
			parentState.mergeBranches(continuing...)
		default:
			if len(continuing) > 2 {
				parentState.mergeBranches(continuing...)
			}
		}
	}
	return checked, diagnostics
}

// sequenceTerminates reports whether a checked statement sequence provably
// ends the current path with break, continue, or return before its end.
func sequenceTerminates(statements []Statement) bool {
	for _, statement := range statements {
		if statementTerminates(statement) {
			return true
		}
	}
	return false
}

func statementTerminates(statement Statement) bool {
	switch statement := statement.(type) {
	case ReturnStatement, BreakStatement, ContinueStatement:
		return true
	case IfStatement:
		// Every branch must terminate for the if itself to terminate.
		if statement.Else == nil {
			return false
		}
		if !sequenceTerminates(statement.Then) {
			return false
		}
		for _, branch := range statement.ElseIf {
			if !sequenceTerminates(branch.Body) {
				return false
			}
		}
		return sequenceTerminates(statement.Else)
	default:
		return false
	}
}

// checkForStatement checks the for-in form: the source must be one
// iterable concrete type, the binder arity must match the source kind, and
// every binder is a fresh immutable binding in a fresh body scope.
func checkForStatement(statement parser.ForStatement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) (ForStatement, compilerTypes.Diagnostics) {
	checked := ForStatement{
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)

	seen := make(map[string]bool, len(statement.Binders))
	for _, binder := range statement.Binders {
		if seen[binder.Lexeme] {
			diagnostics = append(diagnostics, nameErrorAt(binder, "duplicate loop binder name "+binder.Lexeme))
		}
		seen[binder.Lexeme] = true
	}

	// The source is read as a value but keeps its place addressability when
	// it names storage: the generator iterates an Array place in place and
	// only materializes genuine temporaries.
	var source checkedExpression
	switch statement.Source.(type) {
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		source = checkPlace(statement.Source, names, typeEnvironment)
	default:
		source = checkExpression(statement.Source, expressionContext{foldConstants: false}, names, typeEnvironment)
	}
	if source.diagnostic != nil {
		return checked, append(diagnostics, *source.diagnostic)
	}
	if diagnosticsFromSource := initializerDiagnostics(source); len(diagnosticsFromSource) > 0 {
		return checked, append(diagnostics, diagnosticsFromSource...)
	}

	binderTypes, arityDiagnostic := forBinderTypes(source.typ, statement.Binders)
	if arityDiagnostic != nil {
		return checked, append(diagnostics, *arityDiagnostic)
	}
	if len(binderTypes) != len(statement.Binders) {
		return checked, append(diagnostics, typeErrorAt(statement.Keyword, "for-in binder count does not match the source type"))
	}

	parentState := names.flow
	bodyState := parentState
	if parentState != nil {
		bodyState = parentState.clone()
	}
	bodyScope := names.child()
	bodyScope.flow = bodyState
	for index, binder := range statement.Binders {
		binderType := binderTypes[index]
		bound := binding{typ: binderType, use: compilerTypes.NewTypeUse(binderType), loopBinder: true, id: names.newBindingID()}
		bodyScope.local[binder.Lexeme] = bound
		checked.Binders = append(checked.Binders, ForBinder{
			Name:         binder.Lexeme,
			Type:         binderType,
			Binding:      bound.id,
			SourceLine:   binder.Line,
			SourceColumn: binder.Column,
		})
	}

	body, bodyDiagnostics := checkStatements(statement.Body, bodyScope, typeEnvironment, loopDepth+1)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = body
	checked.BodyDefers = append(checked.BodyDefers, bodyScope.defers...)
	if len(bodyDiagnostics) == 0 {
		names.recordChildReturnFlows(bodyScope.returnFlows)
	}
	checked.Source = source.source
	// Iterator invalidation: direct structural mutations and frees through any
	// copied handle are rejected when the active traversal can identify them;
	// other copied-handle mutations remain defined by the generated version
	// check. Every checked expression in the body is scanned, not only calls in
	// statement position.
	if len(bodyDiagnostics) == 0 && (source.typ.List != nil || source.typ.Dict != nil) {
		if binding := baseBindingID(&source.source.Node); binding != 0 {
			root := collectionRootForOperand(source.source, names, binding)
			if mutationDiagnostics := checkForIterationMutations(binding, root, source.typ, body); len(mutationDiagnostics) > 0 {
				diagnostics = append(diagnostics, mutationDiagnostics...)
				return checked, diagnostics
			}
		}
	}
	if len(bodyDiagnostics) == 0 && parentState != nil && bodyState != nil {
		parentState.mergeBranches(parentState.clone(), bodyState)
	}
	return checked, diagnostics
}

// forBinderTypes resolves the binder list for one iterable source type. The
// returned slice matches the written binders one to one; a count mismatch
// reports the arity diagnostic.
func forBinderTypes(source compilerTypes.Type, binders []lexer.Token) ([]compilerTypes.Type, *compilerTypes.Diagnostic) {
	switch {
	case source.Array != nil || source.View != nil || source.List != nil:
		var element compilerTypes.Type
		if source.Array != nil {
			element = source.Array.Element
		} else if source.View != nil {
			element = source.View.Element
		} else {
			element = source.List.Element
		}
		switch len(binders) {
		case 1:
			return []compilerTypes.Type{element}, nil
		case 2:
			return []compilerTypes.Type{compilerTypes.SizeType, element}, nil
		default:
			diagnostic := typeErrorAt(binders[0], "sequence iteration requires one value binder or index and value binders")
			return nil, &diagnostic
		}
	case compilerTypes.IsString(source) || compilerTypes.IsStrand(source):
		switch len(binders) {
		case 1:
			return []compilerTypes.Type{compilerTypes.Rune}, nil
		case 2:
			return []compilerTypes.Type{compilerTypes.SizeType, compilerTypes.Rune}, nil
		default:
			diagnostic := typeErrorAt(binders[0], "sequence iteration requires one value binder or index and value binders")
			return nil, &diagnostic
		}
	case source.Dict != nil:
		switch len(binders) {
		case 2:
			return []compilerTypes.Type{source.Dict.Key, source.Dict.Value}, nil
		case 3:
			return []compilerTypes.Type{compilerTypes.SizeType, source.Dict.Key, source.Dict.Value}, nil
		default:
			diagnostic := typeErrorAt(binders[0], "dictionary iteration requires key and value binders or index, key, and value binders")
			return nil, &diagnostic
		}
	default:
		diagnostic := typeErrorAt(binders[0], "value of type "+source.Name+" is not iterable")
		return nil, &diagnostic
	}
}

// checkForIterationMutations walks every checked statement and expression in
// a traversal body. A free through any copied handle is rejected because a
// later version check would itself dereference freed storage. Other direct
// structural mutations are rejected when they use the loop source binding;
// copied-handle mutations remain defined by the generated version check.
func checkForIterationMutations(sourceBinding, sourceRoot BindingID, collectionType compilerTypes.Type, body []Statement) compilerTypes.Diagnostics {
	scanner := iterationMutationScanner{
		sourceBinding:  sourceBinding,
		sourceRoot:     sourceRoot,
		collectionType: collectionType,
	}
	scanner.walkStatements(body)
	return scanner.diagnostics
}

type iterationMutationScanner struct {
	sourceBinding  BindingID
	sourceRoot     BindingID
	collectionType compilerTypes.Type
	diagnostics    compilerTypes.Diagnostics
}

func (scanner *iterationMutationScanner) report(line, column int, message string) {
	scanner.diagnostics = append(scanner.diagnostics, typeErrorAt(lexer.Token{Line: line, Column: column}, message))
}

func (scanner *iterationMutationScanner) walkStatements(statements []Statement) {
	for _, statement := range statements {
		switch node := statement.(type) {
		case Declaration:
			scanner.walkOperand(node.Source, node.SourceLine, node.SourceColumn)
		case Assignment:
			scanner.walkOperand(node.Target, node.SourceLine, node.SourceColumn)
			scanner.walkOperand(node.Source, node.SourceLine, node.SourceColumn)
		case CallStatement:
			scanner.walkOperand(node.Call, node.SourceLine, node.SourceColumn)
		case ReturnStatement:
			if node.Value != nil {
				scanner.walkOperand(*node.Value, node.SourceLine, node.SourceColumn)
			}
		case TryStatement:
			scanner.walkOperand(node.Expression, node.SourceLine, node.SourceColumn)
		case DeferStatement:
			scanner.walkOperand(node.Expression, node.SourceLine, node.SourceColumn)
		case ErrdeferStatement:
			scanner.walkOperand(node.Expression, node.SourceLine, node.SourceColumn)
		case IfStatement:
			scanner.walkOperand(node.Condition, node.ConditionLine, node.ConditionColumn)
			scanner.walkStatements(node.Then)
			for _, branch := range node.ElseIf {
				scanner.walkOperand(branch.Condition, branch.SourceLine, branch.SourceColumn)
				scanner.walkStatements(branch.Body)
			}
			scanner.walkStatements(node.Else)
		case WhileStatement:
			scanner.walkOperand(node.Condition, node.ConditionLine, node.ConditionColumn)
			scanner.walkStatements(node.Body)
		case ForStatement:
			scanner.walkOperand(node.Source, node.SourceLine, node.SourceColumn)
			scanner.walkStatements(node.Body)
		case FunctionDeclaration:
			scanner.walkStatements(node.Body)
		case MethodDeclaration:
			scanner.walkStatements(node.Body)
		}
	}
}

func (scanner *iterationMutationScanner) walkOperand(operand Operand, line, column int) {
	scanner.walkExpression(&operand.Node, line, column)
	if operand.Object != nil {
		for _, member := range operand.Object.Initializers {
			scanner.walkOperand(member.Source, line, column)
		}
	}
}

func (scanner *iterationMutationScanner) walkExpression(node *Expression, line, column int) {
	if node == nil {
		return
	}
	switch node.Kind {
	case CollectionMethodCallExpression:
		receiverRoot := collectionRootOfNode(node.Operand)
		if receiverRoot == scanner.sourceRoot && node.OperandType == scanner.collectionType {
			switch node.Name {
			case "free":
				scanner.report(line, column, "cannot free collection during iteration")
			case "push", "pop", "clear", "insert", "remove":
				if baseBindingID(node.Operand) == scanner.sourceBinding {
					scanner.report(line, column, "cannot mutate collection during iteration")
				}
			}
		}
	case CallExpression, MethodCallExpression:
		if scanner.callReceivesSource(node) {
			scanner.report(line, column, "cannot pass traversed collection to call during iteration")
		}
	}

	scanner.walkExpression(node.Operand, line, column)
	scanner.walkExpression(node.Left, line, column)
	scanner.walkExpression(node.Right, line, column)
	for _, argument := range node.Arguments {
		scanner.walkOperand(argument, line, column)
	}
	if node.Constant != nil {
		scanner.walkOperand(*node.Constant, line, column)
	}
}

func (scanner *iterationMutationScanner) callReceivesSource(node *Expression) bool {
	if node.Operand != nil && node.Kind == MethodCallExpression && scanner.collectionOperand(node.Operand, node.OperandType) {
		return true
	}
	for _, argument := range node.Arguments {
		if scanner.collectionOperand(&argument.Node, argument.Type) {
			return true
		}
	}
	return false
}

func (scanner *iterationMutationScanner) collectionOperand(node *Expression, typ compilerTypes.Type) bool {
	return isTrackedCollection(typ) && collectionRootOfNode(node) == scanner.sourceRoot
}

func checkWhileStatement(statement parser.WhileStatement, names *scope, typeEnvironment *compilerTypes.Environment, loopDepth int) (WhileStatement, compilerTypes.Diagnostics) {
	checked := WhileStatement{
		SourceLine:   statement.Keyword.Line,
		SourceColumn: statement.Keyword.Column,
		EndLine:      statement.End.Line,
		EndColumn:    statement.End.Column,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	condition, conditionKnown, conditionToken, conditionDiagnostics := checkCondition(statement.Condition, names, typeEnvironment)
	diagnostics = append(diagnostics, conditionDiagnostics...)
	checked.Condition = condition
	checked.ConditionKnown = conditionKnown
	checked.ConditionLine = conditionToken.Line
	checked.ConditionColumn = conditionToken.Column

	// The condition's narrowing holds for the body. The parent state is also
	// a zero-iteration path, so a body free cannot become definite after the
	// loop merely because one iteration can execute it.
	parentState := names.flow
	bodyState := parentState
	if parentState != nil {
		bodyState = parentState.clone()
		if len(conditionDiagnostics) == 0 {
			if fact := conditionNarrowing(condition, parentState, typeEnvironment); fact != nil {
				bodyState.narrow(fact.binding, fact.typ)
			}
		}
	}
	bodyScope := names.child()
	bodyScope.flow = bodyState
	body, bodyDiagnostics := checkStatements(statement.Body, bodyScope, typeEnvironment, loopDepth+1)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = body
	checked.BodyDefers = append(checked.BodyDefers, bodyScope.defers...)
	if len(bodyDiagnostics) == 0 {
		names.recordChildReturnFlows(bodyScope.returnFlows)
	}
	if len(bodyDiagnostics) == 0 && parentState != nil && bodyState != nil {
		parentState.mergeBranches(parentState.clone(), bodyState)
	}
	return checked, diagnostics
}

func checkReturnStatement(statement parser.ReturnStatement, names *scope, typeEnvironment *compilerTypes.Environment) (ReturnStatement, compilerTypes.Diagnostics) {
	checked := ReturnStatement{SourceLine: statement.Keyword.Line, SourceColumn: statement.Keyword.Column}
	if !names.inFunction() {
		return checked, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "return is only valid inside a function body")}
	}
	if statement.Value == nil {
		if names.result != nil {
			return checked, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword,
				fmt.Sprintf("return requires a value; %s declares %s", names.owner, names.result.Name))}
		}
		return checked, nil
	}
	if names.result == nil {
		return checked, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, names.owner+" returns no value; use a bare return")}
	}

	resultUse := compilerTypes.NewTypeUse(*names.result)
	if names.resultUse != nil {
		resultUse = *names.resultUse
	}
	value := checkInitializer(statement.Value, resultUse, statement.Keyword, names, typeEnvironment)
	if valueDiagnostics := initializerDiagnostics(value); len(valueDiagnostics) > 0 {
		return checked, valueDiagnostics
	}
	if value.typ != (compilerTypes.Type{}) && !assignable(*names.result, value.typ) {
		return checked, compilerTypes.Diagnostics{typeErrorAt(value.token,
			fmt.Sprintf("%s returns %s; got %s", names.owner, names.result.Name, value.typ.Name))}
	}
	if value.typ != (compilerTypes.Type{}) {
		if diagnostic := atomicCopyDiagnostic(value.source, statement.Keyword); diagnostic != nil {
			return checked, compilerTypes.Diagnostics{*diagnostic}
		}
		if value.typ.View != nil {
			if diagnostic := viewReturnDiagnostic(value.source.Node, statement.Keyword, names); diagnostic != nil {
				return checked, compilerTypes.Diagnostics{*diagnostic}
			}
		}
	}
	source := value.source
	checked.Value = &source
	// A collection return value is an ordinary shallow copy; the caller
	// accepts the cleanup responsibility the function documents.
	return checked, nil
}
