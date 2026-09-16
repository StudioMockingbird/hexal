# RFC 0193: Automatic C Header Binding Generation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. `hexal build` turns every reachable `Alias from c <header>`
  import into a deterministic in-memory RFC 0039 binding module. The driver
  preprocesses each requested header through the pinned Zig backend with the
  target, ordered include roots, definitions, and effective environment,
  decodes the standalone Clang 18-or-newer JSON AST, and normalizes the
  supported declarations into one ordinary binding string under the reserved
  prepared key; the unchanged compiler parses, checks, and lowers it.
  Unsupported declarations are omitted whole; opaque and nullable-pointer
  typedefs resolve inline; records and typedefs are ordered so dependencies
  precede their users; declarations are ordered by source location and exact C
  name. A 64 MiB per-command and 30-second total budget fails closed, as do a
  missing or old Clang and malformed JSON. A missing member of an automatic C
  import reports the binding-guidance diagnostic; an escaped name reports the
  mapped-name diagnostic. Covered by pure-Go discovery, line-index, selection,
  normalization, ordering, escaping, and fail-closed tests plus tagged
  end-to-end builds.
- Created: 2026-09-15
- Scope: let `hexal build` turn reachable `Alias from c <header>` imports into
  deterministic in-memory RFC 0039 binding modules
- Depends on: RFC 0039 (C import syntax and normalized foreign declarations),
  RFC 0192 (C include/define command-line inputs), closed RFC 0052 (installed
  Zig backend), and closed ADR 0055 (filesystem/build driver)
- Coordinates with: deferred RFC 0191 (advanced C interoperability)
- Does not add: a C parser to the core compiler, checked-in binding files,
  project manifests, foreign build-system execution, macros as declarations,
  callbacks, variadics, raw C unions, bit-fields, or flexible arrays

## Summary

Users import a supported C header without repeating its declarations:

```hexal
import
    Adder from c "adder.h"
end
```

The driver asks a separately installed, version-qualified Clang frontend for
the header's typed AST,
normalizes the supported declarations into RFC 0039 source, inserts that source
into a copied in-memory Hexal source map, and calls the ordinary compiler.

The core compiler remains string-in/string-out. It discovers logical C-import
requests and consumes prepared source strings; it never reads a header, starts
a process, or trusts an unchecked foreign representation.

## Problem

RFC 0039 can express an exact C API manually:

```hexal
extern c from "adder.h" do
    fun adder_add(left: Int32, right: Int32): Int32
end

export
    adder_add
end
```

Rewriting an existing header is redundant, creates drift, and is especially
poor for large libraries. Hexal already uses a C frontend to build generated C;
the driver should reuse that frontend's type information instead of adding a
second C parser.

## Goals

- Make direct C-header import the ordinary low-ceremony path.
- Preserve exact C identifiers and ABI spellings.
- Reuse RFC 0039's parser, checker, type model, diagnostics, and lowering.
- Keep manual bindings as the explicit escape hatch and curation mechanism.
- Keep preprocessing facts identical between header inspection and generated C.
- Omit unsupported declarations rather than approximate their ABI.
- Make output independent of frontend traversal and JSON object order.

## Ownership boundary

| Concern | Owner |
| --- | --- |
| `Alias from c <header>` syntax and prepared-source protocol | RFC 0039 / core compiler |
| Header paths, include roots, definitions, target, Zig preprocessing | RFC 0192 / driver |
| Standalone Clang qualification, AST decoding, and RFC 0039 normalization | This RFC / driver |
| ABI validation and C call lowering | RFC 0039 / core compiler |
| C sources, objects, archives, libraries, and final linking | RFC 0192 / driver |

`compiler.Compile` is unchanged. The driver supplies the additional binding
strings through its existing `map[string]string` input.

## End-to-end example

This section is the canonical end-to-end reference for the initial automatic
C-interoperability workflow. RFC 0039 owns the language/compiler pieces and RFC
0192 owns the physical compilation/link pieces; this RFC composes them without
creating another execution path.

`native/adder.h`:

```c
#ifndef ADDER_H
#define ADDER_H

#include <stdint.h>

int32_t adder_add(int32_t left, int32_t right);

#endif
```

`native/adder.c`:

```c
#include "adder.h"

int32_t adder_add(int32_t left, int32_t right) {
    return left + right;
}
```

`app.hex`:

```hexal
import
    Adder from c "adder.h"
end

mut total: Int32 := 0
unsafe do
    total = Adder.adder_add(20, 22)
end
print(total)
```

Build:

```text
hexal build -root ./adder-demo -entry app.hex -out ./adder-demo/build/adder-demo.exe -c-source ./native/adder.c -c-include ./native
```

If the header or implementation depends on preprocessor configuration, the
same flow uses repeatable explicit definitions:

```text
hexal build -root ./adder-demo -entry app.hex -out ./adder-demo/build/adder-demo.exe -c-source ./native/adder.c -c-include ./native -c-define ADDER_STATIC
```

That definition reaches header inspection, generated module compilation, and
foreign-C compilation. It does not enter Hexal source, compiler-owned runtime
components, bundled dependencies, or the core compiler.

The driver internally prepares the equivalent normalized module shown in the
Problem section. It is not written into the project. Generated C includes
`adder.h` and calls the symbol directly:

```c
hex_v_total = adder_add(20, 22);
```

Running the binary writes exactly `42`; `print` adds no newline.

Replacing `adder.c` with an ABI-compatible object or archive changes only the
RFC 0192 option:

```text
-object ./native/adder.obj
```

or:

```text
-archive ./native/adder.lib
```

The header import and Hexal source do not change. Headers supply checked names
and types; C sources, objects, and archives supply linked definitions.

## Discovery and prepared-source protocol

1. The driver reads ordinary Hexal sources under ADR 0055.
2. It calls RFC 0039's pure `DiscoverCImports(sources, entrypoint)` helper.
3. The helper returns only reachable requests in deterministic module order.
4. Equal target/header-form/header-payload requests are prepared once.
5. The driver prepares one binding source for each distinct request.
6. It inserts each source into a copy of the source map under the reserved,
   user-inaccessible key:

```text
hexalc/h<sha256(target NUL header-form NUL header-payload)>.hex
```

7. It calls `compiler.Compile` with the copied map and original entrypoint.

- Quoted and system header forms are distinct identities.
- The key is an in-memory protocol, not a project path or cache key. The
  leading `h` keeps the digest component within Hexal's logical-key grammar;
  source discovery rejects the `hexalc` top-level component for user modules.
- The driver never writes a generated binding `.hex` file.
- A digest collision or mismatched prepared-module identity is a Configuration
  Error.
- A future persistent cache must additionally key every include root,
  definition, frontend identity/version, and normalization version. This RFC
  adds no cache.

## Frontend invocation

For each distinct request, the driver writes one staging translation unit whose
only semantic content is the exact requested include:

```c
#include "adder.h"
```

Inspection is a two-command pipeline:

1. The installed Zig backend preprocesses the staging source as C23 with the
   selected target, ordered `-c-include` roots, ordered `-c-define` values, and
   effective child environment. It retains line markers.
2. A separately installed, version-qualified Clang 18-or-newer executable
   parses that preprocessed `.i` file with the same target and C23 mode and
   emits the typed JSON AST. It receives no include roots or definitions
   because preprocessing has already consumed them.

RFC 0052's qualified target profile maps explicitly to both Zig's target
spelling and Clang's target triple; the driver never assumes the two command
spellings are textually identical.

The initial AST operation is:

```text
clang -target <target> -x c -std=c23 -Xclang -ast-dump=json -fsyntax-only <staging-source.i>
```

A fresh probe established that Zig 0.16's forwarded JSON-AST operation emits
output but exits unsuccessfully; it is not used. Zig remains the C compiler and
linker. The driver resolves `clang` from `PATH`, records its absolute path and
`--version` identity, and rejects an absent or unsupported frontend before
inspection. JSON AST is a frontend interface, not an ISO format; changing the
qualified Clang version requires re-running all importer fixtures.

The driver bounds both preprocessed text and AST output to 64 MiB each and
applies one 30-second deadline to the complete two-command header inspection.
Exceeding any bound reports `C header inspection exceeded the
initial automatic-binding budget; use a smaller wrapper header or a handwritten
binding`. Partial JSON is discarded. These are conservative first-version
bounds, not language limits; changing them requires measured importer evidence.

Zig preprocessing and module/foreign C compilation receive the same
target-relevant interface configuration, include roots, and definitions. The
Clang AST stage consumes the resulting preprocessed text under the same target.
The link receives the same target and effective environment, but no compile-only
options. A header also used by a foreign C source must independently compile
under that source's selected dialect.

