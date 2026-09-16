#include "hexal/program.h"

#include <stddef.h>
#include <stdint.h>
{{if .Paths}}
#include "hexal/handle.h"

#include <stdckdint.h>
#include <stdlib.h>
#include <string.h>
#include <uv.h>

#if defined(_WIN32)
#include <windows.h>
#include <shellapi.h>
#endif
{{end}}
{{if .Arguments}}
#include <stdckdint.h>
#include <stdlib.h>
#include <string.h>

#if defined(_WIN32)
#include <windows.h>
#include <shellapi.h>
#endif
{{end}}

// hex_program_validate_utf8 mirrors hex_utf8_next's branching exactly, but
// reports invalid encoding by returning false instead of trapping: host path
// and argument bytes are untrusted input, never a Hexal-proven-valid String.
static bool hex_program_validate_utf8(const uint8_t *data, size_t length, size_t *rune_length) {
    size_t index = 0;
    size_t runes = 0;
    while (index < length) {
        uint8_t lead = data[index];
        size_t width;
        if (lead < 0x80) {
            width = 1;
        } else {
            if (lead < 0xC2 || lead >= 0xF5 || index + 2 > length) {
                return false;
            }
            uint8_t first = data[index + 1];
            if ((lead == 0xE0 && first < 0xA0) || (lead == 0xED && first >= 0xA0) ||
                (lead == 0xF0 && first < 0x90) || (lead == 0xF4 && first >= 0x90)) {
                return false;
            }
            width = (lead < 0xE0) ? 2 : (lead < 0xF0) ? 3 : 4;
            if (index + width > length) {
                return false;
            }
            for (size_t continuation = 1; continuation < width; continuation++) {
                if ((data[index + continuation] & 0xC0) != 0x80) {
                    return false;
                }
            }
        }
        index += width;
        runes++;
    }
    *rune_length = runes;
    return true;
}
{{if .Paths}}

// Every message below is a fixed, process-lifetime C string literal: none is
// a Hexal String literal, so none goes through the module literal registry.
static const uint8_t hex_program_msg_cwd_bytes[] = "current directory unavailable";
static const hex_string hex_program_msg_cwd = {hex_program_msg_cwd_bytes, sizeof(hex_program_msg_cwd_bytes) - 1, sizeof(hex_program_msg_cwd_bytes) - 1, HEX_STRING_STATIC};
static const uint8_t hex_program_msg_home_bytes[] = "home directory unavailable";
static const hex_string hex_program_msg_home = {hex_program_msg_home_bytes, sizeof(hex_program_msg_home_bytes) - 1, sizeof(hex_program_msg_home_bytes) - 1, HEX_STRING_STATIC};
static const uint8_t hex_program_msg_tmp_bytes[] = "temporary directory unavailable";
static const hex_string hex_program_msg_tmp = {hex_program_msg_tmp_bytes, sizeof(hex_program_msg_tmp_bytes) - 1, sizeof(hex_program_msg_tmp_bytes) - 1, HEX_STRING_STATIC};
static const uint8_t hex_program_msg_exe_bytes[] = "executable path unavailable";
static const hex_string hex_program_msg_exe = {hex_program_msg_exe_bytes, sizeof(hex_program_msg_exe_bytes) - 1, sizeof(hex_program_msg_exe_bytes) - 1, HEX_STRING_STATIC};
static const uint8_t hex_program_msg_busy_bytes[] = "path changed during query";
static const hex_string hex_program_msg_busy = {hex_program_msg_busy_bytes, sizeof(hex_program_msg_busy_bytes) - 1, sizeof(hex_program_msg_busy_bytes) - 1, HEX_STRING_STATIC};
static const uint8_t hex_program_msg_utf8_bytes[] = "path is not valid UTF-8";
static const hex_string hex_program_msg_utf8 = {hex_program_msg_utf8_bytes, sizeof(hex_program_msg_utf8_bytes) - 1, sizeof(hex_program_msg_utf8_bytes) - 1, HEX_STRING_STATIC};

