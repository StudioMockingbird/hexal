package benchmarks

import (
	"hexal/compiler"
	"testing"
)

// Failure-path benchmarks, one per failing stage (RFC 0080 Part 2). RFC 0075
// carried a single BenchmarkFailure, which measured one checker error and left
// the lexer's, the parser's, the resolver's, and the at-volume diagnostic
// paths unmeasured across a diagnostic surface of roughly 978 construction
// sites.
// failureBenchmarkPrograms are the five shapes. Each must fail, and
// TestFailureBenchmarkProgramsFail proves it: a silently-succeeding failure
// benchmark measures the success path and reports nothing about diagnostics.
var failureBenchmarkPrograms = []benchmarkProgram{
	{
		name: "lex",
		sources: map[string]string{
			"app.hex": "value: Int32 := 1 $ 2\nother: Int32 := 0b\ntext: String := \"unterminated\n",
		},
		entrypoint: "app.hex",
	},
	{
		name: "parse",
		sources: map[string]string{
			"app.hex": "fun broken( do\nend\n" +
				"type = { x: Int32 }\n" +
				"fun other(): Int32 do\n    return\n" +
				"value: Int32 := \n",
		},
		entrypoint: "app.hex",
	},
	{
		name: "resolve",
		sources: map[string]string{
			"app.hex":   "module Missing = import \"./absent\"\nmodule Bad = import \"nonrelative\"\nvalue: Int32 := 1\n",
			"cycle.hex": "module Back = import \"./app\"\nexport fun f(): Int32 do\n    return 1\nend\n",
		},
		entrypoint: "app.hex",
	},
	{
		name: "check",
		sources: map[string]string{
			"app.hex": "fun demo(h: Heap) do\n" +
				"    wrong: Int32 := true\n" +
				"    unknown: Int32 := notDeclared\n" +
				"    p: MutPtr<Int32> := h.allocate<Int32>(1)\n" +
				"    h.free(p)\n" +
				"    h.free(p)\n" +
				"end\n",
		},
		entrypoint: "app.hex",
	},
	{
		name:       "many",
		sources:    map[string]string{"app.hex": manyErrorsSource()},
		entrypoint: "app.hex",
	},
}

// manyErrorsSource builds a program with fifty independent type errors. It is
// the only benchmark where diagnostic construction, module stamping, sorting,
// and rendering dominate rather than appear as noise.
func manyErrorsSource() string {
	source := ""
	for index := 0; index < 50; index++ {
		source += "bad" + string(rune('a'+index%26)) + string(rune('0'+index/26)) + ": Int32 := true\n"
	}
	return source
}
func BenchmarkFailureLex(b *testing.B)     { runBenchmarkProgram(b, failureBenchmarkPrograms[0]) }
func BenchmarkFailureParse(b *testing.B)   { runBenchmarkProgram(b, failureBenchmarkPrograms[1]) }
func BenchmarkFailureResolve(b *testing.B) { runBenchmarkProgram(b, failureBenchmarkPrograms[2]) }
func BenchmarkFailureCheck(b *testing.B)   { runBenchmarkProgram(b, failureBenchmarkPrograms[3]) }
func BenchmarkFailureMany(b *testing.B)    { runBenchmarkProgram(b, failureBenchmarkPrograms[4]) }

// TestBenchmarkProgramsCompile guards the suite's contract: every success-path
// program compiles cleanly.
func TestBenchmarkProgramsCompile(t *testing.T) {
	for _, program := range benchmarkPrograms {
		result := compiler.Compile(program.sources, program.entrypoint, compiler.Project{})
		if result.ExitCode != compiler.ExitSuccess {
			t.Errorf("%s: benchmark program failed to compile:\n%s", program.name, result.Stderr)
		}
	}
}

// TestFailureBenchmarkProgramsFail is the other half of that contract: each
// failure benchmark must actually fail, with at least one diagnostic.
func TestFailureBenchmarkProgramsFail(t *testing.T) {
	for _, program := range failureBenchmarkPrograms {
		result := compiler.Compile(program.sources, program.entrypoint, compiler.Project{})
		if result.ExitCode != compiler.ExitFailure {
			t.Errorf("%s: failure program compiled successfully; the benchmark is not exercising diagnostics", program.name)
			continue
		}
		if len(result.Stderr) == 0 {
			t.Errorf("%s: failure program produced no diagnostics", program.name)
		}
	}
}

// The at-volume shape must report many diagnostics, not one otherwise it
// measures the same thing as BenchmarkFailureCheck under a different name.
func TestFailureManyReportsManyDiagnostics(t *testing.T) {
	program := failureBenchmarkPrograms[4]
	result := compiler.Compile(program.sources, program.entrypoint, compiler.Project{})
	if len(result.Stderr) < 25 {
		t.Fatalf("the at-volume failure program reported %d diagnostics, want at least 25", len(result.Stderr))
	}
}
