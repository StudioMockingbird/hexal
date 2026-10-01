# RFC 0144: High-Throughput Network Runtime

- Kind: Architecture Decision Record (ADR)
- Status: Closed. Phases 0-5 landed: deadline-bearing read and write and
  close-cancel of parked read, accept, and write on the existing event bridge;
  an accept defect that stalled every connection after a burst, found by the
  baseline and fixed; qualification on `x86_64-windows-gnu-ucrt` and
  `x86_64-linux-gnu`; and the named baseline recorded below. Scheduler,
  context, and stack-allocation changes were not needed and are not made
- Created: 2026-09-07
- Updated: 2026-10-01
- Scope: Task-aware socket and timer operations built on the libuv foundation
- Depends on: RFC 0132 (root scheduler bootstrap) and RFC 0145 (libuv async
  runtime backend)
- Coordinates with: RFC 0039 (C interoperability), RFC 0052 (C compiler
  backend), RFC 0055 (filesystem/build driver), RFC 0118 (concurrency safety),
  RFC 0145 (libuv async runtime backend), and the current Task, IO, Stash,
  Pool, String, List, and Slice contracts in `docs/reference.md`
- Does not define: final socket syntax, an HTTP API, HTTP parsing, routing,
  middleware, TLS, HTTP/2, HTTP/3, or a benchmark-derived performance promise

## Goal

Define and implement the runtime boundary that lets socket operations park
Tasks without occupying scheduler workers and resolve completion, timeout,
cancellation, and close races exactly once. RFC 0145 supplies libuv; this RFC
integrates its event delivery with Hexal Tasks. HTTP policy, per-connection
buffer limits, and server shutdown behavior belong to RFCs 0194 and 0210.

Throughput, latency, CPU cost, and memory use are measured outcomes, not a
release gate against Axum/Hyper/Tokio or May. Such comparisons are useful only
when workload, hardware, operating system, toolchain, optimization, and worker
count are equivalent. Every optimization requires measurement against an
unchanged functional baseline.

## Conclusion

The current stackful-fiber design and libuv event-loop foundation are viable.
The Task park/commit/wake protocol, M:N workers, synchronous-looking calls,
Channels, Mutexes, joins, guarded stacks, and separate blocking-operation path
are useful building blocks.

The remaining gap is the scheduler-facing socket adapter and its wait-record
contract. Libuv provides non-blocking event delivery, but it does not define
Hexal Task parking, exactly-once wakeup, or wait-record lifetime. Using the
libuv worker pool for steady-state socket waits would turn idle connections
into blocked work items and defeat the M:N scheduling goal.

The first server cut requires the first two capabilities below. The third is
an optimization program, not a prerequisite:

1. scheduler-integrated non-blocking sockets;
2. monotonic deadlines and cancellation for outstanding socket waits;
3. measurement of scheduler and allocation costs, with changes only when a
   bottleneck is demonstrated, especially around the global ready queue and
   per-Task stack/context allocation.

## Existing foundation

- `Task<R>` is a stackful coroutine scheduled cooperatively over native CPU
  workers.
- A Task may park without blocking its scheduler worker.
- Park, wake, and resume have an exactly-once publication protocol with
  release/acquire payload visibility.
- Channel, Mutex, join, and scheduler-aware blocking calls share that protocol.
- RFC 0145 provides the shared libuv loop, cross-thread submission, and worker
  pool for genuinely blocking operations; steady-state socket waits use the
  event loop instead of that pool.
- Task stacks have explicit reserve/commit settings and overflow guards.
- RFC 0132 keeps the initial-process root fiber on worker zero.
- Slices and byte collections permit parsers to work without immediately
  converting every input field into an owned String.
- Stash and Pool provide future request-lifetime allocation strategies.

These properties should be retained unless a benchmark and replacement design
show a material benefit.

## Reconciled implementation baseline

The current TCP component already submits accept, read, write, shutdown, and
close through the event bridge and parks Tasks. Task-aware sleep also exists.
This ADR does not authorize implementing these foundations twice. The remaining
work is deadline/terminal cleanup, target qualification, and measured throughput.
Existing handle aliasing, busy-operation, and half-close semantics in
docs/reference.md are the starting contract; changing them needs an explicit decision.

The source probe finds uv_read_start/uv_read_stop per completed read, uv_write
with one buffer, and hex_event_submit for writes. event submission arms and
suspends the Task. The ready queue uses one mutex, signals ordinary Tasks, and
broadcasts root. libuv's async pending flag coalesces notifications. These facts
establish cross-thread submission and wake costs, not five syscalls per read or
a demonstrated bottleneck. POSIX mmap plus mprotect also consumes mappings;
connection-scale qualification measures actual map count and committed memory,
not just reserved address space. No universal OS map-count default is assumed.

## Review disposition and performance acceptance

