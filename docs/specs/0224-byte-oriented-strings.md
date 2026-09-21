# RFC 0224: Byte-Oriented Strings and Inline Capacity

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Thirteen decisions are settled and
  all six blocking issues are resolved. No open questions remain; a list of
  unspecified contracts remains before an implementation spec can be written
- Created: 2026-09-20
- Updated: 2026-09-21
- History: this RFC began as a proposal to replace both `Strand` and
  `Array<T, N>` under one storage pattern. The sequence half became RFC 0226.
  The text half then grew to absorb the `Rune` removal, because removing Rune
  is what makes the remaining text model simple enough to state in one place
- Depends on: the current `String`, `Strand`, and `Rune` contracts in
  `docs/reference.md`
- Coordinates with: RFC 0227 (vendored utf8proc static library), which owns
  the dependency this RFC's removal is pending, and which supersedes the
  archived RFC 0147; RFC 0139 (String/Strand comparison), which this RFC
  largely subsumes; RFC 0161 (implemented; supersedes RFC 0153);
  RFC 0160 (memory-bug inventory); RFC 0165 (local alias diagnosis); RFC 0223
  (Dict correctness), concurrently reconciling the `Strand` hash this RFC
  deletes; and RFC 0226 (inline bounded sequences)
- Does not update `docs/reference.md`: implementation and reference migration
  require a later decision

## Summary

Three changes to Hexal's text model, which together reduce it to one idea:

```text
Before                              After
------                              -----
Strand        inline, 31 bytes      String<N>   inline, N bytes
String        heap, dynamic         String      heap, dynamic
Rune          Unicode scalar        (removed, pending RFC 0227)
RuneCursor    rune traversal        (removed, pending RFC 0227)
```

1. **`Strand` becomes `String<N>`** — the same inline text, with the arbitrary
   31-byte capacity replaced by an explicit parameter.
2. **`Rune` and `RuneCursor` are removed entirely**, pending a real Unicode
   backend. A code point is not a useful unit without normalization, case
   folding, grapheme clustering, and width — none of which Hexal has. Shipping
   half a Unicode model invites code that looks correct and is not.
3. **Text is bytes.** Both forms store a byte length and nothing else. Every
   operation is byte-oriented, O(1), and decodes nothing.

**A String is still validated UTF-8** — it is a byte sequence the compiler and
runtime guarantee is well-formed. What is gone is the ability to *interpret*
those bytes as code points, until RFC 0227 provides a backend that can do it
properly.

## Settled decisions

Decided 2026-09-21. Each is recorded with the consequence it carries.

### 1. `Rune` and `RuneCursor` are removed

Removed from the language:

| Surface | Replacement |
|---|---|
| `Rune` type (`uint32_t`, excludes surrogates) | none |
| `RuneCursor`, `has_next()`, `next()` | none |
| `String.rune_cursor()` | none |
| `String.from_runes(heap, Slice<Rune>)` | none |
| Bare-quote literals `'a'`, `'\u{41}'` | none; the syntax is reserved |
| `Rune` as match scrutinee, in `print`, in interpolation | none |
| Rune ordering, equality, `to<T>()` conversion | none |
| `Slice<Rune>` | none |
| `for r in text` yielding a Rune | `for b: Byte in text` — see 3 |

Explicitly **not** affected, each verified against the tree:

- **`Byte` is untouched.** Byte literals are `b'a'` with `\xHH` escapes — a
  different syntax from the bare-quote Rune literal, so removing one leaves
  the other intact and frees the bare-quote form entirely.
- **String literal escapes are untouched.** `"héllo \u{1F600}"` still
  compiles and still produces the same UTF-8 bytes: `string_escape` includes
  `unicode_escape` at the lexer level, independently of whether a `Rune`
  *type* exists.
- **Byte access is untouched**: `bytes()`, `slice()`, and iteration over
  bytes all keep working.

**Consequence:** character-oriented text processing is unavailable until
RFC 0227. Byte-oriented processing — parsing, protocols, paths, ASCII
handling — is unaffected. This is a deliberate capability removal on the
project's established pattern: remove what cannot be done correctly yet,
rather than ship an approximation.

**Contract for RFC 0227.** When a Unicode backend lands it must restore, at
minimum, a code-point type, forward traversal over a String's code points, and
construction from code points. It inherits a language where `length()` means
bytes and `for` binders may be typed, so it must add `rune_length()` and
`for c: Rune in text` beside the byte forms rather than redefining them. The
bare-quote literal syntax is reserved for it.

