# RFC 0078: Compiler Package Boundary

- Kind: Architecture Decision Record (ADR)
- Status: Draft; implementation-ready
- Created: 2026-08-16
- Scope: Go package visibility for the compiler's stage packages
- Depends on: nothing. Independent of RFCs 0072–0079.
- Coordinates with: RFC 0074 (its R16 unexports individual symbols),
  `AGENTS.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, generated C, or the
  `compiler.Compile` contract

## Decision

Move `lexer`, `parser`, `checker`, `generator`, and `types` under
`compiler/internal/`. `compiler` remains the only importable package.

## Motivation

The five stage packages export **774 symbols** between them:

| Package | Exported |
|---|---|
| `checker` | 277 |
| `parser` | 176 |
| `generator` | 159 |
| `types` | 122 |
| `lexer` | 40 |

Nothing outside the module imports any of them. `workbench` uses
`compiler.Compile` and `compiler.CompilationResult`; the compiler's own tests
use the stage packages, and Go test files inside `internal/` keep that access.

The current arrangement means every stage symbol is, formally, public API. That
has two costs:

- **Unexporting decisions are advisory.** RFC 0074's R16 identifies exported
  symbols with no consumer and proposes unexporting them. Nothing prevents the
  next one from being added.
- **Refactoring is over-constrained.** A rename inside `checker` looks like a
  breaking change even though no consumer exists, so it gets treated with more
  caution than it warrants.

`internal/` makes the boundary a compiler-enforced fact. Go rejects an import of
`hexal/compiler/internal/checker` from outside `hexal/compiler`, so the intended
surface becomes the actual surface.

## Scope

```text
compiler/lexer/      → compiler/internal/lexer/
compiler/parser/     → compiler/internal/parser/
compiler/checker/    → compiler/internal/checker/
compiler/generator/  → compiler/internal/generator/
compiler/types/      → compiler/internal/types/
```

`compiler/compile.go` and `compiler/tests/` stay where they are.

Import paths update mechanically:

```go
import compilerTypes "hexal/compiler/types"
// becomes
import compilerTypes "hexal/compiler/internal/types"
```

Package names, file names, symbol names, and visibility are unchanged. This is a
directory move plus an import rewrite, nothing more.

## What this does not do

**It does not unexport anything.** All 774 symbols keep their current case. RFC
0074 R16 remains the spec that decides which should become unexported; this ADR
only removes the argument that unexporting is a breaking change.

Doing both at once would make an unreviewable diff — a directory move touching
every import, plus visibility changes touching every call site.

## Test access

`compiler/tests/integration` imports `hexal/compiler` only, so it is unaffected.

`compiler/tests/c23validation` likewise imports `hexal/compiler`.

In-package tests (`compiler/internal/checker/*_test.go`) move with their
packages and keep full access.

Cross-package tests inside the compiler — `generator_test.go` importing
`checker`, for example — continue to work: `internal/` restricts imports from
outside `hexal/compiler`, and both are inside it.

Verify this before starting. If any test outside `compiler/` imports a stage
package, it must move under `compiler/tests/` or switch to the public API first;
that is a prerequisite, not part of this change.

## Invariants

1. No symbol is renamed, unexported, added, or removed.
2. Generated C is byte-identical; the snippet manifest does not move.
3. `compiler.Compile` and `CompilationResult` are untouched.
4. `go build ./...`, `go test ./...`, and `go vet ./...` pass with no test
   modified except for its own import block.
5. `git diff --find-renames` shows file moves and import-line edits only.

## Validation

- `go test ./...`, `go vet ./...`,
  `go vet -tags c23 ./compiler/tests/c23validation`.
- The snippet manifest is unchanged.
- `grep -rn '"hexal/compiler/\(lexer\|parser\|checker\|generator\|types\)"'`
  returns nothing.
- No file outside `compiler/` imports an internal package — enforced by the Go
  toolchain after the move, so a successful build is the proof.
- The workbench builds and runs.

## Sequencing

Independent of every other open spec, but **land it when the tree is quiet**. It
rewrites the import block of ~150 files, so it conflicts textually with anything
in flight even though it conflicts logically with nothing.

Best executed as a single mechanical commit, immediately after another spec
closes rather than alongside one.

## Non-goals

- Unexporting symbols. RFC 0074 R16 owns that.
- Splitting or merging stage packages.
- Introducing a plugin, driver, or alternative front end that would need stage
  access. If one is ever wanted, the correct move is to widen `compiler`'s API
  deliberately, which this ADR makes a conscious act rather than an accident.
- Moving `compiler/tests/`.

## Drawbacks

- A large mechanical diff across ~150 files with no behavior change, which
  pollutes `git blame` on every import block. Land it as one commit and record
  it in `.git-blame-ignore-revs`.
- Import paths get longer.
- If a future consumer genuinely needs a stage package, this must be partly
  reversed. That is the intended cost: the reversal is a design decision made in
  the open, which is exactly what the current arrangement lets people skip.

## Expected result

- `compiler` is the compiler's only importable package, enforced by the
  toolchain.
- 774 stage symbols stop being public API without a single one being renamed.
- RFC 0074 R16's unexporting work becomes cleanup rather than an API change.
