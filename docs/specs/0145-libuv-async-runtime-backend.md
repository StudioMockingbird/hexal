# RFC 0145: libuv Async Runtime Backend

- Kind: Architecture Decision Record (ADR)
- Status: Draft; architecture selected, implementation not started
- Created: 2026-09-07
- Scope: use libuv as Hexal's portable event, native-work, networking, time,
  and OS-runtime foundation
- Depends on: RFC 0132 (root scheduler bootstrap)
- Coordinates with: RFC 0052 (C compiler backend), RFC 0055 (filesystem/build
  driver), RFC 0118 (concurrency safety), RFC 0144 (high-throughput network
  runtime), RFC 0146 (mimalloc allocation backend), and the current Task and IO
  contracts in `docs/reference.md`
- Supersedes: the compiler-generated program-wide blocking-thread pool and RFC
  0144's open choice between direct OS reactors and a portable dependency
- Does not replace: Hexal Tasks, fibers, the M:N scheduler, Channels, Mutexes,
  the Task park/commit/wake protocol, or language-level IO semantics

## Decision

Hexal uses a pinned libuv release as its portable asynchronous runtime backend.

Libuv owns:

- the event loop;
- TCP and UDP event delivery;
- timers and cross-thread loop wakeups;
- asynchronous DNS when selected;
- asynchronous filesystem operations when their handle and cursor semantics
  match the Hexal operation;
- the shared native worker pool used for blocking operations; and
- the platform split among epoll, kqueue, and IOCP;
- portable native threads, runtime-internal synchronization, and available-CPU
  discovery where their contracts match; and
- portable OS facilities used by later filesystem, process, pipe, terminal,
  signal, random, dynamic-library, and observability libraries.

Hexal owns:

- fibers and context switching;
- Task creation, scheduling, parking, waking, joining, and detaching;
- the mapping from one language operation to one libuv request;
- request state, result translation, ownership, and buffer lifetime;
- cancellation and shutdown semantics exposed by Hexal; and
- the public IO, socket, timer, and future HTTP APIs.

Hexal does not retain a second general-purpose blocking pool. The current
`hex_blocking_*` queue, workers, growth policy, synchronization, and shutdown
machinery are removed. Scheduler-aware blocking work uses libuv's global worker
pool. A specialized libuv request is preferred whenever it implements the
operation; `uv_queue_work` is the fallback for genuinely blocking work without
such an API.

The first implementation uses one program-wide libuv loop on one dedicated
libuv-created native thread. This is the simplest ownership model. The public
runtime contract and Hexal syntax do not expose loop count or loop ownership.
Loop sharding is a measured follow-up, not part of this RFC.

## Rationale

Implementing epoll, kqueue, IOCP, timers, DNS work dispatch, cancellation
plumbing, and a portable worker pool would reproduce mature infrastructure with
high concurrency and platform risk.

Libuv provides the required platform abstraction in C, supports static builds,
has a stable major-version ABI policy, and permits multiple loops on separate
threads if measurement later requires sharding. Reusing its filesystem,
network, time, DNS, process, IPC, terminal, random, dynamic-loading, metrics,
threading, and synchronization facilities avoids recreating a second portable
OS layer beside the event loop.

Using one dependency for event delivery and blocking work also removes a
duplicate-pool problem. Socket waits use the event loop; operations that really
block use libuv's worker pool. Both complete through one Hexal-to-libuv bridge
and wake Tasks through the same scheduler protocol.

This dependency reduces native backend code. It does not remove the difficult
language-runtime obligations: exactly-once wakeup, request lifetime, cancellation
races, buffer ownership, backpressure, and shutdown remain Hexal contracts.

## Facility adoption policy

Using libuv fully means preferring its qualified portable facility whenever it
implements Hexal's required semantics. It does not mean exposing every libuv
type or callback as a language builtin.

