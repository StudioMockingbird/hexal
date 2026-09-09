# RFC 0150: Slice Syntax — `[]T` / `[mut]T`

- Kind: Language Semantics
- Status: Superseded by RFC 0153; not implemented
- Created: 2026-09-08
- Updated: 2026-09-09
- Superseded because: this RFC's own Open Decision 1 asked to confirm bracket
  grammar over a nameable `Slice`/`MutSlice`-style generic pair, specifically
  to avoid a name that "invites treating it like one — adding methods, adding
  safety machinery, extending its provenance tracking." That tradeoff was
  revisited after further discussion of a fuller ownership/borrow model, and
  the decision landed the other way: named generic types (`View<T>`/
  `MutView<T>`), explicitly to have a name capable of carrying more
  capability later, not despite it. This document is kept for its
  Migration table (still the accurate destination list for every
  `View<T>`-dependent call site) and its reasoning about what bracket grammar
  would have avoided, which is exactly what RFC 0153 chose to accept.
- Origin: user request, following a comparison of how C, Zig, and Odin handle
  a byte-stream write's input type, and the position taken in that
  discussion: types describe structure, not lifetime
- Supersedes: RFC 0148 (`Span<T>`/`MutSpan<T>` as generic type constructors).
  Keeps 0148's naming lessons (`Reader`/`Writer` rejected as a name — collides
  with a future byte-stream abstraction; `Mut`-prefix marks the writable
  variant) but replaces both the generic-type-constructor shape and the
  exclusivity checker in 0148's Semantics 5 with dedicated grammar and no
  bolted-on lifetime analysis
- Retires: `View<T>` as a generic type constructor (an attempt to remove it
  outright, made and reverted earlier in this session, found it load-bearing
  for `Array`/`List.slice`, `String.bytes`/`.slice`/`.from_bytes`/`.from_runes`,
  and `IO.write`/`MutPtr<Bytes>.write`; this RFC is the replacement design
  those call sites move to — see Migration)
- Coordinates with: RFC 0137 (View return-safety). This RFC keeps only its
  cheapest, no-new-concept piece (Semantics 6) and drops the rest
- Does not own: RFC 0149's ownership/borrow model, left untouched as an open
  draft per an earlier, explicit decision in this session unrelated to this
  RFC
- Amended by: RFC 0151, which removes the `Array<T, N>` spelling and reserves
  `[N]T` for the same fixed inline semantics

## Summary

Replace `View<T>` — a generic type constructor with the same shape as
`List<T>` — with dedicated slice grammar: `[]T` (read-only) and
`[mut]T` (mutable), following Zig's `[]const T`/`[]T` and Odin's `[]T`. A
slice is `{pointer, length}` either way; the bracket form is a first-class
type-expression grammar production, not a name the checker resolves through
ordinary generic-type lookup the way `View<T>` was.

This RFC also settles the design question that produced it: **a type
describes structure — its representation and the capabilities that follow
from it — not how long a value lives.** RFC 0148's exclusivity checker
(rejecting even two disjoint, memory-safe `slice_mut` sub-ranges to keep the
analysis tractable) does not carry over, and neither does RFC 0137's
nested-aggregate escape walk. What does carry over, because it costs nothing
new: the direct local-root return check `Ptr`/`MutPtr` already has from
earlier this session (`ptrReturnDiagnostic` in `compiler/checker/views.go`),
extended to slices for the same reason it was worth adding to `Ptr` — reusing
existing provenance tracking to reject a provably dangling return is not "a
lifetime system," it is the same cheap, no-new-concept check applied one more
place. Nothing past that direct case is attempted; a slice returned inside a
struct, stored in a `List`, or aliased across two live bindings is exactly as
uncheckable as a raw pointer is today, which is the explicit point: this
matches Zig and Odin, both of which have first-class slices and neither of
which does any compile-time escape analysis on them at all.

## Naming and syntax

```text
[]T       read-only slice: non-owning, non-null, contiguous, {pointer, length}
[mut]T    mutable slice: the same shape, writable through the pointer
```

