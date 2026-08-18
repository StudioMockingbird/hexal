# RFC 0074: Audit Refactor Batch

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-18. Stages 1–6 are the whole spec;
  Stage 7 is closed and its items are specified as RFCs 0073 and 0076–0079, of
  which 0078 was itself rejected.

  **Landed and validated 2026-08-18** (`go test ./...`, `go vet ./...`,
  `go vet -tags c23`, `gofmt -l` all clean at each step):

  | Item | State |
  |---|---|
  | R1 drained seams | Done. `FamilyContent`, its nine dead input fields, and the `{{.FamilyContent}}` template line are gone; `hexal.h` lost one blank line and the manifest was regenerated for that alone. |
  | R2 unreachable functions | Done, with one correction below. |
  | R3 test-only specialization API | Done. |
  | R4 orphans | Done. |
  | R5 write-only fields | `CloseParen` and `ElseColumn` done. `ImportKeyword` **is read** — see corrections. |
  | Stage 3 stdlib | Done. `sort` no longer imported anywhere in `compiler/` or `workbench/`. |
  | Stage 2 R8 | Done. `ContainsUnionMember` and `RemoveUnionMember` iterate members directly. |
  | R9 one diagnostic idiom | Done to its floor. Every token-shaped composite in `checker/` now routes through `typeErrorAt` or one of the three new siblings, plus a `diagnosticAt` adapter for the many pointer-returning sites; **1,155 net lines removed from `checker/`**. 27 composites remain: the 4 constructor definitions themselves, 3 built from `SourceLine`/`SourceColumn` rather than a token, and 19 that carry no position at all and so cannot take a token-based constructor. |
  | R11 diagnostic carries no module | Done. `Diagnostic.Module` holds the logical source key, stamped once per stage at the point where module identity is known — the `CheckModules` loop, the reachability walk, and the generator's per-module loops — rather than at any of the ~900 construction sites. `Error()` renders `at app.hex:5:3`. The prescribed `(Module, Line, Column)` sort was tried and reverted — see the ordering note below. 71 assertions across 11 files updated; lexer and parser unit tests keep the unqualified form because those stages legitimately do not know their module. |
  | R12 | Done except one bullet, deliberately. Three unwrap ladders now use `errors.As`, `blockFailure.Unwrap` added, 35 `, got` → `; got`, both trap-message outliers normalized. **"Route every stage through `mergeDiagnostics`" is not done** — see the ordering note below; its premise is wrong and doing it makes the output worse. |
  | R13 structure | Done. `validateExpressionNode` 876 → **388** lines and `renderExpressionUncheckedWithState` 661 → **279**, both matching the spec's targets. Collections moved to `arrays.go`, text to `strings.go`, concurrency to `concurrency.go`, view bridge to `views.go`; cases were moved verbatim, so every check and its order are unchanged. Zero new files. |
  | R17 tests | Done. `generateOne(t, program)` replaces the call-plus-error-check that appeared verbatim at 42 sites; six `Test<X>ComponentDeterministic` copies became one table (Error stays separate — it renders from a synthetic emission, a different shape); the byte-identical Array and List `ComponentAbsentWithout` pair became one test (View stays separate, as the spec requires); `compileMulti`'s 61 pass-through uses call `compiler.Compile` directly; the five scattered artifact accessors moved beside `hexalH`/`rootC`/`rootH`; and the three assertion vocabularies are one `assert*` family. The `TestDictComponentHexalHeaderOwnsNoDictText` finding is stale — that test is 30 lines, not 289. |
  | R14 parameter bundling | Done for the 15 `(expected Type, hasExpected bool)` sites; `expected *Type`, nil = absent. |
  | R15 naming | Done: 42 `environment *scope` → `names`, component builders already plural, `alloc.go` → `heap.go`. Err-suffix item is stale — see corrections. |
  | R16 exported API | Done: generator exports unexported, `UnsupportedError`/`LimitError` deleted, package docs added for `compiler`, `compiler/types`, and `workbench`, and every `CompilationResult`/`CompilationStats` field documented. |
  | R18 coverage | Items 3 and 4 done: `determinism_test.go` asserts byte-identical artifacts and stable diagnostic order across repeated compiles, and every `CompilationStats` field on both success and failure. |

  **Stages 1–6 are complete.** The only unactioned findings are the ones the
  corrections below record as stale or wrong.

  **R17's assertion-vocabulary hazard was honoured, not merged away.**
  `assertRejects` requires the *first* diagnostic to match (27 tests) and
  `assertRejectsAnyDiagnostic` — the former `requireRejected` — accepts a match
  in *any* (26 tests). The spec is right that merging them naively weakens 27
  tests or breaks 26, so the two semantics kept two names that say which is
  which. The vocabulary is unified at the prefix, which is what made three
  families in one package confusing; the assertions themselves are untouched,
  as invariant 2 requires.

  **R11's sort key and R12's routing bullet were both tried and both reverted,
  by decision.** Implemented literally, they route every stage through
  `mergeDiagnostics` and sort by `(Module, Line, Column)` — which reorders
  multi-module diagnostics from *dependency-first* to *alphabetical*, since
  `mergeDiagnostics` is the only place a module comparison would apply.

  R12's premise for the routing is factually wrong: checker diagnostics do not
  "reach `failureResult` unsorted". `CheckModules` already emits them grouped by
  module in the graph's dependency order and sorted by position within each
  module, which is strictly better than what `mergeDiagnostics` would produce.
  The generator fails fast with a single diagnostic and has nothing to sort.

  So neither landed, and both the module comparison and the three routing
  wrappers are gone. That is less code than either alternative, and it keeps
  dependency-first order: a failure is reported in the module it originates in
  before the module that consumes it. `Diagnostic.Module` and its rendering —
  R11's actual value — are independent of the sort and did land.

  **Corrections to the spec's findings, all verified against the tree:**

  - **`IsAtomic` is not dead.** R2 lists it, but `render.go` reads it to decide
    whether an Atomic binding carries top-level `const` — the caller RFC 0073's
    D26 fix added, after this audit was taken. It is inlined as `typ.Atomic != nil`
    rather than deleted.
  - **`ImportKeyword` is not write-only.** R5 lists it; `parser.go:95` reads it
    for the misplaced-import diagnostic, which is D21's fix. Kept.
  - **The err-suffix singletons are gone.** R15 counts 35 names occurring once;
    none remain. `spawnError` is not an error variable at all — it is the
    `Task<T> | Error` union type, correctly named.
  - **Stage 4's "manifest unchanged" is wrong.** R12 mandates fixing two
    generated-C trap messages, which necessarily moves the manifest. It moved for
    `hexal/string.c` only, deliberately, alongside R1's `hexal.h` line.