| Libuv family | Hexal state before this RFC | RFC 0145 picks up | Later or excluded |
| --- | --- | --- | --- |
| Event loop and epoll/kqueue/IOCP selection | absent | yes: implement the program-wide loop | later sharding only if measured |
| `uv_async_t` cross-thread wake | no event-loop bridge | yes: implement scheduler-to-loop submission | none |
| Global worker pool and `uv_queue_work` | custom Hexal pool | yes: replace the custom pool; retain as fallback work path | no second pool |
| Native threads | custom POSIX/Windows wrappers | yes: replace runtime-thread wrappers where semantics match | no user-visible native threads |
| Native mutex and condition variable | custom POSIX/Windows wrappers | yes: replace runtime-internal guards | Task-aware locks remain Hexal-owned |
| Once initialization | ad hoc runtime initialization | yes: use for racing process-wide initialization | none |
| Available parallelism | custom OS query | yes: use for scheduler worker-count default | affinity and detailed topology remain future tools |
| C23 atomics and `_Thread_local` | already portable and direct | no: retain current standard facilities | libuv adds no simpler semantic match |
| Task, Channel, and Mutex | scheduler-aware language facilities | no: preserve their semantics and implementation layer | native libuv locks cannot replace Task parking |
| TCP and UDP | absent | yes: implement internal adapters and qualification probes | public socket APIs need focused specs |
| Generic streams and shutdown | absent as network primitives | yes: consume internally for socket adapters | pipe/TTY surfaces need focused specs |
| Polling imported descriptors | absent | yes: implement an internal `uv_poll_t` adapter | public foreign-handle API needs a focused spec |
| Timers and clocks | absent | yes: implement internal timers, deadlines, cancellation support, and metrics clocks | public sleep/time APIs need focused specs |
| Asynchronous DNS | absent | yes: implement internal network-resolution adapters | public resolver API needs a focused spec |
| Filesystem requests | only descriptor IO exists | yes: use where current IO probes prove an exact match | complete path-based file API needs a focused spec |
| Filesystem events and polling | absent | no runtime surface; select libuv as required backend | future Hexal watcher library |
| Unix-domain sockets and named pipes | absent | no runtime surface; select libuv as required backend | future local-IPC library |
| TTY | basic standard-handle IO only | probe for current standard-stream routing | terminal API needs a focused spec |
| Child processes and process IPC | absent | no runtime surface; select libuv as required backend | future process library |
| Signals | only stack-overflow runtime handling | no ordinary signal surface; preserve special overflow path | future process-signal library uses libuv |
| Dynamic-library loading | absent | no runtime surface; select libuv as required backend | future dynamic C interoperability |
| Cryptographic random bytes | absent | qualification probe and required future backend only | future random API |
| Environment, paths, host/user/group, priority, memory, CPU, interface, and load queries | absent | use only parallelism and required qualification/measurement facts | future OS and observability libraries |
| Loop metrics and active-handle diagnostics | absent | yes: implement internally from the first loop | no stable language API |
| UTF-16/WTF-8 conversion | private Windows conversions exist where needed | use when a qualified Windows boundary would otherwise need custom conversion | no general text-conversion surface |
| Libuv allocator replacement | Heap, Stash, and Pool define Hexal allocation | delegated to RFC 0146: install the same mimalloc backend before libuv initialization | does not replace language allocator ownership |

### Selection rule

For each operation, use this order:

1. Use the specialized libuv asynchronous request or handle when it matches the
   required semantics and native-handle representation.
2. Use `uv_poll_t` only for an imported descriptor not already owned by a libuv
   TCP, UDP, pipe, TTY, or stream handle.
3. Use `uv_queue_work` around the smallest blocking native operation when no
   specialized libuv API matches.
4. Retain direct synchronous execution only outside a running Task or where the
   language contract explicitly requires it.
5. Do not add a custom worker, reactor, timer wheel, DNS executor, or platform
   abstraction as a fallback.

### Facilities not exposed automatically

- Prepare, check, and idle callbacks remain runtime implementation tools.
  `uv_idle_t` must not keep the production loop spinning merely to schedule
  Tasks.
- `uv_sleep` must not run on a scheduler worker; Task sleep uses a timer and
  parking.
- Native libuv threads, locks, semaphores, barriers, and thread-local keys are
  not language concurrency primitives.
- Synchronous libuv filesystem and DNS forms must not run from a Task.
- Process, signal, filesystem, pipe, TTY, random, dynamic-library, environment,
  and system-information APIs receive focused library specifications before
  becoming Hexal surface area.
- The Go filesystem/build driver keeps using appropriate Go facilities; linking
  libuv into the driver solely for API uniformity has no value.

## Accepted costs

- Programs selecting the async runtime link a statically built libuv archive.
- Each supported target needs a compatible libuv build; one archive is not
  portable across operating systems, architectures, object formats, or ABIs.
- The initial design adds one dedicated event-loop thread.
- Libuv's worker pool is global across all loops and all facilities that use it.
- The pool size and eager worker initialization follow the pinned libuv release.
  Initially Hexal adds no second sizing mechanism and no private fallback pool.
- The standard `UV_THREADPOOL_SIZE` process environment setting remains the
  advanced override. Hexal does not mutate the process environment to set it.
- A saturated worker pool can delay unrelated filesystem, DNS, or foreign
  blocking work. Network readiness and completion do not consume pool workers.
- Cancelling queued libuv work is best effort. Work already executing in a
  native worker generally cannot be interrupted safely.
- A single event loop can become a throughput bottleneck. Sharding requires
  evidence from the measurement contract in RFC 0144.
