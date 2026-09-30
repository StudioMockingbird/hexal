package checker

import (
	"maps"
	"slices"
	"strings"

	"hexal/compiler/span"
)

// State of the stale-view analysis. A storage root is one allocated List or
// String backing store; a view is a pointer, Slice, or cursor whose bytes lie
// inside a root. The analysis tracks, per place, which roots a view may point
// into and whether a structural change has happened since, joining paths by
// union because one stale path is enough to reach released storage.

// viewRoot identifies one backing store by the place whose handle first named
// it: the binding plus a member or element path. Aliases share a root through
// the handle relation, never through a copy of this value.
type viewRoot struct {
	binding BindingID
	path    string
}

// viewRootIncoming is the path spelling of the view a parameter carries into a
// callee: it names no backing store the callee can see, so it is an abstract
// root that only summaries consume.
const viewRootIncoming = "#view"

// rootSet is a sorted, duplicate-free set of roots.
type rootSet []viewRoot

func compareRoot(a, b viewRoot) int {
	if a.binding != b.binding {
		if a.binding < b.binding {
			return -1
		}
		return 1
	}
	return strings.Compare(a.path, b.path)
}

func (set rootSet) union(other rootSet) rootSet {
	if len(other) == 0 {
		return set
	}
	if len(set) == 0 {
		return other
	}
	merged := make(rootSet, 0, len(set)+len(other))
	merged = append(merged, set...)
	merged = append(merged, other...)
	slices.SortFunc(merged, compareRoot)
	return slices.CompactFunc(merged, func(a, b viewRoot) bool { return a == b })
}

func (set rootSet) intersects(other rootSet) bool {
	for _, root := range set {
		if _, found := slices.BinarySearchFunc(other, root, compareRoot); found {
			return true
		}
	}
	return false
}

func (set rootSet) contains(root viewRoot) bool {
	_, found := slices.BinarySearchFunc(set, root, compareRoot)
	return found
}

func singleRoot(root viewRoot) rootSet { return rootSet{root} }

// viewPlace is a tracked storage place: a binding and a member or element
// path. The empty path is the binding itself; ".name" a member; "[]" any
// element of an inline List.
type viewPlace struct {
	binding BindingID
	path    string
}

func (place viewPlace) child(path string) viewPlace {
	return viewPlace{binding: place.binding, path: place.path + path}
}

// relativeTo reports place's path below prefix, when place lies at or below it.
func (place viewPlace) relativeTo(prefix viewPlace) (string, bool) {
	if place.binding != prefix.binding || !strings.HasPrefix(place.path, prefix.path) {
		return "", false
	}
	return place.path[len(prefix.path):], true
}

type viewKind uint8

const (
	viewPointer viewKind = iota
	viewSlice
	viewCursor
)

func (kind viewKind) noun() string {
	switch kind {
	case viewSlice:
		return "slice"
	case viewCursor:
		return "cursor"
	default:
		return "pointer"
	}
}

// viewFact is what one place may hold: a view into any of roots, stale when a
// structural change reached one of those roots on some path.
type viewFact struct {
	roots rootSet
	kind  viewKind
	stale bool
	// span is the earliest structural change that made the view stale.
	span span.Span
	// cause is the root whose change made the view stale, for the message.
	cause viewRoot
}

func earlierSpan(a, b span.Span) span.Span {
	if a == (span.Span{}) {
		return b
	}
	if b == (span.Span{}) {
		return a
	}
	if a.File != b.File {
		if a.File < b.File {
			return a
		}
		return b
	}
	if b.Start < a.Start {
		return b
	}
	return a
}

func joinFacts(a, b viewFact) viewFact {
	joined := viewFact{roots: a.roots.union(b.roots), kind: a.kind}
	joined.stale = a.stale || b.stale
	switch {
	case a.stale && b.stale:
		joined.span = earlierSpan(a.span, b.span)
		joined.cause = a.cause
		if joined.span == b.span {
			joined.cause = b.cause
		}
	case a.stale:
		joined.span, joined.cause = a.span, a.cause
	case b.stale:
		joined.span, joined.cause = b.span, b.cause
	}
	return joined
}

