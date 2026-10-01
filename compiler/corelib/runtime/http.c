/* Private HTTP/1 request parser adapter over llhttp. Parser callbacks run
   synchronously inside hex_http_parser_feed on the owning connection Task and
   record only offsets and counters into caller storage; no callback retains an
   input pointer past its own call. */
#include "hexal/http.h"
#include "llhttp.h"
#include <string.h>

static_assert(sizeof(llhttp_t) <= sizeof(((hex_http_parser *)nullptr)->llhttp_storage), "llhttp_t no longer fits hex_http_parser");
static_assert(alignof(llhttp_t) <= alignof(max_align_t), "llhttp_t alignment exceeds max_align_t");

enum {
    HEX_HTTP_PAUSE_NONE,
    HEX_HTTP_PAUSE_HEAD,
    HEX_HTTP_PAUSE_MESSAGE,
};

/* The pause kind and the in-progress field phase live beside the public
   cursor; the public record carries them as plain bytes so the header stays
   free of private enumerations. */
static inline llhttp_t *hex_http_llhttp(hex_http_parser *parser) {
    return (llhttp_t *)parser->llhttp_storage;
}

/* hex_http_fail records the first protocol failure and returns HPE_USER so
   llhttp stops with a terminal error. Later failures keep the first cause. */
static int hex_http_fail(hex_http_parser *parser, hex_http_error error, const char *reason) {
    if (parser->error == HEX_HTTP_ERROR_NONE) {
        parser->error = error;
    }
    llhttp_set_error_reason(hex_http_llhttp(parser), reason);
    return HPE_USER;
}

/* hex_http_append copies callback bytes into the head storage. The caller has
   already charged them to a limit, so the capacity check is the last defense
   against a request-line plus header limit pair that exceeds the storage. */
static bool hex_http_append(hex_http_parser *parser, const char *at, size_t length) {
    hex_http_head *head = parser->head;
    if (length > head->capacity - head->length) {
        return false;
    }
    memcpy(head->bytes + head->length, at, length);
    head->length += length;
    return true;
}

static int hex_http_on_message_begin(llhttp_t *llhttp) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    hex_http_head *head = parser->head;
    head->length = 0;
    head->field_count = 0;
    head->method_offset = head->method_length = 0;
    head->target_offset = head->target_length = 0;
    head->path_offset = head->path_length = 0;
    head->query_offset = head->query_length = 0;
    head->has_query = false;
    head->target_form = HEX_HTTP_TARGET_ORIGIN;
    head->connection_close = head->connection_keep_alive = head->connection_upgrade = false;
    head->upgrade = head->chunked = head->has_content_length = false;
    head->content_length = 0;
    head->expect_continue = head->expect_other = false;
    parser->body_bytes = 0;
    parser->request_line_bytes = 0;
    parser->header_bytes = 0;
    parser->trailer_bytes = 0;
    parser->extension_bytes = 0;
    parser->trailer_count = 0;
    parser->have_name = false;
    parser->have_value = false;
    parser->in_trailers = false;
    parser->head_done = false;
    parser->started = true;
    return 0;
}

static int hex_http_on_method(llhttp_t *llhttp, const char *at, size_t length) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    hex_http_head *head = parser->head;
    if (length == 0) {
        return 0;
    }
    if (head->method_length == 0) {
        head->method_offset = head->length;
    }
    if (length > parser->limits->max_request_line_bytes - parser->request_line_bytes) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_REQUEST_LINE, "request line too long");
    }
    parser->request_line_bytes += length;
    if (!hex_http_append(parser, at, length)) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_REQUEST_LINE, "request line too long");
    }
    head->method_length += length;
    return 0;
}

static int hex_http_on_url(llhttp_t *llhttp, const char *at, size_t length) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    hex_http_head *head = parser->head;
    if (length == 0) {
        return 0;
    }
    if (head->target_length == 0) {
        head->target_offset = head->length;
    }
    if (length > parser->limits->max_request_line_bytes - parser->request_line_bytes) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_REQUEST_LINE, "request target too long");
    }
    parser->request_line_bytes += length;
    if (!hex_http_append(parser, at, length)) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_REQUEST_LINE, "request target too long");
    }
    head->target_length += length;
    return 0;
}

/* hex_http_trailer_charge counts one trailer fragment against the trailer
   limit; trailer bytes are validated by llhttp and discarded. */
static int hex_http_trailer_charge(hex_http_parser *parser, size_t length) {
    if (length > parser->limits->max_trailer_bytes - parser->trailer_bytes) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_TRAILERS, "trailers too large");
    }
    parser->trailer_bytes += length;
    return 0;
}

