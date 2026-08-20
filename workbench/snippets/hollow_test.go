package snippets_test

import (
	"strings"
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

// A component artifact declares only the dependencies its emitted content
// uses, so an emitted component always carries content. A hollow one (guard,
// includes, and nothing else) means some template declared a dependency it
// does not use, which is how a program with an Array but no slicing once
// emitted a 70-byte hexal/view.h.
//
// This guards the class rather than that instance: the next unconditional
// include reintroduces the defect silently, and no other test would see it.
func TestNoComponentArtifactIsHollow(t *testing.T) {
	catalog, err := snippets.Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, category := range catalog {
		for _, snippet := range category.Snippets {
			result := compiler.Compile(snippet.Sources, snippet.Entrypoint, compiler.Project{})
			if result.ExitCode != compiler.ExitSuccess {
				continue // TestCatalogProgramsCompile owns compile failures.
			}
			for name, body := range result.Files {
				if !strings.HasPrefix(name, "hexal/") || !strings.HasSuffix(name, ".h") {
					continue
				}
				if declarationCount(body) == 0 {
					t.Errorf("%s/%s emitted %s with no declaration beyond its guard and includes:\n%s",
						category.ID, snippet.ID, name, body)
				}
			}
		}
	}
}

// declarationCount counts the lines of a component header that declare
// something, ignoring the include guard, includes, blank lines, and comments.
func declarationCount(body string) int {
	declarations := 0
	for _, line := range strings.Split(body, "\n") {
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "",
			strings.HasPrefix(trimmed, "#ifndef"),
			strings.HasPrefix(trimmed, "#define HEXAL_"),
			strings.HasPrefix(trimmed, "#include"),
			strings.HasPrefix(trimmed, "#endif"),
			strings.HasPrefix(trimmed, "//"):
			continue
		}
		declarations++
	}
	return declarations
}
