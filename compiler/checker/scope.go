package checker

import (
	"fmt"

	"hexal/compiler/lexer"
	compilerTypes "hexal/compiler/types"
)

// bindingKind separates storage from a declared function and from an import
// alias. A function name is not a place: it can be read as a Fun<...> value
// and nothing else. An import alias is not a value at all: name lookup skips
// it, and only qualified resolution reaches its target module.
type bindingKind uint8

const (
	dataBinding bindingKind = iota
	functionBinding
	genericFunctionBinding
	aliasBinding
)

// scope is one lexical name frame. Module bindings remain in module and are
// intentionally hidden from function bodies; control-flow frames chain to
// their enclosing frame so branch declarations do not escape their block.
type scope struct {
	module       map[string]binding
	local        map[string]binding // nil only at module level
	parent       *scope
	moduleID     string // the enclosing module's canonical identity
	logicalKey   string // the enclosing module's source-map filename
	owner        string // enclosing function or method name, for diagnostics
	result       *compilerTypes.Type
	resultUse    *compilerTypes.TypeUse
	methods      *methodTable
	self         *compilerTypes.Type // the impl target type; nil outside an impl body
	selfID       BindingID
	function     bool
	nextID       *BindingID
	flow         *flowState // branch-local narrowing facts
	generics     *genericTable
	defers       []DeferredAction
	returnFlows  []returnFlow // states and active actions reaching a return
	cleanupDepth int          // checking a defer or errdefer action
	// registry is the compilation's module graph: it resolves import aliases
	// against the target modules' exported records.
	// It is shared by reference with every child scope.
	registry *ModuleRegistry
}

// moduleScope builds the root frame of one module. Import aliases are read
// from the registry through importTarget; the scope keeps no copy of its own.
func moduleScope(moduleID string, logicalKey string, registry *ModuleRegistry) *scope {
	next := BindingID(0)
	generics := newGenericTable()
	if registry != nil {
		// Qualified type references resolve through the module's import
		// graph, which the generic table carries for the type resolver.
		generics.registry = registry
		generics.moduleID = moduleID
	}
	return &scope{module: make(map[string]binding), moduleID: moduleID, logicalKey: logicalKey, methods: newMethodTable(), nextID: &next, flow: newFlowState(), generics: generics, registry: registry}
}

// flowFact records the branch-local treatment of one binding. Narrowing and
// freed facts survive only while the binding remains trackable; escape clears
// both because a write through the escaped address can change the slot.
type flowFact struct {
	typ     compilerTypes.Type // effective read type; zero Type when not narrowed
	escaped bool
	variant *compilerTypes.AdtVariant // active ADT variant when variant-narrowed
	freed   bool                      // the tracked pointer's pointee was released on every path here
	version uint64                    // identity of the pointer value currently in the binding
}

// returnFlow carries one reachable return state and the actions registered
// before that return in the scope being validated.
type returnFlow struct {
	state   *flowState
	actions []DeferredAction
}

// flowState is the branch-local fact table for one function body or module
// scope. tracked distinguishes a known cleanup state from an intentionally
// unknown state after a copy or escape. released retains proven cleanup of
// older pointer values so deferred captures survive later rebinding.
type flowState struct {
	facts    map[BindingID]flowFact
	tracked  map[BindingID]bool
	released map[BindingID]map[uint64]bool
}

func newFlowState() *flowState {
	return &flowState{
		facts:    make(map[BindingID]flowFact),
		tracked:  make(map[BindingID]bool),
		released: make(map[BindingID]map[uint64]bool),
	}
}

func (state *flowState) clone() *flowState {
	cloned := newFlowState()
	for id, fact := range state.facts {
		cloned.facts[id] = fact
	}
	for id := range state.tracked {
		cloned.tracked[id] = true
	}
	for id, versions := range state.released {
		cloned.released[id] = make(map[uint64]bool, len(versions))
		for version := range versions {
			cloned.released[id][version] = true
		}
	}
	return cloned
}

// withoutFreedChecks gives exit-time expression typing a flow view that
// retains types but cannot observe or mutate cleanup facts at registration.
func (state *flowState) withoutFreedChecks() *flowState {
	cloned := state.clone()
	cloned.tracked = make(map[BindingID]bool)
	cloned.released = make(map[BindingID]map[uint64]bool)
	for id, fact := range cloned.facts {
		fact.freed = false
		cloned.facts[id] = fact
	}
	return cloned
}

