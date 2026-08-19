package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
	"hexal/compiler/lexer"
	"hexal/compiler/parser"
)

// A Task/spawn program emits the hexal/concurrency.h and hexal/concurrency.c
// pair: the header owns the program-wide handle prelude and the runtime
// entry-point declarations, the source owns the scheduler definitions and
// process-wide state exactly once, hexal.h keeps none of the family, and the
// root module C keeps only its call sites.
func TestConcurrencyComponentEmitsHeaderAndSource(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    return task.join()\nend\n")
	files := generateOne(t, program)
	header, exists := files["hexal/concurrency.h"]
	if !exists {
		t.Fatalf("concurrency program emitted no hexal/concurrency.h: %v", files)
	}
	source, exists := files["hexal/concurrency.c"]
	if !exists {
		t.Fatalf("concurrency program emitted no hexal/concurrency.c: %v", files)
	}
	if !strings.HasPrefix(header, "#ifndef HEXAL_CONCURRENCY_H\n#define HEXAL_CONCURRENCY_H\n") || !strings.HasSuffix(header, "\n#endif\n") {
		t.Fatalf("hexal/concurrency.h lost its guard: %q", header)
	}
	// The prelude and the runtime core name no Heap, Error, or String type,
	// so the component declares no dependency beyond hexal.h (the graph is
	// the allowed edge set; actual declarations govern the includes).
	if !strings.Contains(header, "#include \"hexal.h\"") {
		t.Fatalf("hexal/concurrency.h lacks its hexal.h include: %q", header)
	}
	for _, forbidden := range []string{"#include \"hexal/heap.h\"", "#include \"hexal/error.h\""} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("hexal/concurrency.h carries an undeclared dependency %q: %q", forbidden, header)
		}
	}
	if !strings.HasPrefix(source, "/*") || !strings.Contains(source, "#include \"hexal/concurrency.h\"") {
		t.Fatalf("hexal/concurrency.c must include its matching header: %q", source)
	}
	for _, fragment := range []string{
		"typedef struct hex_task hex_task;",
		"typedef struct hex_chan hex_chan;",
		"typedef struct hex_mutex_control hex_mutex;",
		"typedef void (*hex_task_entry)(hex_task *task);",
		"typedef hex_task *hex_task_Int32;",
		"void hex_task_entry_hex_f_m3_app_square(hex_task *task);",
		"hex_task *hex_task_spawn(void (*entry)(hex_task *), size_t args_size, size_t args_align, const void *args, size_t result_size, size_t result_align);",
		"void hex_task_complete(hex_task *task);",
		"hex_mutex *hex_mutex_new(void);",
	} {
		if !strings.Contains(header, fragment) {
			t.Fatalf("hexal/concurrency.h lacks %q: %q", fragment, header)
		}
	}
	for _, fragment := range []string{
		"static void hex_scheduler_init(void) {",
		"hex_task *hex_task_spawn(hex_task_entry entry, size_t args_size, size_t args_align, const void *args, size_t result_size, size_t result_align) {",
		"void *hex_task_join(hex_task *task) {",
		"static _Thread_local hex_task *hex_current_task;",
		"static void hex_worker_loop(void *param) {",
	} {
		if strings.Count(source, fragment) != 1 {
			t.Fatalf("hexal/concurrency.c defines %q %d times, want once: %q", fragment, strings.Count(source, fragment), source)
		}
	}
	// hexal.h owns none of the concurrency family.
	for _, forbidden := range []string{"hex_scheduler_", "typedef struct hex_task", "typedef struct hex_chan", "typedef struct hex_mutex_control", "hex_task_entry_", "hex_task_spawn(", "hex_chan_send(", "hex_mutex_new("} {
		if strings.Contains(files["hexal.h"], forbidden) {
			t.Fatalf("hexal.h retains concurrency prelude text %q: %q", forbidden, files["hexal.h"])
		}
	}
	// The root module C keeps the scheduler call sites but no runtime
	// definitions, state, or platform layer.
	rootC := files["modules/app.c"]
	for _, fragment := range []string{
		"static void hex_scheduler_init(void) {",
		"static _Thread_local hex_task *hex_current_task;",
		"hex_ready_push",
		"hex_context_switch(",
		"#include <threads.h>",
	} {
		if strings.Contains(rootC, fragment) {
			t.Fatalf("modules/app.c retains scheduler runtime text %q: %q", fragment, rootC)
		}
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/concurrency.h\"") {
		t.Fatalf("modules/app.h = %q, want the concurrency component include", files["modules/app.h"])
	}
	if !strings.Contains(rootC, "hex_scheduler_init();") || !strings.Contains(rootC, "hex_task_complete(hex_root_task);") {
		t.Fatalf("modules/app.c lost the scheduler call sites: %q", rootC)
	}
}

