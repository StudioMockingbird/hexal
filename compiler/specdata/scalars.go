package specdata

import "fmt"

// C scalar mappings: the canonical Hexal identity one C scalar spelling
// denotes under one target's C data model. A spelling alone names no type
// because `long` is 64-bit under LP64 and 32-bit under LLP64, so the mapping is
// target-qualified and is looked up beside a target, never by spelling alone.
//
// A mapping carries only the C spelling and the compiler-owned type identity.
// Width, signedness, and exactness are properties of that identity and of the
// header the spelling comes from, not of the mapping, so storing them here
// would be a second authority for facts compiler/types already owns.

// CScalarMapping is one C scalar spelling and the Hexal type identity it
// denotes. The identity is a concrete constructor-free TypeID: a scalar
// resolves to an interned type through ResolveSpecID, so a constructor family
// such as List never appears here.
type CScalarMapping struct {
	CSpelling string
	HexalType TypeID
}

// TargetScalars is one target's complete C scalar mapping set. A repeated
// spelling inside a set is a source-tree defect, which is why Mappings is a
// slice rather than a map: Validate reports it instead of the compiler
// silently keeping the last record.
type TargetScalars struct {
	Target   TargetID
	Mappings []CScalarMapping
}

// targetScalars is the registry. Each target declares its own complete set
// because the mapping is an ABI fact: the two sets agree on every spelling
// except the long family, and a future target whose char signedness or long
// width differs edits only its own set. It is unexported so no importer can
// rewrite a record, and queries clone the mapping slices they return.
var targetScalars = []TargetScalars{
	{
		Target: TargetWindowsUCRT,
		Mappings: []CScalarMapping{
			{CSpelling: "_Bool", HexalType: TypeBool},
			{CSpelling: "bool", HexalType: TypeBool},
			{CSpelling: "char", HexalType: TypeInt8},
			{CSpelling: "signed char", HexalType: TypeInt8},
			{CSpelling: "unsigned char", HexalType: TypeUInt8},
			{CSpelling: "short", HexalType: TypeInt16},
			{CSpelling: "short int", HexalType: TypeInt16},
			{CSpelling: "signed short", HexalType: TypeInt16},
			{CSpelling: "signed short int", HexalType: TypeInt16},
			{CSpelling: "unsigned short", HexalType: TypeUInt16},
			{CSpelling: "unsigned short int", HexalType: TypeUInt16},
			{CSpelling: "int", HexalType: TypeInt32},
			{CSpelling: "signed", HexalType: TypeInt32},
			{CSpelling: "signed int", HexalType: TypeInt32},
			{CSpelling: "unsigned", HexalType: TypeUInt32},
			{CSpelling: "unsigned int", HexalType: TypeUInt32},
			{CSpelling: "long", HexalType: TypeInt32},
			{CSpelling: "long int", HexalType: TypeInt32},
			{CSpelling: "signed long", HexalType: TypeInt32},
			{CSpelling: "signed long int", HexalType: TypeInt32},
			{CSpelling: "unsigned long", HexalType: TypeUInt32},
			{CSpelling: "unsigned long int", HexalType: TypeUInt32},
			{CSpelling: "long long", HexalType: TypeInt64},
			{CSpelling: "long long int", HexalType: TypeInt64},
			{CSpelling: "signed long long", HexalType: TypeInt64},
			{CSpelling: "signed long long int", HexalType: TypeInt64},
			{CSpelling: "unsigned long long", HexalType: TypeUInt64},
			{CSpelling: "unsigned long long int", HexalType: TypeUInt64},
			{CSpelling: "float", HexalType: TypeFloat32},
			{CSpelling: "double", HexalType: TypeFloat64},
			{CSpelling: "size_t", HexalType: TypeSize},
			{CSpelling: "int8_t", HexalType: TypeInt8},
			{CSpelling: "int16_t", HexalType: TypeInt16},
			{CSpelling: "int32_t", HexalType: TypeInt32},
			{CSpelling: "int64_t", HexalType: TypeInt64},
			{CSpelling: "uint8_t", HexalType: TypeUInt8},
			{CSpelling: "uint16_t", HexalType: TypeUInt16},
			{CSpelling: "uint32_t", HexalType: TypeUInt32},
			{CSpelling: "uint64_t", HexalType: TypeUInt64},
		},
	},
	{
		Target: TargetLinuxGNU,
		Mappings: []CScalarMapping{
			{CSpelling: "_Bool", HexalType: TypeBool},
			{CSpelling: "bool", HexalType: TypeBool},
			{CSpelling: "char", HexalType: TypeInt8},
			{CSpelling: "signed char", HexalType: TypeInt8},
			{CSpelling: "unsigned char", HexalType: TypeUInt8},
			{CSpelling: "short", HexalType: TypeInt16},
			{CSpelling: "short int", HexalType: TypeInt16},
			{CSpelling: "signed short", HexalType: TypeInt16},
			{CSpelling: "signed short int", HexalType: TypeInt16},
			{CSpelling: "unsigned short", HexalType: TypeUInt16},
			{CSpelling: "unsigned short int", HexalType: TypeUInt16},
			{CSpelling: "int", HexalType: TypeInt32},
			{CSpelling: "signed", HexalType: TypeInt32},
			{CSpelling: "signed int", HexalType: TypeInt32},
			{CSpelling: "unsigned", HexalType: TypeUInt32},
			{CSpelling: "unsigned int", HexalType: TypeUInt32},
			{CSpelling: "long", HexalType: TypeInt64},
			{CSpelling: "long int", HexalType: TypeInt64},
			{CSpelling: "signed long", HexalType: TypeInt64},
			{CSpelling: "signed long int", HexalType: TypeInt64},
			{CSpelling: "unsigned long", HexalType: TypeUInt64},
			{CSpelling: "unsigned long int", HexalType: TypeUInt64},
			{CSpelling: "long long", HexalType: TypeInt64},
			{CSpelling: "long long int", HexalType: TypeInt64},
			{CSpelling: "signed long long", HexalType: TypeInt64},
			{CSpelling: "signed long long int", HexalType: TypeInt64},
			{CSpelling: "unsigned long long", HexalType: TypeUInt64},
			{CSpelling: "unsigned long long int", HexalType: TypeUInt64},
			{CSpelling: "float", HexalType: TypeFloat32},
			{CSpelling: "double", HexalType: TypeFloat64},
			{CSpelling: "size_t", HexalType: TypeSize},
			{CSpelling: "int8_t", HexalType: TypeInt8},
			{CSpelling: "int16_t", HexalType: TypeInt16},
			{CSpelling: "int32_t", HexalType: TypeInt32},
			{CSpelling: "int64_t", HexalType: TypeInt64},
			{CSpelling: "uint8_t", HexalType: TypeUInt8},
			{CSpelling: "uint16_t", HexalType: TypeUInt16},
			{CSpelling: "uint32_t", HexalType: TypeUInt32},
			{CSpelling: "uint64_t", HexalType: TypeUInt64},
		},
	},
}

