//go:build c23

package c23validation

// The fixture catalog: the suite's single source of truth for what gets
// compiled, run, and trapped, and on which hosts. A fixture names exactly
// one program (a workbench snippet ID or inline sources), an optional
// process expectation, and the hosts that expectation applies to. Tier 1
// (compile) is implicit for every fixture; a zero-exit expectation derives
// Tier 2 (exact output), a non-zero expectation derives Tier 3 (trap). There
// is no separate tier-registration list, so a fixture cannot be added to one
// tier and silently missed by another.

import (
	"runtime"
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

// processExpectation is a fixture's optional runtime contract. Exactly one
// of the two forms below applies, selected by zeroExit.
type processExpectation struct {
	// zeroExit selects Tier 2 (true, exact stdout) or Tier 3 (false, a
	// required stderr substring naming the runtime trap).
	zeroExit bool
	// exactStdout is Tier 2's complete expected stdout, after normalizing
	// "\r\n" to "\n". Required when zeroExit.
	exactStdout string
	// requiredStderrSubstring is Tier 3's required "[Runtime Error] ..."
	// text. Required when !zeroExit.
	requiredStderrSubstring string
}

// fixture is one catalog entry. Exactly one of snippetID or sources+
// entrypoint selects its program. hosts lists the operating systems (as
// runtime.GOOS spellings) its expectation can execute on; nil means every
// host. A fixture whose platform code cannot run here is simply not
// collected here -- never a skipped result.
type fixture struct {
	name        string
	snippetID   string
	sources     map[string]string
	entrypoint  string
	expectation *processExpectation
	hosts       []string
}

// appliesToHost reports whether f should be collected on the running host.
func (f fixture) appliesToHost() bool {
	if len(f.hosts) == 0 {
		return true
	}
	for _, host := range f.hosts {
		if host == runtime.GOOS {
			return true
		}
	}
	return false
}

// resolve returns f's compiled program, from its inline source or from the
// workbench snippet catalog.
func (f fixture) resolve(t *testing.T) compiler.CompilationResult {
	t.Helper()
	if f.snippetID != "" {
		categories, err := snippets.Load()
		if err != nil {
			t.Fatalf("snippets.Load() error = %v", err)
		}
		for _, category := range categories {
			for _, snippet := range category.Snippets {
				if snippet.ID == f.snippetID {
					return assertCompilesSources(t, snippet.Sources, snippet.Entrypoint)
				}
			}
		}
		t.Fatalf("fixture %q names unknown snippet ID %q", f.name, f.snippetID)
	}
	return assertCompilesSources(t, f.sources, f.entrypoint)
}

// assertCompilesSources is assertCompiles generalized to a full source map,
// for fixtures and snippets that span more than one module.
func assertCompilesSources(t *testing.T, sources map[string]string, entrypoint string) compiler.CompilationResult {
	t.Helper()
	result := compiler.Compile(sources, entrypoint, compiler.Project{})
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("expected success; got %d diagnostic(s):\n%v", len(result.Stderr), result.Stderr)
	}
	return result
}

// validateCatalog is the executable guard every fixture must pass before
// the suite trusts it: a fixture that declares an expectation nothing ever
// executes is worse than no fixture, since it claims coverage that does not
// exist.
func validateCatalog(t *testing.T, catalog []fixture) {
	t.Helper()
	seenNames := make(map[string]bool, len(catalog))
	knownSnippetIDs := loadSnippetIDs(t)
	for _, f := range catalog {
		if seenNames[f.name] {
			t.Fatalf("duplicate fixture name %q", f.name)
		}
		seenNames[f.name] = true

		hasSnippet := f.snippetID != ""
		hasInline := f.sources != nil
		if hasSnippet == hasInline {
			t.Fatalf("fixture %q must select exactly one source form (snippet ID xor inline sources); has snippet=%v inline=%v", f.name, hasSnippet, hasInline)
		}
		if hasSnippet && !knownSnippetIDs[f.snippetID] {
			t.Fatalf("fixture %q names unknown snippet ID %q", f.name, f.snippetID)
		}
		if hasInline && f.entrypoint == "" {
			t.Fatalf("fixture %q has inline sources but no entrypoint", f.name)
		}
		if f.expectation != nil {
			exp := f.expectation
			if exp.zeroExit && exp.requiredStderrSubstring != "" {
				t.Fatalf("fixture %q is a zero-exit expectation but also names a trap substring", f.name)
			}
			if !exp.zeroExit && exp.requiredStderrSubstring == "" {
				t.Fatalf("fixture %q is a non-zero expectation but names no required stderr substring", f.name)
			}
			if !exp.zeroExit && exp.exactStdout != "" {
				t.Fatalf("fixture %q is a non-zero (trap) expectation but also names an exact stdout; Tier 3 does not constrain stdout", f.name)
			}
		}
	}
	// Every fixture entering the catalog is selected by the single unified
	// runner in runner_test.go by construction (it iterates this catalog
	// directly with no parallel registration list), so there is no separate
	// "selected by no runner" case to detect here beyond the shape checks
	// above.
}

func loadSnippetIDs(t *testing.T) map[string]bool {
	t.Helper()
	categories, err := snippets.Load()
	if err != nil {
		t.Fatalf("snippets.Load() error = %v", err)
	}
	ids := make(map[string]bool)
	for _, category := range categories {
		for _, snippet := range category.Snippets {
			ids[snippet.ID] = true
		}
	}
	return ids
}

// TestCatalogIsWellFormed is the catalog's own guard test: every fixture in
// fixtureCatalog must have the exact shape validateCatalog checks. This is
// separate from actually building or running any fixture, so a malformed
// catalog entry fails fast and by name rather than surfacing as a confusing
// downstream compiler or runtime error.
func TestCatalogIsWellFormed(t *testing.T) {
	validateCatalog(t, fixtureCatalog)
}
