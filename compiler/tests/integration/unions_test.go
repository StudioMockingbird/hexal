package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestUnionAliasesNormalizeAndInject(t *testing.T) {
	result := compileSource("type Number = Int32 | Float64 mut value: Number := 1 value = 2.5")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "typedef struct hex_t_Int32_Float64") || !strings.Contains(rootC(t, result), "hex_v_value") {
		t.Fatalf("generated union output = H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionContextUsesWrittenCandidateOrder(t *testing.T) {
	result := compileSource("first: UInt8 | UInt16 := 7 second: Int64 | Int32 := 7")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected candidate-order source: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), ".tag = hex_tag_UInt8") || !strings.Contains(rootC(t, result), ".tag = hex_tag_Int64") {
		t.Fatalf("candidate tags missing from generated C: %q", rootC(t, result))
	}
}

func TestUnionWideningPreservesSourceEvaluation(t *testing.T) {
	result := compileSource("small: Int32 | Bool := true wide: Int32 | Bool | Nil := small")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected widening source: %v", result.Stderr)
	}
	if !strings.Contains(rootH(t, result), "hex_internal_widen_") || strings.Count(rootC(t, result), "hex_internal_widen_") != 1 {
		t.Fatalf("widening helper output = H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionIsNarrowsIfElseAndWhile(t *testing.T) {
	// The else arm narrows value to Nil, which is printable but cannot
	// initialize a standalone Nil binding.
	result := compileSource("value: Int32 | Float64 | Nil := 1 if value is Int32 then integer: Int32 := value elseif value != nil then floating: Float64 := value else print(value) end mut state: Int32 | Float64 := 1 while state is Int32 do current: Int32 := state state = 2 end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected flow narrowing source: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), ".payload.hex_m_") {
		t.Fatalf("narrowed payload missing from generated C: %q", rootC(t, result))
	}
}

func TestUnionNullTestsAndTruthiness(t *testing.T) {
	result := compileSource("value: Int32 | Bool | Nil := true present: Bool := value != nil if value then end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected Nil/truthiness source: %v", result.Stderr)
	}
	if !strings.Contains(hexalH(t, result), "#include <stddef.h>") || !strings.Contains(rootH(t, result), "_truthy") || !strings.Contains(rootC(t, result), ".tag !=") {
		t.Fatalf("null/truthiness output = H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionEqualityUsesTagsAndPayloads(t *testing.T) {
	result := compileSource("left: Int32 | Bool := true right: Bool | Int32 := false same: Bool := left == right")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected union equality source: %v", result.Stderr)
	}
	if !strings.Contains(rootH(t, result), "_equal(") || !strings.Contains(rootC(t, result), "_equal(") {
		t.Fatalf("union equality helper missing: H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestNullablePointerUnionKeepsNullNiche(t *testing.T) {
	result := compileSource("mut value: Int32 := 1 maybe: Ptr<Int32> | Nil := ref value if maybe != nil then result: Int32 := maybe.value end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected nullable pointer source: %v", result.Stderr)
	}
	if strings.Contains(rootH(t, result), "hex_t_") || !strings.Contains(rootC(t, result), "nullptr") {
		t.Fatalf("nullable pointer was tagged: H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionNestedPointerAndFunctionPositions(t *testing.T) {
	result := compileSource("fun identity(value: Int32 | Bool): Int32 | Bool do return value end mut value: Int32 | Bool := true slot: MutPtr<Int32 | Bool> := ref value result: Int32 | Bool := identity(value)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected nested/function union source: %v", result.Stderr)
	}
	if !strings.Contains(rootH(t, result), "hex_t_") || !strings.Contains(rootC(t, result), "hex_f_m3_app_identity") {
		t.Fatalf("nested/function union output = H:%q C:%q", rootH(t, result), rootC(t, result))
	}
}

func TestUnionDiagnosticsFailAtTheEarliestPhase(t *testing.T) {
	result := compileSource("value: UInt8 | UInt16 := missing + 1")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "unknown variable missing") {
		t.Fatalf("diagnostics = %#v, want earliest unknown-variable error", result.Stderr)
	}
}

func TestGeneratedUnionNamesAreDeterministic(t *testing.T) {
	source := "first: Int32 | Float64 := 1 second: Bool | Int32 := true"
	first := compileSource(source)
	second := compileSource(source)
	if first.ExitCode != compiler.ExitSuccess || second.ExitCode != compiler.ExitSuccess || rootC(t, first) != rootC(t, second) || rootH(t, first) != rootH(t, second) {
		t.Fatalf("repeated union output differs: first := %q/%q second=%q/%q", rootC(t, first), rootH(t, first), rootC(t, second), rootH(t, second))
	}
}

func TestSameUnionAcrossModulesProducesOneName(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module A = import \"./a\"\nmodule B = import \"./b\"\na_val: A.Result := true\nb_val: B.Result := 1\n",
		"a.hex":   "export type Result = Int32 | Bool\n",
		"b.hex":   "export type Result = Int32 | Bool\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected identical union across modules: %v", result.Stderr)
	}
	// The union name hex_t_Bool_Int32 is shared across both module imports.
	if strings.Count(result.Files["modules/app.h"], "typedef struct hex_t_Bool_Int32") != 1 {
		t.Fatalf("app.h must define shared union exactly once; got:\n%s", result.Files["modules/app.h"])
	}
}

