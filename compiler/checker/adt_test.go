package checker

import "testing"

func TestCheckADTDeclarationAndConstruction(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 }"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Node.Kind != AdtConstructExpression || declaration.Source.Type.Name != "Shape" {
		t.Fatalf("source = %#v, want Shape construction", declaration.Source)
	}
}

func TestCheckADTRequiresTwoVariants(t *testing.T) {
	requireDiagnostic(t, "type Single = | Only as { value: Int32 }", "ADT declarations require at least two variants")
}

func TestCheckADTRejectsDuplicateVariant(t *testing.T) {
	requireDiagnostic(t, "type Shape = | Circle as { r: Int32 } | Circle as { a: Int32 }", "ADT variant name is duplicated")
}

func TestCheckADTConstructorValidatesPayload(t *testing.T) {
	requireDiagnostic(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } bad: Shape = Shape.Circle { a = 20 }", "Circle has no field named a")
	requireDiagnostic(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } bad: Shape = Shape.Circle { r = 1, r = 2 }", "Circle initializes field r more than once")
}

func TestCheckADTUnknownVariant(t *testing.T) {
	requireDiagnostic(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } bad: Shape = Shape.Triangle { x = 1 }", "unknown qualified variant Shape.Triangle")
}

func TestCheckADTUnitVariantConstruction(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Direction = | East | West heading: Direction = Direction.East"))
	if err != nil {
		t.Fatal(err)
	}
	declaration := checked.Statements[0].(Declaration)
	if declaration.Source.Node.Kind != AdtConstructExpression || declaration.Source.Type.Name != "Direction" {
		t.Fatalf("source = %#v, want Direction unit variant", declaration.Source)
	}
}

func TestCheckADTIndirectRecursion(t *testing.T) {
	requireAccepted(t, "type Expr = | Literal as { value: Int32 } | Add as { left: Ptr<Expr>, right: Ptr<Expr> }")
}

func TestCheckADTRejectsByValueRecursion(t *testing.T) {
	requireDiagnostic(t, "type Expr = | Literal as { value: Int32 } | Wrap as { inner: Expr }", "ADT recursion has no finite representation")
}

func TestCheckMatchValueMode(t *testing.T) {
	checked, err := Check(parseProgram(t, "ready: Bool = true label: Int32 = match ready\n| true then 1\n| false then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
	if checked.Statements[1].(Declaration).Source.Node.Kind != MatchExpression {
		t.Fatalf("source = %#v, want match expression", checked.Statements[1])
	}
}

func TestCheckMatchTypeModeNarrowsVariantPayload(t *testing.T) {
	_, err := Check(parseProgram(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } radius: Int32 = match shape is\n| Shape.Circle then shape.r\n| Shape.Square then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckMatchTypeModeExactUnionMembers(t *testing.T) {
	_, err := Check(parseProgram(t, "value: Int32 | Float32 | Nil = nil label: Int32 = match value is\n| Int32 then 1\n| Float32 then 2\n| Nil then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckMatchExhaustiveness(t *testing.T) {
	requireDiagnostic(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } label: Int32 = match shape is\n| Shape.Circle then 1\nend", "match is not exhaustive; missing Shape.Square")
}

func TestCheckMatchElseIsFinalAndCovers(t *testing.T) {
	requireAccepted(t, "value: Int32 | Nil = nil label: Int32 = match value is\n| Nil then 0\n| else then 1\nend")
	requireDiagnostic(t, "value: Int32 | Nil = nil label: Int32 = match value is\n| else then 1\n| Nil then 0\nend", "else must be the final match arm")
}

func TestCheckMatchModeErrors(t *testing.T) {
	// A type-mode Bool pattern covers the complete scrutinee and is valid.
	requireAccepted(t, "ready: Bool = true label: Int32 = match ready is\n| Bool then 1\nend")
	requireDiagnostic(t, "shape: Int32 = 1 label: Int32 = match shape\n| Int32 then 1\n| else then 0\nend", "type and variant patterns are not valid in value mode")
}

func TestCheckMatchArmNarrowingIsArmScoped(t *testing.T) {
	requireDiagnostic(t, "type Shape = | Circle as { r: Int32 } | Square as { a: Int32 } shape: Shape = Shape.Circle { r = 10 } radius: Int32 = match shape is\n| Shape.Circle then shape.r\n| Shape.Square then 0\nend bad: Int32 = shape.r", "cannot access .r on Shape; expected Ptr<T> or an object member")
}

func TestCheckGenericADTDeclarationAndConstruction(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Result<T, E> = | Ok as { value: T } | Err as { error: E } success: Result<Int32, Bool> = Result<Int32, Bool>.Ok { value = 42 }"))
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
	_, err := Check(parseProgram(t, "type Result<T, E> = | Ok as { value: T } | Err as { error: E } success: Result<Int32, Bool> = Result.Ok { value = 42 }"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckGenericADTUnitVariantInference(t *testing.T) {
	_, err := Check(parseProgram(t, "type Maybe<T> = | Some as { value: T } | None value: Maybe<Int32> = Maybe.None"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckGenericADTMatchPatterns(t *testing.T) {
	_, err := Check(parseProgram(t, "type Result<T, E> = | Ok as { value: T } | Err as { error: E } success: Result<Int32, Bool> = Result.Ok { value = 42 } label: Int32 = match success is\n| Result.Ok then success.value\n| Result.Err then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
}

func TestCheckGenericADTMatchExplicitOwnerPattern(t *testing.T) {
	_, err := Check(parseProgram(t, "type Result<T, E> = | Ok as { value: T } | Err as { error: E } success: Result<Int32, Bool> = Result.Ok { value = 42 } label: Int32 = match success is\n| Result<Int32, Bool>.Ok then success.value\n| Result.Err then 0\nend"))
	if err != nil {
		t.Fatal(err)
	}
}
