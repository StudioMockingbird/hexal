package types

// AdtVariant is one variant of a nominal ADT. Payload is empty for a unit
// variant; payload fields are immutable.
type AdtVariant struct {
	Name    string
	Payload []ObjectMember
}

// AdtType is the compilation-owned nominal record behind an ADT Type.
type AdtType struct {
	Name         string
	CName        string
	Variants     []AdtVariant
	SourceLine   int
	SourceColumn int
	// ModuleID is the canonical identity of the module that declared the
	// ADT; it is empty for compiler-owned builtins. The checker stamps it
	// on every ADT it creates in a module scope.
	// Owner caches EncodeModuleOwner(ModuleID): the encoded spelling that
	// generated C names embed. Write it only through SetModuleOwner so the
	// cached spelling can never disagree with ModuleID.
	ModuleID string
	Owner    string
	identity *typeIdentity
}

// SetModuleOwner stamps the declaring module's canonical id and its derived
// encoded owner as one operation, so generation can read the spelling without
// re-encoding it at every derivation site.
func (adt *AdtType) SetModuleOwner(moduleID string) {
	adt.ModuleID = moduleID
	adt.Owner = EncodeModuleOwner(moduleID)
}

// IsADT reports whether typ is a nominal ADT type.
func IsADT(typ Type) bool { return typ.Adt != nil }

// BeginADT publishes a provisional nominal ADT identity and binds the source
// name before variants are resolved, so payload fields may reach the ADT
// behind a pointer layer. The ADT is stamped with the declaring module's
// canonical id, like the object family, so the C name is owner-qualified and
// two ADTs named alike in different modules never collide; the name is
// reserved as definition-keying.
func (environment *Environment) BeginADT(name string, sourceLine, sourceColumn int) Type {
	if environment == nil {
		return Type{}
	}
	identity := newTypeIdentity()
	cName := "hex_t_" + SanitizeIdentifier(name)
	if environment.owner != "" {
		cName = "hex_t_" + environment.owner + "_" + SanitizeIdentifier(name)
	}
	adt := &AdtType{
		Name:         name,
		CName:        cName,
		SourceLine:   sourceLine,
		SourceColumn: sourceColumn,
		identity:     identity,
	}
	adt.SetModuleOwner(environment.moduleID)
	identity.object = nil
	typ := Type{
		Name:         name,
		CName:        adt.CName,
		CanonicalKey: canonicalNominalKey(name, environment.moduleID),
		Adt:          adt,
		identity:     identity,
	}
	environment.arena.ReserveDefinitionName(cName, typ)
	environment.names[name] = typ
	return typ
}

// CompleteADT finalizes a provisional ADT with its resolved variants.
func (environment *Environment) CompleteADT(name string, variants []AdtVariant) Type {
	if environment == nil {
		return Type{}
	}
	typ, ok := environment.names[name]
	if !ok || typ.Adt == nil {
		return Type{}
	}
	typ.Adt.Variants = append([]AdtVariant(nil), variants...)
	return typ
}

// AbandonADT releases a provisional ADT whose variants failed to resolve.
func (environment *Environment) AbandonADT(name string) {
	delete(environment.names, name)
}

// AdtVariant resolves a qualified variant name through the environment.
func (environment *Environment) AdtVariant(name, variant string) (*AdtVariant, bool) {
	if environment == nil {
		return nil, false
	}
	typ, ok := environment.names[name]
	if !ok || typ.Adt == nil {
		return nil, false
	}
	for index := range typ.Adt.Variants {
		if typ.Adt.Variants[index].Name == variant {
			return &typ.Adt.Variants[index], true
		}
	}
	return nil, false
}
