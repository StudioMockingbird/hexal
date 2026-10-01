package types

// NominalOwner is the identity of a nominal struct or union record: the thing a
// method belongs to. The pointer itself is the identity, so two owners are the
// same exactly when the interface values are equal; the accessors read the
// fields the two record kinds share.
type NominalOwner interface {
	NominalName() string
	NominalCName() string
	NominalModuleID() string
	NominalEncodedOwner() string
}

// NominalName returns the declared name of the struct.
func (object *ObjectType) NominalName() string { return object.Name }

// NominalCName returns the C name of the struct.
func (object *ObjectType) NominalCName() string { return object.CName }

// NominalModuleID returns the canonical id of the declaring module.
func (object *ObjectType) NominalModuleID() string { return object.ModuleID }

// NominalEncodedOwner returns the encoded module spelling generated C names embed.
func (object *ObjectType) NominalEncodedOwner() string { return object.Owner }

// NominalName returns the declared name of the union.
func (adt *AdtType) NominalName() string { return adt.Name }

// NominalCName returns the C name of the union.
func (adt *AdtType) NominalCName() string { return adt.CName }

// NominalModuleID returns the canonical id of the declaring module.
func (adt *AdtType) NominalModuleID() string { return adt.ModuleID }

// NominalEncodedOwner returns the encoded module spelling generated C names embed.
func (adt *AdtType) NominalEncodedOwner() string { return adt.Owner }

// NominalOwnerOf returns the nominal owner of typ: its struct or union record,
// or nil for any other type. A nil return is a true nil interface.
func NominalOwnerOf(typ Type) NominalOwner {
	switch {
	case typ.Object != nil:
		return typ.Object
	case typ.Adt != nil:
		return typ.Adt
	}
	return nil
}
