package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Smoke-check that RFC 0029 Error/try/errdefer programs generate C that gcc
// accepts.
func TestGeneratedErrorCCompiles(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "fun cleanup(value: Int32)\nend\nfun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"no count\")\nend\nfun demo(release: Bool): Int32 | Error\n    errdefer cleanup(1)\n    defer cleanup(2)\n    mut total: Int32 = 0\n    while true do\n        count: Int32 = try read_count()\n        total = total + count\n        break\n    end\n    if release\n        return Error.new(\"Final Error\", \"done\")\n    end\n    return total\nend"
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
	command := exec.Command("gcc", "-std=c23", "-Wall", "-Wextra", "-c", mainC, "-o", filepath.Join(dir, "main.o"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("gcc rejected generated C: %v\n%s", err, output)
	}
}
