package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
)

func TestGenerateHeapAllocationAndFree(t *testing.T) {
	program := checkedGeneratorSource(t, "h: Heap := Heap.new() p: MutPtr<Int32> := h.allocate<Int32>(0) defer h.free(p)")
	files := generateOne(t, program)
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	heapH, heapC := files["hexal/heap.h"], files["hexal/heap.c"]
	if heapH == "" || heapC == "" {
		t.Fatalf("heap program emitted no component pair: %v", files)
	}
	// The token and the three operation declarations own hexal/heap.h; the
	// typed helper stays in the module header.
	for _, want := range []string{"typedef unsigned char hex_heap;", "void *hex_heap_allocate(size_t size);", "void *hex_heap_allocate_zeroed(size_t count, size_t size);", "void hex_heap_free(void *pointer);"} {
		if !strings.Contains(heapH, want) {
			t.Fatalf("hexal/heap.h does not contain %q: %q", want, heapH)
		}
	}
	// No allocator identity, header, offset marker, or live flag remains.
	for _, forbidden := range []string{"struct hex_heap", "HEX_HEAP_DEFAULT", "hex_heap_header", "hex_heap_raw_allocate", "uintptr_t", "live"} {
		if strings.Contains(heapH, forbidden) {
			t.Fatalf("hexal/heap.h retains %q: %q", forbidden, heapH)
		}
	}
	if !strings.Contains(rootH, "hex_heap_allocate_Int32") {
		t.Fatalf("modules/app.h = %q, want the typed allocation helper", rootH)
	}
	if !strings.Contains(rootH, "#include \"hexal/heap.h\"") {
		t.Fatalf("modules/app.h = %q, want the component include", rootH)
	}
	if strings.Contains(heapH, "hex_heap_allocate_Int32") || strings.Contains(heapC, "hex_heap_allocate_Int32") {
		t.Fatalf("component pair = H:%q C:%q, typed helpers must stay module-owned", heapH, heapC)
	}
	if !strings.Contains(heapC, "#include \"hexal/heap.h\"") {
		t.Fatalf("hexal/heap.c = %q, want its matching header first", heapC)
	}
	for _, definition := range []string{
		"void *hex_heap_allocate(size_t size) {",
		"void *hex_heap_allocate_zeroed(size_t count, size_t size) {",
		"void hex_heap_free(void *pointer) {",
	} {
		if strings.Count(heapC, definition) != 1 {
			t.Fatalf("hexal/heap.c = %q, want exactly one %q", heapC, definition)
		}
	}
	// hexal.h owns none of the migrated heap machinery.
	for _, forbidden := range []string{"hex_heap", "HEX_HEAP_DEFAULT"} {
		if strings.Contains(files["hexal.h"], forbidden) {
			t.Fatalf("hexal.h retains %q: %q", forbidden, files["hexal.h"])
		}
	}
	if strings.Contains(rootC, "free(") && !strings.Contains(rootC, "hex_heap_free(") {
		t.Fatalf("generated C = %q, want only checked deallocation", rootC)
	}
	assertHeapOperationsCheckedAndTrapping(t, heapC)
}

