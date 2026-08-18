package types

import "strconv"

// Arena interns every constructed type of one compilation. The checker creates
// one arena and shares it across module environments, so List<Int32> written
// in two modules is one interned type. Module-scoped names, aliases, and
// generic declarations stay on Environment; only the constructed-type family
// maps are compilation-global.
type Arena struct {
	pointerTypes  map[string]Type
	nullableTypes map[string]Type
	funTypes      map[string]Type
	arrayTypes    map[string]Type
	viewTypes     map[string]Type
	listTypes     map[string]Type
	dictTypes     map[string]Type
	taskTypes     map[string]Type
	channelTypes  map[string]Type
	atomicTypes   map[string]Type
	unionTypes    map[string]Type
}

// NewArena returns an empty per-compilation constructed-type arena.
func NewArena() *Arena {
	return &Arena{
		pointerTypes:  make(map[string]Type),
		nullableTypes: make(map[string]Type),
		funTypes:      make(map[string]Type),
		arrayTypes:    make(map[string]Type),
		viewTypes:     make(map[string]Type),
		listTypes:     make(map[string]Type),
		dictTypes:     make(map[string]Type),
		taskTypes:     make(map[string]Type),
		channelTypes:  make(map[string]Type),
		atomicTypes:   make(map[string]Type),
		unionTypes:    make(map[string]Type),
	}
}

// uniqueCollectionCName returns base when no type in family already uses it;
// otherwise it appends the module encoding of the first nominal element, with
// a numeric tail when even that collides. Resolution happens once at
// interning, deterministically in construction order; every derivation site
// reads the stored name, so a typedef and its helper suffixes stay paired.
func uniqueCollectionCName(family map[string]Type, base string, element Type) string {
	if !collectionCNameTaken(family, base) {
		return base
	}
	stem := base
	if module := nominalModuleOf(element); module != "" {
		stem += "_" + EncodeModuleOwner(module)
	}
	candidate := stem
	for counter := 0; collectionCNameTaken(family, candidate); counter++ {
		candidate = stem + "_" + strconv.Itoa(counter)
	}
	return candidate
}

func collectionCNameTaken(family map[string]Type, name string) bool {
	for _, existing := range family {
		if existing.CName == name {
			return true
		}
	}
	return false
}

// nominalModuleOf returns the canonical id of the first nominal (object or
// ADT) type reachable through typ's structure, or "" when typ contains none.
func nominalModuleOf(typ Type) string {
	switch {
	case typ.Object != nil:
		return typ.Object.ModuleID
	case typ.Adt != nil:
		return typ.Adt.ModuleID
	case typ.Element != nil:
		return nominalModuleOf(*typ.Element)
	case typ.NullableBase != nil:
		return nominalModuleOf(*typ.NullableBase)
	case typ.Array != nil:
		return nominalModuleOf(typ.Array.Element)
	case typ.View != nil:
		return nominalModuleOf(typ.View.Element)
	case typ.List != nil:
		return nominalModuleOf(typ.List.Element)
	case typ.Dict != nil:
		if module := nominalModuleOf(typ.Dict.Value); module != "" {
			return module
		}
		return nominalModuleOf(typ.Dict.Key)
	case typ.Task != nil:
		return nominalModuleOf(typ.Task.Result)
	case typ.Channel != nil:
		return nominalModuleOf(typ.Channel.Element)
	case typ.Atomic != nil:
		return nominalModuleOf(typ.Atomic.Element)
	case typ.Union != nil:
		for _, member := range typ.Union.Members {
			if module := nominalModuleOf(member); module != "" {
				return module
			}
		}
	}
	return ""
}
