# RFC 0099: Program-Wide Discriminants

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed
- Created: 2026-08-20
- Updated: 2026-08-20
- Scope: replace per-union and per-ADT tag enums with one demand-generated
  program-wide discriminant registry; inline each union's payload storage
- Depends on: RFC 0095 for injective canonical generated identities
- Coordinates with: the generator, `hexal.h`, union and ADT lowering,
  `docs/reference.md`, `docs/status.md`, the snippet manifest
- Does not change: Hexal syntax, union or ADT identity, active-member
  semantics, pointer-null niches, payload ownership, or accepted programs

## Summary

Every general union currently emits a private tag enum, private payload union,
and wrapper struct. Every ADT emits another private tag enum. Exported values
cause those definitions to be repeated in producer and consumer headers.

One program-wide registry is sufficient because each discriminant represents
either one canonical union-member type or one canonical ADT variant:

```c
typedef enum hex_tag {
    hex_tag_Int32,
    hex_tag_Nil,
    hex_tag_m4_math_Shape_Circle,
    hex_tag_m4_math_Shape_Square,
} hex_tag;
```

A general union uses the shared tag and an unnamed payload-union type:

```c
typedef struct hex_t_Int32_Nil {
    hex_tag tag;
    union {
        int32_t hex_m_Int32;
    } payload;
} hex_t_Int32_Nil;
```

An ADT uses the same tag type and keeps its existing nested payload structure:

```c
typedef struct hex_t_m4_math_Shape {
    hex_tag tag;
    union {
        struct {
            int32_t radius;
        } Circle;
    } payload;
} hex_t_m4_math_Shape;
```

Nil, EoS, and payload-free ADT variants have discriminants and no payload
fields.

## Why the registry is program-wide

Tagged values cross module boundaries. Today, every module re-emitting one ADT
or union receives its complete canonical variant or member list, so the
duplicated implicit enum values are identical. A focused producer/consumer
probe emitted the same anonymous `Shape` tag enum in both headers. There is no
current tag-number miscompile.

The disagreement is a latent hazard: any later per-module reachability filter
for members or variants would let producer and consumer ordinals diverge. This
RFC does not claim that filter exists. Its immediate benefits are one generated
owner, removal of repeated tag and payload helper typedefs, and one uniform
discriminant identity across unions and ADTs. Establishing the program-wide
registry also makes later reachability filtering unable to create a tag-number
mismatch.

`hex_tag` is emitted once in `hexal.h`, which every module header includes.
Every generated translation unit therefore sees one enum type, one constant
set, and one numeric assignment.

The core compiler already receives the complete source map and emits the
complete generated project. No filesystem or incremental-build mechanism is
introduced.

## Discriminant identities

Two disjoint identity classes share `hex_tag`:

- `type:<canonical-type-key>` for a member of a reachable general structural
  union;
- `variant:<canonical-adt-key>:<variant-name>` for a variant of a reachable
  concrete ADT.

Consequences:

- A canonical type repeated across several unions contributes one constant.
- Transparent aliases share the canonical target's constant.
- Rune and UInt32 receive distinct constants despite sharing `uint32_t`.
- An ADT used as a union member has a type discriminant distinct from every
  internal variant discriminant.
- Re-emitting one exported ADT in several module headers contributes each
  variant once.
- Same-named types, ADTs, and variants in distinct modules remain distinct
  through canonical module ownership.

## Discovery and emission

- Discover all canonical members of all reachable general tagged unions.
- Discover all variants of all reachable concrete ADTs.
- Include tag-only union members and ADT variants.
- Exclude pointer-plus-Nil unions using the null-pointer niche; include either
  member if another reachable general union uses it.
- Deduplicate by discriminant identity, then sort by stable canonical key.
- Emit `hex_tag` in `hexal.h` before every module-header use.
- Omit `hex_tag` when no reachable general union or ADT exists.
- Never derive ordering from map iteration, discovery order, source position,
  or process-global identity serials.

### Generator structure

- Add one program-owned discriminant state containing records keyed by the two
  identities above, their stable canonical sort keys, readable bases, and
  final collision-resolved C names.
- Discover and finalize the complete registry before rendering `hexal.h` or
  any module pair.
