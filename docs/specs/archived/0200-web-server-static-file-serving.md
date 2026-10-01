# RFC 0200: Web Server — Static File Serving

- Kind: Feature Specification (Rust-Style RFC)
- Status: Closed. `Http.FileServer`, `serve_file`, `serve_directory`, and
  `Router.mount` are implemented with handle-relative no-follow containment on
  the two qualified profiles (`x86_64-linux-gnu`, `x86_64-windows-gnu-ucrt`),
  bounded buffered transfer, and the single-range and conditional policy below;
  every Validation bullet maps to the evidence under Closure record. Other
  profiles are unqualified, not implied
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

- Returns the MIME type based on file extension. This is the native lookup behind
  `Content-Type`; it is not exported as std/http API.
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

## Pinned records

### Containment record (Phase 0)

Owner: `compiler/corelib/runtime/fileserver.c` and `fileserver.h`.

- The FileServer record holds the retained root handle, copied `index` and
  `cache_control` bytes, the dotfile flag, and `mounted`, an atomic count of routers
  that mount it. `free` traps while `mounted` is nonzero; `Router.free` detaches.
- Root: POSIX `lstat` rejects a symbolic link, then `open(O_RDONLY | O_DIRECTORY |
  O_NOFOLLOW | O_CLOEXEC)` retains the descriptor (the flag closes the gap between the
  two calls). Windows opens the directory with `CreateFileW(FILE_FLAG_BACKUP_SEMANTICS |
  FILE_FLAG_OPEN_REPARSE_POINT)` and share mode read, write, and delete, and rejects a
  handle whose attributes carry the reparse-point bit. The share mode lets the root be
  renamed while retained.
- Lookup: the path is decoded once into slash-separated components. On a filesystem worker
  (`hex_event_work_call`; the Task parks, no scheduler worker blocks) each component is
  opened beneath the previous handle. POSIX `openat(parent, name, O_RDONLY | O_NOFOLLOW |
  O_CLOEXEC | O_NONBLOCK)`, never `O_DIRECTORY`, so a link fails with `ELOOP` (`403`) and a
  FIFO cannot block the open; `fstat` on the opened descriptor then accepts directories
  (continue) and regular files (end of walk) and refuses other types. Windows `NtCreateFile`
  with `RootDirectory` set to the parent handle and `FILE_OPEN_REPARSE_POINT`, then
  `GetFileInformationByHandle`: a reparse point is `403`; a component containing `:` is
  refused before any open. A directory result opens the index file the same way.
- Metadata (size, modification seconds) comes from the opened handle, and the transfer
  reads that handle with explicit offsets (`pread`, `ReadFile` with an `OVERLAPPED`
  offset) in `HEX_FILES_CHUNK`-sized pieces on the worker pool. The handle is closed once,
  on every path out of `hex_files_serve`.
- No `realpath`, canonicalization, or prefix comparison exists. An unavailable primitive is
  a build failure of the target, not a fallback: each platform branch is the handle-relative
  one.

### Policies the design left to the implementation

