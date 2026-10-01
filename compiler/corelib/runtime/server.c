#include "server.h"
{{if .NeedParser}}#include "hexal/http.h"
{{end}}{{if .NeedRuntime}}#include "hexal/concurrency.h"
#include "hexal/handle.h"
#include "hexal/list.h"
#include "hexal/network.h"
#include <limits.h>
#include <time.h>
{{end}}#include <stdatomic.h>
#include <stdckdint.h>
#include <string.h>

// Failure texts are static headers: no allocation, nothing to free, and the
// adapters copy the bytes into the Error they build.
#define HEX_HTTP_MESSAGE(identifier, text) \
    static const hex_string identifier = { .data = (const uint8_t *)(text), .byte_length = sizeof(text) - 1 }

static inline hex_t_ErrorKind hex_http_kind_invalid_input(void) {
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_InvalidInput };
}

static inline hex_t_ErrorKind hex_http_kind_resource(void) {
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_ResourceExhausted };
}

static inline hex_http_status_result hex_http_status_failure(hex_t_ErrorKind kind, const hex_string *message) {
    return (hex_http_status_result){ .ok = false, .kind = kind, .message = message };
}

{{if .NeedConfig}}
// The defaults are the compiler configuration's values rendered once; this is
// the only place a ServerConfig is filled in.
hex_t_ServerConfig hex_http_default_config_raw(const hex_string *host, uint16_t port) {
    return (hex_t_ServerConfig){
        .hex_m_host = host,
        .hex_m_port = port,
        .hex_m_max_request_line_bytes = {{.MaxRequestLineBytes}},
        .hex_m_max_header_bytes = {{.MaxHeaderBytes}},
        .hex_m_max_header_count = {{.MaxHeaderCount}},
        .hex_m_max_body_bytes = {{.MaxBodyBytes}},
        .hex_m_max_trailer_bytes = {{.MaxTrailerBytes}},
        .hex_m_receive_buffer_bytes = {{.ReceiveBufferBytes}},
        .hex_m_write_buffer_bytes = {{.WriteBufferBytes}},
        .hex_m_max_connections = {{.MaxConnections}},
        .hex_m_backlog = {{.Backlog}},
        .hex_m_header_timeout = {{.HeaderTimeout}}ULL,
        .hex_m_body_timeout = {{.BodyTimeout}}ULL,
        .hex_m_write_timeout = {{.WriteTimeout}}ULL,
        .hex_m_idle_timeout = {{.IdleTimeout}}ULL,
        .hex_m_shutdown_timeout = {{.ShutdownTimeout}}ULL,
        .hex_m_tcp_nodelay = {{if .TCPNoDelay}}true{{else}}false{{end}},
    };
}
{{end}}
{{if .NeedRouter}}
// A route owns copies of its method and path bytes, so registration never
// depends on caller string storage. attached counts the servers listening over
// the router: registration and free are refused while it is nonzero.
typedef struct hex_http_route {
    uint8_t *method;
    size_t method_length;
    uint8_t *path;
    size_t path_length;
    hex_http_handler handler;
    hex_http_invoke invoke;
} hex_http_route;

struct hex_http_router_state {
    hex_http_route *routes;
    size_t count;
    size_t capacity;
    atomic_size_t attached;
};

hex_http_router hex_http_router_new_raw(hex_heap h) {
    (void)h;
    return hex_heap_allocate_zeroed(1, sizeof(struct hex_http_router_state));
}
{{end}}
{{if .NeedRoute}}
HEX_HTTP_MESSAGE(hex_http_message_router_attached, "router is attached to a running server");
HEX_HTTP_MESSAGE(hex_http_message_unknown_method, "unknown HTTP method");
HEX_HTTP_MESSAGE(hex_http_message_bad_path, "route path must be an absolute path without query, fragment, or control bytes");
HEX_HTTP_MESSAGE(hex_http_message_duplicate_route, "duplicate method and path route");
HEX_HTTP_MESSAGE(hex_http_message_route_memory, "route table ran out of memory");

// A route path is an absolute raw path: the request target's path component,
// so it carries no query, fragment, space, or control byte.
static bool hex_http_path_valid(const hex_string *path) {
    if (path->byte_length == 0 || path->data[0] != '/') {
        return false;
    }
    for (size_t index = 0; index < path->byte_length; index++) {
        uint8_t byte = path->data[index];
        if (byte <= 0x20 || byte == 0x7f || byte == '?' || byte == '#') {
            return false;
        }
    }
    return true;
}

static bool hex_http_route_exists(const struct hex_http_router_state *router, const hex_string *method, const hex_string *path) {
    for (size_t index = 0; index < router->count; index++) {
        const hex_http_route *route = &router->routes[index];
        if (route->method_length == method->byte_length && route->path_length == path->byte_length &&
            memcmp(route->method, method->data, method->byte_length) == 0 &&
            memcmp(route->path, path->data, path->byte_length) == 0) {
            return true;
        }
    }
    return false;
}

static uint8_t *hex_http_copy_bytes(const hex_string *text) {
    uint8_t *copy = hex_heap_allocate_or_null(text->byte_length);
    if (copy != NULL) {
        memcpy(copy, text->data, text->byte_length);
    }
    return copy;
}

// Growth doubles; the multiplication is checked so a pathological count fails
// as exhaustion instead of wrapping.
static bool hex_http_reserve_route(struct hex_http_router_state *router) {
    if (router->count < router->capacity) {
        return true;
    }
    size_t capacity = router->capacity == 0 ? 8 : router->capacity;
    size_t bytes = 0;
    if (router->capacity != 0 && ckd_mul(&capacity, capacity, (size_t)2)) {
        return false;
    }
    if (ckd_mul(&bytes, capacity, sizeof(hex_http_route))) {
        return false;
    }
    hex_http_route *grown = hex_heap_allocate_or_null(bytes);
    if (grown == NULL) {
        return false;
    }
    if (router->count != 0) {
        memcpy(grown, router->routes, router->count * sizeof(hex_http_route));
    }
    if (router->routes != NULL) {
        hex_heap_free(router->routes);
    }
    router->routes = grown;
    router->capacity = capacity;
    return true;
}

hex_http_status_result hex_http_router_route_raw(hex_http_router router, const hex_string *method, const hex_string *path, hex_http_handler handler, hex_http_invoke invoke) {
    if (atomic_load(&router->attached) != 0) {
        return hex_http_status_failure((hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_Busy }, &hex_http_message_router_attached);
    }
    if (!hex_http_method_known(method->data, method->byte_length)) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_unknown_method);
    }
    if (!hex_http_path_valid(path)) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_bad_path);
    }
    if (hex_http_route_exists(router, method, path)) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_duplicate_route);
    }
    if (!hex_http_reserve_route(router)) {
        return hex_http_status_failure(hex_http_kind_resource(), &hex_http_message_route_memory);
    }
    uint8_t *method_copy = hex_http_copy_bytes(method);
    uint8_t *path_copy = hex_http_copy_bytes(path);
    if (method_copy == NULL || path_copy == NULL) {
        if (method_copy != NULL) {
            hex_heap_free(method_copy);
        }
        if (path_copy != NULL) {
            hex_heap_free(path_copy);
        }
        return hex_http_status_failure(hex_http_kind_resource(), &hex_http_message_route_memory);
    }
    router->routes[router->count++] = (hex_http_route){
        .method = method_copy,
        .method_length = method->byte_length,
        .path = path_copy,
        .path_length = path->byte_length,
        .handler = handler,
        .invoke = invoke,
    };
    return (hex_http_status_result){ .ok = true };
}
{{end}}
{{if .NeedFree}}
void hex_http_router_free_raw(hex_http_router router, hex_heap h) {
    (void)h;
    if (atomic_load(&router->attached) != 0) {
        hex_runtime_trap("[Runtime Error] router freed while attached to a server\n");
    }
    for (size_t index = 0; index < router->count; index++) {
        hex_heap_free(router->routes[index].method);
        hex_heap_free(router->routes[index].path);
    }
    if (router->routes != NULL) {
        hex_heap_free(router->routes);
    }
    hex_heap_free(router);
}
{{end}}
{{if .NeedRuntime}}
// The connection runtime. One Task accepts; each accepted connection gets one
// Task that parses, dispatches, and answers requests until the connection
// closes. Native operations carry absolute monotonic deadlines (a hex_instant
// reading), so a phase deadline bounds the whole phase, not each read. No
// handler runs in a libuv callback.

