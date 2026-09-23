# RFC 0237: Observability and Analysis Harness

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation ready. Scope is measurement and documented gates; the
  `go/analysis` restructure of the source-policy checks is cut and recorded
  under Rejected alternatives. Phase 0 is independently landable and adds no
  code
- Created: 2026-09-22
- Updated: 2026-09-23
- Blocked by: nothing. The Windows `replaceFile` build break that previously
  blocked every Validation item is fixed; `go build ./...` and `go vet ./...`
  are clean
- Origin: go.dev/doc/diagnostics names four facilities — profiling, tracing,
  debugging, and runtime statistics and events — that a Go project can use to
  find expensive sections and latency. The harness has tests, benchmarks,
  complexity, fuzzing, and source-policy analysis, but only the cheapest corner
  of the first facility, and none of the four is reachable in one documented
  place
- Depends on: the benchmarking policy in `docs/benchmarks.md`, the existing
  suites under `compiler/tests/` (`benchmarks`, `fuzz`, `integration`,
  `c23validation`), and the third-party confinement invariant
  (`TestNoThirdPartyImportsOutsideBenchmarks`)
- Does not update: `docs/reference.md`. Nothing here is language-visible: no
  syntax, semantics, signature, or generated-C contract moves

## Decision summary

Add one new package, `compiler/tests/observability/`, holding the profiling,
tracing, and runtime-statistics harness. It is separate from
`compiler/tests/benchmarks/` because it measures a different thing: benchmarks
measure *how fast and how much* a compilation is, and are compared as ratios
across a suite run; observability measures *where the cost is*, is consumed by
a human reading a profile or a trace, and has no cross-run comparability at
all. Folding both into one package would put two unrelated result shapes, two
exit criteria, and two reading habits behind one directory.

The source-policy checks are left exactly as they are. Restructuring them onto
`go/analysis` was considered and rejected; see Non-goals.

Every number this spec produces lands as a dated entry in `docs/benchmarks.md`,
appended, never overwritten.

## Harness inventory

Recorded so no category is silently missing a home.

| Category | Where | State |
| --- | --- | --- |
| Unit and integration tests | `compiler/tests/integration`, per-package `_test.go` | exists |
| Benchmarks | `compiler/tests/benchmarks` | exists, `docs/benchmarks.md` history |
| Complexity | `compiler/tests/benchmarks/complexity_test.go` | exists |
| Fuzzing | `compiler/tests/fuzz` — `FuzzLex`, `FuzzParse`, `FuzzCompile`, `FuzzCompileMultiModule` | exists |
| Source-policy analysis | eight files, fourteen parse sites: `architecture_policy_test.go`, `comment_policy_test.go`, `grammar_test.go`, `compiler/checker/contextual_forms_test.go`, and the registry, error, dispatch, and trap inventory tests | exists, hand-rolled, unchanged by this spec |
| Generated-artifact regression | `workbench/snippets/testdata/generated-c-sha256.json` | exists |
| External-toolchain tiers | `compiler/tests/c23validation` — tagged suite, qualified profile, UBSan, leak | exists |
| Driver output measurement | `TestProgramAndEntropyMeasurements`, `docs/benchmarks.md` build-mode section | exists |
| **Profiling** | — | **this spec** |
| **Tracing** | — | **this spec** |
| **Runtime statistics** | — | **this spec** |
| **Race detection** | documented ad hoc only, `./workbench/snippets/...` | **this spec** |
| **Coverage** | — | **this spec** |
| **Order randomization and repetition** | — | **this spec** |
| Debugging | human workflow; generated-C side has trap fixtures and UBSan | out of scope; the reproducible-build flags are noted under Non-goals and nothing else is added |
| Dependency vulnerability scan | — | out of scope, below |
| Third-party linters | — | out of scope, below |

## What to add

### 1. Profiling — `compiler/tests/observability`

- `TestCPUProfileCapture` and `TestHeapProfileCapture` wrap a real
  `compiler.Compile` in `runtime/pprof`, then assert each profile parses and
  holds at least one sample. A profile that silently captured nothing fails
  rather than reporting an empty measurement.
