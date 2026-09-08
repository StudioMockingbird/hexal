# RFC 0149: `Box<T>`, `Ref<T>`, and `MutRef<T>`

- Kind: Feature Specification (Rust-Style RFC)
- Status: Design decisions required
- Created: 2026-09-08
- Updated: 2026-09-08
- Origin: adapt Snacc's implemented ownership and call-scoped reference model
  to Hexal's C23, raw-pointer, allocator, and C-interoperability contracts
- Coordinates with: RFC 0039 (foreign ownership), RFC 0110 (affine ownership,
  Stash/Pool lifetimes, and cleanup obligations), RFC 0118 (cross-Task
  ownership), RFC 0137 (borrowed-view provenance), RFC 0148 (bounded sequence
  borrows)
- Does not update `docs/reference.md`: this is a draft; synchronize the
  reference only after the remaining decisions are settled and implementation
  is approved

## Summary

Add three memory capabilities:

```text
Box<T>      one non-null, uniquely owned heap allocation containing T
Ref<T>      call-scoped read-only borrow of an existing T place
MutRef<T>   call-scoped exclusive read-write borrow of an existing T place
```

Keep the existing raw-pointer layer:

```text
Ptr<T>      storable non-owning raw read pointer
MutPtr<T>   storable non-owning raw read-write pointer
```

- `Box<T>` carries ownership in its type and moves instead of copying.
- `Ref<T>` and `MutRef<T>` are parameter modes, not values. They cannot escape
  their call and need no general lifetime syntax.
- `Ptr<T>` and `MutPtr<T>` remain first-class values for C interoperation,
  opaque handles, platform APIs, and low-level memory access. They carry no
  ownership or lifetime guarantee.
- `View<T>` remains the existing bounded read-only sequence borrow. RFC 0148
  separately owns its rename and writable counterpart.

This adapts Snacc rather than copying it literally. Hexal retains raw pointers
and explicit allocation families because it must express C programs and foreign
APIs directly.

## Problem

`Heap.allocate<T>` returns `MutPtr<T>`, which is non-owning. No value is
identified by the type system as the allocation's unique owner:

```hexal
heap := Heap()
node := heap.allocate<Node>(Node(value = 1))
alias := node
heap.free(node)
// alias now dangles
```

Losing every pointer leaks the allocation. Copying one creates another address
that can outlive cleanup. Local checks reject some obvious mistakes, but the
type does not distinguish ownership from observation.

Raw pointers also carry more power than an ordinary function call needs. A
callee needing temporary access currently receives a storable, returnable
pointer whose lifetime is not represented in its type.

## Goals

- Make ownership of one default-allocated object explicit.
- Reject locally decidable use-after-move, duplicate ownership, double cleanup,
  and overlapping mutable borrows.
- Make ordinary borrowed parameters low ceremony: callers pass places without
  address-of or dereference syntax.
- Support recursive data structures through explicit Box edges.
- Preserve pointer-sized, zero-overhead C representations.
- Preserve Ptr/MutPtr for C ABI fidelity and low-level code.
- Reuse one place-identity and flow-state model for moves, references, Views,
  Stash allocations, and Pool slots.
- Keep allocation failure consistent with default allocation: failure or
  unrepresentable size traps.

## Non-goals

- Garbage collection, reference counting, shared ownership, or weak ownership.
- General lifetime parameters or Rust-style lifetime syntax.
- Making foreign memory safe merely by changing its pointer type.
- Pointer arithmetic or pointer casts.
- Settling RFC 0148's `View`/`Span` naming decision.
- Replacing typed `Stash<T>` or `Pool<T>` allocation policies.
- Inferring foreign ownership from a C pointer type.

## Syntax

Conceptual grammar additions:

```ebnf
special-form-type-constructor = existing-special-form
                              | "Box"
                              | "Ref"
                              | "MutRef" ;
box-expression = "Box" , "(" , expression , ")" ;
```

```hexal
node := Box(Node(value = 1))
```

- Each type constructor takes exactly one type argument.
- `Box(expression)` evaluates its operand exactly once and produces `Box<T>`,
  where the checked operand determines T.
- There is no `.new()`, explicit Heap, `box(...)`, `Box<T>(...)`, or implicit
  conversion between T and Box<T>.
- Expected `Box<T>` context flows into the operand where ordinary contextual
  typing already permits it.

## Call-scoped references

```hexal
fun inspect(node: Ref<Node>) do
    print(node.value)
end

fun increment(node: MutRef<Node>) do
    node.value = node.value + 1
end

fixed := Node(value = 1)
mut writable := Node(value = 2)

inspect(fixed)
inspect(writable)
increment(writable)
```