- Libuv is not an HTTP implementation. HTTP remains a separate library layer.
- Specialized libuv APIs do not always match an existing native handle or
  language contract. A documented mismatch may retain one small native core
  behind `uv_queue_work`; it does not justify a second pool or event backend.
- Libuv's official support tier is not uniform across Hexal target packs.
  GNU/Linux with glibc, macOS, and Windows are Tier 1; musl is Tier 2 and
  MinGW-w64 is Tier 3. Hexal must qualify its exact Clang/MinGW Windows packs,
  especially AArch64, instead of inheriting a broad "Windows supported" claim.

## Dependency and build contract

### Pinning

- RFC 0052's target pack pins one exact libuv source revision.
- The pinned revision must provide every API selected by this RFC, including
  thread detachment, available-parallelism discovery, loop metrics, async
  wakeup, filesystem requests, timers, DNS, and generic work requests. No
  version-dependent call is emitted without that qualification.
- Hexal does not depend on an arbitrary system-installed libuv by default.
- Every supported target pack builds and qualifies libuv for its exact target
  architecture, OS, object format, C runtime, threading model, and linker.
- The Windows GNU packs are qualified independently rather than inferred from
  libuv's Tier-1 MSVC-oriented Windows coverage; MinGW-w64 is an upstream Tier-3
  target. Both x86-64 and AArch64 must compile, link, and run the required probe
  set before Hexal reports them supported.
- The bundled archive uses static linkage so a normal Hexal executable has no
  runtime dependency on a libuv shared library.
- Required libuv license and attribution files ship with the distribution.
- An external libuv override is permitted only when RFC 0055's driver verifies
  a compatible major version, target ABI, headers, and archive.
- Apply the pinned release's documented consumer flags, including its strict-
  aliasing requirement where applicable, through the target profile rather
  than hardcoding them into generated source.

### Compiler boundary

- The core compiler remains string-in/string-out and performs no dependency
  discovery, filesystem access, compilation, or linking.
- Generated runtime components may include the private adapter header, but no
  generated module header exposes `uv_*` types or includes `<uv.h>`.
- The compiler emits Hexal's adapter as ordinary generated artifacts under
  `hexal/`; it does not emit libuv's source code.
- RFC 0055's driver supplies the qualified libuv include root and static archive
  when the generated component manifest selects the async runtime.
- Programs that do not select the event runtime emit no event adapter. Programs
  selecting Task/Channel/Mutex still link libuv's native threading substrate;
  programs selecting neither concurrency nor event facilities do not link it.

### Source dialect

- Hexal-generated adapter files remain C23.
- Libuv is compiled separately under the dialect and flags qualified for the
  pinned release. It is not rewritten or forced to adopt Hexal's C23 style.
- The final objects link only when the target ABI and C-runtime contracts match.

## Generated component boundary

Add one demand-driven component pair:

```text
hexal/event.h
hexal/event.c
```

Its source template lives with the other runtime templates under
`compiler/generator/packages/`. The names describe the Hexal responsibility,
not the current dependency, so replacing libuv would not rename generated
artifacts or public compiler metadata.

`hexal/event.h` exposes only compiler-private Hexal runtime operations. It does
not form a language or foreign ABI. At minimum:

```c
typedef void (*hex_event_work_entry)(void *context);

void hex_event_runtime_init(void);
void hex_event_work_call(hex_event_work_entry entry, void *context);
```

These functions are not wrappers that merely rename libuv calls:

- initialization owns the loop thread and submission channel;
- `hex_event_work_call` connects a stackful Task to `uv_queue_work`;
- both enforce Hexal's park/commit/wake and result-visibility contracts; and
- later socket and timer adapters share the same loop owner.

`hexal/event.c` owns the loop, request, command-queue, and completion state.
`hexal/io.c` calls the neutral adapter. `hexal/concurrency.c` supplies the Task
parking and wake operations and may use libuv's native thread, mutex, condition,
and parallelism APIs directly, but contains no worker-pool or event-loop
implementation. `<uv.h>` may appear only in generated runtime implementation
files; it never appears in a module header or a Hexal-facing declaration.

### Runtime-native substrate

When Task/Channel/Mutex support is selected:

- create non-root native scheduler workers with `uv_thread_create` or
  `uv_thread_create_ex` and release them with the pinned detach/join policy;
- use `uv_mutex_t` and `uv_cond_t` for scheduler-internal ready queues and
  control-block guards;
- use `uv_once_t` where process-wide runtime initialization can race;
- use `uv_available_parallelism()` for the zero-configuration scheduler-worker
  default; and
- retain the existing Task-aware Channel and Mutex queues above those native
  guards.

Keep C23 `_Atomic` and `_Thread_local` where they already state the required
contract directly. Keep the Windows Fiber and qualified POSIX context-switch,
guard-page, alternate-stack, and stack-overflow mechanisms because libuv does
not provide fiber contexts or async-signal-safe overflow recovery.