func TestStructurallyDifferentModuleUnionsProduceDistinctNames(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nm_val: M.Point | Bool := true\ns_val: S.Point | Bool := true\n",
		"m.hex":   "export type Point = { x: Int32, }\n",
		"s.hex":   "export type Point = { x: Int32, }\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected module distinct union source: %v", result.Stderr)
	}
	// Both unions read the short member name "Point", so the first interner
	// keeps hex_t_Bool_Point and the distinct second union is suffixed.
	if !strings.Contains(result.Files["modules/app.h"], "typedef struct hex_t_Bool_Point {") || !strings.Contains(result.Files["modules/app.h"], "typedef struct hex_t_Bool_Point_0 {") {
		t.Fatalf("distinct module unions must have distinct names in app.h:\n%s", result.Files["modules/app.h"])
	}
}

func TestNestedUnionEncodingIntegration(t *testing.T) {
	result := compileSource("type Inner = Int32 | Bool\ntype Outer = Inner | Nil\nval: Outer := nil\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected nested union source: %v", result.Stderr)
	}
	if !strings.Contains(rootH(t, result), "hex_t_Bool_Int32_Nil") {
		t.Fatalf("flattened union name not found in header:\n%s", rootH(t, result))
	}
}

// Rune and UInt32 share the C spelling uint32_t, so their unions must be
// told apart by the registry, not by member C spellings: each union is
// defined exactly once per translation unit.
func TestRuneAndUInt32UnionsStayDistinct(t *testing.T) {
	result := compileSource("a: Rune | Nil := nil\nb: UInt32 | Nil := nil")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected Rune/UInt32 union source: %v", result.Stderr)
	}
	rootH := rootH(t, result)
	if strings.Count(rootH, "typedef struct hex_t_Rune_Nil") != 1 || strings.Count(rootH, "typedef struct hex_t_UInt32_Nil") != 1 {
		t.Fatalf("Rune and UInt32 unions must be distinct and defined once:\n%s", rootH)
	}
}

// A composed member spells its sanitized Hexal name; the wrapper name must
// stay a plain C identifier.
func TestComposedUnionMemberSpellingIsIdentifierSafe(t *testing.T) {
	result := compileSource("value: List<Int32> | Nil := nil")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected composed union member: %v", result.Stderr)
	}
	rootH := rootH(t, result)
	if !strings.Contains(rootH, "typedef struct hex_t_List_Int32__Nil") {
		t.Fatalf("composed union name not found in header:\n%s", rootH)
	}
	if strings.Contains(rootH, "typedef struct hex_t_List_Int32__Nil>") {
		t.Fatalf("composed union name contains a non-identifier character:\n%s", rootH)
	}
}

