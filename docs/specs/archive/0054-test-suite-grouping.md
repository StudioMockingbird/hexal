# ADR 0054: Integration and Dormant C23 Test Packages

- Kind: Architecture Decision Record (ADR)
- Status: Implemented; conformance verified 2026-08-14
- Created: 2026-08-14
- Scope: Go test organization only
- Depends on: ADR 0053 (compiler test layout, implemented)
- Supersedes: ADR 0053's flat-package decision, only for the two-package split
  below; every other ADR 0053 rule remains in force
- Coordinates with: `AGENTS.md`, `docs/status.md`

## Context

ADR 0053 moved every direct-root compiler test into `compiler/tests/` as one
`package tests`. The verified current inventory is:

- 54 `_test.go` files total;
- 44 ordinary files:
  - 37 active full-pipeline facet files;
  - `helpers_test.go`; and
  - 6 `modules_*_test.go` files (RFC 0034 module tests); and
  - 467 runnable `Test...` functions;
- 10 `c23_*_test.go` files:
  - 9 dormant validation files;
  - `c23_harness_test.go`;
  - 469 lines total; and
  - zero runnable `Test...` functions in every tag mode.

The C23 files are excluded from ordinary builds by `//go:build c23`. A tagged
run type-checks them but executes no validation and launches no external
toolchain. They are retained deliberately as dormant, compile-only canaries;
they are not an active test suite and provide no C23 conformance evidence.

The populations are not independent today. The dormant C23 functions call
`assertCompiles` from `helpers_test.go` 30 times. The split therefore requires a
private C23-package copy of that small assertion.

## Decision

- Create two packages under `compiler/tests/`:
  - `compiler/tests/integration/`, `package integration`, containing the 37
    active facet files, `helpers_test.go`, and the 6 `modules_*_test.go`
    files; and
  - `compiler/tests/c23validation/`, `package c23validation`, containing the 9
    dormant validation files plus `c23_harness_test.go`.
- Preserve every filename; do not rename `helpers_test.go`.
- Change moved package declarations from `tests` to the destination package
  name.
- Keep `//go:build c23` on all 10 C23 files.
- Add a private `assertCompiles` helper to `c23_harness_test.go`. It calls
  `compiler.Compile` directly and preserves the existing assertion behavior.
- Keep every C23 validation function non-runnable; no function may begin with
  `Test`.
- Do not create or export a production test-support package.
- Do not create another test package in this change.

## Package contracts

### `integration`

- Contains active public, full-pipeline compiler tests.
- Imports `hexal/compiler` and uses only its exported API.
- Keeps one compiler-behavior file per language facet.
- `helpers_test.go` retains shared assertions and source/output utilities; it
  does not alias the compiler API.

### `c23validation`

- Contains dormant external-toolchain validation code only.
- Is excluded unless the `c23` build tag is selected.
- Contains zero runnable `Test...` functions.
- `go test -tags c23` and `go vet -tags c23` type-check the package but execute
  no validation and launch no process.
- The package must remain dormant until a later specification explicitly
  restores external-toolchain execution.
- Restoring entrypoints requires a new decision covering toolchain policy,
  harness ownership, supported compilers, and C23 conformance expectations.

## File movement

- Move the 44 current ordinary files into `compiler/tests/integration/`:
  - 37 facet files;
  - `helpers_test.go`; and
  - 6 `modules_*_test.go` files.
- Move the 10 current C23 files into `compiler/tests/c23validation/`:
  - 9 validation files; and
  - `c23_harness_test.go`.
- Preserve every function, test case, assertion, build tag, and source fixture,
  except for adding the required private C23 `assertCompiles` helper.
- After migration, no `*_test.go` file remains directly under
  `compiler/tests/`.

## Naming and ownership

- Never name a test file or test function after this ADR; cite specifications
  in header comments.
- Keep the `c23_` prefix on dormant C23 files.
- Do not create a directory per language facet. A Go directory is a package
  boundary, not a visual grouping mechanism.
- A new test package is justified only by a distinct execution lifecycle,
  dependency boundary, or toolchain requirement.

## Documentation changes

- Update `AGENTS.md` to state:
  - active tests live in `compiler/tests/integration/`;
  - dormant compile-only C23 canaries live in
    `compiler/tests/c23validation/`;
  - ordinary tests never invoke an external tool;
  - tagged C23 runs compile but execute no tests or external processes;
  - `go test ./compiler` does not run the full-pipeline suite; use
    `go test ./...` or target `./compiler/tests/integration`; and
  - future test packages require a genuinely distinct lifecycle or dependency
    boundary.
- Update `docs/status.md` paths that name retained C23 files.
- Do not change `docs/reference.md`: this ADR changes neither Hexal syntax nor
  semantics.

## Non-goals

- Activating, deleting, merging, splitting, or rewriting C23 validations.
- Changing active test expectations, diagnostics, generated C, or language
  behavior.
- Changing compiler architecture or exported APIs.
- Creating a per-facet package.
- Creating a shared production test-support package.
- Adding fuzz, benchmark, stress, or external-toolchain entrypoints.

## Deferred test packages

- `compiler/tests/fuzz/`: add when Go fuzz targets are designed. Its corpus,
  seed ownership, time bounds, and failure-promotion rules require a separate
  specification.
- `compiler/tests/benchmarks/`: add only when the project defines stable
  workloads and a regression-reporting policy. A benchmark without a tracked
  decision threshold is informational, not a gate.
- `compiler/tests/stress/`: add only if long-running, race-enabled, repeated, or
  high-volume checks become necessary and cannot run in the ordinary suite.
- Active C23 conformance remains in `c23validation/` if restored; do not create
  a second external-toolchain package for the same lifecycle.

Regression tests, negative tests, module tests, generated-C inspection, and
golden cases do not justify packages by themselves. They remain facet files or
`testdata/` under `integration`, or stage tests beside the implementation.

## Validation

- `go test ./...` passes without an external toolchain.
- `compiler/tests/integration/` contains exactly 44 files: 37 facet files,
  `helpers_test.go`, and 6 `modules_*_test.go` files.
- `compiler/tests/c23validation/` contains exactly 10 files: 9 validation files
  and `c23_harness_test.go`.
- No `*_test.go` file remains directly under `compiler/tests/`.
- The same 467 active `Test...` function names exist after the move.
- All dormant C23 function names remain and none begins with `Test`.
- `go test -tags c23 ./...` passes and executes no C23 tests or external
  processes.
- `go vet -tags c23 ./...` passes.
- Stage test locations and package declarations are unchanged.
- No production file changes.
- `git diff --find-renames` contains only file moves, package declarations, the
  private C23 helper, `AGENTS.md`, `docs/status.md`, and ADR lifecycle changes.

## Consequences

- Active tests and dormant compile-only canaries have explicit package
  boundaries.
- The C23 package duplicates one small compile assertion to remain independent.
- Tagged compilation detects Go-level drift in dormant C23 code but provides no
  C23 conformance evidence.
- Future fuzz, benchmark, or stress packages are added only when their distinct
  execution contracts are specified.

