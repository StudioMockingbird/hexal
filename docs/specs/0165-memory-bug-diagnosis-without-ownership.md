# RFC 0165: Memory-Bug Diagnosis Without Ownership Semantics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion (proposal); not scheduled. This RFC is an
  alternative to one *part* of the ownership arc, not a replacement for it;
  see Relationship to the ownership arc before promoting either
- Created: 2026-09-10
- Updated: 2026-09-20
- Origin: an audit of eleven classical memory-bug classes against the shipped
  compiler, asking which are already solved, which are not, and what the
  cheapest remaining fix is
- Depends on: nothing. Every mechanism below extends code that ships today
- Coordinates with: RFC 0110, RFC 0149, RFC 0153, RFC 0154, RFC 0155 (the
  ownership arc, all in design), RFC 0156 (fenced pointer arithmetic),
  RFC 0157 (uninitialized allocation), RFC 0158 (debug allocation tracking)
- Does not update `docs/reference.md`: synchronize only after implementation
  is approved and behavior stabilizes

## Summary

Hexal already rejects several locally provable classical memory-bug cases, and does
so without any ownership, lifetime, or borrow machinery. The confirmed alias
gaps are use-after-free and double-free through copied bindings. They share
one root cause: the shipped cleanup analysis tracks a *binding*, not an
*allocation*, so a single assignment turns both checks off. Leaks are a
separate runtime-tracking concern owned by RFC 0158.

This RFC proposes closing the alias portion of that gap in place, by extending
the flow state the checker already maintains. It adds no keyword, no type, no
move semantics, and no lifetime model. Runtime physical leak detection remains
RFC 0158's responsibility. Static leak rejection is deliberately not selected
by this revision; the decision is recorded below.

It is deliberately a **bug finder, not a guarantee**: it reports what local
facts prove and stays silent otherwise, which is the policy `docs/reference.md`
already states for cleanup misuse ("An undecided case is always accepted").

## Evidence

Every row below was probed against the current tree by compiling a source
program through `compiler.Compile` and reading the returned diagnostics. The
Result column is observed behavior, not a reading of specification text.

### Current locally-proven coverage

| Bug class | Probe | Result |
| --- | --- | --- |
| Null pointer dereference | `.value` on an unnarrowed nullable pointer | rejected: "may be Nil; narrow it before using .value" |
| Out-of-bounds read (constant) | `a[7]` on `Array<Int32, 3>` | rejected: "array index 7 is out of bounds for Array<Int32, 3>" |
| Stack buffer overflow | pointer arithmetic `p + 1` | rejected: "operator + requires numeric operands; got MutPtr<Int32>" |
| Heap buffer overflow | same, plus no pointer casts | rejected: "expected Ptr<Int64> initializer; got MutPtr<Int32>" |
| Off-by-one (as memory safety) | runtime `l[i]` | accepted, then bounds-checked at runtime; traps rather than corrupting |
| Uninitialized memory read | `let n: Int32` with no initializer | rejected by the current `let` declaration syntax |
| Uninitialized heap allocation | `h.allocate<Int32>()` | rejected: "allocation requires an explicit initializer" |
| Missing string null-terminator | `s[0]` on a String | rejected: "cannot index String; use rune_cursor() ..."; String is pointer-plus-length with the count in its header and is never NUL-terminated |
| Invalid / arbitrary free | `h.free(ref n)` for a local `n` | rejected: "free does not accept a pointer into this function's local storage" |

The mechanism is worth naming, because it is the cheap one and it is already
this project's habit: **these were solved by removing the capability, not by
analyzing it.** No pointer arithmetic, so no overflow. No casts, so no type
confusion. Nullability in the type, so no null dereference. Mandatory
initializers, so no garbage reads. Length-carrying strings, so no terminator
bug. Removal costs nothing at compile time and cannot be unsound.

RFC 0156 and RFC 0157 propose reintroducing two of these capabilities
(pointer arithmetic, uninitialized allocation) behind RFC 0155's
`unsafe do ... end`. That preserves the pattern rather than breaking it: the
capability returns fenced and named, and safe Hexal keeps the guarantee.

### Confirmed remaining gaps

| Bug class | Probe | Result |
| --- | --- | --- |
| Use-after-free, direct | `h.free(p)` then `p.value` | rejected: "this pointer's storage was released on every path to this point" |
| Use-after-free, **through an alias** | `let q = p`, `h.free(p)`, then `q.value` | **accepted** |
| Double free, direct | `h.free(p)` twice | rejected: "free releases storage already released on every path to this point" |
| Double free, **through an alias** | `let q = p`, `h.free(p)`, `h.free(q)` | **accepted** |
| Use-after-free, **across a call** | `release(h, p)` then `p.value` | **accepted** |
| Memory leak | `let p = h.allocate<Int32>(1)` and nothing else | **accepted** |

