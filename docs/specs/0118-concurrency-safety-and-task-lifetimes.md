# RFC 0118: Task and Cross-Task Escape Boundaries

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; rescoped to a regression-only RFC. The escape rule
  it previously proposed is withdrawn: probing showed it would delete six
  working diagnostics and that its narrowing variant is empty. What remains is
  the analysis and the tests that keep it true. See Readiness
- Created: 2026-08-22
- Updated: 2026-09-20
- Scope: establish that task creation, message transfer, `join`, and `detach`
  are **not** escape boundaries for local checker facts, and lock that in with
  regression coverage
- Depends on: the implemented Task, Channel, Stash, Pool, and foreign-call
  contracts in `docs/reference.md`
- Coordinates with: RFC 0039 (foreign contracts), RFC 0157 (uninitialized
  allocation), RFC 0158 (debug allocation tracking), RFC 0160 (memory-bug
  inventory), and RFC 0165 (intraprocedural alias diagnosis)
- Does not update `docs/reference.md`: this revision defines checker/tooling
  integration only; language concurrency semantics remain unchanged

## Summary

**This RFC adds no rule. Its result is that none is needed, and its value is
the proof of that.**

Earlier revisions proposed classifying task creation, message transfer, and
opaque calls as escape boundaries that abandon local pointer facts. Probing
the tree shows that rule would be actively harmful in one direction and empty
in the other:

- For **cleanup facts** (`freed`), escaping at a task boundary deletes six
  working diagnostics and prevents no false positive.
- For **narrowing and capability facts**, a task boundary cannot falsify them,
  because arguments are copied and a task has no way to reach the creator's
  binding without an address — and taking a writable address already escapes.

So the correct classification of `spawn`, `send`, `receive`, `join`, and
`detach` is: **they are not escape boundaries, and the checker is already
correct about them.** This document records the reasoning so the question
stops being re-opened, and states the one condition under which the answer
would change.

None of this adds ownership, borrowing, affine handles, race checking, or
lifetime syntax. It also removes the need for any.

## Current contract

The current reference remains authoritative:

- `Task<T>` is a shallow-copyable shared handle;
- exactly one successful `join` or `detach` is allowed across aliases;
- `spawn` returns `Task<R> | Error`;
- `Channel<T>` accepts only its existing copyable element contract;
- the scheduler and blocking-pool behavior are unchanged;
- foreign calls require their existing `unsafe do ... end` contract.

This RFC does not change any of those rules.

## Why a task boundary is not an escape boundary

There are exactly two kinds of local fact a boundary could plausibly
invalidate. Each is examined against the tree.

### Cleanup facts survive, and must

`flowFact.freed` means "released on every path to this point". It is set by
exactly one thing: a `free` call in *this* function on a locally tracked
binding. A callee or a spawned task releasing the pointer never sets it.

That has a direct consequence: there is no false positive at a task boundary
for an escape rule to suppress. What an escape rule would do instead is
discard true positives. Probed 2026-09-20:

| Program | Today | With a spawn/send escape rule |
| --- | --- | --- |
| `spawn worker(p)`; `h.free(p)`; `^p` | rejected | accepted |
| `spawn worker(p)`; `h.free(p)`; `h.free(p)` | rejected | accepted |
| `c.send(p)`; `h.free(p)`; `^p` | rejected | accepted |
| `use(p)`; `h.free(p)`; `h.free(p)` | rejected | accepted |
| `return p`; caller double-frees the result | rejected | accepted |

Every one of those is a real bug that local analysis decides. Discarding them
is a direct loss against language goal 18.

### Narrowing and capability facts cannot be falsified by a task

A narrowing fact is a claim about a *binding in this function*. A spawned
function receives a copy of the argument value, into its own task frame; it
holds no reference to the creator's slot and cannot write through it. The
same is true of a Channel send, which copies the element.

The only way to give another task access to a binding's storage is to take
its address — and `@` on a writable place already calls `flowState.escape`,
which clears narrowing for exactly that reason. Probed:

```hexal
if maybe != nil then
  let t: Task<Int32> | Error = spawn worker(maybe)
  let v: Int32 = ^maybe        # accepted: narrowing survives, correctly
end
```

```hexal
if maybe != nil then
  let pp: Ptr<mut Ptr<mut Int32> | Nil> = @maybe
  let v: Int32 = ^maybe        # rejected: narrowing dropped, correctly
end
```

The existing `@` rule is not a partial implementation of a task-boundary
rule. It is the complete one, because address-taking is the only channel
through which the hazard can arrive.

### What this means for each operation

