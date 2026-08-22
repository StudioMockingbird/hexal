# RFC 0110: Affine Ownership and Arena Lifetimes

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Features: affine owning values, explicit clone/share, Arena and Pool
  lifetime integration, and foreign ownership contracts
- Created: 2026-08-22
- Depends on: RFC 0019 (generics), RFC 0020 (collections), RFC 0026
  (allocation and cleanup), RFC 0027 (Arena and Pool allocators), RFC 0035
  (copying and manual lifetimes), RFC 0039 (C interoperability), and RFC 0069
  (C23-backed compiler simplification)
- Coordinates with: RFC 0091 (blocking-syscall boundary), RFC 0108
  (descriptor and memory streams), and `docs/reference.md`

## Summary

Replace implicit shallow copying of owning values with affine ownership:

- copyable values may be copied freely;
- owning values may be moved at most once;
- explicit `clone` creates an independent owner when the type supports it;
- explicit `share` creates a non-owning or shared handle only where the type
  contract permits it; and
- borrowed views cannot outlive the storage they borrow from.

Arena and Pool allocations use the same ownership model. An Arena owns a
region, and every allocation or View rooted in that region is invalid after
`reset` or `destroy`. A Pool owns reusable slots, and a slot token is invalid
after release or Pool destruction.

This RFC does not introduce a general Rust-style borrow checker. The checker
must reject every misuse decidable from local ownership, scope, and escape
facts; undecidable cases require an explicit unsafe boundary rather than being
silently treated as safe.

## Goals

- Reject use-after-move, double-free, and double-release statically where local
  facts decide them.
- Prevent implicit copying of values that own allocations or external
  resources.
- Make `defer` consume the exact owner registered for cleanup.
- Make Arena and Pool invalidation explicit and composable with Views.
- Preserve zero-cost representations: ownership state is checker metadata,
  not a mandatory runtime header.
- Compose with C interop ownership, retention, and deallocator annotations.
- Keep copying of scalars, inline aggregates, function pointers, and borrowed
  descriptors cheap and ordinary.

## Ownership classes

Every complete type is classified after generic substitution:

| Class | Copy | Move | Borrow | Example |
| --- | --- | --- | --- | --- |
| Copyable | implicit | ordinary copy | allowed | scalar, object, Array |
| Affine owner | forbidden | consumes source | produces borrow | String, List, Heap |
| Borrowed view | descriptor copy | ordinary copy | rooted in owner | View, `Ptr<T>` |
| Shared handle | explicit contract | handle copy | shared access | Mutex, Channel |
| Atomic value | forbidden | restricted | explicit pointer rules | Atomic<T> |

The final type table must name the class for every compiler-owned type. A type
containing an affine owner is affine unless it provides an explicit ownership
implementation. An affine owner may be discarded only where the type contract
defines discard as a leak or region abandonment; ordinary `defer` should be
the default cleanup path.

## Move and clone rules

- Assignment of an affine value moves it and marks the source binding moved.
- Passing an affine value by value moves it unless the parameter is explicitly
  borrowed or shared.
- Returning an affine value moves it to the caller.
- Reading a moved binding is a compile-time error.
- A branch merge retains a binding only when every continuing branch leaves the
  same ownership state.
- `clone` is explicit and available only for types that can duplicate their
  resource safely. It may allocate and may fail according to the type's
  contract.
- `share` is explicit and does not transfer exclusive ownership. It is valid
  only for a type with a defined shared-handle protocol.
- `defer owner.destroy(...)` captures and consumes the owner. Registering a
  second cleanup for the same affine value is an error.
- Passing an affine value to an unknown foreign function requires an explicit
  ownership annotation: borrow, transfer, retain, or return-transfer.

The source syntax for `clone`, `share`, and ownership annotations remains open;
the semantic distinction is not open.

## Arenas

- `Arena` is an affine owner of its region and its backing allocator contract.
- `arena.allocate<T>(initial)` returns an owned allocation rooted in that Arena.
- Individual release of an Arena allocation is invalid; `reset` or `destroy`
  releases the region.
- An allocation or View rooted in an Arena cannot escape the Arena's lifetime.
- `reset` requires that no live borrowed value or foreign retention is proven
  to depend on the invalidated region. An undecidable escape requires unsafe.
