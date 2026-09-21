package generator

import (
	"strings"

	compilerTypes "hexal/compiler/types"
)

// dictComponentModel is the typed render model for packages/dict.h: one
// pre-sorted record per reachable Dict specialization.
type dictComponentModel struct {
	Dicts []dictComponentRecord
	// NeedsFile and NeedsNetwork are true when some specialization's value is
	// File or a networking type, whose typedefs hexal/file.h and
	// hexal/network.h own; a component header naming them directly cannot
	// rely on a consuming module's own include order to supply them first.
	NeedsFile    bool
	NeedsNetwork bool
	NeedsProcess bool
	NeedsSignal  bool
}

// dictComponentRecord is one reachable Dict specialization's spelling and
// selection facts: the struct C names, the accessor suffix, the spelled key
// and value types, the once-per-header hash helper name, whether the key is
// text (an inline String<N>, probed and hashed over its logical bytes by the
// shared text helpers), and whether this specialization is the first Int32
// key and therefore emits that key's hash helper. The template lays out the structs and the typed inline operations
// from these fields; canonical naming, ordering, and C spelling stay Go
// decisions.
type dictComponentRecord struct {
	CName         string
	Suffix        string
	EntryName     string
	KeySpelling   string
	ValueSpelling string
	// FindValueSpelling is find's return type: a read-only pointer to the
	// stored value, for the caller to null-check. Appending "const *" to
	// ValueSpelling directly, rather than the template prefixing a literal
	// "const ", is required once ValueSpelling is itself already a pointer
	// (String's "const hex_string *"): a textual "const " prefix there
	// produces "const const hex_string * *", a duplicate qualifier.
	FindValueSpelling string
	TextKey           bool
	EmitHash          bool
	NeedsFile         bool
	NeedsNetwork      bool
	NeedsProcess      bool
	NeedsSignal       bool
}

// dictComponentRecordFor builds the spelling record of one Dict
// specialization. hashEmitted is per rendered header: the first Int32-key
// dict of the header emits the Int32 hash helper. A text key needs none of its
// own: hex_hash_text and hex_equal_text serve every capacity.
func dictComponentRecordFor(dict compilerTypes.Type, hashEmitted map[string]bool) dictComponentRecord {
	key := dict.Dict.Key
	textKey := compilerTypes.IsText(key)
	hashHelper := "hex_hash_Int32"
	suffix := dictSuffix(dict)
	valueSpelling := typeSpelling(dict.Dict.Value)
	findValueSpelling := "const " + valueSpelling
	if strings.HasSuffix(valueSpelling, "*") {
		findValueSpelling = qualifyLastPointer(valueSpelling)
	}
	return dictComponentRecord{
		CName:             dict.CName,
		Suffix:            suffix,
		EntryName:         "hex_dict_entry_" + suffix,
		KeySpelling:       typeSpelling(key),
		ValueSpelling:     valueSpelling,
		FindValueSpelling: findValueSpelling,
		TextKey:           textKey,
		EmitHash:          !textKey && !hashEmitted[hashHelper],
		NeedsFile:         compilerTypes.IsFile(dict.Dict.Value),
		NeedsNetwork:      elementNeedsNetwork(dict.Dict.Value),
		NeedsProcess:      elementNeedsProcess(dict.Dict.Value),
		NeedsSignal:       elementNeedsSignal(dict.Dict.Value),
	}
}

// dictComponents returns the generated hexal/dict.h artifact when builtin-
// value Dict specializations are reachable; module-owned value
// specializations emit into the consuming module headers instead.
func dictComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.dictState == nil || len(merged.dictState.order) == 0 {
		return nil, nil
	}
	records := make([]dictComponentRecord, 0, len(merged.dictState.order))
	hashEmitted := make(map[string]bool)
	for _, dict := range merged.dictState.order {
		if collectionElementModuleTyped(dict) {
			continue
		}
		record := dictComponentRecordFor(dict, hashEmitted)
		if !record.TextKey {
			hashEmitted["hex_hash_Int32"] = true
		}
		records = append(records, record)
	}
	if len(records) == 0 {
		return nil, nil
	}
	needsFile, needsNetwork, needsProcess, needsSignal := false, false, false, false
	for _, record := range records {
		needsFile = needsFile || record.NeedsFile
		needsNetwork = needsNetwork || record.NeedsNetwork
		needsProcess = needsProcess || record.NeedsProcess
		needsSignal = needsSignal || record.NeedsSignal
	}
	return []componentArtifact{{
		key:      "hexal/dict.h",
		template: "dict.h",
		model: dictComponentModel{
			Dicts: records, NeedsFile: needsFile, NeedsNetwork: needsNetwork, NeedsProcess: needsProcess, NeedsSignal: needsSignal,
		},
	}}, nil
}

// moduleDictComponent selects hexal/dict.h for a module with reachable
// builtin-value Dict specializations; a module whose only dicts are
// module-owned re-emits them in its own header and includes nothing.
func moduleDictComponent(emission *moduleEmission) []string {
	if emission == nil || emission.dictState == nil || len(emission.dictState.order) == 0 {
		return nil
	}
	for _, dict := range emission.dictState.order {
		if !collectionElementModuleTyped(dict) {
			return []string{"hexal/dict.h"}
		}
	}
	return nil
}
