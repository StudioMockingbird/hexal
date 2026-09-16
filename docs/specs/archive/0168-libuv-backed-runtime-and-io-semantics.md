# RFC 0168: libuv Capability Arc

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. The runtime substrate is implemented and every
  non-deferred child surface it indexes is implemented and closed. The
  remaining children, RFC 0175 (filesystem watchers) and RFC 0177 (dynamic
  libraries), are deferred and retain their own records, which do not keep
  this coordination index active. The `docs/reference.md` POSIX-target
  mismatch this specification flagged is corrected with explicit user
  approval.
- Created: 2026-09-13
- Updated: 2026-09-14
- Scope: assign libuv-backed operating-system capabilities to focused RFCs
- Baseline: RFC 0145 and RFC 0146 are closed and implemented
- Does not authorize: implementing a child language surface or editing
  `docs/reference.md`

## Purpose

This RFC is the index and ownership policy for Hexal's libuv capability arc.
It is not an executable implementation specification. Each child RFC owns its
public types, operations, semantics, implementation plan, exhaustive
validation, and reference synchronization.

## Current implemented baseline

The compiler and generated runtime already provide:

- pinned and verified libuv and mimalloc dependencies;
- libuv native threads, mutexes, conditions, thread detachment, and available-
  parallelism discovery for the Task scheduler;
- one program-wide `uv_loop_t` on one dedicated native thread;
- a mutex-protected command FIFO and `uv_async_t` loop wakeup;
- one Task-parking bridge through `uv_queue_work` for blocking native work;
- stack-resident one-shot work records retained by the parked Task;
- current IO read, write, seek, close, and print operations routed through that
  bridge only when called from a Task;
- direct synchronous native IO outside a Task;
- demand-driven `hexal/event.h` and `hexal/event.c`; and
- one demand-driven native bootstrap that installs mimalloc through
  `uv_replace_allocator` before every other libuv call, independently of
  scheduler selection;
- the shared generation-checked handle registry and portable libuv ErrorKind
  mapper;
- File, Duration/Instant/WallTime, Task sleep, Address/DNS/TCP, Process/Pipe,
  Signals, and Terminal source surfaces; and
- exact component selection for those implemented capabilities.

The old `hex_blocking_*` pool and replaced Windows/POSIX scheduler wrappers no
longer exist. Child RFCs must start from this baseline and must not schedule
their removal again.

## Adoption rule

For one required operation, choose the first exact semantic match:

1. Use a C23 language or standard-library facility when it implements the
   contract directly.
2. Otherwise use the pinned libuv facility when it supplies the required
   portability, asynchronous completion, or operating-system abstraction.
3. Otherwise retain the smallest direct native operation. When it may block a
   Task scheduler worker, execute it through `uv_queue_work`.

Consequences:

- Retain C23 `_Atomic` and `_Thread_local`; do not replace them with libuv
  wrappers.
- Retain Hexal fibers, Task scheduling, Channel queues, and Task-aware Mutex
  queues; libuv does not supply their language semantics.
- Do not add a second event loop, native-work pool, or alternate backend for an
  operation owned by libuv.
- Do not route an operation through libuv solely for API uniformity.
- Do not expose `uv_*` types, callbacks, handles, error numbers, or loop APIs in
  Hexal source or generated public module headers.
- A source-facing capability is added only when its focused RFC establishes a
  cohesive Hexal use case. Availability in libuv alone is not sufficient.

## Asynchronous runtime topology

```text
Hexal Task
    -> private typed request
    -> event command FIFO
    -> one libuv loop thread or libuv worker pool
    -> typed result publication
    -> common Task wake transition
    -> scheduler worker resumes Hexal code
```

- This topology applies only to asynchronous or Task-parking operations.
  Stateless queries and synchronous calls may use libuv directly after native
  bootstrap and do not traverse the event FIFO.
- No libuv callback executes Hexal user code.
- The loop-owner thread exclusively mutates ordinary libuv handles unless the
  libuv API explicitly permits cross-thread use.
- Result and Error storage are written before the Task wake is published.
- One terminal path owns request and handle cleanup.

## Demand rules

- Task, Channel, or Mutex selects `RuntimeLibuv` for the native scheduler
  substrate.
- Any reachable operation selecting `RuntimeLibuv` also selects the one native
  runtime bootstrap. The root entrypoint calls it exactly once before module
  statements and before scheduler initialization.
- Atomic alone selects neither libuv nor the event component.
- `hexal/event.h` and `hexal/event.c` are selected only when a reachable Task
  operation can suspend on native work or a focused child RFC requires the
  event loop.
- Existing IO or print without Task currently uses its direct synchronous
  native path and does not select libuv. A child that changes this must state
  the complete compile-time demand and accepted cost; runtime terminal
  detection cannot make a link-time dependency conditional.
- A future event-driven capability selects libuv and the event component even
  when no source-level Task value is written explicitly, because its operation
  requires the Hexal runtime to park and resume execution.
- Component discovery remains program-wide and conservative over reachable
  checked code.

## Capability ownership

