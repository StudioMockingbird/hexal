# Compiler benchmark baseline

Benchmarks live in `compiler/bench_test.go` (eight shape-focused programs) and
`workbench/snippets/bench_test.go` (`BenchmarkCorpus`). Numbers below mean
something only as ratios on one machine: absolute timings across machines and
Go versions are not comparable, so none of them is a target.

Update this file only when someone deliberately re-measures on the same machine
class, and state in the same change what moved and why.

## First committed-suite measurement

Measured by the committed suite on 2026-08-17, run with
`go test -bench . -benchmem -benchtime 1x ./compiler ./workbench/snippets`.
This is the live baseline for before/after ratios.

Go 1.26.4 windows/amd64 on AMD Ryzen 5 7530U with Radeon Graphics.

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

## Audit provenance

The pre-suite audit figure is kept for provenance only and must not be mixed
into before/after math: it measured the same 98-snippet corpus — allocs match
the suite (78,364 vs 78,303) but wall time differs by an order of magnitude
under the ad-hoc measurement conditions — so it is not on the same metric as
the table above.

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| BenchmarkCorpus (pre-suite audit) | 350.2 ms/op | 15.79 MB/op | 78,364 allocs/op |