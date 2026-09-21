# RFC 0224: Byte-Oriented Strings and Inline Capacity

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented. Nineteen decisions were settled and every Validation
  item passes. Settled decisions 13 gated implementation on RFC 0223 landing;
  the author directed implementation to proceed without it, so the Dict work
  here (text keys, one shared text helper) landed before 0223's reconciliation
  of the deleted `Strand` hash, which remains 0223's to close
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
- Updates `docs/reference.md` and `GRAMMAR.ebnf`, as the implementation
  requires

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

**Heap allocations keep one trailing zero byte, outside the length.**
`String.c_pointer()` hands C a `char *` and relies on it; nothing else does.
It is not part of the handle, is not counted by `byte_length`, and is not
stored in an inline value. Every heap-producing operation (`from_bytes`,
`concat`, `interpolate`, `copy`) writes it, and static literals carry it. An
embedded NUL still ends the string on the C side, exactly as today. `String<N>`
has no `c_pointer()`; see Settled decisions 17.

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

return Error(ErrorKind.InvalidInput(), some_string)  # coerces; traps if over 256
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

### 14. Printing and interpolation

`String<N>` prints and interpolates exactly as `String` does, at every
capacity. Direct text prints raw, nested text prints quoted and escaped, and
the capacity is never shown. One print path over `(bytes, length)` serves every
text form, for the same reason Settled decisions 11 gives for equality.

In interpolation, any text operand — a `String` or a `String<M>` of any `M` —
contributes only its bytes and gains no lifetime relation to the result. The
supported-operand list becomes "`String` and every `String<N>`" where it
named `String` and `Strand`.

`String<N>.interpolate(template)` follows the heap form's template rules: the
template must contain at least one interpolation and `{{ ... }}` is valid only
as that call's argument. It takes no `Heap`. **Every embedded expression
evaluates exactly once, left to right, before capacity is checked**, so an
overflow can never skip or repeat a side effect. Overflow returns `| Error`,
never a truncated value.

### 15. Typed `for` binders are permitted on every collection

The annotation from Settled decisions 3 is accepted on every source, not only
where it is required. A binder's annotation must be the exact canonical type of
what that binder receives — no conversion, and no weakening (`Slice<Byte>` does
not annotate a `Slice<mut Byte>` element). Aliases are transparent, so `Byte`
and `UInt8` are the same annotation.

| Source | Binder | Annotation |
|---|---|---|
| Array, Slice, List | value | optional; must be `T` |
| Dict | key, value | optional; must be `K`, `V` |
| Text (`String`, `String<N>`) | value | **required**; must be `Byte` |
| Any source with an index binder | index | optional; must be `Size` |

- The annotation attaches to the binder it follows and binders are annotated
  independently, so the two- and three-binder forms need no new rule:
  `for i, b: Byte in text` is valid, and `i` stays `Size` without one.
- The requirement applies to exactly the binder whose type the source does not
  determine. Over text that is the value binder; the index binder is always
  `Size` and is never required to be annotated.
- A missing required annotation names the binder, the source type, and that its
  element type must be annotated. A disagreeing annotation names both types.
  `for c: Rune in text` is a Name Error until RFC 0227 restores the type.
- Binders remain fresh immutable copies. An annotation carries no `mut`.
- **Text iteration reads a snapshot.** A heap `String` copies its handle. A
  `String<N>` is materialized into one inline copy before the loop, the rule a
  temporary `Strand` already follows, so reassigning a `mut` binding inside the
  body cannot change what the loop reads or leave the captured length stale.
  The cost is one copy of the value.

### 16. Positions, union injection, and generics

`String<N>` is a complete, finite, copyable value and is valid in every
position under Position eligibility. Assignment, arguments, returns, object
members, ADT payloads, union members, collection elements, Dict values, Task
arguments and results, Channel elements, and Heap, Stash, and Pool allocations
copy the inline region. Nothing needs cleanup: freeing a `List<String<N>>`
releases only the list's storage.

**Every position requires the exact type.** The only conversions into a
`String<N>` destination are identity and a contextual string literal, which is
checked against N at compile time. There is no widening, narrowing, or
`String` conversion in any position; a `String<16>` passed to a `String<32>`
parameter is a type error that names `widen<32>()`.

**Union injection.** A `String<N>` value injects only into a union that holds
`String<N>` itself. There is no smallest-fitting-member search, so a
`String<16>` does not inject into `String<32> | Nil`. A union may hold several
capacities (`String<16> | String<32>`): they are distinct canonical members,
and `is` and type-mode `match` select exactly one.

A contextual literal follows the existing rule that written order chooses among
contextual candidates, unchanged: the first written member that accepts it wins.

```hexal
let a: String<16> | String<32> = "hello"   # String<16>
let b: String<32> | String<16> = "hello"   # String<32>
let c: String<4> | String<32> = "hello"    # String<32>: 5 bytes skip the first member
```

The rule is recorded because text now gives it a visible representation
difference: with `String | String<16>`, written order also decides whether a
literal becomes a static heap handle or an inline value.

**Generics and inference.**

