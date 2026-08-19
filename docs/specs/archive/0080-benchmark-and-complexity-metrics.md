# RFC 0080: Benchmark and Complexity Metrics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; verified 2026-08-18. All four parts landed in
  `compiler/tests/benchmarks/`, every file a `_test.go` file. Throughput reports
  MB/s for all thirteen benchmarks; `BenchmarkFailure` became five benchmarks
  split by failing stage, each asserted to fail with diagnostics; the
  `benchmetrics`-tagged counter reports walks and nodes per operation; and the
  complexity report covers 989 functions with cyclomatic and cognitive
  complexity. Dependencies are exactly as predicted — 2 direct, `golang.org/x/tools`
  indirect, 10 `go.sum` lines — and `go list -deps ./compiler/... ./workbench`
  links none of them. Suite runtime 3.9s against the ten-second budget. Baselines
  are in `docs/benchmarks.md` and the new `docs/complexity.md`; `AGENTS.md`
  carries the corrected testing note and the three run commands.

  **The traversal data answers RFC 0074's deferred question, and the answer is
  don't fuse.** 25 walks per module compile, scaling linearly with module count
  (200 for eight modules, 2,500 across the catalog) — but only 36,225
  expression-node visits per corpus compile against 80,192 allocations, about 14
  nodes per walk, because corpus programs are small. Fusing 25 traversals into one
  would remove ~35,000 cheap node visits and couple every family's discovery into
  one callback. Recorded in `docs/benchmarks.md` with the condition that would
  reopen it.
- Created: 2026-08-18
- Scope: three metrics the committed benchmark suite does not capture, plus a
  complexity report over the compiler's own Go source, with the whole suite
  consolidated into `compiler/tests/benchmarks/`
- Supersedes: RFC 0075's two-file benchmark layout. Its benchmark set, its
  policy, and its `docs/benchmarks.md` discipline all stand unchanged; only the
  file placement moves.
- Depends on: RFC 0075, which built the suite this extends. Independent of every
  other open spec.
- Coordinates with: `AGENTS.md`, `docs/benchmarks.md`, `docs/status.md`,
  RFC 0074 (whose deferred traversal-fusing decision this makes decidable)
- Does not change: Hexal syntax, semantics, diagnostics, generated C, the
  `compiler.Compile` contract, or any compiler behavior. Everything here
  measures; nothing here optimizes.

## Summary

RFC 0075's suite reports `ns/op`, `B/op`, and `allocs/op` for nine programs.
That is the right foundation and it stays. It leaves four things unmeasured,
three of which have already cost a decision:

1. **Traversal count.** RFC 0074 explicitly deferred fusing the 21 generator
   discovery walks "pending data", naming this suite as the data. The suite
   reports no traversal count, so the decision is still un-actionable a spec
   later.
2. **Failure-path breadth.** One `BenchmarkFailure` shape covers a diagnostic
   surface of roughly 978 construction sites.
3. **Throughput.** Nine benchmarks over differently sized programs report
   absolute times that cannot be compared to each other.
4. **Complexity of the compiler's own source.** RFC 0074 R13 split two
   functions on the intuition that they were too big. The intuition was right
   and the split was real, but nothing measured before or after, so nothing
   says whether it worked — see Evidence, where it turns out it half did.

This RFC adds all four as **reports, never gates**. RFC 0075's policy stands
verbatim: no benchmark asserts a threshold, and a benchmark that fails is a
test.

**All four live in one package, `compiler/tests/benchmarks/`,** beside the
existing `integration/` and `c23validation/` harnesses. Measurement is not on any
hot path, and one place to look is worth more than a minimal dependency
manifest — so the two third-party complexity libraries land in the root
`go.mod`, and the suite stops being split across two directories.

That consolidation is a change to RFC 0075, which put the eight inline
benchmarks in `compiler/bench_test.go` and `BenchmarkCorpus` in
`workbench/snippets/bench_test.go`. See Consolidation for why the layering
constraint that forced that split is satisfied by a third package, and why
moving the eight out of `package compiler` in fact serves 0075's own goal better
than leaving them there.