// lookupBinding resolves a binding record by identity through the scope
// chain.
func (names *scope) lookupBinding(id BindingID) (binding, bool) {
	for frame := names; frame != nil; frame = frame.parent {
		for _, bound := range frame.local {
			if bound.id == id {
				return bound, true
			}
		}
		for _, bound := range frame.module {
			if bound.id == id {
				return bound, true
			}
		}
	}
	return binding{}, false
}

// setFromRef updates the fromRef flag of a binding by identity, writing the
// change back into the frame that owns the binding. It reports whether the
// binding was found.
func (names *scope) setFromRef(id BindingID, fromRef bool) bool {
	for frame := names; frame != nil; frame = frame.parent {
		for name, bound := range frame.local {
			if bound.id == id {
				bound.fromRef = fromRef
				frame.local[name] = bound
				return true
			}
		}
		for name, bound := range frame.module {
			if bound.id == id {
				bound.fromRef = fromRef
				frame.module[name] = bound
				return true
			}
		}
	}
	return false
}

// narrowedType returns the branch-local effective read type of a binding, if
// a narrowing covers it. An escaped binding has no narrowing.
func (state *flowState) narrowedType(id BindingID) (compilerTypes.Type, bool) {
	if state == nil {
		return compilerTypes.Type{}, false
	}
	fact, ok := state.facts[id]
	if !ok || fact.escaped {
		return compilerTypes.Type{}, false
	}
	return fact.typ, fact.typ != (compilerTypes.Type{})
}

// narrow records that a null test proved the binding holds typ. An escaped
// binding is never narrowable: a write through the escaped address could
// replace the slot at any time. Cleanup state is independent of the
// narrowing, so narrowing never revives or loses a freed fact.
func (state *flowState) narrow(id BindingID, typ compilerTypes.Type) {
	fact, ok := state.facts[id]
	if ok && fact.escaped {
		return
	}
	fact.typ = typ
	fact.variant = nil
	state.facts[id] = fact
}

// narrowVariant records that a match arm proved the binding holds one ADT
// variant. An escaped binding is never narrowable.
func (state *flowState) narrowVariant(id BindingID, variant *compilerTypes.AdtVariant) {
	fact, ok := state.facts[id]
	if ok && fact.escaped {
		return
	}
	fact.typ = compilerTypes.Type{}
	fact.variant = variant
	state.facts[id] = fact
}

// narrowedVariant returns the binding's active ADT variant, if any.
func (state *flowState) narrowedVariant(id BindingID) (*compilerTypes.AdtVariant, bool) {
	if state == nil {
		return nil, false
	}
	fact, ok := state.facts[id]
	if !ok || fact.escaped || fact.variant == nil {
		return nil, false
	}
	return fact.variant, true
}

// invalidateNarrowing drops a binding's narrowing without escaping it. This
// is the effect of assignment: the slot may now hold nil again.
func (state *flowState) invalidateNarrowing(id BindingID) {
	if fact, ok := state.facts[id]; ok {
		fact.typ = compilerTypes.Type{}
		state.facts[id] = fact
	}
}

// trackFreed starts cleanup tracking for a binding in the known-live state.
func (state *flowState) trackFreed(id BindingID) {
	if state == nil || id == 0 {
		return
	}
	if state.tracked == nil {
		state.tracked = make(map[BindingID]bool)
	}
	state.tracked[id] = true
	fact := state.facts[id]
	if fact.version == 0 {
		fact.version = state.nextFreedVersion(id, 0)
	}
	fact.freed = false
	state.facts[id] = fact
}

// dropFreed abandons cleanup tracking without treating the binding's address
// as escaped. Narrowing facts remain available to their existing consumers.
func (state *flowState) dropFreed(id BindingID) {
	if state == nil {
		return
	}
	delete(state.tracked, id)
	delete(state.released, id)
	if fact, ok := state.facts[id]; ok {
		fact.freed = false
		state.facts[id] = fact
	}
}

// freed reports only a known released state. Missing tracking is deliberately
// indistinguishable from a live value to diagnostics.
func (state *flowState) freed(id BindingID) bool {
	if state == nil || !state.tracked[id] {
		return false
	}
	fact, ok := state.facts[id]
	return ok && fact.freed
}

