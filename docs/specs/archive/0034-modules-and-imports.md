# RFC 0034: Modules and Imports

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; conformance verified 2026-08-14
- Features: file modules, explicit aliased imports, private-by-default
  declarations, qualified access, path-derived identity, dependency ordering,
  and per-module C output
- Created: 2026-08-11
- Updated: 2026-08-14
- Depends on: RFC 0004 (identifiers), RFC 0005 (type identity and declaration
  order), RFC 0008 (functions), RFC 0019 (generics), RFC 0041 (no module
  globals)
- Coordinates with: RFC 0039 (C interop), future build-system, package,
  foreign-ABI, and exported-layout specifications

## Summary

- Every `.hex` file is one module. Source contains no module declaration.
- The build-selected root file is the program entry module.
- Imports create mandatory local aliases:

```hexal
module Math = import "./math"

result: Int32 = Math.add(2, 3)
```

- Declarations are private by default. `export` exposes declarations to Hexal
  importers:

```hexal
export fun add(left: Int32, right: Int32): Int32
    return left + right
end
```

- Imported declarations are always qualified.
- Imported modules contain declarations only. They have no runtime
  initialization, module values, or import-time side effects.
- Every reachable module produces one C/header pair. The build also produces
  top-level `main.c` and `main.h` for the process entrypoint and compiler-owned
  program support.
- Native module visibility is distinct from foreign ABI exposure. `export`
  means Hexal-module visibility only; a future proposal may use `extern` for C
  linkage.

## Source model

- One `.hex` file defines exactly one module.
- A source file contains no explicit module declaration.
- A module's canonical identity is its normalized logical source-map key,
  excluding `.hex`.
- Changing a source module's logical key changes its canonical module identity.
- Absolute host paths never participate in type identity, generated names,
  or generated artifacts.
- Multi-file modules and implicit directory packages are unavailable in V1.

Example:

```text
logical key: graphics/shapes.hex
canonical identity: graphics/shapes
```

The selected root file is the only entry module. Its basename has no semantic
meaning; `main.hex` is valid and not reserved.

## Compiler input and output model

- The compiler is exclusively an in-memory string transformation. It performs
  no filesystem reads, writes, discovery, directory inspection, symlink
  resolution, or working-directory lookup.
- Compilation accepts all source text and the selected entrypoint directly:

```go
func Compile(sources map[string]string, entrypoint string) CompilationResult
```

- `sources` maps logical `.hex` filenames to complete Hexal source strings.
- Logical filenames use `/` as their separator on every host platform.
- `entrypoint` is the logical `.hex` filename of the selected root module and
  must name exactly one entry in `sources`.
- Native import resolution operates only over `sources`. An import fails when
  its resolved logical key is absent; the compiler never searches elsewhere.
- Source-map keys are logical compiler input, not claims about host files.
  The compiler does not test whether they exist on disk, denote directories,
  traverse symlinks, match host path casing, or are valid for a host filesystem.
- Compilation returns:

```go
type CompilationResult struct {
    MainC    string
    MainH    string
    Files    map[string]string
    Stderr   []string
    ExitCode int
    Stats    CompilationStats
}
```

- `Files` maps normalized logical generated filenames to complete file text.
- `Files` is the authoritative generated-artifact result and contains every
  emitted C/header file, including `main.c`, `main.h`, and all
  `modules/<canonical-path>.c/.h` pairs.
- `MainC` and `MainH` are entrypoint conveniences. They are derived after
  generation and must equal `Files["main.c"]` and `Files["main.h"]`; they are
  never generated or mutated independently.
- Root module code is available only through
  `Files["modules/<canonical-root>.c/.h"]`. `MainC`/`MainH` do not alias root
  module artifacts.
- On compilation failure, no `modules/` artifacts are returned. `Files`
  contains only the complete generated failure `main.c` and `main.h`;
  `MainC`/`MainH` mirror them. These failure entrypoint files are deliberate
  fail-closed output, not partially generated source output.