- The caller writes an ordinary initialized place expression; no `ref`, `&`,
  or dereference expression is used.
- `Ref<T>` accepts fixed or writable places of exact type T.
- `MutRef<T>` accepts only writable places of exact type T.
- Inside the callee, the parameter name denotes the referent. Reads, field
  selection, method calls, and whole-value assignment act on caller storage.
- Assignment and mutating methods are invalid through Ref.
- Passing a reference parameter to another compatible reference parameter
  reborrows it for that nested call.
- Passing a reference parameter to a by-value T parameter reads its current
  value. An affine T cannot thereby be copied.

### Placement

Ref and MutRef are not value types. They are valid only as direct function or
explicit method parameters and, if Open decision 2 selects it, method receiver
targets.

They are invalid as results, bindings, members, payloads, union members,
collection arguments, Box arguments, pointer pointees, function-value
parameters/results, Task/Channel values, or nested reference arguments. There
is no reference literal, equality, printing, nullability, address-of syntax, or
dereference syntax. Escape is impossible by construction.

### Referents

- T is a complete finite type valid in the borrowed place.
- Matching is exact after transparent-alias resolution.
- Numeric widening, union injection, pointer weakening, and literal conversion
  do not change a place's referent type.
- A selected field is a place. Distinct sibling fields do not overlap; a place
  and its ancestor or descendant do.
- Literals, temporaries, call results, conversion results, and conditional
  results are not borrowable places.
- A narrowed union binding borrows its active payload only while the narrowing
  remains valid.

### Exclusivity

Borrowing lasts exactly for the dynamic call:

- overlapping Ref arguments may coexist;
- MutRef may not overlap any Ref or MutRef in the same call;
- a move, replacement, cleanup, or structural invalidation of the referent is
  forbidden while the call borrow exists;
- arguments evaluate once, left to right;
- value arguments retain their evaluated values; place identities are retained
  for reference arguments; simultaneous borrow checking occurs after all
  arguments are evaluated.

```hexal
fun exchange(left: MutRef<Int32>, right: MutRef<Int32>) do
    previous := left
    left = right
    right = previous
end

exchange(point.x, point.y) // valid
exchange(value, value)     // rejected
```

Overlap is proved from canonical roots and projections, never runtime address
comparison.

## Box semantics

- Box owns exactly one non-null default-allocator allocation containing one
  initialized T.
- Its representation is one pointer, independent of T.
- Box is affine even when T is copyable. An aggregate containing Box is affine
  transitively for every consumer enabled by Open decision 3.
- `Box<Box<T>>` is valid. `Box<Ref<T>>` and `Box<MutRef<T>>` are invalid.
- Box itself is never Nil. Absence is `Box<T> | Nil`.
- `Box<T> | Nil` uses the nullable-pointer niche: null is Nil and non-null is
  Box. It emits no tagged wrapper.
- Box has no direct equality, ordering, hashing, or printing. A nullable Box may
  be compared with Nil and tested for truthiness; a non-null Box is truthy.

### Recursive layout

```hexal
type Node is struct
    value: Int32,
    next: Box<Node> | Nil,
end
```

- A by-value layout cycle remains invalid unless every recursive path crosses a
  Box edge.
- Layout size/alignment traversal stops at Box; semantic dependency traversal
  does not.
- Mutually recursive nominal types are valid when Box breaks every layout
  cycle.

### Access and borrowing

- Field selection and method dispatch automatically traverse Box layers needed
  to reach the selected member. This neither copies nor consumes the Box.
- A fixed Box root permits read-only pointee access. A mutable Box root permits
  read-write pointee access and Box replacement.
- There is no general implicit Box-to-T conversion.
- A Box<T> place automatically lends its pointee to Ref<T>.
- A mutable Box<T> place automatically lends its pointee to MutRef<T>.
- The Box place itself may bind to Ref<Box<T>> or MutRef<Box<T>> when that is
  the exact declared parameter type.
- Borrowing never transfers ownership.

### Moves

Initialization, assignment source, by-value argument, return, aggregate
construction, and selected collection insertion consume an affine value.
Consumption transfers the pointer and cleanup obligation, then marks the source
unavailable:

```hexal
first := Box(Node(value = 1))
second := first
inspect(second)
inspect(first) // rejected: first was moved
```

- Passing Box by value moves it; use a reference parameter to avoid transfer.
- Returning Box moves it to the caller.
- Reassigning an initialized Box first discharges or transfers the old
  destination obligation.
