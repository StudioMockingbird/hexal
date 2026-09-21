#ifndef HEXAL_PRINT_H
#define HEXAL_PRINT_H

#include "hexal.h"
#include <stdio.h>
#include <inttypes.h>
#include <math.h>

// One source print call owns one builder: every helper below appends to it
// and nothing reaches standard output until the single commit, so no other
// writer can land between two fragments of the same call. The 256 bytes of
// inline storage let a short call format without allocating at all; that size
// is an implementation detail of the generated runtime, not a limit on what a
// call may print.
typedef struct hex_print_buffer {
    uint8_t *data;
    size_t length;
    size_t capacity;
    uint8_t inline_storage[256];
} hex_print_buffer;

void hex_print_begin(hex_print_buffer *out);
void hex_print_commit(hex_print_buffer *out);
void hex_print_destroy(hex_print_buffer *out);
void hex_print_text(hex_print_buffer *out, const uint8_t *data, size_t length);
void hex_print_bool(hex_print_buffer *out, bool value);
void hex_print_nil(hex_print_buffer *out);
void hex_print_int8(hex_print_buffer *out, int8_t value);
void hex_print_uint8(hex_print_buffer *out, uint8_t value);
void hex_print_int16(hex_print_buffer *out, int16_t value);
void hex_print_uint16(hex_print_buffer *out, uint16_t value);
void hex_print_int32(hex_print_buffer *out, int32_t value);
void hex_print_uint32(hex_print_buffer *out, uint32_t value);
void hex_print_int64(hex_print_buffer *out, int64_t value);
void hex_print_uint64(hex_print_buffer *out, uint64_t value);
void hex_print_size(hex_print_buffer *out, size_t value);
void hex_print_float32(hex_print_buffer *out, float value);
void hex_print_float64(hex_print_buffer *out, double value);
void hex_print_quoted_text(hex_print_buffer *out, const uint8_t *data, size_t length);

#endif
