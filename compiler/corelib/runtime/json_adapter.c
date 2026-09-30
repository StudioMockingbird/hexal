#define HEX_JSON_MAX_DEPTH {{.Depth}}
#include "json.h"

{{if .Adapter}}
// The only generated translation unit that includes <yyjson.h>. Every byte it
// accepts or produces runs on the caller's Heap through the custom allocator
// below; no C allocator participates. Neither entry leaks a byte on any path;
// no yyjson type, flag, or code crosses this unit's boundary.

// The disables are value macros: yyjson's preprocessor tests them with !.
#ifndef YYJSON_DISABLE_FILE
#define YYJSON_DISABLE_FILE 1
#endif
#ifndef YYJSON_DISABLE_INCR_READER
#define YYJSON_DISABLE_INCR_READER 1
#endif
#ifndef YYJSON_DISABLE_UTILS
#define YYJSON_DISABLE_UTILS 1
#endif
#include <yyjson.h>
#include <string.h>
#include <math.h>
#include <stdio.h>
#include "hexal/list.h"

static void *hex_yyjson_malloc(void *ctx, size_t size) {
    (void)ctx;
    return hex_heap_allocate_or_null(size);
}

// Realloc preserves min(old, new) bytes on grow and shrink, treats a nil and
// zero-size input per the boundary contract, and leaves the original
// allocation valid when the replacement fails: no path loses the only live
// allocation.
static void *hex_yyjson_realloc(void *ctx, void *ptr, size_t old_size, size_t new_size) {
    (void)ctx;
    if (ptr == NULL) {
        return hex_heap_allocate_or_null(new_size);
    }
    if (new_size == 0) {
        hex_heap_free(ptr);
        return NULL;
    }
    void *replacement = hex_heap_allocate_or_null(new_size);
    if (replacement == NULL) {
        return NULL;
    }
    memcpy(replacement, ptr, old_size < new_size ? old_size : new_size);
    hex_heap_free(ptr);
    return replacement;
}

static void hex_yyjson_free(void *ctx, void *ptr) {
    (void)ctx;
    if (ptr != NULL) {
        hex_heap_free(ptr);
    }
}

static yyjson_alc hex_yyjson_alc(void) {
    return (yyjson_alc){
        .malloc = hex_yyjson_malloc,
        .realloc = hex_yyjson_realloc,
        .free = hex_yyjson_free,
        .ctx = NULL,
    };
}

// The read flags accept RFC 8259 with JSONC's two deliberate extensions:
// C-style comments and one trailing comma. BIGNUM_AS_RAW keeps every integer
// outside Int64/UInt64 and every literal outside finite Float64 as a typed
// RAW value instead of a read failure, so the range rows classify the lexeme
// instead of yyjson's number code. No other extension is admitted.
static const int hex_json_read_flags =
    YYJSON_READ_ALLOW_COMMENTS | YYJSON_READ_ALLOW_TRAILING_COMMAS |
    YYJSON_READ_BIGNUM_AS_RAW;
static const int hex_json_write_flags = 0;

// The table-owned constant messages. The yyjson detail message plus the
// computed location is the only message composed at run time.
static const char hex_json_oom_message[] = "JSON parsing ran out of memory";
static const char hex_json_impossible_message[] = "JSON parser reported an impossible state";
static const char hex_json_depth_message[] = "JSON value nests too deeply";
static const char hex_json_cycle_message[] = "JSON value contains a cycle";
static const char hex_json_nonfinite_message[] = "JSON cannot represent a non-finite number";
static const char hex_json_integer_range_message[] = "JSON integer is out of range";
static const char hex_json_number_range_message[] = "JSON number is out of range";

static hex_t_ErrorKind hex_json_kind_invalid_input(void) {
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_InvalidInput };
}

static hex_t_ErrorKind hex_json_kind_resource(void) {
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_ResourceExhausted };
}

