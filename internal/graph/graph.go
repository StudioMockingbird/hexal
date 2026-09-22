// Package graph owns the graph mechanics a module resolver needs: a
// single-pass depth-first walk with a visited set, an active path for cycle
// detection, and dependency-first post-order accumulation. It is deliberately
// syntax-free. A node is any comparable identity the caller chooses, and
// neighbour order is whatever the caller supplies, so traversal order is the
// caller's contract and this package never sorts. It performs no file,
// process, or parsing work.
package graph

import "slices"

// Walker is one depth-first traversal. The caller drives it: Enter marks a
// node and reports whether it is new, Leave closes the node most recently
// entered and appends it to the post-order, and Cycle tests a neighbour
// against the active path. Enters and Leaves nest like call frames, so the
// active path is exactly the chain of open nodes from the walk's start to the
// current one.
type Walker[N comparable] struct {
	visited map[N]bool
	active  []N
	order   []N
}

// NewWalker returns a Walker with an empty visited set, active path, and
// post-order. N is comparable because visited membership and active-path
// search key on identity alone.
func NewWalker[N comparable]() *Walker[N] {
	return &Walker[N]{visited: make(map[N]bool)}
}

// Enter begins a visit to node. It reports false when node has already been
// entered, which tells the caller not to descend into it again; otherwise it
// records node as visited, pushes it onto the active path, and reports true.
// Every true result must be paired with one Leave, in reverse order, unless
// the caller abandons the walk without using its result (for example on an
// error). A Walker is single-use, so abandoned state is never observed.
func (w *Walker[N]) Enter(node N) bool {
	if w.visited[node] {
		return false
	}
	w.visited[node] = true
	w.active = append(w.active, node)
	return true
}

// Leave closes the node most recently opened by Enter, appending it to the
// dependency-first post-order. It must follow that Enter and precede the
// enclosing node's Leave.
func (w *Walker[N]) Leave() {
	last := len(w.active) - 1
	node := w.active[last]
	w.active = w.active[:last]
	w.order = append(w.order, node)
}

// Cycle reports whether node is on the active path. When it is, the returned
// slice is the cycle node closes: the active path from node's first
// occurrence through the current node, followed by node again. A caller
// rejects a back-edge before descending by testing its target here. The slice
// is freshly allocated, so the caller may retain it without aliasing the
// walk's active path.
func (w *Walker[N]) Cycle(node N) ([]N, bool) {
	at := slices.Index(w.active, node)
	if at < 0 {
		return nil, false
	}
	cycle := make([]N, 0, len(w.active)-at+1)
	cycle = append(cycle, w.active[at:]...)
	cycle = append(cycle, node)
	return cycle, true
}

// Order returns the dependency-first post-order accumulated so far: every node
// entered below node appears before node. It is the walk's own storage, read
// once the walk is complete; a caller must not mutate it.
func (w *Walker[N]) Order() []N {
	return w.order
}
