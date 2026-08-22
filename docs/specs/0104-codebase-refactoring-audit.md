# RFC 0104: Codebase Refactoring Audit

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready. Every accepted finding has one selected
  mechanism, the accepted set has a binding execution order, and Validation is
  the exhaustive definition of done. Rejected and deferred findings do not
  return without new evidence.
- Created: 2026-08-21
- Updated: 2026-08-22 — re-verified against HEAD and dispositioned.
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

On 2026-08-22 a second, twenty-pass re-audit re-probed every focus from the
prompt table (hygiene through final conformance) with fresh parallel probes
(`gofmt`/`git eol`/`file`, `grep -rn` for diagnostics/maps/panics, complexity
report, `go vet`/`go test`, `slices.Sorted` determinism sweep, spec↔reference
sampling). It re-examined R1–R42 and added eight delta findings R43–R50
plus one elaboration of R5 (global counter non-atomicity). Green suite,
`gofmt` clean, `go vet` clean, and determinism invariants still hold; new
findings are mechanical hygiene/stdlib/doc gaps, not architecture breaches.

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

Twenty-pass re-audit (2026-08-22): twenty focused passes mapped 1:1 to the
prompt table — (1) line-ending/`gofmt` hygiene, (2) dead-code removal, (3)
legacy API removal, (4) determinism, (5) diagnostic centralization, (6)
naming consistency, (7) giant-file splitting, (8) parameter bundling, (9)
exported-API docs, (10) error-handling unification, (11) stdlib
modernization, (12) phase-dispatch audit, (13) import single-source, (14)
literal-registry invariant, (15) builtin interning unification, (16)
ownership/cleanup, (17) test-helper consolidation, (18) reference sync, (19)
allocation/hot-path, (20) final conformance — each with live `grep`/`bash`
evidence. Deltas are recorded as R43–R50; re-verified clean invariants are
folded into the Verified-clean record.

## Baseline record (pass 20)

Recorded 2026-08-21, before any refactor work:

- `go test ./...` fully green (all eleven packages).
- `gofmt -l` empty; `.go` files LF-only; `.gitattributes` pins `*.go text eol=lf`.
- `go vet ./...` clean; `go vet -tags c23 ./...` clean.
- Benchmarks (benchtime=1x, windows/amd64):

| Benchmark | ns/op | B/op | allocs/op |
| --- | --- | --- | --- |
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
| --- | --- | --- | --- | --- |
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
| R29 | Per-module BindingID counters meet in specializations — excluded by probe | latent trap probe | S | M |
| R30 | Statement structs heap-boxed per hoist pass — 13.6% of bytes | performance | M | M |
| R31 | `text/template` executes component templates per compilation — rejected; templates remain authoritative | performance | L | M |
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
| R43 | Incorrect `gofmt` invocation plus unrelated working-tree deletion — rejected | hygiene | S | L |
| R44 | Working-tree line-ending cleanup with an LF-correct index — rejected | hygiene | S | L |
| R45 | `indexIn` duplicates `slices.Index` / `slices.Contains` | stdlib | S | L |
| R46 | `typeSerialCounter` global non-atomic, process-history trap | dead code / determinism | S | L |
| R47 | Exported-API documentation gap — deferred pending a scoped AST inventory | docs | S | L |
| R48 | `containsTypeParameter` allocation claim — excluded by benchmark and corpus profile | performance (minor) | S | L |
| R49 | `git config core.autocrlf=true` on Windows host vs repo `eol=lf` expectation | hygiene | S | L |
| R50 | Pilens graph stale (5811 min) — review-graph findings may miss HEAD | tooling | S | L |

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

Action: return errors through every source-reachable generator path. Extend
visitor callbacks where required; do not use panic/recover as error control
flow. Startup panics may remain only for corrupt embedded assets that make the
compiler binary unable to perform any compilation.

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

Action: rename both to `validate` and `lineLimitWarnings`. Move only their
direct tests into `package snippets`; public-API catalog compilation remains in
`package snippets_test`.

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

The same stale provenance appears in
`compiler/tests/integration/inferred_declaration_test.go` as a Go comment.

Action: drop the clause from the diagnostic and rewrite the test comment as a
present-tense contract with no RFC provenance.

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

Action: clone `names.flow` before checking each statement and restore that
snapshot when the statement produces diagnostics. Keep the successful
statement's resulting flow state.

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

Ordering note: R26 is independent and lands in this RFC. R25 and the R22-R24
file splits remain deferred together; their successor re-measures and defines
its own seams after the accepted cleanup lands.

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

