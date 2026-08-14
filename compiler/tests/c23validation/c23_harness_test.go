//go:build c23

package c23validation

// C23 harness: the single compile/run path for every C23-tagged test.
// The tag gates the suite; an explicitly requested tagged run fails when
// the toolchain is missing instead of skipping (RFC 0048).

import (
	"hexal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// assertCompiles requires the source to compile and returns the result.
// Private copy of the integration package's helper: this package must remain
// independent of the active suite (ADR 0054).
func assertCompiles(t *testing.T, source string) compiler.CompilationResult {
	t.Helper()
	result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("expected success, got %d diagnostic(s):\n%s\n--- source ---\n%s", len(result.Stderr), strings.Join(result.Stderr, "\n"), source)
	}
	return result
}

// c23Compiler resolves the gcc toolchain once per call. Failure is fatal:
// the tagged suite must not silently skip.
func c23Compiler(t *testing.T) string {
	t.Helper()
	command, err := exec.LookPath("gcc")
	if err != nil {
		t.Fatalf("c23 suite requires gcc: %v", err)
	}
	return command
}

// buildGeneratedC writes main.c/main.h and compiles main.c to an executable
// with the harness warning policy, returning the executable path.
func buildGeneratedC(t *testing.T, result compiler.CompilationResult, dir string) string {
	t.Helper()
	mainC := filepath.Join(dir, "main.c")
	mainH := filepath.Join(dir, "main.h")
	if err := os.WriteFile(mainC, []byte(result.MainC), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainH, []byte(result.MainH), 0644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "main.exe")
	command := exec.Command(c23Compiler(t), "-std=c23", "-Wall", "-Wextra", "-Werror", "-Wno-unused-function", "-Wno-unused-variable", "-Wno-unused-parameter", mainC, "-o", exe)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
	return exe
}

// compileGeneratedC writes main.c/main.h and compiles main.c with
// -std=c23 -Wall -Wextra -Werror: any warning or error fails the test.
// ponytail: -Wno-unused-function -Wno-unused-variable -Wno-unused-parameter
// tolerate generator helper-family emission and legally-unused bindings;
// const-discards and other warnings still fail. Family-emission debt in
// status.md.
func compileGeneratedC(t *testing.T, result compiler.CompilationResult) {
	t.Helper()
	buildGeneratedC(t, result, t.TempDir())
}

// runGeneratedC compiles and runs the program with the same warning policy
// and returns its stdout with line endings normalized.
func runGeneratedC(t *testing.T, result compiler.CompilationResult) string {
	t.Helper()
	exe := buildGeneratedC(t, result, t.TempDir())
	run, err := exec.Command(exe).CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, run)
	}
	return strings.ReplaceAll(string(run), "\r\n", "\n")
}

// trapGeneratedC compiles and runs a program that must terminate by a
// runtime trap: a successful exit fails the test, and the trap must carry
// the generated runtime's diagnostic text.
func trapGeneratedC(t *testing.T, result compiler.CompilationResult) {
	t.Helper()
	exe := buildGeneratedC(t, result, t.TempDir())
	run, err := exec.Command(exe).CombinedOutput()
	if err == nil {
		t.Fatalf("program must trap but exited successfully: %s", run)
	}
	if !strings.Contains(string(run), "[Runtime Error]") {
		t.Fatalf("program failed without a runtime diagnostic: %v\n%s", err, run)
	}
}