- Created: 2026-08-16
- Updated: 2026-08-18
- Scope: behavior-preserving cleanup found by the six-pass refactor audit —
  deletion, consolidation, structure, and one measured performance item
- Depends on: nothing. Every item here lands independently of RFCs 0072–0079.
- Coordinates with: RFC 0073 (defect batch), `AGENTS.md`, `docs/reference.md`,
  `docs/status.md`
- Companion: RFC 0073 carries every correctness defect from the same audit

## Summary

Roughly **1,900 deletable lines** and three measured allocation hot spots worth
**33% of compiler allocations**, none of which changes observable behavior.

Every item is behavior-preserving **with one carve-out, stated here rather than
buried in a table**: removing the `.at()`, `.is_empty()`, and `.addr` migration
branches changes the *diagnostic text* for programs that stay rejected either
way. Acceptance is unchanged — every program rejected before is rejected after —
but the message differs, and message text is emitted output. See invariant 6.

Anything else that changes what the compiler accepts, rejects, or emits belongs
to RFC 0073 and is out of scope here.

Stages are independent unless stated. Do not land them as one change.

## Guiding invariants

1. Generated C is byte-identical before and after, except where an item states
   otherwise and explains why.
2. No test is weakened, skipped, or deleted to accommodate a refactor.
3. No exported symbol is removed without evidence of zero consumers, counted in
   production **and** test code separately.
4. The compiler remains string-in/string-out with no filesystem access.
5. Regenerate the RFC 0057 snippet manifest once per stage that legitimately
   changes output, never as a way to make a failure go away.
6. **The migration-branch removal is the one carve-out from invariant 1.**
   Removing the `.at()`, `.is_empty()`, and `.addr` branches changes the
   diagnostic text those programs receive; it must not change whether any
   program is accepted. Two test sites assert the current text verbatim and must
   be updated in the same commit as the removal, not adapted afterwards:
   `compiler/checker/objects_test.go:143` (`.addr`, from
   `compiler/checker/places.go:172`) and
   `compiler/parser/syntax_test.go:230-232` (`.is_empty()`). Updating them is
   not a violation of invariant 2 — the assertion moves to the replacement
   diagnostic, it is not deleted. Generated C is unaffected: these programs
   never reach the generator.

## Stage 1 — Deletion (~1,900 lines, zero behavior change)

### R1. Drained transition seams from the component migration — ~89 lines, 12 files

Ten drained functions — nine `*FamilyContent` plus `concurrencyRuntimeContent` —
all `return ""`:
`array_component.go:58`, `concurrency_component.go:115`, `:122`,
`dict_component.go:69`, `error_component.go:16`, `heap_component.go:30`,
`list_component.go:65`, `string_component.go:23`, `view_component.go:47`,
`wrap_component.go:53`.

`emission.go:889-903` concatenates all of them into `familyContent`, assigns it
to `hexalHeaderModel.FamilyContent` (`:878`), and `packages/hexal.h:12` renders
`{{.FamilyContent}}` — always empty. `emission.go:725` writes
`concurrencyRuntimeContent(...)` into the root module body — also always empty.

**Nine of eleven `hexalHeaderInput` fields exist only to feed these drained
seams** (`emission.go:828-837`, `generator.go:56-64`); only `sizeLiterals` and
`requirements` are live.

This is completed-migration residue. Output changes by one blank line in
`hexal.h`.

### R2. Unreachable functions — ~126 lines

Reachability established by running `deadcode` twice — once with main-package
roots, once with `-test` roots — so "dead" and "test-only" are a diff of two
runs rather than a grep.

