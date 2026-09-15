# RFC 0196: Web Server — Redis Driver

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
  RFC 0195 (TLS) for encrypted Redis connections, and RFC 0186 (stdlib
  boundary) for module placement
- Does not add: Redis server implementation, Redis Cluster, Redis Sentinel,
  Redis Streams, or Redis modules

## Motivation

Redis is the most common in-memory data store for web applications: session
caching, rate limiting, pub/sub, job queues, and distributed locks. Without a
Redis driver, every Hexal web server must shell out to `redis-cli` or use a
C library through FFI. A native driver keeps the deployment story simple and
lets the driver integrate with the Task scheduler for non-blocking I/O.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| TCP connection to Redis | Pick up | Foundation |
| RESP2/RESP3 protocol | Pick up | Wire protocol |
| String commands (GET, SET, DEL, ...) | Pick up | Most common operations |
| Hash commands (HGET, HSET, HDEL, ...) | Pick up | Common for structured data |
| List commands (LPUSH, RPUSH, LPOP, RPOP, ...) | Pick up | Common for queues |
| Set commands (SADD, SREM, SMEMBERS, ...) | Pick up | Common for tags/unique values |
| Sorted set commands (ZADD, ZRANGE, ...) | Pick up | Common for leaderboards |
| Pipelining | Pick up | Required for performance |
| Pub/Sub | Pick up | Required for real-time features |
| Transactions (MULTI/EXEC) | Pick up | Required for atomicity |
| Lua scripting (EVAL) | Skip in v1 | Complex, can be added later |
| Redis Cluster | Skip in v1 | Complex, requires slot routing |
| Redis Sentinel | Skip in v1 | Complex, requires failover logic |
| Redis Streams | Skip in v1 | Complex, can be added later |
| ACL authentication | Pick up | Required for production |
| TLS connections | Separate RFC | RFC 0195 owns TLS |
| Connection pooling | Pick up | Required for production servers |
| Sentinel/Cluster failover | Skip in v1 | Complexity disproportionate to initial surface |

## Source surface

### Connection

```hexal
import
    Redis from "std/redis"
end

connection := try Redis.Connection.new(
    host = "127.0.0.1",
    port = 6379,
    password = "secret",
    database = 0,
    timeout = Redis.Duration.seconds(5)
)
```

```text
type Connection is struct
    -- internal representation
end

fun Connection.new(
    host: String,
    port: UInt16,
    password: String | Nil,
    database: UInt8,
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
    password: String | Nil,
    database: UInt8,
    min_connections: Size,
    max_connections: Size,
    timeout: Duration,
) -> ConnectionPool | Error

method ConnectionPool.acquire() -> Connection | Error
method ConnectionPool.release(connection: Connection)
method ConnectionPool.close()
```

### Commands

Every Redis command is a method on `Connection`:

