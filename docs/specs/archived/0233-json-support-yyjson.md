# RFC 0233: JSON Support via yyjson

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed 2026-09-30; implemented and validated on Windows and
  Linux/WSL
- Created: 2026-09-22
- Updated: 2026-09-30
- Origin: requested language-level JSON support by integrating yyjson 0.13.0
- Depends on: RFC 0052 (C backend), RFC 0055 (build and runtime-pack inputs),
  the current target-profile matrix, and the current String, List, Dict, Heap,
  and Error contracts in `docs/reference.md`
- Coordinates with: the implemented Dict contract, RFC 0225 (cross-allocator
  release rejection), RFC 0227 (utf8proc, the sibling vendored dependency this
  RFC mirrors in layout and boundary), and RFC 0231 (unified C emission; the
  new component templates join its mechanism)
- Updates `docs/reference.md`: yes — one new JSON section and one `std/json`
  row in the standard-library module table. The grammar is untouched: this RFC
  adds no syntax, only a std module, a nominal type, and three functions
- Swept code: none. No code in the tree exists solely because JSON support, a
  mixed-allocator JSON buffer, or a yyjson-facing defense was absent, and RFC
  0225's provenance machinery needs no change: no yyjson-allocated pointer ever
  becomes a Hexal value, so there is no wrong-allocator release for it to
  reject

## Decision summary

Hexal will vendor one exact yyjson release as a target-qualified static
library in each shipped runtime pack, exactly as the runtime pack already
does for libuv, mimalloc, and utf8proc. The core compiler records a logical
`yyjson` runtime dependency; the build driver materializes the matching
header, archive, and license from the selected pack and links the archive.
The compiler never discovers, builds, or loads yyjson.

The language surface is a `std/json` core-library module exporting one
closed nominal union, `Value` — the JSON value model — and three module
functions: `parse`, `stringify`, and `free`. The generated C runtime owns a
private adapter that is the only code including `yyjson.h` or naming a
`yyjson_*` symbol; Hexal source never calls the library directly, and no
yyjson type, flag, enum, or error code appears in Hexal or in any public
generated header.

Parsing accepts RFC 8259 JSON plus JSONC's two deliberate extensions: C-style
line/block comments and one trailing comma in an object or array. It accepts no
other JSON5 or yyjson extension. yyjson runs entirely on the
caller's Heap through one custom allocator, so no C `malloc` participates and
the cross-allocator hazard RFC 0225 exists to reject cannot arise. Every
failure reports as a Hexal-owned `Error` built from yyjson's complete read
error record: code, English detail, and byte position. Hexal computes the
one-based line and byte column from that position and owns the resulting
message; no pointer into yyjson storage crosses the boundary.

## Why this dependency

Correct JSON is not a weekend parser. It requires, together:

- the complete RFC 8259 grammar plus the deliberately accepted JSONC comment
  and trailing-comma forms, while still rejecting single quotes, unquoted keys,
  `NaN`/`Infinity`, and trailing content;
- string escape decoding including UTF-16 surrogate pairs to UTF-8;
- number parsing to exact 64-bit integers and shortest-round-trip `double`
  formatting on output;
- duplicate-member policy, arbitrarily long member names, and depth bounds
  against adversarial nesting; and
- input validation and performance work (sequential scanning) that a
  hand-written parser will not match.

