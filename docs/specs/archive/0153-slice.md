# RFC 0153: Scoped `Slice<T>` and `Slice<mut T>`

- Kind: Language Semantics
- Status: Superseded by RFC 0161 on its implementation, 2026-09-10. RFC 0161 adopts
  the Slice naming and writable mode with first-class copyable descriptors and explicitly
  rejects this RFC's call/lexical-scoped placement and lifetime model. Do not implement.
- Created: 2026-09-09
- Updated: 2026-09-10
- Depends on: RFC 0154's `<T>` / `<mut T>` capability syntax and RFC 0149's
  scoped capability and canonical-place machinery
- Supersedes: RFC 0150's proposed bracket slice syntax and the shipped
  first-class `View<T>` surface
- Coordinates with: RFC 0110 (affine owners) and RFC 0155 (unsafe boundaries)

## Summary

Use one call- or lexical-block-scoped capability family for borrowed contiguous
ranges:

```text
Slice<T>       non-owning, non-nullable read-only contiguous-range capability
Slice<mut T>   non-owning, non-nullable exclusive writable-range capability
```

`Ref<T>` borrows one T place. `Slice<T>` borrows zero or more contiguous T
places. Both are parameter or lexical-block capabilities rather than storable
values. These syntactic lifetimes avoid stored descriptors, returned borrows,
owner-version metadata, and general lifetime inference.

Non-nullable means the Slice capability is never `Nil`. Its empty
representation may contain a null data pointer paired with zero length; no
element access may observe or dereference that pointer.

The shipped `View<T>` name and its first-class storage/return behavior are
removed rather than retained as aliases.

## Syntax and placement

```ebnf
slice-type = "Slice" , "<" , [ "mut" ] , type-expression , ">" ;
```

Slice is valid only as a direct function parameter, the implicit receiver of
its compiler-owned operations, or the bound capability of RFC 0149's
`borrow ... do ... end` statement. It is invalid as a result, ordinary local or
root binding, member, payload, union member, Array/List/Dict element, Box
argument, pointer pointee, function-value result, Task/Channel value, or nested
Slice argument. A Slice parameter mode may appear in an exact `Fun` signature:
the function value stores code, not a Slice capability, and each invocation
creates a fresh call-scoped capability. There is no Slice literal.

A Slice-producing expression is valid only as a direct argument to a compatible
Slice parameter or as the source of a lexical borrow block:

```hexal
fun consume(values: Slice<Int32>) do
    print(values[0])
end

consume(values.slice(0, 2))

borrow window from values.slice(0, 2) do
    print(window[0])
end
```

It cannot initialize an ordinary binding or be returned:

```hexal
window := values.slice(0, 2) // Type Error
return values.slice(0, 2)    // Type Error
```

Passing a Slice parameter or lexical Slice binding to another compatible Slice
parameter reborrows the same root for the nested call. A call capability ends
when that dynamic call returns, including through `try`; a lexical capability
ends on every control-flow exit from its borrow block.

## Element eligibility

- T may be any complete finite storable type available at implementation,
  including objects, ADTs, unions, String, List, Dict, and nominal aggregates.
- `Slice<Slice<T>>`, `Slice<Slice<mut T>>`, and their mutable outer forms are
  invalid. A Slice cannot contain another Slice descriptor.
- Atomic and Unknown remain invalid where their existing position contracts
  reject them.
- Later owning types such as Box become eligible only when their owning RFC
  adds and validates the corresponding Slice position.
- Element ownership never transfers through Slice. After RFC 0110, slicing an
  affine T borrows the owners rather than copying or moving them.

## Representation

```c
/* Slice<T> */
struct { const T *data; size_t length; }

/* Slice<mut T> */
struct { T *data; size_t length; }
```

- One monomorphized C struct is emitted per concrete T and access mode.
- Generated families are named `hex_slice_*` and `hex_mut_slice_*`.
- Slice itself has no Nil value. Its data pointer is null exactly for an empty
  Slice and its length is then zero; every non-empty Slice has non-null data.
- No owner pointer, reference count, generation, version, or runtime borrow
  state is added.

## Construction

```text
Array<T,N>.slice(start: Integer, end: Integer) -> ephemeral Slice<T>
Array<T,N>.mut_slice(start: Integer, end: Integer) -> ephemeral Slice<mut T>
List<T>.slice(start: Integer, end: Integer) -> ephemeral Slice<T>
List<T>.mut_slice(start: Integer, end: Integer) -> ephemeral Slice<mut T>
String.bytes() -> ephemeral Slice<Byte>
String.slice(start: Integer, end: Integer) -> ephemeral Slice<Byte>
Slice<T>.from_pointer(pointer: Ptr<T> | Ptr<mut T>, length: Size)
    -> ephemeral Slice<T>
Slice<mut T>.from_pointer(pointer: Ptr<mut T>, length: Size)
    -> ephemeral Slice<mut T>
Slice<T>.empty() -> ephemeral Slice<T>
Slice<mut T>.empty() -> ephemeral Slice<mut T>
```