The requested header is included by generated C23 translation units. It must
therefore be accepted when included from C23. A legacy header that defines
identifiers newly reserved by C23, or otherwise fails in C23 mode, requires a
small user-supplied compatibility wrapper header. The foreign implementation
source may still compile under its selected older dialect.

## Declaration selection

The initial importer recognizes declarations that RFC 0039 can represent:

- functions;
- complete and opaque records;
- typedefs;
- enums and enumerators; and
- external globals.

Rules:

- Export every supported declaration made visible by preprocessing the
  requested header, including declarations from its transitive includes.
- Exclude compiler-predefined and builtin-only declarations that no included
  header owns. Supported declarations owned by included system headers are
  visible and are imported under the same rule; the importer does not guess
  which includes a library author considers public.
- Include only transitive record, enum, and typedef dependencies required to
  make exported interfaces complete.
- Coalesce transitive declarations by canonical C identity before export.
- Preserve exact C identifiers, ABI type spellings, qualifiers, and record
  completeness.
- Retain callable `static inline` functions supplied by the requested header.
- Omit other internal-linkage functions and objects.
- If a declaration or any required dependency uses an unsupported surface,
  omit that complete declaration. Never emit a partial signature or guessed
  layout.
- Unsupported declarations unrelated to supported ones do not reject the
  entire header.
- Object-like and function-like macros are not imported. They are not ordinary
  typed declaration nodes. A handwritten RFC 0039 `constant` or wrapper is the
  explicit bridge.
- Keep the exact C declaration name when it is a legal, non-protected,
  unambiguous Hexal name. Otherwise apply RFC 0039's deterministic
  `hex_cvar_` naming rules while retaining the exact C spelling for lowering.

Before normalization, redeclarations are coalesced by canonical frontend
identity and compatible C type. Common pairs such as
`typedef struct Vector2 { ... } Vector2`, opaque forward declaration plus later
definition, and repeated compatible function prototypes produce one Hexal
declaration. An incompatible redeclaration fails header inspection; it is
never selected by traversal order.

The initial decoder recognizes the frontend forms required for this subset:
translation units, function/parameter declarations, record/field declarations,
typedefs, enums/enumerators, external variables, qualifiers, canonical type
spellings, storage/linkage, declaration identity/redeclaration links, and source
locations. Unknown unrelated top-level nodes are ignored. An unknown node or
missing fact on the dependency path of a selected declaration makes that
declaration unsupported or fails closed when safe classification is impossible.

An omitted declaration is absent from the module. Access reports the ordinary
automatic-C-import diagnostic defined below. The diagnostic does not claim the
header contains that name; it distinguishes a misspelled C name from a
declaration the automatic subset omitted and gives the valid next actions.

## Automatic import versus handwritten binding

Automatic import is the default. A user writes no `extern c` declarations when
the required header declarations fit RFC 0039's supported automatic subset.

| Need | Required action | Reason |
| --- | --- | --- |
| Supported function, record, typedef, enum, enumerator, external global, or `static inline` function | `Alias from c <header>` only | The importer can recover the complete checked ABI contract |
| Header found through a non-default directory | Add `-c-include`; keep the automatic import | Search configuration is not a binding |
| Conditional declaration or layout | Add matching `-c-define`; keep the automatic import | Inspection and compilation must use one preprocessor configuration |
| C source, object, archive, or system library implementation | Add the corresponding RFC 0192 option; keep the automatic import | Link inputs do not declare Hexal names |
| Ergonomic name different from the exact or generated `hex_cvar_` name, curated export subset, or trusted non-null assertion | Import a handwritten binding module | These are deliberate contract overrides, not facts automatic import may invent |
| Object-like macro used as a value | Write a handwritten `constant` when representable, otherwise expose it through a C wrapper | Macros are not typed declarations in the selected AST |
| Function-like macro used as an operation | Expose it through a C wrapper function | A handwritten foreign function declaration cannot name a macro as a linkable symbol |
| Declaration omitted by the importer but expressible by RFC 0039's normalized forms | Replace the direct C import with a normal import of a handwritten binding module | Manual source supplies the missing checked contract |
| Variadic function, callback/function pointer, raw C union, bit-field, flexible array, unsupported calling convention, or another surface RFC 0039 cannot represent | Write a C wrapper with an RFC 0039-compatible interface and import the wrapper header, or wait for deferred RFC 0191 | Handwriting the same unsupported ABI does not make it representable |
| Supported declaration exposed through an umbrella header | Use the umbrella import | Its visible transitive declaration surface is imported and coalesced |

