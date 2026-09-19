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
	"debug/elf"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
	"hexal/internal/backend"
	"hexal/internal/version"
)

// withTestBackend fills the required compiler and target into one test's build
// options. Every tagged driver test that runs a real build goes through it, so
// the qualified configuration lives in one place. The runtime pack is embedded
// in the compiler, so there is no runtime directory to select.
func withTestBackend(t *testing.T, options BuildOptions) BuildOptions {
	t.Helper()
	options.CompilerPath = requireBackend(t).Exe
	options.Target = compilerTypes.TargetX86_64LinuxGNU
	return options
}

// doctorOptionsForTest selects the qualified configuration for doctor.
func doctorOptionsForTest(t *testing.T) DoctorOptions {
	t.Helper()
	return DoctorOptions{
		CompilerPath: requireBackend(t).Exe,
		Target:       compilerTypes.TargetX86_64LinuxGNU,
	}
}

var (
	testBackendOnce sync.Once
	testBackend     *backend.Backend
	testBackendErr  error
)

// requireBackend resolves and caches the installed Clang once per test binary:
// resolution spawns the compiler, and every helper and build needs the same
// executable. It never skips a missing or too-old Clang; the exact reason is
// reported so an unqualified host cannot pass silently.
func requireBackend(t *testing.T) *backend.Backend {
	t.Helper()
	testBackendOnce.Do(func() {
		exe, err := resolveTestClang()
		if err != nil {
			testBackendErr = err
			return
		}
		selected, err := backend.NewBackend(exe)
		if err != nil {
			testBackendErr = fmt.Errorf("qualification gate needs a usable Clang backend at %s: %v", exe, err)
			return
		}
		if selected.Major < clangMinimumMajor {
			testBackendErr = fmt.Errorf("qualification gate needs Clang %d or newer; %s reports %q", clangMinimumMajor, exe, selected.Version)
			return
		}
		testBackend = selected
	})
	if testBackendErr != nil {
		t.Fatal(testBackendErr)
	}
	// Return a private copy: a build pins Backend.Directory to its own staging
	// tree, and one test's staging path must never leak into another's
	// invocation.
	selected := *testBackend
	return &selected
}

// resolveTestClang locates the installed Clang for the tagged gate. The
// HEXAL_CLANG override names one exact executable; otherwise `clang` is
// resolved from PATH. This discovery policy is test-only: production `build`
// and `doctor` still require an explicit `-cc`.
func resolveTestClang() (string, error) {
	if override := os.Getenv("HEXAL_CLANG"); override != "" {
		absolute, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("HEXAL_CLANG %q cannot be resolved: %v", override, err)
		}
		if !executableFile(absolute) {
			return "", fmt.Errorf("HEXAL_CLANG %q is not an executable file", override)
		}
		return absolute, nil
	}
	exe, err := exec.LookPath("clang")
	if err != nil {
		return "", fmt.Errorf("qualification gate needs clang on PATH or HEXAL_CLANG set: %v", err)
	}
	return exe, nil
}

// materializeTestPack materializes exactly the demanded embedded-pack entries
// for target beneath staging and returns their include roots, archives, and the
// loaded pack inputs (whose SystemLibraries feed the link). Tests that compile
// or link generated and fixture C by hand use it instead of the retired
// source-build dependency machinery.
func materializeTestPack(t *testing.T, staging string, target compilerTypes.TargetProfileID, dependencies []compiler.RuntimeDependency) (includeDirs, archives []string, pack packInputs) {
	t.Helper()
	fsys, err := runtimePackFS(target)
	if err != nil {
		t.Fatal(err)
	}
	_, loaded, err := loadRuntimeManifest(fsys, target, dependencies)
	if err != nil {
		t.Fatal(err)
	}
	includeDirs, archives, err = materializePack(staging, fsys, loaded)
	if err != nil {
		t.Fatal(err)
	}
	return includeDirs, archives, loaded
}

// includeDirOptions renders materialized include roots as -I arguments.
func includeDirOptions(includeDirs []string) []string {
	options := make([]string, 0, len(includeDirs))
	for _, directory := range includeDirs {
		options = append(options, "-I"+directory)
	}
	return options
}

// TestDependencyFreeBuildSelectsNoPackInput proves a program selecting no
// runtime dependency neither links a checked-in archive nor a pack system
// library.
func TestDependencyFreeBuildSelectsNoPackInput(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "let value: Int32 = 1\n")

	result, err := Build(withTestBackend(t, BuildOptions{Root: dir}))
	if err != nil {
		t.Fatalf("dependency-free build failed: %v", err)
	}
	for _, command := range result.Commands {
		joined := strings.Join(command.Arguments, " ")
		for _, marker := range []string{"libuv.a", "mimalloc.a", "-lpthread", "-ldl", "-lrt"} {
			if strings.Contains(joined, marker) {
				t.Fatalf("%s command names a runtime-pack input %q:\n%v", command.Stage, marker, command.Arguments)
			}
		}
	}
}

