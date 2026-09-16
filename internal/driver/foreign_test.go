package driver

// Pure-Go tests for RFC 0192's configuration model: environment parsing,
// validation, ordering, and rooted path resolution. They invoke no external
// tool, so they run in the ordinary suite.

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestParseEnvironmentOverrides(t *testing.T) {
	overrides, failure := parseEnvironmentOverrides([]string{"FOO=bar", "EMPTY=", "EQ=a=b=c"})
	if failure != nil {
		t.Fatalf("valid overrides rejected: %v", failure)
	}
	if len(overrides) != 3 {
		t.Fatalf("overrides = %d, want 3", len(overrides))
	}
	if overrides[1].Value != "" || overrides[2].Value != "a=b=c" {
		t.Fatalf("values = %q, %q; want empty and a=b=c", overrides[1].Value, overrides[2].Value)
	}

	for _, testCase := range []struct{ entry, want string }{
		{"NOEQUALS", "-c-env requires NAME=VALUE"},
		{"=value", "invalid C environment variable name"},
		{"FOO\x00=value", "invalid C environment variable name"},
		{"FOO=bad\x00value", "invalid C environment value for FOO"},
	} {
		_, rejected := parseEnvironmentOverrides([]string{testCase.entry})
		if rejected == nil || !strings.Contains(rejected.Message, testCase.want) {
			t.Errorf("parseEnvironmentOverrides(%q) = %v, want containing %q", testCase.entry, rejected, testCase.want)
		}
	}

	_, repeated := parseEnvironmentOverrides([]string{"FOO=a", "FOO=b"})
	if repeated == nil || !strings.Contains(repeated.Message, "is repeated") {
		t.Fatalf("repeated name = %v, want a repetition diagnostic", repeated)
	}

	// Host key semantics: Windows folds ASCII case, POSIX does not.
	_, folded := parseEnvironmentOverrides([]string{"PATH=a", "Path=b"})
	if runtime.GOOS == "windows" {
		if folded == nil {
			t.Fatal("Windows must treat PATH and Path as one override name")
		}
	} else if folded != nil {
		t.Fatalf("POSIX must treat PATH and Path as distinct: %v", folded)
	}
}

func TestEffectiveEnvironmentOrderingAndDedup(t *testing.T) {
	inherited := []string{"B=2", "A=1", "C=3"}
	overrides, failure := parseEnvironmentOverrides([]string{"B=override"})
	if failure != nil {
		t.Fatal(failure)
	}
	environment := effectiveEnvironment(inherited, overrides)
	if len(environment) != 3 {
		t.Fatalf("environment = %v, want three entries", environment)
	}
	// Deterministic order, no duplicate keys, the override replaces the
	// inherited value and keeps its own name spelling.
	want := []string{"A=1", "B=override", "C=3"}
	for index, entry := range want {
		if environment[index] != entry {
			t.Fatalf("environment = %v, want %v", environment, want)
		}
	}
}

func TestEffectiveEnvironmentDoesNotExpandValues(t *testing.T) {
	raw := "$HOME%~user%${OTHER}/a/../b"
	environment := effectiveEnvironment(nil, []environmentOverride{{Name: "X", Value: raw}})
	if len(environment) != 1 || environment[0] != "X="+raw {
		t.Fatalf("environment = %v, want the exact unexpanded value", environment)
	}
}

func TestValidateDefines(t *testing.T) {
	if failure := validateDefines([]string{"FOO", "BAR=1", "EMPTY=", "_x9=..."}); failure != nil {
		t.Fatalf("valid defines rejected: %v", failure)
	}
	for _, testCase := range []struct{ name, want string }{
		{"1BAD", "invalid C define"},
		{"HAS SPACE", "invalid C define"},
		{"", "invalid C define"},
	} {
		if failure := validateDefines([]string{testCase.name}); failure == nil || !strings.Contains(failure.Message, testCase.want) {
			t.Errorf("validateDefines(%q) = %v, want containing %q", testCase.name, failure, testCase.want)
		}
	}
	if failure := validateDefines([]string{"FOO=1", "FOO=2"}); failure == nil || !strings.Contains(failure.Message, "is repeated") {
		t.Fatalf("duplicate define = %v, want a repetition diagnostic", failure)
	}
}

func TestValidateSystemLibraries(t *testing.T) {
	if failure := validateSystemLibraries([]string{"user32", "gdi32", "ws2_32", "libc++.1", "a-b+c"}); failure != nil {
		t.Fatalf("valid library names rejected: %v", failure)
	}
	for _, name := range []string{"", "path/lib", `path\lib`, "lib:name", "lib name"} {
		if failure := validateSystemLibraries([]string{name}); failure == nil {
			t.Errorf("validateSystemLibraries(%q) accepted an invalid name", name)
		}
	}
}

func TestConfigureForeignDialect(t *testing.T) {
	config, failure := configureForeign(BuildOptions{})
	if failure != nil {
		t.Fatalf("default configuration rejected: %v", failure)
	}
	if config.Standard != defaultForeignDialect {
		t.Fatalf("default dialect = %q, want %q", config.Standard, defaultForeignDialect)
	}
	if config.active {
		t.Fatal("a build with no foreign option must not be active")
	}
	for _, dialect := range []string{"c89", "c99", "c11", "c17", "c23", "gnu89", "gnu17", "gnu23"} {
		if _, failure := configureForeign(BuildOptions{CStandard: dialect}); failure != nil {
			t.Errorf("dialect %q rejected: %v", dialect, failure)
		}
	}
	if _, failure := configureForeign(BuildOptions{CStandard: "c++"}); failure == nil || !strings.Contains(failure.Message, "unknown C standard") {
		t.Fatalf("invalid dialect = %v, want an unknown-standard diagnostic", failure)
	}
}

