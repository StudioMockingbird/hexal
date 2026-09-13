# RFC 0168: libuv-Backed Runtime and IO Semantics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: deeply integrate the vendored libuv capabilities into Hexal's Task,
  IO, networking, timing, process, and portable OS semantics
- Depends on: ADR 0145 (qualified vendored libuv), ADR 0146 (mimalloc), and
  the reference's current Task, IO, Error, EoS, and cleanup contracts
- Coordinates with: RFC 0118 (future concurrency safety) and RFC 0144
  (high-throughput network goals), plus child RFCs 0169 through 0178
- Would supersede: the compiler-generated blocking pool and the direct native
  portability wrappers that libuv can replace without changing semantics
- Does not authorize: implementation or `docs/reference.md` changes before
  the open semantic decisions and focused public API specifications close

## Summary

Use libuv as the sole backend for every Hexal runtime or library capability
that libuv implements with matching semantics. Keep libuv invisible in Hexal
source.

Hexal code remains direct and synchronous-looking:

```hexal
bytes := try input.read(buffer, 4096)
try output.write(buffer.slice(0, bytes))
```

Inside a Task, a native operation submits asynchronous work, parks only the
current fiber, and resumes it with an ordinary Hexal result. Outside a Task,
the synchronous form of the same libuv operation executes without starting an
event loop.

The proposal adds no `async`, `await`, Future, Promise, callback syntax, event
loop object, worker-pool object, or `uv_*` type. The compiler and runtime absorb
that machinery. Public socket, timer, DNS, process, watcher, and related APIs
remain ordinary typed Hexal library surfaces. Each requires a focused child
specification before exposure.

## Libuv arc

RFC 0168 owns the shared architecture and maximal-adoption rule. The child
specifications own implementable units and public semantics:

| RFC | Responsibility |
| --- | --- |
| 0169 | Runtime bridge, event loop, Task wakeup, native workers, synchronization, TLS, and metrics |
| 0170 | Existing IO plus filesystem operations, paths, directories, and metadata |
| 0171 | Monotonic and wall time, timers, sleep, deadlines, and timeouts |
| 0172 | Addresses, TCP, UDP, DNS, socket ownership, and backpressure |
| 0173 | Child processes, pipes, standard streams, exit, and process IPC |
| 0174 | TTY classification, terminal modes, dimensions, and terminal IO |
| 0175 | File and directory change observation |
| 0176 | Ordinary process signals and Task delivery |
| 0177 | Dynamic-library loading and C-symbol lookup |
| 0178 | Secure random bytes and portable OS/system information |

No child may introduce a second native backend for a capability assigned to
libuv here. A child moves from deferred to active only after its open decisions
are settled and its Validation section becomes exhaustive.

## Goals

- Preserve Hexal's M:N fibers and normal call syntax.
- Prevent blocking IO from occupying scheduler workers.
- Reuse the complete applicable libuv surface: event delivery, blocking work,
  threads, synchronization, TLS keys, networking, DNS, timers, pipes, TTY,
  filesystem operations and observation, processes, signals, polling, random
  generation, dynamic libraries, and system information.
- Remove duplicate reactors, blocking pools, timer wheels, DNS executors, and
  platform wrappers from Hexal.
- Keep callbacks and native request state out of the language surface.
- Preserve existing IO, Error, EoS, evaluation-order, and cleanup contracts.
- Scale toward high-throughput servers without async language ceremony.
- Require every future overlapping Hexal API to use libuv rather than adding a
  second native backend.
- Expose cohesive Hexal concepts rather than mirroring every libuv function.

## Non-goals

- Replacing stackful Tasks with libuv callbacks.
- Exposing raw libuv handles, requests, loops, callbacks, or negative error
  codes.
- Treating libuv as an HTTP implementation.
- Promising that executing native work can always be cancelled.
- Adding a second reactor or worker pool beside libuv.
- Sharding loops before one-loop measurements demonstrate a bottleneck.
- Making every libuv utility a builtin.
- Using libuv in the Go build driver.
- Settling every future networking, process, filesystem, or OS API here.
- Replacing an exact C23 language facility when libuv offers no equivalent
  semantic operation. In particular, libuv does not replace C23 atomics.

## Maximal-adoption rule

For every current or future Hexal capability:

1. Inventory the complete pinned libuv API family before designing its
   backend.
2. If libuv implements the required operation and contract, use it as the only
   backend on every target qualified by ADR 0145.
