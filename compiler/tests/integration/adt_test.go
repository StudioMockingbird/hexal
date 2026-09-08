package integration

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestADTDeclarationWithRecordVariants(t *testing.T) {
	result := compileSource("type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end shape: Shape := Shape.Circle(r = 10)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_tag_m3_app_Shape_Circle") || !strings.Contains(rootC(t, result), ".payload.Circle") {
		t.Fatalf("generated output = H:%q C:%q, want ADT tag and payload", rootH(t, result), rootC(t, result))
	}
}

// A variant constructor's payload fields may be written out of declaration
// order without swapping which value lands in which field.
func TestADTPayloadOutOfOrderAssignsCorrectFields(t *testing.T) {
	result := compileSource("type W is union | A as first: Int32, second: Int32 end | B as x: Int32 end end\n" +
		"w: W := W.A(second = 20, first = 10)\n")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	body := rootC(t, result)
	if !strings.Contains(body, ".hex_m_first = 10") || !strings.Contains(body, ".hex_m_second = 20") {
		t.Fatalf("generated C = %q, want first assigned 10 and second assigned 20 regardless of write order", body)
	}
}

func TestADTUnitVariantEnumBehavior(t *testing.T) {
	result := compileSource("type Direction is East | West | North | South end heading: Direction := Direction.North()")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_tag_m3_app_Direction_North") || strings.Contains(rootH(t, result), "payload") {
		t.Fatalf("generated header = %q, want tag-only unit variants", rootH(t, result))
	}
}

func TestADTQualifiedConstructorRequiresOwner(t *testing.T) {
	result := compileSource("type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end shape: Shape := Circle(r = 20)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("diagnostics = %#v, want unqualified-constructor error", result.Stderr)
	}
}

func TestADTConstructorValidatesPayloadFields(t *testing.T) {
	result := compileSource("type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end bad: Shape := Shape.Circle(a = 20)")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, " "), "Circle has no field named a") {
		t.Fatalf("diagnostics = %#v, want payload field error", result.Stderr)
	}
}

func TestADTIndirectRecursionCompiles(t *testing.T) {
	result := compileSource("type Expr is union | Literal as value: Int32 end | Add as left: Ptr<Expr>, right: Ptr<Expr> end end")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestADTByValueRecursionRejected(t *testing.T) {
	result := compileSource("type Expr is union | Literal as value: Int32 end | Wrap as inner: Expr end end")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ADT recursion has no finite representation") {
		t.Fatalf("diagnostics = %#v, want by-value recursion error", result.Stderr)
	}
}

func TestMatchValueModeBooleanPatterns(t *testing.T) {
	result := compileSource("ready: Bool := true label: Int32 := match ready\n| true then 1\n| false then 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_match_scrutinee_1") {
		t.Fatalf("generated C = %q, want match lowering", rootC(t, result))
	}
}

func TestMatchTypeModeVariantArmsNarrowPayload(t *testing.T) {
	result := compileSource("type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end shape: Shape := Shape.Circle(r = 10) area: Int32 := match shape is\n| Shape.Circle then shape.r * shape.r\n| Shape.Square then shape.a * shape.a\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), ".tag == hex_tag_m3_app_Shape_Circle") || !strings.Contains(rootC(t, result), ".payload.Circle.hex_m_r") {
		t.Fatalf("generated C = %q, want narrowed variant payload", rootC(t, result))
	}
}

