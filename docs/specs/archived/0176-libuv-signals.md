# RFC 0176: libuv Signals

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented with one documented deviation beyond this RFC's
  own text. `Signals(subscriptions)`, `.next()`, and `.close()`, plus the
  `Signal` ADT (`Interrupt`/`Hangup`/`Terminate`), are in place over the
  shared generation-checked handle registry and its common libuv ErrorKind
  mapper, with the exact pending-bitset coalescing, declaration-order
  delivery, Busy/EoS/Closed linearization, and Windows Unsupported/
  best-effort rules this RFC specifies. Deviation: a collection specialized
  over `Signal` (List, Array, Slice, Dict, Pool) is routed to the consuming
  module's own header instead of the shared collection component, and is
  excluded from eager equality-helper generation, to avoid a circular
  header-inclusion conflict between `hexal/signal.h` and the early-positioned
  `hexal/slice.h`; bare `Signal == Signal` comparison is unaffected. See
  `docs/status.md`'s "Known coverage gaps" for the full account. Verified by
  `go test ./...`, `go vet ./...`, and the tagged C23 suite (including a real
  subscription racing a concurrent `close()` against a parked `next()`)
  running under GCC, Clang, and `zig cc`. `docs/reference.md` documents the
  public surface, the error table, and component selection including this
  deviation
- Created: 2026-09-13
- Updated: 2026-09-14
- Scope: define Task-oriented ordinary process-signal observation over libuv
- Depends on: ADR 0145, RFC 0168, RFC 0169, RFC 0181, and RFC 0180
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
fun wait_for_shutdown(): Signal | EoS | Error do
    wanted: Array<Signal, 2> := [Signal.Interrupt(), Signal.Hangup()]
    signals := try Signals(wanted.slice(0, wanted.length()))
    defer signals.close()
    signal := try signals.next()
    return signal
end
```

These are the settled names and result shapes. ADT variants use the current
capitalized call-shaped construction syntax.

## First implementation boundary

The first implementation observes ordinary process signals only. It does not
send signals, expose signal numbers, handle synchronous faults, or add Task
cancellation or deadlines. Reachable Signals construction, `next`, or `close`
selects the scheduler, event component, libuv, and native bootstrap; merely
constructing or matching a Signal variant selects only its type-definition
component. There is no synchronous non-Task wait.

Signals known to be uncatchable, unsafe through libuv, reserved by the native
thread/process runtime, or silently undeliverable on the selected target are
universally spellable but rejected by construction with Unsupported.
Registration never succeeds for an event that the qualified target cannot
deliver.

## Public surface

All names in this section are compiler-protected builtins. They remain
compiler-owned until a future standard-library split moves declarations out of
the compiler without changing their contracts.

```text
type Signal is Interrupt | Hangup | Terminate end

