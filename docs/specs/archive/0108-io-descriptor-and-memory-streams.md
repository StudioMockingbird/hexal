# RFC 0108: IO - Descriptor and Memory Byte Streams

- Kind: Feature Specification (Rust-Style RFC)
- Status: Implemented; closed. The surface, checking, lowering, and print
  integration described here are in the compiler, `docs/reference.md` records
  the contract, and the ordinary suite covers every textual gate. Runtime
  transfer behavior remains a generated-C coverage gap under the standing
  no-execution policy.
- Created: 2026-08-22
- Updated: 2026-08-22
- Scope: synchronous byte streams for compiled Hexal programs
- Ancestry: RFC 0065 established the byte-stream boundary and primitive
  operation set. RFC 0105 established the raw descriptor backend. RFC 0106
  established the two-type generic design. RFC 0107's C23 `FILE *` direction
  was considered and rejected for its lifetime model and narrower scope.
- Supersedes RFCs 0065 and 0105-0107, which are archived as discarded design
  alternatives.
- Coordinates with: RFC 0039 (C interop), RFC 0052 (target profiles), RFC
  0055 (filesystem and build driver), RFC 0091 (blocking calls), RFC 0110
  (affine ownership), RFC 0113 (library end-of-stream), RFC 0115 (iterator
  invalidation), RFC 0118 (concurrency safety), the Error, List, View, generic,
  and lifetime contracts, `docs/reference.md`, and `docs/status.md`

## Decision

Hexal has two concrete byte-stream types:

```text
IO       byte stream over an open operating-system handle
Bytes    byte stream over an in-memory byte buffer
```

Generic code operates on either concrete type through ordinary duck-typed
generics. Generic instantiations are monomorphized. There is no interface,
vtable, runtime stream tag, or indirect dispatch in the initial design.
State-changing memory-stream operations use the existing `MutPtr<Bytes>`
receiver mechanism; OS-backed operations use `IO` directly.

The operating-system implementation uses raw descriptors or native handles,
not C `FILE *`. The IO specification does not open paths; the filesystem
specification produces an `IO` value from a path or other named resource.

The initial blocking contract accepts that a descriptor operation may block its
current OS worker. A later scheduler or evented-I/O specification may move the
operation to a blocking pool or readiness mechanism without changing these
stream operation signatures.

This RFC currently writes its result type with the existing builtin `EoS`
value. RFC 0113 replaces that sentinel with the library-level `End` result
variant before this RFC is implemented. Ownership, iterator invalidation, and
safe-task behavior are governed by RFCs 0110, 0115, and 0118 respectively.

## Goals

- Provide one byte-oriented operation surface for files, pipes, terminals, and
  standard handles.
- Let the same generic algorithm operate on OS-backed and memory-backed data.
- Use existing Hexal `List`, `View`, `Error`, `EoS`, generic, `try`, and
  `errdefer` mechanisms.
- Add no parser syntax, AST node, protected-operation syntax, interface, or
  type-erasure mechanism. `IO` and `Bytes` are protected nominal types whose
  methods are registered by the checker like the existing protected types.
- Preserve direct access to a low-level descriptor backend for future sockets
  and readiness-based I/O.
- Keep short transfers observable and ordinary.
- Reject known read/write capability mismatches before issuing a platform call.
- Preserve a native failure code in the existing `Error` value without adding a
  new error type.
- Avoid C `FILE *` buffering and its alias-after-`fclose` lifetime hazards.
- Keep all compiler behavior in the in-memory string-in/string-out boundary.

## Non-goals

- Filesystem paths, path encoding, directory operations, metadata, permissions,
  creation, or deletion.
- Sockets, socket addressing, readiness registration, polling, timeouts, or
  half-close.
- Text decoding, line iteration, formatted I/O, scanning, or framing.
- Implicit read-all, read-exact, write-all, or write-exact loops.
- C `FILE *`, C stdio buffering, or a portable stdio compatibility wrapper.
- A runtime interface or type-erased stream value.
- Mutable `View` or a new unsafe pointer/count read operation.
- Compiler filesystem access, artifact writing, external process execution, or
  runtime execution from ordinary Go tests.

## Compiler boundary

This RFC concerns runtime behavior of a compiled program. The compiler remains
string-in/string-out:

