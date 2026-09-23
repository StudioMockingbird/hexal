package generator

import (
	"fmt"

	compilerTypes "hexal/compiler/types"
)

// errorKindVariantModel is one unit ErrorKind variant's generated tag
// constant and its fixed display header, already a valid C string literal.
type errorKindVariantModel struct {
	Tag           string
	HeaderLiteral string
	HeaderLength  int
}

// errorComponentModel is the render model for packages/error.h: every unit
// ErrorKind variant plus the Other tag, resolved against the program's
// finalized tag registry.
//
// HeaderType and MessageType are the C spellings of the two bounded-text types
// Error fixes: the ErrorKind.Other header and the Error message. They are model
// fields, never template literals, because compiler/types derives each spelling
// from the compiler/config capacity and defines the inline-string struct with
// it. A literal in error.h would keep printing the old name the moment a
// capacity changes, leaving the header's layout silently disagreeing with the
// struct it stores.
type errorComponentModel struct {
	KindVariants []errorKindVariantModel
	OtherTag     string
	HeaderType   string
	MessageType  string
}

// errorKindTag resolves one ErrorKind variant's generated tag constant. The
// nil fallback only serves isolated component-rendering tests that build a
// bare programEmission without running full discovery; a real compilation
// always finalizes merged.tags first.
func errorKindTag(tags *tagRegistry, variant string) string {
	if tags == nil {
		return "hex_tag_ErrorKind_" + variant
	}
	index := compilerTypes.ErrorKindVariantIndex(variant)
	return tags.adtVariantTag(compilerTypes.ErrorKindType.Adt, index)
}

// buildErrorComponentModel resolves every ErrorKind variant's tag and fixed
// header in declaration order.
func buildErrorComponentModel(tags *tagRegistry) errorComponentModel {
	model := errorComponentModel{
		HeaderType:  compilerTypes.ErrorHeaderText.CName,
		MessageType: compilerTypes.ErrorMessageText.CName,
	}
	for _, name := range compilerTypes.ErrorKindVariantNames {
		if name == "Other" {
			model.OtherTag = errorKindTag(tags, name)
			continue
		}
		header, _ := compilerTypes.ErrorKindHeader(name)
		model.KindVariants = append(model.KindVariants, errorKindVariantModel{
			Tag:           errorKindTag(tags, name),
			HeaderLiteral: fmt.Sprintf("%q", header),
			HeaderLength:  len(header),
		})
	}
	return model
}

// errorComponents returns the generated hexal/error.h artifact when Error is
// reachable. Error's own member set is static, but the ErrorKind header
// derivation switch needs the program's finalized tag names.
func errorComponents(merged *programEmission) ([]componentArtifact, error) {
	if !merged.errorUsed {
		return nil, nil
	}
	return []componentArtifact{{key: "hexal/error.h", template: "error.h", model: buildErrorComponentModel(merged.tags)}}, nil
}

// moduleErrorComponent selects hexal/error.h for a module whose signatures
// or unions name Error.
func moduleErrorComponent(emission *moduleEmission) []string {
	if !emission.errorUsed {
		return nil
	}
	return []string{"hexal/error.h"}
}