## Evidence

### Complexity is unmeasured, and the top of the distribution is extreme

Measured over the 984 functions in `compiler/` (excluding tests) with the two
libraries this RFC adopts:

| Function | Cyclomatic | Cognitive | Lines |
|---|---|---|---|
| `validateExpressionNode` (`generator/validation.go`) | **267** | **288** | 389 |
| `validateConcurrencyExpression` (`generator/concurrency.go`) | 118 | 146 | 185 |
| `renderExpressionUncheckedWithState` (`generator/render.go`) | 107 | 113 | 280 |
| `Lex` (`lexer/lexer.go`) | 107 | **179** | 401 |
| `validateCollectionExpression` (`generator/arrays.go`) | 106 | 135 | 155 |
| `checkBinaryExpression` (`checker/operator_checking.go`) | 98 | 90 | 199 |

Distribution: 801 functions at cyclomatic ≤ 10, 119 at 11–20, 49 at 21–50, and
**15 above 50**. Worst function per package: generator 267, lexer 107, checker
98, parser 35, types 25.

**The finding that justifies measuring this.** RFC 0074 R13 cut
`validateExpressionNode` from 876 lines to 389 — a real 55% reduction, and the
spec's stated goal. Its cyclomatic complexity is still 267 and its cognitive
complexity 288 — both the worst in the compiler. The split moved branches into
four new functions rather than removing them, and two of those functions
immediately entered this table at 118 and 106. Line count went down; branching
did not. R13 was still worth doing — four focused functions beat one 876-line one
— but "we split it" and "it got simpler" turned out to be different claims, and
only one of them was true. Nothing in the repository could have told us that.

### The three benchmark gaps

`walkProgram` has 21 non-test call sites in `generator/`, matching RFC 0074's
count. Each is a full traversal of the checked program, per module. Nothing
observes how many run, so "20 of 21 traversals are redundant" remains an
assertion.

`BenchmarkFailure` compiles one program with a type error and an operator error.
It exercises neither the parser's recovery path, nor module resolution failure,
nor a large diagnostic set, nor the checker's cleanup-misuse analysis added by
RFC 0079.

No benchmark calls `b.SetBytes`, so Go reports no MB/s and the nine programs'
timings are not comparable across shapes.

## Consolidation

`compiler/tests/benchmarks/` already exists in the working tree as an empty
directory, alongside the equally empty `fuzzing/` and `memory/`. Git cannot store
an empty directory, so those three are untracked intent rather than committed
structure — this RFC fills the first of them and leaves the other two alone.

```
compiler/tests/benchmarks/
  shared_test.go       package doc and the shared program table
  compile_test.go      the seven success-path benchmarks, plus throughput
  corpus_test.go       BenchmarkCorpus over the snippet catalog
  failure_test.go      the five failure-path benchmarks and their smoke test
  traversal_test.go    //go:build benchmetrics — walk and node counts
  complexity_test.go   cyclomatic and cognitive complexity, and the report
```

`compiler/bench_test.go` and `workbench/snippets/bench_test.go` are deleted; their
contents move here.

**Every file is a `_test.go` file, and that is a requirement rather than a
convention.** `go build ./compiler/...` matches subdirectory packages, so a
non-test file here importing `gocyclo` would mean building the compiler compiles
third-party code. With test files only, the package is empty to `go build` and
nothing third-party is compiled on the ordinary path. This is why the shared
program table is `shared_test.go` and not `benchmarks.go`.

**The layering rule RFC 0075 protected still holds, though it now needs saying
out loud.** This package sits under `compiler/` and imports
`hexal/workbench/snippets`, which reads like the inversion 0075 argued against.
It is not one. 0075's constraint was that *`package compiler`'s own test binary*
must not import the workbench, so that the compiler stays testable in isolation
from the tool built on it. Go attaches no dependency meaning to a subdirectory:
`go test ./compiler` does not build `compiler/tests/benchmarks`, and `package
compiler` imports nothing new. `workbench/snippets/bench_test.go` is already this
same shape from the other side. The directory nesting is organisational, matching
`integration/` and `c23validation/`; the import graph is unchanged.

