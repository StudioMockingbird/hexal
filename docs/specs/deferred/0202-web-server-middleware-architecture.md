# RFC 0202: Web Server — Middleware Architecture

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the web server syntax (RFC 0210)
  and lowering (RFC 0194) landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax), RFC 0194 (web server lowering),
  and the implemented RFCs 0145 (libuv runtime), 0146 (mimalloc), and 0168
  (libuv capability arc)
- Coordinates with: RFC 0210 (web server syntax) for the Request and Response
  types, and RFC 0186 (stdlib boundary) for module placement
- Does not add: specific middleware implementations (logging, compression,
  rate limiting) — those are separate specs

## Motivation

Without middleware, every web server rewrites the same boilerplate:
logging, error handling, CORS headers, authentication checks, request
transformation. Middleware provides a composable interceptor chain that
separates cross-cutting concerns from business logic.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Request interceptor chain | Pick up | Foundation |
| Response interceptor chain | Pick up | Foundation |
| Next function (pass to next middleware) | Pick up | Required for chaining |
| Short-circuit (return early) | Pick up | Required for auth, rate limiting |
| Error middleware | Pick up | Required for error handling |
| Middleware ordering (compose) | Pick up | Required for predictable behavior |
| Per-route middleware | Pick up | Required for route-specific logic |
| Per-method middleware | Skip in v1 | Rare, can be added later |
| Global middleware | Pick up | Required for logging, auth |
| Middleware state (per-request context) | Pick up | Required for passing data between middleware |
| Middleware error handling | Pick up | Required for resilience |
| Conditional middleware | Skip in v1 | Complexity disproportionate to initial surface |

## Source surface

### Middleware function

```text
type Middleware is Fun<(Request, Next) : Response | Error>
```

```text
type Next is Fun<() : Response | Error>
```

- A middleware receives the `Request` and a `Next` function.
- Calling `Next()` passes the request to the next middleware in the chain.
- The middleware can modify the request before calling `Next()`, or
  short-circuit by returning a response without calling `Next()`.

### Router

```text
type Router is struct
    -- internal representation
end

fun Router.new() -> Router

method Router.use(middleware: Middleware) -> Router
method Router.get(path: String, handler: Handler, middleware: Slice<Middleware>) -> Router
method Router.post(path: String, handler: Handler, middleware: Slice<Middleware>) -> Router
method Router.put(path: String, handler: Handler, middleware: Slice<Middleware>) -> Router
method Router.delete(path: String, handler: Handler, middleware: Slice<Middleware>) -> Router
method Router.route(path: String, methods: Slice<String>, handler: Handler, middleware: Slice<Middleware>) -> Router
```

### Handler

```text
type Handler is Fun<(Request) : Response | Error>
```

### Request context

```text
type Context is struct
    -- internal representation
end

method Context.get(key: String) -> Value | Nil
method Context.set(key: String, value: Value)
method Context.delete(key: String)
```

- Context is per-request and shared across all middleware in the chain.
- Middleware uses Context to pass data (e.g., authenticated user, request
  ID) to downstream handlers.

### Usage

```hexal
import
    Net from "std/net"
end

logging := fun (req: Net.Request, next: Net.Next) -> Net.Response | Error {
    start := Net.now()
    response := try next()
    elapsed := Net.now() - start
    Net.print(`${req.method} ${req.path} ${response.status} ${elapsed}ms`)
    response
}

auth := fun (req: Net.Request, next: Net.Next) -> Net.Response | Error {
    token := req.headers.get("Authorization")
    match token {
        Nil => {
            return Net.Response.status(401).text("Unauthorized")
        }
        token => {
            user := try verify_token(token)
            req.context.set("user", user)
            next()
        }
    }
}

router := Net.Router.new()
router.use(logging)
router.get("/users", list_users, [auth])
router.post("/users", create_user, [auth])
```

## Middleware execution order

### Global middleware

Global middleware (added with `use`) runs in the order they are added:

```text
router.use(A)  -- runs first
router.use(B)  -- runs second
router.use(C)  -- runs third
```

Request flow: `A -> B -> C -> Handler -> C -> B -> A`

### Per-route middleware

Per-route middleware runs after global middleware and in the order they
are listed:

