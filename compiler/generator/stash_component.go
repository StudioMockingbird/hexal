package generator

// stashComponents returns the generated hexal/stash.h and hexal/stash.c
// artifacts when any Stash is used. The core is type-erased (one hex_stash
// struct and bump-allocation implementation shared by every Stash<T>
// regardless of T), so unlike List/Dict it needs no per-specialization
// render model: it is either wholly present or wholly absent.
func stashComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || !merged.stashUsed {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/stash.h", template: "stash.h", model: struct{}{}},
		{key: "hexal/stash.c", template: "stash.c", model: struct{}{}},
	}, nil
}

// moduleStashComponent selects hexal/stash.h for a module that constructs or
// operates on any Stash.
func moduleStashComponent(emission *moduleEmission) []string {
	if emission == nil || emission.stashState == nil || !emission.stashState.required {
		return nil
	}
	return []string{"hexal/stash.h"}
}
