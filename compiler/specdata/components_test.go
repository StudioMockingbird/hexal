package specdata

import "testing"

// Every component owns at least one file, every file has exactly one owner,
// and the file-owner query agrees with the records it is derived from.
func TestComponentFilesHaveOneOwner(t *testing.T) {
	owners := make(map[string]ComponentID)
	for _, component := range Components() {
		if component.ID == "" {
			t.Fatal("component record has an empty ID")
		}
		if len(component.Files) == 0 {
			t.Fatalf("component %s owns no files", component.ID)
		}
		for _, file := range component.Files {
			if previous, exists := owners[file]; exists {
				t.Fatalf("file %s is owned by both %s and %s", file, previous, component.ID)
			}
			owners[file] = component.ID
			owner, ok := FileOwner(file)
			if !ok || owner != component.ID {
				t.Fatalf("FileOwner(%s) = %q, %v; want %q, true", file, owner, ok, component.ID)
			}
		}
	}
}

// Every declared native dependency is referenced by at least one component,
// and no component lists the same dependency twice.
func TestDependenciesAreReferencedOnce(t *testing.T) {
	declared := Dependencies()
	if len(declared) == 0 {
		t.Fatal("no native dependencies are declared")
	}
	seen := make(map[DependencyID]bool, len(declared))
	for _, dependency := range declared {
		if dependency.ID == "" {
			t.Fatal("dependency record has an empty ID")
		}
		if seen[dependency.ID] {
			t.Fatalf("dependency %s is declared twice", dependency.ID)
		}
		seen[dependency.ID] = true
		if _, ok := Dependency(dependency.ID); !ok {
			t.Fatalf("Dependency(%s) reports undeclared", dependency.ID)
		}
	}
	references := make(map[DependencyID]int, len(declared))
	for _, component := range Components() {
		within := make(map[DependencyID]bool)
		for _, dependency := range component.RuntimeDependencies {
			if within[dependency] {
				t.Fatalf("component %s lists dependency %s twice", component.ID, dependency)
			}
			within[dependency] = true
			if !seen[dependency] {
				t.Fatalf("component %s names undeclared dependency %s", component.ID, dependency)
			}
			references[dependency]++
		}
	}
	for _, dependency := range declared {
		if references[dependency.ID] == 0 {
			t.Fatalf("dependency %s is declared but no component references it", dependency.ID)
		}
	}
}

// Registry queries hand back copies, so a consumer cannot mutate the facts for
// one compilation and leak them into the next.
func TestRegistryQueriesReturnDefensiveCopies(t *testing.T) {
	first, ok := Component(ComponentHeap)
	if !ok {
		t.Fatal("Component(ComponentHeap) not found")
	}
	first.Files[0] = "mutated"
	first.RuntimeDependencies[0] = "mutated"
	first.RequiredCHeaders[0] = "mutated"
	second, _ := Component(ComponentHeap)
	if second.Files[0] == "mutated" || second.RuntimeDependencies[0] == "mutated" || second.RequiredCHeaders[0] == "mutated" {
		t.Fatal("Component returned slices aliasing the registry")
	}
	all := Components()
	all[0].Files[0] = "mutated"
	again := Components()
	if again[0].Files[0] == "mutated" {
		t.Fatal("Components returned slices aliasing the registry")
	}
	ids := ConcreteTypeIDs()
	ids[0] = "mutated"
	if again := ConcreteTypeIDs(); again[0] == "mutated" {
		t.Fatal("ConcreteTypeIDs returned slices aliasing the registry")
	}
}
