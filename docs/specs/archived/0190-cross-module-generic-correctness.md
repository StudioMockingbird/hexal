# RFC 0190: Cross-Module Generic Correctness

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. `docs/reference.md` is synchronized with explicit user
  approval: the Generics section records qualified generic types, defining-
  module specialization context, and exported-specialization linkage and
  artifact ownership. The two real
  bugs the Problem section names are both fixed and verified end to end
  (checked, generated, compiled, linked, and run under the pinned backend):
  a cross-module generic function/method specialization previously kept C
  `static` linkage in its defining module while consumers declared it
  external, so a linked program was never actually achievable; and an
  imported generic's signature and body were rechecked in the importing
  module's environment rather than its own defining module's, so a private
  defining-module name (or a local reference to the module's own generic
  type) failed to resolve and any diagnostic named the wrong file. Also
  implemented: the qualified generic type syntax this fix required
  (`Alias.Name<Arguments>`, in annotations, construction, and matching the
  arity/visibility diagnostics a non-generic qualified type already has),
  qualified generic struct construction (`Alias.Name(...)` and
  `Alias.Name<T>(...)`, previously unsupported even for a non-generic
  exported type), and a qualified generic method call on an imported
  specialized receiver. Not implemented: RFC 0211's declaration-time
  checking of open generic bodies (that RFC is not requested; this RFC
  requires only that its checks, once RFC 0211 lands, run in the defining
  module for exported generics, which the retained defining-context
  mechanism now in place already satisfies structurally).
- Created: 2026-09-15
- Updated: 2026-09-16
- Scope: make exported generic types, functions, and methods work through
  ordinary module aliases with defining-module resolution and correct source
  provenance
- Depends on: the current module, export, generic specialization, nominal type
  identity, and generated-artifact contracts
- Coordinates with: RFC 0186, whose source standard-library modules may export
  generics after this RFC establishes the complete cross-module contract, and
  RFC 0211, which owns declaration-time checking of open generic bodies
- Does not add: re-exports, selective imports, wildcard imports, new generic
  inference, or a package system
- Reference impact: implementation requires explicit user approval to extend
  the normative grammar with qualified generic types and to record the
  cross-module specialization, provenance, linkage, and artifact contracts.
  This RFC cannot close while the implemented behavior and `docs/reference.md`
  disagree.

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

The currently accepted builtin-only case is also not a working generated
program. An importer emits an external declaration for
`hex_f_m3_lib_identity_Int32`, while the defining module emits its concrete
specialization with internal `static` linkage. Clang therefore reports an
undefined symbol when the generated files are linked. This RFC repairs the
parser, checker, generated declarations, definitions, and actual linked
program together; Hexal-level acceptance alone is not success.

Concrete arguments owned by an importer create a second artifact question:

```hexal
-- app.hex
type Point is struct
    x: Int32,
end

box: Boxes.Box<Point> := Boxes.Box<Point>(item = Point(x = 1))
```

The semantic specialization belongs to `boxes.hex`, but its C layout needs the
caller-owned `Point` definition. Generated headers repeat required concrete C
type declarations under canonical per-type guards without creating another
Hexal declaration or type identity.

## Syntax

Two reference grammar rules change:

```ebnf
generic-type-name = identifier - special-form-type-constructor
                  | identifier , "." , identifier ;
qualified-variant-pattern = [ identifier , "." ] , identifier
                            , type-argument-list , "." , identifier ;
```

Expressions need no grammar change: `Alias.Name<T>(...)` is an existing
`member-suffix` followed by `generic-call`, and `Alias.Name<T>.Variant(...)` is
a `member-suffix` followed by `generic-owner-call`. RFC 0186 separately owns the
non-generic `Alias.Adt.Variant(...)` form; the two rules must agree that an
imported variant always names its ADT.

- The qualified form is exactly `ImportAlias.ExportedType<...>`.
- It is not an arbitrary dotted path.
- The AST retains the import alias, exported declaration name, and type
  arguments in one fail-closed qualified-generic node. Do not discard the
  owner and later reconstruct it from tokens.
