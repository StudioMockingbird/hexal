{{- define "listbody" -}}
{{range .Lists}}
typedef struct {{.CName}} {
    {{.ElementSpelling}} *data;
    size_t length;
    size_t capacity;
    uintptr_t allocator;
} {{.CName}};
static inline void hex_list_grow_{{.Suffix}}({{.CName}} *list) {
    size_t next = 1;
    if (list->capacity != 0) {
        if (ckd_mul(&next, list->capacity, 2)) {
            hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
        }
    }
    size_t bytes;
    if (ckd_mul(&bytes, next, sizeof({{.ElementSpelling}}))) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    {{.ElementSpelling}} *region = hex_heap_raw_allocate(list->allocator, bytes, _Alignof({{.ElementSpelling}}));
    if (list->length != 0) {
        memcpy(region, list->data, list->length * sizeof({{.ElementSpelling}}));
    }
    if (list->data != nullptr) {
        hex_heap_free(list->data, list->allocator);
    }
    list->data = region;
    list->capacity = next;
}
static inline void hex_list_reserve_at_least_{{.Suffix}}({{.CName}} *list, size_t minimum) {
    // The stream backends' internal reservation: one allocation grown by the
    // same checked doubling formula and allocator path as push growth, never
    // exposed as a source-visible method.
    if (minimum == 0 || minimum <= list->capacity) {
        return;
    }
    size_t next = list->capacity == 0 ? 1 : list->capacity;
    while (next < minimum) {
        if (ckd_mul(&next, next, 2)) {
            hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
        }
    }
    size_t bytes;
    if (ckd_mul(&bytes, next, sizeof({{.ElementSpelling}}))) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    {{.ElementSpelling}} *region = hex_heap_raw_allocate(list->allocator, bytes, _Alignof({{.ElementSpelling}}));
    if (list->length != 0) {
        memcpy(region, list->data, list->length * sizeof({{.ElementSpelling}}));
    }
    if (list->data != nullptr) {
        hex_heap_free(list->data, list->allocator);
    }
    list->data = region;
    list->capacity = next;
}
static inline {{.CName}} *hex_list_new_{{.Suffix}}(hex_heap h) {
    {{.CName}} *header = hex_heap_raw_allocate(h.identity, sizeof({{.CName}}), _Alignof({{.CName}}));
    header->data = nullptr;
    header->length = 0;
    header->capacity = 0;
    header->allocator = h.identity;
    return header;
}
static inline void hex_list_push_{{.Suffix}}({{.CName}} *list, {{.ElementSpelling}} value) {
    if (list->length == list->capacity) {
        hex_list_grow_{{.Suffix}}(list);
    }
    list->data[list->length++] = value;
}
static inline {{.ElementSpelling}} hex_list_pop_{{.Suffix}}({{.CName}} *list) {
    if (list->length == 0) {
        hex_runtime_trap("[Runtime Error] list index out of bounds\n");
    }
    {{.ElementSpelling}} value = list->data[list->length - 1];
    list->length--;
    return value;
}
static inline void hex_list_clear_{{.Suffix}}({{.CName}} *list) {
    list->length = 0;
}
static inline {{.AtReadReturn}} hex_list_at_{{.Suffix}}(const {{.CName}} *list, size_t index) {
    if (index >= list->length) {
        hex_runtime_trap("[Runtime Error] list index out of bounds\n");
    }
    return &list->data[index];
}
static inline {{.ElementSpelling}} *hex_list_at_mut_{{.Suffix}}({{.CName}} *list, size_t index) {
    if (index >= list->length) {
        hex_runtime_trap("[Runtime Error] list index out of bounds\n");
    }
    return &list->data[index];
}
static inline void hex_list_free_{{.Suffix}}(hex_heap h, {{.CName}} *list) {
    if (list == nullptr || list->allocator != h.identity) {
        hex_runtime_trap("[Runtime Error] deallocation used the wrong allocator\n");
    }
    if (list->data != nullptr) {
        hex_heap_free(list->data, list->allocator);
    }
    hex_heap_free(list, h.identity);
}
{{if .ViewCName}}static inline {{.ViewCName}} hex_list_slice_{{.Suffix}}(const {{.CName}} *list, uint64_t start, uint64_t end) {
    if (!(start <= end && end <= list->length)) {
        hex_runtime_trap("[Runtime Error] list slice bounds out of range\n");
    }
    return ({{.ViewCName}}){list->data == nullptr ? nullptr : &list->data[start], end - start};
}
{{end}}{{end}}
{{- end -}}
#ifndef HEXAL_LIST_H
#define HEXAL_LIST_H

#include "hexal.h"
#include "hexal/heap.h"
{{if .NeedsView}}#include "hexal/view.h"
{{end}}{{template "listbody" .}}
#endif