The direct forms already work. Every failure is an alias, an escape, or a
call boundary.

## Root cause, in the code

`compiler/checker/scope.go` defines the per-function fact table:

```go
type flowState struct {
	facts           map[BindingID]flowFact
	tracked         map[BindingID]bool
	released        map[BindingID]map[uint64]bool
	provenance      map[BindingID]BindingID
	releasedSources map[BindingID]bool
	stringOrigins   map[BindingID]stringOriginSet
	stringPlaces    map[stringPlaceKey]stringOriginSet
}
```

Its own comment states the design: "tracked distinguishes a known cleanup
state from an intentionally unknown state after a copy or escape."

The abandonment happens in the pointer-copy path in
`compiler/checker/declarations.go`:

```go
if sourceBinding := directPointerBinding(initializer.source, declaredType); sourceBinding != 0 {
	ctx.names.flow.dropFreed(sourceBinding)
	ctx.names.flow.dropFreed(declaredBinding.id)
}
```

On `let q = p`, **both** bindings lose tracking. The same behavior appears for
pointer assignment.

The consuming check, `checkTrackedHeapFreeInState`
(`compiler/checker/alloc.go`), then requires a directly named, still
tracked binding:

```go
if state == nil || value.Node.Kind != VariableExpression || value.Node.Binding == 0 || !state.tracked[value.Node.Binding] {
	return nil
}
```

So the analysis is not missing; it is *switched off* by aliasing. The
existing branch merge in `compiler/checker/scope.go` already carries these
maps across control flow, and `flowFact.freed` already means "released on
every path to this point" — the machinery for path-sensitive, no-false-
positive reasoning is present and working.

## Proposal

One focused addition is proposed. It is independently shippable and useful.

### 1. Track allocations, not bindings

Replace the drop-both behavior with an alias set: a union-find over
`BindingID` recording bindings that provably denote the same allocation.

- On `let q = p` (and on assignment) where `directPointerBinding` identifies a
  source, **join** the two bindings instead of untracking them.
- `markFreed` marks the whole set; `freed` reads the whole set.
- Escape (`flowState.escape`) dissolves the set, exactly as it drops tracking
  today.

**Merging at control-flow joins is by intersection.** An alias edge survives
a join only when it holds on every incoming path. This matches the existing
meaning of `freed` ("on every path") and is the direction that produces no
false positives: a `q` that aliases `p` on only one branch is not provably
the same allocation at the join, so nothing is reported.

This alone fixes both alias rows in the evidence table.

### 2. Deferred decision: static leak diagnosis

Static diagnosis of non-escaping leaks is not part of this RFC. RFC 0158 owns
opt-in runtime physical-allocation tracking, which can report leaks after
values escape through calls or containers. Static leak rejection is deferred
until Hexal has an intentional process-lifetime allocation convention.

### 3. Optional, later: per-function summaries

Infer, bottom-up over the module graph, whether a function frees a pointer
parameter and whether it returns a fresh allocation. Feed those summaries
into alias checks to reach the cross-call row in the evidence table.

Deliberately sequenced last. Alias checks need no interprocedural analysis
at all; this one does, and should be built only if the first two prove
insufficient in practice.

## Non-goals

- Ownership, affinity, moves, borrows, or lifetimes.
- Automatic cleanup insertion. This RFC diagnoses; it never frees.
- New keywords, new types, or any change to `Ptr`/`MutPtr` spelling.
- Stash and Pool reset/region alias semantics in the first implementation;
  those handles need a separate invalidation model.
- Soundness. Undecidable cases stay accepted, matching the current contract.
- Runtime leak detection, which RFC 0158 covers with a tracking allocator.
- Cycle detection. Nothing here detects cyclic garbage; nothing here creates
  the ability to build a cycle either.

## Relationship to the ownership arc

RFCs 0110, 0149, 0153, 0154, and 0155 are all in design. Nothing is built.
That makes this a live comparison rather than a retrofit.

The arc pursues two distinct goals that are easy to conflate:

1. **Diagnose memory misuse.** This RFC addresses alias-derived use-after-free
   and double-free at roughly the cost of one alias-set structure.
2. **Delete the cleanup calls.** The arc turns the `List<String>` cleanup in
   `workbench/snippets/categories/07-collections.json` from N+1 correctly
   ordered calls into zero. **This RFC does not do that at all.** Under this
   proposal the author still writes every free, and is merely told when the
   count or the order is wrong.

Goal 2 is the arc's real prize, and it is unreachable from here: automatic
drop requires affine moves, because two shallow copies of one `String` handle
would otherwise both be dropped. There is no cheap version of goal 2.

Consequences of choosing this RFC *instead of* the arc, stated plainly so the
trade stays visible:

- affine iteration stays unaffected — `for name in names` over a
  `List<String>` keeps working, where RFC 0110 currently rejects it;
