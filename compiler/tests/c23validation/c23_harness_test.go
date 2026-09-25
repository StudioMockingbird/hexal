//go:build c23

package c23validation

// C23 harness: the compile/run/trap execution engine shared by every fixture
// in catalog_test.go. It runs generated C under the one discovered Clang,
// caches each distinct generated artifact set per toolchain/flags so the
// suite invokes the compiler once per distinct output, and bounds every
// process it runs with a hard timeout as a general safety boundary so a
// wedged binary fails fast instead of blocking the whole run.

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

// runProcessTimeout bounds every generated binary this suite executes. It is
// a general test-harness safety boundary, never expected scheduler behavior:
// a binary that exceeds it has failed.
const runProcessTimeout = 10 * time.Second

// buildProcessTimeout bounds one compiler invocation (compiling and linking
// one artifact set), distinct from runProcessTimeout above: that one bounds
// running the already-built binary. A hung clang or linker process would
// otherwise block its exec.Command call forever, and the only thing that
// would eventually stop it is go test's own outer -timeout, which discards
// every already-passing result along with it.
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
		t.Fatalf("generated program at %s did not exit within %s", path, runProcessTimeout)
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
// toolchain (host-only). buildRoot is part of the key -- not
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
//
// Equality, print, and union widening/truthiness are demand-driven: a
// helper is emitted only for a type and operation the checked program
// actually exercises (see equality.go's addComparedType, unions.go's
// markTruthy, and print.go's needsNested). Heap, Stash, and IO were
// already demand-driven before this was checked. An unsuppressed run of
// the complete snippet catalog confirms no remaining helper-family
// over-emission causes any of these four warnings; every one it still
// reports is a top-level workbench-snippet pattern with nothing to do
// with generator emission, e.g. a `demo()` function or a `code := f()`
// binding a snippet declares purely to demonstrate that construct
// compiles, never calling or reading it again on purpose. None of these
// four can be removed without either rewriting every such snippet to
// consume its own demonstration values (defeating their purpose as
// minimal examples) or generating a `(void)` discard for every checked
// binding and parameter (a correctness-neutral but pervasive codegen
// change well outside this gap's scope), so each stays Debt against that
// different, out-of-scope cause rather than the helper-family gap this
// comment used to (inaccurately) name for all four.
var warningFlags = []string{
	"-Wno-unused-function",         // Debt: workbench snippets that declare a function purely to demonstrate its syntax compiles, never calling it.
	"-Wno-unused-variable",         // Debt: workbench snippets that bind a demonstration value purely to show it type-checks, never reading it again.
	"-Wno-unused-parameter",        // Debt: same workbench-snippet pattern as unused-variable, for a declared function's own unused parameter.
	"-Wno-unused-but-set-variable", // Debt: same workbench-snippet pattern, for a mutable binding written (e.g. by a loop) but never read afterward.
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
	return buildGeneratedCFlags(t, tc, result, buildRoot, nil)
}

