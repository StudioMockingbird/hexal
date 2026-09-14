/* Networking runtime: inline addresses, DNS resolution, and TCP over libuv.
   DNS and TCP always run inside a Task; there is no synchronous fallback
   path for a parking operation. This first implementation admits one active
   read, one active write, and one active accept at a time per connection or
   listener: a second concurrent call of the same kind returns
   HEX_NETWORK_BUSY immediately rather than joining a FIFO wait queue. */
#include "hexal/network.h"
#include "hexal/heap.h"
{{- if or .Dns .Tcp}}
#include "hexal/list.h"
#include "hexal/event.h"
{{- end}}
#include <stdckdint.h>
#include <string.h>
#include <uv.h>

// hex_address_scan_scope parses a trailing "%<decimal>" IPv6 zone id from a
// NUL-terminated copy; host receives the address portion, also
// NUL-terminated.
static bool hex_address_scan_scope(const char *text, size_t length, char *host, size_t host_size, uint32_t *scope) {
    const char *percent = memchr(text, '%', length);
    if (percent == nullptr) {
        if (length >= host_size) {
            return false;
        }
        memcpy(host, text, length);
        host[length] = '\0';
        *scope = 0;
        return true;
    }
    size_t host_length = (size_t)(percent - text);
    if (host_length >= host_size) {
        return false;
    }
    memcpy(host, text, host_length);
    host[host_length] = '\0';
    const char *digits = percent + 1;
    size_t digit_count = length - host_length - 1;
    if (digit_count == 0 || digit_count > 10) {
        return false;
    }
    uint64_t value = 0;
    for (size_t index = 0; index < digit_count; index++) {
        char c = digits[index];
        if (c < '0' || c > '9') {
            return false;
        }
        value = value * 10 + (uint64_t)(c - '0');
        if (value > UINT32_MAX) {
            return false;
        }
    }
    *scope = (uint32_t)value;
    return true;
}

hex_address_parsed hex_address_parse(const hex_string *text, uint16_t port) {
    if (memchr(text->data, 0, text->byte_length) != nullptr || text->byte_length >= 128) {
        return (hex_address_parsed){.status = HEX_NETWORK_INVALID_INPUT};
    }
    char copy[128];
    memcpy(copy, text->data, text->byte_length);
    copy[text->byte_length] = '\0';

    struct sockaddr_in v4;
    if (uv_ip4_addr(copy, 0, &v4) == 0) {
        hex_t_Address address = {.tag = 0};
        memcpy(address.payload.IPv4.hex_m_bytes.data, &v4.sin_addr, 4);
        address.payload.IPv4.hex_m_port = port;
        return (hex_address_parsed){.status = 0, .address = address};
    }
    char host[128];
    uint32_t scope = 0;
    if (hex_address_scan_scope(copy, text->byte_length, host, sizeof(host), &scope)) {
        struct sockaddr_in6 v6;
        if (uv_ip6_addr(host, 0, &v6) == 0) {
            hex_t_Address address = {.tag = 1};
            memcpy(address.payload.IPv6.hex_m_bytes.data, &v6.sin6_addr, 16);
            address.payload.IPv6.hex_m_port = port;
            address.payload.IPv6.hex_m_scope = scope;
            return (hex_address_parsed){.status = 0, .address = address};
        }
    }
    return (hex_address_parsed){.status = HEX_NETWORK_INVALID_INPUT};
}

const hex_string *hex_address_format(hex_t_Address address, hex_heap heap) {
    char text[64];
    size_t length;
    if (address.tag == 0) {
        struct sockaddr_in v4 = {.sin_family = AF_INET};
        memcpy(&v4.sin_addr, address.payload.IPv4.hex_m_bytes.data, 4);
        (void)uv_ip4_name(&v4, text, sizeof(text));
        length = strlen(text);
    } else {
        struct sockaddr_in6 v6 = {.sin6_family = AF_INET6};
        memcpy(&v6.sin6_addr, address.payload.IPv6.hex_m_bytes.data, 16);
        (void)uv_ip6_name(&v6, text, sizeof(text));
        length = strlen(text);
        if (address.payload.IPv6.hex_m_scope != 0) {
            length += (size_t)snprintf(text + length, sizeof(text) - length, "%%%u", address.payload.IPv6.hex_m_scope);
        }
    }
    return hex_string_from_bytes(heap, (const uint8_t *)text, length);
}

