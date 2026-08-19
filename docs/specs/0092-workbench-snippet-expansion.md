# RFC 0092: Workbench Snippet Expansion

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented
- Created: 2026-08-19
- Scope: add 31 executable examples to the workbench snippet catalog
- Depends on: `docs/reference.md`, the implemented compiler pipeline, and the
  existing catalog manifest
- Coordinates with: `docs/status.md`, the workbench catalog loader, and the
  generated-artifact SHA-256 manifest
- Does not change: Hexal syntax, language semantics, compiler output rules, or
  the existing 98 snippets

## Summary

Add 31 new, short, meaningful programs to the workbench catalog. The current
catalog contains 98 snippets. This RFC raises it to 129 and concentrates the
new coverage on language areas that have grown since the original catalog:
module ownership, fallible control flow, collection mutation, UTF-8 text
construction, synchronization operations, and pointer places.

The expansion is additive. Existing snippets remain unchanged, and every new
snippet is compiled through the public in-memory compiler API and pinned in the
generated-artifact manifest.

## Catalog rules

1. Add exactly the 31 snippet IDs listed in this RFC. Each ID MUST be unique.
2. Every new snippet MUST have a name, description, feature tags, an `app.hex`
   entrypoint, and a complete in-memory source map.
3. Each new snippet SHOULD contain no more than 20 non-empty source lines.
   No new snippet may exceed 24 non-empty source lines.
4. Each new snippet MUST demonstrate an operation or semantic interaction. A
   binding-only example is not sufficient.
5. Each new snippet MUST use only implemented syntax and APIs already accepted
   by `docs/reference.md`.
6. New snippets MUST be valid successful programs. Negative compiler examples
   and runtime-trap demonstrations are outside this catalog.
7. New snippets MUST NOT use `Dict.find`, File I/O, C interop, Arena, Pool, or
   any other excluded or open feature.
8. The catalog MUST remain deterministic and must not depend on host files,
   external processes, wall-clock time, locale, or scheduling order.
9. Feature tags MUST describe features present in the source. Reserved-word
   claims MUST match the source text.
10. Existing catalog source and manifest entries MUST remain unchanged except
    for intentional global artifact changes proven by the manifest diff.

## New snippets

### Modules

| ID | Name | Description |
|---|---|---|
| `modules-private-facade` | Private helper with exported facade | Keep a helper private while exposing one exported function that uses it. |
| `modules-list-imported-type` | List of an imported type | Instantiate and use `List<ImportedType>` across a module boundary. |
| `modules-dict-imported-types` | Dictionary of imported types | Use an imported type as a dictionary key or value with qualified names. |
| `modules-cross-module-nominal-types` | Cross-module nominal identity | Show that same-shaped types from different modules remain distinct. |
| `modules-unreachable-source` | Unreachable source | Supply an unused source module and show that it produces no reachable output. |
| `modules-import-alias-normalization` | Import alias normalization | Use equivalent logical import spellings that resolve to one module. |

### Errors and Cleanup

| ID | Name | Description |
|---|---|---|
| `errors-nested-try-block` | Nested-block try | Propagate a fallible result from inside an `if` or loop block exactly once. |
| `errors-try-expression` | Try expression | Preserve and use the successful value produced by a `try` expression. |
| `errors-try-spawn-in-loop` | Try spawn in a loop | Spawn fallible tasks from inside a `for` loop and join each result. |
| `errors-error-construction` | Error construction | Construct an `Error` with `Error.new` and return it through a fallible function. |
| `errors-channel-send-after-close` | Channel send failure | Propagate the `Error` returned by a send attempted after channel closure. |
| `errors-success-failure-cleanup` | Success and failure cleanup | Combine `defer` and `errdefer` across successful and fallible paths. |

### Collections

| ID | Name | Description |
|---|---|---|
| `collections-dictionary-length-history` | Dictionary length history | Observe dictionary length after insertion, replacement, and removal. |
| `collections-dictionary-remove` | Dictionary removal | Retrieve a value from `remove`, use it, and perform the required cleanup. |
| `collections-list-clear-reuse` | List clear and reuse | Clear a list, append new values, read them, and release the list. |
| `collections-list-slice-view` | List slice view | Create a non-owning view from a list slice and read through it. |
| `collections-empty-view` | Empty view | Construct `View.empty()` and use its zero-length behavior safely. |

