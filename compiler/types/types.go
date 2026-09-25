// Package types is the Hexal type system: the interned Type identity every
// later stage reads, the per-compilation Environment and Arena that intern
// constructed types, and the structured Diagnostic every stage reports through.
package types

import (
	"strconv"
	"strings"
)

// ScalarKind enumerates the scalar types Hexal supports. ScalarNone is the
// zero value shared by every non-scalar type.
type ScalarKind int

// The concrete ScalarKind values, named for the scalar family they classify.
const (
	ScalarNone ScalarKind = iota
	ScalarSignedInteger
	ScalarUnsignedInteger
	ScalarFloat
	ScalarBool
)

// typeIdentity is the compilation-scoped identity behind one interned Type.
// Builtin scalars share one package-level identity; every constructed type
// receives a fresh identity from the environment that created it. signature
// is the interning key the identity was created for, so forged metadata copies
// fail canonical validation even when every field looks self-consistent.
type typeIdentity struct {
	object    *ObjectType
	signature string
}

func newTypeIdentity() *typeIdentity {
	return &typeIdentity{}
}

// Type is the interned identity of one Hexal type. Every field except the
// identity is descriptive metadata; identity decides equality and interning.
type Type struct {
	// Name is the user-facing name of the type, used in diagnostics.
	Name string
	// CName is the C name of the type, used in code generation.
	CName string
	// CanonicalKey is the recursive, module-qualified identity of the type:
	// "m1_m5:Point" for an object, "List:m1_m5:Point" for one constructed
	// type over it. Display names never participate in identity; this key
	// does. It is never displayed.
	CanonicalKey string
	// ScalarKind, when non-zero, identifies this as a scalar type.
	ScalarKind ScalarKind
	// Bits is the bit width of integer and float scalars.
	Bits int
	// Element is the pointee of pointer types and nullable pointer metadata.
	Element *Type
	// PointeeWritable is true when this is a Ptr<mut T>.
	PointeeWritable bool
	// Object is the nominal object record when this is an object type.
	Object *ObjectType
	// Signature is the function signature when this is a function type.
	Signature *FunSignature
	// Incomplete is true for forward-declared or erased types whose value
	// layout is not yet (or never) known.
	Incomplete bool
	// NullableBase, when non-nil, is the base type of a nullable type. The
	// nullable type itself is a distinct identity.
	NullableBase *Type
	// Union holds the union metadata for tagged union types.
	Union *UnionInfo
	// Adt holds the nominal ADT record when this is an ADT type.
	Adt *AdtType
	// Array holds the metadata of fixed inline array types.
	Array *ArrayInfo
	// InlineString holds the metadata of inline text types, String<N>.
	InlineString *InlineStringInfo
	// Slice holds the metadata of non-owning contiguous slice types.
	Slice *SliceInfo
	// List holds the metadata of owning growable list types.
	List *ListInfo
	// Dict holds the metadata of owning dictionary types.
	Dict *DictInfo
	// Task holds the metadata of spawned task handle types.
	Task *TaskInfo
	// Channel holds the metadata of bounded channel handle types.
	Channel *ChannelInfo
	// Mutex holds the metadata of scheduler-aware mutex handle types.
	Mutex *MutexInfo
	// Atomic holds the metadata of inline atomic wrapper types.
	Atomic *AtomicInfo
	// Stash holds the metadata of an independent typed bump-allocator handle.
	Stash *StashInfo
	// Pool holds the metadata of an independent typed fixed-capacity
	// slot-allocator handle.
	Pool *PoolInfo
	// Generic, when non-nil, identifies this type as a generic parameter
	// placeholder; GenericIndex is the parameter's position.
	Generic      *GenericDeclaration
	GenericIndex int
	// identity is the internal identity of this type, unique across the
	// process and scoped to its creating compilation.
	identity *typeIdentity
}

// FunSignature describes the full signature of a function type.
type FunSignature struct {
	Name       string
	Parameters []Type
	Result     *Type // nil when the function has no result
	// Rest marks a homogeneous rest parameter. When true, the final entry of
	// Parameters is the element type T of a `T...` parameter rather than an
	// ordinary T parameter, so this signature is called with m fixed arguments
	// followed by zero or more T elements.
	Rest bool
	// RestSlice is the read-only Slice<T> the C ABI passes for a rest
	// signature. It is the zero Type when Rest is false.
	RestSlice Type
}

