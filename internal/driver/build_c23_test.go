//go:build c23

package driver

// Driver tests that spawn the real C toolchain. Tagged `c23` like the rest of
// this repository's toolchain-dependent suites, so ordinary `go test ./...`
// stays pure Go and passes with no toolchain installed. This is the official
// qualification gate: it never skips a missing or invalid backend, so a
// `c23` run on an unqualified host fails loudly instead of passing silently.
//
// Run with: go test -tags c23 ./internal/driver/

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
	"hexal/internal/backend"
	"hexal/internal/version"
)

func requireBackend(t *testing.T) *backend.Backend {
	t.Helper()
	exe, err := exec.LookPath("zig")
	if err != nil {
		t.Fatalf("qualification gate needs zig on PATH: %v", err)
	}
	selected, err := backend.NewBackend(exe)
	if err != nil {
		t.Fatalf("qualification gate needs a usable backend: %v", err)
	}
	if err := selected.CheckPinned(backendPkgPinnedVersion()); err != nil {
		t.Fatalf("qualification gate needs the pinned backend: %v", err)
	}
	return selected
}

// TestBuildProducesRunnableExecutable is the end-to-end check: source in,
// executable out, correct output when run. It is the smallest thing that
// fails if any pipeline stage breaks.
func TestBuildProducesRunnableExecutable(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "print(\"ok\")\n")

	result, err := Build(BuildOptions{Root: dir})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	if result.HexalVersion != version.String() {
		t.Fatalf("HexalVersion = %q, want %q", result.HexalVersion, version.String())
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "ok" {
		t.Fatalf("output = %q, want %q", got, "ok")
	}
	// A retained staging tree would fail this: a successful build owns its
	// staging directory and removes it.
	assertStagingRemoved(t, dir)
}

// TestBuildCompilesRuntimeComponents guards ADR 0055's rule that every .c
// entry is compiled, not only those under modules/. A program that can trap
// pulls in hexal/runtime.c, so a successful link proves the component was
// compiled rather than skipped.
func TestBuildCompilesRuntimeComponents(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "values: Array<Int32, 2> := [1, 2]\nprint(values[0])\n")

	result, err := Build(BuildOptions{Root: dir})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "1" {
		t.Fatalf("output = %q, want %q", got, "1")
	}
}

// TestBuildResolvesImports checks that the driver hands the compiler a source
// map keyed so imports resolve. Import resolution is the compiler's job; what
// is verified here is that the driver's logical keys agree with it.
func TestBuildResolvesImports(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "util.hex", "export fun double(value: Int32): Int32 do\n    return value * 2\nend\n")
	writeSource(t, dir, "main.hex", "module Util = import \"./util\"\nprint(Util.double(21))\n")

	result, err := Build(BuildOptions{Root: dir})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatal(err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
}

// TestLinkDriverLevelCObject proves a compatible C object links beside the
// generated program without any source-visible C import: the Hexal sources
// name no C file, and the extra object arrives purely through the driver's
// link invocation.
func TestLinkDriverLevelCObject(t *testing.T) {
	selected := requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "print(\"ok\")\n")

	sources, err := discover(dir, stagingPath(dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	compileResult := compiler.Compile(sources, "main.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if len(compileResult.Stderr) > 0 {
		t.Fatalf("hexal compilation failed: %v", compileResult.Stderr)
	}
	staging, err := freshStagingDir(filepath.Join(dir, "build"))
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(staging)
	cFiles, err := materialize(staging, compileResult.Files)
	if err != nil {
		t.Fatal(err)
	}
	const driverC = "int hexal_driver_probe(void) { return 7; }\n"
	probeSource := filepath.Join(staging, "driver_probe.c")
	if err := os.WriteFile(probeSource, []byte(driverC), 0o644); err != nil {
		t.Fatal(err)
	}
	var result BuildResult
	if err := compileTranslationUnits(selected, staging, cFiles, &result); err != nil {
		t.Fatalf("c compilation failed: %v", err)
	}
	probeObject := filepath.Join(staging, "driver_probe.o")
	probe, err := selected.CompileOne(qualifiedTriple, []string{"-I", staging}, probeSource, probeObject)
	if err != nil || probe.ExitCode != 0 {
		t.Fatalf("driver c object failed to compile: %v\n%s", err, probe.Stderr)
	}
	objects := append(cFilesToObjects(staging, cFiles), probeObject)
	output := filepath.Join(dir, "build", "main"+exeSuffix())
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	tempExe, err := linkObjects(selected, staging, objects, output, &result)
	if err != nil {
		t.Fatalf("link with driver object failed: %v", err)
	}
	defer os.Remove(tempExe)
	if err := publishExecutable(tempExe, output); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	combined, err := exec.Command(output).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", output, err)
	}
	if got := string(combined); got != "ok" {
		t.Fatalf("output = %q, want %q", got, "ok")
	}
}

// TestGeneratedArtifactsContainNoAbsolutePaths pins the path-independence
// half of determinism: equivalent inputs produce byte-identical artifacts,
// so no host path may leak into generated C. The check runs the tagged gate
// because it asserts over a backend-qualified profile render.
func TestGeneratedArtifactsContainNoAbsolutePaths(t *testing.T) {
	requireBackend(t)
	sources := map[string]string{"app.hex": "print(\"ok\")\n"}
	result := compiler.Compile(sources, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if len(result.Stderr) > 0 {
		t.Fatalf("hexal compilation failed: %v", result.Stderr)
	}
	temporary := os.TempDir()
	for name, content := range result.Files {
		if strings.Contains(content, temporary) {
			t.Errorf("artifact %q contains the temporary directory", name)
		}
		for _, marker := range []string{`:\`, `:/`} {
			for _, line := range strings.Split(content, "\n") {
				trimmed := strings.TrimSpace(line)
				if strings.HasPrefix(trimmed, "#line") {
					continue
				}
				if index := strings.Index(line, marker); index > 0 && isDriveLetter(line[index-1]) {
					t.Errorf("artifact %q contains absolute path in %q", name, trimmed)
					break
				}
			}
		}
	}
}

// isDriveLetter reports whether byte starts a Windows drive prefix, so the
// absolute-path scan does not mistake a colon inside C code (such as a
// ternary or initializer) for a path.
func isDriveLetter(byte byte) bool {
	return byte >= 'A' && byte <= 'Z' || byte >= 'a' && byte <= 'z'
}

// assertStagingRemoved holds the ownership rule: a successful build owns its
// staging tree and removes it, so the source root is never polluted and no
// stale objective can be picked up later. It observes the staging parent
// directly rather than widening BuildResult for testability.
func assertStagingRemoved(t *testing.T, root string) {
	t.Helper()
	parent := stagingPath(root, "")
	entries, err := os.ReadDir(parent)
	if err != nil {
		if os.IsNotExist(err) {
			return
		}
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "build-") {
			t.Fatalf("staging directory %q was not removed", filepath.Join(parent, entry.Name()))
		}
	}
}

// TestDoctorPassesOnThisHost is a live check that the doctor probe agrees
// with the build path: if a build works here, doctor must not report a
// toolchain problem.
func TestDoctorPassesOnThisHost(t *testing.T) {
	requireBackend(t)
	report, problems := Doctor()
	if len(report) == 0 || report[0] != "Hexal: "+version.String() {
		t.Errorf("doctor report %q lacks the leading Hexal version line", report)
	}
	for _, problem := range problems {
		t.Errorf("doctor reported a problem on a host where builds work: %s", problem)
	}
}