Hand-rolling gives Hexal two implementations — reader and writer — to keep
correct and in sync forever, in the most routinely adversarial input channel a
program has. One pinned backend supplies both directions; Hexal keeps ownership
of the value model (`Value` is a Hexal ADT over Hexal List and String,
not yyjson's DOM exposed), the diagnostics, and the allocation rules.

## Upstream qualification

The vendored source is the official ibireme yyjson release, not a system
package or a moving checkout:

```text
upstream:       https://github.com/ibireme/yyjson
pinned release: 0.13.0 (released 2026-09-08)
license:        MIT
static archive: yyjson.a
API header:     yyjson.h
```

The archive and header are checked against the release before check-in, and
the MIT license text ships beside them. A later yyjson upgrade is a
runtime-pack identity change even when the Hexal surface is unchanged.

Review claims that release 0.13.0 or
`YYJSON_DISABLE_UTF8_VALIDATION` did not exist were rechecked and excluded:
the official release is dated 2026-09-08, and the 0.13.0 `yyjson.h` defines
that option together with the other compile-time switches named below. RFC
0223 is already implemented and archived, so it is not a prerequisite for
this work.

The archive is built separately for each pack with that pack's recorded build
identity from `lib/BUILD.md`; there is no one host-shaped command copied across
targets. The Linux archive uses the Linux target, PIC, and its recorded Clang
and archiver. The Windows archive uses the MinGW target and its recorded Clang
and LLVM archiver. Both builds add these definitions:

```text
-DYYJSON_DISABLE_FILE=1
-DYYJSON_DISABLE_INCR_READER=1
-DYYJSON_DISABLE_UTILS=1
```

These are yyjson 0.13.0 compile-time options. Hexal owns file IO, exposes no
incremental reader in this RFC, and exposes no yyjson utility API. Non-standard
reader support remains compiled in because JSONC needs comments and trailing
commas. No other compile-time option is set:

```text
YYJSON_FREESTANDING   unset; the adapter uses libc-backed formatting
YYJSON_DISABLE_UTF8_VALIDATION
                      unset (therefore 0); input validation stays on as
                      defense in depth, including over escape-decoding output
read flags            YYJSON_READ_ALLOW_COMMENTS |
                      YYJSON_READ_ALLOW_TRAILING_COMMAS
write flags           defaults only; compact RFC 8259 output
```

Both reader and writer depth macros are set from the compiler-owned JSON depth
configuration, default 256. Whether this exact flag set accepts the JSONC cases
and rejects every other extension named by this RFC is a Phase 0 verification
item, not an assumption.

## Runtime-pack layout

Every supported target pack that ships a Hexal runtime dependency contains a
versioned yyjson entry:

```text
lib/<hexal-target>/
  manifest.json
  yyjson_v0.13.0/
    include/yyjson.h
    yyjson.a
    LICENSE
```

`manifest.json`'s schema is closed and already defined by `runtimeManifest`
in `internal/driver/runpack.go`. yyjson adds **one entry to the existing
`dependencies` array**, in the same shape libuv, mimalloc, and utf8proc use:

```json
{
  "name": "yyjson",
  "include_root": "yyjson_v0.13.0/include",
  "archive": "yyjson_v0.13.0/yyjson.a",
  "system_libraries": [],
  "license_file": "yyjson_v0.13.0/LICENSE"
}
```

`system_libraries` is empty; yyjson is single-threaded C with no link
dependency beyond libc. If the combined four-archive probe proves otherwise,
the real list is recorded here as a spec correction before Phase 1.

**No schema change.** `format_version` stays 1. The release version lives in
the directory name (`yyjson_v0.13.0`), exactly as `mimalloc_v3.5.1` and
`utf8proc_v2.11.3` do, and the source commit, compile command, size, and
SHA-256 are recorded in `lib/BUILD.md` beside its other build facts.

**The dependency registry grows atomically with the packs.** The authoritative
registry is `compiler/specdata/dependencyRegistry`; the driver derives manifest
validation and link order from it. Append `yyjson` to that stable registry and
add the matching entry, real archive, and hashes to both checked-in packs
(`x86_64-linux-gnu` and `x86_64-windows-gnu-ucrt`) in the same change. Do not
recreate an ordered dependency list in the driver or sort existing entries as
a side effect.

No host path, environment value, system-installed yyjson, pkg-config result,
or network lookup participates in compilation.

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
yyjson
```

It must not read the vendor tree, compile C, inspect the host, resolve a
library, or include yyjson source in generated files.

The build driver owns selecting the target-qualified pack, validating the
manifest and demanded payload hashes, materializing the private include root
and archive, ordering the archive in the link, running native qualification
probes, and reporting missing, mismatched, or corrupt dependency inputs.

Generated public module headers contain no yyjson include and no yyjson
symbol. Exactly one generated translation unit — the json adapter — includes
the header:

```c
/* private generated adapter unit */
#include <yyjson.h>
```

Hexal source never names a `yyjson_*` function, type, constant, or flag; the
public surface is `std/json` only. Hexal's existing C interoperability is
unchanged: a user who wants their own `from c <yyjson.h>` bindings may write
them through the ordinary foreign-declaration path, which this RFC neither
adds, documents, nor restricts.

## Dependency demand

The dependency is selected by the two operations that call yyjson:

```text
program with no std/json operation    no yyjson dependency
Json.free alone                       json value helpers only, no yyjson
Json.parse                            yyjson dependency
Json.stringify                        yyjson dependency
```

`free` is a walk over Hexal-owned values using the existing String, List, and
Dict release primitives; it calls no yyjson function, so a program that only
releases trees materializes no yyjson header, archive, or symbol. The json
component is emitted as two units to keep that true: value helpers (no
yyjson include) and the adapter (the only `#include <yyjson.h>`).

Selection is program-wide and deterministic, and a selected dependency
contributes to the build identity. The static archive is linked as a private
runtime input; no dynamic yyjson library is searched for or loaded.

## Object representation decision

`Value.Object` stores an ordered `List<Member>`. Each `Member` owns a heap
`String` name and a `Value`; JSON names therefore have no independent byte
capacity. This avoids imposing a JSON-specific key limit or widening the
general Dict key contract. Parsing preserves the first-occurrence order of
distinct names. A later duplicate, compared after escape decoding, replaces
the earlier value in place, so the last occurrence wins without changing that
name's original position. Stringify emits members in list order.

Every JSON traversal uses `config.JSONMaxDepth`, defined once in
`compiler/config` with value 256. No yyjson build script, adapter, generator,
or traversal repeats the literal. It feeds the yyjson reader and writer build
limits and Hexal translation/free/stringify guards, and contributes to
runtime-pack/build identity. It is not a language argument or per-call option.
A later compiler release may change it deliberately without changing the JSON
API.

## Language surface

### The `Value` type

`std/json` exports `Value` and the constructible `Member` record:

```text
type Value is union
    | Null
    | Bool as value: Bool end
    | Int as value: Int64 end
    | UInt as value: UInt64 end
    | Float as value: Float64 end
    | Text as value: String end
    | Array as items: List<Value> end
    | Object as entries: List<Member> end
end

type Member is struct
    name: String,
    value: Value,
end
```

- `Value` is an ordinary nominal closed union with canonical identity
  `std/json.Value`; an importer reaches it through the import alias
  (`Json.Value`, mirroring `Io.Seek` and `Time.Instant` where the alias is the
  module name and the type is the noun). Variants construct as
  `Alias.Value.Variant(...)` with `()` on the unit variant, and a type-mode
  match reads a record payload through the narrowed scrutinee.
- The variant set is closed by the JSON data model and grows never, so a
  type-mode match is exhaustive **without** a final `else` — unlike
  `ErrorKind`, whose set may grow. The compiler checks that exhaustiveness as
  for any closed ADT.
- Recursion is through the `List` handles, which are pointer-sized,
  so the representation is finite; direct by-value recursion stays rejected by
  the existing rule. `Text`'s payload is a heap `String`; `Array` and `Object`
  own their containers. `Null`, `Bool`, `Int`, `UInt`, and `Float` own nothing.
- `Member` is a compiler-owned ordinary record with canonical identity
  `std/json.Member`; callers may construct it with `Member(name = ..., value = ...)`.
  Its `name` is an owning heap String and `value` follows the `Value` ownership
  rules. `Value.Object(entries = ...)` accepts an ordinary `List<Member>`.
- `Value` follows general storability: bindings, parameters and results, ADT
  payloads, Array/Slice/List elements, Dict values, Task arguments and
  results, Channel elements. It is not Dict-key eligible — keys are `Int32` or
  `String<N>`, and that rule is unchanged.
- `Value` is compiler-owned as a core-library export: no user module declares
  its variants or methods, and no instance methods exist — the whole surface
  is the three module functions below.

Baseline confirmed against the tree on 2026-09-22 by compiling through
`compiler.Compile`: this declaration shape (unit and record variants, recursion
through `List<Value>` and `List<Member>`, a `String` payload),
variant construction, container insertion, and exhaustive type-mode match
without `else` all compile today; a match missing a variant is rejected with
the existing exhaustiveness diagnostic. A record variant's member list takes
its own `end`, per the reference grammar.

### `std/json` functions

```text
Json.parse(heap: Heap, text: String)       -> Value | Error
Json.stringify(heap: Heap, value: Value)   -> String | Error
Json.free(heap: Heap, value: Value)        -> no value
```

```hexal
import
    Json from std.json
end

fun load(h: Heap, text: String): Nil | Error do
    let root: Json.Value = try Json.parse(h, text)
    let out: String = try Json.stringify(h, root)
    print(out)
    Json.free(h, root)
    return nil
end
```

- `parse` accepts one complete RFC 8259 value with optional surrounding RFC
  whitespace, C-style line/block comments, and one trailing comma in an object
  or array. It accepts no other JSON5 extension. The heap argument evaluates
  first and exactly once, as everywhere else. The whole document is read into
  memory; there is no streaming or incremental form. The parameter is heap `String` — the same
  shape `std/net.parse_address` takes — so a `String<N>` caller copies first
  through the explicit `copy(heap)` route the no-implicit-conversion rule
  already names.
- `stringify` emits compact JSON: no insignificant whitespace, no trailing
  newline, UTF-8 bytes with non-ASCII characters unescaped (JSON escaping of
  quotes, backslash, and control characters still applies). Object members
  are emitted in their List order; parsing retains first-occurrence order and
  replaces duplicate values in place.
- `free` recursively releases the whole tree through the ordinary shallow
  rules: `Text` frees its String; `Array` frees each element's tree then the
  List storage; `Object` frees each member name, each value tree, then the
  member List storage; scalar variants free
  nothing. Exactly-once per distinct allocation applies as for every container:
  an aliased subtree must not be freed twice, and freeing dangles the handle.
  No yyjson call is involved.

Round-trip contract, stated once:

```text
parse(stringify(v)) is equal to v by JSON data value
```

- Integers in either 64-bit range round-trip exactly, `Float` values round-trip
  through shortest-round-trip spelling, and text round-trips byte-exactly.
- Byte-exact document round-trip is **not** promised: whitespace, duplicate
  members, and number spelling are not preserved
  (`1e2` parses to `Float` and may re-emit as `100`; an integral `Float` or a
  `UInt` within `Int64` reparses as `Int`). Round-trip is by value, never by
  variant identity.

### Number mapping

JSON has one number type; `Value` splits it to keep integers exact:

| JSON input | `Value` variant |
| --- | --- |
| integer in `[-2^63, 2^63)` | `Int` |
| integer in `[2^63, 2^64)` | `UInt` |
| integer below `-2^63` or at/above `2^64` | `Error` |
| fraction or exponent part present (`1.0`, `1e2`, `-0.5`) | `Float` |
| outside `Float64` finite range (`1e400`) | `Error`, see Errors |

"integer" means no fraction and no exponent part. On output, `Int` and `UInt`
emit exact decimal digits and `Float` emits yyjson's shortest round-trip form.
Parsing never truncates, never clamps, never produces NaN or infinity, and
never converts an out-of-range integer to Float64. `-0` has integer spelling
and normalizes to `Int(0)`; the API preserves JSON data values, not number
lexemes or a negative-zero integer spelling.

### Objects: member names and duplicates

- Member names are owning heap `String` values in `Member`; there is no
  JSON-specific member-name byte limit. Allocation failure follows the
  existing `ResourceExhausted` contract and never truncates a name.
- Duplicate member names compare decoded UTF-8 bytes and collapse with **last
  occurrence wins**. The value replaces the prior value in place, retaining the
  first occurrence's position; translation releases the displaced value and
  duplicate name exactly once.

### Errors

yyjson's read error contains a code, a constant English message, and a zero-based
byte position. It does not contain line/column fields. The adapter consumes all
three: the code selects the Hexal `ErrorKind`, the byte position is preserved,
and Hexal scans the input prefix to compute one-based line and byte-column
values. The yyjson message is copied into the Error's owned String; no native
pointer escapes. A typical message is:

```text
unexpected character, expected ':' after key at line 3, column 12 (byte 47)
```

This deliberately makes detailed parse wording part of the pinned yyjson
integration rather than writing and maintaining a second JSON parser only for
diagnostics. Upgrading yyjson must review diagnostic-baseline changes.

| Condition | `ErrorKind` | Message |
| --- | --- | --- |
| empty input; unexpected content/end/character; invalid structure, comment, number, string, or literal; depth exceeded | `InvalidInput` | yyjson detail plus line, byte column, and absolute byte position |
| yyjson allocation failure | `ResourceExhausted` | `JSON parsing ran out of memory` |
| impossible adapter-facing code (`INVALID_PARAMETER`, file codes, incremental `MORE`) | `Unknown Error` | `JSON parser reported an impossible state` |
| integer outside `Int64 | UInt64` | `InvalidInput` | `JSON integer is out of range` |
| number outside `Float64` finite range | `InvalidInput` | `JSON number is out of range` |
| `stringify` of NaN or +/- infinity | `InvalidInput` | `JSON cannot represent a non-finite number` |
| `stringify` encounters a container cycle | `InvalidInput` | `JSON value contains a cycle` |
| `stringify` nesting beyond the writer depth limit | `InvalidInput` | `JSON value nests too deeply` |

The exact reader and writer depth limits are yyjson build facts; they are
recorded in Phase 0 and never surface in a message.

### Equality, ordering, and printing

`Value` has **no `==`/`!=` and no ordering**, as a direct consequence of two
existing rules, not a new restriction: Dicts have no equality or ordering, and
an aggregate is comparable only when all recursively compared components are.
A program compares JSON structurally by matching. If Dict equality ever lands,
`Value` gains it with no change to this RFC.

`Value` is not printable. JSON output is written explicitly through
`Json.stringify`; structural debug printing would add a second recursive output
contract and would still need runtime cycle handling. The checker's recursive
printability and equality predicates gain visited-type guards so merely asking
about a recursive aggregate always terminates; `print(value)` and `left ==
right` then produce their ordinary unsupported-operation diagnostics rather
than overflowing the compiler stack.

### Cleanup

`Value` is not inline-only: a parsed tree owns one String per text and one
container per array/object, plus nested trees. `Json.free(heap, value)` is the
one obvious way to release it. Its private traversal keeps an
allocation-identity visited set and releases every distinct owned allocation
at most once, so shared subtrees do not double-free and cycles terminate. The
visited set is internal runtime state allocated from the supplied Heap and is
released before return. After `free`, every external alias into the tree
dangles as with other explicit container cleanup. `stringify` uses equivalent
cycle detection but returns `InvalidInput` / `JSON value contains a cycle`
instead of emitting a partial document.

## Allocation and ownership boundary

The adapter supplies yyjson one custom allocator. Hexal's Heap exposes allocate
and free, not realloc, so the slots cannot simply be assigned to existing
entry points. The adapter implements them as follows:

```text
malloc(size)                 Heap allocation-or-null
realloc(nil, old, new)       malloc(new)
realloc(ptr, old, 0)         free(ptr), return nil
realloc(ptr, old, new)       allocate-or-null(new); on success copy
                              min(old, new), free(ptr), return replacement;
                              on failure leave ptr valid and return nil
free(ptr)                    Heap free
```

This composition uses existing Heap primitives and does not add realloc to the
language or heap runtime. The adapter returns allocation failure to yyjson;
the owning operation then maps it to its settled Hexal failure behavior. This
one choice settles the allocator boundary:

- **No C allocator participates.** Every byte yyjson uses for a read document,
  a mutable document, or a write buffer is a Hexal Heap allocation, released
  through that same allocator on **every** success and failure path —
  including failure mid-translation (key overflow, non-finite float, depth
  overflow). No yyjson buffer is ever reachable as a Hexal value, so there is
  no pointer from another allocator for `Heap.free` to misuse: RFC 0225 holds
  by construction here, and its provenance rule needs no extension.
- **Reallocation preserves the original on failure.** The adapter copies only
  after replacement allocation succeeds, and releases the original only after
  the copy. It never loses the only live allocation on an out-of-memory path.
- **Hexal-visible values are built only by ordinary Hexal allocation
  operations** — heap String construction, `List`, `Dict`. The yyjson write
  buffer is copied into one Hexal `String` and then released through the
  allocator; it is **never** adopted as a `String`, even when the sizes match,
  because adoption would couple String's layout (allocation shape, trailing
  NUL, storage kind) to yyjson's buffer contract. The copy is the boundary.
