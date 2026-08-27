#include "hexal/io.h"
{{if .Blocking}}#include "hexal/concurrency.h"
{{end}}#include <stdckdint.h>

#ifdef _WIN32

#define WIN32_LEAN_AND_MEAN
#include <windows.h>

static_assert(sizeof(intptr_t) >= sizeof(void *), "HANDLE requires an intptr_t at least pointer width");

constexpr uint32_t HEX_IO_STDIN_ID = STD_INPUT_HANDLE;
constexpr uint32_t HEX_IO_STDOUT_ID = STD_OUTPUT_HANDLE;
constexpr uint32_t HEX_IO_STDERR_ID = STD_ERROR_HANDLE;

#else

#include <errno.h>
#include <unistd.h>
#include <fcntl.h>
#include <limits.h>

static_assert(sizeof(intptr_t) >= sizeof(int), "a POSIX descriptor requires an intptr_t at least int width");

constexpr intptr_t HEX_IO_STDIN_ID = 0;
constexpr intptr_t HEX_IO_STDOUT_ID = 1;
constexpr intptr_t HEX_IO_STDERR_ID = 2;

#endif

constexpr uint8_t HEX_IO_ACCESS_READ = 1;
constexpr uint8_t HEX_IO_ACCESS_WRITE = 2;

// The widest request one transfer call may name: POSIX reads and writes take
// a signed byte count, Windows ones a 32-bit count. Larger caller ceilings
// clamp to this bound and the returned Size reports what the one call moved.
#ifdef _WIN32
constexpr size_t HEX_IO_MAX_REQUEST = 0xFFFFFFFFull;
#else
constexpr size_t HEX_IO_MAX_REQUEST = (size_t)SSIZE_MAX;
#endif

[[noreturn]] extern void hex_runtime_trap(const char *message);

// Renders "IO <operation> <errno|winerr>=<code>" into the inline Strand
// payload: one terminating NUL, zero-filled tail. Every operation name and
// supported code width fits 31 payload bytes by construction; exceeding the
// bound is a compiler-contract violation and traps rather than truncating.
static hex_strand hex_io_header(const char *operation, bool windows_codes, long long code) {
    static const char digits[] = "0123456789";
    char composed[64];
    size_t used = 0;
    for (const char *part = "IO "; *part != '\0'; part++) {
        composed[used++] = *part;
    }
    for (const char *part = operation; *part != '\0'; part++) {
        composed[used++] = *part;
    }
    for (const char *part = windows_codes ? " winerr=" : " errno="; *part != '\0'; part++) {
        composed[used++] = *part;
    }
    unsigned long long magnitude = windows_codes ? (unsigned long long)(uint32_t)code : (unsigned long long)code;
    char reversed[24];
    size_t width = 0;
    do {
        reversed[width++] = digits[magnitude % 10];
        magnitude /= 10;
    } while (magnitude != 0);
    while (width > 0) {
        composed[used++] = reversed[--width];
    }
    if (used > 31) {
        hex_runtime_trap("[Runtime Error] IO error header exceeded its inline capacity\n");
    }
    hex_strand header = {{"{0}"}};
    for (size_t index = 0; index < used; index++) {
        header.data[index] = (uint8_t)composed[index];
    }
    return header;
}

hex_t_Error hex_io_error(size_t line, size_t column, const hex_string *file, const char *operation, long long code, const hex_string *message) {
#ifdef _WIN32
    hex_strand header = hex_io_header(operation, true, code);
#else
    hex_strand header = hex_io_header(operation, false, code);
#endif
    return (hex_t_Error){
        .hex_m_file = file,
        .hex_m_line = line,
        .hex_m_column = column,
        .hex_m_header = header,
        .hex_m_message = message,
    };
}

#ifdef _WIN32

static hex_io_open hex_io_open_handle(HANDLE handle, uint8_t access) {
    if (handle == nullptr || handle == INVALID_HANDLE_VALUE) {
        return (hex_io_open){.status = HEX_IO_ERROR, .stream = {0}, .code = (long long)GetLastError()};
    }
    return (hex_io_open){.status = HEX_IO_OK, .stream = {.desc = (intptr_t)handle, .access = access, .owned = false}, .code = 0};
}

