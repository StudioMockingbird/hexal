package generator

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hexal/compiler/specdata"
)

// Every registry component is claimed by exactly one explicit Go builder, and
// every builder claims only registered components. This is the mechanical
// statement that the builders still own the component set: adding a component
// without a builder, or a builder without a record, fails here.
func TestComponentDemandsCoverTheRegistry(t *testing.T) {
	components := specdata.Components()
	claimed := make(map[specdata.ComponentID]int, len(components))
	for _, demand := range componentDemands(Config{SourceTable: testSpanTable}) {
		if demand.build == nil {
			t.Fatal("component demand has a nil builder")
		}
		if len(demand.ids) == 0 {
			t.Fatal("component demand claims no registry identity")
		}
		for _, id := range demand.ids {
			claimed[id]++
		}
	}
	for _, component := range components {
		if claimed[component.ID] != 1 {
			t.Errorf("component %s is claimed %d times by builders, want exactly once", component.ID, claimed[component.ID])
		}
	}
	for id := range claimed {
		if _, ok := specdata.Component(id); !ok {
			t.Errorf("builder claims unregistered component %s", id)
		}
	}
}

// Demand is decided by the builders' Go bodies, not by registry data. An
// emission with no discovered state selects nothing; adding only the trap
// requirement selects only the runtime component.
func TestComponentDemandStaysInBuilders(t *testing.T) {
	empty, err := renderComponentArtifacts(&programEmission{}, Config{SourceTable: testSpanTable})
	if err != nil {
		t.Fatalf("renderComponentArtifacts(empty) error = %v", err)
	}
	if len(empty) != 0 {
		t.Fatalf("empty program emitted %v, want no artifacts", empty)
	}
	trapped, err := renderComponentArtifacts(&programEmission{requirements: &cHeaderRequirements{trap: true}}, Config{SourceTable: testSpanTable})
	if err != nil {
		t.Fatalf("renderComponentArtifacts(trap) error = %v", err)
	}
	if len(trapped) != 1 || trapped["hexal/runtime.c"] == "" {
		t.Fatalf("trap-only program emitted %v, want only hexal/runtime.c", trapped)
	}
}

// Each native dependency identity is declared in exactly one non-test Go
// source file of the compiler. The registry owns the names; runtime_dependency.go
// aliases them and the generator references those aliases, so the name never
// has a second declaration site inside the compiler. The build driver derives
// its pack-manifest ordering from the same registry.
func TestRuntimeDependencyIdentityIsDeclaredOnce(t *testing.T) {
	identities := map[string]bool{}
	for _, dependency := range specdata.Dependencies() {
		identities[string(dependency.ID)] = true
	}
	if len(identities) == 0 {
		t.Fatal("no native dependency identities are declared")
	}
	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.WalkDir(filepath.Join(root, "compiler"), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if entry.IsDir() {
			if entry.Name() == "testdata" {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)
		for _, literal := range fileStringLiterals(t, path) {
			if identities[literal] && rel != "compiler/specdata/components.go" {
				offenders = append(offenders, rel+": "+literal)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walking compiler: %v", err)
	}
	if len(offenders) != 0 {
		t.Fatalf("dependency identities declared outside compiler/specdata/components.go: %v", offenders)
	}
}

// fileStringLiterals returns every decoded string literal in one Go file.
func fileStringLiterals(t *testing.T, path string) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}
	var literals []string
	ast.Inspect(file, func(node ast.Node) bool {
		basic, ok := node.(*ast.BasicLit)
		if !ok || basic.Kind != token.STRING {
			return true
		}
		value, unquoteErr := strconv.Unquote(basic.Value)
		if unquoteErr == nil {
			literals = append(literals, value)
		}
		return true
	})
	return literals
}
