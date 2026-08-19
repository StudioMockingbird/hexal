# RFC 0087: Cached Rune Length

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-19
- Scope: store the rune count in the String header so `length()` is O(1), and
  the text-surface consequences that follow
- Depends on: nothing
- Supersedes: RFC 0083 Part B's **unimplemented half** — the rename of
  `length()` to `rune_length()` and the retention of `is_empty`. Part B's index
  removal has already landed and is unaffected, as is Part A.
- Coordinates with: `docs/reference.md`, RFC 0039 (C interop pins struct
  layouts), `docs/status.md`
- Does not change: String immutability, ownership, allocation count, or the
  handle representation

## Summary

`String.length()` is an O(n) UTF-8 scan on every call. The count can be computed
once at construction and stored, because **every construction path already knows
it or already walks the bytes**.

This is a strictly better outcome than RFC 0083 Part B reached without it: one
removal instead of a removal plus a rename plus a permanent asymmetry.

## Evidence — the count is free at every construction site

| Path | Cost of maintaining the count |
|---|---|
| `from_bytes` | **zero** — already walks the whole sequence to validate UTF-8 (`string.c:91-93`); the loop's iteration count is the rune count |
| `from_runes` | **zero** — the count is the `length` parameter |
| `concat` | **O(1)** — `left->rune_length + right->rune_length`, beside the `byte_length` add it already performs |
| literals | **zero** — computed at compile time, emitted as a constant |
| `to_string` | **zero** — copies the field |

**Literals are the sixth site and the only one outside the C templates.** They
do not use `hex_string_storage`; the Go emitter writes two objects, and the
header with a **positional** initializer:

```c
const uint8_t hex_lit_0_bytes[5] = { 99, 97, 102, 101, 0 };
const hex_string hex_lit_0 = { hex_lit_0_bytes, 4 };
```

Adding a third field to a positional initializer that is not updated does not
fail to compile — C zero-fills the remainder, so every literal would silently
report a rune length of zero. That is exactly the "wrong length rather than a
crash" failure this RFC's Drawbacks warns about, arriving at the one
construction site the templates do not own.

**Emit literals with designated initializers instead:**

```c
const hex_string hex_lit_0 = {
    .data = hex_lit_0_bytes, .byte_length = 4, .rune_length = 4 };
```

C23 has them, they cost nothing, and a future field added without updating this
site becomes a visible omission rather than a silent zero. Do the same at the
three `storage->header.…` assignments in `string.c` for the same reason.

`from_bytes` is the decisive case:

```c
size_t index = 0;
while (index < length) {
    hex_utf8_next(data, length, &index);
}
```

The scan is mandatory for validation and is discarded today. Counting its
iterations costs one increment.

## The change

```c
typedef struct hex_string {
    const uint8_t *data;
    size_t byte_length;
    size_t rune_length;     /* new */
} hex_string;
```

`hex_string_rune_length` becomes a field read. Nothing else consumes a rune
position: `hex_string_at_rune` and `hex_strand_at_rune` were deleted when RFC
0083 removed text indexing, and `rune_cursor` walks incrementally from its own
stored offset. With the count cached, no O(n) rune path remains.

Space cost is 8 bytes on the header, which is heap-allocated once per string and
shares one allocation with the bytes (`hex_string_storage`). **String values stay
8-byte handles**, so `List<String>` element size, copying, and passing are all
unaffected.

## Text-surface consequences

### `length()` keeps its name

RFC 0083 Part B renamed it `rune_length()` to make an O(n) cost visible in the
name. With the count cached there is no hidden cost, so the rename loses its
justification and String keeps `length()` — the same spelling as `Array`,
`View`, and `List`.

### `is_empty()` is removed from String and Strand

RFC 0063 removed `is_empty` from Array/View/List because `length() == 0` was an
identical O(1) test, and kept it on String and Strand only because their
`length()` was O(n). That reason is now gone for String.

Removing it restores the symmetry RFC 0063 sought and RFC 0083 recorded as an
unavoidable wart. Every collection and text type now answers `length()`, and
none has `is_empty`.

Strand follows String for uniformity, as decided in RFC 0083 — `t.length() == 0`
is a comparison against a bounded scan of at most 31 bytes.

### Indexing stays removed

RFC 0083 Part B already removed `String[index]` and `Strand[index]`, and a
cached count does not argue for their return: reaching the nth rune still
walks. `bytes()` remains the O(1) byte path and `rune_cursor()` the O(n)-total
iteration path.

## Strand keeps its scan

Strand is inline 32 bytes: at most 31 UTF-8 bytes, a NUL, then zero fill. There
is no spare room for a count without reducing capacity to 30 bytes.

**Do not add one.** The scan is bounded by 31 bytes, which is constant time in
the sense that matters, and trading a byte of capacity to avoid it is a bad
exchange. `hex_strand_rune_length` stays as it is.

This is the one place String and Strand differ in implementation while agreeing
in surface, which is the correct direction: the surface is uniform, the
representation follows each type's storage.

## Timing

**Land this before RFC 0039.** Widening `hex_string` from 16 to 24 bytes changes
a struct that C interop will eventually want pinned, and nothing depends on the
layout today. The same change after bindings exist is a breaking ABI change.

## Invariants

1. String remains immutable, remains a pointer-sized handle, and remains one
   header-plus-bytes allocation. `List<String>` element size is unchanged.
2. `rune_length` is always the exact count of Unicode scalars in `data`. Every
   path that produces a `hex_string` sets it; there is no path that leaves it
   stale or unset.
3. `rune_cursor` is unchanged. It walks incrementally and does not consult the
   cached count.
4. No language surface gains an operation. Two are removed: `is_empty` on
   String and on Strand. Indexing is already gone.
5. Strand's representation is unchanged.

## Validation

- `s.length()` performs no scan: assert on the generated C that it reads the
  field rather than calling a walking helper.
- The count is correct for each construction path — literal, `from_bytes`,
  `from_runes`, `concat`, `to_string` — over ASCII, multi-byte, and
  mixed-width inputs, including an empty string.
- `concat` produces a count equal to the sum of its operands' counts, verified
  against an independent scan of the result.
- `s.is_empty()` and `t.is_empty()` are rejected; `s.length() == 0` is accepted.
- `s[0]` and `t[0]` remain rejected — a regression check on landed behaviour,
  since nothing here should reopen indexing.
- `List<String>` element size is unchanged: the generated C still declares
  `const hex_string **data`.
- `go test ./...`, `go vet ./...`; the snippet manifest moves for text snippets
  and no others.

## Non-goals

- Reinstating `String[index]`. RFC 0083 removed it and a cached count does not
  argue for its return: reaching the nth rune still walks. Random rune access
  would need a rune-offset index, a per-string allocation this RFC does not add.
- Caching anything else — grapheme counts, normalization state, hashes.
- Changing Strand's representation.
- Mutable strings, a builder type, or a rope representation.
- Revisiting RFC 0083 Part A, which is independent.

## Drawbacks

- `hex_string` grows 50%, from 16 to 24 bytes. This lands on the heap header,
  once per string, and not on the 8-byte handle that values, parameters, and
  collection elements actually carry — but a program holding very many short
  strings pays it per string.
- Invariant 2 is a maintenance obligation: every future String constructor must
  set the field, and a missed one produces a wrong length rather than a crash.
  A single construction helper that all paths route through is the way to make
  that structural rather than remembered.
- It supersedes part of a spec that is already implementation-ready, which is
  churn. The alternative is implementing a rename that this change would
  immediately make pointless.
