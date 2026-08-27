package types

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// failer is the subset of *testing.T a property check needs: fail the
// current check with a formatted message. Parameterizing over this rather
// than *testing.T lets an injection test prove a check fires without
// failing the test that proves it.
type failer interface {
	Helper()
	Fatalf(format string, args ...any)
}

// recordingFailer satisfies failer without touching the real *testing.T.
type recordingFailer struct {
	fired   bool
	message string
}

func (r *recordingFailer) Helper() {}
func (r *recordingFailer) Fatalf(format string, args ...any) {
	r.fired = true
	r.message = fmt.Sprintf(format, args...)
}

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

// collisionDomain builds the generated type domain the two properties below
// share: every builtin from the package registry, one specialization of every
// constructed family, the same three source names declared by two different
// modules over one shared arena, every ordered pair of those as a union in
// both directions, and one depth-three union per pair.
//
// The domain is generated rather than listed because each of its three
// dimensions carried a defect a hand-written list missed. Two modules
// declaring one name (an ADT named Shape in each of two modules defined the
// same struct tag twice), both member orders (M.Point | S.Point and the
// reverse produced two types and a spurious widen helper), and depth three
// (a union reached through a nested spelling must flatten to the same
// canonical type). A list covering only the first two levels of one module
// reaches none of them.
func collisionDomain(t *testing.T) ([]Type, *Environment) {
	t.Helper()
	// One arena across several module scopes is the real compilation shape:
	// constructed types intern once per compilation, not once per module.
	arena := NewArena()
	builtin := NewCompilationEnvironment(arena, "")
	app := NewCompilationEnvironment(arena, "app")
	graphics := NewCompilationEnvironment(arena, "graphics")

	names := make([]string, 0, len(builtinTypes))
	for name := range builtinTypes {
		names = append(names, name)
	}
	// builtinTypes is a map; sorting keeps the generated domain, and so any
	// failure message, identical from run to run.
	sort.Strings(names)

	bases := make([]Type, 0, len(names)+13)
	for _, name := range names {
		bases = append(bases, builtinTypes[name])
	}
	bases = append(bases,
		builtin.ListType(Int32),
		builtin.ViewType(Int32),
		builtin.ArrayType(Int32, 4),
		builtin.DictType(Int32, StringType),
		builtin.TaskType(Int32),
		builtin.ChannelType(Int32),
		builtin.AtomicType(Int32),
	)
	// The same source spellings from two modules. These are distinct
	// canonical types that any name scheme keyed on the source spelling
	// alone collapses onto one definition.
	for _, environment := range []*Environment{app, graphics} {
		// Objects must be completed to take part: an incomplete nominal is
		// not a valid union member, so a domain that only calls BeginObject
		// contributes inert bases and silently loses this dimension.
		environment.BeginObject("Shape", 1, 1)
		environment.BeginObject("Point", 1, 1)
		bases = append(bases,
			environment.CompleteObject("Shape", []ObjectMember{{Name: "sides", Type: Int32}}),
			environment.CompleteObject("Point", []ObjectMember{{Name: "x", Type: Int32}}),
			environment.BeginADT("Option", 1, 1),
		)
	}

	types := make([]Type, 0, len(bases)*len(bases))
	types = append(types, bases...)
	depth2, depth3 := 0, 0
	for left := range bases {
		for right := range bases {
			if left == right {
				continue
			}
			union := builtin.UnionType([]Type{bases[left], bases[right]})
			if union == (Type{}) {
				continue
			}
			types = append(types, union)
			depth2++
			// Depth three: one member added to the pair. The nested
			// spelling flattens, so this reaches the three-member canonical
			// types a two-level domain never constructs.
			third := bases[(left+right+1)%len(bases)]
			if nested := builtin.UnionType([]Type{union, third}); nested != (Type{}) {
				types = append(types, nested)
				depth3++
			}
		}
	}
	// A degenerate domain -- one that silently lost a dimension, such as an
	// incomplete object contributing an inert base -- would otherwise pass
	// every property below without comment. Reporting composition here makes
	// that visible instead.
	moduleOwned := 0
	for _, typ := range types {
		if typ.Object != nil && typ.Object.ModuleID != "" {
			moduleOwned++
		}
		if typ.Adt != nil && typ.Adt.ModuleID != "" {
			moduleOwned++
		}
	}
	t.Logf("collisionDomain composition: %d types total (%d depth-1 bases, %d depth-2 unions, %d depth-3 unions), %d module-owned",
		len(types), len(bases), depth2, depth3, moduleOwned)
	return types, builtin
}

