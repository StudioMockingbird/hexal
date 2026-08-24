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
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n")
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
		"extern hex_task *hex_root_task;",
		"void hex_scheduler_init(void);",
		"hex_mutex *hex_mutex_new(void);",
	} {
		if !strings.Contains(header, fragment) {
			t.Fatalf("hexal/concurrency.h lacks %q: %q", fragment, header)
		}
	}
	for _, fragment := range []string{
		"void hex_scheduler_init(void) {",
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
		"void hex_scheduler_init(void) {",
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
	program := checkedGeneratorSource(t, "fun run(): Bool do\n    counter: Atomic<Int32> := Atomic<Int32>.new(0)\n    counter.store(1)\n    return counter.load() == 1\nend\n")
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
	program := checkedGeneratorSource(t, "x: Int32 := 1\n")
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
		"app.hex":  "module Math = import \"./math\"\nresult: Int32 | Error := Math.compute()\n",
		"math.hex": "fun double(v: Int32): Int32 do\n    return v * 2\nend\nexport fun compute(): Int32 | Error do\n    task: Task<Int32> := try spawn double(21)\n    return task.join()\nend\n",
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
		"void hex_scheduler_init(void) {",
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

// Every nesting shape (top level, if, while, nested if inside while, and
// the for-in body) emits exactly one hex_task_spawn site in the module C:
// the hoisted function-scope copy that spawned an extra task and leaked it
// must not exist.
func TestGenerateSpawnNestingShapesEmitOneSite(t *testing.T) {
	spawned := "fun square(value: Int32): Int32 do\n    return value * value\nend\n"
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"top level", spawned + "fun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n"},
		{"if", spawned + "fun run(): Int32 | Error do\n    if true then\n        task: Task<Int32> := try spawn square(6)\n        task.join()\n    end\n    return 0\nend\n"},
		{"while", spawned + "fun run(): Int32 | Error do\n    mut n: Int32 := 1\n    while n > 0 do\n        task: Task<Int32> := try spawn square(6)\n        task.join()\n        n = n - 1\n    end\n    return 0\nend\n"},
		{"nested if inside while", spawned + "fun run(): Int32 | Error do\n    mut n: Int32 := 1\n    while n > 0 do\n        if n > 0 then\n            task: Task<Int32> := try spawn square(6)\n            task.join()\n        end\n        n = n - 1\n    end\n    return 0\nend\n"},
		{"for", "fun burn(value: Int64): Int64 do\n    return value\nend\nfun run(): Int64 | Error do\n    a: Array<Int64, 3> := [1, 2, 3]\n    for v in a do\n        w: Task<Int64> := try spawn burn(v)\n        w.join()\n    end\n    return 0\nend\n"},
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

// The POSIX fiber stack maps the whole reserve read-write with one PROT_NONE
// guard page at its low end: ss_sp/ss_size name the usable region above the
// guard (the reserve less one page), the mapping is demand-zero paged and
// torn down with munmap, and the initial commit is documented as a
// Windows-only knob.
func TestConcurrencyPosixStackHasGuardPage(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n")
	files := generateOne(t, program)
	source := files["hexal/concurrency.c"]
	for _, fragment := range []string{
		"mmap(nullptr, stack_size, PROT_READ | PROT_WRITE,",
		"MAP_PRIVATE | MAP_ANONYMOUS | MAP_NORESERVE",
		"mprotect(region, page_size, PROT_NONE)",
		"context->guard_end = (char *)region + page_size;",
		"context->context.uc_stack.ss_sp = (char *)region + page_size;",
		"context->context.uc_stack.ss_size = stack_size - page_size;",
		"deliberately unused",
		"munmap(context->stack, context->stack_mapping_size);",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("hexal/concurrency.c lacks %q:\n%s", fragment, source)
		}
	}
}

