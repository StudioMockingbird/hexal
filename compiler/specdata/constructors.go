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

// TypeFun is the structural function identity. It has no interned Type:
// ResolveSpecID never resolves it, and a function Type reaches it through its
// Signature field. It is recorded so the function placement and comparison
// facts have one owner instead of a Signature check repeated in each consumer.
const TypeFun TypeID = "Fun"

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

// ComparisonForm classifies how a compiler-owned type's equality is decided.
// It is a form, not a per-type verdict, because equality is structural: a
// List<T> is equality-comparable exactly when T is, so one enum value per
// constructor cannot express the fact.
type ComparisonForm uint8

const (
	// ComparisonNever has no equality: == and != are always unavailable.
	ComparisonNever ComparisonForm = iota
	// ComparisonAlways supports == and != regardless of any component.
	ComparisonAlways
	// ComparisonStructural supports == and != exactly when every component
	// the value stores is itself equality-available.
	ComparisonStructural
)

// PositionMask is a bit set over the compiler's storing positions. Bit i is
// set when the type may occupy position index i; the index order is the shared
// position model's declaration order, and a compiler-side test pins the two
// orders together.
type PositionMask uint16

// The position bits, in the shared position model's declaration order.
const (
	StorableBinding PositionMask = 1 << iota
	StorableObjectMember
	StorableADTPayload
	StorableUnionMember
	StorableArrayElement
	StorableSliceElement
	StorableListElement
	StorableDictValue
	StorableFunctionParam
	StorableFunctionResult
	StorableTaskArgument
	StorableTaskResult
	StorableChannelElement
	StorablePointee
	StorableHeapAllocation
)

// The named position sets the registry records. StorableEverywhere is the
// default a complete, finitely sized value gets unless a rule excludes it.
const (
	// StorableEverywhere is every position.
	StorableEverywhere PositionMask = StorableBinding | StorableObjectMember | StorableADTPayload | StorableUnionMember | StorableArrayElement | StorableSliceElement | StorableListElement | StorableDictValue | StorableFunctionParam | StorableFunctionResult | StorableTaskArgument | StorableTaskResult | StorableChannelElement | StorablePointee | StorableHeapAllocation
	// StorableNowhere is no position.
	StorableNowhere PositionMask = 0
	// StorableConstructionOnly is the in-place construction positions.
	StorableConstructionOnly PositionMask = StorableBinding | StorableObjectMember
	// StorableUnionMemberOnly is Nil's single position.
	StorableUnionMemberOnly PositionMask = StorableUnionMember
	// StorableFunction is the function-value placement set: everywhere
	// except a pointer pointee and a heap allocation.
	StorableFunction PositionMask = StorableBinding | StorableObjectMember | StorableADTPayload | StorableUnionMember | StorableArrayElement | StorableSliceElement | StorableListElement | StorableDictValue | StorableFunctionParam | StorableFunctionResult | StorableTaskArgument | StorableTaskResult | StorableChannelElement
	// StorableIO is the stream-descriptor placement set: the short-lived
	// positions a borrowed descriptor may cross, plus a pointer pointee.
	StorableIO PositionMask = StorableBinding | StorableUnionMember | StorableFunctionParam | StorableFunctionResult | StorableTaskArgument | StorableTaskResult | StorablePointee
	// StorableBytes is the memory-stream placement set: StorableIO without
	// the Task positions.
	StorableBytes PositionMask = StorableBinding | StorableUnionMember | StorableFunctionParam | StorableFunctionResult | StorablePointee
)

// Allows reports whether the mask admits one position index.
func (mask PositionMask) Allows(position uint8) bool {
	return mask&(PositionMask(1)<<position) != 0
}

// ConstructorFacts are the facts equal across every specialization of one type
// constructor: representation, copy and free behavior, comparison eligibility,
// the storable positions, and the runtime component the constructor demands. A
// C name, layout, size, and element eligibility vary by argument and are never
// stored here.
//
// Component is the constructor's demand, not a restatement of a component fact:
// the runtime-component registry records each component's own files,
// dependencies, and headers, and never which constructors pull it.
//
// Managed is not Representation. Representation follows ownership, so Slice is
// a value and String a handle; yet both, with List and Dict, are excluded as
// pointer pointees because each carries its own aliasing and invalidation rules
// over borrowed or allocated storage. Managed records that exclusion.
type ConstructorFacts struct {
	Representation Representation
	CopyMode       CopyMode
	FreeMode       FreeMode
	Comparison     ComparisonForm
	Ordered        bool
	Hashable       bool
	Managed        bool
	Positions      PositionMask
	Component      ComponentID
}