- `Files` map iteration order has no meaning. Compiler code, tests, diagnostics,
  and statistics sort logical keys before deterministic traversal.
- The former `Compile(source string)` API is replaced by the multi-source
  `Compile(sources, entrypoint)` API. There is one compilation pipeline and no
  single-source compiler entrypoint.
- Filesystem drivers, project discovery, module-root configuration, file
  watching, caching, and incremental compilation are future layers outside the
  core compiler and outside this RFC.

### Statistics

- `CompilationResult.Stats` is one final project-level summary for the complete
  compilation call.
- No per-module statistics are exposed or returned.
- The summary covers the entrypoint and all reachable modules; unreachable
  source-map entries contribute nothing.

## Grammar

Add module imports and export modifiers to the grammar:

```ebnf
program = lexical-separation , { import-declaration }
          , { top-level-item } ;

import-declaration = "module" , identifier , "=" , "import"
                     , module-path-literal ;

module-path-literal = '"' , relative-import-path , '"' ;

relative-import-path = ( "./" | "../" , { "../" } )
                       , identifier , { "/" , identifier }
                       , [ ".hex" ] ;

top-level-item = ( [ "export" ]
                   , ( type-declaration | function-declaration
                       | implementation-declaration ) )
                 | statement ;
```

Semantic restrictions narrow these forms:

- Imports precede every non-import item.
- `module`, `import`, and `export` are reserved words.
- `export` is invalid before a statement or value binding.
- Only the root module admits executable statements and value bindings.

## Imports

### Form

```hexal
module Math = import "./math"
module Shapes = import "./graphics/shapes"
```

- Every import names an explicit alias; there is no inferred alias.
- The `.hex` extension may be omitted. If written, it must be `.hex`.
- Import paths are compile-time module-path literals, not runtime String
  values.
- Module-path literals use their raw token spelling. Backslash and every String
  escape are invalid; no escape decoding occurs. In particular,
  `"./ma\u{74}h"` does not import `./math` and is rejected lexically.
- The alias is visible throughout only the importing module.
- The alias creates no runtime value or module object.
- The alias cannot be passed, stored, returned, compared, printed, or used as a
  generic argument.

### Qualification

- Every imported declaration use is qualified by its local alias.
- There are no wildcard imports, selective imports, opened namespaces,
  implicit parent imports, unqualified imported names, or re-exports in V1.
- Importing a module does not expose that module's private declarations or its
  dependencies.

```hexal
module Geometry = import "./geometry"

point: Geometry.Point = Geometry.origin()
```

### Alias namespace

- Import aliases occupy the same protected name space as source types, values,
  functions, method owners, generic parameters, qualifiers, and other import
  aliases.
- An alias cannot be redeclared or shadowed in any nested scope.
- Importing the same canonical module twice is an error, including under two
  different aliases.
- Two imports resolving to one canonical file always denote one module
  identity; duplicate imports are rejected rather than merged.

## Path resolution

### Source spelling

- V1 native imports use relative paths beginning with `./` or `../`.
- After the relative prefix, each slash-separated component is a valid Hexal
  identifier.
- `/` is the canonical separator in source on every host platform.
- Empty components, `.`, repeated separators, absolute paths, drive prefixes,
  query/fragment text, and non-`.hex` extensions are invalid.
- `..` components are resolved lexically but cannot walk above the logical
  source-map root.

### Canonicalization

- Resolution is relative to the importing file's directory.
- Omitted extensions resolve only to `.hex`.
- Resolution is lexical over `/`-separated logical keys and must not walk above
  the logical source-map root.
- Path normalization occurs before identity comparison, cycle detection, and
  duplicate-import detection.
- Resolution succeeds only when the resulting logical `.hex` key exists
  exactly in `sources`; otherwise it reports a missing-module error.
- Logical keys and canonical identities are case-sensitive strings. Host
  filesystem case rules do not participate.
