package types

import (
	"testing"
)

// UnionMembers returns a read-only view: no caller receives the canonical
// member slice, and every access shape allocates nothing.
func TestUnionMemberViewExposesReadOnlyAccess(t *testing.T) {
	environment := NewEnvironment()
	tagged := environment.UnionType([]Type{Int32, Float64})
	if tagged.Union == nil {
		t.Fatalf("UnionType(Int32, Float64) = %#v, want a tagged union", tagged)
	}
	nullable := environment.NullableType(environment.PtrType(Int32))
	singleton := Int32

	cases := []struct {
		name  string
		typ   Type
		first Type
		count int
	}{
		{name: "tagged union", typ: tagged, first: Int32, count: 2},
		{name: "nullable pointer niche", typ: nullable, first: environment.PtrType(Int32), count: 2},
		{name: "singleton", typ: singleton, first: Int32, count: 1},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			view := UnionMembers(testCase.typ)
			if view.Len() != testCase.count {
				t.Fatalf("Len() = %d, want %d", view.Len(), testCase.count)
			}
			first, ok := view.At(0)
			if !ok || !Equal(first, testCase.first) {
				t.Fatalf("At(0) = %#v %v, want %s", first, ok, testCase.first.Name)
			}
			if _, ok := view.At(view.Len()); ok {
				t.Fatal("At(count) reported in bounds")
			}
			if _, ok := view.At(-1); ok {
				t.Fatal("At(-1) reported in bounds")
			}
		})
	}
}

// Every access path of the read-only member view allocates zero bytes:
// ordinary and singleton views reference the canonical slice privately and
// the nullable-pointer niche derives its two members from stored metadata.
func TestUnionMemberViewAllocatesNothing(t *testing.T) {
	environment := NewEnvironment()
	tagged := environment.UnionType([]Type{Int32, Float64})
	nullable := environment.NullableType(environment.PtrType(Int32))
	read := func(typ Type) {
		view := UnionMembers(typ)
		for index := 0; index < view.Len(); index++ {
			view.At(index)
		}
	}
	for name, typ := range map[string]Type{
		"tagged union":           tagged,
		"nullable pointer niche": nullable,
		"singleton":              Int32,
	} {
		if allocations := testing.AllocsPerRun(100, func() { read(typ) }); allocations != 0 {
			t.Fatalf("%s: AllocsPerRun = %v, want 0", name, allocations)
		}
	}
}