hex_t_Error hex_network_error(size_t line, size_t column, int status, const hex_string *message) {
    // Zero-initialized so every path -- including a toolchain's static
    // analysis that cannot fold `bool mapped = false; if (!mapped)` down to
    // an unconditional assignment when Dns and Tcp are both absent -- sees a
    // defined value; every reachable case below still assigns its own.
    hex_t_ErrorKind kind = {0};
    switch (status) {
    case HEX_NETWORK_INVALID_INPUT:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput};
        break;
    case HEX_NETWORK_BUSY:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Busy};
        break;
    case HEX_NETWORK_CLOSED:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Closed};
        break;
    case HEX_NETWORK_ALLOCATION_FAILED:
        kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted};
        break;
    default: {
        bool mapped = false;
{{- if or .Dns .Tcp}}
        mapped = hex_handle_error_kind(status, &kind);
{{- end}}
        if (!mapped) {
            hex_strand header = {0};
            static const char text[] = "network error";
            memcpy(header.data, text, sizeof(text) - 1);
            kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Other, .other_header = header};
        }
    }
    }
    return (hex_t_Error){.hex_m_line = line, .hex_m_column = column, .hex_m_kind = kind, .hex_m_message = message};
}
{{- if .Dns}}

typedef struct hex_dns_request {
    hex_event_command command;
    uv_getaddrinfo_t request;
    const char *host;
    const char *service;
    int status;
} hex_dns_request;

static void hex_dns_done(uv_getaddrinfo_t *request, int status, struct addrinfo *result) {
    hex_dns_request *dns = (hex_dns_request *)request->data;
    dns->status = status;
    request->addrinfo = result;
    hex_task_event_wake(dns->command.task);
}

static void hex_dns_start(hex_event_command *command) {
    hex_dns_request *dns = (hex_dns_request *)command;
    struct addrinfo hints = {0};
    hints.ai_family = AF_UNSPEC;
    hints.ai_socktype = SOCK_STREAM;
    dns->request.data = dns;
    int status = uv_getaddrinfo((uv_loop_t *)hex_event_loop_handle(), &dns->request, hex_dns_done, dns->host, dns->service, &hints);
    if (status < 0) {
        dns->status = status;
        hex_task_event_wake(dns->command.task);
    }
}

hex_dns_result hex_dns_resolve(const hex_string *host, const hex_string *service, hex_heap heap) {
    if (memchr(host->data, 0, host->byte_length) != nullptr || memchr(service->data, 0, service->byte_length) != nullptr) {
        return (hex_dns_result){.status = HEX_NETWORK_INVALID_INPUT};
    }
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] network operation outside a Task\n");
    }
    size_t host_size, service_size;
    if (ckd_add(&host_size, host->byte_length, 1) || ckd_add(&service_size, service->byte_length, 1)) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    char *host_copy = (char *)hex_heap_allocate(host_size);
    memcpy(host_copy, host->data, host->byte_length);
    host_copy[host->byte_length] = '\0';
    char *service_copy = (char *)hex_heap_allocate(service_size);
    memcpy(service_copy, service->data, service->byte_length);
    service_copy[service->byte_length] = '\0';

    hex_dns_request dns = {.command = {.task = task, .start = hex_dns_start}, .host = host_copy, .service = service_copy};
    hex_event_submit(task, &dns.command);

    hex_heap_free(host_copy);
    hex_heap_free(service_copy);

    if (dns.status < 0) {
        return (hex_dns_result){.status = dns.status};
    }
    hex_list_Address *list = hex_list_new_Address(heap);
    for (struct addrinfo *entry = dns.request.addrinfo; entry != nullptr; entry = entry->ai_next) {
        hex_t_Address address;
        if (entry->ai_family == AF_INET) {
            struct sockaddr_in *v4 = (struct sockaddr_in *)entry->ai_addr;
            address.tag = 0;
            memcpy(address.payload.IPv4.hex_m_bytes.data, &v4->sin_addr, 4);
            address.payload.IPv4.hex_m_port = ntohs(v4->sin_port);
        } else if (entry->ai_family == AF_INET6) {
            struct sockaddr_in6 *v6 = (struct sockaddr_in6 *)entry->ai_addr;
            address.tag = 1;
            memcpy(address.payload.IPv6.hex_m_bytes.data, &v6->sin6_addr, 16);
            address.payload.IPv6.hex_m_port = ntohs(v6->sin6_port);
            address.payload.IPv6.hex_m_scope = v6->sin6_scope_id;
        } else {
            continue;
        }
        hex_list_push_Address(list, address);
    }
    uv_freeaddrinfo(dns.request.addrinfo);
    return (hex_dns_result){.status = 0, .list = list};
}
{{- end}}
{{- if .Tcp}}

