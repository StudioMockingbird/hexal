# Compiler benchmark baseline

Benchmarks live in `compiler/tests/benchmarks/`. Numbers below mean something
only as ratios on one machine: absolute timings across machines and Go versions
are not comparable, so none of them is a target.

```bash
go test -bench . -benchmem -benchtime 1x ./compiler/tests/benchmarks
```

`-benchtime 1x` is required, not optional: Go's default is one second *per*
benchmark, and thirteen of those do not fit the suite's ten-second budget. It
also means **`ns/op` and `MB/s` are single samples** — use `-count` and
`benchstat` before believing a timing difference. `allocs/op` is the primary
signal; it is stable across machines and load.

**Append, never overwrite.** Every measurement gets its own dated entry under
Measurement history, newest first, with the machine and Go version that produced
it and one line on what moved. The point of the history is that a trend is
visible without digging through git; replacing the previous numbers destroys
exactly that.

## Current baseline

**2026-08-18 22:39 IST** — Go 1.26.4 windows/amd64, AMD Ryzen 5 7530U.

| Benchmark | ns/op | MB/s | B/op | allocs/op |
|---|---|---|---|---|
| BenchmarkScalar | 2,073,500 | 0.15 | 182,408 | 757 |
| BenchmarkGenericsHeavy | 2,787,200 | 0.32 | 713,312 | 3,793 |
| BenchmarkMultiModule | 1,507,200 | 0.61 | 377,408 | 3,567 |
| BenchmarkCollections | 1,569,500 | 0.42 | 497,176 | 1,858 |
| BenchmarkText | 1,295,100 | 0.48 | 287,752 | 1,003 |
| BenchmarkConcurrency | 2,100,600 | 0.33 | 607,584 | 2,453 |
| BenchmarkErrorPaths | 1,284,700 | 0.30 | 377,392 | 2,191 |
| BenchmarkCorpus | 49,784,300 | 0.46 | 15,806,736 | 80,192 |
| BenchmarkFailureLex | 237,600 | 0.29 | 5,448 | 39 |
| BenchmarkFailureParse | 106,600 | 0.83 | 10,288 | 77 |
| BenchmarkFailureResolve | 59,000 | 2.66 | 7,400 | 52 |
| BenchmarkFailureCheck | 259,700 | 0.59 | 52,048 | 241 |
| BenchmarkFailureMany | 257,900 | 3.88 | 241,920 | 750 |

Total suite runtime 3.9s, against the ten-second budget.

## Traversal counts

Reported only under the `benchmetrics` tag, which compiles the counter into
`walkProgram`. An untagged build carries no counter.

```bash
go test -tags benchmetrics -bench Traversal -benchtime 1x ./compiler/tests/benchmarks
```

**2026-08-18 22:39 IST** — Go 1.26.4 windows/amd64, AMD Ryzen 5 7530U.

| Benchmark | walks/op | nodes/op |
|---|---|---|
| BenchmarkTraversalScalar | 25 | 725 |
| BenchmarkTraversalMultiModule | 200 | 875 |
| BenchmarkTraversalCollections | 25 | 1,325 |
| BenchmarkTraversalCorpus | 2,500 | 36,225 |

**This is the data RFC 0074 deferred its traversal-fusing decision on, and it
argues against fusing.** 25 walks per module compile — the 21 discovery call
sites plus four reached more than once — scaling linearly with module count
(200 for the eight-module program). But the corpus figures put the cost in
perspective: 36,225 expression-node visits per full-catalog compile, against
80,192 allocations. Each walk visits about 14 nodes on average, because Hexal
programs in the corpus are small. Fusing 25 traversals into 1 would remove
roughly 35,000 node visits per corpus compile and would couple every family's
discovery into one callback — a poor trade against a traversal that is already
cheap. Revisit only if a program shape appears where nodes/op is large relative
to allocs/op.

## Corpus trend

The one-line view. `BenchmarkCorpus` is the only end-to-end aggregate, so its
`allocs/op` is the number to watch; the rest of the history carries the detail.