## Demand rules

Libuv linkage and event-component emission are separate demand facts:

- Task/Channel/Mutex selects libuv linkage because the scheduler uses its
  native threading and synchronization substrate.
- An event-driven facility, or the scheduler combined with a reachable native
  blocking path, additionally selects `event.h` and `event.c`.
- Atomic alone selects neither.

The compiler selects the event component conservatively from program-wide
demand. It does not try to prove the dynamic caller of each operation. Event
selection requires:

- the program uses Task/Channel/Mutex and reaches an IO transfer, descriptor
  close, or print's descriptor write-all sink;
- a future socket, timer, asynchronous DNS, or explicitly blocking foreign-call
  operation is reachable.

It is not selected for:

- Task/Channel/Mutex without native asynchronous or blocking operations; these
  link libuv but emit no event pair;
- Atomic without another qualifying operation;
- IO or print in a program that selects no scheduler runtime; or
- Bytes and in-memory operations alone.

Selection is program-wide and emits exactly one `event.h`/`event.c` pair.

## Loop ownership and submissions

- Exactly one thread owns the initial `uv_loop_t` and calls `uv_run`.
- Only the loop-owner thread creates, starts, stops, mutates, or closes libuv
  handles and requests unless the libuv API explicitly permits cross-thread use.
- Scheduler workers submit commands to one mutex-protected FIFO and notify the
  loop through one `uv_async_t` handle.
- A submission publishes its fully initialized command before sending the
  async notification.
- The loop drains commands in FIFO order. FIFO is a deterministic bridge rule,
  not a promise about native completion order.
- The queue contains intrusive request records; submitting one operation does
  not allocate merely to communicate with the event loop.
- The loop callback never executes user Hexal code. It stores the typed result
  and wakes the owning Task.
- A libuv callback must not acquire the scheduler ready mutex while holding the
  command-queue mutex.
- A scheduler worker never blocks inside `uv_run` or waits on a libuv condition.

The command queue is adapter plumbing, not a second worker pool. It owns no
worker threads and executes no blocking operation.

## Existing IO routing

Do not route every current descriptor operation through generic work without
first checking libuv's specialized APIs.

- Prefer asynchronous `uv_fs_read`, `uv_fs_write`, and `uv_fs_close` when the
  target's `uv_file` representation accepts the IO handle and preserves the
  current shared-cursor contract. Offset `-1` is required for read/write so the
  native file position is used and updated.
- Classify standard handles with libuv's handle facilities before assuming one
  filesystem request works for terminals, pipes, and ordinary files on every
  target. A matching TTY or pipe stream adapter may be used where it preserves
  the exact IO contract.
- `IO.seek` has no general libuv filesystem seek request. Keep its existing
  minimal native seek core and execute that core through `uv_queue_work` from a
  Task.
- Task-aware descriptor print uses the same selected write route as `IO.write`;
  it does not maintain a second write implementation.
- Every completed `uv_fs_t` request calls `uv_fs_req_cleanup` exactly once after
  its result and any owned data have been consumed.
- If a target probe proves that a current standard-handle representation cannot
  use a specialized request without changing semantics, retain the smallest
  existing native transfer core behind `uv_queue_work` and record that target-
  specific reason in the implementation. Do not generalize that exception to
  other handles or future file APIs.

The future path-based file library uses libuv's filesystem family for open,
close, read, write, metadata, directories, links, permissions, timestamps,
copying, and sendfile. It does not introduce another portable filesystem layer.

## Blocking work bridge

For a scheduler-aware blocking operation:

1. The current Task creates one request record in storage that remains live
   while the Task is parked.
2. The record contains `uv_work_t`, the blocking entry, typed context, owning
   Task, completion result, and submission state.
3. The Task writes its pending link and release phase before registration can
   lead to a wake.
4. The loop owner calls `uv_queue_work`.
5. The libuv worker callback invokes only the blocking native entry and writes
   its result into the typed context.
6. The loop-thread after-work callback translates libuv submission or
   cancellation state, then invokes the existing Task wake transition.
7. The Task resumes only after the after-work callback has finished with the
   request record.
8. The resumed Task consumes its result and lets the request record leave scope.

The worker callback must not touch the scheduler queue, resume a fiber, allocate
through a Task-local allocator, or call user Hexal code.

An operation invoked without a current Task keeps the current synchronous
direct path. It does not start the event runtime solely to block the calling
thread through libuv.

### Lifetime

- A request and every buffer named by it remain valid through the after-work
  callback, including cancellation and shutdown paths.
- Parking retains the fiber stack, so a stack request is valid only while no
  terminal path resumes or destroys the Task before libuv releases the record.
