package diagnostics

import "fmt"

func RestBackedSliceCannotEscape() Message {
	return message("type.rest-backed-slice-cannot-escape", CategoryType, StageChecker,
		"rest-backed Slice cannot escape its function invocation")
}

func RestBackedSliceRequiresFixedAlias() Message {
	return message("type.rest-backed-slice-requires-fixed-alias", CategoryType, StageChecker,
		"rest-backed Slice requires a fixed local alias")
}

func TypeIsNotConstructible(name string) Message {
	return message("type.not-constructible", CategoryType, StageChecker, name+" is not a constructible type")
}

func ExpectedTypeGot(expected, actual string) Message {
	return message("type.expected-got", CategoryType, StageChecker,
		fmt.Sprintf("expected %s; got %s", expected, actual))
}

func TypeHasNoMember(typeName, member string) Message {
	return message("type.no-such-member", CategoryType, StageChecker,
		fmt.Sprintf("%s has no member %s", typeName, member))
}

func DuplicateConstructorMember(typeName, member string) Message {
	return message("type.duplicate-constructor-member", CategoryType, StageChecker,
		fmt.Sprintf("%s constructor initializes member %s more than once", typeName, member))
}

func MissingConstructorMember(typeName, member string) Message {
	return message("type.missing-constructor-member", CategoryType, StageChecker,
		fmt.Sprintf("%s constructor is missing member %s", typeName, member))
}

func NilRequiresExpectedNullableUnion() Message {
	return message("type.nil-requires-expected-nullable-union", CategoryType, StageChecker,
		"nil requires an expected union containing Nil")
}

func StringInterpolationRequiresAPI() Message {
	return message("type.string-interpolation-requires-api", CategoryType, StageChecker,
		"string interpolation requires String.interpolate(heap, template)")
}
