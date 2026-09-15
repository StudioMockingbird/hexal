package integration

import (
	"strings"
	"testing"
)

func TestAlignedAllocationCompiles(t *testing.T) {
	for _, source := range []string{
		// The natural alignment, a larger constant, and a dynamic value.
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, align_of<Int32>())\n    defer h.free(p)\n    return ^p\nend\n",
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 64)\n    defer h.free(p)\n    return ^p\nend\n",
		"fun demo(h: Heap, alignment: Size): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, alignment)\n    defer h.free(p)\n    return ^p\nend\n",
		// Requesting less than the type's natural alignment is valid.
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 1)\n    defer h.free(p)\n    return ^p\nend\n",
		// Structs and arrays allocate the same way.
		"type Point is struct mut x: Int32, mut y: Int32, end\nfun demo(h: Heap): Int32 do\n    p: Ptr<mut Point> := h.allocate_aligned<Point>(Point(x = 1, y = 2), 64)\n    defer h.free(p)\n    return (^p).x\nend\n",
	} {
		assertCompiles(t, source)
	}
}

// A compile-time-known invalid alignment is decided by the checker.
func TestConstantInvalidAlignmentIsRejected(t *testing.T) {
	assertRejects(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 0)\n    return ^p\nend\n",
		"alignment must be a non-zero power of two; got 0")
	assertRejects(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 3)\n    return ^p\nend\n",
		"alignment must be a non-zero power of two; got 3")
	assertRejects(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 48)\n    return ^p\nend\n",
		"alignment must be a non-zero power of two; got 48")
}

// Everything else keeps the ordinary Heap.allocate diagnostics.
func TestAlignedAllocationKeepsOrdinaryDiagnostics(t *testing.T) {
	assertRejects(t,
		"fun demo(h: Heap, alignment: Int32): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, alignment)\n    return ^p\nend\n",
		"allocate_aligned requires Size; got Int32")
	assertRejects(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(true, 64)\n    return ^p\nend\n",
		"allocation initializer requires Int32; got Bool")
	assertRejects(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13)\n    return ^p\nend\n",
		"allocate_aligned expects 2 arguments (initial, alignment)")
	assertRejects(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned(13, 64)\n    return ^p\nend\n",
		"allocate_aligned requires exactly one type argument")
	assertRejectsAnyDiagnostic(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Atomic<Int32>> := h.allocate_aligned<Atomic<Int32>>(Atomic<Int32>(0), 64)\n    return 0\nend\n",
		"allocation requires a complete finite type")
}

// The generated C names alignof(T), one demand-selected primitive, and one
// mi_malloc_aligned call, with no over-allocation header, rounding formula,
// memset, or aligned-free wrapper.
func TestAlignedAllocationGeneratedC(t *testing.T) {
	result := assertCompiles(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 64)\n    defer h.free(p)\n    return ^p\nend\n")
	moduleHeader := moduleFile(t, result, "modules/app.h")
	for _, want := range []string{
		"static int32_t * hex_heap_allocate_aligned_Int32(hex_heap h, int32_t initial, size_t alignment) {",
		"int32_t *pointer = hex_heap_allocate_aligned(sizeof(int32_t), alignment, alignof(int32_t));",
		"*pointer = initial;",
	} {
		if !strings.Contains(moduleHeader, want) {
			t.Fatalf("modules/app.h = %q, want %q", moduleHeader, want)
		}
	}
	if !strings.Contains(rootC(t, result), "hex_heap_allocate_aligned_Int32(hex_v_h, 13, 64)") {
		t.Fatalf("modules/app.c = %q, want the typed aligned call", rootC(t, result))
	}
	heapSource := moduleFile(t, result, "hexal/heap.c")
	if got := strings.Count(heapSource, "void *hex_heap_allocate_aligned(size_t size, size_t alignment,"); got != 1 {
		t.Fatalf("hexal/heap.c defines the aligned primitive %d times, want exactly 1:\n%s", got, heapSource)
	}
	if got := strings.Count(heapSource, "mi_malloc_aligned("); got != 1 {
		t.Fatalf("hexal/heap.c calls mi_malloc_aligned %d times, want exactly 1:\n%s", got, heapSource)
	}
	for _, want := range []string{
		"if (alignment == 0 || (alignment & (alignment - 1)) != 0) {",
		"hex_runtime_trap(\"[Runtime Error] invalid allocation alignment\\n\");",
		"size_t effective_alignment = alignment < minimum_alignment",
	} {
		if !strings.Contains(heapSource, want) {
			t.Fatalf("hexal/heap.c = %q, want %q", heapSource, want)
		}
	}
	for _, unwanted := range []string{"memset", "hex_heap_free_aligned", "+ alignment - 1", "header"} {
		if strings.Contains(heapSource, unwanted) {
			t.Fatalf("hexal/heap.c emitted %q:\n%s", unwanted, heapSource)
		}
	}
	if !strings.Contains(moduleFile(t, result, "hexal/heap.h"), "void *hex_heap_allocate_aligned(size_t size, size_t alignment,") {
		t.Fatalf("hexal/heap.h lacks the aligned declaration")
	}
}