// uv_buf_init counts an unsigned int, so one call transfers at most
// UINT32_MAX bytes, matching the File and IO clamp.
constexpr size_t HEX_NETWORK_MAX_REQUEST = UINT32_MAX;

// hex_tcp_control is the capability control block the handle registry pins
// for the lifetime of one connection or listener. A listener leaves
// busy_read/busy_write unused; a connection leaves pending_client and
// accept_command unused.
typedef struct hex_tcp_control {
    uv_tcp_t socket;
    bool busy_read;
    bool busy_write;
    bool busy_accept;
    // pending_client holds one already-accepted connection's control block
    // when its uv_connection_cb fires with no Task currently parked in
    // accept(); the next accept() call consumes it before submitting a new
    // wait. Both fields are touched only from the loop thread (the
    // callback) or under a park/resume boundary that never overlaps it.
    struct hex_tcp_control *pending_client;
    hex_event_command *accept_command;
} hex_tcp_control;

static hex_tcp_control *hex_tcp_control_new(void) {
    return (hex_tcp_control *)hex_heap_allocate_zeroed_or_null(sizeof(hex_tcp_control));
}

static void hex_tcp_sockaddr(hex_t_Address address, struct sockaddr_storage *out) {
    memset(out, 0, sizeof(*out));
    if (address.tag == 0) {
        struct sockaddr_in *v4 = (struct sockaddr_in *)out;
        v4->sin_family = AF_INET;
        v4->sin_port = htons(address.payload.IPv4.hex_m_port);
        memcpy(&v4->sin_addr, address.payload.IPv4.hex_m_bytes.data, 4);
        return;
    }
    struct sockaddr_in6 *v6 = (struct sockaddr_in6 *)out;
    v6->sin6_family = AF_INET6;
    v6->sin6_port = htons(address.payload.IPv6.hex_m_port);
    v6->sin6_scope_id = address.payload.IPv6.hex_m_scope;
    memcpy(&v6->sin6_addr, address.payload.IPv6.hex_m_bytes.data, 16);
}

typedef struct hex_tcp_close_request {
    hex_event_command command;
    uv_tcp_t *socket;
} hex_tcp_close_request;

static void hex_tcp_close_done(uv_handle_t *handle) {
    hex_tcp_close_request *close_request = (hex_tcp_close_request *)handle->data;
    hex_task_event_wake(close_request->command.task);
}

static void hex_tcp_close_start(hex_event_command *command) {
    hex_tcp_close_request *close_request = (hex_tcp_close_request *)command;
    close_request->socket->data = close_request;
    uv_close((uv_handle_t *)close_request->socket, hex_tcp_close_done);
}

// hex_tcp_native_close marshals uv_close onto the loop thread and parks the
// calling Task until libuv's close callback confirms the handle fully
// released. It does not free control: the handle registry frees a
// published control block itself, while an unpublished one (construction
// failure, an unclaimed accepted connection) is the caller's own to free.
static void hex_tcp_native_close(uv_tcp_t *socket) {
    hex_task *task = hex_task_current();
    hex_tcp_close_request close_request = {.command = {.task = task, .start = hex_tcp_close_start}, .socket = socket};
    hex_event_submit(task, &close_request.command);
}

typedef struct hex_tcp_connect_request {
    hex_event_command command;
    uv_connect_t request;
    hex_tcp_control *control;
    struct sockaddr_storage address;
    bool initialized;
    int status;
} hex_tcp_connect_request;