// The table's Unknown Error row: Other carries the fixed classification
// header, so the wrapper prints the kind itself when no message fits.
static hex_t_ErrorKind hex_json_kind_unknown(void) {
    static const char text[] = "Unknown Error";
    hex_string_128 header = { .byte_length = sizeof(text) - 1 };
    memcpy(header.data, text, sizeof(text) - 1);
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_Other, .other_header = header };
}

static const hex_string *hex_json_message(hex_heap h, const char *text) {
    return hex_string_make(h,
        (hex_text){ .data = (const uint8_t *)text, .length = strlen(text) });
}

// hex_json_translate carries one subtree's translation: the tree beyond the
// recursion, otherwise the stable kind and the owned-message text the
// wrapper wraps in the caller's Error. Both failure fields are set together,
// so the caller never sees a kind without its message.
typedef struct hex_json_translate {
    hex_heap h;
    bool ok;
    hex_t_JsonValue value;
    hex_t_ErrorKind kind;
    const char *message;
} hex_json_translate;

static void hex_json_translate_fail(hex_json_translate *out, hex_t_ErrorKind kind, const char *message) {
    out->ok = false;
    out->kind = kind;
    out->message = message;
}

typedef struct hex_json_translate_frame {
    hex_t_JsonValue value;
    bool object;
    union {
        yyjson_arr_iter array;
        yyjson_obj_iter object_iter;
    } iterator;
} hex_json_translate_frame;

// Scalar translation and container allocation share one path; nested values
// are attached before their children are visited so a failed subtree remains
// reachable from the root cleanup.
static bool hex_json_translate_node(yyjson_val *val, hex_json_translate *out, bool *container) {
    *container = false;
    switch (yyjson_get_type(val)) {
    case YYJSON_TYPE_NULL:
        out->ok = true;
        out->value = (hex_t_JsonValue){ .tag = {{.TagNull}} };
        return true;
    case YYJSON_TYPE_BOOL:
        out->ok = true;
        out->value = (hex_t_JsonValue){ .tag = {{.TagBool}},
            .payload.Bool.hex_m_value = yyjson_get_bool(val) };
        return true;
    case YYJSON_TYPE_NUM:
        switch (yyjson_get_subtype(val)) {
        case YYJSON_SUBTYPE_SINT:
            out->ok = true;
            out->value = (hex_t_JsonValue){ .tag = {{.TagInt}},
                .payload.Int.hex_m_value = yyjson_get_sint(val) };
            return true;
        case YYJSON_SUBTYPE_UINT: {
            // yyjson tags every non-negative integer UINT, so the variant
            // split is decided by value: [0, 2^63) is Int and [2^63, 2^64) is
            // UInt. Classifying on the subtype alone would route every
            // positive integer to UInt and lose the exact Int64 reading.
            uint64_t magnitude = yyjson_get_uint(val);
            out->ok = true;
            if (magnitude <= (uint64_t)INT64_MAX) {
                out->value = (hex_t_JsonValue){ .tag = {{.TagInt}},
                    .payload.Int.hex_m_value = (int64_t)magnitude };
            } else {
                out->value = (hex_t_JsonValue){ .tag = {{.TagUInt}},
                    .payload.UInt.hex_m_value = magnitude };
            }
            return true;
        }
        case YYJSON_SUBTYPE_REAL:
            if (!isfinite(yyjson_get_real(val))) {
                hex_json_translate_fail(out, hex_json_kind_invalid_input(), hex_json_number_range_message);
                return false;
            }
            out->ok = true;
            out->value = (hex_t_JsonValue){ .tag = {{.TagFloat}},
                .payload.Float.hex_m_value = yyjson_get_real(val) };
            return true;
        default:
            hex_json_translate_fail(out, hex_json_kind_unknown(), hex_json_impossible_message);
            return false;
        }
    case YYJSON_TYPE_RAW: {
        // BIGNUM_AS_RAW routed only two shapes here: an integer literal
        // outside Int64/UInt64 and a number literal outside finite Float64.
        // The lexeme alone distinguishes the two range rows.
        const char *lexeme = yyjson_get_raw(val);
        size_t length = yyjson_get_len(val);
        bool fractional = false;
        for (size_t index = 0; index < length; index++) {
            if (lexeme[index] == '.' || lexeme[index] == 'e' || lexeme[index] == 'E') {
                fractional = true;
                break;
            }
        }
        hex_json_translate_fail(out, hex_json_kind_invalid_input(),
            fractional ? hex_json_number_range_message : hex_json_integer_range_message);
        return false;
    }
    case YYJSON_TYPE_STR: {
        const char *data = (const char *)yyjson_get_str(val);
        size_t length = yyjson_get_len(val);
        const hex_string *owned = hex_string_make(out->h,
            (hex_text){ .data = (const uint8_t *)data, .length = length });
        out->ok = true;
        out->value = (hex_t_JsonValue){ .tag = {{.TagText}},
            .payload.Text.hex_m_value = owned };
        return true;
    }
    case YYJSON_TYPE_ARR: {
        hex_list_JsonValue *items = hex_list_new_JsonValue(out->h);
        out->ok = true;
        out->value = (hex_t_JsonValue){ .tag = {{.TagArray}},
            .payload.Array.hex_m_items = items };
        *container = true;
        return true;
    }
    case YYJSON_TYPE_OBJ: {
        hex_list_JsonValueMember *entries = hex_list_new_JsonValueMember(out->h);
        out->ok = true;
        out->value = (hex_t_JsonValue){ .tag = {{.TagObject}},
            .payload.Object.hex_m_entries = entries };
        *container = true;
        return true;
    }
    default:
        hex_json_translate_fail(out, hex_json_kind_unknown(), hex_json_impossible_message);
        return false;
    }
}

