//go:build c23

package c23validation

// The release lane. Every tagged fixture and every workbench snippet is built
// a second time through the build driver's own release option set and
// compared against the same program built through its debug option set: the
// generated C is one artifact set shared by both builds, so any difference
// that appears is an optimization exposing latent undefined behavior in
// generated code, which a debug-only pass would never catch.
//
// It is deliberately additive to the existing harness rather than woven into
// it: the tiered runner above checks one flag set for compile/run/trap, while
// this compares the same Clang against itself at two option sets.

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/internal/driver"
)

// modeRun is one program's complete observable result under one mode.
type modeRun struct {
	stdout   string
	stderr   string
	exitCode int
	exe      string
}

// buildOnlyInMode builds one artifact set with the driver's exact option set
// for mode and returns its executable path without running it. Every
// mode-comparison entry point in this file funnels through here, including
// the ones that also run the result, so the compile cache has one writer.
func buildOnlyInMode(t *testing.T, tc toolchain, result compiler.CompilationResult, buildRoot string, mode driver.BuildMode) string {
	t.Helper()
	options := driver.Options(mode)
	flags := append([]string{"-std=c23"}, options.Compile...)
	key := compileCacheKey{
		artifactHash: canonicalArtifactHash(result.Files),
		toolchain:    tc.Name,
		flags:        strings.Join(append(slices.Clone(flags), options.Link...), " "),
		buildRoot:    buildRoot,
	}

	compileCacheMu.Lock()
	entry, ok := compileCache[key]
	if !ok {
		entry = &compileCacheEntry{}
		compileCache[key] = entry
	}
	compileCacheMu.Unlock()

	entry.once.Do(func() {
		entry.result = buildModeArtifacts(tc, result, flags, options.Link, buildRoot, key.artifactHash, string(mode))
	})
	if entry.result.err != nil {
		t.Fatalf("%s rejected generated C under %s mode options: %v", tc.Name, mode, entry.result.err)
	}
	return entry.result.exe
}

// buildAndRunInMode builds one artifact set with the driver's exact option
// set for mode and runs it, returning everything a fixture is allowed to
// observe. Clang is the compiler the driver itself uses, so its options mean
// what the driver means by them.
func buildAndRunInMode(t *testing.T, tc toolchain, result compiler.CompilationResult, buildRoot string, mode driver.BuildMode) modeRun {
	t.Helper()
	return runInMode(t, buildOnlyInMode(t, tc, result, buildRoot, mode))
}

// buildModeArtifacts materializes one artifact set under its own mode
// subdirectory and builds it in a single invocation, so the link options
// reach the same command as the compile options. It takes no *testing.T for
// the same reason doBuild does not: sync.Once may run it from any waiting
// subtest's goroutine.
func buildModeArtifacts(tc toolchain, result compiler.CompilationResult, compileFlags, linkFlags []string, buildRoot, artifactHash, mode string) buildResult {
	var native *dependencyBuild
	if len(result.Dependencies) > 0 {
		native = buildDependencies(result.Dependencies, buildRoot)
		if native.err != nil {
			return buildResult{err: native.err}
		}
	}
	dir := filepath.Join(buildRoot, artifactHash, tc.Name+"-"+mode)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return buildResult{err: err}
	}
	names := make([]string, 0, len(result.Files))
	for name := range result.Files {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			return buildResult{err: err}
		}
		if err := os.WriteFile(path, []byte(result.Files[name]), 0644); err != nil {
			return buildResult{err: err}
		}
	}
	// The generated tree already contains a hexal/ directory of artifacts, so
	// the executable is named distinctly; a Linux ELF has no extension.
	exe := filepath.Join(dir, "hexal-program")
	args := append([]string{}, compileFlags...)
	// Generated C relies on POSIX 2008 declarations a strict -std=c23 glibc
	// compile hides behind _POSIX_C_SOURCE (see c23_harness_test.go).
	args = append(args, "-D_POSIX_C_SOURCE=200809L", "-I", dir)
	if native != nil {
		args = append(args, native.includeOptions...)
	}
	for _, name := range names {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	if native != nil {
		args = append(args, native.archives...)
		args = append(args, native.linkOptions...)
	}
	args = append(args, linkFlags...)
	args = append(args, "-o", exe)

	ctx, cancel := context.WithTimeout(context.Background(), buildProcessTimeout)
	defer cancel()
	output, err := exec.CommandContext(ctx, tc.Command[0], args...).CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return buildResult{err: fmt.Errorf("%s mode build did not finish within %s (artifact %s)", mode, buildProcessTimeout, artifactHash)}
	}
	if err != nil {
		return buildResult{err: fmt.Errorf("%v\n%s", err, output)}
	}
	return buildResult{exe: exe}
}

