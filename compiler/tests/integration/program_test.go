package integration

// The std/program path and parallelism queries and the std/entropy secure
// random fill: core-library import resolution, demand selection, per-module
// adapters, and the Task-parking bridge.

import (
	"slices"
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

const (
	programImport = "import\n    Prog from \"std/program\"\nend\n"
	entropyImport = "import\n    Ent from \"std/entropy\"\nend\n"
)

func TestCorelibImportResolutionDiagnostics(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"import\n    X from \"other/thing\"\nend\nvalue: Int32 := 1\n", "unknown module collection other; only std is available"},
		{"import\n    X from \"std/nope\"\nend\nvalue: Int32 := 1\n", "unknown stdlib module \"std/nope\""},
		{"import\n    X from \"std/program.hex\"\nend\nvalue: Int32 := 1\n", "must not end in .hex"},
		{programImport + "x := Prog.nope()\n", "nope"},
		{programImport + "x := Prog.current_directory()\n", "current_directory expects 1 argument(s); got 0"},
		{programImport + "x := Prog.available_parallelism(1)\n", "available_parallelism expects 0 argument(s); got 1"},
	} {
		assertRejects(t, testCase.source, testCase.want)
	}
}

// The four path queries and available parallelism select one program
// component, link libuv and mimalloc, and expose no libuv name in any module
// header.
func TestProgramPathQueriesSelectOneComponent(t *testing.T) {
	source := programImport +
		"h: Heap := Heap()\n" +
		"cwd := Prog.current_directory(h)\n" +
		"home := Prog.home_directory(h)\n" +
		"tmp := Prog.temporary_directory(h)\n" +
		"exe := Prog.executable_path(h)\n"
	result := assertCompiles(t, source)
	programC := moduleFile(t, result, "hexal/program.c")
	programH := moduleFile(t, result, "hexal/program.h")
	for _, want := range []string{"uv_cwd", "uv_os_homedir", "uv_os_tmpdir", "uv_exepath", "uv_once(&hex_program_home_temp_once", "128u * 1024u", "1024u * 1024u", "hex_handle_error_kind", "path is not valid UTF-8"} {
		if !strings.Contains(programC, want) {
			t.Errorf("hexal/program.c lacks %q", want)
		}
	}
	for _, want := range []string{"hex_program_string_result", "hex_program_current_directory", "hex_program_home_directory", "hex_program_temporary_directory", "hex_program_executable_path"} {
		if !strings.Contains(programH, want) {
			t.Errorf("hexal/program.h lacks %q", want)
		}
	}
	if strings.Contains(programH, "uv_") {
		t.Errorf("hexal/program.h exposes a libuv name:\n%s", programH)
	}
	if !slices.Equal(dependencyNames(result), []string{"libuv", "mimalloc"}) {
		t.Fatalf("dependencies = %v, want libuv and mimalloc", dependencyNames(result))
	}
	root := rootC(t, result)
	if !strings.Contains(root, "hex_program_current_directory_Error_String(hex_v_h") {
		t.Errorf("root module has no current-directory adapter call:\n%s", root)
	}
	if !strings.Contains(root, "hex_program_executable_path_Error_String(") {
		t.Errorf("root module has no executable-path adapter call:\n%s", root)
	}
}

// available_parallelism is a stateless Size query: it emits the program
// component but no result union, no adapter, and no event bridge.
func TestProgramAvailableParallelismIsDirect(t *testing.T) {
	result := assertCompiles(t, programImport+"count: Size := Prog.available_parallelism()\n")
	root := rootC(t, result)
	if !strings.Contains(root, "hex_program_available_parallelism()") {
		t.Errorf("root module lacks a direct parallelism call:\n%s", root)
	}
	if strings.Contains(root, "_Error_") {
		t.Errorf("parallelism must not produce a result union adapter:\n%s", root)
	}
	if _, exists := result.Files["hexal/event.c"]; exists {
		t.Errorf("parallelism alone selected the event bridge")
	}
	if !strings.Contains(moduleFile(t, result, "hexal/program.h"), "size_t hex_program_available_parallelism(void);") {
		t.Errorf("hexal/program.h lacks the parallelism declaration")
	}
}

// arguments alone selects the snapshot path: no libuv, no uv_setup_args, and
// a host-neutral entry that widens only under #if !defined(_WIN32).
func TestProgramArgumentsSnapshot(t *testing.T) {
	result := assertCompiles(t, programImport+"args := Prog.arguments()\n")
	if slices.Contains(dependencyNames(result), "libuv") {
		t.Fatalf("arguments alone must not select libuv; got %v", dependencyNames(result))
	}
	root := rootC(t, result)
	for _, want := range []string{
		"#if defined(_WIN32)\nint main(void) {\n#else\nint main(int argc, char **argv) {\n#endif",
		"#if defined(_WIN32)\n    hex_program_arguments_init();\n#else\n    hex_program_arguments_init(argc, argv);\n#endif",
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root module lacks %q:\n%s", want, root)
		}
	}
	if strings.Contains(root, "uv_setup_args") {
		t.Errorf("argument demand alone must not call uv_setup_args:\n%s", root)
	}
	argumentsAdapter := "hex_program_arguments_Error_Slice_String_"
	if !strings.Contains(rootH(t, result), "static inline hex_t_Error_Slice_String_ "+argumentsAdapter) {
		t.Errorf("root header lacks the argument adapter:\n%s", rootH(t, result))
	}
	programH := moduleFile(t, result, "hexal/program.h")
	if !strings.Contains(programH, "hex_program_arguments_init") || !strings.Contains(programH, "hex_program_arguments_result") {
		t.Errorf("hexal/program.h lacks the snapshot surface:\n%s", programH)
	}
}

