package checker

import (
	"strconv"

	"hexal/compiler/diagnostics"
	"hexal/compiler/span"
)

// Statement walk of the stale-view analysis: structured control flow with
// unions at joins, loop fixpoints, and deferred cleanup applied where a scope
// exits.

func (a *viewAnalyzer) stmts(list []Statement, st *viewState) *viewState {
	for _, statement := range list {
		if st == nil {
			return nil
		}
		st = a.stmt(statement, st)
	}
	return st
}

// body walks one nested statement list in its own cleanup scope.
func (a *viewAnalyzer) body(list []Statement, st *viewState) *viewState {
	a.cur.scopes = append(a.cur.scopes, nil)
	end := a.stmts(list, st)
	return a.leaveScope(end)
}

// leaveScope applies the innermost scope's deferred cleanup to the state that
// reaches its end and pops it.
func (a *viewAnalyzer) leaveScope(st *viewState) *viewState {
	top := len(a.cur.scopes) - 1
	actions := a.cur.scopes[top]
	a.cur.scopes = a.cur.scopes[:top]
	if st == nil {
		return nil
	}
	a.runDeferred(actions, st)
	return st
}

// exitScopesDown applies the deferred cleanup of every scope a jump leaves,
// innermost first, without popping them: the statements after the jump belong
// to paths that do not leave.
func (a *viewAnalyzer) exitScopesDown(depth int, st *viewState) {
	for index := len(a.cur.scopes) - 1; index >= depth; index-- {
		a.runDeferred(a.cur.scopes[index], st)
	}
}

func (a *viewAnalyzer) runDeferred(actions []deferredCall, st *viewState) {
	for index := len(actions) - 1; index >= 0; index-- {
		action := actions[index]
		switch action.node.Kind {
		case CallExpression, MethodCallExpression:
			a.applyCall(action.node, action.args, action.callee, st, action.span)
		case CollectionMethodCallExpression:
			if action.node.OperandType.List != nil && len(action.args) > 0 && structuralListOperation(action.node.Name) {
				a.invalidate(action.args[0].roots, action.span, st)
			}
		case StringMethodCallExpression:
			if action.node.Name == "free" && len(action.args) > 0 {
				a.invalidate(action.args[0].roots, action.span, st)
			}
		default:
			a.cur.stmtSpan = action.span
			a.evalNode(action.node, st)
		}
	}
}

// registerDefer evaluates a deferred call's operands now and fixes its
// receiver roots, as the generated code captures them at registration; the
// call's effect is applied when the scope exits.
func (a *viewAnalyzer) registerDefer(action DeferredAction, st *viewState) {
	if action.Err {
		return
	}
	operand := action.Value
	if action.IsCall {
		operand = action.Call
	}
	if operand == nil {
		return
	}
	node := &operand.Node
	at := action.Span
	if at == (span.Span{}) {
		at = a.cur.stmtSpan
	}
	deferred := deferredCall{node: node, span: at}
	if action.IsCall {
		switch node.Kind {
		case CallExpression, MethodCallExpression:
			deferred.args, deferred.callee = a.buildCallArgs(node, st)
		case CollectionMethodCallExpression, StringMethodCallExpression:
			receiver := a.evalNode(node.Operand, st)
			args := a.evalArguments(node.Arguments, st)
			head := viewArg{value: receiver, roots: a.handleRootsOf(node.Operand, receiver, st)}
			deferred.args = append([]viewArg{head}, args...)
		}
	}
	top := len(a.cur.scopes) - 1
	a.cur.scopes[top] = append(a.cur.scopes[top], deferred)
}

