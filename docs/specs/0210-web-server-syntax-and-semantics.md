# RFC 0210: Web Server — Syntax and Semantics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; active design for the minimum HTTP server surface;
  implementation not started
- Created: 2026-09-15
- Updated: 2026-09-29
- Depends on: RFC 0144 (Task-aware socket runtime contract) and RFC 0198 (HTTP
  parsing and serialization)
- Coordinates with: RFC 0168 (libuv capability arc) for Event and Task
  integration, RFC 0186 (stdlib boundary) for module placement, RFC 0194
  (default backend), RFC 0195 (TLS), RFC 0200 (static file serving), RFC 0208
  (backend contract), and the current Task, Channel, Mutex, String, Slice,
  Error, and IO contracts in `docs/reference.md`
- Does not add: HTTP/2, HTTP/3, WebSocket, Server-Sent Events, gRPC, or a
  routing framework

## Motivation

A web server is the primary use case driving Hexal's concurrency and networking
investment. Without a surface specification, users cannot write portable HTTP
servers, and every implementation choice remains implicit. This RFC defines the
source-level API for listeners, connections, request/response, and server
lifecycle.

The runtime contract (RFC 0144), built on the libuv foundation (RFC 0145),
provides Task-aware socket and timer operations. This RFC defines how they
compose into an HTTP/1.1 server that a user can start, handle requests on, and
shut down.

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
| TLS termination | Defer | TLS is not required by the first HTTP server cut |
| HTTP/2 and HTTP/3 | Skip in v1 | Complexity disproportionate to initial surface |
| Basic method/path router | Pick up | Common dispatch surface shared by handlers and backends |
| Routing framework | Skip in v1 | Advanced routing remains a library concern |
| Middleware pipeline | Skip in v1 | Library concern |
| WebSocket upgrade | Skip in v1 | Separate protocol, own spec |
| Server-Sent Events | Skip in v1 | Library concern |
| Static file serving | Pick up | Common HTTP feature; specified by RFC 0200 |
| Compression (gzip, brotli) | Skip in v1 | Library concern, uses C interop |
| Connection pooling | Skip in v1 | Client concern, separate RFC |
| Graceful shutdown | Pick up | Required for production servers |
| Timeout/deadline | Pick up | Required for production servers |

## Current design direction

- `Request` and `Response` are built-in standard runtime types whose public
  behavior follows the server-relevant Web Fetch contract: URL, method,
  headers, status, and one-shot streaming bodies. Browser-only behavior is not
  added merely for API resemblance.
- Their representations are opaque to source code. `Request.method` is an
  extensible method-token string; `Request.url` is the absolute request URL;
  request and response bodies are byte streams and are consumed at most once.
- `Headers` follows Fetch `Headers` semantics: case-insensitive valid names,
  `append`, `set`, `delete`, and `get`; ordinary duplicate values are combined
  as the standard specifies, while `Set-Cookie` remains separately
  retrievable. It is not a `Dict`.
- `ServerConfig` carries transport-neutral listener settings, beginning with
  required `host` and `port`. Backend-specific controls do not belong in this
  type. Request-size limits and timeouts have one owner here and are passed
  unchanged to every backend.
- `Router` is a basic standard-library router. Its canonical registration
  operation is `route(method, path, handler)`; per-verb aliases are not part
  of the initial surface.
- `Router.mount(prefix, file_server)` registers RFC 0200's static file server
  at a path prefix. Prefix matching and file-path resolution are owned by RFC
  0200; ordinary `route` registrations retain their exact method/path behavior.
- Ordinary routes match the request URL's path and method token exactly; they
  have no path parameters, wildcard syntax, or implicit method aliases. RFC
  0200's `mount` is the sole prefix-based dispatch operation in v1. An unknown
  path produces 404; a known ordinary route with no matching method produces
  405 and an `Allow` header.
- A handler has type `Fun<(Request): Response | Error>`. Network body reads
  and writes are synchronous-looking Task operations that park rather than
  block a scheduler worker; no async/await syntax is introduced.
- A handler Error becomes a generic 500 response; internal Error details are
  not sent to the client. A disconnected client closes its request/response
  streams; whether it also cancels the handler Task remains open.
