# ADR 0071: Generated Runtime Components

- Kind: Architecture Decision Record (ADR)
- Status: Implemented; conformance verified 2026-08-17. The emission model,
  all component migrations, the workbench support-file toggle, and RFC 0055
  synchronization are complete; `docs/reference.md` was updated with the
  generated-artifact contract after explicit project-owner approval.
  Recorded deviations: the concurrency component declares no Heap/Error
  dependency because its prelude and runtime core name no such type (the
  dependency graph is the allowed edge set; actual declarations govern
  includes); the pre-existing module-owned-object element defect
  (`List<Point>` and similar) is tracked as an open bug in `docs/status.md`
  and requires a representation RFC.
- Created: 2026-08-16
- Scope: generated C/header artifact ownership and dependency selection
- Depends on: RFC 0034 module artifacts, RFC 0060 root-module entrypoint,
  RFC 0062 generated-header cleanup, RFC 0069 C23-backed helpers
- Coordinates with: RFC 0055 filesystem/build driver, RFC 0052 target profiles,
  `docs/reference.md`, `docs/status.md`

## Decision

Split compiler-owned runtime support out of the generated `hexal.h` and into
demand-driven, feature-owned artifacts under `hexal/`.

`hexal.h` remains the mandatory small program-support header. It is not an
umbrella for String, collection, allocation, or concurrency implementations.
Module headers include only the generated component headers required by that
module.

Author the component artifacts as `.h` and `.c` templates in the repository,
embed them in the Go compiler binary, and render them from checked metadata.
Do not encode complete C declarations or function bodies as Go string
literals.

This is an output-architecture change only. It does not change Hexal syntax,
types, APIs, ownership, traps, C representations, or runtime semantics.

## Motivation

Today one use of a built-in family places that family's complete generated C
surface in `hexal.h`. Every reachable module header includes `hexal.h`, so
every translation unit parses every selected program-wide family.

Measured against the compiler when this ADR was written:

| Reachable program | Generated `hexal.h` |
|---|---:|
| One `Int32` binding | 8 lines, 63 bytes |
| One String plus `print` | 351 lines, 12,100 bytes |

One String use therefore grew the mandatory shared header by approximately
44 times by line count before any additional modules were considered.

Consequences:

- `hexal.h` grows with unrelated library machinery;
- a module pays header cost for features used only by another module;
- runtime declarations, inline implementations, literal storage, and
  program-wide state have no clear owners;
- changing one built-in family causes broad generated-output churn; and
- the generated project does not resemble an ordinary modular C project; and
- substantial C implementations are difficult to read, format, and maintain
  while fragmented across Go `WriteString` calls.

The compiler already discovers built-in use before emission and already
returns an arbitrary `map[string]string` of artifacts. The split therefore
requires no filesystem access and no change to the compiler boundary.

## Non-goals

- No Hexal language-surface change.
- No new built-in library API.
- No filesystem reads, writes, discovery, or path probing.
- No build-driver, compiler-driver, linker, cache, watch, or incremental-build
  behavior.
- No change to generic specialization identity or placement semantics.
- No generic type erasure merely to force typed helpers into one C function.
- No operation-level pruning within one selected family in this RFC.
- No hand-written installed runtime library; all artifacts remain generated
  strings for the current compilation.
- No runtime template loading from the host filesystem.

## Compiler boundary

The public compiler remains exclusively in memory:

```text
Compile(sources: map[string]string, entrypoint: string)
    -> CompilationResult.Files: map[string]string
```

The compiler emits additional entries in `CompilationResult.Files`. It does
not materialize them. RFC 0055 owns writing the map and compiling every emitted
`.c` file.

Repository template files are compiled into the compiler executable through
Go embedding. Reading an embedded asset is not host-filesystem access and does
not weaken the in-memory compiler boundary.

On failure, `Files` remains a non-nil empty map. Artifact generation remains
all-or-nothing and deterministic.

## Artifact contract

### Mandatory artifacts

Every successful compilation emits:

- `hexal.h`;
- `modules/<canonical-module>.h`; and
- `modules/<canonical-module>.c`.

The module pair is emitted once per reachable module as before.

### Optional component artifacts

Emit an optional artifact only when its family is reachable:

| Artifact | Owner |
|---|---|
| `hexal/runtime.c` | definition of `hex_runtime_trap`, when selected |
| `hexal/wrap.h` | selected signed wrapping helpers |
| `hexal/heap.h`, `hexal/heap.c` | Heap representation and allocation runtime |
| `hexal/view.h` | reachable View specializations and bounds/slice helpers |
| `hexal/string.h`, `hexal/string.c` | String/Strand types, literals, UTF-8 and String operations |
| `hexal/error.h` | Error representation |
| `hexal/list.h` | reachable List specializations and typed inline operations |
| `hexal/dict.h` | reachable Dict specializations and typed inline operations |
| `hexal/array.h` | reachable Array specializations and typed inline operations |
| `hexal/concurrency.h`, `hexal/concurrency.c` | selected Task, Channel, Mutex, and Atomic support |

An optional `.c` file is emitted only when it contains at least one definition.
Do not emit empty placeholder component files.

The `hexal/` prefix is mandatory. Never emit a bare `string.h`, `list.h`, or
similar key: it would compete with standard or user headers. Generated code
uses quoted, path-qualified includes such as:

```c
#include "hexal/string.h"
```

Standard C headers retain angle brackets:

```c
#include <string.h>
```

## Repository template layout

The generator owns the editable C/header sources under:

```text
compiler/generator/packages/
├── hexal.h
├── runtime.c
├── wrap.h
├── heap.h
├── heap.c
├── view.h
├── string.h
├── string.c
├── error.h
├── list.h
├── dict.h
├── array.h
├── concurrency.h
└── concurrency.c
```

These are generator inputs, not files that the Go build compiles or that the
core compiler discovers at runtime. Their repository names mirror their
generated logical keys; the generator adds the output `hexal/` prefix where
applicable.

All template assets use their eventual `.h` or `.c` extension. Do not replace
them with `.txt`, Go constants, raw-string constants, or one Go file per C
component.

One small Go owner embeds the directory:

```go
//go:embed packages/*.h packages/*.c
var packageTemplates embed.FS
```

The exact Go identifier is implementation-local. There must be one embedded
asset set, not duplicated constants or fallback copies.

## Template contract

Use Go's standard `text/template` package. Do not create a compiler-specific
template language.

- Parse templates from the embedded filesystem, never from a host path.
- Parse with `missingkey=error`; this protects any intentional map lookup,
  while ordinary missing struct fields already fail template execution.
- A template receives a typed render model owned by its component.
- Discovery, semantic choices, dependency selection, canonical naming,
  ordering, and C escaping remain Go responsibilities.
- Templates format already-validated render data. They do not inspect checker
  nodes or decide whether a feature is legal.
- Limit template control flow to presentation: conditionals for selected
  sections and ranges over pre-sorted render records.
- Keep template helper functions small, deterministic, and formatting-only.
  Do not expose arbitrary generator or checker functions to templates.
- A parse or execution failure is an internal compiler error. Generation fails
  closed and returns no partial artifacts.
- Rendered files end with exactly one newline.

Static C declarations, struct layouts, helper bodies, includes, guards, and
comments belong in the corresponding template file. Go rendering code may
produce only values that are intrinsically generated, including:

- canonical C identifiers;
- checked C type spellings;
- numeric constants and sizes;
- escaped literal payload bytes;
- pre-sorted specialization records; and
- selected declaration/definition records derived from checked source.

Do not reconstruct a whole C function in Go and pass it to a template as one
opaque string. A repeated generated function is represented by typed fields
and rendered structurally by the `.h` or `.c` template.

Small generated module files remain owned by the existing module emitter in
this RFC. This template requirement applies to `hexal.h` and every artifact
under the generated `hexal/` directory. Moving complete module emission to
templates would be a separate refactor.

