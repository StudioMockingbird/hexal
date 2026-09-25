package driver

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

// Pure-Go mode tests. Anything that spawns the real backend lives in
// build_c23_test.go behind the `c23` build tag.

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

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
// here, never a drive-by. Linux debug carries the leak flag and the
// non-recovering diagnostic policy; Windows debug replaces the recover flag
// with trap mode so its link never needs a MinGW UBSan runtime; release is
// byte-identical across lanes.
func TestModeOptionsAreExact(t *testing.T) {
	debug := Options(ModeDebug, compilerTypes.TargetX86_64LinuxGNU)
	wantDebugCompile := []string{"-O0", "-g", "-ffp-contract=off", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-fsanitize=leak"}
	if !reflect.DeepEqual(debug.Compile, wantDebugCompile) {
		t.Fatalf("linux debug compile options = %v, want %v", debug.Compile, wantDebugCompile)
	}
	wantDebugLink := []string{"-fsanitize=undefined", "-fno-sanitize-recover=all", "-fsanitize=leak"}
	if !reflect.DeepEqual(debug.Link, wantDebugLink) {
		t.Fatalf("linux debug link options = %v, want %v", debug.Link, wantDebugLink)
	}

	windowsDebug := Options(ModeDebug, compilerTypes.TargetX86_64WindowsGNU)
	wantWindowsDebugCompile := []string{"-O0", "-g", "-ffp-contract=off", "-fsanitize=undefined", "-fsanitize-trap=undefined"}
	if !reflect.DeepEqual(windowsDebug.Compile, wantWindowsDebugCompile) {
		t.Fatalf("windows debug compile options = %v, want %v", windowsDebug.Compile, wantWindowsDebugCompile)
	}
	wantWindowsDebugLink := []string{"-fsanitize=undefined", "-fsanitize-trap=undefined"}
	if !reflect.DeepEqual(windowsDebug.Link, wantWindowsDebugLink) {
		t.Fatalf("windows debug link options = %v, want %v", windowsDebug.Link, wantWindowsDebugLink)
	}

	releaseLinux := Options(ModeRelease, compilerTypes.TargetX86_64LinuxGNU)
	releaseWindows := Options(ModeRelease, compilerTypes.TargetX86_64WindowsGNU)
	if !reflect.DeepEqual(releaseLinux.Compile, releaseWindows.Compile) || !reflect.DeepEqual(releaseLinux.Link, releaseWindows.Link) {
		t.Fatalf("release option sets differ across lanes: linux=%+v windows=%+v", releaseLinux, releaseWindows)
	}
	wantReleaseCompile := []string{"-O2", "-g0", "-ffp-contract=off", "-fno-sanitize=undefined", "-ffunction-sections", "-fdata-sections"}
	if !reflect.DeepEqual(releaseLinux.Compile, wantReleaseCompile) {
		t.Fatalf("release compile options = %v, want %v", releaseLinux.Compile, wantReleaseCompile)
	}
	wantReleaseLink := []string{"-s", "-Wl,--gc-sections"}
	if !reflect.DeepEqual(releaseLinux.Link, wantReleaseLink) {
		t.Fatalf("release link options = %v, want %v", releaseLinux.Link, wantReleaseLink)
	}
}

// Windows debug must never carry -fsanitize=address; the pack is built
// without an ASan runtime and a MinGW link would fail.
func TestWindowsDebugNeverCarriesAddressSanitizer(t *testing.T) {
	for _, mode := range []BuildMode{ModeDebug, ModeRelease} {
		options := Options(mode, compilerTypes.TargetX86_64WindowsGNU)
		for _, option := range append(append([]string(nil), options.Compile...), options.Link...) {
			if strings.Contains(option, "-fsanitize=address") {
				t.Fatalf("%s windows options carry address sanitizer: %s", mode, option)
			}
		}
	}
}

// An empty target is host-neutral and must not arm a leak flag: only a
// qualified Linux profile selects LSan.
func TestModeOptionsEmptyTargetCarriesNoLeakFlag(t *testing.T) {
	options := Options(ModeDebug, "")
	for _, list := range [][]string{options.Compile, options.Link} {
		for _, option := range list {
			if option == "-fsanitize=leak" {
				t.Fatalf("empty target carries the leak flag: %v", list)
			}
		}
	}
}

// Callers append their include and dependency options to what Options
// returns, so a returned slice must never alias the table.
func TestModeOptionsAreFreshCopies(t *testing.T) {
	first := Options(ModeRelease, compilerTypes.TargetX86_64LinuxGNU)
	first.Compile[0] = "-O0"
	first.Link[0] = "-g"
	second := Options(ModeRelease, compilerTypes.TargetX86_64LinuxGNU)
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
	return files, dependencies, Options(ModeDebug, compilerTypes.TargetX86_64LinuxGNU).Compile, Options(ModeDebug, compilerTypes.TargetX86_64LinuxGNU).Link
}

var fullSHA256 = regexp.MustCompile(`^[0-9a-f]{64}$`)

func TestBuildIdentityIsAStableFullDigest(t *testing.T) {
	files, dependencies, compileOptions, linkOptions := identityInputs()
	first := buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)
	if !fullSHA256.MatchString(first) {
		t.Fatalf("identity %q is not a lowercase full SHA-256", first)
	}
	for range 5 {
		if again := buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil); again != first {
			t.Fatalf("identity is not deterministic: %q then %q", first, again)
		}
	}
}

