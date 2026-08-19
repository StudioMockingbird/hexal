//go:build benchmetrics

package generator

// Traversal instrumentation, compiled only under the benchmetrics tag. RFC 0074
// deferred fusing the 21 discovery walks "pending data"; this is that data.
//
// The counters are plain package-level variables rather than atomics because
// compilation is sequential and the benchmark reads them between iterations. If
// generation ever becomes concurrent this must become atomic — the untagged
// build has no counter at all, so nothing production-facing depends on it.
var (
	traversalWalks uint64
	traversalNodes uint64
)

func countTraversal() { traversalWalks++ }
func countNode()      { traversalNodes++ }

// TraversalCounts returns the walk entries and nodes visited since the last
// call, and resets both. It is exported because the benchmark lives in another
// package; the build tag keeps that surface out of every shipping build.
func TraversalCounts() (walks, nodes uint64) {
	walks, nodes = traversalWalks, traversalNodes
	traversalWalks, traversalNodes = 0, 0
	return walks, nodes
}