static void hex_tcp_connect_done(uv_connect_t *request, int status) {
    hex_tcp_connect_request *connect = (hex_tcp_connect_request *)request->data;
    connect->status = status;
    hex_task_event_wake(connect->command.task);
}

static void hex_tcp_connect_start(hex_event_command *command) {
    hex_tcp_connect_request *connect = (hex_tcp_connect_request *)command;
    if (uv_tcp_init((uv_loop_t *)hex_event_loop_handle(), &connect->control->socket) != 0) {
        hex_task_event_wake(connect->command.task);
        return;
    }
    connect->initialized = true;
    connect->request.data = connect;
    int status = uv_tcp_connect(&connect->request, &connect->control->socket, (const struct sockaddr *)&connect->address, hex_tcp_connect_done);
    if (status < 0) {
        connect->status = status;
        hex_task_event_wake(connect->command.task);
    }
}

hex_tcp_connect_result hex_tcp_connect(hex_t_Address address) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] network operation outside a Task\n");
    }
    hex_tcp_control *control = hex_tcp_control_new();
    if (control == nullptr) {
        return (hex_tcp_connect_result){.status = HEX_NETWORK_ALLOCATION_FAILED};
    }
    hex_handle handle = hex_handle_reserve(HEX_HANDLE_KIND_TCP_CONNECTION);
    if (handle.slot == nullptr) {
        hex_heap_free(control);
        return (hex_tcp_connect_result){.status = HEX_NETWORK_ALLOCATION_FAILED};
    }
    hex_tcp_connect_request connect = {.command = {.task = task, .start = hex_tcp_connect_start}, .control = control};
    hex_tcp_sockaddr(address, &connect.address);
    hex_event_submit(task, &connect.command);
    if (connect.status < 0) {
        hex_handle_abandon(handle);
        if (connect.initialized) {
            hex_tcp_native_close(&control->socket);
        }
        hex_heap_free(control);
        return (hex_tcp_connect_result){.status = connect.status};
    }
    hex_handle_publish(handle, control);
    return (hex_tcp_connect_result){.status = 0, .connection = {.handle = handle}};
}

typedef struct hex_tcp_listen_request {
    hex_event_command command;
    hex_tcp_control *control;
    struct sockaddr_storage address;
    size_t backlog;
    bool initialized;
    int status;
} hex_tcp_listen_request;

static void hex_tcp_connection_arrived(uv_stream_t *server, int status) {
    hex_tcp_control *listener = (hex_tcp_control *)server->data;
    if (status < 0 || listener->pending_client != nullptr) {
        // A failed incoming connection is dropped; a connection arriving
        // while one is already held unclaimed stays in libuv's own backlog.
        return;
    }
    hex_tcp_control *client_control = hex_tcp_control_new();
    if (client_control == nullptr) {
        return;
    }
    if (uv_tcp_init(server->loop, &client_control->socket) != 0 || uv_accept(server, (uv_stream_t *)&client_control->socket) != 0) {
        hex_heap_free(client_control);
        return;
    }
    listener->pending_client = client_control;
    if (listener->accept_command != nullptr) {
        hex_event_command *waiting = listener->accept_command;
        listener->accept_command = nullptr;
        hex_task_event_wake(waiting->task);
    }
}

static void hex_tcp_listen_start(hex_event_command *command) {
    hex_tcp_listen_request *listen_request = (hex_tcp_listen_request *)command;
    if (uv_tcp_init((uv_loop_t *)hex_event_loop_handle(), &listen_request->control->socket) != 0) {
        hex_task_event_wake(listen_request->command.task);
        return;
    }
    listen_request->initialized = true;
    listen_request->control->socket.data = listen_request->control;
    unsigned int flags = listen_request->address.ss_family == AF_INET6 ? UV_TCP_IPV6ONLY : 0;
    int status = uv_tcp_bind(&listen_request->control->socket, (const struct sockaddr *)&listen_request->address, flags);
    if (status == 0) {
        status = uv_listen((uv_stream_t *)&listen_request->control->socket, (int)listen_request->backlog, hex_tcp_connection_arrived);
    }
    listen_request->status = status;
    hex_task_event_wake(listen_request->command.task);
}

