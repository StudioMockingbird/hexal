# RFC 0198: Web Server — HTTP/1 Parser Integration

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; llhttp selected for the first cut; upstream revision
  and final qualification remain open
- Created: 2026-09-15
- Updated: 2026-09-29
- Depends on: implemented RFC 0039 (C interoperability) and RFC 0052 (C
  compiler backend)
- Coordinates with: RFC 0210 (built-in HTTP types), RFC 0194 (default server
  backend), and the C23 component build contract
- Does not add: server lifecycle, connection management, TLS, routing, or
  middleware

## Motivation

RFC 0210 defines the user-facing HTTP server API. RFC 0194 specifies the
default backend. This RFC selects and constrains the private HTTP/1 parser used
by that backend. It is a pure C component with no libuv, Task, or Hexal
source-level API.

The selected parser is private to the default server backend; this RFC adds no
public Hexal or C parser API.

## First-cut parser choice

Use [llhttp](https://github.com/nodejs/llhttp) for the first HTTP/1 server cut.
Its stateful, incremental parser maps to the existing connection-Task model:
each connection owns one parser state, and that Task feeds each newly received
byte range after resuming from a libuv read. Parser callbacks run synchronously
inside that Task's call to llhttp; socket-runtime callbacks only complete I/O
and wake Tasks. Parser state is never shared between connections.

llhttp parses request start lines, headers, and HTTP/1 message framing, and
reports body data and completion through callbacks. The adapter retains or
copies callback data before its input buffer is reused, exposes body bytes
through RFC 0210's one-shot stream, and applies the strict RFC 9112 framing
policy. Lenient parser options remain disabled. Client-side response parsing
and request serialization are not public APIs in this server cut.

Before implementation, record the exact upstream release and commit, the
generated C/header source files and hashes, license, and qualification evidence.
Check in the generated C sources used by Hexal so user builds need neither
Node.js/npm nor a network fetch or code-generation step.

### Parser contract

- Parse HTTP/1.0 and HTTP/1.1 requests incrementally, including request line,
  headers, and message framing. RFC 0194 owns connection and response-write
  behavior; RFC 0210 owns public Request/Response types and body streams.
- Keep one mutable llhttp parser state per connection. Only that connection's
  Task may call or reset it, after resuming from I/O.
- Treat callback data as borrowed from the input range. Retain or copy every
  field/body byte that outlives that range before the buffer is reused.
- Distinguish incomplete input, a complete request head, body progress,
  message completion, and malformed input. Pause at message completion until
  the previous response finishes; preserve already-read bytes for the next
  request.
- Use strict parser settings and apply RFC 9112 framing rules before handler
  dispatch. Reject `Transfer-Encoding` together with `Content-Length`,
  conflicting/invalid lengths, and unsupported transfer codings; close after
  the error response rather than guessing a boundary.
- Use the single request-line, header-count, and total-header limits defined by
  RFC 0210's `ServerConfig`. A read-buffer chunk size is not a request-size
  limit.
- Request-body bytes remain arbitrary bytes; parsing does not decode them as
  UTF-8.

### Out of scope

- Parsing HTTP responses or serializing HTTP requests; client-side HTTP is not
  part of the first server cut.
- Public parser objects or duplicate public definitions of `Request`,
  `Response`, or `Headers`.
- A handwritten start-line/header parser or independent message-framing rules.

### First-cut validation

- pinned generated source builds in the generated C23 component on supported
  targets without Node.js, npm, a network fetch, or package manager;
- each connection has independent parser state; partial request lines,
  headers, and bodies resume correctly across arbitrary read boundaries;
- socket-runtime callbacks never call llhttp or user handlers; parser callbacks
  execute only while the connection Task owns the parser;
- callback data is not used after its input range is released or recycled;
- completed requests pause parsing until their response completes, preserving
  any bytes already read for a later request;
- malformed lines, invalid headers, excessive limits, invalid lengths, and
  ambiguous transfer-framing combinations are rejected;
- fixed-length and chunked framing deliver the expected body bytes through the
  adapter; trailer fields are validated and discarded, never merged into
  request Headers;
- connection reuse and response serialization remain owned by RFC 0194;
- no parser allocation or libuv call occurs;
- ordinary and tagged C23 suites pass.

The legacy proposal from `Scope decision` through `C23 lowering` below is
historical only and is not an implementation requirement. The later Demand
rules, Required sweep, Validation, and Open questions sections state the
current contract.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| HTTP/1.0 request parsing | Pick up | Backward compatibility |
| HTTP/1.1 request parsing | Pick up | Primary use case |
| HTTP/1.0 response parsing | Pick up | Client-side testing |
| HTTP/1.1 response parsing | Pick up | Client-side testing |
| Header parsing (case-insensitive) | Pick up | Required for HTTP |
| Chunked transfer encoding | Pick up | Required for streaming |
| Content-Length body | Pick up | Required for fixed bodies |
| Transfer-Encoding: chunked | Pick up | Required for streaming |
| Request body parsing | Pick up | Required for POST/PUT |
| Response body parsing | Pick up | Required for client |
| Trailer headers | Skip in v1 | Rare, can be added later |
| HTTP/2 framing | Skip in v1 | Different wire protocol |
| HTTP/3 (QUIC) | Skip in v1 | Different transport |
| WebSocket framing | Skip in v1 | Different protocol |
| HTTP digest authentication | Skip in v1 | Deprecated |
| HTTP basic authentication | Skip in v1 | Library concern |

## Source surface

### Parser

```hexal
import
    Http from "std/http"
end

parser := Http.Parser.new()
```

```text
type Parser is struct
    -- internal representation
end

fun Parser.new() -> Parser
```

### Parsing

```text
method Parser.execute(data: String) -> ParseResult
```

```text
type ParseResult is
    Complete(Request) |
    Partial |
    Error(ParseError)
end
```

- `Complete(request)`: a full request was parsed.
- `Partial`: more data is needed.
- `Error(error)`: a parse error occurred.

### Request

```text
type Request is struct
    method: String,
    path: String,
    version: (UInt8, UInt8),
    headers: Headers,
    body_start: String,
    body_complete: Bool,
    content_length: Size | Nil,
    transfer_encoding_chunked: Bool,
end
```

### Headers

```text
type Headers is struct
    entries: Slice<Header>,
end

type Header is struct
    name: String,
    value: String,
end

method Headers.get(name: String) -> String | Nil
method Headers.get_all(name: String) -> Slice<String>
method Headers.set(name: String, value: String)
method Headers.add(name: String, value: String)
method Headers.contains(name: String) -> Bool
method Headers.remove(name: String)
method Headers.iter() -> Headers.Iterator
method Headers.len() -> Size
```

- Header lookup is case-insensitive.
- `get` returns the first matching header.
- `get_all` returns all matching headers (for `Set-Cookie`).
- Headers are stored as a flat array; O(n) lookup.

### Body reading

For `Content-Length` bodies:

```text
method Parser.read_body(
    request: Request,
    data: String,
) -> BodyResult
```

```text
type BodyResult is
    Complete(String) |
    Partial(String) |
    Error(ParseError)
end
```

- `Complete(body)`: the full body was read.
- `Partial(partial_body)`: more data is needed; returns what was read
  so far.
- `Error(error)`: a read error occurred.

For chunked bodies:

```text
method Parser.read_chunked_body(
    request: Request,
    data: String,
) -> ChunkedResult
```

```text
type ChunkedResult is
    Complete(String, Headers) |
    Partial(String) |
    Chunk(String) |
    Error(ParseError)
end
```

- `Complete(body, trailers)`: the full chunked body was read, including
  trailer headers.
- `Partial(partial_body)`: more data is needed.
- `Chunk(chunk)`: one complete chunk was decoded.
- `Error(error)`: a decode error occurred.

### Response parsing

```text
type Response is struct
    version: (UInt8, UInt8),
    status: UInt16,
    reason: String,
    headers: Headers,
    body_start: String,
    body_complete: Bool,
    content_length: Size | Nil,
    transfer_encoding_chunked: Bool,
end

method ResponseParser.execute(data: String) -> ResponseParseResult
```

- `ResponseParser` is identical to `Parser` but parses HTTP responses
  instead of requests.

### Serialization

```text
fun serialize_request(
    method: String,
    path: String,
    version: (UInt8, UInt8),
    headers: Headers,
    body: String | Nil,
) -> String

fun serialize_response(
    version: (UInt8, UInt8),
    status: UInt16,
    reason: String,
    headers: Headers,
    body: String | Nil,
) -> String
```

- `serialize_request` produces a complete HTTP request message.
- `serialize_response` produces a complete HTTP response message.
- `Content-Length` is added automatically when `body` is provided.
- `Transfer-Encoding: chunked` is added when `body` is `Nil` and the
  caller uses chunked writing.

### Chunked serialization

```text
fun serialize_chunk(data: String) -> String
fun serialize_chunks_end() -> String
```

- `serialize_chunk` produces one chunk with size prefix.
- `serialize_chunks_end` produces the zero-length terminator.

## Parser state machine

The parser is a byte-at-a-time state machine that processes input
incrementally. It does not allocate memory, call libuv, or depend on
any runtime facility.

### States

```text
START -> METHOD -> PATH -> VERSION -> HEADER_KEY -> HEADER_VALUE
     -> HEADERS_DONE -> BODY_CONTENT_LENGTH -> BODY_CHUNKED -> BODY_DONE
     | ERROR
```

### State transitions

| State | Condition | Next state |
| --- | --- | --- |
| START | First byte of request line | METHOD |
| METHOD | Space | PATH |
| PATH | Space | VERSION |
| VERSION | `\r` | HEADER_KEY |
| HEADER_KEY | `:` | HEADER_VALUE |
| HEADER_VALUE | `\r` | HEADER_KEY (next header) or HEADERS_DONE |
| HEADERS_DONE | `Content-Length` present | BODY_CONTENT_LENGTH |
| HEADERS_DONE | `Transfer-Encoding: chunked` | BODY_CHUNKED |
| HEADERS_DONE | Neither | COMPLETE |
| BODY_CONTENT_LENGTH | Read `content_length` bytes | COMPLETE |
| BODY_CHUNKED | Read chunk size | BODY_CHUNKED (read chunk) |
| BODY_CHUNKED | Chunk size is 0 | COMPLETE |
| Any | Invalid byte | ERROR |

### Error types

```text
type ParseError is
    InvalidRequestLine |
    InvalidHeaderName |
    InvalidHeaderValue |
    InvalidChunkSize |
    BodyTooLarge |
    HeadersTooLarge |
    InvalidVersion
end
```

### Limits

| Limit | Default | Configurable |
| --- | --- | --- |
| Maximum request line length | 8 KiB | Yes |
| Maximum header name length | 8 KiB | Yes |
| Maximum header value length | 8 KiB | Yes |
| Maximum total header size | 64 KiB | Yes |
| Maximum body size | 1 MiB | Yes |
| Maximum chunk size | 64 KiB | Yes |

## Incremental parsing

The parser supports incremental input across multiple calls:

```hexal
parser := Http.Parser.new()
result := parser.execute(chunk1)
-- result is Partial
result := parser.execute(chunk2)
-- result is Partial
result := parser.execute(chunk3)
-- result is Complete(request)
```

- The parser maintains internal state between calls.
- Partial results include any data consumed from the input.
- The caller is responsible for buffering incomplete data between calls.

## C23 lowering

The parser is a pure C library. It does not call libuv, malloc, or any
runtime facility. All state is held in caller-provided buffers or on the
stack.

```c
typedef struct hex_http_parser {
    int state;
    size_t bytes_parsed;
    size_t content_length;
    int transfer_encoding;
    /* internal fields */
} hex_http_parser;

void hex_http_parser_init(hex_http_parser *parser);
int hex_http_parser_execute(hex_http_parser *parser,
                            const char *data, size_t len,
                            hex_http_request *request);
```

- The parser is re-entrant; multiple parsers can exist simultaneously.
- The parser does not allocate memory; all output is written to
  caller-provided structures.

## Demand rules

- The private parser adapter and pinned parser source are selected by the
  default server backend when `Http.serve` is reachable. A custom backend
  selects them only if it declares that dependency.
- The parser component has no Hexal public API and does not select libuv,
  native bootstrap, or the event bridge by itself.
- Programs that do not use the default backend or an adapter declaring llhttp
  do not select this parser component.

## Required sweep

- pin, license, vendor, and hash the exact llhttp release, commit, and generated
  source snapshot;
- keep the parser adapter private to the server component;
- remove the competing handwritten parser and public parser/serializer API;
- feed new byte ranges into per-connection parser state and enforce RFC 0210
  limits;
- validate framing before dispatch and hand body ownership to RFC 0194;
- preserve callback-data lifetimes across input-buffer reuse;
- select the parser only when an HTTP server backend is reachable;
- update the workbench snippet and generated-C manifest only if the public
  example or generated artifact changes;
- update `docs/reference.md` only for a changed public contract owned by
  RFC 0210.

## Validation

This section is exhaustive for the private server parser integration:

- pinned generated source builds for each qualified target without Node.js,
  npm, a network fetch, or package manager;
- per-connection parser state handles complete, incomplete, and malformed
  HTTP/1.0 and HTTP/1.1 requests across arbitrary read boundaries;
- parser state is never concurrently accessed by a libuv callback and its
  connection Task;
- malformed request lines, invalid headers, configured limit violations,
  invalid lengths, unsupported transfer codings, and ambiguous framing are
  rejected before handler dispatch;
- equal duplicate Content-Length values are handled per RFC 9112, while
  conflicting values and Transfer-Encoding plus Content-Length are rejected;
- callback data is copied or retained before its backing input range is
  released or recycled;
- fixed-length and chunked request bodies produce the exact byte sequence;
  trailer fields are not merged into ordinary request headers;
- parser state and settings are caller-owned; llhttp performs no parser-owned
  dynamic allocation and makes no libuv calls;
- client response parsing and request serialization are not exposed as APIs;
- ordinary and tagged C23 suites pass.

## Open questions

1. Pin the exact llhttp release/commit and its generated C/header source list,
   hashes, and license before implementation.
2. Confirm whether HTTP/1.0 connection persistence is reported by the parser
   adapter or derived by the RFC 0194 connection state machine.

## Reference synchronization

This RFC adds no public parser API. Update `docs/reference.md` only if
implementation changes a public HTTP contract owned by RFC 0210; update it
after behavior stabilizes and before this RFC is marked implemented or closed.