func (state *flowState) trackedVersion(id BindingID) (uint64, bool) {
	if state == nil || !state.tracked[id] {
		return 0, false
	}
	fact, ok := state.facts[id]
	return fact.version, ok && fact.version != 0
}

func (state *flowState) freedAt(id BindingID, version uint64) bool {
	if state == nil || !state.tracked[id] || version == 0 {
		return false
	}
	fact, ok := state.facts[id]
	if ok && fact.version == version && fact.freed {
		return true
	}
	return state.released[id][version]
}

func (state *flowState) markFreed(id BindingID) {
	if state == nil || !state.tracked[id] {
		return
	}
	fact, ok := state.facts[id]
	if !ok {
		return
	}
	fact.freed = true
	state.facts[id] = fact
	if fact.version != 0 {
		state.markFreedVersion(id, fact.version)
	}
}

func (state *flowState) markFreedVersion(id BindingID, version uint64) {
	if state == nil || !state.tracked[id] || version == 0 {
		return
	}
	if fact, ok := state.facts[id]; !ok {
		return
	} else if fact.version == version {
		fact.freed = true
		state.facts[id] = fact
	}
	if state.released == nil {
		state.released = make(map[BindingID]map[uint64]bool)
	}
	if state.released[id] == nil {
		state.released[id] = make(map[uint64]bool)
	}
	state.released[id][version] = true
}

func (state *flowState) clearFreed(id BindingID) {
	if state == nil || !state.tracked[id] {
		return
	}
	fact := state.facts[id]
	fact.version = state.nextFreedVersion(id, fact.version)
	fact.freed = false
	state.facts[id] = fact
}

func (state *flowState) nextFreedVersion(id BindingID, current uint64) uint64 {
	next := current + 1
	if next == 0 {
		next = 1
	}
	for version := range state.released[id] {
		if version < next {
			continue
		}
		next = version + 1
		if next == 0 {
			next = 1
		}
	}
	return next
}

// escape records that a writable address of the binding escaped. It clears
// narrowing and cleanup tracking because the slot can now change unseen.
func (state *flowState) escape(id BindingID) {
	if state == nil {
		return
	}
	state.dropFreed(id)
	state.facts[id] = flowFact{escaped: true}
}

// mergeBranch merges one branch's invalidation effects. New control-flow code
// uses mergeBranches so freed facts include every continuing path.
func (state *flowState) mergeBranch(branch *flowState) {
	state.mergeBranches(branch)
}

// mergeBranches merges invalidation effects from all continuing branches.
// Narrowing remains invalidated conservatively; unlike narrowing, freed is
// retained only when every branch still tracks the binding and has freed it.
func (state *flowState) mergeBranches(branches ...*flowState) {
	if state == nil || len(branches) == 0 {
		return
	}
	parent := state.clone()
	for _, branch := range branches {
		if branch == nil {
			continue
		}
		for id, fact := range branch.facts {
			if fact.escaped {
				state.escape(id)
				continue
			}
			parentFact, exists := parent.facts[id]
			if !exists {
				continue
			}
			if !compilerTypes.Equal(parentFact.typ, fact.typ) {
				current := state.facts[id]
				current.typ = compilerTypes.Type{}
				state.facts[id] = current
			}
		}
	}
	for id := range parent.tracked {
		allTracked := true
		allFreed := true
		commonVersion := uint64(0)
		sameVersion := true
		firstBranch := true
		for _, branch := range branches {
			if branch == nil || !branch.tracked[id] {
				allTracked = false
				break
			}
			fact, ok := branch.facts[id]
			if !ok || fact.escaped {
				allTracked = false
				break
			}
			if firstBranch {
				commonVersion = fact.version
				firstBranch = false
			} else if fact.version != commonVersion {
				sameVersion = false
			}
			if !branch.freed(id) {
				allFreed = false
			}
		}
		if !allTracked {
			state.dropFreed(id)
			continue
		}
		fact := state.facts[id]
		if sameVersion && commonVersion != 0 {
			fact.version = commonVersion
		} else {
			// A divergent current version cannot be assigned a historical
			// identity, but it may still be definitely freed on every path.
			fact.version = 0
		}
		fact.freed = allFreed
		state.facts[id] = fact
	}

	// Deferred actions may retain an older binding version after every branch
	// reassigns the slot. Intersect the versions each continuing branch proves
	// released; disagreement is an unknown state and stays accepted.
	versions := make(map[BindingID]map[uint64]bool)
	addVersions := func(branch *flowState) {
		if branch == nil {
			return
		}
		for id, released := range branch.released {
			if versions[id] == nil {
				versions[id] = make(map[uint64]bool)
			}
			for version := range released {
				versions[id][version] = true
			}
		}
		for id, fact := range branch.facts {
			if branch.tracked[id] && fact.freed && fact.version != 0 {
				if versions[id] == nil {
					versions[id] = make(map[uint64]bool)
				}
				versions[id][fact.version] = true
			}
		}
	}
	addVersions(parent)
	for _, branch := range branches {
		addVersions(branch)
	}
	mergedReleased := make(map[BindingID]map[uint64]bool)
	for id, candidates := range versions {
		if !state.tracked[id] {
			continue
		}
		for version := range candidates {
			allReleased := true
			for _, branch := range branches {
				if branch == nil || !branch.tracked[id] || !branch.freedAt(id, version) {
					allReleased = false
					break
				}
			}
			if allReleased {
				if mergedReleased[id] == nil {
					mergedReleased[id] = make(map[uint64]bool)
				}
				mergedReleased[id][version] = true
			}
		}
	}
	state.released = mergedReleased
}