```text
-- String commands
method Connection.get(key: String) -> String | Nil | Error
method Connection.set(key: String, value: String) -> Nil | Error
method Connection.set_ex(key: String, value: String, seconds: UInt32) -> Nil | Error
method Connection.set_px(key: String, value: String, milliseconds: UInt32) -> Nil | Error
method Connection.set_nx(key: String, value: String) -> Bool | Error
method Connection.del(keys: Slice<String>) -> UInt64 | Error
method Connection.exists(keys: Slice<String>) -> UInt64 | Error
method Connection.expire(key: String, seconds: UInt32) -> Bool | Error
method Connection.ttl(key: String) -> Int64 | Error
method Connection.incr(key: String) -> Int64 | Error
method Connection.incr_by(key: String, amount: Int64) -> Int64 | Error
method Connection.decr(key: String) -> Int64 | Error
method Connection.append(key: String, value: String) -> UInt64 | Error

-- Hash commands
method Connection.hget(key: String, field: String) -> String | Nil | Error
method Connection.hset(key: String, field: String, value: String) -> Bool | Error
method Connection.hdel(key: String, fields: Slice<String>) -> UInt64 | Error
method Connection.hget_all(key: String) -> Dict<String, String> | Nil | Error
method Connection.hkeys(key: String) -> List<String> | Error
method Connection.hvals(key: String) -> List<String> | Error
method Connection.hexists(key: String, field: String) -> Bool | Error
method Connection.hincr_by(key: String, field: String, amount: Int64) -> Int64 | Error

-- List commands
method Connection.lpush(key: String, values: Slice<String>) -> UInt64 | Error
method Connection.rpush(key: String, values: Slice<String>) -> UInt64 | Error
method Connection.lpop(key: String) -> String | Nil | Error
method Connection.rpop(key: String) -> String | Nil | Error
method Connection.lrange(key: String, start: Int64, stop: Int64) -> List<String> | Error
method Connection.llen(key: String) -> UInt64 | Error
method Connection.lindex(key: String, index: Int64) -> String | Nil | Error

-- Set commands
method Connection.sadd(key: String, members: Slice<String>) -> UInt64 | Error
method Connection.srem(key: String, members: Slice<String>) -> UInt64 | Error
method Connection.smembers(key: String) -> List<String> | Error
method Connection.sismember(key: String, member: String) -> Bool | Error
method Connection.scard(key: String) -> UInt64 | Error

-- Sorted set commands
method Connection.zadd(key: String, members: Slice<(String, Float64)>) -> UInt64 | Error
method Connection.zrange(key: String, start: Int64, stop: Int64) -> List<String> | Error
method Connection.zrange_with_scores(key: String, start: Int64, stop: Int64) -> List<(String, Float64)> | Error
method Connection.zscore(key: String, member: String) -> Float64 | Nil | Error
method Connection.zrank(key: String, member: String) -> UInt64 | Nil | Error
method Connection.zrem(key: String, members: Slice<String>) -> UInt64 | Error
method Connection.zcard(key: String) -> UInt64 | Error
```

### Pipelining

```text
type Pipeline is struct
    -- internal representation
end

fun Pipeline.new(connection: Connection) -> Pipeline

method Pipeline.get(key: String)
method Pipeline.set(key: String, value: String)
method Pipeline.del(keys: Slice<String>)
-- ... all commands available on Pipeline

method Pipeline.execute() -> List<Result | Error>
```

- Pipelining sends multiple commands in a single write and reads all
  responses in a single read.
- Each command's result is a `Result` (or `Error` if that command failed).

### Pub/Sub

```text
type Subscriber is struct
    -- internal representation
end

method Connection.subscribe(channels: Slice<String>) -> Subscriber | Error
method Subscriber.next() -> (String, String) | Nil | Error
method Subscriber.close() -> Nil | Error
```

- `next` returns the channel name and message, or `Nil` if the
  subscription was closed.
- Pub/Sub connections are dedicated; they cannot execute other commands
  while subscribed.

### Transactions

```text
type Transaction is struct
    -- internal representation
end

fun Transaction.new(connection: Connection) -> Transaction

method Transaction.watch(keys: Slice<String>) -> Nil | Error
method Transaction.multi() -> Nil | Error
method Transaction.exec() -> List<Result | Error> | Nil | Error
method Transaction.discard() -> Nil | Error
```

- `exec` returns `Nil` when the transaction was aborted by a watched key
  change.

## RESP protocol

The driver implements RESP2 (Redis Serialization Protocol) and auto-upgrades
to RESP3 when the server supports it.

### RESP2 encoding

- Simple strings: `+OK\r\n`
- Errors: `-ERR message\r\n`
- Integers: `:1000\r\n`
- Bulk strings: `$6\r\nfoobar\r\n`
- Arrays: `*2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n`
- Null: `$-1\r\n`

### RESP3 encoding

