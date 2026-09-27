package integration

import (
	"strings"
	"testing"
)

func TestNestedInlineListLoopsUseLogicalLength(t *testing.T) {
	result := assertCompiles(t, "fun demo(): Int32 do\n    let grid: List<List<Int32, 3>, 2> = [[1, 2, 3], [4, 5, 6]]\n    let mut total: Int32 = 0\n    for row in grid do\n        for cell in row do\n            total = total + cell\n        end\n    end\n    return total\nend\n")
	body := rootC(t, result)
	for _, want := range []string{
		"hex_for_1_index < hex_for_1->length",
		"hex_for_2_index < hex_for_2->length",
		"if (hex_for_1->version != hex_for_1_version)",
		"if (hex_for_2->version != hex_for_2_version)",
		"hex_for_1_index++, (hex_for_1->version != hex_for_1_version ? hex_runtime_trap",
		"*hex_list_inline_at_List_Int32__3__2(hex_for_1",
		"*hex_list_inline_at_Int32_3(hex_for_2",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("generated loop lacks %q:\n%s", want, body)
		}
	}
	if !strings.Contains(listH(t, result), "if (index >= list->length)") {
		t.Fatalf("list.h lacks the logical-length index check")
	}
}

func TestInlineListLiteralAndRuntimeIndicesCheckLogicalLength(t *testing.T) {
	result := assertCompiles(t, "fun demo(i: Size): Int32 do\n    let fixed: List<Int32, 5> = [1, 2, 3, 4, 5]\n    return fixed[0] + fixed[i]\nend\n")
	body := rootC(t, result)
	if strings.Count(body, "hex_list_inline_at_Int32_5(&(hex_v_fixed)") != 2 {
		t.Fatalf("both literal and dynamic indices must use the checked accessor:\n%s", body)
	}
	if !strings.Contains(listH(t, result), "if (index >= list->length)") {
		t.Fatal("the inline accessor must compare against logical length")
	}
}

func TestInlineListAccessorDemandIsPerDirection(t *testing.T) {
	readOnly := rootC(t, assertCompiles(t, "fun demo(i: Size): Int32 do\n    let fixed: List<Int32, 3> = [1, 2, 3]\n    return fixed[i]\nend\n"))
	if !strings.Contains(readOnly, "hex_list_inline_at_Int32_3(") {
		t.Fatal("a read lost its inline accessor")
	}
	if strings.Contains(readOnly, "hex_list_inline_at_mut_Int32_3(") {
		t.Fatal("read-only access calls the mutable accessor")
	}
	writing := rootC(t, assertCompiles(t, "fun demo(i: Size): Int32 do\n    let mut fixed: List<Int32, 3> = [1, 2, 3]\n    fixed[i] = 9\n    return fixed[0]\nend\n"))
	if !strings.Contains(writing, "hex_list_inline_at_mut_Int32_3(") {
		t.Fatal("a write lost its mutable accessor")
	}
}
