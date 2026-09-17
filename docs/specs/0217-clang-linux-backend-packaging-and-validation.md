# RFC 0217: Clang Linux Backend, Packaging, and Validation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; replacement candidate for RFC 0214 and RFC 0215;
  implementation not authorized until the replacement is approved
- Created: 2026-09-17
- Scope: add an installed-Clang Linux build path beside the Windows compiler
  target, package the checked-in Linux runtime pack inside `bin/hexal`, preserve
  automatic zero-file C bindings, and make Clang the sole external-validation
  dependency
- Intended to supersede after approval: RFC 0214 and RFC 0215
- Amends after approval: RFC 0213's backend, target, runtime-pack delivery,
  compiler entrypoint, external validation, diagnostics, and validation where
  this RFC explicitly replaces them
- Does not change: Hexal syntax, the string-in/string-out compiler boundary,
  generated C23 as the backend representation, named foreign-input options,
  runtime ABI ownership, dependency demand, or the rule that unsupported C ABI
  shapes fail rather than being guessed

## Summary

RFC 0217 adds this native toolchain alongside the existing Windows compiler
target:

```text
host:                  x86_64 Linux with glibc 2.31 or newer
initial environment:   WSL/Linux on x86_64 Windows
C compiler:            user-installed Clang 18 or newer
Hexal target profile:  x86_64-linux-gnu
Clang target triple:   x86_64-linux-gnu
runtime pack:          checked-in lib/x86_64-linux-gnu/
compiler artifact:     bin/hexal
program artifact:      Linux ELF executable
```

WSL is the initial development environment, not a semantic target property.
The driver qualifies native `linux/amd64` with glibc and does not inspect
`/proc` or environment variables to distinguish WSL from another compatible
Linux host.

Clang is installed separately by compiler developers and Linux end users.
Hexal does not download, bundle, update, or build Clang, LLVM, a linker, libc,
an SDK, or a sysroot. GCC and Zig are not required, discovered, invoked, or
retained as optional external-validation lanes. This revision qualifies no
native Windows build path: Windows remains a core C-generation target, while a
later Windows-focused specification owns native Clang/MinGW-w64/UCRT
qualification.

The compiler build embeds the checked-in Linux libuv and mimalloc pack in the
single `bin/hexal` executable. A user build extracts only the dependencies the
program actually demands into its private staging directory. It never builds a
native dependency from source.

Direct C-header import remains the ordinary path:

```hexal
import
    Raylib from c "raylib.h"
end
```

The selected Clang preprocesses and inspects the header in memory. No binding
file is written or normally authored by the user. When the requested C surface
cannot be represented automatically, the diagnostic explicitly directs the
user to a handwritten binding when that is sufficient or to a C wrapper when
the ABI shape itself is unsupported.

## Goals

1. One concrete Clang path for Linux builds and external validation, while
   retaining Windows as a core compiler target but not a native driver target.
2. One repository-local compiler executable containing its native runtime
   inputs, licenses, workbench, and commands.
3. Checked-in, hash-verified libuv and mimalloc archives are the only native
   dependency inputs to ordinary program builds.
4. C headers normally import without generated project files or handwritten
   declarations.
5. C-import failures state the actionable fallback instead of exposing the
   prepared-binding implementation.
6. Preserve the process-free and filesystem-free core compiler API.
7. Remove GCC/Zig external-fixture discovery, sanitizer, and comparison
   machinery while keeping backend code required by an actually supported
   Windows path.

## Non-goals

- A new GCC backend or a general compiler-plugin interface.
- Native Windows backend requalification; a later RFC must qualify installed
  Clang with an explicit MinGW-w64/UCRT toolchain and sysroot.
- New macOS, AArch64, musl, or cross-compilation targets.
- Downloading or rebuilding native dependencies during compiler setup, tests,
  or a user build.
- A package manager, project manifest, dependency solver, or C build-system
  integration.
- Raw compiler or linker arguments.
- General C parsing in the core compiler.
- Automatic support for C variadics, function pointers, callbacks, raw unions,
  bit-fields, flexible arrays, or arbitrary macro evaluation.
