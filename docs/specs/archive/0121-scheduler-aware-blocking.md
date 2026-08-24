# RFC 0121: Scheduler-Aware Blocking Pool

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-08-24
- Updated: 2026-08-24
- Scope: prevent synchronous native operations from blocking scheduler workers
  and make every Task park/wake transition lost-wakeup-safe
- Depends on: the implemented M:N Task runtime, implemented RFC 0108
  (synchronous descriptor IO), and `docs/reference.md`
- Coordinates with: RFC 0039 (foreign calls), RFC 0055 (runnable generated-C
  validation), and RFC 0118 (concurrency safety)
- Supersedes: discarded RFC 0091's unresolved implementation directions
- Changes no Hexal grammar, type, function signature, or result contract

## Summary

When the Task scheduler and a potentially blocking native operation are both
reachable, execute that operation on one program-wide blocking pool.
The calling Task parks; its scheduler worker immediately runs another ready
Task. Completion requeues the parked Task exactly once.

Use the same synchronous native calls already emitted by RFC 0108. Add no
poller, readiness API, async type, callback surface, executor parameter,
`would block` result, or platform-specific source syntax.

When no Task scheduler is emitted, native operations remain direct. Extracting
one shared synchronous native-call core may change generated IO text; do not
duplicate native implementations merely to preserve old artifact hashes.

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
run them on a separate blocking pool. This preserves the C-like IO
model while separating scheduler workers from threads whose job is explicitly
to block.

The current scheduler also publishes Tasks too early. `Task.yield()` pushes
itself to the ready queue before switching out. Channel and Mutex waits publish
the Task on a wait list before switching out, and another worker may wake it in
that interval. Task join additionally reads completion and installs its joiner
without synchronization. These paths can run one fiber concurrently on two OS
threads or lose a join wake. The blocking path must not add a fifth parking
protocol while leaving those four defects intact.

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

### Demand-grown blocking capacity

The pool begins with the scheduler's logical worker count, with minimum one.
The existing platform-specific processor query is reused. This RFC adds no
`Project` setting, hard thread ceiling, or source-level tuning knob.

The blocking mutex protects these logical counts in addition to the FIFO:

- baseline workers;
- total live or reserved workers;
- workers currently executing jobs; and
- queued jobs.

On submission:

1. Append the job under the blocking mutex.
2. Determine whether existing idle or already-reserved starting capacity can
   service every queued job.
3. If not, reserve exactly one additional worker slot before releasing the
   mutex and create one detached overflow worker.
4. Thread-creation failure removes that reservation, leaves the job in FIFO
   order, and signals the baseline workers. It adds no Error or trap; the job
   runs when existing blocking capacity becomes available.
5. Signal the condition variable and enter the common parking protocol.

Concurrent submissions serialize this calculation under the blocking mutex,
so they neither omit required growth nor create multiple workers for one unit
of unmet demand.

Baseline workers wait while idle. After an overflow worker completes a job, it
exits when the queue is empty and total workers exceed the baseline; it reserves
its removal from the count under the mutex before exiting. A submission racing
with retirement either sees that worker as available or sees the decremented
count and creates a replacement. Overflow workers require no idle timeout,
timer, or retirement manager.

The pool may temporarily approach one native thread per outstanding blocking
operation. Growth is nevertheless bounded by live Tasks because a Task can
have at most one blocking job. The design prevents a fixed pool from creating
an IO dependency deadlock while preserving one pool, one synchronous API, and
no readiness backend. Native thread exhaustion can still delay queued work, but
it degrades to baseline-pool behavior instead of terminating the program.

Synchronization initialization and creation of every baseline thread occur
before user code. Partial baseline initialization never runs the program.

A fixed N-thread ceiling is rejected. It can strand a write, close, or other
operation behind N indefinitely blocked reads even when that later operation
would let the reads finish. Readiness polling remains a possible measured
socket optimization, not the general solution for synchronous files, standard
handles, or future foreign calls.

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
- Parking grants no new aliasing permission. Concurrent mutation, resize, or
  free of a List/View buffer used by an outstanding IO operation remains an
  unsynchronized conflict under the current contract and must be rejected when
  RFC 0118 can prove it. The pool does not make that existing race safe.

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