func TestResolveForeignRootedPaths(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "native"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSource(t, root, "native/adder.c", "int adder_add(void) { return 42; }\n")
	includeDir := filepath.Join(root, "native", "include")
	if err := os.MkdirAll(includeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	objectPath := filepath.Join(root, "native", "adder.obj")
	if err := os.WriteFile(objectPath, []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}

	config, failure := configureForeign(BuildOptions{
		CSources:     []string{"native/adder.c"},
		CIncludeDirs: []string{"native/include"},
		Objects:      []string{"native/adder.obj"},
	})
	if failure != nil {
		t.Fatal(failure)
	}
	if failure := config.resolveForeignRootedPaths(root, BuildOptions{
		CSources:     []string{"native/adder.c"},
		CIncludeDirs: []string{"native/include"},
		Objects:      []string{"native/adder.obj"},
	}); failure != nil {
		t.Fatalf("valid inputs rejected: %v", failure)
	}
	if !filepath.IsAbs(config.Sources[0]) || config.Sources[0] != filepath.Join(root, "native", "adder.c") {
		t.Fatalf("source = %q, want the rooted absolute path", config.Sources[0])
	}
	if !config.active {
		t.Fatal("a build with foreign sources must be active")
	}

	// A directory supplied where a file is required, and vice versa.
	if failure := newConfig().resolveForeignRootedPaths(root, BuildOptions{CSources: []string{"native/include"}}); failure == nil || !strings.Contains(failure.Message, "is not a regular file") {
		t.Fatalf("directory as source = %v, want a regular-file failure", failure)
	}
	if failure := newConfig().resolveForeignRootedPaths(root, BuildOptions{CIncludeDirs: []string{"native/adder.c"}}); failure == nil || !strings.Contains(failure.Message, "is not a directory") {
		t.Fatalf("file as include dir = %v, want a directory failure", failure)
	}
	if failure := newConfig().resolveForeignRootedPaths(root, BuildOptions{CSources: []string{"native/missing.c"}}); failure == nil || failure.Stage != StageFilesystem {
		t.Fatalf("missing source = %v, want a filesystem-stage failure", failure)
	}

	// Duplicate sources and objects are rejected; archives may repeat.
	duplicateSource := BuildOptions{CSources: []string{"native/adder.c", "./native/adder.c"}}
	if failure := newConfig().resolveForeignRootedPaths(root, duplicateSource); failure == nil || !strings.Contains(failure.Message, "duplicate C source") {
		t.Fatalf("duplicate source = %v, want a duplicate failure", failure)
	}
	duplicateObject := BuildOptions{Objects: []string{"native/adder.obj", "native/./adder.obj"}}
	if failure := newConfig().resolveForeignRootedPaths(root, duplicateObject); failure == nil || !strings.Contains(failure.Message, "duplicate object") {
		t.Fatalf("duplicate object = %v, want a duplicate failure", failure)
	}
	if failure := newConfig().resolveForeignRootedPaths(root, BuildOptions{Archives: []string{"native/adder.obj", "native/adder.obj"}}); failure != nil {
		t.Fatalf("repeated archives must survive: %v", failure)
	}
}

// newConfig returns a configured foreignConfig for path resolution in
// isolation.
func newConfig() *foreignConfig {
	config, _ := configureForeign(BuildOptions{})
	return &config
}

func TestForeignObjectNameDeterministicAndDistinct(t *testing.T) {
	first := foreignObjectName("/staging", filepath.FromSlash("/a/x/adder.c"), 0)
	second := foreignObjectName("/staging", filepath.FromSlash("/a/x/adder.c"), 0)
	if first != second {
		t.Fatalf("object names differ for one input: %q and %q", first, second)
	}
	// Equal basenames in different directories never collide.
	other := foreignObjectName("/staging", filepath.FromSlash("/b/y/adder.c"), 0)
	if first == other {
		t.Fatalf("equal basenames collided on %q", first)
	}
	// The occurrence ordinal participates.
	if foreignObjectName("/staging", filepath.FromSlash("/a/x/adder.c"), 1) == first {
		t.Fatal("occurrence ordinal did not participate in the object name")
	}
	if !strings.HasPrefix(first, filepath.Join("/staging", "foreign")) {
		t.Fatalf("object %q is not under the foreign object directory", first)
	}
}

func TestModuleCompileOptionsOrder(t *testing.T) {
	config := foreignConfig{
		IncludeDirs: []string{"/one", "/two"},
		Defines:     []string{"A", "B=2"},
	}
	want := []string{"-I/one", "-I/two", "-DA", "-DB=2"}
	got := config.moduleCompileOptions()
	if len(got) != len(want) {
		t.Fatalf("options = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("options = %v, want %v", got, want)
		}
	}
}

func TestSystemLibraryOptions(t *testing.T) {
	config := foreignConfig{SystemLibraries: []string{"user32", "gdi32"}}
	got := config.systemLibraryOptions()
	if len(got) != 2 || got[0] != "-luser32" || got[1] != "-lgdi32" {
		t.Fatalf("system library options = %v, want [-luser32 -lgdi32]", got)
	}
}
