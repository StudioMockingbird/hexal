# RFC 0052: C Compiler Backend and Target Profiles

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; backend direction settled, detailed profile schema and first
  target-pack builds not started
- Features: bundled C23 backend, host toolchain packages, target packs, trusted
  ABI metadata, cross-compilation, and external-toolchain overrides
- Created: 2026-08-13
- Updated: 2026-08-27
- Depends on: the reference's scalar, `Size`, layout-query, Task-target, and C23
  output contracts
- Coordinates with: RFC 0039 (C interoperability compiler core) and RFC 0055
  (filesystem and build driver)

## Summary

Hexal uses a pinned, trimmed Clang/LLVM backend. The default backend is a
host-specific executable distribution, not C++ code statically linked into the
Go compiler. It contains Clang, LLD, required LLVM object tools, Clang resource
headers, and compiler-rt support for the enabled targets.

One portable libc is not a valid target abstraction. Each supported target has
a separate, immutable target pack containing its sysroot, C library or platform
CRT contract, startup objects, system/import libraries, compiler runtime, and
trusted ABI metadata. The core compiler consumes only the metadata through
`Project`; RFC 0055's driver owns tools, files, processes, installed packs, and
linking.

The bundled backend is the supported default. An external Clang or GCC remains
an explicit override and must satisfy the same target profile. Generated Hexal
C always compiles as C23; imported C translation units compile under their own
declared dialect and link only when their target ABI and runtime contract match.

## Goals

- Give Hexal one reproducible, C23-capable default backend.
- Preserve readable generated C as a first-class build artifact.
- Keep the in-memory compiler string-in/string-out and process-free.
- Support native and cross builds without using host layout as target evidence.
- Make C interoperability depend on an exact ABI profile, not an OS label.
- Allow older C projects to coexist with Hexal-generated C23.
- Limit LLVM to the X86, AArch64, ARM, and RISC-V code-generation families.
- Keep backend and target packs independently replaceable and versioned.
- Permit an external GCC or Clang without weakening the supported contract.

## Non-goals

- Statically linking Clang's C++ libraries into the Go compiler in the first
  implementation.
- A single executable object that runs unchanged on every host platform.
- One libc for Windows, macOS, Linux, embedded systems, and other POSIX systems.
- Every LLVM target or every OS/architecture pair accepted by C compilers.
- Treating `POSIX` as an ABI or target triple.
- Making every imported C source file compile as C23.
- Toolchain discovery, filesystem access, process execution, caching, or project
  loading in the core compiler.
- Embedded, WASI, Cosmopolitan APE, BSD, Android, iOS, or other target profiles
  in the first implementation.
- Source-visible target reflection beyond the existing layout operations.

## 1. Three separate identities

The implementation must not collapse these concepts.

### 1.1 Host backend

A host backend is executable tooling built for the machine running Hexal:

- Clang driver and frontend;
- LLVM optimizer and selected code generators;
- integrated assembler support;
- LLD for supported link formats;
- `llvm-ar`, `llvm-ranlib`, and any other object tool actually required by the
  driver;
- Clang resource headers; and
- the compiler-rt pieces required to construct target packs.

A host backend is specific to both host OS and host architecture. A Windows
x86-64 Clang binary or static archive cannot run on Linux, macOS, or AArch64.
Every distributed Hexal host therefore has its own backend build.

One host backend may generate code for every enabled output architecture. Host
identity and target identity are independent.

### 1.2 Target pack

A target pack contains the material needed to turn target objects into a
program or library:

- target triple and ABI variant;
- sysroot and standard headers;
- C library or platform CRT contract;
- startup and termination objects;
- system libraries or import libraries;
- target compiler-rt builtins;
- linker defaults and minimum OS version;
- supported static/dynamic linkage modes; and
- an immutable manifest naming every pinned component and its digest.

Target packs are installed and updated separately from the Hexal compiler. A
normal distribution may include one native pack; additional packs are acquired
on demand. The first implementation need not define the user-facing installer.

### 1.3 Target profile

A target profile is the trusted, path-free metadata contract consumed by the
core compiler and driver. It identifies the target pack but contains no host
filesystem path and no executable handle.

The profile is selected through `Project`. Its zero value selects a documented
native default supplied by the caller or driver. Cross-compilation requires an
explicit named profile.

