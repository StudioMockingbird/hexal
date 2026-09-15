package driver

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"hexal/compiler"
)

// Pure-Go mode tests. Anything that spawns the real backend lives in
// build_c23_test.go behind the `c23` build tag.

func TestParseModeResolvesDefaultAndBothModes(t *testing.T) {
	for _, testCase := range []struct {
		value string
		want  BuildMode
	}{
		{"", ModeDebug},
		{"debug", ModeDebug},
		{"release", ModeRelease},
	} {
		got, err := ParseMode(testCase.value)
		if err != nil {
			t.Fatalf("ParseMode(%q) failed: %v", testCase.value, err)
		}
		if got != testCase.want {
			t.Fatalf("ParseMode(%q) = %q, want %q", testCase.value, got, testCase.want)
		}
	}
}

// The diagnostic text is part of the CLI contract, so it is asserted exactly
// rather than by substring.
func TestParseModeRejectsUnknownValueWithExactDiagnostic(t *testing.T) {
	for _, value := range []string{"fast", "Debug", "RELEASE", "small", "-O2"} {
		_, err := ParseMode(value)
		if err == nil {
			t.Fatalf("ParseMode(%q) accepted an unknown mode", value)
		}
		want := "unknown build mode " + value + "; expected debug or release"
		if err.Error() != want {
			t.Fatalf("ParseMode(%q) error = %q, want %q", value, err.Error(), want)
		}
	}
}

// A caller that sets Mode directly, bypassing flag parsing, must hit the same
// diagnostic and fail at configuration, before any compilation runs.
func TestBuildRejectsUnknownModeAtConfiguration(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "print(\"x\")\n")

	_, err := Build(BuildOptions{Root: dir, Mode: BuildMode("fast")})
	buildErr, ok := err.(*BuildError)
	if !ok {
		t.Fatalf("error %v is not a staged build error", err)
	}
	if buildErr.Stage != StageConfiguration {
		t.Fatalf("stage = %q, want configuration", buildErr.Stage)
	}
	want := "unknown build mode fast; expected debug or release"
	if buildErr.Message != want {
		t.Fatalf("message = %q, want %q", buildErr.Message, want)
	}
}

// The exact option set of each mode is the whole surface this feature adds,
// so it is pinned literally: a change to any entry must be a deliberate edit
// here, never a drive-by.
func TestModeOptionsAreExact(t *testing.T) {
	debug := Options(ModeDebug)
	wantDebugCompile := []string{"-O0", "-g", "-ffp-contract=off", "-fno-sanitize-recover=undefined"}
	if !reflect.DeepEqual(debug.Compile, wantDebugCompile) {
		t.Fatalf("debug compile options = %v, want %v", debug.Compile, wantDebugCompile)
	}
	if len(debug.Link) != 0 {
		t.Fatalf("debug link options = %v, want none", debug.Link)
	}

	release := Options(ModeRelease)
	wantReleaseCompile := []string{"-O2", "-g0", "-ffp-contract=off", "-fno-sanitize=undefined", "-ffunction-sections", "-fdata-sections"}
	if !reflect.DeepEqual(release.Compile, wantReleaseCompile) {
		t.Fatalf("release compile options = %v, want %v", release.Compile, wantReleaseCompile)
	}
	wantReleaseLink := []string{"-s", "-Wl,--gc-sections"}
	if !reflect.DeepEqual(release.Link, wantReleaseLink) {
		t.Fatalf("release link options = %v, want %v", release.Link, wantReleaseLink)
	}
}

// Callers append their include and dependency options to what Options
// returns, so a returned slice must never alias the table.
func TestModeOptionsAreFreshCopies(t *testing.T) {
	first := Options(ModeRelease)
	first.Compile[0] = "-O0"
	first.Link[0] = "-g"
	second := Options(ModeRelease)
	if second.Compile[0] != "-O2" || second.Link[0] != "-s" {
		t.Fatalf("option table was mutated through a returned slice: %+v", second)
	}
}

func identityInputs() (map[string]string, []compiler.RuntimeDependency, []string, []string) {
	files := map[string]string{
		"hexal/runtime.c": "int runtime(void){return 0;}\n",
		"modules/app.c":   "int app(void){return 1;}\n",
		"hexal/runtime.h": "int runtime(void);\n",
	}
	dependencies := []compiler.RuntimeDependency{compiler.RuntimeMimalloc}
	return files, dependencies, Options(ModeDebug).Compile, Options(ModeDebug).Link
}

var fullSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestBuildIdentityIsAStableFullDigest(t *testing.T) {
	files, dependencies, compileOptions, linkOptions := identityInputs()
	first := buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions)
	if !fullSHA256.MatchString(first) {
		t.Fatalf("identity %q is not a lowercase full SHA-256", first)
	}
	for range 5 {
		if again := buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions); again != first {
			t.Fatalf("identity is not deterministic: %q then %q", first, again)
		}
	}
}

// Every field of the stream must move the identity, or two builds with
// different outputs could claim one debug-information name.
func TestBuildIdentityRespondsToEveryField(t *testing.T) {
	files, dependencies, compileOptions, linkOptions := identityInputs()
	base := buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions)

	otherFiles := map[string]string{}
	for name, content := range files {
		otherFiles[name] = content
	}
	otherFiles["modules/app.c"] = "int app(void){return 2;}\n"

	renamedFiles := map[string]string{}
	for name, content := range files {
		renamedFiles[name] = content
	}
	delete(renamedFiles, "modules/app.c")
	renamedFiles["modules/other.c"] = files["modules/app.c"]

	for _, testCase := range []struct {
		name     string
		identity string
	}{
		{"mode", buildIdentity(ModeRelease, "zig=0.16.0", files, dependencies, compileOptions, linkOptions)},
		{"backend", buildIdentity(ModeDebug, "zig=0.15.1", files, dependencies, compileOptions, linkOptions)},
		{"artifact bytes", buildIdentity(ModeDebug, "zig=0.16.0", otherFiles, dependencies, compileOptions, linkOptions)},
		{"artifact names", buildIdentity(ModeDebug, "zig=0.16.0", renamedFiles, dependencies, compileOptions, linkOptions)},
		{"dependencies", buildIdentity(ModeDebug, "zig=0.16.0", files, []compiler.RuntimeDependency{compiler.RuntimeLibuv}, compileOptions, linkOptions)},
		{"compile options", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, []string{"-O0"}, linkOptions)},
		{"link options", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, []string{"-luser32"})},
	} {
		if testCase.identity == base {
			t.Fatalf("identity ignores %s", testCase.name)
		}
	}
}

// The identity names the staging directory, so it must be computable before
// that directory exists and must carry no host path: a host path would make
// the identity of one program differ per checkout and the byte-equality rule
// for a republished debug file could never be exercised.
func TestBuildIdentityCarriesNoHostPath(t *testing.T) {
	files, dependencies, compileOptions, linkOptions := identityInputs()
	identity := buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions)
	if !fullSHA256.MatchString(identity) {
		t.Fatalf("identity %q is not a bare digest", identity)
	}
	for _, option := range append(append([]string(nil), compileOptions...), linkOptions...) {
		if strings.ContainsAny(option, `\/:`) {
			t.Fatalf("a hashed option %q looks like a path; the identity must name none", option)
		}
	}
	// A staging directory named from it must be a plain sibling name.
	staging, err := identityStagingDir(t.TempDir(), identity)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(staging) != "build-"+identity {
		t.Fatalf("staging directory %q is not named after the identity", staging)
	}
}

