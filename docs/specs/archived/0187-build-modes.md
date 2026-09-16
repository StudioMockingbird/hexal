# RFC 0187: Build Modes

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented with one documented scope narrowing beyond
  this RFC's own text. `hexal build -mode debug|release` is in place,
  driven from one option table (`internal/driver` stays mode-unaware):
  debug is `-O0` with target-native debug information and a
  non-recoverable UBSan backstop, release is `-O2` with no debug
  information and `-ffunction-sections`/`-fdata-sections` plus linker
  `--gc-sections`; both disable floating-point contraction. Generated C
  is unaffected by mode. Windows debug links under a build-identity SHA-256
  versioned basename, publishes its PDB first, then atomically publishes
  the executable as the commit point; a same-identity PDB must be
  byte-equal or the build fails before publication, and old PDBs are
  retained. `hexal play` always builds debug; `hexal doctor` verifies
  both option sets. Deviation: the release lane added to the tagged C23
  harness runs and compares stdout/stderr/exit status across modes only
  for fixtures carrying a run expectation; a compile-only fixture and
  every workbench snippet are compile-only compared under both modes
  instead, because several of them (an OS-signal wait, a blocking stdin
  read) never terminate under automated execution regardless of mode --
  the same restriction the pre-existing tiered harness and
  `TestC23SnippetCatalogCompiles` already apply to the identical catalog,
  for the identical reason. Verified by `go test ./...`, `go vet ./...`,
  and the tagged C23 suite (including the new release lane and a
  representative-program size/link-time/runtime measurement recorded in
  `docs/benchmarks.md`) running under GCC, Clang, and `zig cc`.
  `docs/reference.md` is not yet updated: that edit awaits explicit user
  approval per this RFC's own text
- Created: 2026-09-14
- Updated: 2026-09-15
- Scope: add `debug` and `release` build modes to the driver, defining exactly
  which backend options change and which program behavior must not
- Depends on: ADR 0055 (filesystem and build driver), RFC 0052 (C compiler
  backend), and the C23 output contract in `docs/reference.md`
- Coordinates with: RFC 0183 (sanitizer coverage and target qualification) and
  RFC 0186 (whole-module emission, unused functions removed at link time)
- Does not add: a mode that removes safety checks, a `Project` field, generated-C
  differences between modes, user-selectable C flags, LTO, cross-compilation, or
  a project manifest

## Current behavior (verified)

`hexal build` passes no optimization option for generated C. With the pinned Zig
0.16.0, `zig cc -### -std=c23 -target x86_64-windows-gnu -c` expands to Zig's
Debug defaults:

- `-O0`;
- `-fsanitize=` for alignment, array-bounds, bool, builtin, enum,
  float-cast-overflow, integer-divide-by-zero, nonnull-attribute, null,
  pointer-overflow, return, returns-nonnull-attribute, shift-base,
  shift-exponent, signed-integer-overflow, unreachable, and vla-bound, with
  `-fsanitize-recover` for most of them;
- CodeView debug info, unwind tables, and the stack protector; and
- `-target-cpu x86-64` (baseline) and `-ffp-contract=on`.

Adding `-O2` removes the sanitizer set and the stack protector but keeps CodeView
debug info. libuv and mimalloc are always compiled with `-O2`.

So every Hexal executable today is an unoptimized, UBSan-instrumented debug
build, and nobody chose that.

## Principle: modes never change semantics

