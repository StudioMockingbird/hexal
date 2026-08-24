# RFC 0119: Refactoring Audit Residue

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented 2026-08-24. R21 added
  `TestExpressionDispatchersCoverEveryConcreteKind` in
  `compiler/generator/expression_dispatch_coverage_test.go`, an AST-based test
  comparing `ExpressionKind` constants against the case labels of
  `renderExpressionUncheckedWithState` and `validateExpressionNode`. The test
  found a genuine, pre-existing gap: `renderExpressionUncheckedWithState` had
  no case for `PrintExpression` (validation.go already had one), silently
  relying on the shared fail-closed default; a `case checker.PrintExpression:`
  was added matching the neighboring `MatchExpression` case's
  "lowers at statement level" shape, since a print call's zero result type
  makes it unreachable there in any valid or currently-diagnosed program (no
  test's diagnostic text changed). R22-R24 were re-measured and confirmed
  Drop with no code move. R25 added `checkContext{names, typeEnvironment}` in
  `compiler/checker/expressions.go` and mechanically migrated 108 checker
  functions from separate `names *scope, typeEnvironment
  *compilerTypes.Environment` parameters to one `ctx checkContext`, via a
  temporary `.tmp/` codemod (two-phase: rewrite each migrated function's own
  signature and body, then collapse call-site `ctx.names, ctx.typeEnvironment`
  argument pairs into `ctx` wherever the callee was itself migrated) plus
  hand-fixed exceptions the codemod could not safely automate: one
  pre-existing local variable shadow (`adt.go`'s `names` renamed to
  `remainingNames`), calls passing a derived child or closure-root scope
  instead of the bundle's own `names` (`checkContext{names: child, ...}`
  wherever a callee needed a scope value that was not simply the caller's
  own), `checkModule`'s own 19 call sites (which pass its locally named
  `environment`/`typeEnvironment`, not `names`), and one function whose
  scope/environment parameters were both already blank `_` identifiers
  (`checkRuneCursorMethodCall`). The parameter is named `ctx`, not `context`,
  because 8 of the migrated functions already had an unrelated `context
  expressionContext` parameter. `bindParametersAndCheckBody` was confirmed as
  the one deliberate exception: it takes two distinct `*scope` values
  (`enclosing`, `body`), not the bundleable pair. R47 inventoried every
  exported production Go declaration under `compiler/` via `go/ast` (a
  temporary `.tmp/` script), found 168 missing doc comments concentrated in
  `compiler/lexer/lexer.go`'s `TokenKind` block, `compiler/checker/operands.go`'s
  `OperandKind`/`Operator`/`ExpressionKind`/`LiteralRadix` blocks,
  `compiler/types/types.go`'s `ScalarKind`/`ErrorCategory`/`TruthinessKind`
  blocks and two `Error()` methods, `compiler/types/collections.go`'s
  `Position` block, `compiler/types/io.go`'s `StreamCapability` block,
  `compiler/types/unions.go`'s six `TypeUse` constructors and two
  `UnionMember` functions, and three `compiler/checker/checker.go` statement
  types; closed every one, mostly via one CARE-compliant group comment per
  constant block per the RFC's own grouped-constant allowance, down to zero
  missing.
- Created: 2026-08-23
- Updated: 2026-08-24
- Scope: the six findings deferred by RFC 0104: R21-R25 and R47
- Depends on: implemented RFC 0104
- Coordinates with: `AGENTS.md`, `docs/reference.md`, and `docs/status.md`
- Changes no Hexal syntax, semantics, builtin API, checked representation, or
  generated C

## Summary

Execute three remaining high-value refactors:

- R21: prove checked-expression dispatch coverage.
- R25: bundle the checker scope and type environment passed together across
  expression checking.
- R47: document exported Go declarations under `compiler/`.

Drop R22-R24. Re-measurement found no cohesive responsibility exceeding the
split threshold, so moving code would create files without materially reducing
architectural complexity.

## Verified baseline

Measurements on 2026-08-24:

| File | Lines | Disposition |
|---|---:|---|
| `compiler/generator/render.go` | 1,868 | Keep cohesive |
| `compiler/generator/validation.go` | 1,730 | Keep cohesive |
| `compiler/types/types.go` | 1,333 | Keep cohesive |

