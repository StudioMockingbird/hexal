# RFC 0197: Web Server — PostgreSQL Driver

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the network runtime (RFC 0144)
  and TCP socket operations landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0144 (high-throughput network runtime), the implemented
  RFCs 0145 (libuv runtime), 0146 (mimalloc), and 0168 (libuv capability
  arc), and the current Task, Channel, IO, String, Slice, Dict, and Error
  contracts in `docs/reference.md`
- Coordinates with: RFC 0210 (web server syntax) for the server ecosystem,
  RFC 0195 (TLS) for encrypted PostgreSQL connections, and RFC 0186 (stdlib
  boundary) for module placement
- Does not add: PostgreSQL server implementation, PostgreSQL extensions,
  logical replication, or PostgreSQL-specific SQL functions

## Motivation

PostgreSQL is the most common relational database for production web
applications. Without a native driver, Hexal programs must use `libpq`
through C interop (complex setup, blocking by default) or shell out to
`psql`. A native driver that integrates with the Task scheduler provides
non-blocking queries, prepared statements, and connection pooling.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| TCP connection to PostgreSQL | Pick up | Foundation |
| PostgreSQL wire protocol (v3.0) | Pick up | Wire protocol |
| Simple query protocol | Pick up | Basic queries |
| Extended query protocol | Pick up | Prepared statements |
| Parameterized queries ($1, $2, ...) | Pick up | SQL injection prevention |
| Row result parsing | Pick up | Query results |
| COPY protocol | Pick up | Bulk data loading |
| Notice/Error handling | Pick up | Error reporting |
| SSL/TLS connections | Pick up | Required for production |
| Connection pooling | Pick up | Required for production servers |
| LISTEN/NOTIFY | Pick up | Required for real-time features |
| prepared statement caching | Pick up | Performance |
| Transaction support | Pick up | Required for data integrity |
| Binary protocol | Skip in v1 | Text protocol is sufficient initially |
| Server-side cursors | Skip in v1 | Complex, can be added later |
| Pipeline mode | Skip in v1 | PostgreSQL 14+ only, measure first |
| Connection failover | Skip in v1 | Complexity disproportionate to initial surface |
| Logical replication | Skip in v1 | Operational concern, not driver |
| Advisory locks | Skip in v1 | Can be added later |

## Source surface

### Connection

```hexal
import
    Pg from "std/pg"
end

connection := try Pg.Connection.new(
    host = "localhost",
    port = 5432,
    database = "myapp",
    user = "postgres",
    password = "secret",
    timeout = Pg.Duration.seconds(5)
)
```

```text
type Connection is struct
    -- internal representation
end

fun Connection.new(
    host: String,
    port: UInt16,
    database: String,
    user: String,
    password: String | Nil,
    timeout: Duration,
) -> Connection | Error
```

### Connection pool

```text
type ConnectionPool is struct
    -- internal representation
end

fun ConnectionPool.new(
    host: String,
    port: UInt16,
    database: String,
    user: String,
    password: String | Nil,
    min_connections: Size,
    max_connections: Size,
    timeout: Duration,
) -> ConnectionPool | Error

method ConnectionPool.acquire() -> Connection | Error
method ConnectionPool.release(connection: Connection)
method ConnectionPool.close()
```

### Simple query

```text
method Connection.query(sql: String) -> Result | Error
```

```text
type Result is struct
    -- internal representation
end

method Result.row_count() -> UInt64
method Result.column_count() -> UInt32
method Result.column_name(index: UInt32) -> String
method Result.column_type(index: UInt32) -> PgType
method Result.next() -> Row | Nil
```

```text
type Row is struct
    -- internal representation
end

method Row.get_string(index: UInt32) -> String | Nil
method Row.get_int32(index: UInt32) -> Int32 | Nil
method Row.get_int64(index: UInt32) -> Int64 | Nil
method Row.get_float64(index: UInt32) -> Float64 | Nil
method Row.get_bool(index: UInt32) -> Bool | Nil
method Row.get_bytes(index: UInt32) -> List<Byte> | Nil
method Row.is_null(index: UInt32) -> Bool
```

