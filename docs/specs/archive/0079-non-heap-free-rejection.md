# RFC 0079: Statically Decidable Memory Misuse

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-18. Both checks are live in
  `compiler/checker/`: check 1 consumes the existing `fromRef` fact in
  `alloc.go`, and check 2 adds `flowFact.freed` in `scope.go` with the
  survives-the-branch merge rule. All six Validation rejections reject with the
  three diagnostic wordings this RFC dictates, and all eleven acceptances still
  compile — including the two load-bearing boundaries, `defer h.free(p)`
  followed by `p.value` (so `freed` is set when the deferred call fires, not at
  registration) and `h.free(p); consume(p)` (so passing is not a dereference).
  Verified by an independent probe through `compiler.Compile`, not only by the
  committed tests. Generated C is untouched and the snippet SHA-256 manifest
  never moved. Coverage: `TestHeapFreeRejectsRFCBoundaries` and
  `TestHeapFreeAcceptsUntrackedAndSafeCases` in
  `compiler/tests/integration/pointers_test.go`, plus thirteen unit tests in
  `compiler/checker/alloc_test.go` covering deref sites this RFC named only in
  passing — method receivers, volatile access, terminating-return paths, and
  deferred capture after reallocation.

  The Sequencing note's known gap did not materialize: RFC 0073's D4 landed
  first, so `p = ref x; q = p; h.free(q)` is rejected by check 1 rather than
  recorded as a gap.
- Created: 2026-08-16
- Updated: 2026-08-18
- Scope: the memory misuses a local flow analysis can decide with no new
  language concept — freeing a non-heap pointer, double free, and
  use-after-free, each on a local binding the checker can still see
- Depends on: nothing. Independent of RFCs 0072–0077 (0078 is closed). RFC 0073's D4 widens what
  check 1 catches; see Sequencing.
- Coordinates with: `docs/reference.md`, `AGENTS.md`, `docs/status.md`,
  RFC 0027 (Arena and Pool — allocator matching lands there)
- Does not change: Hexal syntax, allocation, cleanup, ownership, aliasing, or
  copying semantics; generated C

## Summary

Hexal accepts every memory misuse today. Verified by probe against the current
checker, all accepted:

| Program | Today |
|---|---|
| `h.free(ref x)` on a local | ACCEPTED |
| `h.free(p) h.free(p)` | ACCEPTED |
| `h.free(p)` then `p.value` | ACCEPTED |
| `defer h.free(p)` then `h.free(p)` | ACCEPTED |
| `p: MutPtr<Int32> = h.allocate<Int32>(0)`, never freed | ACCEPTED |

The project position is that the compiler should catch as many memory errors at
compile time as is feasible **without adding a concept to the language** — no
moves, no borrow states, no lifetimes, no ownership annotations, nothing the
programmer writes or reads. That constraint is on the *language surface*, not on
the compiler's internal analysis.

**Four of the five rows above are decidable** under that constraint using
machinery the checker already has. This RFC specifies those four — the
`defer`-plus-explicit row is a subcase of check 2, not a separate check. Only
leak detection is undecidable, and it is a non-goal.

## The ruling that supersedes the previous framing

An earlier draft of this RFC treated the single `free(ref local)` case as
possibly not worth having, on the grounds that `docs/reference.md` says:

> there are no moves, borrow states, retain counts, implicit destructors, or
> compiler-enforced exactly-once cleanup

That sentence is correct about *mechanism* and was being read as a statement of
*ambition*. It is not one. Absence of an ownership model does not imply absence
of diagnosis: the compiler already proves bounds, nullability, union tags, and
initialization without any of those mechanisms. `docs/reference.md` and
`AGENTS.md` goal 18 have since been corrected to say so; this RFC applies the
corrected policy rather than arguing for it.

## What is decidable, and what is not

### Decidable — check 1: category

`h.free(p)` where `p` is locally traceable to `ref` on a local or parameter
binding. The argument is statically known not to have come from an allocator.

The analysis already exists and already behaves correctly.
`View.from_pointer` consumes `binding.fromRef` (`compiler/checker/checker.go:223`,
set at `declarations.go:392`, read at `views_bridge.go:114`). Probed:

```
from_pointer(ref local)            rejected: from_pointer does not accept a pointer
                                             into this function's local storage
from_pointer(p) where p = ref x    rejected  ← propagates through one binding
from_pointer(opaque param ptr)     ACCEPTED
from_pointer(heap ptr)             ACCEPTED
```

That is exactly the policy `free` needs, including the two acceptances that
must not regress. `Heap.free` consumes the same fact. No new analysis.

### Decidable — check 2: freed state

`h.free(p)` twice, or a dereference through `p` after `h.free(p)`, where `p` is
a local binding whose address the checker has not lost. The `defer`-plus-explicit
case is a subcase of the first, not a third rule.