- `parse` receives a read-only Hexal String; the adapter never mutates or
  writes through it (no in-place read mode), and it holds no reference to the
  input after returning — every `Text` payload owns its bytes.

## Rejected integration choices

- Do not use a system-installed yyjson, pkg-config, or a network fetch.
- Do not vendor a dynamic library or load one at runtime.
- Do not expose `yyjson_*` types, enums, flags, iterators, or error codes in
  Hexal source or in any public generated header.
- Do not expose yyjson's document/DOM as a Hexal value or replace `Value`
  with borrowed yyjson views; a `Value` owns everything it holds.
- Do not add a parse/write options surface: JSONC comments and trailing commas
  are always accepted; relaxed escapes or numbers, single-quoted strings,
  unquoted keys, inf/nan, the rest of JSON5, pretty-printing, indentation,
  byte-order marks, key-order preservation, and caller-tunable depth remain
  unavailable.
- Do not parse from `Slice<Byte>` or raw pointers; text enters through the
  validated `String` boundary.
- Do not accept `String<N>` implicitly; the explicit `copy(heap)` route
  stands.
- Do not add a streaming or incremental reader, a SAX-style callback, or a
  schema/struct decoder; `Value` is the whole model. Typed decode into user
  structs would need machinery Hexal does not have and is a separate future
  specification if it is ever wanted.
