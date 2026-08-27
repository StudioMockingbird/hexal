// Package types is the Hexal type system: the interned Type identity every
// later stage reads, the per-compilation Environment and Arena that intern
// constructed types, and the structured Diagnostic every stage reports through.
package types

import (
	"errors"
	"fmt"
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
	// PointeeWritable is true when this is a MutPtr<T>.
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
	// View holds the metadata of non-owning contiguous view types.
	View *ViewInfo
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
}

// ObjectMember is one member of a nominal object type or ADT payload field.
type ObjectMember struct {
	Name         string
	Type         Type
	Use          TypeUse
	Mutable      bool
	SourceLine   int
	SourceColumn int
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
type ErrorCategory string

// The concrete ErrorCategory values. Each string is the exact bracketed
// label a Diagnostic renders in its Error() text, so renaming one changes
// every diagnostic of that category.
const (
	SemanticError      ErrorCategory = "Semantic Error"
	TypeError          ErrorCategory = "Type Error"
	NameError          ErrorCategory = "Name Error"
	ModuleError        ErrorCategory = "Module Error"
	ConfigurationError ErrorCategory = "Configuration Error"
	UnknownError       ErrorCategory = "Unknown Error"
	SyntaxError        ErrorCategory = "Syntax Error"
)

// Diagnostic is one structured compilation error.
type Diagnostic struct {
	Category ErrorCategory
	Stage    string
	// Module is the logical source key the diagnostic belongs to, the same
	// key the caller supplied in the sources map. Construction sites do not
	// set it: each stage stamps its diagnostics at the one point where it
	// knows which module it was checking, so a message can never claim the
	// wrong module. It is empty only for a diagnostic about the compilation
	// as a whole, such as a missing entrypoint.
	Module  string
	Line    int
	Column  int
	Message string
}

// InModule returns a copy of the diagnostic stamped with its module, leaving
// an already-stamped diagnostic alone so an inner stage's attribution wins
// over an outer one's.
func (diagnostic Diagnostic) InModule(module string) Diagnostic {
	if diagnostic.Module == "" {
		diagnostic.Module = module
	}
	return diagnostic
}

// InModule stamps every diagnostic of the set with its module.
func (diagnostics Diagnostics) InModule(module string) Diagnostics {
	stamped := make(Diagnostics, len(diagnostics))
	for index, diagnostic := range diagnostics {
		stamped[index] = diagnostic.InModule(module)
	}
	return stamped
}

// Diagnostics is the ordered set of diagnostics one stage produced.
type Diagnostics []Diagnostic

// Error renders the diagnostic's bracketed category, message, and source
// location, satisfying the standard error interface.
func (diagnostic Diagnostic) Error() string {
	// An empty category is a construction defect in the compiler: every site
	// must name a category, so an empty one renders as "[]" instead of being
	// masked as a compiler Unknown Error that a user error never merits.
	// The module qualifies the position: in a multi-module build "at 5:3"
	// names no file, and two modules' messages read as one interleaved list.
	location := ""
	if diagnostic.Line > 0 {
		if diagnostic.Module != "" {
			location = fmt.Sprintf(" at %s:%d:%d", diagnostic.Module, diagnostic.Line, diagnostic.Column)
		} else {
			location = fmt.Sprintf(" at %d:%d", diagnostic.Line, diagnostic.Column)
		}
	}
	return "[" + string(diagnostic.Category) + "] " + diagnostic.Message + location
}

// Error joins every diagnostic's own Error() text with a newline, satisfying
// the standard error interface for a whole diagnostic set.
func (diagnostics Diagnostics) Error() string {
	if len(diagnostics) == 0 {
		return ""
	}
	messages := make([]string, len(diagnostics))
	for index, diagnostic := range diagnostics {
		messages[index] = diagnostic.Error()
	}
	return strings.Join(messages, "\n")
}

// NewDiagnostic builds one structured diagnostic for a source location.
func NewDiagnostic(category ErrorCategory, stage string, line, column int, message string) Diagnostic {
	return Diagnostic{Category: category, Stage: stage, Line: line, Column: column, Message: message}
}

// ErrorMessages renders an error into the message lines of a failed build. It
// traverses wrappers with errors.As, so a diagnostic wrapped by any stage still
// renders as itself rather than as an opaque message.
func ErrorMessages(err error) []string {
	if err == nil {
		return nil
	}
	var diagnostics Diagnostics
	if errors.As(err, &diagnostics) {
		messages := make([]string, 0, len(diagnostics))
		for _, diagnostic := range diagnostics {
			messages = append(messages, diagnostic.Error())
		}
		return messages
	}
	var diagnostic Diagnostic
	if errors.As(err, &diagnostic) {
		return []string{diagnostic.Error()}
	}
	return []string{err.Error()}
}

// StampModule attributes a stage error to a module's logical source key.
func StampModule(err error, logicalKey string) error {
	if err == nil {
		return nil
	}
	var diagnostics Diagnostics
	if errors.As(err, &diagnostics) {
		return diagnostics.InModule(logicalKey)
	}
	var diagnostic Diagnostic
	if errors.As(err, &diagnostic) {
		return diagnostic.InModule(logicalKey)
	}
	return err
}

// CompareDiagnostic orders two diagnostics by line, then column. It is the
// one position comparator shared by every stage that sorts diagnostics.
func CompareDiagnostic(a, b Diagnostic) int {
	if a.Line != b.Line {
		return a.Line - b.Line
	}
	return a.Column - b.Column
}

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
	parts := strings.Split(canonicalID, "/")
	for index, part := range parts {
		parts[index] = strconv.Itoa(len(part)) + "_" + part
	}
	return "m" + strings.Join(parts, "")
}

