# RFC 0180: libuv Handle and Error Foundation

- Kind: Architecture Decision Record (ADR)
- Status: Closed; implemented. The shared generation-checked handle registry
  (`hexal/handle.h`/`hexal/handle.c`), its reserve/publish/resolve/release/
  close-begin/close-finish lifecycle, mimalloc-backed private allocation, and
  the common libuv ErrorKind mapper are in place; File is migrated onto the
  common handle representation with its public operations and semantics
  unchanged, and now occupies every ordinary complete-value position instead
  of IO's more restricted shallow-copy-only set. Verified by `go test ./...`,
  `go vet ./...`, and the tagged C23 suite compiling and running under GCC,
  Clang, and `zig cc`. `docs/reference.md` documents the copied-handle
  lifecycle, generation check, valid storage positions, absence of
  equality/hash/print, private allocation rule, and portable ErrorKind
  mapping
- Created: 2026-09-14
- Scope: define the common source representation, runtime lifetime, allocation,
  and Error mapping used by long-lived libuv-backed values
- Depends on: ADR 0145, RFC 0181, and closed RFCs 0146, 0169, and 0170
- Blocks: RFCs 0172, 0173, and 0176
- Does not add: a public allocator, raw `uv_*` access, automatic cleanup,
  equality, ordering, printing, or a second event backend

## Decision

Long-lived libuv-backed values use a generation-checked runtime slot. Their
control blocks use the private mimalloc-backed runtime allocator. All libuv
capabilities use one portable `ErrorKind` mapper before applying a
capability-specific `ErrorKind.Other` fallback.

This RFC owns the common mechanism only. Child RFCs own public types,
operations, capability-specific state, and fallback ErrorKind headers.

## Source contract

The common contract initially applies to:

- the existing libuv-backed File capability;
- TCP connections and listeners;
- child processes and child standard-stream pipes; and
- signal subscriptions.

Each public handle is an ordinary copyable value containing an opaque slot
identity and generation. Copies name one external resource and observe one
shared lifecycle.

- A live copy may occupy every ordinary complete-value position: bindings,
  parameters, results, structs, ADT payloads, unions, Arrays, Slices, Lists,
  Dict values, pointer pointees, Heap/Stash/Pool allocations, Task
  arguments/results, and Channels.
- A handle is not a Dict key and has no language equality, ordering, hash, or
  print contract.
- Closing through one copy makes every copy closed.
- A locally provable operation through a closed copy is a check error.
- Escaped or stale misuse returns the owning operation's `closed` Error; it
  never dereferences freed control storage.
- Copying does not clone the native resource and does not transfer exclusive
  ownership.

The public representation exposes no libuv type, pointer, request, callback,
or numeric error code.

File is the first public consumer implemented by this RFC. Its existing source
operations and results do not change, but its representation and valid storage
positions become the common copied-handle contract above. A later child RFC
therefore does not introduce a second storage rule merely because its handle is
network- or process-backed.

## Runtime representation

Conceptually:

```c
typedef struct hex_handle_slot hex_handle_slot;

typedef struct {
    hex_handle_slot *slot;
    uint64_t generation;
} hex_handle;
```

The exact private names may follow the generator's current naming rules. The
following representation facts are normative:

- Slot addresses remain valid until process termination.
- A slot contains a generation, lifecycle state, capability kind, capability
  control-block pointer, and the synchronization required by that slot.
- Capability control blocks contain the concrete `uv_*` handle and operation
  state. They remain at a stable address until libuv's final callback permits
  release.
- The slot registry grows in stable-address chunks. Registry growth may move a
  chunk directory but never a published slot.
- Closed slots may be reused only after the final native callback, every
  in-flight operation, and every parked waiter have released the old
  generation.
- Reuse increments the generation before publication. A generation that would
  wrap is retired permanently; the registry grows instead.
- A handle resolves only when its slot is live, its generation matches, and
  its capability kind matches the requested operation.
- The registry lock protects only growth and free-slot selection. Ordinary
  operations synchronize through the resolved slot and do not take one
  program-wide lock.
- Every slot owns one private mutex and an in-flight operation count. Resolving
  a handle locks the slot, verifies `live`, generation, and capability kind,
  increments the count, and returns a private operation lease before unlocking.
  Releasing the lease decrements the count under the same mutex.
- Close locks the slot and linearizes at `live -> closing`. It prevents new
  leases immediately and invalidates every public copy. Native close and final
  storage release may complete later.
- The final callback may recycle or retire a slot only after its native handle,
  parked waiters, and in-flight count have all reached their terminal state.
  Result payloads are written before a waiter is published ready.

This makes a copied handle cheap while preventing use-after-free after a slot
is recycled for another resource.

## Lifecycle

