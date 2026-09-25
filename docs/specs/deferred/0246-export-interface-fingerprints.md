# RFC 0246: Export Interface Fingerprints

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; design recorded, implementation not scheduled. Promote and
  revalidate this proposal when incremental compilation needs the metadata
- Created: 2026-09-25
- Origin: RFC 0241 finding T4, split from RFC 0244 so measured-policy cleanup
  can land without unused fingerprint machinery
- Depends on: the current checked module registry and the current
  `CompilationResult` API
- Coordinates with: RFC 0241, RFC 0244 (measured policy), deferred RFC 0232
  (which will consume interface fingerprints when incremental compilation is
  designed), and deferred RFC 0164 (object caching, which remains separate)
- Does not change: Hexal syntax or semantics, module visibility, generated C,
  `docs/reference.md`, the string-in/string-out compiler boundary, or whether
  compilation is incremental today

## Summary

Make each successfully checked module produce a deterministic fingerprint of
the exported semantic interface its importers consume. This is metadata for a
future incremental driver: it does not add a cache, retain compiler state, skip
checking, or inspect the filesystem.

## Goals

- Produce one self-contained semantic-interface fingerprint per reachable
  source module.
- Make implementation-only, whitespace, comment, and private edits that leave
  the checked exported interface unchanged preserve a module's fingerprint.
- Make every change visible to an importer change the appropriate
  fingerprint.
- Keep the result deterministic across source-map insertion order and process
  runs.
- Expose the fingerprints without making the compiler stateful.

## Non-goals

- Incremental parsing, checking, specialization, or generation.
- A persistent cache, watch mode, filesystem driver, or object-cache policy.
- Reverse-dependency invalidation. RFC 0232 will consume these fingerprints
  when it defines snapshots and invalidation.
- A user-authored declaration file or emitted `.d.hex` artifact.
- Hashing generated headers as a substitute for semantic interfaces.
- Promising that the fingerprint format is stable across schema versions.
- Changing compiler policy values; RFC 0244 owns their rationale cleanup.

## Exported-interface fingerprints

### Public result

`CompilationResult` gains:

```go
// ExportFingerprints maps every reachable source module's exact logical key
// to the lowercase full SHA-256 of its checked exported semantic interface.
// It is non-nil on success and failure; failure returns an empty map.
ExportFingerprints map[string]string
```

The map includes:

- the entry module, even when it exports nothing;
- ordinary imported Hexal modules;
- reachable embedded source-standard-library modules; and
- prepared C-binding modules that join the source module graph.

Compiler-owned core libraries have no source `ModuleNode` and therefore no
entry. Keys are the graph's recorded `LogicalKey`; callers never reconstruct
them from canonical module identities.

This remains string-in/string-out compilation. The additional strings are
compiler metadata, not files, host paths, or retained state.

This is a fingerprint of the **checked Hexal interface**, not of native input
bytes. A foreign record includes the checked C spelling, header identity, and
layout facts below, but editing a header without changing those checked facts
cannot change this digest. An eventual build cache must track the actual C
headers, native sources, objects, and toolchain inputs separately; this digest
must never be treated as their replacement.

### Production point

The checker computes a module's fingerprint only after:

1. the module checks without diagnostics;
2. its trailing export block resolves;
3. export flags and registry records are complete; and
4. exported-interface closure validation succeeds.

It computes the interface before importer-requested generic specialization
records are assembled. Specialization demand is a property of the current
program, not part of the defining module's declared interface.

### Why excluding generic bodies is sound

Hexal monomorphizes: `docs/reference.md:667` states that only reachable
concrete specializations emit C and that there is no erasure. That invites an
obvious objection — if an importer's output contains a specialized copy of the
exporter's generic body, then excluding that body from the exporter's
fingerprint would let an invalidation walk skip an importer that genuinely
needs regenerating.

Probed, and the objection does not hold. With `lib.hex` exporting
`fun twice<T>(v: T): Int32` and `app.hex` calling `L.twice<Int32>(21)`,
changing only the generic's body changes exactly one artifact:

```text
ARTIFACT CHANGED: modules/lib.c
   new body line: return 222;
```

`modules/app.c` is byte-identical. The specialization is emitted into the
**defining** module's artifact even though the importer is what demanded it, so
a generic body change never reaches a dependent's output. The exporter's own
artifact changes and is rebuilt on artifact identity, which is a separate
mechanism from this fingerprint.

The two exclusions therefore compose correctly: bodies are excluded because
they are not interface, and specialization demand is excluded because it
belongs to the program rather than the module. Neither omission can make an
invalidation walk skip work it owed.

`checker.Program` carries the completed fingerprint to `compiler.Compile`.
The compile entrypoint gathers the map in graph order after all checking
succeeds. Every failure path, including panic recovery, returns a non-nil empty
map and no partial fingerprints.

