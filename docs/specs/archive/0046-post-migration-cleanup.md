# RFC 0046: Reference Reconciliation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-12
- Features: canonical synthetic filename, one storability rule, shallow
  collection element storage, View return and `from_pointer` checking, Atomic
  non-copyability enforcement, protected `Stream` name, and range-based
  `UInt64`/`Size` widening
- Created: 2026-08-12
- Revised: 2026-08-12
- Depends on: RFC 0008 (function-value positions), RFC 0014 (union members),
  RFC 0016 (lossless widening), RFC 0019 (generic type names), RFC 0020
  (collections), RFC 0029 (Error source location), RFC 0031 (`Stream<T>`),
  RFC 0035 (C-style copying and manual lifetimes), RFC 0036 (`Size`), RFC 0037
  (`Atomic<T>`), and RFC 0043 (View bridge)
- Coordinates with: `docs/reference.md`, the normative language reference

## Summary

Distilling RFCs 0001-0045 into `docs/reference.md` surfaced two classes of
mismatch. This RFC settles both.

**Part A — language decisions.** RFC 0035 replaced affine ownership with
C-style copying and manual lifetimes, but several RFC 0020 rules that existed
only to keep that ownership model decidable were never withdrawn. They are
withdrawn here. Part A also fixes the source extension and defines the one
lifetime rule Hexal still enforces for Views.

**Part B — conformance cleanup.** Three compiler behaviors disagree with
already-settled decisions in RFCs 0031, 0036, and 0037.

Part A changes what the language means and supersedes clauses in accepted RFCs;
its decisions are proposed here and pending approval. Part B changes nothing
semantically. Both are already written into `docs/reference.md`, which is
normative; this RFC is the decision record and implementation contract behind
it. Part B may be implemented independently of Part A.

---

# Part A — language decisions

## 1. Canonical synthetic filename

The compiler takes source text and returns generated text; it does not read or
write files. This item therefore changes only the **synthetic filename** the
compiler substitutes when no source name is supplied, used consistently in
three places:

- structured diagnostics;
- generated `#line` directives; and
- `Error.file` under RFC 0029.

That name is `main.hex`.

```c
#line 2 "main.hex"
```

RFC 0029 fixed it as `main.seawitch`, and the RFC 0045 rename deliberately
excluded it because that rename was scoped to the `sw_` prefix. This is a
string change with no semantic content and no observable file behavior.

`.hex` is the project's chosen source extension, and `main.hex` is named to
match it. Nothing here accepts a `.hex` file, rejects a `.hexal` one, discovers
an entry file, or preserves a caller-supplied name — no such behavior exists to
change. When file loading and a CLI are specified, they own that contract and
should adopt `.hex`; until then the extension is a naming convention this item
merely anticipates.

Markdown code fences in live documentation remain ```` ```hexal ````; the fence
tag is a highlighting hint, not a filename.

## 2. One storability rule

### Decision

**Any complete, finitely sized value may be stored in any position that stores
a value**, subject to the exceptions below. The positions are enumerated under
"Position model".

This RFC uses `docs/reference.md`'s three independent properties rather than a
two-way type split. An earlier draft classified aggregates as storable only when
built entirely from non-handle members, which wrongly excluded `Row =
{ name: String }` and, fatally, `Error` — making `T | Error` unstorable. The
properties are:

- **representation** — every value is stored inline where it is placed and every
  copy is shallow; a handle is a pointer, so copying it copies the pointer;
- **external state** — some values refer to state outliving an individual copy
  (`String`, `List`, `Dict`, `Stream`, `Task`, `Channel`, `Mutex`, `File`,
  `RuneCursor`, `View`, and any aggregate transitively containing one), with
  cleanup defined per type rather than universally as `free`;
- **copyability** — every value is copyable except `Atomic<T>` and any value
  transitively containing one.

An aggregate is storable when its members are. Storability and copyability are
separate questions; see the Atomic exception.

```hexal
type Row = { name: String, tags: List<Strand>, }