Action: make checked address-of nodes carry their already-known canonical
result type. Generator rendering and validation consume that checked metadata
and never construct a fresh pointer type for comparison.

#### R29 — Cross-module BindingID meeting — excluded by probe

Evidence: `scope.go:53-54` numbers bindings per module scope;
`specializeFunctionIn` runs in the requesting module's scope
(`generics.go:384-386,450`) yet emits into the defining module's artifacts
(`modules.go:235-242`). Sampled consumers look per-function, but not every
BindingID consumer was traced.

Probe: a two-module program defined `identity<T>` in `lib.hex`, specialized it
from `app.hex`, and also defined a local function with the same local binding
name. Compilation succeeded with empty diagnostics. The specialization emitted
`hex_f_m3_lib_identity_Int32` in `modules/lib.c`; the local function emitted
`hex_f_m3_app_local` in `modules/app.c`. Both used `hex_v_copy` only inside
their separate C function scopes. `BindingID` is consumed through per-function
generator maps, so the per-module counters never meet in one C binding scope.

Disposition: reject. The proposed collision does not reproduce.

### Performance (measured)

All percentages are of BenchmarkCorpus allocated bytes (23.5MB/op) unless
stated. Re-benchmark before/after each fix; see Baseline record.

#### R30 — Statement structs heap-boxed per hoist pass

Evidence: `generator/walk.go:166-172` takes `&statement.Source` from switch
copies of value-typed statement variants, escaping whole structs to the heap.
`walkStatementExpressions` measures 66MB flat = 13.6%, fed by the three
hoist passes running per statement.

Action: keep value-typed statement variants. Make the three read-only hoist
walks traverse expression values, preserving the stable child pointers used as
hoist keys without taking addresses of fields in copied statement values.

#### R31 — Template engine executes per compilation

Evidence: `components.go:72,77` execute embedded `text/template` instances;
profile shows `text/template.(*state).walk` → Builder.Write = 51MB = 10.5%,
plus reflection per field access.

The measured cost is real, but the proposed action conflicts with the settled
component architecture: `compiler/generator/packages/*.c` and `*.h` are the
authoritative runtime source, while Go owns only selection and typed render
models. Moving C presentation into Go would trade allocation savings for a
larger, less maintainable generator and would reverse that ownership rule.

Disposition: reject. Keep the package templates. Reopen only with a measured
optimization that preserves those files as the sole C/header source of truth.

#### R32 — walkProgram overhead per call

Evidence: `walk.go:258-270` allocates four closures and two maps per call;
×3,425 corpus walks ≈ 20k allocs/op (~17% of 121k); `walkProgram.func1-4` ≈
10% cumulative CPU.

Action: hoist walkers to package-level functions taking a `*walkState`.

#### R33 — Defensive copy on every member query

Evidence: `types/unions.go:129` `return append([]Type(nil),
typ.Union.Members...)`; 43 generator call sites, all index/range only, none
mutate. Measures 24MB = 4.9%.

Action: replace the allocating exported slice result with a read-only member
view exposing `Len` and `At`. The view may hold the canonical slice privately;
no caller receives a mutable slice. Convert callers mechanically to indexed
reads.

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

Disposition: reject as a reference change. These are compiler-internal names,
not a stable C ABI. Generated-text tests and the snippet manifest may protect
their current spellings without making them normative. Only explicitly
exported C symbols have a stable external spelling contract.

Reference sync otherwise verified exact on five sampled normative claims:
private prefixes (`hex_v_/hex_t_/hex_m_/hex_f_`), component artifact list and
guards, `hex_tag_<label>` spellings with `_0/_1` collisions, module-owner
encoding, collection C-name disambiguation. `docs/status.md` has no stale
rows; no references to retired documents survive.

### Twenty-pass delta findings (2026-08-22)

#### R43 — `gofmt -l` phantom path + staged deletion

Evidence: `gofmt -l ./...` reports `open ...\\.claude: The system cannot find the path specified.` then `EXIT:0` with `gofmt -l | wc -l` = 1 phantom line. `git status --porcelain` shows `D docs/superpowers/plans/0100-0101-program-wide-helpers.md` (staged 226-line deletion) at `git diff --stat HEAD`. `file` over `compiler/*.go` reports `ASCII text`/`Unicode text` with no `CRLF`.

Why it matters: every `gofmt -l` CI gate must filter the phantom path or it flips to 1; staged deletion is uncommitted working-tree drift.

Disposition: reject. `gofmt -l ./...` treats `./...` as a path rather than a Go
package pattern; the repository's tracked-Go-file invocation is clean. An
unrelated working-tree deletion is not part of a compiler refactor and must not
be committed or reverted through this RFC.

