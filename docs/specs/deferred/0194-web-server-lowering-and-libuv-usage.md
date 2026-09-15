# RFC 0194: Web Server — Lowering and libuv Usage

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on RFC 0144 (network runtime)
  implementation landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0144 (high-throughput network runtime), RFC 0210 (web server
  syntax and semantics), and the implemented RFCs 0145 (libuv runtime), 0146
  (mimalloc), 0168 (libuv capability arc), and 0184 (atomic print)
- Coordinates with: RFC 0195 (TLS 1.3 integration) for the shared socket
  layer and libuv handle lifecycle
- Does not add: language syntax, source semantics, or public APIs

## Motivation

RFC 0210 defines the user-facing HTTP server API. This RFC specifies how that
API maps to libuv facilities, C23 generated code, and the existing runtime
substrate. The lowering must preserve the async runtime's guarantees: no
scheduler worker blocks on a socket wait, every libuv callback resumes user
code only through Task scheduling, and one program-wide event loop is the
sole reactor.

## Design principles

1. **C23-first.** HTTP parsing and response writing are pure C functions
   operating on byte buffers. No libuv dependency enters the HTTP parser
   itself; only the socket I/O layer uses libuv.
2. **One event loop.** All network I/O routes through the existing program-wide
   `uv_loop_t` on one dedicated native thread (implemented by RFC 0145).
3. **No libuv in Hexal source.** The generated C uses libuv handles and
   callbacks internally; no `uv_*` type, handle, or callback appears in
   Hexal source or generated public module headers.
4. **Demand-driven.** Programs that do not use the HTTP server emit no HTTP
   parser, no TCP listener, and no libuv TCP handle code.
5. **Backpressure.** Bounded buffers and explicit high-water marks prevent
   unbounded memory growth under slow clients.

## Component model

The HTTP server adds three runtime components:

| Component | Header | Source | Demand trigger |
| --- | --- | --- | --- |
| HTTP parser | `hexal/http.h` | `hexal/http.c` | Any reachable `Request` or `Headers` type |
| Server lifecycle | `hexal/server.h` | `hexal/server.c` | `Server.new` or `Server.serve` |
| TCP listener | `hexal/tcp_listener.h` | `hexal/tcp_listener.c` | `listen` or `Server.new` |

Each component is emitted only when its demand trigger is reachable. A program
that only reads requests (as a client) does not select the server component.

## libuv handle lifecycle

### TCP listener

```c
uv_tcp_t server_handle;
uv_tcp_init(uv_default_loop(), &server_handle);
```

- The handle is owned by the `Server` value.
- `uv_listen` registers the connection callback.
- On each accept, `uv_accept` produces a new `uv_tcp_t` handle for the
  connection.
- The listener handle is closed only during graceful shutdown via
  `uv_close`.

### TCP connection

```c
uv_tcp_t connection_handle;
uv_tcp_init(uv_default_loop(), &connection_handle);
```

- One `uv_tcp_t` per accepted connection.
- The handle is closed after the response is sent and the connection is
  released.
- Read operations use `uv_read_start` with a `alloc_cb`/`read_cb` pair;
  the `alloc_cb` provides a stack or stash-allocated buffer, and the
  `read_cb` feeds the HTTP parser incrementally.
- Write operations use `uv_write` with a single buffer or a vectored
  `uv_write_t` + `uv_buf_t[]`.

### Shutdown

```c
uv_shutdown_t shutdown_req;
uv_shutdown(&shutdown_req, &connection_handle, shutdown_cb);
```

- Graceful shutdown sends a shutdown request, waits for in-flight writes
  to complete, then closes the handle.
- A hard timeout calls `uv_close` directly, aborting pending operations.

## HTTP parser

The HTTP parser is a pure C state machine operating on byte buffers. It does
not call libuv, malloc, or any runtime facility. It is a modified version of
the `llhttp` parser (MIT licensed, used by Node.js) or an equivalent
hand-written parser.

### Parser interface

