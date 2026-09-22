# RFC 0237: Diagnostics and Analysis Harness

- Kind: Feature Specification (Rust-Style RFC)
- Status: Proposed
- Created: 2026-09-22
- Updated: 2026-09-22
- Origin: go.dev/doc/diagnostics names four facilities — profiling, tracing,
  debugging, and runtime statistics and events — that a Go project can use to
  find expensive sections and latency. The measurement harness under
  `compiler/tests/benchmarks` covers throughput and allocation counts but only
  touches the first category's cheapest corner, and none of the four is
  currently reachable in one documented place
- Depends on: the existing measurement harness
  (`compiler/tests/benchmarks`), its benchmarking policy, and the third-party
  confinement invariant
  (`TestNoThirdPartyImportsOutsideBenchmarks`)
- Does not update: `docs/reference.md`. Nothing here is language-visible: no
  syntax, semantics, signature, or generated-C contract moves

## Decision summary

Extend the existing measurement harness rather than add a parallel one. Every
addition is a Go-side measurement of the compiler's own process, lives in the
benchmarks package's `_test.go` files, and asserts nothing about the numbers it
reports — the benchmarking policy stands: measure, report, compare by hand.

Profile, trace, and runtime-metric collection are reachable through Go's own
facilities (`go test -cpuprofile`/`-memprofile`, `runtime/trace`,
`runtime/metrics`) with no new dependency, and the source-policy checks adopt
`golang.org/x/tools/go/analysis`'s `Analyzer` shape without taking its
`go/packages` driver.

## Coverage today

Recorded so the additions do not re-invent what exists:

- **Throughput and allocation counts.** Seven success-path benchmarks
  (`Benchmark{Scalar,GenericsHeavy,MultiModule,Collections,Text,Concurrency,
  ErrorPaths}`) and the whole-catalog `BenchmarkCorpus` call `b.ReportAllocs()`
  and `b.SetBytes`, so `-benchmem` already reports allocs/op, B/op, and MB/s.
- **Failure and diagnostic paths.** Five benchmarks
  (`BenchmarkFailure{Lex,Parse,Resolve,Check,Many}`) cover each failing stage,
  and `TestFailureBenchmarkProgramsFail` /
  `TestFailureManyReportsManyDiagnostics` prove they actually fail and that the
  at-volume shape reports many diagnostics.
- **Custom counters.** `-tags benchmetrics` adds walk and node counts through
  `b.ReportMetric`, with `TestTraversalCounterRecords` guarding that the
  counter is not silently zero.
- **Source complexity.** `TestComplexityReport` measures every compiler
  function with gocyclo and gocognit.
- **Generated-C runtime.** `TestProgramAndEntropyMeasurements` in
  `compiler/tests/c23validation` records generated size, build-and-link time,
  runtime allocation statistics, and per-run latency; UBSan reruns every
  runnable fixture, and the trap fixtures cover trap behaviour.

Not covered by anything: CPU and heap profiles, execution traces, Go runtime
metrics, and goroutine or GC observation of the test process itself.

## What to add

### 1. Profiling

No code is required for the basic route — `go test -bench BenchmarkCorpus
-benchtime 1x -cpuprofile cpu.out -memprofile heap.out
./compiler/tests/benchmarks` already works. What is missing is discoverability
and a guard:

- A package comment on `shared_test.go` naming the profile invocations beside
  the benchmark and report invocations it already documents.
- `TestProfileCapture` writes a CPU profile and a heap profile around a real
  compile using `runtime/pprof` directly, then asserts each profile parses and
  contains at least one sample. Analogous to `TestTraversalCounterRecords`,
  this is the guard that keeps the documented route from rotting silently.

Block and mutex profiles are in scope for the documentation only: the compiler
is single-goroutine on its own path, so a contention profile says nothing a
reader of the frontend can act on.

### 2. Tracing

- A `-tags benchmetrics` benchmark, `BenchmarkTraceCorpus`, starts
  `runtime/trace` around one catalog compile, stops it into a buffer, and
  reports span and task counts with `b.ReportMetric`.
- Tracing is parsed in-process with `golang.org/x/exp/trace`, which the module
  already requires directly. The trace is never written to a file and no
  external tool reads it, so the ordinary-test rule holds.
- `TestTraceRecordsSpans` compiles a fixed program under `trace.Start`, parses
  the result, and asserts at least one span was recorded, so a trace that
  captured nothing fails rather than reporting zero as a measurement.

### 3. Runtime statistics and events

- A `-tags benchmetrics` benchmark, `BenchmarkRuntimeMetricsCorpus`, samples a
  fixed metric set around one catalog compile and reports each delta with
  `b.ReportMetric`. The set is the three the compiler's own behaviour can move:
  `/memory/classes/heap/objects:bytes`, `/gc/heap/allocs:bytes`,
  `/sched/goroutines:goroutines`.