Each `hex_task` gains one nullable pending-park link and one C23 atomic park
phase. The link identifies the live wait/job state for dispatcher fast-path and
cleanup purposes; it carries no mutex identity. Every dispatcher and waker
coordinates the phase through that one atomic.

### Queue

- One program-wide FIFO queue is protected by one C23 `mtx_t`.
- Every baseline or overflow worker locks the queue first and waits on one C23
  `cnd_t` only while the queue is empty. A newly created overflow worker drains
  already-queued work before waiting; it does not depend on observing the
  submission's condition signal.
- Submission appends once and signals one worker.
- A worker removes one job, releases the queue mutex, invokes the entry, and
  completes the park/wake protocol.
- Native calls execute with neither the blocking mutex nor ready-queue mutex
  held.
- Queue length is bounded by live Tasks; it needs no separate allocation or
  capacity policy.

### One lost-wakeup-safe parking protocol

Every operation that suspends a Task uses one scheduler-owned park/commit/wake
protocol. Its users are `Task.yield()`, Task join, Channel send/receive, Mutex
lock, and blocking jobs. Do not retain independent check-then-switch sequences.

A wake may occur immediately. It must be safe before or after the fiber
switches back to its scheduler. Use these logical values in the Task's C23
atomic park phase:

```text
running   Task is executing and has no pending park
parking   Task registered its wait but its fiber has not committed its switch
parked    worker dispatcher has regained control from the fiber
notified  wake arrived before the dispatcher committed the park
ready     Task is published exactly once after it is no longer executing
```

Required common handshake:

1. Under the wait source's synchronization, the Task records `parking` and
   registers its wait. Channel and Mutex use their existing mutexes; join gains
   synchronization around target completion and joiner registration; a
   blocking job uses the blocking-queue mutex.
2. The Task releases that source synchronization and switches to its scheduler
   context. It never publishes itself to the ready queue.
3. The worker dispatcher sees the Task's non-null pending-park link after the
   switch. It uses compare-exchange to commit `parking` to `parked`.
4. A waker uses compare-exchange to change `parking` to `notified` without
   publishing the Task, or `parked` to `ready` followed by exactly one
   ready-queue publication.
5. A dispatcher that observes `notified` changes it to `ready` and publishes
   exactly once. Otherwise it leaves the Task parked.
6. The reactivated Task clears its pending-park state before returning to user
   code.

The waker's successful phase transition is a release operation. Dispatcher and
resumed-Task observations are acquire operations. A pool worker writes the job
result before its release transition; the resumed Task acquires that result
before reading it. Ready-queue mutex synchronization remains present but is not
the protocol's unstated visibility mechanism.

`Task.yield()` is the degenerate case: it has no wait source. The running Task
sets its pending link, records itself as already `notified`, and switches. The
dispatcher alone changes it to `ready` and publishes it after the switch.

Each Task control block has one lifecycle mutex protecting its completion
state, joiner slot, and join/detach terminal claim. Join holds the target's
lifecycle mutex while checking completion, recording its own `parking` phase,
and installing itself as joiner. No unsynchronized `state`/`joiner`
check-then-switch sequence remains. Spawn initializes this mutex before
publishing the Task; release destroys it after the fiber is reclaimable.

Private implementation states may differ, but a check-then-switch sequence that
can enqueue a still-running fiber is forbidden.

Each wait source's mutex, when required, precedes its atomic wake transition and
any ready-queue push. No path acquires a wait-source mutex while holding the
ready-queue mutex. The dispatcher never acquires a foreign wait-source mutex.

The dispatcher checks the pending-park link first. An ordinary switch-back with
no pending park performs no park-phase compare-exchange and acquires no parking
or blocking mutex.

### Completion and join reclamation

Task completion is a two-step scheduler transition:

1. On its fiber, the Task records `completing` under its lifecycle mutex and
   switches to its dispatcher. It does not publish a joiner and is not yet
   reclaimable.
2. After the switch returns, the dispatcher records `done`, extracts the
   registered joiner and terminal disposition, then releases the lifecycle
   mutex. It wakes the extracted joiner only after unlocking and performs no
   later access to the completed Task. A detached Task is reclaimed by that
   dispatcher after unlocking; a joined Task is reclaimable by its resumed
   joiner.