static int hex_http_on_header_field(llhttp_t *llhttp, const char *at, size_t length) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    hex_http_head *head = parser->head;
    bool trailing = (llhttp->flags & F_TRAILING) != 0;
    if (!parser->have_name) {
        if (trailing) {
            if (parser->trailer_count >= parser->limits->max_header_count) {
                return hex_http_fail(parser, HEX_HTTP_ERROR_TRAILERS, "too many trailer fields");
            }
            parser->trailer_count++;
            parser->in_trailers = true;
        } else {
            if (head->field_count >= head->field_capacity || head->field_count >= parser->limits->max_header_count) {
                return hex_http_fail(parser, HEX_HTTP_ERROR_HEADERS, "too many header fields");
            }
            hex_http_field *field = &head->fields[head->field_count];
            field->name_offset = head->length;
            field->name_length = 0;
            field->value_offset = head->length;
            field->value_length = 0;
        }
        parser->have_name = true;
        parser->have_value = false;
    }
    if (parser->in_trailers) {
        return hex_http_trailer_charge(parser, length);
    }
    if (length > parser->limits->max_header_bytes - parser->header_bytes) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_HEADERS, "header fields too large");
    }
    parser->header_bytes += length;
    if (!hex_http_append(parser, at, length)) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_HEADERS, "header fields too large");
    }
    head->fields[head->field_count].name_length += length;
    return 0;
}

static int hex_http_on_header_field_complete(llhttp_t *llhttp) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    if (!parser->in_trailers && parser->have_name) {
        hex_http_head *head = parser->head;
        head->fields[head->field_count].value_offset = head->length;
    }
    return 0;
}

static int hex_http_on_header_value(llhttp_t *llhttp, const char *at, size_t length) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    hex_http_head *head = parser->head;
    parser->have_value = true;
    if (parser->in_trailers) {
        return hex_http_trailer_charge(parser, length);
    }
    if (length > parser->limits->max_header_bytes - parser->header_bytes) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_HEADERS, "header fields too large");
    }
    parser->header_bytes += length;
    if (!hex_http_append(parser, at, length)) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_HEADERS, "header fields too large");
    }
    head->fields[head->field_count].value_length += length;
    return 0;
}

static int hex_http_on_header_value_complete(llhttp_t *llhttp) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    if (!parser->in_trailers && parser->have_name) {
        parser->head->field_count++;
    }
    parser->have_name = false;
    parser->have_value = false;
    parser->in_trailers = false;
    return 0;
}

static int hex_http_on_chunk_extension(llhttp_t *llhttp, const char *at, size_t length) {
    (void)at;
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    if (length > parser->limits->max_trailer_bytes - parser->extension_bytes) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_TRAILERS, "chunk extensions too large");
    }
    parser->extension_bytes += length;
    return 0;
}

static int hex_http_on_body(llhttp_t *llhttp, const char *at, size_t length) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    if (length > parser->limits->max_body_bytes - parser->body_bytes) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_BODY_TOO_LARGE, "request body too large");
    }
    if (length > parser->out_capacity - parser->out_length) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_INTERNAL, "body output does not fit");
    }
    memcpy(parser->out + parser->out_length, at, length);
    parser->out_length += length;
    parser->body_bytes += length;
    return 0;
}