- `TestRuntimeMetricsMove` compiles a non-trivial program between two
  `runtime/metrics` reads and asserts the allocation counter advanced. Like
  the traversal guard, it exists so a metric name that silently stops
  resolving fails loudly instead of reporting a stable zero.

### 4. Debugging

Debugging is a human workflow, not a measurement, and the repo has no
debugger-driven gate. The spec therefore covers reproducibility only:

- Document, beside the profiling invocations, the optimizations-off debug
  build (`go test -c -gcflags=all='-N -l' ./compiler/tests/benchmarks`) and a
  source shape that is worth pausing in.
- The generated-C side already carries the debugging evidence that matters to
  this project — trap fixtures that assert exact messages and a UBSan rerun —
  and this spec adds nothing there.
- No Delve configuration, no `dlv` invocation, and no debugger is added to the
  suite: an ordinary test may not invoke an external tool.

### 5. Source-policy analysis

The five source-policy checks — architecture, comment policy, trap inventory,
error inventory, and expression-dispatch coverage — each hand-roll the same
`go/ast` + `go/parser` + `go/token` walk. Adopt `go/analysis`'s `Analyzer`
shape for them:

- Each check becomes an `*analysis.Analyzer` with `Name`, `Doc`, `Run`, and an
  explicit `Requires` set, so the walk, the diagnostic construction, and the
  reporting policy are declared once per check instead of being re-derived per
  file.
- The analyzers are driven from tests over `go/parser`-produced ASTs, filling
  `analysis.Pass.Fset`, `Files`, and `TypesInfo` directly. `golang.org/x/tools`
  is not currently needed at all — `go mod why golang.org/x/tools` reports the
  main module does not need it — and this adoption adds exactly the one
  package set it uses.
- Facts, `analysis.Pass.Report`, and `analysis.Diagnostic`'s position and
  suggested-fix fields replace the ad-hoc `t.Errorf` calls, so a policy
  finding reads the same way in every check.

## Constraints

- **Measure, never gate.** No threshold, no budget, and no assertion on any
  reported number. The guards assert only that collection happened.
- **Ordinary tests are pure Go.** No test invokes a debugger, a profiler
  binary, `go list`, or `go test` on itself; profile and trace bytes are
  produced and consumed in-process by `runtime/pprof`, `runtime/trace`, and
  `golang.org/x/exp/trace`.
- **Third-party imports stay confined.** Every addition lives in a `_test.go`
  file under `compiler/tests/benchmarks`, so
  `TestNoThirdPartyImportsOutsideBenchmarks` keeps passing and the compiler
  binary gains nothing.
- **`go test ./...` stays toolchain-free.** Nothing here requires an installed
  C compiler, and nothing here runs on the ordinary path unless it is cheap and
  pure Go.

## Validation

This section is exhaustive.

- `go test ./...` passes unchanged, with no additional external tool
  installed.
- `go test -bench . -benchmem -benchtime 1x ./compiler/tests/benchmarks`
  reports the existing seven success shapes, the five failure shapes, and the
  corpus, with allocation columns still present.
- The documented profile invocation produces a non-empty CPU profile and a
  non-empty heap profile, and `TestProfileCapture` fails when either is empty.
- `TestTraceRecordsSpans` fails when the trace contains no span, and
  `TestRuntimeMetricsMove` fails when the allocation metric does not advance.
- `-tags benchmetrics` adds `BenchmarkTraceCorpus` and
  `BenchmarkRuntimeMetricsCorpus`, each reporting its metrics beside the
  ordinary benchmark columns, and both keep working alongside the existing
  traversal benchmarks.
- Every source-policy check is reachable as a named `analysis.Analyzer` and
  reports through `analysis.Pass`, and each check's existing findings — a
  comment-policy violation, an unclassified trap literal, an undispatched
  expression-dispatch kind — are still reported, by the same test names, from
  the same files.
- `TestNoThirdPartyImportsOutsideBenchmarks` still passes, and
  `golang.org/x/tools` is absent from every non-test package's imports.
- `go build ./compiler/...` still compiles no third-party library, and the
  compiler binary's size and dependency set are unchanged.
- No reported measurement is asserted against a threshold anywhere in the
  suite.

## Rejected alternatives

- **A separate `compiler/tests/diagnostics` package.** It would duplicate the
  benchmark programs, the corpus loader, and the package comment that already
  documents the harness; the additions are measurements of the same
  compilations and belong beside them.
- **`analysis/analysistest` and `go/packages` for the policy checks.**
  `packages.Load` shells out to `go list`, which makes an ordinary test depend
  on the Go toolchain being present and invocable, against the repo's rule that
  ordinary tests are pure Go. The `Analyzer` shape is what those checks need;
  the golden-file driver is not.
- **`net/http/pprof` and a long-running measurement server.** A server makes a
  measurement depend on an out-of-process client and a port, and every one of
  these measurements is already reachable synchronously inside a test binary.
- **A Delve-driven debug fixture.** It would add a hard external-tool
  dependency to the ordinary suite for a workflow that is inherently
  interactive.