- User generic parameters stay types only (Settled decisions 10), so no
  function is generic over a capacity. `T = String<16>` is a valid
  substitution, and `List<String<16>>` and `List<String<32>>` are different
  types. Code that must accept any capacity takes `Slice<Byte>`.
- Capacity is never inferred: not from a literal's length, and not from a
  binding's declared type. `let s = "hi"` remains rejected as a bare contextual
  literal. `let s = String<16>.from_bytes(b)` is valid because the call names
  its capacity, and has type `String<16> | Error`.
- `String<1_024>` and `String<1024>` are one canonical type, identical across
  modules, like `Array<T, N>`.

### 17. C ABI and layout

- **Generated form.** One C struct per canonical capacity, emitted once per
  compilation and shared by every module, laid out as in Settled decisions 5.
  It passes, returns, and copies by value like `Array` does. A copy copies the
  whole struct.
- **The tail is unspecified.** Bytes past `byte_length` have no defined value
  and no operation reads them — equality, hashing, printing, and `bytes()` all
  work from `(bytes, length)`. An implementation may leave them uninitialized.
- **Pointee.** `String<N>` is a valid `Ptr` pointee, and `@place` yields
  `Ptr<String<N>>` or `Ptr<mut String<N>>`. The reference's exclusion of
  `String` as a pointee is scoped to the unparameterized heap form: it owns an
  allocation and carries its own aliasing rules, where `String<N>` owns nothing
  and is a plain value like `Array`. `offset` and indexing are permitted, since
  only Array, Slice, and List pointees are rejected there.
- **`size_of` and `align_of`.** `size_of<String<N>>()` reports the C `sizeof`
  of the generated struct and `align_of<String<N>>()` its alignment, both
  target-resolved Size constants. Both need one explicit literal N. The size is
  at least `8 + N` and is rounded to alignment: `String<31>` is 40 on a 64-bit
  target. `size_of<String>()` is unchanged, since it reports the handle.
- **Foreign declarations.** `String<N>` has no stable foreign ABI. As a
  foreign parameter, result, global, or foreign record field it reports the
  existing `<type> has no supported C ABI mapping for target <target>`.
  `Ptr<String<N>>` crosses like any other pointer, as an address whose layout C
  must treat as opaque. Text crosses to C as bytes and a length through
  `bytes()` and `Slice<Byte>.pointer()`, which is unsafe and not
  NUL-terminated.
- **`c_pointer()`.** `String.c_pointer()` is unchanged, backed by the trailing
  zero from Settled decisions 5. `String<N>` has no `c_pointer()`: it reports
  the generic `String<N> has no method c_pointer`, which is what `Strand`
  reports today. `checker/strings.go` also carries a `Strand` branch worded
  "copy it to a String first" that no receiver can reach — `Strand` dispatches
  to its own method checker — and it is deleted, not carried over.
- **Imported C arrays.** Nothing in the current import surface maps a C
  character array, and none may map to `String<N>`: a `char[N]` carries neither
  a length nor a validity guarantee. If one is added, its type is
  `Array<Byte, N>`, and it becomes text only through `String<M>.from_bytes`.

### 18. Provenance of `bytes()` and `slice()` into inline storage

`bytes()` and `slice(start, end)` on a `String<N>` require a source place,
exactly as `Array.slice` does. A temporary receiver is rejected at compile time
because there is no place to point into, so a call result or `try` result must
be bound before its bytes can be taken:

```hexal
let s = try String<16>.from_bytes(input)   # bind first
let b = s.bytes()                          # valid

f().bytes()                                # rejected: no source place
```

The result is a read-only `Slice<Byte>`. There is no `Slice<mut Byte>` over
text. It addresses the inline storage, so it dangles when the binding is
reassigned or leaves scope. That is the existing Slice contract — the backing
store is the programmer's responsibility — and this RFC adds no tracking.
Whatever diagnosis RFC 0160 and RFC 0165 give a Slice outliving a local `Array`
applies to `String<N>` identically. A slice of a heap `String` is unchanged.

### 19. Failure kinds and messages

Two conditions can fail, and each has one kind and one fixed message, the same
on the heap and inline forms:

| Condition | `ErrorKind` | `Error.message` |
|---|---|---|
| Result is not well-formed UTF-8 | `InvalidInput` | `invalid UTF-8 in string` |
| Result does not fit the inline capacity | `ResourceExhausted` | `string exceeds capacity` |

- **Distinct kinds, because callers branch on kind.** Classification compares
  `error.kind`, never message text. A caller that wants to degrade on a length
  problem but reject bad data needs the two to differ, as in Settled decisions
  8's bounded-conversion example.
- **No new kind.** `InvalidInput` already covers local contract failures on
  caller-supplied data. `ResourceExhausted` covers a bounded resource running
  out, which a fixed capacity is. Adding a kind would grow the language surface
  for a condition these describe correctly.
- **Messages are fixed, authored, and allocation-free.** They embed no length,
  offset, or capacity, following every other built-in producer. Both are
  23 bytes, so they fit `String<256>` by construction, and they belong in the
  message-inventory test from Settled decisions 8.
- **Capacity is checked first, then content.** A value that is both too long
  and malformed reports `string exceeds capacity`, so the outcome does not
  depend on validation order.