// Case is part of a Hexal type name, so a lowercasing scheme would collapse
// distinct unions; both spellings must survive side by side.
func TestCaseDistinctUnionMembersStayDistinct(t *testing.T) {
	result := compileSource("type Foo = { a: Int32, }\ntype foo = { b: Int32, }\nu: Foo | Nil := nil\nv: foo | Nil := nil")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected case-distinct union source: %v", result.Stderr)
	}
	rootH := rootH(t, result)
	if strings.Count(rootH, "typedef struct hex_t_Foo_Nil") != 1 || strings.Count(rootH, "typedef struct hex_t_foo_Nil") != 1 {
		t.Fatalf("case-distinct unions must be distinct and defined once:\n%s", rootH)
	}
}

// The program-wide discriminant enum lives in hexal.h, its members spelled
// exactly as every reference resolves them: hex_tag_ plus the registry label.
func TestProgramWideTagEnumMembersMatchReferenceSpellings(t *testing.T) {
	result := compileSource("value: Int32 | Nil := nil")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected union source: %v", result.Stderr)
	}
	hexalH := hexalH(t, result)
	for _, want := range []string{
		"typedef enum hex_tag {",
		"    hex_tag_Int32,",
		"    hex_tag_Nil,",
		"} hex_tag;",
	} {
		if !strings.Contains(hexalH, want) {
			t.Fatalf("hexal.h missing %q:\n%s", want, hexalH)
		}
	}
}

// A program without unions, ADTs, or Error must not emit the shared enum at
// all; the registry stays empty and hexal.h stays minimal.
func TestTagFreeProgramHasNoTagEnum(t *testing.T) {
	result := compileSource("value: Int32 := 1")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected tag-free source: %v", result.Stderr)
	}
	if strings.Contains(hexalH(t, result), "hex_tag") {
		t.Fatalf("tag-free hexal.h must not mention hex_tag:\n%s", hexalH(t, result))
	}
}

// Two distinct identities whose labels collide resolve in identity order:
// the first keeps the base, the later ones append _0, never _2.
func TestTagCollisionSuffixesStartAtZero(t *testing.T) {
	result := compileSource("type Direction = | North | East type Direction_North = { a: Int32 } heading: Direction := Direction.North marker: Direction_North := Direction_North { a = 1 } u: Direction_North | Nil := marker")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected tag-collision source: %v", result.Stderr)
	}
	hexalH := hexalH(t, result)
	if !strings.Contains(hexalH, "hex_tag_m3_app_Direction_North") || !strings.Contains(hexalH, "hex_tag_m3_app_Direction_North_0") {
		t.Fatalf("colliding tags must keep the base and suffix the second:\n%s", hexalH)
	}
	if strings.Contains(hexalH, "_2") {
		t.Fatalf("two colliding identities must never reach the _2 suffix:\n%s", hexalH)
	}
}

// Int32 | Nil keeps the corrected wrapper stem, its members resolve to the
// program-wide hex_tag constants, and Nil has no payload field.
func TestInt32NilWrapperUsesCorrectedStem(t *testing.T) {
	result := compileSource("value: Int32 | Nil := nil")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected Int32 | Nil source: %v", result.Stderr)
	}
	rootH := rootH(t, result)
	for _, want := range []string{
		"typedef struct hex_t_Int32_Nil",
		"hex_tag tag;",
		"case hex_tag_Int32:",
		"case hex_tag_Nil:",
		"int32_t hex_m_Int32;",
	} {
		if !strings.Contains(rootH, want) {
			t.Fatalf("wrapper stem missing %q from header:\n%s", want, rootH)
		}
	}
	if strings.Contains(rootH, "member_") {
		t.Fatalf("Nil must not carry a payload field:\n%s", rootH)
	}
}