rows:   List<Row>          = List<Row>.new(h)
names:  Array<String, 4>   = ["a", "b", "c", "d"]
nested: List<List<Int32>>  = List<List<Int32>>.new(h)
lookup: Dict<Strand, List<Int32>> = Dict<Strand, List<Int32>>.new(h)
maybe:  String | Nil       = nil
```

### What this withdraws

RFC 0020 rejected managed handles from `Array` elements, nested collections
from `List` and `Dict`, `String`/`List`/`Dict` from union members, and `List`,
`Dict`, and `View` from object and ADT members. Every one of those bans existed
to keep affine owner state, borrow roots, and exactly-once cleanup decidable.
RFC 0035 deleted that machinery, so the bans protect nothing and are removed.

RFC 0043 restated the View half of those restrictions as still standing. This
RFC supersedes that clause together with the RFC 0020 rules it cites, for
storage here and for returns in item 4.

Storing a managed handle stores the handle. The container frees its own region;
the referenced allocations remain the programmer's, exactly as in item 3.

### Exceptions, each for its own reason

These are not ownership rules and they survive:

| Excluded | From | Reason |
|---|---|---|
| `Atomic<T>` | every storing position | non-copyable; see item 5 |
| `Fun<...>` | object/ADT members, collection elements | RFC 0008 defers the C declarator, addressability, and FFI rules these need |
| `Unknown` | every position by value | incomplete; valid only as a pointee |

`View<T>` has no exception. It is a read-only `{pointer, length}` descriptor
that owns nothing, so it is storable everywhere, including as a union member
and as another View's element. A union payload or a `View<View<T>>` region is
no less visible to the checker than a `List` slot, and once slots are permitted
there is no principled line that keeps the other two out. Keeping borrowed
storage alive stays a manual-lifetime obligation under RFC 0035.

`Fun<...>` is storable only as a `Binding`, `UnionMember`, or `FunctionParam`,
so `Fun<(Int32) : Int32> | Nil` keeps working. Every other position — object and
ADT members, all collection elements including `ViewElement`, `StreamElement`,
and `StreamState`, `FunctionResult`, `Ptr<Fun<...>>`, and `ref` — stays as RFC
0008 left it. Widening those is deferred, not decided here.

`Dict` keys remain exactly `Int32` or `Strand`. This RFC does not touch key
eligibility.

### Position model

Restrictions are stated against an explicit position set, so no aggregate or
generic specialization can bypass one by accident:

```text
Binding          ObjectMember     ADTPayload       UnionMember
ArrayElement     ViewElement      ListElement      DictValue
FunctionParam    FunctionResult   TaskArgument     TaskResult
ChannelElement   StreamElement    StreamState
```

A **construction position** is `Binding` or `ObjectMember`: storage that direct
construction can initialize in place. Every other position acquires its value by
copying.

### Implementation

Two predicates, not one. Conflating them is what made the earlier draft reject
the `Atomic` object member it also permitted.

```text
storable(T, position) =
    T is complete and finitely sized
    and T is not Unknown
    and T is not an unspecialized type parameter
    and (T is not a Fun
         or position in { Binding, UnionMember, FunctionParam })

