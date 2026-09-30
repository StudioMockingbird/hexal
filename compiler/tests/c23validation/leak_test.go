//go:build c23

package c23validation

// Leak checking is limited to fixtures with complete cleanup contracts:
// transforms release utf8proc buffers, while JSON and regex fixtures exercise
// their owning boundaries and failure cleanup. Other catalog fixtures
// intentionally retain program allocations.

import (
	"bytes"
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

// leakCheckedFixtures names programs whose selected path must leave no
// allocation behind.
var leakCheckedFixtures = map[string]bool{
	"normalize-runs": true,
	"casefold-runs":  true,
	"json-nested-ownership-and-partial-failure-leak-runs": true,
	"json-user-built-graph-ownership-runs":                true,
	"regex-unicode-byte-spans-and-captures-runs":          true,
	"regex-subject-over-pattern-limit-runs":               true,
	"regex-invalid-pattern-and-no-match-runs":             true,
	"regex-shared-pattern-concurrent-matches-run":         true,
	"regex-resource-limits-run":                           true,
}

// leakFlags instruments the binary with LeakSanitizer alone. LSan reports a
// leak on stderr and exits non-zero, so the fixture's own zero-exit
// expectation already fails on a leak; the stderr scan names the cause.
var leakFlags = []string{"-fsanitize=leak"}

// TestC23SuiteLeak rebuilds each leak-checked fixture with LeakSanitizer and
// requires it to run clean: exact stdout, no report, zero exit. LSan exists
// only on the Linux lane; the Windows lane has no leak instrumentation, so
// this track does not run there rather than failing on an unsupported flag.
// Fixture subtests run in parallel, bounded by -parallel; each program runs
// in its own temporary working directory.
func TestC23SuiteLeak(t *testing.T) {
	if testing.Short() {
		t.Skip("short mode: smoke fixtures only; run without -short for the exhaustive gate")
	}
	if runtime.GOOS == "windows" {
		t.Skip("LeakSanitizer does not exist for the Windows target")
	}
	buildRoot := t.TempDir()
	clang := clangToolchain(t)
	ran := 0
	for _, f := range fixtureCatalog {
		if !leakCheckedFixtures[f.name] || !f.appliesToHost() || f.expectation == nil || !f.expectation.zeroExit {
			continue
		}
		ran++
		t.Run(f.name, func(t *testing.T) {
			t.Parallel()
			result := f.resolve(t)
			exe := buildGeneratedCFlags(t, clang, result, buildRoot, leakFlags)
			ctx, cancel := context.WithTimeout(context.Background(), runProcessTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe)
			cmd.Dir = t.TempDir()
			var outBuf, errBuf bytes.Buffer
			cmd.Stdout = &outBuf
			cmd.Stderr = &errBuf
			err := cmd.Run()
			if ctx.Err() == context.DeadlineExceeded {
				t.Fatalf("generated program at %s did not exit within %s", exe, runProcessTimeout)
			}
			if strings.Contains(errBuf.String(), "LeakSanitizer") {
				t.Fatalf("LeakSanitizer reported a leak:\n%s", errBuf.String())
			}
			if err != nil {
				t.Fatalf("leak-checked run failed: %v; stdout=%q stderr=%q", err, outBuf.String(), errBuf.String())
			}
			out := strings.ReplaceAll(outBuf.String(), "\r\n", "\n")
			if out != f.expectation.exactStdout {
				t.Fatalf("stdout = %q, want %q", out, f.expectation.exactStdout)
			}
		})
	}
	if ran == 0 {
		t.Fatal("no leak-checked fixture ran; the catalog names changed")
	}
}