- The same owner-bearing node is preserved through struct construction and
  through `Alias.Type<...>.Variant(...)` construction and match patterns.

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
- Written and inferred concrete arguments are resolved in the importer and
  converted to canonical type identities. The open declaration's signature,
  constraints, private names, imports, body, and nested generic calls are then
  resolved only in its retained defining context. The concrete call is finally
  checked against that signature in the importer.
- Source diagnostics from a generic declaration use the defining module's
  logical key and coordinates. Diagnostics about the importing call's arguments
  or qualification use the importing module's location.
- Generated `#line` directives for a specialization body use the defining
  module's logical key. An `Error(...)` constructed by that body records the
  defining key, not the requesting module's key.
- The same defining declaration plus canonical concrete arguments interns one
  semantic specialization program-wide. Repeated requests emit exactly one
  function or method definition. A required concrete C type declaration may be
  repeated in multiple generated headers under one deterministic guard derived
  from its canonical identity.
- Existing nominal module identity remains unchanged: `A.Box<Int32>` and
  `B.Box<Int32>` are distinct when A and B declare distinct Box templates.
- Only requested concrete specializations are emitted. A defining module's
  concrete artifact is project-dependent because demands may originate in any
  reachable importer. This RFC owns that rule; RFC 0186 consumes it rather than
  defining it.
- A generic method exported as `Type.method` remains available on an imported
  specialized receiver. Its receiver/type arguments, body, private names,
  specialization ownership, and diagnostics follow the same defining-module
  rules as an imported generic function.
- Every open exported generic interface closes over builtins and exported types
  only after resolving aliases and nested generic members. A private type may
  occur inside a generic function body when it is absent from the exported
  signature or exported generic type layout.
- Declaration-time checking of open generic bodies is owned by RFC 0211. This
  RFC requires only that, once RFC 0211 lands, those checks run in the defining
  module for exported generics; concrete dependent checks run after
  substitution in the defining context as specified here.
- Specialization publication is transactional. Recursion detection may retain
  provisional in-progress state, but any diagnostic removes the provisional
  type/function/method cache entry and no incomplete declaration reaches a
  later request or the generator.
- A cross-module function or method specialization has one external-linkage C
  definition in its defining module and compatible declarations for its
  consumers. This linkage is private to the generated program and does not make
  the symbol a supported foreign ABI export.

## Diagnostics

- An unknown or private `Alias.Name<T>` uses the same exact visibility
  diagnostic as an unknown or private non-generic `Alias.Name`.
- Wrong generic arity uses the existing exact arity diagnostic, anchored at the
  qualified type or call in the importing source.
- A failure in the specialized declaration body uses the existing diagnostic
  message and the defining module's logical key.
- A compiler state missing the defining module's specialization environment
  fails closed as Unknown Error; it never retries in the importing environment.
- A failed specialization is not cached as a valid declaration and cannot
  suppress the same diagnostic at a later request.

## Required sweep

- qualified type-expression parsing and AST representation;
- module export records for open generic types, functions, and methods;
- generic template ownership, defining-module type environments, imports,
  logical keys, scopes, recursion guards, and specialization collections;
- qualified generic calls, constructors, annotations, parameters, results,
  object/ADT members, aliases, and nested constructed types;
- source-location ownership for diagnostics produced during specialization;
- deterministic defining-module specialization assembly and generated symbols;
- external-versus-static linkage and prototype generation for cross-module
  specializations;
- concrete generic type declaration ownership, include order, definition
  guards, and caller-owned nominal arguments;
- removal of incomplete cache entries after failed specialization;
- `Error.file` and generated `#line` provenance inside specialization bodies;
- existing imported-generic tests that cover only builtin signatures; and
- RFC 0186's generic stdlib-module prerequisite.

Do not introduce an analyzer pass or copy a defining module's semantic
declarations into an importer. The module registry is the authoritative
cross-module boundary. Guarded repetition is only a generated-C declaration
strategy and creates no second Hexal declaration or type identity.

## Detailed implementation plan

### Phase 1: reproduce and freeze

1. Add focused failing tests for the example above, qualified generic type
   annotations/construction, and defining-module diagnostic provenance.
