package generator

import (
	"strings"
	"testing"
)

// RFC 0049 item 8.3: try lowering. A try statement hoists the union into a
// hex_try_N temporary, propagates the Error member, and discards the success
// value without a normalization temporary; a try expression rebinds a
// multi-success payload through a hex_try_result_N temporary.

func TestGenerateTryStatementLowering(t *testing.T) {
	program := checkedGeneratorSource(t, "fun fail(): Nil | Error\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error\n    try fail()\n    return 1\nend\n")
	rootC, _, _, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"const hex_internal_union_1 hex_try_1 = hex_f_fail();",
		"if (hex_try_1.tag == hex_internal_union_1_tag_member_",
		"return (hex_internal_union_2){ .tag = hex_internal_union_2_tag_member_1, .payload.member_1 = hex_try_1.payload.member_",
	} {
		if !strings.Contains(rootC, want) {
			t.Fatalf("generated C = %q, want %q", rootC, want)
		}
	}
	if strings.Contains(rootC, "hex_try_result_") {
		t.Fatalf("try statement must not normalize a discarded success value")
	}
}

func TestGenerateTryExpressionNormalizesSuccess(t *testing.T) {
	program := checkedGeneratorSource(t, "fun read_count(): Int32 | Error\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error\n    count: Int32 = try read_count()\n    return count\nend\n")
	rootC, _, _, _, err := GenerateChecked(program)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"const hex_internal_union_1 hex_try_1 = hex_f_read_count();",
		"if (hex_try_1.tag == hex_internal_union_1_tag_member_1) {",
		"hex_v_count = hex_try_1.payload.member_0;",
	} {
		if !strings.Contains(rootC, want) {
			t.Fatalf("generated C = %q, want %q", rootC, want)
		}
	}
	// A union with several success members needs a normalization temporary.
	multiple := checkedGeneratorSource(t, "fun read_number(): Int32 | Float32 | Error\n    return Error.new(\"Read Error\", \"bad\")\nend\nfun demo(): Int32 | Error\n    value: Int32 | Float32 = try read_number()\n    return 1\nend\n")
	multiC, _, _, _, err := GenerateChecked(multiple)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(multiC, "hex_try_result_") || !strings.Contains(multiC, "switch (hex_try_1.tag) {") {
		t.Fatalf("multi-success try must normalize through a temporary, got %q", multiC)
	}
}
