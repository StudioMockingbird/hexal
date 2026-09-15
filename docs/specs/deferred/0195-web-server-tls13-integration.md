# RFC 0195: Web Server — TLS 1.3 Integration

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on RFC 0144 (network runtime),
  RFC 0210 (web server syntax), and RFC 0194 (web server lowering) landing
  first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax and semantics), RFC 0194 (web server
  lowering), and the implemented RFCs 0145 (libuv runtime), 0146 (mimalloc),
  0168 (libuv capability arc), and 0184 (atomic print)
- Coordinates with: RFC 0194 for the shared socket layer and libuv handle
  lifecycle, and RFC 0039 (C interop) for the underlying TLS library binding
- Does not add: TLS 1.2, DTLS, certificate generation, ACME/Let's Encrypt
  automation, or client certificate authentication

## Motivation

TLS is mandatory for production web servers. Without it, Hexal servers cannot
serve HTTPS, and every deployment must terminate TLS at a reverse proxy. This
RFC specifies how TLS 1.3 integrates with the existing TCP socket layer and
HTTP server.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| TLS 1.3 handshake | Pick up | Minimum viable TLS |
| TLS 1.3 data transfer | Pick up | Encrypted communication |
| Server-side TLS | Pick up | HTTPS server |
| Client-side TLS | Skip in v1 | Client-side HTTP is out of scope |
| TLS 1.2 | Skip in v1 | Deprecated; TLS 1.3 is sufficient |
| DTLS | Skip in v1 | Different protocol, own spec |
| Certificate loading | Pick up | Required for server |
| Private key loading | Pick up | Required for server |
| SNI (Server Name Indication) | Pick up | Required for virtual hosting |
| ALPN (Application-Layer Protocol Negotiation) | Pick up | Required for HTTP/2 later |
| Client certificate verification | Skip in v1 | Mutual TLS is a separate concern |
| Certificate chain validation | Pick up | Required for server |
| OCSP stapling | Skip in v1 | Complexity disproportionate to initial surface |
| Certificate pinning | Skip in v1 | Library concern |
| ACME/Let's Encrypt | Skip in v1 | Automation concern, not TLS library |
| Session resumption | Skip in v1 | Optimization, measure first |
| 0-RTT early data | Skip in v1 | Replay risk, measure first |
| Post-quantum key exchange | Skip in v1 | Not yet standardized for TLS |

## TLS library

The TLS implementation uses **BearSSL** (MIT licensed, ~60 KiB, written in C,
no external dependencies, designed for embedded systems and high performance).
BearSSL supports TLS 1.3, has a clean C API, and does not depend on OpenSSL,
BoringSSL, or any system TLS library.

Alternative candidates considered:

| Library | License | Size | TLS 1.3 | Notes |
| --- | --- | --- | --- | --- |
| BearSSL | MIT | ~60 KiB | Yes | Clean C API, no dependencies |
| mbedTLS | Apache 2.0 | ~200 KiB | Yes | Heavier, more features |
| OpenSSL | Apache 2.0 | ~3 MB | Yes | Massive dependency, complex API |
| BoringSSL | BSD | ~2 MB | Yes | Google's fork, not a stable API |
| wolfSSL | GPL | ~200 KiB | Yes | Commercial license for non-GPL |

BearSSL is selected for its small size, MIT license, clean API, and TLS 1.3
support. It is vendored as a git submodule at `modules/bearssl/` following the
same model as mimalloc (RFC 0146) and libuv (RFC 0145).

## Source surface

### TLS context

A TLS context holds the server certificate and private key:

```text
type TlsContext is struct
    -- internal representation
end

fun TlsContext.new(
    certificate: String,
    private_key: String,
) -> TlsContext | Error

fun TlsContext.new_from_files(
    certificate_path: String,
    private_key_path: String,
) -> TlsContext | Error
```

- `certificate` is PEM-encoded.
- `private_key` is PEM-encoded.
- Loading validates the certificate chain and key immediately.
- Invalid certificates or keys return `ErrorKind.InvalidInput()` with
  message `invalid TLS certificate` or `invalid TLS private key`.

### TLS listener

A TLS listener wraps a TCP listener:

```text
type TlsListener is struct
    -- internal representation
end

fun TlsListener.new(
    listener: TcpListener,
    context: TlsContext,
) -> TlsListener

method TlsListener.serve(handler: Fun<(TlsConnection) : Nil>) -> Nil | Error
method TlsListener.shutdown() -> Nil
method TlsListener.set_timeout(timeout: Duration)
```

