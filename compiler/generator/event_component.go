package generator

// eventComponents emits the private libuv adapter only for a Task program
// with a native operation that can suspend the current Task.
func eventComponents(merged *programEmission) ([]componentArtifact, error) {
	if !eventSelected(merged) {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/event.h", template: "event.h", model: struct{}{}},
		{key: "hexal/event.c", template: "event.c", model: struct{}{}},
	}, nil
}