```text
Compile(sources: map[string]string, entrypoint: string, project: Project)
    -> generated C/header strings and diagnostics
```

The compiler does not discover paths, open files, inspect descriptors, or
perform host I/O. RFC 0055 owns the driver that supplies source and foreign
bindings, materializes generated artifacts, and invokes the C toolchain.

## Types

### `IO`

`IO` is a protected nominal handle representing an open operating-system
resource. Its generated representation is one native descriptor value, a
read/write capability mask, and an ownership bit:

```c
typedef struct hex_io {
    intptr_t desc;
    uint8_t access;
    bool owned;
} hex_io;
```

The access mask has two bits: readable and writable. Seekability is not encoded
in the type because it is an environmental property: the same standard handle
may refer to a file, pipe, or terminal at runtime. `seek` always asks the
backend and returns `Error` when the resource does not support it.

Capability checking has two tiers. If constructor or flow facts prove that a
read or write capability is absent, the checker rejects the call. Otherwise the
generated operation checks the access mask and returns `Error` before issuing a
platform call. This is a runtime capability check, not subtyping or a second
stream type hierarchy.

The checker records readable, writable, and unknown per-binding capability
facts from constructors and assignments. Branch merge keeps a capability only
when every incoming path proves it. Passing through a parameter, result, member,
union, or other untracked alias makes the fact unknown and selects the runtime
mask check. This extends the existing local flow-fact machinery; it is not
whole-program alias analysis.

The supported target profile defines the conversion between `intptr_t` and the
target's native descriptor or handle. POSIX uses an integer descriptor; Windows
uses a native `HANDLE` representation. The platform branch is confined to
`hexal/io.c` and never appears in a Hexal signature or generated module header.

The access mask and ownership bit are copied with the descriptor. They do not
provide unique ownership, reference counting, or shared closed-state tracking.
The filesystem constructor and foreign binding declaration must set them
explicitly; they must not be inferred from a descriptor number.

Copies of `IO` alias the same external resource. Closing one copy invalidates
the resource for every copy. The checker rejects direct use after close and
repeated close when local flow facts prove the state. When aliases escape into
parameters, members, or other external state, the existing undecidable-state
envelope applies.

An escaped stale descriptor is a resource-lifetime hazard, not permission to
emit C that dereferences freed memory. The generated operation must pass the
descriptor to the platform API and translate a platform failure to `Error`.
Descriptor reuse after close remains an explicit limitation until Hexal has a
unique-handle or shared-liveness mechanism. No specification may claim that
the current shallow-copy model provides complete alias lifetime safety.

### `Bytes`

`Bytes` is a memory-backed stream over a caller-provided `List<Byte>` and a
cursor:

```c
typedef struct hex_bytes {
    hex_list_Byte *buffer;
    size_t cursor;
} hex_bytes;
```

The initial constructor is:

```text
Bytes.over(buffer: List<Byte>) -> Bytes
```

`Bytes` borrows the list allocation and owns no heap resource. The source list
must remain live and must not be freed while the `Bytes` value is usable. The
current `View` rules do not track region lifetime, so they are not evidence that
this requirement is already enforced. The checker carries local provenance from
the `Bytes` binding to its source List and rejects use after a locally proved
free. Once either alias escapes the local facts, the existing undecidable-state
envelope applies; construction does not require a whole-program lifetime proof.

`Bytes` has no `close` operation. Releasing its underlying list remains the
caller's responsibility after every `Bytes` alias is dead.

Copying `Bytes` copies its inline cursor. Each copy subsequently advances
independently. `read`, `write`, and `seek` are methods of `MutPtr<Bytes>`, so a
direct call auto-addresses a mutable Bytes binding through the existing mutable
receiver rule. Generic code receives `MutPtr<Bytes>` explicitly and therefore
advances the caller's selected copy. This adds no Bytes-specific mutation rule,
allocation, shared cursor, cleanup operation, or hidden indirection.

Self-aliasing returns `Error` before reserve, copy, cursor movement, or List
mutation. `Bytes.read(buffer, max)` compares List identity and rejects when its
destination is its backing List. `Bytes.write(view)` rejects when the non-empty
View overlaps any byte in the backing List allocation. The runtime overlap test
converts addresses to `uintptr_t` under the two qualified flat-address-space
profiles and uses checked integer bounds; it never relationally compares
potentially unrelated C pointers.

