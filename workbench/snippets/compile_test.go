package snippets_test

import (
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

// Every workbench example is also a public-API compiler smoke test. This keeps
// the executable examples from drifting away from the language implementation.
func TestCatalogProgramsCompile(t *testing.T) {
	catalog, err := snippets.Load()
	if err != nil {
		t.Fatal(err)
	}

	for _, category := range catalog {
		for _, snippet := range category.Snippets {
			t.Run(category.ID+"/"+snippet.ID, func(t *testing.T) {
				if snippet.ID == "module-import-export" {
					t.Skip("cross-module resolution is not implemented yet")
				}
				result := compiler.Compile(snippet.Sources, snippet.Entrypoint)
				if result.ExitCode != compiler.ExitSuccess {
					t.Fatalf("snippet did not compile:\n%s", result.Stderr)
				}
				for _, name := range []string{"main.c", "main.h"} {
					if result.Files[name] == "" {
						t.Errorf("generated file %q is missing or empty", name)
					}
				}
			})
		}
	}
}
