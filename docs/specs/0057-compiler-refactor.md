# RFC 0057: Compiler Refactor — Safety Baseline and Local Cleanup

- Kind: Rust-Style RFC
- Status: Implemented; conformance verified 2026-08-15
- Created: 2026-08-15
- Updated: 2026-08-15
- Scope: compiler internals and compiler-test infrastructure only
- Depends on: RFC 0049 (shared program walker), RFC 0056 (workbench snippet
  corpus), ADR 0054 (test package split)
- Coordinates with: `AGENTS.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, generated C, or the
  public `compiler.Compile` contract

## Summary

Perform one conservative compiler refactor pass:

- establish a byte-for-byte generated-artifact baseline from the complete
  workbench snippet corpus;
- repair confirmed text corruption and two local naming problems;
- deduplicate method-receiver rendering and the twin statement hoisters;
- replace two long header-builder parameter lists with explicit input structs.

This pass retains every validation layer and every test. It deliberately leaves
high-risk and low-return rewrites for later RFCs.

## Current verified inventory

- `compiler/generator/generator.go`: 5,824 lines, 293,359 bytes.
- `compiler/generator/generator_test.go`: 2,166 lines.
- Generator `unknownExpressionDiagnostic` references: 588.
- Active integration tests: 470.
- Workbench corpus: 110 snippets across 12 categories, including two
  multi-module programs.
- Dormant C23 canaries: 10 files, 481 lines, zero runnable test functions.
- `go test ./...` and `go vet ./...` pass before the refactor.

These measurements describe the starting tree. They are not permanent
acceptance thresholds.

## Required invariants

Every phase MUST preserve all of the following:

1. The same source map and logical entrypoint produce byte-identical entries in
   `CompilationResult.Files`.
2. Diagnostics, diagnostic categories, ordering, locations, and exit codes are
   unchanged.
3. Unsupported or malformed internal nodes continue to fail closed with a
   structured diagnostic. No guard or validation check is removed unless an
   equivalent or stronger guard covers every formerly guarded path.
4. No exported Go symbol is removed or renamed.
5. The compiler remains string-in/string-out and performs no filesystem access.
6. `compiler/tests/c23validation/` remains intact and dormant exactly as
   required by ADR 0054 and `AGENTS.md`.
7. No third-party dependency is added.
8. `docs/reference.md` is reviewed at completion and remains unchanged unless
   an observable discrepancy is discovered. Such a discrepancy blocks closure
   because this RFC does not authorize a language change.

## Item 1 — Add an exact generated-artifact baseline

Extend the existing
`workbench/snippets.TestCatalogProgramsCompile` catalog traversal and add:

```text
workbench/snippets/testdata/generated-c-sha256.json
```

The manifest records every catalog snippet and every generated artifact:

```text
category ID -> snippet ID -> artifact filename -> SHA-256(content)
```

Rules:

- Preserve its current compilation and required-artifact assertions; do not
  add a second catalog traversal that can drift from it.
- Generate the manifest from the unmodified compiler before any other RFC item.
- Compile through `compiler.Compile(snippet.Sources, snippet.Entrypoint)`.
- Hash every entry in `CompilationResult.Files`, including every module
  `.c`/`.h` pair and `main.c`/`main.h`.
- Sort category IDs, snippet IDs, and artifact filenames before serialization.
- The test fails on a missing snippet, extra snippet, missing artifact, extra
  artifact, or hash mismatch and identifies the exact snippet and filename.
- The baseline is committed test data and is not regenerated during this RFC.
- Intentional generated-C changes in later RFCs may update it explicitly.

This baseline supplements semantic integration tests; it does not replace
them.

### Acceptance

- The manifest covers all 110 current snippets and all their artifacts.
- `TestCatalogProgramsCompile` verifies the manifest using only pure Go.
- The test passes before refactoring begins.

## Item 2 — Repair confirmed text corruption

Repair encoding corruption without imposing an ASCII-only policy:

- restore the two bloomed comments in `generator.go` to short readable text;
- repair the corrupted em dash in `generator_test.go`;
- repair the corrupted em dash in `compiler/types/types.go`;
- repair mojibake test text such as `cafÃ©` to its intended Unicode spelling
  while preserving the tested rejection behavior;
- replace the literal `\n//` embedded in the `moduleFilename` comment with
  two actual comment lines.

Use `Fun<...>` in the two formerly bloomed comments. Legitimate Unicode in
source literals, diagnostics, and unaffected comments remains valid.

### Acceptance

- No known mojibake sequence remains under `compiler/`.
- The two formerly bloomed source lines are each under 200 characters.
- The malformed `moduleFilename` comment contains no literal `\n//`.
- No diagnostic text or test assertion changes except correction of corrupted
  character encoding with identical intended meaning.

## Item 3 — Apply two local naming fixes

- Rename every generator parameter named `strings` whose type is
  `*generatedStringState` to `stringState`.
- Rename the lexer rune-decoder local `close` to `closeIndex`.

Do not perform a compiler-wide error-variable rename, builtin-shadow audit, or
`sort`-to-`slices` migration in this pass.

### Acceptance

- No `*generatedStringState` parameter is named `strings`.
- The rune decoder has no local named `close`.
- The Item 3 diff contains only these two naming changes and their direct
  references.

## Item 4 — Extract receiver rendering

The generator repeatedly performs the same operation:

1. reject a missing receiver expression;
2. render it with its checked expected type;
3. parenthesize it only when it is not already one C atom.