Keep llhttp and the current event/scheduler architecture as the baseline.
Thread-per-core loops, connection affinity, assembly context switches, stack
caches, and smaller stack classes are comparative candidates, not mandatory
optimizations justified solely by source inspection. Pinning network Tasks does
not remove migration or cross-thread wakes for the rest of the language.
A loop per CPU worker also needs an explicit event-polling/fairness protocol;
a CPU-bound handler must not prevent that worker's loop servicing other sockets.

Record release echo and HTTP keep-alive plaintext baselines, streaming/slow-client
loads, and connection churn on a named host/toolchain with fixed workers,
concurrency, payloads, warmup and repeated runs. Record throughput, p50/p99,
CPU by thread, allocation/copy counts, submissions and resumes per request,
bytes per idle connection, mapping count, and spawn/switch/wake latency.
Record variance before proposing a performance-regression tolerance; neither an
invented percentage nor an Axum/may ranking is a completion gate.

Do not replace swapcontext before measuring the pinned libc implementation and
qualifying migration/stack guards. An alternative must preserve ABI registers,
stack alignment, overflow reporting and sanitizer hooks on each qualified target.
Unsupported target qualification is a release blocker, not a performance waiver.
Current source does not prove comparative picohttpparser/llhttp throughput.

Shared review decisions are indexed in RFC 0210. This ADR remains not ready
until its exact deadline and terminal-cleanup contract is written.

## Required network architecture

RFC 0145 owns the libuv loop and platform backend. This RFC owns the private,
target-neutral operations that connect that backend to Hexal Tasks: socket
operation completion, wait-record ownership, parking, wake publication,
timeouts, cancellation, and shutdown races. RFC 0194 consumes those operations
for HTTP and does not own libuv handles or callbacks. No public socket syntax
or exact internal function names are selected here until the contract is
implementation-ready.

### Socket operations

Sockets use non-blocking or asynchronous native APIs. A socket operation:

1. consumes already buffered data or attempts an owner-thread immediate operation
   when the backend supports it; no direct worker-thread access to a libuv handle
   is implied;
2. returns immediately when it completes;
3. on would-block, registers one live wait record with the network driver;
4. parks the current Task through the common Task protocol;
5. is woken by readiness, completion, timeout, cancellation, or shutdown;
6. retries a readiness-based operation or consumes a completion result;
7. unregisters exactly once on every terminal path.

Expected platform families:

| Target | libuv baseline | Later optional mechanism |
| --- | --- | --- |
| Linux | libuv over `epoll` | `io_uring`, only after measurement and qualification |
| macOS | libuv over `kqueue` | none assumed |
| Windows | libuv over IOCP | none assumed |

The portable runtime contract describes operation completion rather than
exposing raw readiness semantics; IOCP is completion-based while `epoll` and
`kqueue` are readiness-oriented.

### Blocking worker boundary

Libuv's global worker pool remains appropriate for operations without a
suitable non-blocking interface, including ordinary file operations,
synchronous DNS, and foreign calls explicitly classified as blocking. Hexal
does not retain a second general-purpose pool.

Steady-state socket `accept`, `connect`, `read`, and `write` must not use the
libuv worker pool. Thousands of parked socket Tasks must require a bounded
number of native threads.

A would-block result is internal scheduler control flow, not a Hexal `Error`.
Only a terminal native failure crosses the language boundary as Error.

### Reactor ownership

RFC 0145 owns the one program-wide libuv loop on one dedicated native thread
for the first implementation. The Task-aware socket contract does not expose
loop ownership. Preserve the backend boundary so measurement may justify later
sharding without changing the runtime contract.

## Timers and operation cancellation

Task-aware socket operations require monotonic deadline support, cancellation
of an outstanding wait, and one winner when completion, timeout, cancellation,
and operation close race. Cancelling a timer or wait must not publish a stale
wake. Protocol-specific deadlines and graceful server shutdown belong to the
owning feature.

The runtime may initially expose deadline-bearing socket operations instead of
a general language-level `select`. One Task has one active wait registration;
the network driver and timer owner resolve competing completion sources before
publishing the Task exactly once.

Cancellation must not resume a Task while a native backend can still write into
its stack-resident wait record or buffer.

## Scheduler scaling

### Current risk

The current scheduler uses one mutex-protected global FIFO for spawn, yield,
and wake. Every worker competes for the same lock and queue cache lines.

This is correct as an initial implementation but may cap throughput under high
connection counts and cross-thread network wakes.

### Candidate measured successor

- One local ready deque per CPU worker.
- Same-worker wakes remain local.
- External network/timer wakes enter a bounded global injection path or the
  target worker's remote queue.
- Idle workers steal ordinary Tasks from other workers.
- Root remains worker-zero-affine and outside stealing.
- Global work is sampled often enough to prevent starvation.
- Queue ownership, task migration, and wake publication retain the common Task
  phase protocol.

Do not replace the global FIFO solely because local queues are conventional.
First measure mutex contention, queue wait time, wake latency, cache misses, and
scaling across workers. The replacement must improve the measured server or
scheduler benchmark without weakening determinism or safety.