static bool hex_json_translate_attach(hex_json_translate_frame *frame, yyjson_val *key,
        hex_t_JsonValue value, hex_heap h) {
    if (!frame->object) {
        hex_list_push_JsonValue(frame->value.payload.Array.hex_m_items, value);
        return true;
    }

    const char *name = (const char *)yyjson_get_str(key);
    size_t name_length = yyjson_get_len(key);
    const hex_string *owned = hex_string_make(h,
        (hex_text){ .data = (const uint8_t *)name, .length = name_length });
    hex_list_JsonValueMember *entries = frame->value.payload.Object.hex_m_entries;
    for (size_t index = 0; index < entries->length; index++) {
        hex_t_JsonValueMember *entry = hex_list_at_mut_JsonValueMember(entries, index);
        const hex_string *prior = entry->hex_m_name;
        if (prior == NULL || prior->byte_length != owned->byte_length) {
            continue;
        }
        if (owned->byte_length > 0 &&
            memcmp(prior->data, owned->data, owned->byte_length) != 0) {
            continue;
        }
        hex_json_free_raw(h, entry->hex_m_value);
        hex_string_free(h, owned);
        entry->hex_m_value = value;
        return true;
    }
    hex_list_push_JsonValueMember(entries, (hex_t_JsonValueMember){
        .hex_m_name = owned, .hex_m_value = value });
    return true;
}