- The bare bracket form is read-only, mirroring `Ptr<T>` being the plain,
  default-safe name; `mut` inside the brackets marks the writable variant,
  mirroring `MutPtr<T>`'s prefix. This is a deliberate inversion of Zig's
  convention, where the *unmarked* `[]T` is mutable and `const` marks the
  safe one — Hexal already made the opposite default with `Ptr`/`MutPtr`, and
  this RFC keeps that precedent rather than importing Zig's.
  `[mut]T` was chosen over `[]mut T` or `mut []T` because Zig places its
  modifier immediately inside/adjacent to the empty brackets (`[]const T`),
  and keeping the modifier inside the brackets means the parser resolves
  mutability before it starts parsing the element type, rather than needing a
  second modifier position outside the brackets to disambiguate.
- RFC 0151 introduces `[N]T` fixed-array syntax. The token after `[`
  distinguishes `[]T`, `[mut]T`, and `[N]T`; this RFC owns only the unsized
  slice forms.
- `[]T`/`[mut]T` are type-expression grammar, parsed wherever a type is
  expected (declarations, parameters, results, generic arguments), the same
  position `List<T>` or `Ptr<T>` occupies today. They are not identifiers:
  there is no bare `Slice` or `View` name to look up, no way to write
  `[]T.something(...)` the way `View<T>.from_pointer(...)` worked, because
  `[]T` is not a value the checker resolves through name lookup at all — see
  Construction below for how the operations that used to hang off `View` as a
  type name are re-homed.

## Problem

`View<T>` is a generic type constructor — a name (`View`) plus a type
argument, resolved through the same lookup path as `List<T>` or `Dict<K,V>`.
But a slice isn't a container with construction/growth/ownership semantics
the way `List` is; it's a bare pointer-length pair, structurally closer to
`Ptr<T>` than to any owning collection. Spelling it as an ordinary generic
type invites treating it like one — adding methods, adding safety
machinery, extending its provenance tracking — the way RFC 0148 did with
`MutSpan<T>`'s exclusivity checker. Zig and Odin both avoid this by making
the slice a grammar primitive instead of a library type: there is no `Slice`
name to attach anything to, so a slice stays exactly what its representation
says it is.

## Semantics

1. **Grammar.** `[]T` and `[mut]T` are parsed as type expressions, T
   following the same inline-element eligibility `View<T>` used
   (`Eligible(T, PositionSliceElement)`, renamed from `PositionViewElement`).
   Slices of slices, and slices of other managed/owning values (`String`,
   `List`, `Dict`), are rejected — unchanged from `View<T>`'s existing rule.

2. **Representation.** `[]T` lowers to `{ const T* data; size_t length; }`;
   `[mut]T` lowers to `{ T* data; size_t length; }`. One monomorphized C
   struct per instantiated T either way — this is unchanged from `View<T>`'s
   generated shape; only the frontend spelling changes; `hex_view_*` C
   identifiers become `hex_slice_*`/`hex_mut_slice_*`.

3. **Construction.**
   - `[N]T.slice(start: Integer, end: Integer) -> []T` (unchanged
     signature, new result spelling).
   - `[N]T.slice_mut(start: Integer, end: Integer) -> [mut]T`, valid
     only on a `mut` receiver — new, filling the gap RFC 0148 identified.
   - `List<T>.slice`/`.slice_mut` mirror Array's, same mutability gating.
   - `Ptr<T>.to_slice(length: Size) -> []T` and
     `MutPtr<T>.to_slice(length: Size) -> [mut]T` replace
     `View<T>.from_pointer(pointer, length)`. Making this a method on the
     pointer (the receiver) rather than a static call against a bare type
     name is what `[]T` not being a lookup-able identifier requires, and
     reads at least as naturally: "this pointer, extended to `length`
     elements." Same preconditions as `from_pointer` carried: the pointer
     must be statically non-null, and `fromPointerRefTrace`-style rejection
     of a pointer that traces to a local `ref` within the same function
     applies unchanged (renamed, not removed — this is exactly the
     no-new-concept direct-root check this RFC keeps).
   - An empty slice is the literal `[]`, contextually typed to `[]T` or
     `[mut]T` the way an empty context already disambiguates it from an
     `Array<T,N>` literal (which requires exactly N elements, N positive, so
     `[]` is never a valid Array literal and is unambiguous). Replaces
     `View<T>.empty()`.

4. **Indexing and re-slicing.**
   - `[]T[index: Integer] -> read-only-place<T>`; `[mut]T[index: Integer] ->
     place<T>`.
   - `[]T.slice(start, end) -> []T`; `[mut]T.slice(start, end) -> [mut]T`
     (re-slicing preserves the receiver's own mutability — no `slice_mut`
     needed here, unlike fixed-array/List construction methods, because the
     dispatch is on the slice value's own type, not on a separate binding
     mutability signal).
   - `[]T.length() -> Size`; same for `[mut]T`.

