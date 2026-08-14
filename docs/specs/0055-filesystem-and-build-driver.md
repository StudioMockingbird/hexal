# ADR 0055: Filesystem and Build Driver

- Kind: Architecture Decision Record (ADR)
- Status: Draft; design not started
- Created: 2026-08-14
- Scope: filesystem, project discovery, artifact materialization, external C
  builds, and linking outside the core compiler
- Depends on: RFC 0034 (modules and imports), RFC 0039 (C interop), and RFC
  0052 (target profiles)
- Coordinates with: future package and incremental-build specifications

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
- Later own dependency tracking, caching, file watching, and incremental
  compilation.

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
- Cross-compilation and target-profile selection.
- Case-insensitive filesystem collision policy.
- Symlink, sandbox, reproducibility, and supply-chain policy.
- Incremental dependency graph, cache storage, and invalidation.
- Watch mode, diagnostics presentation, and IDE integration.

## Non-goals

- Changing Hexal syntax or semantics in this placeholder ADR.
- Giving the core compiler filesystem or process-execution capabilities.
- Choosing a C frontend, build system, package manager, or cache format now.
- Treating arbitrary third-party build scripts as trusted by default.

## Readiness

Not ready for design or implementation. Detail this ADR after RFC 0034 and RFC
0039 establish the complete in-memory compiler inputs, outputs, and foreign ABI
contracts.

