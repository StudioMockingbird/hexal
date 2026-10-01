#include "fileserver.h"
#include "hexal/event.h"
#include <limits.h>
#include <stdatomic.h>
#include <stdckdint.h>
#include <string.h>
#if defined(_WIN32)
#include <windows.h>
#include <winternl.h>
#else
#include <errno.h>
#include <fcntl.h>
#include <sys/stat.h>
#include <unistd.h>
#endif

// The static file server. The one containment rule is that a file is reached
// only by opening it beneath the retained root handle, one path component at a
// time, with every component opened without following a symbolic link or
// reparse point. A pathname is never canonicalized and then opened, and no
// string-prefix comparison stands in for containment, so a path that changes
// between lookup and open cannot lead outside the root. All file system calls
// run on the event bridge's worker pool, off the scheduler worker.

#define HEX_FILES_MESSAGE(identifier, text) \
    static const hex_string identifier = { .data = (const uint8_t *)(text), .byte_length = sizeof(text) - 1 }

HEX_FILES_MESSAGE(hex_files_message_bad_root, "file server root must name an existing directory");
HEX_FILES_MESSAGE(hex_files_message_link_root, "file server root must not be a symbolic link");
HEX_FILES_MESSAGE(hex_files_message_missing_root, "file server root was not found");
HEX_FILES_MESSAGE(hex_files_message_denied_root, "file server root is not accessible");
HEX_FILES_MESSAGE(hex_files_message_bad_index, "index must be a file name without separators");
HEX_FILES_MESSAGE(hex_files_message_bad_cache, "cache control must not contain CR, LF, or NUL");
HEX_FILES_MESSAGE(hex_files_message_memory, "file server ran out of memory");
HEX_FILES_MESSAGE(hex_files_message_changed, "file changed while it was being served");
HEX_FILES_MESSAGE(hex_files_message_failed, "file could not be read");

static inline hex_t_ErrorKind hex_files_kind(hex_tag tag) {
    return (hex_t_ErrorKind){ .tag = tag };
}

static inline hex_t_ErrorKind hex_files_kind_other(void) {
    static const char text[] = "file error";
    hex_string_128 header = { .byte_length = sizeof(text) - 1 };
    memcpy(header.data, text, sizeof(text) - 1);
    return (hex_t_ErrorKind){ .tag = hex_tag_ErrorKind_Other, .other_header = header };
}

static inline hex_http_status_result hex_files_failure(hex_t_ErrorKind kind, const hex_string *message) {
    return (hex_http_status_result){ .ok = false, .kind = kind, .message = message };
}

#if defined(_WIN32)
typedef HANDLE hex_files_handle;
#define HEX_FILES_NO_HANDLE INVALID_HANDLE_VALUE
#else
typedef int hex_files_handle;
#define HEX_FILES_NO_HANDLE (-1)
#endif

struct hex_http_files_state {
    hex_files_handle root;
    uint8_t *index;
    size_t index_length;
    uint8_t *cache_control;
    size_t cache_length;
    bool dotfiles;
    atomic_size_t mounted;
};

// --- Opening beneath the root ---

enum {
    HEX_FILES_OK,
    HEX_FILES_NOT_FOUND,
    HEX_FILES_FORBIDDEN,
    HEX_FILES_FAILED,
};

// hex_files_found is one opened regular file and the metadata read from that
// same handle, so the validators and the transferred bytes describe one file.
typedef struct hex_files_found {
    hex_files_handle handle;
    uint64_t size;
    int64_t modified;
} hex_files_found;

// hex_files_walk is one open request: the decoded path (components separated
// by single slashes, none empty) and, when a directory results, whether to
// open its index file. The outcome and the found file come back through it;
// indexed says the found file is the directory's index, not the named path.
typedef struct hex_files_walk {
    const struct hex_http_files_state *files;
    const uint8_t *path;
    size_t length;
    bool directory_ok;
    bool indexed;
    int outcome;
    hex_files_found found;
} hex_files_walk;

// hex_files_next_component yields the next slash-separated component.
static bool hex_files_next_component(const uint8_t *path, size_t length, size_t *cursor, const uint8_t **name, size_t *size) {
    if (*cursor >= length) {
        return false;
    }
    size_t start = *cursor;
    size_t end = start;
    while (end < length && path[end] != '/') {
        end++;
    }
    *name = path + start;
    *size = end - start;
    *cursor = end < length ? end + 1 : end;
    return true;
}

#if defined(_WIN32)

#ifndef FILE_DIRECTORY_FILE
#define FILE_DIRECTORY_FILE 0x00000001
#endif
#ifndef FILE_NON_DIRECTORY_FILE
#define FILE_NON_DIRECTORY_FILE 0x00000040
#endif
#ifndef FILE_SYNCHRONOUS_IO_NONALERT
#define FILE_SYNCHRONOUS_IO_NONALERT 0x00000020
#endif
#ifndef FILE_OPEN_REPARSE_POINT
#define FILE_OPEN_REPARSE_POINT 0x00200000
#endif
#ifndef FILE_OPEN
#define FILE_OPEN 0x00000001
#endif

typedef NTSTATUS (NTAPI *hex_nt_create_file)(PHANDLE, ACCESS_MASK, POBJECT_ATTRIBUTES, PIO_STATUS_BLOCK, PLARGE_INTEGER, ULONG, ULONG, ULONG, ULONG, PVOID, ULONG);

static hex_nt_create_file hex_files_nt_create(void) {
    static hex_nt_create_file function;
    if (function == nullptr) {
        HMODULE ntdll = GetModuleHandleW(L"ntdll.dll");
        function = ntdll == nullptr ? nullptr : (hex_nt_create_file)(void *)GetProcAddress(ntdll, "NtCreateFile");
    }
    return function;
}