static bool hex_http_ascii_equal(const uint8_t *text, size_t length, const char *name) {
    size_t expected = strlen(name);
    if (length != expected) {
        return false;
    }
    for (size_t index = 0; index < length; index++) {
        uint8_t a = text[index];
        uint8_t b = (uint8_t)name[index];
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

bool hex_http_name_equals(const hex_http_head *head, const hex_http_field *field, const char *name) {
    return hex_http_ascii_equal(head->bytes + field->name_offset, field->name_length, name);
}

/* hex_http_value_is_continue reports whether an Expect value is exactly
   100-continue after trimming optional whitespace. */
static bool hex_http_value_is_continue(const uint8_t *value, size_t length) {
    while (length > 0 && (value[0] == ' ' || value[0] == '\t')) {
        value++;
        length--;
    }
    while (length > 0 && (value[length - 1] == ' ' || value[length - 1] == '\t')) {
        length--;
    }
    return hex_http_ascii_equal(value, length, "100-continue");
}

/* hex_http_classify_target derives the target form and the raw path and query
   spans without decoding or allocating. */
static void hex_http_classify_target(hex_http_head *head, bool connect) {
    const uint8_t *target = head->bytes + head->target_offset;
    size_t length = head->target_length;
    size_t path_start = 0;
    if (connect) {
        head->target_form = HEX_HTTP_TARGET_AUTHORITY;
        head->path_offset = head->target_offset;
        head->path_length = length;
        return;
    }
    if (length == 1 && target[0] == '*') {
        head->target_form = HEX_HTTP_TARGET_ASTERISK;
        head->path_offset = head->target_offset;
        head->path_length = 1;
        return;
    }
    if (length > 0 && target[0] != '/') {
        /* absolute-form: skip "scheme://authority". */
        head->target_form = HEX_HTTP_TARGET_ABSOLUTE;
        size_t index = 0;
        while (index + 2 < length && !(target[index] == ':' && target[index + 1] == '/' && target[index + 2] == '/')) {
            index++;
        }
        index = index + 3 <= length ? index + 3 : length;
        while (index < length && target[index] != '/' && target[index] != '?') {
            index++;
        }
        path_start = index;
    } else {
        head->target_form = HEX_HTTP_TARGET_ORIGIN;
    }
    size_t path_end = path_start;
    while (path_end < length && target[path_end] != '?') {
        path_end++;
    }
    head->path_offset = head->target_offset + path_start;
    head->path_length = path_end - path_start;
    if (path_end < length) {
        head->has_query = true;
        head->query_offset = head->target_offset + path_end + 1;
        head->query_length = length - path_end - 1;
    }
}

static int hex_http_on_headers_complete(llhttp_t *llhttp) {
    hex_http_parser *parser = (hex_http_parser *)llhttp->data;
    hex_http_head *head = parser->head;
    head->version_major = llhttp_get_http_major(llhttp);
    head->version_minor = llhttp_get_http_minor(llhttp);
    if (head->version_major == 0) {
        /* An HTTP/0.9 simple request has no version token: not an HTTP/1 request line. */
        return hex_http_fail(parser, HEX_HTTP_ERROR_MALFORMED, "missing HTTP version");
    }
    if (head->version_major != 1 || head->version_minor > 1) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_VERSION, "unsupported HTTP version");
    }
    head->method = llhttp_get_method(llhttp);
    uint16_t flags = llhttp->flags;
    head->connection_close = (flags & F_CONNECTION_CLOSE) != 0;
    head->connection_keep_alive = (flags & F_CONNECTION_KEEP_ALIVE) != 0;
    head->connection_upgrade = (flags & F_CONNECTION_UPGRADE) != 0;
    head->chunked = (flags & F_CHUNKED) != 0;
    head->has_content_length = (flags & F_CONTENT_LENGTH) != 0;
    head->content_length = head->has_content_length ? llhttp->content_length : 0;
    head->upgrade = llhttp_get_upgrade(llhttp) != 0;
    size_t hosts = 0;
    for (size_t index = 0; index < head->field_count; index++) {
        const hex_http_field *field = &head->fields[index];
        if (hex_http_name_equals(head, field, "host")) {
            hosts++;
        } else if (hex_http_name_equals(head, field, "expect")) {
            if (hex_http_value_is_continue(head->bytes + field->value_offset, field->value_length)) {
                head->expect_continue = true;
            } else {
                head->expect_other = true;
            }
        }
    }
    if (head->version_minor == 1 && hosts != 1) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_MALFORMED, "HTTP/1.1 requires exactly one Host field");
    }
    hex_http_classify_target(head, head->method == HTTP_CONNECT);
    if (head->upgrade || head->method == HTTP_CONNECT) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_UNSUPPORTED, "CONNECT and Upgrade are unsupported");
    }
    if (head->expect_other) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_EXPECTATION, "unsupported expectation");
    }
    if (head->has_content_length && head->content_length > parser->limits->max_body_bytes) {
        return hex_http_fail(parser, HEX_HTTP_ERROR_BODY_TOO_LARGE, "request body too large");
    }
    parser->head_done = true;
    return HPE_PAUSED;
}

static int hex_http_on_message_complete(llhttp_t *llhttp) {
    (void)llhttp;
    return HPE_PAUSED;
}

static const llhttp_settings_t hex_http_settings = {
    .on_message_begin = hex_http_on_message_begin,
    .on_method = hex_http_on_method,
    .on_url = hex_http_on_url,
    .on_header_field = hex_http_on_header_field,
    .on_header_value = hex_http_on_header_value,
    .on_chunk_extension_name = hex_http_on_chunk_extension,
    .on_chunk_extension_value = hex_http_on_chunk_extension,
    .on_headers_complete = hex_http_on_headers_complete,
    .on_body = hex_http_on_body,
    .on_message_complete = hex_http_on_message_complete,
    .on_header_field_complete = hex_http_on_header_field_complete,
    .on_header_value_complete = hex_http_on_header_value_complete,
};