**Moving the eight out of `package compiler` serves 0075's goal rather than
undermining it.** They use only the exported surface — `Compile`,
`CompilationResult`, `ExitSuccess`, `ExitFailure` — so nothing is lost by
qualifying them. And `go test ./compiler` goes back to declaring no benchmark at
all, which is the property 0075 had to weaken `AGENTS.md` to describe. That note
(`AGENTS.md:203`) is corrected in the same change: after this, the `compiler`
package declares only `modulegraph_test.go`.

**Where the dependency ends up.** `compiler/tests/benchmarks/` is the only package
importing `gocyclo` or `gocognit`, and it imports them only from test files — so
`go build ./compiler/... ./workbench` compiles and links none of them, and neither
binary changes. The modules do appear in the root `go.mod` and a `go.sum` now
exists: Go has no test-only dependency scope, and the accepted trade is that a
benchmark package is not a hot path and cohesion beats an empty manifest.

## The change

### Part 1 — Throughput

Every benchmark calls `b.SetBytes(n)` where `n` is the total source byte count
of its program, computed once before `b.ResetTimer()`. Go then reports MB/s
beside the existing columns, at no cost and with no new harness.

This makes the shapes comparable to each other for the first time: a
per-byte cost difference between `BenchmarkScalar` and `BenchmarkGenericsHeavy`
is a statement about the phases they stress, where a raw ns/op difference is
mostly a statement about their size.

### Part 2 — Failure-path breadth

`BenchmarkFailure` becomes five benchmarks, one per failure class, so the cost
of producing diagnostics is measured where diagnostics are actually produced:

| Benchmark | Class | Stage that fails |
|---|---|---|
| `BenchmarkFailureLex` | invalid characters, unterminated literals | lexer |
| `BenchmarkFailureParse` | recovery across several malformed items | parser |
| `BenchmarkFailureResolve` | missing module, cycle, non-relative path | reachability |
| `BenchmarkFailureCheck` | type, name, and cleanup-misuse errors | checker |
| `BenchmarkFailureMany` | ~50 independent errors in one program | checker, at volume |

`BenchmarkFailureMany` is the one that matters most: it is the only benchmark
where diagnostic construction, `InModule` stamping, sorting, and rendering
dominate rather than appear as noise.

An ordinary test asserts each of the five actually fails, for the reason
RFC 0075 gave: a silently-succeeding failure benchmark measures the wrong path.

### Part 3 — Traversal count

`walkProgram` is a single funnel, so counting is one place.

Instrument it behind a build tag rather than in production code:

```go
//go:build benchmetrics

package generator

// traversals counts walkProgram entries and nodes visited. It exists only
// under the benchmetrics tag: the compiler carries no counter, and no
// production build has global mutable state for a benchmark's benefit.
```

The tag precedent is `-tags c23`, already used by
`compiler/tests/c23validation`. Under the tag, `walkProgram` increments the
counter and `generator` additionally exports a reader:

```go
//go:build benchmetrics

// TraversalCounts returns walk entries and nodes visited since the last reset,
// and resets. It exists only under the benchmetrics tag, so no untagged build
// has an exported accessor for a counter that does not exist.
func TraversalCounts() (walks, nodes uint64)
```

An exported reader is required because `compiler/tests/benchmarks` is a different
package from `generator`. Being nested under `compiler/` grants no access — Go
scoping is per package, not per directory — so the counter has to be exported
whichever of the two candidate locations the suite had gone to. The tag keeps that
exported surface from existing in a normal build, so this widens `generator`'s API
only under a configuration nothing ships.

`compiler/tests/benchmarks/traversal_test.go` carries the same tag and reports
both figures with `b.ReportMetric`:

```
BenchmarkCorpus-12   ...   4312 walks/op   1841203 nodes/op
```