// ObjectMember is one member of a nominal object type or ADT payload field.
type ObjectMember struct {
	Name         string
	Type         Type
	Use          TypeUse
	Mutable      bool
	SourceLine   int
	SourceColumn int
	// CName is the exact C field spelling when it differs from Name. A foreign
	// record keeps the original C field name here while Name carries the
	// usable Hexal spelling, so generated C never renames a foreign field.
	// Empty means the field has no distinct C spelling and generation uses
	// Name.
	CName string
}

// ObjectType is the compilation-owned nominal record behind an object Type.
type ObjectType struct {
	Name  string
	CName string
	// ModuleID is the canonical identity of the module that declared the
	// object; it is empty for compiler-owned builtins. The checker stamps
	// it on every object it creates in a module scope; imported objects
	// carry their defining module's id, which is what lets implementation
	// ownership and method routing find the owner.
	// Owner caches EncodeModuleOwner(ModuleID): the encoded spelling that
	// generated C names embed. Write it only through SetModuleOwner so the
	// cached spelling can never disagree with ModuleID.
	ModuleID     string
	Owner        string
	Members      []ObjectMember
	SourceLine   int
	SourceColumn int
	Incomplete   bool
	identity     *typeIdentity
}

// SetModuleOwner stamps the declaring module's canonical id and its derived
// encoded owner as one operation, so generation can read the spelling without
// re-encoding it at every derivation site.
func (object *ObjectType) SetModuleOwner(moduleID string) {
	object.ModuleID = moduleID
	object.Owner = EncodeModuleOwner(moduleID)
}

// Member resolves one member of the object by name.
func (object *ObjectType) Member(name string) (*ObjectMember, bool) {
	if object == nil {
		return nil, false
	}
	for index := range object.Members {
		if object.Members[index].Name == name {
			return &object.Members[index], true
		}
	}
	return nil, false
}

// ErrorCategory classifies a compilation error.
// Environment is the store of all module-scoped types known to one
// compilation: builtins, objects, ADTs, names, aliases, and generic
// declarations. Constructed types are interned in the shared arena, once per
// compilation.
type Environment struct {
	names               map[string]Type // builtins, objects, and ADTs by name
	aliases             map[string]Type
	aliasUses           map[string]TypeUse
	genericDeclarations map[string]*GenericDeclaration
	identity            *typeIdentity
	// arena holds the compilation-wide constructed-type intern maps.
	arena *Arena
	// moduleID is the canonical id of the module this environment belongs
	// to ("" for the compiler-owned builtin environment); it is the source
	// of CanonicalKey module qualification.
	moduleID string
	// owner is the encoded module owner ("" for the compiler-owned builtin
	// environment). User object types interned here carry it in their C
	// name: hex_t_m3_app_Point names module "app".
	owner string
}

// EncodeModuleOwner encodes each "/"-separated component as its decimal
// UTF-8 byte length, "_", then the source spelling, all prefixed with one
// leading "m". Case-preserving; no case folding; "graphics/shapes" ->
// "m8_graphics6_shapes". The leading "m" keeps the encoded owner a valid
// identifier prefix wherever it is embedded, as in the module header guard
// HEX_MODULE_m8_graphics6_shapes_H. An empty canonical id (a compiler-owned
// type with no defining module) encodes to nothing. Nominal records cache
// their spelling at construction; this function stays pure so repeated calls
// never share state across compilations.
func EncodeModuleOwner(canonicalID string) string {
	if canonicalID == "" {
		return ""
	}
	// A source stdlib module drops the reserved std collection and takes the
	// s prefix, so its symbols can never collide with a user module's.
	prefix := "m"
	if IsStdlibModule(canonicalID) {
		prefix = "s"
		canonicalID = strings.TrimPrefix(canonicalID, "std/")
	}
	parts := strings.Split(canonicalID, "/")
	for index, part := range parts {
		parts[index] = strconv.Itoa(len(part)) + "_" + part
	}
	return prefix + strings.Join(parts, "")
}

