package snippets_test

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

// Every workbench example is also a public-API compiler smoke test. This keeps
// the executable examples from drifting away from the language implementation.
// The test also verifies the committed generated-artifact SHA-256 baseline, so
// any change to generated C is explicit and intentional.
func TestCatalogProgramsCompile(t *testing.T) {
	catalog, err := snippets.Load()
	if err != nil {
		t.Fatal(err)
	}

	manifest := loadGeneratedManifest(t)

	// The manifest must not name a snippet the catalog no longer has.
	for categoryID, categoryManifest := range manifest {
		var category *snippets.Category
		for index := range catalog {
			if catalog[index].ID == categoryID {
				category = &catalog[index]
				break
			}
		}
		if category == nil {
			t.Errorf("baseline manifest names unknown category %q", categoryID)
			continue
		}
		for snippetID := range categoryManifest {
			found := false
			for _, snippet := range category.Snippets {
				if snippet.ID == snippetID {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("baseline manifest names unknown snippet %s/%s", categoryID, snippetID)
			}
		}
	}

	for _, category := range catalog {
		for _, snippet := range category.Snippets {
			t.Run(category.ID+"/"+snippet.ID, func(t *testing.T) {
				result := compiler.Compile(snippet.Sources, snippet.Entrypoint, compiler.Project{})
				if result.ExitCode != compiler.ExitSuccess {
					t.Fatalf("snippet did not compile:\n%s", result.Stderr)
				}
				if result.Files["hexal.h"] == "" {
					t.Errorf("generated file \"hexal.h\" is missing or empty")
				}
				verifyManifestEntry(t, manifest, category.ID, snippet.ID, result.Files)
			})
		}
	}
	for _, warning := range snippets.LineLimitWarnings(catalog) {
		t.Logf("note: %s", warning)
	}
}

// loadGeneratedManifest reads the committed baseline of every catalog
// snippet's generated artifacts.
func loadGeneratedManifest(t *testing.T) map[string]map[string]map[string]string {
	t.Helper()
	data, err := os.ReadFile("testdata/generated-c-sha256.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest map[string]map[string]map[string]string
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode generated-artifact baseline: %v", err)
	}
	return manifest
}

// verifyManifestEntry compares one snippet's generated artifacts against the
// baseline: a missing or extra artifact or any hash mismatch fails with the
// exact snippet and filename.
func verifyManifestEntry(t *testing.T, manifest map[string]map[string]map[string]string, categoryID, snippetID string, files map[string]string) {
	t.Helper()
	categoryManifest, ok := manifest[categoryID]
	if !ok {
		t.Fatalf("baseline manifest has no entry for category %q", categoryID)
	}
	want, ok := categoryManifest[snippetID]
	if !ok {
		t.Fatalf("baseline manifest has no entry for snippet %s/%s", categoryID, snippetID)
	}
	got := make(map[string]string, len(files))
	for name, content := range files {
		sum := sha256.Sum256([]byte(content))
		got[name] = hex.EncodeToString(sum[:])
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("snippet %s/%s: artifact %q is missing from the generated output", categoryID, snippetID, name)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("snippet %s/%s: unexpected generated artifact %q", categoryID, snippetID, name)
		}
	}
	for name, hash := range got {
		if want[name] != hash {
			t.Errorf("snippet %s/%s: artifact %q changed (baseline %s, got %s)", categoryID, snippetID, name, want[name], hash)
		}
	}
}
