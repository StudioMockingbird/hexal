# RFC 0098: Reject Duplicate Union Members

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented
- Created: 2026-08-20
- Scope: reject source union expressions that repeat one canonical member
- Coordinates with: the checker, `docs/reference.md`, `docs/status.md`
- Does not change: structural union identity, canonical member ordering, union
  representation, or generated C for any accepted program

## Summary

A union type expression must name each canonical member exactly once.

```hexal
x: Int32 | Nil | Int32 := 0   -- rejected: Int32 is repeated
y: Int32 | Nil | Nil := nil   -- rejected: Nil is repeated
```

The compiler currently accepts both forms by silently removing the repeated
member during type resolution. That hides a source error and contradicts the
rule that a union is written as a set of distinct alternatives.

Equivalent union expressions in different places remain one structural type:

```hexal
x: Int32 | Nil := 0
y: Int32 | Nil := nil
z: Nil | Int32 := 0
```

All three bindings use the same canonical `Int32 | Nil` type. Reusing a union
is not duplication; repeating a member inside one union expression is.

## Evidence

Focused full-pipeline probe:

```text
source:      x: Int32 | Nil | Int32 := 0
exit:        0
diagnostics: []
```

The checker currently resolves written members, skips any candidate equal to a
candidate already seen, and constructs the union from the remaining members.
The type arena then sorts and interns that normalized member set. Normalization
is required for identity; silently repairing the source is not.

## Semantics

- Duplicate detection occurs after name lookup, transparent-alias resolution,
  generic substitution, and nested-union flattening.
- Two members duplicate one another when their resolved canonical types are
  `Equal`; source spelling and written order do not make them distinct.
- The later source member that introduces an already-present canonical member
  is the diagnostic location.
- Duplicate detection is fail-fast and precedes the post-normalization member
  count. `Int32 | Int32` therefore reports only
  `union member Int32 appears more than once`; it does not also report that the
  normalized union has fewer than two members.
- If one alias or nested union introduces several members, the first canonical
  overlap in written expansion order owns the diagnostic.
- A generic union whose members are distinct while open remains valid. A
  specialization that makes two members equal is rejected at specialization;
  it does not collapse into a smaller union.
- After a source union passes duplicate validation, its distinct members are
  flattened, canonically ordered, and interned exactly as they are today.
- Compiler-synthesized unions may continue to normalize member sets internally.
  This RFC changes validation of source type expressions, not the arena's
  ability to combine already-checked result types.

Examples of canonical duplication:

```hexal
type Score = Int32

x: Int32 | Nil | Score := 0
-- Score resolves to Int32, so the final member is a duplicate.
```

```hexal
type MaybeScore = Int32 | Nil

x: Bool | MaybeScore | Int32 := true
-- MaybeScore already contributes Int32, so the final member is a duplicate.
```

These are not duplicates:

```hexal
module Math = import "./math"
module UI = import "./ui"

type Choice = Math.Point | UI.Point
-- Nominal ownership makes these distinct canonical types.
```

## Diagnostic

Exact message:

```text
union member <canonical-type-name> appears more than once
```

Examples:

```text
union member Int32 appears more than once
union member Nil appears more than once
```

The diagnostic points at the later written member expression. When an alias or
nested union introduces the overlap, it points at that alias or nested union
expression while the message names the duplicated canonical member.

## Implementation

- In source union resolution, retain the canonical members already introduced
  by each written member expression.
- Before appending a resolved candidate, compare it with the retained members.
- Report the duplicate diagnostic instead of skipping an equal candidate.
- Return immediately after that diagnostic. Do not report both a duplicate and
  a derived member-count error for one source expression.
- Keep candidate order for contextual literal checking only after the union has
  passed duplicate validation.
- Delete the source-resolution branch that reports
  `a union requires at least two distinct members; ... has one`: after
  fail-fast duplicate rejection, a parsed source union cannot reach that state.
- Delete the subsequent `len(members) == 1` passthrough, which is unreachable
  for the same reason. Preserve `Environment.UnionType`'s arity validation for
  compiler-synthesized and direct type-layer construction.
- Remove or rewrite tests that describe duplicate removal as accepted source
  normalization. Preserve arena-level normalization tests for
  compiler-synthesized member sets.

## Invariants

1. One written union expression cannot contain the same canonical member more
   than once.
2. Member order does not affect structural union identity.
3. Separate equivalent union expressions share one interned type.
4. Accepted programs generate byte-identical C before and after this RFC.
5. Duplicate rejection belongs to the checker; the parser continues to
   preserve written member expressions and pipe locations.

## Validation

This section is exhaustive.

- `Int32 | Nil | Int32` is rejected at the final `Int32` with
  `union member Int32 appears more than once`.
- `Int32 | Nil | Nil` is rejected at the final `Nil` with
  `union member Nil appears more than once`.
- `Int32 | Int32` is rejected at the second `Int32` with only
  `union member Int32 appears more than once`; the former distinct-member-count
  diagnostic is not also emitted.
- A repeated member is rejected even when at least two other distinct members
  remain.
- A transparent alias of an earlier member is rejected as a duplicate and the
  message names the canonical member.
- An outer union that overlaps a member supplied by a union alias or nested
  union is rejected.
- A generic specialization that substitutes two written members to one
  canonical type is rejected rather than collapsed.
- `Int32 | Nil`, a second `Int32 | Nil`, and `Nil | Int32` are accepted, share
  one canonical type and one generated C definition, and use the same C name.
- A type declaration whose union has distinct nominal members with the same
  short name, such as `type Choice = Math.Point | UI.Point`, remains accepted.
- The snippet manifest is byte-identical because no accepted program's output
  changes.
- `docs/reference.md` replaces duplicate removal as a source rule with exact
  duplicate rejection while retaining flattened, duplicate-free canonical
  identity.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Changing union C names, tag names, payload names, or representation.
- Treating equivalent union expressions in separate declarations as distinct
  nominal types.
- Rejecting compiler-synthesized attempts to combine a member already present
  in a result union; internal construction may normalize those sets.
