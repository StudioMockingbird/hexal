package driver

// Embedded runtime-pack resolution, verification, and demand-driven
// materialization. The driver opens the pack only when the program selects a
// runtime dependency, validates the demanded entries against their listed
// digests, and materializes only those entries into the private build staging
// directory. Nothing is read from a source checkout or a user directory, and
// there is no adjacent-pack fallback or -runtime-dir override.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"hexal/compiler"
	compilerConfig "hexal/compiler/config"
	compilerTypes "hexal/compiler/types"
	"hexal/internal/backend"
	"hexal/lib"
)

// runtimeManifest is the closed v1 manifest of one checked-in runtime pack.
type runtimeManifest struct {
	FormatVersion     int                 `json:"format_version"`
	RuntimeABIVersion uint32              `json:"runtime_abi_version"`
	TargetProfile     string              `json:"target_profile"`
	Dependencies      []runtimeDependency `json:"dependencies"`
	Files             map[string]string   `json:"files"`
}

// runtimeDependency is one archive and its public header closure.
type runtimeDependency struct {
	Name            string   `json:"name"`
	IncludeRoot     string   `json:"include_root"`
	Archive         string   `json:"archive"`
	SystemLibraries []string `json:"system_libraries"`
	LicenseFile     string   `json:"license_file"`
}

// packInputs is the demanded slice of one pack a normal build consumes.
type packInputs struct {
	IncludeDirs     []string
	Archives        []string
	SystemLibraries []string
	// ManifestDigest is the exact manifest bytes' SHA-256, for the build
	// identity.
	ManifestDigest string
	// PayloadHashes are the listed hashes of the selected dependencies'
	// archive, include-root, and license files, for the build identity.
	PayloadHashes []string
	// demanded is the manifest-ordered dependency list to materialize.
	demanded []runtimeDependency
}

// runtimePackFS is the private filesystem seam tests replace. Production uses
// the checked-in pack embedded in the compiler binary, rooted at the selected
// target profile's directory.
func runtimePackFS(target compilerTypes.TargetProfileID) (fs.FS, error) {
	sub, err := fs.Sub(lib.RuntimePacks(), string(target))
	if err != nil {
		return nil, fmt.Errorf("embedded runtime pack for %s is missing or corrupt; rebuild bin/hexal", target)
	}
	return sub, nil
}

// packSystemLibraryOptions renders the demanded runtime system libraries as
// linker arguments, in manifest dependency then occurrence order.
func packSystemLibraryOptions(pack packInputs) []string {
	options := make([]string, 0, len(pack.SystemLibraries))
	for _, library := range pack.SystemLibraries {
		options = append(options, backend.SystemLibraryArgument(library))
	}
	return options
}

// loadRuntimeManifest reads and strictly validates the embedded manifest for
// the selected profile, then verifies every demanded dependency's embedded
// bytes against their listed digests. It returns the manifest, its exact
// bytes' digest, and the demanded inputs. It writes nothing.
func loadRuntimeManifest(fsys fs.FS, target compilerTypes.TargetProfileID, required []compiler.RuntimeDependency) (runtimeManifest, packInputs, error) {
	raw, readErr := fs.ReadFile(fsys, "manifest.json")
	if readErr != nil {
		return runtimeManifest{}, packInputs{}, fmt.Errorf("embedded runtime pack for %s is missing or corrupt; rebuild bin/hexal", target)
	}
	manifest, err := decodeRuntimeManifest(raw)
	if err != nil {
		return runtimeManifest{}, packInputs{}, err
	}
	if err := validateRuntimeManifest(manifest, target); err != nil {
		return runtimeManifest{}, packInputs{}, err
	}
	digest := sha256.Sum256(raw)
	inputs := packInputs{ManifestDigest: hex.EncodeToString(digest[:])}

	selected := make(map[string]bool, len(required))
	for _, dependency := range required {
		selected[string(dependency)] = true
	}
	for _, dependency := range manifest.Dependencies {
		if !selected[dependency.Name] {
			continue
		}
		for _, file := range selectedHashes(manifest, dependency) {
			content, readErr := fs.ReadFile(fsys, file)
			if readErr != nil {
				return runtimeManifest{}, packInputs{}, fmt.Errorf("embedded runtime pack file %s is missing; rebuild bin/hexal", file)
			}
			sum := sha256.Sum256(content)
			if got := hex.EncodeToString(sum[:]); got != manifest.Files[file] {
				return runtimeManifest{}, packInputs{}, fmt.Errorf("embedded runtime pack file %s failed SHA-256 verification; rebuild bin/hexal", file)
			}
			inputs.PayloadHashes = append(inputs.PayloadHashes, manifest.Files[file])
		}
		inputs.SystemLibraries = append(inputs.SystemLibraries, dependency.SystemLibraries...)
		inputs.demanded = append(inputs.demanded, dependency)
	}
	return manifest, inputs, nil
}