// Two unions whose sanitized member name sequences spell one string both
// compile; the registry suffixes the second, and each is defined once.
func TestSanitizedMemberCollisionsReceiveSuffixedNames(t *testing.T) {
	result := compileSource("type Int32_Nil = { a: Int32, }\ntype Nil_Foo = { b: Int32, }\ntype Foo = { c: Int32, }\nfirst: Int32_Nil | Foo := Int32_Nil { a = 1 }\nsecond: Int32 | Nil_Foo := Nil_Foo { b = 1 }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected colliding-name union source: %v", result.Stderr)
	}
	rootH := rootH(t, result)
	if strings.Count(rootH, "typedef struct hex_t_Int32_Nil_Foo {") != 1 || strings.Count(rootH, "typedef struct hex_t_Int32_Nil_Foo_0 {") != 1 {
		t.Fatalf("colliding unions must receive distinct names, each defined once:\n%s", rootH)
	}
}

// A union base that spells a reachable nominal C name must yield: the
// nominal keeps the base regardless of traversal order, the union is
// suffixed.
func TestUnionBaseCollidingWithNominalNameIsSuffixed(t *testing.T) {
	result := compileSource("type m3_app = { a: Int32, }\ntype Point = { x: Int32, }\nu: m3_app | Point := m3_app { a = 1 }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected nominal-colliding union source: %v", result.Stderr)
	}
	rootH := rootH(t, result)
	if !strings.Contains(rootH, "struct hex_t_m3_app_Point {") {
		t.Fatalf("object Point lost its fixed nominal name:\n%s", rootH)
	}
	if strings.Contains(rootH, "typedef struct hex_t_m3_app_Point {") || !strings.Contains(rootH, "typedef struct hex_t_m3_app_Point_0 {") {
		t.Fatalf("union must be suffixed and never claim the nominal name:\n%s", rootH)
	}
}

// Two imported nominal members with identical short names and source
// coordinates spell one structural union; both written orders intern to it,
// and the same-type assignment emits no widening helper.
func TestReversedImportedObjectUnionsInternTogether(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\na: M.Point | S.Point := M.make()\nb: S.Point | M.Point := a\n",
		"m.hex":   "export type Point = { x: Int32, }\nexport fun make(): Point do\n    return Point { x = 1, }\nend\n",
		"s.hex":   "export type Point = { x: Int32, }\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected reversed object union source: %v", result.Stderr)
	}
	rootH := result.Files["modules/app.h"]
	if strings.Count(rootH, "typedef struct hex_t_Point_Point") != 1 {
		t.Fatalf("reversed object unions must share one interned type:\n%s", rootH)
	}
	if strings.Contains(rootH, "hex_internal_widen_") {
		t.Fatalf("same-type assignment must not widen:\n%s", rootH)
	}
}

// The same structural union over two same-named imported ADTs, reversed,
// also interns to one type with no widening; the ADT C-name qualification
// alone cannot satisfy this case because ADTs sort by short name.
func TestReversedImportedADTUnionsInternTogether(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\na: M.Shape | S.Shape := M.make()\nb: S.Shape | M.Shape := a\n",
		"m.hex":   "export type Shape = | Circle as { r: Int32 } | Square as { a: Int32 }\nexport fun make(): Shape do\n    return Shape.Circle { r = 1 }\nend\n",
		"s.hex":   "export type Shape = | Circle as { r: Int32 } | Square as { a: Int32 }\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected reversed ADT union source: %v", result.Stderr)
	}
	rootH := result.Files["modules/app.h"]
	if strings.Count(rootH, "typedef struct hex_t_Shape_Shape") != 1 {
		t.Fatalf("reversed ADT unions must share one interned type:\n%s", rootH)
	}
	if strings.Contains(rootH, "hex_internal_widen_") {
		t.Fatalf("same-type assignment must not widen:\n%s", rootH)
	}
}

// Pointer members sort by the element's short name only, so the pointer
// branch of the display key needs the same canonical tie-break.
func TestReversedImportedPointerUnionsInternTogether(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nmut p: M.Point := M.make()\na: Ptr<M.Point> | Ptr<S.Point> := ref p\nb: Ptr<S.Point> | Ptr<M.Point> := a\n",
		"m.hex":   "export type Point = { x: Int32, }\nexport fun make(): Point do\n    return Point { x = 1, }\nend\n",
		"s.hex":   "export type Point = { x: Int32, }\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected reversed pointer union source: %v", result.Stderr)
	}
	rootH := result.Files["modules/app.h"]
	if strings.Count(rootH, "typedef struct hex_t_Ptr_Point__Ptr_Point_") != 1 {
		t.Fatalf("reversed pointer unions must share one interned type:\n%s", rootH)
	}
	if strings.Contains(rootH, "hex_internal_widen_") {
		t.Fatalf("same-type assignment must not widen:\n%s", rootH)
	}
}

