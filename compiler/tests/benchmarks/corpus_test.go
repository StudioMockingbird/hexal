package benchmarks

import (
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

// corpusCompilation is one catalog snippet shaped for compiler.Compile.
type corpusCompilation struct {
	sources    map[string]string
	entrypoint string
}

// corpusBenchmarkSink receives every compilation result so the Compile calls
// are not eliminated.
var corpusBenchmarkSink compiler.CompilationResult

// BenchmarkCorpus compiles every catalog snippet once per iteration; it is the
// only end-to-end aggregate in the suite. Loading the catalog and
// totalling its source bytes happen before ResetTimer, so only compilation is
// measured.
func BenchmarkCorpus(b *testing.B) {
	catalog, err := snippets.Load()
	if err != nil {
		b.Fatal(err)
	}
	programs := make([]corpusCompilation, 0, 98)
	sourceBytes := 0
	for _, category := range catalog {
		for _, snippet := range category.Snippets {
			programs = append(programs, corpusCompilation{
				sources: snippet.Sources, entrypoint: snippet.Entrypoint,
			})
			for _, source := range snippet.Sources {
				sourceBytes += len(source)
			}
		}
	}
	b.ReportAllocs()
	b.SetBytes(int64(sourceBytes))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, program := range programs {
			corpusBenchmarkSink = compiler.Compile(program.sources, program.entrypoint, compiler.Project{})
		}
	}
}