// A request's life on one connection, and the response bytes it produces,
// share one exchange record owned by the connection.
enum {
    HEX_HTTP_BODY_NOT_STARTED,
    HEX_HTTP_BODY_COMPLETE,
    HEX_HTTP_BODY_FAILED,
};

enum {
    HEX_HTTP_RESPONSE_OPEN,      // status and headers may still change
    HEX_HTTP_RESPONSE_COMMITTED, // a write began; bytes are buffered or sent
};

enum {
    HEX_HTTP_FRAMING_NONE,
    HEX_HTTP_FRAMING_LENGTH,
    HEX_HTTP_FRAMING_CHUNKED,
    HEX_HTTP_FRAMING_CLOSE,
};

// The output buffer is [head region][chunk prefix][body region][slack]. The
// head region holds the user's header lines while the response is open; the
// final head is assembled right-aligned so it ends where the body starts (or
// where a chunk header starts), making head, chunk header, and body one
// contiguous write. The slack holds a chunk's trailing CRLF and the final
// zero-size chunk.
constexpr size_t HEX_HTTP_HEAD_BYTES = {{.ResponseHeadBytes}};
constexpr size_t HEX_HTTP_CHUNK_PREFIX = 18;
constexpr size_t HEX_HTTP_CHUNK_SLACK = 8;
constexpr size_t HEX_HTTP_BODY_OFFSET = HEX_HTTP_HEAD_BYTES + HEX_HTTP_CHUNK_PREFIX;
// The status line and the server-owned framing fields need room beside the
// user's headers; the user's share is what is left.
constexpr size_t HEX_HTTP_HEAD_RESERVE = 512;
constexpr hex_duration HEX_HTTP_LINGER_NANOSECONDS = {{.LingerTimeout}}ULL;
constexpr hex_duration HEX_HTTP_POLL_NANOSECONDS = {{.PollInterval}}ULL;

typedef struct hex_http_connection hex_http_connection;

struct hex_http_exchange {
    hex_http_connection *connection;
    uint8_t body_state;
    bool body_continued;
    bool continue_sent;
    uint64_t body_deadline;
    bool headers_built;

    uint8_t response;
    bool head_sent;
    uint8_t framing;
    uint16_t status;
    bool has_length;
    uint64_t declared_length;
    uint64_t written;
    size_t user_length;
    size_t body_length;
    bool has_date;
    bool suppress_body;
    // keep_alive is the connection's persistence for this exchange; false makes
    // the response announce Connection: close.
    bool keep_alive;
    // chunk_capable is true for an HTTP/1.1 request: only it may be answered
    // with chunked transfer coding.
    bool chunk_capable;
    // failed records a native write failure, after which the connection is
    // unusable and every later write reports it.
    bool failed;
    int failure_status;
};

struct hex_http_server_state;

struct hex_http_connection {
    struct hex_http_connection *next;
    struct hex_http_connection *previous;
    struct hex_http_server_state *server;
    hex_tcp_connection tcp;
    // idle is true while the Task waits for the first byte of a request; the
    // server closes idle connections at stop. stop_closed marks a connection
    // the server already closed so shutdown never repeats itself.
    atomic_bool idle;
    bool stop_closed;

    uint8_t *recv;
    size_t recv_capacity;
    size_t recv_start;
    size_t recv_end;
    uint8_t *out;
    size_t out_capacity;

    hex_http_limits limits;
    hex_http_head head;
    hex_http_parser parser;
    hex_t_Header *header_view;

    struct hex_http_exchange exchange;
};

enum {
    HEX_HTTP_SERVER_CREATED,
    HEX_HTTP_SERVER_RUNNING,
    HEX_HTTP_SERVER_DONE,
};

struct hex_http_server_state {
    hex_t_ServerConfig config;
    struct hex_http_router_state *router;
    const void *app;
    hex_tcp_listener listener;
    // lock guards phase, the stop flags, the connection registry, and the
    // result; done is closed once run finishes and wakes every waiter.
    hex_mutex *lock;
    hex_chan *done;
    int phase;
    atomic_bool stopping;
    bool listener_closed;
    size_t active;
    hex_http_connection *connections;
    bool failed;
    hex_t_ErrorKind failure_kind;
    const hex_string *failure_message;
};

HEX_HTTP_MESSAGE(hex_http_message_bad_host, "host must be a numeric IP address");
HEX_HTTP_MESSAGE(hex_http_message_bad_config, "server configuration values must be positive and in range");
HEX_HTTP_MESSAGE(hex_http_message_server_memory, "server ran out of memory");
HEX_HTTP_MESSAGE(hex_http_message_listen_failed, "listen failed");
HEX_HTTP_MESSAGE(hex_http_message_address_in_use, "address already in use");
HEX_HTTP_MESSAGE(hex_http_message_denied, "permission denied");
HEX_HTTP_MESSAGE(hex_http_message_accept_failed, "accept failed");
HEX_HTTP_MESSAGE(hex_http_message_already_run, "server already ran or is running");
HEX_HTTP_MESSAGE(hex_http_message_not_started, "server has not started");
HEX_HTTP_MESSAGE(hex_http_message_connection_closed, "connection closed");
HEX_HTTP_MESSAGE(hex_http_message_reset, "connection reset by peer");
HEX_HTTP_MESSAGE(hex_http_message_broken_pipe, "broken pipe");
HEX_HTTP_MESSAGE(hex_http_message_write_timeout, "response write timed out");
HEX_HTTP_MESSAGE(hex_http_message_write_failed, "response write failed");
HEX_HTTP_MESSAGE(hex_http_message_body_timeout, "request body timed out");
HEX_HTTP_MESSAGE(hex_http_message_body_limit, "request body exceeds limit");
HEX_HTTP_MESSAGE(hex_http_message_body_malformed, "malformed request body");
HEX_HTTP_MESSAGE(hex_http_message_body_truncated, "connection closed before the request body completed");
HEX_HTTP_MESSAGE(hex_http_message_read_failed, "request body read failed");
HEX_HTTP_MESSAGE(hex_http_message_committed, "response already committed");
HEX_HTTP_MESSAGE(hex_http_message_bad_status, "status code must be between 200 and 599");
HEX_HTTP_MESSAGE(hex_http_message_bad_header_name, "header name must be a nonempty token");
HEX_HTTP_MESSAGE(hex_http_message_bad_header_value, "header value must not contain CR, LF, or NUL");
HEX_HTTP_MESSAGE(hex_http_message_owned_header, "header is owned by the server");
HEX_HTTP_MESSAGE(hex_http_message_head_full, "response head storage is full");
HEX_HTTP_MESSAGE(hex_http_message_length_exceeded, "response body exceeds the declared content length");

static inline hex_t_ErrorKind hex_http_kind_other(void) {
    static const char text[] = "network error";
    hex_string_128 header = { .byte_length = sizeof(text) - 1 };
    memcpy(header.data, text, sizeof(text) - 1);
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_Other, .other_header = header };
}

// hex_http_status_error classifies one native status: the network layer's own
// codes first, then libuv's through the shared mapper. fallback names a
// failure no table row covers.
static void hex_http_status_error(int status, const hex_string *fallback, hex_t_ErrorKind *kind, const hex_string **message) {
    *message = fallback;
    if (status == HEX_NETWORK_CLOSED) {
        *kind = (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_Closed };
        *message = &hex_http_message_connection_closed;
    } else if (status == HEX_NETWORK_TIMED_OUT) {
        *kind = (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_TimedOut };
    } else if (status == HEX_NETWORK_BUSY) {
        *kind = (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_Busy };
    } else if (status == HEX_NETWORK_ALLOCATION_FAILED) {
        *kind = hex_http_kind_resource();
        *message = &hex_http_message_server_memory;
    } else if (hex_handle_error_kind(status, kind)) {
        switch (kind->tag) {
        case hex_tag_ErrorKind_AddressInUse:
            *message = &hex_http_message_address_in_use;
            break;
        case hex_tag_ErrorKind_PermissionDenied:
            *message = &hex_http_message_denied;
            break;
        case hex_tag_ErrorKind_ConnectionReset:
            *message = &hex_http_message_reset;
            break;
        case hex_tag_ErrorKind_BrokenPipe:
            *message = &hex_http_message_broken_pipe;
            break;
        default:
            break;
        }
    } else {
        *kind = hex_http_kind_other();
    }
}