- Release archives, installers, PATH editing, updates, or uninstallers.

## Toolchain selection

The production commands are:

```text
hexal build <filepath> -cc /usr/bin/clang \
    -target x86_64-linux-gnu [options]

hexal build -entry <key> -cc /usr/bin/clang \
    -target x86_64-linux-gnu [project options]

hexal doctor -cc /usr/bin/clang -target x86_64-linux-gnu
```

- `-cc` is required and names one executable path, not a command line. The
  production driver never searches PATH.
- Relative `-cc` paths resolve once against the invocation directory.
- The resolved path must be a regular executable file.
- The driver runs `<path> --version`, accepts a banner containing
  `clang version <major>[.<component>...]`, and requires major 18 or newer.
- The complete trimmed first nonempty version-banner line enters build
  identity. Installation paths and later banner lines do not.
- For this Linux Clang path, `-target` is required and accepts the
  compiler-owned `x86_64-linux-gnu` identity.
- Every preprocess, AST inspection, generated-C compile, foreign-C compile,
  and link command invokes the same resolved Clang executable and passes
  `--target=x86_64-linux-gnu`.
- Generated Hexal translation units use `-std=c23`. Foreign sources retain the
  existing explicit `-c-standard` selection, defaulting to C17.
- No backend interface, plugin registry, family switch, or generic
  compiler-family record is introduced. The driver recognizes exactly one
  qualified target/host pair and rejects every other pair before command
  construction.

The driver requires a native `linux/amd64` host because this first lifecycle
links and runs its results. `hexal doctor` proves glibc, linker, startup-object,
header, and executable compatibility by compiling, linking, and running probes;
it does not infer compatibility from distribution names. Native Windows hosts
and Windows-target driver requests are not qualified in this revision.

## Target profile

`x86_64-linux-gnu` is added as a second nonempty qualified
`compiler.Project.Target` identity. The current compiler consumes that identity
directly when selecting POSIX rather than Windows generation; it does not yet
consume a generic target-facts record. The table below is the ABI contract that
target-aware import normalization, generation, the runtime pack, and
qualification must agree on, not a requirement to add unused fields:

```text
OS:                 Linux
architecture:       x86_64
byte order:         little endian
pointer width:      64
Size width:         64
C data model:       LP64
threading and IO:   POSIX/Linux branches
libc:               glibc 2.31 or newer
```

`x86_64-windows-gnu-ucrt` remains a compiler target with its existing LLP64,
Windows, threading, fiber, IO, and C-ABI facts. It remains usable through the
string-in/string-out compiler API and pure-Go generated-C tests. The driver no
longer embeds, selects, validates, or links a Windows runtime pack. The zero
`Project{}` remains host-neutral and continues to emit both guarded platform
branches where it does today.

This RFC qualifies the new installed-Clang build and packaging path for Linux.
It cannot remove the Windows compiler target as an incidental Linux cleanup.
It deliberately retires the native Zig-powered Windows driver until a focused
RFC qualifies Clang/MinGW-w64/UCRT and a Windows runtime pack.

This target change intentionally changes target-qualified generated C where
the generator currently selects Windows versus POSIX declarations,
entrypoints, threading, fibers, IO, process, signal, terminal, or system-header
paths. It does not change host-neutral `Project{}` output. Consequently:

- the workbench snippet manifest, which compiles with `Project{}`, remains
  byte-identical;
- Linux-qualified integration and external fixtures receive new expected
  Linux/POSIX output; and
- no validation may claim that all target-qualified generated C remains
  unchanged.

## Automatic C-header import

### Ordinary path

`Alias from c <header>` remains sufficient whenever the imported declarations
fit Hexal's existing C ABI model. The driver:

1. discovers only reachable C imports through `compiler.DiscoverCImports`;
2. preprocesses the exact requested header with the selected Clang, target,
   ordered include roots, definitions, and effective environment;
