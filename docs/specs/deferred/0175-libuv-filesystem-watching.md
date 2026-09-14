# RFC 0175: libuv Filesystem Watching

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; no current consumer and portable event semantics remain unsettled
- Created: 2026-09-13
- Updated: 2026-09-14
- Scope: define Task-oriented file and directory change observation over libuv filesystem event and polling handles
- Depends on: RFC 0168 and the implemented RFCs 0169, 0170, 0171, 0180, and 0181
- Does not add: compiler file watching or incremental compilation

## Summary

Use `uv_fs_event_t` and `uv_fs_poll_t` as the sole runtime backends for an event watcher and an explicitly selected polling watcher. This is a future language/runtime library facility; the Go build driver's future watch mode is outside this RFC.

The proposal remains deferred until a real Hexal program requires it. On return, it must be rewritten around the current handle registry, ErrorKind, Task bridge, and allocation contracts before implementation.

## Constraints preserved for reconsideration

- Libuv portably reports only `Rename` and `Change`; it cannot promise distinct create, modify, remove, rename-from, or rename-to events.
- A callback filename is relative when available and may be absent.
- Event coalescing and duplicate delivery are platform behavior.
- Recursive event watching is not portable across Hexal's target matrix.
- `uv_fs_poll_t` owns its own interval and metadata-difference behavior; it is not a transparent fallback for `uv_fs_event_t`.
- A long-lived watcher must use the program-wide generation-checked handle registry.
- Callback-owned path bytes need a bounded private runtime queue. A public owned path result needs an explicit Heap, likely `watcher.next(heap: Heap) -> WatchEvent | Error`.
- Queue capacity, overflow, close/wake, single-versus-multiple waiter, and path ownership rules remain open.
- No alternate native watcher backend currently exists to remove.

## Re-entry condition

Promote this RFC from `deferred/` only with a concrete non-build-driver consumer. The promoted RFC must provide a complete source API, exhaustive Validation section, detailed implementation plan, and explicit reference synchronization.
