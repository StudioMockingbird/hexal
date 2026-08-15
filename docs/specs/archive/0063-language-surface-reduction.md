# RFC 0063: Language Surface Reduction

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-15
- Created: 2026-08-15
- Updated: 2026-08-15
- Scope: user-visible language surface — two builtin method removals and two
  documentation corrections
- Depends on: RFC 0057 (generated-artifact baseline)
- Coordinates with: RFC 0064 (which endorses every item here), RFC 0056
  (workbench snippet catalog), `AGENTS.md`, `docs/reference.md`,
  `docs/status.md`
- Supersedes: the `at` and Array/View/List `is_empty` contracts in
  `docs/reference.md`

## Summary

Remove **8 of the language's 101 builtin operations**, each a second spelling of
an existing operation at identical cost. Correct two documentation defects that
cost nothing to fix.

Every item here is independent of RFC 0064's open decisions, and RFC 0064's
"Retain from RFC 0063" list endorses all four.

This RFC is surface hygiene, not architectural simplification. RFC 0064 owns the
changes that delete whole compiler-owned concepts.

### Revision history

This RFC originally proposed 14 removals across 8 items. Six were withdrawn.

**Three were wrong.** Their replacements were assumed rather than checked
against the generated C:

| Original claim | Actual | Outcome |
|---|---|---|
| `is_empty()` ≡ `length() == 0` on all five receivers | True for Array/View/List; **String/Strand `length()` is O(n)** | narrowed to three receivers |
| `String.bytes()` ≡ `slice(0, length())` | `bytes()` is O(1); the replacement is **three O(n) passes** | withdrawn |
| `Channel.capacity()` is racy | Capacity is immutable and never stale | withdrawn |

RFC 0064 identified all three. The lesson is now invariant 8.

**Three were low return.** `Channel.length()`/`capacity()` removal and the
`Stdio` → `File` move were both contingent on RFC 0064 decisions that may delete
`Channel` and `File` outright, and `Stream.new()` is subsumed by RFC 0064's
removal of the whole `Stream` concept. All three were dropped rather than parked:
if RFC 0064 retains those types, they can be reproposed against a settled
surface.

## Motivation

`AGENTS.md` states:

> There should only be one obvious way to do things.

A measured inventory of `docs/reference.md` finds 101 operations across 19
receivers. Eight offer a second spelling at no advantage:

| Redundancy | Count | Replacement | Cost of replacement |
|---|---|---|---|
| `at(index)` on Array, View, List, String, Strand | 5 | `[index]` | identical |
| `is_empty()` on Array, View, List | 3 | `length() == 0` | identical, O(1) |

`Array<T,N>.is_empty()` is worse than redundant: `N` is a positive decimal
literal (`compiler/checker/arrays.go:14`), so the method can only ever return
`false`.

## Verified premises

Both load-bearing claims were checked against the compiler rather than assumed.

**1. `[index]` is valid in value position, not only as an assignment target.**
`checkExpression` handles `parser.IndexExpression` in the same case as
`VariableExpression` and `PropertyExpression`
(`compiler/checker/expressions.go:266`); it routes through `checkPlace` and the
resulting place decays to a value. `x: Int32 = a[0]` is already legal, so `at`
adds nothing. If this ceases to be true, Item 1 is invalid and must be withdrawn.

**2. `length()` is O(1) for Array, View, and List, and O(n) for String and
Strand.** Array length is a compile-time constant; View and List render
`((receiver)->length == 0)` against a stored field. String and Strand count
Runes by scanning UTF-8 (`compiler/generator/strings.go:230`), while
`hex_string_is_empty` tests `byte_length == 0` in constant time (`:237`). Item 2
is therefore valid for the first three receivers and invalid for the last two.

## Required invariants

1. No program expressible before this RFC becomes inexpressible after it.
2. Diagnostics for the removed names are actionable: each names its replacement.
3. Removal is complete — no deprecated alias, no compatibility shim, no
   grace-period acceptance of the old spelling.
4. Trap behavior, bounds checking, evaluation order, and aliasing rules are
   unchanged for every retained operation.
5. Generated C for every rewritten program is semantically equivalent; the RFC
   0057 manifest is regenerated deliberately, not incidentally.
6. `docs/reference.md` is updated in the same change that lands the behavior.
7. No third-party dependency is added.
8. **No replacement may have a worse complexity class than the operation it
   replaces.** Verify against the generated C, not against the reference's
   prose. This invariant exists because its absence produced three defects in
   the first draft of this RFC.