3. asks that same Clang for its typed JSON AST over the preprocessed bytes;
4. normalizes supported declarations into ordinary `extern c` Hexal source;
5. inserts the source under the reserved in-memory prepared-binding key; and
6. invokes the unchanged `compiler.Compile` API.

No `.hex` binding file is written. Builds without C imports perform no AST
inspection. Equal target/header-form/header-payload requests are inspected
once per build.

The prepared binding is only Hexal's compile-time view of the C interface:
names, types, function signatures, record completeness, constants, and exact C
spellings. It does not copy the library implementation or replace the header.
Generated C includes the original header directly and refers to its original C
names:

```c
#include "raylib.h"

/* A Hexal call lowers directly to the declaration supplied by raylib.h. */
InitWindow(800, 450, title);
```

The distinction is necessary because the Hexal checker runs before the C
compiler. `#include "raylib.h"` tells Clang about `InitWindow`, but by itself
does not tell the Hexal checker that `Raylib.InitWindow` exists, how many
arguments it accepts, or their Hexal types. Automatic inspection supplies only
that missing typed interface information.

The automatic subset includes functions, complete and opaque records,
typedefs, enums and enumerators, external globals, callable `static inline`
functions, and object-like macros that Clang proves are value expressions of a
supported type. Unsupported unrelated declarations are omitted whole and do
not invalidate supported declarations. Required unsupported dependencies cause
the dependent declaration to be omitted rather than approximated.

Clang's preprocessing output supplies the object-like macro inventory. For
each candidate, the driver asks Clang to type-check a private expression probe
under the same header, target, includes, definitions, and environment. If
Clang proves that the macro is a value expression whose type maps to a Hexal
scalar, pointer, string literal view, or complete imported foreign record, the
importer exposes it as a typed constant. The generated C still names the macro
itself, so Clang performs the real expansion and constant evaluation.

This handles literals, parenthesized expressions, arithmetic and bitwise
constant expressions, casts, enum-based expressions, and record-valued
compound literals without teaching Hexal to parse or evaluate C preprocessor
expressions. Macros that expand to declarations, statement fragments, type
fragments, initializers that are not expressions, or otherwise unsupported
types are omitted with the ordinary binding/wrapper guidance.

For example, this requires no handwritten binding:

```c
#define MAX_TOUCH_POINTS 10
```

It is imported automatically as the equivalent of:

```hexal
constant MAX_TOUCH_POINTS as "MAX_TOUCH_POINTS": Int32
```

These also require no handwritten binding when Clang proves their types:

```c
#define DEFAULT_FLAGS (FLAG_VISIBLE | FLAG_RESIZABLE)
#define LIGHTGRAY ((Color){ 200, 200, 200, 255 })
```

Function-like macros remain different: they do not declare one fixed C
function signature, and their parameters may be evaluated multiple times or
used as tokens rather than values. They require a typed wrapper unless a later
specification defines safe macro-call semantics.

The existing 64 MiB output bounds and one 30-second inspection deadline remain.
Inspection failure removes its private staging data and runs no later build
stage.

### Linux ABI correction

Normalization is target-aware. The current automatic importer accepts a target
but maps fundamental `long` independently of it. This RFC requires the same
mapping used by handwritten foreign declarations:

| C type | `x86_64-linux-gnu` Hexal type |
| --- | --- |
| `long`, `signed long` | `Int64` |
| `unsigned long` | `UInt64` |
| `size_t`, `uintptr_t` | `Size`, `UInt64` |
| pointer | 64-bit Hexal pointer representation |

The normalizer must consume the selected target explicitly; an unused target
field is a defect. Focused automatic-import fixtures cover LP64 `long`,
`unsigned long`, `size_t`, records containing them, parameters, and results.

### When user binding code is required

Automatic import remains the default. The fallbacks are:

| C surface | User action |
| --- | --- |
| Supported declaration | Import the header directly; write no binding |
| Non-default include directory or conditional API | Add `-c-include` or `-c-define`; keep the direct import |
| Object-like macro Clang proves is a supported value expression | Import automatically; write no binding |
| Declaration omitted automatically but expressible by existing `extern c` forms | Replace the direct alias with a handwritten binding module |
| Function-like macro | Expose a normal C wrapper function and import its header |
| Variadic function, callback/function pointer, raw union, bit-field, flexible array, unsupported calling convention, or unrepresentable layout | Expose a C wrapper with supported parameters/results |
| Header incompatible with C23 | Add a compatibility wrapper header and import that header |

A handwritten binding replaces one direct C-import alias; it is not merged
silently into the automatically prepared module:

```hexal
-- bindings/raylib.hex
extern c from "raylib.h" do
    fun open_window as "InitWindow"(
        width: Int32 as "int",
        height: Int32 as "int",
        title: Ptr<Byte> | Nil as "const char *",
    )
end

export
    open_window
end
```

```hexal
import
    Raylib from "./bindings/raylib.hex"
end
```

Missing names on an automatic C module retain the actionable diagnostic:

```text
Name Error: C import <header> has no automatically imported declaration <name>; check the C name, add a handwritten binding, or expose a C wrapper
```

Header preprocessing or AST inspection that proves a compatibility header is
needed reports:

```text
C header <header> is not accepted as C23; import a compatibility wrapper header
```

The compiler must not recommend a handwritten binding for an ABI shape the
existing foreign model cannot represent. Such diagnostics name a C wrapper as
the required action. They do not suggest raw casts, guessed layouts, or
disabling ABI checking.

## Checked-in Linux runtime pack

### Source of truth

Ordinary builds consume only checked-in pack artifacts:

```text
lib/x86_64-linux-gnu/
  manifest.json
  libuv_v1.52.1/
    include/
    libuv.a
    LICENSE
    LICENSE-docs
    LICENSE-extra
  mimalloc_v3.5.1/
    include/
    mimalloc.a
    LICENSE
```

The source versions remain the repository's pinned revisions:

- libuv v1.52.1, commit
  `1cfa32ff59c076ffb6ed735bbc8c18361558661f`;
- mimalloc v3.5.1, commit
  `34fbd7e7cd4627424490afe19b20f8066bfc537d`.

The existing Windows archives are not reused. The Linux archives are rebuilt
once for `x86_64-linux-gnu` in an environment with Clang 18 or newer and glibc
2.31 headers, then checked in. Pack production is an explicit maintainer
operation, never compiler setup or a user build.

`lib/BUILD.md` records the complete Linux source list, exact compiler and
archiver versions, sysroot/glibc baseline, commands, definitions, optimization
flags, archive sizes, SHA-256 values, and a successful native combined probe.
The build uses target-portable x86-64 and never `-march=native`.

The Linux manifest retains RFC 0213's closed format-version-1 schema. It names
the Linux target and runtime ABI, lists every header, archive, and license with
its digest, and declares dependencies in `libuv`, then `mimalloc` order. Its
Linux system-library lists contain only libraries demonstrated by the combined
probe; the expected initial union is `pthread`, `dl`, and `rt`, deduplicated in
manifest dependency order. The final recorded list is the probe-backed
authority, not an inherited Windows list.

### Embedded delivery

`lib/` becomes a small Go resource package with one hand-written declaration:

```go
//go:embed x86_64-linux-gnu
var runtimePacks embed.FS
```

Release and repository builds use this immutable embedded filesystem. There is
no adjacent-pack fallback and no `-runtime-dir` option in `BuildOptions`,
`doctor`, CLI help, or diagnostics. Tests inject an internal `fs.FS` seam.

The complete Linux pack bytes and the `//go:embed` declaration land in the same
change. A repository state in which ordinary `go build ./...` fails because an
embed target is absent is never an intermediate or accepted result.

If a program has no runtime dependency, the driver does not open the embedded
manifest or materialize any pack entry. Otherwise it:

1. parses the closed manifest;
2. validates target, runtime ABI, dependency order, paths, and demanded files;
3. verifies demanded embedded bytes against their listed digests;
4. materializes only demanded headers and archives beneath the private build
   staging directory; and