// runInMode runs one built program under the same hard timeout every other
// binary in this suite gets, capturing the streams separately and the exit
// status. Stdout is normalized the way the tiered runner normalizes it, to
// collapse any platform CRLF translation, so a fixture asserts the bytes the
// program wrote.
func runInMode(t *testing.T, path string) modeRun {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), runProcessTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, path)
	var stdout, stderr bytes.Buffer
	command.Stdout = &stdout
	command.Stderr = &stderr
	err := command.Run()
	if ctx.Err() == context.DeadlineExceeded {
		t.Fatalf("program at %s did not exit within %s", path, runProcessTimeout)
	}
	run := modeRun{
		stdout: strings.ReplaceAll(stdout.String(), "\r\n", "\n"),
		stderr: strings.ReplaceAll(stderr.String(), "\r\n", "\n"),
		exe:    path,
	}
	if exit, ok := err.(*exec.ExitError); ok {
		run.exitCode = exit.ExitCode()
	} else if err != nil {
		t.Fatalf("running %s failed: %v", path, err)
	}
	return run
}

// compareModes builds one program under both modes and requires every
// observable of the two runs to agree. Only resource exhaustion that depends
// on code generation is permitted to differ, and no fixture in this catalog
// exercises one.
func compareModes(t *testing.T, tc toolchain, result compiler.CompilationResult, buildRoot string) {
	t.Helper()
	debug := buildAndRunInMode(t, tc, result, buildRoot, driver.ModeDebug)
	release := buildAndRunInMode(t, tc, result, buildRoot, driver.ModeRelease)
	if debug.stdout != release.stdout {
		t.Errorf("stdout differs across modes:\ndebug   %q\nrelease %q", debug.stdout, release.stdout)
	}
	if debug.stderr != release.stderr {
		t.Errorf("stderr differs across modes:\ndebug   %q\nrelease %q", debug.stderr, release.stderr)
	}
	if debug.exitCode != release.exitCode {
		t.Errorf("exit status differs across modes: debug %d, release %d", debug.exitCode, release.exitCode)
	}
}

// compileOnlyCompareModes builds a program under both modes without running
// either build. It exists for exactly the programs compareModes must not
// touch: one that parks waiting for a real OS signal or blocks reading
// unavailable input runs forever under either mode, so only their
// compilability under both option sets is a safe, meaningful comparison.
func compileOnlyCompareModes(t *testing.T, tc toolchain, result compiler.CompilationResult, buildRoot string) {
	t.Helper()
	buildOnlyInMode(t, tc, result, buildRoot, driver.ModeDebug)
	buildOnlyInMode(t, tc, result, buildRoot, driver.ModeRelease)
}

// TestReleaseLaneFixtures runs the complete tagged fixture catalog through
// both of the driver's mode option sets. A fixture with a process expectation
// is compiled and run under both, so an optimization that changed its
// behavior would not go unobserved; the tiered runner already vetted that
// running it terminates. A fixture with no expectation is compile-only in the
// tiered runner for the same reason here: some of them (an OS-signal wait, a
// blocking stdin read) never terminate under automated execution regardless
// of mode, so this lane only compares whether both option sets accept the
// generated C.
func TestReleaseLaneFixtures(t *testing.T) {
	buildRoot := t.TempDir()
	clang := clangToolchain(t)
	for _, f := range fixtureCatalog {
		if !f.appliesToHost() {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			result := f.resolve(t)
			if f.expectation == nil {
				compileOnlyCompareModes(t, clang, result, buildRoot)
				return
			}
			compareModes(t, clang, result, buildRoot)
		})
	}
}

// TestReleaseLaneSnippetCatalog gives every workbench snippet the same
// compile-only two-mode comparison the tiered harness's own
// TestC23SnippetCatalogCompiles uses for the same catalog: no snippet in it
// is vetted for unattended execution (several read stdin or otherwise block
// with no driving fixture to supply input), so this lane only confirms both
// mode option sets accept its generated C, not that running it terminates.
// Snippets run in parallel for the same reason the tiered snippet test does:
// the count does not complete sequentially in a reasonable time.
func TestReleaseLaneSnippetCatalog(t *testing.T) {
	buildRoot := t.TempDir()
	clangToolchain(t)
	for _, snippet := range allSnippets(t) {
		t.Run(snippet.ID, func(t *testing.T) {
			t.Parallel()
			clang := clangToolchain(t)
			compileOnlyCompareModes(t, clang, assertCompilesSources(t, snippet.Sources, snippet.Entrypoint), buildRoot)
		})
	}
}

