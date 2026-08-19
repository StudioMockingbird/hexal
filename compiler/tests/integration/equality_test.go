package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestLosslessNumericComparisonWidening(t *testing.T) {
	result := compileSource("fun demo() do\n    i32: Int32 := 1\n    i64: Int64 := 2\n    u32: UInt32 := 3\n    f32: Float32 := 1.5\n    same: Bool := i32 == i64\n    cross: Bool := i32 == u32\n    order: Bool := i32 < f32\n    small: Int16 := 1\n    tiny: UInt8 := 2\n    narrow: Bool := small == tiny\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"((int64_t)(hex_v_i32) == hex_v_i64)",
		"((int64_t)(hex_v_i32) == (int64_t)(hex_v_u32))",
		"((double)(hex_v_i32) < (double)(hex_v_f32))",
		"hex_v_narrow = (hex_v_small == (int16_t)(hex_v_tiny));",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
		}
	}
}

func TestLosslessNumericComparisonRejections(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo() do\n    i64: Int64 := 1\n    u64: UInt64 := 2\n    bad: Bool := i64 == u64\nend", "comparison has no lossless common numeric type"},
		{"fun demo() do\n    i64: Int64 := 1\n    u64: UInt64 := 2\n    bad: Bool := i64 < u64\nend", "comparison has no lossless common numeric type"},
		{"fun demo() do\n    f32: Float32 := 1.5\n    i64: Int64 := 1\n    bad: Bool := f32 == i64\nend", "comparison has no lossless common numeric type"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPointerIdentityEquality(t *testing.T) {
	result := compileSource("fun demo() do\n    mut value: Int32 := 1\n    left: Ptr<Int32> := ref value\n    right: Ptr<Int32> := left\n    same: Bool := left == right\n    different: Bool := left != right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "(hex_v_left == hex_v_right)") {
		t.Fatalf("modules/app.c = %q, want pointer identity comparison", rootC(t, result))
	}
}

func TestPointerEqualityRejectsStrengthening(t *testing.T) {
	result := compileSource("fun demo() do\n    mut value: Int32 := 1\n    left: Ptr<Int32> := ref value\n    right: MutPtr<Int32> := ref value\n    bad: Bool := left == right\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "pointer equality requires identical pointer types") {
		t.Fatalf("Compile stderr = %#v, want pointer-identity diagnostic", result.Stderr)
	}
}

func TestObjectEquality(t *testing.T) {
	result := compileSource("type Point = { x: Int32, y: Int32, }\nfun demo() do\n    left: Point := Point { x = 1, y = 2, }\n    right: Point := Point { x = 1, y = 2, }\n    same: Bool := left == right\n    different: Bool := left != right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static bool hex_equal_hex_t_m3_app_Point(const hex_t_m3_app_Point *left, const hex_t_m3_app_Point *right) {",
		"if (!((*left).hex_m_x == (*right).hex_m_x)) return false;",
		"hex_v_same = hex_equal_hex_t_m3_app_Point(&(hex_v_left), &(hex_v_right));",
		"(!hex_equal_hex_t_m3_app_Point(&(hex_v_left), &(hex_v_right)))",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestEqualityUnavailable(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun helper(value: Int32) do\nend\nfun demo() do\n    callback: Fun<(Int32)> := helper\n    other: Fun<(Int32)> := callback\n    bad: Bool := callback == other\nend", "function values are not equality-comparable"},
		{"fun helper(value: Int32) do\nend\nfun demo() do\n    mixed: Fun<(Int32)> | Int32 := helper\n    other: Fun<(Int32)> | Int32 := mixed\n    bad: Bool := mixed == other\nend", "union member Fun<(Int32)> does not support equality"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestStringEqualityAndOrdering(t *testing.T) {
	result := compileSource("fun demo() do\n    left: String := \"abc\"\n    right: String := \"abd\"\n    same: Bool := left == right\n    less: Bool := left < right\n    atMost: Bool := left <= right\n    greater: Bool := left > right\n    atLeast: Bool := left >= right\n    a: Strand := \"abc\"\n    b: Strand := \"abd\"\n    strandLess: Bool := a < b\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The per-type equality and ordering helpers are module-owned; the
	// module files spell the comparisons.
	output := rootC(t, result) + rootH(t, result)
	for _, want := range []string{
		"static bool hex_equal_hex_string(const hex_string *left, const hex_string *right) {",
		"static int hex_compare_hex_string(const hex_string *left, const hex_string *right) {",
		"hex_v_same = hex_equal_hex_string(hex_v_left, hex_v_right);",
		"(hex_compare_hex_string(hex_v_left, hex_v_right) < 0)",
		"(hex_compare_hex_string(hex_v_left, hex_v_right) <= 0)",
		"(hex_compare_hex_string(hex_v_left, hex_v_right) > 0)",
		"(hex_compare_hex_string(hex_v_left, hex_v_right) >= 0)",
		"memcmp(hex_v_a.data, hex_v_b.data, 32) < 0",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
	// String equality compares length first, then one memcmp over the shared
	// nonzero length; ordering memcmp's the shorter nonzero length and falls
	// back to the length comparison. No global Strand equality/ordering
	// helpers are emitted.
	for _, want := range []string{
		"memcmp(left->data, right->data, left->byte_length)",
		"memcmp(left->data, right->data, limit)",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated output lacks %q: %q %q", want, rootC(t, result), rootH(t, result))
		}
	}
	for _, forbidden := range []string{"static bool hex_equal_hex_strand(", "static int hex_compare_hex_strand("} {
		if strings.Contains(output, forbidden) {
			t.Fatalf("generated output retains deleted helper %q", forbidden)
		}
	}
}

// String and Strand equality/ordering produce identical results through
// memcmp. Empty values skip the standard memory call safely, prefix and
// differing-length payloads compare against the canonical zero-filled tail,
// non-ASCII UTF-8 compares bytewise, and the maximum 31-byte Strand payload
// still lowers to one direct 32-byte memcmp.
func TestTextEqualityOrderingThroughMemcmp(t *testing.T) {
	maxPayload := strings.Repeat("a", 31)
	source := "fun demo() do\n" +
		"    emptyA: String := \"\"\n" +
		"    emptyB: String := \"\"\n" +
		"    sameEmpty: Bool := emptyA == emptyB\n" +
		"    prefix: String := \"caf\u00E9\"\n" +
		"    longer: Bool := prefix < \"caf\u00E9!\"\n" +
		"    full: Strand := \"" + maxPayload + "\"\n" +
		"    fullSame: Bool := full == full\n" +
		"    tail: Strand := \"aa\"\n" +
		"    tailLess: Bool := tail < full\n" +
		"    emptyStrandA: Strand := \"\"\n" +
		"    emptyStrandB: Strand := \"\"\n" +
		"    sameEmptyStrand: Bool := emptyStrandA == emptyStrandB\n" +
		"end"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	output := rootC(t, result) + rootH(t, result)
	for _, want := range []string{
		"hex_equal_hex_string(hex_v_emptyA, hex_v_emptyB)",
		"memcmp(hex_v_full.data, hex_v_full.data, 32)",
		"memcmp(hex_v_tail.data, hex_v_full.data, 32) < 0",
		"memcmp(hex_v_emptyStrandA.data, hex_v_emptyStrandB.data, 32)",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated output lacks %q:\n%q\n%q", want, rootC(t, result), rootH(t, result))
		}
	}
	if strings.Contains(output, "static bool hex_equal_hex_strand(") || strings.Contains(output, "static int hex_compare_hex_strand(") {
		t.Fatalf("generated output retains a global Strand equality/ordering helper")
	}
}

func TestStrandMemberEqualityUsesMemcmp(t *testing.T) {
	source := "type Label = { tag: Strand, }\nfun demo() do\n    left: Label := Label { tag = \"a\" }\n    right: Label := Label { tag = \"a\" }\n    same: Bool := left == right\nend"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	output := rootC(t, result) + rootH(t, result)
	for _, want := range []string{
		"if (memcmp((*left).hex_m_tag.data, (*right).hex_m_tag.data, 32) != 0) return false;",
		"hex_equal_hex_t_m3_app_Label(&(hex_v_left), &(hex_v_right))",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated output lacks %q:\n%q\n%q", want, rootC(t, result), rootH(t, result))
		}
	}
	if strings.Contains(output, "static bool hex_equal_hex_strand(") {
		t.Fatalf("generated output retains the global Strand equality helper")
	}
}

func TestStringStrandEqualityRejected(t *testing.T) {
	result := compileSource("fun demo() do\n    text: String := \"abc\"\n    key: Strand := \"abc\"\n    bad: Bool := text == key\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "equality requires identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want strict text-type diagnostic", result.Stderr)
	}
}

func TestSequenceEquality(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    fixed: Array<Int32, 2> := [1, 2]\n    other: Array<Int32, 2> := [1, 2]\n    same: Bool := fixed == other\n    values: List<Int32> := List<Int32>.new(h)\n    defer values.free(h)\n    values.push(1)\n    view: View<Int32> := fixed.slice(0, 2)\n    total: Bool := view == fixed.slice(0, 2)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static bool hex_equal_hex_array_Int32_2(const hex_array_Int32_2 *left, const hex_array_Int32_2 *right) {",
		"if (!((*left).data[0] == (*right).data[0])) return false;",
		"hex_v_same = hex_equal_hex_array_Int32_2(&(hex_v_fixed), &(hex_v_other));",
	} {
		if !strings.Contains(rootC(t, result), want) && !strings.Contains(rootH(t, result), want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
}

func TestSequenceEqualityRequiresSameShape(t *testing.T) {
	result := compileSource("fun demo() do\n    fixed: Array<Int32, 2> := [1, 2]\n    other: Array<Int32, 3> := [1, 2, 3]\n    bad: Bool := fixed == other\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want shape-mismatch diagnostic", result.Stderr)
	}
}

func TestDictionaryEqualityRejected(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    left: Dict<Int32, Int32> := Dict<Int32, Int32>.new(h)\n    defer left.free(h)\n    right: Dict<Int32, Int32> := left\n    same: Bool := left == right\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "dictionary equality is not available") {
		t.Fatalf("Compile stderr = %#v, want dictionary rejection", result.Stderr)
	}
}

func TestAdtEquality(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32, } | Square as { a: Int32, }\nfun demo() do\n    left: Shape := Shape.Circle { r = 1, }\n    right: Shape := Shape.Circle { r = 1, }\n    same: Bool := left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static bool hex_equal_hex_Shape(const hex_Shape *left, const hex_Shape *right) {",
		"if ((*left).tag != (*right).tag) return false;",
		"if (!((*left).payload.Circle.hex_m_r == (*right).payload.Circle.hex_m_r)) return false;",
	} {
		if !strings.Contains(rootH(t, result), want) {
			t.Fatalf("modules/app.h = %q, want %q", rootH(t, result), want)
		}
	}
}

func TestUnionEqualityWithObjectMember(t *testing.T) {
	result := compileSource("type Point = { x: Int32, }\nfun demo() do\n    left: Point | Bool := Point { x = 1, }\n    right: Point | Bool := Point { x = 1, }\n    same: Bool := left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "static bool hex_union_4_bool14_t_m3_app_Point_equal(hex_union_4_bool14_t_m3_app_Point left, hex_union_4_bool14_t_m3_app_Point right) {") {
		t.Fatalf("modules/app.h = %q, want recursive union equality helper", rootH(t, result))
	}
}

func TestOrderingRejections(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo() do\n    left: Bool := true\n    right: Bool := false\n    bad: Bool := left < right\nend", "ordering is unavailable for Bool values"},
		{"fun demo() do\n    mut value: Int32 := 1\n    left: Ptr<Int32> := ref value\n    right: Ptr<Int32> := left\n    bad: Bool := left < right\nend", "ordering is unavailable for Ptr<Int32> values"},
		{"type Point = { x: Int32, }\nfun demo() do\n    left: Point := Point { x = 1, }\n    right: Point := left\n    bad: Bool := left < right\nend", "ordering is unavailable for Point values"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestGenericEqualityRecheckedAtSpecialization(t *testing.T) {
	result := compileSource("fun same<T>(a: T, b: T): Bool do\n    return a == b\nend\nfun demo() do\n    equal: Bool := same(1, 2)\n    matched: Bool := same(\"a\", \"b\")\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_equal_hex_string") {
		t.Fatalf("modules/app.h = %q, want specialized String equality helper", rootH(t, result))
	}
}

func TestNilComparisonRulesPreserved(t *testing.T) {
	// == nil requires a union containing Nil. A plain pointer has no Nil
	// member, so the literal gate rejects the comparison.
	result := compileSource("fun demo() do\n    nilSame: Bool := nil == nil\n    mut value: Int32 := 1\n    pointer: Ptr<Int32> := ref value\n    bad: Bool := pointer == nil\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "nil requires an expected union containing Nil") {
		t.Fatalf("Compile stderr = %#v, want standalone-nil diagnostic", result.Stderr)
	}
}

func TestListEqualityAccepted(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    left: List<Int32> := List<Int32>.new(h)\n    defer left.free(h)\n    left.push(1)\n    right: List<Int32> := left\n    same: Bool := left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "static bool hex_equal_hex_list_Int32(const hex_list_Int32 *left, const hex_list_Int32 *right) {") {
		t.Fatalf("modules/app.h = %q, want List equality helper", rootH(t, result))
	}
}

func TestManagedHandleEqualityRejected(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"Task", "fun worker(): Bool do\n    return true\nend\nfun f(h: Heap): Int32 | Error do\n    task: Task<Bool> := try spawn worker()\n    same: Bool := task == task\n    return 0\nend\n"},
		{"Channel", "fun f(h: Heap): Int32 | Error do\n    channel: Channel<Int32> := try Channel<Int32>.new(h, 4)\n    same: Bool := channel == channel\n    return 0\nend\n"},
		{"Mutex", "fun f(h: Heap): Int32 | Error do\n    mutex: Mutex := try Mutex.new(h)\n    same: Bool := mutex == mutex\n    return 0\nend\n"},
		{"Atomic", "counter: Atomic<Int32> := Atomic<Int32>.new(0)\nsame: Bool := counter == counter\n"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			assertRejects(t, testCase.source, "equality is unavailable")
		})
	}
}

// EoS compares by value like other value types: eos == eos folds to the
// singleton-identity truth and neither rendered binding cascades a spurious
// diagnostic about itself.
func TestEosComparesByValue(t *testing.T) {
	source := "same: Bool := eos == eos\ndifferent: Bool := eos != eos\n"
	result := assertCompiles(t, source)
	app := rootC(t, result)
	for _, want := range []string{"hex_v_same", "= true;", "hex_v_different", "= false;"} {
		if !strings.Contains(app, want) {
			t.Fatalf("modules/app.c = %q, want %q", app, want)
		}
	}
}
