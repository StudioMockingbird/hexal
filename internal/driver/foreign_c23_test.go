//go:build c23

package driver

// RFC 0192 end-to-end: compile and link explicitly supplied C sources,
// precompiled objects, static archives, and named system libraries through the
// public Build API and the pinned backend. Tagged like the rest of this
// repository's toolchain-dependent suites.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// adderFixture writes one handwritten binding, one C header, and one C source
// under root, and returns the binding's include directory.
func adderFixture(t *testing.T, root string) string {
	t.Helper()
	native := filepath.Join(root, "native")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSource(t, root, "native/adder.h", "#ifndef ADDER_H\n#define ADDER_H\n#include <stdint.h>\nint32_t adder_add(int32_t left, int32_t right);\n#endif\n")
	writeSource(t, root, "native/adder.c", "#include \"adder.h\"\nint32_t adder_add(int32_t left, int32_t right) {\n    return left + right;\n}\n")
	writeSource(t, root, "binding.hex", "extern c from \"adder.h\" do\n"+
		"    fun adder_add(left: Int32 as \"int32_t\", right: Int32 as \"int32_t\"): Int32 as \"int32_t\"\n"+
		"end\n"+
		"export\n    adder_add\nend\n")
	writeSource(t, root, "main.hex", "import\n    Adder from \"./binding\"\nend\n"+
		"mut total: Int32 := 0\n"+
		"unsafe do\n    total = Adder.adder_add(20, 22)\nend\n"+
		"print(total)\n")
	return native
}