3. Remove the direct Windows, POSIX, C-runtime, or compiler-owned alternative
   after equivalence is proven.
4. If libuv supplies only a lower-level primitive, keep only the smallest
   Hexal adapter needed to implement the higher-level language contract.
5. Retain a native or C23 path only when libuv has no matching operation or
   cannot implement the contract safely. Record the mismatch and test the
   retained boundary.

“Use libuv fully” means one native implementation per overlapping capability.
It does not mean exposing libuv names or replacing Hexal semantics with its C
API shape.

Examples:

- Hexal Task Mutex remains fiber-aware because `uv_mutex_t` blocks a native
  thread, but its internal native guards use `uv_mutex_t`.
- Hexal `Atomic<T>` remains a C23 atomic because libuv has no atomic-value API.
- Ordinary process signals use `uv_signal_t`; stack-overflow and synchronous
  fault handling remain native because libuv does not safely implement them.
- File operations use `uv_fs_*` in both synchronous and Task-aware modes;
  Hexal does not retain a parallel direct-OS filesystem implementation.

## Core semantic model

### 1. Ordinary calls with implicit suspension

A Task-aware native operation has one source-level call and result contract.
It may suspend internally, but suspension does not appear in its return type.

When called from a running Task:

1. Construct one typed native request.
2. Publish it to the libuv loop or worker pool.
3. Park the current Task through the existing park/commit/wake protocol.
4. Let no libuv callback execute Hexal user code.
5. Store the complete typed result before the release wake.
6. Resume the Task exactly once.
7. Return the existing Hexal value, `EoS`, or `Error`.

Outside a Task, the synchronous form of the same libuv family executes and
does not initialize an event loop merely for uniformity. A direct native path
survives only when libuv has no matching operation.

The language does not distinguish synchronous and asynchronous forms of the
same operation. Execution context determines whether the runtime parks a
fiber.

### 2. Runtime ownership

Libuv owns:

- platform readiness and completion engines;
- the event loop and its native thread;
- its native worker pool;
- native request and handle state below the Hexal adapter;
- native threads, mutexes, read/write locks, semaphores, conditions, barriers,
  once initialization, TLS keys, thread naming and affinity, and portable
  processor-count discovery where used by Hexal's runtime; and
- TCP, UDP, pipes, TTY, timers, DNS, filesystem requests, processes, signals,
  polling, random generation, dynamic-library loading, filesystem watching,
  and portable OS queries selected by later surfaces.

Hexal owns:

- fibers and context switching;
- Task creation, scheduling, parking, waking, joining, and detaching;
- Task-aware Channel and Mutex queues;
- source evaluation order;
- typed request context and result translation;
- ownership and lifetime of Hexal-visible buffers;
- Error and EoS mapping;
- cancellation and shutdown semantics;
- demand-driven dependency and component selection; and
- every public library API.

### 3. Initial loop topology

The initial implementation uses one program-wide `uv_loop_t` on one dedicated
native thread. Scheduler workers never call `uv_run`; loop callbacks never run
user code.

Workers submit fully initialized commands to one mutex-protected FIFO and wake
the loop through one `uv_async_t`. Only the loop owner mutates ordinary libuv
handles unless the pinned API explicitly permits another thread.

One loop is an implementation baseline, not a permanent scalability promise.
RFC 0144's connection, throughput, latency, and loop-idle measurements decide
whether a later design shards loops.

### 4. Request lifetime

- A request remains live through final completion or cancellation callback.
- Its Task cannot be reclaimed while the request can callback.
- A Task observes result storage only after the acquire side of its wake.
- Submission failure cannot leave a Task parked.
- Immediate completion cannot resume a still-running fiber or publish twice.
- Completion, cancellation, close, and shutdown choose exactly one terminal
  owner for request and buffer cleanup.
- Every completed `uv_fs_t` receives exactly one `uv_fs_req_cleanup` after its
  result and owned data have been consumed.

The request-storage strategy is deliberately open. A later revision must
choose fiber-stack records retained while parked, Task-owned request storage,
or mimalloc-backed request objects and prove the corresponding destruction
rules.

### 5. Buffer access

An outstanding native operation keeps its submitted memory valid until final
callback completion. The initiating Task cannot continue past the call until
completion, so ordinary same-Task code cannot reuse the buffer concurrently.

Aliases used by another parallel Task remain subject to Hexal's existing
unsynchronized-conflict rules. This RFC introduces no borrow checking and does
not pretend libuv can make an aliased mutable buffer safe.

