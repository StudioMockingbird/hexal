# RFC 0156: Fenced Pointer Arithmetic and Foreign-Owned Values

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion (proposal); not scheduled. Promote to
  Implementation-ready only after RFC 0155 lands and RFC 0039's scope is
  settled
- Created: 2026-09-10
- Updated: 2026-09-10
- Depends on: RFC 0154 (`Ptr<T>` / `Ptr<mut T>` spelling), RFC 0155
  (`unsafe do ... end` lexical permission)
- Coordinates with: RFC 0039 (foreign ownership and deallocator contracts),
  RFC 0110 (escape and invalidation rules for owner-rooted pointers)
- Does not update `docs/reference.md`: synchronize only after implementation
  is approved and behavior stabilizes

## Summary

No systems language can keep allocator authorship to itself forever: bump
allocators, slab allocators, ring buffers, and wire-format parsers all need
address arithmetic. This proposal fences that power instead of deleting it:

1. `Ptr<mut T>` (and read-only `Ptr<T>`) gains offset and indexed access as
   unsafe-capable operations, usable only inside `unsafe do ... end`.
2. At minimum, an `extern c` declaration can name the C deallocator for a
   foreign-owned value, so a C-built allocator can own Hexal values without
   the default backend freeing (or leaking) them.

Neither part introduces user-implementable Hexal allocators (a Zig-style
allocator interface remains future work); together they make Hexal complete
for memory *infrastructure* written in C and consumed from Hexal, while safe
Hexal keeps today's arithmetic-free guarantees.

## Problem

Safe Hexal has no address arithmetic on any type (reference Excluded
features; RFC 0149 and RFC 0153 non-goals; RFC 0155 adds permission but no
operations). Consequences:

- A bump allocator over a slab, a pool with computed slots, or a parser over
  a byte buffer cannot be written in Hexal at any safety level — not even
  inside `unsafe`, because the operations do not exist.
- A value allocated by a C allocator (arena, pool, mmap-backed region) has no
  Hexal spelling for its ownership: the default backend would free foreign
  storage, or the value leaks by design. RFC 0110's foreign transfer rules
  assume RFC 0039 metadata that names no concrete deallocator form.

## Goals

- Make `unsafe do ... end` sufficient to write real allocator and parser code:
  offset, indexed read/write, and one-past-end pointers over `Ptr<T>`.
- Keep every arithmetic operation unsafe-capable, so safe code is provably
  arithmetic-free by construction.
- Give foreign-owned values a minimum viable contract: a named deallocator
  that both explicit cleanup and automatic drop honor.
- Add no operators: Hexal excludes operator overloading, so the surface is
  compiler-owned methods, not `ptr + n`.

## Non-goals

- A user-implementable Hexal allocator interface (vtable, context passing).
- Pointer casts, `bit_cast` on pointers, integer-to-pointer conversion, or
  pointer-to-integer exposure beyond what C interop already describes.
- Bounds-checked arithmetic: the fenced operations are deliberately raw. Any
  checked alternative (slices already cover the checked range case) is a
  separate proposal.
- Settling RFC 0039's full ownership metadata; this RFC proposes only the
  deallocator minimum it is blocked on.

## Proposed design

### Arithmetic operations

```text
Ptr<T>.offset(count: Size) -> Ptr<T>
Ptr<mut T>.offset(count: Size) -> Ptr<mut T>
Ptr<T>[index: Size] -> read-only-place<T>
Ptr<mut T>[index: Size] -> writable-place<T>
```

- Both operations are unsafe-capable: outside `unsafe do ... end` the checker
  rejects them naming the missing lexical permission (RFC 0155 diagnostic).
- `offset` preserves the receiver's access mode; there is no upgrade from
  `Ptr<T>` to `Ptr<mut T>`.
- Results are ordinary storable, copyable raw pointers. Escape, retention,
  and lifetime of an arithmetic pointer are the programmer's assertion under
  RFC 0155's undefined-behavior statement; the checker tracks nothing further.
- Producing a one-past-end pointer is valid; dereferencing it is an
  unsafe-precondition violation, not a trap. Offset arithmetic itself never
  traps on overflow: wraparound is part of the asserted-raw contract, matching
  the C target rather than Hexal's checked arithmetic.
- Null-pointer offset and dereference remain ordinary invalid operations
  diagnosed before any unsafe permission is consulted.

### Foreign deallocator minimum

An `extern c` value-returning declaration may name its deallocator:

```text
extern c arena_alloc(size: Size) -> Ptr<mut Byte> frees_with arena_free
```

- The spelled deallocator is part of the declaration's contract, checked at
  the foreign boundary like any other signature fact.
- A value carrying a foreign deallocator is still an affine owner for move
  purposes, but its cleanup — explicit early cleanup and RFC 0110 automatic
  drop alike — routes to the named deallocator instead of the default
  backend. Routing to only one of the two would either double-free or leak,
  so the contract covers both or the declaration is rejected.
- Passing a Hexal owner to a foreign consumer continues to follow RFC 0110's
  transfer rules; this proposal adds no new transfer syntax.

## Diagnostics

- Arithmetic outside `unsafe do ... end` reports the missing lexical
  permission and names the operation.
- Dereference of a null or provably-invalid arithmetic pointer reports the
  ordinary invalid-operation diagnostic; unsafe permission never downgrades it.
- A foreign declaration naming an unknown deallocator, or a deallocator whose
  signature does not consume one pointer, is an ABI Error at the declaration.
- A foreign-owned value reaching a cleanup path with no routed deallocator is
  a compiler error, never a silent default-backend free.

## Acceptance sketch

Non-exhaustive: the implementing RFC promotes this to an exhaustive
Validation section.

- Offset/index on both pointer modes succeed inside `unsafe do ... end` and
  fail outside it with the permission diagnostic.
- Read-only pointers never yield writable places through either operation.
- Generated C for the operations is plain pointer arithmetic with source
  mapping; no runtime provenance metadata is emitted.
- Foreign-owned values route both explicit and automatic cleanup to the named
  deallocator; missing or mismatched deallocators fail at the declaration.
- Existing manifest hashes outside deliberate additions do not move.

## Open questions

1. Spelling: `offset(count)` method versus an `offset_by`/`advance` name — and
   whether indexing should be spelled `ptr[index]` (proposed) or a named
   `at(index)` method for visual distinction from Slice indexing.
2. Whether `offset` on a null pointer should trap (fail-closed) or join the
   asserted-raw contract (C-identical). The proposal currently traps nothing;
   the alternative costs one branch per offset.
3. The exact `extern c` deallocator attachment syntax; `frees_with` above is a
   placeholder, not a decision.
4. Whether a foreign-owned value may be moved into aggregates and collections
   in v1, or is restricted to bindings and returns until its drop plan is
   proven for recursive cleanup.
