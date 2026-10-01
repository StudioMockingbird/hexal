# RFC 0258: Web Server Benchmark Suite

- Kind: Feature Specification (Rust-Style RFC)
- Status: Draft; implementation not started. Phase 0 needs the user's
  authorization to obtain the load tool
- Created: 2026-10-02
- Updated: 2026-10-02
- Scope: a tagged, reproducible suite that builds the web server programs,
  drives them with bombardier, samples the server process, and checks the
  results into the repository for the machine that produced them
- Depends on: the implemented `std/http` server, the build driver
  (`internal/driver`), and the C23 suite's toolchain-discovery convention
- Coordinates with: ADR 0257 (web server performance), which uses this suite as
  its Phase 0 harness; archived RFC 0125 and ADR 0256 (the tagged C23 suite and
  its validation scopes), whose structure this suite follows; the compiler
  benchmark and complexity reports, whose append-only history it follows
- Does not add: language syntax, a standard-library API, a runtime dependency of
  any Hexal program, or a network download performed by a test

## Motivation

The web server has a recorded baseline and a comparison against Go's `net/http`,
but both came from scratch programs that were deleted. Nothing in the repository
can reproduce them, so ADR 0257 cannot judge an optimization against a baseline
and a regression in throughput or memory would go unnoticed. The compiler already
has a measurement discipline: a benchmark suite with an append-only history and a
tagged external suite with actionable toolchain discovery. The web server gets
the same: a suite anyone can rerun, whose output is committed for the machine that
produced it, so a later change is compared with a file and not with a memory.

The load tool is bombardier (`github.com/codesenberg/bombardier`, MIT licensed).
It is an established, widely run HTTP benchmarking client, so the figures are
produced by a tool other people can run and cross-check rather than by a harness
written for this repository.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Closed-loop keep-alive `GET /` at several connection counts | Pick up | The headline throughput and latency measure |
| Throughput against server core count | Pick up | The ceiling ADR 0257 investigates |
| Fixed offered rate | Pick up | Latency below saturation, with bombardier's own limits stated |
| Startup, idle CPU, idle memory, binary size | Pick up | Footprint, driven by the harness (bombardier has no idle mode) |
| Memory with 1,000 and 10,000 idle connections | Pick up | The per-connection cost, driven by the harness |
| Per-thread server CPU | Pick up | Whether the loop thread is the ceiling (ADR 0257 H1) |
| A Go `net/http` reference server | Pick up | The comparison already used, kept as context, never as acceptance |
| Connection per request, pipelining | Skip | Bombardier offers neither; a later record may add a second tool |
| File serving, request bodies, TLS, HTTP/2 | Skip | Not part of the hello-world measurement; each needs its own record |
| Downloading or installing the tool from a test | Reject | Tests never fetch; the user supplies the binary |
| Comparison against servers other than the Go reference | Skip | Out of scope; no acceptance depends on it |

## The load tool

Verified against bombardier's public documentation on 2026-10-02: the latest
tagged release is v2.0.2 (2025-03-04); releases publish binaries for several
platforms and no checksums; the flags used here exist (`-c` connections, `-d`
duration, `-r` rate limit, `-t` timeout, `-l` latency statistics, `-p` print
selection, `-o` output format including json, `-H` header, and the client
selectors `--fasthttp`, `--http1`, `--http2`); the default client is fasthttp. The
documentation does not describe the JSON output schema and does not say whether
latency is corrected for coordinated omission, so this suite treats the schema as
something to capture and verify (Phase 0) and reports latency exactly as bombardier
defines it, never as coordinated-omission corrected.

Decisions:

1. **Pinned version.** Phase 0 pins one exact release. The suite refuses any other
   version, because the JSON schema and the client's behavior are not documented
   and may differ between releases.
2. **Acquisition is the user's step.** The suite discovers an installed binary; it
   never downloads. The documented ways to obtain the pinned version are
   `go install github.com/codesenberg/bombardier@<version>` (integrity from the Go
   module checksum database) or the release binary. Because releases publish no
   checksums, a release binary's SHA-256 is computed at first acquisition and
   recorded in the results. Fetching requires the user's explicit go-ahead naming
   the source and approximate size; Phase 0 asks for it.
3. **Explicit client.** Every run passes `--fasthttp` explicitly so a change of
   bombardier's default cannot change the measurement, and the results record the
   client choice. Phase 0 confirms that this client completes keep-alive
   exchanges with both servers without protocol errors.
4. **Output.** Every run requests the result only, in JSON, with latency
   statistics; the raw JSON is stored verbatim in the results.

## Suite layout

A new package `compiler/tests/webbench/` (`package webbench`), every file a
`_test.go` file so that `go build ./compiler/...` compiles none of it. A new
package is justified by a distinct lifecycle and toolchain: it needs an external
load tool, a C compiler, and a Go toolchain, runs for tens of minutes, and
measures rather than asserts.

