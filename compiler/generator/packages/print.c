#include "hexal/print.h"
#include "hexal/io.h"

void hex_print_bytes(const uint8_t *data, size_t length) {
    // The shared descriptor transfer core: print and raw IO writes use one
    // buffering domain on the standard output descriptor.
    if (!hex_io_write_all(hex_io_stdout_desc(), data, length)) {
        hex_runtime_trap("[Runtime Error] standard output write failed\n");
    }
}
void hex_print_text(const uint8_t *data, size_t length) {
    hex_print_bytes(data, length);
}
void hex_print_bool(bool value) {
    if (value) { hex_print_bytes((const uint8_t *)"true", 4); } else { hex_print_bytes((const uint8_t *)"false", 5); }
}
void hex_print_nil(void) {
    hex_print_bytes((const uint8_t *)"nil", 3);
}
void hex_print_int8(int8_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRId8, value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_uint8(uint8_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRIu8, value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_int16(int16_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRId16, value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_uint16(uint16_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRIu16, value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_int32(int32_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRId32, value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_uint32(uint32_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRIu32, value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_int64(int64_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRId64, value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_uint64(uint64_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRIu64, value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_size(size_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%zu", value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_float64(double value) {
    if (isnan(value)) { hex_print_text((const uint8_t *)"nan", 3); return; }
    if (isinf(value)) { hex_print_text((const uint8_t *)"inf", 3); return; }
    if (signbit(value)) { hex_print_text((const uint8_t *)"-", 1); value = -value; }
    if (value == 0.0) { hex_print_text((const uint8_t *)"0", 1); return; }
    char buffer[64];
    int n = snprintf(buffer, sizeof buffer, "%.17g", value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_float32(float value) {
    if (isnan(value)) { hex_print_text((const uint8_t *)"nan", 3); return; }
    if (isinf(value)) { hex_print_text((const uint8_t *)"inf", 3); return; }
    if (signbit(value)) { hex_print_text((const uint8_t *)"-", 1); value = -value; }
    if (value == 0.0f) { hex_print_text((const uint8_t *)"0", 1); return; }
    char buffer[64];
    int n = snprintf(buffer, sizeof buffer, "%.9g", value);
    hex_print_bytes((const uint8_t *)buffer, (size_t)n);
}
void hex_print_rune(uint32_t value) {
    uint8_t bytes[4];
    size_t length = 0;
    if (value < 0x80) { bytes[0] = (uint8_t)value; length = 1; }
    else if (value < 0x800) { bytes[0] = (uint8_t)(0xC0 | (value >> 6)); bytes[1] = (uint8_t)(0x80 | (value & 0x3F)); length = 2; }
    else if (value < 0x10000) { bytes[0] = (uint8_t)(0xE0 | (value >> 12)); bytes[1] = (uint8_t)(0x80 | ((value >> 6) & 0x3F)); bytes[2] = (uint8_t)(0x80 | (value & 0x3F)); length = 3; }
    else { bytes[0] = (uint8_t)(0xF0 | (value >> 18)); bytes[1] = (uint8_t)(0x80 | ((value >> 12) & 0x3F)); bytes[2] = (uint8_t)(0x80 | ((value >> 6) & 0x3F)); bytes[3] = (uint8_t)(0x80 | (value & 0x3F)); length = 4; }
    hex_print_bytes(bytes, length);
}
void hex_print_quoted_text(const uint8_t *data, size_t length) {
    hex_print_text((const uint8_t *)"\"", 1);
    for (size_t index = 0; index < length; index++) {
        uint8_t c = data[index];
        switch (c) {
        case '\"': hex_print_text((const uint8_t *)"\\\"", 2); break;
        case '\\': hex_print_text((const uint8_t *)"\\\\", 2); break;
        case 0: hex_print_text((const uint8_t *)"\\0", 2); break;
        case '\n': hex_print_text((const uint8_t *)"\\n", 2); break;
        case '\r': hex_print_text((const uint8_t *)"\\r", 2); break;
        case '\t': hex_print_text((const uint8_t *)"\\t", 2); break;
        default:
            if (c < 0x20 || c == 0x7F) {
                char escape[6];
                int n = snprintf(escape, sizeof escape, "\\x%02X", c);
                hex_print_bytes((const uint8_t *)escape, (size_t)n);
            } else {
                hex_print_bytes((const uint8_t *)&c, 1);
            }
        }
    }
    hex_print_text((const uint8_t *)"\"", 1);
}
void hex_print_quoted_rune(uint32_t value) {
    hex_print_text((const uint8_t *)"'", 1);
    switch (value) {
    case '\\': hex_print_text((const uint8_t *)"\\\\", 2); break;
    case 0: hex_print_text((const uint8_t *)"\\0", 2); break;
    case '\n': hex_print_text((const uint8_t *)"\\n", 2); break;
    case '\r': hex_print_text((const uint8_t *)"\\r", 2); break;
    case '\t': hex_print_text((const uint8_t *)"\\t", 2); break;
    default:
        if (value < 0x20 || value == 0x7F) {
            char escape[16];
            int n = snprintf(escape, sizeof escape, "\\u{%X}", value);
            hex_print_bytes((const uint8_t *)escape, (size_t)n);
        } else {
            hex_print_rune(value);
        }
    }
    hex_print_text((const uint8_t *)"'", 1);
}