// A union crossing a module boundary as a function result is spelled
// identically in every header that declares the function against it.
func TestCrossModuleUnionResultSpelledIdentically(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nvalue: M.Maybe := M.make()\n",
		"m.hex":   "export type Maybe = Int32 | Nil\nexport fun make(): Maybe do\n    return nil\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected cross-module union result: %v", result.Stderr)
	}
	for _, file := range []string{"modules/app.h", "modules/m.h"} {
		if strings.Count(result.Files[file], "typedef struct hex_t_Int32_Nil") != 1 {
			t.Fatalf("%s must define the crossing union exactly once:\n%s", file, result.Files[file])
		}
	}
}

// The canonical Size | EoS narrowing and injection path into a fallible
// Size | Error function compiles, and a contextual integer literal returned
// from inside if, elseif, else, while, for, and match-arm scopes selects the
// Size member through the enclosing function's result use.
func TestContextualReturnsInsideBlocksInjectSize(t *testing.T) {
	prefix := "fun source(): Size | EoS do\n    return eos\nend\n"
	cases := map[string]string{
		"if": prefix + "fun demo(flag: Bool): Size | Error do\n" +
			"    outcome: Size | EoS := source()\n" +
			"    if outcome is EoS then\n        return 0\n    end\n" +
			"    return outcome\nend\n",
		"elseif-else": "fun demo(flag: Bool, other: Bool): Size | Error do\n" +
			"    if flag then\n        return 0\n" +
			"    elseif other then\n        return 1\n" +
			"    else\n        return 2\n    end\nend\n",
		"while": "fun demo(): Size | Error do\n" +
			"    mut guard: Bool := true\n" +
			"    while guard do\n        return 0\n    end\n" +
			"    return 1\nend\n",
		"for": "fun demo(): Size | Error do\n" +
			"    flags: Array<Bool, 2> := [true, false]\n" +
			"    for flag in flags do\n        return 0\n    end\n" +
			"    return 1\nend\n",
		"match-arm": "fun demo(flag: Bool): Size | Error do\n" +
			"    return match flag | true then 0 | false then 1 end\nend\n",
	}
	for name, source := range cases {
		caller := "called: Size | Error := demo(true)\n"
		if name == "while" || name == "for" {
			caller = "called: Size | Error := demo()\n"
		}
		if name == "elseif-else" {
			caller = "called: Size | Error := demo(true, true)\n"
		}
		result := compileSource(source + caller)
		if result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("%s case rejected: %v", name, result.Stderr)
		}
		if strings.Contains(strings.Join(result.Stderr, "\n"), "Unknown Error") {
			t.Fatalf("%s case produced an Unknown Error: %v", name, result.Stderr)
		}
		if !strings.Contains(rootC(t, result), ".tag = hex_tag_Size") {
			t.Fatalf("%s case must inject the literal as Size:\n%s", name, rootC(t, result))
		}
	}
}

// The recorded narrowed-union defect shape stays fixed: a narrowed Size read
// returned into Size | Error carries valid injection metadata.
func TestNarrowedEoSReturnPathProducesValidInjection(t *testing.T) {
	result := compileSource("fun source(): Size | EoS do\n    return eos\nend\n" +
		"fun demo(): Size | Error do\n" +
		"    outcome: Size | EoS := source()\n" +
		"    if outcome is EoS then\n        return 0\n    end\n" +
		"    return outcome\nend\n" +
		"called: Size | Error := demo()\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("narrowed return path rejected: %v", result.Stderr)
	}
	if !strings.Contains(rootC(t, result), "hex_t_Error_Size") || !strings.Contains(rootC(t, result), ".tag = hex_tag_Size") {
		t.Fatalf("generated C must carry the Size member of the result union:\n%s", rootC(t, result))
	}
}
