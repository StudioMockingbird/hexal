# RFC 0118: Concurrency Safety and Task Lifetimes

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Created: 2026-08-22
- Scope: data-race safety, task-handle ownership, foreign-thread entry, and
  Arena interaction with concurrent tasks
- Depends on: RFC 0027 (Arena and Pool), RFC 0039 (C interoperability), RFC
  0052 (target profiles), RFC 0110 (affine ownership), and the existing Task,
  Channel, Mutex, Atomic, and synchronous IO contracts in
  `docs/reference.md`
- Coordinates with: RFC 0055 (build and validation driver), RFC 0108 (I/O),
  RFC 0121 (scheduler-aware blocking), RFC 0122 (safe Task parking), native
  module storage RFC 0116, `docs/reference.md`, and `docs/status.md`

## Summary

Safe Hexal code has a defined concurrency contract. An unsynchronized
conflicting access is not “no guarantee” and is never silently accepted as
safe. It is either rejected by local checking, protected by a specified
synchronization primitive, or placed behind an explicit unsafe/foreign
boundary.

Affine ownership supplies task and resource lifetime. It does not by itself
make shared mutation safe, so this RFC defines the boundary between ownership,
sharing, synchronization, and scheduler operations.

## Current behavior being changed

The current reference deliberately provides a weaker contract:

- `Task<R>` is a shallow-copyable shared handle; aliases race to the one
  successful `join` or `detach`.
- Unsynchronized conflicting access is recorded as a data race with no Hexal
  guarantee.
- `Mutex` is an untyped synchronization handle with no checker-visible
  association to the storage it is intended to protect.
- Hexal has no general `unsafe` block or expression syntax.

This RFC cannot be implemented as a checker-only tightening without settling
those surface changes. In particular, treating "some Mutex is locked" as proof
that an arbitrary pointer access is protected would accept programs whose two
tasks lock different mutexes before racing on the same storage.

## Data-race rule

Two operations conflict when they access the same mutable storage and at least
one writes. Safe code must establish one of these proofs before the operations
can overlap:

- exclusive ownership or an exclusive borrow;
- an `Atomic<T>` operation for the entire accessed value;
- a `Mutex` or equivalent lock protecting the value;
- a `Channel` or another RFC-defined message-transfer operation; or
- a stronger target/library synchronization contract explicitly named by a
  future specification.

The checker rejects a conflict it can decide locally. If it cannot prove the
access safe, the program must use an explicit unsafe operation or a declared
foreign synchronization contract. Generated C must not rely on C's data-race
undefined behavior as a Hexal semantic fallback.

`volatile` expresses device-observation semantics only. It does not establish
inter-thread synchronization and does not satisfy this rule.

## Task handles

- A `Task<T>` handle is affine. Copying it is forbidden; moving it transfers
  the right to perform the terminal operation.
- Exactly one terminal operation consumes a handle: `join`, `detach`, or an
  explicit cancellation operation if a later task RFC adds one.
- `join` transfers the result and any ownership explicitly returned by the
  task. `detach` relinquishes observation and requires the task's remaining
  state to satisfy its lifetime contract.
- A task cannot retain a borrow of a stack binding, a local View, or an Arena
  region that may end before the task. Moving an affine owner into a task is
  permitted only when the task consumes it or returns it through `join`.
- A task handle that reaches scope exit without a terminal operation is a
  compile-time error unless a type contract explicitly defines abandonment.
- Joining or detaching a moved, already-consumed, or otherwise invalid handle
  is rejected statically where local facts decide it and traps at the runtime
  boundary only where the state is genuinely dynamic.

## Shared state

- `Atomic<T>` is the only lock-free shared mutation primitive in the initial
  safe surface. Its supported types and sequentially consistent operations
  remain those of the reference until a memory-order RFC changes them.
- `Mutex` protects the data reached through its protocol. A lock must be held
  for the protected access, and lock misuse is a runtime error under the
  existing Mutex contract.
- `Channel<T>` transfers ownership or a permitted shared value according to
  the element's affine classification. Sending an affine value moves it; the
  sender cannot use it afterward.
- A mutable module `static` is shared state, not an implicit atomic. RFC 0116
  supplies storage and this RFC supplies its access requirements.
- An Arena or Pool is not a synchronization primitive. Concurrent access to
  its region or slots requires ownership transfer, a lock, atomic access where
  applicable, or an explicit unsafe contract.

## Arena and Pool interaction

- A task may own an Arena or Pool and must consume or explicitly return it
  before `join` completes, unless a shared allocator contract says otherwise.
- A task may not receive a borrow or View rooted in an Arena or Pool whose
  lifetime does not provably cover the task's entire execution.
- `reset`, `destroy`, and slot release are rejected while another live task may
  access the affected region and the checker can prove that relation.
- An undecidable cross-task lifetime or alias relation requires an explicit
  unsafe operation; it is not treated as safe because the scheduler is
  cooperative.
- A task that outlives its creator after `detach` must own or otherwise retain
  every resource it can access. Creator-stack borrows and creator-owned Arena
  regions cannot remain reachable.

## Blocking operations

RFC 0091 was discarded after RFC 0108 settled synchronous descriptor IO. A
native IO operation may block its current OS worker; other workers continue,
and all-worker blocking can delay ready Tasks until one operation returns.
This RFC does not change that scheduling contract.

