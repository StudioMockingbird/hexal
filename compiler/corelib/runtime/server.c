#include "server.h"
{{if .NeedParser}}#include "hexal/http.h"
{{end}}#include <stdckdint.h>
#include <string.h>

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
// depends on caller string storage.
typedef struct hex_http_route {
    uint8_t *method;
    size_t method_length;
    uint8_t *path;
    size_t path_length;
    hex_http_handler handler;
} hex_http_route;

struct hex_http_router_state {
    hex_http_route *routes;
    size_t count;
    size_t capacity;
};

hex_http_router hex_http_router_new_raw(hex_heap h) {
    (void)h;
    return hex_heap_allocate_zeroed(1, sizeof(struct hex_http_router_state));
}
{{end}}
{{if .NeedRoute}}
static hex_http_status_result hex_http_route_failure(hex_t_ErrorKind kind, const char *text) {
    const hex_string *message = hex_string_make((hex_heap)0,
        (hex_text){ .data = (const uint8_t *)text, .length = strlen(text) });
    return (hex_http_status_result){ .ok = false, .kind = kind, .message = message };
}

static hex_t_ErrorKind hex_http_kind_invalid_input(void) {
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_InvalidInput };
}

static hex_t_ErrorKind hex_http_kind_resource(void) {
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_ResourceExhausted };
}

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

hex_http_status_result hex_http_router_route_raw(hex_http_router router, const hex_string *method, const hex_string *path, hex_http_handler handler) {
    if (!hex_http_method_known(method->data, method->byte_length)) {
        return hex_http_route_failure(hex_http_kind_invalid_input(), "unknown HTTP method");
    }
    if (!hex_http_path_valid(path)) {
        return hex_http_route_failure(hex_http_kind_invalid_input(), "route path must be an absolute path without query, fragment, or control bytes");
    }
    if (hex_http_route_exists(router, method, path)) {
        return hex_http_route_failure(hex_http_kind_invalid_input(), "duplicate method and path route");
    }
    if (!hex_http_reserve_route(router)) {
        return hex_http_route_failure(hex_http_kind_resource(), "route table ran out of memory");
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
        return hex_http_route_failure(hex_http_kind_resource(), "route table ran out of memory");
    }
    router->routes[router->count++] = (hex_http_route){
        .method = method_copy,
        .method_length = method->byte_length,
        .path = path_copy,
        .path_length = path->byte_length,
        .handler = handler,
    };
    return (hex_http_status_result){ .ok = true };
}
{{end}}
{{if .NeedFree}}
void hex_http_router_free_raw(hex_http_router router, hex_heap h) {
    (void)h;
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
