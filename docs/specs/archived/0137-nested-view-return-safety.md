# RFC 0137: Nested View Return Safety

- Kind: Language Semantics
- Status: Closed; implemented
- Created: 2026-08-27
- Updated: 2026-09-08
- Origin: RFC 0103 finding F19
- Restores: the language goal that locally decidable dangling borrows are
  rejected
- Coordinates with: RFC 0110 (mutable owning-container alias and lifetime
  rules), `docs/reference.md`
- Does not own: interprocedural result-provenance summaries; RFC 0103 finding
  F43's caller provenance for `View.from_pointer`; Views stored behind
  pointers or in mutable List/Dict storage; or Views transferred through Task
  and Channel

## Summary

Reject a returned inline value when it or any nested `View` in it is proven to
borrow storage local to the returning function.

The checker currently rejects a directly returned local-rooted `View`, but the
same View escapes when wrapped in an object, ADT, union, Array, or another
returnable aggregate. This is a current memory-safety bug.

## Current failure

```hexal
type Window is struct visible: View<Int32> end

fun bad(): Window do
    fixed: Array<Int32, 4> := [1, 2, 3, 4]
    return Window(visible = fixed.slice(0, 2))
end
```

The program currently compiles. `visible.data` points into `fixed`, whose
storage ends when `bad` returns.

## Semantics

1. Return safety follows the checked inline value, not merely its declared
   type.
2. The checker recursively inspects every inline value component that can
   contain a `View`: object members, ADT payloads, union payloads, Array
   elements, match-arm and try-success results, and values copied through local
   bindings.
3. Local bindings that merely copy a View or aggregate are transparent:
   ultimate storage roots, not intermediate binding names, decide return
   safety. If any possible root is local, including after control-flow merging,
   the return is rejected.
4. Empty Views, direct View parameters, Views rooted in external storage
   reached through String/List parameters, `self`-reached storage, and
   `View.from_pointer` regions remain returnable under the current direct
   provenance contract. A parameter is not inherently safe: an Array or other
   inline value is copied into callee storage, so a View newly derived from
   that parameter copy is local-rooted and cannot return.
5. A type containing `View` is not rejected by itself. The checker rejects
   only a value whose tracked provenance proves a local root.
6. Unknown or interprocedural provenance retains the current behavior. This
   RFC adds no whole-program borrow analysis and no conservative rejection of
   opaque calls.
7. The diagnostic is:
   `a returned value contains a View that borrows a local of this function`.
   A directly returned View keeps its existing diagnostic.
8. List and Dict are mutable owning handles, and a heap-allocated aggregate is
   observed only through pointer aliases. Proving which Views those locations
   contain requires mutation and alias tracking rather than recursive
   inspection of one return value. RFC 0110 owns those larger lifetime
   problems; this RFC neither rejects all such result types nor claims to make
   them safe.
9. Task arguments/results and Channel elements can also retain a local-rooted
   View beyond the originating function. RFC 0118 owns those cross-task
   lifetime rules; this RFC does not claim to make those transfers safe.

## Mutation provenance

An inline aggregate can be constructed safely and later receive a local-rooted
View through member or Array-element assignment. Construction-only provenance
would miss that escape.

Binding provenance is place-sensitive. Assignment to a statically known object
member or constant Array index replaces that place's roots. Assignment through
a dynamic Array index conservatively joins the assigned roots into every
element the index may select. Replacing one known place does not preserve stale
roots from the value previously stored there.

This rule covers assignment as well as construction and avoids rejecting an
aggregate after its only unsafe component has been completely overwritten.

## Implementation plan

### Phase 1: provenance representation

1. Inventory checked expression forms that construct, select, normalize, or
   copy returnable inline aggregates, including object, ADT, union and Array
   construction, variable reads, `MatchExpression` arm results, and
   `TryExpression` success results.
2. Extend checked expression metadata so nested components preserve their View
   root sets and root kinds through object, ADT, union, Array, and binding-copy
   construction.