5. links demanded archives and Linux system libraries in manifest order.

Unexpected, missing, absolute, escaping, duplicate, or digest-mismatched pack
entries fail before C compilation. No pack input is read from a source checkout
or user-controlled directory.

The existing link-group order remains:

1. generated Hexal objects;
2. compiled foreign-C objects;
3. explicit user objects;
4. user archives;
5. demanded checked-in runtime archives;
6. demanded runtime system libraries; and
7. user system libraries.

## Compiler packaging

The only repository-local compiler artifact is:

```text
bin/hexal
```

The canonical build command from the repository root is:

```text
go build -o bin/hexal ./cmd/hexal
```

The repository root is not a Go `main` package, so bare `go build .` is not the
correct command unless the CLI is moved to the root. The command builds the
existing `cmd/hexal` main package directly. It creates no second build program,
does not execute Hexal as part of compilation, and retains the existing version
mechanism.

The executable contains `build`, `doctor`, `version`, and `play`, the workbench
asset, the Linux runtime pack, and all required notices. No standalone
workbench or second compiler executable is produced. The command does not run
`go install` or write into a user or system binary directory.

## Build CLI

`build` accepts zero or one positional filepath, before or after named options.
Because Go's standard flag parser stops at the first positional argument, the
CLI first separates exactly one positional operand while respecting every
named option's value, then passes only named options to its flag set.

With a filepath:

- the path must resolve to one regular `.hex` file;
- its absolute, symlink-resolved parent is the source root;
- its basename is the logical entrypoint;
- `-root` and `-entry` are forbidden; and
- imports remain contained under that resolved parent.

Without a filepath, `-entry` is required. `-root` selects the source root and
defaults to the invocation directory. The entrypoint remains a slash-separated
logical key under that root.

All existing named C-source, include, define, environment, object, archive,
system-library, output, and mode options remain. No raw compiler or linker
option is added.

## Build modes

Both modes invoke direct Clang:

- debug uses `-O0 -g -ffp-contract=off -fsanitize=undefined
  -fno-sanitize-recover=all` for generated Hexal C and links the required
  Clang UBSan runtime;
- release uses `-O2 -g0 -ffp-contract=off -fno-sanitize=undefined
  -ffunction-sections -fdata-sections`, strips symbols, and garbage-collects
  unused sections; and
- foreign C sources retain the existing rule that Hexal's sanitizer policy is
  not imposed on unmodified third-party source.

`doctor` fails if the selected installed Clang cannot link and execute the
debug UBSan probe. The current ineffective combination of
`-fno-sanitize-recover` without `-fsanitize=undefined` does not remain.

## External validation

Clang 18 or newer is the sole external compiler required for compiler
development, tagged generated-C validation, driver qualification, and release
gates.

Ordinary `go test ./...` and `go vet ./...` remain process-free and require no
C compiler. Tagged tests use one test-only resolver:

```text
HEXAL_CLANG=/absolute/path/to/clang
```

Without the override, the resolver searches the supported versioned Clang
names from newest to 18, then `clang`, using PATH and the existing bounded
test-only fallback locations. It resolves and version-checks one executable
once per test binary. This discovery policy is test-only; production `build`
and `doctor` still require `-cc`.

The external suite retains all behavioral tiers:

1. compile every applicable fixture once with strict warnings;
2. run successful fixtures and compare exact output;
3. run failing fixtures and verify the required runtime diagnostic;
4. compare debug and release behavior and artifact properties; and
5. run every runnable fixture under Clang UBSan.

The complete workbench snippet catalog retains compile coverage. Linux
equivalents are added for Windows-only process fixtures rather than deleting
the Windows source cases. Linux foreign fixtures select `x86_64-linux-gnu` and
exercise LP64. In the Clang-only Linux gate, Windows-target cases are pure-Go
compiler/generated-C assertions for LLP64 and Windows branch selection; they
are not linked or executed on Linux. The one qualified compile/link/run gate
moves to `x86_64-linux-gnu`. There is no native-Windows external gate in this
revision. No fixture, runtime expectation, warning category, timeout, sanitizer
category, or catalog entry is removed merely to make the migration pass; a
Windows-only runtime expectation that has no Linux equivalent is recorded
honestly as a coverage gap.