// hex_program_owned_string_or_null validates data as UTF-8 and, only if
// valid, copies it into one caller-Heap allocation. It never traps: an
// allocation or validation failure returns nullptr, and the caller maps that
// to the operation's own ErrorKind.
static const hex_string *hex_program_owned_string_or_null(const uint8_t *data, size_t length, bool *invalid) {
    size_t runes;
    if (!hex_program_validate_utf8(data, length, &runes)) {
        *invalid = true;
        return nullptr;
    }
    size_t total;
    if (ckd_add(&total, sizeof(hex_string_storage), length) || ckd_add(&total, total, 1)) {
        *invalid = false;
        return nullptr;
    }
    hex_string_storage *storage = hex_heap_allocate_or_null(total);
    if (storage == nullptr) {
        *invalid = false;
        return nullptr;
    }
    storage->header = (hex_string){.data = storage->bytes, .byte_length = length, .rune_length = runes, .storage_kind = HEX_STRING_OWNED};
    if (length != 0) {
        memcpy(storage->bytes, data, length);
    }
    storage->bytes[length] = 0;
    return &storage->header;
}

// hex_program_sized_query runs the documented UV_ENOBUFS sizing protocol
// shared by uv_cwd, uv_os_homedir, and uv_os_tmpdir: an initial guess, then
// at most two growth retries using the size libuv reports back. Every
// temporary buffer is plain process memory, released before this function
// returns; only the validated, copied result reaches the caller's Heap.
static hex_program_string_result hex_program_sized_query(int (*query)(char *, size_t *), const hex_string *unavailable) {
    size_t capacity = 4096;
    for (int attempt = 0; attempt < 3; attempt++) {
        char *buffer = malloc(capacity);
        if (buffer == nullptr) {
            return (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, .message = unavailable};
        }
        size_t size = capacity;
        int status = query(buffer, &size);
        if (status == 0) {
            if (size == 0) {
                free(buffer);
                return (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidPath}, .message = unavailable};
            }
            bool invalid = false;
            const hex_string *value = hex_program_owned_string_or_null((const uint8_t *)buffer, size, &invalid);
            free(buffer);
            if (value != nullptr) {
                return (hex_program_string_result){.ok = true, .value = value};
            }
            if (invalid) {
                return (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidPath}, .message = &hex_program_msg_utf8};
            }
            return (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, .message = unavailable};
        }
        free(buffer);
        if (status != UV_ENOBUFS) {
            hex_t_ErrorKind kind;
            if (!hex_handle_error_kind(status, &kind)) {
                kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Other};
            }
            return (hex_program_string_result){.ok = false, .kind = kind, .message = unavailable};
        }
        capacity = size;
    }
    return (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Busy}, .message = &hex_program_msg_busy};
}

hex_program_string_result hex_program_current_directory(hex_heap heap) {
    (void)heap;
    return hex_program_sized_query(uv_cwd, &hex_program_msg_cwd);
}

static uv_once_t hex_program_home_temp_once = UV_ONCE_INIT;
static uv_mutex_t hex_program_home_temp_mutex;

static void hex_program_home_temp_init(void) {
    uv_mutex_init(&hex_program_home_temp_mutex);
}

hex_program_string_result hex_program_home_directory(hex_heap heap) {
    (void)heap;
    uv_once(&hex_program_home_temp_once, hex_program_home_temp_init);
    uv_mutex_lock(&hex_program_home_temp_mutex);
    hex_program_string_result result = hex_program_sized_query(uv_os_homedir, &hex_program_msg_home);
    uv_mutex_unlock(&hex_program_home_temp_mutex);
    return result;
}

hex_program_string_result hex_program_temporary_directory(hex_heap heap) {
    (void)heap;
    uv_once(&hex_program_home_temp_once, hex_program_home_temp_init);
    uv_mutex_lock(&hex_program_home_temp_mutex);
    hex_program_string_result result = hex_program_sized_query(uv_os_tmpdir, &hex_program_msg_tmp);
    uv_mutex_unlock(&hex_program_home_temp_mutex);
    return result;
}