// buildGeneratedCFlags is buildGeneratedC with additional flags appended
// after the harness's own baseline (e.g. UBSan's sanitizer flags), sharing
// the identical cache so a distinct flag set naturally becomes a distinct
// cache entry rather than colliding with the plain build.
func buildGeneratedCFlags(t *testing.T, tc toolchain, result compiler.CompilationResult, buildRoot string, extra []string) string {
	t.Helper()
	flags := []string{"-std=c23", "-Wall", "-Wextra", "-Werror"}
	flags = append(flags, warningFlags...)
	flags = append(flags, extra...)
	key := compileCacheKey{artifactHash: canonicalArtifactHash(result.Files), toolchain: tc.Name, flags: strings.Join(flags, " "), buildRoot: buildRoot}

	compileCacheMu.Lock()
	entry, ok := compileCache[key]
	if !ok {
		entry = &compileCacheEntry{}
		compileCache[key] = entry
	}
	compileCacheMu.Unlock()

	entry.once.Do(func() {
		entry.result = doBuild(tc, result.Files, result.Dependencies, flags, buildRoot, key.artifactHash, t.Name())
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
func doBuild(tc toolchain, files map[string]string, dependencies []compiler.RuntimeDependency, flags []string, buildRoot, artifactHash, subtestName string) buildResult {
	var native *dependencyBuild
	if len(dependencies) > 0 {
		native = buildDependencies(dependencies, buildRoot)
		if native.err != nil {
			return buildResult{err: native.err}
		}
	}
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
	// The generated tree already contains a hexal/ directory of artifacts, so
	// the executable is named distinctly; Windows needs the .exe extension.
	exe := filepath.Join(dir, "hexal-program")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	args := append([]string{}, flags...)
	// Generated C relies on platform feature-test declarations (POSIX 2008 on
	// Linux; the pack's Windows defines on Windows) that a strict -std=c23
	// compile hides behind them. The target triple selects the ABI the pack
	// was built for; without it clang defaults to the host's MSVC target on
	// Windows and cannot link the MinGW archives.
	args = append(args, "--target="+tc.DefaultTarget)
	if runtime.GOOS == "windows" {
		args = append(args, "-DWIN32_LEAN_AND_MEAN", "-D_WIN32_WINNT=0x0A00", "-D_CRT_DECLARE_NONSTDC_NAMES=0", "-D_CRT_SECURE_NO_WARNINGS", "-DUTF8PROC_STATIC")
	} else {
		args = append(args, "-D_POSIX_C_SOURCE=200809L")
	}
	args = append(args, "-I", dir)
	if native != nil {
		args = append(args, native.includeOptions...)
	}
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
	// The scheduler runtime needs a real thread library on POSIX; the
	// Windows lane gets threading through the pack's system libraries and
	// has no -lpthread to pass.
	if runtime.GOOS != "windows" && strings.Contains(strings.Join(names, " "), "concurrency.c") {
		args = append(args, "-lpthread")
	}
	if native != nil {
		args = append(args, native.archives...)
		args = append(args, native.linkOptions...)
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
// every .c translation unit with -std=c23 -Wall -Wextra -Werror under the
// one discovered Clang: any warning or error fails the test.
func compileGeneratedC(t *testing.T, result compiler.CompilationResult, buildRoot string) {
	t.Helper()
	buildGeneratedC(t, clangToolchain(t), result, buildRoot)
}

// runGeneratedC (Tier 2) compiles under Clang, runs the resulting binary,
// and returns its normalized stdout for the caller to assert exactly. Stdout
// is normalized here only to collapse any platform CRLF translation; a
// fixture asserts the bytes the program wrote, not the platform's line-ending
// habits. Stderr must be empty and the process must exit zero, or the fixture
// fails -- a run that produced correct stdout while also writing to stderr or
// trapping is not a passing Tier 2 result.
func runGeneratedC(t *testing.T, result compiler.CompilationResult, buildRoot string) string {
	t.Helper()
	exe := buildGeneratedC(t, clangToolchain(t), result, buildRoot)
	stdout, stderr, exitedZero := runProcess(t, exe)
	if !exitedZero {
		t.Fatalf("generated program exited non-zero; stdout=%q stderr=%q", stdout, stderr)
	}
	if stderr != "" {
		t.Fatalf("generated program wrote to stderr: %q", stderr)
	}
	return strings.ReplaceAll(stdout, "\r\n", "\n")
}

// trapGeneratedC (Tier 3) compiles under Clang and runs a program that must
// terminate by a runtime trap: a successful exit fails the test, and stderr
// must contain requiredSubstring, the fixture's exact expected
// "[Runtime Error] ..." text. Stdout up to the trap point is not constrained
// by this helper; callers with output expectations before the trap assert it
// themselves.
func trapGeneratedC(t *testing.T, result compiler.CompilationResult, buildRoot, requiredSubstring string) {
	t.Helper()
	exe := buildGeneratedC(t, clangToolchain(t), result, buildRoot)
	_, stderr, exitedZero := runProcess(t, exe)
	if exitedZero {
		t.Fatalf("program must trap but exited successfully")
	}
	if !strings.Contains(stderr, requiredSubstring) {
		t.Fatalf("program's stderr = %q, want it to contain %q", stderr, requiredSubstring)
	}
}