// materializePack writes exactly the demanded dependencies' include trees and
// archives beneath the private staging directory and returns their staging
// paths in manifest dependency order. It never writes a license or an
// undemanded entry.
func materializePack(staging string, fsys fs.FS, pack packInputs) ([]string, []string, error) {
	includeDirs := make([]string, 0, len(pack.demanded))
	archives := make([]string, 0, len(pack.demanded))
	for _, dependency := range pack.demanded {
		includeDir := filepath.Join(staging, "dependencies", dependency.Name, "include")
		if err := materializeTree(fsys, dependency.IncludeRoot, includeDir); err != nil {
			return nil, nil, err
		}
		archive := filepath.Join(staging, "dependencies", dependency.Name, filepath.Base(dependency.Archive))
		if err := materializeFile(fsys, dependency.Archive, archive); err != nil {
			return nil, nil, err
		}
		includeDirs = append(includeDirs, includeDir)
		archives = append(archives, archive)
	}
	return includeDirs, archives, nil
}

// materializeTree copies one embedded directory tree under destination.
func materializeTree(fsys fs.FS, root, destination string) error {
	return fs.WalkDir(fsys, root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		relative, err := filepath.Rel(filepath.FromSlash(root), filepath.FromSlash(path))
		if err != nil {
			return err
		}
		return materializeFile(fsys, path, filepath.Join(destination, relative))
	})
}

// materializeFile copies one embedded file to destination, creating parents.
func materializeFile(fsys fs.FS, path, destination string) error {
	content, err := fs.ReadFile(fsys, path)
	if err != nil {
		return fmt.Errorf("embedded runtime pack file %s is missing; rebuild bin/hexal", path)
	}
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destination, content, 0o644)
}

// selectedHashes returns the manifest file keys belonging to one dependency's
// versioned directory, sorted.
func selectedHashes(manifest runtimeManifest, dependency runtimeDependency) []string {
	prefix := versionedPrefix(dependency.IncludeRoot)
	keys := make([]string, 0)
	for key := range manifest.Files {
		if strings.HasPrefix(key, prefix) {
			keys = append(keys, key)
		}
	}
	slices.Sort(keys)
	return keys
}

// versionedPrefix returns the dependency versioned directory prefix of one
// manifest path ("libuv_v1.52.1/" for "libuv_v1.52.1/include").
func versionedPrefix(path string) string {
	if index := strings.IndexByte(path, '/'); index >= 0 {
		return path[:index+1]
	}
	return path + "/"
}