An application chooses one module source for one alias. A handwritten binding
replaces the direct C import; it is not an untracked supplement merged into an
automatic module.

Example fallback module, `bindings/adder.hex`:

```hexal
extern c from "adder.h" do
    fun add as "adder_add"(
        left: Int32,
        right: Int32,
    ): Int32
end

export
    add
end
```

The application then uses an ordinary Hexal import:

```hexal
import
    Adder from "./bindings/adder.hex"
end

mut total: Int32 := 0
unsafe do
    total = Adder.add(20, 22)
end
print(total)
```

The C header and RFC 0192 build inputs remain necessary. The handwritten module
replaces declaration discovery only; it does not replace the physical header
or linked implementation.

## Guidance diagnostic

Name resolution knows whether an imported module came from a prepared C-header
request. Accessing a missing member of such a module reports:

```text
Name Error: C import <header> has no automatically imported declaration <name>; check the C name, use a handwritten binding, or expose a C wrapper
```

When `<name>` is the exact C name of a declaration that required escaping, use
the more precise diagnostic:

```text
Name Error: C declaration <C-name> is imported as <Hexal-name>
```

- This diagnostic replaces the generic unknown-export diagnostic only for a
  qualified lookup through an automatic C-import alias.
- It does not assert that the physical header contains `<name>`.
- A typo and an omitted declaration intentionally receive the same guidance;
  deciding which occurred would require importing unsupported declarations as
  trusted compiler facts.
- A handwritten binding that attempts an unsupported ABI receives RFC 0039's
  precise unsupported-declaration/type diagnostic. The user must then use a C
  wrapper or wait for the owning interop extension.
- Unknown members of ordinary Hexal modules retain the ordinary diagnostic.
- Unsupported declarations include the valid next action. A declaration that
  RFC 0039 can express names a handwritten binding; one that it cannot express
  names a C wrapper. An unprovable handwritten C spelling directs the user to
  automatic import or a C wrapper instead of reporting only a generic Type
  Error.

## Normalization

- Produce ordinary RFC 0039 source with one `extern c from` block. Add a final
  export block only when at least one supported root declaration is exported.
- Apply RFC 0039's target integer mapping, pointer qualification,
  nullable-pointer default, opaque/complete record rules, and unsafe rules.
- Order required type dependencies before their users.
- Order otherwise independent declarations by source location, declaration
  kind, and exact C name.
- Frontend traversal order and JSON object order have no effect.
- Pass the result through the ordinary Hexal parser, checker, ABI validation,
  and generator. There is no trusted binding AST or alternate lowering path.
- Keep driver-private provenance from each normalized declaration to its
  physical header location so a normalization/checking failure names the C
  declaration and header rather than only the opaque prepared-source key. This
  diagnostic map is transient build state, not a second semantic manifest.

A handwritten binding remains valid when users need renaming, macro values,
wrappers, a curated subset, or an unsupported automatic declaration.

## Failures

- Invalid CLI configuration fails before header inspection under RFC 0192.
- Header lookup, preprocessing, frontend execution, malformed JSON, and AST
  decoding failures are C-compilation-stage failures and record the exact
  frontend command.
- Missing Clang or Clang older than 18 is Configuration Error
  `automatic C imports require clang 18 or newer on PATH` before staging.
- Reserved-key collisions and prepared-identity mismatches are Configuration
  Errors.
- Invalid normalized Hexal source or ABI declarations are ordinary Hexal
  compilation diagnostics. The driver does not bypass them.
- The earliest failed stage stops the build; later compilation and linking do
  not run.

## Accepted limitations

- Only RFC 0039's initial declaration subset is automatic.
- Macros require manual bindings or C wrapper functions.
- Missing members receive the automatic-C-import binding-guidance diagnostic;
  the compiler does not claim whether the name is misspelled or omitted.
- One header request produces one logical module containing its supported
  visible transitive declaration surface; selective imports are future work.
- The installed frontend and its JSON schema are qualified implementation
  dependencies.