### 2. UTF-8 validation is retained

`String.from_bytes` still validates malformed UTF-8 and refuses it. A String
is well-formed UTF-8 at all times.

What changes is only *how* the refusal is reported: `from_bytes` returns
`| Error` rather than trapping, on both forms. See Surface. The guarantee
itself — that no String ever holds malformed bytes — is unchanged.

Validation is a byte-level property that needs no `Rune` type, so keeping it
costs nothing now and preserves the invariant RFC 0227 will rely on. Dropping
it would let malformed bytes accumulate in stored data and make its
reintroduction a change that rejects previously-accepted programs.

### 3. `for` binders may be typed, and must be where the element type is ambiguous

New grammar:

```ebnf
ForStatement = "for" ForBinders "in" Expression "do" Block "end" .
ForBinders   = ForBinder | ForBinder "," ForBinder
                 | ForBinder "," ForBinder "," ForBinder .
ForBinder    = identifier [ ":" TypeExpression ] .
```

The rule is general, and text is its first instance:

- Where the collection determines the element type, the annotation is
  optional and must agree if present. `for x in list` and
  `for x: Int32 in list` over a `List<Int32>` are both valid.
- Where the collection does **not** determine it, the annotation is
  **required**. A `String` is a sequence of bytes today and a sequence of code
  points once RFC 0227 lands, so the element type is a property of the
  iteration, not of the collection.

```hexal
for b: Byte in text do ... end     # valid
for b in text do ... end           # rejected: element type is ambiguous
```

This is why it is preferable to simply removing `for ... in text`. It keeps
byte iteration available without forcing `.bytes()` at every site, it states
the unit at the point of use, and it is **forward-compatible**: when RFC 0227
restores code points, `for c: Rune in text` is added beside
`for b: Byte in text` with no change to either spelling and no silent
reinterpretation of existing loops.

**Consequence:** a grammar change, and every existing `for r in text` becomes
a compile error naming the ambiguity. That is the desired outcome — those
loops were written expecting characters and must be revisited.

**Text is currently the only instance of the second case.** The rule is about
genuine inference failure, not about the element type being complicated. A
union element does not qualify, which is worth stating because it looks like
it should:

```hexal
let l: List<X | Y>
for a in l do ... end          # valid: a is X | Y
for a: X | Y in l do ... end   # valid: the annotation agrees
for a: X in l do ... end       # error: X is not the element type
```

`List<X | Y>` determines its element type exactly — it *is* `X | Y`. What
varies per element is which variant is held, which is a runtime property,
discriminated with a type-mode `match` where exhaustiveness is checked. An
annotation naming one member would have to mean either "skip the others",
which is hidden control flow, or "trap on the others", which turns a
data-shape condition into a crash. Neither is a type annotation, so neither
belongs to this rule.

### 4. `length()` means bytes

`String.length()` returns the byte count. It is O(1), read from the stored
length.

**Consequence and migration hazard, stated once:** `length()` counts Runes
today. Code calling it for a character count silently becomes a byte count,
and for non-ASCII text those differ. There is no diagnostic for this
specific change. It is mitigated only by the fact that every other
rune-dependent construct in the same program — the `for` loop, the cursor,
the literals — becomes a hard error, so a program doing character work will
fail to compile for other reasons and be audited. A program using `length()`
alone on non-ASCII text is the one case that changes meaning silently.

`byte_length()` is **not** introduced: with runes gone there is no second unit
to disambiguate from, and a synonym would violate "one obvious way". RFC 0227
adds `rune_length()` when a rune count becomes meaningful again.

### 5. Representation: byte length only, no terminator, no rune count

Both forms store a byte length and nothing else. Capacity is not stored for
the inline form because N is part of the type.

```c
/* Inline: 8 + N bytes. */
typedef struct {
    size_t byte_length;
    uint8_t data[N];
} hex_string_N;

/* Heap handle: 24 bytes, down from 32. */
typedef struct hex_string {
    const uint8_t *data;
    size_t byte_length;
    hex_string_storage_kind storage_kind;
} hex_string;
```

Today's `Strand` uses a NUL terminator purely as an internal sentinel — every
operation scans (`while (index < 32 && text.data[index] != 0)`) and it is
never handed to C as a `char *`, so nothing in interop depends on it.

What this buys:

- every read operation is O(1) at any N;
- **embedded NUL is accepted**, exactly as `String` accepts it today, so both
  forms validate content identically and conversion between them cannot fail
  on a NUL;