// assertHeapOperationsCheckedAndTrapping verifies that the two allocating
// operations trap on a null result, that only the zeroing one separates an
// unrepresentable product from exhaustion with ckd_mul, and that release adds
// no check of its own. The non-zeroing operation must not zero: its callers
// write every byte they read.
func assertHeapOperationsCheckedAndTrapping(t *testing.T, heapC string) {
	t.Helper()
	plain := heapOperationBody(t, heapC, "void *hex_heap_allocate(size_t size) {")
	zeroed := heapOperationBody(t, heapC, "void *hex_heap_allocate_zeroed(size_t count, size_t size) {")
	release := heapOperationBody(t, heapC, "void hex_heap_free(void *pointer) {")

	if !strings.Contains(plain, "malloc(size)") || strings.Contains(plain, "calloc") || strings.Contains(plain, "memset") {
		t.Fatalf("hex_heap_allocate = %q, want an unzeroed malloc", plain)
	}
	if strings.Contains(plain, "ckd_") {
		t.Fatalf("hex_heap_allocate = %q, want no size arithmetic on a single size", plain)
	}
	if !strings.Contains(zeroed, "ckd_mul(&total, count, size)") {
		t.Fatalf("hex_heap_allocate_zeroed = %q, want the checked product", zeroed)
	}
	if !strings.Contains(zeroed, "calloc(count, size)") {
		t.Fatalf("hex_heap_allocate_zeroed = %q, want calloc", zeroed)
	}
	if strings.Index(zeroed, "ckd_mul") > strings.Index(zeroed, "calloc") {
		t.Fatalf("hex_heap_allocate_zeroed = %q, want the overflow check before calloc", zeroed)
	}
	for name, body := range map[string]string{"hex_heap_allocate": plain, "hex_heap_allocate_zeroed": zeroed} {
		if !strings.Contains(body, "[Runtime Error] heap allocation failed") {
			t.Fatalf("%s = %q, want the allocation-failure trap", name, body)
		}
	}
	if !strings.Contains(zeroed, "[Runtime Error] allocation size is not representable") {
		t.Fatalf("hex_heap_allocate_zeroed = %q, want the size trap distinct from failure", zeroed)
	}
	// Release owns no null, identity, liveness, or ownership check.
	if strings.Count(release, "if") != 0 || !strings.Contains(release, "free(pointer)") {
		t.Fatalf("hex_heap_free = %q, want an unconditional free", release)
	}
	// Nothing recovers a header, offset, or allocator from an allocation.
	for _, bad := range []string{"hex_heap_header", "offset", "->allocator", "->live", "uintptr_t allocator"} {
		if strings.Contains(heapC, bad) {
			t.Fatalf("hexal/heap.c retains %q: %q", bad, heapC)
		}
	}
}

// heapOperationBody returns the text of one hexal/heap.c definition, from its
// signature to the closing brace in column zero.
func heapOperationBody(t *testing.T, heapC, signature string) string {
	t.Helper()
	start := strings.Index(heapC, signature)
	if start < 0 {
		t.Fatalf("hexal/heap.c = %q, want the definition %q", heapC, signature)
	}
	rest := heapC[start:]
	end := strings.Index(rest, "\n}")
	if end < 0 {
		t.Fatalf("hexal/heap.c definition %q is unterminated: %q", signature, rest)
	}
	return rest[:end]
}

// A bare Heap handle selects the raw machinery without any typed allocation;
// the module header still needs the representation for its initializer.
func TestGenerateHeapHandleSelectsComponentPair(t *testing.T) {
	program := checkedGeneratorSource(t, "h: Heap := Heap.new()\n")
	files := generateOne(t, program)
	if _, exists := files["hexal/heap.h"]; !exists {
		t.Fatalf("Heap handle program emitted no hexal/heap.h: %v", files)
	}
	if _, exists := files["hexal/heap.c"]; !exists {
		t.Fatalf("Heap handle program emitted no hexal/heap.c: %v", files)
	}
	if !strings.Contains(files["modules/app.h"], "#include \"hexal/heap.h\"") {
		t.Fatalf("modules/app.h = %q, want the component include", files["modules/app.h"])
	}
}

// A program that never touches Heap emits no heap component artifact.
func TestGenerateScalarOnlyEmitsNoHeapArtifacts(t *testing.T) {
	program := checkedGeneratorSource(t, "x: Int32 := 1\n")
	files := generateOne(t, program)
	if _, exists := files["hexal/heap.h"]; exists {
		t.Fatalf("scalar-only program emitted hexal/heap.h: %v", files)
	}
	if _, exists := files["hexal/heap.c"]; exists {
		t.Fatalf("scalar-only program emitted hexal/heap.c: %v", files)
	}
}

