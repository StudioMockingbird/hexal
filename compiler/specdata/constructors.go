package specdata

import "fmt"

// Type identity records for compiler-owned types. A TypeID is primitive: this
// package imports no compiler package, so a type crosses the boundary as an
// identifier, never as a compiler Type. compiler/types resolves an identifier
// back to its interned Type through ResolveSpecID.
//
// Records describe type constructors, never specializations. String<N> makes a
// per-specialization registry impossible because N is unbounded, so a record
// carries only the facts equal across every specialization; a C name, layout,
// and size are derived from the arguments instead of stored.

// TypeID identifies one compiler-owned type constructor or one concrete
// compiler-owned type. Constructors and concrete types share one identifier
// space, so a reference can never name an identity the registry does not know.
type TypeID string

// Concrete compiler-owned type identifiers: the identities ResolveSpecID
// resolves to an interned Type. Scalar aliases are separate identifiers so a
// source spelling resolves even though it shares one canonical Type with its
// target.
const (
	TypeBool            TypeID = "Bool"
	TypeInt8            TypeID = "Int8"
	TypeInt16           TypeID = "Int16"
	TypeInt32           TypeID = "Int32"
	TypeInt64           TypeID = "Int64"
	TypeUInt8           TypeID = "UInt8"
	TypeUInt16          TypeID = "UInt16"
	TypeUInt32          TypeID = "UInt32"
	TypeUInt64          TypeID = "UInt64"
	TypeByte            TypeID = "Byte"
	TypeInt             TypeID = "Int"
	TypeUInt            TypeID = "UInt"
	TypeRune            TypeID = "Rune"
	TypeFloat32         TypeID = "Float32"
	TypeFloat64         TypeID = "Float64"
	TypeFloat           TypeID = "Float"
	TypeSize            TypeID = "Size"
	TypeNil             TypeID = "Nil"
	TypeEoS             TypeID = "EoS"
	TypeUnknown         TypeID = "Unknown"
	TypeHeap            TypeID = "Heap"
	TypeString          TypeID = "String"
	TypeError           TypeID = "Error"
	TypeMutex           TypeID = "Mutex"
	TypeByteCursor      TypeID = "ByteCursor"
	TypeRuneCursor      TypeID = "RuneCursor"
	TypeGrapheme        TypeID = "Grapheme"
	TypeGraphemeCursor  TypeID = "GraphemeCursor"
	TypeErrorKind       TypeID = "ErrorKind"
	TypeNormalization   TypeID = "NormalizationForm"
	TypeUnicodeCategory TypeID = "UnicodeCategory"
)

// Core-library type identifiers. Each aliases the canonical export identifier
// its module publishes, so the two spellings cannot drift apart.
const (
	TypeIO                  TypeID = TypeID(CoreTypeIO)
	TypeBytes               TypeID = TypeID(CoreTypeBytes)
	TypeSeek                TypeID = TypeID(CoreTypeSeek)
	TypeFile                TypeID = TypeID(CoreTypeFile)
	TypeFileMode            TypeID = TypeID(CoreTypeFileMode)
	TypeDuration            TypeID = TypeID(CoreTypeDuration)
	TypeInstant             TypeID = TypeID(CoreTypeInstant)
	TypeWallTime            TypeID = TypeID(CoreTypeWallTime)
	TypeAddress             TypeID = TypeID(CoreTypeAddress)
	TypeTcpConnection       TypeID = TypeID(CoreTypeTcpConnection)
	TypeTcpListener         TypeID = TypeID(CoreTypeTcpListener)
	TypeProcess             TypeID = TypeID(CoreTypeProcess)
	TypePipe                TypeID = TypeID(CoreTypePipe)
	TypeProcessOptions      TypeID = TypeID(CoreTypeProcessOptions)
	TypeStartedProcess      TypeID = TypeID(CoreTypeStartedProcess)
	TypeEnvironment         TypeID = TypeID(CoreTypeEnvironment)
	TypeEnvironmentVariable TypeID = TypeID(CoreTypeEnvironmentVariable)
	TypeProcessStream       TypeID = TypeID(CoreTypeProcessStream)
	TypeExitStatus          TypeID = TypeID(CoreTypeExitStatus)
	TypeSignal              TypeID = TypeID(CoreTypeSignal)
	TypeSignals             TypeID = TypeID(CoreTypeSignals)
	TypeTerminalSize        TypeID = TypeID(CoreTypeTerminalSize)
)

// The parameterized compiler-owned type constructors. Each identifier names a
// family; a specialization such as List<Int32> is identified by its arguments,
// never by an identifier of its own.
const (
	TypeArray        TypeID = "Array"
	TypeInlineString TypeID = "InlineString"
	TypeSlice        TypeID = "Slice"
	TypeList         TypeID = "List"
	TypeDict         TypeID = "Dict"
	TypeTask         TypeID = "Task"
	TypeChannel      TypeID = "Channel"
	TypeAtomic       TypeID = "Atomic"
	TypeStash        TypeID = "Stash"
	TypePool         TypeID = "Pool"
)