- Pass the sorted records through the typed `hexal.h` render model; the
  `generator/packages/hexal.h` template owns the enum's C presentation.
- Expose one lookup from canonical member type or canonical ADT variant to its
  finalized constant. Every construction, match, equality, truthiness,
  narrowing, widening, `try`, print, and concurrency lowering uses that
  lookup.
- A missing lookup is an internal generation error. Never reconstruct a tag
  spelling locally or fall back to an ordinal.
- Delete `unionTagName`, `adtTagName`, and the family-local enum/payload-typedef
  emitters after all call sites use the registry. Keep one payload-label lookup
  derived from the finalized member constant.

## Names

- The registry type is exactly `hex_tag`. RFC 0096 discarded the capitalized
  `Hex_*` convention; this RFC follows the existing lowercase generated-C
  convention and does not add a redundant `_t` suffix.
- `hex_tag_...` is an enum value and follows the lowercase `hex_` convention.
- A union-member label describes canonical Hexal type identity, not C
  representation.
- A nominal label contains canonical module ownership; import aliases never
  participate.
- An ADT-variant label is
  `<canonical-owner>_<adt-name>_<variant-name>`.
- Constructed type labels include their constructor and canonical arguments.
- Sanitize the readable base as a C identifier. If distinct identities still
  request one base, sort the collision group by canonical key, keep the base
  for the first, and append `_0`, `_1`, and so on. This is the same collision
  suffix convention as RFC 0095's program-wide definition-name registry.
- A union payload field is `hex_m_` plus its member's assigned `hex_tag_`
  suffix. The enum constant and payload field therefore use the same
  collision-resolved label, while the unconditional member prefix prevents C
  keywords and source spellings from becoming bare field identifiers.

Examples:

```c
hex_tag_Int32
hex_tag_Nil
hex_tag_Rune
hex_tag_UInt32
hex_tag_m4_math_Point
hex_tag_List_Int32
hex_tag_m4_math_Shape_Circle
```

The final owner and constructed-type fragments follow RFC 0095's settled
injective naming contract.

## Union representation

- Replace each per-union tag field with `hex_tag tag`.
- Remove every per-union `_tag` enum typedef.
- Remove every per-union `_payload` union typedef.
- Keep `payload` as a named member whose type is an unnamed union.
- Name payload fields by the canonical member-label rule in this RFC's Names
  section, including its unconditional `hex_m_` prefix.
- Emit no payload field for Nil or EoS.
- Preserve checked tag-plus-inline-payload semantics.
- Do not promise byte-for-byte `sizeof` stability against the old enum
  representation. The complete generated program uses one representation, and
  Hexal exposes no stable foreign ABI for structural unions.

Injection:

```c
hex_t_Int32_Nil score = {
    .tag = hex_tag_Int32,
    .payload.hex_m_Int32 = 13,
};
```

Nil:

```c
hex_t_Int32_Nil score = {
    .tag = hex_tag_Nil,
};
```

Union equality, truthiness, narrowing, matching, `try`, concurrency results,
and widening use canonical type discriminants. Widening copies the source tag
directly instead of translating union-local ordinals; payload movement remains
explicit because source and destination are different C structs.

## ADT representation

- Replace each per-ADT tag field with `hex_tag tag`.
- Remove every per-ADT tag enum typedef.
- Keep the wrapper struct, nested payload union, nested variant structs,
  variant order, member order, and field spelling.
- ADT construction, matching, equality, and printing use the canonical variant
  discriminant.
- A payload-free variant remains tag-only.

Construction:

```c
hex_t_m4_math_Shape shape = {
    .tag = hex_tag_m4_math_Shape_Circle,
    .payload.Circle = { .radius = 4 },
};
```

Impossible-tag defaults retain their existing fail-closed behavior.

## Determinism and ABI boundary

- Two compilations of identical sources emit byte-identical names, order, and
  numeric values.
- Adding a reachable discriminant may renumber later implicit enum values.
  This is safe because the compiler emits the complete program as one unit.
- Stable explicit numeric IDs belong to future incremental-build or C ABI work.
- `hex_tag` is generated implementation detail, not a supported foreign ABI.

## Invariants

