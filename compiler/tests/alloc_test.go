package tests

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestHeapNewPerformsNoAllocation(t *testing.T) {
	result := compiler.Compile("h: Heap = Heap.new()")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Contains(result.MainC, "malloc") || !strings.Contains(result.MainC, "HEX_HEAP_DEFAULT") {
		t.Fatalf("generated C = %q, want no allocation in Heap.new()", result.MainC)
	}
}

func TestHeapAllocateInitializesAndReturnsWritablePointer(t *testing.T) {
	result := compiler.Compile("h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) p.value = 42")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainH, "hex_heap_allocate_Int32") || !strings.Contains(result.MainC, "*hex_v_p = 42") {
		t.Fatalf("generated output = H:%q C:%q, want checked allocation and write", result.MainH, result.MainC)
	}
}

func TestHeapFreeAcceptsReadOnlyAndWritablePointers(t *testing.T) {
	result := compiler.Compile("h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) defer h.free(p)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_heap_free(") {
		t.Fatalf("generated C = %q, want checked deallocation", result.MainC)
	}
	result = compiler.Compile("h: Heap = Heap.new() p: MutPtr<Int32> = h.allocate<Int32>(0) reader: Ptr<Int32> = p defer h.free(reader)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("read-only free exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestHeapAllocateRejectsIncompleteAndFunTargets(t *testing.T) {
	result := compiler.Compile("h: Heap = Heap.new() p: MutPtr<Unknown> = h.allocate<Unknown>(nil)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "allocation requires a complete finite type") {
		t.Fatalf("diagnostics = %#v, want incomplete-type error", result.Stderr)
	}
}

func TestDeferCapturesDirectCallArguments(t *testing.T) {
	result := compiler.Compile("fun record(value: Int32) end mut value: Int32 = 1 defer record(value) value = 2")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_defer_capture_1 = hex_v_value;") {
		t.Fatalf("generated C = %q, want registration-time capture", result.MainC)
	}
}

func TestDeferEvaluatesOtherExpressionsAtExit(t *testing.T) {
	result := compiler.Compile("mut value: Int32 = 1 defer value value = 2")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "(void)(hex_v_value);") {
		t.Fatalf("generated C = %q, want exit-time expression evaluation", result.MainC)
	}
}

func TestDeferRunsInReverseRegistrationOrder(t *testing.T) {
	result := compiler.Compile("fun record(value: Int32) end defer record(1) defer record(2)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	call2 := strings.Index(result.MainC, "record(hex_defer_capture_2)")
	call1 := strings.Index(result.MainC, "record(hex_defer_capture_1)")
	if call2 < 0 || call1 < 0 || call2 > call1 {
		t.Fatalf("generated C = %q, want capture 2 before capture 1", result.MainC)
	}
}

func TestDeferRunsOnBranchCompletion(t *testing.T) {
	result := compiler.Compile("fun record(value: Int32) end h: Heap = Heap.new() flag: Bool = true if flag defer record(1) end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_f_record(hex_defer_capture_1);") {
		t.Fatalf("generated C = %q, want branch-completion cleanup", result.MainC)
	}
}

func TestDeferRunsOnLoopIterationCompletion(t *testing.T) {
	result := compiler.Compile("fun record(value: Int32) end mut flag: Bool = true while flag do defer record(1) flag = false end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_f_record(hex_defer_capture_1);") {
		t.Fatalf("generated C = %q, want iteration-completion cleanup", result.MainC)
	}
}

func TestDeferRunsOnReturn(t *testing.T) {
	result := compiler.Compile("fun record(value: Int32) end fun run(): Int32\nmut flag: Bool = true\nif flag\n    defer record(1)\n    return 0\nend\nreturn 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_return_1") || !strings.Contains(result.MainC, "hex_f_record(hex_defer_capture_1);") {
		t.Fatalf("generated C = %q, want return-path cleanup", result.MainC)
	}
}

func TestDeferRunsOnBreakAndContinue(t *testing.T) {
	result := compiler.Compile("fun record(value: Int32) end mut flag: Bool = true while flag do defer record(1) break end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_f_record(hex_defer_capture_1);\n        break;") {
		t.Fatalf("generated C = %q, want break-path cleanup", result.MainC)
	}
}

func TestDeferNestedScopesUnwindInnerToOuter(t *testing.T) {
	result := compiler.Compile("fun record(value: Int32) end fun run(): Int32\nmut flag: Bool = true\nif flag\n    defer record(1)\n    while flag do\n        defer record(2)\n        break\n    end\n    return 0\nend\nreturn 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	capture2 := strings.Index(result.MainC, "record(hex_defer_capture_2)")
	capture1 := strings.Index(result.MainC, "record(hex_defer_capture_1)")
	if capture2 < 0 || capture1 < 0 || capture2 > capture1 {
		t.Fatalf("generated C = %q, want inner cleanup before outer cleanup", result.MainC)
	}
}

func TestHeapDiagnosticsFailClosed(t *testing.T) {
	result := compiler.Compile("h: Heap = Heap.new() mut v: Int32 = 1 h.free(v)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "value is not an allocation produced by this Heap") {
		t.Fatalf("diagnostics = %#v, want non-pointer free error", result.Stderr)
	}
}
