package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestSliceSliceReadOperations(t *testing.T) {
	result := compileSource("fun demo() do\n    fixed: Array<Int32, 3> := [10, 20, 30]\n    view: Slice<Int32> := fixed.slice(0, 2)\n    count: Size := view.length()\n    empty: Bool := view.length() == 0\n    first: Int32 := view[0]\n    second: Int32 := view[1]\n    tail: Slice<Int32> := view.slice(1, 2)\n    last: Int32 := tail[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"const hex_slice_Int32 hex_v_view = hex_array_slice_Int32_3(&hex_v_fixed, (size_t)(0), (size_t)(2));",
		"(hex_v_view).length",
		"(hex_v_view).length == 0",
		"*hex_slice_at_Int32(hex_v_view, (size_t)(0))",
		"*hex_slice_at_Int32(hex_v_view, (size_t)(1))",
		"hex_v_tail = hex_slice_slice_Int32(hex_v_view, (size_t)(1), (size_t)(2));",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
	// The specialization struct and its typed inline helpers are owned by
	// the slice component, not hexal.h.
	viewH := moduleFile(t, result, "hexal/slice.h")
	for _, want := range []string{
		"#ifndef HEXAL_SLICE_H",
		"#include \"hexal.h\"",
		"typedef struct hex_slice_Int32 {",
		"const int32_t *data;",
		"size_t length;",
		"static inline const int32_t *hex_slice_at_Int32(hex_slice_Int32 slice, size_t index) {",
		"hex_runtime_trap(\"[Runtime Error] slice index out of bounds\\n\");",
		"static inline hex_slice_Int32 hex_slice_slice_Int32(hex_slice_Int32 slice, uint64_t start, uint64_t end) {",
		"hex_runtime_trap(\"[Runtime Error] slice slice bounds out of range\\n\");",
		"#endif",
	} {
		if !strings.Contains(viewH, want) {
			t.Fatalf("hexal/slice.h = %q, want %q", viewH, want)
		}
	}
	if !strings.Contains(rootH(t, result), "#include \"hexal/slice.h\"") {
		t.Fatalf("modules/app.h = %q, want the hexal/slice.h component include", rootH(t, result))
	}
}

func TestSliceIsReadOnly(t *testing.T) {
	result := compileSource("fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(0, 2)\n    view[0] = 5\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "read-only") {
		t.Fatalf("Compile stderr = %#v, want read-only view diagnostic", result.Stderr)
	}
}

func TestSliceCannotBeRootedInTemporaryArray(t *testing.T) {
	result := compileSource("fun make_fixed(): Array<Int32, 2> do\n    return [1, 2]\nend\nfun demo() do\n    view: Slice<Int32> := make_fixed().slice(0, 2)\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "temporary Array") {
		t.Fatalf("Compile stderr = %#v, want temporary-Array diagnostic", result.Stderr)
	}
}

// Reassigning root storage while a slice is live is the programmer's
// responsibility, so the compiler accepts it.
func TestSliceAfterRootReassignmentIsValid(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"array binding", "fun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(0, 1)\n    fixed = [3, 4]\nend"},
		{"intermediate view", "fun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    mut view: Slice<Int32> := fixed.slice(0, 1)\n    tail: Slice<Int32> := view.slice(0, 1)\n    view = fixed.slice(0, 1)\nend"},
		{"member array", "fun demo() do\n    mut pair: Pair := Pair(values = [1, 2],)\n    view: Slice<Int32> := pair.values.slice(0, 1)\n    pair.values = [3, 4]\nend"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			source := "type Pair is struct mut values: Array<Int32, 2>, end\n" + testCase.source
			if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
				t.Fatalf("Compile(%q) exit code = %d (%v), want 0", testCase.source, result.ExitCode, result.Stderr)
			}
		})
	}
}

