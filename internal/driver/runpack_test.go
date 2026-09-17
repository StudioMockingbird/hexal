package driver

// Pure-Go tests for the checked-in runtime-pack manifest: strict decoding,
// schema rules, path containment, demanded-file existence, and full
// verification. They build a synthetic pack under a temporary directory and
// invoke no external tool.

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

// writeTestPack materializes a synthetic pack whose manifest is exactly
// manifestJSON, plus the given payload files. It returns the runtime root.
func writeTestPack(t *testing.T, manifestJSON string, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	directory := filepath.Join(root, "x86_64-windows-gnu-ucrt")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, content := range files {
		path := filepath.Join(directory, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), []byte(manifestJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

// hashOf renders the manifest hash of one payload byte string.
func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// validPackFiles is the minimal payload a synthetic pack declares.
func validPackFiles() map[string]string {
	return map[string]string{
		"libuv_v1.52.1/include/uv.h":         "uv",
		"libuv_v1.52.1/libuv.a":              "libuv-archive",
		"libuv_v1.52.1/LICENSE":              "libuv-license",
		"mimalloc_v3.5.1/include/mimalloc.h": "mimalloc",
		"mimalloc_v3.5.1/mimalloc.a":         "mimalloc-archive",
		"mimalloc_v3.5.1/LICENSE":            "mimalloc-license",
	}
}

// validPackManifest renders a valid manifest over validPackFiles.
func validPackManifest() string {
	files := validPackFiles()
	var entries []string
	for name, content := range files {
		entries = append(entries, `"`+name+`": "`+hashOf(content)+`"`)
	}
	return `{
  "format_version": 1,
  "runtime_abi_version": ` + "1" + `,
  "target_profile": "x86_64-windows-gnu-ucrt",
  "dependencies": [
    {
      "name": "libuv",
      "include_root": "libuv_v1.52.1/include",
      "archive": "libuv_v1.52.1/libuv.a",
      "system_libraries": ["psapi", "ws2_32"],
      "license_file": "libuv_v1.52.1/LICENSE"
    },
    {
      "name": "mimalloc",
      "include_root": "mimalloc_v3.5.1/include",
      "archive": "mimalloc_v3.5.1/mimalloc.a",
      "system_libraries": ["bcrypt"],
      "license_file": "mimalloc_v3.5.1/LICENSE"
    }
  ],
  "files": { ` + strings.Join(entries, ", ") + ` }
}`
}

func TestResolveZigProfile(t *testing.T) {
	if _, err := resolveZigProfile(compilerTypes.TargetX86_64WindowsGNU); err != nil {
		t.Fatalf("qualified profile rejected: %v", err)
	}
	for _, target := range []string{"", "x86_64-windows-gnu", "x86_64-linux-gnu", "aarch64-macos"} {
		if _, err := resolveZigProfile(compilerTypes.TargetProfileID(target)); err == nil || !strings.Contains(err.Error(), "is not qualified") {
			t.Fatalf("profile %q = %v, want a not-qualified error", target, err)
		}
	}
}

func TestResolveBackendRequiresCompilerPath(t *testing.T) {
	profile := zigProfiles[compilerTypes.TargetX86_64WindowsGNU]
	_, failure := resolveBackend("", profile)
	if failure == nil || failure.Error() != "C backend is required; pass -cc <path>" {
		t.Fatalf("missing -cc = %v, want the exact required diagnostic", failure)
	}
}

func TestResolveBackendRejectsWrongVersion(t *testing.T) {
	t.Setenv("HEXAL_FAKE_ZIG_VERSION", "0.15.0")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	profile := zigProfiles[compilerTypes.TargetX86_64WindowsGNU]
	_, failure := resolveBackend(executable, profile)
	if failure == nil || !strings.Contains(failure.Error(), "requires Zig 0.16.0") {
		t.Fatalf("wrong version = %v, want the exact version diagnostic", failure)
	}
}

func TestLoadRuntimeManifestDemand(t *testing.T) {
	root := writeTestPack(t, validPackManifest(), validPackFiles())

	_, inputs, err := loadRuntimeManifest(root, "x86_64-windows-gnu-ucrt", []compiler.RuntimeDependency{compiler.RuntimeMimalloc})
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(inputs.IncludeDirs) != 1 || !strings.Contains(inputs.IncludeDirs[0], "mimalloc_v3.5.1") {
		t.Fatalf("mimalloc-only include dirs = %v", inputs.IncludeDirs)
	}
	if len(inputs.Archives) != 1 || !strings.Contains(inputs.Archives[0], "mimalloc.a") {
		t.Fatalf("mimalloc-only archives = %v", inputs.Archives)
	}
	if len(inputs.SystemLibraries) != 1 || inputs.SystemLibraries[0] != "bcrypt" {
		t.Fatalf("mimalloc-only libraries = %v", inputs.SystemLibraries)
	}
	if inputs.ManifestDigest == "" || len(inputs.PayloadHashes) == 0 {
		t.Fatalf("identity inputs missing: %+v", inputs)
	}

	_, both, err := loadRuntimeManifest(root, "x86_64-windows-gnu-ucrt", []compiler.RuntimeDependency{compiler.RuntimeLibuv, compiler.RuntimeMimalloc})
	if err != nil {
		t.Fatal(err)
	}
	if len(both.IncludeDirs) != 2 || len(both.Archives) != 2 {
		t.Fatalf("combined inputs = %+v", both)
	}
	if both.SystemLibraries[0] != "psapi" || both.SystemLibraries[len(both.SystemLibraries)-1] != "bcrypt" {
		t.Fatalf("library order = %v", both.SystemLibraries)
	}

	if _, empty, err := loadRuntimeManifest(root, "x86_64-windows-gnu-ucrt", nil); err != nil || len(empty.Archives) != 0 {
		t.Fatalf("no demand = %+v, %v", empty, err)
	}
}

func TestLoadRuntimeManifestMissingPack(t *testing.T) {
	root := t.TempDir()
	_, _, err := loadRuntimeManifest(root, "x86_64-windows-gnu-ucrt", []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err == nil || err.Error() != "runtime pack for x86_64-windows-gnu-ucrt is missing; install the checked-in pack or pass -runtime-dir <path>" {
		t.Fatalf("missing pack = %v", err)
	}
}

func TestLoadRuntimeManifestMissingDemandedFile(t *testing.T) {
	files := validPackFiles()
	delete(files, "mimalloc_v3.5.1/mimalloc.a")
	root := writeTestPack(t, validPackManifest(), files)
	_, _, err := loadRuntimeManifest(root, "x86_64-windows-gnu-ucrt", []compiler.RuntimeDependency{compiler.RuntimeMimalloc})
	if err == nil || !strings.Contains(err.Error(), "runtime pack file mimalloc_v3.5.1/mimalloc.a is missing") {
		t.Fatalf("missing demanded archive = %v", err)
	}
}

func TestManifestRejections(t *testing.T) {
	valid := validPackManifest()
	for _, testCase := range []struct {
		name     string
		manifest string
		want     string
	}{
		{"unknown field", strings.Replace(valid, `"format_version": 1,`, `"format_version": 1, "extra": true,`, 1), "malformed"},
		{"duplicate field", strings.Replace(valid, `"format_version": 1,`, `"format_version": 1, "format_version": 1,`, 1), "repeats field"},
		{"bad format", strings.Replace(valid, `"format_version": 1`, `"format_version": 2`, 1), "format 2 is unsupported"},
		{"bad abi", strings.Replace(valid, `"runtime_abi_version": 1`, `"runtime_abi_version": 2`, 1), "runtime pack ABI 2 is incompatible; this Hexal compiler requires ABI 1"},
		{"wrong target", strings.Replace(valid, `"target_profile": "x86_64-windows-gnu-ucrt"`, `"target_profile": "x86_64-linux-gnu"`, 1), "does not match"},
		{"malformed hash", strings.Replace(valid, hashOf("uv"), "not-a-hash", 1), "malformed hash"},
		{"wrong order", strings.Replace(valid, `"name": "libuv"`, `"name": "mimalloc_x"`, 1), "must declare libuv, mimalloc in order"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := writeTestPack(t, testCase.manifest, validPackFiles())
			_, _, err := loadRuntimeManifest(root, "x86_64-windows-gnu-ucrt", []compiler.RuntimeDependency{compiler.RuntimeLibuv})
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want containing %q", err, testCase.want)
			}
		})
	}
}

// A normal build validates demanded paths and existence but never hashes
// payload bytes; only doctor does.
func TestNormalLoadDoesNotHashPayload(t *testing.T) {
	files := validPackFiles()
	// A manifest whose declared hashes are all wrong but whose files exist.
	wrong := strings.Replace(validPackManifest(), hashOf("libuv-archive"), hashOf("something-else"), 1)
	root := writeTestPack(t, wrong, files)
	_, _, err := loadRuntimeManifest(root, "x86_64-windows-gnu-ucrt", []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err != nil {
		t.Fatalf("normal load hashed payload bytes: %v", err)
	}
	manifest, decodeErr := decodeRuntimeManifest([]byte(wrong))
	if decodeErr != nil {
		t.Fatal(decodeErr)
	}
	if verifyErr := verifyRuntimePack(filepath.Join(root, "x86_64-windows-gnu-ucrt"), manifest); verifyErr == nil {
		t.Fatal("full verification accepted a wrong hash")
	}
}

// A demanded manifest path that escapes the target directory fails before
// compilation.
func TestManifestEscapingPathRejected(t *testing.T) {
	files := validPackFiles()
	escaping := strings.ReplaceAll(validPackManifest(), "libuv_v1.52.1", "../escape")
	root := writeTestPack(t, escaping, files)
	_, _, err := loadRuntimeManifest(root, "x86_64-windows-gnu-ucrt", []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err == nil || !strings.Contains(err.Error(), "escapes the target directory") {
		t.Fatalf("escaping path = %v, want an escape diagnostic", err)
	}
}

func TestVerifyRuntimePack(t *testing.T) {
	files := validPackFiles()
	root := writeTestPack(t, validPackManifest(), files)
	directory := filepath.Join(root, "x86_64-windows-gnu-ucrt")
	manifest, err := decodeRuntimeManifest([]byte(validPackManifest()))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimePack(directory, manifest); err != nil {
		t.Fatalf("valid pack failed verification: %v", err)
	}

	// A corrupt archive is a hash mismatch.
	corruptFiles := validPackFiles()
	corruptFiles["libuv_v1.52.1/libuv.a"] = "corrupted"
	corruptRoot := writeTestPack(t, validPackManifest(), corruptFiles)
	if err := verifyRuntimePack(filepath.Join(corruptRoot, "x86_64-windows-gnu-ucrt"), manifest); err == nil ||
		!strings.Contains(err.Error(), "failed SHA-256 verification") {
		t.Fatalf("corrupt archive = %v, want a hash failure", err)
	}

	// An unlisted payload file is rejected.
	unlistedFiles := validPackFiles()
	unlistedFiles["libuv_v1.52.1/EXTRA"] = "unlisted"
	unlistedRoot := writeTestPack(t, validPackManifest(), unlistedFiles)
	if err := verifyRuntimePack(filepath.Join(unlistedRoot, "x86_64-windows-gnu-ucrt"), manifest); err == nil ||
		!strings.Contains(err.Error(), "is not listed") {
		t.Fatalf("unlisted file = %v, want an unlisted-file failure", err)
	}
}

func TestSafeManifestPath(t *testing.T) {
	directory := t.TempDir()
	for _, bad := range []string{"", "/abs", "a/../b", "a//b", "./a", "a\\b", `C:/abs`, `\\unc\share`} {
		if _, err := safeManifestPath(directory, bad); err == nil {
			t.Errorf("safeManifestPath(%q) accepted an escaping path", bad)
		}
	}
	good, err := safeManifestPath(directory, "libuv_v1.52.1/libuv.a")
	if err != nil || !strings.HasPrefix(good, directory) {
		t.Fatalf("safeManifestPath(good) = %q, %v", good, err)
	}
}
