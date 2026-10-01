# RFC 0210: Web Server — Syntax and Semantics

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. The approved public surface is implemented and recorded in
  `docs/reference.md`: opaque Request, Writer, Router<App>, Server<App>, ordinary
  Header and ServerConfig, `default_config`, `listen`, and the Router mount form
  owned by RFC 0200; every Validation bullet maps to the evidence under Closure
  record
- Created: 2026-09-15
- Updated: 2026-10-01
- Depends on: RFC 0144 (Task-aware socket runtime contract) and RFC 0198 (HTTP
  parsing and serialization)
- Coordinates with: RFC 0168 (libuv capability arc) for Event and Task
  integration, RFC 0186 (stdlib boundary) for module placement, RFC 0194
  (default backend), RFC 0195 (TLS), RFC 0200 (subsequent static file serving),
  deferred RFC 0208 (future backend substitution), and the current Task, Channel, Mutex, String, Slice,
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
| Static file serving | Separate subsequent milestone | Safe buffered serving under RFC 0200; not a dynamic-server prerequisite |
| Compression (gzip, brotli) | Skip in v1 | Library concern, uses C interop |
| Connection pooling | Skip in v1 | Client concern, separate RFC |
| Graceful shutdown | Pick up | Required for production servers |
| Timeout/deadline | Pick up | Required for production servers |

## Approved public contract

Use std.http and the existing minimal corelib mechanism for opaque Request,
Writer, Router<App> and Server<App> resources. ServerConfig and Header are ordinary
records. No Response-returning handler, ResponseBuilder, serve_with, language-wide
interface, async syntax, closure or generic allocator feature is added.
Header is an ordinary record with name: Slice<Byte> and value: Slice<Byte>;
both fields borrow the handler's head storage. Header count/bytes bound the record
table as well as retained field bytes. Server construction copies host and config
values once; it does not depend on caller string storage after listen returns.

The handler type is Fun<(Ptr<App>, Request, Writer): Nil | Error>. Each server
receives an initialized Ptr<App> explicitly; it remains live until run/wait finish
and server resources are freed. Concurrent handlers may read it; shared mutable
resources require existing Mutex/Atomic synchronization. Entry-environment functions
are not valid handlers. Configuration and router registrations freeze at listen;
duplicate method/path routes return InvalidInput. No registration mutation while
attached to a server. Router/free and application destruction follow server teardown.

### API signatures

Signatures below are contracts, not new function declaration syntax:

- Http.default_config(host: String, port: UInt16) -> ServerConfig.
- Http.Router<App>(heap: Heap) -> Router<App>.
- Router<App>.route(method: String, path: String,
  handler: Fun<(Ptr<App>, Request, Writer): Nil | Error>) -> Nil | Error.
- Router<App>.free(heap: Heap) -> Nil, only after all attached servers are freed.
- Http.listen<App>(heap: Heap, config: ServerConfig, router: Router<App>,
  app: Ptr<App>) -> Server<App> | Error.
- Server<App>.run() -> Nil | Error; runs once, parks its calling Task and returns
  after listener/connection native operations quiesce.
- Server<App>.stop() -> Nil; shared-handle, thread-safe, idempotent graceful stop.
- Server<App>.wait() -> Nil | Error; waits for run completion, observes its result;
  before run starts reports InvalidInput. stop before run causes run to finish
  without accepting. Only the run caller or post-run callers use free.
- Server<App>.free(heap: Heap) -> Nil, after run/wait and native cleanup.
- Request.method(), Request.target(), Request.path() -> Slice<Byte>.
- Request.headers() -> Slice<Header>, preserving ordered duplicate fields.
- Request.header(name: String) -> Slice<Byte> | Nil, first matching field;
  names compare ASCII case-insensitively; inspect headers() for all duplicates.
- Request.read(into: List<Byte>, max: Size) -> Size | EoS | Error; append decoded
  body bytes through the connection parser. max == 0 returns zero without I/O.
- Writer.status(code: UInt16) -> Nil | Error, before commitment; default 200.
- Writer.header(name: String, value: Slice<Byte>) -> Nil | Error; append, never
  eagerly combine. Validate token name and reject CR/LF/NUL in values. Copy into
  bounded response-head storage during this call; Set-Cookie stays separate.
- Writer.content_length(length: Size) -> Nil | Error, before commitment.
- Writer.write(bytes: Slice<Byte>) -> Nil | Error, bounded write-all with backpressure.

These are per-instance methods under the current call-shape and explicit-mutating
method rules. A copied Writer shares the same response state; no copy duplicates
commitment. Runtime serialization is single-producer: cross-Task writer use is
unsupported, not an implied concurrent streaming API.