### Extended query (prepared statements)

```text
method Connection.prepare(name: String, sql: String) -> Statement | Error
method Connection.execute(name: String, params: Slice<String>) -> Result | Error
method Connection.close_statement(name: String) -> Nil | Error
```

```text
type Statement is struct
    -- internal representation
end

method Statement.execute(params: Slice<String>) -> Result | Error
method Statement.close() -> Nil | Error
```

- `prepare` sends a `Parse` message and stores the prepared statement
  server-side.
- `execute` sends a `Bind` + `Execute` + `Sync` message sequence.
- Statements are cached per connection; re-preparing an existing name
  updates the server-side statement.

### Parameterized queries

```text
method Connection.query_params(
    sql: String,
    params: Slice<String>,
) -> Result | Error
```

- Uses the extended query protocol internally.
- Parameters are sent as text format; the driver does not support binary
  parameters in v1.
- Prevents SQL injection by separating SQL from data.

### COPY

```text
method Connection.copy_in(table: String, columns: Slice<String>, data: String) -> UInt64 | Error
method Connection.copy_out(table: String, columns: Slice<String>) -> String | Error
```

- `copy_in` sends data in text CSV format.
- `copy_out` receives data in text CSV format.
- Binary COPY is not supported in v1.

### LISTEN/NOTIFY

```text
method Connection.listen(channel: String) -> Nil | Error
method Connection.notify(channel: String, payload: String) -> Nil | Error
method Connection.next_notification() -> Notification | Nil | Error
```

```text
type Notification is struct
    channel: String,
    payload: String,
    sender_pid: UInt32,
end
```

- `listen` registers the connection for notifications on the given channel.
- `next_notification` returns the next notification or `Nil` if none are
  pending.
- Notifications are received asynchronously; the Task parks during
  `next_notification`.

### Transactions

```text
method Connection.begin() -> Nil | Error
method Connection.commit() -> Nil | Error
method Connection.rollback() -> Nil | Error
```

- `begin` sends `BEGIN`.
- `commit` sends `COMMIT`.
- `rollback` sends `ROLLBACK`.
- Nested transactions (savepoints) are not supported in v1.

## PostgreSQL wire protocol

The driver implements PostgreSQL wire protocol v3.0 (PostgreSQL 7.0+).

### Message format

Every message has a 1-byte type code, 4-byte length, and payload:

```
| type (1 byte) | length (4 bytes, big-endian) | payload (length - 4 bytes) |
```

### Startup

1. Send `StartupMessage` with protocol version 3.0 and parameters
   (`user`, `database`, `client_encoding`).
2. Server responds with `AuthenticationOk` or an authentication request.
3. For password auth, send `PasswordMessage` with MD5 or SASL hash.
4. Server responds with `BackendKeyData` and `ReadyForQuery`.

### Simple query

1. Send `Query` with the SQL string.
2. Server responds with `RowDescription`, `DataRow`, `CommandComplete`,
   and `ReadyForQuery`.

### Extended query

1. Send `Parse` with statement name and SQL.
2. Send `Bind` with parameter values.
3. Send `Describe` (optional, for result column info).
4. Send `Execute` with row count limit.
5. Send `Sync`.
6. Server responds with `ParseComplete`, `BindComplete`, `RowDescription`,
   `DataRow`, `CommandComplete`, and `ReadyForQuery`.

### COPY

1. Send `Query` with `COPY ... FROM STDIN` or `COPY ... TO STDOUT`.
2. For `COPY IN`, send `CopyData` rows followed by `CopyDone`.
3. For `COPY OUT`, receive `CopyData` rows followed by `CopyDone`.
4. Server responds with `CommandComplete` and `ReadyForQuery`.

### Error and notice handling

- `ErrorResponse` contains severity, code, message, detail, hint, and
  other fields.
