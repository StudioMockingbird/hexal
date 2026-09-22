# RFC 0227: Vendored utf8proc Static Library

- Kind: Feature Specification (Rust-Style RFC). This began as a dependency
  ADR; it now also specifies the Unicode language surface the dependency
  exists to serve, because vendoring a Unicode library to run a validator
  Hexal already implements correctly would be cost without benefit
- Status: Implemented. Every Validation item passes. Phases 0-7 landed: the
  archive is vendored and qualified in the one shipped pack, the validator is
  the utf8proc adapter, `Rune`, all three cursors, `Grapheme`, `UnicodeCategory`,
  `NormalizationForm`, and the Tier 3 `normalize`/`casefold` transforms are
  implemented and exercised end to end under the Clang gate, and
  `docs/reference.md` carries the whole surface. Two items are recorded as
  reachable-only dispositions in `docs/status.md`: the Tier 3 failure path
  cannot be driven from a checked program and, by `utf8proc_map`'s own
  contract, has nothing to release there; and the retired Windows pack is no
  longer a shipped pack, so the one shipped pack is the one that carries the
  archive
- Created: 2026-09-21
- Updated: 2026-09-22
- Origin: replaces the integration proposal in archived RFC 0147 with a
  target-pack-qualified dependency contract and an explicit utf8proc capability
  inventory
- Depends on: RFC 0052 (C backend), RFC 0055 (build and runtime-pack inputs),
  the current target-profile matrix, and the current String contract in
  `docs/reference.md`
- Coordinates with: RFC 0158 (allocation tracking), RFC 0213 and RFC 0214
  (target-qualified static runtime packs), and RFC 0224 (byte-oriented
  strings)
- **Sequencing note.** RFC 0224 removes the code-point type, its cursor, its
  literal syntax, and code-point iteration from the language, deliberately,
  pending this RFC. Everything Unicode-related here is therefore a surface it
  **restores or introduces**, not one it adapts. RFC 0224 lands first; this
  RFC reads its byte-oriented storage, typed `for` binders, bytewise
  comparison, and reserved bare-quote literal syntax as the baseline
- Terminology: *Unicode scalar* and *code point* both name a single Unicode
  value; the Hexal type for one is `Rune`, restored by this RFC under the name
  RFC 0224 reserved for it
- Does not update: `docs/reference.md`, Hexal syntax, or text representation

## Decision summary

Hexal will vendor one exact utf8proc release as a target-qualified static
library in each shipped runtime pack. The core compiler records a logical
`utf8proc` runtime dependency; the build driver materializes the matching
header, archive, and license files from the selected pack and links the
archive. The compiler never discovers, builds, or loads utf8proc.

The pinned release is utf8proc **2.11.3**, whose upstream release records
Unicode **17.0.0** support. The release, archive digest, build flags, target
identity, API version, Unicode-data version, and license material become part
of the pack identity.

This RFC restores `Rune`, introduces `Grapheme`, and specifies four tiers of
Unicode surface over them — see Language surface. Storage is unchanged: text
remains validated UTF-8 bytes with a stored byte length, exactly as RFC 0224
leaves it, and `Rune` and `Grapheme` are *views* over those bytes rather than
alternative representations.

Nothing becomes implicit. Normalization, case folding, and segmentation happen
only where a program asks for them, and text equality, ordering, and hashing
stay bytewise.

## Why this dependency

The current runtime owns UTF-8 validation, scalar decoding, and scalar
encoding formulas. Those formulas are small enough for scalar stepping, but
future Unicode behavior would otherwise require Hexal to maintain independent
tables and algorithms for:

- canonical and compatibility normalization;
- locale-independent case folding;
- extended grapheme-cluster boundaries;
- Unicode general categories and character properties;
- simple case conversion;
- terminal-oriented character width; and
- synchronization with new Unicode releases.

One pinned backend gives generated runtime components one Unicode data source.
It does not make the dependency part of the Hexal language and does not make
Unicode normalization implicit.

## Upstream qualification

The vendored source is the official JuliaStrings utf8proc release archive,
not an arbitrary system package or a moving repository checkout:

```text
upstream:       https://github.com/JuliaStrings/utf8proc
pinned release: v2.11.3
Unicode data:   17.0.0
static archive: utf8proc.a
API header:     utf8proc.h
software:       MIT/Expat license
data:           Unicode data license
```

The archive and header must be checked against the selected release before
they are checked in. The package's software and Unicode-data license texts are
shipped beside the archive. A later utf8proc or Unicode-data upgrade is a
runtime-pack identity change, even when the Hexal API remains unchanged.

## Runtime-pack layout

Every supported target pack that ships a Hexal runtime dependency contains a
versioned utf8proc entry:

```text
lib/<hexal-target>/
  manifest.json
  utf8proc_v2.11.3/
    include/utf8proc.h
    utf8proc.a
    LICENSE.md
```

The manifest schema is closed and already defined by `runtimeManifest` in
`internal/driver/runpack.go`. utf8proc adds **one entry to the existing
`dependencies` array**, in the same shape libuv and mimalloc use, and its file
hashes join the existing top-level `files` map:

```json
{
  "name": "utf8proc",
  "include_root": "utf8proc_v2.11.3/include",
  "archive": "utf8proc_v2.11.3/utf8proc.a",
  "system_libraries": [],
  "license_file": "utf8proc_v2.11.3/LICENSE.md"
}
```

`system_libraries` is empty: utf8proc is freestanding C with no link
dependencies beyond libc.

**No schema change.** `format_version` stays 1. The release version lives in
the directory name (`utf8proc_v2.11.3`), exactly as `mimalloc_v3.5.1` and
`libuv_v1.52.1` do, and the Unicode-data version is recorded in `lib/BUILD.md`
beside the source commit, compile command, size, and digest — the same place
every other build fact lives. Adding `version` or `unicode_version` fields to
the manifest would change a closed schema for information that is already
recorded and already covered by the file digests.