- `destroy` consumes the Arena owner and invalidates every allocation from it.
- Element cleanup is separate from byte-region release. Arena destruction does
  not silently run arbitrary destructors for values stored in the region.

## Pools

- `Pool<T>` is an affine owner of its slot storage.
- `pool.allocate(initial)` returns an affine slot owner or a typed allocation
  token whose exact syntax is settled with RFC 0027.
- `pool.free(slot)` consumes that slot owner and makes the slot reusable.
- A released slot cannot be read, written, or freed again.
- `pool.destroy` requires that no live slot owner or borrow remains and consumes
  the Pool owner.
- A Pool does not become a general allocator and cannot back a different
  canonical element type.

## Views and pointers

- `View<T>` remains a copyable descriptor, but every non-empty View carries a
  checker-visible root lifetime.
- A View rooted in an affine owner cannot outlive that owner or cross a reset,
  destroy, move, or consuming transfer that invalidates the root.
- `Ptr<T>` and `MutPtr<T>` remain non-owning capabilities. Taking `ref` never
  creates an owner and therefore cannot satisfy a deallocator contract.
- A pointer or View whose provenance cannot be proven locally is accepted only
  through an unsafe boundary or a foreign contract that assumes responsibility
  for validity.

## C interoperation

RFC 0039 supplies the foreign ownership annotation. A foreign function may:

- borrow a value for the call;
- consume a Hexal owner;
- return a new foreign owner;
- retain a shared handle; or
- return storage whose lifetime is governed by a named foreign deallocator.

The compiler must not infer one category from a C pointer type. Missing or
contradictory ownership metadata is an ABI Error, not an implicit borrow.

## C23 lowering

- Moves lower to ordinary C value passing; ownership state is not emitted.
- Clone and allocator operations lower to their selected runtime/library
  implementation.
- Arena and Pool control blocks retain the allocator identity and invalidation
  metadata required by their runtime checks.
- Generated C must not copy an affine handle through a helper that the checker
  classified as a move.
- Unsafe operations lower only after the checker records the explicit unsafe
  boundary; no safety annotation is emitted as if it were a C guarantee.

## Non-goals

- Garbage collection.
- Reference counting for every owned value.
- Implicit destructors.
- A whole-program borrow checker.
- Making foreign C memory safe by wrapping it in a Hexal pointer type.
- Allowing Arena reset or Pool destruction while live borrows remain.

## Validation

This section is exhaustive. RFC 0110 is complete only when every item below
passes:

- Copyable scalars and inline aggregates retain ordinary copy behavior.
- An affine binding cannot be copied implicitly, read after move, moved twice,
  or cleaned twice on any locally decidable path.
- Affine parameters and returns transfer ownership exactly once.
- Branch and loop merges preserve the conservative ownership state.
- Explicit clone is required for an independent owner and is rejected for a
  type without a clone contract.
- `defer` captures the moved owner exactly once and rejects later use or a
  second cleanup of that owner.
- Arena allocations and Views cannot locally outlive Arena reset or destroy.
- Pool slots cannot be locally used or released after `pool.free`.
- Pool destruction rejects live slots and live borrows.
- Unknown ownership at a foreign call produces an ABI diagnostic or requires
  the explicit unsafe form; it is never silently treated as a borrow.
- Generated C preserves the existing representation of values and emits no
  ownership bookkeeping unless the selected runtime allocator requires it.
- Existing non-ownership behavior remains unchanged until the reference is
  synchronized after this RFC stabilizes.
- Ordinary tests remain pure Go and assert checked trees and generated C text.

## Open questions

1. Whether `share` is a language primitive or a type-owned method.
2. Whether affine values may be discarded silently, warned, or must be
   explicitly abandoned.
3. Whether Arena reset uses lexical regions only or accepts explicit unsafe
   invalidation of outstanding borrows.
4. The exact Pool slot-owner representation and syntax.
5. Which current shared handles become affine owners, shared handles, or
   explicit runtime capabilities.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` with ownership
classes, move/clone/share rules, Arena and Pool lifetime rules, View provenance,
foreign ownership annotations, and generated-C contracts. Remove the current
shallow-copy and unconstrained-free rules only in the same change.
