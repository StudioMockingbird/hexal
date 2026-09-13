package compiler

import "sort"

// RuntimeDependency names a native runtime input required by generated C.
// Values are logical identities; target-pack paths remain owned by the build
// driver and never enter the in-memory compiler.
type RuntimeDependency string

const (
	RuntimeMimalloc RuntimeDependency = "mimalloc"
	RuntimeLibuv    RuntimeDependency = "libuv"
)

func runtimeDependencies(values []string) []RuntimeDependency {
	dependencies := make([]RuntimeDependency, 0, len(values))
	seen := make(map[RuntimeDependency]bool, len(values))
	for _, value := range values {
		switch RuntimeDependency(value) {
		case RuntimeMimalloc, RuntimeLibuv:
			dependency := RuntimeDependency(value)
			if !seen[dependency] {
				seen[dependency] = true
				dependencies = append(dependencies, dependency)
			}
		default:
			panic("generator returned unknown runtime dependency " + value)
		}
	}
	sort.Slice(dependencies, func(left, right int) bool { return dependencies[left] < dependencies[right] })
	return dependencies
}