hex_tcp_listen_result hex_tcp_listen(hex_t_Address address, size_t backlog) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] network operation outside a Task\n");
    }
    hex_tcp_control *control = hex_tcp_control_new();
    if (control == nullptr) {
        return (hex_tcp_listen_result){.status = HEX_NETWORK_ALLOCATION_FAILED};
    }
    hex_handle handle = hex_handle_reserve(HEX_HANDLE_KIND_TCP_LISTENER);
    if (handle.slot == nullptr) {
        hex_heap_free(control);
        return (hex_tcp_listen_result){.status = HEX_NETWORK_ALLOCATION_FAILED};
    }
    hex_tcp_listen_request listen_request = {.command = {.task = task, .start = hex_tcp_listen_start}, .control = control, .backlog = backlog};
    hex_tcp_sockaddr(address, &listen_request.address);
    hex_event_submit(task, &listen_request.command);
    if (listen_request.status < 0) {
        hex_handle_abandon(handle);
        if (listen_request.initialized) {
            hex_tcp_native_close(&control->socket);
        }
        hex_heap_free(control);
        return (hex_tcp_listen_result){.status = listen_request.status};
    }
    hex_handle_publish(handle, control);
    return (hex_tcp_listen_result){.status = 0, .listener = {.handle = handle}};
}

typedef struct hex_tcp_accept_request {
    hex_event_command command;
    hex_tcp_control *listener;
} hex_tcp_accept_request;

static void hex_tcp_accept_start(hex_event_command *command) {
    hex_tcp_accept_request *accept_request = (hex_tcp_accept_request *)command;
    // Both this callback and hex_tcp_connection_arrived run only on the
    // loop thread, so there is no race between checking pending_client here
    // and a connection that arrived first setting it.
    if (accept_request->listener->pending_client != nullptr) {
        hex_task_event_wake(accept_request->command.task);
        return;
    }
    accept_request->listener->accept_command = command;
}

hex_tcp_accept_result hex_tcp_accept(hex_tcp_listener listener) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] network operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(listener.handle, HEX_HANDLE_KIND_TCP_LISTENER);
    if (lease.control == nullptr) {
        return (hex_tcp_accept_result){.status = HEX_NETWORK_CLOSED};
    }
    hex_tcp_control *control = (hex_tcp_control *)lease.control;
    if (control->busy_accept) {
        hex_handle_release(lease);
        return (hex_tcp_accept_result){.status = HEX_NETWORK_BUSY};
    }
    control->busy_accept = true;
    hex_tcp_control *client_control = control->pending_client;
    control->pending_client = nullptr;
    if (client_control == nullptr) {
        hex_tcp_accept_request accept_request = {.command = {.task = task, .start = hex_tcp_accept_start}, .listener = control};
        hex_event_submit(task, &accept_request.command);
        client_control = control->pending_client;
        control->pending_client = nullptr;
    }
    control->busy_accept = false;
    hex_handle_release(lease);
    if (client_control == nullptr) {
        return (hex_tcp_accept_result){.status = HEX_NETWORK_CLOSED};
    }
    hex_handle client_handle = hex_handle_reserve(HEX_HANDLE_KIND_TCP_CONNECTION);
    if (client_handle.slot == nullptr) {
        hex_tcp_native_close(&client_control->socket);
        hex_heap_free(client_control);
        return (hex_tcp_accept_result){.status = HEX_NETWORK_ALLOCATION_FAILED};
    }
    hex_handle_publish(client_handle, client_control);
    return (hex_tcp_accept_result){.status = 0, .connection = {.handle = client_handle}};
}

int hex_tcp_listener_close(hex_tcp_listener listener) {
    void *control_ptr = hex_handle_close_begin(listener.handle, HEX_HANDLE_KIND_TCP_LISTENER);
    if (control_ptr == nullptr) {
        return HEX_NETWORK_CLOSED;
    }
    hex_tcp_control *control = (hex_tcp_control *)control_ptr;
    if (control->pending_client != nullptr) {
        hex_tcp_native_close(&control->pending_client->socket);
        hex_heap_free(control->pending_client);
        control->pending_client = nullptr;
    }
    hex_tcp_native_close(&control->socket);
    hex_handle_close_finish(listener.handle);
    return 0;
}

