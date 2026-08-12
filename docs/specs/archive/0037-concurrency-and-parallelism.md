# RFC 0037: M:N Tasks, Concurrency, and Parallelism

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Targets: Windows x64 and POSIX x86-64
- Features: stackful `Task<T>` fibers, cooperative M:N scheduling, `spawn`,
  explicit `Task.yield()`, joining and detaching, bounded `Channel<T>`,
  scheduler-aware Mutex, and sequentially consistent atomics
- Created: 2026-08-11
- Revised: 2026-08-11
- Depends on: RFC 0008 (functions), RFC 0019 (generics), RFC 0026 (allocation
  and defer), RFC 0028 (`for`/`while` loop syntax), RFC 0029 (Error values),
  RFC 0031 (`EoS`), RFC 0035 (C-style copying and manual lifetimes), and RFC
  0036 (`Size`)
- Coordinates with: RFC 0020 (collections), RFC 0027 (Arena and Pool), RFC 0030
  (`print`), RFC 0032 (low-level operations), and the future I/O, FFI, and
  target-platform specifications

## Summary

Seawitch provides lightweight stackful tasks scheduled M:N over OS worker
threads:

```text
N tasks and fiber stacks
          |
          v
M scheduler worker threads
          |
          v
available processor cores
```

Many tasks can make concurrent progress while CPU-bound tasks execute in
parallel on multiple cores. Ordinary functions remain ordinary functions;
Seawitch adds no `async`, `await`, closure, or coroutine function kind.

```seawitch
fun square(number: Int32): Int32
    return number * number
end

fun calculate_both(): Int32 | Error
    first: Task<Int32> = try spawn square(6)
    errdefer first.detach()
    second: Task<Int32> = try spawn square(7)

    a: Int32 = first.join()
    b: Int32 = second.join()
    return a + b
end
```

Other examples using `try` are function-body fragments whose enclosing
function returns a type containing Error, as required by RFC 0029.

Task arguments and results use the language's normal shallow C-style copying.
Shared-memory synchronization and allocation lifetimes remain the programmer's
responsibility.

## Goals

1. Provide genuine M:N concurrency and multicore parallelism.
2. Make spawning ordinary named functions low ceremony.
3. Let ordinary blocking-looking code suspend only its current task when it
   uses scheduler-aware operations.
4. Preserve C23's shared-memory model and Seawitch's manual ownership rules.
5. Keep the language surface smaller than an async/await system.
6. Use a small runtime only when task features are linked.
7. Permit a simple first scheduler implementation without making its queue or
   stack strategy part of language semantics.

## Non-goals

- Closures or captured anonymous task bodies.
- Async functions, Futures, Promises, or `await`.
- Compiler-enforced race freedom, ownership transfer, or Send/Sync traits.
- Deterministic scheduling.
- Automatic safe-point, timer, signal, or arbitrary-instruction preemption.
- Automatic cancellation, deadlines, or timeouts.
- `select`, unbounded Channels, or zero-capacity rendezvous Channels.
- `parallel for`, task groups, priorities, or processor affinity.
- Direct source-level OS-thread creation in v1.
- Portable suspension of an arbitrary blocking C FFI call.
- Distributed, GPU, or SIMD execution.

## Task type

`Task<R>` is a built-in reference-like handle to one spawned execution whose
function returns `R`.

```seawitch
fun calculate(job: Job): Int32
    return job.left + job.right
end

task: Task<Int32> = try spawn calculate(job)
result: Int32 = task.join()
```

`R` may be any complete, shallow-copyable return type accepted by an ordinary
function, including Nil, Error, an object, an ADT, or a union. Inline
`Atomic<T>` and any aggregate containing one recursively are excluded because
the runtime stores R and `join()` copies it out. `Task<R>` is not itself the
result and does not add an Error member to `R`.

Task handles are shallow reference-like values under RFC 0035. Assignment,
parameter passing, return, and aggregate storage copy the handle. Every alias
refers to the same task. The compiler does not track which alias joins or
detaches it.

