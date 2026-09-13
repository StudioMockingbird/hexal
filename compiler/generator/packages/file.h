#ifndef HEXAL_FILE_H
#define HEXAL_FILE_H

#include "hexal.h"
#include "hexal/list.h"
#include "hexal/slice.h"
#include "hexal/error.h"

// desc stores the numeric libuv descriptor without exposing its typedef.
typedef struct hex_file {
    intptr_t desc;
    uint8_t access;
} hex_file;

typedef struct hex_t_FileMode {
    hex_tag tag;
} hex_t_FileMode;

// A core status is 0 for success, a negative libuv result, or one of the
// positive Hexal outcomes below.
enum {
    HEX_FILE_EOS = 1,
    HEX_FILE_NOT_READABLE = 2,
    HEX_FILE_NOT_WRITABLE = 3,
    HEX_FILE_INVALID_PATH = 4,
};

typedef struct hex_file_opened {
    int status;
    hex_file file;
} hex_file_opened;

typedef struct hex_file_transfer {
    int status;
    size_t count;
} hex_file_transfer;

// variant is the FileMode declaration index: Read, Write, Append, ReadWrite,
// CreateNew.
hex_file_opened hex_file_open(const hex_string *path, uint8_t variant);
hex_file_transfer hex_file_read(hex_file file, hex_list_UInt8 *into, size_t max);
hex_file_transfer hex_file_write(hex_file file, hex_slice_UInt8 from);
// whence: 0 names Start, 1 Current, 2 End.
hex_file_transfer hex_file_seek(hex_file file, uint8_t whence, int64_t offset);
int hex_file_flush(hex_file file);
int hex_file_close(hex_file file);
hex_t_Error hex_file_error(size_t line, size_t column, const hex_string *file, int status, bool opening, const hex_string *message);

#endif