Request, its Header slices and Writer are borrowed for this handler invocation.
They do not own connection storage and cannot be used after handler return or
sent to another Task. Apply local escape checks where provable; opaque aliases and
foreign escapes remain programmer responsibility, not a new lifetime system.
Explicitly copy bytes with Heap to retain them. Request head storage is separate
from reusable body/pipeline storage so body reads do not invalidate head access.
No per-header String allocation is required. Stash<T> is not a general byte Heap.

Writer.write either copies bounded bytes into connection output storage or waits
until native access finishes; after it returns the caller may reuse its input.
The handler owns and explicitly frees its allocated response/scratch values;
the server never silently takes ownership of String/List allocations. Before first
write, error returns permit generic 500. After commitment, any handler/native/length
failure closes the connection. Successful handler return finalizes the response;
there is no second public finish operation. Enforce declared byte length; unknown
length uses chunked HTTP/1.1 and close-delimited HTTP/1.0. HEAD and body-forbidden
statuses emit no body. User Headers cannot independently set CL/TE; framing is
owned by the serializer and content_length().

### Routing, parsing and lifecycle

Route on raw request-target path bytes minus query, without percent decoding or
absolute-URL allocation. Method tokens are case-sensitive and restricted to the
pinned llhttp supported method set. Exact routes precede mounts; longest matching
segment-boundary mount wins. Unknown path -> 404, known exact path/wrong method ->
405 with deterministic sorted Allow. No implicit GET-to-HEAD route alias.
Public Header access is lazy, preserves duplicates, and never merges framing evidence.
Absolute URI targets are parsed as needed under HTTP rules without building an owned
absolute URL per request; path/query extraction preserves raw encoded bytes.

Unread body on handler return closes the connection; no automatic drain. HTTP/1.0
closes by default. Reject all duplicate Content-Length, including identical values.
Send 100 Continue when an accepted handler first reads a nonempty expected body;
reject unsupported expectations with 417. CONNECT/tunneling receives 501 and close;
Upgrade is unsupported in v1 and receives 501 and close. No handler Task interrupt.
Disconnect closes I/O and returns Error; a CPU-bound handler remains cooperative.

Server.stop closes admission, lets in-flight handlers finish until the finite
shutdown deadline, then closes their I/O. It does not kill Tasks or free buffers
still in use. run/wait cannot return until those handlers and native operations
quiesce; a handler that never cooperates may delay completion beyond the I/O grace
period. ServerConfig and Router are immutable while attached to the server.

### Config defaults

Default values live in compiler/config, remain conservative resource ceilings,
and are copied by default_config into an ordinary ServerConfig. Required host and
port plus all fields below are supplied by default_config; config fields are
declared replaceable and users replace fields
before listen. Reject zero/invalid bounds and arithmetic-overflow combinations.
Timeouts use the existing Duration type, monotonic time and round-up conversion.

| ServerConfig field | Default |
| --- | --- |
| max_request_line_bytes | 8 KiB |
| max_header_bytes | 32 KiB |
| max_header_count | 100 |
| max_body_bytes | 8 MiB |
| max_trailer_bytes | 8 KiB |
| receive_buffer_bytes | 32 KiB |
| write_buffer_bytes | 64 KiB |
| max_connections | 4096 |
| backlog | 512 |
| header_timeout | 5 seconds |
| body_timeout | 30 seconds |
| write_timeout | 30 seconds |
| idle_timeout | 60 seconds |
| shutdown_timeout | 30 seconds |
| tcp_nodelay | true |

Header/body deadlines bound the whole corresponding phase, not sliding per-byte
timeouts. write_timeout bounds each write of the response, so a reader that makes
progress is never cut off; idle_timeout applies between requests. Shutdown timeout bounds grace, not forced Task destruction.
At the active-connection ceiling pause accepting until a slot returns; the kernel
backlog remains bounded. Body limits count decoded payload; trailer/head bounds
also bound chunk-extension/framing storage with incremental processing.

### End-to-end source example

This is proposed std.http usage; it becomes an executable acceptance fixture when
the module is implemented. It uses existing syntax and no source main:

```hexal
import
    Http from std.http
end

type App is struct
    greeting: String<32>
end

fun home(app: Ptr<App>, request: Http.Request, response: Http.Writer): Nil | Error do
    try response.content_length((^app).greeting.bytes().length())
    try response.write((^app).greeting.bytes())
    return nil
end

let heap = Heap()
let app = App(greeting = "Hello, world!")
let mut router = Http.Router<App>(heap)
defer router.free(heap)
try router.route("GET", "/", home)
let config = Http.default_config("127.0.0.1", 8080)
let server = try Http.listen<App>(heap, config, router, @app)
defer server.free(heap)
try server.run()
```