`Task.yield()`, `Channel<T>.new(...)`, `Mutex.new(...)`, and
`Atomic<T>.new(...)` are compiler-owned built-in operations. Their
type-qualified spelling is an explicit intrinsic exemption from RFC 0008's
rule that user-defined methods require a value receiver. This RFC does not add
user-defined static methods or constructors. The remaining Task, Channel,
Mutex, and Atomic operations are built-in instance methods on their values.

## Spawn expression

```ebnf
spawn-expression = "spawn" , direct-call-expression ;
```

`spawn` is a reserved prefix expression. Its operand must be a direct call to
a named, non-capturing function:

```seawitch
task: Task<Int32> = try spawn calculate(job)
```

The function call does not run in the spawning task. Instead:

1. the callee is resolved statically;
2. arguments are evaluated exactly once, from left to right;
3. each argument is shallow-copied into the new task's initial frame;
4. the runtime allocates a task control block and fiber stack; and
5. the new task becomes runnable after all argument copies complete.

If the function returns `R`, `spawn function(arguments)` has type
`Task<R> | Error`. Creation failure returns Error and starts no task. `try`
normally removes this creation Error:

```seawitch
task: Task<Int32> = try spawn calculate(job)
```

The entry function cannot capture lexical variables. All state must be passed
as explicit arguments. Those argument values follow ordinary shallow-copy
rules, so pointer and collection handles continue to alias their original
allocations:

```seawitch
type Job = {
    input: Ptr<Int32>,
    output: MutPtr<Int32>,
}

fun run_job(job: Job)
    job.output.value = job.input.value * 2
end

task: Task<Nil> = try spawn run_job(job)
task.join()
```

The programmer must keep referenced allocations alive until the task has
stopped using them.

## Result and Error propagation

The task retains the entry function's exact result until it is joined:

```seawitch
fun load(path: String): Config | Error
    ...
end

task: Task<Config | Error> = try spawn load(path)
config: Config = try task.join()
```

The two `try` expressions handle different failures:

```seawitch
task: Task<Config | Error> = try spawn load(path) // Task creation failed.
config: Config = try task.join()                  // Task returned Error.
```

`join()` returns exactly `R`; it does not add a scheduler Error. An
unrecoverable trap in any task terminates the process under the ordinary trap
rules; it is not converted into an Error value.

Each task has its own ordinary call stack and lexical cleanup stacks. `defer`,
`errdefer`, `try`, and explicit return behave inside a task exactly as they do
inside any other function execution.

Every recoverable runtime failure in this RFC constructs the reserved Error
with the Seawitch operation's call-site location. The compiler passes the
source-unit name, line, and byte column as constants to the runtime helper. A
spawn failure therefore identifies the `spawn` expression; a Channel failure
identifies the corresponding constructor or send call. Runtime C
implementation filenames never replace the Seawitch location.

## Join, detach, and reclamation

```text
task.join(): R
task.detach(): Nil
```

`join()` waits until the task finishes, copies out its result, and lets the
runtime reclaim the task's stack and control block. If called by another task,
waiting parks only the caller. The joining OS worker remains available to run
other tasks.

`detach()` declares that no result will be observed. If the task has completed,
its runtime storage is reclaimed immediately. Otherwise, the runtime reclaims
it when the task completes.

Exactly one successful `join()` or `detach()` is permitted for a Task. Joining
twice, detaching twice, joining after detach, detaching after join, or using an
invalid alias is a programmer error under the ordinary C-style lifetime model.
The runtime is not required to retain a tombstone merely to diagnose a stale
shallow alias after reclaiming the control block.

Joining the current Task through one of its own aliases is a programmer error.
Concurrent join or detach calls through different aliases are programmer
errors. Join cycles are ordinary deadlocks and are not detected. Implementations
may trap cheaply detectable misuse before reclaiming the control block, but no
use-after-reclaim trap is guaranteed.

A Task that is never joined or detached retains its result and runtime metadata
until process termination. `detach()` is part of v1 so intentionally
fire-and-forget work has an explicit C-like reclamation path.

There is no `Task.free()`. Join or detach is the C-like lifecycle boundary and
avoids a second cleanup operation that has no independent purpose.

## Automatic process scheduler

The runtime owns one process scheduler. When a program uses Task, Channel, or
the scheduler-aware Mutex, generated C initializes that scheduler before
executing Seawitch source. Source code does not construct or pass a Scheduler
object. Programs using none of those features link no scheduler startup path.

