# RFC 0237: Documented Diagnostic and Correctness Runs

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented. Scope is documentation of four `go` invocations and
  nothing else; the "Diagnostic and correctness runs" section landed in
  `docs/benchmarks.md` and every Validation bullet was verified on the tree. The
  measurement harness this RFC originally proposed — a
  `compiler/tests/observability` package holding profiling, tracing, and
  runtime-statistics tests — is cut, with the measurements that killed it
  recorded under Rejected alternatives. The file name predates that narrowing
  and is kept because a spec number is a permanent identifier
- Created: 2026-09-22
- Updated: 2026-09-23
- Origin: go.dev/doc/diagnostics names four facilities — profiling, tracing,
  debugging, and runtime statistics and events — that a Go project can use to
  find expensive sections and latency. This project can already reach all four,
  and reaches none of them in a documented place. That gap is a documentation
  gap, not a missing harness
- Depends on: the benchmarking policy in `docs/benchmarks.md` and the existing
  suites under `compiler/tests/`
- Does not update: `docs/reference.md`. Nothing here is language-visible: no
  syntax, semantics, signature, diagnostic, or generated-C contract moves.
  `docs/status.md` gains no entry either, because nothing here is an open bug
  or TODO

## Decision summary

Write four `go` invocations into `docs/benchmarks.md` with one line each on what
they catch and when to reach for them. Add no package, no test, no build tag,
and no dependency.

Three of the four are correctness gates that find real defects and are not
currently run: the race detector, coverage, and order randomization. The fourth
is the profiling invocation, which already works and is written down nowhere.

The source-policy checks are left exactly as they are, and no measurement
harness is built. Both were considered and rejected; see Rejected alternatives.

## Harness inventory

Recorded so no category is silently missing a home. This table is the durable
part of this RFC: it is the answer to "what does this project already have, and
where."

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
| Profiling | `-cpuprofile` / `-memprofile` on the benchmarks suite | works today; **documented by this spec** |
| **Race detection** | documented ad hoc only, `./workbench/snippets/...` | **this spec** |
| **Coverage** | — | **this spec** |
| **Order randomization and repetition** | — | **this spec** |
| Tracing | `go tool trace` on the benchmarks suite | works today; not documented, and see Rejected alternatives |
| Runtime statistics | `-benchmem`'s `B/op` and `allocs/op` | already the primary signal in `docs/benchmarks.md` |
| Debugging | human workflow; generated-C side has trap fixtures and UBSan | out of scope |
| Dependency vulnerability scan | — | out of scope, below |
| Third-party linters | — | out of scope, below |

## What to document

### 1. Race detection

```bash
go test -race ./...
```

A race is a correctness failure, not a measurement, so the no-threshold policy
does not apply to it. This extends the narrower
`go test -race ./workbench/snippets/...` run that RFC 0140 established to the
whole tree.

The gate has a prerequisite the other runs do not: `-race` requires
`CGO_ENABLED=1` and a working C toolchain, so on Windows it needs an installed
gcc. That does not breach the rule that `go test ./...` passes with no external
toolchain — this is a separate invocation — but the gate is not free and the
requirement is recorded where the command is.

No package holds a race-detector guard, because a race cannot be detected from
inside the process that has it; the gate is the run mode itself.

### 2. Coverage

```bash
go test -coverprofile=cover.out ./...
go tool cover -func=cover.out
```

No threshold is added and no coverage number is asserted. This is the
measurement `docs/status.md`'s qualitative "Known coverage gaps" list has never
had, and it is the input for deciding which gap to close next.

### 3. Order randomization and repetition

```bash
go test -shuffle=on -count=2 ./...
```

Shuffling exposes inter-test order dependence; a second count exposes time- or
state-dependence that one pass cannot.

### 4. Profiling

```bash
go test -bench . -benchtime 1x -cpuprofile cpu.out -memprofile heap.out ./compiler/tests/benchmarks
go tool pprof cpu.out
```