typedef struct hex_tcp_read_request {
    hex_event_command command;
    hex_tcp_control *control;
    uv_buf_t buffer;
    ssize_t result;
} hex_tcp_read_request;

static void hex_tcp_read_alloc(uv_handle_t *handle, size_t suggested, uv_buf_t *buf) {
    (void)suggested;
    hex_tcp_read_request *read_request = (hex_tcp_read_request *)handle->data;
    *buf = read_request->buffer;
}

static void hex_tcp_read_cb(uv_stream_t *stream, ssize_t nread, const uv_buf_t *buf) {
    (void)buf;
    hex_tcp_read_request *read_request = (hex_tcp_read_request *)stream->data;
    if (nread == 0) {
        // Not completion: no byte and no error, keep the read armed.
        return;
    }
    uv_read_stop(stream);
    read_request->result = nread;
    hex_task_event_wake(read_request->command.task);
}

static void hex_tcp_read_start(hex_event_command *command) {
    hex_tcp_read_request *read_request = (hex_tcp_read_request *)command;
    read_request->control->socket.data = read_request;
    if (uv_read_start((uv_stream_t *)&read_request->control->socket, hex_tcp_read_alloc, hex_tcp_read_cb) != 0) {
        read_request->result = UV_EINVAL;
        hex_task_event_wake(read_request->command.task);
    }
}

hex_tcp_transfer hex_tcp_read(hex_tcp_connection connection, hex_list_UInt8 *into, size_t max) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] network operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(connection.handle, HEX_HANDLE_KIND_TCP_CONNECTION);
    if (lease.control == nullptr) {
        return (hex_tcp_transfer){.status = HEX_NETWORK_CLOSED};
    }
    hex_tcp_control *control = (hex_tcp_control *)lease.control;
    if (control->busy_read) {
        hex_handle_release(lease);
        return (hex_tcp_transfer){.status = HEX_NETWORK_BUSY};
    }
    if (max == 0) {
        hex_handle_release(lease);
        return (hex_tcp_transfer){.status = 0, .count = 0};
    }
    control->busy_read = true;
    size_t count = max > HEX_NETWORK_MAX_REQUEST ? HEX_NETWORK_MAX_REQUEST : max;
    size_t needed;
    if (ckd_add(&needed, into->length, count)) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    hex_list_reserve_at_least_UInt8(into, needed);
    hex_tcp_read_request read_request = {
        .command = {.task = task, .start = hex_tcp_read_start},
        .control = control,
        .buffer = uv_buf_init((char *)(into->data + into->length), (unsigned int)count),
    };
    hex_event_submit(task, &read_request.command);
    control->busy_read = false;
    hex_handle_release(lease);
    if (read_request.result == UV_EOF) {
        return (hex_tcp_transfer){.status = HEX_NETWORK_EOS};
    }
    if (read_request.result < 0) {
        return (hex_tcp_transfer){.status = (int)read_request.result};
    }
    into->length += (size_t)read_request.result;
    return (hex_tcp_transfer){.status = 0, .count = (size_t)read_request.result};
}

typedef struct hex_tcp_write_request {
    hex_event_command command;
    uv_write_t request;
    uv_buf_t buffer;
    hex_tcp_control *control;
    int status;
} hex_tcp_write_request;

static void hex_tcp_write_done(uv_write_t *request, int status) {
    hex_tcp_write_request *write_request = (hex_tcp_write_request *)request->data;
    write_request->status = status;
    hex_task_event_wake(write_request->command.task);
}

static void hex_tcp_write_start(hex_event_command *command) {
    hex_tcp_write_request *write_request = (hex_tcp_write_request *)command;
    write_request->request.data = write_request;
    int status = uv_write(&write_request->request, (uv_stream_t *)&write_request->control->socket, &write_request->buffer, 1, hex_tcp_write_done);
    if (status < 0) {
        write_request->status = status;
        hex_task_event_wake(write_request->command.task);
    }
}

