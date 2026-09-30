package types

import "hexal/compiler/specdata"

// std/json's data model: the compiler-owned Value union over Hexal containers
// and the ordered object Member record. Value recurses only through owning
// container handles (Array's List, Object's member List), so every variant
// value has a finite representation; a parsed tree owns everything it holds.

type jsonModeler struct {
	value      Type
	member     Type
	valueList  Type
	memberList Type
}

var jsonModelData = buildJSONModel()

// JsonValueType is the canonical std/json Value union; its C name is the one
// spelling the adapter and the std/json module share.
func JsonValueType() Type  { return jsonModelData.value }
func JsonMemberType() Type { return jsonModelData.member }

// IsJsonValue reports whether typ is the canonical std/json Value union.
func IsJsonValue(typ Type) bool {
	return typ.Adt != nil && typ.Adt == jsonModelData.value.Adt
}

// IsJsonMember reports whether typ is the canonical ordered Member record.
func IsJsonMember(typ Type) bool {
	return typ.identity != nil && typ.identity == jsonModelData.member.identity
}

// JsonValueListType is List<Value>, the Array payload's owning container.
func JsonValueListType() Type { return jsonModelData.valueList }

// JsonMemberListType is List<JsonValueMember>, the Object payload's list.
func JsonMemberListType() Type { return jsonModelData.memberList }

// signatureIdentityCarrier is one constructed identity whose signature is the
// given canonical key, the shape isCanonicalList's shape check verifies.
func signatureIdentityCarrier(canonicalKey string) *typeIdentity {
	identity := newTypeIdentity()
	identity.signature = canonicalKey
	return identity
}

// buildJSONModel ties the recursion in one place: the ADT and both owning
// containers exist before any payload member reads them, and the late variant
// write is visible through the shared ADT pointer.
func buildJSONModel() jsonModeler {
	adt := &AdtType{
		Name:  "Value",
		CName: "hex_t_JsonValue",
		Variants: []AdtVariant{
			{Name: "Null"},
		},
		identity: newTypeIdentity(),
	}
	self := Type{
		Name:         "Value",
		CName:        adt.CName,
		CanonicalKey: canonicalNominalKey("JsonValue", ""),
		Adt:          adt,
		identity:     adt.identity,
	}
	valueList := Type{
		Name:         "List<Value>",
		CName:        "hex_list_JsonValue",
		CanonicalKey: "list:JsonValue",
		List:         &ListInfo{Element: self},
		identity:     signatureIdentityCarrier("list:JsonValue"),
	}
	member := builtinObject("JsonValueMember", "hex_t_JsonValueMember", []ObjectMember{
		{Name: "name", Type: StringType, Use: NewTypeUse(StringType)},
		{Name: "value", Type: self, Use: NewTypeUse(self)},
	})
	memberList := Type{
		Name:         "List<JsonValueMember>",
		CName:        "hex_list_JsonValueMember",
		CanonicalKey: "list:JsonValueMember",
		List:         &ListInfo{Element: member},
		identity:     signatureIdentityCarrier("list:JsonValueMember"),
	}
	adt.Variants = []AdtVariant{
		{Name: "Null"},
		{Name: "Bool", Payload: []ObjectMember{{Name: "value", Type: Bool, Use: NewTypeUse(Bool)}}},
		{Name: "Int", Payload: []ObjectMember{{Name: "value", Type: Int64, Use: NewTypeUse(Int64)}}},
		{Name: "UInt", Payload: []ObjectMember{{Name: "value", Type: UInt64, Use: NewTypeUse(UInt64)}}},
		{Name: "Float", Payload: []ObjectMember{{Name: "value", Type: Float64, Use: NewTypeUse(Float64)}}},
		{Name: "Text", Payload: []ObjectMember{{Name: "value", Type: StringType, Use: NewTypeUse(StringType)}}},
		{Name: "Array", Payload: []ObjectMember{{Name: "items", Type: valueList, Use: NewTypeUse(valueList)}}},
		{Name: "Object", Payload: []ObjectMember{{Name: "entries", Type: memberList, Use: NewTypeUse(memberList)}}},
	}
	_ = specdata.TypeJsonValue
	return jsonModeler{value: self, member: member, valueList: valueList, memberList: memberList}
}
