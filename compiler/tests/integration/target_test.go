package integration

import (
	"reflect"
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

// An explicit qualified profile emits only the selected platform
// implementation: inactive POSIX branches and headers are absent from the
// generated runtime components, while Windows paths remain.
func TestExplicitProfileOmitsPosixBranches(t *testing.T) {
	sources := map[string]string{"app.hex": "fun worker(): Int32 do\n    return 1\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn worker()\n    return task.join()\nend\noutcome: Int32 | Error := run()\nvalue: Int32 := match outcome is\n| Int32 then outcome\n| Error then 0\nend\nprint(value)\n"}
	result := compiler.Compile(sources, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("explicit-profile compile failed: %v", result.Stderr)
	}
	concurrencyC := result.Files["hexal/concurrency.c"]
	concurrencyH := result.Files["hexal/concurrency.h"]
	ioC := result.Files["hexal/io.c"]
	if concurrencyC == "" || concurrencyH == "" || ioC == "" {
		t.Fatalf("missing runtime artifacts: %v", keysOf(result.Files))
	}
	for _, marker := range []string{
		"pthread_create", "ucontext_t", "sys/mman.h", "sigaction",
		"mmap(", "munmap(", "#include <unistd.h>", "#include <errno.h>",
		"SSIZE_MAX", "#include <pthread.h>", "_GNU_SOURCE", "PROT_NONE",
		"#include <fcntl.h>", "#include <limits.h>",
	} {
		if strings.Contains(concurrencyC, marker) || strings.Contains(concurrencyH, marker) || strings.Contains(ioC, marker) {
			t.Fatalf("explicit-profile output contains inactive POSIX marker %q", marker)
		}
	}
	for _, marker := range []string{
		"CreateFiberEx", "ReadFile", "#include <windows.h>",
	} {
		if !strings.Contains(concurrencyC+concurrencyH+ioC, marker) {
			t.Fatalf("explicit-profile output lacks Windows marker %q", marker)
		}
	}
}

// The Linux profile selects the POSIX runtime path and the widened POSIX
// entrypoint. The generator's platform blocks keep their host-neutral guards,
// so a Windows branch that remains in the text stays behind `#if defined(_WIN32)`
// and is never compiled on Linux.
func TestExplicitLinuxProfileSelectsPosixEntrypoint(t *testing.T) {
	concurrent := map[string]string{"app.hex": "fun worker(): Int32 do\n    return 1\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn worker()\n    return task.join()\nend\noutcome: Int32 | Error := run()\nvalue: Int32 := match outcome is\n| Int32 then outcome\n| Error then 0\nend\nprint(value)\n"}
	result := compiler.Compile(concurrent, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("explicit Linux-profile compile failed: %v", result.Stderr)
	}
	combined := result.Files["hexal/concurrency.c"] + result.Files["hexal/concurrency.h"] + result.Files["hexal/io.c"]
	for _, marker := range []string{"uv_thread_create", "sys/mman.h", "ucontext_t"} {
		if !strings.Contains(combined, marker) {
			t.Fatalf("explicit Linux-profile output lacks POSIX marker %q", marker)
		}
	}

	arguments := map[string]string{"app.hex": "import\n  Prog from std.program\nend\nfun demo(): Bool | Error do\n    args := Prog.arguments()\n    if args is Error then\n        return false\n    end\n    return true\nend\noutcome: Bool | Error := demo()\nif outcome is Error then\n    return 1\nend\nprint(outcome)\n"}
	linux := compiler.Compile(arguments, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU})
	if linux.ExitCode != compiler.ExitSuccess {
		t.Fatalf("argument-demand Linux-profile compile failed: %v", linux.Stderr)
	}
	if root := linux.Files["modules/app.c"]; !strings.Contains(root, "int main(int argc, char **argv)") {
		t.Fatalf("explicit Linux-profile root lacks the widened POSIX entrypoint")
	}
	windows := compiler.Compile(arguments, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if windows.ExitCode != compiler.ExitSuccess {
		t.Fatalf("argument-demand Windows-profile compile failed: %v", windows.Stderr)
	}
	if root := windows.Files["modules/app.c"]; !strings.Contains(root, "int main(void)") {
		t.Fatalf("explicit Windows-profile root lacks the fixed Windows entrypoint")
	}
}

// The host-neutral zero value retains both platform paths selected at
// C-compile time; an explicit profile never leaks into it.
func TestHostNeutralRetainsBothBranches(t *testing.T) {
	sources := map[string]string{"app.hex": "fun worker(): Int32 do\n    return 1\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn worker()\n    return task.join()\nend\noutcome: Int32 | Error := run()\nvalue: Int32 := match outcome is\n| Int32 then outcome\n| Error then 0\nend\nprint(value)\n"}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("host-neutral compile failed: %v", result.Stderr)
	}
	combined := result.Files["hexal/concurrency.c"] + result.Files["hexal/concurrency.h"] + result.Files["hexal/io.c"]
	for _, marker := range []string{
		"#else", "uv_thread_create", "ucontext_t", "CreateFiberEx",
	} {
		if !strings.Contains(combined, marker) {
			t.Fatalf("host-neutral output lacks dual-platform marker %q", marker)
		}
	}
}

// Equivalent explicit-profile inputs produce byte-identical artifacts.
func TestExplicitProfileDeterministic(t *testing.T) {
	sources := map[string]string{"app.hex": "print(1)\n"}
	project := compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU}
	first := compiler.Compile(sources, "app.hex", project)
	second := compiler.Compile(sources, "app.hex", project)
	if first.ExitCode != compiler.ExitSuccess || second.ExitCode != compiler.ExitSuccess {
		t.Fatalf("explicit-profile compile failed: %v %v", first.Stderr, second.Stderr)
	}
	if !reflect.DeepEqual(first.Files, second.Files) {
		t.Fatalf("explicit-profile artifacts differ between identical compilations")
	}
}

func keysOf(files map[string]string) []string {
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	return keys
}
