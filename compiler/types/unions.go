package types

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// globalUnionTypes interns every canonical union across all environments of
// one process. Compilation is deterministic: the key is built from canonical
// member identity serials, and the C name depends only on the members, so
// the cache never changes generated text.
var (
	globalUnionTypes = make(map[string]Type)
	globalUnionMu    sync.Mutex
)

// UnionInfo is immutable metadata for one canonical structural union. Type
// stores a pointer so Type remains comparable while the member set remains
// compilation-scoped and order-independent.
type UnionInfo struct {
	Members []Type
}

// TypeUse keeps source-written candidate order separate from canonical Type
// identity. Nested constructor views preserve contextual order recursively.
type TypeUse struct {
	Type       Type
	Candidates []TypeUse
	Element    *TypeUse
	Parameters []TypeUse
	Result     *TypeUse
}

func NewTypeUse(typ Type) TypeUse { return TypeUse{Type: typ} }

func UnionTypeUse(typ Type, candidates []TypeUse) TypeUse {
	return TypeUse{Type: typ, Candidates: append([]TypeUse(nil), candidates...)}
}

func PointerTypeUse(typ Type, element TypeUse) TypeUse {
	return TypeUse{Type: typ, Element: &element}
}

func FunctionTypeUse(typ Type, parameters []TypeUse, result *TypeUse) TypeUse {
	return TypeUse{
		Type:       typ,
		Parameters: append([]TypeUse(nil), parameters...),
		Result:     result,
	}
}

// UnionType interns a normalized member set in this compilation. Nullable
// pointer unions reuse the existing null-niche descriptor instead of creating a
// tagged wrapper.
func (environment *Environment) UnionType(members []Type) Type {
	if environment == nil {
		return Type{}
	}
	flattened := make([]Type, 0, len(members))
	for _, member := range members {
		flattened = append(flattened, unionMembers(member)...)
	}
	unique := make([]Type, 0, len(flattened))
	for _, member := range flattened {
		// Open generic bodies build unions with placeholder members (such as
		// T | Nil); those members are validated after substitution and can
		// never pass canonical validation while they still contain a
		// placeholder.
		//
		// Owning pointer-sized handles (String, List, Dict, Task,
		// Channel, Mutex) and the read-only View descriptor are ordinary union
		// members: Error-carrying results return String | Error and
		// List<Byte> | Error. Atomic values are non-copyable and are rejected
		// here because union injection copies by definition (RFC 0046). Nil is
		// canonical only as a union member (RFC 0049 item 8.1), so member
		// validation admits it through allowNilMember.
		if !ContainsTypeParameter(member) &&
			(!isCanonicalForEnvironment(environment, member, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true, allowNilMember: true}, false) || !Storable(member, PositionUnionMember)) {
			return Type{}
		}
		found := false
		for _, existing := range unique {
			if Equal(existing, member) {
				found = true
				break
			}
		}
		if !found {
			unique = append(unique, member)
		}
	}
	// RFC 0049 item 8.1: a union holds at least two distinct canonical
	// members; the flatten-and-deduplicate pass runs first, and a written
	// union that collapses to fewer than two is an error, never an alias for
	// the surviving member.
	if len(unique) < 2 {
		return Type{}
	}
	sort.SliceStable(unique, func(left, right int) bool {
		return compareUnionMembers(unique[left], unique[right]) < 0
	})
	if len(unique) == 2 && IsNil(unique[1]) && IsPointerLike(unique[0]) {
		return environment.NullableType(unique[0])
	}
	key := unionKey(unique)
	// RFC 0034: a union is a compiler-owned builtin constructor, so its
	// identity is canonical and compilation-global: the same member set in
	// any module yields the same Type, one C name, and one generated
	// definition. The key is built from canonical member identity serials,
	// so it never depends on which module wrote the union first.
	globalUnionMu.Lock()
	defer globalUnionMu.Unlock()
	if cached, ok := globalUnionTypes[key]; ok {
		return cached
	}
	info := &UnionInfo{Members: append([]Type(nil), unique...)}
	union := Type{
		Name:     unionName(unique),
		CName:    unionCName(unique),
		Union:    info,
		identity: newTypeIdentity(environment.identity),
	}
	globalUnionTypes[key] = union
	return union
}

