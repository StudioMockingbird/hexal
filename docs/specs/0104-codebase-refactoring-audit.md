# RFC 0104: Codebase Refactoring Audit

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; findings catalog for discussion. Nothing here is committed —
  each finding carries a recommendation, and the Disposition table records the
  proposed order. Every item awaits an explicit accept/reject decision before
  implementation; behavioral items then get their own ordinary defect/spec
  treatment.
- Created: 2026-08-21
- Scope: the Go compiler implementation under `compiler/` and `workbench/` —
  hygiene, dead code, API surface, determinism, diagnostics, error handling,
  naming, file structure, signatures, architecture invariants, tests,
  documentation synchronization, and measured performance. Language syntax,
  semantics, and builtin APIs are covered by RFC 0103 and out of scope here.
- Coordinates with: `AGENTS.md`, `docs/reference.md`, `docs/status.md`,
  RFC 0103 (language surface), archived RFCs 0073/0074 (prior defect/refactor
  batches)
- Companion: none. Successor work cites finding numbers from this RFC.

## Summary

Forty-two findings from seven parallel audit passes over the full codebase,
plus a recorded conformance and benchmark baseline. The headline: the
architecture invariants the project worries about most — import-resolution
single-sourcing, literal-registry ownership, builtin interning, ownership/
cleanup enforcement, determinism of output, phase-dispatch fail-closedness —
were probed hard and **verified healthy**. The actionable residue clusters
elsewhere: two diagnostic/error contract breaches, a dozen provably-dead
leftovers, ~30 sites bypassing the checker's own diagnostic builders, two
dominant parameter groups, three mixed-responsibility files, and a measured
performance profile whose top four mechanical fixes account for roughly 35%
of allocated bytes.

Line numbers in this RFC are advisory and will drift; the verbatim quotations
are the checkable part (see Validation).

## Method

Seven read-only passes, run in parallel: (1) hygiene/dead-code/legacy-API/
stdlib modernization; (2) determinism + phase-dispatch coverage; (3)
diagnostics + error-handling unification; (4) import resolution + literal
registry + builtin interning + ownership/cleanup implementation; (5) test
helpers + reference synchronization; (6) allocation/hot-path profiling with
benchmarks executed; (7) file structure/naming/signatures/exported-API docs.
Every claim below was required to carry file:line evidence and a verbatim
quote; claims that could not be fully verified are marked UNVERIFIED with the
probe that would settle them.

## Baseline record (pass 20)

Recorded 2026-08-21, before any refactor work:

- `go test ./...` fully green (all eleven packages).
- `gofmt -l` empty; `.go` files LF-only; `.gitattributes` pins `*.go text eol=lf`.
- `go vet ./...` clean; `go vet -tags c23 ./...` clean.
- Benchmarks (benchtime=1x, windows/amd64):

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| Scalar | 1,162,200 | 192,192 | 761 |
| GenericsHeavy | 2,302,300 | 785,120 | 3,896 |
| MultiModule | 2,125,000 | 389,552 | 3,611 |
| Collections | 2,259,700 | 535,304 | 2,099 |
| Text | 1,798,000 | 296,416 | 1,110 |
| Concurrency | 3,160,600 | 692,432 | 2,737 |
| ErrorPaths | 2,400,800 | 396,672 | 2,459 |
| Corpus (98 snippets) | 85,180,000 | 23,497,640 | 121,101 |

Traversal counts (`-tags benchmetrics`): Scalar 25 walks/op; MultiModule 200;
Corpus 3,425 walks/op against 47,300 nodes/op. Clean-corpus CPU profile:
GC/scheduler ≈ 40% of samples — allocation churn dominates.

Any performance finding implemented below must re-run this table before and
after; a change without a before/after pair is not accepted as a perf fix.

## Severity ordering