// A bounded explicit stack keeps the configured maximum independent of the
// platform's much smaller and mode-dependent C call stack.
static bool hex_json_translate_value(yyjson_val *root, hex_json_translate *out) {
    hex_json_translate_frame frames[HEX_JSON_MAX_DEPTH];
    size_t frame_count = 0;
    yyjson_val *node = root;
    yyjson_val *member_key = NULL;

    for (;;) {
        hex_json_translate translated = { .h = out->h };
        bool container = false;
        if (!hex_json_translate_node(node, &translated, &container)) {
            hex_json_translate_fail(out, translated.kind, translated.message);
            hex_json_free_raw(out->h, out->value);
            return false;
        }
        if (frame_count == 0) {
            out->value = translated.value;
        } else if (!hex_json_translate_attach(&frames[frame_count - 1], member_key,
                translated.value, out->h)) {
            hex_json_translate_fail(out, hex_json_kind_unknown(), hex_json_impossible_message);
            hex_json_free_raw(out->h, out->value);
            return false;
        }

        if (container) {
            if (frame_count == HEX_JSON_MAX_DEPTH) {
                hex_json_translate_fail(out, hex_json_kind_invalid_input(), hex_json_depth_message);
                hex_json_free_raw(out->h, out->value);
                return false;
            }
            hex_json_translate_frame *frame = &frames[frame_count++];
            frame->value = translated.value;
            frame->object = yyjson_get_type(node) == YYJSON_TYPE_OBJ;
            if (frame->object) {
                yyjson_obj_iter_init(node, &frame->iterator.object_iter);
            } else {
                frame->iterator.array = yyjson_arr_iter_with(node);
            }
        }

        bool found = false;
        while (frame_count > 0) {
            hex_json_translate_frame *frame = &frames[frame_count - 1];
            if (frame->object) {
                member_key = yyjson_obj_iter_next(&frame->iterator.object_iter);
                if (member_key != NULL) {
                    node = yyjson_obj_iter_get_val(member_key);
                    found = true;
                    break;
                }
            } else {
                node = yyjson_arr_iter_next(&frame->iterator.array);
                if (node != NULL) {
                    member_key = NULL;
                    found = true;
                    break;
                }
            }
            frame_count--;
        }
        if (!found) {
            out->ok = true;
            return true;
        }
    }
}

// The reader reports one zero-based byte position; line and column are
// recomputed over the prefix in bytes, so CRLF and multibyte input pin the
// one-based byte column the error table states.
static void hex_json_location(const char *data, size_t length, size_t position,
        size_t *line, size_t *column) {
    size_t current_line = 1;
    size_t current_column = 1;
    if (position > length) {
        position = length;
    }
    for (size_t index = 0; index < position; index++) {
        if (data[index] == '\n') {
            current_line++;
            current_column = 1;
        } else {
            current_column++;
        }
    }
    *line = current_line;
    *column = current_column;
}

hex_json_value_result hex_json_parse_raw(hex_heap h, const hex_string *text) {
    yyjson_alc alc = hex_yyjson_alc();
    yyjson_read_err err;
    memset(&err, 0, sizeof(err));
    yyjson_doc *doc = yyjson_read_opts((char *)text->data, text->byte_length,
        hex_json_read_flags, &alc, &err);
    if (doc == NULL && text->byte_length == 0) {
        // yyjson reports a zero-length buffer as INVALID_PARAMETER, which the
        // error table reserves for impossible states, so the adapter restates
        // empty input as yyjson's own EMPTY_CONTENT result: same wording, same
        // zero position the reader reports for whitespace-only input.
        err.code = YYJSON_READ_ERROR_EMPTY_CONTENT;
        err.msg = "input data is empty";
        err.pos = 0;
    }
    if (doc == NULL) {
        hex_json_value_result failure = { .ok = false };
        if (err.code == YYJSON_READ_ERROR_MEMORY_ALLOCATION) {
            failure.kind = hex_json_kind_resource();
            failure.message = hex_json_message(h, hex_json_oom_message);
            return failure;
        }
        // The remaining adapter-impossible codes: no flag set can produce
        // them, so they take the table's Unknown row untouched.
        if (err.code == YYJSON_READ_ERROR_INVALID_PARAMETER ||
            err.code == YYJSON_READ_ERROR_FILE_OPEN ||
            err.code == YYJSON_READ_ERROR_FILE_READ ||
            err.code == YYJSON_READ_ERROR_MORE) {
            failure.kind = hex_json_kind_unknown();
            failure.message = hex_json_message(h, hex_json_impossible_message);
            return failure;
        }
        size_t line;
        size_t column;
        hex_json_location((const char *)text->data, text->byte_length, err.pos,
            &line, &column);
        char buffer[512];
        const char *detail = err.msg != NULL ? err.msg : "the document could not be read";
        int composed = snprintf(buffer, sizeof(buffer),
            "%s at line %zu, column %zu (byte %zu)", detail, line, column, err.pos);
        size_t length = composed > 0 && (size_t)composed < sizeof(buffer)
            ? (size_t)composed
            : strlen(buffer);
        failure.kind = hex_json_kind_invalid_input();
        failure.message = hex_string_make(h,
            (hex_text){ .data = (const uint8_t *)buffer, .length = length });
        return failure;
    }
    hex_json_translate translated = { .h = h };
    if (!hex_json_translate_value(yyjson_doc_get_root(doc), &translated)) {
        yyjson_doc_free(doc);
        hex_json_value_result failure = { .ok = false,
            .kind = translated.kind,
            .message = hex_json_message(h, translated.message) };
        return failure;
    }
    yyjson_doc_free(doc);
    return (hex_json_value_result){ .ok = true, .value = translated.value };
}

