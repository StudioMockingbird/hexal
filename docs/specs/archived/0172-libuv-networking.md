# RFC 0172: libuv Networking

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed; implemented with one documented deviation. Address
  parse/format, `Dns.resolve`, and `Tcp.connect`/`listen`/`accept` plus
  `TcpConnection`/`TcpListener` (read, write, shutdown, no_delay, close) are
  in place over the shared generation-checked handle registry and its common
  libuv ErrorKind mapper. Deviation: this implementation admits one active
  read, one active write, and one active accept at a time per connection or
  listener; a second concurrent call of the same kind returns Error with
  kind `Busy` immediately rather than joining the FIFO wait queue this RFC's
  text specifies, a scope trim accepted to keep the first implementation's
  synchronization tractable. Verified by `go test ./...`, `go vet ./...`, and
  the tagged C23 suite (including a real TCP loopback connect/listen/accept/
  read/write/close exchange and DNS/Address compilation) running under GCC,
  Clang, and `zig cc`. `docs/reference.md` documents the public surface, the
  error table, component selection, and this deviation
- Created: 2026-09-13
- Updated: 2026-09-14
- Scope: define addresses, DNS, TCP, socket ownership, backpressure, and
  Task-parking network operations over libuv
- Depends on: ADR 0145, RFC 0168, RFC 0169, RFC 0181, and RFC 0180
- Coordinates with: RFC 0144, RFC 0171 for `Duration`/`Instant` only, RFC 0173
  for pipe-stream reuse, RFC 0155, and deferred C interoperability work
- Does not add: HTTP, TLS, async syntax, callbacks, or raw `uv_*` handles

## Summary

Use libuv's address, DNS, stream, and TCP facilities as Hexal's sole portable
network backend. UDP remains a separately gated follow-on.

Conceptual source shape:

```hexal
fun fetch(heap: Heap, request: Slice<Byte>, buffer: List<Byte>): Nil | Error do
    addresses := try Dns.resolve(heap, "example.com", "443")
    defer addresses.free(heap)
    connection := try Tcp.connect(addresses[0])
    defer connection.close()
    try connection.write(request)
    count := try connection.read(buffer, 4096)
    return nil
end
```

Server shape:

```hexal
fun run_server(address: Address): Nil | Error do
    listener := try Tcp.listen(address, 128)
    defer listener.close()
    while true do
        connection := try listener.accept()
        task := try spawn serve(connection)
        task.detach()
        Task.yield()
    end
end
```

These examples use the settled names and signatures. They deliberately put
`try` inside an Error-returning function, retain the current
mandatory `while ... do` spelling, claim the spawned Task through `detach`, and
keep the explicit `Task.yield()` required on every task-reachable literal
`while true` path.

## First implementation boundary

The first implementation contains only:

- inline IPv4/IPv6 address values and parse/format operations;
- asynchronous forward DNS resolution;
- TCP connect, bind/listen, accept, read, write, write-half shutdown, and
  close;
- a caller-supplied listen backlog;
- one simultaneous read and one simultaneous write per connection; and
- TCP no-delay.

UDP, multicast, source membership, basic and extended keepalive, reusable-port
controls, simultaneous-accept tuning, batched receive, interface discovery,
foreign socket adoption, and imported-descriptor polling remain separately
gated follow-ons. Availability in libuv is not sufficient reason to enlarge
the first public surface.

RFC 0171 supplies time values but deliberately does not supply operation
deadlines. The first networking implementation has no deadline or timeout API.
Close is its only cancellation request. A later focused deadline RFC must own
the timer-versus-operation race; no Task-cancellation surface is implied here.

## Public surface

All names in this section are compiler-protected builtins. They remain
compiler-owned until a future standard-library split moves declarations out of
the compiler without changing their contracts.

