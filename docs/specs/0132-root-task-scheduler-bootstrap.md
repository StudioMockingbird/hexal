# RFC 0132: Root Task Scheduler Bootstrap

- Kind: Architecture Decision Record (ADR)
- Status: Implementation-ready; implementation not started
- Created: 2026-08-27
- Updated: 2026-08-27
- Scope: scheduler startup, the root Task's first fiber switch, and runtime
  execution coverage
- Coordinates with: the implemented M:N scheduler and blocking pool described
  by `docs/reference.md`, RFC 0118 (concurrency safety), and RFC 0125 (external
  C23 validation)
- Does not change: Hexal syntax, Task/Channel/Mutex APIs, worker count, parking
  phases, wake ordering, or blocking-pool policy

## Decision

Scheduler initialization returns to generated `main` so the root Task executes
the program's source statements immediately on the initial process thread.

The root Task is not placed on the ready queue during initialization. Its first
later switch into worker zero is committed by a dedicated worker-zero bootstrap
path using the same post-switch transition logic as every later dispatch.

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
5. Worker zero then enters the ordinary dispatch loop.
6. Once the root is published and later selected, subsequent switches use the
   ordinary dispatcher's post-switch path exactly like any other Task.
7. Root completion records shutdown, broadcasts the ready condition, and lets
   worker zero switch back to the root fiber so generated `main` returns.

## Invariants

1. User root statements begin before any root park or completion.
2. The root Task is never concurrently running and present on the ready queue.
3. The root's first park uses the same pending-link, phase, release/acquire, and
   exactly-once publication rules as later parks.
4. The root's first completion uses the same lifecycle commit as later root
   completion; no synthetic ready-queue round trip is required.
5. Worker zero is the only dispatcher that switches directly to the initial
   process-thread root fiber.
6. Other workers may run spawned Tasks in parallel while root is running or
   parked, but never run the root before it has been published.
7. Initialization failure traps before user statements. Successful
   initialization never blocks merely because the ready queue is empty.
8. Root shutdown wakes all dispatcher workers and returns to generated `main`
   exactly once.
9. The existing Task park/commit/wake protocol, blocking pool, and task result
   ownership remain unchanged outside the new first-switch bootstrap.

## C runtime structure

- Extract the existing code after a dispatched fiber switches back into one
  helper that commits either a park or completion for a known Task.
- The ordinary worker loop calls that helper after every dispatched Task
  returns control.
- Add a worker-zero entry that receives `hex_root_task`, commits the root's
  first switch through that helper, then enters the ordinary worker loop.
- Create worker zero with that entry instead of entering the ordinary loop
  directly.
- Remove the final context switch from successful `hex_scheduler_init`.
- Do not enqueue root during initialization and do not invent a separate root
  state machine.

## Required sweep

- Update comments that claim `hex_scheduler_init` starts dispatch before
  returning.
- Remove compile-only treatment from concurrency fixtures only after their
  runtime expectations are defined and passing.
- Search for every root-only branch in completion, shutdown, release, join, and
  worker-zero handling; keep one terminal owner for each resource.
- Retain the ten-second external process timeout as a general test-harness
  safety boundary, not as expected scheduler behavior.
- Add no polling loop, startup sleep, extra root thread, or ready-queue sentinel.

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

### Phase 2: return from initialization

1. Add the worker-zero bootstrap entry receiving the root Task.
2. Create worker zero with that entry.
3. Remove initialization's eager root-to-dispatcher switch and return to
   generated `main`.
4. On the root's first later switch, commit it once and enter normal dispatch.
5. Add text assertions that initialization contains no context switch and no
   root ready publication.

### Phase 3: runtime lifecycle fixtures

1. Add an exact-output root-yield fixture.
2. Run spawn/join, Channel send/receive/close, and Mutex contention fixtures.
3. Add root completion with no spawned Task, root completion after joined
   Tasks, and root completion with detached Tasks.
4. Bound every process with the existing timeout; a timeout is a failure.
5. Promote the three existing compile-only concurrency fixtures to runtime
   fixtures with exact expectations.

### Phase 4: conformance and docs

1. Run all Validation items under GCC, Clang, and Zig where supported.
2. Run `gofmt`, `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
3. Regenerate the snippet manifest once and review only legitimate concurrency
   runtime artifact changes.
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
- Root completion with no child, joined children, and detached children shuts
  workers down and returns from `main` exactly once.
- The root is never observed both running and ready, and its first park is
  published at most once.
- No startup sleep, polling, extra root thread, or new scheduling phase exists.
- Existing component assertions for park/commit/wake ordering remain true.
- The full external concurrency fixture set compiles and runs under every
  configured supported toolchain.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

After behavior stabilizes, review the Task runtime contract in
`docs/reference.md`. If synchronization is required, record only the precise
root-startup and first-switch semantics; do not copy this implementation plan
or add tutorial examples.
