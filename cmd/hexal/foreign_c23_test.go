//go:build c23

package main

// Foreign C inputs through the public CLI: one source-based library and one
// precompiled archive, built and run by `hexal build` itself. Tagged `c23`
// like every other toolchain-dependent suite.

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"hexal/internal/backend"
)

// writeCLIFile writes one fixture file under root, creating its directory.
func writeCLIFile(t *testing.T, root, name, content string) string {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// writeAdderProject writes the canonical adder project: a handwritten binding,
// a header, a C source, and the Hexal entrypoint.
func writeAdderProject(t *testing.T, root string) {
	t.Helper()
	writeCLIFile(t, root, "native/adder.h", "#ifndef ADDER_H\n#define ADDER_H\n#include <stdint.h>\nint32_t adder_add(int32_t left, int32_t right);\n#endif\n")
	writeCLIFile(t, root, "native/adder.c", "#include \"adder.h\"\nint32_t adder_add(int32_t left, int32_t right) {\n    return left + right;\n}\n")
	writeCLIFile(t, root, "binding.hex", "extern c from \"adder.h\" do\n"+
		"    fun adder_add(left: Int32 as \"int32_t\", right: Int32 as \"int32_t\"): Int32 as \"int32_t\"\n"+
		"end\n"+
		"export\n    adder_add\nend\n")
	writeCLIFile(t, root, "main.hex", "import\n    Adder from \"./binding\"\nend\n"+
		"mut total: Int32 := 0\n"+
		"unsafe do\n    total = Adder.adder_add(20, 22)\nend\n"+
		"print(total)\n")
}

func TestCLIForeignSourceBuild(t *testing.T) {
	selected := cliBackend(t)
	root := t.TempDir()
	writeAdderProject(t, root)
	output := filepath.Join(root, "adder")
	err := run([]string{
		"build",
		"-cc", selected.Exe,
		"-target", "x86_64-linux-gnu",
		"-root", root,
		"-entry", "main.hex",
		"-out", output,
		"-c-source", "native/adder.c",
		"-c-include", "native",
	})
	if err != nil {
		t.Fatalf("hexal build failed: %v", err)
	}
	combined, err := exec.Command(output).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v\n%s", output, err, combined)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
}

func TestCLIForeignArchiveBuild(t *testing.T) {
	selected := cliBackend(t)
	root := t.TempDir()
	writeAdderProject(t, root)

	// Compile the C source to an object and archive it with the same selected
	// Clang and the platform archiver before the CLI build, so the archive is
	// a genuinely precompiled input.
	staging := t.TempDir()
	object := filepath.Join(staging, "adder.o")
	archive := filepath.Join(staging, "adder.a")
	compile, err := selected.CompileOneDialect("x86_64-linux-gnu", "c17", []string{"-I", filepath.Join(root, "native")}, filepath.Join(root, "native", "adder.c"), object)
	if err != nil || compile.ExitCode != 0 {
		t.Fatalf("probe compile failed: %v\n%s", err, compile.Stderr)
	}
	archiver, err := exec.LookPath("ar")
	if err != nil {
		t.Fatalf("toolchain-dependent CLI test needs ar on PATH: %v", err)
	}
	if out, err := exec.Command(archiver, "rcs", archive, object).CombinedOutput(); err != nil {
		t.Fatalf("probe archive failed: %v\n%s", err, out)
	}

	output := filepath.Join(root, "adder")
	err = run([]string{
		"build",
		"-cc", selected.Exe,
		"-target", "x86_64-linux-gnu",
		"-root", root,
		"-entry", "main.hex",
		"-out", output,
		"-c-include", "native",
		"-archive", archive,
	})
	if err != nil {
		t.Fatalf("hexal build failed: %v", err)
	}
	combined, err := exec.Command(output).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v\n%s", output, err, combined)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
}

// cliBackend resolves the installed Clang for the tagged CLI tests. The
// HEXAL_CLANG override names one exact executable; otherwise `clang` is
// resolved from PATH. Clang 18 or newer is required.
func cliBackend(t *testing.T) *backend.Backend {
	t.Helper()
	exe := os.Getenv("HEXAL_CLANG")
	if exe == "" {
		found, err := exec.LookPath("clang")
		if err != nil {
			t.Fatalf("toolchain-dependent CLI test needs clang on PATH or HEXAL_CLANG set: %v", err)
		}
		exe = found
	}
	selected, err := backend.NewBackend(exe)
	if err != nil {
		t.Fatalf("toolchain-dependent CLI test needs a usable Clang at %s: %v", exe, err)
	}
	if selected.Major < 18 {
		t.Fatalf("toolchain-dependent CLI test needs Clang 18 or newer; %s reports %q", exe, selected.Version)
	}
	return selected
}