| Symbol | Location |
|---|---|
| `renderExpressionUnchecked`, `renderOperation`, `renderUnaryOperation`, `renderBinaryOperation`, `renderExpressionNode`, `renderExpressionNodeWithState`, `objectLiteral` | `generator/render.go:673,1343,1369,1437,1713,1717,1938` |
| `validateCheckedOperand`, `validateExpressionChild` | `generator/validation.go:444,2015` |
| `signedMaximumMacro`, `hasPendingDefers` | `generator/render.go:2063,324` |
| `unionHelperName`, `viewCName`, `endianEligible`, `containsSizeConversion` | `generator/unions.go:39`, `views.go:63`, `bitwise.go:344`, `conversions.go:128` |
| `variableNode`, `isSignedMinimum`, `isNegativeOne`, `stringConstantFoldIndex` | `checker/expressions.go:330`, `operator_checking.go:819,827`, `strings.go:460` |
| `IsArray`, `IsView`, `IsTask`, `IsChannel`, `IsAtomic` | `types/collections.go:49,52,68,71,77` |
| `assertEmits` | `tests/integration/helpers_test.go:49` |

`IsList`, `IsDict`, `IsString`, `IsMutex`, and `IsManaged` **are** live — do not
batch-delete the predicate family. `IsManaged` has one in-package caller;
unexport rather than delete.

Six of the render entries are the stateless half of a `renderX`/`renderXWithState`
pair. The pattern is eight pairs deep and has survived at least three prior
refactors.

### R3. `types/generics.go` specialization API is test-only — ~143 lines

`Environment.LookupGeneric` (`:41`), `.Substitute` (`:147`, ~96 lines),
`.Specialize` (`:243`), `typeSerialKey` (`:259`), and the
`Environment.specializations` field (`types.go:231`, initialized `:287`) have
zero non-test callers. Only `types/generics_test.go` exercises them.

The checker built its own machinery instead — `specializeObjectType`,
`specializeADTType`, `specializeFunction`, `specializeMethod` in
`checker/generics.go`.

`Substitute` additionally returns `typ` unsubstituted for `Array`, `Task`,
`Channel`, and `Atomic`, so it carries a latent placeholder leak. Delete rather
than fix.

`DeclareGeneric`, `TypeParameter`, `ContainsTypeParameter`, and
`SanitizeIdentifier` in the same file are live. Keep them.

### R4. Orphans

`old_sections.txt` (repo root, 0 bytes, untracked, not gitignored); `tools/`
(empty); `.oven/main.c` and `.oven/main.h` (gitignored pre-rename leftovers).

### R5. Write-only struct fields

`parser.GroupedTypeExpression.CloseParen` (`type_expressions.go:122`),
`parser.ImportDeclaration.ImportKeyword` (`ast.go:45`),
`checker.IfBranch.ElseColumn` (`checker.go:75`). Each assigned once, never read.

`UnionTypeExpression.Pipes` is read only by `type_expressions_test.go` — a
judgment call, not a deletion.

### Do not delete

**`checker.Check` is test-only, not dead.** It has five callers —
`generator_test.go:53,76,170` and `walk_test.go:30,140`. One audit pass reported
it as dead including tests; that was wrong, and deleting it breaks those sites.
Treat it as a declared test seam or inline it, but verify before removing.

**Component render-model fields** (`AtReadReturn`, `Helper`, `EmitHash`,
`HashHelper`, `StrandKey`, and siblings) read as unused to a Go-only census but
are consumed by `packages/*.h` templates — verified at `list.h:66`,
`wrap.h:11,17,23,29`. **Never run a Go-only field-usage tool against these
structs.**

## Stage 2 — Measured performance (~33% of allocations)

Baseline over the 98-snippet corpus: **350 ms/op, 15.79 MB/op, 78,364
allocs/op**. Memory profile sampled 105.7 MB.

| # | Site | Cost | Action |
|---|---|---|---|
| R8 | `types.unionMembers` (`unions.go:145-153`) does `append([]Type(nil), …)` on every call; read-only callers copy for nothing — `ContainsUnionMember` (`:236`), `RemoveUnionMember` (`:248` and `:250`, twice per call) | **11.00 MB, 10.4%** | Iterate `typ.Union.Members` directly in read-only callers |

**R6 and R7 moved to RFC 0073** as A6 and A10's companion A7. Each edits the same
loop or function as a defect there — R6 shares `compile.go:84` with D13, R7
shares `walkStatementExpressions` with D8 — so keeping them here would force an
ordering dependency between two specs. Their measurements (4.3% and 18.0% of
allocations) travel with them.

R8 stands alone: `types/unions.go` is touched by nothing else in either spec.

### Fusing — deferred, and what it would mean

A separate, larger idea: the 21 discovery passes each call `walkProgram` over
the entire program independently — collecting arrays, lists, dicts, strings,
concurrency, conversions, and so on. Fusing means one traversal carrying 21
visitors:

```go
// today: 21 full traversals
discoverGeneratedArrays(program)   // walkProgram #1
discoverGeneratedLists(program)    // walkProgram #2
// ...19 more

// fused: one traversal, 21 collectors
walkProgram(program, &programVisitor{
	Type: func(t compilerTypes.Type) error { /* every collector inspects t */ },
})
```

That removes 20 of 21 traversals, but it conflicts with `walk.go`'s stated
design goal — each collector stays focused on what it collects and never
re-implements traversal — and it couples every family's discovery into one
callback.

**Do not fuse under this RFC.** R7 captures the allocation win without
restructuring. Revisit once RFC 0075's `BenchmarkCollections` and
`BenchmarkCorpus` can show what the remaining 20 traversals actually cost; if
they are cheap relative to the allocation already removed, the coupling is not
worth it.

