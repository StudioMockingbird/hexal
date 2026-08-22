#ifndef HEXAL_IO_H
#define HEXAL_IO_H

#include "hexal.h"
#include "hexal/list.h"
#include "hexal/view.h"
#include "hexal/error.h"

typedef struct hex_io {
    intptr_t desc;
    uint8_t access;
    bool owned;
} hex_io;

typedef struct hex_bytes {
    hex_list_UInt8 *buffer;
    size_t cursor;
} hex_bytes;

typedef enum hex_io_status : uint8_t {
    HEX_IO_OK = 0,
    HEX_IO_EOS = 1,
    HEX_IO_ERROR = 2,
    HEX_IO_NOT_READABLE = 3,
    HEX_IO_NOT_WRITABLE = 4,
    HEX_IO_SELF_READ = 5,
    HEX_IO_OVERLAP = 6,
} hex_io_status;

typedef struct hex_io_open {
    hex_io_status status;
    hex_io stream;
    long long code;
} hex_io_open;

typedef struct hex_io_transfer {
    hex_io_status status;
    size_t count;
    long long code;
} hex_io_transfer;

typedef struct hex_io_position {
    hex_io_status status;
    long long position;
    long long code;
} hex_io_position;

typedef struct hex_io_status_only {
    hex_io_status status;
    long long code;
} hex_io_status_only;

hex_io_open hex_io_stdin(void);
hex_io_open hex_io_stdout(void);
hex_io_open hex_io_stderr(void);
hex_io_transfer hex_io_read(hex_io stream, hex_list_UInt8 *into, size_t max);
hex_io_transfer hex_bytes_read(hex_bytes *stream, hex_list_UInt8 *into, size_t max);
hex_io_transfer hex_io_write(hex_io stream, hex_view_UInt8 from);
hex_io_transfer hex_bytes_write(hex_bytes *stream, hex_view_UInt8 from);
hex_io_position hex_io_seek_start(hex_io stream, uint64_t position);
hex_io_position hex_io_seek_current(hex_io stream, int64_t offset);
hex_io_position hex_io_seek_end(hex_io stream, int64_t offset);
// whence: 0 names Start, 1 Current, 2 End; the memory backend resolves the
// target itself under checked arithmetic and the [0, length] bound.
hex_io_position hex_bytes_seek_from(hex_bytes *stream, uint8_t whence, int64_t offset);
hex_bytes hex_bytes_over(hex_list_UInt8 *buffer);
hex_io_status_only hex_io_close(hex_io stream);
bool hex_io_write_all(intptr_t desc, const uint8_t *data, size_t length);
intptr_t hex_io_stdout_desc(void);
hex_t_Error hex_io_error(size_t line, size_t column, const hex_string *file, const char *operation, long long code, const hex_string *message);

#endif

