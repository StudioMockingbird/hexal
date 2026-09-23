# RFC 0225: Cross-Allocator Release Rejection

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation ready, blocked only on sequencing. Graduated from
  RFC 0160's Invalid-free row, which records the gap but owns no work.
  Implement after RFC 0165, whose allocation identity is the key this RFC's
  allocator-kind fact uses; every open question is settled
- Created: 2026-09-20
- Updated: 2026-09-23
- Scope: reject releasing a pointer through an allocator that provably did not
  produce it, using the provenance edge the checker already records
- Depends on: nothing. The provenance edge, its recorder, and one consumer of
  it all ship today
- Coordinates with: RFC 0165 (allocation identity), which determines whether
  the rule reaches aliased pointers
- Updates `docs/reference.md`: yes, the Allocation and lifetime cleanup-misuse
  paragraph, after behavior stabilizes

## Summary

Releasing a Stash or Pool allocation through `Heap.free`, or a Heap allocation
through `Pool.free`, is undefined behavior today and compiles without a
diagnostic. Both are decidable from a local, syntactic fact the checker already
computes and already consults for a neighbouring rule.

```hexal
let h: Heap = Heap()
let mut pl: Pool<Int32> = Pool<Int32>(4)
let p: Ptr<mut Int32> = pl.allocate(1)
h.free(p)                                 # accepted today; must be rejected
pl.destroy()
```

This adds no type, keyword, ownership rule, lifetime, or runtime cost. It adds
one classification to an existing flow fact and three rejection sites.

## Motivation

A Pool slot is an interior pointer into `pool->slots`, and a Stash allocation
is an interior pointer into a stash block. Passing either to `mi_free` is
undefined behavior at the C level — not a leak, not a double free, but a
corrupt-the-allocator bug whose symptom appears arbitrarily later. The mirror
case, handing a `mi_malloc` pointer to `Pool.free`, writes a foreign address
onto the pool's free stack and hands it out as a live slot on the next
`allocate`.

Neither is exotic. Both arise from the ordinary refactor of moving an
allocation from one allocator to another and missing one release site, which is
exactly the mistake a compiler should catch and a human should not have to.

Language goal 18 asks the compiler to catch every memory error a local analysis
can decide. This one is decidable from the initializer expression of the
pointer binding, with no interprocedural analysis, no new type-system
dimension, and no data structure that does not already exist.

## Current behavior

Probed against the tree on 2026-09-20 by compiling through `compiler.Compile`
and reading the returned diagnostics.

| Program | Today |
| --- | --- |
| `st.free(p)` | rejected: "Stash allocations are released by reset or destroy" |
| `pl.free(p)` where `p` came from a different Pool | rejected: "pointer was allocated from a different Pool" |
| `pl.free(p)` where `p` came from a Stash | rejected, with the **wrong message**: "pointer was allocated from a different Pool" |
| `pl.free(p)` where `p` came from the same Pool | accepted, correctly |
| `h.free(p)` where `p` came from a Stash | **accepted** |
| `h.free(p)` where `p` came from a Pool | **accepted** |
| `pl.free(p)` where `p` came from the Heap | **accepted** |
| `h.free(q)` where `let q = p` and `p` came from a Pool | **accepted** |

The three accepted rows in bold are the gap. The misleading Stash message is a
separate, smaller defect fixed by the same change.

## The fact already exists

`compiler/checker/io.go` records an allocator edge for every pointer binding as
it is declared:

```go
if declaredType.Element != nil {
    flow.setProvenance(id, allocatorSourceBinding(source.Node))
}
```

`allocatorSourceBinding` returns the receiver binding of a direct
`stash.allocate(...)` or `pool.allocate(...)` call, and zero for every other
pointer-producing expression. `compiler/checker/pool.go` already consults it:

```go
if source, ok := ctx.names.flow.provenance[pointerBinding]; ok && source != 0 && source != receiverBinding {
    return ... "pointer was allocated from a different Pool"
}
```

So the Pool-to-Pool case is solved, and the Stash-to-Pool case is solved by
accident — a Stash handle binding is non-zero and unequal to the Pool receiver,
so it trips the same branch and reports the wrong allocator.

What is missing is the other direction. `Heap.free` consults nothing, and
cannot: zero means *unknown*, not *Heap*. A pointer from `h.allocate`, from a
parameter, from a member read, and from a foreign call are all indistinguishable
today.

## Proposal

### 1. Classify the allocator, not just the handle

Record which kind of allocator produced a pointer binding, alongside the
existing handle edge:

```go
allocatorKind map[BindingID]allocatorKind   // heapAllocator | stashAllocator | poolAllocator
```

A binding absent from the map is **unknown**, which is the default and stays
accepted everywhere.