### TLS connection

A TLS connection wraps a TCP connection:

```text
type TlsConnection is struct
    -- internal representation
end

method TlsConnection.read_request() -> Request | Nil | Error
method TlsConnection.send(response: Response) -> Nil | Error
method TlsConnection.close() -> Nil
method TlsConnection.peer_certificate() -> String | Nil
method TlsConnection.negotiated_protocol() -> String
```

- `read_request` reads and parses the HTTP request through the TLS
  decryption layer.
- `send` writes the HTTP response through the TLS encryption layer.
- `peer_certificate` returns the client certificate when client
  verification is enabled (not in v1).
- `negotiated_protocol` returns the ALPN-selected protocol (e.g., "http/1.1"
  or "h2" when HTTP/2 is added later).

### Usage

```hexal
import
    Net from "std/net"
end

context := try Net.TlsContext.new_from_files(
    "cert.pem",
    "key.pem"
)
listener := try Net.listen(
    Net.Address.parse("0.0.0.0", 443),
    128
)
tls_listener := Net.TlsListener.new(listener, context)
tls_listener.serve(handler)
```

## TLS 1.3 handshake

The TLS handshake is performed during the first `read_request` or `send` on
a `TlsConnection`. The handshake is asynchronous and parks the Task:

1. The client sends `ClientHello` with supported cipher suites and ALPN
   extensions.
2. The server selects a cipher suite and ALPN protocol.
3. The server sends `ServerHello`, encrypted extensions, certificate, and
   `CertificateVerify`.
4. The client sends `Finished`.
5. The server sends `Finished` and the handshake is complete.

BearSSL handles the entire handshake; the generated C calls
`bearssl_ssl_init`, `bearssl_ssl_set_buffer`, and `bearssl_ssl_set_io`
before the first I/O, then feeds data through `bearssl_ssl_recv` and
`bearsl_ssl_send`.

## Cipher suites

TLS 1.3 cipher suites are fixed by the standard and cannot be configured
in v1:

| Suite | ID |
| --- | --- |
| TLS_AES_256_GCM_SHA384 | 0x13,0x02 |
| TLS_CHACHA20_POLY1305_SHA256 | 0x13,0x03 |
| TLS_AES_128_GCM_SHA256 | 0x13,0x01 |

The server selects the first suite supported by both client and server.
No cipher suite configuration is exposed in v1.

## Certificate handling

- PEM-encoded certificates and private keys are supported.
- DER-encoded certificates and keys are not supported in v1.
- Certificate chain validation follows BearSSL's default trust anchor
  store; custom trust anchors are not exposed in v1.
- Self-signed certificates are accepted for development (no validation).
- Expired certificates are rejected.
- Hostname verification is performed for client connections (not in v1
  since client TLS is out of scope).

## libuv integration

TLS I/O uses the same libuv event loop as plain TCP:

### TLS read

```c
static void on_tls_read(uv_stream_t *stream, ssize_t nread,
                         const uv_buf_t *buf) {
    if (nread > 0) {
        /* decrypt through BearSSL */
        bearssl_ssl_recv(&ctx->ssl, buf->base, nread);
        /* feed to HTTP parser */
    }
}
```

### TLS write

```c
int hex_tls_write(bearssl_ssl_context *ctx,
                   const char *data, size_t len) {
    /* encrypt through BearSSL */
    /* write encrypted data through uv_write */
}
```

- BearSSL's I/O callbacks use the existing libuv `uv_read_start` /
  `uv_write` pattern.
- The TLS layer is transparent to the HTTP parser; decrypted bytes are
  fed directly to `hex_http_parser_execute`.
- Encrypted writes go through `uv_write` with the encrypted output from
  BearSSL.

### Shutdown

- TLS shutdown sends `close_notify` through BearSSL before closing the
  TCP connection.
- A hard timeout skips `close_notify` and closes the TCP handle directly.

## Memory model

- BearSSL context is per-connection, allocated on the connection's Stash
  or Heap.
- BearSSL I/O buffers are stack-resident or connection-scoped.
- Certificate and key data are loaded once and shared across connections
  (read-only).
- No persistent TLS state beyond the `TlsContext`.

## Demand rules

- `TlsContext`, `TlsListener`, and `TlsConnection` select the TLS
  component, which selects BearSSL and native bootstrap.
- The TLS component additionally selects the event bridge when used from
  a Task.
- A program using only plain TCP does not select the TLS component or
  BearSSL.