static inline uint64_t hex_http_deadline(hex_duration timeout) {
    uint64_t now = hex_instant_now();
    uint64_t deadline;
    if (ckd_add(&deadline, now, (uint64_t)timeout)) {
        return UINT64_MAX;
    }
    return deadline;
}

static inline uint64_t hex_http_nonzero(uint64_t deadline) {
    return deadline == 0 ? 1 : deadline;
}

// --- Response serialization ---

static const char *hex_http_reason(uint16_t status) {
    switch (status) {
    case 100: return "Continue";
    case 200: return "OK";
    case 201: return "Created";
    case 202: return "Accepted";
    case 204: return "No Content";
    case 206: return "Partial Content";
    case 301: return "Moved Permanently";
    case 302: return "Found";
    case 303: return "See Other";
    case 304: return "Not Modified";
    case 307: return "Temporary Redirect";
    case 308: return "Permanent Redirect";
    case 400: return "Bad Request";
    case 401: return "Unauthorized";
    case 403: return "Forbidden";
    case 404: return "Not Found";
    case 405: return "Method Not Allowed";
    case 408: return "Request Timeout";
    case 409: return "Conflict";
    case 410: return "Gone";
    case 411: return "Length Required";
    case 413: return "Content Too Large";
    case 414: return "URI Too Long";
    case 415: return "Unsupported Media Type";
    case 416: return "Range Not Satisfiable";
    case 417: return "Expectation Failed";
    case 422: return "Unprocessable Content";
    case 429: return "Too Many Requests";
    case 431: return "Request Header Fields Too Large";
    case 500: return "Internal Server Error";
    case 501: return "Not Implemented";
    case 502: return "Bad Gateway";
    case 503: return "Service Unavailable";
    case 504: return "Gateway Timeout";
    case 505: return "HTTP Version Not Supported";
    default:
        return status < 300 ? "OK" : status < 400 ? "Redirection" : status < 500 ? "Client Error" : "Server Error";
    }
}

// hex_http_append copies text onto a bounded buffer. Every caller reserves the
// room it needs beforehand, so this checks nothing.
static inline size_t hex_http_append(uint8_t *buffer, size_t length, const char *text) {
    size_t size = strlen(text);
    memcpy(buffer + length, text, size);
    return length + size;
}

static inline size_t hex_http_append_decimal(uint8_t *buffer, size_t length, uint64_t value) {
    char digits[20];
    size_t count = 0;
    do {
        digits[count++] = (char)('0' + value % 10);
        value /= 10;
    } while (value != 0);
    while (count != 0) {
        buffer[length++] = (uint8_t)digits[--count];
    }
    return length;
}

// hex_http_append_date writes the IMF-fixdate of the current time. The
// calendar comes from the civil-from-days conversion below: C23's gmtime_r was
// considered, but the qualified Windows headers do not declare it and their
// gmtime_s takes its arguments in the opposite order, so no one standard call
// serves every target.
static size_t hex_http_append_date(uint8_t *buffer, size_t length) {
    static const char days[7][4] = { "Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat" };
    static const char months[12][4] = { "Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec" };
    int64_t seconds = (int64_t)time(nullptr);
    int64_t day_count = seconds / 86400;
    int64_t second_of_day = seconds % 86400;
    if (second_of_day < 0) {
        second_of_day += 86400;
        day_count--;
    }
    // 1970-01-01 was a Thursday.
    unsigned weekday = (unsigned)((day_count % 7 + 11) % 7);
    int64_t shifted = day_count + 719468;
    int64_t era = (shifted >= 0 ? shifted : shifted - 146096) / 146097;
    unsigned day_of_era = (unsigned)(shifted - era * 146097);
    unsigned year_of_era = (day_of_era - day_of_era / 1460 + day_of_era / 36524 - day_of_era / 146096) / 365;
    int64_t year = (int64_t)year_of_era + era * 400;
    unsigned day_of_year = day_of_era - (365 * year_of_era + year_of_era / 4 - year_of_era / 100);
    unsigned month_index = (5 * day_of_year + 2) / 153;
    unsigned day = day_of_year - (153 * month_index + 2) / 5 + 1;
    unsigned month = month_index < 10 ? month_index + 3 : month_index - 9;
    if (month <= 2) {
        year++;
    }
    unsigned hour = (unsigned)(second_of_day / 3600);
    unsigned minute = (unsigned)(second_of_day % 3600 / 60);
    unsigned second = (unsigned)(second_of_day % 60);

    length = hex_http_append(buffer, length, "Date: ");
    length = hex_http_append(buffer, length, days[weekday]);
    length = hex_http_append(buffer, length, ", ");
    buffer[length++] = (uint8_t)('0' + day / 10);
    buffer[length++] = (uint8_t)('0' + day % 10);
    buffer[length++] = ' ';
    length = hex_http_append(buffer, length, months[month - 1]);
    buffer[length++] = ' ';
    length = hex_http_append_decimal(buffer, length, (uint64_t)year);
    buffer[length++] = ' ';
    buffer[length++] = (uint8_t)('0' + hour / 10);
    buffer[length++] = (uint8_t)('0' + hour % 10);
    buffer[length++] = ':';
    buffer[length++] = (uint8_t)('0' + minute / 10);
    buffer[length++] = (uint8_t)('0' + minute % 10);
    buffer[length++] = ':';
    buffer[length++] = (uint8_t)('0' + second / 10);
    buffer[length++] = (uint8_t)('0' + second % 10);
    return hex_http_append(buffer, length, " GMT\r\n");
}

static inline bool hex_http_status_has_body(uint16_t status) {
    return !(status < 200 || status == 204 || status == 304);
}

// hex_http_send writes one contiguous range with the response write deadline.
// A native failure marks the connection unusable and is reported once.
static int hex_http_send(hex_http_connection *connection, const uint8_t *data, size_t length) {
    if (length == 0) {
        return 0;
    }
    uint64_t deadline = hex_http_nonzero(hex_http_deadline(connection->server->config.hex_m_write_timeout));
    return hex_tcp_write_until(connection->tcp, (hex_slice_UInt8){ .data = data, .length = length }, deadline);
}

// hex_http_assemble_head places the status line, the user's header lines, and
// the server-owned fields in the head region, right-aligned to end at end, and
// returns where it starts. The user's lines move up by memmove: the region the
// final head occupies may overlap where they were stored.
static size_t hex_http_assemble_head(struct hex_http_exchange *exchange, size_t end, bool chunked, bool has_length, uint64_t length) {
    hex_http_connection *connection = exchange->connection;
    uint8_t *out = connection->out;
    uint8_t line[96];
    size_t line_length = hex_http_append(line, 0, "HTTP/1.1 ");
    line_length = hex_http_append_decimal(line, line_length, exchange->status);
    line[line_length++] = ' ';
    line_length = hex_http_append(line, line_length, hex_http_reason(exchange->status));
    line_length = hex_http_append(line, line_length, "\r\n");

    uint8_t tail[160];
    size_t tail_length = 0;
    if (!exchange->has_date) {
        tail_length = hex_http_append_date(tail, tail_length);
    }
    if (!exchange->keep_alive) {
        tail_length = hex_http_append(tail, tail_length, "Connection: close\r\n");
    }
    if (chunked) {
        tail_length = hex_http_append(tail, tail_length, "Transfer-Encoding: chunked\r\n");
    } else if (has_length) {
        tail_length = hex_http_append(tail, tail_length, "Content-Length: ");
        tail_length = hex_http_append_decimal(tail, tail_length, length);
        tail_length = hex_http_append(tail, tail_length, "\r\n");
    }
    tail_length = hex_http_append(tail, tail_length, "\r\n");

    size_t total = line_length + exchange->user_length + tail_length;
    size_t start = end - total;
    memmove(out + start + line_length, out, exchange->user_length);
    memcpy(out + start, line, line_length);
    memcpy(out + start + line_length + exchange->user_length, tail, tail_length);
    return start;
}