## `hexal.h` ownership

`hexal.h` owns only program-wide foundations that cannot belong to one
feature component:

- its include guard;
- the demand-driven standard-header prerequisite set established by RFC 0062;
- source-dependent `Size` literal assertions;
- `hex_eos`, when EoS is reachable; and
- the declaration of `hex_runtime_trap`, when a selected path can trap.

`hexal.h` must not:

- include any `hexal/<component>.h`;
- define Heap, View, String, Strand, Error, List, Dict, Array, Task, Channel,
  Mutex, or Atomic;
- contain their helper implementations;
- contain String literal payload storage; or
- contain process-wide runtime state.

Keeping the standard-header prerequisite set in `hexal.h` is intentional for
this RFC. It preserves RFC 0062's verified include discovery while component
ownership changes. Localizing standard includes may be considered separately
after this split is stable.

## Component-header contract

Every `hexal/<component>.h`:

- has a stable `HEXAL_<COMPONENT>_H` include guard;
- includes `"hexal.h"` directly;
- includes only the component headers named by the dependency graph below;
- is emitted once per compilation;
- contains no process-wide mutable state;
- contains no user function or module implementation; and
- is deterministic for equivalent reachable programs.

Component headers may contain:

- public C representations;
- reachable generic specializations;
- prototypes for definitions in the matching component `.c`; and
- small typed `static inline` operations whose implementation depends on the
  concrete specialization.

Their source-of-truth text lives in the matching repository `.h` template.

Do not move a typed helper behind `void *` or a runtime descriptor merely to
reduce a header. A later representation RFC must justify such a change.

## Component-source contract

Every emitted `hexal/<component>.c`:

- includes its matching component header first;
- owns the externally linked definitions and mutable state of that component;
- does not define `main` or module functions;
- does not contain Hexal source `#line` mappings; and
- has no dependency on a host filesystem path.

Its source-of-truth text lives in the matching repository `.c` template.

`hexal/runtime.c` is the sole exception to the matching-header rule: it
includes `"hexal.h"` first because the trap declaration belongs to the core
header and no `runtime.h` is emitted.

Move sizeable representation-independent bodies out of headers when they have
one stable C signature. Retain specialization-dependent bodies as typed inline
helpers unless they can move without type erasure, duplicated state, or a
change to the C representation.

The initial required moves are:

- the trap body from the root module C file to `hexal/runtime.c`;
- Heap allocation/free bodies to `hexal/heap.c`;
- String literal storage and non-specialized String/UTF-8 bodies to
  `hexal/string.c`; and
- concurrency runtime definitions and state from the root module C file to
  `hexal/concurrency.c`.

`hexal/string.h` declares emitted literal objects with external const linkage;
`hexal/string.c` defines each literal byte array and String object exactly
once. Literal ordering and generated names remain the existing canonical
program-wide ordering.

## Split-family ownership

The component split does not pull every helper associated with a feature into
the component artifact. Ownership follows whether the generated declaration
depends on module-owned types or functions.

### Heap

`hexal/heap.h` owns:

- the `hex_heap` representation;
- declarations of the raw allocation and release operations; and
- stable Heap operations whose signatures do not depend on a Hexal module
  type.

`hexal/heap.c` owns their external definitions.

Typed `Heap.allocate<T>` helpers remain in the module header that contains the
allocation site. They are emitted after that module's object/type definitions,
retain `static inline` linkage, and call the raw operations declared by
`hexal/heap.h`. This rule applies to built-in and module-owned T alike, giving
every allocation helper one uniform placement rule.

### Concurrency

`hexal/concurrency.h` owns:

- the Task, Channel, Mutex, and Atomic program-wide type prelude;
- reachable program-wide handle/specialization typedefs; and
- declarations of the scheduler, Task, Channel, and Mutex runtime core.

`hexal/concurrency.c` owns the scheduler platform layer, process-wide state,
Task/join/yield runtime, Channel core, and Mutex core.

