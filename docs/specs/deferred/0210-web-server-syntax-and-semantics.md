# RFC 0210: Web Server — Syntax and Semantics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the network runtime and TLS
  integration landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0144 (high-throughput network runtime), RFC 0194 (web server
  lowering), and RFC 0195 (TLS 1.3 integration)
- Coordinates with: RFC 0168 (libuv capability arc) for Event and Task
  integration, RFC 0186 (stdlib boundary) for module placement, and the
  current Task, Channel, Mutex, String, Slice, Error, and IO contracts in
  `docs/reference.md`
- Does not add: HTTP/2, HTTP/3, WebSocket, Server-Sent Events, gRPC, or a
  routing framework

## Motivation

A web server is the primary use case driving Hexal's concurrency and networking
investment. Without a surface specification, users cannot write portable HTTP
servers, and every implementation choice remains implicit. This RFC defines the
source-level API for listeners, connections, request/response, and server
lifecycle.

The network runtime (RFC 0144) provides the low-level socket and timer
facilities. This RFC defines how they compose into an HTTP/1.1 server that a
user can start, handle requests on, and shut down.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| TCP listener and connection | Pick up | Foundation for any server |
| HTTP/1.1 request parsing | Pick up | Minimal viable server |
| HTTP/1.1 response writing | Pick up | Minimal viable server |
| Keep-alive | Pick up | Required for production servers |
| Request body reading | Pick up | POST/PUT support |
| Header parsing | Pick up | Required for request handling |
| Chunked transfer encoding | Pick up | Required for streaming responses |
| TLS 1.3 termination | Separate RFC | RFC 0195 owns TLS integration |
| HTTP/2 and HTTP/3 | Skip in v1 | Complexity disproportionate to initial surface |
| Routing framework | Skip in v1 | Library concern, not language surface |
| Middleware pipeline | Skip in v1 | Library concern |
| WebSocket upgrade | Skip in v1 | Separate protocol, own spec |
| Server-Sent Events | Skip in v1 | Library concern |
| Static file serving | Skip in v1 | Library concern, uses IO |
| Compression (gzip, brotli) | Skip in v1 | Library concern, uses C interop |
| Connection pooling | Skip in v1 | Client concern, separate RFC |
| Graceful shutdown | Pick up | Required for production servers |
| Timeout/deadline | Pick up | Required for production servers |

## Source surface

### Listener

```text
import
    Net from "std/net"
end

listener: Net.TcpListener := try Net.listen(address, backlog)
```

The existing `TcpListener` and `TcpConnection` types from `std/net` are the
foundation. This RFC does not add new types for listeners or connections.

### Request

A request is a value produced by the server's accept loop:

```hexal
type Request is struct
    method: Net.HttpMethod,
    path: String,
    version: Net.HttpVersion,
    headers: Net.Headers,
    body: Net.RequestBody,
end
```

`Net.HttpMethod` is a protected enum:

```text
type HttpMethod is
    Get | Post | Put | Delete | Patch | Head | Options | Trace
end
```

`Net.HttpVersion` is:

```text
type HttpVersion is
    Http10 | Http11
end
```

`Net.Headers` is an ordered key-value store with case-insensitive key lookup:

```text
type Headers is struct
    -- internal representation
end

method Headers.get(name: String) -> String | Nil
method Headers.get_all(name: String) -> List<String>
method Headers.set(name: String, value: String)
method Headers.add(name: String, value: String)
method Headers.contains(name: String) -> Bool
method Headers.remove(name: String)
method Headers.iter() -> Headers.Iterator
```

`Net.RequestBody` supports incremental reading:

```text
type RequestBody is struct
    -- internal representation
end

method RequestBody.read(max: Size) -> String | Nil | Error
method RequestBody.read_all() -> String | Error
method RequestBody.is_complete() -> Bool
method RequestBody.content_length() -> Size | Nil
```

### Response

A response is built and sent explicitly:

```hexal
type Response is struct
    status: Net.StatusCode,
    headers: Net.Headers,
    body: Net.ResponseBody,
end

type ResponseBody is struct
    -- internal representation
end
```

Construction and sending:

```hexal
response := Response(
    status = Net.StatusCode.Ok,
    headers = Net.Headers(),
    body = Net.ResponseBody.from_string("Hello, World!")
)
response.headers.set("Content-Type", "text/plain")
response.send(connection)
```

Or as a builder:

```hexal
response := Net.ResponseBuilder()
    .status(Net.StatusCode.Ok)
    .header("Content-Type", "text/plain")
    .body("Hello, World!")
    .build()
response.send(connection)
```

`Net.StatusCode` is a protected enum covering standard HTTP status codes:

```text
type StatusCode is
    Ok,               -- 200
    Created,          -- 201
    Accepted,         -- 202
    NoContent,        -- 204
    MovedPermanently, -- 301
    Found,            -- 302
    NotModified,      -- 304
    BadRequest,       -- 400
    Unauthorized,     -- 401
    Forbidden,        -- 403
    NotFound,         -- 404
    MethodNotAllowed, -- 405
    InternalServerError, -- 500
    ServiceUnavailable,  -- 503
    Other(code: UInt16)
end
```

### Server lifecycle

The server runs inside a Task:

```hexal
import
    Net from "std/net",
    Prog from "std/program"
end

fun handler(connection: Net.TcpConnection) do
    match try connection.read_request()
        Net.Request as request then
            response := Net.Response(
                status = Net.StatusCode.Ok,
                headers = Net.Headers(),
                body = Net.ResponseBody.from_string("Hello, World!")
            )
            response.send(connection)
        Nil then
            -- connection closed
        Error as err then
            print(err)
    end
end

fun main() do
    listener := try Net.listen(
        Net.Address.parse("0.0.0.0", 8080),
        128
    )
    server := Net.Server.new(listener, handler)
    server.serve()
end
```