```c
typedef struct hex_http_parser {
    int state;
    size_t bytes_parsed;
    /* internal fields */
} hex_http_parser;

typedef struct hex_http_request {
    hex_string method;
    hex_string path;
    hex_http_version version;
    hex_http_headers headers;
    int content_length;       /* -1 for chunked */
    int transfer_encoding;    /* 0 = none, 1 = chunked */
} hex_http_request;

void hex_http_parser_init(hex_http_parser *parser);
int hex_http_parser_execute(hex_http_parser *parser,
                            const char *data, size_t len,
                            hex_http_request *request);
```

### Parse states

```text
INIT -> METHOD -> PATH -> VERSION -> HEADER_KEY -> HEADER_VALUE
     -> HEADERS_DONE -> BODY_CONTENT_LENGTH -> BODY_CHUNKED -> COMPLETE
     | ERROR
```

- `hex_http_parser_execute` returns:
  - `0`: more data needed
  - `1`: request complete
  - `-1`: parse error

- On parse error, the caller sends a 400 response and closes the
  connection.

### Header storage

```c
typedef struct hex_http_headers {
    hex_http_header *entries;
    size_t count;
    size_t capacity;
} hex_http_headers;

typedef struct hex_http_header {
    hex_string key;
    hex_string value;
} hex_http_header;
```

- Headers are stored as a flat array; O(n) case-insensitive lookup.
- Maximum header count and maximum header size are configurable per
  server; defaults are 100 headers and 8 KiB per header.

### Body reading

For `Content-Length` bodies:

```c
int hex_http_body_read_content_length(
    hex_http_parser *parser,
    uv_tcp_t *handle,
    size_t content_length,
    hex_string *result
);
```

- Reads exactly `content_length` bytes, using the existing `uv_read_start`
  / `uv_read_stop` pattern.
- Returns the complete body as a Hexal `String` (heap-allocated UTF-8
  bytes).

For chunked bodies:

```c
int hex_http_body_read_chunked(
    hex_http_parser *parser,
    uv_tcp_t *handle,
    hex_string *result
);
```

- Reads chunks until the zero-length terminator.
- Each chunk is decoded and appended to the result.

## Response writing

### Fixed-length response

```c
int hex_http_response_write(
    uv_tcp_t *handle,
    uint16_t status_code,
    const hex_http_headers *headers,
    const char *body,
    size_t body_length
);
```

- Writes the status line, headers, and body in sequence.
- Uses `uv_write` with a single buffer containing the complete serialized
  response.
- Flushes the write buffer before returning.

### Chunked response

```c
int hex_http_response_write_chunked_start(
    uv_tcp_t *handle,
    uint16_t status_code,
    const hex_http_headers *headers
);

int hex_http_response_write_chunk(
    uv_tcp_t *handle,
    const char *data,
    size_t length
);

int hex_http_response_write_chunked_end(
    uv_tcp_t *handle
);
```

- `write_chunked_start` writes the status line, headers, and
  `Transfer-Encoding: chunked`.
- `write_chunk` writes one chunk with chunk-size prefix.
- `write_chunked_end` writes the zero-length terminator.

## Server accept loop

The accept loop is one libuv callback:

```c
static void on_new_connection(uv_stream_t *server, int status) {
    if (status < 0) return;

    uv_tcp_t *client = malloc(sizeof(uv_tcp_t));
    uv_tcp_init(uv_default_loop(), client);
    if (uv_accept(server, (uv_stream_t *)client) == 0) {
        /* spawn a Task to handle this connection */
        hex_task_spawn(hex_handle_connection, client);
    } else {
        uv_close((uv_stream_t *)client, on_close);
    }
}
```

- `hex_handle_connection` is the Task entry point; it reads the request,
  invokes the user handler, writes the response, and closes the connection.
- The Task parks during `uv_read_start` / `uv_write` and is woken by
  libuv callbacks.
- No libuv worker pool threads are used for socket I/O.

## Backpressure

- Each connection has a bounded read buffer (default 8 KiB).
- If the read buffer fills before the parser completes, the server sends
  413 (Payload Too Large) and closes the connection.
- Each connection has a bounded write buffer (default 64 KiB).
- If the write buffer fills (slow client), the server closes the
  connection.
