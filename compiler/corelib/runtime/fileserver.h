#ifndef HEXAL_FILESERVER_H
#define HEXAL_FILESERVER_H

#include "hexal.h"
#include "hexal/server.h"
// std/http's static file server. A FileServer is an opaque handle over one
// retained root directory handle and copied configuration; every file is
// opened beneath that handle, one component at a time, so no pathname string
// ever names a file outside the root.

typedef struct hex_http_files_result {
    bool ok;
    hex_http_files files;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_http_files_result;

extern hex_http_files_result hex_http_files_open_raw(hex_heap h, const hex_string *root, const hex_string *index, bool enabled, const hex_string *cache_control);
extern hex_http_status_result hex_http_files_serve_file_raw(hex_http_files files, hex_http_request request, hex_http_writer writer, const hex_string *text);
extern hex_http_status_result hex_http_files_serve_directory_raw(hex_http_files files, hex_http_request request, hex_http_writer writer, const hex_string *text);
extern void hex_http_files_free_raw(hex_http_files files, hex_heap h);

// The connection runtime's entry points: counting a mount against a file
// server, and answering a request whose path matched a mount. The path is the
// raw request path after the mount prefix. The result is whether the response
// completed without a handler-visible failure.
extern void hex_http_files_attach(hex_http_files files);
extern void hex_http_files_detach(hex_http_files files);
extern bool hex_http_files_serve_mounted(hex_http_files files, hex_http_request request, hex_http_writer writer, const uint8_t *path, size_t length);

#endif
