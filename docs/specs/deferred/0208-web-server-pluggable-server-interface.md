# RFC 0208: Web Server — Pluggable Server Interface

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the web server syntax (RFC 0210)
  and middleware architecture (RFC 0202) landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax), RFC 0202 (middleware architecture),
  RFC 0194 (web server lowering), and the implemented RFCs 0145 (libuv
  runtime), 0146 (mimalloc), and 0168 (libuv capability arc)
- Coordinates with: RFC 0210 (web server syntax) for the Request and Response
  types, RFC 0195 (TLS) for encrypted backends, and RFC 0186 (stdlib
  boundary) for module placement
- Does not add: specific backend implementations (those are separate specs)

## Motivation

The default libuv-based HTTP server works for most use cases, but library
writers need to provide specialized backends: HTTP/2 servers, gRPC servers,
WebSocket-only servers, mock servers for testing, or custom protocol
servers. Without a pluggable interface, every backend reimplements the
same server lifecycle, middleware chain, and handler dispatch, or the
standard library locks into one implementation.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Server trait/interface | Pick up | Foundation |
| Request/Response contract | Pick up | Required for interoperability |
| Middleware compatibility | Pick up | Required for reuse |
| Handler dispatch contract | Pick up | Required for reuse |
| Backend selection (compile-time) | Pick up | Zero runtime overhead |
| Backend selection (runtime) | Skip in v1 | Complexity disproportionate to initial surface |
| Custom request types | Pick up | Required for protocol-specific data |
| Custom response types | Pick up | Required for protocol-specific data |
| Backend negotiation | Skip in v1 | Complexity disproportionate to initial surface |
| Backend discovery (plugin system) | Skip in v1 | Complexity disproportionate to initial surface |

## Design principle

A server backend is any type that satisfies the `Server` interface. The
standard library provides the libuv backend. Library writers provide their
own by implementing the same interface. The middleware chain, router, and
handler types are backend-agnostic; they work with any `Server`
implementation.

## Source surface

### Server interface

```text
type Server is interface
    fun start(handler: Handler) -> Nil | Error
    fun stop() -> Nil
    fun is_running() -> Bool
end
```

- `start` begins accepting connections and dispatching to `handler`.
- `stop` initiates graceful shutdown.
- `is_running` returns whether the server is accepting requests.

### Handler interface

```text
type Handler is interface
    fun handle(request: Request) -> Response | Error
end
```

- `handle` processes a request and returns a response.
- The default `Handler` is `Fun<(Request) : Response | Error>`.

### Request interface

```text
type Request is interface
    fun method() -> String
    fun path() -> String
    fun version() -> (UInt8, UInt8)
    fun headers() -> Headers
    fun body() -> String | Nil
    fun body_stream() -> Stream | Nil
    fun context() -> Context
    fun remote_address() -> String
    fun request_id() -> String
end
```

### Response interface

```text
type Response is interface
    fun status() -> UInt16
    fun reason() -> String
    fun headers() -> Headers
    fun body() -> String | Nil
    fun body_stream() -> Stream | Nil
end
```

### Stream interface

```text
type Stream is interface
    fun read(chunk_size: Size) -> String | Nil | Error
    fun write(data: String) -> Nil | Error
    fun close() -> Nil
end
```

## Usage

### Default server (libuv)

```hexal
import
    Net from "std/net"
end

server := Net.Server.new(host = "0.0.0.0", port = 8080)
server.serve(handler)
```

The default server uses the libuv backend (RFC 0194).

### Custom backend

```hexal
import
    Net from "std/net"
end

type MyServer is struct
    -- custom server state
end

impl MyServer is Net.Server
    fun start(handler: Net.Handler) -> Nil | Error {
        -- custom server implementation
    }

    fun stop() -> Nil {
        -- custom shutdown
    }

    fun is_running() -> Bool {
        -- custom status
    }
end

server := MyServer.new()
server.serve(handler)
```

### Backend selection

