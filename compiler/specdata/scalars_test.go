package specdata

import "testing"

// validScalarSet is the smallest set validateScalars accepts, so each rejection
// case changes exactly one fact.
func validScalarSet() TargetScalars {
	return TargetScalars{
		Target: TargetWindowsUCRT,
		Mappings: []CScalarMapping{
			{CSpelling: "long", HexalType: TypeInt32},
			{CSpelling: "unsigned long", HexalType: TypeUInt32},
		},
	}
}

// validateScalars rejects a registry that would change foreign ABI behavior
// silently: an empty or unknown target, an empty or repeated spelling, an
// identity no concrete type declares, and a long family that is neither the
// LP64 nor the LLP64 answer.
func TestValidateScalarsRejectsMalformedRegistry(t *testing.T) {
	original := targetScalars
	defer func() { targetScalars = original }()
	withMapping := func(mapping CScalarMapping) []TargetScalars {
		set := validScalarSet()
		set.Mappings[0] = mapping
		return []TargetScalars{set}
	}
	for _, testCase := range []struct {
		name     string
		registry []TargetScalars
	}{
		{"empty target", []TargetScalars{{Mappings: validScalarSet().Mappings}}},
		{"unknown target", []TargetScalars{{Target: "missing", Mappings: validScalarSet().Mappings}}},
		{"empty spelling", withMapping(CScalarMapping{CSpelling: "", HexalType: TypeInt32})},
		{"unknown type", withMapping(CScalarMapping{CSpelling: "long", HexalType: TypeID("Missing")})},
		{"constructor type", withMapping(CScalarMapping{CSpelling: "long", HexalType: TypeList})},
		{"long neither answer", withMapping(CScalarMapping{CSpelling: "long", HexalType: TypeInt16})},
		{"long and unsigned long disagree", []TargetScalars{{
			Target: TargetWindowsUCRT,
			Mappings: []CScalarMapping{
				{CSpelling: "long", HexalType: TypeInt64},
				{CSpelling: "unsigned long", HexalType: TypeUInt32},
			},
		}}},
		{"long missing", []TargetScalars{{
			Target:   TargetWindowsUCRT,
			Mappings: []CScalarMapping{{CSpelling: "unsigned long", HexalType: TypeUInt32}},
		}}},
		{"repeated spelling", []TargetScalars{{
			Target: TargetWindowsUCRT,
			Mappings: []CScalarMapping{
				{CSpelling: "long", HexalType: TypeInt32},
				{CSpelling: "long", HexalType: TypeInt64},
				{CSpelling: "unsigned long", HexalType: TypeUInt32},
			},
		}}},
		{"repeated target", []TargetScalars{validScalarSet(), validScalarSet()}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			targetScalars = testCase.registry
			if err := validateScalars(); err == nil {
				t.Fatalf("validateScalars() = nil, want a rejection for %s", testCase.name)
			}
		})
	}
}

// CScalar and CScalars are the two consumer queries; a target or spelling the
// registry does not declare reports absent rather than producing a zero
// mapping, and CScalars copies so a caller cannot reach the registry.
func TestCScalarQueries(t *testing.T) {
	if _, ok := CScalar(TargetWindowsUCRT, "long"); !ok {
		t.Fatal("windows long must resolve")
	}
	if _, ok := CScalar("missing", "long"); ok {
		t.Fatal("an unknown target must not resolve")
	}
	if _, ok := CScalar(TargetWindowsUCRT, "long double"); ok {
		t.Fatal("an unregistered spelling must not resolve")
	}
	if got := CScalars("missing"); got != nil {
		t.Fatalf("CScalars(unknown) = %v, want nil", got)
	}
	mappings := CScalars(TargetWindowsUCRT)
	if len(mappings) == 0 {
		t.Fatal("windows must declare mappings")
	}
	mappings[0].CSpelling = "mutated"
	if again, _ := CScalar(TargetWindowsUCRT, "mutated"); again.CSpelling == "mutated" {
		t.Fatal("CScalars returned the registry's own slice")
	}
}
