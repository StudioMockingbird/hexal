# RFC 0052: C Compiler Backend

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; every Validation item is implemented and covered by
  pure-Go tests plus the non-skipping `c23` qualification gate
- Created: 2026-08-13
- Updated: 2026-09-12
- Depends on: the reference's `Project`, scalar, `Size`, layout-query,
  Task-target, and C23 output contracts
- Coordinates with: RFC 0055 (filesystem and build driver) and RFC 0039
  (future C interoperability)

## Summary

Hexal's first binary backend is an installed Zig 0.16.0 found through `PATH`
and invoked as `zig cc` in child processes. It qualifies exactly one output
profile: `x86_64-windows-gnu`, dynamically linked against UCRT.

This RFC deliberately does not design every future host, target, SDK, foreign
compiler, or C-import workflow. It establishes the smallest backend that can
turn generated Hexal C23 into a Windows executable without weakening the
architecture needed to add those capabilities later.

The core compiler remains an in-memory, process-free transformation. RFC 0055's
driver owns backend discovery, filesystem access, process execution, object
production, and linking.

## Goals

- Use one pinned, C23-capable backend version for x86-64 Windows.
- Compile every generated translation unit and link one native executable.
- Keep the compiler string-in/string-out and host-neutral by default.
- Make target-dependent checking and generation consume explicit trusted
  profile identity rather than host inspection.
- Preserve readable generated C and `#line` source mapping.
- Leave a narrow extension point for later targets and external compilers.

## Non-goals

- Linux, macOS, AArch64, ARM32, RISC-V, WASI, embedded, Cosmopolitan, or other
  qualified profiles in this RFC.
- Cross-compilation as a supported user feature.
- External GCC or Clang selection.
- Apple SDK or other external-resource packaging.
- Source-visible C imports, headers, projects, objects, or libraries; RFC 0039
  owns that language and ABI work.
- Static linkage.
- A universal libc.
- Building, trimming, patching, or statically linking Zig/Clang/LLVM.
- Bundling, downloading, installing, or verifying a Zig distribution.
- Backend discovery, download, caching, or process execution in the core
  compiler.
- An object cache or incremental compilation.

## Backend discovery

The driver resolves `zig` through `PATH`. It accepts the result only when
`zig version` is exactly `0.16.0` and `zig env` reports a readable `lib_dir`.
Missing Zig, a different version, or an incomplete installation is a backend
diagnostic before C compilation begins.

Hexal neither downloads Zig nor looks beside `hexal.exe` for a private copy.
This is the complete v1 discovery rule, not a fallback path. A future CI/CD and
distribution specification may replace it with a bundled, verified backend.

The driver invokes `zig cc` as a child process. It never links Zig or LLVM into
the Go compiler. This boundary provides process isolation, no cgo dependency,
capturable commands and output, and replaceable backend implementation.

Zig chooses and invokes its linker. Depending on target and pinned release,
that may be LLD or a Zig linker implementation. Hexal neither selects nor
invokes a linker executable independently; it records the effective linker
identity reported by the backend.

## Target profile API

Callers select compiler-owned profile identities; they do not construct ABI
facts.

Conceptual Go surface:

```go
type TargetProfileID string

const TargetX86_64WindowsGNU TargetProfileID = "x86_64-windows-gnu"

type Project struct {
    Target TargetProfileID
    // Existing project settings remain.
}
```

Rules:

- `Project{}` remains host-neutral and preserves the existing deterministic
  generated-C contract.
- `Target == TargetX86_64WindowsGNU` selects the one profile qualified here.
- Any other non-empty identity is rejected as an unqualified target before
  checking or generation.
- The driver passes the explicit qualified identity for a real binary build.
- The public identity is string-backed for stable serialization, diagnostics,
  and build records; arbitrary string conversion does not create a valid
  profile.
- Profile records are immutable compiler-owned data. A caller cannot supply or
  override individual ABI facts.

The compiler's private profile record contains only facts consumed by the
current checker or generator:

```text
identity              x86_64-windows-gnu
target OS             windows
target architecture   x86_64
byte order             little endian
byte width             8 bits
pointer width          64 bits
Size/size_t width      64 bits
runtime capabilities   qualified Windows threading, TLS, fiber and IO paths
```