int hex_tcp_write(hex_tcp_connection connection, hex_slice_UInt8 from) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] network operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(connection.handle, HEX_HANDLE_KIND_TCP_CONNECTION);
    if (lease.control == nullptr) {
        return HEX_NETWORK_CLOSED;
    }
    hex_tcp_control *control = (hex_tcp_control *)lease.control;
    if (control->busy_write) {
        hex_handle_release(lease);
        return HEX_NETWORK_BUSY;
    }
    control->busy_write = true;
    int status = 0;
    size_t offset = 0;
    while (offset < from.length) {
        size_t count = from.length - offset > HEX_NETWORK_MAX_REQUEST ? HEX_NETWORK_MAX_REQUEST : from.length - offset;
        hex_tcp_write_request write_request = {
            .command = {.task = task, .start = hex_tcp_write_start},
            .control = control,
            .buffer = uv_buf_init((char *)(from.data + offset), (unsigned int)count),
        };
        hex_event_submit(task, &write_request.command);
        if (write_request.status < 0) {
            status = write_request.status;
            break;
        }
        offset += count;
    }
    control->busy_write = false;
    hex_handle_release(lease);
    return status;
}

typedef struct hex_tcp_shutdown_request {
    hex_event_command command;
    uv_shutdown_t request;
    hex_tcp_control *control;
    int status;
} hex_tcp_shutdown_request;

static void hex_tcp_shutdown_done(uv_shutdown_t *request, int status) {
    hex_tcp_shutdown_request *shutdown_request = (hex_tcp_shutdown_request *)request->data;
    shutdown_request->status = status;
    hex_task_event_wake(shutdown_request->command.task);
}

static void hex_tcp_shutdown_start(hex_event_command *command) {
    hex_tcp_shutdown_request *shutdown_request = (hex_tcp_shutdown_request *)command;
    shutdown_request->request.data = shutdown_request;
    int status = uv_shutdown(&shutdown_request->request, (uv_stream_t *)&shutdown_request->control->socket, hex_tcp_shutdown_done);
    if (status < 0) {
        shutdown_request->status = status;
        hex_task_event_wake(shutdown_request->command.task);
    }
}

int hex_tcp_shutdown(hex_tcp_connection connection) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] network operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(connection.handle, HEX_HANDLE_KIND_TCP_CONNECTION);
    if (lease.control == nullptr) {
        return HEX_NETWORK_CLOSED;
    }
    hex_tcp_shutdown_request shutdown_request = {.command = {.task = task, .start = hex_tcp_shutdown_start}, .control = (hex_tcp_control *)lease.control};
    hex_event_submit(task, &shutdown_request.command);
    hex_handle_release(lease);
    return shutdown_request.status;
}

typedef struct hex_tcp_nodelay_request {
    hex_event_command command;
    hex_tcp_control *control;
    bool enabled;
    int status;
} hex_tcp_nodelay_request;

static void hex_tcp_nodelay_start(hex_event_command *command) {
    hex_tcp_nodelay_request *nodelay_request = (hex_tcp_nodelay_request *)command;
    nodelay_request->status = uv_tcp_nodelay(&nodelay_request->control->socket, nodelay_request->enabled ? 1 : 0);
    hex_task_event_wake(nodelay_request->command.task);
}

int hex_tcp_no_delay(hex_tcp_connection connection, bool enabled) {
    hex_task *task = hex_task_current();
    if (task == nullptr) {
        hex_runtime_trap("[Runtime Error] network operation outside a Task\n");
    }
    hex_handle_lease lease = hex_handle_resolve(connection.handle, HEX_HANDLE_KIND_TCP_CONNECTION);
    if (lease.control == nullptr) {
        return HEX_NETWORK_CLOSED;
    }
    hex_tcp_nodelay_request nodelay_request = {.command = {.task = task, .start = hex_tcp_nodelay_start}, .control = (hex_tcp_control *)lease.control, .enabled = enabled};
    hex_event_submit(task, &nodelay_request.command);
    hex_handle_release(lease);
    return nodelay_request.status;
}

int hex_tcp_close(hex_tcp_connection connection) {
    void *control_ptr = hex_handle_close_begin(connection.handle, HEX_HANDLE_KIND_TCP_CONNECTION);
    if (control_ptr == nullptr) {
        return HEX_NETWORK_CLOSED;
    }
    hex_tcp_control *control = (hex_tcp_control *)control_ptr;
    hex_tcp_native_close(&control->socket);
    hex_handle_close_finish(connection.handle);
    return 0;
}
{{- end}}