A join arriving during `completing` registers and parks. A join observing
`done` may return and reclaim because the completed fiber is no longer running.
No joiner can destroy a fiber stack while its completion switch still uses it
or destroy a lifecycle mutex still held by the dispatcher.

### Mutex handoff

Unlock retains direct ownership transfer to one waiter under the Mutex's
internal mutex. The wake record marks that ownership was transferred. After
resumption, that waiter returns successfully from `lock()` instead of
re-entering acquisition and treating its transferred ownership as a recursive
lock. A fresh lock attempt by the current owner still traps as recursive.

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
- Every Task-side print uses the pool even when a console write is likely to
  finish immediately. Each call therefore pays queue synchronization, two
  fiber scheduling transitions, and possibly overflow-worker creation.
  Uniform scheduler safety is preferred over allowing redirected stdout or
  stderr to block a scheduler worker. Reconsider a proven fast path only after
  RFC 0055 can execute and benchmark generated programs.

### Bytes

Bytes operations remain in-memory calls on the scheduler worker and never
enter the pool.

## Generated-component rules

- Add a `Blocking` demand fact to the program-wide concurrency render model.
- Add a scheduler-aware flag to the IO render model.
- `concurrency.h` declares the internal blocking entrypoint only when selected.
- `concurrency.c` emits the pool and handshake only when selected.
- `io.c` includes `concurrency.h` only in the combined variant.
- Each native operation has one synchronous private implementation shared by
  direct and task-aware frontends. Do not keep two native-call bodies merely to
  preserve old generated text.
- Task-only, Channel-only, Mutex-only, Atomic-only, IO-only, print-only, and
  Bytes-only programs emit no pool.
- A program combining scheduler reachability with native IO or print emits one
  pool inside the existing concurrency pair.
- Module headers acquire no blocking declarations.
- Repeated compilation produces byte-identical artifacts.

## Failure behavior

- Baseline pool initialization failure traps before user code.
- Lifecycle-mutex initialization failure follows the existing owner: root
  initialization traps; spawned Task creation fails through its existing Error
  path without publishing a partial Task.
- Submission allocates nothing and adds no recoverable Error path.
- Native failures retain existing Error or trap behavior.
- Queued work parks Tasks; it is not an Error. Unmet blocking demand creates an
  overflow worker rather than imposing a fixed saturation ceiling.
- Overflow-worker creation failure cancels only the worker reservation. The job
  remains queued for existing workers and no new Error or trap is added.
- A pool worker never adds retries beyond the existing operation contract.
- Root completion retains the existing process-lifetime rule and does not join
  detached Tasks. Process termination ends detached scheduler and pool threads.
- A detached Task blocked in a native operation retains its blocking worker
  until that operation returns or the process exits; cancellation is not added.

## Non-goals

- Readiness polling, IOCP dispatch, `epoll`, `kqueue`, `poll`, or `io_uring`.
- Async syntax, futures, promises, callbacks, executors, or an explicit IO
  context parameter.
- Timeouts, cancellation, priorities, work stealing, or configurable pool size.
- Making concurrent IO aliases safe; RFC 0118 owns rejection of provable
  unsynchronized buffer mutation.
- Scheduler integration for arbitrary foreign calls before RFC 0039.
- Timer or sleep parking. A later RFC may reuse the common parking protocol.

## Required sweep

- Route every selected native IO transfer and print write-all path through the
  task-aware internal frontend; no running-Task path may bypass it.
- Replace the early-publication and unsynchronized-join sequences in yield,
  join, Channel, and Mutex with the common park/commit/wake protocol.
- Add the Task's atomic park phase and nullable pending-park link to the shared
  runtime declaration; the resulting layout change affects every Task program
  and is expected manifest movement.
- Replace completion-side early joiner publication with dispatcher-finalized
  `completing` to `done` transition, so join cannot reclaim a live fiber stack.