// hex_files_wide converts UTF-8 to a NUL-terminated UTF-16 string, rejecting
// invalid UTF-8. The caller frees the result.
static wchar_t *hex_files_wide(const uint8_t *text, size_t length, size_t *wide_length) {
    if (length > (size_t)INT_MAX) {
        return nullptr;
    }
    int needed = length == 0 ? 0 : MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, (const char *)text, (int)length, nullptr, 0);
    if (length != 0 && needed <= 0) {
        return nullptr;
    }
    wchar_t *wide = hex_heap_allocate_or_null(((size_t)needed + 1) * sizeof(wchar_t));
    if (wide == nullptr) {
        return nullptr;
    }
    if (needed != 0) {
        MultiByteToWideChar(CP_UTF8, MB_ERR_INVALID_CHARS, (const char *)text, (int)length, wide, needed);
    }
    wide[needed] = L'\0';
    if (wide_length != nullptr) {
        *wide_length = (size_t)needed;
    }
    return wide;
}

// hex_files_open_relative opens one component relative to parent without
// following a reparse point: the reparse point itself is opened, and its
// attribute rejects it afterwards.
static int hex_files_open_relative(HANDLE parent, const uint8_t *name, size_t size, bool directory, HANDLE *out) {
    hex_nt_create_file create = hex_files_nt_create();
    if (create == nullptr) {
        return HEX_FILES_FAILED;
    }
    size_t wide_length = 0;
    wchar_t *wide = hex_files_wide(name, size, &wide_length);
    if (wide == nullptr) {
        return HEX_FILES_NOT_FOUND;
    }
    UNICODE_STRING unicode = { .Length = (USHORT)(wide_length * sizeof(wchar_t)), .MaximumLength = (USHORT)((wide_length + 1) * sizeof(wchar_t)), .Buffer = wide };
    OBJECT_ATTRIBUTES attributes = { .Length = sizeof(attributes), .RootDirectory = parent, .ObjectName = &unicode, .Attributes = 0x40 };
    IO_STATUS_BLOCK block = { 0 };
    HANDLE handle = INVALID_HANDLE_VALUE;
    NTSTATUS status = create(&handle, FILE_GENERIC_READ, &attributes, &block, nullptr, FILE_ATTRIBUTE_NORMAL,
        FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE, FILE_OPEN,
        FILE_SYNCHRONOUS_IO_NONALERT | FILE_OPEN_REPARSE_POINT | (directory ? FILE_DIRECTORY_FILE : 0), nullptr, 0);
    hex_heap_free(wide);
    if (status < 0) {
        switch ((ULONG)status) {
        case 0xC0000034: // object name not found
        case 0xC000003A: // object path not found
        case 0xC0000033: // object name invalid
        case 0xC0000103: // not a directory
        case 0xC00000BA: // is a directory
            return HEX_FILES_NOT_FOUND;
        case 0xC0000022: // access denied
        case 0xC0000043: // sharing violation
            return HEX_FILES_FORBIDDEN;
        default:
            return HEX_FILES_FAILED;
        }
    }
    *out = handle;
    return HEX_FILES_OK;
}

static int hex_files_inspect(HANDLE handle, bool *directory, hex_files_found *found) {
    BY_HANDLE_FILE_INFORMATION information;
    if (!GetFileInformationByHandle(handle, &information)) {
        return HEX_FILES_FAILED;
    }
    if (information.dwFileAttributes & FILE_ATTRIBUTE_REPARSE_POINT) {
        return HEX_FILES_FORBIDDEN;
    }
    *directory = (information.dwFileAttributes & FILE_ATTRIBUTE_DIRECTORY) != 0;
    if (found != nullptr) {
        found->size = ((uint64_t)information.nFileSizeHigh << 32) | information.nFileSizeLow;
        uint64_t ticks = ((uint64_t)information.ftLastWriteTime.dwHighDateTime << 32) | information.ftLastWriteTime.dwLowDateTime;
        // FILETIME counts 100 ns from 1601; the Unix epoch is 11644473600 s later.
        found->modified = (int64_t)(ticks / 10000000ULL) - 11644473600LL;
    }
    return HEX_FILES_OK;
}

static void hex_files_close_handle(HANDLE handle) {
    if (handle != INVALID_HANDLE_VALUE) {
        CloseHandle(handle);
    }
}

// An alternate data stream is named with a colon, which no component may carry.
static bool hex_files_component_valid(const uint8_t *name, size_t size) {
    return memchr(name, ':', size) == nullptr;
}

#else

constexpr size_t HEX_FILES_COMPONENT_MAX = 255;

// hex_files_open_relative opens one component beneath the directory descriptor
// parent without following a symbolic link. O_NONBLOCK keeps a named pipe from
// blocking the open, and only regular files and directories are accepted
// afterwards.
static int hex_files_open_relative(int parent, const uint8_t *name, size_t size, int *out) {
    char text[HEX_FILES_COMPONENT_MAX + 1];
    if (size > HEX_FILES_COMPONENT_MAX) {
        return HEX_FILES_NOT_FOUND;
    }
    memcpy(text, name, size);
    text[size] = '\0';
    int descriptor = openat(parent, text, O_RDONLY | O_NOFOLLOW | O_CLOEXEC | O_NONBLOCK);
    if (descriptor < 0) {
        switch (errno) {
        case ENOENT:
        case ENOTDIR:
        case ENAMETOOLONG:
            return HEX_FILES_NOT_FOUND;
        case ELOOP:
#ifdef EMLINK
        case EMLINK:
#endif
        case EACCES:
        case EPERM:
            return HEX_FILES_FORBIDDEN;
        default:
            return HEX_FILES_FAILED;
        }
    }
    *out = descriptor;
    return HEX_FILES_OK;
}

