#ifndef HEXAL_PROGRAM_H
#define HEXAL_PROGRAM_H

#include "hexal.h"
#include "hexal/error.h"
#include "hexal/heap.h"
#include "hexal/string.h"

// The path surface is selected by std/program path queries and
// available_parallelism. Its result structs name hex_string and
// hex_t_ErrorKind, so this header always carries the Error/String/Heap/Slice
// dependency; the component source gates the libuv body instead.
{{if .Paths}}
// hex_program_string_result is the raw result of one path query: a caller-
// Heap-owned String on success, or a stable ErrorKind and fixed message on
// failure. Every path query shares this shape.
typedef struct hex_program_string_result {
    bool ok;
    const hex_string *value;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_program_string_result;

hex_program_string_result hex_program_current_directory(hex_heap heap);
hex_program_string_result hex_program_home_directory(hex_heap heap);
hex_program_string_result hex_program_temporary_directory(hex_heap heap);
hex_program_string_result hex_program_executable_path(hex_heap heap);
size_t hex_program_available_parallelism(void);

{{if .Event}}
hex_program_string_result hex_program_current_directory_task(hex_heap heap);
hex_program_string_result hex_program_home_directory_task(hex_heap heap);
hex_program_string_result hex_program_temporary_directory_task(hex_heap heap);
hex_program_string_result hex_program_executable_path_task(hex_heap heap);
{{end}}
{{end}}
{{if .Arguments}}
// hex_program_arguments_result mirrors hex_program_string_result for the
// argument snapshot: items is a contiguous array of count non-owning
// hex_string values, directly compatible with any Slice<String>'s
// {data, length} representation.
typedef struct hex_program_arguments_result {
    bool ok;
    const hex_string *items;
    size_t count;
    hex_t_ErrorKind kind;
    const hex_string *message;
} hex_program_arguments_result;

// hex_program_arguments_init runs exactly once in the entry adapter, before
// any module statement, when arguments() is reachable. POSIX passes the
// resolved (argc, argv) -- uv_setup_args's return value when executable-path
// demand also ran it, otherwise main's own parameters. Windows reads
// GetCommandLineW() itself and takes no parameters.
#if defined(_WIN32)
void hex_program_arguments_init(void);
#else
void hex_program_arguments_init(int argc, char **argv);
#endif
hex_program_arguments_result hex_program_arguments(void);
{{end}}

#endif