hex_io_open hex_io_stdin(void) {
    return hex_io_open_handle(GetStdHandle(HEX_IO_STDIN_ID), HEX_IO_ACCESS_READ);
}

hex_io_open hex_io_stdout(void) {
    return hex_io_open_handle(GetStdHandle(HEX_IO_STDOUT_ID), HEX_IO_ACCESS_WRITE);
}

hex_io_open hex_io_stderr(void) {
    return hex_io_open_handle(GetStdHandle(HEX_IO_STDERR_ID), HEX_IO_ACCESS_WRITE);
}

intptr_t hex_io_stdout_desc(void) {
    return (intptr_t)GetStdHandle(HEX_IO_STDOUT_ID);
}

// The descriptor read path: capability gate first, then the caller-ceiling
// reservation through the List component's checked helper, then one platform
// call. A capability mismatch never allocates.

static hex_io_transfer hex_io_read_transfer(hex_io stream, uint8_t *target, size_t request);
static hex_io_transfer hex_io_write_transfer(hex_io stream, const uint8_t *data, size_t request);
static hex_io_position hex_io_seek_move(hex_io stream, int64_t offset, int whence);
static hex_io_status_only hex_io_close_native(hex_io stream);
static bool hex_io_write_all_native(intptr_t desc, const uint8_t *data, size_t length);
{{if .Blocking}}
typedef struct hex_io_read_job {
    hex_io stream;
    uint8_t *target;
    size_t request;
    hex_io_transfer result;
} hex_io_read_job;

static void hex_io_read_entry(void *raw) {
    hex_io_read_job *job = (hex_io_read_job *)raw;
    job->result = hex_io_read_transfer(job->stream, job->target, job->request);
}

typedef struct hex_io_write_job {
    hex_io stream;
    const uint8_t *data;
    size_t request;
    hex_io_transfer result;
} hex_io_write_job;

static void hex_io_write_entry(void *raw) {
    hex_io_write_job *job = (hex_io_write_job *)raw;
    job->result = hex_io_write_transfer(job->stream, job->data, job->request);
}

typedef struct hex_io_seek_job {
    hex_io stream;
    int64_t offset;
    int whence;
    hex_io_position result;
} hex_io_seek_job;

static void hex_io_seek_entry(void *raw) {
    hex_io_seek_job *job = (hex_io_seek_job *)raw;
    job->result = hex_io_seek_move(job->stream, job->offset, job->whence);
}

typedef struct hex_io_close_job {
    hex_io stream;
    hex_io_status_only result;
} hex_io_close_job;

static void hex_io_close_entry(void *raw) {
    hex_io_close_job *job = (hex_io_close_job *)raw;
    job->result = hex_io_close_native(job->stream);
}

typedef struct hex_io_write_all_job {
    intptr_t desc;
    const uint8_t *data;
    size_t length;
    bool result;
} hex_io_write_all_job;

static void hex_io_write_all_entry(void *raw) {
    hex_io_write_all_job *job = (hex_io_write_all_job *)raw;
    job->result = hex_io_write_all_native(job->desc, job->data, job->length);
}
{{end}}

hex_io_transfer hex_io_read(hex_io stream, hex_list_UInt8 *into, size_t max) {
    if ((stream.access & HEX_IO_ACCESS_READ) == 0) {
        return (hex_io_transfer){.status = HEX_IO_NOT_READABLE};
    }
    if (max == 0) {
        return (hex_io_transfer){.status = HEX_IO_OK, .count = 0};
    }
    size_t request = max > HEX_IO_MAX_REQUEST ? HEX_IO_MAX_REQUEST : max;
    size_t needed = 0;
    if (ckd_add(&needed, into->length, request)) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    hex_list_reserve_at_least_UInt8(into, needed);
    uint8_t *target = into->data + into->length;
{{if .Blocking}}    hex_io_read_job job = {.stream = stream, .target = target, .request = request};
    hex_blocking_call(hex_io_read_entry, &job);
    hex_io_transfer transfer = job.result;
{{else}}    hex_io_transfer transfer = hex_io_read_transfer(stream, target, request);
{{end}}    if (transfer.status == HEX_IO_OK) {
        into->length += transfer.count;
    }
    return transfer;
}

