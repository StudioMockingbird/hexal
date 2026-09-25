//go:build c23

package driver

// The one foreign target-object link fixture the qualified profile's own
// Runtime and linkage section requires: proof that the installed Clang can
// consume an ordinary target object it did not produce, alongside the
// objects it compiled from Hexal-generated C, without checking a
// platform-specific binary into the repository (testdata/mode-probe.c is
// portable C, compiled fresh here on every run). The probe object's own
// function is never called from Hexal source -- there is no C-interop
// syntax to call it with yet -- so this proves only the backend's linker
// accepts a foreign object in the same link, not language-level
// interoperability; the foreign binding tests cover that separately.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"hexal/compiler"
)

func TestForeignTargetObjectLinksWithHexalObjects(t *testing.T) {
	selected := requireBackend(t)
	dir := t.TempDir()

	compileResult := compiler.Compile(map[string]string{"main.hex": "print(1)\n"}, "main.hex", compiler.Project{Target: hostQualifiedTarget()})
	if len(compileResult.Stderr) > 0 {
		t.Fatalf("Hexal compilation failed: %v", compileResult.Stderr)
	}

	cFiles, err := materialize(dir, compileResult.Files)
	if err != nil {
		t.Fatalf("materialize: %v", err)
	}
	includes, archives, pack := materializeTestPack(t, dir, hostQualifiedTarget(), compileResult.Dependencies)
	selected.Directory = dir
	options := Options(ModeDebug, hostQualifiedTarget())
	compileOptions := append(append([]string{}, options.Compile...), includeDirOptions(includes)...)
	linkOptions := append(append([]string{}, options.Link...), packSystemLibraryOptions(pack)...)
	var result BuildResult
	if err := compileTranslationUnitsWithOptions(selected, dir, cFiles, compileOptions, nil, hostQualifiedTriple(), featureDefines(hostQualifiedTarget()), &result); err != nil {
		t.Fatalf("compiling Hexal-generated C failed: %v", err)
	}
	objects := append(cFilesToObjects(dir, cFiles), archives...)

	probeSource, err := filepath.Abs(filepath.Join("testdata", "mode-probe.c"))
	if err != nil {
		t.Fatal(err)
	}
	probeObject := filepath.Join(dir, "mode-probe.o")
	invocation, err := selected.CompileOne(hostQualifiedTriple(), options.Compile, probeSource, probeObject)
	if err != nil {
		t.Fatalf("cannot run backend on foreign source: %v", err)
	}
	if invocation.ExitCode != 0 {
		t.Fatalf("compiling foreign target object failed: %s", invocation.Stderr)
	}
	objects = append(objects, probeObject)

	binary := filepath.Join(dir, "combined"+exeSuffix())
	if err := linkObjectsWithOptions(selected, dir, hostQualifiedTriple(), objects, linkOptions, binary, &result); err != nil {
		t.Fatalf("linking Hexal objects with the foreign object failed: %v", err)
	}

	output, err := exec.Command(binary).CombinedOutput()
	if err != nil {
		t.Fatalf("running the combined executable failed: %v\n%s", err, output)
	}
	if string(output) != "1" {
		t.Fatalf("combined executable output = %q, want %q", output, "1")
	}
	if _, err := os.Stat(binary); err != nil {
		t.Fatalf("expected executable at %s: %v", binary, err)
	}
}
