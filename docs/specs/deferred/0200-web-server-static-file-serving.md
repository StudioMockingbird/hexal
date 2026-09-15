# RFC 0200: Web Server — Static File Serving

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the web server syntax (RFC 0210)
  and lowering (RFC 0194) landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax), RFC 0194 (web server lowering),
  RFC 0198 (HTTP parsing), and the implemented RFCs 0145 (libuv runtime),
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
| sendfile(2) optimization | Pick up | Zero-copy file serving |
| Gzip/Brotli pre-compressed files | Skip in v1 | Compression is a separate concern |
| Server-Side Includes (SSI) | Skip in v1 | Deprecated technology |
| CGI/FastCGI | Skip in v1 | Process management, not file serving |
| WebSocket file watching | Skip in v1 | WebSocket is a separate concern |
| Symbolic link following | Pick up | Required for deployment flexibility |
| Path traversal prevention | Pick up | Security requirement |
| Hidden file filtering (.dotfiles) | Pick up | Security default |
| Last-Modified timestamp precision | Pick up | Required for caching |

## Source surface

### File server

```hexal
import
    Net from "std/net"
end

-- Serve files from a directory
server := try Net.FileServer.new(
    root = "public",
    index = "index.html",
    listing = false,
    dotfiles = false,
)
```

```text
type FileServer is struct
    -- internal representation
end

fun FileServer.new(
    root: String,
    index: String,
    listing: Bool,
    dotfiles: Bool,
) -> FileServer | Error
```

### Single file

```text
fun FileServer.serve_file(
    path: String,
) -> Response | Error
```

- `path` is relative to `root`.
- Returns a `Response` with the file contents, `Content-Type`, and
  `Content-Length`.

### Directory serving

```text
fun FileServer.serve_directory(
    path: String,
) -> Response | Error
```

- If `index` file exists in the directory, serve it.
- If `listing` is true and no index exists, return a directory listing.
- If `listing` is false and no index exists, return 404.

### MIME detection

```text
fun FileServer.mime_type(path: String) -> String
```

- Returns the MIME type based on file extension.
- Default: `application/octet-stream`.
- Common types:

| Extension | MIME type |
| --- | --- |
| `.html` | `text/html; charset=utf-8` |
| `.css` | `text/css; charset=utf-8` |
| `.js` | `application/javascript; charset=utf-8` |
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

```text
method FileServer.set_cache_control(
    path_pattern: String,
    max_age: UInt32,
    immutable: Bool,
)
```

- `max_age` is in seconds.
- `immutable` adds the `immutable` directive.
- Pattern matching uses glob syntax (`*.css`, `/static/*`).

### Response headers

For each file served, the response includes:

```
Content-Type: <mime-type>
Content-Length: <file-size>
Last-Modified: <http-date>
ETag: "<hash>"
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
- Invalid ranges return 416 Range Not Satisfiable.

### Conditional GET

When the client sends `If-Modified-Since` or `If-None-Match`:

- If the file has not changed, return 304 Not Modified.
- If the file has changed, return 200 OK with the new content.

## C23 lowering

### sendfile(2)

On Linux and macOS, use `sendfile(2)` for zero-copy file serving:

```c
#ifdef __linux__
    sendfile(out_fd, in_fd, &offset, count);
#elif defined(__APPLE__)
    off_t len = count;
    sendfile(in_fd, out_fd, offset, &len, NULL, 0);
#endif
```

- On Windows, use `TransmitFile`.
- Fallback to `read`/`write` if sendfile is unavailable.

### Path traversal prevention

```c
bool hex_is_path_safe(const char *root, const char *path) {
    /* resolve both root and path to absolute */
    /* verify resolved path starts with root */
    /* reject .. components */
    /* reject symlinks that escape root */
}
```

- No symlink following outside `root`.
- No `..` components that escape `root`.
- No null bytes in path.

### ETag generation

```c
void hex_generate_etag(const char *path, char *etag, size_t len) {
    struct stat st;
    stat(path, &st);
    snprintf(etag, len, "\"%lx-%lx\"",
             (unsigned long)st.st_size,
             (unsigned long)st.st_mtime);
}
```

- ETag is based on file size and modification time.
- Strong ETag (double quotes) is used.

## Performance considerations

- **sendfile**: Zero-copy from disk to socket; no user-space buffer.
- **Buffered I/O**: Use `uv_fs_read` for non-sendfile platforms.
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
- sendfile optimization (Linux, macOS, Windows fallback);
- Path traversal prevention;
- Hidden file filtering;
- demand discovery for file serving component;
- workbench snippet and manifest entries;
- `docs/reference.md` file server surface after explicit approval.

## Validation

This section is exhaustive:

- `FileServer.new` with valid root directory succeeds;
- `FileServer.new` with non-existent root returns error;
- `serve_file` returns correct `Content-Type` for common extensions;
- `serve_file` returns correct `Content-Length` for file size;
- `serve_file` returns 404 for non-existent file;
- `serve_file` returns 403 for hidden file when `dotfiles = false`;
- `serve_directory` serves `index.html` when present;
- `serve_directory` returns 404 when no index and `listing = false`;
- `serve_directory` returns directory listing when `listing = true`;
- Range request returns 206 with correct `Content-Range` header;
- Range request with invalid range returns 416;
- Conditional GET with matching `If-Modified-Since` returns 304;
- Conditional GET with matching `If-None-Match` returns 304;
- Conditional GET with non-matching header returns 200;
- Path traversal with `..` returns 403;
- Path traversal with symlink escaping root returns 403;
- Hidden file (dotfile) returns 403 when `dotfiles = false`;
- sendfile is used on supported platforms;
- fallback to read/write on unsupported platforms;
- MIME type detection returns correct type for all listed extensions;
- MIME type detection returns `application/octet-stream` for unknown
  extensions;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support `If-Match` and `If-Unmodified-Since` conditional
   requests.
2. Whether to support multiple range requests (multipart ranges).
3. Whether to expose a configurable MIME type table.
4. Whether to support pre-compressed files (`.gz`, `.br`).
5. Whether to add a `FileWatcher` that invalidates cached ETags on file
   changes.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the file serving types and contracts only after behavior stabilizes
and with explicit user approval.
