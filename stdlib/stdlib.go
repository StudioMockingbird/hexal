// Package stdlib embeds the Hexal source standard library in the compiler
// binary, so a compilation needs no host filesystem access to find it. The
// embed root cannot reference a parent directory, which is why the sources
// live under this package rather than under compiler/.
package stdlib

import (
	"embed"
	"io/fs"
	"strings"
)

//go:embed std
var embedded embed.FS

// Sources returns a fresh copy of every embedded source stdlib module, keyed
// by its source key "stdlib/std/<path>.hex". Each call returns newly allocated
// strings, so a caller can never mutate a later compilation's stdlib.
func Sources() map[string]string {
	sources := make(map[string]string)
	_ = fs.WalkDir(embedded, "std", func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		body, readErr := embedded.ReadFile(path)
		if readErr != nil {
			return nil
		}
		sources["stdlib/"+path] = string(body)
		return nil
	})
	return sources
}

// sourceModules is the immutable canonical set of embedded source stdlib
// modules, derived once so existence checks never rebuild the map.
var sourceModules = func() map[string]bool {
	modules := make(map[string]bool)
	for key := range Sources() {
		modules[canonicalFromSourceKey(key)] = true
	}
	return modules
}()

// IsSourceModule reports whether canonical names an embedded source stdlib
// module ("std/ascii", ...).
func IsSourceModule(canonical string) bool {
	return sourceModules[canonical]
}

// SourceKey returns the source key of a canonical source stdlib module.
func SourceKey(canonical string) string {
	return "stdlib/" + canonical + ".hex"
}

// canonicalFromSourceKey inverts SourceKey for one embedded source key.
func canonicalFromSourceKey(key string) string {
	return strings.TrimSuffix(strings.TrimPrefix(key, "stdlib/"), ".hex")
}