## Fiber context portability and cost

- Windows Fiber APIs explicitly support switching a synchronized fiber created
  by another thread.
- The POSIX backend currently depends on `ucontext`, an obsolete interface
  removed from POSIX.1-2008 for portability reasons.
- Ordinary non-root Tasks may migrate between workers through the global queue.
- Cross-thread context migration, signal-mask behavior, sanitizer hooks, and
  libc support must be verified for every supported POSIX target.
- Context-switch cost must be benchmarked independently from scheduler queue
  and wake cost.

If `ucontext` is unsupported, unsafe across the required migration pattern, or
materially slower, replace only the context layer. Candidate replacements are
a small architecture-specific switch for the supported x86-64/AArch64 targets
or a qualified dependency. Do not rewrite the scheduler protocol with it.

## Task and stack allocation

Current spawn may allocate a Task control block, argument frame, result frame,
context record, and stack mapping. High connection churn can make those costs
visible even when steady-state scheduling is efficient.

Measure before changing. Candidate improvements, in order:

1. reuse completed Task control blocks;
2. cache stacks by reserve class per worker;
3. reuse common argument/result frame size classes;
4. batch or slab allocation only if the preceding measures remain material.

The default 1 MiB reserve is virtual address space, not necessarily committed
physical memory, but large Task counts still consume address space, mappings,
page tables, and guard pages. Record peak committed stack and high-water usage
before changing the default. A smaller server profile must retain deterministic
overflow trapping.

No pooling may expose stale task state, skip required zero initialization,
reuse a stack before the previous Task's final dispatcher commit, or change the
observable Task API.

## Cooperative fairness

Hexal currently requires an explicit `Task.yield()` on every repeating path
through a task-reachable literal `while true`. A network operation may park,
but immediate completion does not guarantee a scheduling point.

HTTP request batching and yield points are protocol policy owned by RFC 0194.
The runtime does not make an immediately completing socket operation an
implicit scheduling point.

Potential later alternatives require a language/runtime decision:

- treat scheduler-aware I/O as a syntactic yield, risking starvation when it
  repeatedly completes immediately;
- insert unconditional loop-backedge yields, adding overhead to all loops; or
- use a small cooperative operation budget, adding runtime accounting but
  yielding only after bounded work.

No alternative is selected until the initial server measures starvation and
yield overhead.

## Root Task usage

RFC 0132 broadcasts when root becomes ready because only worker zero may run
it. That cost is acceptable for control-plane work but unsuitable for every
connection or request wake.

The server runtime keeps root off the hot path: root initializes the server,
starts ordinary acceptor/connection Tasks, and waits for shutdown. Ordinary
network Tasks use ordinary targeted ready publication. A benchmark must record
root broadcasts; frequent broadcasts indicate that hot-path work remained on
root or that targeted worker-zero wakeup needs a later design.

## Task-local and foreign thread-local state

An ordinary Task may resume on a different native worker. Native `_Thread_local`
or foreign-library TLS therefore describes the current worker, not stable Task
state. A Task must not retain an assumption about native TLS across a scheduling
point.

Future Task-local storage, if required, is scheduler-owned and distinct from
native TLS. C interoperability must specify whether a foreign call can yield or
re-enter Hexal before permitting TLS-sensitive libraries on migrating Tasks.

## Byte and buffer boundary

Socket operations transport bytes and retain buffers only for the lifetime
specified by their wait record. They do not parse HTTP, choose request/response
buffer sizes, or impose protocol backpressure policy. RFCs 0194 and 0198 own
HTTP buffering, incremental parsing, and backpressure; application-visible
strings and request-lifetime allocation remain with their language contracts.

## Additional performance risks

| Risk | Likely effect | First response |
| --- | --- | --- |
| One global ready mutex | worker contention and cache bouncing | measure, then local queues/work stealing |
| Native thread per blocked socket | memory and scheduler collapse | non-blocking network driver |
| Per-spawn allocations/mappings | connection-churn latency | Task/stack caches after profiling |
| Fixed large stack reserve | address-space and mapping cost | measure high-water use |
| Full `ucontext` switch | context-switch and portability cost | isolate benchmark; replace context layer if material |
| Root broadcast on hot path | thundering wakeups | ordinary acceptor/connection Tasks |
| Unconditional per-request yield | excess context switching | bounded batching |
| Never-yielding ready connection | unfairness and tail latency | explicit yield initially; measure budget later |
| Owned String per header | allocation and copying | byte Slices and request-lifetime storage |
| Unbounded queues/buffers | memory exhaustion and latency collapse | backpressure and hard bounds |
| No deadlines/cancellation | slow-client resource retention | timer-integrated waits |
| Central reactor | cross-worker wake contention | measure before sharding |

## Measurement contract

All measurements use release/optimized builds, fixed worker counts, a warmed
server, pinned protocol/features, and recorded hardware, OS, toolchain, command,
connection count, request duration, and response size.

### Runtime microbenchmarks

