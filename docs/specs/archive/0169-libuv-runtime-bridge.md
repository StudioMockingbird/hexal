# RFC 0169: libuv Runtime Bridge Cleanup

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; cleanup of the RFC 0145 runtime bridge
- Created: 2026-09-13
- Updated: 2026-09-13
- Scope: centralize libuv bootstrap, remove unused speculative bridge
  facilities, and close two runtime diagnostic/lifetime gaps without changing
  Hexal semantics
- Depends on: RFC 0145, RFC 0146, and RFC 0168
- Does not add: syntax, public APIs, source-level cancellation, timers, DNS,
  polling, metrics, or orderly process shutdown

## Summary

RFC 0145 already implemented the libuv runtime bridge. This RFC does not build
a second bridge or repeat that migration. It makes the generated event
component match the source-reachable runtime:

- retain the `uv_queue_work` Task bridge used by current IO;
- retain the loop-owner thread, command FIFO, `uv_async_t` wakeup, and common
  Task park/wake transition;
- retain C23 `_Atomic` and `_Thread_local`;
- move libuv allocator installation from scheduler initialization to one
  demand-driven program-wide native-runtime bootstrap;
- remove unreachable timer, poll, DNS, shutdown, and metrics entry points;
- replace the loop-thread's silent `abort()` on initialization failure with the
  normal stable Runtime Error path; and
- pin the stack-resident request lifetime proof beside the bridge.

Focused capability RFCs add their typed event commands when their public
surface is implemented. Keeping dormant adapters in every event-enabled
program is not integration and conflicts with Hexal's YAGNI rule.

## Retained bridge contract

```text
Task calls blocking native operation
    -> initialize complete request record on current fiber stack
    -> arm Task with pending record
    -> submit uv_queue_work
    -> commit park
    -> worker writes typed result
    -> loop callback publishes completion
    -> common Task wake transition
    -> Task resumes and consumes result
```

- One-shot work records stay on the parked fiber stack.
- A Task with an armed request cannot resume, complete, detach-reclaim, or have
  its fiber stack destroyed before the terminal callback releases that request.
- Submission failure cancels the pending Task transition before the call
  returns and never parks the Task.
- Worker and callback code execute no Hexal user code.
- Result and status storage are complete before wake publication.
- Completion wakes the Task exactly once.
- Existing IO outside a Task remains a direct synchronous native call.
- The first pool policy remains libuv's default plus pre-start
  `UV_THREADPOOL_SIZE`; Hexal adds no setting.

## Deliberately retained facilities

- libuv native scheduler threads, mutexes, conditions, detachment, and
  available-parallelism discovery;
- C23 `_Thread_local` current-Task and synchronous-fault state;
- the private event component and its work request;
- the single loop-owner thread and asynchronous command wakeup;
- mimalloc installation before any other libuv call; and
- process-exit abandonment of detached Tasks and their native work.

`uv_key_t` is not an improvement over `_Thread_local`: it adds a lookup on the
scheduler hot path, and the current-guard value is read by synchronous fault
handling where a pthread-key lookup is not a valid replacement.

## Program-wide native bootstrap

Libuv initialization cannot belong to the scheduler: File and monotonic-clock
operations can select libuv without selecting Task.

- When `RuntimeLibuv` is selected, generated `hexal.h` declares
  `hex_runtime_native_init()` and generated `hexal/runtime.c` defines it.
- The selected root module calls it exactly once, before any module statement
  and before `hex_scheduler_init()` when the scheduler is also selected.
- It calls `uv_replace_allocator(mi_malloc, mi_realloc, mi_calloc, mi_free)`
  before any other libuv function. Failure traps with exactly
  `[Runtime Error] libuv allocator installation failed\n`.
- Remove `hex_install_libuv_allocator` and its call from
  `hexal/concurrency.c`; scheduler initialization assumes the program bootstrap
  has completed.
- Programs that select neither libuv nor a libuv-backed capability emit no
  declaration, definition, call, `<uv.h>`, or `<mimalloc.h>` for this
  bootstrap. Wall-clock-only code therefore does not select it.
- This bootstrap installs process-global dependency policy only. Event-loop
  initialization remains owned by the scheduler/event bridge and occurs only
  when demanded.

## Required removals

The following generated declarations and definitions have no caller outside
their own component and no current Hexal operation can reach them:

- `HEX_EVENT_TIMER`, `hex_event_timer`, its callbacks, and
  `hex_event_timer_wait`;
- `HEX_EVENT_POLL`, `hex_event_poll`, its callbacks, and
  `hex_event_poll_wait`;
- `HEX_EVENT_DNS`, `hex_event_dns`, its callback, and
  `hex_event_dns_lookup`;
- `hex_event_runtime_shutdown` and its accepting, shutdown-requested, and
  stopped state; and
- `hex_event_idle_time` and idle-metrics-only configuration.

Remove their declarations from `hexal/event.h`, their definitions and command
branches from `hexal/event.c`, their now-unused standard/libuv dependencies,
and tests that require dormant names.

RFC 0171 may add a final one-shot timer command. RFC 0172 may add typed socket,
poll, and DNS commands. RFC 0144 may add measured runtime metrics. Those RFCs
must add only the exact facilities their validation reaches.

## Initialization failure

Failure of `uv_loop_init`, required loop configuration, or `uv_async_init`
currently calls `abort()` without a diagnostic. Replace that path with:

```text
[Runtime Error] event runtime initialization failed
```

written through the program-wide runtime trap. Partial initialization must
release every successfully initialized libuv object for which libuv permits
cleanup before termination; it must not call `uv_loop_close` over active or
unclosed handles merely to hide the failure.

## Why there is no shutdown API

Generated programs have one runtime lifecycle. Root return follows the current
contract: it does not join detached Tasks, and process termination may abandon
detached work. Calling the existing shutdown function would either wait for
detached work or cancel it, changing that contract. Since no caller can invoke
the function and the operating system reclaims process resources, remove it.

Any future reusable/embedded Hexal runtime needs a separate lifecycle RFC. A
server's ordinary resource close is owned by its File, socket, timer, process,
or watcher handles; it is not global loop shutdown.

## Cancellation boundary

This RFC adds no cancellation command because no current source operation can
request cancellation.

- `uv_cancel` applies only to documented request kinds and is best effort.
- Timer, poll, socket, and other persistent handles stop or close on the loop
  thread rather than through `uv_cancel`.
- A cancelled libuv request still reaches its completion callback, so request
  and buffer storage remain live until that callback.
- The first focused RFC needing private cancellation owns its command, terminal
  race, and cleanup validation.
- A public Task-cancellation surface remains a separate language decision.

## Implementation plan

### Phase 1: refresh the bridge inventory

1. Prove by repository search that only the work bridge is called outside the
   generated event pair.
2. Record any newly discovered caller as a blocker rather than deleting its
   target.
3. Snapshot generated artifacts and snippet-manifest hashes for every snippet
   selecting `hexal/event.c`.

### Phase 2: remove unreachable facilities

1. Remove timer declarations, request state, callbacks, command dispatch, and
   entry point.
2. Remove poll declarations, request state, callbacks, command dispatch, and
   entry point.
3. Remove DNS declarations, request state, callback, command dispatch, and
   entry point.
4. Remove shutdown declarations, state, branches, waits, destruction, and entry
   point.
5. Remove idle-time retrieval and metrics-only configuration.
6. Update the component tests to assert the smaller exact surface rather than
   the former speculative inventory.

### Phase 3: centralize native bootstrap

1. Add the demand-driven `hex_runtime_native_init` declaration, definition,
   and root-entrypoint call.
2. Move allocator installation out of the concurrency component without
   changing the installed function set or diagnostic.
3. Assert call order for libuv-only, scheduler-only, and combined programs.
4. Assert programs without libuv retain byte-identical artifacts.

### Phase 4: close the initialization diagnostic

1. Replace the loop-thread `abort()` with the stable runtime trap.
2. Preserve startup publication ordering: scheduler initialization returns only
   after the loop is ready, while initialization failure terminates with the
   diagnostic rather than deadlocking the waiting thread.
3. Add a focused external fixture or injectable C-level failure seam that
   proves exact stderr without changing the public compiler API.

### Phase 5: pin request lifetime

1. Keep the request record on the parked fiber stack.
2. Add structural assertions for arm-before-submit, submit-before-park-commit,
   result-before-wake, and no reclamation before terminal callback.
3. Exercise immediate submission failure, delayed completion, worker-pool
   saturation, and completion racing park commit.
4. Verify callbacks never invoke generated user functions.

### Phase 6: conformance

1. Run ordinary Go tests and vet without an external toolchain requirement.
2. Run generated C through the tagged C23 harness under qualified toolchains.
3. Run Task-aware IO and print fixtures and assert exact output and termination.
4. Verify IO/print without Task remains byte-identical and selects no libuv.
5. Regenerate only the expected `hexal/event.c`, `hexal/event.h`,
   `hexal/runtime.c`, `hexal.h`, `hexal/concurrency.c`, and root-module C hashes
   for snippets selecting libuv. A snippet selecting no libuv and every other
   artifact retain their existing hashes.

## Validation

- `hexal/event.h` exposes only event initialization and the work-call contract
  needed by current generated components.
- Every program selecting `RuntimeLibuv` calls
  `hex_runtime_native_init()` exactly once before its first possible libuv use.
- A libuv-only program emits that bootstrap without emitting the scheduler or
  event component; a program selecting no libuv emits none of it.
- Allocator installation has exactly one generated definition and no longer
  lives in `hexal/concurrency.c`.
- `hexal/event.c` contains no timer, poll, DNS, shutdown, metrics, or
  `uv_key_t` symbol.
- The work request remains stack-resident on a parked fiber and cannot outlive
  that fiber's retained stack.
- Submission failure does not park; ordinary completion writes its result
  before exactly one wake.
- Scheduler workers continue making progress when native work exceeds their
  count.
- No libuv callback executes user code.
- Event-loop initialization failure emits exactly
  `[Runtime Error] event runtime initialization failed\n` and does not leave the
  scheduler waiting for readiness.
- Task-aware IO and print retain their existing observable behavior.
- IO and print outside a Task retain their direct path and select no event pair.
- No custom native-work pool or second event backend exists.
- Manifest changes are confined to the exact bootstrap/event/concurrency/root
  artifacts named by the implementation plan; artifacts in programs selecting
  no libuv do not change.
- Generated C compiles and relevant fixtures run under every qualified target
  gate.

## Reference synchronization

This cleanup changes no public Hexal contract. During implementation, verify
that `docs/reference.md` already describes the retained Task and IO behavior;
do not edit it unless explicit user approval is given for a concrete mismatch.
