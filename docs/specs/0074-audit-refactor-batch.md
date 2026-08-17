# RFC 0074: Audit Refactor Batch

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready by stage
- Created: 2026-08-16
- Scope: behavior-preserving cleanup found by the six-pass refactor audit —
  deletion, consolidation, structure, and one measured performance batch
- Coordinates with: RFC 0073 (defect batch), `AGENTS.md`, `docs/reference.md`,
  `docs/status.md`
- Companion: RFC 0073 carries every correctness defect from the same audit

## Summary

Roughly **1,900 deletable lines** and three measured allocation hot spots worth
**33% of compiler allocations**, none of which changes observable behavior.

Every item is behavior-preserving. Anything that changes what the compiler
accepts, rejects, or emits belongs to RFC 0073 and is out of scope here.

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

## Stage 1 — Deletion (~1,900 lines, zero behavior change)

### R1. Drained transition seams from the component migration — ~89 lines, 12 files

Eight `*FamilyContent` functions all `return ""`:
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
| R6 | `compile.go:84` re-lexes every reachable module a second time solely to populate `Stats.TokenCount`; `reachableModules` already lexed it at `:74` | **4.53 MB, 4.3%** — pure waste | Return token counts from `reachableModules` |
| R7 | `generator.walkStatementExpressions` (`walk.go:144-180`) allocates two mutually-recursive Go closures **per statement**, invoked by 21 independent `walkProgram` discovery passes | **19.04 MB, 18.0%**; `discoverModuleEmission` is 17.2% of CPU | Convert the closures to package-level functions taking `visit` — see below |
| R8 | `types.unionMembers` (`unions.go:145-153`) does `append([]Type(nil), …)` on every call; read-only callers copy for nothing — `ContainsUnionMember` (`:236`), `RemoveUnionMember` (`:248` and `:250`, twice per call) | **11.00 MB, 10.4%** | Iterate `typ.Union.Members` directly in read-only callers |

R6 disappears together with defect D13 in RFC 0073 — the loop that drops the
lexer error is the same loop.

### R7 in detail

This concerns **Go closures in the compiler's own source**. Hexal has no
closures; nothing about the language changes.

`walkStatementExpressions` declares two function values as local variables. They
are mutually recursive — `walkExpression` calls `walkOperand` and vice versa —
and both capture `visit`, so Go allocates both on the heap on **every call**:

```go
func walkStatementExpressions(statement checker.Statement, visit func(*checker.Expression) error) error {
	if visit == nil {
		return nil
	}
	var walkExpression func(*checker.Expression) error   // ← allocated per call
	var walkOperand func(*checker.Operand) error         // ← allocated per call

	walkExpression = func(node *checker.Expression) error {
		if err := visit(node); err != nil {              // captures visit
			return err
		}
		...
		for index := range node.Arguments {
			if err := walkOperand(&node.Arguments[index]); err != nil {   // captures walkOperand
				return err
			}
		}
		return nil
	}
	walkOperand = func(source *checker.Operand) error {
		if source.Node.Kind != checker.InvalidExpression {
			return walkExpression(&source.Node)          // captures walkExpression
		}
		return nil
	}

	switch statement := statement.(type) { /* ... */ }
}
```

The function is called once per statement, and 21 independent discovery passes
each walk the whole program — so the allocation is multiplied by statements ×
passes. That is the 19.04 MB.

**The fix ("hoisting") is moving them out to package level and passing `visit`
as an ordinary parameter.** Nothing is captured, so nothing is allocated:

```go
func walkStatementExpressions(statement checker.Statement, visit func(*checker.Expression) error) error {
	if visit == nil {
		return nil
	}
	switch statement := statement.(type) { /* ... calls walkExpressionTree(node, visit) ... */ }
}

func walkExpressionTree(node *checker.Expression, visit func(*checker.Expression) error) error {
	if err := visit(node); err != nil {
		return err
	}
	if node.Operand != nil {
		if err := walkExpressionTree(node.Operand, visit); err != nil {
			return err
		}
	}
	// Left, Right identically
	for index := range node.Arguments {
		if err := walkOperandTree(&node.Arguments[index], visit); err != nil {
			return err
		}
	}
	return nil
}

func walkOperandTree(source *checker.Operand, visit func(*checker.Expression) error) error {
	if source.Node.Kind != checker.InvalidExpression {
		return walkExpressionTree(&source.Node, visit)
	}
	return nil
}
```

Identical traversal, identical order, zero closures allocated. `visit` is a
function value that already exists at the call site and is merely passed along.

Sequence this **after** RFC 0073's D8, which adds the missing fail-closed
`default:` to this same switch. Add the guard first, then simplify around it.

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

### R10. Drop `error` from `walkProgram` and `programVisitor`

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