static int hex_files_inspect(int descriptor, bool *directory, hex_files_found *found) {
    struct stat information;
    if (fstat(descriptor, &information) != 0) {
        return HEX_FILES_FAILED;
    }
    if (S_ISDIR(information.st_mode)) {
        *directory = true;
        return HEX_FILES_OK;
    }
    if (!S_ISREG(information.st_mode)) {
        return HEX_FILES_NOT_FOUND;
    }
    *directory = false;
    if (found != nullptr) {
        found->size = (uint64_t)information.st_size;
        found->modified = (int64_t)information.st_mtime;
    }
    return HEX_FILES_OK;
}

static void hex_files_close_handle(int descriptor) {
    if (descriptor >= 0) {
        close(descriptor);
    }
}

static bool hex_files_component_valid(const uint8_t *name, size_t size) {
    (void)name;
    (void)size;
    return true;
}

#endif

// hex_files_walk_entry resolves one decoded path beneath the root on a worker
// thread: each component but the last must be a directory; a directory result
// is replaced by its index file when the request allows one.
static void hex_files_walk_entry(void *context) {
    hex_files_walk *walk = (hex_files_walk *)context;
    const struct hex_http_files_state *files = walk->files;
    hex_files_handle directory = files->root;
    bool owned = false;
    const uint8_t *name;
    size_t size;
    size_t cursor = 0;
    bool have_file = false;
    hex_files_handle next = HEX_FILES_NO_HANDLE;
    bool is_directory = false;
    hex_files_found metadata = { 0 };
    int outcome = HEX_FILES_OK;
    walk->outcome = HEX_FILES_OK;
    walk->found.handle = HEX_FILES_NO_HANDLE;
    while (hex_files_next_component(walk->path, walk->length, &cursor, &name, &size)) {
        bool last = cursor >= walk->length;
        if (!hex_files_component_valid(name, size)) {
            walk->outcome = HEX_FILES_FORBIDDEN;
            goto done;
        }
        next = HEX_FILES_NO_HANDLE;
#if defined(_WIN32)
        outcome = hex_files_open_relative(directory, name, size, false, &next);
#else
        outcome = hex_files_open_relative(directory, name, size, &next);
#endif
        if (outcome != HEX_FILES_OK) {
            walk->outcome = outcome;
            goto done;
        }
        is_directory = false;
        metadata = (hex_files_found){ 0 };
        outcome = hex_files_inspect(next, &is_directory, &metadata);
        if (outcome != HEX_FILES_OK) {
            hex_files_close_handle(next);
            walk->outcome = outcome;
            goto done;
        }
        if (owned) {
            hex_files_close_handle(directory);
        }
        if (is_directory) {
            directory = next;
            owned = true;
            continue;
        }
        // A regular file ends the walk; one before the last component would
        // mean the path names something beneath a file.
        directory = HEX_FILES_NO_HANDLE;
        owned = false;
        if (!last) {
            hex_files_close_handle(next);
            walk->outcome = HEX_FILES_NOT_FOUND;
            goto done;
        }
        walk->found = metadata;
        walk->found.handle = next;
        have_file = true;
        break;
    }
    if (!have_file) {
        // The path names a directory, or the root itself.
        if (!walk->directory_ok || files->index_length == 0) {
            walk->outcome = HEX_FILES_NOT_FOUND;
            goto done;
        }
        next = HEX_FILES_NO_HANDLE;
#if defined(_WIN32)
        outcome = hex_files_open_relative(directory, files->index, files->index_length, false, &next);
#else
        outcome = hex_files_open_relative(directory, files->index, files->index_length, &next);
#endif
        if (outcome != HEX_FILES_OK) {
            walk->outcome = outcome == HEX_FILES_FORBIDDEN ? HEX_FILES_NOT_FOUND : outcome;
            goto done;
        }
        is_directory = false;
        metadata = (hex_files_found){ 0 };
        outcome = hex_files_inspect(next, &is_directory, &metadata);
        if (outcome != HEX_FILES_OK || is_directory) {
            hex_files_close_handle(next);
            walk->outcome = outcome == HEX_FILES_OK ? HEX_FILES_NOT_FOUND : outcome;
            goto done;
        }
        walk->found = metadata;
        walk->found.handle = next;
        walk->indexed = true;
    }
done:
    if (owned) {
        hex_files_close_handle(directory);
    }
}

static void hex_files_job_failed(void *context) {
    hex_files_walk *walk = (hex_files_walk *)context;
    walk->outcome = HEX_FILES_FAILED;
    walk->found.handle = HEX_FILES_NO_HANDLE;
}

// --- Reading ---

typedef struct hex_files_read {
    hex_files_handle handle;
    uint64_t offset;
    uint8_t *buffer;
    size_t length;
    size_t count;
    bool failed;
} hex_files_read;

static void hex_files_read_entry(void *context) {
    hex_files_read *read_job = (hex_files_read *)context;
    read_job->count = 0;
    read_job->failed = false;
    while (read_job->count < read_job->length) {
        size_t want = read_job->length - read_job->count;
#if defined(_WIN32)
        if (want > (size_t)0x40000000) {
            want = 0x40000000;
        }
        OVERLAPPED position = { 0 };
        uint64_t at = read_job->offset + read_job->count;
        position.Offset = (DWORD)(at & 0xFFFFFFFFu);
        position.OffsetHigh = (DWORD)(at >> 32);
        DWORD got = 0;
        if (!ReadFile(read_job->handle, read_job->buffer + read_job->count, (DWORD)want, &got, &position)) {
            if (GetLastError() != ERROR_HANDLE_EOF) {
                read_job->failed = true;
            }
            return;
        }
        if (got == 0) {
            return;
        }
        read_job->count += got;
#else
        ssize_t got = pread(read_job->handle, read_job->buffer + read_job->count, want, (off_t)(read_job->offset + read_job->count));
        if (got < 0) {
            if (errno == EINTR) {
                continue;
            }
            read_job->failed = true;
            return;
        }
        if (got == 0) {
            return;
        }
        read_job->count += (size_t)got;
#endif
    }
}