// ModuleHeaderGuard returns the include guard for a module header:
// "HEX_MODULE_" + encoded owner + "_H". Case-preserving; hexal.h keeps its
// own fixed guard.
func ModuleHeaderGuard(canonicalID string) string {
	return "HEX_MODULE_" + EncodeModuleOwner(canonicalID) + "_H"
}

// IsStdlibModule reports whether canonicalID names a source stdlib module.
// User module ids are relative, so the reserved std collection is unambiguous.
func IsStdlibModule(canonicalID string) bool {
	return strings.HasPrefix(canonicalID, "std/")
}

// ModuleArtifactStem returns a module's generated artifact path without its
// extension: user modules emit under modules/, source stdlib modules under
// the reserved stdlib/ path.
func ModuleArtifactStem(canonicalID string) string {
	if IsStdlibModule(canonicalID) {
		return "stdlib/" + strings.TrimPrefix(canonicalID, "std/")
	}
	return "modules/" + canonicalID
}

// NewEnvironment returns an empty environment seeded with the builtin types
// and its own fresh arena.
func NewEnvironment() *Environment {
	return newEnvironmentWithOwner("")
}

func newEnvironmentWithOwner(moduleID string) *Environment {
	return NewCompilationEnvironment(NewArena(), moduleID)
}

// NewCompilationEnvironment returns one module scope over a shared
// compilation arena. Every module of one compilation must share one arena so
// constructed types intern once per compilation; the checker creates the
// arena in CheckModules and passes it to each module.
func NewCompilationEnvironment(arena *Arena, moduleID string) *Environment {
	environment := &Environment{
		names:               make(map[string]Type),
		aliases:             make(map[string]Type),
		aliasUses:           make(map[string]TypeUse),
		genericDeclarations: make(map[string]*GenericDeclaration),
		identity:            newTypeIdentity(),
		arena:               arena,
		moduleID:            moduleID,
		owner:               EncodeModuleOwner(moduleID),
	}
	return environment
}

// Arena exposes the compilation-wide constructed-type arena this environment
// interns through. The checker's foreign record registry reads it so one C
// record has one identity across every module.
func (environment *Environment) Arena() *Arena {
	if environment == nil {
		return nil
	}
	return environment.arena
}

// Lookup resolves a declared object or ADT type name, falling back to the
// immutable builtin registry. Aliases resolve through their own registry,
// which shadows nothing published by the environment. Module declarations can
// never collide with builtin names -- every declaration path rejects taken
// names before binding -- so the fallback order preserves the flat-namespace
// semantics without copying the builtin table into each module environment.
func (environment *Environment) Lookup(name string) (Type, bool) {
	if environment == nil {
		return Type{}, false
	}
	if typ, ok := environment.names[name]; ok {
		return typ, true
	}
	if typ, ok := environment.aliases[name]; ok {
		return typ, true
	}
	typ, ok := builtinTypes[name]
	return typ, ok
}

// Contains reports whether a type name is already taken by a builtin, object,
// ADT, or alias in this compilation.
func (environment *Environment) Contains(name string) bool {
	if environment == nil {
		return false
	}
	if _, ok := environment.names[name]; ok {
		return true
	}
	if _, ok := environment.aliases[name]; ok {
		return true
	}
	if _, ok := environment.aliasUses[name]; ok {
		return true
	}
	_, ok := builtinTypes[name]
	return ok
}

// DeclareAlias declares a type alias by its resolved canonical type.
func (environment *Environment) DeclareAlias(name string, typ Type) {
	if environment == nil {
		return
	}
	environment.aliases[name] = typ
	environment.aliasUses[name] = NewTypeUse(typ)
}

// DeclareAliasUse records an alias use site for later resolution.
func (environment *Environment) DeclareAliasUse(name string, use TypeUse) {
	if environment == nil {
		return
	}
	environment.aliasUses[name] = use
}

// LookupUse resolves a type name to its written use: aliases first, then
// module declarations, then the immutable builtin registry.
func (environment *Environment) LookupUse(name string) (TypeUse, bool) {
	if environment == nil {
		return TypeUse{}, false
	}
	if use, ok := environment.aliasUses[name]; ok {
		return use, true
	}
	if typ, ok := environment.names[name]; ok {
		return NewTypeUse(typ), true
	}
	typ, ok := builtinTypes[name]
	if !ok {
		return TypeUse{}, false
	}
	return NewTypeUse(typ), true
}

