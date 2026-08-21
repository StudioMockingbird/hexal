package generator

// printComponents returns the generated hexal/print.h and hexal/print.c
// artifacts when print is reachable. The core print primitives live here
// with external linkage; nested aggregate helpers stay module-owned.
func printComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || !merged.printUsed {
		return nil, nil
	}
	model := struct{}{}
	return []componentArtifact{
		{key: "hexal/print.h", template: "print.h", model: model},
		{key: "hexal/print.c", template: "print.c", model: model},
	}, nil
}

// modulePrintComponent selects hexal/print.h for a module using print.
func modulePrintComponent(emission *moduleEmission) []string {
	if emission == nil || emission.printState == nil || !emission.printState.used {
		return nil
	}
	return []string{"hexal/print.h"}
}