| When | ns/op | B/op | allocs/op | What changed |
|---|---|---|---|---|
| 2026-08-18 22:39 | 49,784,300 | 15,806,736 | 80,192 | RFC 0080 — suite consolidated, throughput added |
| 2026-08-18 14:20 | 46,015,700 | 15,816,552 | 80,230 | after RFCs 0072/0073/0074/0076/0079 |
| 2026-08-17 | 34,851,500 | 15,790,632 | 78,303 | first committed suite (RFC 0075) |
| before 2026-08-17 | 350,200,000 | 15,790,000 | 78,364 | ad-hoc audit profile, not comparable |

## Measurement history

Newest first.

### 2026-08-18 22:39 IST — RFC 0080 lands

Go 1.26.4 windows/amd64, AMD Ryzen 5 7530U. The table under Current baseline.

Suite consolidated into `compiler/tests/benchmarks/`, `b.SetBytes` added for
MB/s, and `BenchmarkFailure` replaced by five failure-path benchmarks. No
compiler behavior changed, so the only comparable rows against the previous entry
are the eight that survived. `BenchmarkFailure` has no successor row: the five
that replaced it are different programs and are not comparable to it.

Against the 14:20 entry on identical compiler code, `BenchmarkCorpus` moved
+8.2% on ns/op and −0.05% on allocs/op. **That contrast is the clearest argument
in this file for why allocation count is the primary signal** — an 8% wall-clock
swing at `-benchtime 1x` with nothing to cause it, while allocations held to
within 38 of 80,000.

### 2026-08-18 14:20 IST — after the RFC 0072–0079 batch

Go 1.26.4 windows/amd64, AMD Ryzen 5 7530U. Measured with the suite still split
across `compiler/bench_test.go` and `workbench/snippets/bench_test.go`.

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| BenchmarkScalar | 3,828,800 | 182,408 | 757 |
| BenchmarkGenericsHeavy | 3,146,500 | 713,312 | 3,793 |
| BenchmarkMultiModule | 1,708,600 | 377,040 | 3,561 |
| BenchmarkCollections | 1,738,300 | 497,192 | 1,859 |
| BenchmarkText | 1,192,700 | 287,752 | 1,003 |
| BenchmarkConcurrency | 2,010,800 | 607,584 | 2,453 |
| BenchmarkErrorPaths | 1,243,400 | 377,392 | 2,191 |
| BenchmarkCorpus | 46,015,700 | 15,816,552 | 80,230 |
| BenchmarkFailure | 208,000 | 26,128 | 179 |

**Allocations rose against 2026-08-17 and the cause is not identified.** Corpus
78,303 → 80,230 (+2.5%), GenericsHeavy 3,405 → 3,793 (+11.4%), Scalar 685 → 757
(+10.5%), Failure 161 → 179 (+11.2%). The window covers RFCs 0072, 0073, 0074,
0076, and 0079. RFC 0073's `CanonicalKey` building a string per interned type is
the obvious first suspect and has **not** been bisected. Allocation count is the
primary signal under RFC 0075's policy, so this is an open item, not something a
re-baseline settles.

### 2026-08-17 — first committed suite

Go 1.26.4 windows/amd64, AMD Ryzen 5 7530U. The baseline RFC 0075 established.

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| BenchmarkScalar | 726,100 | 175,376 | 685 |
| BenchmarkGenericsHeavy | 2,425,200 | 694,464 | 3,405 |
| BenchmarkMultiModule | 1,436,600 | 389,912 | 3,367 |
| BenchmarkCollections | 1,320,900 | 506,208 | 1,852 |
| BenchmarkText | 850,100 | 291,448 | 934 |
| BenchmarkConcurrency | 1,385,200 | 612,800 | 2,391 |
| BenchmarkErrorPaths | 1,270,100 | 374,208 | 2,123 |
| BenchmarkCorpus | 34,851,500 | 15,790,632 | 78,303 |
| BenchmarkFailure | 169,600 | 26,296 | 161 |

### Before 2026-08-17 — pre-suite audit, provenance only

The six-pass refactor audit's ad-hoc profile, taken with temporary files that no
longer exist. Kept because RFC 0074's three optimization targets were sized
against it, and **not** comparable to anything above: allocations match the first
committed suite closely (78,364 vs 78,303), but wall time differs by an order of
magnitude under ad-hoc measurement conditions.

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| BenchmarkCorpus (pre-suite audit) | 350.2 ms/op | 15.79 MB/op | 78,364 |