// hex_http_transmit sends the head (once) and the buffered body. A non-final
// call is a buffer-full flush: framing is chosen now, because the body length
// is not yet known. A final call finishes the response; a response whose whole
// body is still buffered gets an exact Content-Length instead of chunking.
static int hex_http_transmit(struct hex_http_exchange *exchange, bool final) {
    hex_http_connection *connection = exchange->connection;
    uint8_t *out = connection->out;
    uint8_t *body = out + HEX_HTTP_BODY_OFFSET;
    size_t body_length = exchange->body_length;
    size_t start_offset;
    size_t end_offset = HEX_HTTP_BODY_OFFSET + body_length;

    if (!exchange->head_sent) {
        bool chunked = false;
        bool has_length = false;
        uint64_t length = 0;
        if (exchange->suppress_body) {
            exchange->framing = HEX_HTTP_FRAMING_NONE;
            // A HEAD response states the length its GET would have; a status
            // that forbids a body states none.
            if (exchange->status >= 200 && exchange->status != 204 && exchange->status != 304) {
                has_length = true;
                length = exchange->has_length ? exchange->declared_length : exchange->written;
            }
            body_length = 0;
            end_offset = HEX_HTTP_BODY_OFFSET;
        } else if (exchange->has_length) {
            exchange->framing = HEX_HTTP_FRAMING_LENGTH;
            has_length = true;
            length = exchange->declared_length;
        } else if (final) {
            exchange->framing = HEX_HTTP_FRAMING_LENGTH;
            has_length = true;
            length = body_length;
        } else if (exchange->chunk_capable) {
            exchange->framing = HEX_HTTP_FRAMING_CHUNKED;
            chunked = true;
        } else {
            exchange->framing = HEX_HTTP_FRAMING_CLOSE;
        }
        size_t head_end = HEX_HTTP_BODY_OFFSET;
        size_t prefix = 0;
        if (chunked && body_length != 0) {
            char digits[17];
            size_t count = 0;
            size_t size = body_length;
            do {
                digits[count++] = "0123456789abcdef"[size & 15];
                size >>= 4;
            } while (size != 0);
            prefix = count + 2;
            uint8_t *place = body - prefix;
            for (size_t index = 0; index < count; index++) {
                place[index] = (uint8_t)digits[count - 1 - index];
            }
            place[count] = '\r';
            place[count + 1] = '\n';
            head_end -= prefix;
        }
        start_offset = hex_http_assemble_head(exchange, head_end, chunked, has_length, length);
        exchange->head_sent = true;
        if (chunked && body_length != 0) {
            body[body_length] = '\r';
            body[body_length + 1] = '\n';
            end_offset += 2;
        }
    } else if (exchange->framing == HEX_HTTP_FRAMING_CHUNKED && body_length != 0) {
        char digits[17];
        size_t count = 0;
        size_t size = body_length;
        do {
            digits[count++] = "0123456789abcdef"[size & 15];
            size >>= 4;
        } while (size != 0);
        size_t prefix = count + 2;
        uint8_t *place = body - prefix;
        for (size_t index = 0; index < count; index++) {
            place[index] = (uint8_t)digits[count - 1 - index];
        }
        place[count] = '\r';
        place[count + 1] = '\n';
        start_offset = HEX_HTTP_BODY_OFFSET - prefix;
        body[body_length] = '\r';
        body[body_length + 1] = '\n';
        end_offset += 2;
    } else {
        start_offset = HEX_HTTP_BODY_OFFSET;
    }
    if (final && exchange->framing == HEX_HTTP_FRAMING_CHUNKED) {
        memcpy(out + end_offset, "0\r\n\r\n", 5);
        end_offset += 5;
    }
    exchange->body_length = 0;
    int status = hex_http_send(connection, out + start_offset, end_offset - start_offset);
    if (status != 0) {
        exchange->failed = true;
        exchange->failure_status = status;
    }
    return status;
}

// hex_http_simple_response answers without a handler: the server's own
// status, a short plain-text body, and Connection: close when the connection
// will not be reused. Every byte lives in the head region, so it needs no
// body buffer.
static int hex_http_simple_response(hex_http_connection *connection, uint16_t status, const char *allow, bool close_after) {
    uint8_t *out = connection->out;
    const char *reason = hex_http_reason(status);
    size_t body_length = strlen(reason) + 1;
    size_t length = hex_http_append(out, 0, "HTTP/1.1 ");
    length = hex_http_append_decimal(out, length, status);
    out[length++] = ' ';
    length = hex_http_append(out, length, reason);
    length = hex_http_append(out, length, "\r\n");
    length = hex_http_append_date(out, length);
    if (allow != nullptr) {
        length = hex_http_append(out, length, "Allow: ");
        length = hex_http_append(out, length, allow);
        length = hex_http_append(out, length, "\r\n");
    }
    if (close_after) {
        length = hex_http_append(out, length, "Connection: close\r\n");
    }
    length = hex_http_append(out, length, "Content-Type: text/plain; charset=utf-8\r\nContent-Length: ");
    length = hex_http_append_decimal(out, length, body_length);
    length = hex_http_append(out, length, "\r\n\r\n");
    length = hex_http_append(out, length, reason);
    out[length++] = '\n';
    return hex_http_send(connection, out, length);
}

// --- Writer ---

static inline bool hex_http_token_byte(uint8_t byte) {
    if ((byte >= '0' && byte <= '9') || (byte >= 'a' && byte <= 'z') || (byte >= 'A' && byte <= 'Z')) {
        return true;
    }
    return byte != 0 && strchr("!#$%&'*+-.^_`|~", (char)byte) != nullptr;
}

static bool hex_http_equals_nocase(const uint8_t *left, const uint8_t *right, size_t length) {
    for (size_t index = 0; index < length; index++) {
        uint8_t a = left[index];
        uint8_t b = right[index];
        if (a >= 'A' && a <= 'Z') {
            a = (uint8_t)(a + ('a' - 'A'));
        }
        if (b >= 'A' && b <= 'Z') {
            b = (uint8_t)(b + ('a' - 'A'));
        }
        if (a != b) {
            return false;
        }
    }
    return true;
}

static bool hex_http_name_is(const hex_string *name, const char *text) {
    size_t length = strlen(text);
    return name->byte_length == length && hex_http_equals_nocase(name->data, (const uint8_t *)text, length);
}

hex_http_status_result hex_http_writer_status_raw(hex_http_writer writer, uint16_t code) {
    if (writer->response != HEX_HTTP_RESPONSE_OPEN) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_committed);
    }
    if (code < 200 || code > 599) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_bad_status);
    }
    writer->status = code;
    return (hex_http_status_result){ .ok = true };
}

hex_http_status_result hex_http_writer_header_raw(hex_http_writer writer, const hex_string *name, hex_slice_UInt8 value) {
    if (writer->response != HEX_HTTP_RESPONSE_OPEN) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_committed);
    }
    if (name->byte_length == 0) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_bad_header_name);
    }
    for (size_t index = 0; index < name->byte_length; index++) {
        if (!hex_http_token_byte(name->data[index])) {
            return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_bad_header_name);
        }
    }
    for (size_t index = 0; index < value.length; index++) {
        uint8_t byte = value.data[index];
        if (byte == '\r' || byte == '\n' || byte == 0) {
            return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_bad_header_value);
        }
    }
    if (hex_http_name_is(name, "content-length") || hex_http_name_is(name, "transfer-encoding") || hex_http_name_is(name, "connection")) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_owned_header);
    }
    size_t needed;
    if (ckd_add(&needed, name->byte_length, value.length) || ckd_add(&needed, needed, 4) ||
        needed > HEX_HTTP_HEAD_BYTES - HEX_HTTP_HEAD_RESERVE - writer->user_length) {
        return hex_http_status_failure(hex_http_kind_resource(), &hex_http_message_head_full);
    }
    uint8_t *place = writer->connection->out + writer->user_length;
    memcpy(place, name->data, name->byte_length);
    place[name->byte_length] = ':';
    place[name->byte_length + 1] = ' ';
    if (value.length != 0) {
        memcpy(place + name->byte_length + 2, value.data, value.length);
    }
    place[name->byte_length + 2 + value.length] = '\r';
    place[name->byte_length + 3 + value.length] = '\n';
    writer->user_length += needed;
    if (hex_http_name_is(name, "date")) {
        writer->has_date = true;
    }
    return (hex_http_status_result){ .ok = true };
}

