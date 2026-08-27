{{- define "poolbody" -}}
{{range .Pools}}
typedef struct {{.CName}} {
    {{.ElementSpelling}} *slots;
    unsigned char *live;
    size_t *free_stack;
    size_t capacity;
    size_t free_count;
} {{.CName}};
static inline {{.CName}} *hex_pool_new_{{.Suffix}}(size_t capacity) {
    if (capacity == 0) {
        hex_runtime_trap("[Runtime Error] pool capacity must be positive\n");
    }
    size_t slot_bytes;
    if (ckd_mul(&slot_bytes, capacity, sizeof({{.ElementSpelling}}))) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    size_t stack_bytes;
    if (ckd_mul(&stack_bytes, capacity, sizeof(size_t))) {
        hex_runtime_trap("[Runtime Error] allocation size is not representable\n");
    }
    {{.CName}} *pool = hex_heap_allocate(sizeof({{.CName}}));
    pool->slots = hex_heap_allocate(slot_bytes);
    pool->live = hex_heap_allocate_zeroed(capacity, sizeof(unsigned char));
    pool->free_stack = hex_heap_allocate(stack_bytes);
    for (size_t index = 0; index < capacity; index++) {
        pool->free_stack[index] = index;
    }
    pool->capacity = capacity;
    pool->free_count = capacity;
    return pool;
}
static inline {{.ElementSpelling}} *hex_pool_alloc_{{.Suffix}}({{.CName}} *pool, {{.ElementSpelling}} initial) {
    if (pool->free_count == 0) {
        hex_runtime_trap("[Runtime Error] pool exhausted\n");
    }
    size_t index = pool->free_stack[--pool->free_count];
    pool->live[index] = 1;
    pool->slots[index] = initial;
    return &pool->slots[index];
}
static inline void hex_pool_free_{{.Suffix}}({{.CName}} *pool, {{.ElementSpelling}} *pointer) {
    uintptr_t base = (uintptr_t)pool->slots;
    uintptr_t address = (uintptr_t)pointer;
    uintptr_t offset = address - base;
    if (address < base || offset % sizeof({{.ElementSpelling}}) != 0 || offset / sizeof({{.ElementSpelling}}) >= pool->capacity) {
        hex_runtime_trap("[Runtime Error] pointer does not name a slot in this pool\n");
    }
    size_t index = offset / sizeof({{.ElementSpelling}});
    if (!pool->live[index]) {
        hex_runtime_trap("[Runtime Error] pool slot is not live\n");
    }
    pool->live[index] = 0;
    pool->free_stack[pool->free_count++] = index;
}
static inline void hex_pool_destroy_{{.Suffix}}({{.CName}} *pool) {
    if (pool->free_count != pool->capacity) {
        hex_runtime_trap("[Runtime Error] pool destroy with live slots\n");
    }
    hex_heap_free(pool->slots);
    hex_heap_free(pool->live);
    hex_heap_free(pool->free_stack);
    hex_heap_free(pool);
}
{{end}}
{{- end -}}
#ifndef HEXAL_POOL_H
#define HEXAL_POOL_H

#include "hexal.h"
#include "hexal/heap.h"
{{template "poolbody" .}}
#endif
