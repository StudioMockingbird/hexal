package generator

// eventComponents emits the private libuv adapter only for a Task program
// with a native operation that can suspend the current Task.
func eventComponents(merged *programEmission) ([]componentArtifact, error) {
	if !eventSelected(merged) {
		return nil, nil
	}
	model := eventSourceModel{
		Sleep: merged.timeState != nil && merged.timeState.sleep,
		File:  merged.fileState != nil && merged.fileState.used,
	}
	return []componentArtifact{
		{key: "hexal/event.h", template: "event.h", model: model},
		{key: "hexal/event.c", template: "event.c", model: model},
	}, nil
}

// eventSourceModel gates the Task sleep timer command and the component
// command API File requests submit through.
type eventSourceModel struct {
	Sleep bool
	File  bool
}
