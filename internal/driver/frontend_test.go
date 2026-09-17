package driver

// Pure-Go tests for the frontend seam: the bounded output buffer, the
// inspection budget diagnostic, and Clang qualification. They invoke no
// external frontend except where a test explicitly changes PATH.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBoundedBufferOverflow(t *testing.T) {
	buffer := newBoundedBuffer(8)
	if _, err := buffer.Write([]byte("12345")); err != nil {
		t.Fatal(err)
	}
	if buffer.overflow {
		t.Fatal("a write under the limit must not overflow")
	}
	if _, err := buffer.Write([]byte("67890")); err != nil {
		t.Fatal(err)
	}
	if !buffer.overflow {
		t.Fatal("a write past the limit must set overflow")
	}
	if buffer.String() != "12345678" {
		t.Fatalf("buffer = %q, want the first eight bytes only", buffer.String())
	}
}

func TestInspectionBudgetMessageIsExact(t *testing.T) {
	const want = "C header inspection exceeded the initial automatic-binding budget; use a smaller wrapper header or a handwritten binding"
	if inspectionBudgetMessage != want {
		t.Fatalf("budget message = %q, want %q", inspectionBudgetMessage, want)
	}
}

func TestInspectionDeadlineFailsClosed(t *testing.T) {
	exe, err := exec.LookPath("cmd")
	if err != nil {
		t.Skip("a deadline probe needs a shell on PATH")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var result BuildResult
	_, failure := runExternalBounded(exe, "", nil, []string{"PROBE"}, ctx, &result, StageCompile, []string{"/c", "exit 0"})
	if failure == nil || failure.Message != inspectionBudgetMessage {
		t.Fatalf("expired deadline = %v, want the budget diagnostic", failure)
	}
	if failure.Stage != StageCompile {
		t.Fatalf("deadline stage = %v, want the C-compilation stage", failure.Stage)
	}
}

func TestResolveBackendRequiresAnExecutablePath(t *testing.T) {
	if _, err := resolveBackend(""); err == nil || err.Error() != "C backend is required; pass -cc <path>" {
		t.Fatalf("empty path = %v, want the exact required diagnostic", err)
	}
	if _, err := resolveBackend(filepath.Join(t.TempDir(), "missing")); err == nil || !strings.Contains(err.Error(), "is not an executable file") {
		t.Fatalf("missing path = %v, want a non-executable diagnostic", err)
	}
	if _, err := resolveBackend(t.TempDir()); err == nil || !strings.Contains(err.Error(), "is not an executable file") {
		t.Fatalf("directory path = %v, want a non-executable diagnostic", err)
	}
}

// A relative -cc path resolves once against the invocation working directory,
// which the diagnostic names as an absolute path.
func TestResolveBackendResolvesRelativePathsAgainstTheWorkingDirectory(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe-tool"), []byte("#!/bin/sh\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	_, err := resolveBackend("probe-tool")
	if err == nil || !strings.Contains(err.Error(), filepath.Join(dir, "probe-tool")) {
		t.Fatalf("relative path = %v, want it resolved against the working directory", err)
	}
}