- **Validation covers the result.** `String<N>.concat(left, right)` takes two
  byte slices, so a multi-byte sequence split across them is valid once joined
  and is accepted. Heap `concat` appends to text that is already valid, so it
  validates only what it appends.
- The old trap `[Runtime Error] invalid UTF-8 in string` is removed with the
  behavior it belonged to; the Error message reuses its wording.
- A failure allocates nothing and produces no partial value. Heap allocation
  failure is unchanged and still traps with `heap allocation failed`.

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
String.interpolate(heap: Heap, template: InterpolationTemplate) -> String
String.free(heap: Heap) -> no value
String.c_pointer() -> Ptr<Byte>                               unsafe; unchanged
```

`String<N>` has the read operations `length`, `bytes`, `slice`, and `copy`, and
no `free` or `c_pointer`. Every operation is O(1) or an explicit allocation. Nothing decodes UTF-8.
`from_bytes` and `concat` are the validating boundaries: bytes become text only
there, and the check covers the result, not each operand.

**Every operation that can fail returns `| Error`, on both forms.** Malformed
UTF-8 arriving from a file, socket, or FFI boundary is a data condition, not a
programmer mistake, and so is a capacity overflow on an inline destination.
Both are reported, not trapped. Those are `from_bytes` and `concat` on both
forms and `interpolate` on the inline form. Heap `interpolate` and `copy` have
no failure to report — their operands are already valid text, and heap
allocation failure traps as it does everywhere — so they return a plain
`String`. Kinds and messages are Settled decisions 19.

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
| `Dict<Strand, V>` | `Dict<String<128>, V>` for every in-tree use (tests, fixtures, snippets); other capacities are valid for new code |
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
- *"Compiler-typed `self` and `for` binders are the remaining exceptions"* to
  written types, which now admit a written `for` binder type (Settled
  decisions 15).
- *"String, List, Dict, and Slice cannot be `Ptr` pointees"*, which becomes
  the unparameterized `String` only (Settled decisions 17).
- The `for ... in` source table, whose text rows list `String, Strand`, and its
  "temporary Arrays and Strands materialize once" sentence.
- The `Strand` "no `c_pointer`" and "dispatch separately" statements, which
  become `String<N>` statements.

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
| Concurrent conflict with RFC 0223 | Settled decisions 13 — RFC 0223 lands first and this RFC inherits its hash semantics |

## Implementation plan

Additive work comes first and removal last, so `Rune` and `Strand` keep
compiling until the phase that deletes them. Every phase ends with `gofmt -l`,
`go test ./...`, and `go vet ./...` clean; phases 1 to 6 also run the tagged
C23 suite (`go test -tags c23 ./compiler/tests/c23validation`). Tests are named
for the facet they protect and carry no RFC number. Fixture data is added to
`compiler/tests/c23validation/fixtures_test.go`.

**Phase 0 — Preconditions and baseline.** RFC 0223 has landed. On the
unmodified tree, record the full gate result, the snippet manifest, and the
measurements listed under Measurements. Nothing else in this RFC starts before
that.

**Phase 1 — `String<N>` as a type.**

- `compiler/types/collections.go`: add `StringCapacityType(n)` beside
  `ArrayType`, interned by canonical key `string:N`, displayed `String<N>`, C
  name `hex_string_<N>`, valid in every position except the Atomic and Unknown
  exclusions, `N` limited to 1 through 4096. Add `IsInlineString` and
  `IsText` (`String` or `String<N>`).
- `compiler/checker/type_resolution.go`: resolve `String<N>` beside
  `Array<T, N>` with the capacity diagnostics in Validation 3.
- `compiler/checker/strings.go` and `operator_checking.go`: a contextual string
  literal in a `String<N>` position replaces `checkedStringLiteralValue`'s
  Strand branch; capacity and UTF-8 are checked at compile time, embedded NUL
  is accepted.
- `checker/strings.go`, `methods.go`: `String<N>` receives `length`, `bytes`,
  `slice`, `copy`, `widen<M>`, and the static `from_bytes`, `concat`, and
  `interpolate`, with the failure kinds and messages of Settled decisions 19.
  It receives no `free`, no `c_pointer`, and no other method.
- `checker/equality.go`, `operands.go`, `print.go`, `unions.go`, `foreign.go`:
  mixed-form equality and ordering, printing, union injection and literal
  selection by written order, `Ptr` pointee, and the foreign ABI rejection.
- `generator/string_component.go`, `packages/string.h`, `strings.go`: emit one
  struct per demanded capacity in `hexal/string.h`, ahead of every component
  that names it, once per compilation. `interpolation.go`, `print.go`,
  `unions.go`, `arrays.go`: lowering for the new type.
- `compiler/generator/equality.go`, `equality_component.go`, `packages/
  equality.h`: one `hex_equal_text` and one `hex_compare_text` over
  `(bytes, length)` serving every text form.

**Phase 2 — Typed `for` binders.** `compiler/parser/ast.go` and
`statements.go`: `ForStatement.Binders` becomes a list of `ForBinder{Name,
Type}`. `GRAMMAR.ebnf` and the EBNF at the head of `docs/reference.md` change
together, and `grammar_test.go` still verifies. `checker/control_flow.go`
checks each annotation against the source (Settled decisions 15); text iterates
bytes and requires the annotation; `String<N>` iteration copies once.
`generator/for.go` replaces the Rune-decoding text loop with a byte loop.
Every existing `for r in text` test and snippet migrates in this phase.

**Phase 3 — Byte semantics of `String`.**

- `packages/string.h` and `.c`: `hex_string` drops `rune_length` and becomes
  24 bytes. `length()` reads `byte_length`. `slice` uses byte bounds in O(1).
  UTF-8 validation returns a status instead of trapping, and is used only by
  `from_bytes` and `concat`. Heap producers write the trailing zero
  (Settled decisions 5); literal storage keeps its own.
- `checker/strings.go`, `generator/strings.go`: `from_bytes` and `concat` gain
  `| Error`, `concat` takes `Slice<Byte>`, heap `interpolate` and `copy` stay
  plain `String`. `to_string(heap)` becomes `copy(heap)`.
- `generator/string_component.go`: literal records drop `RuneLength` and the
  `utf8.RuneCountInString` call.
- Every call site of `from_bytes` and `concat` in tests, fixtures, and snippets
  gains a `try` or a match.

**Phase 4 — `Error` migration.**

- `compiler/types/error_kind.go`, `checker/errors.go`: `ErrorKind.Other`
  carries `header: String<128>`, `Error.message` is `String<256>`, and
  `header()` returns `String<128>`. `Error(kind, message)` accepts any text
  form, checked at compile time when the length is known and at run time
  otherwise (Settled decisions 8).
- `generator/packages/error.h`: `hex_t_ErrorKind.other_header` and
  `hex_t_Error.hex_m_message` become inline structs; `hex_error_kind_header`
  returns the inline header.
- Every runtime producer builds an inline message: `packages/file.c`, `io.c`,
  `network.c`, `process.c`, `signal.c`, and the generator sites in `io.go`,
  `time.go`, `corelib.go`, `concurrency.go`, `adt.go`, `emission.go`, and
  `print.go`. Each message is authored text; none is built from unbounded data.
- A message-inventory test asserts that no compiler- or stdlib-authored message
  exceeds its capacity.

**Phase 5 — Dict keys.**

- `compiler/types/collections.go`: `IsDictKey` accepts `Int32` and every
  `String<N>`; `DictType` interns `Dict<String<N>, V>` per capacity.
  `checker/dicts.go`: the key diagnostics in Validation 7 replace
  `dictionary key type must be Int32 or Strand`, and the key argument of
  `insert`, `get`, `find`, `contains`, and `remove` is checked against the
  exact key type.
- `generator/dict_component.go` and `packages/dict.h`: the `StrandKey` flag,
  the `memcmp` probe, and `hex_hash_Strand` are replaced by one
  `hex_hash_text` and `hex_equal_text` over `(bytes, length)`, so a
  `Dict<String<128>, V>` and a `Dict<String<16>, V>` share both helpers and
  differ only in the stored key struct. The tail past `byte_length` is never
  read.
- Every in-tree `Dict<Strand, V>` migrates to `Dict<String<128>, V>`:
  `integration/dict_test.go`, `integration/collections_test.go`,
  `generator/dict_component_test.go`, `generator/alloc_test.go`,
  `c23validation/fixtures_test.go`, and the `12-modules` snippet. A second
  capacity is exercised alongside it so the rule is shown to hold at more than
  one N.

**Phase 6 — Removal sweep.** Delete `Rune`, `RuneCursor`, `Strand`, the
bare-quote literal, `rune_cursor`, and `from_runes`, with the diagnostics of
Validation 1. Delete every item on the sweep list below and record each as
deleted or retained with a reason. Rewrite the four snippet categories that
use `Rune` and the others that use `Strand`, and delete
`integration/string_rune_length_test.go`, whose contract is replaced by the
byte-length cases in `string_test.go`.

**Phase 7 — Reference, status, snippets, and measurements.**

- `docs/reference.md`: every rule listed under "Reference rules this
  invalidates", plus the `for` source table, the representation-follows-
  ownership enumeration and its clarifying sentence, the position and
  interpolation and print lists, the `Error` and `ErrorKind` signatures, the
  Dict key rule, the `Ptr` pointee exclusion, and the layout-intrinsics note.
- `docs/status.md`: add no entry unless work remains with an owning spec.
- Rebuild the snippet manifest by the procedure in `AGENTS.md`, review which
  artifacts moved, and record the breakdown in the commit message.
- Rebuild the compiler and `bin/hexal`, run `hexal play`, and load every
  rewritten snippet in the workbench.
- Run the Measurements.

**Phase 8 — Closure.** Only when every Validation item passes: set `Status:`
to implemented with the date, and move this file to `docs/specs/archived/`.

### Sweep list

Code that exists only because `Strand` or the rune count exists, to be deleted
in Phase 6 or named as retained with a reason:

- `hex_strand`, `hex_strand_rune_length`, `hex_strand_byte_length`,
  `hex_strand_to_string`, the `NeedStrand` template flag, and the per-type
  `Strand` compare helper in `generator/equality.go`.
- `StrandKey`, the `memcmp` probe, and `hex_hash_Strand` in
  `generator/dict_component.go` and `packages/dict.h`.
- `hex_string.rune_length`, `stringLiteralModel.RuneLength`, and the
  `utf8.RuneCountInString` call in `string_component.go`.
- `hex_rune_cursor`, `hex_string_rune_cursor`, `hex_utf8_decode`,
  `hex_utf8_encode`, `hex_string_from_runes`, and every trap in `string.c` that
  fires during traversal rather than validation.
- `checkStrandMethodCall`, `checkRuneCursorMethodCall`, and the unreachable
  `Strand` branch of `c_pointer` in `checker/strings.go`.
- The `Strand literal exceeds 31 UTF-8 bytes` and `Strand literal cannot
  contain NUL` diagnostics, and `dictionary key type must be Int32 or Strand`.
- The `String.from_runes` mention in the `String has no such operation`
  diagnostic.
- `RuneLiteral` in `lexer.go` and `ast.go`, `scanQuotedBody`'s rune path,
  `IsRune`, `IsRuneCursor`, `StrandType`, and their protected-name and
  exclusion-list entries in `types.go`, `bitwise.go`, `conversions.go`,
  `adt.go`, and `operands.go`.
- Rune and Strand entries in `compiler/tests/c23validation/
  trap_inventory_test.go` and the fuzz corpus under
  `compiler/tests/fuzz/testdata`.

## Validation

This section is exhaustive. Diagnostics are written `[Class] message`. A test
asserts the message text exactly and the class, and asserts nothing else about
the diagnostic list.

### 1. Removals

- `let c: Rune = 'x'`, `Slice<Rune>`, and any other `Rune` in a type position
  report `[Type Error] unknown type Rune; Rune was removed: text is bytes, use
  Byte`.
- `RuneCursor` reports `unknown type RuneCursor; RuneCursor was removed with
  Rune`.
- `Strand` reports `unknown type Strand; use String<N> (String<31> keeps the
  former capacity)`.
- A bare-quote literal, including `'a'`, `'\u{41}'`, and an unterminated `'`,
  reports the lexer diagnostic `bare-quote literals are reserved; use b'a' for
  a byte or "a" for text`.
