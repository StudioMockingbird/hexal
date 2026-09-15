# RFC 0192: Command-Line C Build Inputs

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready after RFC 0039; design and execution plan settled, implementation not started
- Created: 2026-09-15
- Scope: let `hexal build` compile and link explicitly supplied C sources,
  objects, static archives, and system libraries with an explicit build
  environment and without a project manifest
- Depends on: RFC 0039 (typed C binding modules), closed RFC 0052 (installed
  Zig backend), and closed ADR 0055 (filesystem/build driver)
- Coordinates with: RFC 0187 (build modes), RFC 0193 (automatic C-header
  binding generation), deferred RFC 0164 (object cache), and deferred RFC 0191
  (advanced C interoperability)
- Does not add: project manifests, package discovery, C header parsing in the
  core compiler, foreign build-system execution, runtime dynamic loading, or
  incremental compilation

## Summary

`hexal build` accepts every initial foreign implementation input through
repeatable command-line options, compiles foreign C sources separately from
Hexal-generated C23, and links all resulting objects and libraries into the
final program. External commands inherit the environment that launched Hexal;
repeatable `-c-env NAME=VALUE` options provide deterministic per-build
overrides without requiring a project manifest.

The core compiler remains string-in/string-out. It never receives an object,
archive, library, macro, compiler argument, or host path. RFC 0193 separately
owns automatic preparation of binding source strings.

A later project-manifest RFC may provide persistent equivalents of these
options. This RFC introduces no manifest and does not reserve a manifest
format.

## End-to-end interop model

C interoperability has three distinct inputs. None substitutes for another:

| Input | Role | Owner |
| --- | --- | --- |
| Prepared or handwritten Hexal binding module | Declares the C names and their checked Hexal types | RFC 0039 and RFC 0193 |
| C header | Supplies the declarations included by generated C | Selected C frontend |
| C source, object, archive, or library | Supplies the linked symbol definitions | This RFC and the build driver |

- `Alias from c <header>` is a source-level request governed by RFC 0039. RFC
  0193 owns automatic preparation from a physical header.
- Ordinary Hexal module imports still name only logical `.hex` modules. A `.c`,
  `.o`, `.obj`, `.a`, or `.lib` file is never a language module.
- A binding module uses RFC 0039's normalized
  `extern c from <header> do ... end` representation internally. A handwritten
  module using that syntax remains the explicit fallback.
- `extern c from` causes generated C to contain the corresponding `#include`.
  It does not locate the header or supply a symbol definition.
- `-c-include` tells the C frontend where to search for a named header. It
  imports no Hexal names and forces no include by itself.
- `-c-source`, `-object`, and `-archive` supply implementations at build/link
  time. They introduce no Hexal names or types.
- `-system-library` supplies a target library by logical name. It likewise
  introduces no Hexal declarations.
- This RFC supplies physical headers and implementations to the backend. RFC
  0193 supplies automatic header-to-Hexal binding generation.

### Complete minimal example

Project layout:

```text
adder-demo/
    app.hex
    native/
        adder.h
        adder.c
```

`native/adder.h` declares the C API:

```c
#ifndef ADDER_H
#define ADDER_H

#include <stdint.h>

int32_t adder_add(int32_t left, int32_t right);

#endif
```

`native/adder.c` supplies its implementation:

```c
#include "adder.h"

int32_t adder_add(int32_t left, int32_t right) {
    return left + right;
}
```

`app.hex` imports the header, calls C inside the required lexical unsafe region,
and prints the result:

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

The alias exposes the header's exact C identifiers, so `adder_add` remains
`Adder.adder_add`. Automatic imports do not invent friendlier names.

`total` is declared outside `unsafe` because an unsafe block is an ordinary
lexical scope. The foreign call is unsafe; consuming the resulting checked
`Int32` and printing it are ordinary safe operations.

From the `adder-demo` parent directory, the complete build is:

```text
hexal build -root ./adder-demo -entry app.hex -out ./adder-demo/build/adder-demo.exe -c-source ./native/adder.c -c-include ./native
```