#### R44 — Former working-tree `w/mixed` state — rejected

Audit-time evidence reported `i/lf w/mixed attr/text eol=lf` for
`compiler/generator/packages/io.c` and `io.h`. Current verification reports
`i/lf w/lf` for both. The index was LF-correct throughout.

Disposition: reject. The index is already LF-correct, so this is local checkout
state rather than a repository defect. Repository-wide renormalization is
forbidden cleanup and would create review noise without changing tracked
content.

#### R45 — `indexIn` duplicates `slices.Index`

Evidence: `compiler/compile.go:408 func indexIn(haystack []string, needle string) int { for i, candidate := range haystack { if candidate == needle { return i } } return -1 }`. Stdlib `slices.Index` provides identical semantics.

Action: replace `indexIn` with `slices.Index` and delete `indexIn`. No adjacent
predicate sweep belongs to this finding.

#### R46 — `typeSerialCounter` global non-atomic (elaborates R5)

Evidence: `compiler/types/types.go:37 var typeSerialCounter uint64 = 0` + `:39 func newTypeIdentity(scope *typeIdentity) *typeIdentity { typeSerialCounter++; return &typeIdentity{serial: typeSerialCounter, parent: scope} }`. `serial`/`parent` write-only today (R5), but counter is process-global, not per-Arena, non-atomic, increments across repeated `Compile` calls in one process.

Why it matters: if `serial` ever becomes read (ordering, tie-break), output becomes process-history-dependent — byte-reproducibility violation + data race under concurrent `Compile`.

Action: delete fields and counter per R5; if serial is retained, make it per-Arena atomic or remove global.

#### R47 — Exported-API doc gap

The original count was invalid: the three named files contain 65 exported
declarations, and all production Go under `compiler/` contains 206. A textual
declaration count also cannot decide whether the immediately preceding comment
is a valid Go documentation comment.

Disposition: defer. A successor must use the Go AST to inventory one package at
a time and add only missing CARE-quality API contracts.

#### R48 — `containsTypeParameter` allocation hypothesis — excluded

The original mechanism claim is false: a Go map is passed as a small descriptor;
recursive calls do not copy its contents. Five 500,000-iteration benchmark runs
measured `0 B/op, 0 allocs/op` for a scalar, a 32-pointer chain, a recursive
object, and an immediate generic-parameter hit. A five-iteration corpus
allocation profile contained no `ContainsTypeParameter` or
`containsTypeParameter` allocation node.

Disposition: reject. The visited map remains because it makes recursive nominal
types terminate and currently costs no heap allocation.

#### R49 — Host `core.autocrlf=true` vs repo `eol=lf` expectation

Evidence: `git config --get core.autocrlf` → `true` on Windows host; `git ls-files --eol` shows `w/crlf` for `.editorconfig`, `.vscode/*`, `workbench/snippets/categories/*.json`, `notes.md` despite `* text=auto` + `*.go text eol=lf`. Go files unaffected; non-Go text files acquire CRLF in working tree (index LF correct).

Action: document expected host setting (`core.autocrlf=false` or `core.eol=lf`) in `AGENTS.md`/contributor notes, or accept drift — not a source bug, but explains Pass 1 `w/crlf` noise.

#### R50 — Pilens review graph stale

Evidence: `project_report` header: `built 2026-08-18T11:55:37.132Z — 154/155 files (99% coverage) — built at commit 92138b62; HEAD is now 88940413. Results below reflect the earlier revision — stale 5811m ago.` Hubs/entry-points/dead-weight lists may miss recent edits.

Action: run `pilens_rebuild` or `re-analyze` before relying on graph for dead-code decisions (R3–R6). No code change.

## Verified-clean record

Recorded so future audits do not re-probe these:

- Import resolution is genuinely single-source: normalization, cycle
  detection, reachability live only in `compile.go`; the checker consumes
  edges without re-deriving; the generator contains zero import logic; one
  graph struct threads all phases. Re-probed 2026-08-22: `grep -rn resolveImport|ModuleGraph|reachableModules` confirms single graph struct threads all phases; `slices.Sorted(maps.Keys(...))` everywhere (emission headers, concurrency, collections).
- Literal-registry ownership is unambiguous: created once, shared by pointer,
  exactly one production nil-guard, render-phase lookups fail closed. Re-probed 2026-08-22: `compiler/checker/literals.go` single registry, `constantOperand`/`constantNode` helpers traced, no second registry.
