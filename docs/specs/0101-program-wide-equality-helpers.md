# RFC 0101: Program-Wide Equality Helpers

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; design settled, implementation blocked on RFC 0100
- Created: 2026-08-20
- Scope: emit equality helpers for program-owned concrete types once while
  retaining helpers for module-owned complete types in module headers
- Depends on: RFC 0100 moving String equality into the String component
- Coordinates with: component partitioning, generator equality discovery,
  `docs/reference.md`, `docs/status.md`, the snippet manifest
- Does not change: equality semantics, type eligibility, evaluation order,
  representation, or accepted programs

## Summary

Equality discovery currently mixes program-owned component types and
module-owned types in one per-module state. Two modules comparing the same
builtin collection therefore emit the same recursive helper twice.

Partition equality by C definition ownership:

```text
hexal/equality.h       program-owned equality specializations
modules/<name>.h       module-owned equality specializations
```

`equality.h` is header-only and demand-generated. Static inline definitions
preserve current call and optimization behavior.

## Ownership classification

Program-owned equality helpers include equality-capable concrete types whose
complete definitions are emitted by program components:

- builtin-element `Array<T, N>`;
- builtin-element `View<T>`;
- builtin-element `List<T>`;
- recursively composed program-owned forms of those types;
- compiler-owned component objects such as Error when equality is valid.

Module-owned equality helpers remain local for:

- user objects;
- user ADTs;
- every structural union, because its wrapper is currently re-emitted in
  module headers;
- collections containing any module-owned type;
- any aggregate whose complete definition is unavailable to a program
  component.

String equality is owned by `hexal/string.h/.c` under RFC 0100 and is not
redefined in `equality.h`.

## Discovery and emission

- Discover equality requirements per module as today, recursively including
  member and element dependencies.
- Partition each discovered type with the existing complete-type ownership
  predicate; do not infer ownership from source spelling.
- Merge program-owned requirements by canonical identity and sort them in
  dependency-first deterministic order.
- Emit one `hexal/equality.h` containing the merged program-owned helpers.
- Include the exact component headers that define the helper parameter/member
  types, in dependency order. Include the String component when a recursive
  comparison calls String equality.
- A module includes `equality.h` when its expressions or module-owned helpers
  call a program-owned equality helper.
- Emit no equality component when the merged set is empty.
- Module-owned helper generation excludes the program-owned partition and
  calls it instead.
- Keep helper names unchanged in this RFC.

Program-level ownership means one generated source artifact. As a header-only
component, the C preprocessor may expose its static inline definitions to
several translation units; this RFC promises no external symbol or single
machine-code body.

## Invariants

1. Every equality helper has exactly one generated ownership class.
2. No program component names an incomplete module-owned type.
3. Recursive helper dependencies precede their callers.
4. String equality has one owner: the String component.
5. Equality behavior and eligibility remain unchanged.
6. Component and module output remain deterministic.

## Validation

This section is exhaustive.

- Two modules comparing the same `Array<Int32, N>` emit one helper in
  `hexal/equality.h`; both modules include it and neither re-emits it.
- Repeat for builtin-element View and List equality.
- A recursively composed program-owned collection emits each dependency once
  before its caller.
- A program-owned helper comparing String calls the String component equality
  function and does not define another String helper.
- A user object, user ADT, structural union, and collection containing a user
  type each retain their module-owned helper after the complete type
  definition.
- A module-owned helper may call a program-owned helper through
  `hexal/equality.h`; the include precedes that call.
- A module not using or depending on program-owned equality does not include
  `equality.h`.
- A program with no program-owned aggregate equality emits no equality
  component.
- Every helper name appears in exactly one ownership artifact per program.
- Repeated compilation produces byte-identical files.
- `docs/reference.md` records generated equality-helper ownership only.
- The snippet manifest moves only for snippets comparing affected
  program-owned aggregate types.
- `go test ./...`, `go vet ./...`.

## Non-goals

- Centralizing structural-union wrappers or module-owned types.
- Changing which types support equality.
- Adding Dict, Task, Channel, Mutex, Atomic, or function equality.
- Creating an external equality ABI.
- External C compilation.

## Drawbacks

- Adds one component and a partition step to equality discovery.
- Some module-owned helpers remain duplicated when the same foreign nominal
  type is re-emitted in several consuming headers; removing that requires a
  module-header topology redesign.

