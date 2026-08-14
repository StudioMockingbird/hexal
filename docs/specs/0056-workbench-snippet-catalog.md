# RFC 0056: Workbench Snippet Catalog

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-14
- Created: 2026-08-14
- Updated: 2026-08-14
- Scope: the executable example catalog embedded in the Hexal workbench
- Depends on: `docs/reference.md` and the implemented compiler pipeline
- Coordinates with: workbench snippet validation and future workbench UX work

## Summary

Expand the workbench catalog from its current 41 examples across 7 categories to
108 small, meaningful Hexal programs across 11 categories. Every snippet
demonstrates an implemented language feature or semantic nuance and compiles
through the public in-memory compiler API.

Category sizes follow their feature areas rather than a quota: most hold ten,
Errors and Cleanup holds eight because `try`, `defer`, and `errdefer` are a
small surface that earlier drafts padded with four restatements of "return
`T \| Error` and handle it".

This specification defines the catalog content and quality bar. It does not
change Hexal syntax or semantics, and it does not authorize draft features
that are outside the current implementation.

## Goals

- Provide approximately 100 snippets grouped into coherent categories sized by
  their feature area.
- Keep each snippet short enough to understand at a glance.
- Prefer a tiny complete program over an isolated syntax fragment.
- Demonstrate interactions and semantic nuances such as copying, narrowing,
  explicit conversion, deferred cleanup, and end-of-stream handling.
- Keep every example executable through `compiler.Compile` with in-memory
  sources and a logical entrypoint.
- Make the catalog useful as both a workbench showcase and a compiler smoke
  corpus.

## Non-goals

- Adding language features solely to support a snippet.
- Adding C interop, Arena, Pool, or multi-module examples before those features
  are implemented and accepted by the reference contract.
- Executing generated C during catalog validation.
- Turning the catalog into a tutorial or replacing the normative reference.

## Catalog rules

1. Around 100 snippets across coherent categories is a rough target, not a
   requirement. A category holds as many snippets as its feature area justifies
   and may exceed or fall short of any nominal count. A category MUST NOT be
   padded to reach a quota, and a snippet MUST NOT exist solely to fill one.
   Coverage of an implemented feature outranks the total.
2. Every snippet MUST have a unique stable ID, a name, a concise description,
   an `app.hex` entrypoint, one or more in-memory source strings, and feature
   tags.
3. A snippet SHOULD contain no more than 20 non-empty source lines. A longer
   example requires a concrete justification in its description or review
   notes.
4. A snippet MUST do more than declare a value. It SHOULD contain an operation,
   branch, iteration, call, mutation, narrowing step, resource action, or
   interaction between at least two language features.
5. Each snippet SHOULD demonstrate one primary concept and no more than two
   supporting concepts. Examples that combine several features are permitted
   when the combination is itself the point.
6. Every snippet MUST compile with the current public compiler API. The catalog
   test MUST remain pure Go and MUST NOT invoke GCC, Clang, or another external
   process.
7. Snippets MUST use syntax and library/runtime operations already accepted by
   the parser, checker, and generator. A feature may be included only when it
   is implemented in the current compiler and consistent with `reference.md`.
8. Features and reserved words claimed by a snippet MUST occur in its source.
9. Runtime behavior MUST be safe for ordinary execution. Snippets MUST NOT
   intentionally trigger documented traps such as out-of-bounds access, empty
   `pop`, missing dictionary keys, zero division, invalid mutex use, or failed
   allocation.
10. The catalog SHOULD prefer stable, deterministic examples that do not depend
    on host files, scheduling order, locale, wall-clock time, or external
    services.

## Proposed catalog

The following catalog is the required content for the expansion. Descriptions
state the intended behavior; implementation may choose equivalent source as
long as the catalog rules and feature coverage remain intact.

### Values and Bindings

