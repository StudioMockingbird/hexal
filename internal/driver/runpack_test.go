package driver

// Pure-Go tests for the embedded runtime-pack manifest: strict decoding,
// schema rules, path containment, demanded-file existence, digest
// verification, and full verification. They build a synthetic in-memory pack
// with testing/fstest and invoke no external tool.

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"testing/fstest"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
)

// testPackFS builds an in-memory pack filesystem whose manifest is exactly
// manifestJSON, plus the given payload files.
func testPackFS(manifestJSON string, files map[string]string) fs.FS {
	mfs := fstest.MapFS{}
	for name, content := range files {
		mfs[name] = &fstest.MapFile{Data: []byte(content)}
	}
	mfs["manifest.json"] = &fstest.MapFile{Data: []byte(manifestJSON)}
	return mfs
}

// hashOf renders the manifest hash of one payload byte string.
func hashOf(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// validPackFiles is the minimal payload a synthetic pack declares.
func validPackFiles() map[string]string {
	return map[string]string{
		"libuv_v1.52.1/include/uv.h":          "uv",
		"libuv_v1.52.1/libuv.a":               "libuv-archive",
		"libuv_v1.52.1/LICENSE":               "libuv-license",
		"mimalloc_v3.5.1/include/mimalloc.h":  "mimalloc",
		"mimalloc_v3.5.1/mimalloc.a":          "mimalloc-archive",
		"mimalloc_v3.5.1/LICENSE":             "mimalloc-license",
		"utf8proc_v2.11.3/include/utf8proc.h": "utf8proc",
		"utf8proc_v2.11.3/utf8proc.a":         "utf8proc-archive",
		"utf8proc_v2.11.3/LICENSE.md":         "utf8proc-license",
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
  "target_profile": "x86_64-linux-gnu",
  "dependencies": [
    {
      "name": "libuv",
      "include_root": "libuv_v1.52.1/include",
      "archive": "libuv_v1.52.1/libuv.a",
      "system_libraries": ["pthread", "dl", "rt"],
      "license_file": "libuv_v1.52.1/LICENSE"
    },
    {
      "name": "mimalloc",
      "include_root": "mimalloc_v3.5.1/include",
      "archive": "mimalloc_v3.5.1/mimalloc.a",
      "system_libraries": [],
      "license_file": "mimalloc_v3.5.1/LICENSE"
    },
    {
      "name": "utf8proc",
      "include_root": "utf8proc_v2.11.3/include",
      "archive": "utf8proc_v2.11.3/utf8proc.a",
      "system_libraries": [],
      "license_file": "utf8proc_v2.11.3/LICENSE.md"
    }
  ],
  "files": { ` + strings.Join(entries, ", ") + ` }
}`
}

func TestResolveProfile(t *testing.T) {
	for _, target := range []compilerTypes.TargetProfileID{
		compilerTypes.TargetX86_64LinuxGNU,
		compilerTypes.TargetX86_64WindowsGNU,
	} {
		profile, err := resolveProfile(target)
		if err != nil {
			t.Fatalf("qualified profile %s rejected: %v", target, err)
		}
		if profile.profile != target || profile.triple == "" || profile.packDir == "" {
			t.Fatalf("profile for %s = %+v, want matching profile, a nonempty triple, and packDir", target, profile)
		}
	}
	for _, target := range []string{"", "x86_64-windows-gnu", "aarch64-macos"} {
		if _, err := resolveProfile(compilerTypes.TargetProfileID(target)); err == nil || !strings.Contains(err.Error(), "is not qualified for native builds in this release") {
			t.Fatalf("profile %q = %v, want a not-qualified error", target, err)
		}
	}
}

func TestCheckHostPairsKnownTargets(t *testing.T) {
	host := runtime.GOOS + "/" + runtime.GOARCH
	known := map[compilerTypes.TargetProfileID]bool{
		compilerTypes.TargetX86_64LinuxGNU:   true,
		compilerTypes.TargetX86_64WindowsGNU: true,
	}
	for _, target := range []compilerTypes.TargetProfileID{
		compilerTypes.TargetX86_64LinuxGNU,
		compilerTypes.TargetX86_64WindowsGNU,
	} {
		err := checkHost(target)
		if runtime.GOOS == "linux" && runtime.GOARCH == "amd64" {
			if target == compilerTypes.TargetX86_64LinuxGNU && err != nil {
				t.Fatalf("linux/amd64 cannot build its own target: %v", err)
			}
			if target == compilerTypes.TargetX86_64WindowsGNU && (err == nil || !strings.Contains(err.Error(), "this release builds x86-64 Linux on linux/amd64 and x86-64 Windows on windows/amd64")) {
				t.Fatalf("windows target on linux host = %v, want the pair-set diagnostic", err)
			}
		}
		if runtime.GOOS == "windows" && runtime.GOARCH == "amd64" {
			if target == compilerTypes.TargetX86_64WindowsGNU && err != nil {
				t.Fatalf("windows/amd64 cannot build its own target: %v", err)
			}
			if target == compilerTypes.TargetX86_64LinuxGNU && (err == nil || !strings.Contains(err.Error(), "this release builds x86-64 Linux on linux/amd64 and x86-64 Windows on windows/amd64")) {
				t.Fatalf("linux target on windows host = %v, want the pair-set diagnostic", err)
			}
		}
		_ = known
		_ = host
	}
	// An unknown target is not checkHost's failure; resolveProfile owns it.
	if err := checkHost("aarch64-macos"); err != nil {
		t.Fatalf("unknown target = %v, want nil", err)
	}
}

func TestFeatureDefinesMatchTarget(t *testing.T) {
	linux := featureDefines(compilerTypes.TargetX86_64LinuxGNU)
	if len(linux) != 1 || linux[0] != "-D_POSIX_C_SOURCE=200809L" {
		t.Fatalf("linux defines = %v", linux)
	}
	windows := featureDefines(compilerTypes.TargetX86_64WindowsGNU)
	want := []string{
		"-DWIN32_LEAN_AND_MEAN",
		"-D_WIN32_WINNT=0x0A00",
		"-D_CRT_DECLARE_NONSTDC_NAMES=0",
		"-D_CRT_SECURE_NO_WARNINGS",
		"-DUTF8PROC_STATIC",
	}
	if !reflect.DeepEqual(windows, want) {
		t.Fatalf("windows defines = %v, want %v", windows, want)
	}
}

func TestLoadRuntimeManifestDemand(t *testing.T) {
	fsys := testPackFS(validPackManifest(), validPackFiles())

	_, mimalloc, err := loadRuntimeManifest(fsys, compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeMimalloc})
	if err != nil {
		t.Fatalf("load failed: %v", err)
	}
	if len(mimalloc.demanded) != 1 || mimalloc.demanded[0].Name != "mimalloc" {
		t.Fatalf("mimalloc-only demand = %v", mimalloc.demanded)
	}
	if mimalloc.ManifestDigest == "" || len(mimalloc.PayloadHashes) == 0 {
		t.Fatalf("identity inputs missing: %+v", mimalloc)
	}

	_, both, err := loadRuntimeManifest(fsys, compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeLibuv, compiler.RuntimeMimalloc})
	if err != nil {
		t.Fatal(err)
	}
	if len(both.demanded) != 2 {
		t.Fatalf("combined demand = %+v", both.demanded)
	}
	if len(both.SystemLibraries) != 3 || both.SystemLibraries[0] != "pthread" || both.SystemLibraries[2] != "rt" {
		t.Fatalf("library order = %v", both.SystemLibraries)
	}

	if _, empty, err := loadRuntimeManifest(fsys, compilerTypes.TargetX86_64LinuxGNU, nil); err != nil || len(empty.demanded) != 0 || len(empty.PayloadHashes) != 0 {
		t.Fatalf("no demand = %+v, %v", empty, err)
	}
}

func TestLoadRuntimeManifestMissingPack(t *testing.T) {
	_, _, err := loadRuntimeManifest(fstest.MapFS{}, compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err == nil || err.Error() != "embedded runtime pack for x86_64-linux-gnu is missing or corrupt; rebuild bin/hexal" {
		t.Fatalf("missing pack = %v", err)
	}
}

func TestLoadRuntimeManifestMissingDemandedFile(t *testing.T) {
	files := validPackFiles()
	delete(files, "mimalloc_v3.5.1/mimalloc.a")
	fsys := testPackFS(validPackManifest(), files)
	_, _, err := loadRuntimeManifest(fsys, compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeMimalloc})
	if err == nil || !strings.Contains(err.Error(), "mimalloc_v3.5.1/mimalloc.a is missing") {
		t.Fatalf("missing demanded archive = %v", err)
	}
}

// TestRejectDuplicateKeysTreatsEndOfInputAsSuccess pins that the decoder's
// terminal signal is matched structurally: a complete document is accepted, a
// repeated field is rejected by name, and a malformed token is rejected. A
// future decoder that wraps io.EOF keeps working because the end-of-input test
// is errors.Is rather than a comparison of rendered text.
func TestRejectDuplicateKeysTreatsEndOfInputAsSuccess(t *testing.T) {
	if err := rejectDuplicateKeys([]byte(`{"a": 1, "b": [2, 3]}`)); err != nil {
		t.Fatalf("valid document rejected: %v", err)
	}
	if err := rejectDuplicateKeys([]byte(`{"a": 1, "a": 2}`)); err == nil || !strings.Contains(err.Error(), "repeats field") {
		t.Fatalf("duplicate key = %v", err)
	}
	if err := rejectDuplicateKeys([]byte(`}`)); err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("malformed token = %v", err)
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
		{"wrong target", strings.Replace(valid, `"target_profile": "x86_64-linux-gnu"`, `"target_profile": "x86_64-windows-gnu-ucrt"`, 1), "does not match"},
		{"malformed hash", strings.Replace(valid, hashOf("uv"), "not-a-hash", 1), "malformed hash"},
		{"wrong order", strings.Replace(valid, `"name": "libuv"`, `"name": "mimalloc_x"`, 1), "must declare libuv, mimalloc, utf8proc in order"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			fsys := testPackFS(testCase.manifest, validPackFiles())
			_, _, err := loadRuntimeManifest(fsys, compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeLibuv})
			if err == nil || !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %v, want containing %q", err, testCase.want)
			}
		})
	}
}

// A normal build verifies the demanded dependencies' bytes against their
// listed digests; an undemanded dependency's digest is never read.
func TestLoadVerifiesOnlyDemandedPayload(t *testing.T) {
	// A demanded libuv archive whose listed digest is wrong.
	wrong := strings.Replace(validPackManifest(), hashOf("libuv-archive"), hashOf("something-else"), 1)
	_, _, err := loadRuntimeManifest(testPackFS(wrong, validPackFiles()), compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err == nil || !strings.Contains(err.Error(), "failed SHA-256 verification") {
		t.Fatalf("demanded wrong hash = %v, want a verification failure", err)
	}

	// The same wrong digest is not read when only mimalloc is demanded.
	if _, _, err := loadRuntimeManifest(testPackFS(wrong, validPackFiles()), compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeMimalloc}); err != nil {
		t.Fatalf("undemanded wrong hash was read: %v", err)
	}
}

// A demanded manifest path that escapes the target directory fails before
// compilation.
func TestManifestEscapingPathRejected(t *testing.T) {
	escaping := strings.ReplaceAll(validPackManifest(), "libuv_v1.52.1", "../escape")
	_, _, err := loadRuntimeManifest(testPackFS(escaping, validPackFiles()), compilerTypes.TargetX86_64LinuxGNU, []compiler.RuntimeDependency{compiler.RuntimeLibuv})
	if err == nil || !strings.Contains(err.Error(), "escapes the target directory") {
		t.Fatalf("escaping path = %v, want an escape diagnostic", err)
	}
}

func TestVerifyRuntimePack(t *testing.T) {
	manifest, err := decodeRuntimeManifest([]byte(validPackManifest()))
	if err != nil {
		t.Fatal(err)
	}
	if err := verifyRuntimePack(testPackFS(validPackManifest(), validPackFiles()), manifest); err != nil {
		t.Fatalf("valid pack failed verification: %v", err)
	}

	corruptFiles := validPackFiles()
	corruptFiles["libuv_v1.52.1/libuv.a"] = "corrupted"
	if err := verifyRuntimePack(testPackFS(validPackManifest(), corruptFiles), manifest); err == nil ||
		!strings.Contains(err.Error(), "failed SHA-256 verification") {
		t.Fatalf("corrupt archive = %v, want a hash failure", err)
	}

	unlistedFiles := validPackFiles()
	unlistedFiles["libuv_v1.52.1/EXTRA"] = "unlisted"
	if err := verifyRuntimePack(testPackFS(validPackManifest(), unlistedFiles), manifest); err == nil ||
		!strings.Contains(err.Error(), "is not listed") {
		t.Fatalf("unlisted file = %v, want an unlisted-file failure", err)
	}
}

func TestValidPackPath(t *testing.T) {
	for _, bad := range []string{"", "/abs", "a/../b", "a//b", "./a", "a\\b", `C:/abs`, `\\unc\share`} {
		if validPackPath(bad) {
			t.Errorf("validPackPath(%q) accepted an escaping path", bad)
		}
	}
	if !validPackPath("libuv_v1.52.1/libuv.a") {
		t.Fatal("validPackPath rejected a rooted relative path")
	}
}