hex_program_string_result hex_program_executable_path(hex_heap heap) {
    (void)heap;
#if defined(_WIN32)
    size_t bound = 128u * 1024u;
#else
    size_t bound = 1024u * 1024u;
#endif
    size_t capacity = 4096;
    for (;;) {
        char *buffer = malloc(capacity);
        if (buffer == nullptr) {
            return (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, .message = &hex_program_msg_exe};
        }
        size_t size = capacity;
        int status = uv_exepath(buffer, &size);
        if (status != 0) {
            free(buffer);
            hex_t_ErrorKind kind;
            if (!hex_handle_error_kind(status, &kind)) {
                kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Other};
            }
            return (hex_program_string_result){.ok = false, .kind = kind, .message = &hex_program_msg_exe};
        }
        // Exact saturation (size == capacity - 1) is indistinguishable from
        // truncation on the pinned backends: grow and retry rather than trust
        // a possibly-truncated path.
        if (size < capacity - 1) {
            bool invalid = false;
            const hex_string *value = hex_program_owned_string_or_null((const uint8_t *)buffer, size, &invalid);
            free(buffer);
            if (value != nullptr) {
                return (hex_program_string_result){.ok = true, .value = value};
            }
            hex_t_ErrorKind kind = invalid ? (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidPath} : (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted};
            const hex_string *message = invalid ? &hex_program_msg_utf8 : &hex_program_msg_exe;
            return (hex_program_string_result){.ok = false, .kind = kind, .message = message};
        }
        free(buffer);
        if (capacity >= bound) {
            return (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, .message = &hex_program_msg_exe};
        }
        size_t grown;
        if (ckd_mul(&grown, capacity, (size_t)2)) {
            return (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, .message = &hex_program_msg_exe};
        }
        capacity = grown < bound ? grown : bound;
    }
}

size_t hex_program_available_parallelism(void) {
    return (size_t)uv_available_parallelism();
}
{{if .Event}}
#include "hexal/event.h"

// hex_program_query_job carries one path-query request across the Task
// park/resume boundary; it lives on the parked Task's own stack frame. One
// job shape serves every path query: each entry function differs only in
// which raw query it calls.
typedef struct hex_program_query_job {
    hex_heap heap;
    hex_program_string_result result;
} hex_program_query_job;

static void hex_program_query_failure(void *raw) {
    hex_program_query_job *job = (hex_program_query_job *)raw;
    job->result = (hex_program_string_result){.ok = false, .kind = (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_Other}, .message = &hex_program_msg_cwd};
}

static void hex_program_current_directory_entry(void *raw) {
    hex_program_query_job *job = (hex_program_query_job *)raw;
    job->result = hex_program_current_directory(job->heap);
}

hex_program_string_result hex_program_current_directory_task(hex_heap heap) {
    hex_program_query_job job = {.heap = heap};
    hex_event_work_call(hex_program_current_directory_entry, hex_program_query_failure, &job);
    return job.result;
}

static void hex_program_home_directory_entry(void *raw) {
    hex_program_query_job *job = (hex_program_query_job *)raw;
    job->result = hex_program_home_directory(job->heap);
}

hex_program_string_result hex_program_home_directory_task(hex_heap heap) {
    hex_program_query_job job = {.heap = heap};
    hex_event_work_call(hex_program_home_directory_entry, hex_program_query_failure, &job);
    return job.result;
}

static void hex_program_temporary_directory_entry(void *raw) {
    hex_program_query_job *job = (hex_program_query_job *)raw;
    job->result = hex_program_temporary_directory(job->heap);
}

hex_program_string_result hex_program_temporary_directory_task(hex_heap heap) {
    hex_program_query_job job = {.heap = heap};
    hex_event_work_call(hex_program_temporary_directory_entry, hex_program_query_failure, &job);
    return job.result;
}

static void hex_program_executable_path_entry(void *raw) {
    hex_program_query_job *job = (hex_program_query_job *)raw;
    job->result = hex_program_executable_path(job->heap);
}

hex_program_string_result hex_program_executable_path_task(hex_heap heap) {
    hex_program_query_job job = {.heap = heap};
    hex_event_work_call(hex_program_executable_path_entry, hex_program_query_failure, &job);
    return job.result;
}
{{end}}
{{end}}
{{if .Arguments}}

