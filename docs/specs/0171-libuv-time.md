# RFC 0171: Time and Task Sleep

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implementation-ready; implementation not started
- Created: 2026-09-13
- Updated: 2026-09-13
- Scope: add duration, monotonic measurement, wall time, and Task sleep
- Depends on: RFC 0145, RFC 0168, and RFC 0169
- Does not add: async syntax, callbacks, deadlines on other operations,
  timeouts, repeating timers, calendar arithmetic, parsing, formatting, or time
  zones

## Summary

The first time surface is deliberately narrow:

- one `Duration` value;
- one monotonic `Instant` value;
- one wall-clock `WallTime` value; and
- `Task.sleep`.

Deadlines, timeout races, and repeating delivery are separate features. They
must not enlarge this RFC before a concrete Channel, network, or filesystem API
needs them.

## Backend ownership

Apply RFC 0168's C23-first selection rule:

- monotonic measurement uses `uv_hrtime()`;
- Task sleep uses one-shot `uv_timer_t` on the existing loop-owner thread;
- wall time uses C23 `timespec_get(TIME_UTC)` because it provides the required
  synchronous wall-clock contract directly;
- `uv_now()` remains private cached loop time and is not a second public clock;
  and
- `uv_sleep()` is never called on a scheduler worker.

`Instant.now()` selects RFC 0169's native bootstrap, libuv, and mimalloc, but
not the scheduler or event component. `WallTime.now()` alone selects none of
them. `Task.sleep()` selects the bootstrap, scheduler, and event component.

No custom timer thread, timer wheel, clock portability layer, or sleep backend
exists to remove. RFC 0169 removes the current unreachable speculative timer
adapter before this RFC adds one typed one-shot request implementing only the
contract below.

## Proposed values

### `Duration`

- A distinct protected scalar value containing an unsigned 64-bit nanosecond
  count.
- It is not a transparent alias of `UInt64`; units must not mix accidentally.
- `==`, `!=`, `<`, `<=`, `>`, and `>=` compare nanoseconds.
- `Duration + Duration -> Duration` traps on overflow.
- `Duration - Duration -> Duration` traps when the right operand exceeds the
  left. Duration multiplication and division operators do not exist.
- Constructors convert an unsigned count of nanoseconds, microseconds,
  milliseconds, or seconds using checked arithmetic.
- Converting a duration to a coarser unit truncates toward zero.

Conceptual surface:

```text
Duration.nanoseconds(value: UInt64)  -> Duration
Duration.microseconds(value: UInt64) -> Duration
Duration.milliseconds(value: UInt64) -> Duration
Duration.seconds(value: UInt64)      -> Duration
Duration.as_nanoseconds()             -> UInt64
Duration.as_microseconds()            -> UInt64
Duration.as_milliseconds()            -> UInt64
Duration.as_seconds()                 -> UInt64
```

These are compiler-owned associated operations, not permission for user-
defined static methods.

### `Instant`

- A distinct protected monotonic timestamp.
- Its numeric representation and origin are not observable.
- It cannot be constructed from an integer, serialized, or converted to wall
  time.
- Ordering is valid only between values from the same process.
- `Instant - Instant -> Duration` requires the left operand not precede the
  right. No other arithmetic operator accepts Instant.

Conceptual surface:

```text
Instant.now() -> Instant
Instant.elapsed() -> Duration
Instant.duration_since(earlier: Instant) -> Duration
```

`duration_since` requires the receiver not precede `earlier`; violation traps
as an invalid time operation rather than wrapping.

Exact runtime diagnostics are:

```text
[Runtime Error] duration overflow
[Runtime Error] duration underflow
[Runtime Error] invalid instant subtraction
```

### `WallTime`

- A distinct protected UTC timestamp consisting of signed whole seconds since
  the Unix epoch plus a nanosecond fraction in `0..999_999_999`.
- It is for observation and interchange, never elapsed-time measurement.
- This RFC adds no local time, time-zone, calendar, parsing, or formatting API.
- Equality and ordering compare seconds, then the nanosecond fraction. WallTime
  has no arithmetic operators.

Conceptual surface:

```text
WallTime.now() -> WallTime | Error
WallTime.seconds() -> Int64
WallTime.nanosecond() -> UInt32
```

Wall-clock acquisition failure returns an owned `Error`.

- Its header is the static Strand `time unavailable`.
- Its message is the static-backed String `wall clock acquisition failed`.
- The failure path performs no allocation and exposes no native code.

## Task sleep contract

Conceptual surface:

```text
Task.sleep(duration: Duration) -> no value
```

- Calling `Task.sleep` selects the Task scheduler, libuv, and the event
  component; source execution therefore always has a current Task.
- Zero duration is a no-op. It is not a scheduling point and does not replace
  the explicit `Task.yield()` required by a task-reachable `while true` path.