| # | Finding | Class | Effort | Risk |
|---|---|---|---|---|
| **R1** | **Generator panics crash Compile instead of returning Unknown Error** | contract breach | M | M |
| **R2** | **Bare `fmt.Errorf` reaches Stderr with no diagnostic class** | contract breach | S | L |
| R3 | Dead `expectedUses` built then discarded | dead code | S | L |
| R4 | Stale `var _ =` keep-alives + orphaned comment | dead code | S | L |
| R5 | Write-only `typeIdentity.serial`/`.parent` + global counter | dead code / determinism trap | S | L |
| R6 | Dead `headerLiteral` field with hidden Intern side effect | dead code / generated C | S | M |
| R7 | Generator duplicates checker's bit-cast eligibility predicate | dead code / drift risk | S | M |
| R8 | `NewEnvironmentWithOwner` exported, zero external callers | legacy API | S | L |
| R9 | `snippets.Validate`/`LineLimitWarnings` exported for tests only | legacy API | S | L |
| R10 | ~15 checker sites bypass the four exclusive diagnostic builders | diagnostics | S | L |
| R11 | Position-less user-facing Type Error | diagnostics | S | L |
| R12 | Compiler-bug defaults classified as Type Error | diagnostics | S | L |
| R13 | Generic-conflict message always names parameter 0 | diagnostics | S | L |
| R14 | RFC citation leaked into user-facing diagnostic | diagnostics | S | L |
| R15 | Lexer helper plus ten inline copies of itself | diagnostics | S | L |
| R16 | Fail-closed fallback diagnostic carries no position | diagnostics/dispatch | S | M |
| R17 | `stampModule` duplicated across packages; comparator duplicated in-file | error handling | S | M |
| R18 | Dropped `ok` on `importTarget` — latent fail-open | error handling | S | M |
| R19 | Failed statements pollute flow facts for later diagnostics | error handling (cosmetic) | S | L |
| R20 | Stats fold iterates module map instead of order slice | determinism hygiene | S | L |
| R21 | Kind↔render↔validate coverage matrix unproven | dispatch test gap | M | M |
| R22 | `render.go` mixes five responsibilities | file structure | M | M |
| R23 | `validation.go` embeds an extractable constant-validation cluster | file structure | S-M | M |
| R24 | `types/types.go` is the package everything-file | file structure | M | M |
| R25 | Checker `(names, typeEnvironment)` parameter group recurs 20+ times | signatures | M | M |
| R26 | Generator definition-writing group: 10-parameter signatures | signatures | M | M |
| R27 | Nil-registry `expressionValidation{}` helpers — panic path | latent trap | S | M |
| R28 | Generator forges fresh-identity types compared against interned ones | latent trap | S | M |
| R29 | Per-module BindingID counters meet in specializations | latent trap probe | S | M |
| R30 | Statement structs heap-boxed per hoist pass — 13.6% of bytes | performance | M | M |
| R31 | `text/template` executes component templates per compilation — 10.5% | performance | L | M |
| R32 | `walkProgram` allocates closures+maps per call — ~17% of allocs | performance | S-M | M |
| R33 | `unionMembers` defensive-copies on every call — 4.9% | performance | S | L |
| R34 | `EncodeModuleOwner` recomputed per tag derivation | performance | S | L |
| R35 | Per-module environments copy the builtin seed map — 6.0% | performance | S | M |
| R36 | `collectionCNameTaken` O(n²) scan | performance | S | L |
| R37 | 22 separate discovery walks per compilation | performance (structural) | L | M |
| R38 | `walk_test.go` re-implements the pipeline twice intra-package | tests | S | L |
| R39 | Invalid-identifier table copy-pasted across three tests | tests | S | L |
| R40 | `assertGeneratedC` used by one file amid ~150 hand-rolled loops | tests | M | M |
| R41 | `strings.Count(x,y) != 1` hand-written ~50 times, no helper | tests | M | L |
| R42 | Helper-family C spellings load-bearing in tests, unnamed in reference.md | doc sync | S | L |

## Findings

### Contract breaches

#### R1 — Generator panics crash Compile instead of returning Unknown Error

Evidence: `panic(err)` at `compiler/generator/concurrency.go:133`,
`print.go:81`, `heap.go:25`, `tags.go:122`, `unions.go:72`, `walk.go:446`
("Unknown Error: generator walker cannot visit statement of type %T"). No
`recover()` exists anywhere in non-test compiler code.