3. Reuse `ViewRoots`, `RootKind`, binding IDs, the current `selfID` exclusion,
   and the existing direct-return classification; do not add a separate
   analyzer pass. A variable read consumes provenance already carried by its
   binding rather than recursively resolving the binding initializer.
4. Extend binding flow state with place-sensitive nested provenance. A member
   or constant-index assignment replaces the selected place; a dynamic-index
   assignment joins into every possible element. Branch merging unions the
   roots of corresponding places.

### Phase 2: return checking

5. Add one recursive return-provenance walk beside `viewReturnDiagnostic` and
   invoke it whenever the checked return type can recursively contain a View,
   not only when the top-level result is itself `View<T>`.
6. Traverse only checked value components that may contain a View and guard
   recursive nominal shapes with a seen set. Union injection, match arms, and
   try normalization preserve the selected value's provenance.
7. Report one diagnostic at the return operand on the first proven local root.
8. Preserve the direct-View diagnostic and earliest diagnostic ownership.

### Phase 3: fail-closed generation

9. Extend checked-metadata validation for every new provenance-bearing node.
10. Reject missing or structurally inconsistent nested provenance as an
   `Unknown Error`; generation must not reconstruct borrow facts.

### Phase 4: tests and documentation

11. Replace the integration test that currently asserts nested escape compiles.
12. Add checker-unit coverage for place updates and branch merging, then add
    the exhaustive Validation cases below.
13. Update the View return contract in `docs/reference.md` after behavior is
    stable: remove objects, ADTs, unions, and Arrays from the depth limitation,
    distinguish borrowed/handle parameters from copied inline parameter
    storage, and retain the exact pointer, mutable-container, concurrency, and
    interprocedural limits.
14. Regenerate snippet hashes only if an existing accepted snippet legitimately
    changes output; a rejection-only change should move no accepted artifact.

## Validation

This section is exhaustive.

- Reject a local Array slice returned inside an object member.
- Reject it inside an ADT payload.
- Reject it inside a union payload.
- Reject a direct local-rooted View injected into a View-or-Nil union result.
- Reject it inside an Array element and through one intervening local binding.
- Reject an object member or Array element that receives a local-rooted View by
  assignment after the aggregate was constructed.
- Accept an object member or constant Array element after its local-rooted View
  has been completely overwritten by a parameter-rooted or empty View.
- Reject a returned Array after a dynamic-index assignment inserts a
  local-rooted View, because any element may contain that root.
- Reject an unsafe aggregate selected by a returned match arm or propagated as
  a returned try-success value.
- Accept the same aggregate shapes when their View is copied from a direct View
  parameter, including through one intervening local binding.
- Reject a returned View newly sliced from an Array parameter or an inline
  Array member of an object parameter, because the callee owns those copies.
- Accept a method result rooted in `self`-reached caller storage.
- Reject when merged control-flow provenance contains both a parameter root
  and a local root.
- Accept the same aggregate shapes when their View is empty.
- Accept the current `View.from_pointer` parameter-return cases.
- Preserve current List/Dict behavior pending RFC 0110; add no blanket
  result-type rejection for them.
- Preserve current pointer-stored aggregate behavior pending RFC 0110 and Task
  and Channel transfer behavior pending RFC 0118; add no blanket result-type
  rejection for those handles.
- Direct local-rooted View returns retain their existing diagnostic.
- No accepted snippet hash changes.
- `go test ./...` and `go vet ./...` pass.

## Non-goals

- Interprocedural provenance propagation, including a helper that accepts a
  View and returns it inside an aggregate.
- Mutable List/Dict element provenance and pointer-pointee alias tracking,
  including a heap-allocated aggregate that stores a local-rooted View.
- Task argument/result and Channel element lifetime analysis.
- Runtime borrow tracking.
- New ownership, lifetime, or reference syntax.
- Rejecting every value whose type can contain a View.
