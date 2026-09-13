//go:build c23

package driver

// Event-bridge qualification fixtures: startup failure reporting and
// completion racing the Task park commit under real scheduler Tasks.

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

// TestEventRuntimeInitializationFailure compiles the generated event bridge
// with one libuv initializer renamed to a probe that fails. The rename exists
// only in this probe's compile command; generated C carries no seam. The
// process must print the stable Runtime Error and terminate instead of
// leaving the initializing thread waiting for readiness.
func TestEventRuntimeInitializationFailure(t *testing.T) {
	selected := requireBackend(t)
	compileResult := compiler.Compile(map[string]string{
		"main.hex": "fun helper(): Int32 do\n    return 7\nend\nfun run(): Int32 | Error do\n    out: IO := try IO.stdout()\n    w: Size | Error := out.write(\"ok\".bytes())\n    task: Task<Int32> := try spawn helper()\n    return task.join()\nend\nvalue: Int32 | Error := run()\n",
	}, "main.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if len(compileResult.Stderr) > 0 {
		t.Fatalf("Hexal compilation failed: %v", compileResult.Stderr)
	}
	for _, testCase := range []struct {
		name   string
		rename string
		probe  string
	}{
		{"loop-init", "-Duv_loop_init=probe_uv_loop_init", "int probe_uv_loop_init(uv_loop_t *loop) { (void)loop; return UV_ENOMEM; }"},
		{"async-init", "-Duv_async_init=probe_uv_async_init", "int probe_uv_async_init(uv_loop_t *loop, uv_async_t *handle, uv_async_cb cb) { (void)loop; (void)handle; (void)cb; return UV_ENOMEM; }"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			staging := t.TempDir()
			if _, err := materialize(staging, compileResult.Files); err != nil {
				t.Fatal(err)
			}
			native, err := materializeDependencies(staging, compileResult.Dependencies)
			if err != nil {
				t.Fatal(err)
			}
			var result BuildResult
			if err := compileNativeDependencies(selected, staging, native, &result); err != nil {
				failWithLastCommand(t, &result, err)
			}
			fixture := filepath.Join(staging, "init_failure_probe.c")
			source := `#include "hexal/event.h"
#include <stdio.h>
#include <stdlib.h>
#include <uv.h>

hex_task *hex_task_current(void) { return nullptr; }
void hex_task_event_arm(hex_task *task, void *pending) { (void)task; (void)pending; }
void hex_task_event_wake(hex_task *task) { (void)task; }
void hex_task_event_suspend(hex_task *task) { (void)task; }
[[noreturn]] void hex_runtime_trap(const char *message) {
    fputs(message, stderr);
    fflush(stderr);
    _Exit(3);
}
` + testCase.probe + `

int main(void) {
    hex_event_runtime_init();
    puts("initialization unexpectedly succeeded");
    return 0;
}
`
			if err := os.WriteFile(fixture, []byte(source), 0o644); err != nil {
				t.Fatal(err)
			}
			eventSource := filepath.Join(staging, "hexal", "event.c")
			eventOptions := append([]string{testCase.rename}, native.compileOptions...)
			if err := compileTranslationUnitsWithOptions(selected, staging, []string{eventSource}, eventOptions, &result); err != nil {
				failWithLastCommand(t, &result, err)
			}
			if err := compileTranslationUnitsWithOptions(selected, staging, []string{fixture}, native.compileOptions, &result); err != nil {
				failWithLastCommand(t, &result, err)
			}
			objects := []string{strings.TrimSuffix(eventSource, ".c") + ".o", strings.TrimSuffix(fixture, ".c") + ".o"}
			objects = append(objects, native.linkObjects...)
			output := filepath.Join(staging, "init_failure_probe"+exeSuffix())
			tempExe, err := linkObjectsWithOptions(selected, staging, objects, native.linkOptions, output, &result)
			if err != nil {
				failWithLastCommand(t, &result, err)
			}
			if err := os.Rename(tempExe, output); err != nil {
				t.Fatal(err)
			}
			command := exec.Command(output)
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			finished := make(chan error, 1)
			go func() { finished <- command.Wait() }()
			select {
			case err := <-finished:
				if err == nil {
					t.Fatalf("initialization failure exited zero; stdout=%q", stdout.String())
				}
			case <-time.After(10 * time.Second):
				_ = command.Process.Kill()
				<-finished
				t.Fatal("initialization failure left the initializing thread waiting for readiness")
			}
			if got := strings.ReplaceAll(stderr.String(), "\r\n", "\n"); got != "[Runtime Error] event runtime initialization failed\n" {
				t.Fatalf("stderr = %q", got)
			}
			if stdout.Len() != 0 {
				t.Fatalf("stdout = %q, want empty", stdout.String())
			}
		})
	}
}

// TestEventBridgeParkRace drives many real Tasks through near-immediate
// native completions (File open, write, flush, and close requests and a
// one-nanosecond sleep) so completions routinely race each Task's park
// commit. Every Task must resume exactly once with its own result; a lost,
// duplicated, or cross-wired wake deadlocks, crashes, or changes the total.
func TestEventBridgeParkRace(t *testing.T) {
	requireBackend(t)
	source := `fun churn(h: Heap, index: Int32): Int32 | Error do
    name := String.interpolate(h, "race-{{index}}.txt")
    defer name.free(h)
    mut round: Int32 := 0
    while round < 20 do
        out := try File.open(name, FileMode.Write())
        wrote := try out.write("x".bytes())
        try out.flush()
        try out.close()
        Task.sleep(Duration.nanoseconds(1))
        round = round + 1
    end
    return index
end

fun run(): Int32 | Error do
    h: Heap := Heap()
    tasks: List<Task<Int32 | Error>> := List<Task<Int32 | Error>>(h)
    defer tasks.free(h)
    mut index: Int32 := 0
    while index < 64 do
        tasks.push(try spawn churn(h, index))
        index = index + 1
    end
    mut total: Int32 := 0
    for task in tasks do
        result := task.join()
        if result is Int32 then
            total = total + result
        end
    end
    return total
end

total := run()
if total is Int32 then
    print(total, "\n")
end
`
	runRuntimeFixture(t, runtimeFixture{name: "park-race", source: source, stdout: "2016\n", exitZero: true})
}
