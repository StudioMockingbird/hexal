# RFC 0132: Root Task Scheduler Bootstrap

- Kind: Architecture Decision Record (ADR)
- Status: Implementation-ready; implementation not started
- Created: 2026-08-27
- Updated: 2026-09-07
- Scope: scheduler startup, the root Task's first fiber switch, and runtime
  execution coverage
- Coordinates with: the implemented M:N scheduler and blocking pool described
  by `docs/reference.md`, RFC 0118 (concurrency safety), and the external C23
  suite in `compiler/tests/c23validation/`
- Does not change: Hexal syntax, Task/Channel/Mutex APIs, worker count, parking
  phases, wake ordering, or blocking-pool policy

## Decision

Scheduler initialization returns to generated `main` so the root Task executes
the program's source statements immediately on the initial process thread.

The root Task is not placed on the ready queue during initialization. Once it
parks, it is published in the existing FIFO like every other ready Task, but
only worker zero may remove it. Non-zero workers skip root while retaining it
in queue order and may remove a later eligible Task. Root publication broadcasts
the ready condition so worker zero necessarily wakes. This preserves FIFO
fairness on a one-worker target: an ordinary Task queued before a yielding root
runs before root resumes.

Root's first later switch into worker zero is committed by a dedicated
worker-zero bootstrap path using the same post-switch transition logic as every
later dispatch.

## Defect

Current `hex_scheduler_init`:

1. creates the root Task and worker-zero dispatcher fiber;
2. creates other worker threads;
3. switches immediately from the root fiber to worker zero;
4. finds an empty ready queue and waits forever.

No root statement has run, so nothing can spawn a Task or signal the queue.
Every program selecting scheduler initialization hangs before its Hexal body.

The defect is architectural rather than a local code-generation typo: if init
merely returns, the root's first `yield`, wait, or completion enters a dispatcher
that has never dispatched the root and therefore has no post-switch `task` to
commit.

## Root lifecycle

1. `hex_scheduler_init` initializes scheduler state, converts the initial
   thread to the root fiber, creates worker zero and the remaining workers, sets
   `hex_current_task`, and returns.
2. Generated root statements run normally.
3. A root yield or wait writes its pending link and phase under the existing
   protocol, then switches to worker zero.
4. Worker zero's bootstrap receives the root Task explicitly and applies the
   shared post-switch commit operation:
   - a non-null pending link commits the root's park;
   - a null pending link with lifecycle `completing` commits root completion.
   A straight-line root that never parks reaches this second branch on the
   bootstrap's first entry when generated `main` calls `hex_task_complete`.
5. A commit or wake that makes root ready appends it to the shared FIFO and
   broadcasts the ready condition.
6. Worker zero then enters the ordinary dispatch loop and consumes the FIFO in
   order. Non-zero workers skip root without removing or reordering it and may
   select a later eligible Task.
7. Once root is selected, subsequent switches use the ordinary dispatcher's
   post-switch path; only ready publication and selection retain root affinity.
8. Root completion records shutdown, broadcasts the ready condition, and lets
   worker zero switch back to the root fiber so generated `main` returns.

## Invariants

1. User root statements begin before any root park or completion.
2. The root Task is never concurrently running and present on the ready queue,
   and exactly-once publication permits at most one queued root node.
3. The root's first park uses the same pending-link, phase, release/acquire, and
   exactly-once publication rules as later parks.
4. The root's first completion uses the same lifecycle commit as later root
   completion; no synthetic ready-queue round trip is required.
5. Worker zero is the only dispatcher that selects or switches directly to the
   initial-process-thread root fiber.
6. Other workers may run spawned Tasks in parallel while root is running or
   parked, but never run root.
7. Initialization failure traps before user statements. Successful
   initialization never blocks merely because the ready queue is empty.
8. Root shutdown requests dispatcher shutdown, broadcasts to all dispatcher
   workers, and returns to generated `main` exactly once. It does not join
   detached Tasks or native worker threads; remaining Tasks are abandoned to
   process termination under the existing contract.
9. The existing Task park/commit/wake protocol, blocking pool, and task result
   ownership remain unchanged outside the new first-switch bootstrap.

## C runtime structure

- Extract the existing code after a dispatched fiber switches back into one
  helper that commits either a park or completion for a known Task.
- Keep every ready publication in one helper. Under the ready mutex, it appends
  to the existing FIFO; it signals one worker for an ordinary Task and
  broadcasts for root so worker zero necessarily wakes.
