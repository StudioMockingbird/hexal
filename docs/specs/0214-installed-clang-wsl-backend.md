# RFC 0214: Installed Clang Backend on WSL

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-17
- Updated: 2026-09-17
- Scope: revise RFC 0213's first backend and target from Zig on Windows to
  installed Clang on WSL/Linux and produce one repository-local compiler binary
- Amends: RFC 0213's backend, command-line entrypoint form, initial target,
  runtime-pack ownership, diagnostics, implementation plan, and validation
  where explicitly replaced below
- Does not change: direct in-memory C-header import, generated C23, the
  string-in/string-out compiler boundary, explicit tool selection, runtime ABI,
  dependency demand, foreign-input options, build identity, or manifest rules

## Decision

The initial supported environment is:

```text
host environment:       WSL/Linux on x86_64 Windows
C compiler:             installed Clang 18 or newer
Hexal target profile:   x86_64-linux-gnu
Clang target triple:    x86_64-linux-gnu
system ABI and libc:    Linux glibc supplied by WSL
runtime pack:           lib/x86_64-linux-gnu/
output:                 Linux ELF executable
```

Users install Clang before using Hexal. Hexal does not download,
bundle, update, or build Clang, LLVM, a linker, libc, SDK, or sysroot.

Compiling the compiler writes exactly one repository-local artifact:
`bin/hexal`. It does not use `go install` and writes nothing into the user's Go
binary directory or WSL's system `/bin`.

Clang is the only compiler backend qualified by this RFC. GCC, Zig, native
Windows, and other targets are future work. This is deliberate YAGNI: no
compiler-family interface or plugin architecture is introduced before a second
backend is actually accepted.

## Why this RFC exists

The current driver is Zig- and Windows-specific while development is moving to
WSL with Clang installed. This RFC changes only the build environment:

- direct Clang invocation replaces `zig cc`;
- `x86_64-linux-gnu` replaces `x86_64-windows-gnu-ucrt` as the first qualified
  profile;
- WSL supplies the linker, startup objects, headers, and glibc; and
- a checked-in Linux runtime pack replaces the Windows pack for this target.

Direct C-header import already uses a separately installed Clang and creates no
project-visible binding file. This RFC does not redesign or re-specify that
feature. It reuses the selected Clang executable for both header inspection and
final C compilation, removing the redundant second lookup.

## Compiler build artifact

The compiler build produces:

```text
<repository>/bin/hexal
```

The build injects Hexal's timestamp version through the existing version
mechanism. `bin/hexal` is a build output, not a tracked source file.

The checked-in `lib/` directory is also a small Go resource package. A single
`//go:embed x86_64-linux-gnu` declaration embeds its manifest, headers, static
archives, licenses, and notices in `hexal` at build time. The driver reads that
immutable filesystem and materializes only demanded entries into the private
build staging directory before invoking Clang.

Release builds always use embedded resources and never require adjacent files.
Dependency-free programs neither verify nor materialize native dependency
entries. Tests substitute an internal filesystem seam rather than exposing a
runtime-pack path through the CLI or `BuildOptions`.

The executable contains the required third-party license and notice text in the
embedded pack. `hexal play` remains part of the same executable; no workbench
binary is produced.

## Command-line contract

RFC 0213's build surface remains:

```text
hexal build <filepath> -cc /usr/bin/clang -target x86_64-linux-gnu [options]
hexal build -entry <key> -cc /usr/bin/clang \
    -target x86_64-linux-gnu [project options]
hexal doctor -cc /usr/bin/clang -target x86_64-linux-gnu
```

- `-cc` is a path to Clang, not a command line. PATH is not searched.
- The driver invokes that path directly; it does not insert `cc`.
- `-target` selects a compiler-owned Hexal profile, not an arbitrary triple.
- The driver maps the profile to `--target=x86_64-linux-gnu`.
- The same selected Clang path performs automatic C-header inspection when
  imports demand it.
- `build` accepts zero or one positional filepath, before or after named
  options. More than one is rejected.
- With a filepath, the canonical parent directory is the source root and the
  basename is the logical entrypoint. The path must name one regular `.hex`
  file. `-root` and `-entry` are forbidden because the filepath already defines
  both.
- Without a filepath, the invocation is a project build: `-entry` is required,
  `-root` selects the source root and defaults to the invocation directory, and
  the entrypoint is a slash-separated logical key under that root.