- The CPU capture is one catalog-corpus compile, and that is sized, not assumed.
  `runtime/pprof` samples at 100 Hz, so a capture window shorter than a few
  sampling intervals can produce a profile with zero samples — a flake, not a
  finding. One corpus compile measures 119-137 ms on the reference machine, is
  twelve or more sampling intervals wide, and captured compiler frames on 15 of
  15 consecutive runs. No repetition loop is required. The test does not call
  `runtime.SetCPUProfileRate` to force samples: that changes the thing being
  measured. The heap profile has no such hazard, because a compile allocates
  unconditionally.
- **Neither capture needs a profile parser, and none is added.** `runtime/pprof`
  writes a CPU profile as gzipped protobuf, and the canonical parser
  (`github.com/google/pprof/profile`) is not in this module's graph; adding it
  would be a new direct dependency for one assertion. It is not needed. The
  protobuf string table stores function names as plain bytes, so decompressing
  with `compress/gzip` and searching for `hexal/compiler` proves compiler frames
  were sampled. That check discriminates rather than passing vacuously: a 5 ms
  control window containing no compilation yields a well-formed profile in which
  the search finds nothing. The heap profile needs even less —
  `pprof.Lookup("heap").WriteTo(w, 1)` emits stdlib text carrying `heap profile:`
  and the sampled frames, inspectable with `strings`.
- Profile and trace bytes are written to an in-memory buffer and read there. No
  test writes a profile, trace, or coverage file to disk, so the scratch-file
  rule has nothing to govern here and `go test ./...` leaves no artifact behind.
- The package comment documents the direct route as well:
  `go test -bench . -benchtime 1x -cpuprofile cpu.out -memprofile heap.out
  ./compiler/tests/benchmarks`.
- Block and mutex profiling are documented and not exercised: the compiler's
  own path is single-goroutine, so a contention profile says nothing a reader
  of the frontend can act on.

### 2. Tracing — `compiler/tests/observability`

- `BenchmarkTraceCorpus` (`-tags observability`) starts `runtime/trace` around
  one catalog compile, stops it into a buffer, parses it, and reports event
  counts with `b.ReportMetric`, broken out by the kinds a compile actually
  produces: `StateTransition` and `Range`.
- `TestTraceRecordsEvents` compiles one fixed program under `trace.Start` and
  asserts the trace parses and holds at least one `StateTransition` event.

**Spans and tasks are deliberately not reported.** An earlier draft counted
them. They are user annotations — `trace.NewTask`, `trace.StartRegion` — and the
compiler creates none, so both counts would be a constant zero. Measured over
one corpus compile, the 10,177 events break down as `Metric` 6200,
`StateTransition` 3263, `Label` 526, and `RangeBegin`/`RangeEnd` 75 each, with no
`Task` or `Region` event at all. Reporting a span count would be the same
stable-zero failure this spec rejects for the goroutine metric, and asserting
"at least one span" would simply fail. Adding `trace.StartRegion` calls to the
compiler to make the number non-zero is rejected for the reason the tag
discussion below gives: it is instrumentation in non-test compiler source, and
it would mean tracing measures an annotated compiler rather than the shipping
one.
- Parsing uses `golang.org/x/exp/trace`, which the module already requires, so
  no trace is ever written to a file and no external tool reads one. That parser
  tracks the Go execution-trace wire format, which is versioned and can change
  with a Go release. A parse failure after a toolchain upgrade is an expected
  maintenance event on this benchmark, not a compiler finding; it is fixed by
  updating the parser, and nothing gates on it in the meantime.

### Why these do not reuse `-tags benchmetrics`

`benchmetrics` is not a test selector. It swaps a compiler source file:
`compiler/generator/traversal_metrics.go` is `//go:build !benchmetrics` with
no-op `countTraversal`/`countNode`, and `traversal_metrics_bench.go` replaces
them under the tag with real package-level counters.

A trace or a `runtime/metrics` delta captured under `benchmetrics` therefore
measures an instrumented compiler, and the allocation and timing figures it
reports include the counters' own cost. That is precisely the compiler these
facilities exist to *not* measure. The observability benchmarks take their own
`observability` tag, which gates test files only and changes no compiler source.
The two tags are never required together.

### 3. Runtime statistics and events — `compiler/tests/observability`

