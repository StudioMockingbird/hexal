#ifndef HEXAL_DICT_H
#define HEXAL_DICT_H

#include "hexal.h"
#include "hexal/heap.h"
#include "hexal/string.h"
{{range .Dicts}}
typedef struct {{.EntryName}} {
    bool active;
    {{.KeySpelling}} key;
    {{.ValueSpelling}} value;
} {{.EntryName}};
typedef struct {{.CName}} {
    {{.EntryName}} *buckets;
    size_t length;
    size_t capacity;
    uintptr_t allocator;
} {{.CName}};
{{if .EmitHash}}{{if .StrandKey}}
static inline uint64_t hex_hash_Strand(hex_strand key) {
    uint64_t hash = 14695981039346656037ULL;
    for (size_t index = 0; index < 32; index++) {
        hash ^= key.data[index];
        hash *= 1099511628211ULL;
    }
    return hash;
}
{{else}}
static inline uint64_t hex_hash_Int32(int32_t key) {
    uint64_t x = (size_t)(uint32_t)key + 0x9E3779B97F4A7C15ULL;
    x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9ULL;
    x = (x ^ (x >> 27)) * 0x94D049BB133111EBULL;
    return x ^ (x >> 31);
}
{{end}}{{end}}static inline uint64_t hex_dict_probe_{{.Suffix}}_region({{.EntryName}} *region, uint64_t capacity, {{.KeySpelling}} key) {
    uint64_t hash = {{.HashHelper}}(key);
    size_t index = hash & (capacity - 1);
    while (region[index].active && {{if .StrandKey}}memcmp(region[index].key.data, key.data, 32) != 0{{else}}region[index].key != key{{end}}) {
        index = (index + 1) & (capacity - 1);
    }
    return index;
}
static inline uint64_t hex_dict_probe_{{.Suffix}}(const {{.CName}} *dict, {{.KeySpelling}} key) {
    uint64_t hash = {{.HashHelper}}(key);
    size_t index = hash & (dict->capacity - 1);
    while (dict->buckets[index].active && {{if .StrandKey}}memcmp(dict->buckets[index].key.data, key.data, 32) != 0{{else}}dict->buckets[index].key != key{{end}}) {
        index = (index + 1) & (dict->capacity - 1);
    }
    return index;
}
static inline {{.CName}} *hex_dict_new_{{.Suffix}}(hex_heap h) {
    {{.CName}} *header = hex_heap_raw_allocate(h.identity, sizeof({{.CName}}), _Alignof({{.CName}}));
    header->buckets = nullptr;
    header->length = 0;
    header->capacity = 0;
    header->allocator = h.identity;
    return header;
}
static inline void hex_dict_grow_{{.Suffix}}({{.CName}} *dict) {
    size_t next = 8;
    if (dict->capacity != 0) {
        if (ckd_mul(&next, dict->capacity, 2)) {
            hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
        }
    }
    size_t bytes;
    if (ckd_mul(&bytes, next, sizeof({{.EntryName}}))) {
        hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
    }
    {{.EntryName}} *region = hex_heap_raw_allocate(dict->allocator, bytes, _Alignof({{.EntryName}}));
    memset(region, 0, bytes);
    for (size_t index = 0; index < dict->capacity; index++) {
        if (dict->buckets[index].active) {
            uint64_t probe = hex_dict_probe_{{.Suffix}}_region(region, next, dict->buckets[index].key);
            region[probe] = dict->buckets[index];
        }
    }
    if (dict->buckets != nullptr) {
        hex_heap_free(dict->buckets, dict->allocator);
    }
    dict->buckets = region;
    dict->capacity = next;
}
static inline void hex_dict_insert_{{.Suffix}}({{.CName}} *dict, {{.KeySpelling}} key, {{.ValueSpelling}} value) {
    if (dict->capacity == 0) {
        hex_dict_grow_{{.Suffix}}(dict);
    } else {
        size_t length_plus_one;
        size_t load_times_10;
        size_t capacity_times_7;
        if (ckd_add(&length_plus_one, dict->length, 1) || ckd_mul(&load_times_10, length_plus_one, 10) || ckd_mul(&capacity_times_7, dict->capacity, 7)) {
            hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
    }
        if (load_times_10 >= capacity_times_7) {
            hex_dict_grow_{{.Suffix}}(dict);
        }
    }
    size_t index = hex_dict_probe_{{.Suffix}}(dict, key);
    if (dict->buckets[index].active) {
        dict->buckets[index].value = value;
        return;
    }
    dict->buckets[index].active = true;
    dict->buckets[index].key = key;
    dict->buckets[index].value = value;
    dict->length++;
}
static inline {{.ValueSpelling}} hex_dict_get_{{.Suffix}}(const {{.CName}} *dict, {{.KeySpelling}} key) {
    if (dict->capacity == 0) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    size_t index = hex_dict_probe_{{.Suffix}}(dict, key);
    if (!dict->buckets[index].active) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    return dict->buckets[index].value;
}
static inline bool hex_dict_contains_{{.Suffix}}(const {{.CName}} *dict, {{.KeySpelling}} key) {
    if (dict->capacity == 0) {
        return false;
    }
    size_t index = hex_dict_probe_{{.Suffix}}(dict, key);
    return dict->buckets[index].active;
}
static inline {{.ValueSpelling}} hex_dict_remove_{{.Suffix}}({{.CName}} *dict, {{.KeySpelling}} key) {
    if (dict->capacity == 0) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    size_t index = hex_dict_probe_{{.Suffix}}(dict, key);
    if (!dict->buckets[index].active) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    {{.ValueSpelling}} value = dict->buckets[index].value;
    dict->buckets[index].active = false;
    dict->length--;
    return value;
}
static inline void hex_dict_free_{{.Suffix}}(hex_heap h, {{.CName}} *dict) {
    if (dict == nullptr || dict->allocator != h.identity) {
        hex_runtime_trap("[Runtime Error] deallocation used the wrong allocator\n");
    }
    if (dict->buckets != nullptr) {
        hex_heap_free(dict->buckets, dict->allocator);
    }
    hex_heap_free(dict, h.identity);
}
{{end}}
#endif
