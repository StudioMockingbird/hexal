#ifndef HEXAL_PROCESS_H
#define HEXAL_PROCESS_H

#include "hexal.h"
#include "hexal/string.h"
#include "hexal/slice.h"
#include "hexal/error.h"
#include "hexal/handle.h"

// hex_list_String, hex_list_EnvironmentVariable, and hex_list_UInt8 are used
// here only by pointer; hexal/list.h itself includes this header when one of
// their element types is reachable, and a mutual #include would leave one
// side's struct incomplete when the other needs it whole (see the identical
// hex_list_Address comment in hexal/network.h).
typedef struct hex_list_String hex_list_String;
typedef struct hex_list_EnvironmentVariable hex_list_EnvironmentVariable;
typedef struct hex_list_UInt8 hex_list_UInt8;

// The protected process/IPC value types. Their tags are ordinary
// compilation-assigned discriminants (hex_tag_Environment_Replace and so on,
// resolved through the program-wide tag registry exactly like any other
// reachable ADT or union) -- unlike hex_t_Address in hexal/network.h, these
// are reachable through ordinary variant construction and match narrowing, so
// nothing here may assume a fixed numeric tag. Every module-owned adapter in
// hexal/process.h's generated inline helpers translates a checked tag to one
// of this header's plain enums below before calling into hexal/process.c, and
// translates a plain result back into the correct tag on the way out; the
// runtime functions declared past this point never read or write a hex_tag.
typedef struct hex_t_EnvironmentVariable {
    const hex_string *hex_m_name;
    const hex_string *hex_m_value;
} hex_t_EnvironmentVariable;

typedef struct hex_t_Environment {
    hex_tag tag;
    union {
        struct {
            hex_list_EnvironmentVariable *hex_m_values;
        } Replace;
    } payload;
} hex_t_Environment;

typedef struct hex_t_ProcessStream {
    hex_tag tag;
} hex_t_ProcessStream;

// hex_t_String_Nil is the fixed structural `String | Nil` union
// ProcessOptions.working_directory carries: String is not pointer-like, so it
// cannot use the null-pointer-niche Nil representation ordinary `Ptr<T> |
// Nil` fields use.
typedef struct hex_t_String_Nil {
    hex_tag tag;
    union {
        const hex_string *hex_m_String;
    } payload;
} hex_t_String_Nil;

typedef struct hex_t_ProcessOptions {
    const hex_string *hex_m_program;
    hex_list_String *hex_m_arguments;
    hex_t_Environment hex_m_environment;
    hex_t_String_Nil hex_m_working_directory;
    hex_t_ProcessStream hex_m_input;
    hex_t_ProcessStream hex_m_output;
    hex_t_ProcessStream hex_m_error;
} hex_t_ProcessOptions;

typedef struct hex_t_ExitStatus {
    hex_tag tag;
    union {
        struct {
            int64_t hex_m_code;
        } Exited;
    } payload;
} hex_t_ExitStatus;

// Process and Pipe wrap the shared generation-checked handle; every operation
// resolves it before touching native state.
typedef struct hex_process {
    hex_handle handle;
} hex_process;

typedef struct hex_pipe {
    hex_handle handle;
} hex_pipe;

// hex_t_Pipe_Nil is the fixed structural `Pipe | Nil` union StartedProcess's
// three stream fields carry: Pipe is a plain generation-checked struct, not a
// raw pointer, so it cannot use the pointer-null-niche optimization either.
typedef struct hex_t_Pipe_Nil {
    hex_tag tag;
    union {
        hex_pipe hex_m_Pipe;
    } payload;
} hex_t_Pipe_Nil;

typedef struct hex_t_StartedProcess {
    hex_process hex_m_process;
    hex_t_Pipe_Nil hex_m_input;
    hex_t_Pipe_Nil hex_m_output;
    hex_t_Pipe_Nil hex_m_error;
} hex_t_StartedProcess;
{{- if .Operations}}

enum {
    HEX_PROCESS_INVALID_INPUT = 1,
    HEX_PROCESS_PERMISSION_DENIED = 2,
    HEX_PROCESS_CLOSED = 3,
    HEX_PROCESS_ALLOCATION_FAILED = 4,
    HEX_PROCESS_EOS = 5,
    HEX_PROCESS_BUSY = 6,
};

// The three ProcessStream raw values a module-owned adapter translates a
// checked hex_t_ProcessStream tag into before calling hex_process_start.
enum {
    HEX_PROCESS_STREAM_IGNORE = 0,
    HEX_PROCESS_STREAM_INHERIT = 1,
    HEX_PROCESS_STREAM_PIPE = 2,
};

// hex_process_options is the plain-C spawn request a module-owned adapter
// builds from one checked hex_t_ProcessOptions value: every ADT and union tag
// has already been resolved to the raw fields below. arguments is never
// NULL (an empty argument list is legal); environment_values is read only
// when environment_replace is true; working_directory is NULL for "inherit".
typedef struct hex_process_options {
    const hex_string *program;
    hex_list_String *arguments;
    bool environment_replace;
    hex_list_EnvironmentVariable *environment_values;
    const hex_string *working_directory;
    uint8_t input;
    uint8_t output;
    uint8_t error;
} hex_process_options;

typedef struct hex_process_pipe_result {
    hex_pipe pipe;
    bool present;
} hex_process_pipe_result;

typedef struct hex_process_start_result {
    int status;
    hex_process process;
    hex_process_pipe_result input;
    hex_process_pipe_result output;
    hex_process_pipe_result error;
} hex_process_start_result;

// hex_process_start parks the calling Task. Validation (embedded NUL,
// environment name shape, target-defined duplicate names) runs before any
// native submission; a staged spawn failure closes every already-initialized
// native handle and leaves no child or zombie reachable from Hexal.
hex_process_start_result hex_process_start(hex_process_options options);

typedef struct hex_process_wait_result {
    int status;
    bool terminated;
    int64_t exit_code;
} hex_process_wait_result;

// hex_process_wait parks the calling Task until the process exits, or
// returns immediately with the cached result when it already has. It may be
// called concurrently and after exit; every successful call observes the
// same result.
hex_process_wait_result hex_process_wait(hex_process process);
// hex_process_terminate requests uv_process_kill(process, SIGTERM); it is the
// one v1 termination request, with no separate forceful companion.
int hex_process_terminate(hex_process process);
// hex_process_close invalidates the public handle immediately. It never
// kills the process and never closes the native handle before the exit
// callback has reaped the child.
int hex_process_close(hex_process process);

typedef struct hex_pipe_transfer {
    int status;
    size_t count;
} hex_pipe_transfer;

// A second concurrent read or write on one Pipe returns HEX_PROCESS_BUSY
// without consuming bytes or blocking, matching TCP's connection contract.
// An operation contrary to the Pipe's fixed direction (input is writable
// only; output and error are readable only) returns HEX_PROCESS_PERMISSION_DENIED.
hex_pipe_transfer hex_pipe_read(hex_pipe pipe_value, hex_list_UInt8 *into, size_t max);
int hex_pipe_write(hex_pipe pipe_value, hex_slice_UInt8 from);
int hex_pipe_shutdown(hex_pipe pipe_value);
int hex_pipe_close(hex_pipe pipe_value);

// pipe distinguishes the fallback header a Pipe failure gets ("pipe error")
// from a Process failure's ("process error"); every other status maps the
// same way regardless.
hex_t_Error hex_process_error(size_t line, size_t column, int status, bool pipe, const hex_string *message);
{{- end}}

#endif
