# RFC 0207: Web Server — Multipart Parsing

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the web server syntax (RFC 0210)
  and HTTP parsing (RFC 0198) landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax), RFC 0198 (HTTP parsing), and
  the implemented RFCs 0145 (libuv runtime), 0146 (mimalloc), and 0168
  (libuv capability arc)
- Coordinates with: RFC 0210 (web server syntax) for the Request type,
  and RFC 0186 (stdlib boundary) for module placement
- Does not add: JSON body parsing, XML body parsing, or URL-encoded body
  parsing (those are simpler and can be added later)

## Motivation

Multipart form data is the standard format for file uploads and complex
form submissions. Without multipart parsing, every Hexal server must
manually parse boundaries, headers, and content, which is error-prone
and insecure.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| multipart/form-data parsing | Pick up | Required for file uploads |
| multipart/mixed parsing | Skip in v1 | Rare, can be added later |
| Boundary detection | Pick up | Foundation |
| Part headers (Content-Disposition, Content-Type) | Pick up | Required for file uploads |
| Part content (text fields) | Pick up | Required for form data |
| Part content (binary files) | Pick up | Required for file uploads |
| Streaming parsing | Pick up | Required for large files |
| File size limits | Pick up | Security requirement |
| Part count limits | Pick up | Security requirement |
| Nested multipart | Skip in v1 | Rare, can be added later |
| Quoted boundary strings | Pick up | Required for spec compliance |
| Content-Transfer-Encoding | Skip in v1 | Deprecated |

## Source surface

### Multipart parser

```hexal
import
    Net from "std/net"
end

parser := Net.MultipartParser.new(
    boundary = "----WebKitFormBoundary7MA4YWxkTrZu0gW",
    max_file_size = 10 * 1024 * 1024,
    max_parts = 10,
)
```

```text
type MultipartParser is struct
    -- internal representation
end

fun MultipartParser.new(
    boundary: String,
    max_file_size: Size,
    max_parts: Size,
) -> MultipartParser
```

### Parsing

```text
method MultipartParser.execute(data: String) -> MultipartResult
```

```text
type MultipartResult is
    Complete(List<Part>) |
    Partial |
    Error(MultipartError)
end
```

### Part

```text
type Part is struct
    name: String,
    filename: String | Nil,
    content_type: String,
    content: PartContent,
end
```

```text
type PartContent is
    Text(String) |
    File(String, List<Byte>) |
    Stream(PartStream)
end
```

### Streaming

```text
type PartStream is struct
    -- internal representation
end

method PartStream.read(chunk_size: Size) -> String | Nil | Error
method PartStream.close() -> Nil
```

### Usage

```hexal
import
    Net from "std/net"
end

handler := fun (req: Net.Request) -> Net.Response | Error {
    boundary := req.headers.get("Content-Type")
        .split("boundary=")[1]

    parser := Net.MultipartParser.new(
        boundary = boundary,
        max_file_size = 10 * 1024 * 1024,
        max_parts = 10,
    )

    result := parser.execute(req.body)
    match result {
        Net.MultipartResult.Complete(parts) => {
            for part in parts {
                match part.content {
                    Net.PartContent.Text(value) => {
                        -- handle text field
                    }
                    Net.PartContent.File(name, data) => {
                        -- handle file upload
                    }
                    Net.PartContent.Stream(stream) => {
                        -- handle streaming file
                    }
                }
            }
        }
        Net.MultipartResult.Error(err) => {
            Net.Response.status(400).text("Invalid multipart data")
        }
    }
}
```

### Multipart response

```text
fun serialize_multipart(
    boundary: String,
    parts: Slice<Part>,
) -> String
```

- Serializes parts into a valid multipart message.
- Used for testing or generating multipart responses.

## Multipart format

### Request

```
POST /upload HTTP/1.1
Content-Type: multipart/form-data; boundary=----WebKitFormBoundary7MA4YWxkTrZu0gW
Content-Length: 1234

------WebKitFormBoundary7MA4YWxkTrZu0gW
Content-Disposition: form-data; name="field1"

value1
------WebKitFormBoundary7MA4YWxkTrZu0gW
Content-Disposition: form-data; name="file1"; filename="test.txt"
Content-Type: text/plain

[file content]
------WebKitFormBoundary7MA4YWxkTrZu0gW--
```

### Boundary

- Boundary is a string provided by the client in `Content-Type`.
- Boundary is prepended to `--` in the multipart body.
- Final boundary is followed by `--`.

### Part headers

| Header | Required | Description |
| --- | --- | --- |
| Content-Disposition | Yes | `form-data; name="field"` or `form-data; name="file"; filename="test.txt"` |
| Content-Type | No | MIME type of the part content |

### Part content

- Text fields have no `Content-Type` or `Content-Type: text/plain`.
- File uploads have `Content-Type` matching the file type.
- Content is everything after the part headers until the next boundary.