Another Task can call server.stop() through its shared handle while run is parked.
No callback from libuv directly invokes a Hexal handler.

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
| Connection timeout | `TimedOut` | `connection timed out` |
| Header timeout | `TimedOut` | `request header timed out` |
| Body timeout | `TimedOut` | `request body timed out` |
| Connection reset | `ConnectionReset` | `connection reset by peer` |
| Broken pipe | `BrokenPipe` | `broken pipe` |
| Address in use | `AddressInUse` | `address already in use` |
| Permission denied | `PermissionDenied` | `permission denied` |

The exact messages and the conditions a handler can observe are in `docs/reference.md`
(`std/http`, Errors). A malformed request line, an oversized head, and a header timeout are
answered on the wire (`400`, `431`, `408`) before any handler runs, so they are not Error
values; the Error rows that reach a handler or a `run` caller are the body, write, reset,
broken-pipe, address, and permission conditions.

## C23 lowering

Use existing corelib registration for std.http's native opaque resources and
ordinary config/Header records. No new syntax, analyzer, general interface or
foreign-callback feature is introduced. Router<App> specializes its context and
handler pointer statically; it needs no erased application context or closure.
Parsing and transport reuse llhttp and existing TCP. The current single reactor
is the baseline; context switching, stack reuse and loop sharding remain measured
and target-qualified follow-ups.

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

## Approved decision record

All twelve review recommendations were approved. The public contract above is
authoritative for this arc: single-reactor measured baseline; opaque corelib
resources/ordinary config; Router<App> and explicit Ptr<App>; writer-based handlers;
handler-scoped request borrowing/explicit Heap copies; shared server run/stop/wait;
raw paths/lazy duplicate headers; finite configurable defaults with TCP_NODELAY;
deferred 0208; subsequent safe buffered static files; pinned llhttp methods and
reject-all duplicate CL; unread-body close/HTTP1.0 close/explicit expectations.

RFC 0208 is deferred, not a prerequisite of this milestone. RFC 0200 is the
subsequent buffered-file milestone; zero-copy is excluded from its initial scope.
Remaining specification work is concrete runtime records, parser pinning and
target qualification, not reopening these approved design choices.

## Review findings retained without overclaiming

Current source proves event submissions, Task suspension, ready-queue locking and
ucontext/Fiber use. It does not prove relative server throughput, five syscalls per
read, a universal mapping limit, or superiority of picohttpparser. Reject those
claims as unmeasured, retaining the associated benchmark/qualification questions.
Static-file metadata does not prove strong content identity. The packet path and
buffer lifecycle, not Fetch resemblance, determine the common-path allocation cost.
Zig/Odin comparisons distinguish standard APIs from third-party HTTP frameworks;
do not import an entire ownership/interface model to imitate their surface.

## Required sweep

- opaque Request/Writer resources, borrowed Header spans and writer-owned framing;
- standard-library `Headers`, `Router`, `ServerConfig`, handler, and serving
  APIs;
- generic route registration and method/path dispatch;
- a single default backend; no serve_with or dependency on deferred RFC 0208;
- server lifecycle, graceful shutdown, and deadline management;
- integration with RFC 0144's non-blocking socket operations;
- workbench snippet and manifest entries;
- `docs/reference.md` HTTP surface after behavior stabilizes and before this
  RFC is closed.

## Validation

This section is exhaustive for the source-level contract; wire parsing is
validated by RFC 0198, HTTP server behavior by RFC 0194, and Task-aware socket
lifecycle by RFC 0144:

- the entry-module example compiles/runs without source main; server.run returns
  Nil on stop and Error on startup/runtime failure without leaking resources;
- Request/Writer and Server/Router use the approved opaque corelib model;
- Header lookup compares names case-insensitively and returns the first raw field;
  ordered duplicates and Set-Cookie remain separate without eager concatenation;
- Request body is byte-oriented and consumed once; Writer permits multiple bounded
  writes to the one response and is finalized by handler return;
- every config default equals the compiler/config constant; pre-listen overrides
  reach the backend unchanged, including TCP_NODELAY and finite shutdown grace;
- routes accept the pinned method set, reject other method tokens, preserve method
  case, reject duplicates and forbid mutation while attached;
- exact path-and-method match invokes the registered handler;
- unknown path returns 404; known path with an unsupported method returns 405
  with `Allow` listing the registered methods;
