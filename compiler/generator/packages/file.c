/* File runtime: regular files over the libuv filesystem request family. A
   call outside a Task uses libuv's synchronous callback-null form{{if .Event}};
   a call inside a Task submits the same request from the loop thread and
   parks only that Task{{end}}. Seek is the one direct native operation: libuv
   has no seek request. */
#include "hexal/file.h"
#include "hexal/heap.h"
{{- if .Event}}
#include "hexal/event.h"
{{- end}}
#include <stdckdint.h>
#include <string.h>
#include <uv.h>
#ifdef _WIN32
#include <windows.h>
#else
#include <errno.h>
#include <unistd.h>
#endif

enum {
    HEX_FILE_ACCESS_READ = 1,
    HEX_FILE_ACCESS_WRITE = 2,
};

// uv_buf_init counts an unsigned int, so one call transfers at most
// UINT32_MAX bytes, matching the Windows IO clamp.
constexpr size_t HEX_FILE_MAX_REQUEST = UINT32_MAX;

static const int hex_file_flags[5] = {
    UV_FS_O_RDONLY,
    UV_FS_O_WRONLY | UV_FS_O_CREAT | UV_FS_O_TRUNC,
    UV_FS_O_WRONLY | UV_FS_O_CREAT | UV_FS_O_APPEND,
    UV_FS_O_RDWR,
    UV_FS_O_WRONLY | UV_FS_O_CREAT | UV_FS_O_EXCL,
};

static const uint8_t hex_file_access[5] = {
    HEX_FILE_ACCESS_READ,
    HEX_FILE_ACCESS_WRITE,
    HEX_FILE_ACCESS_WRITE,
    HEX_FILE_ACCESS_READ | HEX_FILE_ACCESS_WRITE,
    HEX_FILE_ACCESS_WRITE,
};

typedef enum hex_file_operation : uint8_t {
    HEX_FILE_OPEN,
    HEX_FILE_READ,
    HEX_FILE_WRITE,
    HEX_FILE_FSYNC,
    HEX_FILE_CLOSE,
} hex_file_operation;

// One request record lives on the calling fiber stack. Its path copy and
// buffer stay live until the request's result has been consumed and cleaned.
typedef struct hex_file_request {
{{- if .Event}}
    hex_event_command command;
{{- end}}
    uv_fs_t fs;
    hex_file_operation operation;
    const char *path;
    int flags;
    uv_file desc;
    uv_buf_t buffer;
} hex_file_request;

static int hex_file_submit(hex_file_request *request, uv_loop_t *loop, uv_fs_cb done) {
    switch (request->operation) {
    case HEX_FILE_OPEN:
        return uv_fs_open(loop, &request->fs, request->path, request->flags, 0666, done);
    case HEX_FILE_READ:
        return uv_fs_read(loop, &request->fs, request->desc, &request->buffer, 1, -1, done);
    case HEX_FILE_WRITE:
        return uv_fs_write(loop, &request->fs, request->desc, &request->buffer, 1, -1, done);
    case HEX_FILE_FSYNC:
        return uv_fs_fsync(loop, &request->fs, request->desc, done);
    default:
        return uv_fs_close(loop, &request->fs, request->desc, done);
    }
}
{{- if .Event}}

// The callback runs no user code: the result already sits in fs.result, so
// it only wakes the Task, which consumes and cleans the request itself.
static void hex_file_done(uv_fs_t *fs) {
    hex_file_request *request = (hex_file_request *)fs->data;
    hex_task_event_wake(request->command.task);
}

static void hex_file_start(hex_event_command *command) {
    hex_file_request *request = (hex_file_request *)command;
    request->fs.data = request;
    int status = hex_file_submit(request, (uv_loop_t *)hex_event_loop_handle(), hex_file_done);
    if (status < 0) {
        // A rejected submission queues no callback; publish its status as
        // the result before the one wake.
        request->fs.result = status;
        hex_task_event_wake(request->command.task);
    }
}
{{- end}}

// hex_file_run executes one request and returns its libuv result. uv_fs_req_cleanup
// runs exactly once, after the result is read.
static ssize_t hex_file_run(hex_file_request *request) {
{{- if .Event}}
    hex_task *task = hex_task_current();
    if (task != nullptr) {
        request->command.task = task;
        request->command.start = hex_file_start;
        hex_event_submit(task, &request->command);
    } else {
        // CARE: the pinned libuv never dereferences the loop of a synchronous
        // filesystem request on either platform, so no loop starts here.
        (void)hex_file_submit(request, nullptr, nullptr);
    }
{{- else}}
    // CARE: the pinned libuv never dereferences the loop of a synchronous
    // filesystem request on either platform, so no loop starts here.
    (void)hex_file_submit(request, nullptr, nullptr);
{{- end}}
    ssize_t result = request->fs.result;
    uv_fs_req_cleanup(&request->fs);
    return result;
}

hex_file_opened hex_file_open(const hex_string *path, uint8_t variant) {
    if (memchr(path->data, 0, path->byte_length) != nullptr) {
        return (hex_file_opened){.status = HEX_FILE_INVALID_PATH};
    }
    size_t size;
    if (ckd_add(&size, path->byte_length, 1)) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    char *copy = (char *)hex_heap_allocate(size);
    memcpy(copy, path->data, path->byte_length);
    copy[path->byte_length] = '\0';
    hex_file_request request = {.operation = HEX_FILE_OPEN, .path = copy, .flags = hex_file_flags[variant]};
    ssize_t result = hex_file_run(&request);
    hex_heap_free(copy);
    if (result < 0) {
        return (hex_file_opened){.status = (int)result};
    }
    return (hex_file_opened){.status = 0, .file = {.desc = (intptr_t)result, .access = hex_file_access[variant]}};
}

