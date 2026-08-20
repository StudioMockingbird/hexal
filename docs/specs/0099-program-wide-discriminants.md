# RFC 0099: Program-Wide Discriminants

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; naming decision required, implementation blocked on RFC 0095
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
typedef enum Hex_Tag {
    hex_tag_Int32,
    hex_tag_Nil,
    hex_tag_m4_math_Shape_Circle,
    hex_tag_m4_math_Shape_Square,
} Hex_Tag;
```

A general union uses the shared tag and an unnamed payload-union type:

```c
typedef struct Hex_t_Int32_Nil {
    Hex_Tag tag;
    union {
        int32_t Int32;
    } payload;
} Hex_t_Int32_Nil;
```

An ADT uses the same tag type and keeps its existing nested payload structure:

```c
typedef struct Hex_t_m4_math_Shape {
    Hex_Tag tag;
    union {
        struct {
            int32_t radius;
        } Circle;
    } payload;
} Hex_t_m4_math_Shape;
```

Nil, EoS, and payload-free ADT variants have discriminants and no payload
fields.

## Why the registry is program-wide

Tagged values cross module boundaries. Per-module implicit enum values can
disagree when modules reach different member or variant sets. A producer could
emit tag zero while a consumer interprets zero as another state.

`Hex_Tag` is emitted once in `hexal.h`, which every module header includes.
Every generated translation unit therefore sees one enum type, one constant
set, and one numeric assignment.

The core compiler already receives the complete source map and emits the
complete generated project. No filesystem or incremental-build mechanism is
introduced.

## Discriminant identities

Two disjoint identity classes share `Hex_Tag`:

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
- Emit `Hex_Tag` in `hexal.h` before every module-header use.
- Omit `Hex_Tag` when no reachable general union or ADT exists.
- Never derive ordering from map iteration, discovery order, source position,
  or process-global identity serials.

## Names

- `Hex_Tag` is a C type and follows the generated type naming convention chosen
  before implementation.
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
  for the first, and append `_2`, `_3`, and so on.

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

- Replace each per-union tag field with `Hex_Tag tag`.
- Remove every per-union `_tag` enum typedef.
- Remove every per-union `_payload` union typedef.
- Keep `payload` as a named member whose type is an unnamed union.
- Name payload fields by the canonical member-label rule in this RFC's Names
  section.
- Emit no payload field for Nil or EoS.
- Preserve checked tag-plus-inline-payload semantics.
- Do not promise byte-for-byte `sizeof` stability against the old enum
  representation. The complete generated program uses one representation, and
  Hexal exposes no stable foreign ABI for structural unions.

Injection:

```c
Hex_t_Int32_Nil score = {
    .tag = hex_tag_Int32,
    .payload.Int32 = 13,
};
```

Nil:

```c
Hex_t_Int32_Nil score = {
    .tag = hex_tag_Nil,
};
```

Union equality, truthiness, narrowing, matching, `try`, concurrency results,
and widening use canonical type discriminants. Widening copies the source tag
directly instead of translating union-local ordinals; payload movement remains
explicit because source and destination are different C structs.

## ADT representation

- Replace each per-ADT tag field with `Hex_Tag tag`.
- Remove every per-ADT tag enum typedef.
- Keep the wrapper struct, nested payload union, nested variant structs,
  variant order, member order, and field spelling.
- ADT construction, matching, equality, and printing use the canonical variant
  discriminant.
- A payload-free variant remains tag-only.

Construction:

```c
Hex_t_m4_math_Shape shape = {
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
- `Hex_Tag` is generated implementation detail, not a supported foreign ABI.

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

## Validation

This section is exhaustive.

- A program without a general tagged union or ADT emits no `Hex_Tag` or
  `hex_tag_` identifier.
- `Int32 | Nil` emits one `Hex_Tag` definition in `hexal.h` containing exactly
  `hex_tag_Int32` and `hex_tag_Nil`; its wrapper uses `Hex_Tag tag`.
- The union wrapper contains an unnamed payload-union type and emits no
  `<union>_tag` or `<union>_payload` typedef.
- Nil and EoS receive constants and no payload fields.
- `Int32 | Nil` and `Int32 | Error` together emit `hex_tag_Int32` once.
- A union returned from an imported module uses the same constants in producer
  and consumer translation units.
- Rune and UInt32 receive distinct constants.
- Same-named nominal types from two modules receive distinct owner-qualified
  constants.
- Transparent aliases share one constant with their canonical target.
- A pointer-plus-Nil null-niche union alone emits no registry.
- Injection, narrowing, equality, truthiness, match, `try`, concurrency result,
  and widening output contain no per-union ordinal tag names.
- Widening assigns the source tag directly and copies the active payload.
- An exported ADT used in its defining module and an importer contributes each
  variant once; both headers use `Hex_Tag tag` and emit no ADT-local enum.
- Same-named ADTs and variants from two modules receive distinct
  owner-qualified constants.
- ADT construction, matching, equality, and printing use shared variant
  constants; payload-free variants emit no payload member.
- `Hex_Tag` precedes every generated use and is emitted exactly once.
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
- Exposing `Hex_Tag` as a C interoperability ABI.
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