The classifier extends `allocatorSourceBinding` rather than replacing it:

| Initializer expression | Kind | Handle edge |
| --- | --- | --- |
| `h.allocate<T>(initial)` | heap | none |
| `h.allocate_aligned<T>(initial, n)` | heap | none |
| `st.allocate(initial)`, `st` a named handle | stash | `st` |
| `pl.allocate(initial)`, `pl` a named handle | pool | `pl` |
| anything else | unknown | none |

`provenance` keeps its exact current meaning and its current consumers —
`invalidateAllocationsFrom` for Stash reset, `hasLiveTrackedAllocation` for
Pool destroy — untouched. This RFC only adds a parallel fact.

### 2. Reject a release through a disagreeing allocator

| Release | Rejected when the pointer's kind is | Accepted when |
| --- | --- | --- |
| `Heap.free(p)` | stash, pool | heap, unknown |
| `Pool<T>.free(p)` | heap, stash, or a *different* pool handle | same pool handle, unknown |
| `Stash<T>.free(p)` | — already rejected unconditionally; unchanged | — |

Unknown is always accepted. That is the existing contract in
`docs/reference.md` — "An undecided case is always accepted" — and it is what
keeps this rule free of false positives: a pointer arriving as a parameter or
read out of a collection is never classified, so it is never rejected.

### 3. Report the allocator that actually produced it

The Stash-to-Pool case currently borrows the different-Pool message. With a
kind available, each case names the real source.

## Diagnostics

Worded to match the existing `free does not accept a pointer into this
function's local storage`, which is the nearest shipped diagnostic.

- `free does not accept a pointer allocated from a Stash`
- `free does not accept a pointer allocated from a Pool`
- `Pool free does not accept a pointer allocated from the Heap`
- `Pool free does not accept a pointer allocated from a Stash`
- `pointer was allocated from a different Pool` — unchanged, and now reported
  only when the source really is another Pool

Diagnostics name source bindings and operations, never `BindingID` values,
allocator-kind identities, or generated C names.

## Relationship to RFC 0165

These two specs want the same fact keyed the same way, and the order they land
in changes how much this rule covers.

`allocatorKind` keyed by `BindingID` does not follow an alias, so the last row
of the Current behavior table stays accepted:

```hexal
let p: Ptr<mut Int32> = pl.allocate(1)
let q: Ptr<mut Int32> = p
h.free(q)                                 # still accepted if keyed by binding
```

RFC 0165 introduces exactly the right key: an allocation identity that every
binding denoting the same allocation shares. Keying `allocatorKind` by
allocation identity instead of by binding closes the aliased case for free,
because the kind is a property of the allocation, not of the name.

**Recommended sequencing: RFC 0165 first, then this RFC keyed by allocation
identity.** If this RFC lands first, key by `BindingID`, accept that aliased
pointers are undiagnosed, and re-key during RFC 0165's Phase 1 — the Validation
case for the aliased row moves from accepted to rejected at that point, which
must be a deliberate, recorded change rather than an incidental one.

## Non-goals

- Ownership, affinity, moves, borrows, lifetimes, or automatic cleanup.
- Interprocedural or whole-program analysis. A pointer arriving as a parameter
  is unknown and stays unknown.
- Proving that a pointer from a foreign or `unsafe` source belongs to any
  allocator.
- Runtime allocator-mismatch detection. RFC 0158 owns any runtime facility;
  nothing here adds metadata or a check to a generated program.
- Tracking allocator kind through `.offset` or `.cast` results, which RFC 0165
  leaves as fresh identities.
- Rejecting a release of a pointer whose kind is unknown, in either direction.
- Changing Stash or Pool reset/destroy semantics, or the existing live-slot and
  stale-allocation rules built on `provenance`.

## Validation

This section is exhaustive.

Rejections:

- `let p = st.allocate(1)` then `h.free(p)` is rejected with `free does not
  accept a pointer allocated from a Stash`.
- `let p = pl.allocate(1)` then `h.free(p)` is rejected with `free does not
  accept a pointer allocated from a Pool`.
- `let p = h.allocate<Int32>(1)` then `pl.free(p)` is rejected with `Pool free
  does not accept a pointer allocated from the Heap`.
- `let p = h.allocate_aligned<Int32>(1, 64)` then `pl.free(p)` is rejected with
  the same Heap message.
- `let p = st.allocate(1)` then `pl.free(p)` is rejected with `Pool free does
  not accept a pointer allocated from a Stash`, **not** with the different-Pool
  message.
- `let p = a.allocate(1)` then `b.free(p)` for two Pool handles is rejected with
  the existing `pointer was allocated from a different Pool`, unchanged.

Acceptances, each a case the rule must not break:

