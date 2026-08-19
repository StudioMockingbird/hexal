//go:build !benchmetrics

package generator

// countTraversal and countNode are no-ops in every ordinary build: the
// production compiler carries no counter and no package-level mutable state for
// a benchmark's benefit. The instrumented forms live in
// traversal_metrics_bench.go behind the benchmetrics tag (RFC 0080 Part 3).
func countTraversal() {}
func countNode()      {}
