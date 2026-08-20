# RFC 0083: Text and Collection Surface Corrections

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented. Verified 2026-08-20. Landed: A1 `List.set`
  deleted, A2 `Dict.length` added, and Part B index removal for both String
  and Strand.

  A3 `Dict.find(key) -> V | Nil` is implemented with a component helper that
  returns `const V *`; the module call site hoists that one lookup and builds
  the union. Part B `length()` rename and `is_empty` removal remain withdrawn:
  RFC 0087 caches the rune count, which removes the reason for both.
- Created: 2026-08-19
- Scope: redundant and missing collection operations; the cost model of text
  indexing
- Depends on: nothing
- Coordinates with: `docs/reference.md`, `docs/status.md`, RFC 0063 (which
  removed `at` and `is_empty` from Array/View/List)
- Does not change: collection representation, allocator passing, bounds
  checking, or generated-C structure

## Summary

The collection surface is coherent — uniform `receiver[index]`, uniform
`slice(start, end)`, uniform `length()`, explicit allocator passing. It does not
need an overhaul. It has one redundancy, one capability hole, and one place
where identical syntax hides a different complexity class.

**Part A** removes a duplicate operation and fills the `Dict` gap.
**Part B** decides whether text indexing should stop hiding an O(n) scan.

## Part A — collection surface

### A1. `List.set` duplicates index assignment

Both forms compile today, verified by probe:

```
l[0] = 5      exit=0
l.set(0, 5)   exit=0
```

`List<T>[index]` already yields `place<T>`, so assignment through the index is
the general form. `set` is a second spelling of one operation, against
`AGENTS.md` goal 2 ("only one obvious way").

**Delete `List.set`.** This is the same judgment RFC 0063 applied to `at` and
`is_empty`: when two spellings are provably identical, the specialized one goes.

### A2. `Dict` cannot report its size

```
d.length()    -> Dict<Int32, Int32> has no method length
d.is_empty()  -> Dict<Int32, Int32> has no method is_empty
```

Every other collection answers `length()`. Dict's exclusion is not a design
choice that survives inspection: iteration is deliberately excluded because
order is unstable and unspecified, but **a count is not order-dependent**.

**Add `Dict<K,V>.length() -> Size`**, O(1) from the existing entry counter.

### A3. `Dict.get` traps, so the safe path costs two lookups

`get` and `remove` trap on a missing key. The only safe idiom is `contains`
followed by `get` — two hash lookups for one logical operation, and a wasteful
pattern the language otherwise avoids.

Hexal already has the type to express this. `V | Nil` is a valid union, and
nullability narrowing (`== nil`, `!= nil`, match) is already enforced before
use.

**Add `Dict<K,V>.find(key: K) -> V | Nil`**, one lookup, narrowed before use.

`get` is retained: trapping is correct when the key is known present, and
removing it would force narrowing on every call site that does not need it.
`contains` is also retained — it is the right operation when the value is not
wanted. Both keep their current semantics.

## Part B — the text cost model

### The problem

`String[index]` and `List[index]` are the same syntax with different complexity
classes. From `compiler/generator/packages/string.c`:

```c
size_t hex_string_rune_length(const hex_string *text) {
    while (index < text->byte_length) { ... }      /* O(n) */
}
uint32_t hex_string_at_rune(const hex_string *text, size_t rune_index) {
    for (;;) { ... }                               /* O(n), walks from 0 */
}
bool hex_string_is_empty(const hex_string *text) {
    return text->byte_length == 0;                 /* O(1) */
}
```

So `s[i]` is a UTF-8 walk from the start of the string, while `l[i]` is a
pointer offset. A loop indexing a String by position is silently O(n^2), and
nothing in the syntax says so.

This is the property `AGENTS.md` values most in C — reading code tells you what
it costs — and it is the one place in the collection surface where that fails.

### Two consequences already visible in the surface

- **`is_empty` is asymmetric.** RFC 0063 removed it from Array/View/List because
  `length() == 0` was identical; String and Strand keep it because their
  `length()` is O(n) and `is_empty` is O(1). The asymmetry is *correct* and
  looks arbitrary, because nothing in the names explains it.
- **`String.slice` mixes units.** It takes Rune bounds and returns `View<Byte>`.
  `s.slice(0, 2)` means runes 0 through 2 and produces a byte view — two unit
  systems in one signature, which only reads as reasonable once the reader knows
  indexing is rune-based.

### Decided — Option A, then partly superseded by RFC 0087

**Read RFC 0087 before implementing this part.** It caches the rune count in the
String header, which removes the O(n) cost this section exists to make visible.
The consequences:

- **`length()` keeps its name.** The rename to `rune_length()` specified below is
  withdrawn — it flagged a cost that no longer exists.
- **`is_empty()` is removed** from String and Strand rather than retained, since
  `length() == 0` becomes an identical test. That reverses the asymmetry this
  section records as unavoidable.
- **`[index]` removal stands unchanged.** Indexing is O(n) with or without the
  cached count.

The reasoning below is still the reasoning; only the rename and the `is_empty`
retention change. The alternatives are recorded because they constrain any future
text addition, not because the choice is open.

**Option A — remove `String[index]`, rename `length()` to `rune_length()`.**
**Decided.**

`bytes()` remains the O(1) byte path, `rune_cursor()` the O(n)-total iteration
path. Random rune access is dropped; `bytes()` still supports byte indexing.

