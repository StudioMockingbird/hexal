#ifndef HEXAL_HTTP_H
#define HEXAL_HTTP_H

#include <stddef.h>
#include <stdint.h>

/* Private HTTP/1 request parser adapter. It wraps one caller-owned llhttp
   state per connection and exposes no llhttp type. The adapter has no libuv,
   Task, or Hexal dependency and performs no dynamic allocation: every byte it
   retains lives in storage the caller supplies, and the caller feeds it only
   from the owning connection Task. */

/* hex_http_limits are the protocol bounds the adapter enforces. The caller
   owns the record; it must outlive the parser. A read-buffer chunk size is not
   a limit and never appears here. */
typedef struct hex_http_limits {
    size_t max_request_line_bytes; /* method plus request-target bytes */
    size_t max_header_bytes;       /* field-name plus field-value bytes of the head */
    size_t max_header_count;       /* header fields of the head */
    size_t max_trailer_bytes;      /* trailer bytes and chunk-extension bytes, each counted per message */
    uint64_t max_body_bytes;       /* decoded request-body bytes */
} hex_http_limits;

/* hex_http_field locates one header field inside hex_http_head.bytes. */
typedef struct hex_http_field {
    size_t name_offset;
    size_t name_length;
    size_t value_offset;
    size_t value_length;
} hex_http_field;

typedef enum hex_http_target_form : uint8_t {
    HEX_HTTP_TARGET_ORIGIN,    /* /path?query */
    HEX_HTTP_TARGET_ABSOLUTE,  /* scheme://authority/path?query */
    HEX_HTTP_TARGET_AUTHORITY, /* CONNECT host:port */
    HEX_HTTP_TARGET_ASTERISK,  /* OPTIONS * */
} hex_http_target_form;

/* hex_http_head is the retained request head. The caller supplies bytes and
   fields; the adapter packs the method, the request-target, then every field
   name and value into bytes in arrival order and records offsets, so a field
   split across callbacks or reads needs no per-field allocation. Everything
   here is valid from HEX_HTTP_HEAD_READY until the next hex_http_parser_next. */
typedef struct hex_http_head {
    uint8_t *bytes;
    size_t capacity; /* max_request_line_bytes + max_header_bytes */
    size_t length;
    hex_http_field *fields;
    size_t field_capacity; /* max_header_count */
    size_t field_count;

    size_t method_offset;
    size_t method_length;
    size_t target_offset;
    size_t target_length;
    /* The raw request-target path without the query, and the raw query bytes
       after '?'. Both are offsets into bytes and keep percent-encoding. */
    size_t path_offset;
    size_t path_length;
    size_t query_offset;
    size_t query_length;
    bool has_query;
    hex_http_target_form target_form;

    uint8_t method; /* the pinned llhttp method id */
    uint8_t version_major;
    uint8_t version_minor;

    /* Framing and connection facts. The adapter reports; the server decides
       persistence and shutdown. */
    bool connection_close;
    bool connection_keep_alive;
    bool connection_upgrade;
    bool upgrade; /* Upgrade with Connection: upgrade, or CONNECT */
    bool chunked;
    bool has_content_length;
    uint64_t content_length;
    bool expect_continue; /* an Expect field whose only value is 100-continue */
    bool expect_other;    /* any other Expect value */
} hex_http_head;

typedef enum hex_http_state : uint8_t {
    HEX_HTTP_INCOMPLETE,       /* every input byte was consumed; more are needed */
    HEX_HTTP_HEAD_READY,       /* the head is complete; consumed counts through its final CRLF */
    HEX_HTTP_MESSAGE_COMPLETE, /* the whole message is consumed; paused until hex_http_parser_next */
    HEX_HTTP_ERROR,
} hex_http_state;