static hex_io_transfer hex_io_read_transfer(hex_io stream, uint8_t *target, size_t request) {
    DWORD moved = 0;
    if (!ReadFile((HANDLE)stream.desc, (LPVOID)target, (DWORD)request, &moved, nullptr)) {
        return (hex_io_transfer){.status = HEX_IO_ERROR, .count = 0, .code = (long long)GetLastError()};
    }
    if (moved == 0) {
        return (hex_io_transfer){.status = HEX_IO_EOS};
    }
    return (hex_io_transfer){.status = HEX_IO_OK, .count = (size_t)moved};
}

hex_io_transfer hex_io_write(hex_io stream, hex_view_UInt8 from) {
    if ((stream.access & HEX_IO_ACCESS_WRITE) == 0) {
        return (hex_io_transfer){.status = HEX_IO_NOT_WRITABLE};
    }
    if (from.length == 0) {
        return (hex_io_transfer){.status = HEX_IO_OK, .count = 0};
    }
    size_t request = from.length > HEX_IO_MAX_REQUEST ? HEX_IO_MAX_REQUEST : from.length;
{{if .Blocking}}    hex_io_write_job job = {.stream = stream, .data = from.data, .request = request};
    hex_blocking_call(hex_io_write_entry, &job);
    return job.result;
{{else}}    return hex_io_write_transfer(stream, from.data, request);
{{end}}}

static hex_io_transfer hex_io_write_transfer(hex_io stream, const uint8_t *data, size_t request) {
    DWORD moved = 0;
    if (!WriteFile((HANDLE)stream.desc, data, (DWORD)request, &moved, nullptr)) {
        return (hex_io_transfer){.status = HEX_IO_ERROR, .count = 0, .code = (long long)GetLastError()};
    }
    return (hex_io_transfer){.status = HEX_IO_OK, .count = (size_t)moved};
}

static hex_io_position hex_io_seek_move(hex_io stream, int64_t offset, int whence) {
    DWORD method;
    switch (whence) {
    case 0:
        method = FILE_BEGIN;
        break;
    case 1:
        method = FILE_CURRENT;
        break;
    default:
        method = FILE_END;
        break;
    }
    LARGE_INTEGER target;
    target.QuadPart = (LONGLONG)offset;
    LARGE_INTEGER moved;
    if (!SetFilePointerEx((HANDLE)stream.desc, target, &moved, method)) {
        return (hex_io_position){.status = HEX_IO_ERROR, .code = (long long)GetLastError()};
    }
    return (hex_io_position){.status = HEX_IO_OK, .position = (long long)moved.QuadPart};
}

hex_io_position hex_io_seek_start(hex_io stream, uint64_t position) {
{{if .Blocking}}    hex_io_seek_job job = {.stream = stream, .offset = (int64_t)position, .whence = 0};
    hex_blocking_call(hex_io_seek_entry, &job);
    return job.result;
{{else}}    return hex_io_seek_move(stream, (int64_t)position, 0);
{{end}}}

hex_io_position hex_io_seek_current(hex_io stream, int64_t offset) {
{{if .Blocking}}    hex_io_seek_job job = {.stream = stream, .offset = offset, .whence = 1};
    hex_blocking_call(hex_io_seek_entry, &job);
    return job.result;
{{else}}    return hex_io_seek_move(stream, offset, 1);
{{end}}}

hex_io_position hex_io_seek_end(hex_io stream, int64_t offset) {
{{if .Blocking}}    hex_io_seek_job job = {.stream = stream, .offset = offset, .whence = 2};
    hex_blocking_call(hex_io_seek_entry, &job);
    return job.result;
{{else}}    return hex_io_seek_move(stream, offset, 2);
{{end}}}

hex_io_status_only hex_io_close(hex_io stream) {
    if (!stream.owned) {
        hex_runtime_trap("[Runtime Error] close of a borrowed stream\n");
    }
{{if .Blocking}}    hex_io_close_job job = {.stream = stream};
    hex_blocking_call(hex_io_close_entry, &job);
    return job.result;
{{else}}    return hex_io_close_native(stream);
{{end}}}

static hex_io_status_only hex_io_close_native(hex_io stream) {
    if (!CloseHandle((HANDLE)stream.desc)) {
        return (hex_io_status_only){.status = HEX_IO_ERROR, .code = (long long)GetLastError()};
    }
    return (hex_io_status_only){.status = HEX_IO_OK};
}

