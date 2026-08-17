#ifndef HEXAL_HEAP_H
#define HEXAL_HEAP_H

#include "hexal.h"

typedef struct hex_heap {
    uintptr_t identity;
} hex_heap;

#define HEX_HEAP_DEFAULT 0

/* The header's last size_t slot is the offset marker every free reads
   at (pointer - sizeof(size_t)). The live flag must not share that
   region: the marker write at base + offset - 8 would clobber it. */
typedef struct hex_heap_header {
    uintptr_t allocator;
    size_t size;
    size_t offset;
    bool live;
    size_t marker;
} hex_heap_header;

void *hex_heap_raw_allocate(uintptr_t allocator, size_t size, size_t align);
void hex_heap_free(void *pointer, uintptr_t allocator);

#endif