typedef enum hex_http_error : uint8_t {
    HEX_HTTP_ERROR_NONE,
    HEX_HTTP_ERROR_MALFORMED,        /* 400: syntax, framing, Host, or Content-Length failure */
    HEX_HTTP_ERROR_REQUEST_LINE,     /* 414: method plus request-target exceed the limit */
    HEX_HTTP_ERROR_HEADERS,          /* 431: header bytes or count exceed the limit */
    HEX_HTTP_ERROR_BODY_TOO_LARGE,   /* 413: decoded body exceeds the limit */
    HEX_HTTP_ERROR_TRAILERS,         /* 431: trailer or chunk-extension bytes or count exceed the limit */
    HEX_HTTP_ERROR_VERSION,          /* 505: not HTTP/1.0 or HTTP/1.1 */
    HEX_HTTP_ERROR_EXPECTATION,      /* 417: unsupported Expect value */
    HEX_HTTP_ERROR_UNSUPPORTED,      /* 501: CONNECT or Upgrade */
    HEX_HTTP_ERROR_TRUNCATED,        /* the stream ended inside a message */
    HEX_HTTP_ERROR_INTERNAL,         /* a body step could not fit its output; a caller defect */
} hex_http_error;

/* hex_http_step is one feed's outcome. On HEX_HTTP_ERROR, consumed is the
   input position of the failure and status is the response code the server
   should send (0 for a truncated stream or an internal failure, which send
   none). */
typedef struct hex_http_step {
    hex_http_state state;
    size_t consumed;
    size_t produced;
    hex_http_error error;
    uint16_t status;
} hex_http_step;

/* The parser owns only llhttp's state and the cursor of the head it fills.
   llhttp_storage is sized in http.c by a static assertion against the pinned
   llhttp_t. */
typedef struct hex_http_parser {
    _Alignas(max_align_t) unsigned char llhttp_storage[120];
    const hex_http_limits *limits;
    hex_http_head *head;
    uint8_t *out;
    size_t out_capacity;
    size_t out_length;
    uint64_t body_bytes;
    size_t request_line_bytes;
    size_t header_bytes;
    size_t trailer_bytes;
    size_t extension_bytes;
    size_t trailer_count;
    size_t current_name;
    size_t current_value;
    bool have_name;
    bool have_value;
    bool in_trailers;
    bool started;     /* a message began since the last init or next */
    bool head_done;
    bool resumed_body;
    hex_http_error error;
} hex_http_parser;

/* hex_http_parser_init prepares one parser over caller-owned limits and head
   storage. It reuses no state of an earlier message. */
void hex_http_parser_init(hex_http_parser *parser, const hex_http_limits *limits, hex_http_head *head);

/* hex_http_parser_feed consumes input. Before the head is complete it fills
   the head and returns HEX_HTTP_HEAD_READY at the final CRLF of the head,
   without consuming the body. After hex_http_parser_continue it decodes body
   bytes into out, at most out_capacity of them, and returns
   HEX_HTTP_MESSAGE_COMPLETE after the last one. Callers keep length at most
   out_capacity in the body phase: decoded bytes never outnumber the raw bytes
   that produced them, so the output always fits. Input is never retained;
   every offset the parser keeps points into head storage. */
hex_http_step hex_http_parser_feed(hex_http_parser *parser, const uint8_t *input, size_t length, uint8_t *out, size_t out_capacity);

/* hex_http_parser_continue ends the pause at head completion so the next feed
   delivers body bytes or the message-complete step. */
void hex_http_parser_continue(hex_http_parser *parser);

/* hex_http_parser_next ends the pause at message completion, clears the
   retained head, and readies the parser for the next pipelined message. */
void hex_http_parser_next(hex_http_parser *parser);

/* hex_http_parser_at_boundary reports whether no message is in progress, so
   an end of stream here is an ordinary idle close and anywhere else is a
   truncated message. */
bool hex_http_parser_at_boundary(const hex_http_parser *parser);

/* hex_http_method_known reports whether name is in the pinned llhttp method
   set; method tokens are case-sensitive. */
bool hex_http_method_known(const uint8_t *name, size_t length);

/* hex_http_name_equals compares one retained field name with an ASCII name,
   ignoring ASCII case. */
bool hex_http_name_equals(const hex_http_head *head, const hex_http_field *field, const char *name);

#endif
