# RFC 0091: Blocking-Syscall Boundary

- Kind: Feature Specification (Rust-Style RFC)
- Status: Discarded; superseded by RFC 0108's synchronous descriptor contract
- Created: 2026-08-19
- Closed: 2026-08-24
- Scope: scheduler behavior while a native operation blocks an OS worker

## Decision

Do not add scheduler-aware blocking machinery now.

Retain the implemented RFC 0108 contract:

- `IO` operations use synchronous native descriptor or handle calls.
- A descriptor operation may block its current OS worker.
- Other scheduler workers continue running.
- If every worker blocks, ready Tasks wait until one native call returns.
- `IO` signatures expose no scheduler parameter, blocking mode, or
  `would block` result.
- Programs that use no Tasks acquire no scheduler solely because they use IO.

`docs/reference.md` already records this behavior. Discarding this RFC requires
no language-reference change.

## The underlying issue remains real

Hexal uses cooperative M:N scheduling. `Task.yield()` explicitly requeues the
current Task and switches its fiber back to the worker dispatcher. A synchronous
native `read`, `write`, or equivalent operation performs no such switch. If the
native call blocks, its entire OS worker blocks.

Yielding before an operation only gives another Task one opportunity to run.
When the yielding Task resumes and enters the native call, that worker can still
block. With N workers, fewer than N blocked calls reduce parallel capacity; N
blocked calls stop all remaining ready Tasks.

Channel waits, Mutex waits, and Task joins are different: their runtime knows
the Task is waiting, marks it parked, and switches back to the dispatcher.

This is a documented scheduling limitation, not undefined behavior and not an
unimplemented part of the current IO surface.

## Why this RFC was discarded

### RFC 0108 settled the source surface

The original RFC treated blocking behavior as a prerequisite for files and byte
streams and considered a caller-supplied scheduler/IO policy. RFC 0108 has since
implemented byte streams and deliberately made scheduler integration an
internal backend concern. No source-visible decision remains here.

### The current backend is synchronous by construction

`IO` stores only a native descriptor or handle, access bits, and ownership
state. It stores no pollability class, nonblocking-mode ownership, completion
record, waiter, or asynchronous operation state. Standard handles are borrowed
and were not necessarily created for nonblocking or overlapped operation.

A readiness or completion backend would therefore be a new runtime subsystem,
not a small lowering change.

### One portable mechanism does not cover the current targets

- POSIX readiness APIs do not make regular-file IO asynchronous.
- Changing a borrowed POSIX descriptor to nonblocking mode can affect other
  process users of the same open file description.
- Windows completion IO generally requires handles created for overlapped
  operation; borrowed standard handles do not promise that property.
- A blocking pool works for synchronous handles but adds threads, queues,
  scheduler wakeups, and lifetime coordination to every relevant operation.

### The old scope became stale

The original RFC referred to RFC 0065 as blocked work, although RFC 0108
superseded and implemented that IO family. It also omitted the implemented
`print` backend, which now shares raw stdout descriptor transfer and can block a
worker too.

### The repository cannot verify the critical behavior

Ordinary tests do not compile or execute generated C. Text assertions could
show that an operation enqueues or parks, but cannot prove that:

- unrelated Tasks continue after every worker would otherwise block;
- completion wakes exactly one Task;
- close and completion cannot race;
- the poller or blocking pool remains live; or
- platform-specific native behavior matches the generated protocol.

Adding a large scheduler subsystem before the build driver can execute these
properties would create high-risk, weakly verified runtime code.

## Rejected implementation directions

### Full poller plus blocking pool now

This would eventually provide the strongest scaling model: readiness or
completion parking for pollable handles and a bounded blocking pool for
synchronous operations. It is rejected now as premature platform architecture.
Hexal has no filesystem-opening or socket surface that demonstrates the need,
and current borrowed handles do not carry the metadata the design requires.

### One replacement thread per blocked operation

This preserves scheduler progress but lets native thread count grow with the
number of blocked calls, defeating the bounded-thread purpose of M:N Tasks.

### Treat `Task.yield()` as sufficient

Rejected because a yield occurs before or after a native call, never while the
OS thread is blocked inside it.

### Count IO as the required yield in `while true`

Rejected because an IO operation may complete immediately. A loop repeatedly
receiving immediate completion or `EoS` would still monopolize its worker.

## Future trigger

Create a new, narrower scheduler-aware-native-operations RFC only when at least
one of these is true:

- a filesystem surface introduces concurrent regular-file workloads;
- a socket or pipe surface requires scalable waiting;
- profiling shows worker loss from current IO or `print` is material; or
- the build driver can execute generated scheduler conformance tests.

That future RFC starts from the implemented representation rather than this
historical option inventory. It must:

- preserve existing `IO` source signatures;
- include `print` and every other shared descriptor-transfer path;
- activate scheduler machinery only when concurrency and a blocking native
  component are both reachable, unless measurement justifies otherwise;
- specify how constructors establish pollability or completion metadata;
- handle borrowed synchronous standard handles explicitly;
- separate synchronous offload from socket readiness/completion when the
  targets require it; and
- provide runnable validation for progress and exactly-once wakeup before
  claiming implementation completeness.

## Reference synchronization

None. The existing reference rule that descriptor operations may block their OS
worker remains the current language contract.