func TestGenerateDeferReverseOrderAndCapture(t *testing.T) {
	program := checkedGeneratorSource(t, "fun record(value: Int32) do end mut first: Int32 := 1 mut second: Int32 := 2 defer record(first) defer record(second)")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_v_first") || !strings.Contains(rootC, "hex_v_second") {
		t.Fatalf("generated C = %q, want captured arguments", rootC)
	}
	if !strings.Contains(rootC, "record(hex_defer_capture_2)") || !strings.Contains(rootC, "record(hex_defer_capture_1)") {
		t.Fatalf("generated C = %q, want reverse-order deferred calls", rootC)
	}
}

func TestGenerateDeferRoutesBreakAndReturn(t *testing.T) {
	program := checkedGeneratorSource(t, "fun record(value: Int32) do end fun run(): Int32 do\nmut flag: Bool := true\nwhile flag do\n    defer record(1)\n    break\nend\nreturn 0\nend")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program}, Config{})
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_f_m3_app_record(hex_defer_capture_1);\n        break;") {
		t.Fatalf("generated C = %q, want deferred call on the break path", rootC)
	}
}

// The List and Dict growth helpers keep every capacity temporary in size_t
// and lower doubling, element-region byte sizing, and the Dict load-factor
// decision through checked arithmetic; List relocation uses one guarded
// memcpy, a fresh Dict bucket region zeroes with one memset, Strand key
// probes compare the canonical 32-byte representation with memcmp, every
// diagnostic reports through hex_runtime_trap, and no compiler-owned NULL or
// raw fputs remains.
func TestGenerateListAndDictCheckedGrowth(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    scores: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    labels: Dict<Strand, Int32> := Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"a\", 1)\nend")
	files := generateOne(t, program)
	listH := files["hexal/list.h"]
	dictH := files["hexal/dict.h"]
	for _, want := range []string{
		"size_t next = 1;",
		"ckd_mul(&next, list->capacity, 2)",
		"ckd_mul(&bytes, next, sizeof(int32_t))",
		"if (list->length != 0) {\n        memcpy(region, list->data, list->length * sizeof(int32_t));",
		"hex_runtime_trap(\"[Runtime Error] list capacity is not representable\\n\")",
	} {
		if !strings.Contains(listH, want) {
			t.Fatalf("hexal/list.h does not contain %q:\n%s", want, listH)
		}
	}
	for _, want := range []string{
		"size_t next = 8;",
		"ckd_mul(&next, dict->capacity, 2)",
		"ckd_mul(&bytes, next, sizeof(hex_dict_entry_Int32_Int32))",
		"hex_heap_allocate_zeroed(1, bytes);",
		"ckd_add(&length_plus_one, dict->length, 1)",
		"ckd_mul(&load_times_10, length_plus_one, 10)",
		"ckd_mul(&capacity_times_7, dict->capacity, 7)",
		"memcmp(region[index].key.data, key.data, 32) != 0",
		"memcmp(dict->buckets[index].key.data, key.data, 32) != 0",
		"hex_runtime_trap(\"[Runtime Error] dictionary capacity is not representable\\n\")",
	} {
		if !strings.Contains(dictH, want) {
			t.Fatalf("hexal/dict.h does not contain %q:\n%s", want, dictH)
		}
	}
	for _, forbid := range []string{
		"uint64_t next", "SIZE_MAX /", "(dict->length + 1) * 10 >= dict->capacity * 7",
		"hex_dict_key_equal_", "fputs(", "NULL", "region[index].active = false",
		// The zeroed allocation replaces the clear; no allocator identity,
		// header, or post-allocation memset survives in either family.
		"memset(region", "hex_heap_raw_allocate", "->allocator", "h.identity",
	} {
		if strings.Contains(listH, forbid) || strings.Contains(dictH, forbid) {
			t.Fatalf("component header retains %q:\n%s\n%s", forbid, listH, dictH)
		}
	}
}
