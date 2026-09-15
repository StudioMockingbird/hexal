# RFC 0198: Web Server — HTTP Request and Response Parsing

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the network runtime (RFC 0144)
  and TCP socket operations landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0144 (high-throughput network runtime), the implemented
  RFCs 0145 (libuv runtime), 0146 (mimalloc), and 0168 (libuv capability
  arc), and the current Task, String, Slice, Dict, and Error contracts in
  `docs/reference.md`
- Coordinates with: RFC 0210 (web server syntax), RFC 0194 (web server
  lowering), and the existing IO and print contracts
- Does not add: server lifecycle, connection management, TLS, routing, or
  middleware

## Motivation

RFC 0210 defines the user-facing HTTP server API. RFC 0194 specifies the
libuv lowering. This RFC defines the standalone HTTP parsing library: the
state machine that incrementally parses HTTP/1.1 requests and serializes
HTTP/1.1 responses. It is a pure C library with no libuv, no Task, and no
Hexal-specific dependencies.

The parser is usable from both Hexal and C code, and can be embedded in
non-Hexal programs (tests, benchmarks, tools).

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

- `Parser`, `Request`, `Response`, `Headers`, `ParseResult`,
  `BodyResult`, `ChunkedResult`, `ParseError`, `serialize_request`,
  `serialize_response`, `serialize_chunk`, and `serialize_chunks_end`
  select the HTTP parser component.
- The HTTP parser component is a pure C library; it does not select
  libuv, native bootstrap, or the event bridge.
- A program that uses only the parser (without a server) does not select
  the server lifecycle or TCP listener components.

## Required sweep

- HTTP parser state machine in `hexal/http_parser.c` and `hexal/http.h`;
- header storage and case-insensitive lookup;
- body reading for `Content-Length` and chunked transfer encoding;
- response parsing;
- request and response serialization;
- chunked serialization;
- parser limits and error handling;
- demand discovery for HTTP parser component;
- workbench snippet and manifest entries;
- `docs/reference.md` HTTP parser surface after explicit approval.

## Validation

This section is exhaustive:

- parser accepts valid HTTP/1.0 and HTTP/1.1 request lines;
- parser accepts valid HTTP/1.0 and HTTP/1.1 response lines;
- parser rejects malformed request lines (missing method, path, or
  version);
- parser rejects malformed headers (missing colon, invalid name, or
  invalid value);
- parser rejects oversized request line, header name, header value, and
  total headers;
- header case-insensitive lookup matches all standard HTTP header names;
- `Content-Length` body reading reads exactly the specified bytes;
- chunked body reading decodes all chunks including multi-chunk bodies;
- chunked body reading handles the zero-length terminator correctly;
- chunked body reading handles trailer headers;
- response parsing reads status code, reason phrase, headers, and body;
- serialization produces valid HTTP/1.1 requests and responses;
- serialization adds `Content-Length` automatically;
- chunked serialization produces valid chunked transfer encoding;
- incremental parsing works across multiple calls;
- parser state is preserved between calls;
- parser does not allocate memory;
- parser does not call libuv or any runtime facility;
- existing Task, Channel, IO, and print behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to use an existing HTTP parser (like `llhttp` or `http-parser`)
   or write a hand-optimized parser from scratch.
2. Whether `Headers` should be a separate type or a `Dict`-like type
   with case-insensitive keys.
3. Whether the parser should support HTTP/1.0 `Connection: close`
   detection and reporting.
4. Whether to expose the parser state for custom validation or keep it
   opaque.
5. Whether `serialize_request` should validate the request before
   serializing.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the HTTP parser types, state machine, and serialization contracts
only after behavior stabilizes and with explicit user approval.
