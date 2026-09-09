# RFC 0149: `Box<T>`, `Ref<T>`, and `Ref<mut T>`

- Kind: Feature Specification (Rust-Style RFC)
- Status: Design decisions required. All five of this RFC's own decisions are
  resolved (see Resolved decisions); what remains is one sequencing question
  Decision 3 created — whether affine collection and Task/Channel transfer
  rules are defined here or in RFCs 0110/0118. Third in the
  0154 -> 0153 -> 0149 chain
- Created: 2026-09-08
- Updated: 2026-09-09
- Origin: adapt Snacc's implemented ownership and call-scoped reference model
  to Hexal's C23, raw-pointer, allocator, and C-interoperability contracts
- Renamed by RFC 0154: originally drafted with a separate `MutRef<T>` type
  name; renamed throughout to `Ref<mut T>` (the writable variant is `Ref`
  with `mut` marking its type argument, not a differently-spelled type),
  fitting the unified `<T>`/`<mut T>` convention RFC 0154 establishes across
  `Ptr`, `Ref`, and `Borrow` (RFC 0153). This document already reflects that
  renaming; it was never implemented under the old name, so this is the
  current design, not a historical record of one. `Ptr<T>`/`MutPtr<T>` below
  are left in their current, implemented spelling — RFC 0154 proposes
  renaming those too, but that specific rename carries real migration cost
  and is not yet confirmed, unlike this one
- Coordinates with: RFC 0039 (foreign ownership), RFC 0110 (affine ownership,
  Stash/Pool lifetimes, and cleanup obligations), RFC 0118 (cross-Task
  ownership), RFC 0137 (borrowed-view provenance), RFC 0153 (bounded sequence
  borrows)
- Does not update `docs/reference.md`: this is a draft; synchronize the
  reference only after the remaining decisions are settled and implementation
  is approved

## Summary

Add three memory capabilities:

```text
Box<T>       one non-null, uniquely owned heap allocation containing T
Ref<T>       call-scoped read-only borrow of an existing T place
Ref<mut T>   call-scoped exclusive read-write borrow of an existing T place
```

Keep the existing raw-pointer layer:

```text
Ptr<T>      storable non-owning raw read pointer
MutPtr<T>   storable non-owning raw read-write pointer
```

- `Box<T>` carries ownership in its type and moves instead of copying.
- `Ref<T>` and `Ref<mut T>` are parameter modes, not values. They cannot
  escape their call and need no general lifetime syntax.
- `Ptr<T>` and `MutPtr<T>` remain first-class values for C interoperation,
  opaque handles, platform APIs, and low-level memory access. They carry no
  ownership or lifetime guarantee.
- `Borrow<T>`/`Borrow<mut T>` (RFC 0153) is the existing bounded sequence
  borrow — a different shape (pointer + length, for slicing a contiguous
  range) from `Ref`/`Ref<mut T>` (a borrow of one whole T place). RFC 0153
  separately owns its own naming and semantics.

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
- Reuse one place-identity and flow-state model for moves, references,
  Borrows, Stash allocations, and Pool slots.
- Keep allocation failure consistent with default allocation: failure or
  unrepresentable size traps.

## Non-goals

- Garbage collection, reference counting, shared ownership, or weak ownership.
- General lifetime parameters or Rust-style lifetime syntax.
- Making foreign memory safe merely by changing its pointer type.
- Pointer arithmetic or pointer casts.
- Settling RFC 0153's `Borrow` semantics or naming.
- Replacing typed `Stash<T>` or `Pool<T>` allocation policies.
- Inferring foreign ownership from a C pointer type.

## Syntax

Conceptual grammar additions:

```ebnf
special-form-type-constructor = existing-special-form
                              | "Box"
                              | "Ref" ;
ref-type-argument = "mut" , type-expression | type-expression ;
box-expression = "Box" , "(" , expression , ")" ;
```

```hexal
node := Box(Node(value = 1))
```

- `Box<T>` takes exactly one type argument. `Ref<...>` takes exactly one type
  argument, optionally `mut`-marked (`Ref<T>` vs `Ref<mut T>`) — the same
  `mut`-inside-`<...>` convention RFC 0154 establishes for `Ptr` and
  `Borrow`, not a separate `MutRef` type name.
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

fun increment(node: Ref<mut Node>) do
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
- `Ref<mut T>` accepts only writable places of exact type T.
- Inside the callee, the parameter name denotes the referent. Reads, field
  selection, method calls, and whole-value assignment act on caller storage.
- Assignment and mutating methods are invalid through `Ref<T>`.
- Passing a reference parameter to another compatible reference parameter
  reborrows it for that nested call.
- Passing a reference parameter to a by-value T parameter reads its current
  value. An affine T cannot thereby be copied.

### Placement

