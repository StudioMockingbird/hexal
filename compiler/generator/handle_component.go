package generator

// handleSelected reports whether the program selects the shared
// generation-checked handle registry and its common libuv ErrorKind mapper:
// File, TCP (connections and listeners), or DNS (which has no handle of its
// own but shares the common mapper for its libuv failures). Address
// parse/format needs neither.
func handleSelected(merged *programEmission) bool {
	return (merged.fileState != nil && merged.fileState.used) ||
		(merged.networkState != nil && (merged.networkState.dns || merged.networkState.tcp)) ||
		(merged.processState != nil && merged.processState.used) ||
		(merged.signalState != nil && merged.signalState.used) ||
		// The core-library path and entropy runtimes classify their libuv
		// failures through the same mapper, so they select its component.
		(merged.corelibState != nil && (merged.corelibState.paths || merged.corelibState.entropy))
}

// handleComponents returns hexal/handle.h and hexal/handle.c when a
// copied-handle capability is reachable. The registry, generation check,
// lifecycle transitions, and common libuv Error mapper live here so no
// capability-specific component copies them.
func handleComponents(merged *programEmission) ([]componentArtifact, error) {
	if !handleSelected(merged) {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/handle.h", template: "handle.h", model: struct{}{}},
		{key: "hexal/handle.c", template: "handle.c", model: struct{}{}},
	}, nil
}
