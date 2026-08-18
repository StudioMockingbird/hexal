# Task 1 Report: Literal Registry Contract Tests

## Status

Complete. Added the RFC 0077 registry contract tests and the two requested multi-module generation regressions. No registry implementation or other production code was added.

## Changes

- Created `compiler/generator/strings_test.go`:
  - `TestLiteralRegistryInternsPayloadOnce` covers duplicate interning, stable handles, `CName`, successful lookup, missing lookup, and one payload entry.
  - `TestLiteralRegistryPreservesRegistrationOrder` covers first-registration order and duplicate suppression in `All()`.
- Extended `compiler/tests/integration/modules_generation_test.go`:
  - `TestModuleGenerationLiteralOrder` covers dependency-first module discovery, shared literal deduplication, exact literal object definitions, stable indices, and root/imported module references.
  - `TestModuleGenerationConcurrencyLiteralHandles` covers a root literal plus non-root `spawn`/recoverable scheduler generation and asserts the program-wide source filename, Scheduler, task-failure, and root literal objects/indices.
- Existing unrelated RFC 0078 working-tree changes were not edited or staged.

## Verification

- `gofmt -w compiler/generator/strings_test.go compiler/tests/integration/modules_generation_test.go`: passed.
- `git diff --check`: passed.
- `go test ./compiler/generator -run TestLiteralRegistry -count=1`: expected RED; package build fails because `newLiteralRegistry` and `literalHandle` do not exist yet.
- `go test ./compiler/tests/integration -run 'TestModuleGeneration(Literal|Concurrency)' -count=1`: passed (`ok`, including both new regressions and the existing matching concurrency test).
- Post-commit status confirms only the pre-existing RFC 0078/spec-worktree files remain outside the commit.

The integration regressions pass against the current payload-based merge/rebase implementation; the registry unit tests provide the intentional RED evidence required before Task 2 implements the API.

## Commit

`d783fa2 test: pin literal registry contract`

The commit contains only:

- `compiler/generator/strings_test.go`
- `compiler/tests/integration/modules_generation_test.go`

## Concerns

- The generator unit package remains intentionally uncompilable until the ordered RFC 0077 implementation task adds `literalRegistry`, `literalHandle`, and `newLiteralRegistry`.
- No unrelated dirty files were included.

## Fix Round 1 Report

### Changes

- Strengthened `TestModuleGenerationLiteralOrder` with exact generated uses for the imported literal, the non-root shared literal, the root literal, and the root module's shared literal. Both modules now have to use the shared payload through the same `hex_lit_1` object.
- Changed `TestModuleGenerationConcurrencyLiteralHandles` to include an earlier reachable `root.hex` literal that is repeated by the entrypoint. The non-root `math` module therefore has shifted program-wide handles: `main.hex` is `hex_lit_1`, `Scheduler` is `hex_lit_2`, and `task creation failed` is `hex_lit_3`.
- Added exact `math.h` coverage for the shifted source filename and generated Scheduler header initializer, plus exact root-literal references in both `modules/root.c` and `modules/app.c`.
- No production code or RFC 0078 files were edited.

### Tests and commands

- `gofmt -w compiler/tests/integration/modules_generation_test.go`: passed.
- `gofmt -l compiler/tests/integration/modules_generation_test.go compiler/generator/strings_test.go`: no output.
- `git diff --check`: passed.
- `go test ./compiler/tests/integration -run 'TestModuleGeneration(Literal|Concurrency)' -count=1 -v`: passed; 3 tests passed (`TestModuleGenerationConcurrencyOwnedByDefiningModule`, `TestModuleGenerationLiteralOrder`, and `TestModuleGenerationConcurrencyLiteralHandles`).
- `go test ./compiler/generator -run TestLiteralRegistry -count=1`: expected RED; package build failed because `newLiteralRegistry` and `literalHandle` are not implemented by Task 1. The command reported `exit status: 1` with the three expected undefined-name diagnostics.
- Primary LSP diagnostics for `compiler/tests/integration/modules_generation_test.go`: clean, 0 diagnostics.
- Session lens diagnostics for the edited test files: no blocking diagnostics; the remaining Go test naming warning is pre-existing/expected.

### Self-review

- Finding 1: `math.h` is checked for the exact Scheduler initializer instead of only checking that `hexal/string.c` contains a Scheduler object.
- Finding 2: the fixture now places a deduplicated root payload before the concurrency module in dependency order, forcing all non-root generated handles to be offset; source filename, Scheduler payload, task-failure message, and root-module uses are checked at exact indices.
- Finding 3: the shared payload is asserted at `hex_lit_1` in both `math.c` and `app.c`, while the distinct imported and root payloads are asserted at their own exact uses.
- The focused integration suite passes against the current payload/rebase implementation; the registry unit suite remains intentionally red until the separate Task 2 production implementation. The test changes therefore preserve the required TDD boundary without adding generator code.
