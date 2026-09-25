// type_validation.go owns generated-type validation: the type walker,
// scalar support, declared-object sets, and the layout and volatility
// eligibility gates.
package generator

import (
	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func supportedGeneratedType(typ compilerTypes.Type) bool {
	return validateGeneratedType(typ, &generatedTypeValidation{}, false)
}

type generatedTypeValidation struct {
	activeObjects   map[*compilerTypes.ObjectType]bool
	validObjects    map[*compilerTypes.ObjectType]bool
	declaredObjects map[*compilerTypes.ObjectType]bool
	// arrays is the module's array state, carried here because it is the
	// one per-module channel already threaded into every expression render.
	// Accessor demand is recorded from the render site, which is
	// the only place that knows which accessor a surviving access names:
	// deriving it a second time from the checked tree would be two sources
	// of truth for one fact, and a disagreement would emit generated C
	// naming an undeclared function.
	arrays *generatedArrayState
}

// IsCanonical owns identity and recursive type metadata. This pass keeps only
// generator-specific source-name and declaration checks.
func validateGeneratedType(typ compilerTypes.Type, state *generatedTypeValidation, throughPointer bool) bool {
	if compilerTypes.IsNil(typ) {
		// Nil is not canonical outside union construction, but the checker
		// admits it only where the language allows it (union members, nil
		// operands, narrowed payloads). The generator validates the
		// checker's output, so the singleton passes as-is.
		return true
	}
	if !compilerTypes.IsCanonical(typ) {
		// Unknown is canonical only behind a pointer layer, exactly as the
		// type environment interning rule states: Ptr<Unknown> and
		// Ptr<mut Unknown> are the erased object pointer types.
		if compilerTypes.IsUnknown(typ) {
			return throughPointer
		}
		// A foreign record behind a pointer is canonical when complete and
		// also when opaque: generated C references the C type but never
		// defines it, so an incomplete layout is exactly what an opaque
		// pointer holds.
		if compilerTypes.IsForeignRecord(typ) && throughPointer {
			return true
		}
		return false
	}
	if typ.Signature != nil {
		// A Fun result, including one that is itself a Fun, lowers through
		// standaloneResultSpelling's C23 typeof wrapping; ordinary recursion
		// is enough here.
		if typ.Signature.Result != nil && !validateGeneratedType(*typ.Signature.Result, state, false) {
			return false
		}
		for _, parameter := range typ.Signature.Parameters {
			if !validateGeneratedType(parameter, state, false) {
				return false
			}
		}
		return true
	}
	if typ.Union != nil {
		if len(typ.Union.Members) < 2 || typ.CName == "" {
			return false
		}
		for _, member := range typ.Union.Members {
			if !validateGeneratedType(member, state, false) {
				return false
			}
		}
		return true
	}
	if typ.Element != nil {
		return validateGeneratedType(*typ.Element, state, true)
	}
	if typ.Array != nil {
		return validateGeneratedType(typ.Array.Element, state, false)
	}
	if typ.Slice != nil {
		return validateGeneratedType(typ.Slice.Element, state, false)
	}
	if typ.List != nil {
		return validateGeneratedType(typ.List.Element, state, false)
	}
	if typ.Dict != nil {
		return validateGeneratedType(typ.Dict.Key, state, false) && validateGeneratedType(typ.Dict.Value, state, false)
	}
	if compilerTypes.IsEoS(typ) {
		return true
	}
	if typ.Object == nil {
		return true
	}
	object := typ.Object
	// A foreign record is defined by its C header, not by generated C: it
	// keeps the exact C type spelling and is never in the module's declared
	// object set, so only its Hexal name is validated here. Behind a pointer
	// an opaque record is canonical; by value it must be complete.
	foreign := compilerTypes.IsForeignRecord(typ)
	if foreign {
		if !validSourceName(compilerTypes.SanitizeIdentifier(object.Name)) {
			return false
		}
	} else if object == compilerTypes.ErrorType.Object {
		// The built-in Error object is compiler-owned: its C name is the plain
		// hex_t_Error, never owner-encoded, even though its ModuleID is empty.
		if state.declaredObjects != nil && !state.declaredObjects[object] {
			return false
		}
	} else {
		expectedCName := privateCName(typeName, compilerTypes.SanitizeIdentifier(object.Name), object.Owner)
		if state.declaredObjects != nil && !state.declaredObjects[object] || !validSourceName(compilerTypes.SanitizeIdentifier(object.Name)) || object.CName != expectedCName {
			return false
		}
	}
	if state.activeObjects == nil {
		state.activeObjects = make(map[*compilerTypes.ObjectType]bool)
		state.validObjects = make(map[*compilerTypes.ObjectType]bool)
	}
	if state.validObjects[object] {
		return true
	}
	if state.activeObjects[object] {
		return throughPointer
	}
	if object.Incomplete {
		// A provisional object that never reached CompleteObject is a
		// checker defect reaching the generator; a deliberately empty
		// struct is complete with a zero-length member slice and passes. An
		// opaque foreign record is the one deliberate incomplete object, and
		// only a pointer to it is valid.
		return foreign && throughPointer
	}
	state.activeObjects[object] = true
	seenNames := make(map[string]bool, len(object.Members))
	for _, member := range object.Members {
		if !validSourceName(member.Name) || seenNames[member.Name] || !validateGeneratedType(member.Type, state, false) {
			delete(state.activeObjects, object)
			return false
		}
		seenNames[member.Name] = true
	}
	delete(state.activeObjects, object)
	state.validObjects[object] = true
	return true
}

func supportedGeneratedScalarType(typ compilerTypes.Type) bool {
	return typ.Element == nil && typ.Object == nil && typ.Signature == nil && compilerTypes.IsCanonical(typ)
}

type generatedPlace struct {
	typ         compilerTypes.Type
	addressable bool
	writable    bool
}

func declaredObjects(program checker.Program) map[*compilerTypes.ObjectType]bool {
	objects := make(map[*compilerTypes.ObjectType]bool)
	for _, declaration := range program.TypeDeclarations {
		if declaration.Type.Object != nil {
			objects[declaration.Type.Object] = true
		}
	}
	return objects
}

// errorDeclaredObjects augments the declared object table with the built-in
// Error object when the program references it.
func errorDeclaredObjects(program checker.Program) map[*compilerTypes.ObjectType]bool {
	objects := declaredObjects(program)
	// Imported object types are reachable through the module's statements
	// and must validate like local ones; the header emission carries their
	// definitions, so the validator admits them too.
	definitions, err := objectDefinitions(program)
	if err == nil {
		for _, object := range definitions {
			objects[object] = true
		}
	}
	if discoverErrorUsed(program) {
		objects[compilerTypes.ErrorType.Object] = true
	}
	return objects
}

func supportedGeneratedTypeWithState(typ compilerTypes.Type, state *expressionValidation) bool {
	if state != nil && state.generatedTypes != nil {
		return validateGeneratedType(typ, state.generatedTypes, false)
	}
	return supportedGeneratedType(typ)
}

// layoutEligibleGenerated is the generator-side layout gate: the type must
// have a settled representation at this point, so a type parameter is a
// generation failure (specialization must have resolved it).
func layoutEligibleGenerated(typ compilerTypes.Type) bool {
	if typ == (compilerTypes.Type{}) || compilerTypes.ContainsTypeParameter(typ) {
		return false
	}
	if compilerTypes.IsUnknown(typ) || typ.Incomplete {
		return false
	}
	if typ.Signature != nil {
		return typ.Signature.Result != nil
	}
	return compilerTypes.IsCompleteValue(typ)
}

// volatileEligibleGenerated mirrors the checker's integer-only volatile set.
func volatileEligibleGenerated(typ compilerTypes.Type) bool {
	return compilerTypes.Equal(typ, compilerTypes.Int8) ||
		compilerTypes.Equal(typ, compilerTypes.Int16) ||
		compilerTypes.Equal(typ, compilerTypes.Int32) ||
		compilerTypes.Equal(typ, compilerTypes.Int64) ||
		compilerTypes.Equal(typ, compilerTypes.UInt8) ||
		compilerTypes.Equal(typ, compilerTypes.UInt16) ||
		compilerTypes.Equal(typ, compilerTypes.UInt32) ||
		compilerTypes.Equal(typ, compilerTypes.UInt64) ||
		compilerTypes.IsSize(typ)
}