// decodeRuntimeManifest rejects duplicate JSON names, unknown fields, and
// trailing content before decoding into the closed schema.
func decodeRuntimeManifest(raw []byte) (runtimeManifest, error) {
	if err := rejectDuplicateKeys(raw); err != nil {
		return runtimeManifest{}, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	var manifest runtimeManifest
	if err := decoder.Decode(&manifest); err != nil {
		return runtimeManifest{}, fmt.Errorf("runtime pack manifest is malformed: %v", err)
	}
	if decoder.More() {
		return runtimeManifest{}, fmt.Errorf("runtime pack manifest has trailing content")
	}
	return manifest, nil
}

// rejectDuplicateKeys fails when any JSON object repeats a name. The standard
// decoder silently keeps the last value, which would let a malformed manifest
// pass.
func rejectDuplicateKeys(raw []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	type frame struct {
		object    bool
		seen      map[string]bool
		expectKey bool
	}
	stack := make([]frame, 0, 8)
	for {
		token, err := decoder.Token()
		if err != nil {
			if err.Error() == "EOF" {
				return nil
			}
			return fmt.Errorf("runtime pack manifest is malformed: %v", err)
		}
		switch value := token.(type) {
		case json.Delim:
			switch value {
			case '{':
				stack = append(stack, frame{object: true, seen: map[string]bool{}, expectKey: true})
			case '[':
				stack = append(stack, frame{})
			case '}', ']':
				if len(stack) == 0 {
					return fmt.Errorf("runtime pack manifest is malformed")
				}
				stack = stack[:len(stack)-1]
				if len(stack) > 0 && stack[len(stack)-1].object {
					stack[len(stack)-1].expectKey = true
				}
			}
		case string:
			if len(stack) > 0 && stack[len(stack)-1].object && stack[len(stack)-1].expectKey {
				if stack[len(stack)-1].seen[value] {
					return fmt.Errorf("runtime pack manifest repeats field %s", value)
				}
				stack[len(stack)-1].seen[value] = true
				stack[len(stack)-1].expectKey = false
			} else if len(stack) > 0 && stack[len(stack)-1].object {
				stack[len(stack)-1].expectKey = true
			}
		default:
			if len(stack) > 0 && stack[len(stack)-1].object {
				stack[len(stack)-1].expectKey = true
			}
		}
	}
}

// validateRuntimeManifest enforces the closed schema's semantic rules.
func validateRuntimeManifest(manifest runtimeManifest, target compilerTypes.TargetProfileID) error {
	if manifest.FormatVersion != 1 {
		return fmt.Errorf("runtime pack format %d is unsupported", manifest.FormatVersion)
	}
	if manifest.RuntimeABIVersion != compilerConfig.RuntimeABIVersion {
		return fmt.Errorf("runtime pack ABI %d is incompatible; this Hexal compiler requires ABI %d", manifest.RuntimeABIVersion, compilerConfig.RuntimeABIVersion)
	}
	if manifest.TargetProfile != string(target) {
		return fmt.Errorf("runtime pack target %s does not match %s", manifest.TargetProfile, target)
	}
	want := []string{"libuv", "mimalloc", "utf8proc"}
	if len(manifest.Dependencies) != len(want) {
		return fmt.Errorf("runtime pack must declare %s in order", strings.Join(want, ", "))
	}
	for index, name := range want {
		if manifest.Dependencies[index].Name != name {
			return fmt.Errorf("runtime pack must declare %s in order", strings.Join(want, ", "))
		}
	}
	seenNames := map[string]bool{}
	seenPaths := map[string]bool{}
	for _, dependency := range manifest.Dependencies {
		if seenNames[dependency.Name] {
			return fmt.Errorf("runtime pack repeats dependency %s", dependency.Name)
		}
		seenNames[dependency.Name] = true
		for _, path := range []string{dependency.IncludeRoot, dependency.Archive, dependency.LicenseFile} {
			if !validPackPath(path) {
				return fmt.Errorf("runtime pack path %s escapes the target directory", path)
			}
			if seenPaths[path] {
				return fmt.Errorf("runtime pack repeats path %s", path)
			}
			seenPaths[path] = true
		}
		if versionedPrefix(dependency.IncludeRoot) != versionedPrefix(dependency.Archive) ||
			versionedPrefix(dependency.Archive) != versionedPrefix(dependency.LicenseFile) {
			return fmt.Errorf("runtime pack dependency %s paths must share one versioned directory", dependency.Name)
		}
	}
	for path, digest := range manifest.Files {
		if !validPackPath(path) {
			return fmt.Errorf("runtime pack path %s escapes the target directory", path)
		}
		if !validManifestHash(digest) {
			return fmt.Errorf("runtime pack file %s has a malformed hash", path)
		}
	}
	return nil
}

// validPackPath reports whether one embedded manifest path is a rooted,
// slash-separated path with no ".", "..", backslash, or absolute form. The
// embedded filesystem rejects a leading slash or a drive letter, so this is a
// defense against a manifest that names one.
func validPackPath(path string) bool {
	if path == "" || strings.Contains(path, "\\") || strings.HasPrefix(path, "/") {
		return false
	}
	if len(path) >= 2 && path[1] == ':' {
		return false
	}
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return false
	}
	for _, component := range strings.Split(path, "/") {
		if component == "" || component == "." || component == ".." {
			return false
		}
	}
	return true
}

// validManifestHash reports whether digest is exactly 64 lowercase hex
// characters.
func validManifestHash(digest string) bool {
	if len(digest) != 64 {
		return false
	}
	for index := 0; index < len(digest); index++ {
		character := digest[index]
		if !(character >= '0' && character <= '9' || character >= 'a' && character <= 'f') {
			return false
		}
	}
	return true
}

// verifyRuntimePack hashes every listed file, rejects an unlisted regular
// payload file, and reports the first mismatch. Doctor uses it; a normal build
// never hashes undemanded bytes.
func verifyRuntimePack(fsys fs.FS, manifest runtimeManifest) error {
	listed := make(map[string]bool, len(manifest.Files))
	for path, digest := range manifest.Files {
		listed[path] = true
		content, err := fs.ReadFile(fsys, path)
		if err != nil {
			return fmt.Errorf("runtime pack file %s is missing", path)
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != digest {
			return fmt.Errorf("runtime pack file %s failed SHA-256 verification; restore the checked-in pack", path)
		}
	}
	return fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		if path == "manifest.json" || listed[path] {
			return nil
		}
		return fmt.Errorf("runtime pack file %s is not listed in the manifest", path)
	})
}
