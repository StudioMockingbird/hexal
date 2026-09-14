package generator

// printComponents returns the generated hexal/print.h and hexal/print.c
// artifacts when print is reachable. The core print primitives live here
// with external linkage; nested aggregate helpers stay module-owned.
func printComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || !merged.printUsed {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/print.h", template: "print.h", model: struct{}{}},
		{key: "hexal/print.c", template: "print.c", model: printSourceModel{Event: eventSelected(merged)}},
	}, nil
}

// printSourceModel gates hexal/print.c's Task-aware wrapping of its Windows
// attached-console write: outside a Task the entry runs directly, inside one
// it runs through the existing worker bridge, exactly like hex_io_write_all.
type printSourceModel struct {
	Event bool
}

// modulePrintComponent selects hexal/print.h for a module using print.
func modulePrintComponent(emission *moduleEmission) []string {
	if emission == nil || emission.printState == nil || !emission.printState.used {
		return nil
	}
	return []string{"hexal/print.h"}
}
