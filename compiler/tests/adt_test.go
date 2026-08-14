package tests

import (
	"hexal/compiler"
	"strings"
	"testing"
)

func TestADTDeclarationWithRecordVariants(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_Shape_tag") || !strings.Contains(rootC(t, result), ".payload.Circle") {
		t.Fatalf("generated output = H:%q C:%q, want ADT tag and payload", rootH(t, result), rootC(t, result))
	}
}

func TestADTUnitVariantEnumBehavior(t *testing.T) {
	result := compileSource("type Direction = | East | West | North | South heading: Direction = Direction.North")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_Direction_North") || strings.Contains(rootH(t, result), "payload") {
		t.Fatalf("generated header = %q, want tag-only unit variants", rootH(t, result))
	}
}

func TestADTQualifiedConstructorRequiresOwner(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Circle { r = 20 }")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("diagnostics = %#v, want unqualified-constructor error", result.Stderr)
	}
}

func TestADTConstructorValidatesPayloadFields(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } bad: Shape = Shape.Circle { a = 20 }")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, " "), "Circle has no field named a") {
		t.Fatalf("diagnostics = %#v, want payload field error", result.Stderr)
	}
}

func TestADTIndirectRecursionCompiles(t *testing.T) {
	result := compileSource("type Expr = | Literal as { value: Int32 } | Add as { left: Ptr<Expr>, right: Ptr<Expr> }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestADTByValueRecursionRejected(t *testing.T) {
	result := compileSource("type Expr = | Literal as { value: Int32 } | Wrap as { inner: Expr }")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ADT recursion has no finite representation") {
		t.Fatalf("diagnostics = %#v, want by-value recursion error", result.Stderr)
	}
}

func TestMatchValueModeBooleanPatterns(t *testing.T) {
	result := compileSource("ready: Bool = true label: Int32 = match ready\n| true then 1\n| false then 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), "hex_match_scrutinee_1") {
		t.Fatalf("generated C = %q, want match lowering", rootC(t, result))
	}
}

func TestMatchTypeModeVariantArmsNarrowPayload(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } area: Int32 = match shape is\n| Shape.Circle then shape.r * shape.r\n| Shape.Square then shape.a * shape.a\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootC(t, result), ".tag == hex_Shape_Circle") || !strings.Contains(rootC(t, result), ".payload.Circle.hex_m_r") {
		t.Fatalf("generated C = %q, want narrowed variant payload", rootC(t, result))
	}
}

func TestMatchTypeModeUnionMembersAndNil(t *testing.T) {
	result := compileSource("value: Int32 | Float32 | Nil = nil label: Int32 = match value is\n| Int32 then 1\n| Float32 then 2\n| Nil then 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestMatchElseCoversRemainder(t *testing.T) {
	result := compileSource("value: Int32 | Nil = nil label: Int32 = match value is\n| Nil then 0\n| else then 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
}

func TestMatchExhaustivenessDiagnostic(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } label: Int32 = match shape is\n| Shape.Circle then 1\nend")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "match is not exhaustive; missing Shape.Square") {
		t.Fatalf("diagnostics = %#v, want exhaustiveness error", result.Stderr)
	}
}

func TestMatchScrutineeEvaluatedOnce(t *testing.T) {
	result := compileSource("fun read_value(): Int32 return 1 end label: Int64 = match read_value()\n| else then 1\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if strings.Count(rootC(t, result), "hex_match_scrutinee_1 = hex_f_read_value()") != 1 {
		t.Fatalf("generated C = %q, want one scrutinee evaluation", rootC(t, result))
	}
}

func TestGeneratedADTTagLayoutAndInvalidTagTrap(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 }")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_Shape_tag") || !strings.Contains(rootH(t, result), "typedef struct hex_Shape") {
		t.Fatalf("generated header = %q, want deterministic tag-and-payload layout", rootH(t, result))
	}
}

func TestADTDiagnosticsFailClosed(t *testing.T) {
	result := compileSource("type Shape = | Circle as { r: Int32 } | Circle as { a: Int32 }")
	if result.ExitCode != compiler.ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ADT variant name is duplicated") {
		t.Fatalf("diagnostics = %#v, want duplicate variant error", result.Stderr)
	}
}

func TestGenericADTSpecializesAndMatches(t *testing.T) {
	result := compileSource("type Result<T, E> = | Ok as { value: T } | Err as { error: E } success: Result<Int32, Bool> = Result.Ok { value = 42 } label: Int32 = match success is\n| Result.Ok then success.value\n| Result.Err then 0\nend")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_Result_Int32__Bool__tag") || !strings.Contains(rootC(t, result), ".payload.Ok.hex_m_value") {
		t.Fatalf("generated output = H:%q C:%q, want specialized ADT and match", rootH(t, result), rootC(t, result))
	}
}

func TestGenericADTUnitVariantNoPayload(t *testing.T) {
	result := compileSource("type Maybe<T> = | Some as { value: T } | None value: Maybe<Int32> = Maybe.None")
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, compiler.ExitSuccess)
	}
	if !strings.Contains(rootH(t, result), "hex_Maybe_Int32_") {
		t.Fatalf("generated header = %q, want specialized unit variant ADT", rootH(t, result))
	}
}

func TestMatchAdmitsFullExpressions(t *testing.T) {
	accepted := []string{
		"ready: Bool = true\nenabled: Bool = true\nr: Int32 = match ready and enabled\n| true then 1\n| false then 0\nend\n",
		"a: Bool = true\nb: Bool = true\nready: Bool = true\nr: Bool = match ready\n| true then a or b\n| false then false\nend\n",
		"x: Int32 = 1\ny: Int32 = 2\nr: Int32 = match x < y\n| true then 1\n| false then 0\nend\n",
		"mask: Bool = true\nflag: Bool = false\nr: Int32 = match (mask or flag)\n| true then 1\n| false then 0\nend\n",
		"value: Int32 | Float64 = 1\nr: Int32 = match (value is Int32)\n| true then 1\n| false then 0\nend\n",
		"value: Int32 | Float64 = 1\nr: Int32 = match value is\n| Int32 then 1\n| Float64 then 0\nend\n",
	}
	for _, source := range accepted {
		if result := compileSource(source); result.ExitCode != compiler.ExitSuccess {
			t.Fatalf("want accept, got %v:\n%s", result.Stderr, source)
		}
	}
}
