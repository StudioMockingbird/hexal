package generator

import (
	"strings"
	"testing"
)

// A list-using program emits hexal/list.h with every reachable
// specialization exactly once, in C-name order, with its guard, its declared
// hexal.h/heap.h/view.h includes, and exactly one trailing newline; the
// owning module header includes the component.
func TestListComponentEmitsReachableSpecializationsOnce(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    numbers: List<Int32> = List<Int32>.new(h)\n    defer numbers.free(h)\n    names: List<String> = List<String>.new(h)\n    defer names.free(h)\nend")
	files := generateOne(t, program)
	listH := files["hexal/list.h"]
	if listH == "" {
		t.Fatalf("generated files %v lack hexal/list.h", files)
	}
	if !strings.HasPrefix(listH, "#ifndef HEXAL_LIST_H\n#define HEXAL_LIST_H\n\n#include \"hexal.h\"\n#include \"hexal/heap.h\"\n#include \"hexal/view.h\"\n") {
		t.Fatalf("hexal/list.h lost its guard or one of its declared includes: %q", listH)
	}
	if !strings.HasSuffix(listH, "\n#endif\n") {
		t.Fatalf("hexal/list.h must close its guard with exactly one trailing newline: %q", listH)
	}
	if count := strings.Count(listH, "typedef struct hex_list_"); count != 2 {
		t.Fatalf("hexal/list.h defines %d specializations, want 2: %q", count, listH)
	}
	if !strings.Contains(listH, "typedef struct hex_list_Int32 {") || !strings.Contains(listH, "typedef struct hex_list_String {") {
		t.Fatalf("hexal/list.h = %q, want Int32 and String specializations", listH)
	}
	if strings.Index(listH, "typedef struct hex_list_Int32") > strings.Index(listH, "typedef struct hex_list_String") {
		t.Fatalf("hexal/list.h = %q, specializations must follow C-name order", listH)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/list.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/list.h component include", files["modules/app.h"])
	}
}

