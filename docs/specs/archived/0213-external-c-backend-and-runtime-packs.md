# RFC 0213: External Zig Backend and Checked-In Runtime Pack

- Kind: Feature Specification (Rust-Style RFC)
- Status: Partially implemented. Done: `compiler.RuntimeABIVersion = 1`; the
  target identity renamed to `x86_64-windows-gnu-ucrt` with the old spelling
  rejected and `docs/reference.md` updated; the driver profile record with the
  Zig target spelling and pack directory; `-cc`, `-target`, and `-runtime-dir`
  on `build` and `doctor` with the exact Zig 0.16.0 check, no PATH search, and
  no raw tool arguments; the checked-in `lib/x86_64-windows-gnu-ucrt` pack with
  its manifest and upstream licenses, its archives verified byte-for-byte
  against `lib/BUILD.md`; normal builds consuming the demanded include roots,
  archives, and system libraries in the specified link order with a
  dependency-free fast path; strict manifest decoding, runtime ABI/target
  checks, path containment, and demanded-file checks; doctor's full payload
  hashing and two-archive consumption probe; the extended build identity; and
  focused pure-Go and tagged C23 tests. Not done: the vendored-source
  materialization and compilation path (`materializeDependencies`,
  `compileNativeDependencies`, `PlanDependencies`, and the `modules` import) is
  retained for the external libuv/bridge probes and the
  `compiler/tests/c23validation` harness, which must consume the pack instead;
  doctor does not yet run the foreign-target-object probe as part of its own
  exhaustive set; a non-Zig executable that reports no `lib_dir` fails as
  unusable before the exact version-mismatch diagnostic; and the `-cc`
  relative-path resolution against the invocation working directory is
  implemented but not separately tested.
- Created: 2026-09-16
- Updated: 2026-09-17
- Scope: select an installed Zig 0.16.0 C backend and link the checked-in,
  target-qualified static libuv/mimalloc pack
- Depends on: the current build driver, C-interoperability inputs, build modes,
  libuv/mimalloc runtime integration, and target-profile contract
- Coordinates with: automatic C bindings and the deferred object cache
- Amended by: RFC 0214 for the first backend and target; where the two differ,
  RFC 0214 selects installed Clang on WSL and uses `x86_64-linux-gnu`
- Does not add: another compiler family, raw compiler/linker arguments, a pack
  builder, project manifest, package registry, dependency download,
  incremental compilation, or Hexal syntax

## Decision summary

1. Hexal ships no compiler, linker, libc, SDK, or sysroot.
2. `hexal build` receives the exact installed Zig executable through
   `-cc PATH`; it performs no compiler search.
3. The only backend qualified here is Zig 0.16.0 `cc` for
   `x86_64-windows-gnu-ucrt`. The driver inserts `cc` and target arguments.
4. The existing named C-input options remain the complete user surface. Raw
   `-cc-arg` and `-link-arg` escape hatches do not exist.
5. Automatic C binding keeps its existing separately resolved Clang frontend;
   this RFC does not add `-clang` or change that lifecycle.
6. The existing libuv and mimalloc archives, headers, and build record under
   `lib/` are checked-in inputs. Setup, builds, tests, and packaging never
   rebuild them.
7. A small checked-in manifest identifies the target, compiler-owned runtime
   ABI, dependency paths, system libraries, and payload hashes.
8. A normal build performs cheap manifest/path checks and links only demanded
   dependencies. `hexal doctor` performs full hashes and native probes.
9. A program selecting no runtime dependency never reads `lib/`.
10. The core compiler remains string-in/string-out and receives no host paths,
    environment, compiler, archive, or pack metadata.

This is deliberately a concrete first backend, not a framework for hypothetical
compilers. A later Clang or GCC backend must first demonstrate its real command
and ABI differences and receives its own qualification change.

## Boundaries

### Core compiler

The API remains:

```go
func Compile(sources map[string]string, entrypoint string, project Project) CompilationResult
```

The core compiler validates Hexal, emits C/header strings, and records logical
runtime dependencies `libuv` and `mimalloc`. It performs no process or
filesystem operation.

The compiler package exports:

```go
const RuntimeABIVersion uint32 = 1
```

This is the single owner of the ABI expected by generated runtime components.
The driver compares it with the checked-in manifest. It is not a `Project`
field and users cannot override it. Any incompatible generated-runtime change
increments this constant and requires refreshed target packs in the same
change.

### Build driver

The driver owns source discovery/materialization, installed Zig validation,
target selection, checked-in pack resolution, foreign inputs, environment,
compilation, linking, atomic publication, and external-build diagnostics.