// The argument snapshot is process-lifetime private memory, published once
// before any module statement or Task can observe it, and never freed: an
// abandoned detached Task may still hold a view of it at process exit.
static bool hex_program_argv_ready = false;
static bool hex_program_argv_ok = false;
static const hex_string **hex_program_argv_items = nullptr;
static size_t hex_program_argv_count = 0;
static hex_t_ErrorKind hex_program_argv_kind;
static const hex_string *hex_program_argv_message = nullptr;

static void hex_program_release_partial_arguments(const hex_string **items, size_t count) {
    for (size_t index = 0; index < count; index++) {
        free((void *)items[index]);
    }
    free(items);
}

// Argument text is copied into process-lifetime storage because argv and the
// Windows command-line buffer are host-owned. The returned handle is marked
// non-owning so a user String.free cannot release this snapshot.
static const hex_string *hex_program_argument_string_or_null(const uint8_t *data, size_t length, bool *invalid) {
    size_t runes;
    if (!hex_program_validate_utf8(data, length, &runes)) {
        *invalid = true;
        return nullptr;
    }
    size_t total;
    if (ckd_add(&total, sizeof(hex_string_storage), length) || ckd_add(&total, total, 1)) {
        *invalid = false;
        return nullptr;
    }
    hex_string_storage *storage = malloc(total);
    if (storage == nullptr) {
        *invalid = false;
        return nullptr;
    }
    storage->header = (hex_string){.data = storage->bytes, .byte_length = length, .rune_length = runes, .storage_kind = HEX_STRING_NONOWNING};
    if (length != 0) {
        memcpy(storage->bytes, data, length);
    }
    storage->bytes[length] = 0;
    return &storage->header;
}

static void hex_program_argv_fail(hex_t_ErrorKind kind, const hex_string *message) {
    hex_program_argv_ok = false;
    hex_program_argv_kind = kind;
    hex_program_argv_message = message;
    hex_program_argv_ready = true;
}

#if defined(_WIN32)
static const uint8_t hex_program_msg_args_bytes[] = "program arguments unavailable";
static const hex_string hex_program_msg_args = {hex_program_msg_args_bytes, sizeof(hex_program_msg_args_bytes) - 1, sizeof(hex_program_msg_args_bytes) - 1, HEX_STRING_STATIC};
static const uint8_t hex_program_msg_args_unicode_bytes[] = "program argument is not valid Unicode";
static const hex_string hex_program_msg_args_unicode = {hex_program_msg_args_unicode_bytes, sizeof(hex_program_msg_args_unicode_bytes) - 1, sizeof(hex_program_msg_args_unicode_bytes) - 1, HEX_STRING_STATIC};