- File-build imports resolve under the selected file's parent and may not walk
  above it. A broader module tree uses the project form.
- All existing named C-source, header, object, archive, library, include,
  define, environment, output, and build-mode options remain unchanged.
- `-runtime-dir` is removed from `build`, `doctor`, and `BuildOptions`. Pack
  selection is compiler-owned; internal tests inject a filesystem directly.
- No raw compiler or linker argument option is added.

## Qualification

A normal build performs only cheap checks:

1. resolve the explicit `-cc` path;
2. require a regular executable file;
3. run `clang --version` and require major version 18 or newer;
4. select `x86_64-linux-gnu`; and
5. require the runtime pack only when generated dependencies demand it.

`hexal doctor` additionally proves that the selected Clang:

- accepts `--target=x86_64-linux-gnu` and `-std=c23`;
- provides every C23 facility generated code uses;
- finds the WSL linker, startup objects, Linux headers, and glibc;
- compiles, links, and runs debug and release probes;
- preprocesses and inspects a representative imported header;
- links a foreign object produced by that same Clang;
- verifies the demanded checked-in libuv and mimalloc archives; and
- compiles, links, and runs a program using both archives.

Hexal records the complete normalized Clang version in build identity. It
requires a minimum major version, not one exact patch release.

## Runtime pack

The initial checked-in pack is:

```text
lib/x86_64-linux-gnu/
  manifest.json
  libuv_v<version>/
    include/
    libuv.a
    LICENSE
  mimalloc_v<version>/
    include/
    mimalloc.a
    LICENSE
```

The existing Windows archives cannot be reused: they contain Windows/MinGW
objects and system-library assumptions. The Linux archives are produced once
for `x86_64-linux-gnu`, verified, recorded in `lib/BUILD.md`, and checked in.
Normal setup, builds, tests, and packaging never rebuild them.

The manifest follows RFC 0213 and records the exact target, glibc ABI,
`compiler.RuntimeABIVersion`, producer Clang identity, dependency paths, Linux
system libraries, sizes, and hashes. Dependency-free programs do not inspect
the pack.

## Diagnostics

```text
C backend is required; pass -cc <path>
C backend path <path> is not an executable file
C backend <path> is not Clang 18 or newer
C backend <path> does not accept target x86_64-linux-gnu with C23
target profile <target> is not qualified
embedded runtime pack for x86_64-linux-gnu is missing or corrupt; rebuild bin/hexal
build accepts at most one source filepath
build filepath cannot be combined with -root or -entry
build filepath must name a regular .hex file
project build requires -entry when no filepath is given
```

RFC 0213's manifest, path, ABI, hash, external-process, and secret-redaction
diagnostics remain unchanged apart from backend and target names.

## Implementation plan

### Phase 1: target and backend

1. Add the core `x86_64-linux-gnu` profile and its Linux/glibc facts.
2. Replace the active Zig profile with one direct-Clang driver profile.
3. Replace `zig version`, `zig env`, and `zig cc` handling with explicit Clang
   version parsing and direct invocation.
4. Preserve the existing driver shape; do not add a compiler abstraction.
5. Restore one positional filepath in `hexal build`; derive root and entrypoint
   exactly as specified above and make the no-file form require `-entry`.
6. Remove `-runtime-dir` from CLI and `BuildOptions`; replace its test uses with
   a private injected filesystem.

### Phase 2: C imports

1. Remove independent Clang PATH discovery.
2. Pass the selected Clang identity into the existing header-inspection path.
3. Preserve its current preprocessing, AST normalization, in-memory synthetic
   module, limits, diagnostics, and no-written-binding behavior.
4. Confirm builds without C-header imports perform no inspection work.

### Phase 3: Linux runtime pack

1. Produce or obtain target-correct libuv and mimalloc archives once outside
   normal Hexal builds.
2. Verify them with native compile/link/run probes and record exact commands,
   versions, sizes, and SHA-256 values in `lib/BUILD.md`.
3. Check in complete headers, licenses, archives, and the closed manifest under
   `lib/x86_64-linux-gnu/`.
4. Preserve the existing Windows pack unchanged as unqualified future input.

### Phase 4: compile and link

1. Emit direct Clang commands with `--target=x86_64-linux-gnu` and existing
   debug/release C23 flags.
