# RFC 0203: Web Server — Logging and Observability

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
- Does not add: distributed tracing (OpenTelemetry), metrics export
  (Prometheus), or structured logging libraries

## Motivation

Production web servers need logging for debugging, monitoring, and
compliance. Without structured logging and observability, operators cannot
understand server behavior, diagnose issues, or meet audit requirements.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Request logging (method, path, status, duration) | Pick up | Most common use case |
| Log levels (debug, info, warn, error) | Pick up | Required for filtering |
| Structured logging (JSON) | Pick up | Required for log aggregation |
| Unstructured logging (text) | Pick up | Required for development |
| Log to stdout/stderr | Pick up | Default output |
| Log to file | Pick up | Required for production |
| Log rotation (size-based) | Pick up | Required for disk management |
| Log rotation (time-based) | Skip in v1 | Complexity disproportionate to initial surface |
| Custom log fields | Pick up | Required for context |
| Request ID tracking | Pick up | Required for tracing |
| Response time tracking | Pick up | Required for performance monitoring |
| Error logging with stack traces | Pick up | Required for debugging |
| Access log format (Apache/Nginx) | Pick up | Required for compatibility |
| Metrics export (Prometheus) | Skip in v1 | Separate concern, can be added later |
| Distributed tracing (OpenTelemetry) | Skip in v1 | Complexity disproportionate to initial surface |
| Log filtering (sample rate) | Skip in v1 | Complexity disproportionate to initial surface |
| Log redaction (PII masking) | Pick up | Required for compliance |

## Source surface

### Logger

```hexal
import
    Log from "std/log"
end

logger := Log.Logger.new(
    level = Log.Level.info,
    format = Log.Format.json,
    output = Log.Output.stdout,
)
```

```text
type Logger is struct
    -- internal representation
end

fun Logger.new(
    level: Level,
    format: Format,
    output: Output,
) -> Logger
```

### Log levels

```text
type Level is
    debug |
    info |
    warn |
    error
end
```

### Log format

```text
type Format is
    json |
    text
end
```

### Log output

```text
type Output is
    stdout |
    stderr |
    file(String)
end
```

### Logging methods

```text
method Logger.debug(message: String, fields: Dict<String, Value> | Nil)
method Logger.info(message: String, fields: Dict<String, Value> | Nil)
method Logger.warn(message: String, fields: Dict<String, Value> | Nil)
method Logger.error(message: String, fields: Dict<String, Value> | Nil)
```

### Usage

```hexal
logger := Log.Logger.new(
    level = Log.Level.info,
    format = Log.Format.json,
    output = Log.Output.stdout,
)

logger.info("Server started", Dict(
    "port" = 8080,
    "host" = "0.0.0.0",
))
```

### JSON output

```json
{
  "level": "info",
  "message": "Server started",
  "timestamp": "2026-09-15T10:30:00Z",
  "fields": {
    "port": 8080,
    "host": "0.0.0.0"
  }
}
```

### Text output

```
2026-09-15T10:30:00Z INFO Server started port=8080 host=0.0.0.0
```

### Request logging middleware

```text
fun request_logging(logger: Logger) -> Middleware
```

```hexal
router.use(Log.request_logging(logger))
```

The middleware logs:

```json
{
  "level": "info",
  "message": "GET /users 200 12ms",
  "timestamp": "2026-09-15T10:30:00Z",
  "fields": {
    "method": "GET",
    "path": "/users",
    "status": 200,
    "duration_ms": 12,
    "request_id": "a1b2c3d4-e5f6-7890-abcd-ef1234567890",
    "remote_addr": "192.168.1.1:54321",
    "user_agent": "Mozilla/5.0"
  }
}
```

### Request ID

```text
method Request.request_id() -> String
```

- Request ID is generated per request and stored in the Context.
- Default format: UUID v4.
- The request ID is logged with every log entry for that request.

### Log rotation

```text
type RotatingFile is struct
    -- internal representation
end

fun RotatingFile.new(
    path: String,
    max_size: UInt64,
    max_files: UInt32,
) -> RotatingFile | Error
```

- `max_size` is the maximum file size in bytes before rotation.
- `max_files` is the maximum number of rotated files to keep.
- Old files are deleted when `max_files` is exceeded.

### Log redaction