No host path, environment value, system-installed utf8proc, pkg-config result,
or network lookup participates in compilation.

The archive is built independently for each supported target profile. The
initial repository may qualify one target pack first, but the RFC is not
complete for a profile until that profile has matching header, archive, license,
ABI, and link evidence.

## Compiler and driver boundary

The core compiler remains string-in/string-out:

```go
func Compile(
    sources map[string]string,
    entrypoint string,
    project Project,
) CompilationResult
```

It may return a logical dependency fact:

```text
utf8proc
```

It must not read the vendor tree, compile C, inspect the host, resolve a
library, or include utf8proc source or Unicode tables in generated files.

The build driver owns:

- selecting the target-qualified pack;
- validating the pack manifest and demanded payload hashes;
- materializing the private include root and archive;
- ordering the archive in the link command;
- running native qualification probes; and
- reporting missing, mismatched, or corrupt dependency inputs.

Generated public module headers must not include `utf8proc.h` or expose a
`utf8proc_*` type. The private runtime component may include the header:

```c
/* private generated runtime source */
#define UTF8PROC_STATIC
#include <utf8proc.h>
```

The generated C runtime owns the Hexal adapter. Hexal code does not call
`utf8proc_*` directly.

## Dependency demand and linkage

The dependency is selected only when a generated runtime component uses the
utf8proc adapter or a separately specified Unicode component requests it.

```text
program with no runtime text component       no utf8proc dependency
literal-only program                         no new dependency solely for UTF-8 bytes
byte iteration or ByteCursor only            no utf8proc dependency
runtime text construction (validation)       utf8proc dependency
any Rune or Grapheme operation               utf8proc dependency
normalization or case folding                utf8proc dependency
```

`ByteCursor` and `for b: Byte in text` are index arithmetic over bytes and
select nothing, so a byte-oriented scanner over literal text stays
dependency-free.

Selection is program-wide and deterministic. A non-ASCII literal alone must
not broaden runtime-component demand. A future component that uses utf8proc
must not silently assume that the String component is present; its dependency
fact is explicit.

The static archive is linked as a private runtime input. No dynamic utf8proc
library is searched for or loaded at runtime.

## Initial Hexal integration

### What the language actually needs today

After RFC 0224 the runtime has exactly **one** live Unicode requirement:
proving that a byte sequence is well-formed UTF-8 when text is constructed
from bytes. Everything else that once used the hand-rolled UTF-8 formulas is
gone — there is no code-point type, no cursor, no code-point iteration, no
construction from code points, and no terminator to maintain.

So the initial integration is smaller than an earlier revision of this section
assumed:

| Runtime use | Before RFC 0224 | After RFC 0224 |
| --- | --- | --- |
| Validate bytes on construction | `hex_utf8_next` walk | **live — the only caller** |
| Step a cursor over code points | `hex_utf8_next` | removed |
| Count code points | `hex_utf8_next` walk | removed |
| Encode a code point to UTF-8 | `hex_utf8_encode` | removed |
| Decode a code point | `hex_utf8_decode` | removed |

That scoping observation is what shaped this RFC. Taking the dependency for
the validator alone would buy one function Hexal already implements
correctly — cost without benefit. So this RFC also specifies the Unicode
surface in Language surface above, and the dependency is taken when that
surface lands, not before. The validator substitution rides along as a
no-behavior-change cleanup, not as the justification.

### Validation

The adapter validates a whole byte range, reporting rather than trapping,
because RFC 0224 makes `from_bytes` return `| Error`:

```c
/* Returns true when [data, data+length) is well-formed UTF-8.
   Reports; never traps. The caller turns false into a Hexal Error. */
static bool hex_utf8_validate(const uint8_t *data, size_t length) {
    size_t index = 0;
    while (index < length) {
        size_t remaining = length - index;
        utf8proc_ssize_t available =
            (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
        utf8proc_int32_t codepoint = 0;
        utf8proc_ssize_t width =
            utf8proc_iterate(data + index, available, &codepoint);
        if (width <= 0) {
            return false;
        }
        index += (size_t)width;
    }
    return true;
}
```

`utf8proc_iterate` rejects overlong forms, surrogates, truncated sequences,
and scalars above U+10FFFF, which is exactly the set the hand-rolled validator
rejects today. Every call passes a non-negative length no greater than four;
an arbitrary `size_t` string length is never narrowed to `utf8proc_ssize_t`.

utf8proc's negative error codes and English error strings are not Hexal
diagnostics. The adapter reports a boolean; the caller owns the `Error`.

### Code-point stepping, for when the Unicode API returns

Not currently reachable from any Hexal operation. Recorded because it is the
primitive every future Unicode surface is built on:

```c
/* One code-point step. Since a String is already validated UTF-8, a
   well-formed input cannot fail here; width <= 0 is a runtime invariant
   violation, not user-data error. */
static size_t hex_utf8_next(
    const uint8_t *data,
    size_t length,
    size_t *index,
    uint32_t *codepoint_out
) {
    size_t remaining = length - *index;
    utf8proc_ssize_t available =
        (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
    utf8proc_int32_t codepoint = 0;
    utf8proc_ssize_t width = utf8proc_iterate(
        data + *index,
        available,
        &codepoint
    );
    if (width <= 0) {
        hex_runtime_trap("[Runtime Error] invalid UTF-8 in string\n");
    }
    *index += (size_t)width;
    *codepoint_out = (uint32_t)codepoint;
    return (size_t)width;
}
```

Note the split in failure handling, which follows from RFC 0224's validation
boundary: **validation reports, stepping traps.** Bytes entering a String are
caller data and can legitimately be malformed, so validation returns a
boolean the caller turns into an `Error`. Bytes already inside a String are
well-formed by invariant, so a failure while stepping means the invariant
broke — a runtime defect, which traps.

