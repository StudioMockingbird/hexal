package compiler

import (
	"strings"
	"testing"
)

// RFC 0024: equality, ordering, and hashability — lossless numeric widening,
// deep value equality, pointer identity, and text ordering.

func TestLosslessNumericComparisonWidening(t *testing.T) {
	result := Compile("fun demo()\n    i32: Int32 = 1\n    i64: Int64 = 2\n    u32: UInt32 = 3\n    f32: Float32 = 1.5\n    same: Bool = i32 == i64\n    cross: Bool = i32 == u32\n    order: Bool = i32 < f32\n    small: Int16 = 1\n    tiny: UInt8 = 2\n    narrow: Bool = small == tiny\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"((int64_t)((1)) == INT64_C(2))",
		"((int64_t)((1)) == (int64_t)((3)))",
		"((double)((1)) < (double)((0x1.4p+0f)))",
		"sw_v_narrow = (1 == (int16_t)((2)));",
	} {
		if !strings.Contains(result.MainC, want) {
			t.Fatalf("main.c = %q, want %q", result.MainC, want)
		}
	}
}

func TestLosslessNumericComparisonRejections(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo()\n    i64: Int64 = 1\n    u64: UInt64 = 2\n    bad: Bool = i64 == u64\nend", "comparison has no lossless common numeric type"},
		{"fun demo()\n    i64: Int64 = 1\n    u64: UInt64 = 2\n    bad: Bool = i64 < u64\nend", "comparison has no lossless common numeric type"},
		{"fun demo()\n    f32: Float32 = 1.5\n    i64: Int64 = 1\n    bad: Bool = f32 == i64\nend", "comparison has no lossless common numeric type"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPointerIdentityEquality(t *testing.T) {
	result := Compile("fun demo()\n    mut value: Int32 = 1\n    left: Ptr<Int32> = ref value\n    right: Ptr<Int32> = left\n    same: Bool = left == right\n    different: Bool = left != right\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, "(sw_v_left == sw_v_right)") {
		t.Fatalf("main.c = %q, want pointer identity comparison", result.MainC)
	}
}

func TestPointerEqualityRejectsStrengthening(t *testing.T) {
	result := Compile("fun demo()\n    mut value: Int32 = 1\n    left: Ptr<Int32> = ref value\n    right: MutPtr<Int32> = ref value\n    bad: Bool = left == right\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "pointer equality requires identical pointer types") {
		t.Fatalf("Compile stderr = %#v, want pointer-identity diagnostic", result.Stderr)
	}
}

func TestObjectEquality(t *testing.T) {
	result := Compile("type Point = { x: Int32, y: Int32, }\nfun demo()\n    left: Point = Point { x = 1, y = 2, }\n    right: Point = Point { x = 1, y = 2, }\n    same: Bool = left == right\n    different: Bool = left != right\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static bool sw_equal_sw_t_Point(sw_t_Point *left, sw_t_Point *right) {",
		"if (!((*left).sw_m_x == (*right).sw_m_x)) return false;",
		"sw_v_same = sw_equal_sw_t_Point(&(sw_v_left), &(sw_v_right));",
		"(!sw_equal_sw_t_Point(&(sw_v_left), &(sw_v_right)))",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestEqualityUnavailable(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun helper(value: Int32)\nend\nfun demo()\n    callback: Fun<(Int32)> = helper\n    other: Fun<(Int32)> = callback\n    bad: Bool = callback == other\nend", "function values are not equality-comparable"},
		{"fun helper(value: Int32)\nend\nfun demo()\n    mixed: Fun<(Int32)> | Int32 = helper\n    other: Fun<(Int32)> | Int32 = mixed\n    bad: Bool = mixed == other\nend", "union member Fun<(Int32)> does not support equality"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestStringEqualityAndOrdering(t *testing.T) {
	result := Compile("fun demo()\n    left: String = \"abc\"\n    right: String = \"abd\"\n    same: Bool = left == right\n    less: Bool = left < right\n    atMost: Bool = left <= right\n    greater: Bool = left > right\n    atLeast: Bool = left >= right\n    a: Strand = \"abc\"\n    b: Strand = \"abd\"\n    strandLess: Bool = a < b\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static bool sw_equal_sw_string(const sw_string *left, const sw_string *right) {",
		"static int sw_compare_sw_string(const sw_string *left, const sw_string *right) {",
		"static int sw_compare_sw_strand(sw_strand left, sw_strand right) {",
		"sw_v_same = sw_equal_sw_string(sw_v_left, sw_v_right);",
		"(sw_compare_sw_string(sw_v_left, sw_v_right) < 0)",
		"(sw_compare_sw_string(sw_v_left, sw_v_right) <= 0)",
		"(sw_compare_sw_string(sw_v_left, sw_v_right) > 0)",
		"(sw_compare_sw_string(sw_v_left, sw_v_right) >= 0)",
		"(sw_compare_sw_strand(sw_v_a, sw_v_b) < 0)",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestStringStrandEqualityRejected(t *testing.T) {
	result := Compile("fun demo()\n    text: String = \"abc\"\n    key: Strand = \"abc\"\n    bad: Bool = text == key\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "equality requires identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want strict text-type diagnostic", result.Stderr)
	}
}

func TestSequenceEquality(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    fixed: Array<Int32, 2> = [1, 2]\n    other: Array<Int32, 2> = [1, 2]\n    same: Bool = fixed == other\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    view: View<Int32> = fixed.slice(0, 2)\n    total: Bool = view == fixed.slice(0, 2)\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static bool sw_equal_sw_array_Int32_2(sw_array_Int32_2 *left, sw_array_Int32_2 *right) {",
		"if (!((*left).data[0] == (*right).data[0])) return false;",
		"sw_v_same = sw_equal_sw_array_Int32_2(&(sw_v_fixed), &(sw_v_other));",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestSequenceEqualityRequiresSameShape(t *testing.T) {
	result := Compile("fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    other: Array<Int32, 3> = [1, 2, 3]\n    bad: Bool = fixed == other\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want shape-mismatch diagnostic", result.Stderr)
	}
}

func TestDictionaryEqualityRejected(t *testing.T) {
	result := Compile("fun demo(h: Heap)\n    left: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer left.free(h)\n    right: Dict<Int32, Int32> = left\n    same: Bool = left == right\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "dictionary equality is not available") {
		t.Fatalf("Compile stderr = %#v, want dictionary rejection", result.Stderr)
	}
}

func TestAdtEquality(t *testing.T) {
	result := Compile("type Shape = | Circle as { r: Int32, } | Square as { a: Int32, }\nfun demo()\n    left: Shape = Shape.Circle { r = 1, }\n    right: Shape = Shape.Circle { r = 1, }\n    same: Bool = left == right\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	for _, want := range []string{
		"static bool sw_equal_sw_Shape(sw_Shape *left, sw_Shape *right) {",
		"if ((*left).tag != (*right).tag) return false;",
		"if (!((*left).payload.Circle.sw_m_r == (*right).payload.Circle.sw_m_r)) return false;",
	} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want %q", result.MainH, want)
		}
	}
}

func TestUnionEqualityWithObjectMember(t *testing.T) {
	result := Compile("type Point = { x: Int32, }\nfun demo()\n    left: Point | Bool = Point { x = 1, }\n    right: Point | Bool = Point { x = 1, }\n    same: Bool = left == right\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "static bool sw_internal_union_1_equal(sw_internal_union_1 left, sw_internal_union_1 right) {") {
		t.Fatalf("main.h = %q, want recursive union equality helper", result.MainH)
	}
}

func TestOrderingRejections(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo()\n    left: Bool = true\n    right: Bool = false\n    bad: Bool = left < right\nend", "ordering is unavailable for Bool values"},
		{"fun demo()\n    mut value: Int32 = 1\n    left: Ptr<Int32> = ref value\n    right: Ptr<Int32> = left\n    bad: Bool = left < right\nend", "ordering is unavailable for Ptr<Int32> values"},
		{"type Point = { x: Int32, }\nfun demo()\n    left: Point = Point { x = 1, }\n    right: Point = left\n    bad: Bool = left < right\nend", "ordering is unavailable for Point values"},
	} {
		result := Compile(testCase.source)
		if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestGenericEqualityRecheckedAtSpecialization(t *testing.T) {
	result := Compile("fun same<T>(a: T, b: T): Bool\n    return a == b\nend\nfun demo()\n    equal: Bool = same(1, 2)\n    matched: Bool = same(\"a\", \"b\")\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "sw_equal_sw_string") {
		t.Fatalf("main.h = %q, want specialized String equality helper", result.MainH)
	}
}

func TestNilComparisonRulesPreserved(t *testing.T) {
	result := Compile("fun demo()\n    nilSame: Bool = nil == nil\n    mut value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: Bool = pointer == nil\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "is never Nil") {
		t.Fatalf("Compile stderr = %#v, want non-null nil-test diagnostic", result.Stderr)
	}
}