- Do not adopt yyjson buffers as Hexal Strings or Slices.
- Do not write a second Hexal JSON parser to reproduce yyjson diagnostics. Use
  the complete yyjson read error, copy its message, and derive location from its
  byte position as specified above.
- Do not truncate a member name or clamp an out-of-range number; allocation
  failure or an invalid number fails the operation.
- Do not change the grammar, Text representation, Dict key rules, shallow-free
  rules, or Error shape.

## Implementation plan

Five phases, ordered so the archive and plumbing are proven before any
language surface depends on them.

### Phase 0 — vendor and qualify the archive

Mirrors exactly what `lib/BUILD.md` and `modules/MIMALLOC.md` record for the
existing dependencies.

1. Add yyjson as a git submodule at `modules/yyjson`, checked out at the
   **0.13.0** release tag. Record the tag, pinned commit, release archive URL,
   byte size, and SHA-256.
2. Confirm the release's source layout against the tree before writing the
   compile command. The command below assumes the expected single translation
   unit (`src/yyjson.c` plus `src/yyjson.h`) and is written from upstream
   documentation, not from a vendored tree — **no part of this RFC has been
   compiled**.
3. Build one archive per shipped pack with that target's exact compiler,
   target, PIC, threading, and archiver identity from `lib/BUILD.md`. Record
   separate Linux and Windows commands; do not present one host command as the
   recipe for both. Both compile commands define
   `YYJSON_DISABLE_FILE=1`, `YYJSON_DISABLE_INCR_READER=1`,
   `YYJSON_DISABLE_UTILS=1`, and the selected finite reader/writer depth
   limits. They must not define `YYJSON_DISABLE_NON_STANDARD`, because JSONC
   needs the comment and trailing-comma readers.

