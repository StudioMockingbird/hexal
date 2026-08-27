/* Type-erased bump allocation shared by every Stash<T>. Typed constructor
   and allocate wrappers (emitted per module, per reachable T) record T's
   size and alignment once and cast the returned storage; this file never
   sees T. */
#include "hexal/stash.h"
#include "hexal/heap.h"

#define HEX_STASH_MIN_BLOCK ((size_t)4096)

/* Rounds element_size up to the next multiple of element_align so every
   slot in a block starts aligned, given the block's own start address is
   at least element_align-aligned (guaranteed by hex_heap_allocate for any
   alignment this suite supports). */
static size_t hex_stash_stride(const hex_stash *stash) {
    size_t size = stash->element_size;
    size_t align = stash->element_align;
    size_t remainder = size % align;
    if (remainder == 0) {
        return size;
    }
    size_t stride;
    if (ckd_add(&stride, size, align - remainder)) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    return stride;
}

/* Doubles the previous block's capacity until it satisfies stride, or uses
   the exact required capacity when doubling would overflow; the very first
   block is at least HEX_STASH_MIN_BLOCK. */
static hex_stash_block *hex_stash_grow(hex_stash *stash, size_t stride) {
    size_t capacity = HEX_STASH_MIN_BLOCK;
    if (stash->current != nullptr) {
        capacity = stash->current->capacity;
        while (capacity < stride) {
            size_t doubled;
            if (ckd_mul(&doubled, capacity, (size_t)2)) {
                capacity = stride;
                break;
            }
            capacity = doubled;
        }
    }
    if (capacity < stride) {
        capacity = stride;
    }
    hex_stash_block *block = (hex_stash_block *)hex_heap_allocate(sizeof(hex_stash_block));
    block->data = (unsigned char *)hex_heap_allocate(capacity);
    block->capacity = capacity;
    block->used = 0;
    block->next = nullptr;
    if (stash->current != nullptr) {
        stash->current->next = block;
    } else {
        stash->first = block;
    }
    stash->current = block;
    return block;
}

hex_stash *hex_stash_new(size_t element_size, size_t element_align) {
    hex_stash *stash = (hex_stash *)hex_heap_allocate(sizeof(hex_stash));
    stash->first = nullptr;
    stash->current = nullptr;
    stash->element_size = element_size;
    stash->element_align = element_align;
    return stash;
}

void *hex_stash_allocate(hex_stash *stash) {
    size_t stride = hex_stash_stride(stash);
    hex_stash_block *block = stash->current;
    size_t required;
    if (block == nullptr || ckd_add(&required, block->used, stride) || required > block->capacity) {
        if (block != nullptr && block->next != nullptr) {
            /* A retained block from a prior reset is reused in chain order
               before growing. */
            block = block->next;
            block->used = 0;
            stash->current = block;
        } else {
            block = hex_stash_grow(stash, stride);
        }
    }
    void *result = block->data + block->used;
    block->used += stride;
    return result;
}

void hex_stash_reset(hex_stash *stash) {
    /* Rewinding to the first block is enough: hex_stash_allocate resets
       used to 0 for every later block as it advances into it, so the whole
       chain is correctly reusable without an eager walk. Nothing is
       zeroed -- retained bytes may still hold a stale prior value until
       reused. */
    if (stash->first != nullptr) {
        stash->current = stash->first;
        stash->current->used = 0;
    }
}

void hex_stash_destroy(hex_stash *stash) {
    hex_stash_block *block = stash->first;
    while (block != nullptr) {
        hex_stash_block *next = block->next;
        hex_heap_free(block->data);
        hex_heap_free(block);
        block = next;
    }
    hex_heap_free(stash);
}
