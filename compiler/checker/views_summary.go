package checker

import (
	"slices"
	"strings"

	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// Callable summaries and the analysis driver. A summary records, per
// callable, which argument or captured storage it may structurally change,
// which view arguments it may hide in untracked storage, and which arguments
// its result may derive from. Summaries are computed to a least fixpoint over
// the module's calls; an unresolved callee fails closed.

// summaryRoot names one piece of a callable's interface: parameter param
// (the receiver is parameter 0 of a method) at a member path, or, when param
// is negative, an entry-root binding the callable captures.
type summaryRoot struct {
	param   int
	binding BindingID
	path    string
}

func compareSummaryRoot(a, b summaryRoot) int {
	if a.param != b.param {
		if a.param < b.param {
			return -1
		}
		return 1
	}
	if a.binding != b.binding {
		if a.binding < b.binding {
			return -1
		}
		return 1
	}
	return strings.Compare(a.path, b.path)
}

// viewSummary is one callable's effect on collection views.
type viewSummary struct {
	changes []summaryRoot
	escapes []summaryRoot
	borrows []summaryRoot
	// unknown marks a callee the analysis cannot resolve: every handle
	// argument may change, every view argument may escape, and the result may
	// derive from any argument.
	unknown bool
	dirty   bool
}

func insertSummaryRoot(list *[]summaryRoot, root summaryRoot) bool {
	index, found := slices.BinarySearchFunc(*list, root, compareSummaryRoot)
	if found {
		return false
	}
	*list = slices.Insert(*list, index, root)
	return true
}

func (summary *viewSummary) addChange(root summaryRoot) {
	if insertSummaryRoot(&summary.changes, root) {
		summary.dirty = true
	}
}

func (summary *viewSummary) addEscape(root summaryRoot) {
	if insertSummaryRoot(&summary.escapes, root) {
		summary.dirty = true
	}
}

func (summary *viewSummary) addBorrow(root summaryRoot) {
	if insertSummaryRoot(&summary.borrows, root) {
		summary.dirty = true
	}
}

func (summary *viewSummary) escapesParam(index int) bool {
	if summary == nil || summary.unknown {
		return true
	}
	for _, root := range summary.escapes {
		if root.param == index {
			return true
		}
	}
	return false
}

func (summary *viewSummary) changesParam(index int) bool {
	if summary == nil || summary.unknown {
		return true
	}
	for _, root := range summary.changes {
		if root.param == index {
			return true
		}
	}
	return false
}

// callableKey identifies a callable for summary lookup: a module-level
// function by name, a method by its owner object and name.
type callableKey struct {
	name  string
	owner *compilerTypes.ObjectType
}

// callable is one body the analysis walks.
type callable struct {
	key        callableKey
	keyed      bool
	name       string
	params     []FunctionParameter
	selfType   *compilerTypes.Type
	selfID     BindingID
	captures   []Capture
	body       []Statement
	defers     []DeferredAction
	result     *compilerTypes.Type
	entryRoot  bool
	declaredAt span.Span
}

// bodyContext is the walk state of the callable being analyzed.
type bodyContext struct {
	callable    *callable
	summary     *viewSummary
	params      map[BindingID]int
	captured    map[BindingID]bool
	stmtSpan    span.Span
	unsafeDepth int
	loops       []*loopFrame
	scopes      [][]deferredCall
}

// viewAnalyzer holds one module's analysis.
type viewAnalyzer struct {
	table        *span.Table
	registry     *ModuleRegistry
	moduleID     string
	names        map[BindingID]string
	bindingTypes map[BindingID]compilerTypes.Type
	summaries    map[callableKey]*viewSummary
	callables    []*callable
	cur          *bodyContext
	reporting    bool
	seen         map[viewDiagnosticKey]bool
	diagnostics  compilerTypes.Diagnostics
}

// checkCollectionViews runs the stale-view analysis over one checked module.
// It runs after the module checked clean, so every operand it reads carries
// resolved types, and returns the diagnostics and the module's callable
// summaries for importers.
func checkCollectionViews(program Program, moduleID string, table *span.Table, registry *ModuleRegistry, entry bool) (compilerTypes.Diagnostics, map[callableKey]*viewSummary) {
	analyzer := &viewAnalyzer{
		table:        table,
		registry:     registry,
		moduleID:     moduleID,
		names:        make(map[BindingID]string),
		bindingTypes: make(map[BindingID]compilerTypes.Type),
		summaries:    make(map[callableKey]*viewSummary),
		seen:         make(map[viewDiagnosticKey]bool),
	}
	analyzer.collect(program, entry)
	for _, candidate := range analyzer.callables {
		if candidate.keyed {
			analyzer.summaries[candidate.key] = &viewSummary{}
		}
	}
	// Summaries only gain entries, and only over the finite set of parameters
	// and captures, so the iteration reaches a fixpoint: an unbounded loop
	// terminates, and a cap could leave a deep call chain under-approximated.
	for changed := true; changed; {
		changed = false
		for _, candidate := range analyzer.callables {
			analyzer.analyze(candidate)
			if candidate.keyed && analyzer.summaries[candidate.key].dirty {
				analyzer.summaries[candidate.key].dirty = false
				changed = true
			}
		}
	}
	analyzer.reporting = true
	for _, candidate := range analyzer.callables {
		analyzer.analyze(candidate)
	}
	return analyzer.diagnostics, analyzer.summaries
}

// collect gathers every callable body of the module: declared functions and
// methods, specializations, nested declarations, function literals, and the
// entry root.
func (a *viewAnalyzer) collect(program Program, entry bool) {
	for _, statement := range program.Statements {
		switch declaration := statement.(type) {
		case FunctionDeclaration:
			a.addFunction(declaration)
		case MethodDeclaration:
			a.addMethod(declaration)
		}
	}
	for _, declaration := range program.SpecializedFunctions {
		a.addFunction(declaration)
	}
	for _, declaration := range program.SpecializedMethods {
		a.addMethod(declaration)
	}
	if entry {
		a.callables = append(a.callables, &callable{name: "entry", body: program.Statements, defers: program.Defers, entryRoot: true})
		a.collectLiterals(program.Statements)
	}
}

func (a *viewAnalyzer) addFunction(declaration FunctionDeclaration) {
	a.callables = append(a.callables, &callable{
		key: callableKey{name: declaration.Name}, keyed: true, name: declaration.Name,
		params: declaration.Parameters, captures: declaration.Captures, body: declaration.Body,
		defers: declaration.Defers, result: declaration.Result, declaredAt: declaration.Span,
	})
	a.collectLiterals(declaration.Body)
}

func (a *viewAnalyzer) addMethod(declaration MethodDeclaration) {
	self := declaration.SelfType
	a.callables = append(a.callables, &callable{
		key: callableKey{name: declaration.Name, owner: declaration.Object}, keyed: true, name: declaration.Name,
		params: declaration.Parameters, selfType: &self, selfID: declaration.SelfBinding,
		captures: declaration.Captures, body: declaration.Body, defers: declaration.Defers,
		result: declaration.Result, declaredAt: declaration.Span,
	})
	a.collectLiterals(declaration.Body)
}

// collectLiterals finds anonymous function literals inside a body. Each is
// analyzed for its own diagnostics but is not summarized: a call through a
// Fun value resolves as unknown.
func (a *viewAnalyzer) collectLiterals(statements []Statement) {
	visit := func(operand Operand) { a.collectLiteralsIn(&operand.Node) }
	for _, statement := range statements {
		switch s := statement.(type) {
		case Declaration:
			visit(s.Source)
		case Assignment:
			visit(s.Source)
			visit(s.Target)
		case CallStatement:
			visit(s.Call)
		case TryStatement:
			visit(s.Expression)
		case DeferStatement:
			visit(s.Expression)
		case ReturnStatement:
			if s.Value != nil {
				visit(*s.Value)
			}
		case IfStatement:
			visit(s.Condition)
			a.collectLiterals(s.Then)
			for _, branch := range s.ElseIf {
				visit(branch.Condition)
				a.collectLiterals(branch.Body)
			}
			a.collectLiterals(s.Else)
		case WhileStatement:
			visit(s.Condition)
			a.collectLiterals(s.Body)
		case ForStatement:
			visit(s.Source)
			a.collectLiterals(s.Body)
		case UnsafeStatement:
			a.collectLiterals(s.Body)
		}
	}
}

func (a *viewAnalyzer) collectLiteralsIn(node *Expression) {
	if node == nil {
		return
	}
	if node.Kind == FunctionLiteralExpression && node.Function != nil {
		literal := node.Function
		a.callables = append(a.callables, &callable{
			name: "literal", params: literal.Parameters, body: literal.Body,
			defers: literal.Defers, result: literal.Result, declaredAt: literal.Span,
		})
		a.collectLiterals(literal.Body)
	}
	a.collectLiteralsIn(node.Operand)
	a.collectLiteralsIn(node.Left)
	a.collectLiteralsIn(node.Right)
	for index := range node.Arguments {
		a.collectLiteralsIn(&node.Arguments[index].Node)
	}
	if node.Object != nil {
		for _, initializer := range node.Object.Initializers {
			a.collectLiteralsIn(&initializer.Source.Node)
		}
	}
}

// typeMayHoldView reports whether a value of typ can carry a pointer, Slice,
// or cursor.
func typeMayHoldView(typ compilerTypes.Type, depth int) bool {
	if depth > 6 {
		return true
	}
	switch {
	case typ.Slice != nil, compilerTypes.IsCursor(typ), compilerTypes.IsGrapheme(typ):
		return true
	case typ.Element != nil && typ.Signature == nil && typ.Union == nil && !compilerTypes.IsNullable(typ):
		return true
	case typ.Object != nil:
		for _, member := range typ.Object.Members {
			if typeMayHoldView(member.Type, depth+1) {
				return true
			}
		}
	case typ.Adt != nil:
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if typeMayHoldView(member.Type, depth+1) {
					return true
				}
			}
		}
	case typ.InlineList != nil:
		return typeMayHoldView(typ.InlineList.Element, depth+1)
	case typ.Union != nil:
		members := compilerTypes.UnionMembers(typ)
		for index := 0; index < members.Len(); index++ {
			if member, _ := members.At(index); typeMayHoldView(member, depth+1) {
				return true
			}
		}
	case compilerTypes.IsNullable(typ) && typ.NullableBase != nil:
		return typeMayHoldView(*typ.NullableBase, depth+1)
	}
	return false
}