- the inline and heap forms become one family: identical stored facts,
  identical cost model, identical validation.

What it costs:

- **No "zero runtime metadata" claim.** A stored byte length is metadata.
- An inline value is `8 + N` bytes: `String<31>` is 40 after alignment where
  `Strand` is 32.

Removing the stored rune count also deletes work from every construction path:
`from_bytes` currently runs a decode pass purely to count runes before
allocating, `concat` sums both operands' counts, and `interpolate` threads a
running count through every segment. All of that disappears, so construction
gets cheaper. The heap handle shrinks from 32 bytes to 24, so every `String`
in every collection, struct, and union gets smaller.

### 6. Spelling: `String<N>`, angle brackets

```hexal
let short: String<32> = "Hello"
```

This matches `Array<T, N>`'s existing shape, needs no grammar change, and
introduces no parse ambiguity. A postfix `String[N]` form was considered and
withdrawn: `IndexSuffix = "[" Expression "]"` already claims postfix brackets,
so `String[32].from_bytes(x)` would read as *index `String` by 32*.

**Reference consequence:** unifying the name under `String` requires the
"representation follows ownership" rule's enumerations to be updated. It is
not an amendment to the rule itself — see Resolved blockers.

### 7. `Error` becomes entirely inline and allocation-free

```hexal
ErrorKind.Other(header: String<128>)
Error.header() -> String<128>
Error(kind: ErrorKind, message: String<256>) -> Error
```

`Error` is **not** generic over these capacities. They are fixed literals, so
`Error` stays one concrete type and no signature returning an `Error` acquires
a parameter.

**The point of this is ownership, not capacity.** `message` is a heap `String`
today, so an `Error` carrying a runtime-built message owns an allocation that
someone must free — on an error path, which is exactly where cleanup is most
often missed and hardest to test. Making it inline means:

- an `Error` owns nothing and needs no cleanup, ever;
- constructing one cannot allocate, so it cannot fail for lack of memory;
- `file` stays a `String` handle because it is a compiler-injected literal:
  static-backed, never freed, 8 bytes.

A whole class of leak disappears from error propagation.

**Consequence: `Error` grows about 6x**, from roughly 72 bytes to roughly 432:

| Field | Today | Proposed |
| --- | --- | --- |
| `ErrorKind` tag + header | 8 + 32 = 40 | 8 + (8 + 128) = 144 |
| `Error.file` | 8 (handle) | 8 (handle, unchanged) |
| `Error.line`, `Error.column` | 16 | 16 |
| `Error.message` | 8 (handle) | 8 + 256 = 264 |
| **Total** | **~72** | **~432** |

`Error` is returned by value throughout the runtime and standard library and
appears in most result unions, so every `T | Error` return, every `try`
propagation, and every error copy moves ~432 bytes instead of ~72. Stack
consumption in deep propagation chains rises by the same factor.

**Why 256 and not more.** `PATH_MAX` is 4096, so no inline capacity a value
type could reasonably carry makes a path-embedding message safe. Under
Settled decisions 8 an over-long message traps, which means callers must bound
unbounded data before it reaches a message regardless of the capacity —
extra capacity only delays the trap, it never prevents one. Given that, the
smaller figure is strictly better: 256 bytes holds any authored diagnostic
plus a bounded identifier (Hexal's own longest shipped messages are near 80
characters), at 60% of the 512-byte variant's cost.

**Worth knowing for context:** Zig makes errors payload-free — an error is a
`u16` tag and diagnostics travel separately — precisely so error unions stay
cheap, and Odin pairs an enum with multiple returns. Hexal's `Error` already
carried file, line, column, kind, and message before this RFC, so it is a
different design; but an error value returned by value at 432 bytes is well
outside what either neighbour does, and that is a deliberate trade of size
for the absence of allocation and cleanup.

### 8. Over-long text fails; it is never truncated

Bounding `message` at 256 bytes and `header` at 128 means over-long text has
to do something. It fails, at the earliest point that can detect it:

| Source | Behavior |
| --- | --- |
| Literal, or any statically-known length | **Compile error** |
| Computed at runtime | **Trap** |

#### The trap is confined to two constructors

`Error(kind, message)` and `ErrorKind.Other(header = ...)` are the **only**
sites in the language where text coerces into a bounded capacity implicitly
and a runtime overflow traps. Their text parameters accept any text form —
a literal, a `String`, or a `String<M>` of any capacity — and check the
capacity at compile time where the length is statically known, at runtime
otherwise.