- **Task creation.** Arguments are copied into the task frame. Creator facts
  are unaffected. The spawned function's parameters are already untracked
  (`trackablePointerBinding` excludes parameters), so it starts with no
  inherited fact and needs no new rule.
- **Channel transfer.** Elements are copied. The sender keeps its facts; a
  received value sources from a call expression and is tracked as a fresh
  local fact, which is correct.
- **Task results.** `let p = t.join()` sources from a call expression, so `p`
  receives a fresh identity and a local double free of it is rejected today.
  This is the desired behavior and requires no change.
- **Detach.** Detaching changes no local pointer fact and should not. Runtime
  task reclamation remains the Task contract.
- **Opaque or foreign calls.** Unchanged. RFC 0039 owns any stronger foreign
  ownership contract.

The checker must still not infer ownership transfer, lifetime extension, or a
happens-before relation from `spawn`, `join`, `detach`, `send`, or `receive`.
It does not infer them today.

### The condition that would change this answer

This conclusion rests on one property: **task and channel arguments are
copied, and no Hexal construct hands another task a writable reference to a
caller's binding except `@`, which escapes.** If a future RFC adds by-
reference task arguments, a borrow-like parameter mode, or any construct that
lets a task write to a creator's local slot without an address being taken,
this analysis must be redone. Until then, a task boundary is not an escape
boundary.

## Stash, Pool, and uninitialized allocation

- Stash and Pool reset/destroy semantics remain their existing local rules.
  Cross-task region ownership is not added here.
- RFC 0165's allocation-identity work covers pointers, which includes
  pointers allocated from Stash and Pool. Their *handles*, and `String`,
  `List`, and `Dict` handle invalidation, require separate specifications.
  Nothing in this RFC changes that scope in either direction.
- An uninitialized allocation from RFC 0157, if that feature is ever
  promoted, is carried across a task boundary like any other pointer value.
  Transfer neither establishes nor destroys initialization; RFC 0157 owns
  read-before-write rules.

## Non-goals

- Affine Task or Channel handles.
- Ownership transfer, borrow checking, lifetime inference, or automatic
  cleanup.
- A whole-program race detector or cross-task alias proof.
- Rejecting every use after a value has escaped.
- **Adding any `flowState.escape` call site.** The single existing one
  (writable `@`) is the complete rule; this RFC's regression suite exists to
  catch a second one being added.
- Changing Task, Channel, Mutex, Atomic, scheduler, or foreign-call semantics.
- Making detached-task leaks compile-time errors.
- Requiring always-on allocation metadata or runtime race detection.

## Validation

This section is exhaustive for this RFC. Every item is a **regression test
that current behavior is preserved**, because this RFC changes no code. Their
purpose is to make the next proposal to escape at a task boundary fail a test
rather than pass review.

- `spawn worker(p)` followed by `h.free(p)` and `^p` is rejected with
  "this pointer's storage was released on every path to this point".
- `spawn worker(p)` followed by two `h.free(p)` calls is rejected with
  "free releases storage already released on every path to this point".
- `c.send(p)` followed by `h.free(p)` and `^p` is rejected.
- A call argument, a `return`, a member store, and a collection store each
  preserve the pointer's cleanup fact; a subsequent local double free is
  rejected in every case.
- `spawn worker(@p)` drops `p`'s cleanup fact, because `@` escapes; a
  subsequent double free is accepted.
- A narrowing established before `spawn` is still available after it.
- A narrowing established before `@` of the same binding is not.
- `let p = t.join()` receives a fresh local fact: a local double free of `p`
  is rejected.
- A value received from a Channel receives a fresh local fact on the same
  rule.
- `detach()` changes no local pointer fact.
- No diagnostic that exists before this RFC is removed, and none is added.
- Generated C is byte-identical and the snippet manifest moves no hash.
- Ordinary tests remain pure Go.

Compatibility notes, deliberately **not** Validation items because they name
optional behavior owned by another spec: a selected RFC 0158 debug backend may
independently observe a physical allocation that outlived a detached task, and
RFC 0157's uninitialized-state fact, if that feature is ever promoted, rides
the pointer value like any other and is unaffected by task transfer. Neither
is required for this RFC to be complete.

## Implementation plan

No checker change. The entire deliverable is the regression suite.

1. Add the Validation cases to `compiler/tests/integration/`, in the existing
   facet files rather than a new one: the concurrency-boundary cases beside
   the other Task and Channel tests, the pointer-fact cases beside the other
   pointer cleanup tests.
2. Confirm zero movement in the snippet manifest, which follows trivially
   because no production file changes.
3. Coordinate with RFC 0165: its allocation-identity work must keep every one
   of these cases passing. They are the tripwire for an over-broad `escape`
   call site being added there.