func kindOfType(typ compilerTypes.Type) viewKind {
	switch {
	case typ.Slice != nil:
		return viewSlice
	case compilerTypes.IsCursor(typ), compilerTypes.IsGrapheme(typ):
		return viewCursor
	}
	return viewPointer
}

// analyze walks one callable body and accumulates its summary.
func (a *viewAnalyzer) analyze(target *callable) {
	context := &bodyContext{
		callable: target,
		params:   make(map[BindingID]int),
		captured: make(map[BindingID]bool),
		summary:  &viewSummary{},
	}
	if target.keyed {
		context.summary = a.summaries[target.key]
	}
	a.cur = context
	state := newViewState()
	index := 0
	if target.selfType != nil {
		context.params[target.selfID] = index
		a.names[target.selfID] = "self"
		a.bindingTypes[target.selfID] = *target.selfType
		a.seedParam(state, target.selfID, *target.selfType)
		index++
	}
	for _, parameter := range target.params {
		context.params[parameter.Binding] = index
		a.names[parameter.Binding] = parameter.Name
		a.bindingTypes[parameter.Binding] = parameter.Type
		a.seedParam(state, parameter.Binding, parameter.Type)
		index++
	}
	for _, capture := range target.captures {
		context.captured[capture.Binding] = true
		a.names[capture.Binding] = capture.Name
		a.bindingTypes[capture.Binding] = capture.Type
	}
	context.scopes = append(context.scopes, nil)
	end := a.stmts(target.body, state)
	a.leaveScope(end)
	a.cur = nil
}