// An atomic-only program selects the pair without the scheduler prelude or
// runtime: the header owns the Atomic typedefs, the source owns no runtime
// definition, and the module header includes the component.
func TestConcurrencyComponentAtomicOnly(t *testing.T) {
	program := checkedGeneratorSource(t, "fun run(): Bool do\n    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n    counter.store(1)\n    return counter.load() == 1\nend\n")
	files := generateOne(t, program)
	header, exists := files["hexal/concurrency.h"]
	if !exists {
		t.Fatalf("atomic-only program emitted no hexal/concurrency.h: %v", files)
	}
	// The source artifact is emitted only when it contains at least one
	// runtime core definition; an atomic-only program gets the
	// header alone.
	if _, exists := files["hexal/concurrency.c"]; exists {
		t.Fatalf("atomic-only program emitted an empty hexal/concurrency.c: %v", files)
	}
	if !strings.Contains(header, "typedef _Atomic(int32_t) hex_atomic_Int32;") {
		t.Fatalf("hexal/concurrency.h lacks the Atomic typedef: %q", header)
	}
	for _, forbidden := range []string{"typedef struct hex_task", "hex_task_entry", "hex_task_spawn", "hex_chan_", "hex_mutex_"} {
		if strings.Contains(header, forbidden) {
			t.Fatalf("atomic-only hexal/concurrency.h carries scheduler text %q: %q", forbidden, header)
		}
	}
	if strings.Contains(files["hexal.h"], "hex_atomic_") {
		t.Fatalf("hexal.h retains the Atomic typedef: %q", files["hexal.h"])
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/concurrency.h\"") {
		t.Fatalf("modules/app.h = %q, want the concurrency component include", files["modules/app.h"])
	}
}

// A scalar-only program selects no concurrency artifact and no module
// includes the component.
func TestConcurrencyComponentAbsentWithoutConcurrency(t *testing.T) {
	program := checkedGeneratorSource(t, "x: Int32 = 1\n")
	files := generateOne(t, program)
	for _, key := range []string{"hexal/concurrency.h", "hexal/concurrency.c"} {
		if _, exists := files[key]; exists {
			t.Fatalf("scalar-only program emitted %s: %v", key, files)
		}
	}
	if strings.Contains(files["modules/app.h"], "hexal/concurrency.h") {
		t.Fatalf("modules/app.h = %q, must not include an unselected component", files["modules/app.h"])
	}
}

