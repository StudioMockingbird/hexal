# RFC 0233: JSON Support via yyjson

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation ready. The release is pinned at **yyjson 0.13.0**, the
  dependency boundary matches the shipped runtime-pack contract, the value
  model and language surface are settled, and the Validation section is
  exhaustive. Implementation has not started. Everything upstream-facing —
  source layout, API signatures, flag defaults, digests — is produced by
  Phase 0 and listed under Open implementation inputs
- Created: 2026-09-22
- Updated: 2026-09-22
- Origin: requested language-level JSON support by integrating yyjson 0.13.0
- Depends on: RFC 0052 (C backend), RFC 0055 (build and runtime-pack inputs),
  the current target-profile matrix, and the current String, List, Dict, Heap,
  and Error contracts in `docs/reference.md`
- Coordinates with: RFC 0223 (Dict correctness), RFC 0225 (cross-allocator
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

Parsing is strict RFC 8259 with no extensions. yyjson runs entirely on the
caller's Heap through one custom allocator, so no C `malloc` participates and
the cross-allocator hazard RFC 0225 exists to reject cannot arise. Every
failure reports as a Hexal-owned `Error` with a fixed, allocation-free
message; yyjson's codes, English strings, line, and position never cross the
boundary.

## Why this dependency

Correct JSON is not a weekend parser. It requires, together:

- the complete RFC 8259 grammar with its rejection set (comments, trailing
  commas, single quotes, unquoted keys, `NaN`/`Infinity`, trailing content);
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
of the value model (`Value` is a Hexal ADT over Hexal List, Dict, and String,
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

The archive is built with the pack's existing build identity (Clang 23.1.1,
`-O2 -DNDEBUG -fPIC -pthread`, target-portable x86-64, no `-march=native`,
C dialect C11), plus exactly one compile definition:

```text
-DYYJSON_DISABLE_FILE=1
```

`YYJSON_DISABLE_FILE` is a 0.13.0 compile-time option that removes yyjson's
`FILE*`/path read and write APIs. Hexal owns file IO through `std/fs`; that
surface is unreachable from `Value` and only widens the archive and the
`stdio.h` dependency, so it is compiled out. No other option is set:

```text
YYJSON_FREESTANDING   unset; the adapter uses libc-backed formatting
YYJSON_DISABLE_UTF8_VALIDATION
                      unset; input validation stays on as defense in depth,
                      including over yyjson's own escape-decoding output
read/write flags      defaults only; the strict sets, no JSON5/extension flag
```

Whether the default flag set rejects every rejected input this RFC names is a
Phase 0 verification item, not an assumption.

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

**The closed dependency list grows atomically with the packs.**
`validateRuntimeManifest` (`internal/driver/runpack.go`) enforces the ordered
list `libuv, mimalloc, utf8proc` against every manifest the driver loads, and
the driver loads a manifest whenever a program demands any dependency. Adding
`yyjson` to that list therefore fails manifest validation for *every*
dependency-demanding program on any pack that lacks the entry. Both checked-in
packs (`x86_64-linux-gnu` and `x86_64-windows-gnu-ucrt`) gain their yyjson
entry, with real archives and hashes, in the same change that extends the
list to `libuv, mimalloc, utf8proc, yyjson`.

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

## Language surface

### The `Value` type

`std/json` exports one type, declared in the surface's own syntax:

```text
type Value is union
    | Null
    | Bool as value: Bool end
    | Int as value: Int64 end
    | UInt as value: UInt64 end
    | Float as value: Float64 end
    | Text as value: String end
    | Array as items: List<Value> end
    | Object as entries: Dict<String<256>, Value> end
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
- Recursion is through the `List` and `Dict` handles, which are pointer-sized,
  so the representation is finite; direct by-value recursion stays rejected by
  the existing rule. `Text`'s payload is a heap `String`; `Array` and `Object`
  own their containers. `Null`, `Bool`, `Int`, `UInt`, and `Float` own nothing.
- `Value` follows general storability: bindings, parameters and results, ADT
  payloads, Array/Slice/List elements, Dict values, Task arguments and
  results, Channel elements. It is not Dict-key eligible — keys are `Int32` or
  `String<N>`, and that rule is unchanged.
- `Value` is compiler-owned as a core-library export: no user module declares
  its variants or methods, and no instance methods exist — the whole surface
  is the three module functions below.

Baseline confirmed against the tree on 2026-09-22 by compiling through
`compiler.Compile`: this declaration shape (unit and record variants, recursion
through `List<Value>` and `Dict<String<256>, Value>`, a `String` payload),
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
  whitespace, and nothing else. The heap argument evaluates first and exactly
  once, as everywhere else. The whole document is read into memory; there is
  no streaming or incremental form. The parameter is heap `String` — the same
  shape `std/net.parse_address` takes — so a `String<N>` caller copies first
  through the explicit `copy(heap)` route the no-implicit-conversion rule
  already names.
- `stringify` emits compact JSON: no insignificant whitespace, no trailing
  newline, UTF-8 bytes with non-ASCII characters unescaped (JSON escaping of
  quotes, backslash, and control characters still applies). Member order of an
  object is unspecified in both directions — Dict iteration order is
  unspecified — and duplicate spellings never occur in output because parsed
  objects have already collapsed them.
- `free` recursively releases the whole tree through the ordinary shallow
  rules: `Text` frees its String; `Array` frees each element's tree then the
  List storage; `Object` frees each entry's value tree then the Dict storage
  (keys are inline `String<256>` and own nothing); scalar variants free
  nothing. Exactly-once per distinct allocation applies as for every container:
  an aliased subtree must not be freed twice, and freeing dangles the handle.
  No yyjson call is involved.

Round-trip contract, stated once:

```text
parse(stringify(v)) is equal to v by JSON data value
```

- Integers in either 64-bit range round-trip exactly, `Float` values round-trip
  through shortest-round-trip spelling, and text round-trips byte-exactly.
- Byte-exact document round-trip is **not** promised: whitespace, member
  order, duplicate members, and number spelling are not preserved
  (`1e2` parses to `Float` and may re-emit as `100`; an integral `Float` or a
  `UInt` within `Int64` reparses as `Int`). Round-trip is by value, never by
  variant identity.

### Number mapping

JSON has one number type; `Value` splits it to keep integers exact:

| JSON input | `Value` variant |
| --- | --- |
| integer in `[-2^63, 2^63)` | `Int` |
| integer in `[2^63, 2^64)` | `UInt` |
| integer below `-2^63` or at/above `2^64` | `Float` |
| fraction or exponent part present (`1.0`, `1e2`, `-0.5`) | `Float` |
| outside `Float64` finite range (`1e400`) | `Error`, see Errors |

"integer" means no fraction and no exponent part. On output, `Int` and `UInt`
emit exact decimal digits and `Float` emits yyjson's shortest round-trip form.
Parsing never truncates, never clamps, never produces NaN or infinity, and
never narrows a 64-bit integer into a narrower variant.

### Objects: member names and duplicates

- Member names are `String<256>` Dict keys: a **256-byte** capacity, counted
  in UTF-8 bytes, not runes. A name of 1..256 bytes is accepted whatever its
  encoding; a longer name fails the whole `parse` with
  `ResourceExhausted` / `JSON object key exceeds capacity` and is never
  truncated. The capacity exists because a Dict stores its keys inline and a
  heap `String` is not a Dict key; 256 bytes matches the existing Error
  message capacity and holds practical keys. It is a documented contract of
  `parse`, not a silent limit.
- Duplicate member names collapse with **last occurrence wins** — `Dict.insert`
  replaces. Translation releases each displaced earlier value exactly once, so
  duplicates do not leak.

### Errors

yyjson reports failures with a code, an English message, a line, and a
position. None of them crosses the boundary. The adapter returns a failure
code to the generated corelib call site, which builds the `Error` exactly as
every other capability operation does. Every message below is a fixed,
allocation-free, Hexal-owned literal within the `String<256>` bound, and none
embeds a native number:

| Condition | `ErrorKind` | Message |
| --- | --- | --- |
| syntax error, empty input, truncated document, trailing non-whitespace content, more than one top-level value, comment, trailing comma, single-quoted or unquoted member name, `NaN`/`Infinity` literal, leading BOM, unpaired surrogate escape, nesting beyond the reader depth limit | `InvalidInput` | `invalid JSON text` |
| object member name exceeds 256 bytes | `ResourceExhausted` | `JSON object key exceeds capacity` |
| number outside `Float64` finite range | `InvalidInput` | `JSON number is out of range` |
| `stringify` of NaN or +/- infinity | `InvalidInput` | `JSON cannot represent a non-finite number` |
| `stringify` nesting beyond the writer depth limit | `InvalidInput` | `JSON value nests too deeply` |

The exact reader and writer depth limits are yyjson build facts; they are
recorded in Phase 0 and never surface in a message.

### Equality, ordering, and printing

`Value` has **no `==`/`!=` and no ordering**, as a direct consequence of two
existing rules, not a new restriction: Dicts have no equality or ordering, and
an aggregate is comparable only when all recursively compared components are.
A program compares JSON structurally by matching. If Dict equality ever lands,
`Value` gains it with no change to this RFC.

Printing follows the existing contract unchanged: every component of `Value`
is printable, so a `Value` prints in the structural forms (record-variant,
list, dict spellings already defined for `print`). This is distinct from
`stringify`, which is the JSON serialization.

### Cleanup

`Value` is not inline-only: a parsed tree owns one String per text, one List
per array, one Dict per object, plus nested trees. `Json.free(heap, value)` is
the one obvious way to release it and the only cleanup operation this RFC
adds; per-node hand-rolled recursion would re-derive exactly the shallow-free
rules the language already states for containers. After `free` the handle
dangles, aliases of the tree become invalid, and nothing diagnoses a double
free — the same contract every owning type has.

## Allocation and ownership boundary

The adapter supplies yyjson one custom allocator whose `malloc`, `realloc`,
and `free` slots are the default Heap's existing entry points (the
mimalloc-backed primitives the heap component already owns). This one choice
settles the whole boundary:

- **No C allocator participates.** Every byte yyjson uses for a read document,
  a mutable document, or a write buffer is a Hexal Heap allocation, released
  through that same allocator on **every** success and failure path —
  including failure mid-translation (key overflow, non-finite float, depth
  overflow). No yyjson buffer is ever reachable as a Hexal value, so there is
  no pointer from another allocator for `Heap.free` to misuse: RFC 0225 holds
  by construction here, and its provenance rule needs no extension.
- **Allocation failure behaves like every Heap allocation.** The adapter's
  allocation slots trap with the standard `[Runtime Error] heap allocation
  failed` rather than inventing an allocator-specific error path, matching
  the contract `Heap.allocate` and heap text construction already have.
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
- Do not add a parse/write options surface: no JSON5, comments, trailing
  commas, relaxed escapes or numbers, unquoted keys, inf/nan writing,
  pretty-printing, indentation, byte-order marks, key-order preservation, or
  caller-tunable depth — every option is a future spec, not a flag parameter.
- Do not parse from `Slice<Byte>` or raw pointers; text enters through the
  validated `String` boundary.
- Do not accept `String<N>` implicitly; the explicit `copy(heap)` route
  stands.
- Do not add a streaming or incremental reader, a SAX-style callback, or a
  schema/struct decoder; `Value` is the whole model. Typed decode into user
  structs would need machinery Hexal does not have and is a separate future
  specification if it is ever wanted.
- Do not adopt yyjson buffers as Hexal Strings or Slices.
- Do not put yyjson's error codes, English messages, line, or position into
  any `Error`, diagnostic, or message.
- Do not truncate an over-capacity member name or clamp an out-of-range
  number; both fail the operation.
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
3. Build one archive per shipped pack with the build identity `lib/BUILD.md`
   states:

   ```text
   clang -std=c11 -O2 -DNDEBUG -fPIC -pthread -DYYJSON_DISABLE_FILE=1 \
     -I modules/yyjson/src -c modules/yyjson/src/yyjson.c -o yyjson.o
   ar rcs yyjson_v0.13.0/yyjson.a yyjson.o
   ```

4. Lay out `yyjson_v0.13.0/{include/yyjson.h, yyjson.a, LICENSE}` in **both**
   checked-in packs and extend `validateRuntimeManifest`'s ordered list to
   `libuv, mimalloc, utf8proc, yyjson` in the same change, as Runtime-pack
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
   query, the error struct's code/message/line/position fields, the value
   type and number subtype enums and their accessors, the object/array
   iterators, the version function, and the `YYJSON_DISABLE_FILE`, default
   flag, and depth-limit macros. A signature that differs from this RFC is a
   **spec correction before the dependent phase starts**, not an
   implementation improvisation.

*Verify:* the four-archive probe runs; `go test ./...` is unaffected because
no compiler code has changed yet.

### Phase 1 — dependency plumbing, with no caller

1. Add `RuntimeYyjson RuntimeDependency = "yyjson"` to
   `compiler/runtime_dependency.go` and to the `switch` in
   `runtimeDependencies`, which panics on unknown names.
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
   exhaustiveness, position eligibility, storability, and the no-equality
   consequence fall out of the existing ADT, Dict, and aggregate rules and
   need no special case beyond registering the type.
2. Add the `std/json` entry to the core-library module table
   (`compiler/corelib/corelib.go`): type `Value`, functions `parse` and
   `free` with their fixed builtin call names, in the shape `std/net` uses.
3. Add the json component as two generated units per Dependency demand: value
   helpers (construct, release, no yyjson include) and the adapter (the only
   `#include <yyjson.h>`). The adapter parses with the custom Heap-backed
   allocator, translates the document into `Value` per the mapping, key, and
   duplicate rules, releases the document on every path, and returns failure
   codes the call site turns into the fixed Hexal Errors.
4. Wire `json_free` to the helper unit's recursion, which selects no yyjson.

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

- the standard-library module table gains the `std/json` row: exported type
  `Value`, module functions `parse`, `stringify`, `free`;
- a new JSON section carries the `Value` declaration, the three signatures and
  their contracts, the number mapping table, the member-name capacity and
  duplicate rule, the fixed error table, the round-trip statement, and the
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

This section is exhaustive.

Pack and driver:

- Both shipped pack manifests declare four dependencies in the order `libuv,
  mimalloc, utf8proc, yyjson`, `format_version` 1, with every
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

Parsing (runtime behavior, exercised by external C23 fixtures against each
qualified pack):

- Each JSON type parses to its named variant: `null` → `Null`, `true`/`false`
  → `Bool`, strings → `Text`, arrays → `Array`, objects → `Object`, and the
  number table's five rows each produce the stated variant or Error —
  including `9223372036854775807` → `Int`,
  `9223372036854775808` → `UInt`,
  `18446744073709551615` → `UInt`,
  `18446744073709551616` → `Float`,
  `-9223372036854775808` → `Int`, and
  `-9223372036854775809` → `Float`.
- `1e400` fails with `InvalidInput` / `JSON number is out of range`; no parse
  path produces NaN, infinity, a truncated integer, or a clamped number.
- String escapes decode to the exact expected UTF-8 bytes:
  `\" \\ \/ \b \f \n \r \t`, `\uXXXX`, and a surrogate pair (`😀`)
  yields one scalar, while an unpaired surrogate escape fails as
  `invalid JSON text`.
- Nested arrays and objects parse to the tested depth, and one level beyond
  the Phase-0-recorded reader limit fails as `invalid JSON text`.
- Every rejected input fails with `InvalidInput` / `invalid JSON text`: empty
  string, truncated document, trailing non-whitespace content, two
  top-level values, leading BOM, `-- comment`, trailing comma, single-quoted
  string, unquoted member name, `NaN`, `Infinity`.
- `{"a":1,"a":2}` yields one entry whose value is `Int(2)`, and a leak check
  proves the displaced value was released exactly once.
- A 256-byte member name parses; a 257-byte name fails with
  `ResourceExhausted` / `JSON object key exceeds capacity`, leaves no partial
  value, and truncates nothing (bytes, not runes, decide acceptance).
- Every `Error` produced carries exactly the fixed message and kind from the
  error table; no yyjson code, English text, line, position, or number
  appears in any field.

Stringify (same fixture gate):

- Output is compact: exact bytes asserted for documents with deterministic
  single-member objects (for example `{"a":[1,2],"b":null}` built in member
  order), no insignificant whitespace, no trailing newline; multi-member
  objects are asserted by content because member order is unspecified.
- Output is valid UTF-8 and non-ASCII characters are emitted unescaped: a
  string containing `é` and an emoji round-trips byte-exactly through
  parse → stringify → parse.
- `Json.Float` of NaN or +/- infinity fails with `InvalidInput` /
  `JSON cannot represent a non-finite number`; a tree nested beyond the
  Phase-0-recorded writer limit fails with `InvalidInput` /
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
- A program that `print`s a `Value` compiles.
- Generated-C text assertions: the adapter include placement, the public-header
  absence of `yyjson`, and the per-operation dependency-list contents stated
  under Demand and boundary.

Cleanup:

- A leak check over a deeply nested parsed tree — strings at several depths,
  nested arrays, nested objects, duplicate members — proves `Json.free`
  releases every allocation `parse` created, on the success path and on each
  in-translation failure path (key overflow mid-document releases what was
  already built).
- `Json.free` on each scalar variant performs no release and traps nowhere.
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

## Open implementation inputs

Everything design-level is settled. What remains is produced by Phase 0:

- the exact source file list, confirmed against the 0.13.0 release archive
  rather than assumed from documentation;
- the release archive URL, byte size, SHA-256, and pinned commit, per target
  pack;
- the archive and probe evidence: symbol and ABI results from the
  four-archive probe, plus generated-C size, link time, and pack-size deltas;
- the recorded reader/writer depth limits and the default read/write flag
  sets, each confirmed to reject exactly the rejected-input list this RFC
  names (comments, trailing commas, single quotes, unquoted keys, extension
  numbers, trailing content, BOM) — a default that accepts one of them is a
  spec correction before Phase 2;
- confirmation of yyjson's handling of the two remaining number edges —
  integers between `2^63` and `2^64`, and out-of-`Float64`-range literals —
  against the value-based mapping table; and
- every API signature listed in Phase 0 step 8, checked against the vendored
  `yyjson.h`.

No open input permits system discovery, dynamic loading, public yyjson types,
an options surface, implicit conversion, truncation, clamping, or a change to
current text, dictionary, cleanup, or error contracts.

## Implementation readiness

**Ready.** The release is pinned, the dependency boundary reuses the shipped
runtime-pack contract rather than inventing one, the value model is settled
across all eight variants and three functions, the Validation section is
exhaustive, and the plan is phased so the archive and the plumbing prove
themselves before any surface depends on them.

Baseline confirmed against the tree on 2026-09-22: the `Value` declaration
shape, construction, container use, and exhaustive match compile through
`compiler.Compile`, so this RFC specifies against the language as it actually
is.

Start at Phase 0. It is the only phase that cannot be done from the
specification alone, because it produces the archive every later phase links
against and the header every unverified signature in this document gets
checked against. Two things an implementer should hold onto:

- **Phase 1 is the cheap proof.** Dependency plumbing moves no artifact; if
  the snippet manifest or any existing hash moves there, the predicate is
  wired to the wrong demand.
- **The allocator decision is load-bearing.** Every ownership, leak, and
  cross-allocator item in Validation follows from running yyjson on the
  caller's Heap through one custom allocator; an implementation that lets
  yyjson reach the C allocator has broken the RFC's central contract, not
  just a test.
