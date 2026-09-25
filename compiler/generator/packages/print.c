#include "hexal/print.h"
#include "hexal/io.h"
{{if .Event}}#include "hexal/event.h"
{{end}}#include <stdckdint.h>
#include <stdlib.h>

// The builder is the only destination every formatter knows. Growth uses the
// bundled C library allocator rather than Heap, mimalloc, or libuv: the
// storage is call-local, is released before the call returns, and must be
// available to a print-only program that selects none of those runtimes.

void hex_print_begin(hex_print_buffer *out) {
    out->data = out->inline_storage;
    out->length = 0;
    out->capacity = sizeof out->inline_storage;
}

void hex_print_destroy(hex_print_buffer *out) {
    if (out->data != out->inline_storage) {
        free(out->data);
    }
    out->data = out->inline_storage;
    out->length = 0;
    out->capacity = sizeof out->inline_storage;
}

// hex_print_text appends one complete UTF-8 run and is the builder's only
// growth point: appending nothing allocates nothing, and every capacity step
// doubles under checked arithmetic, falling back to the exact requirement
// when doubling would not be representable. A failed growth traps, so no
// prefix of the call has reached standard output.
void hex_print_text(hex_print_buffer *out, const uint8_t *data, size_t length) {
    if (length == 0) {
        return;
    }
    size_t needed = 0;
    if (ckd_add(&needed, out->length, length)) {
        hex_runtime_trap("[Runtime Error] print buffer allocation failed\n");
    }
    if (needed > out->capacity) {
        size_t capacity = out->capacity;
        while (capacity < needed) {
            if (ckd_mul(&capacity, capacity, 2)) {
                capacity = needed;
                break;
            }
        }
        uint8_t *grown;
        if (out->data == out->inline_storage) {
            grown = (uint8_t *)malloc(capacity);
            if (grown != nullptr) {
                for (size_t index = 0; index < out->length; index++) {
                    grown[index] = out->inline_storage[index];
                }
            }
        } else {
            grown = (uint8_t *)realloc(out->data, capacity);
        }
        if (grown == nullptr) {
            hex_runtime_trap("[Runtime Error] print buffer allocation failed\n");
        }
        out->data = grown;
        out->capacity = capacity;
    }
    for (size_t index = 0; index < length; index++) {
        out->data[out->length + index] = data[index];
    }
    out->length = needed;
}

#ifdef _WIN32

#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
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

// hex_print_console_write_all converts and writes one complete print call to
// an attached console inside a single critical section, so chunking stays an
// internal detail no other writer can land between. Each chunk is at most
// HEX_PRINT_CONSOLE_CHUNK bytes, backed off to the nearest earlier UTF-8
// scalar boundary (a continuation byte is never the first byte after a chunk
// boundary), so a multibyte scalar and the UTF-16 surrogate pair it can
// convert to are never split across chunks. Each converted chunk is submitted
// until every UTF-16 unit is written; a short successful write continues,
// while zero progress or a native failure reports failure to the caller.
static bool hex_print_console_write_all(HANDLE handle, const uint8_t *data, size_t length) {
    constexpr size_t HEX_PRINT_CONSOLE_CHUNK = 512;
    bool ok = true;
{{if .Event}}    hex_stdout_lock();
{{end}}    size_t offset = 0;
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
            ok = false;
            break;
        }
        DWORD units = (DWORD)wide_needed;
        DWORD done = 0;
        while (done < units) {
            DWORD written = 0;
            if (!WriteConsoleW(handle, wide + done, units - done, &written, nullptr) || written == 0) {
                ok = false;
                break;
            }
            done += written;
        }
        if (!ok) {
            break;
        }
        offset += chunk;
    }
{{if .Event}}    hex_stdout_unlock();
{{end}}    return ok;
}

#endif

// hex_print_commit_native is the whole standard-output transfer of one print
// call. On an attached Windows console it performs the classification, the
// UTF-8-to-UTF-16 conversion, and every console submission itself; everywhere
// else it hands the complete byte sequence to the serialized descriptor loop
// IO.write shares. It issues no work submission of its own, so the call
// remains one native job.
static bool hex_print_commit_native(const uint8_t *data, size_t length) {
#ifdef _WIN32
    HANDLE console;
    if (hex_print_console_handle(&console)) {
        return hex_print_console_write_all(console, data, length);
    }
#endif
    return hex_io_stdout_write_all(data, length);
}
{{if .Event}}
typedef struct hex_print_commit_job {
    const uint8_t *data;
    size_t length;
    bool result;
} hex_print_commit_job;