### Name reservation

`IO`, `Bytes`, and `Seek` are protected global type names and are reserved like
the existing built-in types. Existing source declarations with any of those
names become Type Errors. Seek variants remain qualified under the ADT and do
not reserve the unqualified names `Start`, `Current`, or `End`.

### Initial storage restriction

Until Hexal has stronger handle and borrow semantics, `IO` is valid in
bindings, function parameters, function results, direct union members, and
Task arguments or results. `Bytes` is valid only in bindings, function
parameters, function results, and direct union members. Both are rejected in
object members, ADT payloads, collections, Channels, and heap allocation,
including aggregates that contain them recursively.

This is a bootstrap restriction, not a permanent property of stream values. It
limits long-lived aliases while the existing shallow-copy and external-state
rules remain in force. `IO` may cross a Task boundary as a shallow copy, but
the caller must synchronize concurrent operations externally. `IO` may not
cross a Channel boundary in the initial surface. These restrictions do not make
escaped parameter aliases safe; those still use the undecidable-state envelope
described above.

## Constructors

The IO specification defines only standard-handle constructors:

```text
IO.stdin()  -> IO | Error
IO.stdout() -> IO | Error
IO.stderr() -> IO | Error
```

They return borrowed handles with `owned = false`. Closing a borrowed handle is
a contract violation and traps before the platform close call. `stdin` sets
the readable access bit; `stdout` and `stderr` set the writable access bit. The
constructors are fallible because a process may lack the requested standard
handle, including a Windows process without an attached console. An absent or
invalid native handle returns `Error` and is never wrapped as successful `IO`.

Opening a path is not an IO operation. A future filesystem specification owns
the path-to-handle operation and returns an owned `IO` value. Foreign C
functions that return descriptors or native handles are imported through RFC
0039 with explicit ownership and read/write capability declarations. The
filesystem specification owns the named mode ADT and its mapping to platform
open flags; IO does not duplicate those flags.

## Shared operations

The stream types expose equivalent operations through their natural receiver
forms:

```text
IO.read(into: List<Byte>, max: Size)            -> Size | EoS | Error
IO.write(from: View<Byte>)                       -> Size | Error
IO.seek(to: Seek)                                -> Size | Error
MutPtr<Bytes>.read(into: List<Byte>, max: Size)  -> Size | EoS | Error
MutPtr<Bytes>.write(from: View<Byte>)             -> Size | Error
MutPtr<Bytes>.seek(to: Seek)                      -> Size | Error
```

`IO` additionally exposes:

```text
IO.close() -> Nil | Error
```

The receiver is evaluated once before the other arguments. Other argument
evaluation follows the existing left-to-right call contract.

A duck-typed generic stream parameter may instantiate as `IO` or
`MutPtr<Bytes>`. Calling such a generic with a memory stream passes `ref bytes`,
where `bytes` is a mutable binding; calling it with IO passes the IO value.

Each `IO.read` or `IO.write` issues at most one platform transfer call. POSIX
`EINTR` before transfer returns `Error` with the native code and is not retried.
Because the portable Error surface does not expose a structured native code,
portable source cannot branch specifically on `EINTR`; this is the deliberate
cost of the one-call primitive. Print uses its separate private write-all sink
and may retry there.

### `Seek`

```text
type Seek =
    | Start(Size)
    | Current(Int64)
    | End(Int64)
```

`Start` names an absolute position. `Current` and `End` name signed offsets.
The operation returns the resulting position. A position outside the stream's
representable range returns `Error` and does not change the position.

`position()` is `seek(Current(0))`; `skip(delta)` is `seek(Current(delta))`.
They are not primitive operations.

Seeking a non-seekable OS handle returns `Error`. A `Bytes` stream is seekable
within `[0, buffer.length]`; seeking beyond that range returns `Error`. `Bytes`
does not model sparse holes, so a write may extend the list only from a cursor
position already within that range.

### Read

`read` appends at most `max` bytes to the destination list. It preserves all
bytes already in the list. `max == 0` returns `Size(0)` without allocating or
touching the stream.

