# RFC 0175: libuv Filesystem Watching

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: define Task-oriented file and directory change observation over
  libuv filesystem event and polling handles
- Depends on: ADR 0145, RFC 0168, RFC 0169, RFC 0170, and RFC 0171
- Does not add: compiler file watching or incremental compilation

## Summary

Use `uv_fs_event_t` and `uv_fs_poll_t` as the sole runtime backend for
filesystem observation. This is a language/runtime library facility; the Go
build driver's future watch mode is outside this RFC.

Conceptual source shape:

```hexal
watcher := try Watcher(path)
event := try watcher.next()
try watcher.close()
```

Exact names and event representation remain open.

## Semantic direction

- Waiting for an event parks the current Task.
- Events are delivered as typed values, never callbacks.
- Delivery has a bounded queue and an explicit overflow result.
- Rename and change notifications expose only portable guarantees.
- Recursive watching is available only on qualified targets where libuv
  supports it.
- Polling is a selected semantic fallback, not a hidden claim of identical
  event fidelity.
- Closing a watcher wakes its waiter with one defined terminal result.
- Watcher handles and queued path storage have explicit ownership.

## Libuv ownership

- `uv_fs_event_t` owns native event notification.
- `uv_fs_poll_t` owns explicitly selected polling observation.
- RFC 0171 timers own polling intervals and deadlines.

No direct ReadDirectoryChangesW, inotify, kqueue watcher, or FSEvents backend
coexists with the libuv abstraction.

## Detailed implementation outline

1. Settle Watcher, WatchEvent, event-kind, overflow, and close semantics.
2. Define when event watching versus polling is selected and observable.
3. Implement one bounded Task-facing event queue over RFC 0169.
4. Add close, cancellation, path storage, and root-shutdown ownership.
5. Add recursive mode only for explicitly qualified targets.
6. Delete every alternate native watcher backend.
7. Validate real create, modify, rename, remove, overflow, and shutdown cases.

## Open design questions

1. Is event coalescing observable, and what ordering is guaranteed?
2. What bounded capacity and overflow behavior apply?
3. Does an event contain a full path, a relative name, or both?
4. Is polling explicit in source or selected by target capability?
5. Is recursive watching omitted from the portable v1 contract?

## Validation direction

The final exhaustive Validation section must cover all event kinds, duplicate
and coalesced delivery, rename ambiguity, queue overflow, polling differences,
close/cancel races, path lifetime, recursive support, Task progress, shutdown,
and absence of target-native watcher code.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation adds only settled watcher contracts.