- The publication helper is the only ready-queue writer. Its callers are
  spawn, the notified-yield dispatcher commit, the ordinary parked-task
  dispatcher commit, and the parked branch of the common wake operation used
  by join, Channel, Mutex, and the blocking pool. Spawn always publishes a
  non-root Task and therefore uses signal-one.
- Make ready selection worker-aware. Worker zero removes the FIFO head.
  Non-zero workers remove the first non-root Task, skipping root in place. A
  non-zero worker waits when the queue contains no eligible non-root Task even
  if root is present.
- Each worker retains the spurious-wakeup loop. While holding the ready mutex
  continuously, worker zero waits while the FIFO is empty; a non-zero worker
  waits while the FIFO contains no non-root Task. Predicate evaluation,
  selection, removal, and head/tail/link repair occur under that same mutex
  hold.
- Removing the first eligible non-root Task preserves queue integrity when
  root is at the head, middle, or tail. Removing the final node clears both
  head and tail; removing a middle or tail node repairs the predecessor and
  tail without changing root's position.
- The ordinary worker loop calls that helper after every dispatched Task
  returns control.
- Add `hex_worker_zero_bootstrap(void *raw_root)`. It casts `raw_root` to
  `hex_task *`, commits root's first switch exactly once through the shared
  post-switch helper, and enters `hex_worker_loop((void *)1)` on the same fiber
  and stack. It is the entry passed to
  `hex_context_create(hex_worker_zero_bootstrap, hex_root_task)`; no second
  worker-zero context exists. The non-null argument passed onward retains the
  existing `is_worker_zero` convention. When the ordinary loop later selects
  root, it refreshes `root->scheduler_fiber` from that loop's current context
  exactly as it does for every dispatched Task.
- Remove the final context switch from successful `hex_scheduler_init`.
- Do not invent a separate root queue, state machine, or scheduling phase.

## Required sweep

- Update comments that claim `hex_scheduler_init` starts dispatch before
  returning.
- Remove compile-only treatment from concurrency fixtures only after their
  runtime expectations are defined and passing.
- Remove the stale `Unowned` and known-startup-hang wording from
  `c23_harness_test.go` and `fixtures_test.go`; timeout failures become generic
  external-process failures.
- Search for every root-only branch in completion, shutdown, release, join, and
  worker-zero handling; keep one terminal owner for each resource.
- Retain the ten-second external process timeout as a general test-harness
  safety boundary, not as expected scheduler behavior.
- Add no polling loop, startup sleep, extra root thread, or ready-queue sentinel.

## Accepted costs

- A non-zero worker intentionally remains idle when root is the only queued
  Task; root affinity reserves that work for worker zero.
- Publishing root broadcasts and may wake workers that immediately wait again.
  Ordinary Tasks retain signal-one because every worker may run them; root
  requires broadcast because only worker zero is eligible. Both predicates
  remain spurious-wakeup-safe loops.
- The external suite currently has no trustworthy thread-race sanitizer tier
  for the fiber scheduler. Component assertions and repeated exact-output
  fixtures cover this change, but they are not equivalent to race-detector
  evidence.

## Detailed implementation plan

### Phase 0: reproduce and freeze

1. Compile a minimal program that selects scheduler initialization and prints
   before and after one `Task.yield()`.
2. Run it under the external harness timeout and record the current timeout.
3. Record generated scheduler text, ordinary Go test/vet state, and snippet
   manifest before runtime edits.

### Phase 1: factor the common commit

1. Extract the dispatcher's post-switch pending-link/completion branch into a
   helper taking the known Task.
2. Preserve the existing mutex, atomic ordering, ready publication, joiner,
   detach, reclamation, and root-shutdown operations byte-for-byte where
   possible.
3. Add component tests proving ordinary workers call the shared helper.

### Phase 2: enforce root affinity

1. Pass worker identity into ready selection and its wait predicate.
2. Implement the exact wait predicates under the ready mutex: worker zero
   waits while the FIFO is empty; a non-zero worker waits while no non-root
   Task is present. Retain the condition wait in a loop.
3. Keep worker zero's selection as FIFO-head removal. Make non-zero selection
   skip root in place and remove the first eligible non-root Task. Cover root
   at head, middle, and tail; removal of the only, middle, and tail eligible
   Task; and correct clearing or repair of head, tail, predecessor, and
   `ready_next`.
4. Broadcast when root becomes ready and retain one-worker signalling for an
   ordinary Task.
5. Add component assertions that only worker zero may select root, non-zero
   selection leaves root queued, and every ready publication uses the shared
   helper.
