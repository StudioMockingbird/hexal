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
	"fmt"
	"regexp"
	"runtime"
	"slices"
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
	// exitStatus, when non-zero, is the exact status Tier 3 requires instead
	// of any non-zero status.
	exitStatus int
	// exactOutput, when non-empty, is Tier 3's complete stdout and stderr
	// merged onto one stream in write order, after "\r\n" normalization.
	exactOutput string
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
	// project carries build-time settings the fixture requires. The zero
	// value is the default program; a foreign fixture sets a qualified target
	// because every foreign ABI fact is target-dependent.
	project compiler.Project
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

// smokeFixtures is the fixture set a -short run keeps, one per contract the
// short gate must still observe: compile-only acceptance, execution, an exact
// trap, and one program per native runtime dependency (libuv, utf8proc,
// yyjson, PCRE2, and a foreign C call; mimalloc backs every allocation). The
// list is selection policy only and never changes what a fixture asserts.
var smokeFixtures = []string{
	"list-compiles",
	"list-runs",
	"division-by-zero-traps",
	"concurrency-spawn-channel-runs",
	"normalize-runs",
	"json-reader-writer-conformance-runs",
	"regex-unicode-byte-spans-and-captures-runs",
	"foreign-call-runs",
}

// inScope reports whether f runs in this invocation: every fixture normally,
// only the smoke set under -short. It composes with appliesToHost; a fixture
// must satisfy both.
func (f fixture) inScope() bool {
	return !testing.Short() || slices.Contains(smokeFixtures, f.name)
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
	return assertCompilesProject(t, f.sources, f.entrypoint, f.project)
}

// assertCompilesSources is assertCompiles generalized to a full source map,
// for fixtures and snippets that span more than one module. It uses the
// default project settings.
func assertCompilesSources(t *testing.T, sources map[string]string, entrypoint string) compiler.CompilationResult {
	t.Helper()
	return assertCompilesProject(t, sources, entrypoint, compiler.Project{})
}

// assertCompilesProject compiles sources with explicit project settings. A
// snippet lookup uses the defaults; a foreign fixture supplies a qualified
// target.
func assertCompilesProject(t *testing.T, sources map[string]string, entrypoint string, project compiler.Project) compiler.CompilationResult {
	t.Helper()
	result := compiler.Compile(sources, entrypoint, project)
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
			if exp.zeroExit && (exp.exitStatus != 0 || exp.exactOutput != "") {
				t.Fatalf("fixture %q is a zero-exit expectation but also names a Tier 3 exit status or merged output", f.name)
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

	// The smoke set must name real, host-applicable fixtures exactly once and
	// together demand every native runtime dependency; otherwise the short
	// gate could pass while a dependency's pack entry or link input is broken.
	t.Run("smoke set", func(t *testing.T) {
		demanded := make(map[compiler.RuntimeDependency]bool)
		listed := make(map[string]bool, len(smokeFixtures))
		for _, name := range smokeFixtures {
			if listed[name] {
				t.Fatalf("smoke fixture %q is listed more than once", name)
			}
			listed[name] = true
			var match []fixture
			for _, f := range fixtureCatalog {
				if f.name == name {
					match = append(match, f)
				}
			}
			if len(match) != 1 {
				t.Fatalf("smoke fixture %q appears %d times in fixtureCatalog, want exactly 1", name, len(match))
			}
			if !match[0].appliesToHost() {
				t.Fatalf("smoke fixture %q does not apply to host %q", name, runtime.GOOS)
			}
			for _, dependency := range match[0].resolve(t).Dependencies {
				demanded[dependency] = true
			}
		}
		for _, dependency := range []compiler.RuntimeDependency{
			compiler.RuntimeMimalloc, compiler.RuntimeLibuv, compiler.RuntimeUtf8proc,
			compiler.RuntimeYyjson, compiler.RuntimePcre2,
		} {
			if !demanded[dependency] {
				t.Errorf("smoke set demands no %s dependency", dependency)
			}
		}
	})

	// Fixture subtests run in parallel, so two fixtures binding one fixed
	// loopback port would race for it.
	t.Run("unique ports", func(t *testing.T) {
		snippetSources := make(map[string]map[string]string)
		for _, snippet := range allSnippets(t) {
			snippetSources[snippet.ID] = snippet.Sources
		}
		if problems := duplicateLoopbackPorts(fixtureCatalog, snippetSources); len(problems) > 0 {
			t.Fatalf("fixtures share a loopback port: %v", problems)
		}
		synthetic := []fixture{
			{name: "first", sources: map[string]string{"app.hex": `Net.parse_address("127.0.0.1", 19001)`}},
			{name: "second", sources: map[string]string{"app.hex": `Net.parse_address("127.0.0.1", 19001)`}},
		}
		if problems := duplicateLoopbackPorts(synthetic, nil); len(problems) != 1 {
			t.Fatalf("duplicateLoopbackPorts on a catalog sharing one port = %v, want exactly one problem", problems)
		}
	})
}

// loopbackPortLiteral matches the address form a fixture uses to bind a fixed
// loopback TCP port, capturing the port number.
var loopbackPortLiteral = regexp.MustCompile(`parse_address\(\s*"127\.0\.0\.1"\s*,\s*([0-9]+)\s*\)`)

// duplicateLoopbackPorts returns one message per loopback port literal that
// more than one fixture in catalog contains. A fixture's programs are its
// inline sources or, for a snippet-backed fixture, the snippet's sources from
// snippetSources. One fixture repeating its own port is not a conflict.
func duplicateLoopbackPorts(catalog []fixture, snippetSources map[string]map[string]string) []string {
	owners := make(map[string][]string)
	for _, f := range catalog {
		sources := f.sources
		if f.snippetID != "" {
			sources = snippetSources[f.snippetID]
		}
		ports := make(map[string]bool)
		for _, text := range sources {
			for _, match := range loopbackPortLiteral.FindAllStringSubmatch(text, -1) {
				ports[match[1]] = true
			}
		}
		for port := range ports {
			owners[port] = append(owners[port], f.name)
		}
	}
	var problems []string
	for port, names := range owners {
		if len(names) > 1 {
			slices.Sort(names)
			problems = append(problems, fmt.Sprintf("port %s is bound by fixtures %v", port, names))
		}
	}
	slices.Sort(problems)
	return problems
}