| ID | Snippet name | Description |
|---|---|---|
| values-scalar-dashboard | Scalar dashboard | Declare representative signed, unsigned, floating-point, boolean, and size values, then use them in a small calculation. |
| values-mutable-accumulator | Mutable accumulator | Update a mutable integer through several meaningful steps and return the total. |
| values-packet-header | Packet header | Decode flags using hexadecimal, binary, octal, and separated integer literals. |
| values-text-record | Text record | Combine `String`, `Strand`, `Byte`, and `Rune` values in one small record-like program. |
| values-immutable-snapshot | Immutable snapshot | Copy an object, mutate the original, and demonstrate value copying. |
| values-score-alias | Score alias | Define a domain-specific numeric alias and use it in an object. |
| values-boolean-policy | Boolean policy | Combine comparisons with `and`, `or`, and boolean negation to choose a policy. |
| values-layout-total | Compile-time layout total | Combine `size_of` and `align_of` for an object type. |
| values-commented-config | Commented configuration | Group related settings with line and block comments while using the values. |
| values-literal-normalization | Literal normalization | Use escaped strings, bytes, runes, and numeric separators together in a normalized value. |

### Functions and Objects

| ID | Snippet name | Description |
|---|---|---|
| functions-temperature-classifier | Temperature classifier | Return a label from a function using conditional branches. |
| functions-counter-method | Counter method | Define a mutable receiver method that increments and returns object state. |
| functions-point-translation | Point translation | Use an object method to update two coordinates. |
| functions-value-pipeline | Function value pipeline | Store a named function in a `Fun` value and invoke it with typed arguments. |
| functions-no-result-notifier | No-result notifier | Define and call a function without a return value. |
| functions-generic-identity | Generic identity | Preserve a value's type through a generic function. |
| functions-generic-container | Generic container | Store and retrieve a value through a generic object. |
| functions-object-formatter | Object formatter | Pass an object to `print` as a structured value. |
| functions-nested-object-update | Nested object update | Mutate a field inside a nested object through a mutable binding. |
| functions-receiver-forms | Receiver forms | Contrast the three `impl` targets: a value receiver copies, `Ptr<T>` reads caller storage, and `MutPtr<T>` writes it. |

### Control Flow

| ID | Snippet name | Description |
|---|---|---|
| control-grade-bands | Grade bands | Use an `if`/`elseif`/`else` chain to classify a score. |
| control-search-break | Search with break | Scan an array and stop when a matching value is found. |
| control-filter-continue | Filter with continue | Iterate through values while skipping unwanted elements. |
| control-bounded-counter | Bounded counter | Use a `while` loop with a changing termination condition. |
| control-nested-coordinate-scan | Nested coordinate scan | Traverse a small grid using nested loops. |
| control-indexed-total | Indexed total | Use `for index, value in array` to calculate a weighted total. |
| control-three-value-iteration | Three-value iteration | Use the supported three-binder `for ... in` form to expose iteration context. |
| control-guarded-update | Guarded update | Reject an invalid state early before performing an update. |
| control-boolean-state-machine | Boolean state machine | Move through a small state sequence using conditions and a mutable label. |
| control-truthiness | Truthiness | Show that only `false` and `nil` are falsey: a zero integer, an empty text value, and an empty collection all take the truthy branch. |

### Types and Matching

| ID | Snippet name | Description |
|---|---|---|
| types-nullable-score | Nullable score | Narrow `Int32 \| Nil` before reading the value. |
| types-multi-type-value | Multi-type value | Distinguish an `Int32 \| Bool \| Nil` union with `is`. |
| types-direction-match | Direction match | Convert an ADT direction into a numeric step. |
| types-command-dispatch | Command dispatch | Match unit and record variants of a command type. |
| types-shape-area | Shape area | Match circle and rectangle variants to calculate an area. |
| types-optional-configuration | Optional configuration | Match a present configuration value against `nil`. |
| types-result-like-union | Result-like union | Return either a value or `Error` from a function. |
| types-exhaustive-protocol | Exhaustive protocol | Match every protocol variant with an explicit fallback. |
| types-variant-payload | Variant payload access | Match a record variant and use its payload fields. |
| types-type-based-formatting | Type-based formatting | Select formatting behavior based on a union member type. |

### Numeric and Operators