The adapter must prove `*index <= length` before subtracting, and every call
passes a non-negative length no greater than four.

### Code-point encoding, for when the Unicode API returns

Also not currently reachable: RFC 0224 removed construction from code points.
Recorded for the same reason.

```c
if (!utf8proc_codepoint_valid((utf8proc_int32_t)value)) {
    /* caller-supplied scalar: report, do not trap */
    return false;
}

utf8proc_ssize_t written = utf8proc_encode_char(
    (utf8proc_int32_t)value,
    output
);
```

`output` always has at least four writable bytes.
`utf8proc_encode_char` does not validate the scalar itself, so the validation
call is mandatory. A future construction API retains Hexal-owned checked size
arithmetic, one Heap allocation, and header initialization — but **not** a
trailing NUL, which RFC 0224 removed from both text representations.

### Existing representations remain unchanged

This dependency RFC changes no representation. Taking RFC 0224 as the baseline,
that means:

```text
String        heap-backed handle: data pointer, byte length, storage kind
String<N>     inline value: byte length plus N payload bytes
```

There is no code-point type, cursor, or terminator to preserve — RFC 0224
removed all three. This RFC does not change String ownership, `free`,
literals, interpolation, byte slices, bytewise equality/order, hashing, or
the reported-not-trapped malformed-input contract.

## Language surface

This RFC ships Unicode capability, not only a dependency. The surface is
organized around three element types over one storage.

### Three element types, one text

Text is bytes (RFC 0224). Unicode adds two further ways to *view* those bytes,
neither of which changes how they are stored:

```text
Byte       UInt8                     one storage unit             exists
Rune       UInt32 Unicode scalar     one code point               restored here
Grapheme   borrowed byte range       one user-perceived character new here
```

- **`Byte`** is unchanged by this RFC. It is the storage unit, and every
  positional operation on text continues to use byte offsets.
- **`Rune`** is a `UInt32` Unicode scalar value, excluding surrogates — the
  name RFC 0224 retired and reserved. It is a value type, copied by value,
  with literals restored (`'a'`, `'\u{1F600}'`).
- **`Grapheme`** is a **byte range borrowed from the text it came from**, with
  the invariant that the range spans exactly one extended grapheme cluster of
  valid UTF-8. It is not a scalar: `"👨‍👩‍👧"` is one Grapheme, five Runes, and
  eighteen Bytes. Its representation is a pointer and a length, the same shape
  as `Slice<Byte>`, nominally distinct so the invariant is carried in the type.

**`Grapheme` is a view, not storage.** It borrows the bytes it spans, so it has
the provenance behavior every borrowed range in Hexal has: it dangles if the
text it views is freed, reassigned, or leaves scope. That is
programmer-managed, exactly as `bytes()` and `Slice<T>` are today, and this
RFC adds no lifetime machinery for it.

One consequence is a rule rather than a hazard: **`Grapheme` is not Dict-key
eligible**, at any capacity, for the same reason heap `String` is not. A Dict
stores its keys in the table, and a key that points at bytes the Dict does not
own is a dangling key waiting to happen. Code that wants to key by a character
copies the grapheme's bytes into owned text first:

```hexal
for g: Grapheme in text do
    let key: String<16> = try String<16>.from_bytes(g.bytes())
    counts.set(key, counts.get(key) + 1)
end
```

The same caution applies to storing a `Grapheme` in any longer-lived position —
a struct member, a collection element — where it follows `Slice`'s existing
rules and carries `Slice`'s existing risk. Transient use inside the traversal
that produced it is the intended shape.

### Iteration: the payoff of RFC 0224's typed binder

RFC 0224 required a `for` binder's type annotation wherever the collection does
not determine the element type, and named text as the only instance. With three
element types that rule stops being a special case and becomes the mechanism it
was written as:

```hexal
for b: Byte     in text do ... end     # storage units, O(1) per step
for r: Rune     in text do ... end     # code points, decodes as it walks
for g: Grapheme in text do ... end     # characters, stateful break as it walks

for x in text do ... end               # still rejected: which of the three?
```

One collection, three well-defined traversals, each naming its unit at the
call site, and no default that silently picks the wrong one. A program that
means characters says `Grapheme` and gets characters.

The binder's optional index form covers offset capture, so that is not a
reason to reach for a cursor:

```hexal
for i: Size, b: Byte in text do
    if b == b'=' then
        let key: Slice<Byte> = text.slice(0, i)
    end
end
```

### Cursors

A `for` loop advances one position, forward, to completion. Four things a
scanner needs cannot be written that way, and each is why cursors exist rather
than being a convenience over `for`.

```text
String.byte_cursor()     -> ByteCursor
String.rune_cursor()     -> RuneCursor
String.grapheme_cursor() -> GraphemeCursor

ByteCursor.has_next()     -> Bool      RuneCursor / GraphemeCursor alike
ByteCursor.next()         -> Byte      RuneCursor -> Rune, GraphemeCursor -> Grapheme
ByteCursor.peek()         -> Byte      same element type as next()
ByteCursor.offset()       -> Size      byte offset, on all three
```

**`offset()` is always a byte offset**, on every cursor. It is the one unit all
three share, it is what `slice()` takes, and it lets a Rune or Grapheme scan
capture a byte range without converting units.

**`next()` and `peek()` trap when exhausted**, with `has_next()` as the guard.
This matches the cursor contract the language had before RFC 0224. Returning a
union instead would force a match at every scanner step for a condition the
guard already answers.

**Cursors borrow and copy by value.** A cursor is a small descriptor over the
text's bytes; copying one yields an independent position over the same
storage, which is what makes save-and-restore work. Like `Grapheme`, a cursor
is a view rather than storage: it dangles if the text is freed, reassigned, or
leaves scope, and it is not Dict-key eligible.

