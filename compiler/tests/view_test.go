package tests

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0020 Phase B: View<T> — non-owning contiguous views, source-tied
// lexical lifetimes, and the slice operations on Array and View.

func TestViewSliceReadOperations(t *testing.T) {
	result := compileSource("fun demo()\n    fixed: Array<Int32, 3> = [10, 20, 30]\n    view: View<Int32> = fixed.slice(0, 2)\n    count: Size = view.length()\n    empty: Bool = view.is_empty()\n    first: Int32 = view.at(0)\n    second: Int32 = view[1]\n    tail: View<Int32> = view.slice(1, 2)\n    last: Int32 = tail[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"typedef struct hex_view_Int32 {",
		"const int32_t *data;",
		"size_t length;",
		"const hex_view_Int32 hex_v_view = hex_array_slice_Int32_3(&hex_v_fixed, (size_t)(0), (size_t)(2));",
		"(hex_v_view).length",
		"(hex_v_view).length == 0",
		"*hex_view_at_Int32(hex_v_view, (size_t)(0))",
		"*hex_view_at_Int32(hex_v_view, (size_t)(1))",
		"hex_v_tail = hex_view_slice_Int32(hex_v_view, (size_t)(1), (size_t)(2));",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestViewIsReadOnly(t *testing.T) {
	result := compileSource("fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    view: View<Int32> = fixed.slice(0, 2)\n    view[0] = 5\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "read-only") {
		t.Fatalf("Compile stderr = %#v, want read-only view diagnostic", result.Stderr)
	}
}

func TestViewCannotBeRootedInTemporaryArray(t *testing.T) {
	result := compileSource("fun make_fixed(): Array<Int32, 2>\n    return [1, 2]\nend\nfun demo()\n    view: View<Int32> = make_fixed().slice(0, 2)\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "temporary Array") {
		t.Fatalf("Compile stderr = %#v, want temporary-Array diagnostic", result.Stderr)
	}
}

// RFC 0035: reassigning root storage while a view is live is now the
// programmer's responsibility.
func TestViewAfterRootReassignmentIsValid(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"array binding", "fun demo()\n    mut fixed: Array<Int32, 2> = [1, 2]\n    view: View<Int32> = fixed.slice(0, 1)\n    fixed = [3, 4]\nend"},
		{"intermediate view", "fun demo()\n    mut fixed: Array<Int32, 2> = [1, 2]\n    mut view: View<Int32> = fixed.slice(0, 1)\n    tail: View<Int32> = view.slice(0, 1)\n    view = fixed.slice(0, 1)\nend"},
		{"member array", "fun demo()\n    mut pair: Pair = Pair { values = [1, 2], }\n    view: View<Int32> = pair.values.slice(0, 1)\n    pair.values = [3, 4]\nend"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := "type Pair = { mut values: Array<Int32, 2>, }\n" + testCase.source
			if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("Compile(%q) exit code = %d (%v), want 0", testCase.source, result.ExitCode, result.Stderr)
			}
		})
	}
}

func TestViewAllowsElementWritesToRootArray(t *testing.T) {
	result := compileSource("fun demo()\n    mut fixed: Array<Int32, 2> = [1, 2]\n    view: View<Int32> = fixed.slice(0, 2)\n    fixed[0] = 5\n    total: Int32 = view[0] + fixed[1]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestViewRestrictions(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"ref of view", "fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    view: View<Int32> = fixed.slice(0, 1)\n    result: Int32 = ref view\nend", "ref cannot take the address of a View binding"},
		{"pointer to view", "fun demo()\n    pointer: Ptr<View<Int32>> = nil\nend", "could not construct pointer type"},
		{"view of function", "fun demo()\n    callbacks: View<Fun<(Int32)>> = [1]\nend", "not an inline view element type"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

func TestViewSliceConstantBoundsAreCompileErrors(t *testing.T) {
	for _, source := range []string{
		"fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    view: View<Int32> = fixed.slice(1, 3)\nend",
		"fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    view: View<Int32> = fixed.slice(2, 1)\nend",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "out of bounds") {
			t.Fatalf("Compile(%q) stderr = %#v, want slice-bounds diagnostic", source, result.Stderr)
		}
	}
}

func TestViewPassedToFunctionParameter(t *testing.T) {
	result := compileSource("fun sum(values: View<Int32>): Int32\n    return values[0] + values[1]\nend\nfun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    total: Int32 = sum(fixed.slice(0, 2))\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static int32_t hex_f_3_app_sum(const hex_view_Int32 hex_v_values)",
		"*hex_view_at_Int32(hex_v_values, (size_t)(0))",
		"hex_v_total = hex_f_3_app_sum(hex_array_slice_Int32_2(&hex_v_fixed, (size_t)(0), (size_t)(2)));",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("main.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestViewPreservesMutPtrPointeeCapability(t *testing.T) {
	result := compileSource("type Node = { mut score: Int32, }\nfun demo()\n    mut first: Node = Node { score = 1, }\n    mut second: Node = Node { score = 2, }\n    mut nodes: Array<MutPtr<Node>, 2> = [ref first, ref second]\n    view: View<MutPtr<Node>> = nodes.slice(0, 2)\n    view[0].score = 42\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_v_view") {
		t.Fatalf("main.c = %q, want view-based pointee write", rootC(t, result))
	}
}

func TestViewReturnRules(t *testing.T) {
	accepted := []string{
		"fun empty_demo(): View<Int32>\n    return View<Int32>.empty()\nend\n",
		"type Packet = { bytes: String }\nfun payload(packet: Ptr<Packet>): View<Byte>\n    return packet.bytes.slice(0, 4)\nend\n",
		"fun adopt(pointer: Ptr<Int32>, count: Size): View<Int32>\n    return View<Int32>.from_pointer(pointer, count)\nend\n",
		"fun slice_of_param(xs: List<Int32>): View<Int32>\n    return xs.slice(0, 1)\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept, got %v:\n%s", result.Stderr, source)
		}
	}
	rejected := []string{
		"fun head(): View<Int32>\n    fixed: Array<Int32, 4> = [1, 2, 3, 4]\n    return fixed.slice(0, 2)\nend\n",
		"fun head(): View<Int32>\n    fixed: Array<Int32, 4> = [1, 2, 3, 4]\n    view: View<Int32> = fixed.slice(0, 2)\n    return view\nend\n",
	}
	for _, source := range rejected {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure {
			t.Fatalf("want reject, got accept:\n%s", source)
		}
	}
	// Documented limitation: a View nested inside a returned aggregate is not
	// diagnosed; the escape analysis RFC 0035 removed would be required.
	nested := "type Window = { visible: View<Int32> }\nfun bad(): Window\n    fixed: Array<Int32, 4> = [1, 2, 3, 4]\n    return Window { visible = fixed.slice(0, 2) }\nend\n"
	if result := compileSource(nested); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("nested View return must compile by design: %v", result.Stderr)
	}
}