Initialization either completes fully or releases every partial runtime
resource and terminates with a runtime-startup diagnostic. It cannot return an
ordinary Error because Seawitch source has not started. A later Task allocation
failure still returns Error from its exact `spawn` expression.

The scheduler uses the number of online logical processors reported by the
target runtime, with a minimum of one. The initial process thread is worker
zero; the runtime creates the remaining workers with C23 `thrd_create`. This
count is runtime policy in v1 and does not affect program correctness. The
small platform layer provides the processor-count query because C23 has no
portable equivalent.

Generated C runs the Seawitch entry point as one compiler-owned root Task on
worker zero. The root has the same stack, yield, Channel, join, and Mutex
behavior as a spawned Task but has no source-level Task handle and cannot be
joined or detached. Keeping one execution model avoids separate blocking paths
for the initial thread.

The root Task remains pinned to worker zero so generated C `main` regains its
scheduler context when the root completes. Every spawned Task may execute on
any worker and may resume on another worker after suspension. Programs cannot
depend on worker identity or task execution order.

The initial implementation may use:

- one global ready queue protected by a native mutex;
- one native condition variable to wake idle workers; and
- a fixed set of worker threads.

This is the minimum practical multicore M:N scheduler. Per-worker queues, work
stealing, lock-free queues, NUMA placement, and adaptive worker counts are
permitted later optimizations. They do not change language behavior.

Process exit follows C. When the root Task returns, the scheduler stops taking
new work and generated C returns from `main`; process termination ends the
remaining worker threads. It does not wait for, implicitly join, or run deferred
actions in source-level Tasks. Joinable and detached Tasks still active when
root returns are abandoned to process termination. Programs requiring their
results or cleanup must join them explicitly before returning from the root.

## Scheduling and fairness

V1 scheduling is cooperative. A running task keeps its worker until it:

- completes;
- actually parks in `Task.join()`, Channel, or Mutex; or
- explicitly calls `Task.yield()`.

```text
Task.yield(): Nil
```

`Task.yield()` places the current task at the back of a ready queue and lets the
worker run another runnable task. If no other task is runnable, the yielding
task may resume immediately. It is valid from any ordinary function executing
inside the root or a spawned Task; it is not generator `yield` and produces no
value.

The compiler inserts no scheduler polls at function entries, loop backedges, or
ordinary expressions. A CPU loop that neither blocks nor yields can monopolize
one worker indefinitely:

```seawitch
while has_work() do
    calculate()
end
```

This condition-controlled form is legal and remains the programmer's
responsibility. The narrower literal `while true do` case is rejected by the
starvation rule below when any repeating path lacks an explicit yield.

Cooperative code yields explicitly at a frequency appropriate to its work:

```seawitch
while has_work() do
    calculate_chunk()
    Task.yield()
end
```

Other workers continue running in parallel, so one non-yielding task consumes
one worker rather than stopping the complete process. If at least as many
non-yielding tasks run as there are workers, queued tasks may make no progress.
Such starvation is a programmer error.

### Starvation checks

Pure cooperative scheduling cannot guarantee fairness without turning a check
into an automatic scheduling poll, which would restore the overhead this design
removes. V1 does not introduce warning severity or emit advisory concurrency
diagnostics.

When a program links the scheduler runtime, the checker builds the direct
named-function call graph rooted at the Seawitch entry point and every direct
`spawn` entry. Every literal `while true do` loop in that reachable code must
execute an explicit `Task.yield()` call on every path that repeats the loop.
Violation is a fatal Semantic Error and suppresses C generation.

A path ending in `break` targeting that loop, `return`, or an unrecoverable trap
does not repeat the loop and needs no yield. A `break` targeting a nested loop
does not exit the checked outer loop. Normal fallthrough and `continue`
targeting the checked loop are repeating paths and must pass through
`Task.yield()` first:

```seawitch
while true do
    if finished then
        break
    end

    if skip then
        Task.yield()
        continue
    end

    process_one()
    Task.yield()
end
```

A yield satisfies the rule only on paths that actually execute it. A yield
inside a conditional or nested loop does not cover an outer repeating path
unless every such path is guaranteed to reach and execute that yield.