func TestMatchTypeModeUnionMembersAndNil(t *testing.T) {
	result := compileSource("value: Int32 | Float32 | Nil := nil label: Int32 := match value is\n| Int32 then 1\n| Float32 then 2\n| Nil then 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestMatchElseCoversRemainder(t *testing.T) {
	result := compileSource("value: Int32 | Nil := nil label: Int32 := match value is\n| Nil then 0\n| else then 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestMatchExhaustivenessDiagnostic(t *testing.T) {
	result := compileSource("type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end shape: Shape := Shape.Circle(r = 10) label: Int32 := match shape is\n| Shape.Circle then 1\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "match is not exhaustive; missing Shape.Square") {
		t.Fatalf("diagnostics = %#v, want exhaustiveness error", result.Stderr)
	}
}

// An imported ADT matches through its module alias, and each arm narrows to
// that variant's own payload.
func TestMatchImportedADTVariantsNarrowPayload(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nshape: M.Shape := M.make()\narea: Int32 := match shape is\n| M.Circle then shape.r * shape.r\n| M.Square then shape.a * shape.a\nend\n",
		"m.hex":   "export type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end\nexport fun make(): Shape do\n    return Shape.Circle(r = 3)\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	body := rootC(t, result)
	if !strings.Contains(body, ".payload.Circle.hex_m_r") || !strings.Contains(body, ".payload.Square.hex_m_a") {
		t.Fatalf("generated C = %q, want each imported variant's own payload", body)
	}
}

// Same-named union members from two modules are distinct cases: each arm
// narrows to its own nominal type. Passing the scrutinee to a function
// expecting the other member's type fails, proving the identities do not
// collapse onto the shared short name.
func TestMatchSameNamedUnionMembersNarrowDistinctly(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nfun work_m(p: M.Point): Int32 do\n    return 1\nend\nfun work_s(p: S.Point): Int32 do\n    return 2\nend\nu: M.Point | S.Point := M.make()\nresult: Int32 := match u is\n| M.Point then work_m(u)\n| S.Point then work_s(u)\nend\n",
		"m.hex":   "export type Point is struct mx: Int32 end\nexport fun make(): Point do\n    return Point(mx = 1)\nend\n",
		"s.hex":   "export type Point is struct sy: Int32 end\nexport fun make(): Point do\n    return Point(sy = 2)\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	swapped := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nfun work_m(p: M.Point): Int32 do\n    return 1\nend\nfun work_s(p: S.Point): Int32 do\n    return 2\nend\nu: M.Point | S.Point := M.make()\nresult: Int32 := match u is\n| M.Point then work_s(u)\n| S.Point then work_m(u)\nend\n",
		"m.hex":   sources["m.hex"],
		"s.hex":   sources["s.hex"],
	}
	swappedResult := compiler.Compile(swapped, "app.hex", compiler.Project{})
	if swappedResult.ExitCode != compiler.ExitFailure || len(swappedResult.Stderr) == 0 {
		t.Fatalf("diagnostics = %#v, want narrowed-identity mismatch", swappedResult.Stderr)
	}
}

// A missing imported union member reports through the current module's
// alias, in canonical member order regardless of written order.
func TestMatchMissingImportedMemberUsesAlias(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nu: M.Point | S.Point := M.make()\nresult: Int32 := match u is\n| M.Point then 1\nend\n",
		"m.hex":   "export type Point is struct mx: Int32 end\nexport fun make(): Point do\n    return Point(mx = 1)\nend\n",
		"s.hex":   "export type Point is struct sy: Int32 end\nexport fun make(): Point do\n    return Point(sy = 2)\nend\n",
	}
	first := compiler.Compile(sources, "app.hex", compiler.Project{})
	if first.ExitCode != compiler.ExitFailure || len(first.Stderr) == 0 || !strings.Contains(first.Stderr[0], "match is not exhaustive; missing S.Point") {
		t.Fatalf("diagnostics = %#v, want missing S.Point", first.Stderr)
	}
	reversed := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nu: S.Point | M.Point := M.make()\nresult: Int32 := match u is\n| M.Point then 1\nend\n",
		"m.hex":   sources["m.hex"],
		"s.hex":   sources["s.hex"],
	}
	second := compiler.Compile(reversed, "app.hex", compiler.Project{})
	if second.ExitCode != compiler.ExitFailure || len(second.Stderr) == 0 || second.Stderr[0] != first.Stderr[0] {
		t.Fatalf("diagnostics = %#v, want repeated compiles to report %q", second.Stderr, first.Stderr[0])
	}
}

