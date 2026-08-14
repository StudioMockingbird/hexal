package tests

// Shared test helpers.

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// rootSourceKey is the logical .hex key every test source compiles as.
const rootSourceKey = "app.hex"

// compileSource compiles one test source string as the root module.
func compileSource(source string) compiler.CompilationResult {
	return compiler.Compile(map[string]string{rootSourceKey: source}, rootSourceKey)
}

// assertCompiles requires the source to compile and returns the result.
func assertCompiles(t *testing.T, source string) compiler.CompilationResult {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("expected success, got %d diagnostic(s):\n%s\n--- source ---\n%s", len(result.Stderr), strings.Join(result.Stderr, "\n"), source)
	}
	return result
}

// assertRejects requires the source to fail and the first diagnostic to
// contain want.
func assertRejects(t *testing.T, source, want string) {
	t.Helper()
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure {
		t.Fatalf("expected rejection, but the source compiled:\n%s", source)
	}
	if len(result.Stderr) == 0 {
		t.Fatalf("expected a diagnostic containing %q, got none:\n%s", want, source)
	}
	if !strings.Contains(result.Stderr[0], want) {
		t.Fatalf("first diagnostic %q does not contain %q:\n%s\n--- source ---\n%s", result.Stderr[0], want, strings.Join(result.Stderr, "\n"), source)
	}
}

// assertEmits requires the source to compile and the generated files to
// contain every want string.
func assertEmits(t *testing.T, source string, wants ...string) {
	t.Helper()
	result := assertCompiles(t, source)
	generated := result.MainC + "\n" + result.MainH
	for _, want := range wants {
		if !strings.Contains(generated, want) {
			t.Fatalf("generated output does not contain %q:\n%s\n--- source ---\n%s", want, generated, source)
		}
	}
}

func withoutLineDirectives(source string) string {
	lines := strings.Split(source, "\n")
	filtered := lines[:0]
	for _, line := range lines {
		if !strings.HasPrefix(line, "#line ") {
			filtered = append(filtered, line)
		}
	}
	return strings.Join(filtered, "\n")
}
