package types

import (
	"fmt"
	"strings"
	"testing"

	"hexal/compiler/specdata"
)

// TestPositionBitsMatchRegistry pins the compiler's Position declaration order
// to the registry's position bits, so the mask shift in Storable cannot index a
// different position than the one it names.
func TestPositionBitsMatchRegistry(t *testing.T) {
	cases := []struct {
		position Position
		bit      specdata.PositionMask
	}{
		{PositionBinding, specdata.StorableBinding},
		{PositionObjectMember, specdata.StorableObjectMember},
		{PositionADTPayload, specdata.StorableADTPayload},
		{PositionUnionMember, specdata.StorableUnionMember},
		{PositionArrayElement, specdata.StorableArrayElement},
		{PositionSliceElement, specdata.StorableSliceElement},
		{PositionListElement, specdata.StorableListElement},
		{PositionDictValue, specdata.StorableDictValue},
		{PositionFunctionParam, specdata.StorableFunctionParam},
		{PositionFunctionResult, specdata.StorableFunctionResult},
		{PositionTaskArgument, specdata.StorableTaskArgument},
		{PositionTaskResult, specdata.StorableTaskResult},
		{PositionChannelElement, specdata.StorableChannelElement},
		{PositionPointee, specdata.StorablePointee},
		{PositionHeapAllocation, specdata.StorableHeapAllocation},
	}
	for _, testCase := range cases {
		if specdata.PositionMask(1)<<uint(testCase.position) != testCase.bit {
			t.Fatalf("position %d does not match its registry bit %#x", testCase.position, testCase.bit)
		}
	}
}

