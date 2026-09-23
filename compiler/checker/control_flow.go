package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	"hexal/compiler/parser"
	"hexal/compiler/span"
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
	case ReturnStatement, RootReturnStatement:
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
	case UnsafeStatement:
		return FallsThrough(statement.Body)
	default:
		return true
	}
}

// checkBody checks a function or method body with no active loop at entry.
func checkBody(statements []parser.Statement, ctx checkContext) ([]Statement, compilerTypes.Diagnostics) {
	return checkStatements(statements, ctx, 0)
}

// checkStatements recursively checks one lexical statement sequence. A child
// scope is supplied by the control-flow handlers; declarations are installed
// only in the current frame after their own diagnostics have cleared.
func checkStatements(statements []parser.Statement, ctx checkContext, loopDepth int) ([]Statement, compilerTypes.Diagnostics) {
	checked := make([]Statement, 0, len(statements))
	diagnostics := make(compilerTypes.Diagnostics, 0)
	reachable := true
	for _, statement := range statements {
		returnFlowCount := len(ctx.names.returnFlows)
		// A statement that fails mid-check may already have mutated flow
		// facts (narrowing, escapes, freed marks). Restore the pre-statement
		// snapshot so a failed statement contributes no fact to a later
		// diagnostic; the successful statement's resulting state is kept.
		var flowSnapshot *flowState
		if ctx.names.flow != nil {
			flowSnapshot = ctx.names.flow.clone()
		}
		checkedStatement, declaredBinding, define, statementDiagnostics := checkStatement(statement, ctx, loopDepth)
		diagnostics = append(diagnostics, statementDiagnostics...)
		if len(statementDiagnostics) != 0 {
			ctx.names.returnFlows = ctx.names.returnFlows[:returnFlowCount]
			ctx.names.flow = flowSnapshot
			continue
		}
		if define {
			declaration := statement.(parser.Declaration)
			ctx.names.define(declaration.Name.Lexeme, declaredBinding)
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
			switch checkedStatement.(type) {
			case ReturnStatement, RootReturnStatement:
				ctx.names.recordReturnFlow()
			}
		} else {
			// The statement was checked for its own diagnostics, but it cannot
			// add a return path after an earlier terminator.
			ctx.names.returnFlows = ctx.names.returnFlows[:returnFlowCount]
		}
		if reachable && statementTerminates(checkedStatement) {
			reachable = false
		}
	}
	diagnostics = append(diagnostics, validateDeferredActions(ctx.names, !sequenceTerminates(checked))...)
	return checked, diagnostics
}

