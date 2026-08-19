package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
)

// A try statement hoists the union into a hex_try_N temporary, propagates
// the Error member, and discards the success value without a normalization
// temporary; a try expression rebinds a multi-success payload through a
// hex_try_result_N temporary.

func TestGenerateTryStatementLowering(t *testing.T) {
	program := checkedGeneratorSource(t, "fun fail(): Nil | Error do\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error do\n    try fail()\n    return 1\nend\n")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"const hex_union_7_t_Error9_nullptr_t hex_try_1 = hex_f_m3_app_fail();",
		"if (hex_try_1.tag == hex_union_7_t_Error9_nullptr_t_tag_member_",
		"return (hex_union_7_int32_t7_t_Error){ .tag = hex_union_7_int32_t7_t_Error_tag_member_1, .payload.member_1 = hex_try_1.payload.member_",
	} {
		if !strings.Contains(rootC, want) {
			t.Fatalf("generated C = %q, want %q", rootC, want)
		}
	}
	if strings.Contains(rootC, "hex_try_result_") {
		t.Fatalf("try statement must not normalize a discarded success value")
	}
}

func TestGenerateTryExpressionNormalizesSuccess(t *testing.T) {
	program := checkedGeneratorSource(t, "fun read_count(): Int32 | Error do\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error do\n    count: Int32 := try read_count()\n    return count\nend\n")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"const hex_union_7_int32_t7_t_Error hex_try_1 = hex_f_m3_app_read_count();",
		"if (hex_try_1.tag == hex_union_7_int32_t7_t_Error_tag_member_1) {",
		"hex_v_count = hex_try_1.payload.member_0;",
	} {
		if !strings.Contains(rootC, want) {
			t.Fatalf("generated C = %q, want %q", rootC, want)
		}
	}
	// A union with several success members needs a normalization temporary.
	multiple := checkedGeneratorSource(t, "fun read_number(): Int32 | Float32 | Error do\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error do\n    value: Int32 | Float32 := try read_number()\n    return 1\nend\n")
	files, err = GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": multiple}, Config{})
	multiC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(multiC, "hex_try_result_") || !strings.Contains(multiC, "switch (hex_try_1.tag) {") {
		t.Fatalf("multi-success try must normalize through a temporary; got %q", multiC)
	}
}

// A try inside a nested block emits exactly one prologue, at the block's own
// statement position: the operand call appears once and the prologue sits
// inside the block, so an untaken branch performs no call.
func TestGenerateTryInNestedBlockEmitsSinglePrologue(t *testing.T) {
	program := checkedGeneratorSource(t, "fun mk(): Int32 | Error do\n    return 1\nend\nfun run(): Int32 | Error do\n    if true then\n        v: Int32 := try mk()\n        print(v)\n    end\n    return 0\nend\n")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	rootC := files["modules/app.c"]
	if count := strings.Count(rootC, "hex_f_m3_app_mk()"); count != 1 {
		t.Fatalf("try operand must evaluate exactly once; got %d calls:\n%s", count, rootC)
	}
	// The prologue follows the if header, inside the block that contains
	// the try; the function-scope copy that ran unconditionally must not
	// exist.
	ifIdx := strings.Index(rootC, "if (true) {")
	if ifIdx < 0 {
		t.Fatalf("generated C lacks the if header:\n%s", rootC)
	}
	if !strings.Contains(rootC[ifIdx:], "const hex_union_7_int32_t7_t_Error hex_try_1 = hex_f_m3_app_mk();") {
		t.Fatalf("try prologue must appear inside the block that contains the try:\n%s", rootC)
	}
}

// The Atomic helpers emit the C23 <stdatomic.h> operations directly at
// sequential consistency; no delegating generic forwarder over the handle
// typedef exists.
func TestGenerateAtomicHelpersCallStandardOperationsDirectly(t *testing.T) {
	program := checkedGeneratorSource(t, "fun run(): Bool do\n    counter: Atomic<Int32> := Atomic<Int32>.new(0)\n    counter.store(1)\n    return counter.load() == 1\nend\n")
	files := generateOne(t, program)
	rootH := files["modules/app.h"]
	for _, want := range []string{
		"typedef _Atomic(int32_t) hex_atomic_Int32;",
		"atomic_store_explicit(atomic, value, memory_order_seq_cst);",
		"return atomic_load_explicit(atomic, memory_order_seq_cst);",
	} {
		if !strings.Contains(rootH, want) {
			t.Fatalf("generated header = %q, want %q", rootH, want)
		}
	}
	for _, generic := range []string{"hex_atomic_store(", "hex_atomic_load("} {
		if strings.Contains(rootH, generic) {
			t.Fatalf("generated header retains generic atomic forwarder %q: %q", generic, rootH)
		}
	}
}

// lock/unlock lower directly to the core, the scheduler reports failures
// through hex_runtime_trap with the complete literal, and its null constants
// spell nullptr. The scheduler runtime lives in the hexal/concurrency.c
// component; the module C file keeps only the direct core call sites.
func TestGenerateSchedulerTrapAndDirectLowering(t *testing.T) {
	program := checkedGeneratorSource(t, "fun worker(m: Mutex): Bool do\n    m.lock()\n    m.unlock()\n    Task.yield()\n    return true\nend\nfun run(): Int32 | Error do\n    h: Heap := Heap.new()\n    m: Mutex := try Mutex.new(h)\n    task: Task<Bool> := try spawn worker(m)\n    task.join()\n    return 0\nend\n")
	files := generateOne(t, program)
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	concurrencyC := files["hexal/concurrency.c"]
	for _, want := range []string{
		"hex_mutex_lock(hex_v_m)",
		"hex_mutex_unlock(hex_v_m)",
	} {
		if !strings.Contains(rootC, want) {
			t.Fatalf("generated C = %q, want %q", rootC, want)
		}
	}
	for _, want := range []string{
		"hex_runtime_trap(\"[Runtime Error] scheduler worker creation failed\\n\");",
		"hex_runtime_trap(\"[Runtime Error] cannot join the current task\\n\");",
		"hex_runtime_trap(\"[Runtime Error] recursive mutex lock\\n\");",
		"task->ready_next = nullptr;",
	} {
		if !strings.Contains(concurrencyC, want) {
			t.Fatalf("hexal/concurrency.c = %q, want %q", concurrencyC, want)
		}
	}
	if strings.Contains(rootC, "task->ready_next = nullptr;") || strings.Contains(rootC, "static void hex_scheduler_init(void) {") {
		t.Fatalf("module C retains scheduler runtime text:\n%s", rootC)
	}
	for _, gone := range []string{"hex_sched_fatal", "hex_mutex_lock_hex_mutex(", "fputs(\"[Runtime Error]", "NULL"} {
		if strings.Contains(rootC, gone) || strings.Contains(rootH, gone) || strings.Contains(concurrencyC, gone) {
			t.Fatalf("generated output retains %q: C := %q H=%q conc=%q", gone, rootC, rootH, concurrencyC)
		}
	}
	if !strings.Contains(rootH, "static inline void hex_mutex_free_hex_mutex(uintptr_t heap_identity, hex_mutex *mutex)") {
		t.Fatalf("generated header lacks the identity-adapting free: %q", rootH)
	}
}
