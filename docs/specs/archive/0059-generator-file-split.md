# RFC 0059: Generator File Split

- Kind: Rust-Style RFC
- Status: Implemented; conformance verified 2026-08-15
- Created: 2026-08-15
- Updated: 2026-08-15
- Scope: `compiler/generator/generator.go` file organization only
- Depends on: RFC 0057 (generated-artifact baseline and local cleanup)
- Independent of: RFC 0058 (checker file split)
- Coordinates with: `AGENTS.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, generated C, checked
  representation, or any exported Go API

## Summary

Split `compiler/generator/generator.go` into five responsibility files using
complete declaration moves only. Preserve all algorithms, validation,
diagnostics, output ordering, and generated artifacts.

RFC 0057 deliberately excludes this work so its local cleanups and safety
baseline can be implemented and reviewed separately from this large mechanical
diff.

## Prerequisite

Complete RFC 0057 first. Its generated-artifact manifest is the required
behavioral baseline for this RFC and must pass before any declaration moves.

Record the post-0057 function and type inventory of `generator.go`. That
inventory, rather than pre-0057 line numbers, defines the migration set.

## Required invariants

1. Move declarations whole. Do not split or rewrite a function or type.
2. Preserve every declaration name, receiver, parameter, result, body, and
   comment, except a comment may be adjusted only when its file reference
   becomes false after the move.
3. Preserve diagnostics, categories, locations, ordering, and aggregation.
4. Preserve initialization, discovery, rendering, and output order.
5. Preserve every validation layer and validation test.
6. Do not introduce an abstraction, interface, context object, dependency, or
   new mutable package state.
7. Do not move symbols out of package `generator` or change their visibility.
8. Existing tests must pass without weakened assertions or removed cases.
9. Every RFC 0057 snippet artifact remains byte-identical to its recorded
   SHA-256 value.
10. The compiler remains string-in/string-out and performs no filesystem
    access.
11. `docs/reference.md` is reviewed and remains unchanged. A required semantic
    edit indicates that behavior changed and blocks this RFC.

## Required files and ownership

### `generator.go`

Retain only public generation entrypoints and top-level orchestration:

- exported generation entrypoints;
- orchestration that coordinates complete program/module generation;
- the minimum shared declarations required to make that entry flow readable.

It must not retain lower-level validation, statement/expression rendering, or
header assembly merely to satisfy an arbitrary placement preference.

### `emission.go`

Move module/program emission responsibilities:

- emission state;
- artifact discovery and state merging;
- module and main C/header pair emission;
- module and main header assembly;
- ordering and assembly helpers owned by those operations.

The header input structs introduced by RFC 0057 belong here with their
builders.

### `declarations.go`

Move C declaration, definition, prototype, and symbol-name emission:

- `writeFunctionDefinition` and `writeMethodDefinition`;
- `writeSpecializedPrototypes` and `writeSpecializedDefinitions`;
- `writeExportedPrototypes` and `writeForeignPrototypes`;
- `declaredFunctions` and `declaredMethods`;
- `parameterList`;
- `PrivateCName`, `validSourceName`, `isASCIILetter`, `moduleOwner`,
  `methodKey`, and `methodCName`.

This file owns declaration-level C emission and name mangling. `emission.go`
may call these operations while assembling an artifact, and `render.go` may
call lower-level C spelling helpers, but neither relationship changes this
ownership. Preserve `PrivateCName` as an exported symbol.

This file is unrelated to `compiler/checker/declarations.go` planned by RFC
0058, which owns Hexal declaration checking; the package boundary distinguishes
them.

### `validation.go`

Move the existing generator-side validation layer:

- checked-program validation;
- checked-statement validation;
- checked-expression and operand validation;
- constant and checked-metadata validation;
- generated-type validation.

Validation stays separate from rendering. Do not delete it, collapse it into
rendering, or weaken its fail-closed behavior.

### `render.go`

Move shared rendering responsibilities:

- statement and expression rendering dispatch;
- `writeStatements` and `writeStatementsAt`;
- shared C declaration spelling;
- shared rendering helpers that do not belong to an existing facet file;
- `renderReceiver` introduced by RFC 0057.

Also move `expressionValidation`, `generatedBinding`, and their general
scope/binding/name-resolution methods here. Despite its historical name,
`expressionValidation` is the shared state for expression and statement
rendering as well as generator validation; its binding scopes, generated
names, counters, captures, and hoisted-expression state belong with rendering.

Statement rendering belongs here, not in `generator.go` as top-level
orchestration.

A receiver type and all its methods need not occupy one file. General
scope/binding/name-resolution methods move with `expressionValidation` to
`render.go`; a feature-specific method remains in its existing facet file.
In particular, `captureOperand` remains in `defer.go`.

### Existing facet files

Existing generator facet files remain authoritative for their language
features. Do not move their declarations, duplicate their helpers, or use this
RFC to reorganize them.

### Shared-helper rule

Assign each private declaration to the file that owns its semantics. If no
single new file clearly owns it, retain it in `generator.go` and record it
during the post-split review. Do not create a `helpers.go` dumping ground.

## Move rules

- Preserve every function name and signature.
- Preserve function bodies byte-for-byte where `gofmt` permits.
- Locate declarations by symbol and switch branches by case-label name; source
  line numbers are not move instructions.
- Do not combine a simplification, rename, or behavior change with a move.
- Do not split a function merely to satisfy a file-size target.
- Do not normalize unrelated formatting or repository line endings.

## Size targets

- `generator.go` is under 1,500 lines.
- No new file exceeds 2,500 lines.
- These targets must be met through the five responsibility boundaries only.

If those boundaries cannot meet a target, stop and report the remaining
declarations. Do not invent another abstraction or an ambiguous sixth file.

## Required order

1. Complete RFC 0057 and pass its generated-artifact baseline.
2. Record the post-0057 declarations in `generator.go`.
3. Move C declaration/definition emission and name mangling; validate.
4. Move artifact-emission declarations and validate.
5. Move validation declarations and validate.
6. Move rendering declarations and shared rendering state; validate.
7. Review the remainder against `generator.go` ownership and validate.
8. Compare the final declaration inventory with the recorded baseline.

RFC 0058 may run before or after this RFC. Do not interleave their changes in
one review or implementation pass.

## Acceptance

- All five responsibility files exist and follow their ownership contracts.
- Every recorded declaration exists exactly once after the split.
- No declaration was renamed or had its signature changed.
- `generator.go` and all new files meet the size targets.
- All generator validation logic and tests remain present.
- No existing facet file or test file was reorganized incidentally.
- Generated artifacts match the RFC 0057 baseline byte-for-byte.
- `docs/reference.md` requires no change.

## Validation

After each move:

- `gofmt` only the touched generator files;
- `go test ./compiler/generator`;
- `go test ./compiler/tests/integration`;
- `go test ./...`;
- `go vet ./...`;
- run `workbench/snippets.TestCatalogProgramsCompile` and its generated-artifact
  manifest comparison;
- compare the current declaration inventory with the post-0057 inventory.

The touched-file restriction on `gofmt` is normative: package-wide formatting
would broaden this mechanical diff beyond the declarations and files moved by
this RFC.

At completion:

- `go test -tags c23 ./...`;
- `go vet -tags c23 ./...`;
- verify all dormant C23 canaries remain present and contain no runnable test
  entrypoint;
- verify `docs/reference.md` requires no change;
- rebuild the workbench into `bin/` and restart it;
- remove RFC 0059 from `docs/status.md`;
- mark this RFC `Implemented; conformance verified YYYY-MM-DD`.

No version-control operation or commit structure is required by this RFC.

## Non-goals

- Splitting or shortening individual functions.
- Changing generator algorithms or diagnostic construction.
- Deleting or weakening validation.
- Reorganizing existing generator facet files.
- Introducing a generator context object or dependency injection.
- Renaming private or exported declarations.
- Moving embedded C into separate files.
- Rewriting C declarator construction.
- Changing C helper-name or suffix derivation.
- Replacing operator/type switches with maps.
- Generic merge-helper work.
- Changing tests solely because declarations moved.
- Changing generated C, language behavior, the compiler API, or
  `docs/reference.md`.

## Drawbacks

- Total source lines remain essentially unchanged.
- The relocation creates a large mechanical diff.
- More files require maintainers to understand the responsibility boundaries;
  those boundaries are therefore normative for this refactor.

## Expected result

- `generator.go`: public entrypoints and top-level orchestration.
- `emission.go`: program/module discovery, state, pair emission, and headers.
- `declarations.go`: C definitions, prototypes, and name mangling.
- `validation.go`: generator-side fail-closed validation.
- `render.go`: common statement/expression rendering, C spelling, and shared
  rendering state.
- Existing facet files remain unchanged and authoritative.