```text
type Address is union
    | IPv4 as bytes: Array<Byte, 4>, port: UInt16 end
    | IPv6 as bytes: Array<Byte, 16>, port: UInt16, scope: UInt32 end
end

Address.parse(text: String, port: UInt16) -> Address | Error
Address.format(heap: Heap)                -> String
Dns.resolve(heap: Heap, host: String, service: String)
                                             -> List<Address> | Error

Tcp.connect(address: Address)                 -> TcpConnection | Error
Tcp.listen(address: Address, backlog: Size)   -> TcpListener | Error

TcpListener.accept()                          -> TcpConnection | Error
TcpListener.close()                           -> Nil | Error

TcpConnection.read(into: List<Byte>, max: Size)
                                             -> Size | EoS | Error
TcpConnection.write(from: Slice<Byte>)        -> Nil | Error
TcpConnection.shutdown()                      -> Nil | Error
TcpConnection.no_delay(enabled: Bool)         -> Nil | Error
TcpConnection.close()                         -> Nil | Error
```

- `Address` is one inline closed ADT; no address object allocates.
- IPv4 stores four network-order bytes and a host-order port. IPv6 stores
  sixteen network-order bytes, a host-order port, and the platform-neutral
  numeric scope identifier used by libuv.
- `Address.parse` accepts a numeric IPv4 or IPv6 literal only. It performs no
  DNS lookup. An embedded NUL or malformed literal returns InvalidInput.
  A scoped IPv6 literal accepts only a decimal numeric scope after `%` on every
  target; interface-name scopes are deferred because libuv interprets them
  differently on Windows and POSIX.
- `Address.format` emits the numeric host address without a port and allocates
  the returned String from `heap`. For IPv6 with nonzero scope it appends
  `%<unsigned-decimal-scope>` itself; `uv_ip6_name` formats only the address
  bytes and would otherwise silently lose the stored scope. Every valid
  Address has a bounded representation, so formatting is infallible at the
  language level. An impossible tag or failed bounded native formatting is a
  runtime/compiler defect, not Error.
- `Dns.resolve` accepts a host plus numeric or named service, rejects embedded
  NUL before submission, snapshots both Strings into private request storage,
  preserves libuv result order, and allocates the result List from `heap`.
  Each returned Address is inline. A successful result is non-empty; no address
  is NotFound rather than an empty List.
- `backlog` must be positive and fit libuv's `int`; otherwise `Tcp.listen`
  returns InvalidInput before native submission.
- Binding an IPv6 listener always passes `UV_TCP_IPV6ONLY`; v1 never changes
  IPv4 acceptance according to a host's dual-stack default.
- `shutdown` closes only the write half. Reads remain valid until EoS or close.
- `close` invalidates the whole connection or listener through RFC 0180.
- `TcpConnection.close()` and `TcpListener.close()` are valid `defer` and
  `errdefer` cleanup calls.

`Address`, `Dns`, `Tcp`, `TcpListener`, and `TcpConnection` cannot be
redeclared or shadowed. The compiler-owned associated operations above are the
only v1 networking entrypoints.

## Handle and allocation contract

`TcpConnection` and `TcpListener` use RFC 0180's generation-checked copied
handle representation and valid storage positions. Their control blocks,
native requests, and waiter metadata use the private mimalloc-backed runtime
allocator. Constructors therefore take no source-level Heap. Only operations
that materialize an owning String or List receive an explicit Heap.

## Libuv ownership

- `uv_getaddrinfo` and `uv_getnameinfo` own asynchronous name resolution.
- `uv_tcp_t` and stream operations own TCP bind, listen, accept, connect,
  read, write, shutdown, options, and close.
- libuv address helpers own IP parsing and formatting.

### Deferred network controls

Later focused networking work may account for:

- basic and extended TCP keepalive;
- simultaneous-accept behavior where applicable;
- reusable TCP and UDP addresses or ports where supported;
- UDP broadcast;
- UDP multicast membership, source membership, loopback, interface, and TTL;
- connected and unconnected UDP;
- batched UDP receive where `recvmmsg` is available; and
- TCP/UDP queued-write counts and bytes for backpressure decisions.

None of these controls is part of this RFC's first implementation. A later RFC
must expose typed portable choices rather than copying libuv flags into Hexal.

### Deferred foreign socket adoption

C interoperability may eventually transfer a compatible native socket or pipe
into a typed Hexal network value. Adoption is unsafe unless the checker can
prove the foreign ownership contract, because libuv may change native blocking
mode and become responsible for closing the handle.

An adopted handle:

- is validated against the requested stream or datagram kind;
- has one explicit ownership-transfer point;
- cannot remain independently owned by the foreign caller;
- follows ordinary Hexal close and external-state rules after adoption; and
- uses `uv_poll_t` only when no typed libuv handle can own it.

