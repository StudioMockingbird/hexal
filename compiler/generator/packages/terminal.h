#ifndef HEXAL_TERMINAL_H
#define HEXAL_TERMINAL_H

#include "hexal.h"

// TerminalSize is the complete, immutable result of Terminal.size: the
// visible column and row counts of an attached terminal. Reachable
// construction or matching alone selects only this definition, never the
// native query runtime below.
typedef struct hex_t_TerminalSize {
    size_t hex_m_columns;
    size_t hex_m_rows;
} hex_t_TerminalSize;
{{- if .Operations}}

#include "hexal/io.h"

enum {
    HEX_TERMINAL_OK = 0,
    HEX_TERMINAL_ERROR = 1,
    HEX_TERMINAL_NOT_A_TERMINAL = 2,
    HEX_TERMINAL_INVALID_DIMENSIONS = 3,
};

typedef struct hex_terminal_attached_result {
    int status;
    bool attached;
    long long code;
} hex_terminal_attached_result;

// hex_terminal_is_attached takes no ownership of stream and changes no
// terminal state. attached is meaningful only when status is
// HEX_TERMINAL_OK; a valid non-terminal stream is success with attached
// false, never HEX_TERMINAL_NOT_A_TERMINAL, which hex_terminal_size alone
// uses.
hex_terminal_attached_result hex_terminal_is_attached(hex_io stream);

typedef struct hex_terminal_size_result {
    int status;
    size_t columns;
    size_t rows;
    long long code;
} hex_terminal_size_result;

// hex_terminal_size succeeds only for an attached terminal with positive
// visible dimensions; code is meaningful only when status is
// HEX_TERMINAL_ERROR.
hex_terminal_size_result hex_terminal_size(hex_io stream);
{{- end}}

#endif