Calls to helper functions do not satisfy the rule, even if their bodies happen
to yield. Keeping the scheduling point visible in the loop avoids
interprocedural must-yield analysis and prevents a later helper edit from
silently making the loop unsafe. `join`, Channel, and Mutex calls do not count
because they may complete immediately without parking.

The rule intentionally does not reject finite `for` loops or
condition-controlled `while` loops. General termination and starvation are not
decidable cheaply; those loops remain programmer responsibility. This focused
rule catches the clearest accidental worker starvation without adding warning
infrastructure or hidden runtime polling.

A scheduler-aware operation is still not a substitute for the explicit yield:

```seawitch
while true do
    item: Job | EoS = queue.receive()
    handle(item)
    Task.yield()
end
```

`receive()` parks when the Channel is empty, but it may return immediately while
items remain queued. The explicit yield guarantees a scheduling point on every
repeating path regardless of Channel state.

A runtime starvation watchdog is deferred. It would require another native
thread, timing policy, and false-positive policy while remaining unable to
safely reschedule arbitrary code.

## Fiber stacks and context switching

Tasks are stackful fibers. Suspending a task preserves its ordinary function
frames, locals, and cleanup state. No function is transformed into an async
state machine.

Each Task reserves a 1 MiB virtual address range in total. At least one page in
that range is inaccessible and acts as the stack guard; the remainder is usable
stack. Crossing the guarded end traps rather than corrupting adjacent memory.
Physical pages are acquired on demand rather than eagerly allocating the whole
range per Task.

Windows x64 requests a 1 MiB total reserve and 64 KiB initial commit through
`CreateFiberEx`; Windows grows committed pages within that reserve. POSIX has
no separate Windows-style commit operation. POSIX x86-64 maps one 1 MiB
anonymous region whose pages are demand-paged by the OS, then protects its end
page with `mprotect`. It does not add a signal-based stack-growth handler merely
to imitate Windows commit accounting.

Both implementations therefore expose the same source semantics and 1 MiB
maximum while using their native virtual-memory behavior. Growable reserves,
stack copying, and source-configurable stack sizes are deferred.

C23 has no portable fiber context-switch API. V1 therefore ships both of these
verified native context backends:

- Windows x64 uses `ConvertThreadToFiber`, `CreateFiberEx`, `SwitchToFiber`,
  and `DeleteFiber`;
- POSIX x86-64 uses a small compiler-owned System V ABI context switch plus
  `mmap`/`mprotect` stack storage and guard pages.

A target outside those two backend families fails compilation when task
features are used. Adding POSIX AArch64 or another ABI requires another focused
context backend; POSIX thread portability alone cannot supply register and
stack switching for a new processor ABI.

All scheduler code calls one private context interface equivalent to:

```c
context_create(entry, argument)
context_switch(from, to)
context_destroy(context)
logical_processor_count()
```

Only context operations, stack allocation, and the logical-processor query are
platform-specific. Task control blocks, ready and wait queues, worker loops,
join, Channel, Mutex, Atomic, shutdown, and Error construction have one shared
implementation with no Windows/POSIX semantic branches.

Every worker establishes one scheduler context before running Tasks. On
Windows this converts the worker with `ConvertThreadToFiber`; on POSIX it saves
the worker's ordinary C context. A Task context starts in one shared trampoline
that calls the typed Task entry adapter, stores its result, marks completion,
wakes joiners, and switches back without returning through an invalid stack.

The POSIX System V x86-64 switch preserves the stack and instruction position,
required callee-preserved general registers, required floating-point control
state, and ABI stack alignment. The Windows backend delegates the corresponding
context state to the verified Fiber APIs. Context migration uses the same
scheduler synchronization as ready-queue transfer; no context may execute on
two workers simultaneously.

C23 `<threads.h>` supplies portable OS-thread, mutex, and condition-variable
operations such as `thrd_create`, `mtx_lock`, and `cnd_wait`. The runtime uses
them as the only M worker-thread API. C permits an implementation to omit this
optional library and define `__STDC_NO_THREADS__`; such a toolchain cannot build
this RFC's task runtime and receives an Unsupported Error. Seawitch does not
maintain parallel pthread and Win32 worker-thread fallbacks.

