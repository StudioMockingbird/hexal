package types

import "strconv"

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
		if existing.Arity == arity && equalStrings(existing.Parameters, parameters) {
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

// LookupGeneric resolves a registered generic declaration by name.
func (environment *Environment) LookupGeneric(name string) (*GenericDeclaration, bool) {
	if environment == nil {
		return nil, false
	}
	declaration, ok := environment.genericDeclarations[name]
	return declaration, ok
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

// Substitute replaces every placeholder of the bindings' declaration with its
// argument by rebuilding the type through the environment's interners, so the
// result is always canonical. An out-of-range placeholder or an unresolvable
// nested type yields the zero Type, which callers treat as failure.
func (environment *Environment) Substitute(typ Type, bindings []Type) Type {
	if typ.Generic != nil {
		if typ.GenericIndex < 0 || typ.GenericIndex >= len(bindings) {
			return Type{}
		}
		return bindings[typ.GenericIndex]
	}
	if typ.Union != nil {
		members := make([]Type, 0, len(typ.Union.Members))
		for _, member := range typ.Union.Members {
			substituted := environment.Substitute(member, bindings)
			if substituted == (Type{}) {
				return Type{}
			}
			members = append(members, substituted)
		}
		return environment.UnionType(members)
	}
	if typ.NullableBase != nil {
		substituted := environment.Substitute(*typ.NullableBase, bindings)
		if substituted == (Type{}) {
			return Type{}
		}
		return environment.NullableType(substituted)
	}
	if typ.Element != nil {
		substituted := environment.Substitute(*typ.Element, bindings)
		if substituted == (Type{}) {
			return Type{}
		}
		if typ.PointeeWritable {
			return environment.MutPtrType(substituted)
		}
		return environment.PtrType(substituted)
	}
	if typ.View != nil {
		substituted := environment.Substitute(typ.View.Element, bindings)
		if substituted == (Type{}) {
			return Type{}
		}
		return environment.ViewType(substituted)
	}
	if typ.List != nil {
		substituted := environment.Substitute(typ.List.Element, bindings)
		if substituted == (Type{}) {
			return Type{}
		}
		return environment.ListType(substituted)
	}
	if typ.Dict != nil {
		key := environment.Substitute(typ.Dict.Key, bindings)
		if key == (Type{}) {
			return Type{}
		}
		value := environment.Substitute(typ.Dict.Value, bindings)
		if value == (Type{}) {
			return Type{}
		}
		return environment.DictType(key, value)
	}
	if typ.Signature != nil {
		parameters := make([]Type, 0, len(typ.Signature.Parameters))
		for _, parameter := range typ.Signature.Parameters {
			substituted := environment.Substitute(parameter, bindings)
			if substituted == (Type{}) {
				return Type{}
			}
			parameters = append(parameters, substituted)
		}
		var result *Type
		if typ.Signature.Result != nil {
			substituted := environment.Substitute(*typ.Signature.Result, bindings)
			if substituted == (Type{}) {
				return Type{}
			}
			result = &substituted
		}
		return environment.FunType(parameters, result)
	}
	if typ.Object != nil {
		// Open object templates are specialized by the checker with a
		// parameter frame, never by substitution; a placeholder inside a
		// nominal object here is a fail-closed error.
		for _, member := range typ.Object.Members {
			if ContainsTypeParameter(member.Type) {
				return Type{}
			}
		}
		return typ
	}
	return typ
}

// Specialize substitutes the arguments into a template type and interns the
// concrete result keyed by the declaration and the ordered argument serials.
// Repeated requests reuse one canonical type.
func (environment *Environment) Specialize(declaration *GenericDeclaration, arguments []Type, template Type) Type {
	if environment == nil || declaration == nil || len(arguments) != declaration.Arity {
		return Type{}
	}
	key := declaration.Name + "|" + typeSerialKey(arguments)
	if cached, ok := environment.specializations[key]; ok {
		return cached
	}
	specialized := environment.Substitute(template, arguments)
	if specialized == (Type{}) {
		return Type{}
	}
	environment.specializations[key] = specialized
	return specialized
}

func typeSerialKey(types []Type) string {
	key := ""
	for _, typ := range types {
		if typ.identity == nil {
			return ""
		}
		key += strconv.FormatUint(typ.identity.serial, 10) + ","
	}
	return key
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