- RFC 0039 must classify a foreign call whose blocking behavior affects its
  ownership, callback, or synchronization contract; the current runtime does
  not use that classification to park a Task.
- A blocking operation must not hold a Mutex or other scheduler-critical guard
  across the operation unless its owning specification explicitly permits it.
- Worker blocking is documented behavior rather than a data-race proof or an
  implicit scheduling point.
- An IO operation does not satisfy the visible `Task.yield()` requirement for
  a task-reachable literal `while true`, because the operation may complete
  immediately.
- An outstanding `IO.read` destination or `IO.write` source remains shared
  memory. Concurrent mutation, resize, or free is an unsynchronized conflict;
  this RFC owns rejection when local analysis proves it. RFC 0121's Task
  parking neither creates nor removes that aliasing rule.
- RFC 0122 first corrects Task parking, joining, and reclamation without
  changing IO. RFC 0121 then replaces this backend without changing IO
  signatures; neither is a dependency of this safety RFC.

## Foreign-thread entry

- A C thread may not call an arbitrary Hexal function or callback as if it were
  a Hexal task.
- RFC 0039 must provide an explicit attach/entry contract that establishes
  runtime state, target calling convention, ownership transfer, and whether
  the callback may access Hexal-managed shared state.
- An attached foreign thread must obey this RFC's synchronization and task
  lifetime rules. An unregistered or incompatible callback is an unsafe ABI
  operation.

## Memory model and generated C

The compiler emits the C23 atomic, Mutex, Channel, and scheduler operations
specified by their owning RFCs. It must not lower a rejected safe conflict to a
plain C load/store or depend on a C implementation's data-race behavior.
Target-profile facts for atomics, threads, TLS, and OS facilities come from RFC
0052; the driver verifies that the selected toolchain supports them.

## Non-goals

- A whole-program race detector or Rust-style borrow checker.
- Preemptive scheduling or a change to the existing M:N model.
- Making arbitrary C callbacks, device registers, or foreign thread pools safe
  without an explicit RFC 0039 contract.
- Adding a readiness poller, blocking pool, or thread-handoff mechanism inside
  this RFC; RFC 0122 owns safe parking and RFC 0121 owns the separate pool.

## Validation

This section is exhaustive. RFC 0118 is complete only when every item below
passes:

- A locally decidable unsynchronized conflicting access is rejected in safe
  code; an explicit unsafe boundary is required for unknown safety.
- Atomic, Mutex, and Channel accesses satisfy their stated synchronization
  contracts and do not accept a plain mutable alias as synchronization.
- Task handles cannot be copied, terminally consumed twice, or abandoned
  without the specified contract.
- Affine task arguments move exactly once, and joined results transfer exactly
  the declared ownership.
- Detached tasks cannot retain creator-stack borrows or creator-owned Arena or
  Pool storage.
- Arena reset/destroy and Pool slot release reject locally decidable live-task
  access and accept valid ownership transfer.
- Foreign blocking declarations obey RFC 0039's eventual ABI and ownership
  contract. Current synchronous IO remains permitted to block its OS worker and
  does not count as `Task.yield()`.
- Foreign-thread entry requires the explicit attach/ABI contract and obeys the
  same synchronization rules.
- Generated C uses the selected C23 synchronization operations and never emits
  a plain unsafe access for a rejected safe program.
- Ordinary tests remain pure Go and assert diagnostics and generated-C text;
  runtime scheduler and toolchain execution belong to RFC 0055 validation.

## Open questions

1. Whether task transferability uses new user-visible traits or one structural,
   compiler-owned predicate over existing ownership classes.
2. Whether `Task<R>` becomes affine or retains the current shared-alias handle
   contract.
3. Whether `detach` is rejected when `R` is an affine owner whose discarded
   result would leak a resource.
4. Whether the safe Mutex surface becomes `Mutex<T>` with an affine guard, or
   the existing untyped Mutex remains a manual low-level primitive that cannot
   prove safe shared access.
5. Whether borrowed pointers and Views are initially forbidden as task
   arguments except for program-lifetime storage, or accepted through a new
   cross-task lifetime proof.
6. Whether unknown race safety is rejected until RFC 0039 defines a foreign
   unsafe contract, or this RFC adds a general unsafe syntax.
7. Whether cancellation and lock-order diagnostics remain separate future
   features.
8. RFC 0039 owns foreign blocking annotations. RFC 0122 owns the parking
   primitive and RFC 0121 owns scheduler-aware execution for the native
   operations it names; neither blocks this RFC.

## Implementation readiness

This RFC is not implementation-ready. Its ownership checks depend on RFC 0110,
and questions 1-7 above alter source-visible behavior or the builtin API. A
detailed implementation plan must be added after those decisions are settled;
writing one now would hide design choices inside implementation steps.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` with the safe data-
race rule, task-handle affine rules, task transfer and detach lifetimes, Arena
and Pool cross-task restrictions, and foreign-thread entry contract. Preserve
the current synchronous-IO worker-blocking rule only until RFC 0121 lands;
afterward preserve RFC 0121's Task-parking contract. Remove the current “data
races have no guarantee” rule in the same change.