// unionCName derives one union's C name from its canonical members: a
// deterministic, injective, length-delimited encoding of the member C names.
// The name depends only on the members, so the same union written in any
// module spells the same C type and different unions never collide (RFC
// 0034: built-in specialization identity depends only on the constructor and
// its canonical arguments, never on the requesting module).
func unionCName(members []Type) string {
	var builder strings.Builder
	builder.WriteString("hex_union_")
	for _, member := range members {
		builder.WriteString(strconv.Itoa(len(member.CName)))
		builder.WriteString("_")
		builder.WriteString(member.CName)
	}
	return builder.String()
}

func unionMembers(typ Type) []Type {
	if typ.Union != nil {
		return append([]Type(nil), typ.Union.Members...)
	}
	if base, ok := NullableBase(typ); ok {
		return []Type{base, Nil}
	}
	return []Type{typ}
}

func unionKey(members []Type) string {
	parts := make([]string, len(members))
	for index, member := range members {
		parts[index] = strconv.FormatUint(member.identity.serial, 10)
	}
	return strings.Join(parts, ",")
}

func unionName(members []Type) string {
	names := make([]string, len(members))
	for index, member := range members {
		names[index] = member.Name
	}
	return strings.Join(names, " | ")
}

var builtinUnionOrder = map[string]int{
	"Bool":    0,
	"UInt8":   1,
	"UInt16":  2,
	"UInt32":  3,
	"UInt64":  4,
	"Int8":    5,
	"Int16":   6,
	"Int32":   7,
	"Int64":   8,
	"Float32": 9,
	"Float64": 10,
}

func compareUnionMembers(left, right Type) int {
	leftRank, leftKey := unionDisplayKey(left)
	rightRank, rightKey := unionDisplayKey(right)
	if leftRank != rightRank {
		if leftRank < rightRank {
			return -1
		}
		return 1
	}
	if leftKey < rightKey {
		return -1
	}
	if leftKey > rightKey {
		return 1
	}
	return 0
}

func unionDisplayKey(typ Type) (int, string) {
	if IsNil(typ) {
		return 4, ""
	}
	if rank, ok := builtinUnionOrder[typ.Name]; ok {
		return 0, fmt.Sprintf("%02d", rank)
	}
	if typ.Object != nil {
		return 1, fmt.Sprintf("%09d:%09d:%s", typ.Object.SourceLine, typ.Object.SourceColumn, typ.Object.Name)
	}
	if typ.Element != nil {
		constructor := "MutPtr"
		if !typ.PointeeWritable {
			constructor = "Ptr"
		}
		return 2, constructor + ":" + typ.Element.Name
	}
	if typ.Signature != nil {
		return 3, typ.Name
	}
	return 3, typ.Name
}

// IsUnion includes the specialized nullable representation because RFC 0014
// treats P | Nil as an ordinary union with a specialized representation.
func IsUnion(typ Type) bool { return typ.Union != nil || IsNullable(typ) }

func UnionMembers(typ Type) []Type { return unionMembers(typ) }

func ContainsUnionMember(union, member Type) bool {
	if !IsUnion(union) {
		return false
	}
	for _, candidate := range unionMembers(union) {
		if Equal(candidate, member) {
			return true
		}
	}
	return false
}

func RemoveUnionMember(environment *Environment, union, member Type) (Type, bool) {
	if !IsUnion(union) {
		return Type{}, false
	}
	remaining := make([]Type, 0, len(unionMembers(union)))
	removed := false
	for _, candidate := range unionMembers(union) {
		if Equal(candidate, member) {
			removed = true
			continue
		}
		remaining = append(remaining, candidate)
	}
	if !removed || len(remaining) == 0 {
		return Type{}, false
	}
	if len(remaining) == 1 {
		// Narrowing, not written union syntax: RFC 0049 item 8.1's
		// two-distinct-member rule does not apply to a value already proven
		// to hold the surviving member.
		return remaining[0], true
	}
	return environment.UnionType(remaining), true
}