- `s.rune_cursor()` reports `String has no method rune_cursor`.
- `String.from_runes(h, x)` and any other unknown static operation report
  `String has no such operation; use String.from_bytes(heap, view) or
  String.interpolate(heap, template)`.
- Unaffected, each proven by a program that compiles and runs: byte literals
  `b'a'` and `\xHH`; string literals with `\u{1F600}` and `é` producing the same
  UTF-8 bytes; `bytes()`; and `slice()`.

### 2. Text is bytes

- `length()` on `String` and on `String<N>` returns the byte count: `"héllo"` is
  6, and `"\u{1F600}"` is 4.
- `slice(a, b)` uses byte bounds on both forms. `s.slice(0, 1)` on `"é"` is one
  byte, is legal, and re-validates as malformed if passed to `from_bytes`.
- `slice` out of range traps with the existing `string slice bounds out of
  range` on both forms, and is O(1): the generated C contains no loop.
- `hex_string` has no `rune_length` field, and no generated helper computes or
  carries a rune count.
- Embedded NUL is accepted by literals and by both `from_bytes` forms, and
  `"a\0b".length()` is 3 on both forms.

### 3. `String<N>` the type

- `String<16>`, `String<1_024>`, and `String<1024>` resolve; the last two are
  one canonical type, also across two modules.
