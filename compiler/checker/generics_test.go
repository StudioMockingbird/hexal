package checker

import (
	"testing"

	compilerTypes "hexal/compiler/types"
)

func TestCheckGenericObjectTypeDeclarationAndUse(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Box<T> = { value: T } box: Box<Int32> = Box<Int32> { value = 42 }"))
	if err != nil {
		t.Fatal(err)
	}
	var specialized TypeDeclaration
	for _, declaration := range checked.TypeDeclarations {
		if declaration.Type.Name == "Box<Int32>" {
			specialized = declaration
			break
		}
	}
	if specialized.Type.Name == "" || specialized.Type.Object == nil {
		t.Fatalf("type declarations = %#v, want nominal Box<Int32>", checked.TypeDeclarations)
	}
	if len(specialized.Type.Object.Members) != 1 || specialized.Type.Object.Members[0].Type != compilerTypes.Int32 {
		t.Fatalf("specialized members = %#v, want one Int32 member", specialized.Type.Object.Members)
	}
}

func TestCheckGenericAliasSpecializesTransparently(t *testing.T) {
	_, err := Check(parseProgram(t, "type Pointer<T> = Ptr<T> pointer: Pointer<Int32> = nil"))
	if err == nil {
		t.Fatal("Check accepted a Nil initializer for a non-null pointer")
	}
}

func TestCheckGenericTypeArityMismatch(t *testing.T) {
	requireDiagnostic(t,
		"type Pair<Left, Right> = { left: Left, right: Right } bad: Pair<Int32> = value",
		"generic type Pair expects 2 type arguments, got 1")
}

func TestCheckGenericTypeUnknownArgument(t *testing.T) {
	requireDiagnostic(t,
		"type Box<T> = { value: T } bad: Box<Missing> = value",
		"unknown type Missing")
}

func TestCheckGenericObjectPointerIndirectedRecursion(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Link<T> = { value: T, mut next: MutPtr<Link<T>> | Nil, } link: Link<Int32> = Link<Int32> { value = 1, next = nil }"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range checked.TypeDeclarations {
		if declaration.Type.Name == "Link<Int32>" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("type declarations = %#v, want Link<Int32>", checked.TypeDeclarations)
	}
}

func TestCheckGenericFunctionCallInfersArguments(t *testing.T) {
	checked, err := Check(parseProgram(t, "fun identity<T>(value: T): T\nreturn value\nend answer: Int32 = identity(42)"))
	if err != nil {
		t.Fatal(err)
	}
	if len(checked.SpecializedFunctions) != 1 {
		t.Fatalf("specialized functions = %d, want 1", len(checked.SpecializedFunctions))
	}
	specialized := checked.SpecializedFunctions[0]
	if specialized.Name != "identity_Int32" || specialized.Result == nil || *specialized.Result != compilerTypes.Int32 {
		t.Fatalf("specialized function = %#v, want identity_Int32 returning Int32", specialized)
	}
}

func TestCheckGenericFunctionExplicitArguments(t *testing.T) {
	checked, err := Check(parseProgram(t, "fun identity<T>(value: T): T\nreturn value\nend answer: Int64 = identity<Int64>(42)"))
	if err != nil {
		t.Fatal(err)
	}
	if len(checked.SpecializedFunctions) != 1 || checked.SpecializedFunctions[0].Name != "identity_Int64" {
		t.Fatalf("specialized functions = %#v, want identity_Int64", checked.SpecializedFunctions)
	}
}

func TestCheckGenericFunctionArityMismatch(t *testing.T) {
	requireDiagnostic(t,
		"fun identity<T>(value: T): T\nreturn value\nend bad: Int32 = identity<Int32, Bool>(42)",
		"explicit generic argument count does not match declaration")
}

func TestCheckGenericFunctionInferenceConflict(t *testing.T) {
	requireDiagnostic(t,
		"fun same<T>(left: T, right: T): Bool\nreturn left == right\nend bad: Bool = same(1, true)",
		"conflicting inferred types for generic parameter T")
}

func TestCheckGenericMethodSpecializesWithReceiverArguments(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Box<T> = { value: T }\nimpl Box<T>.get(): T\nreturn self.value\nend box: Box<Int32> = Box<Int32> { value = 42 }\nvalue: Int32 = box.get()"))
	if err != nil {
		t.Fatal(err)
	}
	if len(checked.SpecializedMethods) != 1 {
		t.Fatalf("specialized methods = %d, want 1", len(checked.SpecializedMethods))
	}
}

func TestCheckGenericFunctionValueReferenceInfersFromTarget(t *testing.T) {
	checked, err := Check(parseProgram(t, "fun identity<T>(value: T): T\nreturn value\nend callback: Fun<(Int32) : Int32> = identity"))
	if err != nil {
		t.Fatal(err)
	}
	if len(checked.SpecializedFunctions) != 1 || checked.SpecializedFunctions[0].Name != "identity_Int32" {
		t.Fatalf("specialized functions = %#v, want identity_Int32", checked.SpecializedFunctions)
	}
}

func TestCheckGenericObjectLiteralInfersFromExpectedType(t *testing.T) {
	checked, err := Check(parseProgram(t, "type Box<T> = { value: T } box: Box<Int32> = Box { value = 42 }"))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, declaration := range checked.TypeDeclarations {
		if declaration.Type.Name == "Box<Int32>" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("type declarations = %#v, want Box<Int32>", checked.TypeDeclarations)
	}
}

func TestCheckGenericDependentOperationFailsAtSpecialization(t *testing.T) {
	_, err := Check(parseProgram(t, "fun maximum<T>(left: T, right: T): T\nif left > right\nreturn left\nelse\nreturn right\nend\nend largest: Int32 = maximum(10, 20)"))
	if err != nil {
		t.Fatal(err)
	}
	requireDiagnostic(t,
		"fun maximum<T>(left: T, right: T): T\nif left > right\nreturn left\nelse\nreturn right\nend\nend bad: Bool = maximum(true, false)",
		"ordering is unavailable for Bool values")
}