func checkStatement(statement parser.Statement, ctx checkContext, loopDepth int) (Statement, binding, bool, compilerTypes.Diagnostics) {
	switch statement := statement.(type) {
	case parser.Declaration:
		// A direct inferred fixed literal declaration (`name := fun ...`) is
		// module-level declaration sugar only (see directFunctionLiteralSugar
		// and checkModule's Declaration case); at local scope a function
		// literal initializer is ordinary runtime data, checked like any
		// other expression.
		checked, declared, diagnostics := checkDeclaration(statement, ctx, -1, nil)
		return checked, declared, true, diagnostics
	case parser.Assignment:
		checked, diagnostics := checkAssignment(statement, ctx)
		return checked, binding{}, false, diagnostics
	case parser.CallExpression:
		checked, diagnostics := checkCallStatement(statement, ctx)
		return checked, binding{}, false, diagnostics
	case parser.ReturnStatement:
		checked, diagnostics := checkReturnStatement(statement, ctx)
		return checked, binding{}, false, diagnostics
	case parser.IfStatement:
		checked, diagnostics := checkIfStatement(statement, ctx, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.WhileStatement:
		checked, diagnostics := checkWhileStatement(statement, ctx, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.ForStatement:
		checked, diagnostics := checkForStatement(statement, ctx, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.UnsafeStatement:
		checked, diagnostics := checkUnsafeStatement(statement, ctx, loopDepth)
		return checked, binding{}, false, diagnostics
	case parser.BreakStatement:
		if loopDepth == 0 {
			return BreakStatement{}, binding{}, false, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "break is only valid inside a loop")}
		}
		return BreakStatement{Span: statement.Keyword.Span}, binding{}, false, nil
	case parser.ContinueStatement:
		if loopDepth == 0 {
			return ContinueStatement{}, binding{}, false, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "continue is only valid inside a loop")}
		}
		return ContinueStatement{Span: statement.Keyword.Span}, binding{}, false, nil
	case parser.DeferStatement:
		checked, diagnostics := checkDeferStatement(statement, ctx)
		return checked, binding{}, false, diagnostics
	case parser.ErrdeferStatement:
		checked, diagnostics := checkErrdeferStatement(statement, ctx)
		return checked, binding{}, false, diagnostics
	case parser.TryStatement:
		// A try statement reuses the try-expression validation and
		// propagation metadata; the success value is discarded.
		checkedTry := checkTryExpression(parser.TryExpression{Keyword: statement.Keyword, Operand: statement.Operand}, expressionContext{}, ctx)
		if diagnostics := initializerDiagnostics(checkedTry); len(diagnostics) > 0 {
			return nil, binding{}, false, diagnostics
		}
		return TryStatement{
			Expression: checkedTry.source,
			Span:       statement.Keyword.Span,
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

func checkCondition(expression parser.Expression, ctx checkContext) (Operand, *Operand, lexer.Token, compilerTypes.Diagnostics) {
	checked := checkValue(expression, ctx)
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

func checkIfStatement(statement parser.IfStatement, ctx checkContext, loopDepth int) (IfStatement, compilerTypes.Diagnostics) {
	checked := IfStatement{
		Span:     statement.Keyword.Span,
		EndSpan:  statement.End.Span,
		ElseSpan: statement.ElseKeyword.Span,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	condition, _, conditionToken, conditionDiagnostics := checkCondition(statement.Condition, ctx)
	diagnostics = append(diagnostics, conditionDiagnostics...)
	checked.Condition = condition
	checked.ConditionSpan = conditionToken.Span

	// Each branch checks a clone of the pre-test flow carrying its own
	// narrowing fact. Invalidations from a clean branch merge into the
	// pre-test flow only after every branch is checked, so the else side of
	// the construct never observes the then side's effects. Cloning is
	// unconditional so owning-state transitions inside one branch can never
	// leak into a sibling or the continuing path; the strict owner merge
	// then detects disagreements exactly.
	var fact *narrowingFact
	if len(conditionDiagnostics) == 0 {
		fact = conditionNarrowing(condition, ctx.names.flow, ctx.typeEnvironment)
	}
	parentState := ctx.names.flow
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

	thenScope := ctx.names.child()
	thenScope.flow = thenState
	thenBody, thenDiagnostics := checkStatements(statement.Then, checkContext{names: thenScope, typeEnvironment: ctx.typeEnvironment}, loopDepth)
	diagnostics = append(diagnostics, thenDiagnostics...)
	checked.Then = thenBody
	checked.ThenDefers = append(checked.ThenDefers, thenScope.defers...)
	if len(thenDiagnostics) == 0 {
		ctx.names.recordChildReturnFlows(thenScope.returnFlows)
	}

	continuing := make([]*flowState, 0, len(statement.ElseIf)+2)
	if len(thenDiagnostics) == 0 && !sequenceTerminates(thenBody) && thenState != nil {
		continuing = append(continuing, thenState)
	}

	for _, branch := range statement.ElseIf {
		// An elseif condition is checked where every previous condition was
		// false, so its state is the else-side chain; each body narrows a
		// clone of that chain and only its invalidations merge onward.
		conditionScope := ctx.names.child()
		conditionScope.flow = elseState
		branchCondition, _, branchToken, branchConditionDiagnostics := checkCondition(branch.Condition, checkContext{names: conditionScope, typeEnvironment: ctx.typeEnvironment})
		diagnostics = append(diagnostics, branchConditionDiagnostics...)
		// Always clone the else-side chain for the branch body: its own
		// invalidations must not leak into the next elseif condition, and they
		// must still merge into the pre-test flow even when this condition
		// narrows nothing (otherwise a missing final else would drop them).
		branchState := elseState
		if elseState != nil {
			branchState = elseState.clone()
			if len(branchConditionDiagnostics) == 0 {
				if branchFact := conditionNarrowing(branchCondition, elseState, ctx.typeEnvironment); branchFact != nil {
					branchState.narrow(branchFact.binding, branchFact.typ)
					nextElseState := elseState.clone()
					nextElseState.narrow(branchFact.binding, branchFact.other)
					elseState = nextElseState
				}
			}
		}
		branchScope := ctx.names.child()
		branchScope.flow = branchState
		branchBody, branchDiagnostics := checkStatements(branch.Body, checkContext{names: branchScope, typeEnvironment: ctx.typeEnvironment}, loopDepth)
		diagnostics = append(diagnostics, branchDiagnostics...)
		checked.ElseIfDefers = append(checked.ElseIfDefers, append([]DeferredAction(nil), branchScope.defers...))
		if len(branchDiagnostics) == 0 {
			ctx.names.recordChildReturnFlows(branchScope.returnFlows)
		}
		checked.ElseIf = append(checked.ElseIf, IfBranch{
			Condition:     branchCondition,
			ConditionSpan: branchToken.Span,
			Body:          branchBody,
			Span:          branch.Keyword.Span,
		})
		if len(branchDiagnostics) == 0 && !sequenceTerminates(branchBody) && branchState != nil {
			continuing = append(continuing, branchState)
		}
	}
	if statement.Else != nil {
		elseScope := ctx.names.child()
		elseScope.flow = elseState
		elseBody, elseDiagnostics := checkStatements(statement.Else, checkContext{names: elseScope, typeEnvironment: ctx.typeEnvironment}, loopDepth)
		diagnostics = append(diagnostics, elseDiagnostics...)
		checked.Else = elseBody
		checked.ElseDefers = append(checked.ElseDefers, elseScope.defers...)
		if len(elseDiagnostics) == 0 {
			ctx.names.recordChildReturnFlows(elseScope.returnFlows)
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
	case ReturnStatement, RootReturnStatement, BreakStatement, ContinueStatement:
		return true
	case UnsafeStatement:
		return sequenceTerminates(statement.Body)
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
func checkForStatement(statement parser.ForStatement, ctx checkContext, loopDepth int) (ForStatement, compilerTypes.Diagnostics) {
	checked := ForStatement{
		Span: statement.Keyword.Span,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)

	seen := make(map[string]bool, len(statement.Binders))
	for _, binder := range statement.Binders {
		if seen[binder.Name.Lexeme] {
			diagnostics = append(diagnostics, nameErrorAt(binder.Name, "duplicate loop binder name "+binder.Name.Lexeme))
		}
		seen[binder.Name.Lexeme] = true
	}

	// The source is read as a value but keeps its place addressability when
	// it names storage: the generator iterates an Array place in place and
	// only materializes genuine temporaries.
	var source checkedExpression
	switch statement.Source.(type) {
	case parser.VariableExpression, parser.PropertyExpression, parser.IndexExpression:
		source = checkPlace(statement.Source, ctx)
	default:
		source = checkExpression(statement.Source, expressionContext{foldConstants: false}, ctx)
	}
	if source.diagnostic != nil {
		return checked, append(diagnostics, *source.diagnostic)
	}
	if diagnosticsFromSource := initializerDiagnostics(source); len(diagnosticsFromSource) > 0 {
		return checked, append(diagnostics, diagnosticsFromSource...)
	}

	binderNames := make([]lexer.Token, len(statement.Binders))
	for index, binder := range statement.Binders {
		binderNames[index] = binder.Name
	}
	binderTypes, arityDiagnostic := forBinderTypes(source.typ, binderNames)
	if arityDiagnostic != nil {
		return checked, append(diagnostics, *arityDiagnostic)
	}
	if len(binderTypes) != len(statement.Binders) {
		return checked, append(diagnostics, typeErrorAt(statement.Keyword, "for-in binder count does not match the source type"))
	}
	if compilerTypes.IsText(source.typ) {
		// Text iteration's element type belongs to the binder annotation:
		// Byte is the storage unit, Rune is a decoded scalar.
		if element, ok := textBinderElement(statement.Binders, ctx); ok {
			binderTypes[len(binderTypes)-1] = element
		}
	}
	if annotationDiagnostics := checkForBinderAnnotations(statement.Binders, binderTypes, source.typ, ctx); len(annotationDiagnostics) > 0 {
		return checked, append(diagnostics, annotationDiagnostics...)
	}

	parentState := ctx.names.flow
	bodyState := parentState
	if parentState != nil {
		bodyState = parentState.clone()
	}
	bodyScope := ctx.names.child()
	bodyScope.flow = bodyState
	for index, binder := range statement.Binders {
		binderType := binderTypes[index]
		bound := binding{typ: binderType, use: compilerTypes.NewTypeUse(binderType), loopBinder: true, id: ctx.names.newBindingID()}
		bodyScope.local[binder.Name.Lexeme] = bound
		checked.Binders = append(checked.Binders, ForBinder{
			Name:    binder.Name.Lexeme,
			Type:    binderType,
			Binding: bound.id,
			Span:    binder.Name.Span,
		})
	}

	body, bodyDiagnostics := checkStatements(statement.Body, checkContext{names: bodyScope, typeEnvironment: ctx.typeEnvironment}, loopDepth+1)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = body
	checked.BodyDefers = append(checked.BodyDefers, bodyScope.defers...)
	if len(bodyDiagnostics) == 0 {
		ctx.names.recordChildReturnFlows(bodyScope.returnFlows)
	}
	checked.Source = source.source
	// Iterator invalidation: direct structural mutations and frees through any
	// copied handle are rejected when the active traversal can identify them;
	// other copied-handle mutations remain defined by the generated version
	// check. Every checked expression in the body is scanned, not only calls in
	// statement position.
	if len(bodyDiagnostics) == 0 && (source.typ.List != nil || source.typ.Dict != nil) {
		if binding := baseBindingID(&source.source.Node); binding != 0 {
			root := collectionRootForOperand(source.source, ctx.names, binding)
			if mutationDiagnostics := checkForIterationMutations(binding, root, source.typ, body, ctx.names.table); len(mutationDiagnostics) > 0 {
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
	case source.Array != nil || source.Slice != nil || source.List != nil:
		var element compilerTypes.Type
		if source.Array != nil {
			element = source.Array.Element
		} else if source.Slice != nil {
			element = source.Slice.Element
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
	case compilerTypes.IsText(source):
		switch len(binders) {
		case 1:
			return []compilerTypes.Type{compilerTypes.UInt8}, nil
		case 2:
			return []compilerTypes.Type{compilerTypes.SizeType, compilerTypes.UInt8}, nil
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
func checkForIterationMutations(sourceBinding, sourceRoot BindingID, collectionType compilerTypes.Type, body []Statement, table *span.Table) compilerTypes.Diagnostics {
	scanner := iterationMutationScanner{
		sourceBinding:  sourceBinding,
		sourceRoot:     sourceRoot,
		collectionType: collectionType,
		table:          table,
	}
	scanner.walkStatements(body)
	return scanner.diagnostics
}

type iterationMutationScanner struct {
	sourceBinding  BindingID
	sourceRoot     BindingID
	collectionType compilerTypes.Type
	diagnostics    compilerTypes.Diagnostics
	table          *span.Table
}

func (scanner *iterationMutationScanner) report(s span.Span, message string) {
	scanner.diagnostics = append(scanner.diagnostics, typeErrorAt(tokenAt(scanner.table, s), message))
}

func (scanner *iterationMutationScanner) walkStatements(statements []Statement) {
	for _, statement := range statements {
		switch node := statement.(type) {
		case Declaration:
			scanner.walkOperand(node.Source, node.Span)
		case Assignment:
			scanner.walkOperand(node.Target, node.Span)
			scanner.walkOperand(node.Source, node.Span)
		case CallStatement:
			scanner.walkOperand(node.Call, node.Span)
		case ReturnStatement:
			if node.Value != nil {
				scanner.walkOperand(*node.Value, node.Span)
			}
		case TryStatement:
			scanner.walkOperand(node.Expression, node.Span)
		case DeferStatement:
			scanner.walkOperand(node.Expression, node.Span)
		case ErrdeferStatement:
			scanner.walkOperand(node.Expression, node.Span)
		case IfStatement:
			scanner.walkOperand(node.Condition, node.ConditionSpan)
			scanner.walkStatements(node.Then)
			for _, branch := range node.ElseIf {
				scanner.walkOperand(branch.Condition, branch.Span)
				scanner.walkStatements(branch.Body)
			}
			scanner.walkStatements(node.Else)
		case WhileStatement:
			scanner.walkOperand(node.Condition, node.ConditionSpan)
			scanner.walkStatements(node.Body)
		case ForStatement:
			scanner.walkOperand(node.Source, node.Span)
			scanner.walkStatements(node.Body)
		case UnsafeStatement:
			scanner.walkStatements(node.Body)
		case FunctionDeclaration:
			scanner.walkStatements(node.Body)
		case MethodDeclaration:
			scanner.walkStatements(node.Body)
		}
	}
}

func (scanner *iterationMutationScanner) walkOperand(operand Operand, s span.Span) {
	scanner.walkExpression(&operand.Node, s)
	if operand.Object != nil {
		for _, member := range operand.Object.Initializers {
			scanner.walkOperand(member.Source, s)
		}
	}
}

func (scanner *iterationMutationScanner) walkExpression(node *Expression, s span.Span) {
	if node == nil {
		return
	}
	switch node.Kind {
	case CollectionMethodCallExpression:
		receiverRoot := collectionRootOfNode(node.Operand)
		if receiverRoot == scanner.sourceRoot && node.OperandType == scanner.collectionType {
			switch node.Name {
			case "free":
				scanner.report(s, "cannot free collection during iteration")
			case "push", "pop", "clear", "insert", "remove":
				if baseBindingID(node.Operand) == scanner.sourceBinding {
					scanner.report(s, "cannot mutate collection during iteration")
				}
			}
		}
	case CallExpression, MethodCallExpression:
		if scanner.callReceivesSource(node) {
			scanner.report(s, "cannot pass traversed collection to call during iteration")
		}
	}

	scanner.walkExpression(node.Operand, s)
	scanner.walkExpression(node.Left, s)
	scanner.walkExpression(node.Right, s)
	for _, argument := range node.Arguments {
		scanner.walkOperand(argument, s)
	}
	if node.Constant != nil {
		scanner.walkOperand(*node.Constant, s)
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

func checkWhileStatement(statement parser.WhileStatement, ctx checkContext, loopDepth int) (WhileStatement, compilerTypes.Diagnostics) {
	checked := WhileStatement{
		Span:    statement.Keyword.Span,
		EndSpan: statement.End.Span,
	}
	diagnostics := make(compilerTypes.Diagnostics, 0)
	condition, conditionKnown, conditionToken, conditionDiagnostics := checkCondition(statement.Condition, ctx)
	diagnostics = append(diagnostics, conditionDiagnostics...)
	checked.Condition = condition
	checked.ConditionKnown = conditionKnown
	checked.ConditionSpan = conditionToken.Span

	// The condition's narrowing holds for the body. The parent state is also
	// a zero-iteration path, so a body free cannot become definite after the
	// loop merely because one iteration can execute it.
	parentState := ctx.names.flow
	bodyState := parentState
	if parentState != nil {
		bodyState = parentState.clone()
		if len(conditionDiagnostics) == 0 {
			if fact := conditionNarrowing(condition, parentState, ctx.typeEnvironment); fact != nil {
				bodyState.narrow(fact.binding, fact.typ)
			}
		}
	}
	bodyScope := ctx.names.child()
	bodyScope.flow = bodyState
	body, bodyDiagnostics := checkStatements(statement.Body, checkContext{names: bodyScope, typeEnvironment: ctx.typeEnvironment}, loopDepth+1)
	diagnostics = append(diagnostics, bodyDiagnostics...)
	checked.Body = body
	checked.BodyDefers = append(checked.BodyDefers, bodyScope.defers...)
	if len(bodyDiagnostics) == 0 {
		ctx.names.recordChildReturnFlows(bodyScope.returnFlows)
	}
	if len(bodyDiagnostics) == 0 && parentState != nil && bodyState != nil {
		parentState.mergeBranches(parentState.clone(), bodyState)
	}
	return checked, diagnostics
}

// checkReturnStatement checks a `return` wherever the parser accepts one: a
// function body (its existing, unchanged behavior) or, now that the parser
// no longer rejects it before the entrypoint is known, the entry module's
// own root scope (nested inside root if/while/for or written directly at
// root), which it delegates to checkRootReturnStatement. Anywhere else -- an
// imported module's root, whether direct or nested -- it fails closed with
// the exact diagnostic below.
func checkReturnStatement(statement parser.ReturnStatement, ctx checkContext) (Statement, compilerTypes.Diagnostics) {
	if !ctx.names.inFunction() {
		if !ctx.names.isEntryModule() {
			return ReturnStatement{}, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, "return is valid only in the entry module or a function body")}
		}
		return checkRootReturnStatement(statement, ctx)
	}
	checked := ReturnStatement{Span: statement.Keyword.Span}
	if statement.Value == nil {
		if ctx.names.result != nil {
			return checked, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword,
				fmt.Sprintf("return requires a value; %s declares %s", ctx.names.owner, ctx.names.result.Name))}
		}
		return checked, nil
	}
	if ctx.names.result == nil {
		return checked, compilerTypes.Diagnostics{typeErrorAt(statement.Keyword, ctx.names.owner+" returns no value; use a bare return")}
	}

	resultUse := compilerTypes.NewTypeUse(*ctx.names.result)
	if ctx.names.resultUse != nil {
		resultUse = *ctx.names.resultUse
	}
	value := checkInitializer(statement.Value, resultUse, statement.Keyword, ctx)
	if valueDiagnostics := initializerDiagnostics(value); len(valueDiagnostics) > 0 {
		return checked, valueDiagnostics
	}
	if value.typ != (compilerTypes.Type{}) && !assignable(*ctx.names.result, value.typ) {
		return checked, compilerTypes.Diagnostics{typeErrorAt(value.token,
			fmt.Sprintf("%s returns %s; got %s", ctx.names.owner, ctx.names.result.Name, value.typ.Name)+textMismatchHint(*ctx.names.result, value.typ))}
	}
	if diagnostic := restEscapeDiagnostic(value.source, value.token); diagnostic != nil {
		return checked, compilerTypes.Diagnostics{*diagnostic}
	}
	if value.typ != (compilerTypes.Type{}) {
		if diagnostic := atomicCopyDiagnostic(value.source, statement.Keyword); diagnostic != nil {
			return checked, compilerTypes.Diagnostics{*diagnostic}
		}
		// Only raw pointers carry a return-site lifetime check: a Slice is
		// a copyable descriptor whose backing storage is the programmer's
		// responsibility.
		if value.typ.Element != nil && value.typ.Signature == nil {
			if diagnostic := ptrReturnDiagnostic(value.source.Node, statement.Keyword, ctx.names, value.typ.PointeeWritable); diagnostic != nil {
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

// checkRootReturnStatement checks a `return` at the entry module's own root:
// a bare return and fallthrough both record status zero (represented here as
// a nil Value, mirroring a no-result function return); a valued return
// requires exact UInt8 with no implicit conversion.
func checkRootReturnStatement(statement parser.ReturnStatement, ctx checkContext) (RootReturnStatement, compilerTypes.Diagnostics) {
	checked := RootReturnStatement{Span: statement.Keyword.Span}
	if statement.Value == nil {
		return checked, nil
	}
	value := checkInitializer(statement.Value, compilerTypes.NewTypeUse(compilerTypes.UInt8), statement.Keyword, ctx)
	if valueDiagnostics := initializerDiagnostics(value); len(valueDiagnostics) > 0 {
		return checked, valueDiagnostics
	}
	if !compilerTypes.Equal(value.typ, compilerTypes.UInt8) {
		return checked, compilerTypes.Diagnostics{typeErrorAt(value.token, fmt.Sprintf("entry-module return requires UInt8; got %s", value.typ.Name))}
	}
	source := value.source
	checked.Value = &source
	return checked, nil
}

// textBinderElement resolves the element type a text for-binder annotation
// names: Byte is the storage unit and Rune is a decoded scalar. An absent or
// unrecognized annotation leaves the Byte default, and the annotation check
// then reports the mismatch.
func textBinderElement(binders []parser.ForBinder, ctx checkContext) (compilerTypes.Type, bool) {
	value := binders[len(binders)-1]
	if value.Type == nil {
		return compilerTypes.Type{}, false
	}
	use, diagnostic := resolveTypeUse(value.Type, value.Name, ctx.typeEnvironment, ctx.names.generics)
	if diagnostic != nil {
		return compilerTypes.Type{}, false
	}
	switch {
	case compilerTypes.IsRune(use.Type):
		return compilerTypes.Rune, true
	case compilerTypes.IsGrapheme(use.Type):
		return compilerTypes.GraphemeType, true
	case compilerTypes.Equal(use.Type, compilerTypes.UInt8):
		return compilerTypes.UInt8, true
	}
	return compilerTypes.Type{}, false
}

// checkForBinderAnnotations checks the written types of a for-in header
// against what the source yields to each binder. An annotation is permitted on
// every binder of every source and must be exactly the type that binder
// receives: no conversion and no weakening. It is required on exactly the
// binder whose type the source does not determine. Text is the only such
// source: it is a sequence of bytes today and will also be a sequence of code
// points, so the element type belongs to the iteration, not to the collection.
// The index binder is always Size and is never required to be annotated.
func checkForBinderAnnotations(binders []parser.ForBinder, binderTypes []compilerTypes.Type, source compilerTypes.Type, ctx checkContext) compilerTypes.Diagnostics {
	diagnostics := make(compilerTypes.Diagnostics, 0)
	textSource := compilerTypes.IsText(source)
	for index, binder := range binders {
		valueBinder := index == len(binders)-1
		if binder.Type == nil {
			if textSource && valueBinder {
				diagnostics = append(diagnostics, typeErrorAt(binder.Name, fmt.Sprintf(
					"for binder %s over %s has an ambiguous element type; annotate it, for example for %s: Byte in ...",
					binder.Name.Lexeme, source.Name, binder.Name.Lexeme)))
			}
			continue
		}
		use, diagnostic := resolveTypeUse(binder.Type, binder.Name, ctx.typeEnvironment, ctx.names.generics)
		if diagnostic != nil {
			diagnostics = append(diagnostics, *diagnostic)
			continue
		}
		if !compilerTypes.Equal(use.Type, binderTypes[index]) {
			diagnostics = append(diagnostics, typeErrorAt(binder.Name, fmt.Sprintf(
				"for binder %s is annotated %s, but %s yields %s there",
				binder.Name.Lexeme, use.Type.Name, source.Name, binderTypes[index].Name)))
		}
	}
	return diagnostics
}
