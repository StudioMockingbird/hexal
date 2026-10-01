# RFC 0194: Web Server — Lowering over the Task-Aware Network Runtime

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. The default HTTP backend is implemented over the Task-aware TCP
  runtime: one Task per connection, bounded storage, absolute phase deadlines,
  graceful stop, and the baseline recorded below; every Validation bullet maps to
  the evidence under Closure record
- Created: 2026-09-15
- Updated: 2026-10-01
- Depends on: RFC 0144 (Task-aware socket runtime contract and adapter), RFC
  0198 (HTTP parsing and serialization), RFC 0210
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

- Http.listen/Server.run use the single default backend. RFC 0208 is deferred;
  neither serve_with nor a public substitution ABI gates this implementation.
- `ServerConfig`, route dispatch, opaque `Request`/`Writer`, and body
  streams are defined by RFC 0210. This RFC owns their HTTP-specific runtime
  behavior over RFC 0144's socket operations.
- A connection Task parses the request head, creates a one-shot byte body
  stream, dispatches through Router<App> with explicit context and Writer, and
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

### Request body streams

The request body is not accumulated into one `String`. The backend exposes a
one-shot byte stream through `Request.read` (the public form in `docs/reference.md`):

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

The backend serializes the handler's Writer state. It honors HTTP body-forbidden
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

- HTTP parser state is connection-owned. A typed Stash is not a general
  byte allocator for String/List; no new allocator-polymorphism is implied.
- Request/Writer borrow connection state for the handler call under 0210;
  the server owns buffers and the handler owns its explicit Heap allocations.
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
- The connection Task returns after connection resources and pending native operations are quiescent.
- Connection parser and buffers are isolated. Router/configuration/application
  state may be shared; mutation follows the ordinary synchronization contract.

## Demand rules

- Http.listen/Server.run select the default server and HTTP parser components and
  depends on RFC 0144's Task-aware TCP runtime.
- Merely naming Request/Writer types does not
  select the network backend.
- The HTTP parser adapter selects the pinned llhttp component; it does not
  select TCP operations, timers, or libuv independently.
- A program using only raw TCP (without HTTP) does not select the HTTP
  parser.
- No public parser-only API exists; private adapter tests can select parsing
  without listener/connection lifecycle.

## Tightened backend contract

One Task is created per connection, not per keep-alive request. The receive buffer
retains unconsumed pipeline bytes; no unbounded request queue is built. Stop
reading at the configured buffer bound and resume only when space exists. A
header may span reads without exceeding its separate protocol limit.

The parser stops at head completion before handler dispatch. Body reads drive
that same parser; after body/message completion, the connection Task serializes
the response before admitting the next request. EOF and short bodies prevent reuse.
Unread body on handler return closes the connection, without draining. Emit
100 Continue only when the accepted handler starts body reads; unsupported
expectations -> 417, CONNECT/Upgrade -> 501 and close; HTTP/1.0 closes by default.

Protocol failures before dispatch produce the specified HTTP error response and
close that connection; they do not terminate the whole server. Handler-visible
body failures return Error. Listener/startup failures reach the serving caller.
After response commitment, failure closes the connection instead of writing a
second status line or substituting a 500. Internal diagnostics are not response text.

Head fields share bounded storage rather than requiring an owned allocation per
field. Request/head borrowing and synchronous Writer buffer lifetime follow 0210. Use the
existing allocator boundary; a request Stash is not silently passed where Heap
is required. Common header metadata may be cached while parsing. Gather head/body
writes and bounded buffering are candidates for reducing submissions; obey the
socket owner and completion lifetime. Evaluate whether existing write-all already
provides sufficient backpressure before adding a separate producer queue.

HTTP method-specific behavior and response status framing are centralized here,
including HEAD suppression and body-forbidden statuses. Request limits are not
applied to chunk framing or trailers accidentally; each has a bounded category.
Unknown-length HTTP/1.0 responses close-delimit; never send HTTP/1.1 chunked framing
to HTTP/1.0. 0198 reports parsed version/framing/connection facts; this backend
decides reuse and shutdown. Date formatting belongs to serialization; caching its
formatted value is a measured optimization, not one allocation per response by design.

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

- Http.listen/Server.run select the HTTP server and parser adapter and depend on the
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
- two pipelined requests, partial heads/bodies and input-buffer compaction preserve
  bytes and response order without an unbounded request queue or a Task per request;
- unread body, Expect handling and HTTP/1.0 persistence follow the approved policy;
- slow headers, slow uploads, stalled writes and idle keep-alive enforce separate
  configured deadlines; normal slow-client backpressure does not grow storage;