- Completion payload is written before the release wake transition; the resumed
  Task reads it only after the matching acquire transition.
- One request has one terminal owner. Submission failure, completion,
  cancellation, and shutdown cannot publish the Task twice.
- A detached Task does not permit early request or buffer destruction.

### Failure mapping

- Event-runtime initialization failure traps before affected user operations
  execute because no operation can safely proceed.
- `uv_queue_work` submission failure is translated through the operation's
  existing Error result when that operation can fail.
- Native operation failure retains the existing IO Error contract and does not
  expose a libuv numeric code as a new language concept.
- No failed submission silently falls back to a new Hexal-created worker.

## Event-driven sockets and timers

This RFC chooses the backend but does not define public socket or timer syntax.
Their follow-up specifications use the same component and loop owner.

- TCP/UDP handles and timers live on the loop-owner thread.
- A socket operation first uses the backend's normal asynchronous mechanism; it
  never enters `uv_queue_work` merely to wait for socket readiness.
- One-shot Hexal operations adapt libuv's callback/stream model without exposing
  callbacks to Hexal code.
- Read and write buffers remain live until libuv's completion callback releases
  them.
- Closing a handle waits for libuv's close callback before reclaiming its native
  state.
- Timer, readiness, cancellation, and close races select one terminal owner
  before waking the Task.
- Cross-thread submissions always enter through the command queue and
  `uv_async_t`; Tasks do not call handle-mutating APIs directly.
- TCP uses libuv bind/listen/accept/connect/read/write/shutdown and socket-option
  facilities rather than a parallel socket backend.
- UDP uses libuv bind/connect/send/receive, multicast, broadcast, and queue-
  state facilities rather than a parallel datagram backend.
- Hostname resolution uses `uv_getaddrinfo` and `uv_getnameinfo`; address parsing,
  formatting, and interface enumeration use the corresponding libuv utilities.
- Imported descriptors use `uv_poll_t` only when no typed libuv handle owns
  them. One native descriptor cannot be polled by competing owners.
- Deadlines and Task sleep use `uv_timer_t`; no Task sleeps a scheduler or pool
  thread through `uv_sleep`.

## Later library foundations

Libuv is the required implementation base when these surfaces are specified:

- filesystem: the complete `uv_fs_*` request family, filesystem events, and
  filesystem polling;
- local IPC: Unix-domain sockets, Unix pipes/FIFOs, Windows named pipes, and
  supported inter-process handle passing;
- terminal: TTY mode, size, restoration, and Windows virtual-terminal support;
- process: spawn, stdio routing, environment/cwd, exit observation, signalling,
  and process IPC;
- dynamic C interoperability: shared-library open, symbol lookup, error, and
  close operations;
- randomness: `uv_random` as the portable system-CSPRNG source;
- time and observability: high-resolution and monotonic clocks, resource usage,
  CPU and memory information, network interfaces, loop metrics, and active-
  handle diagnostics; and
- OS utilities: environment, executable/current/home/temp paths, hostname,
  system identity, user/group lookup, process identity, and priority.

Each library still defines a small Hexal-native type and Error contract. This
RFC chooses the implementation substrate; it does not turn the libuv C API into
the Hexal standard library.

The stack-overflow signal/exception path is excluded because it must remain
async-signal-safe and adjacent to the fiber implementation. Libuv signal handles
may back later ordinary process-signal observation but do not replace that path.

RFC 0146 owns libuv's allocator-replacement hook. It may unify libuv's internal
raw allocation with Hexal's mimalloc backend, but it does not express or replace
Hexal's explicit Heap, Stash, or Pool ownership.

## Cancellation

- This RFC adds no language-visible cancellation API.
- The adapter records enough state for later Task cancellation and deadlines.
- `uv_cancel` may be used only for request kinds libuv documents as cancellable.
- Successful cancellation still completes through the normal after-work or
  request callback before the Task or its buffers can be reclaimed.
- Failure to cancel means the operation remains live; it does not authorize
  early wake or early buffer reuse.
- Running blocking work is not promised to stop. A later cancellation spec must
  distinguish abandoning a Task's interest from interrupting the native call.

## Shutdown

- Runtime shutdown first rejects new event and worker submissions.
- Owned libuv handles are closed on the loop-owner thread.
- Requests that can be cancelled are offered cancellation, but their callbacks
  still own final release.
- Graceful subsystem shutdown drains live callbacks before closing the loop.
- The loop thread exits only after `uv_run` has no retained work required by the
  selected shutdown contract.
- Existing root completion does not gain an implicit promise to join detached
  Tasks or wait forever for an uninterruptible native call.
- Process termination may abandon detached work under the existing Task
  contract; the operating system then reclaims libuv threads and resources.
