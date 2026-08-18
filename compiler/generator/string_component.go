package generator

// stringComponents returns the generated hexal/string.h and hexal/string.c
// artifacts when String or Strand is reachable. The types,
// literal declarations, UTF-8 helpers, and String operations migrate here
// from hexal.h; literal storage and non-specialized bodies own the C file.
func stringComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.stringState == nil || !merged.stringState.used {
		return nil, nil
	}
	model := stringRenderModel{
		NeedStrand: merged.stringState.strand,
		Literals:   buildStringLiteralModels(merged.stringState.All()),
	}
	return []componentArtifact{
		{key: "hexal/string.h", template: "string.h", model: model},
		{key: "hexal/string.c", template: "string.c", model: model},
	}, nil
}

// moduleStringComponent selects hexal/string.h for a module using String or
// Strand.
func moduleStringComponent(emission *moduleEmission) []string {
	if emission == nil || !emission.stringUsed {
		return nil
	}
	return []string{"hexal/string.h"}
}

// stringLiteralModel is one emitted String literal object pair: the object
// base name, the payload bytes the source defines, and both lengths the
// templates spell. The byte array carries a trailing zero, so ArraySize is
// one past PayloadLength.
type stringLiteralModel struct {
	Name          string
	Payload       []uint8
	ArraySize     int
	PayloadLength int
}

// stringRenderModel is the render model shared by the string.h and string.c
// templates: the Strand requirement and the canonical program-wide literal
// records in first-use order.
type stringRenderModel struct {
	NeedStrand bool
	Literals   []stringLiteralModel
}

// buildStringLiteralModels converts the program-wide literal payloads into
// template records, preserving the canonical ordering.
func buildStringLiteralModels(payloads []string) []stringLiteralModel {
	literals := make([]stringLiteralModel, 0, len(payloads))
	for index, payload := range payloads {
		literals = append(literals, stringLiteralModel{
			Name:          stringLiteralCName(index),
			Payload:       []byte(payload),
			ArraySize:     len(payload) + 1,
			PayloadLength: len(payload),
		})
	}
	return literals
}
