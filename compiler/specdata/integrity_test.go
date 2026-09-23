package specdata

import (
	"strings"
	"testing"
)

// TestValidateCrossDomainAcceptsTheRegistry pins the real registries against the
// cross-domain pass, so a legal record set is proved not to trip a check.
func TestValidateCrossDomainAcceptsTheRegistry(t *testing.T) {
	if err := validateCrossDomain(); err != nil {
		t.Fatalf("validateCrossDomain() = %v, want nil", err)
	}
}

// TestValidateWiresCrossDomain proves Validate calls the pass: a core-library
// type export that validateCorelib accepts but that names no registered type
// identifier is rejected only by the cross-domain reference check.
func TestValidateWiresCrossDomain(t *testing.T) {
	original := coreModules
	defer func() { coreModules = original }()
	coreModules = []CoreModule{{
		ID:    "std/demo",
		Types: []CoreTypeExport{{Name: "Ghost", TypeID: "Nope"}},
	}}
	err := Validate()
	if err == nil {
		t.Fatal("Validate() accepted a core type export that names no registered type identifier")
	}
	if !strings.Contains(err.Error(), "unknown type identifier") {
		t.Fatalf("Validate() error = %q, want the cross-domain missing-reference rejection", err)
	}
}

// TestValidateCrossDomainRejectsMissingReferences covers each cross-domain
// reference direction: a component's dependency, a type's and method's
// component, a method's owner and type references, a core-library type export
// and function component, an error kind's header constructor, a scalar set's
// target and mapping, and an operator, rank, widening, and conversion identity.
func TestValidateCrossDomainRejectsMissingReferences(t *testing.T) {
	const (
		wantUnknownDependency  = "unknown dependency"
		wantUnknownComponent   = "unknown component"
		wantUnknownConstructor = "unknown constructor"
		wantUnknownConcrete    = "unknown concrete type"
		wantUnknownTypeID      = "unknown type identifier"
		wantUnknownTarget      = "unknown target"
		wantUnknownOperand     = "unknown operand type"
		wantUnknownHeader      = "unknown header constructor"
	)
	cases := []struct {
		name    string
		records domainRecords
		want    string
	}{
		{"component dependency", domainRecords{
			components: []ComponentSpec{{ID: "c", Files: []string{"c.h"}, RuntimeDependencies: []DependencyID{"ghost"}}},
		}, wantUnknownDependency},
		{"type component", domainRecords{
			constructors: []TypeConstructorSpec{{ID: "T", Facts: ConstructorFacts{Component: "ghost"}}},
		}, wantUnknownComponent},
		{"method owner constructor", domainRecords{
			methods: []MethodSpec{{Owner: ConstructorOwner("Nope"), Name: "m", Component: "c"}},
		}, wantUnknownConstructor},
		{"method owner concrete type", domainRecords{
			methods: []MethodSpec{{Owner: ExactOwner("Nope"), Name: "m", Component: "c"}},
		}, wantUnknownConcrete},
		{"method component", domainRecords{
			concretes: []TypeID{TypeString},
			methods:   []MethodSpec{{Owner: ExactOwner(TypeString), Name: "m", Component: "ghost"}},
		}, wantUnknownComponent},
		{"method parameter concrete type", domainRecords{
			concretes:  []TypeID{TypeString},
			components: []ComponentSpec{{ID: "c", Files: []string{"c.h"}}},
			methods: []MethodSpec{{
				Owner:      ExactOwner(TypeString),
				Name:       "m",
				Component:  "c",
				Parameters: []ParameterSpec{{Name: "x", Type: ConcreteType("Nope")}},
			}},
		}, wantUnknownConcrete},
		{"method applied constructor", domainRecords{
			concretes:  []TypeID{TypeString},
			components: []ComponentSpec{{ID: "c", Files: []string{"c.h"}}},
			methods: []MethodSpec{{
				Owner:     ExactOwner(TypeString),
				Name:      "m",
				Component: "c",
				Result:    ResultSpec{Type: AppliedType("Nope", AccessReadOnly, ConcreteType(TypeInt32))},
			}},
		}, wantUnknownConstructor},
		{"core type export", domainRecords{
			modules: []CoreModule{{ID: "std/demo", Types: []CoreTypeExport{{Name: "T", TypeID: "Nope"}}}},
		}, wantUnknownTypeID},
		{"core function component", domainRecords{
			modules: []CoreModule{{ID: "std/demo", Functions: []CoreFunction{{
				Name: "f", Runtime: "hex_demo_f", Components: []ComponentID{"ghost"},
			}}}},
		}, wantUnknownComponent},
		{"error kind header constructor", domainRecords{
			errorKinds: []ErrorKindSpec{{ID: "Other", HeaderType: "Nope"}},
		}, wantUnknownHeader},
		{"scalar set target", domainRecords{
			concretes: []TypeID{TypeInt64},
			scalars:   []TargetScalars{{Target: "ghost", Mappings: []CScalarMapping{{CSpelling: "long", HexalType: TypeInt64}}}},
		}, wantUnknownTarget},
		{"scalar mapping type", domainRecords{
			targets: []TargetFacts{{ID: TargetLinuxGNU, OS: "linux", Architecture: "x86_64", PointerWidth: 64, SizeWidth: 64, Threading: "posix"}},
			scalars: []TargetScalars{{Target: TargetLinuxGNU, Mappings: []CScalarMapping{{CSpelling: "long", HexalType: "Nope"}}}},
		}, wantUnknownConcrete},
		{"operator operand", domainRecords{
			operators: []OperatorSpec{{ID: "add", Operands: []TypeID{"Nope"}}},
		}, wantUnknownOperand},
		{"numeric rank", domainRecords{
			ranks: []NumericRankSpec{{ID: "Nope", Rank: 0}},
		}, wantUnknownConcrete},
		{"widening endpoint", domainRecords{
			concretes: []TypeID{TypeInt32},
			widenings: []WideningSpec{{From: "Nope", To: TypeInt32}},
		}, wantUnknownConcrete},
		{"conversion endpoint", domainRecords{
			concretes:   []TypeID{TypeInt32},
			conversions: []ConversionSpec{{From: "Nope", To: TypeInt32}},
		}, wantUnknownConcrete},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateDomainSet(testCase.records)
			if err == nil {
				t.Fatalf("validateDomainSet accepted a missing reference")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validateDomainSet error = %q, want %q", err, testCase.want)
			}
		})
	}
}

