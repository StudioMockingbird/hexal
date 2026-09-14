#ifndef HEXAL_NETWORK_H
#define HEXAL_NETWORK_H

#include "hexal.h"
#include "hexal/array.h"
#include "hexal/error.h"
#include "hexal/slice.h"
#include "hexal/string.h"
{{- if or .Dns .Tcp}}
// The common handle component's libuv ErrorKind mapper backs
// hex_network_error's fallback for every condition not classified above,
// reachable whenever a parking operation (DNS or TCP) exists.
#include "hexal/handle.h"
{{- end}}
{{- if .Dns}}
// hex_list_Address is used here only by pointer (as List<Address>'s owning
// handle); the full definition, needed by hexal/list.h's inline push/pop
// bodies for this element, lives there instead, which is why this forward
// declaration replaces a full include: hexal/list.h itself includes this
// header when an Address-element List is reachable, and a mutual #include
// would leave one side's struct incomplete when the other needs it whole.
typedef struct hex_list_Address hex_list_Address;
{{- end}}
{{- if .Tcp}}
// hex_list_UInt8 is used here only by pointer (as TcpConnection.read's
// destination); see the hex_list_Address comment above for why this stays a
// forward declaration instead of #include "hexal/list.h".
typedef struct hex_list_UInt8 hex_list_UInt8;
{{- end}}

// hex_t_Address is the protected inline network address ADT: IPv4 stores
// four network-order bytes and a host-order port; IPv6 stores sixteen
// network-order bytes, a host-order port, and a numeric scope. No Address
// value allocates.
typedef struct hex_t_Address {
    hex_tag tag;
    union {
        struct {
            hex_addr_ipv4_bytes hex_m_bytes;
            uint16_t hex_m_port;
        } IPv4;
        struct {
            hex_addr_ipv6_bytes hex_m_bytes;
            uint16_t hex_m_port;
            uint32_t hex_m_scope;
        } IPv6;
    } payload;
} hex_t_Address;

enum {
    HEX_NETWORK_INVALID_INPUT = 1,
    HEX_NETWORK_BUSY = 2,
    HEX_NETWORK_CLOSED = 3,
    HEX_NETWORK_ALLOCATION_FAILED = 4,
    HEX_NETWORK_EOS = 5,
};

typedef struct hex_address_parsed {
    int status;
    hex_t_Address address;
} hex_address_parsed;

// hex_address_parse accepts a numeric IPv4 or IPv6 literal only; it performs
// no DNS lookup. A scoped IPv6 literal accepts only a decimal numeric scope
// after '%'.
hex_address_parsed hex_address_parse(const hex_string *text, uint16_t port);
// hex_address_format emits the numeric host address without a port,
// allocated from heap; a nonzero IPv6 scope is appended as
// "%<unsigned-decimal-scope>".
const hex_string *hex_address_format(hex_t_Address address, hex_heap heap);
{{- if .Dns}}

typedef struct hex_dns_result {
    int status;
    hex_list_Address *list;
} hex_dns_result;

// hex_dns_resolve parks the calling Task. host and service reject an
// embedded NUL before submission.
hex_dns_result hex_dns_resolve(const hex_string *host, const hex_string *service, hex_heap heap);
{{- end}}
{{- if .Tcp}}

// TcpConnection and TcpListener wrap the shared generation-checked handle;
// every operation resolves it before touching native state.
typedef struct hex_tcp_connection {
    hex_handle handle;
} hex_tcp_connection;

typedef struct hex_tcp_listener {
    hex_handle handle;
} hex_tcp_listener;

typedef struct hex_tcp_connect_result {
    int status;
    hex_tcp_connection connection;
} hex_tcp_connect_result;

typedef struct hex_tcp_listen_result {
    int status;
    hex_tcp_listener listener;
} hex_tcp_listen_result;

typedef struct hex_tcp_accept_result {
    int status;
    hex_tcp_connection connection;
} hex_tcp_accept_result;

typedef struct hex_tcp_transfer {
    int status;
    size_t count;
} hex_tcp_transfer;

hex_tcp_connect_result hex_tcp_connect(hex_t_Address address);
// backlog must already be validated positive and within libuv's int range.
hex_tcp_listen_result hex_tcp_listen(hex_t_Address address, size_t backlog);
hex_tcp_accept_result hex_tcp_accept(hex_tcp_listener listener);
int hex_tcp_listener_close(hex_tcp_listener listener);
// A second concurrent read or write on one connection, or a second
// concurrent accept on one listener, returns HEX_NETWORK_BUSY without
// consuming bytes or blocking; each connection and listener admits one
// active operation of its own kind at a time.
hex_tcp_transfer hex_tcp_read(hex_tcp_connection connection, hex_list_UInt8 *into, size_t max);
int hex_tcp_write(hex_tcp_connection connection, hex_slice_UInt8 from);
int hex_tcp_shutdown(hex_tcp_connection connection);
int hex_tcp_no_delay(hex_tcp_connection connection, bool enabled);
int hex_tcp_close(hex_tcp_connection connection);
{{- end}}

hex_t_Error hex_network_error(size_t line, size_t column, int status, const hex_string *message);

#endif