// viewValue is what an evaluated expression carries. facts maps a path
// relative to the value (empty for the value itself, ".name" for a member) to
// the view there; roots maps a path to the storage roots a handle there names.
type viewValue struct {
	facts map[string]viewFact
	roots map[string]rootSet
	// alias names the place an aggregate value was read from, so binding it
	// copies the source's handle relation instead of inventing new roots.
	alias *viewPlace
	// targets are the callables a function value may name; it is the zero
	// value for every other value.
	targets funcTargets
}

// funcTargets is the set of callables a function-valued place may hold. An
// unknown target, such as a Fun parameter, makes a call through it fail
// closed; otherwise a call merges every known target's summary.
type funcTargets struct {
	list    []*viewSummary
	unknown bool
}

func (targets funcTargets) empty() bool { return len(targets.list) == 0 && !targets.unknown }

func (targets funcTargets) union(other funcTargets) funcTargets {
	merged := funcTargets{unknown: targets.unknown || other.unknown}
	merged.list = append(merged.list, targets.list...)
	for _, candidate := range other.list {
		if !slices.Contains(merged.list, candidate) {
			merged.list = append(merged.list, candidate)
		}
	}
	return merged
}

func (value viewValue) empty() bool {
	return len(value.facts) == 0 && len(value.roots) == 0 && value.alias == nil
}

// whole returns the view held at the value itself, joined with every member
// view: a use of the whole value reads all of them.
func (value viewValue) whole() (viewFact, bool) {
	var joined viewFact
	found := false
	for _, fact := range value.facts {
		if !found {
			joined, found = fact, true
			continue
		}
		joined = joinFacts(joined, fact)
	}
	return joined, found
}

func singleFact(fact viewFact) viewValue {
	return viewValue{facts: map[string]viewFact{"": fact}}
}

func (value viewValue) handleRoots() rootSet { return value.roots[""] }

func mergeValues(values ...viewValue) viewValue {
	merged := viewValue{}
	for _, value := range values {
		for path, fact := range value.facts {
			if merged.facts == nil {
				merged.facts = make(map[string]viewFact)
			}
			if existing, ok := merged.facts[path]; ok {
				merged.facts[path] = joinFacts(existing, fact)
			} else {
				merged.facts[path] = fact
			}
		}
		for path, roots := range value.roots {
			if merged.roots == nil {
				merged.roots = make(map[string]rootSet)
			}
			merged.roots[path] = merged.roots[path].union(roots)
		}
	}
	return merged
}

// viewState is one program point's facts. A nil *viewState is an unreachable
// point, so joins skip it.
type viewState struct {
	facts   map[viewPlace]viewFact
	handles map[viewPlace]rootSet
	// aliases records aggregate places whose handle members are copies of
	// another place's: a root for dst.path resolves through src.path.
	aliases map[viewPlace][]viewPlace
	// funcs records the callables a function-valued place may name.
	funcs map[viewPlace]funcTargets
	// inflight are values computed earlier in the expression being
	// evaluated; a structural change made while evaluating a later sibling
	// makes them stale too.
	inflight []*viewValue
}

func newViewState() *viewState {
	return &viewState{
		facts:   make(map[viewPlace]viewFact),
		handles: make(map[viewPlace]rootSet),
		aliases: make(map[viewPlace][]viewPlace),
		funcs:   make(map[viewPlace]funcTargets),
	}
}

func (state *viewState) clone() *viewState {
	if state == nil {
		return nil
	}
	cloned := &viewState{
		facts:   maps.Clone(state.facts),
		handles: maps.Clone(state.handles),
		aliases: maps.Clone(state.aliases),
		funcs:   maps.Clone(state.funcs),
	}
	return cloned
}