- exceeding connection/buffer bounds follows the approved admission policy;
- after response commitment, length mismatch or producer/native failure closes
  the connection, without a replacement 500 or stale access to source buffers;
- protocol errors isolate one connection; a later valid connection is served;
- the benchmark record includes allocations/copies/submissions per plaintext
  request and memory per idle connection, without an invented zero-allocation claim;
- existing Task, Channel, Mutex, IO, and print behavior is unchanged;
- ordinary gates, focused C23 fixtures and short C23 pass; exhaustive C23 runs
  require separate user consent.

## Implementation plan

### Phase 0: pin connection/server records

Consume the written 0144 deadline ABI, 0198 parser ABI and 0210 resource signatures.
Write a C ownership table and state diagram here: accepted, reading-head,
dispatching, reading-body, writing/committed, reusable, closing, quiescent.
Specify receive offsets/compaction, separate retained head storage, output buffer,
parser state, context/handler pointer, deadlines and active-connection accounting.
Define every error exit and exactly one owner for each allocation/native handle.
Exit: no state can resume a handler or recycle storage before native quiescence.

### Phase 1: admission and connection Tasks

Implement default listen/run over existing TCP and Task primitives. Freeze Router/
config at attachment, set TCP_NODELAY, enforce backlog/active limits, pause accepts
at capacity and return the slot on terminal cleanup. Create one Task per accepted
connection; no Task per keep-alive request and no libuv callback invokes a handler.
Exit: startup/accept failure, connection bounds and stopped admission satisfy the
named Validation cases without leaking the server or a connection slot.

### Phase 2: parsing and handler dispatch

Feed only new bytes to 0198; retain pipeline remainders and compact only safe
ranges. Stop at head-ready and call the typed handler with Ptr<App>, Request and
Writer. Request.read drives decoded body progress on the same connection parser.
Implement limits, 100-continue on first accepted read, unsupported expectations/
CONNECT/Upgrade, unread-body close, HTTP/1.0 close and deterministic route errors.
Exit: fragmented/pipelined inputs dispatch once in order, within bounded storage;
invalid protocol input isolates one connection rather than failing run globally.

### Phase 3: response serialization and backpressure

Implement Writer precommit head storage, status, header validation and exclusive
serializer ownership of CL/TE. First output commits; successful handler return
finalizes chunking/length. Suppress HEAD/body-forbidden payloads, detect length
mismatch and close on postcommit failures. Use one bounded synchronous producer
and existing write-all before considering a second queue or gathering fast path.
Exit: response bytes/framing are exact, input buffers are reusable after write
returns, and slow clients do not cause unbounded output or premature frees.

### Phase 4: deadlines, stop and teardown

Apply 0210's absolute phase deadlines through 0144. stop halts admission, lets
handlers finish until grace expires, then closes I/O without killing Tasks.
Join/quiesce handlers and native operations before publishing run/wait completion.
Release head/body/output/parser/connection storage and active slots exactly once.
Yield once after each reusable response; batching is a later measured change.
Exit: repeated stop, close/deadline races and CPU-cooperative shutdown obey the
approved lifecycle, with no stale wake or replacement postcommit status line.

### Phase 5: exhaustive validation and performance record

Map each Validation bullet to generated-text/unit or focused wire execution:
reuse/order, slow head/body/write/idle, committed errors, bounds, shutdown and
dependency isolation. Record 0144's release baseline including allocation/copy/
submission counts and idle-connection memory; do not invent competitive thresholds.
Exit: all named cases pass, generated C actually builds/runs in the focused lane,
and the recorded benchmark explains remaining measured costs separately.

### Phase 6: conformance and handoff

Review manifest movement for HTTP/server/network artifacts only; investigate
unrelated output movement. Run ordinary test/vet/build, focused HTTP/network C23
then short C23 with repository platform parallelism. Update the stabilized public
reference once with 0210; remove status work only when Validation is satisfied.
No exhaustive C23 run without consent. Rebuild hexal and restart hexal play.

## Pinned records

Owner: `compiler/corelib/runtime/server.c` (connection and server), consuming the
deadline operations of `packages/network.c` and the parser ABI of `http.h`.

### Ownership (Phase 0)

