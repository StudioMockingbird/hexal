package diagnostics

import "fmt"

func FreeFunctionCollidesWithMethod(function, method string) Message {
	return message("type.free-function-method-collision", CategoryType, StageChecker, fmt.Sprintf("free function %s collides with method %s", function, method))
}
func MethodCollidesWithMethod(owner, method, existing string) Message {
	return message("type.method-name-collision", CategoryType, StageChecker, fmt.Sprintf("method %s.%s collides with method %s", owner, method, existing))
}
func NullableMemberNotCallable(name string) Message {
	return message("type.nullable-member-not-callable", CategoryType, StageChecker, name+" may be Nil; narrow it before calling it")
}
func MemberNotCallable(name string) Message {
	return message("type.member-not-callable", CategoryType, StageChecker, name+" is not callable")
}
func MemberCallType(name, typ string) Message {
	return message("type.member-call-type", CategoryType, StageChecker, fmt.Sprintf("member %s is not callable; its type is %s", name, typ))
}
func MethodReceiverMustBeStruct(actual string) Message {
	return message("type.method-receiver-must-be-struct", CategoryType, StageChecker, "method receiver must be a struct type; got "+actual)
}
func CannotDeclareMethodsForImportedType(name string) Message {
	return message("type.method-on-imported-type", CategoryType, StageChecker, "cannot declare methods for imported type "+name)
}
func TypeAlreadyHasMethod(owner, name string) Message {
	return message("type.duplicate-method", CategoryType, StageChecker, owner+" already has a method named "+name)
}
func TypeAlreadyHasMember(owner, name string) Message {
	return message("type.method-name-is-member", CategoryType, StageChecker, owner+" already has a member named "+name)
}
func MethodReceiverNotCopyable(name string) Message {
	return message("type.method-receiver-not-copyable", CategoryType, StageChecker, "method receiver must be shallow-copyable; got "+name)
}
func CanonicalConstructorCall(name string) Message {
	return message("type.canonical-constructor-call", CategoryType, StageChecker, "constructors use '"+name+"(...)', not '.new(...)'")
}
func TypeHasNoMethod(owner, name string) Message {
	return message("type.no-such-method", CategoryType, StageChecker, owner+" has no method named "+name)
}

func ReceiverPointerIncompatible(method, expected, place, actual string) Message {
	return message("type.receiver-pointer-incompatible", CategoryType, StageChecker,
		fmt.Sprintf("%s needs %s; @%s is %s", method, expected, place, actual))
}

func ReceiverTypeIncompatible(method, expected, place, actual string) Message {
	return message("type.receiver-type-incompatible", CategoryType, StageChecker,
		fmt.Sprintf("%s needs %s; %s is %s", method, expected, place, actual))
}