A positive count is ordinary success, including a short transfer. `eos` is
returned only when no byte was transferred and the source is drained. Other
failures return `Error`.

For `IO`, a missing readable access bit returns `Error` before allocation or
the platform call. For `Bytes`, the operation is always readable.

For `Bytes`, destination identity equal to the backing List returns `Error`
with message `memory stream cannot read into its backing list` before reserve.

After that check, either backend reserves destination capacity through one
internal List-component reserve helper. `Bytes` reserves current length plus
`max`. `IO` reserves current length plus the effective one-call request:
`min(max, SSIZE_MAX)` on POSIX and `min(max, UINT32_MAX)` on Windows, whose
`DWORD` transfer count is 32 bits.
The helper reuses the List growth allocator and checked arithmetic; it is not a
new source-visible `reserve` method. Capacity overflow and allocation failure
trap under the existing List contract.

`Bytes` reservation uses the caller's full `max`, not the amount eventually
copied. A large `max` may therefore request a correspondingly large List
allocation; callers choose the ceiling explicitly.

For `IO`, one operation issues at most one platform read. POSIX clamps that
call's requested count to `SSIZE_MAX`; Windows clamps it to `UINT32_MAX`. The
returned `Size` is the actual count from that call. For `Bytes`, one operation
copies from the cursor and advances it by the returned count.

### Write

`write` consumes a read-only `View<Byte>` and returns the exact number of bytes
accepted. A short positive write is ordinary success. The caller decides
whether to retry the remaining suffix.

An empty view returns `Size(0)` without calling the platform or memory
backend. No null pointer is passed to a C operation with a zero length.

For `IO`, a missing writable access bit returns `Error` before the platform
call. For `Bytes`, the operation is always writable.

For `Bytes`, a non-empty source View overlapping the backing List allocation
returns `Error` with message `memory stream cannot write from its backing list`
before reserve. A non-overlapping View is unaffected by later backing-List
growth.

For `IO`, one operation issues at most one platform write. POSIX clamps that
call's requested count to `SSIZE_MAX`; Windows clamps it to `UINT32_MAX`. The
returned `Size` is the actual count from that call. For `Bytes`, bytes are
written at the current cursor; existing bytes are overwritten and writes past
the current end extend the list. The Bytes backend reserves capacity for the
resulting end position through the same internal List-component helper. The
cursor advances by the count written.

The initial read surface intentionally has no stack-buffer fast path. If
benchmarks show that reusable `List<Byte>` buffers impose a material cost,
future work may add a safe mutable byte-region operation. It must reuse an
existing checked representation or receive a separate specification; this RFC
does not add a protected value-dependent generic operation or an unsafe
pointer/count pair merely to recover stack allocation.

### Close

`IO.close()` calls the platform close operation for an owned descriptor. A close
success returns `nil`. A close failure returns `Error`; the local handle is
considered closed even when the platform reports failure. POSIX close is never
retried after `EINTR`, because the first call may already have released the
descriptor and a retry may close a reused descriptor.

Closing a borrowed standard or foreign handle traps. Direct use after close and
repeated close are checker errors when the local state is known. Escaped alias
behavior follows the external-state limitation in the `IO` type section.

`Bytes` does not implement `close` and must not acquire a no-op close merely to
make generic cleanup appear symmetric.

## Errors

IO failures use the existing `Error` type and therefore compose with `try` and
`errdefer`. No new error category or hidden result channel is introduced.

Native failures do not allocate a dynamic Error message and do not require a
Heap argument. The generated backend formats a bounded ASCII header in the
existing inline `Strand` field, using `IO <operation> errno=<code>` for POSIX
or `IO <operation> winerr=<code>` for Windows. The message is a static literal
such as `read failed` or `write failed`. Capability mismatch uses the stable
message `stream is not readable` or `stream is not writable` and does not issue
a platform call.

This is component-internal construction of a `Strand`; it adds no source-level
Strand constructor and does not relax Strand's literal-only source contract.
Every permitted operation name and the widest supported signed POSIX or
unsigned Windows error code must fit the 31-byte payload by construction. The
backend writes one terminating NUL and zero-fills the remaining inline bytes;
silent truncation is forbidden. Strand equality, hashing, and printing therefore
continue to observe the canonical inline representation.