- No binding file is persisted or displayed by default.
- Automatic inspection is bounded to 64 MiB each for preprocessed and AST
  output and 30 seconds total per header. A smaller wrapper header or
  handwritten binding is required when a header exceeds any initial budget.

## Required sweep

Inventory and reconcile:

- compiler import grammar and module-graph handling for prepared C imports;
- the pure reachable-import discovery helper;
- driver source-map copying and reserved-key insertion;
- Zig preprocessing plus standalone-Clang JSON-AST command construction and
  command recording;
- AST declaration-origin filtering, dependency closure, normalization, and
  deterministic ordering;
- qualified missing-member diagnostics for automatic C-import aliases;
- RFC 0039 manual-binding fixtures that should remain explicit fallback tests;
- RFC 0192 include-root, define, environment, target, and failure propagation;
  and
- CLI help only if it describes automatic header behavior.

Do not add C parsing, filesystem access, subprocess execution, or frontend JSON
types to the core compiler. Do not change `compiler.Compile`,
`compiler.Project`, RFC 0039 normalized syntax, or RFC 0192 link planning.

## Detailed implementation plan

### Phase 0: baseline and fixtures

1. Land RFC 0039 and RFC 0192.
2. Resolve and version-qualify standalone Clang 18 or newer, prove the exact
   Zig-preprocess/Clang-AST pipeline exits successfully, and pin its minimal
   adder output.
3. Pin the normalized binding text and existing handwritten-binding output.
4. Record ordinary build commands, staging cleanup, and publication behavior.

### Phase 1: pure discovery protocol

1. Implement `DiscoverCImports` using the existing parser/module graph.
2. Return reachable requests in deterministic module order.
3. Implement exact identity, deduplication, reserved-key derivation, collision
   checks, and prepared-module identity validation.
4. Add pure-Go tests with no external frontend.

### Phase 2: frontend seam

1. Add a backend operation that preprocesses the requested header with Zig and
   parses the staged `.i` with standalone Clang for JSON AST.
2. Give Zig RFC 0192's target, ordered include roots, definitions, and
   normalized effective environment; give Clang the same target and C23 mode.
3. Keep stdout and stderr separate and record the exact command.
4. Enforce both 64 MiB byte limits and the shared 30-second deadline; terminate
   the active process and discard all partial output on any limit.
5. Decode into driver-private frontend records; do not expose frontend JSON
   types through compiler APIs.
6. Implement the explicit supported-node inventory, canonical declaration
   coalescing, and fail-closed handling for unknown required shapes or facts.

### Phase 3: selection and normalization

1. Identify supported declarations visible through the requested header's
   complete preprocessed include graph while excluding compiler builtins.
2. Compute the transitive type dependency closure.
3. Classify supported, unsupported-independent, and unsupported-required
   declarations.
4. Normalize supported declarations to deterministic RFC 0039 source.
5. Preserve exact ABI spellings and apply only the settled `hex_cvar_` escape;
   add no convenience wrappers.
6. Retain automatic-C-module origin on the checked import alias and specialize
   only its missing-member diagnostic.
7. Test output against pinned fixtures and shuffled AST input order.

### Phase 4: driver integration

1. Discover requests after ordinary source discovery.
2. Prepare each distinct request once.
3. Insert bindings into a copied source map under reserved keys.
4. Compile through the unchanged public compiler API.
5. Propagate generated includes into RFC 0192's existing compile/link path.
6. Preserve cleanup and atomic executable publication on every failure.

### Phase 5: conformance

1. Add pure-Go command, AST-decoding, filtering, normalization, identity, and
   failure tests.
2. Add tagged external fixtures for direct header import, header-only static
   inline use, include roots, definitions, omitted unsupported declarations,
   required unsupported dependencies, and frontend failure.
3. Build and run the complete adder example; assert output and direct C call.
4. Verify an equivalent handwritten scalar binding produces the same ABI
   declarations and call shape under the same logical binding identity.
5. Verify builds without C imports retain their command and artifact behavior.
6. Update `docs/status.md`; review `docs/reference.md` only with explicit user
   approval.
7. Rebuild `hexal` and restart `hexal play` before handoff.

## Validation

This list is exhaustive.

### Discovery and identity

- Only reachable C imports are returned, in deterministic module order.
- Equal target/header-form/header-payload requests prepare one binding.
- Quoted/system forms and different targets produce different identities.
- Missing, mismatched, and colliding prepared entries report their specified
  Configuration Errors.
