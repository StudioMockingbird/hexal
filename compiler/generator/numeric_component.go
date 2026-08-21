package generator

import (
	"slices"
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

// numericComponentModel is the typed render model for packages/numeric.h:
// pre-rendered C helper bodies for every checked conversion, guarded division,
// guarded shift, bitcast, and endian conversion the program needs. All helpers
// are static inline; the template emits them verbatim.
type numericComponentModel struct {
	Conversions []numericHelperRecord
	Divisions   []numericHelperRecord
	Shifts      []numericHelperRecord
	BitCasts    []numericHelperRecord
	Endians     []numericHelperRecord
	NeedArray   bool
}

// numericHelperRecord carries a pre-rendered static inline C helper body.
type numericHelperRecord struct {
	Body string
}

// numericComponents returns the generated hexal/numeric.h artifact when the
// program needs any conversion, division, shift, bitcast, or endian helper.
func numericComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil {
		return nil, nil
	}
	model := buildNumericModel(merged)
	if len(model.Conversions) == 0 && len(model.Divisions) == 0 &&
		len(model.Shifts) == 0 && len(model.BitCasts) == 0 && len(model.Endians) == 0 {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/numeric.h",
		template: "numeric.h",
		model:    model,
	}}, nil
}

// buildNumericModel pre-renders every helper body from the program-wide merged
// specs using the existing body builders.
func buildNumericModel(merged *programEmission) numericComponentModel {
	model := numericComponentModel{}
	for _, spec := range merged.conversionSpecs {
		var buf strings.Builder
		writeConversionHelper(&buf, spec)
		model.Conversions = append(model.Conversions, numericHelperRecord{Body: buf.String()})
	}
	for _, typ := range merged.divisionTypes {
		for _, suffix := range []string{"div", "rem"} {
			op := checker.DivideOperator
			if suffix == "rem" {
				op = checker.RemainderOperator
			}
			var buf strings.Builder
			writeDivisionHelper(&buf, typ, op, suffix)
			model.Divisions = append(model.Divisions, numericHelperRecord{Body: buf.String()})
		}
	}
	for _, spec := range merged.shiftSpecs {
		var buf strings.Builder
		writeShiftHelper(&buf, spec)
		model.Shifts = append(model.Shifts, numericHelperRecord{Body: buf.String()})
	}
	for _, spec := range merged.bitCastSpecs {
		var buf strings.Builder
		writeBitCastDefinitions(&buf, []bitCastSpec{spec})
		model.BitCasts = append(model.BitCasts, numericHelperRecord{Body: buf.String()})
	}
	for _, spec := range merged.endianSpecs {
		var buf strings.Builder
		writeEndianHelper(&buf, spec)
		model.Endians = append(model.Endians, numericHelperRecord{Body: buf.String()})
		if needsEndianArray(spec) {
			model.NeedArray = true
		}
	}
	return model
}

// needsEndianArray reports whether the endian helper's to_bytes variant
// returns an Array<UInt8, N>.
func needsEndianArray(spec endianSpec) bool {
	return !spec.from
}

// moduleNumericComponent selects hexal/numeric.h for a module using any
// numeric helper.
func moduleNumericComponent(emission *moduleEmission) []string {
	if !moduleUsesNumeric(emission) {
		return nil
	}
	return []string{"hexal/numeric.h"}
}

// moduleUsesNumeric reports whether the module needs any program-level numeric
// helper: checked conversions, guarded division/remainder, guarded shifts,
// bitcasts, or endian conversions. Direct-only and identity conversion programs
// select no numeric component.
func moduleUsesNumeric(emission *moduleEmission) bool {
	if emission == nil {
		return false
	}
	return len(emission.conversionSpecs) > 0 ||
		len(emission.divisionTypes) > 0 ||
		len(emission.shiftSpecs) > 0 ||
		len(emission.bitCastSpecs) > 0 ||
		len(emission.endianSpecs) > 0
}