Neither C23 threads nor pthreads provide the N user-space fiber contexts
scheduled on those workers. Creating one C thread or pthread per Task would be
1:1 threading, not this RFC's M:N design. The backend-specific part is saving
registers and switching between separately allocated Task stacks on one worker.

## Scheduler-owned allocation

Task stacks, task control blocks, worker queues, and scheduler synchronization
objects are runtime implementation storage. The process scheduler allocates and
reclaims them internally. `spawn` does not accept a user Heap, Arena, or Pool.

This is a narrow exception to allocator passing. Arbitrary short-lived user
allocators cannot safely own stacks that may outlive the spawning scope or move
between workers. User payloads, collections, Channels, and application objects
continue to use their explicit allocators.

This exception is a settled v1 rule. Exposing a Scheduler allocator parameter
would add lifetime restrictions and source ceremony without changing ownership
of user data.

The task runtime is linked only when a program uses Task, Channel, or the
scheduler-aware Mutex. Programs that do not use these features pay no scheduler
startup cost.

### Runtime build artifact

The compiler distribution ships the task runtime as source rather than as a
prebuilt library:

- one shared C23 source and header implement Task control blocks, scheduling,
  queues, join, Channel, Mutex, shutdown, and Error construction;
- one small Windows x64 C source implements the private platform interface with
  Fiber and virtual-memory APIs; and
- one small POSIX x86-64 C source plus one System V assembly source implement
  guarded stacks, processor-count discovery, and context switching.

When task-runtime features are present, the compiler's build driver compiles
the shared source and exactly one target backend, then links their objects with
the generated application C. When those features are absent, it compiles and
links none of them. Runtime sources are compiler-owned support files, not copied
into every generated module and not exposed as Seawitch modules.

Shipping source avoids compiler, C-library, and object-format compatibility
problems from prebuilt runtime libraries. The application output remains
human-readable C23; the unavoidable POSIX context-switch routine remains a
small, separately inspectable assembly file.

## Channel<T>

`Channel<T>` is a bounded, scheduler-aware, multi-producer/multi-consumer FIFO
queue. It is distinct from RFC 0031's lazy, single-task `Stream<T>`.

```seawitch
channel: Channel<Job> = try Channel<Job>.new(h, 64)
defer channel.free(h)
```

```text
Channel<T>.new(Heap, capacity: Size): Channel<T> | Error
channel.send(value: T): Nil | Error
channel.receive(): T | EoS
channel.close(): Nil
channel.free(Heap): Nil
channel.length(): Size
channel.capacity(): Size
channel.is_closed(): Bool
```

Capacity must be greater than zero. A compile-time-known zero capacity is a
Type Error. A runtime zero capacity makes `Channel.new` return Error without
allocating Channel storage. `send` parks the current task while the Channel is
full and open. `receive` parks it while the Channel is empty and open. Parking
does not block the worker thread.

Send shallow-copies T into the FIFO. Receive removes and shallow-copies out the
oldest value. Sending after close, or being awakened by close before enqueueing,
returns Error.

Receive has no recoverable runtime Error. Invalid or corrupted runtime state
traps; ordinary closure is represented solely by `eos`. This permits Error to
be sent as an ordinary element without confusing a produced Error with a
receive failure:

```seawitch
errors: Channel<Error> = try Channel<Error>.new(h, 16)
step: Error | EoS = errors.receive()
```

`close()` is idempotent. It wakes blocked senders and receivers and does not
discard queued values. Once a closed Channel becomes empty, `receive()` returns
`eos`.

`free(h)` requires the Channel to be closed, empty, and unused by concurrent or
blocked operations. It releases only Channel storage and never recursively
frees allocations referenced by T values.

Channel handles copy shallowly. Element values use ordinary C-style shallow
copying. The programmer owns synchronization, pointee lifetime, and exactly-once
cleanup of allocations referenced by sent values.

Channel accepts any complete, finite-sized T that can be shallow-copied under
RFC 0035. Task, Channel, and Mutex are pointer-sized handles and may therefore
be sent like other handles. Sending one does not transfer lifecycle ownership;
aliases and exactly-once join, detach, close, or free remain programmer
responsibilities.