### Canonical stream

The digest input is a versioned, length-delimited byte stream. It begins with:

```text
hexal-export-interface-v1
target profile
canonical module identity
```

Every field is written as an unsigned varint byte length followed by its exact
bytes. Each section writes its record count, and each record writes a kind tag
and its field count before its fields; an empty field is distinct from an
absent field. No JSON, Go formatting, pointer identity, map iteration, or
delimiter ambiguity enters the stream. Changing the encoding contract
increments the schema name and deliberately changes every fingerprint.

Export records are sorted by `(kind, fully qualified export key)`. A method's
key includes its receiver's canonical identity and method name; a bare method
name is not a total key when two receivers export the same name. Export-block
order does not matter. Declaration order is retained only where the language
makes it observable: object member layout, ADT variant ordinals, ADT payload
layout, and function parameter order.

The full lowercase SHA-256 is returned. A truncated digest is not sufficient
for a cache identity.

### Export records

The stream records every interface an importer can resolve:

| Record | Included facts |
| --- | --- |
| transparent alias | exported name and normalized target type |
| nominal struct | exported name, nominal identity, ordered member names, mutability, and normalized member types |
| ADT | exported name, nominal identity, ordered variant names, and ordered payload member names/types |
| function | exported name, ordered parameter types, rest marker, and optional result type |
| method | exported receiver identity, method name, ordered parameter types, rest marker, and optional result type |
| module value | exported name and normalized type |
| foreign function | local name, exact C symbol, header identity/form, ordered Hexal parameter/result types, and exact boundary C spellings |
| foreign constant | local name, exact C spelling, header identity/form, and normalized type |
| foreign global | the foreign-constant facts plus mutability |
| foreign record | local and C names, header identity/form, completeness, and complete member layout when present |
| generic type | exported name, parameter arity, and normalized alias/object/ADT target shape |
| generic function | exported name, parameter arity, normalized parameter/rest/result types; body excluded |
| generic method | exported receiver template, receiver and method arities, name, normalized parameter/rest/result types; body excluded |

The ordinary encoder consumes the checker registry's completed export records.
Open-generic records are **not yet completed interface records**: their target,
parameter, and result expressions are parser nodes, and the current
declaration-time open checks resolve placeholder types only temporarily before
restoring the generic-check snapshot. Implementation must retain an immutable,
normalized interface shape when each open generic type, function, or method
checks successfully. Capture the checked placeholder target/signature before
the temporary state is restored, normalize type parameters by ordinal, and
retain no body or specialization request in that shape. The encoder consumes
these retained checked shapes; it neither serializes parser syntax nor
re-checks a template during hashing. A failed open check publishes no shape
and cannot produce a successful fingerprint.

### The record table needs a guard, not a promise

A fingerprint that silently omits an exportable kind is wrong in the one way
nothing can detect: the digest is stable, well-formed, and incomplete. The
sensitivity cases below catch a fact the author thought of and missed; they
cannot catch a fact nobody listed. This matters more here than for most
features because **the fingerprint has no consumer yet**, so an omission
produces no symptom until an incremental driver ships and skips work it owed.

The exportable surface is enumerable from the checker rather than from this
table. `applyExportFlags` stamps ordinary checked declarations, but is **not**
a complete enumeration source: type names and open generic templates do not
all receive an `Exported` field there. The production anchors are
`moduleEntry`'s export-bearing maps in `compiler/checker/modules.go`, populated
by `registerExports` and `registerGenerics` and gated by `entry.exports` at
lookup. The current carriers are `functions`, `types`, `methods`,
`moduleValues`, `foreignFunctions`, `foreignConstants`, `foreignGlobals`,
`foreignRecords`, `genericFunctions`, `genericTypes`, and `genericMethods`.
`types` also holds a foreign-record lookup alias; that does not create a
second interface record beside `foreignRecords`.

So the encoder carries a guard in archived RFC 0238's established pattern.
A test reads the actual `moduleEntry` map fields and the assignment arms in
`registerExports` and `registerGenerics` with Go's syntax-tree facilities,
then compares their export-bearing field names with the field names traversed
by the encoder. It explicitly excludes the registry's import, export-flag,
defining-context, and concrete-specialization bookkeeping fields; an added
field must be classified rather than silently ignored. Within `types` and
`genericTypes`, focused cases cover alias, object, ADT, and foreign-record
forms, and unknown checked type forms fail closed instead of falling back to
a display name. Do not satisfy the guard with two manually maintained copies
of the record table. Demonstrate that adding a registration arm without an
encoder arm, and adding an encoder arm without a registrable kind, each fails
the guard with the missing field or kind named.

### Type normalization

The type encoder is semantic and recursive:

- scalars and compiler-owned leaf types use their canonical identities;
- constructed types record their constructor and normalized arguments;
- union members use canonical order, not written order;
- pointer pointee writability, Slice writability, array length, String
  capacity, function rest status, and every other identity-bearing parameter
  are included;
- generic parameters are encoded by ordinal (`$0`, `$1`, ...), so renaming
  `T` to `Element` does not change the interface;
- every reachable source nominal or foreign record is collected into a
  definition table keyed by its canonical identity: source objects and ADTs
  use module-qualified keys, while foreign records use their target-qualified
  C identity, shared across modules;
- interface records refer to nominal definitions by that key;
- the definition table is emitted in key order and contains object members or
  ADT variants recursively; and
- recursive nominal graphs terminate through key references rather than
  recursive re-emission.

Collecting the reachable nominal-definition closure is important. If module B
exports a function involving module A's exported `Point`, changing `Point`'s
member layout must also change B's interface fingerprint; an eventual
invalidation walk may then safely stop where a fingerprint is unchanged.

Source display names never replace canonical identities. Two same-named types
from different modules remain different, while two declarations of the same
C record on one target share its one canonical identity.

### Included and excluded changes

Fingerprint-changing examples:

```hexal
// Parameter type changes.
fun parse(value: Int32): Bool do ... end
fun parse(value: Int64): Bool do ... end

// Public layout changes.
type Point is struct
    x: Int32,
end

type Point is struct
    x: Int32,
    y: Int32,
end
```

Fingerprint-preserving examples:

```hexal
// Function body only.
return value * 2
return value + value

// Generic parameter spelling only.
fun identity<T>(value: T): T do ... end
fun identity<Value>(value: Value): Value do ... end
```

The fingerprint excludes:

- comments, whitespace, source spans, and logical line numbers;
- private declarations that do not alter a normalized exported type;
- function and method bodies;
- module-value initializers when the checked value type remains unchanged;
- parameter names;
- export-block ordering;
- generated C names derived entirely from semantic identity;
- generated helper demand;
- concrete generic specializations requested by this compilation; and
- generated artifact content.

Transparent aliases are normalized through their targets, regardless of
whether the alias name is exported. For example, changing a private
`type Local is Int32` to `type Local is Int64` changes the fingerprint when an
exported `fun reveal(value: Local): Local` exposes that target; private status
does not make a public signature change invisible. A current compiler probe
emits `int32_t hex_f_m3_app_reveal(int32_t)` for the first form and
`int64_t hex_f_m3_app_reveal(int64_t)` for the second.

The same rule applies to an inferred exported module value: a probe changing
`let signal = true` to `let signal = b'A'` emits `extern const bool
hex_v_m9_constants_signal;` versus `extern const uint8_t
hex_v_m9_constants_signal;`. The initializer is not itself a fingerprint
field, but the checked exported type it determines is.

The target profile is included in the stream because foreign ABI checking is
target-dependent. The fingerprint alone is never a complete persistent cache
key: an eventual cache must additionally key its own schema, compiler version,
project settings, source identity, dependency graph, and any other input RFC
0232 defines.

## Failure and compatibility behavior

- Success returns one fingerprint for every graph module and no extra entry.
- Any configuration, reachability, lexing, parsing, checking, or generation
  failure returns a non-nil empty fingerprint map. Partial checked interfaces
  are not observable.
- The metadata does not affect generated files or dependency discovery.
- Existing callers that ignore the new struct field continue unchanged.
- Fingerprints compare only within the same schema and target profile. They are
  opaque strings to callers.

## Required sweep

Implementation must review:

- every `CompilationResult` construction, including panic recovery and
  `failureResult`, so the new map is always non-nil;
- checker module registration and exported-interface closure ordering;
- generic template records, ensuring bodies and specialization demand do not
  leak into the stream;
- all exported ordinary and foreign declaration families listed above.

Do not add a cache, session, declaration-file emitter, filesystem lookup, or
hash of generated headers during this sweep.

## Future implementation acceptance

This deferred proposal authorizes no implementation. On promotion, recheck
these cases against the then-current compiler and turn them into an exhaustive
Validation section. No fingerprint may be consumed for incremental skipping
before that gate passes.

Result contract:

- A successful single-module compilation returns a non-nil map containing
  exactly `app.hex`; a module with no exports still has a fingerprint.
- A successful multi-module compilation returns exactly one entry for every
  reachable source module, keyed by its recorded logical key, independent of
  source-map insertion order.
- A source-map entry unreachable from the root receives no fingerprint.
- A compilation using a reachable embedded source-stdlib or prepared C binding
  includes that module; compiler-owned core libraries do not.
- Every ordinary failure path and the panic-recovery seam return a non-nil
  empty map.
- Every value is a 64-character lowercase hexadecimal SHA-256.

