package types

import "testing"

func TestDeclareGenericAndTypeParameter(t *testing.T) {
	environment := NewEnvironment()
	declaration := environment.DeclareGeneric("Box", 1, []string{"T"})
	if declaration == nil || declaration.Name != "Box" || declaration.Arity != 1 {
		t.Fatalf("declaration = %#v, want Box with arity 1", declaration)
	}
	parameter := environment.TypeParameter(declaration, 0)
	if parameter.Name != "T" || parameter.Generic != declaration || parameter.GenericIndex != 0 {
		t.Fatalf("parameter = %#v, want T placeholder", parameter)
	}
	if !parameter.Incomplete {
		t.Fatal("placeholder must be incomplete")
	}
	if environment.TypeParameter(declaration, 2).Name != "" {
		t.Fatal("out-of-range parameter index was accepted")
	}
}

func TestContainsTypeParameter(t *testing.T) {
	environment := NewEnvironment()
	declaration := environment.DeclareGeneric("Box", 1, []string{"T"})
	parameter := environment.TypeParameter(declaration, 0)
	pointer := environment.PtrType(parameter)
	if !ContainsTypeParameter(parameter) || !ContainsTypeParameter(pointer) {
		t.Fatal("placeholder-containing types were not detected")
	}
	if ContainsTypeParameter(Int32) || ContainsTypeParameter(environment.PtrType(Int32)) {
		t.Fatal("concrete types were reported as parameter-dependent")
	}
}
