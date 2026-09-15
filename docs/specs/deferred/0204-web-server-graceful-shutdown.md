# RFC 0204: Web Server — Graceful Shutdown

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
- Does not add: process management, supervisor trees, or hot code reloading

## Motivation

Production web servers must shut down gracefully: stop accepting new
connections, finish processing in-flight requests, and release resources
before exiting. Without graceful shutdown, deployments cause dropped
connections, data loss, and error spikes.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Signal handling (SIGINT, SIGTERM) | Pick up | Required for container orchestration |
| Drain connections | Pick up | Required for in-flight requests |
| Health check endpoint | Pick up | Required for load balancer integration |
| Readiness probe | Pick up | Required for Kubernetes |
| Liveness probe | Pick up | Required for Kubernetes |
| Shutdown timeout | Pick up | Required for bounded shutdown |
| Pre-shutdown hook | Pick up | Required for resource cleanup |
| Post-shutdown hook | Skip in v1 | After shutdown, nothing runs |
| Hot code reloading | Skip in v1 | Complexity disproportionate to initial surface |
| Process supervisor | Skip in v1 | Operational concern, not library |
| Rolling restart | Skip in v1 | Requires multiple instances |

## Source surface

### Shutdown

```hexal
import
    Net from "std/net"
end

server := try Net.Server.new(host = "0.0.0.0", port = 8080)
server.serve(handler)

-- Handle shutdown signals
Net.on_shutdown(fn () {
    Net.print("Cleaning up...")
    -- close database connections
    -- flush log buffers
    -- release resources
})

Net.on_timeout(fn () {
    Net.print("Shutdown timed out, forcing exit")
})
```

### Shutdown function

```text
fun on_shutdown(handler: Fun<() : Nil>)
fun on_timeout(handler: Fun<() : Nil>)
fun shutdown()
fun force_shutdown()
```

- `on_shutdown` registers a handler called during graceful shutdown.
- `on_timeout` registers a handler called when shutdown times out.
- `shutdown` initiates graceful shutdown.
- `force_shutdown` exits immediately without draining.

### Health check endpoint

```text
method Server.set_health_check(
    path: String,
    handler: Fun<() : HealthStatus>,
)
```

```text
type HealthStatus is
    Healthy |
    Unhealthy |
    Degraded
end
```

- Default path: `/health`.
- Returns 200 for `Healthy`, 503 for `Unhealthy`, 200 for `Degraded`.
- Response body includes status and optional message:

```json
{
  "status": "healthy",
  "message": "All systems operational"
}
```

### Readiness probe

```text
method Server.set_readiness_check(
    path: String,
    handler: Fun<() : Bool>,
)
```

- Default path: `/ready`.
- Returns 200 when `true`, 503 when `false`.
- Used by load balancers to determine if the server should receive
  traffic.

### Liveness probe

```text
method Server.set_liveness_check(
    path: String,
    handler: Fun<() : Bool>,
)
```

- Default path: `/live`.
- Returns 200 when `true`, 503 when `false`.
- Used by orchestrators to determine if the server should be restarted.

### Shutdown timeout

```text
method Server.set_shutdown_timeout(timeout: Duration)
```

- Default: 30 seconds.
- After timeout, remaining connections are closed forcefully.

## Shutdown sequence

1. **Signal received** (SIGINT, SIGTERM).
2. **Stop accepting new connections** (close listener).
3. **Call pre-shutdown hooks** (on_shutdown handlers).
4. **Drain existing connections** (wait for in-flight requests).
5. **Close idle connections** (connections with no active request).
6. **Wait for shutdown timeout** (bounded by `shutdown_timeout`).
7. **Force close remaining connections** (if timeout exceeded).
8. **Exit process** (code 0 for graceful, code 1 for forced).

## Health check responses

### Healthy

```json
{
  "status": "healthy",
  "message": "All systems operational",
  "uptime_seconds": 3600,
  "active_connections": 42,
  "requests_per_second": 1234.56
}
```

### Unhealthy

```json
{
  "status": "unhealthy",
  "message": "Database connection failed",
  "uptime_seconds": 3600,
  "active_connections": 42,
  "requests_per_second": 0
}
```

### Degraded

