package graph

import (
	"reflect"
	"testing"
)

// walk drives a Walker over an adjacency map the way a resolver does:
// descending into every neighbour that is not a back-edge and closing each
// node on the way out. It returns the completed walk, whose Order is the
// reachable set in dependency-first post-order.
func walk(edges map[string][]string, root string) *Walker[string] {
	w := NewWalker[string]()
	var descend func(node string)
	descend = func(node string) {
		if !w.Enter(node) {
			return
		}
		for _, next := range edges[node] {
			if _, cycle := w.Cycle(next); cycle {
				continue
			}
			descend(next)
		}
		w.Leave()
	}
	descend(root)
	return w
}

// Only nodes reachable from the root enter the post-order; a node that can
// reach the root but is not itself reached stays out.
func TestWalkReachesOnlyConnectedNodes(t *testing.T) {
	edges := map[string][]string{
		"app":       {"math", "util"},
		"math":      {"constants"},
		"util":      {"constants"},
		"constants": {},
		"orphan":    {"app"},
	}
	w := walk(edges, "app")
	got := make(map[string]bool, len(w.Order()))
	for _, node := range w.Order() {
		got[node] = true
	}
	for _, want := range []string{"app", "math", "util", "constants"} {
		if !got[want] {
			t.Fatalf("reachable node %q missing from post-order %v", want, w.Order())
		}
	}
	if got["orphan"] {
		t.Fatalf("unreached node %q present in post-order %v", "orphan", w.Order())
	}
}

// Post-order is dependency-first and preserves the caller's neighbour order:
// a shared dependency is emitted once, before every node that imports it, and
// the root is last.
func TestPostOrderIsDependencyFirst(t *testing.T) {
	edges := map[string][]string{
		"app":       {"math", "util"},
		"math":      {"constants"},
		"util":      {"constants"},
		"constants": {},
	}
	want := []string{"constants", "math", "util", "app"}
	if got := walk(edges, "app").Order(); !reflect.DeepEqual(got, want) {
		t.Fatalf("post-order = %v, want %v", got, want)
	}
}

// Cycle reports the back-edge as the active path from the target's first
// occurrence through the current node, with the target repeated, and reports
// no cycle for a node off the active path.
func TestCycleReportsActivePath(t *testing.T) {
	w := NewWalker[string]()
	for _, node := range []string{"app", "math", "constants"} {
		if !w.Enter(node) {
			t.Fatalf("Enter(%q) = false while descending a fresh path", node)
		}
	}
	cycle, ok := w.Cycle("app")
	if !ok {
		t.Fatal("Cycle(app) = false on the active path")
	}
	if want := []string{"app", "math", "constants", "app"}; !reflect.DeepEqual(cycle, want) {
		t.Fatalf("cycle = %v, want %v", cycle, want)
	}
	if cycle, ok := w.Cycle("math"); !ok || !reflect.DeepEqual(cycle, []string{"math", "constants", "math"}) {
		t.Fatalf("Cycle(math) = %v, %v; want [math constants math], true", cycle, ok)
	}
	if _, ok := w.Cycle("absent"); ok {
		t.Fatal("Cycle reported a node that is not on the active path")
	}
	// The returned slice is the caller's: rewriting it must not corrupt the
	// walker's active path.
	cycle[0] = "mutated"
	if again, _ := w.Cycle("app"); !reflect.DeepEqual(again, []string{"app", "math", "constants", "app"}) {
		t.Fatalf("walk's active path changed after mutating a returned cycle: %v", again)
	}
}
