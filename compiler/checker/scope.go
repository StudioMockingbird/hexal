package checker

import (
	"fmt"
	"maps"

	"hexal/compiler/corelib"
	"hexal/compiler/lexer"
	"hexal/compiler/span"
	compilerTypes "hexal/compiler/types"
)

// tokenAt builds the diagnostic token for a span. A nil table, which only a
// lone module checked outside Compile has, resolves to the zero position and
// so renders as no location rather than inventing one.
func tokenAt(table *span.Table, s span.Span) lexer.Token {
	position := span.Position{}
	if table != nil {
		position = table.Position(s)
	}
	return lexer.Token{Span: s, Line: position.Line, Column: position.Column}
}

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
	// moduleValueBinding is an imported-module constant: program-lifetime module
	// storage visible throughout its defining module independent of textual
	// position, unlike an ordinary dataBinding root value (main-local,
	// unreachable from a function body).
	moduleValueBinding
	// foreignFunctionBinding, foreignConstantBinding, and foreignGlobalBinding
	// name handwritten foreign declarations. They are module-owned like a
	// function or module value, but lower to the exact C symbol the binding
	// records instead of a source-derived spelling.
	foreignFunctionBinding
	foreignConstantBinding
	foreignGlobalBinding
)

// scope is one lexical name frame. Module bindings remain in module and are
// intentionally hidden from function bodies; control-flow frames chain to
// their enclosing frame so branch declarations do not escape their block.
type scope struct {
	module     map[string]binding
	local      map[string]binding // nil only at module level
	parent     *scope
	moduleID   string // the enclosing module's canonical identity
	logicalKey string // the enclosing module's source-map filename
	// table resolves a carried span to the line and column a diagnostic
	// renders. It is the compilation's one table, shared by reference with
	// every child scope, and nil for a lone module checked outside Compile.
	table        *span.Table
	owner        string // enclosing function or method name, for diagnostics
	result       *compilerTypes.Type
	resultUse    *compilerTypes.TypeUse
	methods      *methodTable
	self         *compilerTypes.Type // the method receiver struct; nil outside a method body
	selfID       BindingID
	function     bool
	nextID       *BindingID
	flow         *flowState // branch-local narrowing facts
	generics     *genericTable
	defers       []DeferredAction
	returnFlows  []returnFlow // states and active actions reaching a return
	cleanupDepth int          // checking a defer or errdefer action
	// unsafeDepth counts the lexical unsafe regions enclosing this frame. It
	// is inherited by every nested block frame and deliberately not by a
	// function-body frame: the permission is lexical and never travels
	// through a call.
	unsafeDepth int
	// registry is the compilation's module graph: it resolves import aliases
	// against the target modules' exported records.
	// It is shared by reference with every child scope.
	registry *ModuleRegistry
	// closureRoot marks the body scope of a local named function or
	// anonymous function literal. lookup treats every data or
	// parameter binding beyond this scope's own local map as an invalid
	// capture, while functions declared at or beyond it remain visible: a
	// non-capturing function may call any function visible at its source
	// position, but may not read an enclosing function's data.
	closureRoot bool
	// capture records the entry-root bindings this named entry function or
	// method captures. It is shared by reference with every nested block
	// frame, and nil for an anonymous or local function literal, which never
	// captures.
	capture *captureState
	// envDependent names the entry module's environment-dependent functions
	// and methods; envCaptures maps each to its captured root binding names;
	// initializedRoots tracks the root bindings already initialized at the
	// current point in the root script. All three are shared by reference.
	envDependent     map[string]bool
	envCaptures      map[string]map[string]bool
	initializedRoots map[string]bool
	// rootIndex is this body's source item index, for source-order capture
	// visibility against a root binding's own index.
	rootIndex int
}

// captureState records the root bindings one entry-module named function or
// method captures, in first-use order. allowed is false for a scope that may
// not capture at all.
type captureState struct {
	allowed  bool
	bindings map[string]binding
	order    []string
}

func (state *captureState) record(name string, bound binding) {
	if state == nil || !state.allowed {
		return
	}
	if _, seen := state.bindings[name]; !seen {
		state.order = append(state.order, name)
	}
	state.bindings[name] = bound
}