- `Http.serve(config, router)` selects Hexal's default HTTP backend.
  `Http.serve_with(backend, config, router)` selects a statically linked
  library backend that satisfies RFC 0208's backend contract.
- The entrypoint module executes at module scope. Hexal source does not require
  or define a `main` function.

Illustrative surface:

```hexal
import Http from std.http

fun home(request: Request): Response | Error do
    return Response("Hello, world!", status = 200)
end

let mut router = Http.Router()
router.route("GET", "/", home)

let config = Http.ServerConfig(host = "127.0.0.1", port = 8080)
match Http.serve(config, router) is
    Nil then
        print("server stopped")
    Error as err then
        print(err)
end
```

The example uses the entrypoint module's root statements; it intentionally has
no source-level `main` function. The root explicitly handles `Http.serve`'s
`Nil | Error` result because entry-module root has no Error result to propagate.

These decisions supersede conflicting examples and API sketches later in this
draft. Those sketches are historical discussion material, not current syntax
or normative behavior.

## Earlier source-surface sketch (superseded)

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

### Superseded server-lifecycle sketch

The earlier sample used a source-level `main` function and a connection-level
handler. Both are withdrawn: the entrypoint module runs its root statements,
and application handlers consume built-in `Request` values and return built-in
`Response` values through the Router and selected backend described above.

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
- TLS termination (deferred RFC 0195).
- Middleware and advanced routing (RFC 0202 owns middleware; this RFC owns the
  basic router).
- Backend implementation details (RFCs 0194 and 0208).
- Connection pooling or client-side HTTP.
- Server-Sent Events or long polling.
- Compression or encoding negotiation.
- Session management or cookies.
- Request/response body caching.
- Load balancing or reverse proxy functionality.

## Required sweep

- built-in `Request` and `Response` runtime types and their Web Fetch-aligned
  body, header, URL, and status contracts;
- standard-library `Headers`, `Router`, `ServerConfig`, handler, and serving
  APIs;
- generic route registration and method/path dispatch;
- default backend selection and explicit backend selection through RFC 0208;
- server lifecycle, graceful shutdown, and deadline management;
- integration with RFC 0144's non-blocking socket operations;
- workbench snippet and manifest entries;
- `docs/reference.md` HTTP surface after behavior stabilizes and before this
  RFC is closed.

## Validation

This section is exhaustive for the source-level contract; wire parsing is
validated by RFC 0198, HTTP server behavior by RFC 0194, and Task-aware socket
lifecycle by RFC 0144:

- the entry-module example compiles without a source-level `main` and handles
  both `Nil` and `Error` from `Http.serve`;
- Request and Response expose only the selected server-side Fetch semantics;
- Header lookup compares names case-insensitively, ordinary appended values combine
  per Fetch, and `Set-Cookie` values remain separately retrievable;
- Request and Response bodies are byte-oriented, one-shot streams;
- ServerConfig host, port, request limits, and deadlines reach either backend
  unchanged;
- route registration accepts standard and extension method tokens;
- exact path-and-method match invokes the registered handler;
- unknown path returns 404; known path with an unsupported method returns 405
  with `Allow` listing the registered methods;
- `Http.serve` selects the default backend and `Http.serve_with` selects only
  the explicitly supplied static backend;
- `Router.mount` dispatches requests under its prefix to the mounted
  FileServer, while ordinary routes retain exact method/path matching;
- handler Error details are not exposed in the generated 500 response;
- existing TCP, IO, Task, and Channel behavior is unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. The exact source signatures for Request/Response construction, body-stream
   reads/writes, and Header accessors; these must be a deliberately small
   server subset of Fetch, not a claim to implement the whole web platform.
2. Required ServerConfig fields and defaults for header/body limits, read/write
   queue bounds, timeouts, backlog, and connection bounds.
3. Whether exact route matching uses the raw request-target path or the
   normalized URL pathname, including percent-encoding and query handling.
4. Duplicate route registration behavior.
5. Whether disconnect cancels a handler Task or only closes its I/O streams.
   Recommend the first cut close streams and surface I/O failure, without
   introducing general Task cancellation semantics.
6. The static C backend adapter ABI, including callback lifetime and memory
   ownership across Hexal/C boundaries (RFC 0208).

## Reference synchronization

Implementation updates `docs/reference.md` after HTTP behavior stabilizes and
before this RFC is marked implemented or closed.
