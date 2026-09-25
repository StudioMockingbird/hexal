#include "hexal/string.h"
{{if .NeedValidator}}
// The private runtime owns the one Hexal adapter over utf8proc; no public
// header names it. UTF8PROC_STATIC selects utf8proc's static declarations;
// the guard tolerates the feature-test define the harness and driver also pass.
#ifndef UTF8PROC_STATIC
#define UTF8PROC_STATIC
#endif
#include <utf8proc.h>
{{end}}
{{range .Literals}}const uint8_t {{.Name}}_bytes[{{.ArraySize}}] = { {{- range .Payload}} {{.}},{{end}} 0 };
const hex_string {{.Name}} = { .data = {{.Name}}_bytes, .byte_length = {{.PayloadLength}} };
{{end}}
{{if .NeedValidator}}
// hex_utf8_valid reports whether the bytes are well-formed UTF-8. It reports,
// never traps: the bytes are caller data, and the caller turns false into an
// Error. utf8proc_iterate rejects bare continuations, overlong forms,
// surrogates, truncated sequences, and scalars above U+10FFFF. Each call
// passes a non-negative length no greater than four, so an arbitrary size_t
// length is never narrowed to utf8proc_ssize_t.
bool hex_utf8_valid(const uint8_t *data, size_t length) {
    size_t index = 0;
    while (index < length) {
        size_t remaining = length - index;
        utf8proc_ssize_t available =
            (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
        utf8proc_int32_t codepoint = 0;
        utf8proc_ssize_t width =
            utf8proc_iterate(data + index, available, &codepoint);
        if (width <= 0) {
            return false;
        }
        index += (size_t)width;
    }
    return true;
}
{{end}}{{if .NeedRuneLength}}
// hex_text_rune_length counts the Unicode scalars in validated text. It steps
// with utf8proc_iterate and counts, passing a length no greater than four so an
// arbitrary size_t is never narrowed to utf8proc_ssize_t. Validated text cannot
// make iterate fail, so the width guard only keeps the loop total; it embeds no
// diagnostic string, which the interned literal pool owns.
size_t hex_text_rune_length(hex_text text) {
    size_t index = 0;
    size_t runes = 0;
    while (index < text.length) {
        size_t remaining = text.length - index;
        utf8proc_ssize_t available =
            (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
        utf8proc_int32_t codepoint = 0;
        utf8proc_ssize_t width =
            utf8proc_iterate(text.data + index, available, &codepoint);
        index += (size_t)(width > 0 ? width : 1);
        runes++;
    }
    return runes;
}
{{end}}{{if .NeedRuneIteration}}
// hex_utf8_decode_step decodes the scalar starting at offset in validated text
// and reports its encoded width. It passes a length no greater than four so an
// arbitrary size_t is never narrowed to utf8proc_ssize_t. Validated text cannot
// make iterate fail, so the width guard only keeps the step total.
uint32_t hex_utf8_decode_step(const uint8_t *data, size_t length, size_t offset, size_t *width) {
    size_t remaining = length - offset;
    utf8proc_ssize_t available =
        (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
    utf8proc_int32_t codepoint = 0;
    utf8proc_ssize_t decoded =
        utf8proc_iterate(data + offset, available, &codepoint);
    *width = (size_t)(decoded > 0 ? decoded : 1);
    return (uint32_t)codepoint;
}
{{end}}{{if .NeedByteCursor}}
// next and peek trap when exhausted; has_next is the guard. next advances the
// copy it is given, so a copied cursor keeps an independent position.
uint8_t hex_byte_cursor_next(hex_byte_cursor *cursor) {
    if (cursor->offset >= cursor->length) {
        hex_runtime_trap("[Runtime Error] ByteCursor has no next value\n");
    }
    return cursor->data[cursor->offset++];
}

uint8_t hex_byte_cursor_peek(hex_byte_cursor cursor) {
    if (cursor.offset >= cursor.length) {
        hex_runtime_trap("[Runtime Error] ByteCursor has no next value\n");
    }
    return cursor.data[cursor.offset];
}
{{end}}{{if .NeedRuneCursor}}
// next and peek trap when exhausted; has_next is the guard. next decodes one
// scalar and advances the copy it is given, so a copied cursor keeps an
// independent position.
uint32_t hex_rune_cursor_next(hex_rune_cursor *cursor) {
    if (cursor->offset >= cursor->length) {
        hex_runtime_trap("[Runtime Error] RuneCursor has no next value\n");
    }
    size_t width = 0;
    uint32_t value = hex_utf8_decode_step(cursor->data, cursor->length, cursor->offset, &width);
    cursor->offset += width;
    return value;
}

uint32_t hex_rune_cursor_peek(hex_rune_cursor cursor) {
    if (cursor.offset >= cursor.length) {
        hex_runtime_trap("[Runtime Error] RuneCursor has no next value\n");
    }
    size_t width = 0;
    return hex_utf8_decode_step(cursor.data, cursor.length, cursor.offset, &width);
}
{{end}}{{if .NeedRuneEncode}}
// hex_string_from_runes encodes a validated scalar sequence into one owned
// allocation. Each scalar validates as the byte count accumulates with ckd_add,
// so the allocation below encodes an already-validated result; the caller
// checks validity first, making the trap a broken-invariant guard.
const hex_string *hex_string_from_runes(hex_heap h, const uint32_t *data, size_t length) {
    (void)h;
    size_t bytes = 0;
    for (size_t index = 0; index < length; index++) {
        if (!hex_rune_valid(data[index])) {
            hex_runtime_trap("[Runtime Error] invalid Unicode scalar value\n");
        }
        if (ckd_add(&bytes, bytes, hex_rune_width(data[index]))) {
            hex_runtime_trap("[Runtime Error] string allocation size overflow\n");
        }
    }
    size_t total;
    if (ckd_add(&total, sizeof(hex_string_storage), bytes) ||
        ckd_add(&total, total, 1)) {
        hex_runtime_trap("[Runtime Error] string allocation size overflow\n");
    }
    hex_string_storage *storage = hex_heap_allocate(total);
    size_t out = 0;
    for (size_t index = 0; index < length; index++) {
        utf8proc_encode_char((utf8proc_int32_t)data[index], storage->bytes + out);
        out += hex_rune_width(data[index]);
    }
    storage->header = (hex_string){ .data = storage->bytes, .byte_length = bytes, .storage_kind = HEX_STRING_OWNED };
    storage->bytes[bytes] = 0;
    return &storage->header;
}
{{end}}{{if .NeedRuneProperties}}
// The Rune property surface maps directly onto utf8proc: simple case mapping
// and width are single calls, and the alphabetic/numeric/whitespace predicates
// read the general category (Letter, Number, Separator) the standard fixes.
bool hex_rune_is_lower(uint32_t value) {
    return utf8proc_islower((utf8proc_int32_t)value) != 0;
}

bool hex_rune_is_upper(uint32_t value) {
    return utf8proc_isupper((utf8proc_int32_t)value) != 0;
}

bool hex_rune_is_alphabetic(uint32_t value) {
    switch (utf8proc_category((utf8proc_int32_t)value)) {
    case UTF8PROC_CATEGORY_LU:
    case UTF8PROC_CATEGORY_LL:
    case UTF8PROC_CATEGORY_LT:
    case UTF8PROC_CATEGORY_LM:
    case UTF8PROC_CATEGORY_LO:
        return true;
    default:
        return false;
    }
}

bool hex_rune_is_numeric(uint32_t value) {
    switch (utf8proc_category((utf8proc_int32_t)value)) {
    case UTF8PROC_CATEGORY_ND:
    case UTF8PROC_CATEGORY_NL:
    case UTF8PROC_CATEGORY_NO:
        return true;
    default:
        return false;
    }
}

bool hex_rune_is_whitespace(uint32_t value) {
    switch (utf8proc_category((utf8proc_int32_t)value)) {
    case UTF8PROC_CATEGORY_ZS:
    case UTF8PROC_CATEGORY_ZL:
    case UTF8PROC_CATEGORY_ZP:
        return true;
    default:
        return false;
    }
}

uint32_t hex_rune_to_lower(uint32_t value) {
    return (uint32_t)utf8proc_tolower((utf8proc_int32_t)value);
}

uint32_t hex_rune_to_upper(uint32_t value) {
    return (uint32_t)utf8proc_toupper((utf8proc_int32_t)value);
}

uint32_t hex_rune_to_title(uint32_t value) {
    return (uint32_t)utf8proc_totitle((utf8proc_int32_t)value);
}

int32_t hex_rune_display_width(uint32_t value) {
    return (int32_t)utf8proc_charwidth((utf8proc_int32_t)value);
}

uint8_t hex_rune_combining_class(uint32_t value) {
    return (uint8_t)utf8proc_get_property((utf8proc_int32_t)value)->combining_class;
}
{{end}}{{if .NeedRuneCategory}}
// hex_rune_category_index returns the utf8proc general-category ordinal of one
// scalar, 0 through 29, which indexes the module's variant-tag table.
int32_t hex_rune_category_index(uint32_t value) {
    return (int32_t)utf8proc_category((utf8proc_int32_t)value);
}
{{end}}

// hex_string_make copies text into one owned allocation, followed by the zero
// byte String.c_pointer relies on. The bytes are not validated here: the
// caller has already proven them well-formed.
const hex_string *hex_string_make(hex_heap h, hex_text text) {
    return hex_string_join(h, text, (hex_text){ nullptr, 0 });
}

// hex_string_join copies two texts, one after the other, into one owned
// allocation. The header, payload, and terminator chain is checked with
// ckd_add before the raw allocator sees any sum.
const hex_string *hex_string_join(hex_heap h, hex_text left, hex_text right) {
    (void)h;
    size_t length;
    if (ckd_add(&length, left.length, right.length)) {
        hex_runtime_trap("[Runtime Error] string concatenation length overflow\n");
    }
    size_t total;
    if (ckd_add(&total, sizeof(hex_string_storage), length) ||
        ckd_add(&total, total, 1)) {
        hex_runtime_trap("[Runtime Error] string concatenation length overflow\n");
    }
    hex_string_storage *storage = hex_heap_allocate(total);
    storage->header = (hex_string){ .data = storage->bytes, .byte_length = length, .storage_kind = HEX_STRING_OWNED };
    // Each input copies with a guarded memcpy; the freshly allocated
    // destination cannot overlap the immutable inputs, and a zero-length
    // payload never passes a possibly invalid pointer to a memory function.
    if (left.length != 0) {
        memcpy(storage->bytes, left.data, left.length);
    }
    if (right.length != 0) {
        memcpy(storage->bytes + left.length, right.data, right.length);
    }
    storage->bytes[length] = 0;
    return &storage->header;
}

void hex_string_free(hex_heap h, const hex_string *text) {
    (void)h;
    // Only owned storage names a heap allocation base. A static literal
    // header shares the handle representation but no allocation; freeing it
    // is a programmer error that traps instead of reaching the deallocator.
    if (text->storage_kind == HEX_STRING_STATIC) {
        hex_runtime_trap("[Runtime Error] cannot free a String literal\n");
    }
    if (text->storage_kind != HEX_STRING_OWNED) {
        hex_runtime_trap("[Runtime Error] cannot free a non-owning String\n");
    }
    // hex_string is the first member of hex_string_storage, so the member
    // pointer and the allocation base share an address. The uintptr_t round
    // trip recovers that base without a const-qualified pointer conversion.
    hex_heap_free((hex_string_storage *)(void *)(uintptr_t)text);
}

void *hex_text_fill(void *destination, hex_text text) {
    *(size_t *)destination = text.length;
    if (text.length != 0) {
        memcpy((uint8_t *)destination + sizeof(size_t), text.data, text.length);
    }
    return destination;
}

void *hex_text_fill_checked(void *destination, size_t capacity, hex_text text, const char *message) {
    if (text.length > capacity) {
        hex_runtime_trap(message);
    }
    return hex_text_fill(destination, text);
}
{{if .NeedInterpolation}}
// hex_string_format_* helpers write one interpolation-supported scalar's
// print spelling into a caller-owned stack buffer and return its byte
// length, so String.interpolate can measure and copy each formatted
// segment without an intermediate heap allocation. The spelling matches
// print's own formatting exactly (see hexal/print.c); Bool needs no
// dedicated helper here since a ternary literal already covers it at the
// call site.
size_t hex_string_format_int8(char buffer[32], int8_t value) {
    return (size_t)snprintf(buffer, 32, "%" PRId8, value);
}
size_t hex_string_format_int16(char buffer[32], int16_t value) {
    return (size_t)snprintf(buffer, 32, "%" PRId16, value);
}
size_t hex_string_format_int32(char buffer[32], int32_t value) {
    return (size_t)snprintf(buffer, 32, "%" PRId32, value);
}
size_t hex_string_format_int64(char buffer[32], int64_t value) {
    return (size_t)snprintf(buffer, 32, "%" PRId64, value);
}
size_t hex_string_format_uint8(char buffer[32], uint8_t value) {
    return (size_t)snprintf(buffer, 32, "%" PRIu8, value);
}
size_t hex_string_format_uint16(char buffer[32], uint16_t value) {
    return (size_t)snprintf(buffer, 32, "%" PRIu16, value);
}
size_t hex_string_format_uint32(char buffer[32], uint32_t value) {
    return (size_t)snprintf(buffer, 32, "%" PRIu32, value);
}
size_t hex_string_format_uint64(char buffer[32], uint64_t value) {
    return (size_t)snprintf(buffer, 32, "%" PRIu64, value);
}
size_t hex_string_format_size(char buffer[32], size_t value) {
    return (size_t)snprintf(buffer, 32, "%zu", value);
}
size_t hex_string_format_float64(char buffer[64], double value) {
    if (isnan(value)) { memcpy(buffer, "nan", 3); return 3; }
    if (isinf(value)) {
        if (signbit(value)) { memcpy(buffer, "-inf", 4); return 4; }
        memcpy(buffer, "inf", 3);
        return 3;
    }
    if (value == 0.0) { buffer[0] = '0'; return 1; }
    size_t offset = 0;
    if (signbit(value)) { buffer[0] = '-'; offset = 1; value = -value; }
    int n = snprintf(buffer + offset, 64 - offset, "%.17g", value);
    return offset + (size_t)n;
}
size_t hex_string_format_float32(char buffer[64], float value) {
    if (isnan(value)) { memcpy(buffer, "nan", 3); return 3; }
    if (isinf(value)) {
        if (signbit(value)) { memcpy(buffer, "-inf", 4); return 4; }
        memcpy(buffer, "inf", 3);
        return 3;
    }
    if (value == 0.0f) { buffer[0] = '0'; return 1; }
    size_t offset = 0;
    if (signbit(value)) { buffer[0] = '-'; offset = 1; value = -value; }
    int n = snprintf(buffer + offset, 64 - offset, "%.9g", value);
    return offset + (size_t)n;
}
{{end}}
{{if .NeedEquality}}
bool hex_equal_text(hex_text left, hex_text right) {
    if (left.length != right.length) {
        return false;
    }
    if (left.length != 0) {
        if (memcmp(left.data, right.data, left.length) != 0) {
            return false;
        }
    }
    return true;
}
{{end}}
{{if .NeedOrdering}}
int hex_compare_text(hex_text left, hex_text right) {
    size_t limit = left.length < right.length ? left.length : right.length;
    if (limit != 0) {
        int result = memcmp(left.data, right.data, limit);
        if (result != 0) {
            return result;
        }
    }
    if (left.length < right.length) return -1;
    if (left.length > right.length) return 1;
    return 0;
}
{{end}}
{{if .NeedHash}}
// FNV-1a over the logical bytes only. Capacity and any bytes past the length
// never participate, so equal text hashes equally whichever form holds it.
uint64_t hex_hash_text(hex_text text) {
    uint64_t hash = 14695981039346656037ull;
    for (size_t index = 0; index < text.length; index++) {
        hash ^= text.data[index];
        hash *= 1099511628211ull;
    }
    return hash;
}
{{end}}{{if .NeedGraphemeLength}}
// hex_text_grapheme_length counts the extended grapheme clusters in validated
// text. utf8proc_grapheme_break_stateful must see every adjacent scalar pair in
// order, so the scan decodes each scalar once and feeds the pair as it goes.
size_t hex_text_grapheme_length(hex_text text) {
    utf8proc_int32_t state = 0;
    utf8proc_int32_t previous = 0;
    size_t offset = 0;
    size_t clusters = 0;
    while (offset < text.length) {
        size_t remaining = text.length - offset;
        utf8proc_ssize_t available =
            (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
        utf8proc_int32_t codepoint = 0;
        utf8proc_ssize_t width =
            utf8proc_iterate(text.data + offset, available, &codepoint);
        if (width <= 0) {
            width = 1;
        }
        if (clusters == 0 || utf8proc_grapheme_break_stateful(previous, codepoint, &state)) {
            clusters++;
        }
        previous = codepoint;
        offset += (size_t)width;
    }
    return clusters;
}
{{end}}{{if .NeedCasefold}}
// hex_text_casefold maps text through utf8proc's case-folding tables and copies
// the result into one Hexal Heap allocation. utf8proc_map returns malloc
// memory, which is released here on every path, success or failure; it is
// never exposed as a Hexal String and never reaches Heap.free.
const hex_string *hex_text_casefold(hex_heap h, hex_text text) {
    utf8proc_uint8_t *mapped = nullptr;
    utf8proc_ssize_t length =
        utf8proc_map(text.data, (utf8proc_ssize_t)text.length, &mapped,
                     UTF8PROC_STABLE | UTF8PROC_COMPOSE | UTF8PROC_CASEFOLD);
    if (length < 0 || mapped == nullptr) {
        if (mapped != nullptr) {
            free(mapped);
        }
        return nullptr;
    }
    const hex_string *result = hex_string_make(h, (hex_text){ mapped, (size_t)length });
    free(mapped);
    return result;
}
{{end}}{{if .NeedNormalize}}
// hex_text_normalize maps text through utf8proc's normalization tables for one
// form index (0 NFC, 1 NFD, 2 NFKC, 3 NFKD) and copies the result into one
// Hexal Heap allocation. utf8proc_map returns malloc memory, which is released
// here on every path, success or failure; it is never exposed as a Hexal String
// and never reaches Heap.free. An out-of-range index reports failure.
const hex_string *hex_text_normalize(hex_heap h, hex_text text, int32_t form) {
    utf8proc_int32_t options;
    switch (form) {
    case 0:
        options = UTF8PROC_STABLE | UTF8PROC_COMPOSE;
        break;
    case 1:
        options = UTF8PROC_STABLE | UTF8PROC_DECOMPOSE;
        break;
    case 2:
        options = UTF8PROC_STABLE | UTF8PROC_COMPOSE | UTF8PROC_COMPAT;
        break;
    case 3:
        options = UTF8PROC_STABLE | UTF8PROC_DECOMPOSE | UTF8PROC_COMPAT;
        break;
    default:
        return nullptr;
    }
    utf8proc_uint8_t *mapped = nullptr;
    utf8proc_ssize_t length =
        utf8proc_map(text.data, (utf8proc_ssize_t)text.length, &mapped, options);
    if (length < 0 || mapped == nullptr) {
        if (mapped != nullptr) {
            free(mapped);
        }
        return nullptr;
    }
    const hex_string *result = hex_string_make(h, (hex_text){ mapped, (size_t)length });
    free(mapped);
    return result;
}
{{end}}{{if .NeedGrapheme}}
// hex_grapheme_cursor_scan returns the end offset of the cluster starting at
// the cursor's position and caches it, advancing the break state exactly once.
// A cached lookahead is reused, so a peek followed by next shares the work.
static size_t hex_grapheme_cursor_scan(hex_grapheme_cursor *cursor) {
    if (cursor->cached_end != 0) {
        return cursor->cached_end;
    }
    size_t offset = cursor->offset;
    size_t remaining = cursor->length - offset;
    utf8proc_ssize_t available =
        (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
    utf8proc_int32_t previous = 0;
    utf8proc_ssize_t width =
        utf8proc_iterate(cursor->data + offset, available, &previous);
    offset += (size_t)(width > 0 ? width : 1);
    while (offset < cursor->length) {
        remaining = cursor->length - offset;
        available = (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
        utf8proc_int32_t codepoint = 0;
        width = utf8proc_iterate(cursor->data + offset, available, &codepoint);
        if (width <= 0) {
            width = 1;
        }
        if (utf8proc_grapheme_break_stateful(previous, codepoint, &cursor->state)) {
            break;
        }
        previous = codepoint;
        offset += (size_t)width;
    }
    cursor->cached_end = offset;
    return offset;
}

hex_grapheme hex_grapheme_cursor_next(hex_grapheme_cursor *cursor) {
    if (cursor->offset >= cursor->length) {
        hex_runtime_trap("[Runtime Error] GraphemeCursor has no next value\n");
    }
    size_t start = cursor->offset;
    size_t end = hex_grapheme_cursor_scan(cursor);
    cursor->offset = end;
    cursor->cached_end = 0;
    return (hex_grapheme){ cursor->data + start, end - start };
}

hex_grapheme hex_grapheme_cursor_peek(hex_grapheme_cursor *cursor) {
    if (cursor->offset >= cursor->length) {
        hex_runtime_trap("[Runtime Error] GraphemeCursor has no next value\n");
    }
    size_t end = hex_grapheme_cursor_scan(cursor);
    return (hex_grapheme){ cursor->data + cursor->offset, end - cursor->offset };
}

size_t hex_grapheme_rune_length(hex_grapheme grapheme) {
    size_t offset = 0;
    size_t runes = 0;
    while (offset < grapheme.length) {
        size_t remaining = grapheme.length - offset;
        utf8proc_ssize_t available =
            (utf8proc_ssize_t)(remaining < 4 ? remaining : 4);
        utf8proc_int32_t codepoint = 0;
        utf8proc_ssize_t width =
            utf8proc_iterate(grapheme.data + offset, available, &codepoint);
        offset += (size_t)(width > 0 ? width : 1);
        runes++;
    }
    return runes;
}
{{end}}