- Untagged files hold the pure-Go parts: the parser for bombardier's JSON, the
  statistics (median, range), the results schema and its encoding, the machine
  identifier, the rate computation, and the report generator, each with a focused
  unit test. They run in the ordinary suite and never start a process.
- Files tagged `webbench` hold everything that starts a process: toolchain
  discovery, server builds, process control and sampling, workloads, and the
  record and report entry points.
- `testdata/` holds the Hexal server source (a template with the listening port
  substituted per run), the Go reference server source, and one captured
  bombardier JSON output from the pinned release used by the parser's unit tests.
- `results/` holds the checked-in result files, one per run of the record lane,
  never edited or overwritten.
- `docs/web-benchmarks.md` is generated from `results/` and checked in, in the
  same append-only spirit as `docs/benchmarks.md`: newest first, each entry naming
  its machine, toolchains, and date.

### Servers under test

- **Hexal:** the hello-world server (one route returning `Hello, World!` with
  `Content-Type: text/plain; charset=utf-8`), built with the build driver in release
  mode against the discovered Clang, configured with `max_connections` high enough
  for the idle workload and a large backlog. Release mode and the configuration are
  recorded.
- **Reference:** a Go `net/http` program of the same behavior (same route, body,
  and `Content-Type`), built with the discovered Go toolchain, run with
  `GOMAXPROCS` equal to the number of server cores, otherwise default.

### Discovery

Following the C23 suite's rule: an environment override
(`HEXAL_BOMBARDIER`) naming one exact executable, then the plain executable name
on `PATH`, then a bounded list of conventional locations (the Go install
directory, `GOBIN`, `GOPATH/bin`). Clang resolves exactly as the C23 suite does
(`HEXAL_CLANG`, versioned names, fallbacks); the Go toolchain is the `go` on
`PATH`. A tagged run resolves each tool once and fails with one actionable message
naming every location tried when a tool is missing or the wrong version. There is
no skip and no degraded mode. Sharing the Clang discovery with the C23 suite is
allowed only if the C23 suite's behavior and tests are unchanged; otherwise the
rule is reimplemented.

## Workloads

Fixed by this record, all against `127.0.0.1` on an unoccupied port chosen at
suite start, one fresh server process per run, servers interleaved within each
repetition and at least three repetitions per cell, each measured run preceded by
a discarded warm-up. The server and the client are pinned to disjoint logical
CPUs by the operating system's affinity facility (the harness narrows its own
affinity before starting a process so the child inherits it, then restores it);
when pinning is unavailable the run is recorded as unpinned and flagged not
comparable.