CPU is otherwise flat and healthy: generation 41%, checking 19%, GC 22%. No
quadratic scan and no string-concatenation-in-loop exists; `strings.Builder` is
used consistently. **Nothing else is measurably slow — do not optimize
speculatively.**

## Stage 3 — Standard-library modernization

`slices` and `maps` appear **nowhere** in the repository. go.mod is Go 1.26.

| Pattern | Sites | Replacement |
|---|---|---|
| `make + range map + append + sort.Strings` | 13 | `slices.Sorted(maps.Keys(m))` — 5 lines to 1 |
| `sort.Strings(x)` | 9 | `slices.Sort(x)` |
| `sort.SliceStable(x, i<j)` | 8 (5 are a plain `.CName <` key) | `slices.SortStableFunc` with `strings.Compare` |
| `equalStrings` (`types/generics.go:270`) | 9 lines, 1 caller | `slices.Equal` — verified identical |
| `slicesContains` (`integration/conversion_test.go:203`) | 8 lines | `slices.Contains` |
| `sortedKeys` (`integration/helpers_test.go:89`) | 8 lines | `slices.Sorted(maps.Keys(...))` |
| `containsWord` (`workbench/snippets/catalog.go:185`) | — | `slices.Contains(strings.FieldsFunc(...), word)` |

~70 lines removed, `sort` dropped from 13 files.

`types/unions.go:102` delegates to a comparator returning `int`; converting it
is net-zero. Include it for consistency or skip it — do not treat it as a
finding.

## Stage 4 — Diagnostic and error-handling consolidation

### R9. One diagnostic idiom

Current inventory across ~978 construction sites:

| Idiom | Sites |
|---|---|
| `unknownExpressionDiagnostic` (generator) | 535 |
| Raw `Diagnostic{...}` composite | 258 |
| `typeErrorAt` | 178 |
| **`types.NewDiagnostic` — the exported constructor** | **3** |

Roughly **211 raw `TypeError` composites** carry `Line: tok.Line, Column:
tok.Column` and are mechanically identical to `typeErrorAt` (`scope.go:362`).
Routing them through it removes about 1,200 lines. Add the three missing
siblings — `nameErrorAt`, `moduleErrorAt`, `unknownAt` — for the 9 `NameError`
and 4 `ModuleError` sites.

### R10. Drop `error` from `walkProgram` — moved to RFC 0073

**No longer in scope here.** Removing the error return requires RFC 0073's D8
fail-closed arm to exist first, so keeping it here would force an ordering
dependency between specs. It moved as A10 and travels with A7, which edits the
same function.

<details>
<summary>Original rationale, retained for reference</summary>

### R10 (moved). Drop `error` from `walkProgram` and `programVisitor`

All 21 visitor callbacks return `nil` unconditionally, and `walk.go:516`'s
default is unreachable because all 14 `statementNode()` types are cased. The
return value therefore drives nine `panic(err)` sites (`bitwise.go:39,167,222`,
`division.go:32`, `emission.go:1082`, `errors.go:37`, `print.go:89`,
`wrap.go:67`) and one silent `_ = walkProgram(...)` (`declarations.go:393`) —
the same condition handled two opposite ways.

Removing the error return deletes all ten sites and roughly 180
`if err != nil { return err }` lines.

Sequence this **after** RFC 0073's D8 (adding the missing `default:` to
`walkStatementExpressions`), so the fail-closed arm is added before the plumbing
around it is simplified.

</details>

### R11. Diagnostic carries no module

Fields are `Category, Stage, Line, Column, Message` (`types.go:156-162`). In a
multi-module build every message renders as `at 5:3` with no module, and
`mergeDiagnostics` (`compile.go:370-374`) sorts on `(Line, Column)` alone,
interleaving modules.

Add `Module`; sort by `(Module, Line, Column)`. This changes every message and
will touch tests that assert diagnostic text — scope it as its own change.

### R12. Smaller error-handling items

- Collapse three hand-rolled unwrap ladders (`types.ErrorMessages` at
  `types.go:196-212`, `compile.mergeDiagnostics` at `:351-376`,
  `parser.diagnosticsFrom` at `parser.go:483-498`) and add
  `blockFailure.Unwrap()` (`parser.go:54-58`) so `errors.As` traverses it. There
  are zero uses of `errors.Is`/`As`/`Unwrap` in the tree today, and four `%w`
  wraps that nothing inspects.
- Route every stage's diagnostics through `mergeDiagnostics`. Checker and
  generator diagnostics currently bypass it and reach `failureResult` unsorted.
  Both paths are deterministic, so this is consistency, not a flake fix.
- Normalize 34 `, got X` sites to `; got X` (55 already use the majority form).
- Fix two generated-C trap-message outliers among ~33: `"String index is
  outside its bounds"` (uppercase start, and diverges from the `… index out of
  bounds` phrasing used by list/array/view) and `"invalid UTF-8 in String"`.

### Confirmed already consistent — do not "fix"

0 of 221 messages end in a period. All 48 uppercase-initial messages begin with
a Hexal type name. Identifier quoting is uniformly bare. No error is ever
compared by string. These were read individually, not sampled.

## Stage 5 — Structure

### R13. Split two oversized functions into their existing family files

The RFC 0058/0059 package splits held for 48 of 50 generator files. Two drifted.

