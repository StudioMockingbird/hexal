package checker

import (
	"hexal/compiler/diagnostics"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// Transfer functions of the stale-view analysis over checked expressions and
// statements. The walk is structured: branches join by union, loops iterate
// to a fixpoint, and a nil state marks a point no path reaches.

// freshRoot marks a handle value that names storage no other place names yet
// (a constructor result); binding it gives the receiving place its own root.
var freshRoot = viewRoot{binding: 0, path: "#fresh"}

// loopFrame collects the states that leave a loop body by break or return to
// the loop head by continue.
type loopFrame struct {
	breaks    []*viewState
	continues []*viewState
	// scopeDepth is the cleanup-scope depth outside the loop body: a jump out
	// of the body applies the cleanup of every scope above it.
	scopeDepth int
}

// deferredCall is one registered cleanup whose operands were evaluated at
// registration: its receiver and argument values are fixed, and only its
// effect is applied when the scope exits.
type deferredCall struct {
	node   *Expression
	args   []viewArg
	callee funcTargets
	span   span.Span
}

// viewArg is one evaluated call operand: the value it carries, the handle
// roots it names, and whether its static type can hold a view.
type viewArg struct {
	value viewValue
	roots rootSet
	typ   compilerTypes.Type
	span  span.Span
	// place is the tracked place the operand names, when it names one: a
	// callee's change to a member path below the argument resolves through
	// the place's own handle relation, not through a suffix of its roots.
	place    viewPlace
	hasPlace bool
}

// withPlace records the tracked place an operand node names.
func (arg viewArg) withPlace(node *Expression) viewArg {
	arg.place, arg.hasPlace = placeOfNode(node)
	return arg
}

// rootsAt resolves the handle roots of the argument's member path.
func (arg viewArg) rootsAt(path string, st *viewState) rootSet {
	if path == "" {
		return arg.roots
	}
	if arg.hasPlace {
		return st.rootsOfPlace(arg.place.child(path))
	}
	var derived rootSet
	for _, root := range arg.roots {
		derived = derived.union(singleRoot(viewRoot{binding: root.binding, path: root.path + path}))
	}
	return derived
}

func (a *viewAnalyzer) report(at span.Span, message diagnostics.Message) {
	if !a.reporting {
		return
	}
	key := viewDiagnosticKey{span: at, text: message.Text()}
	if a.seen[key] {
		return
	}
	a.seen[key] = true
	token := tokenAt(a.table, at)
	a.diagnostics = append(a.diagnostics, messageAt(token, message))
}

type viewDiagnosticKey struct {
	span span.Span
	text string
}

func (a *viewAnalyzer) rootName(root viewRoot) string {
	name := a.names[root.binding]
	if name == "" {
		name = "a collection"
	}
	path := root.path
	if index := indexOfGeneration(path); index >= 0 {
		path = path[:index]
	}
	return name + path
}

func indexOfGeneration(path string) int {
	for index := 0; index < len(path); index++ {
		if path[index] == '@' || path[index] == '#' {
			return index
		}
	}
	return -1
}

// currentSpan is the source site diagnostics name: the expression's own span
// when it has one, otherwise the statement being walked.
func (a *viewAnalyzer) currentSpan(node *Expression) span.Span {
	if node != nil && node.Span != (span.Span{}) {
		return node.Span
	}
	return a.cur.stmtSpan
}

// structuralListOperation is the closed set of built-in operations that change
// an allocated List's storage, shared by view invalidation and traversal
// checking. Element replacement is not structural.
func structuralListOperation(name string) bool {
	switch name {
	case "push", "pop", "clear", "free":
		return true
	}
	return false
}

// structuralCollectionOperation classifies a built-in collection method that
// changes the collection's own storage, other than freeing it.
func structuralCollectionOperation(collection compilerTypes.Type, name string) bool {
	if collection.Dict != nil {
		return name == "insert" || name == "remove" || name == "clear"
	}
	return name != "free" && structuralListOperation(name)
}

func canCarryHandles(typ compilerTypes.Type) bool {
	return typ.List != nil || compilerTypes.IsString(typ) || typ.Object != nil || typ.Adt != nil ||
		typ.InlineList != nil || typ.Union != nil || compilerTypes.IsNullable(typ) || typ == (compilerTypes.Type{})
}

// placeOfNode resolves a checked node to a tracked place when it is a binding
// or a member or inline-element path below one.
func placeOfNode(node *Expression) (viewPlace, bool) {
	if node == nil {
		return viewPlace{}, false
	}
	switch node.Kind {
	case VariableExpression:
		if node.Binding == 0 {
			return viewPlace{}, false
		}
		return viewPlace{binding: node.Binding}, true
	case MemberExpression:
		base, ok := placeOfNode(node.Operand)
		if !ok || node.Member == nil {
			return viewPlace{}, false
		}
		return base.child("." + node.Member.Name), true
	case IndexExpression:
		if node.OperandType.InlineList == nil {
			return viewPlace{}, false
		}
		base, ok := placeOfNode(node.Operand)
		if !ok {
			return viewPlace{}, false
		}
		return base.child("[]"), true
	case UnionPayloadExpression, AdtPayloadExpression:
		return placeOfNode(node.Operand)
	}
	return viewPlace{}, false
}

func (a *viewAnalyzer) typeOfBinding(id BindingID) compilerTypes.Type {
	return a.bindingTypes[id]
}

// useCheck rejects a read of a place holding a view a structural change
// reached.
func (a *viewAnalyzer) useCheck(place viewPlace, st *viewState, at span.Span) {
	fact, stale := st.staleFact(place)
	if !stale {
		return
	}
	position := positionAt(a.table, fact.span)
	a.report(at, diagnostics.StaleCollectionView(fact.kind.noun(), a.rootName(fact.cause), position.Line, position.Column))
}

func positionAt(table *span.Table, at span.Span) span.Position {
	if table == nil {
		return span.Position{}
	}
	return table.Position(at)
}

// evalOperand evaluates one checked operand and returns what it carries.
func (a *viewAnalyzer) evalOperand(operand Operand, st *viewState) viewValue {
	value := a.evalNode(&operand.Node, st)
	if operand.Type != (compilerTypes.Type{}) && !canCarryHandles(operand.Type) {
		value.roots = nil
		value.alias = nil
	}
	return value
}

func (a *viewAnalyzer) evalChildren(node *Expression, st *viewState) {
	if node.Operand != nil {
		a.evalNode(node.Operand, st)
	}
	if node.Left != nil {
		a.evalNode(node.Left, st)
	}
	if node.Right != nil {
		a.evalNode(node.Right, st)
	}
	for index := range node.Arguments {
		a.evalOperand(node.Arguments[index], st)
	}
	if node.Object != nil {
		for _, initializer := range node.Object.Initializers {
			a.evalOperand(initializer.Source, st)
		}
	}
	for _, segment := range node.InterpolationSegments {
		if segment.IsValue {
			a.evalOperand(segment.Value, st)
		}
	}
}

// evalArguments evaluates call operands left to right. Each finished value is
// registered in flight so a structural change made by a later operand stales
// it, and every operand is rechecked once the list is complete.
func (a *viewAnalyzer) evalArguments(operands []Operand, st *viewState) []viewArg {
	args := make([]viewArg, len(operands))
	registered := len(st.inflight)
	for index := range operands {
		value := a.evalOperand(operands[index], st)
		args[index] = viewArg{value: value, roots: value.handleRoots(), typ: operands[index].Type, span: a.currentSpan(&operands[index].Node)}
		st.inflight = append(st.inflight, &args[index].value)
	}
	st.inflight = st.inflight[:registered]
	for index := range args {
		if fact, ok := args[index].value.whole(); ok && fact.stale {
			position := positionAt(a.table, fact.span)
			a.report(args[index].span, diagnostics.StaleCollectionView(fact.kind.noun(), a.rootName(fact.cause), position.Line, position.Column))
		}
	}
	return args
}

func (a *viewAnalyzer) newView(roots rootSet, kind viewKind) viewValue {
	if len(roots) == 0 {
		return viewValue{}
	}
	return singleFact(viewFact{roots: roots, kind: kind})
}

// recordChange notes a structural change to roots for the callable summary.
func (a *viewAnalyzer) recordChange(roots rootSet) {
	for _, root := range roots {
		if index, ok := a.cur.params[root.binding]; ok && !isSpecialPath(root.path) {
			a.cur.summary.addChange(summaryRoot{param: index, path: stripGeneration(root.path)})
		} else if a.cur.captured[root.binding] && !isSpecialPath(root.path) {
			a.cur.summary.addChange(summaryRoot{param: -1, binding: root.binding, path: stripGeneration(root.path)})
		}
	}
}

func isSpecialPath(path string) bool {
	return indexOfGeneration(path) >= 0 && path[indexOfGeneration(path)] == '#'
}

func stripGeneration(path string) string {
	if index := indexOfGeneration(path); index >= 0 {
		return path[:index]
	}
	return path
}

func (a *viewAnalyzer) invalidate(roots rootSet, at span.Span, st *viewState) {
	st.invalidate(roots, at)
	a.recordChange(roots)
}

// handleRootsOf resolves the roots the handle an expression names.
func (a *viewAnalyzer) handleRootsOf(node *Expression, value viewValue, st *viewState) rootSet {
	if place, ok := placeOfNode(node); ok {
		return st.rootsOfPlace(place)
	}
	if node != nil && node.Kind == IndexExpression && node.OperandType.List != nil {
		// An element of an allocated List that is itself a handle: its root is
		// the element slot of the outer storage.
		var roots rootSet
		if node.Operand != nil {
			outer := a.evalNode(node.Operand, st)
			for _, root := range outer.handleRoots() {
				roots = roots.union(singleRoot(viewRoot{binding: root.binding, path: root.path + "[]"}))
			}
		}
		return roots
	}
	return value.handleRoots()
}

// stripAdaptation removes the address-of or dereference wrappers a method
// receiver carries so the handle place underneath is visible.
func stripAdaptation(node *Expression) *Expression {
	for node != nil && (node.Kind == AddressOfExpression || node.Kind == DereferenceExpression) && node.Operand != nil {
		node = node.Operand
	}
	return node
}

// evalNode evaluates one checked expression.
func (a *viewAnalyzer) evalNode(node *Expression, st *viewState) viewValue {
	if node == nil || st == nil {
		return viewValue{}
	}
	switch node.Kind {
	case VariableExpression:
		place := viewPlace{binding: node.Binding}
		if node.Binding == 0 {
			return viewValue{}
		}
		a.useCheck(place, st, a.currentSpan(node))
		value := st.readPlace(place)
		if typ, known := a.bindingTypes[node.Binding]; known && !canCarryHandles(typ) {
			value.roots = nil
			value.alias = nil
		}
		return value
	case MemberExpression:
		if place, ok := placeOfNode(node); ok {
			a.useCheck(place, st, a.currentSpan(node))
			return st.readPlace(place)
		}
		a.evalChildren(node, st)
		return viewValue{}
	case IndexExpression:
		if place, ok := placeOfNode(node); ok {
			a.useCheck(place, st, a.currentSpan(node))
			value := st.readPlace(place)
			a.evalArgumentsOnly(node, st)
			return value
		}
		a.evalChildren(node, st)
		return viewValue{}
	case UnionPayloadExpression, AdtPayloadExpression:
		return a.evalNode(node.Operand, st)
	case DereferenceExpression:
		// Reading through a pointer reads the pointee, not the pointer: the
		// result is data, but the pointer operand is used.
		a.evalNode(node.Operand, st)
		return viewValue{}
	case AddressOfExpression:
		return a.evalAddressOf(node, st)
	case CollectionSliceExpression:
		return a.evalSlice(node, st)
	case CollectionMethodCallExpression:
		return a.evalCollectionMethod(node, st)
	case StringMethodCallExpression:
		return a.evalStringMethod(node, st)
	case CursorMethodCallExpression, GraphemeMethodCallExpression:
		value := a.evalNode(node.Operand, st)
		a.evalArgumentsOnly(node, st)
		if node.ResultType.Name != "" && (compilerTypes.IsGrapheme(node.ResultType) || node.ResultType.Slice != nil) {
			if fact, ok := value.whole(); ok {
				return singleFact(viewFact{roots: fact.roots, kind: viewSliceOrCursor(node.ResultType), stale: fact.stale, span: fact.span, cause: fact.cause})
			}
		}
		return viewValue{}
	case PointerOffsetExpression, PointerCastExpression:
		value := a.evalNode(node.Operand, st)
		a.evalArgumentsOnly(node, st)
		return retainFacts(value, viewPointer)
	case PointerIndexExpression:
		a.evalNode(node.Operand, st)
		a.evalArgumentsOnly(node, st)
		return viewValue{}
	case SliceBridgeExpression:
		var carried viewValue
		for index := range node.Arguments {
			value := a.evalOperand(node.Arguments[index], st)
			if index == 0 {
				carried = value
			}
		}
		return retainFacts(carried, viewSlice)
	case CallExpression, MethodCallExpression:
		return a.evalCall(node, st)
	case ObjectExpression:
		return a.evalObject(node, st)
	case AdtConstructExpression:
		args := a.evalArguments(node.Arguments, st)
		values := make([]viewValue, len(args))
		for index := range args {
			values[index] = args[index].value
			values[index].alias = nil
		}
		return mergeWhole(values...)
	case InlineListLiteralExpression:
		args := a.evalArguments(node.Arguments, st)
		merged := viewValue{}
		for index := range args {
			if fact, ok := args[index].value.whole(); ok {
				merged = mergeValues(merged, viewValue{facts: map[string]viewFact{"[]": fact}})
			}
		}
		return merged
	case UnionInjectionExpression, UnionWidenExpression:
		value := a.evalNode(node.Operand, st)
		a.evalArgumentsOnly(node, st)
		return value
	case TryExpression:
		value := a.evalNode(node.Operand, st)
		return value
	case SpawnExpression:
		a.evalSpawn(node, st)
		return viewValue{}
	case MatchExpression:
		return a.evalMatch(node, st)
	case BinaryOperationExpression:
		if node.Operator == LogicalAndOperator || node.Operator == LogicalOrOperator {
			a.evalNode(node.Left, st)
			branch := st.clone()
			a.evalNode(node.Right, branch)
			joined := joinViewStates(st, branch)
			*st = *joined
			return viewValue{}
		}
	case ListNewExpression, DictNewExpression:
		a.evalChildren(node, st)
		return viewValue{roots: map[string]rootSet{"": singleRoot(freshRoot)}}
	case HeapAllocateExpression, HeapAllocateAlignedExpression:
		args := a.evalArguments(node.Arguments, st)
		if node.Operand != nil {
			a.evalNode(node.Operand, st)
		}
		for index := range args {
			a.escape(args[index].value, node, st)
		}
		return viewValue{}
	case StashMethodCallExpression, PoolMethodCallExpression:
		if node.Operand != nil {
			a.evalNode(node.Operand, st)
		}
		args := a.evalArguments(node.Arguments, st)
		if node.Name == "allocate" {
			for index := range args {
				a.escape(args[index].value, node, st)
			}
		}
		return viewValue{}
	case FunctionLiteralExpression:
		return viewValue{targets: funcTargets{unknown: true}}
	case FunctionReferenceExpression, ForeignFunctionReferenceExpression:
		summary, known := a.summaryFor(&Expression{Kind: CallExpression, Operand: node})
		if !known {
			return viewValue{targets: funcTargets{unknown: true}}
		}
		return viewValue{targets: funcTargets{list: []*viewSummary{summary}}}
	}
	a.evalChildren(node, st)
	return viewValue{}
}

func viewSliceOrCursor(typ compilerTypes.Type) viewKind {
	if typ.Slice != nil {
		return viewSlice
	}
	return viewCursor
}

// retainFacts keeps the views of value under a new kind.
func retainFacts(value viewValue, kind viewKind) viewValue {
	fact, ok := value.whole()
	if !ok {
		return viewValue{}
	}
	fact.kind = kind
	return singleFact(fact)
}

func mergeWhole(values ...viewValue) viewValue {
	merged := viewValue{}
	for _, value := range values {
		if fact, ok := value.whole(); ok {
			merged = mergeValues(merged, singleFact(fact))
		}
		if roots := value.handleRoots(); len(roots) > 0 {
			merged = mergeValues(merged, viewValue{roots: map[string]rootSet{"": roots}})
		}
	}
	return merged
}

// evalArgumentsOnly evaluates a node's Arguments for their checks and effects
// when the node's own meaning does not consume their values.
func (a *viewAnalyzer) evalArgumentsOnly(node *Expression, st *viewState) {
	a.evalArguments(node.Arguments, st)
}

// evalAddressOf evaluates &place. The result is a pointer view when the place
// lies inside an allocated List's element storage or below another view.
func (a *viewAnalyzer) evalAddressOf(node *Expression, st *viewState) viewValue {
	var derived viewValue
	current := node.Operand
	for current != nil {
		switch current.Kind {
		case IndexExpression:
			switch {
			case current.OperandType.List != nil:
				receiver := a.evalNode(current.Operand, st)
				a.evalArgumentsOnly(current, st)
				roots := a.handleRootsOf(current.Operand, receiver, st)
				return a.newView(roots, viewPointer)
			case current.OperandType.Slice != nil:
				receiver := a.evalNode(current.Operand, st)
				a.evalArgumentsOnly(current, st)
				return retainFacts(receiver, viewPointer)
			default:
				a.evalArgumentsOnly(current, st)
				current = current.Operand
				continue
			}
		case MemberExpression, UnionPayloadExpression, AdtPayloadExpression:
			current = current.Operand
			continue
		case DereferenceExpression, PointerIndexExpression:
			derived = a.evalNode(current.Operand, st)
			a.evalArgumentsOnly(current, st)
			return retainFacts(derived, viewPointer)
		case VariableExpression:
			// The address of a local's own storage: a local that holds a view
			// reads it; the address itself points at the local, not a root. The
			// receiver is the caller's storage, so an address derived from self
			// is a view the callee cannot name.
			if current.Binding != 0 {
				place := viewPlace{binding: current.Binding}
				a.useCheck(place, st, a.currentSpan(current))
				if a.cur.callable.selfType != nil && current.Binding == a.cur.callable.selfID {
					return singleFact(viewFact{roots: singleRoot(viewRoot{binding: current.Binding, path: viewRootSelf}), kind: viewPointer})
				}
			}
			return viewValue{}
		default:
			return a.evalNode(current, st)
		}
	}
	return viewValue{}
}

// evalSlice evaluates List.slice, List.mut_slice, and Slice.slice.
func (a *viewAnalyzer) evalSlice(node *Expression, st *viewState) viewValue {
	receiver := a.evalNode(node.Operand, st)
	a.evalArgumentsOnly(node, st)
	switch {
	case node.OperandType.List != nil:
		return a.newView(a.handleRootsOf(node.Operand, receiver, st), viewSlice)
	case node.OperandType.Slice != nil:
		return retainFacts(receiver, viewSlice)
	}
	return viewValue{}
}

// evalCollectionMethod evaluates a built-in List, Slice, or Dict method.
func (a *viewAnalyzer) evalCollectionMethod(node *Expression, st *viewState) viewValue {
	receiver := a.evalNode(node.Operand, st)
	args := a.evalArguments(node.Arguments, st)
	switch {
	case node.OperandType.List != nil:
		roots := a.handleRootsOf(node.Operand, receiver, st)
		if node.Name == "push" {
			for index := range args {
				a.escape(args[index].value, node, st)
			}
		}
		if structuralListOperation(node.Name) {
			a.invalidate(roots, a.currentSpan(node), st)
		}
	case node.OperandType.Dict != nil:
		if node.Name == "insert" {
			for index := range args {
				a.escape(args[index].value, node, st)
			}
		}
	}
	return viewValue{}
}

// evalStringMethod evaluates a built-in text method. Only an allocated String
// owns storage a view can outlive.
func (a *viewAnalyzer) evalStringMethod(node *Expression, st *viewState) viewValue {
	receiver := a.evalNode(node.Operand, st)
	a.evalArgumentsOnly(node, st)
	if !compilerTypes.IsString(node.OperandType) {
		return viewValue{}
	}
	roots := a.handleRootsOf(node.Operand, receiver, st)
	switch node.Name {
	case "bytes", "slice":
		return a.newView(roots, viewSlice)
	case "byte_cursor", "rune_cursor", "grapheme_cursor":
		return a.newView(roots, viewCursor)
	case "c_pointer":
		return a.newView(roots, viewPointer)
	case "free":
		a.invalidate(roots, a.currentSpan(node), st)
	}
	return viewValue{}
}

// evalObject evaluates an object literal, keeping each member's views and
// handle roots at the member's own path.
func (a *viewAnalyzer) evalObject(node *Expression, st *viewState) viewValue {
	if node.Object == nil {
		a.evalChildren(node, st)
		return viewValue{}
	}
	result := viewValue{}
	registered := len(st.inflight)
	collected := make([]viewValue, len(node.Object.Initializers))
	for index, initializer := range node.Object.Initializers {
		collected[index] = a.evalOperand(initializer.Source, st)
		st.inflight = append(st.inflight, &collected[index])
	}
	st.inflight = st.inflight[:registered]
	for index, initializer := range node.Object.Initializers {
		if initializer.Member == nil {
			continue
		}
		prefix := "." + initializer.Member.Name
		value := collected[index]
		for path, fact := range value.facts {
			if result.facts == nil {
				result.facts = make(map[string]viewFact)
			}
			result.facts[prefix+path] = fact
		}
		for path, roots := range value.roots {
			if result.roots == nil {
				result.roots = make(map[string]rootSet)
			}
			result.roots[prefix+path] = roots
		}
	}
	return result
}

// evalMatch evaluates a match expression: the arms are alternative paths.
func (a *viewAnalyzer) evalMatch(node *Expression, st *viewState) viewValue {
	if node.Operand != nil {
		a.evalNode(node.Operand, st)
	}
	var arms []*viewState
	var values []viewValue
	for index := range node.Arguments {
		branch := st.clone()
		values = append(values, a.evalOperand(node.Arguments[index], branch))
		arms = append(arms, branch)
	}
	if joined := joinViewStates(arms...); joined != nil {
		*st = *joined
	}
	return mergeWhole(values...)
}

// evalSpawn rejects a view handed to a spawned Task: the Task outlives the
// statement, so the checker cannot follow the view any further.
func (a *viewAnalyzer) evalSpawn(node *Expression, st *viewState) {
	call := node.Operand
	if call == nil {
		return
	}
	args := a.evalArguments(call.Arguments, st)
	if call.Operand != nil && call.Operand.Kind != FunctionReferenceExpression {
		a.evalNode(call.Operand, st)
	}
	for index := range args {
		if fact, ok := args[index].value.whole(); ok {
			for _, root := range fact.roots {
				if root.path == viewRootSelf {
					a.report(a.currentSpan(node), diagnostics.SelfAddressEscapesTask())
				}
			}
		}
		a.escape(args[index].value, node, st)
	}
}

// escape records or rejects hiding a view in storage the checker does not
// follow. A view of an argument the callee received is a summary fact; a view
// of a root the compiler tracks is rejected outside unsafe.
func (a *viewAnalyzer) escape(value viewValue, node *Expression, st *viewState) {
	fact, ok := value.whole()
	if !ok || len(fact.roots) == 0 {
		return
	}
	for _, root := range fact.roots {
		if index, isParam := a.cur.params[root.binding]; isParam && root.path == viewRootIncoming {
			a.cur.summary.addEscape(summaryRoot{param: index})
			continue
		}
		if root.path == viewRootSelf {
			continue
		}
		if a.cur.unsafeDepth > 0 {
			continue
		}
		a.report(a.currentSpan(node), diagnostics.CollectionViewEscapesRoot(a.rootName(root)))
	}
}