- `let p = h.allocate<Int32>(1)` then `h.free(p)`.
- `let p = h.allocate_aligned<Int32>(1, 64)` then `h.free(p)`.
- `let p = a.allocate(1)` then `a.free(p)` for one Pool handle.
- `h.free(p)` where `p` is a function parameter: unknown kind, accepted.
- `h.free(p)` where `p` was read from a struct member or a collection element:
  unknown kind, accepted.
- `h.free(p)` where `p` came from a call result: unknown kind, accepted.
- `pl.free(p)` where `p` is a parameter: unknown kind, accepted.
- A pointer produced inside `unsafe do ... end` by `.offset` or `.cast` has
  unknown kind, and releasing it through any allocator is accepted.

Interaction with existing rules:

- `st.free(p)` remains rejected with `Stash allocations are released by reset or
  destroy`, ahead of any kind check.
- The existing local-storage rejection still fires first for `h.free(p)` where
  `p` traces to `@`: a pointer to a local has unknown kind, so the two rules do
  not compete.
- Stash reset invalidation and the Pool destroy live-slot check are unchanged;
  each still reads `provenance`, which this RFC does not modify.
- A deferred release (`defer h.free(p)`) is checked on the same rule as a
  straight-line one, against the kind recorded when the binding was declared.

Aliasing:

- `let p = pl.allocate(1); let q = p; h.free(q)` is **rejected** with the Pool
  message. The key in force is RFC 0165's allocation identity, which every
  binding denoting one allocation shares, so the kind follows the allocation
  rather than the name.
- An alias formed on only one branch of an `if` does not carry the kind past
  the join, matching RFC 0165's identity-agreement merge: a conditional alias
  is not proof of a shared allocation, and no diagnostic is produced.
- `let p = pl.allocate(1); let q = p; pl.free(q)` is **accepted** — the alias
  carries the correct pool handle, not merely the kind.

Conformance:

- Generated C is byte-identical for every program that still compiles, and the
  snippet manifest moves no hash. No catalog snippet performs a cross-allocator
  release, verified by inspection of every snippet that contains both an
  `allocate` and a `free`.
- Programs that previously compiled and performed a cross-allocator release now
  fail. That is the point of the RFC, and every such program had undefined
  behavior; it is a bug-fix rejection, not a compatibility break to be softened.
- Ordinary tests remain pure Go.

## Implementation plan

1. Add `allocatorKind` to `flowState` in `compiler/checker/scope.go`, with
   `clone` support and a branch merge that keeps a kind only when every
   incoming path agrees — the same discipline `freed` already uses.
2. Extend the classifier beside `allocatorSourceBinding` in
   `compiler/checker/io.go` to return a kind alongside the handle, recognizing
   `HeapAllocateExpression`, `HeapAllocateAlignedExpression`, and the Stash and
   Pool `allocate` method calls.
3. Record the kind in `seedStreamBindingFacts`, where the handle edge is
   already recorded, so both facts are seeded at one site.
4. Add the kind check to `checkHeapFree` in `compiler/checker/alloc.go`, after
   the existing local-storage rejection and before the tracked-free check.
5. Extend the existing provenance branch in `compiler/checker/pool.go` to
   consult the kind, so it reports the real source instead of assuming another
   Pool.
6. Add the Validation cases to `compiler/tests/integration/pointers_test.go`
   beside the other cleanup-misuse cases, and to `compiler/checker/alloc_test.go`
   for the classifier itself.
7. Confirm zero snippet-manifest movement.
8. Update `docs/reference.md`'s cleanup-misuse paragraph, which currently names
   three rejected misuses, to name the allocator-mismatch rule as a fourth.

## Open questions

1. **Settled, 2026-09-23: RFC 0165 lands first and this RFC keys
   `allocatorKind` by allocation identity.** The `BindingID` fallback is not
   implemented, so the aliased row in the Current behavior table is rejected
   from this RFC's first commit rather than flipping later. Every Validation
   entry below that reads "keyed by `BindingID`" is therefore inert and kept
   only to record what was considered.
2. Should a pointer whose kind is heap be *required* to be released through
   `Heap.free` — that is, should a known-heap allocation that is never freed
   become diagnosable? No. That is static leak diagnosis, which RFC 0165
   defers to RFC 0158 and which needs a process-lifetime allocation convention
   Hexal does not have. Recorded here only to close it explicitly.
3. Whether `String`, `List`, `Dict`, and `Channel` should carry an allocator
   kind so that freeing one against the wrong Heap is diagnosable. With exactly
   one default allocator there is no wrong Heap today, so the question is empty
   until Hexal has more than one. It is named because a future multi-allocator
   change would make it live, and this is the fact that would carry it.
