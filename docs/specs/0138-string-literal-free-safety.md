# RFC 0138: String Literal Free Safety

- Kind: Execution Plan
- Status: Implementation-ready; implementation not started
- Created: 2026-08-27
- Origin: RFC 0103 finding F41
- Restores: the existing `String.free(heap)` ownership contract without
  changing its Hexal signature
- Coordinates with: RFC 0123 (stateless Heap runtime), `docs/reference.md`

## Summary

Prevent `String.free(heap)` from passing static literal storage to the heap
deallocator.

String literals and allocated Strings share the same `const hex_string *`
handle. The runtime currently recovers a `hex_string_storage *` from every
handle and frees it unconditionally. Therefore this accepted program reaches
an invalid C free:

```hexal
h: Heap := Heap.new()
text: String := "hello"
text.free(h)
```

This is a current generated-runtime bug, not a new language feature.

## Decision

Add an ownership discriminator to the String header.

- Literal headers are marked static.
- Every constructor that allocates `hex_string_storage` marks the header owned.
- `hex_string_free` frees only owned storage.
- Freeing static storage traps with:
  `[Runtime Error] cannot free a String literal\n`.
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
2. Use a C23 enum or Bool with explicit constants; do not expose the field as a
   Hexal API or generate a public helper.
3. Update every generated literal header initializer to mark static storage.
4. Update `hex_string_from_bytes`, `hex_string_from_runes`, concatenation, and
   every other owning constructor to mark allocated storage.

### Phase 2: cleanup

5. Check the discriminator in `hex_string_free` before recovering the
   allocation base.
6. Trap on static storage with the exact message above.
7. Preserve the current allocator-free behavior and C base-address recovery for
   owned storage.

### Phase 3: discovery and validation

8. Update String component models/templates only where the new initializer
   field is required; keep selection demand-driven.
9. Add generator validation that every String literal carries a complete
   header initializer.
10. Sweep tests and comments that assume all `hex_string *` values name
    `hex_string_storage` allocations.

### Phase 4: tests and documentation

11. Add the exhaustive unit, integration, and tagged C23 cases below.
12. Update `docs/reference.md` after behavior stabilizes to state that freeing
    a literal String traps and that runtime String storage records ownership.
13. Regenerate the snippet manifest because every emitted String header
    initializer and the shared String representation legitimately change.

## Validation

This section is exhaustive.

- The direct literal-free program compiles and traps with the exact message.
- A literal passed through a function parameter still traps on free.
- A literal returned from a function still traps on free.
- A literal copied through an object member still traps on free.
- An allocated String from bytes frees successfully.
- An allocated String from runes frees successfully.
- A concatenated String frees successfully.
- Literal reads, iteration, comparison, printing, and slicing remain unchanged.
- The generated literal header is marked static and every owning constructor is
  marked owned.
- Tagged C23 validation executes both one trapping and one successful cleanup
  fixture under each supported toolchain.
- Manifest movement is limited to artifacts selecting the String component.
- `go test ./...`, `go vet ./...`, and the targeted tagged C23 suite pass.

## Non-goals

- General affine String ownership.
- Double-free detection after an owned allocation has been released.
- Changing `String.free(heap)` or removing its Heap parameter.
- Changing String's pointer-sized source representation.
