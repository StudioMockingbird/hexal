// objects.go owns nominal type bodies: object discovery, forward
// declarations, object and ADT and union body writing, and requirement
// collection.
package generator

import (
	"strings"

	"hexal/compiler/checker"
	compilerTypes "hexal/compiler/types"
)

func objectDefinitions(program checker.Program) ([]*compilerTypes.ObjectType, error) {
	objects := make([]*compilerTypes.ObjectType, 0)
	seen := make(map[*compilerTypes.ObjectType]bool)
	seenCNames := make(map[string]*compilerTypes.ObjectType)
	var conflict error
	// A module's header must carry every object type its translation unit
	// can name by value, including imported modules' exported objects
	// referenced through the import alias. They are reachable through the
	// checked statements' types, so the walk collects local and foreign
	// definitions together; each module file includes only its own header
	// plus hexal.h, so no translation unit ever sees two definitions of one
	// struct.
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			object := typ.Object
			if object == nil || typ.Incomplete {
				return nil
			}
			// A foreign record is defined by its C header: generated C emits
			// neither a forward typedef nor a body for it.
			if compilerTypes.IsForeignRecord(typ) {
				return nil
			}
			if previous, exists := seenCNames[object.CName]; exists && previous != object {
				conflict = unknownExpressionDiagnostic()
				return conflict
			}
			seenCNames[object.CName] = object
			if seen[object] {
				return nil
			}
			seen[object] = true
			objects = append(objects, object)
			return nil
		},
	}
	if err := walkProgram(program, visitor); err != nil {
		return nil, err
	}
	if conflict != nil {
		return nil, conflict
	}
	for _, declaration := range program.TypeDeclarations {
		object := declaration.Type.Object
		if object == nil || compilerTypes.IsForeignRecord(declaration.Type) {
			continue
		}
		seenCNames[object.CName] = object
		if seen[object] {
			continue
		}
		seen[object] = true
		objects = append(objects, object)
	}
	return objects, nil
}

// writeObjectForwardDeclarations emits `typedef struct CName CName;` for
// every object, in source declaration order, ahead of every full body: a
// pointer-typed member naming an object needs only this forward name,
// regardless of full-body emission order, and a recursive object needs its
// own name in scope before its body can name a pointer to itself.
func writeObjectForwardDeclarations(result *strings.Builder, objects []*compilerTypes.ObjectType, filename string) error {
	for _, object := range objects {
		if compilerTypes.IsBuiltinObject(object) {
			continue
		}
		if err := renderInto(result, "module.h", "object_forward", objectForwardModel{
			CName:    object.CName,
			Line:     object.SourceLine,
			Filename: filename,
		}); err != nil {
			return err
		}
	}
	return nil
}

// writeOneObjectBody emits one object's full struct body. Its own forward
// typedef must already be in scope; a member naming another nominal type by
// value additionally needs that type's own full body already written, which
// the dependency-ordered driver in emission.go guarantees before calling
// this.
func writeOneObjectBody(result *strings.Builder, object *compilerTypes.ObjectType, filename string) error {
	// C23 has no portable zero-sized object type, so an empty struct
	// carries one private byte instead; it is never read as part of
	// the object's surface (construction, equality, and printing all
	// special-case the empty member list).
	members := make([]objectMemberModel, 0, len(object.Members))
	for _, member := range object.Members {
		// Reference-like members (String, List, Dict) are pointer-sized
		// handles, spelled like their declarations.
		members = append(members, objectMemberModel{
			Line:        member.SourceLine,
			Filename:    filename,
			Declaration: declaration(member.Type, privateCName(memberName, member.Name, ""), true),
		})
	}
	return renderInto(result, "module.h", "object_body", objectBodyModel{
		CName:    object.CName,
		Line:     object.SourceLine,
		Filename: filename,
		Empty:    len(object.Members) == 0,
		Members:  members,
	})
}

// nominalBodyWriter emits object, ADT, and union struct bodies in dependency
// order: a type embedding another nominal type by value (not through a
// pointer) is written only after that type's own body already exists.
// Forward typedefs for all three categories are already in scope by the time
// this runs, so a pointer-typed reference in any direction needs nothing
// from this ordering. Hexal rejects direct by-value recursion, so this
// recursion always terminates without cycle tracking.
type nominalBodyWriter struct {
	result       *strings.Builder
	filename     string
	tags         *tagRegistry
	definedObj   map[*compilerTypes.ObjectType]bool
	definedAdt   map[*compilerTypes.AdtType]bool
	definedUnion map[*compilerTypes.UnionInfo]bool
}

