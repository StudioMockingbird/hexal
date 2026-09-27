package checker

import (
	"math"
	"testing"

	"hexal/compiler/types"
)

func TestInlineListStorageEstimates(t *testing.T) {
	environment := types.NewEnvironment()
	text := environment.InlineStringType(5)
	allocated := environment.ListType(types.Int32)
	view := environment.SliceType(types.Int32, false)
	nested := environment.InlineListType(types.UInt8, 3)
	environment.BeginObject("EstimateRecord", 1, 1)
	object := environment.CompleteObject("EstimateRecord", []types.ObjectMember{
		{Name: "left", Type: types.Int32},
		{Name: "right", Type: types.Int32},
	})
	adt := environment.BeginADT("EstimateChoice", 1, 1)
	adt = environment.CompleteADT("EstimateChoice", []types.AdtVariant{{Name: "Small"}, {Name: "Large", Payload: []types.ObjectMember{{Name: "value", Type: types.Int64}}}})
	union := environment.UnionType([]types.Type{types.Int32, types.Int64})
	cases := []struct {
		name string
		typ  types.Type
		want uint64
	}{
		{"scalar", types.Int32, 4},
		{"pointer", environment.PtrType(types.Int32), 8},
		{"slice", view, 16},
		{"allocated list", allocated, 32},
		{"inline string", text, 13},
		{"nested inline list", nested, 19},
		{"object", object, 48},
		{"adt", adt, 32},
		{"union", union, 32},
		{"Error", types.ErrorType, 560},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			got, ok := estimatedStorageBytes(test.typ, make(map[string]bool))
			if !ok || got != test.want {
				t.Fatalf("estimatedStorageBytes(%s) = %d, %v; want %d, true", test.typ.Name, got, ok, test.want)
			}
		})
	}
}

func TestInlineListStorageBudgetBoundaries(t *testing.T) {
	if !inlineListWithinBudget(types.UInt8, 65_000) {
		t.Fatal("List<Byte, 65000> should fit the estimated budget")
	}
	for _, capacity := range []uint64{65_520, 65_536, math.MaxUint64} {
		if inlineListWithinBudget(types.UInt8, capacity) {
			t.Errorf("List<Byte, %d> unexpectedly fits the estimated budget", capacity)
		}
	}
	generic := types.NewEnvironment().DeclareGeneric("Estimate", 1, []string{"T"})
	parameter := types.NewEnvironment().TypeParameter(generic, 0)
	if !inlineListWithinBudget(parameter, 65_536) {
		t.Fatal("an open generic should defer its storage estimate")
	}
}
