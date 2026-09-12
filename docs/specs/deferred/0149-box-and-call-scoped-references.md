# RFC 0149: `Box<T>` and Scoped References

- Kind: Feature Specification (Rust-Style RFC)
- Status: Blocked, 2026-09-10. RFC 0165 invalidates this RFC's Ref and Box designs
  and rejects affine ownership, implicit moves, and automatic cleanup as non-goals.
  Do not implement as written; any independently retained work requires a later rescope.
- Created: 2026-09-08
- Updated: 2026-09-10
- Origin: adapt Snacc's ownership and scoped reference model to Hexal's
  C23, allocator, raw-pointer, and C-interoperability contracts
- Depends on: RFC 0154 (`<T>` / `<mut T>` capability spelling)
- Coordinates with: RFC 0039 (foreign ownership), RFC 0110 (affine ownership
  for existing owners), RFC 0118 (cross-Task ownership), and RFC 0153
  (scoped bounded sequence borrows)

## Summary

Add three memory capabilities:

```text
Box<T>       one non-null, uniquely owned default-heap allocation containing T
Ref<T>       call- or lexical-block-scoped read-only borrow of one T place
Ref<mut T>   call- or lexical-block-scoped exclusive writable borrow of one T place
```

Keep the existing raw-pointer layer:

```text
Ptr<T>       storable non-owning raw read pointer
Ptr<mut T>   storable non-owning raw read-write pointer
```

- Box carries ownership, moves implicitly, and is destroyed deterministically.
- Ref modes are parameter/receiver or lexical-block capabilities, not values.
  They cannot escape their dynamic call or `borrow` block.
- Slice modes are the corresponding scoped contiguous-range capabilities
  defined by RFC 0153.
- Raw Ptr remains available for C interoperation, opaque handles, platform
  APIs, and low-level memory. It carries no ownership or lifetime guarantee.
- There is no `copy` or `move` keyword.

## Goals

- Make ownership of one independently allocated object explicit.
- Reject locally decidable use-after-move, duplicate ownership, double cleanup,
  and overlapping mutable capabilities.
- Make ordinary borrowing low ceremony: callers pass expressions without
  address-of or dereference syntax.
- Support recursive data structures through explicit Box edges.
- Preserve pointer-sized, zero-overhead C representations.
- Preserve raw pointers for C ABI fidelity and low-level code.
- Reuse one canonical-place and scoped-capability model for Ref and Slice.
- Keep allocation failure consistent with default allocation: failure or
  unrepresentable size traps.

## Non-goals

- Garbage collection, reference counting, shared Box, weak ownership, or
  explicit move syntax.
- General lifetime parameters or storable references.
- Making foreign memory safe merely by changing its pointer type.
- Pointer arithmetic or pointer casts.
- Replacing typed Stash or Pool allocation policies.
- Inferring foreign ownership from a C pointer type.

## Syntax

```ebnf
special-form-type-constructor = existing-special-form | "Box" | "Ref" ;
ref-type-argument = "mut" , type-expression | type-expression ;
box-expression = "Box" , "(" , expression , ")" ;
borrow-statement = "borrow" , identifier , "from" , [ "mut" ] , expression ,
                   "do" , { statement } , "end" ;
```

```hexal
node := Box(Node(value = 1))
```

- Box and Ref take exactly one type argument; Ref optionally marks it `mut`.
- `Box(expression)` evaluates its operand exactly once and produces Box<T>,
  where the checked operand determines T.
- There is no `.new()`, explicit Heap argument, lowercase `box`, or implicit
  conversion between T and Box<T>.
- Expected Box<T> context flows into the operand where ordinary contextual
  typing permits it.

## Scoped references

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

- The caller supplies an initialized place or, for Ref<T> only, a fresh
  temporary; no `ref`, `&`, or dereference expression is used.
- Ref<T> accepts fixed or writable referents of exact T.
- Ref<mut T> accepts only existing writable referent places of exact T. A fresh
  temporary is rejected because its mutations would not be observable after
  the call.
- Inside the callee, the parameter name denotes the referent. Reads, field
  selection, calls, and whole-value assignment operate on caller storage.
- Assignment and mutating methods are invalid through Ref<T>.
- Passing a Ref parameter to a compatible Ref parameter reborrows it for the
  nested call.
- Passing a Ref parameter to a by-value parameter reads its current value. A
  copyable T copies; an affine T is rejected because borrowing never grants
  permission to move the caller's owner.
