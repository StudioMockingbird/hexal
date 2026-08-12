package generator

import (
	"strings"
	"testing"
)

func TestGenerateHeapAllocationAndFree(t *testing.T) {
	program := checkedGeneratorSource(t, "h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) defer h.free(p)")
	mainC, mainH, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"hex_heap_raw_allocate", "hex_heap_free", "hex_heap_allocate_Int32", "hex_heap_header"} {
		if !strings.Contains(mainC, want) && !strings.Contains(mainH, want) {
			t.Fatalf("generated output does not contain %q: C=%q H=%q", want, mainC, mainH)
		}
	}
	if strings.Contains(mainC, "free(") && !strings.Contains(mainC, "hex_heap_free(") {
		t.Fatalf("generated C = %q, want only checked deallocation", mainC)
	}
}

func TestGenerateDeferReverseOrderAndCapture(t *testing.T) {
	program := checkedGeneratorSource(t, "fun record(value: Int32) end mut first: Int32 = 1 mut second: Int32 = 2 defer record(first) defer record(second)")
	mainC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainC, "hex_v_first") || !strings.Contains(mainC, "hex_v_second") {
		t.Fatalf("generated C = %q, want captured arguments", mainC)
	}
	if !strings.Contains(mainC, "record(hex_defer_capture_2)") || !strings.Contains(mainC, "record(hex_defer_capture_1)") {
		t.Fatalf("generated C = %q, want reverse-order deferred calls", mainC)
	}
}

func TestGenerateDeferRoutesBreakAndReturn(t *testing.T) {
	program := checkedGeneratorSource(t, "fun record(value: Int32) end fun run(): Int32\nmut flag: Bool = true\nwhile flag do\n    defer record(1)\n    break\nend\nreturn 0\nend")
	mainC, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mainC, "hex_f_record(hex_defer_capture_1);\n        break;") {
		t.Fatalf("generated C = %q, want deferred call on the break path", mainC)
	}
}
