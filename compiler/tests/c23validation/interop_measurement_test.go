//go:build c23

package c23validation

// Generated size, build and link time, runtime allocation statistics, and
// per-run latency for the program and entropy components. These are
// measurements, not pass/fail thresholds; the test asserts the artifacts exist
// and records the numbers in its log.

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
	"hexal/internal/driver"
)

// measurementTarget is this host's qualified target profile; the driver
// rejects a cross-host target, so measurements must run on the native lane.
func measurementTarget() compilerTypes.TargetProfileID {
	return hostTarget()
}

func generatedSize(t *testing.T, sources map[string]string, entrypoint string) int {
	t.Helper()
	result := compiler.Compile(sources, entrypoint, compiler.Project{Target: measurementTarget()})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("measurement program failed to compile: %v", result.Stderr)
	}
	total := 0
	for _, content := range result.Files {
		total += len(content)
	}
	return total
}

// TestProgramAndEntropyMeasurements records generated size, build time, run
// latency, and allocation statistics for the two component surfaces.
func TestProgramAndEntropyMeasurements(t *testing.T) {
	program := map[string]string{"app.hex": "import\n  Prog from std.program\nend\n" +
		"fun demo(): Bool | Error do\n" +
		"    let h: Heap = Heap()\n" +
		"    let path: String = try Prog.current_directory(h)\n" +
		"    let workers: Size = Prog.available_parallelism()\n" +
		"    let args = Prog.arguments()\n" +
		"    if args is Error then\n        return false\n    end\n" +
		"    return (path.length() > 0) and (workers > 0) and (args.length() > 0)\n" +
		"end\n" +
		"let outcome: Bool | Error = demo()\n" +
		"if outcome is Error then\n    return 1\nend\n" +
		"print(outcome)\n"}
	entropy := map[string]string{"app.hex": "import\n  Ent from std.entropy\nend\n" +
		"fun demo(): Bool | Error do\n" +
		"    let h: Heap = Heap()\n" +
		"    let p: Ptr<mut Byte> = h.allocate<Byte>(8)\n" +
		"    unsafe do\n" +
		"        let view: Slice<mut Byte> = Slice<mut Byte>.from_pointer(p, 8)\n" +
		"        try Ent.fill(view)\n" +
		"    end\n" +
		"    return true\n" +
		"end\n" +
		"let outcome: Bool | Error = demo()\n" +
		"if outcome is Error then\n    return 1\nend\n" +
		"print(outcome)\n"}

	for _, component := range []struct {
		name    string
		sources map[string]string
	}{{"program", program}, {"entropy", entropy}} {
		t.Run(component.name, func(t *testing.T) {
			size := generatedSize(t, component.sources, "app.hex")
			t.Logf("%s: generated size = %d bytes", component.name, size)
			if size == 0 {
				t.Fatal("no generated artifacts were produced")
			}

			root := t.TempDir()
			if err := os.WriteFile(filepath.Join(root, "app.hex"), []byte(component.sources["app.hex"]), 0o644); err != nil {
				t.Fatal(err)
			}
			started := time.Now()
			result, err := driver.Build(driver.BuildOptions{
				Root:         root,
				Entrypoint:   "app.hex",
				CompilerPath: clangToolchain(t).Command[0],
				Target:       measurementTarget(),
			})
			if err != nil {
				t.Fatalf("measurement build failed: %v", err)
			}
			t.Logf("%s: build and link time = %s", component.name, time.Since(started))

			// Run latency over repeated invocations.
			const runs = 10
			started = time.Now()
			for run := 0; run < runs; run++ {
				command := exec.Command(result.Executable)
				command.Env = append(os.Environ(), "MIMALLOC_SHOW_STATS=1")
				var stdout, stderr bytes.Buffer
				command.Stdout = &stdout
				command.Stderr = &stderr
				if err := command.Run(); err != nil {
					t.Fatalf("measured run failed: %v\n%s", err, stderr.String())
				}
				if got := strings.TrimSpace(stdout.String()); got != "true" {
					t.Fatalf("measured run output = %q, want true", got)
				}
				if run == 0 {
					if stats := allocationStats(stderr.String()); stats != "" {
						t.Logf("%s: runtime allocation stats: %s", component.name, stats)
					} else {
						t.Logf("%s: runtime allocation stats unavailable (mimalloc printed none)", component.name)
					}
				}
			}
			t.Logf("%s: %d runs in %s (%s per run)", component.name, runs, time.Since(started), time.Since(started)/runs)
		})
	}
}

// allocationStats extracts the mimalloc statistics block printed when
// MIMALLOC_SHOW_STATS is set, or an empty string when mimalloc printed none.
func allocationStats(stderr string) string {
	lines := strings.Split(stderr, "\n")
	start := -1
	for index, line := range lines {
		if strings.Contains(line, "peak") && strings.Contains(line, "total") {
			start = index
			break
		}
	}
	if start < 0 {
		return ""
	}
	end := start + 16
	if end > len(lines) {
		end = len(lines)
	}
	collected := make([]string, 0, end-start)
	for _, line := range lines[start:end] {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			collected = append(collected, trimmed)
		}
	}
	return strings.Join(collected, " | ")
}