- A Ref never owns its referent. An operation that consumes the referent itself,
  including `Box.free()`, is invalid through both Ref modes. Whole-value
  assignment through `Ref<mut T>` is replacement rather than extraction: it is
  valid when T has an infallible drop plan and leaves the caller's place
  initialized.

### Placement and escape

Ref is valid only as a direct function parameter, implicit method receiver, or
the bound capability of `borrow ... do ... end`.
A Ref parameter mode may appear in an exact `Fun` signature: the function value
stores code, not a Ref capability, and each invocation still creates a fresh
call-scoped capability. Ref remains invalid as a result, binding, member,
payload, union member, collection argument, Box argument, pointer pointee,
function-value result, Task/Channel value, or nested Ref argument. There is no
Ref literal, equality, printing, nullability, address-of syntax, or dereference
syntax.

Taking `ref` of a Ref-derived place is rejected because that would manufacture
a storable raw pointer whose lifetime exceeds the call capability. Passing a
Ref-derived address to a raw Ptr parameter or foreign declaration is rejected
for the same reason. A foreign declaration may eventually accept Ref directly
only when RFC 0039 states that the C callee does not retain the lowered pointer.

### Referents and writability

- T is a complete finite type valid at the referent place.
- Matching is exact after transparent-alias resolution; widening, union
  injection, pointer weakening, and literal conversion do not adapt T.
- A selected field is a place. Distinct sibling fields do not overlap; a place
  and its ancestor or descendant do.
- Writability belongs to the selected place, not merely its root binding.
  Inline storage requires a writable root. A fixed owning handle may expose
  writable referent storage where its existing contract permits interior
  mutation, as List indexing does.
- A narrowed union place remains borrowable only while narrowing is valid.
- Non-place expressions are accepted only for read-only Ref when they are fresh
  temporaries materialized for this call. Existing call results cannot be
  treated as aliases to some other place.

### Lexical borrow blocks

```hexal
borrow item from value do
    inspect(item)
end

borrow item from mut value do
    item.count = item.count + 1
end
```

- A plain place source creates Ref<T>; `mut` before a place source creates
  Ref<mut T> and requires an existing writable place.
- A fresh read-only source temporary lives through the complete borrow block
  and is dropped after the capability ends. A fresh writable source remains
  invalid because no caller-visible place would retain its mutations.
- A Slice, RuneCursor, or Bytes-producing expression supplies its already
  defined capability/descriptor mode and does not use the optional `mut`.
- The bound name denotes the borrowed referent or descriptor only inside the
  body. It cannot be moved, returned, stored, captured, converted to raw Ptr, or
  used afterward.
- The source root remains active for the complete block. Nested calls and
  nested borrow blocks validate against that capability through the same
  canonical-root engine.
- A `return`, `break`, `continue`, or `try` propagation that leaves the block
  ends its capability before continuing outward. The block emits no runtime
  borrow object or ownership metadata.

### Exclusivity

Borrowing lasts exactly for the dynamic call. Ref and Slice use one combined
capability set:

- overlapping read-only Ref/Slice capabilities with the same root may coexist;
- any same-root set containing `Ref<mut T>` or `Slice<mut T>` is rejected,
  including a writable whole-place Ref overlapping a Slice of that place;
- distinct sibling-field roots may coexist;
- move, replacement, cleanup, or structural invalidation of a referent is
  forbidden in that call's simultaneous capability set;
- arguments evaluate once, left to right; and
- value arguments retain evaluated values while reference arguments retain
  canonical place identities, with simultaneous validation after evaluation.

Capabilities received by the current function remain active for its complete
dynamic call. Every nested call validates its new argument capabilities against
both its own simultaneous arguments and all enclosing active capabilities. A
nested call therefore cannot obtain mutable access to storage already borrowed
read-only by an outer call.

```hexal
fun exchange(left: Ref<mut Int32>, right: Ref<mut Int32>) do
    previous := left
    left = right
    right = previous
end

exchange(point.x, point.y) // valid
exchange(value, value)     // rejected
```

`previous := left` copies the Int32 referent; it does not create a Ref binding.
Overlap is proved from canonical roots and projections, never runtime address
comparison.

## Box semantics

- Box owns exactly one non-null default-allocator allocation containing one
  initialized T.
- Its representation is one pointer, independent of T.
- Box is affine even when T is copyable. An aggregate containing Box is affine
  transitively in every position enabled here.
