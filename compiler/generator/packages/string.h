#ifndef HEXAL_STRING_H
#define HEXAL_STRING_H

#include "hexal.h"
#include "hexal/heap.h"
#include "hexal/slice.h"

typedef enum hex_string_storage_kind {
    HEX_STRING_STATIC = 0,
    HEX_STRING_NONOWNING = 1,
    HEX_STRING_OWNED = 2
} hex_string_storage_kind;

// The heap String handle points at one of these. It stores the byte length and
// nothing else about the text: no rune count, and no terminator in the header.
// An owned or literal allocation carries one zero byte past byte_length, used
// only by String.c_pointer.
typedef struct hex_string {
    const uint8_t *data;
    size_t byte_length;
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

// hex_text is the one read view every text operation takes: the bytes and the
// byte length, whichever form holds them. Equality, ordering, hashing, and
// printing are all defined over it, so they serve String and every String<N>.
typedef struct hex_text {
    const uint8_t *data;
    size_t length;
} hex_text;

static inline hex_text hex_text_heap(const hex_string *text) {
    return (hex_text){ text->data, text->byte_length };
}

static inline hex_text hex_text_view(hex_slice_UInt8 bytes) {
    return (hex_text){ bytes.data, bytes.length };
}

// Every String<N> is a size_t byte length followed by N payload bytes, so one
// accessor views them all. The view addresses the value it is given: the value
// must outlive the view.
static inline hex_text hex_text_inline(const void *value) {
    return (hex_text){ (const uint8_t *)value + sizeof(size_t), *(const size_t *)value };
}

{{range .Inline}}#ifndef HEXAL_TEXT_{{.Capacity}}_DEFINED
#define HEXAL_TEXT_{{.Capacity}}_DEFINED
typedef struct {{.CName}} {
    size_t byte_length;
    uint8_t data[{{.Capacity}}];
} {{.CName}};
static_assert(offsetof({{.CName}}, data) == sizeof(size_t), "inline text layout");
#endif
{{end}}
{{range .Literals}}extern const uint8_t {{.Name}}_bytes[{{.ArraySize}}];
extern const hex_string {{.Name}};
{{end}}
{{if .NeedValidator}}bool hex_utf8_valid(const uint8_t *data, size_t length);
{{end}}{{if .NeedRuneLength}}size_t hex_text_rune_length(hex_text text);
{{end}}{{if .NeedRuneIteration}}uint32_t hex_utf8_decode_step(const uint8_t *data, size_t length, size_t offset, size_t *width);
{{end}}{{if .NeedRuneEncode}}static inline bool hex_rune_valid(uint32_t value) {
    return value <= 0x10FFFF && !(value >= 0xD800 && value <= 0xDFFF);
}
static inline size_t hex_rune_width(uint32_t value) {
    if (value < 0x80) {
        return 1;
    }
    if (value < 0x800) {
        return 2;
    }
    if (value < 0x10000) {
        return 3;
    }
    return 4;
}
const hex_string *hex_string_from_runes(hex_heap h, const uint32_t *data, size_t length);
{{end}}{{if .NeedRuneProperties}}bool hex_rune_is_lower(uint32_t value);
bool hex_rune_is_upper(uint32_t value);
bool hex_rune_is_alphabetic(uint32_t value);
bool hex_rune_is_numeric(uint32_t value);
bool hex_rune_is_whitespace(uint32_t value);
uint32_t hex_rune_to_lower(uint32_t value);
uint32_t hex_rune_to_upper(uint32_t value);
uint32_t hex_rune_to_title(uint32_t value);
int32_t hex_rune_display_width(uint32_t value);
uint8_t hex_rune_combining_class(uint32_t value);
{{end}}{{if .NeedRuneCategory}}int32_t hex_rune_category_index(uint32_t value);
{{end}}{{if .NeedCasefold}}const hex_string *hex_text_casefold(hex_heap h, hex_text text);
{{end}}{{if .NeedNormalize}}// hex_t_NormalizationForm is the protected builtin normalization selector:
// every variant is a unit variant, so the representation is the shared tag
// alone.
typedef struct hex_t_NormalizationForm {
    hex_tag tag;
} hex_t_NormalizationForm;
const hex_string *hex_text_normalize(hex_heap h, hex_text text, int32_t form);
{{end}}{{if .NeedGraphemeLength}}size_t hex_text_grapheme_length(hex_text text);
{{end}}{{if .NeedGrapheme}}
// hex_grapheme is one extended grapheme cluster borrowed from the text it came
// from. It is a view: it dangles if that text is freed, reassigned, or leaves
// scope, and it is not Dict-key eligible.
typedef struct hex_grapheme {
    const uint8_t *data;
    size_t length;
} hex_grapheme;

// hex_grapheme_cursor owns the segmenter's break state, so it must be fed every
// adjacent scalar pair in order. cached_end is the lookahead cluster peek
// computed; zero means none, which no real cluster end can be.
typedef struct hex_grapheme_cursor {
    const uint8_t *data;
    size_t length;
    size_t offset;
    int32_t state;
    size_t cached_end;
} hex_grapheme_cursor;

static inline hex_grapheme_cursor hex_text_grapheme_cursor(hex_text text) {
    return (hex_grapheme_cursor){ text.data, text.length, 0, 0, 0 };
}
static inline bool hex_grapheme_cursor_has_next(hex_grapheme_cursor cursor) {
    return cursor.offset < cursor.length;
}
static inline size_t hex_grapheme_cursor_offset(hex_grapheme_cursor cursor) {
    return cursor.offset;
}
hex_grapheme hex_grapheme_cursor_next(hex_grapheme_cursor *cursor);
hex_grapheme hex_grapheme_cursor_peek(hex_grapheme_cursor *cursor);
static inline hex_slice_UInt8 hex_grapheme_bytes(hex_grapheme grapheme) {
    return (hex_slice_UInt8){ grapheme.data, grapheme.length };
}
size_t hex_grapheme_rune_length(hex_grapheme grapheme);
{{end}}{{if .NeedUnicodeCategory}}// hex_t_UnicodeCategory is the protected builtin general-category value: every
// variant is a unit variant, so the representation is the shared tag alone.
typedef struct hex_t_UnicodeCategory {
    hex_tag tag;
} hex_t_UnicodeCategory;
{{end}}const hex_string *hex_string_make(hex_heap h, hex_text text);
const hex_string *hex_string_join(hex_heap h, hex_text left, hex_text right);
void hex_string_free(hex_heap h, const hex_string *text);

// hex_text_fill writes text into an inline value: the length, then the bytes.
// The caller guarantees the text fits. hex_text_fill_checked traps with message
// when it does not, and never truncates. Both return the destination.
void *hex_text_fill(void *destination, hex_text text);
void *hex_text_fill_checked(void *destination, size_t capacity, hex_text text, const char *message);

// hex_text_slice returns the byte range [start, end) of text as a read-only
// byte slice, in constant time. A range that splits a UTF-8 sequence is legal:
// the result is bytes, not text.
static inline hex_slice_UInt8 hex_text_slice(hex_text text, size_t start, size_t end) {
    if (!(start <= end && end <= text.length)) {
        hex_runtime_trap("[Runtime Error] string slice bounds out of range\n");
    }
    return (hex_slice_UInt8){ text.data + start, end - start };
}

static inline hex_slice_UInt8 hex_text_bytes(hex_text text) {
    return (hex_slice_UInt8){ text.data, text.length };
}
{{if .NeedByteCursor}}
// A ByteCursor is a copyable position over one text's bytes. It is a view: it
// dangles if the text it points at is freed, reassigned, or leaves scope.
typedef struct hex_byte_cursor {
    const uint8_t *data;
    size_t length;
    size_t offset;
} hex_byte_cursor;
static inline hex_byte_cursor hex_text_byte_cursor(hex_text text) {
    return (hex_byte_cursor){ text.data, text.length, 0 };
}
static inline bool hex_byte_cursor_has_next(hex_byte_cursor cursor) {
    return cursor.offset < cursor.length;
}
static inline size_t hex_byte_cursor_offset(hex_byte_cursor cursor) {
    return cursor.offset;
}
uint8_t hex_byte_cursor_next(hex_byte_cursor *cursor);
uint8_t hex_byte_cursor_peek(hex_byte_cursor cursor);
{{end}}{{if .NeedRuneCursor}}
// A RuneCursor is a copyable position over one text's scalars. It is a view:
// it dangles if the text it points at is freed, reassigned, or leaves scope.
typedef struct hex_rune_cursor {
    const uint8_t *data;
    size_t length;
    size_t offset;
} hex_rune_cursor;
static inline hex_rune_cursor hex_text_rune_cursor(hex_text text) {
    return (hex_rune_cursor){ text.data, text.length, 0 };
}
static inline bool hex_rune_cursor_has_next(hex_rune_cursor cursor) {
    return cursor.offset < cursor.length;
}
static inline size_t hex_rune_cursor_offset(hex_rune_cursor cursor) {
    return cursor.offset;
}
uint32_t hex_rune_cursor_next(hex_rune_cursor *cursor);
uint32_t hex_rune_cursor_peek(hex_rune_cursor cursor);
{{end}}{{if .NeedInterpolation}}
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
{{end -}}
{{if .NeedEquality}}
bool hex_equal_text(hex_text left, hex_text right);
{{end}}
{{if .NeedOrdering}}
int hex_compare_text(hex_text left, hex_text right);
{{end}}
{{if .NeedHash}}
uint64_t hex_hash_text(hex_text text);
{{end}}
#endif