`Net.Server` is:

```text
type Server is struct
    -- internal representation
end

fun Server.new(listener: TcpListener, handler: Fun<(TcpConnection) : Nil>) -> Server
method Server.serve() -> Nil | Error
method Server.shutdown() -> Nil
method Server.set_timeout(timeout: Duration)
```

### Graceful shutdown

The server supports graceful shutdown:

```hexal
server := Net.Server.new(listener, handler)
-- start in a Task
spawn fun() do
    server.serve()
end

-- signal handler or timer triggers shutdown
server.shutdown()
```

Shutdown completes in-flight requests, stops accepting new connections, and
returns. A hard timeout forces termination if connections do not close in time.

### Deadlines and timeouts

Connection-level deadlines are set on the server:

```text
method Server.set_timeout(timeout: Duration)
method Server.set_header_timeout(timeout: Duration)
method Server.set_body_timeout(timeout: Duration)
```

A deadline that expires during a read or write returns Error with kind
`Timeout`.

## HTTP parsing

- Parse request lines, headers, and bodies incrementally across partial reads.
- Reject malformed requests with a 400 response before reading the body.
- Support `Content-Length` and chunked `Transfer-Encoding` for request bodies.
- Reject requests exceeding configurable header and body size limits.
- Headers are stored as a flat ordered list; case-insensitive lookup is
  O(n) where n is the number of headers.
- No header compression, HPACK, or QPACK in v1.

## Response writing

- Write status line, headers, and body in sequence.
- Support `Content-Length` for fixed bodies and chunked encoding for streaming.
- A response body that is a String or `List<Byte>` writes the complete body
  before returning.
- A streaming response body writes chunks as they become available.
- Flush the connection write buffer after each complete response.

## Error handling

All server operations return `Nil | Error` or `T | Error`. Errors use the
existing `ErrorKind` contract:

| Condition | ErrorKind | Message |
| --- | --- | --- |
| Invalid request line | `InvalidInput` | `malformed request line` |
| Header too large | `ResourceExhausted` | `request header exceeds limit` |
| Body too large | `ResourceExhausted` | `request body exceeds limit` |
| Connection timeout | `Timeout` | `connection timed out` |
| Header timeout | `Timeout` | `request header timed out` |
| Body timeout | `Timeout` | `request body timed out` |
| Connection reset | `ConnectionReset` | `connection reset by peer` |
| Broken pipe | `BrokenPipe` | `broken pipe` |
| Address in use | `Busy` | `address already in use` |
| Permission denied | `PermissionDenied` | `permission denied` |

## C23 lowering

The HTTP server is a library, not a language feature. The checker and generator
emit no special C for HTTP concepts. All types lower to existing Hexal
representations (structs, enums, function pointers). The HTTP parsing and
response writing are library functions that call through the existing TCP
socket operations.

## Non-goals

- HTTP/2 or HTTP/3 multiplexing and framing.
- WebSocket protocol upgrade.
- TLS termination (owned by RFC 0195).
- Routing, middleware, or plugin systems.
- Connection pooling or client-side HTTP.
- Server-Sent Events or long polling.
- Static file serving or sendfile optimization.
- Compression or encoding negotiation.
- Session management or cookies.
- Request/response body caching.
- Load balancing or reverse proxy functionality.

## Required sweep

- parser and checker recognition of `HttpMethod`, `HttpVersion`, `StatusCode`,
  `Headers`, `Request`, `RequestBody`, `Response`, `ResponseBody`, and
  `Server` types;
- HTTP parsing library functions and incremental state machine;
- response writing and chunked encoding;
- server lifecycle, graceful shutdown, and deadline management;
- integration with RFC 0144's non-blocking socket operations;
- workbench snippet and manifest entries;
- `docs/reference.md` HTTP surface after explicit approval.

## Validation

This section is exhaustive:

- `HttpMethod` enum covers all standard HTTP/1.1 methods plus `Other(String)`;
- `HttpVersion` covers HTTP/1.0 and HTTP/1.1;
- `StatusCode` covers standard codes plus `Other(UInt16)`;
- header case-insensitive lookup, multi-value headers, add/remove/contains;
- request body incremental reading, `Content-Length` and chunked transfer
  encoding;
- response body string, byte list, and streaming chunk writing;
- server listen, accept loop, handler invocation, and connection close;
- graceful shutdown completes in-flight requests and stops accepting;
- header, body, and connection timeouts return exact `Timeout` errors;
- malformed request line, oversized headers, and oversized bodies return
  exact errors;
- connection reset and broken pipe return exact errors;
- address in use and permission denied return exact errors;
- HTTP/1.0 requests without keep-alive close the connection after the response;
- HTTP/1.1 requests default to keep-alive unless `Connection: close` is sent;
- chunked response writing produces valid chunked transfer encoding;
- concurrent Task-safe server when handler is safe;
- existing TCP, IO, Task, and Channel behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether `Server` should be generic over the handler type (allowing both
   `Fun<(TcpConnection) : Nil>` and Task-aware handlers).
2. Whether the response builder pattern is necessary or direct struct
   construction is sufficient.
3. Whether `RequestBody` should implement a read interface or expose a
   `View<Byte>` over buffered data.
4. Whether `Headers` should be an opaque type or a `Dict`-like type with
   case-insensitive keys.
5. Whether chunked encoding should be default for streaming or opt-in.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation adds
the HTTP types, server lifecycle, and request/response contracts only after
behavior stabilizes and with explicit user approval.
