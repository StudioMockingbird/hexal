# RFC 0194: Web Server — Lowering over the Task-Aware Network Runtime

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; active design for the default HTTP backend;
  implementation not started
- Created: 2026-09-15
- Updated: 2026-09-29
- Depends on: RFC 0144 (Task-aware socket runtime contract and adapter), RFC
  0198 (HTTP parsing and serialization), RFC 0208 (backend contract), RFC 0210
  (web server surface), and the implemented RFCs 0145 (libuv runtime), 0146
  (mimalloc), 0168 (libuv capability arc), and 0184 (atomic print)
- Coordinates with: RFC 0195 (TLS 1.3 integration) for HTTP connection policy;
  RFC 0144 owns the shared Task-aware socket layer
- Does not add: language syntax, source semantics, or public APIs

## Motivation

RFC 0210 defines the user-facing HTTP server API. RFC 0144 defines the
Task-aware socket and timer boundary; RFC 0145 supplies its libuv foundation.
This RFC specifies HTTP server behavior over that boundary, including
connection sequencing, parsing integration, response writing, and backpressure.
It does not own libuv handles, callbacks, or Task wait-record mechanics.

## Design principles

1. **C23-first.** HTTP parsing and response serialization operate on byte
   buffers. The HTTP implementation uses RFC 0144's runtime operations rather
   than depending directly on libuv.
2. **Shared runtime.** Network operations use RFC 0144's Task-aware socket
   runtime, backed by RFC 0145; the HTTP layer does not own a loop.
3. **Runtime boundary.** The HTTP implementation uses no libuv handle or
   callback; libuv remains private to the RFC 0144/0145 runtime layers.
4. **Demand-driven.** Programs that do not use the HTTP server emit no HTTP
  parser, no TCP runtime component, and no libuv TCP handle code.
5. **Backpressure.** Bounded buffers and explicit high-water marks prevent
   unbounded memory growth under slow clients.

## First-cut backend contract

- `Http.serve(config, router)` uses this backend by default. It implements the
  RFC 0208 backend contract; it is not a privileged alternate API.
- `ServerConfig`, route dispatch, built-in `Request`/`Response`, and body
  streams are defined by RFC 0210. This RFC owns their HTTP-specific runtime
  behavior over RFC 0144's socket operations.
- A connection Task parses the request head, creates a one-shot byte body
  stream, dispatches through the Router, writes the returned Response, and
  then reuses the connection only when HTTP framing permits it.
- A connection has at most one handler and one response write in flight.
  Requests already buffered for that connection are processed in arrival
  order; their bytes are preserved while the prior response completes.
- Body reads and writes use RFC 0144's Task-aware socket operations. They never
  block a scheduler worker or use the libuv worker pool for steady-state socket
  I/O.
- Runtime allocation uses Hexal's allocator boundary, not C `malloc`.
- The first cut is HTTP/1.0 and HTTP/1.1 without TLS. TLS remains deferred.
- Read chunk capacity is an implementation detail, not a request-size limit.
  Header and body limits have one owner in `ServerConfig`; filling a read
  chunk causes incremental parsing, not an automatic 413.
- A bounded write queue applies backpressure by parking the producer until
  the runtime drains data. It does not close a healthy connection merely
  because a client is slow.

## Runtime boundary

The default backend obtains its listener and connected streams from RFC 0144's
Task-aware TCP runtime. It uses that contract for accept, read, write, shutdown,
and close. RFC 0144 defines native-handle and pending-operation lifetimes,
Task parking and wakeup, and completion/timeout/cancellation races; RFC 0145
implements the libuv handle and event machinery beneath that contract.

This RFC owns the HTTP decisions made above that boundary: when to accept
connections, dispatch connection Tasks, read or stop reading, reuse or close a
connection, and how server shutdown interacts with active requests. No
`uv_*` handle or callback appears in the HTTP adapter's Hexal-facing runtime
contract.

## HTTP parser

RFC 0198 selects llhttp and owns its parser contract. The default backend owns
one llhttp state per connection and feeds it only from that connection's Task
after each runtime read completes. Runtime callbacks do not execute parser or
handler code. The parser has no libuv dependency. The C structs, custom state
machine, and parser-owned body helpers in the earlier subsections below are
superseded by RFC 0198 and must not be implemented.

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

### Request body streams

The request body is not accumulated into one `String`. The backend exposes a
one-shot byte stream on `Request.body`:

- `Content-Length` bounds the stream to exactly the declared number of octets.
- `Transfer-Encoding: chunked` is decoded incrementally; trailer fields are
  validated and discarded, not exposed as ordinary headers.
- An absent body-framing field means an empty request body.
- Invalid or ambiguous framing produces 400 and closes the connection.
- Exceeding `ServerConfig.max_body_bytes` produces 413 and closes the
  connection.
- A handler that does not consume the body cannot return the connection to
  keep-alive reuse until the remaining body is drained safely or the
  connection is closed.

Body bytes are consumed through RFC 0210's one-shot byte stream. Reads use
RFC 0144's Task-aware operations; this RFC does not define whole-body C helpers
or expose socket handles.

## Response writing

