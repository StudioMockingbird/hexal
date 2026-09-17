//go:build c23

package c23validation

// Native runtime dependencies for the harness. A program whose compilation
// result names mimalloc or libuv links the checked-in Linux pack the way the
// build driver does: the embedded manifest supplies each demanded dependency's
// include root, archive, and system libraries, so the harness keeps no second
// source list and compiles no dependency source. Every demanded entry is
// materialized under one staging directory and shared by every fixture in the
// same build root.

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"hexal/compiler"
	"hexal/lib"
)

// packDirName is the one target-profile pack this Linux harness consumes; it
// matches compilerTypes.TargetX86_64LinuxGNU.
const packDirName = "x86_64-linux-gnu"

// packManifest mirrors the closed format-version-1 runtime manifest far
// enough to locate each demanded dependency's headers, archive, and system
// libraries. Unknown fields are ignored because the pack is compiler-owned.
type packManifest struct {
	TargetProfile string           `json:"target_profile"`
	Dependencies  []packDependency `json:"dependencies"`
}

// packDependency is one manifest entry, in declared link order.
type packDependency struct {
	Name            string   `json:"name"`
	IncludeRoot     string   `json:"include_root"`
	Archive         string   `json:"archive"`
	SystemLibraries []string `json:"system_libraries"`
}

// dependencyBuild is the materialized dependency set: the options a generated
// translation unit needs to see the dependency headers, the archives to link,
// and the system libraries.
type dependencyBuild struct {
	once           sync.Once
	includeOptions []string
	archives       []string
	linkOptions    []string
	err            error
}

var (
	dependencyCacheMu sync.Mutex
	dependencyCache   = map[string]*dependencyBuild{}
)

// buildDependencies returns the materialized dependency set for dependencies
// under buildRoot, materializing it on first use.
func buildDependencies(dependencies []compiler.RuntimeDependency, buildRoot string) *dependencyBuild {
	names := make([]string, 0, len(dependencies))
	for _, dependency := range dependencies {
		names = append(names, string(dependency))
	}
	set := strings.Join(names, "+")
	key := set + "|" + buildRoot

	dependencyCacheMu.Lock()
	entry, ok := dependencyCache[key]
	if !ok {
		entry = &dependencyBuild{}
		dependencyCache[key] = entry
	}
	dependencyCacheMu.Unlock()

	entry.once.Do(func() {
		entry.err = materializeDependencies(entry, dependencies, buildRoot, set)
	})
	return entry
}

// materializeDependencies copies the demanded checked-in pack entries into
// buildRoot and records the include, archive, and system-library options the
// generated C and link need. Nothing is compiled.
func materializeDependencies(entry *dependencyBuild, dependencies []compiler.RuntimeDependency, buildRoot, set string) error {
	pack, err := fs.Sub(lib.RuntimePacks(), packDirName)
	if err != nil {
		return fmt.Errorf("open embedded runtime pack %s: %w", packDirName, err)
	}
	raw, err := fs.ReadFile(pack, "manifest.json")
	if err != nil {
		return fmt.Errorf("read embedded runtime manifest: %w", err)
	}
	var manifest packManifest
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return fmt.Errorf("parse embedded runtime manifest: %w", err)
	}
	if manifest.TargetProfile != packDirName {
		return fmt.Errorf("embedded runtime pack names target %q, want %q", manifest.TargetProfile, packDirName)
	}
	demanded := make(map[string]bool, len(dependencies))
	for _, dependency := range dependencies {
		demanded[string(dependency)] = true
	}
	staging := filepath.Join(buildRoot, "dependencies", set)
	if err := os.MkdirAll(staging, 0755); err != nil {
		return err
	}
	seenLibrary := make(map[string]bool)
	found := make(map[string]bool, len(demanded))
	for _, dependency := range manifest.Dependencies {
		if !demanded[dependency.Name] {
			continue
		}
		found[dependency.Name] = true
		if err := materializePackPath(pack, dependency.IncludeRoot, staging); err != nil {
			return fmt.Errorf("materialize %s headers: %w", dependency.Name, err)
		}
		if err := materializePackPath(pack, dependency.Archive, staging); err != nil {
			return fmt.Errorf("materialize %s archive: %w", dependency.Name, err)
		}
		// Generated C sees only the dependency include roots, as system
		// headers: it is compiled with -Werror, its own sources already
		// define the platform macros they need, and a header warning is not
		// a generated-code defect.
		entry.includeOptions = append(entry.includeOptions, "-isystem", filepath.Join(staging, filepath.FromSlash(dependency.IncludeRoot)))
		entry.archives = append(entry.archives, filepath.Join(staging, filepath.FromSlash(dependency.Archive)))
		for _, library := range dependency.SystemLibraries {
			if seenLibrary[library] {
				continue
			}
			seenLibrary[library] = true
			entry.linkOptions = append(entry.linkOptions, "-l"+library)
		}
	}
	for name := range demanded {
		if !found[name] {
			return fmt.Errorf("embedded runtime pack declares no dependency named %q", name)
		}
	}
	return nil
}

// materializePackPath copies one pack-relative file or directory tree into
// staging at the same relative path, so a staged include root and archive
// keep the manifest's own layout.
func materializePackPath(pack fs.FS, relative, staging string) error {
	return fs.WalkDir(pack, relative, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := filepath.Join(staging, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.MkdirAll(target, 0755)
		}
		content, readErr := fs.ReadFile(pack, path)
		if readErr != nil {
			return readErr
		}
		if mkErr := os.MkdirAll(filepath.Dir(target), 0755); mkErr != nil {
			return mkErr
		}
		return os.WriteFile(target, content, 0644)
	})
}
