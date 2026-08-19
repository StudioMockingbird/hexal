//go:build benchmetrics

package benchmarks

import (
	"hexal/compiler"
	"hexal/compiler/generator"
	"hexal/workbench/snippets"
	"testing"
)

// Traversal counts, compiled only under the benchmetrics tag (RFC 0080 Part 3).
// RFC 0074 deferred fusing the 21 generator discovery walks "pending data" and
// named this suite as the data; these two numbers are it.
//
// Run with:
//
//	go test -tags benchmetrics -bench Traversal -benchtime 1x ./compiler/tests/benchmarks
//
// reportTraversals compiles the program b.N times and reports walk entries and
// expression-node visits per operation beside the ordinary columns.
func reportTraversals(b *testing.B, sources map[string]string, entrypoint string) {
	b.ReportAllocs()
	b.ResetTimer()
	generator.TraversalCounts()
	for i := 0; i < b.N; i++ {
		benchmarkCompileSink = compiler.Compile(sources, entrypoint, compiler.Project{})
	}
	b.StopTimer()
	walks, nodes := generator.TraversalCounts()
	b.ReportMetric(float64(walks)/float64(b.N), "walks/op")
	b.ReportMetric(float64(nodes)/float64(b.N), "nodes/op")
}
func BenchmarkTraversalScalar(b *testing.B) {
	program := benchmarkPrograms[0]
	reportTraversals(b, program.sources, program.entrypoint)
}
func BenchmarkTraversalMultiModule(b *testing.B) {
	program := benchmarkPrograms[2]
	reportTraversals(b, program.sources, program.entrypoint)
}
func BenchmarkTraversalCollections(b *testing.B) {
	program := benchmarkPrograms[3]
	reportTraversals(b, program.sources, program.entrypoint)
}

// BenchmarkTraversalCorpus is the aggregate the fusing decision needs: walks
// per compile across the whole catalog, where the per-module multiplier shows.
func BenchmarkTraversalCorpus(b *testing.B) {
	catalog, err := snippets.Load()
	if err != nil {
		b.Fatal(err)
	}
	programs := make([]corpusCompilation, 0, 98)
	for _, category := range catalog {
		for _, snippet := range category.Snippets {
			programs = append(programs, corpusCompilation{
				sources: snippet.Sources, entrypoint: snippet.Entrypoint,
			})
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	generator.TraversalCounts()
	for i := 0; i < b.N; i++ {
		for _, program := range programs {
			corpusBenchmarkSink = compiler.Compile(program.sources, program.entrypoint, compiler.Project{})
		}
	}
	b.StopTimer()
	walks, nodes := generator.TraversalCounts()
	b.ReportMetric(float64(walks)/float64(b.N), "walks/op")
	b.ReportMetric(float64(nodes)/float64(b.N), "nodes/op")
}

// A successful compilation must record traversals; a zero count means the
// counter was compiled out or never reached, which would make the benchmark
// silently meaningless.
func TestTraversalCounterRecords(t *testing.T) {
	generator.TraversalCounts()
	program := benchmarkPrograms[0]
	if result := compiler.Compile(program.sources, program.entrypoint, compiler.Project{}); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("benchmark program failed to compile: %v", result.Stderr)
	}
	walks, nodes := generator.TraversalCounts()
	if walks == 0 || nodes == 0 {
		t.Fatalf("traversal counts = %d walks, %d nodes; want both non-zero", walks, nodes)
	}
}
