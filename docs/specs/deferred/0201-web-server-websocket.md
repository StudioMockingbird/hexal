# RFC 0201: Web Server — WebSocket

- Kind: Feature Specification (Rust-Style RFC)
- Status: Deferred; not scheduled. Depends on the web server syntax (RFC 0210)
  and lowering (RFC 0194) landing first
- Created: 2026-09-15
- Updated: 2026-09-15
- Depends on: RFC 0210 (web server syntax), RFC 0194 (web server lowering),
  RFC 0198 (HTTP parsing), and the implemented RFCs 0145 (libuv runtime),
  0146 (mimalloc), and 0168 (libuv capability arc)
- Coordinates with: RFC 0195 (TLS) for encrypted WebSocket connections, and
  RFC 0186 (stdlib boundary) for module placement
- Does not add: gRPC, Socket.IO, or any protocol over WebSocket

## Motivation

WebSocket is the standard protocol for real-time bidirectional
communication: chat, live updates, notifications, multiplayer games, and
collaborative editing. Without native WebSocket support, every Hexal
server must use long polling or Server-Sent Events, both of which are
inferior to WebSocket for bidirectional use cases.

## Scope decision

| Capability | Disposition | Rationale |
| --- | --- | --- |
| WebSocket upgrade handshake | Pick up | Foundation |
| Text frames | Pick up | Required for JSON APIs |
| Binary frames | Pick up | Required for binary protocols |
| Ping/pong (keep-alive) | Pick up | Required for connection health |
| Close handshake | Pick up | Graceful shutdown |
| Per-message compression (permessage-deflate) | Skip in v1 | Complexity disproportionate to initial surface |
| WebSocket over TLS (wss://) | Pick up | Required for production |
| Multiple subprotocols | Pick up | Required for protocol negotiation |
| Extensions negotiation | Skip in v1 | Only permessage-deflate is common |
| Batched messages | Pick up | Required for performance |
| Connection multiplexing | Skip in v1 | HTTP/2 WebSocket is a separate concern |

## Source surface

### WebSocket server

```hexal
import
    Net from "std/net"
end

handler := fun (socket: Net.WebSocket) -> Nil {
    match socket.receive() {
        Net.Message.Text(text) => {
            socket.send(text)
        }
        Net.Message.Binary(data) => {
            socket.send(data)
        }
        Net.Message.Close => {
            socket.close()
        }
    }
}

server := try Net.WebSocketServer.new(
    host = "0.0.0.0",
    port = 8080,
    path = "/ws",
    subprotocols = ["chat", "binary"],
    max_frame_size = 64 * 1024,
)
server.serve(handler)
```

### WebSocket type

```text
type WebSocket is struct
    -- internal representation
end

method WebSocket.receive() -> Message | Nil | Error
method WebSocket.send(message: Message) -> Nil | Error
method WebSocket.close() -> Nil | Error
method WebSocket.close_with(code: UInt16, reason: String) -> Nil | Error
method WebSocket.ping() -> Nil | Error
method WebSocket.is_open() -> Bool
method WebSocket.remote_address() -> String
method WebSocket.subprotocol() -> String | Nil
```

### Message type

```text
type Message is
    Text(String) |
    Binary(List<Byte>) |
    Ping |
    Pong |
    Close
end
```

### WebSocket server

```text
type WebSocketServer is struct
    -- internal representation
end

fun WebSocketServer.new(
    host: String,
    port: UInt16,
    path: String,
    subprotocols: Slice<String>,
    max_frame_size: Size,
) -> WebSocketServer | Error

method WebSocketServer.serve(handler: Fun<(WebSocket) : Nil>) -> Nil | Error
method WebSocketServer.shutdown() -> Nil
method WebSocketServer.set_timeout(timeout: Duration)
```

### Client (for testing)

```text
type WebSocketClient is struct
    -- internal representation
end

fun WebSocketClient.connect(
    url: String,
    subprotocol: String | Nil,
) -> WebSocketClient | Error

method WebSocketClient.send(message: Message) -> Nil | Error
method WebSocketClient.receive() -> Message | Nil | Error
method WebSocketClient.close() -> Nil | Error
```

## WebSocket protocol

### Upgrade handshake

1. Client sends HTTP request:
   ```
   GET /ws HTTP/1.1
   Upgrade: websocket
   Connection: Upgrade
   Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==
   Sec-WebSocket-Version: 13
   Sec-WebSocket-Protocol: chat, binary
   ```

2. Server responds:
   ```
   HTTP/1.1 101 Switching Protocols
   Upgrade: websocket
   Connection: Upgrade
   Sec-WebSocket-Accept: s3pPLMBiTxaQ9kYGzzhZRbK+xOo=
   Sec-WebSocket-Protocol: chat
   ```

3. Connection is now a WebSocket; HTTP framing ends.

### Frame format

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-------+-+-------------+-------------------------------+
|F|R|R|R| opcode|M| Payload len |    Extended payload length    |
|I|S|S|S|  (4)  |A|     (7)     |             (16/64)           |
|N|V|V|V|       |S|             |   (if payload len==126/127)   |
| |1|2|3|       |K|             |                               |
+-+-+-+-+-------+-+-------------+ - - - - - - - - - - - - - - - +
```

| Opcode | Meaning |
| --- | --- |
| 0x0 | Continuation |
| 0x1 | Text |
| 0x2 | Binary |
| 0x8 | Close |
| 0x9 | Ping |
| 0xA | Pong |

### Close handshake

1. One side sends a Close frame with status code and reason.
2. The other side sends a Close frame in response.
3. The TCP connection is closed.

Close codes:

| Code | Meaning |
| --- | --- |
| 1000 | Normal closure |
| 1001 | Going away |
| 1002 | Protocol error |
| 1003 | Unsupported data |
| 1006 | Abnormal closure (no frame) |
| 1007 | Invalid frame payload data |
| 1008 | Policy violation |
| 1009 | Message too big |
| 1010 | Mandatory extension |
| 1011 | Internal server error |

### Ping/pong

- The server sends Ping frames periodically.
- The client must respond with Pong.
- If no Pong is received within the timeout, the connection is closed.

## libuv integration

### Upgrade

```c
void on_http_upgrade(uv_stream_t *stream, int status) {
    /* verify Sec-WebSocket-Key header */
    /* compute Sec-WebSocket-Accept */
    /* send 101 response */
    /* switch to WebSocket frame reading */
    uv_read_start(stream, alloc_buffer, on_ws_read);
}
```

### Frame reading

```c
void on_ws_read(uv_stream_t *stream, ssize_t nread,
                const uv_buf_t *buf) {
    /* parse frame header */
    /* read payload based on length */
    /* unmask payload if masked */
    /* deliver to handler */
}
```

### Frame writing

```c
int hex_ws_send(uv_stream_t *stream, int opcode,
                const char *data, size_t len) {
    /* construct frame header */
    /* write frame through uv_write */
}
```

### Ping/pong timer

```c
void on_ping_timer(uv_timer_t *handle) {
    /* send ping frame to all connected clients */
    /* start pong timeout timer */
}

void on_pong_timeout(uv_timer_t *handle) {
    /* close connection if no pong received */
}
```

## Demand rules

- `WebSocket`, `WebSocketServer`, `WebSocketClient`, `Message`,
  `Ping`, `Pong`, and `Close` select the WebSocket component.
- The WebSocket component selects HTTP parsing (RFC 0198), libuv,
  native bootstrap, and the event bridge.
- A program that does not use WebSocket types does not select the
  WebSocket component.

## Required sweep

- WebSocket upgrade handshake in `hexal/websocket.c` and `hexal/websocket.h`;
- frame parsing and serialization;
- text and binary frames;
- ping/pong handling;
- close handshake with status codes;
- WebSocket server with path and subprotocol matching;
- WebSocket client for testing;
- demand discovery for WebSocket component;
- workbench snippet and manifest entries;
- `docs/reference.md` WebSocket surface after explicit approval.

## Validation

This section is exhaustive:

- upgrade handshake sends correct `Sec-WebSocket-Accept` header;
- upgrade handshake selects the correct subprotocol;
- text frame round-trips correctly;
- binary frame round-trips correctly;
- ping frame receives pong response;
- close frame initiates graceful shutdown;
- close with status code sends correct code and reason;
- oversized frame returns error;
- invalid frame opcode returns error;
- invalid frame masking returns error;
- connection timeout closes idle connections;
- WebSocket server serves on the correct path;
- WebSocket server rejects non-WebSocket upgrades;
- WebSocket client connects and exchanges messages;
- existing Task, Channel, IO, and TCP behavior unchanged;
- ordinary and tagged C23 suites pass.

## Open questions

1. Whether to support permessage-deflate compression.
2. Whether to expose raw frame control for custom protocols.
3. Whether to add broadcast functionality for pub/sub patterns.
4. Whether to support WebSocket over HTTP/2 (RFC 8441).
5. Whether to add a `WebSocketPool` for connection management.

## Reference synchronization

Do not edit `docs/reference.md` from this draft. Approved implementation
adds the WebSocket types and contracts only after behavior stabilizes and
with explicit user approval.
