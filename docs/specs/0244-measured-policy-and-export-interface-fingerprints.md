# RFC 0244: Measured Policy and Export Interface Fingerprints

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; design settled, implementation not started
- Created: 2026-09-24
- Origin: RFC 0241 findings T2 and T4, selected as the two highest-ROI
  TypeScript learnings to implement next
- Depends on: the current checked module registry and the current
  `CompilationResult` API
- Coordinates with: RFC 0241, deferred RFC 0232 (which will consume interface
  fingerprints when incremental compilation is designed), deferred RFC 0164
  (object caching, which remains separate), `AGENTS.md`
- Does not change: Hexal syntax or semantics, module visibility, generated C,
  `docs/reference.md`, the string-in/string-out compiler boundary, or whether
  compilation is incremental today

## Summary

Implement two TypeScript lessons that have high value without importing its
compiler complexity:

1. require measured heuristics to carry their evidence beside the policy they
   justify; and
2. make each successfully checked module produce a deterministic fingerprint
   of the exported semantic interface its importers consume.

The first is an immediate maintenance rule. The second is metadata for a
future incremental driver: it does not add a cache, retain compiler state, skip
checking, or inspect the filesystem.

## Goals

- Keep performance claims and tuned thresholds reproducible after their
  originating spec is archived.
- Remove unsupported performance language from current compiler policy rather
  than invent measurements after the fact.
- Produce one self-contained semantic-interface fingerprint per reachable
  source module.
- Make implementation-only, private, whitespace, and comment edits preserve a
  module's fingerprint.
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
- Changing any policy value while documenting why it exists.

## Part A: measured compiler policy

### Rule

When measurement selects a non-obvious threshold, ordering, capacity, or
heuristic, its adjacent CARE rationale records:

1. what workload or corpus was measured;
2. the alternatives compared;
3. the figures that selected the current policy; and
4. the condition that requires remeasurement.

Example:

```go
// Four workers minimized the 95th-percentile build time on the recorded
// multi-module corpus: 1=840ms, 2=510ms, 4=370ms, 8=430ms. Remeasure when
// the corpus or checker ownership model changes materially.
const checkerWorkers = 4
```

This rule does not require noise beside values selected for other reasons:

- language or ABI contracts;
- standard-defined constants;
- exact platform requirements;
- safety bounds chosen from a proof;
- resource ceilings selected conservatively rather than through tuning; or
- obvious collection sizes and loop mechanics.

Those values still need an honest Contract, Architecture, Rationale, or Edge
comment when their names do not fully explain them, but they must not pretend
to have benchmark evidence.

### Current-policy audit

Implementation performs a focused audit of `compiler/config/config.go`, the
one compiler-owned home for shared tunable policy. Each declaration is
classified without changing its value:

| Policy | Classification | Required action |
| --- | --- | --- |
| `RuntimeABIVersion` | generated/runtime contract | Keep the contract comment |
| `PageSizeBytes` | platform/layout requirement | Keep the platform rationale |
| `MaxSyntaxDepth` | recursive-stack safety bound | State headroom rationale; do not claim benchmark tuning |
| `MaxInterpolationDepth` | lexer/parser consistency bound | Keep its relationship to syntax depth explicit |
| `ForeignInspectionByteLimit` | conservative hostile-output ceiling | State that it is an initial resource ceiling, not a measured optimum |
| `ForeignInspectionTimeout` | conservative external-process ceiling | State the same and name the qualification boundary |
| task stack reserve/commit | public runtime policy plus platform constraint | Keep the contract and backend distinction |
| `MaxInlineStringCapacity` | language/runtime storage ceiling | Remove the unsupported claim that one page is where inline storage stops being cheaper; state only the bounded-value/stack-layout policy the value actually enforces |
| Error capacities | public runtime representation | Keep the representation contract |

`AGENTS.md` gains the general rule above under CARE or Simplify so future
agents do not bury measured policy only in an expiring spec.

No benchmark is fabricated to defend an existing value. If a later change
wants a different value for performance, that change measures it and records
the results.

## Part B: exported-interface fingerprints

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

Every field is written as its byte length followed by its exact bytes. No JSON,
Go formatting, pointer identity, map iteration, or delimiter ambiguity enters
the stream. Changing the encoding contract increments the schema name and
deliberately changes every fingerprint.

Export records are sorted by `(kind, exported source name)`. Export-block order
does not matter. Declaration order is retained only where the language makes
it observable: object member layout, ADT variant ordinals, ADT payload layout,
and function parameter order.

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

The encoder consumes the checker registry's completed export records and the
existing open-generic records. It does not reparse source and does not print an
AST back into text.

### The record table needs a guard, not a promise

A fingerprint that silently omits an exportable kind is wrong in the one way
nothing can detect: the digest is stable, well-formed, and incomplete. The
sensitivity cases below catch a fact the author thought of and missed; they
cannot catch a fact nobody listed. This matters more here than for most
features because **the fingerprint has no consumer yet**, so an omission
produces no symptom until an incremental driver ships and skips work it owed.

