# RFC 0215: Single-Clang External Validation

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; blocked on RFC 0214
- Created: 2026-09-17
- Scope: remove GCC and Zig as compiler-development and external-test
  dependencies after Clang becomes Hexal's sole qualified backend
- Depends on: RFC 0214's installed-Clang WSL backend and Linux target
- Does not change: Hexal syntax, semantics, generated C, runtime behavior,
  fixture coverage, snippet coverage, ordinary Go tests, or the core compiler
  boundary

## Motivation

The external C23 harness currently requires GCC, Clang, and Zig and runs nearly
every generated program under all three. That matrix was useful while Hexal
claimed compiler portability and used Zig as its production backend. RFC 0214
instead supports only Clang.

GCC and Zig now provide only speculative portability evidence while imposing:

- two additional installations on every compiler-development environment;
- approximately three builds per generated artifact instead of one;
- compiler-specific discovery, version parsing, flags, caches, diagnostics,
  warning workarounds, sanitizer handling, and comments; and
- Zig-specific mode and driver fixtures for a backend being removed.

No language fixture fundamentally needs GCC or Zig. Compile, run, trap,
concurrency, foreign-object, mode, runtime-pack, and sanitizer behavior can all
be validated against the one supported backend.

## Current dependency inventory

### Generic C23 harness

- `toolchain_test.go` defines required GCC, Clang, and Zig specifications,
  discovery, version checks, environment overrides, and one cached result for
  each.
- `compileGeneratedC`, `runGeneratedC`, and `trapGeneratedC` iterate over all
  three compilers.
- runtime-output comparison rejects disagreement between compilers.
- the entire workbench snippet catalog compiles through that same matrix.

### Compiler-specific branches

- `dependencies_test.go` carries a GCC-only incompatible-pointer warning rule.
- `ubsan_test.go` probes all three, skips unsupported GCC, and recognizes a
  Zig-specific UBSan marker and reporting path.
- `modes_test.go` explicitly uses Zig for debug/release comparisons.

### Driver and CLI

RFC 0214 owns replacement of production Zig commands and driver/CLI fixtures
with Clang/Linux equivalents. This RFC removes the remaining expectation that
GCC or Zig must be installed merely to run external validation.

### Documentation

`docs/status.md` contains coverage descriptions that name Zig execution,
Clang/Zig UBSan, and GCC's unavailable sanitizer runtime. Those statements
become stale when this RFC lands.

## Decision

Clang 18 or newer is the sole required external tool for compiler development,
tagged generated-C validation, driver qualification, and release gates.

The suite retains its three behavioral tiers:

1. compile every applicable fixture with strict warnings;
2. run successful fixtures and compare exact output; and
3. run failing fixtures and verify their runtime diagnostic.

The complete workbench snippet catalog still receives compile coverage. The
full runnable fixture set still receives exact behavior coverage. Only the
redundant compiler dimension is removed.

No dormant, optional, best-effort, environment-gated, or CI-only GCC/Zig lane
is retained. Supporting another compiler later requires a new qualification
spec with demonstrated product value.

## Clang selection

The tagged suite retains one developer-facing override:

```text
HEXAL_CLANG=/absolute/path/to/clang
```

Without the override it may discover `clang-<version>` or `clang` using the
existing test-only search policy. This does not weaken RFC 0214's production
rule: `hexal build` and `hexal doctor` still receive an explicit `-cc` path and
never search PATH.

The test resolver:

- accepts only Clang 18 or newer;
- resolves and version-checks it once per test binary;
- exposes one command prefix and identity to the harness; and
- reports one exact actionable failure when it is absent or too old.

## Sanitizers

`TestC23SuiteUBSan` uses Clang only.

- Remove GCC capability probing and skip bookkeeping.
- Remove Zig's `ubsan_rt.zig` marker and Zig-specific stderr interpretation.
- Retain Clang's compiler-rt log handling, ignorelist, fixture expectations,
  and `-fno-sanitize-recover=all` policy.
- A tagged release-gate run fails if the selected supported Clang cannot link
  and execute the required UBSan probe. It does not silently fall back to
  another compiler.

ASan and fiber annotations remain outside this RFC's scope. TSan remains
outside the supported lifecycle.

## Required sweep

Implementation removes or rewrites:

- `gccSpec`, `zigSpec`, `cachedGCC`, `cachedZig`, and
  `discoverAllToolchains`;
- multi-toolchain loops and output-divergence state in the compile/run/trap
  harness;
