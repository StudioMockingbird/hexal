#ifndef HEXAL_SEEK_H
#define HEXAL_SEEK_H

#include "hexal.h"

typedef struct hex_t_Seek {
    hex_tag tag;
    union {
        struct {
            size_t hex_m_position;
        } Start;
        struct {
            int64_t hex_m_offset;
        } Current;
        struct {
            int64_t hex_m_offset;
        } End;
    } payload;
} hex_t_Seek;

#endif
