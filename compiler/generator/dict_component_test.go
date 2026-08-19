package generator

import (
	"strings"
	"testing"
)

// A dict-using program emits hexal/dict.h with every reachable
// specialization exactly once, in C-name order, with its guard, its declared
// hexal.h/heap.h/string.h includes, and exactly one trailing newline; the
// owning module header includes the component.
func TestDictComponentEmitsReachableSpecializationsOnce(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"a\", 1)\nend")
	files := generateOne(t, program)
	dictH := files["hexal/dict.h"]
	if dictH == "" {
		t.Fatalf("generated files %v lack hexal/dict.h", files)
	}
	if !strings.HasPrefix(dictH, "#ifndef HEXAL_DICT_H\n#define HEXAL_DICT_H\n\n#include \"hexal.h\"\n#include \"hexal/heap.h\"\n#include \"hexal/string.h\"\n") {
		t.Fatalf("hexal/dict.h lost its guard or one of its declared includes: %q", dictH)
	}
	if !strings.HasSuffix(dictH, "\n#endif\n") {
		t.Fatalf("hexal/dict.h must close its guard with exactly one trailing newline: %q", dictH)
	}
	if count := strings.Count(dictH, "typedef struct hex_dict_entry_"); count != 2 {
		t.Fatalf("hexal/dict.h defines %d entry structs, want 2: %q", count, dictH)
	}
	if !strings.Contains(dictH, "typedef struct hex_dict_entry_Int32_Int32 {") || !strings.Contains(dictH, "typedef struct hex_dict_entry_Strand_Int32 {") {
		t.Fatalf("hexal/dict.h = %q, want Int32 and Strand specializations", dictH)
	}
	if strings.Index(dictH, "hex_dict_entry_Int32_Int32") > strings.Index(dictH, "hex_dict_entry_Strand_Int32") {
		t.Fatalf("hexal/dict.h = %q, specializations must follow C-name order", dictH)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/dict.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/dict.h component include", files["modules/app.h"])
	}
}

