package generator

// heapHeaderModel is the render model for packages/heap.h: the hex_heap
// handle, its default initializer, the allocation-header layout, and the raw
// operation declarations are static machine-width-independent C, so the model
// carries no per-program fields and the template owns all C text.
type heapHeaderModel struct{}

// heapSourceModel is the render model for packages/heap.c: the raw
// allocation and release bodies are static C with no per-program fields.
type heapSourceModel struct{}

// heapComponents returns the generated hexal/heap.h and hexal/heap.c
// artifacts when Heap machinery is selected. The representation and raw
// allocation runtime migrate here from hexal.h; typed allocation helpers
// remain module-owned.
func heapComponents(merged *programEmission) ([]componentArtifact, error) {
	state := merged.heapState
	if state == nil || (!state.required && len(state.elements) == 0) {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/heap.h", template: "heap.h", model: heapHeaderModel{}},
		{key: "hexal/heap.c", template: "heap.c", model: heapSourceModel{}},
	}, nil
}

// moduleHeapComponent selects hexal/heap.h for a module whose generated
// machinery allocates or spells hex_heap.
func moduleHeapComponent(emission *moduleEmission) []string {
	state := emission.heapState
	if state != nil && (state.required || len(state.elements) > 0) {
		return []string{"hexal/heap.h"}
	}
	return nil
}