## Item 1 — Remove `at(index)`

Remove from Array, View, List, String, and Strand.

```text
Array<T,N>.at(index: Integer) -> T        →  Array<T,N>[index]
View<T>.at(index: Integer) -> T           →  View<T>[index]
List<T>.at(index: Integer) -> T           →  List<T>[index]
String.at(index: Integer) -> Rune         →  String[index]
Strand.at(index: Integer) -> Rune         →  Strand[index]
```

For String and Strand the two forms are already identical — both yield `Rune`,
with no place/value distinction to justify a second name, and both lower to the
same rune-scanning helper. For Array, View, and List, `[index]` yields a place
whose read is exactly what `at` returned.

Bounds checking, index normalization, and trap behavior belong to `[index]` and
do not change. Complexity is unchanged on every receiver, satisfying invariant 8.

### Migration

7 snippet sites, 17 integration-test sites.

### Diagnostic

> `at` was removed; use `receiver[index]`

## Item 2 — Remove `is_empty()` from Array, View, and List

Remove from **Array, View, and List only**. The replacement is `length() == 0`,
which is O(1) on all three: a compile-time constant for Array, a stored field
for View and List.

`Array<T,N>.is_empty()` warrants no replacement expression at all; a call site
can be deleted and its result replaced with `false`.

RFC 0064 explicitly endorses removing Array's. View and List follow the same
argument and the same evidence — `length()` is a stored-field read for both, so
the replacement is O(1) and invariant 8 holds.

### Retained

`String.is_empty()` and `Strand.is_empty()` **are retained.** They test
`byte_length == 0` in constant time, while `length()` counts Runes by scanning
UTF-8. Removing them would convert an O(1) check into an O(n) scan at every text
call site, violating invariant 8.

This asymmetry is not a wart to be tidied away. It is the correct API: emptiness
is cheap to answer and length is not, so the two must not be collapsed merely
because they look related.

### Migration

Only sites whose receiver is an Array, View, or List. Text sites are untouched.

### Diagnostic

> `is_empty` was removed for Array, View, and List; use `receiver.length() == 0`

The diagnostic must not fire for String or Strand receivers.

## Item 3 — Give `Byte` a canonical-spelling rule

Documentation only; no code change.

`Byte` is a transparent alias of `UInt8`. The environment binds them to the same
value — `compiler/types/types.go:1214` is literally `"Byte": UInt8` — so
`View<Byte>` and `View<UInt8>` are one type and both spellings compile
everywhere.

`docs/reference.md` states the aliasing three separate ways but never says which
spelling to write. That is the only unlabeled coin-flip in the type table.

**Do not delete `Byte`.** Deletion is cheap in the compiler (two sites) but
touches 14 reference lines, 14 snippet sites, and 23 test sites, and it makes
`View<UInt8>` the spelling for a byte view — less readable, not more.

Add one rule to `docs/reference.md`:

> `Byte` is the canonical spelling wherever the value is raw storage rather than
> a number: `View<Byte>`, `Array<Byte, N>`, `List<Byte>`, and byte-oriented
> parameters and results. `UInt8` is canonical wherever the value is an 8-bit
> integer participating in arithmetic, comparison, or conversion. Both remain
> the same canonical type; this rule governs spelling, not semantics.

## Item 4 — Correct the `AGENTS.md` syntax warning

Documentation only; no code change. **This is the only urgent item in this RFC
and it should not wait on the other three.**

`AGENTS.md` currently reads:

> Several contain syntax the language never had — `:=` inference (RFCs 0016,
> 0017, 0029, 0036) and `if cond then` (RFCs 0029, 0037, 0043). Do not
> reintroduce either…

RFC 0061 shipped `if cond then`. It is in the grammar (`docs/reference.md:62`),
in the prose (`:591`), and enforced by the parser
(`compiler/parser/statements.go:320`, `"'then' after if condition"`).

The `:=` half remains correct. The `if cond then` half instructs every agent to
avoid syntax the language now **requires**. Split the sentence: keep the `:=`
warning, and replace the other half with a note that RFC 0061 introduced
mandatory block openers and that closed specs predating it show the old form.

## Evaluated and rejected

Recorded so they are not reproposed.

**`String.bytes()`, `String.is_empty()`, `Strand.is_empty()`** — removing any of
these replaces a constant-time operation with a UTF-8 scan. See Verified
premises and the revision history.