- An affine field cannot be moved out through ordinary field selection in v1;
  whole-owner moves remain valid.
- Branch merges keep a binding available only when every continuing path has
  the same ownership state. Loop back-edges preserve the loop-head state.
- Moves lower to ordinary pointer copies; ownership state is checker-only.

## Cleanup

Snacc destroys Box automatically. Hexal's current ownership draft requires
source-visible cleanup. This RFC records the conflict rather than silently
choosing incompatible rules.

The provisional Hexal adaptation is:

```text
Box<T>.free() -> no value
```

- `free()` consumes Box, recursively releases owned values in T according to
  their type contracts, then releases the Box allocation.
- Box carries a cleanup obligation. Every locally decidable path must move it,
  bind it to one deferred cleanup, or free it.
- An outstanding obligation at scope exit is a Type Error.
- Box uses the default allocator, carries no Heap, and accepts no Heap argument.
- No ownership header, reference count, or live-bit metadata is emitted.

To keep `defer` useful, registration produces a `cleanup-bound` binding:

```hexal
mut node := Box(Node(value = 1))
defer node.free()
inspect(node)   // valid temporary borrow
node.value = 2  // valid when node is mutable
```

A cleanup-bound owner remains readable, writable according to its root, and
call-borrowable. It cannot be moved, returned, reassigned, explicitly freed,
escaped, or registered again. Its hidden deferred receiver is the unique
cleanup owner and consumes it on every ordinary scope exit. This refines RFC
0110's earlier immediate-unavailability rule.

Open decision 1 may replace this with automatic destruction.

## Existing allocation families

- Use Box for an idiomatic independently owned Hexal object.
- Use Heap.allocate when a raw stable address and manual pointer protocol are
  intentional.
- Use Stash<T> for typed bulk lifetime and reset.
- Use Pool<T> for fixed-capacity reusable slots.
- Use Ptr/MutPtr for C, platform, opaque, and low-level borrowed addresses.
- V1 provides no implicit Box-to-Ptr conversion. A later explicit raw borrow
  belongs with the C-interoperability ownership surface.

## Aggregates, collections, and Tasks

Box is valid in nominal aggregates and structural unions. The complete v1
matrix depends on Open decision 3. The target model is:

- object/ADT/union construction moves Box fields or payloads;
- union narrowing borrows a Box payload unless a later consuming extraction is
  added;
- Array/List/Dict-value insertion moves Box values;
- replacement/removal transfers or discharges one obligation;
- Dict keys cannot contain Box;
- Task arguments/results and Channel elements move Box rather than copying it.

List/Dict and cross-Task movement require RFCs 0110 and 0118. A staged v1 may
reject those positions until every consumer preserves affine transfer.

## Methods

The intended safe receiver model is:

```hexal
method Ref<Counter>.read(): Int32 do
    return self.value
end

method MutRef<Counter>.increment() do
    self.value = self.value + 1
end
```

Ref receivers read; MutRef receivers read and write. Value and Box places adapt
only for the call. Raw Ptr/MutPtr receivers remain for genuinely raw APIs. Open
decision 2 settles whether this replaces or merely accompanies current
receiver targets.

## C interoperation

- Ref/MutRef are Hexal call modes, not stable foreign ABI value types.
- Box is a Hexal owner, not proof that a foreign pointer follows its allocator
  or destruction contract.
- Initial `extern c` declarations continue to use Ptr/MutPtr and RFC 0039's
  explicit ownership metadata.
- Ref/MutRef/Box are rejected in foreign signatures until the binding contract
  states borrow duration, transfer, retention, and deallocator.
- Foreign ownership is never inferred from pointer spelling.

## C23 lowering

```c
/* Box<T> */    T *
/* Ref<T> */    const T *
/* MutRef<T> */ T *
```

- Local fixedness qualifies the pointer object separately from pointee access.
- Box allocation uses checked sizeof(T) and alignof(T) through the default
  allocator backend and initializes T once.
- References and Box projections lower to direct dereference/member access.
- Moves are pointer assignments with no runtime move helper.
- Concrete drop helpers are deterministic, internal, demand-driven, and emitted
  once. Recursive graphs receive declarations before definitions.
- Box-or-Nil is one nullable T pointer.
- No runtime ownership or borrow registry is emitted.

## Diagnostics

The earliest proving phase diagnoses:

- wrong Box/Ref/MutRef arity or invalid T;
- reference in a non-parameter position;
- non-place reference argument or fixed place passed to MutRef;
- exact referent mismatch;
- overlapping borrows involving MutRef;
- mutation through Ref;
- move/replacement/cleanup during a call borrow;
- layout cycles not broken by Box;
- use, move, borrow, mutation, or cleanup after move;
- inconsistent branch/loop ownership state;
- move from an affine subplace;
- outstanding or duplicate cleanup;
- Box in a consumer not yet supporting moves;
- Box equality, ordering, hashing, or printing;
- unsupported foreign signatures.

Diagnostics name source bindings/places, never C names or internal flow states.

## Open decisions

### 1. Box cleanup

**A. Explicit cleanup obligations (provisional recommendation).**

```hexal
node := Box(Node(value = 1))
defer node.free()
```

Preserves current Hexal policy and source/C correspondence, but requires the
cleanup-bound state and generated deep-drop helpers.

**B. Snacc-style automatic destruction.**

```hexal
node := Box(Node(value = 1))
// automatically destroyed on ordinary scope exit
```

Lowest ceremony and best nested-owner composition, but introduces implicit
cleanup control flow and reverses RFC 0110's decision. Both options need move
analysis and recursive drop helpers.

Recommendation: A for consistency; choose B only as a deliberate language-wide
reversal.

### 2. Method receivers

- **A (recommended):** add Ref/MutRef receiver targets; retain Ptr/MutPtr only
  for raw APIs.
- **B:** keep T/Ptr/MutPtr receivers, leaving ordinary mutable methods dependent
  on raw pointers after safe reference parameters exist.

### 3. First-version Box positions

- **A:** implement affine collection and Task/Channel consumers immediately.
- **B (recommended):** initially permit bindings, parameters, results, objects,
  ADTs, unions, and Arrays; reject List/Dict storage and Task/Channel transfer
  until RFCs 0110/0118 implement consuming operations.

### 4. Boxable resources

- **A:** any storable T, requiring every resource to define deep cleanup.
- **B (recommended):** only T with an infallible compiler-known drop contract.
  Scalars, inline aggregates, String, List, Dict, and nested Box qualify once
  recursive drop exists. IO, Task, Channel, Mutex, Stash, Pool, and foreign
  resources remain rejected until they define compatible consuming cleanup.

## Required implementation sweep

Inventory and reconcile:

- compiler-owned type constructors and position eligibility;
- `ref place`, Ptr/MutPtr construction, and receiver adaptation;
- parameter/result/function-value checks;
- assignment, call, return, aggregate, and collection copy paths;
- branch/loop flow merges and recursive layout checking;
- Heap allocation/free lowering and allocator selection;
- nullable-pointer niche recognition;
- equality, truthiness, hashing, and print classification;
- Task/Channel copyability gates;
- View, Stash, and Pool provenance;
- C spelling, qualification, foreign signatures, component discovery, and
  drop-helper ordering;
- tests/snippets describing every pointer-like value as shallow-copyable.

Do not weaken Ptr/MutPtr to make them resemble Ref/MutRef. Their different
placement and lifetime contracts are intentional.

## Validation

This section becomes exhaustive after the four decisions are resolved. Before
implementation, replace each decision-dependent statement with the selected
rule.

- Box/Ref/MutRef accept exactly one valid T; all listed invalid placements fail.
- `Box(expression)` evaluates once; every alternative constructor spelling
  fails.
- Ref accepts fixed/writable places; MutRef accepts writable places only.
- Temporaries fail; widening/injection cannot adapt referent types.
- Ref/Ref overlap succeeds; every overlap involving MutRef fails; sibling
  fields succeed; ancestor/descendant places fail.
- Value arguments preserve left-to-right values beside a borrow of the same
  place; nested calls reborrow without escape.
- Box has pointer layout; Box-broken recursive layouts succeed and unbroken
  cycles fail.
- Fixed Box roots reject pointee mutation; mutable roots permit it.
- Box lends pointee or owner according to the exact Ref/MutRef expected type.
- Every consuming context moves Box and every later source use fails.
- Branch and loop state merges are conservative; subplace moves fail.
- Direct Box equality/order/hash/print fail; Box-or-Nil tests succeed and emit
  no union wrapper.
- The chosen cleanup model releases every allocation exactly once on all
  locally decidable ordinary paths, including recursive graphs and replacement.
- Explicit mode enforces cleanup obligations and cleanup-bound behavior;
  automatic mode emits reverse-initialization-order cleanup on every ordinary
  exit.