Remove all GCC/Zig machinery: toolchain specifications, discovery, environment
variables, command prefixes, caches, per-toolchain subtests, output-divergence
state, sanitizer capability branches, Zig UBSan markers, GCC-only warning
handling, Zig mode baselines, and the Zig-powered native Windows driver. Do not
turn this cleanup into permission to emit Clang extensions: generated artifacts
remain readable standard C23.

## Qualification and doctor

A normal build performs only cheap selection and demanded-input checks:

1. resolve and validate the explicit Clang executable;
2. parse and record its supported version;
3. select the qualified target;
4. discover and compile the project; and
5. validate/materialize the embedded pack only for demanded dependencies.

It does not run a separate preliminary C program. A real compile failure owns
its ordinary stage diagnostic.

`hexal doctor` additionally proves that the selected Clang:

- accepts `--target=x86_64-linux-gnu` and `-std=c23`;
- provides every generated C23 header and facility;
- finds a compatible linker, startup objects, Linux headers, and glibc;
- compiles, links, and runs exact debug and release option probes;
- links and runs the debug UBSan probe;
- preprocesses and inspects a representative automatic C import;
- links a foreign object produced by the same selected Clang;
- verifies every embedded Linux pack entry and required notice; and
- compiles, links, and runs one program using both checked-in archives.

Doctor never rebuilds the pack.

## Diagnostics

Stable configuration diagnostics are:

```text
C backend is required; pass -cc <path>
C backend path <path> is not an executable file
C backend <path> is not Clang 18 or newer
target profile <target> is not qualified for native builds in this release
embedded runtime pack for x86_64-linux-gnu is missing or corrupt; rebuild bin/hexal
build accepts at most one source filepath
build filepath cannot be combined with -root or -entry
build filepath must name a regular .hex file
project build requires -entry when no filepath is given
```

External compile/link failures continue to record the stage, exact argument
vector, working directory, separated stdout/stderr, exit status, and only the
names of explicit environment overrides. Secret values never enter a command
record or diagnostic.

## Required implementation sweep

Implementation removes or replaces:

- the Zig backend, version pin, target verification, native-Windows driver,
  Windows runtime-pack delivery, and their external-process qualification;
- `internal/driver`'s assumption that every target uses one Zig profile,
  independent Clang lookup for Linux C imports, and `RuntimeDir` flow;
- retained vendored-source materialization and native dependency compilation
  (`materializeDependencies`, `compileNativeDependencies`, `PlanDependencies`,
  and their production `modules` dependency);
- assumptions that the qualified target set contains only Windows; Linux
  fixtures are added without deleting Windows compiler-target fixtures;
- GCC/Zig external-harness branches and testdata;
- `go install`, adjacent runtime-pack, and standalone-workbench build
  instructions; and
- active `docs/status.md` text describing Zig/GCC/Windows validation.

Code that exists solely to defend against an adjacent-pack contract is deleted.
Windows target facts and pure-Go target tests remain.
`exeSuffix`, PDB publication, junction replacement, atomic executable
replacement, and their build-tagged files are removed when they exist only for
the retired driver; shared filesystem operations gain Linux peers where the
Linux driver still needs them. Linux must not pass through a Windows artifact
rule accidentally. Checked-in Windows pack bytes are removed from active pack
delivery and are not embedded in `bin/hexal`.

## Implementation plan

### Phase 0: approve the replacement

1. Approve RFC 0217 as the replacement work order, mark RFC 0214 and RFC 0215
   discarded, and archive them unchanged.
2. Confirm the implementation checkout contains this RFC, the matching status
   update, and the pinned dependency revisions before creating pack artifacts.

### Phase 1: Linux target and direct Clang

1. Add the Linux LP64 profile beside the Windows LLP64 profile in core and
   driver registries.
2. Add one concrete installed-Clang Linux record and direct command
   construction; reject every other driver target/host pair.