Everywhere else the conversion table stands unchanged: `String` ->
`String<N>` is an **explicit checked conversion** through
`String<N>.from_bytes(...)`, which returns `| Error` and traps at nothing.
Passing over-long text to any other bounded parameter is a type error, not a
trap.

```hexal
fun log(tag: String<16>) do ... end

log(some_string)                                   # type error: explicit conversion required
log(try String<16>.from_bytes(some_string.bytes()))  # explicit; failure is handled

return Error(ErrorKind.InvalidInput(), some_string)  # coerces; traps if over 512
```

The exception is warranted by structure, not convenience: error construction
is the one operation whose failure has nowhere to go, because reporting it
would require constructing an `Error`. Every other bounded conversion has a
caller who can receive an `| Error`, so every other one does.

Confining it this way keeps Settled decisions 2's unified failure mode intact
— `from_bytes` still reports rather than traps, on both forms — and keeps the
blast radius to two named constructors that an implementation can enumerate
and a reader can memorize.

Truncation is rejected outright, in either form. RFC 0160 records the
compiler's philosophy as *"report locally, trap loudly, never fix silently"*,
and silently discarding the tail of a diagnostic is the "fix silently" case —
it destroys exactly the detail the message existed to carry, at the moment
someone is trying to understand a failure.

The compile-time half is not a new rule: `String<N>` literals are already
checked against N, so `Error(kind, "…")` with an over-long literal is the
existing literal rule reaching a new site.

**Compiler and standard-library messages must fit by construction.** Every
message the compiler or stdlib produces is authored text with a known bound,
so this is a discipline the implementation owes, and a testable one: no
built-in message may exceed the capacity, and the check belongs in the test
suite rather than in a runtime path.

**Accepted risk.** A runtime message is often interpolated from
caller-controlled data — a path, a URL, a record key — so an unusually long
input can trap a program while it is reporting an unrelated failure. That is
the deliberate cost of refusing to truncate. Callers who prefer degradation to
a trap can bound the text themselves before constructing the `Error`, which is
what "have the user decide" means here:

```hexal
# explicit, caller-chosen degradation; no trap
let bounded: String<256> | Error = String<256>.from_bytes(long.bytes())
let text: String<256> = match bounded is
    | String<256> then bounded
    | Error then "input too long to report"
end
return Error(ErrorKind.InvalidInput(), text)
```

The caller bounds the text, sees the failure, and chooses what to do with it.
The language refuses to make that choice silently.

### 9. Capacity widening is explicit and infallible

```hexal
let small: String<16> = "hi"
let large: String<64> = small.widen<64>()     # N <= M checked at compile time
```

`widen<M>()` is a compile error when `M < N`, and cannot fail at runtime: the
source bytes are already valid UTF-8 by invariant, and `N <= M` means they
fit. It therefore returns `String<M>`, not `String<M> | Error` — forcing `try`
on a provably-safe operation would be noise that trains readers to ignore the
Error.

There is **no implicit widening**. Two reasons:

- No value type in Hexal implicitly converts to a differently-laid-out value
  type. `Array<Int32, 3>` does not widen to `Array<Int32, 5>`, and structs do
  not widen. Every implicit conversion the language has either preserves bits
  exactly (pointer and slice weakening, union injection) or is a scalar
  numeric widening in registers.
- The cost would be invisible. `fun log(msg: String<512>)` called with a
  `String<16>` would silently copy 520 bytes at every call, which is the kind
  of hidden cost the WYSIWYG principle exists to prevent.

### 10. The capacity parameter

- **Compiler-owned only.** `String<N>` joins `Array<T, N>` as a
  compiler-owned type taking an integer literal. User generic parameters
  remain types only; opening integer parameters to user code is const
  generics, a separate feature with its own design space.
- **Literal only.** A named constant would require constant evaluation the
  language does not have; revisit if RFC 0117 lands.
- **Spelled as `PositiveDecimalLiteral`**, reusing `Array<T, N>`'s existing
  grammar rule verbatim, so digit separators (`String<1_024>`) work with no
  new lexing.
- **Maximum 4096.** An inline value lives on a stack whose default commit is
  8 KiB, so one page is the point past which an inline text value is the wrong
  tool. A larger capacity is a Type Error naming the maximum, rather than a C
  compiler diagnostic about an oversized struct.

### 11. Dict keys, and one shared text helper