Every relative foreign path is resolved against `-root`, so the driver resolves
the last two arguments to `adder-demo/native/adder.c` and
`adder-demo/native`. With RFC 0193 implemented, the build performs, in order:

1. read the reachable `.hex` files and discover `Adder from c "adder.h"`;
2. let RFC 0193 preprocess and inspect `adder.h` using
   the same target, include roots, and definitions as generated C;
3. let RFC 0193 normalize `adder_add` into an in-memory RFC 0039 binding module and add it to
   the copied source map under its deterministic reserved key;
4. compile that complete source map in memory into generated C23 artifacts;
5. compile each generated Hexal translation unit as C23;
6. compile `native/adder.c` using the selected foreign-C dialect;
7. link the generated objects and the `adder.c` object; and
8. atomically publish `adder-demo.exe`.

The relevant generated call is direct:

```c
hex_v_total = adder_add(20, 22);
```

No forwarding wrapper, dynamic lookup, marshalling allocation, or runtime
dispatch is generated. Running the executable writes exactly:

```text
42
```

Replacing `adder.c` with a compatible precompiled input changes only the build
option:

```text
hexal build -root ./adder-demo -entry app.hex -out ./adder-demo/build/adder-demo.exe -c-include ./native -object ./native/adder.obj
```

or, for a static library:

```text
hexal build -root ./adder-demo -entry app.hex -out ./adder-demo/build/adder-demo.exe -c-include ./native -archive ./native/adder.lib
```

The automatic header import and application source remain identical because the
ABI is identical; only the source of the linked implementation changes.

## Problem

RFC 0039 can represent a C API and emit the corresponding include and symbol
use. RFC 0193 removes manual declaration repetition. The build still needs to
know:

- where `raylib.h` can be found;
- which C source files must be compiled, if any;
- which precompiled objects or static archives must be linked;
- which preprocessor definitions configure the foreign API;
- which C dialect the foreign source uses; and
- which system libraries and exceptional compiler/linker options it needs.

ADR 0055 deliberately excluded these inputs. Without this RFC, the driver has
no executable interface for supplying and linking a foreign implementation.

## Goals

- Build a small C library directly from source beside a Hexal program.
- Use a header-only C library.
- Link explicitly supplied `.o`, `.obj`, `.a`, or `.lib` inputs.
- Link named system libraries required by a C API.
- Keep header search, macros, target, and ABI consistent across generated and
  foreign translation units.
- Permit older C dialects for foreign sources while generated Hexal remains
  C23.
- Preserve deterministic command construction, failure ownership, staging,
  and atomic executable publication.
- Make the command-line model directly translatable into a later manifest.

## Command-line surface

All options belong to `hexal build`. Options marked repeatable preserve their
command-line occurrence order.

| Option | Cardinality | Meaning |
| --- | --- | --- |
| `-c-source <path>` | Repeatable | Compile one foreign C translation unit and link its object |
| `-c-include <path>` | Repeatable | Add one header search directory to generated module and foreign C compilations |
| `-c-define <name[=value]>` | Repeatable | Define one preprocessor macro for header inspection plus generated module and foreign C compilations |
| `-c-env <name=value>` | Repeatable | Override one inherited environment variable for every C frontend, compiler, and linker invocation |
| `-c-standard <dialect>` | At most once | Select the dialect for every foreign C source; default `c17` |
| `-object <path>` | Repeatable | Link one precompiled object |
| `-archive <path>` | Repeatable | Link one static archive |
| `-system-library <name>` | Repeatable | Link one target toolchain or operating-system library by logical name |

Existing `-root`, `-entry`, and `-out` behavior is unchanged.

The CLI accepts either `-option value` or Go flag package's existing
`-option=value` spelling. A repeatable option is represented internally by a
small ordered string-list flag value; comma splitting is never performed.

### External command environment

- Every header frontend, generated-C compiler, foreign-C compiler, and linker
  process starts from the environment inherited by the `hexal` process.
