package types

import "testing"

// The invalid inline-recursion layout is owned by layout validation, but
// atomic discovery still walks its members first. A cyclic payload must
// terminate with a false result before any later check decides the layout
// is invalid; a real Atomic on a sibling path is still found.
func TestContainsAtomicTerminatesOnRecursiveShape(t *testing.T) {
	leaf := &AdtType{Name: "T"}
	provisional := Type{
		Name:         "T",
		CName:        "hex_t_m3_app_T",
		CanonicalKey: "m1:T",
		Adt:          leaf,
	}
	inline := Type{
		Name:         "List<T, 1>",
		CName:        "hex_list_T",
		CanonicalKey: "inline-list:m1:T,1",
		InlineList:   &InlineListInfo{Element: provisional, Capacity: 1},
	}
	leaf.Variants = []AdtVariant{
		{Name: "Node", Payload: []ObjectMember{{Name: "children", Type: inline}}},
	}
	if ContainsAtomic(provisional) {
		t.Fatalf("ContainsAtomic reported an Atomic on a shape without any Atomic value")
	}
}

func TestContainsAtomicStillFindsSiblingAtomic(t *testing.T) {
	holder := &ObjectType{Name: "Holder"}
	memberHolder := Type{
		Name:         "Holder",
		CName:        "hex_t_m3_app_Holder",
		CanonicalKey: "m1:Holder",
		Object:       holder,
	}
	holder.Members = []ObjectMember{
		{Name: "counter", Type: Type{Atomic: &AtomicInfo{Element: Int32}}},
	}
	if !ContainsAtomic(memberHolder) {
		t.Fatalf("ContainsAtomic missed a real Atomic on a member path")
	}
	wrapper := &AdtType{Name: "Wrap"}
	wrapperType := Type{
		Name:         "Wrap",
		CName:        "hex_t_m3_app_Wrap",
		CanonicalKey: "m1:Wrap",
		Adt:          wrapper,
	}
	wrapper.Variants = []AdtVariant{
		{Name: "Full", Payload: []ObjectMember{{Name: "value", Type: memberHolder}}},
	}
	if !ContainsAtomic(wrapperType) {
		t.Fatalf("ContainsAtomic missed the sibling Atomic through one ADT payload")
	}
}