Add one helper:

```go
func renderReceiver(
    operand *checker.Expression,
    expected compilerTypes.Type,
    state *expressionValidation,
) (string, error)
```

The helper MUST use
`renderExpressionNodeWithExpectedState(*operand, expected, state, true)` and
preserve the existing parenthesization exactly.

Replace every current call site that renders a receiver using its checked
expected type and then conditionally parenthesizes it, whether it has a local
nil guard or relies on earlier whole-expression validation. `renderReceiver`
supplies the guard uniformly. Calls that ignore atomicity, use a special
expected type for a non-receiver operand, or apply different parenthesization
remain unchanged.

### Acceptance

- The complete receiver-render-and-parenthesize block exists only in
  `renderReceiver`.
- Every identified receiver-render-and-parenthesize site calls
  `renderReceiver`; none continues to rely solely on earlier whole-expression
  validation for nil safety.
- Non-receiver operand rendering is unchanged.
- Nil receiver input returns a structured `Unknown Error`; it does not panic.
- Generated artifacts remain byte-identical.

## Item 5 — Merge the twin statement-expression walkers

`hoistTryInStatement` and `hoistConcurrencyInStatement` duplicate traversal
over the same checked statement and expression shapes.

Add to `compiler/generator/walk.go`:

```go
func walkStatementExpressions(
    statement checker.Statement,
    visit func(*checker.Expression) error,
) error
```

Contract:

- visit expressions reachable directly from one statement in pre-order;
- do not descend into nested statement bodies;
- fail closed on an unknown statement or expression shape;
- preserve written operand and argument order.

Each hoister continues to recurse into nested bodies itself. This is
load-bearing: a nested statement's hoisted prologue must remain at that
statement's indentation.

### Acceptance

- Neither hoister declares a local expression/operand/statement walker.
- Adding a checked statement shape requires updating the shared walker rather
  than both hoisters.
- Error and concurrency snippet artifacts remain byte-identical.

## Item 6 — Replace header parameter lists with input structs

Replace the 18-parameter module-header builder and 15-parameter main-header
builder with:

```go
type moduleHeaderInput struct {
    // Existing values consumed by the module-header builder.
}

type mainHeaderInput struct {
    // Existing values consumed by the main-header builder.
}

func moduleHeader(input moduleHeaderInput) string
func mainHeader(input mainHeaderInput) string
```

Rules:

- Each field corresponds directly to one existing argument.
- Do not add derived or cached state.
- Populate each struct once at its existing call site.
- Preserve field and output order.
- Rename `moduleHeaderWithUnions` and `mainHeaderWithUnions`; the suffix
  distinguishes them from nothing.

### Acceptance

- Each header builder takes exactly one argument.
- Each input field is consumed by its builder.
- Header output remains byte-identical.

## Required order

1. Capture and pass the generated-artifact baseline.
2. Apply Items 2 and 3.
3. Apply Items 4, 5, and 6 independently, running validation after each.

The organization into items describes responsibility, not a requirement to
create commits. Version-control operations require separate user instruction.

## Validation

Run after every item:

- `go test ./...`
- `go vet ./...`
- the generated-artifact baseline test

Run at completion:

- `go test -tags c23 ./...`
- `go vet -tags c23 ./...`
- verify the 470 active integration tests remain present;
- verify all 10 dormant C23 files remain present and contain no runnable test
  entrypoint;
- verify `docs/reference.md` requires no change;
- rebuild the workbench into `bin/` and restart it;
- remove RFC 0057 from `docs/status.md`;
- mark this RFC `Implemented; conformance verified YYYY-MM-DD`.

Do not normalize repository line endings as part of this RFC.

## Explicit non-goals

This RFC does not authorize:

- deleting or weakening generator validation;
- deleting or changing generator validation tests;
- deleting or activating `compiler/tests/c23validation/`;
- deleting exported type predicates or another exported Go API;
- deleting empty local directories;
- moving embedded C into separate files;
- rewriting C declarator construction;
- changing C helper-name or suffix derivation;
- replacing operator/type switches with maps;
- generic merge-helper work;
- deleting test-only wrappers;
- mass `sort`/`slices`, error-variable, builtin-shadow, or naming cleanup;
- a global maximum parameter count;
- named-return or result-struct conversion;
- splitting checker functions;
- splitting `generator.go`, which is owned by RFC 0059;
- adding `.git-blame-ignore-revs`;
- changing generated C, diagnostics, language behavior, or
  `docs/reference.md`.

Each may be reconsidered in a later focused RFC with its own evidence and
acceptance criteria. These non-goals are not accepted follow-up work and do not
require owners or `docs/status.md` entries. Add one only if a later decision
accepts the work and creates its owning spec.

## Drawbacks

- Total Go lines may stay nearly flat; the primary result is a regression
  baseline and less repeated traversal/rendering code.
- The exact-output manifest intentionally makes later generated-C formatting
  changes explicit.

## Rationale for the reduced scope

- Validation is valuable compiler-bug containment and its removal has a high
  panic and wrong-C risk.
- Dormant C23 canaries are retained future assets and add no ordinary test
  runtime.
- Embedded C extraction has unresolved interpolation and line-ending concerns.
- Declarator and generated-name rewrites touch correctness-sensitive C syntax.
- Switch-to-map and broad naming conversions add churn with little
  maintainability return.
- Large generator and checker file moves are isolated in RFCs 0059 and 0058.