- `String<0>`, `String<n>` for a name, `String<T>` for a generic parameter, and
  `String<3.5>` report `String capacity must be a positive integer literal`.
- `String<4097>` reports `String capacity 4097 exceeds the maximum of 4096`;
  `String<4096>` compiles.
- `String<1, 2>` reports `String takes at most one capacity argument`.
- `let s: String<5> = "hello"` compiles; `let s: String<4> = "hello"` reports
  `String<4> literal exceeds 4 UTF-8 bytes`; a multi-byte literal is measured
  in bytes, so `String<2> = "é"` compiles and `String<1> = "é"` does not.
- A literal with invalid UTF-8 keeps `string literal contains invalid UTF-8`.
- `let s = "hi"` remains rejected as a bare contextual literal.
- Types intern once: `List<String<16>>` and `List<String<32>>` are different
  types.

### 4. Conversions and producing operations

- `small.widen<64>()` on a `String<16>` returns `String<64>` with no `| Error`;
  `widen<8>()` on a `String<16>` reports `widen<M> requires M greater than or
  equal to N`; `widen<16>()` is accepted.
- `String<N>.from_bytes` on an exact fit, one byte short, and one byte over N;
  `String<N>.concat(left, right)` and `String<N>.interpolate(template)` on the
  same three.
- `String<N>.from_bytes(s.bytes())` for a heap `String`, a wider `String<M>`,
  and a narrower one; the narrowing fails only when the text does not fit.
- `String<N>.copy(heap)` and `String.copy(heap)` produce an owned `String`
  needing one `free`; inline text needs none and produces no allocation.