The native code is diagnostic data; source code must not depend on one target's
numeric values through the portable IO surface. Target-specific bindings may
expose richer native errors separately.

## Ownership and concurrency

`IO` copies alias external state. Hexal does not add moves, reference counting,
or a runtime registry for this RFC.

One `IO` may be used by multiple Tasks only under an external synchronization
policy. The stream contract promises no ordering between concurrent operations
and no compound-write atomicity.

`Bytes` aliases its borrowed list and must not be mutated concurrently without
the synchronization required by the list contract.

The initial implementation accepts blocking descriptor calls on the current OS
worker. Other workers continue to run. If all workers block, the scheduler
stalls until one operation completes. RFC 0091 may later add a blocking pool or
readiness implementation without changing the stream signatures.

## `print` integration

The current `print` implementation writes through C stdio. Once RFC 0108 is
implemented, `print` and raw `IO` writes must not operate through different
buffering domains on the same standard handle.

The implementation must perform a coordinated sweep:

- move the print byte sink to the shared descriptor write path;
- remove its dependency on `fwrite` for output transfer;
- give the print sink a private write-all loop for short writes and `EINTR`,
  because the existing print contract traps only after the complete call fails;
- preserve the existing print argument evaluation and formatting contract;
- preserve the documented output-failure trap behavior; and
- measure the cost of replacing implicit stdio buffering with explicit writes.

This sweep is part of the IO implementation, not an optional cleanup.

## C23 lowering

Demanded programs emit `hexal/io.h` and `hexal/io.c`.

`hexal/io.h` contains the `IO` and `Bytes` representations, declarations, and
only their standard dependencies. Selecting either stream type also selects the
`List<Byte>` specialization and its internal reserve helper. `hexal/list.h`
precedes `hexal/io.h`, which names the List representation. The header contains
no platform branch and no `FILE *`.

`hexal/io.c` owns the platform dispatch:

- POSIX descriptor reads, writes, seeks, and closes;
- Windows handle reads, writes, seeks, and closes;
- read/write capability checks before platform calls;
- translation of native failures and numeric native codes to the existing
  `Error` representation without dynamic message allocation; and
- standard-handle construction.

The supported target profiles are currently POSIX x86-64 and Windows x64.
`<stdint.h>` must provide `intptr_t`. The POSIX branch asserts
`sizeof(intptr_t) >= sizeof(int)`; the Windows branch asserts
`sizeof(intptr_t) >= sizeof(void *)` and converts `HANDLE` through `void *`.
This RFC does not claim that an arbitrary `intptr_t` conversion is portable
outside those profiles. RFC 0052 may later generalize the profile contract
without gating this initial surface. Windows seeking uses `SetFilePointerEx`,
not the legacy split-offset API.

The same profiles qualify `uintptr_t` as a flat numeric address representation
for the Bytes overlap check. The check handles null and empty regions first,
uses checked integer addition for each exclusive end address, and tests interval
overlap on integers. An unrepresentable valid-region bound traps as a target
contract violation rather than permitting unchecked arithmetic or C pointer
ordering.

The List component owns one internal reserve-at-least helper used by `IO.read`,
`Bytes.read`, and `Bytes.write`. IO must not duplicate List growth arithmetic or
allocator ownership.

The generated implementation must use the C23 or toolchain facility that
implements each required operation exactly. C23 `stdio` is not such a facility
for this design because its buffering and `FILE *` lifetime semantics are part
of the rejected abstraction. No private imitation of a C23 facility may be
generated.

Checked offset arithmetic uses `<stdckdint.h>`. C23 `nullptr`, `constexpr`,
fixed-width types, `static_assert`, and `[[noreturn]]` are emitted where their
contracts require them, not merely because they are available.

`hexal/io.h` and `hexal/io.c` are selected when reachable source uses `IO`,
`Bytes`, or `print`. Print-only programs need the same descriptor transfer core
and therefore select the pair even when no source-visible stream type appears.
Programs using none of those three surfaces emit no IO artifact and remain
byte-identical to their pre-IO generated artifacts.

## Compiler architecture

- Parsing adds no syntax or AST node.
- The checker reserves and registers `IO`, `Bytes`, and `Seek`; Bytes methods
  use the existing `MutPtr<T>` receiver machinery.
