# RFC 0171: libuv Time, Timers, and Deadlines

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: define portable monotonic time, wall time, Task sleep, timers,
  deadlines, and timeouts over libuv
- Depends on: ADR 0145, RFC 0168, and RFC 0169
- Does not add: async syntax or callback-based timers

## Summary

Use libuv as the sole time backend wherever it has a matching facility:

- `uv_hrtime()` for high-resolution monotonic measurement;
- loop time and `uv_update_time()` where event-loop time is appropriate;
- `uv_timer_t` for Task sleep, deadlines, and timeouts; and
- `uv_gettimeofday()` for wall-clock time.

User code remains direct:

```hexal
try Task.sleep(250.milliseconds())
value := try channel.receive(before = deadline)
```

The syntax and types above are conceptual, not decisions.

## Semantic direction

- Sleeping parks only the current Task.
- A timer callback stores completion before waking the Task.
- Zero duration completes without an unnecessary native timer.
- Monotonic time governs elapsed time, sleep, deadlines, and timeouts.
- Wall time is never used to measure duration because it may jump.
- Deadline expiry and operation completion choose exactly one winner.
- Repeating timers do not execute user callbacks on the loop thread; they
  publish events or wake Tasks through a typed Hexal abstraction.
- Timer handles are closed and reclaimed exactly once.

## Required sweep

Remove every custom sleep thread, timer queue, clock portability branch, and
native time wrapper whose operation libuv supplies. Retain C facilities only
for a required time representation conversion, not as another clock backend.

## Detailed implementation outline

1. Settle Duration, monotonic Instant, wall-clock value, and conversion rules.
2. Map each value to the exact libuv clock and define overflow bounds.
3. Add one timer request over RFC 0169's Task bridge.
4. Implement sleep, then shared deadline/timeout arbitration.
5. Add repeating delivery only after its buffering and cancellation semantics
   are settled.
6. Delete replaced clock and sleep implementations.
7. Validate drift, ordering, cancellation races, shutdown, and high timer
   counts in generated programs.

## Open design questions

1. What are the names, units, widths, and arithmetic rules of Duration,
   monotonic Instant, and wall time?
2. Are deadlines accepted directly by operations or composed through a common
   timeout mechanism?
3. Is Task sleep fallible only during runtime failure, or does it return no
   value?
4. How are repeating timer events buffered and cancelled?
5. What precision is promised across targets?

## Validation direction

The final exhaustive Validation section must cover monotonic behavior, wall
time separation, zero and maximum durations, timer ordering, timeout races,
exactly-once wakeup, cancellation, shutdown, no blocked scheduler worker, and
absence of a second time backend.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation adds only settled public time contracts.