- Every selected position moves once; every deferred position fails explicitly.
- Ptr/MutPtr semantics and existing C lowering remain unchanged.
- Unsupported foreign signatures fail.
- Generated C adds no runtime ownership/borrow metadata and selects allocator
  and drop dependencies exactly once when demanded.
- Existing manifest hashes outside deliberate ownership, layout, and generated
  component changes do not move.
- Ordinary tests remain pure Go; tagged C23 tests compile/run representative
  allocation, move, borrow, recursive-layout, and cleanup cases on every
  qualified toolchain.

## Detailed implementation plan

### Phase 0: decisions and baseline

1. Resolve the four Open decisions and rewrite conditional contracts.
2. Reconcile RFC 0110 ownership/cleanup and RFC 0148 borrow terminology,
   recording exact supersession or dependency boundaries.
3. Inventory every Required implementation sweep site.
4. Record focused Ptr/MutPtr, layout, allocation, defer, aggregate, collection,
   concurrency, and snippet-manifest baselines.

### Phase 1: syntax and type properties

1. Parse/reserve Box, Ref, MutRef, and Box construction.
2. Add distinct resolved/checked identities; references are not Ptr aliases.
3. Implement placement/arity diagnostics.
4. Add copyability, affinity, borrow-mode, Boxable, and drop-contract
   classifications after generic substitution.
5. Propagate affinity through every selected aggregate family.

### Phase 2: recursive layout

1. Separate semantic dependency traversal from by-value layout traversal.
2. Stop only layout recursion at Box edges.
3. Validate direct, indirect, mutual, nested-Box, and invalid unbroken cycles.

### Phase 3: canonical places and call borrows

1. Centralize place identity as root plus field, payload, Box, and reference
   projections.
2. Add Ref/MutRef checked parameter modes and exact place matching.
3. Process arguments left to right, then validate the simultaneous borrow set.
4. Implement overlap, automatic callee dereference, reborrowing, and selected
   receiver adaptation.

### Phase 4: affine flow

1. Add available, moved, and selected cleanup states.
2. Mark every consuming checked-tree context explicitly.
3. Transfer obligations and invalidate sources through initialization,
   assignment, calls, returns, and enabled storage operations.
4. Merge state through if, match, loops, break, continue, return, try, defer,
   and errdefer.
5. Reject subplace moves and borrow/move/cleanup conflicts.

### Phase 5: cleanup

1. Implement the selected cleanup policy.
2. Derive concrete drop-required types and recursive drop plans.
3. Define member order, active-union cleanup, enabled collection element
   cleanup, and Box release order.
4. Implement either explicit obligations/cleanup-bound defer or automatic scope
   cleanup and move disarming.
5. Reject resources lacking the chosen infallible drop contract.

### Phase 6: C23 lowering

1. Add layer-correct Box/Ref/MutRef C spelling.
2. Lower Box allocation, references, projections, moves, and nullable niche.
3. Emit demand-driven drop declarations/definitions with recursive forward
   declarations.
4. Extend component discovery and include ordering without delegating wrappers.

### Phase 7: consumers and boundaries

1. Enable only selected v1 positions; reject deferred positions explicitly.
2. Update enabled aggregate/collection operations to distinguish copy and move.
3. Update Task/Channel gates through RFC 0118; never shallow-copy Box.
4. Preserve Stash/Pool provenance and raw pointer behavior.
5. Reject foreign use pending RFC 0039 ownership contracts.

### Phase 8: conformance and documentation

1. Add focused stage tests and exhaustive integration cases from Validation.
2. Add small workbench snippets for Box, Ref, MutRef, moves, recursion, and
   nullable Box.
3. Regenerate the manifest only for intentional generated-C changes and review
   its artifact-family blast radius.
4. Run ordinary and tagged C23 suites.
5. With explicit user approval, synchronize `docs/reference.md` once with the
   final grammar, placement, ownership, cleanup, pointer, allocation, and C23
   contracts.
6. Update status, rebuild/restart the workbench, and close only when code,
   tests, snippets, status, and reference agree.

## Consequences

Benefits:

- ownership becomes visible and checked;
- ordinary borrowing no longer exposes storable raw pointers;
- recursive owning structures become expressible;
- APIs state read-only versus exclusive mutable access directly;
- generated C remains pointer-sized and direct;
- C interoperation retains raw pointer fidelity.

Costs:

- the checker gains affine flow and canonical-place overlap analysis;
- Box-containing types require recursive drop planning;
- collections and Tasks need real move consumers before accepting Box;
- two pointer-like layers must remain sharply distinguished;
- explicit cleanup adds cleanup-bound state, while automatic cleanup adds
  source-invisible control flow.