```text
method Logger.redact(field: String, pattern: String)
```

- `redact` marks a field for redaction.
- `pattern` is a regex pattern to match sensitive values.
- Redacted values are replaced with `[REDACTED]`.

```hexal
logger.redact("password", ".*")
logger.redact("credit_card", "\\d{4}-\\d{4}-\\d{4}-\\d{4}")
```

## C23 lowering

### Logger

```c
typedef struct hex_logger {
    hex_log_level level;
    hex_log_format format;
    hex_log_output output;
    FILE *file;
    /* rotation state */
} hex_logger;

void hex_logger_init(hex_logger *logger, hex_log_level level,
                      hex_log_format format, hex_log_output output);
void hex_logger_debug(hex_logger *logger, const char *message,
                       hex_dict *fields);
void hex_logger_info(hex_logger *logger, const char *message,
                      hex_dict *fields);
void hex_logger_warn(hex_logger *logger, const char *message,
                      hex_dict *fields);
void hex_logger_error(hex_logger *logger, const char *message,
                       hex_dict *fields);
```

### JSON serialization

```c
void hex_log_serialize_json(hex_logger *logger, hex_log_level level,
                            const char *message, hex_dict *fields,
                            char *buffer, size_t len);
```

- Fields are serialized as a JSON object.
- Timestamp is ISO 8601 UTC.

### Text serialization

```c
void hex_log_serialize_text(hex_logger *logger, hex_log_level level,
                            const char *message, hex_dict *fields,
                            char *buffer, size_t len);
```

- Fields are serialized as `key=value` pairs.

### File rotation

```c
void hex_log_rotate(hex_logger *logger);
```

- Checks file size against `max_size`.
- Renames current file to `<name>.<number>`.
- Deletes oldest file if `max_files` exceeded.
- Opens new current file.

### Request ID generation

```c
void hex_generate_request_id(char *buffer, size_t len) {
    /* UUID v4 generation using /dev/urandom or BCryptGenRandom */
}
```

## Performance considerations

- **Lazy logging**: Log entries are only serialized if the log level is
  enabled.
- **Buffered I/O**: Log entries are buffered and flushed periodically.
- **Structured fields**: Fields are stored as a dict; O(1) lookup for
  redaction.
- **File rotation**: Rotation is synchronous; does not block request
  processing.

## Demand rules

- `Logger`, `Level`, `Format`, `Output`, `RotatingFile`, and
  `request_logging` select the logging component.
- The logging component does not select libuv, native bootstrap, or the
  event bridge; it is a pure data structure and file I/O.
- A program that does not use `Logger` does not select the logging
  component.

## Required sweep

- Logger implementation in `hexal/log.c` and `hexal/log.h`;
- JSON and text serialization;
- log levels and filtering;
- log output (stdout, stderr, file);
- file rotation (size-based);
- request logging middleware;
- request ID generation;
- log redaction;
- demand discovery for logging component;
- workbench snippet and manifest entries;
- `docs/reference.md` logging surface after explicit approval.

## Validation

This section is exhaustive:

- `Logger.new` with valid parameters succeeds;
- `debug` logs only when level is `debug`;
- `info` logs when level is `info`, `warn`, or `error`;
- `warn` logs when level is `warn` or `error`;
- `error` logs when level is `error`;
- JSON format produces valid JSON;
- text format produces readable output;
- stdout output writes to stdout;
- stderr output writes to stderr;
- file output writes to the specified file;
- file rotation triggers at `max_size`;
- file rotation creates new files with incrementing numbers;
- file rotation deletes oldest files when `max_files` exceeded;
- request logging middleware logs method, path, status, and duration;
- request ID is generated per request;
- request ID is consistent across middleware;
- redacted fields are replaced with `[REDACTED]`;
- log entry includes timestamp in ISO 8601 format;
- log entry includes all custom fields;
- empty fields dict produces no field output;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support time-based log rotation (hourly, daily).
2. Whether to add a log sampling rate for high-throughput servers.
3. Whether to support remote logging (syslog, log aggregation services).
4. Whether to add a `Log.Context` that automatically includes request ID
   and other context fields.
5. Whether to support async log flushing (non-blocking file I/O).

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the logging types and contracts only after behavior stabilizes and
with explicit user approval.
