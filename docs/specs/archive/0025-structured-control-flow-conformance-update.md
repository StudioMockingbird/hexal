# ADR 0025: Structured Control Flow Conformance Update

- Kind: Architecture Decision Record (ADR)
- Status: Accepted and Implemented (Closed)
- Updates: [RFC 0015: Structured Control Flow](0015-structured-control-flow.md)
- Reviewed: 2026-08-09
- Implemented: 2026-08-09
- Closed: 2026-08-10

This specification closes the implementation-conformance work discovered
after RFC 0015 was implemented. It does not change Seawitch control-flow
syntax or semantics. RFC 0023 subsequently supersedes RFC 0015's exact-`Bool`
condition rule with truthiness.

## Done

- Lexer keywords and token kinds for `if`, `elseif`, `else`, `while`, `break`,
  and `continue`.
- Recursive parser support for conditional chains, pre-test loops, and loop
  control statements.
- Structural diagnostics for misplaced `else`/`elseif`, unexpected `end`,
  missing conditions, and loop-control statements outside `while`.
- Checker support for exact `Bool` conditions.
- Lexical child scopes for branches and loop bodies.
- Compilation-scoped binding identities for declarations, parameters, `self`,
  and resolved references.
- Deterministic C names for shadowed bindings.
- Loop-depth checking for `break` and `continue`.
- Definite-return/fallthrough checking for returning functions and methods.
- Generator lowering for `if`, `elseif`, `else`, `while`, `break`, and
  `continue`, including source line directives.
- Integration coverage in `compiler/control_flow_test.go`.
- The workbench binary was rebuilt (not restarted; see the workspace
  instruction to launch it only on explicit request).
- `go test ./...` passes with the isolated writable build cache.

## Conformance updates

### 1. Parser recovery must preserve block delimiters

RFC 0015 requires `elseif`, `else`, and `end` to be synchronization points and
requires malformed nested blocks to preserve delimiters belonging to outer
blocks. The parser's `atStatementStart` currently recognizes statement starts
but not those delimiters, so recovery can skip an outer block terminator after
an error.

Required follow-up:

- make recovery delimiter-aware;
- preserve the nearest open construct's delimiter; and
- add parser tests for malformed nested blocks followed by valid siblings.

Status: complete.

- `atStatementStart` recognizes `elseif`, `else`, and `end` as recovery
  points, and resumes at dotted chains followed by assignment or a same-line
  call.
- `block` keeps parsing to its own delimiter after a block-local diagnostic,
  consuming stray delimiters that are not its stop, so an outer `if` never
  claims a nested `while`'s `else`/`elseif` and every `end` reaches its owner.
- EOF reports `expected end to close <owner>` for the open construct exactly
  once.
- Tests: `TestParseRecoveryPreservesNestedBlockDelimiters`,
  `TestParseRecoveryKeepsInvalidDelimiterInsideWhile`,
  `TestParseRecoveryReportsMissingEndAfterMalformedStatement`,
  `TestParseRecoveryKeepsSelfAssignmentSibling`,
  `TestParseRecoveryKeepsSiblingInsideMalformedNestedLoop`, and
  `TestParseRecoveryKeepsDottedCallSibling` in `compiler/parser/parser_test.go`.

### 2. Generator validation must track loop context

RFC 0015 requires impossible checked `break`/`continue` nodes outside an active
loop to fail closed with an `Unknown Error`. The normal checker prevents such
nodes, but generator preflight currently accepts them unconditionally and the
renderer emits them without an active-loop check.

Required follow-up:

- carry loop depth or an equivalent loop-context stack through generator
  validation and rendering;
- reject `break`/`continue` when no loop is active; and
- add malformed checked-program tests proving that no invalid C is emitted.

Status: complete.

- `expressionValidation.loopDepth` is carried through preflight
  (`validateStatements`) and rendering (`writeStatementsAt`), and restored
  after nested loops.
- `break`/`continue` with no active loop are rejected with an `Unknown Error`
  in both phases.
- Nested declarations in module-level control-flow blocks and forged returning
  function/method declarations are also rejected fail-closed.
- Tests: `TestGenerateCheckedRejectsLoopControlOutsideGeneratedLoop`,
  `TestWriteStatementsRejectsLoopControlOutsideGeneratedLoop`,
  `TestGenerateCheckedPreservesNestedLoopContext`,
  `TestGenerateCheckedRestoresLoopContextAfterLoop`,
  `TestGenerateCheckedRejectsNestedDeclarationsInModuleBlocks`,
  `TestWriteStatementsRejectsNestedDeclarationsInModuleBlocks`, and
  `TestGenerateCheckedRejectsForgedReturningDeclarationWithoutReturn` in
  `compiler/generator/generator_test.go`.

### 3. Missing focused acceptance coverage

The implementation tests do not yet directly cover every RFC acceptance case.
Add focused cases for:

- nested-loop targeting of `break` and `continue`;
- empty branches and zero-iteration loops;
- parser recovery around `elseif`, `else`, and `end`;
- a return followed by unreachable statements;
- suppression of derived fallthrough diagnostics when a child diagnostic
  already exists;
- duplicate `else` and `elseif` after `else`; and
- branch/loop declaration cleanup and source-line mappings.

Status: complete. All cases are covered in `compiler/control_flow_test.go`:

- `TestControlFlowNestedLoopsAndLineMappings` (nested-loop `break`/`continue`);
- `TestControlFlowEmptyBranchesAndZeroIterationLoops`;
- `TestControlFlowReturnDiagnosticsDoNotMaskChildErrors` (unreachable
  statements, child-diagnostic suppression, unrelated parser errors, and
  dotted-call sibling recovery);
- `TestControlFlowDiagnostics` (duplicate `else`, `elseif` after `else`, and
  recovery diagnostics);
- `TestControlFlowLoopScopeCleanup` (branch/loop declaration cleanup);
- `TestControlFlowConditionLineMappings` (keyword and condition `#line`
  mappings for `if`, `elseif`, and `while`); and
- lexer unit coverage for the control-flow keywords in
  `compiler/lexer/lexer_test.go`.

## Conformance exit criteria

Complete: the two implementation fixes above are done, the focused acceptance
cases pass, `go test ./...` passes, and the workbench binary has been rebuilt.
The feature is implemented and conforming; no open conformance follow-ups
remain.