// ModuleHeaderGuard returns the include guard for a module header:
// "HEX_MODULE_" + encoded owner + "_H". Case-preserving; hexal.h keeps its
// own fixed guard.
func ModuleHeaderGuard(canonicalID string) string {
	return "HEX_MODULE_" + EncodeModuleOwner(canonicalID) + "_H"
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

// MutPtrType constructs or retrieves the canonical MutPtr<T> type of one
// element, whose pointee is writable.
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
		// Unknown exception that makes Ptr<Unknown>/MutPtr<Unknown> void*.
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
	typ := Type{
		Name:            constructor + "<" + element.Name + ">",
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
// ordered parameter list and optional result.
func (environment *Environment) FunType(parameters []Type, result *Type) Type {
	if environment == nil {
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
	canonicalKey := funKey(parameters, result)
	if cached, ok := environment.arena.funTypes[canonicalKey]; ok {
		return cached
	}
	name := funName(parameters, result)
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name: name,
		Signature: &FunSignature{
			Name:       name,
			Parameters: append([]Type(nil), parameters...),
			Result:     result,
		},
		CanonicalKey: canonicalKey,
		identity:     identity,
	}
	environment.arena.funTypes[canonicalKey] = typ
	return typ
}

func funKey(parameters []Type, result *Type) string {
	var builder strings.Builder
	builder.WriteString("fun:")
	for _, parameter := range parameters {
		builder.WriteString(parameter.CanonicalKey)
		builder.WriteString(",")
	}
	if result != nil {
		builder.WriteString(":")
		builder.WriteString(result.CanonicalKey)
	}
	return builder.String()
}

func funName(parameters []Type, result *Type) string {
	var builder strings.Builder
	builder.WriteString("Fun<(")
	for index, parameter := range parameters {
		if index > 0 {
			builder.WriteString(", ")
		}
		builder.WriteString(parameter.Name)
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

// IsRune reports whether typ is the canonical Rune type.
func IsRune(typ Type) bool { return typ.identity != nil && typ.identity == Rune.identity }

// IsError reports whether typ is the canonical Error type.
func IsError(typ Type) bool { return typ.identity != nil && typ.identity == ErrorType.identity }

// IsUnknown reports whether typ is the canonical Unknown type.
func IsUnknown(typ Type) bool { return typ.identity != nil && typ.identity == Unknown.identity }

// IsHeap reports whether typ is the canonical Heap type.
func IsHeap(typ Type) bool { return typ.identity != nil && typ.identity == Heap.identity }

// IsRuneCursor reports whether typ is the canonical RuneCursor descriptor
// type.
func IsRuneCursor(typ Type) bool {
	return typ.identity != nil && typ.identity == RuneCursorType.identity
}

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

// IsFloat reports whether typ is a float scalar.
func IsFloat(typ Type) bool { return typ.ScalarKind == ScalarFloat }

// IsCompleteValue reports whether typ names a value with a known layout:
// scalars, pointers, function pointers, objects, ADTs, arrays, and unions.
// Unknown and provisional objects are not complete values.
func IsCompleteValue(typ Type) bool {
	if typ.Object != nil {
		return !typ.Incomplete
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

// canonicalTypeState carries the tolerance flags and cycle guards of one
// canonicality validation pass.
type canonicalTypeState struct {
	allowProvisionalObjects bool
	allowTypeParameters     bool
	allowNilMember          bool // Nil is canonical only as a union member
	seenObjects             map[*typeIdentity]bool
	seenADTs                map[*typeIdentity]bool
}

// IsCanonical reports whether typ is fully canonical: every nested type is
// interned, forged copies are rejected, and recursive references resolve.
func IsCanonical(typ Type) bool {
	return isCanonicalForEnvironment(nil, typ, &canonicalTypeState{}, false)
}

func isCanonicalForEnvironment(environment *Environment, typ Type, state *canonicalTypeState, throughPointer bool) bool {
	if typ.identity == nil {
		return false
	}
	if IsNil(typ) {
		// Standalone Nil is invalid in every written position; union
		// construction is the sole Nil-admitting resolver.
		return state.allowNilMember
	}
	if typ.Generic != nil {
		return state.allowTypeParameters
	}
	if typ.Object != nil {
		return isCanonicalObject(environment, typ, state)
	}
	if typ.Adt != nil {
		return isCanonicalADT(environment, typ, state)
	}
	if typ.NullableBase != nil {
		return isCanonicalNullable(environment, typ, state)
	}
	if typ.Signature != nil {
		return isCanonicalFun(environment, typ, state)
	}
	if typ.Element != nil {
		return isCanonicalPointer(environment, typ, state)
	}
	if typ.Union != nil {
		return isCanonicalUnion(environment, typ, state)
	}
	if typ.Array != nil {
		return isCanonicalArray(environment, typ, state)
	}
	if typ.View != nil {
		return isCanonicalView(environment, typ, state)
	}
	if typ.List != nil {
		return isCanonicalList(environment, typ, state)
	}
	if typ.Dict != nil {
		return isCanonicalDict(environment, typ, state)
	}
	if typ.Task != nil {
		if typ.identity.signature != "task:"+typ.Task.Result.CanonicalKey {
			return false
		}
		return isCanonicalForEnvironment(environment, typ.Task.Result, state, false)
	}
	if typ.Channel != nil {
		if typ.identity.signature != "channel:"+typ.Channel.Element.CanonicalKey {
			return false
		}
		return isCanonicalForEnvironment(environment, typ.Channel.Element, state, false)
	}
	if typ.Mutex != nil {
		return typ.identity != nil && typ.identity == MutexType.identity
	}
	if typ.Atomic != nil {
		if typ.identity.signature != "atomic:"+typ.Atomic.Element.CanonicalKey {
			return false
		}
		return isCanonicalForEnvironment(environment, typ.Atomic.Element, state, false)
	}
	if typ.Stash != nil {
		if typ.identity.signature != "stash:"+typ.Stash.Element.CanonicalKey {
			return false
		}
		return isCanonicalForEnvironment(environment, typ.Stash.Element, state, false)
	}
	if typ.Pool != nil {
		if typ.identity.signature != "pool:"+typ.Pool.Element.CanonicalKey {
			return false
		}
		return isCanonicalForEnvironment(environment, typ.Pool.Element, state, false)
	}
	if IsUnknown(typ) {
		// Unknown is canonical only behind a pointer layer: the erased
		// object pointer types Ptr<Unknown> and MutPtr<Unknown>.
		return throughPointer
	}
	return isCanonicalScalar(environment, typ)
}

func isCanonicalObject(environment *Environment, typ Type, state *canonicalTypeState) bool {
	object := typ.Object
	// Completeness lives on the shared record, not the possibly stale copy
	// captured by an interner during provisional member resolution.
	if object.Incomplete && !state.allowProvisionalObjects {
		return false
	}
	if object.identity == nil || object.identity != typ.identity || typ.identity.object != typ.Object {
		return false
	}
	if state.seenObjects == nil {
		state.seenObjects = make(map[*typeIdentity]bool)
	}
	if state.seenObjects[object.identity] {
		return true
	}
	state.seenObjects[object.identity] = true
	for _, member := range object.Members {
		if !isCanonicalForEnvironment(environment, member.Type, state, false) {
			return false
		}
	}
	return true
}

func isCanonicalADT(environment *Environment, typ Type, state *canonicalTypeState) bool {
	adt := typ.Adt
	if len(adt.Variants) == 0 && !state.allowProvisionalObjects {
		return false
	}
	if adt.identity == nil || adt.identity != typ.identity || typ.identity.object != nil {
		return false
	}
	if state.seenADTs == nil {
		state.seenADTs = make(map[*typeIdentity]bool)
	}
	if state.seenADTs[adt.identity] {
		return true
	}
	state.seenADTs[adt.identity] = true
	for _, variant := range adt.Variants {
		for _, member := range variant.Payload {
			if !isCanonicalForEnvironment(environment, member.Type, state, false) {
				return false
			}
		}
	}
	return true
}

func isCanonicalNullable(environment *Environment, typ Type, state *canonicalTypeState) bool {
	base, ok := NullableBase(typ)
	if !ok || !IsPointerLike(base) {
		return false
	}
	if !isCanonicalForEnvironment(environment, base, state, true) {
		return false
	}
	return typ.CName == base.CName
}

func isCanonicalPointer(environment *Environment, typ Type, state *canonicalTypeState) bool {
	if typ.Element == nil {
		return false
	}
	constructor := "Ptr"
	if typ.PointeeWritable {
		constructor = "MutPtr"
	}
	if typ.identity.signature != constructor+":"+typ.Element.CanonicalKey {
		return false
	}
	return isCanonicalForEnvironment(environment, *typ.Element, state, true)
}

func isCanonicalFun(environment *Environment, typ Type, state *canonicalTypeState) bool {
	if typ.Signature == nil {
		return false
	}
	if typ.identity.signature != funKey(typ.Signature.Parameters, typ.Signature.Result) {
		return false
	}
	for _, parameter := range typ.Signature.Parameters {
		if !isCanonicalForEnvironment(environment, parameter, state, false) {
			return false
		}
	}
	if typ.Signature.Result != nil {
		return isCanonicalForEnvironment(environment, *typ.Signature.Result, state, false)
	}
	return true
}

func isCanonicalUnion(environment *Environment, typ Type, state *canonicalTypeState) bool {
	if typ.Union == nil || len(typ.Union.Members) < 2 {
		return false
	}
	// The C name is the registry-shaped spelling of the canonical members:
	// the base name, or the base followed by a numeric suffix when another
	// type already owns the base. A forged name cannot match that shape.
	base := unionBaseName(typ.Union.Members)
	if typ.CName != base {
		suffix := strings.TrimPrefix(typ.CName, base+"_")
		if suffix == typ.CName || !isNumericSuffix(suffix) {
			return false
		}
	}
	// With an environment the registry is reachable, so membership is the
	// stronger check: the name must be owned by this exact canonical union.
	if environment != nil {
		registered, ok := environment.arena.definitionNames[typ.CName]
		if !ok || !Equal(registered, typ) {
			return false
		}
	}
	// Nil is a legitimate canonical union member, so member validation
	// admits it; the parent union is what makes it valid.
	memberState := *state
	memberState.allowNilMember = true
	for _, member := range typ.Union.Members {
		if !isCanonicalForEnvironment(environment, member, &memberState, false) || !IsCompleteValue(member) {
			return false
		}
	}
	return true
}

// isNumericSuffix reports whether suffix is a decimal counter of at least one
// digit, the registry's disambiguator appended to a union base.
func isNumericSuffix(suffix string) bool {
	if suffix == "" {
		return false
	}
	for index := 0; index < len(suffix); index++ {
		if suffix[index] < '0' || suffix[index] > '9' {
			return false
		}
	}
	return true
}

func isCanonicalArray(environment *Environment, typ Type, state *canonicalTypeState) bool {
	if typ.Array == nil || typ.Array.Length == 0 || !Eligible(typ.Array.Element, PositionArrayElement) {
		return false
	}
	return isCanonicalForEnvironment(environment, typ.Array.Element, state, false)
}

func isCanonicalView(environment *Environment, typ Type, state *canonicalTypeState) bool {
	if typ.View == nil || typ.View.Element == (Type{}) || !Eligible(typ.View.Element, PositionViewElement) {
		return false
	}
	key := "view:" + typ.View.Element.CanonicalKey
	if typ.identity.signature != key {
		return false
	}
	return isCanonicalForEnvironment(environment, typ.View.Element, state, false)
}

func isCanonicalList(environment *Environment, typ Type, state *canonicalTypeState) bool {
	if typ.List == nil || typ.List.Element == (Type{}) || !Eligible(typ.List.Element, PositionListElement) {
		return false
	}
	key := "list:" + typ.List.Element.CanonicalKey
	if typ.identity.signature != key {
		return false
	}
	return isCanonicalForEnvironment(environment, typ.List.Element, state, false)
}

func isCanonicalDict(environment *Environment, typ Type, state *canonicalTypeState) bool {
	if typ.Dict == nil || typ.Dict.Key == (Type{}) || typ.Dict.Value == (Type{}) || !IsDictKey(typ.Dict.Key) || !Eligible(typ.Dict.Value, PositionDictValue) {
		return false
	}
	key := "dict:" + typ.Dict.Key.CanonicalKey + "," + typ.Dict.Value.CanonicalKey
	if typ.identity.signature != key {
		return false
	}
	return isCanonicalForEnvironment(environment, typ.Dict.Key, state, false) &&
		isCanonicalForEnvironment(environment, typ.Dict.Value, state, false)
}

func isCanonicalScalar(environment *Environment, typ Type) bool {
	if typ.Incomplete {
		return false
	}
	builtin, ok := builtinTypes[typ.Name]
	if !ok {
		return false
	}
	return builtin.CName == typ.CName && builtin.ScalarKind == typ.ScalarKind && builtin.Bits == typ.Bits
}

// IsProtectedTypeName reports whether a name is reserved by the language.
func IsProtectedTypeName(name string) bool {
	if _, ok := builtinTypes[name]; ok {
		return true
	}
	switch name {
	case "Ptr", "MutPtr", "Fun", "Array", "List", "Dict", "View", "Task", "Channel", "Atomic", "Stash", "Pool":
		return true
	}
	return false
}

// IsStrand reports whether typ is the canonical Strand type.
func IsStrand(typ Type) bool { return typ.identity != nil && typ.identity == StrandType.identity }

// IsSize reports whether typ is the canonical target-sized Size type.
func IsSize(typ Type) bool { return typ.identity != nil && typ.identity == SizeType.identity }

func scalarType(name, cName string, kind ScalarKind, bits int) Type {
	identity := newTypeIdentity()
	identity.signature = "scalar:" + name
	return Type{
		Name:         name,
		CName:        cName,
		CanonicalKey: name,
		ScalarKind:   kind,
		Bits:         bits,
		identity:     identity,
	}
}

// Package-level canonical builtins. Scalars share one package-level identity,
// so they compare equal across compilation environments.
var (
	Int8   = scalarType("Int8", "int8_t", ScalarSignedInteger, 8)
	Int16  = scalarType("Int16", "int16_t", ScalarSignedInteger, 16)
	Int32  = scalarType("Int32", "int32_t", ScalarSignedInteger, 32)
	Int64  = scalarType("Int64", "int64_t", ScalarSignedInteger, 64)
	UInt8  = scalarType("UInt8", "uint8_t", ScalarUnsignedInteger, 8)
	UInt16 = scalarType("UInt16", "uint16_t", ScalarUnsignedInteger, 16)
	UInt32 = scalarType("UInt32", "uint32_t", ScalarUnsignedInteger, 32)
	UInt64 = scalarType("UInt64", "uint64_t", ScalarUnsignedInteger, 64)
	// Rune is the spelling for a Unicode scalar value. It lowers to the
	// uint32_t scalar value set so text iteration and conversions share the
	// ordinary unsigned machinery.
	Rune    = scalarType("Rune", "uint32_t", ScalarUnsignedInteger, 32)
	Float32 = scalarType("Float32", "float", ScalarFloat, 32)
	Float64 = scalarType("Float64", "double", ScalarFloat, 64)
	Bool    = scalarType("Bool", "bool", ScalarBool, 1)

	// Int, Float, and UInt are the idiomatic aliases of the platform-native
	// widths.
	Int   = Int32
	Float = Float64
	UInt  = UInt32

	Nil = Type{
		Name:         "Nil",
		CName:        "nullptr_t",
		CanonicalKey: "Nil",
		identity:     newTypeIdentity(),
	}
	// EoS is the end-of-stream singleton. Its one-byte C value is never
	// allocated; the `T | EoS` result union carries it as a tag-only
	// alternative.
	EoS = Type{
		Name:         "EoS",
		CName:        "hex_eos",
		CanonicalKey: "EoS",
		identity:     newTypeIdentity(),
	}
	Unknown = Type{
		Name:         "Unknown",
		CName:        "void",
		CanonicalKey: "Unknown",
		Incomplete:   true,
		identity:     newTypeIdentity(),
	}
	Heap = Type{
		Name:         "Heap",
		CName:        "hex_heap",
		CanonicalKey: "Heap",
		identity:     newTypeIdentity(),
	}
	StringType = Type{
		Name:         "String",
		CName:        "hex_string",
		CanonicalKey: "String",
		identity:     newTypeIdentity(),
	}
	StrandType = Type{
		Name:         "Strand",
		CName:        "hex_strand",
		CanonicalKey: "Strand",
		identity:     newTypeIdentity(),
	}
	// SizeType is the target-sized unsigned integer corresponding to C's
	// size_t. It is a distinct canonical type even where its width matches
	// a fixed-width integer.
	SizeType = Type{
		Name:         "Size",
		CName:        "size_t",
		CanonicalKey: "Size",
		ScalarKind:   ScalarUnsignedInteger,
		Bits:         64,
		identity:     newTypeIdentity(),
	}
	// ErrorType is the built-in nominal error value: five fixed fields
	// recording the construction site and the program's category and
	// message. It is reserved: user source cannot redeclare or shadow it.
	ErrorType = errorType()
	// MutexType is the scheduler-aware mutual-exclusion handle. It is a
	// heap-backed, pointer-sized reference-like value with one canonical
	// identity, like String; its control block lives on the Heap passed to
	// Mutex.new.
	MutexType = Type{
		Name:         "Mutex",
		CName:        "hex_mutex",
		CanonicalKey: "Mutex",
		identity:     newTypeIdentity(),
	}
	// RuneCursorType is the non-owning UTF-8 cursor: one descriptor holding
	// the source byte pointer, byte length, and current byte offset. It is
	// an inline value with one canonical identity.
	RuneCursorType = Type{
		Name:         "RuneCursor",
		CName:        "hex_rune_cursor",
		CanonicalKey: "RuneCursor",
		identity:     newTypeIdentity(),
	}
)

// errorType constructs the canonical built-in Error object, linking its
// type identity to the shared object record like every interner does.
func errorType() Type {
	object := &ObjectType{
		Name:  "Error",
		CName: "hex_t_Error",
		Members: []ObjectMember{
			{Name: "file", Type: StringType},
			{Name: "line", Type: SizeType},
			{Name: "column", Type: SizeType},
			{Name: "header", Type: StrandType},
			{Name: "message", Type: StringType},
		},
	}
	identity := newTypeIdentity()
	identity.object = object
	object.identity = identity
	return Type{Name: "Error", CName: "hex_t_Error", CanonicalKey: "Error", Object: object, identity: identity}
}

// builtinTypes is the canonical registry of every builtin type name: scalars,
// Nil, Unknown, and Heap. Canonicality compares against these records.
var builtinTypes = map[string]Type{
	"Bool":    Bool,
	"Int8":    Int8,
	"Int16":   Int16,
	"Int32":   Int32,
	"Int64":   Int64,
	"UInt8":   UInt8,
	"UInt16":  UInt16,
	"UInt32":  UInt32,
	"UInt64":  UInt64,
	"Rune":    Rune,
	"Float32": Float32,
	"Float64": Float64,
	"Nil":     Nil,
	"EoS":     EoS,
	"Unknown": Unknown,
	"Heap":    Heap,
	"String":  StringType,
	"Strand":  StrandType,
	"Size":    SizeType,
	"Error":   ErrorType,
	"Mutex":   MutexType,
	// Byte is the canonical transparent alias of UInt8; both spellings
	// share one identity and one C representation.
	"Byte":       UInt8,
	"RuneCursor": RuneCursorType,
}

// Lookup resolves a builtin type by name.
func Lookup(name string) (Type, bool) {
	typ, ok := builtinTypes[name]
	return typ, ok
}