// capturesOf returns one scope's captures in first-use order.
func capturesOf(state *captureState) []Capture {
	if state == nil {
		return nil
	}
	var captures []Capture
	for _, name := range state.order {
		bound := state.bindings[name]
		captures = append(captures, Capture{Name: name, Binding: bound.id, Type: bound.typ, Mutable: bound.mutable})
	}
	return captures
}

// markEntryCaptures records the entry module's captured root bindings in
// source order and flags their checked declarations, so the generator lowers
// them as entry-environment fields.
func markEntryCaptures(checked *Program) {
	captured := make(map[BindingID]bool)
	byBinding := make(map[BindingID]Capture)
	for _, statement := range checked.Statements {
		switch declaration := statement.(type) {
		case FunctionDeclaration:
			for _, capture := range declaration.Captures {
				captured[capture.Binding] = true
				byBinding[capture.Binding] = capture
			}
		case MethodDeclaration:
			for _, capture := range declaration.Captures {
				captured[capture.Binding] = true
				byBinding[capture.Binding] = capture
			}
		}
	}
	for _, declaration := range checked.SpecializedFunctions {
		for _, capture := range declaration.Captures {
			captured[capture.Binding] = true
			byBinding[capture.Binding] = capture
		}
	}
	for _, declaration := range checked.SpecializedMethods {
		for _, capture := range declaration.Captures {
			captured[capture.Binding] = true
			byBinding[capture.Binding] = capture
		}
	}
	for index, statement := range checked.Statements {
		declaration, ok := statement.(Declaration)
		if !ok || !captured[declaration.Binding] {
			continue
		}
		declaration.Captured = true
		checked.Statements[index] = declaration
		checked.EntryCaptures = append(checked.EntryCaptures, byBinding[declaration.Binding])
	}
}

// moduleScope builds the root frame of one module. Import aliases are read
// from the registry through importTarget; the scope keeps no copy of its own.
func moduleScope(moduleID string, logicalKey string, registry *ModuleRegistry, table *span.Table) *scope {
	next := BindingID(0)
	generics := newGenericTable()
	if registry != nil {
		// Qualified type references resolve through the module's import
		// graph, which the generic table carries for the type resolver.
		generics.registry = registry
		generics.moduleID = moduleID
	}
	return &scope{module: make(map[string]binding), moduleID: moduleID, logicalKey: logicalKey, table: table, methods: newMethodTable(), nextID: &next, flow: newFlowState(), generics: generics, registry: registry}
}

// allocationID is one tracked allocation's opaque identity. Zero means none.
// Every tracked binding maps to exactly one live identity; aliases share it,
// reassignment mints a fresh one, and freedAlloc records which identities are
// released on every path to this point.
type allocationID uint64

// allocatorKind records which allocator produced an allocation, keyed by
// allocationID. Zero means unknown and never rejects a release.
type allocatorKind uint8

const (
	unknownAllocator allocatorKind = iota
	heapAllocator
	stashAllocator
	poolAllocator
)

// flowFact records the branch-local treatment of one binding. Narrowing facts
// survive only while the binding remains trackable; escape clears them because
// a write through the escaped address can change the slot. Cleanup state is
// not stored here: it lives in flowState's allocation-identity maps so every
// alias of one allocation observes the same freed decision.
type flowFact struct {
	typ        compilerTypes.Type // effective read type; zero Type when not narrowed
	escaped    bool
	variant    *compilerTypes.AdtVariant // active ADT variant when variant-narrowed
	capability uint8                     // compilerTypes.StreamCapability of an IO binding; zero is unknown
}

// returnFlow carries one reachable return state and the actions registered
// before that return in the scope being validated.
type returnFlow struct {
	state   *flowState
	actions []DeferredAction
}