- Builtin interning is enforced by construction: one arena per CheckModules,
  all eleven constructed families intern through arena maps keyed by
  CanonicalKey, equality is identity-pointer; cross-module generic type
  specialization is grammatically impossible, closing the last fork
  candidate. Re-probed 2026-08-22: 11 families via `unionTypes`/`genericTypes` maps, `Equal` pointer-identity, R46 counter noted as only residual risk.
- Ownership/cleanup enforcement matches its documented envelope precisely:
  the three documented rejections each have one primary site sharing
  centralized primitives; envelope limits (parameters, member reads, alias
  copies) are deliberate conservatism, not holes. Re-probed 2026-08-22: `alloc.go:14 checkDeferStatement`, `errors.go:143 checkErrdeferStatement`, `flowState.freed/markFreed` single-site.
- Naming is consistent: facet-per-file snake_case throughout, discernible
  function-prefix conventions, no symbol found whose name contradicts its
  behavior. Re-probed 2026-08-22: `checker/scope.go` `bindingKind/scope/flowState` vs `generator/concurrency.go` `generatedConcurrencyState` naming coherent.
- Stdlib modernization is essentially complete: no `sort.*`, no
  `interface{}`, no hand-rolled min/max/clamp, no commented-out code blocks. Re-probed 2026-08-22: 35 `slices.Sorted(maps.Keys)` sites, only `indexIn` remains (R45).
- gofmt/LF hygiene: tracked Go files are clean and pinned by `.gitattributes`.
  Re-probed 2026-08-22 with the tracked-file `gofmt` invocation; the former R44
  checkout state is now `w/lf`, and the index remained LF-correct throughout.

Re-audit mapping to prompt table (for traceability):

| Prompt pass | Primary R’s | Re-verified clean |
| --- | ---: | --- |
| 1 hygiene | R43,R44,R49 | `gofmt`/`eol` clean (R50 stale graph noted) |
| 2 dead-code | R3–R7 | — |
| 3 legacy API | R8,R9 | — |
| 4 determinism | R20 | determinism sweep clean |
| 5 diagnostics | R10–R16 | — |
| 6 naming | — | naming clean |
| 7 giant files | R22–R24 | `emission.go` cohesive, skip |
| 8 param bundling | R25,R26 | — |
| 9 exported docs | R47 | core `Compile` doc’d |
| 10 error handling | R1,R2,R17,R18,R19 | — |
| 11 stdlib | R45 | 35 `slices.Sorted` sites clean |
| 12 phase dispatch | R21 | — |
| 13 import single-source | — | single-source clean |
| 14 literal registry | — | single registry clean |
| 15 builtin interning | R5→R46 | interning clean, R46 residual |
| 16 ownership | — | envelope clean |
| 17 test helpers | R38–R41 | — |
| 18 reference sync | R42 rejected as non-normative | 5 sampled claims exact |
| 19 allocation | R30–R37 | R48 excluded by measurement |
| 20 conformance | — | `go test`/`go vet` green |

## Re-verification (2026-08-22, pre-implementation)

Every finding was re-probed against HEAD (`8894041`) before dispositioning,
because the tree moved after the audit: RFC 0074's diagnostic work and the
in-flight IO commit both touched `checker/`, `parser/`, and `generator/`.
Result: the accepted correctness and cleanup findings still reproduce. R29
failed its required probe, R48 failed its allocation hypothesis, R31 was
rejected on architectural grounds, and R43/R44 were rejected as local tooling
or checkout state rather than repository defects. Line numbers remain
advisory:

| Finding | Audit recorded | Re-verified at HEAD |
| --- | --- | --- |
| R1 | 6 `panic(` sites | 10 sites, still zero `recover()` |
| R10 | ~15 bypassing sites | 24 composite-literal diagnostics / 27 `Category:` call sites across 12 files |
| R13 | 1 hardcoded `Parameters[0]` | 2 remain (`generics.go:597,955`); two sibling sites already use `[index]` |
| R41 | ~50 `strings.Count` sites | 80 |

The R44 observation no longer reproduces: both IO templates report
`i/lf w/lf`. It never represented an index change and remains rejected rather
than becoming an implementation item.

## Disposition

Decided 2026-08-22 and revised after targeted review. Thirty-three findings
are accepted, six deferred, and eleven rejected.
A rejected finding is closed and does not return without new evidence; a
deferred finding names the successor condition that reopens it. Implementing
commits cite the finding number.

| Disposition | Findings | Count |
| --- | --- | ---: |
| **Accept** | R1–R20, R26–R28, R30, R32–R36, R38, R39, R45, R46 | 33 |
| **Defer** | R21, R22, R23, R24, R25, R47 | 6 |
| **Reject** | R29, R31, R37, R40–R44, R48–R50 | 11 |

### Rejected, with rationale