Do not add a general ABI database. A new fact enters the compiler record only
when checking or generation consumes it. RFC 0055 separately maps the same
profile identity to Zig's `x86_64-windows-gnu` spelling and dynamic-UCRT link
policy. Backend paths, flags, and linkage configuration never enter `Project`.

## Generated-C target selection

An explicit target profile emits only the selected platform implementation.
For `TargetX86_64WindowsGNU`, generated runtime components contain the Windows
paths and omit inactive POSIX branches and headers.

The host-neutral `Project{}` zero value retains today's conditional generated C
so direct compiler callers keep deterministic, portable artifacts without host
inspection.

In both modes:

- target-dependent decisions never inspect the Go host;
- missing required profile evidence fails before generation;
- equivalent sources and `Project` values produce byte-identical artifacts;
- generated artifacts contain no backend, SDK, or installation path; and
- generic target assertions for byte width and fixed scalar representations do
  not return to `hexal.h`.

## C23 facility qualification

The installed pinned backend must accept every standard facility and platform header
reachable from generated output. RFC 0055 owns the exact demand-driven header
inventory and facility probe.

Qualification is stronger than compiling `int main(void) { return 0; }`. It
must compile and link fixtures selecting every generated runtime component and
every C23 facility the generator emits, including checked arithmetic,
atomics, `typeof`, `nullptr`, attributes, static assertions, and native Windows
threading/IO paths.

The qualification gate fails when the pinned backend is absent, incomplete,
mismatched, or lacks a facility. Developer-local external-toolchain tests may
skip when their explicitly optional toolchain is unavailable; release
qualification may not.

## Runtime and linkage

The qualified profile uses Zig's installed MinGW-w64 headers and import material
with dynamic UCRT linkage. Static linkage is unavailable in this RFC.

The profile is qualified only when:

- every selected generated C/header artifact compiles as C23;
- every resulting object has the expected COFF architecture and ABI;
- the objects link through `zig cc`;
- the executable runs on the qualified x86-64 Windows host; and
- representative generated programs covering every runtime component produce
  their expected exit status and output.

One driver-level fixture compiles a checked-in minimal C source with the pinned
backend into a target object, then links that object with Hexal-generated
objects. This proves that the backend can consume ordinary target objects
without checking a platform-specific binary into the repository. The fixture
does not introduce Hexal C-import syntax or claim full RFC 0039
interoperability.

## Identity and reproducibility

```text
Observed backend record = resolved executable + exact Zig version
                          + reported lib_dir and Clang/LLVM/linker identity
Target identity  = TargetProfileID + profile schema version
Build record     = observed backend record + target identity
                   + generated input digest + compile/link options
```

The record makes a build attributable but does not prove that two installed
Zig trees with the same reported version have identical contents. It is not a
stable backend identity suitable for persistent object reuse. Persistent object
caching remains blocked rather than guessing equivalence from a version string.

## Diagnostics

The core compiler owns configuration diagnostics for an unknown profile or a
profile fact required by checking or generation but absent from its private
record.

The driver owns:

- backend missing, corrupt, incomplete, or version-mismatched;
- target identity unknown or unqualified;
- host not qualified to run the selected profile;
- C23 facility or required header unavailable;
- C translation-unit compilation failure; and
- final link failure.

Driver failures carry a stable stage, complete argument vector, separated
stdout/stderr, and child exit status. The v1 CLI exits `1` for every failed
build while retaining the child status in the diagnostic record. Compiler
diagnostics pass through unchanged.

## Compiler/driver boundary

The core API remains:

```text
Compile(sources map[string]string, entrypoint string, project Project)
    CompilationResult
```

The compiler receives source strings and a profile identity, performs no host
or toolchain probe, and returns generated filenames and contents as strings.

RFC 0055's driver locates the installed backend, supplies the explicit profile,
materializes artifacts, invokes `zig cc`, and publishes the executable.

## Validation

This section is exhaustive for RFC 0052.

- `Project{}` remains accepted and produces the current host-neutral output.
- `TargetX86_64WindowsGNU` resolves to exactly one immutable internal record.
- Unknown non-empty target identities fail before checking or generation.
- Callers cannot supply or override individual ABI facts.
- An explicit Windows profile emits only Windows implementation paths and
  headers; inactive POSIX branches are absent.
