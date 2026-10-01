package config

import "time"

// The std/http ServerConfig defaults. Http.default_config copies every value
// into an ordinary record the user may replace field by field before listen,
// so these are starting points, not enforced constants. They are conservative
// resource ceilings chosen to bound memory per connection and per request
// under hostile clients; none is a measured optimum, and none is derived from
// another server's defaults.
const (
	// HTTPMaxRequestLineBytes bounds method plus request-target bytes.
	HTTPMaxRequestLineBytes = 8 << 10
	// HTTPMaxHeaderBytes bounds the field-name and field-value bytes of one
	// request head.
	HTTPMaxHeaderBytes = 32 << 10
	// HTTPMaxHeaderCount bounds the header fields of one request head and
	// the trailer fields of one chunked body.
	HTTPMaxHeaderCount = 100
	// HTTPMaxBodyBytes bounds decoded request-body bytes.
	HTTPMaxBodyBytes = 8 << 20
	// HTTPMaxTrailerBytes bounds trailer bytes and chunk-extension bytes, each
	// counted per message.
	HTTPMaxTrailerBytes = 8 << 10
	// HTTPReceiveBufferBytes is the per-connection receive buffer capacity.
	// It is a read granularity, never a protocol limit.
	HTTPReceiveBufferBytes = 32 << 10
	// HTTPWriteBufferBytes is the per-connection response output buffer.
	HTTPWriteBufferBytes = 64 << 10
	// HTTPMaxConnections is the active-connection ceiling; accepting pauses at
	// it and the kernel backlog stays bounded.
	HTTPMaxConnections = 4096
	// HTTPBacklog is the listener backlog handed to listen.
	HTTPBacklog = 512
)

// The std/http phase deadlines. Header and body deadlines bound the whole
// phase from its first byte of interest, not each read; the write deadline
// bounds each response from first commitment; the idle deadline applies
// between requests; the shutdown deadline bounds the stop grace period.
const (
	HTTPHeaderTimeout   = 5 * time.Second
	HTTPBodyTimeout     = 30 * time.Second
	HTTPWriteTimeout    = 30 * time.Second
	HTTPIdleTimeout     = 60 * time.Second
	HTTPShutdownTimeout = 30 * time.Second
)

// HTTPTCPNoDelay is the default TCP_NODELAY setting of accepted connections.
const HTTPTCPNoDelay = true