## Capability adoption

| Libuv capability | Hexal today | This proposal picks up | Public surface |
| --- | --- | --- | --- |
| Event loop and IOCP/epoll/kqueue | absent | one runtime-owned loop and portable backend | invisible |
| Prepare, check, and idle loop phases | absent | internal runtime coordination only where required | invisible |
| `uv_async_t` | absent | scheduler-to-loop wakeup | invisible |
| `uv_queue_work` and worker pool | custom Hexal blocking pool | replace it completely | invisible |
| Native threads | direct OS wrappers | replace matching runtime wrappers | invisible |
| Mutex, read/write lock, semaphore, condition, barrier, and once | direct OS wrappers or absent | sole native synchronization backend where needed | Task Mutex remains fiber-aware |
| TLS keys | C23 `_Thread_local` for runtime state | replace runtime TLS storage with `uv_key_t` | invisible |
| Thread naming and affinity | direct or absent | use libuv when Hexal needs either capability | invisible |
| Available parallelism | custom OS query | scheduler-worker default | invisible |
| Atomic values | direct C23 atomics | retain because libuv has no matching atomic API | existing `Atomic<T>` semantics |
| Fibers and guarded stacks | custom runtime | retain; libuv has no fiber contexts | existing Task semantics |
| File open, read, write, sync, truncate, stat, chmod, links, directories, copy, sendfile, and close | partial synchronous native core | use the corresponding `uv_fs_*` operation exclusively | existing IO plus focused filesystem RFC |
| Seek | synchronous native core | smallest native core through `uv_queue_work` | existing IO API |
| Descriptor print | shares IO core | share the libuv-backed write route | existing `print` |
| TCP | absent | typed listen, accept, connect, read, write, and close adapter | focused networking RFC |
| UDP | absent | typed bind, send, and receive adapter | focused networking RFC |
| DNS | absent | asynchronous resolver adapter | focused networking RFC |
| Timers and monotonic clock | absent | Task parking, deadlines, runtime metrics | focused time RFC |
| Imported-descriptor polling | absent | `uv_poll_t` only when no typed libuv handle owns it | C-interop RFC |
| Pipes and local IPC | absent | portable backend | focused IPC RFC |
| TTY | basic standard handles | classify and route matching handles | focused terminal RFC |
| Filesystem paths and metadata | absent | portable backend | focused filesystem RFC |
| Filesystem watch and poll | absent | portable backend | focused watcher RFC |
| Child process and process IPC | absent | portable backend | focused process RFC |
| Signals | stack overflow only | retain overflow path; use libuv for ordinary signals later | focused process RFC |
| Random bytes | absent | portable backend | focused random RFC |
| Dynamic-library loading | absent | portable backend | C-interop RFC |
| Environment, working-directory, executable-path, home-directory, temporary-directory, hostname, user, process, CPU, memory, uptime, load, interface, time-of-day, and resource queries | minimal | use libuv for every exposed matching query | focused OS RFC |
| Loop metrics and handle diagnostics | absent | benchmark and debug instrumentation | no stable language API |
| Allocator replacement | native Heap backend | install ADR 0146 mimalloc before all libuv calls | invisible |

Every row marked for a focused RFC is a commitment about backend ownership,
not a commitment to add that public feature. If Hexal later adds the feature,
that RFC must use the named libuv family or demonstrate that it cannot satisfy
the required semantics.

## Scheduler and worker-pool integration

- Use qualified `uv_thread_*` operations for non-root runtime threads where
  their lifecycle matches the current scheduler contract.
- Use `uv_mutex_t`, `uv_cond_t`, and `uv_once_t` for runtime-internal native
  synchronization where they replace an equivalent platform split.
- Keep Task-aware Channel and Mutex wait queues above those native guards.
- Use `uv_available_parallelism()` for the default scheduler worker count.
- Retain `_Atomic`, fiber context switching, guarded virtual stacks, alternate
  signal stacks, and stack-overflow handling where libuv has no matching safe
  facility. Replace runtime `_Thread_local` state with `uv_key_t`; measure the
  cost but do not retain a parallel TLS backend.
- Delete Hexal's general blocking queue, worker threads, growth and retirement
  policy, counters, and traps only after the libuv route passes runtime
  conformance.
- Prefer a specialized libuv request over `uv_queue_work`; the worker pool is
  a fallback for genuinely blocking operations, not a universal adapter.
- Do not retain a private fallback pool for safety.

## Existing IO integration

