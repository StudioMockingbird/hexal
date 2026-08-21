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

// Definition-keying generated C names are unique per distinct canonical type.
// The deleted encoder was injective over its own encoding but not over type
// identity: Rune and UInt32 share the C spelling uint32_t, so Rune | Nil and
// UInt32 | Nil receives one shared hex_t_ wrapper name and one program-wide tag.
// defined the same struct tag twice. The arena registry must keep distinct
// types on distinct names. Builtins are enumerated from the package registry,
// not a list written here, plus one specialization of every constructed
// family and every binary union of the two; the pair construction is what
// makes this test fire on the historical collision. Ptr is excluded: a
// pointer names no definition, and Ptr<Rune> and Ptr<UInt32> legitimately
// share the spelling uint32_t*.
func TestDefinitionKeyingCNamesNeverCollide(t *testing.T) {
	environment := NewEnvironment()
	bases := make([]Type, 0, len(builtinTypes)+9)
	for _, typ := range builtinTypes {
		bases = append(bases, typ)
	}
	bases = append(bases,
		environment.BeginObject("Shape", 1, 1),
		environment.BeginADT("Option", 1, 1),
		environment.ListType(Int32),
		environment.ViewType(Int32),
		environment.ArrayType(Int32, 4),
		environment.DictType(Int32, StringType),
		environment.TaskType(Int32),
		environment.ChannelType(Int32),
		environment.AtomicType(Int32),
	)
	types := make([]Type, 0, len(bases)+len(bases)*len(bases))
	types = append(types, bases...)
	for index := 0; index < len(bases); index++ {
		for other := index + 1; other < len(bases); other++ {
			if union := environment.UnionType([]Type{bases[index], bases[other]}); union != (Type{}) {
				types = append(types, union)
			}
		}
	}
	seen := make(map[string]Type, len(types))
	for _, typ := range types {
		// Only definition-keying names participate: a scalar builtin such as
		// Rune and UInt32 legitimately shares the C spelling uint32_t, which
		// introduces no typedef and starts no hex_ name, exactly like Ptr.
		if !strings.HasPrefix(typ.CName, "hex_") {
			continue
		}
		if previous, ok := seen[typ.CName]; ok && !Equal(previous, typ) {
			t.Fatalf("distinct types %s and %s share definition-keying C name %q", previous.Name, typ.Name, typ.CName)
		}
		seen[typ.CName] = typ
	}
}

// UnionType collapses a written nested union to the flat member set, so the
// nested spelling and the flat spelling are one canonical type even though
// only the flat spelling is reachable from source. The registry-derived name
// is recorded per canonical union; this pins that the arena owns the name of
// the nested spelling too and that the name is C-identifier-safe.
func TestNestedUnionEncoding(t *testing.T) {
	environment := NewEnvironment()
	inner := environment.UnionType([]Type{Int32, Bool})
	nested := environment.UnionType([]Type{inner, Nil})
	flat := environment.UnionType([]Type{Int32, Bool, Nil})
	if !Equal(nested, flat) {
		t.Fatalf("nested union %#v and flat union %#v are not one canonical type", nested, flat)
	}
	if registered, ok := environment.arena.definitionNames[nested.CName]; !ok || !Equal(registered, nested) {
		t.Fatalf("nested union CName %q is not owned by the arena registry", nested.CName)
	}
	if strings.ContainsAny(nested.CName, "<>,| ") {
		t.Fatalf("nested union CName %q contains a non-identifier character", nested.CName)
	}
}