- `String.concat(heap, other: Slice<Byte>)` accepts an operand from any text
  form via `bytes()`.
- `to_string(heap)` reports `String has no method to_string`.

### 5. Failures

- The kind and message table of Settled decisions 19, on every failing
  operation on both forms: malformed UTF-8 is `InvalidInput` with `invalid UTF-8
  in string`; inline overflow is `ResourceExhausted` with `string exceeds
  capacity`.
- Input both too long and malformed reports the capacity failure.
- A multi-byte sequence split across the operands of `String<N>.concat` is
  accepted; heap `concat` of a malformed appended operand fails.
- No failure allocates or yields a partial value. Heap allocation failure still
  traps with `heap allocation failed`.
- The old trap `[Runtime Error] invalid UTF-8 in string` is unreachable from
  `from_bytes` and `concat`, and its entry in the trap inventory is removed.
- Heap `interpolate` and `copy` return a plain `String`: assigning either to a
  `String` binding compiles, and applying `try` to it is rejected.

### 6. Equality, ordering, and hashing

- `==`, `!=`, `<`, `<=`, `>`, `>=` across every pairing of `String`,
  `String<16>`, and `String<64>`, in both operand orders, for equal content,
  unequal content, a strict prefix, and an embedded NUL; results are bytewise.
- Operands each evaluate exactly once, left before right.
- Equal bytes in different capacities are equal and hash equally.
- Canonically equivalent but byte-different text compares unequal.
- Generated C: one `hex_equal_text` and one `hex_compare_text`, each emitted
  once per program, and no per-capacity or per-type text helper.

### 7. Dict keys

- `Dict<String<128>, V>` compiles for `V` of `Int32`, `String`, a struct, and
  `List<Int32>`, with `insert`, `get`, `find`, `contains`, `remove`, `length`,
  and `free`, each round-tripping.
- Key capacities other than 128 are valid: `Dict<String<16>, V>` and
  `Dict<String<128>, V>` in one program are two types with separate
  specializations.
- `Dict<String, V>` reports `dictionary key type String is not allowed: a Dict
  stores its keys, and String does not own its bytes; use String<N>`.
- `Dict<Bool, V>` and any other key type report `dictionary key type must be
  Int32 or String<N>`.
- A literal key of exactly 128 bytes is accepted; 129 bytes reports `String<128>
  literal exceeds 128 UTF-8 bytes`.
- A key built at run time by `String<128>.from_bytes` over 129 bytes yields
  `Error`, so no key is ever inserted.
- A `String<16>` key given to a `Dict<String<128>, V>` reports
  `dictionary key requires String<128>; got String<16>; use widen<128>()`, the
  same for `insert`, `get`, `find`, `contains`, and `remove`; `key.widen<128>()`
  is accepted.
- Keys `"a\0b"` and `"a\0c"` are distinct entries; `"a"` and `"a\0"` are
  distinct entries.
- A 128-byte non-ASCII key and its ASCII lookalike of equal length are distinct;
  the same bytes find the same entry.
- Overwrite, remove followed by re-insert, growth across a rehash with 128-byte
  keys, and 1000 distinct keys, each reading back correctly.
- Iteration `for k, v in dict` accepts `k: String<128>`, rejects
  `k: String<16>` with the agreement diagnostic of Validation 9, and iterates a
  version-checked traversal unchanged.
- Generated C: the key struct precedes the Dict struct in the header, the shared
  `hex_hash_text` and `hex_equal_text` are each emitted once regardless of
  capacity count, no `hex_hash_Strand` or `memcmp`-over-the-whole-array probe
  remains, and neither helper reads past `byte_length`.
- Two Dict specializations differing only in key capacity in two modules, one
  program, define each struct once.

### 8. Positions, unions, and generics

- `String<N>` compiles and copies by value in: binding, assignment, argument,
  return, object member, ADT payload, union member, `List` element, `Array`
  element, `Slice` element, Dict value, Task argument and result, Channel
  element, and `Heap`, `Stash`, and `Pool` allocation.
- `List<String<N>>.free` releases only list storage.
- `String<16>` passed to a `String<32>` parameter reports `f argument 1
  requires String<32>; got String<16>; use widen<32>()`; the same in return,
  member, and payload positions with each position's existing prefix. A `String`
  to `String<16>` reports the suffix `; use String<16>.from_bytes(...) for a
  checked conversion`, and `String<16>` to `String` reports `; use copy(heap)`.
- `let a: String<16> | String<32> = "hello"` is `String<16>`;
  `String<32> | String<16>` is `String<32>`; `String<4> | String<32>` with a
  5-byte literal is `String<32>`; no member fitting reports `no member of ...
  accepts this expression`.
- A `String<16>` value injects into `String<16> | Nil`, and is rejected for
  `String<32> | Nil`.
- `is String<16>` and a type-mode `match` distinguish `String<16>` from
  `String<32>` in one union.
- No integer generic parameter exists: `String<N>` in a user generic is
  reported by Validation 3. `T = String<16>` substitutes.
- `let s = String<16>.from_bytes(b)` compiles, typed `String<16> | Error`.