- Preserve direct Mutex ownership handoff while making the resumed owner return
  successfully instead of re-entering the recursive-lock check.
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
5. Record the current early-ready publication in yield, Channel, and Mutex; the
   unsynchronized completion/joiner sequence; completion-side early joiner
   publication before the fiber switch finishes; and Mutex handoff re-entering
   the recursive-lock trap as defects being removed, not behavior preserved.

### Phase 1: demand discovery

1. Derive one blocking demand from scheduler selection combined with `readIO`,
   `writeIO`, `seekIO`, `closeIO`, or `printUsed`.
2. Add `Blocking` to typed concurrency and IO render models.
3. Keep it false for Atomic-only concurrency and Bytes-only stream use.
4. Add component tests for the full selection matrix before runtime emission.

### Phase 2: blocking runtime core

1. Add conditional declarations to `packages/concurrency.h`.
2. Replace yield, join, Channel, and Mutex suspension with the common
   park/commit/wake protocol before adding blocking jobs as its fifth user.
3. Add the Task atomic park phase and pending link; implement acquire/release
   compare-exchange transitions without a park mutex or wait-source identity in
   the pending link.
4. Add and lifecycle-manage the per-Task lifecycle mutex. Use it for completion
   state, joiner registration, and join/detach terminal claims; finalize
   completion in the dispatcher before waking a joiner or permitting
   reclamation.
5. Preserve Mutex handoff with an explicit resumed-with-ownership result.
6. Add queue, blocking workers, stack jobs, and direct fallback to
   `packages/concurrency.c`.
7. Initialize the baseline logical-worker count of blocking threads; add the
   mutex-protected demand calculation, overflow reservation/creation, and
   queue-empty overflow retirement rule.
8. Hook the worker dispatcher immediately after a Task switches back. It first
   checks the pending-park link and touches no parking mutex when that link is
   null.
9. Add text tests for FIFO behavior, lock order, no allocation, direct fallback,
   state transitions, and one ready push in every wake ordering and wait family.

### Phase 3: IO integration

1. Give `io.c` a typed render model and conditional concurrency include.
2. Extract each native-call portion once as a synchronous private entry used by
   both direct and task-aware paths. Accept the corresponding IO artifact
   changes rather than duplicating implementations.
3. Add typed stack contexts for read, write, seek, and close.
4. Preserve validation and result handling on the Task.
5. Assert every selected native path uses `hex_blocking_call` and no Bytes path
   does.

### Phase 4: print integration

1. Route the complete descriptor write-all loop through one blocking job.
2. Preserve formatting, retries, and final trap behavior.
3. Assert print-only output remains direct and pool-free, and Task-plus-print
   selects one pool.

### Phase 5: conformance and canonical docs

1. Implement every textual validation item below and no additional behavior.
2. Rebuild the snippet manifest once. Accept only concurrency artifacts changed
   by the parking correction and IO/print artifacts changed by the single-core
   extraction or task-aware path.
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

- IO-only and print-only programs emit no scheduler or pool and continue to use
  the direct synchronous frontend. Their artifacts may move only because the
  native operation is extracted once for both frontends.
- Task-only, Channel-only, Mutex-only, Atomic-only, Atomic-plus-IO, and
  Bytes-plus-Task programs emit no pool.
- Task-plus-IO and Task-plus-print emit exactly one pool in `concurrency.c` and
  one declaration owner in `concurrency.h`.
- Baseline pool size reuses scheduler logical worker count with minimum one.
- Generated submission code reserves one overflow slot only when queued jobs
  exceed idle plus already-reserved starting capacity.
- Generated code reads and changes worker, busy, starting, and queued counts
  only under the blocking mutex; it contains no hard-cap constant or `Project`
  setting.
- The generated overflow-retirement branch requires a completed job, empty
  queue, total workers above baseline, and count decrement under the mutex.
- The overflow-creation failure branch cancels the reservation, retains the
  queued job, signals existing workers, and emits no Error or trap.
- Jobs and typed contexts are stack values; submission contains no allocation.
- The queue is FIFO and protected by one mutex/condition pair.
- Native entries execute with neither the blocking-queue mutex nor the
  ready-queue mutex held.
- Completion-before-commit and commit-before-completion each contain exactly
  one ready-queue publication.