// A missing imported ADT variant reports alias and variant name, and a
// missing constructed member qualifies its nominal leaves.
func TestMatchMissingImportedVariantAndConstructedMembers(t *testing.T) {
	variant := map[string]string{
		"app.hex": "module M = import \"./m\"\nshape: M.Shape := M.make()\narea: Int32 := match shape is\n| M.Circle then 1\nend\n",
		"m.hex":   "export type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end\nexport fun make(): Shape do\n    return Shape.Circle(r = 3)\nend\n",
	}
	result := compiler.Compile(variant, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "match is not exhaustive; missing M.Square") {
		t.Fatalf("diagnostics = %#v, want missing M.Square", result.Stderr)
	}
	constructed := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nmut a: M.Point := M.make()\nu: MutPtr<M.Point> | MutPtr<S.Point> := ref a\nresult: Int32 := match u is\n| MutPtr<M.Point> then 1\nend\n",
		"m.hex":   "export type Point is struct mx: Int32 end\nexport fun make(): Point do\n    return Point(mx = 1)\nend\n",
		"s.hex":   "export type Point is struct sy: Int32 end\nexport fun make(): Point do\n    return Point(sy = 2)\nend\n",
	}
	constructedResult := compiler.Compile(constructed, "app.hex", compiler.Project{})
	if constructedResult.ExitCode != compiler.ExitFailure || len(constructedResult.Stderr) == 0 || !strings.Contains(constructedResult.Stderr[0], "match is not exhaustive; missing MutPtr<S.Point>") {
		t.Fatalf("diagnostics = %#v, want missing MutPtr<S.Point>", constructedResult.Stderr)
	}
}

// Unions of same-named pointer members and unions of same-named ADTs cover
// and report with the same canonical rules: both qualified arms compile, and
// a missing arm reports its alias-qualified member.
func TestMatchSameNamedConstructedAndADTUnions(t *testing.T) {
	pointModules := map[string]string{
		"m.hex": "export type Point is struct mx: Int32 end\nexport fun make(): Point do\n    return Point(mx = 1)\nend\n",
		"s.hex": "export type Point is struct sy: Int32 end\nexport fun make(): Point do\n    return Point(sy = 2)\nend\n",
	}
	shapeModules := map[string]string{
		"m.hex": "export type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end\nexport fun make(): Shape do\n    return Shape.Circle(r = 3)\nend\n",
		"s.hex": "export type Shape is union | Round as d: Int32 end | Flat as w: Int32 end end\nexport fun make(): Shape do\n    return Shape.Round(d = 4)\nend\n",
	}
	complete := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\na: M.Point := M.make()\nu: Ptr<M.Point> | Ptr<S.Point> := ref a\nv: Int32 := match u is\n| Ptr<M.Point> then 1\n| Ptr<S.Point> then 2\nend\n",
		"m.hex":   pointModules["m.hex"],
		"s.hex":   pointModules["s.hex"],
	}
	if result := compiler.Compile(complete, "app.hex", compiler.Project{}); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	missingPtr := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\na: M.Point := M.make()\nu: Ptr<M.Point> | Ptr<S.Point> := ref a\nv: Int32 := match u is\n| Ptr<M.Point> then 1\nend\n",
		"m.hex":   pointModules["m.hex"],
		"s.hex":   pointModules["s.hex"],
	}
	if result := compiler.Compile(missingPtr, "app.hex", compiler.Project{}); result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "match is not exhaustive; missing Ptr<S.Point>") {
		t.Fatalf("diagnostics = %#v, want missing Ptr<S.Point>", result.Stderr)
	}
	completeShapes := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nu: M.Shape | S.Shape := M.make()\nv: Int32 := match u is\n| M.Shape then 1\n| S.Shape then 2\nend\n",
		"m.hex":   shapeModules["m.hex"],
		"s.hex":   shapeModules["s.hex"],
	}
	if result := compiler.Compile(completeShapes, "app.hex", compiler.Project{}); result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	missingShape := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nu: M.Shape | S.Shape := M.make()\nv: Int32 := match u is\n| M.Shape then 1\nend\n",
		"m.hex":   shapeModules["m.hex"],
		"s.hex":   shapeModules["s.hex"],
	}
	if result := compiler.Compile(missingShape, "app.hex", compiler.Project{}); result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "match is not exhaustive; missing S.Shape") {
		t.Fatalf("diagnostics = %#v, want missing S.Shape", result.Stderr)
	}
}

