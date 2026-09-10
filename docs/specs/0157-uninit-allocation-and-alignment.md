# RFC 0157: Explicit Uninitialized Allocation and Alignment Control

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion (proposal); not scheduled. Promote to
  Implementation-ready only after RFC 0155 lands and RFC 0110's drop plans
  are settled
- Created: 2026-09-10
- Updated: 2026-09-10
- Depends on: RFC 0155 (`unsafe do ... end` lexical permission), RFC 0110
  (automatic drop plans whose preconditions this RFC extends)
- Coordinates with: RFC 0156 (fenced arithmetic over the buffers allocated
  here), RFC 0027 (Stash/Pool element policy)
- Does not update `docs/reference.md`: synchronize only after implementation
  is approved and behavior stabilizes

## Summary

Hexal always initializes: `Heap.allocate<T>(initial)` takes a complete value,
so every allocation pays initialization even for buffers the program fills
immediately after (frame buffers, DMA descriptors, scratch scratch). This
proposal keeps initialized allocation the default and adds one explicit fast
path plus alignment control:

```text
Heap.allocate_uninit<T>() -> Ptr<mut T>
Heap.allocate_aligned<T>(initial: T, alignment: Size) -> Ptr<mut T>
Heap.allocate_uninit_aligned<T>(alignment: Size) -> Ptr<mut T>
```

Uninitialized allocation is unsafe-capable: the programmer asserts
write-before-read, write-before-move, and write-before-drop for every
readable or droppable part, and violation is undefined behavior per RFC
0155. Alignment is an ordinary checked operation: zero or non-power-of-two
alignment traps.

## Problem

- Bulk buffers pay double-touch: allocate-with-dummy plus an immediate
  overwriting fill. C's `malloc`-then-fill has no Hexal equivalent, and
  `List<Byte>` growth is not a substitute for fixed raw buffers.
- Over-aligned storage (SIMD scratch, page-aligned DMA, cache-line padding)
  has no spelling: allocation derives alignment solely from T, with no
  parameter anywhere allocation takes effect.
- A raw-bytes alternative (`malloc(size)` returning `Ptr<mut Byte>`) would be
  unusable without pointer casts, which Hexal excludes. Typed per-T uninit
  allocation is therefore the only coherent shape: the pointer already knows
  T, so only arithmetic (RFC 0156) is needed to fill it.

## Goals

- Provide the fast path without changing the safe default: every existing
  allocation form keeps its initialization guarantee untouched.
- Make uninit misuse fail loudly in development: debug builds fill fresh
  uninit storage with a recognizable byte so read-before-write traps or
  visibly corrupts under the existing trap contract rather than silently
  succeeding.
- Fail closed on alignment: representable power-of-two alignments only, with
  a runtime trap otherwise — alignment is checkable, so unlike init order it
  stays out of `unsafe`.
- Keep the surface to three compiler-owned canonical constructors; no syntax,
  no attributes, no type-level alignment annotations in v1.

## Non-goals

- Flow-based definite-initialization analysis. The checker does not track
  which fields or elements have been written; that analysis is explicitly
  deferred, which is why allocation (not each read) carries the unsafe gate.
- User-declared alignment on types or members (`align(N)` declarations).
- Zeroing allocators (`calloc` shape): zeroing is initialization and already
  expressible; this RFC is about skipping it.
- Changing `Stash<T>` / `Pool<T>` element policy; those families keep deriving
  alignment from T until RFC 0027 says otherwise.

## Proposed design

### Uninitialized allocation

- `allocate_uninit<T>()` performs the default-backend allocation with checked
  size/alignment derivation exactly like `allocate<T>`, then skips
  initialization. The result is an ordinary storable `Ptr<mut T>`.
- Allocation requires a non-zero `unsafe` depth; otherwise the checker
  rejects it naming the missing lexical permission.
- The programmer asserts, per RFC 0155: every field, element, and payload
  that is read, moved, or dropped was previously written. Partial
  initialization followed by a read, move (including implicit moves and
  aggregate construction), explicit cleanup, or scope-exit drop of the
  uninitialized part is an unsafe-precondition violation.
- Debug builds fill the region with a fixed recognizable byte before handing
  it out, so assertion failures surface as traps or visible corruption rather
  than stale-data success. Release builds skip the fill; both modes share the
  contract.

### Alignment control

- `allocate_aligned<T>(initial, alignment)` and
  `allocate_uninit_aligned<T>(alignment)` behave as their unaligned twins,
  except the backend aligns the base address to `alignment`.
- `alignment` of zero or not a power of two traps with a capacity/alignment
  diagnostic before any allocation occurs. Unrepresentable alignment (beyond
  the backend maximum) traps the same way.
- The aligned base address propagates through the existing pointer and drop
  machinery unchanged; deallocation needs no alignment argument because the
  backend retains what it requires internally.

## Diagnostics

- Uninit allocation outside `unsafe do ... end` reports the missing lexical
  permission.
- Invalid T (incomplete, non-finite, or otherwise unallocatable) reports the
  existing allocation-eligibility diagnostic, unchanged.
- Zero, non-power-of-two, or unrepresentable alignment traps at runtime with
  an alignment diagnostic; no compile-time evaluation of dynamic alignments
  is attempted.
- Reads, moves, or drops of provably unwritten storage report nothing at
  compile time by design (non-goal above); the debug fill is the development
  backstop.

## Acceptance sketch

Non-exhaustive: the implementing RFC promotes this to an exhaustive
Validation section.

- `allocate_uninit` succeeds only inside `unsafe do ... end`; the checked and
  generated shapes are otherwise identical to `allocate`.
- Fully-written-then-used uninit buffers behave identically to initialized
  buffers across moves, construction, explicit cleanup, and scope-exit drop.
- Invalid alignments trap before allocation in both debug and release modes.
- Debug builds fill uninit storage with the recognizable byte; release builds
  do not, with the contract text identical.
- Existing manifest hashes outside deliberate constructor additions do not
  move.

## Open questions

1. Whether the debug fill byte and its enablement should be a `Project`
   build-time setting or a fixed compiler behavior. Fixed behavior is
   simpler; a setting composes with future sanitizer work.
2. Whether `allocate_uninit` over a type with no droppable or readable parts
   of consequence (pure padding layouts, if expressible) should still require
   `unsafe`. Uniformity says yes; the cost is one keyword on a rare path.
3. Whether Stash/Pool need aligned variants in this RFC or stay deferred to
   RFC 0027. The proposal defers them; driver authors are the counter-voice.