Why it matters: the fail-closed contract says an unclassifiable compiler
inconsistency surfaces as `[Unknown Error]` through `CompilationResult.Stderr`.
Today an internal invariant break aborts the process with a Go panic instead.

Action: thread `error` out of the visitor callbacks, or recover at
`GenerateChecked`'s boundary and convert to `unknownExpressionDiagnostic`.

#### R2 — Bare `fmt.Errorf` reaches Stderr with no diagnostic class

Evidence: `compiler/generator/generator.go:37,66,86`
(`fmt.Errorf("generator: duplicate generated artifact key %s", key)`) and
`components.go:68,73,78`; these flow through `types.go:253`
(`return []string{err.Error()}`) onto stderr as raw prose.

Why it matters: every diagnostic must belong to one externally visible class;
these lines have none, and consumers cannot distinguish them from user errors.

Action: construct them as `Diagnostic{Category: UnknownError, Stage:
"generator", …}` like the existing `unknownExpressionDiagnostic` path.

### Dead code

#### R3 — Dead `expectedUses` built then discarded

Evidence: `compiler/checker/generics.go:580`
`expectedUses := make([]compilerTypes.TypeUse, 0, …)`; `:587` append; `:608`
`_ = expectedUses`. Only `use.Type` is consumed.

Action: delete the variable, its appends, and the discard.

#### R4 — Stale keep-alives and orphaned comment

Evidence: `compiler/generator/bitwise.go:310-313` — a comment describing
`endianEligible` (a function that does not exist in the generator) followed
only by `var _ = strings.TrimPrefix`; `compiler/generator/defer.go:335`
`var _ = compilerTypes.Type{}`. Both imports remain live through other uses,
so the suppressions protect nothing.

Action: delete both `var _ =` lines and the orphaned comment.

#### R5 — Write-only identity fields and a package-global counter

Evidence: `compiler/types/types.go:31-32` fields `serial`, `parent` on
`typeIdentity`; sole write at `:41`; repo-wide grep shows `.serial` never
read. `typeSerialCounter` (`:37-41`) increments forever across repeated
in-process `Compile` calls. Identity is pointer-keyed everywhere.

Why it matters: dead today, but the first future consumer of `serial`
(ordering, tie-breaks) gets process-history-dependent output — a silent
byte-reproducibility violation waiting to happen.

Action: delete both fields and the counter; identity remains pointer-based.

#### R6 — Dead field with a hidden generated-C side effect

Evidence: `compiler/generator/concurrency.go:56` `headerLiteral
literalHandle`; assigned at `:207` (`literals.Intern("Scheduler")`), never
read (sibling `fileLiteral` is read at `:278`). UNVERIFIED sub-claim: whether
the registry emits every interned literal regardless of handle usage.

Why it matters: removing it likely drops an unused `static const hex_string`
from generated C — a legitimate output change that must go through the
snippet-manifest rebuild process, never by hand-editing hashes.

Action: delete field and assignment; rebuild the manifest via the temporary-
test procedure; review the diff (expected: fewer emitted literals).

#### R7 — Duplicated eligibility predicate across phases

Evidence: `compiler/generator/bitwise.go:298` `bitCastEligible` is
byte-for-byte identical to `compiler/checker/bitwise.go:14`
`bitCastEligibleType` (ten-type switch). The generator already imports the
checker package.

Why it matters: two copies of a check-then-generate invariant can drift; a
mismatch turns a checked program into an `unknownExpressionDiagnostic`.

Action: export `BitCastEligibleType` from checker; delete the generator copy.

### Legacy API

#### R8 — Exported constructor with zero external callers

Evidence: `compiler/types/types.go:313` `NewEnvironmentWithOwner`; sole caller
is `NewEnvironment` at `:306`, same package. Repo-wide grep including tests
and workbench finds no other use.

Action: unexport to `newEnvironmentWithOwner`.

#### R9 — Exports existing only for their own external tests