- A positive duration greater than `Int64` maximum nanoseconds traps with
  `[Runtime Error] sleep duration too large\n`. This bound is about 292 years;
  it constrains sleep, not Duration construction or arithmetic.
- Positive duration parks only the current Task. No scheduler worker blocks.
- Convert nanoseconds to libuv's integer milliseconds by ceiling division.
  Compute quotient and remainder rather than adding 999,999 to a value that
  may be `UInt64` maximum.
- Record the monotonic start before arming the timer. On callback, compute
  elapsed nanoseconds as unsigned `uv_hrtime() - start`; the accepted duration
  is below half the counter range, so counter wrap cannot reverse the
  comparison. If elapsed is shorter than requested, re-arm for the rounded-up
  remainder rather than returning early. Never form `start + duration`.
- Sleep promises only that completion is not earlier than the requested
  duration. Scheduler load and host timer resolution impose no finite upper
  lateness bound.
- The loop-owner thread alone initializes, starts, stops, and closes the timer.
- On final expiry, the timer callback initiates `uv_close` and does not wake the
  Task. The close callback is the terminal owner: it publishes completion,
  wakes the Task exactly once, and never runs user code.
- Timer and request storage remain live through that close callback and are
  reclaimed exactly once before the resumed Task can leave the owning frame.
- Runtime initialization or timer submission failure traps with a stable
  Runtime Error. Sleep has no ordinary recoverable failure result.
- The exact sleep failure is `[Runtime Error] task sleep failed\n`. Native
  bootstrap failure retains RFC 0169's allocator-installation diagnostic.
- Root completion retains the existing detached-Task contract. Process exit may
  abandon a detached sleeping Task; this RFC does not make root wait for it.

## Why deadlines and repeating timers are deferred

A deadline on Channel, filesystem, DNS, or socket work races two owners:

```text
operation completion -> remove timer -> wake Task
timer expiry         -> remove operation waiter/request -> wake Task
```

That needs an operation-specific unlink/cancel protocol and a settled timeout
result. It cannot be derived safely from sleep alone. Each consuming RFC owns
its race, result type, and cancellation mechanism; RFC 0171 provides only the
clock and one-shot timer foundations it needs.

Repeating libuv timers do not compensate for callback execution time. A public
repeating abstraction would need drift, missed-tick, buffering, cancellation,
and shutdown contracts. It is omitted until a concrete use case justifies that
surface; a future design should prefer delivery through an existing Channel
over adding another callback abstraction.

## Settled decisions

- `Duration` is unsigned. It represents a magnitude; `Instant` ordering
  determines direction. Negative construction and negative sleep do not exist.
- `Task.sleep(Duration)` returns no value. Runtime initialization or timer
  submission failure uses the stable Runtime Error path rather than adding
  `try` to ordinary sleep.

## Implementation plan

This plan is executable in the listed order.

### Phase 1: types and checking

1. Add protected `Duration`, `Instant`, and `WallTime` identities.
2. Register only the operations listed above.
3. Implement checked unit conversion and time arithmetic.
4. Reject construction, conversion, or arithmetic not explicitly admitted.
5. Treat `Task.sleep` as a visible blocking operation but not as satisfying the
   explicit-yield rule.
6. Add focused compiler files following existing phase ownership: type
   identities in `compiler/types`, call/type checking beside the nearest
   protected builtins, lowering in `compiler/generator`, and public-pipeline
   cases in `compiler/tests/integration/time_test.go`.

### Phase 2: C representation and clocks

1. Lower Duration to `uint64_t` nanoseconds.
2. Lower Instant to a private monotonic `uint64_t` tick value.
3. Lower WallTime to signed seconds plus a normalized nanosecond field.
4. Implement monotonic acquisition through `uv_hrtime()`.
5. Implement wall time through `timespec_get(TIME_UTC)` with checked conversion.
6. Add a demand-driven `hexal/time.h` and `hexal/time.c`; expose no libuv name
   from the header.

### Phase 3: one-shot Task timer

1. Add one private timer request after RFC 0169's speculative timer removal;
   do not restore its generic timer API.
2. Add demand-gated submission through the existing event FIFO.
3. Implement ceiling conversion, monotonic start recording, and early-wake
   re-arming using elapsed subtraction; reject sleep durations above the
   stated bound before submission.
4. On final expiry, close the timer on the loop thread without waking the Task.
5. From the close callback, publish completion and wake through the common Task
   transition, then let the resumed Task reclaim its request exactly once.
6. Map initialization and submission failure to the stable trap.

### Phase 4: conformance and demand

1. Verify time-only code selects the exact required runtime components.
2. Verify programs without time or concurrency retain unchanged artifacts.
3. Run generated C for zero, sub-millisecond, and ordinary durations. Exercise
   maximum accepted, first rejected, overflow, and early-wake boundaries
   through an injected internal clock/timer seam; never make a test wait for a
   boundary-scale real duration.
