#include "hexal/print.h"
#include "hexal/io.h"
{{if .Event}}#include "hexal/event.h"
{{end}}
#ifdef _WIN32

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

// hex_print_console_handle reports whether the current stdout handle is an
// attached console, classifying it fresh at every logical print call: a
// replaced standard handle therefore affects only the next call, with no
// process-lifetime terminal cache.
static bool hex_print_console_handle(HANDLE *out) {
    HANDLE handle = GetStdHandle(STD_OUTPUT_HANDLE);
    if (handle == nullptr || handle == INVALID_HANDLE_VALUE) {
        return false;
    }
    DWORD mode = 0;
    if (!GetConsoleMode(handle, &mode)) {
        return false;
    }
    *out = handle;
    return true;
}
{{if .Event}}
typedef struct hex_print_console_job {
    HANDLE handle;
    const wchar_t *data;
    DWORD count;
    bool result;
} hex_print_console_job;

static void hex_print_console_entry(void *raw) {
    hex_print_console_job *job = (hex_print_console_job *)raw;
    DWORD written = 0;
    job->result = WriteConsoleW(job->handle, job->data, job->count, &written, nullptr) && written == job->count;
}

static void hex_print_console_failure(void *raw) {
    hex_print_console_job *job = (hex_print_console_job *)raw;
    job->result = false;
}
{{end}}
// hex_print_console_write runs one WriteConsoleW call directly outside a
// Task, or through the existing worker bridge inside one, so a slow console
// never blocks a scheduler worker; the backend does not change, only its
// execution placement.
static bool hex_print_console_write(HANDLE handle, const wchar_t *data, DWORD count) {
{{if .Event}}    hex_print_console_job job = {.handle = handle, .data = data, .count = count};
    hex_event_work_call(hex_print_console_entry, hex_print_console_failure, &job);
    return job.result;
{{else}}    DWORD written = 0;
    return WriteConsoleW(handle, data, count, &written, nullptr) && written == count;
{{end}}}

// hex_print_text_console converts and writes complete UTF-8 scalar chunks
// through a fixed-size stack buffer: no heap allocation and no whole-value
// size limit. Each chunk is at most HEX_PRINT_CONSOLE_CHUNK bytes, backed
// off to the nearest earlier UTF-8 scalar boundary (a continuation byte is
// never the first byte after a chunk boundary), so a multibyte scalar and
// the UTF-16 surrogate pair it can convert to are never split across chunks.
static void hex_print_text_console(HANDLE handle, const uint8_t *data, size_t length) {
    constexpr size_t HEX_PRINT_CONSOLE_CHUNK = 512;
    size_t offset = 0;
    while (offset < length) {
        size_t remaining = length - offset;
        size_t chunk = remaining > HEX_PRINT_CONSOLE_CHUNK ? HEX_PRINT_CONSOLE_CHUNK : remaining;
        while (chunk > 0 && chunk < remaining && (data[offset + chunk] & 0xC0) == 0x80) {
            chunk--;
        }
        if (chunk == 0) {
            chunk = remaining;
        }
        int wide_needed = MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, (const char *)(data + offset), (int)chunk, nullptr, 0);
        wchar_t wide[HEX_PRINT_CONSOLE_CHUNK];
        if (wide_needed <= 0 || (size_t)wide_needed > HEX_PRINT_CONSOLE_CHUNK ||
            MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, (const char *)(data + offset), (int)chunk, wide, wide_needed) != wide_needed) {
            hex_runtime_trap("[Runtime Error] standard output write failed\n");
        }
        if (!hex_print_console_write(handle, wide, (DWORD)wide_needed)) {
            hex_runtime_trap("[Runtime Error] standard output write failed\n");
        }
        offset += chunk;
    }
}

#endif

void hex_print_bytes(const uint8_t *data, size_t length) {
    // The shared descriptor transfer core: print and raw IO writes use one
    // buffering domain on the standard output descriptor.
    if (!hex_io_write_all(hex_io_stdout_desc(), data, length)) {
        hex_runtime_trap("[Runtime Error] standard output write failed\n");
    }
}
// hex_print_text owns terminal-aware conversion: an attached Windows console
// receives correct Unicode text through native UTF-8-to-UTF-16 conversion
// and WriteConsoleW, while redirected output (and every POSIX destination)
// keeps the exact byte sink hex_print_bytes uses. Every textual helper below
// routes a complete UTF-8 scalar sequence through this function; only
// hex_print_bytes and IO.write stay byte-exact.
void hex_print_text(const uint8_t *data, size_t length) {
#ifdef _WIN32
    HANDLE console;
    if (hex_print_console_handle(&console)) {
        hex_print_text_console(console, data, length);
        return;
    }
#endif
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
    hex_print_text(bytes, length);
}
// hex_print_quoted_text batches every run of plain (unescaped) bytes between
// escape points into one hex_print_text call instead of emitting one byte at
// a time: none of the fixed ASCII escape triggers below can appear as a
// UTF-8 continuation byte (0x80-0xBF), so a run boundary only ever falls on
// a scalar boundary, and a multibyte scalar is never split across it.
void hex_print_quoted_text(const uint8_t *data, size_t length) {
    hex_print_text((const uint8_t *)"\"", 1);
    size_t run_start = 0;
    size_t index = 0;
    for (; index < length; index++) {
        uint8_t c = data[index];
        const char *escape = nullptr;
        char hex_escape[6];
        size_t escape_length = 0;
        switch (c) {
        case '\"': escape = "\\\""; escape_length = 2; break;
        case '\\': escape = "\\\\"; escape_length = 2; break;
        case 0: escape = "\\0"; escape_length = 2; break;
        case '\n': escape = "\\n"; escape_length = 2; break;
        case '\r': escape = "\\r"; escape_length = 2; break;
        case '\t': escape = "\\t"; escape_length = 2; break;
        default:
            if (c < 0x20 || c == 0x7F) {
                escape_length = (size_t)snprintf(hex_escape, sizeof hex_escape, "\\x%02X", c);
                escape = hex_escape;
            }
        }
        if (escape != nullptr) {
            if (index > run_start) {
                hex_print_text(data + run_start, index - run_start);
            }
            hex_print_text((const uint8_t *)escape, escape_length);
            run_start = index + 1;
        }
    }
    if (index > run_start) {
        hex_print_text(data + run_start, index - run_start);
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
