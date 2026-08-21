#ifndef HEXAL_PRINT_H
#define HEXAL_PRINT_H

#include "hexal.h"
#include <stdio.h>
#include <inttypes.h>
#include <math.h>

void hex_print_bytes(const uint8_t *data, size_t length);
void hex_print_text(const uint8_t *data, size_t length);
void hex_print_bool(bool value);
void hex_print_nil(void);
void hex_print_int8(int8_t value);
void hex_print_uint8(uint8_t value);
void hex_print_int16(int16_t value);
void hex_print_uint16(uint16_t value);
void hex_print_int32(int32_t value);
void hex_print_uint32(uint32_t value);
void hex_print_int64(int64_t value);
void hex_print_uint64(uint64_t value);
void hex_print_size(size_t value);
void hex_print_float32(float value);
void hex_print_float64(double value);
void hex_print_rune(uint32_t value);
void hex_print_quoted_text(const uint8_t *data, size_t length);
void hex_print_quoted_rune(uint32_t value);

#endif
