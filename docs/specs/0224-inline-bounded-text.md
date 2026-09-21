# RFC 0224: Unified Text and Collection Storage

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; exploratory proposal; not scheduled
- Created: 2026-09-20
- Updated: 2026-09-20
- Depends on: the current `String`, `Strand`, `Array<T, N>`, and `List<T>`
  contracts in `docs/reference.md`
- Coordinates with: RFC 0139 (String/Strand comparison), RFC 0151 (fixed-array
  syntax), RFC 0152 (generic Strand capacity), RFC 0153 (slice semantics),
  RFC 0160 (memory-bug inventory), and RFC 0165 (local alias diagnosis)
- Does not update `docs/reference.md`: this RFC explores a surface change;
  implementation and reference migration require a later decision

## Summary

Hexal currently has two pairs of related but separate concepts:

```text
Strand        inline, fixed-capacity, literal-only text
String        heap-backed, dynamically sized, owning text handle

Array<T, N>   inline, fixed-length sequence
List<T>       heap-backed, dynamically sized sequence
```

This RFC explores removing the dedicated `Strand` type and replacing the four
types with two type families:

```text
String        heap-backed dynamic text
String[N]        inline fixed-capacity UTF-8 text

List<T>       heap-backed dynamic sequence
List<T>[N]    inline fixed-capacity sequence
```

`String[N]` would replace `Strand`. `List<T>[N]` would replace
`Array<T, N>`.

The proposal is motivated by one storage-shape pattern: dynamic values use
the unsuffixed family; inline bounded values carry a size parameter. It also
makes the relationship between text and collections visible instead of
maintaining separate `Strand` and `Array` concepts.

## Alignment with Hexal's goals

The proposal aligns with the language goals in several ways:

- fewer dedicated compiler-known types;
- one storage-shape pattern for dynamic and inline values;
- explicit allocation remains visible in dynamic forms;
- inline forms retain predictable C layouts and no heap allocation;
- no ownership, borrow, lifetime, move, or garbage-collection concept is
  required;
- the model remains compatible with Zig/Odin-style explicit allocation and
  value types.

The proposal preserves the current distinction between fixed and dynamic
storage while allowing both forms to have a logical length. `List<T>[N]` has
inline storage for at most N elements and starts empty unless initialized with
elements. Unsuffixed `List<T>` has heap-backed, dynamically growing storage.

## Current behavior being reconciled

`Strand` is an immutable literal-only inline value with 31 payload bytes, a
terminating NUL, and zero-filled tail bytes. It exposes only `length()` and
`to_string(heap)`. It cannot be freed and does not expose byte slices.

`String` is immutable UTF-8 stored behind a heap-backed handle. Runtime
Strings require one matching `free`; literals are static-backed and cannot be
freed. String exposes byte slices, rune iteration, slicing, construction,
concatenation, interpolation, and explicit cleanup.

`Array<T, N>` is inline, fixed-length, and has no structural resize.
`List<T>` is heap-backed, shallow-copyable, dynamically sized, and invalidates
active traversals when its structure changes.

## Proposed surface

```text
String[N]       // inline bounded UTF-8 text
List<T>[N]       // inline bounded sequence

String           // heap-backed dynamic text
List<T>          // heap-backed dynamic sequence
```

The bracket parameter is a positive compile-time integer.

- For `String[N]`, N is the maximum UTF-8 payload capacity in bytes,
  excluding the terminator.
- `List<T>[N]` has capacity N and logical length in `0..N`.
- Unsuffixed `List<T>` is the variable-length heap-backed form.

Storage is selected by the presence of the fixed-size parameter:

```text
String                 heap allocated, variable size
String[N]              inline/stack allocated, fixed capacity
List<T>                heap allocated, variable size
List<T>[N]             inline/stack allocated, capacity N, length 0..N
```

"Stack allocated" means inline value storage. When an inline value appears in
an aggregate, module storage, or another inline value, it follows that
containing value's storage rather than being forced onto the machine stack.

## Candidate text semantics

### Representation

