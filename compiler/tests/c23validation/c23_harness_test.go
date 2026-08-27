//go:build c23

package c23validation

// C23 harness: the compile/run/trap execution engine shared by every fixture
// in catalog_test.go. It runs generated C under all three discovered
// toolchains, caches each distinct generated artifact set per
// toolchain/target/flags so the suite invokes each compiler once per
// distinct output, and bounds every process it runs with a hard timeout so a
// fixture that reaches a known runtime hang fails cleanly instead of
// blocking the whole run.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"hexal/compiler"
)

// assertCompiles requires the source to compile and returns the result.
// Private copy of the integration package's helper: this package must remain
// independent of the active suite.
func assertCompiles(t *testing.T, source string) compiler.CompilationResult {
	t.Helper()
	result := compiler.Compile(map[string]string{"app.hex": source}, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("expected success; got %d diagnostic(s):\n%s\n--- source ---\n%s", len(result.Stderr), strings.Join(result.Stderr, "\n"), source)
	}
	return result
}

// runProcessTimeout bounds every generated binary this suite executes. The
// scheduler is separately known to hang the root Task forever on every
// concurrency program (docs/status.md, Unowned); this bound
// turns that into a clean, fast test failure instead of blocking the whole
// tagged run.
const runProcessTimeout = 10 * time.Second

// buildProcessTimeout bounds one compiler invocation (compiling and linking
// one artifact set under one toolchain), distinct from runProcessTimeout
// above: that one bounds running the already-built binary. A hung gcc,
// clang, zig, or linker process would otherwise block its exec.Command call
// forever, and the only thing that would eventually stop it is go test's
// own outer -timeout, which discards every already-passing result along
// with it.
const buildProcessTimeout = 2 * time.Minute

// runProcess runs path with a hard timeout, returning stdout and stderr
// captured separately (never combined: Tier 2 and Tier 3 both depend on
// telling the two apart) and whether it exited zero.
func runProcess(t *testing.T, path string) (stdout, stderr string, exitedZero bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runProcessTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path)
	var outBuf, errBuf bytes.Buffer
	command.Stdout = &outBuf
	command.Stderr = &errBuf
	err := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("generated program at %s did not exit within %s (a known scheduler defect hangs every concurrency program at startup; see docs/status.md)", path, runProcessTimeout)
	}
	exitedZero = err == nil
	return outBuf.String(), errBuf.String(), exitedZero
}

// canonicalArtifactHash is the cache key: SHA-256 over artifact names sorted
// bytewise, each filename length, filename bytes, content length, and
// content bytes encoded in sequence. Distinct sources routinely collapse
// onto identical generated C, and invoking a compiler is three orders of
// magnitude slower than the in-process checks it would otherwise duplicate.
func canonicalArtifactHash(files map[string]string) string {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	hasher := sha256.New()
	var lengthBuffer [8]byte
	for _, name := range names {
		binary.BigEndian.PutUint64(lengthBuffer[:], uint64(len(name)))
		hasher.Write(lengthBuffer[:])
		hasher.Write([]byte(name))
		content := files[name]
		binary.BigEndian.PutUint64(lengthBuffer[:], uint64(len(content)))
		hasher.Write(lengthBuffer[:])
		hasher.Write([]byte(content))
	}
	return hex.EncodeToString(hasher.Sum(nil))
}

// compileCacheKey identifies one build: a distinct generated artifact set
// compiled by one toolchain under one flag set, scoped to the buildRoot that
// owns it. The target is folded into the toolchain's own cache entry via its
// Command+Version, since this suite never varies target independent of
// toolchain (host-only, see the RFC). buildRoot is part of the key -- not
// just a place the result happens to live -- because compileCache is a
// package-level map shared by every top-level test in this binary, while
// each top-level test's buildRoot is its own t.TempDir(), deleted when that
// top-level test returns: without buildRoot in the key, a cache hit from one
// top-level test could hand back an executable path another top-level test
// already deleted.
type compileCacheKey struct {
	artifactHash string
	toolchain    string
	flags        string
	buildRoot    string
}

// buildResult is the outcome of one build: either an executable path or the
// error the toolchain rejected it with.
type buildResult struct {
	exe string
	err error
}