Signals(subscriptions: Slice<Signal>) -> Signals | Error
Signals.next()                        -> Signal | EoS | Error
Signals.close()                       -> Nil | Error
```

- Signal variants construct as `Signal.Interrupt()`, `Signal.Hangup()`, and
  `Signal.Terminate()`.
- Construction requires a non-empty, duplicate-free Slice and copies its
  values before returning. The Slice is not retained.
- The three variants are legal source on every target. Construction returns
  Unsupported when any requested variant cannot be delivered by libuv on the
  selected target. It never silently installs an inert watcher.
- Linux and macOS profiles support all three variants. The Windows profiles
  support Interrupt and Hangup; Terminate construction returns Unsupported.
  Windows Hangup delivery is best-effort: a console/window close may force
  process termination after a short platform-controlled cleanup interval.
- One `next()` may be active per Signals resource. A concurrent second call
  returns Busy without consuming a pending event.
- `next()` returns `eos` only after close and after any event already selected
  for that caller has been delivered.
- A `next()` already parked when close wins returns EoS. A new `next()` through
  an already closed handle returns Closed.
- `Signals.close()` is a valid `defer` and `errdefer` cleanup call.

`Signal` and `Signals` cannot be redeclared or shadowed. No target-specific
signal name or integer signal number is part of v1.

## Handle and allocation contract

`Signals` uses RFC 0180's generation-checked copied handle representation and
valid storage positions. Its control block, `uv_signal_t` watchers, pending
bits, and waiter state use the private mimalloc-backed runtime allocator.
Construction takes no source-level Heap.

## Semantic direction

- Registration and supported signals are explicit.
- Waiting parks the Task.
- Each subscription stores one pending bit per subscribed Signal. Repeated
  occurrences of one Signal coalesce while its bit is set; different Signals
  remain independently pending. `next()` clears and returns one pending Signal
  in stable Signal declaration order. Occurrence counts are not observable and
  overflow is impossible. Arrival order between different Signal variants is
  not preserved.
- No native signal handler executes Hexal work. The ordinary `uv_signal_t`
  callback runs on the libuv loop thread and may use the established runtime
  synchronization needed to publish an event and wake a Task. It performs no
  Hexal allocation and invokes no user code.
- Every active subscription for a Signal receives the event. This follows
  libuv's watcher routing and avoids an arbitrary process-global winner.
- Closing wakes waiters and releases the native handle exactly once.
- Platform-emulated signals are documented; unsupported signals reject rather
  than becoming watchers that never fire.
- Closing a subscription wakes its parked `next()` callers with `EoS` and
  releases every native watcher through its close callback exactly once.
- Event selection and close linearize under the same slot synchronization. If
  an event is selected first, the active waiter receives that Signal. If close
  wins first, it receives EoS. Callbacks after `closing` publish no event.
- Signal sending belongs to Process control or unsafe C interoperability, not
  this observation API.
- A signal claimed by Hexal cannot simultaneously be claimed by imported C
  code without an explicit unsafe interoperation contract.
- Closing the last Hexal watcher stops Hexal observation. Hexal does not promise
  to preserve or restore a foreign handler installed before its subscription;
  such composition requires future unsafe C-interoperation rules.
- Root completion performs no implicit subscription close. Users close Signals
  explicitly, normally through `defer`; remaining native state is abandoned to
  process termination under the existing runtime lifecycle.
- When no active Hexal subscription owns a signal, the platform's ordinary
  default disposition applies. Closing the last watcher does not promise to
  preserve or restore a handler installed by foreign code.

## Errors

Signal operations first apply RFC 0180's portable libuv ErrorKind mapper. Local
and fallback kinds are:

| Condition | ErrorKind |
| --- | --- |
| unsupported target/Signal combination | Unsupported |
| empty or duplicate subscription set | InvalidInput |
| another `next()` is active | Busy |
| closed subscription operation other than an already parked `next()` | Closed |
| other registration or observation failure | Other(header = `signal error`) |

Messages name the failed operation and follow RFC 0181's one settled diagnostic
detail representation. They contain no native signal number. Every Error
carries the Hexal call site's source location.

## Required sweep

No ordinary POSIX or Windows signal-observation backend exists today. Add only
the libuv path. Retain the POSIX alternate-stack `SIGSEGV`/`SIGBUS` guarded-
stack handlers and Windows vectored stack-overflow handler, with their
non-overlap stated adjacent to the code. Do not route their synchronous faults
through libuv.

## Detailed implementation plan

### Phase 1: types and target facts

1. Register Signal, its variants, Signals, and the exact operations above
   through the existing builtin registry.
2. Keep all three variants source-valid on every compilation target. Generated
   runtime code uses platform conditionals to reject an unsupported requested
   variant during construction; the host-neutral compiler does not guess a
   host platform.
3. Add checker rules for construction, calls, copied-handle storage, return
   unions, and exact local diagnostics.
4. Select the signal type-definition component from Signal value use. Select
   its C runtime, RFC 0180's handle component, event bridge, scheduler, libuv,
   and native bootstrap only from reachable Signals construction or methods.

### Phase 2: construction and watchers

1. Validate non-empty and duplicate-free subscriptions and target support
   before native registration.
2. Copy the subscription values into a fixed bitset in a mimalloc-backed
   control block; retain no source Slice.
3. Initialize one `uv_signal_t` per selected Signal on the loop owner.
4. On partial failure, stop and close every initialized watcher and publish no
   Signals handle.

### Phase 3: delivery and waiting

1. In each callback, set only the corresponding pending bit and wake the one
   active `next()` caller without allocation or user-code execution.
2. Make repeated same-Signal delivery coalesce while its bit is set.
3. Select and clear pending bits in Signal declaration order.
4. Return Busy for a second active `next()` without consuming state.
5. Use release-before-wake/acquire-after-resume ordering for every result.

### Phase 4: close

1. Stop and close every watcher through the loop owner exactly once.
2. Wake an active `next()` with EoS and invalidate every copied handle through
   RFC 0180.
3. Add no root-completion registry or implicit close path.
4. Keep synchronous guarded-stack handlers structurally separate.

### Phase 5: sweep and conformance

1. Verify no ordinary POSIX or Windows observation backend exists.
2. Add the exhaustive tests below and generated-C assertions for selection,
   fixed pending storage, callbacks, and native ownership.
3. Run ordinary Go tests, vet, and applicable C23 validation on every qualified
   target available to the harness.
4. Regenerate snippet hashes only for newly added Signal snippets; no existing
   entry may change.
5. Update `docs/reference.md` only during approved implementation and remove
   this RFC's status entry when it closes.

## Validation

Validation is exhaustive for this RFC.

- Signal and Signals reject redeclaration and shadowing; no other variant or
  raw signal-number API exists.
- Construction rejects an empty set and the first duplicate with InvalidInput,
  copies the Slice, and retains no pointer to it.
- Interrupt, Hangup, and Terminate register and deliver on Linux/macOS.
- Interrupt and Hangup register on Windows; Terminate returns Unsupported
  before installing any watcher.
- A partially failed construction closes every initialized watcher, frees its
  control block, and publishes no handle.
- Every active Signals resource subscribed to one occurrence receives it.
- Repeated occurrences of one already-pending Signal coalesce; distinct Signals
  remain pending and return in declaration order.
- Pending state is a fixed three-bit set; no event queue, occurrence counter,
  overflow path, or callback allocation exists.
- One `next()` parks only its Task; a concurrent second `next()` returns Busy
  and consumes nothing.
- Close wakes an active `next()` with EoS, invalidates every copy, and closes
  every watcher/control block exactly once. A later `next()` returns Closed;
  event-first and close-first races follow the exact linearization rule.
- Root completion adds no implicit close or resource-registry traversal.
- Callbacks run only runtime publication code on the loop thread and never run
  Hexal user code.
- Every common and signal-specific ErrorKind is exact, source-located, and
  target-independent.
- Existing POSIX guarded-stack and Windows vectored stack-overflow handlers
  remain present and are never routed through libuv.
- No ordinary `sigaction`, console-handler, or alternate observation backend is
  added.
- Reachability emits exactly one signal and handle component; absence emits
  none; public generated headers expose no `uv_*` type.
- Sending, raw numbers, synchronous faults, Task cancellation, deadlines, and
  foreign-handler coexistence are absent.
- `go test ./...`, `go vet ./...`, and applicable tagged C23 validation pass.

## Reference synchronization

Do not edit `docs/reference.md` from this draft proposal. An approved
implementation adds only settled ordinary-signal contracts.