| Id | Workload | Driven by |
| --- | --- | --- |
| W1 | closed-loop keep-alive `GET /` at 1, 8, 64, 256, and 1,024 connections | bombardier |
| W2 | 64 connections with the server on 1, 2, 4, and 8 logical CPUs (the client on the rest, so the client's allotment is the same for every point) | bombardier |
| W3 | 64 connections at a fixed rate of 25% and 50% of the lower of the two servers' W1 peak at 64 connections | bombardier (`-r`) |
| W4 | startup (time from process creation to the first correct response), binary size, idle working set and committed memory, idle CPU over 10 s | harness |
| W5 | working set and committed memory with 1,000 and 10,000 idle keep-alive connections that each completed one request | harness |

Every run records, from bombardier, requests per second, the latency distribution,
and response-class counts, and, from the harness sampling the server process,
CPU time over the measured window, the peak and final working set, committed
memory, thread count, handle or descriptor count, and the CPU time of each thread
(so the busiest thread's share is visible). The client's own CPU is recorded, and
a run in which the client used more than 90% of its allotted CPUs is flagged
`client_limited`: the figure is kept but is not read as a server limit. The
threshold is a conservative flag, not a measured optimum. A run with any
non-2xx response or any bombardier error is invalid and fails the lane.

Rate-limited latency is bombardier's own measure: it is reported as such and is
not described as coordinated-omission corrected.

## Results

The record lane writes one file under `results/` named by UTC timestamp and a
machine slug, with a versioned schema:

- **host:** operating system and build, architecture, CPU model, logical CPU
  count, memory, power plan or governor where the platform exposes it, and the
  background CPU load measured before the run;
- **toolchains:** Hexal version, Clang, Go, and bombardier versions, the
  bombardier binary's SHA-256, and the client selector;
- **configuration:** server and client CPU sets, whether pinning was applied,
  durations, warm-up, repetitions, the ports, and the exact commands;
- **runs:** every run with its workload, server, repetition, bombardier's raw JSON,
  the harness's server and client samples, and its flags (`client_limited`,
  `unpinned`);
- **idle and startup:** the W4 and W5 records.

`TestWebBenchmarkReport` regenerates `docs/web-benchmarks.md` from every file in
`results/` deterministically: the same files give the same bytes. A cell shows the
median and the range over the repetitions, the two servers side by side, and the
Hexal-to-reference ratio. Results from different machines or different tool
versions are never merged into one cell.

## Validation scopes

Following AGENTS.md's four scopes, cheapest first:

1. **Ordinary.** `go test ./...`, `go vet ./...`, `go build ./...`. The package
   compiles untagged, its unit tests run, and no process starts.
2. **Check.** `go test -tags webbench -short ./compiler/tests/webbench`. Resolves
   the tools, builds both servers, checks that each answers `GET /` with status 200
   and the expected body, runs each for two seconds at two connection counts,
   parses the output, confirms the sampler reports nonzero CPU, memory, and threads
   and that pinning is applied or flagged, and records nothing. It takes about a
   minute.
3. **Record.** `go test -tags webbench -run TestWebBenchmarkRecord -timeout 90m
   -webbench.record ./compiler/tests/webbench`. The full matrix; writes a result
   file; runs for tens of minutes and occupies the machine. It runs only on the
   user's request, never by default, never to close a spec, and never as part of a
   routine change.
4. **Report.** `go test -tags webbench -run TestWebBenchmarkReport
   ./compiler/tests/webbench` regenerates the document without running a server.

A quiet machine, wall power, and no other load are the operator's job; the
background load before each run is recorded so a disturbed run is visible.

## Required sweep

- the `webbench` package, its testdata, the first result file, and the generated
  `docs/web-benchmarks.md`;
- AGENTS.md's Testing section: the suite, its tag, its four commands, and the rule
  that the record lane runs only on request;
- ADR 0257: its Phase 0 harness is this suite, and its open-loop requirement is
  replaced by bombardier's rate-limited mode with the stated limitation;
- `docs/status.md`: this record's entry.

## Validation

This section is exhaustive:

- the package compiles and its untagged unit tests pass in the ordinary suite with
  no process started: the parser accepts the captured pinned-release output and
  rejects malformed or unrecognized-schema output with a diagnostic naming the
  field; the median, range, and ratio are correct on constructed samples; the
  results schema round-trips; the report is byte-identical for the same files and
  never mixes machines or tool versions in one cell; the machine slug and the rate
  computation are correct;
- tool discovery honors the override, `PATH`, and the fallback locations, rejects a
  bombardier of any version other than the pinned one, names every location tried
  in its one failure message, and never downloads;
- the check lane builds both servers, verifies their responses, drives them,
  parses the output, and reports sampler and pinning status, in about a minute;
- the record lane produces a result file containing every workload W1 to W5 with at
  least three interleaved repetitions per cell, raw bombardier JSON preserved,
  per-thread CPU for the server, the client's CPU, and the flags; a run with any
  non-2xx response or bombardier error fails the lane; a `client_limited` run is
  recorded and flagged, not hidden;
- the generated document and the first result file for the current machine (Windows
  x64, the machine that ran the record lane) are checked in, and the document names
  that machine, every tool version, the bombardier SHA-256, and the caveats of this
  record (same-host client, bombardier's latency definition, no churn or pipelining);
- the Linux (WSL2) environment on the same hardware is a separate machine entry: it
  is produced by the same lane when requested and is not part of this record's
  acceptance;
- AGENTS.md lists the suite and its commands, ADR 0257 points at it, and the status
  entry is removed when this record closes;
- ordinary gates and the check lane pass; the record lane has run once on the
  current machine with the user's consent; no exhaustive C23 run is implied.

## Implementation plan

### Phase 0: the tool

Ask the user to authorize obtaining bombardier, naming the source and size. Pin the
release; record its license and, for a release binary, its SHA-256. Run it against
the Hexal and Go servers by hand to capture one JSON output, confirm the flags in
this record against the pinned release, and confirm the fasthttp client completes
keep-alive exchanges with both servers cleanly. Exit: the version, the captured
output, and the confirmed flag set are recorded; the unit-test sample exists.

### Phase 1: the pure-Go parts

The parser, statistics, schema, machine slug, rate computation, and report
generator with their untagged unit tests. Exit: the ordinary suite covers them and
no process starts.

### Phase 2: processes

Discovery, the server builds, process start with affinity, the harness's server and
client sampling (including per-thread CPU on Windows and Linux), the response check,
and the check lane. Exit: the check lane passes on the current machine.

### Phase 3: workloads and the first result

W1 to W5, validity and `client_limited` flagging, the record and report entry points.
Run the record lane on the current machine with the user's consent, review the file
and the report, and check both in. Exit: the Validation bullets on the result file
and the document hold.

### Phase 4: sweep and closure

Update AGENTS.md and ADR 0257, remove the status entry, review that nothing outside
the package changed, rebuild `hexal` and restart `hexal play`, and close and archive
this record when Validation is satisfied.

## Remaining decisions

None for the author. Measured or machine-dependent values (warm-up length, run
duration, the 90% client threshold) are conventions with a stated rationale, not
measured optima; if a result shows a convention is wrong for a machine, the result
records it and a later record changes it.

## Reference synchronization

None: the suite changes no language or library behavior, so `docs/reference.md` is
unaffected.