// Concurrency use in one module selects the component for that module only;
// the unrelated module's header stays clean while the program-wide pair
// carries the machinery.
func TestConcurrencyComponentSelectionIsModuleLocal(t *testing.T) {
	parsed := make(map[string]parser.Program, 2)
	for key, source := range map[string]string{
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 | Error = Math.compute()\n",
		"math.hex": "fun double(v: Int32): Int32 do\n    return v * 2\nend\nexport fun compute(): Int32 | Error do\n    task: Task<Int32> = try spawn double(21)\n    return task.join()\nend\n",
	} {
		tokens, err := lexer.Lex(source)
		if err != nil {
			t.Fatalf("Lex(%q) error = %v", key, err)
		}
		program, err := parser.Parse(tokens)
		if err != nil {
			t.Fatalf("Parse(%q) error = %v", key, err)
		}
		parsed[key] = program
	}
	graph := moduleGraphOf("app", []string{"math", "app"}, parsed, map[string][]checker.ModuleEdge{"app": {{Alias: "Math", Target: "math"}}})
	programs, err := checker.CheckModules(graph)
	if err != nil {
		t.Fatalf("CheckModules() error = %v", err)
	}
	files, err := GenerateChecked(graph, programs, Config{})
	if err != nil {
		t.Fatalf("GenerateChecked() error = %v", err)
	}
	if _, exists := files["hexal/concurrency.c"]; !exists {
		t.Fatalf("program-wide concurrency pair missing: %v", files)
	}
	if !strings.Contains(files["modules/math.h"], "#include \"hexal/concurrency.h\"") {
		t.Fatalf("modules/math.h = %q, want the component include", files["modules/math.h"])
	}
	if strings.Contains(files["modules/app.h"], "hexal/concurrency.h") {
		t.Fatalf("modules/app.h = %q, must not include a component selected only by math", files["modules/app.h"])
	}
}

// Equivalent compilations render identical concurrency artifacts.
// The templates render structurally from the typed model: pre-sorted handle
// records become the typedef lines, the SpawnEntries the entry prototypes,
// and each source-core flag its own section. Records render in the model's
// order, so the builder must pre-sort.
func TestConcurrencyTemplatesRenderModel(t *testing.T) {
	model := concurrencyHeaderModel{
		Scheduler:    true,
		Tasks:        []string{"Bool", "Int32"},
		Channels:     []string{"Int32"},
		Atomics:      []concurrencyAtomicModel{{Suffix: "Int32", Element: "int32_t"}},
		SpawnEntries: []string{"hex_f_m3_app_worker"},
	}
	header, err := renderComponent(componentArtifact{key: "hexal/concurrency.h", template: "concurrency.h", model: model})
	if err != nil {
		t.Fatalf("concurrency.h render error = %v", err)
	}
	for _, fragment := range []string{
		"typedef hex_task *hex_task_Bool;\ntypedef hex_task *hex_task_Int32;",
		"typedef hex_chan *hex_channel_Int32;",
		"typedef _Atomic(int32_t) hex_atomic_Int32;",
		"void hex_task_entry_hex_f_m3_app_worker(hex_task *task);",
		"void hex_mutex_free(hex_mutex *mutex);",
	} {
		if !strings.Contains(header, fragment) {
			t.Fatalf("hexal/concurrency.h = %q, want %q", header, fragment)
		}
	}
	if !strings.HasSuffix(header, "\n#endif\n") {
		t.Fatalf("hexal/concurrency.h must end with exactly one trailing newline: %q", header)
	}
	source, err := renderComponent(componentArtifact{key: "hexal/concurrency.c", template: "concurrency.c", model: concurrencySourceModel{Scheduler: true, Channels: true, Mutex: true}})
	if err != nil {
		t.Fatalf("concurrency.c render error = %v", err)
	}
	for _, fragment := range []string{
		"static void hex_scheduler_init(void) {",
		"typedef struct hex_chan {",
		"struct hex_mutex_control {",
		"void hex_chan_free(hex_chan *channel) {",
		"void hex_mutex_free(hex_mutex *mutex) {",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("hexal/concurrency.c = %q, want %q", source, fragment)
		}
	}
	if !strings.HasSuffix(source, "}\n") {
		t.Fatalf("hexal/concurrency.c must end with exactly one trailing newline: %q", source)
	}
	// Dropping the scheduler flag removes the prelude and every runtime
	// core; the atomic-only header keeps only its typedefs.
	atomicOnly := concurrencyHeaderModel{Atomics: []concurrencyAtomicModel{{Suffix: "Int32", Element: "int32_t"}}}
	header, err = renderComponent(componentArtifact{key: "hexal/concurrency.h", template: "concurrency.h", model: atomicOnly})
	if err != nil {
		t.Fatalf("concurrency.h render error = %v", err)
	}
	if !strings.Contains(header, "typedef _Atomic(int32_t) hex_atomic_Int32;") || strings.Contains(header, "hex_task") || strings.Contains(header, "hex_chan") || strings.Contains(header, "hex_mutex") {
		t.Fatalf("atomic-only hexal/concurrency.h = %q, want only the Atomic typedefs", header)
	}
	source, err = renderComponent(componentArtifact{key: "hexal/concurrency.c", template: "concurrency.c", model: concurrencySourceModel{}})
	if err != nil {
		t.Fatalf("concurrency.c render error = %v", err)
	}
	if strings.Contains(source, "hex_scheduler_init") || strings.Contains(source, "hex_chan_") || strings.Contains(source, "hex_mutex_") {
		t.Fatalf("hexal/concurrency.c = %q, must not spell a runtime core without selection", source)
	}
}

