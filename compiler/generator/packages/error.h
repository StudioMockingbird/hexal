#ifndef HEXAL_ERROR_H
#define HEXAL_ERROR_H

#include "hexal.h"
#include "hexal/string.h"

// hex_t_ErrorKind is the protected builtin classification. Every unit
// variant is zero-filled in other_header; Other stores its caller-supplied
// header there. One flat field, not a per-variant payload union, since
// ErrorKind has exactly one payload-carrying variant.
typedef struct hex_t_ErrorKind {
    hex_tag tag;
    hex_strand other_header;
} hex_t_ErrorKind;

typedef struct hex_t_Error hex_t_Error;
struct hex_t_Error {
    const hex_string *hex_m_file;
    size_t hex_m_line;
    size_t hex_m_column;
    hex_t_ErrorKind hex_m_kind;
    const hex_string *hex_m_message;
};

// hex_error_kind_header is the one ErrorKind-to-header switch: the fixed
// Strand for a unit tag, the caller-supplied Strand for Other, and a trap for
// any tag that is not an ErrorKind tag. Selecting Error or ErrorKind
// unconditionally registers every variant below, so this switch is always
// complete for the program's own tags.
static inline hex_strand hex_error_kind_header(hex_t_ErrorKind kind) {
    switch (kind.tag) {
{{- range .KindVariants}}
    case {{.Tag}}:
        return (hex_strand){ {{.HeaderLiteral}} };
{{- end}}
    case {{.OtherTag}}:
        return kind.other_header;
    default:
        hex_runtime_trap("[Runtime Error] invalid ErrorKind tag\n");
    }
}

#endif
