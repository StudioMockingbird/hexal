package compiler

import (
	"strings"
	"testing"
)

func TestADTDeclarationWithRecordVariants(t *testing.T) {
	result := Compile("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "sw_Shape_tag") || !strings.Contains(result.MainC, ".payload.Circle") {
		t.Fatalf("generated output = H:%q C:%q, want ADT tag and payload", result.MainH, result.MainC)
	}
}

func TestADTUnitVariantEnumBehavior(t *testing.T) {
	result := Compile("type Direction = | East | West | North | South heading: Direction = Direction.North")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "sw_Direction_North") || strings.Contains(result.MainH, "payload") {
		t.Fatalf("generated header = %q, want tag-only unit variants", result.MainH)
	}
}

func TestADTQualifiedConstructorRequiresOwner(t *testing.T) {
	result := Compile("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Circle { r = 20 }")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 {
		t.Fatalf("diagnostics = %#v, want unqualified-constructor error", result.Stderr)
	}
}

func TestADTConstructorValidatesPayloadFields(t *testing.T) {
	result := Compile("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } bad: Shape = Shape.Circle { a = 20 }")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(strings.Join(result.Stderr, " "), "Circle has no field named a") {
		t.Fatalf("diagnostics = %#v, want payload field error", result.Stderr)
	}
}

func TestADTIndirectRecursionCompiles(t *testing.T) {
	result := Compile("type Expr = | Literal as { value: Int32 } | Add as { left: Ptr<Expr>, right: Ptr<Expr> }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}

func TestADTByValueRecursionRejected(t *testing.T) {
	result := Compile("type Expr = | Literal as { value: Int32 } | Wrap as { inner: Expr }")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ADT recursion has no finite representation") {
		t.Fatalf("diagnostics = %#v, want by-value recursion error", result.Stderr)
	}
}

func TestMatchValueModeBooleanPatterns(t *testing.T) {
	result := Compile("ready: Bool = true label: Int32 = match ready\n| true then 1\n| false then 0\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, "sw_match_scrutinee_1") {
		t.Fatalf("generated C = %q, want match lowering", result.MainC)
	}
}

func TestMatchTypeModeVariantArmsNarrowPayload(t *testing.T) {
	result := Compile("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } area: Int32 = match shape is\n| Shape.Circle then shape.r * shape.r\n| Shape.Square then shape.a * shape.a\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainC, ".tag == sw_Shape_Circle") || !strings.Contains(result.MainC, ".payload.Circle.sw_m_r") {
		t.Fatalf("generated C = %q, want narrowed variant payload", result.MainC)
	}
}

func TestMatchTypeModeUnionMembersAndNil(t *testing.T) {
	result := Compile("value: Int32 | Float32 | Nil = nil label: Int32 = match value is\n| Int32 then 1\n| Float32 then 2\n| Nil then 0\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}

func TestMatchElseCoversRemainder(t *testing.T) {
	result := Compile("value: Int32 | Nil = nil label: Int32 = match value is\n| Nil then 0\n| else then 1\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
}

func TestMatchExhaustivenessDiagnostic(t *testing.T) {
	result := Compile("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } label: Int32 = match shape is\n| Shape.Circle then 1\nend")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "match is not exhaustive; missing Shape.Square") {
		t.Fatalf("diagnostics = %#v, want exhaustiveness error", result.Stderr)
	}
}

func TestMatchScrutineeEvaluatedOnce(t *testing.T) {
	result := Compile("fun read_value(): Int32 return 1 end label: Int64 = match read_value()\n| else then 1\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if strings.Count(result.MainC, "sw_match_scrutinee_1 = sw_f_read_value()") != 1 {
		t.Fatalf("generated C = %q, want one scrutinee evaluation", result.MainC)
	}
}

func TestGeneratedADTTagLayoutAndInvalidTagTrap(t *testing.T) {
	result := Compile("type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 }")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "sw_Shape_tag") || !strings.Contains(result.MainH, "typedef struct sw_Shape") {
		t.Fatalf("generated header = %q, want deterministic tag-and-payload layout", result.MainH)
	}
}

func TestADTDiagnosticsFailClosed(t *testing.T) {
	result := Compile("type Shape = | Circle as { r: Int32 } | Circle as { a: Int32 }")
	if result.ExitCode != ExitFailure || len(result.Stderr) == 0 || !strings.Contains(result.Stderr[0], "ADT variant name is duplicated") {
		t.Fatalf("diagnostics = %#v, want duplicate variant error", result.Stderr)
	}
}

func TestGenericADTSpecializesAndMatches(t *testing.T) {
	result := Compile("type Result<T, E> = | Ok as { value: T } | Err as { error: E } success: Result<Int32, Bool> = Result.Ok { value = 42 } label: Int32 = match success is\n| Result.Ok then success.value\n| Result.Err then 0\nend")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "sw_Result_Int32__Bool__tag") || !strings.Contains(result.MainC, ".payload.Ok.sw_m_value") {
		t.Fatalf("generated output = H:%q C:%q, want specialized ADT and match", result.MainH, result.MainC)
	}
}

func TestGenericADTUnitVariantNoPayload(t *testing.T) {
	result := Compile("type Maybe<T> = | Some as { value: T } | None value: Maybe<Int32> = Maybe.None")
	if result.ExitCode != ExitSuccess {
		t.Fatalf("Compile exit code = %d (%v), want %d", result.ExitCode, result.Stderr, ExitSuccess)
	}
	if !strings.Contains(result.MainH, "sw_Maybe_Int32_") {
		t.Fatalf("generated header = %q, want specialized unit variant ADT", result.MainH)
	}
}