- the roughly 90 consuming `defer` sites across snippets and tests keep
  working, where RFC 0110 currently rejects deferred consuming cleanup;
- `docs/reference.md`'s statement that Hexal has "no moves, borrow states,
  retain counts, implicit destructors, or compiler-enforced exactly-once
  cleanup" remains true and needs no rewrite;
- the three View-escape bugs in `docs/status.md` stay open, where RFC 0153
  closes them by making stored borrows unexpressible;
- cleanup composition stays manual, which is the cost.

The two are not mutually exclusive. Alias checking is useful whether or not
the arc lands, because raw `Heap.allocate`/`free` survives the arc unchanged
as the C-interoperation layer, and nothing in the arc diagnoses misuse there.

One piece of the arc is worth keeping under either choice: **`Box<T>` with
automatic drop as an opt-in type beside `Heap.allocate`.** It requires affine
moves for `Box` alone — not for `String`, `List`, `Dict`, or anything
shipped — so it breaks no `defer`, forces no migration, and gives new code a
leak-proof allocation while raw `Heap` stays for interop.

## Diagnostics

Reuse the three shipped messages verbatim where they apply; an alias-derived
rejection is the same bug as a direct one and should not read differently:

- `free releases storage already released on every path to this point`
- `this pointer's storage was released on every path to this point`
- `free does not accept a pointer into this function's local storage`

Diagnostics name source bindings and operations, never `BindingID` values,
alias-set identities, or generated C names.

## Required sweep

- `flowState` alias representation, `clone`, and the join-by-intersection
  merge in `compiler/checker/scope.go`;
- the two `dropFreed` alias sites in `compiler/checker/declarations.go`;
- `checkTrackedHeapFreeInState` to read the raw-Heap pointer alias set instead
  of a single binding;
- `flowState.escape` to dissolve alias sets;
- the explicit decision that static leak diagnosis is deferred to RFC 0158;
- `compiler/checker/alloc_test.go` and `io_test.go`, which encode the current
  accept-on-alias behavior and need cases added, not changed;
- Stash and Pool handle tracking remains unchanged and outside this first
  alias-set implementation.

## Validation

This section is exhaustive.

- Every current locally-proven evidence row continues to produce its exact current
  diagnostic; this RFC changes none of them.
- Use-after-free and double free through a single alias are rejected with the
  existing messages.
- An alias chain of length three or more is rejected at every link.
- An alias formed on only one branch of an `if` is **accepted** after the
  join; no false positive is produced by a conditional alias.
- An alias formed on every branch is rejected after the join.
- A loop that aliases on the back edge preserves the loop-head alias state and
  reports no false positive.
- Escape through a call, return, member store, collection store, or `ref`
  dissolves the alias set and restores today's accept-everything behavior.
- Stash and Pool behavior is unchanged; their handle/reset semantics require a
  separate design before alias tracking can cover them.
- Generated C is byte-identical for every program that compiled before this
  RFC; this is a checker-only change and moves no manifest hash.
- Ordinary tests remain pure Go.

## Detailed implementation plan

### Phase 1: alias sets

1. Add a union-find alias structure to `flowState` beside `tracked`, with
   `clone` support and an intersection-based join merge.
2. Replace the paired `dropFreed` calls in `declarations.go` with a join.
3. Route `markFreed`, `freed`, and `checkHandleNotDestroyed` through the set.
4. Dissolve sets in `escape`.
5. Add the alias, branch, and loop Validation cases.

### Phase 2: conformance

1. Confirm no generated-C or manifest movement.
2. Add compact workbench snippets only where a snippet demonstrates a
   diagnostic; this RFC otherwise adds no snippet surface.
3. Run ordinary and tagged C23 suites.
4. Synchronize `docs/reference.md`'s cleanup-misuse paragraph — which
   currently states that a pointer "copied to a second binding is not tracked"
   and that "leaks are not diagnosed" — only after behavior stabilizes.

### Phase 3 (optional): interprocedural summaries

1. Compute per-function free-parameter and returns-allocation summaries.
2. Extend alias checks to consume them, keeping fail-open behavior wherever a
   summary is unavailable. Leak summaries remain outside this RFC unless the
   static-leak question is separately resolved.

## Decisions and remaining questions

1. **Does this replace part of the arc, or accompany it?** If RFC 0110 lands
   in full, checks 1 and 2 still apply to raw `Heap` pointers, which the arc
   leaves manual. If the arc is deferred, this becomes the whole memory-safety
   story for cleanup. The answer changes nothing in this document's design but
   determines its priority.
2. Static leak rejection is deferred. RFC 0158's opt-in runtime tracker is the
   selected mechanism for leak reporting. Revisit this only after Hexal has an
   explicit convention for intentional process-lifetime allocations.
3. Alias tracking is limited to raw `Heap` pointers in the first
   implementation. `String`, `List`, `Dict`, `Stash`, and `Pool` handle
   semantics require separate designs.