- Task spawn/join and detached completion.
- Fiber context switch without queue contention.
- Yield/requeue round trip.
- Same-worker and cross-worker park/wake latency.
- Ready-queue throughput from one through all workers.
- Network-driver registration, readiness/completion, timeout, and cancellation.
- Task and stack creation versus cache reuse.
- Idle memory for 1,000, 10,000, and 100,000 parked connections where the host
  permits them.

### End-to-end server benchmarks

1. TCP echo with fixed payloads.
2. HTTP/1.1 fixed response without routing.
3. HTTP/1.1 keep-alive.
4. Pipelined requests where supported by all compared servers.
5. Dynamic route with parsing and response formatting.
6. Slow clients exercising deadlines and cancellation.
7. Connection churn and long-lived idle connections.

Record:

- requests or bytes per second;
- p50, p95, p99, and maximum latency;
- CPU time and utilization;
- resident and virtual memory;
- allocations and bytes allocated per request;
- native thread count;
- syscalls and involuntary context switches where available;
- failures, timeouts, and dropped connections.

Compare against Axum/Hyper/Tokio and May using equivalent HTTP versions,
keep-alive behavior, response bodies, logging, TLS state, worker counts, and
client load. Do not compare a minimal HTTP/1 server to a framework configuration
performing materially more work.

## Implementation plan

### Phase 0: inventory and establish the baseline

Inspect packages/network.c/.h, event.c/.h, handle.c/.h and concurrency.c, plus
generator/network.go, network_render.go and event_component.go. List existing
accept/read/write/shutdown/close/sleep entrypoints, resource owners and demand
flags. Reuse the implemented root bootstrap; do not implement RFC 0132 again.
Run the existing bounded Task/network fixtures named by Validation. Record the
Measurement contract's named workloads, release flags, host, workers and variance.
Exit: an implementation map in this spec distinguishes reused code from required
deadline/cleanup changes, and there is a reproducible baseline before optimization.

### Phase 1: pin the private operation contract before code changes

Add concrete C request/terminal-state records to this spec: operation kind, Task,
native request, result/status, deadline/timer, native ownership and cleanup state.
Write transition tables for completion-before-park, park-before-completion and
completion/timeout/close winners. Identify who arms/disarms each timer and who
acknowledges native quiescence. A logical timeout is not permission to resume a
stack-owning Task before the final native acknowledgment. Map every row to an
existing Validation case; no general select/cancel/Task-interrupt API is added.
Exit: signatures, record lifetime and exact cleanup/wake owners are written here;
the implementation does not discover them by trial and error.

### Phase 2: implement deadline cleanup on the existing event bridge

Change the smallest existing network/event paths; keep libuv calls on the loop
thread and the shared arm/commit/wake protocol. Carry absolute monotonic deadlines
from the HTTP layer. Timer callbacks record terminal intent; read/write/accept
cleanup completes before the one result/wake publication. Handle immediate native
failure, close in flight and timer-init failure through the same ownership table.
Do not add a second reactor, blocking socket job or competing wait protocol.
Exit: ordinary generated-text/unit checks prove demand isolation and the selected
state layout; focused native fixtures prove the specified first-winner paths.

### Phase 3: qualify lifecycle and context behavior

Run repeated completion/park and timeout/close orderings for the qualified target
profiles, retaining guards, root affinity and valid native buffer lifetime.
Record which migration/context combinations were actually qualified. A failed
correctness qualification blocks that target; a green ordinary suite is not C
execution evidence. Keep all fixture additions inside this spec's Validation.
Exit: no stale wake/native access in the named matrix and explicit per-target
qualification evidence; remaining targets remain unqualified, not implied support.

### Phase 4: hand off to the HTTP arc

Publish the private signatures/layout and timeout ownership to 0194/0198/0210.
0198 pins parsing; 0210 registers the approved public resources; 0194 integrates
the connection state machine. They may prepare in parallel but cannot invent
different native request lifetimes. 0208 and 0200 do not gate the dynamic server.
Exit: the default HTTP fixtures and benchmark can use one qualified network path.

### Phase 5: conformance and measured follow-ups

Run ordinary test/vet/build, then the actual bounded Task/network C23 fixture set
and short C23 gate under repository parallelism rules. Review legitimate snippet
manifest changes by artifact family; do not regenerate to conceal failures.
Sync affected reference contracts once and remove completed status entries only
after Validation is satisfied. Rebuild hexal/restart hexal play as handoff.
Re-run the baseline and record throughput, latency, allocations/copies, submissions,
mapping/idle memory and worker scaling. Scheduler sharding, assembly switching,
stack caches and vectored I/O require separately bounded, measured follow-up work;
they are not unfinished mandatory phases of this first implementation.

## Pinned records

### Implementation map (Phase 0)

