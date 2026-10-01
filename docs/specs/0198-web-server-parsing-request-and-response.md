# RFC 0198: Web Server — HTTP/1 Parser Integration

- Kind: Feature Specification (Rust-Style RFC)
- Status: Open Discussion; llhttp selected for the first cut; upstream revision
  and final qualification remain open
- Created: 2026-09-15
- Updated: 2026-10-01
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
  behavior; RFC 0210 owns public Request/Writer types and body streaming.
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
- Public parser objects or duplicate public definitions of Request/Writer or
  Header records.
- A handwritten start-line/header parser or independent message-framing rules.

## Demand rules

- The private parser adapter and pinned parser source are selected by the
  default server backend when Http.listen/Server.run are reachable. Public
  backend substitution is deferred; there is no custom-backend dependency gate.
- The parser component has no Hexal public API and does not select libuv,
  native bootstrap, or the event bridge by itself.
- Programs that do not use the default HTTP backend
  do not select this parser component.

## Adapter and packaging requirements

The private adapter reports consumed offset, head-ready, body progress, complete,
incomplete, paused, or parser error. Define its concrete C record/signatures before
implementation; do not use a single complete/incomplete flag for streaming requests.
Pause at head completion for dispatch and at message completion for response ordering.
Resume only under the owning connection Task; EOF invokes the pinned parser's finish
operation and distinguishes an idle connection from an incomplete message.

Header names/values can be split over callback invocations. Retained head storage
is bounded by the total-header ceiling; body and pipeline bytes remain in the
connection's bounded buffer under 0194 ownership. Record offsets or copy before
compaction; do not retain invalidated pointers. Trailers are validated, bounded,
and discarded, not merged into public Headers. Public Header combination cannot
be used to erase original framing evidence before validation.

0194 owns persistence policy; the adapter reports version, Connection tokens and
framing facts. HTTP/1.0 persistence is not an independent parser policy.

The current upstream method recognizer is a finite METHODS selection ending in
INVALID_METHOD. RFC 0210 limits v1 to the pinned supported method set; arbitrary extension-token
support and parser patches are deferred. Test the
exact pinned snapshot, not a claim about all llhttp versions.

Retain llhttp as the selected baseline. Package reproducible generated sources
for maintainers without npm in user builds, and integrate a profile-keyed native
artifact through the existing runtime-pack/dependency path. Checked-in upstream
sources and prebuilt release artifacts serve different roles; neither forces
users to rebuild the dependency per program. Specify symbols, license, hashes
and ABI/build inputs at the pinning phase. No network access enters core Compile.

## Required sweep

- pin, license, vendor, and hash the exact llhttp release, commit, and generated
  source snapshot;
- keep the parser adapter private to the server component;
- remove obsolete parser sketches from the work order; inventory production code
  before claiming there is a competing parser/API to delete;
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
- all duplicate Content-Length fields/combined values, including identical ones,
  and Transfer-Encoding plus Content-Length are rejected with 400 and close;
- callback data is copied or retained before its backing input range is
  released or recycled;
- an input split at every byte boundary reconstructs method/target/header fields;
  input overwritten or compacted between feeds cannot change retained fields;
- head-ready pauses before dispatch/body consumption; message-complete pause and
  consumed offsets preserve two pipelined requests including a split second head;
- EOF before a complete head, fixed body or chunked terminator fails without reuse;
- TE plus CL, conflicting CL, CL overflow/negative forms, whitespace before colon,
  obs-fold, forbidden CR/LF forms and malformed/overflowing chunk sizes are rejected;
- HTTP/1.1 missing/duplicate/invalid Host fails; request-target forms and
  Expect/unsupported upgrade behavior follow 0210's approved policy;
- chunk extensions and trailer bytes/count obey distinct bounded limits;
  trailers are validated and discarded, never exposed as ordinary Headers;
- the exact pinned method set and duplicate-equal-CL policy are tested on the wire;
- fixed-length and chunked request bodies produce the exact byte sequence;
  trailer fields are not merged into ordinary request headers;
- parser state and settings are caller-owned; llhttp performs no parser-owned
  dynamic allocation and makes no libuv calls;
- client response parsing and request serialization are not exposed as APIs;
- ordinary gates, focused C23 fixtures and short C23 pass; exhaustive C23 runs
  require separate user consent.

## Implementation plan

### Phase 0: pin and package the dependency

Select an exact llhttp release/commit; record generated source/header names,
SHA-256s, license, generation provenance, exported symbols and HTTP method set
in this spec. Inspect strict flags and duplicate-CL behavior on that snapshot.
Maintain sources for reproducibility and profile-keyed native artifacts for user
builds; register through compiler/runtime_dependency.go and existing dependency/
runtime-pack machinery. Keep build/filesystem work outside core Compile.
Exit: a minimal focused C23 artifact builds on each claimed qualified profile
without npm, generation or network access during a user build.

### Phase 1: pin the private adapter ABI

Write the concrete header/record definitions here before implementing callbacks.
Define init/feed/resume/finish/reset, caller-owned state, consumed offset and
head-ready/body-progress/message-complete/incomplete/paused/error results.
Specify offset behavior at both pauses, error position and EOF. Identify 0194's
head/body/pipeline buffers and exactly when pointers cease to be valid.
Exit: 0194 can consume the written ABI without guessing parser statuses or ownership.

### Phase 2: implement callbacks and bounded state

Implement the private adapter in the existing C runtime component model. Accumulate
fragmented names/values into bounded head storage; keep framing fields distinct
from public duplicate Headers. Pause after the head and after the message; expose
decoded body spans synchronously and consume/reset only under the connection Task.
Apply 0210's request-line/head/body/trailer limits, method set and reject-all
duplicate CL policy. EOF calls finish, never silently completes a short body.
Exit: valid fragmented input produces exact offsets/bytes and malformed input
produces one terminal parser error, with no libuv or parser-owned allocation.

### Phase 3: validate the entire parser contract

Map every Validation bullet to a fixture: every-byte splits, overwritten/compacted
input, two pipelined requests, EOF positions, Host/CL/TE errors, invalid lines,
chunk sizes/extensions/trailers and exact method policy. Compile/run the adapter
in focused C23 fixtures; ordinary tests assert dependency selection and emitted
include/declaration order. Do not add a public parser API for test convenience.
Exit: each exhaustive case passes and parser-only native testing selects no reactor.

### Phase 4: integration and closure

Hand the ABI and pinned native build record to 0194; exercise its head/body pause
integration there. Review legitimate output/dependency/manifest changes. Run
ordinary gates, focused parser C23 and short C23, not automatic exhaustive C23.
Review reference impacts with 0210, synchronize only changed public rules, then
close/archive only when all Validation is satisfied. Code handoff rebuilds hexal
and restarts hexal play; native packaging evidence is retained in the spec.

## Remaining readiness work

Pin the exact llhttp release/commit, generated C/header hashes, license and native
build record; this is implementation preparation, not an author parser-choice
question. Specify exact private result records and head/message pause offsets with
0194 before coding. The approved method set is the pinned llhttp HTTP method set;
all duplicate Content-Length is rejected, trailers discarded, and HTTP/1.0 closes.

## Reference synchronization

This RFC adds no public parser API. Update `docs/reference.md` only if
implementation changes a public HTTP contract owned by RFC 0210; update it
after behavior stabilizes and before this RFC is marked implemented or closed.
