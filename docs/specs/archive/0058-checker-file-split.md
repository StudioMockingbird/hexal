# RFC 0058: Checker File Split

- Kind: Rust-Style RFC
- Status: Draft; implementation-ready after RFC 0057
- Created: 2026-08-15
- Updated: 2026-08-15
- Scope: `compiler/checker` file organization only
- Depends on: RFC 0057 (generated-artifact baseline and local cleanup)
- Independent of: RFC 0059 (generator file split)
- Coordinates with: `AGENTS.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, checked representation,
  generated C, or any exported Go API

## Summary

Split the two mixed-responsibility checker files without rewriting their logic:

- `compiler/checker/checker.go`: 3,309 lines, 98 functions, 19 types;
- `compiler/checker/functions.go`: 2,102 lines, 64 functions, 13 types.

Move complete declarations into responsibility-named files. Preserve every
function and type body, name, signature, diagnostic, and execution order.

This RFC is an organizational pass. It does not split large functions; that
requires separate evidence and review.

## Motivation

`checker.go` currently owns the checked statement model, module orchestration,
declarations, type resolution, expression checking, constant folding,
operators, places, references, and literals.

`functions.go` currently owns the function/method model, lexical scopes, flow
narrowing, function and method declarations, statement checking, control flow,
calls, and receiver adaptation.

These boundaries increase navigation cost and make unrelated feature work
conflict in the same files. The package already uses facet files for ADTs,
collections, errors, strings, concurrency, modules, and other language areas;
the two original files should follow the same convention.

## Required invariants

1. Move declarations whole. Do not split or rewrite a function or type.
2. Preserve every declaration name, receiver, parameter, result, body, and
   comment, except a comment may be adjusted only when its file reference
   becomes false after the move.
3. Preserve diagnostic text, category, location, ordering, and aggregation.
4. Preserve source-order visibility, module isolation, narrowing invalidation,
   constant folding, and checked-node construction exactly.
5. Do not add an abstraction, context object, interface, dependency, or new
   mutable package state.
6. Do not move symbols out of package `checker` or change their visibility.
7. Do not modify tests merely to accommodate the file split. Existing tests
   must compile and pass unchanged.
8. The compiler remains string-in/string-out and performs no filesystem access.
9. Generated artifacts for the complete RFC 0057 snippet baseline remain
   byte-identical.
10. `docs/reference.md` is reviewed and remains unchanged. A required semantic
    edit indicates that the refactor changed behavior and therefore blocks this
    RFC.

## Part A — Split `checker.go`

### Retain in `checker.go`

Keep the common checked program/statement model and checker entry
orchestration:

- `Program`, `Statement`, and `TypeDeclaration`;
- `Declaration`, `Assignment`, `IfStatement`, `WhileStatement`,
  `ForStatement`, `BreakStatement`, `ContinueStatement`, `DeferStatement`,
  `ReturnStatement`, `CallStatement`, and `TryStatement`;
- their common companion data types, including `IfBranch`, `ForBinder`, and
  `DeferredAction`;
- `binding`;
- `Check` and `CheckModules`;
- `checkModule`;
- `declarationItem` and `executableItemToken`.

`checker.go` remains the package entry and the place a reader starts to
understand the checked program. General executable statement data stays
together even when its checking logic belongs to another file.

Feature-owned declaration and statement nodes remain with their feature:
`FunctionDeclaration` stays in `functions.go`, `MethodDeclaration` moves to
`methods.go`, and `ErrdeferStatement` stays in `errors.go`. These are deliberate
exceptions, not unassigned statement-model residue.

### Add `declarations.go`

Move declaration, assignment, and declared-layout checking:

- `checkTypeDeclaration`;
- `resolveObjectMembers`;
- `containsPointerType` and `containsTypeName`;
- `registerGenericTypeDeclaration`;
- `checkDeclaration` and `checkAssignment`;
- assignment, binding, type-mismatch, and assignability diagnostic helpers
  directly owned by those operations.

This file owns Hexal declaration checking. The identically named
`compiler/generator/declarations.go` planned by RFC 0059 owns C declaration
emission; the package boundary distinguishes them.

### Add `type_resolution.go`

Move written-type resolution:

- `resolveType` and `resolveTypeUse`;
- `typeUseCandidates`;
- `resolveUnionMemberUse`;
- `resolveFunctionTypeUse`;
- pointer-construction, value-type, and type-expression-token diagnostic
  helpers used by type resolution.

Do not move generic specialization logic from `generics.go`.

### Add `expressions.go`

Move the common checked-expression model and expression dispatch:

- `initializerValue`;
- `expressionContext`, `expressionTypeHint`, and `checkedExpression`;
- `initializerDiagnostics`;
- `checkInitializer` and `checkObjectLiteral`;
- `checkValue`, `checkExpression`, and `checkedFromInitializer`;
- common checked-expression node constructors that are not place-specific.

Feature-specific expression checkers remain in their existing facet files.

### Add `operator_checking.go`

Move unary/binary operator checking and folding:

- `checkUnaryExpression`, `checkBinaryExpression`, and `checkNullTest`;
- `foldUnary`, `foldBinary`, and all folded scalar-result helpers;
- constant truthiness and comparison helpers;
- arithmetic, bitwise, shift, and float operator classification;
- static division checking;
- expression type inference used for operators;
- `operatorFromToken`;
- operator conversion, eligibility, contextual operand, result-type, and
  operator-diagnostic helpers.

Do not replace switches with maps or change constant-folding algorithms.
The filename deliberately differs from the existing `operands.go`, which owns
the checked operand model rather than operator semantics.

### Add `places.go`

Move place and reference checking:

- `checkPlace` and `checkModuleQualifiedReference`;
- `dereferencePlace` and `valueFromPlace`;
- nullable-access and missing-member diagnostics;
- `checkReference` and `placeDescription`;
- place-specific node construction and `baseBindingID`.

### Add `literals.go`

Move contextual scalar literal construction:

- `contextualIntegerType` and `contextualFloatType`;
- negated, integer, and float initializer helpers;
- integer bounds and literal radix;
- out-of-range diagnostics.

### Shared-helper rule

If a private helper is used by multiple new files, leave it in `checker.go`
unless one responsibility clearly owns its semantics. Do not introduce a
`helpers.go` dumping ground.

`assignable` is a package-wide predicate and therefore remains in `checker.go`
under this rule. If shared-helper residue would leave `checker.go` at or above
800 lines, stop and report the residue rather than creating a catch-all file or
forcing an arbitrary owner.

### Part A acceptance

- `checker.go` is under 800 lines.
- Every listed responsibility has exactly one owning file.
- No declaration is duplicated or omitted.
- Existing checker and integration tests pass unchanged.
- Generated artifacts remain byte-identical.

## Part B — Split `functions.go`

### Retain in `functions.go`

Keep function declaration structure and validation:

- `FunctionDeclaration` and `FunctionParameter`;
- `checkFunctionDeclaration`;
- `checkParameters` and `checkResultType`;
- function-specific declaration helpers not owned by another file.

### Add `scope.go`

Move lexical names and flow-sensitive state:

- `bindingKind` and its constants;
- `scope` and `lookupStatus`;
- `flowFact` and `flowState`;
- `moduleScope`;
- all scope lookup, definition, child-scope, binding-ID, import-alias, and
  narrowing/escape/merge/adopt methods;
- `selfPlace` and scope-owned diagnostics.

### Add `methods.go`

Move method declaration and receiver behavior:

- `MethodDeclaration` and `methodTable`;
- method table construction, lookup, and definition;
- `checkImplDeclaration`;
- receiver spelling and receiver-token helpers;
- collision diagnostics;
- `checkMethodCall` and `checkImportedMethodCall`;
- `methodParameterTypes` and `adaptReceiver`.

### Add `control_flow.go`

Move checked executable-statement and control-flow behavior:

- `FallsThrough` and termination helpers;
- `checkBody`, `checkStatements`, and `checkStatement`;
- condition checking and narrowing;
- `checkIfStatement`, `checkForStatement`, `checkWhileStatement`, and
  `checkReturnStatement`;
- loop binder-type resolution.

Statement order, child-scope creation, flow-state merging, and diagnostic
aggregation must remain unchanged.

### Add `calls.go`

Move non-method call checking:

- `checkCallStatement` and `checkCall`;
- `checkQualifiedFunctionCall` and `checkQualifiedGenericCall`;
- `checkArguments`;
- `checkCallValue`.

Generic inference and specialization remain in `generics.go`; calls continue
to delegate to them.

### Part B acceptance

- `functions.go` is under 600 lines.
- Every listed responsibility has exactly one owning file.
- No declaration is duplicated or omitted.
- Existing checker and integration tests pass unchanged.
- Generated artifacts remain byte-identical.

## Required order

1. Complete RFC 0057 and confirm its generated-artifact baseline passes.
2. Record the current declaration inventories of both `checker.go` and
   `functions.go` before moving anything; Part A transfers three statement
   structs from `functions.go` into `checker.go`.
3. Apply Part A as declaration moves only, including that transfer.
4. Run all validation before continuing.
5. Apply Part B as declaration moves only.
6. Run final validation.

Do not combine a function rewrite with a move. If a moved function deserves
simplification, record it for a later RFC after this split is complete.

RFC 0059 may run before or after this RFC once RFC 0057 is complete. Do not
interleave the two file splits in one review or implementation pass.

## Validation

After each part:

- `gofmt` only the touched checker files;
- `go test ./compiler/checker`;
- `go test ./compiler/tests/integration`;
- `go test ./...`;
- `go vet ./...`;
- run the RFC 0057 generated-artifact baseline test;
- compare the before/after declaration inventory and reject any missing,
  duplicated, renamed, or signature-changed declaration.

The RFC 0057 manifest covers successful compilation and generated artifacts
only. Preservation of diagnostic text, category, location, ordering, and
aggregation is established by the checker unit tests and integration tests.

The touched-file restriction on `gofmt` is normative: formatting the complete
package would rewrite unrelated files with different line endings and violate
this RFC's no-unrelated-formatting invariant.

At completion:

- verify `compiler/checker/generics.go`, existing facet files, and test files
  were not reorganized incidentally;
- verify `docs/reference.md` requires no change;
- rebuild the workbench into `bin/` and restart it;
- remove RFC 0058 from `docs/status.md`;
- mark this RFC `Implemented; conformance verified YYYY-MM-DD`.

No version-control operation or commit structure is required by this RFC.

## Non-goals

- Splitting or shortening individual functions.
- Changing checker algorithms or diagnostic construction.
- Introducing a checker context object or dependency injection.
- Renaming private or exported declarations.
- Moving logic already owned by a facet file.
- Splitting `generics.go`, `types.go`, `lexer.go`, or test files.
- Adding unit or integration cases solely because code moved.
- Reformatting unrelated files or normalizing line endings.
- Changing the language reference, generated C, or compiler API.

## Drawbacks

- Total line count remains essentially unchanged.
- The move creates a large mechanical diff.
- More files require a reader to know the responsibility names; the explicit
  ownership rules above are therefore part of the design, not suggestions.

## Expected result

- `checker.go`: package entry, checked statement model, and module orchestration.
- `functions.go`: function declaration model and validation.
- Type resolution, expressions, operator checking, literals, places, scopes,
  methods, control flow, and calls each have one obvious file.
- Feature work touches fewer unrelated declarations and produces fewer merge
  conflicts.
