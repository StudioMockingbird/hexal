package integration

// Shared test helpers.

import (
	"maps"
	"slices"
	"strings"

	"hexal/compiler"
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
		t.Fatalf("expected success; got %d diagnostic(s):\n%s\n--- source ---\n%s", len(result.Stderr), strings.Join(result.Stderr, "\n"), source)
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
		t.Fatalf("expected a diagnostic containing %q; got none:\n%s", want, source)
	}
	if !strings.Contains(result.Stderr[0], want) {
		t.Fatalf("first diagnostic %q does not contain %q:\n%s\n--- source ---\n%s", result.Stderr[0], want, strings.Join(result.Stderr, "\n"), source)
	}
}

// hexalH returns the generated shared program-support header.
func hexalH(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	return moduleFile(t, result, "hexal.h")
}

// rootC returns the entrypoint module's generated C file.
func rootC(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	return moduleFile(t, result, "modules/app.c")
}

// rootH returns the entrypoint module's generated header file.
func rootH(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	return moduleFile(t, result, "modules/app.h")
}

// moduleFile returns one generated artifact by logical key, failing the test
// if the compiler did not produce it.
func moduleFile(t *testing.T, result compiler.CompilationResult, key string) string {
	t.Helper()
	content, ok := result.Files[key]
	if !ok {
		t.Fatalf("generated files %v do not contain %q", sortedKeys(result.Files), key)
	}
	return content
}

func sortedKeys(files map[string]string) []string {
	return slices.Sorted(maps.Keys(files))
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

// The component-artifact accessors live here beside hexalH, rootC, and rootH
// rather than in whichever feature test first needed one.
func arrayH(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	return moduleFile(t, result, "hexal/array.h")
}

func dictH(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	return moduleFile(t, result, "hexal/dict.h")
}

func listH(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	return moduleFile(t, result, "hexal/list.h")
}

func stringH(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	return moduleFile(t, result, "hexal/string.h")
}

func stringC(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	return moduleFile(t, result, "hexal/string.c")
}
