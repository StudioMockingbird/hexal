# RFC 0213: External C Backend and Runtime Packs

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-16
- Scope: select an installed C compiler and link target-qualified static
  libuv/mimalloc runtime packs without shipping a C compiler in Hexal
- Depends on: closed RFC 0052 (C backend), closed ADR 0055 (build driver),
  closed ADR 0145 (libuv), closed ADR 0146 (mimalloc), and the current target
  profile contracts
- Coordinates with: RFC 0039 (C interop compiler core), RFC 0192
  (command-line C build inputs), RFC 0193 (automatic C bindings), RFC 0187
  (build modes), and deferred RFC 0164 (object cache)
- Does not add: a project manifest, a package registry, dependency download,
  project discovery, incremental compilation, or new Hexal syntax

## Decision summary

1. The installed Hexal distribution does not contain GCC, Clang, Zig, a
   linker, or a libc.
2. `hexal build` selects an installed C compiler with `--cc PATH`.
3. Additional compiler and linker arguments are repeatable command-line
   values; they are never parsed from one shell command string.
4. Hexal runtime dependencies are distributed as target-qualified static
   packs under `libs/`.
5. The normal build links the selected pack; it does not compile libuv or
   mimalloc source.
6. The core compiler remains string-in/string-out and never sees paths,
   archives, compiler arguments, or environment variables.
7. A target is supported only after the selected compiler and its runtime pack
   pass the target qualification probes.

This is Nim-like at the compiler boundary (generated C plus an installed
native compiler), but it intentionally does not adopt Nimble-style package
resolution. Runtime packs are Hexal-distributed implementation assets, not
user packages.

## Responsibilities

### Core compiler

The core API remains:

```go
func Compile(sources map[string]string, entrypoint string, project Project) CompilationResult
```

The core compiler:

- validates Hexal and emits C/header contents as strings;
- records logical runtime dependencies such as `libuv` and `mimalloc`;
- consumes only explicit `Project` values;
- performs no process execution or filesystem access; and
- does not inspect the selected compiler or runtime pack.

### Build driver

The driver owns:

- command-line parsing;
- compiler discovery and invocation;
- target and ABI selection;
- runtime-pack discovery and verification;
- foreign C/object/archive inputs;
- environment overrides;
- temporary staging;
- linking; and
- diagnostics for missing or incompatible external build inputs.

### Pack producer

Maintainer or release tooling owns:

- building libuv and mimalloc from pinned source revisions;
- selecting compiler flags and source dialects for those projects;
- producing one pack for each qualified target profile;
- writing the pack manifest and hashes; and
- publishing the pack alongside the Hexal distribution.

Pack production is not part of an ordinary user build.

## Compiler selection

### Command-line contract

```text
hexal build --cc <compiler-path> [options] <entrypoint>
```

The driver stores the compiler executable and arguments separately:

```text
--cc PATH                 executable path
--cc-arg ARG              repeatable argument passed to compile and link
--link-arg ARG            repeatable argument passed only to the link step
--target TARGET           explicit Hexal target profile
--runtime-dir PATH        optional runtime-pack root
--c-env NAME=VALUE        repeatable environment override
```

Rules:

- `--cc` is required for a binary build unless a future driver policy defines
  a configured default; this RFC defines no implicit compiler search.
- `PATH` identifies one executable. It is not a shell command line.
- Empty paths, malformed arguments, and duplicate conflicting options are
  driver errors before compilation starts.
- Arguments are passed as exact process arguments with no shell expansion.
- `--cc-arg` applies to every C compile and link invocation.
- `--link-arg` applies only to the final link invocation.
- The driver does not infer target, ABI, include paths, or libraries from the
  compiler filename.
- A compiler that needs a subcommand receives it explicitly:

  ```text
  hexal build --cc C:\\zig\\zig.exe --cc-arg cc ...
  ```

- A missing executable, failed version probe, failed C23 probe, or failed
  invocation is a build diagnostic; it is never converted into a compiler
  success result.

### Supported compiler families

The initial driver accepts executables implementing the C compiler-driver
contract from:

- Clang;
- GCC; and
- Zig's `cc` frontend when `cc` is supplied as an argument.

The driver does not require one vendor-specific command spelling beyond the
explicit argument list. Compiler-family-specific behavior belongs in the
target qualification record, not scattered through code-generation paths.

### Qualification

Before a build that produces a binary, the driver must establish:

1. the compiler executable can be started;
2. the compiler accepts the selected C23 dialect;
3. the compiler can produce an object for the selected target;
4. the compiler can link a minimal program for that target; and
5. the selected runtime pack has the same target and ABI identity.