static void hex_files_read_failed(void *context) {
    hex_files_read *read_job = (hex_files_read *)context;
    read_job->count = 0;
    read_job->failed = true;
}

// --- Construction ---

static bool hex_files_text_has(const hex_string *text, const char *forbidden) {
    for (size_t index = 0; index < text->byte_length; index++) {
        uint8_t byte = text->data[index];
        if (byte == 0 || strchr(forbidden, (char)byte) != nullptr) {
            return true;
        }
    }
    return false;
}

static uint8_t *hex_files_copy(const hex_string *text) {
    uint8_t *copy = hex_heap_allocate_or_null(text->byte_length == 0 ? 1 : text->byte_length);
    if (copy != nullptr && text->byte_length != 0) {
        memcpy(copy, text->data, text->byte_length);
    }
    return copy;
}

// hex_files_open_root opens the root directory itself. A root that is a symbolic
// link or reparse point is rejected, so containment never begins outside the
// directory the program named.
static hex_http_files_result hex_files_open_root(const hex_string *root, hex_files_handle *out) {
    hex_http_files_result failure = { .ok = false };
#if defined(_WIN32)
    wchar_t *wide = hex_files_wide(root->data, root->byte_length, nullptr);
    if (wide == nullptr) {
        failure.kind = hex_files_kind(hex_tag_ErrorKind_InvalidInput);
        failure.message = &hex_files_message_bad_root;
        return failure;
    }
    HANDLE handle = CreateFileW(wide, GENERIC_READ, FILE_SHARE_READ | FILE_SHARE_WRITE | FILE_SHARE_DELETE, nullptr, OPEN_EXISTING,
        FILE_FLAG_BACKUP_SEMANTICS | FILE_FLAG_OPEN_REPARSE_POINT, nullptr);
    hex_heap_free(wide);
    if (handle == INVALID_HANDLE_VALUE) {
        DWORD code = GetLastError();
        failure.kind = hex_files_kind(code == ERROR_ACCESS_DENIED ? hex_tag_ErrorKind_PermissionDenied : hex_tag_ErrorKind_NotFound);
        failure.message = code == ERROR_ACCESS_DENIED ? &hex_files_message_denied_root : &hex_files_message_missing_root;
        return failure;
    }
    bool directory = false;
    int outcome = hex_files_inspect(handle, &directory, nullptr);
    if (outcome != HEX_FILES_OK || !directory) {
        CloseHandle(handle);
        failure.kind = hex_files_kind(hex_tag_ErrorKind_InvalidInput);
        failure.message = outcome == HEX_FILES_FORBIDDEN ? &hex_files_message_link_root : &hex_files_message_bad_root;
        return failure;
    }
    *out = handle;
#else
    char *path = hex_heap_allocate_or_null(root->byte_length + 1);
    if (path == nullptr) {
        failure.kind = hex_files_kind(hex_tag_ErrorKind_ResourceExhausted);
        failure.message = &hex_files_message_memory;
        return failure;
    }
    memcpy(path, root->data, root->byte_length);
    path[root->byte_length] = '\0';
    struct stat information;
    if (lstat(path, &information) != 0) {
        hex_heap_free(path);
        failure.kind = hex_files_kind(errno == EACCES ? hex_tag_ErrorKind_PermissionDenied : hex_tag_ErrorKind_NotFound);
        failure.message = errno == EACCES ? &hex_files_message_denied_root : &hex_files_message_missing_root;
        return failure;
    }
    if (S_ISLNK(information.st_mode)) {
        hex_heap_free(path);
        failure.kind = hex_files_kind(hex_tag_ErrorKind_InvalidInput);
        failure.message = &hex_files_message_link_root;
        return failure;
    }
    // O_NOFOLLOW closes the gap between the lstat above and this open: a link
    // swapped in between fails the open instead of being entered.
    int descriptor = open(path, O_RDONLY | O_DIRECTORY | O_NOFOLLOW | O_CLOEXEC);
    int saved = errno;
    hex_heap_free(path);
    if (descriptor < 0) {
        failure.kind = hex_files_kind(saved == EACCES ? hex_tag_ErrorKind_PermissionDenied : hex_tag_ErrorKind_InvalidInput);
        failure.message = saved == EACCES ? &hex_files_message_denied_root : &hex_files_message_bad_root;
        return failure;
    }
    *out = descriptor;
#endif
    return (hex_http_files_result){ .ok = true };
}

