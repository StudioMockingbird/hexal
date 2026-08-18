package generator

// errorComponents returns the generated hexal/error.h artifact when Error is
// reachable. The built-in Error object has one canonical fixed
// member set, so the template is static C and the render model is empty.
func errorComponents(merged *programEmission) ([]componentArtifact, error) {
	if !merged.errorUsed {
		return nil, nil
	}
	return []componentArtifact{{key: "hexal/error.h", template: "error.h", model: struct{}{}}}, nil
}

// moduleErrorComponent selects hexal/error.h for a module whose signatures
// or unions name Error.
func moduleErrorComponent(emission *moduleEmission) []string {
	if !emission.errorUsed {
		return nil
	}
	return []string{"hexal/error.h"}
}
