# RFC 0223: Dict Correctness and Completeness Updates

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented and validated 2026-09-27
- Validation: `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass;
  the focused Dict C23 compile/run fixtures pass.
- Created: 2026-09-20
- Updated: 2026-09-27
- Origin: Dict implementation review
- Supersedes: RFC 0136 (Expanded Dict Key Types; never implemented)
- Coordinates with: archived RFC 0226 (Unified Lists with Inline Capacity),
  RFC 0248 (Float Semantics and Collection Eligibility), RFC 0249 (Lazy
  Traversal Pipelines), and
  `docs/reference.md`

## Summary

Keep Dict's open-addressing representation, backward-shift deletion,
versioned iteration, shallow element ownership, and demand-driven component
emission. Make five bounded improvements:

1. remove stale `.new()` constructor wording from compiler-owned diagnostics
   and implementation comments;
2. reject a definitely freed direct List or Dict binding when it is freed
   again or used again;
3. add capacity-retaining `Dict.clear()`;
4. admit Bool, every fixed-width integer, Size, Rune, and existing inline
   `String<N>` as Dict keys; and
5. generalize the existing compiler-owned scalar hash helper rather than add a
   native hashing dependency.

Float keys, EoS keys, heap String keys, aggregate keys, pointers, user-defined
hash/equality protocols, and xxHash are not part of this RFC. Float semantics
and any later Float-key policy belong to RFC 0248.

## Current evidence

The following probes reproduce against the compiler state reviewed on
2026-09-27:

| Program | Current result |
| --- | --- |
| `Dict<Int32, Int32>(13)` | reports the stale text `Dict<K, V>.new requires a Heap` |
| `scores.free(h); scores.free(h)` | accepted |
| `scores.free(h); scores.get(1)` | accepted |
| `Dict<Bool, Int32>` | rejected |
| `Dict<Float64, Int32>` | rejected |
| `Dict<String<8>, V>.get(key)` where `key` is `String<16>` | rejected because the method requires the exact key type |

Implementation verification: the constructor diagnostic now names `Dict<K, V>`;
same-binding repeated free and post-free use are rejected; Bool keys compile;
Float64 keys remain rejected; and cross-capacity String lookup remains rejected.
Tagged C23 execution passes the scalar/text/clear, growth/deletion, and
traversal-invalidation fixtures. The complete tagged fixture run also exposes
the separately tracked RFC 0156 pointer-arithmetic mismatch in
`docs/status.md`.

The stale user-facing messages now live in `compiler/diagnostics`, following
the central-diagnostic architecture. Stale `.new()` narration also remains in
checker and generator comments. The older inventory naming
`compiler/generator/arrays.go` is obsolete after RFC 0226 removed that file;
implementation starts from a fresh repository-wide spelling inventory.

`compiler/types.IsDictKey` currently admits only Int32 and inline `String<N>`.
`compiler/generator/packages/dict.h` has two hash paths: one Int32 mixer and the
shared logical-text helper `hex_hash_text`. The generalized implementation
extends those paths; it does not replace Dict's representation.

## Decisions

### 1. Current constructor spelling only

Compiler-owned constructors use the call-shaped spelling:

```hexal
let scores = Dict<Int32, Int32>(heap)
let values = List<Int32>(heap)
let channel = Channel<Int32>(heap, 16)
```

No diagnostic or implementation comment may describe these as `.new()` calls.
This sweep covers Dict, List, Channel, Mutex, Atomic, Heap, Stash, and Pool. It
does not rewrite a user-defined function whose source name legitimately
contains `new`.

### 2. Exact Dict key types

The accepted key families are:

| Family | Accepted key types |
| --- | --- |
| Boolean | `Bool` |
| Signed integer | `Int8`, `Int16`, `Int32`, `Int64` |
| Unsigned integer | `UInt8`, `UInt16`, `UInt32`, `UInt64` |
| Target-sized integer | `Size` |
| Language aliases | `Byte`, with the same canonical identity as `UInt8` |
| Unicode scalar | `Rune` |
| Inline text | `String<N>` for every valid capacity N |

The following remain invalid keys:

- `Float32` and `Float64`;
- `EoS` and standalone `Nil`;
- heap `String`;
- Slice, List, Dict, objects, ADTs, unions, pointers, function values,
  allocators, synchronization handles, foreign records, and incomplete types.

Dict key identity remains exact. Numeric widening does not make differently
declared Dict key types interchangeable. `Byte` and `UInt8` are one exception
only because Byte is already a transparent alias of UInt8.

An open generic key parameter is permitted while its body is checked; every
concrete specialization must resolve it to one of the accepted families, or
specialization fails with the key-position diagnostic.

Different inline-text capacities remain different key types:

```hexal
let names = Dict<String<8>, Int32>(heap)
let wide: String<16> = "alice"
names.get(wide) -- Type Error: convert explicitly to String<8>
```

A string literal is still contextual and is measured against the declared key
capacity. A non-literal key must already have exactly the Dict's key type.

### 3. Equality and hashing

Equality remains the final identity check after a hash match. Hash collisions
never merge unequal keys.

`String<N>` hashing consumes exactly `byte_length` logical bytes. Capacity,
unused storage, and bytes outside the initialized logical payload never
participate. Embedded zero bytes within the logical payload do participate.
Two equal values of the same exact `String<N>` key type therefore hash equally.

Every non-text key is converted to one canonical `uint64_t` input and passed
through one compiler-owned SplitMix64 finalizer:

- Bool becomes zero or one;
- unsigned integers, Size, and Rune convert value-preservingly to `uint64_t`;
- signed integers first convert to the corresponding same-width unsigned type,
  preserving their two's-complement bit pattern, and then widen to `uint64_t`.

The finalizer is the existing Int32 mixer generalized to
`hex_hash_scalar(uint64_t)`. It is emitted once in `hexal/dict.h` when any
scalar-key Dict is reachable. Text-key Dicts continue to use
`hex_hash_text`. No operation allocates or performs a fallible conversion to
hash a key.

This hash is deterministic and non-cryptographic. Resistance to adversarial
collision attacks remains separate future work; xxHash is not introduced.

### 4. Definitely freed direct bindings

The checker tracks a direct local List or Dict binding using the existing
branch-local allocation-identity machinery. This is a local proof, not an
ownership system.

```hexal
let scores = Dict<Int32, Int32>(heap)
scores.free(heap)
scores.get(1) -- Memory Error: the binding is definitely freed
```

The rules are:

1. A normal `free(heap)` marks that direct local binding's current identity as
   freed.
2. A second `free`, indexing, iteration, or any method call through that same
   definitely freed binding is rejected.
3. The fact survives a control-flow merge only when every continuing branch
   proves the current identity freed.
4. `defer value.free(heap)` does not mark the value freed at the point where
   the defer is registered; the release occurs at scope exit.
5. Reassigning a mutable binding to a new List or Dict gives it a fresh live
   identity.
6. Function parameters, object members, collection elements, returned handles,
   captured handles, escaped handles, and separately copied aliases remain in
   the documented manual-lifetime envelope. This RFC does not claim to prove
   them safe.

The diagnostic uses the central compiler diagnostic package and the existing
Memory Error category. Dict and List use the same diagnostic key and wording as
other definitely-freed values; no collection-specific paraphrase is added.

### 5. `Dict.clear()`

```text
Dict<K,V>.clear() -> no value
```

`clear()`:

- marks every bucket inactive;
- sets logical length to zero;
- retains the existing bucket pointer and capacity;
- increments the structural version exactly once, including when the Dict was
  already empty;
- never frees or destroys stored keys, values, or anything they refer to; and
- performs no allocation.

After `clear()`, a subsequent insertion reuses the retained bucket region. Any
active traversal is invalidated by the version change. Capacity retention is a
generated-C invariant, not a new Hexal `capacity()` API.

## Non-goals

- Float or EoS keys.
- Heap String, pointer, aggregate, collection, or structural keys.
- User-defined hash/equality protocols or operator overloading.
- Secure or process-randomized hashing.
- xxHash or another native hashing dependency.
- Changing open addressing, backward-shift deletion, growth, or iteration
  order.
- Extending the local freed-state guarantee to copied or escaped aliases.
- Adding a Dict capacity API.
- Key-only or value-only traversal, lazy traversal pipelines, `map`, `filter`,
  and `collect`; RFC 0249 owns that surface.

## Required sweep

1. Search compiler diagnostics, comments, tests, and generated-template
   narration for compiler-owned `.new()` constructor spellings. Retain only the
   rejection diagnostic explaining that `.new()` is removed and any genuine
   user-defined identifier.
2. Replace the two-family `TextKey`/Int32 hash selection with an explicit key
   family record. Remove helper names and tests that assume Int32 is the only
   scalar key.
3. Replace every `Int32 or String<N>` eligibility diagnostic, type comment,
   and test with the settled key set.
4. Update the existing tests that intentionally accept direct same-binding
   List/Dict double-free or use-after-free. Preserve copied-alias tests that
   document the manual envelope.
5. Remove no defensive check merely because the new checker fact exists; the
   generated runtime still receives programs compiled earlier and aliases the
   checker cannot prove.
6. Synchronize the Dict and value-lifetime sections of `docs/reference.md`
   only during implementation, after behavior is stable.

## Detailed implementation plan

### Phase 0: refresh the baseline

1. Confirm the RFC 0226 List migration is settled in the implementation
   baseline before editing the overlapping List checker and tests.
2. Re-run the six evidence probes in this RFC and record changed outcomes.
3. Inventory stale compiler-owned `.new()` text from the current tree rather
   than following historical file coordinates.
4. Record the existing snippet-manifest hashes for every Dict-bearing snippet.

### Phase 1: current constructor wording

1. Change the centralized Dict, List, Channel, Mutex, and Atomic diagnostic
   constructors to call-shaped names.
2. Sweep stale checker/generator CARE comments and test descriptions for the
   removed spelling.
3. Keep the one diagnostic that rejects source `.new(...)` and explains the
   current `Type(...)` form.
4. Run focused diagnostic registry and integration tests before any generated-C
   change.

### Phase 2: direct-binding cleanup facts

1. Add List and Dict local data bindings to the existing freed-state tracking
   seed without making parameters, members, captures, or escaped aliases
   trackable.
2. At a non-deferred `free`, reject an already-freed receiver, then mark the
   receiver identity freed after argument/type validation succeeds.
3. Add one common receiver check before List indexing/iteration/method dispatch
   and Dict iteration/method dispatch.
4. Reuse existing branch intersection, deferred-capture depth, and rebinding
   identity behavior; do not add a second collection-specific flow table.
5. Preserve the existing List-source release fact used by borrowed memory
   streams.

### Phase 3: Dict clear

1. Add `clear` to the authoritative Dict method record with zero arguments and
   no result.
2. Check it through ordinary Dict receiver validation, including the freed
   receiver guard.
3. Lower it as a collection method call and add `hex_dict_clear_<suffix>` to
   the Dict template.
4. In generated C, loop over the existing capacity and set each bucket's
   `active` field to false, set length to zero, and increment version. Do not
   assign buckets or capacity and do not call an allocator or element cleanup.

### Phase 4: key eligibility and scalar hashing

1. Expand the one authoritative `types.IsDictKey` predicate to the settled
   families. Resolution, canonical validation, generic specialization, and
   method checking continue to call that predicate rather than duplicate a
   list.
2. Replace the binary `TextKey` model with a scalar/text key-family enum or
   equivalent direct fact.
3. Generalize `hex_hash_Int32` to the once-emitted
   `hex_hash_scalar(uint64_t)` finalizer.
4. Render the canonical conversion at the call site from the exact key type;
   evaluate the source key expression once before probing, as today.
5. Keep text hashing and equality on `hex_text_inline(&key)`, which uses only
   the logical payload.
6. Ensure Dict specialization names continue to contain the exact canonical key
   type, so different integer widths and signedness never collide.

### Phase 5: documentation and conformance

1. Update the normative Dict signatures, accepted key table, exact-key rule,
   `clear`, hashing contract, and local freed-state boundary in
   `docs/reference.md`.
2. Remove RFC 0223's open-bug/TODO rows from `docs/status.md` only after all
   validation passes. RFC 0249 independently owns lazy key/value traversal.
3. Regenerate the snippet manifest only for artifacts legitimately changed by
   the Dict helper and diagnostic work, then review every moved artifact family.
4. Rebuild `hexal` and restart the workbench through `hexal play` for handoff.

## Validation

Validation is exhaustive for this implementation scope. Lazy traversal is not
part of this RFC.

### Constructor spelling

1. A non-Heap Dict constructor argument reports the current call-shaped Dict
   spelling and its stable diagnostic key.
2. Equivalent List, Channel, Mutex, and Atomic cases report their current
   call-shaped spellings.
3. No compiler diagnostic names a removed compiler-owned `.new()` constructor.
4. Source `.new(...)` is still rejected by the dedicated migration diagnostic.

### Definitely freed bindings

5. For both allocated List and Dict direct local bindings, reject a second
   `free` and every operation after free: indexing where supported, iteration,
   and every registered method, including Dict `get`, `find`, `contains`,
   `insert`, `remove`, `length`, `clear`, and `free`.
6. Accept use following a conditional free when at least one continuing branch
   leaves the binding live; reject when every continuing branch frees it.
7. Accept use after registering `defer value.free(heap)` and before scope exit.
8. Accept use after a mutable binding is reassigned to a fresh List or Dict.
9. Preserve acceptance of a separately copied alias after the source is freed,
   while documenting that it may dangle. Do not claim copied-alias safety.
10. Preserve the existing borrowed-memory-stream rejection after a directly
    proved source List free.

### Clear

11. Clearing a populated Dict makes `length()` zero, iteration yields no
    entries, and reinsertion/lookup succeeds.
12. Clearing an already empty Dict succeeds and increments the structural
    version once.
13. Clearing during an active traversal invalidates that traversal.
14. Generated C proves that clear retains the bucket pointer and capacity,
    calls no allocator or element cleanup, marks every bucket inactive, sets
    length to zero, and increments version.
15. A Dict containing String handles proves clear does not emit
    `hex_string_free` or otherwise free referents.

### Key families and hashing

16. End-to-end insert/find/get/contains/remove cases cover Bool; every signed
    and unsigned width; Size; Byte/UInt8 alias identity; Rune; and inline text.
17. Distinct scalar key types remain distinct Dict types and generated helper
    specializations. No implicit cross-type numeric lookup is accepted.
18. Generic code specializes correctly for each admitted key family, and an
    instantiation that substitutes a non-key type for an open key parameter is
    rejected with the key-position diagnostic.
19. Float32, Float64, EoS, heap String, pointers, objects, ADTs, unions,
    collections, allocators, function values, foreign records, and incomplete
    types remain rejected as keys with the key-position diagnostic class.
20. Equal negative and positive integer values expressed through the same key
    type find the same entry; signed conversion preserves the exact same-width
    unsigned bit pattern before hashing.
21. String<N> hashes exactly its used logical bytes. Tests cover unused tail
    bytes, embedded zero bytes, empty text, and two equal values of the same
    capacity stored in independently initialized regions.
22. A differently sized non-literal String key is rejected until explicitly
    converted to the Dict's exact key type; a fitting literal remains
    contextual.
23. Generated C emits `hex_hash_scalar` at most once, emits no per-width wrapper
    that only delegates, preserves `hex_hash_text` for text, and adds no native
    runtime dependency.

### Gates

24. Focused checker, type, generator, diagnostics, component, and integration
    tests pass.
25. `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.
26. The tagged C23 suite compiles and runs representative scalar, text, clear,
    growth, deletion, and traversal-invalidation fixtures.
27. The generated-C manifest changes only for artifacts whose reachable Dict
    helpers or corrected diagnostics legitimately changed.
