package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
)

func TestGenerateHeapAllocationAndFree(t *testing.T) {
	program := checkedGeneratorSource(t, "h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) defer h.free(p)")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program})
	if err != nil {
		t.Fatal(err)
	}
	rootC, rootH := files["modules/app.c"], files["modules/app.h"]
	heapH, heapC := files["hexal/heap.h"], files["hexal/heap.c"]
	if heapH == "" || heapC == "" {
		t.Fatalf("heap program emitted no component pair: %v", files)
	}
	// The representation, default initializer, and raw declarations own
	// hexal/heap.h; the typed helper stays in the module header.
	for _, want := range []string{"typedef struct hex_heap", "HEX_HEAP_DEFAULT", "hex_heap_header", "hex_heap_raw_allocate", "hex_heap_free"} {
		if !strings.Contains(heapH, want) {
			t.Fatalf("hexal/heap.h does not contain %q: %q", want, heapH)
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
		"void *hex_heap_raw_allocate(uintptr_t allocator, size_t size, size_t align) {",
		"void hex_heap_free(void *pointer, uintptr_t allocator) {",
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
	assertRawAllocateCheckedArithmetic(t, heapC)
}

// assertRawAllocateCheckedArithmetic verifies the hexal/heap.c definition of
// hex_heap_raw_allocate: both size additions go through ckd_add with size_t
// destinations, zero alignment is rejected before align - 1 is evaluated, and
// no wrap-detection pattern superseded by the checked arithmetic remains.
func assertRawAllocateCheckedArithmetic(t *testing.T, heapC string) {
	t.Helper()
	start := strings.Index(heapC, "hex_heap_raw_allocate")
	end := strings.Index(heapC, "void hex_heap_free")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("hexal/heap.c = %q, want hex_heap_raw_allocate before hex_heap_free", heapC)
	}
	raw := heapC[start:end]
	if got := strings.Count(raw, "ckd_add"); got != 2 {
		t.Fatalf("hex_heap_raw_allocate uses ckd_add %d times, want 2: %q", got, raw)
	}
	for _, want := range []string{
		"align == 0",
		"ckd_add(&padded, sizeof(hex_heap_header), align - 1)",
		"size_t total;",
		"ckd_add(&total, offset, size)",
	} {
		if !strings.Contains(raw, want) {
			t.Fatalf("hex_heap_raw_allocate does not contain %q: %q", want, raw)
		}
	}
	for _, bad := range []string{"total < size", "(align & (align - 1))"} {
		if strings.Contains(raw, bad) {
			t.Fatalf("hex_heap_raw_allocate retains obsolete pattern %q: %q", bad, raw)
		}
	}
}

// A bare Heap handle selects the raw machinery without any typed allocation;
// the module header still needs the representation for its initializer.
func TestGenerateHeapHandleSelectsComponentPair(t *testing.T) {
	program := checkedGeneratorSource(t, "h: Heap = Heap.new()\n")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program})
	if err != nil {
		t.Fatal(err)
	}
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
	program := checkedGeneratorSource(t, "x: Int32 = 1\n")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program})
	if err != nil {
		t.Fatal(err)
	}
	if _, exists := files["hexal/heap.h"]; exists {
		t.Fatalf("scalar-only program emitted hexal/heap.h: %v", files)
	}
	if _, exists := files["hexal/heap.c"]; exists {
		t.Fatalf("scalar-only program emitted hexal/heap.c: %v", files)
	}
}

func TestGenerateDeferReverseOrderAndCapture(t *testing.T) {
	program := checkedGeneratorSource(t, "fun record(value: Int32) do end mut first: Int32 = 1 mut second: Int32 = 2 defer record(first) defer record(second)")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program})
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
	program := checkedGeneratorSource(t, "fun record(value: Int32) do end fun run(): Int32 do\nmut flag: Bool = true\nwhile flag do\n    defer record(1)\n    break\nend\nreturn 0\nend")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program})
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
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"a\", 1)\nend")
	files, err := GenerateChecked(appModuleGraph(), map[string]checker.Program{"app.hex": program})
	if err != nil {
		t.Fatal(err)
	}
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
		"memset(region, 0, bytes);",
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
	} {
		if strings.Contains(listH, forbid) || strings.Contains(dictH, forbid) {
			t.Fatalf("component header retains %q:\n%s\n%s", forbid, listH, dictH)
		}
	}
}
