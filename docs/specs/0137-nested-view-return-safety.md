# RFC 0137: Nested View Return Safety

- Kind: Language Semantics
- Status: Implementation-ready; implementation not started
- Created: 2026-08-27
- Origin: RFC 0103 finding F19
- Restores: the language goal that locally decidable dangling borrows are
  rejected
- Coordinates with: RFC 0110 (mutable owning-container alias and lifetime
  rules), `docs/reference.md`
- Does not own: RFC 0103 finding F43's interprocedural caller provenance for
  `View.from_pointer`, or Views inserted into mutable List/Dict storage

## Summary

Reject a returned value when any nested `View` in that value is proven to
borrow storage local to the returning function.

The checker currently rejects a directly returned local-rooted `View`, but the
same View escapes when wrapped in an object, ADT, union, Array, or another
returnable aggregate. This is a current memory-safety bug.

## Current failure

```hexal
type Window = { visible: View<Int32> }

fun bad(): Window do
    fixed: Array<Int32, 4> := [1, 2, 3, 4]
    return Window { visible = fixed.slice(0, 2) }
end
```

The program currently compiles. `visible.data` points into `fixed`, whose
storage ends when `bad` returns.

## Semantics

1. Return safety follows the checked inline value, not merely its declared
   type.
2. The checker recursively inspects every inline value component that can
   contain a `View`: object members, ADT payloads, union payloads, Array
   elements, and values copied through local bindings.
3. A nested View is rejected when its root chain contains a non-parameter
   binding local to the returning function.
4. Empty Views, parameter-rooted Views, parameter-reached member storage, and
   `View.from_pointer` regions remain returnable under the current direct
   provenance contract.
5. A type containing `View` is not rejected by itself. The checker rejects
   only a value whose tracked provenance proves a local root.
6. Unknown or interprocedural provenance retains the current behavior. This
   RFC adds no whole-program borrow analysis and no conservative rejection of
   opaque calls.
7. The diagnostic is:
   `a returned value contains a View that borrows a local of this function`.
   A directly returned View keeps its existing diagnostic.
8. List and Dict are mutable owning handles. Proving which Views their storage
   contains requires mutation and alias tracking rather than recursive
   inspection of one return expression. RFC 0110 owns that larger lifetime
   problem; this RFC neither rejects all such result types nor claims to make
   them safe.

## Implementation plan

### Phase 1: provenance representation

1. Inventory checked expression forms that construct or copy returnable inline
   aggregates.
2. Extend checked expression metadata so nested components preserve their View
   root sets and root kinds through object, ADT, union, Array, and binding-copy
   construction.
3. Reuse `ViewRoots`, `RootKind`, binding IDs, and the existing direct-return
   classification; do not add a separate analyzer pass.

### Phase 2: return checking

4. Add one recursive return-provenance walk beside `viewReturnDiagnostic`.
5. Traverse only checked value components that may contain a View and guard
   recursive nominal shapes with a seen set.
6. Report one diagnostic at the return operand on the first proven local root.
7. Preserve the direct-View diagnostic and earliest diagnostic ownership.

### Phase 3: fail-closed generation

8. Extend checked-metadata validation for every new provenance-bearing node.
9. Reject missing or structurally inconsistent nested provenance as an
   `Unknown Error`; generation must not reconstruct borrow facts.

### Phase 4: tests and documentation

10. Replace the integration test that currently asserts nested escape compiles.
11. Add the exhaustive Validation cases below.
12. Update the View return contract in `docs/reference.md` after behavior is
    stable: remove objects, ADTs, unions, and Arrays from the depth limitation,
    while retaining the exact mutable List/Dict and interprocedural limits.
13. Regenerate snippet hashes only if an existing accepted snippet legitimately
    changes output; a rejection-only change should move no accepted artifact.

## Validation

This section is exhaustive.

- Reject a local Array slice returned inside an object member.
- Reject it inside an ADT payload.
- Reject it inside a union payload.
- Reject it inside an Array element and through one intervening local binding.
- Accept the same aggregate shapes when their View is rooted in a parameter.
- Accept the same aggregate shapes when their View is empty.
- Accept the current `View.from_pointer` parameter-return cases.
- Preserve current List/Dict behavior pending RFC 0110; add no blanket
  result-type rejection for them.
- Direct local-rooted View returns retain their existing diagnostic.
- No accepted snippet hash changes.
- `go test ./...` and `go vet ./...` pass.

## Non-goals

- Interprocedural provenance propagation.
- Mutable List/Dict element provenance and alias tracking.
- Runtime borrow tracking.
- New ownership, lifetime, or reference syntax.
- Rejecting every value whose type can contain a View.
