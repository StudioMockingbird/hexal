# RFC 0190: Cross-Module Generic Correctness

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-15
- Scope: make exported generic types, functions, and methods work through
  ordinary module aliases with defining-module resolution and correct source
  provenance
- Depends on: the current module, export, generic specialization, nominal type
  identity, and generated-artifact contracts
- Coordinates with: RFC 0186, whose source standard-library modules may export
  generics
- Does not add: re-exports, selective imports, wildcard imports, new generic
  inference, specialization relocation, or a package system
- Does not update `docs/reference.md`: this restores its existing module/export
  and generic contracts rather than changing them

## Problem

Simple exported generic functions work when their signature and body use only
builtins. A generic exported function that names a generic type from its own
module is rechecked in the importing module's environment and fails:

```hexal
-- boxes.hex
type Box<T> is struct
    item: T,
end

fun new_box<T>(value: T): Box<T> do
    return Box<T>(item = value)
end

export
    Box, new_box
end
```

```hexal
-- app.hex
import
    Boxes from "./boxes"
end

box := Boxes.new_box<Int32>(1)
```

The current compiler reports:

```text
[Type Error] unknown generic type Box at app.hex:4:27
```

`Box` is valid private resolution inside `boxes.hex`, and the reported token is
from that file but is incorrectly attributed to `app.hex`. Qualified generic
type uses such as `Boxes.Box<Int32>` also lack a complete parser/checker path.

## Contract

- `Alias.Name<T, ...>` is a type expression when Alias is an import alias and
  Name is an exported generic type of that module.
- Imported generic struct construction uses `Alias.Name<T, ...>(...)`.
  Imported generic ADT construction and patterns use
  `Alias.Name<T, ...>.Variant(...)`, preserving the generic owner between the
  module alias and variant.
- The generic declaration's arity and concrete arguments are checked exactly as
  for a local generic type.
- Private or unknown imported generic types retain ordinary module visibility
  diagnostics at the importing use site.
- An imported generic function is specialized using:
  - concrete arguments resolved in the importing module;
  - its declaration, private declarations, imports, signature, and body resolved
    in the defining module; and
  - its emitted specialization owned by the defining module.
- Source diagnostics from a generic declaration use the defining module's
  logical key and coordinates. Diagnostics about the importing call's arguments
  or qualification use the importing module's location.
- The same defining declaration plus canonical concrete arguments interns one
  specialization program-wide. Repeated requests do not duplicate generated C.
- Existing nominal module identity remains unchanged: `A.Box<Int32>` and
  `B.Box<Int32>` are distinct when A and B declare distinct Box templates.
- Generic module artifacts remain project-dependent under RFC 0186: only
  requested concrete specializations are emitted.
- A generic method exported as `Type.method` remains available on an imported
  specialized receiver. Its receiver/type arguments, body, private names,
  specialization ownership, and diagnostics follow the same defining-module
  rules as an imported generic function.

## Diagnostics

- An unknown or private `Alias.Name<T>` uses the same exact visibility
  diagnostic as an unknown or private non-generic `Alias.Name`.
- Wrong generic arity uses the existing exact arity diagnostic, anchored at the
  qualified type or call in the importing source.
- A failure in the specialized declaration body uses the existing diagnostic
  message and the defining module's logical key.
- A compiler state missing the defining module's specialization environment
  fails closed as Unknown Error; it never retries in the importing environment.

## Required sweep

- qualified type-expression parsing and AST representation;
- module export records for open generic types, functions, and methods;
- generic template ownership, defining-module type environments, imports,
  logical keys, scopes, recursion guards, and specialization collections;
- qualified generic calls, constructors, annotations, parameters, results,
  object/ADT members, aliases, and nested constructed types;
- source-location ownership for diagnostics produced during specialization;
- deterministic defining-module specialization assembly and generated symbols;
- existing imported-generic tests that cover only builtin signatures; and
- RFC 0186's generic stdlib-module prerequisite.

Do not introduce an analyzer pass or copy a defining module's declarations into
an importer. The module registry is the authoritative cross-module boundary.

## Detailed implementation plan

### Phase 1: reproduce and freeze

1. Add focused failing tests for the example above, qualified generic type
   annotations/construction, and defining-module diagnostic provenance.
2. Record the currently working imported `identity<T>(value: T): T` case so the
   repair does not regress simple generic calls or specialization ownership.
3. Inventory every registry field and checker context fact required to
   specialize a declaration in its defining module.

### Phase 2: qualified generic type syntax

1. Extend type-expression parsing so the final exported declaration in
   `Alias.Name<arguments>` carries both module qualification and type arguments.
2. Resolve the alias once through the module graph, apply the export gate, and
   specialize through the defining module's generic table.
3. Reuse local generic arity, substitution, placement, and nominal-identity
   checks; keep parser and checker dispatch fail-closed.

### Phase 3: defining-module specialization context

1. Retain the defining module facts needed by each exported open template:
   type environment, imports, generic table, module scope, logical key, and
   specialization store. Do not substitute the requester's environment.
2. Resolve explicit concrete arguments and inferred argument facts at the call
   site, then recheck the template signature and body in that defining context.
3. Keep the specialization record in the defining module and preserve the
   existing deterministic canonical-argument key and recursion rules.
4. Split diagnostic ownership: declaration tokens use the defining logical key;
   call/type-use tokens use the requester logical key.

### Phase 4: conformance

1. Implement every Validation item in parser, checker, generator, and public
   integration tests.
2. Add generated-text assertions for one defining-module specialization and no
   importer-owned duplicate.
3. Run ordinary and tagged C23 suites. Existing snippet hashes remain unchanged;
   add no workbench snippet solely for this bug repair.
4. Update RFC 0186's blocker and `docs/status.md` when this RFC closes.

## Validation

This list is exhaustive:

- the motivating `Boxes.new_box<Int32>` program compiles;
- `Boxes.Box<Int32>` works in declaration annotations, parameters, results,
  object members, ADT payloads, aliases, and nested constructed types;
- `Boxes.Box<Int32>(item = 1)` constructs an exported generic struct, and
  `Models.Result<Int32, String>.Ok(value = 1)` works in construction and match
  patterns;
- a method exported as `Box.get` works on `Boxes.Box<Int32>` and any method-
  specific type parameters are inferred or supplied exactly as for a local
  generic method;
- explicit and inferred imported generic-function arguments both work when the
  signature or body names private and exported declarations of its own module;
- a defining generic body can use its defining module's imports without making
  those imports visible through the caller's alias;
- private/unknown generic types and functions, wrong arity, invalid concrete
  arguments, and invalid specialized bodies retain their ordinary diagnostics;
- call-site failures name the importing logical key, while declaration-body
  failures name the defining logical key and correct coordinates;
- same-named generic types in two modules remain nominally distinct;
- repeated equal specialization demand emits one definition in the defining
  module; different concrete demand emits the corresponding deterministic set;
- the importing module emits calls and uses but no duplicate specialization;
- existing local generics, imported builtin-only generic functions, module
  visibility, source order, recursion, and deterministic output remain valid;
  and
- ordinary and tagged C23 suites pass with no existing snippet hash movement.

## Open questions

None.