- Tests and explicit graceful server shutdown must close and drain all owned
  requests so leak and race validation remains meaningful.

## Worker-pool policy

- Hexal uses libuv's one global worker pool; it creates no peer pool.
- Filesystem work, asynchronous DNS, `uv_queue_work`, and any other selected
  libuv facility therefore share capacity.
- The first implementation accepts libuv's default pool configuration.
- Users may set `UV_THREADPOOL_SIZE` before process start as an advanced runtime
  deployment control. Hexal source has no pool-size syntax or API.
- Pool sizing is not derived from scheduler worker count: CPU scheduler
  parallelism and blocking-operation concurrency are different resources.
- A future Hexal setting is added only if measurement shows the environment
  control inadequate and libuv offers a supported programmatic mechanism. Hexal
  does not patch libuv solely to add such a knob.
- Queue delay, active work, completion latency, native thread count, and memory
  must be measured before changing the pinned default or exposing configuration.

## Metrics and diagnostics

- Enable libuv event-provider idle-time accounting on the program loop.
- Use loop iteration, processed-event, waiting-event, and idle-time metrics in
  runtime benchmarks; do not expose them as stable language API in this RFC.
- Use `uv_hrtime` for runtime performance intervals.
- Use libuv error names and messages only as backend evidence. Translate them
  into the owning Hexal operation's existing Error shape rather than exposing
  raw negative libuv codes as a new language type.
- Use libuv version queries during external-backend qualification and reject a
  header/archive mismatch before running user code.
- Active-handle dumps are debug and test diagnostics only; their textual format
  is not stable output and must not enter a language contract.

## Required sweep

Implementation removes every compiler-owned blocking-pool artifact:

- `hex_blocking_entry` and `hex_blocking_call` declarations;
- `hex_blocking_job`;
- blocking queue head, tail, mutex, and condition variable;
- baseline, total, busy, and queued counters;
- `hex_blocking_worker` and `hex_blocking_init`;
- demand-growth, retirement, and failed-growth recovery;
- scheduler initialization of that pool;
- pool-specific runtime trap strings;
- tests and comments asserting `hex_blocking_*` structure or demand growth; and
- status text that describes the custom pool as current behavior.

Implementation also replaces redundant native-runtime portability code where
libuv provides the same contract:

- direct `_beginthreadex`/pthread creation, join, and detach wrappers used only
  for scheduler worker threads;
- direct SRW/pthread mutex and condition implementations used only as
  scheduler-internal guards;
- the custom logical-processor query used to choose the default worker count;
  and
- ad hoc once-initialization machinery superseded by `uv_once_t`.

Retain direct platform code required for fiber contexts, guarded stacks,
alternate signal stacks, stack-overflow recovery, and other behavior libuv does
not provide. The sweep must classify every remaining platform branch by that
need; proximity to deleted code is not a reason to remove it.

Retain and adapt:

- only those native descriptor transfer cores in `hexal/io.c` that the
  specialized-IO probes show are still required;
- the direct path used outside a Task;
- the current IO and print result contracts;
- the common Task park/commit/wake protocol;
- its pending-link and payload-ordering rules; and
- demand selection that distinguishes Task-aware native work from Task-only or
  IO-only programs.

No old worker, queue, condition variable, growth counter, or trap survives as a
fallback “for safety.”

## Implementation plan

### Phase 1: qualify and package libuv

1. Select and record an exact libuv source revision and license files.
2. Build static archives for each currently supported RFC 0052 target pack.
3. Record libuv compile flags, transitive platform libraries, headers, archive
   digest, target identity, and ABI evidence in the pack manifest.
4. Compile and run minimal loop, async wake, timer, filesystem, DNS,
   `uv_queue_work`, native-thread, synchronization, parallelism, clock, random,
   metrics, TCP, UDP, pipe, TTY, process, signal, polling, and dynamic-library
   probes for every executable host target.
5. On Windows, probe ordinary files, stdin/stdout/stderr, pipes, terminals, and
   sockets separately under the exact Clang/MinGW ABI and both supported
   architectures.
6. Reject a pack whose libuv archive, headers, consumer flags, runtime version,
   target, or transitive libraries do not match its profile.

### Phase 2: replace the native scheduler substrate

1. Replace scheduler-worker thread creation and lifecycle with qualified
   `uv_thread_*` operations.
2. Replace scheduler-internal native mutexes and conditions with `uv_mutex_t`
   and `uv_cond_t` while retaining Task-aware wait queues.
3. Replace the logical-processor query with `uv_available_parallelism()`.
4. Use `uv_once_t` only for initialization that can race.
5. Retain and test the direct fiber-context, stack, guard, and overflow paths.
6. Prove that Task/Channel/Mutex selects libuv linkage while Task-only programs
   still emit no event-loop component.

### Phase 3: add the neutral event component

