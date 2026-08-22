# ADR 0055: Filesystem and Build Driver

- Kind: Architecture Decision Record (ADR)
- Status: Draft; design proposed, implementation not started
- Created: 2026-08-14
- Updated: 2026-08-22
- Scope: filesystem, project discovery, artifact materialization, external C
  builds, and linking outside the core compiler
- Depends on: RFC 0034 (modules and imports), RFC 0039 (C interop), RFC 0052
  (target profiles), RFC 0117 (compile-time evaluation), and RFC 0118
  (concurrency safety)
- Coordinates with: the workbench, generated-C artifact manifests, and future
  package/dependency specifications

## Purpose

The core compiler is permanently in-memory and string-in/string-out. A future
driver must connect that compiler to files, projects, C toolchains, and final
build artifacts. This ADR owns that external layer so filesystem and build
responsibilities do not leak into compiler language specifications.

## Intended responsibilities

- Discover and read Hexal source files.
- Select the logical module root and entrypoint.
- Normalize host paths into logical compiler keys.
- Read C source files, headers, and generated binding manifests.
- Invoke external C-project build systems when explicitly configured.
- Resolve include roots, preprocessor definitions, target options, object
  files, static libraries, shared libraries, and system libraries.
- Supply complete in-memory source and foreign-binding strings to the core
  compiler.
- Materialize every generated C/header string returned by the compiler.
- Compile generated and supplied C sources.
- Link generated objects with configured foreign objects and libraries.
- Report filesystem, toolchain, build-system, and linker failures separately
  from compiler diagnostics.
- Compile generated C in C23 mode (RFC 0069): the pinned GCC/Clang toolchain
  plus compatible C library must provide the generated program's selected
  standard headers (`<stdckdint.h>`, `<stdatomic.h>`, `<threads.h>`,
  `<string.h>`, and the RFC 0062 umbrella set). A missing or unusable standard
  header is reported as a toolchain/target failure, never as a Hexal source
  diagnostic, and never repaired by a compiler-side fallback definition.
- Later own dependency tracking, caching, file watching, and incremental
  compilation.
- Compile every `.c` entry returned in `CompilationResult.Files`, not only
  those under `modules/`: the demand-driven component artifacts under
  `hexal/` (for example `hexal/runtime.c`, `hexal/heap.c`,
  `hexal/string.c`, `hexal/concurrency.c`) own external runtime definitions
  and must be translation units (ADR 0071).
- Report a generated artifact that fails to compile or link as a
  compiler/toolchain failure, never as a Hexal source diagnostic.

## Build pipeline

The driver runs these stages in order and records the inputs and outputs of
each stage:

1. Load an explicit project configuration or a caller-supplied project value;
   discover source roots only in the driver.
2. Resolve logical Hexal modules, the entrypoint, imports, C sources, headers,
   binding manifests, object files, libraries, and target profile.
3. Compute the module dependency graph and a content-addressed build identity.
4. Supply the complete logical source map to the in-memory compiler and receive
   generated C/header artifacts and diagnostics.
5. Materialize generated artifacts in an isolated output tree, preserving the
   compiler's logical names and source-map metadata.
6. Compile every generated and configured C translation unit as C23 with the
   selected target/profile/compiler options.
7. Link objects and libraries with the selected linker and platform settings.
8. Optionally execute declared runtime validation programs in a controlled
   validation environment; normal compiler tests do not execute external
   tools.

An error is attributed to the earliest failing stage: configuration,
filesystem, dependency resolution, Hexal compilation, C compilation, linking,
or runtime validation. The driver must preserve compiler diagnostics instead of
rewriting them as generic build failures.

## Project identity and incremental recompilation

The cache key for a module artifact includes the complete source contents,
logical module name, entrypoint when relevant, compiler version, `Project`
settings, selected RFC 0052 profile, C compiler and linker identity/options,
preprocessor definitions, include roots, and the content digests of all
foreign headers, binding manifests, objects, and libraries that affect the
artifact. Host absolute paths are not semantic inputs.

- A private implementation change invalidates that module's generated C and
  downstream artifacts only when the module's public interface, layout,
  exported symbol set, or generated dependency set changes.
- A public signature, type layout, exported storage, generic reachability,
  target profile, foreign header, compiler option, or runtime component change
  invalidates every dependent artifact that consumes it.
- A changed runtime component recompiles every generated translation unit that
  links that component.
- Cache hits are valid only when the artifact content and all identity inputs
  match. The driver never guesses from timestamps alone.