- `BenchmarkRuntimeMetricsCorpus` (`-tags observability`) samples a fixed metric
  set around one catalog compile and reports each delta with `b.ReportMetric`:
  `/memory/classes/heap/objects:bytes`, `/gc/heap/allocs:bytes`, and
  `/gc/cycles/total:gc-cycles` — three the compiler's own behaviour moves.
- `/sched/goroutines:goroutines` is deliberately not in that set. The compiler's
  path is single-goroutine, which is the same reason block and mutex profiling
  are documented and not exercised; a goroutine count would report harness and
  GC noise and could never move on a frontend change. Reporting a metric the
  measured code cannot affect is the stable-zero failure mode
  `TestRuntimeMetricsMove` exists to prevent. Measured across one corpus compile,
  the three selected metrics all move and the goroutine count does not: it reads
  21 before and 21 after.
- `TestRuntimeMetricsMove` compiles a non-trivial program between two
  `runtime/metrics` reads and asserts the allocation counter advanced, so a
  metric name that stops resolving fails loudly instead of reporting a stable
  zero.

### 4. Race detection

- Document `go test -race ./...` beside `go vet` as a required correctness
  gate, extending the narrower `go test -race ./workbench/snippets/...` run that
  RFC 0140 established. A race is a correctness failure, not a measurement, so
  the no-threshold policy does not apply to it.
- The gate has a prerequisite the other documented runs do not: `-race` requires
  `CGO_ENABLED=1` and a working C toolchain, so on Windows it needs an installed
  gcc. That does not breach the rule that `go test ./...` passes with no
  external toolchain — this is a separate invocation — but the gate is not free
  and the requirement is stated where it is documented.
- This gate was unmeetable while `internal/driver` had no Windows `replaceFile`
  and the package failed to build there. That is fixed:
  `internal/driver/replace_windows.go` supplies the `MoveFileEx` replace, and
  `go build ./...` and `go vet ./...` are clean, so the gate can now be
  evaluated on Windows.
- No package holds a race-detector guard, because a race cannot be detected
  from inside the process that has it; the gate is the run mode itself.

### 5. Coverage

- Document `go test -coverprofile=cover.out ./...` and `go tool cover
  -func=cover.out` as an opt-in run beside the profiling invocations.
- No threshold is added, and no coverage number is asserted. This is a
  measurement `docs/status.md`'s qualitative "Known coverage gaps" list has
  never had, and it is the input for deciding which gap to close next.

### 6. Order randomization and repetition

- Document `go test -shuffle=on -count=2 ./...` as the flake-detection run:
  shuffling exposes inter-test order dependence and a second count exposes
  time- or state-dependence that one pass cannot.

## Constraints

- **Measure, never gate** — except for race detection and the existing `go vet`
  and correctness gates, which are not measurements. No threshold, budget, or
  assertion on any reported profile, trace, metric, or coverage number.
- **Append, never overwrite.** Every measurement this spec produces lands as a
  dated entry in `docs/benchmarks.md` under Measurement history, newest first,
  with the machine and Go version and one line on what moved, exactly as the
  existing entries do. A new section per facility is added to that file rather
  than a fourth prose document.
- **Ordinary tests are pure Go.** No test invokes a debugger, `go tool cover`,
  `govulncheck`, `staticcheck`, or `go test` on itself. Profile and trace bytes
  are produced and consumed in-process by `runtime/pprof`, `runtime/trace`, and
  `golang.org/x/exp/trace`.
- **Suite budget.** `docs/benchmarks.md` records the benchmarks suite at 3.9s
  against its ten-second budget, leaving roughly 6.1s. The observability
  additions were measured against that headroom rather than assumed to fit:
  CPU capture ~130 ms, heap capture ~150 ms, trace capture 229 ms plus 3 ms to
  parse, metrics ~150 ms — about 0.7s total, an order of magnitude inside the
  remaining budget. `BenchmarkTraceCorpus` was the item at risk and is not:
  the trace is 112 KB and parses in 3 ms. Should a future corpus grow enough to
  threaten the budget, the trace benchmark drops the corpus for a smaller fixed
  program rather than the budget being raised. The package's untagged tests stay
  cheap enough for `go test ./...`.
