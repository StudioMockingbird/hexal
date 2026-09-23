package specdata

import "testing"

// TestFactsReadsTheRegistry proves the Facts query reads the live registries
// rather than a copy: corrupting a constructor record's position mask and a
// concrete record's position mask each moves the answer. The two registries are
// checked separately because Facts resolves each through a different path.
func TestFactsReadsTheRegistry(t *testing.T) {
	before, ok := Facts(TypeAtomic)
	if !ok {
		t.Fatal("Facts(TypeAtomic) is missing")
	}
	if before.Positions != StorableConstructionOnly {
		t.Fatalf("Atomic positions = %#x, want construction-only", before.Positions)
	}
	constructorIndex := -1
	for index := range typeConstructors {
		if typeConstructors[index].ID == TypeAtomic {
			constructorIndex = index
		}
	}
	if constructorIndex < 0 {
		t.Fatal("Atomic constructor record not found")
	}
	originalConstructor := typeConstructors[constructorIndex].Facts.Positions
	typeConstructors[constructorIndex].Facts.Positions = StorableEverywhere
	defer func() { typeConstructors[constructorIndex].Facts.Positions = originalConstructor }()
	if after, _ := Facts(TypeAtomic); after.Positions != StorableEverywhere {
		t.Fatalf("Facts(TypeAtomic) = %#x after corruption, want the corrupted mask", after.Positions)
	}

	concreteBefore, ok := Facts(TypeNil)
	if !ok {
		t.Fatal("Facts(TypeNil) is missing")
	}
	if concreteBefore.Positions != StorableUnionMemberOnly {
		t.Fatalf("Nil positions = %#x, want union-member-only", concreteBefore.Positions)
	}
	concreteIndex := -1
	for index := range concreteFacts {
		if concreteFacts[index].ID == TypeNil {
			concreteIndex = index
		}
	}
	if concreteIndex < 0 {
		t.Fatal("Nil concrete record not found")
	}
	originalConcrete := concreteFacts[concreteIndex].Facts.Positions
	concreteFacts[concreteIndex].Facts.Positions = StorableNowhere
	defer func() { concreteFacts[concreteIndex].Facts.Positions = originalConcrete }()
	if after, _ := Facts(TypeNil); after.Positions != StorableNowhere {
		t.Fatalf("Facts(TypeNil) = %#x after corruption, want nowhere", after.Positions)
	}
}
