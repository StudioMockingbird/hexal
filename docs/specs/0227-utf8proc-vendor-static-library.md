# RFC 0227: Vendored utf8proc Static Library

- Kind: Architecture Decision Record (ADR)
- Status: Open Discussion; not scheduled. The dependency boundary and
  capability inventory are proposed; target-pack qualification and the first
  runtime adoption are not complete
- Created: 2026-09-21
- Updated: 2026-09-21
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
  pending this RFC. Everything Unicode-related this document describes is
  therefore a surface it **restores or introduces**, not one it adapts. After
  RFC 0224 the language's only live Unicode requirement is UTF-8
  well-formedness validation on text construction; every other capability in
  the inventory below is new surface needing its own specification
- Terminology: this RFC says *Unicode scalar* or *code point* for a single
  Unicode value, and reserves the concrete Hexal type name as an open question
  — RFC 0224 retired `Rune` and did not choose its successor
- Does not update: `docs/reference.md`, Hexal syntax, or text representation

## Decision summary

Hexal will vendor one exact utf8proc release as a target-qualified static
library in each shipped runtime pack. The core compiler records a logical
`utf8proc` runtime dependency; the build driver materializes the matching
header, archive, and license files from the selected pack and links the
archive. The compiler never discovers, builds, or loads utf8proc.

The initial candidate is utf8proc **2.11.3**, whose upstream release records
Unicode **17.0.0** support. The release, archive digest, build flags, target
identity, API version, Unicode-data version, and license material become part
of the pack identity. The exact release remains an implementation input until
the archive is independently built and qualified.

The first runtime use is a private adapter for UTF-8 well-formedness
validation. It preserves Hexal's current storage, diagnostics, allocation, and
evaluation contracts. Code-point stepping and encoding are written here as
primitives but have no caller after RFC 0224. Normalization, case folding,
grapheme segmentation, character properties, width, and every other Unicode
behavior are backend capabilities only; each public operation requires its own
focused language specification.

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
candidate:      2.11.3
Unicode data:   17.0.0
static archive: libutf8proc.a
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

The manifest records at least:

```json
{
  "name": "utf8proc",
  "version": "2.11.3",
  "unicode_version": "17.0.0",
  "include_root": "utf8proc_v2.11.3/include",
  "archive": "utf8proc_v2.11.3/utf8proc.a",
  "license_files": ["utf8proc_v2.11.3/LICENSE.md"],
  "archive_sha256": "..."
}
```

The actual manifest schema, digest, compiler identity, target triple, libc or
SDK baseline, and archive build flags are owned by the runtime-pack contract.
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
runtime String validation component          utf8proc dependency
future Unicode component                     utf8proc dependency
```

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

This matters for scoping: taking the dependency **now** buys one validator
that already works correctly, while the capabilities that justify a Unicode
backend — normalization, case folding, grapheme segmentation, properties,
width — all belong to APIs that do not exist yet. See Open questions.

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

1. Select utf8proc 2.11.3 or a newer fully qualified release and record the
   Unicode-data version, upstream archive digest, and licenses.
2. Build one `libutf8proc.a` for every target pack being shipped, with static
   linkage and target-qualified build flags.
3. Add `utf8proc` to the runtime manifest and dependency model without adding
   host paths or filesystem behavior to `compiler.Compile`.
4. Add driver manifest validation, payload hashing, materialization, link
   ordering, and a native archive-consumption probe.
5. Add the private header include to the runtime component and implement the
   Hexal validation adapter around `utf8proc_iterate`.
6. Route every generated-runtime UTF-8 validation call through that adapter —
   after RFC 0224 that is text construction from bytes and program-input
   validation.
7. Delete superseded manual UTF-8 formulas only after the previous step is
   complete.
8. Run ordinary pure-Go tests, generated-C text assertions, target-pack probes,
   and external C23 fixtures for every qualified target.
9. Update `docs/reference.md` only if the implementation changes a language
   contract. The initial backend substitution should require no semantic
   reference change.

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
- The initial adapter performs no utf8proc allocation.
- `String`, `String<N>`, byte slices, equality, ordering, hashing, printing,
  interpolation, and `free` behavior remain unchanged.
- No normalization, case folding, grapheme segmentation, category mapping,
  width transformation, or other Unicode transformation becomes implicit.
- Generated artifacts and the snippet manifest change only for components that
  actually select utf8proc.
- External C23 fixtures compile, link, and run against each qualified static
  archive.
- The ordinary Go suite passes without an installed utf8proc or C toolchain.

## Open implementation inputs

- Final release selection if 2.11.3 is not the accepted pinned input.
- Per-target source build commands and flags.
- Runtime-manifest schema entry and dependency ordering.
- Archive hashes and ABI evidence.
- Size and performance measurements.
- The separate API specifications for normalization, case folding, grapheme
  iteration, properties, width, and text transformations.

No open input permits implicit normalization, public utf8proc types, hidden
allocation, or a change to current text ownership and representation.

## Implementation readiness

The architecture is suitable for dependency qualification. Implementation is
blocked until the exact release is selected, every shipped target pack is
qualified, and the runtime-pack manifest/linking changes are specified in the
owning build-driver contract. The initial scalar adapter is implementation
ready once those inputs are fixed. Public Unicode features are not authorized
by this RFC and require separate focused specifications.