- A failed or interrupted build cannot publish a partial cache entry as a
  complete artifact.

## Toolchain and target selection

The driver selects a named RFC 0052 target profile and a C23-capable compiler
toolchain. Native defaults may be convenient, but cross-compilation requires
an explicit profile and toolchain. The driver must verify compiler version,
required C23 headers, target architecture, ABI options, atomics, threading,
and linker support before accepting the build.

The core compiler remains incapable of host probing. The driver may probe or
invoke tools, then passes the selected evidence as build-time settings. A
profile mismatch is a build failure, not a source-level guess or fallback.

## Validation modes

Build infrastructure must expose distinct validation modes:

- **Compiler tests:** pure Go, in-memory, and generated-C text assertions.
- **C compile validation:** materialize and compile generated artifacts and
  foreign fixtures with the selected C23 toolchain.
- **Link validation:** link representative executables and libraries with the
  configured foreign objects and platform libraries.
- **Runtime validation:** execute declared programs and assert exit status,
  output, traps, ABI calls, concurrency behavior, and resource cleanup.
- **Cross-profile validation:** repeat compile/link checks for each supported
  target profile; runtime checks run only where an executable target exists.

The snippet manifest remains a text-level regression net for ordinary Go
tests. The driver-level generated-C and runtime suites are the authority for
claims that emitted C compiles, links, or behaves correctly. A green
`go test ./...` alone must not be reported as proof of those properties.

## Boundary

- The driver may access the host filesystem and execute configured external
  tools.
- The core compiler may do neither.
- The driver supplies logical names and complete contents; the compiler never
  discovers or opens a path.
- The compiler returns generated logical filenames and complete contents; it
  never writes an artifact.
- Binary objects and libraries remain driver inputs. They are never parsed or
  represented as strings by the core compiler.
- Language checking and C ABI compatibility remain compiler responsibilities;
  file discovery, tool invocation, and linking remain driver responsibilities.

## Deferred design

- Driver package and public API.
- Project/configuration file format.
- Default source roots, output directories, and entrypoint selection.
- C compiler and linker selection.
- CMake, Meson, Make, pkg-config, and custom-command integration.
- Header preprocessing and binding-manifest generation.
- Object, static-library, shared-library, and platform-library configuration.
- Package/dependency manifest and registry policy.
- Case-insensitive filesystem collision policy.
- Symlink, sandbox, reproducibility, and supply-chain policy.
- Cache format, eviction, remote cache policy, and invalidation storage.
- Watch mode, diagnostics presentation, and IDE integration.

## Non-goals

- Changing Hexal syntax or semantics in this placeholder ADR.
- Giving the core compiler filesystem or process-execution capabilities.
- Choosing a C frontend, build system, package manager, or cache format now.
- Treating arbitrary third-party build scripts as trusted by default.

## Validation

This section is exhaustive. ADR 0055 is complete only when every item below
passes:

- The core compiler remains filesystem- and process-free while the driver can
  discover sources, materialize outputs, invoke C23 compilation, and link.
- Every generated `.c` artifact returned by the compiler is compiled exactly
  once per build identity, including demand-driven runtime components.
- Generated headers precede every dependent translation-unit compilation and
  logical source mappings survive materialization.
- Configuration, filesystem, dependency, compiler, C compiler, linker, and
  runtime failures remain distinguishable and preserve source diagnostics.
- Cache keys include all semantic inputs listed above; private changes avoid
  unnecessary downstream recompilation and public/profile/foreign changes
  invalidate every affected artifact.
- Interrupted and failed builds cannot publish complete cache entries.
- C compile, link, runtime, and cross-profile validation are separate from
  pure-Go compiler tests and each reports its own evidence.
- A missing C23 header, unsupported profile feature, or ABI mismatch is a
  toolchain/target failure and never a silent compiler fallback.
- The driver has no implicit permission to execute arbitrary project scripts;
  external commands are configured explicitly and are attributable in the
  build record.

## Readiness

Ready for a design review. Implementation remains blocked until RFC 0039 and
RFC 0052 settle the compiler inputs, generated artifact contract, foreign ABI,
and target evidence, and until the driver API, configuration format, cache
identity, and toolchain policy are chosen.

## Reference synchronization

This ADR does not add language syntax. After the driver and its project/build
inputs stabilize, update `docs/reference.md` only for any compiler-visible
`Project`, target-profile, generated-C, or external-linkage contract introduced
by the implementation. Keep filesystem discovery, cache format, tool commands,
and runtime validation behavior in this ADR and the driver documentation.