**`String<N>` is key-eligible at every capacity; `String` is not.** This keeps
today's rule for today's reason rather than inventing a new one: a Dict stores
its keys in the table, so an inline key is self-contained and safe, while a
heap handle would leave the Dict pointing at bytes it does not own — freeing
such a key would silently corrupt the table. That hazard is unchanged by
unifying the name.

Cross-capacity lookup requires an explicit `widen<M>()`, consistent with
Settled decisions 9. A byte-based lookup accepting any text form was
considered and left out of this RFC; it is a convenience, not a correctness
need, and it wants its own signature.

**Codegen gets simpler.** `Strand`'s fixed-width byte comparison selected no
helper because there was exactly one 32-byte `Strand`. With stored lengths,
equality for *every* text form is "compare length, then compare that many
bytes" — identical to heap `String`. So one `hex_equal_text` and one
`hex_hash_text` over `(bytes, length)` serve `String` and every `String<N>`,
replacing the per-type helpers rather than multiplying them.

### 12. `to_string(heap)` is renamed to `copy(heap)`

```hexal
let owned: String = inline.copy(heap)     # String<N> -> String
let clone: String = other.copy(heap)      # String    -> String
```

One name for one operation — producing an owned heap `String` from existing
text — on both forms. `to_string` reads oddly on a receiver that is already a
`String`, and `String<N>.copy(heap)` wants the same name for the same act.

This is a cosmetic break with no functional motive, and it is the last
surviving piece of the broader API rename an earlier revision proposed; the
rest became moot when `length()` and `slice()` kept their names.

### 13. Sequencing: RFC 0223 lands first

RFC 0223's semantic decision — hash the logical payload, never the zero tail —
is exactly what this RFC adopts and generalizes. Landing it first means this
RFC inherits a correct hash rather than changing one mid-flight, and 0223's
other findings are independently true today regardless of `Strand`'s fate.

## Alignment with Hexal's goals

Aligns:

- **Goal 3 (small surface):** two protected types removed (`Rune`,
  `RuneCursor`), one literal form removed, and the text model reduced to
  "validated UTF-8 bytes with a length".
- **Goal 2 (one obvious way):** one length unit, one slice unit, one way to
  iterate text.
- **Goal 14 (readability):** `for b: Byte in text` states at the call site
  what a loop walks, where `for r in text` required knowing that String
  iteration means code points.
- explicit allocation stays visible; the inline form never allocates;
- no ownership, borrow, lifetime, or move machinery is introduced;
- the byte-only model matches Zig, where `[]const u8` is bytes and Unicode
  iteration is a library (`std.unicode.Utf8View`). It diverges from Odin,
  which keeps `rune` builtin.

### Where it does not align

- **A real capability is removed.** No program can iterate characters until
  RFC 0227. This is a deliberate regression, not an oversight.
- **One name for two representations** is new, though it does not contradict
  the reference's representation rule — see Resolved blockers.
- **Goal 15** moves both ways, and the `Error` direction is large. Inline text
  grows 8 bytes versus `Strand`, and moving `Error`'s header and message
  inline takes it from ~72 bytes to ~432. Against that, the heap handle
  shrinks 8 bytes, construction stops paying for rune counting, and `Error`
  stops allocating entirely — trading bytes on the stack for allocations and
  frees that no longer happen.
- **Capacity widening is explicit** (Settled decisions 9), so the language
  gains no implicit conversion with a runtime cost.
- **`length()` changing meaning silently** is the one migration hazard with no
  diagnostic (Settled decisions 4).

## Current behavior being reconciled

`Strand` is an immutable literal-only inline value: 32 bytes holding at most
**31** UTF-8 payload bytes, a NUL, then zero fill. It exposes only `length()`
and `to_string(heap)` (renamed to `copy(heap)` by Settled decisions 12).

`String` is immutable UTF-8 behind a heap-backed handle, storing both a byte
length and a rune length. Runtime Strings require one matching `free`;
literals are static-backed and cannot be freed.

`Rune` is a `uint32_t` Unicode scalar excluding surrogates, with literals,
ordering, equality, checked conversion, match support, and print support.
`String` iteration and `rune_cursor()` yield Runes.

## Surface

```text
String        heap allocated, variable size
String<N>     inline value storage, capacity N bytes
```

N is a positive compile-time integer literal counting payload bytes, bounded
and spelled per Settled decisions 10.