// The stringify bridge's visited-allocation chain: one Hexal-owned intrusive
// list released before return.
typedef struct hex_json_seen {
    const void *pointer;
    struct hex_json_seen *next;
} hex_json_seen;

static bool hex_json_seen_add(hex_json_seen **head, hex_heap h, const void *pointer) {
    (void)h;
    for (const hex_json_seen *cursor = *head; cursor != NULL; cursor = cursor->next) {
        if (cursor->pointer == pointer) {
            return false;
        }
    }
    hex_json_seen *node = hex_heap_allocate(sizeof(hex_json_seen));
    node->pointer = pointer;
    node->next = *head;
    *head = node;
    return true;
}

static void hex_json_seen_release(hex_json_seen *head, hex_heap h) {
    (void)h;
    while (head != NULL) {
        hex_json_seen *next = head->next;
        hex_heap_free(head);
        head = next;
    }
}

// hex_json_walk_fail records one stringify failure: kind and owned message
// together, so the wrapper never receives a failure without a message.
static void hex_json_walk_fail(hex_json_value_result *failure, hex_heap h,
        hex_t_ErrorKind kind, const char *message) {
    failure->ok = false;
    failure->kind = kind;
    failure->message = hex_json_message(h, message);
}

// Every yyjson mut value comes from the document's pool; a NULL result there
// is allocator exhaustion and takes the one OOM row.
static yyjson_mut_val *hex_json_write_alloc(yyjson_mut_val *made, hex_heap h,
        hex_json_value_result *failure) {
    if (made == NULL) {
        hex_json_walk_fail(failure, h, hex_json_kind_resource(), hex_json_oom_message);
    }
    return made;
}