// mergeNumericSpecs folds one module's numeric helper specs into the
// program-wide aggregate, deduplicated by identity.
func mergeNumericSpecs(merged *programEmission, module *moduleEmission) {
	conversionSeen := make(map[string]bool, len(merged.conversionSpecs))
	for _, spec := range merged.conversionSpecs {
		conversionSeen[numericConversionKey(spec)] = true
	}
	for _, spec := range module.conversionSpecs {
		key := numericConversionKey(spec)
		if !conversionSeen[key] {
			conversionSeen[key] = true
			merged.conversionSpecs = append(merged.conversionSpecs, spec)
		}
	}
	divisionSeen := make(map[string]bool, len(merged.divisionTypes))
	for _, typ := range merged.divisionTypes {
		divisionSeen[canonicalTypeKey(typ)] = true
	}
	for _, typ := range module.divisionTypes {
		key := canonicalTypeKey(typ)
		if !divisionSeen[key] {
			divisionSeen[key] = true
			merged.divisionTypes = append(merged.divisionTypes, typ)
		}
	}
	shiftSeen := make(map[string]bool, len(merged.shiftSpecs))
	for _, spec := range merged.shiftSpecs {
		shiftSeen[numericShiftKey(spec)] = true
	}
	for _, spec := range module.shiftSpecs {
		key := numericShiftKey(spec)
		if !shiftSeen[key] {
			shiftSeen[key] = true
			merged.shiftSpecs = append(merged.shiftSpecs, spec)
		}
	}
	bitCastSeen := make(map[string]bool, len(merged.bitCastSpecs))
	for _, spec := range merged.bitCastSpecs {
		bitCastSeen[numericBitCastKey(spec)] = true
	}
	for _, spec := range module.bitCastSpecs {
		key := numericBitCastKey(spec)
		if !bitCastSeen[key] {
			bitCastSeen[key] = true
			merged.bitCastSpecs = append(merged.bitCastSpecs, spec)
		}
	}
	endianSeen := make(map[string]bool, len(merged.endianSpecs))
	for _, spec := range merged.endianSpecs {
		endianSeen[endianKey(spec)] = true
	}
	for _, spec := range module.endianSpecs {
		key := endianKey(spec)
		if !endianSeen[key] {
			endianSeen[key] = true
			merged.endianSpecs = append(merged.endianSpecs, spec)
		}
	}
}

func endianKey(spec endianSpec) string {
	order := "le"
	if spec.bigEnd {
		order = "be"
	}
	dir := "to"
	if spec.from {
		dir = "from"
	}
	return dir + "_" + order + "_" + canonicalTypeKey(spec.typ)
}

func canonicalTypeKey(typ compilerTypes.Type) string {
	if typ.CanonicalKey != "" {
		return typ.CanonicalKey
	}
	return typ.CName + "|" + typ.Name
}

func numericConversionKey(spec conversionSpec) string {
	return canonicalTypeKey(spec.source) + ">" + canonicalTypeKey(spec.target)
}

func numericShiftKey(spec shiftSpec) string {
	return spec.operator.String() + ">" + canonicalTypeKey(spec.typ)
}

func numericBitCastKey(spec bitCastSpec) string {
	return canonicalTypeKey(spec.source) + ">" + canonicalTypeKey(spec.target)
}

// sortMergedNumericSpecs gives every helper family canonical-key ordering
// after module demands have been merged, independent of traversal order.
func sortMergedNumericSpecs(merged *programEmission) {
	slices.SortStableFunc(merged.conversionSpecs, func(left, right conversionSpec) int {
		return strings.Compare(numericConversionKey(left), numericConversionKey(right))
	})
	slices.SortStableFunc(merged.divisionTypes, func(left, right compilerTypes.Type) int {
		return strings.Compare(canonicalTypeKey(left), canonicalTypeKey(right))
	})
	slices.SortStableFunc(merged.shiftSpecs, func(left, right shiftSpec) int {
		return strings.Compare(numericShiftKey(left), numericShiftKey(right))
	})
	slices.SortStableFunc(merged.bitCastSpecs, func(left, right bitCastSpec) int {
		return strings.Compare(numericBitCastKey(left), numericBitCastKey(right))
	})
	slices.SortStableFunc(merged.endianSpecs, func(left, right endianSpec) int {
		return strings.Compare(endianKey(left), endianKey(right))
	})
}