hex_http_status_result hex_http_writer_content_length_raw(hex_http_writer writer, size_t length) {
    if (writer->response != HEX_HTTP_RESPONSE_OPEN) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_committed);
    }
    writer->has_length = true;
    writer->declared_length = length;
    return (hex_http_status_result){ .ok = true };
}

static hex_http_status_result hex_http_write_failure(struct hex_http_exchange *exchange) {
    hex_t_ErrorKind kind;
    const hex_string *message;
    hex_http_status_error(exchange->failure_status, &hex_http_message_write_failed, &kind, &message);
    if (exchange->failure_status == HEX_NETWORK_TIMED_OUT) {
        message = &hex_http_message_write_timeout;
    }
    return hex_http_status_failure(kind, message);
}

hex_http_status_result hex_http_writer_write_raw(hex_http_writer writer, hex_slice_UInt8 bytes) {
    if (writer->failed) {
        return hex_http_write_failure(writer);
    }
    uint64_t total;
    if (ckd_add(&total, writer->written, (uint64_t)bytes.length) || (writer->has_length && total > writer->declared_length)) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_length_exceeded);
    }
    if (writer->response == HEX_HTTP_RESPONSE_OPEN) {
        writer->response = HEX_HTTP_RESPONSE_COMMITTED;
        writer->suppress_body = writer->suppress_body || !hex_http_status_has_body(writer->status);
    }
    writer->written = total;
    if (writer->suppress_body) {
        return (hex_http_status_result){ .ok = true };
    }
    hex_http_connection *connection = writer->connection;
    size_t capacity = connection->out_capacity;
    const uint8_t *cursor = bytes.data;
    size_t remaining = bytes.length;
    while (remaining != 0) {
        if (writer->body_length == capacity) {
            if (hex_http_transmit(writer, false) != 0) {
                return hex_http_write_failure(writer);
            }
        }
        size_t room = capacity - writer->body_length;
        size_t take = remaining < room ? remaining : room;
        memcpy(connection->out + HEX_HTTP_BODY_OFFSET + writer->body_length, cursor, take);
        writer->body_length += take;
        cursor += take;
        remaining -= take;
    }
    return (hex_http_status_result){ .ok = true };
}

// --- Request ---

hex_slice_UInt8 hex_http_request_method_raw(hex_http_request request) {
    const hex_http_head *head = &request->connection->head;
    return (hex_slice_UInt8){ .data = head->bytes + head->method_offset, .length = head->method_length };
}

hex_slice_UInt8 hex_http_request_target_raw(hex_http_request request) {
    const hex_http_head *head = &request->connection->head;
    return (hex_slice_UInt8){ .data = head->bytes + head->target_offset, .length = head->target_length };
}

hex_slice_UInt8 hex_http_request_path_raw(hex_http_request request) {
    const hex_http_head *head = &request->connection->head;
    return (hex_slice_UInt8){ .data = head->bytes + head->path_offset, .length = head->path_length };
}

hex_slice_Header hex_http_request_headers_raw(hex_http_request request) {
    hex_http_connection *connection = request->connection;
    const hex_http_head *head = &connection->head;
    if (!request->headers_built) {
        for (size_t index = 0; index < head->field_count; index++) {
            const hex_http_field *field = &head->fields[index];
            connection->header_view[index] = (hex_t_Header){
                .hex_m_name = { .data = head->bytes + field->name_offset, .length = field->name_length },
                .hex_m_value = { .data = head->bytes + field->value_offset, .length = field->value_length },
            };
        }
        request->headers_built = true;
    }
    return (hex_slice_Header){ .data = connection->header_view, .length = head->field_count };
}

hex_http_bytes_result hex_http_request_header_raw(hex_http_request request, const hex_string *name) {
    const hex_http_head *head = &request->connection->head;
    for (size_t index = 0; index < head->field_count; index++) {
        const hex_http_field *field = &head->fields[index];
        if (field->name_length == name->byte_length &&
            hex_http_equals_nocase(head->bytes + field->name_offset, name->data, name->byte_length)) {
            return (hex_http_bytes_result){ .found = true, .bytes = { .data = head->bytes + field->value_offset, .length = field->value_length } };
        }
    }
    return (hex_http_bytes_result){ .found = false };
}

// hex_http_fill appends newly received bytes to the receive buffer. A
// partially consumed buffer is compacted first; the parser consumes every byte
// of an incomplete message, so only pipelined remainders ever move.
static int hex_http_fill(hex_http_connection *connection, uint64_t deadline) {
    if (connection->recv_start == connection->recv_end) {
        connection->recv_start = 0;
        connection->recv_end = 0;
    } else if (connection->recv_end == connection->recv_capacity && connection->recv_start != 0) {
        memmove(connection->recv, connection->recv + connection->recv_start, connection->recv_end - connection->recv_start);
        connection->recv_end -= connection->recv_start;
        connection->recv_start = 0;
    }
    hex_tcp_transfer transfer = hex_tcp_read_until(connection->tcp, connection->recv + connection->recv_end, connection->recv_capacity - connection->recv_end, deadline);
    if (transfer.status == 0) {
        connection->recv_end += transfer.count;
    }
    return transfer.status;
}

static hex_http_read_result hex_http_read_failure(hex_t_ErrorKind kind, const hex_string *message) {
    return (hex_http_read_result){ .ok = false, .kind = kind, .message = message };
}

static const char hex_http_continue_line[] = "HTTP/1.1 100 Continue\r\n\r\n";

hex_http_read_result hex_http_request_read_raw(hex_http_request request, hex_list_UInt8 *into, size_t count) {
    hex_http_connection *connection = request->connection;
    if (count == 0) {
        return (hex_http_read_result){ .ok = true };
    }
    if (request->body_state == HEX_HTTP_BODY_COMPLETE) {
        return (hex_http_read_result){ .ok = true, .end_of_stream = true };
    }
    if (request->body_state == HEX_HTTP_BODY_FAILED) {
        return hex_http_read_failure(hex_http_kind_invalid_input(), &hex_http_message_read_failed);
    }
    if (!request->body_continued) {
        hex_http_parser_continue(&connection->parser);
        request->body_continued = true;
        request->body_deadline = hex_http_nonzero(hex_http_deadline(connection->server->config.hex_m_body_timeout));
        const hex_http_head *head = &connection->head;
        bool has_body = head->chunked || (head->has_content_length && head->content_length != 0);
        if (head->expect_continue && has_body && !request->continue_sent) {
            request->continue_sent = true;
            int status = hex_http_send(connection, (const uint8_t *)hex_http_continue_line, sizeof(hex_http_continue_line) - 1);
            if (status != 0) {
                request->failed = true;
                request->failure_status = status;
                request->body_state = HEX_HTTP_BODY_FAILED;
                request->keep_alive = false;
                hex_t_ErrorKind kind;
                const hex_string *message;
                hex_http_status_error(status, &hex_http_message_read_failed, &kind, &message);
                return hex_http_read_failure(kind, message);
            }
        }
    }
    // One call never needs more room than the receive buffer holds: the parser
    // decodes no more bytes than it is fed, and it is fed from that buffer.
    if (count > connection->recv_capacity) {
        count = connection->recv_capacity;
    }
    size_t needed;
    if (ckd_add(&needed, into->length, count)) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    hex_list_reserve_at_least_UInt8(into, needed);
    for (;;) {
        size_t available = connection->recv_end - connection->recv_start;
        if (available != 0) {
            size_t feed = available < count ? available : count;
            hex_http_step step = hex_http_parser_feed(&connection->parser, connection->recv + connection->recv_start, feed, into->data + into->length, count);
            connection->recv_start += step.consumed;
            if (step.state == HEX_HTTP_ERROR) {
                request->body_state = HEX_HTTP_BODY_FAILED;
                request->keep_alive = false;
                if (step.error == HEX_HTTP_ERROR_BODY_TOO_LARGE) {
                    return hex_http_read_failure(hex_http_kind_resource(), &hex_http_message_body_limit);
                }
                return hex_http_read_failure(hex_http_kind_invalid_input(), &hex_http_message_body_malformed);
            }
            into->length += step.produced;
            if (step.state == HEX_HTTP_MESSAGE_COMPLETE) {
                request->body_state = HEX_HTTP_BODY_COMPLETE;
            }
            if (step.produced != 0) {
                return (hex_http_read_result){ .ok = true, .count = step.produced };
            }
            if (step.state == HEX_HTTP_MESSAGE_COMPLETE) {
                return (hex_http_read_result){ .ok = true, .end_of_stream = true };
            }
            continue;
        }
        int status = hex_http_fill(connection, request->body_deadline);
        if (status == 0) {
            continue;
        }
        request->body_state = HEX_HTTP_BODY_FAILED;
        request->keep_alive = false;
        if (status == HEX_NETWORK_EOS) {
            return hex_http_read_failure((hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_ConnectionReset }, &hex_http_message_body_truncated);
        }
        if (status == HEX_NETWORK_TIMED_OUT) {
            return hex_http_read_failure((hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_TimedOut }, &hex_http_message_body_timeout);
        }
        hex_t_ErrorKind kind;
        const hex_string *message;
        hex_http_status_error(status, &hex_http_message_read_failed, &kind, &message);
        return hex_http_read_failure(kind, message);
    }
}