5. **Weakening.** `[mut]T` weakens implicitly to `[]T` at the outermost layer
   only, no upgrade, no nested weakening — the same rule already stated for
   `MutPtr<T>` → `Ptr<T>`.

6. **Return safety (kept, narrowly).** Extend the existing
   `ptrReturnDiagnostic`/`findLocalViewRoot` machinery (already generalized
   once, from `View` to `Ptr`/`MutPtr`, earlier this session) to also cover
   `[]T`/`[mut]T` directly-returned values: a slice built from `ref`-adjacent
   local storage within the same function and returned directly is rejected,
   using the exact existing root-tracking fields (renamed from `ViewRoots` to
   something that no longer implies View, e.g. `BorrowRoots`, since neither
   `View` nor a View-specific concept remains — a mechanical rename, not a
   new mechanism). This is the only safety net a slice gets. It is not
   sound: a slice returned inside an object/ADT/Array, stored in a `List`, or
   passed across a `Task` boundary is not tracked, matching exactly how
   `Ptr<T>` is not tracked in those positions today.

7. **Equality, printing, storability.** Restated for the new spelling, rules
   unchanged from `View<T>`: length-then-elements equality; printable when
   the element is; general storability positions (binding, object member,
  ADT payload, fixed-array element, List element, Dict value, function
   parameter/result, Task argument/result, Channel element) match `View<T>`'s
   current position matrix exactly.

## Migration

These are the call sites RFC 148/149's discovery pass found depending on
`View<T>`; this RFC gives each one a destination instead of leaving it broken:

```text
[N]T.slice(start, end)            -> []T          (was View<T>)
[N]T.slice_mut(start, end)        -> [mut]T        (new)
List<T>.slice / .slice_mut                         (same pattern)
String.bytes()                    -> []Byte        (was View<Byte>)
String.slice(start, end)          -> []Byte        (was View<Byte>)
String.from_bytes(heap, bytes: []Byte)             (was View<Byte> parameter)
String.from_runes(heap, runes: []Rune)             (was View<Rune> parameter)
IO.write(from: []Byte)                             (was View<Byte> parameter)
MutPtr<Bytes>.write(from: []Byte)                  (was View<Byte> parameter)
```

No signature changes direction or capability here — every one of these kept
exactly the mutability it had under `View<T>` (all read-only; nothing in the
current call-site list needed `[mut]T`). `IO.write`/`MutPtr<Bytes>.write`
keep taking a read-only slice, unaffected by anything else in this RFC.

## Non-goals

- RFC 0148's exclusivity/liveness checker. No construction-time rejection for
  two live `[mut]T` values, disjoint or not, over the same storage.
- RFC 0137's nested-aggregate return walk. A slice hidden inside a returned
  object/ADT/union/fixed array is not tracked, same as `Ptr<T>` today.
- Any List/Dict-mediated, pointer-pointee-mediated, interprocedural, or
  cross-Task escape tracking — matches `Ptr<T>`'s current (non-)coverage
  exactly, not RFC 0110/0118's eventual scope.
- Fixed-array semantics beyond reserving `[N]T`; RFC 0151 owns that migration.
- Runtime borrow tracking, new lifetime/borrow keywords, or anything from RFC
  0149's model.
- Reopening RFC 0149's status; it stays an open, undecided draft.

## Open decisions

1. Confirm `[]T` / `[mut]T` as the concrete spelling over alternatives
   (`[]T`/`[]mut T`, or keeping a `Slice`/`MutSlice` generic-name pair and
   only dropping the exclusivity checker while keeping 0148's
   type-constructor shape). This RFC's recommendation is the bracket grammar
   specifically because a nameable generic type invites exactly the kind of
   accreted machinery (0148's checker) this RFC is trying to foreclose.
2. Confirm `Ptr<T>.to_slice`/`MutPtr<T>.to_slice` as the pointer-to-slice
   bridge, replacing `View<T>.from_pointer`.
3. Confirm the `BorrowRoots`-style rename of `ViewRoots`/`RootKind` (or leave
   the existing names as internal-only identifiers — they are not
   user-visible either way, so this is a code-clarity question, not a
   language design one).
