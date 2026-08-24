# RFC 0121: Scheduler-Aware Blocking Pool

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-08-24
- Scope: prevent synchronous native operations from blocking scheduler workers
- Depends on: the implemented M:N Task runtime, implemented RFC 0108
  (synchronous descriptor IO), and `docs/reference.md`
- Coordinates with: RFC 0039 (foreign calls), RFC 0055 (runnable generated-C
  validation), and RFC 0118 (concurrency safety)
- Supersedes: discarded RFC 0091's unresolved implementation directions
- Changes no Hexal grammar, type, function signature, or result contract

## Summary

When the Task scheduler and a potentially blocking native operation are both
reachable, execute that operation on one bounded program-wide blocking pool.
The calling Task parks; its scheduler worker immediately runs another ready
Task. Completion requeues the parked Task exactly once.

Use the same synchronous native calls already emitted by RFC 0108. Add no
poller, readiness API, async type, callback surface, executor parameter,
`would block` result, or platform-specific source syntax.

When no Task scheduler is emitted, native operations remain direct and their
generated artifacts remain byte-identical.

## Motivation

Hexal schedules many cooperative Task fibers over a fixed set of OS worker
threads. `Task.yield()`, Channel waits, Mutex waits, and Task joins switch a
waiting fiber back to its worker dispatcher. A synchronous native `read` or
`write` does not: the kernel blocks the worker thread itself.

With N scheduler workers:

- one blocked native call leaves N-1 workers;
- N blocked native calls prevent every remaining ready Task from running; and
- yielding before the call does not help after the Task resumes and enters the
  kernel.

The smallest general correction is to keep native operations synchronous but
run them on a separate bounded set of threads. This preserves the C-like IO
model while separating scheduler workers from threads whose job is explicitly
to block.

## Decision

### One mechanism

All selected blocking operations use one FIFO blocking pool. Do not classify
handles as pollable or non-pollable. Do not introduce a readiness/completion
backend in this RFC.

The pool works uniformly for borrowed standard handles, future synchronous
files, pipes and sockets, POSIX descriptors, Windows handles, and future
foreign operations explicitly lowered through this internal executor.

A future measured optimization may replace particular socket operations with
readiness or completion parking behind the same Hexal API. It is not required
for correctness or scheduler progress under this RFC.

### Pool size

- The pool contains the same number of threads as the scheduler's logical
  worker count.
- At least one blocking thread exists whenever the pool is selected.
- The existing platform-specific logical-processor query is reused.
- This RFC adds no `Project` setting or source-level tuning knob.
- Threads are created once during scheduler initialization and detached under
  the same process-lifetime model as scheduler workers.
- Failure to initialize synchronization or create every required blocking
  thread traps before user code; partial initialization never runs the program.

The pool bounds native thread growth. More simultaneous blocking operations
remain queued as parked Tasks. Saturation may delay another IO operation, but
it cannot consume a scheduler worker or prevent CPU-only ready Tasks from
running.

### Demand selection

Emit and initialize the pool only when both conditions hold:

1. The program selects the scheduler runtime, not merely `Atomic<T>` support.
2. Reachable code selects a native blocking path.

The initial native blocking paths are `IO.read`, `IO.write`, `IO.seek`, owned
`IO.close`, and `print`'s descriptor write-all sink.

Standard-handle lookup, capability checks, zero-length transfers, and Bytes
operations are not blocking jobs. A Bytes-only program selects no pool.

Selection is program-wide. A blocking operation in any reachable module enables
one shared pool, never one pool per module or operation family.

### Direct fallback

The task-aware internal frontend invokes the synchronous operation directly
when no current Hexal Task exists. This covers programs without the scheduler,
foreign entry before a future attach contract exists, and runtime initialization
before scheduler entry.

The fallback is never used from a running Hexal Task when the pool is selected.

## Source-language contract

No source surface changes.

- Existing `IO`, `Bytes`, `Seek`, `print`, `Task`, and Error signatures remain
  exact.
- `IO` remains synchronous to its caller: one result returns after the native
  operation completes.