**Scoped to files no other open spec touches.** `types.go` and
`checker/generics.go` are excluded: RFC 0073's identity work rewrites both, and
splitting a file that is being rewritten guarantees a conflict. Split them under
that RFC or afterwards, not here.

In scope: `generator/validation.go`, `generator/render.go`,
`checker/operator_checking.go`, `parser/parser_test.go`,
`generator/generator_test.go`.

| Function | Now | Extract to |
|---|---|---|
| `validateExpressionNode` (`validation.go:654-1529`) | 876 lines, 57 cases | collections → `arrays.go`; strings → `strings.go`; concurrency → `concurrency.go`; view bridge → `views.go`. Leaves ~380 |
| `renderExpressionUncheckedWithState` (`render.go:677-1342`) | 665 lines, 53 cases | collections → `arrays.go`; strings → `strings.go`; view bridge → `views.go`. Leaves ~290 |

This is not a new convention: 25 of the render switch's cases
(`render.go:1189-1215`) are already one-line delegates. The fat cases are the
stragglers. Zero new files.

Do **not** re-split the packages. Median file size is 190 (checker) and 130
(generator) lines. Three files exceed 1,500 and all three are in scope here:
`generator/validation.go` (2,144), `generator/generator_test.go` (2,139), and
`generator/render.go` (2,107).

### R14. Parameter bundling

| Group | Functions | Action |
|---|---|---|
| `(expected Type, hasExpected bool)` | **14** (`validation.go` ×8, `unions.go` ×5, `render.go:666`) | `expected *Type`, nil = absent. The package **already uses this idiom** for `result *Type` (`render.go:24,39`) — this fixes an internal inconsistency rather than adding an abstraction. Highest ROI in the stage. |
| `(CallExpression, PropertyExpression, checkedExpression, *scope, *Environment)` | 17 across 11 checker files | a `methodCallSite` struct |
| `writeSpecializedDefinitions` (9 params), `writeFunctionDefinition` (9), `writeMethodDefinition` (8) | `declarations.go:452,158,235` | 6 identical params → one `definitionContext`. The same values are named `functions`/`methods` at `:158` and `functionsTable`/`methodsTable` at `:452` |
| `renderForSequence`/`renderForText`/`renderForDict` | `for.go:71,119,165` | identical 7-param lists → one `forLowering` struct |

Leave the ten `isCanonicalX` functions in `types/types.go` sharing
`(*Environment, Type, *canonicalTypeState)` — that is a deliberate uniform
dispatch family where the signature is the contract.

### R15. Naming

| Issue | Measure |
|---|---|
| `*scope` parameter spelled two ways | `names *scope` 65×, `environment *scope` 43×. The latter sits beside `typeEnvironment *compilerTypes.Environment` in 88 signatures and actively misleads. Normalize to `names` |
| Component builders | `arrayComponents`/`dictComponents`/`listComponents` emit one artifact but are plural; `viewComponent`/`wrapComponent`/`errorComponent` emit one and are singular. All return a slice — make all plural |
| Error variable suffixes | 939 `err`, ~90 ad-hoc, of which **35 occur exactly once**. Collapse the singletons; keep the multi-error-live cases (`heapErr` 11, `receiverErr` 10). Two use `Error` not `Err`: `spawnError`, `unrelatedParserError` |
| `alloc.go` ↔ `heap_component.go` ↔ `packages/heap.{c,h}` | Rename `alloc.go` → `heap.go`, or the component to `alloc_component.go` |

Skip the singular/plural filename inconsistency (`arrays.go` beside
`bitwise.go`). Real, but no reader is misled and it churns blame across 15 files.

### R16. Exported API

Unexport or delete, all with zero external consumers: `generator.PrivateCName`,
`NameKind`, `ValueName`, `TypeName`, `MemberName`, `FunctionName`;
`types.UnsupportedError` and `LimitError` (declared `ErrorCategory` values no
diagnostic ever emits).

Do **not** unexport `checker.ExpressionKind`, `OperandKind`, or `ViewRootKind` —
they are field types on exported structs.

**Include exported struct fields in the sweep.** RFC 0078's census found they are
the larger half of the surface — 164 in `parser` and 161 in `checker`, more than
either package's declarations — so a pass over functions and types alone covers
under half of what is exported. `generator` needs no sweep at all: it exports 7
symbols total and is already encapsulated.

Add package docs for `compiler`, `compiler/types`, and `workbench` — verified
missing; the other five packages have them. The OpenCode audit reported 8 of 8
documented; that is wrong, and the count above was checked directly.

Document `CompilationResult.Stderr`, `.ExitCode`, and every `CompilationStats`
field. Skip the 75 undocumented `lexer.TokenKind` constants — their names are
the documentation.

## Stage 6 — Tests

### R17. Consolidation

| Finding | Measure |
|---|---|
| `GenerateChecked(...)` plus a 3-line error check, verbatim | 105 exact-pattern sites (164 total call sites) across 15 generator test files. One `generateOne(t, source)` helper removes ~420 lines |
| `Test<X>ComponentDeterministic` | 8 near-identical copies differing only in source and artifact key. Table-driven: ~110 lines → ~25 |
| `Test<X>ComponentAbsentWithout<X>` | `array_component_test.go:99` and `list_component_test.go:163` are byte-identical modulo one word. **`view_component_test.go:91` is NOT** — `view.h` is emitted transitively by the array component. Keep it separate |
| Three assertion vocabularies in one package | `assert*` (`helpers_test.go`), `require*` (`functions_test.go`), `want*` (`modules_*_test.go`) |
| `compileMulti` (`modules_resolution_test.go:14`) | pure pass-through to `compiler.Compile`, 51 uses |
| `arrayH`/`dictH`/`listH`/`stringH`/`stringC` | 5 one-line wrappers scattered across 4 files; belong beside `hexalH`/`rootC`/`rootH` |
| `TestDictComponentHexalHeaderOwnsNoDictText` | 289 lines of inline golden C header |

