package checker

import (
	"testing"

	"hexal/compiler/specdata"
	compilerTypes "hexal/compiler/types"
)

// TestEqualityAvailableMatchesTheRegistry pins the comparison form each
// compiler-owned identity declares: equality is structural on a sequence
// element, never for a dictionary or a handle, and always for text, Nil, and a
// pointer identity.
func TestEqualityAvailableMatchesTheRegistry(t *testing.T) {
	environment := compilerTypes.NewEnvironment()
	list := environment.ListType(compilerTypes.Int32)
	nested := environment.ListType(environment.DictType(compilerTypes.Int32, compilerTypes.Int32))
	dict := environment.DictType(compilerTypes.Int32, compilerTypes.Int32)
	inline := environment.InlineStringType(16)
	result := compilerTypes.Int32
	fun := environment.FunType([]compilerTypes.Type{compilerTypes.Int32}, &result)

	cases := []struct {
		name string
		typ  compilerTypes.Type
		want bool
	}{
		{"List<Int32>", list, true},
		{"List<Dict<Int32,Int32>>", nested, false},
		{"Dict<Int32,Int32>", dict, false},
		{"String<16>", inline, true},
		{"String", compilerTypes.StringType, true},
		{"Nil", compilerTypes.Nil, true},
		{"Heap", compilerTypes.Heap, false},
		{"Unknown", compilerTypes.Unknown, false},
		{"IO", compilerTypes.IOType, false},
		{"Bytes", compilerTypes.BytesType, false},
		{"Fun", fun, false},
		{"Ptr<Int32>", environment.PtrType(compilerTypes.Int32), true},
	}
	for _, testCase := range cases {
		if got, _ := EqualityAvailable(testCase.typ); got != testCase.want {
			t.Errorf("EqualityAvailable(%s) = %v, want %v", testCase.name, got, testCase.want)
		}
	}
}

// TestEqualityAvailabilityReadsTheRegistry proves the comparison consumer reads
// the registry: List<Int32> is equality-comparable through its element, and
// corrupting the record's form to never must withdraw that answer.
func TestEqualityAvailabilityReadsTheRegistry(t *testing.T) {
	list := compilerTypes.NewEnvironment().ListType(compilerTypes.Int32)
	if ok, _ := EqualityAvailable(list); !ok {
		t.Fatal("List<Int32> must be equality-comparable through its element")
	}

	original := typeFacts
	typeFacts = func(compilerTypes.Type) (specdata.TypeFacts, bool) {
		return specdata.TypeFacts{Comparison: specdata.ComparisonNever}, true
	}
	defer func() { typeFacts = original }()

	if ok, _ := EqualityAvailable(list); ok {
		t.Fatal("EqualityAvailable ignored the corrupted comparison record")
	}
}

// TestOrderingAvailabilityReadsTheRegistry proves the ordering consumer reads
// the registry: an emptied registry withdraws String<16>'s ordering.
func TestOrderingAvailabilityReadsTheRegistry(t *testing.T) {
	inline := compilerTypes.NewEnvironment().InlineStringType(16)
	if !orderingAvailable(inline) {
		t.Fatal("String<16> must be ordered")
	}

	original := typeFacts
	typeFacts = func(compilerTypes.Type) (specdata.TypeFacts, bool) {
		return specdata.TypeFacts{}, false
	}
	defer func() { typeFacts = original }()

	if orderingAvailable(inline) {
		t.Fatal("orderingAvailable ignored the emptied registry")
	}
}