- Use asynchronous `uv_fs_read`, `uv_fs_write`, and `uv_fs_close` inside a
  Task. Use their synchronous callback-null forms outside a Task. Do not keep a
  separate direct-OS path for the same file operation.
- Use offset `-1` for operations that consume and update the native shared
  cursor.
- Probe ordinary files, standard handles, pipes, terminals, and sockets
  separately; do not assume one handle class represents all of them.
- Keep the smallest existing native operation behind `uv_queue_work` when no
  specialized request matches exactly.
- `IO.seek` uses that fallback because libuv has no general asynchronous seek
  request.
- Task-aware descriptor print uses the same write path as `IO.write`.
- A non-Task call uses the synchronous form of the same libuv operation.
- Preserve current single-call transfer limits, partial-transfer behavior,
  EoS rules, cursor sharing, Error shape, and evaluation order.
- Translate failures into the owning Hexal operation's Error contract; do not
  expose negative libuv error codes as a new language type.

## Networking and timing direction

Network and timer operations use normal calls that park Tasks. They introduce
neither callbacks nor futures into user code.

Conceptual only; exact names and ownership require child RFCs:

```hexal
listener := try tcp_listen(address, port)
connection := try listener.accept()
count := try connection.read(buffer)
try connection.write(buffer.slice(0, count))
try task_sleep(duration)
```

Required direction:

- Socket readiness never occupies a worker-pool thread.
- Accept, connect, read, write, and close have one terminal completion.
- Backpressure parks the calling Task rather than growing an unbounded queue.
- A listener or connection has explicit close and external-state tracking
  consistent with current IO handles.
- Timer waiting parks the Task; it never calls `uv_sleep` on a scheduler
  worker.
- DNS may use libuv's shared pool; socket readiness does not.
- Libuv remains a transport and runtime layer. HTTP parsing, routing, bodies,
  response composition, and server policy belong to an HTTP library.

## Demand and generated components

Add one neutral generated runtime pair when event integration is selected:

```text
hexal/event.h
hexal/event.c
```

`event.h` exposes only private Hexal runtime contracts. `event.c` owns libuv
loop, command, request, and callback state. No generated module header exposes
`<uv.h>` or a `uv_*` name.

Program-wide demand rules:

- Task, Channel, or Mutex selects `RuntimeLibuv` because their native threads,
  guards, initialization, TLS, and processor discovery use libuv.
- A Task combined with reachable native IO, timer, DNS, socket, polling, or
  blocking foreign work additionally selects the event component.
- Selecting libuv also selects ADR 0146's mimalloc dependency and bootstrap
  allocator installation.
- Atomic alone selects neither dependency.
- IO or print without Task selects `RuntimeLibuv` but may use synchronous
  libuv calls without initializing the event loop.
- A program selecting neither concurrency nor an event facility links neither
  libuv nor its adapter.

The compiler selects conservatively from reachable program features. It does
not attempt to prove which dynamic caller reaches an operation.

## Required sweep

After replacement is proven, remove:

- every `hex_blocking_*` declaration, queue, job, worker, counter, growth,
  retirement, initialization, trap, test, and comment;
- scheduler-only `_beginthreadex` and pthread wrappers replaced by libuv;
- scheduler-only native mutex and condition implementations replaced by
  libuv;
- the custom processor-count query;
- redundant once-initialization code; and
- native descriptor and filesystem paths superseded by libuv operations;
- runtime-native thread, TLS, synchronization, naming, affinity, and processor
  discovery wrappers whose operations libuv supplies; and
- every future platform split added for a capability already owned by libuv.

Retain and document the reason for:

- fiber context switching and guarded stacks;
- stack-overflow recovery;
- C23 atomics;
- Task-aware Channel and Mutex queues;
- native operation cores required behind `uv_queue_work` only where libuv has
  no specialized operation.

## Detailed implementation outline

This outline is not executable until the open decisions and child API RFCs
close.

### Phase 0: complete overlap inventory

1. Inventory every compiler-owned runtime component and every public builtin
   or library operation that touches threads, synchronization, time, IO,
   networking, processes, signals, terminals, filesystems, random generation,
   dynamic libraries, or OS information.
2. Map each operation to the exact pinned libuv API or record why no matching
   libuv operation exists.
3. Treat an unmapped overlapping operation as a blocker, not permission to
   keep the old backend.
4. Add the mapping and deletion owner to the implementing child RFC.
5. Reject any design that leaves two native backends for the same operation.

### Phase 1: runtime-native substrate