- Each `-c-env NAME=VALUE` replaces the inherited value of `NAME` for all those
  processes. The split occurs at the first `=`; the value may be empty or
  contain additional `=` characters.
- A name is nonempty and contains neither `=` nor NUL. A value contains no NUL.
- Repeating one name is rejected rather than using occurrence order. Name
  equality follows the execution host: ASCII case-insensitive on Windows and
  byte-sensitive on POSIX.
- Overrides apply uniformly to header inspection, generated Hexal C,
  foreign C, and linking. A build cannot give those stages different `PATH`,
  SDK, include, library, or toolchain environments.
- The driver does not expand `$NAME`, `%NAME%`, `~`, command substitutions, or
  path separators inside a value. The child process receives the exact value
  supplied after normal CLI argument decoding.
- Explicit output and input path resolution remains driver-owned. Other
  environment-sensitive tool behavior, including implicit include/library
  search and Zig cache or library directories, may change when the user
  explicitly overrides the responsible variable. That loss of hermeticity is
  an accepted first-version escape-hatch cost and is surfaced through the
  recorded override name.
- Overriding `PATH` changes only the child process environment. It does not
  change the already selected/version-qualified backend executable or Hexal's
  source and output path resolution.
- The driver constructs one normalized effective environment and orders its
  entries deterministically before process invocation. This removes duplicate
  host keys without changing environment-variable semantics.
- Command diagnostics list the names of explicit overrides but do not print
  their values. Environment values may contain credentials or private paths.
  Private command-plan tests may inspect values; user-facing
  `BuildResult.Commands` and rendered failures must not expose them.
- This RFC adds no environment file, shell script evaluation, allowlist, clean
  environment mode, or environment-variable discovery. Arbitrary explicit
  overrides, including toolchain-control variables, are accepted as a
  deliberate first-version escape hatch. A future hardening RFC must classify
  or replace them before builds claim hermetic toolchain identity. A future
  manifest may store the same explicit overrides.

Invalid forms use these value-safe diagnostics:

```text
Configuration Error: -c-env requires NAME=VALUE
Configuration Error: invalid C environment variable name <name>
Configuration Error: invalid C environment value for <name>
Configuration Error: C environment variable <name> is repeated
```

### Example: C source and header

```text
hexal build \
  -root ./game \
  -c-source ./vendor/widget/widget.c \
  -c-include ./vendor/widget/include \
  -c-define WIDGET_STATIC \
  -c-standard c11
```

Hexal source requests automatic binding preparation:

```hexal
import
    Widget from c <widget.h>
end
```

`-c-include` makes the header available. `-c-source` supplies the symbol
implementation. Neither option independently imports names into Hexal; the
source-level C import requests the prepared compiler-visible binding.

### Example: precompiled library

```text
hexal build \
  -root ./game \
  -c-include ./vendor/raylib/include \
  -archive ./vendor/raylib/lib/libraylib.a \
  -system-library user32 \
  -system-library gdi32 \
  -system-library winmm
```

The archive supplies Raylib. The named libraries supply its Windows system
dependencies. Runtime loading with `dlopen`, `LoadLibrary`, or equivalent is a
different capability owned by deferred RFC 0191.

## Path contract

- `-root` is resolved as ADR 0055 specifies.
- Every new relative foreign-input path (`-c-source`, `-c-include`, `-object`,
  and `-archive`) is resolved against the resolved source root, not the
  invocation working directory. Existing `-entry`, `-out`, and `-root`
  semantics remain unchanged.
- Absolute paths are accepted because installed SDKs and prebuilt libraries
  commonly live outside the project tree.
- An explicitly supplied path may traverse a symlink or Windows junction. It
  is trusted user input, not source discovery.
- The driver cleans and absolutizes every path before invoking the backend.
- A source, include directory, object, or archive that does not exist reports a
  filesystem-stage failure before any external command runs.
- A source, object, or archive path must name a regular file. An include path
  must name a directory.
