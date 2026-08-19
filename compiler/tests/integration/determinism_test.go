package integration

// RFC 0074 R18: two gaps the suite carried without a committed test —
// generated output was verified byte-identical across repeated compiles but
// nothing asserted it, and CompilationStats was covered only for SourceLines
// and a non-zero TokenCount, which is how RFC 0073's D7 (a permanently zero
// ParseDuration) survived.

import (
	"testing"

	"hexal/compiler"
)

// Compiling the same sources twice produces byte-identical artifacts. Map
// iteration order is randomized per run, so a discovery pass that reaches
// output without sorting first fails this within a few runs.
func TestCompilationIsDeterministic(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module Shapes = import \"./shapes\"\n" +
			"fun run(h: Heap): Int32 do\n" +
			"    values: List<Int32> := List<Int32>.new(h)\n" +
			"    defer values.free(h)\n" +
			"    values.push(Shapes.corners())\n" +
			"    text: String := \"corners\"\n" +
			"    print(text)\n" +
			"    counts: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n" +
			"    defer counts.free(h)\n" +
			"    return values.length().to<Int32>()\n" +
			"end\n" +
			"h: Heap := Heap.new()\n" +
			"total: Int32 := run(h)\n",
		"shapes.hex": "export fun corners(): Int32 do\n    return 4\nend\n",
	}
	first := compiler.Compile(sources, "app.hex", compiler.Project{})
	if first.ExitCode != compiler.ExitSuccess {
		t.Fatalf("first compile failed: %v", first.Stderr)
	}
	for attempt := range 8 {
		next := compiler.Compile(sources, "app.hex", compiler.Project{})
		if next.ExitCode != compiler.ExitSuccess {
			t.Fatalf("compile %d failed: %v", attempt+2, next.Stderr)
		}
		if len(next.Files) != len(first.Files) {
			t.Fatalf("compile %d produced %d artifacts, want %d", attempt+2, len(next.Files), len(first.Files))
		}
		for name, content := range first.Files {
			if next.Files[name] != content {
				t.Fatalf("compile %d changed %s:\nfirst:\n%s\nlater:\n%s", attempt+2, name, content, next.Files[name])
			}
		}
	}
}

// Diagnostics are deterministic too: a program with several errors reports
// them in the same order every time.
func TestDiagnosticOrderIsDeterministic(t *testing.T) {
	source := "a: Int32 := true\nb: Bool := 1\nc: Int32 := unknownName\nd: Int32 := 1 / 0\n"
	first := compileSource(source)
	if first.ExitCode != compiler.ExitFailure {
		t.Fatalf("want a failing program, got %#v", first)
	}
	for attempt := range 8 {
		next := compileSource(source)
		if len(next.Stderr) != len(first.Stderr) {
			t.Fatalf("compile %d reported %d diagnostics, want %d", attempt+2, len(next.Stderr), len(first.Stderr))
		}
		for index, message := range first.Stderr {
			if next.Stderr[index] != message {
				t.Fatalf("compile %d diagnostic %d = %q, want %q", attempt+2, index, next.Stderr[index], message)
			}
		}
	}
}

// Every CompilationStats field is populated on a successful build. A field
// that is never assigned reads as zero and no other test would notice.
func TestCompilationStatsArePopulated(t *testing.T) {
	result := compileSource("fun twice(v: Int32): Int32 do\n    return v * 2\nend\nvalue: Int32 := twice(21)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("compile failed: %v", result.Stderr)
	}
	stats := result.Stats
	if stats.SourceLines != 5 {
		t.Errorf("SourceLines = %d, want 5", stats.SourceLines)
	}
	if stats.TokenCount == 0 {
		t.Error("TokenCount = 0, want the lexed token count")
	}
	// Durations are wall-clock and can legitimately round to zero on a fast
	// machine, so the checkable invariant is the relationship between them,
	// not a lower bound on any one.
	sum := stats.LexDuration + stats.CheckDuration + stats.GenerateDuration
	if stats.PixelSubtotal != sum {
		t.Errorf("PixelSubtotal = %v, want the sum of the stage durations %v", stats.PixelSubtotal, sum)
	}
	if stats.TotalDuration < stats.PixelSubtotal {
		t.Errorf("TotalDuration = %v, want at least the stage subtotal %v", stats.TotalDuration, stats.PixelSubtotal)
	}
}

// A failed compilation still reports the stages that ran, so a build that
// fails in checking does not report a zeroed lex phase.
func TestCompilationStatsSurviveFailure(t *testing.T) {
	result := compileSource("x: Int32 := true\n")
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want a failing program, got %#v", result)
	}
	if result.Stats.SourceLines != 2 {
		t.Errorf("SourceLines = %d, want 2", result.Stats.SourceLines)
	}
	if result.Stats.TokenCount == 0 {
		t.Error("TokenCount = 0, want the lexed token count of a program that parsed")
	}
	if result.Stats.TotalDuration < result.Stats.PixelSubtotal {
		t.Errorf("TotalDuration = %v, want at least the stage subtotal %v", result.Stats.TotalDuration, result.Stats.PixelSubtotal)
	}
}