3. Migrate artifact suffixes, host checks, system libraries, facility probes,
   entrypoint generation, and target-qualified tests.
4. Keep the core compiler API unchanged.

### Phase 2: zero-friction C imports

1. Reuse the selected Clang for preprocessing and typed AST inspection.
2. Make automatic normalization consume the Linux target and fix LP64 scalar
   mapping.
3. Preserve in-memory prepared bindings, limits, deterministic ordering, and
   the existing supported declaration subset.
4. Update fallback diagnostics to name handwritten binding, C wrapper, or
   compatibility header accurately.

### Phase 3: checked-in Linux pack

1. Build libuv v1.52.1 and mimalloc v3.5.1 once for the qualified Linux/glibc
   baseline from the pinned repository sources.
2. Run archive-specific and combined native probes.
3. Record exact production evidence in `lib/BUILD.md` and check in the complete
   manifest, headers, archives, and notices.
4. Add the embedded-FS resource package and private test seam.
5. Remove every ordinary-build source-compilation and adjacent-pack path.

### Phase 4: packaging and CLI

1. Make `go build -o bin/hexal ./cmd/hexal` the canonical compiler build and
   retain the existing version mechanism.
2. Implement the positional-file and project build forms.
3. Remove `-runtime-dir` everywhere.
4. Exercise `bin/hexal` directly through `version`, `doctor`, dependency-free
   build, combined-dependency build, and `play` startup.

### Phase 5: Clang-only validation

1. Collapse external toolchain resolution and compile/run/trap loops to Clang.
2. Add Linux equivalents and target-qualified fixtures without deleting the
   Windows compiler-target cases. Keep Windows cases text-only in the Linux
   Clang gate; do not attempt to link or execute them.
3. Run mode comparison and mandatory UBSan through Clang.
4. Remove GCC/Zig branches, baselines, comments, variables, status text, and
   native-Windows driver machinery.

### Phase 6: conformance and documentation

1. Run ordinary tests and vet without an external compiler.
2. Run the complete tagged suite, snippet catalog, driver qualification,
   automatic-import, foreign-input, pack, doctor, and packaging gates with
   only Clang installed.
3. Confirm the host-neutral snippet manifest is byte-identical and review every
   intentional Linux-qualified generated-C change.
4. Update `docs/reference.md` for the qualified target identity and C-import
   diagnostic wording after behavior stabilizes.
5. Update `docs/status.md`, rebuild `bin/hexal`, and restart the workbench
   through `bin/hexal play` before handoff.

## Validation (exhaustive)

- `x86_64-linux-gnu` and `x86_64-windows-gnu-ucrt` are both accepted core
  compiler targets; arbitrary strings are rejected. The Linux driver path
  accepts the Linux target. Native Windows hosts and Windows-target driver
  requests fail with `target profile x86_64-windows-gnu-ucrt is not qualified
  for native builds in this release` before any external command runs.
- Host-neutral `Project{}` compiler output and the workbench snippet manifest
  remain byte-identical; target-qualified Linux output selects only POSIX/Linux
  branches and is reviewed explicitly.
- Production `-cc` invokes the supplied executable directly, never searches
  PATH, accepts Clang 18 or newer, and rejects other or older executables with
  the exact diagnostics.
- Every external production command uses the same Clang path, Linux target,
  effective environment, and appropriate source dialect.
- `hexal build app.hex` and the equivalent `-root`/`-entry` build compile the
  same source tree and produce equivalent generated C and behavior.
- Positional filepath placement before or after named options is equivalent;
  multiple paths, invalid paths, and conflicting root/entry settings receive
  the exact diagnostics.
- A direct C import of supported functions, records, typedefs, enums,
  enumerators, globals, and static-inline functions requires no binding file
  and writes none.
- A direct import automatically exposes object-like value macros such as
  `#define MAX_TOUCH_POINTS 10`, a bitwise flag expression, and a complete
  foreign-record compound literal as typed constants with no handwritten
  binding or wrapper.