- Short transfer, `EoS`, Error, close invalidation, capability, and ownership
  semantics remain unchanged.
- Source cannot observe which pool thread ran an operation.
- Waiting for the pool or native operation parks the current Task.
- A blocking operation is not a visible `Task.yield()` and does not satisfy the
  yield required in a task-reachable literal `while true`; it may complete
  immediately.
- No cancellation or timeout is introduced.

## Runtime architecture

### Ownership

The blocking executor is part of `hexal/concurrency.c` and declared
conditionally in `hexal/concurrency.h`. Do not add another generated component
pair.

`hexal/io.c` continues to own native descriptor and handle operations. In a
combined scheduler/IO program it includes the blocking declaration and wraps
only the native call. List growth, capability checks, union adaptation, and
source diagnostics remain on the calling Task.

`print` continues to use the IO descriptor write-all core; that core becomes
task-aware when the pool is selected.

### Job representation

Use one internal type-erased protocol:

```c
typedef void (*hex_blocking_entry)(void *context);

typedef struct hex_blocking_job {
    hex_blocking_entry entry;
    void *context;
    struct hex_blocking_job *next;
    hex_task *task;
    unsigned phase;
} hex_blocking_job;
```

The private field order may follow the surrounding template. These contracts
are fixed:

- Each operation creates its typed context and job on the calling Task's fiber
  stack.
- The inactive parked fiber retains that stack until completion.
- `hex_blocking_call(entry, context)` returns only after entry completion.
- The queue stores pointers to live stack frames and allocates nothing per call.
- A running Task has at most one blocking job because it cannot run again until
  completion.
- An entry invokes only its synchronous native operation and writes its result
  to the context. It does not call Hexal, yield, acquire a Hexal Mutex, or access
  the scheduler directly.

### Queue

- One program-wide FIFO queue is protected by one C23 `mtx_t`.
- Blocking workers wait on one C23 `cnd_t` while the queue is empty.
- Submission appends once and signals one worker.
- A worker removes one job, releases the queue mutex, invokes the entry, and
  completes the park/wake protocol.
- Native calls execute with neither the blocking mutex nor ready-queue mutex
  held.
- Queue length is bounded by live Tasks; it needs no separate allocation or
  capacity policy.

### Lost-wakeup-safe parking

Native operations may complete immediately. Completion must be safe before or
after the submitting fiber switches back to its scheduler.

Use these logical phases under the blocking mutex:

```text
parking   submitted; Task fiber has not committed its switch
parked    worker dispatcher has regained control from the fiber
done      native entry completed
```

Required handshake:

1. The Task initializes its stack job, records itself as parking, enqueues it,
   and switches to its scheduler context.
2. After the switch returns to the worker dispatcher, that dispatcher commits
   the job as parked under the blocking mutex.
3. If entry completion preceded commit, commit enqueues the Task exactly once.
4. If commit preceded completion, the completing pool worker enqueues the Task
   exactly once.
5. Neither side may publish the Task while its fiber is still executing.
6. The reactivated Task observes the result, clears its job link, and returns
   from `hex_blocking_call`.

Private implementation states may differ, but a check-then-switch sequence that
can enqueue a still-running fiber is forbidden.

Blocking-mutex acquisition precedes any ready-queue push. No path acquires the
blocking mutex while holding the ready-queue mutex.

This protocol is private to blocking jobs. Do not rewrite Channel, Mutex, or
join parking merely to share helpers.

## Native operation lowering

### IO read

- Capability and `max == 0` checks remain on the Task.
- Required List capacity is checked and reserved before submission.
- The job performs exactly the existing one native read into that reserved
  range.
- After wake, the Task updates List length only for positive transfer and
  performs existing result translation.

### IO write

- Capability and empty-View checks remain on the Task.
- The job performs exactly the existing one clamped native write.
- View storage remains governed by existing IO lifetime and synchronization
  rules while the Task is parked.

### Seek and close

- The job performs the existing synchronous native seek or close.
- Close remains non-retried after POSIX `EINTR` and invalidates the local IO
  binding under the existing rule regardless of reported failure.

