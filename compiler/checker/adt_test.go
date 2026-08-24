package checker

import (
	"go/constant"
	"testing"
)

func TestCheckADTDeclarationAndConstruction(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } end shape: Shape := Shape.Circle { r = 10 }"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Node.Kind != AdtConstructExpression || declaration.Source.Type.Name != "Shape" {
		t.Fatalf("source = %#v, want Shape construction", declaration.Source)
	}
}

func TestCheckADTRequiresTwoVariants(t *testing.T) {
	requireDiagnostic(t, "type Single as | Only { value: Int32 } end", "ADT declarations require at least two variants")
}

func TestCheckADTRejectsDuplicateVariant(t *testing.T) {
	requireDiagnostic(t, "type Shape as | Circle { r: Int32 } | Circle { a: Int32 } end", "ADT variant name is duplicated")
}

func TestCheckADTConstructorValidatesPayload(t *testing.T) {
	requireDiagnostic(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } end bad: Shape := Shape.Circle { a = 20 }", "Circle has no field named a")
	requireDiagnostic(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } end bad: Shape := Shape.Circle { r = 1, r = 2 }", "Circle initializes field r more than once")
}

func TestCheckADTUnknownVariant(t *testing.T) {
	requireDiagnostic(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } end bad: Shape := Shape.Triangle { x = 1 }", "unknown qualified variant Shape.Triangle")
}

func TestCheckADTUnitVariantConstruction(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Direction as | East | West end heading: Direction := Direction.East"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Node.Kind != AdtConstructExpression || declaration.Source.Type.Name != "Direction" {
		t.Fatalf("source = %#v, want Direction unit variant", declaration.Source)
	}
}