hex_file_transfer hex_file_read(hex_file file, hex_list_UInt8 *into, size_t max) {
    if ((file.access & HEX_FILE_ACCESS_READ) == 0) {
        return (hex_file_transfer){.status = HEX_FILE_NOT_READABLE};
    }
    if (max == 0) {
        return (hex_file_transfer){.status = 0, .count = 0};
    }
    size_t count = max > HEX_FILE_MAX_REQUEST ? HEX_FILE_MAX_REQUEST : max;
    size_t needed = 0;
    if (ckd_add(&needed, into->length, count)) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    hex_list_reserve_at_least_UInt8(into, needed);
    hex_file_request request = {
        .operation = HEX_FILE_READ,
        .desc = (uv_file)file.desc,
        .buffer = uv_buf_init((char *)(into->data + into->length), (unsigned int)count),
    };
    ssize_t result = hex_file_run(&request);
    if (result < 0) {
        return (hex_file_transfer){.status = (int)result};
    }
    if (result == 0) {
        return (hex_file_transfer){.status = HEX_FILE_EOS};
    }
    into->length += (size_t)result;
    return (hex_file_transfer){.status = 0, .count = (size_t)result};
}

hex_file_transfer hex_file_write(hex_file file, hex_slice_UInt8 from) {
    if ((file.access & HEX_FILE_ACCESS_WRITE) == 0) {
        return (hex_file_transfer){.status = HEX_FILE_NOT_WRITABLE};
    }
    if (from.length == 0) {
        return (hex_file_transfer){.status = 0, .count = 0};
    }
    size_t count = from.length > HEX_FILE_MAX_REQUEST ? HEX_FILE_MAX_REQUEST : from.length;
    hex_file_request request = {
        .operation = HEX_FILE_WRITE,
        .desc = (uv_file)file.desc,
        .buffer = uv_buf_init((char *)from.data, (unsigned int)count),
    };
    ssize_t result = hex_file_run(&request);
    if (result < 0) {
        return (hex_file_transfer){.status = (int)result};
    }
    return (hex_file_transfer){.status = 0, .count = (size_t)result};
}

// Repositioning a descriptor never waits on device IO, so seek runs directly
// even inside a Task. The native error passes through uv_translate_sys_error so
// the portable header table applies; on Windows that needs the OS error of
// the descriptor's handle rather than a CRT errno.
hex_file_transfer hex_file_seek(hex_file file, uint8_t whence, int64_t offset) {
#ifdef _WIN32
    DWORD method = whence == 0 ? FILE_BEGIN : whence == 1 ? FILE_CURRENT : FILE_END;
    LARGE_INTEGER target = {.QuadPart = offset};
    LARGE_INTEGER moved;
    if (!SetFilePointerEx((HANDLE)uv_get_osfhandle((uv_file)file.desc), target, &moved, method)) {
        return (hex_file_transfer){.status = uv_translate_sys_error((int)GetLastError())};
    }
    return (hex_file_transfer){.status = 0, .count = (size_t)moved.QuadPart};
#else
    off_t moved = lseek((int)file.desc, (off_t)offset, whence == 0 ? SEEK_SET : whence == 1 ? SEEK_CUR : SEEK_END);
    if (moved < 0) {
        return (hex_file_transfer){.status = uv_translate_sys_error(errno)};
    }
    return (hex_file_transfer){.status = 0, .count = (size_t)moved};
#endif
}

int hex_file_flush(hex_file file) {
    if ((file.access & HEX_FILE_ACCESS_WRITE) == 0) {
        return HEX_FILE_NOT_WRITABLE;
    }
    hex_file_request request = {.operation = HEX_FILE_FSYNC, .desc = (uv_file)file.desc};
    ssize_t result = hex_file_run(&request);
    return result < 0 ? (int)result : 0;
}

// Close is never retried: every copy is invalid afterwards even on failure.
int hex_file_close(hex_file file) {
    hex_file_request request = {.operation = HEX_FILE_CLOSE, .desc = (uv_file)file.desc};
    ssize_t result = hex_file_run(&request);
    return result < 0 ? (int)result : 0;
}

// The portable File header table. EINVAL means an invalid path only for
// open; every other operation reports it as a generic filesystem error.
hex_t_Error hex_file_error(size_t line, size_t column, const hex_string *file, int status, bool opening, const hex_string *message) {
    const char *text;
    switch (status) {
    case HEX_FILE_INVALID_PATH:
    case UV_ENAMETOOLONG:
    case UV_ELOOP:
        text = "invalid path";
        break;
    case UV_EINVAL:
        text = opening ? "invalid path" : "filesystem error";
        break;
    case UV_ENOENT:
        text = "not found";
        break;
    case UV_EACCES:
    case UV_EPERM:
        text = "permission denied";
        break;
    case UV_EEXIST:
        text = "already exists";
        break;
    case UV_ENOTDIR:
        text = "not a directory";
        break;
    case UV_EISDIR:
        text = "is a directory";
        break;
    case UV_ENOTEMPTY:
        text = "directory not empty";
        break;
    case UV_EROFS:
        text = "read only";
        break;
    case UV_EBUSY:
        text = "busy";
        break;
    case UV_EINTR:
        text = "interrupted";
        break;
    case UV_ECANCELED:
        text = "cancelled";
        break;
    case UV_ENOSYS:
    case UV_ENOTSUP:
        text = "unsupported";
        break;
    default:
        text = "filesystem error";
        break;
    }
    hex_strand header = {0};
    memcpy(header.data, text, strlen(text));
    return (hex_t_Error){
        .hex_m_file = file,
        .hex_m_line = line,
        .hex_m_column = column,
        .hex_m_header = header,
        .hex_m_message = message,
    };
}