- Case-distinct reachable modules produce distinct `Files` keys, C symbols,
  and header guards. The compiler does not reject them based on a possible
  future output filesystem.

Package names, dependency roots, registries, versions, and standard-library
prefixes remain future work. Any later filesystem or build layer must supply
logical keys without changing the compiler's canonical module-identity model.

## Visibility

### Exportable declarations

`export` may prefix only module-level declarations with stable compile-time
identity:

- functions;
- methods/implementation declarations;
- object types;
- algebraic data types;
- transparent aliases;
- generic forms of those declarations.

Examples:

```hexal
export type Point = { x: Int32, y: Int32 }

export fun origin(): Point
    return Point { x = 0, y = 0 }
end

export impl Point.length_squared(): Int32
    return self.x * self.x + self.y * self.y
end
```

### Non-exportable declarations

- Hexal has no native module-level values, constants, globals, or static
  storage; consequently no native variable can be exported.
- `export` is invalid on executable statements, root bindings, parameters,
  locals, object members, ADT payload members, import declarations, and generic
  parameters.
- Ownership does not affect this rule. Scalars, pointers, handles, Arrays, and
  every other value category are equally ineligible as module exports.
- State is constructed by a function, passed explicitly, or stored in
  caller-owned/allocated memory.

```hexal
export fun make_items(heap: Heap): List<Int32>
    items: List<Int32> = List<Int32>.new(heap)
    items.push(1)
    items.push(2)
    return items
end
```

### Meaning

- Private declarations are visible only inside their defining module under the
  normal source-order rules.
- Exported declarations additionally enter the defining module's public
  interface and become visible through importer aliases.
- `export` does not create C ABI linkage or promise a stable foreign name,
  layout, or calling convention.
- A future foreign-ABI proposal must use separate syntax, tentatively `extern`.

### Exported-interface closure

- An exported declaration cannot expose a private nominal declaration.
- Every nominal type appearing anywhere in an exported source-level interface
  must be builtin or exported by its defining module.
- The check recursively traverses:
  - function parameters and results;
  - method receiver, parameters, and result;
  - transparent alias targets;
  - object members;
  - ADT variants and payload members;
  - union members, Arrays, pointers, Fun signatures, and generic arguments;
  - every other nested type constructor in the interface.
- An exported object exposes its member names, types, mutability, order, and
  layout to Hexal importers.
- An exported ADT exposes its variant names, payload types, order, and layout to
  Hexal importers.
- An exported method requires an exported receiver type.
- An exported transparent alias cannot make a private target public
  indirectly.
- Generic declarations may use private types or helpers only inside their
  bodies when those declarations do not appear in the exported interface.
  Defining-module specialization preserves access to such private
  implementation details.
- Export does not propagate implicitly. A private type must be explicitly
  exported before another exported declaration may expose it.

```hexal
type Secret = { value: Int32 }

export fun reveal(): Secret
    return Secret { value = 1 }
end
```

The example is rejected because `Secret` is private. Exporting `Secret` makes
the function interface valid.

## Name resolution

- The parser represents `Alias.name` using qualified syntax; semantic
  resolution determines whether the leftmost name is an import alias.
- An import alias may qualify exported types, functions, methods where method
  syntax requires their type, and qualified ADT variants belonging to an
  exported ADT.
- A private or unknown qualified name is a compile-time error.
- An import alias cannot appear as the receiver of arbitrary source-defined
  static members; Hexal has no static-method feature.
- Qualified type and value lookup remain distinct after resolving the module
  alias.
- Diagnostics use the source alias where describing the use and the canonical
  module identity where identifying the declaration owner or import chain.

## Type and declaration identity

- A nominal type belongs permanently to the module that declares it.
- Its canonical identity contains the defining module identity and declaration
  identity.
- Import aliases are local spellings and never create, wrap, or clone types.
- Renaming an import alias leaves canonical identity unchanged.
- Identically named and shaped types from different modules remain distinct.
- A transparent alias retains its target's canonical identity, including when
  the target belongs to another module.