The following remain module-owned:

- typed Task join, Channel, Mutex, and Atomic inline helpers;
- spawn argument-frame declarations;
- spawn entry adapters; and
- any helper that names a module-owned parameter or result type.

Typed inline helpers remain in the owning module header after its type
definitions and call the core declared by `hexal/concurrency.h`. Spawn entry
adapters remain in the C file that defines the spawned function, because they
call that function directly. Phase 4 moves only the program-wide prelude,
extern interface, runtime definitions, and runtime state.

## Module-owned output retained

This ADR does not relocate the following existing module-owned families:

- object, ADT, and union definitions;
- shift, bit-cast, endian, division, conversion, equality, and print helpers;
- exported and foreign prototypes;
- typed Heap allocation helpers;
- typed concurrency helpers and spawn argument frames;
- Hexal function and method definitions;
- statement and expression lowering;
- spawn entry adapters; and
- the root module's `main` definition.

They remain in the module header or C file that owns them under the existing
placement rules. They do not become artifacts under generated `hexal/` merely
because they call a component runtime.

## Dependency graph

Dependencies are acyclic and flow downward:

```text
hexal.h
├── wrap.h
├── heap.h
├── view.h
├── string.h       -> heap.h, view.h
├── error.h        -> string.h
├── list.h         -> heap.h, view.h
├── dict.h         -> heap.h, string.h
├── array.h        -> view.h
└── concurrency.h  -> heap.h, error.h
```

Rules:

- Component headers never include module headers.
- Module headers never include one another.
- A component may include only dependencies shown above unless its public C
  declarations prove an additional dependency necessary.
- Adding an edge requires checking that the graph remains acyclic.
- A `.c` file may include additional component headers required only by its
  implementation.
- Do not rely on transitive inclusion when spelling a component dependency.

## Module inclusion

Every module header includes `"hexal.h"` first, followed by the component
headers required by that module, in the dependency order above.

A component is required by a module when any checked declaration, statement,
expression, generated inline helper, prototype, adapter, or emitted module-C
body needs one of its C names. This is module-local discovery, not the
program-wide union.

Examples of the normative selection rule:

- String use in module `a` selects `hexal/string.h` for `a`; it does not make
  unrelated module `b` include it.
- `List<T>` selects `hexal/list.h` plus its declared Heap/View dependencies.
- An Error-bearing signature selects `hexal/error.h`, whose declared
  dependency supplies String.
- A module using only integers and functions includes no component header.

The examples above explain the rule and are not Hexal reference documentation.

Module C files continue to include their own module header. They must not
include a component again when their module header already provides it.

## Generic specializations

This RFC relocates existing generated definitions; it does not redesign
specialization identity or eligibility.

- Reachable View/List/Dict/Array specializations retain their current stable C
  names, layouts, order, and once-per-program identity.
- Their component header replaces `hexal.h` as the program-wide owner.
- A specialization must not be duplicated across module headers.
- Component headers must not require a module header to complete a type.
- If the current representation cannot satisfy the preceding rule for a valid
  specialization, stop that family's migration and record the concrete case;
  do not introduce type erasure, module-header cycles, or duplicate typedefs.
  The unaffected component migrations may still land.

This fail-closed migration rule is deliberate: splitting String and Heap has
high value even if a user-defined generic element exposes a separate existing
representation problem.

## Standard-header requirements

RFC 0062's complete-program requirement discovery remains authoritative.
Moving a declaration or definition must not remove its standard prerequisite.

- The requirement set still covers every emitted header and C file.
- `hexal.h` emits each selected portable standard include once in lexical
  order.
- Conditional platform/runtime include blocks remain owned by the component C
  file that uses them.
- Component files may rely on the explicit `hexal.h` prerequisite umbrella,
  but never on one standard header accidentally including another.

## Linkage and process-wide ownership

- Every external runtime symbol has exactly one declaration and one
  definition.
- Only component C files own component runtime state.
- The root module C file owns only module-root behavior and `main` after this
  migration; it is not the fallback runtime container.