// TestBuildProducesRunnableExecutable is the end-to-end check: source in,
// executable out, correct output when run. It is the smallest thing that
// fails if any pipeline stage breaks.
func TestBuildProducesRunnableExecutable(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "print(\"ok\")\n")

	result, err := Build(withTestBackend(t, BuildOptions{Root: dir}))
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

// TestBuildHasNoMimallocSharedLibraryImport proves the demanded mimalloc pack
// is linked statically: the executable's dynamic dependencies name no
// mimalloc shared object.
func TestBuildHasNoMimallocSharedLibraryImport(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "let values: Array<Int32, 2> = [1, 2]\nprint(values[0])\n")

	result, err := Build(withTestBackend(t, BuildOptions{Root: dir}))
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	image, err := elf.Open(result.Executable)
	if err != nil {
		t.Fatalf("open executable: %v", err)
	}
	defer image.Close()
	libraries, err := image.ImportedLibraries()
	if err != nil {
		t.Fatalf("read executable imports: %v", err)
	}
	for _, library := range libraries {
		if strings.Contains(strings.ToLower(library), "mimalloc") {
			t.Fatalf("executable imports mimalloc shared library %q", library)
		}
	}
}

// TestBuildCompilesRuntimeComponents guards ADR 0055's rule that every .c
// entry is compiled, not only those under modules/. A program that can trap
// pulls in hexal/runtime.c, so a successful link proves the component was
// compiled rather than skipped.
func TestBuildCompilesRuntimeComponents(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "let values: Array<Int32, 2> = [1, 2]\nprint(values[0])\n")

	result, err := Build(withTestBackend(t, BuildOptions{Root: dir}))
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
	writeSource(t, dir, "util.hex", "fun double(value: Int32): Int32 do\n    return value * 2\nend\nexport\n    double\nend\n")
	writeSource(t, dir, "main.hex", "import\n    Util from \"./util\"\nend\nprint(Util.double(21))\n")

	result, err := Build(withTestBackend(t, BuildOptions{Root: dir}))
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
	compileResult := compiler.Compile(sources, "main.hex", compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU})
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
	includes, archives, pack := materializeTestPack(t, staging, compilerTypes.TargetX86_64LinuxGNU, compileResult.Dependencies)
	selected.Directory = staging

	var result BuildResult
	compileOptions := append(append([]string{}, Options(ModeDebug).Compile...), includeDirOptions(includes)...)
	if err := compileTranslationUnitsWithOptions(selected, staging, cFiles, compileOptions, nil, &result); err != nil {
		t.Fatalf("c compilation failed: %v", err)
	}
	const driverC = "int hexal_driver_probe(void) { return 7; }\n"
	probeSource := filepath.Join(staging, "driver_probe.c")
	if err := os.WriteFile(probeSource, []byte(driverC), 0o644); err != nil {
		t.Fatal(err)
	}
	probeObject := filepath.Join(staging, "driver_probe.o")
	probe, err := selected.CompileOne(qualifiedTriple, []string{"-I", staging}, probeSource, probeObject)
	if err != nil || probe.ExitCode != 0 {
		t.Fatalf("driver c object failed to compile: %v\n%s", err, probe.Stderr)
	}
	objects := cFilesToObjects(staging, cFiles)
	objects = append(objects, archives...)
	objects = append(objects, probeObject)
	output := filepath.Join(dir, "build", "main")
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatal(err)
	}
	stagedExe := filepath.Join(staging, "main.staged")
	linkOptions := append(append([]string{}, Options(ModeDebug).Link...), packSystemLibraryOptions(pack)...)
	if err := linkObjectsWithOptions(selected, staging, objects, linkOptions, stagedExe, &result); err != nil {
		t.Fatalf("link with driver object failed: %v", err)
	}
	if err := publishExecutable(stagedExe, output); err != nil {
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
	result := compiler.Compile(sources, "app.hex", compiler.Project{Target: compilerTypes.TargetX86_64LinuxGNU})
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
	report, problems := Doctor(doctorOptionsForTest(t))
	if len(report) == 0 || report[0] != "Hexal: "+version.String() {
		t.Errorf("doctor report %q lacks the leading Hexal version line", report)
	}
	for _, problem := range problems {
		t.Errorf("doctor reported a problem on a host where builds work: %s", problem)
	}
}