Profiles are immutable and versioned. Objects built under distinct profile
identities are not linked unless the driver proves the identities ABI-compatible
under an explicit rule. Name similarity is never sufficient.

## 2. Backend choice

### 2.1 Default: trimmed Clang and LLVM

Official Hexal distributions use Clang and LLVM tools as the bundled backend.
The backend build enables only:

```text
LLVM projects: clang;lld
LLVM runtimes: compiler-rt where required
LLVM targets:  X86;AArch64;ARM;RISCV
```

The distribution excludes LLDB, MLIR, Flang, Polly, Clang extra tools, examples,
benchmarks, documentation, tests, and every unused code-generation backend.
Release builds disable assertions, strip release symbols, and may use ThinLTO
and size-oriented compilation after measurements confirm the result.

Build-time exclusion, link-time optimization, dead-section removal, and symbol
stripping are permitted. Hexal does not maintain a source fork that deletes
internal Clang language or driver subsystems merely to reduce size.

Enabling LLVM's `X86` backend does not promise public 32-bit x86 support. Public
target support is defined only by the matrix in this RFC.

### 2.2 Invocation boundary

The driver invokes the bundled Clang executable as a child process. It does not
link Clang's internal C++ libraries into the Go compiler.

This boundary provides:

- no cgo requirement in the core compiler;
- independent backend updates;
- process isolation for Clang failures;
- reproducible command lines for diagnosing generated C;
- ordinary stdout, stderr, and exit-status capture; and
- simpler host cross-builds.

The driver owns an internal backend interface so a later implementation may use
an in-process backend without changing language semantics or target profiles.
Static embedding is reconsidered only after profiling proves child-process
startup is a material build bottleneck. A desire for one downloaded file is not
sufficient: if required, a host-specific Clang payload may instead be embedded
as data and extracted into a verified cache while preserving process isolation.

### 2.3 Linker selection

- ELF and PE/COFF target packs use the matching LLD driver by default.
- macOS initially uses Clang with the installed Apple SDK and Apple-supported
  system linker unless the exact LLD Mach-O configuration is separately
  qualified by its target pack.
- The Clang driver constructs compile and link jobs; Hexal does not duplicate
  its target option or startup-object logic.
- LLD remains independently invocable by the driver when archive or link-only
  operations require it.

### 2.4 External override

The driver may select an externally installed Clang or GCC only through an
explicit override. It must qualify:

- compiler family and version;
- accepted C23 language facilities;
- required resource and standard headers;
- target triple and ABI flags;
- assembler and object format;
- compiler runtime;
- linker behavior; and
- selected target-pack compatibility.

Failure is a toolchain/profile diagnostic. The driver never falls back silently
from the bundled backend to a system compiler.

## 3. Supported architecture and OS matrix

`AArch64` means 64-bit Arm. `ARM` means 32-bit Arm. `RISC-V` initially means
RISC-V 64 for hosted systems; RISC-V 32 belongs to a later embedded profile.

### 3.1 Initial host releases

| Host OS | x86-64 | AArch64 | ARM32 | RISC-V 64 |
|---|---:|---:|---:|---:|
| Windows | required | required after x86-64 | no | no |
| macOS | required where Apple supports it | required | no | no |
| Linux | required | required after x86-64 | later | later |

The first release may ship only the required cells. A later host release does
not imply that the same OS/architecture pair is a supported output target.

### 3.2 Hosted output profiles

| Target family | Initial disposition |
|---|---|
| `x86_64-linux-gnu` | required for native Linux C-library compatibility |
| `x86_64-linux-musl` | required for self-contained/static Linux programs |
| `aarch64-linux-gnu` | next Linux profile |
| `aarch64-linux-musl` | next Linux profile |
| `armv7-linux-musleabihf` | later hosted ARM32 profile |
| `riscv64-linux-musl` | later hosted RISC-V profile |
| `x86_64-w64-windows-gnu` | required Windows UCRT profile |
| `aarch64-w64-windows-gnu` | next Windows UCRT profile |
| `x86_64-apple-darwin` | required while Apple supports the host/target |
| `aarch64-apple-darwin` | required macOS profile |