2. Replace Windows suffixes and system libraries with Linux profile metadata.
3. Consume only demanded Linux pack components in recorded link order.
4. Preserve all foreign-input ordering and environment rules from RFC 0213.

### Phase 5: qualification and sweep

1. Replace active Zig/Windows driver fixtures with Clang/Linux fixtures.
2. Implement the doctor probes above.
3. Remove assumptions that Zig supplies the compiler, libc, or sysroot; retain
   no dead Zig adapter or generic backend layer.
4. Keep ordinary Go tests external-process-free; real Clang checks remain in
   the tagged/external lifecycle.
5. Verify generated C and snippet hashes do not move solely because the driver
   changed backend.
6. Update `docs/reference.md` only for the qualified target identity after
   explicit user approval; backend selection remains a driver contract.

### Phase 6: compiler build output

1. Add one compiler-build command that creates `bin/` when absent, builds
   `cmd/hexal` for the current WSL/Linux host, injects the timestamp version
   through the existing linker setting, and writes `bin/hexal` atomically.
2. Add one hand-written `lib` resource package containing the single embed
   directive and a read-only accessor; do not generate Go source or byte arrays.
3. Reuse the manifest's closed file list and hashes against the embedded
   filesystem; implement demand-driven materialization into the existing
   private staging directory.
4. Reject an incomplete runtime pack, version mismatch, unexpected payload,
   absolute/escaping entry, or digest mismatch.
5. Exercise `bin/hexal` directly: run `version`, `doctor`, a dependency-free
   build, a combined libuv/mimalloc build, and `play` startup.
6. Remove active build instructions and tests that invoke `go install` or write
   another Hexal/workbench executable.

## Validation (exhaustive)

- RFC 0213's runtime ABI, manifest, path-containment, dependency-demand,
  environment, build-identity, and compiler-boundary validations still pass.
- `-cc` invokes the supplied Clang path directly and never searches PATH.
- Non-Clang executables and Clang below major 18 receive the exact diagnostic.
- `hexal build app.hex` derives the source root and entrypoint from that file;
  the equivalent named-option build produces identical generated C and program
  behavior.
- Positional filepath placement before or after named options is equivalent;
  multiple paths, non-`.hex` paths, non-regular paths, and filepath plus
  `-root`/`-entry` receive the exact diagnostics above.
- A no-file project build requires `-entry`; `-root` defaults to the invocation
  directory and imports may address the complete declared project tree.
- `-runtime-dir` is absent from CLI help, parser, `BuildOptions`, doctor, and
  diagnostics; pack tests use only the private filesystem seam.
- Every compile, link, preprocess, and header-inspection command uses the same
  selected Clang identity and target profile.
- Direct C-header import retains its current syntax, semantics, normalization,
  diagnostics, and in-memory-only behavior.
- A dependency-free program compiles, links, and runs without reading `lib/`.
- Mimalloc-only, libuv-only, and combined programs use exactly the demanded
  checked-in Linux headers, archives, and Linux system libraries.
- Windows archives and Windows system libraries are never selected.
- Foreign C source, header, object, archive, and system-library inputs work with
  the selected Clang under existing ordering rules.
- Doctor proves C23 facilities, glibc linkage, both build modes, header import,
  foreign-object compatibility, pack integrity, and combined runtime behavior.
- Normal builds never download or rebuild Clang, libuv, or mimalloc.
- Compiling the compiler creates only `bin/hexal`; it does not invoke
  `go install`, write another executable, or mutate a system directory.
- `bin/hexal` carries the timestamp version, complete Linux runtime pack, and
  required notices and runs `version`, `doctor`, `build`, and `play` directly.
- Dependency-demanded payload extraction is private, strict, and staging-local;
  dependency-free programs materialize nothing.
- Embedded-pack validation rejects incomplete, unexpected, absolute, escaping,
  or corrupt payloads.
- No active compiler-build instruction uses `go install`.
- Ordinary Go tests pass with no external compiler installed.
- Generated C, core compiler API, and unrelated snippet hashes remain unchanged.

## Deferred

- GCC, Zig, or another C compiler backend.
- A compiler-family abstraction or plugin system.
- Native Windows, macOS, AArch64, musl, and cross-compilation targets.
- Bundling or downloading Clang, LLVM, a linker, libc, SDK, or sysroot.
- Automatic compiler discovery or installation.
- Release archives, installers, package-manager formats, PATH edits, updates,
  uninstallers, or download hosting.