// TestForeignSourceBuildRuns is RFC 0192's minimal end-to-end: one C source,
// one header reachable through -c-include, and a handwritten binding. The
// executable writes exactly 42.
func TestForeignSourceBuildRuns(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := adderFixture(t, dir)

	result, err := Build(BuildOptions{
		Root:         dir,
		CSources:     []string{"native/adder.c"},
		CIncludeDirs: []string{native},
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
	// A foreign build compiles the source with the declared dialect and links
	// its object.
	var sawForeignCompile, sawForeignLinkObject bool
	for _, command := range result.Commands {
		if command.Stage == StageCompile && strings.Contains(strings.Join(command.Arguments, " "), "adder.c") {
			sawForeignCompile = true
			if !strings.Contains(strings.Join(command.Arguments, " "), "-std=c17") {
				t.Errorf("foreign compile did not use the default dialect: %v", command.Arguments)
			}
		}
		if command.Stage == StageLink && strings.Contains(strings.Join(command.Arguments, " "), "foreign") {
			sawForeignLinkObject = true
		}
	}
	if !sawForeignCompile {
		t.Error("no foreign compile command was recorded")
	}
	if !sawForeignLinkObject {
		t.Error("the foreign object did not reach the link command")
	}
	assertStagingRemoved(t, dir)
}

// TestForeignHeaderOnlyBuild proves a header-only binding links with no
// foreign source.
func TestForeignHeaderOnlyBuild(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := filepath.Join(dir, "native")
	if err := os.MkdirAll(native, 0o755); err != nil {
		t.Fatal(err)
	}
	// A header-only function defined static inline in the header.
	writeSource(t, dir, "native/adder.h", "#ifndef ADDER_H\n#define ADDER_H\n#include <stdint.h>\nstatic inline int32_t adder_add(int32_t left, int32_t right) { return left + right; }\n#endif\n")
	writeSource(t, dir, "binding.hex", "extern c from \"adder.h\" do\n"+
		"    fun adder_add(left: Int32 as \"int32_t\", right: Int32 as \"int32_t\"): Int32 as \"int32_t\"\n"+
		"end\n"+
		"export\n    adder_add\nend\n")
	writeSource(t, dir, "main.hex", "import\n    Adder from \"./binding\"\nend\n"+
		"mut total: Int32 := 0\n"+
		"unsafe do\n    total = Adder.adder_add(20, 22)\nend\n"+
		"print(total)\n")

	result, err := Build(BuildOptions{Root: dir, CIncludeDirs: []string{native}})
	if err != nil {
		t.Fatalf("header-only build failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
}

// TestForeignObjectAndArchiveLink compiles the same C source to an object and
// then to a static archive, and links each in a separate build.
func TestForeignObjectAndArchiveLink(t *testing.T) {
	selected := requireBackend(t)
	for _, form := range []string{"object", "archive"} {
		t.Run(form, func(t *testing.T) {
			dir := t.TempDir()
			native := adderFixture(t, dir)
			source := filepath.Join(native, "adder.c")
			staging := t.TempDir()
			object := filepath.Join(staging, "adder.o")
			compile, err := selected.CompileOneDialect(qualifiedTriple, "c17", []string{"-I", native}, source, object)
			if err != nil || compile.ExitCode != 0 {
				t.Fatalf("probe compile failed: %v\n%s", err, compile.Stderr)
			}
			options := BuildOptions{Root: dir, CIncludeDirs: []string{native}}
			switch form {
			case "object":
				options.Objects = []string{object}
			case "archive":
				archive := filepath.Join(staging, "adder.a")
				archived, err := selected.ArchiveObjects([]string{object}, archive)
				if err != nil || archived.ExitCode != 0 {
					t.Fatalf("probe archive failed: %v\n%s", err, archived.Stderr)
				}
				options.Archives = []string{archive}
			}
			result, err := Build(options)
			if err != nil {
				t.Fatalf("%s build failed: %v", form, err)
			}
			combined, err := exec.Command(result.Executable).CombinedOutput()
			if err != nil {
				t.Fatalf("running %s failed: %v", result.Executable, err)
			}
			if got := string(combined); got != "42" {
				t.Fatalf("output = %q, want %q", got, "42")
			}
		})
	}
}

// TestForeignSystemLibraryTranslation proves the backend translates a logical
// system-library name without the CLI inventing its filename.
func TestForeignSystemLibraryTranslation(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "print(\"ok\")\n")
	result, err := Build(BuildOptions{Root: dir, SystemLibraries: []string{"user32", "ws2_32"}})
	if err != nil {
		t.Fatalf("build with system libraries failed: %v", err)
	}
	var linkArguments string
	for _, command := range result.Commands {
		if command.Stage == StageLink {
			linkArguments = strings.Join(command.Arguments, " ")
		}
	}
	if !strings.Contains(linkArguments, "-luser32") || !strings.Contains(linkArguments, "-lws2_32") {
		t.Fatalf("link command lacks translated system libraries: %q", linkArguments)
	}
	if strings.Contains(linkArguments, "user32.lib") {
		t.Fatalf("the driver synthesized a platform filename: %q", linkArguments)
	}
}

// TestForeignEnvironmentOverrideIsRecordedAndSecretSafe proves one override
// reaches every external invocation by name and that no argument or record
// exposes its value.
func TestForeignEnvironmentOverrideIsRecordedAndSecretSafe(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "print(\"ok\")\n")
	const secret = "hexal-secret-value-do-not-leak"
	result, err := Build(BuildOptions{Root: dir, CEnvironment: []string{"HEXAL_PROBE=" + secret}})
	if err != nil {
		t.Fatalf("build with an environment override failed: %v", err)
	}
	if len(result.Commands) == 0 {
		t.Fatal("no command records were produced")
	}
	for _, command := range result.Commands {
		if len(command.EnvironmentOverrides) != 1 || command.EnvironmentOverrides[0] != "HEXAL_PROBE" {
			t.Fatalf("command %s lacks the override name: %v", command.Tool, command.EnvironmentOverrides)
		}
		if strings.Contains(strings.Join(command.Arguments, " "), secret) {
			t.Fatalf("command arguments exposed the override value: %v", command.Arguments)
		}
		if strings.Contains(command.Stdout, secret) || strings.Contains(command.Stderr, secret) {
			t.Fatal("command streams exposed the override value")
		}
	}
}

// TestForeignCompileFailureStopsTheBuild proves a failing foreign compile
// records its exact command and prevents linking.
func TestForeignCompileFailureStopsTheBuild(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := adderFixture(t, dir)
	writeSource(t, dir, "native/adder.c", "this is not valid C\n")

	failed, err := Build(BuildOptions{Root: dir, CSources: []string{"native/adder.c"}, CIncludeDirs: []string{native}})
	if err == nil {
		t.Fatal("a failing foreign compile must fail the build")
	}
	buildErr, ok := err.(*BuildError)
	if !ok || buildErr.Stage != StageCompile {
		t.Fatalf("error = %#v, want a C-compilation failure", err)
	}
	if buildErr.Command == nil || !strings.Contains(strings.Join(buildErr.Command.Arguments, " "), "adder.c") {
		t.Fatalf("failure lacks the exact foreign compile command: %#v", buildErr.Command)
	}
	for _, command := range failed.Commands {
		if command.Stage == StageLink {
			t.Fatal("linking ran after a foreign compile failure")
		}
	}
}

// TestForeignDialectSelection proves -c-standard reaches the foreign compile
// and generated Hexal stays C23.
func TestForeignDialectSelection(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := adderFixture(t, dir)
	// C11 permits this source; the point is the selected spelling.
	result, err := Build(BuildOptions{
		Root:         dir,
		CSources:     []string{"native/adder.c"},
		CIncludeDirs: []string{native},
		CStandard:    "c11",
	})
	if err != nil {
		t.Fatalf("C11 build failed: %v", err)
	}
	var foreignArguments, moduleArguments []string
	for _, command := range result.Commands {
		joined := strings.Join(command.Arguments, " ")
		if command.Stage != StageCompile {
			continue
		}
		if strings.Contains(joined, "adder.c") {
			foreignArguments = command.Arguments
		}
		if strings.Contains(joined, "modules") && strings.HasSuffix(joined, ".c") {
			moduleArguments = command.Arguments
		}
	}
	if !strings.Contains(strings.Join(foreignArguments, " "), "-std=c11") {
		t.Fatalf("foreign compile did not use C11: %v", foreignArguments)
	}
	if len(moduleArguments) > 0 && !strings.Contains(strings.Join(moduleArguments, " "), "-std=c23") {
		// The backend receives the dialect through CompileOne's own -std=c23.
		t.Fatalf("generated module compile did not use C23: %v", moduleArguments)
	}
}

// TestForeignIncludeAndDefineReachModulesOnly proves -c-include and -c-define
// reach generated module translation units and foreign sources, never
// compiler-owned runtime components.
func TestForeignIncludeAndDefineReachModulesOnly(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := adderFixture(t, dir)
	result, err := Build(BuildOptions{
		Root:         dir,
		CSources:     []string{"native/adder.c"},
		CIncludeDirs: []string{native},
		CDefines:     []string{"HEXAL_PROBE_DEFINE=1"},
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	var sawModule, sawComponent bool
	for _, command := range result.Commands {
		if command.Stage != StageCompile {
			continue
		}
		source := command.Arguments[len(command.Arguments)-3] // ... -c <source> -o <object>
		relative, relErr := filepath.Rel(command.WorkingDirectory, source)
		if relErr != nil {
			continue
		}
		relative = filepath.ToSlash(relative)
		joined := strings.Join(command.Arguments, " ")
		if strings.HasPrefix(relative, "modules/") {
			sawModule = true
			if !strings.Contains(joined, "-DHEXAL_PROBE_DEFINE=1") {
				t.Errorf("module compile lacks the user define: %v", command.Arguments)
			}
			if !strings.Contains(joined, "-I"+filepath.Clean(native)) {
				t.Errorf("module compile lacks the user include: %v", command.Arguments)
			}
		}
		if strings.HasPrefix(relative, "hexal/") {
			sawComponent = true
			if strings.Contains(joined, "HEXAL_PROBE_DEFINE") {
				t.Errorf("compiler-owned component compile received a user define: %v", command.Arguments)
			}
		}
	}
	if !sawModule || !sawComponent {
		t.Fatalf("expected both module and component compiles; module=%v component=%v", sawModule, sawComponent)
	}
}

// TestForeignMultipleEqualBasenames proves several sources compile separately
// in occurrence order, and equal basenames in different directories receive
// distinct objects that both reach the link.
func TestForeignMultipleEqualBasenames(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeCLIFileForDriver(t, dir, "native/layout.h", "#ifndef LAYOUT_H\n#define LAYOUT_H\n#include <stdint.h>\nint32_t first_value(void);\nint32_t second_value(void);\n#endif\n")
	writeCLIFileForDriver(t, dir, "native/a/adder.c", "#include \"layout.h\"\nint32_t first_value(void) { return 20; }\n")
	writeCLIFileForDriver(t, dir, "native/b/adder.c", "#include \"layout.h\"\nint32_t second_value(void) { return 22; }\n")
	writeSource(t, dir, "binding.hex", "extern c from \"layout.h\" do\n"+
		"    fun first_value(): Int32 as \"int32_t\"\n"+
		"    fun second_value(): Int32 as \"int32_t\"\n"+
		"end\n"+
		"export\n    first_value,\n    second_value\nend\n")
	writeSource(t, dir, "main.hex", "import\n    Native from \"./binding\"\nend\n"+
		"mut total: Int32 := 0\n"+
		"unsafe do\n    total = Native.first_value() + Native.second_value()\nend\n"+
		"print(total)\n")

	result, err := Build(BuildOptions{
		Root:         dir,
		CSources:     []string{"native/a/adder.c", "native/b/adder.c"},
		CIncludeDirs: []string{"native"},
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
	var foreignObjects []string
	var linkArguments []string
	for _, command := range result.Commands {
		joined := strings.Join(command.Arguments, " ")
		if command.Stage == StageCompile && strings.Contains(joined, "adder.c") && strings.Contains(joined, "foreign") {
			foreignObjects = append(foreignObjects, command.Arguments[len(command.Arguments)-1])
		}
		if command.Stage == StageLink {
			linkArguments = command.Arguments
		}
	}
	if len(foreignObjects) != 2 {
		t.Fatalf("foreign objects = %v, want two", foreignObjects)
	}
	if foreignObjects[0] == foreignObjects[1] {
		t.Fatalf("equal basenames collided on one object: %q", foreignObjects[0])
	}
	for _, object := range foreignObjects {
		if !strings.Contains(strings.Join(linkArguments, " "), object) {
			t.Fatalf("foreign object %q did not reach the link", object)
		}
	}
}

// writeCLIFileForDriver writes one fixture file, creating parent directories.
func writeCLIFileForDriver(t *testing.T, root, name, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestForeignMacroControlledLayout proves one -c-define reaches both sides of
// the binding, so a macro-controlled record has one consistent layout.
func TestForeignMacroControlledLayout(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	writeCLIFileForDriver(t, dir, "native/layout.h", "#ifndef LAYOUT_H\n#define LAYOUT_H\n#include <stdint.h>\n"+
		"typedef struct pair {\n"+
		"#ifdef USE_WIDE\n    int64_t left;\n    int64_t right;\n#else\n    int32_t left;\n    int32_t right;\n#endif\n"+
		"} pair;\npair pair_make(int64_t left, int64_t right);\n#endif\n")
	writeCLIFileForDriver(t, dir, "native/layout.c", "#include \"layout.h\"\npair pair_make(int64_t left, int64_t right) {\n    pair result;\n    result.left = left;\n    result.right = right;\n    return result;\n}\n")
	writeSource(t, dir, "binding.hex", "extern c from \"layout.h\" do\n"+
		"    type Pair as \"pair\" is struct\n"+
		"        mut left: Int64,\n"+
		"        mut right: Int64,\n"+
		"    end\n"+
		"    fun pair_make as \"pair_make\"(left: Int64 as \"int64_t\", right: Int64 as \"int64_t\"): Pair\n"+
		"end\n"+
		"export\n    Pair,\n    pair_make\nend\n")
	writeSource(t, dir, "main.hex", "import\n    Native from \"./binding\"\nend\n"+
		"mut total: Int64 := 0\n"+
		"unsafe do\n    result: Native.Pair := Native.pair_make(40, 2)\n    total = result.left + result.right\nend\n"+
		"print(total)\n")

	result, err := Build(BuildOptions{
		Root:         dir,
		CSources:     []string{"native/layout.c"},
		CIncludeDirs: []string{"native"},
		CDefines:     []string{"USE_WIDE=1"},
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	combined, err := exec.Command(result.Executable).CombinedOutput()
	if err != nil {
		t.Fatalf("running %s failed: %v", result.Executable, err)
	}
	if got := string(combined); got != "42" {
		t.Fatalf("output = %q, want %q", got, "42")
	}
}

// TestForeignLinkGroupOrder asserts the five link groups in order: generated
// objects, compiled foreign objects, precompiled objects, static archives,
// then named system libraries.
func TestForeignLinkGroupOrder(t *testing.T) {
	selected := requireBackend(t)
	dir := t.TempDir()
	native := adderFixture(t, dir)
	staging := t.TempDir()
	object := filepath.Join(staging, "stub.o")
	writeCLIFileForDriver(t, staging, "stub.c", "int hexal_stub(void) { return 1; }\n")
	compile := exec.Command(selected.Exe, "cc", "-std=c17", "-target", qualifiedTriple, "-c", filepath.Join(staging, "stub.c"), "-o", object)
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("stub compile failed: %v\n%s", err, out)
	}
	archive := filepath.Join(staging, "stub.a")
	archived := exec.Command(selected.Exe, "ar", "rcs", archive, object)
	if out, err := archived.CombinedOutput(); err != nil {
		t.Fatalf("stub archive failed: %v\n%s", err, out)
	}

	result, err := Build(BuildOptions{
		Root:            dir,
		CSources:        []string{"native/adder.c"},
		CIncludeDirs:    []string{native},
		Objects:         []string{object},
		Archives:        []string{archive},
		SystemLibraries: []string{"user32", "user32"},
	})
	if err != nil {
		t.Fatalf("build failed: %v", err)
	}
	var arguments []string
	for _, command := range result.Commands {
		if command.Stage == StageLink {
			arguments = command.Arguments
		}
	}
	position := func(needle string) int {
		for index, argument := range arguments {
			if strings.Contains(argument, needle) {
				return index
			}
		}
		return -1
	}
	foreign := position("foreign")
	precompiled := position("stub.o")
	archivedAt := position("stub.a")
	firstLibrary := position("-luser32")
	if foreign < 0 || precompiled < 0 || archivedAt < 0 || firstLibrary < 0 {
		t.Fatalf("link command lacks an input: foreign=%d object=%d archive=%d library=%d\n%v", foreign, precompiled, archivedAt, firstLibrary, arguments)
	}
	if !(foreign < precompiled && precompiled < archivedAt && archivedAt < firstLibrary) {
		t.Fatalf("link groups are out of order: foreign=%d object=%d archive=%d library=%d", foreign, precompiled, archivedAt, firstLibrary)
	}
	// A repeated system library survives: the user's own arguments are last,
	// immediately before the output option.
	if length := len(arguments); length < 4 || arguments[length-3] != "-luser32" || arguments[length-4] != "-luser32" {
		t.Fatalf("the repeated user system library did not survive at the end: %v", arguments)
	}
}

// TestForeignLinkFailurePreservesExecutable proves a link-stage failure leaves
// any existing executable untouched.
func TestForeignLinkFailurePreservesExecutable(t *testing.T) {
	selected := requireBackend(t)
	dir := t.TempDir()
	native := adderFixture(t, dir)
	staging := t.TempDir()
	object := filepath.Join(staging, "stub.o")
	writeCLIFileForDriver(t, staging, "stub.c", "int hexal_stub(void) { return 1; }\n")
	compile := exec.Command(selected.Exe, "cc", "-std=c17", "-target", qualifiedTriple, "-c", filepath.Join(staging, "stub.c"), "-o", object)
	if out, err := compile.CombinedOutput(); err != nil {
		t.Fatalf("stub compile failed: %v\n%s", err, out)
	}

	output := filepath.Join(dir, "adder.exe")
	const previous = "previously published executable bytes"
	if err := os.WriteFile(output, []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Build(BuildOptions{
		Root:         dir,
		Output:       output,
		CIncludeDirs: []string{native},
		Objects:      []string{object},
	})
	if err == nil {
		t.Fatal("a link with a missing symbol must fail")
	}
	buildErr, ok := err.(*BuildError)
	if !ok || buildErr.Stage != StageLink {
		t.Fatalf("error = %#v, want a link-stage failure", err)
	}
	preserved, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatalf("the previously published executable is gone: %v", readErr)
	}
	if string(preserved) != previous {
		t.Fatalf("the previously published executable changed: %q", preserved)
	}
}

// TestForeignBuildsUseDistinctStaging proves two foreign builds receive
// distinct private staging trees and both are cleaned.
func TestForeignBuildsUseDistinctStaging(t *testing.T) {
	requireBackend(t)
	dir := t.TempDir()
	native := adderFixture(t, dir)
	options := BuildOptions{Root: dir, CSources: []string{"native/adder.c"}, CIncludeDirs: []string{native}}

	first, err := Build(options)
	if err != nil {
		t.Fatalf("first build failed: %v", err)
	}
	second, err := Build(options)
	if err != nil {
		t.Fatalf("second build failed: %v", err)
	}
	firstStaging := first.Commands[0].WorkingDirectory
	secondStaging := second.Commands[0].WorkingDirectory
	if firstStaging == secondStaging {
		t.Fatalf("two foreign builds reused staging %q", firstStaging)
	}
	if _, err := os.Stat(firstStaging); !os.IsNotExist(err) {
		t.Fatalf("first staging %q was not removed", firstStaging)
	}
	if _, err := os.Stat(secondStaging); !os.IsNotExist(err) {
		t.Fatalf("second staging %q was not removed", secondStaging)
	}
}