// writeNominalBodies writes every object, ADT, and union full body reachable
// from these three discovery lists, each preceded by the bodies of every
// nominal type it embeds by value.
func writeNominalBodies(result *strings.Builder, objects []*compilerTypes.ObjectType, adts *generatedAdtState, unions *generatedUnionState, filename string, tags *tagRegistry) error {
	writer := &nominalBodyWriter{
		result:       result,
		filename:     filename,
		tags:         tags,
		definedObj:   make(map[*compilerTypes.ObjectType]bool),
		definedAdt:   make(map[*compilerTypes.AdtType]bool),
		definedUnion: make(map[*compilerTypes.UnionInfo]bool),
	}
	for _, object := range objects {
		if err := writer.ensureObject(object); err != nil {
			return err
		}
	}
	if adts != nil {
		for _, adtType := range adts.order {
			if err := writer.ensureAdt(adtType); err != nil {
				return err
			}
		}
	}
	if unions != nil {
		for _, union := range unions.order {
			if err := writer.ensureUnion(union); err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureType writes whatever by-value nominal body one member's type still
// needs before that member can be spelled: an array's inline element,
// recursively, and an object, ADT, or non-nullable union in whichever of the
// three categories it belongs to. A nullable union (Ptr<T> | Nil and its
// kind) lowers to a bare pointer or an inline tag-and-pointer pair, never the
// tagged-struct body writeOneUnionBody produces, so it needs nothing here. A
// pointer, scalar, string, or other reference-like handle needs only the
// forward typedef every category already has.
func (writer *nominalBodyWriter) ensureType(typ compilerTypes.Type) error {
	if compilerTypes.IsNullable(typ) {
		return nil
	}
	switch {
	case typ.Object != nil:
		return writer.ensureObject(typ.Object)
	case typ.Adt != nil:
		return writer.ensureAdt(typ)
	case typ.Union != nil:
		return writer.ensureUnion(typ)
	case typ.Array != nil:
		return writer.ensureType(typ.Array.Element)
	}
	return nil
}

func (writer *nominalBodyWriter) ensureObject(object *compilerTypes.ObjectType) error {
	if object == nil || writer.definedObj[object] || compilerTypes.IsBuiltinObject(object) {
		// ProcessOptions, EnvironmentVariable, and StartedProcess are
		// compiler-owned structs whose bodies are hand-written once in
		// hexal/process.h, mirroring the identical IsBuiltinAdt skip above.
		return nil
	}
	writer.definedObj[object] = true
	for _, member := range object.Members {
		if err := writer.ensureType(member.Type); err != nil {
			return err
		}
	}
	return writeOneObjectBody(writer.result, object, writer.filename)
}

func (writer *nominalBodyWriter) ensureAdt(adtType compilerTypes.Type) error {
	adt := adtType.Adt
	if adt == nil || writer.definedAdt[adt] || compilerTypes.IsBuiltinAdt(adtType) {
		// Seek is a fixed, module-ownerless built-in ADT emitted once, by
		// seekComponents/moduleSeekComponent, into a shared header instead
		// of repeated inline per module; see the identical skip this
		// replaced in the former writeAdtDefinitions.
		return nil
	}
	writer.definedAdt[adt] = true
	for _, variant := range adt.Variants {
		for _, member := range variant.Payload {
			if err := writer.ensureType(member.Type); err != nil {
				return err
			}
		}
	}
	return writeOneAdtBody(writer.result, adtType)
}

func (writer *nominalBodyWriter) ensureUnion(union compilerTypes.Type) error {
	info := union.Union
	if info == nil || writer.definedUnion[info] || compilerTypes.IsBuiltinUnion(union) {
		// String | Nil and Pipe | Nil, ProcessOptions and StartedProcess's
		// fixed structural fields, are hand-written once in hexal/process.h
		// alongside the objects that embed them.
		return nil
	}
	writer.definedUnion[info] = true
	for _, member := range info.Members {
		if err := writer.ensureType(member); err != nil {
			return err
		}
	}
	return writeOneUnionBody(writer.result, union, writer.tags)
}

// collectTypeRequirements folds one module's written checked types into the
// program-wide standard-header and hex_eos requirement set.
// Exact-width integers and their aliases (Byte) require <stdint.h>;
// Size and Nil require <stddef.h>; a written EoS type requires <stdint.h>
// and the hex_eos typedef. The walk descends into ADT payloads, union
// members, pointer pointees, signatures, and collection element types, so a
// nested EoS or Nil member is discovered wherever it is spelled.
func collectTypeRequirements(program checker.Program, requirements *cHeaderRequirements) error {
	visitor := &programVisitor{
		Type: func(typ compilerTypes.Type) error {
			switch {
			case compilerTypes.IsSize(typ):
				// Size spells size_t, owned by <stddef.h> independently of
				// any allocation or Nil usage. Checked before IsInteger:
				// Size is also an unsigned integer scalar.
				requirements.add("stddef.h")
			case compilerTypes.IsInteger(typ):
				// Int8..Int64, UInt8..UInt64, and the Byte alias all
				// spell exact-width C types from <stdint.h>.
				requirements.add("stdint.h")
			case compilerTypes.IsEoS(typ):
				// EoS spells hex_eos, one compiler-owned byte typedef.
				requirements.eos = true
				requirements.add("stdint.h")
			}
			return nil
		},
		Operand: func(source checker.Operand) error {
			// A special float literal renders the NAN/INFINITY macros from
			// <math.h>; a finite literal needs no header.
			if compilerTypes.IsFloat(source.Type) && source.FloatBits != 0 {
				bitSize := 64
				bits := source.FloatBits
				if compilerTypes.Equal(source.Type, compilerTypes.Float32) {
					bitSize = 32
					bits = uint64(uint32(bits))
				}
				if _, special := floatSignAndSpecial(bits, bitSize); special {
					requirements.add("math.h")
				}
			}
			return nil
		},
		Expression: func(node checker.Expression) error {
			// Unsigned add/subtract/multiply renders through a width-picked
			// uint64_t or uint32_t intermediate, so the operation selects
			// <stdint.h> even when no written type spells an exact-width
			// integer (the Size-only case).
			if node.Kind == checker.BinaryOperationExpression &&
				(node.Operator == checker.AddOperator || node.Operator == checker.SubtractOperator || node.Operator == checker.MultiplyOperator) &&
				compilerTypes.IsUnsignedInteger(node.OperandType) {
				requirements.add("stdint.h")
			}
			return nil
		},
	}
	return walkProgram(program, visitor)
}
