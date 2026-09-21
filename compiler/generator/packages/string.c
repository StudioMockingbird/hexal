#include "hexal/string.h"
{{range .Literals}}const uint8_t {{.Name}}_bytes[{{.ArraySize}}] = { {{- range .Payload}} {{.}},{{end}} 0 };
const hex_string {{.Name}} = { .data = {{.Name}}_bytes, .byte_length = {{.PayloadLength}} };
{{end}}
// hex_utf8_valid reports whether the bytes are well-formed UTF-8: no bare
// continuation, no overlong form, no surrogate, nothing above U+10FFFF, and no
// truncated sequence. It is the one place bytes become text.
bool hex_utf8_valid(const uint8_t *data, size_t length) {
    size_t index = 0;
    while (index < length) {
        uint8_t lead = data[index];
        size_t width;
        if (lead < 0x80) {
            index++;
            continue;
        }
        // The lead range beyond continuation shape carries the scalar
        // limits: 0x80-0xC1 are bare continuation bytes or overlong 2-byte
        // leads, and 0xF5-0xFF cannot encode any scalar.
        if (lead < 0xC2 || lead >= 0xF5) {
            return false;
        }
        if (index + 2 > length) {
            return false;
        }
        uint8_t first = data[index + 1];
        // The boundary leads restrict their first continuation to the valid
        // scalar span: E0 (scalar >= U+0800), ED (surrogates rejected),
        // F0 (scalar >= U+10000), F4 (scalar <= U+10FFFF).
        if ((lead == 0xE0 && first < 0xA0) ||
            (lead == 0xED && first >= 0xA0) ||
            (lead == 0xF0 && first < 0x90) ||
            (lead == 0xF4 && first >= 0x90)) {
            return false;
        }
        if (lead < 0xE0) {
            width = 2;
        } else if (lead < 0xF0) {
            width = 3;
        } else {
            width = 4;
        }
        if (index + width > length) {
            return false;
        }
        for (size_t continuation = 1; continuation < width; continuation++) {
            if ((data[index + continuation] & 0xC0) != 0x80) {
                return false;
            }
        }
        index += width;
    }
    return true;
}

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
{{end}}