- The checker rejects statically known read/write capability mismatches and
  applies the initial stream storage restrictions.
- Generic method calls are checked and monomorphized through the existing
  generic machinery.
- Existing List, View, Error, EoS, `try`, and `errdefer` lowering is reused.
- Child scopes inherit the enclosing function's exact result `TypeUse`, not
  only its canonical result type, so contextual return expressions keep their
  union-member candidates inside blocks.
- The generator emits one demand-selected component pair and module-owned
  adapters where structural union injection requires them.
- The core compiler performs no host filesystem or descriptor operation.

## Validation

This section is exhaustive for the initial implementation.

- `IO`, `Bytes`, and `Seek` are reserved protected type names and have exactly
  the signatures in this RFC. Redeclaring any one is rejected; `Start`,
  `Current`, and `End` remain available as unqualified names.
- IO has no generic type parameter and no parser syntax.
- `IO` lowers with descriptor, access mask, and ownership state; capability
  metadata is not inferred from descriptor values.
- A statically proved read/write capability mismatch is a checker error. An
  unknown capability is checked at runtime; mismatch returns `Error` without
  allocation or a platform call.
- IO and Bytes are rejected in the initial long-lived storage positions listed
  in this RFC, including recursive aggregates.
- `IO` is accepted as a spawn argument or result; `Bytes` is rejected there
  because its borrowed List cannot cross the Task boundary initially.
- One generic function calling `read`, `write`, and `seek` compiles with
  `S = IO` and `S = MutPtr<Bytes>`, is called with an IO value or `ref bytes`,
  and emits monomorphized calls with no vtable or shared runtime dispatch.
- `Bytes.over` applies the specified list-lifetime restrictions.
- Bytes operations reject a fixed Bytes binding and auto-address a mutable
  binding through the existing `MutPtr<T>` method rule.
- Copying Bytes creates an independent cursor. A call through
  `MutPtr<Bytes>` advances that selected binding's cursor and is visible to the
  caller.
- Bytes self-read returns the specified Error by List identity before reserve.
- Bytes write from any non-empty View overlapping its backing allocation
  returns the specified Error before reserve; disjoint Views still work when
  growth reallocates the destination.
- The overlap check uses checked `uintptr_t` interval arithmetic and contains no
  relational comparison between C pointers.
- `Bytes.read` preserves prior list bytes, appends at most `max`, advances the
  cursor, and returns the specified count or `eos`.
- `Bytes.write` overwrites or extends at the cursor and advances it by the
  returned count.
- `Bytes.seek` accepts only representable positions in the specified range.
- `Bytes` rejects sparse-hole seeks and never writes an implicit gap.
- `IO.read` and `IO.write` preserve short positive transfers as success.
- `IO.read` distinguishes positive transfer, proven end-of-input, and failure.
- Native IO failures include the operation and numeric native failure code in
  the inline Error header; the message uses a static literal and no Heap is
  required on the failure path.
- Runtime-constructed Error headers fit the Strand payload, contain one NUL and
  zero-filled tail, and expose no source-level Strand construction operation.
- A failed standard-handle constructor returns `Error`; an invalid Windows
  standard handle is never wrapped as a successful `IO`.
- `read(buffer, 0)` performs no allocation or platform call.
- A readable IO capability mismatch performs no allocation or platform call.
- IO and Bytes reserve through the one internal List helper, with checked
  length-plus-capacity arithmetic and no source-visible reserve method.
- `EINTR` before read or write transfer returns Error and is not retried by the
  primitive; close is never retried after `EINTR`.
- POSIX read and write requests larger than `SSIZE_MAX`, and Windows requests
  larger than `UINT32_MAX`, issue one clamped call and return its actual count.
- `IO.seek` lowers to the descriptor backend and returns `Error` for a
  non-seekable handle.
- Standard handles lower to borrowed descriptors and closing one traps before
  the platform close call.
- Owned close returns `nil` on success or `Error` on platform failure and
  invalidates the local binding in either case.
- Direct locally provable use-after-close and repeated close are rejected.
- Escaped aliases follow the documented external-state envelope; no generated
  operation dereferences a freed C object.