- Integer is the existing signature metavariable for any Hexal integer.
  Bounds normalize to Size, are checked, and use half-open `[start, end)`.
- Array mutable slicing requires a writable Array place.
- List mutable slicing requires no `mut` binding solely to mutate backing
  storage; a fixed List handle already permits interior element mutation.
- Every constructor evaluates its operands once, left to right.
- `from_pointer` accepts only the exact listed non-null pointer capability.
  Construction from a pointer whose live provenance the checker cannot prove
  is an unsafe-capable operation and requires RFC 0155's lexical permission.
  A pointer to a currently live local place is locally proved provenance and
  may construct a Slice in safe code when the Slice remains confined to its
  permitted dynamic call.
  A pointer traced to local storage cannot be used to evade the direct-call or
  lexical-block lifetime.
- `empty()` and an empty slice of an empty List use null data plus zero length.

## Operations

```text
Slice<T>[index: Integer] -> read-only-place<T>
Slice<mut T>[index: Integer] -> writable-place<T>
Slice<T>.slice(start: Integer, end: Integer) -> ephemeral Slice<T>
Slice<mut T>.slice(start: Integer, end: Integer) -> ephemeral Slice<mut T>
Slice<T>.length() -> Size
Slice<mut T>.length() -> Size
```

- Indexing is bounds-checked.
- Re-slicing cannot widen the range and preserves access mode.
- `Slice<mut T>` weakens implicitly to `Slice<T>` at the outermost layer only.
  There is no upgrade or nested weakening.
- Slice never transfers element ownership. RFC 0110 owns the later rules for
  reading, replacing, or extracting affine elements through these places.
- A Slice parameter or lexical Slice binding is iterable with the existing one-
  or two-binder sequence rules while T is copyable. RFC 0110 owns affine-element
  iteration.

## Exclusivity

All argument expressions evaluate once from left to right. RFC 0149's shared
Ref/Slice engine then validates the call's simultaneous capabilities together
with capabilities active in every enclosing dynamic call:

- read-only Ref/Slice capabilities with the same root may coexist;
- any same-root set containing `Ref<mut T>` or `Slice<mut T>` is rejected, even
  when numeric bounds appear disjoint;
- distinct sibling field roots may coexist; and
- a move, cleanup, reset, reallocation, writable Ref, or other invalidation of
  a sliced root cannot occur in the same call.

No numeric interval proof is performed. Rejecting every same-root mutable pair
keeps exclusivity a root/place decision rather than introducing range analysis.
List reallocation needs no stored-Slice rule because no Slice survives its
dynamic call or lexical borrow block. Structural invalidation of the root is
rejected while either capability is active.

## API migration

```text
View<T>                                      -> Slice<T>
Array<T,N>.slice(...) -> View<T>             -> ephemeral Slice<T>
String.bytes() -> View<Byte>                 -> ephemeral Slice<Byte>
String.slice(...) -> View<Byte>              -> ephemeral Slice<Byte>
String.from_bytes(heap, View<Byte>)           -> Slice<Byte> parameter
String.from_runes(heap, View<Rune>)           -> Slice<Rune> parameter
IO.write(from: View<Byte>)                    -> Slice<Byte> parameter
MutPtr<Bytes>.write(from: View<Byte>)         -> Bytes.write(from: Slice<Byte>)
```

`View`, `Borrow`, and their mutable variants are not retained as aliases. The
existing direct and nested View-return provenance machinery becomes dead when
Slice results and storage are rejected and must be removed in this migration.

## Diagnostics

The earliest proving checker operation diagnoses:

- invalid Slice arity, element type, nesting, or placement;
- use of a removed View/Borrow spelling;
- a Slice-producing expression outside a direct compatible call argument or
  lexical borrow source;
- invalid construction pointer capability, bounds, or mutable source place;
- mutation through `Slice<T>` or invalid affine-element replacement/extraction;
- overlapping same-root capabilities involving `Slice<mut T>`;
- root invalidation in the same call; and
- unknown pointer provenance used outside an unsafe block.

Diagnostics use source-level Slice spellings and places, never generated C
family names or internal capability states.

## Reference synchronization

With explicit user approval after behavior stabilizes, replace View grammar,
placement, APIs, provenance, and generated-C names in `docs/reference.md` with
Slice's direct-call and lexical-block capability contracts. Record the
non-nullable-capability/null-empty-representation distinction, combined
Ref/Slice exclusivity, exact `Fun` parameter use, from-pointer unsafe gate, and
removal of every storable/returnable borrow position.