// flowState is the branch-local fact table for one function body or module
// scope. tracked distinguishes a known cleanup state from an intentionally
// unknown state after a copy or escape. allocation maps each tracked binding
// to the allocation it currently denotes; freedAlloc is monotone per path and
// retains history for identities no binding still names, so deferred captures
// survive later rebinding. nextAlloc is a shared mint counter: every clone and
// adopt of one function's state shares the pointer so branch-local mints stay
// globally unique within the function.
// provenance records which List binding each Bytes stream borrows, and
// releasedSources marks lists the local facts prove already freed.
// stringOrigins records possible String storage origins per binding and
// stringPlaces per member or element place; both union at control-flow
// joins, and reads without a record fall through to the runtime check.
type flowState struct {
	facts           map[BindingID]flowFact
	tracked         map[BindingID]bool
	allocation      map[BindingID]allocationID
	freedAlloc      map[allocationID]bool
	allocatorKind   map[allocationID]allocatorKind
	nextAlloc       *allocationID
	provenance      map[BindingID]BindingID
	releasedSources map[BindingID]bool
	stringOrigins   map[BindingID]stringOriginSet
	stringPlaces    map[stringPlaceKey]stringOriginSet
}

func newFlowState() *flowState {
	counter := allocationID(0)
	return &flowState{
		facts:           make(map[BindingID]flowFact),
		tracked:         make(map[BindingID]bool),
		allocation:      make(map[BindingID]allocationID),
		freedAlloc:      make(map[allocationID]bool),
		allocatorKind:   make(map[allocationID]allocatorKind),
		nextAlloc:       &counter,
		provenance:      make(map[BindingID]BindingID),
		releasedSources: make(map[BindingID]bool),
		stringOrigins:   make(map[BindingID]stringOriginSet),
		stringPlaces:    make(map[stringPlaceKey]stringOriginSet),
	}
}

func (state *flowState) clone() *flowState {
	cloned := &flowState{
		facts:           maps.Clone(state.facts),
		tracked:         maps.Clone(state.tracked),
		allocation:      maps.Clone(state.allocation),
		freedAlloc:      maps.Clone(state.freedAlloc),
		allocatorKind:   maps.Clone(state.allocatorKind),
		nextAlloc:       state.nextAlloc,
		provenance:      maps.Clone(state.provenance),
		releasedSources: maps.Clone(state.releasedSources),
		stringOrigins:   maps.Clone(state.stringOrigins),
		stringPlaces:    maps.Clone(state.stringPlaces),
	}
	return cloned
}

