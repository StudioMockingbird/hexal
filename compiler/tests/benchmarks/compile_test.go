package benchmarks

import "testing"

// The seven success-path shapes, each stressing a different phase. Sources live
// in shared_test.go; each benchmark is one line so the table stays the single
// place a shape is described.

func BenchmarkScalar(b *testing.B)        { runBenchmarkProgram(b, benchmarkPrograms[0]) }
func BenchmarkGenericsHeavy(b *testing.B) { runBenchmarkProgram(b, benchmarkPrograms[1]) }
func BenchmarkMultiModule(b *testing.B)   { runBenchmarkProgram(b, benchmarkPrograms[2]) }
func BenchmarkCollections(b *testing.B)   { runBenchmarkProgram(b, benchmarkPrograms[3]) }
func BenchmarkText(b *testing.B)          { runBenchmarkProgram(b, benchmarkPrograms[4]) }
func BenchmarkConcurrency(b *testing.B)   { runBenchmarkProgram(b, benchmarkPrograms[5]) }
func BenchmarkErrorPaths(b *testing.B)    { runBenchmarkProgram(b, benchmarkPrograms[6]) }
