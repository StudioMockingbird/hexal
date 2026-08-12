package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// Smoke-check that a Stream program (RFC 0031) generates C that gcc accepts
// with -std=c23: ops tables, produce node, list source, adapters, and free.
func TestGeneratedStreamCCompiles(t *testing.T) {
	if _, err := exec.LookPath("gcc"); err != nil {
		t.Skip("gcc not installed")
	}
	source := "type Counter = {\n    mut current: Int32,\n    limit: Int32,\n}\nfun counter_next(state: MutPtr<Counter>): Int32 | EoS\n    if state.current >= state.limit\n        return eos\n    end\n    result: Int32 = state.current\n    state.current = state.current + 1\n    return result\nend\nfun is_even(value: Int32): Bool\n    return value % 2 == 0\nend\nfun double(value: Int32): Int32\n    return value * 2\nend\nfun demo(h: Heap)\n    initial: Counter = Counter { current = 0, limit = 4 }\n    numbers: Stream<Int32> = Stream<Int32>.produce(h, initial, counter_next)\n    even: Stream<Int32> = numbers.filter(h, is_even)\n    doubled: Stream<Int32> = even.map(h, double)\n    limited: Stream<Int32> = doubled.take(h, 2)\n    defer limited.free(h)\n    mut total: Int32 = 0\n    for value in limited do\n        total = total + value\n    end\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    values.push(2)\n    source: Stream<Int32> = values.stream(h)\n    defer source.free(h)\n    step: Int32 | EoS = source.next()\n    empty: Stream<Int32> = Stream<Int32>.new()\n    empty.free(h)\nend"
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