**Hazard, not a task:** `assertRejects` (`helpers_test.go:44`) matches
`Stderr[0]` only; `requireRejected` (`functions_test.go:22`) scans all
diagnostics. Merging them naively **weakens 26 tests**. Only under explicit
review of each.

### R18. Coverage gaps

Listed in priority order. The first is a design decision, not a task.

1. **Nothing verifies that generated C compiles — accepted, not scheduled.** By
   policy no test invokes a toolchain, and `compiler/tests/c23validation/` has no
   runnable entries. RFC 0073's D2 (undeclared `hex_channel`) and D26 (discarded
   `const`) both emit uncompilable C while `go test ./...`, `go vet ./...`, and
   `go vet -tags c23` all pass.

   **Decision: no external toolchain, and no compile gate in this RFC or 0073.**
   The no-external-tool rule stands. When C-compiler validation is eventually
   introduced — most likely under RFC 0055's driver — it will surface this whole
   class at once and they get fixed as a batch. Until then each instance is
   fixed individually as an ordinary defect, exactly as D2 and D26 are.

   Recorded so it is not re-proposed each time an instance is found. The gap is
   known, priced, and deliberately carried.
2. **The SHA-256 manifest is a change detector, not an oracle.** It verifies the
   exact artifact set and per-file hash for 98 programs — strong against
   accidental drift. It cannot distinguish a correct refactor from a regression:
   both fail, both are "fixed" by re-baselining. It also covers only successful
   compilations.
3. **Determinism has no committed test.** Generated output was verified
   byte-identical across 300 compiles, but nothing in the repository asserts it.
   Compiling each snippet twice and comparing before hashing is cheap insurance.
4. **`CompilationStats` is nearly untested** — only `SourceLines` and
   `TokenCount != 0`. This is exactly why RFC 0073's D7 survived.
5. **Failure paths have no artifact protection.** Every snippet compiles.
   Diagnostic text and ordering are covered only by first-diagnostic substring
   matching.

## Required order

| Stage | Depends on | Manifest |
|---|---|---|
| 1 — deletion | — | one blank line in `hexal.h` (R1) |
| 3 — stdlib | — | unchanged |
| 5 — structure | — | unchanged |
| 6 — tests | — | unchanged |
| 2 — performance | — | unchanged |
| 4 — diagnostics | — | unchanged |

**Every stage is independent.** Stages 2 and 4 previously depended on RFC 0073
D13 and D8, but the items that carried those dependencies moved into 0073
itself: R6 became A6 and R10 became A10. Stage 2 now holds only R8, which stands
alone, and Stage 4's remaining items (R9, R11, R12, and the recover boundary)
never had a D8 dependency. This matches the header's "Stages 1–6 are the whole
spec."

Landing RFC 0073 first is still the recommendation — fixing defects on a moving
codebase is harder than refactoring a correct one — but it is now a preference,
not a constraint.

## Validation

Per stage:

- `go test ./...`, `go vet ./...`,
  `go vet -tags c23 ./compiler/tests/c23validation`
- Snippet catalog compiles; manifest unchanged except where a stage states
  otherwise
- No test weakened, skipped, or deleted
- Exported-symbol removals verified against production and test callers
  separately

At completion:

- `docs/reference.md` verified to require no change — this RFC alters no
  contract
- `docs/status.md` updated; this RFC's entry removed on close
- Workbench rebuilt into `bin/` and restarted

## Stage 7 — Architecture consolidation

**This stage is closed. Every item now has its own spec.** It recorded six
architecture programs — the structural form of things RFC 0073 fixes tactically
— on the understanding that each needed a bounded spec before implementation.
All six now have one.

| Item | Now specified as |
|---|---|
| R19 — authoritative module graph | **RFC 0076** |
| R20 — per-compilation type arena | **RFC 0073** (D25 made it a correctness requirement) |
| R21 — canonical type key | **RFC 0073** (must land with D19) |
| R22 — literal registry | **RFC 0077** |
| R23 — package boundary | **RFC 0078 — rejected there, see R23 below** |
| R24 — ownership and lifetime | **RFC 0079**, scoped to the three decidable cases |

Identifiers are retired rather than reused, so a reference to any R19–R24 still
resolves. The subsections below are pointers only; nothing in this stage is
implementable from this document.

### R19. One authoritative module graph — specified as RFC 0076

**No longer in scope here.** RFC 0076 owns this work: an immutable `moduleGraph`
built during reachability, holding per module the canonical id, the exact
logical source key, the parsed program, and its resolved ordered imports.

It subsumes the duplicate import-path resolution recorded earlier in this RFC
(`compile.go:132` validating, `checker/modules.go:542` silently mirroring it),
the dead `scope.imports` table, and the four-times-duplicated
canonical-id-to-logical-key lookup. Verification while writing that spec also
found the two mirrors are **both named `canonicalModuleID`, in two packages,
with different signatures** — a name collision on top of the logic duplication.

