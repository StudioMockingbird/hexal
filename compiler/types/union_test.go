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

// unionCName embeds each member's CName with its leading hex_ stripped, so the
// encoding stays injective exactly as long as stripping never merges two
// distinct C spellings into one. That is the property, and it is not the same
// as "every member's stripped form is unique": several builtins deliberately
// share a spelling (Byte and UInt8 are both uint8_t, Rune and UInt32 are both
// uint32_t), and a union of two of those has one member, not two.
//
// The builtin half enumerates types.builtinTypes rather than a list written
// here, so a builtin added without a distinct spelling fails this test instead
// of silently escaping it. The parameterized half must stay written out: those
// types are constructed, not registered.
func TestStrippingHexPrefixNeverMergesTwoCSpellings(t *testing.T) {
	environment := NewEnvironment()
	spellings := map[string]string{}
	record := func(name, cName string) {
		if previous, exists := spellings[cName]; exists && previous != name {
			return // one C spelling reached by two Hexal names is fine and pre-existing
		}
		spellings[cName] = name
	}
	for name, member := range builtinTypes {
		record(name, member.CName)
	}
	for name, member := range map[string]Type{
		"Point":          environment.BeginObject("Point", 1, 1),
		"Option":         environment.BeginADT("Option", 1, 1),
		"Array<Int32,4>": environment.ArrayType(Int32, 4),
		"View<Int32>":    environment.ViewType(Int32),
		"List<Int32>":    environment.ListType(Int32),
		"Dict<Int32,S>":  environment.DictType(Int32, StringType),
		"Task<Int32>":    environment.TaskType(Int32),
		"Channel<Int32>": environment.ChannelType(Int32),
		"Atomic<Int32>":  environment.AtomicType(Int32),
		"Ptr<Int32>":     environment.PtrType(Int32),
	} {
		record(name, member.CName)
	}

	stripped := map[string]string{}
	for cName := range spellings {
		form := strings.TrimPrefix(cName, "hex_")
		if previous, exists := stripped[form]; exists {
			t.Fatalf("stripping hex_ merges two distinct C spellings into %q: %q (%s) and %q (%s)",
				form, cName, spellings[cName], previous, spellings[previous])
		}
		stripped[form] = cName
	}
	if len(stripped) != len(spellings) {
		t.Fatalf("stripped forms = %d, distinct C spellings = %d", len(stripped), len(spellings))
	}
}

// unionCName is exercised directly here on a member that is itself a union.
// UnionType flattens, so this shape never reaches the encoder through normal
// construction: TestNestedUnionEncodingIntegration covers what a source-level
// nested union actually produces. This pins the encoder's own behaviour so the
// length prefix stays correct if flattening ever changes.
func TestNestedUnionEncoding(t *testing.T) {
	environment := NewEnvironment()
	inner := environment.UnionType([]Type{Int32, Bool})
	name := unionCName([]Type{inner, Nil})
	if name != "hex_union_21_union_4_bool7_int32_t9_nullptr_t" {
		t.Fatalf("nested union CName = %q, want hex_union_21_union_4_bool7_int32_t9_nullptr_t", name)
	}
}
