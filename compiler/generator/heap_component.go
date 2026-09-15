package generator

// heapHeaderModel is the render model for packages/heap.h: the hex_heap token
// and the default allocation operation declarations are static
// machine-width-independent C, so the only per-program field is Aligned, the
// demand bit for the aligned primitive. A program that never asks for an
// alignment does not inherit its declaration.
type heapHeaderModel struct{ Aligned bool }

// heapSourceModel is the render model for packages/heap.c: the allocation and
// release bodies are static C, with the same aligned-allocation demand bit as
// the header.
type heapSourceModel struct{ Aligned bool }

// heapComponents returns the generated hexal/heap.h and hexal/heap.c
// artifacts when Heap machinery is selected. The representation and the
// allocation runtime live here; typed allocation helpers remain module-owned.
func heapComponents(merged *programEmission) ([]componentArtifact, error) {
	state := merged.heapState
	if !state.selected() {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/heap.h", template: "heap.h", model: heapHeaderModel{Aligned: len(state.alignedElements) > 0}},
		{key: "hexal/heap.c", template: "heap.c", model: heapSourceModel{Aligned: len(state.alignedElements) > 0}},
	}, nil
}

// moduleHeapComponent selects hexal/heap.h for a module whose generated
// machinery allocates or spells hex_heap.
func moduleHeapComponent(emission *moduleEmission) []string {
	if emission.heapState.selected() {
		return []string{"hexal/heap.h"}
	}
	return nil
}
