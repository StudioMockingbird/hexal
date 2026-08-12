package compiler

import (
	"strings"
	"testing"
)

// RFC 0020 Phase B: View<T> — non-owning contiguous views, source-tied
// lexical lifetimes, and the slice operations on Array and View.

func TestViewSliceReadOperations(t *testing.T) {
	result := Compile("fun demo()\n    fixed: Array<Int32, 3> = [10, 20, 30]\n    view: View<Int32> = fixed.slice(0, 2)\n    count: UInt64 = view.length()\n    empty: Bool = view.is_empty()\n    first: Int32 = view.at(0)\n    second: Int32 = view[1]\n    tail: View<Int32> = view.slice(1, 2)\n    last: Int32 = tail[0]\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
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
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestViewIsReadOnly(t *testing.T) {
	result := Compile("fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    view: View<Int32> = fixed.slice(0, 2)\n    view[0] = 5\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "read-only") {
		t.Fatalf("Compile stderr = %#v, want read-only view diagnostic", result.Stderr)
	}
}

func TestViewCannotBeRootedInTemporaryArray(t *testing.T) {
	result := Compile("fun make_fixed(): Array<Int32, 2>\n    return [1, 2]\nend\nfun demo()\n    view: View<Int32> = make_fixed().slice(0, 2)\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "temporary Array") {
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
			if result := Compile(source); result.ExitCode != ExitSuccess {
				t.Fatalf("Compile(%q) exit code = %d (%v), want 0", testCase.source, result.ExitCode, result.Stderr)
			}
		})
	}
}

func TestViewAllowsElementWritesToRootArray(t *testing.T) {
	result := Compile("fun demo()\n    mut fixed: Array<Int32, 2> = [1, 2]\n    view: View<Int32> = fixed.slice(0, 2)\n    fixed[0] = 5\n    total: Int32 = view[0] + fixed[1]\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
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
			result := Compile(testCase.source)
			if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
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
		result := Compile(source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "out of bounds") {
			t.Fatalf("Compile(%q) stderr = %#v, want slice-bounds diagnostic", source, result.Stderr)
		}
	}
}

func TestViewPassedToFunctionParameter(t *testing.T) {
	result := Compile("fun sum(values: View<Int32>): Int32\n    return values[0] + values[1]\nend\nfun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    total: Int32 = sum(fixed.slice(0, 2))\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static int32_t hex_f_sum(const hex_view_Int32 hex_v_values)",
		"*hex_view_at_Int32(hex_v_values, (size_t)(0))",
		"hex_v_total = hex_f_sum(hex_array_slice_Int32_2(&hex_v_fixed, (size_t)(0), (size_t)(2)));",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestViewPreservesMutPtrPointeeCapability(t *testing.T) {
	result := Compile("type Node = { mut score: Int32, }\nfun demo()\n    mut first: Node = Node { score = 1, }\n    mut second: Node = Node { score = 2, }\n    mut nodes: Array<MutPtr<Node>, 2> = [ref first, ref second]\n    view: View<MutPtr<Node>> = nodes.slice(0, 2)\n    view[0].score = 42\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, "hex_v_view") {
		t.Fatalf("main.c = %q, want view-based pointee write", result.MainC)
	}
}