R11's `Module` field on `Diagnostic` stays in this RFC; RFC 0076 supplies the
identity it needs.

### R22. Literal registry — specified as RFC 0077

### R23. Package boundary — **closed, rejected**

Specified as RFC 0078 and rejected there. `internal/` restricts importing from
outside the module subtree, and `hexal` is an unpublished, unqualified module
path that nothing outside this repository can import in the first place. The move
would have rewritten 101 files' import blocks to enforce a restriction that
already holds.

R16 below is unaffected — it never depended on this. See RFC 0078 for the full
rationale and the trigger that would reopen it.

### R24. Ownership and lifetime — specified as RFC 0079

**This scope has since widened; RFC 0079 is authoritative.** This section
described one case — rejecting `free` of a pointer statically known not to be
heap-derived — and recorded double-free and use-after-free as correct by design.
That reading has been rejected. `AGENTS.md` goal 18 now reads "catch every
memory error a local analysis can decide without adding a language concept or
disproportionate checker complexity", and 0079 covers three cases under it:
non-heap free, double free, and use-after-free. Freeing a literal and leaks stay
undiagnosed — no local analysis decides them. Do not implement from this
paragraph.

## Additional items from the external audit

Folded into the stages above where they fit; listed here where they are new.

| Item | Stage | Note |
|---|---|---|
| `resolveType` one-caller compatibility wrapper | 1 | verify caller count before removing |
| `.at()`, collection `.is_empty()`, `.addr` migration branches | 1 | **Decided: remove all three.** These are RFC 0063's tailored removal diagnostics. The language has no external users and the removals are several specs old, so they help nobody. Remove them together — keeping one and dropping two is the outcome to avoid |
| Stale tracked `.vscode` templates; `notes.md` | 1 | confirm `notes.md` is unwanted before deleting |
| `indexIn` → `slices.Index`; map-copy loops → `maps.Clone` | 3 | joins the `slices`/`maps` batch |
| `PixelSubtotal` → `CompilerSubtotal`, and workbench `pixelSubtotalMs` | 5 | **exported API rename**; a pre-rename name survives in the public surface |
| `"in this RFC"` text inside an ADT diagnostic | 5 | a spec reference in user-visible output — the CARE contract from RFC 0070 forbids it in comments, and a diagnostic is worse |
| Defer `Adt`→`ADT` and `Eos`→`EOS` casing churn | 5 | agreed: not worth the blame churn on its own |
| ADR 0071's implementation comments cite "ADR 0071" | 6 | violates the CARE contract RFC 0070 established. Rewrite as present-tense contracts before 0071 closes |

## Items from the OpenCode audit

| Item | Stage | Note |
|---|---|---|
| Package-level `PtrType`/`MutPtrType` (`types/types.go:469-476`) construct a throwaway `NewEnvironment()` **per call** | 2 | allocation waste on a hot path; fold into the R20 arena work |
| Add one `recover()` at the `GenerateChecked` boundary converting a panic to `Unknown Error` | 4 | makes fail-closed **architectural** rather than per-site. Complements RFC 0073's panic removals: the sites go away, and the boundary guarantees nothing new reintroduces one. There is no `recover()` anywhere in the tree today |
| Table-ify `computeHeaderRequirements` (`emission.go:319`) | 5 | a switch that is really a table |
| Parameter-order aberrations at `bitwise.go:126`, `checker/adt.go:13` | 5 | inconsistent with their siblings |
| Dead exports: `parser.Parser` (zero-value only), `lexer.TokenKind.String` (test-only), `types.RemoveUnionMember` (test-only) | 1 / 5 | joins R2 and R16; verify test callers separately as R2 requires |
| `TestModuleGenerationDeterministic` (`modules_generation_test.go:129-144`) compares only **first-run** keys | 6 | a second-run-only extra artifact passes. Make it bidirectional; the SHA-256 manifest already is |
| Diagnostic tie-break is `(Line, Column)` only (`compile.go:209,372`) | 4 | deterministic today by append order; adding category and stage makes the contract explicit rather than incidental |
| `mergeDiagnostics` wraps a plain `error` with empty `Stage` and `0:0` (`compile.go:363`) | 4 | pairs with RFC 0073 D29 — an empty category then renders as `Unknown Error` |
| `foldNumericConversion` and `foldIntegerConversion` (`checker/conversions.go:160,197`) return `*Diagnostic` while the package flows diagnostics by value | 4 | two sites, not three as first reported; verified |
| `components.go:37,47,51,78` panic on embedded-template read and parse failure | 4 | four more sites for R10's panic sweep. These fire at process start on a corrupt binary rather than on user input, so they are the weakest case for conversion — but the `recover()` boundary covers them for free |
| `lexer.Lex` (401 lines) has near-duplicate Byte and Rune literal scanning (`lexer.go:486`, `:518`) | 5 | one scanner parameterised by the closing quote and escape set |
| Keep `discoverModuleEmission` and `emitModulePair` as single delegates | 5 | **do not split** — OpenCode flags this explicitly and it agrees with this RFC's decision to leave `emission.go` alone until ADR 0071 settles |
| `slices.Sorted(maps.Keys(...))` exclusions: `compile.go:319` (filters while collecting) and `workbench/snippets/catalog.go:80` (`ReadDir`, already ordered) | 3 | named so the mechanical sweep does not over-apply |

