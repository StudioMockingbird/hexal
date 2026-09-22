package compiler

import (
	"sort"

	"hexal/compiler/specdata"
)

// RuntimeDependency names a native runtime input required by generated C.
// Values are logical identities; target-pack paths remain owned by the build
// driver and never enter the in-memory compiler. The identities are declared in
// compiler/specdata, the sole storage owner; these exported names alias them
// while their last consumer remains.
type RuntimeDependency string

const (
	RuntimeMimalloc RuntimeDependency = RuntimeDependency(specdata.DependencyMimalloc)
	RuntimeLibuv    RuntimeDependency = RuntimeDependency(specdata.DependencyLibuv)
	RuntimeUtf8proc RuntimeDependency = RuntimeDependency(specdata.DependencyUtf8proc)
)

// runtimeDependencies validates the generator's dependency names against the
// registry, deduplicates them, and sorts them into deterministic order. An
// unknown name is a generator defect, never a silently dropped dependency.
func runtimeDependencies(values []string) []RuntimeDependency {
	dependencies := make([]RuntimeDependency, 0, len(values))
	seen := make(map[RuntimeDependency]bool, len(values))
	for _, value := range values {
		if _, known := specdata.Dependency(specdata.DependencyID(value)); !known {
			panic("generator returned unknown runtime dependency " + value)
		}
		dependency := RuntimeDependency(value)
		if !seen[dependency] {
			seen[dependency] = true
			dependencies = append(dependencies, dependency)
		}
	}
	sort.Slice(dependencies, func(left, right int) bool { return dependencies[left] < dependencies[right] })
	return dependencies
}