// concreteTypeIDs lists every identifier ResolveSpecID resolves to an interned
// Type. It is a slice so Validate reports a repeated identifier as a
// source-tree defect instead of the compiler refusing an identical map key
// before the check runs.
var concreteTypeIDs = []TypeID{
	TypeBool, TypeInt8, TypeInt16, TypeInt32, TypeInt64,
	TypeUInt8, TypeUInt16, TypeUInt32, TypeUInt64,
	TypeByte, TypeInt, TypeUInt, TypeRune,
	TypeFloat32, TypeFloat64, TypeFloat, TypeSize,
	TypeNil, TypeEoS, TypeUnknown, TypeHeap, TypeString, TypeError, TypeMutex,
	TypeByteCursor, TypeRuneCursor, TypeGrapheme, TypeGraphemeCursor,
	TypeErrorKind, TypeNormalization, TypeUnicodeCategory,
	TypeIO, TypeBytes, TypeSeek, TypeFile, TypeFileMode,
	TypeDuration, TypeInstant, TypeWallTime,
	TypeAddress, TypeTcpConnection, TypeTcpListener,
	TypeProcess, TypePipe, TypeProcessOptions, TypeStartedProcess,
	TypeEnvironment, TypeEnvironmentVariable, TypeProcessStream, TypeExitStatus,
	TypeSignal, TypeSignals, TypeTerminalSize,
}

// Representation classifies how a specialization's value is stored, the rule
// that decides passing and copying. Ownership, not C shape, selects it: String
// and Slice share a pointer-length struct but String owns bytes and Slice
// borrows them, so String is a handle and Slice a value.
type Representation uint8

const (
	// RepresentationValue stores the whole value at the use site; a copy
	// copies its region or descriptor.
	RepresentationValue Representation = iota
	// RepresentationHandle is a pointer-sized reference whose copies alias
	// one allocation.
	RepresentationHandle
)

// CopyMode classifies how a specialization's representation is copied.
type CopyMode uint8

const (
	// CopyValue copies the value's own bytes or descriptor, so source and
	// copy share no owned state.
	CopyValue CopyMode = iota
	// CopyShallow copies a handle, so source and copy alias one allocation.
	CopyShallow
	// CopyUnavailable marks a value with no copy operation, such as Atomic<T>.
	CopyUnavailable
)

// FreeMode classifies whether a specialization owns storage a caller releases.
type FreeMode uint8

const (
	// FreeNone releases nothing.
	FreeNone FreeMode = iota
	// FreeOwned owns storage released through free, or through Stash/Pool
	// reset and destroy in place of free.
	FreeOwned
)

// ComparisonMode classifies equality and ordering eligibility.
type ComparisonMode uint8

const (
	// ComparisonNone has neither equality nor ordering.
	ComparisonNone ComparisonMode = iota
	// ComparisonEquality supports == and != only.
	ComparisonEquality
	// ComparisonOrdered supports equality plus <, <=, >, and >=.
	ComparisonOrdered
)

// ConstructorFacts are the facts equal across every specialization of one type
// constructor: representation, copy and free behavior, and comparison
// eligibility. A C name, layout, size, and element eligibility vary by
// argument and are never stored here.
//
// Runtime-component demand is also invariant, but the component identity it
// would name is owned by the runtime-component registry; restating it here
// would give one fact two owners.
type ConstructorFacts struct {
	Representation Representation
	CopyMode       CopyMode
	FreeMode       FreeMode
	Comparable     ComparisonMode
	Hashable       bool
}

// ParamKind classifies one type-constructor parameter.
type ParamKind uint8

const (
	// ParamType is a type parameter, such as List's element.
	ParamType ParamKind = iota
	// ParamInteger is an integer parameter, such as String<N>'s capacity or
	// Array's length.
	ParamInteger
)

// TypeConstructorSpec is one type constructor. Params lists its parameters in
// written order; a method owner refers to a parameter by that position.
type TypeConstructorSpec struct {
	ID         TypeID
	SourceName string
	Params     []ParamKind
	Facts      ConstructorFacts
}