- Extensions are descriptive, not authoritative. The driver does not reject a
  valid toolchain input solely because its filename lacks a conventional
  extension.
- Inputs are read or passed only by the driver. Their contents never enter the
  core compiler's `sources` map.

The driver performs no recursive discovery beneath a C directory. A C project
is represented initially by explicitly repeating `-c-source` for its required
translation units and supplying its configuration options. CMake, Meson,
Make, pkg-config, and vendor-specific project adapters are future work.

## Header and preprocessor contract

- RFC 0039 binding declarations remain the sole source of generated `#include`
  directives. RFC 0193 may prepare those declarations automatically.
- `-c-include` adds an ordered backend include-search argument. It does not
  force a header into any translation unit.
- `-c-define` applies identically to generated module translation units,
  foreign C compilations, and automatic header inspection. It does not apply
  to compiler-owned `hexal/` runtime components or bundled third-party
  dependencies.
- `-c-include` follows the same scope. Compiler-owned staging and dependency
  include roots precede user roots so user input cannot shadow `hexal.h` or a
  bundled component header.
- A macro name matches `[A-Za-z_][A-Za-z0-9_]*`.
- A macro value is one nonempty command-line argument suffix after the first
  `=`. The driver does not parse it as C source.
- Repeating a macro name is rejected as a configuration error. The user must
  provide one unambiguous definition.
- Header existence and compatibility are ultimately checked by the C
  frontend. RFC 0039's core compiler never searches include directories.
- RFC 0193 consumes the same ordered include roots and definitions when it
  prepares a binding, so inspection and compilation cannot interpret one
  conditional interface differently.

## Foreign C compilation

- Each `-c-source` is compiled separately to one object in the fresh staging
  tree.
- Generated Hexal translation units always use `-std=c23` and the exact
  qualified target profile.
- Foreign C translation units use the same target profile and selected build
  mode, plus `-std=<c-standard>`.
- The accepted initial dialects are `c89`, `c99`, `c11`, `c17`, `c23`, and
  their `gnu89`, `gnu99`, `gnu11`, `gnu17`, and `gnu23` forms. The driver passes
  the selected spelling to Zig and fails if Zig rejects it.
- The initial default is `c17`: modern enough for ordinary libraries without
  claiming that foreign source is C23.
- One build has one foreign-source dialect. Per-source dialects are deferred
  to the future manifest or a focused CLI extension.
- `-c-include` and `-c-define` are the only user-controlled foreign compile
  options in this version. Raw compiler arguments are not accepted.
- Foreign C sources receive optimization and debug-information choices from
  the selected build mode, but not Hexal's UBSan instrumentation. UBSan is a
  backstop for generated Hexal C and is not imposed on unmodified third-party
  code.
- Every source receives its own deterministic staging object name derived from
  its normalized absolute path and its zero-based occurrence ordinal. Equal
  basenames never collide.
- The same normalized C source path may appear only once.
- A compilation failure reports the existing C-compilation stage and exact
  command record. Later sources and linking do not run.

## Linking

The final link argument groups are ordered as follows:

1. generated Hexal objects in their existing deterministic order;
2. compiled foreign-source objects in `-c-source` occurrence order;
3. `-object` inputs in occurrence order;
4. `-archive` inputs in occurrence order;
5. `-system-library` inputs in occurrence order.

- The driver preserves repeated archives and system libraries because static
  link resolution can make repetition meaningful.
- Duplicate object inputs are rejected after normalized path comparison;
  linking one object twice is not a useful operation.
- `-system-library` accepts a nonempty logical name containing ASCII letters,
  digits, `_`, `-`, `.`, or `+`. It does not accept a path. Use `-archive` or
  `-object` for a concrete file.
- The backend translates a system-library name to its native Zig/Clang linker
  form. The CLI does not synthesize platform filenames.
- Link-time use of operating-system libraries or import libraries is allowed.
  Runtime library discovery, loading, symbol lookup, and unload remain deferred
  RFC 0191 work.