Evidence: `workbench/snippets/catalog.go:113` `Validate` (non-test caller
only `catalog.go:106`, same package) and `catalog.go:165`
`LineLimitWarnings` (sole caller `compile_test.go:67`); tests live in
`package snippets_test`.

Action: unexport both and move the two tests into `package snippets`; or
accept as-is if external-test access is wanted.

### Diagnostics

#### R10 — The checker bypasses its own exclusive builders

Evidence: `compiler/checker/scope.go:721-723` states every checker diagnostic
is built by one of four helpers and "a category is never spelled at a call
site." Approximately fifteen composite-literal sites spell categories
directly: `generics.go:160,233,391,681,774,906,998`, `calls.go:143`,
`adt.go:140`, `expressions.go:258`, `places.go:186`,
`operator_checking.go:508,898`, `functions.go:136`, `methods.go:182`,
`starvation.go:165`, `modules.go:383`, `checker.go:483`.

Why it matters: two mechanisms for one job guarantee drift — R11, R12, and
R13 were all born at these sites.

Action: route every site through `typeErrorAt`/`nameErrorAt`/
`moduleErrorAt`/`unknownAt`; then the contract comment becomes true.

#### R11 — Position-less Type Error

Evidence: `compiler/checker/adt.go:140-144` builds
`Category: TypeError, Message: "cannot infer generic parameter for %s"` with
no Line/Column; renders without `at l:c`.

Action: take the declaration-name token callers already hold and use
`typeErrorAt`.

#### R12 — Compiler bugs labeled as user Type Errors

Evidence: defaults at `compiler/checker/expressions.go:258`
("unsupported expression"), `places.go:186` ("unsupported place"),
`operator_checking.go:898` — all `TypeError`, all position-less. All parser
expression kinds are covered upstream, so these defaults fire only on
compiler inconsistency. Sibling code classifies identical defects correctly:
`walk.go:446` spells Unknown Error.

Why it matters: a checker bug would blame the user's program instead of
reporting `[Unknown Error]`.

Action: reclassify to `unknownAt(token, …)`.

#### R13 — Conflict message names the wrong generic parameter

Evidence: `compiler/checker/generics.go:596-598` loops over `index` but the
message hardcodes `open.Parameters[0].Lexeme`: "conflicting inferred types
for generic parameter %s".

Action: report `open.Parameters[index]` or the failing placeholder.

#### R14 — Provenance leaked into a user-facing message

Evidence: `compiler/parser/type_expressions.go:316` "ADT payload fields
cannot be mutable in this RFC." "This RFC" is meaningless to users and
violates the repo rule against citing internal RFCs in user-visible text.

Action: drop the clause.

#### R15 — Helper plus ten inline copies in one file

Evidence: `compiler/lexer/lexer.go:982-983` defines `literalDiagnostic`;
lines 442, 469, 482, 501, 514, 668, 677, 739, 748, 791 each re-expand
`Diagnostic{Category: SyntaxError, Stage: "lexer", …}` inline.

Action: replace inline literals with the helper (add a value-returning
variant if positions differ).

#### R16 — Position-less fail-closed fallback

Evidence: `compiler/checker/checker.go:482-487` default branch emits
`Diagnostic{Category: UnknownError, Stage: "checker", Message: "unsupported
top-level item"}` with no Line/Column. Currently unreachable (all sixteen
top-level item forms are cased), but every sibling phase default stamps a
position.

Action: stamp the offending item's first token position.

### Error-handling unification

#### R17 — Module stamping duplicated across packages

Evidence: `compiler/compile.go:496-508` and
`compiler/generator/generator.go:96-108` contain identical `stampModule`
implementations; the diagnostic position comparator appears twice inside
compile.go itself (`:244-258`, `:448-462`).

Why it matters: propagation semantics can fork between stages unnoticed.

Action: export one `StampModule` and one comparator from `compiler/types`.

#### R18 — Dropped ok on import lookup

Evidence: `compiler/checker/checker.go:478`
`target, _ := registry.importTarget(moduleID, statement.Alias.Lexeme)` binds
`moduleID: target`; `importTarget` (`modules.go:273`) legitimately returns
`("", false)`.