`String[N]` is an inline value containing byte storage. The final byte is NUL.
The logical text length is bounded by N and may be less than N. The value is
immutable, copied by value, and never freed.

This preserves the useful part of `Strand` while making capacity explicit.
The representation may instead include a stored length if bounded scanning is
shown to be materially expensive.

`String[N]` stores UTF-8 bytes internally, matching the current `String` and
`Strand` representations. N measures payload bytes, excluding the terminating
NUL. The text API may expose either view without changing the storage type:

- rune-oriented operations use UTF-8 decoding and Rune bounds;
- byte-oriented views expose the encoded bytes and use byte bounds;
- neither view changes the fixed value's capacity or creates a second text
  representation.

There is no `String<Byte>` or `String<Rune>` type in this proposal. `Rune`
remains the scalar returned by rune iteration, not an alternative String
storage parameter.

### Construction and conversion

Preferred literal construction is contextual:

```hexal
let short: String[32] = "Hello"
```

A literal exceeding N bytes, containing NUL, or containing invalid UTF-8 is
rejected at compile time.

Computed construction needs an explicit checked operation:

```hexal
let fixed: String[32] = String[32].from_bytes(bytes)
```

The recommended behavior is a runtime trap or `Error` for invalid UTF-8 or
overflow, with no implicit heap allocation.

A smaller inline value may widen without allocation:

```hexal
let small: String[16] = "Hello"
let large: String[64] = small
```

The reverse conversion must not silently truncate. Inline-to-heap conversion
is explicit:

```hexal
let owned: String = small.copy(heap)
```

The proposed public String API is:

```text
String.rune_length() -> Size
String.byte_length() -> Size
String.bytes() -> Slice<Byte>
String.byte_slice(start: Integer, end: Integer) -> Slice<Byte>
String.rune_slice(start: Integer, end: Integer) -> Slice<Rune>
String.runes() -> RuneCursor
String.copy(heap: Heap) -> String
String.concat(heap: Heap, other: String) -> String
String.from_bytes(heap: Heap, bytes: Slice<Byte>) -> String
String.from_runes(heap: Heap, runes: Slice<Rune>) -> String
String.interpolate(heap: Heap, template: InterpolationTemplate) -> String
String.free(heap: Heap) -> no value
```

The same read and conversion surface is intended for `String[N]` where it
fits. `String[N].copy(heap)` converts inline UTF-8 storage to a heap-backed
String. `String.copy(heap)` explicitly clones an existing heap-backed String.

`byte_slice` uses byte bounds. `rune_slice` uses Rune bounds, but its return
type cannot be a zero-copy view over UTF-8 storage; its materialization and
storage lifetime remain an open design issue below.

## Candidate collection semantics

### Recommended interpretation

Use `List<T>[N]` as the inline fixed-capacity replacement for `Array<T, N>`:

```hexal
let values: List<Int32>[10] = []
values.push(1)
```

Under this interpretation:

- storage is inline and capacity is exactly N;
- logical length starts at zero for `[]` and ranges from zero through N;
- `push` appends while length is less than N and traps at capacity;
- `pop` removes the last element and traps when the list is empty;
- `clear` removes all logical elements without changing capacity;
- indexing and slicing use the current logical length;
- elements copy by value using ordinary shallow-copy rules;
- no heap allocation occurs.

`List<T>` remains the dynamic heap-backed sequence with `push`, `pop`,
`clear`, and `free` operations.

Both forms expose `push`, `pop`, and `clear`. Only heap-backed `List<T>`
exposes `free`; calling `free` on `List<T>[N]` is a compile-time error because
the fixed form has no heap allocation.

## Generic and conversion rules

Each capacity/size is recommended to be part of the canonical inline type:

```text
String[16] and String[32] are distinct types
List<Int32>[3] and List<Int32>[4] are distinct types
```

Recommended conversions:

