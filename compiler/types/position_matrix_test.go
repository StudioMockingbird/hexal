package types

import "testing"

// Every concrete type is checked in its named position. This matrix pins
// the registry over the exceptional types Fun, Nil, Unknown, View, Atomic,
// and an aggregate containing Atomic.
func TestPositionEligibilityMatrix(t *testing.T) {
	environment := NewEnvironment()
	result := Int32
	fun := environment.FunType([]Type{Int32}, &result)
	if fun == (Type{}) {
		t.Fatal("FunType construction failed")
	}
	view := environment.ViewType(Int32)
	if view == (Type{}) {
		t.Fatal("ViewType construction failed")
	}
	atomic := environment.AtomicType(Int32)
	if atomic == (Type{}) {
		t.Fatal("AtomicType construction failed")
	}
	object := environment.BeginObject("Shared", 1, 1)
	object = environment.CompleteObject("Shared", []ObjectMember{{Name: "count", Type: atomic}})
	if object == (Type{}) {
		t.Fatal("object construction failed")
	}

	positions := []Position{
		PositionBinding,
		PositionObjectMember,
		PositionADTPayload,
		PositionUnionMember,
		PositionArrayElement,
		PositionViewElement,
		PositionListElement,
		PositionDictValue,
		PositionFunctionParam,
		PositionFunctionResult,
		PositionTaskArgument,
		PositionTaskResult,
		PositionChannelElement,
		PositionPointee,
		PositionHeapAllocation,
	}
	// want maps each position to the types eligible there. Eligible is the
	// shared copy-requiring model: Atomic and Atomic-containing aggregates
	// are eligible nowhere, because the copy ban is unconditional. Their
	// construction positions (Binding, ObjectMember) are governed by
	// Storable, asserted separately below.
	want := map[Position]map[Type]bool{}
	for _, position := range positions {
		want[position] = map[Type]bool{
			fun:     position == PositionBinding || position == PositionUnionMember || position == PositionFunctionParam || position == PositionObjectMember,
			Nil:     position == PositionUnionMember,
			Unknown: false,
			view:    true,
			atomic:  false,
			object:  false,
		}
	}
	for _, position := range positions {
		for _, typ := range []Type{fun, Nil, Unknown, view, atomic, object} {
			if got := Eligible(typ, position); got != want[position][typ] {
				t.Fatalf("Eligible(%s, position %d) = %v, want %v", typ.Name, position, got, want[position][typ])
			}
		}
	}

	// The explicit Unknown exception: Ptr<Unknown> and MutPtr<Unknown> are
	// valid pointees even though Unknown is not storable anywhere.
	if environment.PtrType(Unknown) == (Type{}) || environment.MutPtrType(Unknown) == (Type{}) {
		t.Fatal("Unknown must remain a valid pointee")
	}
	// The pointee position uses Storable, not Eligible: a direct Atomic
	// element is rejected, but an Atomic-containing object stays valid
	// because containment stops at pointer indirection.
	if Storable(atomic, PositionPointee) {
		t.Fatal("Atomic must not be storable as a pointee")
	}
	if !Storable(object, PositionPointee) {
		t.Fatal("an Atomic-containing object must be storable as a pointee")
	}
	if environment.PtrType(object) == (Type{}) {
		t.Fatal("pointer to an Atomic-containing object must stay valid")
	}
	if environment.PtrType(atomic) != (Type{}) {
		t.Fatal("pointer to a direct Atomic element must be rejected")
	}
	// Construction positions: Storable admits Atomic and Atomic-containing
	// values into direct declarations and object members.
	if !Storable(atomic, PositionBinding) || !Storable(atomic, PositionObjectMember) {
		t.Fatal("Atomic must be storable in construction positions")
	}
	if !Storable(object, PositionBinding) || !Storable(object, PositionObjectMember) {
		t.Fatal("an Atomic-containing object must be storable in construction positions")
	}
}