```text
router.use(Logging)
router.get("/admin", handler, [Auth, RateLimit])
```

Request flow: `Logging -> Auth -> RateLimit -> Handler -> RateLimit -> Auth -> Logging`

### Response flow

Middleware can modify the response on the way back:

```text
fun cors(req, next) -> Response | Error {
    response := next()
    response.headers.set("Access-Control-Allow-Origin", "*")
    response
}
```

## Error handling

### Error middleware

```text
type ErrorHandler is Fun<(Error, Request, Next) : Response | Error
```

```text
method Router.on_error(handler: ErrorHandler) -> Router
```

- When a middleware or handler returns an `Error`, the error middleware
  is called.
- The error middleware can return a custom response or re-throw the
  error.

### Usage

```hexal
error_handler := fun (
    err: Error,
    req: Net.Request,
    next: Net.Next,
) -> Net.Response | Error {
    Net.print(`Error: ${err.message}`)
    Net.Response.status(500).text("Internal Server Error")
}

router.on_error(error_handler)
```

### Default error handling

When no error middleware is registered:

```text
Response.status(500).text("Internal Server Error")
```

## C23 lowering

### Middleware chain

The middleware chain is a linked list of function pointers:

```c
typedef struct hex_middleware {
    hex_middleware_fn fn;
    struct hex_middleware *next;
} hex_middleware;
```

### Request processing

```c
hex_response hex_middleware_execute(hex_middleware *chain,
                                    hex_request *req) {
    if (chain == NULL) {
        return hex_handler_default(req);
    }
    hex_next_fn next = hex_middleware_next(chain, req);
    return chain->fn(req, next);
}
```

### Next function

```c
hex_response hex_middleware_next(hex_middleware *current,
                                 hex_request *req) {
    return hex_middleware_execute(current->next, req);
}
```

### Context

Context is a per-request key-value store implemented as a hash map:

```c
typedef struct hex_context {
    hex_dict entries;
} hex_context;

void *hex_context_get(hex_context *ctx, const char *key);
void hex_context_set(hex_context *ctx, const char *key, void *value);
void hex_context_delete(hex_context *ctx, const char *key);
```

## Performance considerations

- **Zero allocation**: Middleware chain is built once at startup; no
  per-request allocation for the chain itself.
- **Inlined next**: The `Next()` call can be inlined by the compiler;
  no function pointer overhead in the hot path.
- **Context is a hash map**: O(1) lookup for context values; acceptable
  for typical middleware use.
- **Error propagation**: Errors short-circuit the chain; no wasted work.

## Demand rules

- `Router`, `Middleware`, `Next`, `Handler`, `ErrorHandler`, and
  `Context` select the middleware component.
- The middleware component does not select libuv, native bootstrap, or
  the event bridge; it is a pure data structure and function composition.
- A program that uses only `Router` without a server does not select the
  server lifecycle or TCP listener components.

## Required sweep

- Middleware chain implementation in `hexal/middleware.c` and
  `hexal/middleware.h`;
- Router implementation with path matching and method dispatch;
- Request context (per-request key-value store);
- Error middleware and default error handling;
- demand discovery for middleware component;
- workbench snippet and manifest entries;
- `docs/reference.md` middleware surface after explicit approval.

## Validation

This section is exhaustive:

- global middleware runs in the order they are added;
- per-route middleware runs after global middleware;
- middleware can short-circuit by returning a response;
- middleware can modify the request before calling `Next()`;
- middleware can modify the response after calling `Next()`;
- error middleware is called when a middleware or handler returns error;
- default error handling returns 500 when no error middleware is
  registered;
- context is per-request and shared across middleware;
- context `get`, `set`, and `delete` work correctly;
- middleware chain handles errors without leaking resources;
- empty middleware chain calls the handler directly;
- route matching is case-sensitive;
- route matching does not match partial paths;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support wildcard routes (`/users/*`) or only parameterized
   routes (`/users/:id`).
2. Whether to add route groups for shared middleware.
3. Whether to add a `Router.mount` for sub-routers.
4. Whether to support async middleware (non-blocking next calls).
5. Whether to add a `Context.clone` for middleware that needs to capture
   the context.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the middleware types and contracts only after behavior stabilizes and
with explicit user approval.
