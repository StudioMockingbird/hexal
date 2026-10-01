//go:build c23

package c23validation

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/compiler/corelib"
)

// parserAdapterBuild compiles the private HTTP parser adapter and one C driver
// against the checked-in llhttp archive, with no other runtime component: the
// adapter has no libuv, scheduler, or Hexal dependency, so the link line names
// only the pinned archive. adapterFlags apply to the adapter alone, so a
// poisoned allocation name is checked against its own source and not the
// driver's use of the C library.
func parserAdapterBuild(t *testing.T, driver string, adapterFlags []string) string {
	t.Helper()
	tc := clangToolchain(t)
	buildRoot := t.TempDir()
	native := buildDependencies([]compiler.RuntimeDependency{compiler.RuntimeLlhttp}, buildRoot)
	if native.err != nil {
		t.Fatalf("materialize llhttp: %v", native.err)
	}
	templates, err := corelib.RuntimeTemplates()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(buildRoot, "parser")
	if err := os.MkdirAll(filepath.Join(dir, "hexal"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"http.h", "http.c"} {
		body, ok := templates[name]
		if !ok {
			t.Fatalf("runtime template %s is missing", name)
		}
		if strings.Contains(body, "{{") {
			t.Fatalf("%s must be plain C: the parser adapter has no template model", name)
		}
		if err := os.WriteFile(filepath.Join(dir, "hexal", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	driverText, err := os.ReadFile(filepath.Join("testdata", driver))
	if err != nil {
		t.Fatal(err)
	}
	driverPath := filepath.Join(dir, driver)
	if err := os.WriteFile(driverPath, driverText, 0o644); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(dir, "parser-cases")
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	base := []string{"-std=c23", "-Wall", "-Wextra", "-Werror", "--target=" + tc.DefaultTarget, "-I", dir}
	base = append(base, native.includeOptions...)
	ctx, cancel := context.WithTimeout(context.Background(), buildProcessTimeout)
	defer cancel()
	run := func(args ...string) {
		t.Helper()
		output, err := exec.CommandContext(ctx, tc.Command[0], args...).CombinedOutput()
		if err != nil {
			t.Fatalf("parser adapter build failed: %v\n%s", err, output)
		}
	}
	adapterObject := filepath.Join(dir, "http.o")
	driverObject := filepath.Join(dir, "driver.o")
	adapter := append(append([]string{}, base...), adapterFlags...)
	run(append(adapter, "-c", filepath.Join(dir, "hexal", "http.c"), "-o", adapterObject)...)
	run(append(append([]string{}, base...), "-c", driverPath, "-o", driverObject)...)
	link := append([]string{"--target=" + tc.DefaultTarget, adapterObject, driverObject}, native.archives...)
	link = append(link, native.linkOptions...)
	run(append(link, "-o", exe)...)
	return exe
}

// TestHTTPParserAdapterRuns executes the adapter's native cases: fragmented
// and byte-split input, head and message pauses, pipelining, bounds, framing
// and Host rejections, and EOF positions.
func TestHTTPParserAdapterRuns(t *testing.T) {
	exe := parserAdapterBuild(t, "http_parser_cases.c", nil)
	stdout, stderr, exitedZero := runProcess(t, exe)
	if !exitedZero || stderr != "" {
		t.Fatalf("parser cases failed: stdout=%q stderr=%q", stdout, stderr)
	}
	if !strings.HasPrefix(stdout, "http-parser-ok ") {
		t.Fatalf("parser cases stdout = %q, want http-parser-ok", stdout)
	}
}

// TestHTTPParserAdapterAllocatesNothing compiles the adapter with the C
// library's allocation names poisoned: the adapter source must not name one,
// and it never calls llhttp's llhttp_alloc, the only allocating entry point in
// the pinned archive.
func TestHTTPParserAdapterAllocatesNothing(t *testing.T) {
	poison := filepath.Join(t.TempDir(), "poison.h")
	if err := os.WriteFile(poison, []byte("#include <string.h>\n#include \"llhttp.h\"\n#include \"hexal/http.h\"\n#pragma GCC poison malloc calloc realloc free llhttp_alloc llhttp_free\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	parserAdapterBuild(t, "http_parser_cases.c", []string{"-include", poison})
}
