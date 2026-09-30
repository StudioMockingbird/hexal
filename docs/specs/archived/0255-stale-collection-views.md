# RFC 0255: Stale Collection Views

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. Implemented and verified 2026-09-30: the checker tracks
  storage roots for allocated List and String handles, stale pointer, Slice,
  and cursor views with union joins and loop fixpoints, exact structural
  operations, callable summaries (exported to importers, merged across known
  indirect targets, fail-closed otherwise), and the three diagnostics; the
  22 validation cases pass and no snippet artifact moved; see Implementation
  state
- Supersedes: the "programmer's responsibility" clause for Slices into
  growing or freed storage in `docs/reference.md` (Slice section)

## Summary

A pointer or Slice into an allocated List's storage, or a Slice into a String,
stays usable after the storage moves or is released. The checker accepts the
use, and the generated C reads freed memory:

```hexal
let h = Heap()
let mut xs: List<Int32> = List<Int32>(h)
xs.push(1)
let p = @xs[0]
xs.push(2)          -- may reallocate: p now points at released storage
print(^p)           -- accepted today; undefined behavior in C
```

This RFC makes the checker reject every such use that a local analysis can
see. It adds no syntax and no lifetime types: it reuses the flow-state
provenance edges that already invalidate Stash allocations on
`reset`/`destroy`.

## Evidence

Probed on 2026-09-29 with `compiler.Compile` on the snippets below (each
prefixed by the three lines above that create `xs`). Every case compiled with
exit 0:

| Case | Source |
| --- | --- |
| pointer, push | `let p = @xs[0]` / `xs.push(2)` / `print(^p)` |
| pointer, pop | `let p = @xs[0]` / `xs.pop()` / `print(^p)` |
| pointer, free | `let p = @xs[0]` / `xs.free(h)` / `print(^p)` |
| pointer, push through alias | `let p = @xs[0]` / `let ys = xs` / `ys.push(2)` / `print(^p)` |
| pointer, push through captured call | `fun grow() do xs.push(2) end` / `let p = @xs[0]` / `grow()` / `print(^p)` |
| Slice, push | `let s = xs.slice(0, 1)` / `xs.push(2)` / `print(s[0])` |
| Slice, free | `let s = xs.slice(0, 1)` / `xs.free(h)` / `print(s[0])` |

Two related cases are not bugs:

- An inline `List<T, N>` never moves, so `@a[0]` stays valid across `push`.
- `Dict` has no element places (`cannot index Dict<...>`), so no pointer into
  its buckets exists.

An allocated List is structurally mutable through every binding that holds its
handle, including a fixed `let`, a parameter, and an entry-environment capture
(probed: all three `push` forms compile). So a call is a real mutation
boundary.

`docs/reference.md` currently declares the Slice cases outside the contract
("may produce undefined behavior in generated C"), and says nothing about
element pointers. Both contradict the language goals of no undefined
behavior and "if it compiles, it runs". Goal 18 requires the compiler to
catch every memory error that a local analysis can decide.

## Definitions

- **Storage root**: one compiler identity for the backing allocation of an
  allocated `List<T>` or allocated `String`. Handle copies, parameters, and
  handle-valued members initialized or later assigned from an alias share that
  identity. Existing List/Dict collection roots are reused where correct;
  String and handle-valued member places require explicit storage-root
  propagation rather than assuming the existing binding-only machinery covers
  them.
- **View of R**: a value whose storage points into R's backing store:
  - `@R[i]` and any place under it (`@R[i].member`);
  - `R.slice(...)`, `R.mut_slice(...)`, and an allocated String's `bytes()`
    Slice, `ByteCursor`, `RuneCursor`, or `GraphemeCursor`;
  - any re-slice or `offset` of a view of R;
  - a binding or inline aggregate (object, ADT, inline List) initialized or
    later assigned from a view of R. Provenance is place-sensitive; replacing
    that place with a fresh view replaces its previous stale relationship.