1. Add `packages/event.h` and `packages/event.c` plus typed render models.
2. Implement loop initialization, its dedicated `uv_thread_t`, the command
   FIFO, `uv_async_t` wakeup, and ordered loop shutdown.
3. Keep event-loop and request `uv_*` spellings inside `event.c`; only
   runtime-native threading and synchronization spellings may additionally
   occur in `concurrency.c`.
4. Add demand-driven component selection and exact dependency propagation.
5. Enable loop idle-time metrics and add internal measurement access without a
   language-visible API.
6. Verify programs outside the event-demand matrix emit no event artifacts.

### Phase 4: replace the custom blocking pool and route IO

1. Implement the stack-resident libuv work request and its state transitions.
2. Probe `uv_fs_read`, `uv_fs_write`, and `uv_fs_close` against every supported
   IO handle class and the existing cursor, EoS, partial-transfer, and Error
   contracts.
3. Route each matching operation through its specialized asynchronous request.
4. Route seek and every documented mismatch through `hex_event_work_call` and
   `uv_queue_work` around the smallest retained native core.
5. Route descriptor print through the same write path as IO.
6. Preserve direct native calls outside a current Task.
7. Call request cleanup exactly once and map submission/operation failures
   without changing language signatures.
8. Remove all custom-pool and superseded native-core code named in Required
   sweep.
9. Replace structural tests of the old pool with structural tests of the libuv
   bridge and absence tests for every superseded symbol.

### Phase 5: runtime race validation

1. Add tagged external fixtures for immediate completion, delayed completion,
   saturation, cancellation-before-start where supported, and shutdown.
2. Exercise N scheduler workers with more than N blocking requests and verify
   scheduler progress while work is outstanding.
3. Verify result visibility, exactly-once resume, and no early request reuse.
4. Run sanitizer-qualified fixtures subject to the fiber-annotation limits
   recorded by the external validation suite.
5. Record libuv pool size, native thread count, peak memory, and queue delay.

### Phase 6: establish the network and timer backend

1. Add internal TCP listen, accept, connect, read, write, and close adapters
   without yet defining the final public Hexal API.
2. Prove socket waits use the event loop and create no worker-pool job.
3. Add UDP, DNS, imported-descriptor polling, timer registration, and
   cancellation primitives behind internal adapters.
4. Verify DNS uses libuv's shared pool while socket waiting does not.
5. Exercise 1,000 and 10,000 idle connections where the host permits them and
   verify bounded native thread count.
6. Feed the measured results and adapter constraints into RFC 0144's focused
   socket and timer specifications.

### Phase 7: synchronize contracts

1. Update RFC 0052 with the pinned per-target libuv pack requirements.
2. Update RFC 0055 with event-component detection, include/archive selection,
   link flags, and external-override qualification.
3. Update RFC 0132 only where its implementation plan or tests name the removed
   custom blocking pool; do not change its scheduler semantics.
4. Update RFC 0144 to select libuv and remove the backend/topology questions
   decided here.
5. With explicit approval, update `docs/reference.md` once for the stabilized
   runtime backend contract and removal of the custom pool description.
6. Remove the custom-pool coverage-gap entry from `docs/status.md` only after
   the replacement runtime fixtures pass.
7. Review and regenerate the snippet artifact manifest only for generated files
   whose content legitimately changes.
8. Record libuv as the required substrate in later filesystem, IPC, terminal,
   process, random, dynamic-library, and OS-library specifications; do not add
   those language surfaces in this RFC.

## Validation

This section is exhaustive. The RFC is complete only when all items pass.

### Dependency and artifacts

- Every supported target pack contains one pinned, statically linkable libuv
  archive, matching headers, required consumer flags, transitive libraries,
  license material, version evidence, and ABI evidence.
- The x86-64 and AArch64 Windows GNU packs independently pass the complete
  probe set; upstream's generic Windows support claim is insufficient.
- A selected async runtime emits exactly one `hexal/event.h` and
  `hexal/event.c`; an unselected program emits neither.
- Task/Channel/Mutex links libuv and uses its native threading substrate even
  without event artifacts; a program selecting neither concurrency nor an
  event facility does not link libuv.
- No generated module header includes `<uv.h>` or exposes a `uv_*` type.
- Event-loop/request calls occur only in `hexal/event.c`; runtime-native thread,
  mutex, condition, once, and parallelism calls may additionally occur in
  `hexal/concurrency.c`.
- The core compiler performs no filesystem, process, archive, or target probe.

### Native runtime substrate

- Scheduler worker creation, join/detach policy, internal mutexes and
  conditions, once initialization, and default parallelism use the qualified
  libuv facilities.
- No superseded `_beginthreadex`/pthread thread wrapper, direct SRW/pthread
  guard implementation, or custom logical-processor query remains.
