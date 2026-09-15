# RFC 0205: Web Server — Rate Limiting

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the web server syntax (RFC 0210)
  and middleware architecture (RFC 0202) landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax), RFC 0202 (middleware architecture),
  and the implemented RFCs 0145 (libuv runtime), 0146 (mimalloc), and 0168
  (libuv capability arc)
- Coordinates with: RFC 0196 (Redis) for distributed rate limiting, and
  RFC 0186 (stdlib boundary) for module placement
- Does not add: DDoS protection, IP blocking, or WAF functionality

## Motivation

Public APIs and web servers need rate limiting to prevent abuse, protect
backend services, and ensure fair resource allocation. Without rate
limiting, a single client can overwhelm the server with requests.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Token bucket algorithm | Pick up | Most flexible and common |
| Sliding window counter | Pick up | Simpler alternative |
| Per-IP rate limiting | Pick up | Most common use case |
| Per-key rate limiting (API key, user ID) | Pick up | Required for authenticated APIs |
| Custom key rate limiting | Pick up | Required for flexibility |
| HTTP 429 response | Pick up | Standard rate limit response |
| Retry-After header | Pick up | Required for client retry |
| Rate limit headers (X-RateLimit-*) | Pick up | Required for client awareness |
| In-memory rate limiting | Pick up | Single-server use case |
| Distributed rate limiting (Redis) | Pick up | Multi-server use case |
| Sliding window log | Skip in v1 | Memory-intensive for high throughput |
| Fixed window | Skip in v1 | Boundary burst issues |
| Leaky bucket | Skip in v1 | Token bucket is more flexible |
| Rate limit bypass (whitelist) | Pick up | Required for trusted clients |

## Source surface

### Rate limiter

```hexal
import
    Net from "std/net"
end

limiter := Net.RateLimiter.new(
    algorithm = Net.Algorithm.token_bucket,
    rate = 100,
    burst = 200,
    key = Net.RateKey.ip,
)
```

```text
type RateLimiter is struct
    -- internal representation
end

fun RateLimiter.new(
    algorithm: Algorithm,
    rate: UInt64,
    burst: UInt64,
    key: RateKey,
) -> RateLimiter
```

### Algorithms

```text
type Algorithm is
    token_bucket |
    sliding_window
end
```

### Rate keys

```text
type RateKey is
    ip |
    header(String) |
    query(String) |
    path |
    custom(Fun<(Request) : String>)
end
```

### Rate limiting middleware

```text
fun rate_limit(limiter: RateLimiter) -> Middleware
```

```hexal
router.use(Net.rate_limit(limiter))
```

### Usage

```hexal
import
    Net from "std/net"
end

-- Per-IP: 100 requests/second, burst of 200
limiter := Net.RateLimiter.new(
    algorithm = Net.Algorithm.token_bucket,
    rate = 100,
    burst = 200,
    key = Net.RateKey.ip,
)

-- Per-API key: 1000 requests/minute, burst of 2000
api_limiter := Net.RateLimiter.new(
    algorithm = Net.Algorithm.sliding_window,
    rate = 1000,
    burst = 2000,
    key = Net.RateKey.header("X-API-Key"),
)

router.use(Net.rate_limit(limiter))
router.use(Net.rate_limit(api_limiter))
```

### Response

When rate limit is exceeded:

```
HTTP/1.1 429 Too Many Requests
Content-Type: application/json
Retry-After: 1
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 0
X-RateLimit-Reset: 1694764801

{
  "error": "rate_limit_exceeded",
  "message": "Rate limit exceeded. Try again in 1 seconds.",
  "retry_after": 1
}
```

### Rate limit headers

Every response includes:

```
X-RateLimit-Limit: 100
X-RateLimit-Remaining: 95
X-RateLimit-Reset: 1694764801
```

- `X-RateLimit-Limit`: Maximum requests per window.
- `X-RateLimit-Remaining`: Remaining requests in current window.
- `X-RateLimit-Reset`: Unix timestamp when the window resets.

### Whitelist

```text
method RateLimiter.whitelist(key: String)
method RateLimiter.whitelist_range(cidr: String)
```

- Whitelisted keys bypass rate limiting.
- `whitelist_range` whitelists an entire CIDR range (e.g., `10.0.0.0/8`).

## Token bucket algorithm

The token bucket algorithm allows bursts while maintaining a steady rate:

1. Bucket starts full with `burst` tokens.
2. Each request consumes one token.
3. Tokens are added at `rate` tokens per second.
4. When bucket is empty, requests are rejected.

```c
typedef struct hex_token_bucket {
    double tokens;
    double last_refill;
    uint64_t rate;
    uint64_t burst;
} hex_token_bucket;

bool hex_token_bucket_allow(hex_token_bucket *bucket) {
    double now = uv_now(uv_default_loop()) / 1000.0;
    double elapsed = now - bucket->last_refill;
    bucket->tokens = min(bucket->burst,
                         bucket->tokens + elapsed * bucket->rate);
    bucket->last_refill = now;

    if (bucket->tokens >= 1.0) {
        bucket->tokens -= 1.0;
        return true;
    }
    return false;
}
```

## Sliding window algorithm

The sliding window counter uses the current and previous window counts:

1. Divide time into fixed windows (e.g., 1 minute).
2. Count requests in the current window.
3. Estimate the count in the previous window based on the position in
   the current window.