### Checked-in runtime pack

`lib/` is the repository source of truth. Its archives are ordinary tracked
release inputs, not caches or generated intermediates. A packaged distribution
copies the exact checked-in target directory beside the `hexal` executable.

The source commits, production commands, archive sizes, and SHA-256 values are
recorded in `lib/BUILD.md`. An explicit future dependency/target/ABI refresh may
replace them, but no refresh tool or automatic rebuild is introduced here.

## Command-line contract

The existing command shape remains flag-based; the entrypoint is not a
positional argument:

```text
hexal build -cc <zig-path> -target x86_64-windows-gnu-ucrt [options]
hexal doctor -cc <zig-path> -target x86_64-windows-gnu-ucrt [-runtime-dir <dir>]
```

### Complete build surface

RFC 0213 adds the first three options and retains the existing remainder:

```text
-cc <path>               exact Zig executable path; required
-target <profile>        exact Hexal target profile; required
-runtime-dir <path>      runtime-pack root override
-mode <name>             debug or release
-root <dir>              source root
-entry <key>             entrypoint logical key; default main.hex
-out <path>              executable output path
-c-source <path>         foreign C source; repeatable
-c-include <dir>         C include directory; repeatable
-c-define <name[=value]> C definition; repeatable
-c-env <name=value>      C-tool environment override; repeatable
-c-standard <dialect>    foreign-source dialect; default c17
-object <path>           precompiled object; repeatable
-archive <path>          static archive; repeatable
-system-library <name>   logical system library; repeatable
```

Rules:

- `-cc` and `-target` are required for `build` and `doctor`.
- `-cc` is a path, not a command name or shell command. `-cc zig` resolves as a
  relative path against the invocation working directory and normally fails;
  PATH is never searched.
- Relative `-cc` and `-runtime-dir` values resolve once against the invocation
  working directory. Diagnostics record their canonical absolute paths.
- Empty, missing, directory, and non-executable compiler paths fail before
  source discovery.
- The driver invokes `<zig-path> cc`; users neither supply nor override `cc`,
  `-target`, `-std`, mode safety flags, phase, input, output, includes,
  definitions, objects, archives, or libraries outside the named options.
- Argument boundaries and repeatable-option occurrence order are preserved;
  no shell expansion or comma splitting occurs.
- Existing `-c-standard` semantics remain: generated Hexal units are C23 and
  foreign sources use the selected foreign dialect.
- Debug/release flags remain driver-owned, so a user cannot remove the debug
  undefined-behavior backstop or redefine release optimization.

If a real C library later needs an unsupported tool argument, add a focused
named option or a separate exact-allowlist specification. Do not introduce an
open-ended denylist.

## Zig backend

### Exact qualification

The driver accepts exactly Zig 0.16.0 for this profile. The existing
`PinnedZigWindows()` record remains the version authority; archive-download
fields in that record are historical verification metadata and do not imply
that Hexal downloads or bundles Zig.

Before source discovery, the driver:

1. starts the exact `-cc` executable;
2. obtains and normalizes `zig version`;
3. requires exact version `0.16.0`; and
4. verifies that `zig cc` accepts target spelling `x86_64-windows-gnu`.

The Hexal target profile is `x86_64-windows-gnu-ucrt`; Zig's toolchain target
spelling intentionally omits the Hexal CRT suffix. The profile establishes
that this Zig target uses the MinGW-w64 GNU ABI over UCRT.

There is no backend interface, plugin registry, compiler-family switch, or
family-specific runtime-pack directory in this RFC. Plain Zig-specific command
functions construct preprocessing, compilation, and linking vectors. A second
compiler justifies the abstraction it actually needs.

### Qualification lifecycle

A normal build performs only the version/target checks above and then the real
build. It does not compile a preliminary C program.

`hexal doctor` performs the expensive checks:

1. compile the complete target-specific generated-C facility probe, including
   every required C23 facility and header;
2. compile and link the exact debug and release option sets;
3. compile and link a foreign target object with generated Hexal objects;
4. fully verify the runtime manifest and every payload hash;
5. compile and link a probe including the checked-in libuv and mimalloc
   headers, representative symbols from both archives, and every declared
   system library; and
6. run the resulting probes because this first profile is native to the
   qualified Windows host.

Ordinary Go tests invoke no external compiler. Command construction and failure
tests use the Go helper-process pattern. External qualification remains behind
the repository's external-C23 test lifecycle.

## Automatic C bindings

This RFC does not supersede automatic-binding frontend selection. The existing
lifecycle remains:

- the selected Zig backend preprocesses with target-relevant include roots,
  definitions, environment, and the existing named C options;
- the separately version-qualified Clang frontend is resolved as already
  specified and parses the preprocessed bytes into a typed JSON AST; and
- builds with no automatic header binding do not require that frontend.

No raw compiler option is forwarded to either stage because none is added.
Changing Clang discovery from PATH to an explicit path is separate work.

## Target profile ownership

The first and only profile implemented here is:

```text
x86_64-windows-gnu-ucrt
```

The compiler's existing private `targetProfile` keeps all current semantic and
generation facts unchanged: identity, OS, architecture, byte order,
pointer/`Size` widths, Windows selection, threading, TLS, fibers, and native IO.
Only its public identity is renamed from the ambiguous
`x86_64-windows-gnu`; the old spelling is rejected without an alias.

Driver-only facts do not enter the core profile. A separate private driver
record for this identity owns:

- exact Zig version and Zig target spelling;
- object, archive, executable, and system-library conventions;
- UCRT/MinGW-w64 policy; and
- the checked-in pack directory.

Adding another compiler/profile pair must prove its C23 facility set, mode
flags, foreign-object compatibility, target ABI, and ability to consume the
same non-LTO target pack. Cross-family archive compatibility is required
evidence, never inferred from C linkage.

## Checked-in pack

### Layout

```text
lib/
  BUILD.md
  x86_64-windows-gnu-ucrt/
    manifest.json
    libuv_v1.52.1/
      include/
        uv.h
        uv/...
      libuv.a
      LICENSE
    mimalloc_v3.5.1/
      include/
        mimalloc.h
        mimalloc/...
      mimalloc.a
      LICENSE
```

The versioned directories, archives, and public headers already exist. This RFC
adds the manifest and copies the upstream license files; it does not rebuild the
archives. The empty untracked `lib/x86_64-linux-gnu/` directory is not part of
the repository or distribution and carries no semantic meaning.

The default installed runtime root is `lib/` beside the physical running
`hexal` executable after resolving executable symlinks. `-runtime-dir`
overrides it. Repository driver tests derive the repository root from their
test source location and set `BuildOptions.RuntimeDir` to its checked-in `lib/`;
the production driver never searches parent directories or source checkouts.

### Manifest

`manifest.json` is UTF-8 JSON with a closed v1 schema:

```json
{
  "format_version": 1,
  "runtime_abi_version": 1,
  "target_profile": "x86_64-windows-gnu-ucrt",
  "dependencies": [
    {
      "name": "libuv",
      "include_root": "libuv_v1.52.1/include",
      "archive": "libuv_v1.52.1/libuv.a",
      "system_libraries": [
        "psapi", "user32", "advapi32", "iphlpapi", "userenv",
        "ws2_32", "dbghelp", "ole32", "shell32"
      ],
      "license_file": "libuv_v1.52.1/LICENSE"
    },
    {
      "name": "mimalloc",
      "include_root": "mimalloc_v3.5.1/include",
      "archive": "mimalloc_v3.5.1/mimalloc.a",
      "system_libraries": [
        "psapi", "shell32", "user32", "advapi32", "bcrypt"
      ],
      "license_file": "mimalloc_v3.5.1/LICENSE"
    }
  ],
  "files": {
    "libuv_v1.52.1/include/uv.h": "<lowercase sha256>",
    "libuv_v1.52.1/libuv.a": "<lowercase sha256>"
  }
}
```

`files` contains every regular payload file under the target directory except
`manifest.json` itself, including all headers, archives, and licenses.

Rules:

- Unknown or missing fields, duplicate JSON names, duplicate dependency names
  or paths, unsupported versions, target mismatch, and dependency order other
  than libuv then mimalloc are rejected.
- `runtime_abi_version` must equal `compiler.RuntimeABIVersion`.
- Hashes are exactly 64 lowercase hexadecimal characters.
- Paths use `/`, are relative, and contain no empty, `.` or `..` component.
  Absolute, drive-qualified, UNC, alternate-data-stream, symlink, junction,
  and other reparse-point paths are rejected; normalized paths stay inside the
  exact target directory.
- Each include root, archive, and license stays inside its dependency's
  versioned directory and is present in `files`.
- Hashes detect repository/distribution corruption, not malicious replacement
  of both payload and manifest. Signatures are deferred.

Normal builds parse the manifest, compare format/ABI/target, validate demanded
paths, and require demanded files to exist. They do not hash headers or
archives. Doctor hashes every listed file and rejects unlisted regular payload
files. This keeps normal compilation fast while retaining an explicit integrity
check.