| Conversion | Recommendation |
|---|---|
| `String[N]` -> `String[M]`, N <= M | implicit widening copy |
| `String[N]` -> `String[M]`, N > M | reject unless explicitly checked |
| `String[N]` -> `String` | explicit `copy(heap)` |
| `String` -> `String[N]` | explicit checked conversion only |
| `List<T>[N]` -> `List<T>[M]` | explicit copy unless safe widening is approved |
| `List<T>[N]` -> `List<T>` | explicit heap copy; no implicit allocation |
| `List<T>` -> `List<T>[N]` | explicit checked copy only |

No conversion transfers ownership or creates a lifetime relationship.

## Memory and C representation

- Inline text and fixed lists have no `free` operation. Calling `free` on a
  fixed list is a compile-time error because its backing storage is inline.
- Dynamic `String` and `List<T>` retain explicit cleanup.
- Copies remain shallow for dynamic handles and by-value for inline values.
- RFC 0165 may track raw-pointer aliases inside elements only where its local
  rules can prove the allocation identity; it gains no collection ownership
  model.
- RFC 0158 tracks physical heap allocations only. Inline forms produce no
  allocation-leak events.
- Fixed forms do not make pointers or slices into their storage automatically
  safe after reassignment or scope exit. Existing provenance and escape rules
  remain.
- No runtime metadata is required in ordinary builds.

The generated C should remain obvious, for example:

```c
typedef struct { uint8_t data[33]; } hex_string_32;
typedef struct { T data[3]; } hex_list_T_3;
```

Inline forms are complete value types. Dynamic forms remain pointer-sized
handles. Inline-to-heap conversion is visible in generated C.

## Migration outline

| Current form | Proposed form |
|---|---|
| `Strand` | `String[32]` or another explicit capacity |
| `Array<T, N>` | `List<T>[N]` initialized with N elements |
| `String` | `String` |
| `List<T>` | `List<T>` |

The migration must audit grammar, type interning, literal contextual typing,
Error headers, Dict key eligibility and hashing, iteration, slicing, generated
C helpers, manifests, snippets, tests, and all reference-document rules.

## Non-goals

- Ownership, borrowing, lifetimes, affine moves, or automatic cleanup.
- A fourth text representation or separate fixed-buffer abstraction.
- Implicit heap allocation during conversions.
- Implicit truncation.
- Changing UTF-8 validation or rune semantics.
- Making slices into inline values lifetime-safe through a new checker system.
- Replacing dynamic List invalidation checks with static ownership analysis.

## Open questions

1. **How should `rune_slice()` materialize `Slice<Rune>` from UTF-8?**
   - A. Allocate a temporary Rune buffer and return a Slice to it. This needs
     an owner or lifetime rule and is not valid as a bare non-owning Slice.
   - B. Return a compiler-owned Rune view object instead of `Slice<Rune>`.
   - C. Make the caller provide a destination `List<Rune>`/Slice and keep
     `rune_slice()` out of the first implementation (recommended until the
     ownership of the returned storage is defined).

2. **How should computed inline strings be constructed?**
   - A. Explicit checked constructors returning `String[N] | Error`
     (recommended).
   - B. Trapping constructors matching fixed Array indexing.
   - C. No computed construction; literals only.

3. `free` is available only on heap-backed `List<T>`. Calling it on
   `List<T>[N]` is a compile-time error because the fixed form has no heap
   allocation.

4. **Should inline-to-heap conversions be implicit?**
   - A. No; require `copy(heap)` or another explicit copy operation
     (recommended).
   - B. Permit contextual implicit allocation, hiding a heap operation.

5. Should dynamic `String` and fixed `String[N]` be distinct canonical types with
   shared methods? The recommended answer is yes because representation,
   allocation, and cleanup differ.

## Validation sketch

A later implementation spec must exhaustively validate literal capacity and
UTF-8/NUL diagnostics, widening and narrowing conversions, computed
 construction, dynamic versus inline cleanup, fixed-list capacity, length,
indexing, slicing, iteration, push/pop/clear behavior, Dict key eligibility,
Error headers, generated C and helper emission, manifest impact,
and the absence of new ownership, borrow, lifetime, or automatic-cleanup
rules. It must also validate push-at-capacity, pop-on-empty, clear, and the
compile-time rejection of `free` on fixed lists.