This works today and needs no code. It is written down because the Origin's
complaint is that it is reachable but undocumented.

Block and mutex profiling are noted as available and deliberately unused: the
compiler's own path is single-goroutine, so a contention profile says nothing a
reader of the frontend can act on.

## Constraints

- **Documentation, not gates in the suite.** `go test ./...` is unchanged. None
  of these four runs joins it, and none of them is wired into a package.
- **No threshold anywhere.** Coverage and profile numbers are diagnostics. A
  target would turn a diagnostic into a goal, which is the failure mode
  `docs/benchmarks.md` exists to avoid for allocations.
- **No `docs/benchmarks.md` measurement entry.** The Measurement history
  section records benchmark numbers. Race, coverage, and shuffle produce no
  number this project tracks, and the profiling invocation produces a profile a
  human reads once. These four go in their own section, not in the history.
- **No new dependency, package, build tag, or test.** If an item cannot be
  expressed as a documented `go` command, it is not in this spec.

## Implementation plan

One phase. It touches one file.

1. Add a "Diagnostic and correctness runs" section to `docs/benchmarks.md`,
   after the benchmark baseline and before Measurement history, holding the four
   invocations above with one line each on what each catches and when to reach
   for it.
2. Record the `-race` toolchain prerequisite beside that command.
3. Record block and mutex profiling as available and unused, with the
   single-goroutine reason, beside the profiling invocation.
4. Run all four on the current tree and confirm each completes.

## Reference synchronization

None. Nothing here is language-visible, so `docs/reference.md` is unaffected.
This was verified rather than assumed: the change adds no Go file and no Hexal
surface.

## Validation

This section is exhaustive.

- `docs/benchmarks.md` holds a "Diagnostic and correctness runs" section naming
  all four invocations, each with its purpose, and the existing baseline,
  traversal, and Measurement history sections are unchanged.
- The `-race` entry records the `CGO_ENABLED=1` and C-toolchain prerequisite.
- `go test -race ./...` completes on the tree.
- `go test -coverprofile=cover.out ./...` and `go tool cover -func=cover.out`
  complete on the tree.
- `go test -shuffle=on -count=2 ./...` completes on the tree.
- The documented profiling invocation produces non-empty `cpu.out` and
  `heap.out`.
- `go test ./...`, `go vet ./...`, and `gofmt -l` are unchanged and clean: this
  spec adds no Go file.
- `go.mod` and `go.sum` are unchanged, and the compiler binary's dependency set
  is unchanged.
- The snippet manifest is unmoved. This spec changes no generated byte, so any
  movement is a defect in the change rather than an expected rebaseline.

## Implementation readiness

Implementation-ready. One file changes, no design question is open, and all four
commands were run against the tree while this RFC was written:

| Run | Result on the tree |
| --- | --- |
| `go test -race ./...` | 23 packages pass; no data race reported |
| `go test -coverprofile` + `go tool cover -func` | completes; **59.7% of statements** |
| `go test -shuffle=on -count=2 ./...` | passes; no order dependence surfaced |
| `-cpuprofile` / `-memprofile` on the benchmarks suite | `cpu.out` 11.7 KB, `heap.out` 23.9 KB; `go tool pprof` reports 480 ms of samples across 235 nodes |

The 59.7% figure is the point of item 2: `docs/status.md` has carried a
qualitative "Known coverage gaps" list with no number behind it, and this is
the first one. It is recorded as a starting observation, not a target.

## Non-goals

- **A measurement harness.** Cut; see Rejected alternatives.
- **Restructuring the source-policy checks onto `go/analysis`.** Cut; see
  Rejected alternatives.
- **Dependency vulnerability scanning.** `govulncheck` is worth an opt-in run,
  but it is an external tool whose database changes independently of the tree,
  so a finding is not a property of this commit. It gets no place in the
  ordinary suite.
- **Third-party linters.** `staticcheck` and `golangci-lint` overlap `go vet`
  and the eight policy checks, and would be a second source of truth for the
  same rules. Adopting a linter driver is a later decision with its own
  argument.