- Function, method, generic, and specialization identities likewise include
  their defining module identity.

## Implementation ownership

- An implementation declaration may target only a nominal object type defined
  in the same module.
- Only a type's defining module may add or export methods for that type.
- Imported types may call their exported methods but cannot receive new
  methods in the importing module.
- Transparent aliases do not bypass this rule: an alias of an imported type
  retains the imported type's defining-module identity and is not a valid local
  implementation target.
- This keeps the existing one-method-name-across-receiver-forms rule local to
  the one module authorized to declare the type's methods.

```hexal
module Geometry = import "./geometry"

impl Geometry.Point.rotate(): Geometry.Point
    return self
end
```

The example is rejected because `Geometry.Point` is defined by `geometry`, not
the importing module.

## Source order across modules

- The compiler resolves the complete reachable import graph before checking
  module bodies.
- Within one module, declarations retain the language's existing source-order
  visibility rules.
- An exported declaration is available to importers after its defining module
  successfully checks, regardless of its textual position relative to the
  importer's declarations.
- Importing a module does not permit forward references to later private
  declarations inside the importing module.
- A public signature may name an exported foreign type through an import alias.
  Consumers retain a dependency on that type's defining module identity.

## Dependency graph

- Only modules transitively reachable from the selected root are compiled.
- Unreachable source-map entries are ignored and cannot create diagnostics or
  artifacts.
- Missing modules, duplicate imports, and import cycles are compile-time Module
  Errors.
- Any cycle is rejected in V1, including cycles containing declarations only.
- The diagnostic identifies the complete canonical cycle.
- Graph traversal and emitted artifact ordering are deterministic and
  independent of Go map iteration order.
- Each canonical module is loaded and checked exactly once per build.

Rejecting cycles keeps declaration availability, generic ownership,
diagnostics, and generation deterministic. It is not an initialization-order
rule because imported modules never initialize at runtime.

## Root and imported modules

### Root module

- Exactly one build-selected root module supplies the program entry body.
- It may contain type, function, method, and import declarations plus the
  executable statements and bindings already permitted at program root.
- Root bindings lower to automatic locals of the root run function. They are
  not module globals and declared functions cannot capture them.
- Root execution follows source order and has no final-expression process-result
  convention.
- Unless a future entrypoint specification changes it, normal root completion
  returns C `EXIT_SUCCESS` after root defers and shutdown behavior complete.

### Imported modules

- A non-root module contains imports plus type, function, and method
  declarations only.
- Every executable statement and value binding in a non-root module is a
  compile-time Module Error.
- Non-root modules have no hidden initializer, runtime Heap, final expression,
  import-time side effect, or exactly-once initialization state.
- Diamonds require no runtime deduplication; a module contributes declarations
  and generated definitions once.

## Generic specialization ownership

### User-declared generics

- Public generic declarations are structurally checked in their defining
  module.
- Concrete uses in reachable importers request specializations from the
  defining module.
- The defining module owns and emits every specialization of its declarations.
- A specialization key contains defining module identity, declaration identity,
  and canonical type arguments.
- The defining module receives the sorted set of reachable requested
  specializations during generation.
- Repeated requests reuse one specialization and one C definition.
- Same source plus the same request set produces identical specialization names
  and output, independent of import traversal order.
- An invalid specialization diagnostic identifies both the generic declaration
  and every importing use necessary to explain the request.

Changing the request set may change the defining module's generated artifact
even when its source is unchanged.

### Built-in generics

- Array, View, List, Dict, Stream, Task, Channel, Atomic, Ptr, MutPtr, and Fun
  are compiler-owned constructors and have no defining source module.
- The compiler-owned program-support sections of `main.c` and `main.h` own
  their reachable concrete specializations.
- The compiler collects every reachable built-in specialization request across
  all modules, canonicalizes it after type substitution, sorts it, and emits
  each specialization exactly once where external identity or state is
  required.
