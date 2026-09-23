package specdata

import "testing"

// The operator, widening, and conversion matrices are the sole authority for
// the facts they hold. Each query is exercised against a swapped record set: an
// answer that did not move with the records would mean the consumer reads a
// second, hidden table.
func TestOperatorQueryReadsTheRecords(t *testing.T) {
	saved := operators
	defer func() { operators = saved }()
	if _, ok := Operator(OperatorAdd); !ok {
		t.Fatal("Operator(OperatorAdd) not found in the registry")
	}
	operators = nil
	if _, ok := Operator(OperatorAdd); ok {
		t.Fatal("Operator(OperatorAdd) survived an emptied registry; the query ignores its records")
	}
}

func TestWideningQueriesReadTheRecords(t *testing.T) {
	savedPairs := wideningPairs
	savedRanks := numericRanks
	defer func() { wideningPairs, numericRanks = savedPairs, savedRanks }()
	if !Widening(TypeInt8, TypeInt16) {
		t.Fatal("Widening(Int8, Int16) is not registered")
	}
	if rank, ok := NumericRank(TypeInt8); !ok || rank != 0 {
		t.Fatalf("NumericRank(Int8) = %d, %v; want 0, true", rank, ok)
	}
	wideningPairs, numericRanks = nil, nil
	if Widening(TypeInt8, TypeInt16) {
		t.Fatal("Widening(Int8, Int16) survived an emptied registry; the query ignores its records")
	}
	if _, ok := NumericRank(TypeInt8); ok {
		t.Fatal("NumericRank(Int8) survived an emptied registry; the query ignores its records")
	}
}

func TestConversionQueryReadsTheRecords(t *testing.T) {
	saved := conversions
	defer func() { conversions = saved }()
	if !Conversion(TypeInt32, TypeFloat64) {
		t.Fatal("Conversion(Int32, Float64) is not registered")
	}
	conversions = nil
	if Conversion(TypeInt32, TypeFloat64) {
		t.Fatal("Conversion(Int32, Float64) survived an emptied registry; the query ignores its records")
	}
}

// The conversion matrix ranges over the whole numeric identity domain in both
// directions, identity included.
func TestConversionMatrixCoversTheNumericDomain(t *testing.T) {
	domain := make(map[TypeID]bool, len(numericIdentities))
	for _, id := range numericIdentities {
		domain[id] = true
	}
	for _, from := range numericIdentities {
		for _, to := range numericIdentities {
			if !Conversion(from, to) {
				t.Fatalf("Conversion(%s, %s) is missing", from, to)
			}
		}
	}
	if got, want := len(Conversions()), len(domain)*len(domain); got != want {
		t.Fatalf("Conversions() has %d records, want %d", got, want)
	}
	// A non-numeric identity is outside the domain in both directions.
	if Conversion(TypeInt32, TypeBool) || Conversion(TypeBool, TypeInt32) {
		t.Fatal("Conversion admits Bool, which is not a numeric identity")
	}
}

// Registry queries clone the slices a record owns, so a consumer cannot mutate
// the facts for one compilation and leak them into the next.
func TestOperatorQueriesReturnDefensiveCopies(t *testing.T) {
	first, ok := Operator(OperatorAdd)
	if !ok {
		t.Fatal("Operator(OperatorAdd) not found")
	}
	first.Operands[0] = "mutated"
	second, _ := Operator(OperatorAdd)
	if second.Operands[0] == "mutated" {
		t.Fatal("Operator returned a slice aliasing the registry")
	}
	all := Operators()
	all[0].Operands[0] = "mutated"
	again := Operators()
	if again[0].Operands[0] == "mutated" {
		t.Fatal("Operators returned a slice aliasing the registry")
	}
}

// validateOperators rejects each shape of untrustworthy record. One case per
// named rejection: an empty identifier, a repeated key, an undeclared operand
// type identity, a widening pair that is not a widening, and a conversion that
// leaves the numeric domain or drops a direction.
func TestValidateOperatorsRejectsBadRecords(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		apply func()
	}{
		{"empty operator id", func() {
			operators = []OperatorSpec{{AnyOperand: true}}
		}},
		{"repeated operator", func() {
			operators = []OperatorSpec{
				{ID: OperatorAdd, Operands: []TypeID{TypeInt8}},
				{ID: OperatorAdd, Operands: []TypeID{TypeInt8}},
			}
		}},
		{"undeclared operand type", func() {
			operators = []OperatorSpec{{ID: OperatorAdd, Operands: []TypeID{"NoSuchType"}}}
		}},
		{"both operand forms", func() {
			operators = []OperatorSpec{{ID: OperatorAdd, Operands: []TypeID{TypeInt8}, AnyOperand: true}}
		}},
		{"neither operand form", func() {
			operators = []OperatorSpec{{ID: OperatorAdd}}
		}},
		{"widening runs backwards", func() {
			wideningPairs = []WideningSpec{{From: TypeFloat64, To: TypeInt8}}
		}},
		{"widening is identity", func() {
			wideningPairs = []WideningSpec{{From: TypeInt8, To: TypeInt8}}
		}},
		{"widening names a non-numeric type", func() {
			wideningPairs = []WideningSpec{{From: TypeInt8, To: TypeRune}}
		}},
		{"widening repeats a pair", func() {
			wideningPairs = []WideningSpec{
				{From: TypeInt8, To: TypeInt16},
				{From: TypeInt8, To: TypeInt16},
			}
		}},
		{"conversion leaves the numeric domain", func() {
			conversions = []ConversionSpec{{From: TypeInt8, To: TypeString}}
		}},
		{"conversion drops a direction", func() {
			conversions = conversions[:len(conversions)-1]
		}},
		{"conversion repeats a direction", func() {
			conversions = []ConversionSpec{{From: TypeInt8, To: TypeInt16}, {From: TypeInt8, To: TypeInt16}}
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			savedOperators, savedPairs, savedConversions := operators, wideningPairs, conversions
			defer func() { operators, wideningPairs, conversions = savedOperators, savedPairs, savedConversions }()
			testCase.apply()
			if err := validateOperators(); err == nil {
				t.Fatalf("validateOperators() = nil, want an error")
			}
		})
	}
}
