package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// These tests compile concurrency programs only; build-and-run coverage
// against a C23 toolchain lives behind the c23 build tag.

// A loop that spawns N workers emits exactly one spawn site inside the loop
// body: the hoisted function-scope copy would have created one extra task
// per loop, never joined or detached, so N iterations must produce exactly
// N sites' worth of tasks.
func TestSpawnLoopEmitsOneSiteInsideBody(t *testing.T) {
	source := "fun burn(value: Int64): Int64 do\n    return value\nend\nfun run(): Int64 | Error do\n    mut total: Int64 = 0\n    mut n: Int64 = 0\n    while n < 8 do\n        w: Task<Int64> = try spawn burn(n)\n        total = total + w.join()\n        n = n + 1\n    end\n    return total\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	rootC := rootC(t, result)
	if count := strings.Count(rootC, "hex_task_spawn("); count != 1 {
		t.Fatalf("loop of 8 spawns emits %d spawn sites, want exactly one per iteration:\n%s", count, rootC)
	}
	if !strings.Contains(rootC, "while ((hex_v_n < INT64_C(8))) {\n") {
		t.Fatalf("generated C lacks the loop header:\n%s", rootC)
	}
	if !strings.Contains(rootC[strings.Index(rootC, "while ((hex_v_n < INT64_C(8))) {"):], "hex_spawn_task_1 = hex_task_spawn(") {
		t.Fatalf("the one spawn site must live inside the loop body:\n%s", rootC)
	}
}

// A non-default TaskStackReserve must reach the generated runtime and appear
// in the emitted C: the POSIX usable-stack expression and the CreateFiberEx
// reserve argument both spell the configured bytes, while a zero Project
// keeps the historical "1u << 20" text byte-identical.
func TestTaskStackReserveReachesGeneratedRuntime(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    return task.join()\nend\n"
	configured := compiler.Compile(map[string]string{rootSourceKey: source}, rootSourceKey, compiler.Project{TaskStackReserve: 2 << 20})
	if configured.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", configured.Stderr)
	}
	concurrencyC := moduleFile(t, configured, "hexal/concurrency.c")
	if !strings.Contains(concurrencyC, "const size_t stack_size = 2097152u;") {
		t.Fatalf("generated runtime lacks the configured POSIX stack size:\n%s", concurrencyC)
	}
	if !strings.Contains(concurrencyC, "CreateFiberEx(0, 2097152u, FIBER_FLAG_FLOAT_SWITCH") {
		t.Fatalf("generated runtime lacks the configured fiber reserve:\n%s", concurrencyC)
	}
	zero := compileSource(source)
	if zero.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", zero.Stderr)
	}
	zeroConcurrencyC := moduleFile(t, zero, "hexal/concurrency.c")
	if !strings.Contains(zeroConcurrencyC, "const size_t stack_size = 1u << 20;") {
		t.Fatalf("zero Project must keep the default stack expression:\n%s", zeroConcurrencyC)
	}
	if !strings.Contains(zeroConcurrencyC, "CreateFiberEx(0, 0, FIBER_FLAG_FLOAT_SWITCH") {
		t.Fatalf("zero Project must keep the default fiber arguments:\n%s", zeroConcurrencyC)
	}
}