`Ref<T>`/`Ref<mut T>` are not value types. They are valid only as direct
function or explicit method parameters and, per Resolved decision 2,
method receiver targets.

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

- overlapping `Ref<T>` arguments may coexist;
- `Ref<mut T>` may not overlap any `Ref<T>` or `Ref<mut T>` in the same call;
- a move, replacement, cleanup, or structural invalidation of the referent is
  forbidden while the call borrow exists;
- arguments evaluate once, left to right;
- value arguments retain their evaluated values; place identities are retained
  for reference arguments; simultaneous borrow checking occurs after all
  arguments are evaluated.

```hexal
fun exchange(left: Ref<mut Int32>, right: Ref<mut Int32>) do
    previous := left
    left = right
    right = previous
end

exchange(point.x, point.y) // valid
exchange(value, value)     // rejected
```

Overlap is proved from canonical roots and projections, never runtime address
comparison.

## Extension: block-scoped references (`with`)

Raised in later discussion of a full storable-borrow model (`Borrow<T>`/
`Borrow<mut T>` with general lifetime tracking, à la Rust): that model needs a
real borrow checker — computed lifetimes, moves coupled to live borrows,
some answer for interprocedural borrows (lifetime parameters or an
equivalent). This repo's own specs already show what that costs: RFC 0137's
narrowest possible slice of it (does a directly-returned borrow dangle)
needed its own provenance representation and a 4-phase plan, and still
punted List/Dict/Task-mediated escapes to RFC 0110/0118, both still open.
This RFC's whole reason for choosing call-scoped `Ref<T>`/`Ref<mut T>` over
storable borrows was to avoid exactly that cost: **the cost collapses
specifically when a borrow's scope is a syntactic fact instead of something
the compiler has to compute.** A call's argument list is syntactically
bounded (starts at the call, ends when it returns) — that's the entire
reason Semantics "Exclusivity" above can state its overlap rule without any
lifetime inference at all.

A lexical block is *also* syntactically bounded, just a larger one than a
single call. That observation extends call-scoping to a `with` statement
without paying the general-lifetime cost, using this RFC's own `Ref`/
`Ref<mut T>` — not a separate `Borrow`/`Borrow<mut T>` naming, which would
just be a second name for the same "borrow of a whole T place" concept this
RFC already has:

```hexal
with scan: Ref<Error> of err do
    print(scan.message)
end

with scan: Ref<mut Error> of err do
    scan.set_code(500)
    clear_bytes(scan.message_bytes)   // reborrows scan for the nested call
end
```

- `with <name>: Ref<T> of <place> do ... end` binds `<name>` as `Ref<T>` for
  the block's dynamic extent; `with <name>: Ref<mut T> of <place> do ... end`
  binds `Ref<mut T>`, requiring `<place>` to be writable. The declared type is
  written explicitly (Resolved decision 5) so mutability is marked inside the
  type argument, per RFC 0154, rather than by a statement-level `mut` that
  already means something else.
- This adds a third valid position for `Ref<T>`/`Ref<mut T>` alongside
  parameters and method receivers (Resolved decision 2): a
  `with`-bound name. Everything else in Placement continues to hold — the
  bound name still cannot be returned, stored in a member, boxed, or
  otherwise escape the block, by the same construction that already makes
  escape impossible for a parameter.
- Exclusivity extends unchanged: `<place>` cannot be read, written, moved,
  or reborrowed from outside the block for its dynamic extent, checked the
  same way a moved binding becomes unavailable and later available again —
  a syntactic scope, not a computed one. Reborrowing into a nested call
  inside the block works exactly as it already does for a parameter.
- This is a strict subset of the storable-borrow model that motivated it:
  no `Borrow`/`Borrow<mut T>`-shaped value ever exists to store in a `let`
  or return from a function, so there is still no dangling-reference or
  aliasing bug possible across a function boundary — because there is no
  "across a function boundary" for one of these borrows to survive to.
  What's given up, deliberately, is holding a borrow past one lexical block:
  exactly the boundary that keeps this cheap.

Not yet resolved: whether `with` needs its own exclusivity proof pass
distinct from a call's (a block can contain arbitrarily more statements
than one argument list, though the same "prove from canonical roots and
projections" method should still apply), and whether nested `with` blocks
over overlapping places need anything beyond the existing overlap rule
applied at each block's own scope.

## Box semantics

- Box owns exactly one non-null default-allocator allocation containing one
  initialized T.
- Its representation is one pointer, independent of T.
- Box is affine even when T is copyable. An aggregate containing Box is affine
  transitively for every consumer, which Resolved decision 3 makes the full
  position set.
