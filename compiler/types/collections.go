package types

import "strconv"

// ArrayInfo is the metadata of one fixed inline array type.
type ArrayInfo struct {
	Element Type
	Length  uint64
}

// ViewInfo is the metadata of one non-owning contiguous view type.
type ViewInfo struct {
	Element Type
}

// ListInfo is the metadata of one owning growable list type.
type ListInfo struct {
	Element Type
}

// DictInfo is the metadata of one owning dictionary type.
type DictInfo struct {
	Key   Type
	Value Type
}

// TaskInfo is the metadata of one spawned task handle type.
type TaskInfo struct {
	Result Type
}

// ChannelInfo is the metadata of one bounded channel handle type.
type ChannelInfo struct {
	Element Type
}

// MutexInfo is the metadata of the scheduler-aware mutex handle type.
type MutexInfo struct{}

// AtomicInfo is the metadata of one inline atomic wrapper type.
type AtomicInfo struct {
	Element Type
}

// IsList reports whether typ is an owning growable list.
func IsList(typ Type) bool { return typ.List != nil }

// IsDict reports whether typ is an owning dictionary.
func IsDict(typ Type) bool { return typ.Dict != nil }

// isManaged reports whether typ is a reference-like handle value rejected
// from inline positions, storage, and union alternatives. Views are
// the borrowed form; String, List, and Dict are owning forms.
func isManaged(typ Type) bool {
	return typ.View != nil || IsString(typ) || IsList(typ) || IsDict(typ)
}

// IsMutex reports whether typ is the canonical scheduler-aware Mutex handle.
func IsMutex(typ Type) bool { return typ.identity != nil && typ.identity == MutexType.identity }

// IsString reports whether typ is the canonical String type.
func IsString(typ Type) bool { return typ.identity != nil && typ.identity == StringType.identity }

// ArrayType constructs or retrieves the canonical Array<T, N> type for one
// element and positive length.
func (environment *Environment) ArrayType(element Type, length uint64) Type {
	if environment == nil || length == 0 ||
		!isCanonicalForEnvironment(environment, element, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!Eligible(element, PositionArrayElement) {
		return Type{}
	}
	canonicalKey := "array:" + element.CanonicalKey + "," + strconv.FormatUint(length, 10)
	if cached, ok := environment.arena.arrayTypes[canonicalKey]; ok {
		return cached
	}
	name := "Array<" + element.Name + ", " + strconv.FormatUint(length, 10) + ">"
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name:         name,
		CName:        environment.arena.uniqueCollectionCName("hex_array_"+SanitizeIdentifier(element.Name)+"_"+strconv.FormatUint(length, 10), element),
		CanonicalKey: canonicalKey,
		Array:        &ArrayInfo{Element: element, Length: length},
		identity:     identity,
	}
	environment.arena.arrayTypes[canonicalKey] = typ
	return typ
}

// ViewType constructs or retrieves the canonical View<T> type of one inline
// element. Views of managed or other view elements are rejected: a view may
// never expose or copy an owning payload.
func (environment *Environment) ViewType(element Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, element, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!Eligible(element, PositionViewElement) {
		return Type{}
	}
	canonicalKey := "view:" + element.CanonicalKey
	if cached, ok := environment.arena.viewTypes[canonicalKey]; ok {
		return cached
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name:         "View<" + element.Name + ">",
		CName:        environment.arena.uniqueCollectionCName("hex_view_"+SanitizeIdentifier(element.Name), element),
		CanonicalKey: canonicalKey,
		View:         &ViewInfo{Element: element},
		identity:     identity,
	}
	environment.arena.viewTypes[canonicalKey] = typ
	return typ
}

// ListType constructs or retrieves the canonical List<T> type of one
// collection element: an inline element or a direct String.
func (environment *Environment) ListType(element Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, element, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!Eligible(element, PositionListElement) {
		return Type{}
	}
	canonicalKey := "list:" + element.CanonicalKey
	if cached, ok := environment.arena.listTypes[canonicalKey]; ok {
		return cached
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name:         "List<" + element.Name + ">",
		CName:        environment.arena.uniqueCollectionCName("hex_list_"+SanitizeIdentifier(element.Name), element),
		CanonicalKey: canonicalKey,
		List:         &ListInfo{Element: element},
		identity:     identity,
	}
	environment.arena.listTypes[canonicalKey] = typ
	return typ
}