// A variant constructor's payload fields may be written out of declaration
// order; the checked tree must still assign each field the value its own
// name was initialized with, indexed by declaration position (the shape
// generation assumes), never by write position.
func TestCheckADTPayloadOutOfOrderAssignsCorrectFields(t *testing.T) {
	checked, err := Check(parseProgram(t,
		"type W as | A { first: Int32, second: Int32 } | B { x: Int32 } end\n"+
			"w: W := W.A { second = 20, first = 10 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	node := checked.Statements[0].(Declaration).Source.Node
	if node.Kind != AdtConstructExpression || len(node.Arguments) != 2 {
		t.Fatalf("node = %#v, want a two-field AdtConstructExpression", node)
	}
	// Arguments is declaration order: first, second.
	if got, ok := constant.Int64Val(node.Arguments[0].Constant); !ok || got != 10 {
		t.Fatalf("Arguments[0] (first) = %v, want 10", node.Arguments[0].Constant)
	}
	if got, ok := constant.Int64Val(node.Arguments[1].Constant); !ok || got != 20 {
		t.Fatalf("Arguments[1] (second) = %v, want 20", node.Arguments[1].Constant)
	}
	// EvaluationOrder is written order: second (declared index 1) was
	// written first, then first (declared index 0).
	if len(node.EvaluationOrder) != 2 || node.EvaluationOrder[0] != 1 || node.EvaluationOrder[1] != 0 {
		t.Fatalf("EvaluationOrder = %v, want [1 0]", node.EvaluationOrder)
	}
}

// The same out-of-order correctness holds for a generic ADT's variant
// constructor.
func TestCheckGenericADTPayloadOutOfOrderAssignsCorrectFields(t *testing.T) {
	checked, err := Check(parseProgram(t,
		"type W<T> as | A { first: T, second: T } | B { x: T } end\n"+
			"w: W<Int32> := W<Int32>.A { second = 20, first = 10 }\n"))
	if err != nil {
		t.Fatal(err)
	}
	node := checked.Statements[0].(Declaration).Source.Node
	if node.Kind != AdtConstructExpression || len(node.Arguments) != 2 {
		t.Fatalf("node = %#v, want a two-field AdtConstructExpression", node)
	}
	if got, ok := constant.Int64Val(node.Arguments[0].Constant); !ok || got != 10 {
		t.Fatalf("Arguments[0] (first) = %v, want 10", node.Arguments[0].Constant)
	}
	if got, ok := constant.Int64Val(node.Arguments[1].Constant); !ok || got != 20 {
		t.Fatalf("Arguments[1] (second) = %v, want 20", node.Arguments[1].Constant)
	}
}

func TestCheckADTIndirectRecursion(t *testing.T) {
	requireAccepted(t, "type Expr as | Literal { value: Int32 } | Add { left: Ptr<Expr>, right: Ptr<Expr> } end")
}

func TestCheckADTRejectsByValueRecursion(t *testing.T) {
	requireDiagnostic(t, "type Expr as | Literal { value: Int32 } | Wrap { inner: Expr } end", "ADT recursion has no finite representation")
}

func TestCheckMatchValueMode(t *testing.T) {
	checked, err := Check(parseProgram(t, "ready: Bool := true label: Int32 := match ready\n| true then 1\n| false then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
	if checked.Statements[1].(Declaration).Source.Node.Kind != MatchExpression {
		t.Fatalf("source = %#v, want match expression", checked.Statements[1])
	}
}

func TestCheckMatchTypeModeNarrowsVariantPayload(t *testing.T) {
	_, err := Check(parseProgram(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } end shape: Shape := Shape.Circle { r = 10 } radius: Int32 := match shape is\n| Shape.Circle then shape.r\n| Shape.Square then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckMatchTypeModeExactUnionMembers(t *testing.T) {
	_, err := Check(parseProgram(t, "value: Int32 | Float32 | Nil := nil label: Int32 := match value is\n| Int32 then 1\n| Float32 then 2\n| Nil then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckMatchExhaustiveness(t *testing.T) {
	requireDiagnostic(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } end shape: Shape := Shape.Circle { r = 10 } label: Int32 := match shape is\n| Shape.Circle then 1\nend", "match is not exhaustive; missing Shape.Square")
}

// With several uncovered members the diagnostic names the first in
// declaration order, so repeated checks of one source report one stable
// message.
func TestCheckMatchExhaustivenessNamesFirstMissingInDeclarationOrder(t *testing.T) {
	requireDiagnostic(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } | Triangle { b: Int32 } end shape: Shape := Shape.Circle { r = 10 } label: Int32 := match shape is\n| Shape.Square then 1\nend", "match is not exhaustive; missing Shape.Circle")
	requireDiagnostic(t, "value: Int32 | Float64 | Nil := nil label: Int32 := match value is\n| Float64 then 1\nend", "match is not exhaustive; missing Int32")
}

func TestCheckMatchElseIsFinalAndCovers(t *testing.T) {
	requireAccepted(t, "value: Int32 | Nil := nil label: Int32 := match value is\n| Nil then 0\n| else then 1\nend")
	requireDiagnostic(t, "value: Int32 | Nil := nil label: Int32 := match value is\n| else then 1\n| Nil then 0\nend", "else must be the final match arm")
}

func TestCheckMatchModeErrors(t *testing.T) {
	// A type-mode Bool pattern covers the complete scrutinee and is valid.
	requireAccepted(t, "ready: Bool := true label: Int32 := match ready is\n| Bool then 1\nend")
	requireDiagnostic(t, "shape: Int32 := 1 label: Int32 := match shape\n| Int32 then 1\n| else then 0\nend", "type and variant patterns are not valid in value mode")
}

func TestCheckMatchArmNarrowingIsArmScoped(t *testing.T) {
	requireDiagnostic(t, "type Shape as | Circle { r: Int32 } | Square { a: Int32 } end shape: Shape := Shape.Circle { r = 10 } radius: Int32 := match shape is\n| Shape.Circle then shape.r\n| Shape.Square then 0\nend bad: Int32 := shape.r", "cannot access .r on Shape; expected Ptr<T> or an object member")
}

func TestCheckGenericADTDeclarationAndConstruction(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Result<T, E> as | Ok { value: T } | Err { error: E } end success: Result<Int32, Bool> := Result<Int32, Bool>.Ok { value = 42 }"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Node.Kind != AdtConstructExpression || declaration.Source.Type.Name != "Result<Int32, Bool>" {
		t.Fatalf("source = %#v, want specialized Result construction", declaration.Source)
	}
	found := false
	for _, typeDeclaration := range checked.TypeDeclarations {
		if typeDeclaration.Type.Name == "Result<Int32, Bool>" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("type declarations = %#v, want specialized Result", checked.TypeDeclarations)
	}
}

func TestCheckGenericADTExpectedTypeInference(t *testing.T) {
	_, err := Check(parseProgram(t, "type Result<T, E> as | Ok { value: T } | Err { error: E } end success: Result<Int32, Bool> := Result.Ok { value = 42 }"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckGenericADTUnitVariantInference(t *testing.T) {
	_, err := Check(parseProgram(t, "type Maybe<T> as | Some { value: T } | None end value: Maybe<Int32> := Maybe.None"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckGenericADTMatchPatterns(t *testing.T) {
	_, err := Check(parseProgram(t, "type Result<T, E> as | Ok { value: T } | Err { error: E } end success: Result<Int32, Bool> := Result.Ok { value = 42 } label: Int32 := match success is\n| Result.Ok then success.value\n| Result.Err then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckGenericADTMatchExplicitOwnerPattern(t *testing.T) {
	_, err := Check(parseProgram(t, "type Result<T, E> as | Ok { value: T } | Err { error: E } end success: Result<Int32, Bool> := Result.Ok { value = 42 } label: Int32 := match success is\n| Result<Int32, Bool>.Ok then success.value\n| Result.Err then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
}