- Header-defined helpers are `static inline` and hold no mutable static state.
- Immutable String literals have one external const definition in
  `hexal/string.c`.
- Existing symbol spellings and runtime behavior remain unchanged unless a
  collision forces a separately reviewed rename.

## Determinism

- Equivalent source maps emit identical keys and contents.
- Optional artifact selection depends only on checked reachable programs.
- Component headers and sources use existing canonical specialization and
  literal ordering.
- Map iteration order must never affect file text or artifact selection.
- Reordering input map insertion must not change any output.

## Workbench presentation

The workbench output area provides a checkbox labelled:

```text
Show generated support files
```

Rules:

- The checkbox is unchecked by default on each fresh workbench load.
- Unchecked hides the output panels for `hexal.h` and every key under
  `hexal/`.
- Checked shows those support files alongside the generated module files.
- Module files under `modules/` remain visible in both states.
- Visibility is presentation-only. Compilation always produces the complete
  `CompilationResult.Files` map.
- Toggling the checkbox updates the current output immediately and does not
  compile again.
- Hidden support files retain their contents and their deterministic artifact
  order.
- Diagnostics, exit status, statistics, source inputs, and compilation behavior
  are unaffected.

## Implementation plan

### Phase 1: emission model

- Add the `compiler/generator/packages/` template assets and one embedded
  template set.
- Add typed component render models and a fail-closed renderer based on
  `text/template`.
- Add component-artifact builders and a per-module component requirement set.
- Reduce `hexalHeader` to its stated ownership.
- Make `GenerateChecked` merge every generated artifact into `Files` and reject
  duplicate logical keys.
- Update the `CompilationResult.Files` contract comment.

### Phase 2: independent components

- Move the static C text for wrapping helpers, Heap, View, String/Strand, and
  Error out of Go string construction and into their component templates.
- Move trap, Heap, and String external definitions into their C owners.
- Retain typed Heap allocation helpers in their module headers.
- Preserve all signatures, layouts, literal names, diagnostics, and traps.

### Phase 3: specialized collections

- Move List, Dict, and Array specialization structure into their templates
  without changing representation; pass typed, pre-sorted specialization
  records from Go.
- Apply the generic-specialization fail-closed rule.
- Do not block already-correct independent components on an unrelated generic
  placement defect.

### Phase 4: concurrency

- Move the program-wide concurrency type prelude, runtime declarations,
  runtime definitions, and state to the concurrency templates and generated
  pair.
- Retain typed inline helpers and argument frames in module headers and spawn
  entry adapters beside their spawned function definitions in module C files.
- Remove the corresponding runtime ownership from the root module C file.

### Phase 5: synchronization

- Add the default-off workbench support-file visibility checkbox.
- Update `docs/reference.md` only with explicit user approval.
- Update RFC 0055 so the future driver compiles every `.c` entry returned in
  `Files`; it must not assume that only `modules/` contains C files.
- Remove this RFC's entry from `docs/status.md` when implementation closes.

## Required tests

### Artifact selection

- A scalar-only program emits `hexal.h` and module pairs, with no `hexal/`
  component artifact.
- Each feature family selects exactly its documented header and transitive
  component dependencies.
- Optional `.c` files are absent when they would be empty.
- An unrelated module does not include a component selected only by another
  module.

### Template source of truth

- Every mandatory runtime template exists under
  `compiler/generator/packages/` with its eventual C/header extension.
- The embedded asset set contains every template exactly once.
- Tests fail if an expected template is absent, cannot parse, or receives a
  render model without a field referenced by the template.
- If a render model intentionally exposes a map, a missing lookup key fails
  because templates use `missingkey=error`.
- No generated-runtime C function body or complete declaration is duplicated
  in a Go string literal.
- Template execution is deterministic for equivalent typed render models.
- A deliberately invalid render model fails without returning a partial
  generated map.

### Ownership

