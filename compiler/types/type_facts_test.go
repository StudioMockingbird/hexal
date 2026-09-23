package types

import (
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
