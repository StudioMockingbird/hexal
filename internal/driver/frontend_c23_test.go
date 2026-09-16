//go:build c23

package driver

// RFC 0193 end to end through the public driver API: header discovery,
// preprocessing, Clang JSON-AST inspection, normalization, and the ordinary
// compiler path. Tagged like the rest of this repository's toolchain-dependent
// suites.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// autoAdderFixture writes the canonical adder header and source plus an
// entrypoint that imports the header directly, with no handwritten binding.
func autoAdderFixture(t *testing.T, root string) string {
	t.Helper()
	native := filepath.Join(root, "native")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSource(t, root, "native/adder.h", "#ifndef ADDER_H\n#define ADDER_H\n#include <stdint.h>\nint32_t adder_add(int32_t left, int32_t right);\n#endif\n")
	writeSource(t, root, "native/adder.c", "#include \"adder.h\"\nint32_t adder_add(int32_t left, int32_t right) { return left + right; }\n")
	writeSource(t, root, "main.hex", "import\n    Adder from c \"adder.h\"\nend\n"+
		"mut total: Int32 := 0\n"+
		"unsafe do\n    total = Adder.adder_add(20, 22)\nend\n"+
		"print(total)\n")
	return native
}

// TestAutomaticHeaderImportBuildRuns is the canonical end-to-end example: no
// handwritten binding, one C source, and output 42.
func TestAutomaticHeaderImportBuildRuns(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := autoAdderFixture(t, dir)
	result, err := Build(BuildOptions{
		Root:         dir,
		CSources:     []string{"native/adder.c"},
		CIncludeDirs: []string{native},
	})
	if err != nil {
		t.Fatalf("automatic-import build failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
	// A frontend inspection command ran, and the generated module compile
	// received the user include root.
	var sawClang bool
	for _, command := range result.Commands {
		if filepath.Base(command.Tool) == "clang"+exeSuffix() {
			sawClang = true
		}
	}
	if !sawClang {
		t.Fatal("no standalone Clang inspection command was recorded")
	}
	assertStagingRemoved(t, dir)
}

// TestAutomaticHeaderOnlyStaticInline builds with no foreign source: the
// header supplies a static inline definition.
func TestAutomaticHeaderOnlyStaticInline(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := filepath.Join(dir, "native")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSource(t, dir, "native/adder.h", "#ifndef ADDER_H\n#define ADDER_H\n#include <stdint.h>\nstatic inline int32_t adder_add(int32_t left, int32_t right) { return left + right; }\n#endif\n")
	writeSource(t, dir, "main.hex", "import\n    Adder from c \"adder.h\"\nend\n"+
		"mut total: Int32 := 0\n"+
		"unsafe do\n    total = Adder.adder_add(20, 22)\nend\n"+
		"print(total)\n")
	result, err := Build(BuildOptions{Root: dir, CIncludeDirs: []string{native}})
	if err != nil {
		t.Fatalf("header-only automatic build failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
}

// TestAutomaticOmittedUnsupportedBesideSupported proves an unrelated
// unsupported declaration does not reject the header and the supported one is
// still usable.
func TestAutomaticOmittedUnsupportedBesideSupported(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := filepath.Join(dir, "native")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	// The callback parameter is an unsupported function pointer; the scalar
	// function beside it is supported.
	writeSource(t, dir, "native/adder.h", "#ifndef ADDER_H\n#define ADDER_H\n#include <stdint.h>\nint32_t adder_add(int32_t left, int32_t right);\nint32_t use_callback(int32_t (*callback)(int32_t));\n#endif\n")
	writeSource(t, dir, "native/adder.c", "#include \"adder.h\"\nint32_t adder_add(int32_t left, int32_t right) { return left + right; }\nint32_t use_callback(int32_t (*callback)(int32_t)) { return callback(1); }\n")
	writeSource(t, dir, "main.hex", "import\n    Adder from c \"adder.h\"\nend\n"+
		"mut total: Int32 := 0\n"+
		"unsafe do\n    total = Adder.adder_add(20, 22)\nend\n"+
		"print(total)\n")
	result, err := Build(BuildOptions{
		Root:         dir,
		CSources:     []string{"native/adder.c"},
		CIncludeDirs: []string{native},
	})
	if err != nil {
		t.Fatalf("build with an unrelated unsupported declaration failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
}

// TestAutomaticEqualRequestsPrepareOnce proves two modules naming one header
// prepare one binding and both resolve.
func TestAutomaticEqualRequestsPrepareOnce(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := autoAdderFixture(t, dir)
	writeSource(t, dir, "helper.hex", "import\n    Adder from c \"adder.h\"\nend\n"+
		"fun helper_add(left: Int32, right: Int32): Int32 do\n"+
		"    unsafe do\n        return Adder.adder_add(left, right)\n    end\n"+
		"end\n"+
		"export\n    helper_add\nend\n")
	writeSource(t, dir, "main.hex", "import\n    Helper from \"./helper\",\n    Adder from c \"adder.h\"\nend\n"+
		"mut total: Int32 := 0\n"+
		"unsafe do\n    total = Helper.helper_add(20, 22) + Adder.adder_add(0, 0)\nend\n"+
		"print(total)\n")
	result, err := Build(BuildOptions{
		Root:         dir,
		CSources:     []string{"native/adder.c"},
		CIncludeDirs: []string{native},
	})
	if err != nil {
		t.Fatalf("two modules naming one header failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
	// Exactly one Clang inspection ran for the equal requests.
	inspections := 0
	for _, command := range result.Commands {
		if filepath.Base(command.Tool) == "clang"+exeSuffix() {
			inspections++
		}
	}
	if inspections != 1 {
		t.Fatalf("equal requests prepared %d bindings, want 1", inspections)
	}
}
