package snippets_test

import (
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

// corpusCompilation is one catalog snippet shaped for compiler.Compile. The
// corpus lives here rather than in compiler's test binary: the loader is
// already a workbench package, and the compiler must not depend on it.
type corpusCompilation struct {
	sources    map[string]string
	entrypoint string
}

// corpusBenchmarkSink receives every compilation result so the Compile calls
// are not eliminated.
var corpusBenchmarkSink compiler.CompilationResult

// BenchmarkCorpus compiles every catalog snippet once per iteration; it is
// the only end-to-end aggregate in the suite (RFC 0075).
func BenchmarkCorpus(b *testing.B) {
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
	for i := 0; i < b.N; i++ {
		for _, program := range programs {
			corpusBenchmarkSink = compiler.Compile(program.sources, program.entrypoint)
		}
	}
}
