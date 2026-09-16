package driver

// Pure-Go tests for RFC 0193's frontend seam: the bounded output buffer, the
// inspection budget diagnostic, and Clang qualification. They invoke no
// external frontend except where a test explicitly changes PATH.

import (
	"context"
	"os/exec"
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

func TestResolveClangRequiresClang18(t *testing.T) {
	// An empty search path must fail with the exact configuration diagnostic.
	t.Setenv("PATH", t.TempDir())
	if _, failure := resolveClang(); failure == nil || failure.Message != "automatic C imports require clang 18 or newer on PATH" {
		t.Fatalf("missing clang = %v, want the exact configuration error", failure)
	}
	if _, failure := resolveClang(); failure != nil && !strings.Contains(failure.Message, "clang 18 or newer") {
		t.Fatalf("unexpected clang diagnostic %q", failure.Message)
	}
}
