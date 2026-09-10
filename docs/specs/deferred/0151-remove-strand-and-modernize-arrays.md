# RFC 0151: Remove `Strand` and Modernize Fixed Arrays

- Kind: Language Semantics
- Status: Open Discussion; not scheduled. The selected direction keeps both
  `Strand` and `Array<T, N>` and improves Strand through RFC 0152. This RFC is
  retained as a considered alternative to revisit, not as an amendment to the
  selected language direction.
- Created: 2026-09-08
- Updated: 2026-09-08
- Origin: user decision to keep one text type and one dynamic owning sequence
- Would change: fixed arrays from `Array<T, N>` to `[N]T` if this alternative
  is selected in the future; it currently amends no active specification
- Coordinates with: RFC 0153 (`Slice<T>`/`Slice<mut T>`); named Slice syntax
  leaves `[N]T` available but does not itself select this RFC's array spelling
- Coordinates with: RFCs 0039, 0117, 0136, 0139, 0147, 0149, and
  `docs/reference.md`
- Does not change: String UTF-8 encoding, fixed-array value semantics, List
  shallow-copy semantics, slice ownership, or the in-memory compiler boundary
- Accepted costs: String-key iteration allocates one owned key per entry;
  Error construction allocates owned header and message Strings; Error copies
  retain ordinary shallow-alias hazards

## Summary

Remove `Strand` and the generic spelling `Array<T, N>`.

- `String` is the only text type.
- `List<T>` is the only growable heap-backed sequence.
- `[N]T` is the fixed-length inline sequence.
- `Slice<T>` and `Slice<mut T>` are borrowed ranges under RFC 0153.

Fixed storage remains because it provides stack storage, inline aggregate
layout, compile-time length, allocation-free byte values, and the shape needed
to model C arrays. A List cannot provide those contracts.

## Sequence model

| Type | Storage | Length | Copy | Growth |
| --- | --- | --- | --- | --- |
| `[N]T` | inline | compile-time | copies elements | no |
| `Slice<T>` | borrowed pointer/length | runtime | copies descriptor | no |
| `Slice<mut T>` | writable borrowed pointer/length | runtime | copies descriptor | no |
| `List<T>` | heap-backed handle | runtime | aliases List | yes |

## Grammar

```ebnf
fixed-array-type = "[" , positive-decimal-literal , "]"
                   , type-expression ;
```

- `N` is positive.
- `Array<T, N>` and `Strand` are invalid type names.
- Existing fixed-array literals remain:

```hexal
bytes: [4]Byte := [0, 1, 2, 3]
matrix: [2][2]Float32 := [[1.0, 0.0], [0.0, 1.0]]
```

- Empty borrowed ranges remain explicit `Slice<T>.empty()`/
  `Slice<mut T>.empty()` constructions under RFC 0153; `[]` remains only a
  contextual fixed-array literal.

## String

- String retains its pointer-sized, shallow-copyable UTF-8 handle.
- Literals remain static; allocating constructors produce owned storage.
- `String.free(heap)` retains current origin checks and runtime discrimination.
- `length()` counts Runes; equality and ordering compare UTF-8 bytes.
- `bytes()` and `slice()` return `Slice<Byte>`; indexing remains unavailable.
- A literal now infers String because no competing text type remains:

```hexal
label := "ready"
```

- Remove Strand's protected name, 31-byte limit, inline representation,
  methods, conversion, comparison, hashing, and printing paths.
- `String.to_string(heap)` remains an owned-copy operation.

## Fixed arrays

`[N]T` retains all current `Array<T, N>` semantics except spelling:

- inline storage, by-value copying, and no cleanup;
- `N` is part of the type;
- nested arrays and left-to-right element evaluation;
- exact literal arity;
- current eligibility, indexing, iteration, equality, printing, addressing,
  bounds, and provenance rules.

```text
[N]T.length() -> Size
[N]T[index: Integer] -> place<T>
[N]T.slice(start: Integer, end: Integer) -> Slice<T>
[N]T.slice_mut(start: Integer, end: Integer) -> Slice<mut T>
```

`slice_mut` requires a writable receiver.

### C interoperability

- Imported inline C arrays retain every extent and map to `[N]T` shape.
- Nested C arrays are never silently flattened.
- C parameters written `T parameter[N]` follow C parameter adjustment and map
  to the appropriate pointer/slice contract, not a passed array value.
- Imported aggregate definitions remain owned by their C headers; Hexal does
  not emit a competing C struct to model an array field.
- RFC 0039 owns exact imported/exported ABI validation. This RFC does not claim
  that the existing generated fixed-array wrapper typedef is itself a C array.

## List construction

```text
List<T>(heap) -> List<T>
List<T>(heap, value, ...) -> List<T>
```

