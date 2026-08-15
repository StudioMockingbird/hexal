package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestHeapNewPerformsNoAllocation(t *testing.T) {
	result := compileSource("h: Heap = Heap.new()")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Contains(rootC(t, result), "malloc") || !strings.Contains(rootC(t, result), "HEX_HEAP_DEFAULT") {
		t.Fatalf("generated C = %q, want no allocation in Heap.new()", rootC(t, result))
	}
}

func TestHeapAllocateInitializesAndReturnsWritablePointer(t *testing.T) {
	result := compileSource("h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) p.value = 42")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(hexalH(t, result), "hex_heap_allocate_Int32") || !strings.Contains(rootC(t, result), "*hex_v_p = 42") {
		t.Fatalf("generated output = H:%q C:%q, want checked allocation and write", hexalH(t, result), rootC(t, result))
	}
}

func TestHeapFreeAcceptsReadOnlyAndWritablePointers(t *testing.T) {
	result := compileSource("h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) defer h.free(p)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_heap_free(") {
		t.Fatalf("generated C = %q, want checked deallocation", rootC(t, result))
	}
	result = compileSource("h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) reader: Ptr<Int32> = p defer h.free(reader)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("read-only free exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestHeapAllocateRejectsIncompleteAndFunTargets(t *testing.T) {
	result := compileSource("h: Heap = Heap.new() p: MutPtr<Unknown> = h.allocate<Unknown>(nil)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "allocation requires a complete finite type") {
		t.Fatalf("diagnostics = %#v, want incomplete-type error", result.Stderr)
	}
}

func TestDeferCapturesDirectCallArguments(t *testing.T) {
	result := compileSource("fun record(value: Int32) do end mut value: Int32 = 1 defer record(value) value = 2")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_defer_capture_1 = hex_v_value;") {
		t.Fatalf("generated C = %q, want registration-time capture", rootC(t, result))
	}
}

func TestDeferEvaluatesOtherExpressionsAtExit(t *testing.T) {
	result := compileSource("mut value: Int32 = 1 defer value value = 2")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "(void)(hex_v_value);") {
		t.Fatalf("generated C = %q, want exit-time expression evaluation", rootC(t, result))
	}
}

func TestDeferRunsInReverseRegistrationOrder(t *testing.T) {
	result := compileSource("fun record(value: Int32) do end defer record(1) defer record(2)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	call2 := strings.Index(rootC(t, result), "record(hex_defer_capture_2)")
	call1 := strings.Index(rootC(t, result), "record(hex_defer_capture_1)")
	if call2 < 0 || call1 < 0 || call2 > call1 {
		t.Fatalf("generated C = %q, want capture 2 before capture 1", rootC(t, result))
	}
}

func TestDeferRunsOnBranchCompletion(t *testing.T) {
	result := compileSource("fun record(value: Int32) do end h: Heap = Heap.new() flag: Bool = true if flag then defer record(1) end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_record(hex_defer_capture_1);") {
		t.Fatalf("generated C = %q, want branch-completion cleanup", rootC(t, result))
	}
}

func TestDeferRunsOnLoopIterationCompletion(t *testing.T) {
	result := compileSource("fun record(value: Int32) do end mut flag: Bool = true while flag do defer record(1) flag = false end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_record(hex_defer_capture_1);") {
		t.Fatalf("generated C = %q, want iteration-completion cleanup", rootC(t, result))
	}
}

func TestDeferRunsOnReturn(t *testing.T) {
	result := compileSource("fun record(value: Int32) do end fun run(): Int32 do\nmut flag: Bool = true\nif flag then\n    defer record(1)\n    return 0\nend\nreturn 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_return_1") || !strings.Contains(rootC(t, result), "hex_f_m3_app_record(hex_defer_capture_1);") {
		t.Fatalf("generated C = %q, want return-path cleanup", rootC(t, result))
	}
}

func TestDeferRunsOnBreakAndContinue(t *testing.T) {
	result := compileSource("fun record(value: Int32) do end mut flag: Bool = true while flag do defer record(1) break end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_f_m3_app_record(hex_defer_capture_1);\n        break;") {
		t.Fatalf("generated C = %q, want break-path cleanup", rootC(t, result))
	}
}

func TestDeferNestedScopesUnwindInnerToOuter(t *testing.T) {
	result := compileSource("fun record(value: Int32) do end fun run(): Int32 do\nmut flag: Bool = true\nif flag then\n    defer record(1)\n    while flag do\n        defer record(2)\n        break\n    end\n    return 0\nend\nreturn 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	capture2 := strings.Index(rootC(t, result), "record(hex_defer_capture_2)")
	capture1 := strings.Index(rootC(t, result), "record(hex_defer_capture_1)")
	if capture2 < 0 || capture1 < 0 || capture2 > capture1 {
		t.Fatalf("generated C = %q, want inner cleanup before outer cleanup", rootC(t, result))
	}
}

func TestHeapDiagnosticsFailClosed(t *testing.T) {
	result := compileSource("h: Heap = Heap.new() mut v: Int32 = 1 h.free(v)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "value is not an allocation produced by this Heap") {
		t.Fatalf("diagnostics = %#v, want non-pointer free error", result.Stderr)
	}
}

// A bare h.free(p) statement releases the allocation directly. Only the
// defer path used to render this form; the statement whitelist omitted
// HeapFreeExpression (snippet-audit regression).
func TestHeapFreeAsBareStatement(t *testing.T) {
	result := compileSource("fun f(h: Heap) do\n    p: MutPtr<Int32> = h.allocate<Int32>(0)\n    h.free(p)\nend\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_heap_free(hex_v_p") {
		t.Fatalf("generated C = %q, want a direct free call statement", rootC(t, result))
	}
}
