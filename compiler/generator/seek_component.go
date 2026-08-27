package generator

// seekComponents returns the generated hexal/seek.h artifact when Bytes.seek
// or IO.seek is reachable. Seek is a fixed, three-variant built-in ADT with
// no module ownership, so unlike a user-declared ADT its struct belongs in
// one shared header rather than repeated per module -- generating it inline
// per module (as any other reachable ADT is) placed the struct after
// hexal/equality.h's own #include of it, since equality.h is always
// included before a module's own ADT definitions.
func seekComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || !merged.seekUsed {
		return nil, nil
	}
	return []componentArtifact{{key: "hexal/seek.h", template: "seek.h", model: struct{}{}}}, nil
}

// moduleSeekComponent selects hexal/seek.h for a module that reaches
// Bytes.seek or IO.seek.
func moduleSeekComponent(emission *moduleEmission) []string {
	if emission == nil || emission.ioState == nil || !(emission.ioState.seekIO || emission.ioState.seekBytes) {
		return nil
	}
	return []string{"hexal/seek.h"}
}