The value form evaluates Heap, evaluates values left to right into temporaries,
allocates once with sufficient capacity, then initializes elements left to
right. Allocation failure traps after argument evaluation. No partial List is
observable. Bracket literals never allocate Lists. Existing List behavior and
slice invalidation remain unchanged.

## Byte conversions

Output stays allocation-free and exactly sized:

```text
Int8.to_le_bytes() -> [1]Byte
Int8.to_be_bytes() -> [1]Byte
...
UInt64.to_le_bytes() -> [8]Byte
UInt64.to_be_bytes() -> [8]Byte
```

Input accepts a borrowed slice:

```text
Int8.from_le_bytes(bytes: Slice<Byte>) -> Int8
Int8.from_be_bytes(bytes: Slice<Byte>) -> Int8
...
UInt64.from_le_bytes(bytes: Slice<Byte>) -> UInt64
UInt64.from_be_bytes(bytes: Slice<Byte>) -> UInt64
```

Known wrong lengths are Type Errors. Unknown wrong runtime lengths trap with
`[Runtime Error] integer byte input has the wrong length\n`. Input is not
retained.

## Dictionary String keys

Dict keys become exactly `Int32 | String`; RFC 0136 may later expand this
baseline.

### Stored keys

1. New-key insertion clones logical UTF-8 bytes into Dict-owned String storage.
2. Every caller origin follows the same rule; insertion consumes nothing.
3. Equal-key insertion changes only the value and retains the existing key.
4. Removal frees the owned key once before returning the value.
5. Dict cleanup frees all active owned keys before its own storage.
6. Rehash moves handles without cloning or freeing them.
7. Hash/equality use byte length and logical bytes, never pointer identity.
8. Lookup operations borrow their argument only for the call.

The general container rule remains: cleanup never frees caller-provided
elements, values, or nested handles. `Dict<String, V>` has one narrow exception
for private keys the Dict created.

Deletion must preserve linear-probing clusters. The implementation must repair
the existing defect where clearing one bucket makes a later colliding key
unreachable. Use backward-shift deletion: walk the remaining cluster, move each
entry whose probe path crosses the hole, and clear only the final hole. Moving
entries never clones or frees their keys. This applies to Int32 and String keys.

### Iterated keys

Each iteration clones the selected key before entering the body:

```hexal
for key, value in words do
    defer key.free(heap)
    print(key, value)
end
```

- the key binder owns the clone and never aliases Dict storage;
- cloning may allocate and trap before the body;
- the programmer frees it on every exit path; no implicit destructor is added;
- the value binder retains ordinary shallow-copy behavior.

This is temporary. RFC 0149 or a later ownership RFC must revisit iteration
once a borrow can expose a key without permitting free or escape.

## Error ownership

```text
Error(header: String, message: String) -> Error
Error.free() -> no value
```

- `file`, `header`, and `message` are String fields.
- `file` refers to the compiler-generated static module-key String.
- Construction clones header and message into privately owned storage,
  regardless of argument origin.
- Construction uses the default allocator without a Heap parameter. Requiring
  Heap would infect every fallible API.
- Allocation failure traps rather than recursively producing Error.
- Runtime-generated Errors use the same representation. No inline diagnostic
  buffer or Strand-like representation remains.
- Dynamic IO operation/code text is formatted into owned String storage.

Error remains an ordinary shallow-copied value. Return, assignment, arguments,
union injection, `try`, and `errdefer` copy the Error struct and String handles;
they do not clone again. Heap storage keeps a returned Error valid.

`Error.free()` frees header and message once, not static file. Copies alias the
same allocations: freeing one invalidates the others under the existing
external-state policy. There is no implicit destruction, reference counting,
copy-on-write, or copy-time allocation.

Error equality and printing use String content and select required String
helpers exactly once.

## Required active-spec reconciliation

- RFC 0039: use String, `[N]T`, `Slice<T>`, and `Slice<mut T>`; own exact
  C-array ABI.
- Deferred RFC 0117: replace `Array<T, N>` with `[N]T`.
- RFC 0136: use `Int32 | String` baseline; remove its String exclusion.
- RFC 0139: close as superseded without implementation.
- RFC 0147: remove Strand work; retain String UTF-8 work.
- RFC 0149: replace Array spelling.
- RFC 0153: retain its named Slice grammar unchanged; `[N]T` remains this
  alternative's independent fixed-array spelling.
- `docs/status.md`: remove or rewrite current Strand/Array/View wording.

Archived RFCs 0137 and 0138 remain immutable. Their implemented behavior is
migrated through code and `docs/reference.md`, not archive edits.

## Required sweep