- The final executable retains ADR 0055's atomic publication rule.

## Raw tool arguments

`-c-arg` and `-link-arg` do not exist in this version. They obscure ABI and
pipeline ownership and make a fail-closed classifier larger than the feature
it protects. Add a focused named option when a real library requires one. A
future raw-argument escape hatch must define an exact allowlist and must not
duplicate inputs, output, target, dialect, preprocessing, staging, dependency,
or build-mode ownership.

## Build options and internal ownership

`internal/driver.BuildOptions` gains ordered fields corresponding to the CLI:

```go
type BuildOptions struct {
    Root            string
    Entrypoint      string
    OutDir          string
    Output          string
    Mode            BuildMode
    CSources        []string
    CIncludeDirs    []string
    CDefines        []string
    CEnvironment    []string
    CStandard       string
    Objects         []string
    Archives        []string
    SystemLibraries []string
}
```

These fields are driver configuration. They are never copied into
`compiler.Project`; that value contains only facts that can change Hexal
checking or generation, such as the selected target.

`CEnvironment` stores only explicit CLI overrides. The normalized inherited
environment belongs to the driver command plan and is not copied into
`BuildOptions`, `compiler.Project`, or compiler input.

`CommandResult` gains only the non-secret override names:

```go
type CommandResult struct {
    // Existing fields remain unchanged.
    EnvironmentOverrides []string
}
```

The implementation updates the existing comment that says command records do
not capture the complete environment: records remain intentionally incomplete
and must no longer claim that their argument vector alone fully reproduces an
environment-dependent failure.

The driver owns normalization and validation. The backend owns only direct
tool invocation and version qualification; it does not discover dependencies
or reinterpret project policy.

The current content-derived staging identity is not reused for a build with
foreign inputs because it omits foreign file bytes, options, headers, and the
effective environment. Each such invocation receives a fresh private staging
identity. Atomic executable publication remains unchanged. Deferred RFC 0164
owns reusable content-addressed identity and must include every foreign input
and transitive header dependency before caching them.

## Failure ownership

- Invalid, duplicate, conflicting, or unsafe command-line/environment
  configuration fails at the configuration stage.
- Missing or wrong-kind paths and unreadable files fail at the filesystem
  stage.
- Header lookup and foreign-source compiler failures use the C-compilation
  stage and record the exact frontend command. RFC 0193 owns preprocessing,
  AST, and prepared-binding failures.
- Incompatible objects, missing symbols, missing libraries, and archive-order
  failures use the link stage.
- The earliest failed stage stops the build. No later command runs and no
  existing published executable is replaced.
- External stdout, stderr, exit status, working directory, tool, and exact
  argument vector remain recorded in `BuildResult.Commands`.

## Accepted limitations

- Large C projects require many repeated flags until a project manifest or
  build-system adapter exists.
- Builds inherit ambient environment values unless explicitly overridden.
  Fully hermetic clean-environment builds are deferred.
- Environment configuration does not run CMake, Meson, Make, pkg-config,
  configure scripts, or vendor build scripts. The user supplies any files those
  systems would have generated.
- All foreign source files use one C dialect per build.
- The driver does not derive transitive library dependencies.
- The user supplies static libraries in a linker-valid order.
- The installed Zig toolchain decides which object and archive formats are
  compatible with the selected target.
- Only already-qualified Hexal targets may execute this path. This RFC does not
  qualify another host or target.
- A dynamically linked system dependency can prevent a physically standalone
  binary. Static objects and archives remain the portable single-binary path.

## Required sweep

Inventory and reconcile:

- `cmd/hexal` build usage, option parsing, repeated-flag support, and error
  rendering;
- `internal/driver.BuildOptions`, inherited-environment normalization,
  secret-safe override reporting, path resolution, staging, compilation order,
  link order, command recording, cleanup, and publication;
- `CommandResult` documentation and every formatter/fixture that assumes an
  argument vector alone reproduces an external invocation;
- `internal/backend` compile and link operations without moving driver policy
  into the backend;