// A program without aligned allocation inherits none of the new surface.
func TestOrdinaryAllocationEmitsNoAlignedSurface(t *testing.T) {
	result := assertCompiles(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate<Int32>(1)\n    defer h.free(p)\n    return ^p\nend\n")
	for _, name := range []string{"hexal/heap.h", "hexal/heap.c", "modules/app.h", "modules/app.c"} {
		if strings.Contains(moduleFile(t, result, name), "allocate_aligned") {
			t.Fatalf("%s mentions aligned allocation:\n%s", name, moduleFile(t, result, name))
		}
	}
}

// Receiver, initializer, and alignment each evaluate exactly once, in source
// order, with the initializer complete before the alignment is validated.
func TestAlignedAllocationEvaluatesOperandsOnceInOrder(t *testing.T) {
	source := "fun initial(): Int32 do\n    return 13\nend\n" +
		"fun requested(): Size do\n    return 64\nend\n" +
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(initial(), requested())\n    defer h.free(p)\n    return ^p\nend\n"
	generated := withoutLineDirectives(rootC(t, assertCompiles(t, source)))
	// The prototype and definition both spell (void), so a bare "()" call
	// spelling counts exactly the call sites.
	for _, name := range []string{"hex_f_m3_app_initial()", "hex_f_m3_app_requested()"} {
		if got := strings.Count(generated, name); got != 1 {
			t.Fatalf("modules/app.c = %q, want exactly one call of %s; got %d", generated, name, got)
		}
	}
	initialAt := strings.Index(generated, "= hex_f_m3_app_initial();")
	requestedAt := strings.Index(generated, "= hex_f_m3_app_requested();")
	if initialAt < 0 || requestedAt < 0 || initialAt > requestedAt {
		t.Fatalf("modules/app.c = %q, want the initializer hoisted before the alignment", generated)
	}
}

// The result is an ordinary Heap allocation for cleanup purposes.
func TestAlignedAllocationKeepsOrdinaryReleaseRules(t *testing.T) {
	result := assertCompiles(t,
		"fun demo(h: Heap) do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 64)\n    h.free(p)\nend\n")
	if !strings.Contains(rootC(t, result), "hex_heap_free(") {
		t.Fatalf("modules/app.c = %q, want the ordinary release", rootC(t, result))
	}
	assertRejects(t,
		"fun demo(h: Heap) do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 64)\n    h.free(p)\n    h.free(p)\nend\n",
		"already released")
	assertRejects(t,
		"fun demo(h: Heap): Int32 do\n    p: Ptr<mut Int32> := h.allocate_aligned<Int32>(13, 64)\n    h.free(p)\n    return ^p\nend\n",
		"released")
}