func (a *viewAnalyzer) stmt(statement Statement, st *viewState) *viewState {
	switch s := statement.(type) {
	case Declaration:
		a.cur.stmtSpan = s.Span
		a.names[s.Binding] = s.Name
		a.bindingTypes[s.Binding] = s.Type
		value := a.evalOperand(s.Source, st)
		if s.Pipeline != nil {
			a.evalPipelineTerminal(s.Pipeline, st)
		}
		a.bindPlace(st, viewPlace{binding: s.Binding}, value, false, "")
	case Assignment:
		a.cur.stmtSpan = s.Span
		value := a.evalOperand(s.Source, st)
		if s.Pipeline != nil {
			a.evalPipelineTerminal(s.Pipeline, st)
		}
		a.assign(s, value, st)
	case CallStatement:
		a.cur.stmtSpan = s.Span
		a.evalOperand(s.Call, st)
	case TryStatement:
		a.cur.stmtSpan = s.Span
		a.evalOperand(s.Expression, st)
	case DeferStatement:
		a.cur.stmtSpan = s.Span
		a.registerDefer(s.Action, st)
	case ErrdeferStatement:
		// An errdefer runs only on an Error exit; its effect is not applied
		// on the success paths the analysis joins.
		a.cur.stmtSpan = s.Span
	case ReturnStatement:
		a.cur.stmtSpan = s.Span
		if s.Value != nil {
			value := a.evalOperand(*s.Value, st)
			a.returnValue(value)
		}
		if s.Pipeline != nil {
			a.evalPipelineTerminal(s.Pipeline, st)
		}
		a.exitScopesDown(0, st)
		return nil
	case RootReturnStatement:
		a.cur.stmtSpan = s.Span
		if s.Value != nil {
			a.evalOperand(*s.Value, st)
		}
		a.exitScopesDown(0, st)
		return nil
	case IfStatement:
		return a.ifStmt(s, st)
	case WhileStatement:
		return a.whileStmt(s, st)
	case ForStatement:
		return a.forStmt(s, st)
	case BreakStatement:
		if frame := a.topLoop(); frame != nil {
			a.exitScopesDown(frame.scopeDepth, st)
			frame.breaks = append(frame.breaks, st)
		}
		return nil
	case ContinueStatement:
		if frame := a.topLoop(); frame != nil {
			a.exitScopesDown(frame.scopeDepth, st)
			frame.continues = append(frame.continues, st)
		}
		return nil
	case UnsafeStatement:
		a.cur.unsafeDepth++
		end := a.body(s.Body, st)
		a.cur.unsafeDepth--
		return end
	}
	return st
}

func (a *viewAnalyzer) topLoop() *loopFrame {
	if len(a.cur.loops) == 0 {
		return nil
	}
	return a.cur.loops[len(a.cur.loops)-1]
}

func (a *viewAnalyzer) evalPipelineTerminal(terminal *PipelineTerminal, st *viewState) {
	if terminal == nil {
		return
	}
	if terminal.Pipeline != nil {
		a.evalOperand(terminal.Pipeline.Source, st)
	}
	for _, operand := range []*Operand{terminal.Heap, terminal.Initial, terminal.Combiner} {
		if operand != nil {
			a.evalOperand(*operand, st)
		}
	}
}

// bindPlace makes a tracked place hold value. A handle root the value names
// as fresh becomes the place's own root, distinguished by site so storage
// replaced at an assignment never shares identity with its predecessor.
func (a *viewAnalyzer) bindPlace(st *viewState, place viewPlace, value viewValue, weak bool, site string) {
	if len(value.roots) > 0 {
		replaced := make(map[string]rootSet, len(value.roots))
		for path, roots := range value.roots {
			if roots.contains(freshRoot) {
				own := viewRoot{binding: place.binding, path: place.path + path + site}
				cleaned := make(rootSet, 0, len(roots))
				for _, root := range roots {
					if root != freshRoot {
						cleaned = append(cleaned, root)
					}
				}
				roots = rootSet(cleaned).union(singleRoot(own))
			}
			replaced[path] = roots
		}
		value.roots = replaced
	}
	st.bindValue(place, value, weak)
}

// assign applies an assignment's effect on the tracked places and rejects a
// view written into storage the analysis does not follow.
func (a *viewAnalyzer) assign(s Assignment, value viewValue, st *viewState) {
	target := &s.Target.Node
	place, tracked := placeOfNode(target)
	if tracked && a.cur.captured[place.binding] {
		// A captured entry-root binding outlives the callable, so a view
		// stored there leaves the storage the analysis follows.
		tracked = false
	}
	if !tracked {
		a.evalTargetReads(target, st)
		a.escape(value, target, st)
		return
	}
	weak := false
	for node := target; node != nil; node = node.Operand {
		if node.Kind == IndexExpression {
			weak = true
			a.evalArgumentsOnly(node, st)
		}
	}
	a.bindPlace(st, place, value, weak, "@"+strconv.Itoa(s.Span.Start))
}

// evalTargetReads evaluates the operands an assignment target reads on the way
// to the place it writes.
func (a *viewAnalyzer) evalTargetReads(node *Expression, st *viewState) {
	switch node.Kind {
	case VariableExpression, ModuleValueExpression:
		return
	case MemberExpression, UnionPayloadExpression, AdtPayloadExpression:
		a.evalTargetReads(node.Operand, st)
	case IndexExpression:
		a.evalNode(node.Operand, st)
		a.evalArgumentsOnly(node, st)
	default:
		a.evalNode(node, st)
	}
}

