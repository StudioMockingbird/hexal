# ADR 0257: Web Server Performance: Reactor Hand-off and Per-Connection Cost

- Kind: Architecture Decision Record (ADR)
- Status: Draft; implementation not started. Phase 0 (harness and evidence)
  is authorized by this record; every later phase is gated by the measurement
  it names
- Created: 2026-10-02
- Updated: 2026-10-02
- Scope: the default HTTP server's cost per request, its throughput ceiling,
  and its memory per connection, over the Task-aware TCP runtime
- Depends on: archived RFC 0144 (Task-aware socket runtime and its recorded
  baseline), archived RFC 0194 (HTTP backend and ownership tables), archived
  RFC 0200 (static files, the one user of the filesystem worker pool), and the
  implemented scheduler and event bridge
- Coordinates with: RFC 0258 (the benchmark suite that is this record's harness)
- Does not add: public syntax, standard-library API, a dependency, a new
  qualified target, or a change to any guarantee in `docs/reference.md`

## Purpose

The server is correct and cheap in CPU per request, but its throughput stops
growing at about two cores while a Go `net/http` hello world keeps scaling, and
an idle connection costs far more committed memory. This record decides how to
find out why, which changes are worth trying and in what order, how each is
judged, and what is deliberately not attempted. It authorizes measurement
first; it does not pre-approve any optimization.

## Evidence (recorded 2026-10-02)

Hosts and method follow archived RFC 0144's baseline. This comparison ran on
`x86_64-windows-gnu-ucrt` only (Windows 11, AMD Ryzen 5 7530U, 12 logical CPUs,
Clang 23.1.2, Go 1.27.0). Both servers returned `Hello, World!` with `Date`,
`Content-Type`, and `Content-Length` over keep-alive HTTP/1.1; Hexal was a
release build with `max_connections` 20,000; Go was `net/http` with defaults and
`GOMAXPROCS` 4. The server ran on logical CPUs 0-3 and a Go closed-loop client on
4-11. Each cell is the median of three interleaved runs of 5 s after a 1 s
warm-up, a fresh server process per run. The harness was a scratch program and
was deleted; Phase 0 recreates it inside the repository.

Closed loop, 4 server cores, Hexal / Go:

| Connections | Requests/s | p50 | p99 | CPU per request | Server cores used |
| --- | --- | --- | --- | --- | --- |
| 1 | 13.9k / 10.9k | 66 / 87 us | 151 / 164 us | 76 / 114 us | 1.0 / 1.2 |
| 8 | 28.6k / 45.0k | 278 / 173 us | 512 / 417 us | 60 / 78 us | 1.7 / 3.5 |
| 64 | 37.6k / 55.7k | 1.69 / 1.08 ms | 2.70 / 3.43 ms | 41 / 69 us | 1.6 / 3.8 |
| 256 | 35.3k / 49.8k | 7.1 / 4.8 ms | 10.4 / 9.6 ms | 49 / 76 us | 1.7 / 3.8 |
| 1024 | 26.6k / 42.8k | 37.5 / 22.0 ms | 54.7 / 42.6 ms | 61 / 87 us | 1.6 / 3.8 |

Scaling with server cores, 64 connections, requests per second, Hexal / Go:
1 core 15.2k / 9.9k (Go noisy, 5k-20k), 2 cores 32.4k / 32.5k, 4 cores 34.8k /
58.2k, 8 cores 37.3k / 80.6k (the client was at 343% of 400%, so Go's 8-core
figure may be client-limited). Open loop at 8 connections, latency from the
scheduled send time: at 5k and 10k requests per second Hexal's p99 was 2.07 and
2.69 ms against Go's 2.80 and 3.46 ms; at 20k both tails varied from about 3 ms
to over 100 ms between runs and are not read. Connection per request: Hexal
3.1k connections/s (448 us CPU each, one 2 s outlier), Go 4.9k (375 us). Binary:
481 KB against 8.7 MB. Idle: both 0% CPU; working set per idle connection at
10,000 connections 36 KiB against 22 KiB; committed memory 1.87 GB against
0.27 GB (about 187 KiB against 22 KiB per connection).

From archived RFC 0144's counted baseline (Linux, `-O2`): a keep-alive request
on the fixed route makes 0 allocations, 2 loop-thread submissions, 1 `uv_write`,
1 `uv_read_start` and 1 `uv_read_stop`, and 2 `uv_timer_start`; one request per
connection makes 11 allocations (156,274 B). The same Linux baseline spent about
260-270 us of server CPU per request under WSL2 against 41-60 us on Windows.

## Findings and hypotheses

Findings are measured. Hypotheses are not, and each names what would falsify it.

| Id | Statement | Basis | Falsified when |
| --- | --- | --- | --- |
| F1 | Throughput is flat from 2 to 8 cores while the server uses about 1.6 of 4 | the tables above | a rerun shows the plateau tracking core count |
| F2 | CPU per request is 30-45% below Go's at every concurrency | the tables above | a rerun with the same method shows otherwise |
| F3 | Committed memory per idle connection is about 187 KiB against about 22 KiB | idle runs, 10,000 connections | a breakdown attributes it elsewhere (see H5) |
| H1 | One loop thread serializes all socket work and is the ceiling | F1; every socket operation is a command on the one libuv loop | per-thread CPU shows the loop thread well below 100% at the plateau |
| H2 | The per-request cost on the loop thread (two submissions, a read start and stop, two timer starts, a write request) sets the plateau, so removing calls raises it | measured call counts; H1 | removing calls moves per-request counts but not the plateau |
| H3 | The `Task.yield` after every reusable response adds a scheduler hop that is redundant unless another request is already buffered | a connection with no buffered request parks in its next read, which also yields | a fairness fixture shows a sibling Task starved without it, or removing it shows no change in cost |
| H4 | The Linux/WSL2 cost per request is dominated by cross-thread wake-ups (futex), not by computation | 260-270 us against 41-60 us, with the same counts | a native Linux run shows the Windows-like cost |
| H5 | The committed memory is the fixed per-connection buffers (32 KiB receive, about 72 KiB output, 40 KiB head, about 6 KiB tables) plus the Task stack and control records | the configured sizes total about 150 KiB | a breakdown shows a different dominant term |
| H6 | Connection churn is bounded by per-connection allocation and Task creation, not by the loop | 11 allocations and 156 KB per connection; 448 us CPU | profiling shows the accept path or the loop dominating |

## Decisions

1. **Measure before changing.** Phase 0 records per-thread CPU, a memory
   breakdown, and per-request counts in a committed harness. No optimization is
   merged before the evidence that motivates it exists in the repository.
2. **Reduce the loop thread's work before changing its number.** Steps that
   keep one loop and remove calls come first (Phase 1). More loops are
   considered only if the plateau is still loop-bound afterward (Phase 4, a
   decision gate, not an authorization).
3. **Preserve every guarantee.** No change may weaken exactly-once Task wake,
   retention of native-accessible buffers until quiescence, close-cancel
   semantics, absolute phase deadlines, the active-connection ceiling, or the
   ownership tables of archived RFC 0194. A worker thread still never calls
   libuv on a socket; the filesystem worker pool stays distinct from socket
   waiting.
4. **Adopt by measurement, record by exclusion.** A change is kept only when it
   improves its target metric by more than the run-to-run range without a
   regression beyond the range elsewhere; a change that fails is removed and
   recorded as excluded with its figures, so it is not proposed again. Every
   non-obvious constant or ordering that a measurement selects carries an
   adjacent CARE comment naming the workload, alternatives, figures, and the
   condition that requires remeasurement. Safety bounds are not labeled as
   measured optima.
5. **No public change.** Observable behavior, API, defaults, and
   `docs/reference.md` stay as they are. A change that cannot be made without
   altering observable behavior stops and is brought back as a spec change.
6. **Competitive figures are context, not acceptance.** Go is compared to
   explain the ceiling; no phase is accepted or rejected on matching another
   server's numbers.

## Candidate changes

Each is a candidate until its phase measures it.

**C1. Lazy deadline timers (Phase 1).** Today each read and write with a
deadline starts a libuv timer and stops it on completion (two starts per
request). A timer instead stays armed at the earliest outstanding deadline: when
it fires it re-checks the operation's absolute deadline and re-arms for the
remainder or stays idle (the existing early-expiry path already re-arms), and a
new operation restarts it only when its deadline is earlier than the armed one.
In steady state deadlines move later, so timers are neither stopped nor
restarted per request. Constraint: no stale timer callback may reach a settled
request record; the control-embedded timers and synchronous stop at close remain.

**C2. Non-blocking write attempt (Phase 1).** Before queuing a `uv_write`, the
loop thread tries `uv_try_write`; a complete write wakes the Task at once with no
write request or completion callback, and a partial write or `EAGAIN` queues the
unwritten remainder as today. Constraint: per-target behavior of `uv_try_write`
on TCP must be verified on both qualified targets, and the write deadline and
close-cancel orderings must be unchanged.

**C3. Yield only when a request is already buffered (Phase 1).** The `Task.yield`
after each reusable response is kept when the receive buffer already holds
bytes of another request (a pipelined remainder) and dropped otherwise, because
the next read then parks. Constraint: a fixture shows that a connection with a
long pipeline cannot starve a sibling Task, and that the change does not alter
response order.

**C4. Arm-once reads (Phase 1, design record first).** Removing the read
start/stop and the read submission per request needs a loop-owned inbox so that
libuv can keep reading while the Task works, which changes buffer ownership.
It is attempted only after C1-C3 are measured and only with a written ownership
and compaction record that keeps archived RFC 0194's retention rules; otherwise
it is excluded.

**C5. Cheaper wake-ups (Phase 2).** The loop thread readies Tasks through the
scheduler's ready queue, which publishes under a lock and signals a sleeping
worker per ready Task. Candidates: publish the Tasks readied in one loop
iteration together and signal once; let an idle worker spin briefly before
sleeping. Constraint: spin length is a measured trade of CPU for latency with
its figures recorded; the scheduler's park/commit protocol is not altered.
Decided on a native Linux run as well as Windows (H4).

**C6. Per-connection memory (Phase 3).** After the Phase 0 breakdown, reduce the
dominant terms: acquire the receive, output, and head storage from a bounded
pool only when a request's first bytes arrive and return them when the exchange
is complete and the receive buffer is empty, so an idle connection holds its
record and a small probe buffer. Constraint: the Request/Writer borrow rules
hold (head storage valid until the parser advances); steady state on the fixed
route keeps 0 allocations (pool hits, not heap calls); pool growth is bounded by
`max_connections`.

**C7. Churn (Phase 3).** Pool the connection record and, if the breakdown shows
Task creation dominating, evaluate Task stack reuse (named as a conditional
follow-up in archived RFC 0144). Constraint: a reused stack keeps guard pages
and root affinity.

**C8. Multiple loops (Phase 4, gate only).** Sharding connections over several
loop threads is the only change that can raise the ceiling beyond one thread's
capacity. It touches the global event loop, the handle registry, and listener
distribution (a listener per loop with `SO_REUSEPORT` on Linux; no equivalent
on Windows), so this record does not authorize it. Phase 4 decides, from the
Phase 1-3 evidence, whether a separate record is justified.

## Rejected

- **Socket I/O on scheduler worker threads** (try a read or write on the worker
  before parking). It removes hand-offs but contradicts the runtime's ownership
  rule that sockets are touched only on the loop thread. It stays rejected unless
  a future record replaces that rule.
- **Zero-copy transfer** (`sendfile`, `TransmitFile`): deferred by archived RFC
  0200 and unrelated to the plaintext path.
- **Scheduler, context-switch, or stack-class rewrites** without a measured
  bottleneck, as archived RFC 0144 already states.
- **Gathering pipelined responses into one write** is not in scope: it helps only
  pipelined clients and the plaintext baseline does not select it.

## Required constraints on measurement

The harness is the suite specified by RFC 0258, whose load client is bombardier, and
meets these requirements.

- It lives in the repository, runs only under an explicit tag or command, and never
  affects `go test ./...` or the ordinary suite, which stay free of external
  processes. It builds the server programs through the ordinary compile path and
  records the exact command, host, toolchain, and server cores.
- Where the suite and this list differ, the suite governs how a workload is measured
  and this record governs what the measurement must show. Bombardier has no
  connection-per-request or idle-connection mode and does not document its latency
  as coordinated-omission corrected; the suite's own harness drives the idle
  workloads, a fixed offered rate uses bombardier's rate limit with its latency
  reported as bombardier defines it, and connection churn (C7) needs a measurement
  the suite does not provide, which Phase 3 must supply or record as not measured.
- Server and client are pinned to disjoint cores; the client's own CPU is recorded
  so a saturated client is visible. Each run starts a fresh server. Runs of the
  compared builds are interleaved and repeated at least three times; results report
  the median and the range.
- Workloads: closed-loop keep-alive at 1, 8, 64, 256, and 1,024 connections; scaling
  over 1, 2, 4, and 8 server cores; fixed offered rates; idle connections at 1,000
  and 10,000 recording working set and committed memory; startup; idle CPU. These
  are the suite's workloads W1 to W5; connection per request is added only if a
  phase needs it, with port reuse accounted for (Windows exhausts its dynamic ports).
- Per request it records latency percentiles, CPU time, allocations, loop-thread
  submissions, and libuv calls. Per-thread CPU (loop thread against workers) is
  recorded. A tagged C23 fixture asserts the count metrics (submissions, timer
  starts, allocations per keep-alive request), which are deterministic, so a
  regression of those counts fails a test while timing remains a recorded
  measurement.

## Required sweep

- the harness and the count fixture (Phase 0);
- `compiler/generator/packages/network.c` and `event.c` (timers, write path,
  submissions), `compiler/corelib/runtime/server.c` (yield, buffer lifetime),
  `packages/concurrency.c` (wake and spin), as each phase selects;
- the snippet manifest, reviewed by artifact family: movement is expected only for
  snippets that select the changed runtime files, and any other movement is a
  finding;
- the CARE comments that record measured selections;
- `docs/reference.md`: verified unchanged, or updated in the same change if a step
  proves to alter observable behavior.

## Validation

This section is exhaustive:

- the harness (RFC 0258's suite) exists in the repository, is excluded from the
  ordinary suite, records its host, toolchain, command, and server cores, and
  reproduces the closed-loop, scaling, startup, and idle-memory rows of the evidence
  table above within run-to-run variation on the Windows target; the open-loop and
  connection-per-request rows came from a different client and are not expected to
  match;
- per-thread CPU at the plateau is recorded, and H1 is stated as confirmed or
  falsified with the figures; the memory breakdown for an idle connection (buffers,
  Task stack, control and registry records) is recorded and H5 is stated likewise;
- each adopted candidate has an interleaved before-and-after record meeting
  decision 4; each candidate that fails is removed and recorded as excluded with
  its figures;
- the tagged count fixture asserts the per-request submission, timer-start, and
  allocation counts after the adopted changes, and the keep-alive fixed route keeps
  0 allocations in steady state;
- the existing fixtures pass unchanged on both qualified targets: close-cancel,
  deadline, shutdown and admission, accept burst, routing and framing, lifecycle,
  and the file-server tests; a fixture shows that a pipelined connection does not
  starve a sibling Task if C3 is adopted;
- if C6 is adopted, committed memory per idle connection at 10,000 connections is
  recorded before and after, and the Request/Writer borrow rules and the ownership
  tables of archived RFC 0194 still hold;
- if C5 is adopted, the figures include a native Linux run, and the spin length
  carries a measured CARE rationale;
- no public API, default, or documented behavior changes, and `docs/reference.md`
  is verified against the result;
- Phase 4 records, from the evidence, whether a multi-loop record is justified;
- ordinary gates, focused C23 fixtures for HTTP and network, and short C23 pass;
  exhaustive C23 runs require separate user consent.

## Implementation plan

### Phase 0: harness and evidence

Implement RFC 0258's suite (its own phases) and use it to recreate the evidence
table's closed-loop, scaling, startup, and idle-memory rows, and add the per-thread
CPU and idle-connection memory breakdown. Add the count fixture
with the current counts as its first assertion. Record H1 and H5 as confirmed or
falsified. Exit: the baseline is reproducible from the repository and every
hypothesis in the table has a stated status.

### Phase 1: loop-thread work

Implement C1, C2, and C3 as three separate changes, each with its own interleaved
record and its own commit, in the order of expected benefit as the Phase 0 figures
suggest. Then decide C4: write its ownership record, or exclude it. Exit: each
change is adopted or excluded on its figures; the count fixture reflects the adopted
counts; every existing fixture passes.

### Phase 2: wake-ups

Measure C5 on Windows and on a native Linux host. Adopt a candidate only on the
decision-4 rule, record the spin figures, and keep the scheduler's park protocol
intact. Exit: adopted or excluded with Windows and Linux figures.

### Phase 3: memory and churn

Using the Phase 0 breakdown, implement C6 and C7 as far as the dominant terms
justify. Exit: committed memory per idle connection and churn throughput recorded
before and after; steady-state allocations unchanged at 0.

### Phase 4: ceiling decision

With Phases 1-3 complete, rerun the scaling table and the per-thread CPU. If the
plateau is still loop-bound and the workload justifies it, write a separate record
for C8; otherwise record that one loop suffices for the measured range. Exit: the
decision and its figures are recorded here.

### Phase 5: conformance and handoff

Review manifest movement by artifact family, verify `docs/reference.md`, run ordinary
test, vet, and build, the focused HTTP and network C23 fixtures, and short C23.
Close only when Validation is satisfied: update the Status header, move this record
to `docs/specs/archived/`, and remove its entry from `docs/status.md`. Rebuild
`hexal` and restart `hexal play`.

## Remaining decisions

None for the author. Open measurements, not choices: the order of the Phase 1
candidates (decided by the Phase 0 figures), the pool bound and spin length (decided
by measurement), and whether a native Linux host is available for Phases 2 and 3; if
none is, those phases record the Windows and WSL2 figures and say that native Linux
was not measured.

## Reference synchronization

None expected: the work preserves observable behavior. If any step changes it, the
change updates `docs/reference.md` in the same commit and returns to this record as a
scope change before merging.