```json
{
  "status": "degraded",
  "message": "Redis unavailable, using fallback",
  "uptime_seconds": 3600,
  "active_connections": 42,
  "requests_per_second": 500.00
}
```

## C23 lowering

### Signal handling

```c
#include <signal.h>

static uv_signal_t sigint_handle;
static uv_signal_t sigterm_handle;

void hex_signal_init(uv_loop_t *loop) {
    uv_signal_init(loop, &sigint_handle);
    uv_signal_init(loop, &sigterm_handle);
    uv_signal_start(&sigint_handle, hex_on_signal, SIGINT);
    uv_signal_start(&sigterm_handle, hex_on_signal, SIGTERM);
}

void hex_on_signal(uv_signal_t *handle, int signum) {
    hex_server_shutdown();
}
```

### Connection draining

```c
void hex_server_shutdown(void) {
    /* close listener */
    uv_close((uv_handle_t *)&server.listener, NULL);

    /* call pre-shutdown hooks */
    hex_call_shutdown_hooks();

    /* set shutdown timer */
    uv_timer_init(loop, &shutdown_timer);
    uv_timer_start(&shutdown_timer, hex_on_shutdown_timeout,
                   shutdown_timeout_ms, 0);
}

void hex_on_shutdown_timeout(uv_timer_t *handle) {
    /* force close all remaining connections */
    hex_force_close_all();
    exit(1);
}
```

### Health check

```c
void hex_health_check(uv_stream_t *client) {
    hex_health_status status = hex_call_health_handler();
    const char *body;
    int status_code;

    switch (status) {
        case HEX_HEALTHY:
            body = "{\"status\":\"healthy\"}";
            status_code = 200;
            break;
        case HEX_UNHEALTHY:
            body = "{\"status\":\"unhealthy\"}";
            status_code = 503;
            break;
        case HEX_DEGRADED:
            body = "{\"status\":\"degraded\"}";
            status_code = 200;
            break;
    }

    hex_send_http_response(client, status_code, body);
}
```

## Performance considerations

- **No allocation on shutdown**: Shutdown hooks are called once; no
  per-request allocation.
- **Bounded drain time**: Shutdown timeout prevents infinite drain.
- **Health checks are cheap**: Health check handlers should be fast;
  no database queries or external calls.
- **Signal handling is lightweight**: Signal handlers only set a flag;
  actual shutdown happens on the event loop.

## Demand rules

- `on_shutdown`, `on_timeout`, `shutdown`, `force_shutdown`,
  `HealthStatus`, `set_health_check`, `set_readiness_check`, and
  `set_liveness_check` select the graceful shutdown component.
- The graceful shutdown component selects libuv, native bootstrap, and
  the event bridge.
- A program that does not use shutdown types does not select the graceful
  shutdown component.

## Required sweep

- Signal handling (SIGINT, SIGTERM) in `hexal/server.c`;
- connection draining and shutdown timeout;
- health check endpoints (health, readiness, liveness);
- pre-shutdown hooks;
- force shutdown;
- demand discovery for graceful shutdown component;
- workbench snippet and manifest entries;
- `docs/reference.md` graceful shutdown surface after explicit approval.

## Validation

This section is exhaustive:

- SIGINT triggers graceful shutdown;
- SIGTERM triggers graceful shutdown;
- shutdown stops accepting new connections;
- shutdown calls pre-shutdown hooks;
- shutdown drains in-flight requests;
- shutdown closes idle connections;
- shutdown respects shutdown timeout;
- force shutdown exits immediately;
- health check returns 200 for healthy;
- health check returns 503 for unhealthy;
- health check returns 200 for degraded;
- readiness check returns 200 when ready;
- readiness check returns 503 when not ready;
- liveness check returns 200 when alive;
- liveness check returns 503 when dead;
- health check response includes uptime and connection count;
- shutdown timeout fires when drain exceeds timeout;
- pre-shutdown hooks are called in order;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to add a `drain` endpoint that returns the number of
   in-flight requests.
2. Whether to support pre-stop hooks (Kubernetes lifecycle hooks).
3. Whether to add a `shutdown` event that middleware can listen to.
4. Whether to support graceful shutdown for WebSocket connections
   (send Close frame before closing).
5. Whether to add a `shutdown` log entry with the number of drained
   connections.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the graceful shutdown types and contracts only after behavior
stabilizes and with explicit user approval.
