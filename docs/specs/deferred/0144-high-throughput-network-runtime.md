# RFC 0144: High-Throughput Network Runtime

- Kind: Architecture Decision Record (ADR)
- Status: Open Discussion; not scheduled. Design state: Draft; architecture goals recorded, implementation not started
- Created: 2026-09-07
- Scope: runtime foundations required for high-throughput TCP and HTTP servers
- Depends on: RFC 0132 (root scheduler bootstrap)
- Coordinates with: RFC 0039 (C interoperability), RFC 0052 (C compiler
  backend), RFC 0055 (filesystem/build driver), RFC 0118 (concurrency safety),
  RFC 0145 (libuv async runtime backend), and the current Task, IO, Stash,
  Pool, String, List, and View contracts in `docs/reference.md`
- Does not define: final socket syntax, an HTTP API, HTTP parsing, routing,
  middleware, TLS, HTTP/2, HTTP/3, or a benchmark-derived performance promise

## Goal

Hexal must be capable of hosting a production HTTP server with throughput,
latency, CPU cost, and memory use competitive with mature Axum/Hyper/Tokio and
May-based servers under the same workload, hardware, operating system,
toolchain optimization, and worker count.

Architecture alone does not establish that result. Every optimization in this
RFC requires measurement against an unchanged functional baseline. Published
benchmarks from another machine or protocol are directional evidence only.

## Conclusion

The current stackful-fiber design is a viable foundation. The Task
park/commit/wake protocol, M:N workers, synchronous-looking calls, Channels,
Mutexes, joins, guarded stacks, and separate blocking-operation path are useful
building blocks.

The current runtime is not yet suitable for high-concurrency networking. The
missing primary facility is a scheduler-integrated non-blocking network driver.
Using the demand-grown blocking pool for steady-state socket waits would turn
idle connections into native threads and defeat M:N scheduling.

Three capabilities are mandatory before claiming competitive server support:

1. scheduler-integrated non-blocking sockets;
2. timers, deadlines, cancellation, and graceful shutdown;
3. measured removal of scheduler and allocation bottlenecks, especially the
   single global ready queue and per-Task stack/context allocation.

## Existing foundation

- `Task<R>` is a stackful coroutine scheduled cooperatively over native CPU
  workers.
- A Task may park without blocking its scheduler worker.
- Park, wake, and resume have an exactly-once publication protocol with
  release/acquire payload visibility.
- Channel, Mutex, join, and scheduler-aware blocking calls share that protocol.
- The current blocking pool preserves scheduler progress for synchronous
  operations; RFC 0145 replaces its implementation with libuv's global worker
  pool before the network runtime lands.
- Task stacks have explicit reserve/commit settings and overflow guards.
- RFC 0132 keeps the initial-process root fiber on worker zero.
- Views and byte collections permit parsers to work without immediately
  converting every input field into an owned String.
- Stash and Pool provide future request-lifetime allocation strategies.

These properties should be retained unless a benchmark and replacement design
show a material benefit.

## Required network architecture

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

RFC 0145 selects one program-wide libuv loop on one dedicated native thread for
the first implementation. The public socket contract does not expose this
choice. Preserve the neutral event-driver boundary so measurement may justify
later sharding without changing Hexal syntax.

## Timers, cancellation, and shutdown

A production server requires:

- connection and request-header deadlines;
- keep-alive and write deadlines;
- timer cancellation without a stale wake;
- cancellation of an outstanding socket wait;
- listener and connection shutdown;
- graceful server shutdown with a bounded deadline;
- one winner when readiness, timeout, cancellation, and close race.

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

The first HTTP implementation keeps explicit fairness at request or bounded
batch boundaries. The library may process several immediately available
requests before yielding, but an unbounded always-ready connection must not
monopolize one worker.

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

## HTTP data path

The runtime does not require every request component to become an owned String.
The HTTP library should prefer:

- reusable connection read/write buffers;
- byte-oriented `View<UInt8>` slices for request lines, headers, and bodies;
- validation or String allocation only when requested by application code;
- request-lifetime Stash allocation where ownership is clear;
- bounded buffers and explicit backpressure;
- vectored writes and platform send-file facilities when representation and
  ownership permit them;
- incremental parsing across partial reads;
- no allocation for would-block or ordinary readiness transitions.

HTTP parsing, routing, middleware, response construction, compression, TLS,
and protocol versions remain library concerns. They receive separate specs
after the network runtime is operational.

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
2. Execute Task, Channel, Mutex, and blocking-pool runtime fixtures.
3. Establish spawn, switch, yield, park/wake, worker-scaling, and memory
   baselines before changing scheduling architecture.

### Stage 1: networking contract

1. Write a focused socket API and ownership specification.
2. Define target-neutral listener, connection, address, shutdown, and Error
   contracts.
3. Define one active wait record and exact close/read/write race ownership.
4. Consume RFC 0145's single-loop libuv backend and neutral event adapter.
5. Keep all filesystem access and native linking in the driver/backend layers,
   not the in-memory compiler.

### Stage 2: network driver

1. Implement non-blocking accept/connect/read/write through libuv for one host
   target.
2. Integrate registration and wakeup with the common Task protocol.
3. Prove socket waits consume no libuv worker-pool thread.
4. Qualify the same libuv adapter and target-neutral runtime contract on
   Windows, Linux, and macOS.
5. Run echo, idle-connection, wake-race, and close-race tests.

### Stage 3: timers and lifecycle

1. Add monotonic timers and deadline cancellation.
2. Resolve readiness/timeout/cancel/close races with one terminal owner.
3. Add graceful listener and connection shutdown.
4. Test stale-event rejection and wait-record lifetime under repeated races.

### Stage 4: minimal HTTP/1.1 library

1. Implement incremental byte parsing and bounded reusable buffers.
2. Support keep-alive, bounded request bodies, response framing, and
   backpressure.
3. Keep root off accept and connection hot paths.
4. Establish the first end-to-end comparison baseline.

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

## Required follow-up specifications

This RFC is an umbrella and is not implemented as one change. Before code work,
create focused specs for:

1. socket language API, ownership, and C interoperability;
2. libuv network-driver adapters building on RFC 0145;
3. timers, deadlines, cancellation, and shutdown;
4. minimal HTTP/1.1 parsing and server lifecycle;
5. scheduler local queues/work stealing, only if measured;
6. Task/control-block/stack reuse, only if measured;
7. POSIX context replacement, only if qualification or measurement requires it.

Each follow-up owns its exhaustive Validation section and reference
synchronization. None may silently broaden this RFC into a general async syntax
or futures system.

## Open decisions

1. Socket handle ownership, aliasing, close, and half-close semantics.
2. Deadline-bearing operations versus a general wait/select surface.
3. Initial explicit-yield batching policy for server loops.
4. Whether POSIX Tasks may migrate with the existing context backend.
5. Concrete benchmark workloads and thresholds that qualify “competitive.”

## Implementation readiness

This umbrella RFC is not implementation-ready. It records the target
architecture, constraints, risks, staging, and measurement discipline. Stage 1
begins only after RFC 0132 is implemented and its runtime baselines exist. Each
required subsystem receives a focused implementation-ready specification before
code changes.

## Reference synchronization

Do not update `docs/reference.md` from this draft. Each implemented follow-up
updates only its stabilized socket, scheduling, timer, or server contract after
explicit approval.