// TestFindReferenceCycleDetectsAClosedPath proves the cycle detector has teeth.
// The real record fields cannot close a cross-domain cycle, because every
// cross-domain edge points from a consumer domain (method, type, error kind,
// core export, operator) to a fact domain (type, component, target) whose only
// outgoing edge reaches a further leaf; the detector is therefore exercised on a
// synthetic graph, and TestValidateCrossDomainAcceptsTheRegistry asserts the
// real graph is acyclic.
func TestFindReferenceCycleDetectsAClosedPath(t *testing.T) {
	cyclic := []integrityEdge{
		{from: nodeComponent("a"), to: nodeDependency("d")},
		{from: nodeConstructor("T"), to: nodeComponent("a")},
		{from: nodeDependency("d"), to: nodeConstructor("T")},
	}
	cycle := findReferenceCycle(cyclic)
	if cycle == nil {
		t.Fatal("findReferenceCycle(cyclic) = nil, want a cycle")
	}
	if !strings.Contains(renderCycle(cycle), "->") {
		t.Fatalf("renderCycle(cycle) = %q, want a rendered path", renderCycle(cycle))
	}
	if err := validateDomainSet(domainRecords{}); err != nil {
		t.Fatalf("validateDomainSet(empty) = %v, want nil", err)
	}
	acyclic := []integrityEdge{{from: nodeConstructor("T"), to: nodeComponent("a")}}
	if findReferenceCycle(acyclic) != nil {
		t.Fatal("findReferenceCycle(acyclic) reported a cycle")
	}
	if findReferenceCycle(nil) != nil {
		t.Fatal("findReferenceCycle(nil) reported a cycle")
	}
}

