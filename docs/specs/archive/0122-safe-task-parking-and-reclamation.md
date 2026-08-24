# RFC 0122: Safe Task Parking and Reclamation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented 2026-08-24. `hexal/concurrency.h`'s `hex_task`
  gained `_Atomic(uint8_t) park_phase`, `void *pending_park`, `uint8_t
  wake_result` (replacing `wake_error`), and `mtx_t lifecycle_mutex`; the
  superseded `uint8_t state` field and its four `HEX_TASK_READY/RUNNING/
  PARKED/DONE` constants were deleted entirely. `hexal/concurrency.c` gained
  three shared transition helpers (`hex_task_wake`, `hex_task_commit_park`,
  `hex_task_resume_commit`) implementing the specified acquire/release
  compare-exchange handshake, and every wait family was migrated onto them:
  `hex_task_yield` (source-less, release-stores `notified` directly since it
  has no wait-source mutex to register under), `hex_chan_send`/`receive`/
  `close`, `hex_mutex_lock`/`unlock`, and `hex_task_join`. Completion became
  the specified two-step transition: `hex_task_complete` (step one, on the
  completing task's own fiber) records `completing` under the lifecycle
  mutex with no mutex held across the switch; `hex_worker_loop`'s
  switch-back handler (step two) is now the sole place that tests the
  pending link to decide between `hex_task_commit_park` and completion
  finalization (record `done`, extract and wake the joiner after unlocking,
  then handle root shutdown or detached reclamation) — this is a real
  behavior fix, not a refactor: the old code's early ready-publication in
  yield, its check-then-park race in join (a completing target's own
  `hex_task_complete` could run between join's completion check and its
  joiner registration, losing the wake permanently), and its Mutex handoff
  incorrectly re-entering `hex_mutex_lock`'s acquisition loop and
  self-trapping the just-woken waiter as a recursive lock, are all closed by
  construction under the new protocol. `hex_task_detach` and the completion
  dispatcher now both read/write `flags` and `life` exclusively under the
  target's own `lifecycle_mutex`, so whichever of them observes the other's
  effect first is deterministically the one that releases, closing a
  detach/completion race the old code did not guard at all. One ordering
  defect was found and fixed during self-review before closure: the RFC's
  own normative order is pending-link write, then release-store `parking`,
  then wait-source registration; an initial implementation pass had these
  reversed (registering the task into `wait_send`/`wait_recv`/`wait_list`/
  `joiner` before writing `pending_park`), which a mutex-safety argument
  showed to be race-free in this specific codebase but which contradicted
  the RFC's exact text — corrected to match the specified order exactly in
  all four registration sites. `mtx_t lifecycle_mutex` needed
  `hexal/concurrency.h` itself to validate C23 thread support and include
  `<threads.h>` before defining `struct hex_task`, since the header (not
  only `concurrency.c`) is now included by any module header selecting Task/
  Channel/Mutex; the `__STDC_NO_THREADS__` check and its `#include` moved
  there from `concurrency.c` accordingly. `compiler/generator/
  concurrency_component_test.go` gained four new structural tests (exact
  field/helper cardinality, pending-link-before-parking-store ordering
  checked generically across every registration site by byte-offset search
  rather than brittle exact-indentation string matching, and the Mutex
  handoff's direct-return shape) alongside the untouched existing suite,
  which already fully covered selection/inclusion/prototype text and passed
  unchanged. The snippet manifest was rebuilt twice (once before the
  ordering fix, once after): the final diff touches exactly
  `hexal/concurrency.c`/`.h` in the 15 catalog snippets that select Task,
  Channel, or Mutex, confirmed by an explicit added/removed-line-only diff
  scope check; every Atomic-only, IO-only, and print-only entry is
  byte-identical. `docs/reference.md` required no edit: its existing Task/
  Channel/Mutex contracts (`join()` waits, copies the exact result, and
  reclaims storage; recursive lock is a programmer error that traps; wrong-
  owner/double unlock traps) already described the intended behavior this
  RFC's fix makes the implementation actually satisfy, and none of them
  described the old buggy behavior as correct. The runtime-only behaviors
  this RFC specifies (no fiber ever runs on two workers, no wake is lost,
  Channel close wakes every waiter exactly once even mid-switch, a
  contended Mutex handoff never traps, a join cannot reclaim a
  still-switching fiber, and each of `completing`/`done`/detach/root
  shutdown uses its defined destruction owner) are recorded under
  `docs/status.md`'s known coverage gaps pending RFC 0055's ability to
  execute generated C; RFC 0121, whose blocking pool depended on this RFC's
  parking primitive, was moved from blocked to implementation-ready in the
  same status update.
- Created: 2026-08-24
- Updated: 2026-08-24
- Scope: one lost-wakeup-safe Task parking protocol, synchronized completion,
  safe reclamation, and correct Mutex ownership handoff
- Depends on: the implemented M:N Task runtime and `docs/reference.md`
- Coordinates with: RFC 0055 (runnable generated-C validation), RFC 0118
  (concurrency safety), and RFC 0121 (scheduler-aware blocking pool)
- Accepted runtime cost: one new `mtx_t`, one atomic phase, and one opaque
  pending pointer per scheduler Task; existing `wake_error` storage is
  generalized and `wait_next` is retained. At 10,000 live Tasks the additional
  control state is target-dependent and may total hundreds of KiB; correctness
  justifies that cost.
- Changes no Hexal grammar, type, function signature, or result contract

## Summary

Replace the independent yield, join, Channel, and Mutex check-then-switch paths
with one scheduler-owned park/commit/wake protocol. A Task becomes ready only
after its fiber has stopped executing, every wait is completed exactly once,
and completion cannot make a fiber reclaimable while its final switch still
uses its stack.

Preserve direct Mutex ownership transfer, but make the selected waiter consume
that transfer instead of re-entering the recursive-lock rejection path.

This RFC changes runtime correctness and generated C only. It adds no source
surface and is the required parking foundation for RFC 0121 and future timers.

## Motivation

The current runtime has four related defects:

- `Task.yield()` pushes its Task to the ready queue before switching out, so a
  second scheduler worker can run the same fiber concurrently.
- Channel and Mutex waits expose a still-running Task to a waker before its
  fiber has committed the switch, allowing early republish or a lost wake.
- Task join checks completion and installs the joiner without one synchronized
  decision, and completion exposes the joiner before its fiber switch is
  finished. A wake can be lost or a joiner can reclaim a live stack.
- Mutex unlock transfers ownership to a selected waiter, but that waiter
  re-enters acquisition and reports its transferred ownership as a recursive
  lock.

All four failures come from duplicated suspension protocols. Fixing one path
without establishing a common invariant leaves the same bug class in the
others and gives RFC 0121 no safe primitive on which to park blocking calls.

## Decision

### Task parking state

Each Task control block gains:

- one nullable pending-park link; and
- one C23 atomic parking phase.

The pending link identifies the current live wait record for dispatcher
fast-path and cleanup purposes. It carries no mutex identity. The phase has
these logical values:

```text
running   Task is executing and has no pending park
parking   Task registered its wait but has not committed its fiber switch
parked    dispatcher regained control after the switch
notified  wake arrived before the dispatcher committed the park
ready     Task is published exactly once and may resume
```

Private spellings may differ. The state transitions and ordering below do not.

### Wait representation

The pending link is one opaque `void *pending_park`. The dispatcher tests it for
null but never dereferences it or infers a wait family from its value. Each wait
source continues to own and interpret its typed storage:

| Wait family | `pending_park` value | Source-owned storage |
|---|---|---|
| Yield | the current Task pointer as a non-null sentinel | none |
| Channel | the current Task pointer | existing intrusive `wait_next` list |
| Mutex | the current Task pointer | existing intrusive `wait_next` list |
| Join | the target Task pointer | target's existing `joiner` slot |
| RFC 0121 blocking job | address of the stack `hex_blocking_job` | job `next` and typed context |

`wait_next` therefore survives for Channel and Mutex. Replace `wake_error` with
one private `wake_result` byte shared by mutually exclusive waits. Its values
distinguish normal wake, Channel-close wake, and transferred Mutex ownership.
The waiting operation resets it before registration and consumes it after
resumption before any re-park. Blocking jobs keep their result in their typed
stack context instead; join and yield need no wake payload.

The opaque link exists only to distinguish an ordinary scheduler return from a
pending park and to keep the live registration associated with that Task. It is
not a new allocation, tagged union, general callback record, or owner of the
source's wait-list state.

### Common park/commit/wake protocol

Every suspending operation follows this handshake:

1. Under the wait source's mutex, write the pending link, release-store
   `parking`, and register the Task with that source, in that order.
2. Release the source mutex and switch to the scheduler context. The suspending
   fiber never publishes itself to the ready queue.
3. After switch-back, the dispatcher reads the pending link and acquire-loads
   the phase. It compare-exchanges `parking` to `parked`.
4. A waker compare-exchanges:
   - `parking` to `notified`, without publication; or
   - `parked` to `ready`, followed by exactly one ready-queue publication.
5. A dispatcher observing `notified` changes it to `ready` and publishes once.
   A dispatcher observing `parked` leaves the Task suspended.
6. On resumption, the Task acquire-transitions `ready` to `running` and clears
   its pending link before reading wake results, rechecking its condition,
   registering another wait, or returning to user code.

The successful waker transition is a release operation. Dispatcher and resumed
Task observations are acquire operations. Ready-queue mutex synchronization is
retained but is not an unstated substitute for the phase ordering.

Every waker writes its payload before the release phase transition: Channel
close writes `wake_result`, Mutex unlock writes transferred ownership, and a
blocking worker writes its typed context result. The resumed Task reads that
payload only after acquiring `ready`. A correct atomic transition with the
payload store on the wrong side of it is invalid.

A failed waker compare-exchange re-reads the phase and applies the transition
for the observed value:

- `parking` becomes `notified`;
- `parked` becomes `ready` and is published;
- `notified` means the early wake is already recorded; and
- `ready` or `running` means another waker already completed this wait, so the
  stale waker does nothing.

It never assumes that failure means `parked` and never publishes twice.

### Wait-source serialization

Channel, Mutex, and future blocking-job waits serialize registration, dequeue,
wake decision, and phase transition under their owning wait-source mutex. This
prevents a stale waker from applying to a later park of the same Task. A future
lock-free source must preserve an equivalent single-wake and ABA-exclusion
proof.

A source waking multiple Tasks, including Channel close, applies the transition
separately to each dequeued Task. It publishes only a Task successfully changed
from `parked` to `ready`; a `parking` Task becomes `notified` for its dispatcher
to publish.

Join is the one transition-after-unlock exception. The target lifecycle mutex
serializes completion checking, joiner registration, and extraction. Extracting
the joiner makes the completion dispatcher its unique waker, and the unpublished
joiner cannot re-park before that wake. The dispatcher therefore unlocks the
target lifecycle mutex before transitioning the extracted joiner.

No path acquires a wait-source mutex while holding the ready-queue mutex. The
dispatcher never acquires a foreign wait-source mutex.

### Yield

`Task.yield()` is the source-less form. The running Task writes its pending
link, release-stores `notified`, and switches. The dispatcher alone acquires
that phase, changes it to `ready`, and publishes after the switch.

Yield retains its source-language semantics. This protocol is not a new visible
yield point and does not change the explicit-yield rule for literal
`while true` loops.

### Dispatcher rules

The dispatcher checks the nullable pending link first. An ordinary switch-back
with no pending park performs no park-phase compare-exchange and acquires no
wait-source mutex.

Before publishing a Task, the dispatcher computes every disposition it still
needs. The ready-queue push is its final access to that Task because another
worker may resume the Task immediately.

For an ordinary parked Task, no dispatcher access follows publication. The root
Task is the sole exception: after recording root completion, the scheduler may
switch back to the process-owned root fiber once so generated `main` can return.
That shutdown switch is not a ready publication or reclamation path.

### Completion, join, and reclamation

Each Task control block has one lifecycle mutex protecting:

- completion state;
- the single joiner slot; and
- the join/detach terminal claim.

Spawn initializes the mutex before publishing the Task. Task completion is a
two-step scheduler transition:

1. On its fiber, the Task locks its lifecycle mutex, records `completing`,
   unlocks, and switches to its dispatcher. It publishes no joiner and is not
   reclaimable. No C23 mutex remains held across the switch.
2. After switch-back, the dispatcher locks the lifecycle mutex, records `done`,
   extracts the joiner and terminal disposition, and unlocks. It then wakes the
   extracted joiner or reclaims a detached Task.

A join arriving during `completing` registers and parks. A join observing
`done` may return and reclaim because the target fiber is no longer running.
The resumed joiner destroys a joined Task. The completion dispatcher destroys
a detached Task. The process owns the root Task until exit. No other path
destroys the lifecycle mutex or fiber storage.

The joiner wake is the completion dispatcher's final target-related action.
The root shutdown switch described above is the only permitted Task access
after root finalization.

### Mutex ownership handoff

Unlock retains direct ownership transfer to one waiter under the Mutex's
internal mutex. The wait record marks that ownership was transferred. After
resumption, the selected waiter returns successfully from `lock()` instead of
re-entering acquisition. A fresh lock attempt by the current owner still traps
as recursive.

## Generated-component rules

- The Task phase, pending link, lifecycle mutex, and common transition helpers
  belong to `hexal/concurrency.h` and `hexal/concurrency.c`.
- Support is selected by Task, Channel, or Mutex scheduler demand; it does not
  require IO, print, or RFC 0121's blocking pool.
- Atomic-only programs select no Task parking runtime.
- No module header owns scheduler-private state.
- No new generated component pair or public ABI is introduced.
- Repeated compilation produces byte-identical artifacts.

## Failure behavior

- Lifecycle-mutex initialization failure follows the existing owner: root
  initialization traps; spawned Task creation returns its existing Error and
  never publishes a partial Task.
- The protocol allocates no parking record and adds no recoverable Error path.
- Existing Channel, Mutex, join, detach, and Task result failures remain exact.
- Private impossible phase transitions fail closed through the existing runtime
  trap convention; they are compiler/runtime defects, not user errors.

## Non-goals

- Blocking-operation execution, native thread pools, readiness, timers, sleep,
  cancellation, timeout, asynchronous fiber interruption, or scheduler
  preemption.
- A source-language memory-order surface or configurable scheduler policy.
- More than one joiner or a change to Task handle semantics.
- Lock-free wait-source queues.
- Concurrency ownership and alias checking; RFC 0118 owns that language work.

## Required sweep

- Replace every early-ready publication in yield, Channel, and Mutex with the
  common protocol.
- Replace join's unsynchronized completion/joiner check and completion-side
  early publication with the lifecycle transition.
- Remove every independent check-then-switch sequence that can expose a running
  fiber to a waker.
- Route Channel close through one transition per dequeued waiter.
- Preserve direct Mutex handoff while consuming its transferred-ownership fact
  after resumption.
- Add the Task phase, pending link, and lifecycle mutex once in the shared Task
  declaration; do not retain superseded fields or duplicate protocols.
- Preserve intrusive `wait_next`, replace `wake_error` with the specified
  `wake_result`, and add no heap or stack wait record for Channel or Mutex.
- Keep runtime C in `compiler/generator/packages/*.c` and `*.h`, not Go strings.
- Preserve demand-driven emission and byte-identical unaffected artifacts.

## Implementation plan

### Implementation map

| Area | Required work |
|---|---|
| `compiler/generator/packages/concurrency.h` | Add phase, `pending_park`, `wake_result`, and lifecycle mutex; retain `wait_next`; define private phase/wake constants and transition declarations. |
| `compiler/generator/packages/concurrency.c` | Implement common transition helpers, dispatcher commit, yield, Channel and Mutex migration, synchronized join/completion, root exception, handoff consumption, initialization, and destruction. |
| `compiler/generator/concurrency_component.go` | Carry the typed demand/render facts required by the revised shared Task layout without introducing IO demand. |
| `compiler/generator/concurrency_component_test.go` | Assert exact layout ownership, selection, transition ordering, absence of duplicate protocols, and unchanged Atomic-only output. |
| `compiler/tests/integration/concurrency_test.go` | Compile focused yield, join, Channel-close/re-park, and contended-Mutex programs and assert the required generated-C structure. |
| snippet manifest | Record only Task/Channel/Mutex artifact movement; preserve IO-only and print-only hashes. |

### Phase 0: baseline and defect inventory

1. Record the green test/vet baseline and snippet manifest.
2. Record Task-only, Channel-only, Mutex-only, Atomic-only, Task-plus-IO, and
   Task-plus-print artifacts before editing.
3. Probe and quote the current yield early publication, Channel and Mutex
   check-then-switch paths, joiner race, completion-before-switch publication,
   and Mutex handoff re-entry.
4. Inventory Task construction, publication, switch-back, terminal claims, and
   destruction paths on both generated platforms.

### Phase 1: Task state and transition helpers

1. Add the atomic phase, nullable pending link, and lifecycle mutex to the Task
   declaration and initialize them before publication.
2. Retain `wait_next`, replace `wake_error` with `wake_result`, and implement
   the family mapping in the Wait representation table without new records.
3. Implement common suspend, dispatcher-commit, wake, resume, and multi-wake
   operations with the specified acquire/release ordering.
4. Make failed compare-exchange total for every observable phase and add no
   implicit fallback publication.
5. Enforce pending-link write, release phase store, and source registration in
   that order under the source mutex.
6. Require every wake payload store before its release transition and every
   payload read after the resumed acquire.
7. Make ready publication the dispatcher's final ordinary access; encode the
   root shutdown exception separately.

### Phase 2: migrate existing wait families

1. Convert `Task.yield()` to the source-less notified path.
2. Convert Channel send, receive, and close, including condition recheck and
   re-park without returning to user code.
3. Convert Mutex lock and unlock, retaining explicit ownership transfer.
4. Remove all superseded early-publication and local wake paths.

### Phase 3: completion and join

1. Initialize and destroy the lifecycle mutex at the defined ownership sites.
2. Implement `running -> completing -> done`, with no mutex held across a fiber
   switch.
3. Serialize joiner installation and terminal claims under the target mutex.
4. Extract the joiner under the mutex, wake it after unlock as unique waker, and
   make destruction ownership explicit for joined, detached, and root Tasks.

### Phase 4: conformance and documentation

1. Implement every validation item below and no additional behavior.
2. Rebuild the snippet manifest once. Accept movement only in artifacts that
   select Task, Channel, or Mutex runtime state; IO-only and print-only entries
   remain byte-identical.
3. Verify `docs/reference.md` already states the preserved yield, join, Channel,
   Mutex, and Task-lifetime contracts. Edit it only if implementation exposes a
   genuine mismatch; this RFC introduces no new language rule.
4. Record the runtime-only cases below under `docs/status.md` known coverage
   gaps until RFC 0055 can execute them.
5. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
6. Rebuild and restart the workbench.
7. Remove this RFC's TODO and bug entries, mark it implemented, and archive it
   only after code, text tests, artifacts, and canonical docs agree.

## Validation

This section is exhaustive.

### Compiler and generated-text validation

- `hex_task` contains one C23 atomic phase, one nullable pending link, and one
  lifecycle mutex; no parking mutex or wait-source-mutex pointer is emitted.
- `pending_park` is opaque to the dispatcher. Yield uses the Task sentinel,
  Channel and Mutex retain `wait_next`, join points at its target, and RFC 0121
  blocking points at its stack job.
- `wake_error` is absent. One `wake_result` carries Channel close or Mutex
  ownership transfer, is reset before registration, and is consumed before
  another park.
- Pending-link write precedes release-storing `parking`, which precedes wait
  registration under the source mutex.
- Dispatcher and waker transitions use acquire/release compare-exchange. Failed
  waker compare-exchange re-reads and handles `parking`, `parked`, `notified`,
  `ready`, and `running` without duplicate publication.
- Yield writes its pending link before release-storing `notified` and never
  publishes from the yielding fiber.
- Resumption acquire-transitions `ready` to `running` and clears the pending
  link before result access, condition recheck, or another park.
- Channel and Mutex serialize registration, dequeue, wake decision, and phase
  transition under their source mutexes.
- Channel close applies the transition once per waiter and never publishes a
  waiter left in `notified`.
- Channel close, Mutex handoff, and blocking-job completion write their payload
  before the release transition; resumed code reads it only after acquire.
- Join extracts its unique joiner under the lifecycle mutex, unlocks before the
  transition, and contains no competing wake path for that registration.
- Yield, join, Channel send/receive, and Mutex lock use the common protocol;
  none publishes a Task before its fiber has switched out.
- Dispatcher switch-back checks the pending link before any phase operation and
  acquires no foreign wait-source mutex.
- An ordinary dispatcher ready publication is its final Task access. The only
  generated post-finalization Task access is the root shutdown switch-back.
- Lifecycle mutex initialization precedes Task publication and destruction
  follows fiber reclamation.
- Completion changes through `completing`; only the dispatcher records `done`,
  and no lifecycle mutex is held across the switch.
- Joined Tasks are destroyed by their resumed joiner, detached Tasks by their
  completion dispatcher, and root storage remains process-owned.
- Mutex handoff records transferred ownership and the resumed waiter returns
  without re-entering recursive-lock rejection.
- Atomic-only, IO-only, and print-only artifacts remain byte-identical. Changed
  manifest entries are limited to programs selecting Task, Channel, or Mutex.
- Repeated compilation produces byte-identical artifacts.
- Ordinary tests remain pure Go.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

### Runtime behavior retained as a coverage gap

Ordinary tests cannot execute generated C, but these semantics remain required:

- Immediate yield, join completion, Channel wake, and Mutex wake never run one
  fiber on two workers and never lose a wake.
- Completion before and after dispatcher park commit each publish exactly once.
- Channel close wakes every registered waiter exactly once, including waiters
  whose fiber switches are not yet committed.
- A resumed Channel operation can recheck and re-park without stale-waker ABA.
- A contended Mutex transfers ownership without trapping its selected waiter.
- A join cannot reclaim the target fiber before its completion switch returns
  to the dispatcher.
- Join during `completing`, join after `done`, detach completion, and root
  shutdown each use their defined destruction owner.

Implementation records these cases under `docs/status.md` known coverage gaps.
RFC 0055 must make them executable when the driver can run generated programs.
Their temporary unverifiability does not permit weaker text gates or claims of
runtime proof.

## Reference synchronization

After implementation, verify that `docs/reference.md` already expresses the
preserved Task yield, join, Channel, Mutex, completion, and process-lifetime
contracts. Add no scheduler implementation narrative to the reference. If the
corrected runtime disagrees with a current semantic rule, resolve that conflict
before closing this RFC; otherwise record that no reference edit was required.