Why it matters: if the invariant ever breaks, the alias silently binds an
empty module id instead of diagnosing — fail-open.

Action: on `!ok`, append an `unknownAt` diagnostic instead of binding.

#### R19 — Flow facts leak past failed statements

Evidence: `compiler/checker/control_flow.go:62-64` rolls back `returnFlows`
on a failed statement, but `names.flow` mutations made before the failure was
detected are not rolled back.

Why it matters: can only add spurious diagnostics to already-failing
compilations, never wrong acceptance — cosmetic, fail-closed direction.

Action: clone-before-check per statement if diagnostic precision ever
matters; otherwise accept and record.

### Determinism

#### R20 — Stats fold iterates the map, not the order slice

Evidence: `compiler/compile.go:107` `for _, node := range graph.Modules`
sums TokenCount/SourceLines. Safe today only because `+=` commutes; every
other fold in the pipeline deliberately iterates `graph.Order`.

Action: iterate `graph.Order` and index into the map. One line.

Verified clean elsewhere (probed, no action): merge folds iterate Order; all
output-reaching maps render via `slices.Sorted(maps.Keys(…))`; unstable sorts
run only over tie-free keys; tag collisions resolve after canonical sort;
diagnostics sort stably by position; no goroutines or shared mutable state in
the pipeline.

### Phase dispatch

#### R21 — The kind-coverage matrix is unproven

Evidence: the checker defines ~50 expression kinds
(`checker/operands.go:34-207`); `generator/render.go:678-955` and
`generator/validation.go:703-1086` each case a large subset with fail-closed
defaults. Sampling covered ~20 kinds across both switches; the full
three-way matrix (kind ↔ render case ↔ validate case) is UNVERIFIED.

Why it matters: both dispatchers fail closed, so a gap manifests as a valid
program rejected with "unsupported checked expression" — invisible to a suite
that asserts success or diagnostic text, never compiled C.

Action: one table-driven unit test enumerating every Kind constant against
both switches' case lists, failing on any kind absent from either.

### File structure

#### R22 — render.go mixes five responsibilities

Evidence: function inventory of `compiler/generator/render.go` (1801 lines):
statement rendering (`writeStatements*`, `renderCallStatement`,
`renderReturnStatement`); expression dispatch and operations
(`renderExpression*`, `renderOperation*`, ring-rendering family); C
declaration spelling utilities (`declaration`, `typeSpelling`,
`pointerSpelling`, `funDeclaration`, `qualifyLastPointer`); binding-state
methods on `expressionValidation` (`pushScope`…`cNameFor`); literal
formatting (`integerLiteral`, `formatInteger`, `formatDecimalFloat`,
`signedMinimumMacro`).

Action: extract c_spelling.go, bindings.go, and literals_format.go; render.go
retains statement/expression rendering. Behavior-neutral; snippet manifest
must not move.

#### R23 — validation.go embeds an extractable cluster

Evidence: `compiler/generator/validation.go` (1701 lines) mixes
program/function/statement validation with self-contained constant/literal
validation: `validateConstantOperand`, `validateFloatConstant`,
`floatSignAndSpecial`, `floatBitsForConstant`, `validateIntegerConstant`,
`parseIntegerLiteral`.

Action: extract constants_validation.go (~200 lines).

#### R24 — types.go is the package everything-file

Evidence: `compiler/types/types.go` (1293 lines) holds Diagnostics +
`ErrorMessages` + module-owner encoding + Environment + Is* predicates + the
canonicality recursion (`isCanonical*`, ~250 lines) + scalar constructors.

Action: split diagnostics.go, canonical.go; predicates may follow. Package
boundaries unchanged.

Explicitly not proposed: splitting `emission.go` (1052) — its inventory is
cohesive around the emission pipeline.

### Parameter bundling

#### R25 — Checker context group recurs 20+ times

