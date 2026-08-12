package types

import "testing"

func TestADTTypeIdentityAndVariants(t *testing.T) {
	environment := NewEnvironment()
	adt := environment.BeginADT("Shape", 1, 1)
	if !IsADT(adt) || adt.Adt == nil || len(adt.Adt.Variants) != 0 {
		t.Fatalf("adt = %#v, want provisional nominal ADT", adt)
	}
	adt = environment.CompleteADT("Shape", []AdtVariant{
		{Name: "Circle", Payload: []ObjectMember{{Name: "r", Type: Int32}}},
		{Name: "Square", Payload: []ObjectMember{{Name: "a", Type: Int32}}},
	})
	if len(adt.Adt.Variants) != 2 || !IsCanonical(adt) || !IsCompleteValue(adt) {
		t.Fatalf("completed adt = %#v, want canonical nominal ADT", adt)
	}
	if variant, ok := environment.AdtVariant("Shape", "Circle"); !ok || variant.Name != "Circle" || len(variant.Payload) != 1 {
		t.Fatalf("AdtVariant = %#v, %v, want Circle", variant, ok)
	}
	if _, ok := environment.AdtVariant("Shape", "Triangle"); ok {
		t.Fatal("unknown variant resolved")
	}
}