// joinViewStates folds continuing paths into one state: views union with
// stale dominating, handle relations union, and a place missing on one path
// keeps the other path's fact.
func joinViewStates(states ...*viewState) *viewState {
	var joined *viewState
	for _, state := range states {
		if state == nil {
			continue
		}
		if joined == nil {
			joined = state.clone()
			continue
		}
		for place, fact := range state.facts {
			if existing, ok := joined.facts[place]; ok {
				joined.facts[place] = joinFacts(existing, fact)
			} else {
				joined.facts[place] = fact
			}
		}
		for place, roots := range state.handles {
			if existing, ok := joined.handles[place]; ok {
				joined.handles[place] = existing.union(roots)
			} else {
				joined.handles[place] = roots.union(singleRoot(viewRoot{binding: place.binding, path: place.path}))
			}
		}
		for place := range joined.handles {
			if _, ok := state.handles[place]; !ok {
				joined.handles[place] = joined.handles[place].union(singleRoot(viewRoot{binding: place.binding, path: place.path}))
			}
		}
		for place, sources := range state.aliases {
			joined.aliases[place] = unionPlaces(joined.aliases[place], sources)
		}
		for place, targets := range state.funcs {
			joined.funcs[place] = joined.funcs[place].union(targets)
		}
	}
	return joined
}

func unionPlaces(a, b []viewPlace) []viewPlace {
	merged := append(slices.Clone(a), b...)
	slices.SortFunc(merged, func(x, y viewPlace) int {
		if x.binding != y.binding {
			if x.binding < y.binding {
				return -1
			}
			return 1
		}
		return strings.Compare(x.path, y.path)
	})
	return slices.Compact(merged)
}

// sameViewState reports whether two states hold identical facts, the loop
// fixpoint test. Stale flags and roots only grow, so equality is reachable.
func sameViewState(a, b *viewState) bool {
	if a == nil || b == nil {
		return a == b
	}
	if len(a.facts) != len(b.facts) || len(a.handles) != len(b.handles) || len(a.aliases) != len(b.aliases) || len(a.funcs) != len(b.funcs) {
		return false
	}
	for place, targets := range a.funcs {
		other, ok := b.funcs[place]
		if !ok || targets.unknown != other.unknown || !slices.Equal(targets.list, other.list) {
			return false
		}
	}
	for place, fact := range a.facts {
		other, ok := b.facts[place]
		if !ok || fact.stale != other.stale || fact.kind != other.kind || !slices.Equal(fact.roots, other.roots) || fact.span != other.span {
			return false
		}
	}
	for place, roots := range a.handles {
		if other, ok := b.handles[place]; !ok || !slices.Equal(roots, other) {
			return false
		}
	}
	for place, sources := range a.aliases {
		if other, ok := b.aliases[place]; !ok || !slices.Equal(sources, other) {
			return false
		}
	}
	return true
}

// rootsOfPlace resolves the storage roots a handle place may name: an
// explicit relation, then the relation of an aliased aggregate above it, then
// the place itself.
func (state *viewState) rootsOfPlace(place viewPlace) rootSet {
	return state.resolveRoots(place, 0)
}

func (state *viewState) resolveRoots(place viewPlace, depth int) rootSet {
	if roots, ok := state.handles[place]; ok {
		return roots
	}
	if depth < 8 {
		var resolved rootSet
		found := false
		for prefix, sources := range state.aliases {
			relative, below := place.relativeTo(prefix)
			if !below {
				continue
			}
			for _, source := range sources {
				resolved = resolved.union(state.resolveRoots(source.child(relative), depth+1))
				found = true
			}
		}
		if found {
			return resolved
		}
	}
	return singleRoot(viewRoot{binding: place.binding, path: place.path})
}