`hexal doctor` may run the same probes without compiling a Hexal program.
Ordinary core compiler tests do not invoke an external compiler.

The driver records compiler identity in the build result or diagnostic record:

- executable path after canonicalization;
- compiler family and version output;
- effective target triple when the compiler reports one;
- exact compile/link arguments; and
- runtime-pack identity.

The path is diagnostic metadata only. Generated C and `CompilationResult.Files`
must not contain host paths.

## Target profiles

The caller selects a compiler-owned target profile, not individual ABI facts.
The profile supplies:

- canonical target identity;
- OS and architecture;
- object format;
- byte order and pointer width;
- `Size` representation;
- C runtime/SDK policy;
- compiler target spelling; and
- runtime-pack key.

The driver rejects an unqualified target before C compilation. A compiler's
own default target is never silently substituted for an omitted or invalid
Hexal target.

Adding a target requires all of the following in one qualification change:

- a target profile;
- compiler and linker probes;
- libuv and mimalloc packs;
- system-library requirements;
- generated-C and C-interop fixtures; and
- target-specific documentation and tests.

No pack makes a target supported by itself.

## Runtime-pack layout

An installed Hexal distribution contains a runtime root with this logical
layout:

```text
libs/
  <target-profile>/
    manifest.json
    libuv/
      include/uv.h
      libuv.<archive-extension>
      LICENSE
    mimalloc/
      include/mimalloc.h
      mimalloc.<archive-extension>
      LICENSE
```

`<target-profile>` is the exact compiler-owned profile identity, not an OS
nickname. The archive extension is target-specific (`.a`, `.lib`, or another
qualified form); the logical dependency name remains `libuv` or `mimalloc`.

The default runtime root is `libs/` beside the running `hexal` executable.
`--runtime-dir` overrides it explicitly. The driver must not search the
current working directory, a source checkout, parent directories, or random
system locations for Hexal runtime packs.

### Pack manifest

The manifest is distribution metadata, not a project manifest. It contains:

- format version;
- target profile identity;
- archive and header paths;
- SHA-256 for every shipped file;
- libuv and mimalloc source versions and commit identities;
- compiler family/version used to produce the pack;
- source dialect and compile definitions;
- debug/release classification;
- required system libraries and their link order; and
- license identifiers and license-file paths.

The manifest is verified before any generated or foreign object is linked.
Missing files, extra required files, hash mismatches, malformed paths, target
mismatch, and unsupported format versions are driver errors.

### Compiler compatibility

The pack's target and ABI identity must match the selected profile. The driver
must also run the profile's compiler-compatibility probe before release
qualification. If a target cannot consume one pack with all supported Clang,
GCC, and Zig CC configurations, the pack producer adds a compiler-family
dimension to the pack key rather than weakening the check.

This rule avoids assuming that every static archive is portable merely because
its exported functions use C linkage.

### Link requirements

When generated code requests a runtime dependency:

1. select the one pack matching the target profile;
2. verify its manifest and headers;
3. expose the dependency include roots to C compilation;
4. add the archive and manifest-declared system libraries in order; and
5. invoke the selected compiler driver for the final link.

The driver must not invoke `ar`, `ld`, or a platform linker as a separate
required tool when the selected compiler driver can perform the operation.
Pack archives are already built; archive creation is pack-production work.

The system libc, SDK, startup objects, and platform libraries remain supplied
by the selected compiler/toolchain. Hexal does not claim that libuv or mimalloc
archives provide a libc.

## libuv and mimalloc policy

- Pack production starts from the pinned source revisions already recorded by
  the libuv and mimalloc decisions.
- Upstream source is compiled with the dialect and definitions required by that
  dependency; Hexal's C23 dialect is not imposed on third-party sources.
- The normal user build consumes archives and headers only.
- A release pack is statically linked into an executable that requests the
  dependency; no runtime DLL or shared object is required for these two
  dependencies.
- Release packs may be used by both debug and release Hexal builds in v1.
  Separate debug packs are deferred until measurements require them.
- `uv_replace_allocator` is applied only when the libuv integration contract
  requires it and before the first other libuv call.
- Foreign libraries are not silently routed through Hexal's mimalloc pack;
  ownership remains governed by the foreign library's ABI contract.

## Removal and migration from the current backend

The implementation must sweep the old assumptions from the normal driver path:

- remove the requirement for Zig 0.16.0 as the only backend;
- remove normal-build materialization and compilation of embedded libuv and
  mimalloc source;
- remove driver logic that searches for repository submodules or embedded
  dependency trees during a user build;
- retain pinned upstream sources only in the maintainer pack-production path;
- replace Zig-specific backend identity with the generic compiler identity;
- keep the core compiler's runtime-dependency names unchanged; and
- preserve the string-in/string-out compiler boundary.

