package generator

import (
	compilerTypes "hexal/compiler/types"
)

// poolComponentModel is the typed render model for packages/pool.h: one
// pre-sorted record per reachable Pool specialization.
type poolComponentModel struct {
	Pools []poolComponentRecord
}

// poolComponentRecord is one reachable Pool specialization's spelling
// facts: the struct C name, the accessor suffix, and the spelled element
// type. The template lays out the struct and the typed inline operations
// from these fields; canonical naming, ordering, and C spelling stay Go
// decisions.
type poolComponentRecord struct {
	CName           string
	Suffix          string
	ElementSpelling string
}

// poolComponentRecordFor builds the spelling record of one Pool
// specialization.
func poolComponentRecordFor(pool compilerTypes.Type) poolComponentRecord {
	return poolComponentRecord{
		CName:           pool.CName,
		Suffix:          poolSuffix(pool),
		ElementSpelling: typeSpelling(pool.Pool.Element),
	}
}

// poolComponents returns the generated hexal/pool.h artifact when builtin-
// element Pool specializations are reachable; module-owned element
// specializations emit into the consuming module headers instead.
func poolComponents(merged *programEmission) ([]componentArtifact, error) {
	if merged == nil || merged.poolState == nil || len(merged.poolState.order) == 0 {
		return nil, nil
	}
	records := make([]poolComponentRecord, 0, len(merged.poolState.order))
	for _, pool := range merged.poolState.order {
		if collectionElementModuleTyped(pool) {
			continue
		}
		records = append(records, poolComponentRecordFor(pool))
	}
	if len(records) == 0 {
		return nil, nil
	}
	return []componentArtifact{{
		key:      "hexal/pool.h",
		template: "pool.h",
		model:    poolComponentModel{Pools: records},
	}}, nil
}

// modulePoolComponent selects hexal/pool.h for a module with reachable
// builtin-element Pool specializations; a module whose only pools are
// module-owned re-emits them in its own header and includes nothing.
func modulePoolComponent(emission *moduleEmission) []string {
	if emission == nil || emission.poolState == nil || len(emission.poolState.order) == 0 {
		return nil
	}
	for _, pool := range emission.poolState.order {
		if !collectionElementModuleTyped(pool) {
			return []string{"hexal/pool.h"}
		}
	}
	return nil
}