- BearSSL is always compiled with `-O2` regardless of build mode (same
  as libuv and mimalloc).

## Vendoring

BearSSL is vendored as a git submodule at `modules/bearssl/`:

```
modules/bearssl/           # git submodule, pinned version
modules/BEARSSL.md         # version, commit, license, verification
modules/embed.go           # go:embed for bearssl source
modules/MANIFEST.sha256    # extended with bearssl artifacts
```

The vendoring model follows RFC 0146 (mimalloc) exactly:
- BearSSL source is embedded in the compiler binary at Go build time.
- The driver materializes the source from embedded bytes.
- The generated C includes `#include <bearssl.h>` from the materialized
  path.
- A SHA-256 manifest verifies integrity.

## Performance considerations

- BearSSL is significantly faster than OpenSSL for TLS 1.3 handshakes
  and data transfer on small messages.
- Session resumption (not in v1) would reduce handshake cost from ~2 RTT
  to ~1 RTT.
- 0-RTT early data (not in v1) would allow sending application data in
  the first ClientHello.
- The cipher suite selection is hardware-dependent: AES-GCM is faster on
  CPUs with AES-NI, while ChaCha20-Poly1305 is faster on CPUs without it.
  BearSSL selects automatically.

## Error handling

| Condition | ErrorKind | Message |
| --- | --- | --- |
| Invalid certificate | `InvalidInput` | `invalid TLS certificate` |
| Invalid private key | `InvalidInput` | `invalid TLS private key` |
| Certificate chain validation failed | `SecurityError` | `certificate chain validation failed` |
| Handshake failed | `SecurityError` | `TLS handshake failed` |
| Decryption failed | `SecurityError` | `TLS decryption failed` |
| Connection reset during handshake | `ConnectionReset` | `connection reset during TLS handshake` |
| Handshake timeout | `Timeout` | `TLS handshake timed out` |

## Non-goals

- TLS 1.2 and earlier versions.
- DTLS (Datagram TLS).
- Client-side TLS (client certificates, mTLS).
- Certificate generation or management.
- ACME/Let's Encrypt automation.
- OCSP stapling.
- Certificate pinning.
- Session resumption or 0-RTT.
- Post-quantum key exchange.
- Hardware security module (HSM) integration.
- FIPS 140 compliance.
- TLS configuration beyond certificate and key loading.

## Required sweep

- BearSSL submodule, embedding, and manifest verification;
- TLS context loading and validation in `hexal/tls.c`;
- TLS listener and connection wrapping in `hexal/tls.c`;
- TLS handshake integration with libuv event loop;
- TLS read/write through BearSSL I/O callbacks;
- TLS shutdown and `close_notify` handling;
- demand discovery for TLS component;
- error mapping for TLS-specific failures;
- workbench snippet and manifest entries;
- `docs/reference.md` TLS surface after explicit approval.

## Validation

This section is exhaustive:

- `TlsContext.new` accepts valid PEM certificate and private key;
- `TlsContext.new` rejects invalid certificate with exact error;
- `TlsContext.new` rejects invalid private key with exact error;
- `TlsContext.new_from_files` loads certificate and key from files;
- `TlsContext.new_from_files` rejects missing or unreadable files;
- TLS handshake completes successfully with a valid certificate;
- TLS handshake fails with an expired certificate;
- TLS handshake fails with a self-signed certificate (when validation
  is enabled);
- TLS handshake fails with an invalid certificate chain;
- TLS data transfer encrypts and decrypts correctly;
- TLS read returns decrypted HTTP request data;
- TLS write encrypts HTTP response data;
- TLS shutdown sends `close_notify` before closing TCP connection;
- Hard timeout skips `close_notify` and closes TCP directly;
- TLS component is selected only when TLS types are reachable;
- TLS component is not selected for plain TCP programs;
- BearSSL is always compiled with `-O2`;
- existing TCP, IO, Task, Channel, and HTTP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to expose ALPN configuration (for future HTTP/2) or lock it
   to "http/1.1" in v1.
2. Whether `TlsContext` should support multiple certificate/key pairs
   (for SNI with multiple domains).
3. Whether to expose custom trust anchor loading or keep BearSSL's
   default trust store.
4. Whether to support DER-encoded certificates in addition to PEM.
5. Whether `TlsConnection` should expose the negotiated cipher suite
   and protocol version for debugging.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the TLS types, handshake, and encryption contracts only after
behavior stabilizes and with explicit user approval.
