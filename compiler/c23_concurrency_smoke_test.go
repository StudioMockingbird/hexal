//go:build c23

package compiler

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// RFC 0037 smoke tests: a spawn/Channel program generates C that gcc
// accepts, and a compiled run computes the expected total through a real
// task. The task runtime requires C23 <threads.h>; toolchains that define
// __STDC_NO_THREADS__ (such as the MinGW builds without winpthreads
// threads.h) cannot build it and the run tests skip.

// c23ThreadsAvailable probes whether the toolchain provides <threads.h>.
func c23ThreadsAvailable(t *testing.T) bool {
	t.Helper()
	if _, err := exec.LookPath("gcc"); err != nil {
		return false
	}
	dir := t.TempDir()
	probe := filepath.Join(dir, "probe.c")
	if err := os.WriteFile(probe, []byte("#include <threads.h>\nint main(void) { thrd_t t; (void)t; return 0; }\n"), 0644); err != nil {
		t.Fatal(err)
	}
	command := exec.Command("gcc", "-std=c23", "-c", probe, "-o", filepath.Join(dir, "probe.o"))
	output, err := command.CombinedOutput()
	if err != nil {
		t.Logf("threads.h unavailable: %v\n%s", err, output)
		return false
	}
	return true
}

func compileConcurrencySmoke(t *testing.T, source string) string {
	t.Helper()
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
	return exe
}

func TestGeneratedConcurrencyRuns(t *testing.T) {
	if !c23ThreadsAvailable(t) {
		t.Skip("gcc without C23 <threads.h> cannot build the task runtime")
	}
	source := "fun worker(count: Int32, ch: Channel<Int32>): Nil\n    mut index: Int32 = 0\n    while index < count do\n        ch.send(index)\n        Task.yield()\n        index = index + 1\n    end\n    ch.close()\n    return nil\nend\nfun run(): Nil | Error\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 8)\n    defer ch.free(h)\n    worker_task: Task<Nil> = try spawn worker(4, ch)\n    mut total: Int32 = 0\n    while true do\n        step: Int32 | EoS = ch.receive()\n        if step is EoS\n            break\n        end\n        total = total + step\n        Task.yield()\n    end\n    worker_task.join()\n    print(total)\n    return nil\nend\nrun()\n"
	exe := compileConcurrencySmoke(t, source)
	run, err := exec.Command(exe).CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, run)
	}
	normalized := strings.ReplaceAll(string(run), "\r\n", "\n")
	if normalized != "6" {
		t.Fatalf("program output = %q, want %q", normalized, "6")
	}
}

func TestGeneratedTaskJoinRuns(t *testing.T) {
	if !c23ThreadsAvailable(t) {
		t.Skip("gcc without C23 <threads.h> cannot build the task runtime")
	}
	source := "fun square(value: Int32): Int32\n    return value * value\nend\nfun run(): Int32 | Error\n    first: Task<Int32> = try spawn square(6)\n    second: Task<Int32> = try spawn square(7)\n    return first.join() + second.join()\nend\nfun demo(): Int32\n    value: Int32 = try run()\n    return value\nend\nprint(demo())\n"
	exe := compileConcurrencySmoke(t, source)
	run, err := exec.Command(exe).CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, run)
	}
	normalized := strings.ReplaceAll(string(run), "\r\n", "\n")
	if normalized != "85" {
		t.Fatalf("program output = %q, want %q", normalized, "85")
	}
}

func TestGeneratedMutexRuns(t *testing.T) {
	if !c23ThreadsAvailable(t) {
		t.Skip("gcc without C23 <threads.h> cannot build the task runtime")
	}
	source := "fun worker(m: Mutex, counter: MutPtr<Int32>): Nil\n    mut index: Int32 = 0\n    while index < 100 do\n        m.lock()\n        counter.value = counter.value + 1\n        m.unlock()\n        Task.yield()\n        index = index + 1\n    end\n    return nil\nend\nfun run(): Int32 | Error\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    defer m.free(h)\n    mut count: Int32 = 0\n    first: Task<Nil> = try spawn worker(m, ref count)\n    second: Task<Nil> = try spawn worker(m, ref count)\n    first.join()\n    second.join()\n    return count\nend\nfun demo(): Int32\n    value: Int32 = try run()\n    return value\nend\nprint(demo())\n"
	exe := compileConcurrencySmoke(t, source)
	run, err := exec.Command(exe).CombinedOutput()
	if err != nil {
		t.Fatalf("generated program failed: %v\n%s", err, run)
	}
	normalized := strings.ReplaceAll(string(run), "\r\n", "\n")
	if normalized != "200" {
		t.Fatalf("program output = %q, want %q", normalized, "200")
	}
}