- No generated IO signature contains `#ifdef`, `FILE *`, or a platform type.
- Platform branches occur only in `hexal/io.c`.
- Offset arithmetic uses checked C23 arithmetic with no unchecked equivalent.
- The canonical `Size | EoS` narrowing and injection path into a fallible
  `Size | Error` function produces no Unknown Error.
- A contextual literal returned from inside `if`, `elseif`, `else`, `while`,
  `for`, and match-arm scopes uses the enclosing function's exact result
  `TypeUse`. In particular, `return 0` inside a block of a `Size | Error`
  function injects Size, not Int32.
- `print` and IO writes use one output backend and no longer mix buffered stdio
  transfer with descriptor transfer.
- Print's private sink retries short writes and `EINTR` before trapping.
- IO support is emitted once when IO, Bytes, or print is reachable and is absent
  only when all three are unreachable.
- Selecting IO or Bytes selects `List<Byte>` support once, places `list.h`
  before `io.h`, and exposes only one internal reserve-at-least helper.
- Repeated compilation produces byte-identical artifacts.
- After behavior stabilizes, `docs/reference.md` records the protected names,
  receiver forms, placement, ownership, capability, transfer, error, seek,
  concurrency, print, and C23 contracts in this RFC.
- The snippet manifest changes only for existing print snippets and new IO or
  Bytes snippets. Any baseline rebuild follows the repository's temporary-test
  procedure and reviews the artifact breakdown explicitly.
- Generated-C assertions cover declarations before uses, required includes,
  component ownership, and absence of unused IO artifacts.
- `go test ./...`, `go vet ./...`, and `go vet -tags c23 ./...` pass without an
  external C compiler.

Runtime transfer, descriptor reuse, platform close behavior, scheduler progress,
and exact trap execution remain generated-C coverage gaps until the future
driver/toolchain lifecycle can execute artifacts. Text assertions must not be
described as runtime proof.

## Drawbacks

- Raw descriptors require POSIX and Windows backend code rather than only the
  ISO C library.
- A descriptor can block an OS worker under the initial scheduler contract.
- `List<Byte>` read buffers may allocate where a stack byte buffer would not.
- A stack-buffer fast path is intentionally deferred until measurement proves
  that the safe List-based surface is insufficient.
- Shallow copied external handles retain an unavoidable stale-alias hazard until
  Hexal gains stronger handle lifetime semantics.
- The initial placement restriction limits stream-wrapping abstractions until
  stronger lifetime semantics exist.
- `Bytes` borrowing a List requires a lifetime rule that current ownership
  machinery only partially enforces.
- Generic code cannot accept an unknown stream type at runtime without a future
  interface or explicit function-table representation.
- Moving print away from stdio buffering may reduce throughput for unbatched
  output.

## Implementation plan

This RFC is implementation-ready. No language-design decision remains. Execute
the phases in order; each phase leaves the ordinary Go suite green before the
next begins.

### Phase 0 - Baseline

1. Record the current test result and snippet-manifest diff before editing.
2. Do not regenerate the manifest during intermediate phases. An unexpected
   artifact change remains a finding until its owning phase explains it.
3. Preserve unrelated working-tree changes and the core compiler's in-memory
   boundary.

### Phase 1 - Contextual return fix

1. In `compiler/checker/scope.go`, make `scope.child()` inherit `resultUse`
   together with `result`.
2. Add focused checker and integration regressions for contextual returns from
   `if`, `elseif`, `else`, `while`, `for`, and match-arm scopes. Prove that a
   nested `return 0` in a `Size | Error` function selects Size and that the
   canonical narrowed `Size | EoS` return emits no invalid union injection.
3. Keep the checker as the owner. Do not weaken generator validation or special-
   case member index `-1`.

### Phase 2 - Types and checking

1. Add canonical `IO`, `Bytes`, and `Seek` identities to the existing type
   registry; reserve all three through the existing protected-name path. Build
   Seek's three qualified variants as compiler-owned canonical metadata beside
   the other protected types; do not register a source declaration implicitly.
2. Extend the position matrix and recursive containment checks exactly as the
   Initial storage restriction requires. Keep Bytes out of Task positions and
   both stream types out of objects, ADTs, collections, Channels, and heap
   allocation.
3. Add a focused `compiler/checker/io.go` owner for constructors and stream
   methods. Route calls from the existing method dispatcher; add no parser node
   or source syntax.
