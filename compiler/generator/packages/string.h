#ifndef HEXAL_STRING_H
#define HEXAL_STRING_H

#include "hexal.h"
#include "hexal/heap.h"
#include "hexal/view.h"

typedef enum hex_string_storage_kind {
    HEX_STRING_STATIC = 0,
    HEX_STRING_OWNED = 1
} hex_string_storage_kind;

typedef struct hex_string {
    const uint8_t *data;
    size_t byte_length;
    size_t rune_length;
    // Storage kind is zero (static) for literal headers through C's
    // zero-initialization of the omitted field; every owning constructor
    // marks its header owned explicitly. An omitted owning mark therefore
    // fails safely by trapping instead of permitting an invalid free.
    hex_string_storage_kind storage_kind;
} hex_string;

typedef struct hex_string_storage {
    hex_string header;
    uint8_t bytes[];
} hex_string_storage;
{{if .NeedStrand}}
typedef struct hex_strand {
    uint8_t data[32];
} hex_strand;
{{end}}
{{range .Literals}}extern const uint8_t {{.Name}}_bytes[{{.ArraySize}}];
extern const hex_string {{.Name}};
{{end}}
uint64_t hex_utf8_next(const uint8_t *data, size_t length, size_t *index);
uint32_t hex_utf8_decode(const uint8_t *data, size_t length, size_t *index);
size_t hex_utf8_encode(uint8_t *out, uint32_t value);
const hex_string *hex_string_from_bytes(hex_heap h, const uint8_t *data, size_t length);
const hex_string *hex_string_from_runes(hex_heap h, const uint32_t *data, size_t length);
const hex_string *hex_string_to_string(hex_heap h, const hex_string *text);
const hex_string *hex_string_concat(hex_heap h, const hex_string *left, const hex_string *right);
void hex_string_free(hex_heap h, const hex_string *text);

typedef struct hex_rune_cursor {
    const uint8_t *data;
    size_t length;
    size_t offset;
} hex_rune_cursor;

hex_rune_cursor hex_string_rune_cursor(const hex_string *text);
bool hex_rune_cursor_has_next(hex_rune_cursor cursor);
uint32_t hex_rune_cursor_next(hex_rune_cursor *cursor);

static inline size_t hex_string_rune_length(const hex_string *text) {
    return text->rune_length;
}

static inline hex_view_UInt8 hex_string_bytes(const hex_string *text) {
    return (hex_view_UInt8){ text->data, text->byte_length };
}

static inline hex_view_UInt8 hex_string_slice(const hex_string *text, size_t start, size_t end) {
    if (!(start <= end && end <= text->rune_length)) {
        hex_runtime_trap("[Runtime Error] string slice bounds out of range\n");
    }
    size_t byteStart = 0;
    size_t byteEnd = 0;
    size_t index = 0;
    for (size_t rune = 0; rune < end; rune++) {
        hex_utf8_next(text->data, text->byte_length, &index);
        if (rune + 1 == start) {
            byteStart = index;
        }
    }
    byteEnd = index;
    return (hex_view_UInt8){ text->data + byteStart, byteEnd - byteStart };
}
{{if .NeedStrand}}
size_t hex_strand_rune_length(hex_strand text);
const hex_string *hex_strand_to_string(hex_heap h, hex_strand text);
{{end}}
{{if .NeedInterpolation}}
size_t hex_string_format_int8(char buffer[32], int8_t value);
size_t hex_string_format_int16(char buffer[32], int16_t value);
size_t hex_string_format_int32(char buffer[32], int32_t value);
size_t hex_string_format_int64(char buffer[32], int64_t value);
size_t hex_string_format_uint8(char buffer[32], uint8_t value);
size_t hex_string_format_uint16(char buffer[32], uint16_t value);
size_t hex_string_format_uint32(char buffer[32], uint32_t value);
size_t hex_string_format_uint64(char buffer[32], uint64_t value);
size_t hex_string_format_size(char buffer[32], size_t value);
size_t hex_string_format_float32(char buffer[64], float value);
size_t hex_string_format_float64(char buffer[64], double value);
{{if .NeedStrand}}
size_t hex_strand_byte_length(hex_strand text);
{{end}}
{{end -}}
{{if .NeedEquality}}
bool hex_equal_hex_string(const hex_string *left, const hex_string *right);
{{end}}
{{if .NeedOrdering}}
int hex_compare_hex_string(const hex_string *left, const hex_string *right);
{{end}}
#endif
