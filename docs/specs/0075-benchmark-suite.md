# RFC 0075: Compiler Benchmark Suite

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation-ready
- Created: 2026-08-16
- Scope: committed Go benchmarks for `compiler.Compile`, and the policy for
  acting on their results
- Depends on: nothing. Independent of RFCs 0072–0079.
- Coordinates with: RFC 0074 (which optimizes three already-measured sites),
  `AGENTS.md`, `docs/status.md`
- Does not change: Hexal syntax, semantics, diagnostics, or generated C

## Summary

The compiler has **no committed benchmark**. Three allocation hot spots were
found during the refactor audit with an ad-hoc profile that was deleted
afterwards, so nothing in the repository can detect a performance regression, and
the next person to ask "is this slow?" starts from zero.

This RFC adds a small benchmark suite covering the shapes that stress different
compiler phases, plus the rule for when a measurement authorizes a change.

It does **not** optimize anything. RFC 0074 Stage 2 owns the three sites already
measured; this RFC is the instrument, not the surgery.

## Motivation

The audit measured, over the 98-snippet corpus:

```
350.2 ms/op   15.79 MB/op   78,364 allocs/op
```

and identified three sites worth 33% of allocated bytes. Those numbers came from
temporary files that no longer exist. Re-deriving them costs the same effort
next time, and a regression introduced between now and then would be invisible.

`AGENTS.md` lists "Fast compilation" as a language goal. A goal with no
measurement is an aspiration.

The audit also found the *correct* answer for most of the codebase was "nothing
measurable is slow" — CPU flat at generation 41%, checking 19%, GC 22%, no
quadratic scan, `strings.Builder` used consistently. A committed suite makes that
answer reproducible instead of a claim.

## Non-goals

- Optimizing anything. Measurement only.
- A performance CI gate. Benchmarks on shared runners are too noisy to fail a
  build; this suite is for humans comparing two commits deliberately.
- Micro-benchmarks of individual functions. The unit is `compiler.Compile`,
  because that is the contract.
- Tracking wall-clock numbers in the repository as expected values. Machines
  differ; committed absolute timings rot immediately.
- Invoking an external C toolchain. Ordinary benchmarks stay pure Go, matching
  the testing policy.
- Benchmarking the workbench, the snippet catalog loader, or anything outside
  the compiler boundary.

## Benchmark set

One file, `compiler/bench_test.go`, package `compiler`. Each benchmark compiles
a fixed in-memory source map through `compiler.Compile` and reports
`-benchmem`.

| Benchmark | Shape | Phase it stresses |
|---|---|---|
| `BenchmarkScalar` | arithmetic, conditionals, one function | baseline floor; lexer and parser share |
| `BenchmarkGenericsHeavy` | several user generic functions and objects specialized over many argument sets | checker specialization, monomorphization |
| `BenchmarkMultiModule` | 8–12 modules, a diamond dependency, cross-module calls and exported types | module resolution, per-module emission |
| `BenchmarkCollections` | `List`, `Dict`, `Array`, `View` over several element types | component discovery, specialization families |
| `BenchmarkText` | String and Strand operations, literals, UTF-8 iteration | literal registry, text helper emission |
| `BenchmarkConcurrency` | `spawn`, `Task`, `Channel`, `Mutex`, `Atomic` | concurrency discovery and runtime emission |
| `BenchmarkErrorPaths` | `try`, `defer`, `errdefer`, Error-returning chains | cleanup lowering, hoisting |
| `BenchmarkCorpus` | every snippet in the catalog, compiled once per iteration | end-to-end aggregate |
| `BenchmarkFailure` | a program with diagnostics, compiled to failure | diagnostic construction and merge |

`BenchmarkFailure` matters because every other benchmark measures the success
path only, and diagnostic construction is roughly 978 sites.

Sources are Hexal literals in the benchmark file, not loaded from
`workbench/snippets/`, except `BenchmarkCorpus` which reads the catalog through
its existing loader. Inline sources keep a benchmark stable when the catalog
changes for unrelated reasons.

## Requirements

- Each benchmark calls `compiler.Compile` inside the loop and nothing else;
  source-map construction happens once, before `b.ResetTimer()`.
- Assign the result to a package-level sink so the call is not eliminated.
- No benchmark asserts a threshold. A benchmark that fails is a test.
- No benchmark writes a file, reads the host filesystem, or starts a goroutine.
- `go test ./...` must not run them; benchmarks execute only under `-bench`.
- Total `-bench . -benchtime 1x` runtime stays under ten seconds so the suite is
  cheap enough to run before and after a change.

## Recording a baseline

Commit `docs/benchmarks.md` containing, per benchmark: ns/op, B/op, allocs/op,
the Go version, and the CPU model. Update it only when someone deliberately
re-measures on the same machine class, and state in the same change what moved
and why.

Absolute numbers across machines are meaningless; the file exists so a
*ratio* between two commits on one machine is checkable, and so a large shift is
noticeable.

Record the audit's existing figures as the initial entry, marked as measured on
the corpus before any RFC 0074 work:

```
BenchmarkCorpus    350.2 ms/op   15.79 MB/op   78,364 allocs/op
```

## Policy for acting on a result

This is the part that outlives the benchmarks.

1. **A measurement authorizes a change; a hypothesis does not.** No optimization
   lands without a profile naming the site and its share.
2. **Allocation count is the primary signal**, not wall-clock. It is stable
   across machines and load, and GC was 22% of CPU at the time of the audit.
3. **A change that adds complexity must show its measured gain in the spec that
   proposes it**, including the before and after numbers.
4. **"Nothing measurable is slow" is a valid and expected result.** Record it and
   stop. Most of this compiler is already fine.
5. **Do not optimize generated C for speed under this policy.** Generated-C
   quality is governed by `docs/reference.md`; this suite measures the compiler,
   not its output.

## Interaction with RFC 0074

RFC 0074 Stage 2 optimizes three sites measured before this suite existed:

| Site | Measured share |
|---|---|
| `compile.go:84` double lex for `Stats.TokenCount` | 4.3% of allocations |
| `walkStatementExpressions` closure allocation per statement | 18.0% |
| `types.unionMembers` copying for read-only callers | 10.4% |

Those measurements stand and Stage 2 does not wait on this RFC. But land this
suite **first** if the two are scheduled together, so Stage 2's gain is recorded
as a before/after ratio rather than asserted.

RFC 0074 also defers fusing the ~21 generator discovery walks pending data. This
suite — specifically `BenchmarkCollections` and `BenchmarkCorpus` — is the data
that decision needs.

## Validation

- `go test ./...` passes and runs no benchmark.
- `go test -bench . -benchmem ./compiler` completes under ten seconds and
  reports all nine.
- `go vet ./...` passes.
- Each benchmark's source compiles successfully, except `BenchmarkFailure`,
  which must fail with diagnostics — assert that in an ordinary test so a
  silently-succeeding failure benchmark is caught.
- `docs/benchmarks.md` exists with an initial entry.

## Drawbacks

- Nine benchmarks are nine more things to keep compiling as the language
  changes. Mitigated by keeping sources small and shape-focused rather than
  exhaustive.
- A committed baseline invites treating absolute numbers as targets. The policy
  section states explicitly that only ratios on one machine are meaningful.
- `BenchmarkCorpus` couples to the snippet catalog, so a catalog change moves the
  number for reasons unrelated to the compiler. It is included anyway because it
  is the only end-to-end aggregate; the other eight are insulated.