// hexal.h owns none of the list family: a list-using program leaves hexal.h
// free of hex_list_ text, and the rendered list.h matches the previous
// Go-written definitions byte for byte (struct, every typed inline operation,
// bounds guards, growth with the checked multiply chain, the guarded memcpy,
// and trap messages).
func TestListComponentHexalHeaderOwnsNoListText(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    view: View<Int32> = values.slice(0, 1)\n    first: Int32 = values[0]\n    values.set(0, 9)\n    last: Int32 = values.pop()\n    values.clear()\nend")
	files := generateOne(t, program)
	if strings.Contains(files["hexal.h"], "hex_list_") {
		t.Fatalf("hexal.h = %q, list definitions must live in hexal/list.h", files["hexal.h"])
	}
	want := `#ifndef HEXAL_LIST_H
#define HEXAL_LIST_H

#include "hexal.h"
#include "hexal/heap.h"
#include "hexal/view.h"

typedef struct hex_list_Int32 {
    int32_t *data;
    size_t length;
    size_t capacity;
    uintptr_t allocator;
} hex_list_Int32;
static inline void hex_list_grow_Int32(hex_list_Int32 *list) {
    size_t next = 1;
    if (list->capacity != 0) {
        if (ckd_mul(&next, list->capacity, 2)) {
            hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
        }
    }
    size_t bytes;
    if (ckd_mul(&bytes, next, sizeof(int32_t))) {
        hex_runtime_trap("[Runtime Error] list capacity is not representable\n");
    }
    int32_t *region = hex_heap_raw_allocate(list->allocator, bytes, _Alignof(int32_t));
    if (list->length != 0) {
        memcpy(region, list->data, list->length * sizeof(int32_t));
    }
    if (list->data != nullptr) {
        hex_heap_free(list->data, list->allocator);
    }
    list->data = region;
    list->capacity = next;
}
static inline hex_list_Int32 *hex_list_new_Int32(hex_heap h) {
    hex_list_Int32 *header = hex_heap_raw_allocate(h.identity, sizeof(hex_list_Int32), _Alignof(hex_list_Int32));
    header->data = nullptr;
    header->length = 0;
    header->capacity = 0;
    header->allocator = h.identity;
    return header;
}
static inline void hex_list_push_Int32(hex_list_Int32 *list, int32_t value) {
    if (list->length == list->capacity) {
        hex_list_grow_Int32(list);
    }
    list->data[list->length++] = value;
}
static inline void hex_list_set_Int32(hex_list_Int32 *list, size_t index, int32_t value) {
    if (index >= list->length) {
        hex_runtime_trap("[Runtime Error] list index out of bounds\n");
    }
    list->data[index] = value;
}
static inline int32_t hex_list_pop_Int32(hex_list_Int32 *list) {
    if (list->length == 0) {
        hex_runtime_trap("[Runtime Error] list index out of bounds\n");
    }
    int32_t value = list->data[list->length - 1];
    list->length--;
    return value;
}
static inline void hex_list_clear_Int32(hex_list_Int32 *list) {
    list->length = 0;
}
static inline const int32_t * hex_list_at_Int32(const hex_list_Int32 *list, size_t index) {
    if (index >= list->length) {
        hex_runtime_trap("[Runtime Error] list index out of bounds\n");
    }
    return &list->data[index];
}
static inline int32_t *hex_list_at_mut_Int32(hex_list_Int32 *list, size_t index) {
    if (index >= list->length) {
        hex_runtime_trap("[Runtime Error] list index out of bounds\n");
    }
    return &list->data[index];
}
static inline void hex_list_free_Int32(hex_heap h, hex_list_Int32 *list) {
    if (list == nullptr || list->allocator != h.identity) {
        hex_runtime_trap("[Runtime Error] deallocation used the wrong allocator\n");
    }
    if (list->data != nullptr) {
        hex_heap_free(list->data, list->allocator);
    }
    hex_heap_free(list, h.identity);
}
static inline hex_view_Int32 hex_list_slice_Int32(const hex_list_Int32 *list, uint64_t start, uint64_t end) {
    if (!(start <= end && end <= list->length)) {
        hex_runtime_trap("[Runtime Error] list slice bounds out of range\n");
    }
    return (hex_view_Int32){list->data == nullptr ? nullptr : &list->data[start], end - start};
}

#endif
`
	if got := files["hexal/list.h"]; got != want {
		t.Fatalf("hexal/list.h = %q, want %q", got, want)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/list.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/list.h component include", files["modules/app.h"])
	}
}

// A program without reachable List types emits no list artifact and no
// module includes it.
// Equivalent compilations render identical list.h bytes.
// A specialization whose element is a module-owned object is emitted into the
// consuming module's header, after that element's typedef, because the
// program-wide component cannot declare a per-module type.
func TestListModuleOwnedElementSpecializationLivesInModuleHeader(t *testing.T) {
	program := checkedGeneratorSource(t, "type Point = { x: Int32, }\nfun demo(h: Heap) do\n    points: List<Point> = List<Point>.new(h)\n    defer points.free(h)\n    points.push(Point { x = 1, })\nend")
	files := generateOne(t, program)
	if got := files["hexal/list.h"]; got != "" {
		t.Fatalf("hexal/list.h = %q, want no component artifact: its only specialization is module-typed", got)
	}
	header := files["modules/app.h"]
	specialization := strings.Index(header, "typedef struct hex_list_Point {")
	if specialization < 0 {
		t.Fatalf("modules/app.h = %q, want the Point specialization", header)
	}
	element := strings.Index(header, "struct hex_t_m3_app_Point {")
	if element < 0 || element > specialization {
		t.Fatalf("modules/app.h declares hex_list_Point at %d before its element type at %d; the element must precede it", specialization, element)
	}
}