Each slot has these public lifecycle states:

```text
free -> opening -> live -> closing -> free-or-retired
```

- Construction reserves a slot, allocates a control block, and asks the libuv
  loop owner to initialize the native handle.
- Construction publishes the source handle only after successful native
  initialization.
- Failure before publication releases the control block and returns the slot
  without exposing a handle.
- The first close changes `live` to `closing`. That transition is successful
  close completion at the public API: every copy is invalid immediately, while
  native cleanup may finish asynchronously. Later close or operation calls
  observe `closed`.
- Only the libuv loop-owner thread calls `uv_close` or otherwise mutates an
  ordinary libuv handle, except where libuv explicitly documents a thread-safe
  operation.
- Closing wakes every parked operation for that resource with its
  capability's closed result.
- Operation payload and Error storage are written before the Task wake is
  published.
- The final callback releases the control block, clears capability state, and
  only then recycles or retires the slot.
- A child RFC may retain private native state after public close when the OS
  resource still requires completion or reaping. The public slot remains
  closed throughout that retention.

No finalizer, destructor, public reference count, ownership type, or implicit
close is introduced. The private in-flight count is operation-lifetime
bookkeeping; it neither counts public handle copies nor keeps a publicly closed
resource live.

## Allocation

- Registry chunks, slots, capability control blocks, and private request state
  use the runtime's mimalloc backend.
- Their constructors take no source-level `Heap`.
- Allocation failure returns Error and publishes no partial handle. Its message
  is a fixed static operation message under RFC 0181; reporting an exhausted
  allocator must not require another allocation.
- A child operation that materializes an owning Hexal collection or String
  still receives the caller's `Heap` for that result.
- Callback paths use only storage reserved before native submission unless a
  libuv contract explicitly requires callback-owned allocation and the child
  RFC specifies its terminal owner.

## Common libuv Error mapping

One runtime function maps libuv status codes to stable portable `ErrorKind`
values. It returns no mapping for an unknown or capability-specific code; the
child RFC then uses its named `ErrorKind.Other` fallback.

Required common categories:

| libuv condition | Hexal ErrorKind |
| --- | --- |
| `UV_ENOENT` | NotFound |
| `UV_EACCES`, `UV_EPERM` | PermissionDenied |
| `UV_EEXIST` | AlreadyExists |
| `UV_EINVAL` | InvalidInput |
| `UV_ENOMEM`, `UV_ENOBUFS`, `UV_EMFILE`, `UV_ENFILE` | ResourceExhausted |
| `UV_ENOSYS`, `UV_ENOTSUP` | Unsupported |
| `UV_ECANCELED` | Cancelled |
| `UV_EINTR` | Interrupted |
| `UV_ETIMEDOUT` | TimedOut |
| `UV_EADDRINUSE` | AddressInUse |
| `UV_EADDRNOTAVAIL` | AddressUnavailable |
| `UV_ECONNREFUSED` | ConnectionRefused |
| `UV_ECONNRESET` | ConnectionReset |
| `UV_ECONNABORTED` | ConnectionAborted |
| `UV_EHOSTUNREACH` | HostUnreachable |
| `UV_ENETUNREACH` | NetworkUnreachable |
| `UV_EPIPE` | BrokenPipe |
| `UV_ENOTCONN` | NotConnected |

The mapper returns classification only. The caller constructs Error with the
current Hexal source location, the mapped kind or capability fallback, and the
operation message representation settled by RFC 0181. It exposes no
`uv_err_name`, `uv_strerror`, errno, Win32 error, or libuv integer as a
language classification value.

Existing File mapping is reconciled to consult this common mapper for shared
conditions while retaining File-specific categories and deliberate contextual
overrides. No duplicate libuv switch remains in a child component.

## Generated components

- Reachable use selects exactly one dependency-neutral `hexal/handle.h` and
  `hexal/handle.c` pair.
- The header contains only the common opaque handle/slot declarations needed
  by generated capability headers.
- The C file owns slot-registry growth, resolution, generation checks,
  lifecycle transitions, and the common Error mapper.
- The component depends on the event bridge, runtime Error/String support, and
  mimalloc integration already selected by reachable libuv use.
- Child headers define distinct source-visible wrapper types so Process, Pipe,
  Signals, TcpListener, and TcpConnection never become interchangeable.

## Required sweep

- Do not copy the slot registry, generation check, close-state machine, or
  common Error switch into networking, process, pipe, or signal components.
- Reconcile the existing File libuv Error mapper with the common mapper rather
  than retaining two overlapping mappings.
- Do not expose one shared untyped handle as a source-level escape hatch.
- Do not retain a raw control-block pointer as the sole public lifetime check.

## Accepted costs

