/* Native cases for the private HTTP/1 parser adapter. The program links only
   hexal/http.c and the pinned llhttp archive: no libuv, scheduler, or Hexal
   runtime, which is itself part of what it demonstrates. */
#include "hexal/http.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

static int failures;
static int checks;

#define CHECK(condition)                                                           \
    do {                                                                           \
        checks++;                                                                  \
        if (!(condition)) {                                                        \
            failures++;                                                            \
            printf("FAIL %s:%d: %s\n", __FILE__, __LINE__, #condition);            \
        }                                                                          \
    } while (0)

enum {
    FIELD_CAPACITY = 16,
    HEAD_CAPACITY = 4096,
    BODY_CAPACITY = 4096,
};

static hex_http_limits default_limits(void) {
    return (hex_http_limits){
        .max_request_line_bytes = 128,
        .max_header_bytes = 512,
        .max_header_count = 8,
        .max_trailer_bytes = 64,
        .max_body_bytes = 1024,
    };
}

/* One simulated connection read loop. Input arrives in pieces of `chunk` bytes
   copied into a scratch buffer that is scribbled after every feed, so any
   retained pointer into the input would corrupt a field. */
typedef struct result {
    int heads;
    int messages;
    int errors;
    hex_http_error error;
    unsigned status;
    char method[32];
    char target[256];
    char path[256];
    char query[256];
    bool has_query;
    int fields;
    char names[FIELD_CAPACITY][64];
    char values[FIELD_CAPACITY][256];
    uint8_t body[BODY_CAPACITY];
    size_t body_length;
    int version_minor;
    bool chunked;
    bool has_content_length;
    unsigned long long content_length;
    bool expect_continue;
    bool close_token;
    bool keep_alive_token;
    bool boundary_after;
    size_t first_head_consumed;
    hex_http_target_form form;
    /* Method and target of the head that began the last message. */
    char last_target[256];
    int pipelined;
} result;

static void snapshot(const hex_http_head *head, result *out) {
    snprintf(out->method, sizeof(out->method), "%.*s", (int)head->method_length, head->bytes + head->method_offset);
    snprintf(out->target, sizeof(out->target), "%.*s", (int)head->target_length, head->bytes + head->target_offset);
    snprintf(out->path, sizeof(out->path), "%.*s", (int)head->path_length, head->bytes + head->path_offset);
    snprintf(out->query, sizeof(out->query), "%.*s", (int)head->query_length, head->bytes + head->query_offset);
    out->has_query = head->has_query;
    out->fields = (int)head->field_count;
    for (size_t index = 0; index < head->field_count && index < FIELD_CAPACITY; index++) {
        const hex_http_field *field = &head->fields[index];
        snprintf(out->names[index], sizeof(out->names[index]), "%.*s", (int)field->name_length, head->bytes + field->name_offset);
        snprintf(out->values[index], sizeof(out->values[index]), "%.*s", (int)field->value_length, head->bytes + field->value_offset);
    }
    out->version_minor = head->version_minor;
    out->chunked = head->chunked;
    out->has_content_length = head->has_content_length;
    out->content_length = head->content_length;
    out->expect_continue = head->expect_continue;
    out->close_token = head->connection_close;
    out->keep_alive_token = head->connection_keep_alive;
    out->form = head->target_form;
}

/* run feeds wire in pieces of chunk bytes and drives the server-shaped
   sequence: head, continue, body, message complete, next. */
static void run_with(const hex_http_limits *limits, const char *wire, size_t wire_length, size_t chunk, bool eof, result *out) {
    memset(out, 0, sizeof(*out));
    uint8_t head_bytes[HEAD_CAPACITY];
    hex_http_field fields[FIELD_CAPACITY];
    hex_http_head head = {
        .bytes = head_bytes,
        .capacity = limits->max_request_line_bytes + limits->max_header_bytes,
        .fields = fields,
        .field_capacity = limits->max_header_count < FIELD_CAPACITY ? limits->max_header_count : FIELD_CAPACITY,
    };
    hex_http_parser parser;
    hex_http_parser_init(&parser, limits, &head);

    /* pending holds bytes read but not yet consumed, like a receive buffer. */
    uint8_t pending[8192];
    size_t pending_length = 0;
    size_t read_position = 0;
    bool in_body = false;
    bool must_feed = false;
    size_t guard = 0;
    for (;;) {
        if (++guard > 100000) {
            printf("FAIL runaway loop\n");
            failures++;
            return;
        }
        if (pending_length == 0 && !must_feed) {
            if (read_position >= wire_length) {
                break;
            }
            size_t take = wire_length - read_position < chunk ? wire_length - read_position : chunk;
            memcpy(pending, wire + read_position, take);
            read_position += take;
            pending_length = take;
        }
        must_feed = false;
        uint8_t scratch[8192];
        size_t feed = pending_length;
        uint8_t body_out[BODY_CAPACITY];
        if (in_body && feed > sizeof(body_out)) {
            feed = sizeof(body_out);
        }
        memcpy(scratch, pending, feed);
        hex_http_step step = hex_http_parser_feed(&parser, scratch, feed, body_out, sizeof(body_out));
        memset(scratch, 0xAA, sizeof(scratch));
        if (step.produced > 0 && out->body_length + step.produced <= BODY_CAPACITY) {
            memcpy(out->body + out->body_length, body_out, step.produced);
            out->body_length += step.produced;
        }
        size_t consumed = step.consumed;
        if (consumed > feed) {
            printf("FAIL consumed beyond input\n");
            failures++;
            return;
        }
        memmove(pending, pending + consumed, pending_length - consumed);
        pending_length -= consumed;
        switch (step.state) {
        case HEX_HTTP_INCOMPLETE:
            break;
        case HEX_HTTP_HEAD_READY:
            out->heads++;
            if (out->heads == 1) {
                out->first_head_consumed = consumed;
                snapshot(&head, out);
            } else {
                out->pipelined++;
                snprintf(out->last_target, sizeof(out->last_target), "%.*s", (int)head.target_length, head.bytes + head.target_offset);
            }
            hex_http_parser_continue(&parser);
            in_body = true;
            must_feed = true;
            break;
        case HEX_HTTP_MESSAGE_COMPLETE:
            out->messages++;
            in_body = false;
            hex_http_parser_next(&parser);
            break;
        case HEX_HTTP_ERROR:
            out->errors++;
            out->error = step.error;
            out->status = step.status;
            return;
        }
    }
    out->boundary_after = hex_http_parser_at_boundary(&parser);
    (void)eof;
}

static void run(const char *wire, size_t chunk, result *out) {
    hex_http_limits limits = default_limits();
    run_with(&limits, wire, strlen(wire), chunk, true, out);
}

static void expect_error(const char *label, const char *wire, unsigned status) {
    for (size_t chunk = 1; chunk <= 7; chunk += 6) {
        result r;
        run(wire, chunk, &r);
        if (r.errors != 1 || r.status != status) {
            failures++;
            printf("FAIL %s (chunk %zu): errors=%d status=%u want %u\n", label, chunk, r.errors, r.status, status);
        }
        checks++;
    }
}

static void expect_error_limits(const char *label, const hex_http_limits *limits, const char *wire, unsigned status, hex_http_error error) {
    result r;
    run_with(limits, wire, strlen(wire), 5, true, &r);
    if (r.errors != 1 || r.status != status || r.error != error) {
        failures++;
        printf("FAIL %s: errors=%d status=%u error=%d want %u/%d\n", label, r.errors, r.status, r.error, status, error);
    }
    checks++;
}

static void test_split_every_byte(void) {
    const char wire[] =
        "GET /a%2Fb?x=1&y=%20 HTTP/1.1\r\nHost: example\r\nAccept: a\r\nAccept: b\r\nX-Empty:\r\nSet-Cookie: k=1\r\nSet-Cookie: j=2\r\n\r\n";
    for (size_t chunk = 1; chunk <= sizeof(wire); chunk++) {
        result r;
        run(wire, chunk, &r);
        CHECK(r.heads == 1);
        CHECK(r.messages == 1);
        CHECK(r.errors == 0);
        CHECK(strcmp(r.method, "GET") == 0);
        CHECK(strcmp(r.target, "/a%2Fb?x=1&y=%20") == 0);
        CHECK(strcmp(r.path, "/a%2Fb") == 0);
        CHECK(r.has_query && strcmp(r.query, "x=1&y=%20") == 0);
        CHECK(r.fields == 6);
        CHECK(strcmp(r.names[0], "Host") == 0 && strcmp(r.values[0], "example") == 0);
        CHECK(strcmp(r.names[1], "Accept") == 0 && strcmp(r.values[1], "a") == 0);
        CHECK(strcmp(r.names[2], "Accept") == 0 && strcmp(r.values[2], "b") == 0);
        CHECK(strcmp(r.names[3], "X-Empty") == 0 && strcmp(r.values[3], "") == 0);
        CHECK(strcmp(r.names[4], "Set-Cookie") == 0 && strcmp(r.values[4], "k=1") == 0);
        CHECK(strcmp(r.names[5], "Set-Cookie") == 0 && strcmp(r.values[5], "j=2") == 0);
        CHECK(r.form == HEX_HTTP_TARGET_ORIGIN);
        CHECK(r.boundary_after);
    }
}

static void test_head_pause_offsets(void) {
    const char wire[] = "POST /p HTTP/1.1\r\nHost: h\r\nContent-Length: 5\r\n\r\nhelloGET /q HTTP/1.1\r\nHost: h\r\n\r\n";
    result r;
    run(wire, sizeof(wire), &r);
    /* The first head ends at the blank line, before the body byte. */
    const char head_text[] = "POST /p HTTP/1.1\r\nHost: h\r\nContent-Length: 5\r\n\r\n";
    CHECK(r.first_head_consumed == sizeof(head_text) - 1);
    CHECK(r.heads == 2);
    CHECK(r.messages == 2);
    CHECK(r.body_length == 5 && memcmp(r.body, "hello", 5) == 0);
    CHECK(strcmp(r.last_target, "/q") == 0);
    CHECK(r.has_content_length && r.content_length == 5);
    /* The same pair, delivered a byte at a time, splits the second head. */
    run(wire, 1, &r);
    CHECK(r.heads == 2 && r.messages == 2);
    CHECK(r.body_length == 5 && memcmp(r.body, "hello", 5) == 0);
    CHECK(strcmp(r.last_target, "/q") == 0);
    for (size_t chunk = 2; chunk < 40; chunk += 3) {
        run(wire, chunk, &r);
        CHECK(r.heads == 2 && r.messages == 2 && r.errors == 0);
        CHECK(r.body_length == 5);
    }
}

static void test_chunked_bodies(void) {
    const char wire[] =
        "POST /c HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n3;ext=1\r\nabc\r\n4\r\ndefg\r\n0\r\nTrailer-X: v\r\nTrailer-Y: w\r\n\r\n";
    for (size_t chunk = 1; chunk < sizeof(wire); chunk += 4) {
        result r;
        run(wire, chunk, &r);
        CHECK(r.errors == 0);
        CHECK(r.heads == 1 && r.messages == 1);
        CHECK(r.chunked);
        CHECK(r.body_length == 7 && memcmp(r.body, "abcdefg", 7) == 0);
        /* Trailers are validated and discarded: no field was added. */
        CHECK(r.fields == 2);
        CHECK(strcmp(r.names[0], "Host") == 0 && strcmp(r.names[1], "Transfer-Encoding") == 0);
        CHECK(r.boundary_after);
    }
}

static void test_framing_rejections(void) {
    expect_error("TE with CL", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\nContent-Length: 3\r\n\r\n", 400);
    expect_error("duplicate CL differing", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 3\r\nContent-Length: 4\r\n\r\n", 400);
    expect_error("duplicate CL identical", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 3\r\nContent-Length: 3\r\n\r\nabc", 400);
    expect_error("CL combined list", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 3, 3\r\n\r\nabc", 400);
    expect_error("CL negative", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: -1\r\n\r\n", 400);
    expect_error("CL overflow", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 99999999999999999999999\r\n\r\n", 400);
    expect_error("CL non-digit", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 3x\r\n\r\n", 400);
    expect_error("space before colon", "GET / HTTP/1.1\r\nHost : h\r\n\r\n", 400);
    expect_error("obs-fold", "GET / HTTP/1.1\r\nHost: h\r\nX: a\r\n b\r\n\r\n", 400);
    expect_error("bare LF", "GET / HTTP/1.1\nHost: h\n\n", 400);
    expect_error("bare CR in value", "GET / HTTP/1.1\r\nHost: h\r\nX: a\rb\r\n\r\n", 400);
    expect_error("NUL in value", "GET / HTTP/1.1\r\nHost: h\r\nX: a\x01" "b\r\n\r\n", 400);
    expect_error("chunk size invalid", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\nzz\r\n", 400);
    expect_error("chunk size overflow", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\nFFFFFFFFFFFFFFFFF\r\n", 400);
    expect_error("unsupported coding", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: gzip\r\n\r\n", 400);
    expect_error("chunked not final", "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked, gzip\r\n\r\n", 400);
    expect_error("invalid method", "G@T / HTTP/1.1\r\nHost: h\r\n\r\n", 400);
    expect_error("missing version", "GET /\r\nHost: h\r\n\r\n", 400);
    expect_error("version 2.0", "GET / HTTP/2.0\r\nHost: h\r\n\r\n", 505);
    expect_error("version 1.2", "GET / HTTP/1.2\r\nHost: h\r\n\r\n", 505);
}

static void test_host_and_target_forms(void) {
    expect_error("missing host", "GET / HTTP/1.1\r\n\r\n", 400);
    expect_error("duplicate host", "GET / HTTP/1.1\r\nHost: a\r\nHost: b\r\n\r\n", 400);
    result r;
    run("GET / HTTP/1.0\r\n\r\n", 3, &r);
    CHECK(r.errors == 0 && r.heads == 1 && r.version_minor == 0);
    run("GET http://example.com/abs/path?q=1 HTTP/1.1\r\nHost: example.com\r\n\r\n", 5, &r);
    CHECK(r.errors == 0 && r.form == HEX_HTTP_TARGET_ABSOLUTE);
    CHECK(strcmp(r.path, "/abs/path") == 0 && r.has_query && strcmp(r.query, "q=1") == 0);
    run("OPTIONS * HTTP/1.1\r\nHost: h\r\n\r\n", 4, &r);
    CHECK(r.errors == 0 && r.form == HEX_HTTP_TARGET_ASTERISK);
    expect_error("connect", "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n", 501);
    expect_error("upgrade", "GET /ws HTTP/1.1\r\nHost: h\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n", 501);
    expect_error("expect other", "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 1\r\nExpect: 200-ok\r\n\r\n", 417);
    run("POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 1\r\nExpect: 100-Continue\r\n\r\nx", 6, &r);
    CHECK(r.errors == 0 && r.expect_continue && r.body_length == 1);
    run("GET / HTTP/1.1\r\nHost: h\r\nConnection: close\r\n\r\n", 6, &r);
    CHECK(r.close_token && !r.keep_alive_token);
    run("GET / HTTP/1.0\r\nConnection: keep-alive\r\n\r\n", 6, &r);
    CHECK(r.keep_alive_token && !r.close_token);
}

static void test_methods(void) {
    CHECK(hex_http_method_known((const uint8_t *)"GET", 3));
    CHECK(hex_http_method_known((const uint8_t *)"PATCH", 5));
    CHECK(hex_http_method_known((const uint8_t *)"M-SEARCH", 8));
    CHECK(!hex_http_method_known((const uint8_t *)"get", 3));
    CHECK(!hex_http_method_known((const uint8_t *)"FETCH", 5));
    CHECK(!hex_http_method_known((const uint8_t *)"", 0));
    result r;
    run("PROPFIND /dav HTTP/1.1\r\nHost: h\r\n\r\n", 5, &r);
    CHECK(r.errors == 0 && strcmp(r.method, "PROPFIND") == 0);
    expect_error("lowercase method", "get / HTTP/1.1\r\nHost: h\r\n\r\n", 400);
}

static void test_limits(void) {
    hex_http_limits limits = default_limits();
    char wire[2048];

    /* Request line: 128 bytes of method plus target. */
    char target[200];
    memset(target, 'a', sizeof(target) - 1);
    target[0] = '/';
    target[sizeof(target) - 1] = '\0';
    snprintf(wire, sizeof(wire), "GET %s HTTP/1.1\r\nHost: h\r\n\r\n", target);
    expect_error_limits("request line too long", &limits, wire, 414, HEX_HTTP_ERROR_REQUEST_LINE);
    /* Exactly at the limit passes: 3 method bytes plus a 125-byte target. */
    memset(target, 'a', 125);
    target[0] = '/';
    target[125] = '\0';
    snprintf(wire, sizeof(wire), "GET %s HTTP/1.1\r\nHost: h\r\n\r\n", target);
    result r;
    run_with(&limits, wire, strlen(wire), 9, true, &r);
    CHECK(r.errors == 0 && r.heads == 1);

    /* Header bytes. */
    char value[600];
    memset(value, 'v', sizeof(value) - 1);
    value[sizeof(value) - 1] = '\0';
    snprintf(wire, sizeof(wire), "GET / HTTP/1.1\r\nHost: h\r\nX: %s\r\n\r\n", value);
    expect_error_limits("header bytes", &limits, wire, 431, HEX_HTTP_ERROR_HEADERS);

    /* Header count: nine fields against a limit of eight. */
    snprintf(wire, sizeof(wire), "GET / HTTP/1.1\r\nHost: h\r\nA: 1\r\nB: 1\r\nC: 1\r\nD: 1\r\nE: 1\r\nF: 1\r\nG: 1\r\nH: 1\r\n\r\n");
    expect_error_limits("header count", &limits, wire, 431, HEX_HTTP_ERROR_HEADERS);

    /* Body: a declared length above the limit fails before the body. */
    expect_error_limits("declared body", &limits, "POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 2000\r\n\r\n", 413, HEX_HTTP_ERROR_BODY_TOO_LARGE);

    /* Chunked body above the limit fails while decoding. */
    limits.max_body_bytes = 8;
    expect_error_limits("chunked body", &limits,
                        "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nabcde\r\n5\r\nfghij\r\n0\r\n\r\n", 413, HEX_HTTP_ERROR_BODY_TOO_LARGE);
    limits = default_limits();

    /* Trailers and chunk extensions are separate bounded categories. */
    char trailer[80];
    memset(trailer, 't', sizeof(trailer) - 1);
    trailer[sizeof(trailer) - 1] = '\0';
    snprintf(wire, sizeof(wire), "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n1\r\na\r\n0\r\nX: %s\r\n\r\n", trailer);
    expect_error_limits("trailer bytes", &limits, wire, 431, HEX_HTTP_ERROR_TRAILERS);
    snprintf(wire, sizeof(wire), "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n1;e=%s\r\na\r\n0\r\n\r\n", trailer);
    expect_error_limits("extension bytes", &limits, wire, 431, HEX_HTTP_ERROR_TRAILERS);
    limits.max_header_count = 2;
    expect_error_limits("trailer count", &limits,
                        "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n1\r\na\r\n0\r\nA: 1\r\nB: 2\r\nC: 3\r\n\r\n", 431, HEX_HTTP_ERROR_TRAILERS);
    limits = default_limits();
    /* Trailers within every bound are accepted and discarded. */
    const char within[] = "POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n1;e=1\r\na\r\n0\r\nA: 1\r\n\r\n";
    run_with(&limits, within, sizeof(within) - 1, 7, true, &r);
    CHECK(r.errors == 0 && r.messages == 1 && r.body_length == 1);
}

static void test_eof(void) {
    result r;
    /* Nothing sent: an idle boundary. */
    run("", 4, &r);
    CHECK(r.boundary_after && r.heads == 0);
    /* A complete message leaves the parser at a boundary. */
    run("GET / HTTP/1.1\r\nHost: h\r\n\r\n", 4, &r);
    CHECK(r.messages == 1 && r.boundary_after);
    /* Partial head. */
    run("GET / HTT", 3, &r);
    CHECK(!r.boundary_after && r.heads == 0);
    /* Head without its blank line. */
    run("GET / HTTP/1.1\r\nHost: h\r\n", 4, &r);
    CHECK(!r.boundary_after);
    /* Fixed body short by one byte. */
    run("POST / HTTP/1.1\r\nHost: h\r\nContent-Length: 4\r\n\r\nabc", 4, &r);
    CHECK(r.heads == 1 && r.messages == 0 && r.body_length == 3 && !r.boundary_after);
    /* Chunked body missing its terminator. */
    run("POST / HTTP/1.1\r\nHost: h\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n", 4, &r);
    CHECK(r.messages == 0 && !r.boundary_after);
}

int main(void) {
    setvbuf(stdout, nullptr, _IONBF, 0);
    test_split_every_byte();
    test_head_pause_offsets();
    test_chunked_bodies();
    test_framing_rejections();
    test_host_and_target_forms();
    test_methods();
    test_limits();
    test_eof();
    if (failures != 0) {
        printf("http-parser-failed %d of %d\n", failures, checks);
        return 1;
    }
    printf("http-parser-ok %d\n", checks);
    return 0;
}
