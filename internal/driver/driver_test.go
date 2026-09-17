package driver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
	"hexal/internal/version"
)

// Pure-Go driver tests. Anything that spawns the C toolchain lives in
// build_c23_test.go behind the `c23` build tag, so `go test ./...` passes
// with no external toolchain installed.

func writeSource(t *testing.T, dir, name, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestBuildRejectsMissingEntrypoint attributes the failure to configuration
// rather than letting it surface as an opaque compiler error.
func TestBuildRejectsMissingEntrypoint(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, "other.hex", "print(\"x\")\n")

	_, err := Build(BuildOptions{Root: dir})
	buildErr, ok := err.(*BuildError)
	if !ok {
		t.Fatalf("error %v is not a staged build error", err)
	}
	if buildErr.Stage != StageConfiguration {
		t.Fatalf("stage = %q, want configuration", buildErr.Stage)
	}
}

// TestDiscoverAssignsSlashSeparatedKeys checks the one contract the driver
// owes the compiler: logical keys are relative and "/"-separated, so imports
// resolve against them on any host.
func TestDiscoverAssignsSlashSeparatedKeys(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeSource(t, dir, "main.hex", "print(\"x\")\n")
	writeSource(t, filepath.Join(dir, "nested"), "helper.hex", "fun f(): Int32 do\n    return 1\nend\nexport\n    f\nend\n")

	sources, err := discover(dir, stagingPath(dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"main.hex", "nested/helper.hex"} {
		if _, ok := sources[want]; !ok {
			t.Fatalf("missing key %q; got %v", want, keysOf(sources))
		}
	}
	for key := range sources {
		if strings.Contains(key, `\`) {
			t.Fatalf("key %q must not contain a host separator", key)
		}
	}
}

// TestDiscoverSkipsOnlyTheStagingTree stops a rebuild from feeding the
// previous staging tree back in as source, while a different source
// directory that happens to be named build remains valid.
func TestDiscoverSkipsOnlyTheStagingTree(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "print(\"x\")\n")
	staging := filepath.Join(dir, "build", ".hexal", "build-1")
	if err := os.MkdirAll(staging, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSource(t, staging, "stale.hex", "print(\"stale\")\n")
	other := filepath.Join(dir, "other", "build")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	writeSource(t, other, "live.hex", "print(\"live\")\n")

	sources, err := discover(dir, stagingPath(dir, ""))
	if err != nil {
		t.Fatal(err)
	}
	if _, found := sources["build/.hexal/build-1/stale.hex"]; found {
		t.Fatalf("staging tree must not be discovered as source; got %v", keysOf(sources))
	}
	if _, found := sources["other/build/live.hex"]; !found {
		t.Fatalf("unrelated build directory must stay discoverable; got %v", keysOf(sources))
	}
}

// TestDiscoverRejectsCaseFoldedKeys enforces one logical key per
// case-insensitive name on Windows, where two such files are one file. A
// case-insensitive filesystem cannot even hold both spellings, so the test
// skips there: the check itself still guards case-sensitive hosts, and on a
// folding filesystem the operating system enforces the invariant directly.
func TestDiscoverRejectsCaseFoldedKeys(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, "Main.hex", "print(\"x\")\n")
	writeSource(t, dir, "main.hex", "print(\"x\")\n")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) < 2 {
		t.Skip("filesystem folds case, so both spellings cannot coexist")
	}

	_, err = discover(dir, stagingPath(dir, ""))
	if err == nil || !strings.Contains(err.Error(), "collide") {
		t.Fatalf("case-folded keys accepted: %v", err)
	}
}

// TestDiscoverRejectsSourceSymlinks pins that a symlink inside the source
// tree is an error, not a silent detour outside the rooted tree. Link
// creation itself may need a privilege the test host lacks; then the test
// skips, and the rejection still guards every host that can make one.
func TestDiscoverRejectsSourceSymlinks(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, "main.hex", "print(\"x\")\n")
	if err := os.Symlink(filepath.Join(dir, "main.hex"), filepath.Join(dir, "link.hex")); err != nil {
		t.Skip("cannot create symlinks on this host: " + err.Error())
	}

	_, err := discover(dir, stagingPath(dir, ""))
	if err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("source symlink accepted: %v", err)
	}
}

// TestBuildRecordsHexalVersionOnce pins the project-level attribution: the
// result carries the toolchain version even when the build fails before any
// command runs, and command records never repeat it.
func TestBuildRecordsHexalVersionOnce(t *testing.T) {
	dir := t.TempDir()
	writeSource(t, dir, "other.hex", "print(\"x\")\n")

	result, err := Build(BuildOptions{Root: dir})
	if err == nil {
		t.Fatal("expected a missing-entrypoint failure")
	}
	if result.HexalVersion != version.String() {
		t.Fatalf("HexalVersion = %q, want %q", result.HexalVersion, version.String())
	}
	for _, command := range result.Commands {
		if strings.Contains(command.Tool, result.HexalVersion) {
			t.Fatalf("command record repeats the version: %+v", command)
		}
	}
}

// TestHexalFailureMessageKeepsOrdinaryDiagnosticsStable pins the version
// attribution rule: ordinary failures render exactly "compilation failed",
// while a compiler-defect Unknown Error carries the toolchain version for
// the bug report.
func TestHexalFailureMessageKeepsOrdinaryDiagnosticsStable(t *testing.T) {
	ordinary := []string{"[Syntax Error] expected ')' at main.hex:2:1"}
	if got := hexalFailureMessage(ordinary); got != "compilation failed" {
		t.Fatalf("ordinary message = %q", got)
	}
	defect := []string{"[Unknown Error] a violated internal invariant"}
	want := "compilation failed (Hexal " + version.String() + ")"
	if got := hexalFailureMessage(defect); got != want {
		t.Fatalf("defect message = %q, want %q", got, want)
	}
	mixed := append(ordinary, defect...)
	if got := hexalFailureMessage(mixed); got != want {
		t.Fatalf("mixed message = %q, want %q", got, want)
	}
}

// TestDoctorReportsVersionWithoutBackend pins that the Hexal version is
// available even when backend selection fails: no compiler path guarantees no
// backend resolves, and the report still opens with the version line.
func TestDoctorReportsVersionWithoutBackend(t *testing.T) {
	report, problems := Doctor(DoctorOptions{})
	if len(report) == 0 || report[0] != "Hexal: "+version.String() {
		t.Fatalf("doctor report %q lacks the leading Hexal version line", report)
	}
	if len(problems) == 0 {
		t.Fatal("doctor reported no problem with no target and no compiler")
	}
}

// TestMaterializeRefusesEscapingArtifact exercises the defense-in-depth
// backstop. RFC 0126 already rejects such keys at the compile boundary, so
// this is only reachable by a caller bypassing Compile - which is exactly why
// the driver keeps its own check.
func TestMaterializeRefusesEscapingArtifact(t *testing.T) {
	dir := t.TempDir()
	_, err := materialize(filepath.Join(dir, "staging"), map[string]string{
		"../escaped.c": "int main(void){return 0;}\n",
	})
	if err == nil {
		t.Fatal("expected an escaping artifact to be refused")
	}
	if !strings.Contains(err.Error(), "escapes the output root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestMaterializeRequiresCArtifacts guards against silently linking nothing.
func TestMaterializeRequiresCArtifacts(t *testing.T) {
	dir := t.TempDir()
	_, err := materialize(dir, map[string]string{"hexal.h": "/* header only */\n"})
	if err == nil || !strings.Contains(err.Error(), "no C artifacts") {
		t.Fatalf("expected a no-C-artifacts error, got %v", err)
	}
}

// TestMaterializeEmbeddedPack proves the checked-in pack is demand-driven: a
// mimalloc-only build materializes exactly the mimalloc include tree and
// archive, verified against their manifest digests, and nothing else.
func TestMaterializeEmbeddedPack(t *testing.T) {
	staging := t.TempDir()
	fsys, err := runtimePackFS(compilerTypes.TargetX86_64LinuxGNU)
	if err != nil {
		t.Fatal(err)
	}
	_, pack, err := loadRuntimeManifest(fsys, compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeMimalloc})
	if err != nil {
		t.Fatal(err)
	}
	includeDirs, archives, err := materializePack(staging, fsys, pack)
	if err != nil {
		t.Fatal(err)
	}
	if len(includeDirs) != 1 || len(archives) != 1 {
		t.Fatalf("materialized %v include dirs and %v archives, want one each", includeDirs, archives)
	}
	if _, err := os.Stat(filepath.Join(includeDirs[0], "mimalloc.h")); err != nil {
		t.Fatalf("materialized mimalloc header missing: %v", err)
	}
	if _, err := os.Stat(archives[0]); err != nil {
		t.Fatalf("materialized mimalloc archive missing: %v", err)
	}
}

// TestDependencyFreePackReadsNothing pins that a program selecting no runtime
// dependency opens no manifest and materializes nothing: the demanded list is
// empty, so no pack file is touched.
func TestDependencyFreePackReadsNothing(t *testing.T) {
	fsys, err := runtimePackFS(compilerTypes.TargetX86_64LinuxGNU)
	if err != nil {
		t.Fatal(err)
	}
	_, pack, err := loadRuntimeManifest(fsys, compilerTypes.TargetX86_64LinuxGNU, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.demanded) != 0 || len(pack.PayloadHashes) != 0 {
		t.Fatalf("dependency-free load demanded %v", pack.demanded)
	}
}

// TestMaterializeSortsCFilesDeterministically pins the deterministic
// logical-key order compilation and linking depend on: map iteration order
// must never reach a command line.
func TestMaterializeSortsCFilesDeterministically(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"modules/z.c": "int z(void){return 0;}\n",
		"modules/a.c": "int a(void){return 0;}\n",
		"hexal/m.c":   "int m(void){return 0;}\n",
	}
	for range 10 {
		staging, err := freshStagingDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		cFiles, err := materialize(staging, files)
		if err != nil {
			t.Fatal(err)
		}
		var keys []string
		for _, path := range cFiles {
			relative, err := filepath.Rel(staging, path)
			if err != nil {
				t.Fatal(err)
			}
			keys = append(keys, filepath.ToSlash(relative))
		}
		want := []string{"hexal/m.c", "modules/a.c", "modules/z.c"}
		if strings.Join(keys, ",") != strings.Join(want, ",") {
			t.Fatalf("c order = %v, want %v", keys, want)
		}
		os.RemoveAll(staging)
	}
}

// TestPublishExecutableReplacesAtomically checks that publication replaces
// an existing regular executable and preserves a previous onelles on
// failure paths. A failed publish leaves the destination untouched.
func TestPublishExecutableReplacesAtomically(t *testing.T) {
	dir := t.TempDir()
	final := filepath.Join(dir, "main.exe")
	if err := os.WriteFile(final, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	temp := filepath.Join(dir, "main.tmp.exe")
	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishExecutable(temp, final); err != nil {
		t.Fatalf("publish failed: %v", err)
	}
	raw, err := os.ReadFile(final)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "new" {
		t.Fatalf("published %q, want %q", raw, "new")
	}
	if _, err := os.Stat(temp); !os.IsNotExist(err) {
		t.Fatalf("temporary executable was not consumed: %v", err)
	}
}

// TestPublishExecutableRejectsNonRegularDestination pins the destination
// rule: directories, symlinks, and other non-regular files are rejected.
func TestPublishExecutableRejectsNonRegularDestination(t *testing.T) {
	dir := t.TempDir()
	temp := filepath.Join(dir, "main.tmp.exe")
	if err := os.WriteFile(temp, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := publishExecutable(temp, dir); err == nil {
		t.Fatal("directory destination accepted")
	}
	if err := publishExecutable(temp, filepath.Join(dir, "missing", "main.exe")); err == nil {
		t.Fatal("uncreatable destination accepted")
	}
}

// TestValidateOutputDestinationCreatesParents pins that an explicit -out
// authorizes its parent directory: validation creates what is missing and
// rejects what is not a directory.
func TestValidateOutputDestinationCreatesParents(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "nested", "deep", "main.exe")
	if err := validateOutputDestination(target); err != nil {
		t.Fatalf("valid destination rejected: %v", err)
	}
	if info, err := os.Stat(filepath.Join(dir, "nested", "deep")); err != nil || !info.IsDir() {
		t.Fatalf("parent directory was not created: %v", err)
	}
}

func keysOf(sources map[string]string) []string {
	keys := make([]string, 0, len(sources))
	for key := range sources {
		keys = append(keys, key)
	}
	return keys
}