// --- Connection ---

// The outcomes of reading one request head besides a complete head: a
// positive value is the status the server answers with before closing.
constexpr int HEX_HTTP_HEAD_CLOSE = -1;

static int hex_http_read_head(hex_http_connection *connection) {
    const hex_t_ServerConfig *config = &connection->server->config;
    bool started = connection->recv_end != connection->recv_start;
    uint64_t header_deadline = started ? hex_http_nonzero(hex_http_deadline(config->hex_m_header_timeout)) : 0;
    uint64_t idle_deadline = hex_http_nonzero(hex_http_deadline(config->hex_m_idle_timeout));
    for (;;) {
        if (connection->recv_end != connection->recv_start) {
            hex_http_step step = hex_http_parser_feed(&connection->parser, connection->recv + connection->recv_start, connection->recv_end - connection->recv_start, nullptr, 0);
            connection->recv_start += step.consumed;
            if (!started) {
                started = true;
                header_deadline = hex_http_nonzero(hex_http_deadline(config->hex_m_header_timeout));
            }
            if (step.state == HEX_HTTP_HEAD_READY) {
                return 0;
            }
            if (step.state == HEX_HTTP_ERROR) {
                return step.status != 0 ? (int)step.status : HEX_HTTP_HEAD_CLOSE;
            }
        }
        // The server closes idle connections at stop. It sets stopping before
        // it looks for idle ones, so setting idle and then reading stopping
        // makes at least one side observe the other.
        atomic_store(&connection->idle, !started);
        if (!started && atomic_load(&connection->server->stopping)) {
            atomic_store(&connection->idle, false);
            return HEX_HTTP_HEAD_CLOSE;
        }
        int status = hex_http_fill(connection, started ? header_deadline : idle_deadline);
        atomic_store(&connection->idle, false);
        if (status == 0) {
            continue;
        }
        if (status == HEX_NETWORK_TIMED_OUT && started) {
            return 408;
        }
        return HEX_HTTP_HEAD_CLOSE;
    }
}

// hex_http_complete_body finishes a message that has no body without any I/O,
// so a GET keeps its connection. A message with an unread body stays
// incomplete: nothing drains it.
static bool hex_http_complete_body(struct hex_http_exchange *exchange) {
    if (exchange->body_state == HEX_HTTP_BODY_COMPLETE) {
        return true;
    }
    if (exchange->body_state == HEX_HTTP_BODY_FAILED) {
        return false;
    }
    hex_http_connection *connection = exchange->connection;
    const hex_http_head *head = &connection->head;
    if (head->chunked || (head->has_content_length && head->content_length != 0)) {
        return false;
    }
    if (!exchange->body_continued) {
        hex_http_parser_continue(&connection->parser);
        exchange->body_continued = true;
    }
    hex_http_step step = hex_http_parser_feed(&connection->parser, connection->recv + connection->recv_start, 0, nullptr, 0);
    if (step.state != HEX_HTTP_MESSAGE_COMPLETE) {
        return false;
    }
    exchange->body_state = HEX_HTTP_BODY_COMPLETE;
    return true;
}

// hex_http_find_route matches the raw path and method exactly. When the path
// is registered under other methods only, allow receives their sorted names.
static const hex_http_route *hex_http_find_route(const struct hex_http_router_state *router, const hex_http_head *head, char *allow, size_t allow_size, bool *path_known) {
    const uint8_t *method = head->bytes + head->method_offset;
    const uint8_t *path = head->bytes + head->path_offset;
    const hex_http_route *match = nullptr;
    const hex_http_route *methods[64];
    size_t method_count = 0;
    *path_known = false;
    for (size_t index = 0; index < router->count; index++) {
        const hex_http_route *route = &router->routes[index];
        if (route->path_length != head->path_length || memcmp(route->path, path, head->path_length) != 0) {
            continue;
        }
        *path_known = true;
        if (route->method_length == head->method_length && memcmp(route->method, method, head->method_length) == 0) {
            match = route;
        }
        if (method_count < sizeof(methods) / sizeof(methods[0])) {
            methods[method_count++] = route;
        }
    }
    if (match == nullptr && allow != nullptr) {
        // The method set is closed and small, so an insertion sort over the
        // registered names gives the deterministic order the header needs.
        for (size_t outer = 1; outer < method_count; outer++) {
            const hex_http_route *item = methods[outer];
            size_t inner = outer;
            while (inner > 0) {
                const hex_http_route *other = methods[inner - 1];
                size_t common = other->method_length < item->method_length ? other->method_length : item->method_length;
                int order = memcmp(other->method, item->method, common);
                if (order < 0 || (order == 0 && other->method_length <= item->method_length)) {
                    break;
                }
                methods[inner] = other;
                inner--;
            }
            methods[inner] = item;
        }
        size_t length = 0;
        for (size_t index = 0; index < method_count; index++) {
            size_t size = methods[index]->method_length;
            if (length + size + 2 >= allow_size) {
                break;
            }
            if (index != 0) {
                allow[length++] = ',';
                allow[length++] = ' ';
            }
            memcpy(allow + length, methods[index]->method, size);
            length += size;
        }
        allow[length] = '\0';
    }
    return match;
}

// hex_http_close discards the connection: a half-close announces the end of
// the response, and an error close first drains what the client still sends
// for a bounded time, so the client reads the response before a reset can
// destroy it.
static void hex_http_close(hex_http_connection *connection, bool linger) {
    (void)hex_tcp_shutdown(connection->tcp);
    if (linger) {
        uint64_t deadline = hex_http_nonzero(hex_http_deadline(HEX_HTTP_LINGER_NANOSECONDS));
        size_t budget = connection->recv_capacity * 8;
        for (;;) {
            connection->recv_start = 0;
            connection->recv_end = 0;
            hex_tcp_transfer transfer = hex_tcp_read_until(connection->tcp, connection->recv, connection->recv_capacity, deadline);
            if (transfer.status != 0 || transfer.count >= budget) {
                break;
            }
            budget -= transfer.count;
        }
    }
    (void)hex_tcp_close(connection->tcp);
}

static void hex_http_reset_exchange(hex_http_connection *connection, bool keep_alive, bool head_request) {
    struct hex_http_exchange *exchange = &connection->exchange;
    *exchange = (struct hex_http_exchange){
        .connection = connection,
        .status = 200,
        .keep_alive = keep_alive,
        .chunk_capable = connection->head.version_major == 1 && connection->head.version_minor >= 1,
        .suppress_body = head_request,
    };
}