// A render model missing a field referenced by the concurrency templates
// fails closed under missingkey=error.
func TestConcurrencyTemplateMissingFieldFailsClosed(t *testing.T) {
	_, err := renderComponent(componentArtifact{
		key:      "hexal/concurrency.h",
		template: "concurrency.h",
		model:    struct{ Missing string }{Missing: "x"},
	})
	if err == nil {
		t.Fatal("concurrency.h render with a model missing the template field must fail")
	}
}

// Every nesting shape — top level, if, while, nested if inside while, and
// the for-in body — emits exactly one hex_task_spawn site in the module C:
// the hoisted function-scope copy that spawned an extra task and leaked it
// must not exist.
func TestGenerateSpawnNestingShapesEmitOneSite(t *testing.T) {
	spawned := "fun square(value: Int32): Int32 do\n    return value * value\nend\n"
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"top level", spawned + "fun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    return task.join()\nend\n"},
		{"if", spawned + "fun run(): Int32 | Error do\n    if true then\n        task: Task<Int32> = try spawn square(6)\n        task.join()\n    end\n    return 0\nend\n"},
		{"while", spawned + "fun run(): Int32 | Error do\n    mut n: Int32 = 1\n    while n > 0 do\n        task: Task<Int32> = try spawn square(6)\n        task.join()\n        n = n - 1\n    end\n    return 0\nend\n"},
		{"nested if inside while", spawned + "fun run(): Int32 | Error do\n    mut n: Int32 = 1\n    while n > 0 do\n        if n > 0 then\n            task: Task<Int32> = try spawn square(6)\n            task.join()\n        end\n        n = n - 1\n    end\n    return 0\nend\n"},
		{"for", "fun burn(value: Int64): Int64 do\n    return value\nend\nfun run(): Int64 | Error do\n    a: Array<Int64, 3> = [1, 2, 3]\n    for v in a do\n        w: Task<Int64> = try spawn burn(v)\n        w.join()\n    end\n    return 0\nend\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			program := checkedGeneratorSource(t, testCase.source)
			files := generateOne(t, program)
			rootC := files["modules/app.c"]
			if count := strings.Count(rootC, "hex_task_spawn("); count != 1 {
				t.Fatalf("spawn site count = %d, want 1:\n%s", count, rootC)
			}
		})
	}
}

// The POSIX fiber stack maps the usable region plus one PROT_NONE guard
// page at its low end: the runtime mprotects the guard, names only the
// usable region above it in ss_sp/ss_size, and tears the whole mapping
// down with munmap.
func TestConcurrencyPosixStackHasGuardPage(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    return task.join()\nend\n")
	files := generateOne(t, program)
	source := files["hexal/concurrency.c"]
	for _, fragment := range []string{
		"mmap(nullptr, stack_size + page_size, PROT_READ | PROT_WRITE,",
		"mprotect(region, page_size, PROT_NONE)",
		"context->context.uc_stack.ss_sp = (char *)region + page_size;",
		"context->context.uc_stack.ss_size = stack_size;",
		"munmap(context->stack, context->stack_mapping_size);",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("hexal/concurrency.c lacks %q:\n%s", fragment, source)
		}
	}
}