static void hex_print_commit_entry(void *raw) {
    hex_print_commit_job *job = (hex_print_commit_job *)raw;
    job->result = hex_print_commit_native(job->data, job->length);
}

static void hex_print_commit_failure(void *raw) {
    hex_print_commit_job *job = (hex_print_commit_job *)raw;
    job->result = false;
}
{{end}}
// hex_print_commit submits the complete call once. Outside a Task the native
// transfer runs directly; inside one it is a single worker job, so a print
// call costs one submission however many fragments it formatted. A failure
// may already have written a prefix, so the call is never retried.
void hex_print_commit(hex_print_buffer *out) {
    if (out->length == 0) {
        return;
    }
{{if .Event}}    hex_print_commit_job job = {.data = out->data, .length = out->length};
    hex_event_work_call(hex_print_commit_entry, hex_print_commit_failure, &job);
    if (!job.result) {
{{else}}    if (!hex_print_commit_native(out->data, out->length)) {
{{end}}        hex_runtime_trap("[Runtime Error] standard output write failed\n");
    }
}
void hex_print_bool(hex_print_buffer *out, bool value) {
    if (value) { hex_print_text(out, (const uint8_t *)"true", 4); } else { hex_print_text(out, (const uint8_t *)"false", 5); }
}
void hex_print_nil(hex_print_buffer *out) {
    hex_print_text(out, (const uint8_t *)"nil", 3);
}
void hex_print_int8(hex_print_buffer *out, int8_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRId8, value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_uint8(hex_print_buffer *out, uint8_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRIu8, value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_int16(hex_print_buffer *out, int16_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRId16, value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_uint16(hex_print_buffer *out, uint16_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRIu16, value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_int32(hex_print_buffer *out, int32_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRId32, value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_uint32(hex_print_buffer *out, uint32_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRIu32, value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_int64(hex_print_buffer *out, int64_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRId64, value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_uint64(hex_print_buffer *out, uint64_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%" PRIu64, value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_size(hex_print_buffer *out, size_t value) {
    char buffer[32];
    int n = snprintf(buffer, sizeof buffer, "%zu", value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_float64(hex_print_buffer *out, double value) {
    if (isnan(value)) { hex_print_text(out, (const uint8_t *)"nan", 3); return; }
    if (isinf(value)) { hex_print_text(out, (const uint8_t *)"inf", 3); return; }
    if (signbit(value)) { hex_print_text(out, (const uint8_t *)"-", 1); value = -value; }
    if (value == 0.0) { hex_print_text(out, (const uint8_t *)"0", 1); return; }
    char buffer[64];
    int n = snprintf(buffer, sizeof buffer, "%.17g", value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
void hex_print_float32(hex_print_buffer *out, float value) {
    if (isnan(value)) { hex_print_text(out, (const uint8_t *)"nan", 3); return; }
    if (isinf(value)) { hex_print_text(out, (const uint8_t *)"inf", 3); return; }
    if (signbit(value)) { hex_print_text(out, (const uint8_t *)"-", 1); value = -value; }
    if (value == 0.0f) { hex_print_text(out, (const uint8_t *)"0", 1); return; }
    char buffer[64];
    int n = snprintf(buffer, sizeof buffer, "%.9g", value);
    hex_print_text(out, (const uint8_t *)buffer, (size_t)n);
}
// hex_print_quoted_text appends every run of plain (unescaped) bytes between
// escape points in one step instead of one byte at a time: none of the fixed
// ASCII escape triggers below can appear as a UTF-8 continuation byte
// (0x80-0xBF), so a run boundary only ever falls on a scalar boundary.
void hex_print_quoted_text(hex_print_buffer *out, const uint8_t *data, size_t length) {
    hex_print_text(out, (const uint8_t *)"\"", 1);
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
                hex_print_text(out, data + run_start, index - run_start);
            }
            hex_print_text(out, (const uint8_t *)escape, escape_length);
            run_start = index + 1;
        }
    }
    if (index > run_start) {
        hex_print_text(out, data + run_start, index - run_start);
    }
    hex_print_text(out, (const uint8_t *)"\"", 1);
}