// CScalar resolves one C scalar spelling under one target's data model. The
// bool is false when the target or spelling has no record, which a caller
// reports as an unsupported spelling rather than as an empty mapping.
func CScalar(target TargetID, spelling string) (CScalarMapping, bool) {
	for _, set := range targetScalars {
		if set.Target != target {
			continue
		}
		for _, mapping := range set.Mappings {
			if mapping.CSpelling == spelling {
				return mapping, true
			}
		}
	}
	return CScalarMapping{}, false
}

// CScalars returns one target's mappings in registration order as a copy, so a
// caller shares no backing array with the registry. An unknown target returns
// nil, which is a compiler-development defect because Validate rejects a
// registry that cannot answer a registered target.
func CScalars(target TargetID) []CScalarMapping {
	for _, set := range targetScalars {
		if set.Target == target {
			return append([]CScalarMapping(nil), set.Mappings...)
		}
	}
	return nil
}

// validateScalars checks each target's mapping set: a registered target with a
// non-empty identity, declared once; no empty or repeated C spelling; every
// mapped identity a concrete compiler-owned type; and a long family that is
// exactly the LP64 or the LLP64 answer. The long rule is the check that
// matters most: a typo there would silently change foreign ABI behavior
// instead of failing a build.
func validateScalars() error {
	seenTargets := make(map[TargetID]bool, len(targetScalars))
	for _, set := range targetScalars {
		if set.Target == "" {
			return fmt.Errorf("specdata/scalars: mapping set has an empty target id")
		}
		if _, known := Target(set.Target); !known {
			return fmt.Errorf("specdata/scalars: mapping set names unknown target %q", set.Target)
		}
		if seenTargets[set.Target] {
			return fmt.Errorf("specdata/scalars: target %q is declared twice", set.Target)
		}
		seenTargets[set.Target] = true
		seenSpellings := make(map[string]bool, len(set.Mappings))
		longType, ulongType := TypeID(""), TypeID("")
		for _, mapping := range set.Mappings {
			if mapping.CSpelling == "" {
				return fmt.Errorf("specdata/scalars: target %q has a mapping with an empty C spelling", set.Target)
			}
			if seenSpellings[mapping.CSpelling] {
				return fmt.Errorf("specdata/scalars: target %q maps %q twice", set.Target, mapping.CSpelling)
			}
			seenSpellings[mapping.CSpelling] = true
			if !isConcreteTypeID(mapping.HexalType) {
				return fmt.Errorf("specdata/scalars: target %q spelling %q names unknown type %q", set.Target, mapping.CSpelling, mapping.HexalType)
			}
			switch mapping.CSpelling {
			case "long":
				longType = mapping.HexalType
			case "unsigned long":
				ulongType = mapping.HexalType
			}
		}
		if longType != TypeInt32 && longType != TypeInt64 {
			return fmt.Errorf("specdata/scalars: target %q long maps to %q, neither the LP64 Int64 nor the LLP64 Int32", set.Target, longType)
		}
		wantULong := TypeUInt32
		if longType == TypeInt64 {
			wantULong = TypeUInt64
		}
		if ulongType != wantULong {
			return fmt.Errorf("specdata/scalars: target %q unsigned long maps to %q, want %q", set.Target, ulongType, wantULong)
		}
	}
	return nil
}