// TestValidateSymbolOwnershipRejectsDisagreement proves the rule distinguishes
// legitimate one-helper sharing from records that disagree. Two records may
// share a symbol only when they name the same component set and the same
// failure behavior.
func TestValidateSymbolOwnershipRejectsDisagreement(t *testing.T) {
	shared := "hex_shared"
	legitimate := domainRecords{methods: []MethodSpec{
		{Owner: ConstructorOwner(TypeList), Name: "a", RuntimeSymbol: shared, Component: ComponentList},
		{Owner: ConstructorOwner(TypeInlineString), Name: "b", RuntimeSymbol: shared, Component: ComponentList},
	}}
	if err := validateSymbolOwnership(legitimate); err != nil {
		t.Fatalf("validateSymbolOwnership(legitimate sharing) = %v, want nil", err)
	}

	differentComponent := domainRecords{methods: []MethodSpec{
		{Owner: ConstructorOwner(TypeList), Name: "a", RuntimeSymbol: shared, Component: ComponentList},
		{Owner: ConstructorOwner(TypeString), Name: "b", RuntimeSymbol: shared, Component: ComponentString},
	}}
	if err := validateSymbolOwnership(differentComponent); err == nil || !strings.Contains(err.Error(), "different components") {
		t.Fatalf("validateSymbolOwnership(different components) = %v, want a component disagreement", err)
	}

	differentFailure := domainRecords{methods: []MethodSpec{
		{Owner: ConstructorOwner(TypeList), Name: "a", RuntimeSymbol: shared, Component: ComponentList},
		{Owner: ConstructorOwner(TypeString), Name: "b", RuntimeSymbol: shared, Component: ComponentList, Failure: FailureChecked},
	}}
	if err := validateSymbolOwnership(differentFailure); err == nil || !strings.Contains(err.Error(), "different failure modes") {
		t.Fatalf("validateSymbolOwnership(different failure) = %v, want a failure disagreement", err)
	}

	coreShare := domainRecords{modules: []CoreModule{{ID: "std/demo", Functions: []CoreFunction{
		{Name: "a", Runtime: shared, Components: []ComponentID{ComponentProgram}},
		{Name: "b", Runtime: shared, Components: []ComponentID{ComponentEntropy}},
	}}}}
	if err := validateSymbolOwnership(coreShare); err == nil || !strings.Contains(err.Error(), "different components") {
		t.Fatalf("validateSymbolOwnership(core function disagreement) = %v, want a component disagreement", err)
	}
}

// TestValidateABICompletenessRejectsIncompleteRecords covers the four ABI roles
// the records express: a target without exactly one non-empty foreign-ABI
// record or without widths, and a type, method, or runtime function demanding a
// component that owns no generated file.
func TestValidateABICompletenessRejectsIncompleteRecords(t *testing.T) {
	target := TargetFacts{ID: TargetLinuxGNU, OS: "linux", Architecture: "x86_64", PointerWidth: 64, SizeWidth: 64, Threading: "posix"}
	mapping := CScalarMapping{CSpelling: "long", HexalType: TypeInt64}
	cases := []struct {
		name    string
		records domainRecords
		want    string
	}{
		{"target without a foreign-ABI record", domainRecords{
			targets: []TargetFacts{target},
		}, "no foreign-ABI scalar mapping record"},
		{"target with two foreign-ABI records", domainRecords{
			targets: []TargetFacts{target},
			scalars: []TargetScalars{
				{Target: TargetLinuxGNU, Mappings: []CScalarMapping{mapping}},
				{Target: TargetLinuxGNU, Mappings: []CScalarMapping{mapping}},
			},
		}, "want one"},
		{"empty foreign-ABI record", domainRecords{
			targets: []TargetFacts{target},
			scalars: []TargetScalars{{Target: TargetLinuxGNU}},
		}, "empty foreign-ABI scalar mapping record"},
		{"target without widths", domainRecords{
			targets: []TargetFacts{{ID: TargetLinuxGNU, OS: "linux", Architecture: "x86_64", Threading: "posix"}},
			scalars: []TargetScalars{{Target: TargetLinuxGNU, Mappings: []CScalarMapping{mapping}}},
		}, "no pointer or size width"},
		{"type demands a fileless component", domainRecords{
			components:   []ComponentSpec{{ID: "empty"}},
			constructors: []TypeConstructorSpec{{ID: "T", Facts: ConstructorFacts{Component: "empty"}}},
		}, "owns no ABI file"},
		{"method demands a fileless component", domainRecords{
			components: []ComponentSpec{{ID: "empty"}},
			methods:    []MethodSpec{{Owner: ExactOwner(TypeList), Name: "m", Component: "empty"}},
		}, "owns no ABI file"},
		{"function demands a fileless component", domainRecords{
			components: []ComponentSpec{{ID: "empty"}},
			modules: []CoreModule{{ID: "std/demo", Functions: []CoreFunction{{
				Name: "f", Runtime: "hex_demo_f", Components: []ComponentID{"empty"},
			}}}},
		}, "owns no ABI file"},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateABICompleteness(testCase.records)
			if err == nil {
				t.Fatalf("validateABICompleteness accepted an incomplete ABI record")
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("validateABICompleteness error = %q, want %q", err, testCase.want)
			}
		})
	}

	complete := domainRecords{
		targets:      []TargetFacts{target},
		scalars:      []TargetScalars{{Target: TargetLinuxGNU, Mappings: []CScalarMapping{mapping}}},
		components:   []ComponentSpec{{ID: "c", Files: []string{"c.h"}}},
		constructors: []TypeConstructorSpec{{ID: "T", Facts: ConstructorFacts{Component: "c"}}},
	}
	if err := validateABICompleteness(complete); err != nil {
		t.Fatalf("validateABICompleteness(complete) = %v, want nil", err)
	}
}