Stability:

- Recompiling identical inputs in separate calls produces identical maps.
- Comments, whitespace, source positions, export-list order, private
  declarations that leave exported normalized types unchanged, function/method
  bodies, parameter names, and module-value initializer changes that preserve
  the checked value type preserve the affected module's fingerprint.
- Changing a private transparent alias used in an exported signature from
  `Int32` to `Int64` changes the defining module's fingerprint.
- Reordering the input source map preserves every fingerprint.
- Reordering independent exported methods with the same bare name on distinct
  receivers preserves the fingerprint; changing either receiver's identity or
  method signature does not.
- Renaming a generic parameter while preserving its ordinal use preserves the
  fingerprint.

Completeness:

- Every exportable kind the checker can register has an encoder arm, and every
  encoder arm corresponds to a registrable kind. Adding an exportable kind
  without an encoder arm fails this guard, naming the kind.
- A generic body-only change in an exporting module leaves every importing
  module's generated artifact byte-identical, which is the property that makes
  excluding bodies sound rather than merely convenient.

Sensitivity:

- Adding or removing an export changes the defining module's fingerprint.
- Function and method parameter, rest, or result changes change it.
- Alias target changes change it.
- Object member name, order, mutability, or type changes change it.
- ADT variant name/order or payload member name/order/type changes change it.
- Module-value type changes change it.
- For an exported inferred module value, changing its initializer so its
  inferred type changes also changes the fingerprint; the initializer's
  *value* alone is not an interface fact.
- Foreign header form/payload, C symbol/spelling, boundary type, record
  completeness/layout, or global mutability changes change it.
- Generic arity, public signature, alias target, object layout, or ADT layout
  changes change it even when no concrete specialization is requested;
  generic body-only changes do not.
- A reachable imported nominal type's public layout change changes the
  fingerprint of an exported interface that exposes that type.
- A reachable foreign record's checked completeness or member layout changes
  the fingerprint of an exported interface that exposes that record, even
  when another module declared the same C record identity first.
- The same sources checked under two distinct target profiles produce distinct
  fingerprints.
- Changing external C header bytes without changing the prepared checked
  interface does not, by itself, change this fingerprint; native-input
  invalidation remains the later driver's responsibility.

Regression boundary:

- Generated artifacts are byte-identical and the existing snippet manifest
  changes no hash.
- No external tool runs in ordinary tests.
- `go test ./...`, `go vet ./...`, `gofmt -l`, and `git diff --check` pass.
- `docs/reference.md` is reviewed and intentionally unchanged because this is
  compiler metadata, not a language contract.

## Future implementation plan

### Phase 1: checked generic shapes and canonical interface encoder

1. Preserve each open generic's checked, placeholder-normalized interface
   shape during its declaration-time check, before its temporary state is
   restored. Do not reuse parser expressions as the fingerprint record.
2. Add one checker-owned encoder in a focused file such as
   `compiler/checker/interface_fingerprint.go`.
3. Implement length-prefixed field writing and the versioned stream prefix.
4. Implement normalized type references and the sorted reachable nominal
   definition table, with cycle-safe key references.
5. Implement the ordinary, generic, module-value, and foreign export record
   families.
6. Add the two-way registered-kind/encoder-arm guard and prove both failure
   directions against deliberately missing arms.
7. Add focused checker tests for ordering, recursion, same-named nominal types,
   generic-parameter normalization, and every record family.

### Phase 2: checker production point

1. Add the fingerprint field to `checker.Program`.
2. Produce the fingerprint only after export registration and closure
   validation succeed, before generic specializations are assembled.
3. Prove a body-only generic change and different specialization demand do not
   affect it.
4. Prove imported reachable nominal definitions participate transitively.

### Phase 3: public result

1. Add `ExportFingerprints map[string]string` to `CompilationResult`.
2. Gather successful module fingerprints using `graph.Order` and each node's
   recorded `LogicalKey`.
3. Initialize the map in success, ordinary failure, and panic recovery.
4. Add exported-API integration tests for membership, stability, sensitivity,
   target separation, and unreachable sources.

### Phase 4: conformance and handoff

1. Revalidate and run the promoted RFC's exhaustive Validation matrix.
2. Confirm the snippet manifest is byte-identical.
3. Review `docs/reference.md` and record that no edit is required.
4. Coordinate with RFC 0232 as the eventual consumer; keep its cache and
   invalidation design outside this fingerprint contract.
5. Mark the promoted RFC closed only when every gate passes.

## Implementation readiness

Deferred. The public field, production point, encoding ownership, schema
version, included interface facts, exclusions, and failure behavior are
recorded, but no current compiler consumer needs this metadata. Before
implementation, revalidate the plan and acceptance cases against the compiler
and the incremental build design that makes fingerprints useful.
