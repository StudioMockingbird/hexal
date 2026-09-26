package diagnostics

import "fmt"

func EntryEnvironmentFunctionNotCallableAsValue(name string) Message {
	return message("type.entry-environment-function-value", CategoryType, StageChecker,
		"function "+name+" uses the entry environment and is valid only as a direct entry-module call")
}

func GenericFunctionValueNeedsContext(name string) Message {
	return message("type.generic-function-value-needs-context", CategoryType, StageChecker, "cannot infer generic parameter for "+name)
}

func MethodIsNotValue(method, receiver string) Message {
	return message("type.method-is-not-value", CategoryType, StageChecker,
		fmt.Sprintf("%s is a method on %s; methods are not values", method, receiver))
}

func PropertyOnPointerRequiresDereference(property, receiver, place string) Message {
	return message("type.property-on-pointer-requires-dereference", CategoryType, StageChecker,
		fmt.Sprintf("cannot access .%s on %s; use ^%s to access the pointee", property, receiver, place))
}

func PropertyRequiresObjectMember(property, receiver string) Message {
	return message("type.property-requires-object-member", CategoryType, StageChecker,
		fmt.Sprintf("cannot access .%s on %s; expected Ptr<T> or an object member", property, receiver))
}

func ExpressionIsNotPlace() Message {
	return message("type.expression-is-not-place", CategoryType, StageChecker,
		"expression is not a place; assignment and @ require a variable, member, dereference, or index")
}

func QualifiedVariantRequiresPayload(owner, variant string) Message {
	return message("type.qualified-variant-requires-payload", CategoryType, StageChecker,
		fmt.Sprintf("%s.%s requires a payload", owner, variant))
}

func UnknownPointeeCannotBeDereferenced(pointer string) Message {
	return message("type.unknown-pointee-cannot-be-dereferenced", CategoryType, StageChecker,
		pointer+" cannot be dereferenced; recover a concrete pointer type first")
}

func NullableValueMustBeNarrowed(typeName string) Message {
	return message("type.nullable-value-must-be-narrowed", CategoryType, StageChecker,
		typeName+" may be Nil; narrow it before dereferencing")
}

func MemberPathCannotBeNarrowed(path string) Message {
	return message("type.member-path-cannot-be-narrowed", CategoryType, StageChecker,
		"only a local binding can be narrowed; bind "+path+" before testing it")
}

func DereferenceRequiresPointer(typeName string) Message {
	return message("type.dereference-requires-pointer", CategoryType, StageChecker,
		"cannot dereference "+typeName+"; ^ requires Ptr<T>")
}

func FunctionDeclarationNotAddressable(name string) Message {
	return message("type.function-declaration-not-addressable", CategoryType, StageChecker,
		"function declarations are not addressable; use "+name+" as a Fun value")
}

func FunctionBindingNotAddressable(typeName string) Message {
	return message("type.function-binding-not-addressable", CategoryType, StageChecker, typeName+" bindings are not addressable")
}

func SliceBindingNotAddressable() Message {
	return message("type.slice-binding-not-addressable", CategoryType, StageChecker, "@ cannot take the address of a Slice binding")
}

func MemberNotFound(typeName, member string) Message {
	return message("type.member-not-found", CategoryType, StageChecker, fmt.Sprintf("%s has no member %s", typeName, member))
}
