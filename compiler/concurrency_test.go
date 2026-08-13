package compiler

import (
	"strings"
	"testing"
)

// RFC 0037 concurrency integration tests: spawn, Task join/detach/yield,
// Channel, Mutex, and Atomic programs compile end to end, and invalid uses
// fail with the specified diagnostics. The gcc build-and-run coverage lives
// behind the c23 build tag (c23_concurrency_smoke_test.go).

func TestSpawnAndJoinCompile(t *testing.T) {
	source := "fun square(value: Int32): Int32\n    return value * value\nend\nfun run(): Int32 | Error\n    task: Task<Int32> = try spawn square(6)\n    return task.join()\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.MainC, "hex_task_spawn(") {
		t.Fatalf("generated C lacks the spawn call:\n%s", result.MainC)
	}
	if !strings.Contains(result.MainC, "hex_task_join_Int32(") {
		t.Fatalf("generated C lacks the typed join call:\n%s", result.MainC)
	}
	if !strings.Contains(result.MainH, "hex_scheduler_init") {
		t.Fatalf("generated header lacks the scheduler runtime:\n%s", result.MainH)
	}
	if !strings.Contains(result.MainC, "hex_scheduler_init();") {
		t.Fatalf("generated main does not initialize the scheduler:\n%s", result.MainC)
	}
	if !strings.Contains(result.MainC, "hex_task_complete(hex_root_task);") {
		t.Fatalf("generated main does not complete the root task:\n%s", result.MainC)
	}
}

func TestSpawnNilResultCompiles(t *testing.T) {
	// RFC 0048: replace the accepted Task<Nil> program with a rejection;
	// standalone Nil is invalid, so the worker result is rejected.
	source := "fun worker(): Nil\n    Task.yield()\n    return nil\nend\nfun run(): Int32 | Error\n    task: Task<Nil> = try spawn worker()\n    task.join()\n    return 1\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Nil is valid only as a member of a union with a non-Nil type") {
		t.Fatalf("want standalone-Nil diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestChannelPipelineCompiles(t *testing.T) {
	source := "fun produce(ch: Channel<Int32>): Bool\n    ch.send(1)\n    ch.send(2)\n    ch.close()\n    return true\nend\nfun run(): Int32 | Error\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n    defer ch.free(h)\n    producer: Task<Bool> = try spawn produce(ch)\n    producer.join()\n    mut total: Int32 = 0\n    while true do\n        step: Int32 | EoS = ch.receive()\n        if step is EoS\n            break\n        end\n        total = total + step\n        Task.yield()\n    end\n    return total\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
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
		if !strings.Contains(result.MainC, fragment) && !strings.Contains(result.MainH, fragment) {
			t.Fatalf("generated output lacks %s:\n%s", fragment, result.MainC)
		}
	}
}

func TestMutexCompiles(t *testing.T) {
	source := "fun worker(m: Mutex): Bool\n    m.lock()\n    m.unlock()\n    return true\nend\nfun run(): Int32 | Error\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    defer m.free(h)\n    mutex_task: Task<Bool> = try spawn worker(m)\n    mutex_task.join()\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
	if !strings.Contains(result.MainH, "hex_mutex_lock_hex_mutex(") {
		t.Fatalf("generated header lacks the mutex helpers:\n%s", result.MainH)
	}
}

func TestAtomicOperationsCompile(t *testing.T) {
	source := "fun run(): Bool\n    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n    old: Int32 = counter.fetch_add(1)\n    counter.fetch_sub(1)\n    counter.store(5)\n    loaded: Int32 = counter.load()\n    swapped: Int32 = counter.exchange(6)\n    expected: Bool = counter.compare_exchange(6, 7)\n    mut flag: Atomic<Bool> = Atomic<Bool>.new(true)\n    ready: Bool = flag.load()\n    return ready and expected\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
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
		if !strings.Contains(result.MainH, fragment) && !strings.Contains(result.MainC, fragment) {
			t.Fatalf("generated output lacks %s", fragment)
		}
	}
}