### 9. Typed `for` binders

- Annotation accepted and exact on `List`, `Array`, `Slice`, and `Dict` at one,
  two, and three binders, including `for i: Size, x: Int32 in list`.
- A wrong annotation reports `for binder x is annotated Int64, but List<Int32>
  yields Int32 there`; a wrong index annotation reports the same with `Size`.
- `Byte` and `UInt8` are the same annotation. `Slice<Byte>` does not annotate a
  `Slice<mut Byte>` element.
- `for b in text` reports `for binder b over String has an ambiguous element
  type; annotate it, for example for b: Byte in ...`, for `String` and for
  `String<N>`, and for the two-binder form when only the index is annotated.
- `for i, b: Byte in text` and `for b: Byte in text` compile and yield the bytes
  of `"héllo"` in order, six of them.
- `for c: Rune in text` reports Validation 1's `unknown type Rune`.
- The `List<X | Y>` cases of Settled decisions 3: the plain binder and the
  `X | Y` annotation compile; `for a: X in l` is rejected.
- Snapshot: reassigning a `mut String<N>` inside the loop body does not change
  the bytes read or the count.
- Binders stay immutable: assigning to an annotated binder is rejected exactly
  as for an unannotated one.
- The `ForBinder` grammar verifies under `grammar_test.go`.

### 10. Printing and interpolation

- `print` of `String<N>` at two capacities, direct and inside a struct, list,
  and Dict, matches `String` byte for byte, raw when direct and quoted and
  escaped when nested.
- A `String<M>` operand of any capacity interpolates into heap and inline
  templates, contributing only its bytes.
- `String<N>.interpolate` takes no `Heap`, requires at least one interpolation,
  and rejects `{{ }}` outside the call.
- On overflow, every embedded expression has evaluated exactly once, in order,
  and the result is `Error`; the side-effect order is identical on the fitting
  and the overflowing call.

### 11. `Error` and `ErrorKind`

- `ErrorKind.Other(header = "x")` and `Error(kind, "x")` compile with the
  inline types; `Error.header()` and `ErrorKind.header()` return `String<128>`;
  `Error.message` is `String<256>`.
- An `Error` value owns nothing: no `free`, and no allocation on a build-and-
  discard path, shown under the debug allocation counters.
- Compile-time: an over-long literal reports `Error message literal exceeds 256
  UTF-8 bytes` and `ErrorKind.Other header literal exceeds 128 UTF-8 bytes`;
  exactly 256 and 128 compile.
- Run time: a computed message over 256 bytes traps with `[Runtime Error] Error
  message exceeds 256 bytes`; a computed header over 128 traps with `[Runtime
  Error] ErrorKind.Other header exceeds 128 bytes`; neither truncates.
- Both constructors accept a `String`, a `String<M>` of any `M`, and a literal.
- The coercion is confined: an over-long `String` to any other bounded
  parameter is the type error of Validation 8, and `String<N>.from_bytes` still
  returns `| Error`.
- Equality of two `ErrorKind.Other` values compares header bytes; propagation
  through `try` preserves location.
- Message inventory: every message the compiler and the runtime components
  produce is at most 256 bytes, and every `Other` header at most 128.
- Every runtime component (`file`, `io`, `network`, `process`, `signal`, `time`)
  still returns the same kind and message text as before.

### 12. Layout, pointers, and the C ABI

- `size_of<String<31>>()` is 40 and `align_of` is 8 on `x86_64-linux-gnu`;
  `size_of<String>()` is unchanged; `size_of<String<N>>` with a non-literal N is
  rejected.
- `size_of<Error>()` is 432 on `x86_64-linux-gnu`.
- `Ptr<String<N>>` and `Ptr<mut String<N>>` are valid, `@place` yields them, and
  `^p = other` replaces the whole value. `Ptr<String>` is still rejected, and
  `offset` and indexing on `Ptr<String<N>>` compile.
- `String<N>` as a foreign parameter, result, global, and record field reports
  `String<N> has no supported C ABI mapping for target <target>`;
  `Ptr<String<N>>` crosses.
- `String<N>` has no `c_pointer`: `String<N> has no method c_pointer`.
  `String.c_pointer()` on heap text, on a literal, and on the result of
  `from_bytes`, `concat`, `interpolate`, and `copy` yields a NUL-terminated
  string in C, checked by `strlen` in a fixture.

### 13. Provenance

- `bytes()` and `slice()` on a `String<N>` binding, a member, and a `mut`
  binding compile and return a read-only `Slice<Byte>`; no `Slice<mut Byte>`
  exists over text.
- On a temporary receiver, such as `f().bytes()`, both report `a Slice cannot be
  rooted in a temporary String<N>`; binding first compiles.
- A slice read after reassigning its `mut` binding is the programmer's
  responsibility, and no new diagnostic exists.

### 14. Generated C

Each is asserted on the emitted text:

- `hex_string` has exactly `data`, `byte_length`, and `storage_kind`.
- One struct per demanded capacity, defined once per program, in
  `hexal/string.h` and before every header that names it; no struct for an
  undemanded capacity.