Manifest SHA-256 and the selected dependencies' listed payload hashes enter the
build identity without rehashing payload bytes. The repository's
`.gitattributes` keeps archives binary and hashed text bytes stable.

## Demand and link order

If `CompilationResult.Dependencies` is empty, the driver does not resolve the
runtime root or read the manifest.

For selected dependencies, the driver validates the demanded manifest entries,
places their include roots before user `-c-include` roots, and applies this
complete link order:

1. generated Hexal objects in deterministic artifact order;
2. compiled foreign-C objects in `-c-source` order;
3. explicit `-object` inputs in command-line order;
4. user `-archive` inputs in command-line order;
5. demanded runtime archives in manifest order;
6. demanded runtime system libraries by manifest dependency and occurrence;
7. user `-system-library` values in command-line order; and
8. the driver-owned output argument.

This extends the existing foreign-input order only by inserting compiler-owned
runtime archives and their libraries before user system libraries. Relative
ordering of every existing user group is unchanged. The Zig compiler driver
performs final linking; Hexal never invokes `ar` or `ld` during a user build.

The installed Zig toolchain supplies UCRT, MinGW-w64, startup objects, and
platform libraries. Neither checked-in archive is treated as a libc. libuv and
mimalloc remain separate archives so dependency demand and failures stay
visible. `uv_replace_allocator` still runs before every other libuv call when
both integrations are selected.

## Build identity

The existing length-delimited build identity additionally includes:

- exact Zig version and Zig target spelling;
- target profile and `compiler.RuntimeABIVersion`;
- manifest SHA-256 and selected listed payload hashes; and
- SHA-256 of each explicit `NAME=VALUE` environment override, including its
  name and value without recording plaintext.

It continues to include Hexal version/mode, generated artifact bytes, ordered
runtime dependencies, mode options, and foreign build inputs. Canonical host
paths remain diagnostic metadata, not generated C content or cache identity.

Equivalent sources, settings, tool identity, environment values, and manifest
produce the same identity and command vectors.

## Exact diagnostics

Configuration failures use these stable forms:

```text
C backend is required; pass -cc <path>
C backend path <path> is not an executable file
C backend <path> reports Zig <actual>; target x86_64-windows-gnu-ucrt requires Zig 0.16.0
target profile <target> is not qualified
runtime pack for x86_64-windows-gnu-ucrt is missing; install the checked-in pack or pass -runtime-dir <path>
runtime pack ABI <actual> is incompatible; this Hexal compiler requires ABI 1
runtime pack path <path> escapes the target directory
runtime pack file <path> is missing
runtime pack file <path> failed SHA-256 verification; restore the checked-in pack
```

External compile/link failures retain the existing stage, exact argument
vector, separated stdout/stderr, and next-action guidance. Environment values
never appear in diagnostics or command records.

## Migration sweep

Implementation must:

- rename `x86_64-windows-gnu` to `x86_64-windows-gnu-ucrt` in core, driver,
  tests, snippets, and reference; retain no alias;
- add `compiler.RuntimeABIVersion` and the private driver profile;
- replace implicit Zig PATH lookup with required `-cc` while retaining the
  exact Zig 0.16.0 qualification;
- add `-target` and `-runtime-dir` to `BuildOptions`, `build`, and `doctor`;
- remove normal-build vendored libuv/mimalloc materialization, compilation,
  module lookup, and source fallback;
- retain vendored sources as provenance for the checked-in archives, not as a
  build input;
- add and track the manifest and upstream licenses beside existing archives;
- remove `/lib/` from `.gitignore` and keep binary/LF attributes for pack bytes;
- preserve automatic-binding Clang discovery and every existing named foreign
  option;
- update build identity with target, ABI, manifest, payload, and environment
  inputs; and
- preserve the string-in/string-out core boundary.

## Non-goals and deferred work

- Clang, GCC, or another compiler backend.
- Backend adapter/plugin architecture.
- Raw compiler or linker argument escape hatches.
- Explicit Clang frontend selection.
- Compiler, libc, SDK, sysroot, or pack download/bundling.
- Pack production tooling or automatic pack refresh.
- Project manifest, package registry, dependency solver, or C build-system
  discovery.
- Family-specific, LTO, sanitizer, debug, or signed runtime packs.
- Cross-compilation beyond the one qualified host/target pair.
- Incremental object caching. A future cache key must include the target,
  backend version, runtime ABI, environment hashes, manifest digest, and
  selected payload hashes.

## Implementation plan

### Phase 1: target and ABI ownership