copyable(T) = not containsAtomic(T)
```

`storable` governs whether a type may appear in a position at all. `copyable`
is checked separately, at every operation that copies a value: assignment,
argument passing, return, collection insertion, union injection, and `spawn`
argument capture.

`Atomic<T>` is storable only in a construction position — `Binding` or
`ObjectMember` — and rejected everywhere else, because every other position
acquires its value by copying. This is exactly the placement RFC 0037 blesses:
"They may be object members shared by pointer."

`ADTPayload` and `UnionMember` are deliberately excluded. Neither has a
demonstrated use, an ADT payload would need its own in-place variant
construction rule, and union injection copies by definition. Widening later is
additive and needs no rule here to change.

`containsAtomic` recurses through object members, ADT payloads, Array elements,
and union members, and **stops at every indirection**: `Ptr<T>`, `MutPtr<T>`,
and every handle. Copying a `Ptr<Shared>` or a `List<T>` handle copies the
pointer, not the pointee, so neither propagates non-copyability. Collection
constructors reject an `Atomic` element type directly rather than through
`containsAtomic`.

Existing `CollectionElement` and inline-element checks collapse into `storable`.
Generic specializations are checked after substitution, as today.

## 3. Collections store handles, never copies

### Decision

Every collection element, `String` included, uses shallow copy and shallow
discard. There is no deep-copy or nested-cleanup case for any element type.

- `push`, `set`, and `insert` store a copy of the value's C representation. For
  a managed handle that is the handle, not the allocation.
- `at`, indexing, `get`, `pop`, and `remove` hand back that same representation.
  A returned handle is an ordinary alias carrying no cleanup obligation.
- `set`, `clear`, `remove`, replacement, and `free` drop slots without freeing
  anything they reference.
- `free` releases only storage the container itself owns, its header and
  backing region, however many physical allocations that is. It never releases
  anything an element refers to.

```hexal
names: List<String> = List<String>.new(h)
defer names.free(h)          -- registered first, runs last

text: String = "hi".to_string(h)
names.push(text)             -- stores the handle; text is still yours

for name in names do
    name.free(h)             -- elements first
end
```

The element loop above is correct only because each element is a distinct
allocation this code owns. Insertion stores an alias, so the rule is per
allocation, not per slot: free each distinct runtime allocation exactly once,
through any live alias, after which every other alias dangles. Pushing one
handle into two slots and then looping double-frees it.

Order matters only when the container holds the last reference to an element.
Freeing it first then releases the region holding those handles and anything not
reachable elsewhere leaks — a leak rather than a use-after-free, so nothing
diagnoses it. A container whose elements are all reachable elsewhere may be
freed in any order.

### What this withdraws

RFC 0020 made a direct `String` element the one compiler-owned nested
allocation: `push`/`set`/`insert` allocated a collection-owned copy, and
`set`/`clear`/`free`/replacement destroyed it. That is a synthesized per-element
copy operation and a synthesized per-element destructor — precisely what RFC
0035 rejected when it said that automatically walking nested values "would add
destructor semantics that C does not have."

It was also arbitrary at one level of nesting: `List<String>` cleaned up, while
`List<Row>` with `Row = { name: String }` did not. Under this RFC neither does.

### Implementation

- Delete the generated per-element String copy and destroy helpers, and the
  calls to them from `push`, `set`, `insert`, `clear`, `remove`, and `free`.
- `free` and `clear` no longer iterate elements for any `T`. Element
  destruction disappears from the generated collection surface entirely.
- `pop` and `remove` become plain slot reads followed by a length or entry
  update; there is no ownership transfer to model.
- Dict value replacement writes the new representation over the old without a
  prepare-then-destroy sequence. Prepare-before-commit remains required for
  fallible steps such as growth and rehash, which is unchanged.

This is a net deletion. It removes the only place the compiler synthesized a
destructor.

## 4. Returning a View

### Decision

A `View<T>` may be returned when its root outlives the call. A return whose
view roots in a local binding of the returning function is rejected.

```hexal
fun payload(packet: Ptr<Packet>): View<Byte>
    return packet.bytes.slice(0, 4)   -- root reached through a parameter
end

fun adopt(pointer: Ptr<Int32>, count: Size): View<Int32>
    return View<Int32>.from_pointer(pointer, count)   -- foreign root
end

fun head(): View<Int32>
    fixed: Array<Int32, 4> = [1, 2, 3, 4]
    return fixed.slice(0, 2)          -- Error: root is a local