The old embedded-source route may remain in a maintainer-only pack builder,
but it must not be an implicit fallback in `hexal build`. A missing pack is an
explicit error, not a request to compile vendored sources.

## Diagnostics

Required driver diagnostics include:

- `--cc` missing or invalid;
- compiler process unavailable;
- compiler does not accept the required C23 probe;
- target profile missing or unqualified;
- runtime root missing;
- runtime pack missing or malformed;
- manifest hash mismatch;
- target, ABI, or compiler compatibility mismatch;
- required header or archive missing;
- unsupported archive format;
- required system library unavailable; and
- compiler or linker failure with preserved command and output.

Diagnostics must identify the failing input and the next action. For example:

```text
runtime pack for x86_64-windows-gnu is missing libuv.lib;
install the Hexal runtime pack for x86_64-windows-gnu or pass --runtime-dir
to a compatible pack
```

## Non-goals and deferred work

- Bundling or downloading a C compiler.
- Bundling or replacing libc.
- A package registry or dependency solver.
- Project manifests. A future manifest may persist these command-line values.
- Automatic discovery of C build systems.
- Runtime source compilation during ordinary builds.
- Universal archives shared across incompatible targets or ABIs.
- Automatic cross-compilation without an explicit target profile and matching
  compiler/toolchain.
- Incremental object caching; RFC 0164 may cache pack-independent objects
  later.

## Implementation plan

### Phase 1: model the external backend

1. Replace the Zig-only backend configuration with an executable-plus-argument
   command model.
2. Add target-profile validation at the driver boundary.
3. Add compiler version and C23 qualification probes using fake executables in
   pure-Go tests; do not invoke a real compiler in ordinary tests.
4. Preserve existing Zig behavior through an explicit `--cc zig --cc-arg cc`
   invocation in driver tests.

### Phase 2: define and verify runtime packs

1. Define the manifest schema and target-profile key.
2. Add maintainer tooling that builds pinned libuv and mimalloc sources and
   writes archives, headers, licenses, and hashes.
3. Add pack verification with path-containment and hash checks.
4. Add compiler-compatibility probes for every supported compiler family.
5. Produce the first qualified pack for the currently supported target.

### Phase 3: switch normal builds to packs

1. Replace embedded-source materialization in the normal dependency path with
   pack selection and verification.
2. Add include roots, archives, and manifest-declared system libraries to the
   existing driver pipeline.
3. Remove the implicit Zig-only and source-build fallbacks.
4. Keep runtime dependency names and generated-C contracts stable.
5. Preserve command, environment, and failure diagnostics.

### Phase 4: qualification and cleanup

1. Run every existing driver and generated-C text test.
2. Add pack fixtures for no dependency, mimalloc only, libuv only, and both.
3. Verify missing, corrupt, mismatched, and incompatible packs fail closed.
4. Run external C23 and target qualification for each supported profile.
5. Remove obsolete Zig-only helpers, embedded normal-build paths, and stale
   comments.
6. Update `docs/reference.md` only if a user-visible build or C-output
   contract changes; otherwise record that no language rule changed.
7. Update `docs/status.md` and close this RFC only after all validation passes.

## Validation (exhaustive)

- Core compiler tests remain process-free and filesystem-free.
- `--cc` and repeatable argument parsing preserve argument boundaries exactly.
- Missing, invalid, and failed compiler probes produce diagnostics.
- C23 qualification rejects a compiler that accepts only an older dialect.
- An explicit target is required for a binary build and unqualified targets
  are rejected.
- A valid pack is selected only by exact target-profile identity.
- Missing, malformed, hash-mismatched, path-escaping, and version-unsupported
  manifests are rejected before linking.
- Missing headers, archives, and required system libraries are diagnosed.
- The four dependency-demand cases (none, mimalloc, libuv, both) select the
  correct pack inputs and link order.
- Compiler-family compatibility probes pass for every qualified compiler and
  reject an incompatible compiler/pack combination.
- A Zig CC executable works when `cc` is supplied as an explicit argument.
- A foreign C source, object, archive, and system library continue to flow
  through RFC 0192 without entering the core compiler.
- The normal build never compiles embedded libuv or mimalloc source.
- Equivalent source, project, compiler arguments, target, and pack contents
  produce identical generated C and deterministic driver inputs.
- Compiler paths and host filesystem paths do not appear in generated C or
  `CompilationResult.Files`.
- Existing generated-C and runtime behavior remains unchanged for a matching
  pack.
- `hexal doctor` reports compiler, target, pack, and system-library failures
  with actionable diagnostics.