4. Lay out `yyjson_v0.13.0/{include/yyjson.h, yyjson.a, LICENSE}` in **both**
   checked-in packs and append `yyjson` to
   `compiler/specdata/dependencyRegistry` in the same change, as Runtime-pack
   layout requires.
5. Record in `lib/BUILD.md`, in the section shape its existing entries use:
   source commit, compile command, archive size, archive SHA-256, the reader
   and writer depth limits, and the default read/write flag sets actually
   enforced.
6. Add the dependency entry and every new file hash to both
   `manifest.json` files. `format_version` stays 1.
7. Extend the combined native probe in `lib/BUILD.md` to compile, link, and
   run a program using all four archives together, proving no symbol or
   link-order conflict.
8. Verify every yyjson name this RFC relies on against the vendored header:
   the whole-document read entry and options, document release, the custom
   allocator struct and its use by read/write, mutable-document construction
   and container/string/number insertions, compact write with a size/bound
   query, the error struct's code/message/position fields, the value
   type and number subtype enums and their accessors, the object/array
   iterators, the version function, and the `YYJSON_DISABLE_FILE`, default
   flag, and depth-limit macros. A signature that differs from this RFC is a
   **spec correction before the dependent phase starts**, not an
   implementation improvisation.

*Verify:* the four-archive probe runs; `go test ./...` is unaffected because
no compiler code has changed yet.

