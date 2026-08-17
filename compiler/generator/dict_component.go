package generator

import (
	compilerTypes "hexal/compiler/types"
)

// dictComponentModel is the typed render model for packages/dict.h: one
// pre-sorted record per reachable Dict specialization.
type dictComponentModel struct {
	Dicts []dictComponentRecord
}

// dictComponentRecord is one reachable Dict specialization's spelling and
// selection facts: the struct C names, the accessor suffix, the spelled key
// and value types, the once-per-header hash helper name, whether the key is
// Strand (driving the memcmp probe and the FNV-1a hash body), and whether
// this specialization is the first of its key kind and therefore emits that
// helper. The template lays out the structs and the typed inline operations
// from these fields; canonical naming, ordering, and C spelling stay Go
// decisions.
type dictComponentRecord struct {
	CName         string
	Suffix        string
	EntryName     string
	KeySpelling   string
	ValueSpelling string
	HashHelper    string
	StrandKey     bool
	EmitHash      bool
}

// dictComponents returns the generated hexal/dict.h artifact when Dict
// specializations are reachable.
func dictComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.dictState == nil || len(merged.dictState.order) == 0 {
		return nil, nil
	}
	records := make([]dictComponentRecord, 0, len(merged.dictState.order))
	hashEmitted := make(map[string]bool)
	for _, dict := range merged.dictState.order {
		key := dict.Dict.Key
		strandKey := compilerTypes.IsStrand(key)
		hashHelper := "hex_hash_Int32"
		if strandKey {
			hashHelper = "hex_hash_Strand"
		}
		suffix := dictSuffix(dict)
		records = append(records, dictComponentRecord{
			CName:         dict.CName,
			Suffix:        suffix,
			EntryName:     "hex_dict_entry_" + suffix,
			KeySpelling:   typeSpelling(key),
			ValueSpelling: typeSpelling(dict.Dict.Value),
			HashHelper:    hashHelper,
			StrandKey:     strandKey,
			EmitHash:      !hashEmitted[hashHelper],
		})
		hashEmitted[hashHelper] = true
	}
	return []componentArtifact{{
		key:      "hexal/dict.h",
		template: "dict.h",
		model:    dictComponentModel{Dicts: records},
	}}, nil
}

// dictFamilyContent is the drained transition seam: the Dict machinery owns
// the component artifact, so hexal.h contributes nothing.
func dictFamilyContent(state *generatedDictState) string {
	return ""
}

// moduleDictComponent selects hexal/dict.h for a module with reachable Dict
// specializations.
func moduleDictComponent(emission *moduleEmission) []string {
	if emission == nil || emission.dictState == nil || len(emission.dictState.order) == 0 {
		return nil
	}
	return []string{"hexal/dict.h"}
}