| ID | Snippet name | Description |
|---|---|---|
| numeric-arithmetic-expression | Arithmetic expression | Combine addition, subtraction, multiplication, division, and remainder. |
| numeric-precedence-check | Precedence check | Show how grouped and ungrouped expressions produce different results. |
| numeric-range-comparison | Range comparison | Use relational decisions to classify a number. |
| numeric-bit-mask-test | Bit mask test | Test and combine flag bits with `&`, `\|`, and shifts. |
| numeric-bit-flag-update | Bit flag update | Set, clear, and toggle individual flags. |
| numeric-signed-conversion | Signed conversion | Convert a wide signed integer to a narrow type explicitly. |
| numeric-floating-conversion | Floating conversion | Widen an integer into a floating-point value without explicit syntax. |
| numeric-rune-conversion | Rune conversion | Convert a numeric code point into a `Rune`. |
| numeric-bit-cast | Bit cast | Reinterpret a `UInt32` bit pattern as `Float32`. |
| numeric-endian-round-trip | Endian round trip | Encode an integer as bytes and reconstruct it. |

### Pointers and Memory

| ID | Snippet name | Description |
|---|---|---|
| memory-read-pointer | Read pointer | Create a read-only pointer and read its value. |
| memory-mutable-pointer | Mutable pointer | Update a variable through `MutPtr<T>`. |
| memory-nullable-pointer | Nullable pointer | Handle a pointer that may contain `nil`. |
| memory-pointer-view | Pointer view | Convert pointer plus length into a bounded `View<T>`. |
| memory-opaque-pointer | Opaque pointer | Erase and restore a pointer through `Unknown`. |
| memory-heap-integer | Heap integer | Allocate an integer, update it, and release it. |
| memory-deferred-free | Deferred free | Ensure heap cleanup runs when a function returns. |
| memory-heap-object | Heap object | Allocate a small object and mutate it through a pointer. |
| memory-pointer-swap | Pointer-based swap | Exchange two values through mutable pointers. |
| memory-volatile-register | Volatile register | Write and read a simulated memory-mapped register. |

### Collections

| ID | Snippet name | Description |
|---|---|---|
| collections-array-statistics | Array statistics | Calculate minimum, maximum, and total over a fixed array. |
| collections-array-slicing | Array slicing | Create a view over the middle of an array. |
| collections-view-summation | View summation | Sum values through indexed and checked view access. |
| collections-list-builder | List builder | Push several values into a heap-backed list and read them. |
| collections-list-stack | List stack | Use `push` and `pop` to model a small stack. |
| collections-dictionary-lookup | Dictionary lookup | Insert scores and retrieve a value by key, guarding with `contains`. |
| collections-frequency-table | Frequency table | Count repeated integer values with a dictionary. |
| collections-handle-elements | Handle elements | Store `String` handles in an `Array` and a `List`; insertion copies the handle, not the allocation. |
| collections-element-cleanup | Element cleanup | Free each distinct element allocation exactly once before freeing the container, and show why the container never frees its elements. |
| collections-nested-list | Nested list | Build a `List<List<Int32>>` and read through both levels. |

### Text

| ID | Snippet name | Description |
|---|---|---|
| text-text-inspection | Text inspection | Read the first rune and first byte from a string. |
| text-rune-cursor | Rune cursor | Iterate through text using a `RuneCursor`. |
| text-byte-view | Byte view | Expose a string's bytes as a `View<Byte>`. |
| text-byte-inspection | Byte inspection | Read selected bytes from a text view. |
| text-text-report | Text report | Print multiple text values as one formatted report. |
| text-escaped-message | Escaped message | Build a message containing newline, tab, and quote escapes. |
| text-string-building | String building | Build a runtime `String` with `concat` and `to_string`, then free it exactly once. |
| text-strand-inline | Inline strand | Use `Strand` as an inline literal-only value and convert it to a `String`. |
| text-comparison | Text comparison | Compare `String` values by bytes and order them lexicographically. |
| text-protocol-parser | Text protocol | Use `String`, `Rune`, and `EoS` in one small parser loop. |

### Streams