- driver and CLI tests that assume generated files are the only C inputs;
- RFC 0039 tagged fixtures that currently arrange headers or objects directly
  in test scaffolding;
- build-mode propagation from RFC 0187; and
- help/version snapshots affected by the expanded command surface.

Do not change the `compiler.Compile` signature, `compiler.Project`, ordinary
source discovery, RFC 0039 normalized declaration syntax, or generated-C23
dialect selection.

## Detailed implementation plan

### Phase 0: baseline and seams

1. Land RFC 0039 and record the current CLI help, driver command records,
   staging contents, cleanup behavior, and atomic-publication tests.
2. Identify the backend's compilation and link argument builders and keep
   their tool-execution boundary unchanged.
3. Add focused flag-list and path-normalization helpers with no build behavior.

### Phase 1: configuration model

1. Add the ordered fields to `BuildOptions`.
2. Register every CLI option, including repeatable options, and expand help.
3. Parse and validate environment overrides, merge them over the inherited
   environment using host key semantics, and produce one deterministic child
   environment without exposing values in public command records.
4. Normalize path inputs relative to the resolved source root.
5. Validate existence, kind, duplicate rules, macro syntax, dialect, and system
   library names before staging or tool invocation.
6. Implement the fresh foreign-input staging identity before compiling a
   foreign source or accepting a prebuilt object/archive.
6. Preserve the existing defaults when no foreign option is supplied.

### Phase 2: headers and foreign sources

1. Add common include and define arguments to header inspection plus generated
   module and foreign compile commands; exclude runtime/dependency commands.
2. Apply the one effective environment to generated and foreign compilation.
3. Add a foreign-source compile operation using the selected dialect, target,
   and build mode.
4. Generate collision-free deterministic staging object names.
5. Compile foreign sources sequentially in occurrence order and append each
   command record.
6. Preserve generated translation-unit compilation and its C23 options.

### Phase 3: objects and linking

1. Add validated object and archive paths to the final link plan without
   copying their contents into the compiler.
2. Add ordered system-library translation in the backend.
3. Apply the same effective environment to the linker.
4. Assert the complete five-group ordering and preserve repeated libraries.
5. Retain separate link failure reporting and atomic publication.

### Phase 4: conformance

1. Add pure-Go option, validation, ordering, fencing, and
   command-construction tests that invoke no external tool.
2. Add tagged C23 integration fixtures for a handwritten/prepared binding,
   source, header-only, object, static archive, system library,
   macro-controlled layout, older dialect, and failure ownership.
3. Compile and run one small source-based library and one precompiled archive
   through the public CLI.
4. Verify a build with no foreign options retains its pre-RFC command and
   artifact behavior.
5. Update CLI help and `docs/status.md`; review `docs/reference.md` only if
   RFC 0039 has made the build-driver surface normative, and never edit it
   without explicit user approval.
6. Rebuild `hexal` and restart `hexal play` before handoff.

## Validation

This list is exhaustive.

### CLI and configuration

- Every new option parses in separated and `=` forms; repeatable options retain
  occurrence order and empty operands fail.
- With no `-c-env`, every external stage inherits the launching process
  environment. One override replaces the inherited value for header
  inspection, generated-C compilation, foreign-C compilation, and linking.
- Environment values may be empty or contain `=`. Empty names, NUL, and
  duplicate names under host equality fail with the exact value-safe
  diagnostic before an external command runs.
- Windows treats `PATH` and `Path` as one override name; POSIX treats them as
  distinct names.
- Effective child environments have deterministic order and no duplicate keys.
- Public command records and rendered failures identify overridden names but
  never contain their values.
- Environment values receive no driver-side variable, shell, home-directory,
  or path expansion.
- Relative path inputs resolve against `-root`; absolute inputs remain absolute.
- Missing, unreadable, wrong-kind, duplicate-source, duplicate-object,
  duplicate-macro, invalid-macro, invalid-dialect, and invalid-system-library
  inputs fail at their specified stage before an external command runs.