Untagged builds compile the same `walkProgram` with no counter, so production
cost is exactly zero and there is no shared mutable state — the defect class
that produced a cross-compilation type-identity bug earlier in this codebase's
history and must not be reintroduced for a metric.

**This is the number RFC 0074's fusing decision was waiting on.** With walks and
nodes per compile recorded, "fuse 21 traversals into one" becomes a measurable
proposal instead of a plausible one.

### Part 4 — Complexity report

`compiler/tests/benchmarks/complexity_test.go` parses every non-test `.go` file under
`compiler/` through `go/parser` and reports two metrics per function, plus lines,
per-package aggregates, and the worst function per package.

| Metric | Source | API |
|---|---|---|
| Cyclomatic (McCabe) — branching | `github.com/fzipp/gocyclo` | `gocyclo.Complexity(fn ast.Node) int` |
| Cognitive (Sonar) — readability | `github.com/uudashr/gocognit` | `gocognit.Complexity(fn *ast.FuncDecl) int` |
| Lines per function | ours | `fset` positions |

Two metrics, two one-call APIs, nothing derived. Cyclomatic is the *actual*
branching a test would have to cover; cognitive is the *perceived* difficulty of
reading it. Neither substitutes for the other — see Why only two.

It is an ordinary test that asserts nothing about the numbers — only that the
walk found functions at all, so a wrong path or a parse failure cannot read as
"no complexity found". It runs in `go test ./...` and prints through `t.Log`:

```bash
go test -run TestComplexityReport -v ./compiler/tests/benchmarks
```

It reads source, not the compiler's API, so it imports `go/parser` and the three
metric libraries and nothing from `hexal`.

The report is written to `docs/complexity.md` by hand, on the same discipline as
`docs/benchmarks.md`: re-measured deliberately, with the change that moved it
stated in the same commit.

### Why only two

Halstead measures and the Maintainability Index were prototyped over the same
984 functions with `github.com/yagipy/maintidx` and **dropped on the
measurements**, recorded here so they are not re-proposed:

| Relationship | Measured over 984 functions |
|---|---|
| Halstead volume vs. lines of code | **r = 0.957 (r² = 0.92)** |
| Cyclomatic vs. cognitive | r = 0.931 (r² = 0.87) |

**Halstead volume is 92% explained by line count.** It is an expensive way to
measure size, and lines come free from `fset`.

**MI carries almost nothing of its own.** It is by construction a formula over
volume, cyclomatic, and lines; since volume ≈ lines, MI ≈ f(cyclomatic, lines) —
both already reported, more directly and more legibly. A composite of inputs you
already publish is a presentation choice, not a measurement. It does discriminate
across the range — only 2 of 984 functions pinned at its 0.0 floor — so the
reason to drop it is redundancy, not breakage.

Dropping both removes the `maintidx` dependency entirely, and with it a real
fragility: `maintidx` does not expose MI at all. Its `Visitor.Visit` accumulates
coefficients but computes nothing, and `Visitor.calc` — which does — is
unexported, so `visitor.MaintIdx` is always 0 when the package is driven as a
library rather than as a `go/analysis` pass. Obtaining MI meant calling the two
exported `Calc` methods and reproducing the normalized formula from the library's
own source — verified working, but resting on two methods a future release could
reasonably remove, for a number that adds no information.

**Cyclomatic and cognitive both stay, because their 13% disagreement is the
signal, in both directions:**

```
validateStatements   cyc=74   cog=179    deep nesting; cyclomatic understates it
String               cyc=71   cog=1      flat switch returning a string per case
operatorFromToken    cyc=21   cog=1
binaryCOperator      cyc=15   cog=1
```

A flat 71-case switch mapping tokens to strings is genuinely trivial to read, and
cognitive complexity says so. Cyclomatic says 71. Reporting only cyclomatic sends
someone to refactor `String()` methods; reporting only cognitive loses the
branch-count answer to "how many paths must a test cover", which is a different
question and the one cyclomatic is actually for.

## The dependency decision

**This RFC adds the first external dependencies this repository has ever had.**
`go.mod` is currently `module hexal` and a Go version — no `go.sum`, no
`vendor/`, nothing. That is stated plainly because it is the part of this RFC
most worth rejecting.