- GCC-only dependency warning handling;
- Zig-only mode-runner selection;
- GCC/Zig UBSan capability, marker, and reporting paths;
- comments claiming every fixture runs under three independent toolchains;
- test environment variables and path candidates for GCC and Zig;
- active testdata whose only purpose is a Zig command expansion; and
- current `docs/status.md` wording that describes GCC or Zig as part of the
  active validation gate.

Do not delete legitimate generated-C logic merely because it is portable to
GCC or Zig. This is a test/toolchain dependency cleanup, not permission to emit
Clang extensions or weaken the C23 contract.

## Implementation plan

### Phase 1: simplify toolchain resolution

1. Replace the three-spec registry and `discoverAllToolchains` with one
   Clang-specific resolver and cached result.
2. Retain `HEXAL_CLANG`, Clang versioned-name discovery, timeout handling, and
   exact version diagnostics.
3. Remove GCC/Zig environment variables, path candidates, version patterns,
   command prefixes, and cache fields.

### Phase 2: collapse the tiered harness

1. Make compile, run, and trap helpers build once with the resolved Clang.
2. Remove per-toolchain subtests and output-divergence accumulation.
3. Preserve strict C23 flags, warning policy, process timeouts, exact stdout,
   exact trap substrings, dependency demand, compile caching, and deterministic
   artifact handling.
4. Run every existing applicable fixture and every catalog snippet; delete no
   fixture or expectation.

### Phase 3: migrate modes and sanitizers

1. Run every debug/release comparison with Clang.
2. Replace Zig-specific expansion baselines with Clang/Linux baselines where
   RFC 0214 has not already done so.
3. Make UBSan Clang-only and remove alternate capability/report paths.
4. Remove the GCC-only warning branch and verify the shared strict-warning set
   is sufficient for Clang.

### Phase 4: driver coordination and cleanup

1. Confirm RFC 0214 has migrated driver, CLI, foreign-input, target, doctor,
   and runtime-pack tests from Zig/Windows to Clang/Linux.
2. Search active Go source, testdata, and comments for GCC/Zig dependencies;
   retain a mention only when it describes deliberately deferred support or a
   source-level portability fact independent of test execution.
3. Rewrite affected `docs/status.md` coverage descriptions around the sole
   Clang gate.
4. Confirm ordinary tests remain pure Go and toolchain-independent.

### Phase 5: conformance

1. Run ordinary tests and vet without any external compiler requirement.
2. Run the complete tagged fixture suite with only Clang installed.
3. Run the complete snippet catalog with only Clang installed.
4. Run debug/release, UBSan, driver, foreign-object, automatic-header-import,
   runtime-pack, and doctor qualification with Clang.
5. Confirm the generated-C manifest is byte-identical; this RFC changes test
   execution, not compiler output.
6. Verify `docs/reference.md` needs no edit because language and generated-C
   contracts did not change.

## Validation (exhaustive)

- Ordinary `go test ./...` and `go vet ./...` require no external toolchain.
- The tagged C23 suite requires Clang 18 or newer and does not discover, invoke,
  mention as required, or condition behavior on GCC or Zig.
- A machine with Clang installed and no GCC or Zig runs every tagged release
  gate successfully.
- A missing or old Clang produces one exact actionable toolchain diagnostic.
- Every pre-existing applicable fixture still compiles once under strict C23
  warnings.
- Every pre-existing successful runtime expectation still produces exact
  stdout and empty stderr.
- Every pre-existing trap expectation still exits unsuccessfully with its
  required runtime diagnostic.
- Every workbench snippet still compiles under the external C23 harness.
- Debug/release behavior, size/debug-information checks, floating output, and
  cache separation use Clang.
- UBSan runs the complete runnable set under Clang; GCC/Zig capability probes,
  skips, markers, and special handling do not remain.
- GCC-only warning handling and three-toolchain output-divergence machinery do
  not remain.
- RFC 0214's driver, CLI, C import, foreign input, target qualification,
  runtime-pack, and doctor checks all use installed Clang on WSL/Linux.
- No fixture, runtime expectation, sanitizer category, strict warning, process
  timeout, or catalog entry is removed to accomplish the cleanup.
- Generated C and the snippet manifest do not change.
- `docs/status.md` describes the actual Clang-only gate and contains no stale
  claim that GCC or Zig participates in active validation.
- `docs/reference.md` is verified unchanged.

## Consequences

- Compiler contributors install one external compiler instead of three.
- External validation performs one compilation per artifact instead of three.
- Compiler-specific discovery and sanitizer code becomes materially smaller.
- Hexal stops claiming evidence for unsupported GCC and Zig backends.
- A future second backend must justify and own its own implementation and test
  cost instead of inheriting dormant infrastructure.