- **R37 (fuse the 22 discovery walks).** Answered with measurement during the
  RFC 0080 work: the corpus runs 3,425 walks/op against 47,300 nodes/op and
  121,101 allocs/op, so traversal count is not where the allocation goes.
  Fusing trades a clear per-concern walk for one entangled pass and buys
  nothing the benchmark can see. Closed.
- **R40, R41 (test-helper adoption across ~150 loops and 80 `strings.Count`
  sites).** Mass rewrite of a green test suite with no behavior change, which
  destroys `git blame` over the compiler's only regression net. The helpers
  stay; new tests use them. Existing sites are not converted.
- **R29 (cross-module BindingID collision).** The required two-module generic
  probe emitted colliding local spellings only in separate C function scopes;
  no generated binding scope combines the counters.
- **R31 (replace package templates with Go writers).** The measured template
  execution cost does not justify reversing the settled ownership of runtime C
  and headers by `generator/packages/`.
- **R43, R44 (format invocation and checkout line endings).** Neither is a
  tracked compiler defect. The proper tracked-file `gofmt` check is clean and
  the Git index is LF-correct.
- **R48 (`containsTypeParameter` allocation).** Focused benchmarks report zero
  allocations and the corpus allocation profile contains no matching node.
- **R42 (private generated-helper spellings).** Internal generated names remain
  deterministic implementation output, not stable ABI. Tests may assert their
  current text without promoting it into `reference.md`.
- **R49 (`core.autocrlf=true`).** Host git configuration, not a repository
  artifact.
- **R50 (stale Pilens graph).** External tooling state, outside this RFC's
  scope.

### Deferred, with the condition that reopens each

- **R22, R23, R24 (file splits) and R25 (the `(names, typeEnvironment)`
  group).** These share one seam, and splitting invalidates the file:line
  evidence of every finding not yet implemented. They go to a successor RFC
  raised only after the accepted set has landed — at which point the sizes
  are re-measured and the split is justified against the smaller tree or
  dropped.
- **R21 (kind↔render↔validate coverage matrix).** A real gap, but building
  the matrix is test infrastructure that should be designed alongside the
  next change to phase dispatch rather than speculatively.
- **R47 (exported API documentation).** Accepted in principle, too broad for
  this batch. A successor uses the Go AST and scopes its inventory to one
  package at a time.

### Selected implementation decisions

- **R1:** source-reachable generator failures return errors; only corrupt
  embedded assets may panic during startup.
- **R9:** snippet-only helpers become unexported; direct tests join their
  package while public compiler smoke tests remain external.
- **R19:** statement checking snapshots flow and restores it on failure.
- **R28:** checked address-of nodes carry canonical result types; generation
  never reconstructs them.
- **R30:** hoist walkers traverse values; statement representation stays
  unchanged.
- **R33:** union members are exposed through a read-only `Len`/`At` view.
- **R42:** private generated names remain non-normative implementation details.

## Implementation plan

Stages 1-8 are separate commits. Stage 0 and final handoff create no commit.
At every commit boundary: run `gofmt` on changed Go files; require
`go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...`; compare the
snippet manifest with the preceding stage. Only Stage 3 may change it.

### Stage 0 - Refresh evidence and baselines

1. Record `git status`, the snippet-manifest checksum, the three repository
   gates, and the existing corpus benchmark/profile before editing.
2. Re-run every accepted finding's named-symbol probe. Record bounded line or
   count drift in the handoff; if a mechanism no longer reproduces, revise its
   disposition before implementation.
3. Capture focused allocation baselines for R30 and R32-R36. Probes and
   profiles live under `.tmp/` and are removed after their result is recorded.
4. Preserve unrelated working-tree changes. Do not normalize line endings,
   regenerate the manifest, or edit canonical documentation in this stage.

### Stage 1 - Fail-closed generator errors (R1, R2)

1. Change `walkProgram` to return `error`. Make its recursive walk stop on the
   first error and return an Unknown Error for an unsupported checked statement
   instead of panicking.
2. Let visitor callbacks that can fail return `error`; update every caller
   mechanically to return `nil` when collection succeeds. In particular,
   `discoverGeneratedConcurrency` and `discoverGeneratedPrint` return the
   errors currently raised through callback panics.
3. Replace source-reachable panics in `heap.go`, `tags.go`, `unions.go`, and
   the generator walker with ordinary returned diagnostics propagated through
   discovery, merge, validation, or emission as appropriate.
4. Retain panic only in `components.go` while reading or parsing embedded
   package assets during package initialization: corrupt embedded assets make
   the compiler binary unusable for every source program.