```hexal
import
    Net from "std/net"
end

-- Compile-time backend selection
type MyServer is struct
    -- custom server state
end

impl MyServer is Net.Server
    fun start(handler: Net.Handler) -> Nil | Error {
        -- custom implementation
    }

    fun stop() -> Nil {
        -- custom shutdown
    }

    fun is_running() -> Bool {
        -- custom status
    }
end

-- Use custom backend
server := MyServer.new(host = "0.0.0.0", port = 8080)
server.serve(handler)
```

### Middleware compatibility

```hexal
import
    Net from "std/net"
end

-- Middleware works with any backend
logging := fun (req: Net.Request, next: Net.Next) -> Net.Response | Error {
    start := Net.now()
    response := next()
    elapsed := Net.now() - start
    Net.print(`${req.method} ${req.path} ${response.status} ${elapsed}ms`)
    response
}

router := Net.Router.new()
router.use(logging)
router.get("/users", list_users)

-- Router works with any Server implementation
server := MyServer.new(host = "0.0.0.0", port = 8080)
server.serve(router.handler())
```

### Custom request type

```hexal
type GrpcRequest is struct
    -- gRPC-specific request data
    service: String,
    method: String,
    metadata: Dict<String, String>,
    message: List<Byte>,
end

impl GrpcRequest is Net.Request
    fun method() -> String {
        "POST"
    }

    fun path() -> String {
        `/${self.service}/${self.method}`
    }

    fun version() -> (UInt8, UInt8) {
        (2, 0)  -- HTTP/2
    }

    fun headers() -> Net.Headers {
        -- convert gRPC metadata to HTTP headers
    }

    fun body() -> String | Nil {
        -- serialize gRPC message
    }

    fun body_stream() -> Net.Stream | Nil {
        -- stream gRPC message
    }

    fun context() -> Net.Context {
        -- gRPC context
    }

    fun remote_address() -> String {
        -- client address
    }

    fun request_id() -> String {
        -- gRPC call ID
    }
end
```

### Custom response type

```hexal
type GrpcResponse is struct
    -- gRPC-specific response data
    status_code: UInt16,
    status_message: String,
    metadata: Dict<String, String>,
    message: List<Byte>,
end

impl GrpcResponse is Net.Response
    fun status() -> UInt16 {
        self.status_code
    }

    fun reason() -> String {
        self.status_message
    }

    fun headers() -> Net.Headers {
        -- convert gRPC metadata to HTTP headers
    }

    fun body() -> String | Nil {
        -- serialize gRPC message
    }

    fun body_stream() -> Net.Stream | Nil {
        -- stream gRPC message
    }
end
```

## Backend implementations

### Standard libuv backend

```hexal
type LibuvServer is struct
    -- libuv-specific state
    host: String,
    port: UInt16,
    running: Bool,
end

impl LibuvServer is Net.Server
    fun start(handler: Net.Handler) -> Nil | Error {
        -- libuv implementation (RFC 0194)
    }

    fun stop() -> Nil {
        -- libuv graceful shutdown (RFC 0204)
    }

    fun is_running() -> Bool {
        self.running
    }
end
```

### Mock backend (for testing)

```hexal
type MockServer is struct
    -- mock state
    requests: List<Net.Request>,
    responses: List<Net.Response>,
end

impl MockServer is Net.Server
    fun start(handler: Net.Handler) -> Nil | Error {
        -- no-op for testing
    }

    fun stop() -> Nil {
        -- no-op for testing
    }

    fun is_running() -> Bool {
        true
    }

    fun send(request: Net.Request) -> Net.Response | Error {
        handler.handle(request)
    }
end
```

### HTTP/2 backend (future)

```hexal
type Http2Server is struct
    -- HTTP/2-specific state
end

impl Http2Server is Net.Server
    fun start(handler: Net.Handler) -> Nil | Error {
        -- HTTP/2 implementation (RFC 7540)
    }

    fun stop() -> Nil {
        -- HTTP/2 graceful shutdown
    }

    fun is_running() -> Bool {
        -- HTTP/2 status
    }
end
```

## C23 lowering

### Interface dispatch

Interfaces are lowered to function pointer tables:

```c
typedef struct hex_server_vtable {
    int (*start)(void *self, hex_handler handler);
    void (*stop)(void *self);
    bool (*is_running)(void *self);
} hex_server_vtable;

typedef struct hex_server {
    void *impl;
    hex_server_vtable *vtable;
} hex_server;

int hex_server_start(hex_server *server, hex_handler handler) {
    return server->vtable->start(server->impl, handler);
}

void hex_server_stop(hex_server *server) {
    server->vtable->stop(server->impl);
}

bool hex_server_is_running(hex_server *server) {
    return server->vtable->is_running(server->impl);
}
```

### Request dispatch

```c
typedef struct hex_request_vtable {
    const char *(*method)(void *self);
    const char *(*path)(void *self);
    hex_version (*version)(void *self);
    hex_headers *(*headers)(void *self);
    const char *(*body)(void *self);
    hex_stream *(*body_stream)(void *self);
    hex_context *(*context)(void *self);
    const char *(*remote_address)(void *self);
    const char *(*request_id)(void *self);
} hex_request_vtable;

typedef struct hex_request {
    void *impl;
    hex_request_vtable *vtable;
} hex_request;
```

### Response dispatch

```c
typedef struct hex_response_vtable {
    uint16_t (*status)(void *self);
    const char *(*reason)(void *self);
    hex_headers *(*headers)(void *self);
    const char *(*body)(void *self);
    hex_stream *(*body_stream)(void *self);
} hex_response_vtable;

typedef struct hex_response {
    void *impl;
    hex_response_vtable *vtable;
} hex_response;
```

## Performance considerations

- **Zero-cost abstraction**: Interface dispatch is a function pointer
  table; no runtime overhead beyond a vtable lookup.
- **Compile-time selection**: Backend is selected at compile time; no
  dynamic dispatch unless interfaces are used.
- **Inlining**: Small interface methods (status, path, method) can be
  inlined by the compiler.
- **No allocation**: Interface implementation is a struct; no heap
  allocation for the server itself.

## Demand rules

- `Server`, `Handler`, `Request`, `Response`, and `Stream` interfaces
  select the pluggable server component.
- The pluggable server component does not select libuv, native bootstrap,
  or the event bridge; it is a pure interface definition.
- A specific backend (e.g., libuv) selects its own dependencies.
- A program that does not use the `Server` interface does not select the
  pluggable server component.

## Required sweep

- `Server` interface definition in `hexal/server.h`;
- `Handler` interface definition;
- `Request` interface definition;
- `Response` interface definition;
- `Stream` interface definition;
- vtable generation for interface dispatch;
- default `Handler` implementation (function adapter);
- demand discovery for pluggable server component;
- workbench snippet and manifest entries;
- `docs/reference.md` pluggable server surface after explicit approval.

## Validation

This section is exhaustive:

- `Server.start` dispatches through vtable;
- `Server.stop` dispatches through vtable;
- `Server.is_running` dispatches through vtable;
- `Handler.handle` dispatches through vtable;
- `Request.method` dispatches through vtable;
- `Request.path` dispatches through vtable;
- `Request.headers` dispatches through vtable;
- `Request.body` dispatches through vtable;
- `Response.status` dispatches through vtable;
- `Response.headers` dispatches through vtable;
- `Response.body` dispatches through vtable;
- `Stream.read` dispatches through vtable;
- `Stream.write` dispatches through vtable;
- `Stream.close` dispatches through vtable;
- default `Handler` works with any `Request`/`Response`;
- middleware works with any `Server` implementation;
- custom `Request` type works with middleware;
- custom `Response` type works with middleware;
- libuv backend satisfies `Server` interface;
- mock backend satisfies `Server` interface;
- interface dispatch is zero-cost (function pointer table);
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support runtime backend selection (dynamic dispatch) in
   addition to compile-time selection.
2. Whether to add a `ServerBuilder` pattern for configuring backends.
3. Whether to add backend-specific extensions (e.g., HTTP/2 server push)
   through an optional interface.
4. Whether to support backend composition (e.g., TLS wrapping any
   backend).
5. Whether to add a `Server.test` method that returns a mock server for
   testing.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the pluggable server interface and contracts only after behavior
stabilizes and with explicit user approval.