| Concern | Owner | Disposition |
| --- | --- | --- |
| accept/read/write/shutdown/close entry points, handle leases, busy flags | `packages/network.c`, `network.h`, `handle.c` | reused unchanged in shape |
| submit, park, wake | `event.c` `hex_event_submit`, `concurrency.c` arm/suspend/wake | reused; no second wait protocol |
| Task sleep timer | `event.c` | reused as the timer precedent; network deadlines use control-embedded timers |
| per-socket native state and parked-operation pointers | `hex_tcp_control` | extended: state, `read_op`, `write_op`, `closer`, two lazily initialized timers |
| deadline-bearing read and write | `hex_tcp_read_until`, `hex_tcp_write_until` | new; the public `hex_tcp_read`, `hex_tcp_write` call them with deadline 0 |
| close of a parked operation | `hex_tcp_begin_close` | new; replaces a close that left a parked read or accept waiting forever |
| accept deadline | none | not added: the server stops accepting by closing the listener, and no configured timeout bounds accept |
| accept | `hex_tcp_accept` | changed: accept is a loop-thread command that returns the connection in its request record; the listener holds `pending_client`, `accept_declined`, and `spare_client` (see Transitions) |

Before this change a parked read or accept never woke when another Task closed its
handle, because libuv delivers no read or connection callback after `uv_close`;
the reference already specified Closed for that case.

### Records and ownership (Phase 1)

- Deadlines are absolute `hex_instant` (monotonic nanosecond) readings; zero means
  none. A deadline already past at operation start settles immediately.
- `hex_tcp_control` fields below the busy flags are loop-thread-owned.
  `state` is `OPEN`, `CLOSING` (`uv_close` issued, close callbacks outstanding), or
  `CLOSED`. `read_op`, `write_op`, and `accept_command` name a parked Task's
  stack-resident request only between that request's start and the one decision
  that wakes it. `closer` is the waiting explicit close request, if any.
- Request outcome: `PENDING`, `DONE` (native completion or native failure, status in
  the record), `DEADLINE`, `CLOSED`. Public statuses: `DEADLINE` returns
  `HEX_NETWORK_TIMED_OUT`, `CLOSED` returns `HEX_NETWORK_CLOSED`.
- Timers live in the control block (`read_timer`, `write_timer`), initialized on
  first deadline use on the loop thread and closed with the socket. Stopping one is
  synchronous, so a timer never has to outlive the Task stack and no stale timer
  callback can reach a request record. `close_pending` counts the socket and
  initialized timers; the last close callback sets `CLOSED` and wakes `closer`.
- Early timer expiry, judged by `uv_hrtime`, re-arms instead of settling.
  A timer init or start failure completes the operation with that libuv status.

### Transitions

Read (loop thread; each row wakes the Task exactly once):

| Event while a read is parked | Winner | Action | Result |
| --- | --- | --- | --- |
| `uv_read_cb` with nread != 0 | completion | `uv_read_stop`, stop timer, wake | bytes, `EOS`, or native error |
| read timer fires at or after the deadline | deadline | `uv_read_stop`, stop timer, wake | `HEX_NETWORK_TIMED_OUT`; connection stays usable |
| another Task closes the connection | close | `uv_read_stop`, stop timer, wake; then `uv_close` | `Closed` |
| start finds `state != OPEN` | close | wake | `Closed` |
| start finds the deadline past | deadline | wake without reading | `HEX_NETWORK_TIMED_OUT` |

A read that completes before the Task parks wakes through the same path: the
start command runs on the loop thread after the Task has armed and suspended.

Write (one `uv_write_t` per chunk; the deadline is shared by all chunks):

| Event while a write is parked | Winner | Action | Result |
| --- | --- | --- | --- |
| `uv_write_cb` with status | completion | stop timer, wake | success or native error |
| write timer fires at or after the deadline | deadline | mark `DEADLINE`, `uv_close` of the socket; libuv completes the write with `UV_ECANCELED` before the close callback, and that callback is the one wake | `HEX_NETWORK_TIMED_OUT`; connection closed natively |
| another Task closes the connection | close | mark `CLOSED`, close timers and socket; the write callback is the one wake | `Closed` |
| start finds `state != OPEN` | close | wake | `Closed` |
| start finds the deadline past | deadline | `uv_close`, wake (no write in flight) | `HEX_NETWORK_TIMED_OUT`; connection closed natively |

A later explicit close of a connection closed by a write deadline finds
`CLOSING` (waits for the close callbacks) or `CLOSED` (returns at once).

Accept (loop thread; every accept is a command, so the Task never reads listener
state): the request record carries the accepted connection, `client`, set before the
one wake. The listener holds at most one unclaimed connection, `pending_client`.

| Event | Action | Result |
| --- | --- | --- |
| connection arrives, an accept is parked | `uv_accept` into the spare or a new socket, store it in the parked record, wake | the connection |
| connection arrives, no accept parked, slot free | `uv_accept`, store in `pending_client` | held for the next accept |
| connection arrives, slot occupied | leave it in libuv, set `accept_declined` | held by libuv: the Linux backend stops watching the listener, the Windows backend queues it |
| accept starts, `pending_client` set | hand it to the record, then try `uv_accept` again if `accept_declined`, wake | the connection |
| accept starts, nothing pending | park | woken by an arrival |
| a retry finds nothing | clear `accept_declined`, keep the unconnected spare socket for the next attempt | no allocation per probe |
| listener closes | wake the parked accept with no connection; close the spare and any unclaimed pending connection, all on the loop thread | `Closed` |