// hexal.h owns none of the dict family: a dict-using program leaves hexal.h
// free of hex_dict_ text, and the rendered dict.h matches the previous
// Go-written definitions byte for byte (entry and dict structs, the
// once-per-key-kind hash helpers, every typed inline operation, growth with
// the checked ckd_mul chain and the memset region init, the load-factor
// checked operands, the direct Strand memcmp probes, and trap messages).
func TestDictComponentHexalHeaderOwnsNoDictText(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"a\", 1)\nend")
	files := generateOne(t, program)
	if strings.Contains(files["hexal.h"], "hex_dict_") || strings.Contains(files["hexal.h"], "hex_hash_") {
		t.Fatalf("hexal.h = %q, dict definitions must live in hexal/dict.h", files["hexal.h"])
	}
	want := `#ifndef HEXAL_DICT_H
#define HEXAL_DICT_H

#include "hexal.h"
#include "hexal/heap.h"
#include "hexal/string.h"

typedef struct hex_dict_entry_Int32_Int32 {
    bool active;
    int32_t key;
    int32_t value;
} hex_dict_entry_Int32_Int32;
typedef struct hex_dict_Int32_Int32 {
    hex_dict_entry_Int32_Int32 *buckets;
    size_t length;
    size_t capacity;
    uintptr_t allocator;
} hex_dict_Int32_Int32;

static inline uint64_t hex_hash_Int32(int32_t key) {
    uint64_t x = (size_t)(uint32_t)key + 0x9E3779B97F4A7C15ULL;
    x = (x ^ (x >> 30)) * 0xBF58476D1CE4E5B9ULL;
    x = (x ^ (x >> 27)) * 0x94D049BB133111EBULL;
    return x ^ (x >> 31);
}
static inline uint64_t hex_dict_probe_Int32_Int32_region(hex_dict_entry_Int32_Int32 *region, uint64_t capacity, int32_t key) {
    uint64_t hash = hex_hash_Int32(key);
    size_t index = hash & (capacity - 1);
    while (region[index].active && region[index].key != key) {
        index = (index + 1) & (capacity - 1);
    }
    return index;
}
static inline uint64_t hex_dict_probe_Int32_Int32(const hex_dict_Int32_Int32 *dict, int32_t key) {
    uint64_t hash = hex_hash_Int32(key);
    size_t index = hash & (dict->capacity - 1);
    while (dict->buckets[index].active && dict->buckets[index].key != key) {
        index = (index + 1) & (dict->capacity - 1);
    }
    return index;
}
static inline hex_dict_Int32_Int32 *hex_dict_new_Int32_Int32(hex_heap h) {
    hex_dict_Int32_Int32 *header = hex_heap_raw_allocate(h.identity, sizeof(hex_dict_Int32_Int32), _Alignof(hex_dict_Int32_Int32));
    header->buckets = nullptr;
    header->length = 0;
    header->capacity = 0;
    header->allocator = h.identity;
    return header;
}
static inline void hex_dict_grow_Int32_Int32(hex_dict_Int32_Int32 *dict) {
    size_t next = 8;
    if (dict->capacity != 0) {
        if (ckd_mul(&next, dict->capacity, 2)) {
            hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
        }
    }
    size_t bytes;
    if (ckd_mul(&bytes, next, sizeof(hex_dict_entry_Int32_Int32))) {
        hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
    }
    hex_dict_entry_Int32_Int32 *region = hex_heap_raw_allocate(dict->allocator, bytes, _Alignof(hex_dict_entry_Int32_Int32));
    memset(region, 0, bytes);
    for (size_t index = 0; index < dict->capacity; index++) {
        if (dict->buckets[index].active) {
            uint64_t probe = hex_dict_probe_Int32_Int32_region(region, next, dict->buckets[index].key);
            region[probe] = dict->buckets[index];
        }
    }
    if (dict->buckets != nullptr) {
        hex_heap_free(dict->buckets, dict->allocator);
    }
    dict->buckets = region;
    dict->capacity = next;
}
static inline void hex_dict_insert_Int32_Int32(hex_dict_Int32_Int32 *dict, int32_t key, int32_t value) {
    if (dict->capacity == 0) {
        hex_dict_grow_Int32_Int32(dict);
    } else {
        size_t length_plus_one;
        size_t load_times_10;
        size_t capacity_times_7;
        if (ckd_add(&length_plus_one, dict->length, 1) || ckd_mul(&load_times_10, length_plus_one, 10) || ckd_mul(&capacity_times_7, dict->capacity, 7)) {
            hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
    }
        if (load_times_10 >= capacity_times_7) {
            hex_dict_grow_Int32_Int32(dict);
        }
    }
    size_t index = hex_dict_probe_Int32_Int32(dict, key);
    if (dict->buckets[index].active) {
        dict->buckets[index].value = value;
        return;
    }
    dict->buckets[index].active = true;
    dict->buckets[index].key = key;
    dict->buckets[index].value = value;
    dict->length++;
}
static inline int32_t hex_dict_get_Int32_Int32(const hex_dict_Int32_Int32 *dict, int32_t key) {
    if (dict->capacity == 0) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    size_t index = hex_dict_probe_Int32_Int32(dict, key);
    if (!dict->buckets[index].active) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    return dict->buckets[index].value;
}
static inline bool hex_dict_contains_Int32_Int32(const hex_dict_Int32_Int32 *dict, int32_t key) {
    if (dict->capacity == 0) {
        return false;
    }
    size_t index = hex_dict_probe_Int32_Int32(dict, key);
    return dict->buckets[index].active;
}
static inline int32_t hex_dict_remove_Int32_Int32(hex_dict_Int32_Int32 *dict, int32_t key) {
    if (dict->capacity == 0) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    size_t index = hex_dict_probe_Int32_Int32(dict, key);
    if (!dict->buckets[index].active) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    int32_t value = dict->buckets[index].value;
    dict->buckets[index].active = false;
    dict->length--;
    return value;
}
static inline void hex_dict_free_Int32_Int32(hex_heap h, hex_dict_Int32_Int32 *dict) {
    if (dict == nullptr || dict->allocator != h.identity) {
        hex_runtime_trap("[Runtime Error] deallocation used the wrong allocator\n");
    }
    if (dict->buckets != nullptr) {
        hex_heap_free(dict->buckets, dict->allocator);
    }
    hex_heap_free(dict, h.identity);
}

typedef struct hex_dict_entry_Strand_Int32 {
    bool active;
    hex_strand key;
    int32_t value;
} hex_dict_entry_Strand_Int32;
typedef struct hex_dict_Strand_Int32 {
    hex_dict_entry_Strand_Int32 *buckets;
    size_t length;
    size_t capacity;
    uintptr_t allocator;
} hex_dict_Strand_Int32;

static inline uint64_t hex_hash_Strand(hex_strand key) {
    uint64_t hash = 14695981039346656037ULL;
    for (size_t index = 0; index < 32; index++) {
        hash ^= key.data[index];
        hash *= 1099511628211ULL;
    }
    return hash;
}
static inline uint64_t hex_dict_probe_Strand_Int32_region(hex_dict_entry_Strand_Int32 *region, uint64_t capacity, hex_strand key) {
    uint64_t hash = hex_hash_Strand(key);
    size_t index = hash & (capacity - 1);
    while (region[index].active && memcmp(region[index].key.data, key.data, 32) != 0) {
        index = (index + 1) & (capacity - 1);
    }
    return index;
}
static inline uint64_t hex_dict_probe_Strand_Int32(const hex_dict_Strand_Int32 *dict, hex_strand key) {
    uint64_t hash = hex_hash_Strand(key);
    size_t index = hash & (dict->capacity - 1);
    while (dict->buckets[index].active && memcmp(dict->buckets[index].key.data, key.data, 32) != 0) {
        index = (index + 1) & (dict->capacity - 1);
    }
    return index;
}
static inline hex_dict_Strand_Int32 *hex_dict_new_Strand_Int32(hex_heap h) {
    hex_dict_Strand_Int32 *header = hex_heap_raw_allocate(h.identity, sizeof(hex_dict_Strand_Int32), _Alignof(hex_dict_Strand_Int32));
    header->buckets = nullptr;
    header->length = 0;
    header->capacity = 0;
    header->allocator = h.identity;
    return header;
}
static inline void hex_dict_grow_Strand_Int32(hex_dict_Strand_Int32 *dict) {
    size_t next = 8;
    if (dict->capacity != 0) {
        if (ckd_mul(&next, dict->capacity, 2)) {
            hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
        }
    }
    size_t bytes;
    if (ckd_mul(&bytes, next, sizeof(hex_dict_entry_Strand_Int32))) {
        hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
    }
    hex_dict_entry_Strand_Int32 *region = hex_heap_raw_allocate(dict->allocator, bytes, _Alignof(hex_dict_entry_Strand_Int32));
    memset(region, 0, bytes);
    for (size_t index = 0; index < dict->capacity; index++) {
        if (dict->buckets[index].active) {
            uint64_t probe = hex_dict_probe_Strand_Int32_region(region, next, dict->buckets[index].key);
            region[probe] = dict->buckets[index];
        }
    }
    if (dict->buckets != nullptr) {
        hex_heap_free(dict->buckets, dict->allocator);
    }
    dict->buckets = region;
    dict->capacity = next;
}
static inline void hex_dict_insert_Strand_Int32(hex_dict_Strand_Int32 *dict, hex_strand key, int32_t value) {
    if (dict->capacity == 0) {
        hex_dict_grow_Strand_Int32(dict);
    } else {
        size_t length_plus_one;
        size_t load_times_10;
        size_t capacity_times_7;
        if (ckd_add(&length_plus_one, dict->length, 1) || ckd_mul(&load_times_10, length_plus_one, 10) || ckd_mul(&capacity_times_7, dict->capacity, 7)) {
            hex_runtime_trap("[Runtime Error] dictionary capacity is not representable\n");
    }
        if (load_times_10 >= capacity_times_7) {
            hex_dict_grow_Strand_Int32(dict);
        }
    }
    size_t index = hex_dict_probe_Strand_Int32(dict, key);
    if (dict->buckets[index].active) {
        dict->buckets[index].value = value;
        return;
    }
    dict->buckets[index].active = true;
    dict->buckets[index].key = key;
    dict->buckets[index].value = value;
    dict->length++;
}
static inline int32_t hex_dict_get_Strand_Int32(const hex_dict_Strand_Int32 *dict, hex_strand key) {
    if (dict->capacity == 0) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    size_t index = hex_dict_probe_Strand_Int32(dict, key);
    if (!dict->buckets[index].active) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    return dict->buckets[index].value;
}
static inline bool hex_dict_contains_Strand_Int32(const hex_dict_Strand_Int32 *dict, hex_strand key) {
    if (dict->capacity == 0) {
        return false;
    }
    size_t index = hex_dict_probe_Strand_Int32(dict, key);
    return dict->buckets[index].active;
}
static inline int32_t hex_dict_remove_Strand_Int32(hex_dict_Strand_Int32 *dict, hex_strand key) {
    if (dict->capacity == 0) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    size_t index = hex_dict_probe_Strand_Int32(dict, key);
    if (!dict->buckets[index].active) {
        hex_runtime_trap("[Runtime Error] dictionary key not found\n");
    }
    int32_t value = dict->buckets[index].value;
    dict->buckets[index].active = false;
    dict->length--;
    return value;
}
static inline void hex_dict_free_Strand_Int32(hex_heap h, hex_dict_Strand_Int32 *dict) {
    if (dict == nullptr || dict->allocator != h.identity) {
        hex_runtime_trap("[Runtime Error] deallocation used the wrong allocator\n");
    }
    if (dict->buckets != nullptr) {
        hex_heap_free(dict->buckets, dict->allocator);
    }
    hex_heap_free(dict, h.identity);
}

#endif
`
	if got := files["hexal/dict.h"]; got != want {
		t.Fatalf("hexal/dict.h = %q, want %q", got, want)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/dict.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/dict.h component include", files["modules/app.h"])
	}
}

