# RFC 0200: Web Server — Static File Serving

- Kind: Feature Specification (Rust-Style RFC)
- Status: Design approved; subsequent safe buffered-file milestone;
  containment qualification remains before implementation
- Created: 2026-09-15
- Updated: 2026-10-01
- Depends on: RFC 0144 (Task-aware socket runtime), RFC 0194 (web server
  lowering), RFC 0198 (HTTP parsing), RFC 0210 (web server syntax), and the
  implemented RFCs 0145 (libuv runtime),
  0146 (mimalloc), and 0168 (libuv capability arc)
- Coordinates with: RFC 0195 (TLS) for encrypted file serving, and RFC 0186
  (stdlib boundary) for module placement
- Does not add: directory listing, CGI, server-side includes, or PHP

## Motivation

Static file serving is the most common non-API use case for web servers:
asset delivery, documentation hosting, file downloads, and media streaming.
Without a native static file server, every Hexal program must read files
into memory and manually construct responses, losing performance (no
sendfile, no range requests) and correctness (MIME detection, caching
headers).

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Serve a single file | Pick up | Foundation |
| Serve files from a directory tree | Pick up | Most common use case |
| MIME type detection (extension-based) | Pick up | Required for correct Content-Type |
| Content-Length header | Pick up | Required for client progress |
| Range requests (byte ranges) | Pick up | Required for media streaming |
| Conditional GET (If-Modified-Since, ETag) | Pick up | Required for caching |
| Last-Modified header | Pick up | Required for caching |
| ETag generation | Pick up | Required for caching |
| Cache-Control header | Pick up | Required for production |
| Directory listing | Skip in v1 | Security risk, can be added later |
| sendfile/TransmitFile optimization | Defer | Target-qualified follow-up after buffered baseline |
| Gzip/Brotli pre-compressed files | Skip in v1 | Compression is a separate concern |
| Server-Side Includes (SSI) | Skip in v1 | Deprecated technology |
| CGI/FastCGI | Skip in v1 | Process management, not file serving |
| WebSocket file watching | Skip in v1 | WebSocket is a separate concern |
| Symbolic link/reparse traversal | Reject in initial milestone | Simpler containment contract |
| Path traversal prevention | Pick up | Security requirement |
| Hidden file filtering (.dotfiles) | Pick up | Security default |
| Last-Modified timestamp precision | Pick up | Required for caching |

## Source surface

FileServer is opaque, allocated with explicit Heap and retains a validated root
directory handle plus copied configuration. Root and index use String; reject
symlink/reparse roots and index separators. FileServer is immutable/shared while
mounted; free after all attached Router/Server resources are freed.

- Http.FileServer(heap: Heap, root: String, index: String, dotfiles: Bool,
  cache_control: String) -> FileServer | Error.
- FileServer.serve_file(request: Http.Request, writer: Http.Writer,
  path: String) -> Nil | Error.
- FileServer.serve_directory(request: Http.Request, writer: Http.Writer,
  path: String) -> Nil | Error.
- FileServer.free(heap: Heap) -> Nil.
- Router<App>.mount(prefix: String, files: FileServer) -> Nil | Error,
  before listen; reject duplicate mount prefixes.

Serving writes the response synchronously through Writer; it never returns a
Response value holding whole file contents. Only GET/HEAD are accepted; others
receive 405 with Allow: GET, HEAD. Index absent -> 404, no directory listing.
Mounts use raw path segment boundaries: /assets matches /assets and /assets/...,
not /assets2. Exact routes precede mounts; longest prefix wins. Strip the mount
prefix, decode once, reject encoded slash/backslash, NUL, dot traversal, symlink
and Windows reparse/alternate-stream forms. Decode failures -> 400; forbidden
paths -> 403; missing files -> 404. No physical path enters the response.


### MIME detection

```text
fun mime_type(path: String) -> String
```

- Returns the MIME type based on file extension.
- Default: `application/octet-stream`.
- Common types:

| Extension | MIME type |
| --- | --- |
| `.html` | `text/html; charset=utf-8` |
| `.css` | `text/css; charset=utf-8` |
| `.js` | `text/javascript; charset=utf-8` |
| `.json` | `application/json; charset=utf-8` |
| `.png` | `image/png` |
| `.jpg`, `.jpeg` | `image/jpeg` |
| `.gif` | `image/gif` |
| `.svg` | `image/svg+xml` |
| `.woff` | `font/woff` |
| `.woff2` | `font/woff2` |
| `.ttf` | `font/ttf` |
| `.mp4` | `video/mp4` |
| `.webm` | `video/webm` |
| `.pdf` | `application/pdf` |
| `.zip` | `application/zip` |
| `.txt` | `text/plain; charset=utf-8` |
| `.xml` | `application/xml; charset=utf-8` |

