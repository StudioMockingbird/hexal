package types

import (
	"strings"
	"testing"
)

func TestUnionTypeNormalizesIdentity(t *testing.T) {
	environment := NewEnvironment()
	left := environment.UnionType([]Type{Int32, Bool})
	right := environment.UnionType([]Type{Bool, Int32})
	nested := environment.UnionType([]Type{environment.UnionType([]Type{Int32, Bool}), Nil})
	flat := environment.UnionType([]Type{Int32, Bool, Nil})
	if !Equal(left, right) || !Equal(nested, flat) {
		t.Fatalf("union identity is order/grouping dependent: %v %v %v %v", left.Name, right.Name, nested.Name, flat.Name)
	}
	// A union must have at least two distinct canonical members, so
	// duplicates collapse to the zero Type instead of a one-member alias.
	if got := environment.UnionType([]Type{Int32, Int32}); got != (Type{}) {
		t.Fatalf("duplicate union = %s, want zero Type", got.Name)
	}
}

func TestUnionTypeUsesNullableNiche(t *testing.T) {
	environment := NewEnvironment()
	pointer := environment.PtrType(Int32)
	union := environment.UnionType([]Type{Nil, pointer})
	if !IsNullable(union) || !Equal(union, environment.NullableType(pointer)) {
		t.Fatalf("union = %#v, want canonical nullable pointer", union)
	}
}

func TestUnionTypeRejectsUnknownValue(t *testing.T) {
	environment := NewEnvironment()
	if got := environment.UnionType([]Type{Unknown, Int32}); got != (Type{}) {
		t.Fatalf("union = %#v, want zero Type", got)
	}
}

func TestUnionAssignableMembersAndWidening(t *testing.T) {
	environment := NewEnvironment()
	small := environment.UnionType([]Type{Int32, Bool})
	wide := environment.UnionType([]Type{Int32, Bool, Nil})
	if !Assignable(wide, Int32) || !Assignable(wide, small) {
		t.Fatal("member injection or union widening was rejected")
	}
	if Assignable(small, wide) {
		t.Fatal("union narrowing was accepted")
	}
}

func TestUnionTruthiness(t *testing.T) {
	environment := NewEnvironment()
	union := environment.UnionType([]Type{Bool, Int32, Nil})
	if got := Truthiness(union); got != TruthinessUnion {
		t.Fatalf("Truthiness(%s) = %v, want TruthinessUnion", union.Name, got)
	}
}

func TestTypeUsePreservesCandidatesAndNestedElement(t *testing.T) {
	environment := NewEnvironment()
	inner := environment.UnionType([]Type{UInt16, UInt8})
	pointer := environment.PtrType(inner)
	use := PointerTypeUse(pointer, UnionTypeUse(inner, []TypeUse{NewTypeUse(UInt16), NewTypeUse(UInt8)}))
	if use.Element == nil || len(use.Element.Candidates) != 2 || use.Element.Candidates[0].Type != UInt16 {
		t.Fatalf("type use = %#v, want nested UInt16 then UInt8 candidates", use)
	}
}

// Stripping the leading hex_ from member CNames preserves injectivity only if
// all stripped forms are pairwise distinct across constructible member types.
func TestStrippedUnionMemberFormsArePairwiseDistinct(t *testing.T) {
	environment := NewEnvironment()
	userObject := environment.BeginObject("Point", 1, 1)
	userAdt := environment.BeginADT("Option", 1, 1)

	constructibleTypes := []Type{
		Int8, Int16, Int32, Int64,
		UInt8, UInt16, UInt32, UInt64,
		Float32, Float64,
		Bool, SizeType,
		Nil, EoS, Unknown, Heap,
		StringType, StrandType, ErrorType, MutexType, RuneCursorType,
		userObject, userAdt,
		environment.ArrayType(Int32, 4),
		environment.ViewType(Int32),
		environment.ListType(Int32),
		environment.DictType(Int32, StringType),
		environment.TaskType(Int32),
		environment.ChannelType(Int32),
		environment.AtomicType(Int32),
		environment.PtrType(Int32),
	}

	seen := make(map[string]Type)
	for _, member := range constructibleTypes {
		stripped := strings.TrimPrefix(member.CName, "hex_")
		if previous, exists := seen[stripped]; exists {
			t.Fatalf("collision in stripped union member form %q: between %s (CName: %s) and %s (CName: %s)",
				stripped, member.Name, member.CName, previous.Name, previous.CName)
		}
		seen[stripped] = member
	}
}

func TestNestedUnionEncoding(t *testing.T) {
	environment := NewEnvironment()
	inner := environment.UnionType([]Type{Int32, Bool})
	name := unionCName([]Type{inner, Nil})
	if name != "hex_union_21_union_4_bool7_int32_t9_nullptr_t" {
		t.Fatalf("nested union CName = %q, want hex_union_21_union_4_bool7_int32_t9_nullptr_t", name)
	}
}