// Every field of the stream must move the identity, or two builds with
// different outputs could claim one debug-information name.
func TestBuildIdentityRespondsToEveryField(t *testing.T) {
	files, dependencies, compileOptions, linkOptions := identityInputs()
	base := buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)

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
		{"mode", buildIdentity(ModeRelease, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)},
		{"backend", buildIdentity(ModeDebug, "zig=0.15.1", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)},
		{"artifact bytes", buildIdentity(ModeDebug, "zig=0.16.0", otherFiles, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)},
		{"artifact names", buildIdentity(ModeDebug, "zig=0.16.0", renamedFiles, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)},
		{"dependencies", buildIdentity(ModeDebug, "zig=0.16.0", files, []compiler.RuntimeDependency{compiler.RuntimeLibuv}, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)},
		{"compile options", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, []string{"-O0"}, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)},
		{"link options", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, []string{"-luser32"}, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)},
		{"target profile", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, "x86_64-linux-gnu", packInputs{}, nil)},
		{"pack manifest presence", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{ManifestDigest: "manifest-a", PayloadHashes: []string{"a"}}, nil)},
		{"manifest digest", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{ManifestDigest: "manifest-b", PayloadHashes: []string{"a"}}, nil)},
		{"selected payload hash", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{ManifestDigest: "manifest-a", PayloadHashes: []string{"b"}}, nil)},
		{"environment override hash", buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, []string{"override-hash"})},
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
	identity := buildIdentity(ModeDebug, "zig=0.16.0", files, dependencies, compileOptions, linkOptions, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)
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
	first := buildIdentity(ModeDebug, "zig", empty, nil, []string{"ab", "c"}, nil, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)
	second := buildIdentity(ModeDebug, "zig", empty, nil, []string{"a", "bc"}, nil, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)
	if first == second {
		t.Fatal("adjacent option fields are not length-delimited")
	}
	third := buildIdentity(ModeDebug, "zig", map[string]string{"a": "b"}, nil, nil, nil, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)
	fourth := buildIdentity(ModeDebug, "zig", map[string]string{"ab": ""}, nil, nil, nil, compilerTypes.TargetX86_64WindowsGNU, packInputs{}, nil)
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

// The mode option table is the single source of a mode's backend flags. Debug
// enables the UBSan backstop and its non-recovering policy at both compile and
// link; release drops the backstop and garbage-collects unused sections.
func TestModeOptionTableMatchesTheContract(t *testing.T) {
	debug := Options(ModeDebug, compilerTypes.TargetX86_64LinuxGNU)
	debugJoined := strings.Join(append(append([]string{}, debug.Compile...), debug.Link...), " ")
	for _, want := range []string{"-O0", "-g", "-ffp-contract=off", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-fsanitize=leak"} {
		if !strings.Contains(debugJoined, want) {
			t.Errorf("debug options lack %s: %v", want, debugJoined)
		}
	}
	if strings.Contains(debugJoined, "-fno-sanitize=undefined") {
		t.Errorf("debug options disable the sanitizer: %v", debugJoined)
	}

	release := Options(ModeRelease, compilerTypes.TargetX86_64LinuxGNU)
	releaseJoined := strings.Join(append(append([]string{}, release.Compile...), release.Link...), " ")
	for _, absent := range []string{"-fsanitize=undefined", "-fno-sanitize-recover"} {
		if strings.Contains(releaseJoined, absent) {
			t.Errorf("release options carry %s: %v", absent, releaseJoined)
		}
	}
	for _, want := range []string{"-O2", "-g0", "-ffp-contract=off", "-ffunction-sections", "-fdata-sections", "-Wl,--gc-sections"} {
		if !strings.Contains(releaseJoined, want) {
			t.Errorf("release options lack %s: %v", want, releaseJoined)
		}
	}
}

// Foreign C compilation never inherits the generated C sanitizer backstop or
// the leak instrument: the filter drops every option containing "sanitize",
// and the leak flag never reaches this path because it is appended only in
// Options for a Linux profile.
func TestForeignCompileOptionsDropSanitizers(t *testing.T) {
	for _, mode := range []BuildMode{ModeDebug, ModeRelease} {
		for _, option := range ForeignCompileOptions(mode) {
			if strings.Contains(option, "sanitize") {
				t.Fatalf("%s foreign options carry sanitizer option %q", mode, option)
			}
			if option == "-fsanitize=leak" {
				t.Fatalf("%s foreign options carry the leak flag", mode)
			}
		}
	}
}