// hex_http_finish answers the request once the handler returned, and reports
// whether the connection may serve another request.
static bool hex_http_finish(hex_http_connection *connection, bool handler_ok) {
    struct hex_http_exchange *exchange = &connection->exchange;
    if (!handler_ok) {
        // Before the first write an error permits a generic 500; after it the
        // status line is already committed, so the connection just closes.
        if (exchange->response == HEX_HTTP_RESPONSE_OPEN && !exchange->head_sent && !exchange->failed) {
            (void)hex_http_simple_response(connection, 500, nullptr, true);
        }
        return false;
    }
    if (exchange->failed) {
        return false;
    }
    if (!hex_http_complete_body(exchange)) {
        exchange->keep_alive = false;
    }
    bool wrote = exchange->response == HEX_HTTP_RESPONSE_COMMITTED;
    exchange->response = HEX_HTTP_RESPONSE_COMMITTED;
    exchange->suppress_body = exchange->suppress_body || !hex_http_status_has_body(exchange->status);
    if (exchange->has_length && !exchange->suppress_body && exchange->written != exchange->declared_length) {
        if (!wrote && !exchange->head_sent) {
            (void)hex_http_simple_response(connection, 500, nullptr, true);
        }
        return false;
    }
    if (hex_http_transmit(exchange, true) != 0) {
        return false;
    }
    return exchange->keep_alive && exchange->framing != HEX_HTTP_FRAMING_CLOSE;
}

static void hex_http_serve(hex_http_connection *connection) {
    struct hex_http_server_state *server = connection->server;
    hex_http_parser_init(&connection->parser, &connection->limits, &connection->head);
    for (;;) {
        int outcome = hex_http_read_head(connection);
        if (outcome == HEX_HTTP_HEAD_CLOSE) {
            hex_http_close(connection, false);
            return;
        }
        if (outcome > 0) {
            hex_http_reset_exchange(connection, false, false);
            (void)hex_http_simple_response(connection, (uint16_t)outcome, nullptr, true);
            hex_http_close(connection, true);
            return;
        }
        const hex_http_head *head = &connection->head;
        bool head_request = head->method_length == 4 && memcmp(head->bytes + head->method_offset, "HEAD", 4) == 0;
        bool keep_alive = head->version_major == 1 && head->version_minor >= 1 && !head->connection_close && !atomic_load(&server->stopping);
        hex_http_reset_exchange(connection, keep_alive, head_request);
        struct hex_http_exchange *exchange = &connection->exchange;

        char allow[640];
        bool path_known;
        const hex_http_route *route = hex_http_find_route(server->router, head, allow, sizeof(allow), &path_known);
        bool reusable;
        if (route != nullptr) {
            bool handler_ok = route->invoke(route->handler, server->app, exchange, exchange);
            reusable = hex_http_finish(connection, handler_ok);
        } else {
            if (!hex_http_complete_body(exchange)) {
                exchange->keep_alive = false;
            }
            int status = hex_http_simple_response(connection, path_known ? 405 : 404, path_known ? allow : nullptr, !exchange->keep_alive);
            reusable = status == 0 && exchange->keep_alive;
        }
        if (!reusable) {
            hex_http_close(connection, !exchange->failed && exchange->body_state != HEX_HTTP_BODY_COMPLETE);
            return;
        }
        hex_http_parser_next(&connection->parser);
        hex_task_yield();
    }
}

// --- Server lifecycle ---

static void hex_http_connection_free(hex_http_connection *connection) {
    hex_heap_free(connection->recv);
    hex_heap_free(connection->out);
    hex_heap_free(connection->head.bytes);
    hex_heap_free(connection->head.fields);
    hex_heap_free(connection->header_view);
    hex_heap_free(connection);
}

static void hex_http_connection_entry(hex_task *task) {
    hex_http_connection *connection = *(hex_http_connection **)task->args;
    struct hex_http_server_state *server = connection->server;
    hex_http_serve(connection);
    // Unlink first so shutdown stops visiting this connection, free every
    // buffer, and only then give the slot back: run finishes when the active
    // count reaches zero, so zero means no connection memory remains.
    hex_mutex_lock(server->lock);
    if (connection->previous != nullptr) {
        connection->previous->next = connection->next;
    } else {
        server->connections = connection->next;
    }
    if (connection->next != nullptr) {
        connection->next->previous = connection->previous;
    }
    hex_mutex_unlock(server->lock);
    hex_http_connection_free(connection);
    hex_mutex_lock(server->lock);
    server->active--;
    hex_mutex_unlock(server->lock);
    hex_task_complete(task);
}

// hex_http_connection_new allocates one connection's storage up front, so no
// request path allocates. Every size comes from the validated configuration.
static hex_http_connection *hex_http_connection_new(struct hex_http_server_state *server, hex_tcp_connection tcp) {
    const hex_t_ServerConfig *config = &server->config;
    hex_http_connection *connection = hex_heap_allocate_zeroed_or_null(sizeof(hex_http_connection));
    if (connection == nullptr) {
        return nullptr;
    }
    connection->server = server;
    connection->tcp = tcp;
    connection->recv_capacity = config->hex_m_receive_buffer_bytes;
    connection->out_capacity = config->hex_m_write_buffer_bytes;
    connection->limits = (hex_http_limits){
        .max_request_line_bytes = config->hex_m_max_request_line_bytes,
        .max_header_bytes = config->hex_m_max_header_bytes,
        .max_header_count = config->hex_m_max_header_count,
        .max_trailer_bytes = config->hex_m_max_trailer_bytes,
        .max_body_bytes = config->hex_m_max_body_bytes,
    };
    connection->head.capacity = config->hex_m_max_request_line_bytes + config->hex_m_max_header_bytes;
    connection->head.field_capacity = config->hex_m_max_header_count;
    connection->recv = hex_heap_allocate_or_null(connection->recv_capacity);
    connection->out = hex_heap_allocate_or_null(HEX_HTTP_BODY_OFFSET + connection->out_capacity + HEX_HTTP_CHUNK_SLACK);
    connection->head.bytes = hex_heap_allocate_or_null(connection->head.capacity);
    connection->head.fields = hex_heap_allocate_or_null(connection->head.field_capacity * sizeof(hex_http_field));
    connection->header_view = hex_heap_allocate_or_null(connection->head.field_capacity * sizeof(hex_t_Header));
    if (connection->recv == nullptr || connection->out == nullptr || connection->head.bytes == nullptr ||
        connection->head.fields == nullptr || connection->header_view == nullptr) {
        hex_http_connection_free(connection);
        return nullptr;
    }
    return connection;
}

static bool hex_http_config_valid(const hex_t_ServerConfig *config) {
    size_t sizes[] = {
        config->hex_m_max_request_line_bytes, config->hex_m_max_header_bytes, config->hex_m_max_header_count,
        config->hex_m_max_body_bytes, config->hex_m_max_trailer_bytes, config->hex_m_receive_buffer_bytes,
        config->hex_m_write_buffer_bytes, config->hex_m_max_connections, config->hex_m_backlog,
    };
    for (size_t index = 0; index < sizeof(sizes) / sizeof(sizes[0]); index++) {
        if (sizes[index] == 0) {
            return false;
        }
    }
    hex_duration durations[] = {
        config->hex_m_header_timeout, config->hex_m_body_timeout, config->hex_m_write_timeout,
        config->hex_m_idle_timeout, config->hex_m_shutdown_timeout,
    };
    for (size_t index = 0; index < sizeof(durations) / sizeof(durations[0]); index++) {
        if (durations[index] == 0) {
            return false;
        }
    }
    // The sums and products below size one connection's storage; reject a
    // configuration whose arithmetic would wrap.
    size_t head;
    size_t fields;
    size_t output;
    if (ckd_add(&head, config->hex_m_max_request_line_bytes, config->hex_m_max_header_bytes) ||
        ckd_mul(&fields, config->hex_m_max_header_count, sizeof(hex_t_Header)) ||
        ckd_add(&output, config->hex_m_write_buffer_bytes, HEX_HTTP_BODY_OFFSET + HEX_HTTP_CHUNK_SLACK)) {
        return false;
    }
    return config->hex_m_backlog <= INT_MAX;
}