// The overflow trap reaches the generated runtime on both platforms: a
// per-worker alternate signal stack, a process-wide SIGSEGV/SIGBUS handler
// that names the current Task's guard range through one thread-local read,
// the structured message and clean exit on a guard fault, an unchanged
// re-raise for every other fault, and the Windows vectored exception handler
// on EXCEPTION_STACK_OVERFLOW. Every worker installs its setup.
func TestConcurrencyOverflowTrapReachesRuntime(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n")
	files := generateOne(t, program)
	source := files["hexal/concurrency.c"]
	posix := []string{
		"typedef struct hex_guard_range {",
		"static _Thread_local hex_guard_range hex_current_guard;",
		"hex_current_guard.base = to->stack;",
		"hex_current_guard.end = to->guard_end;",
		"#define HEX_GUARD_HANDLER_STACK (64 << 10)",
		"stack_t alt = {0};",
		"sigaltstack(&alt, nullptr)",
		"action.sa_sigaction = hex_guard_handler;",
		"action.sa_flags = SA_SIGINFO | SA_ONSTACK;",
		"sigaction(SIGSEGV, &action, nullptr) != 0 || sigaction(SIGBUS, &action, nullptr) != 0",
		"hex_stack_overflow_message, sizeof(hex_stack_overflow_message) - 1);",
		"_exit(1);",
		"signal(sig, SIG_DFL);",
		"raise(sig);",
		"hex_worker_guard_setup();",
	}
	win32 := []string{
		"EXCEPTION_STACK_OVERFLOW",
		"WriteFile(stderr_handle, hex_stack_overflow_message",
		"ExitProcess(1);",
		"return EXCEPTION_CONTINUE_SEARCH;",
		"AddVectoredExceptionHandler(1, hex_stack_overflow_handler)",
	}
	for _, fragment := range append(posix, win32...) {
		if !strings.Contains(source, fragment) {
			t.Fatalf("hexal/concurrency.c lacks %q:\n%s", fragment, source)
		}
	}
	if strings.Count(source, "hex_worker_guard_setup();") != 2 {
		t.Fatalf("worker guard setup must run once for worker zero and once for each spawned worker, count = %d:\n%s", strings.Count(source, "hex_worker_guard_setup();"), source)
	}
	// The faulting context cannot run hex_runtime_trap; the handler must
	// write the structured message itself.
	if !strings.Contains(source, "hex_stack_overflow_message") {
		t.Fatalf("hexal/concurrency.c lacks the shared overflow message: %s", source)
	}
	if strings.Contains(files["hexal.h"], "hex_stack_overflow_message") {
		t.Fatalf("hexal.h must not own the handler message: %s", files["hexal.h"])
	}
}

// hex_task carries exactly one atomic park phase, one nullable pending link,
// and one lifecycle mutex; no superseded state or wake_error field remains,
// and wake_result is the one generalized payload byte shared by Channel
// close and Mutex ownership transfer.
func TestConcurrencyTaskLayoutOwnsParkingAndLifecycleFieldsOnce(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n")
	files := generateOne(t, program)
	header := files["hexal/concurrency.h"]
	for _, want := range []string{
		"_Atomic(uint8_t) park_phase;",
		"void *pending_park;",
		"mtx_t lifecycle_mutex;",
		"uint8_t wake_result;",
	} {
		if strings.Count(header, want) != 1 {
			t.Fatalf("hexal/concurrency.h defines %q %d times, want exactly once: %q", want, strings.Count(header, want), header)
		}
	}
	for _, forbidden := range []string{"uint8_t state;", "wake_error", "HEX_TASK_READY", "HEX_TASK_RUNNING", "HEX_TASK_PARKED", "HEX_TASK_DONE"} {
		if strings.Contains(header, forbidden) || strings.Contains(files["hexal/concurrency.c"], forbidden) {
			t.Fatalf("concurrency artifacts retain the superseded field or constant %q", forbidden)
		}
	}
}

// The common park/commit/wake transition helpers exist exactly once each:
// no wait family duplicates the protocol with its own local variant.
func TestConcurrencyNoDuplicateParkWakeProtocol(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n")
	files := generateOne(t, program)
	source := files["hexal/concurrency.c"]
	for _, fragment := range []string{
		"static void hex_task_wake(hex_task *waiter) {",
		"static void hex_task_commit_park(hex_task *task) {",
		"static void hex_task_resume_commit(hex_task *self) {",
	} {
		if strings.Count(source, fragment) != 1 {
			t.Fatalf("hexal/concurrency.c defines %q %d times, want exactly once:\n%s", fragment, strings.Count(source, fragment), source)
		}
	}
}