- **Third-party imports stay confined.** Every addition lives in a `_test.go`
  file under `compiler/tests/observability`. `TestNoThirdPartyImportsOutsideBenchmarks`
  skips `_test.go` files entirely and inspects only non-test sources, so it
  *exempts* these additions rather than approving them — the binary is protected
  because nothing here is reachable from a non-test file, not because a walk
  vetted the imports. It follows that no addition under this spec may introduce
  a non-test helper file or a non-test package; doing so would fail that walk,
  correctly.

## Implementation plan

Every phase leaves the tree green. Phase 0 adds no code and can land alone;
phases 2, 3, and 4 are independent of each other once phase 1 exists.

### Phase 0 — documented gates, no new code

Sections 4, 5, and 6 are documentation of `go` commands that already work. They
add no file under `compiler/` and gate nothing that is not already gated.

1. Add a "Diagnostic and correctness runs" section to `docs/benchmarks.md`
   recording, each with one line on what it catches and when to reach for it:
   `go test -race ./...`, `go test -coverprofile=cover.out ./...` with
   `go tool cover -func=cover.out`, and `go test -shuffle=on -count=2 ./...`.
2. Record with `-race` that it requires `CGO_ENABLED=1` and an installed C
   toolchain, so a reader on Windows knows why it is not part of
   `go test ./...`.
3. Run all three on the current tree and confirm each completes. They pass
   today; `internal/driver/replace_windows.go` closed the Windows build break
   that previously made this unmeetable.

No `docs/benchmarks.md` measurement entry is produced by this phase: race,
coverage, and shuffle report no number this project tracks.

### Phase 1 — package skeleton and the `observability` tag

1. Create `compiler/tests/observability/` as `package observability`, test files
   only. No non-test file is added here, in this phase or any later one: the
   third-party confinement walk inspects only non-test sources, so a non-test
   file in this package would be the one way these additions could reach the
   compiler binary.
2. Write the package comment as the entry-point narrative: what this package
   measures, how it differs from `compiler/tests/benchmarks`, and the direct
   invocation a human should use —
   `go test -bench . -benchtime 1x -cpuprofile cpu.out -memprofile heap.out ./compiler/tests/benchmarks`.
3. Establish the `observability` build tag. Unlike `benchmetrics`, it constrains
   test files only and pairs with no `//go:build !observability` sibling,
   because it selects benchmarks rather than swapping a compiler implementation.
   The untagged package still holds every `Test*` function; only the `Benchmark*`
   functions sit behind the tag.
4. Add the corpus loader the later phases share: load `snippets.Load()`, flatten
   to `{sources, entrypoint}` pairs once, and keep a package-level sink for the
   `CompilationResult` so no compile is optimized away. This mirrors
   `compiler/tests/benchmarks/corpus_test.go` rather than inventing a second
   shape.
5. Confirm `go test ./...`, `go vet ./...`, and
   `TestNoThirdPartyImportsOutsideBenchmarks` all still pass with the empty
   package present.

### Phase 2 — profiling

1. `TestCPUProfileCapture`: `pprof.StartCPUProfile` into a `bytes.Buffer`, one
   corpus compile, `StopCPUProfile`. Decompress with `compress/gzip` and assert
   the bytes contain `hexal/compiler`.
2. `TestHeapProfileCapture`: one corpus compile, then
   `pprof.Lookup("heap").WriteTo(&buffer, 1)`, asserting the text holds
   `heap profile:` and a `hexal/compiler` frame.
3. Add the negative control that keeps the CPU assertion honest: a capture
   window containing no compilation must *not* find compiler frames. Without it
   the assertion could pass on a profile that captured nothing, which is the
   failure this item exists to prevent.
4. Document block and mutex profiling in the package comment as available and
   deliberately unexercised, naming the single-goroutine reason.

### Phase 3 — runtime statistics and events

1. `TestRuntimeMetricsMove`: read the metric set, run one corpus compile, read
   again, assert `/gc/heap/allocs:bytes` advanced. A metric name that stops
   resolving reports `metrics.KindBad`, which fails rather than reading zero.
2. `BenchmarkRuntimeMetricsCorpus` behind the `observability` tag: sample
   `/memory/classes/heap/objects:bytes`, `/gc/heap/allocs:bytes`, and
   `/gc/cycles/total:gc-cycles` around one corpus compile and report each delta
   with `b.ReportMetric`.
