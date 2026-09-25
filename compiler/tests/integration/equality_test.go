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

// Every package-level builtin field whose type is a constructed collection
// reads back through the compilation arena, so an ordinary List binding and
// the field compare with one canonical identity. The pre-seeded builtin
// structural union (working_directory) holds the same identity already.
func TestBuiltinAggregateFieldIdentityEquality(t *testing.T) {
	source := "import\n  Proc from std.process\nend\n" +
		"fun check(h: Heap): Bool do\n" +
		"    let arguments = List<String>(h)\n" +
		"    let options = Proc.ProcessOptions(\n" +
		"        program = \"x\",\n" +
		"        arguments = arguments,\n" +
		"        environment = Proc.Environment.Replace(values = List<Proc.EnvironmentVariable>(h)),\n" +
		"        working_directory = nil,\n" +
		"        input = Proc.ProcessStream.Pipe(),\n" +
		"        output = Proc.ProcessStream.Pipe(),\n" +
		"        error = Proc.ProcessStream.Ignore(),\n" +
		"    )\n" +
		"    let same_arguments: Bool = options.arguments == arguments\n" +
		"    let values = List<Proc.EnvironmentVariable>(h)\n" +
		"    let env = Proc.Environment.Replace(values = values)\n" +
		"    let same_values: Bool = match env is\n" +
		"    | Proc.Environment.Replace then env.values == values\n" +
		"    | else then false\n" +
		"    end\n" +
		"    let directory: String | Nil = nil\n" +
		"    let same_directory: Bool = options.working_directory == directory\n" +
		"    return same_arguments and same_values and same_directory\n" +
		"end\n"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	for _, want := range []string{
		"hex_equal_hex_list_String(hex_v_options.hex_m_arguments, hex_v_arguments)",
		"hex_equal_hex_list_EnvironmentVariable(hex_match_scrutinee_1.payload.Replace.hex_m_values, hex_v_values)",
		"hex_t_String_Nil_equal(hex_v_options.hex_m_working_directory, hex_v_directory)",
	} {
		if !strings.Contains(rootC(t, result), want) {
			t.Fatalf("modules/app.c = %q, want %q", rootC(t, result), want)
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
	result := compileSource("fun demo() do\n    let left: String = \"abc\"\n    let right: String = \"abd\"\n    let same: Bool = left == right\n    let less: Bool = left < right\n    let atMost: Bool = left <= right\n    let greater: Bool = left > right\n    let atLeast: Bool = left >= right\n    let a: String<8> = \"abc\"\n    let b: String<8> = \"abd\"\n    let inlineLess: Bool = a < b\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	// The one shared equality helper and the one shared ordering helper live in
	// hexal/string.h and hexal/string.c with external linkage, and take a byte
	// view, so they serve every text form.
	stringH := moduleFile(t, result, "hexal/string.h")
	stringC := moduleFile(t, result, "hexal/string.c")
	for _, want := range []string{
		"bool hex_equal_text(hex_text left, hex_text right);",
		"int hex_compare_text(hex_text left, hex_text right);",
	} {
		if !strings.Contains(stringH, want) {
			t.Fatalf("hexal/string.h = %q, want %q", stringH, want)
		}
	}
	for _, want := range []string{
		"bool hex_equal_text(hex_text left, hex_text right) {",
		"int hex_compare_text(hex_text left, hex_text right) {",
	} {
		if !strings.Contains(stringC, want) {
			t.Fatalf("hexal/string.c = %q, want %q", stringC, want)
		}
	}
	// The module file spells the comparisons calling the extern helpers.
	output := rootC(t, result) + rootH(t, result)
	for _, want := range []string{
		"hex_v_same = hex_equal_text(hex_text_heap(hex_v_left), hex_text_heap(hex_v_right));",
		"(hex_compare_text(hex_text_heap(hex_v_left), hex_text_heap(hex_v_right)) < 0)",
		"(hex_compare_text(hex_text_heap(hex_v_left), hex_text_heap(hex_v_right)) <= 0)",
		"(hex_compare_text(hex_text_heap(hex_v_left), hex_text_heap(hex_v_right)) > 0)",
		"(hex_compare_text(hex_text_heap(hex_v_left), hex_text_heap(hex_v_right)) >= 0)",
		"(hex_compare_text(hex_text_inline(&(hex_v_a)), hex_text_inline(&(hex_v_b))) < 0)",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated output = %q %q, want %q", rootC(t, result), rootH(t, result), want)
		}
	}
	// Equality compares length first, then one memcmp over the shared nonzero
	// length; ordering memcmp's the shorter nonzero length and falls back to
	// the length comparison.
	for _, want := range []string{
		"memcmp(left.data, right.data, left.length)",
		"memcmp(left.data, right.data, limit)",
	} {
		if !strings.Contains(stringC, want) {
			t.Fatalf("hexal/string.c lacks %q: %q", want, stringC)
		}
	}
	for _, forbidden := range []string{"hex_equal_hex_string", "hex_compare_hex_string", "hex_equal_hex_strand", "hex_compare_hex_strand"} {
		if strings.Contains(stringC, forbidden) || strings.Contains(stringH, forbidden) {
			t.Fatalf("generated output retains a per-type helper %q", forbidden)
		}
	}
}

// Every pairing of text forms compares bytewise in either operand order, and
// capacity never participates: equal bytes are equal in every form. Empty
// values skip the standard memory call safely, non-ASCII UTF-8 compares
// bytewise, and a maximum-length inline value is compared over its length.
func TestTextEqualityOrderingAcrossForms(t *testing.T) {
	source := "fun demo() do\n" +
		"    let emptyA: String = \"\"\n" +
		"    let emptyB: String<8> = \"\"\n" +
		"    let sameEmpty: Bool = emptyA == emptyB\n" +
		"    let prefix: String = \"caf\u00e9\"\n" +
		"    let longer: Bool = prefix < \"caf\u00e9!\"\n" +
		"    let full: String<64> = \"" + strings.Repeat("a", 64) + "\"\n" +
		"    let small: String<4> = \"aa\"\n" +
		"    let mixed: Bool = full == small\n" +
		"    let reversed: Bool = small < full\n" +
		"    let heap: String = \"aa\"\n" +
		"    let crossed: Bool = small == heap\n" +
		"    let crossedBack: Bool = heap != small\n" +
		"end"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	output := rootC(t, result) + rootH(t, result)
	for _, want := range []string{
		"hex_equal_text(hex_text_heap(hex_v_emptyA), hex_text_inline(&(hex_v_emptyB)))",
		"hex_equal_text(hex_text_inline(&(hex_v_full)), hex_text_inline(&(hex_v_small)))",
		"hex_compare_text(hex_text_inline(&(hex_v_small)), hex_text_inline(&(hex_v_full))) < 0",
		"hex_equal_text(hex_text_inline(&(hex_v_small)), hex_text_heap(hex_v_heap))",
		"(!hex_equal_text(hex_text_heap(hex_v_heap), hex_text_inline(&(hex_v_small))))",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated output lacks %q:\n%q\n%q", want, rootC(t, result), rootH(t, result))
		}
	}
	if strings.Contains(output, "memcmp(hex_v_") {
		t.Fatalf("a text comparison bypasses the shared helper")
	}
}

func TestInlineTextMemberEqualityUsesLogicalBytes(t *testing.T) {
	source := "type Label is struct tag: String<16>, end\nfun demo() do\n    let left: Label = Label(tag = \"a\")\n    let right: Label = Label(tag = \"a\")\n    let same: Bool = left == right\nend"
	result := compileSource(source)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	output := rootC(t, result) + rootH(t, result)
	for _, want := range []string{
		"if (!hex_equal_text(hex_text_inline(&((*left).hex_m_tag)), hex_text_inline(&((*right).hex_m_tag)))) return false;",
		"hex_equal_hex_t_m3_app_Label(&(hex_v_left), &(hex_v_right))",
	} {
		if !strings.Contains(output, want) {
			t.Fatalf("generated output lacks %q:\n%q\n%q", want, rootC(t, result), rootH(t, result))
		}
	}
}

// Text of any two forms compares; text against a non-text operand does not.
func TestTextEqualityRejectsNonTextOperand(t *testing.T) {
	result := compileSource("fun demo() do\n    let text: String = \"abc\"\n    let number: Int32 = 1\n    let bad: Bool = text == number\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "equality requires identical canonical non-numeric operand types") {
		t.Fatalf("Compile stderr = %#v, want strict operand-type diagnostic", result.Stderr)
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
		t.Fatalf("hexal/equality.h omits <string.h> for Error's text comparison:\n%s", equality)
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
	if !strings.Contains(rootH(t, result), "hex_equal_text") && !strings.Contains(moduleFile(t, result, "hexal/string.h"), "hex_equal_text") {
		t.Fatalf("no hex_equal_text in module header or string.h: modules/app.h = %q, hexal/string.h = %q", rootH(t, result), moduleFile(t, result, "hexal/string.h"))
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