### Caching

One Cache-Control value is supplied at FileServer construction and copied once.
Validate it as a response field value. There is no path-pattern/glob setter.
Weak size/mtime ETags and Last-Modified are computed from the opened file handle.
If-Match/If-Unmodified-Since, MIME overrides, precompressed files, watchers and
open-file caching are deferred.


### Response headers

For each file served, the response includes:

```
Content-Type: <mime-type>
Content-Length: <file-size>
Last-Modified: <http-date>
ETag: W/"<size-mtime>"
Cache-Control: <cache-control>
Accept-Ranges: bytes
```

### Range requests

When the client sends `Range: bytes=0-1023`:

```
HTTP/1.1 206 Partial Content
Content-Range: bytes 0-1023/4096
Content-Length: 1024
```

- Multiple ranges are not supported in v1.
- Unsatisfiable byte ranges return 416 with Content-Range: bytes */<size>.
- Ignore malformed, unknown-unit and multi-range requests and serve the full
  representation (200); valid unsatisfiable byte ranges return 416. HEAD ignores
  Range and emits GET representation metadata without a body.

### Conditional GET

When the client sends `If-Modified-Since` or `If-None-Match`:

- If the file has not changed, return 304 Not Modified.
- If the file has changed, return 200 OK with the new content.

## C23 lowering

### Transport and containment requirements

File bodies are streamed with bounded memory, not loaded into a whole String.
The response uses RFC 0210's synchronous Writer API. No raw
sendfile/TransmitFile sketch authorizes access to libuv-owned sockets from a
scheduler worker. All socket operations preserve the common owner and wait
cleanup. Filesystem work may use libuv's worker pool: it is distinct from idle
socket waiting. uv_fs_sendfile is not a free readiness-only operation; disk work
can block. A zero-copy path requires target qualification, partial-transfer,
would-block and close/deadline handling, plus a safe bounded fallback.

Resolve and open beneath a retained root handle. A string-prefix check is not
containment, and realpath-then-open is not race-safe. Pin a target-specific
handle-relative traversal algorithm before implementation, including symlink,
Windows reparse-point, alternate-stream, separator and embedded-NUL policy.
Decode the URL path at most once under the approved route policy; query bytes
are not a filename. Metadata and transfer refer to the same opened file.
Reject containment failures without exposing the physical root. Files changing
or truncating during transfer must not cause wrong lengths or buffer overreads.

### ETag generation

Use a weak ETag for the size-plus-modification-time scheme: W/"...". This scheme
does not establish byte identity and must not be described as a strong validator.
Derive size and timestamps from the opened file; check every metadata failure.
Use width-correct integer formatting, not unsigned long casts on LLP64.
If-None-Match uses weak comparison and takes precedence over If-Modified-Since.
HTTP dates use the protocol's fixed format and second precision; invalid dates
are ignored rather than crashing or becoming local-time comparisons.

## Performance considerations

- **Buffered baseline**: bounded filesystem reads through the existing file runtime
  and Task-aware TCP writes; no whole-file allocation.
- **Zero-copy**: deferred; no raw socket handles or optimized-transfer gate here.
- **Range requests**: Seek to the start offset; read only the requested
  bytes.
- **Caching**: ETag and Last-Modified avoid re-reading unchanged files.
- **MIME detection**: Extension-based; no file content inspection.

## Demand rules

- `FileServer`, `serve_file`, and `serve_directory` select the file
  serving component.
- The file serving component selects libuv, native bootstrap, and the
  event bridge.
- A program that does not use `FileServer` does not select the file
  serving component.

## Required sweep

- File server implementation in `hexal/fileserver.c` and `hexal/fileserver.h`;
- MIME type detection and table;
- Range request handling;
- Conditional GET handling (ETag, Last-Modified);
- bounded buffered transfer; zero-copy is explicitly outside this milestone;
- Path traversal prevention;
- Hidden file filtering;
- demand discovery for file serving component;
- workbench snippet and manifest entries;
- `docs/reference.md` file server surface after behavior stabilizes.

## Validation

This section is exhaustive:

- `Http.FileServer` with a valid root directory succeeds;
- `Http.FileServer` with a non-existent root returns error;
- `serve_file` returns correct `Content-Type` for common extensions;
- `serve_file` returns correct `Content-Length` for file size;
- `serve_file` returns 404 for non-existent file;
- `serve_file` returns 403 for hidden file when `dotfiles = false`;
- `serve_directory` serves `index.html` when present;
- `serve_directory` returns 404 when no index exists;
- Range request returns 206 with correct `Content-Range` header;
- Unsatisfiable range returns 416 with Content-Range bytes */size; malformed and
  multi-range inputs follow the approved ignore/reject policy;