The line counts are evidence, not permanent acceptance conditions. Stage 0
re-measures them because active compiler work may move the tree.

## Dispositions

| Finding | Decision |
|---|---|
| R21 - checked-kind/render/validate coverage | Accept |
| R22 - split `render.go` | Drop |
| R23 - split constant validation from `validation.go` | Drop |
| R24 - split `types/types.go` | Drop |
| R25 - checker context bundle | Accept, scope and environment only |
| R47 - exported Go API documentation | Accept |

## R21: checked-expression dispatch coverage

`checker.ExpressionKind` is a closed checked language. The generator's primary
render and validation switches must explicitly own every concrete kind. Both
dispatchers fail closed, but a missing case otherwise appears only when a valid
program reaches it.

Add one generator unit test that uses the Go AST to compare:

- the concrete `ExpressionKind` constants in `compiler/checker/operands.go`;
- case labels in `renderExpressionUncheckedWithState`; and
- case labels in `validateExpressionNode`.

Rules:

- Every concrete kind except `InvalidExpression` appears in both dispatch
  switches.
- `InvalidExpression` remains intentionally unsupported and reaches the
  fail-closed diagnostic path.
- Grouped case clauses count each named kind separately.
- The test reads declarations and case labels, not source line numbers or
  comments.
- The test does not maintain a second handwritten kind list.
- Adding a kind without both owners fails the test.
- Removing either owner for an existing kind fails the test.

The test is structural by design: malformed metadata cannot be used to infer
dispatch coverage reliably because a correctly owned case may reject before
rendering.

## R22-R24: no file splits

### `render.go`

The file contains statement rendering, expression rendering, C declaration
spelling, binding state, and literal formatting. The largest cohesive cluster
is expression rendering and remains below the approximately 1,000-line split
threshold. All clusters serve one generator phase and share
`expressionValidation`. Splitting them now would increase navigation across
files without producing a new abstraction or package boundary.

### `validation.go`

The constant-validation cluster is self-contained but small relative to the
file. Extracting it would move code without simplifying the validation entry
path, state, or dispatch. Keep it until it grows or gains an independent
consumer.

### `types/types.go`

Diagnostics, environment identity, canonicality, predicates, and builtin
types form the package's central type model. At 1,333 lines, no measured
subsystem justifies a split solely for size. Reconsider only when an owner can
be named independently of `Type` and `Environment`.

These are explicit drop decisions, not deferred work. A future split requires
new evidence and a new spec.

## R25: checker context bundle

Introduce:

```go
type checkContext struct {
    names           *scope
    typeEnvironment *compilerTypes.Environment
}
```

Contracts:

- The struct is checker-internal and passed by value.
- It carries only the stable pair used throughout expression checking.
- Lexer tokens, expected types, generic tables, flow facts, and other
  operation-specific values remain explicit parameters.
- In particular, there is no `current token` field. Different diagnostics in
  one operation commonly belong to different tokens; storing one in shared
  context would obscure earliest diagnostic ownership.
- The migration changes signatures and field access only.
- Scope creation, environment ownership, diagnostic locations, checking order,
  and checked output are byte-for-byte behaviorally unchanged.
- Functions requiring only one member keep that direct parameter. Do not pass
  the bundle merely to extract one value.
- `scope.go` methods continue receiving their own receiver and are outside the
  paired-signature sweep.

## R47: exported Go API documentation

Use the Go AST to inventory every exported production declaration under
`compiler/`, excluding `_test.go` files. Process one Go package at a time.

For every missing declaration comment:

- add a concise CARE comment; this RFC does not change visibility;
- state a Contract, Architecture, Rationale, or Edge fact not already obvious
  from the declaration;
- begin Go doc comments with the declared identifier where Go convention
  requires it;
- keep comments ASCII and free of RFC, ADR, plan, or spec provenance;
- document grouped constants once when one group contract covers them; and
- record apparently unnecessary exports as future audit candidates rather than
  changing their visibility here.

This is Go API documentation. It does not copy language rules out of
`docs/reference.md` and does not add implementation commentary to generated C
templates.

## Invariants

1. No Hexal source program changes acceptance, diagnostics, checked output, or
   generated artifacts.