// representativeModePrograms is the set the size and debug-information claims
// are measured over: a trivial program, a collection program that pulls in a
// vendored dependency, and a text program.
var representativeModePrograms = map[string]map[string]string{
	"trivial":     {"app.hex": "print(\"ok\")\n"},
	"collections": {"app.hex": "fun demo(h: Heap): Int32 do\n    values: List<Int32> := List<Int32>(h)\n    defer values.free(h)\n    values.push(7)\n    values.push(35)\n    return values[0] + values[1]\nend\nprint(demo(Heap()))\n"},
	"text":        {"app.hex": "fun demo(h: Heap): Size do\n    text: String := \"ready\".to_string(h)\n    defer text.free(h)\n    loud: String := text.concat(h, \"!\")\n    defer loud.free(h)\n    return loud.length()\nend\nprint(demo(Heap()))\n"},
}

// TestReleaseExecutablesAreSmallerAndUndebuggable checks the two properties a
// release build exists for, over the representative set. A debug build emits
// DWARF sections (the ELF section table names .debug_info); a stripped
// release build carries none.
func TestReleaseExecutablesAreSmallerAndUndebuggable(t *testing.T) {
	buildRoot := t.TempDir()
	clang := clangToolchain(t)
	names := make([]string, 0, len(representativeModePrograms))
	for name := range representativeModePrograms {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			result := assertCompilesSources(t, representativeModePrograms[name], "app.hex")
			debug := buildAndRunInMode(t, clang, result, buildRoot, driver.ModeDebug)
			release := buildAndRunInMode(t, clang, result, buildRoot, driver.ModeRelease)

			debugSize := fileSize(t, debug.exe)
			releaseSize := fileSize(t, release.exe)
			if releaseSize >= debugSize {
				t.Errorf("release is %d bytes, not smaller than debug's %d", releaseSize, debugSize)
			}
			debugRaw, err := os.ReadFile(debug.exe)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Contains(debugRaw, []byte(".debug_info")) {
				t.Error("debug executable carries no DWARF debug information")
			}
			raw, err := os.ReadFile(release.exe)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(raw, []byte(".debug_info")) {
				t.Error("release executable carries debug information")
			}
			t.Logf("%s: debug %d bytes, release %d bytes (%.1f%% of debug)", name, debugSize, releaseSize, 100*float64(releaseSize)/float64(debugSize))
		})
	}
}

// TestFloatingOutputIsModeIndependent is the contraction check. Contraction
// into a fused multiply-add would change these results, so both modes disable
// it; the fixture computes values where a contracted form would differ.
func TestFloatingOutputIsModeIndependent(t *testing.T) {
	buildRoot := t.TempDir()
	clang := clangToolchain(t)
	const program = "fun demo(a: Float64, b: Float64, c: Float64): Float64 do\n" +
		"    return (a * b) + c\n" +
		"end\n" +
		"print(demo(0.1, 0.2, 0.3))\n" +
		"print(demo(1.0 / 3.0, 3.0, -1.0))\n" +
		"print(1.0 / 3.0)\n"
	result := assertCompilesSources(t, map[string]string{"app.hex": program}, "app.hex")
	debug := buildAndRunInMode(t, clang, result, buildRoot, driver.ModeDebug)
	release := buildAndRunInMode(t, clang, result, buildRoot, driver.ModeRelease)
	if debug.stdout != release.stdout {
		t.Fatalf("floating output depends on the mode:\ndebug   %q\nrelease %q", debug.stdout, release.stdout)
	}
	if debug.stdout == "" {
		t.Fatal("the floating fixture produced no output")
	}
}

func fileSize(t *testing.T, path string) int64 {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Size()
}

// This file shares compileCache with the tiered harness, so its keys must
// never collide with that harness's: the two build the same artifacts with
// different flags, and a collision would hand one lane the other's binary.
// The flag string carries the mode's compile and link options, which the
// tiered harness never sets, so the keys differ by construction.
func TestModeCacheKeysDoNotCollideWithTheTieredHarness(t *testing.T) {
	tiered := strings.Join(append([]string{"-std=c23", "-Wall", "-Wextra", "-Werror"}, warningFlags...), " ")
	for _, mode := range []driver.BuildMode{driver.ModeDebug, driver.ModeRelease} {
		options := driver.Options(mode)
		lane := strings.Join(append(append([]string{"-std=c23"}, options.Compile...), options.Link...), " ")
		if lane == tiered {
			t.Fatalf("%s mode flag key collides with the tiered harness key", mode)
		}
	}
}