- `Box<Box<T>>` is valid. `Box<Ref<T>>` and `Box<Ref<mut T>>` are invalid.
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
- A Box<T> place automatically lends its pointee to `Ref<T>`.
- A mutable Box<T> place automatically lends its pointee to `Ref<mut T>`.
- The Box place itself may bind to `Ref<Box<T>>` or `Ref<mut Box<T>>` when
  that is the exact declared parameter type.
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

Resolved decision 1 confirms this explicit form; automatic destruction is
rejected.

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
matrix is the full set below: Resolved decision 3 admits every position in
the first version. The model is:

- object/ADT/union construction moves Box fields or payloads;
- union narrowing borrows a Box payload unless a later consuming extraction is
  added;
- Array/List/Dict-value insertion moves Box values;
- replacement/removal transfers or discharges one obligation;
- Dict keys cannot contain Box;
- Task arguments/results and Channel elements move Box rather than copying it.

List/Dict and cross-Task movement need affine move analysis and transfer
rules that RFCs 0110 and 0118 own. Resolved decision 3 admits them anyway, so
implementation must first settle where those rules are defined: here, with
0110/0118 adopting them, or there, with this RFC sequenced behind both.
Defining them independently in two places is the failure to avoid.

## Methods

The intended safe receiver model is:

```hexal
method Ref<Counter>.read(): Int32 do
    return self.value
end

method Ref<mut Counter>.increment() do
    self.value = self.value + 1
end
```

`Ref<T>` receivers read; `Ref<mut T>` receivers read and write. Value and Box
places adapt only for the call. Raw Ptr/MutPtr receivers remain for genuinely
raw APIs. Resolved decision 2 keeps both legal: `Ref` receivers are added,
and `Ptr` receivers are not narrowed by rule until unsafe boundaries are
designed.

## C interoperation

- `Ref<T>`/`Ref<mut T>` are Hexal call modes, not stable foreign ABI value
  types.
- Box is a Hexal owner, not proof that a foreign pointer follows its allocator
  or destruction contract.
- Initial `extern c` declarations continue to use Ptr/MutPtr and RFC 0039's
  explicit ownership metadata.
- `Ref<T>`/`Ref<mut T>`/Box are rejected in foreign signatures until the
  binding contract states borrow duration, transfer, retention, and
  deallocator.
- Foreign ownership is never inferred from pointer spelling.

## C23 lowering