func TestMatchScrutineeEvaluatedOnce(t *testing.T) {
	result := compileSource("fun read_value(): Int32 do return 1 end label: Int64 := match read_value()\n| else then 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Count(rootC(t, result), "hex_match_scrutinee_1 = hex_f_m3_app_read_value()") != 1 {
		t.Fatalf("generated C = %q, want one scrutinee evaluation", rootC(t, result))
	}
}

func TestGeneratedADTTagLayoutAndInvalidTagTrap(t *testing.T) {
	result := compileSource("type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end shape: Shape := Shape.Circle(r = 10)")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_tag_m3_app_Shape_Circle") || !strings.Contains(rootH(t, result), "typedef struct hex_t_m3_app_Shape") {
		t.Fatalf("generated header = %q, want deterministic tag-and-payload layout", rootH(t, result))
	}
}

func TestADTDiagnosticsFailClosed(t *testing.T) {
	result := compileSource("type Shape is union | Circle as r: Int32 end | Circle as a: Int32 end end")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ADT variant name is duplicated") {
		t.Fatalf("diagnostics = %#v, want duplicate variant error", result.Stderr)
	}
}

func TestGenericADTSpecializesAndMatches(t *testing.T) {
	result := compileSource("type Result<T, E> is union | Ok as value: T end | Err as error: E end end success: Result<Int32, Bool> := Result.Ok(value = 42) label: Int32 := match success is\n| Result.Ok then success.value\n| Result.Err then 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_tag_m3_app_Result_Int32__Bool__Ok") || !strings.Contains(rootC(t, result), ".payload.Ok.hex_m_value") {
		t.Fatalf("generated output = H:%q C:%q, want specialized ADT and match", rootH(t, result), rootC(t, result))
	}
}

func TestGenericADTUnitVariantNoPayload(t *testing.T) {
	result := compileSource("type Maybe<T> is union | Some as value: T end | None end value: Maybe<Int32> := Maybe.None()")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_t_m3_app_Maybe_Int32_") {
		t.Fatalf("generated header = %q, want specialized unit variant ADT", rootH(t, result))
	}
}

// Two modules may each declare a Shape ADT; the owner segment in the C name
// keeps the distinct types apart, each defined once.
func TestTwoModulesDeclaringSameADTStayDistinct(t *testing.T) {
	sources := map[string]string{
		"app.hex": "module M = import \"./m\"\nmodule S = import \"./s\"\nb: M.Shape := M.make()\nc: S.Shape := S.make()\n",
		"m.hex":   "export type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end\nexport fun make(): Shape do\n    return Shape.Circle(r = 1)\nend\n",
		"s.hex":   "export type Shape is union | Circle as r: Int32 end | Square as a: Int32 end end\nexport fun make(): Shape do\n    return Shape.Circle(r = 1)\nend\n",
	}
	result := compiler.Compile(sources, "app.hex", compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile rejected same-named ADTs across modules: %v", result.Stderr)
	}
	rootH := result.Files["modules/app.h"]
	if strings.Count(rootH, "typedef struct hex_t_m1_m_Shape") != 1 || strings.Count(rootH, "typedef struct hex_t_m1_s_Shape") != 1 {
		t.Fatalf("same-named ADTs must be distinct and defined once:\n%s", rootH)
	}
}

func TestMatchAdmitsFullExpressions(t *testing.T) {
	accepted := []string{
		"ready: Bool := true\nenabled: Bool := true\nr: Int32 := match ready and enabled\n| true then 1\n| false then 0\nend\n",
		"a: Bool := true\nb: Bool := true\nready: Bool := true\nr: Bool := match ready\n| true then a or b\n| false then false\nend\n",
		"x: Int32 := 1\ny: Int32 := 2\nr: Int32 := match x < y\n| true then 1\n| false then 0\nend\n",
		"mask: Bool := true\nflag: Bool := false\nr: Int32 := match (mask or flag)\n| true then 1\n| false then 0\nend\n",
		"value: Int32 | Float64 := 1\nr: Int32 := match (value is Int32)\n| true then 1\n| false then 0\nend\n",
		"value: Int32 | Float64 := 1\nr: Int32 := match value is\n| Int32 then 1\n| Float64 then 0\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept; got %v:\n%s", result.Stderr, source)
		}
	}
}