#### 1. Lookahead

Deciding a two-character token requires seeing the next element without
consuming it:

```hexal
let mut c: ByteCursor = source.byte_cursor()
while c.has_next() do
    let b: Byte = c.next()
    if b == b'-' and c.has_next() and c.peek() == b'-' then
        let _: Byte = c.next()
        emit(TokenKind.Decrement())
    else
        emit(TokenKind.Minus())
    end
end
```

#### 2. Sub-scans that share a position

```hexal
let mut c: RuneCursor = source.rune_cursor()
while c.has_next() do
    let r: Rune = c.next()
    if r.is_numeric() then
        let start: Size = c.offset() - r.utf8_length()
        while c.has_next() and c.peek().is_numeric() do
            let _: Rune = c.next()
        end
        emit_number(source.slice(start, c.offset()))
    end
end
```

The inner loop advances the same position the outer loop reads. Two `for`
loops cannot share a cursor.

#### 3. Two texts at once

```hexal
let mut a: GraphemeCursor = left.grapheme_cursor()
let mut b: GraphemeCursor = right.grapheme_cursor()
let mut shared: Size = 0
while a.has_next() and b.has_next() do
    if a.next().bytes() != b.next().bytes() then break end
    shared = shared + 1
end
```

#### 4. Save and restore

```hexal
let saved: RuneCursor = c        # copy: independent position
if not try_parse_date(c) then
    c = saved                    # rewind
end
```

#### Implementation notes

- **`ByteCursor` needs no utf8proc.** Byte stepping is index arithmetic, so a
  program using only byte cursors and byte iteration selects no Unicode
  dependency. Only text *construction* (validation) and the Rune/Grapheme
  surfaces do.
- **`GraphemeCursor` carries break state.** `utf8proc_grapheme_break_stateful`
  must see every adjacent scalar pair in order, so the cursor owns that state
  and is a few bytes larger than the other two. Copying a cursor copies the
  state, which is precisely what makes case 4 restore correct boundaries.
- **`peek()` must not corrupt that state.** A grapheme peek cannot simply run
  the break machine forward and discard, because the state has advanced. The
  implementation computes the next cluster once and caches it, so `peek()` and
  a following `next()` share the work and the state advances exactly once.
- `Rune.utf8_length() -> Size` is required by case 2 and is added to Tier 1:
  the encoded byte length of the scalar, 1 through 4.

### Tier 1 — Runes

```text
Rune.value() -> UInt32
Rune.from(value: UInt32) -> Rune | Error     # rejects surrogates, > U+10FFFF
Rune.utf8_length() -> Size                   # encoded byte length, 1..4
String.rune_length() -> Size                 # O(n), decodes
String.rune_cursor() -> RuneCursor
String.from_runes(heap: Heap, runes: Slice<Rune>) -> String | Error
```

Rune literals return under the bare-quote syntax RFC 0224 reserved: `'a'`,
`'\n'`, `'\u{1F600}'`. Runes are equality-comparable and ordered by scalar
value, and are valid as a match scrutinee, in `print`, and in interpolation —
restoring what RFC 0224 removed, under the same spellings.

### Tier 2 — Rune properties

```text
Rune.category() -> UnicodeCategory
Rune.is_lower() -> Bool
Rune.is_upper() -> Bool
Rune.is_alphabetic() -> Bool
Rune.is_numeric() -> Bool
Rune.is_whitespace() -> Bool
Rune.to_lower() -> Rune                      # simple mapping, not case folding
Rune.to_upper() -> Rune
Rune.to_title() -> Rune
Rune.display_width() -> Int32                # terminal columns, not layout width
Rune.combining_class() -> UInt8
```

**Simple case mappings are not case folding.** `to_lower()` maps one scalar to
one scalar; it does not handle the multi-scalar cases (`ss` for a sharp s)
that Tier 3's `casefold` does. The names must not suggest otherwise, and a
test must name at least one scalar where the two disagree.

#### `UnicodeCategory` is a closed enum

All thirty Unicode general categories, exactly as the pinned utf8proc release
defines them:

```text
Letter        Lu Ll Lt Lm Lo
Mark          Mn Mc Me
Number        Nd Nl No
Punctuation   Pc Pd Ps Pe Pi Pf Po
Symbol        Sm Sc Sk So
Separator     Zs Zl Zp
Other         Cc Cf Cs Co Cn
```

Unlike `ErrorKind`, this set does **not** grow: the general category set is
fixed by the Unicode standard, and a new Unicode release assigns new code
points to existing categories rather than adding categories. So a type-mode
`match` over `UnicodeCategory` is exhaustive without a final `else`, and the
compiler checks that exhaustiveness the way it does for any closed ADT.

The set is pinned to the vendored release. If a future utf8proc or Unicode
upgrade ever did change it, that is a runtime-pack identity change and must be
caught at pack qualification, not discovered at runtime.

### Tier 3 — text transforms

```text
String.normalize(heap: Heap, form: NormalizationForm) -> String | Error
String.casefold(heap: Heap) -> String | Error

NormalizationForm is NFC | NFD | NFKC | NFKD
```

Each allocates and each returns `| Error`, consistent with every other
producing text operation after RFC 0224.

These are the operations that justify the dependency: they require the full
Unicode decomposition, composition, and case-folding tables, and are not
reasonably hand-rolled or kept in sync with new Unicode releases.

**Allocation contract.** utf8proc's `utf8proc_NFC`/`NFD`/`NFKC`/`NFKD`,
`utf8proc_map`, and `utf8proc_NFKC_Casefold` return `malloc` memory. A
transform must copy the result into one Hexal Heap allocation and release the
utf8proc buffer with C `free` **on every success and failure path**. That
buffer is never exposed as a Hexal `String` and never reaches `Heap.free`;
mixing the two allocators is precisely the bug class RFC 0225 exists to
reject.