static yyjson_mut_val *hex_json_write_node(yyjson_mut_doc *doc, hex_heap h,
        hex_json_seen **seen, hex_t_JsonValue value, hex_json_value_result *failure,
        int depth_left, bool *container) {
    *container = false;
    if (value.tag != {{.TagArray}} && value.tag != {{.TagObject}} && depth_left < 0) {
        hex_json_walk_fail(failure, h, hex_json_kind_invalid_input(), hex_json_depth_message);
        return NULL;
    }
    switch (value.tag) {
    case {{.TagNull}}:
        return hex_json_write_alloc(yyjson_mut_null(doc), h, failure);
    case {{.TagBool}}:
        return hex_json_write_alloc(yyjson_mut_bool(doc, value.payload.Bool.hex_m_value), h, failure);
    case {{.TagInt}}:
        return hex_json_write_alloc(yyjson_mut_sint(doc, value.payload.Int.hex_m_value), h, failure);
    case {{.TagUInt}}:
        return hex_json_write_alloc(yyjson_mut_uint(doc, value.payload.UInt.hex_m_value), h, failure);
    case {{.TagFloat}}:
        if (!isfinite(value.payload.Float.hex_m_value)) {
            hex_json_walk_fail(failure, h, hex_json_kind_invalid_input(), hex_json_nonfinite_message);
            return NULL;
        }
        return hex_json_write_alloc(yyjson_mut_real(doc, value.payload.Float.hex_m_value), h, failure);
    case {{.TagText}}:
        return hex_json_write_alloc(yyjson_mut_strn(doc,
            (const char *)(value.payload.Text.hex_m_value->data),
            value.payload.Text.hex_m_value->byte_length), h, failure);
    case {{.TagArray}}: {
        // The seen set is checked before the depth guard so a cycle at any
        // depth terminates through the set instead of the guard.
        if (!hex_json_seen_add(seen, h, value.payload.Array.hex_m_items)) {
            hex_json_walk_fail(failure, h, hex_json_kind_invalid_input(), hex_json_cycle_message);
            return NULL;
        }
        if (depth_left < 0) {
            hex_json_walk_fail(failure, h, hex_json_kind_invalid_input(), hex_json_depth_message);
            return NULL;
        }
        yyjson_mut_val *array = hex_json_write_alloc(yyjson_mut_arr(doc), h, failure);
        *container = array != NULL;
        return array;
    }
    case {{.TagObject}}: {
        if (!hex_json_seen_add(seen, h, value.payload.Object.hex_m_entries)) {
            hex_json_walk_fail(failure, h, hex_json_kind_invalid_input(), hex_json_cycle_message);
            return NULL;
        }
        if (depth_left < 0) {
            hex_json_walk_fail(failure, h, hex_json_kind_invalid_input(), hex_json_depth_message);
            return NULL;
        }
        yyjson_mut_val *object = hex_json_write_alloc(yyjson_mut_obj(doc), h, failure);
        *container = object != NULL;
        return object;
    }
    default:
        hex_json_walk_fail(failure, h, hex_json_kind_unknown(), hex_json_impossible_message);
        return NULL;
    }
}

typedef struct hex_json_write_frame {
    hex_t_JsonValue source;
    yyjson_mut_val *output;
    size_t next_index;
    size_t member_index;
    int depth_left;
} hex_json_write_frame;

static bool hex_json_write_attach(yyjson_mut_doc *doc, hex_heap h,
        hex_json_write_frame *frame, yyjson_mut_val *child,
        hex_json_value_result *failure) {
    if (frame->source.tag == {{.TagArray}}) {
        if (!yyjson_mut_arr_add_val(frame->output, child)) {
            hex_json_walk_fail(failure, h, hex_json_kind_resource(), hex_json_oom_message);
            return false;
        }
        return true;
    }

    hex_list_JsonValueMember *entries = frame->source.payload.Object.hex_m_entries;
    const hex_t_JsonValueMember *entry = &entries->data[frame->member_index];
    yyjson_mut_val *name = hex_json_write_alloc(yyjson_mut_strn(doc,
        (const char *)entry->hex_m_name->data, entry->hex_m_name->byte_length), h, failure);
    if (name == NULL || !yyjson_mut_obj_add(frame->output, name, child)) {
        if (name != NULL) {
            hex_json_walk_fail(failure, h, hex_json_kind_resource(), hex_json_oom_message);
        }
        return false;
    }
    return true;
}