Inline Atomic values cannot be copied, so `Atomic<T>` and any aggregate that
contains one recursively are invalid Channel element types. This is the only
special recursive Channel element exclusion in v1.

`EoS` and a normalized top-level union containing `EoS` are also rejected as T
because a produced `eos` would be indistinguishable from closed-and-drained
completion. An aggregate payload may contain EoS when the outer T identity
remains distinct.

`length()` and `is_closed()` take the Channel's internal synchronization and
return snapshots. Another Task may change the state immediately afterward;
neither result predicts whether a later send or receive will park. `capacity()`
returns the immutable construction capacity.

## Mutex

Mutex is a heap-backed, scheduler-aware mutual exclusion handle:

```text
Mutex.new(Heap): Mutex | Error
mutex.lock(): Nil
mutex.unlock(): Nil
mutex.free(Heap): Nil
```

Waiting for a held Mutex parks the current task rather than its worker thread.
Unlock wakes one waiter. Successful unlock synchronizes with the later lock
that acquires the Mutex.

Mutex ownership is recorded by stable Task identity, never by worker-thread
identity. A suspended Task may resume on another worker and still owns every
Mutex it has not unlocked.

Mutex is non-recursive. Recursive lock, unlock by a non-owner, double unlock,
free while locked or awaited, double free, and use after free are programmer
errors. The runtime traps states detectable from a live Mutex control block,
such as recursive lock or wrong-owner unlock. It need not retain freed control
blocks solely to diagnose stale aliases. Ordinary lock and unlock have no
recoverable failure and therefore do not return Error.

The protected data remains separate. Seawitch neither infers what a Mutex
protects nor prevents unsynchronized access through another alias.

## Atomic<T>

`Atomic<T>` is an inline built-in wrapper over C23 `_Atomic(T)`. V1 supports
Bool, Int32, UInt32, Int64, UInt64, and Size.

```text
Atomic<T>.new(initial: T): Atomic<T>
atomic.load(): T
atomic.store(value: T): Nil
atomic.exchange(value: T): T
atomic.fetch_add(value: T): T
atomic.fetch_sub(value: T): T
atomic.compare_exchange(expected: T, desired: T): Bool
```

`fetch_add` and `fetch_sub` are unavailable for Bool. Every v1 operation is
sequentially consistent; no source-level memory-order argument exists.

`compare_exchange` is the strong, non-spurious form. It compares the current
value with `expected`; on equality it stores `desired` and returns true,
otherwise it leaves the Atomic unchanged and returns false. `expected` is an
ordinary input value and is never rewritten.

Atomic is inline, has no allocator and no `free`, and is accessed only through
its methods. Ordinary copying, assignment, arithmetic, address-taking, and
collection storage are rejected. An Atomic may be an object member shared by
pointer.

Whether an operation is lock-free is not a language guarantee.

## C23 memory model

Shared memory follows the C23 memory model. Concurrent conflicting access to
the same non-atomic object, when at least one access writes and no
synchronization orders them, is a data race and the program has no guaranteed
behavior. The compiler does not attempt general race detection.

The following establish synchronization edges:

- spawn publishes copied arguments before the new task begins;
- successful join publishes task writes before the joiner continues;
- Mutex unlock synchronizes with the later acquiring lock;
- Channel send publishes prior writes to the receive removing that value; and
- Atomic operations follow sequential consistency.

List, Dict, String handles, pointers, Arena, and Pool are not automatically
synchronized. Read-only sharing is valid while storage remains alive. Mutation
requires explicit synchronization. Arena and Pool are not concurrently usable
in v1 without external synchronization.

Heap allocation and free operations are safe to call concurrently from
different Tasks. This guarantees only allocator metadata safety; it does not
synchronize access to allocated payloads. A platform Heap backend that cannot
provide concurrent allocation must serialize its allocator operations
internally. Arena and Pool retain their cheaper unsynchronized behavior.

## Blocking calls and future I/O/FFI

Task join, Channel waits, Mutex waits, and future scheduler-aware timers and I/O
park only the current task.

