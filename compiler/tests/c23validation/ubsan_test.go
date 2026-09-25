//go:build c23

package c23validation

// Track 4: UBSan coverage. Every runnable (has a process expectation)
// fixture in fixtureCatalog is rebuilt with -fsanitize=undefined
// -fno-sanitize-recover=all under the one installed Clang, then run again: its
// ordinary expectation (exact stdout, or the exact "[Runtime Error] ..." trap
// substring) must still hold, and no UBSan report may appear. Clang is the
// sole external compiler this release supports, so there is no capability
// branch: a Clang that cannot link and run a sanitizer-instrumented binary is
// a hard release-gate failure, not a skip.
//
// clang's compiler-rt UBSan runtime honors UBSAN_OPTIONS=log_path=... (a
// relative path, run with the executable's own directory as the process's
// working directory), which diverts a report into its own file so it never
// mixes with the exact stderr text a trap fixture already asserts.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// ubsanFlags is the decided flag set: report and abort on the first
// violation, nothing recovered. -fno-sanitize=function drops just the
// indirect-call type-mismatch check: Hexal's Task entry points and libuv
// work-queue callbacks are intentionally type-erased (stored as a generic
// function pointer and cast back to their real signature before calling),
// the exact pattern that check exists to flag. The ignorelist further
// excludes reports attributed to platform headers this project does not own
// (see testdata/ubsan-ignorelist.txt); every other undefined-behavior
// category, Hexal's own generated C included, is still checked.
var ubsanFlags = func() []string {
	_, file, _, _ := runtime.Caller(0)
	ignorelist := filepath.Join(filepath.Dir(file), "testdata", "ubsan-ignorelist.txt")
	return []string{
		"-fsanitize=undefined",
		"-fno-sanitize=function",
		"-fno-sanitize-recover=all",
		"-fsanitize-ignorelist=" + ignorelist,
	}
}()

// probeUBSanCapability compiles and runs a trivial, well-defined program
// under ubsanFlags for the resolved Clang. It confirms the compiler can
// produce and execute a sanitizer-instrumented binary at all (e.g. that its
// sanitizer runtime library is actually installed) -- a question distinct
// from whether any given fixture's own run reports undefined behavior.
func probeUBSanCapability(tc toolchain, root string) error {
	dir := filepath.Join(root, "probe-"+tc.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	src := filepath.Join(dir, "probe.c")
	if err := os.WriteFile(src, []byte("int main(void) { return 0; }\n"), 0644); err != nil {
		return err
	}
	exe := filepath.Join(dir, "probe")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	args := []string{"-std=c23", "--target=" + tc.DefaultTarget}
	args = append(args, ubsanFlags...)
	args = append(args, src, "-o", exe)
	ctx, cancel := context.WithTimeout(context.Background(), buildProcessTimeout)
	defer cancel()
	if out, err := exec.CommandContext(ctx, tc.Command[0], args...).CombinedOutput(); err != nil {
		return fmt.Errorf("compile: %v\n%s", err, out)
	}
	cmd := exec.Command(exe)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("run: %v\n%s", err, out)
	}
	return nil
}

// requireUBSanCapable resolves the one Clang and proves it can compile, link,
// and run a sanitizer-instrumented binary. A compiler that cannot is a hard
// failure: the UBSan tier is mandatory, so the release gate fails rather than
// skips.
func requireUBSanCapable(t *testing.T, root string) toolchain {
	t.Helper()
	tc := clangToolchain(t)
	if err := probeUBSanCapability(tc, root); err != nil {
		t.Fatalf("clang cannot compile and run a UBSan-instrumented binary: %v", err)
	}
	return tc
}

// runUBSanProcess runs path with its own directory as the working directory
// (see the package comment on why log_path must be relative) and
// UBSAN_OPTIONS pointed at a log file there, returning stdout, stderr, exit
// status, and the sanitizer report text if one fired -- from the log file
// clang's compiler-rt honors, or from stderr when the runtime wrote there
// instead.
func runUBSanProcess(t *testing.T, path string) (stdout, stderr string, exitedZero bool, report string) {
	t.Helper()
	dir := filepath.Dir(path)
	ctx, cancel := context.WithTimeout(context.Background(), runProcessTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "UBSAN_OPTIONS=log_path=ubsan_report")
	var outBuf, errBuf bytes.Buffer
	cmd.Stdout = &outBuf
	cmd.Stderr = &errBuf
	err := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("generated program at %s did not exit within %s", path, runProcessTimeout)
	}
	stdout, stderr, exitedZero = outBuf.String(), errBuf.String(), err == nil
	matches, _ := filepath.Glob(filepath.Join(dir, "ubsan_report*"))
	for _, match := range matches {
		content, readErr := os.ReadFile(match)
		if readErr == nil && len(content) > 0 {
			return stdout, stderr, exitedZero, string(content)
		}
	}
	if strings.Contains(stderr, "UndefinedBehaviorSanitizer:") {
		return stdout, stderr, exitedZero, stderr
	}
	return stdout, stderr, exitedZero, ""
}

// TestC23SuiteUBSan reruns every runnable fixture under UBSan on the one
// Clang. A fixture with no process expectation is compile-only (never
// executed at Tier 1 either) and has nothing for a runtime sanitizer to
// check, so it is skipped here exactly as it is never run by TestC23Suite.
// Clang ships no UBSan runtime for the MinGW ABI; Windows debug uses trap
// mode (mode_c23 in the driver package), so this diagnostic track is
// Linux-only.
func TestC23SuiteUBSan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("UBSan runtime does not exist for the Windows target")
	}
	buildRoot := t.TempDir()
	clang := requireUBSanCapable(t, buildRoot)
	for _, f := range fixtureCatalog {
		if !f.appliesToHost() || f.expectation == nil {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			result := f.resolve(t)
			exe := buildGeneratedCFlags(t, clang, result, buildRoot, ubsanFlags)
			stdout, stderr, exitedZero, report := runUBSanProcess(t, exe)
			if report != "" {
				t.Fatalf("UndefinedBehaviorSanitizer reported undefined behavior:\n%s", report)
			}
			if f.expectation.zeroExit {
				if !exitedZero {
					t.Fatalf("generated program exited non-zero; stdout=%q stderr=%q", stdout, stderr)
				}
				if stderr != "" {
					t.Fatalf("generated program wrote to stderr: %q", stderr)
				}
				out := strings.ReplaceAll(stdout, "\r\n", "\n")
				if out != f.expectation.exactStdout {
					t.Fatalf("stdout = %q, want %q", out, f.expectation.exactStdout)
				}
				return
			}
			if exitedZero {
				t.Fatalf("program must trap but exited successfully")
			}
			if !strings.Contains(stderr, f.expectation.requiredStderrSubstring) {
				t.Fatalf("program's stderr = %q, want it to contain %q", stderr, f.expectation.requiredStderrSubstring)
			}
		})
	}
}