## Remaining items from the Codex audit

| Item | Stage | Note |
|---|---|---|
| **Node-coverage invariant test** — enumerate every parser statement and expression node and prove the checker, generator preflight, walker, and renderer each handle or explicitly reject it | 6 | The single highest-value test in any of the three audits. RFC 0073 contains **four** separate missing-dispatch defects (D1 checker top-level, D27 generator preflight ×2, D8 walker default). One table-driven test makes that whole class impossible to reintroduce, and it is the only proposal here that prevents rather than finds |
| `reference.md` drift: root-module-C versus `hexal/runtime.c` trap ownership | 4 | ADR 0071 moved trap definition; the reference may still describe the old owner. Verify during 0071 closure |
| `reference.md` drift: deterministic-timing claims | 4 | counts are deterministic, wall-clock statistics cannot be. Any reference wording implying reproducible durations is wrong |
| `CompilationStats` determinism contract | 4 | states which fields are reproducible (`SourceLines`, `TokenCount`) and which are not (every duration). Pairs with RFC 0073 D7 |
| Final conformance gate additions: manual GCC **and** Clang builds of a generated project, sanitizer runs, runtime trap/output checks, and race-enabled compiler calls | 6 | extends R18's compile-gate decision. `-race` on `compiler.Compile` is worth doing regardless: RFC 0074 R20 notes a process-global mutex-guarded type cache, and nothing exercises it concurrently today |

### Where Codex disagrees on file splitting

Recorded because it directly contradicts R13 above.

R13 splits `validateExpressionNode` (876 lines) and
`renderExpressionUncheckedWithState` (665 lines) into the existing family files.
**Codex recommends deferring both** — its stated rationale was "until expression
family dispatch is designed", a concept no spec defines; read it as "until the
lower-risk splits have validated the approach", which is what the resolution
below actually schedules. Codex splits `types.go` (1,184),
`checker/operator_checking.go` (1,122),
`checker/generics.go` (1,186), `parser_test.go` (1,109), and
`generator_test.go` (2,139) instead.

Both positions have merit and they are not mutually exclusive:

- Codex is right that the two expression giants are the *riskiest* split, since
  each case carries validation or lowering semantics.
- R13 is right that the seams already exist — 25 of the render switch's cases
  are already one-line delegates, so the fat cases are stragglers rather than a
  new architecture.

**Resolution, in three ordered steps** — R13's exclusion of `types.go` and
`generics.go` is a *sequencing* constraint, not a permanent one, and stating the
order removes the apparent contradiction:

1. **Now:** split `operator_checking.go`, `parser_test.go`, and
   `generator_test.go`. No open spec touches them.
2. **After RFC 0073's D19/D25 lands:** split `types.go` and `generics.go`. The
   canonical-key work rewrites both, so splitting first guarantees a merge
   conflict.
3. **After both:** reassess R13's two expression giants with the approach
   already validated on five lower-risk files.

### Contradicted — do not act on these

Two OpenCode consolidation suggestions conflict with evidence recorded elsewhere
in this RFC. Both are traps:

- **Do not merge `assertCompiles` in `c23_harness_test.go:22`** into the shared
  helper. ADR 0054 introduced it as a *deliberate* private copy so the dormant
  C23 package stays independent of `helpers_test.go`. OpenCode flags this
  correctly; recording it here so a later consolidation pass does not undo it.
- **Keep the `alloc.go:45` parameter group** out of R14's bundling. The callee
  token varies across its call sites, so a struct would fix what is genuinely
  per-call. OpenCode reached the same conclusion independently.

## Resolved audit disagreements

**Line endings — settled, not a trade-off.** `gofmt` emits LF unconditionally, so
a CRLF `.go` file is unformatted by definition and there is no Windows-convention
alternative for Go source. Add one line to `.gitattributes`:

```gitattributes
*.go text eol=lf
```

The working tree then holds LF for Go files on every platform and `gofmt -l`
stays silent, while everything else keeps `text=auto` — CRLF in the working tree
for Markdown, JSON, and text is correct on Windows and invisible to git because
the index is already LF.

Both audit positions were partly right and neither reached this. Do **not**
renormalize the index; it is already LF, so that produces a whole-file diff
carrying no change. Recorded in `AGENTS.md`.

**Performance sequencing — both, in order.** RFC 0075 adds the committed
benchmark suite. R6–R8 below stand on measurements already taken and do not wait
on it, but if the two are scheduled together land 0075 first so Stage 2's gain is
recorded as a before/after ratio rather than asserted.

**Discovery-walk consolidation — hoist now, fuse only with data.** Both audits
found ~17–21 generator discovery walks over the same program. R7 takes the
allocation win without restructuring; fusing stays deferred until RFC 0075's
`BenchmarkCollections` and `BenchmarkCorpus` can price it. See R7 for what each
option actually means.

## Non-goals

- Any behavior change. RFC 0073 owns every defect.
- Re-splitting the checker or generator packages.
- Fusing the 21 discovery walks.
- Speculative optimization beyond the three measured hot spots.
- Renaming files for singular/plural consistency.
- Merging `assertRejects` with `requireRejected` without per-test review.
- Deleting `checker.Check`, the live `Is*` predicates, or component render-model
  fields consumed by templates.
