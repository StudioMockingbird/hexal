package types

import (
	"strings"

	"hexal/compiler/config"
)

// Canonicality: validation that every nested type is interned in this
// compilation's environment, with forged copies, incomplete records, and
// stale signatures rejected.

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
		return isCanonicalObject(environment, typ, state, throughPointer)
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
	if typ.InlineString != nil {
		return typ.InlineString.Capacity >= 1 && typ.InlineString.Capacity <= config.MaxInlineStringCapacity &&
			typ.identity.signature == inlineStringKey(typ.InlineString.Capacity)
	}
	if typ.Slice != nil {
		return isCanonicalSlice(environment, typ, state)
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
		// object pointer types Ptr<Unknown> and Ptr<mut Unknown>.
		return throughPointer
	}
	if canonicalOpaqueTypes[typ.identity] {
		// The moved capability handles and value types are compiler-owned
		// canonical identities with no members to validate, but they are not
		// scalars and are no longer in the protected-name registry.
		return true
	}
	return isCanonicalScalar(environment, typ)
}

// canonicalOpaqueTypes is the identity set of compiler-owned capability types
// that carry no members and no scalar shape, so canonicality is identity
// alone. It is populated after every package-level type initializer runs.
var canonicalOpaqueTypes map[*typeIdentity]bool

func init() {
	canonicalOpaqueTypes = map[*typeIdentity]bool{
		IOType.identity:            true,
		BytesType.identity:         true,
		FileType.identity:          true,
		TcpConnectionType.identity: true,
		TcpListenerType.identity:   true,
		ProcessType.identity:       true,
		PipeType.identity:          true,
		SignalsType.identity:       true,
		DurationType.identity:      true,
		InstantType.identity:       true,
		WallTimeType.identity:      true,
	}
}

func isCanonicalObject(environment *Environment, typ Type, state *canonicalTypeState, throughPointer bool) bool {
	object := typ.Object
	// Completeness lives on the shared record, not the possibly stale copy
	// captured by an interner during provisional member resolution. An opaque
	// foreign record is canonical behind a pointer: generated C references the
	// C type and never defines, sizes, or copies it.
	if object.Incomplete && !state.allowProvisionalObjects && !(throughPointer && IsForeignRecord(typ)) {
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
	if typ.identity.signature != funKey(typ.Signature.Parameters, typ.Signature.Result, typ.Signature.Rest) {
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

func isCanonicalSlice(environment *Environment, typ Type, state *canonicalTypeState) bool {
	if typ.Slice == nil || typ.Slice.Element == (Type{}) || !Eligible(typ.Slice.Element, PositionSliceElement) {
		return false
	}
	key := "slice:" + typ.Slice.Element.CanonicalKey
	if typ.Slice.Writable {
		key = "slicemut:" + typ.Slice.Element.CanonicalKey
	}
	if typ.identity.signature != key {
		return false
	}
	return isCanonicalForEnvironment(environment, typ.Slice.Element, state, false)
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
