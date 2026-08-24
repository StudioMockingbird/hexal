# RFC 0091: Blocking-Syscall Boundary

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design decision required
- Created: 2026-08-19
- Scope: what the scheduler does when generated code calls something that blocks
- Depends on: implemented RFC 0108 (descriptor I/O), RFC 0052 (target
  profiles), and the existing M:N scheduler contract
- Gates: complete safe-task behavior for `IO`, a future sockets specification,
  and scheduler-aware filesystem, pipe, and process surfaces
- Coordinates with: `docs/reference.md` (Tasks and synchronization),
  `docs/status.md`, RFC 0039 (foreign blocking declarations), and RFC 0118
  (safe-task and foreign-thread contract)
- Does not change: the M:N model, worker count, `spawn`/`join`/`detach`, or
  `Task.yield()`

## Summary

The scheduler is cooperative. A worker is freed when its task yields, parks, or
finishes — and by no other means. A blocking syscall parks the **OS thread**, so
N workers blocked in `read` stall the scheduler completely, and no construct
available to a Hexal program prevents it.

RFC 0108 has since shipped synchronous descriptor I/O. Its source signatures
are settled and its initial contract explicitly permits a descriptor operation
to block the current worker. Other workers continue; if all workers block, the
scheduler stalls until one call returns.

This RFC is therefore a scheduler-progress upgrade, not a prerequisite for all
I/O. It must preserve the existing `IO` signatures while deciding whether safe
blocking operations continue to consume workers or become scheduler-aware.

Nothing here is designed yet. What follows is what is already determined, so
the design pass starts from it rather than re-deriving it.

RFC 0118 will own the safety rule around this mechanism: an undeclared blocking
foreign call is unsafe, a safe blocking operation must be scheduler-classified,
and it must not silently convert all scheduler progress into an unspecified
stall. This RFC chooses the implementation for that contract; it does not
redefine data-race or task-lifetime rules.

## What is already determined

**The scheduler's existing parking mechanism works and is the model to match.**
`hex_task_join` sets `HEX_TASK_PARKED`, writes itself into the target's joiner
slot, and switches to the worker's dispatch context without requeueing; the
completing task pushes it back. Channel send/receive and `Mutex.lock` park the
same way. In every case **the task parks and the worker stays free** — which is
exactly the property a blocking call destroys.

**Per-worker state already exists.** RFC 0085 added a `_Thread_local` guard
range updated on every context switch, and `hex_current_task` is already
thread-local. A readiness design needing per-worker bookkeeping has somewhere to
put it.

**The scheduler is not preemptive and this RFC does not make it so.** A task
that neither yields nor blocks owns its worker until it finishes. That is
settled and `reference.md` already requires a visible `Task.yield()` on every
repeating path through a task-reachable literal `while true`.

## The boundary has two sides, not one

This is the fact that shapes everything, and it is why one mechanism cannot
serve.

**Pollable.** Sockets, pipes, terminals. `epoll`, `kqueue`, and IOCP report
readiness on them, so an operation can register interest, park the task, and be
woken when the handle is ready. The worker stays free throughout. This is
Go's netpoller and Crystal's event loop.

**Not portably readiness-pollable. Regular files.** `epoll` rejects them and
`select` reports them ready. A read may still block in the kernel. Target
completion APIs such as IOCP or `io_uring` may optimize regular-file operations,
but no one readiness mechanism covers every supported target.

Go, the only surveyed language that handles both, uses two mechanisms: the
netpoller for sockets, and detaching the P from the M — letting that thread
block while another runs the remaining goroutines — for files.

**So this RFC must produce two answers.** A design that solves sockets and
declares files out of scope leaves `0065` still blocked, because files are
`0065`'s primary subject.

## Options — pollable handles

**P1. Readiness parking (netpoller).** Handles are opened non-blocking. An
operation attempts the syscall; on `EAGAIN` it registers with the platform
poller, parks the task, and is requeued on readiness. One poller thread, or
polling folded into the idle path of the existing worker loop.

- Matches the existing park/wake machinery exactly.
- Costs a platform abstraction over `epoll`, `kqueue`, and IOCP — and IOCP is
  completion-based rather than readiness-based, so it is not a third case of the
  same shape.
- Substantial. This is the largest single piece of work implied by any open
  spec.

**P2. Thread hand-off.** Let the call block and ensure another worker picks up
the remaining tasks — by growing the worker pool, or by handing the ready queue
to a fresh thread.