- Box<Box<T>> is valid; Box<Ref<T>>, Box<Ref<mut T>>, and Box<Slice<T>> are
  invalid.
- Box itself is never Nil. Absence is `Box<T> | Nil`, using the null-pointer
  niche with no tagged wrapper.
- Box has no direct equality, ordering, hashing, or printing. Box-or-Nil may be
  compared with Nil and tested for truthiness; non-null Box is truthy.

### Recursive layout

```hexal
type Node is struct
    value: Int32,
    next: Box<Node> | Nil,
end
```

A by-value layout cycle remains invalid unless every recursive path crosses a
Box edge. Layout size/alignment traversal stops at Box; semantic dependency
traversal does not. Mutually recursive nominal types are valid when Box breaks
every layout cycle.

### Access and borrowing

- Field selection and method dispatch automatically traverse Box layers needed
  to reach the selected member without copying or consuming Box.
- Compiler-owned Box members, including `free`, take precedence over a same-
  named pointee member. A pointee method remains reachable through its declared
  static receiver when no Box member matches.
- A fixed Box root permits read-only pointee access. A mutable Box root permits
  read-write pointee access and Box replacement.
- A Box<T> place lends its pointee to Ref<T>; a mutable Box<T> place lends it to
  Ref<mut T>. Exact Ref<Box<T>> modes borrow the owner rather than its pointee.
- A temporary Box may receive a method call. It remains alive through that
  call and is then destroyed automatically.
- There is no general implicit Box-to-T or Box-to-Ptr conversion.

### Moves and duplication

Initialization, assignment, by-value argument, return, and aggregate
construction implicitly move an affine value and make its source unavailable:

```hexal
first := Box(Node(value = 1))
second := first
inspect(second)
inspect(first) // Type Error: first was moved
```

- A consuming context is determined by the destination/parameter/result type;
  no `move` keyword exists.
- Copyable values continue to copy by ordinary assignment and passing.
- Reassigning an initialized affine destination first destroys its old value,
  then moves in the new value.
- Moving an affine field or element out of an aggregate is rejected in v1;
  whole-owner moves remain valid.
- Branch merges retain availability only when every continuing path agrees.
  Loop back-edges preserve the loop-head ownership state.
- Moves lower to ordinary pointer/value copies; move state is checker-only.

## Deterministic cleanup

Box is destroyed automatically on every ordinary exit from the scope that owns
it. Destruction recursively releases owned values in T according to their
infallible drop contracts, then releases the Box allocation. Initialization
order determines cleanup order: later initialized locals are destroyed first;
aggregate members and Array elements are destroyed in reverse declaration or
index order; only the active union/ADT payload is destroyed.

Explicit `box.free()` performs the same consuming destruction early and makes
the binding unavailable. Registering a consuming affine cleanup with `defer` or
`errdefer` is rejected: automatic destruction already covers every ordinary
exit, and retaining cleanup-bound state would add a second lifetime model.

No ownership header, reference count, live bit, or runtime cleanup registry is
emitted. Process traps need not run cleanup, matching the existing cleanup
contract.

Only T with an infallible compiler-known drop plan is Boxable. Scalars, inline
aggregates, String, Error, List, Dict, and nested Box qualify once RFC 0110 defines
their recursive plans. Literal-backed String storage is a no-op only for
compiler-generated automatic drop; an explicit user `String.free(heap)` on a
proved literal remains RFC 0138's error. IO, Task, Channel, Mutex, Stash, Pool,
and foreign resources are not Boxable. A fallible resource such as IO requires
explicit close/error handling and cannot participate in implicit drop.

Before RFC 0110 lands, `Box<String>`, `Box<Error>`, `Box<List<T>>`, and
`Box<Dict<K, V>>` are rejected because their recursive ownership/drop plans do
not yet exist. RFC 0149 alone enables scalars, eligible inline aggregates, and
recursive Box graphs.

## Existing allocation families

- Use Box for an idiomatic independently owned Hexal object.
- Use Heap.allocate for a raw stable address and manual pointer protocol.
- Use Stash<T> for typed bulk lifetime and reset.
- Use Pool<T> for fixed-capacity reusable slots.
- Use raw Ptr for C, platform, opaque, and low-level addresses.

## Aggregates, collections, and Tasks

