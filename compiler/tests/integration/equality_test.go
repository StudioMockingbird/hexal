package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestLosslessNumericComparisonWidening(t *testing.T) {
	result := compileSource("fun demo() do\n    let i32: Int32 = 1\n    let i64: Int64 = 2\n    let u32: UInt32 = 3\n    let f32: Float32 = 1.5\n    let same: Bool = i32 == i64\n    let cross: Bool = i32 == u32\n    let order: Bool = i32 < f32\n    let small: Int16 = 1\n    let tiny: UInt8 = 2\n    let narrow: Bool = small == tiny\nend")
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
		{"fun demo() do\n    let i64: Int64 = 1\n    let u64: UInt64 = 2\n    let bad: Bool = i64 == u64\nend", "comparison has no lossless common numeric type"},
		{"fun demo() do\n    let i64: Int64 = 1\n    let u64: UInt64 = 2\n    let bad: Bool = i64 < u64\nend", "comparison has no lossless common numeric type"},
		{"fun demo() do\n    let f32: Float32 = 1.5\n    let i64: Int64 = 1\n    let bad: Bool = f32 == i64\nend", "comparison has no lossless common numeric type"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestPointerIdentityEquality(t *testing.T) {
	result := compileSource("fun demo() do\n    let mut value: Int32 = 1\n    let left: Ptr<Int32> = @value\n    let right: Ptr<Int32> = left\n    let same: Bool = left == right\n    let different: Bool = left != right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "(hex_v_left == hex_v_right)") {
		t.Fatalf("modules/app.c = %q, want pointer identity comparison", rootC(t, result))
	}
}

func TestPointerEqualityRejectsStrengthening(t *testing.T) {
	result := compileSource("fun demo() do\n    let mut value: Int32 = 1\n    let left: Ptr<Int32> = @value\n    let right: Ptr<mut Int32> = @value\n    let bad: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "pointer equality requires identical pointer types") {
		t.Fatalf("Compile stderr = %#v, want pointer-identity diagnostic", result.Stderr)
	}
}

func TestObjectEquality(t *testing.T) {
	result := compileSource("type Point is struct x: Int32, y: Int32, end\nfun demo() do\n    let left: Point = Point(x = 1, y = 2, )\n    let right: Point = Point(x = 1, y = 2, )\n    let same: Bool = left == right\n    let different: Bool = left != right\nend")
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
		{"fun helper(value: Int32) do\nend\nfun demo() do\n    let callback: Fun<(Int32)> = helper\n    let other: Fun<(Int32)> = callback\n    let bad: Bool = callback == other\nend", "function values are not equality-comparable"},
		{"fun helper(value: Int32) do\nend\nfun demo() do\n    let mixed: Fun<(Int32)> | Int32 = helper\n    let other: Fun<(Int32)> | Int32 = mixed\n    let bad: Bool = mixed == other\nend", "union member Fun<(Int32)> does not support equality"},
		// An Array/Slice/List element that cannot compare must name its
		// element type; it must never fall back to an empty member name,
		// which is what "member  does not support ==" would render as.
		{"fun helper(x: Int32): Int32 do return x end\nlet a: Array<Fun<(Int32) : Int32>, 1> = [helper]\nlet b: Array<Fun<(Int32) : Int32>, 1> = [helper]\nlet x: Bool = a == b\n", "element type Fun<(Int32) : Int32> does not support =="},
		{"let h1: Array<Heap, 1> = [Heap()]\nlet h2: Array<Heap, 1> = [Heap()]\nlet x: Bool = h1 == h2\n", "element type Heap does not support =="},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestStringEqualityAndOrdering(t *testing.T) {
	result := compileSource("fun demo() do\n    let left: String = \"abc\"\n    let right: String = \"abd\"\n    let same: Bool = left == right\n    let less: Bool = left < right\n    let atMost: Bool = left <= right\n    let greater: Bool = left > right\n    let atLeast: Bool = left >= right\n    let a: Strand = \"abc\"\n    let b: Strand = \"abd\"\n    let strandLess: Bool = a < b\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The string equality and ordering helpers live in hexal/string.h and
	// hexal/string.c with external linkage.
	stringH := moduleFile(t, result, "hexal/string.h")
	stringC := moduleFile(t, result, "hexal/string.c")
	for _, want := range []string{
		"bool hex_equal_hex_string(const hex_string *left, const hex_string *right);",
		"int hex_compare_hex_string(const hex_string *left, const hex_string *right);",
	} {
		if !strings.Contains(stringH, want) {
			t.Fatalf("hexal/string.h = %q, want %q", stringH, want)
		}
	}
	for _, want := range []string{
		"bool hex_equal_hex_string(const hex_string *left, const hex_string *right) {",
		"int hex_compare_hex_string(const hex_string *left, const hex_string *right) {",
	} {
		if !strings.Contains(stringC, want) {
			t.Fatalf("hexal/string.c = %q, want %q", stringC, want)
		}
	}
	// The module file spells the comparisons calling the extern helpers.
	output := rootC(t, result) + rootH(t, result)
	for _, want := range []string{
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
		if !strings.Contains(stringC, want) {
			t.Fatalf("hexal/string.c lacks %q: %q", want, stringC)
		}
	}
	for _, forbidden := range []string{"static bool hex_equal_hex_strand(", "static int hex_compare_hex_strand("} {
		if strings.Contains(stringC, forbidden) || strings.Contains(stringH, forbidden) {
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
		"    let emptyA: String = \"\"\n" +
		"    let emptyB: String = \"\"\n" +
		"    let sameEmpty: Bool = emptyA == emptyB\n" +
		"    let prefix: String = \"café\"\n" +
		"    let longer: Bool = prefix < \"café!\"\n" +
		"    let full: Strand = \"" + maxPayload + "\"\n" +
		"    let fullSame: Bool = full == full\n" +
		"    let tail: Strand = \"aa\"\n" +
		"    let tailLess: Bool = tail < full\n" +
		"    let emptyStrandA: Strand = \"\"\n" +
		"    let emptyStrandB: Strand = \"\"\n" +
		"    let sameEmptyStrand: Bool = emptyStrandA == emptyStrandB\n" +
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
	source := "type Label is struct tag: Strand, end\nfun demo() do\n    let left: Label = Label(tag = \"a\")\n    let right: Label = Label(tag = \"a\")\n    let same: Bool = left == right\nend"
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
	result := compileSource("fun demo() do\n    let text: String = \"abc\"\n    let key: Strand = \"abc\"\n    let bad: Bool = text == key\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "equality requires identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want strict text-type diagnostic", result.Stderr)
	}
}

func TestSequenceEquality(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let fixed: Array<Int32, 2> = [1, 2]\n    let other: Array<Int32, 2> = [1, 2]\n    let same: Bool = fixed == other\n    let values: List<Int32> = List<Int32>(h)\n    defer values.free(h)\n    values.push(1)\n    let view: Slice<Int32> = fixed.slice(0, 2)\n    let total: Bool = view == fixed.slice(0, 2)\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	equalityH := moduleFile(t, result, "hexal/equality.h")
	for _, want := range []string{
		"if (!((*left).data[0] == (*right).data[0])) return false;",
	} {
		if !strings.Contains(equalityH, want) {
			t.Fatalf("hexal/equality.h = %q, want %q", equalityH, want)
		}
	}
	if !strings.Contains(rootC(t, result), "hex_v_same = hex_equal_hex_array_Int32_2(&(hex_v_fixed), &(hex_v_other));") {
		t.Fatalf("modules/app.c = %q, want the component equality call", rootC(t, result))
	}
	if strings.Contains(rootH(t, result), "static bool hex_equal_hex_array_Int32_2") {
		t.Fatalf("modules/app.h re-emits the program-owned Array equality helper")
	}
}

func TestProgramOwnedEqualitySharedAcrossModules(t *testing.T) {
	compare := `fun compare(h: Heap): Bool do
    let left: Array<Int32, 2> = [1, 2]
    let right: Array<Int32, 2> = [1, 2]
    let leftView: Slice<Int32> = left.slice(0, 2)
    let rightView: Slice<Int32> = right.slice(0, 2)
    let leftList: List<Int32> = List<Int32>(h)
    let rightList: List<Int32> = List<Int32>(h)
    leftList.push(1)
    rightList.push(1)
    let leftError: Error = Error(ErrorKind.Other(header = "x"), "y")
    let rightError: Error = Error(ErrorKind.Other(header = "x"), "y")
    return (left == right) and (leftView == rightView) and (leftList == rightList) and (leftError == rightError)
end
`
	result := compiler.Compile(map[string]string{
		"app.hex":  "import\n    Math from \"./math\"\nend\n" + compare + "let root: Bool = compare(Heap())\nlet other: Bool = Math.compare(Heap())\n",
		"math.hex": compare + "export\n    compare\nend\n",
	}, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v)", result.ExitCode, result.Stderr)
	}
	equality := result.Files["hexal/equality.h"]
	for _, helper := range []string{
		"hex_equal_hex_array_Int32_2",
		"hex_equal_hex_slice_Int32",
		"hex_equal_hex_list_Int32",
		"hex_equal_hex_t_Error",
	} {
		if count := strings.Count(equality, "static bool "+helper+"("); count != 1 {
			t.Fatalf("hexal/equality.h defines %s %d times, want once:\n%s", helper, count, equality)
		}
		for _, module := range []string{"modules/app.h", "modules/math.h"} {
			if strings.Contains(result.Files[module], "static bool "+helper+"(") {
				t.Fatalf("%s re-emits program-owned helper %s:\n%s", module, helper, result.Files[module])
			}
		}
	}
	if !strings.Contains(equality, "#include <string.h>") {
		t.Fatalf("hexal/equality.h omits <string.h> for Error's Strand comparison:\n%s", equality)
	}
	for _, module := range []string{"modules/app.h", "modules/math.h"} {
		if !strings.Contains(result.Files[module], `#include "hexal/equality.h"`) {
			t.Fatalf("%s does not include hexal/equality.h:\n%s", module, result.Files[module])
		}
	}
}

func TestModuleOwnedEqualityCallsProgramOwnedHelper(t *testing.T) {
	result := compileSource(`fun demo(h: Heap): Bool do
    let leftList: List<Int32> = List<Int32>(h)
    let rightList: List<Int32> = List<Int32>(h)
    let left: List<Int32> | Bool = leftList
    let right: List<Int32> | Bool = rightList
    return left == right
end
`)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v)", result.ExitCode, result.Stderr)
	}
	header := rootH(t, result)
	if !strings.Contains(header, `#include "hexal/equality.h"`) {
		t.Fatalf("modules/app.h does not include hexal/equality.h:\n%s", header)
	}
	if !strings.Contains(header, "hex_equal_hex_list_Int32") {
		t.Fatalf("modules/app.h does not call the program-owned List helper:\n%s", header)
	}
}

func TestSequenceEqualityRequiresSameShape(t *testing.T) {
	result := compileSource("fun demo() do\n    let fixed: Array<Int32, 2> = [1, 2]\n    let other: Array<Int32, 3> = [1, 2, 3]\n    let bad: Bool = fixed == other\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want shape-mismatch diagnostic", result.Stderr)
	}
}

func TestDictionaryEqualityRejected(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let left: Dict<Int32, Int32> = Dict<Int32, Int32>(h)\n    defer left.free(h)\n    let right: Dict<Int32, Int32> = left\n    let same: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "dictionary equality is not available") {
		t.Fatalf("Compile stderr = %#v, want dictionary rejection", result.Stderr)
	}
}

func TestAdtEquality(t *testing.T) {
	result := compileSource("type Shape is union | Circle as r: Int32, end | Square as a: Int32, end end\nfun demo() do\n    let left: Shape = Shape.Circle(r = 1, )\n    let right: Shape = Shape.Circle(r = 1, )\n    let same: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"static bool hex_equal_hex_t_m3_app_Shape(const hex_t_m3_app_Shape *left, const hex_t_m3_app_Shape *right) {",
		"if ((*left).tag != (*right).tag) return false;",
		"if (!((*left).payload.Circle.hex_m_r == (*right).payload.Circle.hex_m_r)) return false;",
	} {
		if !strings.Contains(rootH(t, result), want) {
			t.Fatalf("modules/app.h = %q, want %q", rootH(t, result), want)
		}
	}
}

func TestUnionEqualityWithObjectMember(t *testing.T) {
	result := compileSource("type Point is struct x: Int32, end\nfun demo() do\n    let left: Point | Bool = Point(x = 1, )\n    let right: Point | Bool = Point(x = 1, )\n    let same: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "static bool hex_t_Bool_Point_equal(hex_t_Bool_Point left, hex_t_Bool_Point right) {") {
		t.Fatalf("modules/app.h = %q, want recursive union equality helper", rootH(t, result))
	}
}

func TestOrderingRejections(t *testing.T) {
	for _, testCase := range []struct {
		source string
		want   string
	}{
		{"fun demo() do\n    let left: Bool = true\n    let right: Bool = false\n    let bad: Bool = left < right\nend", "ordering is unavailable for Bool values"},
		{"fun demo() do\n    let mut value: Int32 = 1\n    let left: Ptr<Int32> = @value\n    let right: Ptr<Int32> = left\n    let bad: Bool = left < right\nend", "ordering is unavailable for Ptr<Int32> values"},
		{"type Point is struct x: Int32, end\nfun demo() do\n    let left: Point = Point(x = 1, )\n    let right: Point = left\n    let bad: Bool = left < right\nend", "ordering is unavailable for Point values"},
	} {
		result := compileSource(testCase.source)
		if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], testCase.want) {
			t.Fatalf("Compile(%q) stderr = %#v, want %q", testCase.source, result.Stderr, testCase.want)
		}
	}
}

func TestGenericEqualityRecheckedAtSpecialization(t *testing.T) {
	result := compileSource("fun same<T>(a: T, b: T): Bool do\n    return a == b\nend\nfun demo() do\n    let equal: Bool = same(1, 2)\n    let matched: Bool = same(\"a\", \"b\")\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The specialized String equality calls the extern helper from
	// hexal/string.h; the module header includes string.h.
	if !strings.Contains(rootH(t, result), "hex_equal_hex_string") && !strings.Contains(moduleFile(t, result, "hexal/string.h"), "hex_equal_hex_string") {
		t.Fatalf("no hex_equal_hex_string in module header or string.h: modules/app.h = %q, hexal/string.h = %q", rootH(t, result), moduleFile(t, result, "hexal/string.h"))
	}
}

func TestNilComparisonRulesPreserved(t *testing.T) {
	// == nil requires a union containing Nil. A plain pointer has no Nil
	// member, so the literal gate rejects the comparison.
	result := compileSource("fun demo() do\n    let nilSame: Bool = nil == nil\n    let mut value: Int32 = 1\n    let pointer: Ptr<Int32> = @value\n    let bad: Bool = pointer == nil\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "nil requires an expected union containing Nil") {
		t.Fatalf("Compile stderr = %#v, want standalone-nil diagnostic", result.Stderr)
	}
}

func TestListEqualityAccepted(t *testing.T) {
	result := compileSource("fun demo(h: Heap) do\n    let left: List<Int32> = List<Int32>(h)\n    defer left.free(h)\n    left.push(1)\n    let right: List<Int32> = left\n    let same: Bool = left == right\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Contains(rootH(t, result), "static bool hex_equal_hex_list_Int32(const hex_list_Int32 *left, const hex_list_Int32 *right) {") {
		t.Fatalf("modules/app.h re-emits the program-owned List equality helper")
	}
}

func TestManagedHandleEqualityRejected(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		source string
	}{
		{"Task", "fun worker(): Bool do\n    return true\nend\nfun f(h: Heap): Int32 | Error do\n    let task: Task<Bool> = try spawn worker()\n    let same: Bool = task == task\n    return 0\nend\n"},
		{"Channel", "fun f(h: Heap): Int32 | Error do\n    let channel: Channel<Int32> = try Channel<Int32>(h, 4)\n    let same: Bool = channel == channel\n    return 0\nend\n"},
		{"Mutex", "fun f(h: Heap): Int32 | Error do\n    let mutex: Mutex = try Mutex(h)\n    let same: Bool = mutex == mutex\n    return 0\nend\n"},
		{"Atomic", "let counter: Atomic<Int32> = Atomic<Int32>(0)\nlet same: Bool = counter == counter\n"},
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
	source := "let same: Bool = eos == eos\nlet different: Bool = eos != eos\n"
	result := assertCompiles(t, source)
	app := rootC(t, result)
	for _, want := range []string{"hex_v_same", "= true;", "hex_v_different", "= false;"} {
		if !strings.Contains(app, want) {
			t.Fatalf("modules/app.c = %q, want %q", app, want)
		}
	}
}