```text
String.length() -> Size                                       O(1), stored
String.bytes() -> Slice<Byte>                                 O(1)
String.slice(start: Integer, end: Integer) -> Slice<Byte>     O(1), byte bounds
String.copy(heap: Heap) -> String
String.concat(heap: Heap, other: Slice<Byte>) -> String | Error
String.from_bytes(heap: Heap, bytes: Slice<Byte>) -> String | Error
String.interpolate(heap: Heap, template: InterpolationTemplate) -> String | Error
String.free(heap: Heap) -> no value
```

Every operation is O(1) or an explicit allocation. Nothing decodes UTF-8.
`from_bytes` is the one validating boundary: bytes become text only there.

**Every producing operation returns `| Error`, on both forms.** Malformed
UTF-8 arriving from a file, socket, or FFI boundary is a data condition, not a
programmer mistake, and so is a capacity overflow on an inline destination.
Both are reported, not trapped.

This **changes shipped behavior**: heap `from_bytes` traps on malformed UTF-8
today. Unifying the name while leaving the two halves with different failure
modes for the same mistake would be worse than the migration, which is
mechanical — every call site gains a `try` or a match. The trap is removed,
not moved.

Two changes to shipped behavior are hiding in that list:

- **`slice` moves from Rune bounds to byte bounds.** Today
  `hex_string_slice` validates against `rune_length` then walks
  `hex_utf8_next` to find byte offsets, so `s.slice(i, i + 1)` in a loop is
  quadratic behind slice syntax — the exact cost `docs/reference.md` cites
  when it rejects indexing. Byte bounds make it O(1). For ASCII the results
  coincide; for anything else they do not, and call sites cannot be migrated
  mechanically.
- **A byte slice may split a UTF-8 sequence, and that is legal.** `slice`
  returns `Slice<Byte>` — bytes, not text. Feeding them back through
  `from_bytes` validates and fails there. Validity is checked at the
  bytes-to-text boundary, not at every slice.

### All text forms are mutually operable

`String` and every `String<N>` are one family. Any operation between them is
available, and the only thing a cross-form operation ever needs is a `Heap` —
and only when its *result* is heap-backed.

Equality and ordering are bytewise over the logical bytes, across every
combination, and need no `Heap` because they allocate nothing:

```hexal
let a: String<16> = "hi"
let b: String<64> = "hi"
let c: String = "hi"

a == b      # true
a == c      # true
a < c       # valid, bytewise
```

Capacity never participates: two values holding the same bytes are equal and
hash equally regardless of which form holds them. Bytewise ordering over UTF-8
is also code-point ordering, so it is correct rather than merely convenient —
but it is **not** collation, and canonically equivalent text with different
bytes compares unequal. That is the same guarantee `String` gives today.

This reverses the current rule that "String and Strand are not mutually
comparable", which existed because the two were separate types with separate
dispatch. Unifying the name removes that justification.

**The universal read-only text parameter is `Slice<Byte>`.** `bytes()` is O(1)
on both forms, so a function that only reads text takes bytes and accepts any
form without conversion, allocation, or a supertype:

```hexal
fun starts_with(text: Slice<Byte>, prefix: Slice<Byte>): Bool do ... end

starts_with(a.bytes(), c.bytes())     # String<16> and String, no heap
```

`Slice<Byte>` carries no UTF-8 guarantee, so anything converting bytes back to
text revalidates. That cost is real but small, and it is what keeps the family
interoperable without a new view type.

### Producing operations: heap by default, inline by explicit capacity

Two ways to produce text, and the spelling says which:

```hexal
# Heap result. Works on any receiver. Passing a Heap is the allocation signal.
let joined: String = a.concat(heap, b)
defer joined.free(heap)

# Inline result. No Heap, no allocation. Capacity is named at the call site.
let joined: String<48> | Error = String<48>.concat(a, b)
```

The inline form is a constructor on the capacity, not a method on the
receiver, because the destination capacity is the thing being chosen and it
belongs where the reader can see it. It returns `| Error` because the operands
may not fit.

Capacity is **never inferred from context**. A contextual form
(`let c: String<48> = a.concat(b)`) was considered and rejected: whether the
call allocates would depend on the binding's declared type, which is hidden
allocation and is on the language's explicit Keep-out list.

The full inline set:

```text
String<N>.from_bytes(bytes: Slice<Byte>) -> String<N> | Error
String<N>.concat(left: Slice<Byte>, right: Slice<Byte>) -> String<N> | Error
String<N>.interpolate(template: InterpolationTemplate) -> String<N> | Error
```