// Every wait-family registration writes its pending link before
// release-storing parking, matching the required ordering under the wait
// source's own mutex. Each occurrence of the parking store is checked
// against the nearest preceding pending-link write, independent of exact
// indentation.
func TestConcurrencyPendingLinkPrecedesParkingPhaseStore(t *testing.T) {
	program := checkedGeneratorSource(t, "fun worker(ch: Channel<Int32>, m: Mutex): Bool do\n    m.lock()\n    ch.send(1)\n    m.unlock()\n    Task.yield()\n    return true\nend\nfun run(): Int32 | Error do\n    h: Heap := Heap.new()\n    ch: Channel<Int32> := try Channel<Int32>.new(h, 4)\n    m: Mutex := try Mutex.new(h)\n    task: Task<Bool> := try spawn worker(ch, m)\n    task.join()\n    return 0\nend\n")
	files := generateOne(t, program)
	source := files["hexal/concurrency.c"]
	const parkingStore = "atomic_store_explicit(&self->park_phase, HEX_PARK_PARKING, memory_order_release);"
	count := strings.Count(source, parkingStore)
	if count == 0 {
		t.Fatalf("hexal/concurrency.c has no parking-phase store to check:\n%s", source)
	}
	searchFrom := 0
	for i := 0; i < count; i++ {
		storeIndex := strings.Index(source[searchFrom:], parkingStore) + searchFrom
		linkIndex := strings.LastIndex(source[:storeIndex], "pending_park = ")
		if linkIndex < 0 {
			t.Fatalf("parking-phase store at byte %d has no preceding pending-link write:\n%s", storeIndex, source)
		}
		searchFrom = storeIndex + len(parkingStore)
	}
}

// A Mutex waiter resumed from unlock's direct ownership transfer returns
// from lock() immediately instead of re-entering acquisition, which is what
// would incorrectly trap transferred ownership as a recursive lock.
func TestConcurrencyMutexHandoffReturnsWithoutReenteringAcquisition(t *testing.T) {
	program := checkedGeneratorSource(t, "fun run(): Int32 | Error do\n    h: Heap := Heap.new()\n    m: Mutex := try Mutex.new(h)\n    m.lock()\n    m.unlock()\n    defer m.free(h)\n    return 0\nend\n")
	files := generateOne(t, program)
	source := files["hexal/concurrency.c"]
	if !strings.Contains(source, "hex_task_resume_commit(self);\n        if (self->wake_result) {\n            return;\n        }\n    }\n}") {
		t.Fatalf("hex_mutex_lock must return directly on a transferred wake_result rather than re-entering acquisition:\n%s", source)
	}
	if !strings.Contains(source, "waiter->wake_result = 1;\n        hex_task_wake(waiter);") {
		t.Fatalf("hex_mutex_unlock must mark transferred ownership before waking its selected waiter:\n%s", source)
	}
}

