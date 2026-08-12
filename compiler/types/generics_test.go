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
	if got, ok := environment.LookupGeneric("Box"); !ok || got != declaration {
		t.Fatalf("LookupGeneric = %#v, %v, want the declared record", got, ok)
	}
}

func TestSubstituteReplacesParameters(t *testing.T) {
	environment := NewEnvironment()
	declaration := environment.DeclareGeneric("Pair", 2, []string{"Left", "Right"})
	left := environment.TypeParameter(declaration, 0)
	right := environment.TypeParameter(declaration, 1)
	pointer := environment.PtrType(left)
	if pointer == (Type{}) {
		t.Fatal("Ptr<T> could not be constructed for the open template")
	}
	substituted := environment.Substitute(pointer, []Type{Int32, Bool})
	if substituted.Element == nil || !Equal(*substituted.Element, Int32) {
		t.Fatalf("substituted pointer = %#v, want Ptr<Int32>", substituted)
	}
	union := environment.UnionType([]Type{left, right})
	if union == (Type{}) {
		t.Fatal("T | U could not be constructed for the open template")
	}
	substitutedUnion := environment.Substitute(union, []Type{Int32, Bool})
	if !ContainsUnionMember(substitutedUnion, Int32) || !ContainsUnionMember(substitutedUnion, Bool) {
		t.Fatalf("substituted union = %#v, want Int32 | Bool", substitutedUnion)
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

func TestSpecializeIsInternedByDeclarationAndArguments(t *testing.T) {
	environment := NewEnvironment()
	declaration := environment.DeclareGeneric("Alias", 1, []string{"T"})
	parameter := environment.TypeParameter(declaration, 0)
	template := environment.PtrType(parameter)
	first := environment.Specialize(declaration, []Type{Int32}, template)
	second := environment.Specialize(declaration, []Type{Int32}, template)
	if !Equal(first, second) || first.Element == nil || !Equal(*first.Element, Int32) {
		t.Fatalf("specializations = %#v and %#v, want interned Ptr<Int32>", first, second)
	}
	if Equal(first, environment.Specialize(declaration, []Type{Bool}, template)) {
		t.Fatal("different arguments produced one specialization")
	}
}