// A rebuild must reuse the same staging path and start from an empty tree,
// so that no file of an interrupted earlier build is mistaken for input.
func TestIdentityStagingDirIsStableAndCleared(t *testing.T) {
	outDir := t.TempDir()
	first, err := identityStagingDir(outDir, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(first, "leftover.o"), "stale")
	second, err := identityStagingDir(outDir, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if second != first {
		t.Fatalf("staging path moved between builds: %q then %q", first, second)
	}
	if _, err := os.Stat(filepath.Join(second, "leftover.o")); !os.IsNotExist(err) {
		t.Fatalf("an earlier build's file survived into the new tree: %v", err)
	}
}

// The stream is length-delimited so that field boundaries cannot be forged by
// running one field's content into the next.
func TestBuildIdentitySeparatesAdjacentFields(t *testing.T) {
	empty := map[string]string{}
	first := buildIdentity(ModeDebug, "zig", empty, nil, []string{"ab", "c"}, nil)
	second := buildIdentity(ModeDebug, "zig", empty, nil, []string{"a", "bc"}, nil)
	if first == second {
		t.Fatal("adjacent option fields are not length-delimited")
	}
	third := buildIdentity(ModeDebug, "zig", map[string]string{"a": "b"}, nil, nil, nil)
	fourth := buildIdentity(ModeDebug, "zig", map[string]string{"ab": ""}, nil, nil, nil)
	if third == fourth {
		t.Fatal("artifact name and content are not length-delimited")
	}
}

func TestVersionedBasenameCarriesTheIdentity(t *testing.T) {
	got := versionedBasename(filepath.Join("C:\\", "out", "main"+exeSuffix()), "abc123")
	if got != "main.abc123" {
		t.Fatalf("versioned basename = %q, want main.abc123", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPublishVersionedPDBPublishesAndRepublishesIdentically(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged.pdb")
	published := filepath.Join(dir, "main.abc123.pdb")
	writeFile(t, staged, "debug-information")

	if err := publishVersionedPDB(staged, published); err != nil {
		t.Fatalf("first publication failed: %v", err)
	}
	raw, err := os.ReadFile(published)
	if err != nil {
		t.Fatalf("published file unreadable: %v", err)
	}
	if string(raw) != "debug-information" {
		t.Fatalf("published %q", raw)
	}

	// A rebuild of the same program reaches the same identity and must accept
	// the already-published file rather than rewriting it.
	writeFile(t, staged, "debug-information")
	if err := publishVersionedPDB(staged, published); err != nil {
		t.Fatalf("republishing identical debug information failed: %v", err)
	}
}

// Two different builds claiming one identity is a defect in the identity, and
// must fail before the executable is published rather than leave an
// executable pointing at debug information that does not describe it.
func TestPublishVersionedPDBRejectsDisagreement(t *testing.T) {
	dir := t.TempDir()
	staged := filepath.Join(dir, "staged.pdb")
	published := filepath.Join(dir, "main.abc123.pdb")
	writeFile(t, published, "one build")
	writeFile(t, staged, "a different build")

	err := publishVersionedPDB(staged, published)
	if err == nil {
		t.Fatal("disagreeing debug information was accepted")
	}
	if !strings.Contains(err.Error(), "identical debug information") {
		t.Fatalf("unexpected error: %v", err)
	}
	raw, readErr := os.ReadFile(published)
	if readErr != nil || string(raw) != "one build" {
		t.Fatalf("published debug information was modified: %q %v", raw, readErr)
	}
}

// The recorded expansions are the evidence behind the option table: the
// settings each mode promises must be what the pinned backend actually
// selects, and the settings it promises to omit must be absent.
func TestRecordedModeExpansionsMatchTheOptionTable(t *testing.T) {
	for _, testCase := range []struct {
		mode    BuildMode
		file    string
		present []string
		absent  []string
	}{
		{
			mode: ModeDebug,
			file: "testdata/zig-cc-expansion-debug.txt",
			present: []string{
				`"-O0"`,
				`"-ffp-contract=off"`,
				`"-gcodeview"`,
				`"-debug-info-kind=`,
				`"-fsanitize=alignment,array-bounds,bool,builtin,enum,float-cast-overflow,integer-divide-by-zero,nonnull-attribute,null,pointer-overflow,return,returns-nonnull-attribute,shift-base,shift-exponent,signed-integer-overflow,unreachable,vla-bound"`,
				`"-target-cpu" "x86-64"`,
			},
			// A recoverable report would let a program continue past
			// undefined behavior the generated C is required never to have.
			absent: []string{`"-fsanitize-recover=`, `"-ffunction-sections"`, `"-fdata-sections"`},
		},
		{
			mode: ModeRelease,
			file: "testdata/zig-cc-expansion-release.txt",
			present: []string{
				`"-O2"`,
				`"-ffp-contract=off"`,
				`"-ffunction-sections"`,
				`"-fdata-sections"`,
				`"-target-cpu" "x86-64"`,
			},
			// No debug information is emitted at all, and the backstop the
			// contract says can never fire is not paid for.
			absent: []string{`"-debug-info-kind=`, `"-fsanitize=`, `"-fsanitize-recover=`},
		},
	} {
		t.Run(string(testCase.mode), func(t *testing.T) {
			raw, err := os.ReadFile(testCase.file)
			if err != nil {
				t.Fatal(err)
			}
			// The recorded expansion wraps long command lines; joining it into
			// one line lets an argument pair be matched as it was written.
			expansion := strings.Join(strings.Fields(string(raw)), " ")
			// Only settings are asserted, never the options themselves: the
			// compiler driver consumes an option such as -g0 or
			// -fno-sanitize-recover=undefined and expresses it by what the
			// expansion below does and does not select.
			for _, want := range testCase.present {
				if !strings.Contains(expansion, want) {
					t.Errorf("recorded expansion lacks %s", want)
				}
			}
			for _, unwanted := range testCase.absent {
				if strings.Contains(expansion, unwanted) {
					t.Errorf("recorded expansion carries %s", unwanted)
				}
			}
		})
	}
}
