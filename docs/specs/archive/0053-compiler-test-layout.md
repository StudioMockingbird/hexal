# ADR 0053: Compiler Test Layout

- Kind: Architecture Decision Record (ADR)
- Status: Implemented
- Created: 2026-08-14
- Scope: Go test organization only
- Depends on: RFC 0048 (test helpers and harness, implemented)
- Coordinates with: `AGENTS.md`

## Decision

- Move every `compiler/*_test.go` file directly under `compiler/` into
  `compiler/tests/`.
- Preserve each filename, test function, test case, assertion, build tag, and
  source fixture.
- Use `package tests` for every file in `compiler/tests/`.
- Import `hexal/compiler` normally and qualify every compiler API use with
  `compiler.`; do not use a dot import, wrapper, or alias to hide the package
  boundary.
- `compiler/tests/` contains public, full-pipeline compiler tests:
  - ordinary tests call the compiler from Hexal source through generated C;
  - `c23_*_test.go` tests additionally compile or run generated C; and
  - shared assertions and the C23 harness remain test-only files in this
    package.
- Stage tests remain beside their implementations:
  - `compiler/lexer/`;
  - `compiler/parser/`;
  - `compiler/checker/`;
  - `compiler/types/`; and
  - `compiler/generator/`.
- Do not create a subdirectory per language feature. A Go directory is a
  package boundary, not a visual grouping mechanism.
- The idiomatic same-directory `package compiler_test` alternative is rejected:
  it would enforce black-box access but leave all full-pipeline tests mixed
  with production files, which is the layout this ADR changes.

## Package boundary

- Tests in `compiler/tests/` must import `hexal/compiler` and may use only its
  exported API.
- Existing direct calls and type references are changed mechanically:
  - `Compile(...)` becomes `compiler.Compile(...)`;
  - `CompilationResult` becomes `compiler.CompilationResult`;
  - `ExitSuccess` becomes `compiler.ExitSuccess`; and
  - `ExitFailure` becomes `compiler.ExitFailure`.
- `helpers_test.go` retains only shared assertions and source/output utilities;
  it must not wrap or alias the compiler API.
- No production API may be exported or changed solely to support this move.
- Do not add a production `testutil` package.

## File movement

- The migration set is determined at implementation time by the direct-child
  glob `compiler/*_test.go`.
- Record that complete set before moving any file; the recorded set, rather
  than a hard-coded count, is the migration and validation baseline.
- The set includes:
  - `helpers_test.go`;
  - all language-facet integration tests; and
  - all `c23_*_test.go` files.
- Files already below compiler stage directories are outside the migration
  set.
- `compile.go` and all other production files remain in place.
- After migration, no `*_test.go` file may remain directly under `compiler/`.

## C23 suite

- Preserve `//go:build c23` on the C23 harness and every C23 test.
- Preserve the existing rule that ordinary `go test ./...` requires no C
  toolchain.
- An explicitly requested C23 run must still fail when GCC is unavailable; it
  must not skip.
- C23 tests continue sharing `c23_harness_test.go`; do not introduce a second
  harness.

## Naming and ownership

- Keep one public compiler-behavior test file per language facet.
- Keep existing facet filenames unless two files demonstrably test the same
  contract; deduplication is outside this ADR.
- Keep the `c23_` prefix for tests that invoke the external C23 toolchain.
- Never name a test file or test function after this ADR.
- Spec citations remain header comments rather than test names.

## Documentation changes

- Update `AGENTS.md` so its Testing section states:
  - full-pipeline integration tests live in `compiler/tests/`;
  - the package imports `hexal/compiler` and tests only its exported behavior;
  - stage unit tests remain beside their implementations; and
  - `go test ./compiler` does not run the full-pipeline suite; use
    `go test ./...` or target `./compiler/tests`;
  - the existing facet naming, C23 build-tag, and overlap rules remain in
    force.
- Do not change `docs/reference.md`: this ADR changes neither Hexal syntax nor
  semantics.
- Do not change grammar documentation or language status solely for this move.

## Non-goals

- Adding, deleting, merging, splitting, or rewriting tests.
- Changing test expectations, diagnostics, generated C, or language behavior.
- Changing compiler architecture or exported APIs.
- Separating C23 tests into another Go package.
- Introducing golden files, fixtures, snapshots, or a new test framework.
- Reorganizing stage unit tests.

## Consequences

- The full-pipeline suite is structurally restricted to the public compiler
  API; its current lack of private-identifier use becomes an enforced package
  boundary rather than a convention.
- `compiler/` presents production entry-point files without dozens of
  full-pipeline test files mixed into the listing.
- Stage tests remain easy to find next to the code they isolate.
- The additional `compiler/tests` package is intentional and has no production
  build output because it contains only test files.
- `go test ./compiler` no longer runs the full-pipeline suite and still exits
  successfully. Developers and CI must use `go test ./...` or target
  `./compiler/tests` explicitly.
