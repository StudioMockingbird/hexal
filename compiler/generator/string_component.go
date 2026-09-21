package generator

// stringComponents returns the generated hexal/string.h and hexal/string.c
// artifacts when any text type is reachable. The types, literal declarations,
// inline capacity structs, UTF-8 validation, and text operations live here;
// literal storage and non-specialized bodies own the C file.
func stringComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.stringState == nil || !merged.stringState.used {
		return nil, nil
	}
	model := stringRenderModel{
		Inline:            buildInlineStringModels(merged.stringState),
		NeedEquality:      merged.equalityNeed,
		NeedOrdering:      merged.orderingNeed,
		NeedHash:          merged.hashNeed,
		NeedInterpolation: merged.interpolationNeed,
		Literals:          buildStringLiteralModels(merged.stringState.All()),
	}
	return []componentArtifact{
		{key: "hexal/string.h", template: "string.h", model: model},
		{key: "hexal/string.c", template: "string.c", model: model},
	}, nil
}

// moduleStringComponent selects hexal/string.h for a module using any text
// type.
func moduleStringComponent(emission *moduleEmission) []string {
	if emission == nil || !emission.stringUsed {
		return nil
	}
	return []string{"hexal/string.h"}
}

// stringLiteralModel is one emitted String literal object pair: the object
// base name, the payload bytes the source defines, and the length the
// templates spell. The byte array carries a trailing zero, so ArraySize is one
// past PayloadLength.
type stringLiteralModel struct {
	Name          string
	Payload       []uint8
	ArraySize     int
	PayloadLength int
}

// inlineStringModel is one emitted String<N> struct: its C name and capacity.
type inlineStringModel struct {
	CName    string
	Capacity uint64
}

// stringRenderModel is the render model shared by the string.h and string.c
// templates: the inline capacity structs, the demand-selected helpers, and the
// canonical program-wide literal records in first-use order.
type stringRenderModel struct {
	Inline            []inlineStringModel
	NeedEquality      bool
	NeedOrdering      bool
	NeedHash          bool
	NeedInterpolation bool
	Literals          []stringLiteralModel
}

// buildInlineStringModels lists the demanded capacities in ascending order, so
// the emitted header is deterministic.
func buildInlineStringModels(registry *literalRegistry) []inlineStringModel {
	capacities := registry.inlineCapacities()
	models := make([]inlineStringModel, 0, len(capacities))
	for _, capacity := range capacities {
		models = append(models, inlineStringModel{CName: registry.inline[capacity], Capacity: capacity})
	}
	return models
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