- **Invalidating operation on R**: the closed allocated-List structural set
  `push`, `pop`, `clear`, and `free`, shared with traversal checking, plus
  `free` for an allocated String root. Element replacement is not structural
  and does not invalidate views. Legacy checker names for unsupported List
  operations do not extend this language-level set.

## Rules

1. **Local invalidation.** After an invalidating operation on R, or on any
   alias of R, every view of R is stale on that path.
2. **Call boundary.** Every checked callable has a summary recording which
   parameters and captured roots may be structurally changed, which view
   parameters may escape, and whether its result borrows a parameter or
   receiver. A call invalidates views of R only when the summary says the
   corresponding argument, receiver, or captured root may be structurally
   changed. A view argument is rejected when the summary says it may escape.
   A missing or unresolved summary fails closed as may-change and may-escape.
   Compiler-owned operations carry exact summaries.
3. **Use is rejected.** Reading, writing, dereferencing, indexing,
   re-slicing, passing, returning, or storing a view that is stale on **any**
   path reaching the use is rejected with `type.stale-collection-view`:
   `this <pointer|slice|cursor> points into <root>'s storage, which was structurally changed at <line:column>`.
   Staleness merges as a union at control-flow joins, because one stale path
   is enough to reach invalid storage. The stale fact stores its invalidation
   span; when paths supply different spans, the earliest source position wins
   deterministically. This may-stale state is separate from freed-state,
   whose existing merge has different semantics.
4. **View and root to one call.** A call that receives a view of R and also
   receives R, an alias of R, or has R in its capture set is rejected when its
   summary may structurally change or escape either relationship, with
   `type.collection-view-passed-with-root`:
   `call receives a view of <root> and can also change <root>`.
   A proven readonly, non-escaping helper may receive both. This is the
   element-pointer form of traversal call checking. Inside the callee, a view
   parameter and a List parameter cannot be related, so the relationship check
   belongs at the call.
5. **Re-deriving from live storage is valid.** A new `@R[i]` or `R.slice(...)`
   after a non-freeing invalidation is a fresh view. Existing freed-state
   checks still reject deriving anything from a freed root.
6. **Untracked escape is explicit.** Safe code rejects an escape described
   below with `type.collection-view-escapes-root`:
   `view of <root> escapes storage tracked by the compiler; place this operation in unsafe only when the root outlives every use`.
   The same operation is admitted inside `unsafe ... end`.

```hexal
let p = @xs[0]
xs.push(2)
print(^p)                    -- rejected: stale-collection-view
let q = @xs[0]               -- ok: fresh view
print(^q)                    -- ok

let s = xs.slice(0, 1)
sum(s)                       -- ok: summary says s does not escape
sum_with(s, xs)              -- ok only when summary is readonly and non-escaping
```

### Escape boundary

Safe code rejects hiding a rooted view in storage the checker cannot follow:
allocated List and Dict elements, Heap pointees, writes through `Ptr<mut T>`,
module storage, and spawned Task arguments or captures. Returning a direct or
nested view is accepted only when the callable result summary preserves its
source root. `unsafe ... end` may perform an otherwise rejected escape and
explicitly transfers lifetime responsibility to the programmer; the compiler
adds no taint or tracking after that escape. Unsynchronized cross-Task access
remains governed by the existing data-race contract; this RFC does not make a
racing program safe.

## Implementation

- `compiler/checker/flow.go`: reuse the provenance walk, but add a distinct
  may-stale fact carrying an invalidation span. A place that becomes a view of
  R gets a provenance edge to R's storage root. An invalidating operation, or
  a call under rule 2, marks every reachable view stale. Stale facts join by
  union across branches and loop back-edges until the forward flow state
  stabilizes; `try`, early exits, and deferred paths use the same merge rule.
- The use check sits beside the existing `type.use-after-free` check, where
  pointer and Slice operands are read.
- The rule 4 check sits beside the traversal "unproven call" check and reuses
  its capture-set lookup.