end
```

RFC 0020 and RFC 0043 reject every View return. That was correct while the
checker tracked borrow lifetimes; after RFC 0035 it blocks safe and useful
zero-copy signatures while the genuinely dangerous case — returning a pointer
into a dead frame — is the one thing still cheaply provable.

### Root resolution

Root resolution must be built. The checker currently reasons only about whether
an `Array` or `List` receiver is an addressable place or a temporary; it has no
general root-tracking pass to reuse, so implementing this item means adding one
that follows slices and copies back to an originating place. At a `return`
whose value is a `View<T>`, classify the root:

| Root | Result |
|---|---|
| a parameter, or storage reached through a parameter | permitted |
| a `from_pointer` region | permitted; see the trust-boundary note |
| `View<T>.empty()` | permitted; no root |
| a local binding of the returning function | rejected |
| a temporary `Array` or `List` | rejected, as today |

Slicing a View preserves its original root, as today.

A `from_pointer` root is permitted for returns because the stack case is
rejected at the `from_pointer` call itself, by the rule below.

### `from_pointer` rejects stack-derived pointers

`View<T>.from_pointer(pointer, length)` rejects a pointer the checker can trace
to `ref` within the current function:

```hexal
mut value: Int32 = 1

direct: View<Int32> = View<Int32>.from_pointer(ref value, 1)  -- Error
p: Ptr<Int32> = ref value
traced: View<Int32> = View<Int32>.from_pointer(p, 1)          -- Error
```

A `ref` names inline storage — a local, parameter, object member, or Array
element — whose lifetime is its enclosing scope. Building a bounds-checked
window over it and handing that window around is the mistake this rule exists
to catch, and it is what made a `from_pointer`-rooted return dangle.

Heap-allocated and opaque pointers are permitted:

```hexal
p: MutPtr<Int32> = h.allocate<Int32>(0)
ok: View<Int32> = View<Int32>.from_pointer(p, 1)              -- heap

fun wrap(p: Ptr<Int32>, n: Size): View<Int32>
    return View<Int32>.from_pointer(p, n)                     -- opaque
end
```

The check is a local def-use walk, not provenance analysis: it follows a
pointer back through assignments and initializations inside one function body
and rejects it when that chain reaches a `ref`. It deliberately does not
attempt an interprocedural or heap-only proof.

- Requiring a provably heap pointer is not decidable. A parameter, member, or
  call result has no visible origin, and `from_pointer` exists chiefly to adapt
  foreign C regions under RFC 0043, which are neither Hexal-heap nor stack.
- The remaining hole is caller-side: `wrap(ref local, 1)` still produces a
  dangling View, because proving that needs the interprocedural tracking RFC
  0035 removed. RFC 0043's trust-boundary contract continues to own it.

### Deliberate limitation

Only a directly returned `View` expression is checked. A View nested inside a
returned object, ADT, union, or collection is not:

```hexal
type Window = { visible: View<Int32>, }

fun bad(): Window
    fixed: Array<Int32, 4> = [1, 2, 3, 4]
    return Window { visible = fixed.slice(0, 2) }   -- not diagnosed
end
```

Item 2 makes that reachable. Chasing views through aggregates needs the escape
analysis RFC 0035 removed, so it stays programmer responsibility under the
ordinary manual-lifetime rule. This limitation is stated so it reads as a
scope boundary rather than an oversight.

This is the only View lifetime rule the compiler enforces. Resize
invalidation and `from_pointer` region lifetime remain unchecked.

---

# Part B — conformance cleanup

## 5. Enforce Atomic non-copyability everywhere

`Atomic<T>` is inline storage with identity. Copying its C representation would
create a distinct atomic value while the source appears to alias the original.
`Atomic<T>` and every value recursively containing one is therefore
non-copyable.

### Permitted

- Direct initialization of fresh storage with `Atomic<T>.new(value)`.
- Direct initialization of an Atomic object member with `Atomic<T>.new(value)`.
- The Atomic instance operations defined by RFC 0037.
- Sharing an object containing an Atomic through a pointer to that object.

```hexal
counter: Atomic<Int32> = Atomic<Int32>.new(0)

