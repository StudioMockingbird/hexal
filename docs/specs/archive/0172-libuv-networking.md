# RFC 0172: libuv Networking

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; not scheduled. Design state: Draft; not
  implementation-ready
- Created: 2026-09-13
- Scope: define addresses, DNS, TCP, UDP, socket ownership, backpressure, and
  Task-parking network operations over libuv
- Depends on: ADR 0145, RFC 0168, RFC 0169, and RFC 0171 for timeouts
- Coordinates with: RFC 0144 and deferred C interoperability work
- Does not add: HTTP, TLS, async syntax, callbacks, or raw `uv_*` handles

## Summary

Use libuv's address, DNS, stream, TCP, and UDP facilities as Hexal's sole
portable network backend.

Conceptual source shape:

```hexal
addresses := try Dns.resolve("example.com", "443")
connection := try Tcp.connect(addresses[0])
try connection.write(request)
count := try connection.read(buffer)
```

Server shape:

```hexal
listener := try Tcp.listen(address)
while true then
    connection := try listener.accept()
    spawn serve(connection)
end
```

Exact names remain open.

## Libuv ownership

- `uv_getaddrinfo` and `uv_getnameinfo` own asynchronous name resolution.
- `uv_tcp_t` and stream operations own TCP bind, listen, accept, connect,
  read, write, shutdown, options, and close.
- `uv_udp_t` owns UDP bind, connect, send, receive, options, and close.
- libuv address helpers own IP parsing and formatting.
- libuv interface queries own portable network-interface discovery.
- `uv_poll_t` is reserved for imported descriptors without a typed libuv
  handle; it is not a second socket implementation.
- `uv_tcp_open`, `uv_udp_open_ex`, and the matching pipe facility own adoption
  of compatible native sockets or handles received through C interoperability.

### Network controls

The public design must account for the libuv controls needed by real servers
and network tools:

- TCP no-delay;
- basic and extended TCP keepalive;
- IPv6-only binding and simultaneous-accept behavior where applicable;
- reusable TCP and UDP addresses or ports where supported;
- UDP broadcast;
- UDP multicast membership, source membership, loopback, interface, and TTL;
- connected and unconnected UDP;
- batched UDP receive where `recvmmsg` is available; and
- TCP/UDP queued-write counts and bytes for backpressure decisions.

Hexal exposes typed, portable choices. It does not copy libuv flags into the
language or promise a control on a target where the qualified backend reports
it unsupported.

### Foreign socket adoption

C interoperability may transfer a compatible native socket or pipe into a
typed Hexal network value. Adoption is unsafe unless the checker can prove the
foreign ownership contract, because libuv may change native blocking mode and
become responsible for closing the handle.

An adopted handle:

- is validated against the requested stream or datagram kind;
- has one explicit ownership-transfer point;
- cannot remain independently owned by the foreign caller;
- follows ordinary Hexal close and external-state rules after adoption; and
- uses `uv_poll_t` only when no typed libuv handle can own it.

## Semantic direction

- Every potentially waiting operation parks its Task.
- Socket readiness never occupies the worker pool.
- Backpressure parks or bounds the producer; it never grows an unbounded
  runtime queue.
- Each connection, listener, and UDP socket has explicit close and external
  live-state tracking.
- Copying a handle follows the same external-state rules as current IO.
- Partial reads and writes are observable and documented.
- DNS and socket failures become owned Hexal `Error` values.
- No libuv callback runs application code.
- HTTP, TLS, framing, routing, and serialization remain libraries above this
  transport layer.

## Required sweep

No direct Winsock, BSD socket, resolver, readiness-reactor, or DNS worker
backend may coexist with the matching libuv operation. Imported foreign
descriptors may use `uv_poll_t` only under its documented restrictions.

## Detailed implementation outline

1. Settle address, endpoint, listener, connection, datagram, and DNS-result
   types and their ownership.
2. Implement address parsing/formatting and DNS over the RFC 0169 bridge.
3. Implement TCP connect/read/write/shutdown/close.
4. Implement listen/accept with bounded admission and backpressure.
5. Implement UDP bind/connect/send/receive/close.
6. Add the settled TCP keepalive/no-delay and UDP multicast/broadcast controls.
7. Add queued-write observations and enforce bounded backpressure.
8. Add native socket and pipe adoption through C-interoperability rules.
9. Add deadlines through RFC 0171 without duplicating timer machinery.
10. Add imported-descriptor polling only when typed adoption cannot apply.
11. Validate generated servers and clients under concurrency, errors, partial
   transfers, cancellation, and shutdown.

## Open design questions

1. What are the exact address and endpoint types?
2. Does DNS return a List, iterator, or purpose-built result?
3. How are accepted connections owned and closed on handler failure?
4. What buffering and backpressure limits apply to reads, writes, and accept?
5. Are TCP half-close and UDP connected mode part of v1?
6. How do cancellation and deadlines appear in source?
7. What address and socket-option subset is portable enough for v1?
8. Does native-handle adoption always transfer ownership, or is a separately
   named borrowed form ever safe enough to justify?
9. Which queued-write threshold and policy define backpressure?
10. Is batched UDP receive automatic when supported or an explicit socket
    construction option because it changes buffer handling?

## Validation direction

The final exhaustive Validation section must cover IPv4/IPv6, DNS, TCP client
and server lifecycle, UDP, partial IO, backpressure, high idle-connection
counts, TCP controls, UDP multicast/broadcast and batched receive, queued-write
metrics, foreign socket adoption and ownership transfer, Task progress,
deadlines, cancellation races, close races, Error mapping, no worker-pool
socket waiting, and no alternate socket backend.

## Reference synchronization

Do not edit `docs/reference.md` from this deferred proposal. An approved
implementation adds only the settled transport contracts.