- `hexal.h` contains none of the migrated type or helper names.
- Every migrated name appears in exactly one owning header or source.
- Trap, Heap, String, and concurrency external definitions occur exactly once.
- Typed Heap allocation and concurrency inline helpers remain module-owned and
  are absent from component C files.
- Spawn entry adapters remain beside the spawned function definitions they
  call.
- The root module C contains no migrated runtime definition or state.
- String literal objects are declared once and defined once.

### Includes

- Every module header includes `hexal.h` first.
- Component includes are path-qualified, deterministic, deduplicated, and in
  dependency order.
- No generated file emits `#include "string.h"` or another bare component
  include.
- No component header includes a module header.
- No include cycle exists in the declared component graph.

### Preservation

- Existing focused generated-C assertions are moved to the owning artifact;
  no assertion is weakened or case removed merely because the file changed.
- Existing integration tests preserve language behavior and diagnostics.
- Multi-module generic, String literal, allocation, Error, and concurrency
  cases preserve C symbol spelling and representation.
- Generated output remains deterministic under reordered source-map insertion.
- Failure returns a non-nil empty `Files` map.

### Validation

- `go test ./...`
- `go vet ./...`
- `go vet -tags c23 ./compiler/tests/c23validation`
- No ordinary test invokes an external compiler.

### Workbench

- A successful compile stores all support files even while their panels are
  hidden.
- The initial view shows module output panels and hides `hexal.h` and
  `hexal/*` panels.
- Checking the control reveals every support-file panel without recompiling.
- Unchecking it hides the panels again without losing their contents.
- A subsequent compile respects the current checkbox state.

The dormant C23 package must materialize the complete `Files` map and compile
all returned `.c` files when its future execution lifecycle is restored.

## Reference synchronization

Implementation changes the generated-artifact contract and therefore requires
an approved update to `docs/reference.md` covering:

- the mandatory `hexal.h` and module pairs;
- optional demand-driven `hexal/<component>.h/.c` artifacts;
- `hexal.h`'s reduced ownership;
- module-local component inclusion;
- component linkage and once-per-program ownership; and
- the requirement that a build driver compile every emitted `.c` artifact.

Do not change language syntax or runtime semantic contracts. If implementation
reveals such a need, stop and amend this RFC before proceeding.

The approval requirement is a deliberate project-owner instruction, not an
exemption from synchronization. Implementation must pause before closure,
request approval, and remain open until the approved reference update is made
or the owner explicitly confirms that no reference edit is required.

## Acceptance criteria

- `hexal.h` contains only the foundations assigned to it here.
- Each selected built-in family has exactly one generated owner.
- Unselected families emit no component artifacts or includes.
- One module's private feature use does not pollute unrelated module headers.
- No generated include shadows a C standard header.
- No component/header cycle or duplicate external definition exists.
- Generated C representations, public symbols, traps, and behavior are
  unchanged.
- `CompilationResult.Files` contains every generated artifact as a string;
  the compiler performs no filesystem operation.
- Generated runtime C/header source is maintained in embedded `.c` and `.h`
  templates, not fragmented across Go string literals.
- The workbench hides generated support artifacts by default and can reveal
  them without recompilation.
- Templates contain presentation only; Go retains every semantic and selection
  decision.
- Tests and approved canonical documentation agree before closure.

## Consequences

Positive:

- `hexal.h` becomes small and stable.
- Generated support has explicit ownership.
- Runtime C is readable and editable as C/header-shaped source instead of Go
  string-building statements.
- Translation units include only relevant built-in families.
- Large universal bodies and program-wide state leave headers.
- Future driver behavior is naturally expressed as compiling all returned C
  artifacts.
- Feature-local generated output and tests become easier to inspect.

Costs:

- Successful compilations may return more artifact keys.
- Template parse and execution become a generation failure mode and require
  focused tests.
- Per-module component discovery is more precise than the current program-wide
  umbrella.
- The driver must compile component C files as well as module C files.
- Generic components retain typed header bodies until a safe representation-
  preserving move is available.