5. Replace raw generator `fmt.Errorf` results in `generator.go` and
   `components.go` with `compilerTypes.Diagnostic{Category: UnknownError,
   Stage: "generator"}`. Preserve module stamping when a logical source key is
   known.
6. Add focused generator tests that induce each reachable invariant path and
   an exported-API integration test proving `Compile` returns `ExitFailure`, no
   artifacts, and a `[Unknown Error]` diagnostic instead of panicking.

### Stage 2 - Diagnostic construction (R10-R16)

1. Route every checker diagnostic call site through `typeErrorAt`,
   `nameErrorAt`, `moduleErrorAt`, or `unknownAt`; only their shared adapter may
   construct `Diagnostic` directly.
2. Give the generic-ADT inference failure and unsupported-top-level fallback
   their source tokens so rendered diagnostics include module, line, and
   column.
3. Classify unsupported expression, place, and operator-hint defaults as
   Unknown Error; these represent checked-tree inconsistencies, not user type
   errors.
4. Use the active generic-parameter index at both conflict sites instead of
   `Parameters[0]`.
5. Remove RFC provenance from the ADT payload diagnostic and the stale
   integration-test comment while preserving their present-tense contract.
6. Replace the lexer's inline syntax-diagnostic composites with the one
   `literalDiagnostic` builder, adjusting its return shape only as needed by
   existing callers.
7. Update focused checker, lexer, and integration assertions for exact category
   and position. Do not weaken any existing message assertion unrelated to the
   corrected text.

### Stage 3 - Dead code, API surface, and stdlib (R3-R9, R45, R46)

1. Delete `expectedUses`, its appends, and discard in `checker/generics.go`.
2. Delete the stale keep-alives and orphaned comment in generator bitwise and
   defer code.
3. Delete `typeIdentity.serial`, `typeIdentity.parent`,
   `typeSerialCounter`, and the writes in `newTypeIdentity`; pointer identity
   remains the sole type-identity mechanism.
4. Delete concurrency's unused `headerLiteral` field and its `Scheduler`
   interning side effect. Recompile every catalog snippet, inspect the semantic
   C diff, then rebuild the manifest once through the required temporary-test
   procedure. Only affected concurrency artifacts may move.
5. Export the checker's predicate as `BitCastEligibleType`, switch generator
   callers to it, and delete the generator copy.
6. Rename `NewEnvironmentWithOwner` to `newEnvironmentWithOwner` and update its
   same-package caller. Rename snippet helpers to `validate` and
   `lineLimitWarnings`; move catalog validation and line-limit coverage into
   package `snippets`. Remove the warning-only call from the external catalog
   compiler smoke test; its compilation and manifest assertions remain in
   package `snippets_test`.
7. Replace `indexIn` with `slices.Index` and delete the wrapper. Perform no
   adjacent stdlib sweep.
8. Run the manifest gate after every substep; only step 4 may move it. Review
   the final artifact breakdown before committing the stage.

### Stage 4 - Error propagation and canonical metadata (R17-R20, R27-R28)

1. Add one shared diagnostic-module stamping helper and one stable
   line/column comparator under `compiler/types`; replace the two
   `stampModule` implementations and duplicate comparators without changing
   ordering.
2. Consume `importTarget`'s `ok` result at every call site. A missing edge after
   successful graph resolution returns a positioned checker Unknown Error and
   never binds an empty module id.
3. In `checkStatements`, clone `names.flow` immediately before each statement.
   Restore the snapshot when that statement returns diagnostics; retain the
   mutated state only on success. Keep existing return-flow rollback intact.
4. Fold compilation statistics through `graph.Order`, indexing
   `graph.Modules`, rather than ranging the map.
5. Make the two render helpers that currently construct
   `expressionValidation{}` require a non-nil literal registry through their
   signatures or a constructor. A missing registry returns a generator
   diagnostic before String rendering.
6. Stamp canonical `ResultType` and required operand metadata onto every
   checker-created address-of node, including explicit `ref` and automatic
   method-receiver adaptation. Generator type recovery consumes this metadata;
   remove package-level `PtrType`/`MutPtrType` reconstruction from those paths.
7. Add focused regressions for module stamping/order, missing import edges,
   failed-statement flow rollback, nil literal registries, and canonical
   address-of metadata. Generated artifacts remain byte-identical.

### Stage 5 - Low-risk measured performance (R33-R36, R32)

Implement one finding at a time, run its focused measurement and the corpus
allocation gate, then keep or revert that finding before proceeding.

