#ifndef HEXAL_STASH_H
#define HEXAL_STASH_H

#include "hexal.h"

/* A Stash<T> value is hex_stash * for every T: the core is type-erased,
   recording T's size and alignment once at construction. Only the typed
   constructor and allocate wrappers (emitted per module, per reachable T)
   need the element type; reset and destroy operate on this shared struct
   directly. */
typedef struct hex_stash_block {
    struct hex_stash_block *next;
    unsigned char *data;
    size_t capacity;
    size_t used;
} hex_stash_block;

typedef struct hex_stash {
    hex_stash_block *first;
    hex_stash_block *current;
    size_t element_size;
    size_t element_align;
} hex_stash;

hex_stash *hex_stash_new(size_t element_size, size_t element_align);
void *hex_stash_allocate(hex_stash *stash);
void hex_stash_reset(hex_stash *stash);
void hex_stash_destroy(hex_stash *stash);

#endif