- The cost is stated in the name instead of hidden behind brackets.
- `is_empty` stops looking arbitrary: O(1) `is_empty` beside an explicitly O(n)
  `rune_length` explains itself.
- `slice` stops competing with an indexing operator, so mixed units become a
  documentation issue rather than a trap.
- Random rune indexing into UTF-8 is rarely correct anyway — grapheme clusters,
  not scalars, are usually what callers mean.
- Cost: a breaking change to the text surface, and callers that legitimately
  want the nth scalar must use a cursor.

**Option B — keep the surface, document the cost.** Cheapest, and weakest: a
documented hidden cost is still a hidden cost, and this is precisely the
property the language claims over higher-level languages.

**Option C — make `String[index]` byte-indexed.** O(1) and consistent with
`List`, but it breaks the invariant that String length and indexing agree, and
silently changes the meaning of existing programs. Rejected: a semantic change
that still compiles is worse than one that does not.

### Why A, and not B or C

It is the only option that makes the cost visible, and it resolves the
`is_empty` and `slice` warts as a side effect rather than requiring separate
decisions. It was taken now because a breaking change to the text surface is
cheap today and expensive once programs exist.

### Strand follows String

`Strand[index]` has the same shape — rune-indexed, therefore a scan — but Strand
is inline and capped at 31 UTF-8 bytes, so its cost is bounded by a constant.
That argues it could keep indexing.

**It does not.** Strand gets the same treatment: `Strand[index]` is removed and
`Strand.length()` becomes `Strand.rune_length()`.

A bounded cost is still an unstated one, and the alternative is worse than the
saving: `s[i]` valid on Strand and rejected on String is precisely the arbitrary
inconsistency this part exists to remove, and it would force every reader to
know which of two text types they hold before knowing whether indexing is
available. Uniformity across the two text types is worth more than random access
into at most 31 bytes.

`Strand.is_empty()` is retained for the same reason String's is — O(1) beside an
explicitly O(n) `rune_length`.

## Invariants

1. Collection representation, allocator passing, and bounds checking are
   unchanged.
2. Part A changes no existing program's behaviour except programs calling
   `List.set`, which is a mechanical rewrite to index assignment.
3. `Dict` iteration remains excluded; `length()` exposes no ordering.
4. Part B changes no runtime behaviour — only which operations are spellable and
   what they are named. `hex_string_at_rune` and `hex_string_rune_length` remain
   in the runtime for `rune_cursor` and `rune_length`.
5. Generated C for programs not using the removed or renamed operations is
   byte-identical; the snippet manifest moves only for affected snippets.

## Validation

- `l.set(0, 5)` is rejected; `l[0] = 5` is accepted and generates the C that
  `set` generated before.
- `d.length()` returns the entry count, is O(1), and matches insert/remove
  history across a sequence of operations.
- `d.find(k)` returns `Nil` for a missing key and requires narrowing before use;
  a program that uses the result without narrowing is rejected. The generated
  module performs one `hex_dict_find_*` call per source `find`, while the
  component helper performs one probe.
- `d.get(k)` and `d.remove(k)` still trap on a missing key — unchanged.
- Part B: `s[0]` and `t[0]` are rejected. `s.length()`, `s.bytes()`,
  `s.rune_cursor()`, `s.is_empty()`, and `s.slice(a, b)` remain available;
  `t.length()`, `t.is_empty()`, and `t.to_string(h)` remain available.
- Every affected catalog snippet is updated, and the manifest moves for exactly
  those. Source-level survey of the current 129-snippet catalog confirms:

  | Change | Snippets |
  |---|---|
  | text indexing removed | none remain; zero String or Strand index expressions |
  | `length()` → `rune_length()` | withdrawn by RFC 0087; zero `rune_length` uses |
  | `List.set` removed | none — no snippet calls it |

  `List.set` has zero catalog users, and text `length()` remains the current
  surface after RFC 0087.

### Do not enumerate by grepping generated C

The survey above is source-level for a reason. Searching generated C for
`hex_string_at_rune` or `hex_string_rune_length` reports **41 snippets**, and
searching for the Strand equivalents reports 29 — but the two probes return
*identical* snippet lists within each pair, which is the tell. The generator
emits helper families wholesale (`docs/status.md`, known coverage gaps), so a
helper's presence in the output proves only that a String appeared somewhere,
not that it was indexed.

Any enumeration for this RFC must come from the checker, which knows receiver
types, or from source inspection. Generated-C grep over-reports by roughly an
order of magnitude here.

## Non-goals

- Dict iteration, ordering, or key enumeration — deliberately excluded, and
  `length()` does not reopen it.
- Dict equality, or a source-visible hash operation.
- Changing Dict's key restriction (`Int32` or `Strand`).
- Grapheme-cluster support, normalization, or any Unicode operation beyond
  scalar iteration.
- Making `String` mutable, or adding a builder type.
- Revisiting RFC 0063's removals, which remain correct.

## Drawbacks

- Part A's `find` adds a third lookup-shaped operation beside `get` and
  `contains`. The justification is that they answer different questions — "give
  me the value, I know it exists", "does it exist", "give me the value if it
  exists" — and only the third is currently unexpressible in one lookup.
- Part B is a breaking change to the most-used type in the language, and it
  removes a convenience that reads naturally even when it is the wrong tool.
  That readability is exactly what makes it a trap.
