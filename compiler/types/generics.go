package types

import "slices"

// GenericDeclaration is the compilation-scoped identity of one generic type,
// alias, function, or method declaration. Names are unique within a
// compilation, so the name is the stable identity used by specialization keys
// and generated C names.
type GenericDeclaration struct {
	Name         string
	Arity        int
	Parameters   []string
	placeholders []Type // lazily filled placeholder types, index-aligned
}

// DeclareGeneric registers a generic declaration. A redeclaration with the
// same arity and parameter names reuses the existing record; an incompatible
// redeclaration returns nil so the checker can diagnose it. Arity zero is a
// degenerate generic with no own parameters, used by methods that inherit
// only the receiver's parameters.
func (environment *Environment) DeclareGeneric(name string, arity int, parameters []string) *GenericDeclaration {
	if environment == nil || arity < 0 || len(parameters) != arity {
		return nil
	}
	if existing, ok := environment.genericDeclarations[name]; ok {
		if existing.Arity == arity && slices.Equal(existing.Parameters, parameters) {
			return existing
		}
		return nil
	}
	declaration := &GenericDeclaration{
		Name:       name,
		Arity:      arity,
		Parameters: append([]string(nil), parameters...),
	}
	environment.genericDeclarations[name] = declaration
	return declaration
}

// TypeParameter returns the cached placeholder type for one parameter
// position. Placeholders are incomplete so they can never be complete values;
// they are canonical only behind a pointer layer during open generic body
// checking and never reach generation unresolved.
func (environment *Environment) TypeParameter(declaration *GenericDeclaration, index int) Type {
	if environment == nil || declaration == nil || index < 0 || index >= declaration.Arity {
		return Type{}
	}
	if declaration.placeholders == nil {
		declaration.placeholders = make([]Type, declaration.Arity)
	}
	if declaration.placeholders[index].Generic == nil {
		declaration.placeholders[index] = Type{
			Name:         declaration.Parameters[index],
			CanonicalKey: declaration.Parameters[index],
			Incomplete:   true,
			Generic:      declaration,
			GenericIndex: index,
			identity:     newTypeIdentity(environment.identity),
		}
	}
	return declaration.placeholders[index]
}

// ContainsTypeParameter reports whether typ contains a generic parameter
// placeholder anywhere in its structure.
func ContainsTypeParameter(typ Type) bool {
	return containsTypeParameter(typ, make(map[*typeIdentity]bool))
}

func containsTypeParameter(typ Type, seenObjects map[*typeIdentity]bool) bool {
	if typ.Generic != nil {
		return true
	}
	if typ.Union != nil {
		for _, member := range typ.Union.Members {
			if containsTypeParameter(member, seenObjects) {
				return true
			}
		}
	}
	if typ.NullableBase != nil {
		return containsTypeParameter(*typ.NullableBase, seenObjects)
	}
	if typ.Element != nil {
		return containsTypeParameter(*typ.Element, seenObjects)
	}
	if typ.View != nil {
		return containsTypeParameter(typ.View.Element, seenObjects)
	}
	if typ.List != nil {
		return containsTypeParameter(typ.List.Element, seenObjects)
	}
	if typ.Dict != nil {
		if containsTypeParameter(typ.Dict.Key, seenObjects) || containsTypeParameter(typ.Dict.Value, seenObjects) {
			return true
		}
	}
	if typ.Signature != nil {
		for _, parameter := range typ.Signature.Parameters {
			if containsTypeParameter(parameter, seenObjects) {
				return true
			}
		}
		if typ.Signature.Result != nil {
			return containsTypeParameter(*typ.Signature.Result, seenObjects)
		}
	}
	if typ.Object != nil {
		if seenObjects[typ.Object.identity] {
			return false
		}
		seenObjects[typ.Object.identity] = true
		for _, member := range typ.Object.Members {
			if containsTypeParameter(member.Type, seenObjects) {
				return true
			}
		}
	}
	if typ.Adt != nil {
		if seenObjects[typ.Adt.identity] {
			return false
		}
		seenObjects[typ.Adt.identity] = true
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if containsTypeParameter(member.Type, seenObjects) {
					return true
				}
			}
		}
	}
	return false
}

// SanitizeIdentifier replaces every character that is not an ASCII letter,
// digit, or underscore with an underscore, so a display name such as
// "Box<Int32>" becomes the C-identifier-safe "Box_Int32". Ordinary source
// names are already identifier-safe and pass through unchanged.
func SanitizeIdentifier(name string) string {
	result := []byte(name)
	for index := 0; index < len(result); index++ {
		character := result[index]
		if !(character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9' ||
			character == '_') {
			result[index] = '_'
		}
	}
	return string(result)
}