- listen/run select only the default backend; serve_with and Response-returning
  handler forms are not exposed;
- handler Error details are not exposed in the generated 500 response;
- the approved context-bearing handler can access initialized application state
  without becoming an entry-environment function value; shared mutation obeys
  existing synchronization rules;
- the approved construction, body, cleanup, stop and route-registration signatures
  have exact acceptance/rejection tests and a working stateful server example;
- request retention, response buffer lifetime, copies and cross-Task use follow
  the approved ownership table; no unproven opaque-resource lifetime is claimed;
- no response Error after commitment emits a second status line;
- raw versus normalized paths, method case, duplicate routes, mount precedence,
  limits/default overrides, and stream-close behavior follow the closed decisions;
- existing TCP, IO, Task, and Channel behavior is unchanged;
- ordinary gates, focused C23 fixtures and short C23 pass; exhaustive C23 runs
  require separate user consent.

## Implementation plan

### Phase 0: implementation map and private ABI agreement

Map new std.http types/functions into compiler/specdata/corelib.go,
compiler/corelib/, checker/corelib.go, generator/corelib.go and
generator/corelib_results.go patterns.
Locate config defaults in compiler/config/config.go, native source embedding and
component-demand discovery before adding parallel registries. Use 0194's written
resource layouts and 0144/0198 ABIs; do not invent separate timeout/parser state.
Exit: a per-type/function map identifies checker metadata, lowering, native owner,
declaration order, cleanup and the corresponding existing Validation bullet.

### Phase 1: register the exact public surface

Register Request, Writer, Router<App>, Server<App>, ordinary Header/ServerConfig,
default_config and listen. Implement only the approved signatures, generic App
identity and explicit-mutating call shapes; no Response/serve/serve_with aliases.
Place every default in compiler/config and copy it into default_config; validate
overrides at listen. Keep unsupported forms fail-closed rather than emitting stubs.
Exit: ordinary source tests cover exact calls, type errors, method set and config
validation; generated text contains the right native declarations and demand.

### Phase 2: router/context and resource lifetimes

Implement typed handler registration, copied registration bytes, duplicate errors,
freeze/attachment accounting and sorted Allow behavior. Store explicit Ptr<App>;
do not wrap an entry-environment function as Fun or erase the context type.
Implement Request head/header spans, body read lowering, Writer status/header/
length/write, and shared server handle operations through 0194's native state.
Apply local provable escape checks without claiming a general borrow checker.
Exit: context initialization, shared mutation rules, registration freeze and
borrow/write-return lifetime cases meet the approved Validation contract.

### Phase 3: integrate run/stop/wait and streaming

Land connected native behavior with 0194 rather than success-returning placeholders.
Verify one run caller, idempotent stop, pre-run wait error, stop-before-run,
completion result sharing and cleanup ordering. Handler return finalizes Writer;
precommit Error -> generic 500, postcommit Error -> close. No automatic frees of
handler-owned Heap values or general Task cancellation.
Exit: the stateful example and streaming/shutdown fixtures execute through the
same public API, with exact output and resource-lifetime assertions.

### Phase 4: source and generated-artifact conformance

Map all Validation bullets to tests in the existing integration facet and C23
fixture catalog; assert emitted declaration/include order, demand isolation,
typed handler/context representation, all defaults/overrides and omitted old APIs.
Mount behavior belongs to 0200 and cannot block closing the dynamic-server surface.
Exit: every accepted case is present, all rejection cases use the earliest phase,
and focused C23 runs prove the example/writer lifecycle beyond compiler success.

### Phase 5: reference, regression baseline and handoff

Review grammar changes explicitly: no new syntax is approved, so do not alter
GRAMMAR.ebnf merely to expose std.http. Update current signatures, memory/sharing,
config, routing and generated-C rules in reference.md once behavior stabilizes.
Add the stateful snippet and regenerate manifests only for verified intended output
changes; review artifacts by family. Run ordinary test/vet/build, focused C23 then
short C23. Preserve exhaustive-consent policy. Record common-path cost with 0194,
close only after Validation, rebuild hexal and restart hexal play at handoff.

## Pinned records

### Implementation map (Phase 0)