hex_http_files_result hex_http_files_open_raw(hex_heap h, const hex_string *root, const hex_string *index, bool enabled, const hex_string *cache_control) {
    (void)h;
    hex_http_files_result failure = { .ok = false };
    if (root->byte_length == 0 || memchr(root->data, 0, root->byte_length) != nullptr) {
        failure.kind = hex_files_kind(hex_tag_ErrorKind_InvalidInput);
        failure.message = &hex_files_message_bad_root;
        return failure;
    }
    if (hex_files_text_has(index, "/\\:")) {
        failure.kind = hex_files_kind(hex_tag_ErrorKind_InvalidInput);
        failure.message = &hex_files_message_bad_index;
        return failure;
    }
    for (size_t position = 0; position < cache_control->byte_length; position++) {
        uint8_t byte = cache_control->data[position];
        if (byte == '\r' || byte == '\n' || byte == 0) {
            failure.kind = hex_files_kind(hex_tag_ErrorKind_InvalidInput);
            failure.message = &hex_files_message_bad_cache;
            return failure;
        }
    }
    hex_files_handle handle = HEX_FILES_NO_HANDLE;
    hex_http_files_result opened = hex_files_open_root(root, &handle);
    if (!opened.ok) {
        return opened;
    }
    struct hex_http_files_state *files = hex_heap_allocate_zeroed_or_null(sizeof(struct hex_http_files_state));
    if (files == nullptr) {
        hex_files_close_handle(handle);
        failure.kind = hex_files_kind(hex_tag_ErrorKind_ResourceExhausted);
        failure.message = &hex_files_message_memory;
        return failure;
    }
    files->root = handle;
    files->index = hex_files_copy(index);
    files->cache_control = hex_files_copy(cache_control);
    files->index_length = index->byte_length;
    files->cache_length = cache_control->byte_length;
    files->dotfiles = enabled;
    if (files->index == nullptr || files->cache_control == nullptr) {
        hex_files_close_handle(handle);
        if (files->index != nullptr) {
            hex_heap_free(files->index);
        }
        if (files->cache_control != nullptr) {
            hex_heap_free(files->cache_control);
        }
        hex_heap_free(files);
        failure.kind = hex_files_kind(hex_tag_ErrorKind_ResourceExhausted);
        failure.message = &hex_files_message_memory;
        return failure;
    }
    return (hex_http_files_result){ .ok = true, .files = files };
}

void hex_http_files_free_raw(hex_http_files files, hex_heap h) {
    (void)h;
    if (atomic_load(&files->mounted) != 0) {
        hex_runtime_trap("[Runtime Error] file server freed while mounted\n");
    }
    hex_files_close_handle(files->root);
    hex_heap_free(files->index);
    hex_heap_free(files->cache_control);
    hex_heap_free(files);
}

void hex_http_files_attach(hex_http_files files) {
    atomic_fetch_add(&files->mounted, 1);
}

void hex_http_files_detach(hex_http_files files) {
    atomic_fetch_sub(&files->mounted, 1);
}

// --- Request decoding ---

// hex_files_decode percent-decodes a raw request path exactly once into
// normalized components: separators collapse, an encoded slash or backslash, an
// embedded NUL, and a malformed escape are refused, and a dot or dot-dot
// component is a traversal. The result is never longer than the input.
enum {
    HEX_FILES_DECODED,
    HEX_FILES_BAD_REQUEST,
    HEX_FILES_TRAVERSAL,
};

static int hex_files_hex(uint8_t byte) {
    if (byte >= '0' && byte <= '9') {
        return byte - '0';
    }
    if (byte >= 'a' && byte <= 'f') {
        return byte - 'a' + 10;
    }
    if (byte >= 'A' && byte <= 'F') {
        return byte - 'A' + 10;
    }
    return -1;
}

// hex_files_component_refused reports a finished component that must not be
// served: a dot or dot-dot traversal, or a hidden name when dotfiles are off.
static bool hex_files_component_refused(const uint8_t *name, size_t size, bool dotfiles) {
    if (size == 1 && name[0] == '.') {
        return true;
    }
    if (size == 2 && name[0] == '.' && name[1] == '.') {
        return true;
    }
    return !dotfiles && size != 0 && name[0] == '.';
}

static int hex_files_decode(const uint8_t *raw, size_t length, bool dotfiles, uint8_t *out, size_t *out_length) {
    size_t written = 0;
    size_t component_start = 0;
    bool component_open = false;
    for (size_t index = 0; index < length;) {
        uint8_t byte = raw[index];
        if (byte == '?' || byte == '#') {
            // A query is not a file name, and the request path carries none.
            return HEX_FILES_BAD_REQUEST;
        }
        if (byte == '%') {
            if (length - index < 3) {
                return HEX_FILES_BAD_REQUEST;
            }
            int high = hex_files_hex(raw[index + 1]);
            int low = hex_files_hex(raw[index + 2]);
            if (high < 0 || low < 0) {
                return HEX_FILES_BAD_REQUEST;
            }
            byte = (uint8_t)(high * 16 + low);
            index += 3;
            if (byte == '/' || byte == '\\' || byte == 0) {
                return HEX_FILES_BAD_REQUEST;
            }
        } else {
            index++;
            if (byte == '\\' || byte == 0) {
                return HEX_FILES_BAD_REQUEST;
            }
            if (byte == '/') {
                if (component_open) {
                    if (hex_files_component_refused(out + component_start, written - component_start, dotfiles)) {
                        return HEX_FILES_TRAVERSAL;
                    }
                    out[written++] = '/';
                    component_open = false;
                }
                continue;
            }
        }
        if (!component_open) {
            component_open = true;
            component_start = written;
        }
        out[written++] = byte;
    }
    if (component_open) {
        if (hex_files_component_refused(out + component_start, written - component_start, dotfiles)) {
            return HEX_FILES_TRAVERSAL;
        }
    } else if (written != 0) {
        // A trailing slash left a separator with no component after it.
        written--;
    }
    *out_length = written;
    return HEX_FILES_DECODED;
}

// --- Response helpers ---

#define HEX_FILES_NAME(identifier, text) \
    static const hex_string identifier = { .data = (const uint8_t *)(text), .byte_length = sizeof(text) - 1 }

HEX_FILES_NAME(hex_files_name_content_type, "Content-Type");
HEX_FILES_NAME(hex_files_name_last_modified, "Last-Modified");
HEX_FILES_NAME(hex_files_name_etag, "ETag");
HEX_FILES_NAME(hex_files_name_cache_control, "Cache-Control");
HEX_FILES_NAME(hex_files_name_accept_ranges, "Accept-Ranges");
HEX_FILES_NAME(hex_files_name_content_range, "Content-Range");
HEX_FILES_NAME(hex_files_name_allow, "Allow");
HEX_FILES_NAME(hex_files_name_range, "Range");
HEX_FILES_NAME(hex_files_name_if_none_match, "If-None-Match");
HEX_FILES_NAME(hex_files_name_if_modified_since, "If-Modified-Since");

