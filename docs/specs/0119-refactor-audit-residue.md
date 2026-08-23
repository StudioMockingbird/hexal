# RFC 0119: Refactoring Audit Residue

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready
- Created: 2026-08-23
- Scope: the six deferred findings of RFC 0104 (R21, R22, R23, R24, R25,
  R47), carried forward with post-cleanup measurements. No language syntax,
  semantics, or builtin API changes.
- Depends on: RFC 0104's accepted set, which has landed through its stage 8.
- Coordinates with: `AGENTS.md`, `docs/reference.md`, `docs/status.md`

## Summary

RFC 0104 implemented its thirty-three accepted findings in eight stages and
closed with six deferred findings. This RFC owns them so none disappears
unowned, records the re-measurement 0104's handoff required, and sets the
disposition each finding needs before work starts. The tree moved since the
audit: render.go is now 1816 lines, validation.go 1699, types.go 1333, and
the generator contains no signature over six parameters.

## Findings

| # | Finding | Disposition |
| --- | --- | --- |
| **R21** | Kind/render/validate coverage matrix unproven | Accept |
| **R22** | render.go mixes five responsibilities | Decision required |
| **R23** | validation.go embeds a constant-validation cluster | Decision required |
| **R24** | types/types.go is the package everything-file | Decision required |
| **R25** | Checker `(names, typeEnvironment)` group recurs 20+ times | Accept |
| **R47** | Exported-API documentation gap | Accept |

### R21 - kind coverage matrix (Accept)

One table-driven unit test enumerating every checker expression Kind constant
against both generator switches' case lists (render.go and validation.go),
failing on any kind absent from either. Both dispatchers fail closed today, so
a missing case surfaces as a rejected valid program invisible to a suite that
asserts success or diagnostic text. The original deferral was timing only;
the matrix is cheap and protects every future dispatch change.

### R25 - checker context bundle (Accept)

Introduce one `checkContext` struct carrying scope, environment, and current
token; migrate `(names *scope, typeEnvironment *compilerTypes.Environment)`
signatures mechanically. Land before any R22-R24 split: the migration rewrites
every checker file the splits would move, so doing it first makes the split
sizes final.

### R47 - exported-API documentation (Accept)

Use the Go AST to inventory one package at a time; add only missing
CARE-quality contracts (Contract, Architecture, Rationale, or Edge fact).
No comment may cite an internal RFC number or path. Order packages by import
fan-out: `types`, then `checker`, then `generator`.

### R22-R24 - file splits (Decision required)

Post-cleanup measurements are essentially unchanged from audit time
(render.go 1801 -> 1816; validation.go 1701 -> 1699; types.go 1292 -> 1333),
so size alone does not newly justify a split. At the start of this RFC's
execution, decide per file, with the R25-migrated tree as the measured base:
split where a cohesive cluster exceeds ~1000 lines, otherwise record the drop
decision here. emission.go remains excluded as cohesive.

## Validation

- **R21** - the matrix test fails when a Kind constant is added to the
  checker but not handled by either generator switch; removing any case from
  either switch fails the same test.
- **R25** - no checker function outside scope.go takes both
  `(names *scope, typeEnvironment *compilerTypes.Environment)`; grep confirms
  the pair appears only inside `checkContext`.
- **R47** - every exported declaration under `compiler/` has a doc comment;
  a Go-AST-based check lists zero offenders; no comment cites internal RFC
  provenance.
- **R22-R24** - each file either has its split committed with the snippet
  manifest byte-identical, or this RFC records the drop decision with the
  measured size that justified it.
- Whole-RFC: `go test ./...`, `go vet ./...`, `go vet -tags c23 ./...`, and
  tracked-file `gofmt` stay green at every commit boundary; the snippet
  manifest never moves.

## Non-goals

- Reopening any RFC 0104 rejected finding without new evidence.
- Fusing discovery walks (RFC 0104 rejected R37 on measurement).
- Any change to generated C text beyond what R25's mechanical migration
  provably preserves byte-for-byte.