### Both text forms get the whole surface

Every operation in all four tiers is available on `String` and on `String<N>`
alike. The two differ in storage and cleanup, never in what can be asked of
them, and `Rune` and `Grapheme` are views over bytes that both forms have:

```hexal
let heap_text: String     = try String.from_bytes(h, raw)
let inline_text: String<64> = try String<64>.from_bytes(raw)

for g: Grapheme in heap_text   do ... end     # identical
for g: Grapheme in inline_text do ... end     # identical
```

A `Grapheme` borrowed from a `String<64>` views that value's inline bytes, so
it dangles when the value is reassigned or leaves scope — the provenance
caution above, applied to inline storage.

Tier 3's transforms allocate and therefore take a `Heap` and produce a heap
`String` on either receiver. An inline destination uses the capacity-explicit
form RFC 0224 established:

```hexal
let folded: String        | Error = inline_text.casefold(h)
let bounded: String<128>  | Error = String<128>.from_bytes(folded.bytes())
```

### Tier 4 — Graphemes

```text
Grapheme.bytes() -> Slice<Byte>
Grapheme.rune_length() -> Size
String.grapheme_length() -> Size             # O(n)
String.grapheme_cursor() -> GraphemeCursor
```

Plus `for g: Grapheme in text`, which is the primary surface; the cursor
covers the scanning cases `for` cannot express.

Segmentation uses `utf8proc_grapheme_break_stateful`. Its state machine must
be fed **every** adjacent scalar pair in order — skipping a pair silently
corrupts later boundaries, which is why the cursor owns the state and no
caller may resume mid-text without it.

### Normalization is never implicit

Settled, and it governs equality, ordering, hashing, and Dict keys alike:

```hexal
let a: String = "é"          # U+00E9
let b: String = "e\u{301}"   # U+0065 U+0301

a == b                        # false, and stays false
```

Text comparison is bytewise at every level, as RFC 0224 specifies. Two
canonically equivalent strings with different bytes are different values, are
ordered by their bytes, hash differently, and are **distinct Dict keys**.

A caller wanting normalization-insensitive behavior normalizes explicitly
first:

```hexal
let key: String = try text.normalize(heap, NFC)
d.set(key, value)
```

Making this implicit would mean hashing allocates, equality depends on Unicode
data version, and two byte-identical programs could disagree across a pack
upgrade. A normalizing `Dict` variant is a defensible later addition; implicit
normalization is permanently refused.

## Complete utf8proc capability inventory

The following table describes what the vendored library can provide. These are
backend capabilities, not automatic Hexal language features.

| Capability | Representative API | What it enables in Hexal | Allocation/contract concern |
|---|---|---|---|
| UTF-8 decode | `utf8proc_iterate` | **validation (live today)**, plus future code-point cursor and iteration | caller-owned input; negative errors must be adapted |
| UTF-8 encode | `utf8proc_encode_char` | future text construction from code points, and code-point formatting | caller-owned output; validate scalar first |
| Scalar validity | `utf8proc_codepoint_valid` | future checked `Integer -> code point` conversion | does not decide whether a scalar is assigned |
| Unicode version | `utf8proc_unicode_version` | target-pack diagnostics and version reporting | version is pack metadata, not inferred from host |
| API version | `utf8proc_version` | doctor and pack qualification | not a user-visible text semantic by itself |
| Codepoint properties | `utf8proc_get_property` | future category, combining-class, bidi, decomposition, case, and width APIs | property struct must not cross the Hexal ABI |
| General category | `utf8proc_category`, `utf8proc_category_string` | future general-category queries | requires a stable Hexal enum/value contract |
| Simple case conversion | `utf8proc_tolower`, `utf8proc_toupper`, `utf8proc_totitle` | future scalar or text case APIs | simple mappings are not full case folding |
| Case predicates | `utf8proc_islower`, `utf8proc_isupper` | future character classification | behavior is Unicode-version dependent |
| Grapheme breaks | `utf8proc_grapheme_break_stateful` | future user-perceived-character cursor | state must process all candidate breaks in order; this, not the code point, is what users call a character |
| Character width | `utf8proc_charwidth`, `utf8proc_charwidth_ambiguous` | future terminal/display-width queries | terminal width is not visual width or layout width |
| NFC/NFD | `utf8proc_NFC`, `utf8proc_NFD` | future canonical normalization | convenience APIs allocate with `malloc` |
| NFKC/NFKD | `utf8proc_NFKC`, `utf8proc_NFKD` | future compatibility normalization | compatibility normalization can lose formatting distinctions |
| NFKC casefold | `utf8proc_NFKC_Casefold` | future identifier/search normalization | not suitable as implicit String normalization |
| General mapping | `utf8proc_map` | future explicit transform operation | returns `malloc` memory; cannot be returned as Hexal String directly |
| Custom mapping | `utf8proc_map_custom`, `utf8proc_decompose_custom` | future compiler/runtime-owned transforms | callbacks and allocation need a separate ABI contract |
| UTF-32 normalization | `utf8proc_normalize_utf32` | future caller-buffer Unicode pipelines | caller must guarantee valid scalar input and capacity |
| UTF-32 re-encoding | `utf8proc_reencode` | future buffer-oriented transforms | output sizing and ownership remain Hexal responsibilities |
| Transform flags | `COMPOSE`, `DECOMPOSE`, `COMPAT`, `CASEFOLD`, `IGNORE`, `STRIPCC`, `STRIPMARK`, `LUMP`, `CHARBOUND`, `REJECTNA`, NLF mappings | future explicit normalization and text-cleaning operations | each combination needs semantic, allocation, and error rules |

### Examples of future operations

These are illustrations of possible future Hexal APIs, not part of this RFC:

```hexal
let normalized: String | Error = text.normalize(heap, NFC)
let folded: String | Error     = text.casefold(heap)
let category: UnicodeCategory  = scalar.category()
let width: Int32               = scalar.display_width()
let cursor: GraphemeCursor     = text.graphemes()
```

The following must **not** happen implicitly:

```hexal
let a: String = "é"       # do not silently normalize
let b: String = "e\u{301}" # do not make b equal to a automatically
```

Hexal's byte identity, equality, ordering, hashing, source fidelity, foreign
interoperability, and allocation behavior remain stable until a future API
explicitly requests a transformation.

## Allocation and ownership boundary

utf8proc has both caller-buffer APIs and convenience APIs that allocate with
the C allocator. Hexal must prefer caller-buffer forms:

```text
utf8proc_iterate       caller-owned input
utf8proc_encode_char   caller-owned output
utf8proc_decompose     caller-owned UTF-32 buffer
utf8proc_reencode      caller-owned buffer
```

The following functions return memory allocated by `malloc`:

```text
utf8proc_map
utf8proc_map_custom
utf8proc_NFC/NFD/NFKC/NFKD
utf8proc_NFKC_Casefold
```

Their result must never be exposed as a Hexal `String` or freed through
`Heap.free`. If a future implementation uses one internally, it must copy the
result into one Hexal Heap allocation and release the utf8proc allocation with
the matching C `free` on every success and failure path. That use requires a
separate API specification and allocator audit.

No utf8proc allocation is permitted in the initial scalar adapter.

## Rejected integration choices

- Do not use a system-installed utf8proc.
- Do not vendor a dynamic library or load it at runtime.
- Do not expose `utf8proc_*` types, enums, callbacks, or error codes in Hexal
  or generated public headers.
- Do not replace Hexal String storage with utf8proc-owned memory.
- Do not use `utf8proc_map` for every String conversion.
- Do not normalize literals, Strings, dictionary keys, or identifiers
  implicitly.
- Do not use utf8proc errors as stable Hexal diagnostics.
- Do not retain the superseded manual UTF-8 algorithm beside the adapter after
  all callers migrate; two validators would create semantic drift.

## Required qualification and measurements

Before a target pack is accepted, maintainers must record:

- exact release archive and source digest;
- API version and Unicode-data version;
- compiler, archiver, C dialect, flags, defines, target triple, libc or SDK,
  and CPU baseline;
- archive symbol and ABI evidence;
- MIT and Unicode license files;
- static link and runtime probe results; and
- archive and executable size.

Compare the existing and utf8proc-backed runtime for:

- ASCII and two-, three-, and four-byte sequence validation;
- malformed sequences at every byte position;
- surrogate, overlong, truncated, and above-U+10FFFF inputs;
- text construction from bytes, at both the heap and inline forms;
- generated C size and link time; and
- target-pack size.

The first implementation must not change observable behavior to improve a
benchmark. Measurements record the cost of the dependency and the benefit of
deleting duplicate Unicode logic.

## Implementation plan

Eight phases, ordered so that every phase is independently verifiable and the
dependency is proven before any language surface depends on it. Phases 3-7 each
add one element type or tier and can land as separate changes.

Current baseline, verified against the tree on 2026-09-21: RFC 0224 is
implemented. `String<N>` compiles, `Strand` is gone with a migration
diagnostic, bare-quote literals are reserved, `RuneCursor` is gone,
`for b: Byte in text` works and bare `for b in text` is rejected for
ambiguity. The runtime carries `hex_string {data, byte_length, storage_kind}`,
the shared `hex_text {data, length}` read view, and the hand-rolled
`hex_utf8_valid(data, length) -> bool`.

### Phase 0 — vendor and qualify the archive

Mirrors exactly what `lib/BUILD.md` records for mimalloc and libuv.

1. Add utf8proc as a git submodule at `modules/utf8proc`, checked out at the
   **v2.11.3** release tag. Record the tag, the pinned commit, the release
   archive URL, its byte size, and its SHA-256, as `modules/MIMALLOC.md` does.
2. Confirm the release's source layout before writing the compile command.
   utf8proc is expected to be one translation unit (`utf8proc.c`, which
   includes the generated `utf8proc_data.c`) plus the public `utf8proc.h`, but
   **this must be checked against the tarball rather than assumed** — the
   command below is written from upstream documentation, not from a vendored
   tree, and no part of this RFC has been compiled.
3. Build one archive per shipped pack, matching the existing build identity
   (Clang 23.1.1, `-O2 -DNDEBUG -fPIC -pthread`, target-portable x86-64, no
   `-march=native`):

   ```text
   clang -std=c11 -O2 -DNDEBUG -fPIC -pthread -DUTF8PROC_STATIC \
     -I modules/utf8proc -c modules/utf8proc/utf8proc.c -o utf8proc.o
   ar rcs utf8proc_v2.11.3/utf8proc.a utf8proc.o
   ```

4. Lay the pack entry out as its siblings are:

   ```text
   lib/x86_64-linux-gnu/utf8proc_v2.11.3/
     include/utf8proc.h
     utf8proc.a
     LICENSE.md
   ```

5. Record in `lib/BUILD.md`, in the section shape mimalloc and libuv use: the
   source commit, the Unicode-data version (**17.0.0**), the compile command,
   the archive size, and the archive SHA-256.
6. Add the dependency entry and every new file hash to
   `lib/<target>/manifest.json`. `format_version` stays 1.
7. Extend the combined native probe in `lib/BUILD.md` to compile, link, and run
   a program using all three archives together, proving no symbol or link-order
   conflict.

