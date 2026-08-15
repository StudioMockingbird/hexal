package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0037 concurrency integration tests: spawn, Task join/detach/yield,
// Channel, Mutex, and Atomic programs compile end to end, and invalid uses
// fail with the specified diagnostics. The gcc build-and-run coverage lives
// behind the c23 build tag (c23_concurrency_smoke_test.go).

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
	if !strings.Contains(rootC(t, result), "hex_scheduler_init") {
		t.Fatalf("generated root C lacks the scheduler runtime:\n%s", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_scheduler_init();") {
		t.Fatalf("generated main does not initialize the scheduler:\n%s", rootC(t, result))
	}
	if !strings.Contains(rootC(t, result), "hex_task_complete(hex_root_task);") {
		t.Fatalf("generated main does not complete the root task:\n%s", rootC(t, result))
	}
}

// RFC 0049 item 8.6: spawning a no-result function is a Type Error because
// no Task<R> can be formed; effect-only tasks return an explicit payload.
func TestSpawnRequiresFunctionWithResult(t *testing.T) {
	source := "fun worker() do\n    Task.yield()\nend\nfun run(): Int32 | Error do\n    task: Task<Bool> = try spawn worker()\n    task.join()\n    return 1\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "spawn requires a function with a result") {
		t.Fatalf("want spawn-without-result diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
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
		"hex_chan_close_Int32(",
		"hex_chan_free_Int32(",
		"hex_chan_length",
	} {
		if !strings.Contains(rootC(t, result), fragment) && !strings.Contains(rootH(t, result), fragment) {
			t.Fatalf("generated output lacks %s:\n%s", fragment, rootC(t, result))
		}
	}
}

func TestMutexCompiles(t *testing.T) {
	source := "fun worker(m: Mutex): Bool do\n    m.lock()\n    m.unlock()\n    return true\nend\nfun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    defer m.free(h)\n    mutex_task: Task<Bool> = try spawn worker(m)\n    mutex_task.join()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(rootH(t, result), "hex_mutex_lock_hex_mutex(") {
		t.Fatalf("generated header lacks the mutex helpers:\n%s", rootH(t, result))
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
}

func TestTaskMethodCallsRequireCorrectArity(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    return task.join(1)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "join expects no arguments") {
		t.Fatalf("want join arity diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestTaskHasNoUnknownMethods(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    task: Task<Int32> = try spawn square(6)\n    task.cancel()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Task has no method cancel") {
		t.Fatalf("want unknown-method diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectsNonDirectCall(t *testing.T) {
	for _, source := range []string{
		"fun run(): Int32 | Error do\n    task: Task<Int32> = try spawn 5\n    return 0\nend\n",
		"type Point = { x: Int32, }\nfun scale(point: Point, factor: Int32): Int32 do\n    return point.x * factor\nend\nfun run(): Int32 | Error do\n    point: Point = Point { x = 2 }\n    task: Task<Int32> = try spawn point.scale(3)\n    return 0\nend\n",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "spawn requires a direct call") {
			t.Fatalf("want direct-call diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
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
		t.Fatalf("want atomic-argument diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectsAtomicResult(t *testing.T) {
	source := "fun make_atomic(): Atomic<Int32> do\n    return Atomic<Int32>.new(0)\nend\nfun run(): Int32 | Error do\n    task: Task<Atomic<Int32>> = try spawn make_atomic()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "shallow-copyable") {
		t.Fatalf("want atomic-result diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestChannelRejectsInvalidElements(t *testing.T) {
	for _, source := range []string{
		"fun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Int32 | EoS> = try Channel<Int32 | EoS>.new(h, 4)\n    return 0\nend\n",
		"fun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Atomic<Int32>> = try Channel<Atomic<Int32>>.new(h, 4)\n    return 0\nend\n",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want channel element diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

func TestChannelZeroCapacityRejectedAtCompileTime(t *testing.T) {
	source := "fun run(): Int32 | Error do\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 0)\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "capacity must be positive") {
		t.Fatalf("want capacity diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestAtomicRejectsUnsupportedElements(t *testing.T) {
	source := "fun run(): Atomic<Float64> do\n    return Atomic<Float64>.new(1.5)\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Atomic element type is not supported") {
		t.Fatalf("want atomic element diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestAtomicFetchAddUnavailableForBool(t *testing.T) {
	source := "fun run(): Bool do\n    flag: Atomic<Bool> = Atomic<Bool>.new(true)\n    old: Bool = flag.fetch_add(true)\n    return old\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "unavailable for Bool") {
		t.Fatalf("want Bool fetch diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectedInsideDefer(t *testing.T) {
	source := "fun square(value: Int32): Int32 do\n    return value * value\nend\nfun run(): Int32 | Error do\n    defer spawn square(1)\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "not permitted inside defer") {
		t.Fatalf("want defer diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestWhileTrueWithoutYieldRejectedWhenSchedulerLinked(t *testing.T) {
	source := "fun worker(): Bool do\n    while true do\n    end\n    return true\nend\nfun run(): Int32 | Error do\n    task: Task<Bool> = try spawn worker()\n    task.join()\n    return 0\nend\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "while true loop must execute Task.yield()") {
		t.Fatalf("want starvation diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
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
		t.Fatalf("want protected-name diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
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
			t.Fatalf("want reject, got accept:\n%s", source)
		}
	}
	accepted := []string{
		"counter: Atomic<Int32> = Atomic<Int32>.new(0)\n",
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\n",
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\npointer: Ptr<Shared> = ref shared\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept, got %v:\n%s", result.Stderr, source)
		}
	}
}

// RFC 0049 item 8.5: a direct Atomic element is an invalid Ptr/MutPtr
// pointee in every spelling, while pointers to an enclosing object stay
// valid and Atomic operations work through them.
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
			t.Fatalf("want reject, got accept:\n%s", source)
		}
	}
	accepted := []string{
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\npointer: Ptr<Shared> = ref shared\npointer.count.store(1)\n",
		"type Shared = { count: Atomic<Int32> }\nmut shared: Shared = Shared { count = Atomic<Int32>.new(0) }\npointer: MutPtr<Shared> = ref shared\npointer.count.store(1)\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept, got %v:\n%s", result.Stderr, source)
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
			t.Fatalf("want Fun excluded from Channel/Task, got accept:\n%s", source)
		}
	}
}

// RFC 0049 item 8.2: no-value commands are valid as call statements and as
// direct defer/errdefer cleanup actions.
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

// RFC 0049 item 8.2: no-value commands are rejected in value positions with
// the "<name> produces no value" diagnostic.
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
