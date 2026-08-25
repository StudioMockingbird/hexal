#ifndef HEXAL_HEAP_H
#define HEXAL_HEAP_H

#include "hexal.h"

/* The one default allocator has no runtime state to distinguish, so Heap is a
   value token rather than a descriptor. Every Heap value selects the same
   allocator; the token exists only so source Heap expressions keep an
   ordinary C representation and evaluate where they are written. */
typedef unsigned char hex_heap;

void *hex_heap_allocate(size_t size);
void *hex_heap_allocate_zeroed(size_t count, size_t size);
void hex_heap_free(void *pointer);

#endif