There is no initial Windows ARM32 or Windows RISC-V profile. There is no macOS
ARM32 or macOS RISC-V profile. Unsupported matrix cells are rejected before
compilation rather than passed speculatively to Clang.

Other POSIX systems require distinct target profiles because POSIX does not
define object format, syscall ABI, dynamic loader, startup objects, SDK, or C
library ABI.

## 4. C library and platform runtime policy

No libc is the universal default.

| Target | Runtime policy |
|---|---|
| Linux GNU | versioned glibc-compatible sysroot for ecosystem compatibility |
| Linux musl | versioned musl sysroot for static/self-contained Linux output |
| Windows | MinGW-w64 headers and import libraries targeting UCRT |
| macOS | Apple SDK and `libSystem`; SDK presence and version are qualified |
| Bare metal, later | Picolibc is the preferred candidate; requires a freestanding Hexal contract |
| WebAssembly, later | WASI SDK and wasi-libc under a distinct target family |
| Cosmopolitan, later | optional APE deployment profile, never the native default |

Newlib remains an option when an existing embedded vendor toolchain requires
it. mlibc is not a mainstream application target; it is relevant only if Hexal
later supports an operating system whose port uses it.

A target pack must qualify every standard header and behavior used by generated
C. A compiler accepting `-std=c23` does not prove that its chosen libc/sysroot
provides the required C23 library surface.

## 5. Translation-unit dialects and C interoperability

The C standard is a translation-unit property, not a link-unit property.

- Every Hexal-generated `.c` file compiles under the profile's pinned C23 mode.
- An imported C source file compiles under its project-declared dialect, such
  as C99, C11, GNU11, or C23.
- Hexal does not rewrite an older C project merely because generated files use
  C23.
- Objects may link only when architecture, object format, ABI, calling
  convention, C runtime, compiler-runtime, and cross-boundary ownership rules
  match the selected profile.
- A foreign header that cannot be included by a C23 translation unit requires a
  bridge translation unit compiled under the foreign project's dialect.
- Static libraries built against a different CRT are rejected or isolated by a
  separately specified ABI bridge. DLL/shared-library use does not waive
  allocator, `FILE`, TLS, or other runtime-ownership constraints.

This permits an older C library and Hexal-generated C23 to coexist without
lowering Hexal's generated dialect to the library's source dialect.

## 6. Trusted profile evidence

### 6.1 Required scalar and layout facts

A profile carries every pre-generation fact required by checking or lowering:

- byte width;
- pointer size, representation, and alignment;
- `size_t` size, alignment, and maximum value;
- scalar sizes, alignments, signed representations, and conversion behavior;
- binary32 and binary64 representation guarantees;
- byte order;
- aggregate layout, padding, union, bit-field, and flexible-array rules where
  foreign declarations expose them;
- function-pointer representation and calling conventions;
- symbol spelling, visibility, and linkage rules; and
- fundamental allocation alignment and any supported over-aligned allocation.

### 6.2 Required runtime and toolchain facts

A profile also carries:

- compiler family and minimum qualified version;
- C23 headers, keywords, attributes, and shared GCC/Clang builtins relied on by
  generated code;
- checked-arithmetic and signed wrapping behavior used by the reference's C23
  output contract;
- atomic widths, lock-free promises, and memory-order support;
- native thread, TLS, signal, process-entry, and stack-switching capabilities;
- platform IO capabilities selected by generated runtime components;
- compiler-runtime and unwind requirements; and
- linker, archive, object-format, and minimum-OS requirements.

The current frontend floor remains GCC 15 or Clang 18. A target profile may
require a newer release. Version alone is never sufficient evidence.

### 6.3 Evidence boundary

- The core compiler never probes the host or invokes a C compiler.
- The driver selects and validates an installed target pack.
- The driver supplies the corresponding path-free profile metadata through
  `Project`.
- Unknown facts are not replaced with host defaults.
- A feature requiring absent evidence is rejected before generation.
- Generated C assertions may corroborate source-dependent target facts but
  cannot substitute for checker input.
- Generic target assertions for byte width, fixed integers, or float format are
  not reintroduced into `hexal.h`.

Unsafe foreign declarations may request additional profile-qualified facts.
`unsafe` relaxes Hexal's language checks; it does not waive target ABI evidence.

