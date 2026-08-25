/* Default allocation: standard C allocation carrying Hexal's exact traps.
   Callers check their own component-specific sums before calling, so the
   checks here are the last backstop rather than the only one. */
#include "hexal/heap.h"

void *hex_heap_allocate(size_t size) {
    void *pointer = malloc(size);
    if (pointer == nullptr) {
        hex_runtime_trap("[Runtime Error] heap allocation failed\n");
    }
    return pointer;
}

/* calloc performs its own overflow check, but reports it by returning null,
   which is indistinguishable from exhaustion. The ckd_mul separates the two so
   each reports its own message. */
void *hex_heap_allocate_zeroed(size_t count, size_t size) {
    size_t total;
    if (ckd_mul(&total, count, size)) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    (void)total;
    void *pointer = calloc(count, size);
    if (pointer == nullptr) {
        hex_runtime_trap("[Runtime Error] heap allocation failed\n");
    }
    return pointer;
}

void hex_heap_free(void *pointer) {
    free(pointer);
}