// BeginObject publishes a provisional nominal object identity and binds the
// source name before members are resolved, so a member may reach the object
// behind at least one pointer layer.
func (environment *Environment) BeginObject(name string, sourceLine, sourceColumn int) Type {
	if environment == nil {
		return Type{}
	}
	identity := newTypeIdentity()
	identity.signature = "object:" + name
	cName := "hex_t_" + SanitizeIdentifier(name)
	if environment.owner != "" {
		cName = "hex_t_" + environment.owner + "_" + SanitizeIdentifier(name)
	}
	object := &ObjectType{
		Name:         name,
		CName:        cName,
		SourceLine:   sourceLine,
		SourceColumn: sourceColumn,
		identity:     identity,
	}
	object.SetModuleOwner(environment.moduleID)
	identity.object = object
	typ := Type{
		Name:         name,
		CName:        object.CName,
		CanonicalKey: canonicalNominalKey(name, environment.moduleID),
		Object:       object,
		Incomplete:   true,
		identity:     identity,
	}
	object.Incomplete = true
	environment.arena.ReserveDefinitionName(cName, typ)
	environment.names[name] = typ
	return typ
}

// canonicalNominalKey builds the recursive identity key of a nominal type:
// the encoded defining-module id plus the bare name. Compiler-owned types
// with no module keep the bare name.
func canonicalNominalKey(name, moduleID string) string {
	if moduleID == "" {
		return name
	}
	return EncodeModuleOwner(moduleID) + ":" + name
}

// CanonicalNominalKey is the exported form of canonicalNominalKey. The
// checker re-keys a generic specialization's provisional object or ADT after
// stamping its defining module, which may differ from the requesting
// module's environment.
func CanonicalNominalKey(name, moduleID string) string {
	return canonicalNominalKey(name, moduleID)
}

// CompleteObject finalizes a provisional object with its resolved members.
func (environment *Environment) CompleteObject(name string, members []ObjectMember) Type {
	if environment == nil {
		return Type{}
	}
	typ, ok := environment.names[name]
	if !ok || typ.Object == nil {
		return Type{}
	}
	typ.Object.Members = append([]ObjectMember(nil), members...)
	typ.Incomplete = false
	typ.Object.Incomplete = false
	return typ
}

// AbandonObject releases a provisional object whose members failed to resolve.
func (environment *Environment) AbandonObject(name string) {
	if environment == nil {
		return
	}
	delete(environment.names, name)
}

// PtrType constructs or retrieves the canonical Ptr<T> type of one element.
func (environment *Environment) PtrType(element Type) Type {
	return environment.pointerType(element, false)
}

// MutPtrType constructs or retrieves the canonical writable-pointee
// pointer type of one element, displayed as Ptr<mut T>.
func (environment *Environment) MutPtrType(element Type) Type {
	return environment.pointerType(element, true)
}

func (environment *Environment) pointerType(element Type, writable bool) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, element, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, true) ||
		isManaged(element) ||
		// The pointee occupies a named position: a direct Atomic element is
		// rejected by Storable, while an enclosing object stays valid
		// because containment stops at the indirection. The check defers
		// for open type parameters and provisional objects, which are
		// rechecked when they become concrete, and keeps the explicit
		// Unknown exception that makes Ptr<Unknown>/Ptr<mut Unknown> void*.
		(!IsUnknown(element) && !ContainsTypeParameter(element) && IsCompleteValue(element) && !Storable(element, PositionPointee)) {
		return Type{}
	}
	constructor := "Ptr"
	if writable {
		constructor = "MutPtr"
	}
	canonicalKey := constructor + ":" + element.CanonicalKey
	if cached, ok := environment.arena.pointerTypes[canonicalKey]; ok {
		return cached
	}
	cName := element.CName + "*"
	if IsUnknown(element) {
		cName = "void*"
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	// The source spelling is one Ptr family: the writable form displays as
	// Ptr<mut T>. The canonical key keeps the legacy constructor tag; it is
	// an interning detail, never a user-visible spelling.
	display := "Ptr<" + element.Name + ">"
	if writable {
		display = "Ptr<mut " + element.Name + ">"
	}
	typ := Type{
		Name:            display,
		CName:           cName,
		CanonicalKey:    canonicalKey,
		Element:         &element,
		PointeeWritable: writable,
		identity:        identity,
	}
	environment.arena.pointerTypes[canonicalKey] = typ
	return typ
}

