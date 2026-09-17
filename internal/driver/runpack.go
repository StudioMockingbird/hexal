package driver

// Checked-in runtime-pack resolution and verification. A normal build resolves
// the pack only when the program selects a runtime dependency, validates the
// demanded entries, and never hashes payload bytes. Doctor performs the full
// hash and completeness verification.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"hexal/compiler"
	compilerTypes "hexal/compiler/types"
	"hexal/internal/backend"
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
}

// resolveRuntimeRoot selects the runtime-pack root: the explicit override, or
// the lib/ directory beside the physical running executable. It never searches
// the working tree, parents, or system paths.
func resolveRuntimeRoot(override string) (string, error) {
	if override != "" {
		absolute, err := filepath.Abs(override)
		if err != nil {
			return "", fmt.Errorf("cannot resolve -runtime-dir %q: %v", override, err)
		}
		return filepath.Clean(absolute), nil
	}
	executable, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot locate the running executable: %v", err)
	}
	if resolved, err := filepath.EvalSymlinks(executable); err == nil {
		executable = resolved
	}
	return filepath.Join(filepath.Dir(executable), "lib"), nil
}

// packDirectory is the manifest directory for one profile under a runtime
// root.
func packDirectory(root string, profile zigProfile) string {
	return filepath.Join(root, profile.packDir)
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

// loadRuntimeManifest reads, strictly decodes, and validates the pack manifest
// for the selected profile, then requires every demanded dependency's paths to
// exist. It returns the manifest, its exact bytes' digest, and the demanded
// inputs.
func loadRuntimeManifest(root string, target compilerTypes.TargetProfileID, required []compiler.RuntimeDependency) (runtimeManifest, packInputs, error) {
	profile, err := resolveZigProfile(target)
	if err != nil {
		return runtimeManifest{}, packInputs{}, err
	}
	directory := packDirectory(root, profile)
	manifestPath := filepath.Join(directory, "manifest.json")
	raw, readErr := os.ReadFile(manifestPath)
	if readErr != nil {
		return runtimeManifest{}, packInputs{}, fmt.Errorf("runtime pack for %s is missing; install the checked-in pack or pass -runtime-dir <path>", target)
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
		includeDir, pathErr := safeManifestPath(directory, dependency.IncludeRoot)
		if pathErr != nil {
			return runtimeManifest{}, packInputs{}, pathErr
		}
		if info, statErr := os.Stat(includeDir); statErr != nil || !info.IsDir() {
			return runtimeManifest{}, packInputs{}, fmt.Errorf("runtime pack file %s is missing", dependency.IncludeRoot)
		}
		archive, pathErr := safeManifestPath(directory, dependency.Archive)
		if pathErr != nil {
			return runtimeManifest{}, packInputs{}, pathErr
		}
		if info, statErr := os.Stat(archive); statErr != nil || !info.Mode().IsRegular() {
			return runtimeManifest{}, packInputs{}, fmt.Errorf("runtime pack file %s is missing", dependency.Archive)
		}
		if _, pathErr := safeManifestPath(directory, dependency.LicenseFile); pathErr != nil {
			return runtimeManifest{}, packInputs{}, pathErr
		}
		inputs.IncludeDirs = append(inputs.IncludeDirs, includeDir)
		inputs.Archives = append(inputs.Archives, archive)
		inputs.SystemLibraries = append(inputs.SystemLibraries, dependency.SystemLibraries...)
		for _, file := range selectedHashes(manifest, dependency) {
			inputs.PayloadHashes = append(inputs.PayloadHashes, manifest.Files[file])
		}
	}
	return manifest, inputs, nil
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
	sort.Strings(keys)
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
	if manifest.RuntimeABIVersion != compiler.RuntimeABIVersion {
		return fmt.Errorf("runtime pack ABI %d is incompatible; this Hexal compiler requires ABI %d", manifest.RuntimeABIVersion, compiler.RuntimeABIVersion)
	}
	if manifest.TargetProfile != string(target) {
		return fmt.Errorf("runtime pack target %s does not match %s", manifest.TargetProfile, target)
	}
	want := []string{"libuv", "mimalloc"}
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
		if !validManifestHash(digest) {
			return fmt.Errorf("runtime pack file %s has a malformed hash", path)
		}
	}
	return nil
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

// safeManifestPath resolves one manifest path under directory, rejecting
// absolute, drive-qualified, UNC, empty, ".", "..", and backslash forms, any
// path that normalizes outside directory, and every symlink or reparse-point
// component.
func safeManifestPath(directory, relative string) (string, error) {
	if relative == "" || strings.Contains(relative, "\\") || strings.HasPrefix(relative, "/") ||
		filepath.IsAbs(relative) || filepath.VolumeName(relative) != "" {
		return "", fmt.Errorf("runtime pack path %s escapes the target directory", relative)
	}
	for _, component := range strings.Split(relative, "/") {
		if component == "" || component == "." || component == ".." {
			return "", fmt.Errorf("runtime pack path %s escapes the target directory", relative)
		}
	}
	target := filepath.Join(directory, filepath.FromSlash(relative))
	absoluteDirectory, err := filepath.Abs(directory)
	if err != nil {
		return "", err
	}
	absoluteTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	if absoluteTarget != absoluteDirectory && !strings.HasPrefix(absoluteTarget, absoluteDirectory+string(os.PathSeparator)) {
		return "", fmt.Errorf("runtime pack path %s escapes the target directory", relative)
	}
	// Every component must be an ordinary directory or file, never a symlink
	// or junction.
	current := absoluteDirectory
	for _, component := range strings.Split(relative, "/") {
		current = filepath.Join(current, component)
		info, statErr := os.Lstat(current)
		if statErr != nil {
			continue
		}
		if info.Mode()&os.ModeSymlink != 0 || isJunction(current) {
			return "", fmt.Errorf("runtime pack path %s escapes the target directory", relative)
		}
	}
	return absoluteTarget, nil
}

// verifyRuntimePack hashes every listed file, rejects an unlisted regular
// payload file, and reports the first mismatch. Doctor uses it; a normal build
// never does.
func verifyRuntimePack(directory string, manifest runtimeManifest) error {
	listed := make(map[string]bool, len(manifest.Files))
	for path, digest := range manifest.Files {
		listed[path] = true
		absolute, err := safeManifestPath(directory, path)
		if err != nil {
			return err
		}
		content, readErr := os.ReadFile(absolute)
		if readErr != nil {
			return fmt.Errorf("runtime pack file %s is missing", path)
		}
		sum := sha256.Sum256(content)
		if hex.EncodeToString(sum[:]) != digest {
			return fmt.Errorf("runtime pack file %s failed SHA-256 verification; restore the checked-in pack", path)
		}
	}
	return filepath.WalkDir(directory, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		relative, err := filepath.Rel(directory, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(relative)
		if key == "manifest.json" {
			return nil
		}
		if !listed[key] {
			return fmt.Errorf("runtime pack file %s is not listed in the manifest", key)
		}
		return nil
	})
}