- `main.c` owns compiler-generated external definitions and per-specialization
  state. `main.h` owns common declarations usable before module definitions.
- Dependency-safe generated sections in module headers may contain complete
  specialization types, prototypes, or `static inline` helpers when the
  element types must first be complete.
- A specialization repeated in multiple generated headers uses one canonical
  guard and identical text. Header-local repetition is permitted only for C-safe
  type declarations, prototypes, and stateless `static inline` helpers. It must
  never create distinct state or externally linked definitions.
- Built-in specialization identity depends only on the constructor and
  canonical arguments, never on the requesting module or traversal order.
- The sorted built-in-specialization set deterministically controls the
  corresponding content in `main.c`, `main.h`, and affected module headers.
- User-declared generic specializations remain owned by their defining source
  modules under the preceding rules.

## Generated C artifacts

### Artifact set

Every reachable native module produces one C/header pair under the logical
`modules/` output directory. Its generated path preserves the canonical module
path and replaces only `.hex` with `.c` or `.h`. Every program build also
produces top-level `main.c` and `main.h`.

```text
app.hex                 -> modules/app.c, modules/app.h
math.hex                -> modules/math.c, modules/math.h
graphics/shapes.hex     -> modules/graphics/shapes.c
                           modules/graphics/shapes.h
main.hex                -> modules/main.c, modules/main.h
main.c                  -> process entrypoint and program support
main.h                  -> entrypoint/program-support declarations
```

- Generated paths never overwrite source files. The `modules/` namespace keeps
  a source module named `main.hex` distinct from entrypoint `main.c`/`main.h`.
- `main.h` owns declarations shared by the process entrypoint, compiler-owned
  program support, generated runtime bootstrap, and C libraries used by core
  libraries or bootstrapping.
- `main.h` must contain only declarations needed across generated translation
  units; it is not a public Hexal FFI header.
- `main.c` contains compiler-owned external definitions and
  per-specialization state, includes `main.h` and the root module header,
  initializes required generated runtime support, invokes the root run
  function, and returns its C status.
- The root module pair owns the root execution body exposed to `main.c` through
  a generated internal declaration.
- Non-root modules expose no initializer function.

Conceptually:

```c
#include "main.h"
#include "modules/app.h"

int main(void) {
    return hex_module_root_run();
}
```

### Header and linkage rules

- A module header contains exported type declarations, declarations needed by
  importers, and compiler-private cross-module declarations.
- Native module headers are compiler artifacts, not stable C ABI promises.
- Exported Hexal functions use deterministic compiler-private external linkage
  across generated translation units.
- Private functions remain `static` in their defining module C file unless
  required internally for compiler-owned specialization machinery.
- Private types and helpers remain outside importer-visible headers where C
  completeness rules permit it.
- Dependency-safe forward declarations precede definitions.
- `#line` directives preserve original `.hex` file names and lines.

### C identifier encoding

- Generated filenames preserve canonical module paths as specified above.
- C symbols and header guards use one deterministic, reversible,
  collision-free ASCII encoding of the canonical module identity.
- The encoding is case-preserving and length-delimited. C symbols and macro
  identifiers are case-sensitive, matching canonical module identity.
  Replacing `/` with `_` and `_` with `__` is forbidden because it is not
  injective for all paths.
- V1 uses the UTF-8 byte length and source spelling of each path component:

```text
graphics/shapes -> m8_graphics6_shapes
audio/math      -> m5_audio4_math
```

- Each component is encoded as its decimal UTF-8 byte length, `_`, then its
  source spelling. V1 path components use the current ASCII Hexal identifier
  grammar, so no additional payload escaping is required.
- If Hexal later admits non-ASCII identifiers, that feature must define an
  injective ASCII byte encoding before such identifiers become valid module
  path components.
- Generated native identifiers retain the source-declaration prefixes from
  `reference.md` after the encoded module owner.
