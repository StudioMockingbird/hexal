//go:build c23

package c23validation

// The single runner over the fixture catalog. Every fixture enters Tier 1
// unconditionally; a zero-exit expectation additionally runs Tier 2, and a
// non-zero expectation additionally runs Tier 3. There is no parallel
// tier-registration list, so a fixture cannot be added here and silently
// missed by one tier.

import (
	"testing"

	"hexal/workbench/snippets"
)

// allSnippets loads every catalog snippet once, matching the equivalent
// helper the fuzz test package uses.
func allSnippets(t *testing.T) []snippets.Snippet {
	t.Helper()
	categories, err := snippets.Load()
	if err != nil {
		t.Fatalf("snippets.Load() error = %v", err)
	}
	all := make([]snippets.Snippet, 0)
	for _, category := range categories {
		all = append(all, category.Snippets...)
	}
	if len(all) == 0 {
		t.Fatal("snippet catalog is empty")
	}
	return all
}

// TestC23Suite drives every applicable fixture in fixtureCatalog through
// the tiers its own declared expectation selects. buildRoot is this test's
// own t.TempDir(), shared by every fixture and toolchain build below it so
// the compile cache can hand out an executable that outlives the specific
// subtest that first built it.
func TestC23Suite(t *testing.T) {
	buildRoot := t.TempDir()
	for _, f := range fixtureCatalog {
		if !f.appliesToHost() {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			result := f.resolve(t)
			t.Run("compile", func(t *testing.T) {
				compileGeneratedC(t, result, buildRoot)
			})
			if f.expectation == nil {
				return
			}
			if f.expectation.zeroExit {
				t.Run("run", func(t *testing.T) {
					got := runGeneratedC(t, result, buildRoot)
					if got != f.expectation.exactStdout {
						t.Fatalf("stdout = %q, want %q", got, f.expectation.exactStdout)
					}
				})
				return
			}
			t.Run("trap", func(t *testing.T) {
				trapGeneratedC(t, result, buildRoot, f.expectation.requiredStderrSubstring)
			})
		})
	}
}

// TestC23SnippetCatalogCompiles gives every workbench snippet Tier 1
// coverage automatically -- "workbench snippets are compile fixtures
// automatically" -- without hand-listing each one in fixtureCatalog. A new
// snippet becomes a compile fixture with no edit here.
func TestC23SnippetCatalogCompiles(t *testing.T) {
	buildRoot := t.TempDir()
	for _, snippet := range allSnippets(t) {
		t.Run(snippet.ID, func(t *testing.T) {
			result := assertCompilesSources(t, snippet.Sources, snippet.Entrypoint)
			compileGeneratedC(t, result, buildRoot)
		})
	}
}
