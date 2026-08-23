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
	// definitionNames owns cross-family uniqueness for definition-keying
	// generated C names: a name that a typedef introduces a type under. The
	// map resolves a name to the canonical type that owns it, so two distinct
	// Hexal types never share one C definition name. Fixed compiler-owned
	// types, every reachable nominal object/ADT, and every constructed type
	// with a definition reserve through it; union reservations suffix instead
	// of displacing.
	definitionNames map[string]Type
	// collectionCNames is the per-family C-name index for O(1) collision checks;
	// it mirrors the CName values of every collection family to avoid O(n^2) scans.
	collectionCNames map[string]bool
}

// NewArena returns an empty per-compilation constructed-type arena. Builtin
// C spellings are reserved up front: no constructed type may claim one, even
// though their prefixes make a collision unreachable today.
func NewArena() *Arena {
	arena := &Arena{
		pointerTypes:     make(map[string]Type),
		nullableTypes:    make(map[string]Type),
		funTypes:         make(map[string]Type),
		arrayTypes:       make(map[string]Type),
		viewTypes:        make(map[string]Type),
		listTypes:        make(map[string]Type),
		dictTypes:        make(map[string]Type),
		taskTypes:        make(map[string]Type),
		channelTypes:     make(map[string]Type),
		atomicTypes:      make(map[string]Type),
		unionTypes:       make(map[string]Type),
		definitionNames:  make(map[string]Type),
		collectionCNames: make(map[string]bool),
	}
	for _, builtin := range builtinTypes {
		arena.ReserveDefinitionName(builtin.CName, builtin)
	}
	return arena
}

// ReserveDefinitionName claims a fixed definition-keying name for its owning
// canonical type. A nominal reservation always wins: the checker reserves
// every concrete declaration's name before any union is constructed, so a
// union can never claim a nominal name, and re-reservation of the same
// canonical type is idempotent.
func (arena *Arena) ReserveDefinitionName(name string, typ Type) {
	if arena == nil || name == "" {
		return
	}
	arena.definitionNames[name] = typ
}

// ReserveUnionName claims a definition-keying name for a structural union:
// base when free, otherwise base_0, base_1, and so on until one is free.
// Nominal names are reserved first, so a suffix means another distinct type
// genuinely wanted the same base. The same canonical union re-reserves its
// stored name through the Equal check.
func (arena *Arena) ReserveUnionName(base string, typ Type) string {
	if arena == nil || base == "" {
		return base
	}
	candidate := base
	for counter := 0; ; counter++ {
		if existing, ok := arena.definitionNames[candidate]; !ok || Equal(existing, typ) {
			arena.definitionNames[candidate] = typ
			return candidate
		}
		candidate = base + "_" + strconv.Itoa(counter)
	}
}

// uniqueCollectionCName returns base when no constructed collection already
// uses it; otherwise it appends the module encoding of the first nominal
// element, with a numeric tail when even that collides. Resolution happens
// once at interning, deterministically in construction order; every
// derivation site reads the stored name, so a typedef and its helper suffixes
// stay paired. The selected name is reserved in the arena's C-name index
// exactly once, so the probe is O(1) instead of a per-candidate family scan.
func (arena *Arena) uniqueCollectionCName(base string, element Type) string {
	if !arena.collectionCNames[base] {
		arena.collectionCNames[base] = true
		return base
	}
	stem := base
	if module := nominalModuleOf(element); module != "" {
		stem += "_" + EncodeModuleOwner(module)
	}
	candidate := stem
	for counter := 0; arena.collectionCNames[candidate]; counter++ {
		candidate = stem + "_" + strconv.Itoa(counter)
	}
	arena.collectionCNames[candidate] = true
	return candidate
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