**Two packs exist on disk.** `x86_64-linux-gnu` is embedded into `bin/hexal`
(`//go:embed x86_64-linux-gnu` in `lib/pack.go`); `x86_64-windows-gnu-ucrt`
is checked in but not embedded. Qualify Linux first and ship it. The Windows
pack must gain a matching entry before any program targeting it selects
utf8proc, or that target regresses to a link failure; until then, Windows is
qualified for the phases that select no Unicode dependency.

*Verify:* the probe runs; `go test ./...` is unaffected because no compiler
code has changed yet.

### Phase 1 — dependency plumbing, with no caller

1. Add `RuntimeUtf8proc RuntimeDependency = "utf8proc"` to
   `compiler/runtime_dependency.go` and to the `switch` in
   `runtimeDependencies`, which panics on unknown names.
2. Add the selection predicate beside the existing two in
   `compiler/generator/generator.go`, where `merged.heapState.selected()` emits
   `mimalloc` and `libuvSelected(merged)` emits `libuv`.
3. Add utf8proc to `internal/driver/doctor.go`'s verified dependency list so
   `hexal doctor` checks the new payload.

Nothing selects it yet. This phase is complete when a hand-forced selection
materializes the include root and archive, orders them in the link, and
contributes to the build identity.

*Verify:* pure-Go driver tests over a fixture manifest; no generated artifact
moves, so the snippet manifest is unchanged.

### Phase 2 — substitute the validator

The one existing caller. `hex_utf8_valid` keeps its exact signature and
contract — reports, never traps — and its body becomes the
`utf8proc_iterate` loop in Validation above. Selecting the String component
now selects `utf8proc`.

Delete the hand-rolled lead-byte, continuation-byte, shortest-form, surrogate,
and maximum-scalar formulas **only after** the substitution is in place. Two
validators would drift.

*Verify:* a differential test over every byte sequence of length 1-3 plus a
corpus of 4-byte forms, asserting old and new agree on accept/reject before
the old one is deleted. This is the phase that proves the dependency works,
and it changes no observable behavior.

### Phase 3 — `Rune`, `RuneCursor`, and literals

1. Reintroduce `Rune` as a protected type in `compiler/types`, `UInt32`
   scalar excluding surrogates.
2. Restore bare-quote literals in the lexer. RFC 0224 reserved the syntax and
   emits `bare-quote literals are reserved`; that diagnostic is replaced, not
   removed, so any program written against 0224 keeps compiling.
3. Add `Rune.value`, `Rune.from`, `Rune.utf8_length`, ordering, equality,
   `to<T>()`, match-scrutinee support, `print`, and interpolation.
4. Add `String.rune_length`, `String.rune_cursor`, `String.from_runes`, and
   the `for r: Rune in text` binder arm, on `String` and `String<N>` alike.
5. Add `ByteCursor` in the same change. It selects no utf8proc dependency and
   is the simplest instance of the cursor shape, so it proves `has_next`,
   `next`, `peek`, `offset`, and copy-independence without any Unicode logic.

*Verify:* the Validation section's Rune and cursor items; generated-C text
assertions for the new component; snippet manifest moves only for snippets
that use the new surface.

### Phase 4 — `UnicodeCategory` and Rune properties

Add the closed 30-variant enum and the Tier 2 predicates and mappings. Match
exhaustiveness is checked without a final `else`, unlike `ErrorKind`.

*Verify:* an exhaustive match over `UnicodeCategory` compiles with no `else`;
a non-exhaustive one is rejected; `to_lower` and `casefold` disagree on at
least one named scalar.

### Phase 5 — `Grapheme` and `GraphemeCursor`

1. Add `Grapheme` as a borrowed byte range, not Dict-key eligible.
2. Implement the cursor over `utf8proc_grapheme_break_stateful`, with the
   compute-once-and-cache behavior that keeps `peek()` from advancing the
   break state.
3. Add `String.grapheme_length`, `String.grapheme_cursor`, and the
   `for g: Grapheme in text` binder arm.

*Verify:* combining marks, an emoji ZWJ sequence, and a regional-indicator
pair each yield one Grapheme; a saved-and-restored cursor reproduces the same
later boundaries.

### Phase 6 — normalization and case folding

Tier 3. Each transform copies the utf8proc result into one Hexal Heap
allocation and releases the utf8proc buffer with C `free` on **both** the
success and every failure path.

*Verify:* a leak check covering both paths; no utf8proc pointer is reachable
as a Hexal `String` or passed to `Heap.free`; `"é" == "e\u{301}"` stays false
after the API exists.

### Phase 7 — reference synchronization

Update `docs/reference.md` for the restored and new surface: `Rune`,
`Grapheme`, the three cursors, `UnicodeCategory`, the Tier 3 transforms, the
protected-type list, the scalar table, the `for`-binder element types, and the
print and interpolation type lists.

Phase 2 alone requires no reference change: it is a backend substitution with
identical observable behavior.

### Phase 8 — full gate

`go test ./...`, `go vet ./...`, the tagged C23 suite, the snippet manifest
rebuild with a reviewed artifact diff, and the native pack probes for every
qualified target.

## Validation

This section is exhaustive.

- Every shipped target pack contains one verified static utf8proc archive,
  matching header, license material, build record, digest, API version, and
  Unicode-data version.
- The driver rejects a missing, mismatched, corrupt, unlisted, or
  target-incompatible utf8proc payload.
- The compiler remains usable with no utf8proc installation and performs no
  host discovery or native compilation.
- Programs without a demanded runtime text or Unicode component select no
  utf8proc dependency and preserve the existing generated artifacts.
- Literal-only programs do not acquire utf8proc solely because a literal is
  non-ASCII.
- Generated public headers contain no utf8proc include or symbol.
- The private runtime uses `utf8proc_iterate` with a non-negative input length
  no greater than four for every scalar step.
- Validation accepts and rejects exactly the byte sequences the hand-rolled
  validator accepts and rejects: overlong forms, surrogates, truncated
  sequences, and scalars above U+10FFFF are refused, and every well-formed
  sequence is accepted.