// PtrType constructs the canonical Ptr<T> type of one element in a fresh
// compilation scope. Later stages use this when only the pointer's metadata
// (name, C name) is needed; the checker always interns through its own
// environment.
//
// The environment must be fresh per call, never shared across calls. An
// arena interns by CanonicalKey, which is module-qualified but not
// identity-qualified, so a shared arena hands a second compilation the first
// compilation's ObjectType for a same-named type, and generation then
// rejects it as undeclared.
func PtrType(element Type) Type {
	return NewEnvironment().PtrType(element)
}

// MutPtrType is the package-level convenience form of MutPtrType, with the
// same fresh-environment requirement as PtrType.
func MutPtrType(element Type) Type {
	return NewEnvironment().MutPtrType(element)
}

// NullableType constructs or retrieves the canonical nullable form of a
// pointer-like base type, reusing the base's C representation. Nullable forms
// are idempotent: a nullable base returns itself.
func (environment *Environment) NullableType(base Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, base, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!IsPointerLike(base) {
		return Type{}
	}
	if base.NullableBase != nil {
		return base
	}
	canonicalKey := "nullable:" + base.CanonicalKey
	if cached, ok := environment.arena.nullableTypes[canonicalKey]; ok {
		return cached
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	nullable := Type{
		Name:            base.Name + " | Nil",
		CName:           base.CName,
		CanonicalKey:    canonicalKey,
		Element:         base.Element,
		PointeeWritable: base.PointeeWritable,
		Signature:       base.Signature,
		NullableBase:    &base,
		identity:        identity,
	}
	environment.arena.nullableTypes[canonicalKey] = nullable
	return nullable
}

// NullableBase returns the base type of a nullable type.
func NullableBase(typ Type) (Type, bool) {
	if typ.NullableBase == nil {
		return Type{}, false
	}
	return *typ.NullableBase, true
}

// IsNullable reports whether typ is the nullable form of a pointer-like base.
func IsNullable(typ Type) bool { return typ.NullableBase != nil }

// FunType constructs or retrieves the canonical function type for one
// ordered parameter list and optional result. It is the non-rest form; a
// signature whose final parameter is `T...` is built with FunTypeRest.
func (environment *Environment) FunType(parameters []Type, result *Type) Type {
	return environment.FunTypeRest(parameters, result, false)
}

// FunTypeRest constructs or retrieves the canonical function type for one
// ordered parameter list and optional result, with rest marking a final `T...`
// parameter. Rest is part of the type's identity: Fun<(T...)> and
// Fun<(Slice<T>)> are distinct and not assignable to one another.
func (environment *Environment) FunTypeRest(parameters []Type, result *Type, rest bool) Type {
	if environment == nil {
		return Type{}
	}
	if rest && len(parameters) == 0 {
		return Type{}
	}
	state := canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}
	for _, parameter := range parameters {
		if !isCanonicalForEnvironment(environment, parameter, &state, false) || !IsCompleteValue(parameter) && !ContainsTypeParameter(parameter) {
			return Type{}
		}
	}
	if result != nil {
		if !isCanonicalForEnvironment(environment, *result, &state, false) || !IsCompleteValue(*result) && !ContainsTypeParameter(*result) {
			return Type{}
		}
	}
	canonicalKey := funKey(parameters, result, rest)
	if cached, ok := environment.arena.funTypes[canonicalKey]; ok {
		return cached
	}
	name := funName(parameters, result, rest)
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	var restSlice Type
	if rest {
		restSlice = environment.SliceType(parameters[len(parameters)-1], false)
	}
	typ := Type{
		Name: name,
		Signature: &FunSignature{
			Name:       name,
			Parameters: append([]Type(nil), parameters...),
			Result:     result,
			Rest:       rest,
			RestSlice:  restSlice,
		},
		CanonicalKey: canonicalKey,
		identity:     identity,
	}
	environment.arena.funTypes[canonicalKey] = typ
	return typ
}