### Phase 1 — dependency plumbing, with no caller

1. Add the `yyjson` dependency fact through the current dependency-registry
   path in `compiler/specdata/components.go`; do not restore the deleted
   driver-local dependency switch or a second registry.
2. Add the demand flags for the three `std/json` operations to the corelib
   emission state, and a `yyjsonSelected(merged)` predicate beside
   `utf8procSelected` and `libuvSelected` in
   `compiler/generator/runtime_component.go`, true for `parse` or
   `stringify`; append `yyjson` in `compiler/generator/generator.go` where the
   other three dependencies are appended.
3. Extend the native probe in `internal/driver/doctor.go` to include
   `<yyjson.h>` and call the version function verified in Phase 0.

Nothing selects the dependency yet. This phase is complete when a
hand-forced selection materializes the include root and archive, orders them
in the link, and contributes to the build identity.

*Verify:* pure-Go driver tests over a fixture manifest; no generated artifact
moves, so the snippet manifest is unchanged.

### Phase 2 — `Value`, `std/json`, `parse`, and `free`

1. Declare the `Value` canonical type in `compiler/types` under its `std/json`
   identity, with the eight variants and their payload fields; match
   exhaustiveness, position eligibility, and storability follow the existing
   ADT and aggregate rules. Add visited-type guards to the checker predicates
   that decide recursive equality and printability; register `Value` as neither
   equality-comparable nor printable.
2. Add the `std/json` entry to the core-library module table
   (`compiler/corelib/corelib.go`): type `Value`, functions `parse` and
   `free` with their fixed builtin call names, in the shape `std/net` uses.
3. Add the json component as two generated units per Dependency demand: value
   helpers (construct, release, no yyjson include) and the adapter (the only
   `#include <yyjson.h>`). The adapter parses with the custom Heap-backed
   allocator, translates the document into `Value` per the mapping, key, and
   duplicate rules, releases the document on every path, and returns failure
   record whose code, message, and position the call site turns into the owned
   detailed Hexal Error above.
4. Wire `json_free` to the helper unit's visited-allocation traversal, which
   releases aliases/cycles exactly once and selects no yyjson.

*Verify:* `compiler/tests/integration/json_test.go` covering the
compile-time surface cases in Validation, plus generated-C text assertions —
the adapter include is present in exactly one artifact, public headers
contain no `yyjson` spelling, and the dependency list contains `yyjson` for a
`parse` program and not for a `free`-only program. Add catalog snippets for
the facet; rebuild the snippet manifest and review the diff: only the new
snippets' entries appear, no existing hash moves.

