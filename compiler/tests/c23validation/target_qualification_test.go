//go:build c23

package c23validation

// Track 6's qualification gate: the fixture catalog compiled, linked, and
// run through the one profile the compiler-owned registry currently
// qualifies (compilerTypes.TargetX86_64WindowsGNU), never the host-neutral
// Project{} every other test in this package uses. Project{} keeps both
// platform branches and lets the C compiler's own target macros select one
// at C-compile time (see TestHostNeutralRetainsBothBranches in
// compiler/tests/integration); an explicit profile instead has the Hexal
// compiler itself omit the inactive branch (TestExplicitProfileOmitsPosixBranches,
// same package), a materially different code path through every runtime
// component with concurrency or IO code. Without this test, that path's
// only real-toolchain execution coverage was internal/driver's own
// hand-written qualification gate (TestBuildProducesRunnableExecutable and
// its neighbors) -- real, but far narrower than the full fixture and
// snippet catalog this package otherwise runs against Project{}.

import (
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
	"hexal/workbench/snippets"
)

// qualifiedProject is Track 6's one target: the compiler-owned registry's
// only current entry (compiler/profile.go's targetProfiles).
var qualifiedProject = compiler.Project{Target: compilerTypes.TargetX86_64WindowsGNU}

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
					return assertCompilesSourcesWithProject(t, snippet.Sources, snippet.Entrypoint, qualifiedProject)
				}
			}
		}
		t.Fatalf("fixture %q names unknown snippet ID %q", f.name, f.snippetID)
	}
	return assertCompilesSourcesWithProject(t, f.sources, f.entrypoint, qualifiedProject)
}

// assertCompilesSourcesWithProject is assertCompilesSources generalized to
// an explicit Project, so the qualified-profile gate can reuse every other
// fixture-resolution rule (snippet lookup, inline sources) without
// duplicating it.
func assertCompilesSourcesWithProject(t *testing.T, sources map[string]string, entrypoint string, project compiler.Project) compiler.CompilationResult {
	t.Helper()
	result := compiler.Compile(sources, entrypoint, project)
	if result.ExitCode != compiler.ExitSuccess {
		t.Fatalf("expected success under the qualified profile; got %d diagnostic(s):\n%v", len(result.Stderr), result.Stderr)
	}
	return result
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