func TestSliceAllowsElementWritesToRootArray(t *testing.T) {
	result := compileSource("fun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(0, 2)\n    fixed[0] = 5\n    total: Int32 := view[0] + fixed[1]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestSliceRestrictions(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"@of slice", "fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(0, 1)\n    result: Int32 := @view\nend", "@ cannot take the address of a Slice binding"},
		{"pointer to slice", "fun demo() do\n    pointer: Ptr<Slice<Int32>> := nil\nend", "could not construct pointer type"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

func TestSliceSliceConstantBoundsAreCompileErrors(t *testing.T) {
	for _, source := range []string{
		"fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(1, 3)\nend",
		"fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(2, 1)\nend",
	} {
		result := compileSource(source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "out of bounds") {
			t.Fatalf("Compile(%q) stderr = %#v, want slice-bounds diagnostic", source, result.Stderr)
		}
	}
}

func TestSlicePassedToFunctionParameter(t *testing.T) {
	result := compileSource("fun sum(values: Slice<Int32>): Int32 do\n    return values[0] + values[1]\nend\nfun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    total: Int32 := sum(fixed.slice(0, 2))\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static int32_t hex_f_m3_app_sum(const hex_slice_Int32 hex_v_values)",
		"*hex_slice_at_Int32(hex_v_values, (size_t)(0))",
		"hex_v_total = hex_f_m3_app_sum(hex_array_slice_Int32_2(&hex_v_fixed, (size_t)(0), (size_t)(2)));",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestSlicePreservesWritablePointeeCapability(t *testing.T) {
	result := compileSource("type Node is struct mut score: Int32, end\nfun demo() do\n    mut first: Node := Node(score = 1,)\n    mut second: Node := Node(score = 2,)\n    mut nodes: Array<Ptr<mut Node>, 2> := [@first, @second]\n    view: Slice<Ptr<mut Node>> := nodes.slice(0, 2)\n    view[0].score = 42\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_v_view") {
		t.Fatalf("modules/app.c = %q, want view-based pointee write", rootC(t, result))
	}
}

func TestSliceReturnRules(t *testing.T) {
	// A Slice carries no lifetime: direct, stored, nested, and returned
	// slices all compile. Backing-storage validity is the programmer's
	// responsibility.
	accepted := []string{
		"fun empty_demo(): Slice<Int32> do\n    return Slice<Int32>.empty()\nend\n",
		"type Packet is struct bytes: String end\nfun payload(packet: Ptr<Packet>): Slice<Byte> do\n    return packet.bytes.slice(0, 4)\nend\n",
		"fun adopt(pointer: Ptr<Int32>, count: Size): Slice<Int32> do\n    return Slice<Int32>.from_pointer(pointer, count)\nend\n",
		"fun slice_of_param(xs: List<Int32>): Slice<Int32> do\n    return xs.slice(0, 1)\nend\n",
		"fun head(): Slice<Int32> do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return fixed.slice(0, 2)\nend\n",
		"fun head(): Slice<Int32> do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    view: Slice<Int32> := fixed.slice(0, 2)\n    return view\nend\n",
		"type Window is struct visible: Slice<Int32> end\nfun bad(): Window do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return Window(visible = fixed.slice(0, 2))\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}

// Nested Slice returns compile in every aggregate shape that can carry
// one: a Slice is a copyable descriptor with no lifetime of its own.
func TestNestedSliceReturnsAreAccepted(t *testing.T) {
	accepted := []string{
		"type W is union | A as v: Slice<Int32> end | B as x: Int32 end end\nfun bad(): W do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return W.A(v = fixed.slice(0, 2))\nend\n",
		"type U is union Slice<Int32> | Nil end\nfun bad(): U do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    v: Slice<Int32> | Nil := fixed.slice(0, 2)\n    return v\nend\n",
		"type Window is struct visible: Slice<Int32> end\nfun bad(): Array<Window, 1> do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return [Window(visible = fixed.slice(0, 2))]\nend\n",
		"type Window is struct visible: Slice<Int32> end\nfun bad(): Window do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    tmp := Window(visible = fixed.slice(0, 2))\n    return tmp\nend\n",
		"fun bad(): Slice<Int32> | Nil do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return fixed.slice(0, 2)\nend\n",
		"type Window is struct visible: Slice<Int32> end\nfun bad(ok: Bool): Window do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return match ok\n    | true then Window(visible = fixed.slice(0, 2))\n    | false then Window(visible = fixed.slice(2, 4))\n    end\nend\n",
		"type Window is struct visible: Slice<Int32> end\nfun ok(v: Slice<Int32>): Window do\n    return Window(visible = v)\nend\n",
		"type Window is struct visible: Slice<Int32> end\nfun ok(v: Slice<Int32>): Window do\n    tmp := Window(visible = v)\n    return tmp\nend\n",
		"type Window is struct visible: Slice<Int32> end\nfun ok(): Window do\n    return Window(visible = Slice<Int32>.empty())\nend\n",
		"type Window is struct visible: Slice<Int32> end\nfun ok(p: Ptr<Int32>, n: Size): Window do\n    return Window(visible = Slice<Int32>.from_pointer(p, n))\nend\n",
		"fun keep(h: Heap): List<Int32> do\n    values: List<Int32> := List<Int32>(h)\n    return values\nend\n",
		"fun head(): Slice<Int32> do\n    fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    return fixed.slice(0, 2)\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}

// Slicing an empty List or Slice renders the null-address guard, so a
// zero-length slice never forms &data[0] on a null backing pointer (which is
// undefined behavior in C even when unused).
func TestEmptyListViewSliceGuardsNullAddress(t *testing.T) {
	source := "fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>(h)\n    first: Slice<Int32> := values.slice(0, 0)\n    empty: Slice<Int32> := Slice<Int32>.empty()\n    nested: Slice<Int32> := empty.slice(0, 0)\nend\n"
	result := assertCompiles(t, source)
	var generated strings.Builder
	for _, content := range result.Files {
		generated.WriteString(content)
	}
	all := generated.String()
	for _, want := range []string{
		"list->data == nullptr ? nullptr : &list->data[start]",
		"slice.data == nullptr ? nullptr : &slice.data[start]",
	} {
		if !strings.Contains(all, want) {
			t.Fatalf("generated artifacts lack the zero-length slice guard %q:\n%s", want, all)
		}
	}
}

func TestSliceMutSliceConstructionAndWrites(t *testing.T) {
	result := compileSource("fun demo() do\n    mut fixed: Array<Int32, 3> := [10, 20, 30]\n    window: Slice<mut Int32> := fixed.mut_slice(0, 2)\n    window[0] = 99\n    total: Int32 := window[0] + window[1]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_array_mut_slice_Int32_3(&hex_v_fixed, (size_t)(0), (size_t)(2));",
		"*hex_mut_slice_at_Int32(hex_v_window, (size_t)(0)) = 99;",
		"typedef struct hex_mut_slice_Int32 {",
		"int32_t *data;",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(moduleFile(t, result, "hexal/slice.h"), want) {
			t.Fatalf("generated output lacks %q", want)
		}
	}
}

func TestSliceMutSliceOnFixedList(t *testing.T) {
	// A fixed List handle already permits interior element mutation, so no
	// mut binding is required for mutable slicing.
	result := compileSource("fun demo(h: Heap) do\n    values: List<Int32> := List<Int32>(h)\n    values.push(1)\n    window: Slice<mut Int32> := values.mut_slice(0, 1)\n    window[0] = 7\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_list_mut_slice_") {
		t.Fatalf("modules/app.c = %q, want the mutable List slice helper", rootC(t, result))
	}
}

func TestSliceMutSliceRestrictions(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		want   string
	}{
		{"fixed array", "fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    window: Slice<mut Int32> := fixed.mut_slice(0, 2)\nend", "mut_slice requires a writable Array place"},
		{"slice receiver", "fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(0, 2)\n    window: Slice<mut Int32> := view.mut_slice(0, 1)\nend", "Slice has no method mut_slice"},
		{"read-only write", "fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(0, 2)\n    view[0] = 5\nend", "read-only"},
		{"string mut slice", "fun demo(text: String) do\n    window: Slice<mut Byte> := text.mut_slice(0, 1)\nend", "has no method mut_slice"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
				t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
			}
		})
	}
}

func TestSliceMutableReslicePreservesMode(t *testing.T) {
	// Re-slicing a writable Slice keeps writable access without another
	// mut_slice call.
	result := compileSource("fun demo() do\n    mut fixed: Array<Int32, 4> := [1, 2, 3, 4]\n    window: Slice<mut Int32> := fixed.mut_slice(0, 4)\n    narrow: Slice<mut Int32> := window.slice(1, 3)\n    narrow[0] = 9\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_mut_slice_slice_Int32") {
		t.Fatalf("modules/app.c = %q, want the mutable re-slice helper", rootC(t, result))
	}
}

func TestSliceWeakeningRules(t *testing.T) {
	accepted := compileSource("fun consume(values: Slice<Int32>): Int32 do\n    return values[0]\nend\nfun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    window: Slice<mut Int32> := fixed.mut_slice(0, 2)\n    total: Int32 := consume(window)\nend")
	if accepted.ExitCode != compiler.ExitSuccess {
		t.Fatalf("mutable-to-read-only weakening failed: %#v", accepted.Stderr)
	}
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"upgrade", "fun consume(values: Slice<mut Int32>) do\nend\nfun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    view: Slice<Int32> := fixed.slice(0, 2)\n    consume(view)\nend\n"},
		{"nested", "fun consume(values: Slice<Slice<Int32>>) do\nend\nfun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    window: Slice<mut Int32> := fixed.mut_slice(0, 2)\n    nested: Slice<Slice<mut Int32>> := Slice<Slice<mut Int32>>.empty()\n    consume(nested)\nend\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if result := compileSource(testCase.source); result.ExitCode != compiler.ExitFailure {
				t.Fatalf("want reject; got accept:\n%s", testCase.source)
			}
		})
	}
}

func TestSliceFromPointerModes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
		accept bool
	}{
		{"read from read", "fun wrap(p: Ptr<Int32>, n: Size): Slice<Int32> do\n    return Slice<Int32>.from_pointer(p, n)\nend\n", true},
		{"read from write", "fun wrap(p: Ptr<mut Int32>, n: Size): Slice<Int32> do\n    return Slice<Int32>.from_pointer(p, n)\nend\n", true},
		{"write from write", "fun wrap(p: Ptr<mut Int32>, n: Size): Slice<mut Int32> do\n    return Slice<mut Int32>.from_pointer(p, n)\nend\n", true},
		{"write from read", "fun wrap(p: Ptr<Int32>, n: Size): Slice<mut Int32> do\n    return Slice<mut Int32>.from_pointer(p, n)\nend\n", false},
		{"empty mutable", "fun demo(): Slice<mut Int32> do\n    return Slice<mut Int32>.empty()\nend\n", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			result := compileSource(testCase.source)
			if accept := result.ExitCode == compiler.ExitSuccess; accept != testCase.accept {
				t.Fatalf("accept = %v, want %v: %#v\n%s", accept, testCase.accept, result.Stderr, testCase.source)
			}
		})
	}
}