The exportable surface is enumerable from the checker rather than from this
table. `applyExportFlags` in `compiler/checker/modules.go:293` switches over
`FunctionDeclaration` and `MethodDeclaration` and then walks `ModuleValues`,
`ForeignFunctions`, `ForeignConstants`, `ForeignGlobals`, and `ForeignRecords`;
nominal types and generics reach importers through the registry's own export
tables.

So the encoder carries a guard in archived RFC 0238's established pattern:
every exportable kind the checker can register has an encoder arm, and every
encoder arm corresponds to a kind the checker can register. A new exportable
kind added later fails that guard instead of silently falling out of every
fingerprint. This is the same mechanism that pins the ErrorKind tags and the
component include literals, applied to the one surface where a gap is
undetectable by construction.

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
- every reachable nominal type is collected into a definition table keyed by
  its module-qualified canonical identity;
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
from different modules remain different.

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
- private declarations;
- function and method bodies;
- module-value initializers;
- parameter names;
- export-block ordering;
- generated C names derived entirely from semantic identity;
- generated helper demand;
- concrete generic specializations requested by this compilation; and
- generated artifact content.

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
- all exported ordinary and foreign declaration families listed above;
- `compiler/config/config.go` comments under Part A's classification; and
- `AGENTS.md` CARE guidance.

Do not add a cache, session, declaration-file emitter, filesystem lookup, or
hash of generated headers during this sweep.

## Validation

This section is exhaustive.

Measured policy:

- `AGENTS.md` states the four-part evidence rule for empirically selected
  policy and distinguishes empirical tuning from contracts, platform facts,
  proofs, and conservative ceilings.
- Every declaration in `compiler/config/config.go` has an accurate
  classification comment; no comment invents benchmark evidence.
- `MaxInlineStringCapacity` no longer claims that one page is an empirically
  proven cost crossover.
- No policy value changes under Part A.

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
  declarations, function/method bodies, parameter names, and module-value
  initializer changes preserve the affected module's fingerprint.
- Reordering the input source map preserves every fingerprint.
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
- Foreign header form/payload, C symbol/spelling, boundary type, record
  completeness/layout, or global mutability changes change it.
- Generic arity, public signature, alias target, object layout, or ADT layout
  changes change it; generic body-only changes do not.
- A reachable imported nominal type's public layout change changes the
  fingerprint of an exported interface that exposes that type.
- The same sources checked under two distinct target profiles produce distinct
  fingerprints.

Regression boundary:

- Generated artifacts are byte-identical and the existing snippet manifest
  changes no hash.
- No external tool runs in ordinary tests.
- `go test ./...`, `go vet ./...`, `gofmt -l`, and `git diff --check` pass.
- `docs/reference.md` is reviewed and intentionally unchanged because this is
  compiler metadata, not a language contract.

## Implementation plan

### Phase 1: measured-policy rule

1. Add the four-part empirical-policy requirement to `AGENTS.md`.
2. Audit every declaration in `compiler/config/config.go` against Part A's
   classification table.
3. Correct unsupported or ambiguous rationale without changing a value.
4. Run the ordinary suite and prove generated artifacts did not move.

### Phase 2: canonical interface encoder

1. Add one checker-owned encoder in a focused file such as
   `compiler/checker/interface_fingerprint.go`.
2. Implement length-prefixed field writing and the versioned stream prefix.
3. Implement normalized type references and the sorted reachable nominal
   definition table, with cycle-safe key references.
4. Implement the ordinary, generic, module-value, and foreign export record
   families.
5. Add focused checker tests for ordering, recursion, same-named nominal types,
   generic-parameter normalization, and every record family.

### Phase 3: checker production point

1. Add the fingerprint field to `checker.Program`.
2. Produce it only after export registration and closure validation succeed,
   before generic specializations are assembled.
3. Prove a body-only generic change and different specialization demand do not
   affect it.
4. Prove imported reachable nominal definitions participate transitively.

### Phase 4: public result

1. Add `ExportFingerprints map[string]string` to `CompilationResult`.
2. Gather successful module fingerprints using `graph.Order` and each node's
   recorded `LogicalKey`.
3. Initialize the map in success, ordinary failure, and panic recovery.
4. Add exported-API integration tests for membership, stability, sensitivity,
   target separation, and unreachable sources.

### Phase 5: conformance and handoff

1. Run the exhaustive Validation matrix.
2. Confirm the snippet manifest is byte-identical.
3. Review `docs/reference.md` and record that no edit is required.
4. Update RFC 0232 to consume, rather than redefine, the fingerprint contract.
5. Mark this RFC closed and remove its `docs/status.md` row only when every
   gate passes.

## Implementation readiness

Implementation-ready. The public field, production point, encoding ownership,
schema/version rule, included interface facts, exclusions, failure behavior,
tests, and future incremental boundary are all specified. No cache or
persistent state decision is required to implement this RFC.
