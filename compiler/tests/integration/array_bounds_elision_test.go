package integration

import (
	"strings"
	"testing"
)

// RFC 0088 removes bounds checks the compiler has already proven dead, and
// with them the accessors that existed only to carry those checks. These are
// its Validation cases.

// The spec's Evidence case. A nested for-in indexes nothing: both loops emit
// their own counter over a literal bound, so neither level keeps a check, and
// hexal/array.h collapses to the two typedefs.
func TestNestedForInEmitsNoAccessorAtAnyLevel(t *testing.T) {
	result := assertCompiles(t, "fun demo(): Int32 do\n    grid: Array<Array<Int32, 3>, 2> = [[1, 2, 3], [4, 5, 6]]\n    mut total: Int32 = 0\n    for row in grid do\n        for cell in row do\n            total = total + cell\n        end\n    end\n    return total\nend\n")
	body := rootC(t, result)
	if strings.Contains(body, "hex_array_at_") {
		t.Fatalf("modules/app.c still calls an accessor for a for-in binder:\n%s", body)
	}
	for _, want := range []string{
		"hex_for_1->data[hex_for_1_index]",
		"hex_for_2->data[hex_for_2_index]",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("modules/app.c = %q, want the direct member access %q", body, want)
		}
	}
	header := arrayH(t, result)
	if strings.Contains(header, "hex_array_at_") {
		t.Fatalf("hexal/array.h emits an accessor nothing calls:\n%s", header)
	}
	for _, want := range []string{
		"typedef struct hex_array_Int32_3 {",
		"typedef struct hex_array_Array_Int32__3__2 {",
	} {
		if !strings.Contains(header, want) {
			t.Fatalf("hexal/array.h = %q, want %q", header, want)
		}
	}
}

// Invariant 5 and the literal-only scope. A runtime index keeps its accessor
// and its check; a literal beside it does not. The for-in binder in the same
// body stays direct even though the accessor now exists.
func TestRuntimeIndexKeepsItsCheckBesideAnElidedOne(t *testing.T) {
	result := assertCompiles(t, "fun demo(i: Size): Int32 do\n    fixed: Array<Int32, 5> = [1, 2, 3, 4, 5]\n    mut total: Int32 = fixed[0] + fixed[i]\n    for value in fixed do\n        total = total + value\n    end\n    return total\nend\n")
	body := rootC(t, result)
	for _, want := range []string{
		"hex_v_fixed.data[0]", // literal: elided
		"*hex_array_at_Int32_5(&hex_v_fixed, (size_t)(hex_v_i))", // runtime: checked
		"hex_for_1->data[hex_for_1_index]",                       // binder: elided
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("modules/app.c = %q, want %q", body, want)
		}
	}
	header := arrayH(t, result)
	if !strings.Contains(header, "if (index >= UINT64_C(5)) {") {
		t.Fatalf("hexal/array.h dropped the check a runtime index still needs:\n%s", header)
	}
}

// The demand rule is per direction. A read-only program emits no at_mut even
// when its check survives; a write through a mut binding emits it.
func TestAccessorDemandIsPerDirection(t *testing.T) {
	readOnly := arrayH(t, assertCompiles(t, "fun demo(i: Size): Int32 do\n    fixed: Array<Int32, 3> = [1, 2, 3]\n    return fixed[i]\nend\n"))
	if !strings.Contains(readOnly, "hex_array_at_Int32_3(") {
		t.Fatalf("a surviving read lost its accessor:\n%s", readOnly)
	}
	if strings.Contains(readOnly, "hex_array_at_mut_") {
		t.Fatalf("a read-only program emits the mutable accessor:\n%s", readOnly)
	}
	writing := arrayH(t, assertCompiles(t, "fun demo(i: Size): Int32 do\n    mut fixed: Array<Int32, 3> = [1, 2, 3]\n    fixed[i] = 9\n    return fixed[0]\nend\n"))
	if !strings.Contains(writing, "hex_array_at_mut_Int32_3(") {
		t.Fatalf("a writing program lost the mutable accessor:\n%s", writing)
	}
}

// Non-goals: List and View length is a runtime field, so nothing about their
// output changes. This is the regression guard for that promise. The List read
// spells at_mut because every live List reference permits mutation without a
// mut binding — pre-existing behaviour this RFC does not touch.
func TestListAndViewAccessorsAreUntouched(t *testing.T) {
	result := assertCompiles(t, "fun demo(h: Heap): Int32 do\n    fixed: Array<Int32, 3> = [1, 2, 3]\n    window: View<Int32> = fixed.slice(0, 2)\n    values: List<Int32> = List<Int32>.new(h)\n    values.push(1)\n    total: Int32 = window[0] + values[0]\n    values.free(h)\n    return total\nend\n")
	body := rootC(t, result)
	for _, want := range []string{
		"*hex_view_at_Int32(",
		"*hex_list_at_mut_Int32(",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("modules/app.c = %q, want the unchanged %q", body, want)
		}
	}
}

// The class guard for the whole change: eliding an accessor that is still
// called would name an undeclared identifier in the generated C, which no
// test in this repository executes a compiler to catch. Assert it structurally
// instead — every hex_array_at* the module body calls must be defined in some
// generated artifact.
func TestEveryCalledArrayAccessorIsDefined(t *testing.T) {
	for _, source := range []string{
		"fun demo(): Int32 do\n    grid: Array<Array<Int32, 3>, 2> = [[1, 2, 3], [4, 5, 6]]\n    mut total: Int32 = 0\n    for row in grid do\n        for cell in row do\n            total = total + cell\n        end\n    end\n    return total\nend\n",
		"fun demo(i: Size): Int32 do\n    fixed: Array<Int32, 5> = [1, 2, 3, 4, 5]\n    return fixed[0] + fixed[i]\nend\n",
		"fun demo(i: Size): Int32 do\n    mut fixed: Array<Int32, 3> = [1, 2, 3]\n    fixed[i] = 9\n    return fixed[i]\nend\n",
		"type Pair = { mut values: Array<Int32, 2>, }\nfun demo(i: Size): Int32 do\n    mut pair: Pair = Pair { values = [3, 4], }\n    pair.values[i] = 9\n    return pair.values[0]\nend\n",
	} {
		result := assertCompiles(t, source)
		defined := ""
		for _, content := range result.Files {
			defined += content
		}
		body := rootC(t, result)
		for _, call := range calledArrayAccessors(body) {
			if !strings.Contains(defined, "static inline const") && !strings.Contains(defined, "static inline") {
				t.Fatalf("no accessor definitions at all, yet %s is called:\n%s", call, body)
			}
			if !strings.Contains(defined, call+"(const ") && !strings.Contains(defined, call+"(hex_array_") {
				t.Fatalf("modules/app.c calls %s but no artifact defines it.\nsource:\n%s\nbody:\n%s", call, source, body)
			}
		}
	}
}

// calledArrayAccessors returns the distinct hex_array_at* names the generated
// body invokes.
func calledArrayAccessors(body string) []string {
	seen := map[string]bool{}
	names := []string{}
	for _, fragment := range strings.Split(body, "hex_array_at_") {
		end := strings.IndexByte(fragment, '(')
		if end <= 0 {
			continue
		}
		name := "hex_array_at_" + fragment[:end]
		if strings.ContainsAny(name, " \t\n;,*&") || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, name)
	}
	return names
}