func TestSliceNestingAndPositions(t *testing.T) {
	accepted := []string{
		// Nested descriptors are first-class values.
		"fun demo() do\n    nested: Slice<Slice<Int32>> := Slice<Slice<Int32>>.empty()\nend\n",
		// Slices live in members, results, and collection elements.
		"type Holder is struct view: Slice<Int32> end\nfun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    holder: Holder := Holder(view = fixed.slice(0, 2))\nend\n",
		"fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    table: Array<Slice<Int32>, 1> := [fixed.slice(0, 2)]\nend\n",
		"fun demo(h: Heap) do\n    values: List<Slice<Int32>> := List<Slice<Int32>>(h)\nend\n",
		// Mutable slices iterate with copy binders like read-only slices.
		"fun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    window: Slice<mut Int32> := fixed.mut_slice(0, 2)\n    mut total: Int32 := 0\n    for value in window do\n        total = total + value\n    end\nend\n",
		// Mutable slices compare, print, and weaken like read-only slices.
		"fun demo() do\n    mut left: Array<Int32, 1> := [1]\n    mut right: Array<Int32, 1> := [1]\n    a: Slice<mut Int32> := left.mut_slice(0, 1)\n    b: Slice<mut Int32> := right.mut_slice(0, 1)\n    same: Bool := a == b\n    print(a)\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
	if result := compileSource("fun demo() do\n    pointer: Ptr<Slice<Int32>> := nil\nend"); result.ExitCode != compiler.ExitFailure {
		t.Fatalf("want Ptr-to-Slice rejection; got accept")
	}
}

func TestRetiredTypeNamesRejected(t *testing.T) {
	for _, source := range []string{
		"x: MutPtr<Int32> := x",
		"x: View<Int32> := x",
		"x: MutView<Int32> := x",
		"x: Ref<Int32> := x",
		"x: Box<Int32> := x",
	} {
		if result := compileSource(source); result.ExitCode != compiler.ExitFailure {
			t.Fatalf("want unknown-type rejection; got accept:\n%s", source)
		}
	}
	// The never-implemented Ref/Box names remain free for user declaration,
	// while the retired MutPtr/View names stay reserved.
	accepted := []string{
		"type Ref is struct value: Int32 end\nr: Ref := Ref(value = 1)",
		"type Box is struct value: Int32 end\nb: Box := Box(value = 2)",
		"type MutRef is struct value: Int32 end\nr: MutRef := MutRef(value = 3)",
		"type MutSlice is struct value: Int32 end\ns: MutSlice := MutSlice(value = 4)",
		"ref: Int32 := 1",
		"borrow: Int32 := 2",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}

func TestSliceCopiesAliasElements(t *testing.T) {
	// Copies share elements while holding independent descriptors: a write
	// through one copy reads back through the other.
	result := compileSource("fun demo() do\n    mut fixed: Array<Int32, 2> := [1, 2]\n    first: Slice<mut Int32> := fixed.mut_slice(0, 2)\n    second: Slice<mut Int32> := first.slice(0, 2)\n    first[0] = 9\n    check: Int32 := second[0]\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}