// withoutFreedChecks gives exit-time expression typing a flow slice that
// retains types but cannot observe or mutate cleanup facts at registration.
func (state *flowState) withoutFreedChecks() *flowState {
	cloned := state.clone()
	cloned.tracked = make(map[BindingID]bool)
	cloned.freedAlloc = make(map[allocationID]bool)
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

// setCollectionRoot records the shared List or Dict state for one binding.
// The binding is updated in its owning lexical frame so later reads and
// nested loop checks see the same copied-handle identity.
func (names *scope) setCollectionRoot(id BindingID, root BindingID) bool {
	for frame := names; frame != nil; frame = frame.parent {
		for name, bound := range frame.local {
			if bound.id == id {
				bound.collectionRoot = root
				frame.local[name] = bound
				return true
			}
		}
		for name, bound := range frame.module {
			if bound.id == id {
				bound.collectionRoot = root
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

// mintAllocationID returns the next fresh identity on the shared per-function
// counter. Clones and adopts share the counter pointer so identities minted in
// sibling branches never collide.
func (state *flowState) mintAllocationID() allocationID {
	if state.nextAlloc == nil {
		zero := allocationID(0)
		state.nextAlloc = &zero
	}
	*state.nextAlloc++
	return *state.nextAlloc
}

// trackFreed starts cleanup tracking for a binding in the known-live state.
// It is idempotent: a second call on an already-tracked binding keeps the
// existing identity, which is what double-seeding a declaration does. A zero
// fact entry is established so every tracked binding has one; the join merge
// requires it.
func (state *flowState) trackFreed(id BindingID) {
	if state == nil || id == 0 {
		return
	}
	if state.tracked == nil {
		state.tracked = make(map[BindingID]bool)
	}
	if state.tracked[id] {
		return
	}
	state.tracked[id] = true
	if state.allocation == nil {
		state.allocation = make(map[BindingID]allocationID)
	}
	state.allocation[id] = state.mintAllocationID()
	if _, ok := state.facts[id]; !ok {
		state.facts[id] = flowFact{}
	}
}

// dropFreed abandons cleanup tracking for one binding without treating its
// address as escaped. The identity mapping is removed but freedAlloc history
// is retained: a deferred capture or another alias may still key on it.
func (state *flowState) dropFreed(id BindingID) {
	if state == nil {
		return
	}
	delete(state.tracked, id)
	delete(state.allocation, id)
}

// freed reports only a known released state through the binding's current
// identity. Missing tracking is deliberately indistinguishable from a live
// value to diagnostics.
func (state *flowState) freed(id BindingID) bool {
	if state == nil || !state.tracked[id] {
		return false
	}
	alloc, ok := state.allocation[id]
	if !ok || alloc == 0 {
		return false
	}
	return state.freedAlloc[alloc]
}

// trackedAllocation returns the identity a currently tracked binding denotes,
// for deferred-capture sites that must outlive later rebinding of the slot.
func (state *flowState) trackedAllocation(id BindingID) (allocationID, bool) {
	if state == nil || !state.tracked[id] {
		return 0, false
	}
	alloc, ok := state.allocation[id]
	return alloc, ok && alloc != 0
}

// aliasFreed maps target onto source's current identity when source is
// tracked, forming a must-alias. When source is untracked the call is a no-op
// and target keeps whatever identity it already has (typically a fresh mint
// from trackFreed).
func (state *flowState) aliasFreed(source, target BindingID) {
	if state == nil || source == 0 || target == 0 || !state.tracked[source] {
		return
	}
	alloc, ok := state.allocation[source]
	if !ok || alloc == 0 {
		return
	}
	if state.tracked == nil {
		state.tracked = make(map[BindingID]bool)
	}
	if state.allocation == nil {
		state.allocation = make(map[BindingID]allocationID)
	}
	state.tracked[target] = true
	state.allocation[target] = alloc
}

// setAllocatorKind records which allocator produced the binding's current
// identity. Unknown is the default and is never written; a kind is a
// property of the allocation, so aliases that share the identity observe it.
func (state *flowState) setAllocatorKind(id BindingID, kind allocatorKind) {
	if state == nil || id == 0 || kind == unknownAllocator {
		return
	}
	alloc, ok := state.allocation[id]
	if !ok || alloc == 0 {
		return
	}
	if state.allocatorKind == nil {
		state.allocatorKind = make(map[allocationID]allocatorKind)
	}
	state.allocatorKind[alloc] = kind
}

// allocatorKindOf reports which allocator produced the binding's current
// identity. Unknown covers untracked bindings, missing identities, and
// never-classified allocations; every release site accepts unknown.
func (state *flowState) allocatorKindOf(id BindingID) allocatorKind {
	if state == nil || !state.tracked[id] {
		return unknownAllocator
	}
	alloc, ok := state.allocation[id]
	if !ok || alloc == 0 {
		return unknownAllocator
	}
	return state.allocatorKind[alloc]
}

// capabilityOf reports the IO capability the local facts prove. Zero means
// unknown and selects the runtime access-mask check.
func (state *flowState) capabilityOf(id BindingID) uint8 {
	if state == nil {
		return 0
	}
	return state.facts[id].capability
}

// setCapability overwrites one binding's proven IO capability. Assignment
// seeding calls this with the new initializer's fact, so a rebinding replaces
// rather than intersects.
func (state *flowState) setCapability(id BindingID, capability uint8) {
	if state == nil || id == 0 {
		return
	}
	fact := state.facts[id]
	fact.capability = capability
	state.facts[id] = fact
}

// setProvenance records which List binding a Bytes stream borrows; zero
// clears the edge, selecting the unknown envelope.
func (state *flowState) setProvenance(id BindingID, source BindingID) {
	if state == nil || id == 0 {
		return
	}
	if source == 0 {
		delete(state.provenance, id)
		return
	}
	if state.provenance == nil {
		state.provenance = make(map[BindingID]BindingID)
	}
	state.provenance[id] = source
}

// releaseSource marks a List binding locally freed through list.free so its
// borrowers reject further use.
func (state *flowState) releaseSource(id BindingID) {
	if state == nil || id == 0 {
		return
	}
	if state.releasedSources == nil {
		state.releasedSources = make(map[BindingID]bool)
	}
	state.releasedSources[id] = true
}

// sourceReleased reports whether the local facts prove the list freed.
func (state *flowState) sourceReleased(id BindingID) bool {
	if state == nil {
		return false
	}
	return state.releasedSources[id]
}

// invalidateAllocationsFrom marks every directly tracked binding whose
// provenance names source as freed. Stash.reset()/destroy() call this
// eagerly at the reset/destroy call site: a later stash.allocate(...)
// produces a fresh binding with its own provenance edge, so it is untouched
// by an earlier reset's walk, while every allocation from before that reset
// is now correctly proven stale.
func (state *flowState) invalidateAllocationsFrom(source BindingID) {
	if state == nil || source == 0 {
		return
	}
	for derived, from := range state.provenance {
		if from == source {
			state.markFreed(derived)
		}
	}
}

// hasLiveTrackedAllocation reports whether any directly tracked binding
// whose provenance names source is still live (not proven freed). Pool
// destroy consults this to reject destroying a pool with a locally tracked
// live slot; an escaped or aliased allocation is untracked and therefore
// invisible here, matching the documented undecided-case policy.
func (state *flowState) hasLiveTrackedAllocation(source BindingID) bool {
	if state == nil || source == 0 {
		return false
	}
	for derived, from := range state.provenance {
		if from == source && state.tracked[derived] && !state.freed(derived) {
			return true
		}
	}
	return false
}

// markFreed records the binding's current identity as released on every path
// to this point. Every alias of that identity observes the mark.
func (state *flowState) markFreed(id BindingID) {
	if state == nil || !state.tracked[id] {
		return
	}
	alloc, ok := state.allocation[id]
	if !ok || alloc == 0 {
		return
	}
	if state.freedAlloc == nil {
		state.freedAlloc = make(map[allocationID]bool)
	}
	state.freedAlloc[alloc] = true
}

// clearFreed re-mints a fresh identity for a still-tracked binding, the
// effect of rebinding its slot to a new value. The old identity's freedAlloc
// history is retained for deferred captures that still name it.
func (state *flowState) clearFreed(id BindingID) {
	if state == nil || !state.tracked[id] {
		return
	}
	if state.allocation == nil {
		state.allocation = make(map[BindingID]allocationID)
	}
	state.allocation[id] = state.mintAllocationID()
}

// markFreedAlloc marks a captured identity released, used by the deferred
// validation path where the binding may since have been rebound or dropped.
func (state *flowState) markFreedAlloc(alloc allocationID) {
	if state == nil || alloc == 0 {
		return
	}
	if state.freedAlloc == nil {
		state.freedAlloc = make(map[allocationID]bool)
	}
	state.freedAlloc[alloc] = true
}

// freedAllocReport reports whether a captured identity is already proven
// released on every path to this point.
func (state *flowState) freedAllocReport(alloc allocationID) bool {
	if state == nil || alloc == 0 {
		return false
	}
	return state.freedAlloc[alloc]
}

// escape records that a writable address of the binding escaped. It clears
// narrowing and cleanup tracking because the slot can now change unseen.
// Stream facts ride the same wipe: capability and borrow provenance fall to
// the unknown envelope. String origins fall back to the runtime check the
// same way: the binding record is dropped and every member record rooted at
// it is removed, so later reads observe opaque instead of stale static.
func (state *flowState) escape(id BindingID) {
	if state == nil {
		return
	}
	state.dropFreed(id)
	state.facts[id] = flowFact{escaped: true}
	for borrowed, source := range state.provenance {
		if borrowed == id || source == id {
			delete(state.provenance, borrowed)
		}
	}
	delete(state.releasedSources, id)
	delete(state.stringOrigins, id)
	state.dropStringPlaces(id)
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
	// Identity agreement: a binding keeps its mapping only when every
	// continuing branch still tracks it and names the same identity;
	// disagreement drops the binding to untracked. A nil branch fails the
	// agreement.
	for id := range parent.tracked {
		allTracked := true
		commonAlloc := allocationID(0)
		sameAlloc := true
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
			alloc, ok := branch.allocation[id]
			if !ok || alloc == 0 {
				allTracked = false
				break
			}
			if firstBranch {
				commonAlloc = alloc
				firstBranch = false
			} else if alloc != commonAlloc {
				sameAlloc = false
			}
		}
		if !allTracked || !sameAlloc || commonAlloc == 0 {
			state.dropFreed(id)
			continue
		}
		if state.allocation == nil {
			state.allocation = make(map[BindingID]allocationID)
		}
		state.tracked[id] = true
		state.allocation[id] = commonAlloc
	}

	// freedAlloc is monotone history keyed by identity, not by binding.
	// Branches are clones of this parent and already carry its history, so
	// the merge is the intersection across branches alone: an identity stays
	// released only when every continuing path proves it. A nil branch fails
	// the intersection. Historical entries for dropped bindings survive,
	// which is what lets a deferred capture still validate after every branch
	// rebinds its slot.
	mergedFreed := make(map[allocationID]bool)
	firstSet := true
	for _, branch := range branches {
		if branch == nil {
			mergedFreed = make(map[allocationID]bool)
			break
		}
		if firstSet {
			for alloc, freed := range branch.freedAlloc {
				if freed {
					mergedFreed[alloc] = true
				}
			}
			firstSet = false
			continue
		}
		for alloc := range mergedFreed {
			if !branch.freedAlloc[alloc] {
				delete(mergedFreed, alloc)
			}
		}
	}
	if firstSet {
		mergedFreed = make(map[allocationID]bool)
	}
	state.freedAlloc = mergedFreed

	// Allocator kind is a property of the identity itself and is written once
	// when the identity is minted; every branch that still names an identity
	// agrees on its kind, so the parent's table needs no rewrite. Branch-local
	// kinds for identities that did not survive the join stay unreachable.

	// Stream facts merge conservatively in the same direction as freed: a
	// capability survives only when every continuing branch proves it, a
	// borrow edge only when every branch names the same source list, and a
	// released source only when every branch proves it.
	streamIDs := make(map[BindingID]bool)
	for _, branch := range branches {
		if branch == nil {
			continue
		}
		for id, fact := range branch.facts {
			if fact.capability != 0 {
				streamIDs[id] = true
			}
		}
		for id := range branch.provenance {
			streamIDs[id] = true
		}
	}
	for id := range streamIDs {
		capability := uint8(compilerTypes.StreamReadWrite)
		for _, branch := range branches {
			fact, ok := branch.facts[id]
			if branch == nil || !ok || fact.escaped {
				capability = 0
				break
			}
			capability &= fact.capability
		}
		current := state.facts[id]
		current.capability = capability
		state.facts[id] = current

		source := BindingID(0)
		sameSource := len(branches) > 0
		firstBranch := true
		for _, branch := range branches {
			borrowed, ok := branch.provenance[id]
			if branch == nil || !ok {
				sameSource = false
				break
			}
			if firstBranch {
				source = borrowed
				firstBranch = false
			} else if borrowed != source {
				sameSource = false
				break
			}
		}
		if sameSource && !firstBranch && !state.facts[id].escaped {
			state.provenance[id] = source
		} else {
			delete(state.provenance, id)
		}

		allReleased := true
		for _, branch := range branches {
			if branch == nil || !branch.releasedSources[id] {
				allReleased = false
				break
			}
		}
		if allReleased {
			state.releasedSources[id] = true
		} else {
			delete(state.releasedSources, id)
		}
	}

	// String storage origins union across continuing paths: a static
	// possibility on any path keeps the merged place rejectable, while an
	// escaped binding stays dropped and reads opaque. Keys the parent never
	// saw belong to branch-local bindings and stay out of the continuation.
	mergedOrigins := make(map[BindingID]stringOriginSet, len(parent.stringOrigins))
	for id, set := range parent.stringOrigins {
		if fact, ok := state.facts[id]; ok && fact.escaped {
			continue
		}
		merged := set
		for _, branch := range branches {
			if branch != nil {
				merged |= branch.stringOrigins[id]
			}
		}
		mergedOrigins[id] = merged
	}
	state.stringOrigins = mergedOrigins
	mergedPlaces := make(map[stringPlaceKey]stringOriginSet, len(parent.stringPlaces))
	for key, set := range parent.stringPlaces {
		if fact, ok := state.facts[key.root]; ok && fact.escaped {
			continue
		}
		merged := set
		for _, branch := range branches {
			if branch != nil {
				merged |= branch.stringPlaces[key]
			}
		}
		mergedPlaces[key] = merged
	}
	state.stringPlaces = mergedPlaces
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
	state.allocation = maps.Clone(branch.allocation)
	state.freedAlloc = maps.Clone(branch.freedAlloc)
	state.allocatorKind = maps.Clone(branch.allocatorKind)
	state.nextAlloc = branch.nextAlloc
	state.provenance = make(map[BindingID]BindingID, len(branch.provenance))
	for id, source := range branch.provenance {
		state.provenance[id] = source
	}
	state.releasedSources = make(map[BindingID]bool, len(branch.releasedSources))
	for id := range branch.releasedSources {
		state.releasedSources[id] = true
	}
	state.stringOrigins = make(map[BindingID]stringOriginSet, len(branch.stringOrigins))
	for id, set := range branch.stringOrigins {
		state.stringOrigins[id] = set
	}
	state.stringPlaces = make(map[stringPlaceKey]stringOriginSet, len(branch.stringPlaces))
	for key, set := range branch.stringPlaces {
		state.stringPlaces[key] = set
	}
}

// selfPlace resolves the implicit receiver. `self` is a keyword, so it can
// never be declared or shadowed; it exists exactly when a scope carries a
// method receiver. The place is never writable: the receiver is a fixed copy
// of the caller's struct, so neither assigning to self nor writing a member
// through it can reach caller storage.
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
//
// Crossing a closureRoot scope narrows this further: a local
// function or anonymous literal's own body may read its own parameters, its
// own self-recursion binding, and bindings it declares itself, but nothing
// data-shaped from an enclosing function. dataVisible starts true (this
// scope's own bindings are ordinary) and turns off for good the moment the
// walk steps out of a closureRoot scope into its parent, so every binding
// beyond that point counts only if it is itself a function. A rejected
// capture is reported as nameMissing, not a distinct status: a closed
// function has no environment, so an enclosing local is, from its
// perspective, simply not there - the same ordinary unknown-name diagnostic
// that a genuine typo gets.
func (names *scope) lookup(name string) (binding, lookupStatus) {
	dataVisible := true
	for current := names; current != nil && current.local != nil; current = current.parent {
		if bound, ok := current.local[name]; ok {
			if dataVisible || bound.kind == functionBinding || bound.kind == genericFunctionBinding {
				return bound, nameFound
			}
			return binding{}, nameMissing
		}
		if current.closureRoot {
			dataVisible = false
		}
	}
	if bound, ok := names.module[name]; ok && bound.kind != aliasBinding {
		if names.inFunction() && bound.kind == dataBinding && names.capture != nil && names.capture.allowed && bound.rootIndex < names.rootIndex {
			// An entry-module named function or method captures an earlier
			// root binding by reference.
			names.capture.record(name, bound)
			return bound, nameFound
		}
		if names.inFunction() && bound.kind != functionBinding && bound.kind != genericFunctionBinding &&
			bound.kind != moduleValueBinding && bound.kind != foreignFunctionBinding &&
			bound.kind != foreignConstantBinding && bound.kind != foreignGlobalBinding {
			return bound, nameModuleData
		}
		return bound, nameFound
	}
	return binding{}, nameMissing
}

// importAlias reports whether name is an import alias of the enclosing
// module. The module frame is shared by reference through every child scope,
// so one lookup reaches it at any depth; shadowing an alias is forbidden.
// isEntryModule reports whether this scope's module is the compilation's own
// entrypoint, the only module where a root return is valid. A nil registry
// means a single-module compilation outside the module-graph pipeline, where
// the one module being checked is trivially its own entrypoint.
func (names *scope) isEntryModule() bool {
	return names.registry == nil || names.moduleID == names.registry.entrypoint
}

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
	if corelib.IsModule(bound.moduleID) {
		// A core-library module publishes no registry entry -- it has no
		// Hexal source and no defining scope -- but its alias still resolves
		// to the compiler-owned declaration table.
		return bound.moduleID, true
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

// closureRootScope builds the body scope for a local named function or
// anonymous function literal. Unlike child, it does not inherit
// self, result, or resultUse - a nested function has neither the enclosing
// method's receiver nor its result type - and it starts a fresh flow state
// and defer list, exactly like a module function's own body: it is a
// distinct call frame, not a continuation of the enclosing one. Its parent
// is still the declaring scope, so lookup can find every function visible at
// this source position while closureRoot blocks everything else beyond this
// scope's own local map.
func (names *scope) closureRootScope(owner string, captureAllowed bool) *scope {
	body := &scope{
		module:           names.module,
		local:            make(map[string]binding),
		parent:           names,
		owner:            owner,
		methods:          names.methods,
		function:         true,
		nextID:           names.nextID,
		flow:             newFlowState(),
		generics:         names.generics,
		registry:         names.registry,
		moduleID:         names.moduleID,
		logicalKey:       names.logicalKey,
		table:            names.table,
		closureRoot:      true,
		envDependent:     names.envDependent,
		envCaptures:      names.envCaptures,
		initializedRoots: names.initializedRoots,
	}
	if captureAllowed {
		body.capture = &captureState{allowed: true, bindings: make(map[string]binding)}
	}
	return body
}

func (names *scope) child() *scope {
	return &scope{
		module:           names.module,
		local:            make(map[string]binding),
		parent:           names,
		owner:            names.owner,
		result:           names.result,
		resultUse:        names.resultUse,
		methods:          names.methods,
		self:             names.self,
		selfID:           names.selfID,
		function:         names.function,
		nextID:           names.nextID,
		flow:             names.flow,
		generics:         names.generics,
		registry:         names.registry,
		moduleID:         names.moduleID,
		logicalKey:       names.logicalKey,
		table:            names.table,
		unsafeDepth:      names.unsafeDepth,
		capture:          names.capture,
		envDependent:     names.envDependent,
		envCaptures:      names.envCaptures,
		initializedRoots: names.initializedRoots,
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
		Span:     token.Span,
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func moduleDataDiagnostic(owner, name string, token lexer.Token) compilerTypes.Diagnostic {
	return typeErrorAt(token, fmt.Sprintf("function %s cannot access module data binding %s; pass it as a parameter", owner, name))
}

func selfNotBoundDiagnostic(token lexer.Token) *compilerTypes.Diagnostic {
	diagnostic := typeErrorAt(token, "self is not bound outside a method body")
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

// diagnosticInDefiningModule stamps a diagnostic returned from specializing
// an imported generic template with the defining module's own logical key,
// before it propagates back up through the requesting module's own
// checkModule call: CheckModules's own InModule stamp only ever applies to
// an unstamped diagnostic, so a body or signature failure inside the
// template still names the module that declares it, never the importer
// that merely triggered the specialization. A nil diagnostic (success)
// passes through unchanged.
func diagnosticInDefiningModule(diagnostic *compilerTypes.Diagnostic, logicalKey string) *compilerTypes.Diagnostic {
	if diagnostic == nil {
		return nil
	}
	stamped := diagnostic.InModule(logicalKey)
	return &stamped
}

// nameErrorAt, moduleErrorAt, semanticErrorAt, and unknownAt are
// typeErrorAt's siblings for the checker's other categories. Every diagnostic
// the checker reports is built by one of these five, so a category is never
// spelled at a call site.
func nameErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.NameError,
		Stage:    "checker",
		Span:     token.Span,
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func moduleErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.ModuleError,
		Stage:    "checker",
		Span:     token.Span,
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

// semanticErrorAt reports a whole-program semantic contract failure, such as
// scheduler starvation, that is neither a type nor a name error.
func semanticErrorAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.SemanticError,
		Stage:    "checker",
		Span:     token.Span,
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}

func unknownAt(token lexer.Token, message string) compilerTypes.Diagnostic {
	return compilerTypes.Diagnostic{
		Category: compilerTypes.UnknownError,
		Stage:    "checker",
		Span:     token.Span,
		Line:     token.Line,
		Column:   token.Column,
		Message:  message,
	}
}
