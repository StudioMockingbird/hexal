//go:build c23

package c23validation

// C23 harness: the single compile/run path for every C23-tagged test.
// The tag gates the suite; an explicitly requested tagged run fails when
// the toolchain is missing instead of skipping.

import (
	"slices"

	"hexal/compiler"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// assertCompiles requires the source to compile and returns the result.
// Private copy of the integration package's helper: this package must remain
// independent of the active suite.
func assertCompiles(t *testing.T, source string) compiler.CompilationResult {
	t.Helper()
	result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("expected success; got %d diagnostic(s):\n%s\n--- source ---\n%s", len(result.Stderr), strings.Join(result.Stderr, "\n"), source)
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

// buildGeneratedC materializes every artifact in Files (hexal.h, each module
// C/header pair, and the demand-driven hexal/ component files) under dir and
// compiles every .c translation unit with the harness warning policy,
// returning the executable path.
func buildGeneratedC(t *testing.T, result compiler.CompilationResult, dir string) string {
	t.Helper()
	for name, content := range result.Files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}
	exe := filepath.Join(dir, "hexal.exe")
	command := exec.Command(c23Compiler(t), "-std=c23", "-Wall", "-Wextra", "-Werror", "-Wno-unused-function", "-Wno-unused-variable", "-Wno-unused-parameter", "-Wno-unused-but-set-variable", "-Wno-maybe-uninitialized", "-I", dir)
	names := make([]string, 0, len(result.Files))
	for name := range result.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if strings.HasSuffix(name, ".c") {
			command.Args = append(command.Args, filepath.Join(dir, name))
		}
	}
	command.Args = append(command.Args, "-o", exe)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
	return exe
}

// compileGeneratedC writes every generated artifact and compiles every .c
// translation unit with -std=c23 -Wall -Wextra -Werror: any warning or error
// fails the test. The -Wno-unused-function, -Wno-unused-variable, and
// -Wno-unused-parameter flags tolerate generator helper-family emission and
// legally-unused bindings; const-discards and other warnings still fail.
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