4. Register IO value-receiver methods and `MutPtr<Bytes>` methods. Reuse the
   existing mutable-receiver auto-addressing rule, so only a mutable Bytes
   binding may call a state-changing method directly.
5. Extend local flow state with readable/writable/unknown IO capability facts.
   Constructors seed facts, assignment copies them, branch merge intersects
   proofs, and untracked escape drops them to unknown. Proven mismatch is a
   checker error; unknown capability emits a runtime check.
6. Record the Bytes-to-List provenance edge beside existing binding facts.
   Reject locally proved use after List free and use the documented undecidable-
   state envelope after escape.
7. Add checked-only expression metadata for constructors and method calls and
   validate every field fail-closed before generation.

### Phase 3 - List support and component selection

1. Add one internal reserve-at-least helper to the List component template. It
   reuses the List allocator, uses checked arithmetic, preserves contents, and
   is not registered as a Hexal method.
2. Selecting IO, Bytes, or print selects the `List<Byte>` specialization and the
   helper exactly once.
3. Add `compiler/generator/io_component.go` and register it with the existing
   component builders and module-header dependency ordering.
4. Add `compiler/generator/packages/io.h` and `io.c`; keep all runtime C in
   those templates rather than Go string literals. `io.h` depends on the exact
   List, View, and Error component headers it names; `list.h` precedes `io.h`.
5. Demand the pair once for reachable IO, Bytes, or print and omit it when all
   three are absent.

### Phase 4 - C23 and platform lowering

1. Implement the platform-neutral representations, access bits, ownership bit,
   Seek representation, standard-handle constructors, and module-owned result-
   union adapters.
2. Confine POSIX and Windows branches to `io.c`. Apply the specified
   `intptr_t`, `SSIZE_MAX`, `UINT32_MAX`, `SetFilePointerEx`, partial-transfer,
   end-of-input, `EINTR`, and non-retried-close contracts.
3. Construct IO Errors with bounded canonical Strand headers and static String
   messages; allocate no failure-path message.
4. Implement Bytes reads, writes, seeks, cursor mutation, and List growth.
   Reject self-read by List identity. Reject overlapping write Views before
   growth through checked, profile-qualified `uintptr_t` interval arithmetic.
5. Add generator validation for every new checked node and text assertions for
   declarations before use, includes, platform-branch confinement, one component
   owner, absence when undemanded, and deterministic artifacts.

### Phase 5 - Print migration

1. Make `print.c` use the internal descriptor transfer operation from the IO
   component; remove `fwrite` and any now-dead stdio transfer dependency.
2. Keep formatting and argument evaluation unchanged. The private print sink
   loops over short writes, retries `EINTR`, and traps only when the complete
   print call cannot finish.
3. Ensure IO writes and print share one buffering domain and one standard-output
   backend. Print-only programs select the IO component pair.
4. Measure and record the generated-artifact and benchmark impact; measurement
   does not authorize a semantic or buffering change outside this RFC.

### Phase 6 - Conformance and handoff

1. Implement every case in Validation and no additional language behavior.
   Ordinary tests remain pure Go and assert generated C text; they do not invoke
   a C compiler or execute artifacts.
2. Add compact workbench snippets covering standard handles, Bytes mutation,
   generic IO/`MutPtr<Bytes>`, Seek, short-result handling, `eos`, Error, and
   cleanup. No snippet opens a filesystem path.
3. Rebuild the snippet manifest once. Existing hashes may move only for print
   snippets; new entries may appear only for the new IO/Bytes snippets. Review
   the artifact breakdown before accepting it.
4. Synchronize `docs/reference.md` once behavior stabilizes, covering every
   contract named by Validation. Remove RFC 0108 and its contextual-return bug
   from `docs/status.md` only after code, tests, generated output, and reference
   agree.
5. Run `gofmt` on changed Go files, then `go test ./...`, `go vet ./...`, and
   `go vet -tags c23 ./...`.
6. Rebuild the workbench binary into `bin/` and restart it before handoff.
7. Mark RFC 0108 closed and move it to the archive only after every preceding
   gate passes.

RFC 0055 may later add owned path constructors, RFC 0039 may add foreign
descriptor constructors, and RFC 0052 may generalize the two initial target
profiles without changing this surface.