- Configurable per-server:

```text
method Server.set_max_header_size(size: Size)
method Server.set_max_body_size(size: Size)
method Server.set_read_buffer_size(size: Size)
method Server.set_write_buffer_size(size: Size)
```

## Memory model

- HTTP parser state is stack-resident or Stash-allocated per connection.
- Headers are heap-allocated `String` values; freed when the connection
  closes.
- Request body is heap-allocated; freed when the response is sent.
- Response body is heap-allocated from the caller's `Heap`; freed after
  write completion.
- libuv handles are heap-allocated and freed on connection close.
- No persistent global HTTP state beyond the listener handle and server
  configuration.

## Task integration

- Each accepted connection spawns one Task via `hex_task_spawn`.
- The Task parks during `uv_read_start` and `uv_write` without holding
  a scheduler worker.
- libuv callbacks resume the Task through the existing event bridge
  (FIFO + `uv_async_t` wakeup).
- The Task is completed (`hex_task_complete`) after the connection closes.
- Concurrent connection Tasks share no mutable state; each has its own
  parser, headers, and buffers.

## Demand rules

- `Server.new` or `Server.serve` selects the server component, TCP
  listener component, HTTP parser component, and libuv event bridge.
- `Request`, `Headers`, `HttpMethod`, `HttpVersion`, `StatusCode`,
  `RequestBody`, `Response`, and `ResponseBody` select the HTTP parser
  component.
- The HTTP parser component selects libuv and native bootstrap.
- A program using only raw TCP (without HTTP) does not select the HTTP
  parser.
- A program using only the HTTP parser (without a server) does not select
  the server lifecycle component.

## Required sweep

- HTTP parser state machine and header storage in `hexal/http.c`;
- response serialization and chunked encoding in `hexal/server.c`;
- TCP listener accept loop and connection spawning in
  `hexal/tcp_listener.c`;
- backpressure, buffer limits, and timeout integration;
- demand discovery for HTTP parser, server, and TCP listener components;
- libuv handle lifecycle (init, accept, read, write, shutdown, close);
- integration with the existing event bridge and Task park/wake protocol;
- memory allocation and cleanup for parser state, headers, and bodies;
- workbench snippet and manifest entries;
- `docs/reference.md` HTTP backend contract after explicit approval.

## Validation

This section is exhaustive:

- HTTP parser accepts valid HTTP/1.0 and HTTP/1.1 request lines;
- HTTP parser rejects malformed request lines, oversized headers, and
  missing required fields;
- header case-insensitive lookup matches all standard HTTP header names;
- `Content-Length` body reading reads exactly the specified bytes;
- chunked body reading decodes all chunks including multi-chunk bodies;
- chunked body reading handles the zero-length terminator correctly;
- response writing produces valid HTTP/1.1 responses with status line,
  headers, and body;
- chunked response writing produces valid chunked transfer encoding;
- server accept loop spawns one Task per connection;
- concurrent connections do not share mutable state;
- connection read buffer fills trigger 413 and close;
- connection write buffer fills trigger connection close;
- header timeout, body timeout, and connection timeout return exact
  `Timeout` errors;
- graceful shutdown stops accepting and completes in-flight requests;
- hard timeout forces connection close after deadline;
- libuv handle init, accept, read, write, shutdown, and close follow
  the documented lifecycle;
- no libuv worker pool threads are used for socket I/O;
- demand rules select the correct components and no others;
- HTTP parser does not call libuv, malloc, or any runtime facility;
- existing Task, Channel, Mutex, IO, and print behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to use `llhttp` (proven, MIT-licensed) or a hand-written parser
   (simpler dependency story).
2. Whether chunked response writing should be a builder or a direct API.
3. Whether `Server` should support multiple listener handles (for binding
   to multiple ports).
4. Whether the accept loop should use `uv_tcp_keepalive` for connection
   health.
5. Whether to expose `Server.set_on_connection(handler)` as an alternative
   to the constructor parameter.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the HTTP backend contract, libuv handle lifecycle, and demand rules
only after behavior stabilizes and with explicit user approval.