// The blocking pool selects only for one combination: the scheduler runtime
// (Task, Channel, or Mutex) reaching a native descriptor transfer
// (IO.read/write/seek/close) or print's descriptor write-all sink. Every
// other combination (IO alone, print alone, Task alone, Atomic beside IO, or
// Bytes beside Task) selects no pool at all.
func TestConcurrencyBlockingSelectionMatrix(t *testing.T) {
	spawnJoin := "fun square(value: Int32): Int32 do\n    return value * value\nend\n"
	for _, testCase := range []struct {
		name     string
		source   string
		blocking bool
	}{
		{
			"io only",
			"fun run(): Nil | Error do\n    out: IO := try IO.stdout()\n    w: Size | Error := out.write(\"hi\".bytes())\n    closed: Nil | Error := out.close()\n    return nil\nend\n",
			false,
		},
		{
			"print only",
			"fun demo() do\n    print(42)\nend\n",
			false,
		},
		{
			"task only",
			spawnJoin + "fun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n",
			false,
		},
		{
			"atomic plus io",
			"fun run(): Nil | Error do\n    counter: Atomic<Int32> := Atomic<Int32>.new(0)\n    counter.store(1)\n    out: IO := try IO.stdout()\n    w: Size | Error := out.write(\"hi\".bytes())\n    closed: Nil | Error := out.close()\n    return nil\nend\n",
			false,
		},
		{
			"bytes plus task",
			spawnJoin + "fun run(): Int32 | Error do\n    h: Heap := Heap.new()\n    data: List<Byte> := List<Byte>.new(h)\n    defer data.free(h)\n    dst: List<Byte> := List<Byte>.new(h)\n    defer dst.free(h)\n    mut live: Bytes := Bytes.over(data)\n    r: Size | EoS | Error := live.read(dst, 4)\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n",
			false,
		},
		{
			"task plus io",
			spawnJoin + "fun run(): Int32 | Error do\n    out: IO := try IO.stdout()\n    w: Size | Error := out.write(\"hi\".bytes())\n    closed: Nil | Error := out.close()\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n",
			true,
		},
		{
			"task plus print",
			spawnJoin + "fun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    v: Int32 := task.join()\n    print(v)\n    return 0\nend\n",
			true,
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			program := checkedGeneratorSource(t, testCase.source)
			files := generateOne(t, program)
			combined := files["hexal/concurrency.h"] + files["hexal/concurrency.c"] + files["hexal/io.c"]
			count := strings.Count(combined, "hex_blocking")
			if testCase.blocking {
				if count == 0 {
					t.Fatalf("%s: want the blocking pool selected, found no hex_blocking text", testCase.name)
				}
				if n := strings.Count(files["hexal/concurrency.c"], "static int hex_blocking_worker(void *unused) {"); n != 1 {
					t.Fatalf("%s: hex_blocking_worker defined %d times, want exactly once:\n%s", testCase.name, n, files["hexal/concurrency.c"])
				}
				if n := strings.Count(files["hexal/concurrency.c"], "static void hex_blocking_init(void) {"); n != 1 {
					t.Fatalf("%s: hex_blocking_init defined %d times, want exactly once:\n%s", testCase.name, n, files["hexal/concurrency.c"])
				}
				if !strings.Contains(files["hexal/concurrency.c"], "hex_current_task") {
					t.Fatalf("%s: hex_blocking_call must read hex_current_task in hexal/concurrency.c", testCase.name)
				}
				if strings.Contains(files["hexal/io.c"], "hex_current_task") {
					t.Fatalf("%s: hexal/io.c must never reference hex_current_task directly, only hexal/concurrency.c may:\n%s", testCase.name, files["hexal/io.c"])
				}
			} else if count != 0 {
				t.Fatalf("%s: want no blocking pool, found %d hex_blocking occurrences across concurrency.h/.c and io.c", testCase.name, count)
			}
		})
	}
}

// Pooled IO frontends must see complete job types and entry definitions before
// their first use. Generated C is checked as text because ordinary tests do
// not invoke an external C compiler.
func TestBlockingIODeclarationsPrecedeFrontendUses(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    h: Heap := Heap.new()\n    stream: IO := try IO.stdin()\n    buffer: List<Byte> := List<Byte>.new(h)\n    defer buffer.free(h)\n    transfer: Size | EoS | Error := try stream.read(buffer, 16)\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n")
	ioSource := generateOne(t, program)["hexal/io.c"]
	for _, operation := range []string{"read", "write", "seek", "close", "write_all"} {
		definition := strings.Index(ioSource, "typedef struct hex_io_"+operation+"_job")
		use := strings.Index(ioSource, "hex_io_"+operation+"_job job")
		if definition < 0 || use < 0 || definition >= use {
			t.Fatalf("hex_io_%s_job definition must precede its frontend use: definition=%d use=%d", operation, definition, use)
		}
		entryDefinition := strings.Index(ioSource, "static void hex_io_"+operation+"_entry(void *raw)")
		entryUse := strings.Index(ioSource, "hex_blocking_call(hex_io_"+operation+"_entry, &job);")
		if entryDefinition < 0 || entryUse < 0 || entryDefinition >= entryUse {
			t.Fatalf("hex_io_%s_entry definition must precede its frontend use: definition=%d use=%d", operation, entryDefinition, entryUse)
		}
	}
}

