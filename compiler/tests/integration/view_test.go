package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestViewSliceReadOperations(t *testing.T) {
	result := compileSource("fun demo() do\n    fixed: Array<Int32, 3> := [10, 20, 30]\n    view: View<Int32> := fixed.slice(0, 2)\n    count: Size := view.length()\n    empty: Bool := view.length() == 0\n    first: Int32 := view[0]\n    second: Int32 := view[1]\n    tail: View<Int32> := view.slice(1, 2)\n    last: Int32 := tail[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_view_Int32 hex_v_view = hex_array_slice_Int32_3(&hex_v_fixed, (size_t)(0), (size_t)(2));",
		"(hex_v_view).length",
		"(hex_v_view).length == 0",
		"*hex_view_at_Int32(hex_v_view, (size_t)(0))",
		"*hex_view_at_Int32(hex_v_view, (size_t)(1))",
		"hex_v_tail = hex_view_slice_Int32(hex_v_view, (size_t)(1), (size_t)(2));",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
	// The specialization struct and its typed inline helpers are owned by
	// the view component, not hexal.h.
	viewH := moduleFile(t, result, "hexal/view.h")
	for _, want := range []string{
		"#ifndef HEXAL_VIEW_H",
		"#include \"hexal.h\"",
		"typedef struct hex_view_Int32 {",
		"const int32_t *data;",
		"size_t length;",
		"static inline const int32_t *hex_view_at_Int32(hex_view_Int32 view, size_t index) {",
		"hex_runtime_trap(\"[Runtime Error] view index out of bounds\\n\");",
		"static inline hex_view_Int32 hex_view_slice_Int32(hex_view_Int32 view, uint64_t start, uint64_t end) {",
		"hex_runtime_trap(\"[Runtime Error] view slice bounds out of range\\n\");",
		"#endif",
	} {
		if !strings.Contains(viewH, want) {
			t.Fatalf("hexal/view.h = %q, want %q", viewH, want)
		}
	}
	if !strings.Contains(rootH(t, result), "#include \"hexal/view.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/view.h component include", rootH(t, result))
	}
}

func TestViewIsReadOnly(t *testing.T) {
	result := compileSource("fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: View<Int32> := fixed.slice(0, 2)\n    view[0] = 5\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "read-only") {
		t.Fatalf("Compile stderr = %#v, want read-only view diagnostic", result.Stderr)
	}
}

func TestViewCannotBeRootedInTemporaryArray(t *testing.T) {
	result := compileSource("fun make_fixed(): Array<Int32, 2> do\n    return [1, 2]\nend\nfun demo() do\n    view: View<Int32> := make_fixed().slice(0, 2)\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "temporary Array") {
		t.Fatalf("Compile stderr = %#v, want temporary-Array diagnostic", result.Stderr)
	}
}

// Reassigning root storage while a view is live is the programmer's
// responsibility, so the compiler accepts it.
func TestViewAfterRootReassignmentIsValid(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"array binding", "fun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    view: View<Int32> := fixed.slice(0, 1)\n    fixed = [3, 4]\nend"},
		{"intermediate view", "fun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    mut view: View<Int32> := fixed.slice(0, 1)\n    tail: View<Int32> := view.slice(0, 1)\n    view = fixed.slice(0, 1)\nend"},
		{"member array", "fun demo() do\n    mut pair: Pair := Pair { values = [1, 2], }\n    view: View<Int32> := pair.values.slice(0, 1)\n    pair.values = [3, 4]\nend"},
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
	result := compileSource("fun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    view: View<Int32> := fixed.slice(0, 2)\n    fixed[0] = 5\n    total: Int32 := view[0] + fixed[1]\nend")
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
		{"ref of view", "fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: View<Int32> := fixed.slice(0, 1)\n    result: Int32 := ref view\nend", "ref cannot take the address of a View binding"},
		{"pointer to view", "fun demo() do\n    pointer: Ptr<View<Int32>> := nil\nend", "could not construct pointer type"},
		{"view of function", "fun demo() do\n    callbacks: View<Fun<(Int32)>> := [1]\nend", "not an inline view element type"},
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
		"fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: View<Int32> := fixed.slice(1, 3)\nend",
		"fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: View<Int32> := fixed.slice(2, 1)\nend",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "out of bounds") {
			t.Fatalf("Compile(%q) stderr = %#v, want slice-bounds diagnostic", source, result.Stderr)
		}
	}
}