- `NoticeResponse` has the same structure; used for warnings and info.
- The driver maps PostgreSQL error codes to `ErrorKind` values.

## libuv integration

PostgreSQL I/O uses the same libuv event loop:

### Connection

```c
uv_tcp_t pg_handle;
uv_tcp_init(uv_default_loop(), &pg_handle);
uv_tcp_connect(&connect_req, &pg_handle, addr, on_connect);
```

- The connection is non-blocking; `uv_read_start` feeds data to the
  PostgreSQL protocol parser.

### Query

- A query sends a `Query` or extended query sequence through `uv_write`.
- Results are read and parsed incrementally through `uv_read_start`.
- The Task parks during the read and is woken by the libuv callback
  when the result set is complete.

## Error handling

| Condition | ErrorKind | Message |
| --- | --- | --- |
| Connection refused | `ConnectionRefused` | `PostgreSQL connection refused` |
| Connection timeout | `Timeout` | `PostgreSQL connection timed out` |
| Query timeout | `Timeout` | `PostgreSQL query timed out` |
| Authentication failed | `PermissionDenied` | `PostgreSQL authentication failed` |
| Database not found | `InvalidInput` | `PostgreSQL database not found` |
| Protocol error | `InvalidInput` | `PostgreSQL protocol error` |
| Syntax error | `InvalidInput` | `PostgreSQL syntax error` |
| Unique violation | `Busy` | `PostgreSQL unique constraint violation` |
| Foreign key violation | `Busy` | `PostgreSQL foreign key constraint violation` |
| Deadlock detected | `Busy` | `PostgreSQL deadlock detected` |
| Connection closed | `ConnectionReset` | `PostgreSQL connection closed` |

## Demand rules

- `Connection`, `ConnectionPool`, `Statement`, `Result`, `Row`, and
  `Notification` select the PostgreSQL component.
- The PostgreSQL component selects libuv, native bootstrap, and the
  event bridge.
- A program that does not use PostgreSQL types does not select the
  PostgreSQL component.

## Required sweep

- PostgreSQL wire protocol v3.0 parser and serializer in `hexal/pg.c`;
- connection management, authentication, and SSL negotiation;
- simple query and extended query protocols;
- COPY IN/OUT support;
- LISTEN/NOTIFY support;
- notice/error handling and error code mapping;
- connection pool management;
- demand discovery for PostgreSQL component;
- error mapping for PostgreSQL-specific failures;
- workbench snippet and manifest entries;
- `docs/reference.md` PostgreSQL surface after explicit approval.

## Validation

This section is exhaustive:

- connection establishment with password authentication (MD5 and SASL);
- connection timeout returns exact error;
- simple query returns correct `Result` with `RowDescription` and
  `DataRow` messages;
- prepared statement `Parse` + `Bind` + `Execute` returns correct
  `Result`;
- parameterized query returns correct results;
- `COPY IN` sends data and returns row count;
- `COPY OUT` receives data as text CSV;
- `LISTEN` registers for notifications and `next_notification` returns
  correct `Notification`;
- `NOTIFY` sends notification to other connections;
- `begin`/`commit`/`rollback` send correct protocol messages;
- error response maps PostgreSQL error codes to correct `ErrorKind`;
- notice response is captured and accessible;
- connection pool acquire, release, and close;
- authentication failed returns exact error;
- database not found returns exact error;
- unique violation returns exact error;
- deadlock detected returns exact error;
- connection closed during query returns exact error;
- existing Task, Channel, Mutex, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support SASL authentication (SCRAM-SHA-256) in addition to
   MD5, or require the server to accept MD5.
2. Whether `Result` should provide named column access in addition to
   index-based access.
3. Whether to expose the raw PostgreSQL protocol for custom commands.
4. Whether `ConnectionPool` should support automatic reconnection and
   statement re-prepare on connection loss.
5. Whether to add binary parameter format support for numeric types.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the PostgreSQL types, commands, and protocol contracts only after
behavior stabilizes and with explicit user approval.