// compileCacheEntry lets exactly one caller per key perform the build while
// every other caller for that same key -- including the one that
// performed it -- waits on and then reads the same published result.
// sync.Once.Do's own happens-before guarantee makes the plain field read
// below safe with no further locking: check-then-act on a bare map (lock,
// check, unlock, build, lock, write) would let two callers that miss the
// cache for the same key build concurrently into the same directory and the
// same output path.
type compileCacheEntry struct {
	once   sync.Once
	result buildResult
}

var (
	compileCacheMu sync.Mutex
	compileCache   = map[compileCacheKey]*compileCacheEntry{}
)

// warningFlags is the complete Tier 1 warning-suppression list. Every entry
// is labelled Debt (names the open gap it tolerates, removed when that gap
// closes) or Principle (names the language guarantee it protects,
// permanent); an unlabelled suppression does not belong here.
var warningFlags = []string{
	"-Wno-unused-function",         // Debt: the generator emits whole helper families (equality, print, union, heap, io) demand-independently; narrowing this is that gap's job, not this suite's.
	"-Wno-unused-variable",         // Debt: same generator over-emission gap as above, for module-scope helper state rather than functions.
	"-Wno-unused-parameter",        // Debt: same generator over-emission gap, for helper parameters unused by a given instantiation.
	"-Wno-unused-but-set-variable", // Debt: same generator over-emission gap, for a helper local written but never read by a given instantiation.
}

// buildGeneratedC materializes every artifact under a fresh subdirectory of
// buildRoot and compiles every .c translation unit with the harness warning
// policy under toolchain, returning the executable path. The build is
// cached by canonical artifact hash, toolchain, flag set, and buildRoot, so
// the same generated output is never compiled twice by the same toolchain
// under the same buildRoot. buildRoot is the calling top-level test's own
// t.TempDir(), threaded down rather than requested here again: an
// executable this cache hands out must outlive the specific subtest that
// first built it but not the top-level test that owns buildRoot, since Go
// deletes a t.TempDir() when its owning top-level test returns -- buildRoot
// is part of the cache key specifically so a cache hit can never outlive it.
func buildGeneratedC(t *testing.T, tc toolchain, result compiler.CompilationResult, buildRoot string) string {
	t.Helper()
	flags := []string{"-std=c23", "-Wall", "-Wextra", "-Werror"}
	flags = append(flags, warningFlags...)
	key := compileCacheKey{artifactHash: canonicalArtifactHash(result.Files), toolchain: tc.Name, flags: strings.Join(flags, " "), buildRoot: buildRoot}

	compileCacheMu.Lock()
	entry, ok := compileCache[key]
	if !ok {
		entry = &compileCacheEntry{}
		compileCache[key] = entry
	}
	compileCacheMu.Unlock()

	entry.once.Do(func() {
		entry.result = doBuild(tc, result.Files, flags, buildRoot, key.artifactHash, t.Name())
	})
	if entry.result.err != nil {
		t.Fatalf("%s rejected generated C: %v", tc.Name, entry.result.err)
	}
	return entry.result.exe
}

// doBuild materializes one artifact set under a fresh subdirectory of
// buildRoot and compiles every .c translation unit with the harness warning
// policy under tc. It takes no *testing.T: compileCacheEntry.once may run it
// from any one of several waiting subtests' goroutines, and only
// buildGeneratedC's own caller reports failure, through its own t.
// subtestName is t.Name() from whichever caller happened to run the build --
// identifying, not necessarily the specific caller that later reads the
// cached result, an accepted imprecision of any dedup cache.
func doBuild(tc toolchain, files map[string]string, flags []string, buildRoot, artifactHash, subtestName string) buildResult {
	dir := filepath.Join(buildRoot, artifactHash, tc.Name)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return buildResult{err: err}
	}
	for name, content := range files {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return buildResult{err: err}
		}
		if err := os.WriteFile(path, []byte(content), 0644); err != nil {
			return buildResult{err: err}
		}
	}
	exe := filepath.Join(dir, "hexal.exe")
	args := append([]string{}, tc.Command[1:]...)
	args = append(args, flags...)
	args = append(args, "-I", dir)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	// The scheduler and blocking-pool runtime need a real thread library on
	// POSIX targets; the Windows primitives this suite's own host targets
	// need nothing extra (SRWLOCK/CONDITION_VARIABLE/_beginthreadex are
	// libc/kernel32, not a separate link dependency).
	if runtime.GOOS != "windows" && strings.Contains(strings.Join(names, " "), "concurrency.c") {
		args = append(args, "-lpthread")
	}
	args = append(args, "-o", exe)

	ctx, cancel := context.WithTimeout(context.Background(), buildProcessTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, tc.Command[0], args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return buildResult{err: fmt.Errorf("%s did not finish within %s (artifact %s)", subtestName, buildProcessTimeout, artifactHash)}
	}
	if err != nil {
		return buildResult{err: fmt.Errorf("%v\n%s", err, output)}
	}
	return buildResult{exe: exe}
}