type Shared = { count: Atomic<Int32>, }
shared: Shared = Shared { count = Atomic<Int32>.new(0) }
pointer: Ptr<Shared> = ref shared
```

Direct construction initializes the destination; it is not a copy. An object
containing an Atomic is itself non-copyable after construction.

### Rejected

- Initialization from an existing Atomic value.
- Assignment or reassignment of an Atomic value.
- Passing or returning an Atomic by value.
- `ref` of an Atomic value or Atomic member.
- Object, ADT, or union construction from an existing value containing an
  Atomic.
- Storage in any position item 2 lists, plus Task argument and result storage,
  Channel elements, and Stream producer State, including recursive containment.

```hexal
counter: Atomic<Int32> = Atomic<Int32>.new(0)

copy: Atomic<Int32> = counter                    -- Error
mut other: Atomic<Int32> = Atomic<Int32>.new(1)
other = counter                                  -- Error
pointer: MutPtr<Atomic<Int32>> = ref counter     -- Error
items: Array<Atomic<Int32>, 1> = [counter]       -- Error
```

The checker must use one recursive non-copyability predicate at every copy and
storage boundary. The existing Task and Channel checks remain but must call that
same predicate rather than their own.

## 6. Protect the `Stream` name

`Stream` is a compiler-owned generic type constructor like `List`, `Dict`,
`Task`, `Channel`, and `Atomic`. It cannot be redeclared or shadowed in either
side of the shared type/value namespace.

Reject `Stream` as a type, alias, ADT, object, function, parameter, local, `for`
binder, or generic type parameter name.

```hexal
type Stream = Int32                     -- Error
Stream: Int32 = 1                       -- Error
fun use<Stream>(value: Stream): Stream  -- Error
    return value
end
```

Add `Stream` to the central protected type-name set; do not special-case
individual declaration forms. `Stream<T>` syntax and semantics do not change.

## 7. Complete `UInt64` and `Size` widening

`Size` is exactly the selected target's `size_t`, so its range is
target-determined. RFC 0036's rule is range-based, which makes widening with
`Size` directional and makes both directions lossless when the ranges are equal.

Where `Size` is 64 bits, `Size` and `UInt64` have identical ranges while keeping
distinct canonical identities, so:

- `Size` widens implicitly to `UInt64`;
- `UInt64` widens implicitly to `Size`; and
- a mixed operation selects `Size` by RFC 0036's explicit equal-range tie-break.

```hexal
raw:        UInt64 = 42
count:      Size   = raw          -- valid where Size is 64-bit
round_trip: UInt64 = count        -- valid
total:      Size   = count + raw  -- valid; common type is Size
```

The current compiler implements only the `Size`-to-`UInt64` direction.

Bidirectional widening applies wherever ordinary lossless widening applies:
initialization, assignment, arguments, returns, fields, collection insertion,
and common-type selection. It does not make the types canonically equal, change
specialization identity, permit signed-to-`Size` widening, or remove
`to<Size>()`.

Where a target's `Size` is narrower than 64 bits, `UInt64` to `Size` is not
lossless and requires `to<Size>()`. The common-type algorithm continues to use
concrete target ranges, so no rule is target-special-cased.

---

## Implementation order

Items 2, 3, and 4 all touch collection and view checking, so implement them as
one pass rather than three. Part B items are independent.

1. **Item 1** — mechanical string change; land it first and independently.
2. **Item 5** — write the recursive `containsAtomic` predicate. Item 2 needs it.
3. **Item 2** — collapse the element predicates into one `storable` function.
4. **Item 3** — delete the String copy/destroy helpers and their call sites.
5. **Item 4** — build root tracking, then apply it at the return position and
   at `from_pointer`. This is the largest item; nothing to reuse exists.
6. **Item 6** — one-line protected-name addition.
7. **Item 7** — extend directional assignability for equal-range `Size`.

This is sequencing guidance, not an execution plan. Under AGENTS.md a change
this size takes a separate `0046-...-plan.md` and explicit sign-off before
coding begins.

## Diagnostics

Reuse existing wording where it already identifies the violation.

```text
[Type Error] Atomic values cannot be copied, assigned, addressed, or stored here
[Type Error] Stream is a protected type name and cannot be redeclared or shadowed
[Type Error] a View cannot be returned when it borrows a local of this function
[Type Error] Fun<…> cannot be stored here
```

Every diagnostic belongs to the earliest checker phase that can prove it.
Removed restrictions must remove their diagnostics, not leave them unreachable.

## Required tests

Integration tests go in the existing facet files — `collections`, `view`,
`concurrency`, `conversion`, `naming` — never a file named for this spec, and
never a spec number in a test name.

**Item 1**
- Diagnostics, `#line` output, and `Error.file` all report `main.hex`.