**`RuneCursor`** — `for value in text do` iterates runes and covers one of its
two catalog uses, but `digit_sum` in
`workbench/snippets/categories/08-text.json` passes its cursor into
`next_token(cursor)`, a function that pulls one rune and returns `Token | EoS`.
A `for` loop's iteration position cannot be handed to a callee, so no rewrite
exists.

**`Strand`** — `compiler/checker/dicts.go:171` documents that a String literal
in a Strand position retains Strand's literal-only construction, so
`d.get("name")` already works and callers are not juggling two text types.
Deleting Strand would relocate its literal-only machinery into String rather
than remove it. RFC 0064 Item 5 owns any later reassessment.

**`Atomic<T>`** — RFC 0064 Item 4 owns this and documents why Mutex is not an
equivalent replacement: Atomic is inline and allocator-free, maps directly to
C23 `_Atomic(T)` and foreign atomic layouts, and needs no lock object. An
earlier reading of this RFC's audit claimed Atomic adds no capability over
Mutex; that claim was wrong.

**Explicit `heap: Heap` threading** — 23 signatures, the largest ongoing
syntactic cost in the language, but a stated design pillar. Retrofitting
allocator passing later is far more expensive.

## Required order

1. **Items 3 and 4 may land immediately**, independently and in either order.
   Neither touches code. Item 4 is urgent.
2. Items 1 and 2 may land at any time; they depend on no open decision. Prefer
   sequencing them after RFC 0064's accepted removals so the RFC 0057 manifest
   is regenerated once rather than twice.
3. Regenerate the RFC 0057 manifest after Items 1 and 2 land.

## Validation

After each item:

- `go test ./compiler/checker`
- `go test ./compiler/tests/integration`
- `go test ./...`
- `go vet ./...`
- the snippet catalog compiles (`workbench/snippets.TestCatalogProgramsCompile`)

At completion:

- Every removed spelling produces a diagnostic naming its replacement, verified
  by one negative test per removed operation.
- `String.is_empty()`, `Strand.is_empty()`, and `String.bytes()` still exist and
  still lower to their constant-time helpers. This is a positive test, not an
  absence check.
- No retained operation's trap, bounds, ordering, or aliasing behavior changed.
- No retained operation's complexity class changed (invariant 8).
- The RFC 0057 manifest is regenerated and committed as a deliberate change.
- `go test -tags c23 ./...` and `go vet -tags c23 ./...` pass.
- Rebuild the workbench into `bin/` and restart it.
- Remove RFC 0063 from `docs/status.md` and mark it
  `Implemented; conformance verified YYYY-MM-DD`.

## Reference synchronization

Update `docs/reference.md` once, after implementation stabilizes:

- delete the five `at` signature lines and any prose naming them, including the
  Collections common rule that lists `at` among the operations sharing bounds;
- delete `is_empty()` from Array, View, and List; **retain** it for String and
  Strand;
- add the `Byte` canonical-spelling rule from Item 3;
- delete the duplicate `Stream<T>.new()` line — that signature currently appears
  twice, which is a defect in the normative document independent of whatever
  RFC 0064 decides about Stream;
- leave every retained contract unchanged.

## Non-goals

- Removing any type.
- Removing `String.is_empty()`, `Strand.is_empty()`, or `String.bytes()`.
- Removing `Stream`, `File`, `Channel`, or the scheduler — RFC 0064 owns those.
- Moving `Stdio` onto `File`. Dropped: RFC 0064 may remove `File` entirely, and
  the move is zero net operations.
- Removing `Channel.length()`/`capacity()`. Dropped: contingent on RFC 0064, and
  neither has any use in the catalog or test suite to reclaim.
- Adding a deprecation period, alias, or compatibility mode.
- Adding operations. This RFC only removes.
- Optimizing for builtin operation count or reference-document line count.

## Drawbacks

- **This is a breaking language change** for a language with no external users.
  The snippet catalog and integration suite must be rewritten in the same change.
- **The return is small.** Eight of 101 operations, roughly 120–200 lines of
  checker and generator. This is cleanup, not architecture.
- **The RFC 0057 manifest must be regenerated**, spending its value as a
  regression check for this specific change. The semantic integration tests
  carry that weight instead.

## Expected result

- 101 operations become 93.
- No receiver exposes two spellings of one operation at equal cost.
- Every operation whose replacement would be asymptotically worse is retained
  and covered by a positive test.
- `docs/reference.md` states which of `Byte` and `UInt8` to write, and no longer
  lists `Stream<T>.new()` twice.
- `AGENTS.md` no longer rejects mandatory syntax.