Search code, templates, tests, snippets, status, and active specs for `Strand`,
`StrandType`, `hex_strand`, `NeedStrand`, `Array<`, `array-type`, `View<`,
`Slice<`, `hex_view_`, and `Error(header: Strand`. Classify every result as removed,
migrated, retained internal fixed-array machinery, or immutable history. Leave
no compatibility aliases or unreachable cases.

## Detailed implementation plan

### Phase 1: fixed-array bracket type

1. Parse `[N]T` as the fixed-array form without changing RFC 0153's named
   `Slice<T>`/`Slice<mut T>` grammar.
2. Remove generic Array source parsing and protected lookup.
3. Route `[N]T` into the existing fixed-array checked representation.
4. Update source diagnostics while retaining generated representation names.

### Phase 2: String-only frontend

1. Remove Strand registration, resolution, operations, and size checks.
2. Infer String for uncontextualized string literals.
3. Remove mixed text paths; retain String origin/storage checks.

### Phase 3: Lists, slices, and bytes

1. Add the sequenced List value constructor.
2. Migrate current View source signatures and C identities under RFC 0153.
3. Preserve slice provenance and invalidation.
4. Retain fixed-array byte outputs; use slice inputs and runtime checks.

### Phase 4: Dict

1. Repair Int32 probe-chain deletion first and add collision regressions.
2. Add String specialization discovery and dependencies.
3. Clone, retain, move, compare, and clean stored keys as specified.
4. Clone iteration keys before binding.

### Phase 5: Error

1. Replace Error.header Strand with String.
2. Add one constructor that clones header/message with the default allocator.
3. Route source and runtime Errors through it.
4. Replace IO inline formatting with owned String formatting.
5. Add `Error.free()` and local external-state checks.
6. Update equality, printing, discovery, and propagation.

### Phase 6: removal and conformance

1. Delete generated Strand definitions, flags, helpers, and demand.
2. Complete the required sweep and active-spec reconciliation.
3. Migrate unit, integration, C23, and workbench sources.
4. Update `docs/reference.md` once behavior stabilizes.
5. Regenerate and review only legitimate manifest changes.
6. Run all gates; rebuild and restart the workbench.

## Validation

This section is exhaustive.

- `Strand` and `Array<T, N>` are rejected in every type position.
- `[N]T` retains all fixed-array positions and behavior, including nesting,
  exact arity, bounds, copying, addressing, slicing, and no allocation.
- `[N]T`, `Slice<T>`, and `Slice<mut T>` parse unambiguously.
- `label := "ready"` infers String; long literals remain static.
- Existing String UTF-8, origin, comparison, interpolation, and free rules hold.
- List value arguments evaluate left to right; one allocation follows; bracket
  literals never allocate Lists.
- Byte outputs return exact fixed arrays without allocation; byte inputs accept
  exact slices and reject/trap wrong lengths as specified.
- Removing any entry in ordinary, colliding, and wraparound Dict clusters keeps
  every remaining key reachable and changes length/version once.
- String insertion clones; replacement retains one key; remove/free clean once;
  rehash neither clones nor double-frees.
- String-key iteration returns an owned clone; freeing Dict or clone does not
  invalidate the other.
- Error construction clones both arguments; every generated Error uses the
  same representation; propagation performs no further clone.
- `Error.free()` frees header/message once and not static file.
- Error equality/printing select String support and preserve coordinates.
- No generated artifact contains `hex_strand` or source-facing Strand text.
- Fixed-array programs may retain `hex_array_*` generated identities.
- Every sweep result and active spec is accounted for.
- Unrelated manifest families do not change.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.
- Tagged C23 fixtures execute fixed-array, String, List, slice, Dict ownership,
  Error ownership, and byte-conversion cases on supported toolchains.

## Reference synchronization

Implementation must update `docs/reference.md` after behavior stabilizes:

- replace Strand and `Array<T, N>` with String and `[N]T`;
- define borrowed ranges consistently with RFC 0153;
- distinguish fixed arrays, slices, and Lists by representation;
- permit String-literal inference;
- add List construction and byte-conversion signatures;
- define Dict String-key/iteration ownership and its cleanup exception;
- define Error String ownership, shallow copying, and `Error.free()`;
- remove obsolete Strand rules and source Array spelling.

Drafting this RFC does not update `docs/reference.md`.

## Non-goals

- Fixed-size Lists or reference-like fixed arrays.
- Allocating bracket literals.
- General deep-copy, implicit destruction, or borrow checking.
- Dict keys beyond Int32 and String.
- Completing the entire C ABI design.
- Renaming generated fixed-array C symbols solely due to source spelling.

## Readiness

This RFC remains an Open Discussion and is not scheduled. If selected later,
its implementation follows RFC 0153 and retains that RFC's Slice spelling;
the choice under discussion is String-only text plus `[N]T` fixed arrays, not
the borrow type's syntax.