3. Assert every name in the set resolves, so a Go upgrade that renames one fails
   loudly at the point of use rather than silently reporting a stable zero.

### Phase 4 — tracing

1. `TestTraceRecordsEvents`: `trace.Start` into a buffer, compile one fixed
   program, `trace.Stop`, parse with `golang.org/x/exp/trace`, and assert at
   least one `StateTransition` event. Not spans: the compiler emits no user
   annotations, so a span assertion fails and a span count is always zero.
2. `BenchmarkTraceCorpus` behind the `observability` tag: capture and parse one
   corpus compile, reporting `StateTransition` and `Range` counts.
3. Record in a comment beside the parser call that `golang.org/x/exp/trace`
   tracks the Go execution-trace wire format, so a parse failure after a
   toolchain upgrade is a maintenance event on this benchmark and not a compiler
   finding.
4. Confirm the tagged benchmark suite still fits the ten-second budget with the
   trace benchmark present.

### Phase 5 — measurement transcription and full gate

1. Run the tagged suite and transcribe its numbers into `docs/benchmarks.md` as
   a dated entry under Measurement history, newest first, with machine and Go
   version, in a new section per facility. Append; overwrite nothing.
2. Run the full gate: `go test ./...`, `go vet ./...`, `gofmt -l`,
   `go test -race ./...`, the untagged and `-tags observability` benchmark runs,
   and `go vet -tags c23`.
3. Confirm the snippet manifest has not moved. This spec adds no non-test file
   and changes no generated byte, so any movement is a defect in the change, not
   an expected rebaseline.
4. Confirm the compiler binary's dependency set is unchanged and
   `go build ./compiler/...` still links no third-party library.

## Reference synchronization

None. Nothing in this spec is language-visible: no syntax, semantics,
signature, diagnostic, or generated-C contract moves, so `docs/reference.md` is
unaffected. `docs/benchmarks.md` is the document this work writes to, and
`docs/status.md` gains no entry because nothing here is an open bug or TODO.

## Validation

This section is exhaustive.

- `go test ./...` passes unchanged, with no additional external tool
  installed.
- `go test -bench . -benchmem -benchtime 1x ./compiler/tests/benchmarks`
  reports the existing seven success shapes, five failure shapes, and the
  corpus, unchanged, with allocation columns present.
- Both profile captures fail when the profile they produce is empty, and the
  documented direct invocation produces non-empty CPU and heap profiles.
- `TestTraceRecordsEvents` fails when the trace holds no `StateTransition`
  event, and `TestRuntimeMetricsMove` fails when the allocation metric does not
  advance. No reported trace figure is a span or task count, both of which a
  compile leaves at zero.
- Every `runtime/metrics` name in the reported set resolves: a name returning
  `metrics.KindBad` fails the test rather than contributing a zero delta.
- `-tags observability` adds the observability benchmarks, each reporting its
  metrics in the benchmark columns, and the untagged build compiles none of
  them. `-tags benchmetrics` continues to add the traversal benchmarks and
  nothing else; neither tag implies the other, and no observability measurement
  is taken from a build carrying the generator's traversal counters.
- The CPU capture finds compiler frames on every one of at least fifteen
  consecutive runs, and the negative control — a capture window with no
  compilation in it — finds none. The first shows the assertion is not a flake;
  the second shows it is not vacuous.
- No profile parser is added: `go.mod` gains no direct requirement, and the
  CPU and heap assertions use `compress/gzip` and `pprof`'s text mode from the
  standard library only.
- The tagged benchmark suite, trace benchmark included, completes inside the
  ten-second budget.
- The eight source-policy checks are untouched: each still reports its existing
  findings — a comment-policy violation, an unclassified trap literal, an
  undispatched expression-dispatch kind — under the same test names, from the
  same files, through `t.Errorf`.
- `TestNoThirdPartyImportsOutsideBenchmarks` still passes, `go build
  ./compiler/...` still compiles no third-party library, and the compiler
  binary's dependency set is unchanged.
- `go test -race ./...` is documented as a gate and passes on the tree.
- The documented coverage and shuffle invocations run to completion on the tree.
- The measurement each new benchmark reports is transcribed into
  `docs/benchmarks.md` as a dated entry with its machine and Go version, under a
  new section per facility, and no existing entry is overwritten. This is a
  process obligation on whoever runs the measurement, checked by review — no
  test can verify it, because `b.ReportMetric` writes to the benchmark log and
  the document is edited by hand. It is recorded here so the obligation is not
  lost, not as a condition a suite run can satisfy.