bool hex_io_write_all(intptr_t desc, const uint8_t *data, size_t length) {
{{if .Blocking}}    hex_io_write_all_job job = {.desc = desc, .data = data, .length = length};
    hex_blocking_call(hex_io_write_all_entry, &job);
    return job.result;
{{else}}    return hex_io_write_all_native(desc, data, length);
{{end}}}

static bool hex_io_write_all_native(intptr_t desc, const uint8_t *data, size_t length) {
    size_t written = 0;
    while (written < length) {
        DWORD moved = 0;
        size_t remaining = length - written;
        DWORD request = remaining > HEX_IO_MAX_REQUEST ? (DWORD)HEX_IO_MAX_REQUEST : (DWORD)remaining;
        if (!WriteFile((HANDLE)desc, data + written, request, &moved, nullptr) || moved == 0) {
            return false;
        }
        written += (size_t)moved;
    }
    return true;
}

#else

static hex_io_open hex_io_open_descriptor(intptr_t desc, uint8_t access) {
    if (fcntl((int)desc, F_GETFL) == -1) {
        return (hex_io_open){.status = HEX_IO_ERROR, .stream = {0}, .code = (long long)errno};
    }
    return (hex_io_open){.status = HEX_IO_OK, .stream = {.desc = desc, .access = access, .owned = false}, .code = 0};
}

hex_io_open hex_io_stdin(void) {
    return hex_io_open_descriptor(HEX_IO_STDIN_ID, HEX_IO_ACCESS_READ);
}

hex_io_open hex_io_stdout(void) {
    return hex_io_open_descriptor(HEX_IO_STDOUT_ID, HEX_IO_ACCESS_WRITE);
}

hex_io_open hex_io_stderr(void) {
    return hex_io_open_descriptor(HEX_IO_STDERR_ID, HEX_IO_ACCESS_WRITE);
}

intptr_t hex_io_stdout_desc(void) {
    return HEX_IO_STDOUT_ID;
}

static hex_io_transfer hex_io_read_transfer(hex_io stream, uint8_t *target, size_t request) {
    ssize_t moved = read((int)stream.desc, target, request);
    if (moved < 0) {
        return (hex_io_transfer){.status = HEX_IO_ERROR, .count = 0, .code = (long long)errno};
    }
    if (moved == 0) {
        return (hex_io_transfer){.status = HEX_IO_EOS};
    }
    return (hex_io_transfer){.status = HEX_IO_OK, .count = (size_t)moved};
}

hex_io_transfer hex_io_read(hex_io stream, hex_list_UInt8 *into, size_t max) {
    if ((stream.access & HEX_IO_ACCESS_READ) == 0) {
        return (hex_io_transfer){.status = HEX_IO_NOT_READABLE};
    }
    if (max == 0) {
        return (hex_io_transfer){.status = HEX_IO_OK, .count = 0};
    }
    size_t request = max > HEX_IO_MAX_REQUEST ? HEX_IO_MAX_REQUEST : max;
    size_t needed = 0;
    if (ckd_add(&needed, into->length, request)) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    hex_list_reserve_at_least_UInt8(into, needed);
    uint8_t *target = into->data + into->length;
{{if .Blocking}}    hex_io_read_job job = {.stream = stream, .target = target, .request = request};
    hex_blocking_call(hex_io_read_entry, &job);
    hex_io_transfer transfer = job.result;
{{else}}    hex_io_transfer transfer = hex_io_read_transfer(stream, target, request);
{{end}}    if (transfer.status == HEX_IO_OK) {
        into->length += transfer.count;
    }
    return transfer;
}

static hex_io_transfer hex_io_write_transfer(hex_io stream, const uint8_t *data, size_t request) {
    ssize_t moved = write((int)stream.desc, data, request);
    if (moved < 0) {
        return (hex_io_transfer){.status = HEX_IO_ERROR, .count = 0, .code = (long long)errno};
    }
    return (hex_io_transfer){.status = HEX_IO_OK, .count = (size_t)moved};
}