Box is valid in bindings, parameters, results, nominal aggregates, structural
unions, and fixed Arrays. Construction implicitly moves named Box operands;
the aggregate owns them and becomes affine. Automatic aggregate destruction
recursively drops them in the defined order. Union narrowing borrows a Box
payload; affine subplace extraction remains rejected.

List<Box<T>>, Dict<K,Box<T>>, and corresponding nested owners remain rejected
until RFC 0110 defines collection movement and cleanup. Task/Channel positions
remain rejected until RFC 0118 defines cross-Task transfer. `Slice<Box<T>>` is
valid only as RFC 0153's direct-call or lexical-block capability: it borrows each Box element and
never stores, copies, or moves one.

## Methods

```hexal
method Counter.read(): Int32 do
    return self.value
end

method mut Counter.increment() do
    self.value = self.value + 1
end
```

`method T.name` receives implicit Ref<T>; `method mut T.name` receives implicit
Ref<mut T>. Methods remain compiler-table functions and lower to ordinary C
functions with an explicit pointer parameter; structs gain no function-pointer
members. Methods declared directly on raw Ptr remain available for raw APIs.

## C interoperation

- Ref modes are Hexal call capabilities, not stable foreign ABI value types.
- Box does not prove that a foreign pointer follows its allocator or cleanup
  contract and has no general Box-to-Ptr conversion.
- RFC 0039 may permit a foreign parameter declared as non-retaining Ref and
  lower it directly to the corresponding C pointer. Retention or ownership
  transfer requires explicit foreign metadata.
- Until that contract lands, extern declarations use raw Ptr and reject Box or
  Ref. Foreign ownership is never inferred from pointer spelling.

## C23 lowering

```c
/* Box<T> */      T *
/* Ref<T> */      const T *
/* Ref<mut T> */  T *
```

- Local fixedness qualifies the pointer object separately from pointee access.
- Box allocation uses checked sizeof(T) and alignof(T) through the default
  allocator backend and initializes T once.
- Ref and Box projections lower to direct dereference/member access.
- Moves are pointer/value assignments with no runtime helper.
- Concrete drop helpers are deterministic, internal, demand-driven, and emitted
  once; recursive graphs receive declarations before definitions.
- Box-or-Nil is one nullable T pointer.

## Diagnostics

The earliest proving phase diagnoses:

- invalid Box/Ref arity, T, or placement;
- invalid reference capability, referent mismatch, or writable requirement;
- overlapping capabilities involving Ref<mut T>;
- mutation through Ref<T>;
- raw-address escape from a Ref-derived place;
- layout cycles not broken by Box;
- use, borrow, mutation, cleanup, or second move after move;
- inconsistent branch/loop ownership state or affine subplace movement;
- Box equality, ordering, hashing, printing, or unsupported consumer position;
- Box over a type without an infallible drop plan; and
- unsupported foreign signatures.

Diagnostics name source bindings and places, never C names or internal states.

## Reference synchronization

With explicit user approval after behavior stabilizes, update
`docs/reference.md` once with Box/Ref grammar, placement, recursive layout,
implicit moves, deterministic Box cleanup, lexical borrow blocks, combined
Ref/Slice overlap, enclosing-call capability tracking, receiver modes, and C23
representations. Remove the current rules that deny moves, borrow states, and
implicit cleanup only when the implementation satisfies this RFC's complete
Validation section.

## Required implementation sweep

Inventory and reconcile:

- compiler-owned type constructors, eligibility, and positions;
- raw `ref place`, lexical `borrow` parsing/scope exits, receiver adaptation,
  and Ref-derived pointer escape;
- parameter/result/function-value and read-only temporary materialization paths,
  including rejection of writable temporary references;
- assignment, call, return, aggregate, and collection copy/move paths;
- branch/loop flow merges and recursive layout checking;
- automatic cleanup insertion on every ordinary scope/control-flow exit;
- explicit early free and reassignment drop;
- Heap allocation lowering, nullable niche recognition, and drop-helper order;
- equality, truthiness, hashing, printing, Task/Channel gates, foreign
  signatures, component discovery, tests, snippets, and manifest entries.

## Validation

This section is exhaustive.

- Box/Ref accept exactly one valid T; every invalid placement fails.
- Box(expression) evaluates once; alternative constructor spellings fail.
- Ref<T> accepts exact fixed/writable places and fresh temporaries; Ref<mut T>
  accepts only exact existing writable places and rejects fresh temporaries.