### Text

| ID | Name | Description |
|---|---|---|
| `text-multibyte-length` | Multibyte length | Distinguish String rune length from the byte length of its UTF-8 view. |
| `text-multibyte-slice` | Multibyte slice | Slice a String with rune bounds and inspect the returned byte view. |
| `text-from-bytes` | String from bytes | Construct a String from a valid UTF-8 byte view. |
| `text-from-runes` | String from runes | Construct a String from a rune view and inspect the result. |
| `text-independent-cursors` | Independent rune cursors | Copy a rune cursor and advance the copies independently over shared text. |

### Tasks and Synchronization

| ID | Name | Description |
|---|---|---|
| `concurrency-task-detach` | Detached task | Spawn a task, detach it, and demonstrate result-discarding ownership. |
| `concurrency-atomic-exchange` | Atomic exchange | Replace an atomic value and use the value returned by `exchange`. |
| `concurrency-atomic-compare-exchange` | Atomic compare-exchange | Demonstrate both successful and unsuccessful compare-exchange operations. |
| `concurrency-atomic-fetch-sub` | Atomic fetch subtraction | Decrement a supported atomic integer with `fetch_sub`. |
| `concurrency-channel-state` | Channel state | Inspect channel length, capacity, and closed state around a receive. |
| `concurrency-cpu-saturating-fibers` | CPU-saturating M:N fibers | Queue 128 CPU-bound fibers so the runtime's one-worker-per-logical-processor scheduler can occupy all available workers. |

### Pointers and Memory

| ID | Name | Description |
|---|---|---|
| `memory-array-element-pointer` | Array element pointer | Take a mutable pointer to an array element and update it. |
| `memory-object-member-pointer` | Object member pointer | Take a pointer to an object field and use it through the pointer. |
| `memory-heap-alias-lifetime` | Heap alias lifetime | Create shallow pointer aliases and perform one explicit cleanup. |

## Explicitly not new

The following concepts already have catalog coverage and MUST NOT be added as
duplicates under new IDs in this RFC:

- nested lists;
- pointer-to-view conversion;
- String concatenation and `to_string`;
- deferred cleanup ordering;
- channel send and receive;
- atomic load, store, and fetch-add;
- cooperative task yielding;
- nested module imports;
- exported type and method usage.

## Implementation notes

- Preserve the existing category IDs where the new snippet belongs to an
  existing category. Add a category only if the loader's current category
  structure requires it; this RFC does not require a ten-category shape.
- Use the current compiler call `compiler.Compile(sources, entrypoint,
  compiler.Project{})` in tests.
- Add each new snippet to the generated-artifact manifest by recompiling the
  catalog and hashing every returned file. Do not hand-edit hashes.
- Do not modify `docs/reference.md`: this RFC uses existing language behavior.
  Verify that each selected API and restriction already matches the reference.

## Validation

This section is exhaustive. RFC 0092 is complete only when every item below
passes:

- The catalog contains the existing 98 snippets plus exactly the 31 IDs in
  this RFC, for a total of 129 snippets.
- Every new snippet has an `app.hex` entrypoint, valid metadata, and no more
  than 24 non-empty source lines.
- Every new snippet compiles successfully through
  `compiler.Compile(snippet.Sources, snippet.Entrypoint, compiler.Project{})`.
- The generated-artifact manifest contains exactly one entry for every new
  snippet and no entry for a removed or renamed snippet.
- All existing snippet manifest entries remain byte-for-byte unchanged unless
  the implementation produces a documented global artifact change.
- `go test ./workbench/snippets` passes, including catalog compilation,
  manifest comparison, and the catalog-wide non-hollow component assertion.
- `go test ./...` passes without invoking an external process.
- `docs/reference.md` is reviewed and no semantic or signature update is
  required.
- `docs/status.md` names RFC 0092 while it is open and removes the entry when
  the RFC is closed.
- The workbench binary is rebuilt into `bin/` and the running workbench is
  restarted before handoff.