### Phase 3 — `stringify`

Add `stringify` to the `std/json` table and the adapter's write path: build a
mutable document from `Value`, write compact, copy the buffer into one heap
String, release every yyjson buffer on success and both failure paths
(non-finite, depth).

*Verify:* the text assertions above extend to `stringify` (write entry and
buffer release present, single copy into a String); the facet's snippets
extend; no artifact outside the json/program family moves.

### Phase 4 — reference synchronization

Update `docs/reference.md`:

- the standard-library module table gains the `std/json` row: exported types
  `Value`, `Member`, module functions `parse`, `stringify`, `free`;
- a new JSON section carries the `Value` declaration, the three signatures and
  their contracts, the number mapping table, member duplicate and ordering
  rules, the fixed error table, the round-trip statement, and the
  equality/printing/cleanup consequences — exactly the surface specified
  above, expressed as rules and tables, no examples beyond what the grammar
  section needs.

The grammar (EBNF) is untouched — this RFC adds no syntax. Verify explicitly
that the Text, Collections, Errors, and Allocation sections need no edit: they
are reused unchanged as `Value`'s component rules.

### Phase 5 — full gate

`go test ./...`, `go vet ./...`, the tagged C23 suite, the snippet manifest
rebuild with a reviewed artifact diff, the native pack probes for every
qualified target, and — per the workbench rule — rebuild the `hexal` binary
and restart the running workbench through `hexal play` before handoff.

## Validation

This section is exhaustive. The object representation is resolved as ordered
`List<Member>` with owning String names, first-occurrence order, in-place
last-value-wins duplicate replacement, and no member-name capacity.

Pack and driver:

- Both shipped pack manifests contain the registry-derived dependency sequence
  with `yyjson` appended, `format_version` 1, with every
  `yyjson_v0.13.0` file hash present; a manifest missing or misordering the
  entry fails the closed-list validation with its existing message.
- The driver rejects a missing, mismatched, corrupt, unlisted, or
  target-incompatible yyjson payload through the existing manifest paths.
- The compiler remains usable with no yyjson installation and performs no host
  discovery or native compilation.
- The combined native probe compiles, links, and runs with all four archives
  together on each qualified target, and `hexal doctor` verifies the new
  payload.

Demand and boundary:

- A program with no `std/json` operation produces byte-identical generated
  artifacts to today, selects no yyjson dependency, and every existing snippet
  keeps its current hash.
- A program using only `Json.free` selects no yyjson: its generated files
  contain no `yyjson.h` include and no `yyjson` symbol.
- A program using `Json.parse` or `Json.stringify` has `yyjson` in its
  dependency list, materializes the include root and archive, and links.
- No public generated header (`hexal.h`, module headers) contains `#include
  <yyjson.h>` or any `yyjson_` spelling; exactly one private adapter artifact
  includes the header.
- Generated C never calls a `yyjson_*` function outside the adapter unit.
- The custom realloc adapter preserves `min(old, new)` bytes on growth and
  shrink, treats nil and zero-size inputs as specified above, leaves the old
  allocation valid when replacement allocation fails, and leaks neither
  allocation on every success/failure branch.

Parsing (runtime behavior, exercised by external C23 fixtures against each
qualified pack):

- Each JSON type parses to its named variant: `null` → `Null`, `true`/`false`
  → `Bool`, strings → `Text`, arrays → `Array`, objects → `Object`, and the
  number table's five rows each produce the stated variant or Error —
  including `9223372036854775807` → `Int`,
  `9223372036854775808` → `UInt`,
  `18446744073709551615` → `UInt`,
  `18446744073709551616` → `Error`,
  `-9223372036854775808` → `Int`, and
  `-9223372036854775809` → `Error`; `-0` normalizes to `Int(0)`.
- `1e400` fails with `InvalidInput` / `JSON number is out of range`; no parse
  path produces NaN, infinity, a truncated integer, or a clamped number.
- String escapes decode to the exact expected UTF-8 bytes:
  `\" \\ \/ \b \f \n \r \t`, `\uXXXX`, and a surrogate pair (`😀`)
  yields one scalar. An escaped NUL (`\u0000`) is accepted and survives
  parse/stringify/free; an unescaped control byte and an unpaired surrogate
  escape fail with yyjson's detailed string error and computed location.
- Nested arrays and objects parse at compiler-configured depth 256, and depth
  257 fails with yyjson's depth detail and computed location; the same
  configured value governs reader, writer, translation, cleanup, and stringify
  traversal.
