# RFC 0206: Web Server — Compression

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the web server syntax (RFC 0210)
  and middleware architecture (RFC 0202) landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax), RFC 0202 (middleware architecture),
  and the implemented RFCs 0145 (libuv runtime), 0146 (mimalloc), and 0168
  (libuv capability arc)
- Coordinates with: RFC 0210 (web server syntax) for the Request and Response
  types, and RFC 0186 (stdlib boundary) for module placement
- Does not add: image compression, video compression, or audio compression

## Motivation

Compression reduces bandwidth usage and improves response times. Without
compression, every response is sent uncompressed, wasting bandwidth and
increasing latency for the client.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| gzip compression | Pick up | Most common, widely supported |
| deflate compression | Pick up | Lightweight alternative |
| Content-Encoding negotiation | Pick up | Required for client preference |
| Minimum size threshold | Pick up | Avoid compressing small responses |
| Content-Type filtering | Pick up | Avoid compressing already-compressed files |
| Compression level configuration | Pick up | Balance speed vs compression ratio |
| Brotli compression | Skip in v1 | Complex, can be added later |
| Zstandard compression | Skip in v1 | Less widely supported |
| Streaming compression | Pick up | Required for large responses |
| Pre-compressed files | Skip in v1 | File serving concern |
| Request decompression | Skip in v1 | Rare, can be added later |
| Adaptive compression | Skip in v1 | Complexity disproportionate to initial surface |

## Source surface

### Compression middleware

```hexal
import
    Net from "std/net"
end

router.use(Net.compress(
    algorithm = Net.Compression.gzip,
    level = 6,
    min_size = 1024,
    types = ["text/html", "text/css", "application/json", "text/plain"],
))
```

```text
fun compress(
    algorithm: Compression,
    level: UInt8,
    min_size: Size,
    types: Slice<String>,
) -> Middleware
```

### Algorithms

```text
type Compression is
    gzip |
    deflate
end
```

### Compression levels

| Level | gzip | deflate |
| --- | --- | --- |
| 1 | Fastest | Fastest |
| 6 | Default | Default |
| 9 | Best compression | Best compression |

### Usage

```hexal
import
    Net from "std/net"
end

-- Compress responses larger than 1KB
router.use(Net.compress(
    algorithm = Net.Compression.gzip,
    level = 6,
    min_size = 1024,
    types = ["text/html", "text/css", "application/json", "text/plain"],
))
```

### Response

When compression is applied:

```
HTTP/1.1 200 OK
Content-Type: text/html; charset=utf-8
Content-Encoding: gzip
Content-Length: 1234
Vary: Accept-Encoding

[gzipped content]
```

### Content-Encoding negotiation

When the client sends `Accept-Encoding`:

```
Accept-Encoding: gzip, deflate
```

The server selects the first supported algorithm:

- If `gzip` is in `Accept-Encoding`, use gzip.
- If `deflate` is in `Accept-Encoding`, use deflate.
- If neither is supported, send uncompressed.

### Content-Type filtering

Compression is applied only when the response `Content-Type` matches
one of the configured types:

| Type | Compressible |
| --- | --- |
| `text/html` | Yes |
| `text/css` | Yes |
| `text/plain` | Yes |
| `application/json` | Yes |
| `application/javascript` | Yes |
| `text/xml` | Yes |
| `application/xml` | Yes |
| `image/svg+xml` | Yes |
| `image/png` | No (already compressed) |
| `image/jpeg` | No (already compressed) |
| `video/mp4` | No (already compressed) |
| `application/octet-stream` | No (binary) |

### Minimum size threshold

Responses smaller than `min_size` are not compressed:

- Small responses may not benefit from compression.
- Compression overhead may make small responses larger.
- Default: 1024 bytes.

## C23 lowering

### gzip compression

```c
#include <zlib.h>

int hex_gzip_compress(const char *input, size_t input_len,
                      char *output, size_t *output_len,
                      int level) {
    z_stream stream = {0};
    deflateInit2(&stream, level, Z_DEFLATED,
                 15 + 16, /* gzip header */
                 8, Z_DEFAULT_STRATEGY);

    stream.next_in = (Bytef *)input;
    stream.avail_in = input_len;
    stream.next_out = (Bytef *)output;
    stream.avail_out = *output_len;

    deflate(&stream, Z_FINISH);
    deflateEnd(&stream);

    *output_len = stream.total_out;
    return 0;
}
```