**Item 2**
- `Array<String, 4>`, `List<List<Int32>>`, `List<View<Int32>>`,
  `Dict<Strand, List<Int32>>`, and an object with `List` and `View` members all
  compile.
- `String | Nil` and `List<Int32> | Nil` compile.
- `View` as a union member, `View<View<T>>`, `List<View<Int32>>`,
  `View<String>`, and `List<String>.slice(...)` all compile; no View placement
  ban survives.
- A root-level `View` binding compiles; the stale "module data" rejection is
  gone.
- `File`, `EoS`, `Task`, `Channel`, `Mutex`, and `Stream` are all valid object
  and ADT members, which RFCs 0040, 0031, and 0037 already required.
- An object holding a handle is valid in every newly permitted position,
  including as a union member, so `T | Error` and `List<Row>` compile.
- `Fun<…>` is rejected as an object member and as an Array, View, List, Dict,
  Stream, or Stream-State element, and as a function result; `Fun<…> | Nil`
  and a `Fun<…>` parameter still compile.
- Generic specialization where `T` becomes `View`, `Fun`, `Atomic`, or
  `Unknown` is rechecked and rejected where the position forbids it.
- `Dict` keys other than `Int32`/`Strand` are still rejected.

**Item 3**
- Mutating a stored referent is visible through every alias: after
  `outer.push(inner)`, `inner.push(10)` is observable through `outer[0]`,
  because both name the same List.
- Rebinding the source is not: a later `inner = List<Int32>.new(h)` leaves the
  handle in `outer[0]` pointing at the original List.
- `free` on `List<String>` emits no element loop; generated C releases only
  container-owned storage.
- `pop` returns a handle usable after the List is freed, proving no transfer.
- A `List<Row>` where `Row` holds a `String` frees nothing nested.
- Pushing one `String` handle into two slots and freeing per slot is a
  double free; the per-allocation rule is what the docs state.
- A static `String` literal stored in a collection is never freed by the
  collection.

**Item 4**
- Parameter-rooted, `from_pointer`-rooted, and `empty()` returns compile.
- A local-rooted return is rejected.
- A temporary-rooted view is still rejected.
- A slice of a parameter-rooted view returns successfully.
- A View nested in a returned object compiles, documenting the limitation.
- `from_pointer(ref local, n)` is rejected, directly and through a local
  pointer binding that traces to `ref`.
- `from_pointer` over an `h.allocate` result compiles, as does one over a
  pointer parameter.
- `wrap(ref local, 1)` compiles, documenting the caller-side hole.

**Item 5**
- Direct construction succeeds as a binding and as an object member, including
  nested one object inside another; copy, assignment, parameter passing,
  return, union injection, and `ref` of the Atomic itself fail.
- `Atomic` as an ADT payload field or a union member is rejected.
- `ref` of an object containing an Atomic succeeds; `ref` of its Atomic member
  fails.
- Recursive containment fails in Array, List/Dict eligibility, Stream
  element/State, Task argument/result, and Channel element checks.
- `containsAtomic` stops at indirection: a `Ptr<Shared>` and a `List<T>` handle
  both copy normally even though an Atomic is reachable through them.
- An object containing an Atomic constructs directly and shares by pointer but
  cannot be copied.