## Non-goals

- General lifetime parameters or stored borrows.
- Ordinary Slice bindings, members, collection elements, or results; the
  lexical borrow binding is the sole naming exception.
- Numeric disjoint-range reasoning.
- Nested slices or cross-Task borrowing.
- Runtime borrow/version tracking.
- Pointer arithmetic, one-past pointers, or unchecked indexing.

## Required sweep

Inventory and update:

- parser/type lookup for View/Slice and `<mut T>`;
- `ViewType`, eligibility, interning, display, and C names;
- parameter/result/function-value and storage position checks;
- Array/List slicing, String/IO/Bytes signatures, and bounds checks;
- call capability roots and simultaneous overlap validation shared with RFC
  0149;
- removal of direct/nested returned-View provenance and stored-View List
  invalidation assumptions;
- every test, snippet, diagnostic, component filename, include, and manifest
  entry containing `View`, `hex_view`, `Borrow`, or `hex_borrow`.

## Validation

This section is exhaustive.

- Both Slice modes accept exactly one valid T; every nested Slice fails.
- Slice is accepted only as a direct function parameter, compiler-owned Slice
  receiver, lexical borrow binding, or parameter mode in an exact `Fun`
  signature; every result, ordinary binding, aggregate, collection, pointer,
  function-value result, Task, and Channel position fails.
- Every Slice-producing expression succeeds only as a direct compatible call
  argument or lexical borrow source; ordinary naming, storing, or returning it
  fails.
- Exact `Fun` parameter signatures may contain Slice modes; Fun results and
  every stored Slice capability remain invalid.
- Read-only Array/List/String slicing preserves contents and checked bounds.
- Mutable Array/List slicing permits indexed mutation during the call or lexical
  borrow block.
- Two same-root read-only arguments succeed; every same-root argument set
  containing mutable Slice fails, including statically disjoint ranges.
- Same-root Ref/Slice combinations follow the identical rule: all-read-only
  succeeds and any mutable capability fails.
- Distinct sibling roots succeed when their place identities do not overlap.
- Reborrowing a Slice parameter into a nested call succeeds without escape.
- A lexical Slice binding supports indexing, length, reslicing, iteration, and
  compatible nested calls, and becomes unavailable on every block exit.
- Nested calls are checked against every Ref/Slice capability active in their
  enclosing calls.
- Mutable-to-read-only weakening succeeds only at the outermost layer.
- Known live raw-pointer construction, including `ref` of a live scalar local,
  succeeds in safe code when confined to a direct call or lexical borrow block.
  Unknown provenance fails outside and succeeds inside `unsafe do ... end`; a
  local address cannot use `from_pointer` to escape either scope.
- Empty Slice uses null data and zero length; non-empty Slice uses non-null data.
- Generated C has exactly the two pointer-plus-length layouts and no runtime
  ownership, lifetime, version, or borrow metadata.
- The obsolete returned-View provenance machinery is removed.
- `View`, `Borrow`, `MutView`, `MutBorrow`, `hex_view`, and `hex_borrow` remain
  nowhere in active syntax, diagnostics, snippets, or generated artifacts.
- Existing manifest hashes outside deliberate naming, signature, placement,
  and mutable-Slice changes do not move.

## Detailed implementation plan

1. Land RFC 0154's shared capability spelling and implement this RFC together
   with RFC 0149's call-capability engine.
2. Replace View identity and generated families with Slice; migrate every
   read-only API and reject all formerly storable/returnable positions.
3. Integrate RFC 0149's lexical borrow statement as the only named Slice scope;
   support indexing, length, reslicing, iteration, nested calls, and balanced
   capability release on every block exit.
4. Add mutable Array/List construction, mutable from-pointer construction,
   writable indexing, re-slicing, and outermost weakening.
5. Reuse RFC 0149 canonical roots, the combined Ref/Slice capability set, and
   enclosing-call validation; reject all same-root mutable combinations without
   interval analysis.
6. Remove returned/stored View provenance machinery made unreachable by the
   placement rule.
7. Coordinate RFC 0110's RuneCursor/Bytes lexical descriptors and affine
   iteration with the same block/capability machinery.
8. Implement every Validation item with focused stage, integration, generated-C
   text, and qualified C23 tests.
9. Update snippets and rebuild the manifest only for intentional artifacts.
10. After behavior stabilizes, synchronize `docs/reference.md` only with explicit
   user approval; then update status, rebuild/restart the workbench, and close
   only when all artifacts agree.

## Open questions

None.