- Callable summaries record structural changes to each
  parameter/captured root, whether a view parameter escapes, and whether a
  result borrows a parameter or receiver. Imported summaries live in checked
  module records and interface fingerprints. Direct effects seed a least
  fixpoint over resolved calls. Indirect function values merge every known
  target summary; an unknown target is conservatively may-change/may-escape.
- Add diagnostic registry records and typed emitters for
  `type.stale-collection-view` and
  `type.collection-view-passed-with-root`, and
  `type.collection-view-escapes-root`. Existing freed/use-after-free diagnostics
  win when the same operand is already known freed.
- Generated C: no change.

### Swept code

Delete both `docs/reference.md` statements that make growth and free
invalidation "the programmer's responsibility" and allow undefined behavior:
the copy-semantics statement and the Slice-section statement. Replace them
with rules 1-6 and the settled escape policy. Keep the separate exclusive-use
rule for aliased writable Slices; aliasing is not solved by stale-view checks.
Allocator reset and destroy keep their existing rule. No compiler code exists
only because this rule was absent.

`GRAMMAR.ebnf`: no change.

## Validation

Integration tests in `compiler/tests/integration/pointers_test.go` (pointer
cases) and `compiler/tests/integration/slice_test.go` (Slice cases):

1. Each of the seven evidence cases is rejected with
   `type.stale-collection-view`.
2. A view is stale when the invalidating operation happens on only one
   branch of an `if` (join by union).
3. A fresh `@xs[0]` after `xs.push(2)` is accepted.
4. A view used before any invalidating operation is accepted.
5. `@a[0]` on an inline `List<T, N>` stays valid across `a.push(...)`.
6. A call receiving a view of `xs` and `xs` is rejected with
   `type.collection-view-passed-with-root` when its summary may structurally
   change or escape them. A proven readonly, non-escaping helper and a call
   with an unrelated List are accepted.
7. A call to a function whose capture set contains `xs`, receiving a view of
   `xs`, is rejected when its summary may structurally change or escape the
   relationship; a proven readonly, non-escaping capture is accepted.
8. A struct binding initialized with `@xs[0]` as a member becomes stale after
   `xs.push(2)`.
9. A String `bytes()` Slice used after `s.free(h)` is rejected.

10. `pop` and `clear` invalidate every existing element pointer and Slice;
    diagnostics use "structurally changed", not a false claim that storage
    necessarily moved.
11. Element replacement does not invalidate a pointer or Slice. Inline
    `List<T, N>` remains outside relocation tracking and retains its existing
    local-lifetime rules.
12. A `mut_slice` write, re-slice, pass, return, or store after invalidation is
    rejected, not only an indexed read.
13. `ByteCursor`, `RuneCursor`, and `GraphemeCursor` derived from an allocated
    String are stale after freeing any String alias. Inline-String cursors are
    outside allocation invalidation.
14. A String `bytes()` Slice is rejected after freeing a different handle alias
    of the same String storage.
15. A handle-valued object member initialized or later assigned from an alias
    shares the root; mutation through that member invalidates existing views.
16. Reassigning a stale view binding or aggregate place to a fresh view clears
    the old stale relationship.
17. A loop-path invalidation makes a later use stale. Two invalidating branch
    sites choose the earliest source span deterministically.
18. Creating a view in one call argument and structurally changing its root in
    another argument is rejected after checking the complete argument list.
19. A fresh derivation after `push`, `pop`, or `clear` is accepted; derivation
    after `free` receives the existing use-after-free diagnostic.
20. A proven read-only, non-escaping helper receiving the root does not stale
    a live view. An unresolved indirect call fails closed and rejects it.
21. A returned pointer or Slice retains its parameter or receiver storage root
    through the callable summary, including when nested in an aggregate.
22. Safe code rejects untracked escape through heap storage, module storage,
    `Ptr<mut T>`, and `spawn`; the same operation is admitted only inside
    `unsafe ... end`, with `type.collection-view-escapes-root` asserted for
    every rejected form.