- Race, coverage, and shuffle produce no measurement, so they add documentation
  only and no `docs/benchmarks.md` entry.
- No reported profile, trace, metric, or coverage number is asserted against a
  threshold anywhere in the suite.
- `compiler/tests/observability` contains only `_test.go` files, and the
  snippet manifest is unmoved.

## Implementation readiness

Implementation-ready. Every open number is measured rather than estimated, on
the reference machine (windows/amd64, Go 1.26.4), and the figures the plan
depends on are recorded here so a later reader can tell whether they still hold:

| Quantity | Measured |
| --- | --- |
| One catalog-corpus compile | 146 ms/op |
| CPU capture, one corpus compile | 119-137 ms, 4.4-6.3 KB gzipped |
| CPU capture reliability | compiler frames found 15 of 15 runs |
| CPU negative control (5 ms, no compile) | no compiler frames, as required |
| Heap profile text mode | ~1.06 MB, carries `heap profile:` and hexal frames |
| Trace capture and parse | 112 KB, 10,177 events, 229 ms capture, 3 ms parse |
| Trace event kinds | Metric 6200, StateTransition 3263, Label 526, Range 75+75, Task/Region 0 |
| Metric movement over one compile | heap objects, gc allocs, gc cycles all move; goroutines flat at 21 |
| Total added suite cost | ~0.7s against 6.1s of remaining budget |

No design question is open. The two that were — how to validate a CPU profile
without adding a parser dependency, and what a trace of a single-goroutine
compile can honestly report — are settled above and in section 2. The Windows
`replaceFile` break that previously made every Validation item unevaluable is
fixed, so the gate runs on both hosts.

## Non-goals

- **Dependency vulnerability scanning.** `govulncheck` is worth an opt-in run,
  but it is an external tool whose database changes independently of the tree,
  so a finding is not a property of this commit. It gets no place in the
  ordinary suite.
- **Restructuring the source-policy checks onto `go/analysis`.** An earlier
  draft of this spec proposed giving each check an `*analysis.Analyzer` shape.
  It is cut. All eight checks are purely syntactic — none imports `go/types`,
  and they inspect imports, comment text, string literals, and switch cases —
  so the framework's reason for existing does not apply to them. With no driver
  and no `analysistest`, each test would have to hand-build an `analysis.Pass`
  and install its own `Report` collector to forward back to `t.Errorf`, which is
  one indirection added around a walk that already works. It promotes
  `golang.org/x/tools` from an indirect requirement to a direct import and
  detects nothing new. The case to revisit is a check that genuinely needs type
  information; that is the event that would make the framework earn its place,
  and it has not happened.
- **Third-party linters.** `staticcheck` and `golangci-lint` overlap `go vet`
  and the eight policy checks, and would be a second source of truth for the
  same rules. Adopting a linter driver is a later decision with its own
  argument.
- **CI.** The repository has no workflow definition, so nothing runs any of
  this automatically. Wiring one up is a separate decision about where the
  gates run, not about what is measured.
- **Testable examples.** The snippet catalog under `workbench/snippets` already
  holds one compiling, output-checked program per language facet; `Example`
  functions would be a second, weaker copy of that.
- **A debugger gate.** Debugging is interactive by nature; the spec covers the
  reproducible build (`go test -c -gcflags=all='-N -l'`) and nothing more.

## Rejected alternatives

- **Fold observability into `compiler/tests/benchmarks`.** The two measure
  different things and are read differently — ratios compared across runs
  versus a profile read once — and the benchmarks package carries a ten-second
  budget and a `-benchtime 1x` requirement that a trace capture does not.
  Sharing a package would couple two result shapes and two exit criteria.
- **`net/http/pprof` and a long-running measurement server.** It makes a
  measurement depend on an out-of-process client and a port, and every one of
  these measurements is already reachable synchronously inside a test binary.
- **A coverage threshold.** It would turn a diagnostic into a target, which is
  exactly the failure mode `docs/benchmarks.md` exists to avoid for
  allocations.
