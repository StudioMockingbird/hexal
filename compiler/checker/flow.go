package checker

import (
	"maps"

	compilerTypes "hexal/compiler/types"
)

// Branch-local flow facts for one function body or module scope: narrowing,
// allocation identity and cleanup, stream provenance, string origins, and the
// control-flow joins that fold branch slices into a continuation.

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