// adopt replaces the continuing state with one continuing branch's facts
// wholesale. It is used only when every other branch terminates, so the
// adopted branch's narrowings and cleanup facts are the only continuation.
func (state *flowState) adopt(branch *flowState) {
	if state == nil || branch == nil {
		return
	}
	state.facts = make(map[BindingID]flowFact, len(branch.facts))
	for id, fact := range branch.facts {
		state.facts[id] = fact
	}
	state.tracked = make(map[BindingID]bool, len(branch.tracked))
	for id := range branch.tracked {
		state.tracked[id] = true
	}
	state.released = make(map[BindingID]map[uint64]bool, len(branch.released))
	for id, versions := range branch.released {
		state.released[id] = make(map[uint64]bool, len(versions))
		for version := range versions {
			state.released[id][version] = true
		}
	}
}

// selfPlace resolves the implicit receiver. `self` is a keyword, so it can
// never be declared or shadowed; it exists exactly when a scope carries an
// impl target. The place is never writable: rule 3 makes the binding fixed,
// while a write through it -- self.x on a MutPtr target -- gets its
// writability from the pointee, not from this binding.
func selfPlace(names *scope, token lexer.Token) checkedExpression {
	if names.self == nil {
		return checkedExpression{token: token, diagnostic: selfNotBoundDiagnostic(token)}
	}
	return checkedExpression{
		source: Operand{
			Kind:        VariableOperand,
			Type:        *names.self,
			Name:        "self",
			Binding:     names.selfID,
			Node:        variableNodeWithBinding("self", names.selfID),
			Addressable: true,
		},
		typ:   *names.self,
		use:   compilerTypes.NewTypeUse(*names.self),
		token: token,
		self:  true,
	}
}

func (names *scope) inFunction() bool { return names.function }

func (names *scope) newBindingID() BindingID {
	if names.nextID == nil {
		return 0
	}
	*names.nextID = *names.nextID + 1
	return *names.nextID
}

// lookupStatus distinguishes a resolved name from the two failures that carry
// different diagnostics.
type lookupStatus uint8

const (
	nameFound lookupStatus = iota
	nameMissing
	nameModuleData
)

// lookup resolves a name for the current scope. Inside a function body a
// module-level data binding is deliberately unreachable; only previously
// declared functions remain visible. An import alias is never a value and is
// skipped entirely, so a bare alias reference fails as an unknown variable.
func (names *scope) lookup(name string) (binding, lookupStatus) {
	for current := names; current != nil && current.local != nil; current = current.parent {
		if bound, ok := current.local[name]; ok {
			return bound, nameFound
		}
	}
	if bound, ok := names.module[name]; ok && bound.kind != aliasBinding {
		if names.inFunction() && bound.kind != functionBinding && bound.kind != genericFunctionBinding {
			return bound, nameModuleData
		}
		return bound, nameFound
	}
	return binding{}, nameMissing
}