| Concern | Owner |
| --- | --- |
| opaque handles, Header record, ServerConfig record, generic `Router<App>`/`Server<App>` interning (one C pointer type per App) | `compiler/types/http.go`, `canonical.go`, `specid.go` |
| every default | `compiler/config/http.go`; `hex_http_default_config_raw` renders them once in `server.c` |
| function and method signatures, selection of the server, parser, and file-server components | rows `hex_http_*` in `compiler/specdata/corelib.go`; components `ComponentHTTP`, `ComponentServer`, `ComponentFileServer` in `specdata/components.go`; parameter and result kinds in `compiler/corelib/corelib.go`; call checking in `compiler/checker/corelib.go` and `generic_templates.go` |
| lowering of each call | `compiler/generator/corelib.go` and `corelib_results.go` (result-record adapters, `routeApplication`), `addon_adapters.go`, module templates in `generator/packages/module.h`; the handler is erased to one pointer plus a per-module invoke thunk `hex_http_router_route_invoke_<Module>` |
| component demand | `generator/http.go` (state flags `used`, `calls`, `config`, `router`, `route`, `free`, `runtime`, `files`, `parser`; `mergeServerInto`; `moduleServerComponent`); `generator/components.go` |
| native owner of every operation | `compiler/corelib/runtime/server.c` and `server.h`; parsing in `http.c`; static files in `fileserver.c` |
| declaration order | `Header` follows the byte slice it holds (`generator/slices.go`, `packages/slice.h`); every module header includes `hexal/server.h` before use |

Router and server lifetimes: a server attaches its router at `listen` and detaches at
`server.free`; the router refuses `route` and `mount` with `Busy` and traps in `free`
while attached. `Request` and `Writer` are two views of one exchange record embedded in
the connection and reset per request, so a copied `Writer` shares one response and neither
outlives the handler call.

One grammar consequence: `method` is a keyword, so the member name `method` is admitted
after `.` (`GRAMMAR.ebnf`, `compiler/parser/expressions.go`) for `request.method()`, without
making `method` a valid identifier elsewhere.

## Closure record

| Validation bullet | Evidence |
| --- | --- |
| entry-module example compiles and runs; `run` returns `Nil` on stop and `Error` on failure without leaks | ordinary `TestHttpEntryModuleServerCompiles` (the example above, verbatim); workbench snippet `networking-http-server`; `http-server-lifecycle-and-state-runs`, `http-server-shutdown-and-admission-runs` |
| opaque Request, Writer, Server, Router | ordinary `TestHttpRejectsInvalidServerUse` ("opaque request cannot be built"), `TestHttpTypeOnlyUseSelectsNoServerRuntime` |
| case-insensitive first-field lookup; ordered duplicates; Set-Cookie separate | wire cases "pipelined" and "cookies" in `http-server-routing-and-framing-runs` |
| byte-oriented single-consumption body; multiple bounded writes finalized by return | wire cases "content-length body", "chunked body", "expect continue" (reads); "chunked streaming", "declared length streaming" (writes) |
| every default equals the constant; overrides reach the backend | `TestHttpConfigDefaultsComeFromConfiguration`; overrides exercised by `http-server-phase-deadlines-run` and `http-server-shutdown-and-admission-runs`; `tcp_nodelay` reaches the backend as a generated-text assertion only, no fixture observes the socket option |
| method set, case, duplicates, mutation while attached | `http-router-registration-runs`; lifecycle case "route while attached" |
| exact match; 404; 405 with sorted `Allow` | wire cases "simple", "unknown path", "method not allowed" |
| only the default backend is selected; no `serve_with` or Response form | `TestHttpRejectsInvalidServerUse` ("a handler returns Nil | Error"); reference lists no such API |
| handler error details absent from the 500 | wire case "handler error" |
| context-bearing handler reaches initialized state; shared mutation follows synchronization rules | lifecycle case "hits seen by the application"; `TestAggregatesHoldingAnAtomicAreNotConst` |
| exact acceptance and rejection tests; working stateful example | `compiler/tests/integration/http_test.go` and `http_files_test.go`; the snippet above |
| ownership table; no unproven lifetime claimed | reference ("Request and Writer are valid for one handler call"); RFC 0194 pinned records |
| no second status line after commitment | wire cases "handler error" after a write, "length shortfall", "length overrun", "writer misuse" |
| raw paths, method case, duplicate routes, mount precedence, limits, stream close | routing fixture; mounts in `TestFileServerServesAndContains` |
| existing TCP, IO, Task, Channel behavior unchanged | `network-*`, `task-*`, `channel-*` fixtures in the focused and short runs |
| ordinary gates, focused C23, short C23 | recorded in the implementing change |

## Reference synchronization

Implementation updated `docs/reference.md` (`std/http`: signatures, handles, configuration
table, routing, lifecycle, deadlines, request rejection, request body, response, errors,
static files, components) after behavior stabilized; the closed decisions above agree with it.