| Function | Now | Extract to |
|---|---|---|
| `validateExpressionNode` (`validation.go:654-1529`) | 876 lines, 57 cases | collections → `arrays.go`; strings → `strings.go`; concurrency → `concurrency.go`; view bridge → `views.go`. Leaves ~380 |
| `renderExpressionUncheckedWithState` (`render.go:677-1342`) | 665 lines, 53 cases | collections → `arrays.go`; strings → `strings.go`; view bridge → `views.go`. Leaves ~290 |

This is not a new convention: 25 of the render switch's cases
(`render.go:1189-1215`) are already one-line delegates. The fat cases are the
stragglers. Zero new files.

Do **not** re-split the packages. Median file size is 190 (checker) and 130
(generator) lines; only these two files exceed 1,500.

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

1. **Nothing verifies that generated C compiles.** By policy no test invokes a
   toolchain, and `compiler/tests/c23validation/` has no runnable entries.
   RFC 0073's defect D2 emits C referencing an undeclared type and passes
   `go test ./...`, `go vet ./...`, and `go vet -tags c23` — all three ran clean
   against it. This is the single highest-value gap in the project and it now
   has a concrete escaped defect to justify it. Options: a manual validation
   pass, a tagged opt-in gate, or ownership by RFC 0055's driver. Decide
   deliberately.
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
| 2 — performance | RFC 0073 D13 (R6 shares its loop) | unchanged |
| 4 — diagnostics | RFC 0073 D8 (R10 needs the fail-closed arm first) | unchanged |

Stages 1, 3, 5, and 6 are independent and may run in parallel or any order.
Stages 2 and 4 each have one ordering dependency on RFC 0073.

Land RFC 0073 first regardless. Fixing defects on a moving codebase is harder
than refactoring a correct one, and two of its items gate stages here.

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

Contributed by the external Codex audit. These are larger than the preceding
stages and several are the structural form of defects RFC 0073 fixes tactically.
**Each needs its own bounded spec; this stage records the shape, not the plan.**

### R19. One authoritative module graph

Build an immutable `moduleGraph` during reachability holding, per module: the
canonical ID, the exact logical source key, the parsed program, resolved ordered
imports, and dependency state. The checker then never re-resolves a path.

This subsumes the duplicate import-path resolution recorded earlier in this RFC
(`compile.go:132` validating, `checker/modules.go:542` silently mirroring it),
the three per-module alias tables, and the four-times-duplicated
canonical-id-to-logical-key lookup. It is also the natural home for the module
field RFC 0074 R11 adds to `Diagnostic`.

### R20. Per-compilation type arena

`globalUnionTypes` is a process-global, mutex-guarded cache keyed by never-reset
identity serials (`compiler/types/unions.go:113-126`, `types.go:33`). It grows
unboundedly across `Compile` calls in a long-running workbench.

Keep immutable builtins process-wide; move constructed types and unions into a
per-compilation arena shared by module environments. Centralize protected
builtin names and constructor classification while doing so.

Note the audit verified this is **not** a determinism or correctness problem
today — 300 compiles produced byte-identical output. It is a retention and
isolation concern.

### R21. Canonical type key

The recursive, module-qualified type key that RFC 0073 D19 requires. **The key
itself must land with D19**, because a same-named-type collision is a live
miscompile. What belongs here is the follow-through: routing every remaining
identity consumer through one key, and deleting the display-name-derived paths
that remain.

### R22. Literal registry

A non-nil `literalRegistry` with `Intern`, `Lookup`, ordered iteration, stable
IDs, C-name lookup, and deterministic merge. This is the structural fix for RFC
0073's D9 and D10, whose silent fallbacks exist only because lookup can miss.

Codex also notes parser expression-start classification omits Byte and Rune in
return recovery — same class of scattered classification, worth folding in.

### R23. Package boundary

Consider moving compiler stages under `compiler/internal/`. External consumers
use only `compiler.Compile`, so the current exported surface of `lexer`,
`parser`, `checker`, `generator`, and `types` is broader than anything depends
on. This makes R16's unexporting decisions enforceable rather than advisory.

### R24. Ownership and lifetime — separate RFC required

`h.free(ref stackLocal)` compiles (verified). So do double-free, use-after-free,
and freeing a String literal. Per `docs/reference.md` the last three are **by
design** — there is no compiler-enforced exactly-once cleanup. The first is a
category error rather than a lifetime error and has no specified enforcement.

`AGENTS.md` goal 18 states "if it compiles, it has no memory issues";
`docs/reference.md` explicitly disclaims compiler-enforced cleanup. **Those two
cannot both stand**, and the contradiction has to be resolved before any
enforcement work is scoped. That resolution is the first deliverable of the
ownership RFC, not of this one.

## Additional items from the external audit

Folded into the stages above where they fit; listed here where they are new.

| Item | Stage | Note |
|---|---|---|
| `resolveType` one-caller compatibility wrapper | 1 | verify caller count before removing |
| `.at()`, collection `.is_empty()`, `.addr` migration branches | 1 | these are RFC 0063's tailored removal diagnostics. Decide once whether exact-diagnostic migration help is still wanted, then remove all three consistently or keep all three |
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