- Exact `Fun` parameter signatures may contain Ref modes; Fun results and every
  stored Ref capability remain invalid.
- Widening/injection cannot adapt referent types.
- Two overlapping read-only Refs succeed; every overlap involving mutable Ref
  fails; siblings succeed; ancestor/descendant places fail.
- Cross-family Ref/Slice overlap uses the same canonical roots: same-root
  read-only capabilities coexist, while any same-root set containing a mutable
  capability fails.
- A nested call is checked against Ref/Slice capabilities active in every
  enclosing call.
- Value arguments preserve left-to-right evaluation; nested calls reborrow.
- `borrow name from place do ... end` creates a read-only lexical Ref;
  `borrow name from mut place do ... end` creates a writable lexical Ref.
  Both end on every control-flow exit from the block and reject escape, raw
  conversion, capture, storage, and conflicting nested capabilities.
- A read-only temporary borrowed by a lexical block is evaluated once, remains
  alive through the body, and is dropped after capability release; its writable
  form is rejected.
- `borrow` is reserved, malformed blocks report syntax diagnostics through the
  ordinary `do ... end` recovery path, and the bound name is a fresh immutable
  source name whose referent writability is determined by its capability mode.
- No Ref exists as an ordinary named value, result, stored field, collection
  element, Box argument, or raw Ptr conversion.
- Method receiver modes enforce read-only/writable self without changing T's
  layout; read-only temporary receivers live through one call and are then
  dropped, while writable methods reject temporary receivers.
- Ownership-consuming methods fail through either Ref mode; whole-value
  replacement through `Ref<mut T>` drops the previous value and leaves the
  referent initialized.
- Box has pointer layout; Box-broken recursion succeeds and unbroken cycles
  fail.
- Before RFC 0110, Box over String, Error, List, or Dict fails explicitly for
  lacking a recursive drop plan.
- Fixed Box roots reject mutation; mutable roots permit it.
- Box lends pointee or owner according to the exact expected Ref mode.
- Every affine transfer is implicit; every later source use fails; copyable
  values retain ordinary copy behavior.
- Reassignment drops the old destination; affine subplace moves fail.
- Automatic cleanup releases every Box allocation exactly once on every
  ordinary path, recursively and through the shared LIFO cleanup order.
- Full-expression Box temporaries, partial initialization followed by `try`,
  self-move rejection, and deferred affine-capture rejection follow RFC 0110's
  settled cleanup rules.
- Explicit free consumes early; deferred consuming cleanup is rejected; traps
  need not clean up.
- Objects, ADTs, unions, and Arrays own Box transitively and auto-drop it.
- Slice<Box<T>> borrows elements only for its direct call or lexical borrow
  block and cannot copy or move them.
- List/Dict and Task/Channel Box positions fail until RFCs 0110/0118.
- Raw Ptr behavior remains unchanged apart from RFC 0154 spelling.
- Unsupported foreign Box/Ref signatures fail pending RFC 0039.
- Generated C adds no ownership/borrow registry and emits required allocator
  and drop dependencies once.
- Manifest hashes outside deliberate ownership/layout/component changes do not
  move; ordinary and qualified C23 suites cover representative cases.

## Detailed implementation plan

1. Land RFC 0154 spelling and inventory every affected dispatch/consumer.
2. Add Box/Ref identities, position checks, drop classification, and recursive
   layout traversal.
3. Reserve `borrow`; parse/check lexical borrow blocks with balanced exit on
   every control-flow path; implement canonical places, read-only
   temporary materialization, one combined Ref/Slice capability set,
   enclosing-call capability tracking, method receivers, and raw-address escape
   rejection.
4. Add available/moved flow, implicit consuming contexts, branch/loop merges,
   subplace rejection, and reassignment drop.
5. Derive explicit checked-tree drop schedules for temporaries, partial
   construction, `try`, replacements, and ordinary exits; add explicit early
   Box.free and reject self-moves, deferred affine capture, and deferred
   consuming cleanup.
6. Lower Box/Ref, nullable Box, and demand-driven recursive drop helpers.
7. Enable aggregate positions, reject deferred consumers, and implement every
   Validation item with generated-C assertions.
8. Implement jointly with RFC 0153's scoped Slice checks; update snippets,
   review the manifest, and run ordinary/qualified suites. With explicit user
   approval, synchronize `docs/reference.md`; then rebuild/restart the workbench
   and close when all artifacts agree.

## Open questions

None.