static hex_http_status_result hex_files_header(hex_http_writer writer, const hex_string *name, const uint8_t *value, size_t length) {
    return hex_http_writer_header_raw(writer, name, (hex_slice_UInt8){ .data = value, .length = length });
}

static hex_http_status_result hex_files_header_text(hex_http_writer writer, const hex_string *name, const char *text) {
    return hex_files_header(writer, name, (const uint8_t *)text, strlen(text));
}

// hex_files_respond answers with a status and a one-line plain-text body: no
// physical path and no error text ever enters a response.
static hex_http_status_result hex_files_respond(hex_http_writer writer, uint16_t status, const char *reason, const char *allow, const char *range) {
    hex_http_status_result result = hex_http_writer_status_raw(writer, status);
    if (!result.ok) {
        return result;
    }
    if (allow != nullptr) {
        result = hex_files_header_text(writer, &hex_files_name_allow, allow);
        if (!result.ok) {
            return result;
        }
    }
    if (range != nullptr) {
        result = hex_files_header_text(writer, &hex_files_name_content_range, range);
        if (!result.ok) {
            return result;
        }
    }
    result = hex_files_header_text(writer, &hex_files_name_content_type, "text/plain; charset=utf-8");
    if (!result.ok) {
        return result;
    }
    uint8_t body[64];
    size_t length = strlen(reason);
    memcpy(body, reason, length);
    body[length++] = '\n';
    return hex_http_writer_write_raw(writer, (hex_slice_UInt8){ .data = body, .length = length });
}

static size_t hex_files_decimal(uint8_t *buffer, size_t length, uint64_t value) {
    char digits[20];
    size_t count = 0;
    do {
        digits[count++] = (char)('0' + value % 10);
        value /= 10;
    } while (value != 0);
    while (count != 0) {
        buffer[length++] = (uint8_t)digits[--count];
    }
    return length;
}

// --- MIME ---

typedef struct hex_files_mime {
    const char *extension;
    const char *type;
} hex_files_mime;

static const hex_files_mime hex_files_mime_table[] = {
    { "html", "text/html; charset=utf-8" },
    { "css", "text/css; charset=utf-8" },
    { "js", "text/javascript; charset=utf-8" },
    { "json", "application/json; charset=utf-8" },
    { "png", "image/png" },
    { "jpg", "image/jpeg" },
    { "jpeg", "image/jpeg" },
    { "gif", "image/gif" },
    { "svg", "image/svg+xml" },
    { "woff", "font/woff" },
    { "woff2", "font/woff2" },
    { "ttf", "font/ttf" },
    { "mp4", "video/mp4" },
    { "webm", "video/webm" },
    { "pdf", "application/pdf" },
    { "zip", "application/zip" },
    { "txt", "text/plain; charset=utf-8" },
    { "xml", "application/xml; charset=utf-8" },
};

static bool hex_files_equals_nocase(const uint8_t *left, const char *right, size_t length) {
    for (size_t index = 0; index < length; index++) {
        uint8_t a = left[index];
        uint8_t b = (uint8_t)right[index];
        if (a >= 'A' && a <= 'Z') {
            a = (uint8_t)(a + ('a' - 'A'));
        }
        if (a != b) {
            return false;
        }
    }
    return true;
}

// hex_files_mime_type looks at the extension of the last path component only.
static const char *hex_files_mime_type(const uint8_t *path, size_t length) {
    size_t dot = length;
    for (size_t index = length; index > 0; index--) {
        if (path[index - 1] == '/') {
            break;
        }
        if (path[index - 1] == '.') {
            dot = index - 1;
            break;
        }
    }
    if (dot == length) {
        return "application/octet-stream";
    }
    const uint8_t *extension = path + dot + 1;
    size_t size = length - dot - 1;
    for (size_t index = 0; index < sizeof(hex_files_mime_table) / sizeof(hex_files_mime_table[0]); index++) {
        const hex_files_mime *row = &hex_files_mime_table[index];
        if (strlen(row->extension) == size && hex_files_equals_nocase(extension, row->extension, size)) {
            return row->type;
        }
    }
    return "application/octet-stream";
}

// --- Range and validators ---

typedef struct hex_files_range {
    bool present;
    bool satisfiable;
    uint64_t first;
    uint64_t last;
} hex_files_range;

// hex_files_parse_number reads decimal digits, saturating at UINT64_MAX: a
// syntactically valid number too large for the size is simply beyond the file.
static bool hex_files_parse_number(const uint8_t *text, size_t length, size_t *cursor, uint64_t *value) {
    size_t start = *cursor;
    uint64_t result = 0;
    while (*cursor < length && text[*cursor] >= '0' && text[*cursor] <= '9') {
        uint64_t digit = (uint64_t)(text[*cursor] - '0');
        if (ckd_mul(&result, result, (uint64_t)10) || ckd_add(&result, result, digit)) {
            result = UINT64_MAX;
            while (*cursor < length && text[*cursor] >= '0' && text[*cursor] <= '9') {
                (*cursor)++;
            }
            *value = result;
            return true;
        }
        (*cursor)++;
    }
    *value = result;
    return *cursor > start;
}