`flowState` (`compiler/checker/scope.go:92`) is already a per-binding,
branch-aware lattice keyed by `BindingID`, with `clone`, `mergeBranch`,
`adopt`, and `escape`. It carries narrowing facts today; its comment records
that ownership tracking was removed from it. Adding a `freed` fact reuses the
structure.

One rule differs and must be written deliberately: **narrowing facts die at
`end`, freed facts survive it.** `mergeBranch` propagates only invalidation
today because no narrowing may outlive its branch. A freed fact is the
opposite — freed on every continuing path means freed after the construct, and
freed on some paths means the state is unknown, which is treated as freed for
double-free purposes only if every continuing branch freed it. Freeing in one
branch and not another is accepted.

### Not decidable — leaks

A local heap pointer never freed is only a leak if it also never escapes: not
returned, not stored in a member, not put in a collection, not passed to a
function that keeps it. Deciding that needs interprocedural escape analysis,
and getting it wrong rejects correct programs. Excluded. `defer h.free(p)` is
the idiom the language offers instead, and it is a programmer discipline, not a
checked property.

### Not decidable here — allocator mismatch

`h.free(p)` where `p` came from a different allocator is a real category error,
but Heap is the only allocator today, so there is nothing to mismatch. RFC 0027
owns it when Arena and Pool land. The `fromRef` fact this RFC generalizes is the
natural place to carry an allocator identity later.

## The change

### Check 1 — free of a non-allocator pointer

`Heap.free` rejects an argument locally traceable to `ref`. Diagnostic, matching
the existing `from_pointer` wording so the two read as one rule:

> `free` does not accept a pointer into this function's local storage

### Check 2 — freed-state tracking

`flowFact` gains one field:

```go
type flowFact struct {
    typ     compilerTypes.Type
    escaped bool
    variant *compilerTypes.AdtVariant
    freed   bool // this binding's pointee was released on every path to here
}
```

- `h.free(p)` where `p` resolves to a local binding sets `freed`.
- `h.free(p)` where the fact is already `freed` is rejected:
  > `free` releases storage already released on every path to this point
- A **dereference** through `p` where the fact is `freed` is rejected:
  > this pointer's storage was released on every path to this point

  Dereference means the concrete sites that read or write the pointee: `.value`,
  auto-dereferencing member access, and indexing. **Passing `p` as an argument is
  not a dereference and is not rejected.** A callee may only store, forward, or
  compare the address, none of which is undefined behaviour, so rejecting the
  pass would invalidate currently-valid programs to catch something the callee
  may never do — and whether it does is interprocedural, which this RFC does not
  attempt. The fact is *retained* across a pass rather than dropped: Hexal passes
  by copy, so a callee cannot rebind the caller's binding. Taking `ref p`
  escapes and drops the fact through the existing `escape` path.
- Assigning a new value to `p` clears `freed`. `mut p` reallocated after a free
  is correct and must keep compiling.
- `defer h.free(p)` is **validated when it fires**, at scope end, against the
  state accumulated to that point — not at the point it is registered. An
  explicit `free` sets `freed` immediately, so by the time the deferred free is
  checked the binding is already `freed` and it rejects. A `defer` alone never
  rejects, because nothing set `freed` before it fired.

  The distinction is load-bearing. Marking `freed` at *registration* would reject

  ```hexal
  defer h.free(p)
  v: Int32 = p.value      -- legal: the free has not run yet
  ```

  which is the language's own cleanup idiom and must keep compiling.

### The conservative drops — where the soundness lives

Every one of these abandons tracking rather than reporting. The rule is that an
unknown state is never an error.

| Situation | Effect |
|---|---|
| `q: MutPtr<T> = p` — the pointer is copied | drop the fact on **both** bindings |
| `ref p` taken, or `p` passed where its address escapes | existing `escape`, drop the fact |
| `p` is a parameter, member read, or collection element | never tracked |
| the free target is not a simple binding reference | not tracked |
| branches disagree on whether `p` was freed | not freed; no diagnostic |

Copy-drops-both is the deliberately lazy choice. Tracking `q` as an alias of
`p`'s allocation would catch `q = p; free(p); free(q)`, and would cost an alias
relation the lattice does not have. The common case — one binding, one
allocation, one free — is caught without it.

## Documentation changes

`AGENTS.md` goal 18 already records the governing policy — catch what a local
analysis decides, add no language concept, no disproportionate checker
complexity, never error on an unknown state. It needs no change; this RFC is
that policy applied.

`docs/reference.md` already carries the placeholder bullet under Allocation and
lifetime that this RFC replaces. Current text:

> Cleanup misuse is rejected at compile time wherever a local analysis decides
> it. **Today that set is empty**: `h.free(ref local)`, double free, and reading
> through a freed pointer all compile.