Zig's `ReleaseFast` and C's `-DNDEBUG` remove checks. Hexal cannot do that:
bounds, overflow, division, conversion, freed-state, and handle checks are
language semantics, not debug assertions ("no undefined behavior", "if it
compiles, it runs").

Therefore:

1. The compiler produces **byte-identical generated C in every mode**. Mode is a
   driver/backend setting only; `Project` gains no field.
2. For every program, stdout, stderr, exit status, and every runtime trap message
   are identical across modes.
3. Modes change only optimization, debug information, backstop instrumentation,
   and executable size.

The one allowed observable difference is resource exhaustion that depends on
code generation, most importantly Task stack overflow: stack frame sizes differ
between `-O0` and `-O2`, so recursion depth that traps in debug may complete in
release. This is a resource limit, not semantics, and the reference must say so.

## Modes

Two modes. No `small`, `fast`, or `safe` variants in v1.

| Setting | `debug` (default) | `release` |
| --- | --- | --- |
| Optimization | `-O0` | `-O2` |
| Debug info | Target-native debug information; Windows uses an immutable versioned PDB | none (`-g0`, linker `-s`) |
| UBSan backstop | Zig default set, non-recoverable | none |
| Stack protector | Zig default | Zig default for `-O2` |
| Floating contraction | `-ffp-contract=off` | `-ffp-contract=off` |
| Section GC | no | `-ffunction-sections -fdata-sections`, linker `--gc-sections` |
| CPU | target profile's baseline CPU | same target profile baseline |
| libuv and mimalloc | `-O2`, unchanged | `-O2`, unchanged |

Rationale for each choice:

- **UBSan in debug is a backstop for compiler bugs.** Generated C is required to
  be free of undefined behavior, so a UBSan report is always a generator defect.
  It must terminate rather than continue, so debug uses
  `-fno-sanitize-recover=undefined`. Release omits it because the contract says
  it can never fire.
- **`-ffp-contract=off` in both modes.** Contraction into fused multiply-add
  changes floating results. Baseline x86-64 has no FMA today, so this is
  latent, but float output must never depend on mode or a future CPU choice.
- **Section GC in release** removes unused generated helpers, unused functions of
  imported modules (RFC 0186 emits modules whole), and unused runtime definitions
  at link time. It
  changes no behavior. Debug skips it to keep links fast and symbols complete.
- **`-O2`, not `-O3`.** `-O3` mostly trades size for speculative speedups; add a
  mode only with measurements.
- **Dependencies stay `-O2` in both modes.** Debugging libuv or mimalloc is not a
  Hexal user workflow, and identical dependency objects keep a future object
  cache (RFC 0164) simple.
- **CPU selection is mode-independent.** The current qualified Windows x64
  profile uses baseline `x86-64`; future profiles retain their own baseline in
  both modes rather than inheriting x86-specific flags from this RFC.
- **No release-only defensive flags** such as `-fwrapv` or
  `-fno-strict-aliasing`. They would hide generator defects that the C23 contract
  forbids; release conformance runs must catch those instead.

## Driver surface

```text
hexal build [-mode debug|release] [-root <dir>] [-entry <key>] [-out <path>]
```

- `-mode` defaults to `debug`. Any other value fails before compilation with
  `unknown build mode <value>; expected debug or release`.
- `driver.BuildOptions` gains `Mode`. The mode selects compile and link options
  in one table in the driver; `internal/backend` receives options and stays
  mode-unaware.
- The default output path is unchanged. Both modes write the same executable
  path; building one mode replaces the other's output.
- On Windows, debug derives one deterministic build identity, links in staging
  as `<stem>.<identity>.exe`, and therefore receives
  `<stem>.<identity>.pdb` from Zig. It publishes that immutable PDB first and
  atomically replaces the requested executable last; the executable is the
  commit point and retains the matching versioned PDB name internally.
- The identity is the lowercase full SHA-256 of a length-delimited stream
  containing the Hexal version, backend identity, target profile, mode, ordered
  generated artifact names and bytes, dependency identities, and exact compile
  and link options. It contains no staging or host-absolute path.
- An existing versioned PDB with the same identity must have identical bytes;
  disagreement is a build failure before executable publication.
- A failed build never changes the published executable or any PDB it names. A
  crash after PDB publication but before executable publication may leave one
  unreferenced immutable PDB, but cannot break the previous executable.
- Old versioned PDBs are retained in v1. Concurrent builds may still reference
  them, and deleting them safely requires an output lock or explicit clean
  lifecycle not added here. Release publishes no new PDB and does not delete
  retained debug artifacts.
- No mutable `main.pdb` convenience copy is emitted; it could disagree with the
  executable selected by a concurrent or interrupted build.
- Recorded `CommandResult` arguments show the exact mode options.
- `hexal play` (workbench) always builds debug.
- `hexal doctor` verifies that the backend accepts every option of both modes.

## Required sweep

- `cmd/hexal` usage text and flag parsing;
- `internal/driver` compile and link option assembly, immutable PDB publication,
  and versioned-PDB retention;
- `internal/driver` tests that assert exact backend arguments;
- `compiler/tests/c23validation`: add a release lane (see Validation); and
- the reference's C23 output contract, which currently states no optimization or
  mode expectations.

No existing code exists only because modes were absent, so nothing else is
removed.

## Implementation plan

### Phase 1: probe and record

1. Record exact `zig cc -###` expansions for both option sets and commit them as
   driver test data, as `internal/backend/testdata` already does.
2. Verify that `-fno-sanitize-recover=undefined` terminates a deliberate UB probe
   non-zero with a diagnostic under the pinned Zig, and record the native debug
   artifact shape for each qualified target; on Windows, verify that Zig's PDB
   path is stable.

### Phase 2: driver

1. Add `Mode` to `BuildOptions` and the option table.
2. Add `-mode` parsing, validation, and usage text.
3. Add the canonical build-identity encoder and SHA-256 calculation. Link a
   Windows debug build under the versioned basename in staging, verify or
   publish its immutable PDB at the final sibling path, then atomically publish
   the requested executable as the commit point.
4. Retain old versioned PDBs and emit no mutable convenience PDB. Release mode
   publishes only the executable.

### Phase 3: conformance

1. Run the tagged C23 fixture catalog and snippet catalog in both modes through
   the driver's exact options.
2. Measure executable size, link time, and representative runtime for both
   modes; record the results in `docs/benchmarks.md`.

### Phase 4: documentation

1. Update `docs/reference.md` only with explicit user approval: mode-independent
   semantics, the stack-overflow resource-limit exception, and the fact that
   generated C does not depend on mode.

## Validation

This list is exhaustive:

- `-mode debug`, `-mode release`, omitted mode, and an unknown value with its
  exact diagnostic;
- exact compile and link arguments for each mode, including dependencies
  unchanged;
- `Compile` output byte-identical for the same sources regardless of mode;
- every tagged C23 runtime fixture and every snippet produces identical stdout,
  stderr, and exit status in debug and release;
- a deliberate UB probe compiled with debug options terminates non-zero;
- floating output fixtures identical across modes with contraction disabled;
- release executables contain no debug info and are smaller than debug for the
  representative set; a program with unused helpers shows them removed;
- Windows debug publishes and internally names the exact versioned PDB derived
  from the canonical full build identity; equal identities require byte-equal
  PDBs, release publishes no PDB, unreferenced versioned PDBs may remain, and a
  failed or interrupted build preserves the previous executable/debug match;
  and
- `hexal doctor` reports a backend that rejects a mode option.

## Settled decisions

- **Default mode.** `debug`.
- **Release debug information.** None; release emits no PDB.
- **Output path.** Both modes publish to the same output path.
- **Mode count.** v1 has only `debug` and `release`; a size-oriented mode waits
  for measured demand.
- **Sanitizers.** No user-facing sanitizer mode is added. Sanitizer execution
  remains a conformance-harness concern.
- **Windows debug publication.** Use immutable, deterministic versioned PDBs;
  publish the PDB first and the executable last. Retain old PDBs in v1 rather
  than adding unsafe concurrent cleanup or an output-locking subsystem.
- **Release conformance gate.** Target qualification (RFC 0183 Track 6) runs the
  complete tagged fixture and snippet catalogs in both debug and release. Strict
  aliasing and other `-O2` transformations are where latent generator undefined
  behavior appears, so a debug-only pass does not qualify a target.

## Open questions

None.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation adds only
the mode-independence contract and the resource-limit exception after behavior
stabilizes, with explicit user approval.