// hex_files_parse_range interprets one Range header against the file size. A
// header that is not a single well-formed byte range is ignored, so the full
// representation is served; a well-formed range outside the file is
// unsatisfiable.
static hex_files_range hex_files_parse_range(const uint8_t *text, size_t length, uint64_t size) {
    hex_files_range range = { 0 };
    size_t cursor = 0;
    while (cursor < length && (text[cursor] == ' ' || text[cursor] == '\t')) {
        cursor++;
    }
    if (length - cursor < 6 || !hex_files_equals_nocase(text + cursor, "bytes=", 6)) {
        return range;
    }
    cursor += 6;
    while (cursor < length && (text[cursor] == ' ' || text[cursor] == '\t')) {
        cursor++;
    }
    uint64_t first = 0;
    uint64_t last = 0;
    bool has_first = hex_files_parse_number(text, length, &cursor, &first);
    if (cursor >= length || text[cursor] != '-') {
        return range;
    }
    cursor++;
    bool has_last = hex_files_parse_number(text, length, &cursor, &last);
    while (cursor < length && (text[cursor] == ' ' || text[cursor] == '\t')) {
        cursor++;
    }
    if (cursor != length || (!has_first && !has_last)) {
        // Trailing text, including a second range after a comma, makes the
        // header unsupported.
        return range;
    }
    if (has_first && has_last && first > last) {
        return range;
    }
    range.present = true;
    if (!has_first) {
        // A suffix range names the final `last` bytes.
        if (last == 0 || size == 0) {
            return range;
        }
        range.first = last >= size ? 0 : size - last;
        range.last = size - 1;
        range.satisfiable = true;
        return range;
    }
    if (size == 0 || first >= size) {
        return range;
    }
    range.first = first;
    range.last = (!has_last || last >= size) ? size - 1 : last;
    range.satisfiable = true;
    return range;
}

// hex_files_etag_matches applies weak comparison: the opaque tags are compared
// and a W/ prefix on either side is ignored. A list may hold several tags.
static bool hex_files_etag_matches(const uint8_t *list, size_t length, const uint8_t *etag, size_t etag_length) {
    size_t cursor = 0;
    while (cursor < length) {
        while (cursor < length && (list[cursor] == ' ' || list[cursor] == '\t' || list[cursor] == ',')) {
            cursor++;
        }
        if (cursor >= length) {
            break;
        }
        if (list[cursor] == '*') {
            return true;
        }
        if (length - cursor >= 2 && list[cursor] == 'W' && list[cursor + 1] == '/') {
            cursor += 2;
        }
        size_t start = cursor;
        if (cursor < length && list[cursor] == '"') {
            cursor++;
            while (cursor < length && list[cursor] != '"') {
                cursor++;
            }
            if (cursor < length) {
                cursor++;
            }
        } else {
            while (cursor < length && list[cursor] != ',') {
                cursor++;
            }
        }
        if (cursor - start == etag_length && memcmp(list + start, etag, etag_length) == 0) {
            return true;
        }
    }
    return false;
}

// --- Serving ---

constexpr size_t HEX_FILES_CHUNK = 65536;

static bool hex_files_request_header(hex_http_request request, const hex_string *name, hex_slice_UInt8 *value) {
    hex_http_bytes_result found = hex_http_request_header_raw(request, name);
    if (found.found) {
        *value = found.bytes;
    }
    return found.found;
}

static hex_http_status_result hex_files_transfer(hex_files_found *found, hex_http_writer writer, uint64_t first, uint64_t count) {
    uint64_t remaining = count;
    uint64_t offset = first;
    size_t chunk = remaining < HEX_FILES_CHUNK ? (size_t)remaining : HEX_FILES_CHUNK;
    uint8_t *buffer = hex_heap_allocate_or_null(chunk == 0 ? 1 : chunk);
    if (buffer == nullptr) {
        return hex_files_failure(hex_files_kind(hex_tag_ErrorKind_ResourceExhausted), &hex_files_message_memory);
    }
    hex_http_status_result result = { .ok = true };
    while (remaining != 0) {
        size_t want = remaining < chunk ? (size_t)remaining : chunk;
        hex_files_read job = { .handle = found->handle, .offset = offset, .buffer = buffer, .length = want };
        hex_event_work_call(hex_files_read_entry, hex_files_read_failed, &job);
        if (job.failed) {
            result = hex_files_failure(hex_files_kind_other(), &hex_files_message_failed);
            break;
        }
        if (job.count != want) {
            // The file shrank after its length was announced.
            result = hex_files_failure(hex_files_kind_other(), &hex_files_message_changed);
            break;
        }
        result = hex_http_writer_write_raw(writer, (hex_slice_UInt8){ .data = buffer, .length = want });
        if (!result.ok) {
            break;
        }
        offset += want;
        remaining -= want;
    }
    hex_heap_free(buffer);
    return result;
}

