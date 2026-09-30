package checker

import (
	"hexal/compiler/diagnostics"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// Call boundaries of the stale-view analysis: a call changes the storage its
// callee's summary says it may change, rejects a view argument the summary
// lets escape, and hands the result the views the summary says it borrows.

// summaryFor resolves the callee of a call node. A nil summary with known
// false means the callee is unresolved and fails closed.
func (a *viewAnalyzer) summaryFor(node *Expression) (summary *viewSummary, known bool) {
	if node.Kind == MethodCallExpression {
		if node.Owner != nil {
			if local, ok := a.summaries[callableKey{name: node.Name, owner: node.Owner}]; ok {
				return local, true
			}
			if a.registry != nil {
				if imported, ok := a.registry.viewSummaryOfMethod(node.Owner, node.Name); ok {
					return imported, true
				}
			}
		}
		return nil, false
	}
	callee := node.Operand
	if callee == nil {
		return nil, false
	}
	switch callee.Kind {
	case FunctionReferenceExpression:
		if callee.LocalHelperOrdinal != 0 {
			return nil, false
		}
		if callee.Module == "" {
			if local, ok := a.summaries[callableKey{name: callee.Name}]; ok {
				return local, true
			}
			return nil, false
		}
		if a.registry != nil {
			if imported, ok := a.registry.viewSummaryOfFunction(callee.Module, callee.Name); ok {
				return imported, true
			}
		}
		return nil, false
	case ForeignFunctionReferenceExpression:
		return &viewSummary{}, true
	}
	return nil, false
}

// buildCallArgs evaluates a call's receiver and operands in order.
func (a *viewAnalyzer) buildCallArgs(node *Expression, st *viewState) ([]viewArg, funcTargets) {
	operands := node.Arguments
	var receiver *viewArg
	var callee funcTargets
	if node.Kind == MethodCallExpression && node.Operand != nil {
		value := a.evalNode(node.Operand, st)
		inner := stripAdaptation(node.Operand)
		head := viewArg{value: value, roots: a.handleRootsOf(inner, value, st), span: a.currentSpan(node)}.withPlace(inner)
		receiver = &head
	} else if node.Operand != nil && node.Operand.Kind != FunctionReferenceExpression && node.Operand.Kind != ForeignFunctionReferenceExpression {
		// A Fun value callee is read like any other operand; the callables it
		// may name are the call's targets.
		callee = a.evalNode(node.Operand, st).targets
	}
	args := a.evalArguments(operands, st)
	for index := range args {
		args[index].roots = a.handleRootsOf(&operands[index].Node, args[index].value, st)
		if !canCarryHandles(operands[index].Type) {
			args[index].roots = nil
		}
		args[index] = args[index].withPlace(&operands[index].Node)
	}
	if receiver != nil {
		args = append([]viewArg{*receiver}, args...)
	}
	return args, callee
}

func (a *viewAnalyzer) evalCall(node *Expression, st *viewState) viewValue {
	args, callee := a.buildCallArgs(node, st)
	return a.applyCall(node, args, callee, st, a.currentSpan(node))
}

// applyCall applies a call's summary: rule checks first, then the structural
// changes, then the result's borrowed views.
func (a *viewAnalyzer) applyCall(node *Expression, args []viewArg, callee funcTargets, st *viewState, at span.Span) viewValue {
	summary, known := a.summaryFor(node)
	if !known {
		summary = mergeTargets(callee)
	}
	a.checkTemporaryReceiver(node, summary, at)
	for index := range args {
		fact, carries := args[index].value.whole()
		if !carries {
			continue
		}
		if summary.escapesParam(index) && a.cur.unsafeDepth == 0 {
			for _, root := range fact.roots {
				if isIncomingOf(a.cur, root) {
					continue
				}
				a.report(at, diagnostics.CollectionViewEscapesRoot(a.rootName(root)))
				break
			}
		}
		if summary.escapesParam(index) {
			for _, root := range fact.roots {
				if param, ok := a.cur.params[root.binding]; ok && root.path == viewRootIncoming {
					a.cur.summary.addEscape(summaryRoot{param: param})
				}
			}
		}
		for other := range args {
			if other == index || len(args[other].roots) == 0 || !fact.roots.intersects(args[other].roots) {
				continue
			}
			if summary.changesParam(other) || summary.escapesParam(index) {
				a.report(at, diagnostics.CollectionViewPassedWithRoot(a.rootName(firstCommon(fact.roots, args[other].roots))))
			}
		}
		if !summary.unknown {
			for _, change := range summary.changes {
				if change.param >= 0 {
					continue
				}
				root := viewRoot{binding: change.binding, path: change.path}
				if fact.roots.contains(root) {
					a.report(at, diagnostics.CollectionViewPassedWithRoot(a.rootName(root)))
				}
			}
		}
	}
	a.applyChanges(summary, args, st, at)
	return a.callResult(node, summary, args, st)
}

func isIncomingOf(context *bodyContext, root viewRoot) bool {
	_, isParam := context.params[root.binding]
	return isParam && root.path == viewRootIncoming
}

func firstCommon(a, b rootSet) viewRoot {
	for _, root := range a {
		if b.contains(root) {
			return root
		}
	}
	return viewRoot{}
}

// applyChanges invalidates the storage a callee may structurally change.
func (a *viewAnalyzer) applyChanges(summary *viewSummary, args []viewArg, st *viewState, at span.Span) {
	if summary.unknown {
		for index := range args {
			a.invalidate(args[index].roots, at, st)
		}
		return
	}
	for _, change := range summary.changes {
		if change.param < 0 {
			a.invalidate(singleRoot(viewRoot{binding: change.binding, path: change.path}), at, st)
			continue
		}
		if change.param >= len(args) {
			continue
		}
		a.invalidate(args[change.param].rootsAt(change.path, st), at, st)
	}
}

// callResult derives the views and handle roots a call's result carries.
func (a *viewAnalyzer) callResult(node *Expression, summary *viewSummary, args []viewArg, st *viewState) viewValue {
	resultType := node.ResultType
	var viewRoots, handleRoots rootSet
	addArg := func(index int, path string) {
		if index < 0 || index >= len(args) {
			return
		}
		if path == viewRootIncoming {
			if fact, ok := args[index].value.whole(); ok {
				viewRoots = viewRoots.union(fact.roots)
			}
			return
		}
		if path == viewRootSelf {
			return
		}
		derived := args[index].rootsAt(path, st)
		viewRoots = viewRoots.union(derived)
		handleRoots = handleRoots.union(derived)
	}
	if summary.unknown {
		for index := range args {
			addArg(index, viewRootIncoming)
			addArg(index, "")
		}
	} else {
		for _, borrow := range summary.borrows {
			if borrow.param < 0 {
				derived := viewRoot{binding: borrow.binding, path: borrow.path}
				viewRoots = viewRoots.union(singleRoot(derived))
				handleRoots = handleRoots.union(singleRoot(derived))
				continue
			}
			addArg(borrow.param, borrow.path)
		}
	}
	result := viewValue{}
	if typeMayHoldView(resultType, 0) && len(viewRoots) > 0 {
		result = singleFact(viewFact{roots: viewRoots, kind: kindOfType(resultType)})
	}
	if canCarryHandles(resultType) && resultType != (compilerTypes.Type{}) {
		roots := handleRoots.union(singleRoot(freshRoot))
		result.roots = map[string]rootSet{"": roots}
	}
	return result
}

// mergeTargets folds the summaries of every callable a function value may name
// into one. An unknown target, or none at all, is an unresolved callee.
func mergeTargets(targets funcTargets) *viewSummary {
	if targets.unknown || len(targets.list) == 0 {
		return &viewSummary{unknown: true}
	}
	merged := &viewSummary{}
	for _, target := range targets.list {
		if target.unknown {
			return &viewSummary{unknown: true}
		}
		for _, root := range target.changes {
			insertSummaryRoot(&merged.changes, root)
		}
		for _, root := range target.escapes {
			insertSummaryRoot(&merged.escapes, root)
		}
		for _, root := range target.borrows {
			insertSummaryRoot(&merged.borrows, root)
		}
	}
	return merged
}

// checkTemporaryReceiver rejects a method call whose receiver is a
// materialized temporary when the result may be a view borrowed from the
// receiver: the temporary would die while the result stayed usable.
func (a *viewAnalyzer) checkTemporaryReceiver(node *Expression, summary *viewSummary, at span.Span) {
	if node.Kind != MethodCallExpression || node.Operand == nil || !node.Operand.MaterializedReceiver || !typeMayHoldView(node.ResultType, 0) {
		return
	}
	borrowsReceiver := summary.unknown
	for _, borrow := range summary.borrows {
		if borrow.param == 0 {
			borrowsReceiver = true
		}
	}
	if borrowsReceiver {
		owner := node.OperandType.Name
		if node.Owner != nil {
			owner = node.Owner.Name
		}
		a.report(at, diagnostics.MethodResultBorrowsTemporaryReceiver(owner, node.Name))
	}
}