- Far simpler, no poller, no platform abstraction.
- Thread count grows with blocked calls rather than with cores, which is the
  cost model the M:N scheduler exists to avoid. A thousand blocked sockets means
  a thousand threads.

## Options — regular files

**F1. Offload to a blocking pool.** A fixed set of threads performs file
syscalls; the requesting task parks and is woken on completion.

- Bounded thread count, and the task-parking shape matches P1.
- Every file operation costs a hand-off, including reads that would have hit
  page cache and returned immediately.

**F2. Accept the block, documented.** A file read blocks its worker. With
workers equal to core count, concurrent file I/O is limited to that many
in-flight operations.

- Zero machinery.
- A program doing file I/O across many tasks silently loses parallelism, and
  nothing in the language says so.

**F3. `io_uring`.** True asynchronous file I/O on modern Linux.

- Linux-only, so it cannot be the answer — at most an optimisation behind one of
  the above.

## Settled surface constraint

RFC 0108 deliberately made scheduler integration replaceable without changing
stream signatures. This RFC therefore does not add an explicit `Io` parameter,
a source-visible blocking mode, or a `would block` result. Scheduler handling is
runtime-transparent; primitive `IO` results remain transfer, `EoS`, or `Error`.

## What the answer must specify

1. Which mechanism serves pollable handles, and which serves regular files.
2. The runtime ownership and sizing of any poller or blocking pool.
3. How the runtime classifies a handle as pollable or non-pollable.
4. The interaction with `Task.yield()`: whether a parked I/O task counts as a
   scheduling point for the `while true` rule.
5. What happens on a target where the chosen mechanism is unavailable —
   `reference.md` already answers this shape for Tasks with an `Unsupported
   Error`, and the same discipline should apply.

## Implementation readiness

This RFC is not implementation-ready. It still must choose among:

- retaining RFC 0108's current worker-blocking contract and closing this RFC
  without implementation;
- using one bounded blocking pool for every blocking operation; or
- using readiness/completion parking for pollable handles and a bounded
  blocking pool for regular files and other non-pollable operations.

The selected design must also settle pool sizing, poller ownership, handle
classification, the `while true` yield interaction, and unsupported-target
behavior. A detailed implementation plan must be written after that selection;
the current option inventory is not an execution plan.

## Invariants

1. The M:N model, worker count, and cooperative scheduling are unchanged. This
   RFC decides what happens at a blocking call, not how tasks are scheduled.
2. A parked I/O task frees its worker, on whichever mechanism — that is the
   property being bought, and any design that fails it has not solved the
   problem.
3. `spawn`, `join`, `detach`, `yield`, Channel, and Mutex semantics are
   unchanged.
4. No language surface or existing `IO` signature changes.
5. Generated C for programs that perform no I/O is byte-identical.

## Validation

Most of this cannot be verified by the current suite, which executes no
generated C. Record that limit rather than working around it.

- Textual: a blocking-capable operation registers readiness, or hands off, per
  the chosen mechanism, at every emission site.
- Textual: the platform abstraction compiles for both supported targets, with
  the unsupported path producing the specified error rather than a fallback.
- Runnable, and currently unverifiable: N+1 tasks blocked on I/O with N workers
  make progress; a file read does not stall unrelated tasks; a parked task is
  woken exactly once.

The runnable set is the real acceptance criterion. Until generated C is compiled
and executed, this RFC cannot be closed on evidence — which is worth weighing
when deciding how much machinery to take on.

## Non-goals

- Preemption, work stealing, per-worker queues, or scheduler fairness.
- Asynchronous file I/O beyond what the chosen mechanism provides.
- `select`-style multiplexing at the language surface. `reference.md` excludes
  `select`, and nothing here reopens it.
- Designing the I/O surface itself — RFC 0065 and its siblings own that, and
  they are the consumers of this decision, not part of it.
- Timers, signals, or any other pollable that is not an I/O handle.

## Drawbacks

- P1 plus F1 is a large amount of platform-specific runtime code, in the part of
  the system that is hardest to test without executing generated binaries.
- Any answer here becomes load-bearing for every I/O surface that follows, so
  changing it later is expensive in a way that changing 0065 is not.
- Choosing F2 to move quickly means a documented parallelism limit that is
  invisible in source, which is the kind of hidden cost this language otherwise
  refuses.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` with the selected
blocking-call classification, safe-task behavior, unsupported-target failure,
and any source-visible I/O annotation. RFC 0118 remains the authority for the
data-race, task-lifetime, and foreign-thread safety rules.