| Record | Allocated by | Used by | Released by |
| --- | --- | --- | --- |
| server state: configuration copy (host dropped), router and application pointers, listener handle, `lock`, `done` channel, `phase`, `stopping`, `listener_closed`, `active`, connection list, first failure | `listen` | `run`, `stop`, `wait`, connection Tasks, all under `lock` except the `stopping` atomic | `server.free`, which traps while `phase` is running |
| router attachment (`attached` count) | `listen` increments | the router refuses `route`, `mount`, and `free` while nonzero | `server.free` decrements |
| listener handle | `listen` | the accept loop | closed once by whichever of `stop`, the end of `run`, or `free` reaches it first (`listener_closed`) |
| connection record: receive buffer, output buffer, head bytes, head fields, Header view, parser state, exchange | the accept loop (`hex_http_connection_new`), all sized from the validated configuration, so no request path allocates | its connection Task only | that Task, after unlinking the record from the list and before returning the `active` slot, so `run` finishing means no connection memory remains |
| connection TCP handle | `accept` | its Task | closed once by the Task (`hex_http_close`) or earlier by shutdown; a second close sees a stale handle and returns `Closed` |
| exchange (the `Request` and `Writer` views) | embedded in the connection | one handler call | reset before each request |
| `active` slot | incremented by the accept loop before the Task is spawned | the limit check | decremented last by the Task |

### State machine (Phase 0)

One connection Task moves through: **accepted** (storage built, registered, `TCP_NODELAY`
set, Task spawned and detached) -> **reading-head** (`idle` set until the first byte;
`idle_timeout` then `header_timeout` from the first byte) -> **dispatching** (the parser
is paused at head completion; exact route, then mount, then `404` or `405`) ->
**reading-body** (only if the handler reads; `body_timeout` from the first read,
`100 Continue` on that first read) -> **writing** (the first flush commits the response;
each write carries `write_timeout`) -> **reusable** (response complete, keep-alive, body
fully consumed: `parser_next`, one `Task.yield`, back to reading-head) or **closing**
(shutdown, a bounded drain read when a body was left unread and no write failed, close) ->
**quiescent** (unlink, free, return the slot, complete the Task).

Every error exit: end of stream, an idle timeout, or `stop` while idle closes without a
response; a header deadline answers `408`; a parser error answers its status with
`Connection: close`, then drains briefly and closes; a handler `Error` before the first
write answers a generic `500` and closes, after it only closes; a native write failure marks
the exchange failed and closes without draining; a body left unread closes; failed
connection storage closes the accepted socket and keeps accepting; a failed Task spawn
unlinks, closes, and frees; an accept failure is recorded, ends the loop, and `run` returns it.

### Receive, head, and output storage

- One receive buffer with `recv_start` and `recv_end`. The parser consumes every byte of an
  incomplete message, so only a pipelined remainder is ever retained; the buffer resets when
  empty and is compacted by `memmove` only when it is full and `recv_start` is nonzero.
- Head bytes (request-line limit plus header limit) and field offsets are separate storage
  owned by the parser, valid from head completion until `parser_next`; the Header view is
  built lazily for `request.headers()`.
- The output buffer is `[head region][chunk prefix][body region][slack]`; the final head is
  assembled right-aligned so head, chunk header, and body leave in one contiguous write. A
  write that fills the buffer flushes with the write deadline and parks until the socket
  accepts it; there is no second queue.
- Admission: `run` accepts only while `active` is below `max_connections`, polling the slot
  once per `HEX_HTTP_POLL_NANOSECONDS`; the kernel backlog holds arrivals meanwhile. `stop`
  closes the listener, closes idle connections at once, lets in-flight requests finish until
  `shutdown_timeout`, then closes the rest, and `run` returns after `active` reaches zero.

## Closure record