// everyPosition is the full position list, in declaration order.
var everyPosition = []Position{
	PositionBinding,
	PositionObjectMember,
	PositionADTPayload,
	PositionUnionMember,
	PositionArrayElement,
	PositionSliceElement,
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

func positionSet(positions ...Position) map[Position]bool {
	set := make(map[Position]bool, len(positions))
	for _, position := range positions {
		set[position] = true
	}
	return set
}

// TestStorablePositionSets pins the position model's exceptional identities to
// the placement sets the records declare. The sets are transcribed from the
// rules the records replaced, so a record that disagrees with a consumer fails
// here.
func TestStorablePositionSets(t *testing.T) {
	environment := NewEnvironment()
	result := Int32
	fun := environment.FunType([]Type{Int32}, &result)
	inline := environment.InlineStringType(16)
	atomic := environment.AtomicType(Int32)
	slice := environment.SliceType(Int32, false)
	list := environment.ListType(Int32)
	dict := environment.DictType(Int32, Int32)

	all := positionSet(everyPosition...)
	cases := []struct {
		name  string
		typ   Type
		allow map[Position]bool
	}{
		{"Fun", fun, positionSet(
			PositionBinding, PositionObjectMember, PositionADTPayload, PositionUnionMember,
			PositionArrayElement, PositionSliceElement, PositionListElement, PositionDictValue,
			PositionFunctionParam, PositionFunctionResult, PositionTaskArgument, PositionTaskResult,
			PositionChannelElement)},
		{"Nil", Nil, positionSet(PositionUnionMember)},
		{"Unknown", Unknown, positionSet()},
		{"IO", IOType, positionSet(
			PositionBinding, PositionUnionMember, PositionFunctionParam,
			PositionFunctionResult, PositionTaskArgument, PositionTaskResult, PositionPointee)},
		{"Bytes", BytesType, positionSet(
			PositionBinding, PositionUnionMember, PositionFunctionParam,
			PositionFunctionResult, PositionPointee)},
		{"Atomic<Int32>", atomic, positionSet(PositionBinding, PositionObjectMember)},
		{"String<16>", inline, all},
		{"Slice<Int32>", slice, all},
		{"List<Int32>", list, all},
		{"Dict<Int32,Int32>", dict, all},
	}
	for _, testCase := range cases {
		for _, position := range everyPosition {
			want := testCase.allow[position]
			if got := Storable(testCase.typ, position); got != want {
				t.Fatalf("Storable(%s, %d) = %v, want %v", testCase.name, position, got, want)
			}
		}
	}
}

// TestPointerPointeeExclusion pins the types whose own aliasing and
// invalidation rules exclude them as pointer pointees, and proves the exclusion
// is not a general handle rule: the shared handle types stay valid pointees.
func TestPointerPointeeExclusion(t *testing.T) {
	environment := NewEnvironment()
	inline := environment.InlineStringType(16)
	list := environment.ListType(Int32)
	dict := environment.DictType(Int32, Int32)
	slice := environment.SliceType(Int32, false)
	stash := environment.StashType(Int32)
	pool := environment.PoolType(Int32)
	task := environment.TaskType(Int32)
	channel := environment.ChannelType(Int32)

	for _, typ := range []Type{StringType, list, dict, slice} {
		if environment.PtrType(typ) != (Type{}) {
			t.Errorf("%s must not be a Ptr pointee", typ.Name)
		}
	}
	for _, typ := range []Type{inline, MutexType, stash, pool, task, channel, Int32} {
		if environment.PtrType(typ) == (Type{}) {
			t.Errorf("%s must be a Ptr pointee", typ.Name)
		}
	}
}

// TestStorableAndPointeeReadTheRegistry proves both consumers read the registry
// rather than a hard-coded fact: corrupting the position mask admits Atomic in
// a copy position, and clearing Managed admits a List as a pointer pointee.
func TestStorableAndPointeeReadTheRegistry(t *testing.T) {
	environment := NewEnvironment()
	atomic := environment.AtomicType(Int32)
	list := environment.ListType(Int32)
	if Storable(atomic, PositionFunctionResult) {
		t.Fatal("Atomic must not be storable in a function result")
	}
	if environment.PtrType(list) != (Type{}) {
		t.Fatal("List must not be a Ptr pointee")
	}

	original := typeFactsOf
	typeFactsOf = func(Type) (specdata.TypeFacts, bool) {
		return specdata.TypeFacts{Positions: specdata.StorableEverywhere}, true
	}
	defer func() { typeFactsOf = original }()

	if !Storable(atomic, PositionFunctionResult) {
		t.Fatal("Storable ignored the corrupted position record")
	}
	if environment.PtrType(list) == (Type{}) {
		t.Fatal("pointerType ignored the corrupted Managed fact")
	}
}

// registryRelationError reports the first disagreement between registry
// domains, each passed in so a test can poison one registration and prove the
// check fails rather than trusting the live tables:
//
//   - every concrete identifier resolves to an interned Type, because the
//     inventory claims to list exactly what the resolver must answer;
//   - a resolved identity's facts agree with the consumer's view in both
//     value and presence, because a record only one side can see makes the
//     other side a second authority for the same fact;
//   - every builtin entry is protected, resolves through its own name to its
//     own identity, and canonicalizes back through the registered Type's Name.
func registryRelationError(ids []specdata.TypeID, builtins map[string]Type, resolve func(specdata.TypeID) (Type, bool), protected func(string) bool) error {
	for _, id := range ids {
		typ, ok := resolve(id)
		if !ok {
			return fmt.Errorf("concrete type %q does not resolve", id)
		}
		registryFacts, registryRecord := specdata.Facts(id)
		consumerFacts, consumerRecord := TypeFactsOf(typ)
		if registryRecord != consumerRecord {
			return fmt.Errorf("concrete type %q facts: registry record %v, consumer record %v", id, registryRecord, consumerRecord)
		}
		if registryRecord && registryFacts != consumerFacts {
			return fmt.Errorf("concrete type %q facts: registry %v, consumer %v", id, registryFacts, consumerFacts)
		}
	}
	for name, typ := range builtins {
		if !protected(name) {
			return fmt.Errorf("builtin %q is not protected", name)
		}
		resolved, ok := resolve(specdata.TypeID(name))
		if !ok {
			return fmt.Errorf("builtin %q has no concrete identifier", name)
		}
		if !Equal(resolved, typ) {
			return fmt.Errorf("builtin %q resolves to %s, registered as %s", name, resolved.Name, typ.Name)
		}
		canonical, ok := builtins[typ.Name]
		if !ok {
			return fmt.Errorf("builtin %q registers a type named %q, which has no entry", name, typ.Name)
		}
		if !Equal(canonical, typ) {
			return fmt.Errorf("builtin %q and entry %q hold different identities", name, typ.Name)
		}
	}
	return nil
}

// TestRegistryRelationsHoldAcrossDomains pins each registry to the domain its
// neighbours promise: the concrete inventory resolves everywhere, facts round
// trip through the consumer, and builtin entries stay protected,
// self-resolving, and canonical. A protected constructor name need not name a
// builtin Type, so the constructor loop also requires at least one source
// name to stay unresolvable as a bare builtin.
func TestRegistryRelationsHoldAcrossDomains(t *testing.T) {
	if err := registryRelationError(specdata.ConcreteTypeIDs(), builtinTypes, ResolveSpecID, IsProtectedTypeName); err != nil {
		t.Fatal(err)
	}
	unresolved := 0
	for _, constructor := range specdata.TypeConstructors() {
		if !IsProtectedTypeName(constructor.SourceName) {
			t.Errorf("constructor %q source name %q is not protected", constructor.ID, constructor.SourceName)
		}
		if _, ok := Lookup(constructor.SourceName); !ok {
			unresolved++
		}
	}
	if unresolved == 0 {
		t.Error("every constructor source name resolves as a bare builtin; protectedness must not require a builtin Type")
	}
}

// TestRegistryRelationsFailWhenARegistrationIsMissing drops one concrete
// identifier from the resolver and requires the relation check to reject it:
// a registration missing on any side must fail the guard, never pass quietly.
func TestRegistryRelationsFailWhenARegistrationIsMissing(t *testing.T) {
	poisoned := func(id specdata.TypeID) (Type, bool) {
		if id == specdata.TypeMutex {
			return Type{}, false
		}
		return ResolveSpecID(id)
	}
	err := registryRelationError(specdata.ConcreteTypeIDs(), builtinTypes, poisoned, IsProtectedTypeName)
	if err == nil {
		t.Fatal("registry relations held after one concrete identifier stopped resolving")
	}
	if !strings.Contains(err.Error(), "Mutex") {
		t.Fatalf("relation error %q does not name the poisoned registration", err)
	}
}
