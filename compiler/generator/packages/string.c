#include "hexal/string.h"
{{range .Literals}}const uint8_t {{.Name}}_bytes[{{.ArraySize}}] = { {{- range .Payload}} {{.}},{{end}} 0 };
const hex_string {{.Name}} = { {{.Name}}_bytes, {{.PayloadLength}} };
{{end}}
uint64_t hex_utf8_next(const uint8_t *data, size_t length, size_t *index) {
    uint8_t lead = data[*index];
    uint64_t width;
    if (lead < 0x80) {
        width = 1;
    } else {
        // The lead range beyond continuation shape carries the scalar
        // limits: 0x80-0xC1 are bare continuation bytes or overlong 2-byte
        // leads, and 0xF5-0xFF cannot encode any scalar.
        if (lead < 0xC2 || lead >= 0xF5) {
            hex_runtime_trap("[Runtime Error] invalid UTF-8 in String\n");
        }
        if (*index + 2 > length) {
            hex_runtime_trap("[Runtime Error] invalid UTF-8 in String\n");
        }
        uint8_t first = data[*index + 1];
        // The boundary leads restrict their first continuation to the valid
        // scalar span: E0 (scalar >= U+0800), ED (surrogates rejected),
        // F0 (scalar >= U+10000), F4 (scalar <= U+10FFFF).
        if ((lead == 0xE0 && first < 0xA0) ||
            (lead == 0xED && first >= 0xA0) ||
            (lead == 0xF0 && first < 0x90) ||
            (lead == 0xF4 && first >= 0x90)) {
            hex_runtime_trap("[Runtime Error] invalid UTF-8 in String\n");
        }
        if (lead < 0xE0) {
            width = 2;
        } else if (lead < 0xF0) {
            width = 3;
        } else {
            width = 4;
        }
    }
    if (*index + width > length) {
        hex_runtime_trap("[Runtime Error] invalid UTF-8 in String\n");
    }
    for (uint64_t continuation = 1; continuation < width; continuation++) {
        if ((data[*index + continuation] & 0xC0) != 0x80) {
            hex_runtime_trap("[Runtime Error] invalid UTF-8 in String\n");
        }
    }
    *index += width;
    return width;
}

uint32_t hex_utf8_decode(const uint8_t *data, size_t length, size_t *index) {
    size_t start = *index;
    hex_utf8_next(data, length, index);
    uint8_t lead = data[start];
    if (lead < 0x80) {
        return lead;
    }
    if (lead < 0xE0) {
        return ((uint32_t)(lead & 0x1F) << 6) | (uint32_t)(data[start + 1] & 0x3F);
    }
    if (lead < 0xF0) {
        return ((uint32_t)(lead & 0x0F) << 12) | ((uint32_t)(data[start + 1] & 0x3F) << 6) | (uint32_t)(data[start + 2] & 0x3F);
    }
    return ((uint32_t)(lead & 0x07) << 18) | ((uint32_t)(data[start + 1] & 0x3F) << 12) | ((uint32_t)(data[start + 2] & 0x3F) << 6) | (uint32_t)(data[start + 3] & 0x3F);
}

size_t hex_utf8_encode(uint8_t *out, uint32_t value) {
    if (value < 0x80) {
        out[0] = (uint8_t)value;
        return 1;
    }
    if (value < 0x800) {
        out[0] = (uint8_t)(0xC0 | (value >> 6));
        out[1] = (uint8_t)(0x80 | (value & 0x3F));
        return 2;
    }
    if (value < 0x10000) {
        out[0] = (uint8_t)(0xE0 | (value >> 12));
        out[1] = (uint8_t)(0x80 | ((value >> 6) & 0x3F));
        out[2] = (uint8_t)(0x80 | (value & 0x3F));
        return 3;
    }
    out[0] = (uint8_t)(0xF0 | (value >> 18));
    out[1] = (uint8_t)(0x80 | ((value >> 12) & 0x3F));
    out[2] = (uint8_t)(0x80 | ((value >> 6) & 0x3F));
    out[3] = (uint8_t)(0x80 | (value & 0x3F));
    return 4;
}

const hex_string *hex_string_from_bytes(hex_heap h, const uint8_t *data, size_t length) {
    // The complete sequence validates before any allocation.
    size_t index = 0;
    while (index < length) {
        hex_utf8_next(data, length, &index);
    }
    // The header, payload, and terminator chain is checked with ckd_add
    // before the raw allocator sees any sum; every stage traps with the
    // same allocation-size message.
    size_t total;
    if (ckd_add(&total, sizeof(hex_string_storage), length) ||
        ckd_add(&total, total, 1)) {
        hex_runtime_trap("[Runtime Error] string allocation size overflow\n");
    }
    hex_string_storage *storage = hex_heap_raw_allocate(h.identity, total, _Alignof(hex_string_storage));
    storage->header.data = storage->bytes;
    storage->header.byte_length = length;
    // A zero-length payload skips the guarded memcpy so a possibly invalid
    // source pointer is never passed to a standard memory function.
    if (length != 0) {
        memcpy(storage->bytes, data, length);
    }
    storage->bytes[length] = 0;
    return &storage->header;
}

