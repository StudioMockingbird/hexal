# RFC 0208: Web Server — Pluggable Backend Contract

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; public backend substitution excluded from the first server;
  implementation not started
- Created: 2026-09-15
- Updated: 2026-10-01
- Depends on: RFC 0210 (web server surface) and RFC 0039 (C interoperability)
- Coordinates with: RFC 0144 (Task-aware socket runtime), RFC 0194 (default
  backend), RFC 0195 (TLS), and RFC 0186 (stdlib boundary)
- Does not add: specific backend implementations (those are separate specs)

## Motivation

The default HTTP backend serves the common case, while library writers may
want to adapt specialized C HTTP libraries. Without a stable adapter boundary,
each adapter would need to define competing Request/Response types and handler
contracts, or the standard library would be locked to one implementation.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| Backend contract | Pick up | Foundation for replaceable implementations |
| Shared Request/Response contract | Pick up | Defined by RFC 0210; backends must preserve it |
| Middleware compatibility | Defer | Middleware is outside the minimum server milestone |
| Handler dispatch contract | Pick up | Required for reuse |
| Backend selection (compile-time) | Pick up | Zero runtime overhead |
| Backend selection (runtime) | Skip in v1 | Complexity disproportionate to initial surface |
| Backend-specific Request/Response types | Skip | HTTP handlers use RFC 0210's shared built-in types |
| Backend negotiation | Skip in v1 | Complexity disproportionate to initial surface |
| Backend discovery (plugin system) | Skip in v1 | Complexity disproportionate to initial surface |

## Design principle

A server backend is a statically selected adapter that satisfies the common
backend contract. The standard library provides Hexal's default backend;
library owners can provide adapters for statically linked C libraries. The
router and handler types remain backend-agnostic. Middleware compatibility is
deferred to RFC 0202.

## Current design direction

- The default backend is Hexal's own HTTP implementation. It satisfies the
  same backend contract as third-party adapters and is selected by
  `Http.serve(config, router)`.
- A library owner can statically link a C HTTP library and provide a Hexal/C
  adapter satisfying the backend contract. Runtime plugin discovery and
  runtime backend negotiation are out of scope.
- Every backend receives the same built-in `Request`, `Response`, `Router`,
  and `ServerConfig` contract from RFC 0210. Backends do not define competing
  request or response types for ordinary HTTP handlers.
- `Http.serve_with(backend, config, router)` selects a custom backend.
  Selection is static: generated code links the selected adapter directly.
  Hexal has no interface declaration, so this RFC does not introduce one or
  lower a general-purpose vtable. The exact `backend` parameter and C adapter
  ABI remain to be specified.
- The backend contract must define request/response body streaming,
  cancellation, error translation, callback lifetime, and ownership across
  the C boundary before implementation.

The adapter is the compatibility boundary, not the underlying C library. A
library owner supplies the adapter that translates the library's callbacks and
buffers to the common Hexal contract. Ordinary handlers do not expose
backend-specific Request or Response types.

## Approved deferral

Public serve_with and substitution ABI are deferred until the default backend
works and a named second implementation demonstrates the useful seam. This proposal
does not gate 0144/0194/0198/0210 or buffered static files. No foreign-callback,
interface or vtable feature is authorized by the first server implementation.
The remaining design below is a future proposal, not current language semantics.

## Demand rules

- `Http.serve` selects the default adapter; `Http.serve_with` selects the
  explicitly named adapter.
- The selected adapter and its declared native dependencies are linked
  statically; no runtime discovery or plugin loading occurs.
- Selecting a custom adapter does not select the default HTTP backend or its
  dependencies. A custom adapter selects the Task-aware socket runtime only
  when it declares that dependency; it may instead declare its own transport.
- Programs that do not serve HTTP select no backend adapter.

## Required sweep

- define and implement the static backend adapter ABI without adding a
  language-wide interface feature;
- adapt the default server and at least one statically linked C backend to the
  same public RFC 0210 types;
- define callback, Task, buffer, cancellation, shutdown, and Error ownership;
- select and link only the explicitly requested backend and its dependencies;
- update the workbench snippet and generated-C manifest if the public sample
  or generated output changes;
- synchronize `docs/reference.md` with the stabilized public behavior before
  this RFC is closed.

## Validation

This section is exhaustive for the backend substitution contract:

- the default `Http.serve` path selects and links the default backend;
- `Http.serve_with` selects the explicitly named static adapter and does not
  discover, load, or negotiate a backend at runtime;
- default and custom adapters expose the same Request, Response, Router,
  ServerConfig, handler-error, and body-stream behavior;
- the adapter translates C callbacks to Task wakeups without retaining a
  callback pointer or Hexal buffer past its documented lifetime;
- cancellation, shutdown, and C-side completion cannot resume a Task or free
  request storage twice;
- backend failures map to Error without exposing internal details in an HTTP
  response;
- shared handler-Error to HTTP 500 translation belongs to the common HTTP policy;
  both adapters honor response-commit behavior without reinterpreting it;
- imported C completion callbacks wake through the existing runtime bridge only;
  no Hexal handler is invoked directly on a foreign event-loop thread;
- ordinary HTTP handlers compile without a backend-specific interface or
  request/response type;
- existing Task, Channel, IO, and TCP behavior is unchanged;
- ordinary gates, focused C23 fixtures and short C23 pass; exhaustive C23 runs
  require separate user consent.

## Implementation plan (deferred; not authorized)

### Phase 0: explicit reactivation gate

Do not execute this plan with the first server arc. Reactivation requires a user
request, a working/measured default backend and a named second backend with a
concrete advantage. Record its pin/license, target/build dependencies and the
behavior it must share. No serve_with, foreign callback or interface feature is
implicitly authorized by the current default-server implementation.

### Phase 1: design and approve the ABI

Replace obsolete Response-returning assumptions with current writer/context
contracts. Specify source selection shape, C records/functions, ownership of
transport/parser/routing/serialization, callback attachment, buffers and shutdown.
Map each contract to this proposal's Validation before implementation; reuse the
qualified arm/commit/wake and native-quiescence rules, not a second wait protocol.
Exit: approved exact ABI and named default/second adapters; no unresolved callback
feature is left for an implementer to infer.

### Phase 2: implement both adapters and dependency isolation

Adapt the default path and pinned second backend without backend-specific public
Request/Writer/context types. Implement one common error/commit policy and strict
native lifetime rules. Select only the chosen adapter/dependencies at compile time.
Exit: all reactivated Validation behavior passes on both paths, including custom-
only exclusion of default dependencies and completion/shutdown cleanup races.

### Phase 3: measure and qualify before closure

Compare dispatch, allocations, throughput and latency against the direct baseline
on matched workloads. Record measured overhead; static selection alone does not
prove no indirect streaming calls. Run ordinary and focused/short C23 gates,
review manifest/dependencies and sync reference only for the approved new surface.
Closure still requires the entire reactivated Validation contract and a rebuilt
hexal/restarted play handoff; exhaustive C23 remains consent-only.

## Open questions

1. The exact static adapter shape accepted by `Http.serve_with`: imported C
   functions, a generated function table, or a Hexal value with a fixed set of
   C-callable functions. Recommend the smallest shape that existing C
   interoperability supports without adding language-wide interface
   machinery.
2. The C ABI for body streaming, callback lifetime, cancellation, shutdown,
   and Error translation.
3. How a custom adapter consumes the common Router and handler without
   requiring backend-specific HTTP types.

## Reference synchronization

Implementation updates `docs/reference.md` after backend behavior stabilizes
and before this RFC is marked implemented or closed.
