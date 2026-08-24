package generator

import (
	compilerTypes "hexal/compiler/types"
)

// ioComponents returns the generated hexal/io.h and hexal/io.c artifacts when
// a stream family or print is reachable. Print-only programs select the same
// pair because print's byte sink transfers through the descriptor core.
func ioComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.ioState == nil || (!merged.ioState.used && !merged.printUsed) {
		return nil, nil
	}
	return []componentArtifact{
		{key: "hexal/io.h", template: "io.h", model: struct{}{}},
		{key: "hexal/io.c", template: "io.c", model: ioSourceModel{Blocking: blockingSelected(merged)}},
	}, nil
}

// ioSourceModel is the render model for packages/io.c: whether the blocking
// pool is selected, so each native transfer routes through hex_blocking_call
// instead of calling its private synchronous core directly.
type ioSourceModel struct {
	Blocking bool
}

// moduleStreamComponent selects hexal/io.h for a module using streams.
func moduleStreamComponent(emission *moduleEmission) []string {
	if emission == nil || emission.ioState == nil || !emission.ioState.used {
		return nil
	}
	return []string{"hexal/io.h"}
}

// ensureByteList forces the List<Byte> specialization the IO component names.
// The canonical element spells UInt8: Byte is a transparent alias sharing one
// identity and C name, so the stream surface and any source-visible
// List<Byte> render as exactly one specialization.
func ensureByteList(state *generatedListState) {
	if state == nil {
		return
	}
	for _, list := range state.order {
		if list.CName == "hex_list_UInt8" {
			return
		}
	}
	list := compilerTypes.NewEnvironment().ListType(compilerTypes.UInt8)
	state.seen[list.List] = true
	state.order = append(state.order, list)
}
