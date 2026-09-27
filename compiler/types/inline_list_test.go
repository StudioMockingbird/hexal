package types

import (
	"testing"

	"hexal/compiler/specdata"
)

func TestInlineListIdentityAndStorageFacts(t *testing.T) {
	environment := NewEnvironment()
	allocated := environment.ListType(Int32)
	four := environment.InlineListType(Int32, 4)
	eight := environment.InlineListType(Int32, 8)
	if allocated == (Type{}) || four == (Type{}) || eight == (Type{}) {
		t.Fatal("List type construction returned the zero type")
	}
	if Equal(allocated, four) || Equal(four, eight) {
		t.Fatal("different List storage forms or capacities shared one identity")
	}
	if !IsCanonical(four) || four.CanonicalKey != "inline-list:Int32,4" {
		t.Fatalf("inline List identity = %q, canonical = %v", four.CanonicalKey, IsCanonical(four))
	}
	facts, ok := TypeFactsOf(four)
	if !ok || facts.Managed || facts.Positions&specdata.StorableBinding == 0 {
		t.Fatalf("inline List facts = %#v, %v; want unmanaged storable value", facts, ok)
	}
	allocatedFacts, ok := TypeFactsOf(allocated)
	if !ok || !allocatedFacts.Managed {
		t.Fatalf("allocated List facts = %#v, %v; want managed handle", allocatedFacts, ok)
	}
}