2. Record the currently working imported `identity<T>(value: T): T` case so the
   repair does not regress simple generic calls or specialization ownership.
3. Inventory every registry field and checker context fact required to
   specialize a declaration in its defining module.
4. Add a tagged C23 fixture proving that the existing imported
   `identity<Int32>` output fails to link because its declaration is external
   and its definition is `static`.
5. Freeze importer-owned nominal arguments, importer-name poisoning, runtime
   `Error.file`, and nested defining-import/generic-call failures before
   changing the registry.

### Phase 2: qualified generic type syntax

1. Extend type-expression parsing so the final exported declaration in
   `Alias.Name<arguments>` carries both module qualification and type arguments.
   Use one explicit owner-bearing AST node; accept exactly one alias and one
   exported declaration name.
2. Resolve the alias once through the module graph, apply the export gate, and
   specialize through the defining module's generic table.
3. Reuse local generic arity, substitution, placement, and nominal-identity
   checks; keep parser and checker dispatch fail-closed.
4. Extend owner preservation through generic struct construction, ADT variant
   construction, and qualified variant patterns. Add parser tests for all three
   shapes and for malformed or over-qualified paths.

### Phase 3: defining-module specialization context

1. Extend each module registry entry with exported `genericTypes`,
   `genericFunctions`, and `genericMethods`, plus the defining type environment,
   imports, generic table, module scope, logical key, recursion state, and
   separate type/function/method specialization stores. Do not create a second
   hand-maintained declaration inventory.
2. Resolve explicit concrete arguments and inferred argument facts at the call
   site, then recheck the template signature and body in that defining context.
3. Register open templates in the defining module before publishing their
   exported records. Apply export-interface closure to open
   generic type layouts and function/method signatures.
4. Keep each semantic specialization record in the defining module and preserve
   the existing deterministic declaration-identity plus canonical-argument key
   and recursion rules. A defining generic calling another imported generic
   switches to that second declaration's defining context in the same way.
5. Add late deterministic assembly for requested concrete type declarations as
   well as function and method specializations. Sort by declaration identity
   and canonical argument keys, never by request order.
6. Publish a specialization only after its signature and body or layout checks
   succeed. On failure, remove provisional cache entries while unwinding the
   recursion guard.
7. Make imported generic method lookup use the exported generic-method registry
   and its defining method store; do not fall back to the importer's local
   generic method table.
8. Split diagnostic ownership: declaration tokens use the defining logical key;
   call/type-use tokens use the requester logical key.

### Phase 4: generated-C ownership and linkage

1. Compute each generated header's transitive concrete-type requirements for
   caller-owned arguments and nested concrete generic types. Repeat required C
   declarations under deterministic per-type guards derived from canonical
   identity; guard the owning declaration identically; every repetition must be
   byte-identical and follow its nominal dependencies.
2. Compute the stateless module-owned helpers each specialization requires for
   caller-owned arguments and repeat them under the same guard discipline.
3. Derive specialization symbol suffixes from canonical argument identity, not
   interning order.
4. Emit one non-`static` function or method specialization definition in the
   defining module. Emit compatible prototypes wherever a consumer needs them;
   do not add the symbols to the foreign ABI surface.
5. Preserve defining-module private helper calls and source mappings inside the
   definition. Ensure every C translation unit sees complete compatible
   parameter, result, receiver, and nested member types before first use.
6. Assert definition, prototype, include, guard, and type-declaration counts in
   generated text before invoking an external compiler.

### Phase 5: conformance

1. Implement every Validation item in parser, checker, generator, and public
   integration tests.
2. Add generated-text assertions for one defining-module definition, compatible
   consumer declarations, no duplicate definitions, and the guarded
   type-declaration behavior.
3. Add tagged C23 compile-link-run fixtures for builtin arguments,
   importer-owned nominal arguments, qualified generic objects and ADTs, and
   imported generic methods. Run ordinary and tagged C23 suites. Existing
   snippet hashes remain unchanged;
   add no workbench snippet solely for this bug repair.
4. With explicit user approval, synchronize `docs/reference.md` once behavior
   stabilizes. Update RFC 0186's blocker and `docs/status.md` when this RFC
   closes.

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
- a defining generic body that calls a generic imported by the defining module
  specializes that nested declaration in the nested declaration's own module;