void hex_program_arguments_init(void) {
    int argc = 0;
    LPWSTR *wide = CommandLineToArgvW(GetCommandLineW(), &argc);
    if (wide == nullptr || argc < 0) {
        hex_program_argv_fail((hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, &hex_program_msg_args);
        return;
    }
    size_t count = (size_t)argc;
    size_t itemsBytes;
    if (ckd_mul(&itemsBytes, sizeof(const hex_string *), count)) {
        LocalFree(wide);
        hex_program_argv_fail((hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, &hex_program_msg_args);
        return;
    }
    const hex_string **items = count == 0 ? nullptr : malloc(itemsBytes);
    if (count != 0 && items == nullptr) {
        LocalFree(wide);
        hex_program_argv_fail((hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, &hex_program_msg_args);
        return;
    }
    for (size_t index = 0; index < count; index++) {
        int needed = WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wide[index], -1, nullptr, 0, nullptr, nullptr);
        if (needed <= 0) {
            hex_program_release_partial_arguments(items, index);
            LocalFree(wide);
            hex_t_ErrorKind kind = (GetLastError() == ERROR_NO_UNICODE_TRANSLATION) ? (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput} : (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted};
            const hex_string *message = (kind.tag == hex_tag_ErrorKind_InvalidInput) ? &hex_program_msg_args_unicode : &hex_program_msg_args;
            hex_program_argv_fail(kind, message);
            return;
        }
        // needed includes the trailing NUL WideCharToMultiByte would write;
        // the byte length Hexal records excludes it.
        size_t byteLength = (size_t)needed - 1;
        uint8_t *bytes = malloc((size_t)needed);
        if (bytes == nullptr) {
            hex_program_release_partial_arguments(items, index);
            LocalFree(wide);
            hex_program_argv_fail((hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, &hex_program_msg_args);
            return;
        }
        WideCharToMultiByte(CP_UTF8, WC_ERR_INVALID_CHARS, wide[index], -1, (char *)bytes, needed, nullptr, nullptr);
        bool invalid = false;
        items[index] = hex_program_argument_string_or_null(bytes, byteLength, &invalid);
        free(bytes);
        if (items[index] == nullptr) {
            hex_program_release_partial_arguments(items, index);
            LocalFree(wide);
            hex_t_ErrorKind kind = invalid ? (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput} : (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted};
            const hex_string *message = invalid ? &hex_program_msg_args_unicode : &hex_program_msg_args;
            hex_program_argv_fail(kind, message);
            return;
        }
    }
    LocalFree(wide);
    hex_program_argv_items = items;
    hex_program_argv_count = count;
    hex_program_argv_ok = true;
    hex_program_argv_ready = true;
}
#else
static const uint8_t hex_program_msg_args_bytes[] = "program arguments unavailable";
static const hex_string hex_program_msg_args = {hex_program_msg_args_bytes, sizeof(hex_program_msg_args_bytes) - 1, sizeof(hex_program_msg_args_bytes) - 1, HEX_STRING_STATIC};
static const uint8_t hex_program_msg_args_utf8_bytes[] = "program argument is not valid UTF-8";
static const hex_string hex_program_msg_args_utf8 = {hex_program_msg_args_utf8_bytes, sizeof(hex_program_msg_args_utf8_bytes) - 1, sizeof(hex_program_msg_args_utf8_bytes) - 1, HEX_STRING_STATIC};

void hex_program_arguments_init(int argc, char **argv) {
    if (argc < 0) {
        hex_program_argv_fail((hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, &hex_program_msg_args);
        return;
    }
    size_t count = (size_t)argc;
    size_t itemsBytes;
    if (ckd_mul(&itemsBytes, sizeof(const hex_string *), count)) {
        hex_program_argv_fail((hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, &hex_program_msg_args);
        return;
    }
    const hex_string **items = count == 0 ? nullptr : malloc(itemsBytes);
    if (count != 0 && items == nullptr) {
        hex_program_argv_fail((hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted}, &hex_program_msg_args);
        return;
    }
    for (size_t index = 0; index < count; index++) {
        size_t byteLength = strlen(argv[index]);
        size_t runes;
        if (!hex_program_validate_utf8((const uint8_t *)argv[index], byteLength, &runes)) {
            hex_program_release_partial_arguments(items, index);
            hex_program_argv_fail((hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput}, &hex_program_msg_args_utf8);
            return;
        }
        bool invalid = false;
        items[index] = hex_program_argument_string_or_null((const uint8_t *)argv[index], byteLength, &invalid);
        if (items[index] == nullptr) {
            hex_program_release_partial_arguments(items, index);
            hex_t_ErrorKind kind = invalid ? (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_InvalidInput} : (hex_t_ErrorKind){.tag = hex_tag_ErrorKind_ResourceExhausted};
            hex_program_argv_fail(kind, &hex_program_msg_args);
            return;
        }
    }
    hex_program_argv_items = items;
    hex_program_argv_count = count;
    hex_program_argv_ok = true;
    hex_program_argv_ready = true;
}
#endif

hex_program_arguments_result hex_program_arguments(void) {
    if (!hex_program_argv_ready) {
        hex_runtime_trap("[Runtime Error] program arguments were never initialized\n");
    }
    if (!hex_program_argv_ok) {
        return (hex_program_arguments_result){.ok = false, .kind = hex_program_argv_kind, .message = hex_program_argv_message};
    }
    return (hex_program_arguments_result){.ok = true, .items = (const hex_string *const *)hex_program_argv_items, .count = hex_program_argv_count};
}
{{end}}