// executable_path demand runs the native bootstrap, then exactly one
// uv_setup_args, before the first query, and widens the entry.
func TestProgramExecutablePathWidensEntry(t *testing.T) {
	result := assertCompiles(t, programImport+"h: Heap := Heap()\nexe := Prog.executable_path(h)\n")
	if !slices.Contains(dependencyNames(result), "libuv") {
		t.Fatalf("executable path must select libuv; got %v", dependencyNames(result))
	}
	root := rootC(t, result)
	for _, want := range []string{
		"extern char **uv_setup_args(int argc, char **argv);",
		"#if defined(_WIN32)\nint main(void) {\n#else\nint main(int argc, char **argv) {\n#endif",
		"hex_runtime_native_init();",
		"(void)uv_setup_args(__argc, __argv);",
		"(void)uv_setup_args(argc, argv);",
	} {
		if !strings.Contains(root, want) {
			t.Errorf("root module lacks %q:\n%s", want, root)
		}
	}
	if strings.Count(root, "uv_setup_args(") != 3 {
		t.Errorf("want exactly one declaration and two platform calls of uv_setup_args:\n%s", root)
	}
	if strings.Index(root, "hex_runtime_native_init();") > strings.Index(root, "uv_setup_args(__argc") {
		t.Errorf("native bootstrap must precede uv_setup_args:\n%s", root)
	}
}

// A qualified Windows profile always keeps int main(void) and reads the CRT
// globals, even when arguments are reachable.
func TestProgramWindowsTargetKeepsMainVoid(t *testing.T) {
	result := compiler.Compile(map[string]string{rootSourceKey: programImport + "args := Prog.arguments()\n"}, rootSourceKey, compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("windows target arguments compile failed: %v", result.Stderr)
	}
	root := rootC(t, result)
	if !strings.Contains(root, "int main(void) {") || strings.Contains(root, "char **argv") {
		t.Errorf("windows target must keep int main(void):\n%s", root)
	}
	if !strings.Contains(root, "hex_program_arguments_init();") {
		t.Errorf("windows target must read the CRT argument text:\n%s", root)
	}
}

// Entropy fill is synchronous without the scheduler and Task-parking with it;
// the chunk bound and the failure mapper are runtime-owned.
func TestEntropyFillDirectAndTask(t *testing.T) {
	fill := entropyImport +
		"h: Heap := Heap()\n" +
		"p: Ptr<mut Byte> := h.allocate<Byte>(8)\n" +
		"unsafe do\n" +
		"    view: Slice<mut Byte> := Slice<mut Byte>.from_pointer(p, 8)\n" +
		"    result := Ent.fill(view)\n" +
		"end\n"

	direct := assertCompiles(t, fill)
	entropyC := moduleFile(t, direct, "hexal/entropy.c")
	for _, want := range []string{"#define HEX_ENTROPY_MAX_CHUNK ((size_t)0x7fffffff)", "uv_random(", "hex_handle_error_kind", "if (length == 0)"} {
		if !strings.Contains(entropyC, want) {
			t.Errorf("hexal/entropy.c lacks %q", want)
		}
	}
	if _, exists := direct.Files["hexal/event.c"]; exists {
		t.Errorf("entropy without the scheduler selected the event bridge")
	}
	directAdapter := rootH(t, direct)
	if !strings.Contains(directAdapter, "hex_entropy_fill_result query = hex_entropy_fill(into.data, into.length);") {
		t.Errorf("direct entropy adapter is not synchronous:\n%s", directAdapter)
	}

	tasked := assertCompiles(t, fill+"fun work() do\n    Task.yield()\nend\n")
	if _, exists := tasked.Files["hexal/event.c"]; !exists {
		t.Fatalf("entropy inside the scheduler must select the event bridge")
	}
	if !strings.Contains(moduleFile(t, tasked, "hexal/entropy.c"), "hex_entropy_fill_result hex_entropy_fill_task(uint8_t *data, size_t length)") {
		t.Errorf("hexal/entropy.c lacks the Task-parking variant")
	}
	if !strings.Contains(rootH(t, tasked), "query = hex_entropy_fill_task(into.data, into.length);") {
		t.Errorf("Task entropy adapter is not Task-parking:\n%s", rootH(t, tasked))
	}
}

// Importing a core library without calling it emits no component.
func TestCorelibImportAloneEmitsNothing(t *testing.T) {
	result := assertCompiles(t, "import\n    Prog from \"std/program\",\n    Ent from \"std/entropy\"\nend\nvalue: Int32 := 1\n")
	for _, key := range []string{"hexal/program.c", "hexal/entropy.c", "hexal/event.c"} {
		if _, exists := result.Files[key]; exists {
			t.Errorf("unused core-library import emitted %s", key)
		}
	}
	if len(result.Dependencies) != 0 {
		t.Errorf("unused core-library import selected dependencies %v", dependencyNames(result))
	}
}