- Builds without C imports perform no header preprocessing or AST inspection.
- Automatic Linux bindings map `long` and `unsigned long` to 64-bit Hexal
  integers in aliases, parameters, results, and record members.
- Missing or omitted automatic declarations direct the user to check the C
  name, add a handwritten binding, or expose a wrapper; unrepresentable ABI
  shapes never recommend a binding as sufficient.
- Include roots, definitions, target, dialect, and environment remain
  consistent between inspection and compilation.
- The C-import byte and time budgets, deterministic normalization, reserved
  key, and no-written-binding behavior remain covered.
- The Linux pack contains libuv v1.52.1 and mimalloc v3.5.1 built from the
  pinned revisions, complete matching headers, all notices, a strict manifest,
  and recorded reproducibility evidence.
- A dependency-free build neither reads the embedded manifest nor materializes
  pack files.
- Mimalloc-only, libuv-only, and combined programs materialize and link exactly
  their demanded checked-in artifacts and Linux libraries.
- Linux user builds never compile native dependencies from source and never
  select the Windows pack. The driver has no active Windows pack selection or
  materialization path.
- Embedded-pack validation rejects missing, unexpected, duplicate, absolute,
  escaping, ABI-mismatched, target-mismatched, or digest-mismatched content.
- `-runtime-dir` is absent from public options, CLI help, parsing, doctor, and
  diagnostics; pack tests use only the private filesystem seam.
- `go build -o bin/hexal ./cmd/hexal` creates only `bin/hexal` as a
  repository-local build artifact and invokes neither `go install`, a build
  helper executable, nor a system-directory write.
- `bin/hexal` contains the Linux pack and notices and directly runs `version`,
  `doctor`, `build`, and `play`.
- Debug generated C is actually instrumented with Clang UBSan; release output
  is not; both modes preserve fixture behavior.
- Ordinary Go tests and vet require no external compiler.
- Tagged validation requires only Clang 18 or newer and contains no GCC/Zig
  discovery, invocation, conditional behavior, or optional lane in the Linux
  gate. Windows compiler-target assertions in that gate are pure Go and never
  attempt to link or execute a Windows binary on Linux.
- Every prior applicable fixture and snippet retains compile coverage; every
  runnable success/trap expectation retains runtime coverage; Windows-only
  process cases have Linux replacements.
- Clang UBSan runs every runnable fixture and fails the release gate if the
  selected Clang cannot link or execute its runtime.
- No generated-C rule is changed merely to satisfy Clang; emitted source
  remains readable standard C23.
- The core compiler remains string-in/string-out and performs no filesystem,
  process, header-discovery, runtime-pack, or link operation.

## Language-goal assessment

This RFC adds no language syntax or ownership concept. It reduces the active
external-validation surface from three compilers to one Clang installation,
removes Linux native dependency rebuilding, and makes direct C headers the
default interop path. The remaining explicit `-cc` and `-target` settings are
build reproducibility inputs rather than language concepts.

The accepted cost is a larger `bin/hexal` because it contains the checked-in
native pack. That cost buys one movable executable, no adjacent runtime tree,
no package-manager dependency, and no native source build during ordinary use.
Dependency-free programs pay no extraction, linking, or runtime cost.

## Deferred Windows cleanup

This RFC deliberately solves WSL/Linux first. The Windows compiler target and
its pure-Go target-specific tests remain. The Zig-powered native build path,
Windows runtime-pack delivery, and Windows external-process gate do not.

A later Windows-specific specification must review whether to retain Zig or
qualify Clang with an explicit MinGW-w64/UCRT sysroot, then clean obsolete
backend, packaging, and fixture machinery without weakening Windows target
support.

## Open questions

None. The native-driver disposition is settled: Linux/amd64 plus installed
Clang is the sole qualified build host and backend for this revision. Windows
remains a core generated-C target only.

After the merged RFC is approved, RFC 0214 and RFC 0215 may be marked
`Discarded; consolidated into RFC 0217 before implementation` and archived in
the same change. Until then, they remain active and unchanged.
