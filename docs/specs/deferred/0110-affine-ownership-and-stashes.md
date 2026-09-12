# RFC 0110: Affine Ownership and Deterministic Cleanup

- Kind: Feature Specification (Rust-Style RFC)
- Status: Blocked, 2026-09-10. RFC 0165 invalidates the Ref/Slice-lifetime portions
  and rejects affine ownership, implicit moves, and automatic cleanup as non-goals;
  the `docs/reference.md` Language boundary now states that shallow copying with
  explicit cleanup is the model. Do not implement as written; any independently
  retained work requires a later rescope.
- Features: implicit affine moves, deterministic cleanup, aggregate and
  collection ownership, and Stash/Pool lifetime integration
- Created: 2026-08-22
- Updated: 2026-09-10
- Depends on: RFC 0019 (generics), RFC 0020 (collections), RFC 0026
  (allocation), RFC 0027 (typed Stash and Pool), RFC 0035 (copying and manual
  lifetimes), RFC 0149 (Box and affine flow), RFC 0153 (lexical capabilities),
  and the implemented stateless default Heap
- Coordinates with: RFC 0039 (foreign ownership), RFC 0118 (cross-Task
  ownership), RFC 0152 (generic Strand capacity only; it must not change
  Error's ownership), RFC 0153 (scoped Slice), and RFC 0155 (unsafe boundaries)

## Summary

Replace shallow copying of owning values with affine ownership:

- copyable values continue to copy implicitly;
- assigning, passing, returning, or storing an affine value moves it implicitly;
- the source of a move becomes unavailable; and
- affine owners are destroyed deterministically on ordinary scope exits.

There is no `move`, `copy`, `drop`, or `forget` keyword. `borrow ... do ... end`
is the one lexical capability form and never transfers ownership. Ownership
state and cleanup selection are checker/generator facts and add no runtime
ownership metadata.

Stash and Pool use the same owner model for their control handles. Stash keeps
rooted raw-pointer allocation; Pool returns a restricted affine PoolSlot<T>
whose representation retains the Pool state required for automatic release.

## Goals

- Reject implicit duplication, use-after-move, double cleanup, and locally
  decidable invalidation errors.
- Make safe transfer use ordinary assignment/call/return syntax.
- Prevent forgotten cleanup without requiring source-visible boilerplate.
- Compose owned values through aggregates and collections.
- Integrate Stash and Pool invalidation with call- and lexical-scoped Ref/Slice.
- Preserve zero-cost representations and direct, readable C23 lowering.
- Keep fallible resource shutdown explicit rather than hiding errors in drop.
- Compose with future foreign borrow, transfer, retention, and deallocator
  contracts.

## Decision rationale

Implicit moves keep assignment, calls, returns, and construction as the one
ordinary transfer syntax. The checker still records every transfer explicitly,
so omitting a source keyword removes ceremony without weakening use-after-move
diagnostics or adding runtime work.

Automatic infallible cleanup is required for ownership to compose. A struct,
union, Array, List, or Dict that owns nested values must release them on every
ordinary exit without requiring each caller to reproduce its internal cleanup
order. The generated C keeps every concrete drop call visible and statically
ordered; no virtual destructor, unwind runtime, registry, or hidden allocation
is introduced.

`defer` and `errdefer` remain for explicit actions and fallible/shared resource
lifecycles. They do not duplicate the automatic lifetime of an affine owner.

## Non-goals

- Garbage collection or universal reference counting.
- Explicit move/copy syntax or a universal Clone interface.
- General lifetime parameters, stored Ref/Slice, or a whole-program borrow
  checker.
- Automatic cleanup of fallible resources such as IO.
- New Stash/Pool element cleanup or a generally storable/returnable Pool slot.
- Making foreign memory safe merely by wrapping it in Ptr.

## Ownership classes

Every complete type is classified after generic substitution:

| Class | Copy/transfer | Cleanup | Examples |
| --- | --- | --- | --- |
| Copyable value | assignment copies | none | Nil, Bool, numerics, Rune, Strand, Heap, Fun, raw Ptr |
| Affine owner | assignment moves | deterministic infallible drop | String, Error, List, Dict, Box, Stash, Pool, PoolSlot |
| Owning aggregate | moves when any member is affine | recursively drops owned members | object, ADT, structural union, Array |
| Call capability | never stored; may only be reborrowed into a nested call | ends with dynamic call | Ref, Slice |
| Lexical borrowed descriptor | never copied, stored, returned, or escaped | ends with `borrow` block | RuneCursor, Bytes |
| Shared/external handle | existing explicit lifecycle | never implicitly dropped here | IO, Task, Channel, Mutex |
| Atomic value | non-copyable restricted access | existing contract | Atomic<T> |

Error contains String fields for file, header, and message and owns those String
values. A compiler-injected or literal-backed String may use static storage, but
it has the same String representation and its automatic drop is a no-op. Error
is affine, returning or propagating it moves ownership, and its automatic drop
recursively drops all three fields. Generated C may copy the Error struct while
lowering a move; the Hexal source is unavailable afterward.

`T | Error` is affine whenever either alternative is affine. `try` moves the
selected success value or Error into a hidden propagation/result temporary and
disarms the union payload before running cleanup for scopes it leaves. On the
Error path, applicable `errdefer` actions and ordinary LIFO cleanup run before
the preserved Error is returned; the propagated Error itself is not dropped.

A struct, ADT, structural union, or Array becomes affine transitively when any
member is affine. List and Dict are always affine handles and additionally own
any affine elements admitted below.

## Moves and explicit duplication

A consuming context is determined by its destination type. Named and temporary
affine operands use the same syntax:

```hexal
first := Box(Node(value = 1))
second := first
consume(second)
print(first) // Type Error: first was moved
```

- Initialization, assignment source, by-value call argument, return, aggregate
  construction, and collection insertion implicitly move an affine value.
- Copyable values retain ordinary copy behavior in the same positions.
- A moved binding cannot be read, borrowed, moved, or cleaned. A permitted
  assignment may initialize it again; that initialization is not a read or use
  of the moved value.
- Reassigning an initialized affine binding destroys its old value before
  moving in the new one.
- Branch merges retain availability only when every continuing path agrees;
  loop back-edges preserve the loop-head state.
- Moving an affine field, payload, or element out of an aggregate remains
  rejected in v1. Whole-owner movement is valid.

### Consuming and observing operations

Implicit movement applies only to a consuming parameter or destination.
Compiler-owned operations classify each affine operand explicitly:

- user functions consume an affine by-value parameter and borrow only through
  Ref/Slice;
- `print`, comparison, length/query operations, hashing, iteration, and other
  read-only observers borrow affine operands for that call;
- non-consuming methods use their implicit Ref receiver;
- constructors such as String concatenation borrow source text while returning
  a fresh owner; and
- insertion, removal, return, explicit cleanup, and a foreign parameter marked
  transfer are consuming operations.

Consequently, ordinary observation never moves an owner:

```hexal
print(text)
same := text == other
joined := text.concat(h, other)
print(text, other, joined)
```

The checked tree records consume versus borrow before flow checking; code
generation never infers ownership from the emitted C parameter shape.

## Deterministic cleanup

An affine owner is destroyed exactly once on every ordinary path when its
current owning scope exits. Return, break, continue, and `try` propagation emit
the same cleanup required for the scopes they leave. Process traps need not run
cleanup, matching the existing trap contract.

Cleanup order is deterministic:

- successful affine initialization registers one compiler-owned cleanup action
  at that source position; automatic actions and explicit `defer`/`errdefer`
  actions execute through the same existing LIFO exit order;
- locals are therefore destroyed in reverse successful initialization order;
- object members are destroyed in reverse declaration order;
- Array elements are destroyed in reverse index order;
- only the active ADT/union payload is destroyed; and
- a container destroys owned entries before its backing storage.

An affine temporary lives through its complete expression and is then dropped
in reverse creation order unless that expression moves it into a destination or
consumer. If construction or evaluation propagates Error through `try`, every
already initialized temporary and aggregate member is dropped before control
leaves its scope. A self-move such as `value = value` is rejected before the
destination can be dropped. The checked tree records each move and cleanup
action explicitly; code generation renders that schedule and does not infer
ownership from syntax.

An explicit existing cleanup operation such as `String.free(heap)`,
`List.free(heap)`, `Box.free()`, `Stash.destroy()`, or `Pool.destroy()` consumes
the owner early and disarms its automatic drop. Later use or cleanup fails.
Registering such a consuming cleanup through `defer` or `errdefer` is rejected:
automatic drop already covers ordinary exits, and cleanup-bound bindings would
introduce a second ownership state solely for redundant syntax.

A deferred action may not capture an affine owner in v1, whether the action
would observe or consume it. Supporting such capture would require a separate
move-versus-borrow lifetime for the deferred closure. Explicit lifecycle APIs
for shared/fallible resources retain their existing defer contracts because
those handles are not implicitly dropped by this RFC.

Compiler-generated drop helpers are internal, not source methods. User-defined
aggregates gain no synthesized `free` member and therefore have no method-name
collision or cleanup signature that changes when a private field changes.

Heap is currently one stateless default allocator. Automatic drop calls the
same default backend directly and stores no Heap token in each owner. A future
allocator-choice RFC must make the selected allocator part of the owner or its
drop plan before permitting non-default allocation.

String's automatic drop checks the existing storage discriminator: owned
runtime storage is released and static literal storage is a no-op. This rule is
internal to automatic drop. An explicit user `String.free(heap)` on a statically
proved literal remains RFC 0138's error, and an opaque explicit free retains
its runtime trap.

Fallible cleanup is never implicit. IO and any future resource whose close may
return Error retain explicit lifecycle APIs and are not Boxable or recursively
droppable until a separate RFC selects a failure policy.

## Aggregate ownership

- Construction evaluates operands once, left to right, and implicitly moves
  affine operands into the result.
- An owning aggregate recursively drops its affine members in the deterministic
  order above.
- Reading a complete affine field/payload/element by value is rejected because
  it would duplicate or extract the owner.
- Passing an affine place directly to Ref or invoking a Ref-receiver method
  borrows it for the call.
- A copyable projection may be read without copying the enclosing owner:

```hexal
print(nodes[0].count)
```

- Assignment to an affine object field or Array/List element destroys the old
  value and moves in the new value when the selected place is writable.
- Existing `for ... in` binders remain immutable copies for copyable elements.
  When an Array/List element or Dict value is affine, its binder instead denotes
  a body-scoped read-only Ref to the current element; it cannot be moved,
  returned, stored, assigned, or retained after that iteration. Index and Dict
  key binders remain copyable values. Mutable and consuming iteration are not
  introduced. The collection/root remains actively borrowed for the body and
  participates in RFC 0149's nested capability checks.
- Task arguments/results and Channel elements containing affine owners remain
  rejected until RFC 0118 defines transfer.

## List ownership

For copyable T, existing List operations retain their value behavior. For
affine T:

- `push(value)` moves value into the List;
- indexing denotes an owning place: whole-value reads/moves fail, Ref receiver
  calls and copyable projections succeed;
- writable indexed assignment drops the previous element then moves in the
  replacement;
- `pop()` moves the removed owner to the caller; and
- `clear()` and automatic/explicit List cleanup recursively drop remaining
  elements before releasing storage.

No operation shallow-copies an affine T.

## Dict ownership

Dict keys remain exactly the copyable key types admitted by the current
reference; this RFC adds no affine-key support.

For copyable V, the current Dict API is unchanged. For affine V:

- `insert(key, value)` moves value into the entry; when the key already exists,
  key and value are fully evaluated before mutation, then the old value is
  dropped before the new value is stored, matching assignment and List element
  replacement. If evaluation propagates Error, the existing entry is unchanged;
- `remove(key)` moves the removed owner to the caller;
- `get(key)` and `find(key)` are unavailable because a first-class V result
  would copy or remove the entry;
- `borrow value from dict[key] do ... end` borrows the existing value through a
  body-scoped Ref; a missing key retains Dict's existing missing-key trap;
- `dict[key]` is valid only as this borrow source in v1 and does not become a
  general value-producing indexing expression;
- `for ... in` uses the body-scoped read-only affine value binder defined above;
  and
- automatic/explicit Dict cleanup recursively drops remaining values before
  releasing storage.

Replacing an affine entry is ordinary owning-container behavior, not an
exceptional event. It emits no duplicate-key runtime trap and returns no
displaced owner.

## Stash ownership and lifetime

- Stash<T> is an affine owner; assignment moves the handle and scope exit
  destroys it automatically.
- T remains transitively copyable and valid for the existing HeapAllocation
  position. Stash does not acquire element destructors in this RFC.
- `stash.allocate(initial)` retains its `Ptr<mut T>` result rooted in that
  Stash and accepts no method type argument.
- Individual release remains invalid. Reset/destroy releases only region bytes.
- A raw pointer rooted in a local Stash cannot be returned, stored in longer-
  lived storage, or passed to a call whose contract may retain it. Moving,
  explicitly destroying, or resetting the Stash while a locally tracked rooted-
  pointer binding remains in scope is rejected. On a common lexical scope exit,
  rooted pointer bindings end before the Stash's automatic drop and do not
  prevent that drop. The rule is deliberately scope-based, not liveness-
  inferred.
- Reset requires proof that no live call capability or locally tracked raw
  pointer use conflicts. Unknown retained aliases require an unsafe boundary.
- Destroy consumes Stash and invalidates every allocation.

## Pool ownership and lifetime

```text
Pool<T>.allocate(initial: T) -> PoolSlot<T>
Pool<T>.free(slot: PoolSlot<T>)
```

- Pool<T> is an affine owner; assignment moves the handle and scope exit
  destroys it automatically.
- T remains transitively copyable and valid for the existing HeapAllocation
  position. Pool release/destroy does not run T cleanup.
- `pool.allocate(initial)` returns `PoolSlot<T>`, a non-null affine slot owner
  tied to that Pool. PoolSlot takes exactly the same T as its Pool. Users may
  name the type in an exact direct parameter but cannot construct it directly.
- PoolSlot stores the Pool state needed for cleanup plus the typed slot pointer;
  it lowers to two machine words and carries no reference count or live bit.
- Field access and method dispatch traverse PoolSlot to T as Box traverses to
  its pointee. Ref/Ref<mut T> may borrow the slot value according to the
  selected place's writability. PoolSlot itself has no equality, ordering,
  hashing, printing, or nullability.
- `pool.free(slot)` consumes PoolSlot and retains the runtime range, alignment,
  owning-Pool, and live-slot checks. Automatic PoolSlot drop performs the same
  release and disarms after an explicit free.
- PoolSlot is valid only in local bindings and exact direct-call parameters in
  v1. Passing it by value moves it into the callee, whose cleanup then releases
  it unless the callee explicitly frees it. It cannot be
  returned, stored in an aggregate or collection, boxed, sent through
  Task/Channel, converted to raw Ptr, or outlive its Pool. These restrictions
  avoid general lifetime parameters while preserving ordinary local use.
- Pool destruction rejects directly tracked live slots. Reverse initialization
  cleanup normally drops local slots before their Pool; the existing runtime
  non-empty check remains a fail-closed fallback.

## Ref, Slice, and raw pointers

- Ref and Slice are call-scoped or lexically scoped under RFCs 0149/0153 and
  never own storage.
- A move, cleanup, reset, or reallocation that conflicts with an active call
  capability is rejected by simultaneous call checking.
- Raw Ptr remains copyable and non-owning. Its type never satisfies an owner or
  deallocator contract.
- Unknown raw-pointer provenance is not automatically unsafe. Only a concrete
  operation whose owning RFC states a validity precondition consumes unsafe
  permission.

## RuneCursor and Bytes borrows

RuneCursor and Bytes remain borrowed descriptors but are no longer freely
storable copyable values:

```hexal
borrow cursor from text.rune_cursor() do
    while cursor.has_next() do
        print(cursor.next())
    end
end

borrow stream from Bytes.over(buffer) do
    stream.seek(Seek.Start)
end
```

- Each constructor is valid only as a `borrow` source.
- RuneCursor keeps one read-only String-root capability for the complete block.
- Bytes keeps one exclusive List<Byte>-root capability for the complete block,
  because its write operation may mutate or grow that List.
- The bound descriptor holds its own cursor state but cannot be copied, moved,
  returned, stored, captured, or used after the block.
- Cursor/position-changing descriptor methods may mutate that private state even
  though the borrow binding itself cannot be reassigned. This does not grant
  write access to RuneCursor's String root; Bytes separately retains its
  exclusive mutable root capability.
- Nested calls may borrow the descriptor or its root only when RFC 0149's
  combined capability set permits the overlap.
- The root owner cannot move, drop, reallocate, or otherwise invalidate storage
  while the descriptor block is active.

## C interoperation

RFC 0039 must distinguish foreign operations that borrow for the call, consume
a Hexal owner, return a foreign owner, retain shared state, or use a named
foreign deallocator. Passing an affine value to a declared consuming parameter
moves it. A borrowing parameter does not. Missing or contradictory ownership
metadata is an ABI Error; pointer spelling never implies ownership.

There is no general deliberate-leak operation in v1. Transfer to a foreign
owner must use an explicit foreign contract rather than an unnamed unsafe
discard.

## C23 lowering

- Moves are ordinary C value/pointer assignments with no runtime move helper.
- Scope exits call statically selected internal drop helpers in deterministic
  order.
- Assignment drops an initialized affine destination before storing its new
  value.
- Internal drop helpers are monomorphized, demand-driven, declared before use,
  and emitted once per concrete type.
- No generic ownership header, reference count, per-owner live bit, or cleanup
  registry is emitted.
- Existing Stash/Pool runtime state and Pool live-byte checks remain unchanged.
  PoolSlot's explicit two-word value carries its Pool state and slot pointer so
  automatic release needs no hidden registry; Stash raw pointers gain no
  ownership metadata.

## Diagnostics

The earliest checker operation that proves the fact diagnoses:

- use, borrow, cleanup, or second move after an implicit move;
- inconsistent branch/loop ownership state;
- movement from an affine subplace or by-value read of an affine element;
- unsupported affine consumer or cross-Task position;
- deferred consuming cleanup;
- explicit cleanup after move/drop or repeated explicit cleanup;
- affine Dict `get`/`find`, invalid affine Dict value extraction/indexing, or
  unsupported affine key;
- RuneCursor/Bytes construction outside a borrow source or escape from their
  lexical borrow block;
- invalid PoolSlot construction, placement, escape, use after move/free, or
  owner mismatch;
- reset/destroy/release while a locally proved capability or slot conflicts;
  and
- missing or contradictory foreign ownership metadata.

Diagnostics name source bindings, places, and operations, never generated C
names or internal flow-state labels. RFC 0155 owns the diagnostic for a
specifically unsafe-capable operation lacking lexical permission.

## Required sweep

Inventory and reconcile:

- copyability/affinity classification for every compiler-owned type;
- every assignment, call, return, aggregate, collection, and temporary path;
- branch/loop flow merges and all scope/control-flow exits;
- every current consuming `defer`/`errdefer` cleanup site in tests and snippets,
  recording the migration inventory before replacing redundant cleanup with
  automatic-drop expectations;
- aggregate/union/Array recursive drop planning and generated declaration order;
- String static/owned discriminator handling in automatic drop;
- Error construction, return, `try` propagation, unions, field ownership, and
  compiler/runtime-produced file/header/message Strings;
- List/Dict copy, insertion, extraction, replacement, borrowed iteration,
  lexical indexing, clear, and cleanup paths;
- Stash/Pool handle copies, element eligibility, raw allocation provenance, and
  invalidation checks, plus every Pool allocation/free signature and PoolSlot
  position;
- RuneCursor/Bytes construction, copying, placement, root tracking, and escape;
- Task/Channel/Mutex/IO boundaries and foreign ownership gates;
- generated component discovery, includes, tests, snippets, and manifest.

## Reference synchronization

With explicit user approval after behavior stabilizes, update
`docs/reference.md` once to:

- replace the current prohibition on moves, borrow states, implicit
  destruction, and compiler-enforced cleanup with this affine model;
- classify every compiler-owned type and define implicit moves, reinitialization,
  cleanup ordering, temporaries, partial construction, and early cleanup;
- make Error an affine owner of its three String fields and define move-based
  return and `try` propagation;
- define owning aggregate/List/Dict behavior, borrowed affine iteration, Dict
  replacement and lexical value borrowing;
- define affine Stash/Pool handles and PoolSlot<T>;
- replace freely storable RuneCursor/Bytes with lexical borrowed descriptors;
  and
- record the direct C23 representations and absence of a runtime ownership
  registry.

This is a substantial replacement of the current manual-lifetime contract, not
a local wording edit. No reference edit occurs while implementation behavior is
still changing or without explicit user approval.

## Validation

This section is exhaustive.

- Every compiler-owned type receives exactly the ownership class listed here.
- Copyable values retain implicit copies; affine values move implicitly in
  every consuming context; no `move` or `copy` keyword is accepted.
- Every later use of a moved source fails; branch/loop merges are conservative.
- Reassignment drops the previous affine destination exactly once.
- Compiler-owned observers borrow affine operands without moving them; user
  by-value parameters and explicitly classified consumers move them.
- Every affine owner is dropped exactly once on all ordinary exits in reverse
  successful initialization order; traps need not run cleanup.
- Automatic and explicit cleanup actions share one LIFO order. Full-expression
  temporaries drop unless moved; `try` drops initialized temporaries and partial
  aggregate members before propagation.
- Self-move assignment and deferred capture of an affine owner are rejected.
- Explicit early cleanup consumes and disarms automatic drop; repeated cleanup
  and consuming defer/errdefer fail.
- Static String automatic drop is a no-op, owned String automatic drop frees,
  and explicit literal free retains RFC 0138 behavior.
- Error owns String fields, moves on return/propagation, and recursively drops
  them exactly once; generated C struct copying never makes the source usable.
- `try` moves exactly one active `T | Error` payload, disarms it, runs applicable
  cleanup before propagation, and never drops the preserved result/Error.
- Owning aggregates move and recursively drop affine members; affine subplace
  moves fail while copyable projections succeed.
- Array/List/Dict iteration over affine elements/values creates body-scoped
  read-only binders; they cannot move, escape, or mutate the element.
- List insertion/pop/replacement/clear/drop implement the exact affine rules
  above without shallow copies.
- Affine Dict insertion replaces by dropping the old value and moving the new;
  key/value evaluation failure leaves the old entry unchanged; remove moves;
  get/find fail; `borrow value from dict[key]` succeeds or traps for a missing
  key; remaining values drop during cleanup.
- Stash/Pool are affine owners but admit only transitively copyable T. Stash
  retains rooted raw pointers; Pool returns affine PoolSlot<T> owners.
- PoolSlot is two words, releases exactly once, cannot escape its Pool, and
  retains the existing runtime ownership/range/alignment/live checks. A direct
  by-value call consumes the caller's slot and makes the callee its owner.
- RuneCursor and Bytes exist only as lexical borrowed descriptors and cannot
  outlive or invalidate their roots.
- Local Stash/Pool allocation pointers cannot escape their owner, and moving or
  dropping the owner rejects any directly tracked live allocation or slot.
- Rooted pointer bindings ending at the same lexical scope exit do not block
  the owner's automatic drop.
- Active Ref/Slice conflicts fail; raw Ptr remains non-owning and copyable.
- Affine Task/Channel positions fail pending RFC 0118.
- Foreign borrow/transfer behavior follows only explicit RFC 0039 metadata.
- Generated C contains deterministic drop calls and helpers but no runtime
  ownership registry or per-value allocator field.
- Tests assert cleanup placement and declaration-before-use in generated C;
  ordinary tests stay pure Go and qualified C23 tests execute representative
  move, replacement, nested-drop, String, collection, Stash, and Pool cases.
- Manifest movement is limited to deliberate ownership and cleanup artifacts.

## Detailed implementation plan

1. Build on RFC 0149 affine availability and classify every compiler-owned type.
2. Make consuming contexts implicit moves; remove all unary-move syntax/plans.
3. Derive recursive infallible drop plans and explicit checked-tree cleanup
   schedules for full-expression temporaries, partial construction, every
   ordinary scope/control-flow exit, `try` propagation, and initialized affine
   replacement. Reject self-moves and deferred affine captures.
4. Implement explicit early-cleanup disarming, reject consuming defer, and add
   String static-storage automatic-drop handling.
5. Enable owning aggregates and copyable projections while rejecting affine
   subplace movement.
6. Implement List/Dict ownership, replacement, lexical Dict value borrowing,
   and read-only affine iteration exactly as specified.
7. Make Stash/Pool handles affine while preserving copyable T; retain Stash raw
   pointers and introduce the restricted two-word PoolSlot owner with existing
   runtime validation.
8. Restrict RuneCursor/Bytes to lexical borrowed descriptors, migrate their
   snippets/tests, and remove freely storable/copyable descriptor paths.
9. Add foreign/concurrency gates, generated-C assertions, snippets, and all
   Validation cases.
10. Review manifest blast radius and run ordinary/qualified suites. With explicit
   user approval, synchronize `docs/reference.md`; then rebuild/restart the
   workbench and close when all artifacts agree.

## Open questions

None.