## Streaming parsing

For large files, use streaming to avoid buffering the entire file:

```hexal
handler := fun (req: Net.Request) -> Net.Response | Error {
    boundary := req.headers.get("Content-Type")
        .split("boundary=")[1]

    parser := Net.MultipartParser.new(
        boundary = boundary,
        max_file_size = 100 * 1024 * 1024,
        max_parts = 10,
    )

    -- Stream the body
    loop {
        match req.body_stream.read(65536) {
            Net.Result.Ok(chunk) => {
                parser.execute(chunk)
            }
            Net.Result.Err => {
                break
            }
        }
    }

    -- Get final result
    match parser.finish() {
        Net.MultipartResult.Complete(parts) => {
            -- handle parts
        }
    }
}
```

## C23 lowering

### Parser state machine

```c
typedef struct hex_multipart_parser {
    int state;
    char boundary[256];
    size_t boundary_len;
    size_t max_file_size;
    size_t max_parts;
    size_t parts_count;
    /* internal buffers */
} hex_multipart_parser;
```

### States

```c
enum {
    HEX_MULTIPART_START,
    HEX_MULTIPART_BOUNDARY,
    HEX_MULTIPART_HEADERS,
    HEX_MULTIPART_CONTENT,
    HEX_MULTIPART_END,
    HEX_MULTIPART_ERROR
};
```

### Boundary matching

```c
bool hex_match_boundary(hex_multipart_parser *parser,
                        const char *data, size_t len) {
    /* compare data against --boundary */
    if (len < parser->boundary_len + 2) return false;
    if (data[0] != '-' || data[1] != '-') return false;
    return memcmp(data + 2, parser->boundary,
                  parser->boundary_len) == 0;
}
```

### Part header parsing

```c
void hex_parse_part_header(hex_multipart_parser *parser,
                           const char *line, size_t len) {
    /* Content-Disposition: form-data; name="field"; filename="file.txt" */
    /* Content-Type: text/plain */

    if (strncmp(line, "Content-Disposition:", 19) == 0) {
        /* parse name and filename */
    } else if (strncmp(line, "Content-Type:", 13) == 0) {
        /* parse content type */
    }
}
```

### Content accumulation

```c
void hex_accumulate_content(hex_multipart_parser *parser,
                            const char *data, size_t len) {
    /* check file size limit */
    if (parser->current_part->is_file) {
        parser->current_part->size += len;
        if (parser->current_part->size > parser->max_file_size) {
            parser->state = HEX_MULTIPART_ERROR;
            return;
        }
    }

    /* accumulate content */
    hex_buffer_append(&parser->current_part->content, data, len);
}
```

## Performance considerations

- **Streaming**: Large files are streamed; no buffering of the entire
  file in memory.
- **Boundary matching**: Uses memcmp for fast boundary detection.
- **Incremental parsing**: Parser maintains state between calls;
  suitable for incremental input.
- **Memory usage**: Only one part is buffered at a time; streaming
  parts are not buffered.

## Demand rules

- `MultipartParser`, `Part`, `PartContent`, `PartStream`, and
  `MultipartResult` select the multipart parsing component.
- The multipart parsing component does not select libuv, native
  bootstrap, or the event bridge; it is a pure data structure.
- A program that does not use `MultipartParser` does not select the
  multipart parsing component.

## Required sweep

- Multipart parser state machine in `hexal/multipart.c` and
  `hexal/multipart.h`;
- boundary detection and matching;
- part header parsing (Content-Disposition, Content-Type);
- content accumulation (text and binary);
- streaming parsing for large files;
- file size limits;
- part count limits;
- multipart serialization;
- demand discovery for multipart parsing component;
- workbench snippet and manifest entries;
- `docs/reference.md` multipart surface after explicit approval.

## Validation

This section is exhaustive:

- parser accepts valid multipart form data;
- parser detects boundary correctly;
- parser parses part headers (Content-Disposition, Content-Type);
- parser extracts field name from Content-Disposition;
- parser extracts filename from Content-Disposition;
- parser handles text fields without Content-Type;
- parser handles file uploads with Content-Type;
- parser rejects files exceeding max_file_size;
- parser rejects requests exceeding max_parts;
- parser handles empty parts;
- parser handles parts with no content;
- parser handles binary content correctly;
- streaming parser handles large files without buffering;
- multipart serialization produces valid multipart output;
- parser state is preserved between calls;
- parser does not allocate memory for streaming parts;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support multipart/mixed (multiple files per part).
2. Whether to add a `MultipartStream` that yields parts as a stream.
3. Whether to support nested multipart boundaries.
4. Whether to add a `MultipartBuilder` for constructing multipart
   requests.
5. Whether to support Content-Transfer-Encoding (deprecated but still
   used).

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the multipart parsing types and contracts only after behavior
stabilizes and with explicit user approval.