// UnionContainsEoS reports whether a union type includes EoS as one of its
// normalized top-level members.
func UnionContainsEoS(typ Type) bool {
	members := UnionMembers(typ)
	for index := 0; index < members.Len(); index++ {
		if member, _ := members.At(index); IsEoS(member) {
			return true
		}
	}
	return false
}

// Position is one storing position in the shared position model. Storage
// restrictions are stated against an explicit position set so no aggregate or
// generic specialization can bypass one by accident.
type Position int

// The concrete Position values, named for the storing position they
// classify.
const (
	PositionBinding Position = iota
	PositionObjectMember
	PositionADTPayload
	PositionUnionMember
	PositionArrayElement
	PositionViewElement
	PositionListElement
	PositionDictValue
	PositionFunctionParam
	PositionFunctionResult
	PositionTaskArgument
	PositionTaskResult
	PositionChannelElement
	PositionPointee
	PositionHeapAllocation
)

func isConstructionPosition(position Position) bool {
	return position == PositionBinding || position == PositionObjectMember
}

// Storable reports whether typ may occupy position: complete and finite, not
// Unknown, not an unspecialized type parameter, and Fun only in Binding,
// UnionMember, or FunctionParam. Atomic is storable only in a construction
// position (Binding or ObjectMember); every other position acquires its value
// by copying and is governed by the separate Copyable rule. Nil is storable
// only as a union member.
func Storable(typ Type, position Position) bool {
	if !IsCompleteValue(typ) || IsUnknown(typ) || ContainsTypeParameter(typ) {
		return false
	}
	if IsNil(typ) {
		return position == PositionUnionMember
	}
	if typ.Signature != nil {
		switch position {
		case PositionBinding, PositionUnionMember, PositionFunctionParam, PositionFunctionResult,
			PositionObjectMember, PositionADTPayload, PositionArrayElement, PositionViewElement,
			PositionListElement, PositionDictValue, PositionTaskArgument, PositionTaskResult,
			PositionChannelElement:
			return true
		default:
			return false
		}
	}
	if typ.Atomic != nil && !isConstructionPosition(position) {
		return false
	}
	// Stream bootstrap restriction: IO may cross a Task boundary as a
	// shallow copy; Bytes borrows its List and cannot. Neither survives in
	// long-lived aggregate storage while the shallow-copy alias model is
	// the only lifetime rule. Pointer receivers stay formable because the
	// Bytes operation surface is defined on MutPtr<Bytes>.
	if IsIO(typ) {
		switch position {
		case PositionBinding, PositionUnionMember, PositionFunctionParam,
			PositionFunctionResult, PositionTaskArgument, PositionTaskResult,
			PositionPointee:
			return true
		default:
			return false
		}
	}
	if IsBytes(typ) {
		switch position {
		case PositionBinding, PositionUnionMember, PositionFunctionParam,
			PositionFunctionResult, PositionPointee:
			return true
		default:
			return false
		}
	}
	return true
}

// Eligible reports whether a concrete element may be stored at position: it
// must be storable and copyable. An open type parameter defers to
// specialization rechecking and is always eligible here. This is the shared
// position model every compiler stage consults.
func Eligible(element Type, position Position) bool {
	return ContainsTypeParameter(element) || (Storable(element, position) && !ContainsAtomic(element))
}

// ContainsAtomic reports whether typ contains an inline Atomic<T> value
// recursively through objects, ADTs, arrays, and unions. Atomic values cannot
// be copied, so any shallow-copy position that reaches one is invalid. It
// stops at every indirection: copying a Ptr<T>, MutPtr<T>, or a handle copies
// the pointer, never the pointee.
func ContainsAtomic(typ Type) bool {
	if typ.Atomic != nil {
		return true
	}
	if typ.Object != nil {
		for _, member := range typ.Object.Members {
			if ContainsAtomic(member.Type) {
				return true
			}
		}
	}
	if typ.Adt != nil {
		for _, variant := range typ.Adt.Variants {
			for _, member := range variant.Payload {
				if ContainsAtomic(member.Type) {
					return true
				}
			}
		}
	}
	if typ.Array != nil {
		return ContainsAtomic(typ.Array.Element)
	}
	if typ.Union != nil || typ.NullableBase != nil {
		members := UnionMembers(typ)
		for index := 0; index < members.Len(); index++ {
			member, _ := members.At(index)
			if ContainsAtomic(member) {
				return true
			}
		}
	}
	return false
}