### Print

- The job performs the complete existing write-all loop, including short-write
  and `EINTR` retries.
- Formatting and argument evaluation finish before submission.
- Output failure retains the existing trap behavior after wake.

### Bytes

Bytes operations remain in-memory calls on the scheduler worker and never
enter the pool.

## Generated-component rules

- Add a `Blocking` demand fact to the program-wide concurrency render model.
- Add a scheduler-aware flag to the IO render model.
- `concurrency.h` declares the internal blocking entrypoint only when selected.
- `concurrency.c` emits the pool and handshake only when selected.
- `io.c` includes `concurrency.h` only in the combined variant.
- The uncombined IO template renders byte-identically to the current file.
- Task-only, Channel-only, Mutex-only, Atomic-only, IO-only, print-only, and
  Bytes-only programs emit no pool.
- A program combining scheduler reachability with native IO or print emits one
  pool inside the existing concurrency pair.
- Module headers acquire no blocking declarations.
- Repeated compilation produces byte-identical artifacts.

## Failure behavior

- Pool initialization failure traps before user code.
- Submission allocates nothing and adds no recoverable Error path.
- Native failures retain existing Error or trap behavior.
- Pool saturation parks Tasks; it is not an Error.
- A pool worker never adds retries beyond the existing operation contract.
- Root completion retains the existing process-lifetime rule and does not join
  detached Tasks. Process termination ends detached scheduler and pool threads.

## Non-goals

- Readiness polling, IOCP dispatch, `epoll`, `kqueue`, `poll`, or `io_uring`.
- Async syntax, futures, promises, callbacks, executors, or an explicit IO
  context parameter.
- Timeouts, cancellation, priorities, work stealing, or configurable pool size.
- Increasing scheduler worker count while calls block.
- Making concurrent IO aliases safe.
- Scheduler integration for arbitrary foreign calls before RFC 0039.
- Reworking Channel, Mutex, or Task-join wait protocols.

## Required sweep

- Route every selected native IO transfer and print write-all path through the
  task-aware internal frontend; no running-Task path may bypass it.
- Keep capability checks, zero-length fast paths, List growth, Bytes operations,
  and union adaptation outside the pool.
- Keep runtime C in `compiler/generator/packages/*.c` and `*.h`, not Go strings.
- Remove no error, clamp, `EINTR`, short-transfer, close, or print behavior.
- Preserve demand-driven emission and byte-identical unaffected artifacts.
- During reference synchronization, replace the program-wide statement that
  descriptor operations may block a scheduler worker while preserving direct
  synchronous behavior outside a running Task.

## Implementation plan

### Phase 0: baseline and inventory

1. Record the green test/vet baseline and snippet manifest.
2. Record artifacts for IO-only, print-only, Task-only, Atomic-plus-IO,
   Bytes-plus-Task, Task-plus-IO, and Task-plus-print programs.
3. Inventory every direct native read, write, seek, close, `ReadFile`,
   `WriteFile`, and print write-all call in package templates.
4. Confirm worker count, ready-queue locking, context-switch return points, and
   component-selection facts against the current tree.

### Phase 1: demand discovery

1. Derive one blocking demand from scheduler selection combined with `readIO`,
   `writeIO`, `seekIO`, `closeIO`, or `printUsed`.
2. Add `Blocking` to typed concurrency and IO render models.
3. Keep it false for Atomic-only concurrency and Bytes-only stream use.
4. Add component tests for the full selection matrix before runtime emission.

### Phase 2: blocking runtime core

1. Add conditional declarations to `packages/concurrency.h`.
2. Add queue, synchronization, pool workers, stack jobs, direct fallback, and
   park handshake to `packages/concurrency.c`.
3. Initialize the logical scheduler worker count of pool threads.
4. Hook the worker dispatcher immediately after a Task switches back so it
   commits a parking job before that Task can be republished.
5. Add text tests for FIFO behavior, lock order, no allocation, direct fallback,
   state transitions, and one ready push in either completion ordering.