- JSONC line comments, block comments, and one trailing comma in an object or
  array are accepted, including comments between tokens. An unclosed block
  comment is rejected with yyjson's detailed parse error.
- Every rejected input fails with `InvalidInput` and detailed location: empty
  string, truncated document, trailing non-whitespace content, two
  top-level values, leading BOM, single-quoted string, unquoted member name,
  `NaN`, `Infinity`, extended number/escape/whitespace forms, and every other
  JSON5 feature not shared with JSONC.
- `{"a":1,"a":2}` and `{"a":1,"\u0061":2}` each yield one entry whose
  value is `Int(2)`, proving duplicate identity is tested after escape decoding;
  the member retains its first position and a leak check proves the displaced
  value and duplicate name are released exactly once.
- Empty and member names longer than 256 UTF-8 bytes parse without truncation;
  the fixture checks byte-exact name contents after parsing and stringify.
- Every yyjson read-error code is covered by the error table. Syntax fixtures
  assert its complete copied detail plus computed one-based line and byte
  column and absolute byte position; CRLF, multibyte UTF-8 before the error,
  first-byte failure, and end-of-input failure pin location calculation.

Stringify (same fixture gate):

- Output is compact: exact bytes asserted for scalar, array, and object
  documents; no insignificant whitespace, no trailing newline. Object member
  order matches the stored List order.
- Output is valid UTF-8 and non-ASCII characters are emitted unescaped: a
  string containing `é` and an emoji round-trips byte-exactly through
  parse → stringify → parse.
- `Json.Float` of NaN or +/- infinity fails with `InvalidInput` /
  `JSON cannot represent a non-finite number`; a tree nested beyond configured
  depth 256 fails with `InvalidInput` /
  `JSON value nests too deeply`; neither failure leaks any yyjson buffer.
- Numeric round-trip by value holds: `Int` and `UInt` across their full
  64-bit ranges and `Float(2.5)` survive parse → stringify → parse unchanged
  in value.

Compile-time surface (pure Go, `compiler/tests/integration/json_test.go`):

- The `Value` declaration, variant construction with named payload fields,
  payload read through the narrowed match scrutinee, and container insertion
  compile; a type-mode match over `Value` without `else` is accepted, and one
  missing a variant is rejected with the existing exhaustiveness diagnostic.
- `==` between two `Value`s is rejected with the existing
  no-Dict-equality-derived diagnostic; no new diagnostic is introduced for it.
- Passing `String<N>` to `parse` is rejected with the existing
  no-implicit-conversion diagnostic naming `copy(heap)`.
- `Value` is accepted as a Dict value, List element, function result, and
  Task argument, and rejected as a Dict key with the existing key-type
  diagnostic.
- `print(value)` and `left == right` are rejected without recursive checker
  overflow, including when the object representation and arrays both lead back
  to `Value`.
- Generated-C text assertions: the adapter include placement, the public-header
  absence of `yyjson`, and the per-operation dependency-list contents stated
  under Demand and boundary.

Cleanup:

- A leak check over a deeply nested parsed tree — strings at several depths,
  nested arrays, nested objects, duplicate members — proves `Json.free`
  releases every allocation `parse` created, on the success path and on each
  in-translation failure path.
- User-built shared subtrees and direct/indirect container cycles terminate;
  `Json.free` releases every distinct allocation once, while `stringify`
  returns `InvalidInput` / `JSON value contains a cycle` without leaking.
- `Json.free` on each scalar variant performs no release and traps nowhere.
- A user-built value deeper than `JSONMaxDepth` passed to `Json.free` traps with
  `[Runtime Error] JSON value nests too deeply`; cleanup does not traverse past
  the configured bound.
- No yyjson-allocated pointer is ever reachable as a Hexal value or passed to
  `Heap.free`; the custom allocator is the only allocator yyjson uses.

Conformance:

- The ordinary Go suite passes with no C toolchain installed, as today; the
  runtime rows above are verified by external C23 fixtures that compile,
  link, and run against each qualified static archive, and by hand before
  those fixtures are runnable.
- The snippet manifest rebuild moves only entries for the newly added
  snippets; every pre-existing hash is unchanged, and the artifact diff shows
  movement only in the components `std/json` actually selects.

## Implementation state

The object representation is ordered `List<Member>` with owning String names;
duplicate names replace the prior value in place. Phase 0's source, archive,
ABI, signature, and dependency evidence is recorded in the runtime-pack files.
Windows and Linux/WSL pack validation and doctor checks pass. Ordinary Go
tests, vet, build, the generated-snippet manifest, the full tagged C23 suite,
and the JSON ownership LeakSanitizer fixtures pass. No implementation or
validation item remains open.