## Semantic direction

- Every successfully submitted waiting operation parks its Task; immediate
  validation or submission failure returns Error without parking.
- Only reachable operations that can park -- DNS and TCP handle operations --
  select the scheduler, event component, handle component, and native event
  bootstrap. Address parse/format selects the network component and libuv's
  address helpers but not the scheduler, event bridge, or handle component;
  inline Address construction and comparison select no libuv runtime. There is
  no synchronous non-Task path for a parking operation.
- Socket readiness never occupies the worker pool.
- Backpressure parks the producer and never copies queued payload bytes into an
  unbounded runtime buffer.
- Each connection and listener has explicit close and external live-state
  tracking.
- Copying a handle follows RFC 0180; no copy can retain a freed control-block
  pointer.
- One read and one write may be active simultaneously on one connection.
- A second concurrent read returns Busy before consuming bytes. Concurrent
  writers park in FIFO order and submit one native write at a time.
- `write` is write-all: success means every source byte was accepted. Large
  Slices are submitted as sequential chunks within libuv's representable
  length. Failure after partial progress returns Error; v1 does not expose the
  completed prefix.
- A write borrows its source Slice until the call returns. The backing bytes
  must not be mutated or freed by another Task during that interval. V1 adds no
  ownership or cross-Task alias analysis; the programmer owns this
  synchronization.
- `read` appends at most `max` bytes and preserves prior List contents. It
  reserves destination capacity before native submission; the loop callback
  receives only that reserved tail and publishes the new length before waking
  the Task. Until return, another Task must not mutate, grow, or free the List
  or its backing allocation. V1 adds no cross-Task alias analysis. `max == 0`
  returns `Size(0)` without parking.
- A positive read count is ordinary success. `eos` appears only when the peer
  has ended the stream and no byte was delivered by that call.
- A libuv read callback with `nread == 0` is not completion and leaves the Task
  parked. The first callback delivering a positive count completes the call.
  `UV_EOF` produces EoS only when that call delivered no bytes. Requested
  capacity is clamped to the target representation accepted by `uv_buf_t`.
- Accept callers park in FIFO order. libuv and the kernel listen backlog own
  pending connections; Hexal does not add an unbounded accepted-connection
  queue.
- A resource-exhausted accept leaves the listener live, so a later accept may
  succeed after resources become available.
- POSIX runtime initialization ignores `SIGPIPE` before libuv socket or pipe
  writes can run. A closed peer therefore becomes BrokenPipe instead of
  terminating the process. This process-wide disposition also applies to
  imported C code.
- DNS and socket failures become owned Hexal `Error` values.
- No libuv callback runs application code.
- HTTP, TLS, framing, routing, and serialization remain libraries above this
  transport layer.

## Errors

Networking first applies RFC 0180's portable libuv ErrorKind mapper. Local
contract failures additionally use:

| Condition | ErrorKind |
| --- | --- |
| malformed numeric address | InvalidInput |
| read already active | Busy |
| closed handle or close-cancelled socket operation | Closed |
| other DNS failure | Other(header = `name resolution failed`) |
| other TCP/listener failure | Other(header = `network error`) |

Messages name the failed operation and follow RFC 0181's one settled diagnostic
detail representation. They never embed host text or addresses. Every Error
carries the Hexal call site's source location.

## Required sweep

No direct Winsock, BSD socket, resolver, readiness-reactor, or DNS worker
backend exists today. The implementation adds only the selected libuv path and
must not add a second backend. Deferred imported descriptors may use
`uv_poll_t` only under its documented target restrictions.

## Detailed implementation plan

### Phase 1: types and demand

1. Register the protected builtins and exact signatures above in the existing
   builtin registry; do not add a parallel name list.
2. Add checker rules for construction, calls, storage, capability results, and
   operation-specific diagnostics by mirroring File and IO dispatch.
3. Select `hexal/network.h`/`.c` from reachable networking use. Address
   parse/format additionally selects libuv's address-helper linkage. Select RFC
   0180's handle component, event bridge, scheduler, and native event bootstrap
   only from reachable parking operations; inline Address value use selects
   none of them.
