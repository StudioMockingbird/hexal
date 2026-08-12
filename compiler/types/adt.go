package types

// AdtVariant is one variant of a nominal ADT. Payload is empty for a unit
// variant. Payload fields are immutable in RFC 0022's nongeneric phase.
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
	identity     *typeIdentity
}

// IsADT reports whether typ is a nominal ADT type.
func IsADT(typ Type) bool { return typ.Adt != nil }

// BeginADT publishes a provisional nominal ADT identity and binds the source
// name before variants are resolved, so payload fields may reach the ADT
// behind a pointer layer.
func (environment *Environment) BeginADT(name string, sourceLine, sourceColumn int) Type {
	if environment == nil {
		return Type{}
	}
	identity := newTypeIdentity(environment.identity)
	adt := &AdtType{
		Name:         name,
		CName:        "hex_" + SanitizeIdentifier(name),
		SourceLine:   sourceLine,
		SourceColumn: sourceColumn,
		identity:     identity,
	}
	identity.object = nil
	typ := Type{
		Name:     name,
		CName:    adt.CName,
		Adt:      adt,
		identity: identity,
	}
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