Evidence: `(names *scope, typeEnvironment *compilerTypes.Environment)` —
usually with `token lexer.Token` — recurs across `checker/adt.go:110,269`,
`arrays.go:78`, `calls.go:190,267`, `control_flow.go:88,148`,
`dicts.go:167`, `functions.go:211`, `generics.go:186,221,387,478,678,771,809,
860,986`, `lists.go:176`, `methods.go:481,552`.

Action: introduce one `checkContext` struct carrying scope, environment, and
current token; migrate mechanically, signature by signature.

#### R26 — Generator definition-writing group

Evidence: `writeFunctionDefinition` (declarations.go:157) takes ten
parameters; `(body *strings.Builder, declared, functions, methods,
typeState, stringState, owner, logicalKey, tags)` recurs across
`writeMethodDefinition` (:235) and `writeSpecializedDefinitions` (:452).
Tertiary group `(body, state, result, inFunction)` recurs four times across
for.go/render.go statement rendering.

Action: introduce `renderContext` for the definition-writing family; consider
the tertiary group only if it survives R22's splits.

Ordering note: do R25/R26 before R22-R24 — the bundles define the seams the
file splits should follow.

### Latent traps

#### R27 — Helpers construct registry-less render state

Evidence: `generator/render.go:663` and `:1562` build
`&expressionValidation{}` without setting `strings *literalRegistry`; a
String literal reaching them panics at `strings.go:42`. All current callers
are tests.

Action: give the two helpers a required registry parameter, or route through
a constructor that demands one — converting the panic path into a compile-
time requirement.

#### R28 — Forged types compared against interned types

Evidence: `generator/render.go:1517` and `validation.go:1240-1243` call
package-level `compilerTypes.PtrType(...)` (fresh environment, fresh
identity) and feed the result into `generatedAssignable` →
`compilerTypes.Assignable`, which is identity-first but carries a structural
Element-based fallback for pointers (`types/types.go:779-791`).

Why it matters: correct today only through the fallback; any future
Equal-first comparison of a forged type fails silently.

Action: comment the forgery sites as metadata-only, or thread the module
environment into generator state so real interned pointers are constructed.

#### R29 — Cross-module BindingID meeting — UNVERIFIED

Evidence: `scope.go:53-54` numbers bindings per module scope;
`specializeFunctionIn` runs in the requesting module's scope
(`generics.go:384-386,450`) yet emits into the defining module's artifacts
(`modules.go:235-242`). Sampled consumers look per-function, but not every
BindingID consumer was traced.

Action: one targeted probe — compile a two-module generic case and diff
emitted binding names for collisions — before considering this closed.

### Performance (measured)

All percentages are of BenchmarkCorpus allocated bytes (23.5MB/op) unless
stated. Re-benchmark before/after each fix; see Baseline record.

#### R30 — Statement structs heap-boxed per hoist pass

Evidence: `generator/walk.go:166-172` takes `&statement.Source` from switch
copies of value-typed statement variants, escaping whole structs to the heap.
`walkStatementExpressions` measures 66MB flat = 13.6%, fed by the three
hoist passes running per statement.

Action: hand each hoist pass operand pointers without re-copying, or store
statement variants as pointers. Expected impact: largest single byte source.

#### R31 — Template engine executes per compilation

Evidence: `components.go:72,77` execute embedded `text/template` instances;
profile shows `text/template.(*state).walk` → Builder.Write = 51MB = 10.5%,
plus reflection per field access.

Action: translate the embedded templates to direct Builder writes in Go
(presentation stays mechanical; models unchanged). Largest single consumer
inside the Builder bucket; highest effort in the perf set.

#### R32 — walkProgram overhead per call

Evidence: `walk.go:258-270` allocates four closures and two maps per call;
×3,425 corpus walks ≈ 20k allocs/op (~17% of 121k); `walkProgram.func1-4` ≈
10% cumulative CPU.

Action: hoist walkers to package-level functions taking a `*walkState`.

#### R33 — Defensive copy on every member query

Evidence: `types/unions.go:129` `return append([]Type(nil),
typ.Union.Members...)`; 43 generator call sites, all index/range only, none
mutate. Measures 24MB = 4.9%.

Action: return the slice directly after confirming package-internal callers
are read-only.