| Validation bullet | Evidence |
| --- | --- |
| `listen` and `run` select the server and parser adapter and the TCP runtime; unrelated programs do not | `TestHttpServerSurfaceCompilesAndSelectsTheRuntime`, `TestHttpTypeOnlyAndRegistrationDoNotSelectTheServerRuntime`, `TestHttpServerWithoutFileServerSelectsNoFileComponent` |
| one ordinary Task per connection with isolated parser and buffers | the ownership table; `http-server-shutdown-and-admission-runs` (many connections), the concurrent runs in the baseline below |
| socket reads and writes park the Task, no scheduler worker or worker-pool job | `TestHttpServerSocketWaitsUseNoWorkerPoolJob`; the thread count in the baseline stays constant from zero to 10,000 idle connections |
| incremental parsing; callback data valid for every consumer | RFC 0198 closure; wire cases "pipelined" and "simple" |
| RFC 9112 framing; ambiguous framing rejected, connection not reused | wire cases "duplicate content length", "length and transfer coding" |
| `431` for the header limit, `413` for the body limit, a filled I/O chunk alone neither | wire cases "header bytes over limit", "header count over limit", "body over limit", "chunked body over limit"; "content-length body" with a small receive buffer |
| a full write parks its producer until progress, cancellation, or deadline | `http-server-phase-deadlines-run` (stalled download closed by the write deadline); the stalled and early readers of the file-transfer fixture |
| response framing: method, body-forbidden statuses, known versus streaming length, never both `Content-Length` and `Transfer-Encoding` | wire cases "head route", "no content", "chunked streaming", "declared length streaming", "unknown length over http 1.0" |
| listener and connection shutdown without stale wakeups; stop admits no more, finishes in-flight work to its deadline, then closes | `http-server-shutdown-and-admission-runs`; RFC 0144 close-cancel fixture |
| demand rules select no unrelated components | the ordinary tests above |
| parsing has no libuv dependency and no parser-owned allocation | RFC 0198 closure |
| two pipelined requests, partial heads and bodies, compaction, no unbounded queue, no Task per request | wire case "pipelined"; `hex_http_fill`; allocation counts in the baseline |
| unread body, Expect, HTTP/1.0 persistence | wire cases "expect continue", "unsupported expectation", "unknown length over http 1.0" |
| separate deadlines for slow headers, uploads, stalled writes, idle keep-alive | `http-server-phase-deadlines-run` |
| connection and buffer bounds follow the admission policy | `http-server-shutdown-and-admission-runs` |
| after commitment a length mismatch or native failure closes without a replacement `500` | wire cases "length shortfall", "length overrun", "handler error" |
| a protocol error isolates one connection; a later valid one is served | wire case "served after errors" |
| the benchmark record: allocations, copies, and submissions per plaintext request, memory per idle connection, no invented zero-allocation claim | Measurement record below |
| existing Task, Channel, Mutex, IO, and print behavior unchanged | the focused and short runs |
| ordinary gates, focused C23, short C23 | recorded in the implementing change |

## Measurement record

Method, hosts, and the throughput and latency tables are in RFC 0144's baseline; this record
carries the figures this RFC's Validation names. The counters ran on `x86_64-linux-gnu`
(WSL2, Clang 23.1.1, `-O2`) over 20,000 requests after 500 warm-up requests, with the
allocator and libuv entry points renamed to counting shims in the generated runtime.

- Per keep-alive plaintext request (one fixed route): 0.00 allocations, 2.00 loop-thread
  submissions (the read and the write), 1.00 `uv_write`, 1.00 `uv_read_start` and `stop`,
  2.00 `uv_timer_start`. A dynamic route that formats its body with `String.interpolate` adds
  exactly its own allocation (1.00, 53.5 B). Pipelining 16 requests per write cuts reads and
  submissions to 0.06 and 1.06 per request and leaves one `uv_write` per response. A request per
  connection costs 11.00 allocations (156,274 B, all freed) for the connection's storage and
  6.00 submissions. The zero is measured for this route and this configuration; it is not a
  claim about handlers, which allocate as they choose.
- Copies, by reading `server.c`: the request head is packed once into head storage; a decoded
  body is copied once into the caller's list; response header lines are copied into the output
  buffer's head region and moved once when the final head is assembled right-aligned, and the
  body is copied once into the body region; the buffer leaves in one write.
- Memory per idle keep-alive connection, Linux, resident growth after one request each:
  +176 to +180 MiB for 1,000 connections (180 to 184 KiB each) and +1,707 MiB for 10,000 (175
  KiB each) with the server configured for 20,000 connections; thread count unchanged (13)
  from zero to 10,000 connections. The configured
  storage per connection at the defaults is about 150 KiB (32 KiB receive buffer, 72 KiB
  output buffer, 40 KiB head storage, 6 KiB of field and Header tables); the remainder
  of the measured figure was not broken down. Memory is not returned to the system after the
  connections close within the observation window.
- Windows, working set after one request each: +36 MiB for 1,000 connections (37 KiB each)
  and +352 MiB for 10,000 (36 KiB each); threads unchanged (19). The two hosts report different
  counters and the figures were not reconciled.
- Slow clients: with 1,000 connections holding half a request head, 64 fast connections kept
  7,828 req/s (p99 17.3 ms) on Linux against 8,319 req/s (p99 13.7 ms) alone, and 21,569 req/s
  (p99 5.1 ms) on Windows against 31,259 req/s (p99 3.4 ms) alone; all 1,000 slow connections
  were answered `408` and closed by the header deadline.
- A defect found by this baseline: a second connection arriving before the first was accepted
  was never served (RFC 0144, accept transitions). The server fixtures had connected
  sequentially and never saw it; fixed with a regression fixture before the figures above were
  taken.

## Reference synchronization

Implementation added the HTTP behavior to `docs/reference.md` (`std/http`: Routing,
Lifecycle, Shutdown, Connections, Deadlines, Request rejection, Request body, Response,
Errors, Components). The rules above agree with it.