func TestSpawnAndJoinCompile(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    return task.join()\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_task_spawn(") {
		t.Fatalf("generated C lacks the spawn call:\n%s", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_task_join_Int32(") {
		t.Fatalf("generated C lacks the typed join call:\n%s", rootC(t, result))
	}
	// The scheduler runtime owns the concurrency component; the root module
	// C keeps the init/complete call sites and no runtime definitions.
	concurrencyC := moduleFile(t, result, "hexal/concurrency.c")
	if !strings.Contains(concurrencyC, "static void hex_scheduler_init(void) {") {
		t.Fatalf("hexal/concurrency.c lacks the scheduler runtime:\n%s", concurrencyC)
	}
	if strings.Contains(rootC(t, result), "static void hex_scheduler_init(void) {") {
		t.Fatalf("root module C retains the scheduler runtime:\n%s", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_scheduler_init();") {
		t.Fatalf("generated main does not initialize the scheduler:\n%s", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_task_complete(hex_root_task);") {
		t.Fatalf("generated main does not complete the root task:\n%s", rootC(t, result))
	}
}

// Spawning a no-result function is a Type Error because no Task<R> can be
// formed; effect-only tasks return an explicit payload.
func TestSpawnRequiresFunctionWithResult(t *testing.T) {
	source := "fun worker() do\n    Task.yield()\nend\nfun run(): Int32 | Error do\n    task: Task<Bool> = try spawn worker()\n    task.join()\n    return 1\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "spawn requires a function with a result") {
		t.Fatalf("want spawn-without-result diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestChannelPipelineCompiles(t *testing.T) {
	source := "fun produce(ch: Channel<Int32>): Bool do\n    ch.send(1)\n    ch.send(2)\n    ch.close()\n    return true\nend\nfun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n    defer ch.free(h)\n    producer: Task<Bool> = try spawn produce(ch)\n    producer.join()\n    mut total: Int32 = 0\n    while true do\n        step: Int32 | EoS = ch.receive()\n        if step is EoS then\n            break\n        end\n        total = total + step\n        Task.yield()\n    end\n    return total\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{
		"hex_chan_new_Int32(",
		"hex_chan_send_Int32(",
		"hex_chan_recv_Int32(",
		"hex_chan_close(",
		"hex_chan_free_Int32(",
	} {
		if !strings.Contains(rootC(t, result), fragment) && !strings.Contains(rootH(t, result), fragment) {
			t.Fatalf("generated output lacks %s:\n%s", fragment, rootC(t, result))
		}
	}
	// The slot region is sized with a checked multiply; the constructor's
	// manual overflow guard must not appear. The channel core owns the
	// concurrency component.
	concurrencyC := moduleFile(t, result, "hexal/concurrency.c")
	if !strings.Contains(concurrencyC, "ckd_mul(&slots_bytes, element_size, capacity)") {
		t.Fatalf("channel core does not use checked slot sizing:\n%s", concurrencyC)
	}
	if strings.Contains(concurrencyC, "SIZE_MAX / capacity") {
		t.Fatalf("channel core retains the manual overflow guard:\n%s", concurrencyC)
	}
	// Close, length, capacity, and is_closed lower directly to the core; no
	// per-element forwarding wrappers remain.
	if strings.Contains(concurrencyC, "hex_chan_close_Int32(") || strings.Contains(rootC(t, result), "hex_chan_close_Int32(") || strings.Contains(rootH(t, result), "hex_chan_close_Int32(") {
		t.Fatalf("channel close retains its delegating wrapper:\n%s", rootH(t, result))
	}
	if strings.Contains(concurrencyC, "hex_chan_length_Int32(") || strings.Contains(rootC(t, result), "hex_chan_length_Int32(") || strings.Contains(rootH(t, result), "hex_chan_length_Int32(") {
		t.Fatalf("channel length retains its delegating wrapper:\n%s", rootH(t, result))
	}
}

func TestMutexCompiles(t *testing.T) {
	source := "fun worker(m: Mutex): Bool do\n    m.lock()\n    m.unlock()\n    return true\nend\nfun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    defer m.free(h)\n    mutex_task: Task<Bool> = try spawn worker(m)\n    mutex_task.join()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	// Lock and unlock call the core directly; only the constructor and free
	// adapters remain in the module header.
	if !strings.Contains(rootC(t, result), "hex_mutex_lock(hex_v_m)") {
		t.Fatalf("generated code lacks the direct mutex lock call:\n%s", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_mutex_unlock(hex_v_m)") {
		t.Fatalf("generated code lacks the direct mutex unlock call:\n%s", rootC(t, result))
	}
	if !strings.Contains(rootH(t, result), "hex_mutex_new_mutex(") || !strings.Contains(rootH(t, result), "hex_mutex_free_hex_mutex(") {
		t.Fatalf("generated header lacks the mutex adapters:\n%s", rootH(t, result))
	}
	if strings.Contains(rootH(t, result), "hex_mutex_lock_hex_mutex(") || strings.Contains(rootH(t, result), "hex_mutex_unlock_hex_mutex(") {
		t.Fatalf("generated header retains the mutex lock/unlock wrappers:\n%s", rootH(t, result))
	}
}

func TestAtomicOperationsCompile(t *testing.T) {
	source := "fun run(): Bool do\n    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n    old: Int32 = counter.fetch_add(1)\n    counter.fetch_sub(1)\n    counter.store(5)\n    loaded: Int32 = counter.load()\n    swapped: Int32 = counter.exchange(6)\n    expected: Bool = counter.compare_exchange(6, 7)\n    mut flag: Atomic<Bool> = Atomic<Bool>.new(true)\n    ready: Bool = flag.load()\n    return ready and expected\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	for _, fragment := range []string{
		"hex_atomic_Int32_new(",
		"hex_atomic_Int32_fetch_add(",
		"hex_atomic_Int32_fetch_sub(",
		"hex_atomic_Int32_store(",
		"hex_atomic_Int32_load(",
		"hex_atomic_Int32_exchange(",
		"hex_atomic_Int32_compare_exchange(",
		"hex_atomic_Bool_load(",
	} {
		if !strings.Contains(rootH(t, result), fragment) && !strings.Contains(rootC(t, result), fragment) {
			t.Fatalf("generated output lacks %s", fragment)
		}
	}
	// Load/store/exchange/fetch bodies call the C23 <stdatomic.h> functions
	// directly at sequential consistency; no delegating generic forwarder
	// exists.
	generated := rootH(t, result)
	for _, fragment := range []string{
		"return atomic_load_explicit(atomic, memory_order_seq_cst);",
		"atomic_store_explicit(atomic, value, memory_order_seq_cst);",
		"return atomic_exchange_explicit(atomic, value, memory_order_seq_cst);",
		"return atomic_fetch_add_explicit(atomic, value, memory_order_seq_cst);",
		"return atomic_fetch_sub_explicit(atomic, value, memory_order_seq_cst);",
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("generated atomic helpers lack the direct standard call %q:\n%s", fragment, generated)
		}
	}
	for _, generic := range []string{
		"hex_atomic_store(",
		"hex_atomic_load(",
		"hex_atomic_exchange(",
		"hex_atomic_fetch_add(",
		"hex_atomic_fetch_sub(",
	} {
		if strings.Contains(generated, generic) {
			t.Fatalf("generated output retains the generic atomic forwarder %q:\n%s", generic, generated)
		}
	}
}

// An immutable Atomic<T> binding is mutable-through, like a RuneCursor: it
// carries no top-level const, because its accessors take a non-const receiver
// and a const-qualified load would be a qualifier-discarding cast.
func TestAtomicBindingCarriesNoConstThatAccessorsReject(t *testing.T) {
	source := "fun run(): Int32 do\n" +
		"    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n" +
		"    counter.fetch_add(1)\n" +
		"    counter.fetch_sub(1)\n" +
		"    counter.store(5)\n" +
		"    loaded: Int32 = counter.load()\n" +
		"    swapped: Int32 = counter.exchange(6)\n" +
		"    counter.compare_exchange(6, 7)\n" +
		"    return loaded + swapped\n" +
		"end\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	generated := rootC(t, result)
	if strings.Contains(generated, "const hex_atomic_Int32 ") {
		t.Fatalf("generated C const-qualifies the Atomic binding:\n%s", generated)
	}
	// Every accessor call still passes the binding directly; no const cast
	// workaround appears anywhere.
	if strings.Contains(generated, "((hex_atomic_Int32 *)&") || strings.Contains(generated, "(const hex_atomic_Int32") {
		t.Fatalf("generated C contains a qualifier workaround cast:\n%s", generated)
	}
	for _, call := range []string{
		"hex_atomic_Int32_fetch_add(&(hex_v_counter), 1);",
		"hex_atomic_Int32_fetch_sub(&(hex_v_counter), 1);",
		"hex_atomic_Int32_store(&(hex_v_counter), 5);",
		"hex_atomic_Int32_load(&(hex_v_counter))",
		"hex_atomic_Int32_exchange(&(hex_v_counter), 6)",
		"hex_atomic_Int32_compare_exchange(&(hex_v_counter), 6, 7)",
	} {
		if !strings.Contains(generated, call) {
			t.Fatalf("generated C lacks direct accessor call %q:\n%s", call, generated)
		}
	}
}

// Close, length, capacity, and is_closed call the non-generic hex_chan_*
// core directly; no per-element forwarding wrapper is emitted for them.
func TestChannelDirectCoreCalls(t *testing.T) {
	source := "fun run(): Size | Error do\n" +
		"    h: Heap = Heap.new()\n" +
		"    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n" +
		"    ch.close()\n" +
		"    mut length: Size = ch.length()\n" +
		"    capacity: Size = ch.capacity()\n" +
		"    closed: Bool = ch.is_closed()\n" +
		"    length = length + capacity\n" +
		"    if closed then\n" +
		"        length = length + 1\n" +
		"    end\n" +
		"    return length\n" +
		"end\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	rootC := rootC(t, result)
	rootH := rootH(t, result)
	for _, direct := range []string{
		"hex_chan_close(hex_v_ch)",
		"hex_chan_length(hex_v_ch)",
		"hex_chan_capacity(hex_v_ch)",
		"hex_chan_is_closed(hex_v_ch)",
	} {
		if !strings.Contains(rootC, direct) {
			t.Fatalf("generated code lacks the direct core call %q:\n%s", direct, rootC)
		}
	}
	for _, wrapper := range []string{
		"hex_chan_close_Int32(",
		"hex_chan_length_Int32(",
		"hex_chan_capacity_Int32(",
		"hex_chan_is_closed_Int32(",
	} {
		if strings.Contains(rootC, wrapper) || strings.Contains(rootH, wrapper) {
			t.Fatalf("generated output retains the channel forwarding wrapper %q:\n%s", wrapper, rootH)
		}
	}
	// The typed storage/union adapters survive for new and free even when a
	// program never sends or receives; send and receive adapters are
	// demand-emitted with their unions.
	for _, adapter := range []string{
		"hex_chan_new_Int32(",
		"hex_chan_free_Int32(",
	} {
		if !strings.Contains(rootH, adapter) {
			t.Fatalf("generated header lacks the retained channel adapter %q:\n%s", adapter, rootH)
		}
	}
}

// Both the direct and the deferred Mutex free evaluate the Heap argument
// once and pass its identity token to the same retained adapter; neither
// path drops the argument.
func TestMutexFreePassesHeapIdentity(t *testing.T) {
	source := "fun run(): Int32 | Error do\n" +
		"    h: Heap = Heap.new()\n" +
		"    m: Mutex = try Mutex.new(h)\n" +
		"    defer m.free(h)\n" +
		"    m.free(h)\n" +
		"    return 0\n" +
		"end\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	rootC := rootC(t, result)
	// The direct call passes the Heap's identity token, never the whole Heap
	// object; the identity comes from the binding read of h.
	if !strings.Contains(rootC, "hex_mutex_free_hex_mutex((hex_v_h).identity, hex_v_m)") {
		t.Fatalf("direct mutex free does not pass the heap identity:\n%s", rootC)
	}
	if strings.Contains(rootC, "hex_mutex_free_hex_mutex(hex_v_m)") {
		t.Fatalf("direct mutex free drops the checked Heap argument:\n%s", rootC)
	}
	if !strings.Contains(rootC, "hex_mutex_free_hex_mutex(hex_defer_capture_2.identity, hex_defer_capture_1)") {
		t.Fatalf("deferred mutex free does not pass the captured heap identity:\n%s", rootC)
	}
	rootH := rootH(t, result)
	if !strings.Contains(rootH, "static inline void hex_mutex_free_hex_mutex(uintptr_t heap_identity, hex_mutex *mutex)") {
		t.Fatalf("mutex free adapter lacks the heap identity parameter:\n%s", rootH)
	}
	if !strings.Contains(rootH, "hex_mutex_free(mutex);") {
		t.Fatalf("mutex free adapter does not reach the core free:\n%s", rootH)
	}
}

// The scheduler and Channel/Mutex cores report every failure through the one
// hex_runtime_trap with the complete "[Runtime Error] ...\n" literal, never
// through hex_sched_fatal or a per-site fputs/abort pair, and the emitted
// scheduler text uses nullptr.
func TestSchedulerTrapsUseRuntimeTrap(t *testing.T) {
	source := "fun worker(ch: Channel<Int32>, m: Mutex): Bool do\n" +
		"    m.lock()\n" +
		"    ch.send(1)\n" +
		"    m.unlock()\n" +
		"    Task.yield()\n" +
		"    return true\n" +
		"end\n" +
		"fun run(): Int32 | Error do\n" +
		"    h: Heap = Heap.new()\n" +
		"    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n" +
		"    m: Mutex = try Mutex.new(h)\n" +
		"    defer m.free(h)\n" +
		"    defer ch.free(h)\n" +
		"    task: Task<Bool> = try spawn worker(ch, m)\n" +
		"    task.join()\n" +
		"    return 0\n" +
		"end\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	concurrencyC := moduleFile(t, result, "hexal/concurrency.c")
	for _, trap := range []string{
		"hex_runtime_trap(\"[Runtime Error] scheduler mutex initialization failed\\n\");",
		"hex_runtime_trap(\"[Runtime Error] scheduler condition variable initialization failed\\n\");",
		"hex_runtime_trap(\"[Runtime Error] scheduler allocation failed\\n\");",
		"hex_runtime_trap(\"[Runtime Error] scheduler fiber initialization failed\\n\");",
		"hex_runtime_trap(\"[Runtime Error] scheduler worker-zero context creation failed\\n\");",
		"hex_runtime_trap(\"[Runtime Error] scheduler worker creation failed\\n\");",
		"hex_runtime_trap(\"[Runtime Error] cannot join the current task\\n\");",
		"hex_runtime_trap(\"[Runtime Error] recursive mutex lock\\n\");",
		"hex_runtime_trap(\"[Runtime Error] mutex unlock by a non-owner\\n\");",
		"hex_runtime_trap(\"[Runtime Error] mutex free while locked or awaited\\n\");",
		"hex_runtime_trap(\"[Runtime Error] channel free while tasks are blocked on it\\n\");",
		"hex_runtime_trap(\"[Runtime Error] channel free requires a closed, empty channel\\n\");",
	} {
		if !strings.Contains(concurrencyC, trap) {
			t.Fatalf("hexal/concurrency.c lacks the trap %q:\n%s", trap, concurrencyC)
		}
	}
	rootC := rootC(t, result)
	for _, gone := range []string{"hex_sched_fatal", "fputs(\"[Runtime Error]"} {
		if strings.Contains(concurrencyC, gone) || strings.Contains(rootC, gone) {
			t.Fatalf("generated code retains %q:\n%s", gone, concurrencyC)
		}
	}
	// The scheduler text spells its null pointer constants nullptr.
	if !strings.Contains(concurrencyC, "task->ready_next = nullptr;") || !strings.Contains(concurrencyC, "if (hex_root_task == nullptr) {") {
		t.Fatalf("scheduler text does not use the nullptr spelling:\n%s", concurrencyC)
	}
	if strings.Contains(concurrencyC, "NULL") || strings.Contains(rootC, "NULL") || strings.Contains(rootH(t, result), "NULL") {
		t.Fatalf("generated concurrency output retains NULL:\n%s", concurrencyC)
	}
}

// An atomic-only program emits the Atomic typedefs and helpers with the C23
// nullptr spelling and no raw fputs.
func TestAtomicOnlyOutputUsesNullptr(t *testing.T) {
	source := "fun run(): Bool do\n" +
		"    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n" +
		"    counter.store(1)\n" +
		"    return counter.load() == 1\n" +
		"end\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	generated := rootH(t, result)
	if strings.Contains(generated, "NULL") || strings.Contains(generated, "fputs(") {
		t.Fatalf("atomic-only header retains NULL or fputs:\n%s", generated)
	}
}

func TestTaskMethodCallsRequireCorrectArity(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    return task.join(1)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "join expects no arguments") {
		t.Fatalf("want join arity diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestTaskHasNoUnknownMethods(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    task.cancel()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Task has no method cancel") {
		t.Fatalf("want unknown-method diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectsNonDirectCall(t *testing.T) {
	for _, source := range []string{
		"fun run(): Int32 | Error do\n    task: Task<Int32> = try spawn 5\n    return 0\nend\n",
		"type Point = { x: Int32, }\nfun scale(point: Point, factor: Int32): Int32 do\n    return point.x * factor\nend\nfun run(): Int32 | Error do\n    point: Point = Point { x = 2 }\n    task: Task<Int32> = try spawn point.scale(3)\n    return 0\nend\n",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "spawn requires a direct call") {
			t.Fatalf("want direct-call diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

func TestSpawnRejectsNonCopyableArguments(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n    task: Task<Int32> = try spawn square(0)\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("atomic not passed to spawn should still compile: %v", result.Stderr)
	}
	bad := "fun take_atomic(counter: Atomic<Int32>): Int32 do\n    return 0\nend\nfun run(): Int32 | Error do\n    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n    task: Task<Int32> = try spawn take_atomic(counter)\n    return 0\nend\n"
	result = compileSource(bad)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Atomic") {
		t.Fatalf("want atomic-argument diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectsAtomicResult(t *testing.T) {
	source := "fun make_atomic(): Atomic<Int32> do\n    return Atomic<Int32>.new(0)\nend\nfun run(): Int32 | Error do\n    task: Task<Atomic<Int32>> = try spawn make_atomic()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "shallow-copyable") {
		t.Fatalf("want atomic-result diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestChannelRejectsInvalidElements(t *testing.T) {
	for _, source := range []string{
		"fun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Int32 | EoS> = try Channel<Int32 | EoS>.new(h, 4)\n    return 0\nend\n",
		"fun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Atomic<Int32>> = try Channel<Atomic<Int32>>.new(h, 4)\n    return 0\nend\n",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want channel element diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

func TestChannelZeroCapacityRejectedAtCompileTime(t *testing.T) {
	source := "fun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 0)\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "capacity must be positive") {
		t.Fatalf("want capacity diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestAtomicRejectsUnsupportedElements(t *testing.T) {
	source := "fun run(): Atomic<Float64> do\n    return Atomic<Float64>.new(1.5)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Atomic element type is not supported") {
		t.Fatalf("want atomic element diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestAtomicFetchAddUnavailableForBool(t *testing.T) {
	source := "fun run(): Bool do\n    flag: Atomic<Bool> = Atomic<Bool>.new(true)\n    old: Bool = flag.fetch_add(true)\n    return old\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "unavailable for Bool") {
		t.Fatalf("want Bool fetch diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectedInsideDefer(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    defer spawn square(1)\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "not permitted inside defer") {
		t.Fatalf("want defer diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestWhileTrueWithoutYieldRejectedWhenSchedulerLinked(t *testing.T) {
	source := "fun worker(): Bool do\n    while true do\n    end\n    return true\nend\nfun run(): Int32 | Error do\n    task: Task<Bool> = try spawn worker()\n    task.join()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "while true loop must execute Task.yield()") {
		t.Fatalf("want starvation diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestWhileTrueWithYieldOnEveryRepeatingPathAccepted(t *testing.T) {
	source := "fun worker(): Bool do\n    while true do\n        if false then\n            break\n        end\n        Task.yield()\n    end\n    return true\nend\nfun run(): Int32 | Error do\n    task: Task<Bool> = try spawn worker()\n    task.join()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestWhileTrueWithoutSchedulerIsUnchecked(t *testing.T) {
	source := "fun spin(): Int32 do\n    while true do\n    end\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("a program without the scheduler must not apply the starvation rule: %v", result.Stderr)
	}
}

func TestTaskTypesAreProtected(t *testing.T) {
	source := "type Task = { value: Int32, }\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot be redeclared") {
		t.Fatalf("want protected-name diagnostic; got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestAtomicNonCopyability(t *testing.T) {
	rejected := []string{
		"counter: Atomic<Int32> = Atomic<Int32>.new(0)\ncopy: Atomic<Int32> = counter\n",
		"counter: Atomic<Int32> = Atomic<Int32>.new(0)\nmut other: Atomic<Int32> = Atomic<Int32>.new(1)\nother = counter\n",
		"counter: Atomic<Int32> = Atomic<Int32>.new(0)\npointer: MutPtr<Atomic<Int32>> = ref counter\n",
		"items: Array<Atomic<Int32>, 1> = [Atomic<Int32>.new(0)]\n",
		"type Bad = | V as { a: Atomic<Int32> }\n",
		"counter: Atomic<Int32> = Atomic<Int32>.new(0)\nvalue: Atomic<Int32> | Nil = counter\n",
	}
	for _, source := range rejected {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure {
			t.Fatalf("want reject; got accept:\n%s", source)
		}
	}
	accepted := []string{
		"counter: Atomic<Int32> = Atomic<Int32>.new(0)\n",
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\n",
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\npointer: Ptr<Shared> = ref shared\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}

// A direct Atomic element is an invalid Ptr/MutPtr pointee in every
// spelling, while pointers to an enclosing object stay valid and Atomic
// operations work through them.
func TestAtomicDirectPointeeRules(t *testing.T) {
	rejected := []string{
		"type AtomicPtr = Ptr<Atomic<Int32>>\n",
		"type AtomicPtr = MutPtr<Atomic<Int32>>\n",
		"type AP = Atomic<Int32>\nx: Int32 = 1\npointer: Ptr<AP> = ref x\n",
		"type Alias<T> = Ptr<T>\nx: Int32 = 1\np: Alias<Atomic<Int32>> = ref x\n",
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\np: MutPtr<Atomic<Int32>> = ref shared.count\n",
		"h: Heap = Heap.new()\np: MutPtr<Atomic<Int32>> = h.allocate<Atomic<Int32>>(Atomic<Int32>.new(0))\n",
		"type Shared = { count: Atomic<Int32> }\nh: Heap = Heap.new()\np: MutPtr<Shared> = h.allocate<Shared>(Shared { count = Atomic<Int32>.new(0) })\n",
	}
	for _, source := range rejected {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure {
			t.Fatalf("want reject; got accept:\n%s", source)
		}
	}
	accepted := []string{
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\npointer: Ptr<Shared> = ref shared\npointer.count.store(1)\n",
		"type Shared = { count: Atomic<Int32> }\nmut shared: Shared = Shared { count = Atomic<Int32>.new(0) }\npointer: MutPtr<Shared> = ref shared\npointer.count.store(1)\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}

func TestChannelAndTaskRejectFunElement(t *testing.T) {
	rejected := []string{
		"fun identity(x: Int32): Int32 do\n    return x\nend\nfun f(h: Heap): Nil | Error do\n    ch: Channel<Fun<(Int32) : Int32>> = try Channel<Fun<(Int32) : Int32>>.new(h, 2)\n    return nil\nend\n",
		"fun identity(x: Int32): Int32 do\n    return x\nend\nfun f(h: Heap): Nil | Error do\n    t: Task<Fun<(Int32) : Int32>> = try spawn identity(1)\n    return nil\nend\n",
	}
	for _, source := range rejected {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure {
			t.Fatalf("want Fun excluded from Channel/Task; got accept:\n%s", source)
		}
	}
}

// Naming a handle type without performing a handle operation must still link
// the concurrency runtime: declaration-only reachability selects the
// components exactly like naming a List or Array does, for a Channel<T>
// parameter and return, a Task<R> parameter, a Mutex parameter, and an
// Atomic<T> object member.
func TestDeclarationOnlyHandleReachabilityLinksConcurrency(t *testing.T) {
	shapes := []struct {
		name      string
		shape     string
		operation string
		handle    string
		withC     bool
	}{
		{"channel parameter", "fun consume(c: Channel<Int32>): Int32 do\n    return 1\nend\n", "fun consume(c: Channel<Int32>): Int32 do\n    c.close()\n    return 1\nend\n", "hex_channel_Int32", true},
		{"task parameter", "fun consume(t: Task<Int32>): Int32 do\n    return 1\nend\n", "fun consume(t: Task<Int32>): Int32 do\n    t.join()\n    return 1\nend\n", "hex_task_Int32", true},
		{"channel return", "fun source(h: Heap): Channel<Int32> | Error do\n    return Error.new(\"x\", \"y\")\nend\n", "fun source(h: Heap): Channel<Int32> | Error do\n    return Channel<Int32>.new(h, 2)\nend\n", "hex_channel_Int32", true},
		{"mutex parameter", "fun protect(m: Mutex) do\nend\n", "fun protect(m: Mutex) do\n    m.lock()\n    m.unlock()\nend\n", "hex_mutex", true},
		{"atomic object member", "type Counter = { value: Atomic<Int32>, }\n", "type Counter = { value: Atomic<Int32>, }\nfun run() do\n    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n    counter.store(1)\nend\n", "hex_atomic_Int32", false},
	}
	for _, shape := range shapes {
		t.Run(shape.name, func(t *testing.T) {
			result := compileSource(shape.shape)
			if result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("declaration-only handle rejected (%v):\n%s", result.Stderr, shape.shape)
			}
			if _, ok := result.Files["hexal/concurrency.h"]; !ok {
				t.Fatalf("no concurrency component emitted for a declared handle:\n%s", shape.shape)
			}
			_, hasC := result.Files["hexal/concurrency.c"]
			if hasC != shape.withC {
				t.Fatalf("concurrency.c presence = %v, want %v", hasC, shape.withC)
			}
			// The declared handle's C type is spelled by the component.
			handle := moduleFile(t, result, "hexal/concurrency.h")
			if !strings.Contains(handle, shape.handle) {
				t.Fatalf("hexal/concurrency.h lacks the declared handle %q:\n%s", shape.handle, handle)
			}
			// The artifact set must match the same program performing a
			// handle operation: declaration-only reachability is not a
			// different emission mode.
			withOperation := compileSource(shape.operation)
			if withOperation.ExitCode != compiler.ExitSuccess {
				t.Fatalf("operation companion rejected (%v):\n%s", withOperation.Stderr, shape.operation)
			}
			shapeKeys := make(map[string]bool, len(result.Files))
			for key := range result.Files {
				shapeKeys[key] = true
			}
			operationKeys := make(map[string]bool, len(withOperation.Files))
			for key := range withOperation.Files {
				operationKeys[key] = true
			}
			if len(shapeKeys) != len(operationKeys) {
				t.Fatalf("shape files %v differ from operation files %v", shapeKeys, operationKeys)
			}
			for key := range shapeKeys {
				if !operationKeys[key] {
					t.Fatalf("shape files %v differ from operation files %v", shapeKeys, operationKeys)
				}
			}
		})
	}
}

// No-value commands are valid as call statements and as direct
// defer/errdefer cleanup actions.
func TestNoValueCommandsValidAsStatementsAndCleanup(t *testing.T) {
	source := "fun worker(): Bool do\n    Task.yield()\n    return true\nend\n" +
		"fun run(): Int32 | Error do\n" +
		"    h: Heap = Heap.new()\n" +
		"    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n" +
		"    defer ch.free(h)\n" +
		"    m: Mutex = try Mutex.new(h)\n" +
		"    defer m.free(h)\n" +
		"    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n" +
		"    task: Task<Bool> = try spawn worker()\n" +
		"    ch.close()\n" +
		"    m.lock()\n" +
		"    m.unlock()\n" +
		"    counter.store(1)\n" +
		"    task.detach()\n" +
		"    return 0\n" +
		"end\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("no-value commands as statements/cleanup = %v", result.Stderr)
	}
}

// No-value commands are rejected in value positions with the "<name>
// produces no value" diagnostic.
func TestNoValueCommandsRejectedInValuePositions(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun f(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n    bad: Int32 = ch.close()\n    return 0\nend\n", "close produces no value"},
		{"fun f(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n    bad: Int32 = ch.free(h)\n    return 0\nend\n", "free produces no value"},
		{"fun f(): Int32 | Error do\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    bad: Int32 = m.lock()\n    return 0\nend\n", "lock produces no value"},
		{"fun f(): Int32 | Error do\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    bad: Int32 = m.unlock()\n    return 0\nend\n", "unlock produces no value"},
		{"fun f(): Int32 | Error do\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    bad: Int32 = m.free(h)\n    return 0\nend\n", "free produces no value"},
		{"counter: Atomic<Int32> = Atomic<Int32>.new(0) bad: Int32 = counter.store(1)", "store produces no value"},
		{"fun worker(): Bool do\n    Task.yield()\n    return true\nend\nfun run(): Int32 | Error do\n    task: Task<Bool> = try spawn worker()\n    bad: Int32 = task.detach()\n    return 0\nend\n", "detach produces no value"},
		{"fun worker(): Bool do\n    Task.yield()\n    return true\nend\nfun f(): Int32 do\n    bad: Int32 = Task.yield()\n    return 0\nend\n", "yield produces no value"},
		{"fun bad(ch: Channel<Int32>): Int32 do\n    return ch.close()\nend\n", "close produces no value"},
		{"fun f(ch: Channel<Int32>): Int32 do\n    if ch.close() then noop: Int32 = 0 end\n    return 0\nend\n", "close produces no value"},
		{"fun f(ch: Channel<Int32>) do\n    print(ch.close())\nend\n", "close produces no value"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}
