package snippets_test

import (
	"testing"

	"hexal/workbench/snippets"
)

func TestCatalogLoadsAndCoversLanguage(t *testing.T) {
	catalog, err := snippets.Load()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog) < 2 {
		t.Fatalf("catalog has %d categories, want dependent-selector choices", len(catalog))
	}
}