// returnValue records what a returned value derives from and rejects a view
// whose root the callable's summary cannot preserve.
func (a *viewAnalyzer) returnValue(value viewValue) {
	record := func(root viewRoot) bool {
		if index, ok := a.cur.params[root.binding]; ok {
			path := root.path
			if path != viewRootIncoming {
				path = stripGeneration(path)
			}
			a.cur.summary.addBorrow(summaryRoot{param: index, path: path})
			return true
		}
		if a.cur.captured[root.binding] {
			a.cur.summary.addBorrow(summaryRoot{param: -1, binding: root.binding, path: stripGeneration(root.path)})
			return true
		}
		return false
	}
	if fact, ok := value.whole(); ok {
		for _, root := range fact.roots {
			if record(root) || a.cur.unsafeDepth > 0 {
				continue
			}
			a.report(a.cur.stmtSpan, diagnostics.CollectionViewEscapesRoot(a.rootName(root)))
		}
	}
	for _, root := range value.handleRoots() {
		record(root)
	}
}

func (a *viewAnalyzer) ifStmt(s IfStatement, st *viewState) *viewState {
	a.cur.stmtSpan = s.ConditionSpan
	a.evalOperand(s.Condition, st)
	var results []*viewState
	results = append(results, a.body(s.Then, st.clone()))
	for _, branch := range s.ElseIf {
		a.cur.stmtSpan = branch.Span
		a.evalOperand(branch.Condition, st)
		results = append(results, a.body(branch.Body, st.clone()))
	}
	if len(s.Else) > 0 {
		results = append(results, a.body(s.Else, st.clone()))
	} else {
		results = append(results, st)
	}
	return joinViewStates(results...)
}

// maxLoopIterations bounds the loop fixpoint as a backstop only: facts, stale
// flags, and roots grow monotonically over a finite set of places, so every
// loop reaches a fixpoint long before this count. It is a safety bound, not a
// measured value.
const maxLoopIterations = 32

func (a *viewAnalyzer) whileStmt(s WhileStatement, st *viewState) *viewState {
	entry := st
	head := st.clone()
	forever := isLiteralTrue(s.Condition, s.ConditionKnown)
	var exit *viewState
	for iteration := 0; iteration < maxLoopIterations; iteration++ {
		frame := &loopFrame{scopeDepth: len(a.cur.scopes)}
		a.cur.loops = append(a.cur.loops, frame)
		afterCondition := head.clone()
		a.cur.stmtSpan = s.ConditionSpan
		a.evalOperand(s.Condition, afterCondition)
		bodyEnd := a.body(s.Body, afterCondition.clone())
		a.cur.loops = a.cur.loops[:len(a.cur.loops)-1]
		back := joinViewStates(append([]*viewState{bodyEnd}, frame.continues...)...)
		next := joinViewStates(entry, back)
		exits := frame.breaks
		if !forever {
			exits = append([]*viewState{afterCondition}, exits...)
		}
		exit = joinViewStates(exits...)
		if sameViewState(next, head) {
			break
		}
		head = next
	}
	return exit
}

func (a *viewAnalyzer) forStmt(s ForStatement, st *viewState) *viewState {
	a.cur.stmtSpan = s.Span
	a.evalOperand(s.Source, st)
	if s.Pipeline != nil {
		a.evalOperand(s.Pipeline.Source, st)
	}
	for _, binder := range s.Binders {
		a.names[binder.Binding] = binder.Name
		a.bindingTypes[binder.Binding] = binder.Type
	}
	entry := st
	head := st.clone()
	var exit *viewState
	for iteration := 0; iteration < maxLoopIterations; iteration++ {
		frame := &loopFrame{scopeDepth: len(a.cur.scopes)}
		a.cur.loops = append(a.cur.loops, frame)
		bodyEnd := a.body(s.Body, head.clone())
		a.cur.loops = a.cur.loops[:len(a.cur.loops)-1]
		back := joinViewStates(append([]*viewState{bodyEnd}, frame.continues...)...)
		next := joinViewStates(entry, back)
		exit = joinViewStates(append([]*viewState{head}, frame.breaks...)...)
		if sameViewState(next, head) {
			break
		}
		head = next
	}
	return exit
}