hex_io_transfer hex_io_write(hex_io stream, hex_view_UInt8 from) {
    if ((stream.access & HEX_IO_ACCESS_WRITE) == 0) {
        return (hex_io_transfer){.status = HEX_IO_NOT_WRITABLE};
    }
    if (from.length == 0) {
        return (hex_io_transfer){.status = HEX_IO_OK, .count = 0};
    }
    size_t request = from.length > HEX_IO_MAX_REQUEST ? HEX_IO_MAX_REQUEST : from.length;
{{if .Blocking}}    hex_io_write_job job = {.stream = stream, .data = from.data, .request = request};
    hex_blocking_call(hex_io_write_entry, &job);
    return job.result;
{{else}}    return hex_io_write_transfer(stream, from.data, request);
{{end}}}

static hex_io_position hex_io_seek_move(hex_io stream, int64_t offset, int whence) {
    off_t result = lseek((int)stream.desc, (off_t)offset, whence);
    if (result == (off_t)-1) {
        return (hex_io_position){.status = HEX_IO_ERROR, .code = (long long)errno};
    }
    return (hex_io_position){.status = HEX_IO_OK, .position = (long long)result};
}

hex_io_position hex_io_seek_start(hex_io stream, uint64_t position) {
    if (position > (uint64_t)INT64_MAX) {
        return (hex_io_position){.status = HEX_IO_ERROR, .code = (long long)EINVAL};
    }
{{if .Blocking}}    hex_io_seek_job job = {.stream = stream, .offset = (int64_t)position, .whence = SEEK_SET};
    hex_blocking_call(hex_io_seek_entry, &job);
    return job.result;
{{else}}    return hex_io_seek_move(stream, (int64_t)position, SEEK_SET);
{{end}}}

hex_io_position hex_io_seek_current(hex_io stream, int64_t offset) {
{{if .Blocking}}    hex_io_seek_job job = {.stream = stream, .offset = offset, .whence = SEEK_CUR};
    hex_blocking_call(hex_io_seek_entry, &job);
    return job.result;
{{else}}    return hex_io_seek_move(stream, offset, SEEK_CUR);
{{end}}}

hex_io_position hex_io_seek_end(hex_io stream, int64_t offset) {
{{if .Blocking}}    hex_io_seek_job job = {.stream = stream, .offset = offset, .whence = SEEK_END};
    hex_blocking_call(hex_io_seek_entry, &job);
    return job.result;
{{else}}    return hex_io_seek_move(stream, offset, SEEK_END);
{{end}}}

// A close interrupted by a signal leaves the release state unspecified, so
// it is reported as failure and never retried: a retry could close a reused
// descriptor.
static hex_io_status_only hex_io_close_native(hex_io stream) {
    if (close((int)stream.desc) != 0) {
        return (hex_io_status_only){.status = HEX_IO_ERROR, .code = (long long)errno};
    }
    return (hex_io_status_only){.status = HEX_IO_OK};
}

hex_io_status_only hex_io_close(hex_io stream) {
    if (!stream.owned) {
        hex_runtime_trap("[Runtime Error] close of a borrowed stream\n");
    }
{{if .Blocking}}    hex_io_close_job job = {.stream = stream};
    hex_blocking_call(hex_io_close_entry, &job);
    return job.result;
{{else}}    return hex_io_close_native(stream);
{{end}}}

static bool hex_io_write_all_native(intptr_t desc, const uint8_t *data, size_t length) {
    size_t written = 0;
    while (written < length) {
        size_t remaining = length - written;
        size_t request = remaining > HEX_IO_MAX_REQUEST ? HEX_IO_MAX_REQUEST : remaining;
        ssize_t moved = write((int)desc, data + written, request);
        if (moved < 0) {
            if (errno == EINTR) {
                continue;
            }
            return false;
        }
        if (moved == 0) {
            return false;
        }
        written += (size_t)moved;
    }
    return true;
}

bool hex_io_write_all(intptr_t desc, const uint8_t *data, size_t length) {
{{if .Blocking}}    hex_io_write_all_job job = {.desc = desc, .data = data, .length = length};
    hex_blocking_call(hex_io_write_all_entry, &job);
    return job.result;
{{else}}    return hex_io_write_all_native(desc, data, length);
{{end}}}