hex_http_server_result hex_http_listen_raw(hex_heap h, hex_t_ServerConfig config, hex_http_router router, const void *app) {
    (void)h;
    if (!hex_http_config_valid(&config)) {
        return (hex_http_server_result){ .ok = false, .kind = hex_http_kind_invalid_input(), .message = &hex_http_message_bad_config };
    }
    hex_address_parsed parsed = hex_address_parse(config.hex_m_host, config.hex_m_port);
    if (parsed.status != 0) {
        return (hex_http_server_result){ .ok = false, .kind = hex_http_kind_invalid_input(), .message = &hex_http_message_bad_host };
    }
    struct hex_http_server_state *server = hex_heap_allocate_zeroed_or_null(sizeof(struct hex_http_server_state));
    if (server == nullptr) {
        return (hex_http_server_result){ .ok = false, .kind = hex_http_kind_resource(), .message = &hex_http_message_server_memory };
    }
    server->lock = hex_mutex_new();
    server->done = hex_chan_new(1, 1);
    if (server->lock == nullptr || server->done == nullptr) {
        if (server->lock != nullptr) {
            hex_mutex_free(server->lock);
        }
        if (server->done != nullptr) {
            hex_chan_close(server->done);
            hex_chan_free(server->done);
        }
        hex_heap_free(server);
        return (hex_http_server_result){ .ok = false, .kind = hex_http_kind_resource(), .message = &hex_http_message_server_memory };
    }
    hex_tcp_listen_result listened = hex_tcp_listen(parsed.address, config.hex_m_backlog);
    if (listened.status != 0) {
        hex_mutex_free(server->lock);
        // Freeing a channel requires it closed and empty.
        hex_chan_close(server->done);
        hex_chan_free(server->done);
        hex_heap_free(server);
        hex_http_server_result failure = { .ok = false };
        hex_http_status_error(listened.status, &hex_http_message_listen_failed, &failure.kind, &failure.message);
        return failure;
    }
    server->config = config;
    server->config.hex_m_host = nullptr;
    server->router = router;
    server->app = app;
    server->listener = listened.listener;
    atomic_fetch_add(&router->attached, 1);
    return (hex_http_server_result){ .ok = true, .server = server };
}

// hex_http_record_failure keeps the first runtime failure for run and wait.
static void hex_http_record_failure(struct hex_http_server_state *server, hex_t_ErrorKind kind, const hex_string *message) {
    hex_mutex_lock(server->lock);
    if (!server->failed) {
        server->failed = true;
        server->failure_kind = kind;
        server->failure_message = message;
    }
    hex_mutex_unlock(server->lock);
}

// hex_http_close_listener closes the listener exactly once, whichever of stop
// and run gets there first.
static void hex_http_close_listener(struct hex_http_server_state *server) {
    hex_mutex_lock(server->lock);
    bool close_now = !server->listener_closed;
    server->listener_closed = true;
    hex_mutex_unlock(server->lock);
    if (close_now) {
        (void)hex_tcp_listener_close(server->listener);
    }
}

void hex_http_server_stop_raw(hex_http_server server) {
    atomic_store(&server->stopping, true);
    hex_http_close_listener(server);
}

// hex_http_next_to_close returns the registered connection shutdown should
// close next, marking it so no connection is closed twice. only_idle selects
// connections waiting between requests; the forced pass takes every one.
static bool hex_http_next_to_close(struct hex_http_server_state *server, bool only_idle, hex_tcp_connection *out) {
    bool found = false;
    hex_mutex_lock(server->lock);
    for (hex_http_connection *connection = server->connections; connection != nullptr; connection = connection->next) {
        if (connection->stop_closed || (only_idle && !atomic_load(&connection->idle))) {
            continue;
        }
        connection->stop_closed = true;
        *out = connection->tcp;
        found = true;
        break;
    }
    hex_mutex_unlock(server->lock);
    return found;
}

static size_t hex_http_active(struct hex_http_server_state *server) {
    hex_mutex_lock(server->lock);
    size_t active = server->active;
    hex_mutex_unlock(server->lock);
    return active;
}

// hex_http_shutdown stops admission, closes idle connections at once, lets
// in-flight requests finish until the shutdown deadline, then closes the rest.
// It returns only when every connection Task has finished and released its
// storage, so no native operation or buffer outlives run.
static void hex_http_shutdown(struct hex_http_server_state *server) {
    atomic_store(&server->stopping, true);
    hex_http_close_listener(server);
    uint64_t deadline = hex_http_nonzero(hex_http_deadline(server->config.hex_m_shutdown_timeout));
    bool forced = false;
    for (;;) {
        hex_tcp_connection victim;
        while (hex_http_next_to_close(server, !forced, &victim)) {
            (void)hex_tcp_close(victim);
        }
        if (hex_http_active(server) == 0) {
            return;
        }
        if (!forced && hex_instant_now() >= deadline) {
            forced = true;
            continue;
        }
        hex_task_sleep(HEX_HTTP_POLL_NANOSECONDS);
    }
}

hex_http_status_result hex_http_server_run_raw(hex_http_server server) {
    hex_mutex_lock(server->lock);
    if (server->phase != HEX_HTTP_SERVER_CREATED) {
        hex_mutex_unlock(server->lock);
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_already_run);
    }
    server->phase = HEX_HTTP_SERVER_RUNNING;
    hex_mutex_unlock(server->lock);

    while (!atomic_load(&server->stopping)) {
        // At the connection ceiling accepting pauses and the kernel backlog
        // absorbs arrivals; a finishing connection returns its slot.
        if (hex_http_active(server) >= server->config.hex_m_max_connections) {
            hex_task_sleep(HEX_HTTP_POLL_NANOSECONDS);
            continue;
        }
        hex_tcp_accept_result accepted = hex_tcp_accept(server->listener);
        if (accepted.status == HEX_NETWORK_CLOSED) {
            break;
        }
        if (accepted.status != 0) {
            hex_t_ErrorKind kind;
            const hex_string *message;
            hex_http_status_error(accepted.status, &hex_http_message_accept_failed, &kind, &message);
            hex_http_record_failure(server, kind, message);
            break;
        }
        if (server->config.hex_m_tcp_nodelay) {
            (void)hex_tcp_no_delay(accepted.connection, true);
        }
        hex_http_connection *connection = hex_http_connection_new(server, accepted.connection);
        if (connection == nullptr) {
            (void)hex_tcp_close(accepted.connection);
            continue;
        }
        hex_mutex_lock(server->lock);
        connection->next = server->connections;
        if (server->connections != nullptr) {
            server->connections->previous = connection;
        }
        server->connections = connection;
        server->active++;
        hex_mutex_unlock(server->lock);
        hex_task *task = hex_task_spawn(hex_http_connection_entry, sizeof(connection), alignof(hex_http_connection *), &connection, 0, 0);
        if (task == nullptr) {
            hex_mutex_lock(server->lock);
            if (connection->previous != nullptr) {
                connection->previous->next = connection->next;
            } else {
                server->connections = connection->next;
            }
            if (connection->next != nullptr) {
                connection->next->previous = connection->previous;
            }
            server->active--;
            hex_mutex_unlock(server->lock);
            (void)hex_tcp_close(accepted.connection);
            hex_http_connection_free(connection);
            continue;
        }
        hex_task_detach(task);
    }
    hex_http_shutdown(server);
    hex_mutex_lock(server->lock);
    server->phase = HEX_HTTP_SERVER_DONE;
    hex_http_status_result result = (hex_http_status_result){ .ok = !server->failed, .kind = server->failure_kind, .message = server->failure_message };
    hex_mutex_unlock(server->lock);
    hex_chan_close(server->done);
    return result;
}

hex_http_status_result hex_http_server_wait_raw(hex_http_server server) {
    hex_mutex_lock(server->lock);
    int phase = server->phase;
    hex_mutex_unlock(server->lock);
    if (phase == HEX_HTTP_SERVER_CREATED) {
        return hex_http_status_failure(hex_http_kind_invalid_input(), &hex_http_message_not_started);
    }
    uint8_t token;
    while (hex_chan_receive(server->done, &token)) {
    }
    hex_mutex_lock(server->lock);
    hex_http_status_result result = (hex_http_status_result){ .ok = !server->failed, .kind = server->failure_kind, .message = server->failure_message };
    hex_mutex_unlock(server->lock);
    return result;
}

void hex_http_server_free_raw(hex_http_server server, hex_heap h) {
    (void)h;
    hex_mutex_lock(server->lock);
    int phase = server->phase;
    hex_mutex_unlock(server->lock);
    if (phase == HEX_HTTP_SERVER_RUNNING) {
        hex_runtime_trap("[Runtime Error] server freed while running\n");
    }
    hex_http_close_listener(server);
    atomic_fetch_sub(&server->router->attached, 1);
    hex_mutex_free(server->lock);
    // A server that never ran still holds its open completion channel, and
    // closing a closed one is a no-op.
    hex_chan_close(server->done);
    hex_chan_free(server->done);
    hex_heap_free(server);
}
{{end}}
