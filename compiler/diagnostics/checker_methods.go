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
func MethodReceiverMustBeNominal(actual string) Message {
	return message("type.method-receiver-must-be-nominal", CategoryType, StageChecker, "method receiver must be a struct or union type; got "+actual)
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

// MethodWritesFixedReceiver reports a call to a mut method whose receiver
// cannot take the write. A for binder adds the way to write through the
// collection instead.
func MethodWritesFixedReceiver(owner, name, receiver string, forBinder bool) Message {
	text := fmt.Sprintf("mut method %s.%s requires a writable receiver, but %s is not writable", owner, name, receiver)
	if forBinder {
		text += fmt.Sprintf("; write through the collection instead: for i, x in xs do xs[i].%s(...) end", name)
	}
	return message("type.method-writes-fixed-receiver", CategoryType, StageChecker, text)
}

// ReadonlyMethodWritesSelf reports a method declared without mut that writes
// receiver-owned storage, takes a writable address of it, or calls a mut
// method on it.
func ReadonlyMethodWritesSelf(owner, name, action string) Message {
	return message("type.readonly-method-writes-self", CategoryType, StageChecker,
		fmt.Sprintf("method %s.%s is not declared mut but %s; declare it method mut %s.%s", owner, name, action, owner, name))
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