// importAlias reports whether name is an import alias of the enclosing
// module. The module frame is shared by reference through every child scope,
// so one lookup reaches it at any depth; shadowing an alias is forbidden.
func (names *scope) importAlias(name string) bool {
	bound, exists := names.module[name]
	return exists && bound.kind == aliasBinding
}

// importAliasTarget returns the canonical id of the module an import alias
// names, when that module's source is present in this compilation. The alias
// binding lives in the shared module frame and records its target; a dangling
// alias whose target has no source resolves nowhere and keeps failing as an
// unknown variable until the missing path is reported.
func (names *scope) importAliasTarget(name string) (string, bool) {
	bound, exists := names.module[name]
	if !exists || bound.kind != aliasBinding || bound.moduleID == "" {
		return "", false
	}
	if names.registry == nil {
		return "", false
	}
	_, present := names.registry.modules[bound.moduleID]
	return bound.moduleID, present
}

// declaredHere reports a duplicate in the innermost scope only, so a local may
// shadow a module-level value. An import alias is the exception: it is fixed
// for the whole module, so no nested scope may shadow one.
func (names *scope) declaredHere(name string) bool {
	if names.local != nil {
		if _, exists := names.local[name]; exists {
			return true
		}
		return names.importAlias(name)
	}
	_, exists := names.module[name]
	return exists
}

// define installs one binding in the current frame and reports whether it was
// accepted. At module level an import alias may not collide with any existing
// module binding; in a nested scope a name may not shadow an import alias.
// Other duplicates are validated before define is called, so they never reach
// a rejection here.
func (names *scope) define(name string, bound binding) bool {
	if names.local != nil {
		if names.importAlias(name) {
			return false
		}
		names.local[name] = bound
		return true
	}
	if bound.kind == aliasBinding {
		if _, exists := names.module[name]; exists {
			return false
		}
	}
	names.module[name] = bound
	return true
}

func (names *scope) child() *scope {
	return &scope{
		module:     names.module,
		local:      make(map[string]binding),
		parent:     names,
		owner:      names.owner,
		result:     names.result,
		methods:    names.methods,
		self:       names.self,
		selfID:     names.selfID,
		function:   names.function,
		nextID:     names.nextID,
		flow:       names.flow,
		generics:   names.generics,
		registry:   names.registry,
		moduleID:   names.moduleID,
		logicalKey: names.logicalKey,
	}
}

func (names *scope) activeDeferredActions() []DeferredAction {
	if names == nil || len(names.defers) == 0 {
		return nil
	}
	return append([]DeferredAction(nil), names.defers...)
}

func (names *scope) recordReturnFlow() {
	if names == nil || names.flow == nil {
		return
	}
	names.returnFlows = append(names.returnFlows, returnFlow{
		state:   names.flow.clone(),
		actions: names.activeDeferredActions(),
	})
}

func (names *scope) recordChildReturnFlows(flows []returnFlow) {
	if names == nil || len(flows) == 0 {
		return
	}
	active := names.activeDeferredActions()
	for _, flow := range flows {
		names.returnFlows = append(names.returnFlows, returnFlow{
			state:   flow.state,
			actions: append([]DeferredAction(nil), active...),
		})
	}
}

// typeErrorAt is the checker's single Type Error constructor: every site
// reports through it rather than expanding a composite literal.
func typeErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.TypeError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func moduleDataDiagnostic(owner, name string, token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, fmt.Sprintf("function %s cannot access module data binding %s; pass it as a parameter", owner, name))
}

func selfNotBoundDiagnostic(token lexer.Token) *compilerTypes.Diagnostic {
	diagnostic := typeErrorAt(token, "self is not bound outside an impl body")
	return &diagnostic
}

// diagnosticAt returns an addressable copy of one constructed diagnostic. The
// checker reports a diagnostic by pointer in the many places where absence is
// meaningful, and a constructor's result is not addressable; this adapter
// keeps every construction going through one of the *At builders instead of
// re-expanding a composite literal at each pointer site.
func diagnosticAt(diagnostic compilerTypes.Diagnostic) *compilerTypes.Diagnostic {
	return &diagnostic
}

// nameErrorAt, moduleErrorAt, and unknownAt are typeErrorAt's siblings for the
// checker's other categories. Every diagnostic the checker reports is built by
// one of these four, so a category is never spelled at a call site.
func nameErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.NameError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func moduleErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.ModuleError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func unknownAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.UnknownError,
		Stage:    "checker",
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}