// typeConstructors is the registry. It is unexported so no importer can rewrite
// a record, and every query clones the parameter slice it returns.
var typeConstructors = []TypeConstructorSpec{
	{
		ID:         TypeArray,
		SourceName: "Array",
		Params:     []ParamKind{ParamType, ParamInteger},
		Facts:      ConstructorFacts{Representation: RepresentationValue, CopyMode: CopyValue, FreeMode: FreeNone, Comparable: ComparisonEquality},
	},
	{
		ID:         TypeInlineString,
		SourceName: "String",
		Params:     []ParamKind{ParamInteger},
		Facts:      ConstructorFacts{Representation: RepresentationValue, CopyMode: CopyValue, FreeMode: FreeNone, Comparable: ComparisonEquality, Hashable: true},
	},
	{
		ID:         TypeSlice,
		SourceName: "Slice",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationValue, CopyMode: CopyValue, FreeMode: FreeNone, Comparable: ComparisonEquality},
	},
	{
		ID:         TypeList,
		SourceName: "List",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparable: ComparisonNone},
	},
	{
		ID:         TypeDict,
		SourceName: "Dict",
		Params:     []ParamKind{ParamType, ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparable: ComparisonNone},
	},
	{
		ID:         TypeTask,
		SourceName: "Task",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeNone, Comparable: ComparisonNone},
	},
	{
		ID:         TypeChannel,
		SourceName: "Channel",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparable: ComparisonNone},
	},
	{
		ID:         TypeAtomic,
		SourceName: "Atomic",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationValue, CopyMode: CopyUnavailable, FreeMode: FreeNone, Comparable: ComparisonNone},
	},
	{
		ID:         TypeStash,
		SourceName: "Stash",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparable: ComparisonNone},
	},
	{
		ID:         TypePool,
		SourceName: "Pool",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparable: ComparisonNone},
	},
}

// TypeConstructor resolves one type constructor by identifier.
func TypeConstructor(id TypeID) (TypeConstructorSpec, bool) {
	for _, spec := range typeConstructors {
		if spec.ID == id {
			return cloneTypeConstructor(spec), true
		}
	}
	return TypeConstructorSpec{}, false
}

// TypeConstructors returns every constructor record in registration order as a
// copy.
func TypeConstructors() []TypeConstructorSpec {
	specs := make([]TypeConstructorSpec, len(typeConstructors))
	for index, spec := range typeConstructors {
		specs[index] = cloneTypeConstructor(spec)
	}
	return specs
}

// cloneTypeConstructor deep-copies the parameter slice a record owns, so a
// query result shares no backing array with the registry.
func cloneTypeConstructor(spec TypeConstructorSpec) TypeConstructorSpec {
	spec.Params = append([]ParamKind(nil), spec.Params...)
	return spec
}

// isConcreteTypeID reports whether id names a concrete compiler-owned type,
// the identities ResolveSpecID can resolve.
func isConcreteTypeID(id TypeID) bool {
	for _, candidate := range concreteTypeIDs {
		if candidate == id {
			return true
		}
	}
	return false
}

// isConstructorTypeID reports whether id names a type constructor family.
func isConstructorTypeID(id TypeID) bool {
	for _, spec := range typeConstructors {
		if spec.ID == id {
			return true
		}
	}
	return false
}

// validateConstructors checks the type-constructor registry, then delegates to
// the method registry, because a method's parameter references are meaningful
// only against the constructor it names.
func validateConstructors() error {
	seen := make(map[TypeID]bool, len(typeConstructors))
	for _, spec := range typeConstructors {
		if spec.ID == "" {
			return fmt.Errorf("specdata/constructors: constructor has an empty id")
		}
		if seen[spec.ID] {
			return fmt.Errorf("specdata/constructors: constructor %q is declared twice", spec.ID)
		}
		seen[spec.ID] = true
		if spec.SourceName == "" {
			return fmt.Errorf("specdata/constructors: constructor %q has an empty source name", spec.ID)
		}
		for index, param := range spec.Params {
			if param != ParamType && param != ParamInteger {
				return fmt.Errorf("specdata/constructors: constructor %q parameter %d has unknown kind", spec.ID, index)
			}
		}
		if spec.Facts.Representation != RepresentationValue && spec.Facts.Representation != RepresentationHandle {
			return fmt.Errorf("specdata/constructors: constructor %q has unknown representation", spec.ID)
		}
		if spec.Facts.CopyMode != CopyValue && spec.Facts.CopyMode != CopyShallow && spec.Facts.CopyMode != CopyUnavailable {
			return fmt.Errorf("specdata/constructors: constructor %q has unknown copy mode", spec.ID)
		}
		if spec.Facts.FreeMode != FreeNone && spec.Facts.FreeMode != FreeOwned {
			return fmt.Errorf("specdata/constructors: constructor %q has unknown free mode", spec.ID)
		}
		if spec.Facts.Comparable != ComparisonNone && spec.Facts.Comparable != ComparisonEquality && spec.Facts.Comparable != ComparisonOrdered {
			return fmt.Errorf("specdata/constructors: constructor %q has unknown comparison mode", spec.ID)
		}
	}
	// A concrete identifier must not also be a constructor identifier: the two
	// share one space, so a collision would make an owner reference ambiguous.
	for _, id := range concreteTypeIDs {
		if seen[id] {
			return fmt.Errorf("specdata/constructors: %q names both a concrete type and a constructor", id)
		}
	}
	return validateMethods()
}