// Definition-keying generated C names are unique per distinct canonical type.
// The deleted encoder was injective over its own encoding but not over type
// identity: Rune and UInt32 share the C spelling uint32_t, so Rune | Nil and
// UInt32 | Nil received one shared hex_t_ wrapper name and one program-wide
// tag, defining the same struct tag twice. The arena registry must keep
// distinct types on distinct names. Ptr is excluded: a pointer names no
// definition, and Ptr<Rune> and Ptr<UInt32> legitimately share the spelling
// uint32_t*.
// assertCNamesInjective checks that no two distinct canonical types among
// named share a definition-keying "hex_"-prefixed C name. It is
// parameterized over (Type, name) pairs rather than closing over the type
// registry so an injection test can hand it deliberately colliding names
// without needing a broken registry to produce them.
func assertCNamesInjective(t failer, named map[Type]string) {
	t.Helper()
	seen := make(map[string]Type, len(named))
	for typ, name := range named {
		if !strings.HasPrefix(name, "hex_") {
			continue
		}
		if previous, ok := seen[name]; ok && !Equal(previous, typ) {
			t.Fatalf("distinct types %s and %s share definition-keying C name %q", previous.Name, typ.Name, name)
		}
		seen[name] = typ
	}
}

// Definition-keying generated C names are unique per distinct canonical type.
// The deleted encoder was injective over its own encoding but not over type
// identity: Rune and UInt32 share the C spelling uint32_t, so Rune | Nil and
// UInt32 | Nil received one shared hex_t_ wrapper name and one program-wide
// tag, defining the same struct tag twice. The arena registry must keep
// distinct types on distinct names. Ptr is excluded: a pointer names no
// definition, and Ptr<Rune> and Ptr<UInt32> legitimately share the spelling
// uint32_t*.
func TestDefinitionKeyingCNamesNeverCollide(t *testing.T) {
	types, _ := collisionDomain(t)
	named := make(map[Type]string, len(types))
	for _, typ := range types {
		// Only definition-keying names participate: a scalar builtin such as
		// Rune and UInt32 legitimately shares the C spelling uint32_t, which
		// introduces no typedef and starts no hex_ name, exactly like Ptr.
		named[typ] = typ.CName
	}
	assertCNamesInjective(t, named)
}

// Injection half: the injectivity guard fires when two distinct types are
// handed the same "hex_"-prefixed name, proving the check itself is not
// vacuously passing.
func TestCNameInjectivityGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	assertCNamesInjective(recorder, map[Type]string{
		Int32: "hex_t_broken",
		Bool:  "hex_t_broken",
	})
	if !recorder.fired {
		t.Fatal("assertCNamesInjective did not fire on two distinct types sharing one name")
	}
}

// orderPair is one union type built forward and the same members reversed,
// with both canonical types and both generated names captured as plain
// data.
type orderPair struct {
	label             string
	forward, reversed Type
	forwardCName      string
	reversedCName     string
}

// assertUnionOrderIndependent checks that each pair's forward and reversed
// spellings are one canonical type with one generated name. It takes
// already-built pairs rather than closing over the registry so an injection
// test can hand it a pair with mismatched names without needing a broken
// union constructor to produce one.
func assertUnionOrderIndependent(t failer, pairs []orderPair) {
	t.Helper()
	for _, pair := range pairs {
		if !Equal(pair.forward, pair.reversed) {
			t.Fatalf("union %s reversed is a different canonical type %s", pair.forward.Name, pair.reversed.Name)
		}
		if pair.forwardCName != pair.reversedCName {
			t.Fatalf("union %s has name %q written forward and %q written reversed", pair.label, pair.forwardCName, pair.reversedCName)
		}
	}
}

// A union's canonical identity and generated name do not depend on the order
// its members were written. This is the metamorphic half of the property
// above: injectivity keeps distinct types apart, and this keeps two spellings
// of one type together. Both are required, and a domain that builds each pair
// in only one direction can satisfy the first while violating this one.
func TestUnionNamingIsOrderIndependent(t *testing.T) {
	types, environment := collisionDomain(t)
	pairs := make([]orderPair, 0, len(types))
	for _, typ := range types {
		members := unionMembers(typ)
		if len(members) < 2 {
			continue
		}
		reversed := make([]Type, len(members))
		for index, member := range members {
			reversed[len(members)-1-index] = member
		}
		other := environment.UnionType(reversed)
		pairs = append(pairs, orderPair{label: typ.Name, forward: typ, reversed: other, forwardCName: typ.CName, reversedCName: other.CName})
	}
	assertUnionOrderIndependent(t, pairs)
}

// Injection half: the order-independence guard fires when a pair's forward
// and reversed spellings carry different names, proving the check itself is
// not vacuously passing.
func TestUnionOrderIndependenceGuardFires(t *testing.T) {
	recorder := &recordingFailer{}
	union := NewEnvironment().UnionType([]Type{Int32, Bool})
	assertUnionOrderIndependent(recorder, []orderPair{
		{label: union.Name, forward: union, reversed: union, forwardCName: "hex_t_forward", reversedCName: "hex_t_reversed"},
	})
	if !recorder.fired {
		t.Fatal("assertUnionOrderIndependent did not fire on a pair with mismatched names")
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
