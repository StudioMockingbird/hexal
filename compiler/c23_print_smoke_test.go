package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Smoke-check that RFC 0030 print programs generate C that gcc accepts and
// that a compiled run emits the expected text (newlines translated by the C
// text stream on Windows, so the comparison normalizes line endings).
func TestGeneratedPrintRuns(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "type Point = {\n    x: Int32,\n    y: Int32,\n}\nprint(\"count = \", 42, \"\\n\")\nprint(true, false, nil)\nprint(1.5, -2.5)\npoint: Point = Point { x = 10, y = 20 }\nprint(point)\nprint(\"\\n\")"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	dir := t.TempDir()
	mainC := filepath.Join(dir, "main.c")
	mainH := filepath.Join(dir, "main.h")
	if err := os.WriteFile(mainC, []byte(result.MainC), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(mainH, []byte(result.MainH), 0644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "main.exe")
	command := exec.Command("gcc", "-std=c23", "-Wall", "-Wextra", mainC, "-o", exe)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
	run, err := exec.Command(exe).CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, run)
	}
	normalized := strings.ReplaceAll(string(run), "\r\n", "\n")
	want := "count = 42\ntruefalsenil1.5-2.5Point { x = 10, y = 20 }\n"
	if normalized != want {
		t.Fatalf("program output = %q, want %q", normalized, want)
	}
}
