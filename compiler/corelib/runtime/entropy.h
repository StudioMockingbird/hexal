#ifndef HEXAL_ENTROPY_H
#define HEXAL_ENTROPY_H

#include "hexal.h"
#include "hexal/error.h"

// hex_entropy_fill_result carries no value: success fills the destination in
// place. Failure leaves destination contents unspecified. The raw entry
// point takes a plain pointer and length rather than a named Slice<mut Byte>
// specialization, so this header depends on no per-compilation collection
// type name.
typedef struct hex_entropy_fill_result {
    bool ok;
    hex_t_ErrorKind kind;
} hex_entropy_fill_result;

hex_entropy_fill_result hex_entropy_fill(uint8_t *data, size_t length);
{{if .Event}}
hex_entropy_fill_result hex_entropy_fill_task(uint8_t *data, size_t length);
{{end}}
#endif