void hex_http_parser_init(hex_http_parser *parser, const hex_http_limits *limits, hex_http_head *head) {
    memset(parser, 0, sizeof(*parser));
    parser->limits = limits;
    parser->head = head;
    llhttp_t *llhttp = hex_http_llhttp(parser);
    llhttp_init(llhttp, HTTP_REQUEST, &hex_http_settings);
    llhttp->data = parser;
    head->length = 0;
    head->field_count = 0;
}

/* hex_http_status maps a terminal llhttp error to the adapter's own error and
   the response status the server sends. The adapter's own causes, recorded by
   hex_http_fail, take precedence over llhttp's reason. */
static hex_http_step hex_http_error_step(hex_http_parser *parser, llhttp_errno_t errno_value, size_t consumed) {
    hex_http_error error = parser->error;
    if (error == HEX_HTTP_ERROR_NONE) {
        /* llhttp rejects a version number outside its table itself; the same
           errno also covers a missing CRLF after the version, which is malformed. */
        error = HEX_HTTP_ERROR_MALFORMED;
        if (errno_value == HPE_INVALID_VERSION) {
            const char *reason = llhttp_get_error_reason(hex_http_llhttp(parser));
            if (reason != nullptr && (strcmp(reason, "Invalid minor version") == 0 || strcmp(reason, "Invalid major version") == 0 || strcmp(reason, "Invalid HTTP version") == 0)) {
                error = HEX_HTTP_ERROR_VERSION;
            }
        }
    }
    uint16_t status = 0;
    switch (error) {
    case HEX_HTTP_ERROR_MALFORMED:
        status = 400;
        break;
    case HEX_HTTP_ERROR_REQUEST_LINE:
        status = 414;
        break;
    case HEX_HTTP_ERROR_HEADERS:
    case HEX_HTTP_ERROR_TRAILERS:
        status = 431;
        break;
    case HEX_HTTP_ERROR_BODY_TOO_LARGE:
        status = 413;
        break;
    case HEX_HTTP_ERROR_VERSION:
        status = 505;
        break;
    case HEX_HTTP_ERROR_EXPECTATION:
        status = 417;
        break;
    case HEX_HTTP_ERROR_UNSUPPORTED:
        status = 501;
        break;
    default:
        break;
    }
    return (hex_http_step){.state = HEX_HTTP_ERROR, .consumed = consumed, .produced = parser->out_length, .error = error, .status = status};
}

hex_http_step hex_http_parser_feed(hex_http_parser *parser, const uint8_t *input, size_t length, uint8_t *out, size_t out_capacity) {
    static const uint8_t empty[1] = {0};
    if (input == nullptr) {
        input = empty;
    }
    parser->out = out;
    parser->out_capacity = out_capacity;
    parser->out_length = 0;
    llhttp_t *llhttp = hex_http_llhttp(parser);
    llhttp_errno_t result = llhttp_execute(llhttp, (const char *)input, length);
    if (result == HPE_OK) {
        return (hex_http_step){.state = HEX_HTTP_INCOMPLETE, .consumed = length, .produced = parser->out_length};
    }
    size_t position = (size_t)((const uint8_t *)llhttp_get_error_pos(llhttp) - input);
    if (result == HPE_PAUSED) {
        hex_http_state state = parser->head_done ? HEX_HTTP_HEAD_READY : HEX_HTTP_MESSAGE_COMPLETE;
        return (hex_http_step){.state = state, .consumed = position, .produced = parser->out_length};
    }
    return hex_http_error_step(parser, result, position);
}

void hex_http_parser_continue(hex_http_parser *parser) {
    parser->head_done = false;
    llhttp_resume(hex_http_llhttp(parser));
}

void hex_http_parser_next(hex_http_parser *parser) {
    parser->started = false;
    parser->head_done = false;
    llhttp_resume(hex_http_llhttp(parser));
}

bool hex_http_parser_at_boundary(const hex_http_parser *parser) {
    return !parser->started;
}

bool hex_http_method_known(const uint8_t *name, size_t length) {
    for (int method = 0; method <= HTTP_QUERY; method++) {
        const char *candidate = llhttp_method_name((llhttp_method_t)method);
        if (candidate != nullptr && strlen(candidate) == length && memcmp(candidate, name, length) == 0) {
            return true;
        }
    }
    return false;
}