// TypeFacts returns the subset of facts a concrete or structural identity is
// consumed for: comparison, ordering, pointee exclusion, and storable
// positions. A concrete record stores only this subset, because a concrete
// identity is never consumed for representation, copy, or free behavior.
func (facts ConstructorFacts) TypeFacts() TypeFacts {
	return TypeFacts{
		Comparison: facts.Comparison,
		Ordered:    facts.Ordered,
		Managed:    facts.Managed,
		Positions:  facts.Positions,
	}
}

// TypeFacts are the facts consumed for one concrete or structural
// compiler-owned identity. A concrete identity with every default fact needs no
// record: the resolver reports it absent and the consumer applies the default.
type TypeFacts struct {
	Comparison ComparisonForm
	Ordered    bool
	Managed    bool
	Positions  PositionMask
}

// ConcreteTypeSpec is one concrete or structural compiler-owned identity that
// carries a non-default fact. It has no parameters, so unlike a
// TypeConstructorSpec it stores no parameter list and no constructibility.
type ConcreteTypeSpec struct {
	ID    TypeID
	Facts TypeFacts
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
	// Constructible marks a constructor the language writes as a bare
	// Name(...) call and the checker's canonical-constructor dispatch accepts.
	// A constructor reachable only through another syntax (an Array literal, a
	// Slice bridge, spawn, an inline-string conversion) leaves it false.
	Constructible bool
}

// typeConstructors is the registry. It is unexported so no importer can rewrite
// a record, and every query clones the parameter slice it returns.
var typeConstructors = []TypeConstructorSpec{
	{
		ID:         TypeArray,
		SourceName: "Array",
		Params:     []ParamKind{ParamType, ParamInteger},
		Facts:      ConstructorFacts{Representation: RepresentationValue, CopyMode: CopyValue, FreeMode: FreeNone, Comparison: ComparisonStructural, Positions: StorableEverywhere, Component: ComponentArray},
	},
	{
		ID:         TypeInlineString,
		SourceName: "String",
		Params:     []ParamKind{ParamInteger},
		Facts:      ConstructorFacts{Representation: RepresentationValue, CopyMode: CopyValue, FreeMode: FreeNone, Comparison: ComparisonAlways, Ordered: true, Hashable: true, Positions: StorableEverywhere, Component: ComponentString},
	},
	{
		ID:         TypeSlice,
		SourceName: "Slice",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationValue, CopyMode: CopyValue, FreeMode: FreeNone, Comparison: ComparisonStructural, Managed: true, Positions: StorableEverywhere, Component: ComponentSlice},
	},
	{
		ID:            TypeList,
		SourceName:    "List",
		Params:        []ParamKind{ParamType},
		Facts:         ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparison: ComparisonStructural, Managed: true, Positions: StorableEverywhere, Component: ComponentList},
		Constructible: true,
	},
	{
		ID:            TypeDict,
		SourceName:    "Dict",
		Params:        []ParamKind{ParamType, ParamType},
		Facts:         ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparison: ComparisonNever, Managed: true, Positions: StorableEverywhere, Component: ComponentDict},
		Constructible: true,
	},
	{
		ID:         TypeTask,
		SourceName: "Task",
		Params:     []ParamKind{ParamType},
		Facts:      ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeNone, Comparison: ComparisonNever, Positions: StorableEverywhere, Component: ComponentConcurrency},
	},
	{
		ID:            TypeChannel,
		SourceName:    "Channel",
		Params:        []ParamKind{ParamType},
		Facts:         ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparison: ComparisonNever, Positions: StorableEverywhere, Component: ComponentConcurrency},
		Constructible: true,
	},
	{
		ID:            TypeAtomic,
		SourceName:    "Atomic",
		Params:        []ParamKind{ParamType},
		Facts:         ConstructorFacts{Representation: RepresentationValue, CopyMode: CopyUnavailable, FreeMode: FreeNone, Comparison: ComparisonNever, Positions: StorableConstructionOnly, Component: ComponentConcurrency},
		Constructible: true,
	},
	{
		ID:            TypeStash,
		SourceName:    "Stash",
		Params:        []ParamKind{ParamType},
		Facts:         ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparison: ComparisonNever, Positions: StorableEverywhere, Component: ComponentStash},
		Constructible: true,
	},
	{
		ID:            TypePool,
		SourceName:    "Pool",
		Params:        []ParamKind{ParamType},
		Facts:         ConstructorFacts{Representation: RepresentationHandle, CopyMode: CopyShallow, FreeMode: FreeOwned, Comparison: ComparisonNever, Positions: StorableEverywhere, Component: ComponentPool},
		Constructible: true,
	},
}