// Completion snapshots every target disposition while it still owns the
// lifecycle mutex. Waking a joiner is the final target access because the
// joiner may reclaim the target immediately after publication.
func TestConcurrencyCompletionPublishesOnlyAfterDispositionSnapshot(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    return task.join()\nend\n")
	source := generateOne(t, program)["hexal/concurrency.c"]
	snapshot := strings.Index(source, "bool root = (task->flags & HEX_TASK_ROOT) != 0;")
	detached := strings.Index(source, "bool detached = task->terminal_claim == HEX_TASK_CLAIM_DETACH;")
	unlock := strings.Index(source[snapshot:], "mtx_unlock(&task->lifecycle_mutex);") + snapshot
	wake := strings.Index(source[unlock:], "hex_task_wake(joiner);") + unlock
	if snapshot < 0 || detached < snapshot || unlock < detached || wake < unlock {
		t.Fatalf("completion must snapshot disposition under the lifecycle mutex before waking the joiner:\n%s", source)
	}
	workerEnd := strings.Index(source[wake:], "\nstatic int hex_worker_thread")
	if workerEnd < 0 {
		t.Fatalf("could not isolate the completion dispatcher:\n%s", source)
	}
	afterWake := source[wake : wake+workerEnd]
	if strings.Contains(afterWake, "task->") {
		t.Fatalf("completion accesses the target after waking its joiner:\n%s", afterWake)
	}
}

// Join and detach claim the one terminal ownership slot before either can
// arrange reclamation; the generated runtime contains both claim checks.
func TestConcurrencyTerminalClaimProtectsJoinAndDetach(t *testing.T) {
	program := checkedGeneratorSource(t, "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> := try spawn square(6)\n    task.detach()\n    return 0\nend\n")
	files := generateOne(t, program)
	header := files["hexal/concurrency.h"]
	source := files["hexal/concurrency.c"]
	if strings.Count(header, "uint8_t terminal_claim;") != 1 {
		t.Fatalf("hex_task must own one terminal claim field:\n%s", header)
	}
	for _, fragment := range []string{
		"#define HEX_TASK_CLAIM_NONE 0",
		"#define HEX_TASK_CLAIM_JOIN 1",
		"#define HEX_TASK_CLAIM_DETACH 2",
		"if (task->terminal_claim != HEX_TASK_CLAIM_NONE)",
		"task->terminal_claim = HEX_TASK_CLAIM_JOIN;",
		"task->terminal_claim = HEX_TASK_CLAIM_DETACH;",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("concurrency runtime lacks terminal-claim fragment %q:\n%s", fragment, source)
		}
	}
}

// Resume and dispatcher commit must perform the specified phase transitions;
// an unexpected phase is a runtime defect, not an implicit ready publication.
func TestConcurrencyParkPhaseTransitionsFailClosed(t *testing.T) {
	program := checkedGeneratorSource(t, "fun worker(): Bool do\n    Task.yield()\n    return true\nend\nfun run(): Int32 | Error do\n    task: Task<Bool> := try spawn worker()\n    task.join()\n    return 0\nend\n")
	source := generateOne(t, program)["hexal/concurrency.c"]
	if strings.Contains(source, "atomic_store_explicit(&self->park_phase, HEX_PARK_RUNNING") {
		t.Fatalf("resume must transition ready to running with compare-exchange:\n%s", source)
	}
	for _, fragment := range []string{
		"atomic_compare_exchange_strong_explicit(&self->park_phase, &expected, HEX_PARK_RUNNING",
		"invalid Task park phase during resume",
		"invalid Task park phase during commit",
		"Task park phase changed during commit",
	} {
		if !strings.Contains(source, fragment) {
			t.Fatalf("concurrency runtime lacks fail-closed phase fragment %q:\n%s", fragment, source)
		}
	}
}