### Phase 3: IO integration

1. Give `io.c` a typed render model and conditional concurrency include.
2. Split only native-call portions into synchronous private entries; keep the
   uncombined rendered form byte-identical.
3. Add typed stack contexts for read, write, seek, and close.
4. Preserve validation and result handling on the Task.
5. Assert every selected native path uses `hex_blocking_call` and no Bytes path
   does.

### Phase 4: print integration

1. Route the complete descriptor write-all loop through one blocking job.
2. Preserve formatting, retries, and final trap behavior.
3. Assert print-only output remains byte-identical and Task-plus-print selects
   one pool.

### Phase 5: conformance and canonical docs

1. Implement every textual validation item below and no additional behavior.
2. Rebuild the snippet manifest once; accept only Task/scheduler artifacts
   combined with IO or print.
3. Update `docs/reference.md` once behavior stabilizes.
4. Add the runtime-only cases below to `docs/status.md` known coverage gaps
   until RFC 0055 can execute them.
5. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
6. Rebuild and restart the workbench.
7. Remove this RFC from open status, mark it implemented, and archive it only
   after code, text tests, artifacts, and reference agree.

## Validation

This section is exhaustive.

### Compiler and generated-text validation

- IO-only and print-only programs emit no scheduler or pool and retain current
  artifacts byte-for-byte.
- Task-only, Channel-only, Mutex-only, Atomic-only, Atomic-plus-IO, and
  Bytes-plus-Task programs emit no pool.
- Task-plus-IO and Task-plus-print emit exactly one pool in `concurrency.c` and
  one declaration owner in `concurrency.h`.
- Pool size reuses scheduler logical worker count with minimum one.
- Jobs and typed contexts are stack values; submission contains no allocation.
- The queue is FIFO and protected by one mutex/condition pair.
- Native entries execute with neither the blocking-queue mutex nor the
  ready-queue mutex held.
- Completion-before-commit and commit-before-completion each contain exactly
  one ready-queue publication.
- No path publishes a parking Task from the submitting fiber.
- A task-aware frontend falls back directly when `hex_current_task` is null.
- Capability failures and zero-length operations submit no job.
- IO read reserves on the Task, performs only native transfer in the pool, and
  updates length after wake.
- IO write, seek, close, and print preserve existing operation contracts.
- Bytes operations never reference blocking support.
- Every direct POSIX and Windows blocking call in IO/print is either a private
  synchronous entry behind the task-aware frontend or belongs to the
  byte-identical no-scheduler variant.
- No source signature, checked expression, result union, Error message, helper
  family, or public header changes beyond the internal concurrency declaration.
- Unaffected manifest entries remain byte-identical; changed entries are
  limited to programs combining scheduler use with IO or print.
- Repeated compilation produces byte-identical artifacts.
- Ordinary tests remain pure Go.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

### Runtime behavior retained as a coverage gap

These are required semantics but ordinary tests cannot execute generated C:

- With N scheduler workers blocked on N native operations, an unrelated CPU
  Task continues.
- More than N operations queue without creating more than N pool threads.
- Immediate completion cannot republish a still-running fiber.
- Completion before and after park commit each wake exactly once.
- Results written by a pool thread are visible after wake.
- Initialization failure traps before user code.
- Root completion terminates with blocked detached jobs under the existing
  process-lifetime rule.

Implementation records these exact cases under `docs/status.md` known coverage
gaps. RFC 0055 must make them executable when the driver can run generated
programs. Their temporary unverifiability does not authorize weaker text gates
or claims of runtime proof.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` to state:

- a synchronous native operation invoked by a running Task parks that Task and
  runs on the bounded program-wide pool;
- the pool has the scheduler's logical worker count;
- excess operations queue as parked Tasks;
- calls outside a current Task remain direct;
- Bytes never uses the pool;
- IO and print source semantics and failures are unchanged; and
- a blocking operation does not satisfy the explicit `Task.yield()` rule.

Remove the superseded statement that descriptor operations may block a
scheduler worker. No canonical documentation changes before implementation.