- No foreign options preserve existing `hexal build` behavior and helpfully
  display the new options without changing other command semantics.
- `BuildOptions` values never enter `compiler.Project` or `compiler.Compile`.

### Compilation

- A minimal handwritten or pre-prepared RFC 0039 adder binding compiles with
  `adder.c`; the executable writes exactly `42`, and generated C calls
  `adder_add` directly with no wrapper.
- A header-only handwritten or pre-prepared binding compiles when its header is
  reachable through `-c-include` and links with no foreign source.
- One and multiple C sources compile separately in occurrence order and link
  their resulting objects.
- Equal source basenames in different directories receive different stable
  staging object names.
- Two builds with foreign inputs receive distinct private staging identities
  even when their explicit inputs are identical; both staging trees are
  cleaned and neither is treated as a cache hit.
- Generated files use C23 while a foreign C11 source uses C11 in the same build.
- Include directories and definitions reach generated module and foreign
  compilation but never compiler-owned runtime or dependency compilation.
- An environment-controlled header configuration sees the same override during
  RFC 0193 inspection and every compile/link stage.
- A macro-controlled record definition has one consistent layout on both sides
  of the binding.
- A foreign compile failure records the exact command and prevents remaining
  compilation and linking.

### Linking

- Precompiled object and static-archive inputs link without entering the core
  compiler or staging artifact map.
- Link inputs follow the specified five groups and preserve order within each
  group.
- Repeated archives and system libraries survive; duplicate objects fail before
  invocation.
- A named Windows system library is translated by the backend without the CLI
  inventing its filename.
- A missing symbol or incompatible object reports a link-stage failure and
  leaves any existing executable unchanged.

### Boundaries

- `-c-arg` and `-link-arg` are unknown options; no raw tool argument enters a
  command plan.
- The compiler performs no filesystem access, process invocation, raw C-header
  parsing, or link planning; its C-import discovery is pure over source strings.
- The driver does not recursively discover C source, run a foreign build
  system, or create a project manifest.
- Ordinary Go tests pass without Zig installed; tagged external tests compile,
  link, and run every new input form with the pinned Zig backend.

## Settled decisions

- **Configuration:** command-line options only; a project manifest is future
  work.
- **Compiler boundary:** binding modules describe C declarations; physical
  build inputs remain entirely in the driver.
- **Headers:** `-c-include` supplies search roots; RFC 0193 owns automatic
  binding preparation.
- **C projects:** explicit source enumeration, not recursive discovery or build
  system execution.
- **Foreign dialect:** one explicit dialect per build, defaulting to C17;
  generated Hexal remains C23.
- **Definitions:** one ordered macro set applies to both ABI consumers and
  providers.
- **Environment:** inherit the launching process environment and apply one
  explicit, secret-safe override map uniformly to every external stage.
- **Caching:** this RFC adds none. Deferred RFC 0164 must define which effective
  environment values participate in a future object-cache identity before it
  may cache environment-sensitive compilation.
- **Linking:** explicit objects, static archives, and named system libraries;
  deterministic group ordering.
- **Escape hatches:** raw compiler and linker arguments are deferred; add named
  options for demonstrated requirements.
- **Dynamic loading:** excluded; deferred RFC 0191 owns runtime loading and
  symbol lookup.
- **Targets:** this RFC uses only already-qualified targets.

## Open questions

None.

## Deferred follow-up TODOs

- Revisit unrestricted `-c-env` after real package/build-system integrations
  exist. Classify toolchain-control and search-path variables, then choose an
  allowlist, denylist, or explicit clean-environment mode from measured needs.
- Add raw compiler or linker arguments only when a real library cannot be
  configured through the named options. Any later surface uses an exact
  allowlist and receives its own ABI and pipeline-ownership review.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. During implementation, review
whether the canonical reference records CLI/build behavior. If it does not,
verify that no reference change is required. If RFC 0039 adds a normative
driver handoff there, update it only with explicit user approval.