| ID | Snippet name | Description |
|---|---|---|
| streams-empty | Empty stream | Create an allocation-free empty stream and observe immediate `EoS`. |
| streams-list-source | List source | Pull values from a list-backed stream. |
| streams-manual-pull | Manual pull | Call `next` and distinguish a value from the end marker. |
| streams-for-iteration | Stream iteration | Consume a stream with `for ... in` until it completes. |
| streams-filter | Filter | Keep only the values a predicate accepts. |
| streams-map | Map | Transform each value into another type. |
| streams-take | Take | Limit a stream to a fixed number of values. |
| streams-adapter-chain | Adapter chain | Compose `filter`, `map`, and `take` over one `Heap`, and stop using the upstream handles. |
| streams-produce | Custom producer | Drive a stream from inline state and a named callback. |
| streams-cleanup | Stream cleanup | Free the adapter chain once; exhaustion alone releases nothing. |

### Errors and Cleanup

| ID | Snippet name | Description |
|---|---|---|
| errors-validation-error | Validation error | Return `Int32 \| Error` when an input is invalid, and inspect the union at the call site. |
| errors-propagating-read | Propagating read | Use `try` to propagate a failure through two sequential operations. |
| errors-success-cleanup | Success cleanup | Use `defer` to release a resource on every return path. |
| errors-failure-cleanup | Failure cleanup | Use `errdefer` so cleanup runs only on an Error exit and is discarded on success. |
| errors-defer-order | Cleanup order | Register several deferred actions and show they run in reverse registration order. |
| errors-cleanup-helper | Cleanup helper | Keep resource cleanup in a dedicated no-result function. |
| errors-file-write | File write | Open, write, flush, and close a file with deferred cleanup. |
| errors-file-read | File read | Read a file's text, handle malformed UTF-8 as a recoverable Error, and free the result. |

### Tasks and Concurrency

| ID | Snippet name | Description |
|---|---|---|
| concurrency-spawn-join | Spawn and join | Spawn a typed function and return its joined result. |
| concurrency-two-tasks | Two tasks | Spawn two independent calculations and combine results. |
| concurrency-channel-send | Channel send | Create a bounded channel, send a value, and close it. |
| concurrency-channel-receive | Channel receive | Receive a value and distinguish it from `EoS`. |
| concurrency-channel-round-trip | Channel round trip | Send a computed value through a channel and consume it. |
| concurrency-mutex-critical-section | Mutex critical section | Lock and unlock a heap-backed mutex around a short operation. |
| concurrency-atomic-counter | Atomic counter | Use `load`, `store`, and `fetch_add` on an atomic object member shared by pointer. |
| concurrency-yield-loop | Cooperative yield | Show why every repeating path through a task-reachable `while true` must execute `Task.yield()`. |
| concurrency-worker-handoff | Worker handoff | Send work through a channel and receive the result. |
| concurrency-concurrent-cleanup | Concurrent cleanup | Combine task creation with deferred channel or mutex cleanup. |

## Validation

Implementation of this RFC is complete only when:

- The catalog contains the eleven categories and 108 snippets specified
  above, plus the retained modules category (two snippets), for 110 snippets
  across twelve categories.
- No snippet duplicates another's primary concept. A reviewer can name what each
  one teaches that no sibling does.
- Every catalog source compiles through `compiler.Compile` using the existing
  workbench smoke test.
- No snippet exceeds the agreed line-length target without justification.
- The catalog validator reports no duplicate IDs, missing entrypoints, missing
  feature tags, or missing reserved-word coverage.
- `go test ./...` passes without an external toolchain.
- The workbench binary is rebuilt into `bin/` and the running workbench is
  restarted before handoff.

## Resolved design decisions

- The eleven category names replace the previous seven; every category and
  snippet ID is fresh and stable (`01-values-and-bindings` ...
  `12-modules`).
- The modules category is retained as the twelfth category: RFC 0034 modules
  are implemented, so its snippets compile legitimately. The catalog holds
  110 snippets: the 108 specified plus the ported `module-import-export`
  example and one additional nested-import example.
- The 20-line limit is a soft upper bound: the validator reports over-length
  snippets as warnings, never failures. `text-protocol-parser` exceeds it at
  25 lines because it is the only snippet covering `as`, `match`, `then`, and
  `eos` together.
- The workbench UI exposes feature tags as a snippet filter next to the
  category selector.