// concreteFacts is the registry of concrete and structural compiler-owned
// identities whose facts are not the default. A concrete identity with every
// default fact (a scalar, Mutex, a core object or ADT) is deliberately absent:
// the resolver reports it absent and the consumer applies the default, so the
// absence is itself the recorded default rather than a missing record.
var concreteFacts = []ConcreteTypeSpec{
	{ID: TypeNil, Facts: TypeFacts{Comparison: ComparisonAlways, Positions: StorableUnionMemberOnly}},
	{ID: TypeUnknown, Facts: TypeFacts{Comparison: ComparisonNever, Positions: StorableNowhere}},
	{ID: TypeFun, Facts: TypeFacts{Comparison: ComparisonNever, Positions: StorableFunction}},
	{ID: TypeHeap, Facts: TypeFacts{Comparison: ComparisonNever, Positions: StorableEverywhere}},
	{ID: TypeString, Facts: TypeFacts{Comparison: ComparisonAlways, Ordered: true, Managed: true, Positions: StorableEverywhere}},
	{ID: TypeIO, Facts: TypeFacts{Comparison: ComparisonNever, Positions: StorableIO}},
	{ID: TypeBytes, Facts: TypeFacts{Comparison: ComparisonNever, Positions: StorableBytes}},
}

// Facts resolves one compiler-owned identity to the facts a concrete or
// structural consumer reads. It searches the constructor registry and the
// concrete registry, so a caller never needs to know which kind the identifier
// names.
func Facts(id TypeID) (TypeFacts, bool) {
	if spec, ok := TypeConstructor(id); ok {
		return spec.Facts.TypeFacts(), true
	}
	for _, spec := range concreteFacts {
		if spec.ID == id {
			return spec.Facts, true
		}
	}
	return TypeFacts{}, false
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

// bareConstructibleConcrete lists the concrete compiler-owned types whose
// canonical construction is the bare Name(...) call the checker dispatches even
// though they are complete values, not parameterized constructors. The
// parameterized constructors carry the same fact on their own record.
var bareConstructibleConcrete = []TypeID{TypeHeap, TypeMutex, TypeError}

// BareConstructible reports whether name is a compiler-owned canonical
// constructor written as a bare Name(...) call. It spans both the parameterized
// constructors, whose record carries the fact, and the concrete types above,
// which have no constructor record. The checker's bare-constructor dispatch
// reads this instead of restating the set, so removing a record removes that
// construction form from accepted programs.
func BareConstructible(name string) bool {
	for _, spec := range typeConstructors {
		if spec.SourceName == name {
			return spec.Constructible
		}
	}
	for _, id := range bareConstructibleConcrete {
		if string(id) == name {
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
	names := make(map[string]bool, len(typeConstructors))
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
		// BareConstructible resolves a source name to one record, so a
		// repeated name would silently shadow the later constructor.
		if names[spec.SourceName] {
			return fmt.Errorf("specdata/constructors: source name %q is declared twice", spec.SourceName)
		}
		names[spec.SourceName] = true
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
		if spec.Facts.Comparison != ComparisonNever && spec.Facts.Comparison != ComparisonAlways && spec.Facts.Comparison != ComparisonStructural {
			return fmt.Errorf("specdata/constructors: constructor %q has unknown comparison form", spec.ID)
		}
		if spec.Facts.Positions&^StorableEverywhere != 0 {
			return fmt.Errorf("specdata/constructors: constructor %q has a position bit outside the position model", spec.ID)
		}
		if _, known := Component(spec.Facts.Component); !known {
			return fmt.Errorf("specdata/constructors: constructor %q demands unknown component %q", spec.ID, spec.Facts.Component)
		}
	}
	// A concrete identifier must not also be a constructor identifier: the two
	// share one space, so a collision would make an owner reference ambiguous.
	for _, id := range concreteTypeIDs {
		if seen[id] {
			return fmt.Errorf("specdata/constructors: %q names both a concrete type and a constructor", id)
		}
	}
	concreteSeen := make(map[TypeID]bool, len(concreteFacts))
	for _, spec := range concreteFacts {
		if spec.ID == "" {
			return fmt.Errorf("specdata/constructors: concrete record has an empty id")
		}
		if concreteSeen[spec.ID] {
			return fmt.Errorf("specdata/constructors: concrete record %q is declared twice", spec.ID)
		}
		concreteSeen[spec.ID] = true
		// TypeFun is a structural identity with no interned Type; every
		// other concrete record must name an identity ResolveSpecID resolves.
		if spec.ID != TypeFun && !isConcreteTypeID(spec.ID) {
			return fmt.Errorf("specdata/constructors: concrete record %q names no concrete type", spec.ID)
		}
		if spec.Facts.Comparison != ComparisonNever && spec.Facts.Comparison != ComparisonAlways && spec.Facts.Comparison != ComparisonStructural {
			return fmt.Errorf("specdata/constructors: concrete record %q has unknown comparison form", spec.ID)
		}
		if spec.Facts.Positions&^StorableEverywhere != 0 {
			return fmt.Errorf("specdata/constructors: concrete record %q has a position bit outside the position model", spec.ID)
		}
	}
	return validateMethods(methods)
}
