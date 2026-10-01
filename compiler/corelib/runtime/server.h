#ifndef HEXAL_SERVER_H
#define HEXAL_SERVER_H

#include "hexal.h"
#include "hexal/error.h"
#include "hexal/heap.h"
#include "hexal/slice.h"
#include "hexal/string.h"
#include "hexal/time.h"
// std/http's declarations. Request and Writer are opaque handles over one
// exchange record owned by the connection Task; the router and the server are
// opaque handles over private state in hexal/server.c. The Header record lives
// beside its Slice in hexal/slice.h, and no native parser type appears in any
// public header.

typedef struct hex_http_exchange *hex_http_request;
typedef struct hex_http_exchange *hex_http_writer;
typedef struct hex_http_router_state *hex_http_router;
typedef struct hex_http_server_state *hex_http_server;

// A handler is stored behind one erased function pointer, and an invoke thunk
// the application's module builds restores the typed signature and reports
// whether the handler returned nil, so the runtime never names the
// application's context type or its result union.
typedef void (*hex_http_handler)(void);
typedef bool (*hex_http_invoke)(hex_http_handler handler, const void *app, hex_http_request request, hex_http_writer writer);

// hex_list_UInt8 is used here only by pointer, as Request.read's destination;
// see network.h for why this stays a forward declaration.
typedef struct hex_list_UInt8 hex_list_UInt8;

// ServerConfig is an ordinary record of replaceable fields. host is a borrowed
// String; listen copies it once.
typedef struct hex_t_ServerConfig {
    const hex_string *hex_m_host;
    uint16_t hex_m_port;
    size_t hex_m_max_request_line_bytes;
    size_t hex_m_max_header_bytes;
    size_t hex_m_max_header_count;
    size_t hex_m_max_body_bytes;
    size_t hex_m_max_trailer_bytes;
    size_t hex_m_receive_buffer_bytes;
    size_t hex_m_write_buffer_bytes;
    size_t hex_m_max_connections;
    size_t hex_m_backlog;
    hex_duration hex_m_header_timeout;
    hex_duration hex_m_body_timeout;
    hex_duration hex_m_write_timeout;
    hex_duration hex_m_idle_timeout;
    hex_duration hex_m_shutdown_timeout;
    bool hex_m_tcp_nodelay;
} hex_t_ServerConfig;

{{if .Calls}}
typedef struct hex_http_status_result {
    bool ok;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_http_status_result;

{{if .NeedConfig}}extern hex_t_ServerConfig hex_http_default_config_raw(const hex_string *host, uint16_t port);
{{end}}{{if .NeedRouter}}extern hex_http_router hex_http_router_new_raw(hex_heap h);
{{end}}{{if .NeedRoute}}extern hex_http_status_result hex_http_router_route_raw(hex_http_router router, const hex_string *method, const hex_string *path, hex_http_handler handler, hex_http_invoke invoke);
{{end}}{{if .NeedFree}}extern void hex_http_router_free_raw(hex_http_router router, hex_heap h);
{{end}}{{if .NeedRuntime}}
typedef struct hex_http_server_result {
    bool ok;
    hex_http_server server;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_http_server_result;

typedef struct hex_http_read_result {
    bool ok;
    bool end_of_stream;
    size_t count;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_http_read_result;

typedef struct hex_http_bytes_result {
    bool found;
    hex_slice_UInt8 bytes;
} hex_http_bytes_result;

extern hex_http_server_result hex_http_listen_raw(hex_heap h, hex_t_ServerConfig config, hex_http_router router, const void *app);
extern hex_http_status_result hex_http_server_run_raw(hex_http_server server);
extern hex_http_status_result hex_http_server_wait_raw(hex_http_server server);
extern void hex_http_server_stop_raw(hex_http_server server);
extern void hex_http_server_free_raw(hex_http_server server, hex_heap h);
extern hex_slice_UInt8 hex_http_request_method_raw(hex_http_request request);
extern hex_slice_UInt8 hex_http_request_target_raw(hex_http_request request);
extern hex_slice_UInt8 hex_http_request_path_raw(hex_http_request request);
extern hex_slice_Header hex_http_request_headers_raw(hex_http_request request);
extern hex_http_bytes_result hex_http_request_header_raw(hex_http_request request, const hex_string *text);
extern hex_http_read_result hex_http_request_read_raw(hex_http_request request, hex_list_UInt8 *into, size_t count);
extern hex_http_status_result hex_http_writer_status_raw(hex_http_writer writer, uint16_t port);
extern hex_http_status_result hex_http_writer_header_raw(hex_http_writer writer, const hex_string *text, hex_slice_UInt8 bytes);
extern hex_http_status_result hex_http_writer_content_length_raw(hex_http_writer writer, size_t count);
extern hex_http_status_result hex_http_writer_write_raw(hex_http_writer writer, hex_slice_UInt8 bytes);
{{end}}{{end}}
#endif