- **CI.** The repository has no workflow definition, so nothing runs any of
  this automatically. Wiring one up is a separate decision about where the gates
  run, not about what is documented.
- **Testable examples.** The snippet catalog under `workbench/snippets` already
  holds one compiling, output-checked program per language facet; `Example`
  functions would be a second, weaker copy of that.
- **A debugger gate.** Debugging is interactive by nature; the reproducible
  build is `go test -c -gcflags=all='-N -l'` and nothing more is added.

## Rejected alternatives

### A `compiler/tests/observability` measurement harness

An earlier draft of this RFC proposed a package holding `TestCPUProfileCapture`,
`TestHeapProfileCapture`, `TestTraceRecordsEvents`, `TestRuntimeMetricsMove`,
and two benchmarks behind a build tag, reporting trace and `runtime/metrics`
figures transcribed into `docs/benchmarks.md`. It was measured and cut. The
numbers are recorded here so the proposal is not revived from intuition.

**The runtime-statistics benchmark reports what `-benchmem` already reports.**
Over one catalog-corpus compile, the `/gc/heap/allocs:bytes` delta is 46,806,408
bytes against `-benchmem`'s `B/op` of 46,984,608 — the same quantity, 0.4%
apart. `docs/benchmarks.md` already carries `B/op` and `allocs/op` for all
thirteen benchmarks and already names `allocs/op` the primary signal.

**Two of its three metrics are not measurements of the compiler at all.**
`/memory/classes/heap/objects:bytes` is a gauge, not a counter: differencing it
across a compile produced 2^64 - 620,544, an unsigned underflow, because live
heap fell when the collector ran mid-benchmark. `/gc/cycles/total:gc-cycles`
records when Go's collector chose to run, which is a property of GC scheduling
rather than of the frontend.

**The capture tests assert that the Go standard library works.** None of the
four can fail because of a compiler change. The profiling capability they were
built around needs no code: the documented invocation in item 4 above works
today, and an empty profile announces itself through `go tool pprof` at the
moment a human looks.

**Tracing has nothing to show.** The compiler is single-goroutine and calls no
`trace.NewTask` or `trace.StartRegion`, so a trace holds no user span or task
at all: over one corpus compile its 10,177 events are `Metric` 6200,
`StateTransition` 3263, `Label` 526, and `RangeBegin`/`RangeEnd` 75 each. An
assertion on spans fails outright and a span count is a constant zero. What
remains is goroutine-scheduling noise, and `go tool trace` exists to analyse
concurrency and latency that this compiler does not have.

Adding `trace.StartRegion` calls to the compiler to make those numbers non-zero
is separately rejected: it is instrumentation in non-test compiler source, and
it would mean tracing measures an annotated compiler rather than the shipping
one — the same defect as taking a measurement under `-tags benchmetrics`, which
swaps `compiler/generator/traversal_metrics.go` for a counter-carrying
implementation.

### Restructuring the source-policy checks onto `go/analysis`

An earlier draft proposed giving each check an `*analysis.Analyzer` shape. It is
cut. All eight checks are purely syntactic — none imports `go/types`, and they
inspect imports, comment text, string literals, and switch cases — so the
framework's reason for existing does not apply to them. With no driver and no
`analysistest`, each test would have to hand-build an `analysis.Pass` and
install its own `Report` collector to forward back to `t.Errorf`, which is one
indirection added around a walk that already works. It promotes
`golang.org/x/tools` from an indirect requirement to a direct import and detects
nothing new. The case to revisit is a check that genuinely needs type
information; that has not happened.

### A coverage threshold

It would turn a diagnostic into a target, which is exactly the failure mode
`docs/benchmarks.md` exists to avoid for allocations.

### `net/http/pprof` and a long-running measurement server

It makes a measurement depend on an out-of-process client and a port, and every
measurement worth taking here is already reachable from one command against the
benchmarks suite.