- Header guards use reserved generated `HEX_MODULE_` macro spelling, the exact
  case-preserving encoded module owner, and `_H`. The encoded owner must never
  be uppercased or case-folded.

```text
graphics/shapes.draw -> hex_f_m8_graphics6_shapes_draw
graphics/shapes.h    -> HEX_MODULE_m8_graphics6_shapes_H
Graphics/Shapes.h    -> HEX_MODULE_m8_Graphics6_Shapes_H
```

### Determinism and build order

- Artifact enumeration is dependency-first with canonical identity as the
  stable tie-breaker.
- Dependency-first enumeration does not require serial C compilation;
  independent translation units may compile in parallel.
- Generated text for one module is stable for identical source, dependencies,
  target contract, and specialization request set.

## Diagnostics

Required diagnostic classes include:

```text
[Module Error] imported module ./math was not found
[Module Error] import resolves above the logical source-map root
[Module Error] duplicate import of canonical module graphics/shapes
[Module Error] import cycle: app -> math -> constants -> app
[Module Error] imported module math contains executable statements
[Name Error] declaration helper is private to module math
[Type Error] exported function reveal exposes private type Secret
[Type Error] cannot declare methods for imported type Geometry.Point
[Name Error] import alias Math conflicts with an existing name
[Syntax Error] export may prefix only a module-level type, function, or implementation declaration
```

- Import-path diagnostics identify the importing file and path token.
- Cycle diagnostics show canonical identities, not whichever aliases happened
  to discover the cycle.
- Invalid imported generic specializations also identify their defining
  declaration and requesting source use.
- No module failure may be silently ignored, partially generated, or reported
  as Unknown Error when it is classifiable.

## Reference synchronization

Implementation updates `docs/reference.md` after behavior stabilizes and before
this RFC closes:

- add module/import/export grammar at the top;
- add `module`, `import`, and `export` to the reserved-word list and add the
  raw, escape-free module-path literal;
- add canonical file-module identity and path resolution;
- add visibility, qualification, type identity, source-order, root/non-root,
  exported-interface closure, implementation ownership, dependency, user and
  built-in generic ownership;
- add Module Error to the diagnostic classes; retain Name Error for inaccessible
  names and Type Error for invalid exported interfaces or implementation
  targets;
- add per-module C/header output, module-name encoding, `main.c`, and `main.h`;
- add the `Compile(sources, entrypoint)` in-memory API,
  `CompilationResult.Files`, the project-level statistics summary, and complete
  failure-entrypoint contracts;
- remove native modules from Excluded features;
- retain C interop and foreign ABI syntax as draft/excluded work.

## Required tests

### Lexer and parser

- Reserve `module`, `import`, and `export`.
- Parse imports, omitted/written `.hex`, exportable declaration forms, and
  qualified type/value uses.
- Accept raw module-path literals and reject backslash plus every String escape.
- Reject misplaced imports, missing aliases/operators/paths, and export on
  non-declarations.
- Preserve existing dotted member and ADT syntax when the leftmost name is not
  an import alias.

### Resolver and checker

- Resolve relative logical paths, normalization, duplicates, missing source-map
  entries, logical-root escapes, and cycles.
- Enforce alias collisions/non-shadowing and qualified-only visibility.
- Enforce private/exported access for every exportable declaration kind.
- Reject private nominal types exposed recursively through exported aliases,
  signatures, receivers, members, payloads, unions, constructed types, and
  generic arguments.
- Accept private helpers and types used only inside exported generic bodies.
- Reject every value export and every non-root executable item.
- Preserve module-qualified nominal identity through aliases and generics.
- Check public signatures that name exported foreign types.
- Reject implementation declarations targeting imported types, including
  through transparent aliases; accept method calls exported by the defining
  module.
- Own generic specializations in their defining modules and deduplicate request
  sets deterministically.
- Collect built-in generic specializations program-wide; verify one canonical
  identity, guarded dependency-safe declarations, and exactly one external or
  stateful definition.

### Generator and integration

