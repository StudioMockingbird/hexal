/* Terminal detection and dimensions. Both queries are stateless: they
   initialize no loop, retain no terminal handle, change no terminal mode,
   and take no ownership of stream. Windows uses GetFileType/GetConsoleMode
   for classification and GetConsoleScreenBufferInfo's visible srWindow
   rectangle for dimensions, never uv_tty_get_winsize (which would require a
   live uv_tty_t merely to read two values). POSIX uses uv_guess_handle for
   classification and a direct ioctl(TIOCGWINSZ) for dimensions; classifying
   alone links libuv and therefore the native bootstrap, but the size query
   itself does not. Every failure is classified through hex_io_error, the
   same native-error mapper File/IO/Bytes share; this file keeps no second
   classification table. */
{{- if .Operations}}
#include "hexal/terminal.h"

#ifdef _WIN32

#ifndef WIN32_LEAN_AND_MEAN
#define WIN32_LEAN_AND_MEAN
#endif
#include <windows.h>

hex_terminal_attached_result hex_terminal_is_attached(hex_io stream) {
    HANDLE handle = (HANDLE)stream.desc;
    SetLastError(0);
    DWORD file_type = GetFileType(handle);
    if (file_type == FILE_TYPE_UNKNOWN) {
        DWORD error = GetLastError();
        if (error != 0) {
            return (hex_terminal_attached_result){.status = HEX_TERMINAL_ERROR, .attached = false, .code = (long long)error};
        }
        return (hex_terminal_attached_result){.status = HEX_TERMINAL_OK, .attached = false, .code = 0};
    }
    if (file_type != FILE_TYPE_CHAR) {
        return (hex_terminal_attached_result){.status = HEX_TERMINAL_OK, .attached = false, .code = 0};
    }
    DWORD mode = 0;
    bool attached = GetConsoleMode(handle, &mode) != 0;
    return (hex_terminal_attached_result){.status = HEX_TERMINAL_OK, .attached = attached, .code = 0};
}

hex_terminal_size_result hex_terminal_size(hex_io stream) {
    HANDLE handle = (HANDLE)stream.desc;
    hex_terminal_attached_result attached = hex_terminal_is_attached(stream);
    if (attached.status != HEX_TERMINAL_OK) {
        return (hex_terminal_size_result){.status = HEX_TERMINAL_ERROR, .code = attached.code};
    }
    if (!attached.attached) {
        return (hex_terminal_size_result){.status = HEX_TERMINAL_NOT_A_TERMINAL};
    }
    CONSOLE_SCREEN_BUFFER_INFO info;
    if (!GetConsoleScreenBufferInfo(handle, &info)) {
        return (hex_terminal_size_result){.status = HEX_TERMINAL_ERROR, .code = (long long)GetLastError()};
    }
    long long columns = (long long)info.srWindow.Right - (long long)info.srWindow.Left + 1;
    long long rows = (long long)info.srWindow.Bottom - (long long)info.srWindow.Top + 1;
    if (columns <= 0 || rows <= 0) {
        return (hex_terminal_size_result){.status = HEX_TERMINAL_INVALID_DIMENSIONS};
    }
    return (hex_terminal_size_result){.status = HEX_TERMINAL_OK, .columns = (size_t)columns, .rows = (size_t)rows};
}

{{if not .TargetWindows -}}
#else

#include <errno.h>
#include <fcntl.h>
#include <sys/ioctl.h>
#include <termios.h>
#include <uv.h>

// A descriptor query interrupted by a signal is retried; every other
// failure is reported, never retried, matching hex_io_close_native's close
// contract.
static bool hex_terminal_valid_descriptor(int descriptor, long long *code) {
    for (;;) {
        if (fcntl(descriptor, F_GETFD) != -1) {
            return true;
        }
        if (errno == EINTR) {
            continue;
        }
        *code = (long long)errno;
        return false;
    }
}

hex_terminal_attached_result hex_terminal_is_attached(hex_io stream) {
    int descriptor = (int)stream.desc;
    long long code = 0;
    if (!hex_terminal_valid_descriptor(descriptor, &code)) {
        return (hex_terminal_attached_result){.status = HEX_TERMINAL_ERROR, .attached = false, .code = code};
    }
    bool attached = uv_guess_handle(descriptor) == UV_TTY;
    return (hex_terminal_attached_result){.status = HEX_TERMINAL_OK, .attached = attached, .code = 0};
}

hex_terminal_size_result hex_terminal_size(hex_io stream) {
    int descriptor = (int)stream.desc;
    long long code = 0;
    if (!hex_terminal_valid_descriptor(descriptor, &code)) {
        return (hex_terminal_size_result){.status = HEX_TERMINAL_ERROR, .code = code};
    }
    if (uv_guess_handle(descriptor) != UV_TTY) {
        return (hex_terminal_size_result){.status = HEX_TERMINAL_NOT_A_TERMINAL};
    }
    struct winsize size;
    if (ioctl(descriptor, TIOCGWINSZ, &size) != 0) {
        if (errno == ENOTTY) {
            return (hex_terminal_size_result){.status = HEX_TERMINAL_NOT_A_TERMINAL};
        }
        return (hex_terminal_size_result){.status = HEX_TERMINAL_ERROR, .code = (long long)errno};
    }
    if (size.ws_col == 0 || size.ws_row == 0) {
        return (hex_terminal_size_result){.status = HEX_TERMINAL_INVALID_DIMENSIONS};
    }
    return (hex_terminal_size_result){.status = HEX_TERMINAL_OK, .columns = (size_t)size.ws_col, .rows = (size_t)size.ws_row};
}

{{end -}}
#endif
{{- end}}