- No path publishes a parking Task from the submitting fiber.
- `hex_task` contains one C23 atomic park phase and one nullable pending-park
  link. Dispatcher and waker transitions use acquire/release compare-exchange;
  no park mutex or wait-source-mutex pointer is emitted.
- Yield, join, Channel send/receive, Mutex lock, and blocking jobs all use the
  common park/commit/wake protocol; none publishes a Task before its fiber has
  switched out.
- Each Task lifecycle mutex is initialized before publication, protects
  completion state, joiner registration, and join/detach terminal claims, and
  is destroyed only after the fiber is reclaimable.
- Completion changes through `completing`; only the dispatcher records `done`,
  extracts terminal state after switch-back, unlocks the lifecycle mutex, and
  then either wakes the joiner as its final target-related action or reclaims a
  detached Task.
- Mutex handoff records transferred ownership and the resumed waiter returns
  successfully without re-entering the recursive-lock rejection path.
- Dispatcher switch-back checks a nullable pending-park link before performing
  any park-phase compare-exchange and never acquires wait-source
  synchronization.
- A task-aware frontend falls back directly when `hex_current_task` is null.
- Capability failures and zero-length operations submit no job.
- IO read reserves on the Task, performs only native transfer in the pool, and
  updates length after wake.
- IO write, seek, close, and print preserve existing operation contracts.
- Bytes operations never reference blocking support.
- Every direct POSIX and Windows blocking call in IO/print belongs to one
  private synchronous entry used by the direct or task-aware frontend.
- No source signature, checked expression, result union, Error message, helper
  family, or public header changes beyond the internal concurrency declaration.
- Unaffected manifest entries remain byte-identical; changed entries are
  limited to concurrency programs affected by corrected parking and IO/print
  programs affected by native-core extraction or task-aware lowering.
- Repeated compilation produces byte-identical artifacts.
- Ordinary tests remain pure Go.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

### Runtime behavior retained as a coverage gap

These are required semantics but ordinary tests cannot execute generated C:

- With N scheduler workers blocked on N native operations, an unrelated CPU
  Task continues.
- More than N simultaneously blocked operations grow beyond the N baseline
  workers without consuming a scheduler worker or leaving later IO permanently
  behind the baseline jobs.
- Concurrent submissions create no more than one overflow worker per unit of
  unmet demand.
- Overflow workers retire after demand returns to the baseline, and a submit
  racing with retirement neither loses a job nor omits replacement capacity.
- Overflow creation failure leaves the job queued; an existing worker executes
  it after capacity becomes available.
- Immediate completion cannot republish a still-running fiber.
- Completion before and after park commit each wake exactly once.
- Immediate yield, join completion, Channel wake, and Mutex wake cannot
  republish a still-running fiber or lose a wake.
- A contended Mutex transfers ownership without trapping the selected waiter.
- A join cannot reclaim a completed Task's fiber until that fiber has switched
  back to its dispatcher.
- Results written by a pool thread are visible after wake.
- Baseline pool initialization failure traps before user code.
- Root completion terminates with blocked detached jobs under the existing
  process-lifetime rule.

Implementation records these exact cases under `docs/status.md` known coverage
gaps. RFC 0055 must make them executable when the driver can run generated
programs. Their temporary unverifiability does not authorize weaker text gates
or claims of runtime proof.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` to state:

- a synchronous native operation invoked by a running Task parks that Task and
  runs on the program-wide blocking pool;
- the pool starts with the scheduler's logical worker count, grows when queued
  jobs exceed available or reserved worker capacity, and retires overflow
  workers after demand falls;
- pool growth has no hard ceiling; failure to create an overflow worker leaves
  its operation queued for existing workers rather than adding an Error or
  trap;
- queued operations remain parked Tasks;
- calls outside a current Task remain direct;
- Bytes never uses the pool;
- IO and print source semantics and failures are unchanged; and
- a blocking operation does not satisfy the explicit `Task.yield()` rule.

Remove the superseded statement that descriptor operations may block a
scheduler worker. Verify that the corrected yield, join, Channel, and Mutex
implementations already satisfy their existing reference contracts; they add
no new language rule. No canonical documentation changes before implementation.
