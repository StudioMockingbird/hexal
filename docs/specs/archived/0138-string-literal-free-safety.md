# ADR 0138: String Literal Free Safety

- Kind: Architecture Decision Record (ADR)
- Status: Closed; implemented
- Created: 2026-08-27
- Updated: 2026-09-08
- Origin: RFC 0103 finding F41
- Restores: the existing `String.free(heap)` ownership contract without
  changing its Hexal signature
- Coordinates with: RFC 0039 (future C interoperability), RFC 0123
  (stateless Heap runtime), RFC 0137 (place-sensitive checked provenance),
  `docs/reference.md`

## Summary

Prevent `String.free(heap)` from passing static literal storage to the heap
deallocator.

String literals and allocated Strings share the same `const hex_string *`
handle. The runtime currently recovers a `hex_string_storage *` from every
handle and frees it unconditionally. Therefore this accepted program reaches
an invalid C free:

```hexal
h: Heap := Heap()
text: String := "hello"
text.free(h)
```

This is a current generated-runtime bug, not a new language feature.

## Diagnostic timing

A free whose receiver is statically proven to denote static literal storage is
rejected by the checker with:
`cannot free a String literal`.

Static-literal origin propagates through locally transparent bindings, inline
object members, ADT and union payloads, and Array elements. Calls and mutable
handle storage do not gain new interprocedural or alias summaries in this ADR;
an origin obscured by those boundaries is enforced by the runtime
discriminator instead.

The checker tracks a set of possible storage origins for each locally visible
String place:

- a literal contributes `static`;
- a direct allocating constructor, concatenation, or interpolation contributes
  `owned`;
- a call result, parameter, pointer read, or mutable List/Dict read contributes
  `opaque`.

Assignment to a binding, known object member, or constant Array index replaces
that place's origin set. Assignment through a dynamic Array index joins into
every element the index may select. Control-flow merging unions corresponding
origin sets. `String.free` is rejected when the resulting set contains
`static`; an `opaque` origin without a locally visible `static` possibility is
accepted and checked at runtime.

## Decision

Add an ownership discriminator to the String header.

- Storage kind zero means static; literal headers obtain it from C's
  zero-initialization of omitted fields.
- Every constructor that allocates `hex_string_storage` marks the header owned.
- `hex_string_free` frees only owned storage.
- Freeing static storage traps with:
  `[Runtime Error] cannot free a String literal\n`.
- The checker rejects a `String.free` receiver whose locally tracked origin is
  static literal storage. Unknown origin remains accepted and is checked at
  runtime.
- Copies and aliases share the header and therefore observe the same storage
  class.
- Do not make literal free a silent no-op: explicit cleanup of a non-owned
  handle is a programmer error, and hiding it would make ownership mistakes
  nondeterministic across String origins.
- Do not rely only on compile-time literal detection: literals can cross
  parameters, results, object members, unions, and collections.

The String handle remains pointer-sized. The allocation header grows by one
small discriminator; this is the accepted cost of making the shared handle
representation safe.

## Implementation plan

### Phase 1: representation

1. Add a private two-state storage-kind field to `hex_string` in
   `generator/packages/string.h`.
2. Use a C23 enum with explicit `static = 0` and `owned = 1` constants; do not
   expose the field as a Hexal API or generate a public helper. Zero must mean
   static so an omitted owning initializer fails safely by trapping instead of
   permitting an invalid free.
3. Keep generated literal initializers unchanged: their omitted discriminator
   is zero-initialized to static by C. Assert this representation choice in a
   generator-text test.
4. Mark all four direct allocation sites owned: `hex_string_from_bytes`,
   `hex_string_from_runes`, concatenation in `generator/packages/string.c`, and
   the interpolation allocation emitted by `generator/interpolation.go`.
   `String.to_string` and `Strand.to_string` delegate to
   `hex_string_from_bytes` and require no separate header initialization.

### Phase 2: cleanup

5. Check the discriminator in `hex_string_free` before recovering the
   allocation base.
6. Trap on static storage with the exact message above.
7. Preserve the current allocator-free behavior and C base-address recovery for
   owned storage.

### Phase 3: discovery and validation

8. Update String component models/templates and interpolation generation only
   where the discriminator is required; keep selection demand-driven.
9. Add generator-text assertions covering every emitted `hex_string` compound
   initializer: static literals omit the zero-valued field, while all four
   direct allocation sites explicitly mark owned.
10. Track the `static`, `owned`, and `opaque` origin sets through locally
    transparent copies, place-sensitive assignment, and control-flow merging.
    Reject a `String.free` receiver whose set contains `static` with the exact
    diagnostic above. Do not add interprocedural or mutable-handle alias
    summaries here.
11. Sweep tests and comments that assume all `hex_string *` values name
    `hex_string_storage` allocations.

### Phase 4: tests and documentation

12. Add the exhaustive unit, integration, and tagged C23 cases below.
13. Update `docs/reference.md` after behavior stabilizes to state where a
    statically proven literal free is rejected, where an opaque literal origin
    traps at runtime, and that runtime String storage records ownership.
14. Regenerate the snippet manifest because the shared String representation
    and runtime change. Module artifacts change only when they contain an
    interpolation allocation whose generated header initializer gains the
    owned discriminator.

## Validation

This section is exhaustive.

- Direct literal free is rejected with
  `cannot free a String literal`.
- A literal copied through a local binding, inline object member, ADT or union
  payload, or Array element retains its static origin and is rejected on free
  with the same diagnostic.
- Replacing a binding, known object member, or constant Array element's static
  value with a directly owned String removes the static origin and permits
  free. Replacing an owned value with a literal rejects free.
- A branch merge containing both static and owned origins rejects free.
- A dynamic-index Array assignment of a literal makes every possibly selected
  element reject free until a statically known replacement removes that
  element's static possibility.
- A literal passed into a function that frees its String parameter, returned
  through a function and then freed, or recovered from List/Dict mutable handle
  storage has opaque origin under this ADR. It compiles and traps with the exact
  runtime message when the value is literal-backed.
- Interpreted and raw literal Strings use the same static discriminator and
  behave identically under both checker and runtime enforcement.
- An allocated String from bytes frees successfully.
- An allocated String from runes frees successfully.
- A concatenated String frees successfully.
- Strings produced by `String.to_string`, `Strand.to_string`, and
  `String.interpolate` are marked owned and free successfully.
- Literal reads, iteration, comparison, printing, and slicing remain unchanged.
- A generated literal header obtains static kind through zero-initialization;
  all four direct allocation sites explicitly mark their headers owned.
- Tagged C23 validation directly exercises both one static-storage trap and one
  successful owned cleanup under each supported toolchain, independently of
  whether the Hexal checker rejects a statically proven literal free.
- Manifest movement is limited to shared String-component artifacts and module
  artifacts containing generated interpolation allocations.
- `go test ./...`, `go vet ./...`, and the targeted tagged C23 suite pass.

## Non-goals

- General affine String ownership.
- Double-free detection after an owned allocation has been released.
- Changing `String.free(heap)` or removing its Heap parameter.
- Changing String's pointer-sized source representation.
- Exposing `hex_string` as a stable foreign ABI before RFC 0039 defines that
  boundary.