1. **R33:** introduce `UnionMemberView` containing the source `Type`, with
   `Len() int` and bounds-checked `At(index) (Type, bool)`. Make
   `UnionMembers` return the view; convert all checker, generator, and type
   callers to indexed reads. Expose no mutable member slice. Verify zero
   allocations for ordinary, nullable-pointer-niche, and singleton views.
2. **R34:** cache the encoded module owner on nominal object and ADT metadata at
   construction, including generic specialization re-keying. Replace repeated
   output-path `EncodeModuleOwner` calls with the stored spelling; canonical
   module identity remains unchanged.
3. **R36:** maintain a C-name set beside each constructed collection-family
   interning map. `uniqueCollectionCName` probes the set in O(1), reserves the
   selected name exactly once, and preserves existing construction-order
   suffixes byte-for-byte.
4. **R35:** stop copying the immutable builtin table into every module
   environment. Keep module declarations in the per-environment `names` map
   and make lookup fall back to the immutable builtin registry. Audit direct
   `names` access so protected-name and redeclaration behavior is unchanged.
5. **R32:** after Stage 1's error-returning walker is stable, replace its four
   per-call recursive closures with methods on a short-lived `walkState` that
   owns the two seen maps, visitor, and first error. Do not fuse collectors or
   share mutable state across compilations.
6. For every retained substep, require deterministic generated output, lower
   targeted allocation/profile cost, and no increase in corpus B/op or
   allocs/op. Report at least five CPU-timing runs without treating one sample
   as a gate.

### Stage 6 - Hoist traversal allocations (R30)

1. Keep every checked statement variant value-typed.
2. Change `walkStatementExpressions`, `walkStatementOperand`, and the root
   expression walk to pass read-only values rather than addresses of fields on
   type-switch copies. Nested expression pointer fields remain the stable keys
   used by try, find, and spawn hoist maps.
3. Preserve pre-order, single evaluation, and emitted prologue order across
   nested spawn, Dict find, and try expressions.
4. Add focused hoist-order regressions and compare the corpus allocation
   profile: `walkStatementExpressions` flat allocation must fall, corpus B/op
   and allocs/op must not rise, and every snippet hash remains unchanged.

### Stage 7 - Generator definition context (R26)

1. Introduce one focused definition-render context owning the body builder,
   function and method tables, generated-type state, literal registry, owner,
   logical filename, and tag registry.
2. Convert function, method, and specialized-definition writers to methods on
   that context. Keep each declaration and the function-specific `external`
   flag as explicit call parameters.
3. Do not bundle unrelated statement-rendering parameters or move files; R22-
   R25 remain deferred.
4. Require no generator signature over six parameters, byte-identical output,
   and unchanged definition/prototype order.

### Stage 8 - Test residue (R38, R39)

1. Make `generator/walk_test.go` use the existing checked-source pipeline
   helper; delete its two local pipeline copies without moving tests across
   package boundaries.
2. Replace the three invalid-identifier loops with one table of name kind and
   spelling cases. Preserve every existing case and assertion strength.
3. Run the full gates and require a byte-identical snippet manifest.

### Final conformance and handoff

1. Run every Validation item, the three repository gates, tracked-file
   `gofmt`, repeated-compilation determinism checks, and the final corpus
   benchmark/profile.
2. Review `docs/reference.md` after behavior stabilizes. No language rule or
   private-helper spelling is expected to change; explicitly record that no
   reference edit is required unless implementation evidence proves otherwise.
3. Re-measure the deferred findings after the accepted cleanup lands. Create
   successor specs for those that still meet their reopening conditions, or
   reject them here with evidence before closure. Only then remove RFC 0104
   from `docs/status.md`; no deferred work may disappear unowned.
4. Rebuild the workbench binary into `bin/` and restart it before handoff.
5. Mark this RFC closed and archive it only after code, tests, generated output,
   benchmarks, status, and reference review agree.

Delta trace: R43-R50 were added by the 2026-08-22 twenty-pass re-audit; R46
elaborates R5. The original forty-two remain unchanged.

## Invariants

1. Only accepted findings change code. A deferred or rejected finding that
   turns out to matter reopens through new evidence, not through a commit.
2. R38 and R39 are behavior-neutral and must not move the snippet manifest; a
   moved hash is a defect in their commit. Deferred R22-R24 have no commit in
   this RFC and their successor defines its own manifest contract.
3. Performance items must ship with a focused benchmark or profile proving the
   targeted cost changed plus before/after corpus allocation numbers. Timing is
   reported across at least five runs; one-run timing is not a regression gate.
4. R6 is the only finding expected to legitimately alter generated C; any
   other finding moving manifest hashes has a wider blast radius than its
   spec states.