Verified footprint after `go mod tidy`:

```
require (
	github.com/fzipp/gocyclo v0.6.0
	github.com/uudashr/gocognit v1.2.1
)
require golang.org/x/tools v0.42.0 // indirect
```

Two direct, one indirect, ten `go.sum` lines. `gocyclo`'s own `go.mod` is two
lines — it requires nothing. The single indirect module comes from `gocognit`,
which also ships a `go/analysis` Analyzer alongside its plain `Complexity`
function. The wider module *graph* lists more, but those are `x/tools`' own
requirements and are not in the build list.

### What was considered and rejected

**A separate `metrics/` module.** Go has no test-only dependency scope — no Cargo
`[dev-dependencies]`, no Maven `<scope>test</scope>` — so the only way to keep the
root `go.mod` literally empty is a second module. It would have worked cleanly
here, since the report never imports `hexal`: no `replace` directive, no cycle.
Rejected because a second module is invisible to `go test ./...`, `go vet ./...`,
and `go build ./...`, and with no CI in this repository invisible means unrun.
Cohesion in one place beats an empty manifest for code that is not on any hot
path.

**A separate app in the same module, in `workbench/`'s shape.** This does not
achieve dependency isolation at all: `workbench/` is `package main` inside
`module hexal`, so a sibling app's dependencies land in the root manifest exactly
as a test package's would. The isolation would come from a module boundary, not
from being a distinct binary.

**Implementing both metrics over `go/ast` ourselves.** Keeps the repo at zero
dependencies at the cost of owning a cognitive-complexity implementation whose
correctness is defined by an external specification. One dependency set confined
to `compiler/tests/benchmarks/` is the better trade.

**Moving the timing benchmarks out of `go test -bench`.** `testing.Benchmark` makes
a standalone runner feasible — it returns `BenchmarkResult` carrying `NsPerOp`,
`AllocsPerOp`, `AllocedBytesPerOp`, and the `Extra` map `ReportMetric` feeds — but
it would mean re-implementing `-benchtime`, `-count`, `-cpuprofile`, `-memprofile`,
`-bench` regex selection, and benchstat-compatible output. The benchmarks stay
under `go test -bench`; only their directory changes.

## Invariants

1. No compiler behavior changes. Generated C is byte-identical and the snippet
   SHA-256 manifest does not move.
2. **No non-test file anywhere imports a third-party module.** A test in
   `compiler/tests/benchmarks/` scans every non-test `.go` file under `compiler/`
   and `workbench/` for an import path that is neither stdlib nor `hexal/...`, and
   fails if it finds one. Phrased on non-test files rather than on directories
   because the benchmark package now lives under `compiler/`; the guarantee is the
   same one that matters — nothing the compiler or workbench builds or links
   changes — and it stays a one-pass check rather than an assertion in prose.
3. The traversal counter does not exist in an untagged build. `walkProgram`
   compiles with no counter and no global state.
4. Nothing added here asserts a threshold, on time, allocation, or complexity.
   Every number is reported and compared deliberately by a human.
5. The ten-second budget from RFC 0075 still holds for
   `-bench . -benchtime 1x`; the complexity report is not a benchmark and runs
   separately.

## Validation

- `go test ./...`, `go vet ./...`, `go vet -tags c23 ./compiler/tests/c23validation`,
  `go vet -tags benchmetrics ./compiler/generator ./compiler/tests/benchmarks`.
- `go build ./compiler/... ./workbench` links no third-party module; the
  containment test proves it rather than asserting it in prose.
- `go test -bench . -benchmem -benchtime 1x ./compiler/tests/benchmarks` — now one target, not
  two — completes under ten seconds and reports MB/s for all thirteen benchmarks.
- `go test -tags benchmetrics -bench Corpus -benchtime 1x ./compiler/tests/benchmarks` reports
  non-zero `walks/op` and `nodes/op`.