- Every allocated slot carries one private mutex plus generation, lifecycle,
  kind, control pointer, waiter state, and an in-flight count. This is bounded
  per slot and buys a simple auditable resolve/close protocol; it is not a
  zero-metadata raw-handle representation.
- Registry chunks may remain allocated until process termination. Root return
  performs no global traversal solely to recover process-lifetime metadata.

## Detailed implementation plan

### Phase 1: shared checked representation

1. Add the demand-driven `handle.h`/`.c` package templates and generator
   selection metadata.
2. Define stable slot chunks, generation values, capability kinds, lifecycle
   states, and a typed wrapper embedding the common handle representation.
3. Add registry initialization, growth, free-slot selection, generation
   increment, and exhaustion retirement. Process exit owns remaining registry
   storage; there is no root-shutdown cleanup pass.
4. Add operation-time resolve helpers that distinguish closed, stale, and
   wrong-kind handles without touching released control storage.

### Phase 2: lifecycle and Task bridge

1. Add reserve, publish, begin-close, final-release, and failed-construction
   transitions.
2. Implement the per-slot mutex, atomic resolve-and-pin operation, lease
   release, waiter accounting, and final-release predicate before integrating
   any capability.
3. Route native creation, mutation, and close through the libuv loop owner.
4. Integrate parked-operation wakeup with the existing Task/event bridge.
5. Assert payload-before-wake ordering and exactly one final cleanup owner.

### Phase 3: allocation and errors

1. Route private registry and control allocations through mimalloc.
2. Convert every private allocation failure into an allocation-free Error.
3. Add the common libuv ErrorKind mapper.
4. Reconcile File's mapper with the common categories while preserving its
   public File contract.

### Phase 4: File migration and child integration seam

1. Provide private helpers for child components to declare a distinct handle
   kind, attach stable control state, resolve it, and complete close.
2. Migrate File's public representation, resolution, close, and native control
   lifetime onto the common File capability kind without changing its method
   signatures or operation semantics.
3. Replace File's independent close/liveness state and libuv mapper; do not
   retain two registries, generation paths, or status switches.
4. Admit File in every common handle storage position above while retaining its
   rejection as a Dict key and its lack of equality, ordering, hash, and print.
5. Verify that an unused program selects no handle component or libuv artifact.

### Phase 5: conformance

1. Run focused generator and integration tests.
2. Run the ordinary Go suite and tagged C23 validation required by the repo.
3. Regenerate the snippet manifest only if a legitimate existing File artifact
   changes; unrelated entries must remain byte-identical.
4. Review generated headers for absence of public `uv_*` names.
5. Update `docs/reference.md` only during approved implementation, then remove
   this RFC's status entry when it closes.

## Validation

Validation is exhaustive for this RFC.

- One selected capability emits exactly one `handle.h`/`.c` pair; no use
  emits neither file.
- File remains distinct from IO and every unrelated source type. A private
  second-kind fixture proves that resolving a common handle through the wrong
  capability kind fails without touching control storage; child RFCs own
  source-level cross-wrapper rejection tests once those wrappers exist.
- File is the first wrapper type: it occupies every common handle storage
  position, remains invalid as a Dict key, and gains no equality/order/hash or
  print contract.
- Copies resolve the same live resource.
- Close through one copy makes every copy observe closed.
- Reusing a slot cannot validate a handle carrying the previous generation.
- Generation exhaustion retires the slot instead of wrapping.
- Locally provable post-close use is rejected; escaped stale use returns the
  stable `closed` Error.
- Concurrent resolve/close/final-callback races have one terminal cleanup and
  never touch released control memory.
- Registry growth does not invalidate an existing handle.
- Ordinary operations do not acquire the program-wide registry lock.
- Private allocation failure returns Error and publishes no partial handle.
- Private control allocations use mimalloc; materialized result collections
  continue to use their explicit Heap.
- Every common libuv condition maps to the exact ErrorKind in the table.
- An unmapped code reaches the caller's capability-specific fallback.
- Error messages, diagnostic detail, and source locations follow RFC 0181 and
  survive return-by-value without an ownerless allocation.
- File uses the common mapping and handle lifecycle, retains its File-specific
  operation behavior, and contains no independent liveness registry.
- Generated public declarations contain no libuv type, pointer, callback, or
  integer error code.
- No raw-pointer-only lifetime path or duplicate child registry exists.
- `go test ./...`, `go vet ./...`, and the applicable tagged C23 suite pass.

## Reference synchronization

Do not edit `docs/reference.md` while this RFC remains a design document.
Approved implementation must document the common copied-handle lifecycle,
generation check, valid storage positions, absence of equality/hash/print,
private runtime allocation rule, and portable libuv ErrorKind mapping before
the RFC closes.
