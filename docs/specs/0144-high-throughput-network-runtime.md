# RFC 0144: High-Throughput Network Runtime

- Kind: Architecture Decision Record (ADR)
- Status: Open Discussion; owns the Task-aware socket and timer runtime contract;
  implementation not started
- Created: 2026-09-07
- Updated: 2026-09-29
- Scope: Task-aware socket and timer operations built on the libuv foundation
- Depends on: RFC 0132 (root scheduler bootstrap) and RFC 0145 (libuv async
  runtime backend)
- Coordinates with: RFC 0039 (C interoperability), RFC 0052 (C compiler
  backend), RFC 0055 (filesystem/build driver), RFC 0118 (concurrency safety),
  RFC 0145 (libuv async runtime backend), and the current Task, IO, Stash,
  Pool, String, List, and View contracts in `docs/reference.md`
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
- Views and byte collections permit parsers to work without immediately
  converting every input field into an owned String.
- Stash and Pool provide future request-lifetime allocation strategies.

These properties should be retained unless a benchmark and replacement design
show a material benefit.

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

1. attempts the native operation immediately;
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
| Owned String per header | allocation and copying | byte Views and request-lifetime storage |
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

## Staged implementation plan

### Stage 0: scheduler correctness

1. Implement RFC 0132.
2. Execute Task, Channel, Mutex, and libuv-backed blocking-operation fixtures.
3. Establish spawn, switch, yield, park/wake, worker-scaling, and memory
   baselines before changing scheduling architecture.

### Stage 1: Task-aware network contract

1. Define target-neutral socket operations, ownership, and Error results.
2. Define one active wait record per blocked operation and exact close/read/
   write/timeout/cancel race ownership.
3. Define which operations are Task-aware and which blocking operations use
   RFC 0145's worker-pool path.
4. Keep filesystem access and native linking in the driver/backend layers,
   not the in-memory compiler.

### Stage 2: libuv-backed Task adapter

1. Implement the contract using RFC 0145's libuv loop for non-blocking
   accept/connect/read/write.
2. Integrate registration and wakeup with the common Task protocol.
3. Prove socket waits consume no libuv worker-pool thread.
4. Qualify the Task adapter on each supported target before the HTTP backend
   claims that target.
5. Run echo, idle-connection, wake-race, and close-race tests.

### Stage 3: timers and lifecycle

1. Add monotonic timers and deadline cancellation.
2. Resolve readiness/timeout/cancel/close races with one terminal owner.
3. Define operation-close behavior so native requests and buffers remain live
   until the backend can no longer access them.
4. Test stale-event rejection and wait-record lifetime under repeated races.

### Stage 4: first HTTP/1 server

RFCs 0198, 0210, 0208, and 0194 own the parser, public surface, backend
contract, and default HTTP behavior. Their design work may proceed in parallel,
but settle the public and adapter contracts before implementing the default
backend. After Stages 1-3 establish and qualify the Task-aware runtime, execute
in dependency order: pin and integrate the parser; implement the public
request/response and router surface; implement the backend adapter; then
implement the default server in RFC 0194 over RFC 0144 operations. Qualify the
combined path for bounded request bodies, keep-alive, response framing,
backpressure, and shutdown. Keep root off accept and connection hot paths.
Record a baseline; do not make a cross-language performance comparison a
completion criterion.

### Stage 5: measured runtime optimization

1. Profile queue contention, fiber switching, allocation, stacks, parsing, and
   copies separately.
2. Introduce local queues/work stealing only if the global FIFO is material.
3. Introduce Task/stack caches only if creation or churn is material.
4. Replace `ucontext` only if qualification fails or switch cost is material.
5. Add buffer, vectored-I/O, and send-file optimizations only when the data path
   remains material.
6. Repeat all correctness and end-to-end measurements after each independent
   change; retain only demonstrated wins.

## Follow-up ownership

RFC 0145 owns the libuv event-loop foundation; this RFC owns the Task-aware
socket/timer contract and adapter; RFC 0194 owns HTTP server behavior and
consumes that adapter; RFCs 0198, 0210, and 0208 own parsing, public semantics,
and backend pluggability. No separate generic network-runtime specification is
needed. Scheduler local queues/work stealing, Task/stack reuse, and POSIX
context replacement remain conditional follow-ups: specify and implement them
only if qualification fails or measurement shows a material bottleneck.

This RFC does not authorize general async syntax or a futures system.

## Open decisions

1. Socket handle ownership, aliasing, close, and half-close semantics.
2. Deadline-bearing operations versus a general wait/select surface.
3. Which timer/deadline primitives belong in the private runtime boundary;
   protocol-specific deadlines and defaults remain with the owning feature.
4. Whether POSIX Tasks may migrate with the existing context backend.
5. A fixed workload for recording comparable runtime and server baselines; no
   cross-language competitiveness threshold gates the first server release.

## Implementation readiness

This umbrella RFC is not implementation-ready. It records the target
architecture, constraints, risks, staging, and measurement discipline. The
first HTTP server milestone is gated by this RFC's Task-aware runtime contract
and adapter, plus the contracts in RFCs 0198, 0208, 0210, and 0194. RFC 0145 is
the required libuv foundation. Scheduler optimizations are not prerequisites
unless measurement or target qualification establishes a need.

## Reference synchronization

This architecture RFC defines no public source syntax. Implemented follow-ups
update `docs/reference.md` for their stabilized public contracts before closure.