A start with `state != OPEN` wakes with no connection and returns `Closed`. The retry on
every accept that frees the slot is what keeps a burst of connections moving: before it,
a second connection arriving while the first was unclaimed was never accepted, because
libuv delivers no further callback until the held connection is taken. Fixture
`network-accept-burst-completes-every-accept-runs` connects four clients before the first
accept and requires four accepts.

Close: `CLOSED` wakes the closer immediately, `CLOSING` registers it, `OPEN`
registers it and begins the close. The closer wakes only after every native handle
of the control block has delivered its close callback, so the control block and
every parked request record outlive all native access to them.

### Validation mapping

| Row | Fixture |
| --- | --- |
| close wakes a parked read, accept, and write with Closed | `network-close-cancels-parked-operations-runs` |
| a burst of connections before the first accept: every accept completes | `network-accept-burst-completes-every-accept-runs` |
| existing TCP and Task behavior unchanged | `network-tcp-loopback-runs`, `network-tcp-loopback-stress-runs`, `inline-address-*`, `task-*`, `channel-*`, `mutex-*` |
| deadline winners | RFC 0194 server fixtures (header, body, idle read deadline; write deadline) |

## Follow-up ownership

RFC 0145 owns the libuv event-loop foundation; this RFC owns the Task-aware
socket/timer contract and adapter; RFC 0194 owns HTTP server behavior and
consumes that adapter; RFCs 0198 and 0210 own parsing and public semantics.
Deferred RFC 0208 owns future backend pluggability and is not a prerequisite.
No separate generic network-runtime specification is
needed. Scheduler local queues/work stealing, Task/stack reuse, and POSIX
context replacement remain conditional follow-ups: specify and implement them
only if qualification fails or measurement shows a material bottleneck.

This RFC does not authorize general async syntax or a futures system.

## Validation

This section is exhaustive for this ADR's eventual implementation. Before coding,
close the signature/record-layout decisions and replace design gates with exact
fixtures; this draft does not authorize inventing those contracts mid-implementation.

- Existing TCP and timer behavior is reused, with no second loop or wait protocol.
- Generated artifacts select networking only on actual network demand.
- For read, write, accept and close, completion-before-park and park-before-completion
  resume once; close/deadline races do not reclaim native-accessible buffers or
  stack wait records before the final native completion/cleanup acknowledgment.
- Deadline and close tests cover each allowed first-winner ordering, no duplicate
  ready publication, and no stale callback touching a resumed/freed frame.
- Reactor callbacks never execute handlers; socket waiting uses no blocking-pool job.
- Qualified context backends retain stack guards and root affinity; migration is
  either qualified or explicitly restricted by the approved runtime contract.
- The named baseline above is recorded without unsupported syscall or speed claims.
- Ordinary gates and bounded focused plus short C23 gates pass; exhaustive C23
  execution requires separate user consent.

## Approved direction

Use the existing single reactor, ordinary M:N Tasks and current scheduler as the
measured baseline. No thread-per-core rewrite, custom assembly, smaller stack class
or stack cache is required merely by source inspection. Existing target migration
must be qualified; failed correctness qualification blocks release regardless of
performance. Ordinary socket ownership/half-close/busy behavior follows reference.

Use private monotonic per-operation deadlines for HTTP read/accept/write waits;
do not add a general wait/select surface or Task interruption. Timeout/close must
not release a Task frame or buffer until the native owner acknowledges quiescence.
The loop thread owns timer arm/disarm and terminal native-operation cleanup.
No raw worker-thread libuv operations. Protocol timeout values belong to 0210.

The benchmark contract names echo, keep-alive plaintext, streaming/slow clients
and churn; host/load/variance are recorded before interpreting results. The deadline
request records and cleanup transitions are pinned under Pinned records, and the
engineering and qualification gates are closed under Closure record.

## Closure record

