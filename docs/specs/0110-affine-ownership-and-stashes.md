# RFC 0110: Affine Ownership and Stash Lifetimes

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design proposed, implementation not started
- Features: affine owning values, explicit clone/share, cleanup obligations,
  Stash and Pool lifetime integration, and foreign ownership contracts
- Created: 2026-08-22
- Updated: 2026-08-27
- Depends on: RFC 0019 (generics), RFC 0020 (collections), RFC 0026
  (allocation and cleanup), RFC 0027 (typed Stash and Pool allocators), RFC 0035
  (copying and manual lifetimes), RFC 0039 (C interoperability), RFC 0069
  (C23-backed compiler simplification), and the implemented stateless default
  Heap (see `docs/reference.md`'s Allocation and lifetime section)
- Coordinates with: the implemented synchronous descriptor and memory streams
  and the implemented scheduler-aware blocking pool (both in
  `docs/reference.md`), and `docs/reference.md` generally

## Summary

Replace implicit shallow copying of owning values with affine ownership:

- copyable values may be copied freely;
- owning values may be moved at most once;
- explicit `clone` creates an independent owner when the type supports it;
- explicit `share` creates a non-owning or shared handle only where the type
  contract permits it; and
- borrowed views cannot outlive the storage they borrow from.

Stash and Pool allocations use the same ownership model. A `Stash<T>` owns a
region containing exactly T, and every allocation or View rooted in that region
is invalid after `reset` or `destroy`. A Pool owns reusable slots, and a slot
token is invalid after release or Pool destruction.

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
- Make *forgetting* cleanup a compile error rather than a leak, without making
  cleanup invisible. See Cleanup obligations.
- Make Stash and Pool invalidation explicit and composable with Views.
- Preserve zero-cost representations: ownership state is checker metadata,
  not a mandatory runtime header.
- Compose with C interop ownership, retention, and deallocator annotations.
- Keep copying of scalars, inline aggregates, function pointers, and borrowed
  descriptors cheap and ordinary.

## Why cleanup stays explicit

Affine ownership is the precondition for destructors, so this RFC is where the
question gets asked: once moves are tracked, should an owner's cleanup run
automatically at scope exit? The answer is no, and the reason is recorded here
so it is not re-derived every time the subject returns.

**Cleanup remains source-visible.** Hexal requires the author to write the
operation that ends an owning value's lifetime. `defer` can place that explicit
operation at every relevant exit without making ownership cleanup implicit.
This keeps cleanup order reviewable in Hexal and keeps generated C aligned with
operations present in the source.

The stateless default allocator means automatic destruction is technically
possible for current Heap-backed values: generated cleanup could call the
default free primitive without retaining a Heap identity. Stash and Pool also
own enough state for `destroy()` to release their default-backed storage. Lack
of an allocator argument is therefore not the deciding objection.

Four points define the actual trade:

- **The shallow-copy objection is conditional, not permanent.** Destructors on
  today's non-owning aliases would double-free on every call. This RFC's move
  semantics would fix that, so it is not the deciding argument.
- **Cost is not the objection.** Hexal has no inheritance and no exception
  unwinding, so destructors would be statically dispatched, monomorphized, and
  free of unwind-safety obligations. They would be genuinely zero-cost and
  materially simpler than C++'s. The objection is hidden control flow, not
  overhead.
- **The lowering machinery already exists.** `defer` already injects
  reverse-order cleanup at branch, loop, `return`, `break`, and `continue`
  exits. Destructors would reuse it. The difference is that deferred cleanup
  corresponds to something the author wrote, and generated C that a reader can
  follow is a product goal.
- **Future allocator choice does not change the decision.** If a later feature
  lets one owning value select among allocators, that value must retain the
  choice or receive it during explicit cleanup. This RFC does not add hidden
  destruction in anticipation of that feature.

### What destructors would have solved

One real problem, which this RFC does not get to ignore: **aggregate cleanup
does not compose.** Per `docs/reference.md`, freeing a container releases only
its own backing region and never what its elements refer to. A `List<String>`
needs a loop and then a free; an object with three List members needs four
calls in the right order. That is the strongest argument for destructors and it
must be answered by something.

Three partial answers, in preference order. None makes Stash destruction a
destructor:

1. **Typed region cleanup (RFC 0027).** One `stash.destroy()` releases every T
   allocation in that Stash, including inline nested data. It does not clean a
   Heap-backed handle stored inside T; that resource remains a separate
   obligation. This removes per-node cleanup for Stash-owned object graphs but
   is not a general deep-destructor mechanism.
2. **Compiler-generated deep cleanup.** The generator monomorphizes and knows
   each type's full structure, so a generated whole-value release is
   mechanical. It stays explicit at the call site and receives any required
   cleanup arguments normally. A later RFC owns the surface; this one only
   records that the capability is cheap and does not require destructors.
3. **Cleanup obligations**, below, which do not release anything but make
   forgetting each required release a compile error.

## Cleanup obligations

This resolves the former open question of whether an affine value may be
discarded silently, warned, or explicitly abandoned.

An owning value carries a cleanup obligation. The obligation is discharged by
moving the value onward -- into a call, a return, an aggregate, or a `defer`
that consumes it -- or by an explicit abandonment form where the type contract
defines discard as a leak or as region abandonment.

An owner whose obligation is still outstanding when its binding leaves scope,
on any locally decidable path, is a Type Error. It is not a warning and not a
leak.

This is the guarantee destructors offer, obtained without the mechanism:

- the author cannot forget cleanup, because forgetting does not compile;
- cleanup remains written in the source, so generated C contains no call the
  author did not ask for; and
- the allocator continues to arrive as an ordinary argument.

Bounds, so this stays inside the RFC's stated analysis limits:

- The rule uses the same local ownership, scope, and escape facts as every
  other rule here. A value whose fate needs interprocedural or alias analysis
  is undecidable and is accepted, exactly as elsewhere.
- Region-rooted values are discharged by their region. A Stash allocation
  carries no separate obligation; `stash.destroy()` discharges every value
  rooted in it.
- Abandonment is explicit and reads as abandonment at the call site. Silent
  discard is what this rule removes.

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

## Stashes

- `Stash<T>` is an affine owner of its typed region and its backing allocator
  contract. Its canonical T never changes during that state’s lifetime.
- `stash.allocate(initial)` returns `MutPtr<T>` rooted in that Stash. It accepts
  no method type argument; union T uses ordinary contextual union injection.
- Individual release of a Stash allocation is invalid; `reset` or `destroy`
  releases the region.
- An allocation or View rooted in a Stash cannot escape the Stash's lifetime.
- `reset` requires that no live borrowed value or foreign retention is proven
  to depend on the invalidated region. An undecidable escape requires unsafe.
- `destroy` consumes the Stash owner and invalidates every allocation from it.
- Element cleanup is separate from byte-region release. Stash destruction does
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
- Stash and Pool control blocks retain their state identity and invalidation
  metadata required by runtime checks. A Stash additionally retains its one
  immutable T size/alignment pair; neither family retains a parent Heap.
- Generated C must not copy an affine handle through a helper that the checker
  classified as a move.
- Unsafe operations lower only after the checker records the explicit unsafe
  boundary; no safety annotation is emitted as if it were a C guarantee.

## Non-goals

- Garbage collection.
- Reference counting for every owned value.
- Implicit destructors, and any automatic cleanup at scope exit. See Why
  cleanup stays explicit; cleanup operations remain visible in source and in
  the generated control flow.
- A whole-program borrow checker.
- Storing an allocator in owning values solely to make automatic cleanup
  possible.
- Defining the surface for compiler-generated deep cleanup; a later RFC owns
  it. This RFC records only that it is available without destructors.
- Making foreign C memory safe by wrapping it in a Hexal pointer type.
- Allowing Stash reset or Pool destruction while live borrows remain.

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
- An owner whose cleanup obligation is outstanding when its binding leaves
  scope is rejected on every locally decidable path, including branch and loop
  merges. It is a Type Error, never a warning.
- Moving an owner into a call, a return, an aggregate, or a consuming `defer`
  discharges its obligation exactly once.
- Explicit abandonment discharges the obligation and is required for it;
  silent discard is rejected.
- A value rooted in a Stash or Pool carries no separate obligation, and
  region destruction discharges every value rooted in it.
- An obligation whose discharge needs interprocedural, alias, or escape
  analysis beyond local facts is accepted, consistent with every other rule
  here, and no diagnostic claims it was proven released.
- No destructor, scope-exit cleanup call, or per-value allocator field is
  emitted; generated cleanup corresponds to a cleanup the author wrote.
- Stash allocations and Views cannot locally outlive Stash reset or destroy.
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

Resolved since the draft: whether an affine value may be discarded silently
is now answered by Cleanup obligations -- an outstanding obligation at scope
exit is a Type Error, and abandonment is an explicit form.

1. Whether `share` is a language primitive or a type-owned method.
2. Whether Stash reset uses lexical regions only or accepts explicit unsafe
   invalidation of outstanding borrows.
3. The exact Pool slot-owner representation and syntax.
4. Which current shared handles become affine owners, shared handles, or
   explicit runtime capabilities.

## Reference synchronization

After implementation stabilizes, update `docs/reference.md` with ownership
classes, move/clone/share rules, Stash and Pool lifetime rules, View provenance,
foreign ownership annotations, cleanup obligations, and generated-C contracts.
Remove the current shallow-copy and unconstrained-free rules only in the same
change.

Record that cleanup is always written by the author and never inserted at scope
exit, and that a cleanup obligation left outstanding is a compile error. The
reference states what the language means, so it carries the rule and not the
rationale; the destructor analysis stays in this RFC.