4. Emit none of those artifacts for programs without reachable networking.

### Phase 2: addresses and DNS

1. Add the inline Address representation and libuv sockaddr conversion.
2. Implement numeric parse/format with `uv_ip4_addr`, `uv_ip6_addr`,
   `uv_ip4_name`, and `uv_ip6_name`; validate and append the portable numeric
   IPv6 scope explicitly.
3. Snapshot host/service, implement Task-parking `uv_getaddrinfo`, deterministic
   non-empty result copying, allocation failure, and `uv_freeaddrinfo` on every
   terminal path. DNS has no close or cancellation surface in v1.

### Phase 3: connection operations

1. Implement TCP initialization and connect on the loop owner.
2. Implement one active pull read using pre-reserved List tail storage, the
   `nread == 0` continuation rule, first-positive-callback completion, EOF
   ordering, and target-size clamp.
3. Implement FIFO serialized write-all without copying payload bytes.
4. Add half-shutdown, no-delay, and generation-checked close.
5. Make every callback write result state before publishing the Task wake.

### Phase 4: listeners

1. Implement bind/listen with the validated backlog.
2. Implement FIFO accept waiters without a user-space connection queue. Keep
   the listener live after a ResourceExhausted accept.
3. Close listeners through the common handle lifecycle and wake every waiter
   with Closed.

### Phase 5: sweep and conformance

1. Verify no direct socket, resolver, or alternate readiness backend exists.
2. Add the exhaustive tests below and generated-C assertions for component
   selection, declarations, and callback ownership.
3. Run ordinary Go tests, vet, and applicable C23 validation.
4. Regenerate snippet hashes only for newly added networking snippets; no
   existing entry may change.
5. Update `docs/reference.md` only during approved implementation and remove
   this RFC's status entry when it closes.

## Validation

Validation is exhaustive for this RFC.

- All protected names reject redeclaration and shadowing.
- IPv4, IPv6, and numeric-scope IPv6 parse/format round-trip without a fallible
  format result; interface-name
  scopes, malformed text, and embedded NUL return InvalidInput; Address
  construction performs no allocation.
- DNS returns every libuv result in order, allocates only its returned List
  from the supplied Heap, frees native results on every path, and parks only
  the calling Task.
- Connect, listen, and accept succeed for IPv4 and IPv6 loopback fixtures;
  IPv6 listen is always IPv6-only.
- Zero, oversized, and otherwise invalid backlog returns InvalidInput
  before submission.
- One connection makes simultaneous read and write progress; a second read
  returns Busy without consuming bytes.
- Concurrent writes complete in FIFO call order, success covers the whole
  Slice, large writes chunk correctly, and queued payload bytes are not copied.
- Read and write buffers remain caller-owned and receive no runtime copy;
  cross-Task mutation, growth, or free during the call is programmer-owned and
  gains no ownership-analysis promise from this RFC.
- Read appends without overwriting prior bytes; `max == 0`, `nread == 0`, first
  positive count, target-size clamping, short read, and EoS follow the exact
  contract.
- Accept waiters are FIFO and no unbounded accepted-connection queue exists.
- Shutdown preserves reads and rejects later writes; no-delay reaches libuv.
- Close wakes connect/read/write/accept/shutdown waiters with Closed,
  invalidates every copy, and releases native state exactly once. DNS is not
  close-cancellable in v1.
- A closed peer cannot terminate the process through SIGPIPE on POSIX; the
  runtime-wide ignored disposition is documented as affecting imported C.
- Every named common and networking-specific ErrorKind is exact,
  source-located, and target-independent.
- High idle-connection counts occupy no scheduler worker and make progress.
- Address-only reachability emits the network component without scheduler,
  event, or handle support; parking networking use additionally emits exactly
  one handle/event/scheduler set; absence emits none. Generated public headers
  expose no `uv_*` type.
- UDP and every other deferred control are absent from syntax, builtin
  registration, generated components, and snippets.
- No direct Winsock, BSD-socket, resolver, DNS-pool, or alternate readiness
  backend exists.
- `go test ./...`, `go vet ./...`, and applicable tagged C23 validation pass.

## Reference synchronization

Do not edit `docs/reference.md` from this draft proposal. An approved
implementation adds only the settled transport contracts.