#endif
// The memory backend shares the transfer shapes but never issues a platform
// call. Self-aliasing rejects before any reserve, copy, cursor movement, or
// List mutation; every reserve rides the List component's checked helper.

hex_io_transfer hex_bytes_read(hex_bytes *stream, hex_list_UInt8 *into, size_t max) {
    if (into == stream->buffer) {
        return (hex_io_transfer){.status = HEX_IO_SELF_READ};
    }
    if (max == 0) {
        return (hex_io_transfer){.status = HEX_IO_OK, .count = 0};
    }
    size_t request = max > HEX_IO_MAX_REQUEST ? HEX_IO_MAX_REQUEST : max;
    size_t needed = 0;
    if (ckd_add(&needed, into->length, request)) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    hex_list_reserve_at_least_UInt8(into, needed);
    if (into->capacity - into->length < request) {
        return (hex_io_transfer){.status = HEX_IO_ERROR, .count = 0, .code = 0};
    }
    size_t available = stream->buffer->length - stream->cursor;
    if (available == 0) {
        return (hex_io_transfer){.status = HEX_IO_EOS};
    }
    size_t count = request < available ? request : available;
    for (size_t index = 0; index < count; index++) {
        into->data[into->length + index] = stream->buffer->data[stream->cursor + index];
    }
    stream->cursor += count;
    into->length += count;
    return (hex_io_transfer){.status = HEX_IO_OK, .count = count};
}

static bool hex_io_regions_overlap(const uint8_t *left, size_t left_length, const uint8_t *right, size_t right_length) {
    if (left == nullptr || right == nullptr || left_length == 0 || right_length == 0) {
        return false;
    }
    // The qualified profiles guarantee uintptr_t is a flat numeric address
    // space, so interval overlap tests on integers never compare unrelated C
    // pointers relationally.
    uintptr_t left_base = (uintptr_t)left;
    uintptr_t right_base = (uintptr_t)right;
    uintptr_t left_end = 0;
    uintptr_t right_end = 0;
    if (ckd_add(&left_end, left_base, left_length) || ckd_add(&right_end, right_base, right_length)) {
        hex_runtime_trap("[Runtime Error] stream region bounds are not representable\n");
    }
    return left_base < right_end && right_base < left_end;
}

hex_io_transfer hex_bytes_write(hex_bytes *stream, hex_view_UInt8 from) {
    if (from.length != 0 && stream->buffer->capacity != 0 &&
        hex_io_regions_overlap(from.data, from.length, stream->buffer->data, stream->buffer->capacity)) {
        return (hex_io_transfer){.status = HEX_IO_OVERLAP};
    }
    if (from.length == 0) {
        return (hex_io_transfer){.status = HEX_IO_OK, .count = 0};
    }
    size_t end = 0;
    if (ckd_add(&end, stream->cursor, from.length)) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    hex_list_reserve_at_least_UInt8(stream->buffer, end);
    for (size_t index = 0; index < from.length; index++) {
        stream->buffer->data[stream->cursor + index] = from.data[index];
    }
    if (end > stream->buffer->length) {
        stream->buffer->length = end;
    }
    stream->cursor = end;
    return (hex_io_transfer){.status = HEX_IO_OK, .count = from.length};
}

hex_io_position hex_bytes_seek_from(hex_bytes *stream, uint8_t whence, int64_t offset) {
    long long target = 0;
    switch (whence) {
    case 0:
        target = (long long)(uint64_t)offset;
        break;
    case 1:
        if (ckd_add(&target, (long long)stream->cursor, offset)) {
            return (hex_io_position){.status = HEX_IO_ERROR, .code = 0};
        }
        break;
    default:
        if (ckd_add(&target, (long long)stream->buffer->length, offset)) {
            return (hex_io_position){.status = HEX_IO_ERROR, .code = 0};
        }
        break;
    }
    if (target < 0 || (unsigned long long)target > stream->buffer->length) {
        return (hex_io_position){.status = HEX_IO_ERROR, .code = 0};
    }
    stream->cursor = (size_t)target;
    return (hex_io_position){.status = HEX_IO_OK, .position = target};
}

hex_bytes hex_bytes_over(hex_list_UInt8 *buffer) {
    return (hex_bytes){.buffer = buffer, .cursor = 0};
}
