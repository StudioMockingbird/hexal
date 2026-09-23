package generator

import (
	"path/filepath"
	"strings"
	"testing"

	"hexal/compiler/specdata"
)

// Every hexal/<name>.{h,c} include the generator's builders spell must name a
// file some registered component declares: an undeclared include is a
// generated artifact no emission path owns. The reverse is deliberately not
// asserted: a declared file need not be included by another component.
func TestIncludeLiteralsNameDeclaredComponentFiles(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	declared := make(map[string]specdata.ComponentID)
	for _, component := range specdata.Components() {
		for _, file := range component.Files {
			declared[file] = component.ID
		}
	}
	checked := make(map[string]bool)
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		for _, literal := range fileStringLiterals(t, path) {
			if !strings.HasPrefix(literal, "hexal/") {
				continue
			}
			if !strings.HasSuffix(literal, ".h") && !strings.HasSuffix(literal, ".c") {
				continue
			}
			if strings.ContainsAny(literal, "/\\") && strings.Count(literal, "/") != 1 {
				continue
			}
			checked[literal] = true
			if owner, ok := declared[literal]; !ok {
				t.Errorf("%s spells include %q, which no registered component declares", path, literal)
			} else if owner == "" {
				t.Errorf("%s spells include %q with an empty owner", path, literal)
			}
		}
	}
	if len(checked) == 0 {
		t.Fatal("no hexal include literals were found in the generator's non-test sources")
	}
	t.Logf("include literals: %d distinct, %d declared by components", len(checked), len(declared))
}