## 7. Profile identity and reproducibility

Each profile and target pack has a stable identity derived from at least:

- canonical target triple and ABI variant;
- profile schema version;
- Clang/GCC family and version;
- linker identity and version;
- libc/CRT, SDK, and compiler-runtime identities;
- minimum OS version;
- material ABI flags;
- enabled runtime capabilities; and
- content digests for the pack manifest.

The driver records this identity in build/cache metadata. It participates in
every object and final-link cache key. A pack update cannot reuse objects built
against the previous identity.

Generated C text remains independent of absolute toolchain installation paths.
Equivalent source, `Project`, compiler version, and target profile produce the
same generated artifacts regardless of host path.

## 8. Diagnostics

The core compiler owns source-level diagnostics for a language feature that the
selected profile cannot represent.

The driver owns:

- unknown profile;
- unsupported host/target combination;
- target pack missing, corrupt, or mismatched;
- unsupported external compiler or linker;
- missing C23 facility or resource header;
- incompatible imported object, library, CRT, or ABI;
- C compilation, archive, and link failure; and
- unavailable platform SDK or minimum OS target.

Driver diagnostics preserve the external tool's command, exit status, and
output without reclassifying it as a Hexal syntax or semantic error.

## 9. Compiler and driver boundary

The core API remains:

```text
Compile(sources map[string]string, entrypoint string, project Project)
    CompilationResult
```

The core compiler:

- receives complete Hexal source strings and trusted profile metadata;
- performs checking and C23 generation;
- returns generated filenames and contents as strings; and
- performs no filesystem access, process execution, target-pack discovery, or
  toolchain validation.

RFC 0055's driver:

- reads source and project files;
- installs, locates, and validates backend and target packs;
- materializes generated artifacts;
- compiles generated and foreign translation units under their own dialects;
- archives and links objects; and
- reports build and toolchain failures.

The target profile is build-time configuration, not a Hexal value.

## 10. Validation

Validation is exhaustive for this RFC's eventual implementation.

### 10.1 Core compiler

- Ordinary `go test ./...` remains pure Go and requires no external toolchain.
- The zero-value native profile and every explicit profile produce deterministic
  generated text.
- Cross-compilation uses supplied target metadata and never host layout.
- Missing profile evidence rejects only the feature requiring it, with the
  specified diagnostic.
- Generated artifacts contain no absolute backend, sysroot, or SDK path.
- No generic byte-width, integer-width, or float-representation probe returns to
  `hexal.h`.

### 10.2 Backend distribution

- Each host release reports its backend identity and enabled LLVM targets.
- The bundled Clang accepts the exact generated C23 facility inventory.
- The distributed backend contains only X86, AArch64, ARM, and RISC-V LLVM
  code-generation families.
- Removing an unused LLVM project or tool does not remove an executable or
  resource transitively required by a supported build.
- The core compiler and ordinary tests remain free of cgo and Clang C++ linkage.

### 10.3 Target packs

- Each supported profile compiles and links a minimal generated program.
- Every selected runtime component compiles under the exact target pack.
- The pack manifest identifies and verifies every shipped component.
- A corrupt or identity-mismatched pack is rejected before compilation.
- Unsupported OS/architecture cells are rejected before invoking Clang.
- Linux GNU and Linux musl objects cannot be silently mixed.
- An external override must pass the same facility and ABI qualification as the
  bundled backend.

### 10.4 C interoperability

- One build compiles Hexal-generated C as C23, imported C under a different
  declared dialect, and links the compatible objects successfully.
- An incompatible architecture, object format, calling convention, libc/CRT,
  or compiler runtime is rejected before final linking where metadata makes the
  mismatch decidable.
- A foreign header requiring its own dialect works through a separately compiled
  bridge and is not rewritten by Hexal.

### 10.5 Reproducibility

- Profile identity participates in object and final-link cache keys.
- Changing any material target-pack component invalidates affected objects.
- Changing only the backend installation path does not change generated C or a
  target pack's semantic identity.

## 11. Implementation plan

### Phase 1: profile model

1. Inventory every target fact currently read implicitly from the host or
   assumed by checking and generation.
2. Define one immutable, serializable compiler-facing profile record containing
   only path-free metadata.