6. Assert structurally that worker zero removes the FIFO head rather than
   searching for root. This makes a child queued before a yielding root run
   first on a one-worker target without introducing a worker-count test seam.

### Phase 3: return from initialization

1. Add `hex_worker_zero_bootstrap(void *raw_root)` with the exact one-shot cast,
   commit, and same-context handoff described under C runtime structure.
2. Create worker zero with that entry and `hex_root_task` as its argument; do
   not create another context or stack.
3. Remove initialization's eager root-to-dispatcher switch and return to
   generated `main`.
4. On the root's first later switch, commit it once and enter normal dispatch.
5. Add text assertions that initialization contains no context switch and no
   root ready publication.

### Phase 4: runtime lifecycle fixtures

1. Add an exact-output root-yield fixture.
2. Run spawn/join, Channel send/receive/close, and Mutex contention fixtures.
   Exercise each root wait path repeatedly; functional output plus the Phase 2
   structural assertions establish affinity without a test-only runtime API.
3. Add root completion with no spawned Task, root completion after joined
   Tasks, and root completion with detached CPU Tasks. A Task blocked in a
   native operation remains the blocking-pool lifecycle coverage gap and is not
   claimed here. The no-child case must remain straight-line before
   `hex_task_complete`; it specifically verifies that the bootstrap's first
   entry can commit completion without any preceding park.
4. Bound every process with the existing timeout; a timeout is a failure.
5. Promote the three existing compile-only concurrency fixtures to runtime
   fixtures. Rename them to remove `-compiles` and require exact stdout `6`,
   `85`, and `200`, respectively.

### Phase 5: conformance and docs

1. Run all Validation items under GCC, Clang, and Zig where supported.
2. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
3. Regenerate the snippet manifest once. Existing hashes for snippets that do
   not select `hexal/concurrency.c` remain unchanged; review every changed
   concurrency artifact as an intended runtime consequence.
4. Update `docs/reference.md` only if its scheduler startup/lifecycle contract
   needs a precise root-bootstrap rule; request explicit approval before that
   canonical edit.
5. Remove RFC 0132's status row only after runtime execution is green.

## Validation

This section is exhaustive.

- Successful `hex_scheduler_init` returns without switching fibers, waiting on
  the ready condition, or publishing root.
- A minimal concurrency-selected program executes its first root statement.
- Root `Task.yield()` resumes exactly once and statements after it execute.
- Root Channel wait and Mutex wait park and resume without a lost wake.
- Spawned CPU Tasks run on scheduler workers and `join()` returns their exact
  results.
- Channel send/receive/close and Mutex contention fixtures complete with exact
  output under the existing timeout.
- Root completion with no child, joined children, and detached CPU children
  requests shutdown, broadcasts, and returns from `main` exactly once without
  joining detached Tasks or worker threads.
- Root is published in FIFO order after parking; non-zero workers cannot select
  or reorder it; worker zero removes the FIFO head; and every ready transition
  publishes through the shared helper.
- Worker wait predicates loop under the ready mutex: worker zero waits only for
  a non-empty FIFO, while non-zero workers wait for an eligible non-root Task.
  Predicate evaluation, selection, removal, and link repair share one mutex
  hold.
- Non-zero selection preserves head, tail, predecessor, and `ready_next` for
  root at the FIFO head, middle, and tail, including removal of the final
  eligible node.
- Worker zero is created once with `hex_worker_zero_bootstrap` and root as its
  argument. The bootstrap commits exactly once and enters the ordinary loop on
  that same context and stack; later root dispatch refreshes
  `root->scheduler_fiber` through the ordinary bookkeeping path.
- Worker zero removes the FIFO head rather than searching for root, so root
  affinity cannot turn `Task.yield()` into an immediate self-resume when an
  ordinary Task was queued first.
- Repeated root yield, Channel-wait, Mutex-wait, and join fixtures complete with
  exact output under the timeout; the first root park is published at most
  once.
- No startup sleep, polling, extra root thread, or new scheduling phase exists.
- Existing component assertions for park/commit/wake ordering remain true.
- The full external concurrency fixture set compiles and runs under every
  configured supported toolchain.
- Existing snippet-manifest hashes outside programs selecting
  `hexal/concurrency.c` do not change.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

After behavior stabilizes, review the Task runtime contract in
`docs/reference.md`. If synchronization is required, record only the precise
root-startup and first-switch semantics; do not copy this implementation plan
or add tutorial examples.
