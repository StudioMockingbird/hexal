// Package fuzz asserts absolute properties compiler.Compile and its earlier
// stages already claim -- fail-closed output, determinism, no internal
// Unknown Error, and well-formed diagnostics -- over both arbitrary and
// generated input. This file holds the oracles every target and corpus guard
// shares.
package fuzz

import (
	"strconv"
	"strings"

	"hexal/compiler"
)

// assertFailClosed checks invariant 2 both directions: a failed compile
// carries a non-nil empty Files map, and a successful one carries hexal.h
// plus at least one modules/*.c entry.
func assertFailClosed(t failer, result compiler.CompilationResult) {
	t.Helper()
	if result.ExitCode != compiler.ExitSuccess {
		if result.Files == nil {
			t.Fatalf("failed result has a nil Files map")
		}
		if len(result.Files) != 0 {
			t.Fatalf("failed result Files is not empty: %v", result.Files)
		}
		return
	}
	if _, ok := result.Files["hexal.h"]; !ok {
		t.Fatalf("successful result lacks hexal.h: %v", sortedFileNames(result.Files))
	}
	found := false
	for name := range result.Files {
		if strings.HasPrefix(name, "modules/") && strings.HasSuffix(name, ".c") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("successful result lacks any modules/*.c entry: %v", sortedFileNames(result.Files))
	}
}

// assertNoUnknownError checks invariant 4: an Unknown Error reports a
// violated internal invariant, never an accepted rejection.
func assertNoUnknownError(t failer, result compiler.CompilationResult) {
	t.Helper()
	for _, line := range result.Stderr {
		if strings.Contains(line, "[Unknown Error]") {
			t.Fatalf("Unknown Error reached the fuzz oracle: %q", line)
		}
	}
}

// assertDeterministic checks invariant 3 by compiling the same input twice
// and comparing the complete Files map and full diagnostic slice, not a
// summary or count.
func assertDeterministic(t failer, sources map[string]string, entrypoint string, project compiler.Project) compiler.CompilationResult {
	t.Helper()
	first := compiler.Compile(sources, entrypoint, project)
	second := compiler.Compile(sources, entrypoint, project)
	compareResults(t, first, second)
	return first
}

// compareResults is assertDeterministic's comparison half, split out so an
// injection test can prove it fires on two already-different results
// without needing compiler.Compile itself to misbehave.
func compareResults(t failer, first, second compiler.CompilationResult) {
	t.Helper()
	if first.ExitCode != second.ExitCode {
		t.Fatalf("ExitCode differs across identical compiles: %d vs %d", first.ExitCode, second.ExitCode)
	}
	if len(first.Files) != len(second.Files) {
		t.Fatalf("Files count differs across identical compiles: %d vs %d", len(first.Files), len(second.Files))
	}
	for name, content := range first.Files {
		other, ok := second.Files[name]
		if !ok {
			t.Fatalf("artifact %q present in the first compile, absent in the second", name)
		}
		if content != other {
			t.Fatalf("artifact %q differs across identical compiles", name)
		}
	}
	if len(first.Stderr) != len(second.Stderr) {
		t.Fatalf("diagnostic count differs across identical compiles: %d vs %d", len(first.Stderr), len(second.Stderr))
	}
	for index, line := range first.Stderr {
		if line != second.Stderr[index] {
			t.Fatalf("diagnostic %d differs across identical compiles: %q vs %q", index, line, second.Stderr[index])
		}
	}
}

// assertWellFormedDiagnostics checks invariant 5 over a failed result's
// rendered Stderr lines: each is non-empty, and each anchored suffix (when
// present) names one of the supplied logical keys with a positive
// line:column. sourceKeys is the exact set of keys the compilation was given;
// a compilation-level diagnostic (missing entrypoint, invalid Project) may
// carry no anchored suffix at all and still passes.
func assertWellFormedDiagnostics(t failer, lines []string, sourceKeys map[string]bool) {
	t.Helper()
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			t.Fatalf("diagnostic line is empty")
		}
		position, ok := parseAnchoredPosition(line)
		if !ok {
			// No anchored suffix: a compilation-level diagnostic (missing
			// entrypoint, invalid Project) is allowed to carry none.
			continue
		}
		if position.line <= 0 || position.column <= 0 {
			t.Fatalf("diagnostic %q has a non-positive coordinate", line)
		}
		if position.module != "" && !sourceKeys[position.module] {
			t.Fatalf("diagnostic %q names module %q, not one of the supplied sources", line, position.module)
		}
	}
}

// anchoredPosition is the parsed location suffix of one rendered diagnostic:
// the module key (empty when the diagnostic names none) and its 1-based
// line and column, exactly as Diagnostic.Error renders them.
type anchoredPosition struct {
	module string
	line   int
	column int
}

// parseAnchoredPosition parses the final " at " suffix of one rendered
// diagnostic line: either "line:column" or "module:line:column". It reads
// the two final colon-delimited fields as positive decimal integers and
// treats everything before them as the module key, so a diagnostic whose
// own message contains " at " still round-trips correctly because the
// search anchors on the last occurrence.
func parseAnchoredPosition(rendered string) (anchoredPosition, bool) {
	marker := " at "
	at := strings.LastIndex(rendered, marker)
	if at < 0 {
		return anchoredPosition{}, false
	}
	suffix := rendered[at+len(marker):]
	fields := strings.Split(suffix, ":")
	if len(fields) < 2 {
		return anchoredPosition{}, false
	}
	columnField := fields[len(fields)-1]
	lineField := fields[len(fields)-2]
	column, err := strconv.Atoi(columnField)
	if err != nil {
		return anchoredPosition{}, false
	}
	line, err := strconv.Atoi(lineField)
	if err != nil {
		return anchoredPosition{}, false
	}
	module := strings.Join(fields[:len(fields)-2], ":")
	return anchoredPosition{module: module, line: line, column: column}, true
}

// dispatchTripwires is the closed marker list: an exact byte substring found
// in generated C means a lowering path emitted Hexal spelling instead of
// failing closed, producing C that cannot compile. Changing this list is a
// deliberate test-policy change, never an incidental edit.
var dispatchTripwires = []string{
	"= ;",
	"/* Cannot generate",
	"List<",
	"Dict<",
	"Fun<",
	".push(",
	".new()",
}

// assertNoDispatchTripwire checks every generated .c/.h artifact against the
// closed tripwire list.
func assertNoDispatchTripwire(t failer, files map[string]string) {
	t.Helper()
	for name, content := range files {
		if !strings.HasSuffix(name, ".c") && !strings.HasSuffix(name, ".h") {
			continue
		}
		for _, marker := range dispatchTripwires {
			if strings.Contains(content, marker) {
				t.Fatalf("artifact %q contains dispatch tripwire %q", name, marker)
			}
		}
	}
}

// failer is the subset of *testing.T and *testing.F fuzz targets and corpus
// guards need from an oracle: fail the current check with a formatted
// message. *testing.T satisfies this directly; f.Fuzz's inner func always
// receives a *testing.T, so no *testing.F caller needs it.
type failer interface {
	Helper()
	Fatalf(format string, args ...any)
}

func sortedFileNames(files map[string]string) []string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	return names
}