| Validation bullet | Evidence |
| --- | --- |
| existing TCP and timer behavior reused, no second loop or wait protocol | Implementation map: the event bridge, `hex_event_submit`, and the shared arm and wake protocol are unchanged in shape |
| networking selected only on network demand | ordinary tests `TestHttpTypeOnlyAndRegistrationDoNotSelectTheServerRuntime` and the demand rules of the HTTP surface |
| completion-before-park and park-before-completion resume once; close and deadline races keep native-accessible buffers and wait records alive to the final acknowledgment | the transition tables above (each row wakes exactly once; the Task resumes only after the last close callback); fixtures `network-close-cancels-parked-operations-runs`, `http-server-phase-deadlines-run`, `http-server-shutdown-and-admission-runs`, and the repeated qualification below |
| each allowed first-winner ordering: no duplicate ready publication, no stale callback on a resumed frame | read: completion, deadline, close; write: completion, deadline, close; accept: arrival, close. The orderings are forced by the fixtures above. A completion landing within the same loop iteration as a deadline cannot be scheduled from a test; the table fixes the winner by loop-thread order, and the repeated runs below sampled the interleavings |
| reactor callbacks never run handlers; socket waiting uses no blocking-pool job | handlers run on connection Tasks; ordinary test `TestHttpServerSocketWaitsUseNoWorkerPoolJob`; the thread count in the baseline is constant from zero to 10,000 parked connections |
| qualified context backends keep stack guards and root affinity; migration qualified or restricted | the existing scheduler contract is unchanged; both qualified hosts run the focused set below |
| the named baseline recorded without unsupported syscall or speed claims | Qualification and measurement record |
| ordinary gates, focused and short C23 | recorded in the implementing change |

## Qualification and measurement record

### Qualification (Phase 3)

Qualified: `x86_64-windows-gnu-ucrt` (Windows 11 Home, Clang) and `x86_64-linux-gnu` (WSL2
Linux 6.18, glibc, Clang 23.1.1). No other target profile was run: AArch64, musl, RISC-V and
macOS are unqualified, not implied.

Repeated first-winner and park/wake orderings: the focused HTTP and network fixtures
(`-run '^(TestC23Suite)$/^(http-.*|network-.*)$'`, which includes the close-cancel, deadline,
shutdown, and accept-burst fixtures) were run five times in one invocation on each host
(`-count=5`; Windows with `-parallel=8`), and the two file-server tests three times on each,
all passing. A park or close that raced its completion would have to surface as a hang past
the ten-second process bound or as wrong output; none did. This samples interleavings; it
does not enumerate them.

### Baseline (Phase 5)

Method. Release builds (`hexal build -mode release`, `-O2`), the default server
configuration unless noted, and a Go closed-loop load generator on the same host, so client and
server share the cores and the client's CPU is not measured. Each workload: one 1 s warm-up,
then five runs of 5 s with 64 connections; the tables give the median and the range of the five.
Latency is per request (per 16-request write for the pipelined workload). CPU is the server
process's user plus system time over the measured window. The load generator and the server
programs were scratch programs and are not part of the repository: the servers are one route
returning `hello` (fixed), one route reading the `User-Agent` header and formatting a body with
`String.interpolate` (dynamic), a TCP echo over `std/net`, and a mount of a directory
(static); the generator speaks plain HTTP/1.1 over TCP. The numbers describe these hosts and
this client; they are a baseline for later change, not a ranking, and no other server was
measured.

Hosts. Linux: WSL2 (kernel 6.18, x86_64), glibc, Clang 23.1.1. Windows: Windows 11 Home build
26300, Clang 23.1.2, target `x86_64-windows-gnu-ucrt`. Both on one AMD Ryzen 5 7530U (12
logical CPUs), Linux guest limited to 7.4 GiB, Windows host 15.4 GiB.

Linux (WSL2), 64 connections, median (range) of five runs. CPU is for the 5 s window; a value
of 10 s is two cores busy. Threads include the scheduler workers and the loop thread (13), plus
four filesystem workers once a file is served (17).

| Workload | Requests/s | p50 | p95 | p99 | Max | Server CPU | RSS | Failures |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| HTTP/1.1 keep-alive, fixed response | 7,571 (6,394-8,090) | 8.2 ms | 13.0 ms | 16.5 ms | 140-264 ms | 10.2 s | 35-39 MiB | 0 |
| keep-alive, dynamic route (header read, formatted body) | 7,662 (7,159-8,082) | 8.1 ms | 13.1 ms | 17.3 ms | 141-285 ms | 10.3 s | 37 MiB | 0 |
| pipelined, 16 requests per write (latency per write) | 12,508 (11,136-13,090) | 79.8 ms | 103.0 ms | 112.2 ms | 233-322 ms | 10.3 s | 34-38 MiB | 0 |
| one request per connection | 2,135 (2,105-2,272) | 28.9 ms | 35.5 ms | 39.2 ms | 38-52 ms | 8.5 s | 32 MiB | 0 |
| TCP echo, 64-byte payload | 8,817 (8,504-9,936) | 7.1 ms | 10.1 ms | 13.5 ms | 105-146 ms | 9.2 s | 13 MiB | 0 |
| TCP echo, 1 KiB payload | 9,446 (8,783-9,711) | 6.7 ms | 9.1 ms | 11.2 ms | 91-117 ms | 9.3 s | 13 MiB | 0 |

Windows 11, 64 connections, median (range) of five runs unless noted. Latencies below about
0.5 ms are quantized by the Go client's clock on Windows and are not reported. Threads: 19.