// compileGeneratedC (Tier 1) writes every generated artifact and compiles
// every .c translation unit with -std=c23 -Wall -Wextra -Werror under every
// discovered toolchain: any warning or error fails the test. A program
// accepted by one toolchain and rejected by another is a real divergence and
// fails the fixture rather than being resolved by preferring one compiler's
// judgment.
func compileGeneratedC(t *testing.T, result compiler.CompilationResult, buildRoot string) {
	t.Helper()
	gcc, clang, zig := discoverAllToolchains(t)
	for _, tc := range []toolchain{gcc, clang, zig} {
		t.Run(tc.Name, func(t *testing.T) {
			buildGeneratedC(t, tc, result, buildRoot)
		})
	}
}

// runGeneratedC (Tier 2) compiles under every toolchain, runs the resulting
// binary, and returns its normalized stdout for the caller to assert
// exactly. Stdout is normalized here because a C runtime in text mode on
// Windows translates '\n' to '\r\n' on the way through a pipe; a fixture
// asserts the bytes the program wrote, not the platform's line-ending
// habits. Stderr must be empty and the process must exit zero, or the
// fixture fails -- a run that produced correct stdout while also writing to
// stderr or trapping is not a passing Tier 2 result.
func runGeneratedC(t *testing.T, result compiler.CompilationResult, buildRoot string) string {
	t.Helper()
	gcc, clang, zig := discoverAllToolchains(t)
	var normalized string
	first := true
	for _, tc := range []toolchain{gcc, clang, zig} {
		t.Run(tc.Name, func(t *testing.T) {
			exe := buildGeneratedC(t, tc, result, buildRoot)
			stdout, stderr, exitedZero := runProcess(t, exe)
			if !exitedZero {
				t.Fatalf("generated program exited non-zero; stdout=%q stderr=%q", stdout, stderr)
			}
			if stderr != "" {
				t.Fatalf("generated program wrote to stderr: %q", stderr)
			}
			out := strings.ReplaceAll(stdout, "\r\n", "\n")
			if first {
				normalized = out
				first = false
				return
			}
			if out != normalized {
				t.Fatalf("output diverges across toolchains: %s produced %q, an earlier toolchain produced %q", tc.Name, out, normalized)
			}
		})
	}
	return normalized
}

// trapGeneratedC (Tier 3) compiles under every toolchain and runs a program
// that must terminate by a runtime trap: a successful exit fails the test,
// and stderr must contain requiredSubstring, the fixture's exact expected
// "[Runtime Error] ..." text. Stdout up to the trap point is not
// constrained by this helper; callers with output expectations before the
// trap assert it themselves.
func trapGeneratedC(t *testing.T, result compiler.CompilationResult, buildRoot, requiredSubstring string) {
	t.Helper()
	gcc, clang, zig := discoverAllToolchains(t)
	for _, tc := range []toolchain{gcc, clang, zig} {
		t.Run(tc.Name, func(t *testing.T) {
			exe := buildGeneratedC(t, tc, result, buildRoot)
			_, stderr, exitedZero := runProcess(t, exe)
			if exitedZero {
				t.Fatalf("program must trap but exited successfully")
			}
			if !strings.Contains(stderr, requiredSubstring) {
				t.Fatalf("program's stderr = %q, want it to contain %q", stderr, requiredSubstring)
			}
		})
	}
}