```c
/* Box<T> */      T *
/* Ref<T> */      const T *
/* Ref<mut T> */  T *
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

- wrong Box/Ref arity or invalid T (Ref optionally `mut`-marked);
- reference in a non-parameter position;
- non-place reference argument or fixed place passed to `Ref<mut T>`;
- exact referent mismatch;
- overlapping borrows involving `Ref<mut T>`;
- mutation through `Ref<T>`;
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

## Resolved decisions

All five decisions this RFC carried are settled. They are recorded with their
reasoning so the trade behind each stays visible.

### 1. Box cleanup — explicit `free()`

Option A. Box carries a cleanup obligation; an outstanding obligation at
scope exit is a Type Error, and the `cleanup-bound` binding above keeps
`defer` usable. Automatic destruction is rejected.

The reason is narrower than it used to be, and worth stating precisely. RFC
0110 once rejected destructors because "a destructor cannot receive an
allocator, and Hexal's cleanup always needs one". That argument no longer
holds: `Stash.destroy()`, `Pool.destroy()`, and `Box.free()` all take no
allocator, and RFC 0110 has since withdrawn it -- "lack of an allocator
argument is therefore not the deciding objection". What remains is the
objection that survives: cleanup stays written in the source, so generated C
corresponds to operations the author wrote. That is a product goal, not a
technical constraint, and it is the whole of the reason.

### 2. Method receivers — add `Ref`, keep `Ptr` legal for now

Option A's addition, without its restriction. `Ref<T>`/`Ref<mut T>` become
receiver targets; `Ptr<T>`/`Ptr<mut T>` receivers remain legal everywhere
rather than being narrowed to raw APIs by rule.

`Ref` receivers are not optional: calling a mutating method on a `Box<T>`
needs a writable receiver, and if the only one were `Ptr<mut T>`, Box would
have to hand out a raw pointer to be usable with methods at all -- which
dissolves the reason Box exists.

Leaving `Ptr` receivers legal does mean two ways to write the same method
with no rule choosing between them. That is accepted deliberately and
deferred: when unsafe boundaries are designed, `Ptr` receivers are the
natural thing to confine to that boundary. Until then this RFC adds a form
rather than removing one, which is the smaller change.

### 3. First-version Box positions — all positions now

Option A, against this RFC's earlier recommendation. Affine collection
storage and Task/Channel transfer are implemented in the first version:
List/Dict values move Box, replacement and removal transfer or discharge one
obligation, and Task arguments/results and Channel elements move rather than
copy.

**This materially enlarges the RFC and creates a real dependency.** Those
positions need affine move analysis that RFC 0110 owns and cross-Task
transfer rules that RFC 0118 owns. Implementing them here means either
sequencing this RFC behind both, or defining the move and transfer rules in
this RFC and having 0110/0118 adopt them rather than invent their own. The
second is viable but must be explicit: two RFCs independently defining affine
transfer for the same positions is how the union-coverage class of defect
gets built on purpose.

Implementation must settle that ownership question before Phase 1. The
alternative that was rejected -- shipping bindings, parameters, results, and
inline aggregates first -- deferred exactly the case most people want, a
collection of owned values, which is why it was rejected.

### 4. Boxable resources — infallible drop only, as a design boundary

Option B's restriction, restated. Only `T` with an infallible
compiler-known drop contract is Boxable. Scalars, inline aggregates, String,
List, Dict, and nested Box qualify once recursive drop exists.

IO, Task, Channel, Mutex, Stash, Pool, and foreign resources are excluded --
and this is a **design boundary, not a queue that empties on its own**. An
earlier phrasing said they were rejected "until they define compatible
consuming cleanup", which reads as scheduled work that nothing schedules.
The real obstacle is a question no RFC currently owns:

> What does a failing drop do?

`IO.close()` is fallible. A destructor-shaped cleanup has no way to return
that failure, so the options are to trap, to discard the error silently, or
to keep such resources permanently non-Boxable. Until that question is
answered by its own RFC, `Box<IO>` is not deferred -- it is impossible, and
saying so plainly is better than implying otherwise.

### 5. `with`-block bound-name spelling — explicit type

Option B: `with scan: Ref<mut Error> of err do ... end`.

Option A (`with mut scan of err`) was recommended by an earlier draft for
reusing the existing statement-level `mut` position. That reuse is the
problem. Statement-level `mut` means the *binding is reassignable*, not that
the target is writable -- `docs/reference.md` states it directly: "A fixed
handle can mutate its List; `mut` only reassigns the handle." Spelling a
writable borrow as `with mut scan` would give `mut` a second, contradictory
meaning in the same syntactic position.

That is precisely the overload RFC 0154 exists to remove. Its convention is
that mutability for capability-over-a-target types is marked inside the type
argument -- `Ptr<mut T>`, `Ref<mut T>`, `Borrow<mut T>` -- so the `with`
binding marks it the same way. More verbose, and consistent with the rule the
chain just established.

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
- `Borrow`, Stash, and Pool provenance;
- C spelling, qualification, foreign signatures, component discovery, and
  drop-helper ordering;
- tests/snippets describing every pointer-like value as shallow-copyable.

Do not weaken Ptr/MutPtr to make them resemble `Ref<T>`/`Ref<mut T>`. Their
different placement and lifetime contracts are intentional.

## Validation

This section becomes exhaustive after the four decisions are resolved. Before
implementation, replace each decision-dependent statement with the selected
rule.

- Box/Ref accept exactly one valid T (Ref optionally `mut`-marked); all
  listed invalid placements fail.
- `Box(expression)` evaluates once; every alternative constructor spelling
  fails.
- `Ref<T>` accepts fixed/writable places; `Ref<mut T>` accepts writable
  places only.
- Temporaries fail; widening/injection cannot adapt referent types.
- `Ref<T>`/`Ref<T>` overlap succeeds; every overlap involving `Ref<mut T>`
  fails; sibling fields succeed; ancestor/descendant places fail.
- Value arguments preserve left-to-right values beside a borrow of the same
  place; nested calls reborrow without escape.
- Box has pointer layout; Box-broken recursive layouts succeed and unbroken
  cycles fail.
- Fixed Box roots reject pointee mutation; mutable roots permit it.
- Box lends pointee or owner according to the exact `Ref<T>`/`Ref<mut T>`
  expected type.
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

1. Settle Resolved decision 3's sequencing question -- whether affine
   collection and Task/Channel transfer rules are defined in this RFC or in
   RFCs 0110/0118 -- before any other phase begins. Every other decision is
   resolved; conditional contracts have already been rewritten.
2. Reconcile RFC 0110 ownership/cleanup and RFC 0153 borrow terminology,
   recording exact supersession or dependency boundaries.
3. Inventory every Required implementation sweep site.
4. Record focused Ptr/MutPtr, layout, allocation, defer, aggregate, collection,
   concurrency, and snippet-manifest baselines.

### Phase 1: syntax and type properties

1. Parse/reserve Box, Ref (plain and `mut`-argument forms), and Box
   construction.
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
2. Add `Ref<T>`/`Ref<mut T>` checked parameter modes and exact place matching.
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

1. Add layer-correct Box/`Ref<T>`/`Ref<mut T>` C spelling.
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
2. Add small workbench snippets for Box, `Ref<T>`, `Ref<mut T>`, moves,
   recursion, and nullable Box.
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