#### R34 — Owner encoding recomputed per derivation

Evidence: `types/types.go:289-293` rebuilds the encoding per call; called
from `generator/tags.go:44,50,74`, `emission.go:78,618`,
`validation.go:415` — ~10k objects/op, top allocator by count. The encoding
is already cached once per environment (`types.go:330`) but unused here.

Action: store the encoded owner on Object/ADT records at construction.

#### R35 — Builtin seed copied per module environment

Evidence: `types/types.go:322-334` makes five maps plus a full `builtinTypes`
copy per module environment; 29MB = 6.0%; scales with module count.

Action: share one immutable builtin seed consulted before the per-module
names map (copy-on-write lookup).

#### R36 — Quadratic C-name collision scan

Evidence: `types/arena.go:107-114` scans the whole family per candidate name
from `uniqueCollectionCName` (`arena.go:93,101`) — quadratic string compares
as specialization counts grow. Small today (corpus 85ms), quadratic shape
forever.

Action: keep a `map[string]struct{}` CName index per family.

#### R37 — Discovery performs 22 full-program walks

Evidence: 22 `walkProgram(program, visitor)` sites (adt, arrays, bitwise ×2,
concurrency, conversions, declarations, dicts, division, emission ×2,
equality, errors, heap, lists, print, strings, unions, views, wrap);
measured 25 walks for a tiny program, 3,425 for the corpus.
`traversal_metrics_test.go` records fusing was "deferred pending data."

Action: fuse the collectors onto one shared traversal — collectors stay
separate, traversal does not. Do after R25/R26/R22-R24 settle shapes; this is
the structural win the data now justifies.

### Tests

#### R38 — Intra-package pipeline duplication

Evidence: `generator/walk_test.go:20-33` (`walkTestProgram`) re-implements
lex→parse→check; `checkedGeneratorSource` sits at `generator_test.go:153-168`
in the same package; a third inline copy lives at `walk_test.go:120-131`.

Action: call the existing helper; delete the copies.

#### R39 — Triplicated invalid-identifier table

Evidence: `generator_test.go:357,371,388` loop near-identical forged-spelling
tables (`"value-name", "1value", "café"` etc.).

Action: one table-driven test over `(nameKind, spelling)` pairs.

#### R40 — Assertion helper exists but is nearly unused

Evidence: `assertGeneratedC` at `integration/functions_test.go:184-193` has
ten uses, all in its own file, while want-loops over `strings.Contains(rootC(…))`
span ~30 integration files (~150 sites).

Action: promote to `helpers_test.go` (plus a header-taking variant); adopt
incrementally on touched files — no big-bang rewrite.

#### R41 — Unhelped uniqueness assertion

Evidence: `strings.Count(x, y) != 1` hand-written ~29 times in integration
and ~25 in generator tests, each with its own failure message.

Action: add `assertContainsOnce(t, text, want)` per package; convert
opportunistically.

Accepted as forced (not a finding): the four cross-package source-pipeline
helpers (`parseProgram`, `checkSource`, `checkedGeneratorSource`,
`compileSource`) cannot consolidate — Go test files cannot import across
packages, and a shared testutil package would violate the distinct-execution-
lifecycle rule for new test packages.

Assertion-quality spot-check: zero offenders found — every generate-path test
asserts emitted C text, honoring the text-assertion rule. Recorded as a clean
baseline.

### Reference synchronization

#### R42 — Load-bearing spellings unnamed in the contract

Evidence: `hex_lit_`, `hex_wrap_`, `hex_convert_`, `hex_div_`, `hex_rem_`,
`hex_match_`, `hex_try_`, `hex_task_entry_`, `hex_heap_` appear zero times in
`docs/reference.md`, yet tests assert them as exact contract text
(`string_component_test.go:45,53`, `generator_test.go:892`,
`concurrency_component_test.go:51`) and the snippet manifest freezes their
hashes.