4. Measure early/late completion, high timer counts, allocations, and event-loop
   progress while scheduler workers execute unrelated Tasks.
5. Exercise root and detached-Task process-exit behavior without adding joins.

### Phase 5: synchronization

1. Regenerate only legitimately changed snippet manifest entries.
2. Update `docs/reference.md` once after behavior stabilizes and with explicit
   user approval.
3. Record deadlines, timeouts, and repeating timers as separate unscheduled
   work only when a consuming feature needs them.

## Validation

This section is exhaustive.

### Types and checking

- `Duration`, `Instant`, and `WallTime` are protected and cannot be redeclared.
- Duration constructors accept exactly one `UInt64`; no implicit numeric
  argument conversion is added.
- Nanosecond construction preserves `0` and `UInt64` maximum exactly.
- Microsecond, millisecond, and second construction traps when scaling is not
  representable and succeeds at each exact boundary.
- Duration unit accessors truncate toward zero.
- Duration equality, ordering, addition, and subtraction follow the rules
  above; underflow and overflow use exact runtime diagnostics.
- Duration multiplication/division, integer mixing, and conversion through
  numeric `.to<T>()` are rejected.
- Instant cannot be constructed, converted, serialized, or mixed
  arithmetically with Duration or numeric values.
- Instant ordering and subtraction are accepted; reversed subtraction traps.
- WallTime exposes only `now`, `seconds`, `nanosecond`, equality, and ordering;
  arithmetic and conversion to/from Instant are rejected.

### Generated C

- Duration lowers to `uint64_t`; Instant lowers to a private `uint64_t` value;
  WallTime lowers to `int64_t` seconds plus normalized `uint32_t` nanoseconds.
- Monotonic acquisition calls `uv_hrtime()` exactly once per `Instant.now()`.
- Wall acquisition uses `timespec_get(TIME_UTC)` and no libuv or native wall-
  clock alternative.
- No public generated header contains `uv_*` types or names.
- Nanosecond-to-millisecond conversion uses quotient/remainder ceiling and does
  not overflow at the maximum accepted sleep duration.
- Sleep rejects values above `Int64` maximum nanoseconds before submission.
- A positive sleep records its monotonic start before timer submission, uses
  unsigned elapsed subtraction, and never forms an absolute target by adding
  the duration.
- Timer initialization, start, stop, and close occur only on the loop-owner
  thread.
- Final expiry does not wake the Task. The close callback publishes before
  exactly one Task wake; request storage survives through that callback.
- No scheduler worker calls `uv_sleep`, `uv_run`, or another blocking timer
  wait.

### Runtime behavior

- Zero sleep returns without submitting a timer and does not satisfy the
  explicit-yield checker rule.
- Durations from one nanosecond through 999,999 nanoseconds request at least one
  millisecond and do not complete early.
- Exact millisecond durations are not rounded to an additional millisecond.
- A deliberately early wake re-arms for the remaining rounded-up interval.
- Ordinary and maximum accepted durations publish one completion and reclaim
  timer state once under the internal clock/timer seam; the first larger value
  traps without submitting a timer.
- Timer initialization/submission failure emits the exact stable Runtime Error
  and never leaves the Task parked.
- Wall-clock acquisition failure returns exactly the specified static Error
  header and message without allocation.
- Unrelated Tasks progress while one or many Tasks sleep.
- Root may sleep and resume on worker zero. Root completion does not wait for a
  detached sleeping Task.
- Wall-clock movement does not alter elapsed-time measurement or sleep.

### Demand and regression

- `Task.sleep` selects the scheduler, libuv, and the event pair.
- `Instant.now` selects the minimal libuv dependency required by its
  implementation: native bootstrap, libuv, and mimalloc, but neither scheduler
  nor event pair. `WallTime.now` alone selects none of those dependencies.
- The root calls the native bootstrap before `Instant.now()` or scheduler
  initialization, and no libuv allocator installation remains scheduler-only.
- A program using none of these types or operations has byte-identical
  generated artifacts.
- RFC 0169's removed speculative timer surface does not reappear wholesale;
  only the typed one-shot contract used by Task sleep is emitted.
- Ordinary Go tests remain independent of an external toolchain.
- Tagged generated-C tests compile and run the zero, sub-millisecond, ordinary,
  boundary, overflow, root, detached, and concurrent-sleeper fixtures under
  every qualified toolchain.
- The snippet manifest changes only for newly added time snippets; no existing
  entry changes unless this RFC intentionally changes its generated artifact.

## Reference synchronization

Do not edit `docs/reference.md` from this spec without explicit user approval.
An approved implementation adds
only the settled Duration, Instant, WallTime, and Task sleep contracts.