- Validation **reports**; it does not trap. A malformed byte sequence reaching
  text construction produces the `| Error` RFC 0224 specifies, with the same
  Hexal-owned message, and no utf8proc error code or English string reaches a
  diagnostic.
- The validation adapter performs no utf8proc allocation.
- `String`, `String<N>`, byte slices, equality, ordering, hashing, printing,
  interpolation, and `free` behavior remain unchanged.

Language surface:

- `for b: Byte in text`, `for r: Rune in text`, and `for g: Grapheme in text`
  each traverse the same text with the stated unit; `for x in text` without an
  annotation remains rejected with RFC 0224's ambiguity diagnostic.
- A Rune traversal of known text yields exactly the expected scalars for
  one-, two-, three-, and four-byte sequences; a Grapheme traversal yields one
  element for a combining-mark sequence, an emoji ZWJ sequence, and a regional
  indicator pair.
- `Rune.from` rejects surrogates and values above U+10FFFF and accepts every
  other scalar.
- Rune literals, ordering, equality, match, `print`, and interpolation behave
  as they did before RFC 0224 removed them.
- Simple case mappings map one scalar to one scalar and do **not** perform the
  multi-scalar expansions `casefold` performs; a test names at least one case
  where the two differ.
- A type-mode `match` over `UnicodeCategory` without a final `else` is
  rejected, as it is for `ErrorKind`.
- `Grapheme` borrows: its bytes alias the source text, and no copy is made.
- `Grapheme` is rejected as a Dict key, at every text capacity.
- The grapheme cursor feeds `utf8proc_grapheme_break_stateful` every adjacent
  scalar pair in order; a test proves that a boundary late in the text is
  unaffected by where iteration paused.
- Every tier operation is available on `String` and on `String<N>` alike, and
  a test exercises at least one operation from each tier on both forms.

Cursors:

- `byte_cursor()`, `rune_cursor()`, and `grapheme_cursor()` each traverse the
  same text as the corresponding `for` binder and yield identical elements in
  identical order.
- `offset()` returns a **byte** offset on all three cursors, and a range built
  from two offsets round-trips through `slice()` to the expected bytes.
- `next()` and `peek()` each trap when the cursor is exhausted; `has_next()`
  reports false at exactly that point.
- `peek()` does not advance: a `peek()` followed by `next()` yields the same
  element, and repeated `peek()` calls yield the same element.
- A copied cursor holds an independent position: advancing the copy does not
  move the original, and restoring from a saved copy resumes at the saved
  position with the same subsequent elements — including grapheme boundaries,
  which proves the break state copied correctly.
- A `ByteCursor`-only program selects no utf8proc dependency.
- Each Tier 3 transform copies its result into one Hexal Heap allocation and
  releases the utf8proc buffer with C `free` on **both** the success and every
  failure path; a leak check covers both.
- No utf8proc-allocated pointer is ever reachable as a Hexal `String` or
  passed to `Heap.free`.

Nothing implicit:

- No normalization, case folding, grapheme segmentation, category mapping,
  width transformation, or other Unicode transformation becomes implicit.
- `"é" == "e\u{301}"` is **false**; the two hash differently, order by their
  bytes, and are distinct Dict keys. Bytewise comparison is unchanged by the
  presence of a normalization API.
- Generated artifacts and the snippet manifest change only for components that
  actually select utf8proc.
- External C23 fixtures compile, link, and run against each qualified static
  archive.
- The ordinary Go suite passes without an installed utf8proc or C toolchain.

## Open implementation inputs

Everything design-level is settled. What remains is produced by building the
archive, and all of it lands in Phase 0:

- the exact source file list, confirmed against the v2.11.3 tarball rather
  than assumed from upstream documentation;
- the archive SHA-256 and byte size, per target pack;
- symbol and ABI evidence from the combined three-archive probe;
- generated-C size, link time, and pack-size deltas.

Every utf8proc API named in this RFC comes from upstream documentation and has
**not** been checked against a vendored header, because none is vendored yet.
Phase 0 verifies each one that later phases call —
`utf8proc_iterate`, `utf8proc_codepoint_valid`, `utf8proc_encode_char`,
`utf8proc_category`, `utf8proc_tolower`/`toupper`/`totitle`,
`utf8proc_charwidth`, `utf8proc_grapheme_break_stateful`, and the
`utf8proc_NFC`/`NFD`/`NFKC`/`NFKD`/`NFKC_Casefold` family — and any signature
that differs is a spec correction before that phase starts, not an
implementation improvisation.

No open input permits implicit normalization, public utf8proc types, hidden
allocation, or a change to current text ownership and representation.

## Implementation readiness

**Ready.** The release is pinned, the dependency boundary matches the shipped
runtime-pack contract rather than inventing one, the language surface is
settled across all four tiers and three cursors, the Validation section is
exhaustive, and the plan is phased so each step is verifiable on its own.

Baseline confirmed against the tree on 2026-09-21: RFC 0224 is implemented, so
this RFC builds on byte-oriented storage, typed `for` binders, bytewise
comparison, and the reserved bare-quote literal syntax as they actually exist.

Start at Phase 0. It is the only phase that cannot be done from the
specification alone, because it produces the archive every later phase links
against, and it is where every unverified utf8proc signature in this document
gets checked against a real header.

Two things a implementer should hold onto:

- **Phase 2 is the proof.** Substituting the validator changes no observable
  behavior, so if anything moves — a diagnostic, an artifact hash, a test —
  the dependency plumbing is wrong and it is cheap to find out there rather
  than four phases later.
- **Phases 3-6 are independently shippable.** Each adds one element type or
  tier with its own Validation items. A phase that turns out harder than
  expected can be deferred without stranding the ones before it.

