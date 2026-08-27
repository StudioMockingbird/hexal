package fuzz

import (
	"fmt"
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

// recordingFailer satisfies failer without touching the real *testing.T, so
// an injection test can prove a guard fires without failing the test that
// proves it.
type recordingFailer struct {
	fired   bool
	message string
}

func (r *recordingFailer) Helper() {}
func (r *recordingFailer) Fatalf(format string, args ...any) {
	r.fired = true
	r.message = fmt.Sprintf(format, args...)
}

// allSnippets loads every catalog snippet once; every caller in this package
// shares one load rather than re-reading the embedded catalog per test.
func allSnippets(t *testing.T) []snippets.Snippet {
	t.Helper()
	categories, err := snippets.Load()
	if err != nil {
		t.Fatalf("snippets.Load() error = %v", err)
	}
	all := make([]snippets.Snippet, 0)
	for _, category := range categories {
		all = append(all, category.Snippets...)
	}
	if len(all) == 0 {
		t.Fatal("snippet catalog is empty")
	}
	return all
}

// Tier 0, corpus half: no snippet in the catalog reaches an Unknown Error.
// An Unknown Error means the compiler itself is at fault, never an accepted
// rejection, so it must never fire for a program the catalog considers
// meaningful.
func TestCorpusNeverProducesUnknownError(t *testing.T) {
	for _, snippet := range allSnippets(t) {
		result := compiler.Compile(snippet.Sources, snippet.Entrypoint, compiler.Project{})
		assertNoUnknownError(t, result)
	}
}

// Tier 0, corpus half: no snippet's generated output contains a dispatch
// tripwire. A lowering path that falls back to emitting Hexal spelling
// instead of failing closed produces C that cannot compile, and this is the
// only place across the whole corpus that would notice.
func TestCorpusNeverProducesDispatchTripwire(t *testing.T) {
	for _, snippet := range allSnippets(t) {
		result := compiler.Compile(snippet.Sources, snippet.Entrypoint, compiler.Project{})
		if result.ExitCode != compiler.ExitSuccess {
			continue
		}
		assertNoDispatchTripwire(t, result.Files)
	}
}

// Tier 0, injection half: the Unknown Error guard fires on a result that
// actually carries one. A tripwire nobody has ever seen fire is
// indistinguishable from a broken one.
func TestUnknownErrorGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	assertNoUnknownError(recorder, compiler.CompilationResult{
		ExitCode: compiler.ExitFailure,
		Stderr:   []string{"[Unknown Error] a violated internal invariant"},
	})
	if !recorder.fired {
		t.Fatal("assertNoUnknownError did not fire on a result carrying an Unknown Error")
	}
}

// Tier 0, injection half: the dispatch tripwire guard fires on generated
// text carrying a marker from the closed list.
func TestDispatchTripwireGuardFires(t *testing.T) {
	for _, marker := range dispatchTripwires {
		recorder := &recordingFailer{}
		assertNoDispatchTripwire(recorder, map[string]string{
			"modules/app.c": "int main(void) {\n    " + marker + "\n}\n",
		})
		if !recorder.fired {
			t.Fatalf("assertNoDispatchTripwire did not fire on marker %q", marker)
		}
	}
}

// Tier 0, injection half: the fail-closed guard fires on a failed result
// that still carries a non-empty Files map, and on a successful result
// missing hexal.h or every modules/*.c entry.
func TestFailClosedGuardFires(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		result compiler.CompilationResult
	}{
		{"failed result with files", compiler.CompilationResult{ExitCode: compiler.ExitFailure, Files: map[string]string{"hexal.h": ""}}},
		{"success missing hexal.h", compiler.CompilationResult{ExitCode: compiler.ExitSuccess, Files: map[string]string{"modules/app.c": ""}}},
		{"success missing modules/*.c", compiler.CompilationResult{ExitCode: compiler.ExitSuccess, Files: map[string]string{"hexal.h": ""}}},
	} {
		recorder := &recordingFailer{}
		assertFailClosed(recorder, testCase.result)
		if !recorder.fired {
			t.Fatalf("%s: assertFailClosed did not fire", testCase.name)
		}
	}
}

// Tier 0, injection half: the diagnostic well-formedness guard fires on an
// empty line, a non-positive coordinate, and a module the compilation was
// never given.
func TestWellFormedDiagnosticsGuardFires(t *testing.T) {
	sourceKeys := map[string]bool{"app.hex": true}
	for _, testCase := range []struct {
		name string
		line string
	}{
		{"empty line", ""},
		{"zero line", "[Syntax Error] bad at app.hex:0:1"},
		{"zero column", "[Syntax Error] bad at app.hex:1:0"},
		{"unknown module", "[Syntax Error] bad at other.hex:1:1"},
	} {
		recorder := &recordingFailer{}
		assertWellFormedDiagnostics(recorder, []string{testCase.line}, sourceKeys)
		if !recorder.fired {
			t.Fatalf("%s: assertWellFormedDiagnostics did not fire for %q", testCase.name, testCase.line)
		}
	}
}

// Tier 0, injection half: the determinism guard fires when the same input
// compiled twice disagrees. compiler.Compile is itself deterministic, so this
// proves the guard by feeding it two already-different results rather than
// by making Compile misbehave.
func TestDeterminismGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	first := compiler.CompilationResult{ExitCode: compiler.ExitSuccess, Files: map[string]string{"hexal.h": "a"}}
	second := compiler.CompilationResult{ExitCode: compiler.ExitSuccess, Files: map[string]string{"hexal.h": "b"}}
	compareResults(recorder, first, second)
	if !recorder.fired {
		t.Fatal("compareResults did not fire on two results with a differing artifact")
	}
}