- The host-neutral zero value retains the current conditional platform output.
- Equivalent source and `Project` inputs produce byte-identical artifacts.
- Target-sensitive behavior uses profile facts and never the Go host.
- Generated artifacts contain no absolute backend or build path.
- The driver resolves `zig` only through `PATH`, requires exactly version
  `0.16.0`, and validates its reported `lib_dir` before C compilation.
- A missing, incomplete, or wrong-version installation is rejected, and no
  build downloads a backend.
- The installed backend record is attributable but cannot use the persistent
  object cache as a stable backend identity.
- The complete generated C23 facility and header suite passes through the
  pinned backend.
- Every demand-driven runtime component compiles and links under
  `x86_64-windows-gnu`.
- A representative generated executable runs on x86-64 Windows with exact
  expected output and exit status.
- A compatible driver-level C object links successfully without introducing
  source-visible C imports.
- C compilation and linking are separate stages with separate diagnostics.
- Target identity and generated artifacts are deterministic and
  path-independent; the driver records the resolved installed backend path.
- Ordinary `go test ./...` remains pure Go and requires no external toolchain.

## Implementation plan

Implementation ownership:

```text
compiler/project.go and generator   profile identity, facts, validation,
                                    target-specialized generated C
internal/backend                    PATH discovery, version and facility
                                    validation, compile/link operations
internal/driver                     consumes the backend under RFC 0055
```

### Phase 1: compiler-owned profile

1. Add `TargetProfileID`, `TargetX86_64WindowsGNU`, and `Project.Target`.
2. Add the minimal private record and one registry entry described above.
3. Extend the existing `validateProject` pre-lexing hook to validate the target;
   preserve the host-neutral zero value.
4. Route every currently target-sensitive checker/generator decision through
   the selected profile when non-empty.
5. Add pure-Go zero-value, unknown-profile, determinism, fact-routing, and
   platform-branch tests.

### Phase 2: installed backend

1. Resolve `zig` through `PATH`.
2. Implement exact version and `lib_dir` validation.
3. Record the resolved executable and reported tool identities for diagnostics.
4. Mark the installed backend ineligible for persistent object caching.

### Phase 3: backend interface

1. Define the `internal/backend` operations required by RFC 0055:
   identity, facility qualification, compile one translation unit, and link.
2. Implement them with child-process `zig cc` calls.
3. Capture arguments, stdout, stderr, exit status, and effective tool identity.
4. Do not add archive or external-compiler operations until a caller exists.

### Phase 4: first target qualification

1. Specialize explicit-profile generated runtime components to Windows.
2. Run the complete facility/header suite.
3. Compile and link every selected runtime component.
4. Run representative generated programs on x86-64 Windows.
5. Inspect resulting objects and link the compatible-object fixture.

### Phase 5: synchronization and closure

1. Complete RFC 0055's profile and backend integration.
2. Run pure-Go tests, the external C23 gate, deterministic artifact comparison,
   runtime fixtures, and generated-C manifest review.
3. With explicit approval, update `docs/reference.md` once with the qualified
   profile, `Project.Target`, and generated-C target contract.
4. Remove hardcoded target/backend assumptions superseded by the profile.

## Deferred work

Each item requires its own specification or an explicit extension when picked
up:

- x86-64/AArch64 Linux using glibc or musl;
- x86-64/AArch64 macOS base programs;
- Apple-framework SDK discovery and licensing;
- AArch64 Windows, ARM32, RISC-V, WASI, embedded and Cosmopolitan targets;
- cross-compilation as a supported workflow;
- external GCC/Clang backends;
- static linkage;
- source-visible C interoperability;
- in-process backend integration, only if profiling proves process startup is
  material;
- bundled-backend assembly, archive locking, and CI/CD verification; and
- a stable backend identity suitable for persistent object caching.

Measured future note: Zig 0.16.0 can compile and link base hosted C for the
macOS `*-macos-none` targets without an Apple SDK; Apple frameworks require a
separately sourced SDK. Do not turn that observation into a supported profile
without qualification.

## Reference synchronization

This RFC changes a compiler-visible build contract but no Hexal syntax. After
behavior stabilizes and with explicit approval, record only the exact qualified
profile, `Project.Target` semantics, and target-dependent C23 contract in
`docs/reference.md`. Backend installation and commands remain driver
documentation.