// seedParam gives a parameter that can carry a view an abstract incoming view,
// so summaries can tell what the callable does with the caller's views.
func (a *viewAnalyzer) seedParam(state *viewState, binding BindingID, typ compilerTypes.Type) {
	if !typeMayHoldView(typ, 0) {
		return
	}
	state.facts[viewPlace{binding: binding}] = viewFact{
		roots: singleRoot(viewRoot{binding: binding, path: viewRootIncoming}),
		kind:  kindOfType(typ),
	}
}

// registerViewSummaries publishes one module's callable summaries so its
// importers resolve calls through them.
func (registry *ModuleRegistry) registerViewSummaries(moduleID string, summaries map[callableKey]*viewSummary) {
	if registry == nil {
		return
	}
	if entry := registry.modules[moduleID]; entry != nil {
		entry.viewSummaries = summaries
	}
}

func (registry *ModuleRegistry) viewSummaryOfFunction(moduleID, name string) (*viewSummary, bool) {
	entry := registry.modules[moduleID]
	if entry == nil {
		return nil, false
	}
	summary, ok := entry.viewSummaries[callableKey{name: name}]
	return summary, ok
}

func (registry *ModuleRegistry) viewSummaryOfMethod(owner *compilerTypes.ObjectType, name string) (*viewSummary, bool) {
	entry := registry.modules[owner.ModuleID]
	if entry == nil {
		return nil, false
	}
	summary, ok := entry.viewSummaries[callableKey{name: name, owner: owner}]
	return summary, ok
}