An arbitrary C call is opaque to the scheduler. If it blocks, it blocks that
worker OS thread. The future FFI specification must provide an explicit
blocking-call mechanism backed by dedicated blocking threads. V1 does not
pretend that unknown C calls are automatically asynchronous.

## C and platform lowering

- Use C23 `<threads.h>` exclusively for worker creation, scheduler mutexes, and
  condition variables.
- If `__STDC_NO_THREADS__` is defined or the implementation is not verified,
  fail with an Unsupported Error instead of selecting another thread API.
- Windows x64 fiber contexts use native Windows Fiber APIs and
  `CreateFiberEx` stack reserve/commit parameters.
- POSIX x86-64 fiber contexts use the compiler-owned System V ABI switch and
  guarded `mmap` stack regions. The switch saves and restores every register
  required by that ABI, including stack and instruction position.
- The same narrow platform layer reports the online logical-processor count;
  all worker creation and scheduler behavior above it remains shared.
- Windows obtains that count with `GetActiveProcessorCount(ALL_PROCESSOR_GROUPS)`;
  POSIX obtains it with `sysconf(_SC_NPROCESSORS_ONLN)`. A failed, zero, or
  negative query result falls back to one worker.
- Task functions lower to ordinary generated C functions plus a small typed
  entry adapter.
- Ordinary functions and loops receive no hidden scheduler polls; only explicit
  `Task.yield()` and scheduler-aware blocking operations enter the scheduler.
- Spawn argument frames and results use concrete generated C structs; no
  source-level `void *` erasure is exposed.
- Runtime helpers that return Error receive compiler-emitted Seawitch filename,
  line, and byte-column constants for the source operation.
- Task handles lower to pointer-sized references to runtime control blocks.
- The baseline ready queue uses a native mutex and condition variable.
- Context switching remains below portable C23 even when worker creation uses
  `<threads.h>`.
- Channel uses a fixed-capacity ring buffer and scheduler wait queues.
- Mutex uses a scheduler wait queue rather than blocking a worker-native mutex
  for the complete wait.
- Atomic lowers to C23 `_Atomic(T)` or a verified equivalent.
- Unsupported target and impossible runtime states fail closed.

Target validation does not trust the header name alone. A native toolchain
profile must compile, link, and pass a focused probe covering `thrd_create`,
`thrd_join`, `mtx_*`, and `cnd_*`. A cross-compilation profile must name a
previously verified C23 thread runtime for its exact toolchain and target;
otherwise Task use fails before generated program compilation.

## Diagnostics

Required focused diagnostics include:

```text
[Type Error] spawn requires a direct call to a named function
[Type Error] task entry arguments must be complete and shallow-copyable
[Type Error] Task result type must be complete and shallow-copyable
[Type Error] compile-time Channel capacity must be positive
[Type Error] Channel element contains a non-copyable Atomic value
[Type Error] Channel element cannot be or include EoS as a top-level member
[Type Error] Atomic element type is not supported
[Type Error] Atomic values cannot be copied or assigned directly
[Unsupported Error] verified C23 threads are unavailable for the selected target
[Unsupported Error] no verified task context backend exists for the selected target
[Semantic Error] repeating while true path must call Task.yield()
```

Task alias misuse, failure to join or detach, pointee lifetime violations,
data races, deadlocks, invalid Mutex ownership, and unsynchronized access remain
programmer errors rather than ownership-checker diagnostics.

## Required conformance coverage

Implementation is complete only when focused tests establish all of the
following:

1. more runnable Tasks than workers are scheduled M:N, and independent
   CPU-bound Tasks can execute on different workers;
2. spawn arguments evaluate once from left to right and become visible before
   the new Task starts;
3. failed scheduler startup releases partial runtime storage and exits before
   Seawitch source; failed Task creation starts no Task and reports the spawn
   call's Seawitch source location;
4. join parks a Task caller, returns exactly R, publishes completed writes, and
   reclaims runtime storage; non-copyable Task result types are rejected;
5. detach discards the result and reclaims storage at completion;
6. explicit `Task.yield()` requeues the root or a spawned Task;
7. every repeating path through a task-reachable literal `while true` requires
   a visible, executed `Task.yield()`; nested-loop breaks do not exit the outer
   loop, and conditional or nested yields count only for paths that execute
   them, while exiting paths and other loop forms are not rejected;
