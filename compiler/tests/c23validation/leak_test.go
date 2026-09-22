//go:build c23

package c23validation

// A dedicated LeakSanitizer run for the Tier 3 text transforms. Every other
// fixture deliberately leaves bindings unfreed, so a program-wide leak check
// would report their intentional leaks; only the transforms promise to release
// the malloc buffer utf8proc_map returns, so only they are leak-checked.
//
// The transforms' failure path is unreachable from any checker-accepted
// program -- text is always validated UTF-8, so utf8proc_map cannot report
// malformed input, and allocator exhaustion is not injectable here -- so this
// run covers the success path. See docs/status.md for that disposition.

import (
	"bytes"
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// leakCheckedFixtures names the transforms whose success path must leave no
// allocation behind.
var leakCheckedFixtures = map[string]bool{
	"normalize-runs": true,
	"casefold-runs":  true,
}

// leakFlags instruments the binary with LeakSanitizer alone. LSan reports a
// leak on stderr and exits non-zero, so the fixture's own zero-exit
// expectation already fails on a leak; the stderr scan names the cause.
var leakFlags = []string{"-fsanitize=leak"}

// TestC23SuiteLeak rebuilds each leak-checked fixture with LeakSanitizer and
// requires it to run clean: exact stdout, no report, zero exit.
func TestC23SuiteLeak(t *testing.T) {
	buildRoot := t.TempDir()
	clang := clangToolchain(t)
	ran := 0
	for _, f := range fixtureCatalog {
		if !leakCheckedFixtures[f.name] || !f.appliesToHost() || f.expectation == nil || !f.expectation.zeroExit {
			continue
		}
		ran++
		t.Run(f.name, func(t *testing.T) {
			result := f.resolve(t)
			exe := buildGeneratedCFlags(t, clang, result, buildRoot, leakFlags)
			ctx, cancel := context.WithTimeout(context.Background(), runProcessTimeout)
			defer cancel()
			cmd := exec.CommandContext(ctx, exe)
			cmd.Dir = filepath.Dir(exe)
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