- Original source maps are not mutated and no binding file is written.

### Frontend and normalization

- The adder header normalizes to the exact expected function and export.
- A header with no supported visible declaration normalizes without an empty
  export block.
- Missing Clang and Clang older than 18 fail with the exact Configuration Error
  before preprocessing or staging.
- Zig preprocessing receives the target, include roots, definitions, and
  effective environment; Clang receives the same target and the staged `.i`
  file without repeating include or definition options.
- Include roots and definitions match header inspection plus generated-module
  and foreign-C compilation, never runtime/dependency compilation; target and
  effective environment match every relevant compile and link invocation.
- Supported declarations visible through root and transitive includes are
  exported and coalesced; compiler builtin declarations are not; required
  dependent types are retained.
- Supported functions, complete/opaque records, typedefs, enums/enumerators,
  globals, and `static inline` functions each have a focused fixture.
- A supported declaration beside unrelated unsupported declarations remains
  available.
- A declaration depending on an unsupported type is omitted whole.
- Macros and non-inline internal-linkage declarations are not imported.
- Shuffled frontend traversal and JSON object order produce identical binding
  text.
- A typedef-plus-record definition, a forward declaration plus definition, and
  repeated compatible prototypes each normalize once; incompatible
  redeclarations fail deterministically.
- A legacy implementation may compile under C11/C17, but its imported wrapper
  header is independently accepted under C23; a deliberately C23-incompatible
  header fails with guidance to provide a compatibility wrapper.
- Unknown required AST shapes fail closed rather than producing guessed source.
- A keyword, protected name, leading-underscore name, tag/ordinary-namespace
  collision, and collision with an existing `hex_cvar_` name receive RFC
  0039's exact deterministic spelling. Looking up the original C name reports
  the mapped-name diagnostic.
- Preprocessed or AST output above 64 MiB and total inspection exceeding 30
  seconds fail with the exact budget diagnostic, discard partial source, and
  run no later stage.
- A missing qualified member of an automatic C module reports the exact
  guidance diagnostic; an ordinary module retains its ordinary unknown-export
  diagnostic.
- A handwritten declaration using an unsupported ABI reaches RFC 0039's exact
  unsupported diagnostic rather than being accepted because it is manual.

### End-to-end and boundaries

- The complete example contains no handwritten binding, builds, writes `42`,
  and emits a direct `adder_add` call with no wrapper or marshalling allocation.
- Replacing its C source with an ABI-compatible object or archive changes only
  the RFC 0192 input and produces the same result.
- A header-only `static inline` fixture builds without a foreign source.
- An equivalent handwritten scalar binding follows the same checker and
  generator path and emits the same ABI shape under the same logical identity.
- A frontend failure records the exact command and prevents compilation,
  linking, and output replacement.
- Ordinary Go tests require no Zig or Clang installation; tagged tests use the
  qualified installed Zig backend and standalone Clang frontend.
- The core compiler performs no filesystem access, process invocation, raw C
  parsing, or link planning.

## Settled decisions

- Direct C-header import is the ordinary path; handwritten RFC 0039 bindings
  are the fallback.
- A separately version-qualified standalone Clang frontend supplies typed C
  facts; Zig continues to compile and link; Hexal does not parse C itself.
- Prepared bindings are ephemeral Hexal source strings using the ordinary
  compiler pipeline.
- Automatic imports preserve exact C spellings, escape only unusable Hexal
  names with `hex_cvar_`, and do not import macros.
- Unsupported declarations are omitted whole; supported independent
  declarations remain usable.
- Missing names on automatic C modules receive binding guidance without
  claiming that the header actually declares the requested name.
- Handwritten bindings replace, rather than merge into, one automatic module
  alias and cannot bypass RFC 0039 ABI restrictions.
- Header inspection plus generated-module and foreign-C compilation share
  interface include roots and definitions. All external stages share the
  selected target and effective environment; link receives no compile-only
  option.
- Visible transitive declarations make umbrella headers useful.
- A 64 MiB/30-second per-header budget bounds the uncached initial importer.
- No project manifest or persistent binding cache is introduced.

## Open questions

None.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. During implementation, add the
direct C-import syntax and prepared-module semantic contract only with explicit
user approval. Driver/frontend mechanics do not belong in the language
reference.