func funKey(parameters []Type, result *Type, rest bool) string {
	var builder strings.Builder
	builder.WriteString("fun:")
	for _, parameter := range parameters {
		builder.WriteString(parameter.CanonicalKey)
		builder.WriteString(",")
	}
	if rest {
		builder.WriteString(";rest")
	}
	if result != nil {
		builder.WriteString(":")
		builder.WriteString(result.CanonicalKey)
	}
	return builder.String()
}

func funName(parameters []Type, result *Type, rest bool) string {
	var builder strings.Builder
	builder.WriteString("Fun<(")
	last := len(parameters) - 1
	for index, parameter := range parameters {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(parameter.Name)
		if rest && index == last {
			builder.WriteString("...")
		}
	}
	builder.WriteString(")")
	if result != nil {
		builder.WriteString(" : ")
		builder.WriteString(result.Name)
	}
	builder.WriteString(">")
	return builder.String()
}

// Equal reports whether two types share one interned identity. Two zero types
// (no identity, no metadata) compare equal so that "no result" positions match.
func Equal(left, right Type) bool {
	if left.identity == nil || right.identity == nil {
		return left == right
	}
	return left.identity == right.identity
}

// IsNil reports whether typ is the canonical Nil type.
func IsNil(typ Type) bool { return typ.identity != nil && typ.identity == Nil.identity }

// IsEoS reports whether typ is the canonical EoS type.
func IsEoS(typ Type) bool { return typ.identity != nil && typ.identity == EoS.identity }

// IsError reports whether typ is the canonical Error type.
func IsError(typ Type) bool { return typ.identity != nil && typ.identity == ErrorType.identity }

// IsUnknown reports whether typ is the canonical Unknown type.
func IsUnknown(typ Type) bool { return typ.identity != nil && typ.identity == Unknown.identity }

// IsHeap reports whether typ is the canonical Heap type.
func IsHeap(typ Type) bool { return typ.identity != nil && typ.identity == Heap.identity }

// IsPointerLike reports whether typ is a pointer, a function pointer, or a
// nullable form of either: the values that can hold Nil.
func IsPointerLike(typ Type) bool {
	return typ.Element != nil || typ.Signature != nil
}

// IsInteger reports whether typ is a signed or unsigned integer scalar.
func IsInteger(typ Type) bool {
	return typ.ScalarKind == ScalarSignedInteger || typ.ScalarKind == ScalarUnsignedInteger
}

// IsSignedInteger reports whether typ is a signed integer scalar.
func IsSignedInteger(typ Type) bool { return typ.ScalarKind == ScalarSignedInteger }

// IsUnsignedInteger reports whether typ is an unsigned integer scalar.
func IsUnsignedInteger(typ Type) bool { return typ.ScalarKind == ScalarUnsignedInteger }

// IsRune reports whether typ is the canonical Rune scalar.
func IsRune(typ Type) bool { return typ.identity != nil && typ.identity == Rune.identity }

// IsByteCursor reports whether typ is the canonical ByteCursor descriptor.
func IsByteCursor(typ Type) bool {
	return typ.identity != nil && typ.identity == ByteCursorType.identity
}

// IsRuneCursor reports whether typ is the canonical RuneCursor descriptor.
func IsRuneCursor(typ Type) bool {
	return typ.identity != nil && typ.identity == RuneCursorType.identity
}

// IsGrapheme reports whether typ is the canonical Grapheme borrowed range.
func IsGrapheme(typ Type) bool { return typ.identity != nil && typ.identity == GraphemeType.identity }

// IsGraphemeCursor reports whether typ is the canonical GraphemeCursor.
func IsGraphemeCursor(typ Type) bool {
	return typ.identity != nil && typ.identity == GraphemeCursorType.identity
}

// IsCursor reports whether typ is one of the text cursor descriptors.
func IsCursor(typ Type) bool {
	return IsByteCursor(typ) || IsRuneCursor(typ) || IsGraphemeCursor(typ)
}

// IsFloat reports whether typ is a float scalar.
func IsFloat(typ Type) bool { return typ.ScalarKind == ScalarFloat }