Because `Slice<Byte>` is the universal text parameter, `from_bytes` is also
the conversion operation — it subsumes `String` to inline, inline widening,
and inline narrowing in one signature:

```hexal
let s: String    = "hello"
let t: String<8> = "hi"

let u: String<32> | Error = String<32>.from_bytes(s.bytes())   # heap -> inline
let v: String<32> | Error = String<32>.from_bytes(t.bytes())   # widening
let w: String<4>  | Error = String<4>.from_bytes(s.bytes())    # narrowing; fails
```

`String<N>.copy(heap)` converts inline storage to heap-backed;
`String.copy(heap)` clones an existing heap String.

### Construction and conversion

```hexal
let short: String<32> = "Hello"                      # contextual literal
let fixed: String<32> | Error = String<32>.from_bytes(bytes)   # checked; reports
let large: String<64> = small.widen<64>()            # explicit, infallible
let owned: String = small.copy(heap)                 # explicit allocation
```

A literal exceeding N bytes or containing invalid UTF-8 is rejected at compile
time. Embedded NUL is accepted.

| Conversion | Rule |
|---|---|
| `String<N>` -> `String<M>`, N <= M | explicit `widen<M>()`; infallible, compile-checked |
| `String<N>` -> `String<M>`, N > M | reject unless explicitly checked |
| `String<N>` -> `String` | explicit `copy(heap)` |
| `String` -> `String<N>` | explicit checked conversion; fails on capacity, never on content |

Because both forms accept identical content, `String` -> `String<N>` fails
only when the text does not fit.

## Memory and C representation

- Inline text has no `free`; dynamic `String` retains explicit cleanup.
- Copies are shallow for the handle and by-value for inline values.
- RFC 0158 tracks physical heap allocations only; inline text produces none.
- Inline text does not make slices into its storage safe after reassignment or
  scope exit; existing provenance and escape rules remain.

No generated helper, constructor, or literal header computes or carries a rune
count. `hex_utf8_next` is retained **only** for `from_bytes` validation; all
traversal uses of it are deleted.

## Migration outline

| Current | Proposed |
|---|---|
| `Strand` | `String<31>` to preserve capacity, or another explicit capacity |
| `ErrorKind.Other(header: Strand)` | `ErrorKind.Other(header: String<128>)` |
| `Error(kind, message: String)` | `Error(kind, message: String<256>)`; no cleanup |
| `for r in text do` | `for b: Byte in text do` |
| `s.rune_cursor()` / `c.next()` | no replacement; RFC 0227 |
| `String.from_runes(h, runes)` | no replacement; RFC 0227 |
| `'a'` / `'\u{41}'` | `b'a'` where a byte was meant; otherwise no replacement |
| `s.length()` | `s.length()`, now a byte count |
| `s.slice(a, b)` | `s.slice(a, b)`, now byte bounds |

`Strand` maps to `String<31>`, not `String<32>`: today's `Strand` holds **31**
usable payload bytes (a 31-byte literal is accepted; 32 is rejected with
`Strand literal exceeds 31 UTF-8 bytes`). Capacity is preserved; size is not —
`String<31>` is 40 bytes after alignment where `Strand` is 32.

### Reference rules this invalidates

Beyond the `Strand` and `Rune` removals, these are stated as fact today and
become false:

- *"String stores the count in its heap header, set at every construction, so
  `length()` is a field read and `slice` validates its bounds without
  scanning."* — `length()` remains a field read, but of the byte count; the
  `slice` clause and the rune count both go.
- *"String and Strand `length()` counts Runes."*
- *"String slice uses Rune bounds and returns the corresponding zero-copy
  UTF-8 bytes."*
- *"Text iterates decoded Runes."*
- *"Neither is indexable: reaching the nth Rune of UTF-8 walks from the
  start..."* — the reasoning stands but its subject is gone.
- The protected-type list, the scalar table's `Rune` row, the interpolation
  and print type lists, the ordering rule, the match-scrutinee rule, the
  bitwise and `bit_cast` exclusion lists, and the "representation follows
  ownership" enumeration, which names `Strand` among inline value types.

The migration must additionally audit grammar, type interning, literal
contextual typing, Dict key eligibility and hashing, generated C helpers,
manifests, snippets, and tests. Four snippet categories use Rune
(`01-values-and-bindings`, `04-types-and-matching`, `05-numeric-and-operators`,
`08-text`) and must be rewritten or removed.

## Non-goals