// A bounded explicit stack keeps supported JSON depth independent of the C
// call stack, whose available space changes across platforms and build modes.
static yyjson_mut_val *hex_json_write_value(yyjson_mut_doc *doc, hex_heap h,
        hex_json_seen **seen, hex_t_JsonValue value, hex_json_value_result *failure) {
    hex_json_write_frame frames[HEX_JSON_MAX_DEPTH + 2];
    size_t frame_count = 0;
    hex_t_JsonValue current = value;
    int depth_left = HEX_JSON_MAX_DEPTH;
    yyjson_mut_val *root = NULL;

    for (;;) {
        bool container = false;
        yyjson_mut_val *made = hex_json_write_node(doc, h, seen, current, failure,
            depth_left, &container);
        if (made == NULL) {
            return NULL;
        }
        if (frame_count == 0) {
            root = made;
        } else if (!hex_json_write_attach(doc, h, &frames[frame_count - 1], made, failure)) {
            return NULL;
        }
        if (container) {
            frames[frame_count++] = (hex_json_write_frame){
                .source = current,
                .output = made,
                .depth_left = depth_left,
            };
        }

        bool found = false;
        while (frame_count > 0) {
            hex_json_write_frame *frame = &frames[frame_count - 1];
            size_t length;
            if (frame->source.tag == {{.TagArray}}) {
                hex_list_JsonValue *items = frame->source.payload.Array.hex_m_items;
                length = items->length;
                if (frame->next_index < length) {
                    current = items->data[frame->next_index++];
                    depth_left = frame->depth_left - 1;
                    found = true;
                    break;
                }
            } else {
                hex_list_JsonValueMember *entries = frame->source.payload.Object.hex_m_entries;
                length = entries->length;
                if (frame->next_index < length) {
                    frame->member_index = frame->next_index++;
                    current = entries->data[frame->member_index].hex_m_value;
                    depth_left = frame->depth_left - 1;
                    found = true;
                    break;
                }
            }
            frame_count--;
        }
        if (!found) {
            return root;
        }
    }
}

hex_json_string_result hex_json_stringify_raw(hex_heap h, hex_t_JsonValue value) {
    yyjson_alc alc = hex_yyjson_alc();
    yyjson_mut_doc *doc = yyjson_mut_doc_new(&alc);
    if (doc == NULL) {
        return (hex_json_string_result){ .ok = false,
            .kind = hex_json_kind_resource(),
            .message = hex_json_message(h, hex_json_oom_message) };
    }
    hex_json_seen *seen = NULL;
    hex_json_value_result walk_failure = { .ok = true };
    yyjson_mut_val *root = hex_json_write_value(doc, h, &seen, value, &walk_failure);
    hex_json_seen_release(seen, h);
    if (root == NULL) {
        yyjson_mut_doc_free(doc);
        return (hex_json_string_result){ .ok = false,
            .kind = walk_failure.kind, .message = walk_failure.message };
    }
    yyjson_mut_doc_set_root(doc, root);
    size_t written = 0;
    yyjson_write_err write_error;
    memset(&write_error, 0, sizeof(write_error));
    char *raw = yyjson_mut_val_write_opts(root, hex_json_write_flags, &alc, &written, &write_error);
    yyjson_mut_doc_free(doc);
    if (raw == NULL) {
        hex_json_string_result failure = { .ok = false };
        if (write_error.code == YYJSON_WRITE_ERROR_MEMORY_ALLOCATION) {
            failure.kind = hex_json_kind_resource();
            failure.message = hex_json_message(h, hex_json_oom_message);
        } else if (write_error.code == YYJSON_WRITE_ERROR_DEPTH) {
            failure.kind = hex_json_kind_invalid_input();
            failure.message = hex_json_message(h, hex_json_depth_message);
        } else if (write_error.code == YYJSON_WRITE_ERROR_NAN_OR_INF) {
            failure.kind = hex_json_kind_invalid_input();
            failure.message = hex_json_message(h, hex_json_nonfinite_message);
        } else {
            failure.kind = hex_json_kind_unknown();
            failure.message = hex_json_message(h, hex_json_impossible_message);
        }
        return failure;
    }
    const hex_string *copied = hex_string_make(h,
        (hex_text){ .data = (const uint8_t *)raw, .length = written });
    hex_yyjson_free(NULL, raw);
    return (hex_json_string_result){ .ok = true, .value = copied };
}
{{end}}
