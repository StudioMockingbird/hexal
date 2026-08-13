package types

import (
	"fmt"
	"strconv"
)

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

// StreamInfo is the metadata of one lazy pull stream type (RFC 0031).
type StreamInfo struct {
	Element Type
}

// TaskInfo describes one spawned task handle type (RFC 0037).
type TaskInfo struct {
	Result Type
}

// ChannelInfo describes one bounded channel handle type (RFC 0037).
type ChannelInfo struct {
	Element Type
}

// MutexInfo is the metadata of the scheduler-aware mutex handle type.
type MutexInfo struct{}

// AtomicInfo describes one inline atomic wrapper type (RFC 0037).
type AtomicInfo struct {
	Element Type
}

// IsArray reports whether typ is a fixed inline array.
func IsArray(typ Type) bool { return typ.Array != nil }

// IsView reports whether typ is a non-owning contiguous view.
func IsView(typ Type) bool { return typ.View != nil }

// IsList reports whether typ is an owning growable list.
func IsList(typ Type) bool { return typ.List != nil }

// IsDict reports whether typ is an owning dictionary.
func IsDict(typ Type) bool { return typ.Dict != nil }

// IsManaged reports whether typ is a reference-like handle value rejected
// from inline positions, storage, and union alternatives in v1. Views are
// the borrowed form; String, List, Dict, and Stream are owning forms.
func IsManaged(typ Type) bool {
	return typ.View != nil || IsString(typ) || IsList(typ) || IsDict(typ) || IsStream(typ)
}

// IsStream reports whether typ is a Stream<T> handle.
func IsStream(typ Type) bool { return typ.Stream != nil }

// IsTask reports whether typ is a Task<R> handle (RFC 0037).
func IsTask(typ Type) bool { return typ.Task != nil }

// IsChannel reports whether typ is a Channel<T> handle (RFC 0037).
func IsChannel(typ Type) bool { return typ.Channel != nil }

// IsMutex reports whether typ is the canonical scheduler-aware Mutex handle.
func IsMutex(typ Type) bool { return typ.identity != nil && typ.identity == MutexType.identity }

// IsAtomic reports whether typ is an inline Atomic<T> wrapper (RFC 0037).
func IsAtomic(typ Type) bool { return typ.Atomic != nil }

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
	key := arrayKey(element, length)
	if cached, ok := environment.arrayTypes[key]; ok {
		return cached
	}
	name := "Array<" + element.Name + ", " + strconv.FormatUint(length, 10) + ">"
	array := &ArrayInfo{Element: element, Length: length}
	typ := Type{
		Name:     name,
		CName:    "hex_array_" + SanitizeIdentifier(element.Name) + "_" + strconv.FormatUint(length, 10),
		Array:    array,
		identity: newTypeIdentity(environment.identity),
	}
	environment.arrayTypes[key] = typ
	return typ
}

func arrayKey(element Type, length uint64) string {
	if element.identity == nil {
		return ""
	}
	return fmt.Sprintf("%d,%d", element.identity.serial, length)
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
	key := "view:" + strconv.FormatUint(element.identity.serial, 10)
	if cached, ok := environment.viewTypes[key]; ok {
		return cached
	}
	identity := newTypeIdentity(environment.identity)
	identity.signature = key
	typ := Type{
		Name:     "View<" + element.Name + ">",
		CName:    "hex_view_" + SanitizeIdentifier(element.Name),
		View:     &ViewInfo{Element: element},
		identity: identity,
	}
	environment.viewTypes[key] = typ
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
	key := "list:" + strconv.FormatUint(element.identity.serial, 10)
	if cached, ok := environment.listTypes[key]; ok {
		return cached
	}
	identity := newTypeIdentity(environment.identity)
	identity.signature = key
	typ := Type{
		Name:     "List<" + element.Name + ">",
		CName:    "hex_list_" + SanitizeIdentifier(element.Name),
		List:     &ListInfo{Element: element},
		identity: identity,
	}
	environment.listTypes[key] = typ
	return typ
}

// StreamType constructs or retrieves the canonical Stream<T> type. The
// element must be complete and finite-sized, and it must not be EoS or a
// union containing EoS as a top-level member: the produced-value and
// completion alternatives would be indistinguishable (RFC 0031).
func (environment *Environment) StreamType(element Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, element, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!Eligible(element, PositionStreamElement) || IsEoS(element) || UnionContainsEoS(element) {
		return Type{}
	}
	key := "stream:" + strconv.FormatUint(element.identity.serial, 10)
	if cached, ok := environment.streamTypes[key]; ok {
		return cached
	}
	identity := newTypeIdentity(environment.identity)
	identity.signature = key
	typ := Type{
		Name:     "Stream<" + element.Name + ">",
		CName:    "hex_stream_" + SanitizeIdentifier(element.Name),
		Stream:   &StreamInfo{Element: element},
		identity: identity,
	}
	environment.streamTypes[key] = typ
	return typ
}

// UnionContainsEoS reports whether a union type includes EoS as one of its
// normalized top-level members.
func UnionContainsEoS(typ Type) bool {
	for _, member := range UnionMembers(typ) {
		if IsEoS(member) {
			return true
		}
	}
	return false
}

// Position is one storing position from RFC 0046's position model. Storage
// restrictions are stated against an explicit position set so no aggregate or
// generic specialization can bypass one by accident.
type Position int

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
	PositionStreamElement
	PositionStreamState
	PositionPointee
	PositionHeapAllocation
)