Action (author's call): either name the families in the C23 output contract
section or record that they are deliberately unspecified. Not a conformance
bug either way.

Reference sync otherwise verified exact on five sampled normative claims:
private prefixes (`hex_v_/hex_t_/hex_m_/hex_f_`), component artifact list and
guards, `hex_tag_<label>` spellings with `_0/_1` collisions, module-owner
encoding, collection C-name disambiguation. `docs/status.md` has no stale
rows; no references to retired documents survive.

## Verified-clean record

Recorded so future audits do not re-probe these:

- Import resolution is genuinely single-source: normalization, cycle
  detection, reachability live only in `compile.go`; the checker consumes
  edges without re-deriving; the generator contains zero import logic; one
  graph struct threads all phases.
- Literal-registry ownership is unambiguous: created once, shared by pointer,
  exactly one production nil-guard, render-phase lookups fail closed.
- Builtin interning is enforced by construction: one arena per CheckModules,
  all eleven constructed families intern through arena maps keyed by
  CanonicalKey, equality is identity-pointer; cross-module generic type
  specialization is grammatically impossible, closing the last fork
  candidate.
- Ownership/cleanup enforcement matches its documented envelope precisely:
  the three documented rejections each have one primary site sharing
  centralized primitives; envelope limits (parameters, member reads, alias
  copies) are deliberate conservatism, not holes.
- Naming is consistent: facet-per-file snake_case throughout, discernible
  function-prefix conventions, no symbol found whose name contradicts its
  behavior.
- Stdlib modernization is essentially complete: no `sort.*`, no
  `interface{}`, no hand-rolled min/max/clamp, no commented-out code blocks.
- gofmt/LF hygiene: clean, pinned by `.gitattributes`.

## Proposed execution order (non-binding)

1. R1, R2 — contract breaches.
2. R10–R16 — diagnostic hygiene, one commit.
3. R3–R9 — dead code and legacy API, one commit (R6 includes manifest
   rebuild).
4. R38–R41 — test hygiene, incremental.
5. R33, R32, R34, R30 — measured performance, smallest risk first, re-
   benchmarking after each.
6. R25, R26 — parameter bundles before structural splits.
7. R22–R24 — file splits along the bundle seams.
8. R31, R37 — remaining structural performance.
9. R17–R20, R27–R29, R21, R35, R36, R42 — residue.

## Disposition

All forty-two findings are Undecided pending discussion. Recommendations
above are the auditor's, not decisions. A finding leaves this RFC only by
being implemented (cite the number in the commit), explicitly rejected with
rationale here, or folded into another finding. Silence is not rejection.

## Invariants

1. No finding here changes code until its disposition is decided and recorded.
2. Behavior-neutral items (R22–R24, R38–R41) must not move the snippet
   manifest; a moved hash on one of these commits is a defect in the commit.
3. Performance items must ship with before/after benchmark pairs from the
   Baseline record's command set.
4. R6 is the only finding expected to legitimately alter generated C; any
   other finding moving manifest hashes has a wider blast radius than its
   spec states.
5. Successor commits cite finding numbers; this RFC does not track
   implementation state — `docs/status.md` and git history do.

## Validation

Exhaustive for this RFC as a findings catalog:

- Every finding carries file:line evidence and a verbatim quotation; a
  finding without both is invalid and removed.
- Quotations were verified against the working tree at creation time; line
  numbers are advisory. On any mismatch between quote and tree, re-verify the
  finding before acting on it.
- Exactly three claims are marked UNVERIFIED (R6's emission sub-claim, R21's
  matrix, R29's probe); each names the probe that settles it.
- The severity table covers all forty-two findings with no gaps.
- The baseline record's benchmark table is the measurement contract for every
  performance finding.

## Non-goals

- Deciding any disposition.
- Implementing anything.
- Re-auditing language surface (RFC 0103 owns that).
- Proposing caching layers, incremental compilation, or abstraction — the
  performance set is deliberately mechanical only.

## Drawbacks

- Like any findings catalog, this ages: the tree moves, quotes go stale. The
  validation rule turns staleness into a detectable condition rather than a
  silent one.
- Forty-two items invite cherry-picking; the execution order and disposition
  discipline exist to force whole-accounting, but the author may reject the
  ordering wholesale.