| Area | Implemented baseline | Child RFC responsibility |
| --- | --- | --- |
| Native scheduler substrate | RFC 0145 | None; preserve it |
| Runtime allocation | RFC 0146 | None; preserve it |
| Event-loop and Task bridge | RFC 0145 | Focused operations extend the existing bridge |
| Long-lived libuv handle lifecycle and portable Error mapping | Implemented shared generation-checked registry and ErrorKind mapper | Children reuse it; none may add a parallel registry or mapper |
| Existing standard IO | Native operation through `uv_queue_work` in a Task; direct outside a Task | RFC 0170 must not reopen this without an explicit representation migration |
| Files and directories | File operations implemented; broader directory operations absent | RFC 0170 is closed; new directory surface requires a focused RFC |
| Time and Task sleep | Duration, Instant, WallTime, and Task.sleep implemented | RFC 0171 is closed |
| TCP, UDP, DNS, and imported-descriptor polling | Address, DNS, and TCP implemented; UDP and imported-descriptor polling absent | UDP or polling requires a focused RFC |
| Processes and process IPC | Process and parent/child standard-stream Pipe implemented; named IPC and handle passing absent | RFC 0173 is closed; deferred IPC requires a focused RFC |
| TTY | Terminal detection, dimensions, and Windows Unicode print implemented; raw mode and resize notification absent | RFC 0174 is closed; RFC 0184 owns whole-print transactions and deferred RFC 0188 owns the selected POSIX `isatty` cleanup after a POSIX target is qualified |
| Filesystem watchers | None | RFC 0175 is deferred |
| Ordinary signals | Signals implemented; stack-overflow handling remains separate | RFC 0176 is closed |
| Dynamic libraries | None | RFC 0177 is deferred |
| Secure entropy and focused program queries | Existing narrow runtime queries only | RFC 0178 |
| Program entry and exit | Generated C `main(void)` with no Hexal argument or status surface | RFC 0182 owns arguments, root return, and any target-specific `uv_setup_args` call |
| Performance and loop scaling | Single loop | RFC 0144, after measurement |

Imported-descriptor polling and any `uv_poll_t` use were not implemented by RFC
0172. They require a focused future RFC and remain outside the C-interop
surface.

## Accepted constraints

- Programs selecting the asynchronous runtime link the pinned static libuv
  archive and mimalloc.
- Existing IO-only and print-only programs retain a direct path. RFC 0174's
  native Windows-console branch and RFC 0184's call-local builder add no libuv
  dependency by themselves.
- Libuv filesystem, DNS, random, and `uv_queue_work` requests share libuv's
  global worker pool and may contend.
- The first implementation accepts libuv's default pool policy and the
  pre-start `UV_THREADPOOL_SIZE` environment control. Hexal adds no project
  setting until measurement proves it necessary.
- One loop thread is the initial architecture. RFC 0144 owns any measured case
  for sharding; no child RFC is blocked on speculative sharding.
- POSIX target branches remain unqualified until their target profiles pass the
  external generated-C and runtime gates. A source branch existing in the tree
  is not a support claim.

`docs/reference.md` currently names POSIX x86-64 alongside the qualified
Windows x64 runtime. RFC 0052 qualifies only Windows x64 today. This is a known
canonical-document mismatch; correct it only with explicit user approval rather
than allowing a child RFC to treat the POSIX branch as qualified evidence.

## Child-RFC requirements

Every child RFC must:

1. Start from the implemented baseline above.
2. Map each operation to an exact C23, libuv, or justified native facility.
3. Define Task and non-Task behavior only for forms the chosen facility can
   actually provide.
4. Define request, handle, buffer, and result lifetimes through final callback
   and close completion.
5. Define submission failure, runtime failure, cancellation, and shutdown.
6. Keep callbacks private and resume user code only through Task scheduling.
7. State demand-driven component and dependency selection.
8. Remove a replaced backend in the same change; state explicitly when no old
   implementation exists.
9. Measure generated size, link time, runtime allocations, and relevant
   throughput when the capability adds a runtime dependency to programs that
   previously avoided it.
10. Provide exhaustive generated-C compilation and runtime validation.
11. Update `docs/reference.md` only after approval and stable implementation.
12. Depend on RFC 0169 when the capability can select libuv without selecting
    Task; allocator installation must not remain an accidental scheduler side
    effect.

## Coordination

- RFC 0169 is a behavior-preserving cleanup of the implemented RFC 0145 bridge;
  it does not reimplement that bridge or own future capability surfaces.
- RFC 0180 owns the shared generation-checked copied-handle lifecycle, private
  runtime allocation, and common libuv Error categories. A child capability
  owns only its distinct wrapper type and capability-specific state/errors.
- RFC 0182 is the sole owner of generated process-entry widening and
  `uv_setup_args`. A child that needs setup coordinates with that adapter and
  never emits a second call.
- RFC 0184 restores the existing whole-print atomicity contract over the
  terminal-aware sink implemented by closed RFC 0174. It does not reopen the
  Terminal source API.
- Cancellation is specified by the operation that first requires it. A later
  public Task-cancellation RFC may unify source-level policy, but must not be a
  prerequisite for private timer, DNS, filesystem, or socket cancellation.
- Runtime metrics needed only for performance work belong to RFC 0144. Child
  RFCs record the counters needed to validate their own operations.
- The build driver remains separate from runtime filesystem APIs. The compiler
  remains string-in/string-out.
- After every non-deferred child closes, close and archive this coordination
  index. Deferred ideas retain their own records and do not keep the umbrella
  active indefinitely.

## Reference synchronization

Do not edit `docs/reference.md` from this umbrella. Each implemented child RFC
updates only its observable Hexal contracts after explicit user approval.