**Item 6**
- Every declaration category rejects `Stream`; ordinary `Stream<T>` still works.

**Item 7**
- `UInt64` widens to `Size` in every assignability context.
- `Size` still widens to `UInt64`; mixed operations select `Size`; the types
  remain canonically distinct.
- The generated `size_t` assertion matches the selected target profile.

Only the 64-bit profile exists today, so these tests exercise equal-range
behavior alone. They cannot distinguish a range-based implementation from one
that hardcodes 64 bits. RFC 0036's 16- and 32-bit profiles — where `UInt64` to
`Size` must be rejected and `UInt32` to `Size` becomes explicit — remain a
separate conformance gap, not closed by this RFC.

`go test ./...` must pass with no external C toolchain. Generated-C coverage
stays behind the `c23` build tag.

## Non-goals

- Reintroducing ownership, borrow tracking, moves, or destructors in any form.
- Escape analysis for Views nested inside returned aggregates.
- Widening `Fun<…>` placement; that remains RFC 0008's deferred work.
- Movable or heap-boxed Atomic values, memory-order selection, or lock-free
  guarantees.
- Renaming or redesigning `Stream`.
- Making `Size` an alias of `UInt64`, or adding target introspection.
- Changing `Dict` key eligibility.
- A collection element cleanup helper, iterator, or convenience free-all
  operation. Programs write the loop.

## Acceptance criteria

This RFC is implemented when:

1. diagnostics, `#line` directives, and `Error.file` use `main.hex`;
2. `storable(T, position)` and `copyable(T)` are two separate predicates,
   `storable` governs every position in the position model, and `Fun` and
   `Unknown` are its only positional exceptions;
3. every RFC 0020 placement ban withdrawn by item 2 is gone from the checker
   along with its diagnostic, including the root-level View "module data"
   rejection, and `View` retains no placement exception;
4. collections copy and discard every element shallowly, no generated element
   copy or destroy helper remains, and `free` releases only storage the
   container itself owns;
5. a View may be returned from a root that outlives the call, a local-rooted
   return is rejected, `from_pointer` rejects a pointer traceable to `ref`
   within the function, and the aggregate-nesting and caller-side limitations
   are left unchecked by design;
6. every prohibited Atomic copy, address, or storage path fails before
   generation, `containsAtomic` stops at every indirection, and construction in
   a `Binding` or `ObjectMember` plus pointer-sharing remain valid;
7. every user binding of `Stream` is rejected through the central protected-name
   rule;
8. `UInt64` and `Size` widen in both directions at equal range, mixed operations
   select `Size`, and canonical identity remains distinct;
9. focused integration and stage tests cover every item above; and
10. `docs/reference.md` needs no change, because it already states these rules;
    `docs/grammar.ebnf` and `docs/status.md` are updated after implementation
    stabilizes, and each conformance gap listed in `reference.md` is struck as
    its item lands.

RFC 0036's 16- and 32-bit `Size` profiles are explicitly **not** in scope. They
remain an open conformance gap after this RFC is complete.

## Open questions

None. Part A's decisions are settled and Part B follows from RFCs 0031, 0036,
and 0037. Part B may be implemented independently.

Three scope boundaries are deliberate, not unresolved:

- A View reachable inside a **returned aggregate** is not diagnosed. Closing it
  needs the escape analysis RFC 0035 removed.
- `from_pointer` does not catch the **caller-side** case, `wrap(ref local, 1)`,
  because the callee sees an opaque parameter. RFC 0043's trust-boundary
  contract owns it.
- `Atomic` placement is limited to `Binding` and `ObjectMember`, matching RFC
  0037. Nested direct construction follows without a separate rule, because
  each level is an ordinary object member initialized in place:

  ```hexal
  type Inner = { count: Atomic<Int32>, }
  type Outer = { inner: Inner, }
  outer: Outer = Outer { inner = Inner { count = Atomic<Int32>.new(0) } }
  ```