// A Dict whose value is a module-owned object emits its entry struct and
// specialization into the consuming module's header, after the value type's
// typedef. The program-wide component cannot spell a per-module type.
func TestDictModuleObjectValueSpecializationLivesInModuleHeader(t *testing.T) {
	program := checkedGeneratorSource(t, "type Point = { x: Int32, }\nfun demo(h: Heap) do\n    points: Dict<Int32, Point> = Dict<Int32, Point>.new(h)\n    defer points.free(h)\n    points.insert(1, Point { x = 1 })\nend")
	files := generateOne(t, program)
	if got := files["hexal/dict.h"]; got != "" {
		t.Fatalf("hexal/dict.h = %q, want no component artifact: its only specialization is module-typed", got)
	}
	header := files["modules/app.h"]
	entry := strings.Index(header, "typedef struct hex_dict_entry_Int32_Point {")
	if entry < 0 || !strings.Contains(header, "hex_t_m3_app_Point value;") {
		t.Fatalf("modules/app.h = %q, want the entry struct spelling the module object by value", header)
	}
	element := strings.Index(header, "struct hex_t_m3_app_Point {")
	if element < 0 || element > entry {
		t.Fatalf("modules/app.h declares the entry struct at %d before its value type at %d; the value type must precede it", entry, element)
	}
}

// A program without reachable Dict types emits no dict artifact and no
// module includes it.
func TestDictComponentAbsentWithoutReachableDicts(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo() do\n    value: Int32 = 1\nend")
	files := generateOne(t, program)
	if _, exists := files["hexal/dict.h"]; exists {
		t.Fatalf("scalar-only program emitted hexal/dict.h")
	}
	if strings.Contains(files["modules/app.h"], "hexal/dict.h") {
		t.Fatalf("modules/app.h = %q, must not include an unselected component", files["modules/app.h"])
	}
}

// Equivalent compilations render identical dict.h bytes.