### deflate compression

```c
int hex_deflate_compress(const char *input, size_t input_len,
                         char *output, size_t *output_len,
                         int level) {
    z_stream stream = {0};
    deflateInit(&stream, level);

    stream.next_in = (Bytef *)input;
    stream.avail_in = input_len;
    stream.next_out = (Bytef *)output;
    stream.avail_out = *output_len;

    deflate(&stream, Z_FINISH);
    deflateEnd(&stream);

    *output_len = stream.total_out;
    return 0;
}
```

### Streaming compression

For large responses, compress in chunks:

```c
int hex_gzip_compress_stream(uv_stream_t *client,
                             uv_reader_t *reader,
                             int level) {
    z_stream stream = {0};
    deflateInit2(&stream, level, Z_DEFLATED,
                 15 + 16, 8, Z_DEFAULT_STRATEGY);

    char in_buf[65536];
    char out_buf[65536];

    while (true) {
        size_t nread = uv_read(reader, in_buf, sizeof(in_buf));
        if (nread == 0) break;

        stream.next_in = (Bytef *)in_buf;
        stream.avail_in = nread;

        do {
            stream.next_out = (Bytef *)out_buf;
            stream.avail_out = sizeof(out_buf);

            deflate(&stream, Z_NO_FLUSH);

            size_t have = sizeof(out_buf) - stream.avail_out;
            uv_write(client, out_buf, have);
        } while (stream.avail_out == 0);
    }

    /* flush remaining data */
    deflate(&stream, Z_FINISH);
    deflateEnd(&stream);

    return 0;
}
```

### Content-Type matching

```c
bool hex_should_compress(const char *content_type,
                         const char **types, size_t types_len) {
    for (size_t i = 0; i < types_len; i++) {
        if (strncmp(content_type, types[i], strlen(types[i])) == 0) {
            return true;
        }
    }
    return false;
}
```

### Content-Encoding negotiation

```c
const char *hex_select_encoding(const char *accept_encoding,
                                const char **supported,
                                size_t supported_len) {
    for (size_t i = 0; i < supported_len; i++) {
        if (strstr(accept_encoding, supported[i]) != NULL) {
            return supported[i];
        }
    }
    return NULL; /* no supported encoding */
}
```

## Performance considerations

- **Level 6 default**: Good balance of speed and compression ratio.
- **Minimum threshold**: Avoids compressing small responses where
  compression overhead exceeds savings.
- **Streaming**: Large responses are compressed in chunks; no buffering
  of the entire response.
- **Content-Type filtering**: Avoids compressing already-compressed
  formats (images, video).
- **Memory usage**: gzip and deflate use ~256 KB of memory per
  compression stream.

## Demand rules

- `compress`, `Compression`, and the compression middleware select the
  compression component.
- The compression component selects zlib (for gzip and deflate).
- The compression component does not select libuv, native bootstrap,
  or the event bridge; it is a pure data transformation.
- A program that does not use `compress` does not select the compression
  component.

## Required sweep

- gzip compression in `hexal/compress.c` and `hexal/compress.h`;
- deflate compression;
- Content-Encoding negotiation (Accept-Encoding parsing);
- Content-Type filtering;
- minimum size threshold;
- streaming compression for large responses;
- compression level configuration;
- demand discovery for compression component;
- workbench snippet and manifest entries;
- `docs/reference.md` compression surface after explicit approval.

## Validation

This section is exhaustive:

- gzip compression produces valid gzip output;
- deflate compression produces valid deflate output;
- compression level 1 produces fastest output;
- compression level 9 produces best compression;
- Content-Encoding header is set correctly;
- Vary: Accept-Encoding header is set;
- responses smaller than min_size are not compressed;
- responses with non-matching Content-Type are not compressed;
- Accept-Encoding: gzip selects gzip;
- Accept-Encoding: deflate selects deflate;
- Accept-Encoding: identity sends uncompressed;
- Accept-Encoding with no supported encoding sends uncompressed;
- streaming compression works for large responses;
- compressed response has correct Content-Length;
- decompressed response matches original content;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to add Brotli compression support.
2. Whether to add Zstandard compression support.
3. Whether to support request decompression (Content-Encoding on requests).
4. Whether to add adaptive compression (adjust level based on response
   size and server load).
5. Whether to support pre-compressed files (`.gz`, `.br`).

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the compression types and contracts only after behavior stabilizes
and with explicit user approval.