- A program using none of the text types emits no string component.
- No `hex_rune_cursor`, `hex_strand`, `rune_length`, `hex_utf8_decode`, or
  `hex_utf8_encode` appears in any output.
- Include order, linkage, and declaration-before-use hold in `error.h`, `dict.h`,
  and every module header naming a `String<N>`.
- No unused helper is emitted for an unused capacity.

### 15. C23 compile, link, and run

Fixtures in `fixtures_test.go`, each compiled under every resolved toolchain
with `-std=c23 -Wall -Wextra -Werror`, linked, run, and run under UBSan where
the gate supports it:

- `inline-string-runs`: construction, `length`, `bytes`, `slice`, `widen`,
  `copy`, equality, and ordering across capacities, with exact stdout.
- `inline-string-failures-run`: each failure of Validation 5, with the returned
  kind and message printed.
- `text-bytes-run`: `length` of non-ASCII text, byte `slice`, embedded NUL, and
  the trailing zero via `strlen`.
- `dict-string-key-runs`: the `Dict<String<128>, V>` cases of Validation 7,
  including growth, embedded NUL, and the 128-byte boundary, with exact stdout.
- `typed-for-runs`: byte iteration, the snapshot case, and `List`, `Dict`, and
  `Array` annotations.
- `error-inline-runs`: an `Error` built with a runtime-built message, its
  header, propagation, and no allocation.
- `error-message-overflow-traps`: the message trap, expecting `[Runtime Error]
  Error message exceeds 256 bytes`; and the header trap.
- `sizes-run`: prints `size_of` and `align_of` of `String<31>`, `String`, and
  `Error`.
- A program that puts `String<16>` and `String<128>` in two modules and compiles
  and links.
- The removed-trap and new-trap literals are entered in the trap inventory with
  a disposition.

### 16. Conformance, cleanup, and documentation

- No new ownership, borrow, lifetime, or automatic-cleanup rule exists;
  verified by the absence of any such checker code change.
- Programs that compiled before and use none of the removed forms keep their
  behavior, except `length()` and `slice()` on non-ASCII text (Settled decisions
  4) and the `| Error` results.
- The snippet manifest moves only for programs that use text, `Error`, or Dict
  keys; the artifact breakdown is reviewed and recorded, and the baseline is
  regenerated by the `AGENTS.md` procedure and never by hand.
- Every sweep-list entry is deleted, or retained with a stated reason.
- `docs/reference.md` states every changed rule and disagrees with neither the
  code nor this RFC; the reference EBNF matches `GRAMMAR.ebnf`.
- `docs/status.md` names no completed work.
- Ordinary tests remain pure Go and invoke no external tool.
- `.tmp/` is empty.

### Measurements

Recorded on `x86_64-linux-gnu`, before and after, and reported; none has a
pass threshold except where a size is stated.

- `sizeof(hex_string)` is 24, `sizeof(hex_string_31)` is 40, and
  `sizeof(hex_t_Error)` is 432, asserted by the `sizes-run` fixture.
- A build-and-discard of an `Error` with a runtime-built message performs zero
  heap allocations, against one before.
- Generated size and build-and-link time for the text, error, and Dict fixtures.
- The existing benchmark suite's text and Error cases, before and after.

### Test map

| Validation | Tests |
| --- | --- |
| 1 Removals | `integration/string_test.go`, `syntax_test.go`, `literals_test.go`; `lexer/lexer_test.go` |
| 2 Text is bytes | `integration/string_test.go`, `text_conformance_test.go` |
| 3 The type | `integration/inline_string_test.go`; `types/types_test.go` |
| 4 Conversions | `integration/inline_string_test.go`; c23 `inline-string-runs` |
| 5 Failures | `integration/inline_string_test.go`, `string_test.go`; c23 `inline-string-failures-run`, `trap_inventory_test.go` |
| 6 Equality | `integration/equality_test.go`, `inline_string_test.go`; `generator/string_component_test.go`; c23 `text-comparison-matrix-runs` |
| 7 Dict keys | `integration/dict_test.go`; `generator/dict_component_test.go`; c23 `dict-string-key-runs`, `inline-string-two-modules-runs` |
| 8 Positions | `integration/inline_string_test.go`; c23 `inline-string-runs` |
| 9 Typed `for` | `integration/for_test.go`; `grammar_test.go`; c23 `typed-for-runs` |
| 10 Print, interpolation | `integration/print_test.go`, `inline_string_test.go`; c23 `text-print-and-interpolation-runs` |
| 11 `Error` | `integration/error_test.go`; `generator/error_component_test.go`, `error_inventory_test.go`; c23 `error-inline-runs`, `error-message-overflow-traps`, `error-header-overflow-traps` |
| 12 Layout, ABI | `integration/inline_string_test.go`; c23 `sizes-run`, `text-bytes-run` |
| 13 Provenance | `integration/inline_string_test.go` |
| 14 Generated C | `generator/inline_string_test.go`, `string_component_test.go`, `dict_component_test.go`, `error_component_test.go` |
| 15 C23 | `c23validation/fixtures_test.go`, `trap_inventory_test.go` |
| 16 Conformance | `workbench/snippets` manifest test; review checklist |