becomes:

> Cleanup misuse is rejected at compile time wherever a local analysis decides
> it. Three are rejected: freeing a pointer traceable to `ref`, freeing a local
> binding already freed on every path to that point, and reading through one.

The remainder of that bullet — the untracked cases, leaks, and "an undecided
case is always accepted" — is already correct and stays verbatim.

The edit lands **with the implementation**, not before it. `reference.md`
records what the language means today; changing it first would make it false.

## Invariants

1. **The acceptance surface shrinks by exactly the four misuse classes above and
   nothing else.** Rejecting programs is the point, so "no currently valid
   program becomes invalid" would contradict the RFC; the real invariant is that
   every *other* currently-valid program keeps compiling. Specifically, every
   accepted `free` of a heap-derived pointer still compiles — through
   parameters, members, collection elements, copies, and returns — as does every
   dereference the four classes do not name.
2. Leaks remain undiagnosed. Interprocedural misuse remains undiagnosed.
3. Generated C is byte-identical. Both checks are checker-only; no runtime
   check is emitted. The snippet SHA-256 manifest must not move.
4. No language surface changes: no keyword, annotation, type, or syntax.
5. An unknown state never produces a diagnostic.

## Validation

Must reject:

```hexal
h.free(ref x)                                        -- check 1, direct
p: MutPtr<Int32> = ref x  h.free(p)                  -- check 1, one binding
h.free(p) h.free(p)                                  -- check 2, double
h.free(p) v: Int32 = p.value                         -- check 2, use after
defer h.free(p) h.free(p)                            -- check 2, defer + explicit
if flag then h.free(p) else h.free(p) end h.free(p)  -- check 2, both branches
```

Must continue to accept — one negative test each, because these are what
invariant 1 protects:

```hexal
p: MutPtr<Int32> = h.allocate<Int32>(5)  h.free(p)        -- allocator
fun release(h: Heap, p: MutPtr<Int32>) do h.free(p) end   -- parameter
h.free(obj.pointerMember)                                 -- member
h.free(list[0])                                           -- collection element
q: MutPtr<Int32> = p  h.free(p)  h.free(q)                -- copy: not tracked
mut p: ... h.free(p) p = h.allocate<Int32>(1) h.free(p)   -- reallocated
if flag then h.free(p) end                                -- one branch only
p: MutPtr<Int32> = h.allocate<Int32>(0)                   -- leak
defer h.free(p)                                           -- the idiom
defer h.free(p)  v: Int32 = p.value                       -- defer timing boundary
h.free(p)  consume(p)                                     -- pass is not a deref
```

The last two are the boundaries of the amendments to check 2 and must not be
dropped: the first fails if `freed` is set at `defer` registration instead of
when the deferred call fires, and the second fails if argument passing is
treated as a dereference. Both would break invariant 1.

Plus `go test ./...`, `go vet ./...`, and the snippet manifest unchanged.

The probe in this RFC's Summary table should land as a test file recording the
before/after acceptance of all five rows — four flipping to rejected, the leak row unchanged.

## Sequencing

Independent of every other open spec. Two notes:

- Check 1 catches `p = ref x; q = p; h.free(q)` only once RFC 0073's D4 fixes
  `fromRef` propagation through copies. Land either order; if this RFC lands
  first, record the two-binding case as a known gap referencing D4 rather than
  omitting the test.
- Check 1 is a day of work and check 2 is not. They are separable commits and
  should be separate: check 1 consumes an existing fact, check 2 adds a fact
  with a new merge rule.

## Non-goals

- Leak detection, escape analysis, or reachability of allocations.
- Ownership, moves, borrow states, lifetimes, exactly-once cleanup, or any
  annotation the programmer writes.
- Interprocedural provenance or interprocedural freed state.
- Alias tracking between pointer bindings.
- Allocator matching — RFC 0027.
- Any runtime check.

## Drawbacks

- A partial check invites the reading that `free` is generally validated. The
  reference wording states every limit explicitly for that reason, and the
  "must continue to accept" list is deliberately longer than the reject list.
- `freed` is the first flow fact that outlives its branch. That asymmetry with
  narrowing is a real subtlety in `mergeBranch` and needs a comment at the
  merge site saying why the two directions differ.
- Copy-drops-both means `q = p; free(p); free(q)` compiles. This is a knowingly
  accepted false negative, not an oversight.
- Loops are not specified here, and the implementation does not carry a `freed`
  fact across a back edge: `while true do h.free(p) end` compiles. Observed
  during closing verification. It is consistent with "an unknown state never
  errors", but unlike the copy case it is decidable — a body that frees on
  every path and does not reallocate frees twice on the second iteration.
  Recorded as a gap rather than fixed, because the merge rule for back edges is
  a second analysis question, not a detail of this one.
