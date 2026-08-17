#ifndef HEXAL_ERROR_H
#define HEXAL_ERROR_H

#include "hexal.h"
#include "hexal/string.h"

typedef struct hex_t_Error hex_t_Error;
struct hex_t_Error {
    const hex_string *hex_m_file;
    size_t hex_m_line;
    size_t hex_m_column;
    hex_strand hex_m_header;
    const hex_string *hex_m_message;
};

#endif