// clearPlace forgets every fact, handle relation, and alias at or below place;
// binding a new value replaces the old one.
func (state *viewState) clearPlace(place viewPlace) {
	for key := range state.facts {
		if _, below := key.relativeTo(place); below {
			delete(state.facts, key)
		}
	}
	for key := range state.handles {
		if _, below := key.relativeTo(place); below {
			delete(state.handles, key)
		}
	}
	for key := range state.aliases {
		if _, below := key.relativeTo(place); below {
			delete(state.aliases, key)
		}
	}
	for key := range state.funcs {
		if _, below := key.relativeTo(place); below {
			delete(state.funcs, key)
		}
	}
}

// bindValue makes place hold value, replacing whatever it held. A weak update
// joins instead, for storage shared by every element path of an inline List.
func (state *viewState) bindValue(place viewPlace, value viewValue, weak bool) {
	if !weak {
		state.clearPlace(place)
	}
	for path, fact := range value.facts {
		target := place.child(path)
		if weak {
			if existing, ok := state.facts[target]; ok {
				fact = joinFacts(existing, fact)
			}
		}
		state.facts[target] = fact
	}
	for path, roots := range value.roots {
		target := place.child(path)
		if weak {
			roots = state.rootsOfPlace(target).union(roots)
		}
		state.handles[target] = roots
	}
	if value.alias != nil {
		state.aliases[place] = unionPlaces(state.aliases[place], []viewPlace{*value.alias})
	}
	if !value.targets.empty() {
		targets := value.targets
		if weak {
			targets = state.funcs[place].union(targets)
		}
		state.funcs[place] = targets
	}
}

// readPlace returns the value a place holds: every fact at or below it, and
// every fact at a prefix of it (a view stored on an aggregate covers its
// members), plus the aggregate's alias so a copy keeps the handle relation.
func (state *viewState) readPlace(place viewPlace) viewValue {
	value := viewValue{}
	for key, fact := range state.facts {
		if relative, below := key.relativeTo(place); below {
			if value.facts == nil {
				value.facts = make(map[string]viewFact)
			}
			value.facts[relative] = joinIfPresent(value.facts, relative, fact)
			continue
		}
		if _, above := place.relativeTo(key); above {
			if value.facts == nil {
				value.facts = make(map[string]viewFact)
			}
			value.facts[""] = joinIfPresent(value.facts, "", fact)
		}
	}
	roots := state.rootsOfPlace(place)
	if len(roots) > 0 {
		value.roots = map[string]rootSet{"": roots}
	}
	alias := place
	value.alias = &alias
	value.targets = state.funcs[place]
	return value
}

func joinIfPresent(facts map[string]viewFact, path string, fact viewFact) viewFact {
	if existing, ok := facts[path]; ok {
		return joinFacts(existing, fact)
	}
	return fact
}

// staleFact returns the joined stale view, if any, that a place holds or
// covers.
func (state *viewState) staleFact(place viewPlace) (viewFact, bool) {
	var stale viewFact
	found := false
	for key, fact := range state.facts {
		if !fact.stale {
			continue
		}
		_, below := key.relativeTo(place)
		_, above := place.relativeTo(key)
		if !below && !above {
			continue
		}
		if !found {
			stale, found = fact, true
			continue
		}
		stale = joinFacts(stale, fact)
	}
	return stale, found
}

// invalidate marks stale every view into any of roots, in the state and in
// the in-flight values, recording the earliest change site.
func (state *viewState) invalidate(roots rootSet, at span.Span) {
	if len(roots) == 0 {
		return
	}
	mark := func(fact viewFact) viewFact {
		for _, root := range fact.roots {
			if !roots.contains(root) {
				continue
			}
			if !fact.stale || earlierSpan(at, fact.span) == at {
				fact.cause = root
			}
			fact.span = earlierSpan(at, fact.span)
			fact.stale = true
			return fact
		}
		return fact
	}
	for place, fact := range state.facts {
		state.facts[place] = mark(fact)
	}
	for _, value := range state.inflight {
		for path, fact := range value.facts {
			value.facts[path] = mark(fact)
		}
	}
}