| Workload | Requests/s | p50 | p95 | p99 | Max | Server CPU | RSS | Failures |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| HTTP/1.1 keep-alive, fixed response | 26,197 (25,358-26,755) | 2.3 ms | 3.6 ms | 4.6 ms | 40-45 ms | 10.6 s | 15-16 MiB | 0 |
| keep-alive, dynamic route | 26,184 (24,198-27,025) | 2.4 ms | 3.4 ms | 4.4 ms | 36-41 ms | 10.8 s | 15 MiB | 0 |
| pipelined, 16 requests per write | 29,286 (24,279-33,484) | 35.4 ms | 52.6 ms | 65.3 ms | 70-98 ms | 9.7 s | 15 MiB | 0 |
| one request per connection (one 3 s run) | 3,868 | 8.0 ms | 11.3 ms | 14.0 ms | 3.0 s | 3.7 s | 11 MiB | 0 |
| TCP echo, 64-byte payload | 28,816 (22,753-29,520) | 1.9 ms | 3.3 ms | 4.8 ms | 60-94 ms | 8.5 s | 10-11 MiB | 0 |
| TCP echo, 1 KiB payload | 26,880 (16,805-33,894) | - | - | 0.6 ms | 21-410 ms | 7.1 s | 10-11 MiB | 0 |

The one-request-per-connection row is a single run: that workload opens thousands of
connections a second, and repeated runs exhausted the host's 16,384 dynamic TCP ports
(TIME_WAIT sockets), after which the client's own connects failed. Its 3.0 s maximum is one
connect retried by the host. The Linux client and server share the WSL2 guest's CPUs and
the figures are not comparable with Windows': both are baselines for later change.

An idle server (no connections) used about 0.2% of a core over 10 s (20 ms of CPU, Linux).

Per-request cost, Linux, steady state (20,000 requests on one keep-alive connection after 500
warm-up requests; counted by compiling the generated runtime with the allocator and libuv
entry points renamed to counting shims, so libuv's own allocations are included):

| Workload | Allocations | Loop-thread submissions | `uv_write` | `uv_read_start` / `stop` | `uv_timer_start` |
| --- | --- | --- | --- | --- | --- |
| keep-alive, fixed route | 0.00 | 2.00 | 1.00 | 1.00 / 1.00 | 2.00 |
| keep-alive, dynamic route | 1.00 (53.5 B, the handler's own `String.interpolate`) | 2.00 | 1.00 | 1.00 / 1.00 | 2.00 |
| pipelined, 16 per write, fixed | 0.00 | 1.06 | 1.00 | 0.06 / 0.06 | 1.06 |
| one request per connection, fixed | 11.00 (156,274 B, all freed) | 6.00 | 1.00 | 1.00 / 1.00 | 2.00 |

A loop-thread submission is one `uv_async_send` by `hex_event_submit`. The two per keep-alive
request are the read and the write; each response is one `uv_write` (no gathering), so
pipelining shares only the reads. The zero is a measurement of this route, not a claim about
other handlers or other configurations. Copies, from reading `server.c` rather than from a
counter: the parser packs the request head once into head storage; a decoded body is copied
once into the caller's list; a response's header lines are copied into the output buffer's head
region and moved once more when the final head is assembled right-aligned, and the body is
copied once into the body region; the output buffer is then written in one call.

Parked connections (one request each, then idle; resident growth over the empty server):

| Host | 1,000 | 10,000 | Threads |
| --- | --- | --- | --- |
| Linux (resident set) | +176-180 MiB (180-184 KiB each) | +1,707 MiB (175 KiB each) | 13, unchanged |
| Windows (working set) | +36 MiB (37 KiB each) | +352 MiB (36 KiB each) | 19, unchanged |

The two hosts report different counters (resident set, working set) and the figures were not
reconciled; the configured storage per connection is about 150 KiB (RFC 0194). The server was
configured for 20,000 connections for this row (the default ceiling is 4,096). 100,000
connections were not attempted.

Slow clients, 1,000 connections holding half a request head beside 64 fast connections: Linux
7,828 req/s (p99 17.3 ms) against 8,319 (13.7 ms) alone; Windows 21,569 req/s (p99 5.1 ms)
against 31,259 (3.4 ms) alone. Every slow connection was answered `408` and closed by the
header deadline; resident memory did not shrink afterward (Linux 193 to 201 MiB, Windows 39 to
63 MiB) within the observation.

Not measured: the runtime microbenchmarks (Task spawn and join, context switch, park and wake
latency, queue throughput), worker scaling, and any comparison with other servers; none is
claimed. Cost per request on this baseline is 260 to 270 microseconds of server CPU on Linux
(WSL2) at one connection (97% of a core at 3,671 req/s) and at 64 (10.2 s for about 37,900
requests); the follow-ups the Follow-up ownership section names
(batching, vectored writes, scheduler changes) remain conditional on a measurement that
shows a bottleneck, and this record does not select one.

## Reference synchronization

This architecture RFC defines no public source syntax. The public contracts of its
follow-ups are in `docs/reference.md` (`std/net`, `std/http`).