- Each of the five failure benchmarks is asserted to fail by an ordinary test.
- `go test -run TestComplexityReport -v ./compiler/tests/benchmarks` reports every non-test file
  under `compiler/` and asserts a non-zero function count.
- `go test ./compiler` declares no benchmark, and `AGENTS.md:203` is corrected to
  say so.
- The snippet manifest is unchanged.
- `docs/benchmarks.md` and `docs/complexity.md` both carry a dated, timed entry
  naming the Go version and CPU, under a Measurement history section that is
  appended to and never overwritten, so a trend is readable without git.

**A note for whoever re-baselines.** `docs/benchmarks.md`'s committed figures are
already stale, for reasons that predate this RFC: measured 2026-08-18 against the
2026-08-17 baseline, `allocs/op` is up across the board — Corpus 78,303 → 80,230
(+2.5%), GenericsHeavy 3,405 → 3,793 (+11.4%), Failure 161 → 179 (+11.2%). That
window covers RFCs 0072, 0073, 0074, 0076, and 0079; RFC 0073's `CanonicalKey`
building a string per interned type is the obvious first suspect but has not been
bisected. Adding MB/s and five failure benchmarks forces a re-baseline anyway.
**Do not attribute that drift to this RFC**, and do not treat re-baselining as
having absorbed it: by RFC 0075's own policy allocation count is the primary
signal, so a 2.5% corpus regression deserves its own look, separately.

## Non-goals

- Any optimization. This RFC measures; RFC 0075's policy governs what a
  measurement then authorizes.
- Thresholds, ratchets, or CI gates on any metric.
- Complexity of Hexal programs the compiler compiles. This measures the
  compiler's own Go source only.
- Halstead measures and the Maintainability Index. Prototyped, measured, and
  dropped as redundant — see Why only two. Reopening either needs evidence that
  beats r² = 0.92 against line count.
- **Per-phase durations. Considered and declined — the granularity is not
  wanted.** `CompilationStats` already tracks `LexDuration`, `CheckDuration`, and
  `GenerateDuration`, and `b.ReportMetric` would surface them for a few lines of
  work, so the omission is a decision rather than an oversight. Against it: at the
  `-benchtime 1x` the ten-second budget forces, each phase figure would be a
  single sample — three noisier numbers in place of one already-noisy one — and it
  would give no per-phase *allocation* attribution, since `CompilationStats`
  records no allocation fields. Allocation count is the primary signal under
  RFC 0075's policy, so the metric would add noise where the signal is weakest.
  Whoever wants stage attribution should reach for a memory profile instead.
- Peak memory, GC pause time, generated-C size, and scaling curves against module
  count. All real gaps; none has a caller asking for it yet. `B/op` is cumulative
  bytes allocated, not high-water heap, so nothing here would catch a peak-RSS
  regression.
- Replacing `gofmt`/`go vet` with a linter, or adopting `golangci-lint`.
- Acting on the complexity numbers in this RFC. `validateExpressionNode` at 267
  is a finding, not a task; whoever takes it needs its own spec.

## Drawbacks

- **The root `go.mod` is no longer empty, and a `go.sum` now exists.** This is the
  accepted cost, not an oversight: `go test ./...` on a clean checkout with no
  module cache will fetch three modules, where today it fetches none. `AGENTS.md`'s
  rule that "`go test ./...` must pass with no external toolchain installed" is
  unaffected — a Go module is not a toolchain — but the property that this repo
  built from nothing but a Go install is weaker, and someone auditing or vendoring
  `hexal` now sees two third-party names in the manifest. The containment test
  keeps them from spreading; it cannot make them invisible.
- Moving `compiler/bench_test.go` rewrites its blame. Unavoidable, and the file
  is three months old.
- A build tag is a second compilation configuration for `generator`, and tagged
  code is not compiled by a default `go build`. `go vet -tags benchmetrics` in
  Validation exists to stop it rotting.
- Five failure benchmarks in place of one make the suite thirteen, tightening the
  ten-second budget. Failure paths are the cheapest programs in the suite, so the
  margin is not at risk, but the budget should be re-checked when it changes.