func isConstructionPosition(position Position) bool {
	return position == PositionBinding || position == PositionObjectMember
}

// Storable reports whether typ may occupy position under RFC 0046 item 2:
// complete and finite, not Unknown, not an unspecialized type parameter, and
// Fun only in Binding, UnionMember, or FunctionParam. Atomic is storable only
// in a construction position (Binding or ObjectMember); every other position
// acquires its value by copying and is governed by the separate Copyable rule.
// Nil is storable only as a union member (RFC 0049 item 8.1).
func Storable(typ Type, position Position) bool {
	if !IsCompleteValue(typ) || IsUnknown(typ) || ContainsTypeParameter(typ) {
		return false
	}
	if IsNil(typ) {
		return position == PositionUnionMember
	}
	if typ.Signature != nil {
		return position == PositionBinding || position == PositionUnionMember || position == PositionFunctionParam
	}
	if typ.Atomic != nil && !isConstructionPosition(position) {
		return false
	}
	return true
}

// Eligible reports whether a concrete element may be stored at position: it
// must be storable and copyable. An open type parameter defers to
// specialization rechecking and is always eligible here. This is the shared
// position model every compiler stage consults (RFC 0046, RFC 0049 item 8.4).
func Eligible(element Type, position Position) bool {
	return ContainsTypeParameter(element) || (Storable(element, position) && !ContainsAtomic(element))
}

// ContainsAtomic reports whether typ contains an inline Atomic<T> value
// recursively through objects, ADTs, arrays, and unions. Atomic values cannot
// be copied, so any shallow-copy position that reaches one is invalid (RFC
// 0037, RFC 0046). It stops at every indirection: copying a Ptr<T>, MutPtr<T>,
// or a handle copies the pointer, never the pointee.
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
		for _, member := range UnionMembers(typ) {
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
	key := "task:" + strconv.FormatUint(result.identity.serial, 10)
	if cached, ok := environment.taskTypes[key]; ok {
		return cached
	}
	identity := newTypeIdentity(environment.identity)
	identity.signature = key
	typ := Type{
		Name:     "Task<" + result.Name + ">",
		CName:    "hex_task_" + SanitizeIdentifier(result.Name),
		Task:     &TaskInfo{Result: result},
		identity: identity,
	}
	environment.taskTypes[key] = typ
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
	key := "channel:" + strconv.FormatUint(element.identity.serial, 10)
	if cached, ok := environment.channelTypes[key]; ok {
		return cached
	}
	identity := newTypeIdentity(environment.identity)
	identity.signature = key
	typ := Type{
		Name:     "Channel<" + element.Name + ">",
		CName:    "hex_channel_" + SanitizeIdentifier(element.Name),
		Channel:  &ChannelInfo{Element: element},
		identity: identity,
	}
	environment.channelTypes[key] = typ
	return typ
}

// isAtomicElement reports whether element is a supported v1 Atomic payload:
// Bool, Int32, UInt32, Int64, UInt64, or Size (RFC 0037).
func isAtomicElement(element Type) bool {
	return Equal(element, Bool) || Equal(element, Int32) || Equal(element, UInt32) ||
		Equal(element, Int64) || Equal(element, UInt64) || Equal(element, SizeType)
}

// AtomicType constructs or retrieves the canonical inline Atomic<T> wrapper
// over C23 _Atomic(T). Only the six v1 payload scalars are accepted.
func (environment *Environment) AtomicType(element Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, element, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!isAtomicElement(element) {
		return Type{}
	}
	key := "atomic:" + strconv.FormatUint(element.identity.serial, 10)
	if cached, ok := environment.atomicTypes[key]; ok {
		return cached
	}
	identity := newTypeIdentity(environment.identity)
	identity.signature = key
	typ := Type{
		Name:     "Atomic<" + element.Name + ">",
		CName:    "hex_atomic_" + SanitizeIdentifier(element.Name),
		Atomic:   &AtomicInfo{Element: element},
		identity: identity,
	}
	environment.atomicTypes[key] = typ
	return typ
}

// IsDictKey reports whether typ may be a dictionary key in v1: exactly
// Int32 or Strand.
func IsDictKey(typ Type) bool {
	return Equal(typ, Int32) || IsStrand(typ)
}

// DictType constructs or retrieves the canonical Dict<K, V> type of one key
// and one collection-element value. Only Int32 and Strand keys are valid in
// v1.
func (environment *Environment) DictType(key, value Type) Type {
	if environment == nil ||
		!isCanonicalForEnvironment(environment, key, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!IsDictKey(key) ||
		!isCanonicalForEnvironment(environment, value, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) ||
		!Eligible(value, PositionDictValue) {
		return Type{}
	}
	keyName := "dict:" + strconv.FormatUint(key.identity.serial, 10) + "," + strconv.FormatUint(value.identity.serial, 10)
	if cached, ok := environment.dictTypes[keyName]; ok {
		return cached
	}
	identity := newTypeIdentity(environment.identity)
	identity.signature = keyName
	typ := Type{
		Name:     "Dict<" + key.Name + ", " + value.Name + ">",
		CName:    "hex_dict_" + SanitizeIdentifier(key.Name) + "_" + SanitizeIdentifier(value.Name),
		Dict:     &DictInfo{Key: key, Value: value},
		identity: identity,
	}
	environment.dictTypes[keyName] = typ
	return typ
}