const hex_string *hex_string_from_runes(hex_heap h, const uint32_t *data, size_t length) {
    // Every scalar validates as the byte count accumulates with ckd_add,
    // so the single allocation below encodes a validated result.
    size_t bytes = 0;
    for (size_t index = 0; index < length; index++) {
        uint32_t value = data[index];
        if (value > 0x10FFFF || (value >= 0xD800 && value <= 0xDFFF)) {
            hex_runtime_trap("[Runtime Error] invalid Unicode scalar value\n");
        }
        size_t width;
        if (value < 0x80) {
            width = 1;
        } else if (value < 0x800) {
            width = 2;
        } else if (value < 0x10000) {
            width = 3;
        } else {
            width = 4;
        }
        if (ckd_add(&bytes, bytes, width)) {
            hex_runtime_trap("[Runtime Error] string allocation size overflow\n");
        }
    }
    // The header, payload, and terminator chain is checked with ckd_add
    // before the raw allocator sees any sum.
    size_t total;
    if (ckd_add(&total, sizeof(hex_string_storage), bytes) ||
        ckd_add(&total, total, 1)) {
        hex_runtime_trap("[Runtime Error] string allocation size overflow\n");
    }
    hex_string_storage *storage = hex_heap_raw_allocate(h.identity, total, _Alignof(hex_string_storage));
    size_t out = 0;
    for (size_t index = 0; index < length; index++) {
        out += hex_utf8_encode(storage->bytes + out, data[index]);
    }
    storage->header.data = storage->bytes;
    storage->header.byte_length = bytes;
    storage->bytes[bytes] = 0;
    return &storage->header;
}

const hex_string *hex_string_to_string(hex_heap h, const hex_string *text) {
    return hex_string_from_bytes(h, text->data, text->byte_length);
}

const hex_string *hex_string_concat(hex_heap h, const hex_string *left, const hex_string *right) {
    // The combined payload, header, and terminator chain is checked
    // with ckd_add before the raw allocator sees any sum; every stage
    // traps with the same concatenation-length message.
    size_t length;
    if (ckd_add(&length, left->byte_length, right->byte_length)) {
        hex_runtime_trap("[Runtime Error] string concatenation length overflow\n");
    }
    size_t total;
    if (ckd_add(&total, sizeof(hex_string_storage), length) ||
        ckd_add(&total, total, 1)) {
        hex_runtime_trap("[Runtime Error] string concatenation length overflow\n");
    }
    hex_string_storage *storage = hex_heap_raw_allocate(h.identity, total, _Alignof(hex_string_storage));
    storage->header.data = storage->bytes;
    storage->header.byte_length = length;
    // Each input copies with a guarded memcpy; the freshly allocated
    // destination cannot overlap the immutable inputs.
    if (left->byte_length != 0) {
        memcpy(storage->bytes, left->data, left->byte_length);
    }
    if (right->byte_length != 0) {
        memcpy(storage->bytes + left->byte_length, right->data, right->byte_length);
    }
    storage->bytes[length] = 0;
    return &storage->header;
}

void hex_string_free(hex_heap h, const hex_string *text) {
    if (text == nullptr) {
        hex_runtime_trap("[Runtime Error] double deallocation\n");
    }
    size_t offset = *((size_t *)((unsigned char *)text - sizeof(size_t)));
    hex_heap_header *header = (hex_heap_header *)((unsigned char *)text - offset);
    if (header->allocator != h.identity) {
        hex_runtime_trap("[Runtime Error] deallocation used the wrong allocator\n");
    }
    if (!header->live) {
        hex_runtime_trap("[Runtime Error] double deallocation\n");
    }
    header->live = false;
    free(header);
}

size_t hex_string_rune_length(const hex_string *text) {
    size_t index = 0;
    size_t runes = 0;
    while (index < text->byte_length) {
        hex_utf8_next(text->data, text->byte_length, &index);
        runes++;
    }
    return runes;
}

bool hex_string_is_empty(const hex_string *text) {
    return text->byte_length == 0;
}

uint32_t hex_string_at_rune(const hex_string *text, size_t rune_index) {
    size_t index = 0;
    size_t rune = 0;
    for (;;) {
        if (index >= text->byte_length) {
            hex_runtime_trap("[Runtime Error] String index is outside its bounds\n");
        }
        if (rune == rune_index) {
            return hex_utf8_decode(text->data, text->byte_length, &index);
        }
        hex_utf8_next(text->data, text->byte_length, &index);
        rune++;
    }
}

hex_rune_cursor hex_string_rune_cursor(const hex_string *text) {
    return (hex_rune_cursor){ text->data, text->byte_length, 0 };
}

bool hex_rune_cursor_has_next(hex_rune_cursor cursor) {
    return cursor.offset < cursor.length;
}

uint32_t hex_rune_cursor_next(hex_rune_cursor *cursor) {
    if (cursor->offset >= cursor->length) {
        hex_runtime_trap("[Runtime Error] RuneCursor has no next value\n");
    }
    return hex_utf8_decode(cursor->data, cursor->length, &cursor->offset);
}
{{if .NeedStrand}}
size_t hex_strand_rune_length(hex_strand text) {
    size_t index = 0;
    size_t runes = 0;
    while (index < 32 && text.data[index] != 0) {
        hex_utf8_next(text.data, 32, &index);
        runes++;
    }
    return runes;
}

bool hex_strand_is_empty(hex_strand text) {
    return text.data[0] == 0;
}

uint32_t hex_strand_at_rune(hex_strand text, size_t rune_index) {
    size_t index = 0;
    size_t rune = 0;
    for (;;) {
        if (index >= 32 || text.data[index] == 0) {
            hex_runtime_trap("[Runtime Error] String index is outside its bounds\n");
        }
        if (rune == rune_index) {
            return hex_utf8_decode(text.data, 32, &index);
        }
        hex_utf8_next(text.data, 32, &index);
        rune++;
    }
}

const hex_string *hex_strand_to_string(hex_heap h, hex_strand text) {
    size_t length = 0;
    while (length < 32 && text.data[length] != 0) {
        length++;
    }
    return hex_string_from_bytes(h, text.data, length);
}
{{end}}