func TestTaskMethodCallsRequireCorrectArity(t *testing.T) {
	source := "fun square(value: Int32): Int32\n    return value * value\nend\nfun run(): Int32 | Error\n    task: Task<Int32> = try spawn square(6)\n    return task.join(1)\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "join expects no arguments") {
		t.Fatalf("want join arity diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestTaskHasNoUnknownMethods(t *testing.T) {
	source := "fun square(value: Int32): Int32\n    return value * value\nend\nfun run(): Int32 | Error\n    task: Task<Int32> = try spawn square(6)\n    task.cancel()\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Task has no method cancel") {
		t.Fatalf("want unknown-method diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectsNonDirectCall(t *testing.T) {
	for _, source := range []string{
		"fun run(): Int32 | Error\n    task: Task<Int32> = try spawn 5\n    return 0\nend\n",
		"type Point = { x: Int32, }\nfun scale(point: Point, factor: Int32): Int32\n    return point.x * factor\nend\nfun run(): Int32 | Error\n    point: Point = Point { x = 2 }\n    task: Task<Int32> = try spawn point.scale(3)\n    return 0\nend\n",
	} {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "spawn requires a direct call") {
			t.Fatalf("want direct-call diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

func TestSpawnRejectsNonCopyableArguments(t *testing.T) {
	source := "fun square(value: Int32): Int32\n    return value * value\nend\nfun run(): Int32 | Error\n    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n    task: Task<Int32> = try spawn square(0)\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("atomic not passed to spawn should still compile: %v", result.Stderr)
	}
	bad := "fun take_atomic(counter: Atomic<Int32>): Int32\n    return 0\nend\nfun run(): Int32 | Error\n    counter: Atomic<Int32> = Atomic<Int32>.new(0)\n    task: Task<Int32> = try spawn take_atomic(counter)\n    return 0\nend\n"
	result = Compile(bad)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Atomic") {
		t.Fatalf("want atomic-argument diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectsAtomicResult(t *testing.T) {
	source := "fun make_atomic(): Atomic<Int32>\n    return Atomic<Int32>.new(0)\nend\nfun run(): Int32 | Error\n    task: Task<Atomic<Int32>> = try spawn make_atomic()\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "shallow-copyable") {
		t.Fatalf("want atomic-result diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestChannelRejectsInvalidElements(t *testing.T) {
	for _, source := range []string{
		"fun run(): Int32 | Error\n    h: Heap = Heap.new()\n    ch: Channel<Int32 | EoS> = try Channel<Int32 | EoS>.new(h, 4)\n    return 0\nend\n",
		"fun run(): Int32 | Error\n    h: Heap = Heap.new()\n    ch: Channel<Atomic<Int32>> = try Channel<Atomic<Int32>>.new(h, 4)\n    return 0\nend\n",
	} {
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
			t.Fatalf("want channel element diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
		}
	}
}

func TestChannelZeroCapacityRejectedAtCompileTime(t *testing.T) {
	source := "fun run(): Int32 | Error\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 0)\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "capacity must be positive") {
		t.Fatalf("want capacity diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestAtomicRejectsUnsupportedElements(t *testing.T) {
	source := "fun run(): Atomic<Float64>\n    return Atomic<Float64>.new(1.5)\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "Atomic element type is not supported") {
		t.Fatalf("want atomic element diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestAtomicFetchAddUnavailableForBool(t *testing.T) {
	source := "fun run(): Bool\n    flag: Atomic<Bool> = Atomic<Bool>.new(true)\n    old: Bool = flag.fetch_add(true)\n    return old\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "unavailable for Bool") {
		t.Fatalf("want Bool fetch diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestSpawnRejectedInsideDefer(t *testing.T) {
	source := "fun square(value: Int32): Int32\n    return value * value\nend\nfun run(): Int32 | Error\n    defer spawn square(1)\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "not permitted inside defer") {
		t.Fatalf("want defer diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestWhileTrueWithoutYieldRejectedWhenSchedulerLinked(t *testing.T) {
	source := "fun worker(): Bool\n    while true do\n    end\n    return true\nend\nfun run(): Int32 | Error\n    task: Task<Bool> = try spawn worker()\n    task.join()\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "while true loop must execute Task.yield()") {
		t.Fatalf("want starvation diagnostic, got exit=%d stderr=%v", result.ExitCode, result.Stderr)
	}
}

func TestWhileTrueWithYieldOnEveryRepeatingPathAccepted(t *testing.T) {
	source := "fun worker(): Bool\n    while true do\n        if false\n            break\n        end\n        Task.yield()\n    end\n    return true\nend\nfun run(): Int32 | Error\n    task: Task<Bool> = try spawn worker()\n    task.join()\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile failed: %v", result.Stderr)
	}
}

func TestWhileTrueWithoutSchedulerIsUnchecked(t *testing.T) {
	source := "fun spin(): Int32\n    while true do\n    end\n    return 0\nend\n"
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
		t.Fatalf("a program without the scheduler must not apply the starvation rule: %v", result.Stderr)
	}
}

func TestTaskTypesAreProtected(t *testing.T) {
	source := "type Task = { value: Int32, }\n"
	result := Compile(source)
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "cannot be redeclared") {
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
		if result := Compile(source); result.ExitCode != ExitFailure {
			t.Fatalf("want reject, got accept:\n%s", source)
		}
	}
	accepted := []string{
		"counter: Atomic<Int32> = Atomic<Int32>.new(0)\n",
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\n",
		"type Shared = { count: Atomic<Int32> }\nshared: Shared = Shared { count = Atomic<Int32>.new(0) }\npointer: Ptr<Shared> = ref shared\n",
	}
	for _, source := range accepted {
		if result := Compile(source); result.ExitCode != ExitSuccess {
			t.Fatalf("want accept, got %v:\n%s", result.Stderr, source)
		}
	}
}

func TestChannelAndTaskRejectFunElement(t *testing.T) {
	rejected := []string{
		"fun identity(x: Int32): Int32\n    return x\nend\nfun f(h: Heap): Nil | Error\n    ch: Channel<Fun<(Int32) : Int32>> = try Channel<Fun<(Int32) : Int32>>.new(h, 2)\n    return nil\nend\n",
		"fun identity(x: Int32): Int32\n    return x\nend\nfun f(h: Heap): Nil | Error\n    t: Task<Fun<(Int32) : Int32>> = try spawn identity(1)\n    return nil\nend\n",
	}
	for _, source := range rejected {
		if result := Compile(source); result.ExitCode != ExitFailure {
			t.Fatalf("want Fun excluded from Channel/Task, got accept:\n%s", source)
		}
	}
}

// RFC 0049 item 8.2: no-value commands are valid as call statements and as
// direct defer/errdefer cleanup actions.
func TestNoValueCommandsValidAsStatementsAndCleanup(t *testing.T) {
	source := "fun worker(): Bool\n    Task.yield()\n    return true\nend\n" +
		"fun run(): Int32 | Error\n" +
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
	result := Compile(source)
	if result.ExitCode != ExitSuccess {
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
		{"fun f(): Int32 | Error\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n    bad: Int32 = ch.close()\n    return 0\nend\n", "close produces no value"},
		{"fun f(): Int32 | Error\n    h: Heap = Heap.new()\n    ch: Channel<Int32> = try Channel<Int32>.new(h, 4)\n    bad: Int32 = ch.free(h)\n    return 0\nend\n", "free produces no value"},
		{"fun f(): Int32 | Error\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    bad: Int32 = m.lock()\n    return 0\nend\n", "lock produces no value"},
		{"fun f(): Int32 | Error\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    bad: Int32 = m.unlock()\n    return 0\nend\n", "unlock produces no value"},
		{"fun f(): Int32 | Error\n    h: Heap = Heap.new()\n    m: Mutex = try Mutex.new(h)\n    bad: Int32 = m.free(h)\n    return 0\nend\n", "free produces no value"},
		{"counter: Atomic<Int32> = Atomic<Int32>.new(0) bad: Int32 = counter.store(1)", "store produces no value"},
		{"fun worker(): Bool\n    Task.yield()\n    return true\nend\nfun run(): Int32 | Error\n    task: Task<Bool> = try spawn worker()\n    bad: Int32 = task.detach()\n    return 0\nend\n", "detach produces no value"},
		{"fun worker(): Bool\n    Task.yield()\n    return true\nend\nfun f(): Int32\n    bad: Int32 = Task.yield()\n    return 0\nend\n", "yield produces no value"},
		{"fun bad(ch: Channel<Int32>): Int32\n    return ch.close()\nend\n", "close produces no value"},
		{"fun f(ch: Channel<Int32>): Int32\n    if ch.close() noop: Int32 = 0 end\n    return 0\nend\n", "close produces no value"},
		{"fun f(ch: Channel<Int32>)\n    print(ch.close())\nend\n", "close produces no value"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, "\n"), testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}