// IsCompleteValue reports whether typ names a value with a known layout:
// scalars, pointers, function pointers, objects, ADTs, arrays, and unions.
// Unknown and provisional objects are not complete values.
func IsCompleteValue(typ Type) bool {
	if typ.Object != nil {
		// Read the shared record: a Type copy captured before the record was
		// completed must not keep reporting an incomplete layout.
		return !typ.Object.Incomplete
	}
	if typ.Array != nil {
		return IsCompleteValue(typ.Array.Element)
	}
	if typ.NullableBase != nil {
		return IsCompleteValue(*typ.NullableBase)
	}
	if typ.Union != nil || typ.Adt != nil {
		return true
	}
	return typ.identity != nil && !typ.Incomplete
}

// Assignable reports whether a value of source type may be assigned to a
// target of target type: identity, pointer weakening, nullable injection,
// one-layer Unknown erasure or recovery, union member injection, and union
// widening. Everything else, including any form of narrowing, is rejected.
func Assignable(target, source Type) bool {
	if Equal(target, source) {
		return true
	}
	if WidensTo(source, target) {
		return true
	}
	if target.identity == nil || source.identity == nil {
		return false
	}
	if base, ok := NullableBase(target); ok {
		if IsNil(source) {
			return true
		}
		if sourceBase, ok := NullableBase(source); ok {
			return Assignable(base, sourceBase)
		}
		return Assignable(base, source)
	}
	if target.Union != nil {
		for _, member := range target.Union.Members {
			if Equal(member, source) {
				return true
			}
		}
		if IsUnion(source) {
			for _, member := range unionMembers(source) {
				if !ContainsUnionMember(target, member) {
					return false
				}
			}
			return true
		}
		return false
	}
	if IsNil(source) {
		return false
	}
	if source.NullableBase != nil {
		// Nullable removal is narrowing: a value that may be Nil cannot flow
		// into a non-nullable slot.
		return false
	}
	if target.Slice != nil && source.Slice != nil {
		// Slice weakening mirrors pointer weakening: a writable Slice
		// converts to the read-only Slice over the identical element at
		// the outermost layer only. Nested modes never weaken, so unequal
		// elements (including nested Slice modes) reject.
		return Equal(target.Slice.Element, source.Slice.Element) && !target.Slice.Writable && source.Slice.Writable
	}
	if target.Element != nil && source.Element != nil {
		if !Equal(*target.Element, *source.Element) {
			targetErased := IsUnknown(*target.Element)
			sourceErased := IsUnknown(*source.Element)
			if targetErased == sourceErased {
				return false
			}
		}
		if target.PointeeWritable && !source.PointeeWritable {
			return false
		}
		return true
	}
	if target.List != nil && source.List != nil {
		// Two List<T> specializations sharing one canonical key are the same
		// physical hex_list_<T> struct: List's own interning lives on the
		// per-compilation arena, so a builtin object built at package init()
		// time (before any arena exists) necessarily gets a distinct
		// identity from a live environment.ListType(...) call for the
		// identical element, even though both describe the same generated
		// type. CanonicalKey, not CName, is the comparison: it is the
		// recursive, module-qualified identity Type.CanonicalKey documents,
		// exactly the fact this fallback needs.
		return target.CanonicalKey == source.CanonicalKey && target.CanonicalKey != ""
	}
	return false
}

// TruthinessKind classifies how a value's truthiness is decided.
type TruthinessKind int

// The concrete TruthinessKind values. TruthinessInvalid is the unset zero
// value, and TruthinessAlwaysTrue marks a type with no false representation
// at all.
const (
	TruthinessInvalid TruthinessKind = iota
	TruthinessBool
	TruthinessNil
	TruthinessNullable
	TruthinessUnion
	TruthinessAlwaysTrue
)

// Truthiness reports the category of a type's truthiness decision.
func Truthiness(typ Type) TruthinessKind {
	if typ.ScalarKind == ScalarBool {
		return TruthinessBool
	}
	if IsNil(typ) {
		return TruthinessNil
	}
	if typ.NullableBase != nil {
		return TruthinessNullable
	}
	if typ.Union != nil {
		return TruthinessUnion
	}
	if typ.Incomplete {
		return TruthinessInvalid
	}
	return TruthinessAlwaysTrue
}