static hex_http_status_result hex_files_serve(hex_http_files files, hex_http_request request, hex_http_writer writer, const uint8_t *raw, size_t raw_length, bool directory) {
    hex_slice_UInt8 method = hex_http_request_method_raw(request);
    bool head = method.length == 4 && memcmp(method.data, "HEAD", 4) == 0;
    bool get = method.length == 3 && memcmp(method.data, "GET", 3) == 0;
    if (!head && !get) {
        return hex_files_respond(writer, 405, "Method Not Allowed", "GET, HEAD", nullptr);
    }
    uint8_t *decoded = hex_heap_allocate_or_null(raw_length == 0 ? 1 : raw_length);
    if (decoded == nullptr) {
        return hex_files_respond(writer, 500, "Internal Server Error", nullptr, nullptr);
    }
    size_t decoded_length = 0;
    int decoding = hex_files_decode(raw, raw_length, files->dotfiles, decoded, &decoded_length);
    if (decoding != HEX_FILES_DECODED) {
        hex_heap_free(decoded);
        return decoding == HEX_FILES_TRAVERSAL ? hex_files_respond(writer, 403, "Forbidden", nullptr, nullptr)
                                               : hex_files_respond(writer, 400, "Bad Request", nullptr, nullptr);
    }
    hex_files_walk walk = { .files = files, .path = decoded, .length = decoded_length, .directory_ok = directory };
    hex_event_work_call(hex_files_walk_entry, hex_files_job_failed, &walk);
    if (walk.outcome != HEX_FILES_OK) {
        hex_heap_free(decoded);
        switch (walk.outcome) {
        case HEX_FILES_NOT_FOUND:
            return hex_files_respond(writer, 404, "Not Found", nullptr, nullptr);
        case HEX_FILES_FORBIDDEN:
            return hex_files_respond(writer, 403, "Forbidden", nullptr, nullptr);
        default:
            return hex_files_respond(writer, 500, "Internal Server Error", nullptr, nullptr);
        }
    }
    hex_files_found found = walk.found;
    // The MIME type follows the file actually opened: the index file's own
    // name when the path named a directory.
    const char *mime = walk.indexed ? hex_files_mime_type(files->index, files->index_length)
                                    : hex_files_mime_type(decoded, decoded_length);
    hex_http_status_result result = { .ok = true };

    uint8_t etag[64];
    size_t etag_length = 0;
    etag[etag_length++] = '"';
    etag_length = hex_files_decimal(etag, etag_length, found.size);
    etag[etag_length++] = '-';
    etag_length = hex_files_decimal(etag, etag_length, found.modified < 0 ? 0 : (uint64_t)found.modified);
    etag[etag_length++] = '"';
    uint8_t weak[68];
    memcpy(weak, "W/", 2);
    memcpy(weak + 2, etag, etag_length);
    size_t weak_length = etag_length + 2;
    uint8_t modified[40];
    size_t modified_length = hex_http_format_date(modified, found.modified < 0 ? 0 : found.modified);

    // Conditional requests: If-None-Match decides alone when present.
    bool not_modified = false;
    hex_slice_UInt8 condition;
    if (hex_files_request_header(request, &hex_files_name_if_none_match, &condition)) {
        not_modified = hex_files_etag_matches(condition.data, condition.length, etag, etag_length);
    } else if (hex_files_request_header(request, &hex_files_name_if_modified_since, &condition)) {
        int64_t since;
        not_modified = hex_http_parse_date(condition.data, condition.length, &since) && found.modified <= since;
    }

    hex_files_range range = { 0 };
    hex_slice_UInt8 range_header;
    if (get && !not_modified && hex_files_request_header(request, &hex_files_name_range, &range_header)) {
        range = hex_files_parse_range(range_header.data, range_header.length, found.size);
    }

    uint16_t status = 200;
    uint64_t first = 0;
    uint64_t count = found.size;
    if (not_modified) {
        status = 304;
    } else if (range.present && !range.satisfiable) {
        status = 416;
    } else if (range.present) {
        status = 206;
        first = range.first;
        count = range.last - range.first + 1;
    }

    result = hex_http_writer_status_raw(writer, status);
    if (result.ok && status == 416) {
        uint8_t line[48];
        size_t length = 0;
        memcpy(line, "bytes */", 8);
        length = hex_files_decimal(line, 8, found.size);
        result = hex_files_header(writer, &hex_files_name_content_range, line, length);
        if (result.ok) {
            result = hex_files_header_text(writer, &hex_files_name_content_type, "text/plain; charset=utf-8");
        }
        if (result.ok) {
            static const uint8_t body[] = "Range Not Satisfiable\n";
            result = hex_http_writer_write_raw(writer, (hex_slice_UInt8){ .data = body, .length = sizeof(body) - 1 });
        }
        hex_files_close_handle(found.handle);
        hex_heap_free(decoded);
        return result;
    }
    if (result.ok) {
        result = hex_files_header(writer, &hex_files_name_etag, weak, weak_length);
    }
    if (result.ok) {
        result = hex_files_header(writer, &hex_files_name_last_modified, modified, modified_length);
    }
    if (result.ok && files->cache_length != 0) {
        result = hex_files_header(writer, &hex_files_name_cache_control, files->cache_control, files->cache_length);
    }
    if (result.ok && status != 304) {
        result = hex_files_header_text(writer, &hex_files_name_accept_ranges, "bytes");
    }
    if (result.ok && status != 304) {
        result = hex_files_header_text(writer, &hex_files_name_content_type, mime);
    }
    if (result.ok && status == 206) {
        uint8_t line[80];
        size_t length = 0;
        memcpy(line, "bytes ", 6);
        length = hex_files_decimal(line, 6, first);
        line[length++] = '-';
        length = hex_files_decimal(line, length, first + count - 1);
        line[length++] = '/';
        length = hex_files_decimal(line, length, found.size);
        result = hex_files_header(writer, &hex_files_name_content_range, line, length);
    }
    if (result.ok && status != 304) {
        result = hex_http_writer_content_length_raw(writer, (size_t)count);
    }
    if (result.ok && status != 304 && !head && count != 0) {
        result = hex_files_transfer(&found, writer, first, count);
    }
    hex_files_close_handle(found.handle);
    hex_heap_free(decoded);
    return result;
}

hex_http_status_result hex_http_files_serve_file_raw(hex_http_files files, hex_http_request request, hex_http_writer writer, const hex_string *text) {
    return hex_files_serve(files, request, writer, text->data, text->byte_length, false);
}

hex_http_status_result hex_http_files_serve_directory_raw(hex_http_files files, hex_http_request request, hex_http_writer writer, const hex_string *text) {
    return hex_files_serve(files, request, writer, text->data, text->byte_length, true);
}

bool hex_http_files_serve_mounted(hex_http_files files, hex_http_request request, hex_http_writer writer, const uint8_t *path, size_t length) {
    return hex_files_serve(files, request, writer, path, length, true).ok;
}