2. No parser, checker, or generator dispatch case is added or removed except
   test-only mutation used to prove R21 locally before the final change.
3. R25 preserves argument evaluation and checking order.
4. R47 changes comments only; it does not alter Go visibility or behavior.
5. The snippet manifest remains byte-identical in every stage.
6. `docs/reference.md` requires no semantic edit. Implementation explicitly
   verifies that fact before closure.

## Implementation plan

### Stage 0: refresh and baseline

1. Record `go test ./...`, `go vet ./...`, `go vet -tags c23 ./...`, tracked
   `gofmt`, and snippet-manifest status.
2. Re-measure the three candidate files and record their function inventories.
3. Confirm no newly cohesive cluster exceeds approximately 1,000 lines. A
   mismatch stops implementation and reopens only the affected split decision.
4. AST-inventory concrete expression kinds and exported production
   declarations before editing.

### Stage 1: R21 coverage test

1. Add a generator-package unit test using `go/parser` and `go/ast`.
2. Resolve repository-relative source paths from the test package directory;
   do not depend on the process working directory.
3. Extract `ExpressionKind` constants from the declaration block and case
   labels from the two named dispatcher functions.
4. Assert exact concrete-kind equality for both dispatchers, with
   `InvalidExpression` as the sole intentional fail-closed sentinel.
5. Locally prove the test fails when one render case and one validation case
   are independently omitted, then restore both before committing.

### Stage 2: R25 context definition

1. Add `checkContext` beside the checker expression entrypoint that owns the
   pair.
2. Construct it at existing roots where both scope and environment are already
   available.
3. Add no methods until repeated behavior, rather than field access, requires
   one.

### Stage 3: R25 mechanical migration

1. Migrate expression checking and place checking first.
2. Migrate builtin collection, String, IO, allocation, layout, volatile, and
   print checking next.
3. Migrate calls, methods, generics, functions, ADTs, and unions last.
4. Keep token and expected-type parameters explicit throughout.
5. After each coherent package slice, run focused checker tests and the full
   ordinary suite.
6. Finish with an AST or exact-signature audit proving no non-`scope.go`
   function still receives both members separately.

### Stage 4: R47 inventory and comments

1. Generate the package-by-package missing-doc inventory from the Go AST.
2. Record suspicious exports separately, but retain every existing visibility
   decision in this RFC.
3. Document foundational packages first, then their consumers, so terminology
   stays uniform.
4. Run `gofmt`, `go vet`, and the existing CARE comment guard after each
   package.
5. Re-run the AST inventory and require zero missing exported-declaration docs.

### Stage 5: conformance and closure

1. Run every validation item below.
2. Require the snippet manifest to remain byte-identical; do not regenerate it.
3. Verify `docs/reference.md` remains accurate without modification.
4. Remove this RFC from `docs/status.md`, mark it implemented, and archive it.
5. Rebuild and restart the workbench before handoff because checker code
   changed even though language behavior did not.

## Validation

This section is exhaustive. RFC 0119 is complete only when every item passes.

- R21's AST matrix contains every concrete `ExpressionKind` in both primary
  generator dispatchers and treats only `InvalidExpression` as unsupported.
- Adding a concrete kind without either dispatcher case fails R21's test.
- Removing any existing render or validation case fails R21's test.
- No checker function outside `scope.go` accepts both `names *scope` and
  `typeEnvironment *compilerTypes.Environment`; the pair occurs together only
  as fields of `checkContext`.
- Tokens and expected types remain explicit at their diagnostic and contextual
  checking sites.
- Every exported production Go declaration under `compiler/` has a Go doc
  comment, and the AST inventory reports zero omissions.
- All added comments satisfy CARE, contain ASCII only, and cite no internal
  provenance.
- R22-R24 produce no code move; refreshed measurements and drop decisions are
  recorded in the implementation commit.
- No diagnostic text, checked program, or generated artifact changes.
- The snippet manifest is byte-identical.
- `gofmt -l` is silent for tracked Go files.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass.

## Reference synchronization

No language-reference edit is expected. Before closure, verify that the
refactor preserves every affected `docs/reference.md` contract and record that
verification. Any discovered semantic drift is outside this RFC and stops the
implementation rather than being absorbed into it.