Owned by RFC 0169.

1. Consume ADR 0145's `RuntimeLibuv` dependency and ADR 0146's allocator.
2. Install mimalloc callbacks before every other libuv call.
3. Replace scheduler thread, native guard, read/write lock, semaphore,
   condition, barrier, once, TLS, thread metadata, affinity, and
   processor-count facilities wherever the pinned libuv release supplies the
   operation.
4. Preserve fibers, guarded stacks, atomics, Channel, and Task Mutex semantics.
5. Prove scheduler conformance before touching IO.

### Phase 2: event bridge

Owned by RFC 0169.

1. Add `hexal/event.h` and `hexal/event.c`.
2. Start one dedicated loop thread.
3. Add the command FIFO and `uv_async_t` wakeup.
4. Complete typed requests through the existing Task wake protocol.
5. Settle request storage, cancellation, and shutdown ownership before enabling
   any source-reachable operation.

### Phase 3: replace blocking work

Owned by RFC 0169.

1. Route genuine blocking operations through `uv_queue_work`.
2. Use synchronous libuv forms outside Tasks; preserve a native work function
   only when no libuv operation exists.
3. Run saturation, immediate-completion, failure, and shutdown fixtures.
4. Remove the custom pool only after every replacement passes.

### Phase 4: migrate existing IO and print

Owned by RFC 0170.

1. Probe every Windows handle class against the applicable libuv stream,
   filesystem, pipe, TTY, or polling abstraction.
2. Route every match through that abstraction.
3. Keep a mismatch as a minimal `uv_queue_work` callback only when the
   qualification record proves that libuv has no matching operation.
4. Preserve every current IO and print contract.
5. Remove only native code proven redundant.

### Phase 5: add focused capability surfaces

1. Settle time, sleep, deadlines, and timeouts in RFC 0171.
2. Settle TCP, UDP, DNS, ownership, and backpressure in RFC 0172.
3. Settle processes, pipes, and process IPC in RFC 0173.
4. Settle terminal behavior in RFC 0174.
5. Settle filesystem watching in RFC 0175.
6. Settle ordinary signals in RFC 0176.
7. Settle dynamic-library loading in RFC 0177.
8. Settle random and OS information in RFC 0178.

### Phase 6: conformance and performance

1. Exercise immediate and delayed completion, submission failure, saturation,
   cancellation races, shutdown, and buffer lifetime in generated C.
2. Exercise more native work than scheduler workers while unrelated Tasks
   continue making progress.
3. Exercise 1,000 and 10,000 idle connections where the host permits.
4. Record throughput, tail latency, loop idle time, worker queue delay, native
   thread count, memory, and request allocations.
5. Consider loop sharding only if measurements isolate the loop as the limit.
6. Update `docs/reference.md` once after behavior stabilizes and only with
   explicit user approval.

## Open design questions

1. RFC 0169 owns cancellation, request storage, root shutdown, worker-pool
   sizing, and loop lifetime.
2. RFC 0170 owns Windows handle equivalence and filesystem representations.
3. RFC 0171 owns public duration, clock, timer, deadline, and timeout types.
4. RFC 0172 owns addresses, TCP, UDP, DNS, buffering, and backpressure.
5. RFCs 0173 through 0178 own their respective public surface decisions.
6. RFC 0144 owns the measured threshold for any later loop sharding.

## Validation direction

The final revision's exhaustive Validation section must cover:

- a mechanically checked inventory mapping every overlapping current runtime
  and language operation to libuv;
- no retained Windows, POSIX, C-runtime, or compiler-owned alternate backend
  for an operation implemented by libuv;
- no user callback running on a libuv thread;
- exactly-once Task wakeup under completion, cancellation, and shutdown races;
- result publication before wake;
- safe buffer and request lifetime;
- no scheduler-worker blocking for socket readiness;
- continued progress under worker-pool saturation;
- exact Error and EoS mapping;
- existing IO and print behavior through synchronous and asynchronous libuv
  forms;
- demand-driven component and dependency selection;
- no libuv names in public generated module headers;
- removal of the superseded custom pool, TLS storage, and platform wrappers;
- generated-C compilation and execution through ADR 0125's external harness;
- high-connection-count throughput and bounded native threads; and
- exact reference synchronization after implementation approval.

## Reference synchronization

Do not edit `docs/reference.md` from this proposal. Its eventual implementation
must update only observable Hexal contracts, not libuv deployment details,
after semantics stabilize and with explicit user approval.