No existing snippet hash changes. Run `gofmt -l`, `go test ./...`, and
`go vet ./...`; the ordinary suite remains toolchain-free.

## Implementation state

- The analysis is a checked-tree pass (`compiler/checker/views*.go`) that runs
  after a module checks clean, beside the starvation pass, not threaded
  through the in-line flow state. Summaries need every callable body, loops
  need a fixpoint, and joins need a total order over paths, none of which the
  single-pass checker provides; the pass owns them and reuses only the
  collection-root and freed-state facts the checker already computes.
  `use-after-free` keeps precedence because the checker reports it first.
- One closed structural classification (`structuralListOperation`) serves view
  invalidation and the traversal scanner.
- Summaries live in the module registry records (`moduleEntry.viewSummaries`)
  that importers read. The tree has no interface-fingerprint mechanism, so the
  plan's fingerprint participation has nothing to attach to yet.
- Indirect calls merge the summaries of every callable a function-valued place
  was initialized or assigned from; a `Fun` parameter or any other source is an
  unresolved target and fails closed. A call to an imported generic
  specialization resolves only when its summary is published, otherwise fails
  closed.
- An `errdefer` action is not applied on the joined success paths. A deferred
  call's operands are checked at registration and its effect applied where its
  scope exits.
- Diagnostics point at the use's statement, or the expression's own span when it
  has one; the change site is the statement that made the view stale.
- No generated file or snippet hash moved.

## Settled decisions

- Callable summaries provide structural-change, non-escape, and borrowed-result
  facts before this RFC lands; unknown indirect calls fail closed.
- Safe code rejects a rooted view escaping tracking. `unsafe ... end` is the
  explicit programmer-responsibility boundary.
- `pop` invalidates every existing view because the removed element may be the
  referenced element and a non-constant index cannot prove otherwise.

## Alternatives considered

- **Programmer responsibility, as in Zig and Odin.** Rejected because Hexal
  promises to diagnose locally decidable memory errors and generated-C
  undefined behavior.
- **Runtime allocation generations or List version checks.** This could avoid
  conservative compile-time rejection, and List traversal already uses a
  version check, but it would add state and checks to every view use. Rejected
  for the initial design because local provenance can catch the named cases
  without runtime overhead. A debug-only defense remains possible later.
- **A full ownership/lifetime type system.** Rejected as disproportionate and
  contrary to the language goal of compiler-assisted manual memory management.

## Detailed implementation plan

1. Introduce a generalized storage-root identity for allocated List and String
   handles. Propagate it through copies, parameters, member initialization, and
   later place assignment without changing Dict's unrelated root behavior.
2. Extend view discovery to element pointers, Slices, String cursors,
   re-slices, offsets, and inline aggregates. Replace provenance when a place
   is assigned a fresh value.
3. Add separate may-stale flow facts with deterministic invalidation spans.
   Implement union merges for branches and loop fixpoints without changing
   freed-state intersection semantics.
4. Define one checker-owned closed structural-operation classification shared
   by traversal and view invalidation: allocated-List `push`, `pop`, `clear`,
   `free`, and allocated-String `free`.
5. Implement whole-call checking: implicit receivers and explicit arguments
   contribute views and roots, and mutation discovered later in the argument
   list revalidates earlier views.
6. Compute callable summaries to a least fixpoint, export and fingerprint them,
   merge indirect-call targets, and fail closed for unknown targets. Apply
   summaries to returned borrows, captures, root mutation, and view escape.
7. Reject untracked safe-code escape through heap/module storage, mutable raw
   pointers, and Tasks; admit it only in `unsafe ... end`.
8. Register and emit the three diagnostics, preserving earlier freed-state
   diagnostics and deterministic source positions.
9. Add every exhaustive validation case above. Assert that generated files and
   the snippet manifest are byte-identical because this is checker-only.
10. Synchronize both affected `docs/reference.md` sections, retain the separate
   writable-Slice aliasing warning, and run the ordinary formatting, test, and
   vet gates.
