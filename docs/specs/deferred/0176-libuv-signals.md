# RFC 0176: libuv Signals

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: define Task-oriented ordinary process-signal observation over libuv
- Depends on: ADR 0145, RFC 0168, and RFC 0169
- Coordinates with: RFC 0173 for child-process signalling
- Does not replace: guarded-stack overflow recovery or synchronous-fault
  handling

## Summary

Use `uv_signal_t` as the sole backend for ordinary process signals that libuv
supports safely. Deliver signals as typed Task events, never as user callbacks
on the event-loop thread.

Synchronous faults remain outside libuv. Stack overflow, segmentation faults,
illegal instructions, and equivalent process-corruption paths retain the
smallest native handler required for runtime safety because libuv does not
provide a safe matching contract.

Conceptual source shape:

```hexal
signals := try Signals([Signal.interrupt, Signal.terminate])
signal := try signals.next()
try signals.close()
```

Exact names remain open.

## Semantic direction

- Registration and supported signals are explicit.
- Waiting parks the Task.
- Delivery uses a bounded typed queue with defined coalescing and overflow.
- No signal callback performs allocation, locking, or user work outside what
  libuv itself permits.
- Multiple subscribers have one settled routing policy.
- Closing wakes waiters and releases the native handle exactly once.
- Platform-emulated and unsupported signals are reported honestly.

## Required sweep

Remove ordinary POSIX `sigaction`, Windows console-handler, and other signal
paths replaced by `uv_signal_t`. Retain synchronous-fault and alternate-stack
handlers, with their non-overlap stated adjacent to the code.

## Detailed implementation outline

1. Settle Signal identities, subscription, delivery, overflow, and close.
2. Build one signal-handle adapter over RFC 0169.
3. Implement registration, event publication, cancellation, and shutdown.
4. Separate ordinary signals structurally from synchronous runtime faults.
5. Delete replaced ordinary-signal platform paths.
6. Validate each supported and unsupported target case in generated programs.

## Open design questions

1. Does one signal go to one subscriber, every subscriber, or one
   process-global receiver?
2. What coalescing and queue-overflow behavior is guaranteed?
3. Which signals are portable names and which are target-specific?
4. How does signal delivery compose with Task cancellation and root shutdown?
5. Is sending a signal exposed here or only through Process and unsafe C
   interoperability?

## Validation direction

The final exhaustive Validation section must cover registration, delivery,
coalescing, overflow, multiple subscribers, unsupported signals, close and
shutdown races, callback isolation, retained synchronous-fault handling, and
absence of an ordinary alternate signal backend.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation adds only settled ordinary-signal contracts.
