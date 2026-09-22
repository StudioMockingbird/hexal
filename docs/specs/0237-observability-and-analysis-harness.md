# RFC 0237: Observability and Analysis Harness

- Kind: Feature Specification (Rust-Style RFC)
- Status: Proposed
- Created: 2026-09-22
- Updated: 2026-09-22
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

The source-policy checks adopt `go/analysis`'s `Analyzer` shape where they
already live; they do not move, because each one's value comes from sitting
next to the files it inspects.

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
| Source-policy analysis | `architecture_policy_test.go`, `comment_policy_test.go`, and the trap, error, and dispatch inventory tests | exists, hand-rolled |
| Generated-artifact regression | `workbench/snippets/testdata/generated-c-sha256.json` | exists |
| External-toolchain tiers | `compiler/tests/c23validation` — tagged suite, qualified profile, UBSan, leak | exists |
| Driver output measurement | `TestProgramAndEntropyMeasurements`, `docs/benchmarks.md` build-mode section | exists |
| **Profiling** | — | **this spec** |
| **Tracing** | — | **this spec** |
| **Runtime statistics** | — | **this spec** |
| **Race detection** | documented ad hoc only, `./workbench/snippets/...` | **this spec** |
| **Coverage** | — | **this spec** |
| **Order randomization and repetition** | — | **this spec** |
| Debugging | human workflow; generated-C side has trap fixtures and UBSan | this spec, documentation only |
| Dependency vulnerability scan | — | out of scope, below |
| Third-party linters | — | out of scope, below |

## What to add

### 1. Profiling — `compiler/tests/observability`

- `TestCPUProfileCapture` and `TestHeapProfileCapture` wrap a real
  `compiler.Compile` in `runtime/pprof`, then assert each profile parses and
  holds at least one sample. A profile that silently captured nothing fails
  rather than reporting an empty measurement.
- The package comment documents the direct route as well:
  `go test -bench . -benchtime 1x -cpuprofile cpu.out -memprofile heap.out
  ./compiler/tests/benchmarks`.
- Block and mutex profiling are documented and not exercised: the compiler's
  own path is single-goroutine, so a contention profile says nothing a reader
  of the frontend can act on.

### 2. Tracing — `compiler/tests/observability`

- `BenchmarkTraceCorpus` (`-tags benchmetrics`) starts `runtime/trace` around
  one catalog compile, stops it into a buffer, parses it, and reports span and
  task counts with `b.ReportMetric`.
- `TestTraceRecordsSpans` compiles one fixed program under `trace.Start` and
  asserts at least one span was recorded.
- Parsing uses `golang.org/x/exp/trace`, which the module already requires, so
  no trace is ever written to a file and no external tool reads one.

### 3. Runtime statistics and events — `compiler/tests/observability`

- `BenchmarkRuntimeMetricsCorpus` (`-tags benchmetrics`) samples a fixed metric
  set around one catalog compile and reports each delta with `b.ReportMetric`:
  `/memory/classes/heap/objects:bytes`, `/gc/heap/allocs:bytes`, and
  `/sched/goroutines:goroutines` — the three the compiler's own behaviour can
  move.
- `TestRuntimeMetricsMove` compiles a non-trivial program between two
  `runtime/metrics` reads and asserts the allocation counter advanced, so a
  metric name that stops resolving fails loudly instead of reporting a stable
  zero.

### 4. Race detection

- Document `go test -race ./...` beside `go vet` as a required correctness
  gate, extending the narrower `go test -race ./workbench/snippets/...` run that
  RFC 0140 established. A race is a correctness failure, not a measurement, so
  the no-threshold policy does not apply to it.
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

### 7. Source-policy analysis

The five source-policy checks each hand-roll the same `go/ast` + `go/parser` +
`go/token` walk. Adopt `go/analysis`'s `Analyzer` shape for them, in place:

- Each check becomes an `*analysis.Analyzer` with `Name`, `Doc`, `Run`, and an
  explicit `Requires` set, so the walk, the diagnostic construction, and the
  reporting policy are declared once per check.
- The analyzers are driven from tests over `go/parser`-produced ASTs, filling
  `analysis.Pass.Fset`, `Files`, and `TypesInfo` directly.
  `golang.org/x/tools` is not currently needed at all — `go mod why
  golang.org/x/tools` reports the main module does not need it — so this
  adoption adds back exactly the packages it uses.
- `Pass.Report` and `analysis.Diagnostic`'s position and suggested-fix fields
  replace the ad-hoc `t.Errorf` calls.

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
- **Suite budget.** The benchmarks suite runs under a ten-second budget with
  `-benchtime 1x`; the observability package's benchmarks stay within the same
  budget and its unhidden tests stay cheap enough for `go test ./...`.
- **Third-party imports stay confined.** Every addition lives in a `_test.go`
  file under `compiler/tests/observability`, which the existing confinement
  walk already covers, so the compiler binary gains nothing.

## Validation

This section is exhaustive.

- `go test ./...` passes unchanged, with no additional external tool
  installed.
- `go test -bench . -benchmem -benchtime 1x ./compiler/tests/benchmarks`
  reports the existing seven success shapes, five failure shapes, and the
  corpus, unchanged, with allocation columns present.
- Both profile captures fail when the profile they produce is empty, and the
  documented direct invocation produces non-empty CPU and heap profiles.
- `TestTraceRecordsSpans` fails when the trace holds no span, and
  `TestRuntimeMetricsMove` fails when the allocation metric does not advance.
- `-tags benchmetrics` adds the observability benchmarks beside the existing
  traversal benchmarks, each reporting its metrics in the benchmark columns,
  and the untagged build compiles neither counter.
- Every source-policy check is reachable as a named `analysis.Analyzer` and
  reports through `analysis.Pass`, and each check's existing findings — a
  comment-policy violation, an unclassified trap literal, an undispatched
  expression-dispatch kind — are still reported by the same test names from the
  same files.
- `TestNoThirdPartyImportsOutsideBenchmarks` still passes, `go build
  ./compiler/...` still compiles no third-party library, and the compiler
  binary's dependency set is unchanged.
- `go test -race ./...` is documented and passes on the tree as it stands.
- The documented coverage and shuffle invocations run to completion on the tree
  as it stands.
- Every measurement produced lands as a dated `docs/benchmarks.md` entry with
  its machine and Go version; no existing entry is overwritten.
- No reported profile, trace, metric, or coverage number is asserted against a
  threshold anywhere in the suite.

## Non-goals

- **Dependency vulnerability scanning.** `govulncheck` is worth an opt-in run,
  but it is an external tool whose database changes independently of the tree,
  so a finding is not a property of this commit. It gets no place in the
  ordinary suite.
- **Third-party linters.** `staticcheck` and `golangci-lint` overlap `go vet`
  and the five policy checks, and would be a second source of truth for the
  same rules. The `go/analysis` adoption makes the checks uniform first;
  adopting a linter driver on top is a later decision with its own argument.
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
- **`analysis/analysistest` and `go/packages` for the policy checks.**
  `packages.Load` shells out to `go list`, which makes an ordinary test depend
  on the Go toolchain being present and invocable, against the rule that
  ordinary tests are pure Go. The `Analyzer` shape is what those checks need;
  the golden-file driver is not.
- **`net/http/pprof` and a long-running measurement server.** It makes a
  measurement depend on an out-of-process client and a port, and every one of
  these measurements is already reachable synchronously inside a test binary.
- **A coverage threshold.** It would turn a diagnostic into a target, which is
  exactly the failure mode `docs/benchmarks.md` exists to avoid for
  allocations.
