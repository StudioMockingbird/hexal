package generator

import (
	"strings"
	"testing"

	"hexal/compiler/checker"
)

func TestGenerateHeapAllocationAndFree(t *testing.T) {
	program := checkedGeneratorSource(t, "h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) defer h.free(p)")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC, rootH, hexalH := files["modules/app.c"], files["modules/app.h"], files["hexal.h"]
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hex_heap_raw_allocate", "hex_heap_free", "hex_heap_allocate_Int32", "hex_heap_header"} {
		if !strings.Contains(rootC, want) && !strings.Contains(rootH, want) && !strings.Contains(hexalH, want) {
			t.Fatalf("generated output does not contain %q: C=%q H=%q", want, rootC, rootH)
		}
	}
	if strings.Contains(rootC, "free(") && !strings.Contains(rootC, "hex_heap_free(") {
		t.Fatalf("generated C = %q, want only checked deallocation", rootC)
	}
	assertRawAllocateCheckedArithmetic(t, hexalH)
}

// assertRawAllocateCheckedArithmetic verifies the RFC 0069 shape of
// hex_heap_raw_allocate: both size additions go through ckd_add with size_t
// destinations, zero alignment is rejected before align - 1 is evaluated,
// and neither old wrap-detection pattern remains.
func assertRawAllocateCheckedArithmetic(t *testing.T, hexalH string) {
	t.Helper()
	start := strings.Index(hexalH, "hex_heap_raw_allocate")
	end := strings.Index(hexalH, "static void hex_heap_free")
	if start < 0 || end < 0 || end <= start {
		t.Fatalf("hexal.h = %q, want hex_heap_raw_allocate before hex_heap_free", hexalH)
	}
	raw := hexalH[start:end]
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

func TestGenerateDeferReverseOrderAndCapture(t *testing.T) {
	program := checkedGeneratorSource(t, "fun record(value: Int32) do end mut first: Int32 = 1 mut second: Int32 = 2 defer record(first) defer record(second)")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
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
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	rootC := files["modules/app.c"]
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(rootC, "hex_f_m3_app_record(hex_defer_capture_1);\n        break;") {
		t.Fatalf("generated C = %q, want deferred call on the break path", rootC)
	}
}

// RFC 0069: List and Dict growth helpers keep every capacity temporary in
// size_t and lower doubling, element-region byte sizing, and the Dict
// load-factor decision through checked arithmetic.
// RFC 0069 Amendment 2: List relocation uses one guarded memcpy, a fresh
// Dict bucket region zeroes with one memset, Strand key probes compare the
// canonical 32-byte representation with memcmp, every diagnostic reports
// through hex_runtime_trap, and no compiler-owned NULL or raw fputs remains.
func TestGenerateListAndDictCheckedGrowth(t *testing.T) {
	program := checkedGeneratorSource(t, "fun demo(h: Heap) do\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    scores: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer scores.free(h)\n    scores.insert(1, 10)\n    labels: Dict<Strand, Int32> = Dict<Strand, Int32>.new(h)\n    defer labels.free(h)\n    labels.insert(\"a\", 1)\nend")
	files, err := GenerateChecked(map[string]checker.Program{"app.hex": program}, []string{"app"}, "app")
	if err != nil {
		t.Fatal(err)
	}
	hexalH := files["hexal.h"]
	for _, want := range []string{
		"size_t next = 1;",
		"ckd_mul(&next, list->capacity, 2)",
		"ckd_mul(&bytes, next, sizeof(int32_t))",
		"if (list->length != 0) {\n        memcpy(region, list->data, list->length * sizeof(int32_t));",
		"size_t next = 8;",
		"ckd_mul(&next, dict->capacity, 2)",
		"ckd_mul(&bytes, next, sizeof(hex_dict_entry_Int32_Int32))",
		"memset(region, 0, bytes);",
		"ckd_add(&length_plus_one, dict->length, 1)",
		"ckd_mul(&load_times_10, length_plus_one, 10)",
		"ckd_mul(&capacity_times_7, dict->capacity, 7)",
		"memcmp(region[index].key.data, key.data, 32) != 0",
		"memcmp(dict->buckets[index].key.data, key.data, 32) != 0",
		"hex_runtime_trap(\"[Runtime Error] list capacity is not representable\\n\")",
		"hex_runtime_trap(\"[Runtime Error] dictionary capacity is not representable\\n\")",
	} {
		if !strings.Contains(hexalH, want) {
			t.Fatalf("hexal.h does not contain %q:\n%s", want, hexalH)
		}
	}
	for _, forbid := range []string{
		"uint64_t next", "SIZE_MAX /", "(dict->length + 1) * 10 >= dict->capacity * 7",
		"hex_dict_key_equal_", "fputs(", "NULL", "region[index].active = false",
	} {
		if strings.Contains(hexalH, forbid) {
			t.Fatalf("hexal.h retains %q:\n%s", forbid, hexalH)
		}
	}
}
