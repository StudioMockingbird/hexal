package tests

import (
	"hexal/compiler"
	"strings"
	"testing"
)

// RFC 0024: equality, ordering, and hashability — lossless numeric widening,
// deep value equality, pointer identity, and text ordering.

func TestLosslessNumericComparisonWidening(t *testing.T) {
	result := compileSource("fun demo()\n    i32: Int32 = 1\n    i64: Int64 = 2\n    u32: UInt32 = 3\n    f32: Float32 = 1.5\n    same: Bool = i32 == i64\n    cross: Bool = i32 == u32\n    order: Bool = i32 < f32\n    small: Int16 = 1\n    tiny: UInt8 = 2\n    narrow: Bool = small == tiny\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"((int64_t)((1)) == INT64_C(2))",
		"((int64_t)((1)) == (int64_t)((3)))",
		"((double)((1)) < (double)((0x1.4p+0f)))",
		"hex_v_narrow = (1 == (int16_t)((2)));",
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
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPointerIdentityEquality(t *testing.T) {
	result := compileSource("fun demo()\n    mut value: Int32 = 1\n    left: Ptr<Int32> = ref value\n    right: Ptr<Int32> = left\n    same: Bool = left == right\n    different: Bool = left != right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainC, "(hex_v_left == hex_v_right)") {
		t.Fatalf("main.c = %q, want pointer identity comparison", result.MainC)
	}
}

func TestPointerEqualityRejectsStrengthening(t *testing.T) {
	result := compileSource("fun demo()\n    mut value: Int32 = 1\n    left: Ptr<Int32> = ref value\n    right: MutPtr<Int32> = ref value\n    bad: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "pointer equality requires identical pointer types") {
		t.Fatalf("Compile stderr = %#v, want pointer-identity diagnostic", result.Stderr)
	}
}

func TestObjectEquality(t *testing.T) {
	result := compileSource("type Point = { x: Int32, y: Int32, }\nfun demo()\n    left: Point = Point { x = 1, y = 2, }\n    right: Point = Point { x = 1, y = 2, }\n    same: Bool = left == right\n    different: Bool = left != right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static bool hex_equal_hex_t_Point(const hex_t_Point *left, const hex_t_Point *right) {",
		"if (!((*left).hex_m_x == (*right).hex_m_x)) return false;",
		"hex_v_same = hex_equal_hex_t_Point(&(hex_v_left), &(hex_v_right));",
		"(!hex_equal_hex_t_Point(&(hex_v_left), &(hex_v_right)))",
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
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestStringEqualityAndOrdering(t *testing.T) {
	result := compileSource("fun demo()\n    left: String = \"abc\"\n    right: String = \"abd\"\n    same: Bool = left == right\n    less: Bool = left < right\n    atMost: Bool = left <= right\n    greater: Bool = left > right\n    atLeast: Bool = left >= right\n    a: Strand = \"abc\"\n    b: Strand = \"abd\"\n    strandLess: Bool = a < b\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static bool hex_equal_hex_string(const hex_string *left, const hex_string *right) {",
		"static int hex_compare_hex_string(const hex_string *left, const hex_string *right) {",
		"static int hex_compare_hex_strand(hex_strand left, hex_strand right) {",
		"hex_v_same = hex_equal_hex_string(hex_v_left, hex_v_right);",
		"(hex_compare_hex_string(hex_v_left, hex_v_right) < 0)",
		"(hex_compare_hex_string(hex_v_left, hex_v_right) <= 0)",
		"(hex_compare_hex_string(hex_v_left, hex_v_right) > 0)",
		"(hex_compare_hex_string(hex_v_left, hex_v_right) >= 0)",
		"(hex_compare_hex_strand(hex_v_a, hex_v_b) < 0)",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestStringStrandEqualityRejected(t *testing.T) {
	result := compileSource("fun demo()\n    text: String = \"abc\"\n    key: Strand = \"abc\"\n    bad: Bool = text == key\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "equality requires identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want strict text-type diagnostic", result.Stderr)
	}
}

func TestSequenceEquality(t *testing.T) {
	result := compileSource("fun demo(h: Heap)\n    fixed: Array<Int32, 2> = [1, 2]\n    other: Array<Int32, 2> = [1, 2]\n    same: Bool = fixed == other\n    values: List<Int32> = List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    view: View<Int32> = fixed.slice(0, 2)\n    total: Bool = view == fixed.slice(0, 2)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static bool hex_equal_hex_array_Int32_2(const hex_array_Int32_2 *left, const hex_array_Int32_2 *right) {",
		"if (!((*left).data[0] == (*right).data[0])) return false;",
		"hex_v_same = hex_equal_hex_array_Int32_2(&(hex_v_fixed), &(hex_v_other));",
	} {
		if !strings.Contains(result.MainC, want) && !strings.Contains(result.MainH, want) {
			t.Fatalf("generated output = %q %q, want %q", result.MainC, result.MainH, want)
		}
	}
}

func TestSequenceEqualityRequiresSameShape(t *testing.T) {
	result := compileSource("fun demo()\n    fixed: Array<Int32, 2> = [1, 2]\n    other: Array<Int32, 3> = [1, 2, 3]\n    bad: Bool = fixed == other\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want shape-mismatch diagnostic", result.Stderr)
	}
}

func TestDictionaryEqualityRejected(t *testing.T) {
	result := compileSource("fun demo(h: Heap)\n    left: Dict<Int32, Int32> = Dict<Int32, Int32>.new(h)\n    defer left.free(h)\n    right: Dict<Int32, Int32> = left\n    same: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "dictionary equality is not available") {
		t.Fatalf("Compile stderr = %#v, want dictionary rejection", result.Stderr)
	}
}

func TestAdtEquality(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32, } | Square as { a: Int32, }\nfun demo()\n    left: Shape = Shape.Circle { r = 1, }\n    right: Shape = Shape.Circle { r = 1, }\n    same: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static bool hex_equal_hex_Shape(const hex_Shape *left, const hex_Shape *right) {",
		"if ((*left).tag != (*right).tag) return false;",
		"if (!((*left).payload.Circle.hex_m_r == (*right).payload.Circle.hex_m_r)) return false;",
	} {
		if !strings.Contains(result.MainH, want) {
			t.Fatalf("main.h = %q, want %q", result.MainH, want)
		}
	}
}

func TestUnionEqualityWithObjectMember(t *testing.T) {
	result := compileSource("type Point = { x: Int32, }\nfun demo()\n    left: Point | Bool = Point { x = 1, }\n    right: Point | Bool = Point { x = 1, }\n    same: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainH, "static bool hex_internal_union_1_equal(hex_internal_union_1 left, hex_internal_union_1 right) {") {
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
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestGenericEqualityRecheckedAtSpecialization(t *testing.T) {
	result := compileSource("fun same<T>(a: T, b: T): Bool\n    return a == b\nend\nfun demo()\n    equal: Bool = same(1, 2)\n    matched: Bool = same(\"a\", \"b\")\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainH, "hex_equal_hex_string") {
		t.Fatalf("main.h = %q, want specialized String equality helper", result.MainH)
	}
}

func TestNilComparisonRulesPreserved(t *testing.T) {
	// RFC 0049 item 8.1: == nil requires a union containing Nil. A plain
	// pointer has no Nil member, so the literal gate rejects the comparison.
	result := compileSource("fun demo()\n    nilSame: Bool = nil == nil\n    mut value: Int32 = 1\n    pointer: Ptr<Int32> = ref value\n    bad: Bool = pointer == nil\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "nil requires an expected union containing Nil") {
		t.Fatalf("Compile stderr = %#v, want standalone-nil diagnostic", result.Stderr)
	}
}

// RFC 0049: List equality is deep element equality; Task, Channel, Mutex,
// Atomic, and Stream handles have no equality at all.
func TestListEqualityAccepted(t *testing.T) {
	result := compileSource("fun demo(h: Heap)\n    left: List<Int32> = List<Int32>.new(h)\n    defer left.free(h)\n    left.push(1)\n    right: List<Int32> = left\n    same: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(result.MainH, "static bool hex_equal_hex_list_Int32(const hex_list_Int32 *left, const hex_list_Int32 *right) {") {
		t.Fatalf("main.h = %q, want List equality helper", result.MainH)
	}
}

func TestManagedHandleEqualityRejected(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"Task", "fun worker(): Bool\n    return true\nend\nfun f(h: Heap): Int32 | Error\n    task: Task<Bool> = try spawn worker()\n    same: Bool = task == task\n    return 0\nend\n"},
		{"Channel", "fun f(h: Heap): Int32 | Error\n    channel: Channel<Int32> = try Channel<Int32>.new(h, 4)\n    same: Bool = channel == channel\n    return 0\nend\n"},
		{"Mutex", "fun f(h: Heap): Int32 | Error\n    mutex: Mutex = try Mutex.new(h)\n    same: Bool = mutex == mutex\n    return 0\nend\n"},
		{"Atomic", "counter: Atomic<Int32> = Atomic<Int32>.new(0)\nsame: Bool = counter == counter\n"},
		{"Stream", "fun f()\n    s: Stream<Int32> = Stream<Int32>.new()\n    same: Bool = s == s\nend\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, "equality is unavailable")
		})
	}
}
