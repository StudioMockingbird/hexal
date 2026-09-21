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
    hex_string_128 other_header;
} hex_t_ErrorKind;

// An Error owns nothing: its message and its ErrorKind header are inline, so
// constructing one cannot allocate and there is nothing to release. `file` is a
// compiler-injected literal handle, static-backed and never freed.
typedef struct hex_t_Error hex_t_Error;
struct hex_t_Error {
    const hex_string *hex_m_file;
    size_t hex_m_line;
    size_t hex_m_column;
    hex_t_ErrorKind hex_m_kind;
    hex_string_256 hex_m_message;
};

// hex_error_message copies text into an Error message. Text that does not fit
// traps and is never truncated: error construction is the one operation whose
// failure has nowhere to go, so an over-long message is the caller's bug to
// find loudly.
static inline hex_string_256 hex_error_message(hex_text text) {
    hex_string_256 message = { .byte_length = text.length };
    if (text.length > sizeof message.data) {
        hex_runtime_trap("[Runtime Error] Error message exceeds 256 bytes\n");
    }
    if (text.length != 0) {
        memcpy(message.data, text.data, text.length);
    }
    return message;
}

// hex_error_make builds an Error from a kind, a source site, the file literal,
// and a static message. Every runtime producer's message is authored text.
static inline hex_t_Error hex_error_make(hex_t_ErrorKind kind, size_t line, size_t column, const hex_string *file, const hex_string *message) {
    return (hex_t_Error){
        .hex_m_file = file,
        .hex_m_line = line,
        .hex_m_column = column,
        .hex_m_kind = kind,
        .hex_m_message = hex_error_message(hex_text_heap(message)),
    };
}

// hex_error_kind_header is the one ErrorKind-to-header switch: the fixed
// header for a unit tag, the caller-supplied header for Other, and a trap for
// any tag that is not an ErrorKind tag. Selecting Error or ErrorKind
// unconditionally registers every variant below, so this switch is always
// complete for the program's own tags.
static inline hex_string_128 hex_error_kind_header(hex_t_ErrorKind kind) {
    switch (kind.tag) {
{{- range .KindVariants}}
    case {{.Tag}}:
        return (hex_string_128){ .byte_length = {{.HeaderLength}}, .data = {{.HeaderLiteral}} };
{{- end}}
    case {{.OtherTag}}:
        return kind.other_header;
    default:
        hex_runtime_trap("[Runtime Error] invalid ErrorKind tag\n");
    }
}

#endif