- Booleans: `#t\r\n` / `#f\r\n`
- Doubles: `,1.23\r\n`
- Big numbers: `(3492890328409238509324850943850943825024385\r\n`
- Maps: `%2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n`
- Sets: `~2\r\n$3\r\nfoo\r\n$3\r\nbar\r\n`
- Verbatim strings: `=15\r\ntxt:Hello World\r\n`

### Protocol negotiation

- On connection, the driver sends `HELLO 3` to request RESP3.
- If the server does not support RESP3, the driver falls back to RESP2.
- The driver auto-detects the protocol from the first response.

## libuv integration

Redis I/O uses the same libuv event loop as TCP and TLS:

### Connection

```c
uv_tcp_t redis_handle;
uv_tcp_init(uv_default_loop(), &redis_handle);
uv_tcp_connect(&connect_req, &redis_handle, addr, on_connect);
```

- The connection is non-blocking; `uv_read_start` feeds data to the
  RESP parser.
- The RESP parser is a pure C state machine (similar to the HTTP parser
  in RFC 0194).

### Pipelining

- Pipelined commands are serialized into a single `uv_write`.
- Responses are read and parsed incrementally; each response is matched
  to its command by position.

### Pub/Sub

- Pub/Sub uses the same `uv_read_start` / `uv_write` pattern.
- The subscriber Task parks during `uv_read_start` and is woken by the
  libuv callback when a message arrives.

## Error handling

| Condition | ErrorKind | Message |
| --- | --- | --- |
| Connection refused | `ConnectionRefused` | `Redis connection refused` |
| Connection timeout | `Timeout` | `Redis connection timed out` |
| Command timeout | `Timeout` | `Redis command timed out` |
| Authentication failed | `PermissionDenied` | `Redis authentication failed` |
| Database selection failed | `InvalidInput` | `Redis database selection failed` |
| Protocol error | `InvalidInput` | `Redis protocol error` |
| Connection closed | `ConnectionReset` | `Redis connection closed` |
| Transaction aborted | `Busy` | `Redis transaction aborted by WATCH` |

## Demand rules

- `Connection`, `ConnectionPool`, `Pipeline`, `Subscriber`, and
  `Transaction` select the Redis component.
- The Redis component selects libuv, native bootstrap, and the event
  bridge.
- A program that does not use Redis types does not select the Redis
  component.

## Required sweep

- RESP2/RESP3 parser and serializer in `hexal/redis.c`;
- connection management and authentication;
- command serialization for every supported command;
- pipelining support;
- pub/sub support;
- transaction support;
- connection pool management;
- demand discovery for Redis component;
- error mapping for Redis-specific failures;
- workbench snippet and manifest entries;
- `docs/reference.md` Redis surface after explicit approval.

## Validation

This section is exhaustive:

- RESP2 simple string, error, integer, bulk string, array, and null
  parsing;
- RESP3 boolean, double, big number, map, set, and verbatim string
  parsing;
- connection establishment and `HELLO` negotiation;
- authentication with password;
- database selection;
- every supported command returns the correct result type;
- pipelining sends multiple commands and receives correct responses;
- pub/sub subscribe, receive messages, and unsubscribe;
- transaction MULTI/EXEC with and without WATCH;
- connection pool acquire, release, and close;
- command timeout returns exact error;
- connection timeout returns exact error;
- connection closed during command returns exact error;
- authentication failed returns exact error;
- transaction aborted by WATCH returns exact error;
- existing Task, Channel, Mutex, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support Redis 6+ ACL commands (USER, ACL LIST, etc.) or
   keep authentication simple with password only.
2. Whether `ConnectionPool` should support automatic reconnection on
   connection loss.
3. Whether to expose the raw RESP protocol for custom commands.
4. Whether pub/sub should support pattern subscriptions (PSUBSCRIBE).
5. Whether to add Redis Cluster support as a separate spec or keep it
   out of scope indefinitely.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the Redis types, commands, and protocol contracts only after behavior
stabilizes and with explicit user approval.