- Verify `Compile(sources, entrypoint)`, entrypoint lookup, and relative
  resolution exclusively over the supplied source strings.
- Emit one pair per reachable module plus `main.c` and `main.h`.
- Verify `Files` contains the complete artifact set under normalized logical
  keys and `MainC`/`MainH` exactly mirror its entrypoint entries.
- Verify failures return only complete failure `main.c`/`main.h`, no `modules/`
  artifacts, and no partially generated source output.
- Verify deterministic consumers sort `Files` keys and never depend on Go map
  iteration order.
- Migrate tests that inspect root generated code from `MainC`/`MainH` to the
  corresponding `Files["modules/<root>.c/.h"]` entries; C23 tests materialize
  and compile the complete `Files` set.
- Verify `Stats` reports one project-level summary and exposes no per-module
  statistics.
- Ignore unreachable source-map entries.
- Verify mirrored module filenames plus collision-free symbols, guards, and
  canonical identities.
- Verify modules whose logical keys differ only by case produce distinct
  artifact keys, symbols, and case-preserving header guards.
- Verify exported/private linkage, dependency-safe declarations, original
  `#line` mappings, and absence of non-root initializer functions.
- Verify diamond dependencies emit one module definition.
- Compile and link representative multi-module output in C23-tagged tests.

## Deferred work

- Multi-file modules and directory packages.
- Filesystem reads, writes, discovery, directory validation, symlink handling,
  host-path normalization, and working-directory behavior.
- Materialization and collision policy for case-sensitive logical artifacts on
  case-insensitive filesystems.
- Incremental compilation, file watching, caches, public-interface
  fingerprints, and invalidation.
- Package manifests, dependency names, versions, registries, and downloads.
- Standard-library import prefixes.
- C imports and exports, foreign linkage, ABI annotations, and the final
  `extern` spelling.
- Re-exports, selective imports, opened namespaces, and prelude modules.
- Interface-only import cycles.
- Conditional compilation and target-specific source selection.
- Resource embedding.
- Tests and benchmarks as module members.
- User-selected process exit results or an explicit source `main` function.
- Stable public C headers generated from Hexal modules.

## Acceptance criteria

1. Every reachable `.hex` file has one canonical path-derived module identity;
   no source module declaration exists.
2. `module Alias = import "path"` is the only native import form, and every
   imported use is qualified.
3. `export` exposes only module-level types, functions, methods, aliases, ADTs,
   and their generic forms to Hexal importers.
4. Every nominal type recursively exposed by an exported interface is builtin
   or explicitly exported; export never propagates implicitly.
5. Native module values and value exports do not exist.
6. Only the selected root admits executable statements; imported modules have
   no initialization or side effects.
7. Missing, duplicate, escaping, and cyclic imports fail with
   structured diagnostics before C generation.
8. Nominal and generic identities include the defining canonical module;
   import aliases never alter identity.
9. The defining module deterministically owns each requested generic
   specialization.
10. Only a nominal type's defining module may declare its methods, including
    through transparent aliases.
11. Compiler-owned program support deduplicates every reachable built-in
    generic specialization and emits state/external definitions exactly once.
12. Every reachable module emits one mirrored `modules/<path>.c/.h` pair;
    unreachable source-map entries emit nothing.
13. Each build emits top-level `main.c` and `main.h` containing the process
    entrypoint and compiler-owned program support; non-root modules emit no
    initializer.
14. Generated output is deterministic, preserves `#line`, compiles and links as
    C23, and introduces no stable foreign ABI promise.
15. `Compile(sources, entrypoint)` consumes only in-memory source strings and
    performs no filesystem operations.
16. `CompilationResult.Files` is the authoritative complete artifact map;
    `MainC`/`MainH` mirror its entrypoint files, failures return only complete
    failure entrypoint files, and every value returned by the compiler is
    in-memory generated file content.
17. `docs/reference.md` is synchronized before the RFC is marked implemented or
    closed.

## Open questions

None.