## Open questions

1. Should a future ownership or synchronization RFC add per-operation
   summaries for a particular spawn or Channel operation? Outside this RFC.
2. If RFC 0158 is ever asked to attribute a leaked allocation to a detached
   task, its allocation record needs a task-identity field it does not have.
   That is RFC 0158's decision, and this RFC should not assert that the
   capability exists.

Two questions the earlier revision carried are now closed rather than open:
which Channel element forms can carry raw pointers (any `Storable` element
may contain one, but only a bare top-level pointer variable is nameable at a
send site, so nothing here depends on the answer), and what `join()` produces
(a fresh local fact, verified).

## Readiness

**Ready, as a regression-only RFC.** It adds tests and no production code.

That is a rescope, not the original claim. The earlier "ready for
implementation" rested on an unverified assumption about what
`flowState.escape` does and when the checker calls it; the escape proposal it
declared ready would have removed working diagnostics. The record of how that
was found follows, because the conclusion is only as durable as its evidence.

Probed against the tree on 2026-09-20:

| Program | Today |
| --- | --- |
| `spawn worker(p)`; `h.free(p)`; `^p` | **rejected** (use-after-free) |
| `spawn worker(p)`; `h.free(p)`; `h.free(p)` | **rejected** (double free) |
| `c.send(p)`; `h.free(p)`; `^p` | **rejected** (use-after-free) |
| `use(p)`; `h.free(p)`; `h.free(p)` | **rejected** |
| `return p`; caller double-frees the result | **rejected** |
| `let pp = @p`; `h.free(p)`; `h.free(p)` | accepted |

`flowState.escape` has exactly one production site that *originates* an escape:
a writable `@` in `compiler/checker/places.go:537`. The only other call,
`scope.go:662`, is the branch merge propagating an `escaped` flag some branch
already set, so it creates no escape of its own. Nothing else abandons a
cleanup fact. Routing
`spawn`, Channel `send`, `join`, `detach`, and opaque calls through it would
turn the first five rows from rejected to accepted, deleting six diagnostics
that work today.

The reason is structural, not incidental: **`freed` is only ever set by a
local `free` in the same function.** A callee or a spawned task releasing the
pointer never sets it, so no false positive exists at those boundaries for
escape to suppress. Escape earns its keep for facts a write through an
escaped address can falsify — narrowing, and the IO capability fact — because
those are claims about a value the checker can no longer see. It buys nothing
for a fact this function established about its own `free` calls.

Deleting true positives to prevent false positives that cannot occur is a net
loss against language goal 18 ("catch every memory error that a local
analysis can decide").

Re-aiming the rule at narrowing was considered next, and probing closed that
door too: narrowing survives `spawn` and survives an ordinary call, and is
dropped only by `@`. Since a task can reach a creator's binding by no other
route, the existing `@` rule is already the complete rule. See Why a task
boundary is not an escape boundary.

With both candidate rules empty, what remains worth keeping is the proof, and
the tests that keep it true.

This RFC also intentionally does not make its former data-race and
affine-ownership proposal ready; those remain separate design work and are not
part of this memory-diagnostics arc.

### Questions a rescoped version must answer

Independent of which direction is chosen, these were left open while the RFC
claimed readiness:

- **Exact escapable category.** "Pointer-like or allocation-backed value" is
  not a predicate. RFC 0165 scopes to `Ptr<T>`/`Ptr<mut T>`; this RFC must use
  the same definition or say why it differs.
- **Expression-to-binding traversal.** `spawn worker(p)` names a binding;
  `spawn worker(record.pointer)` and `channel.send(make_pointer())` do not.
  Only a bare pointer variable is nameable, which bounds what any rule here
  can reach.
- **Task results are already answered by the tree, and the current text is
  correct**: `let p = t.join()` sources from a call expression, so `p` is
  tracked as a fresh local fact and a local double free of it is rejected
  today. No change needed; the answer should just be stated rather than left
  as "tracked only if the ordinary local checker can establish a fresh fact".
- **Channel element forms.** Any type `Storable` at `PositionChannelElement`
  may contain a pointer, including inside structs, ADTs, arrays, and unions.
  But only a top-level bare pointer variable is nameable at the send site, so
  nested pointers are outside any rule this RFC can express. Say that, and do
  not imply that widening Channel's element surface is in scope.
- **Detach reporting.** "Physical allocations reachable only by the detached
  task remain RFC 0158's concern" claims more than RFC 0158 can deliver: it
  tracks physical allocations, not reachability, and cannot attribute a block
  to a detached task without task identity in the allocation record. Either
  RFC 0158 adds that field, or this sentence must be weakened.
