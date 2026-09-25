//go:build c23

package c23validation

// The qualification gate: the fixture catalog compiled, linked, and run
// through the one profile the compiler-owned registry qualifies for a native
// build on this host, never the host-neutral Project{} every other test in
// this package uses. Project{} keeps both
// platform branches and lets the C compiler's own target macros select one
// at C-compile time (see TestHostNeutralRetainsBothBranches in
// compiler/tests/integration); an explicit profile instead has the Hexal
// compiler itself omit the inactive branch (TestExplicitProfileOmitsPosixBranches,
// same package), a materially different code path through every runtime
// component with concurrency or IO code. This is the one qualified
// compile/link/run gate; non-host compiler-target cases stay pure-Go
// generated-C assertions in the integration suite, not linked here.

import (
	"testing"

	"hexal/compiler"
	"hexal/workbench/snippets"
)

// qualifiedProject is the compiler-owned registry's one entry for this host
// (compiler/profile.go), the only target this release's native driver
// qualifies on the running machine.
var qualifiedProject = compiler.Project{Target: hostTarget()}

// resolveQualified is fixture.resolve, but compiled against qualifiedProject
// instead of the host-neutral Project{} every other fixture use in this
// package selects.
func (f fixture) resolveQualified(t *testing.T) compiler.CompilationResult {
	t.Helper()
	if f.snippetID != "" {
		categories, err := snippets.Load()
		if err != nil {
			t.Fatalf("snippets.Load() error = %v", err)
		}
		for _, category := range categories {
			for _, snippet := range category.Snippets {
				if snippet.ID == f.snippetID {
					return assertCompilesProject(t, snippet.Sources, snippet.Entrypoint, qualifiedProject)
				}
			}
		}
		t.Fatalf("fixture %q names unknown snippet ID %q", f.name, f.snippetID)
	}
	return assertCompilesProject(t, f.sources, f.entrypoint, qualifiedProject)
}

// TestC23SuiteQualifiedProfile reruns every runnable fixture's own
// expectation (exact stdout, or the exact "[Runtime Error] ..." trap
// substring) against C compiled under the qualified profile. A fixture with
// no expectation is compile-only under the host-neutral suite too, so only
// compiling it here (never running it) matches that same tier boundary.
func TestC23SuiteQualifiedProfile(t *testing.T) {
	buildRoot := t.TempDir()
	for _, f := range fixtureCatalog {
		if !f.appliesToHost() {
			continue
		}
		t.Run(f.name, func(t *testing.T) {
			result := f.resolveQualified(t)
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
