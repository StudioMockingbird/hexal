# RFC 0169: libuv Runtime Bridge

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: connect Hexal's M:N Task scheduler to the vendored libuv event loop,
  worker pool, native threading, synchronization, TLS, and metrics facilities
- Depends on: ADR 0145, ADR 0146, and RFC 0168
- Coordinates with: RFC 0118 and RFC 0144
- Does not add: source syntax, callbacks, futures, promises, or public libuv
  types

## Summary

Retain Hexal fibers and Task semantics. Replace every overlapping native
runtime facility with libuv and establish one bridge through which native
completion wakes a parked Task.

Initial topology:

```text
Hexal scheduler workers -> command FIFO -> uv_async_t -> one uv_loop_t thread
libuv completion        -> typed result -> Task wake -> scheduler worker
```

No libuv callback executes Hexal user code.

## Required backend ownership

Libuv exclusively supplies:

- native worker threads;
- runtime mutexes, conditions, read/write locks, semaphores, barriers, and
  once initialization;
- native-thread TLS through `uv_key_t`;
- thread naming and affinity when required;
- available-parallelism discovery;
- cross-thread loop wakeup through `uv_async_t`;
- the native blocking-work pool through `uv_queue_work`; and
- best-effort cancellation of cancellable requests through `uv_cancel`; and
- loop and handle metrics used for diagnostics, leak detection, and
  benchmarks.

Hexal retains:

- fiber creation, stacks, and context switching;
- Task scheduling, parking, waking, joining, and detaching;
- Task-aware Channel and Mutex queues;
- C23 atomic values;
- typed result and Error translation; and
- stack-overflow and synchronous-fault handling.

## Completion contract

For one native request:

1. Initialize its complete typed context.
2. Submit it before committing the Task to the parked state.
3. On submission failure, return without parking.
4. Store payload and status before publishing completion.
5. Wake the Task exactly once through the common Task transition.
6. Run no user code in the callback.
7. Give exactly one owner final cleanup.

Immediate completion, cancellation, shutdown, and ordinary completion must
converge on the same terminal-state protocol.

### Cancellation floor

Libuv cancellation is best effort, not a promise that executing native work
can be interrupted:

- cancellation before execution completes the request as cancelled;
- cancellation after native execution begins waits for terminal completion;
- completion racing cancellation chooses exactly one terminal result; and
- a losing cancellation attempt never frees request or buffer storage early.

The later public Task-cancellation contract may be stronger in composition,
but it cannot claim that libuv interrupts every executing operation.

## Generated runtime

Demand selects one program-wide pair:

```text
hexal/event.h
hexal/event.c
```

It owns the loop, command queue, callbacks, request base state, result
publication, and Task bridge. Public module headers expose no `uv_*` names.

Task, Channel, or Mutex selects `RuntimeLibuv`. Atomic alone does not.

### Internal observability

Use `uv_metrics_info`, `uv_metrics_idle_time`, loop configuration, and handle
walking to record:

- loop iterations, processed events, and waiting events;
- accumulated loop idle time;
- pending Hexal requests and worker-queue delay;
- active native handles at shutdown;
- native thread count and Task wake latency; and
- leaked handle identities in debug and external-validation builds.

These values are runtime diagnostics and benchmark data, not a stable Hexal
API. Ad hoc libuv handle-print functions remain debug-only because libuv does
not promise their output as a stable interface.

## Required sweep

After conformance is proven, remove:

- the complete `hex_blocking_*` pool;
- direct pthread and Windows thread wrappers replaced by `uv_thread_*`;
- native synchronization and once wrappers replaced by libuv;
- runtime `_Thread_local` storage replaced by `uv_key_t`;
- custom processor-count discovery; and
- any second event, native-work, or wakeup backend.

## Detailed implementation outline

1. Consume ADR 0145's embedded dependency and install ADR 0146's allocator
   before the first other libuv call.
2. Qualify and replace native threads, guards, TLS, affinity, naming, once, and
   processor discovery without altering Task semantics.
3. Add the single loop thread, command FIFO, and `uv_async_t` wakeup.
4. Add one typed request base and the common completion-to-Task transition.
5. Route genuinely blocking runtime work through `uv_queue_work`.
6. Prove immediate completion, delayed completion, failure, cancellation, and
   shutdown races.
7. Remove every superseded backend in the same change.
8. Enable loop metrics and add loop, queue-delay, request, thread-count,
   Task-wake, active-handle, and shutdown-leak measurements.

## Open design questions

1. Are requests stored on a parked fiber stack, in the Task, or in
   mimalloc-backed runtime storage?
2. What source-level result distinguishes successful best-effort cancellation
   from an operation that had already started or completed?
3. Does root completion wait for, cancel, or abandon detached native work?
4. Is libuv's global worker-pool size fixed by the runtime or configurable?
5. Which shutdown owner closes the loop after the last handle and request?

## Validation direction

The final exhaustive Validation section must cover Task progress under native
work saturation, exactly-once wakeup, publication ordering, callback isolation,
all terminal races, each `uv_cancel` outcome, no early request reclamation,
worker-zero/root affinity, clean loop shutdown, active-handle leak reporting,
metrics enablement and retrieval, dependency demand, absence of duplicate
native backends, and generated-C execution.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation updates only observable Task/runtime contracts.