3. Add profile selection to `Project` without giving the core compiler any
   discovery or probing path.
4. Make the zero value select one explicit native-default identity supplied by
   the caller.
5. Route current OS/ABI feature checks through the profile.
6. Add pure-Go determinism, missing-evidence, and cross-profile tests.

### Phase 2: backend contract

1. Define the driver-facing backend interface: identity, facility query,
   compile, archive, and link operations.
2. Implement the child-process Clang backend with exact argument capture.
3. Pin Clang, LLD, resource headers, LLVM object tools, and compiler-rt source
   revisions.
4. Create release builds enabling `X86;AArch64;ARM;RISCV` and only `clang;lld`.
5. Measure installed size before and after target restriction, ThinLTO, release
   stripping, and optional size-oriented compilation; retain only verified wins.
6. Verify that no source-level LLVM fork is required.

### Phase 3: first target packs

1. Define the target-pack manifest and content-addressed identity.
2. Build and qualify `x86_64-w64-windows-gnu` with MinGW-w64 UCRT.
3. Build and qualify `x86_64-linux-musl`.
4. Build and qualify `x86_64-linux-gnu` for native library compatibility.
5. Qualify Apple SDK discovery for `x86_64-apple-darwin` and
   `aarch64-apple-darwin`; do not redistribute an SDK without an explicit legal
   and packaging decision.
6. Run the complete C23 facility and runtime-component matrix for each pack.

### Phase 4: driver integration

1. Implement profile and pack selection in RFC 0055's driver.
2. Materialize compiler artifacts in an isolated build directory.
3. Compile every generated `.c` as C23.
4. Compile imported C translation units under their declared dialects.
5. Validate available object metadata before archive or link operations.
6. Invoke the selected linker through the target pack's qualified policy.
7. Preserve external command, stdout, stderr, exit status, and logical source
   mapping in build diagnostics.

### Phase 5: external override

1. Add explicit external Clang and GCC selection.
2. Run the same C23 facility and ABI qualification used for bundled packs.
3. Reject partial matches instead of silently substituting bundled pieces.
4. Record the external toolchain identity in build metadata and cache keys.

### Phase 6: expanded matrix

1. Add AArch64 Linux GNU and musl packs.
2. Add Windows AArch64 UCRT.
3. Add hosted ARM32 Linux only after its ABI and floating-point variants are
   explicitly enumerated.
4. Add RISC-V 64 Linux only after its ABI variants are explicitly enumerated.
5. Treat embedded Picolibc, WASI, Cosmopolitan, and additional POSIX operating
   systems as separate follow-up specifications.

### Phase 7: synchronization and closure

1. Update RFC 0039 to consume this profile evidence for foreign declarations.
2. Update RFC 0055 with the final driver API, pack selection, and cache identity.
3. Update `docs/reference.md` once for the final compiler-visible `Project`,
   target-profile, and C23 backend contracts; do not add installation guidance
   or target-pack tutorials there.
4. Remove hardcoded host assumptions and defensive probes superseded by trusted
   profile metadata.
5. Run pure-Go tests, external C23 validation, supported target-pack smoke
   builds, deterministic artifact comparisons, and the required generated-C
   manifest review.

## 12. Open design work

The architecture above is settled. The following must be detailed before this
RFC becomes implementation-ready:

- the exact compiler-facing profile fields and serialization;
- the target-pack manifest format and distribution mechanism;
- the native-default selection API between `Project` and the driver;
- glibc baseline/version policy for `linux-gnu`;
- exact ARM32 and RISC-V ABI variants;
- macOS SDK discovery, licensing, minimum-version, and linker policy;
- pack signing or other supply-chain verification beyond content digests;
- cache metadata format; and
- the precise driver diagnostics and exit-code contract.

These are backend and packaging decisions. They do not reopen Clang/LLD as the
default, the child-process boundary, the four LLVM target families, C23 for
generated translation units, per-target runtime packs, or the core compiler's
in-memory boundary.

## Reference synchronization

Implementation must review the reference's `Project`, `Size`, layout-query,
Task-target, foreign ABI, and C23 output contracts. The reference must record
only compiler-visible rules and exact target qualification; backend installation,
download, cache, and filesystem procedures remain in RFC 0055 and driver docs.