- `400`: malformed or truncated `%` escape, encoded `/`, `\`, or NUL, raw `\` or NUL, query
  or fragment byte. `403`: `.` or `..` component, hidden component, link or reparse point,
  Windows `:`. `404`: missing, directory without index, `serve_file` on a directory.
  `405` with `Allow: GET, HEAD`.
- Ranges: one `bytes=first-last`, `first-`, or `-suffix`; a number too large for the size
  saturates, so a start that large is unsatisfiable (`416`) and a suffix that large selects
  the whole file (`206`). `HEAD` ignores `Range`. The `416` response carries `Content-Range:
  bytes */size` and a short `text/plain` body.
- `If-None-Match` matches `*` and compares weakly; `If-Modified-Since` accepts only the exact
  IMF-fixdate (the date parser formats its result back and compares), so a wrong weekday
  or a day past the month's end is ignored.
- `Content-Type` follows the opened file's name, the index file's name for a directory
  result. An empty `index` disables index files.

### Qualification

`TestFileServerServesAndContains` and `TestFileServerBoundedTransfer` pass on
`x86_64-windows-gnu-ucrt` (Windows 11, Clang) and `x86_64-linux-gnu` (WSL2 Linux 6.18,
glibc, Clang 23.1.1). Host-specific cases: a directory junction (Windows) or symlink (Linux)
escaping the root is `403`; a colon in a component is `403` on Windows and an ordinary name on
Linux; the Linux transfer test lists `/proc/<pid>/fd` after every transfer and requires no
descriptor on a served file and exactly the retained root. An unprivileged Windows file
symlink cannot be created, so only the directory form is exercised there; the reparse
attribute check applies to both. No other target is qualified.

Two properties are demonstrated indirectly because a test cannot force them. A component
swapped for a link between the walk's `openat` calls cannot be scheduled from outside; the
design removes the window instead (each component is opened relative to the previous handle,
so nothing is looked up by name twice), and the root-rename handshake shows the retained
handle serves the original directory after its pathname is replaced by a link elsewhere. A
Windows handle leak is not enumerated; the single close in `hex_files_serve` is read by
inspection, and the same test removes and truncates files while a transfer holds them open.

## Closure record

| Validation bullet | Evidence (`http_files.stdout` unless named) |
| --- | --- |
| valid root succeeds; missing root errors | "valid root: ok", "missing root: not found" (and file, link, index, cache-control inputs) |
| `Content-Type` for the listed extensions, `octet-stream` for unknown | the `mime` cases: every listed extension, an uppercase extension, an unknown extension, no extension, an extension only in a directory name |
| `Content-Length` equals the file size; zero-length files | "full response", "digits", "zero-length file", "head zero-length file" |
| `404` for a missing file; `403` for a hidden file with `dotfiles = false`; hidden directory | "missing file", "dotfile refused", "dotfile directory refused"; enabled: "dotfile served when enabled" |
| `serve_directory` serves `index.html`, `404` without one | "serve_directory", "mount root", "serve_directory without index", "mount directory without index" |
| `206` with `Content-Range`; `416` with `bytes */size`; malformed, unknown-unit, multi-range ignored | the `range` cases |
| `If-Modified-Since` and `If-None-Match` `304`; non-matching `200`; weak comparison; precedence; weak ETag spelling | the `none-match` and `modified-since` cases |
| `..` traversal and symlink or junction escape `403` | "dot dot segment", "encoded dot dot", "mixed dot dot", "dot segment", "directory link escape", "directory link itself" |
| sibling path with the root's textual prefix; NUL, alternate stream, escaping separators | "dot dot to the sibling with the root prefix", "encoded NUL", "alternate data stream" (Windows `403`, Linux ordinary name), "encoded slash", "encoded backslash", "raw backslash in a handler path" |
| containment valid after the pathname or symlink state changes | "root retained after the pathname changed", "swapped tree is not reachable", "swapped link is not followed" |
| bounded buffered transfer, partial progress, cleanup; no `sendfile` or `TransmitFile` selected | `http_files_transfer.stdout` ("whole file", "middle of the file") and the descriptor check; ordinary `TestHttpFileServerSurfaceCompilesAndSelectsOnlyBufferedTransfer` |
| disappearance, truncation, slow reader, close or deadline during transfer | "removed while transferring" (completes), "truncated while transferring" (ends short), "stalled reader" (closed by the write deadline), "early reader" (departs), each followed by "served after every reader" |
| mount boundaries, nested and exact precedence, cache-control selection | "sibling prefix is not a mount", "prefix extension is not a mount", "nested mount longest prefix", "outer mount keeps its own directory", "exact route over mount", the `cache control` cases; mount rejections and `Busy` after listen |
| metadata comes from the transmitted handle | one `fstat` or `GetFileInformationByHandle` on the opened handle fills size and time; the same handle is read |
| existing Task, Channel, IO, TCP behavior unchanged | `task-*`, `channel-*`, `network-*` fixtures in the focused and short runs |
| ordinary gates, focused C23, short C23 | recorded in the implementing change |

## Measurement record

Method and hosts are in RFC 0144's baseline (release build, default configuration, a Go
closed-loop client on the same host, five runs of 5 s, median and range). The mount serves a
directory with `Cache-Control` set and no conditional headers.

| Host | Workload | Requests/s | p50 | p95 | p99 | Max | Server CPU (5 s) | RSS | Threads |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| Linux | 1 KiB file, 64 connections | 3,753 (3,588-3,943) | 16.8 ms | 22.5 ms | 25.9 ms | 166-287 ms | 10.0 s | 37 MiB | 17 |
| Linux | 1 MiB file, 16 connections | 295 (269-307), about 295 MiB/s | 53.2 ms | 63.9 ms | 74.8 ms | 77-123 ms | 9.1 s | 33-35 MiB | 17 |
| Windows | 1 KiB file, 64 connections | 10,567 (9,874-11,887) | 5.4 ms | 9.7 ms | 12.6 ms | 28-38 ms | 19.9 s | 15-16 MiB | 23 |
| Windows | 1 MiB file, 16 connections | 937 (666-951), about 937 MiB/s | 17.8 ms | 21.3 ms | 24.6 ms | 27-81 ms | 13.1 s | 17-18 MiB | 23 |

For comparison on the same hosts the fixed route served 7,571 req/s (Linux) and 26,197 req/s
(Windows) at 64 connections.

Memory stayed flat while serving: a 1 MiB body moves through one 64 KiB buffer per active
transfer, so 16 concurrent 1 MiB transfers added a few MiB of resident memory over the fixed
route's figures, and four more threads appeared (the filesystem worker pool: 17 against 13).
The slow-reader results are the transfer fixture's, not rates: a reader that stops reading a 32
MiB file is closed by the 300 ms write deadline having received only the bytes the socket
buffers could hold, and the server serves the next request. No comparison with another server
or with an optimized transfer path was made, and none is claimed.

## Reference synchronization

Implementation added the static-file rules to `docs/reference.md` (`std/http`: Static files,
Mounts, Serving, Containment, Representation, Conditional and range requests, Transfer,
Components). The rules above agree with it.