func TestViewPassedToFunctionParameter(t *testing.T) {
	result := compileSource("fun sum(values: View<Int32>): Int32 do\n    return values[0] + values[1]\nend\nfun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    total: Int32 := sum(fixed.slice(0, 2))\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static int32_t hex_f_m3_app_sum(const hex_view_Int32 hex_v_values)",
		"*hex_view_at_Int32(hex_v_values, (size_t)(0))",
		"hex_v_total = hex_f_m3_app_sum(hex_array_slice_Int32_2(&hex_v_fixed, (size_t)(0), (size_t)(2)));",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestViewPreservesMutPtrPointeeCapability(t *testing.T) {
	result := compileSource("type Node = { mut score: Int32, }\nfun demo() do\n    mut first: Node := Node { score = 1, }\n    mut second: Node := Node { score = 2, }\n    mut nodes: Array<MutPtr<Node>, 2> := [ref first, ref second]\n    view: View<MutPtr<Node>> := nodes.slice(0, 2)\n    view[0].score = 42\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_v_view") {
		t.Fatalf("modules/app.c = %q, want view-based pointee write", rootC(t, result))
	}
}

func TestViewReturnRules(t *testing.T) {
	accepted := []string{
		"fun empty_demo(): View<Int32> do\n    return View<Int32>.empty()\nend\n",
		"type Packet = { bytes: String }\nfun payload(packet: Ptr<Packet>): View<Byte> do\n    return packet.bytes.slice(0, 4)\nend\n",
		"fun adopt(pointer: Ptr<Int32>, count: Size): View<Int32> do\n    return View<Int32>.from_pointer(pointer, count)\nend\n",
		"fun slice_of_param(xs: List<Int32>): View<Int32> do\n    return xs.slice(0, 1)\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
	rejected := []string{
		"fun head(): View<Int32> do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return fixed.slice(0, 2)\nend\n",
		"fun head(): View<Int32> do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    view: View<Int32> := fixed.slice(0, 2)\n    return view\nend\n",
	}
	for _, source := range rejected {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure {
			t.Fatalf("want reject; got accept:\n%s", source)
		}
	}
	// Documented limitation: a View nested inside a returned aggregate is
	// not diagnosed; catching it would require escape analysis.
	nested := "type Window = { visible: View<Int32> }\nfun bad(): Window do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return Window { visible = fixed.slice(0, 2) }\nend\n"
	if result := compileSource(nested); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("nested View return must compile by design: %v", result.Stderr)
	}
}

// Slicing an empty List or View renders the null-address guard, so a
// zero-length slice never forms &data[0] on a null backing pointer (which is
// undefined behavior in C even when unused).
func TestEmptyListViewSliceGuardsNullAddress(t *testing.T) {
	source := "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>.new(h)\n    first: View<Int32> := values.slice(0, 0)\n    empty: View<Int32> := View<Int32>.empty()\n    nested: View<Int32> := empty.slice(0, 0)\nend\n"
	result := assertCompiles(t, source)
	var generated strings.Builder
	for _, content := range result.Files {
		generated.WriteString(content)
	}
	all := generated.String()
	for _, want := range []string{
		"list->data == nullptr ? nullptr : &list->data[start]",
		"view.data == nullptr ? nullptr : &view.data[start]",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("generated artifacts lack the zero-length slice guard %q:\n%s", want, all)
		}
	}
}