8. generated ordinary functions and loops contain no hidden scheduler polls;
9. the Seawitch entry point runs as a root Task on worker zero, and returning
   from it does not implicitly join live source-level Tasks;
10. Channel is bounded, FIFO, multi-producer/multi-consumer, parks Tasks rather
   than workers, wakes on close, drains queued values, and then returns `eos`
   without adding Error to receive;
11. Channel accepts ordinary shallow-copyable handles and Error values, rejects
    recursively contained Atomic values and top-level EoS, and reports send or
    construction failures at the Seawitch operation site;
12. Mutex ownership follows Task identity across worker migration and its
    unlock-to-lock synchronization edge is preserved;
13. supported Atomic operations are sequentially consistent and unsupported
    element types fail before C generation; strong compare-exchange never
    fails spuriously and never rewrites its expected argument;
14. Heap allocation and free remain safe under concurrent Task calls while
    Arena and Pool receive no implicit synchronization;
15. programs not using task runtime features link no scheduler startup path;
16. Channel observation methods return synchronized snapshots without
    promising the next operation will not park;
17. unavailable or unverified C23 thread implementations and unsupported
    context targets fail with the required Unsupported Error;
18. worker count uses the platform-reported online logical-processor count,
    clamped to at least one, without changing shared scheduler semantics; and
19. the compiler builds and links the shipped shared runtime source and exactly
    one target backend only when scheduler features are used.

## Implementation direction

The minimum implementation order is:

1. implement the outstanding dependency RFCs used by this feature;
2. add the shipped runtime-source layout and conditional build/link path;
3. add the C23 `<threads.h>` worker layer, capability probe, and Unsupported
   Error;
4. one private platform interface for context operations, guarded stacks, and
   logical-processor count, implemented by Windows x64 and POSIX x86-64;
5. the shared scheduler-facing wrapper over that platform interface;
6. one global locked ready queue, root Task, and fixed worker set;
7. `Task<R>`, `spawn`, `join`, detach, and Task result storage;
8. explicit `Task.yield()` and repeating-path validation for literal
   `while true` loops;
9. scheduler-aware Mutex;
10. bounded MPMC Channel;
11. C23 Atomic integration; and
12. cross-backend conformance tests.

Work stealing, per-worker queues, growable stacks, automatic preemption, and
specialized blocking pools are later features, not v1 prerequisites.

## Deferred

- Cancellation, deadlines, and timeouts.
- Task groups and structured-concurrency scopes.
- Task priorities, names, affinity, and task-local storage.
- `select`, nonblocking Channel operations, unbounded Channels, and
  zero-capacity rendezvous Channels.
- `parallel for` and automatic collection partitioning.
- Direct OS threads and dedicated blocking-call tasks.
- Weak atomic memory orderings, fences, pointer atomics, and wait/notify.
- Thread-safe Arena and Pool variants.
- Race and deadlock sanitizers.
- Runtime starvation watchdog diagnostics.
- Per-worker queues, work stealing, growable stacks, and NUMA scheduling.

## Finalized implementation choices

All readiness choices are settled in the normative sections:

- v1 implements Windows x64 and POSIX x86-64 together;
- C23 `<threads.h>` is the only worker-thread layer; unavailable implementations
  fail with an Unsupported Error;
- fiber context switching remains native because C23 has no fiber API;
- scheduler behavior above the private context interface is shared across both
  target families;
- the only additional platform query is the online logical-processor count;
- every Task has a 1 MiB total virtual stack reservation including an
  inaccessible guard page;
- Windows initially commits 64 KiB while POSIX uses demand-paged anonymous
  mapping without signal-driven commit emulation;
- the Seawitch entry point is the pinned root Task on worker zero;
- the scheduler owns its runtime allocations;
- detach is in v1;
- Channel receive returns `T | EoS`, so Error is a valid element;
- recursively contained Atomic values and top-level EoS are excluded from
  Channel elements;
- Heap is thread-safe;
- process exit follows C;
- the language adds no warning severity;
- an unsafe repeating path through task-reachable `while true` is a fatal
  Semantic Error; and
- a runtime watchdog is deferred.