- Conditional GET with matching `If-Modified-Since` returns 304;
- Conditional GET with matching `If-None-Match` returns 304;
- Conditional GET with non-matching header returns 200;
- Path traversal with `..` returns 403;
- Path traversal with symlink escaping root returns 403;
- Hidden file (dotfile) returns 403 when `dotfiles = false`;
- generated artifacts and execution prove bounded buffered transfer, partial
  progress and cleanup; no sendfile/TransmitFile dependency is selected;
- MIME type detection returns correct type for all listed extensions;
- MIME type detection returns `application/octet-stream` for unknown
  extensions;
- weak ETag spelling, If-None-Match precedence and weak comparison are correct;
- GET/HEAD, suffix/open-ended ranges, zero-length files and numeric overflow obey
  the approved range policy; HEAD sends no body;
- a sibling path with the root's textual prefix is rejected, as are embedded NUL,
  alternate-stream and escaping separator forms on qualified targets;
- handle-relative containment remains valid when pathname/symlink state changes
  between lookup and open, under the approved symlink/reparse policy;
- file disappearance/truncation, slow readers, close/deadline during transfer
  release the opened file and retain native-accessible buffers until cleanup;
- mount boundaries, nested/exact precedence and cache-control selection follow
  the approved API; filesystem metadata comes from the transmitted file handle;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary gates, focused C23 fixtures and short C23 pass; exhaustive C23 runs
  require separate user consent.

## Implementation plan

### Phase 0: qualify containment before implementing serving

Use the implemented 0210 Writer and 0194 buffered transport; this is a subsequent
milestone. Pin the native FileServer record, retained root handle, copied config,
attachment/free owners and target-specific no-symlink traversal algorithm here.
For POSIX identify handle-relative open and per-component no-follow checks; for
Windows identify relative traversal/reparse/alternate-stream checks on opened
handles. Do not substitute realpath plus a prefix test. Qualify only actual target
profiles; unsafe/unavailable containment is an explicit target gate.
Exit: containment and lookup/open-race fixtures named in Validation demonstrate
the algorithm; a failed platform does not silently fall back to unsafe traversal.

### Phase 1: FileServer construction and mounts

Register the approved opaque resource and constructor/method metadata using the
std.http patterns. Validate/copy root, index and Cache-Control, retain the root
handle, freeze configuration and account for Router attachments. Implement exact
route priority, segment-boundary mounts, longest prefix and duplicate-prefix Error.
Decode once and reject encoded separators, NUL, dot traversal, reparse/symlink
components and hidden entries under policy. Use 400/403/404 without physical paths.
Exit: lifecycle, mount precedence and forbidden-path cases pass without a glob
engine, directory listing, watcher or extra HTTP backend.

### Phase 2: open-file metadata and conditional/range logic

Open the final file once beneath the retained root; metadata/transfer use that
handle. Implement the listed extension MIME table, weak size/mtime ETag and HTTP
date handling. Apply If-None-Match precedence, HEAD metadata/no payload, single
suffix/open-ended range parsing and checked offsets/counts. Ignore malformed/
multi-range; valid unsatisfiable -> 416 plus bytes */size. Index absent -> 404.
Exit: every MIME, validator/date and range case in Validation produces exact
status/headers/count without LLP64 truncation or metadata/pathname races.

### Phase 3: bounded transfer and cleanup

Read bounded file chunks through the existing filesystem runtime and write through
Writer, respecting write deadline/backpressure and native buffer completion.
On file truncation, short transfer or error close after commitment; before commitment
use the approved HTTP failure policy. Release the opened file on every terminal
path, then detach/free FileServer only when no Router/Server still references it.
Exit: large files do not allocate whole bodies, slow readers stay bounded, and
close/deadline/truncation fixtures have no stale buffer/file-handle access.

### Phase 4: qualification, measurement and handoff

Map every Validation bullet to artifact/unit or focused native platform fixtures.
Assert no sendfile/TransmitFile dependency; optimized transfer is deferred, not a
fallback this implementation must build. Record static-file throughput/CPU/memory
and slow-reader results without requiring a particular speedup.
Run ordinary test/vet/build, focused file/HTTP C23 and short C23. Review intended
manifest movement, sync reference once, close only after Validation and target
qualification, then rebuild hexal/restart hexal play. Glob/cache/watch extensions
and zero-copy cannot be smuggled into this phase.

## Remaining readiness work

No author scope choices remain. Pin/qualify handle-relative no-symlink traversal
for each supported target before implementation; path canonicalization followed
by open is not an acceptable substitute. Optimized transfer and cache extensions
are deferred and do not gate buffered serving.

## Reference synchronization

This draft changes no canonical reference contract. Implementation updates
`docs/reference.md` after approved behavior stabilizes and before closure,
under the repository's normal implementation authorization.