// TaskType constructs or retrieves the canonical Task<R> handle type. R must
// be a complete, shallow-copyable value: the runtime stores R in the task's
// result frame and join() copies it out. Inline Atomic values and aggregates
// containing one are excluded because they cannot be copied.
func (environment *Environment) TaskType(result Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, result, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!Eligible(result, PositionTaskResult) {
		return Type{}
	}
	canonicalKey := "task:" + result.CanonicalKey
	if cached, ok := environment.arena.taskTypes[canonicalKey]; ok {
		return cached
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name:         "Task<" + result.Name + ">",
		CName:        environment.arena.uniqueCollectionCName("hex_task_"+SanitizeIdentifier(result.Name), result),
		CanonicalKey: canonicalKey,
		Task:         &TaskInfo{Result: result},
		identity:     identity,
	}
	environment.arena.taskTypes[canonicalKey] = typ
	return typ
}

// ChannelType constructs or retrieves the canonical Channel<T> handle type. T
// must be complete, finite-sized, and shallow-copyable; EoS and a normalized
// top-level union containing EoS are rejected because a produced eos would be
// indistinguishable from closed-and-drained completion. Task, Channel, and
// Mutex handles may be sent like any other pointer-sized value.
func (environment *Environment) ChannelType(element Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, element, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		IsEoS(element) || UnionContainsEoS(element) ||
		!Eligible(element, PositionChannelElement) {
		return Type{}
	}
	canonicalKey := "channel:" + element.CanonicalKey
	if cached, ok := environment.arena.channelTypes[canonicalKey]; ok {
		return cached
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name:         "Channel<" + element.Name + ">",
		CName:        environment.arena.uniqueCollectionCName("hex_channel_"+SanitizeIdentifier(element.Name), element),
		CanonicalKey: canonicalKey,
		Channel:      &ChannelInfo{Element: element},
		identity:     identity,
	}
	environment.arena.channelTypes[canonicalKey] = typ
	return typ
}

// isAtomicElement reports whether element is a supported Atomic payload:
// Bool, Int32, UInt32, Int64, UInt64, or Size.
func isAtomicElement(element Type) bool {
	return Equal(element, Bool) || Equal(element, Int32) || Equal(element, UInt32) ||
		Equal(element, Int64) || Equal(element, UInt64) || Equal(element, SizeType)
}

// AtomicType constructs or retrieves the canonical inline Atomic<T> wrapper
// over C23 _Atomic(T). Only the six payload scalars are accepted.
func (environment *Environment) AtomicType(element Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, element, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!isAtomicElement(element) {
		return Type{}
	}
	canonicalKey := "atomic:" + element.CanonicalKey
	if cached, ok := environment.arena.atomicTypes[canonicalKey]; ok {
		return cached
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name:         "Atomic<" + element.Name + ">",
		CName:        environment.arena.uniqueCollectionCName("hex_atomic_"+SanitizeIdentifier(element.Name), element),
		CanonicalKey: canonicalKey,
		Atomic:       &AtomicInfo{Element: element},
		identity:     identity,
	}
	environment.arena.atomicTypes[canonicalKey] = typ
	return typ
}

// IsDictKey reports whether typ may be a dictionary key: exactly Int32
// or Strand.
func IsDictKey(typ Type) bool {
	return Equal(typ, Int32) || IsStrand(typ)
}

// DictType constructs or retrieves the canonical Dict<K, V> type of one key
// and one collection-element value. Only Int32 and Strand keys are valid.
func (environment *Environment) DictType(key, value Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, key, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!IsDictKey(key) ||
		!isCanonicalForEnvironment(environment, value, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!Eligible(value, PositionDictValue) {
		return Type{}
	}
	canonicalKey := "dict:" + key.CanonicalKey + "," + value.CanonicalKey
	if cached, ok := environment.arena.dictTypes[canonicalKey]; ok {
		return cached
	}
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	typ := Type{
		Name:         "Dict<" + key.Name + ", " + value.Name + ">",
		CName:        environment.arena.uniqueCollectionCName("hex_dict_"+SanitizeIdentifier(key.Name)+"_"+SanitizeIdentifier(value.Name), value),
		CanonicalKey: canonicalKey,
		Dict:         &DictInfo{Key: key, Value: value},
		identity:     identity,
	}
	environment.arena.dictTypes[canonicalKey] = typ
	return typ
}