The backend serializes the built-in Response. It honors HTTP body-forbidden
statuses and HEAD requests, and does not emit both `Content-Length` and
`Transfer-Encoding`. A known-length body uses `Content-Length`; an unknown-size
stream uses chunked transfer coding for HTTP/1.1. Each Task-aware write
completion resumes the producer Task through RFC 0144. A slow client applies
backpressure instead of causing an unbounded queued response or forced close.

## Accept and connection Tasks

The default backend accepts through RFC 0144's Task-aware listener operation.
Each accepted connection is handled by an ordinary Task, which reads request
bytes, invokes the user handler, writes the response, and either reuses or
closes the connection according to HTTP framing and server policy. The runtime
operation parks and resumes that Task; the HTTP layer does not run in a libuv
callback.

## Backpressure

- Each connection has a bounded read buffer; its capacity is not a protocol
  limit and may be smaller than a complete request header.
- If the configured total-header limit is exceeded, the server sends 431
  (Request Header Fields Too Large) and closes the connection.
- If the configured body limit is exceeded, the server sends 413 (Content Too
  Large) and closes the connection.
- Each connection has a bounded write queue.
- A full write buffer parks the response producer until queued bytes drain;
  cancellation or a write deadline terminates the response.
- Exact defaults and overrides belong to RFC 0210's `ServerConfig`, not
  duplicate `Server.set_*` methods.

No separate public setters are added by this backend specification.

## Memory model

- HTTP parser state is stack-resident or Stash-allocated per connection.
- Public Request/Response storage follows RFC 0210's opaque built-in
  representation and explicit manual cleanup contract.
- Parser spans borrow the read buffer; no span survives buffer reuse. A body
  stream owns or pins its backing buffer until consumed, cancelled, or closed.
- Response stream chunks remain live until the corresponding runtime write
  completes.
- Native socket handles and pending-operation storage follow RFC 0144's
  lifetime contract; this RFC owns only HTTP connection and server state.
- No persistent global HTTP state beyond server configuration and the runtime
  listener value.

## Task integration

- Each accepted connection is handled by one ordinary Task.
- Task-aware reads and writes park without holding a scheduler worker; RFC 0144
  owns event delivery and wake publication.
- The Task is completed (`hex_task_complete`) after the connection closes.
- Concurrent connection Tasks share no mutable state; each has its own
  parser, headers, and buffers.

## Demand rules

- `Http.serve` selects the default server and HTTP parser components and
  depends on RFC 0144's Task-aware TCP runtime.
- `Http.serve_with` selects only its specified custom backend and that
  backend's declared dependencies; it does not imply llhttp or libuv.
- Merely naming or constructing built-in `Request`/`Response` values does not
  select the network backend.
- The HTTP parser adapter selects the pinned llhttp component; it does not
  select TCP operations, timers, or libuv independently.
- A program using only raw TCP (without HTTP) does not select the HTTP
  parser.
- A program using only the HTTP parser (without a server) does not select
  the server lifecycle component.

## Required sweep

- llhttp generated source snapshot and private adapter in `hexal/http.c`;
- response serialization and chunked encoding in `hexal/server.c`;
- connection dispatch and reuse over RFC 0144's listener/stream operations;
- backpressure, buffer limits, and timeout integration;
- demand discovery for HTTP parser and server components;
- HTTP-level shutdown and deadline policy over RFC 0144's runtime operations;
- allocator ownership and cleanup for parser spans, streams, and HTTP state;
- workbench snippet and manifest entries;
- synchronize the public behavior owned by RFC 0210 in `docs/reference.md`
  after it stabilizes and before closure.

## Validation

This section is exhaustive for the default backend; parser details are owned
by RFC 0198 and public behavior by RFC 0210:

- `Http.serve` selects the HTTP server and parser adapter and depends on the
  Task-aware TCP runtime; unrelated programs do not select those components;
- each accepted connection is handled by an ordinary Task with isolated
  parser and buffer state;
- socket reads and writes park the Task without occupying a scheduler worker
  or libuv worker-pool thread;
- the connection Task incrementally feeds newly read bytes to its RFC 0198
  parser state, and callback data remains valid for every Request consumer;
- request-body framing follows RFC 9112; ambiguous framing is rejected and
  the connection is not reused;
- exceeding the configured header limit returns 431; exceeding the configured
  body limit returns 413; filling an I/O chunk alone returns neither;
- a full write queue parks its producer until progress, cancellation, or
  deadline rather than growing without bound or closing solely due to
  backpressure;
- response framing honors the request method, body-forbidden statuses, and
  known versus streaming body length, and never emits both `Content-Length`
  and `Transfer-Encoding`;
- listener and connection shutdown obey the RFC 0144 runtime ownership and
  completion contract, without stale Task wakeups;
- shutdown stops accepting, allows in-flight work until its deadline, then
  closes remaining handles without stale Task wakeups;
- demand rules select no unrelated HTTP or network components;
- HTTP parsing has no libuv dependency and uses no parser-owned allocation;
- existing Task, Channel, Mutex, IO, and print behavior is unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether the server yields after each response or after a bounded batch of
   immediately available requests; this is HTTP scheduling policy, not socket
   runtime behavior.

## Reference synchronization

Implementation updates `docs/reference.md` after backend behavior stabilizes
and before this RFC is marked implemented or closed.