5. Successor commits cite finding numbers; this RFC does not track
   implementation state — `docs/status.md` and git history do.

## Validation

Exhaustive for the accepted set. Deferred and rejected findings contribute
nothing to the definition of done.

Per stage:

- **R1** — no source-reachable generator path uses `panic` or `recover` for
  control flow. Every induced generator invariant failure returns
  `[Unknown Error]` through `CompilationResult.Stderr` with
  `ExitCode == ExitFailure`. Remaining production panics are confined to
  startup parsing or reading of compiler-embedded package assets and identify
  corruption that prevents every compilation.
- **R2** — no `fmt.Errorf` value reaches `Stderr`; every generator error
  carries a category, and a test asserts the rendered prefix.
- **R3–R9, R45, R46** — each named symbol is absent from the tree
  (`expectedUses`, both `var _ =` lines, `typeIdentity.serial`/`.parent`,
  `typeSerialCounter`, `concurrency.go`'s `headerLiteral`, the generator's
  `bitCastEligible`, `indexIn`); `NewEnvironmentWithOwner`,
  `snippets.Validate`, and `LineLimitWarnings` are absent; their replacements
  are exactly `newEnvironmentWithOwner`, `validate`, and
  `lineLimitWarnings`; the checker's `BitCastEligibleType` is the single
  definition and the generator calls it.
- **R6 only** — the snippet manifest moves, rebuilt through the
  temporary-test procedure. Existing changes are confined to concurrency
  snippets whose literal pool or literal references lose the unused
  `Scheduler` entry or deterministically renumber later entries. No
  non-concurrency snippet changes. Every other stage leaves the manifest
  byte-identical.
- **R10–R16** — `grep` for `Diagnostic{` under `compiler/checker/` returns
  only `diagnosticAt` adapters; no `Category:` is spelled at a checker call
  site; `adt.go`'s inference error and `checker.go`'s top-level fallback both
  render `at l:c`; the three `unsupported expression`/`unsupported place`
  defaults render `[Unknown Error]`; both surviving `Parameters[0]` sites
  report the failing index; no user-visible string or Go comment under
  `compiler/` or `workbench/` contains internal RFC provenance; `lexer.go`
  contains one `Diagnostic{` composite, inside `literalDiagnostic`.
- **R17–R20** — one `stampModule` exists repo-wide; `importTarget`'s `ok` is
  consumed at every call site; a failed statement contributes no flow fact to
  a later diagnostic; the stats fold iterates the canonical order slice, and
  repeated compilation of a multi-module program is byte-identical.
- **R27–R28** — a nil-registry `expressionValidation{}` returns a diagnostic
  rather than panicking; checked address-of nodes carry their canonical result
  type; the generator compares interned identities and contains no package-level
  `PtrType` or `MutPtrType` reconstruction for those nodes.
- **R30, R32–R36** — each performance finding ships a focused before/after
  benchmark or allocation profile proving its targeted cost changed. Corpus
  B/op and allocs/op do not increase. CPU timing is reported over at least five
  runs and any material median regression is investigated rather than hidden
  behind a one-run threshold. Repeated compilation stays byte-identical.
- **R30** — checked statement variants remain value-typed; the hoist walker
  takes no address of an operand field on a type-switch copy, and its focused
  allocation benchmark improves.
- **R33** — public union-member access returns no mutable `[]Type`; the
  read-only view exposes `Len` and `At`, all existing callers use it, and its
  focused benchmark allocates zero bytes per access.
- **R26** — no generator signature exceeds six parameters.
- **R38, R39** — `walk_test.go` builds the pipeline once and the
  invalid-identifier table exists once.

Whole-RFC:

- Every accepted finding is either implemented with its number cited in the
  commit, or moved to Reject/Defer here with rationale. Silence is not
  completion.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass at every
  stage boundary; `gofmt -l` is empty.
- Line numbers in the Findings section are advisory. On any mismatch between
  a quotation and the tree, re-verify before acting — the 2026-08-22
  re-verification is the current record.

## Non-goals

- Re-auditing language surface (RFC 0103 owns that).
- Proposing caching layers, incremental compilation, or abstraction — the
  performance set is deliberately mechanical only.
- The deferred set (R21–R25, R47): out of scope here by disposition, not by
  merit.

## Drawbacks

- Like any findings catalog, this ages: the tree moves, quotes go stale. The
  validation rule turns staleness into a detectable condition rather than a
  silent one.
- Fifty items invite cherry-picking; the execution order and disposition
  discipline exist to force whole-accounting, but the author may reject the
  ordering wholesale. Twenty-pass mapping table exists so prompt-table coverage can be checked without re-reading fifty findings.
