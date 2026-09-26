package diagnostics

import "fmt"

func NoUnionMemberAccepts(typeName string) Message {
	return message("type.no-union-member-accepts", CategoryType, StageChecker,
		fmt.Sprintf("no member of %s accepts this expression", typeName))
}

func IsRequiresExactMemberType(typeName string) Message {
	return message("type.is-requires-exact-member-type", CategoryType, StageChecker,
		fmt.Sprintf("is requires one exact member type; %s is a union", typeName))
}

func IsCannotTestNil() Message {
	return message("type.is-cannot-test-nil", CategoryType, StageChecker, "is may not test Nil; use == nil or != nil")
}

func IsRequiresUnionValue(actual string) Message {
	return message("type.is-requires-union", CategoryType, StageChecker,
		"is requires a union value; got "+actual)
}

func CannotNarrowAfterMutableAddressEscapes(name string) Message {
	return message("type.cannot-narrow-after-address-escape", CategoryType, StageChecker,
		name+" cannot be narrowed after its mutable address escapes")
}

func IsTestTypeNotMember(query, actual string) Message {
	return message("type.is-test-type-not-member", CategoryType, StageChecker,
		fmt.Sprintf("%s is not a member of %s", query, actual))
}

func RedundantNilIsTest(query, actual string) Message {
	return message("type.redundant-nil-is-test", CategoryType, StageChecker,
		fmt.Sprintf("is test of %s against %s is redundant; use != nil", query, actual))
}

func UnionEqualityRequiresSameTypes(left, right string) Message {
	return message("type.union-equality-requires-same-types", CategoryType, StageChecker,
		fmt.Sprintf("union equality requires identical operand types; got %s and %s", left, right))
}

func UnionMemberDoesNotSupportEquality(name string) Message {
	return message("type.union-member-no-equality", CategoryType, StageChecker,
		"union member "+name+" does not support equality")
}