- Task-aware Channel and Mutex operations still park Tasks and never become
  blocking native libuv lock operations.
- C23 atomics and `_Thread_local` remain direct; fiber contexts, guarded stacks,
  alternate signal stacks, and overflow recovery retain their required native
  implementations.

### Pool replacement

- No generated artifact contains `hex_blocking_`, its queue/counters, worker
  loop, initialization, growth/retirement logic, or pool-specific traps.
- Task-aware IO read/write/close and descriptor print use specialized libuv
  requests wherever the target handle probes prove an exact match.
- Seek and every recorded specialized-request mismatch route through
  `hex_event_work_call`; no matching operation is needlessly wrapped in
  `uv_queue_work`.
- Every completed filesystem request receives exactly one
  `uv_fs_req_cleanup`.
- The same operations outside a current Task retain the direct native path.
- Task-only, Atomic-only, IO-only, print-only, Bytes-only, and memory-only
  programs select no event component unless another reachable operation
  requires it. A program containing both scheduler use and a native blocking
  path selects it conservatively even when those features are not dynamically
  combined.
- Hexal creates no general blocking worker or fallback pool outside libuv.

### Scheduling and memory safety

- Immediate and delayed completion each resume the owning Task exactly once.
- Submission failure cannot leave the Task parked or publish it twice.
- The worker writes its result before the release wake; the resumed Task sees
  the complete result after acquire.
- Request and buffer storage remain live through the final libuv callback on
  completion, cancellation, and shutdown.
- Worker callbacks execute no user code and manipulate no scheduler queue.
- Loop callbacks publish Tasks only through the existing wake transition.
- More blocking operations than scheduler workers do not prevent an unrelated
  ready Task from running.

### Event loop and networking foundation

- The initial runtime owns exactly one event loop and one dedicated loop thread.
- Cross-thread commands use one FIFO and one async wake handle; only the loop
  owner mutates ordinary libuv handles.
- Socket accept, connect, read, write, and close adapters create no
  `uv_queue_work` request merely to wait for network progress.
- UDP and asynchronous DNS use libuv's typed facilities; an imported descriptor
  uses `uv_poll_t` only when no other libuv handle owns it.
- Task sleep and deadlines use `uv_timer_t`; no Task path calls `uv_sleep`.
- Idle-connection fixtures retain a bounded native thread count as connection
  count grows.
- Timer/readiness/cancel/close probes select one terminal owner and produce no
  stale or duplicate Task wake.
- Loop idle time and event metrics are available to the benchmark harness, and
  active-handle diagnostics remain debug/test-only unstable text.

### Build and conformance

- Generated adapter C compiles and links against every supported target pack.
- Runnable host targets pass loop, worker, IO, timer, and socket fixtures under
  the external C validation lifecycle.
- The exact existing IO and print success, EoS, Error, cursor, and evaluation-
  order contracts remain unchanged.
- Ordinary `go test ./...` remains pure Go and needs no installed libuv or C
  toolchain.
- Every manifest change is confined to programs selecting the replaced runtime
  path and is reviewed by artifact family.

## Non-goals

- Replacing Hexal's fiber scheduler with libuv callbacks.
- Exposing libuv handles, callbacks, request types, error codes, or loop APIs to
  Hexal programs.
- Defining socket, timer, cancellation, `select`, async/await, Future, or HTTP
  syntax.
- Promising that queued blocking work can always be interrupted.
- Adding a second worker pool, a direct epoll/kqueue/IOCP fallback, or a patched
  libuv fork.
- Sharding the event loop before the initial topology is measured.
- Making a system-installed shared libuv part of the default runtime contract.

## Open implementation inputs

The architecture is selected. Before implementation begins, record:

- the exact pinned libuv revision;
- static-build flags and transitive system libraries for each target pack;
- the driver-facing component/dependency metadata shape; and
- the external validation mechanism used to obtain the target pack in CI;
- the per-target result of the ordinary-file, standard-handle, pipe, TTY, and
  socket representation probes that decide specialized IO routing; and
- successful x86-64 and AArch64 qualification of the intended Windows GNU
  packs despite their upstream Tier-3 classification.

These inputs do not reopen use of libuv, static default linkage, the single-loop
initial topology, or replacement of the custom blocking pool.

## Implementation readiness

The runtime design is ready for dependency qualification. Code implementation
is blocked on RFC 0132 and on recording the four packaging inputs above in RFCs
0052 and 0055. The language surface has no open decision in this RFC.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. During implementation, and only
with explicit approval, replace the current custom-blocking-pool statements with
the stabilized libuv-backed execution contract. Keep dependency installation,
target-pack contents, loop topology, and worker-pool deployment controls in the
backend and driver specifications rather than the language reference.