- an importer declaring a same-named type, function, method, or module value
  cannot capture or alter a defining generic body's resolution, and source-map
  order does not change the result;
- private/unknown generic types and functions, wrong arity, invalid concrete
  arguments, and invalid specialized bodies retain their ordinary diagnostics;
- call-site failures name the importing logical key, while declaration-body
  failures name the defining logical key and correct coordinates;
- generated `#line` directives and an `Error(...)` value created inside a
  specialized body record the defining logical key;
- a failed specialization request leaves no completed cache entry for a later
  equal request;
- an exported generic layout or signature exposing a private defining-module
  type is rejected, while a private type used only inside the body is valid;
- same-named generic types in two modules remain nominally distinct;
- repeated equal specialization demand emits one definition in the defining
  module; different concrete demand emits the corresponding deterministic set;
- the importing module emits calls and uses but no duplicate function or method
  definition;
- imported generic functions and methods have compatible external-linkage
  program-internal declarations and definitions and link successfully;
- a caller-owned nominal argument and a nested concrete generic argument both
  have complete compatible C declarations in every translation unit that uses
  them, with byte-identical repetitions protected by one canonical per-type
  guard;
- the owning module's declaration of a repeated type uses the same guard as its
  repetitions, and a caller-owned argument containing another caller-owned
  nominal type by value emits both, dependency first, in every header that needs
  them;
- an imported generic body that prints, compares with `==`, or injects into a
  union a caller-owned argument compiles, links, and runs with its required
  helpers repeated under canonical guards;
- two distinct modules' same-named `Point` types specializing one imported
  generic produce two distinct deterministic C symbols, identical regardless of
  source-map order or which importer is compiled first;
- existing local generics, imported builtin-only generic functions, module
  visibility, source order, recursion, and deterministic output remain valid;
  and
- ordinary suites pass, tagged C23 fixtures compile, link, and run, and no
  existing snippet hash moves.

## Generated C type declarations

One semantic specialization must sometimes be spelled in translation units
owned by different source modules:

```hexal
-- boxes.hex owns Box<T>
-- app.hex owns Point
box: Boxes.Box<Point> := Boxes.Box<Point>(item = Point(x = 1))
```

- Intern one semantic specialization in the defining module.
- Emit exactly one external-linkage function or method definition.
- Repeat a required concrete C type declaration only in headers that need it,
  under one deterministic per-type guard derived from canonical identity.
- Every declaration of that type uses the same guard, including the owning
  module's own first declaration. No copy of a guarded type, owner or repeated,
  is emitted unguarded.
- The guard makes two generated headers safe to include together.
- Guarded repetitions of one canonical type must be byte-identical. A mismatch
  is an Unknown Error; the generator never chooses a spelling by request or
  emission order.
- A repeated declaration carries its complete nominal dependency closure. For
  `Boxes.Box<Point>`, `Point`'s complete declaration, and every nominal type
  `Point` itself contains by value, is emitted under its own guard before
  `Box<Point>` in every header that declares `Box<Point>`.
- Module-owned stateless helpers that a specialization body or declaration
  requires for a caller-owned type follow the same rule: equality helpers,
  print adapters, and union widen/inject/extract helpers are repeated under a
  canonical per-helper guard, byte-identical, in the defining module's header
  when its specialization body uses them. Runtime-component helpers already
  owned by `hexal/` components are unaffected.
- A specialization's C symbol suffix is derived from the canonical identities of
  its concrete arguments. When two distinct canonical arguments share a display
  name (`app.Point` and `geometry.Point`), both receive their encoded-owner
  suffix deterministically; the result never depends on which importer
  requested a specialization first.
- Dependency closure and declaration ordering are computed per generated
  header. Unrelated modules do not gain the declaration and do not change when
  the specialization is added.
- “One specialization” means one semantic identity and one executable
  definition, not one physical copy of a guarded C type declaration.
- This preserves the current per-module artifact shape and localized
  incremental rebuilds. Repeated declaration text adds no runtime
  representation or overhead.

## Open questions

None.