4. If the estimated count exceeds the limit, reject.

```c
typedef struct hex_sliding_window {
    uint64_t current_count;
    uint64_t previous_count;
    double window_start;
    uint64_t rate;
    uint64_t burst;
} hex_sliding_window;

bool hex_sliding_window_allow(hex_sliding_window *window) {
    double now = uv_now(uv_default_loop()) / 1000.0;
    double window_size = 60.0; /* 1 minute */
    double position = fmod(now, window_size);
    double weight = 1.0 - (position / window_size);

    uint64_t estimated = window->current_count +
                         (uint64_t)(window->previous_count * weight);

    if (estimated >= window->rate) {
        return false;
    }

    window->current_count++;
    return true;
}
```

## Redis-based distributed rate limiting

For multi-server deployments, rate limiting state is stored in Redis:

```c
bool hex_redis_rate_limit(redis_connection *conn,
                          const char *key,
                          uint64_t rate,
                          uint64_t burst) {
    /* Use Redis INCR + EXPIRE for sliding window */
    char redis_key[256];
    snprintf(redis_key, sizeof(redis_key), "ratelimit:%s", key);

    uint64_t count = redis_incr(conn, redis_key);
    redis_expire(conn, redis_key, 60);

    return count <= rate;
}
```

- Redis-based limiting uses the same sliding window algorithm.
- The Redis key is `ratelimit:<key>`.
- The key expires after the window duration.

## C23 lowering

### In-memory storage

Rate limit state is stored per-key in a hash map:

```c
typedef struct hex_rate_limiter {
    hex_algorithm algorithm;
    uint64_t rate;
    uint64_t burst;
    hex_rate_key key;
    hex_dict buckets; /* key -> token_bucket or sliding_window */
    hex_dict whitelist;
} hex_rate_limiter;
```

### Key extraction

```c
const char *hex_rate_key_extract(hex_rate_key key, hex_request *req) {
    switch (key.type) {
        case HEX_RATE_KEY_IP:
            return req->remote_addr;
        case HEX_RATE_KEY_HEADER:
            return hex_request_header(req, key.header);
        case HEX_RATE_KEY_QUERY:
            return hex_request_query(req, key.query);
        case HEX_RATE_KEY_PATH:
            return req->path;
        case HEX_RATE_KEY_CUSTOM:
            return key.custom_fn(req);
    }
}
```

### Response headers

```c
void hex_add_rate_limit_headers(hex_response *resp,
                                uint64_t limit,
                                uint64_t remaining,
                                uint64_t reset) {
    char header[64];
    snprintf(header, sizeof(header), "%lu", limit);
    hex_response_set_header(resp, "X-RateLimit-Limit", header);
    snprintf(header, sizeof(header), "%lu", remaining);
    hex_response_set_header(resp, "X-RateLimit-Remaining", header);
    snprintf(header, sizeof(header), "%lu", reset);
    hex_response_set_header(resp, "X-RateLimit-Reset", header);
}
```

## Performance considerations

- **Per-key state**: Rate limit state is per-key; memory usage scales
  with unique keys.
- **Lazy cleanup**: Expired entries are cleaned up lazily on access.
- **No lock contention**: In-memory rate limiting is lock-free; each
  key is independent.
- **Redis overhead**: Distributed rate limiting adds one Redis call per
  request; acceptable for most use cases.

## Demand rules

- `RateLimiter`, `Algorithm`, `RateKey`, and `rate_limit` select the
  rate limiting component.
- The rate limiting component does not select libuv, native bootstrap,
  or the event bridge; it is a pure data structure.
- A program that uses `Redis` for distributed rate limiting selects the
  Redis component (RFC 0196).
- A program that does not use `RateLimiter` does not select the rate
  limiting component.

## Required sweep

- Token bucket implementation in `hexal/ratelimit.c`;
- Sliding window implementation;
- In-memory rate limit state (per-key hash map);
- Redis-based distributed rate limiting;
- rate limiting middleware;
- rate limit headers (X-RateLimit-*);
- HTTP 429 response;
- Retry-After header;
- whitelist and whitelist range;
- demand discovery for rate limiting component;
- workbench snippet and manifest entries;
- `docs/reference.md` rate limiting surface after explicit approval.

## Validation

This section is exhaustive:

- token bucket allows requests up to burst;
- token bucket rejects requests when empty;
- token bucket refills tokens at the configured rate;
- sliding window allows requests up to rate;
- sliding window rejects requests when limit exceeded;
- per-IP rate limiting extracts IP from request;
- per-header rate limiting extracts header from request;
- per-query rate limiting extracts query parameter from request;
- custom key rate limiting uses the custom function;
- HTTP 429 response includes Retry-After header;
- rate limit headers are included in every response;
- whitelisted keys bypass rate limiting;
- whitelist range whitelists entire CIDR ranges;
- Redis-based rate limiting works across multiple servers;
- expired entries are cleaned up lazily;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support a `RateLimiterCallback` that is called when a
   request is rate-limited (for logging or alerting).
2. Whether to add a `RateLimiter.reset` method to clear all rate limit
   state.
3. Whether to support per-route rate limiting (different limits for
   different endpoints).
4. Whether to add a `RateLimiter.status` method to get current rate
   limit state for a key.
5. Whether to support adaptive rate limiting (adjust limits based on
   server load).

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the rate limiting types and contracts only after behavior stabilizes
and with explicit user approval.
