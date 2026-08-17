/* Heap allocation runtime: the raw checked allocate/release pair. */
#include "hexal/heap.h"

/* Alignment rounding and the payload offset must not wrap; align == 0 is
   rejected first so align - 1 is never evaluated for it, and both additions
   use ckd_add from <stdckdint.h>. */
void *hex_heap_raw_allocate(uintptr_t allocator, size_t size, size_t align) {
    size_t padded;
    if (align == 0 || ckd_add(&padded, sizeof(hex_heap_header), align - 1)) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    size_t offset = padded & ~(align - 1);
    size_t total;
    if (ckd_add(&total, offset, size)) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    unsigned char *base = (unsigned char *)malloc(total);
    if (base == nullptr) {
        hex_runtime_trap("[Runtime Error] heap allocation failed\n");
    }
    hex_heap_header *header = (hex_heap_header *)base;
    header->allocator = allocator;
    header->size = size;
    header->offset = offset;
    header->live = true;
    *((size_t *)(base + offset - sizeof(size_t))) = offset;
    return base + offset;
}

void hex_heap_free(void *pointer, uintptr_t allocator) {
    if (pointer == nullptr) {
        hex_runtime_trap("[Runtime Error] double deallocation\n");
    }
    size_t offset = *((size_t *)((unsigned char *)pointer - sizeof(size_t)));
    hex_heap_header *header = (hex_heap_header *)((unsigned char *)pointer - offset);
    if (header->allocator != allocator) {
        hex_runtime_trap("[Runtime Error] deallocation used the wrong allocator\n");
    }
    if (!header->live) {
        hex_runtime_trap("[Runtime Error] double deallocation\n");
    }
    header->live = false;
    free(header);
}