- Ownership, borrowing, lifetimes, affine moves, or automatic cleanup.
- Any Unicode operation beyond UTF-8 well-formedness validation. Normalization,
  case folding, grapheme clustering, width, and collation are RFC 0227's.
- Implicit heap allocation during conversions, or implicit truncation.
- A third text representation or separate fixed-buffer abstraction.
- Any change to `Array<T, N>` or `List<T>`; RFC 0226 owns that.
- Changing `Byte`, byte literals, or string-literal escape syntax.

## Resolved blockers

Six issues gated this RFC. All are now settled; each is recorded here with
where its resolution lives, so the reasoning is not lost.

### The representation rule is not violated — it needs an enumeration update

An earlier revision recorded this as a conflict. On a closer reading it is
not. `docs/reference.md` states:

> **Representation follows ownership, not shape.** ... A new type derives its
> representation from this rule **rather than from resemblance to an existing
> one**.

That final clause anticipates precisely this case and says to derive from
ownership rather than from the name. Both types comply: `String` owns an
allocation and is a handle; `String<N>` owns nothing and is a value. The rule
never promised that a name predicts representation — it promised that
representation follows ownership, which still holds.

What is new is only that one base name spans both categories. So this is a
reference synchronization item, not an amendment:

- the handle list becomes "`String` (the unparameterized heap form)";
- the value list drops `Strand` and gains `String<N>`;
- one clarifying sentence is added: *a capacity parameter selects the
  representation; each form still derives its own from its own ownership, and
  the parameter's presence is what a reader checks.*

Listed with the other reference edits in Migration outline.

### The rest

| Blocker | Resolution |
| --- | --- |
| Implicit widening would be a new conversion class | Settled decisions 9 — widening is explicit and infallible (`widen<M>()`); there is no implicit form |
| `Error`'s size change needs confirming | Settled decisions 7 — capacities set to 256 message / 128 header, `Error` ~432 bytes, with the reasoning for the smaller figure recorded there |
| The capacity parameter is under-specified | Settled decisions 10 — compiler-owned only, literal only, `PositiveDecimalLiteral`, maximum 4096 |
| Dict keys and the equality-codegen shortcut | Settled decisions 11 — `String<N>` eligible at every capacity, `String` not; one shared `hex_equal_text`/`hex_hash_text` replaces the per-type helpers |
| Concurrent conflict with RFC 0223 | Settled decisions 12 — RFC 0223 lands first and this RFC inherits its hash semantics |

## Unspecified surface

- `print` and interpolation of `String<N>`.
- Contextual and generic behavior: union injection
  (`let x: String<16> | String<32> = "hello"` — which member?), argument
  passing, returns, object members, ADT payloads, collection insertion,
  inference.
- Typed `for` binders beyond text: whether the annotation is permitted on
  every collection (recommended, for symmetry) or only where required, and how
  it interacts with the two- and three-binder forms.
- C ABI: passing and returning by value, address-of, `Ptr` pointee,
  `size_of`/`align_of`, foreign declarations, imported C arrays.
- Provenance of `bytes()` into inline storage: a Slice into an inline value
  dangles when that value is reassigned or leaves scope.

## Validation sketch

A later implementation spec must replace this with an exhaustive Validation
section covering: `Rune`/`RuneCursor` removal and the diagnostics replacing
each removed form; bare-quote literals rejected and the syntax reserved; byte
literals and string escapes unaffected; typed `for` binders, including the
required-annotation diagnostic for text and agreement checking elsewhere;
`length()` and `slice()` byte semantics; literal capacity and UTF-8
diagnostics; embedded-NUL acceptance in both forms; widening and narrowing
conversions; computed construction; dynamic versus inline cleanup; equality
and ordering across capacities; Dict key eligibility and hashing; the `Error`
header migration and its size change; generated C and helper emission;
manifest impact; and the absence of new ownership, borrow, lifetime, or
automatic-cleanup rules.

It must additionally validate the over-long-text rule from Settled decisions
8 in every half:

- an over-long `Error` message literal, and an over-long
  `ErrorKind.Other` header literal, are each compile errors with exact
  diagnostics;
- a computed over-long message and header each trap with exact runtime
  messages;
- neither path truncates, at either capacity;
- the coercion is confined: passing an over-long `String` to any *other*
  bounded parameter is a **type error**, and `String<N>.from_bytes` still
  returns `| Error` rather than trapping, on both forms;
- **no compiler- or stdlib-authored message exceeds its capacity** — a test
  over the message inventory, not a runtime check, since those messages are
  authored text with known bounds.
