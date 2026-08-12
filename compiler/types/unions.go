package types

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
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
		// Owning pointer-sized handles (String, List, Dict, Stream, Task,
		// Channel, Mutex) are ordinary union members: RFC 0040's File reads
		// return String | Error and List<Byte> | Error. Borrowed Views stay
		// rejected because a copied temporary View could make its data
		// pointer escape the actual storage.
		if !ContainsTypeParameter(member) &&
			(!isCanonicalForEnvironment(environment, member, &canonicalTypeState{allowProvisionalObjects: true, allowTypeParameters: true}, false) || !IsCompleteValue(member) || member.View != nil) {
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
	if len(unique) == 0 {
		return Type{}
	}
	sort.SliceStable(unique, func(left, right int) bool {
		return compareUnionMembers(unique[left], unique[right]) < 0
	})
	if len(unique) == 1 {
		return unique[0]
	}
	if len(unique) == 2 && IsNil(unique[1]) && IsPointerLike(unique[0]) {
		return environment.NullableType(unique[0])
	}
	key := unionKey(unique)
	if cached, ok := environment.unionTypes[key]; ok {
		return cached
	}
	info := &UnionInfo{Members: append([]Type(nil), unique...)}
	union := Type{
		Name:     unionName(unique),
		CName:    fmt.Sprintf("hex_internal_union_%d", environment.unionOrdinal+1),
		Union:    info,
		identity: newTypeIdentity(environment.identity),
	}
	environment.unionOrdinal++
	environment.unionTypes[key] = union
	return union
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
	return environment.UnionType(remaining), true
}