1. Add `compiler.RuntimeABIVersion = 1` with a focused public-API test.
2. Rename the target constant/profile to `x86_64-windows-gnu-ucrt`; update all
   current tests and reject the old spelling.
3. Keep every current core profile field; add the private driver-only profile
   with Zig target/version, artifact conventions, UCRT policy, and pack key.

### Phase 2: explicit Zig and CLI

1. Add `CompilerPath`, `Target`, and `RuntimeDir` to `BuildOptions`.
2. Add the three flags to `hexal build`; give `hexal doctor` the same selection
   flags while retaining every current build option and `-entry` semantics.
3. Resolve/canonicalize the explicit path, verify exact Zig 0.16.0, and build
   direct `zig cc` command vectors.
4. Replace PATH discovery without changing automatic-binding Clang discovery.
5. Add helper-process tests for missing/invalid/version-mismatched tools and
   exact arguments; ordinary tests invoke no compiler.

### Phase 3: adopt the checked-in pack

1. Verify the existing archives against `lib/BUILD.md`: source revisions,
   commands, target, byte sizes, SHA-256 values, and native two-library probe.
   Do not rebuild them.
2. Copy upstream licenses, generate the closed manifest from the checked-in
   payload, and track the complete `lib/` tree.
3. Implement strict manifest decoding, runtime ABI/target checks, path
   containment, demanded-file existence checks, and full doctor verification.
4. Update driver tests to derive the repository root and set the checked-in
   runtime directory explicitly.

### Phase 4: consume the pack

1. Replace normal-build source materialization with dependency-demanded pack
   paths and include roots.
2. Implement the exact link ordering and no-dependency fast path.
3. Preserve current generated C and automatic-binding behavior.
4. Remove every normal-build submodule/source fallback and stale comment.

### Phase 5: doctor, identity, and documentation

1. Implement doctor's full facility, mode, foreign-object, hash, link, and run
   probes using the selected Zig and checked-in pack.
2. Extend build identity with every newly named input and secret-safe hashes.
3. Run ordinary tests without external tools, then external C23, doctor,
   foreign-object, debug, release, and runtime-pack qualification.
4. Confirm generated C and snippet hashes do not move merely because native
   dependencies changed delivery form.
5. Update `docs/reference.md` only for the renamed `Project.Target` identity;
   backend selection, pack layout, and qualification remain driver contracts.
6. Update status and close only after every validation item passes.

## Validation (exhaustive)

- Core compiler tests remain process-free and filesystem-free.
- `compiler.RuntimeABIVersion` is exactly 1 and the manifest must match it.
- The old target spelling is rejected; `x86_64-windows-gnu-ucrt` is the only
  qualified profile and all prior semantic profile facts remain unchanged.
- The full CLI retains `-entry` and every existing named C option, adds exactly
  `-cc`, `-target`, and `-runtime-dir`, and adds no raw tool-argument flag.
- `-cc` accepts explicit absolute/relative paths, never searches PATH, inserts
  `cc` exactly once, and rejects non-Zig or non-0.16.0 executables.
- Existing automatic bindings retain their Clang lifecycle and receive no new
  raw option or CLI surface.
- Dependency-free programs never resolve `lib/` or parse its manifest.
- Mimalloc-only, libuv-only, and combined programs consume exactly the demanded
  include roots, archives, and system libraries in the specified order.
- Strict manifest parsing rejects unknown/missing/duplicate fields, unsupported
  format/ABI, wrong target/order, malformed hashes, escaping/reparse paths, and
  missing demanded files before compilation.
- Normal builds do not hash payload files. Doctor hashes every listed file,
  rejects unlisted payload files, and detects either archive's corruption.
- The libuv include tree is complete for `uv.h`; checked-in headers, archives,
  licenses, and manifest are present in a fresh checkout and copied byte-for-byte
  into a distribution.
- Existing archive byte sizes and SHA-256 values match `lib/BUILD.md`; initial
  implementation does not rebuild them.
- Normal builds never compile vendored libuv or mimalloc source and have no
  source fallback.
- Doctor verifies C23 facilities, both modes, foreign-object compatibility,
  both archives, declared system libraries, and native runtime behavior.
- Build identity changes with backend version, target, runtime ABI, manifest,
  selected listed hashes, environment values, and every prior identity input;
  plaintext environment values appear in no record.
- Equivalent sources/settings/tool/environment/manifest produce identical
  generated C, build identity, and command vectors.
- Host/compiler/runtime paths do not appear in generated C or compiler result
  files.
- Only the target identity changes in `docs/reference.md`; status is updated
  before closure.