1. One canonical union-member type has one discriminant per program.
2. One canonical ADT variant has one discriminant per program.
3. Every generated translation unit observes identical numeric values.
4. Discriminant names are injective on canonical identity.
5. Aliases share the canonical target's union-member discriminant.
6. Every reachable general-union member and ADT variant has an entry.
7. Nil, EoS, and payload-free variants remain tag-only.
8. Pointer-null niche unions remain untagged.
9. No per-union tag/payload typedef or per-ADT tag typedef survives.
10. The registry type is exactly `hex_tag`; no `Hex_Tag`, `hex_tag_t`, or
    family-local tag typedef is emitted.
11. Every union payload field begins with `hex_m_` and uses the finalized
    collision-resolved member label.

## Validation

This section is exhaustive.

- A program without a general tagged union or ADT emits no `hex_tag` or
  `hex_tag_` identifier.
- `Int32 | Nil` emits one `hex_tag` definition in `hexal.h` containing exactly
  `hex_tag_Int32` and `hex_tag_Nil`; its wrapper uses `hex_tag tag`.
- The union wrapper contains an unnamed payload-union type and emits no
  `<union>_tag` or `<union>_payload` typedef.
- Nil and EoS receive constants and no payload fields.
- `Int32 | Nil` and `Int32 | Error` together emit `hex_tag_Int32` once.
- A union returned from an imported module uses the same constants in producer
  and consumer translation units.
- Rune and UInt32 receive distinct constants.
- Same-named nominal types from two modules receive distinct owner-qualified
  constants.
- A nominal type and an ADT variant whose readable bases sanitize to the same
  label receive two deterministic collision-resolved constants; their union
  payload labels remain consistent with the assigned constants. The first
  sorted identity keeps the base and the second uses `_0` (not `_2`).
- A lowercase nominal source name matching a C keyword still produces a valid
  owner-qualified `hex_tag_` constant and `hex_m_` payload field; no payload
  field is a bare source or sanitized label.
- Transparent aliases share one constant with their canonical target.
- A pointer-plus-Nil null-niche union alone emits no registry.
- Injection, narrowing, equality, truthiness, match, `try`, concurrency result,
  and widening output contain no per-union ordinal tag names.
- A generator-level inventory finds no tag spelling assembled outside the
  program registry lookup, and a deliberately incomplete registry fails
  generation instead of emitting a fallback name.
- Widening assigns the source tag directly and copies the active payload.
- An exported ADT used in its defining module and an importer contributes each
  variant once; both headers use `hex_tag tag` and emit no ADT-local enum.
- Same-named ADTs and variants from two modules receive distinct
  owner-qualified constants.
- ADT construction, matching, equality, and printing use shared variant
  constants; payload-free variants emit no payload member.
- `hex_tag` precedes every generated use and is emitted exactly once.
- Generated output contains no `Hex_Tag` or `hex_tag_t` identifier.
- Generated-C text assertions cover declaration order, designated
  initializers, payload access, and producer/consumer header use for the union
  and ADT cases above. Ordinary validation does not invoke GCC or Clang;
  actual compilation remains under the repository's existing generated-C
  coverage gap.
- Two compilations of identical sources produce byte-identical files.
- `docs/reference.md` records the registry, both identity classes, demand
  generation, union payload structure, null-niche exception, and lack of a
  stable external numeric-tag ABI.
- The snippet manifest moves only for snippets containing a reachable general
  union or ADT; review `hexal.h`, module-header, and module-C movement
  separately.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Stable numeric values across changed programs.
- Exposing `hex_tag` as a C interoperability ABI.
- Giving aliases distinct runtime tags.
- Adding tags to pointer-null niche unions.
- Changing payload ownership, copying, equality, truthiness, ADT variant order,
  or language semantics.
- Filesystem access, caching, watching, or incremental compilation.

## Drawbacks

- `hexal.h` gains one compact enum whenever a general union or ADT is reachable.
- Adding one reachable discriminant can renumber later values, requiring a
  complete rebuild. That matches the current compiler boundary.
- A shared tag field can hold a constant invalid for one particular wrapper.
  Generated code remains responsible for constructing valid states, as it is
  today for ordinal tags and payloads.
